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
	(&CopilotWatcher{}).parseCopilotEvents(p, 0, &copilotWatchState{}, func(s, tl, d, ti string) {
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
	(&CopilotWatcher{}).parseCopilotEvents(p, 0, &copilotWatchState{}, func(s, tl, d, ti string) {
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
	(&CopilotWatcher{}).parseCopilotEvents(p, 0, &copilotWatchState{}, func(s, tl, d, ti string) {
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
	(&CopilotWatcher{}).parseCopilotEvents(p, 0, &copilotWatchState{}, func(s, tl, d, ti string) {
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
	(&CopilotWatcher{}).parseCopilotEvents(p, 0, &copilotWatchState{}, func(s, _, d, _ string) {
		status, details = s, d
	})
	if status != "idle" {
		t.Fatalf("turn_end status = %q, want idle", status)
	}
	if details != "Completed the requested change." {
		t.Fatalf("turn_end details = %q, want final assistant response", details)
	}
}

func TestCopilotTurnEndWithActiveSubagentStaysWorking(t *testing.T) {
	// Copilot delegates to a subagent, then the parent finishes its turn. This
	// is not the end of the whole session: the subagent is still running, so
	// reporting idle would fire a spurious "finished" notification.
	events := []copilotEvent{
		{Type: "user.message", Data: rawJSON(t, copilotUserMsg{Content: "implement multi-account support"})},
		{Type: "subagent.started", Data: rawJSON(t, copilotSubagentEvent{ToolCallID: "toolu_1", AgentName: "general-purpose"})},
		{Type: "assistant.turn_end", Data: rawJSON(t, copilotTurnEnd{TurnID: "1"})},
	}
	p := writeCopilotEvents(t, events)
	var status, tool string
	state := &copilotWatchState{}
	(&CopilotWatcher{}).parseCopilotEvents(p, 0, state, func(s, tl, d, ti string) {
		status, tool = s, tl
	})
	if status != "thinking" || tool != "" {
		t.Fatalf("active subagent turn_end status = (%q, %q), want (thinking, \"\")", status, tool)
	}
}

func TestCopilotTurnEndAfterSubagentCompletedReportsIdle(t *testing.T) {
	// Once the delegated subagent finishes, the parent's next turn_end is the
	// real end of the task and should be reported as idle.
	events := []copilotEvent{
		{Type: "subagent.started", Data: rawJSON(t, copilotSubagentEvent{ToolCallID: "toolu_1"})},
		{Type: "assistant.turn_end", Data: rawJSON(t, copilotTurnEnd{TurnID: "1"})},
		{Type: "subagent.completed", Data: rawJSON(t, copilotSubagentEvent{ToolCallID: "toolu_1"})},
		{Type: "assistant.turn_end", Data: rawJSON(t, copilotTurnEnd{TurnID: "2"})},
	}
	p := writeCopilotEvents(t, events)
	var status string
	state := &copilotWatchState{}
	(&CopilotWatcher{}).parseCopilotEvents(p, 0, state, func(s, tl, d, ti string) {
		status = s
	})
	if status != "idle" {
		t.Fatalf("completed subagent turn_end status = %q, want idle", status)
	}
}

func TestCopilotSubagentStateSurvivesIncrementalReads(t *testing.T) {
	// The subagent lifecycle can span multiple reads of events.jsonl. The
	// watcher must carry the "started" state across calls so a turn_end in a
	// later chunk still knows the subagent is active.
	startEvents := []copilotEvent{
		{Type: "subagent.started", Data: rawJSON(t, copilotSubagentEvent{ToolCallID: "toolu_1"})},
		{Type: "assistant.turn_end", Data: rawJSON(t, copilotTurnEnd{TurnID: "1"})},
	}
	p := writeCopilotEvents(t, startEvents)
	state := &copilotWatchState{}
	var status string
	(&CopilotWatcher{}).parseCopilotEvents(p, 0, state, func(s, tl, d, ti string) {
		status = s
	})
	if status != "thinking" {
		t.Fatalf("first chunk status = %q, want thinking", status)
	}

	// Append the completion + final turn_end, then read only the new bytes.
	finishEvents := []copilotEvent{
		{Type: "subagent.completed", Data: rawJSON(t, copilotSubagentEvent{ToolCallID: "toolu_1"})},
		{Type: "assistant.turn_end", Data: rawJSON(t, copilotTurnEnd{TurnID: "2"})},
	}
	f, err := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("open events for append: %v", err)
	}
	var offset int64
	info, _ := f.Stat()
	offset = info.Size()
	for _, ev := range finishEvents {
		b, _ := json.Marshal(ev)
		if _, err := f.Write(append(b, '\n')); err != nil {
			t.Fatalf("append event: %v", err)
		}
	}
	f.Close()

	status = ""
	(&CopilotWatcher{}).parseCopilotEvents(p, offset, state, func(s, tl, d, ti string) {
		status = s
	})
	if status != "idle" {
		t.Fatalf("second chunk status = %q, want idle", status)
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
	(&CopilotWatcher{}).parseCopilotEvents(p, 0, &copilotWatchState{}, func(s, tl, d, ti string) {
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
	(&CopilotWatcher{}).parseCopilotEvents(p, 0, &copilotWatchState{}, func(s, tl, d, ti string) {
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
	(&CopilotWatcher{}).parseCopilotEvents(p, 0, &copilotWatchState{}, func(s, tl, d, ti string) {
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
