package agents

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/04mg/caw/internal/agent"
)

func writeCodexTranscript(t *testing.T, lines []string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "rollout.jsonl")
	f, err := os.Create(p)
	if err != nil {
		t.Fatalf("create rollout: %v", err)
	}
	defer f.Close()
	for _, l := range lines {
		if _, err := f.WriteString(l + "\n"); err != nil {
			t.Fatalf("write rollout: %v", err)
		}
	}
	return p
}

func TestCodexTurnAbortedReportsInterrupted(t *testing.T) {
	// Codex writes a "turn_aborted" payload when the user cancels the turn.
	// The watcher reports "interrupted" (not idle) for a red dot.
	lines := []string{
		`{"type":"event_msg","payload":{"type":"user_message","message":"read /nonexistent/xyz.txt"}}`,
		`{"type":"event_msg","payload":{"type":"function_call","name":"read"}}`,
		`{"type":"event_msg","payload":{"type":"turn_aborted"}}`,
	}
	p := writeCodexTranscript(t, lines)
	var status string
	(&CodexWatcher{}).parseCodexLog(p, 0, func(s, tl, d, ti string) {
		status = s
	})
	if status != "interrupted" {
		t.Fatalf("aborted status = %q, want interrupted", status)
	}
}

func TestCodexTaskCompleteWithErrorReportsInterrupted(t *testing.T) {
	// A task_complete payload carrying an error means the turn failed (e.g.
	// an API or server tool error) and Codex is back at its prompt. The
	// watcher surfaces it as interrupted (idle column, red dot) with the
	// error message — not tool_failed, which would keep the card in the
	// working column forever.
	lines := []string{
		`{"type":"event_msg","payload":{"type":"user_message","message":"read /nonexistent/xyz.txt"}}`,
		`{"type":"event_msg","payload":{"type":"function_call","name":"exec"}}`,
		`{"type":"event_msg","payload":{"type":"task_complete","error":{"message":"Server tool request failed","codex_error_info":"other"}}}`,
	}
	p := writeCodexTranscript(t, lines)
	var status, details string
	(&CodexWatcher{}).parseCodexLog(p, 0, func(s, tl, d, ti string) {
		status, details = s, d
	})
	if status != "interrupted" {
		t.Fatalf("task error status = %q, want interrupted", status)
	}
	if !strings.Contains(details, "Server tool request failed") {
		t.Fatalf("details = %q, want the error message", details)
	}
}

func TestCodexTaskCompleteNoErrorReportsIdle(t *testing.T) {
	lines := []string{
		`{"type":"event_msg","payload":{"type":"user_message","message":"hi"}}`,
		`{"type":"response_item","payload":{"type":"message","role":"assistant","phase":"final_answer","message":"done"}}`,
		`{"type":"event_msg","payload":{"type":"task_complete"}}`,
	}
	p := writeCodexTranscript(t, lines)
	var status string
	(&CodexWatcher{}).parseCodexLog(p, 0, func(s, tl, d, ti string) {
		status = s
	})
	if status != "idle" {
		t.Fatalf("clean task_complete status = %q, want idle", status)
	}
}

func TestCodexModernRolloutTracksToolAndTitle(t *testing.T) {
	// Newer Codex rollouts record tool calls as response_item "custom_tool_call"
	// and carry the user prompt only as a response_item "message" (no
	// event_msg "user_message"). The watcher must surface the in-flight tool
	// name and a title derived from the real prompt.
	lines := []string{
		`{"type":"session_meta","payload":{"session_id":"x"}}`,
		`{"type":"event_msg","payload":{"type":"task_started"}}`,
		`{"type":"response_item","payload":{"type":"message","role":"developer"}}`,
		`{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"<environment_context>\n<cwd>/repo</cwd>\n</environment_context>"}]}}`,
		`{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"Refactor backend to SQLite"}]}}`,
		`{"type":"response_item","payload":{"type":"message","role":"assistant","phase":"commentary","content":[{"type":"output_text","text":"Voy a revisar el repo"}]}}`,
		`{"type":"response_item","payload":{"type":"custom_tool_call","name":"exec"}}`,
		`{"type":"response_item","payload":{"type":"custom_tool_call_output"}}`,
		`{"type":"response_item","payload":{"type":"message","role":"assistant","phase":"commentary","content":[{"type":"output_text","text":"Encontré la estructura"}]}}`,
		`{"type":"response_item","payload":{"type":"custom_tool_call","name":"apply_patch"}}`,
	}
	p := writeCodexTranscript(t, lines)
	var status, tool, title string
	(&CodexWatcher{}).parseCodexLog(p, 0, func(s, tl, d, ti string) {
		status, tool, title = s, tl, ti
	})
	if status != "executing" {
		t.Fatalf("modern rollout status = %q, want executing", status)
	}
	if tool != "apply_patch" {
		t.Fatalf("tool = %q, want apply_patch", tool)
	}
	if title != "Refactor backend to SQLite" {
		t.Fatalf("title = %q, want the real prompt", title)
	}
}

func TestCodexModernRolloutFinalAnswerIdle(t *testing.T) {
	// A turn that completes via final_answer in the modern format (no event
	// user_message, custom tool calls) must settle to idle.
	lines := []string{
		`{"type":"event_msg","payload":{"type":"task_started"}}`,
		`{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"explain AGENTS.md"}]}}`,
		`{"type":"response_item","payload":{"type":"custom_tool_call","name":"read"}}`,
		`{"type":"response_item","payload":{"type":"custom_tool_call_output"}}`,
		`{"type":"response_item","payload":{"type":"message","role":"assistant","phase":"final_answer","content":[{"type":"output_text","text":"Done."}]}}`,
		`{"type":"event_msg","payload":{"type":"task_complete"}}`,
	}
	p := writeCodexTranscript(t, lines)
	var status, tool, title string
	(&CodexWatcher{}).parseCodexLog(p, 0, func(s, tl, d, ti string) {
		status, tool, title = s, tl, ti
	})
	if status != "idle" {
		t.Errorf("final_answer status = %q, want idle", status)
	}
	if tool != "" {
		t.Errorf("final_answer tool = %q, want empty", tool)
	}
	if title != "explain AGENTS.md" {
		t.Errorf("title = %q, want explain AGENTS.md", title)
	}
}

func TestCodexPriorTurnCompletionDoesNotMarkInFlightTurn(t *testing.T) {
	// Rollout files accumulate many turns. A task_complete/final_answer from an
	// EARLIER turn must not be applied to the current, still-in-flight turn,
	// otherwise a busy session would be reported idle. Here the final turn is
	// mid-execution (custom_tool_call with no final_answer yet) after a fully
	// completed prior turn. Once the tool returned the turn is still working
	// (thinking), never idle.
	lines := []string{
		`{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"first prompt"}]}}`,
		`{"type":"response_item","payload":{"type":"custom_tool_call","name":"read"}}`,
		`{"type":"response_item","payload":{"type":"custom_tool_call_output"}}`,
		`{"type":"response_item","payload":{"type":"message","role":"assistant","phase":"final_answer","content":[{"type":"output_text","text":"first done"}]}}`,
		`{"type":"event_msg","payload":{"type":"task_complete"}}`,
		`{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"second prompt"}]}}`,
		`{"type":"response_item","payload":{"type":"custom_tool_call","name":"apply_patch"}}`,
		`{"type":"response_item","payload":{"type":"custom_tool_call_output"}}`,
	}
	p := writeCodexTranscript(t, lines)
	var status, tool string
	(&CodexWatcher{}).parseCodexLog(p, 0, func(s, tl, d, ti string) {
		status, tool = s, tl
	})
	if status != "thinking" {
		t.Errorf("status = %q, want thinking", status)
	}
	if tool != "" {
		t.Errorf("tool = %q, want empty once the call returned", tool)
	}
}

// loadCodexFixture returns the lines of a scrubbed real Codex CLI 0.166 TUI
// rollout captured from live runs through Caw against the test LLM proxy.
func loadCodexFixture(t *testing.T, name string) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "codex", name))
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

func TestCodexTuiRolloutStatesByEntry(t *testing.T) {
	// Real 0.166 TUI rollout (one entry per line, ordinal == line index):
	//   turn 1  CAWTEST:shell:sleep 20 (exec_command yields after 10s; the
	//           command finishes in the background AFTER task_complete)
	//   turn 2  CAWTEST:slow:40 interrupted with ESC
	//   turn 3  CAWTEST:read:/nonexistent/xyz.txt (cat exits 1)
	//   turn 4  Plan mode, CAWTEST:ask -> request_user_input, answered
	//   turn 5  Plan mode, Haiku proposes a plan ("Implement this plan?")
	//   turn 6  "Implement the plan." (Default mode, Haiku runs a command)
	lines := loadCodexFixture(t, "tui-0.166-turns.jsonl")
	want := map[int]struct{ status, tool, title, details string }{
		9:  {"executing", "exec_command", "CAWTEST:shell:sleep 20", ""},
		11: {"thinking", "", "", ""},
		17: {"idle", "", "", "done"},
		// Background completion of the yielded command must not reopen the
		// finished turn (it used to flip the card back to "thinking").
		18: {"idle", "", "", ""},
		23: {"thinking", "", "CAWTEST:slow:40", ""},
		// The injected <turn_aborted> user message must not become the title.
		25: {"interrupted", "", "CAWTEST:slow:40", ""},
		31: {"executing", "exec_command", "CAWTEST:read:/nonexistent/xyz.txt", ""},
		33: {"tool_failed", "exec_command", "", "No such file or directory"},
		34: {"tool_failed", "exec_command", "", ""},
		37: {"tool_failed", "exec_command", "", ""},
		40: {"idle", "", "", ""},
		49: {"waiting_input", "request_user_input", "CAWTEST:ask", ""},
		52: {"thinking", "", "", ""},
		58: {"idle", "", "", ""},
		66: {"executing", "exec_command", "", ""},
		72: {"thinking", "", "", "Add CHANGELOG.md"},
		75: {"waiting_input", "plan", "", ""},
		78: {"thinking", "", "", ""},
		86: {"executing", "exec_command", "Implement the plan.", ""},
		95: {"idle", "", "Implement the plan.", "Added"},
	}
	tr := newCodexTracker()
	for i, l := range lines {
		tr.apply(l)
		w, ok := want[i]
		if !ok {
			continue
		}
		if tr.status != w.status || tr.tool != w.tool {
			t.Errorf("after entry %d: status/tool = %q/%q, want %q/%q", i, tr.status, tr.tool, w.status, w.tool)
		}
		if w.title != "" && tr.title != w.title {
			t.Errorf("after entry %d: title = %q, want %q", i, tr.title, w.title)
		}
		if w.details != "" && !strings.Contains(tr.details, w.details) {
			t.Errorf("after entry %d: details = %q, want it to contain %q", i, tr.details, w.details)
		}
	}
	if len(lines) != 96 {
		t.Fatalf("fixture has %d entries, want 96", len(lines))
	}
}

func TestCodexIncrementalReadsKeepState(t *testing.T) {
	// The watcher reads the rollout incrementally. A read that only contains
	// bookkeeping (here: the background command finishing after the turn)
	// must keep the previous state instead of being re-derived from that
	// chunk alone (which used to report "thinking" until the next prompt).
	lines := loadCodexFixture(t, "tui-0.166-turns.jsonl")
	p := writeCodexTranscript(t, lines[:18])
	tr := newCodexTracker()
	chunk, off, err := readCodexLines(p, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range chunk {
		tr.apply(l)
	}
	if tr.status != "idle" {
		t.Fatalf("after turn 1: status = %q, want idle", tr.status)
	}
	f, err := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(lines[18] + "\n")
	f.Close()
	chunk, _, err = readCodexLines(p, off)
	if err != nil || len(chunk) != 1 {
		t.Fatalf("second read: %d lines, err %v", len(chunk), err)
	}
	for _, l := range chunk {
		tr.apply(l)
	}
	if tr.status != "idle" || tr.title != "CAWTEST:shell:sleep 20" {
		t.Fatalf("after background completion: status=%q title=%q, want idle / original title", tr.status, tr.title)
	}
}

func TestCodexApprovalPromptAndTurnError(t *testing.T) {
	// Real rollout of a session launched with --ask-for-approval on-request:
	//   turn 1  exec_command with sandbox_permissions=require_escalated. Codex
	//           shows "Would you like to run the following command?" in the
	//           TUI only: nothing is written between the call (entry 9) and
	//           the approval (entry 11, ~50s later).
	//   turn 2  CAWTEST:fail:400 -> task_complete with an API error.
	lines := loadCodexFixture(t, "tui-0.166-approval-error.jsonl")
	tr := newCodexTracker()
	for _, l := range lines[:11] {
		tr.apply(l)
	}
	if st, tool, _ := tr.reportState(time.Time{}); st != "executing" || tool != "exec_command" {
		t.Fatalf("before prompt: %q/%q, want executing/exec_command", st, tool)
	}
	// Approval prompt rendered in the PTY after the call was recorded.
	if st, tool, _ := tr.reportState(tr.stateAt.Add(200 * time.Millisecond)); st != "waiting_input" || tool != "exec_command" {
		t.Fatalf("prompt after call: %q/%q, want waiting_input/exec_command", st, tool)
	}
	// A prompt marker older than the in-flight call is stale.
	if st, _, _ := tr.reportState(tr.stateAt.Add(-time.Second)); st != "executing" {
		t.Fatalf("stale prompt: %q, want executing", st)
	}
	pending := tr.stateAt.Add(200 * time.Millisecond)
	// The user approved: the command ran and the turn finished.
	for _, l := range lines[11:19] {
		tr.apply(l)
	}
	if st, _, _ := tr.reportState(pending); st != "idle" {
		t.Fatalf("after approval: %q, want idle", st)
	}
	for _, l := range lines[19:25] {
		tr.apply(l)
	}
	if tr.status != "interrupted" || tr.details != "CAWTEST scripted failure" {
		t.Fatalf("API error: status=%q details=%q, want interrupted / CAWTEST scripted failure", tr.status, tr.details)
	}
	for _, l := range lines[25:30] {
		tr.apply(l)
	}
	if tr.status != "thinking" || tr.title != "CAWTEST:slow:150" {
		t.Fatalf("next turn: status=%q title=%q, want thinking / CAWTEST:slow:150", tr.status, tr.title)
	}
}

func TestReadCodexLinesLeavesPartialLine(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "rollout.jsonl")
	os.WriteFile(p, []byte("{\"a\":1}\n{\"b\":"), 0o644)
	lines, off, err := readCodexLines(p, 0)
	if err != nil || len(lines) != 1 || off != 8 {
		t.Fatalf("lines=%v off=%d err=%v, want 1 line and offset 8", lines, off, err)
	}
	f, _ := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0o644)
	f.WriteString("2}\n")
	f.Close()
	lines, _, _ = readCodexLines(p, off)
	if len(lines) != 1 || lines[0] != `{"b":2}` {
		t.Fatalf("completed line = %v, want {\"b\":2}", lines)
	}
}

func TestCodexOwnsSessionByLastSubmit(t *testing.T) {
	// Two Codex panes share ~/.codex/sessions. A new rollout (fresh launch,
	// /new, /resume) belongs to the pane where Enter was pressed right
	// before its turn started — not to a busy sibling whose spinner keeps
	// its PTY active.
	turnStart := time.Now().Add(-5 * time.Second).UTC()
	lines := []string{
		`{"timestamp":"` + turnStart.Add(-time.Second).Format(time.RFC3339Nano) + `","type":"session_meta","payload":{"id":"x"}}`,
		`{"timestamp":"` + turnStart.Format(time.RFC3339Nano) + `","type":"event_msg","payload":{"type":"task_started"}}`,
		`{"timestamp":"` + turnStart.Add(50*time.Millisecond).Format(time.RFC3339Nano) + `","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]}}`,
	}
	p := writeCodexTranscript(t, lines)
	if got := codexLastTurnStart(p); !got.Equal(turnStart) {
		t.Fatalf("codexLastTurnStart = %v, want %v", got, turnStart)
	}

	const busy, fresh = "codex-own-busy", "codex-own-fresh"
	agent.SetActiveSessionForTest(busy, "codex")
	agent.SetActiveSessionForTest(fresh, "codex")
	t.Cleanup(func() {
		agent.SetActiveSessionForTest(busy, "")
		agent.SetActiveSessionForTest(fresh, "")
		agent.SetPtySubmitForTest(busy, time.Time{})
		agent.SetPtySubmitForTest(fresh, time.Time{})
	})
	watchStart := turnStart.Add(-time.Minute)
	agent.SetPtySubmitForTest(busy, turnStart.Add(-40*time.Second))
	agent.SetPtySubmitForTest(fresh, turnStart.Add(-300*time.Millisecond))
	if codexOwnsSession(busy, p, watchStart) {
		t.Error("busy sibling must not own the session started from the other pane")
	}
	if !codexOwnsSession(fresh, p, watchStart) {
		t.Error("the pane where Enter was pressed must own the session")
	}
	// An Enter pressed after the turn started (e.g. answering a prompt in
	// the sibling) does not steal it.
	agent.SetPtySubmitForTest(busy, turnStart.Add(3*time.Second))
	if codexOwnsSession(busy, p, watchStart) {
		t.Error("a later Enter in a sibling must not claim an earlier turn")
	}
	// Live regression: the owner later did /new + another prompt (more
	// Enters) and left this rollout unclaimed, while the busy sibling had
	// pressed Enter shortly before this rollout's turn. The sibling must
	// still not adopt it.
	agent.SetPtySubmitForTest(busy, time.Time{})
	agent.SetPtySubmitForTest(busy, turnStart.Add(-2*time.Second))
	agent.SetPtySubmitForTest(fresh, turnStart.Add(4*time.Second))
	agent.SetPtySubmitForTest(fresh, turnStart.Add(9*time.Second))
	if codexOwnsSession(busy, p, watchStart) {
		t.Error("sibling adopted a rollout its owner left after /new")
	}
	// Nobody typed anything: only a pane that launched right before the
	// turn (prompt passed on the command line) may take it.
	agent.SetPtySubmitForTest(busy, time.Time{})
	agent.SetPtySubmitForTest(fresh, time.Time{})
	if !codexOwnsSession(fresh, p, turnStart.Add(-3*time.Second)) {
		t.Error("untyped turn right after launch should bind")
	}
	if codexOwnsSession(fresh, p, turnStart.Add(-time.Minute)) {
		t.Error("untyped turn long after launch must not bind")
	}
}
