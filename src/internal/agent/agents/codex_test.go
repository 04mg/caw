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