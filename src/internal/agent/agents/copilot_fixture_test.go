package agents

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// copilotReplay feeds a real (scrubbed) Copilot CLI events.jsonl capture to
// the parser one line at a time — the worst case for incremental reads — and
// returns the status reported after each line (index = 1-based line number;
// "" when that line produced no callback).
type copilotStep struct {
	status, tool, details string
}

func copilotReplay(t *testing.T, fixture string) []copilotStep {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "copilot", fixture))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	lines := strings.SplitAfter(string(data), "\n")
	p := filepath.Join(t.TempDir(), "events.jsonl")
	f, err := os.Create(p)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer f.Close()
	var state copilotWatchState
	steps := []copilotStep{{}}
	var offset int64
	w := &CopilotWatcher{}
	for _, l := range lines {
		if strings.TrimSpace(l) == "" {
			continue
		}
		if _, err := f.WriteString(l); err != nil {
			t.Fatalf("write: %v", err)
		}
		var step copilotStep
		w.parseCopilotEvents(p, offset, &state, func(s, tool, d, _ string) {
			step = copilotStep{s, tool, d}
		})
		offset += int64(len(l))
		steps = append(steps, step)
	}
	return steps
}

func expectStep(t *testing.T, steps []copilotStep, line int, status, tool string) {
	t.Helper()
	if got := steps[line]; got.status != status || (tool != "*" && got.tool != tool) {
		t.Fatalf("after line %d: got (%q, %q, %q), want (%q, %q)", line, got.status, got.tool, got.details, status, tool)
	}
}

// TestCopilotFixtureInteractiveSession replays a live Copilot CLI 1.0.105
// interactive session (BYOK, scripted model) covering a permission prompt, a
// long shell tool, ask_user, a failing command and a user interrupt.
func TestCopilotFixtureInteractiveSession(t *testing.T) {
	steps := copilotReplay(t, "interactive-session.jsonl")

	// sleep 20: permission prompt -> approved -> runs -> done.
	expectStep(t, steps, 9, "executing", "bash")      // tool.execution_start
	expectStep(t, steps, 10, "waiting_input", "bash") // permission.requested
	if !strings.Contains(steps[10].details, "sleep 20") {
		t.Fatalf("permission details = %q, want the command", steps[10].details)
	}
	expectStep(t, steps, 11, "executing", "bash") // permission.completed (approved)
	expectStep(t, steps, 12, "thinking", "")      // tool.execution_complete
	// assistant.turn_end after a tool-requesting model call is NOT the end of
	// the user's turn: Copilot calls the model again with the tool result.
	expectStep(t, steps, 13, "thinking", "")
	expectStep(t, steps, 14, "thinking", "")
	expectStep(t, steps, 16, "thinking", "") // final assistant.message, turn_end not yet written
	expectStep(t, steps, 17, "idle", "")
	if steps[17].details != "done" {
		t.Fatalf("idle details = %q, want final answer", steps[17].details)
	}

	// ask_user blocks on the user.
	expectStep(t, steps, 21, "waiting_input", "ask_user")
	expectStep(t, steps, 22, "waiting_input", "ask_user")
	expectStep(t, steps, 29, "idle", "")

	// `ls` of a missing dir exits 2: surfaced as tool_failed and kept while
	// the model works on the error, until the turn ends.
	expectStep(t, steps, 35, "tool_failed", "bash")
	if !strings.Contains(steps[35].details, "nonexistent-dir-xyz") {
		t.Fatalf("tool_failed details = %q, want the error output", steps[35].details)
	}
	expectStep(t, steps, 36, "tool_failed", "bash")
	expectStep(t, steps, 37, "tool_failed", "bash")
	expectStep(t, steps, 40, "idle", "")

	// sleep 40: approved, then Esc Esc -> abort.
	expectStep(t, steps, 46, "waiting_input", "bash")
	expectStep(t, steps, 47, "executing", "bash")
	expectStep(t, steps, 48, "thinking", "")
	expectStep(t, steps, 49, "interrupted", "")
}

// TestCopilotFixturePlanModeAndHaikuToolFailure replays a resumed session with
// a plan-mode exit_plan_mode approval and a real model turn whose shell command
// failed.
func TestCopilotFixturePlanModeAndHaikuToolFailure(t *testing.T) {
	steps := copilotReplay(t, "plan-and-failure.jsonl")
	expectStep(t, steps, 55, "waiting_input", "exit_plan_mode")
	expectStep(t, steps, 58, "thinking", "")
	expectStep(t, steps, 65, "idle", "")
	expectStep(t, steps, 84, "tool_failed", "bash")
	expectStep(t, steps, 86, "tool_failed", "bash")
	expectStep(t, steps, 88, "tool_failed", "bash")
	expectStep(t, steps, 89, "idle", "")
}

// TestCopilotFixtureBatchReadEndsIdle checks that reading the whole capture at
// once (e.g. reattaching after a Caw restart) lands on the final state.
func TestCopilotFixtureBatchReadEndsIdle(t *testing.T) {
	p := filepath.Join("testdata", "copilot", "interactive-session.jsonl")
	var state copilotWatchState
	var status string
	(&CopilotWatcher{}).parseCopilotEvents(p, 0, &state, func(s, _, _, _ string) { status = s })
	if status != "idle" {
		t.Fatalf("batch status = %q, want idle", status)
	}
	if len(state.pendingPermissions) != 0 {
		t.Fatalf("pending permissions leaked: %v", state.pendingPermissions)
	}
}

func TestCopilotSessionEndedDetectsShutdown(t *testing.T) {
	src := filepath.Join("testdata", "copilot", "interactive-session.jsonl")
	if !copilotSessionEnded(src) {
		t.Fatal("capture ending in session.shutdown must be reported as ended")
	}
	data, _ := os.ReadFile(src)
	lines := strings.SplitAfter(string(data), "\n")
	p := filepath.Join(t.TempDir(), "events.jsonl")
	// Up to the resumed session's last turn_end (line 62): live.
	if err := os.WriteFile(p, []byte(strings.Join(lines[:62], "")), 0o644); err != nil {
		t.Fatal(err)
	}
	if copilotSessionEnded(p) {
		t.Fatal("resumed session (session.resume after shutdown) must not be reported as ended")
	}
	if got := copilotSessionStart(src); got.IsZero() {
		t.Fatal("expected session.start timestamp")
	}
}
