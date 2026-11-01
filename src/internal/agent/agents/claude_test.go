package agents

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/04mg/caw/internal/agent"
)

// writeClaudeTranscript writes the given JSONL lines to a temp file and returns
// its path. Used to exercise parseClaudeLog against realistic transcript
// fixtures without a running Claude process.
func writeClaudeTranscript(t *testing.T, lines []string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "transcript.jsonl")
	f, err := os.Create(p)
	if err != nil {
		t.Fatalf("create transcript: %v", err)
	}
	defer f.Close()
	for _, l := range lines {
		if _, err := f.WriteString(l + "\n"); err != nil {
			t.Fatalf("write transcript: %v", err)
		}
	}
	return p
}

// replayClaude feeds transcript lines through a fresh state machine.
func replayClaude(lines []string) *claudeTurnState {
	st := newClaudeTurnState()
	for _, l := range lines {
		st.apply(l)
	}
	return st
}

func TestClaudeInterruptReportsInterrupted(t *testing.T) {
	// Claude writes a user message containing "[request interrupted by user"
	// when the user cancels the in-flight tool call. The watcher must report
	// "interrupted" (not idle) so the UI shows a red dot.
	lines := []string{
		`{"type":"user","message":{"role":"user","content":"read /nonexistent/xyz.txt"}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"toolu_1","name":"Read"}]}}`,
		`{"type":"user","message":{"role":"user","content":"[request interrupted by user for a tool use]"}}`,
	}
	st := replayClaude(lines)
	status, tool := st.status, st.tool
	if status != "interrupted" {
		t.Fatalf("interrupt status = %q, want interrupted", status)
	}
	if tool != "Read" {
		t.Fatalf("interrupt tool = %q, want Read", tool)
	}
}

func TestClaudeToolFailureReportsToolFailed(t *testing.T) {
	// A tool_result block with is_error:true is a failed tool call. When it's
	// the last meaningful entry (no follow-up assistant tool_use or text), the
	// watcher reports tool_failed with the tool name and error text.
	lines := []string{
		`{"type":"user","message":{"role":"user","content":"read /nonexistent/xyz.txt"}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"toolu_1","name":"Read"}]}}`,
		`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","is_error":true,"content":"Error: File not found"}]}}`,
	}
	st := replayClaude(lines)
	status, tool, details := st.status, st.tool, st.details
	if status != "tool_failed" {
		t.Fatalf("tool failure status = %q, want tool_failed", status)
	}
	if tool != "Read" {
		t.Fatalf("tool = %q, want Read", tool)
	}
	if details == "" {
		t.Fatal("details should carry the error text")
	}
}

func TestClaudeToolFailureThenRecoveryReportsExecuting(t *testing.T) {
	// After a failed tool_result, the assistant issues a new tool_use — the
	// agent recovered, so it's executing again (not stuck in tool_failed).
	lines := []string{
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"toolu_1","name":"Read"}]}}`,
		`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","is_error":true,"content":"Error: File not found"}]}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"toolu_2","name":"Bash"}]}}`,
	}
	st := replayClaude(lines)
	status, tool := st.status, st.tool
	if status != "executing" || tool != "Bash" {
		t.Fatalf("recovered status = (%q, %q), want (executing, Bash)", status, tool)
	}
}

// TestClaudeWatcherPTYInterruptDetectedWithoutTranscriptMarker verifies that
// a Ctrl+C into the Claude PTY flips the card to "interrupted" immediately,
// even though Claude writes the "[request interrupted by user" transcript
// marker only later (sometimes a full turn late). The interrupt stays sticky
// while the transcript still shows the pre-interrupt working state, and only
// clears once a genuinely new user prompt is written.
func TestClaudeWatcherPTYInterruptDetectedWithoutTranscriptMarker(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cwd := filepath.Join(home, "proj")
	projectsDir := filepath.Join(home, ".claude", "projects", encodePathForDir(cwd))
	if err := os.MkdirAll(projectsDir, 0o755); err != nil {
		t.Fatalf("mkdir projects dir: %v", err)
	}
	transcript := filepath.Join(projectsDir, "session.jsonl")

	appendLine := func(line string) {
		t.Helper()
		f, err := os.OpenFile(transcript, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			t.Fatalf("open transcript: %v", err)
		}
		defer f.Close()
		if _, err := f.WriteString(line + "\n"); err != nil {
			t.Fatalf("append transcript: %v", err)
		}
	}

	const leafID = "claude-int-pty"
	var mu sync.Mutex
	var statuses []string
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		(&ClaudeWatcher{}).Watch(ctx, leafID, cwd, false, func(status, _, _, _ string) {
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

	// The transcript already exists with a prompt + an assistant tool_use, so
	// the watcher binds and reports executing.
	appendLine(`{"type":"user","message":{"role":"user","content":"read x"}}`)
	appendLine(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"toolu_1","name":"Read"}]}}`)
	waitFor("executing", 8*time.Second)

	// User presses Ctrl+C → card flips to interrupted immediately, with NO
	// transcript marker written yet.
	agent.SetPtyInterruptForTest(leafID, time.Now())
	waitFor("interrupted", 8*time.Second)

	// REGRESSION GUARD: more working transcript data (still no interrupt
	// marker) must not flip the card back to Working. The sticky interrupt
	// only clears on a genuinely new user prompt.
	appendLine(`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1"}]}}`)
	appendLine(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"toolu_2","name":"Bash"}]}}`)
	time.Sleep(6 * time.Second)
	if got := lastStatus(); got != "interrupted" {
		t.Fatalf("card flipped back to %q after interrupt; want it to stay interrupted (statuses: %v)", got, statuses)
	}

	// A genuinely new user prompt clears the sticky interrupt.
	appendLine(`{"type":"user","message":{"role":"user","content":"next step"}}`)
	waitFor("thinking", 8*time.Second)

	cancel()
	<-done
}

// loadClaudeFixture returns the lines of a scrubbed real Claude Code 2.1
// transcript from testdata/claude (one JSON object per line, original line
// order and indices preserved).
func loadClaudeFixture(t *testing.T, name string) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "claude", name))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var lines []string
	for _, l := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(l) != "" {
			lines = append(lines, l)
		}
	}
	return lines
}

type claudeCheckpoint struct {
	through int // replay lines[0..through] inclusive
	status  string
	tool    string
	detail  string // substring expected in details ("" = don't care)
}

func runClaudeCheckpoints(t *testing.T, lines []string, cps []claudeCheckpoint) *claudeTurnState {
	t.Helper()
	st := newClaudeTurnState()
	next := 0
	for _, cp := range cps {
		for ; next <= cp.through; next++ {
			st.apply(lines[next])
		}
		if st.status != cp.status || st.tool != cp.tool {
			t.Errorf("after line %d: got (%q, %q, %q), want (%q, %q)", cp.through, st.status, st.tool, st.details, cp.status, cp.tool)
		}
		if cp.detail != "" && !strings.Contains(st.details, cp.detail) {
			t.Errorf("after line %d: details %q do not contain %q", cp.through, st.details, cp.detail)
		}
	}
	for ; next < len(lines); next++ {
		st.apply(lines[next])
	}
	return st
}

// TestClaudeRealTranscriptMain replays a real session covering a tool turn,
// an ESC interrupt, a failed tool, AskUserQuestion, a background subagent,
// a background Bash command and /compact. Claude 2.1 interleaves
// api-request blobs, attachments, ai-title, queue-operation and similar
// bookkeeping lines that must never move the status.
func TestClaudeRealTranscriptMain(t *testing.T) {
	lines := loadClaudeFixture(t, "session_main.jsonl")
	st := runClaudeCheckpoints(t, lines, []claudeCheckpoint{
		{through: 4, status: "thinking"},
		{through: 24, status: "thinking"}, // api-request blobs, attachments
		{through: 25, status: "executing", tool: "Bash"},
		{through: 28, status: "executing", tool: "Bash"},
		{through: 29, status: "thinking"},
		{through: 37, status: "idle", detail: "finished."},
		{through: 38, status: "idle", detail: "finished."},
		{through: 45, status: "executing", tool: "Bash"},
		{through: 46, status: "interrupted", tool: "Bash"},
		{through: 48, status: "interrupted", tool: "Bash"},
		{through: 54, status: "executing", tool: "Read"},
		{through: 58, status: "tool_failed", tool: "Read", detail: "File does not exist"},
		{through: 61, status: "idle", detail: "read failed."},
		{through: 67, status: "waiting_input", tool: "AskUserQuestion"},
		{through: 68, status: "thinking"},
		{through: 73, status: "idle"},
		{through: 89, status: "executing", tool: "Agent"},
		{through: 90, status: "thinking"},
		{through: 92, status: "executing", tool: "Agent", detail: "Waiting for 1 background agent"},
		// The main turn ended but the background subagent is still running.
		{through: 93, status: "executing", tool: "Agent", detail: "Waiting for 1 background agent"},
		{through: 94, status: "idle"}, // its <task-notification> arrived
		{through: 98, status: "thinking"},
		{through: 100, status: "idle"},
		{through: 107, status: "thinking"}, // background Bash is not waited for
		{through: 112, status: "idle", detail: "started"},
		{through: 117, status: "thinking"},
		{through: 124, status: "idle"},
		{through: 126, status: "thinking"}, // /compact runs
		{through: 133, status: "thinking"},
		{through: 136, status: "idle"},
	})
	if st.status != "idle" {
		t.Fatalf("final status = %q, want idle", st.status)
	}
	if !strings.HasPrefix(st.title, "Run ./build.sh 10 with the Bash tool using run_in_background") {
		t.Fatalf("title = %q, want the last typed prompt", st.title)
	}
	if len(st.bgAgents) != 0 {
		t.Fatalf("background agents left over: %v", st.bgAgents)
	}
}

// TestClaudeRealTranscriptPermissionsAndPlan replays a manual-approval session:
// an approved Bash permission, a denied one (ESC), plan mode with
// ExitPlanMode approval and a non-zero Bash exit.
func TestClaudeRealTranscriptPermissionsAndPlan(t *testing.T) {
	lines := loadClaudeFixture(t, "session_permissions.jsonl")
	runClaudeCheckpoints(t, lines, []claudeCheckpoint{
		{through: 4, status: "thinking"},
		// Pending permission is not in the transcript; the watcher overlays
		// waiting_input from the PTY (see TestClaudeWatcherPermissionPrompt).
		{through: 25, status: "executing", tool: "Bash"},
		{through: 29, status: "thinking"},
		{through: 38, status: "idle"},
		{through: 45, status: "executing", tool: "Bash"},
		{through: 46, status: "interrupted", tool: "Bash"}, // "The user doesn't want to proceed"
		{through: 48, status: "interrupted", tool: "Bash"},
		{through: 53, status: "thinking"},
		{through: 76, status: "waiting_input", tool: "ExitPlanMode"},
		{through: 81, status: "thinking"},
		{through: 94, status: "tool_failed", tool: "Bash", detail: "Exit code 1"},
		{through: 102, status: "executing", tool: "Bash"},
		{through: 113, status: "idle"},
		{through: 120, status: "idle", detail: "a2 quick"},
	})
}

// TestClaudeRealTranscriptAfterClear replays the transcript Claude starts on
// /clear: the slash-command bookkeeping must leave the session idle (it used
// to read as "thinking" forever), a later /compact must end idle, and an ESC
// during a streamed answer must read as interrupted.
func TestClaudeRealTranscriptAfterClear(t *testing.T) {
	lines := loadClaudeFixture(t, "session_after_clear.jsonl")
	st := runClaudeCheckpoints(t, lines, []claudeCheckpoint{
		{through: 5, status: "idle"},
		{through: 6, status: "thinking"},
		{through: 28, status: "idle", detail: "after clear"},
		{through: 47, status: "executing", tool: "Bash"},
		{through: 54, status: "idle", detail: "long done."},
		{through: 80, status: "tool_failed", tool: "Read"},
		{through: 83, status: "idle"},
		{through: 85, status: "thinking"},
		{through: 95, status: "idle"},
		{through: 126, status: "thinking"},
		{through: 127, status: "interrupted"},
	})
	if st.title != "Write a 400-word essay about the history of tea. No tools." {
		t.Fatalf("title = %q", st.title)
	}
	// Five lines into the file (before the first prompt) the title is empty:
	// a /clear must not inherit the previous conversation's title.
	if fresh := replayClaude(lines[:6]); fresh.title != "" {
		t.Fatalf("title after /clear = %q, want empty", fresh.title)
	}
}

func TestClaudeCompactSummaryQuotingInterruptIsNotAnInterrupt(t *testing.T) {
	st := replayClaude([]string{
		`{"type":"user","message":{"role":"user","content":"/compact"}}`,
		`{"type":"system","subtype":"compact_boundary","content":"Conversation compacted"}`,
		`{"type":"user","isCompactSummary":true,"message":{"role":"user","content":"This session is being continued... the user wrote [Request interrupted by user for tool use] earlier"}}`,
	})
	if st.status != "thinking" {
		t.Fatalf("status = %q, want thinking (compaction still running)", st.status)
	}
}

func TestClaudeBookkeepingLinesDoNotChangeStatus(t *testing.T) {
	st := replayClaude([]string{
		`{"type":"user","origin":{"kind":"human"},"message":{"role":"user","content":"hi"}}`,
		`{"type":"assistant","message":{"role":"assistant","stop_reason":"end_turn","content":[{"type":"text","text":"hello"}]}}`,
		`{"type":"system","subtype":"turn_duration"}`,
		`{"type":"last-prompt","lastPrompt":"hi"}`,
		`{"type":"ai-title","aiTitle":"Greeting"}`,
		`{"type":"mode","mode":"normal"}`,
		`{"type":"cost-state"}`,
		`{"type":"api-request-blob","message":{"role":"user","content":[{"type":"text","text":"<system-reminder>x</system-reminder>"}]}}`,
		`{"type":"user","isMeta":true,"queueTranscriptOnly":true,"message":{"role":"user","content":"<system-reminder>\n<tools-used-before-summary>X</tools-used-before-summary>\n</system-reminder>"}}`,
		`{"type":"user","message":{"role":"user","content":"<command-name>/model</command-name>"}}`,
		`{"type":"system","subtype":"local_command","content":"<local-command-stdout>Set model</local-command-stdout>"}`,
	})
	if st.status != "idle" || st.title != "hi" || st.details != "hello" {
		t.Fatalf("got (%q, title %q, details %q), want (idle, hi, hello)", st.status, st.title, st.details)
	}
}

func TestClaudeSidechainLinesIgnored(t *testing.T) {
	st := replayClaude([]string{
		`{"type":"user","message":{"role":"user","content":"go"}}`,
		`{"type":"assistant","message":{"role":"assistant","stop_reason":"end_turn","content":[{"type":"text","text":"done"}]}}`,
		`{"type":"assistant","isSidechain":true,"message":{"role":"assistant","content":[{"type":"tool_use","id":"t9","name":"Bash"}]}}`,
	})
	if st.status != "idle" {
		t.Fatalf("status = %q, want idle", st.status)
	}
}

func TestClaudePTYInterruptUnsticksWhenWorkContinues(t *testing.T) {
	st := replayClaude([]string{
		`{"type":"user","message":{"role":"user","content":"go"}}`,
		`{"type":"assistant","timestamp":"2026-11-01T10:00:00.000Z","message":{"role":"assistant","content":[{"type":"tool_use","id":"t1","name":"Bash"}]}}`,
	})
	at := time.Date(2026, 11, 1, 10, 0, 1, 0, time.UTC)
	st.ptyInterrupt(at)
	// A late line from the cancelled turn keeps the interrupt.
	st.apply(`{"type":"user","timestamp":"2026-11-01T10:00:02.000Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"ok"}]}}`)
	if st.status != "interrupted" {
		t.Fatalf("status = %q, want interrupted", st.status)
	}
	// Clearly newer activity: the ESC did not interrupt anything.
	st.apply(`{"type":"assistant","timestamp":"2026-11-01T10:00:09.000Z","message":{"role":"assistant","content":[{"type":"tool_use","id":"t2","name":"Read"}]}}`)
	if st.status != "executing" || st.tool != "Read" {
		t.Fatalf("got (%q, %q), want (executing, Read)", st.status, st.tool)
	}
}

func TestClaudeSettleCatchUp(t *testing.T) {
	st := replayClaude([]string{
		`{"type":"user","message":{"role":"user","content":"go"}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"t1","name":"Agent"}]}}`,
		`{"type":"user","toolUseResult":{"status":"async_launched","agentId":"a1"},"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"launched"}]}}`,
		`{"type":"system","subtype":"turn_duration"}`,
	})
	if st.status != "executing" {
		t.Fatalf("status = %q, want executing while the background agent runs", st.status)
	}
	st.settleCatchUp()
	if st.status != "idle" || len(st.bgAgents) != 0 {
		t.Fatalf("after catch-up: %q bg=%v, want idle with no background agents", st.status, st.bgAgents)
	}
}

func TestClaudeEncodePathAndProjectDir(t *testing.T) {
	if got := claudeEncodePath("/root/.caw/worktrees/my_proj/a b"); got != "-root--caw-worktrees-my-proj-a-b" {
		t.Fatalf("claudeEncodePath = %q", got)
	}
	base := t.TempDir()
	cwd := "/home/u/.caw/worktrees/p1/abc"
	if got := claudeProjectDir(base, cwd); got != "" {
		t.Fatalf("missing project dir must not fall back to the projects root, got %q", got)
	}
	want := filepath.Join(base, claudeEncodePath(cwd))
	if err := os.MkdirAll(want, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := claudeProjectDir(base, cwd); got != want {
		t.Fatalf("claudeProjectDir = %q, want %q", got, want)
	}
}

func TestClaudeSessionIDFromCmd(t *testing.T) {
	id := "15bf2d57-9a0d-48de-9953-9f34861a5241"
	cases := []struct {
		cmd  []string
		want string
	}{
		{[]string{"claude", "--dangerously-skip-permissions", "--session-id", id}, id},
		{[]string{"claude", "--resume", id}, id},
		{[]string{"claude", "-r", id}, id},
		{[]string{"claude", "--session-id=" + id}, id},
		{[]string{"claude", "--resume", "some search"}, ""},
		{[]string{"claude", "--resume", id, "--fork-session"}, ""},
		{[]string{"claude", "--continue"}, ""},
		{nil, ""},
	}
	for _, c := range cases {
		if got := claudeSessionIDFromCmd(c.cmd); got != c.want {
			t.Errorf("claudeSessionIDFromCmd(%v) = %q, want %q", c.cmd, got, c.want)
		}
	}
}

func TestReadCompleteLinesKeepsPartialLine(t *testing.T) {
	p := filepath.Join(t.TempDir(), "t.jsonl")
	if err := os.WriteFile(p, []byte("{\"a\":1}\n{\"b\":"), 0o644); err != nil {
		t.Fatal(err)
	}
	lines, off, err := readCompleteLines(p, 0)
	if err != nil || len(lines) != 1 || off != 8 {
		t.Fatalf("got lines=%v off=%d err=%v", lines, off, err)
	}
	f, _ := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0o644)
	f.WriteString("2}\n")
	f.Close()
	lines, off, _ = readCompleteLines(p, off)
	if len(lines) != 1 || lines[0] != `{"b":2}` || off != 16 {
		t.Fatalf("second read: lines=%v off=%d", lines, off)
	}
}

// claudeWatchHarness runs a ClaudeWatcher for a leaf launched with a pinned
// --session-id in a temporary HOME and records every callback.
type claudeWatchHarness struct {
	t        *testing.T
	leaf     string
	cwd      string
	dir      string // the cwd's project dir
	mu       sync.Mutex
	statuses [][4]string
	cancel   context.CancelFunc
	done     chan struct{}
}

func startClaudeWatch(t *testing.T, leaf string, cmd []string) *claudeWatchHarness {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	cwd := filepath.Join(home, ".caw", "worktrees", "proj_1", "abc")
	dir := filepath.Join(home, ".claude", "projects", claudeEncodePath(cwd))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(agent.RegisterActiveSessionForTest(leaf, "claude", cwd, cmd))
	h := &claudeWatchHarness{t: t, leaf: leaf, cwd: cwd, dir: dir, done: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	h.cancel = cancel
	go func() {
		defer close(h.done)
		(&ClaudeWatcher{}).Watch(ctx, leaf, cwd, false, func(s, tl, d, ti string) {
			h.mu.Lock()
			h.statuses = append(h.statuses, [4]string{s, tl, d, ti})
			h.mu.Unlock()
		}, func() {})
	}()
	t.Cleanup(func() { cancel(); <-h.done })
	return h
}

func (h *claudeWatchHarness) appendTo(name string, lines ...string) {
	h.t.Helper()
	f, err := os.OpenFile(filepath.Join(h.dir, name), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		h.t.Fatal(err)
	}
	defer f.Close()
	for _, l := range lines {
		f.WriteString(l + "\n")
	}
}

func (h *claudeWatchHarness) last() [4]string {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.statuses) == 0 {
		return [4]string{}
	}
	return h.statuses[len(h.statuses)-1]
}

func (h *claudeWatchHarness) waitFor(status, tool string) {
	h.t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if l := h.last(); l[0] == status && l[1] == tool {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.t.Fatalf("timed out waiting for (%s, %s); got %v", status, tool, h.statuses)
}

func (h *claudeWatchHarness) seen(status string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, s := range h.statuses {
		if s[0] == status {
			return true
		}
	}
	return false
}

// TestClaudeWatcherBindsPinnedSessionOnly: a sibling Claude in the same cwd
// writing its own fresh transcript must not be adopted; the watcher waits for
// the transcript of the --session-id it was launched with.
func TestClaudeWatcherBindsPinnedSessionOnly(t *testing.T) {
	id := "11111111-2222-4333-8444-555555555555"
	h := startClaudeWatch(t, "leaf-pinned", []string{"claude", "--session-id", id})
	h.appendTo("99999999-2222-4333-8444-555555555555.jsonl",
		`{"type":"user","message":{"role":"user","content":"sibling prompt"}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"s1","name":"Bash"}]}}`)
	time.Sleep(2500 * time.Millisecond)
	if h.seen("executing") {
		t.Fatalf("watcher adopted the sibling transcript: %v", h.statuses)
	}
	h.appendTo(id+".jsonl",
		`{"type":"user","origin":{"kind":"human"},"message":{"role":"user","content":"my prompt"}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"m1","name":"Read"}]}}`)
	h.waitFor("executing", "Read")
	if got := h.last()[3]; got != "my prompt" {
		t.Fatalf("title = %q, want my prompt", got)
	}
	if got := agent.BoundExternalSession("leaf-pinned"); got != filepath.Join(h.dir, id+".jsonl") {
		t.Fatalf("external session = %q", got)
	}
}

// TestClaudeWatcherShortTurnStillReportsWork: a whole turn that lands
// between two reads is surfaced as thinking -> idle (so the "finished"
// notification fires) instead of a silent idle -> idle.
func TestClaudeWatcherShortTurnStillReportsWork(t *testing.T) {
	id := "21111111-2222-4333-8444-555555555555"
	h := startClaudeWatch(t, "leaf-short", []string{"claude", "--session-id", id})
	h.appendTo(id+".jsonl",
		`{"type":"user","origin":{"kind":"human"},"message":{"role":"user","content":"hi"}}`,
		`{"type":"assistant","message":{"role":"assistant","stop_reason":"end_turn","content":[{"type":"text","text":"hello"}]}}`,
		`{"type":"system","subtype":"turn_duration"}`)
	h.waitFor("idle", "")
	if !h.seen("thinking") {
		t.Fatalf("working phase not reported: %v", h.statuses)
	}
}

// TestClaudeWatcherPermissionPrompt: Claude writes nothing while a tool
// permission dialog is pending. The PTY dialog sighting turns the pending
// tool into waiting_input until the user answers.
func TestClaudeWatcherPermissionPrompt(t *testing.T) {
	id := "31111111-2222-4333-8444-555555555555"
	leaf := "leaf-perm"
	agent.SetPtySubmitForTest(leaf, time.Time{})
	agent.ClearPtyPendingInput(leaf)
	h := startClaudeWatch(t, leaf, []string{"claude", "--session-id", id})
	h.appendTo(id+".jsonl",
		`{"type":"user","origin":{"kind":"human"},"message":{"role":"user","content":"run build"}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"b1","name":"Bash"}]}}`)
	h.waitFor("executing", "Bash")
	agent.SetPtyPendingInputForTest(leaf, time.Now())
	h.waitFor("waiting_input", "Bash")
	agent.SetPtySubmitForTest(leaf, time.Now().Add(10*time.Millisecond)) // user picks "Yes"
	h.waitFor("executing", "Bash")
	h.appendTo(id+".jsonl",
		`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"b1","content":"ok"}]}}`)
	h.waitFor("thinking", "")
	if !agent.LastPtyPendingInput(leaf).IsZero() {
		t.Fatal("prompt sighting should be cleared once the tool resolved")
	}
}

// TestClaudeWatcherFollowsClear: /clear makes Claude continue in a brand-new
// transcript. The watcher follows it when this pane submitted the input.
func TestClaudeWatcherFollowsClear(t *testing.T) {
	id := "41111111-2222-4333-8444-555555555555"
	leaf := "leaf-clear"
	agent.SetPtySubmitForTest(leaf, time.Time{})
	h := startClaudeWatch(t, leaf, []string{"claude", "--session-id", id})
	h.appendTo(id+".jsonl",
		`{"type":"user","origin":{"kind":"human"},"message":{"role":"user","content":"old task"}}`,
		`{"type":"assistant","message":{"role":"assistant","stop_reason":"end_turn","content":[{"type":"text","text":"done"}]}}`,
		`{"type":"system","subtype":"turn_duration"}`)
	h.waitFor("idle", "")
	agent.SetPtySubmitForTest(leaf, time.Now()) // user typed /clear + Enter
	newID := "42222222-2222-4333-8444-555555555555"
	h.appendTo(id+".jsonl", `{"type":"cost-state"}`)
	h.appendTo(newID+".jsonl",
		`{"type":"mode","mode":"normal"}`,
		`{"type":"user","isMeta":true,"message":{"role":"user","content":"<local-command-caveat>x</local-command-caveat>"}}`,
		`{"type":"user","message":{"role":"user","content":"<command-name>/clear</command-name>"}}`,
		`{"type":"system","subtype":"local_command","content":"<local-command-stdout></local-command-stdout>"}`)
	deadline := time.Now().Add(8 * time.Second)
	for agent.BoundExternalSession(leaf) != filepath.Join(h.dir, newID+".jsonl") {
		if time.Now().After(deadline) {
			t.Fatalf("watcher did not follow /clear; bound=%q", agent.BoundExternalSession(leaf))
		}
		time.Sleep(100 * time.Millisecond)
	}
	if l := h.last(); l[0] != "idle" || l[3] != "" {
		t.Fatalf("after /clear got %v, want idle with empty title", l)
	}
	h.appendTo(newID+".jsonl",
		`{"type":"user","origin":{"kind":"human"},"message":{"role":"user","content":"new task"}}`)
	h.waitFor("thinking", "")
	if got := h.last()[3]; got != "new task" {
		t.Fatalf("title = %q, want new task", got)
	}
}

// TestClaudeWatcherPermissionDialogBeforeLateBind: in a fresh project dir the
// transcript can be bound only after the dialog was already rendered (the
// prompt and the tool_use are read together). The earlier sighting must not
// be discarded as stale: it is newer than the prompt line.
func TestClaudeWatcherPermissionDialogBeforeLateBind(t *testing.T) {
	id := "51111111-2222-4333-8444-555555555555"
	leaf := "leaf-late-bind"
	agent.SetPtySubmitForTest(leaf, time.Now().Add(-3*time.Second)) // prompt submitted
	h := startClaudeWatch(t, leaf, []string{"claude", "--session-id", id})
	agent.SetPtyPendingInputForTest(leaf, time.Now()) // dialog rendered
	ts := time.Now().Add(-2 * time.Second).UTC().Format(time.RFC3339Nano)
	h.appendTo(id+".jsonl",
		`{"type":"user","timestamp":"`+ts+`","origin":{"kind":"human"},"message":{"role":"user","content":"touch x"}}`,
		`{"type":"assistant","timestamp":"`+ts+`","message":{"role":"assistant","content":[{"type":"tool_use","id":"b1","name":"Bash"}]}}`)
	h.waitFor("waiting_input", "Bash")
}

func TestFileChangeNotifierDescendsIntoCreatedDirs(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "proj", "sess.jsonl")
	n, err := NewFileChangeNotifier()
	if err != nil {
		t.Fatal(err)
	}
	defer n.Close()
	n.Watch(target)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	if err := os.WriteFile(target, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(2 * time.Second)
	for {
		select {
		case <-n.Notify():
			if _, err := os.Stat(target); err == nil {
				return
			}
		case <-deadline:
			t.Fatal("no notification for a target created inside a new directory")
		}
	}
}
