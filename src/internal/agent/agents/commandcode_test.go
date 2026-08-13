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

func TestCommandCodeWatcherPTYInterruptDetectedWithoutTranscriptMarker(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cwd := filepath.Join(home, "proj")
	projectsDir := filepath.Join(home, ".commandcode", "projects", "home-proj")
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

	const leafID = "cc-int-pty"
	var mu sync.Mutex
	var statuses []string
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		(&CommandCodeWatcher{}).Watch(ctx, leafID, cwd, false, func(status, _, _, _ string) {
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

	// Session header with matching cwd so the watcher binds.
	header := `{"type":"session","version":3,"id":"abc","timestamp":"2026-08-13T16:41:01.269Z","cwd":"` + cwd + `"}`
	appendLine(header)
	// A prompt + an assistant tool_use → the watcher binds and reports executing.
	appendLine(`{"type":"message","id":"u1","parentId":null,"timestamp":"2026-08-13T16:41:02.000Z","message":{"role":"user","content":[{"type":"text","text":"read x"}]}}`)
	appendLine(`{"type":"message","id":"a1","parentId":"u1","timestamp":"2026-08-13T16:41:03.000Z","message":{"role":"assistant","content":[{"type":"tool_use","id":"toolu_1","name":"Read"}]}}`)
	waitFor("executing", 8*time.Second)

	// User presses Ctrl+C → card flips to interrupted immediately, with NO
	// transcript marker written yet (Command Code never persists interrupted
	// turns, so the transcript stops growing until the agent resumes or the
	// user starts a new turn).
	agent.SetPtyInterruptForTest(leafID, time.Now())
	waitFor("interrupted", 8*time.Second)

	// REGRESSION GUARD: with no further transcript activity the card must
	// stay interrupted (a genuine interrupt writes nothing to the transcript).
	time.Sleep(4 * time.Second)
	if got := lastStatus(); got != "interrupted" {
		t.Fatalf("card drifted from %q to %q with no transcript activity (statuses: %v)", "interrupted", got, statuses)
	}

	// REGRESSION GUARD: continued assistant work past the interrupt boundary
	// means the agent kept running after a soft interrupt (Command Code drops
	// interrupted turns, so a real interrupt never produces this traffic).
	// The card must flip back to the real working state, not stay stuck on
	// "interrupted".
	appendLine(`{"type":"message","id":"u2","parentId":"a1","timestamp":"2026-08-13T16:41:04.000Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":[{"type":"text","text":"ok"}]}]}}`)
	appendLine(`{"type":"message","id":"a2","parentId":"u2","timestamp":"2026-08-13T16:41:05.000Z","message":{"role":"assistant","content":[{"type":"tool_use","id":"toolu_2","name":"Bash"}]}}`)
	waitFor("executing", 8*time.Second)

	// A genuinely new user prompt also clears the sticky interrupt and lands
	// the card on thinking.
	appendLine(`{"type":"message","id":"u3","parentId":"a2","timestamp":"2026-08-13T16:41:06.000Z","message":{"role":"user","content":[{"type":"text","text":"next step"}]}}`)
	waitFor("thinking", 8*time.Second)

	cancel()
	<-done
}