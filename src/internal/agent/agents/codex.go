package agents

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/04mg/caw/internal/agent"
)

type CodexWatcher struct{}

func init() {
	agent.RegisterStatusWatcher("codex", &CodexWatcher{})
}

// codexUUIDRe matches a UUID embedded in a Codex rollout filename, e.g.
// "rollout-2025-01-02T15_04_05-<uuid>.jsonl" or
// "rollout-test-codex-<uuid>.jsonl". The session id codex resume accepts is
// this UUID; extracting it lets us resume the exact session a pane was
// running instead of always "--last" (which breaks when multiple Codex panes
// share the same cwd).
var codexUUIDRe = regexp.MustCompile(`([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12})`)

// codexSessionIDFromPath returns the Codex session UUID contained in a rollout
// transcript filename, or "" if no UUID is present.
func codexSessionIDFromPath(path string) string {
	return codexUUIDRe.FindString(filepath.Base(path))
}

// codexTranscriptForUUID returns the rollout transcript path whose filename
// contains the given session UUID, or "" if none exists. Used to bind a
// reopened pane to its exact prior session (persisted as the rollout UUID)
// instead of scanning the shared tree heuristically.
func codexTranscriptForUUID(dir, sessionID, cwd string) string {
	if sessionID == "" {
		return ""
	}
	// codex resume --last (no exact id) is handled by the caller with a scan.
	var match string
	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if strings.HasSuffix(info.Name(), ".jsonl") && codexSessionIDFromPath(path) == sessionID {
			match = path
			return filepath.SkipAll
		}
		return nil
	})
	return match
}

type CodexLogLine struct {
	Type    string        `json:"type"`
	Payload *CodexPayload `json:"payload,omitempty"`
}

type CodexContent struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type CodexPayload struct {
	Type    string         `json:"type"`
	Message string         `json:"message,omitempty"`
	Content []CodexContent `json:"content,omitempty"`
	// Phase is set on event_msg "agent_message" and response_item "message"
	// entries: "commentary" (interim thought) or "final_answer" (turn done).
	Phase string `json:"phase,omitempty"`
	// Role is set on response_item "message" entries: developer/user/assistant.
	Role string `json:"role,omitempty"`
	// Name is the tool name for response_item "function_call" entries.
	Name string `json:"name,omitempty"`
	// Error is set on event_msg "task_complete" entries when the turn ended
	// in failure (e.g. an API error). Carries a message and codex_error_info.
	Error *struct {
		Message       string `json:"message,omitempty"`
		CodexErrorInfo string `json:"codex_error_info,omitempty"`
	} `json:"error,omitempty"`
}

func (w *CodexWatcher) Watch(ctx context.Context, sessionID string, cwd string, resume bool, callback func(status, tool, details, title string), heartbeat func()) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".codex", "sessions")
	const agentID = "codex"
	// Codex stores every session transcript under the shared ~/.codex/sessions
	// tree with no per-cwd partitioning, so the watcher cannot narrow the
	// file scan by workspace. Claims are therefore keyed globally per agent —
	// every Codex pane shares one claim namespace regardless of its Caw-side
	// cwd, so two Codex panes in different workspaces never bind the same
	// rollout file (and thus the same session title/state). We pass a fixed
	// sentinel ("") as the claim cwd, mirroring the Hermes watcher.
	const claimCwd = ""
	// On resume (codex resume --last), the agent reattaches to a pre-existing
	// session whose transcript file may predate this watcher. Widen the
	// search window to 1 hour so the resumed session is found. For a fresh
	// start, only look for files modified after the watcher started (no
	// negative offset) to avoid grabbing a sibling agent's session.
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
			UnclaimSession(agentID, claimCwd, watchedFilePath)
		}
	}()

	readWatched := func() bool {
		if watchedFilePath == "" {
			return false
		}
		info, err := os.Stat(watchedFilePath)
		if err != nil {
			UnclaimSession(agentID, claimCwd, watchedFilePath)
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
		w.parseCodexLog(watchedFilePath, lastFileSize, wrappedCallback)
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
				// A reopened pane must resume tracking its exact prior session
				// (set by resumeCmdForAgent via `codex resume <uuid>`). Bind
				// to the persisted UUID's transcript first, never a heuristic
				// scan over the shared rollout tree.
				if exact := agent.PersistedExternalSession(sessionID); exact != "" {
					if exactPath := codexTranscriptForUUID(dir, exact, cwd); exactPath != "" && ClaimSessionForLeaf(agentID, claimCwd, exactPath, sessionID) {
						watchedFilePath = exactPath
						lastFileSize = 0
						lastCheck = time.Now()
						lastActivity = time.Now()
						silentTicks = 0
						if notifyCh != nil {
							notifier.Watch(watchedFilePath)
						}
						agent.RecordExternalSession(sessionID, exact)
					}
				}
				if watchedFilePath == "" {
					candidates, err := FindEarliestFiles(dir, ".jsonl", lastCheck)
					if err != nil {
						continue
					}
					for _, c := range candidates {
						if ClaimSessionForLeaf(agentID, claimCwd, c.Path, sessionID) {
							watchedFilePath = c.Path
							lastFileSize = 0
							lastCheck = time.Now()
							lastActivity = c.ModTime
							silentTicks = 0
							if notifyCh != nil {
								notifier.Watch(watchedFilePath)
							}
							if sid := codexSessionIDFromPath(c.Path); sid != "" {
								agent.RecordExternalSession(sessionID, sid)
							}
							break
						}
					}
				}
			}
			if watchedFilePath != "" {
				if !readWatched() {
					silentTicks++
				}

			// Mid-session re-bind for /new and /resume. Gated on PTY activity
			// OR user focus: only the watcher whose PTY is producing output
			// (or whose pane the user is currently driving) switches, so a
			// sibling Codex in another workspace writing to its own transcript
			// can't make this idle, unfocused watcher steal its session.
			// The focus exemption covers /new or /resume issued in the
			// focused pane before the agent emits any PTY output.
			if silentTicks >= rebindSilenceTicks {
				focused := agent.IsPtyFocused(sessionID)
				lastPtyOut := agent.LastPtyActivity(sessionID)
				if time.Since(lastPtyOut) < 3*time.Second || focused {
					cands, _ := FindLatestFiles(dir, ".jsonl", lastActivity)
					var others []RebindCandidate
					for _, c := range cands {
						others = append(others, RebindCandidate{Key: c.Path, ModTime: c.ModTime})
					}
					newKey := ShouldRebind(silentTicks, watchedFilePath, lastActivity, others)
					if newKey != "" && newKey != watchedFilePath {
						if ClaimSessionForLeaf(agentID, claimCwd, newKey, sessionID) {
							UnclaimSession(agentID, claimCwd, watchedFilePath)
							watchedFilePath = newKey
							lastFileSize = 0
							lastCheck = time.Now()
							silentTicks = 0
							if notifyCh != nil {
								notifier.Watch(watchedFilePath)
							}
							if sid := codexSessionIDFromPath(newKey); sid != "" {
								agent.RecordExternalSession(sessionID, sid)
							}
						}
					}
				}
			}
			}
		}
	}
}

func (w *CodexWatcher) parseCodexLog(filePath string, offset int64, callback func(status, tool, details, title string)) {
	lines, err := ReadNewLines(filePath, offset)
	if err != nil || len(lines) == 0 {
		return
	}

	// Forward pass: collect the first user prompt to use as the session title.
	var sessionTitle string
	for _, line := range lines {
		var logLine CodexLogLine
		if json.Unmarshal([]byte(line), &logLine) != nil {
			continue
		}
		if logLine.Payload != nil && logLine.Payload.Type == "user_message" && logLine.Payload.Message != "" {
			if sessionTitle == "" {
				sessionTitle = logLine.Payload.Message
			}
		}
	}
	sessionTitle = CleanPrompt(sessionTitle)

	// Codex writes a sequence of entries per turn. The status we report must
	// reflect the LAST meaningful entry:
	//
	//   user_message                      → thinking
	//   agent_message / message            → interim "commentary" still WORKING,
	//                                       final_answer → idle (turn complete)
	//   function_call                      → executing <tool>
	//   function_call_output               → thinking (waiting for next step)
	//   task_complete                      → idle (turn definitively done)
	//
	// The previous implementation matched any response_item "message" with
	// role assistant and reported "idle" — but Codex emits many such messages
	// with phase "commentary" mid-turn, which made the status flicker between
	// executing and idle. We now treat "commentary" as still working and only
	// "final_answer" (or an explicit task_complete) as idle.
	var turnCompleted bool
	var turnAborted bool
	var taskError string
	var lastAssistantText string
	var lastAssistantTool string
	var foundTool bool

	for i := len(lines) - 1; i >= 0; i-- {
		var logLine CodexLogLine
		if err := json.Unmarshal([]byte(lines[i]), &logLine); err != nil {
			continue
		}

		if logLine.Payload == nil {
			continue
		}
		p := logLine.Payload
		switch p.Type {
		case "task_complete":
			turnCompleted = true
			if p.Error != nil && p.Error.Message != "" {
				taskError = p.Error.Message
			}
			continue
		case "turn_aborted":
			turnAborted = true
			turnCompleted = true
			continue
		case "function_call":
			if !foundTool {
				tool := p.Name
				if tool == "" {
					tool = "exec"
				}
				lastAssistantTool = tool
				foundTool = true
			}
			continue
		case "function_call_output":
			continue
		case "user_message":
			continue
		case "message":
			if p.Role == "assistant" {
				var msgText string
				if p.Message != "" {
					msgText = p.Message
				} else if len(p.Content) > 0 {
					var textParts []string
					for _, c := range p.Content {
						if c.Text != "" {
							textParts = append(textParts, c.Text)
						}
					}
					msgText = strings.Join(textParts, " ")
				}
				if msgText != "" && lastAssistantText == "" {
					lastAssistantText = msgText
					if p.Phase == "final_answer" {
						turnCompleted = true
					}
				}
			}
			continue
		case "agent_message":
			var msgText string
			if p.Message != "" {
				msgText = p.Message
			} else if len(p.Content) > 0 {
				var textParts []string
				for _, c := range p.Content {
					if c.Text != "" {
						textParts = append(textParts, c.Text)
					}
				}
				msgText = strings.Join(textParts, " ")
			}
			if msgText != "" && lastAssistantText == "" {
				lastAssistantText = msgText
				if p.Phase == "final_answer" {
					turnCompleted = true
				}
			}
			continue
		case "task_started":
			continue
		}
	}

	// An aborted turn means the user cancelled — report "interrupted" so the
	// UI surfaces it with a red dot and no push notification is sent.
	if turnAborted {
		callback("interrupted", lastAssistantTool, "", sessionTitle)
		return
	}

	// A task_complete carrying an error means the turn failed (e.g. an API
	// or server tool error). Surface it with a red dot.
	if taskError != "" {
		callback("tool_failed", lastAssistantTool, taskError, sessionTitle)
		return
	}

	if lastAssistantTool != "" {
		// Tools that request user input should be reported as waiting_input,
		// not as executing. The agent is blocked until the user answers.
		toolLower := strings.ToLower(lastAssistantTool)
		if isUserInputTool(toolLower) {
			callback("waiting_input", lastAssistantTool, "", sessionTitle)
			return
		}
		callback("executing", lastAssistantTool, "", sessionTitle)
		return
	}

	if lastAssistantText != "" {
		status := "thinking"
		if turnCompleted {
			status = "idle"
		}
		// Only canonical user-input tools (handled above) signal waiting_input.
		// Keyword scanning of assistant text for "confirm"/"approve"/"[y/n]"
		// was removed: it produced false positives when the assistant's
		// explanation or plan happened to use those words.
		callback(status, "", "", sessionTitle)
		return
	}

	status := "thinking"
	if turnCompleted {
		status = "idle"
	}
	callback(status, "", "", sessionTitle)
}
