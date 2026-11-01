package agents

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/04mg/caw/internal/agent"
	_ "modernc.org/sqlite"
)

type AntigravityWatcher struct{}

func init() {
	agent.RegisterStatusWatcher("agy", &AntigravityWatcher{})
}

// antigravityLeafPID resolves the PID of the agy process running in a leaf.
// Overridable in tests.
var antigravityLeafPID = agent.LeafPID

// antigravityPollInterval is the fallback poll period; fsnotify on the
// transcript and the conversation DB's WAL delivers most updates sooner.
const antigravityPollInterval = 2 * time.Second

// antigravityBinding is the conversation a watcher is currently following.
type antigravityBinding struct {
	conv       string
	transcript string
	dbPath     string
	tx         *agyTranscript
	// staleBefore: see deriveAntigravityStatus.
	staleBefore int
}

func (w *AntigravityWatcher) Watch(ctx context.Context, sessionID string, cwd string, resume bool, callback func(status, tool, details, title string), heartbeat func()) {
	ticker := time.NewTicker(antigravityPollInterval)
	defer ticker.Stop()

	// Antigravity stores everything under ~/.gemini/antigravity-cli:
	//   brain/<conv>/.system_generated/logs/transcript.jsonl  (completed steps)
	//   conversations/<conv>.db                               (live step state)
	//   presence/<conv>.lock                                  (held open by the agy process)
	// Storage is global (no per-cwd subdirectories), so claims are keyed
	// globally (claimCwd = "") and two Antigravity panes anywhere in Caw never
	// follow the same conversation.
	home, _ := os.UserHomeDir()
	root := filepath.Join(home, ".gemini", "antigravity-cli")
	dir := filepath.Join(root, "brain")
	convDir := filepath.Join(root, "conversations")
	presenceDir := filepath.Join(root, "presence")

	const agentID = "agy"
	const claimCwd = ""
	startedAt := time.Now()
	// On resume (--continue / --conversation), the agent reattaches to a
	// pre-existing conversation whose transcript may predate this watcher.
	// Widen the heuristic search window to 1 hour so the resumed session is
	// found. For a fresh start, look back 10s.
	lookback := 10 * time.Second
	if resume {
		lookback = 1 * time.Hour
	}
	lastCheck := time.Now().Add(-lookback)

	var b antigravityBinding
	// Re-bind bookkeeping for the heuristic (non-PID) path.
	var lastActivity time.Time
	var silentTicks int
	var lastTxSize int64 = -1
	var lastEmit agyDerived
	var lastEmitTitle string
	emitted := false

	txNotifier, nerr := NewFileChangeNotifier()
	var txCh <-chan struct{}
	if nerr == nil {
		defer txNotifier.Close()
		txCh = txNotifier.Notify()
	}
	dbNotifier, nerr := NewFileChangeNotifier()
	var dbCh <-chan struct{}
	if nerr == nil {
		defer dbNotifier.Close()
		dbCh = dbNotifier.Notify()
	}

	defer func() {
		if b.transcript != "" {
			UnclaimSession(agentID, claimCwd, b.transcript)
		}
	}()

	bind := func(conv string) bool {
		transcript := filepath.Join(dir, conv, ".system_generated", "logs", "transcript.jsonl")
		if transcript == b.transcript {
			return true
		}
		if !ClaimSessionForLeaf(agentID, claimCwd, transcript, sessionID) {
			return false
		}
		if b.transcript != "" {
			UnclaimSession(agentID, claimCwd, b.transcript)
		}
		b = antigravityBinding{
			conv:        conv,
			transcript:  transcript,
			dbPath:      filepath.Join(convDir, conv+".db"),
			tx:          newAgyTranscript(transcript),
			staleBefore: -1,
		}
		// A conversation that has not been written since before this
		// watcher started is being resumed: whatever turn or background
		// task it shows as in flight belonged to a previous agy process.
		if mt := antigravityConversationModTime(b.dbPath); !mt.IsZero() && mt.Before(startedAt.Add(-2*time.Second)) {
			if snap, err := readAntigravityDB(b.dbPath); err == nil {
				b.staleBefore = snap.MaxIdx
			}
		}
		lastCheck = time.Now()
		lastTxSize = -1
		silentTicks = 0
		if info, err := os.Stat(transcript); err == nil {
			lastActivity = info.ModTime()
		} else {
			lastActivity = time.Now()
		}
		if txNotifier != nil {
			txNotifier.Watch(transcript)
		}
		if dbNotifier != nil {
			dbNotifier.Watch(b.dbPath + "-wal")
		}
		agent.RecordExternalSession(sessionID, conv)
		return true
	}

	evaluate := func(keepalive bool) {
		if b.conv == "" {
			return
		}
		if b.tx.refresh() {
			silentTicks = 0
			if info, err := os.Stat(b.transcript); err == nil {
				lastActivity = info.ModTime()
			}
		}
		snap, err := readAntigravityDB(b.dbPath)
		haveDB := err == nil
		var term *agyTermination
		if haveDB {
			term = snap.Term
		} else {
			snap = nil
		}
		d := deriveAntigravityStatus(mergeAntigravitySteps(b.tx, snap), term, haveDB, b.staleBefore, b.tx.runningTasks)
		title := b.tx.title
		changed := !emitted || d.Status != lastEmit.Status || d.Tool != lastEmit.Tool || d.Details != lastEmit.Details || title != lastEmitTitle
		// While the agent is provably busy, re-emit on every poll so the
		// shared idle watchdog does not revert a long silent LLM wait or a
		// long background task to "unknown".
		if changed || (keepalive && d.Working) {
			callback(d.Status, d.Tool, d.Details, title)
			lastEmit, lastEmitTitle, emitted = d, title, true
		}
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-txCh:
			evaluate(false)
		case <-dbCh:
			evaluate(false)
		case <-ticker.C:
			heartbeat()

			// 1. Exact binding: the conversation whose presence lock the
			//    leaf's agy process holds. Follows /clear, /new and /resume
			//    immediately and never confuses sibling panes.
			//    When the process could be inspected but holds no lock yet,
			//    the agent has no conversation (agy creates it on the first
			//    prompt): stay as is rather than guessing from mtimes, which
			//    would let this pane grab a sibling's conversation.
			pidBound := false
			if pid := antigravityLeafPID(sessionID); pid > 0 {
				conv, inspected := antigravityPresenceConversation(pid, presenceDir)
				if conv != "" && (conv == b.conv || bind(conv)) {
					pidBound = true
				} else if inspected && conv == "" {
					pidBound = true
				}
			}

			// 2. Heuristic fallback (no procfs, or the lock is not visible).
			if !pidBound && b.conv == "" {
				// Prefer the exact conversation id persisted for this leaf by
				// a previous Caw process so a reopened pane follows its own
				// conversation when several panes share a cwd.
				if exact := agent.PersistedExternalSession(sessionID); exact != "" && antigravityConversationMatchesWorkspace(exact, cwd) {
					if antigravityTranscriptForConversation(dir, exact) != "" {
						bind(exact)
					}
				}
				if b.conv == "" {
					candidates, err := findAntigravityTranscripts(dir, cwd, lastCheck, agentID)
					if err == nil {
						for _, c := range candidates {
							if conv := antigravityConversationIDForTranscript(dir, c); conv != "" && bind(conv) {
								break
							}
						}
					}
				}
			}

			if b.conv != "" && !pidBound {
				// Heuristic mid-session re-bind for /new and /resume. Gated
				// on PTY activity OR user focus: only the watcher whose PTY is
				// producing output (or whose pane the user is driving)
				// switches, so a sibling Antigravity writing to its own
				// transcript can't make this idle, unfocused watcher steal
				// its session.
				size := int64(-1)
				if info, err := os.Stat(b.transcript); err == nil {
					size = info.Size()
				}
				if size == lastTxSize {
					silentTicks++
				} else {
					silentTicks = 0
					lastTxSize = size
				}
				if silentTicks >= rebindSilenceTicks {
					focused := agent.IsPtyFocused(sessionID)
					lastPtyOut := agent.LastPtyActivity(sessionID)
					if time.Since(lastPtyOut) < 3*time.Second || focused {
						cands, _ := listAntigravityCandidates(dir, cwd, lastActivity)
						var others []RebindCandidate
						for _, c := range cands {
							if cwd != "" && !c.workspaceOK {
								continue
							}
							others = append(others, RebindCandidate{Key: c.path, ModTime: c.modTime})
						}
						if newKey := ShouldRebind(silentTicks, b.transcript, lastActivity, others); newKey != "" && newKey != b.transcript {
							if conv := antigravityConversationIDForTranscript(dir, newKey); conv != "" {
								bind(conv)
							}
						}
					}
				}
			}

			evaluate(true)
		}
	}
}

// antigravityConversationModTime returns the newest modification time of a
// conversation DB and its WAL, or the zero time when neither exists.
func antigravityConversationModTime(dbPath string) time.Time {
	var newest time.Time
	for _, p := range []string{dbPath, dbPath + "-wal"} {
		if info, err := os.Stat(p); err == nil && info.ModTime().After(newest) {
			newest = info.ModTime()
		}
	}
	return newest
}

// antigravityConversationIDForTranscript returns the conversation id (the
// immediate subdirectory under the brain dir) that owns the given transcript,
// or "" when it cannot be determined.
func antigravityConversationIDForTranscript(brainDir, transcriptPath string) string {
	if brainDir == "" || transcriptPath == "" {
		return ""
	}
	rel, err := filepath.Rel(brainDir, transcriptPath)
	if err != nil {
		return ""
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) == 0 || parts[0] == "" || parts[0] == "." || strings.HasPrefix(parts[0], "..") {
		return ""
	}
	return parts[0]
}

// antigravityTranscriptForConversation returns the transcript.jsonl path for
// the given conversation id, or "" if it does not exist.
func antigravityTranscriptForConversation(brainDir, convID string) string {
	if convID == "" {
		return ""
	}
	p := filepath.Join(brainDir, convID, ".system_generated", "logs", "transcript.jsonl")
	if info, err := os.Stat(p); err == nil && !info.IsDir() {
		return p
	}
	return ""
}

// extractWorkspaceURIsFromBlob scans a binary blob (such as trajectory_metadata_blob
// in an Antigravity conversation database) for file:// URIs encoded as protobuf strings.
func extractWorkspaceURIsFromBlob(data []byte) []string {
	var uris []string
	prefix := []byte("file://")
	idx := 0
	for {
		pos := bytes.Index(data[idx:], prefix)
		if pos == -1 {
			break
		}
		actualPos := idx + pos
		if actualPos > 0 {
			var length int
			var shift uint
			varintStart := actualPos - 1
			if data[varintStart] < 0x80 {
				length = int(data[varintStart])
			} else {
				start := varintStart
				for start > 0 && data[start-1]&0x80 != 0 {
					start--
				}
				for i := start; i <= varintStart; i++ {
					b := data[i]
					length |= int(b&0x7f) << shift
					shift += 7
				}
			}
			if length > 0 && actualPos+length <= len(data) {
				uri := string(data[actualPos : actualPos+length])
				if strings.HasPrefix(uri, "file://") {
					uris = append(uris, uri)
				}
			}
		}
		idx = actualPos + len(prefix)
	}
	return uris
}

// antigravityConversationWorkspace finds all workspace filesystem paths associated
// with a given conversation ID. It inspects:
// 1. ~/.gemini/antigravity-cli/conversations/<convID>.db (trajectory_metadata_blob) - real-time
// 2. ~/.gemini/antigravity-cli/conversation_summaries.db (workspace_uris) - fallback
func antigravityConversationWorkspace(convID string) []string {
	if convID == "" {
		return nil
	}
	home, _ := os.UserHomeDir()
	if home == "" {
		return nil
	}

	var foundPaths []string
	seen := make(map[string]bool)
	addPath := func(p string) {
		if p == "" {
			return
		}
		clean := filepath.Clean(p)
		if !seen[clean] {
			seen[clean] = true
			foundPaths = append(foundPaths, clean)
		}
	}

	// 1. Check real-time per-conversation database.
	dbPath := filepath.Join(home, ".gemini", "antigravity-cli", "conversations", convID+".db")
	if info, err := os.Stat(dbPath); err == nil && !info.IsDir() {
		db, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro&_journal_mode=WAL")
		if err == nil {
			var blob []byte
			_ = db.QueryRow(`SELECT data FROM trajectory_metadata_blob WHERE id = 'main'`).Scan(&blob)
			if len(blob) > 0 {
				uris := extractWorkspaceURIsFromBlob(blob)
				for _, u := range uris {
					addPath(uriToPath(u))
				}
			}
			db.Close()
		}
	}

	if len(foundPaths) > 0 {
		return foundPaths
	}

	// 2. Fallback to conversation_summaries.db (written on session finish/summary).
	summariesPath := filepath.Join(home, ".gemini", "antigravity-cli", "conversation_summaries.db")
	if info, err := os.Stat(summariesPath); err == nil && !info.IsDir() {
		db, err := sql.Open("sqlite", "file:"+summariesPath+"?mode=ro&_journal_mode=WAL")
		if err == nil {
			var urisJSON string
			_ = db.QueryRow(`SELECT workspace_uris FROM conversation_summaries WHERE conversation_id = ?`, convID).Scan(&urisJSON)
			if urisJSON != "" {
				var uriList []string
				if json.Unmarshal([]byte(urisJSON), &uriList) == nil {
					for _, u := range uriList {
						addPath(uriToPath(u))
					}
				}
			}
			db.Close()
		}
	}

	return foundPaths
}

// antigravityConversationMatchesWorkspace reports whether the given conversation ID
// belongs to the specified workspace directory (cwd). If cwd is empty, returns true.
// If the conversation's workspace is unknown, returns true.
func antigravityConversationMatchesWorkspace(convID, cwd string) bool {
	if convID == "" {
		return false
	}
	if cwd == "" {
		return true
	}
	absCwd, err := filepath.Abs(cwd)
	if err == nil {
		absCwd = filepath.Clean(absCwd)
	} else {
		absCwd = filepath.Clean(cwd)
	}
	wsPaths := antigravityConversationWorkspace(convID)
	if len(wsPaths) == 0 {
		return true
	}
	for _, p := range wsPaths {
		if p == absCwd {
			return true
		}
	}
	return false
}

// findAntigravityTranscripts walks the brain directory looking for the most
// recently modified transcript.jsonl files whose modification time is after
// the given threshold, filtered by cwd, and returns candidates sorted oldest-first.
func findAntigravityTranscripts(brainDir string, cwd string, after time.Time, agentID string) ([]string, error) {
	cands, err := listAntigravityCandidates(brainDir, cwd, after)
	if err != nil {
		return nil, err
	}
	var result []string
	for _, c := range cands {
		if cwd != "" && !c.workspaceOK {
			continue
		}
		result = append(result, c.path)
	}
	return result, nil
}

// antigravityCandidate is a transcript path with its modification time, used
// both by findAntigravityTranscripts (claiming path) and the re-bind path
// (which needs modtimes without claiming).
type antigravityCandidate struct {
	path        string
	convID      string
	modTime     time.Time
	workspaceOK bool
}

// listAntigravityCandidates enumerates transcript.jsonl files under brainDir
// modified after `after`, filtered by cwd, sorted oldest-first
// (workspace-matched entries first within that ordering). It does NOT claim
// any candidate; callers are responsible for calling ClaimSessionForLeaf.
func listAntigravityCandidates(brainDir string, cwd string, after time.Time) ([]antigravityCandidate, error) {
	var absCwd string
	if cwd != "" {
		abs, err := filepath.Abs(cwd)
		if err == nil {
			absCwd = filepath.Clean(abs)
		} else {
			absCwd = filepath.Clean(cwd)
		}
	}

	var cands []antigravityCandidate
	err := filepath.Walk(brainDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			return nil
		}
		if info.Name() != "transcript.jsonl" {
			return nil
		}
		if !info.ModTime().After(after) {
			return nil
		}
		convID := ""
		if rel, rerr := filepath.Rel(brainDir, path); rerr == nil {
			parts := strings.Split(filepath.ToSlash(rel), "/")
			if len(parts) > 0 {
				convID = parts[0]
			}
		}
		if convID == "" {
			return nil
		}

		workspaceOK := false
		if absCwd != "" {
			wsPaths := antigravityConversationWorkspace(convID)
			if len(wsPaths) > 0 {
				for _, p := range wsPaths {
					if p == absCwd {
						workspaceOK = true
						break
					}
				}
				// If the workspace is known for this conversation and does not match absCwd, skip it!
				if !workspaceOK {
					return nil
				}
			}
		}

		cands = append(cands, antigravityCandidate{
			path:        path,
			convID:      convID,
			modTime:     info.ModTime(),
			workspaceOK: workspaceOK,
		})
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Sort: workspace-matched first, then by modTime ASCENDING (oldest first)
	// so the earliest-started watcher claims the earliest qualifying session.
	sort.SliceStable(cands, func(i, j int) bool {
		if cands[i].workspaceOK != cands[j].workspaceOK {
			return cands[i].workspaceOK
		}
		return cands[i].modTime.Before(cands[j].modTime)
	})
	return cands, nil
}

// uriToPath converts a file:// URI as stored in the Antigravity
// conversation_summaries.db workspace_uris column back into an OS filesystem
// path. Handles the triple-slash form ("file:///C:/...") and URL-encoded
// characters (e.g. "%20" for spaces) used by the CLI. Returns "" when the URI
// is not a file:// URI or cannot be parsed.
func uriToPath(uri string) string {
	if !strings.HasPrefix(uri, "file:") {
		return ""
	}
	u, err := url.Parse(uri)
	if err != nil || u.Scheme != "file" {
		return ""
	}
	p := u.Path
	// On Windows, url.Parse leaves a leading slash before the drive letter
	// (e.g. "/C:/Users/..."); strip it so the result is a valid drive path.
	if len(p) > 2 && p[0] == '/' && (p[2] == ':' || p[2] == '|') {
		p = p[1:]
	}
	// Restore any | that the URL encoder produced for colons on some
	// platforms, then convert to the OS-native separator.
	p = strings.ReplaceAll(p, "|", ":")
	return filepath.FromSlash(p)
}
