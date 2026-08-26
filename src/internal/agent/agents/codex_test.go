package agents

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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

func TestCodexTaskCompleteWithErrorReportsToolFailed(t *testing.T) {
	// A task_complete payload carrying an error means the turn failed (e.g.
	// an API or server tool error). The watcher surfaces it as tool_failed
	// with the error message.
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
	if status != "tool_failed" {
		t.Fatalf("task error status = %q, want tool_failed", status)
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
	// event_msg "user_message"). The watcher must surface the tool name and a
	// title derived from the first real prompt.
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
		`{"type":"response_item","payload":{"type":"custom_tool_call_output"}}`,
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
	// completed prior turn.
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
	if status != "executing" {
		t.Errorf("status = %q, want executing", status)
	}
	if tool != "apply_patch" {
		t.Errorf("tool = %q, want apply_patch", tool)
	}
}