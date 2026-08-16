package agents

import (
	"database/sql"
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

func TestCopilotTurnEndRetainsFinalAssistantResponse(t *testing.T) {
	events := []copilotEvent{
		{Type: "assistant.message", Data: rawJSON(t, copilotAssistantMsg{Content: "Completed the requested change."})},
		{Type: "assistant.turn_end"},
	}
	p := writeCopilotEvents(t, events)
	var status, details string
	(&CopilotWatcher{}).parseCopilotEvents(p, 0, func(s, _, d, _ string) {
		status, details = s, d
	})
	if status != "idle" {
		t.Fatalf("turn_end status = %q, want idle", status)
	}
	if details != "Completed the requested change." {
		t.Fatalf("turn_end details = %q, want final assistant response", details)
	}
}

func TestCopilotExitPlanModeReportsWaitingInput(t *testing.T) {
	// Exiting plan mode hands control back to the user: Copilot presents the
	// plan and blocks until the user approves or rejects it. The watcher must
	// report waiting_input (not executing/idle) while the approval is pending.
	events := []copilotEvent{
		{Type: "user.message", Data: rawJSON(t, copilotUserMsg{Content: "plan the change"})},
		{Type: "assistant.message", Data: rawJSON(t, copilotAssistantMsg{ToolRequests: []copilotToolReq{{ToolCallID: "t1", Name: "exit_plan_mode"}}})},
	}
	p := writeCopilotEvents(t, events)
	var status, tool string
	(&CopilotWatcher{}).parseCopilotEvents(p, 0, func(s, tl, d, ti string) {
		status, tool = s, tl
	})
	if status != "waiting_input" || tool != "exit_plan_mode" {
		t.Fatalf("exit_plan_mode status = (%q, %q), want (waiting_input, exit_plan_mode)", status, tool)
	}
}

func TestCopilotExecutionStartExitPlanModeWithoutMessageReportsWaitingInput(t *testing.T) {
	// The durable trace of a plan-mode exit can be just the
	// tool.execution_start event (no assistant.message tool request paired
	// with it). The watcher must still report waiting_input, not executing.
	events := []copilotEvent{
		{Type: "user.message", Data: rawJSON(t, copilotUserMsg{Content: "plan the change"})},
		{Type: "tool.execution_start", Data: rawJSON(t, copilotToolExecution{ToolCallID: "t1", ToolName: "exit_plan_mode"})},
	}
	p := writeCopilotEvents(t, events)
	var status, tool string
	(&CopilotWatcher{}).parseCopilotEvents(p, 0, func(s, tl, d, ti string) {
		status, tool = s, tl
	})
	if status != "waiting_input" || tool != "exit_plan_mode" {
		t.Fatalf("execution_start exit_plan_mode status = (%q, %q), want (waiting_input, exit_plan_mode)", status, tool)
	}
}

func TestCopilotAskUserReportsWaitingInput(t *testing.T) {
	events := []copilotEvent{
		{Type: "user.message", Data: rawJSON(t, copilotUserMsg{Content: "help me decide"})},
		{Type: "assistant.ask_user", Data: rawJSON(t, copilotAskUser{Question: "which option?"})},
	}
	p := writeCopilotEvents(t, events)
	var status, tool string
	(&CopilotWatcher{}).parseCopilotEvents(p, 0, func(s, tl, d, ti string) {
		status, tool = s, tl
	})
	if status != "waiting_input" || tool != "ask_user" {
		t.Fatalf("ask_user status = (%q, %q), want (waiting_input, ask_user)", status, tool)
	}
}

func TestCopilotSessionTitleUsesGeneratedSummary(t *testing.T) {
	dir := t.TempDir()
	sessionStorePath := filepath.Join(dir, "session-store.db")
	db, err := sql.Open("sqlite", sessionStorePath)
	if err != nil {
		t.Fatalf("open session store: %v", err)
	}
	_, err = db.Exec(`
		CREATE TABLE sessions (id TEXT PRIMARY KEY, summary TEXT);
		INSERT INTO sessions (id, summary) VALUES ('session-1', 'Generated session title');
	`)
	if err != nil {
		db.Close()
		t.Fatalf("seed session store: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close session store: %v", err)
	}

	eventsPath := filepath.Join(dir, "session-1", "events.jsonl")
	if got := copilotSessionTitle(sessionStorePath, eventsPath); got != "Generated session title" {
		t.Fatalf("session title = %q, want generated title", got)
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
