package agents

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeCopilotEvents(t *testing.T, events []copilotEvent) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "events.jsonl")
	f, err := os.Create(p)
	if err != nil {
		t.Fatalf("create events: %v", err)
	}
	defer f.Close()
	for _, ev := range events {
		b, err := json.Marshal(ev)
		if err != nil {
			t.Fatalf("marshal event: %v", err)
		}
		if _, err := f.Write(append(b, '\n')); err != nil {
			t.Fatalf("write event: %v", err)
		}
	}
	return p
}

func TestCopilotAbortReportsInterrupted(t *testing.T) {
	// Copilot writes an "abort" event when the user cancels the turn. The
	// watcher reports "interrupted" (not idle) for a red dot.
	events := []copilotEvent{
		{Type: "user.message", Data: rawJSON(t, copilotUserMsg{Content: "read /nonexistent/xyz.txt"})},
		{Type: "assistant.turn_start"},
		{Type: "abort"},
	}
	p := writeCopilotEvents(t, events)
	var status string
	(&CopilotWatcher{}).parseCopilotEvents(p, 0, func(s, tl, d, ti string) {
		status = s
	})
	if status != "interrupted" {
		t.Fatalf("abort status = %q, want interrupted", status)
	}
}

func TestCopilotSessionErrorReportsToolFailed(t *testing.T) {
	// A session.error event (e.g. quota exceeded) surfaces as tool_failed with
	// the error message so the user sees a red dot.
	events := []copilotEvent{
		{Type: "user.message", Data: rawJSON(t, copilotUserMsg{Content: "hi"})},
		{Type: "session.error", Data: rawJSON(t, copilotSessionError{Message: "You have exceeded your monthly quota", ErrorCode: "quota_exceeded"})},
	}
	p := writeCopilotEvents(t, events)
	var status, details string
	(&CopilotWatcher{}).parseCopilotEvents(p, 0, func(s, tl, d, ti string) {
		status, details = s, d
	})
	if status != "tool_failed" {
		t.Fatalf("session.error status = %q, want tool_failed", status)
	}
	if !strings.Contains(details, "quota") {
		t.Fatalf("details = %q, want quota message", details)
	}
}

func TestCopilotToolResultErrorReportsToolFailed(t *testing.T) {
	// A tool.result event with is_error:true is a failed tool call.
	events := []copilotEvent{
		{Type: "user.message", Data: rawJSON(t, copilotUserMsg{Content: "read /nonexistent/xyz.txt"})},
		{Type: "tool.result", Data: rawJSON(t, copilotToolResult{ToolCallID: "t1", IsError: true, Output: "ENOENT: no such file"})},
	}
	p := writeCopilotEvents(t, events)
	var status, details string
	(&CopilotWatcher{}).parseCopilotEvents(p, 0, func(s, tl, d, ti string) {
		status, details = s, d
	})
	if status != "tool_failed" {
		t.Fatalf("tool.result error status = %q, want tool_failed", status)
	}
	if !strings.Contains(details, "ENOENT") {
		t.Fatalf("details = %q, want ENOENT text", details)
	}
}

func TestCopilotToolResultSuccessReportsThinking(t *testing.T) {
	events := []copilotEvent{
		{Type: "tool.result", Data: rawJSON(t, copilotToolResult{ToolCallID: "t1"})},
	}
	p := writeCopilotEvents(t, events)
	var status string
	(&CopilotWatcher{}).parseCopilotEvents(p, 0, func(s, tl, d, ti string) {
		status = s
	})
	if status != "thinking" {
		t.Fatalf("tool.result success status = %q, want thinking", status)
	}
}

func rawJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}