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

type fxStep struct {
	status, tool, details string
	transient             string
}

// fxV2Replay feeds a real (scrubbed) fx 0.0.16 v2 log capture to the parser
// one line at a time and returns what was reported after each line (1-based).
func fxV2Replay(t *testing.T, fixture string) []fxStep {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "fx", fixture))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	p := filepath.Join(t.TempDir(), "log.jsonl")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var state fxV2State
	var offset int64
	steps := []fxStep{{}}
	w := &FxWatcher{}
	for _, l := range strings.SplitAfter(string(data), "\n") {
		if strings.TrimSpace(l) == "" {
			continue
		}
		if _, err := f.WriteString(l); err != nil {
			t.Fatal(err)
		}
		var st fxStep
		w.parseFxV2Log(p, offset, &state, func(s, _, _ string) { st.transient = s }, func(s, tool, d, _ string) {
			st.status, st.tool, st.details = s, tool, d
		})
		offset += int64(len(l))
		steps = append(steps, st)
	}
	return steps
}

func expectFx(t *testing.T, steps []fxStep, line int, status, tool string) {
	t.Helper()
	if got := steps[line]; got.status != status || (tool != "*" && got.tool != tool) {
		t.Fatalf("after line %d: got (%q, %q, %q), want (%q, %q)", line, got.status, got.tool, got.details, status, tool)
	}
}

// TestFxV2FixtureSession replays a live fx 0.0.16 session (custom
// OpenAI-compatible provider, "ask" permission mode): a shell command behind a
// permission prompt, ask_user_question, a cancelled long command and a failed
// read.
func TestFxV2FixtureSession(t *testing.T) {
	steps := fxV2Replay(t, "v2-session.jsonl")
	expectFx(t, steps, 1, "idle", "")
	expectFx(t, steps, 6, "thinking", "")
	expectFx(t, steps, 7, "thinking", "")
	// tool_running is written before the permission prompt and the run; the
	// watcher's PTY prompt check turns it into waiting_input while pending.
	expectFx(t, steps, 9, "executing", "shell")
	expectFx(t, steps, 12, "thinking", "")
	expectFx(t, steps, 14, "idle", "")
	if steps[14].details != "done" {
		t.Fatalf("idle details = %q, want final answer", steps[14].details)
	}
	expectFx(t, steps, 15, "idle", "")

	// ask_user_question pending in the log -> waiting_input.
	expectFx(t, steps, 21, "waiting_input", "ask_user_question")
	expectFx(t, steps, 24, "thinking", "")
	expectFx(t, steps, 27, "idle", "")

	// Esc Esc during sleep 30.
	expectFx(t, steps, 31, "executing", "shell")
	expectFx(t, steps, 33, "interrupted", "*")
	expectFx(t, steps, 34, "interrupted", "*")

	// Missing file: tool_failed until the turn ends.
	expectFx(t, steps, 38, "executing", "read_file")
	expectFx(t, steps, 41, "tool_failed", "read_file")
	if !strings.Contains(steps[41].details, "missing.txt") && steps[41].details == "" {
		t.Fatalf("tool_failed details = %q", steps[41].details)
	}
	expectFx(t, steps, 42, "tool_failed", "read_file")
	expectFx(t, steps, 43, "idle", "")
}

// TestFxV2FixtureBatchReadReportsTransientWork checks that a single read that
// covers a whole turn (fx flushes a fast turn in one burst) reports the working
// state it passed through before idle, so the finished notification fires.
func TestFxV2FixtureBatchReadReportsTransientWork(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "fx", "v2-session.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.SplitAfter(string(data), "\n")
	p := filepath.Join(t.TempDir(), "log.jsonl")
	head := strings.Join(lines[:35], "")
	if err := os.WriteFile(p, []byte(head+strings.Join(lines[35:], "")), 0o644); err != nil {
		t.Fatal(err)
	}
	var state fxV2State
	w := &FxWatcher{}
	w.parseFxV2Log(p, 0, &state, nil, func(string, string, string, string) {})
	var transient, final string
	w.parseFxV2Log(p, int64(len(head)), &state, func(s, _, _ string) { transient = s }, func(s, _, _, _ string) { final = s })
	if final != "idle" {
		t.Fatalf("final = %q, want idle", final)
	}
	if transient != "tool_failed" {
		t.Fatalf("transient = %q, want tool_failed (the failed read before the turn ended)", transient)
	}
}

func TestFxV2LogMatchesWorkspaceAndClosed(t *testing.T) {
	src := filepath.Join("testdata", "fx", "v2-session.jsonl")
	if !fxV2LogMatches(src, "/home/user/proj-fx") {
		t.Fatal("expected workspace match")
	}
	if fxV2LogMatches(src, "/home/user/other") {
		t.Fatal("unexpected match for another cwd")
	}
	if fxV2LogClosed(src) {
		t.Fatal("capture does not end with closed")
	}
	data, _ := os.ReadFile(src)
	p := filepath.Join(t.TempDir(), "log.jsonl")
	closed := `{"v":1,"seq":45,"ts":1793559800000,"kind":"closed","crc":"00000000"}` + "\n"
	if err := os.WriteFile(p, append(data, []byte(closed)...), 0o644); err != nil {
		t.Fatal(err)
	}
	if !fxV2LogClosed(p) {
		t.Fatal("expected closed log")
	}
	if got := fxV2SessionStart(p); got.IsZero() {
		t.Fatal("expected session_created ts")
	}
	child := filepath.Join(t.TempDir(), "log.jsonl")
	if err := os.WriteFile(child, []byte(`{"v":1,"seq":1,"ts":1,"kind":"session_created","id":"x","workspace":"/home/user/proj-fx","role":"child","host":"child","crc":"0"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if fxV2LogMatches(child, "/home/user/proj-fx") {
		t.Fatal("child (subagent) sessions must never bind to a pane")
	}
}

// fxWatchHarness runs FxWatcher.Watch against a temp HOME and records the
// reported statuses.
type fxWatchHarness struct {
	t        *testing.T
	mu       sync.Mutex
	statuses []string
	cancel   context.CancelFunc
	done     chan struct{}
}

func startFxWatch(t *testing.T, leaf, cwd string) *fxWatchHarness {
	t.Helper()
	h := &fxWatchHarness{t: t, done: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	h.cancel = cancel
	go func() {
		defer close(h.done)
		(&FxWatcher{}).Watch(ctx, leaf, cwd, false, func(status, _, _, _ string) {
			h.mu.Lock()
			h.statuses = append(h.statuses, status)
			h.mu.Unlock()
		}, func() {})
	}()
	t.Cleanup(func() { cancel(); <-h.done })
	return h
}

func (h *fxWatchHarness) last() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.statuses) == 0 {
		return ""
	}
	return h.statuses[len(h.statuses)-1]
}

func (h *fxWatchHarness) waitFor(want string, timeout time.Duration) {
	h.t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if h.last() == want {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.t.Fatalf("timed out waiting for %q; statuses=%v", want, h.statuses)
}

// TestFxWatcherPermissionPromptFromPTY: fx writes tool_running and then shows
// the permission screen, writing nothing else until the tool finishes. The
// prompt footer (drawn after the log write, with no busy status line since)
// means waiting_input; once the user approves, fx redraws "• Running (Ns)"
// and the card goes back to executing although the log is still frozen.
func TestFxWatcherPermissionPromptFromPTY(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cwd := filepath.Join(home, "proj")
	dir := filepath.Join(home, ".fx", "sessions", "v2", "AbCdEf012345")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join("testdata", "fx", "v2-session.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.SplitAfter(strings.ReplaceAll(string(data), "/home/user/proj-fx", cwd), "\n")
	logPath := filepath.Join(dir, "log.jsonl")
	// Up to the first tool_running (sleep 20, permission pending).
	if err := os.WriteFile(logPath, []byte(strings.Join(lines[:9], "")), 0o644); err != nil {
		t.Fatal(err)
	}

	const leaf = "fx-perm-leaf"
	agent.SetPtyBusyForTest(leaf, time.Time{})
	agent.SetPtyPendingInputForTest(leaf, time.Time{})
	h := startFxWatch(t, leaf, cwd)
	h.waitFor("executing", 6*time.Second)

	agent.SetPtyPendingInputForTest(leaf, time.Now().Add(time.Second))
	h.waitFor("waiting_input", 8*time.Second)

	// Approved: the busy status line is drawn again.
	stop := make(chan struct{})
	go func() {
		for {
			select {
			case <-stop:
				return
			case <-time.After(200 * time.Millisecond):
				agent.SetPtyBusyForTest(leaf, time.Now())
			}
		}
	}()
	h.waitFor("executing", 8*time.Second)
	close(stop)

	// The tool finishes and the turn is committed.
	f, _ := os.OpenFile(logPath, os.O_APPEND|os.O_WRONLY, 0o644)
	f.WriteString(strings.Join(lines[9:15], ""))
	f.Close()
	h.waitFor("idle", 6*time.Second)
}

// TestFxWatcherFirstTurnBeforeLogExists: a brand-new fx session has no log
// until the model answers, so the "• Thinking (Ns)" status line is the only
// signal that the first turn is in flight.
func TestFxWatcherFirstTurnBeforeLogExists(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cwd := filepath.Join(home, "proj")
	const leaf = "fx-unbound-leaf"
	agent.SetPtyBusyForTest(leaf, time.Time{})
	agent.SetPtyInterruptForTest(leaf, time.Time{})
	h := startFxWatch(t, leaf, cwd)

	stop := make(chan struct{})
	go func() {
		for {
			select {
			case <-stop:
				return
			case <-time.After(200 * time.Millisecond):
				agent.SetPtyBusyForTest(leaf, time.Now())
			}
		}
	}()
	h.waitFor("thinking", 6*time.Second)
	close(stop)
	// The status line disappears without a log ever being written and the
	// user had pressed Esc: the turn was cancelled.
	agent.SetPtyInterruptForTest(leaf, time.Now())
	h.waitFor("interrupted", 10*time.Second)
}
