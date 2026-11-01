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

func TestCommandCodeProjectSlug(t *testing.T) {
	cases := map[string]string{
		"/root/caw/testenv/status-misc/proj-cc": "root-caw-testenv-status-misc-proj-cc",
		"/home/user/My Project":                 "home-user-my-project",
		"/home/user/fooBar":                     "home-user-foo-bar",
		"/":                                     "root",
	}
	for in, want := range cases {
		if got := commandCodeProjectSlug(in); got != want {
			t.Errorf("commandCodeProjectSlug(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestCommandCodeWatcherFirstTurnBeforeTranscriptExists reproduces the live
// Command Code 1.88.2 behaviour on a brand-new session: submitting the first
// prompt creates only <id>.checkpoints.jsonl; the transcript is written when
// the first iteration completes. A tool approval prompt in between used to be
// invisible (card stayed idle, no notification). The watcher now binds to the
// pending transcript via the checkpoints sidecar and reads the rendered
// screen: busy status line -> thinking, permission screen -> waiting_input.
func TestCommandCodeWatcherFirstTurnBeforeTranscriptExists(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cwd := filepath.Join(home, "proj")
	projDir := filepath.Join(home, ".commandcode", "projects", commandCodeProjectSlug(cwd))
	if err := os.MkdirAll(projDir, 0o755); err != nil {
		t.Fatal(err)
	}
	const id = "86154648-037b-45b7-8161-fa10a850296c"
	ckpt := filepath.Join(projDir, id+".checkpoints.jsonl")
	transcript := filepath.Join(projDir, id+".jsonl")

	const leaf = "cc-first-turn"
	agent.SetPtyBusyForTest(leaf, time.Time{})
	agent.SetPtyPendingInputForTest(leaf, time.Time{})
	agent.SetPtyToolRunningForTest(leaf, time.Time{})

	var mu sync.Mutex
	var statuses []string
	var titles []string
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		(&CommandCodeWatcher{}).Watch(ctx, leaf, cwd, false, func(status, _, _, title string) {
			mu.Lock()
			statuses = append(statuses, status)
			titles = append(titles, title)
			mu.Unlock()
		}, func() {})
	}()
	defer func() { cancel(); <-done }()
	last := func() string {
		mu.Lock()
		defer mu.Unlock()
		if len(statuses) == 0 {
			return ""
		}
		return statuses[len(statuses)-1]
	}
	waitFor := func(want string) {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			if last() == want {
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
		mu.Lock()
		defer mu.Unlock()
		t.Fatalf("timed out waiting for %q; statuses=%v", want, statuses)
	}

	// The user submits the first prompt: checkpoint written, spinner drawn.
	agent.SetPtyBusyForTest(leaf, time.Now())
	if err := os.WriteFile(ckpt, []byte(`{"id":"cd61b1f6","messageId":"cd61b1f6","turnNumber":1,"createdAt":"2026-11-01T18:54:50.954Z","prompt":"CAWTEST:shell:sleep 20","messageCount":0,"files":[]}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
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
	waitFor("thinking")
	close(stop)
	mu.Lock()
	gotTitle := titles[len(titles)-1]
	mu.Unlock()
	if gotTitle != "CAWTEST:shell:sleep 20" {
		t.Fatalf("title = %q, want the first prompt from the checkpoint", gotTitle)
	}

	// "Execute Shell Command" permission screen; the spinner is gone.
	agent.SetPtyPendingInputForTest(leaf, time.Now())
	waitFor("waiting_input")

	// Approved and finished: Command Code writes the whole iteration.
	lines := `{"type":"session","version":3,"id":"` + id + `","timestamp":"2026-11-01T18:54:43.499Z","cwd":"` + cwd + `"}
{"type":"message","id":"9e78b525","parentId":null,"timestamp":"2026-11-01T18:55:13.049Z","message":{"role":"user","content":[{"type":"text","text":"CAWTEST:shell:sleep 20"}]}}
{"type":"message","id":"46d6d8ea","parentId":"9e78b525","timestamp":"2026-11-01T18:55:13.049Z","message":{"role":"assistant","content":[{"type":"tool_use","id":"call_bf7c","name":"shell_command","input":{"command":"sleep 20"}}]}}
{"type":"message","id":"7cc76634","parentId":"46d6d8ea","timestamp":"2026-11-01T18:55:13.050Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"call_bf7c","content":[{"type":"text","text":"ok"}]}]}}
{"type":"message","id":"fd69cc6d","parentId":"7cc76634","timestamp":"2026-11-01T18:55:13.258Z","message":{"role":"assistant","content":[{"type":"text","text":"done"}]}}
`
	if err := os.WriteFile(transcript, []byte(lines), 0o644); err != nil {
		t.Fatal(err)
	}
	waitFor("idle")
	mu.Lock()
	defer mu.Unlock()
	// waiting_input -> idle in one read must pass through a working state so
	// the "finished" notification (working -> idle) fires.
	if n := len(statuses); n < 2 || statuses[n-2] != "thinking" {
		t.Fatalf("expected thinking right before idle, statuses=%v", statuses)
	}
}

// TestCommandCodeWatcherInterruptFlushStillInterrupts reproduces the live
// failure where pressing Esc during a running shell tool left the card on
// "thinking" forever: Command Code reacts to the interrupt by flushing the
// iteration with a "Command aborted" tool_result, which the watcher parsed as
// a fresh "thinking" and stamped as the start of a new working turn — after
// the interrupt — so the interrupt was then ignored.
func TestCommandCodeWatcherInterruptFlushStillInterrupts(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cwd := filepath.Join(home, "proj")
	projDir := filepath.Join(home, ".commandcode", "projects", "home-proj")
	if err := os.MkdirAll(projDir, 0o755); err != nil {
		t.Fatal(err)
	}
	transcript := filepath.Join(projDir, "s1.jsonl")
	appendLines := func(s string) {
		f, err := os.OpenFile(transcript, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		f.WriteString(s)
	}
	appendLines(`{"type":"session","version":3,"id":"s1","timestamp":"2026-11-01T18:54:43.499Z","cwd":"` + cwd + `"}
{"type":"message","id":"u1","parentId":null,"timestamp":"2026-11-01T18:59:19.229Z","message":{"role":"user","content":[{"type":"text","text":"run it"}]}}
{"type":"message","id":"a0","parentId":"u1","timestamp":"2026-11-01T18:59:19.229Z","message":{"role":"assistant","content":[{"type":"tool_use","id":"t0","name":"read_file","input":{}}]}}
{"type":"message","id":"r0","parentId":"a0","timestamp":"2026-11-01T18:59:19.229Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t0","content":[{"type":"text","text":"ok"}]}]}}
`)
	const leaf = "cc-int-flush"
	agent.SetPtyInterruptForTest(leaf, time.Time{})
	agent.SetPtyBusyForTest(leaf, time.Time{})
	var mu sync.Mutex
	var statuses []string
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		(&CommandCodeWatcher{}).Watch(ctx, leaf, cwd, false, func(status, _, _, _ string) {
			mu.Lock()
			statuses = append(statuses, status)
			mu.Unlock()
		}, func() {})
	}()
	defer func() { cancel(); <-done }()
	waitFor := func(want string) {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			mu.Lock()
			l := ""
			if len(statuses) > 0 {
				l = statuses[len(statuses)-1]
			}
			mu.Unlock()
			if l == want {
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
		mu.Lock()
		defer mu.Unlock()
		t.Fatalf("timed out waiting for %q; statuses=%v", want, statuses)
	}
	waitFor("thinking")
	time.Sleep(100 * time.Millisecond)

	// Esc, immediately followed by the abort flush.
	agent.SetPtyInterruptForTest(leaf, time.Now())
	appendLines(`{"type":"message","id":"a1","parentId":"r0","timestamp":"2026-11-01T18:59:20.229Z","message":{"role":"assistant","content":[{"type":"tool_use","id":"t1","name":"shell_command","input":{"command":"echo a && sleep 30"}}]}}
{"type":"message","id":"r1","parentId":"a1","timestamp":"2026-11-01T18:59:20.229Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":[{"type":"text","text":"a\n\nCommand aborted"}]}]}}
`)
	waitFor("interrupted")
}
