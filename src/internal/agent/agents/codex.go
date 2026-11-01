package agents

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
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

// codexSessionsDir returns the directory Codex writes its rollout transcripts
// to: $CODEX_HOME/sessions when CODEX_HOME is set (Codex's own override, which
// the PTY inherits from Caw's environment), ~/.codex/sessions otherwise.
func codexSessionsDir() string {
	if h := os.Getenv("CODEX_HOME"); h != "" {
		return filepath.Join(h, "sessions")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".codex", "sessions")
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
	Timestamp string        `json:"timestamp,omitempty"`
	Type      string        `json:"type"`
	Payload   *CodexPayload `json:"payload,omitempty"`
}

type CodexContent struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// CodexItem is the `item` of an event_msg "item_completed" entry (Codex CLI
// 0.16x+). Only the fields the status watcher needs are decoded.
type CodexItem struct {
	// Type is "UserMessage", "AgentMessage", "CommandExecution", "Plan", ...
	Type    string          `json:"type"`
	ID      string          `json:"id,omitempty"`
	Content json.RawMessage `json:"content,omitempty"`
	Text    string          `json:"text,omitempty"`
	// Status/ExitCode/AggregatedOutput are set on CommandExecution items.
	Status           string `json:"status,omitempty"`
	ExitCode         *int   `json:"exit_code,omitempty"`
	AggregatedOutput string `json:"aggregated_output,omitempty"`
}

type CodexPayload struct {
	Type    string `json:"type"`
	Message string `json:"message,omitempty"`
	// Content is the content array of response_item "message" entries. Kept
	// raw so an unexpected shape (string/null) never makes the whole line
	// undecodable.
	Content json.RawMessage `json:"content,omitempty"`
	// Phase is set on event_msg "agent_message" and response_item "message"
	// entries by some CLI versions: "commentary" (interim thought) or
	// "final_answer" (turn done).
	Phase string `json:"phase,omitempty"`
	// Role is set on response_item "message" entries: developer/user/assistant.
	Role string `json:"role,omitempty"`
	// Name is the tool name for response_item "function_call" entries.
	Name string `json:"name,omitempty"`
	// CallID links a function_call to its function_call_output.
	CallID string `json:"call_id,omitempty"`
	// Output is the tool result of a function_call_output (a string in
	// current versions, an object in some older ones).
	Output json.RawMessage `json:"output,omitempty"`
	// Item is set on event_msg "item_completed" entries.
	Item *CodexItem `json:"item,omitempty"`
	// LastAgentMessage is set on event_msg "task_complete".
	LastAgentMessage string `json:"last_agent_message,omitempty"`
	// Error is set on event_msg "task_complete" entries when the turn ended
	// in failure (e.g. an API error). Carries a message and codex_error_info.
	Error json.RawMessage `json:"error,omitempty"`
}

// codexContentText joins the text parts of a raw content array.
func codexContentText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var parts []CodexContent
	if json.Unmarshal(raw, &parts) != nil {
		var s string
		if json.Unmarshal(raw, &s) == nil {
			return s
		}
		return ""
	}
	var textParts []string
	for _, c := range parts {
		if c.Text != "" {
			textParts = append(textParts, c.Text)
		}
	}
	return strings.Join(textParts, " ")
}

// codexPayloadText extracts the plain-text body of a Codex response_item
// payload that carries text either in its `message` field or as the joined
// `content[].text` entries. Used to read assistant answers and user prompts.
func codexPayloadText(p *CodexPayload) string {
	if p.Message != "" {
		return p.Message
	}
	return codexContentText(p.Content)
}

// codexInjectedUserText reports whether a response_item user message is
// context Codex injects on the user's behalf rather than a typed prompt:
// <environment_context>, <user_instructions>, <turn_aborted>,
// <collaboration_mode>, AGENTS.md instructions, ... These must neither start
// a turn nor become the card title (the <turn_aborted> notice used to
// replace the title after every interrupt).
func codexInjectedUserText(text string) bool {
	t := strings.TrimSpace(text)
	return t == "" || strings.HasPrefix(t, "<") || strings.HasPrefix(t, "# AGENTS.md instructions")
}

// codexExitCodeRe extracts the exit status Codex prints at the top of an
// exec_command / shell tool result ("Process exited with code 1").
var codexExitCodeRe = regexp.MustCompile(`(?m)^Process exited with code (-?\d+)`)

// codexOutputText decodes a function_call_output `output` into text.
func codexOutputText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var obj struct {
		Content string `json:"content"`
	}
	if json.Unmarshal(raw, &obj) == nil {
		return obj.Content
	}
	return ""
}

// codexErrorMessage returns the human-readable message of a task_complete
// `error`, unwrapping a JSON API error body ({"error":{"message":...}}).
func codexErrorMessage(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var e struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(raw, &e) != nil {
		var s string
		if json.Unmarshal(raw, &s) == nil {
			e.Message = s
		}
	}
	msg := strings.TrimSpace(e.Message)
	if strings.HasPrefix(msg, "{") {
		var api struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
			Message string `json:"message"`
		}
		if json.Unmarshal([]byte(msg), &api) == nil {
			if api.Error.Message != "" {
				msg = api.Error.Message
			} else if api.Message != "" {
				msg = api.Message
			}
		}
	}
	return msg
}

// codexSnippet returns the first non-empty line of s, truncated for display.
func codexSnippet(s string) string {
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			if len(l) > 200 {
				l = l[:200] + "…"
			}
			return l
		}
	}
	return ""
}

// codexDetails shortens assistant text for the card's details line.
func codexDetails(s string) string {
	s = strings.NewReplacer("<proposed_plan>", " ", "</proposed_plan>", " ").Replace(s)
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return s
}

// codexTracker is the incremental state machine behind the Codex watcher.
// It consumes rollout lines in order and keeps the session state between
// reads, so a read that only contains bookkeeping lines (token_count, a
// background command finishing after the turn, ...) never changes the
// reported status. (The previous implementation re-derived the status from
// each newly appended chunk alone and fell back to "thinking" when the chunk
// had nothing meaningful, which left finished turns stuck in "thinking" —
// e.g. when an exec_command that outlived its turn completed.)
//
// Turn lifecycle in a 0.16x rollout:
//
//	event_msg task_started                       -> thinking (turn start)
//	response_item message role=user              -> prompt (title)
//	event_msg item_completed UserMessage         -> prompt (title)
//	response_item function_call / custom_tool_call -> executing <tool>
//	  (request_user_input -> waiting_input)
//	event_msg item_completed CommandExecution status=failed -> tool_failed
//	response_item function_call_output           -> thinking (or tool_failed)
//	event_msg item_completed AgentMessage / response_item message assistant
//	                                             -> details (still working)
//	event_msg item_completed Plan                -> plan proposed in this turn
//	event_msg task_complete                      -> idle; waiting_input when the
//	                                                turn proposed a plan (Codex
//	                                                asks "Implement this plan?");
//	                                                interrupted + error text when
//	                                                the turn failed (API error)
//	event_msg turn_aborted                       -> interrupted (user cancelled)
//
// Older rollouts (event_msg user_message / agent_message with
// phase=final_answer) are handled too.
type codexTracker struct {
	status, tool, details, title string
	// stateAt is the rollout timestamp of the entry that last changed the
	// state. A TUI-only approval prompt seen in the PTY after it means the
	// in-flight call is blocked on the user.
	stateAt     time.Time
	turnActive  bool
	turnHasPlan bool
	failedCall  string
	calls       map[string]string
}

func newCodexTracker() *codexTracker {
	return &codexTracker{status: "idle", calls: make(map[string]string)}
}

func (t *codexTracker) setTitle(raw string) {
	if title := CleanPrompt(raw); title != "" {
		t.title = title
	}
}

func (t *codexTracker) startTurn() {
	t.turnActive = true
	t.turnHasPlan = false
	t.failedCall = ""
	t.status, t.tool, t.details = "thinking", "", ""
}

func (t *codexTracker) ensureTurn() {
	if !t.turnActive {
		t.startTurn()
	}
}

// assistantText records interim/final assistant text. A tool failure stays
// visible (red dot) while the assistant comments on it; the next tool call
// or the end of the turn clears it.
func (t *codexTracker) assistantText(text, phase string) {
	if strings.Contains(text, "<proposed_plan>") {
		t.turnHasPlan = true
	}
	if text != "" {
		t.details = codexDetails(text)
	}
	if phase == "final_answer" {
		t.finishTurn("", "")
		return
	}
	t.ensureTurn()
	if t.status != "tool_failed" {
		t.status, t.tool = "thinking", ""
	}
}

func (t *codexTracker) finishTurn(errMsg, lastMessage string) {
	t.turnActive = false
	t.failedCall = ""
	switch {
	case errMsg != "":
		// The turn died (API/stream error): Codex is back at its prompt, so
		// the card must leave the working column. Report it as interrupted
		// (idle column, red dot) with the error text instead of tool_failed,
		// which would keep the card in "working" forever.
		t.status, t.tool, t.details = "interrupted", "", codexSnippet(errMsg)
	case t.turnHasPlan:
		// Plan mode: Codex shows "Implement this plan?" and waits.
		t.status, t.tool = "waiting_input", "plan"
	default:
		t.status, t.tool = "idle", ""
	}
	if errMsg == "" && lastMessage != "" {
		t.details = codexDetails(lastMessage)
	}
}

// apply consumes one rollout line and reports whether it changed the state.
func (t *codexTracker) apply(line string) bool {
	var l CodexLogLine
	if json.Unmarshal([]byte(line), &l) != nil || l.Payload == nil {
		return false
	}
	before := [4]string{t.status, t.tool, t.details, t.title}
	p := l.Payload
	switch p.Type {
	case "task_started", "turn_started":
		t.startTurn()
	case "user_message":
		// Legacy event: the exact typed prompt.
		t.setTitle(p.Message)
		t.ensureTurn()
	case "message":
		switch p.Role {
		case "user":
			text := codexPayloadText(p)
			if codexInjectedUserText(text) {
				return false
			}
			t.setTitle(text)
			t.ensureTurn()
		case "assistant":
			t.assistantText(codexPayloadText(p), p.Phase)
		default:
			return false
		}
	case "agent_message":
		t.assistantText(codexPayloadText(p), p.Phase)
	case "item_completed":
		it := p.Item
		if it == nil {
			return false
		}
		switch it.Type {
		case "UserMessage":
			t.setTitle(codexContentText(it.Content))
			t.ensureTurn()
		case "AgentMessage":
			if t.turnActive {
				if text := codexContentText(it.Content); text != "" {
					t.details = codexDetails(text)
				}
			}
		case "Plan":
			t.turnHasPlan = true
		case "CommandExecution":
			// A command can outlive its turn (exec_command yields after
			// ~10s); its completion after task_complete must not reopen
			// the turn.
			if !t.turnActive {
				return false
			}
			failed := it.Status == "failed" || (it.ExitCode != nil && *it.ExitCode != 0)
			if !failed {
				return false
			}
			tool := t.calls[it.ID]
			if tool == "" {
				tool = "exec_command"
			}
			details := codexSnippet(it.AggregatedOutput)
			if details == "" && it.ExitCode != nil {
				details = "exit code " + strconv.Itoa(*it.ExitCode)
			}
			t.status, t.tool, t.details, t.failedCall = "tool_failed", tool, details, it.ID
		default:
			return false
		}
	case "function_call", "custom_tool_call", "local_shell_call":
		tool := p.Name
		if tool == "" {
			tool = "exec"
		}
		if p.CallID != "" {
			t.calls[p.CallID] = tool
		}
		t.ensureTurn()
		t.failedCall = ""
		t.tool, t.details = tool, ""
		if isUserInputTool(strings.ToLower(tool)) {
			t.status = "waiting_input"
		} else {
			t.status = "executing"
		}
	case "function_call_output", "custom_tool_call_output", "local_shell_call_output":
		if !t.turnActive {
			return false
		}
		if t.status == "tool_failed" && t.failedCall != "" && t.failedCall == p.CallID {
			break // already surfaced by item_completed
		}
		out := codexOutputText(p.Output)
		if m := codexExitCodeRe.FindStringSubmatch(out); m != nil && m[1] != "0" {
			tool := t.calls[p.CallID]
			if tool == "" {
				tool = t.tool
			}
			details := "exit code " + m[1]
			if i := strings.Index(out, "\nOutput:\n"); i >= 0 {
				if s := codexSnippet(out[i+len("\nOutput:\n"):]); s != "" {
					details = s
				}
			}
			t.status, t.tool, t.details, t.failedCall = "tool_failed", tool, details, p.CallID
			break
		}
		t.failedCall = ""
		t.status, t.tool = "thinking", ""
	case "task_complete":
		t.finishTurn(codexErrorMessage(p.Error), p.LastAgentMessage)
	case "turn_aborted":
		t.turnActive = false
		t.failedCall = ""
		t.status, t.tool = "interrupted", ""
	case "exec_approval_request", "apply_patch_approval_request", "request_permissions", "elicitation_request":
		// Not persisted by current versions (approvals are detected from the
		// PTY), but honoured if a version writes them.
		if !t.turnActive {
			return false
		}
		t.status = "waiting_input"
	default:
		return false
	}
	after := [4]string{t.status, t.tool, t.details, t.title}
	if before == after {
		return false
	}
	if ts, err := time.Parse(time.RFC3339Nano, l.Timestamp); err == nil {
		t.stateAt = ts
	}
	return true
}

// reportState returns the status to publish. pendingInputAt is the last
// time a Codex approval/plan prompt was seen in the pane's PTY output (see
// agent.LastPtyPendingInput): Codex only renders those prompts in the TUI and
// writes nothing to the rollout until the user decides, so a prompt seen
// after the last state change means the in-flight call is blocked on the
// user.
func (t *codexTracker) reportState(pendingInputAt time.Time) (status, tool, details string) {
	status, tool, details = t.status, t.tool, t.details
	if (status == "executing" || status == "thinking") && !pendingInputAt.IsZero() && pendingInputAt.After(t.stateAt) {
		status, details = "waiting_input", "Approval requested"
	}
	return status, tool, details
}

// readCodexLines reads the complete lines appended to filePath after offset
// and returns them with the offset just past the last complete line. A
// partially written trailing line is left for the next read instead of being
// dropped (or re-read twice, as happened when the offset was taken from a
// Stat() done before reading).
func readCodexLines(filePath string, offset int64) ([]string, int64, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, offset, err
	}
	defer f.Close()
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return nil, offset, err
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, offset, err
	}
	end := bytes.LastIndexByte(data, '\n')
	if end < 0 {
		return nil, offset, nil
	}
	var lines []string
	for _, l := range strings.Split(string(data[:end]), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, l)
		}
	}
	return lines, offset + int64(end) + 1, nil
}

// codexLastTurnStart returns the timestamp of the most recent turn start
// (task_started, or a typed user prompt) in a rollout file, reading only its
// tail. Used to attribute a freshly started session/turn to the pane where
// the user pressed Enter right before it.
func codexLastTurnStart(path string) time.Time {
	f, err := os.Open(path)
	if err != nil {
		return time.Time{}
	}
	defer f.Close()
	const tail = 512 * 1024
	if info, err := f.Stat(); err == nil && info.Size() > tail {
		_, _ = f.Seek(info.Size()-tail, io.SeekStart)
	}
	data, _ := io.ReadAll(f)
	lines := strings.Split(string(data), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := lines[i]
		if !strings.Contains(line, `"task_started"`) && !strings.Contains(line, `"turn_started"`) &&
			!strings.Contains(line, `"user_message"`) && !strings.Contains(line, `"UserMessage"`) {
			continue
		}
		var l CodexLogLine
		if json.Unmarshal([]byte(line), &l) != nil || l.Payload == nil {
			continue
		}
		switch l.Payload.Type {
		case "task_started", "turn_started", "user_message", "item_completed":
			if l.Payload.Type == "item_completed" && (l.Payload.Item == nil || l.Payload.Item.Type != "UserMessage") {
				continue
			}
			if ts, err := time.Parse(time.RFC3339Nano, l.Timestamp); err == nil {
				return ts
			}
		}
	}
	return time.Time{}
}

// codexSubmitWindow bounds how long after an Enter keypress a new turn may
// start and still be attributed to that pane.
const codexSubmitWindow = 2 * time.Minute

// codexOwnsSession decides whether the rollout at path, which is not bound
// to any pane yet, belongs to leaf sessionID. Every Codex pane shares one
// transcript tree (written by Codex's shared app-server daemon), so neither
// the cwd nor PTY output can tell the panes apart: a busy sibling's spinner
// produces PTY output continuously, which used to let it steal the session
// another pane had just created with /new. The session belongs to the pane
// in which the user pressed Enter most recently before its latest turn
// started.
func codexOwnsSession(sessionID, path string, watchStart time.Time) bool {
	ts := codexLastTurnStart(path)
	if ts.IsZero() {
		return false
	}
	if owner := agent.PtySubmitOwner("codex", ts, codexSubmitWindow); owner != "" {
		return owner == sessionID
	}
	// Nobody typed into a Codex pane before this turn: it was started
	// without the keyboard (e.g. a prompt passed on the command line). Only
	// accept it right after this pane launched, and only if this pane has
	// never been typed into (otherwise its own prompts would have matched).
	if !agent.LastPtySubmit(sessionID).IsZero() {
		return false
	}
	return !ts.Before(watchStart.Add(-2*time.Second)) && ts.Sub(watchStart) < 15*time.Second
}

func (w *CodexWatcher) Watch(ctx context.Context, sessionID string, cwd string, resume bool, callback func(status, tool, details, title string), heartbeat func()) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	dir := codexSessionsDir()
	const agentID = "codex"
	// Codex stores every session transcript under the shared sessions tree
	// with no per-cwd partitioning, so claims are keyed globally per agent:
	// two Codex panes never bind the same rollout file. We pass a fixed
	// sentinel ("") as the claim cwd, mirroring the Hermes watcher.
	const claimCwd = ""
	watchStart := time.Now()
	// On resume (codex resume --last), the agent reattaches to a pre-existing
	// session whose transcript file may predate this watcher. Widen the
	// search window to 1 hour so the resumed session is found.
	lookback := 10 * time.Second
	if resume {
		lookback = 1 * time.Hour
	}
	lastCheck := watchStart.Add(-lookback)
	var lastFileSize int64
	var watchedFilePath string
	var tr *codexTracker
	var lastReported string
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

	bind := func(path string) {
		if watchedFilePath != "" && watchedFilePath != path {
			UnclaimSession(agentID, claimCwd, watchedFilePath)
		}
		watchedFilePath = path
		lastFileSize = 0
		tr = newCodexTracker()
		lastCheck = time.Now()
		lastActivity = time.Now()
		silentTicks = 0
		if notifyCh != nil {
			notifier.Watch(watchedFilePath)
		}
		if sid := codexSessionIDFromPath(path); sid != "" {
			agent.RecordExternalSession(sessionID, sid)
		}
	}

	// report publishes the tracker state. Codex renders its approval
	// prompts (exec / patch / permissions / network) and the "Implement
	// this plan?" question only in the TUI, never in the rollout, so a
	// prompt marker seen in the PTY after the last state change turns an
	// in-flight call into waiting_input. force is set after new transcript
	// lines were read (which also feeds the idle watchdog); otherwise only a
	// change is reported.
	report := func(force bool) {
		if tr == nil {
			return
		}
		st, tool, details := tr.reportState(agent.LastPtyPendingInput(sessionID))
		key := st + "\x00" + tool + "\x00" + details + "\x00" + tr.title
		if !force && key == lastReported {
			return
		}
		lastReported = key
		callback(st, tool, details, tr.title)
	}

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
		lines, newOffset, err := readCodexLines(watchedFilePath, lastFileSize)
		if err != nil || newOffset == lastFileSize {
			return false
		}
		lastFileSize = newOffset
		lastActivity = info.ModTime()
		silentTicks = 0
		for _, l := range lines {
			tr.apply(l)
		}
		report(true)
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
						bind(exactPath)
					}
				}
				if watchedFilePath == "" {
					candidates, err := FindEarliestFiles(dir, ".jsonl", lastCheck)
					if err != nil {
						continue
					}
					for _, c := range candidates {
						if owner := ClaimedBy(agentID, claimCwd, c.Path); owner != "" && owner != sessionID {
							continue
						}
						// `codex resume --last` reattaches to an existing
						// session; a fresh pane must only take a session its
						// own user input started.
						if !resume && !codexOwnsSession(sessionID, c.Path, watchStart) {
							continue
						}
						if ClaimSessionForLeaf(agentID, claimCwd, c.Path, sessionID) {
							bind(c.Path)
							break
						}
					}
				}
			}
			if watchedFilePath != "" {
				if !readWatched() {
					silentTicks++
					report(false)
				}

				// Mid-session re-bind for /new and /resume: Codex switches to
				// another rollout while the PTY stays alive. Only a session
				// whose latest turn was started from this pane (Enter pressed
				// here right before it) may be taken over. That ownership
				// check is strict, so one silent poll is enough to switch
				// (the generic rebindSilenceTicks gate would miss most of a
				// short first turn after /new). A pane whose own turn is
				// still in flight never switches: Codex only starts another
				// session from an idle composer.
				if silentTicks >= 1 && tr != nil && !tr.turnActive {
					cands, _ := FindLatestFiles(dir, ".jsonl", lastActivity)
					var others []RebindCandidate
					for _, c := range cands {
						if c.Path == watchedFilePath {
							continue
						}
						if owner := ClaimedBy(agentID, claimCwd, c.Path); owner != "" && owner != sessionID {
							continue
						}
						if !codexOwnsSession(sessionID, c.Path, watchStart) {
							continue
						}
						others = append(others, RebindCandidate{Key: c.Path, ModTime: c.ModTime})
					}
					newKey := ShouldRebind(rebindSilenceTicks, watchedFilePath, lastActivity, others)
					if newKey != "" && newKey != watchedFilePath && ClaimSessionForLeaf(agentID, claimCwd, newKey, sessionID) {
						bind(newKey)
						readWatched()
					}
				}
			}
		}
	}
}

// parseCodexLog parses a rollout from offset with a fresh tracker and
// reports the resulting state once. The watcher keeps one tracker per bound
// rollout instead; this wrapper is used by tests and one-shot callers.
func (w *CodexWatcher) parseCodexLog(filePath string, offset int64, callback func(status, tool, details, title string)) {
	lines, _, err := readCodexLines(filePath, offset)
	if err != nil || len(lines) == 0 {
		return
	}
	tr := newCodexTracker()
	for _, l := range lines {
		tr.apply(l)
	}
	callback(tr.status, tr.tool, tr.details, tr.title)
}
