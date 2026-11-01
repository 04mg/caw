package agents

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/04mg/caw/internal/agent"
)

// CommandCodeWatcher tracks Command Code (commandcode.ai) sessions by
// watching its append-only JSONL transcripts under ~/.commandcode/projects.
type CommandCodeWatcher struct{}

func init() {
	agent.RegisterStatusWatcher("commandcode", &CommandCodeWatcher{})
}

// commandCodeHeader mirrors the first JSONL line of a session transcript:
// {"type":"session","version":N,"id":<id>,"timestamp":<iso>,"cwd":<cwd>}.
type commandCodeHeader struct {
	Type string `json:"type"`
	ID   string `json:"id"`
	Cwd  string `json:"cwd"`
}

// commandCodeEntry mirrors a transcript entry. Entry ids form a tree via
// parentId; each line is appended when a turn commits, so the last message
// entry reflects the most recent durable state.
type commandCodeEntry struct {
	Type      string          `json:"type"`
	ID        string          `json:"id"`
	ParentID  *string         `json:"parentId"`
	Timestamp string          `json:"timestamp"`
	Message   *commandCodeMsg `json:"message,omitempty"`
	// session_info entries carry an optional display name (set by /rename).
	Name string `json:"name,omitempty"`
}

type commandCodeMsg struct {
	Role    string             `json:"role"`
	Content []commandCodeBlock `json:"content"`
}

// commandCodeBlock mirrors the Anthropic-style content blocks Command Code
// stores in message entries: {"type":"text","text":...},
// {"type":"tool_use","id","name","input"}, or
// {"type":"tool_result","tool_use_id","content","is_error"?}.
type commandCodeBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	IsError   bool            `json:"is_error,omitempty"`
	Content   json.RawMessage `json:"content,omitempty"`
}

// Command Code persists each model iteration to the transcript only when the
// iteration completes: the assistant tool_use message and its tool_result are
// written together, and nothing is appended while a tool is still running (a
// long-running tool freezes the transcript for minutes). A brand-new session
// does not even have a transcript file until its first iteration completes —
// only the sidecar <id>.checkpoints.jsonl, appended the moment a turn is
// submitted. The pane's PTY and that sidecar are the only live signals while
// the transcript is frozen.
//
// Raw PTY activity is NOT usable as a liveness signal: Command Code re-emits
// its terminal title (OSC 0/2) every few seconds even while idle or blocked on
// a question. Instead the watcher reads the rendered screen markers recorded
// by the agent package (agent.LastPtyBusy / LastPtyPendingInput /
// LastPtyToolRunning):
//   - "esc to interrupt": the spinner status line, repainted continuously
//     while a turn is in flight (LLM wait or tool run) and hidden as soon as
//     the agent blocks on the user or finishes.
//   - "navigate · enter select" / "type something..." / "to go back and
//     edit": the tool-permission and ask_user_question screens.
//   - "processing…": rendered under a tool block while the tool runs.
//
// ptyWorkingWindow: how recently the busy status line must have been drawn
// for the agent to count as actively working.
// promptBusyGrace: how long the busy line must have been gone, with a prompt
// screen drawn after it, before reporting waiting_input.
// inputWaitThreshold: how long the busy line may be gone mid-turn, with no
// recognised prompt screen, before we conclude Command Code is blocked
// awaiting a user answer on a screen we do not recognise.
const (
	ptyWorkingWindow   = 5 * time.Second
	promptBusyGrace    = 1500 * time.Millisecond
	inputWaitThreshold = 10 * time.Second
)

// isCommandCodeWorking reports whether a status counts as an active turn.
func isCommandCodeWorking(status string) bool {
	switch status {
	case "thinking", "executing", "tool_failed":
		return true
	}
	return false
}

func (w *CommandCodeWatcher) Watch(ctx context.Context, sessionID string, cwd string, resume bool, callback func(status, tool, details, title string), heartbeat func()) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".commandcode", "projects")
	const agentID = "commandcode"
	// Command Code catalogs sessions per project under ~/.commandcode/projects
	// keyed by a slug of the working directory. Rather than replicating the
	// slug encoding, candidate files are matched by the cwd stored in each
	// transcript header. Claims are keyed globally per agent like the Codex
	// watcher: every Command Code pane shares one claim namespace regardless of
	// its Caw-side cwd, but a session file is only ever claimed by a watcher
	// whose cwd matches the file's header cwd.
	const claimCwd = ""
	lookback := 10 * time.Second
	if resume {
		lookback = 1 * time.Hour
	}
	lastCheck := time.Now().Add(-lookback)
	var lastFileSize int64 = 0
	// transcriptMessageCount tracks how many message entries have been
	// committed to the transcript so far. Command Code appends a checkpoint to
	// the sidecar the moment a turn is submitted but only commits the user's
	// prompt to the transcript when the turn's first iteration completes, so a
	// checkpoint messageCount at or above this count signals a pending turn.
	var transcriptMessageCount int64
	var watchedFilePath string
	// pendingCreate is true while the watcher is bound to a transcript path
	// that does not exist yet: a fresh session whose first turn was submitted
	// (its checkpoints sidecar exists) but not yet committed.
	var pendingCreate bool
	// replay marks the first read after binding to a pre-existing transcript
	// (reopen/rebind): its history must not produce live transitions.
	var replay bool
	// resumeReplay is the launch-time resume flag, applied only to the first
	// transcript this watcher binds (the session the agent was resumed into);
	// later rebinds decide replay per session.
	resumeReplay := resume
	var sessionTitle string
	var lastActivity time.Time
	var silentTicks int
	// lastReportedStatus tracks the status most recently sent to the UI so
	// the PTY-interrupt pass (below) can tell whether the turn is working.
	var lastReportedStatus string
	var lastReportedTool string
	var lastReportedDetails string
	// lastWorkingAt marks when the current working turn began. PTY input is
	// handled independently from transcript polling, so an interrupt sent
	// while the card was idle can otherwise be observed after a new prompt has
	// already made the card working. Only interrupts sent during this turn can
	// cancel it.
	var lastWorkingAt time.Time
	// PTY-interrupt detection. Command Code never persists an interrupted
	// turn (an in-flight turn is dropped from the transcript), so
	// transcript-only detection leaves the card showing the stale working
	// state after a Ctrl+C. We therefore also watch for the interrupt byte
	// the user sends into the Command Code PTY and report "interrupted"
	// immediately while the turn is working. interruptApplied stays sticky
	// until a genuinely NEW user prompt lands past interruptBoundarySize,
	// because the transcript may keep showing the pre-interrupt working
	// state until the next prompt is written.
	// Ignore an interrupt that predates this watcher. A reopened Command Code
	// session can inherit its terminal leaf's previous Ctrl+C, but that says
	// nothing about the restored conversation.
	lastInterruptSeen := agent.LastPtyInterrupt(sessionID)
	var interruptApplied bool
	var interruptBoundarySize int64
	// interruptCheckpoints is the number of checkpoint lines (submitted
	// turns) when the interrupt was applied. A new line means the user
	// submitted a fresh turn, which clears the sticky interrupt even though
	// Command Code has not committed anything to the transcript yet.
	var interruptCheckpoints int

	// setReported records the status sent to the UI and stamps the start of a
	// working turn on the transition into a working state. Stamping only on
	// the transition (not on every working report) matters: the transcript
	// flush Command Code performs when the user interrupts a tool ("Command
	// aborted") is itself parsed as "thinking", and must not move the turn
	// start past the interrupt it was caused by.
	setReported := func(status, tool, details string) {
		if isCommandCodeWorking(status) && !isCommandCodeWorking(lastReportedStatus) {
			lastWorkingAt = time.Now()
		}
		lastReportedStatus = status
		lastReportedTool = tool
		lastReportedDetails = details
	}

	var notifyCh <-chan struct{}
	notifier, nerr := NewFileChangeNotifier()
	if nerr == nil {
		defer notifier.Close()
		notifyCh = notifier.Notify()
	}

	defer func() {
		if watchedFilePath != "" {
			UnclaimSession(agentID, claimCwd, watchedFilePath)
		}
	}()

	// matchesCwd reports whether a transcript file belongs to the given cwd
	// by reading its header line. Corrupt/unreadable files are skipped.
	matchesCwd := func(path, wantCwd string) bool {
		if wantCwd == "" {
			return true
		}
		head, err := ReadFirstLine(path)
		if err != nil {
			return false
		}
		var h commandCodeHeader
		if json.Unmarshal([]byte(head), &h) != nil {
			return false
		}
		return h.Type == "session" && h.Cwd == wantCwd
	}

	readWatched := func() bool {
		if watchedFilePath == "" {
			return false
		}
		info, err := os.Stat(watchedFilePath)
		if err != nil {
			if pendingCreate {
				// The first turn has not been committed yet; keep waiting
				// for the transcript to appear.
				return false
			}
			UnclaimSession(agentID, claimCwd, watchedFilePath)
			watchedFilePath = ""
			if notifyCh != nil {
				notifier.Watch("")
			}
			return false
		}
		if pendingCreate {
			pendingCreate = false
			if !matchesCwd(watchedFilePath, cwd) {
				UnclaimSession(agentID, claimCwd, watchedFilePath)
				watchedFilePath = ""
				if notifyCh != nil {
					notifier.Watch("")
				}
				return false
			}
		}
		if info.Size() <= lastFileSize {
			return false
		}
		// The first read consumes a pre-existing transcript when reopening a
		// session. Its final user message is historical, not proof that the
		// restored session is currently running. Subsequent appended messages
		// still report their normal live status.
		initialRead := lastFileSize == 0
		// New agent activity past the interrupt boundary clears the sticky
		// interrupt. Command Code never persists an interrupted turn, so the
		// transcript stops growing after a real interrupt until the user
		// starts a new turn; conversely, new assistant work (tool_use/text)
		// or a fresh plain-text prompt means the agent is running again and
		// the card must reflect the real state instead of staying stuck on
		// "interrupted". Checked before parse so the override below does not
		// re-apply to the resumed turn.
		if interruptApplied && hasNewCommandCodeWork(watchedFilePath, interruptBoundarySize) {
			interruptApplied = false
		}
		replaying := initialRead && (replay || resumeReplay)
		replay = false
		var pStatus, pTool, pDetails string
		var parsed bool
		n := w.parseCommandCodeLog(watchedFilePath, lastFileSize, cwd, sessionID, func(status, tool, details, title string) {
			if title != "" {
				sessionTitle = title
			}
			pStatus, pTool, pDetails, parsed = status, tool, details, true
		})
		transcriptMessageCount += int64(n)
		if parsed {
			status, tool, details := pStatus, pTool, pDetails
			if resumeReplay && initialRead {
				switch status {
				case "thinking", "executing", "tool_failed", "waiting_input":
					status, tool, details = "idle", "", ""
				}
			}
			if interruptApplied && isCommandCodeWorking(status) {
				status = "interrupted"
			}
			// Command Code commits a whole iteration at once, so a single
			// read regularly covers a turn that started and finished between
			// two polls (or the rest of a turn after the user answered a
			// prompt). Report the working state first so the "finished"
			// notification (working -> idle) still fires. Not when replaying
			// a pre-existing transcript on bind.
			if status == "idle" && n > 0 && !replaying && !isCommandCodeWorking(lastReportedStatus) {
				setReported("thinking", "", "")
				callback("thinking", "", "", sessionTitle)
			}
			setReported(status, tool, details)
			callback(status, tool, details, sessionTitle)
		}
		lastFileSize = info.Size()
		lastActivity = info.ModTime()
		silentTicks = 0
		resumeReplay = false
		return true
	}

	// findPendingSession looks for a session of this cwd whose first turn
	// was submitted (its <id>.checkpoints.jsonl exists, modified after
	// `after`) but whose transcript has not been written yet. The sidecar is
	// located through the per-cwd project directory. Only the pane that
	// submitted the turn may claim it: its PTY drew the busy status line
	// around the time the checkpoint was written — otherwise an idle sibling
	// pane in the same cwd could claim the other pane's pending session.
	findPendingSession := func(after time.Time) (string, FileCandidate, bool) {
		busy := agent.LastPtyBusy(sessionID)
		if busy.IsZero() {
			return "", FileCandidate{}, false
		}
		slugDir := filepath.Join(dir, commandCodeProjectSlug(cwd))
		cks, err := FindEarliestFiles(slugDir, ".checkpoints.jsonl", after)
		if err != nil {
			return "", FileCandidate{}, false
		}
		for _, c := range cks {
			if filepath.Dir(c.Path) != slugDir {
				continue
			}
			tp := strings.TrimSuffix(c.Path, ".checkpoints.jsonl") + ".jsonl"
			if tp == watchedFilePath {
				continue
			}
			if _, err := os.Stat(tp); err == nil {
				continue // an existing transcript is matched by its header
			}
			if busy.Before(c.ModTime.Add(-5 * time.Second)) {
				continue
			}
			if ClaimedBy(agentID, claimCwd, tp) != "" {
				continue
			}
			return tp, c, true
		}
		return "", FileCandidate{}, false
	}

	// bindPending binds the watcher to a pending (not yet written) transcript.
	bindPending := func(tp string, ck FileCandidate) bool {
		if !ClaimSessionForLeaf(agentID, claimCwd, tp, sessionID) {
			return false
		}
		if watchedFilePath != "" {
			UnclaimSession(agentID, claimCwd, watchedFilePath)
			// A new session (/clear, /new) starts without the old title.
			sessionTitle = ""
		}
		watchedFilePath = tp
		pendingCreate = true
		resumeReplay = false
		if sessionTitle == "" {
			sessionTitle = CleanPrompt(commandCodeFirstCheckpointPrompt(ck.Path))
		}
		lastFileSize = 0
		replay = false
		transcriptMessageCount = 0
		lastCheck = time.Now()
		lastActivity = ck.ModTime
		silentTicks = 0
		lastReportedStatus = ""
		if notifyCh != nil {
			notifier.Watch(watchedFilePath)
		}
		agent.RecordExternalSession(sessionID, tp)
		return true
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-notifyCh:
			readWatched()
		case <-ticker.C:
			heartbeat()
			if watchedFilePath == "" {
				// Prefer the exact transcript path persisted for this leaf by
				// a previous Caw process so a reopened pane resumes its own
				// session when several Command Code panes share a cwd.
				if exact := agent.PersistedExternalSession(sessionID); exact != "" {
					if isCommandCodeTranscript(exact) && matchesCwd(exact, cwd) {
						if ClaimSessionForLeaf(agentID, claimCwd, exact, sessionID) {
							watchedFilePath = exact
							lastFileSize = 0
							replay = true
							transcriptMessageCount = 0
							lastCheck = time.Now()
							if info, err := os.Stat(exact); err == nil {
								lastActivity = info.ModTime()
							}
							silentTicks = 0
							if notifyCh != nil {
								notifier.Watch(watchedFilePath)
							}
							agent.RecordExternalSession(sessionID, exact)
						}
					}
				}
				if watchedFilePath == "" {
					candidates, err := FindEarliestFiles(dir, ".jsonl", lastCheck)
					if err != nil {
						continue
					}
					for _, c := range candidates {
						if !isCommandCodeTranscript(c.Path) || !matchesCwd(c.Path, cwd) {
							continue
						}
						if ClaimSessionForLeaf(agentID, claimCwd, c.Path, sessionID) {
							watchedFilePath = c.Path
							lastFileSize = 0
							replay = resume
							transcriptMessageCount = 0
							lastCheck = time.Now()
							lastActivity = c.ModTime
							silentTicks = 0
							if notifyCh != nil {
								notifier.Watch(watchedFilePath)
							}
							agent.RecordExternalSession(sessionID, c.Path)
							break
						}
					}
				}
				if watchedFilePath == "" {
					// Fresh session: Command Code has not written the
					// transcript yet (it does so only when the first turn's
					// first iteration completes), but the checkpoints sidecar
					// appears the moment the first prompt is submitted. Bind
					// to the transcript path it will use, so the pending
					// first turn, its permission prompts and its completion
					// are tracked.
					if tp, ck, ok := findPendingSession(lastCheck); ok {
						bindPending(tp, ck)
					}
				}
			}
			if watchedFilePath != "" {
				advanced := readWatched()
				if !advanced {
					silentTicks++
				}

				// PTY-interrupt detection (see the interruptApplied comment
				// above): a fresh Ctrl+C while the turn is working
				// immediately flips the card to "interrupted" instead of
				// waiting for a transcript marker Command Code never writes
				// for an interrupted turn.
				ckLines, ckMax := commandCodeCheckpoints(watchedFilePath)
				last := agent.LastPtyInterrupt(sessionID)
				if last.After(lastInterruptSeen) {
					lastInterruptSeen = last
					if last.After(lastWorkingAt) && isCommandCodeWorking(lastReportedStatus) {
						interruptApplied = true
						interruptBoundarySize = lastFileSize
						interruptCheckpoints = ckLines
						setReported("interrupted", lastReportedTool, lastReportedDetails)
						callback("interrupted", lastReportedTool, lastReportedDetails, sessionTitle)
					}
				}
				// A turn submitted after the interrupt (a new checkpoint line)
				// clears the sticky interrupt right away, so the live-state
				// pass below can track the new turn before Command Code
				// commits anything for it.
				if interruptApplied && ckLines > interruptCheckpoints {
					interruptApplied = false
					if lastReportedStatus == "interrupted" {
						lastReportedStatus = ""
					}
				}

				// Live-state override for frozen transcripts (see the
				// persistence note near the constants above). While the
				// transcript is frozen, the rendered screen is the only live
				// signal: the busy status line means the agent is working (a
				// running tool block on top of it means it is executing), a
				// prompt screen drawn after the busy line went away means it
				// is blocked on the user (tool approval / ask_user_question).
				if !advanced {
					busy := agent.LastPtyBusy(sessionID)
					prompt := agent.LastPtyPendingInput(sessionID)
					if !busy.IsZero() || !prompt.IsZero() {
						pendingTurn := ckMax >= transcriptMessageCount
						active := pendingTurn || isCommandCodeWorking(lastReportedStatus) || lastReportedStatus == "waiting_input"
						if active && lastReportedStatus != "interrupted" && !interruptApplied {
							sinceBusy := time.Since(busy)
							switch {
							case prompt.After(busy) && sinceBusy >= promptBusyGrace:
								if lastReportedStatus != "waiting_input" {
									setReported("waiting_input", "", "")
									callback("waiting_input", "", "", sessionTitle)
								}
							case !busy.IsZero() && sinceBusy < ptyWorkingWindow:
								status := "thinking"
								if tr := agent.LastPtyToolRunning(sessionID); !tr.IsZero() && time.Since(tr) < ptyWorkingWindow {
									status = "executing"
								}
								tool := ""
								if status == lastReportedStatus {
									tool = lastReportedTool
								}
								if lastReportedStatus != status && lastReportedStatus != "tool_failed" {
									setReported(status, tool, "")
									callback(status, tool, "", sessionTitle)
								}
							case !busy.IsZero() && sinceBusy >= inputWaitThreshold:
								if lastReportedStatus != "waiting_input" {
									setReported("waiting_input", "ask_user_question", "")
									callback("waiting_input", "ask_user_question", "", sessionTitle)
								}
							}
						}
					}
				}

				// Mid-session re-bind for /new and /resume. Gated on the
				// pane actually working (its busy status line was drawn
				// recently) OR user focus so an idle sibling pane never
				// steals a session from an active one. Raw PTY activity is
				// not a usable gate here: Command Code re-emits its terminal
				// title every few seconds even when idle, which let an idle
				// sibling grab the session another pane had just left after
				// /clear. Shares the codex-style ShouldRebind heuristic over
				// the project transcript pool.
				if silentTicks >= rebindSilenceTicks {
					focused := agent.IsPtyFocused(sessionID)
					lastBusy := agent.LastPtyBusy(sessionID)
					if (!lastBusy.IsZero() && time.Since(lastBusy) < 3*time.Second) || focused {
						cands, _ := FindLatestFiles(dir, ".jsonl", lastActivity)
						var others []RebindCandidate
						for _, c := range cands {
							if !isCommandCodeTranscript(c.Path) || !matchesCwd(c.Path, cwd) {
								continue
							}
							others = append(others, RebindCandidate{Key: c.Path, ModTime: c.ModTime})
						}
						// Skip candidates owned by sibling panes so their
						// sessions never mask a pending session of ours.
						var free []RebindCandidate
						for _, o := range others {
							if owner := ClaimedBy(agentID, claimCwd, o.Key); owner == "" || owner == sessionID {
								free = append(free, o)
							}
						}
						rebound := false
						newKey := ShouldRebind(silentTicks, watchedFilePath, lastActivity, free)
						if newKey != "" && newKey != watchedFilePath {
							if ClaimSessionForLeaf(agentID, claimCwd, newKey, sessionID) {
								rebound = true
								UnclaimSession(agentID, claimCwd, watchedFilePath)
								// /new (a session created after this pane's last
								// activity) is live; /resume replays history.
								replay = !sessionStartedAfter(commandCodeSessionStart(newKey), lastActivity)
								watchedFilePath = newKey
								pendingCreate = false
								lastFileSize = 0
								transcriptMessageCount = 0
								lastCheck = time.Now()
								silentTicks = 0
								if notifyCh != nil {
									notifier.Watch(watchedFilePath)
								}
								agent.RecordExternalSession(sessionID, newKey)
							}
						}
						if !rebound && !pendingCreate {
							// /clear or /new followed by a prompt: the new
							// session has only its checkpoints sidecar until
							// the first iteration completes. Follow it now so
							// the new turn is tracked while it runs.
							if tp, ck, ok := findPendingSession(lastActivity); ok {
								bindPending(tp, ck)
							}
						}
					}
				}
			}
		}
	}
}

// isCommandCodeTranscript excludes sidecar files (checkpoints, prompts) and
// migration backups from the candidate pool.
func isCommandCodeTranscript(path string) bool {
	name := filepath.Base(path)
	if strings.Contains(name, ".checkpoints.") || strings.Contains(name, ".prompts.") || strings.Contains(name, ".v2.bak") {
		return false
	}
	return strings.HasSuffix(name, ".jsonl")
}

// commandCodeTitleFromMeta reads the display title from the session's
// sidecar <id>.meta.json, falling back to "" when absent.
func commandCodeTitleFromMeta(path string) string {
	metaPath := strings.TrimSuffix(path, ".jsonl") + ".meta.json"
	head, err := ReadFileHead(metaPath, 4096)
	if err != nil {
		return ""
	}
	var meta struct {
		Title string `json:"title"`
	}
	if json.Unmarshal([]byte(head), &meta) != nil {
		return ""
	}
	return meta.Title
}

// parseCommandCodeLog reads the transcript bytes appended since offset and
// reports the derived status. It returns the number of message entries parsed,
// which the watcher accumulates to track how many messages have been committed
// to the transcript (used to detect turns that are pending their first commit).
func (w *CommandCodeWatcher) parseCommandCodeLog(filePath string, offset int64, cwd, sessionID string, callback func(status, tool, details, title string)) int {
	lines, err := ReadNewLines(filePath, offset)
	if err != nil || len(lines) == 0 {
		return 0
	}

	// Prefer the persisted title from the sidecar meta file; fall back to the
	// first plain user prompt below.
	sessionTitle := commandCodeTitleFromMeta(filePath)

	// Parse all lines, skipping the session header, keeping message entries in
	// order so both the title scan (first user text) and the status scan (last
	// meaningful message) can run over the same slice.
	var entries []commandCodeEntry
	var userText string
	for _, line := range lines {
		var e commandCodeEntry
		if json.Unmarshal([]byte(line), &e) != nil {
			continue
		}
		if e.Type == "session" {
			continue
		}
		if e.Type == "session_info" {
			if sessionTitle == "" && e.Name != "" {
				sessionTitle = e.Name
			}
			continue
		}
		if e.Type == "message" && e.Message != nil {
			entries = append(entries, e)
			if e.Message.Role == "user" && userText == "" {
				userText = commandCodeUserText(e.Message.Content)
			}
		}
	}
	if sessionTitle == "" {
		sessionTitle = CleanPrompt(userText)
	}

	// Walk entries backwards to find the last meaningful message.
	//
	// Command Code persists each iteration's tool call together with its
	// result once the tool finishes, so an assistant tool_use whose id has no
	// matching tool_result anywhere is a tool that has NOT run yet: the agent
	// is paused waiting for the user to approve/answer the request. Build the
	// set of answered tool_use ids first so that check has full transcript
	// visibility regardless of scan order.
	answered := make(map[string]bool)
	for _, e := range entries {
		if e.Message == nil {
			continue
		}
		for _, b := range e.Message.Content {
			if b.Type == "tool_result" && b.ToolUseID != "" {
				answered[b.ToolUseID] = true
			}
		}
	}

	var status, tool, details string
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		msg := e.Message
		if msg == nil {
			continue
		}
		switch msg.Role {
		case "assistant":
			var lastTool string
			var lastToolID string
			var hasText bool
			var failedErr string
			for _, b := range msg.Content {
				switch b.Type {
				case "tool_use":
					lastTool = b.Name
					lastToolID = b.ID
					if lastTool == "" {
						lastTool = "exec"
					}
				case "text":
					if b.Text != "" {
						hasText = true
					}
				case "tool_result":
					if b.IsError && b.ToolUseID != "" && failedErr == "" {
						failedErr = "tool call failed"
					}
				}
			}
			if lastTool != "" {
				if isUserInputTool(strings.ToLower(lastTool)) {
					status, tool, details = "waiting_input", lastTool, ""
				} else if lastToolID != "" && !answered[lastToolID] {
					// The tool request was persisted but never produced a
					// result: Command Code is blocked on user approval
					// (e.g. Execute Shell Command). Report need-input, not
					// executing.
					status, tool, details = "waiting_input", lastTool, ""
				} else if failedErr != "" {
					status, tool, details = "tool_failed", lastTool, failedErr
				} else {
					status, tool, details = "executing", lastTool, ""
				}
				break
			}
			if hasText {
				status, tool, details = "idle", "", commandCodeText(msg.Content)
				break
			}
			status, tool, details = "idle", "", ""
			break
		case "user":
			// A pure tool-result message means the agent just ran a tool and
			// is now processing the output. A plain text message is a fresh
			// prompt awaiting a response. Either way the agent is working.
			if failedToolResult(msg.Content) {
				toolName := failedToolName(msg.Content)
				errText := failedToolError(msg.Content)
				if toolName != "" {
					status, tool, details = "tool_failed", toolName, errText
					break
				}
			}
			status, tool, details = "thinking", "", ""
			break
		default:
			continue
		}
		break
	}
	if status == "" {
		status = "idle"
	}

	callback(status, tool, details, sessionTitle)
	return len(entries)
}

// commandCodeCheckpoints returns the number of turns recorded in the
// session's sidecar checkpoints file (<id>.checkpoints.jsonl) and the highest
// messageCount among them, or (0, -1) when the sidecar is missing. Command
// Code appends a checkpoint the instant a turn is submitted; its messageCount
// reflects the messages committed up to that moment, so a count at or above
// the committed transcript means a turn whose prompt has not yet been flushed
// to the transcript (see the persistence note near the Watch constants).
func commandCodeCheckpoints(filePath string) (int, int64) {
	ckptPath := strings.TrimSuffix(filePath, ".jsonl") + ".checkpoints.jsonl"
	lines, err := ReadNewLines(ckptPath, 0)
	if err != nil {
		return 0, -1
	}
	var maxCount int64 = -1
	n := 0
	for _, line := range lines {
		var c struct {
			MessageCount int64 `json:"messageCount"`
		}
		if json.Unmarshal([]byte(line), &c) != nil {
			continue
		}
		n++
		if c.MessageCount > maxCount {
			maxCount = c.MessageCount
		}
	}
	return n, maxCount
}

// commandCodeSessionStart returns the creation time recorded in a
// transcript's session header, or the zero time.
func commandCodeSessionStart(path string) time.Time {
	head, err := ReadFirstLine(path)
	if err != nil {
		return time.Time{}
	}
	var h struct {
		Type      string `json:"type"`
		Timestamp string `json:"timestamp"`
	}
	if json.Unmarshal([]byte(head), &h) != nil || h.Type != "session" {
		return time.Time{}
	}
	t, _ := time.Parse(time.RFC3339Nano, h.Timestamp)
	return t
}

// commandCodeFirstCheckpointPrompt returns the prompt of the first turn
// recorded in a checkpoints sidecar, used as the card title until the
// transcript (and its generated title) exists.
func commandCodeFirstCheckpointPrompt(ckptPath string) string {
	head, err := ReadFirstLine(ckptPath)
	if err != nil {
		return ""
	}
	var c struct {
		Prompt string `json:"prompt"`
	}
	if json.Unmarshal([]byte(head), &c) != nil {
		return ""
	}
	return c.Prompt
}

// commandCodeProjectSlug mirrors how Command Code names the per-project
// directory under ~/.commandcode/projects: the cwd passed through
// @sindresorhus/slugify (decamelize, lowercase, every run of characters other
// than a-z/0-9 collapsed to "-", leading/trailing dashes trimmed), or "root"
// when nothing is left. Only used to locate the checkpoints sidecar of a
// session whose transcript does not exist yet; transcripts themselves are
// always matched by the cwd recorded in their header.
func commandCodeProjectSlug(cwd string) string {
	var b strings.Builder
	prevLowerOrDigit := false
	dash := false
	for _, r := range cwd {
		switch {
		case r >= 'A' && r <= 'Z':
			if prevLowerOrDigit {
				dash = true
			}
			if dash && b.Len() > 0 {
				b.WriteByte('-')
			}
			dash = false
			b.WriteRune(r + ('a' - 'A'))
			prevLowerOrDigit = false
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			if dash && b.Len() > 0 {
				b.WriteByte('-')
			}
			dash = false
			b.WriteRune(r)
			prevLowerOrDigit = true
		default:
			dash = true
			prevLowerOrDigit = false
		}
	}
	if b.Len() == 0 {
		return "root"
	}
	return b.String()
}

// commandCodeUserText extracts the plain text of a user message, ignoring
// tool_result blocks.
func commandCodeUserText(blocks []commandCodeBlock) string {
	var parts []string
	for _, b := range blocks {
		if b.Type == "text" && b.Text != "" {
			parts = append(parts, b.Text)
		}
	}
	return strings.Join(parts, " ")
}

// commandCodeText joins the visible text blocks of an assistant message for
// the card Details line.
func commandCodeText(blocks []commandCodeBlock) string {
	var parts []string
	for _, b := range blocks {
		if b.Type == "text" && b.Text != "" {
			parts = append(parts, b.Text)
		}
	}
	return strings.Join(parts, " ")
}

// failedToolResult reports whether a user message carries a tool_result block
// marked as an error (the tool call failed).
func failedToolResult(blocks []commandCodeBlock) bool {
	for _, b := range blocks {
		if b.Type == "tool_result" && b.IsError {
			return true
		}
	}
	return false
}

// failedToolName resolves the name of the tool whose result failed by scanning
// for a preceding tool_use block referencing the same tool_use_id.
func failedToolName(blocks []commandCodeBlock) string {
	for _, b := range blocks {
		if b.Type == "tool_use" && b.Name != "" {
			return b.Name
		}
	}
	return ""
}

// failedToolError returns a short human-readable message for a failed tool
// result. The raw output blob is arbitrary data, so only a generic message is
// produced to keep the card Details safe to render.
func failedToolError(blocks []commandCodeBlock) string {
	return "tool call failed"
}

// hasNewCommandCodeWork reports whether the unread portion of the transcript
// (bytes after offset) shows the agent resumed work: a fresh plain-text user
// prompt, or a new assistant message carrying tool_use/text blocks. Used to
// clear the sticky PTY interrupt. Command Code drops interrupted turns
// entirely, so any continued assistant work or a new prompt beyond the
// interrupt boundary means the interrupted state is stale.
func hasNewCommandCodeWork(filePath string, offset int64) bool {
	lines, err := ReadNewLines(filePath, offset)
	if err != nil {
		return false
	}
	for _, line := range lines {
		var e commandCodeEntry
		if json.Unmarshal([]byte(line), &e) != nil || e.Type != "message" || e.Message == nil {
			continue
		}
		switch e.Message.Role {
		case "user":
			if commandCodeUserText(e.Message.Content) != "" {
				return true
			}
		case "assistant":
			for _, b := range e.Message.Content {
				if b.Type == "tool_use" && b.Name != "" {
					return true
				}
				if b.Type == "text" && b.Text != "" {
					return true
				}
			}
		}
	}
	return false
}
