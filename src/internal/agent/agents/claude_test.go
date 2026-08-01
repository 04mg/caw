package agents

import (
	"context"
	"os"
	"path/filepath"
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

func TestClaudeInterruptReportsInterrupted(t *testing.T) {
	// Claude writes a user message containing "[request interrupted by user"
	// when the user cancels the in-flight tool call. The watcher must report
	// "interrupted" (not idle) so the UI shows a red dot.
	lines := []string{
		`{"type":"user","message":{"role":"user","content":"read /nonexistent/xyz.txt"}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"toolu_1","name":"Read"}]}}`,
		`{"type":"user","message":{"role":"user","content":"[request interrupted by user for a tool use]"}}`,
	}
	p := writeClaudeTranscript(t, lines)
	var status, tool string
	(&ClaudeWatcher{}).parseClaudeLog(p, 0, func(s, tl, d, ti string) {
		status, tool = s, tl
	})
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
	p := writeClaudeTranscript(t, lines)
	var status, tool, details string
	(&ClaudeWatcher{}).parseClaudeLog(p, 0, func(s, tl, d, ti string) {
		status, tool, details = s, tl, d
	})
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
	p := writeClaudeTranscript(t, lines)
	var status, tool string
	(&ClaudeWatcher{}).parseClaudeLog(p, 0, func(s, tl, d, ti string) {
		status, tool = s, tl
	})
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