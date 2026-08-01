package agents

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
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

func TestParseHermesDBToolFailureReportsToolFailed(t *testing.T) {
	dbPath := createHermesTestDB(t)
	now := float64(time.Now().Unix())
	insertHermesSession(t, dbPath, "s4", now, nil, "probe")
	insertHermesMessage(t, dbPath, "s4", "user", "read /nonexistent/xyz.txt", "", "", now)
	insertHermesMessage(t, dbPath, "s4", "assistant", "", `[{"function":{"name":"read_file","arguments":"{}"}}]`, "tool_calls", now+1)
	// The tool result row carries tool_name (read_file) and an "error" field
	// in its content JSON. Insert it directly so tool_name is populated.
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	_, err = db.Exec(
		`INSERT INTO messages (session_id, role, content, tool_calls, tool_name, finish_reason, timestamp) VALUES (?, 'tool', ?, '', 'read_file', '', ?)`,
		"s4", `{"content":"","error":"File not found: /nonexistent/xyz.txt"}`, now+2,
	)
	db.Close()
	if err != nil {
		t.Fatalf("insert tool message: %v", err)
	}

	var status, tool, details string
	(&HermesWatcher{}).parseHermesDB(dbPath, "s4", func(s, tl, d, ti string) {
		status, tool, details = s, tl, d
	})
	if status != "tool_failed" {
		t.Fatalf("status = %q, want tool_failed", status)
	}
	if tool != "read_file" {
		t.Fatalf("tool = %q, want read_file", tool)
	}
	if details != "File not found: /nonexistent/xyz.txt" {
		t.Fatalf("details = %q, want error text", details)
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
    cwd TEXT
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
	db, err := sql.Open("sqlite", dbPath)
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
	db, err := sql.Open("sqlite", dbPath)
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

func TestFindUnclaimedHermesSessionSkipsEnded(t *testing.T) {
	dbPath := createHermesTestDB(t)
	now := float64(time.Now().Unix())
	insertHermesSession(t, dbPath, "ended", now-100, now-90, "old") // ended_at set
	insertHermesSession(t, dbPath, "live", now-5, nil, "")          // ended_at NULL

	watcherStart := time.Now().Add(-10 * time.Second)
	got := findUnclaimedHermesSession(dbPath, watcherStart, "hermes", false)
	if got != "live" {
		t.Fatalf("expected to claim live session, got %q", got)
	}
}

func TestFindUnclaimedHermesSessionRecencyGate(t *testing.T) {
	dbPath := createHermesTestDB(t)
	now := float64(time.Now().Unix())
	// A stale live session from long before the watcher started must NOT be
	// claimed on a fresh start.
	insertHermesSession(t, dbPath, "stale", now-3600, nil, "")

	watcherStart := time.Now().Add(-10 * time.Second)
	if got := findUnclaimedHermesSession(dbPath, watcherStart, "hermes", false); got != "" {
		t.Fatalf("fresh start should skip stale session, got %q", got)
	}

	// On resume the recency filter is skipped — the reattached session may
	// predate the watcher.
	if got := findUnclaimedHermesSession(dbPath, watcherStart, "hermes", true); got != "stale" {
		t.Fatalf("resume should claim stale session, got %q", got)
	}
}

func TestFindRebindHermesSessionPicksNewer(t *testing.T) {
	dbPath := createHermesTestDB(t)
	now := float64(time.Now().Unix())
	insertHermesSession(t, dbPath, "old-live", now-60, nil, "")
	insertHermesSession(t, dbPath, "new-live", now-1, nil, "")
	// Re-bind should find the newer live session, not the old one.
	if got := findRebindHermesSession(dbPath, "hermes", "old-live"); got != "new-live" {
		t.Fatalf("rebind should pick new-live, got %q", got)
	}
}

func TestParseHermesDBTitleFallback(t *testing.T) {
	dbPath := createHermesTestDB(t)
	now := float64(time.Now().Unix())
	insertHermesSession(t, dbPath, "s1", now, nil, "") // empty title
	insertHermesMessage(t, dbPath, "s1", "user", "list all files please", "", "", now)
	insertHermesMessage(t, dbPath, "s1", "assistant", "done", "", "stop", now+1)

	var status, tool, details, title string
	(&HermesWatcher{}).parseHermesDB(dbPath, "s1", func(s, tl, d, ti string) {
		status, tool, details, title = s, tl, d, ti
	})
	if status != "idle" {
		t.Fatalf("status = %q, want idle", status)
	}
	if title != "list all files please" {
		t.Fatalf("title fallback = %q, want first user prompt", title)
	}
	if tool != "" || details != "" {
		t.Fatalf("unexpected tool/details = (%q, %q)", tool, details)
	}
}

func TestParseHermesDBUsesSessionTitle(t *testing.T) {
	dbPath := createHermesTestDB(t)
	now := float64(time.Now().Unix())
	insertHermesSession(t, dbPath, "s2", now, nil, "Friendly Greeting")
	insertHermesMessage(t, dbPath, "s2", "user", "hey", "", "", now)
	insertHermesMessage(t, dbPath, "s2", "assistant", "", `[{"function":{"name":"clarify","arguments":"{}"}}]`, "tool_calls", now+1)

	var status, tool, _, title string
	(&HermesWatcher{}).parseHermesDB(dbPath, "s2", func(s, tl, d, ti string) {
		status, tool, _, title = s, tl, d, ti
	})
	if status != "waiting_input" || tool != "clarify" {
		t.Fatalf("status = (%q, %q), want (waiting_input, clarify)", status, tool)
	}
	if title != "Friendly Greeting" {
		t.Fatalf("title = %q, want session title", title)
	}
}

func TestHermesLastMessageTime(t *testing.T) {
	dbPath := createHermesTestDB(t)
	now := float64(time.Now().Unix())
	insertHermesSession(t, dbPath, "s3", now, nil, "")
	insertHermesMessage(t, dbPath, "s3", "user", "a", "", "", now)
	insertHermesMessage(t, dbPath, "s3", "assistant", "b", "", "stop", now+5)
	got := hermesLastMessageTime(dbPath, "s3")
	if got != now+5 {
		t.Fatalf("lastMessageTime = %v, want %v", got, now+5)
	}
	// Unknown session returns 0.
	if got := hermesLastMessageTime(dbPath, "nope"); got != 0 {
		t.Fatalf("unknown session lastMessageTime = %v, want 0", got)
	}
}

func TestHermesLatestRole(t *testing.T) {
	dbPath := createHermesTestDB(t)
	now := float64(time.Now().Unix())
	insertHermesSession(t, dbPath, "r1", now, nil, "")
	insertHermesMessage(t, dbPath, "r1", "user", "hi", "", "", now)
	insertHermesMessage(t, dbPath, "r1", "assistant", "hey", "", "stop", now+1)
	if got := hermesLatestRole(dbPath, "r1"); got != "assistant" {
		t.Fatalf("latest role = %q, want assistant", got)
	}
	// A fresh prompt lands as the latest user message.
	insertHermesMessage(t, dbPath, "r1", "user", "next", "", "", now+2)
	if got := hermesLatestRole(dbPath, "r1"); got != "user" {
		t.Fatalf("latest role after prompt = %q, want user", got)
	}
	// Unknown session / missing db returns "".
	if got := hermesLatestRole(dbPath, "nope"); got != "" {
		t.Fatalf("unknown session latest role = %q, want empty", got)
	}
}

func TestHermesLastMessageID(t *testing.T) {
	dbPath := createHermesTestDB(t)
	now := float64(time.Now().Unix())
	insertHermesSession(t, dbPath, "i1", now, nil, "")
	insertHermesMessage(t, dbPath, "i1", "user", "hi", "", "", now)
	insertHermesMessage(t, dbPath, "i1", "assistant", "hey", "", "stop", now+1)
	if got := hermesLastMessageID(dbPath, "i1"); got != 2 {
		t.Fatalf("lastMessageID = %d, want 2", got)
	}
	insertHermesMessage(t, dbPath, "i1", "user", "next", "", "", now+2)
	if got := hermesLastMessageID(dbPath, "i1"); got != 3 {
		t.Fatalf("lastMessageID after prompt = %d, want 3", got)
	}
	if got := hermesLastMessageID(dbPath, "nope"); got != 0 {
		t.Fatalf("unknown session lastMessageID = %d, want 0", got)
	}
}

// TestHermesWatcherPTYInterruptStaysUntilNewPrompt reproduces the regression
// where a Hermes card flipped back to "working" right after showing
// "interrupted": Hermes writes no new DB row when a turn is aborted, so the
// latest row stays the user's pre-interrupt prompt (role=user). The watcher
// must keep the card in "interrupted" until a genuinely NEW user message
// (higher messages.id) arrives.
func TestHermesWatcherPTYInterruptStaysUntilNewPrompt(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	hermesDir := filepath.Join(home, ".hermes")
	if err := os.MkdirAll(hermesDir, 0o755); err != nil {
		t.Fatalf("mkdir hermes dir: %v", err)
	}
	dbPath := filepath.Join(hermesDir, "state.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if _, err := db.Exec(`
CREATE TABLE sessions (
    id TEXT PRIMARY KEY,
    source TEXT NOT NULL,
    started_at REAL NOT NULL,
    ended_at REAL,
    end_reason TEXT,
    title TEXT,
    cwd TEXT
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
`); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	db.Close()

	now := float64(time.Now().Unix())
	insertHermesSession(t, dbPath, "s-int", now, nil, "")
	// The pre-interrupt user prompt. On interrupt this stays the latest row.
	insertHermesMessage(t, dbPath, "s-int", "user", "write a long poem", "", "", now)

	const leafID = "hermes-int-pty"
	var mu sync.Mutex
	var statuses []string
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		(&HermesWatcher{}).Watch(ctx, leafID, "", false, func(status, _, _, _ string) {
			mu.Lock()
			statuses = append(statuses, status)
			mu.Unlock()
		}, func() {})
	}()

	lastStatus := func() string {
		mu.Lock()
		defer mu.Unlock()
		if len(statuses) == 0 {
			return ""
		}
		return statuses[len(statuses)-1]
	}

	waitFor := func(want string, timeout time.Duration) {
		t.Helper()
		deadline := time.Now().Add(timeout)
		for time.Now().Before(deadline) {
			if lastStatus() == want {
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Fatalf("timed out waiting for status %q; last=%q full=%v", want, lastStatus(), statuses)
	}

	// Session binds and reports thinking from the user prompt row. The prompt
	// row stays the LATEST row for the whole interrupted turn: Hermes writes
	// no new row on abort, so the transcript keeps showing a working state.
	waitFor("thinking", 8*time.Second)

	// User presses Ctrl+C in the PTY → card flips to interrupted.
	agent.SetPtyInterruptForTest(leafID, time.Now())
	waitFor("interrupted", 8*time.Second)

	// REGRESSION GUARD: Hermes stores all sessions in one shared state.db, so
	// a write to ANY session — here a sibling session landing while this
	// watcher is active — fires dbChanged and re-runs the interrupt-clear
	// check. The buggy check ("latest role is user") sees the bound session's
	// latest row is still the pre-interrupt user prompt, clears the sticky
	// interrupt, re-reads the prompt as "thinking", and flips the card back
	// to Working. The interrupt must instead survive until a NEW user
	// message (higher messages.id) lands.
	insertHermesSession(t, dbPath, "s-sibling", now-3600, nil, "")
	insertHermesMessage(t, dbPath, "s-sibling", "user", "sibling write", "", "", now+0.5)
	time.Sleep(6 * time.Second)
	if got := lastStatus(); got != "interrupted" {
		t.Fatalf("card flipped back to %q after interrupt; want it to stay interrupted (statuses: %v)", got, statuses)
	}

	// A NEW user prompt clears the sticky interrupt and resumes working.
	insertHermesMessage(t, dbPath, "s-int", "user", "new prompt", "", "", now+2)
	waitFor("thinking", 8*time.Second)

	cancel()
	<-done
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

func TestHermesDBMissingFile(t *testing.T) {
	// A non-existent DB should not panic; the watcher silently no-ops.
	missing := filepath.Join(t.TempDir(), "does-not-exist.db")
	if got := findUnclaimedHermesSession(missing, time.Now().Add(-10*time.Second), "hermes", false); got != "" {
		t.Fatalf("missing db should return empty, got %q", got)
	}
	if got := hermesLastMessageTime(missing, "x"); got != 0 {
		t.Fatalf("missing db lastMessageTime = %v, want 0", got)
	}
	if got := hermesLastMessageID(missing, "x"); got != 0 {
		t.Fatalf("missing db lastMessageID = %v, want 0", got)
	}
	if got := findRebindHermesSession(missing, "hermes", "x"); got != "" {
		t.Fatalf("missing db rebind = %q, want empty", got)
	}
	// parseHermesDB on a missing path calls back with idle.
	var called bool
	(&HermesWatcher{}).parseHermesDB(missing, "x", func(s, tl, d, ti string) {
		called = true
		if s != "idle" {
			t.Fatalf("missing db status = %q, want idle", s)
		}
	})
	if !called {
		t.Fatal("parseHermesDB did not invoke callback for missing db")
	}
}
