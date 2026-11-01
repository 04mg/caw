package agents

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/04mg/caw/internal/agent"
)

// ClaudeWatcher tracks a Claude Code TUI session through its JSONL transcript
// (~/.claude/projects/<encoded-cwd>/<session-uuid>.jsonl).
//
// Binding: Caw launches fresh Claude panes with `--session-id <uuid>` and
// resumes them with `--resume <uuid>` (see terminal.pinClaudeSession and
// resumeCmdForAgent), so the watcher reads the session id from the launch
// command and binds to exactly that transcript. A heuristic scan of the
// project directory is only the fallback for user-customised commands.
//
// Status: every transcript line is fed, in order, into a persistent
// claudeTurnState state machine (prompt -> thinking -> tool_use/executing ->
// tool_result -> ... -> turn_duration/idle). Claude 2.1 interleaves many
// non-conversation lines (api-request blobs, attachments, ai-title, mode,
// queue-operation, cost-state, ...) that must never move the status.
//
// Things the transcript cannot show are taken from the PTY:
//   - tool permission dialogs (Claude writes nothing while one is pending) are
//     detected from the rendered dialog text (agent.LastPtyPendingInput) and
//     reported as waiting_input until the user answers (agent.LastPtyAnswerKey);
//   - ESC / Ctrl+C interrupts are reported immediately (agent.LastPtyInterrupt);
//   - /clear (and /resume inside the TUI) switch to another transcript file,
//     which is attributed to the pane whose submitted input produced it.
type ClaudeWatcher struct{}

func init() {
	agent.RegisterStatusWatcher("claude", &ClaudeWatcher{})
}

// ClaudeLogLine is one JSONL entry of a Claude Code transcript. Only the
// fields the status tracker needs are decoded.
type ClaudeLogLine struct {
	Type                string         `json:"type"`
	Subtype             string         `json:"subtype,omitempty"`
	Message             *ClaudeMessage `json:"message,omitempty"`
	IsSidechain         bool           `json:"isSidechain,omitempty"`
	IsMeta              bool           `json:"isMeta,omitempty"`
	IsCompactSummary    bool           `json:"isCompactSummary,omitempty"`
	IsAPIErrorMessage   bool           `json:"isApiErrorMessage,omitempty"`
	QueueTranscriptOnly bool           `json:"queueTranscriptOnly,omitempty"`
	Origin              *struct {
		Kind string `json:"kind"`
	} `json:"origin,omitempty"`
	// Operation/Content are set on "queue-operation" lines (Content is the
	// queued text, e.g. a <task-notification>).
	Operation     string          `json:"operation,omitempty"`
	Content       json.RawMessage `json:"content,omitempty"`
	Timestamp     string          `json:"timestamp,omitempty"`
	ToolUseResult json.RawMessage `json:"toolUseResult,omitempty"`
}

type ClaudeMessage struct {
	Role       string          `json:"role"`
	Content    json.RawMessage `json:"content"`
	StopReason string          `json:"stop_reason,omitempty"`
}

type ClaudeBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
	Name string `json:"name,omitempty"` // tool name for tool_use blocks
	// ID is the tool_use id, used to link a tool_result back to its tool_use.
	ID string `json:"id,omitempty"`
	// IsError is set on tool_result blocks: Claude writes "is_error": true
	// when a tool call failed. Used to surface tool failures with a red dot.
	IsError   bool   `json:"is_error,omitempty"`
	ToolUseID string `json:"tool_use_id,omitempty"` // links a tool_result to its tool_use
	// Content is the tool_result payload: a string or an array of blocks.
	Content json.RawMessage `json:"content,omitempty"`
}

// parseClaudeContent decodes the Content field of a ClaudeMessage, which may
// be either a JSON array of block objects (the common case) or a plain JSON
// string (used by Claude Code for simple text user messages like "hi").
// Returns the parsed blocks and the raw text if content was a plain string.
func parseClaudeContent(raw json.RawMessage) ([]ClaudeBlock, string) {
	if len(raw) == 0 {
		return nil, ""
	}
	var blocks []ClaudeBlock
	if json.Unmarshal(raw, &blocks) == nil {
		return blocks, ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return nil, s
	}
	return nil, ""
}

// claudeToolResultText flattens a tool_result payload (string or blocks).
func claudeToolResultText(raw json.RawMessage) string {
	blocks, text := parseClaudeContent(raw)
	if text != "" {
		return text
	}
	var parts []string
	for _, b := range blocks {
		if b.Text != "" {
			parts = append(parts, b.Text)
		}
	}
	return strings.Join(parts, "\n")
}

var toolUseErrorTagRe = regexp.MustCompile(`</?tool_use_error>`)

// claudeErrorDetails turns a failed tool_result payload into a short,
// single-line message for the card.
func claudeErrorDetails(text string) string {
	text = strings.TrimSpace(toolUseErrorTagRe.ReplaceAllString(text, ""))
	text = strings.Join(strings.Fields(text), " ")
	if len(text) > 200 {
		text = text[:200] + "…"
	}
	if text == "" {
		return "tool call failed"
	}
	return text
}

var (
	claudeTaskIDRe     = regexp.MustCompile(`<task-id>([^<]+)</task-id>`)
	claudeTaskStatusRe = regexp.MustCompile(`<status>([^<]+)</status>`)
)

// ----- transcript state machine ---------------------------------------------

// claudeTurnState is the incremental status state of one Claude transcript.
// Lines must be applied in file order. It is a pure function of the
// transcript, except for the sticky PTY interrupt (ptyInterrupt).
type claudeTurnState struct {
	status  string
	tool    string
	details string
	// title is the latest prompt the user typed (CleanPrompt'ed).
	title string
	// pending maps unresolved tool_use ids to tool names; toolNames keeps
	// every tool_use id seen so a tool_result can be attributed.
	pending   map[string]string
	toolNames map[string]string
	// bgAgents holds the ids of subagents launched in the background (Agent
	// tool, status "async_launched") that have not reported completion.
	// While any is running after the main turn ended, Claude shows
	// "Waiting for N background agents" and continues on its own when they
	// finish, so the session is still working.
	bgAgents map[string]bool
	// stickyInterruptAt is set when the user interrupted from the PTY. The
	// status then stays "interrupted" — even if late transcript lines from the
	// cancelled turn still arrive — until a new prompt, or until transcript
	// activity clearly newer than the interrupt shows Claude kept working
	// (the keypress did not actually interrupt anything).
	stickyInterruptAt time.Time
	// resolved counts events that end any pending interactive prompt
	// (tool_result, new prompt, interrupt); resolvedAt is when the latest one
	// was written (transcript timestamp, or when it was read). The watcher
	// drops a PTY prompt sighting older than resolvedAt.
	resolved   int
	resolvedAt time.Time
	// sawWork records that a working state was entered since the last
	// takeSawWork call (used to surface short turns that start and finish
	// between two reads).
	sawWork bool
}

func newClaudeTurnState() *claudeTurnState {
	return &claudeTurnState{
		status:    "idle",
		pending:   map[string]string{},
		toolNames: map[string]string{},
		bgAgents:  map[string]bool{},
	}
}

func isClaudeWorking(status string) bool {
	return status == "thinking" || status == "executing" || status == "tool_failed" || status == "waiting_input"
}

func (s *claudeTurnState) set(status, tool, details string) {
	if !s.stickyInterruptAt.IsZero() && status != "interrupted" {
		return
	}
	s.status, s.tool, s.details = status, tool, details
	if isClaudeWorking(status) {
		s.sawWork = true
	}
}

func (s *claudeTurnState) takeSawWork() bool {
	v := s.sawWork
	s.sawWork = false
	return v
}

// maybeUnstick drops a PTY-sticky interrupt when a transcript line written
// well after the keypress shows Claude is still working on the same turn.
func (s *claudeTurnState) maybeUnstick(ts string) {
	if s.stickyInterruptAt.IsZero() || ts == "" {
		return
	}
	t, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		return
	}
	if t.After(s.stickyInterruptAt.Add(3 * time.Second)) {
		s.stickyInterruptAt = time.Time{}
	}
}

func (s *claudeTurnState) apply(line string) {
	var ll ClaudeLogLine
	if json.Unmarshal([]byte(line), &ll) != nil {
		return
	}
	if ll.IsSidechain {
		// Subagent traffic; the main thread's Agent tool_use/tool_result
		// already describes it.
		return
	}
	switch ll.Type {
	case "user":
		s.applyUser(&ll)
	case "assistant":
		s.applyAssistant(&ll)
	case "system":
		switch ll.Subtype {
		case "turn_duration":
			s.endTurn()
		case "api_error":
			if s.status == "thinking" {
				s.set("thinking", "", "API error, retrying")
			}
		}
	case "result":
		// Headless (-p) transcripts end with a result line.
		s.endTurn()
	case "queue-operation":
		if ll.Operation == "enqueue" {
			var text string
			if json.Unmarshal(ll.Content, &text) == nil {
				s.applyTaskNotification(text)
			}
		}
	}
	// Every other type (attachment, api-request*, mode, permission-mode,
	// ai-title, last-prompt, file-history-snapshot, tools-used, cost-state,
	// agent-name, ...) is bookkeeping and never changes the status.
}

// markResolved records an event that answers/ends any pending prompt.
func (s *claudeTurnState) markResolved(ts string) {
	s.resolved++
	s.resolvedAt = time.Now()
	if t, err := time.Parse(time.RFC3339Nano, ts); err == nil && t.Before(s.resolvedAt) {
		s.resolvedAt = t
	}
}

func (s *claudeTurnState) startTurn() {
	s.stickyInterruptAt = time.Time{}
	s.pending = map[string]string{}
	s.set("thinking", "", "")
}

// endTurn marks the main turn as finished. An interrupt stays visible; with
// background agents still running the session keeps working.
func (s *claudeTurnState) endTurn() {
	s.pending = map[string]string{}
	if s.status == "interrupted" {
		return
	}
	s.settleIdle()
}

func (s *claudeTurnState) settleIdle() {
	if n := len(s.bgAgents); n > 0 {
		label := "agent"
		if n > 1 {
			label = "agents"
		}
		s.set("executing", "Agent", fmt.Sprintf("Waiting for %d background %s", n, label))
		return
	}
	details := s.details
	if s.status == "executing" && s.tool == "Agent" {
		details = ""
	}
	s.set("idle", "", details)
}

func (s *claudeTurnState) interrupt(tool string) {
	s.pending = map[string]string{}
	if tool == "" {
		tool = s.tool
	}
	s.status, s.tool, s.details = "interrupted", tool, ""
}

// ptyInterrupt applies an interrupt keypress seen on the PTY while working.
func (s *claudeTurnState) ptyInterrupt(at time.Time) {
	s.interrupt("")
	s.stickyInterruptAt = at
}

func (s *claudeTurnState) applyUser(ll *ClaudeLogLine) {
	if ll.Message == nil {
		return
	}
	blocks, text := parseClaudeContent(ll.Message.Content)
	hasResult := false
	for _, b := range blocks {
		if b.Type == "tool_result" {
			hasResult = true
			s.applyToolResult(b, ll)
		}
	}
	if hasResult {
		return
	}
	if text == "" {
		for _, b := range blocks {
			if b.Type == "text" && b.Text != "" {
				text = b.Text
				break
			}
		}
	}
	if text == "" {
		return
	}
	trimmed := strings.TrimSpace(text)
	if strings.Contains(trimmed, "<task-notification>") {
		s.applyTaskNotification(trimmed)
	}
	if ll.QueueTranscriptOnly || ll.IsCompactSummary {
		// Transcript-only copies of queued notifications and compaction
		// summaries do not start a turn (a summary may also quote earlier
		// interrupt markers, which must not count).
		return
	}
	if strings.HasPrefix(strings.ToLower(trimmed), "[request interrupted by user") {
		s.markResolved(ll.Timestamp)
		s.interrupt("")
		return
	}
	switch {
	case strings.HasPrefix(trimmed, "<local-command-stdout>"), strings.HasPrefix(trimmed, "<local-command-stderr>"),
		strings.HasPrefix(trimmed, "<bash-stdout>"), strings.HasPrefix(trimmed, "<bash-stderr>"):
		// A local slash command (/compact, /model, ...) or a `!` shell
		// command finished. These never get a turn_duration line.
		if s.status != "interrupted" {
			s.settleIdle()
		}
		return
	case strings.HasPrefix(trimmed, "<bash-input>"):
		s.startTurn()
		s.set("executing", "Bash", "")
		return
	case strings.HasPrefix(trimmed, "<command-name>"), strings.HasPrefix(trimmed, "<command-message>"),
		strings.HasPrefix(trimmed, "<local-command-caveat>"):
		// Slash-command bookkeeping (/clear, /model, ...). Prompt-type
		// commands are followed by assistant lines that drive the status.
		return
	}
	kind := ""
	if ll.Origin != nil {
		kind = ll.Origin.Kind
	}
	if ll.IsMeta && kind != "peer" && kind != "task-notification" {
		// System reminders and caveats; but a subagent hand-back ("peer")
		// or a task notification starts a new assistant turn.
		return
	}
	s.markResolved(ll.Timestamp)
	s.startTurn()
	if (kind == "" || kind == "human") && !ll.IsMeta && !strings.HasPrefix(trimmed, "/") && !strings.HasPrefix(trimmed, "<") {
		if t := CleanPrompt(text); t != "" {
			s.title = t
		}
	}
}

func (s *claudeTurnState) applyToolResult(b ClaudeBlock, ll *ClaudeLogLine) {
	s.maybeUnstick(ll.Timestamp)
	name := s.toolNames[b.ToolUseID]
	delete(s.pending, b.ToolUseID)
	s.markResolved(ll.Timestamp)
	text := claudeToolResultText(b.Content)
	if b.IsError {
		low := strings.ToLower(text)
		if strings.HasPrefix(low, "the user doesn't want to proceed") || strings.Contains(low, "[request interrupted by user") {
			// Permission denied / dialog cancelled. Claude follows up with
			// an interrupt marker; report it now.
			s.interrupt(name)
			return
		}
		s.set("tool_failed", name, claudeErrorDetails(text))
		return
	}
	if len(ll.ToolUseResult) > 0 && ll.ToolUseResult[0] == '{' {
		var r struct {
			Status  string `json:"status"`
			AgentID string `json:"agentId"`
		}
		if json.Unmarshal(ll.ToolUseResult, &r) == nil && r.Status == "async_launched" && r.AgentID != "" {
			s.bgAgents[r.AgentID] = true
		}
	}
	if len(s.pending) > 0 {
		s.set("executing", s.anyPendingTool(), "")
		return
	}
	s.set("thinking", "", "")
}

func (s *claudeTurnState) anyPendingTool() string {
	ids := make([]string, 0, len(s.pending))
	for id := range s.pending {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	if len(ids) == 0 {
		return ""
	}
	return s.pending[ids[0]]
}

func (s *claudeTurnState) applyAssistant(ll *ClaudeLogLine) {
	if ll.Message == nil {
		return
	}
	s.maybeUnstick(ll.Timestamp)
	blocks, plain := parseClaudeContent(ll.Message.Content)
	if ll.IsAPIErrorMessage {
		// Synthetic assistant message for a non-retryable API error: the
		// turn is over.
		text := plain
		for _, b := range blocks {
			if b.Type == "text" && b.Text != "" {
				text = b.Text
			}
		}
		s.pending = map[string]string{}
		if s.status != "interrupted" {
			s.set("idle", "", text)
		}
		return
	}
	var lastTool, lastText string
	for _, b := range blocks {
		switch b.Type {
		case "tool_use":
			if b.Name == "" {
				continue
			}
			if b.ID != "" {
				s.pending[b.ID] = b.Name
				s.toolNames[b.ID] = b.Name
			}
			lastTool = b.Name
		case "text":
			if b.Text != "" {
				lastText = b.Text
			}
		}
	}
	if plain != "" {
		lastText = plain
	}
	switch {
	case lastTool != "":
		if isUserInputTool(strings.ToLower(lastTool)) {
			s.set("waiting_input", lastTool, "")
		} else {
			s.set("executing", lastTool, "")
		}
	case len(s.pending) > 0:
		s.set("executing", s.anyPendingTool(), "")
	default:
		details := s.details
		if lastText != "" || s.status != "thinking" {
			details = lastText
		}
		s.set("thinking", "", details)
		if ll.Message.StopReason == "end_turn" {
			s.endTurn()
		}
	}
}

// applyTaskNotification marks background agents as finished when Claude
// receives their <task-notification>.
func (s *claudeTurnState) applyTaskNotification(text string) {
	if !strings.Contains(text, "<task-notification>") {
		return
	}
	id := claudeTaskIDRe.FindStringSubmatch(text)
	if id == nil {
		return
	}
	if st := claudeTaskStatusRe.FindStringSubmatch(text); st != nil && strings.TrimSpace(st[1]) == "running" {
		return
	}
	s.finishBackgroundAgent(strings.TrimSpace(id[1]))
}

func (s *claudeTurnState) finishBackgroundAgent(id string) {
	if !s.bgAgents[id] {
		return
	}
	delete(s.bgAgents, id)
	if s.status == "executing" && s.tool == "Agent" && len(s.pending) == 0 {
		// Main turn already ended; re-evaluate the waiting state.
		s.settleIdle()
	}
}

// settleCatchUp is applied after replaying a transcript that predates this
// Claude process (resume after a Caw/agent restart): whatever turn was in
// flight died with the previous process, so the session is idle at its
// prompt.
func (s *claudeTurnState) settleCatchUp() {
	s.pending = map[string]string{}
	s.bgAgents = map[string]bool{}
	s.stickyInterruptAt = time.Time{}
	if s.status != "idle" {
		details := s.details
		if s.status != "thinking" {
			details = ""
		}
		s.status, s.tool, s.details = "idle", "", details
	}
	s.sawWork = false
}

// ----- transcript discovery ------------------------------------------------

// claudeEncodePath mirrors Claude Code's project directory naming: every
// character that is not an ASCII letter or digit becomes '-'
// ("/root/.caw/worktrees/p_1" -> "-root--caw-worktrees-p-1").
func claudeEncodePath(p string) string {
	var b strings.Builder
	for _, r := range p {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	return b.String()
}

// claudeProjectDir returns the subdirectory of ~/.claude/projects/ that holds
// cwd's transcripts, or "" when it does not exist (yet). It never falls back
// to the whole projects tree: that used to bind panes to transcripts of
// unrelated projects (and to subagent transcripts nested in session dirs).
func claudeProjectDir(baseDir, cwd string) string {
	if cwd == "" {
		return ""
	}
	for _, name := range []string{claudeEncodePath(cwd), encodePathForDir(cwd)} {
		candidate := filepath.Join(baseDir, name)
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate
		}
	}
	return ""
}

// findClaudeTranscript locates <id>.jsonl, preferring cwd's project dir and
// falling back to any project dir (the encoding of very long or unusual paths
// may differ). Returns "" when it does not exist yet.
func findClaudeTranscript(baseDir, cwd, id string) string {
	if dir := claudeProjectDir(baseDir, cwd); dir != "" {
		p := filepath.Join(dir, id+".jsonl")
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return p
		}
	}
	matches, _ := filepath.Glob(filepath.Join(baseDir, "*", id+".jsonl"))
	if len(matches) > 0 {
		return matches[0]
	}
	return ""
}

// claudeSessionIDFromCmd returns the session id pinned by a Claude launch
// command (--session-id <uuid>, --resume/-r <uuid>), or "".
func claudeSessionIDFromCmd(cmd []string) string {
	for _, a := range cmd {
		if a == "--fork-session" {
			return "" // resumed history is copied into a new, unknown id
		}
	}
	for i := 1; i < len(cmd); i++ {
		a := cmd[i]
		var v string
		switch {
		case a == "--session-id" || a == "--resume" || a == "-r":
			if i+1 < len(cmd) {
				v = cmd[i+1]
			}
		case strings.HasPrefix(a, "--session-id="):
			v = strings.TrimPrefix(a, "--session-id=")
		case strings.HasPrefix(a, "--resume="):
			v = strings.TrimPrefix(a, "--resume=")
		}
		if v != "" {
			if _, err := uuid.Parse(v); err == nil {
				return v
			}
		}
	}
	return ""
}

// listClaudeTranscripts returns the top-level *.jsonl files of a project dir
// (subagent transcripts live in <session>/subagents/ and are excluded).
func listClaudeTranscripts(dir string) []FileCandidate {
	if dir == "" {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []FileCandidate
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, FileCandidate{Path: filepath.Join(dir, e.Name()), ModTime: info.ModTime()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ModTime.Before(out[j].ModTime) })
	return out
}

// claudeFileStartTime returns the timestamp of the first timestamped entry of
// a transcript (when the session was created), or the zero time.
func claudeFileStartTime(path string) time.Time {
	f, err := os.Open(path)
	if err != nil {
		return time.Time{}
	}
	defer f.Close()
	buf := make([]byte, 256*1024)
	n, _ := io.ReadFull(f, buf)
	for _, line := range bytes.Split(buf[:n], []byte("\n")) {
		var v struct {
			Timestamp string `json:"timestamp"`
		}
		if json.Unmarshal(line, &v) == nil && v.Timestamp != "" {
			if t, err := time.Parse(time.RFC3339Nano, v.Timestamp); err == nil {
				return t
			}
		}
	}
	return time.Time{}
}

// ----- watcher ---------------------------------------------------------------

func (w *ClaudeWatcher) Watch(ctx context.Context, sessionID string, cwd string, resume bool, callback func(status, tool, details, title string), heartbeat func()) {
	const agentID = "claude"
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	home, _ := os.UserHomeDir()
	baseDir := filepath.Join(home, ".claude", "projects")
	started := time.Now()

	// The session id pinned by the launch command, if any. Pre-claim its
	// expected transcript so a sibling pane in the same cwd can never adopt
	// it before this watcher sees the file appear.
	pinnedID := claudeSessionIDFromCmd(agent.SessionLaunchCmd(sessionID))
	expectedPath := ""
	if pinnedID != "" {
		expectedPath = filepath.Join(baseDir, claudeEncodePath(cwd), pinnedID+".jsonl")
		ClaimSessionForLeaf(agentID, cwd, expectedPath, sessionID)
	}

	var notifyCh <-chan struct{}
	notifier, nerr := NewFileChangeNotifier()
	if nerr == nil {
		defer notifier.Close()
		notifyCh = notifier.Notify()
		if expectedPath != "" {
			notifier.Watch(expectedPath)
		}
	}

	var (
		watched string
		offset  int64
		st      = newClaudeTurnState()
		// lastConvWrite is the wall-clock time a conversation line was last
		// read from the watched transcript (used to attribute /clear).
		lastConvWrite time.Time
		lastResolved  int
		lastInterrupt = agent.LastPtyInterrupt(sessionID)
		emitted       struct{ status, tool, details, title string }
		hasEmitted    bool
	)

	defer func() {
		if watched != "" {
			UnclaimSession(agentID, cwd, watched)
		}
		if expectedPath != "" {
			UnclaimSession(agentID, cwd, expectedPath)
		}
	}()

	// effective overlays the PTY-only permission dialog signal on the
	// transcript state.
	effective := func() (string, string, string) {
		status, tool, details := st.status, st.tool, st.details
		if status == "executing" && !isUserInputTool(strings.ToLower(tool)) {
			if pt := agent.LastPtyPendingInput(sessionID); !pt.IsZero() && pt.After(agent.LastPtyAnswerKey(sessionID)) {
				return "waiting_input", tool, "Waiting for permission"
			}
		}
		return status, tool, details
	}

	emit := func(status, tool, details string) {
		if hasEmitted && emitted.status == status && emitted.tool == tool && emitted.details == details && emitted.title == st.title {
			return
		}
		hasEmitted = true
		emitted.status, emitted.tool, emitted.details, emitted.title = status, tool, details, st.title
		callback(status, tool, details, st.title)
	}

	// publish reports the current state. When a whole short turn (prompt ->
	// answer) landed between two reads, the working phase is reported first
	// so the card (and the "finished" notification) still sees the turn.
	publish := func() {
		if st.resolved != lastResolved {
			lastResolved = st.resolved
			// Only a sighting from before the resolving line was written
			// is stale; a dialog rendered after it (read late) still counts.
			if pt := agent.LastPtyPendingInput(sessionID); !pt.IsZero() && pt.Before(st.resolvedAt) {
				agent.ClearPtyPendingInput(sessionID)
			}
		}
		status, tool, details := effective()
		sawWork := st.takeSawWork()
		if sawWork && !isClaudeWorking(status) && status != "interrupted" && (!hasEmitted || !isClaudeWorking(emitted.status)) {
			emit("thinking", "", "")
		}
		emit(status, tool, details)
	}

	readWatched := func() bool {
		if watched == "" {
			return false
		}
		info, err := os.Stat(watched)
		if err != nil {
			UnclaimSession(agentID, cwd, watched)
			watched = ""
			return false
		}
		if info.Size() < offset {
			// Rewritten/truncated: start over.
			offset = 0
			st = newClaudeTurnState()
		}
		if info.Size() == offset {
			return false
		}
		lines, next, err := readCompleteLines(watched, offset)
		if err != nil {
			return false
		}
		offset = next
		for _, l := range lines {
			st.apply(l)
			if isClaudeConversationLine(l) {
				lastConvWrite = time.Now()
			}
		}
		return len(lines) > 0
	}

	// bind switches the watcher to transcript path. catchUp marks a
	// transcript written by an earlier process (resume): its in-flight turn
	// is gone, so the replayed state settles to idle.
	bind := func(path string, catchUp bool) {
		if watched != "" && watched != path {
			UnclaimSession(agentID, cwd, watched)
		}
		watched = path
		offset = 0
		st = newClaudeTurnState()
		lastConvWrite = time.Time{}
		if notifyCh != nil {
			notifier.Watch(path)
		}
		agent.RecordExternalSession(sessionID, path)
		readWatched()
		if catchUp {
			st.settleCatchUp()
		}
		// Sightings that predate this transcript's latest resolving line are
		// stale; a permission dialog rendered before the (late) bind is not.
		lastResolved = st.resolved
		if pt := agent.LastPtyPendingInput(sessionID); !pt.IsZero() && (catchUp || pt.Before(st.resolvedAt)) {
			agent.ClearPtyPendingInput(sessionID)
		}
		publish()
	}

	tryBind := func() {
		if pinnedID != "" {
			p := findClaudeTranscript(baseDir, cwd, pinnedID)
			if p == "" {
				return
			}
			if p != expectedPath && !ClaimSessionForLeaf(agentID, cwd, p, sessionID) {
				return
			}
			info, err := os.Stat(p)
			bind(p, resume && err == nil && info.ModTime().Before(started))
			return
		}
		// A transcript persisted for this leaf by a previous Caw process.
		if exact := agent.PersistedExternalSession(sessionID); exact != "" && strings.HasSuffix(exact, ".jsonl") {
			if info, err := os.Stat(exact); err == nil && !info.IsDir() && ClaimSessionForLeaf(agentID, cwd, exact, sessionID) {
				bind(exact, info.ModTime().Before(started))
				return
			}
		}
		// Fallback for custom launch commands without a pinned id: scan the
		// cwd's project dir (never the whole projects tree, never subagent
		// transcripts). On resume (--continue) take the most recent
		// unclaimed transcript; otherwise only a transcript created after
		// this pane started, earliest first so concurrent panes pair up in
		// launch order.
		cands := listClaudeTranscripts(claudeProjectDir(baseDir, cwd))
		if resume {
			for i := len(cands) - 1; i >= 0; i-- {
				if ClaimSessionForLeaf(agentID, cwd, cands[i].Path, sessionID) {
					bind(cands[i].Path, cands[i].ModTime.Before(started))
					return
				}
			}
			return
		}
		for _, c := range cands {
			if c.ModTime.Before(started.Add(-10 * time.Second)) {
				continue
			}
			if t := claudeFileStartTime(c.Path); !t.IsZero() && t.Before(started.Add(-10*time.Second)) {
				continue // an older session that merely got touched
			}
			if ClaimSessionForLeaf(agentID, cwd, c.Path, sessionID) {
				bind(c.Path, false)
				return
			}
		}
	}

	// checkRebind follows /clear (new transcript) and /resume inside the TUI
	// (another transcript resumes receiving writes). The switch is attributed
	// to this pane only when the user submitted input here, that input did
	// not land in the current transcript, and an unclaimed transcript in the
	// same project dir was written right after it.
	checkRebind := func() {
		dir := ""
		if watched != "" {
			dir = filepath.Dir(watched)
		} else if pinnedID != "" {
			// The pinned transcript never appeared: the user ran /clear or
			// /resume before sending a first message.
			dir = claudeProjectDir(baseDir, cwd)
		}
		if dir == "" {
			return
		}
		submit := agent.LastPtyAnswerKey(sessionID)
		if submit.IsZero() || time.Since(submit) < 1500*time.Millisecond || time.Since(submit) > 30*time.Second {
			return
		}
		if !lastConvWrite.IsZero() && !lastConvWrite.Before(submit) {
			return
		}
		var best FileCandidate
		for _, c := range listClaudeTranscripts(dir) {
			if c.Path == watched || c.ModTime.Before(submit.Add(-1*time.Second)) || c.ModTime.After(submit.Add(15*time.Second)) {
				continue
			}
			if ClaimedBy(agentID, cwd, c.Path) != "" {
				continue
			}
			if best.Path == "" || c.ModTime.After(best.ModTime) {
				best = c
			}
		}
		if best.Path == "" || !ClaimSessionForLeaf(agentID, cwd, best.Path, sessionID) {
			return
		}
		if expectedPath != "" && expectedPath != best.Path {
			UnclaimSession(agentID, cwd, expectedPath)
			expectedPath = ""
		}
		pinnedID = ""
		// A brand-new file (/clear) is replayed live; an older transcript
		// that just resumed receiving writes (/resume) is replayed up to
		// now and settled, then followed live.
		catchUp := claudeFileStartTime(best.Path).Before(submit.Add(-2 * time.Second))
		bind(best.Path, false)
		if catchUp && !isClaudeWorking(st.status) {
			st.settleCatchUp()
			publish()
		}
	}

	// checkBackgroundAgents drops background agents whose subagent metadata
	// says they finished (backup for a missed <task-notification>).
	checkBackgroundAgents := func() {
		if len(st.bgAgents) == 0 || watched == "" {
			return
		}
		dir := strings.TrimSuffix(watched, ".jsonl")
		for id := range st.bgAgents {
			data, err := os.ReadFile(filepath.Join(dir, "subagents", "agent-"+id+".meta.json"))
			if err != nil {
				continue
			}
			var meta struct {
				Finished bool `json:"finished"`
			}
			if json.Unmarshal(data, &meta) == nil && meta.Finished {
				st.finishBackgroundAgent(id)
			}
		}
	}

	// checkPtyInterrupt reports a fresh ESC/Ctrl+C immediately while the
	// turn is working; Claude may write its interrupt marker a moment later.
	checkPtyInterrupt := func() {
		last := agent.LastPtyInterrupt(sessionID)
		if !last.After(lastInterrupt) {
			return
		}
		lastInterrupt = last
		status, _, _ := effective()
		if status == "thinking" || status == "executing" || status == "tool_failed" {
			st.ptyInterrupt(last)
		}
	}

	poll := func() {
		if watched == "" {
			tryBind()
			if watched == "" {
				checkRebind()
			}
			if watched == "" {
				return
			}
		} else {
			readWatched()
		}
		if watched == "" {
			return
		}
		checkPtyInterrupt()
		checkBackgroundAgents()
		checkRebind()
		publish()
	}

	poll()
	for {
		select {
		case <-ctx.Done():
			return
		case <-notifyCh:
			poll()
		case <-ticker.C:
			heartbeat()
			poll()
		}
	}
}

// isClaudeConversationLine reports whether a raw transcript line is a
// conversation entry (user/assistant/system) as opposed to bookkeeping.
func isClaudeConversationLine(line string) bool {
	var v struct {
		Type string `json:"type"`
	}
	if json.Unmarshal([]byte(line), &v) != nil {
		return false
	}
	return v.Type == "user" || v.Type == "assistant" || v.Type == "system"
}
