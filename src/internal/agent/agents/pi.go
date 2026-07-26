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

type PiWatcher struct{}

func init() {
	agent.RegisterStatusWatcher("pi", &PiWatcher{})
}

type PiMessage struct {
	Role         string    `json:"role"`
	Content      []PiBlock `json:"content"`
	StopReason   string    `json:"stopReason,omitempty"`
	ErrorMessage string    `json:"errorMessage,omitempty"`
}

type PiBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
	Name string `json:"name,omitempty"` // tool name
}

func (w *PiWatcher) Watch(ctx context.Context, sessionID string, cwd string, resume bool, callback func(status, tool, details, title string), heartbeat func()) {
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".pi", "agent", "sessions")
	watchPiFormatSessions(ctx, sessionID, cwd, resume, callback, heartbeat, piFormatWatchConfig{
		agentID:     "pi",
		sessionsDir: dir,
		projectDir: func(cleanCwd string) string {
			if cleanCwd == "" {
				return ""
			}
			// Upstream Pi encodes the absolute cwd as -<encoded-abs-path>-.
			return "-" + encodePathForDir(cleanCwd) + "-"
		},
	})
}

// piFormatWatchConfig parameterizes the shared Pi-compatible JSONL watcher so
// forks (Oh My Pi / omp) can reuse the poll/claim/rebind loop without copying it.
type piFormatWatchConfig struct {
	agentID     string
	sessionsDir string
	// projectDir maps a cleaned cwd to the per-project subdirectory name under
	// sessionsDir. Empty means "search the whole sessions tree".
	projectDir func(cleanCwd string) string
	// acceptPath, when non-nil, filters candidate transcript files after the
	// mtime scan (e.g. to skip nested subagent JSONL files).
	acceptPath func(path string) bool
}

func watchPiFormatSessions(
	ctx context.Context,
	sessionID string,
	cwd string,
	resume bool,
	callback func(status, tool, details, title string),
	heartbeat func(),
	cfg piFormatWatchConfig,
) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	dir := cfg.sessionsDir
	agentID := cfg.agentID
	// On resume (--continue), the agent reattaches to a pre-existing session
	// whose transcript may predate this watcher. Widen the search window to
	// 1 hour so the resumed session is found. For a fresh start, only look
	// for files modified after the watcher started (no negative offset).
	lookback := 10 * time.Second
	if resume {
		lookback = 1 * time.Hour
	}
	lastCheck := time.Now().Add(-lookback)
	var lastFileSize int64 = 0
	var watchedFilePath string
	var sessionTitle string
	// Re-bind bookkeeping for /new and /resume detection.
	var lastActivity time.Time
	var silentTicks int

	var notifyCh <-chan struct{}
	notifier, nerr := NewFileChangeNotifier()
	if nerr == nil {
		defer notifier.Close()
		notifyCh = notifier.Notify()
	}

	defer func() {
		if watchedFilePath != "" {
			UnclaimSession(agentID, cwd, watchedFilePath)
		}
	}()

	projectSearchDir := func() string {
		searchDir := dir
		if cwd == "" || cfg.projectDir == nil {
			return searchDir
		}
		cleanCwd := filepath.Clean(cwd)
		projDir := cfg.projectDir(cleanCwd)
		if projDir == "" {
			return searchDir
		}
		targetDir := filepath.Join(dir, projDir)
		if info, err := os.Stat(targetDir); err == nil && info.IsDir() {
			return targetDir
		}
		return searchDir
	}

	filterCandidates := func(cands []FileCandidate) []FileCandidate {
		if cfg.acceptPath == nil {
			return cands
		}
		out := make([]FileCandidate, 0, len(cands))
		for _, c := range cands {
			if cfg.acceptPath(c.Path) {
				out = append(out, c)
			}
		}
		return out
	}

	readWatched := func() bool {
		if watchedFilePath == "" {
			return false
		}
		info, err := os.Stat(watchedFilePath)
		if err != nil {
			UnclaimSession(agentID, cwd, watchedFilePath)
			watchedFilePath = ""
			if notifyCh != nil {
				notifier.Watch("")
			}
			return false
		}
		if info.Size() <= lastFileSize {
			return false
		}
		wrappedCallback := func(status, tool, details, title string) {
			if title != "" {
				sessionTitle = title
			}
			callback(status, tool, details, sessionTitle)
		}
		parsePiFormatLog(watchedFilePath, lastFileSize, resume, wrappedCallback)
		lastFileSize = info.Size()
		lastActivity = info.ModTime()
		silentTicks = 0
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
				searchDir := projectSearchDir()
				candidates, err := FindEarliestFiles(searchDir, ".jsonl", lastCheck)
				if err != nil {
					continue
				}
				candidates = filterCandidates(candidates)
				for _, c := range candidates {
					if ClaimSession(agentID, cwd, c.Path) {
						watchedFilePath = c.Path
						lastFileSize = 0
						lastCheck = time.Now()
						lastActivity = c.ModTime
						silentTicks = 0
						if notifyCh != nil {
							notifier.Watch(watchedFilePath)
						}
						break
					}
				}
			}
			if watchedFilePath != "" {
				if !readWatched() {
					silentTicks++
				}

				// Mid-session re-bind for /new and /resume. Gated on PTY
				// activity OR user focus: only the watcher whose PTY is producing
				// output (or whose pane the user is currently driving) switches,
				// so a sibling agent in the same cwd writing to its own transcript
				// can't make this idle, unfocused watcher steal its session. The
				// focus exemption covers a /new or /resume issued in the focused
				// pane before the agent emits any PTY output.
				if silentTicks >= rebindSilenceTicks {
					focused := agent.IsPtyFocused(sessionID)
					lastPtyOut := agent.LastPtyActivity(sessionID)
					if time.Since(lastPtyOut) < 3*time.Second || focused {
						rebindDir := projectSearchDir()
						cands, _ := FindLatestFiles(rebindDir, ".jsonl", lastActivity)
						cands = filterCandidates(cands)
						var others []RebindCandidate
						for _, c := range cands {
							others = append(others, RebindCandidate{Key: c.Path, ModTime: c.ModTime})
						}
						newKey := ShouldRebind(silentTicks, watchedFilePath, lastActivity, others)
						if newKey != "" && newKey != watchedFilePath {
							if ClaimSession(agentID, cwd, newKey) {
								UnclaimSession(agentID, cwd, watchedFilePath)
								watchedFilePath = newKey
								lastFileSize = 0
								lastCheck = time.Now()
								silentTicks = 0
								if notifyCh != nil {
									notifier.Watch(watchedFilePath)
								}
							}
						}
					}
				}
			}
		}
	}
}

type PiLogLine struct {
	Type    string     `json:"type"`
	Message *PiMessage `json:"message,omitempty"`
}

func parseOnePiLogLine(line string) (PiMessage, bool) {
	// 1. Try to parse as PiLogLine first (new format)
	var l PiLogLine
	if err := json.Unmarshal([]byte(line), &l); err == nil && l.Type == "message" && l.Message != nil {
		return *l.Message, true
	}
	// 2. Try to parse directly as PiMessage (old format)
	var msg PiMessage
	if err := json.Unmarshal([]byte(line), &msg); err == nil && msg.Role != "" {
		return msg, true
	}
	return PiMessage{}, false
}

func parsePiFormatLog(filePath string, offset int64, resume bool, callback func(status, tool, details, title string)) {
	lines, err := ReadNewLines(filePath, offset)
	if err != nil || len(lines) == 0 {
		return
	}

	// Prefer an explicit title/title_change record (omp writes these), then
	// fall back to the first user prompt (upstream Pi).
	var sessionTitle string
	for _, line := range lines {
		if title, ok := parsePiFormatTitleLine(line); ok && sessionTitle == "" {
			sessionTitle = title
		}
	}
	for _, line := range lines {
		if msg, ok := parseOnePiLogLine(line); ok {
			if strings.EqualFold(msg.Role, "user") {
				for _, b := range msg.Content {
					if b.Type == "text" && b.Text != "" && sessionTitle == "" {
						cleaned := CleanPrompt(b.Text)
						if cleaned != "" {
							sessionTitle = cleaned
						}
					}
				}
			}
		}
	}

	// Forward pass: emit status events for the new log lines.
	var states []struct{ status, tool string }
	for _, line := range lines {
		msg, ok := parseOnePiLogLine(line)
		if !ok {
			continue
		}
		status, tool := piStatusForMessage(msg)
		if status == "" {
			continue
		}
		states = append(states, struct{ status, tool string }{status, tool})
	}

	if len(states) == 0 {
		if sessionTitle != "" && offset == 0 {
			callback("idle", "", "", sessionTitle)
		}
		return
	}

	if offset == 0 && resume {
		// Resumed session: report only the final state to avoid replaying
		// the whole prior session history as a burst of transitions.
		last := states[len(states)-1]
		callback(last.status, last.tool, "", sessionTitle)
		return
	}

	// Incremental read (or fresh session initial read): emit every deduped
	// transition. For a fresh session where the whole turn was already written
	// before the first poll (e.g. a fast one-shot response), this captures the
	// thinking→idle transition so status/push notifications fire correctly.
	var lastState string
	for _, st := range states {
		state := st.status + "|" + st.tool
		if state == lastState {
			continue
		}
		lastState = state
		callback(st.status, st.tool, "", sessionTitle)
	}
}

func parsePiFormatTitleLine(line string) (string, bool) {
	var rec struct {
		Type  string `json:"type"`
		Title string `json:"title"`
	}
	if err := json.Unmarshal([]byte(line), &rec); err != nil {
		return "", false
	}
	if rec.Type != "title" && rec.Type != "title_change" {
		return "", false
	}
	title := strings.TrimSpace(rec.Title)
	if title == "" {
		return "", false
	}
	return CleanPrompt(title), true
}

// piStatusForMessage derives the working/idle state represented by a single
// pi log message. It returns an empty status string when the message does not
// represent a meaningful state change.
func piStatusForMessage(msg PiMessage) (status, tool string) {
	roleLower := strings.ToLower(msg.Role)

	if roleLower == "user" {
		return "thinking", ""
	}

	if roleLower == "toolresult" || roleLower == "tool" {
		return "thinking", ""
	}

	if roleLower == "assistant" || roleLower == "agent" {
		if strings.ToLower(msg.StopReason) == "aborted" || strings.Contains(strings.ToLower(msg.ErrorMessage), "aborted") {
			return "idle", ""
		}
		var lastToolName string
		var hasText bool
		var hasThinking bool
		for _, b := range msg.Content {
			if b.Type == "tool_call" || b.Type == "tool_use" || b.Type == "toolCall" {
				lastToolName = b.Name
			} else if b.Type == "text" {
				hasText = true
			} else if b.Type == "thinking" {
				hasThinking = true
			}
		}

		if lastToolName != "" {
			return "executing", lastToolName
		}
		// An assistant message with a thinking (reasoning) block but no
		// tool call or text yet means the model is still working — report
		// "thinking" so the status doesn't prematurely flip to idle while
		// the LLM is still generating. Once a text block appears alongside
		// the thinking block, the turn is complete and we can report idle.
		// Only canonical user-input tools signal waiting_input; keyword
		// scanning of assistant text was removed due to false positives.
		if hasThinking && !hasText {
			return "thinking", ""
		}
		if hasText {
			return "idle", ""
		}
	}

	return "", ""
}
