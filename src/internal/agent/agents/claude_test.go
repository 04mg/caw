package agents

import (
	"os"
	"path/filepath"
	"testing"
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