package agents

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/04mg/caw/internal/agent"
	_ "modernc.org/sqlite"
)

func TestHermesStatusForMessageUserIsThinking(t *testing.T) {
	status, tool := hermesStatusForMessage("user", "hey hermes", "", "")
	if status != "thinking" || tool != "" {
		t.Fatalf("user status = (%q, %q), want (thinking, \"\")", status, tool)
	}
}

func TestHermesStatusForMessageToolIsThinking(t *testing.T) {
	status, tool := hermesStatusForMessage("tool", `{"bytes_written": 1}`, "", "")
	if status != "thinking" || tool != "" {
		t.Fatalf("tool status = (%q, %q), want (thinking, \"\")", status, tool)
	}
}

func TestHermesStatusForMessageStopIsIdle(t *testing.T) {
	status, tool := hermesStatusForMessage("assistant", "Done!", "", "stop")
	if status != "idle" || tool != "" {
		t.Fatalf("stop status = (%q, %q), want (idle, \"\")", status, tool)
	}
}

func TestHermesStatusForMessageToolCallsWriteFileIsExecuting(t *testing.T) {
	toolCalls := `[{"function":{"name":"write_file","arguments":"{}"}}]`
	status, tool := hermesStatusForMessage("assistant", "", toolCalls, "tool_calls")
	if status != "executing" || tool != "write_file" {
		t.Fatalf("write_file status = (%q, %q), want (executing, write_file)", status, tool)
	}
}

func TestHermesStatusForMessageToolCallsClarifyIsWaitingInput(t *testing.T) {
	toolCalls := `[{"function":{"name":"clarify","arguments":"{}"}}]`
	status, tool := hermesStatusForMessage("assistant", "", toolCalls, "tool_calls")
	if status != "waiting_input" || tool != "clarify" {
		t.Fatalf("clarify status = (%q, %q), want (waiting_input, clarify)", status, tool)
	}
}

func TestHermesStatusForMessageInterruptedIsInterrupted(t *testing.T) {
	// Hermes writes an assistant message starting with "Operation
	// interrupted" (no finish_reason) when the user hits Ctrl+C mid-turn.
	// The watcher reports "interrupted" (not idle) so the UI shows a red dot.
	status, tool := hermesStatusForMessage("assistant", "Operation interrupted: waiting for model response (16.2s elapsed)", "", "")
	if status != "interrupted" || tool != "" {
		t.Fatalf("interrupted status = (%q, %q), want (interrupted, \"\")", status, tool)
	}
}

func TestHermesStatusForMessageAbortedFinishIsInterrupted(t *testing.T) {
	status, _ := hermesStatusForMessage("assistant", "", "", "aborted")
	if status != "interrupted" {
		t.Fatalf("aborted finish status = %q, want interrupted", status)
	}
}

func TestHermesStatusForMessageUnfinishedAssistantIsThinking(t *testing.T) {
	// Empty finish_reason with no interrupted marker: the row is still being
	// written, so the turn is in progress.
	status, tool := hermesStatusForMessage("assistant", "", "", "")
	if status != "thinking" || tool != "" {
		t.Fatalf("unfinished status = (%q, %q), want (thinking, \"\")", status, tool)
	}
}

func TestHermesStatusForMessageToolFailureIsToolFailed(t *testing.T) {
	// A tool-role message whose content JSON carries an "error" field means
	// the tool call failed (e.g. a Read on a missing file).
	content := `{"content":"","error":"File not found: /nonexistent/xyz.txt"}`
	status, tool := hermesStatusForMessage("tool", content, "", "")
	if status != "tool_failed" || tool != "" {
		t.Fatalf("tool failure status = (%q, %q), want (tool_failed, \"\")", status, tool)
	}
}

func TestHermesStatusForMessageToolSuccessIsThinking(t *testing.T) {
	// A tool-role message with no "error" field is a successful result — the
	// agent continues thinking.
	content := `{"content":"hello world","total_lines":1}`
	status, tool := hermesStatusForMessage("tool", content, "", "")
	if status != "thinking" || tool != "" {
		t.Fatalf("tool success status = (%q, %q), want (thinking, \"\")", status, tool)
	}
}

func TestHermesToolErrorText(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{``, ""},
		{`not json`, ""},
		{`{"content":"hi"}`, ""},
		{`{"error":"File not found: /x"}`, "File not found: /x"},
		{`{"error":42}`, "42"},
	}
	for _, c := range cases {
		got := hermesToolErrorText(c.in)
		if got != c.want {
			t.Fatalf("hermesToolErrorText(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestHermesStatusForMessageToolCallsEmptyFallsBackToThinking(t *testing.T) {
	// finish_reason=tool_calls but the tool_calls blob is empty/unparseable:
	// we can't name a tool, so treat as thinking (turn in progress).
	status, tool := hermesStatusForMessage("assistant", "", "", "tool_calls")
	if status != "thinking" || tool != "" {
		t.Fatalf("empty tool_calls status = (%q, %q), want (thinking, \"\")", status, tool)
	}
}

func TestLastHermesToolName(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{``, ""},
		{`not json`, ""},
		{`[]`, ""},
		{`[{"function":{"name":"read"}}]`, "read"},
		{`[{"function":{"name":"write_file"}},{"function":{"name":"clarify"}}]`, "clarify"},
	}
	for _, c := range cases {
		got := lastHermesToolName(c.in)
		if got != c.want {
			t.Fatalf("lastHermesToolName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestIsUserInputToolIncludesClarify(t *testing.T) {
	if !isUserInputTool("clarify") {
		t.Fatal("clarify should be treated as a user-input tool")
	}
	if isUserInputTool("write_file") {
		t.Fatal("write_file should not be a user-input tool")
	}
}

// ----- DB-backed session-finding tests --------------------------------------

func createHermesTestDB(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "state.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	schema := `
CREATE TABLE sessions (
    id TEXT PRIMARY KEY,
    source TEXT NOT NULL,
    started_at REAL NOT NULL,
    ended_at REAL,
    end_reason TEXT,
    title TEXT,
    cwd TEXT,
    parent_session_id TEXT
);
CREATE TABLE messages (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id TEXT NOT NULL,
    role TEXT NOT NULL,
    content TEXT,
    tool_calls TEXT,
    tool_name TEXT,
    finish_reason TEXT,
    timestamp REAL NOT NULL
);
`
	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	return dbPath
}

func insertHermesSession(t *testing.T, dbPath, id string, startedAt float64, endedAt interface{}, title string) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+dbPath+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	_, err = db.Exec(
		`INSERT INTO sessions (id, source, started_at, ended_at, title) VALUES (?, 'tui', ?, ?, ?)`,
		id, startedAt, endedAt, title,
	)
	if err != nil {
		t.Fatalf("insert session: %v", err)
	}
}

func insertHermesMessage(t *testing.T, dbPath, sid, role, content, toolCalls, finishReason string, ts float64) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+dbPath+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	_, err = db.Exec(
		`INSERT INTO messages (session_id, role, content, tool_calls, finish_reason, timestamp) VALUES (?, ?, ?, ?, ?, ?)`,
		sid, role, content, toolCalls, finishReason, ts,
	)
	if err != nil {
		t.Fatalf("insert message: %v", err)
	}
}

func TestHermesSessionTitle(t *testing.T) {
	dbPath := createHermesTestDB(t)
	now := float64(time.Now().Unix())
	insertHermesSession(t, dbPath, "t1", now, nil, "")
	insertHermesMessage(t, dbPath, "t1", "user", "list files please", "", "", now)
	if got := hermesSessionTitle(dbPath, "t1"); got != "list files please" {
		t.Fatalf("title fallback = %q, want first user prompt", got)
	}
	insertHermesSession(t, dbPath, "t2", now, nil, "Friendly Greeting")
	if got := hermesSessionTitle(dbPath, "t2"); got != "Friendly Greeting" {
		t.Fatalf("title = %q, want session title", got)
	}
	if got := hermesSessionTitle(dbPath, "nope"); got != "" {
		t.Fatalf("unknown session title = %q, want empty", got)
	}
}

func execHermesTestDB(t *testing.T, dbPath, q string, args ...any) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+dbPath+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(q, args...); err != nil {
		t.Fatalf("exec %q: %v", q, err)
	}
}

func insertHermesToolResult(t *testing.T, dbPath, sid, toolName, content string, ts float64) {
	t.Helper()
	execHermesTestDB(t, dbPath,
		`INSERT INTO messages (session_id, role, content, tool_calls, tool_name, finish_reason, timestamp) VALUES (?, 'tool', ?, '', ?, '', ?)`,
		sid, content, toolName, ts)
}

// Real rows captured from Hermes 0.19.0 (state.db) driving the CLI through
// the CAWTEST proxy, ids/timestamps scrubbed.
const (
	hermesFixtureTerminalCall = `[{"id": "call_2d09", "call_id": "call_2d09", "response_item_id": "fc_2d09", "type": "function", "function": {"name": "terminal", "arguments": "{\"command\":\"sleep 30\"}"}}]`
	hermesFixtureClarifyCall  = `[{"id": "call_68c6", "call_id": "call_68c6", "response_item_id": "fc_68c6", "type": "function", "function": {"name": "clarify", "arguments": "{\"question\":\"CAWTEST: which option do you choose?\",\"choices\":[\"Option 1\",\"Option 2\"]}"}}]`
	hermesFixtureReadMissing  = `{"content": "", "total_lines": 0, "file_size": 0, "truncated": false, "is_binary": false, "is_image": false, "error": "File not found: /nonexistent/xyz.txt"}`
	hermesFixtureTerminalOK   = `{"output": "", "exit_code": 0, "error": null}`
	hermesFixtureTerminalINT  = `{"output": "[Command interrupted]", "exit_code": 130, "error": null}`
	hermesFixtureClarifyDone  = `{"question": "CAWTEST: which option do you choose?", "choices_offered": ["Option 1", "Option 2"], "user_response": "Option 1"}`
)

func TestHermesStatusForRowRealRows(t *testing.T) {
	cases := []struct {
		name                string
		row                 hermesRow
		status, tool, detls string
	}{
		{"prompt", hermesRow{Role: "user", Content: "CAWTEST:shell:sleep 20"}, "thinking", "", ""},
		{"terminal call", hermesRow{Role: "assistant", ToolCalls: hermesFixtureTerminalCall, FinishReason: "tool_calls"}, "executing", "terminal", ""},
		{"clarify call", hermesRow{Role: "assistant", ToolCalls: hermesFixtureClarifyCall, FinishReason: "tool_calls"}, "waiting_input", "clarify", ""},
		{"clarify answered", hermesRow{Role: "tool", ToolName: "clarify", Content: hermesFixtureClarifyDone}, "thinking", "", ""},
		{"terminal ok (error: null)", hermesRow{Role: "tool", ToolName: "terminal", Content: hermesFixtureTerminalOK}, "thinking", "", ""},
		{"terminal non-zero exit", hermesRow{Role: "tool", ToolName: "terminal", Content: hermesFixtureTerminalINT}, "tool_failed", "terminal", "exit code 130"},
		{"read missing file", hermesRow{Role: "tool", ToolName: "read_file", Content: hermesFixtureReadMissing}, "tool_failed", "read_file", "File not found: /nonexistent/xyz.txt"},
		{"final answer", hermesRow{Role: "assistant", Content: "done", FinishReason: "stop"}, "idle", "", "done"},
		{"ctrl+c", hermesRow{Role: "assistant", Content: "Operation interrupted."}, "interrupted", "", ""},
	}
	for _, c := range cases {
		status, tool, details := hermesStatusForRow(c.row)
		if status != c.status || tool != c.tool || details != c.detls {
			t.Errorf("%s: got (%q, %q, %q), want (%q, %q, %q)", c.name, status, tool, details, c.status, c.tool, c.detls)
		}
	}
}

func TestHermesToolFailureTerminalErrorText(t *testing.T) {
	got, failed := hermesToolFailure("terminal", `{"output": "", "exit_code": 1, "error": "command not found"}`)
	if !failed || got != "command not found" {
		t.Fatalf("got (%q, %v), want (command not found, true)", got, failed)
	}
	// A terminal result with an "error" but exit code 0 is not a failure
	// (mirrors Hermes' _detect_tool_failure).
	if _, failed := hermesToolFailure("terminal", `{"output": "x", "exit_code": 0, "error": "warning"}`); failed {
		t.Fatal("exit_code 0 must not be a failure")
	}
}

// Real agent.log lines (Hermes 0.19.0), session ids scrubbed.
const hermesFixtureLog = `2026-11-01 18:51:21,216 INFO [20261101_184802_aaaaaa] agent.turn_context: conversation turn: session=20261101_184802_aaaaaa model=claude-haiku-5-5 provider=custom platform=cli history=20 msg='CAWTEST:fail:400'
2026-11-01 18:51:21,377 WARNING [20261101_184802_aaaaaa] agent.conversation_loop: API call failed (attempt 1/3) error_type=BadRequestError thread=Thread-28 (run_agent):140565525022400 provider=custom base_url=http://127.0.0.1:8199/v1 model=claude-haiku-5-5 summary=HTTP 400: CAWTEST scripted failure
2026-11-01 18:51:21,543 ERROR [20261101_184802_aaaaaa] agent.conversation_loop: Non-retryable client error: Error code: 400 - {'error': {'message': 'CAWTEST scripted failure', 'type': 'invalid_request_error', 'code': '400'}}
2026-11-01 18:52:00,000 WARNING [20261101_184802_bbbbbb] agent.tool_executor: Tool terminal returned error (5.88s): {"output": "[Command interrupted]", "exit_code": 130, "error": null}
`

func TestHermesLogTailParsesTurnErrors(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "agent.log")
	if err := os.WriteFile(logPath, []byte("2026-11-01 18:00:00,000 ERROR [old] agent.conversation_loop: Non-retryable client error: stale\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tail := newHermesLogTail(logPath)
	tail.poll()
	if _, ok := tail.lastError("old"); ok {
		t.Fatal("history written before the watcher started must be skipped")
	}

	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	// Write the fixture in two pieces split mid-line to cover partial reads.
	half := len(hermesFixtureLog) / 2
	f.WriteString(hermesFixtureLog[:half])
	tail.poll()
	f.WriteString(hermesFixtureLog[half:])
	f.Close()
	tail.poll()

	ev, ok := tail.lastError("20261101_184802_aaaaaa")
	if !ok {
		t.Fatal("expected a turn error for session aaaaaa")
	}
	want := time.Date(2026, 11, 1, 18, 51, 21, 543e6, time.Local)
	if diff := ev.at - float64(want.UnixNano())/1e9; diff > 0.001 || diff < -0.001 {
		t.Fatalf("error time = %v, want %v", ev.at, float64(want.UnixNano())/1e9)
	}
	if !strings.HasPrefix(ev.msg, "Non-retryable client error: Error code: 400") {
		t.Fatalf("error msg = %q", ev.msg)
	}
	if _, ok := tail.lastError("20261101_184802_bbbbbb"); ok {
		t.Fatal("a WARNING from the tool executor is not a turn-ending error")
	}

	// Rotation: a fresh, shorter file is read from the start.
	if err := os.WriteFile(logPath, []byte("2026-11-01 19:00:00,000 ERROR [cccc] agent.conversation_loop: API call failed after 3 retries. HTTP 500\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tail.poll()
	if _, ok := tail.lastError("cccc"); !ok {
		t.Fatal("expected the rotated log to be re-read from the start")
	}
}

// newTestHermesState builds a watcher state over a test DB without goroutines.
func newTestHermesState(t *testing.T, dbPath, leaf, cwd string, resume bool, gate *bool) *hermesWatchState {
	t.Helper()
	db, err := openHermesDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	var ctrlC time.Time
	st := &hermesWatchState{
		db:        db,
		leaf:      leaf,
		cwd:       cwd,
		resume:    resume,
		started:   time.Now().Add(-10 * time.Second),
		gate:      func() bool { return *gate },
		lastCtrlC: func() time.Time { return ctrlC },
		logTail:   newHermesLogTail(filepath.Join(filepath.Dir(dbPath), "logs", "agent.log")),
	}
	t.Cleanup(func() {
		if st.sid != "" {
			UnclaimSession(hermesAgentID, hermesClaimCwd, st.sid)
		}
	})
	return st
}

type hermesTestSink struct{ statuses, tools, details []string }

func (s *hermesTestSink) cb(status, tool, details, _ string) {
	s.statuses = append(s.statuses, status)
	s.tools = append(s.tools, tool)
	s.details = append(s.details, details)
}

func (s *hermesTestSink) last() string {
	if len(s.statuses) == 0 {
		return ""
	}
	return s.statuses[len(s.statuses)-1]
}

func nowSec() float64 { return float64(time.Now().UnixNano()) / 1e9 }

// Two fresh Hermes panes in the same cwd: the session row appears when the
// user submits a prompt in pane B. Pane A (launched first, idle, silent PTY)
// must not grab it; pane B (whose PTY just echoed the prompt) must.
func TestHermesFreshBindIsGatedOnPaneActivity(t *testing.T) {
	dbPath := createHermesTestDB(t)
	gateA, gateB := false, false
	a := newTestHermesState(t, dbPath, "leaf-a", "/w", false, &gateA)
	b := newTestHermesState(t, dbPath, "leaf-b", "/w", false, &gateB)
	var sa, sb hermesTestSink

	now := nowSec()
	insertHermesSession(t, dbPath, "sess-b", now, nil, "")
	insertHermesMessage(t, dbPath, "sess-b", "user", "CAWTEST:shell:sleep 10", "", "", now)

	a.step(sa.cb)
	if a.sid != "" {
		t.Fatalf("idle pane A bound %q", a.sid)
	}
	gateB = true
	b.step(sb.cb)
	if b.sid != "sess-b" || sb.last() != "thinking" {
		t.Fatalf("pane B: sid=%q status=%q, want sess-b/thinking", b.sid, sb.last())
	}
	gateA = true
	a.step(sa.cb)
	if a.sid != "" || len(sa.statuses) != 0 {
		t.Fatalf("pane A stole the claimed session: sid=%q statuses=%v", a.sid, sa.statuses)
	}
}

func TestHermesFreshBindSkipsOldAndForeignSessions(t *testing.T) {
	dbPath := createHermesTestDB(t)
	gate := true
	st := newTestHermesState(t, dbPath, "leaf-f", "/w", false, &gate)
	now := nowSec()
	// A live session left behind by an earlier run (never ended).
	insertHermesSession(t, dbPath, "stale", now-3600, nil, "")
	// A new session in another directory.
	insertHermesSession(t, dbPath, "elsewhere", now, nil, "")
	execHermesTestDB(t, dbPath, `UPDATE sessions SET cwd = '/other' WHERE id = 'elsewhere'`)
	// A delegate_task child of some other session.
	insertHermesSession(t, dbPath, "child", now, nil, "")
	execHermesTestDB(t, dbPath, `UPDATE sessions SET parent_session_id = 'stale' WHERE id = 'child'`)
	var sink hermesTestSink
	st.step(sink.cb)
	if st.sid != "" {
		t.Fatalf("bound %q, want nothing", st.sid)
	}
	insertHermesSession(t, dbPath, "mine", now+1, nil, "")
	execHermesTestDB(t, dbPath, `UPDATE sessions SET cwd = '/w' WHERE id = 'mine'`)
	st.step(sink.cb)
	if st.sid != "mine" {
		t.Fatalf("bound %q, want mine", st.sid)
	}
}

// A reopened pane runs "hermes --resume <id>". At watcher start the row still
// carries ended_at from the previous run (Hermes ends sessions on exit) and
// other live sessions exist; the watcher must bind the exact persisted id,
// not fall back to a scan (which swapped two reopened panes' sessions).
func TestHermesResumeBindsPersistedSessionEvenWhileEnded(t *testing.T) {
	dbPath := createHermesTestDB(t)
	now := nowSec()
	insertHermesSession(t, dbPath, "other-live", now-100, nil, "")
	insertHermesMessage(t, dbPath, "other-live", "user", "x", "", "", now-1)
	insertHermesSession(t, dbPath, "mine", now-200, now-50, "")
	insertHermesMessage(t, dbPath, "mine", "assistant", "done", "", "stop", now-60)
	gate := true
	st := newTestHermesState(t, dbPath, "leaf-r", "/w", true, &gate)
	st.persisted = "mine"
	var sink hermesTestSink
	st.step(sink.cb)
	if st.sid != "mine" || sink.last() != "idle" {
		t.Fatalf("sid=%q status=%q, want mine/idle", st.sid, sink.last())
	}
	// The old ended_at predates the binding: no successor hunt.
	st.step(sink.cb)
	if st.sid != "mine" {
		t.Fatalf("rebound to %q", st.sid)
	}
}

// /new: Hermes ends the bound session ("new_session") and creates the next
// row in the same instant (without a cwd). The pane follows it even when it
// is not focused, and a sibling pane does not.
func TestHermesFollowsNewSession(t *testing.T) {
	dbPath := createHermesTestDB(t)
	gate, sibGate := true, true
	st := newTestHermesState(t, dbPath, "leaf-n", "/w", false, &gate)
	sib := newTestHermesState(t, dbPath, "leaf-sib", "/w", false, &sibGate)
	now := nowSec()
	insertHermesSession(t, dbPath, "s1", now, nil, "")
	insertHermesMessage(t, dbPath, "s1", "assistant", "done", "", "stop", now)
	insertHermesSession(t, dbPath, "sib", now, nil, "")
	insertHermesMessage(t, dbPath, "sib", "assistant", "done", "", "stop", now)
	var sink, sibSink hermesTestSink
	if !ClaimSessionForLeaf(hermesAgentID, hermesClaimCwd, "s1", "leaf-n") || !ClaimSessionForLeaf(hermesAgentID, hermesClaimCwd, "sib", "leaf-sib") {
		t.Fatal("claim")
	}
	st.bind("s1")
	sib.bind("sib")
	st.step(sink.cb)
	sib.step(sibSink.cb)

	gate = false // /new does not need focus or PTY output
	ended := nowSec() + 0.01
	execHermesTestDB(t, dbPath, `UPDATE sessions SET ended_at = ?, end_reason = 'new_session' WHERE id = 's1'`, ended)
	insertHermesSession(t, dbPath, "s2", ended+0.003, nil, "")
	sib.step(sibSink.cb)
	if sib.sid != "sib" {
		t.Fatalf("sibling switched to %q", sib.sid)
	}
	st.step(sink.cb)
	if st.sid != "s2" || sink.last() != "idle" {
		t.Fatalf("sid=%q status=%q, want s2/idle", st.sid, sink.last())
	}
	insertHermesMessage(t, dbPath, "s2", "user", "CAWTEST:shell:sleep 8", "", "", ended+5)
	st.step(sink.cb)
	if sink.last() != "thinking" {
		t.Fatalf("status after prompt in new session = %q", sink.last())
	}
}

// Context compression and /branch end the bound session and create a child
// row linked through parent_session_id.
func TestHermesFollowsChildSession(t *testing.T) {
	dbPath := createHermesTestDB(t)
	gate := false
	st := newTestHermesState(t, dbPath, "leaf-c", "/w", false, &gate)
	now := nowSec()
	insertHermesSession(t, dbPath, "p", now, nil, "")
	if !ClaimSessionForLeaf(hermesAgentID, hermesClaimCwd, "p", "leaf-c") {
		t.Fatal("claim")
	}
	st.bind("p")
	var sink hermesTestSink
	st.step(sink.cb)
	ended := nowSec() + 0.01
	execHermesTestDB(t, dbPath, `UPDATE sessions SET ended_at = ?, end_reason = 'compression' WHERE id = 'p'`, ended)
	insertHermesSession(t, dbPath, "c", ended+30, nil, "")
	execHermesTestDB(t, dbPath, `UPDATE sessions SET parent_session_id = 'p' WHERE id = 'c'`)
	st.step(sink.cb)
	if st.sid != "c" {
		t.Fatalf("sid=%q, want c", st.sid)
	}
}

// /resume <older session>: the bound session ends with "resumed_other" and an
// older row is reopened. The pane follows the row that receives the next
// message, but only while it is the active pane.
func TestHermesFollowsResumedSessionOnlyFromActivePane(t *testing.T) {
	dbPath := createHermesTestDB(t)
	gate := false
	st := newTestHermesState(t, dbPath, "leaf-o", "/w", false, &gate)
	now := nowSec()
	insertHermesSession(t, dbPath, "old", now-3600, nil, "")
	insertHermesSession(t, dbPath, "cur", now, nil, "")
	if !ClaimSessionForLeaf(hermesAgentID, hermesClaimCwd, "cur", "leaf-o") {
		t.Fatal("claim")
	}
	st.bind("cur")
	var sink hermesTestSink
	st.step(sink.cb)
	ended := nowSec() + 0.01
	execHermesTestDB(t, dbPath, `UPDATE sessions SET ended_at = ?, end_reason = 'resumed_other' WHERE id = 'cur'`, ended)
	insertHermesMessage(t, dbPath, "old", "user", "continue please", "", "", ended+3)
	st.step(sink.cb)
	if st.sid != "cur" {
		t.Fatalf("inactive pane switched to %q", st.sid)
	}
	gate = true
	st.step(sink.cb)
	if st.sid != "old" || sink.last() != "thinking" {
		t.Fatalf("sid=%q status=%q, want old/thinking", st.sid, sink.last())
	}
}

// Regression: the old silence-based rebind let an idle, focused pane switch
// to a newer sibling session that was busy. A live bound session is never
// abandoned.
func TestHermesDoesNotStealBusySibling(t *testing.T) {
	dbPath := createHermesTestDB(t)
	gate := true
	st := newTestHermesState(t, dbPath, "leaf-i", "/w", false, &gate)
	now := nowSec()
	insertHermesSession(t, dbPath, "mine", now, nil, "")
	insertHermesMessage(t, dbPath, "mine", "assistant", "done", "", "stop", now)
	if !ClaimSessionForLeaf(hermesAgentID, hermesClaimCwd, "mine", "leaf-i") {
		t.Fatal("claim")
	}
	st.bind("mine")
	var sink hermesTestSink
	st.step(sink.cb)
	insertHermesSession(t, dbPath, "newer", now+5, nil, "")
	insertHermesMessage(t, dbPath, "newer", "user", "busy", "", "", now+6)
	for i := 0; i < 5; i++ {
		st.step(sink.cb)
	}
	if st.sid != "mine" || sink.last() != "idle" {
		t.Fatalf("sid=%q status=%q, want mine/idle", st.sid, sink.last())
	}
}

// CAWTEST:fail:400 — Hermes aborts the turn and persists nothing after the
// prompt; only agent.log records the failure. The card must leave "thinking"
// (and a later prompt must bring it back).
func TestHermesAPIErrorEndsTurn(t *testing.T) {
	dbPath := createHermesTestDB(t)
	logDir := filepath.Join(filepath.Dir(dbPath), "logs")
	os.MkdirAll(logDir, 0o755)
	logPath := filepath.Join(logDir, "agent.log")
	os.WriteFile(logPath, nil, 0o644)
	gate := true
	st := newTestHermesState(t, dbPath, "leaf-e", "/w", false, &gate)
	now := nowSec()
	insertHermesSession(t, dbPath, "e1", now, nil, "")
	insertHermesMessage(t, dbPath, "e1", "user", "CAWTEST:fail:400", "", "", now)
	var sink hermesTestSink
	st.step(sink.cb)
	if sink.last() != "thinking" {
		t.Fatalf("status = %q, want thinking", sink.last())
	}
	line := time.Now().Add(300*time.Millisecond).Format("2006-01-02 15:04:05,000") +
		" ERROR [e1] agent.conversation_loop: Non-retryable client error: Error code: 400 - {'error': {'message': 'CAWTEST scripted failure'}}\n"
	f, _ := os.OpenFile(logPath, os.O_APPEND|os.O_WRONLY, 0o644)
	f.WriteString(line)
	f.Close()
	st.step(sink.cb)
	if sink.last() != "idle" || !strings.HasPrefix(sink.details[len(sink.details)-1], "API error: Non-retryable client error") {
		t.Fatalf("status=%q details=%q, want idle + API error", sink.last(), sink.details[len(sink.details)-1])
	}
	insertHermesMessage(t, dbPath, "e1", "user", "try again", "", "", nowSec()+2)
	st.step(sink.cb)
	if sink.last() != "thinking" {
		t.Fatalf("status after new prompt = %q, want thinking", sink.last())
	}
}

// Ctrl+C mid-turn flips the card to interrupted at once and keeps it there
// (Hermes may persist nothing for the aborted turn, and writes to sibling
// sessions must not clear it) until a NEW prompt arrives. ESC is not an
// interrupt key in Hermes and must not count.
func TestHermesCtrlCInterruptIsStickyAndESCIgnored(t *testing.T) {
	dbPath := createHermesTestDB(t)
	gate := true
	st := newTestHermesState(t, dbPath, "leaf-int", "/w", false, &gate)
	var ctrlC time.Time
	st.lastCtrlC = func() time.Time { return ctrlC }
	now := nowSec()
	insertHermesSession(t, dbPath, "s-int", now, nil, "")
	insertHermesMessage(t, dbPath, "s-int", "user", "write a long poem", "", "", now)
	var sink hermesTestSink
	st.step(sink.cb)
	if sink.last() != "thinking" {
		t.Fatalf("status = %q, want thinking", sink.last())
	}
	// ESC (recorded by the shared PTY hook as a generic interrupt) is ignored.
	agent.SetPtyInterruptForTest("leaf-int", time.Now())
	defer agent.SetPtyInterruptForTest("leaf-int", time.Time{})
	st.step(sink.cb)
	if sink.last() != "thinking" {
		t.Fatalf("ESC changed status to %q", sink.last())
	}

	ctrlC = time.Now()
	st.step(sink.cb)
	if sink.last() != "interrupted" {
		t.Fatalf("status after Ctrl+C = %q, want interrupted", sink.last())
	}
	insertHermesSession(t, dbPath, "s-sib", now-3600, nil, "")
	insertHermesMessage(t, dbPath, "s-sib", "user", "sibling write", "", "", now+0.5)
	st.step(sink.cb)
	if sink.last() != "interrupted" {
		t.Fatalf("status flipped to %q after a sibling write", sink.last())
	}
	insertHermesMessage(t, dbPath, "s-int", "user", "new prompt", "", "", now+2)
	st.step(sink.cb)
	if sink.last() != "thinking" {
		t.Fatalf("status after new prompt = %q, want thinking", sink.last())
	}
}

// End-to-end through Watch: HOME-based paths, fsnotify on the WAL and the
// Ctrl+C hook wired to agent.LastPtyCtrlC.
func TestHermesWatchEndToEnd(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HERMES_HOME", "")
	hermesDir := filepath.Join(home, ".hermes")
	if err := os.MkdirAll(hermesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	src := createHermesTestDB(t)
	dbPath := filepath.Join(hermesDir, "state.db")
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dbPath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	const leafID = "hermes-e2e-leaf"
	agent.SetPtyFocusForTest(leafID, true)
	defer agent.SetPtyFocusForTest(leafID, false)

	var mu sync.Mutex
	var statuses []string
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		(&HermesWatcher{}).Watch(ctx, leafID, "", false, func(status, _, _, _ string) {
			mu.Lock()
			statuses = append(statuses, status)
			mu.Unlock()
		}, func() {})
	}()
	waitFor := func(want string) {
		t.Helper()
		deadline := time.Now().Add(8 * time.Second)
		for time.Now().Before(deadline) {
			mu.Lock()
			last := ""
			if len(statuses) > 0 {
				last = statuses[len(statuses)-1]
			}
			mu.Unlock()
			if last == want {
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
		mu.Lock()
		defer mu.Unlock()
		t.Fatalf("timed out waiting for %q; statuses=%v", want, statuses)
	}

	now := nowSec()
	insertHermesSession(t, dbPath, "w1", now, nil, "")
	insertHermesMessage(t, dbPath, "w1", "user", "CAWTEST:shell:sleep 30", "", "", now)
	waitFor("thinking")
	insertHermesMessage(t, dbPath, "w1", "assistant", "", hermesFixtureTerminalCall, "tool_calls", now+1)
	waitFor("executing")
	agent.SetPtyCtrlCForTest(leafID, time.Now())
	defer agent.SetPtyCtrlCForTest(leafID, time.Time{})
	waitFor("interrupted")
	cancel()
	<-done
}

// A resumed session whose previous process was interrupted while waiting for
// the model ends in a bare user row. The new process is idle: the stale row
// must not read as "thinking", but the next prompt must.
func TestHermesResumeIgnoresUnfinishedTurnOfPreviousProcess(t *testing.T) {
	dbPath := createHermesTestDB(t)
	now := nowSec()
	insertHermesSession(t, dbPath, "r1", now-200, now-50, "")
	insertHermesMessage(t, dbPath, "r1", "user", "CAWTEST:slow:60", "", "", now-60)
	gate := false
	st := newTestHermesState(t, dbPath, "leaf-stale", "/w", true, &gate)
	st.persisted = "r1"
	var sink hermesTestSink
	st.step(sink.cb)
	if st.sid != "r1" || sink.last() != "idle" {
		t.Fatalf("sid=%q status=%q, want r1/idle", st.sid, sink.last())
	}
	insertHermesMessage(t, dbPath, "r1", "user", "next", "", "", nowSec())
	st.step(sink.cb)
	if sink.last() != "thinking" {
		t.Fatalf("status after new prompt = %q, want thinking", sink.last())
	}
}
