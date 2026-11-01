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
	Version    int          `json:"version"`
	TurnID     int          `json:"turn_id"`
	User       *fxUser      `json:"user"`
	Execution  *fxExecution `json:"execution"`
	Cause      *string      `json:"cause"`
	Action     *string      `json:"action"`
	ToolState  *string      `json:"tool_state"`
	RouteModel *string      `json:"route_model"`
}

type fxTurn struct {
	Kind           string       `json:"kind"`
	User           *fxUser      `json:"user"`
	Assistant      *string      `json:"assistant"`
	Execution      *fxExecution `json:"execution"`
	TerminalReason *string      `json:"terminal_reason"`
}

// Fx (fx.sh, vercel-labs/fx) has used two on-disk session layouts:
//
//   - v1 (<= 0.0.15): ~/.fx/sessions/<id>/events.jsonl with
//     history_turn_committed / recovery_checkpoint_set events, plus
//     display.json / session.json carrying the workspace.
//   - v2 (0.0.16+):  ~/.fx/sessions/v2/<id>/log.jsonl, a journal of
//     {"v":1,"seq":N,"ts":ms,"kind":...} lines: session_created (with
//     "workspace" and "role"), turn_started, item (type user / tool_running /
//     tool_call / tool_result / assistant / interruption / turn_end ...),
//     turn_committed, turn_interrupted (reason cancel|failed|closed|crash),
//     child_spawned/child_finished, set and closed.
//
// The watcher binds to whichever layout the running fx writes.
const (
	fxV1LogName = "events.jsonl"
	fxV2LogName = "log.jsonl"
)

// fxPromptBusyGrace is how long after the last "• Running (…)/• Thinking (…)"
// status line a freshly rendered prompt footer is required to stay unanswered
// before it is reported as waiting_input. Fx keeps that status line hidden
// while a permission/question screen is up.
const fxPromptBusyGrace = 1500 * time.Millisecond

// fxBusyWindow is how recently Fx's status line must have been redrawn (it
// ticks once per second while a turn runs) for the turn to count as active.
const fxBusyWindow = 3 * time.Second

// isFxWorking reports whether a status is an in-flight turn state.
func isFxWorking(status string) bool {
	switch status {
	case "thinking", "executing", "tool_failed":
		return true
	}
	return false
}

func (w *FxWatcher) Watch(ctx context.Context, sessionID string, cwd string, resume bool, callback func(status, tool, details, title string), heartbeat func()) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".fx", "sessions")
	dirV2 := filepath.Join(dir, "v2")
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
	var v2State fxV2State
	// parsed is the status most recently derived from the event log; reported
	// is what was last sent to the UI (it differs while a PTY-only prompt
	// override is active).
	var parsedStatus, parsedTool, parsedDetails string
	var reportedStatus string
	// unboundWorkingAt is set while a turn is reported as in flight from the
	// PTY alone, before any session log exists to bind to.
	var unboundWorkingAt time.Time

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

	// replay is true while the first read after binding replays a log that
	// pre-dates this watcher (a resumed or reopened session): its history must
	// not produce live transitions or notifications.
	var replay bool
	bind := func(path string, modTime time.Time, existing bool) {
		replay = existing
		watchedFilePath = path
		lastFileSize = 0
		lastCheck = time.Now()
		lastActivity = modTime
		silentTicks = 0
		v2State = fxV2State{}
		if notifyCh != nil {
			notifier.Watch(watchedFilePath)
		}
		if sid := fxSessionIDFromPath(path); sid != "" {
			agent.RecordExternalSession(sessionID, sid)
		}
	}

	report := func(status, tool, details string) {
		reportedStatus = status
		callback(status, tool, details, sessionTitle)
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
		initialRead := lastFileSize == 0
		replaying := initialRead && replay
		replay = false
		wrappedCallback := func(status, tool, details, title string) {
			if title != "" {
				sessionTitle = title
			}
			parsedStatus, parsedTool, parsedDetails = status, tool, details
			report(status, tool, details)
		}
		// transient is invoked by the v2 parser when one read covers a
		// whole stretch of work that ended the turn (e.g. the user answered
		// a prompt and the agent finished within one poll, or a fast turn
		// with a failing tool). Reporting the working state first keeps the
		// finished notification, which only fires for working -> idle. It is
		// skipped when replaying a pre-existing log on bind.
		transient := func(status, tool, details string) {
			if replaying {
				return
			}
			if !isFxWorking(reportedStatus) {
				report(status, tool, details)
			}
		}
		if filepath.Base(watchedFilePath) == fxV2LogName {
			w.parseFxV2Log(watchedFilePath, lastFileSize, &v2State, transient, wrappedCallback)
		} else {
			w.parseFxLog(watchedFilePath, lastFileSize, wrappedCallback)
		}
		lastFileSize = info.Size()
		lastActivity = info.ModTime()
		silentTicks = 0
		// A prompt footer sighting older than this log write is stale (the
		// answer/decision was persisted). It is NOT cleared here: the prompt
		// for a freshly written tool_running item is typically drawn within
		// milliseconds of the write, i.e. before this (debounced) read runs,
		// and clearing would drop exactly the sighting that matters. The
		// pt.After(lastActivity) comparison below already ignores stale ones.
		return true
	}

	// candidates lists every fx session log (both layouts) modified after the
	// given time that belongs to this cwd, oldest first.
	candidates := func(after time.Time) []FileCandidate {
		var out []FileCandidate
		if v2, err := FindEarliestFiles(dirV2, fxV2LogName, after); err == nil {
			for _, c := range v2 {
				if filepath.Dir(filepath.Dir(c.Path)) != dirV2 || !fxV2LogMatches(c.Path, cwd) {
					continue
				}
				// A log ending in "closed" belongs to an fx process that
				// exited (e.g. a sibling pane that was just closed, whose
				// final write makes it look recently active); it can never
				// be the session this live pane is running.
				if fxV2LogClosed(c.Path) {
					continue
				}
				out = append(out, c)
			}
		}
		if v1, err := FindEarliestFiles(dir, fxV1LogName, after); err == nil {
			for _, c := range v1 {
				if cwd != "" && !fxWorkspaceMatches(filepath.Dir(c.Path), cwd) {
					continue
				}
				out = append(out, c)
			}
		}
		return out
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
					for _, candidatePath := range []string{
						filepath.Join(dirV2, exact, fxV2LogName),
						filepath.Join(dir, exact, fxV1LogName),
					} {
						info, err := os.Stat(candidatePath)
						if err != nil || info.IsDir() {
							continue
						}
						matches := fxWorkspaceMatches(filepath.Dir(candidatePath), cwd)
						if filepath.Base(candidatePath) == fxV2LogName {
							matches = fxV2LogMatches(candidatePath, cwd)
						}
						if matches && ClaimSessionForLeaf(agentID, cwd, candidatePath, sessionID) {
							bind(candidatePath, info.ModTime(), true)
							break
						}
					}
				}
				if watchedFilePath == "" {
					for _, c := range candidates(lastCheck) {
						if ClaimSessionForLeaf(agentID, cwd, c.Path, sessionID) {
							bind(c.Path, c.ModTime, resume)
							break
						}
					}
				}
				if watchedFilePath == "" {
					// A brand-new fx session has no log at all until the model
					// answers the first prompt (the session directory is created
					// with that first write), so a slow first response leaves
					// nothing to bind to. Fx's "• Thinking (Ns)" status line is
					// then the only sign the turn is in flight. If it disappears
					// without a log ever being written, the turn was cancelled
					// before the model answered.
					busy := agent.LastPtyBusy(sessionID)
					if !busy.IsZero() && time.Since(busy) < fxBusyWindow {
						if !isFxWorking(reportedStatus) {
							unboundWorkingAt = time.Now()
							report("thinking", "", "")
						}
					} else if isFxWorking(reportedStatus) && !unboundWorkingAt.IsZero() {
						if agent.LastPtyInterrupt(sessionID).After(unboundWorkingAt) {
							report("interrupted", "", "")
						} else {
							report("idle", "", "")
						}
						unboundWorkingAt = time.Time{}
					}
				}
			}
			if watchedFilePath != "" {
				unboundWorkingAt = time.Time{}
				if !readWatched() {
					silentTicks++
					// Fx writes nothing to its event log while it is blocked on
					// an interactive prompt (ask_user_question, permission
					// allow/deny): the prompt exists only in the TUI until the
					// user answers. In v2 the log even shows the pending call
					// as tool_running, indistinguishable from a running tool.
					// If a prompt footer was rendered after the last event-log
					// write and Fx's "• Running/Thinking (…)" status line has
					// not been redrawn since, Fx is waiting for user input.
					// Once the user answers, the status line comes back while
					// the log stays frozen (the approved tool is running), so
					// the parsed working state is restored.
					//
					// Fx also writes nothing for a new turn until the model's
					// first response arrives (turn_started and the user item are
					// flushed together with it), so a long LLM wait leaves the
					// log on the previous turn's idle state. The "• Thinking
					// (Ns)" status line drawn after the last log write is the
					// only sign that the new turn is in flight.
					pt := agent.LastPtyPendingInput(sessionID)
					busy := agent.LastPtyBusy(sessionID)
					busyNow := busy.After(lastActivity) && time.Since(busy) < fxBusyWindow
					switch {
					case pt.After(lastActivity) && !busy.After(pt) && time.Since(busy) >= fxPromptBusyGrace:
						if reportedStatus != "waiting_input" {
							report("waiting_input", parsedTool, "")
						}
					case busyNow && isFxWorking(parsedStatus):
						if reportedStatus != parsedStatus {
							report(parsedStatus, parsedTool, parsedDetails)
						}
					case busyNow:
						if !isFxWorking(reportedStatus) {
							report("thinking", "", "")
						}
					}
				}

				if silentTicks >= rebindSilenceTicks {
					focused := agent.IsPtyFocused(sessionID)
					lastPtyOut := agent.LastPtyActivity(sessionID)
					if time.Since(lastPtyOut) < 3*time.Second || focused {
						var others []RebindCandidate
						for _, c := range candidates(lastActivity) {
							others = append(others, RebindCandidate{Key: c.Path, ModTime: c.ModTime})
						}
						newKey := ShouldRebind(silentTicks, watchedFilePath, lastActivity, others)
						if newKey != "" && newKey != watchedFilePath {
							if ClaimSessionForLeaf(agentID, cwd, newKey, sessionID) {
								UnclaimSession(agentID, cwd, watchedFilePath)
								var mod time.Time
								if info, err := os.Stat(newKey); err == nil {
									mod = info.ModTime()
								}
								// /new (a session created after this pane's
								// last activity) is live; /resume replays.
								bind(newKey, mod, !sessionStartedAfter(fxV2SessionStart(newKey), lastActivity))
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

// ----- v2 session log (fx 0.0.16+) -----------------------------------------

// fxV2Line is one line of ~/.fx/sessions/v2/<id>/log.jsonl. The header
// ({"v","seq","ts","kind"}) is fixed; the remaining fields depend on kind.
type fxV2Line struct {
	Kind      string          `json:"kind"`
	Turn      *int64          `json:"turn,omitempty"`
	Type      string          `json:"type,omitempty"`
	Data      json.RawMessage `json:"data,omitempty"`
	Key       string          `json:"key,omitempty"`
	Value     json.RawMessage `json:"value,omitempty"`
	Reason    string          `json:"reason,omitempty"`
	Workspace string          `json:"workspace,omitempty"`
	Role      string          `json:"role,omitempty"`
}

// fxV2ToolCall is the data of tool_running / tool_call items.
type fxV2ToolCall struct {
	CallID   string `json:"call_id"`
	ToolName string `json:"tool_name"`
}

// fxV2ToolResult is the data of tool_result items.
type fxV2ToolResult struct {
	CallID   string `json:"call_id"`
	ToolName string `json:"tool_name"`
	Status   string `json:"status"`
	Preview  string `json:"preview,omitempty"`
}

// fxV2State carries the derived status across incremental reads of a v2
// log: fx writes a tool_running item before a tool starts (or while its
// permission/question prompt is pending) and the matching tool_result only
// once it finishes, so the set of unfinished calls spans reads.
type fxV2State struct {
	running      map[string]string
	runningOrder []string
	status       string
	tool         string
	details      string
	title        string
	firstPrompt  string
	lastText     string
}

func (s *fxV2State) setRunning(callID, name string) {
	if s.running == nil {
		s.running = make(map[string]string)
	}
	if _, ok := s.running[callID]; !ok {
		s.runningOrder = append(s.runningOrder, callID)
	}
	s.running[callID] = name
}

func (s *fxV2State) clearRunning() {
	s.running = nil
	s.runningOrder = nil
}

// latestRunning returns the most recently started tool call that has no
// result yet.
func (s *fxV2State) latestRunning() (string, bool) {
	for i := len(s.runningOrder) - 1; i >= 0; i-- {
		if name, ok := s.running[s.runningOrder[i]]; ok {
			return name, true
		}
	}
	return "", false
}

// runningStatus is the status for the newest unfinished tool call: a
// user-input tool (ask_user_question) blocks on the user, anything else is
// executing.
func (s *fxV2State) runningStatus(name string) {
	if isUserInputTool(strings.ToLower(name)) {
		s.status, s.tool, s.details = "waiting_input", name, ""
		return
	}
	s.status, s.tool, s.details = "executing", name, ""
}

// fxV2LogMatches reports whether a v2 log belongs to cwd: its first line is
// session_created with the workspace root. Child (subagent) sessions are
// never bound to a pane.
func fxV2LogMatches(path, cwd string) bool {
	head, err := ReadFirstLine(path)
	if err != nil {
		return false
	}
	var l fxV2Line
	if json.Unmarshal([]byte(head), &l) != nil || l.Kind != "session_created" {
		return false
	}
	if l.Role == "child" {
		return false
	}
	if cwd == "" {
		return true
	}
	return filepath.Clean(l.Workspace) == filepath.Clean(cwd)
}

// fxV2SessionStart returns the creation time of a v2 log (the ts of its
// session_created line), or the zero time for v1 logs / unreadable files.
func fxV2SessionStart(path string) time.Time {
	if filepath.Base(path) != fxV2LogName {
		return time.Time{}
	}
	head, err := ReadFirstLine(path)
	if err != nil {
		return time.Time{}
	}
	var l struct {
		Kind string `json:"kind"`
		Ts   int64  `json:"ts"`
	}
	if json.Unmarshal([]byte(head), &l) != nil || l.Kind != "session_created" || l.Ts == 0 {
		return time.Time{}
	}
	return time.UnixMilli(l.Ts)
}

// fxV2LogClosed reports whether the last line of a v2 log is "closed".
func fxV2LogClosed(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	start := info.Size() - 4096
	if start < 0 {
		start = 0
	}
	lines, err := ReadNewLines(path, start)
	if err != nil {
		return false
	}
	for i := len(lines) - 1; i >= 0; i-- {
		var l fxV2Line
		if json.Unmarshal([]byte(lines[i]), &l) != nil {
			continue
		}
		return l.Kind == "closed"
	}
	return false
}

// parseFxV2Log applies the v2 log lines appended since offset to state and
// reports the resulting status.
//
// transient (optional) receives the last in-flight state the batch passed
// through when the batch as a whole ends the turn, so a turn that started and
// finished between two polls is still observed as working before idle.
func (w *FxWatcher) parseFxV2Log(filePath string, offset int64, state *fxV2State, transient func(status, tool, details string), callback func(status, tool, details, title string)) {
	lines, err := ReadNewLines(filePath, offset)
	if err != nil || len(lines) == 0 {
		return
	}
	var passStatus, passTool, passDetails string
	for _, line := range lines {
		if isFxWorking(state.status) {
			passStatus, passTool, passDetails = state.status, state.tool, state.details
		}
		var l fxV2Line
		if json.Unmarshal([]byte(line), &l) != nil {
			continue
		}
		switch l.Kind {
		case "session_created":
			state.status, state.tool, state.details = "idle", "", ""
		case "turn_started":
			state.clearRunning()
			state.status, state.tool, state.details = "thinking", "", ""
		case "item":
			applyFxV2Item(state, l.Type, l.Data)
		case "turn_committed":
			state.clearRunning()
			state.status, state.tool, state.details = "idle", "", state.lastText
		case "turn_interrupted":
			state.clearRunning()
			switch strings.ToLower(l.Reason) {
			case "failed":
				details := state.lastText
				if details == "" {
					details = "turn failed"
				}
				state.status, state.details = "tool_failed", details
			default:
				// cancel (the user pressed Esc/Ctrl+C), closed (fx exited
				// mid-turn) or crash (repaired on reopen).
				state.status, state.details = "interrupted", ""
			}
		case "child_spawned":
			state.status, state.tool, state.details = "executing", "subagent", ""
		case "child_finished":
			if name, ok := state.latestRunning(); ok {
				state.runningStatus(name)
			} else {
				state.status, state.tool, state.details = "thinking", "", ""
			}
		case "set":
			if l.Key == "title" {
				var t string
				if json.Unmarshal(l.Value, &t) == nil && t != "" {
					state.title = CleanPrompt(t)
				}
			}
		case "closed":
			state.clearRunning()
			if state.status != "interrupted" && state.status != "tool_failed" {
				state.status, state.tool, state.details = "idle", "", state.lastText
			}
		}
	}
	if isFxWorking(state.status) {
		passStatus = ""
	}
	if passStatus != "" && transient != nil {
		transient(passStatus, passTool, passDetails)
	}
	if state.status == "" {
		return
	}
	title := state.title
	if title == "" {
		title = CleanPrompt(state.firstPrompt)
	}
	callback(state.status, state.tool, state.details, title)
}

// applyFxV2Item updates state for one "item" line of a turn.
func applyFxV2Item(state *fxV2State, itemType string, data json.RawMessage) {
	switch itemType {
	case "user", "steering":
		var u fxUser
		if json.Unmarshal(data, &u) == nil && u.Text != "" && state.firstPrompt == "" && itemType == "user" {
			state.firstPrompt = u.Text
		}
		state.status, state.tool, state.details = "thinking", "", ""
	case "tool_running":
		var c fxV2ToolCall
		if json.Unmarshal(data, &c) == nil && c.CallID != "" {
			state.setRunning(c.CallID, c.ToolName)
			state.runningStatus(c.ToolName)
		}
	case "tool_result":
		var r fxV2ToolResult
		if json.Unmarshal(data, &r) != nil {
			return
		}
		delete(state.running, r.CallID)
		if strings.EqualFold(r.Status, "failure") || strings.EqualFold(r.Status, "error") {
			details := fxV2ResultError(r.Preview)
			if details == "" {
				details = "tool call failed"
			}
			state.status, state.tool, state.details = "tool_failed", r.ToolName, details
			return
		}
		if name, ok := state.latestRunning(); ok {
			state.runningStatus(name)
			return
		}
		state.status, state.tool, state.details = "thinking", "", ""
	case "assistant", "assistant_running":
		var a struct {
			Text string `json:"text"`
		}
		if json.Unmarshal(data, &a) == nil && a.Text != "" {
			state.lastText = a.Text
		}
		// A failed tool stays surfaced until the agent moves past it; the
		// final answer itself is still part of the working turn (the turn
		// ends at turn_end / turn_committed).
		if state.status != "tool_failed" {
			state.status, state.tool, state.details = "thinking", "", ""
		}
	case "interruption":
		state.clearRunning()
		state.status, state.details = "interrupted", ""
	case "turn_end":
		state.clearRunning()
		state.status, state.tool, state.details = "idle", "", state.lastText
	}
}

// fxV2ResultError extracts a short error message from a tool_result preview.
// The preview is either plain text or a JSON object such as
// {"error":{"type":...,"message":...}}.
func fxV2ResultError(preview string) string {
	preview = strings.TrimSpace(preview)
	if preview == "" {
		return ""
	}
	var e struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
		Message string `json:"message"`
	}
	if strings.HasPrefix(preview, "{") && json.Unmarshal([]byte(preview), &e) == nil {
		if e.Error.Message != "" {
			return e.Error.Message
		}
		if e.Message != "" {
			return e.Message
		}
	}
	return firstLine(preview)
}
