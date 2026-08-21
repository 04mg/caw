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

type FxWatcher struct{}

func init() {
	agent.RegisterStatusWatcher("fx", &FxWatcher{})
}

// fxEvent is a single line in ~/.fx/sessions/<id>/events.jsonl
type fxEvent struct {
	Kind        string          `json:"kind"`
	Seq         int             `json:"seq"`
	TimestampMs int64           `json:"timestamp_ms"`
	Payload     json.RawMessage `json:"payload"`
}

type fxUser struct {
	Text   string `json:"text"`
	Images []any  `json:"images"`
}

type fxExecution struct {
	SchemaVersion int          `json:"schema_version"`
	ToolSteps     []fxToolStep `json:"tool_steps"`
}

type fxToolStep struct {
	ToolCalls   []fxToolCall   `json:"tool_calls"`
	ToolResults []fxToolResult `json:"tool_results"`
}

type fxToolCall struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	ArgumentsJSON string `json:"arguments_json"`
}

type fxToolResult struct {
	ToolCallID string `json:"tool_call_id"`
	ToolName   string `json:"tool_name"`
	Status     string `json:"status"`
	Output     string `json:"output"`
}

type fxCheckpoint struct {
	Version     int          `json:"version"`
	TurnID      int          `json:"turn_id"`
	User        *fxUser      `json:"user"`
	Execution   *fxExecution `json:"execution"`
	Cause       *string      `json:"cause"`
	Action      *string      `json:"action"`
	ToolState   *string      `json:"tool_state"`
	RouteModel  *string      `json:"route_model"`
}

type fxTurn struct {
	Kind           string       `json:"kind"`
	User           *fxUser      `json:"user"`
	Assistant      *string      `json:"assistant"`
	Execution      *fxExecution `json:"execution"`
	TerminalReason *string      `json:"terminal_reason"`
}

func (w *FxWatcher) Watch(ctx context.Context, sessionID string, cwd string, resume bool, callback func(status, tool, details, title string), heartbeat func()) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".fx", "sessions")
	const agentID = "fx"

	lookback := 10 * time.Second
	if resume {
		lookback = 1 * time.Hour
	}
	lastCheck := time.Now().Add(-lookback)
	var lastFileSize int64
	var watchedFilePath string
	var sessionTitle string
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
		w.parseFxLog(watchedFilePath, lastFileSize, wrappedCallback)
		lastFileSize = info.Size()
		lastActivity = info.ModTime()
		silentTicks = 0
		// The event log advanced, so Fx is no longer blocked on whatever
		// interactive prompt (question / permission) the PTY footer marker
		// saw — drop it so it can't re-assert waiting_input later.
		agent.ClearPtyPendingInput(sessionID)
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
				if exact := agent.PersistedExternalSession(sessionID); exact != "" {
					candidatePath := filepath.Join(dir, exact, "events.jsonl")
					if info, err := os.Stat(candidatePath); err == nil && !info.IsDir() {
						if fxWorkspaceMatches(filepath.Dir(candidatePath), cwd) && ClaimSessionForLeaf(agentID, cwd, candidatePath, sessionID) {
							watchedFilePath = candidatePath
							lastFileSize = 0
							lastCheck = time.Now()
							lastActivity = info.ModTime()
							silentTicks = 0
							if notifyCh != nil {
								notifier.Watch(watchedFilePath)
							}
							agent.RecordExternalSession(sessionID, exact)
						}
					}
				}
				if watchedFilePath == "" {
					candidates, err := FindEarliestFiles(dir, "events.jsonl", lastCheck)
					if err != nil {
						continue
					}
					for _, c := range candidates {
						if cwd != "" && !fxWorkspaceMatches(filepath.Dir(c.Path), cwd) {
							continue
						}
						if ClaimSessionForLeaf(agentID, cwd, c.Path, sessionID) {
							watchedFilePath = c.Path
							lastFileSize = 0
							lastCheck = time.Now()
							lastActivity = c.ModTime
							silentTicks = 0
							if notifyCh != nil {
								notifier.Watch(watchedFilePath)
							}
							if sid := fxSessionIDFromPath(c.Path); sid != "" {
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
				// Fx writes nothing to its event log while it is blocked on
				// an interactive prompt (ask_user_question, permission
				// allow/deny): the prompt exists only in the TUI until the
				// user answers. If a prompt footer was seen in the PTY
				// output after the last event-log write, Fx is waiting for
				// user input — surface that instead of staying in the stale
				// working state.
				if pt := agent.LastPtyPendingInput(sessionID); pt.After(lastActivity) {
					callback("waiting_input", "", "", sessionTitle)
				}
			}

			if silentTicks >= rebindSilenceTicks {
					focused := agent.IsPtyFocused(sessionID)
					lastPtyOut := agent.LastPtyActivity(sessionID)
					if time.Since(lastPtyOut) < 3*time.Second || focused {
						cands, _ := FindLatestFiles(dir, "events.jsonl", lastActivity)
						var others []RebindCandidate
						for _, c := range cands {
							if cwd != "" && !fxWorkspaceMatches(filepath.Dir(c.Path), cwd) {
								continue
							}
							others = append(others, RebindCandidate{Key: c.Path, ModTime: c.ModTime})
						}
						newKey := ShouldRebind(silentTicks, watchedFilePath, lastActivity, others)
						if newKey != "" && newKey != watchedFilePath {
							if ClaimSessionForLeaf(agentID, cwd, newKey, sessionID) {
								UnclaimSession(agentID, cwd, watchedFilePath)
								watchedFilePath = newKey
								lastFileSize = 0
								lastCheck = time.Now()
								silentTicks = 0
								if notifyCh != nil {
									notifier.Watch(watchedFilePath)
								}
								if sid := fxSessionIDFromPath(newKey); sid != "" {
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

func fxSessionIDFromPath(path string) string {
	return filepath.Base(filepath.Dir(path))
}

func fxWorkspaceMatches(sessionDir, cwd string) bool {
	if cwd == "" {
		return true
	}
	// Try display.json first (most reliable, always present after first turn)
	if data, err := os.ReadFile(filepath.Join(sessionDir, "display.json")); err == nil {
		var d struct {
			OriginWorkspaceRoot string `json:"origin_workspace_root"`
			Title               string `json:"title"`
		}
		if json.Unmarshal(data, &d) == nil && d.OriginWorkspaceRoot != "" {
			return d.OriginWorkspaceRoot == cwd
		}
	}
	if data, err := os.ReadFile(filepath.Join(sessionDir, "session.json")); err == nil {
		var s struct {
			WorkspaceRoot string `json:"workspace_root"`
		}
		if json.Unmarshal(data, &s) == nil && s.WorkspaceRoot != "" {
			return s.WorkspaceRoot == cwd
		}
	}
	// Fallback: not enough info to filter, allow it (claim will still be cwd-scoped)
	return true
}

func fxTitleFromDisplay(sessionDir string) string {
	data, err := os.ReadFile(filepath.Join(sessionDir, "display.json"))
	if err != nil {
		return ""
	}
	var d struct {
		Title string `json:"title"`
	}
	if json.Unmarshal(data, &d) != nil {
		return ""
	}
	return CleanPrompt(d.Title)
}

func (w *FxWatcher) parseFxLog(filePath string, offset int64, callback func(status, tool, details, title string)) {
	lines, err := ReadNewLines(filePath, offset)
	if err != nil || len(lines) == 0 {
		return
	}

	sessionDir := filepath.Dir(filePath)
	displayTitle := fxTitleFromDisplay(sessionDir)

	// Collect all events in order
	type decoded struct {
		kind    string
		payload json.RawMessage
	}
	var events []decoded
	for _, line := range lines {
		var ev fxEvent
		if json.Unmarshal([]byte(line), &ev) != nil {
			continue
		}
		events = append(events, decoded{kind: ev.Kind, payload: ev.Payload})
	}
	// Also need to consider already-read prefix for reverse scan: we only have new lines.
	// To determine current status we need the LAST event overall, which is the last of the new lines
	// if any, otherwise we would have no new data. Since we only parse new lines, the reverse scan
	// over new lines suffices: the newest event is at the end of events.
	// However title may come from display.json fallback to first user prompt in new lines.
	var firstUserText string
	for _, ev := range events {
		if ev.kind == "history_turn_committed" {
			var p struct {
				Turn fxTurn `json:"turn"`
			}
			if json.Unmarshal(ev.payload, &p) == nil && p.Turn.User != nil && p.Turn.User.Text != "" && firstUserText == "" {
				firstUserText = p.Turn.User.Text
			}
		} else if ev.kind == "recovery_checkpoint_set" {
			var p struct {
				Checkpoint fxCheckpoint `json:"checkpoint"`
			}
			if json.Unmarshal(ev.payload, &p) == nil && p.Checkpoint.User != nil && p.Checkpoint.User.Text != "" && firstUserText == "" {
				firstUserText = p.Checkpoint.User.Text
			}
		}
	}
	sessionTitle := displayTitle
	if sessionTitle == "" {
		sessionTitle = CleanPrompt(firstUserText)
	}

	// Reverse scan to determine status
	var lastCheckpoint *fxCheckpoint
	// Pre-decode checkpoints for quick lookup
	checkpointCache := make(map[int]*fxCheckpoint)
	for i, ev := range events {
		if ev.kind == "recovery_checkpoint_set" {
			var p struct {
				Checkpoint fxCheckpoint `json:"checkpoint"`
			}
			if json.Unmarshal(ev.payload, &p) == nil {
				cp := p.Checkpoint
				checkpointCache[i] = &cp
			}
		}
	}

	for i := len(events) - 1; i >= 0; i-- {
		ev := events[i]
		switch ev.kind {
		case "history_turn_committed":
			var p struct {
				Turn fxTurn `json:"turn"`
			}
			if json.Unmarshal(ev.payload, &p) != nil {
				continue
			}
			turn := p.Turn
			switch turn.Kind {
			case "assistant":
				callback("idle", "", "", sessionTitle)
				return
			case "interrupted":
				reason := ""
				if turn.TerminalReason != nil {
					reason = strings.ToLower(*turn.TerminalReason)
				}
				tool := fxLastToolName(turn.Execution)
				if reason == "cancelled" {
					callback("interrupted", tool, "", sessionTitle)
					return
				}
				if reason == "failed" {
					details := ""
					if turn.Assistant != nil {
						details = *turn.Assistant
					}
					if details == "" {
						details = "turn failed"
					}
					callback("tool_failed", tool, details, sessionTitle)
					return
				}
				callback("interrupted", tool, "", sessionTitle)
				return
			case "background_command":
				tool := fxLastToolName(turn.Execution)
				if tool == "" {
					tool = "background"
				}
				callback("executing", tool, "", sessionTitle)
				return
			case "compacted_summary":
				callback("idle", "", "", sessionTitle)
				return
			default:
				callback("idle", "", "", sessionTitle)
				return
			}

		case "recovery_checkpoint_set":
			cp := checkpointCache[i]
			if cp == nil {
				continue
			}
			// Paused => waiting for user input (/continue to resume)
			if cp.Action != nil && strings.ToLower(*cp.Action) == "paused" {
				tool := fxLastToolName(cp.Execution)
				callback("waiting_input", tool, "paused — /continue to resume", sessionTitle)
				return
			}
			tool := fxLastToolName(cp.Execution)
			if tool != "" {
				if isUserInputTool(strings.ToLower(tool)) {
					callback("waiting_input", tool, "", sessionTitle)
					return
				}
				callback("executing", tool, "", sessionTitle)
				return
			}
			callback("thinking", "", "", sessionTitle)
			return

		case "usage_checkpointed", "state_replacement_started", "state_replacement_chunk", "state_replacement_committed":
			// Look ahead (backwards) for the most recent checkpoint to get tool context
			if lastCheckpoint == nil {
				for j := i - 1; j >= 0; j-- {
					if c, ok := checkpointCache[j]; ok {
						lastCheckpoint = c
						break
					}
				}
			}
			if lastCheckpoint != nil {
				if lastCheckpoint.Action != nil && strings.ToLower(*lastCheckpoint.Action) == "paused" {
					tool := fxLastToolName(lastCheckpoint.Execution)
					callback("waiting_input", tool, "paused — /continue to resume", sessionTitle)
					return
				}
				tool := fxLastToolName(lastCheckpoint.Execution)
				if tool != "" {
					if isUserInputTool(strings.ToLower(tool)) {
						callback("waiting_input", tool, "", sessionTitle)
						return
					}
					callback("executing", tool, "", sessionTitle)
					return
				}
			}
			callback("thinking", "", "", sessionTitle)
			return

		case "recovery_checkpoint_cleared":
			callback("idle", "", "", sessionTitle)
			return

		case "session_started":
			callback("idle", "", "", sessionTitle)
			return
		}
	}

	// Fallback: if we only saw events but none matched above, treat as thinking
	callback("thinking", "", "", sessionTitle)
}

func fxLastToolName(exec *fxExecution) string {
	if exec == nil || len(exec.ToolSteps) == 0 {
		return ""
	}
	last := exec.ToolSteps[len(exec.ToolSteps)-1]
	if len(last.ToolCalls) > 0 && last.ToolCalls[0].Name != "" {
		return last.ToolCalls[0].Name
	}
	if len(last.ToolResults) > 0 && last.ToolResults[0].ToolName != "" {
		return last.ToolResults[0].ToolName
	}
	return ""
}
