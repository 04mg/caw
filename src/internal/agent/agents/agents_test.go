package agents

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeFile writes content to path and returns the path.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

// appendFile appends content to path.
func appendFile(t *testing.T, path, content string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()
	if _, err := f.WriteString(content); err != nil {
		t.Fatalf("write: %v", err)
	}
}

// capture collects all callback invocations.
type capture struct {
	calls [][]string // [status, tool, details, title]
}

func (c *capture) cb() func(status, tool, details, title string) {
	return func(status, tool, details, title string) {
		c.calls = append(c.calls, []string{status, tool, details, title})
	}
}

func (c *capture) last() []string {
	if len(c.calls) == 0 {
		return nil
	}
	return c.calls[len(c.calls)-1]
}

// ----- Claude tests -----

func TestClaudeAskUserQuestion_WaitingInput(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	writeFile(t, path, `{"type":"user","message":{"role":"user","content":"fix a bug"}}`+"\n")
	appendFile(t, path, `{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","name":"AskUserQuestion","input":{}}]}}`+"\n")

	w := &ClaudeWatcher{}
	c := &capture{}
	w.parseClaudeLog(path, 0, c.cb())

	last := c.last()
	if last == nil {
		t.Fatal("expected a callback invocation")
	}
	if last[0] != "waiting_input" {
		t.Errorf("expected waiting_input, got %s (tool=%s)", last[0], last[1])
	}
	if last[1] != "AskUserQuestion" {
		t.Errorf("expected tool AskUserQuestion, got %s", last[1])
	}
}

func TestClaudeBashTool_Executing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	writeFile(t, path, `{"type":"user","message":{"role":"user","content":"run ls"}}`+"\n")
	appendFile(t, path, `{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","name":"Bash","input":{}}]}}`+"\n")

	w := &ClaudeWatcher{}
	c := &capture{}
	w.parseClaudeLog(path, 0, c.cb())

	last := c.last()
	if last == nil {
		t.Fatal("expected a callback invocation")
	}
	if last[0] != "executing" {
		t.Errorf("expected executing, got %s (tool=%s)", last[0], last[1])
	}
	if last[1] != "Bash" {
		t.Errorf("expected tool Bash, got %s", last[1])
	}
}

func TestClaudeInterrupted_Idle(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	writeFile(t, path, `{"type":"user","message":{"role":"user","content":"run something"}}`+"\n")
	appendFile(t, path, `{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","name":"Bash","input":{}}]}}`+"\n")
	appendFile(t, path, `{"type":"user","message":{"role":"user","content":[{"type":"tool_result","content":"interrupted"}]}}`+"\n")
	appendFile(t, path, `{"type":"user","message":{"role":"user","content":[{"type":"text","text":"[Request interrupted by user for tool use]"}]}}`+"\n")

	w := &ClaudeWatcher{}
	c := &capture{}
	w.parseClaudeLog(path, 0, c.cb())

	last := c.last()
	if last == nil {
		t.Fatal("expected a callback invocation")
	}
	if last[0] != "idle" {
		t.Errorf("expected idle after interrupt, got %s", last[0])
	}
}

func TestClaudeResult_Idle(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	writeFile(t, path, `{"type":"user","message":{"role":"user","content":"hello"}}`+"\n")
	appendFile(t, path, `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"done"}]}}`+"\n")
	appendFile(t, path, `{"type":"result","subtype":"success"}`+"\n")

	w := &ClaudeWatcher{}
	c := &capture{}
	w.parseClaudeLog(path, 0, c.cb())

	last := c.last()
	if last == nil {
		t.Fatal("expected a callback invocation")
	}
	if last[0] != "idle" {
		t.Errorf("expected idle after result, got %s", last[0])
	}
}

// ----- Codex tests -----

func TestCodexRequestUserInput_WaitingInput(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	writeFile(t, path, `{"type":"response_item","payload":{"type":"user_message","message":"fix a bug"}}`+"\n")
	appendFile(t, path, `{"type":"response_item","payload":{"type":"function_call","name":"request_user_input","arguments":"{}"}}`+"\n")

	w := &CodexWatcher{}
	c := &capture{}
	w.parseCodexLog(path, 0, c.cb())

	last := c.last()
	if last == nil {
		t.Fatal("expected a callback invocation")
	}
	if last[0] != "waiting_input" {
		t.Errorf("expected waiting_input, got %s (tool=%s)", last[0], last[1])
	}
	if last[1] != "request_user_input" {
		t.Errorf("expected tool request_user_input, got %s", last[1])
	}
}

func TestCodexExecCommand_Executing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	writeFile(t, path, `{"type":"response_item","payload":{"type":"user_message","message":"run ls"}}`+"\n")
	appendFile(t, path, `{"type":"response_item","payload":{"type":"function_call","name":"exec_command","arguments":"{}"}}`+"\n")

	w := &CodexWatcher{}
	c := &capture{}
	w.parseCodexLog(path, 0, c.cb())

	last := c.last()
	if last == nil {
		t.Fatal("expected a callback invocation")
	}
	if last[0] != "executing" {
		t.Errorf("expected executing, got %s (tool=%s)", last[0], last[1])
	}
}

func TestCodexTaskComplete_Idle(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	writeFile(t, path, `{"type":"response_item","payload":{"type":"user_message","message":"hello"}}`+"\n")
	appendFile(t, path, `{"type":"response_item","payload":{"type":"agent_message","message":"done","phase":"final_answer"}}`+"\n")

	w := &CodexWatcher{}
	c := &capture{}
	w.parseCodexLog(path, 0, c.cb())

	last := c.last()
	if last == nil {
		t.Fatal("expected a callback invocation")
	}
	if last[0] != "idle" {
		t.Errorf("expected idle after final_answer, got %s", last[0])
	}
}

// ----- Antigravity tests -----

func TestAntigravityBackgroundTaskCancel_Idle(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "transcript.jsonl")
	writeFile(t, path, `{"step_index":0,"source":"USER_EXPLICIT","type":"USER_INPUT","status":"DONE","content":"build the project"}`+"\n")
	appendFile(t, path, `{"step_index":60,"source":"MODEL","type":"RUN_COMMAND","status":"RUNNING","content":"Tool is running as a background task with task id: conv-id/task-60\nTask Description: make build"}`+"\n")
	// Timer task started
	appendFile(t, path, `{"step_index":68,"source":"MODEL","type":"GENERIC","status":"RUNNING","content":"Tool is running as a background task with task id: conv-id/task-68\nTask Description: Timer"}`+"\n")
	// Build task finished
	appendFile(t, path, `{"step_index":77,"source":"SYSTEM","type":"SYSTEM_MESSAGE","status":"DONE","content":"[Message] sender=conv-id/task-60 content=Task id \"conv-id/task-60\" finished with result: success"}`+"\n")
	// Timer task cancelled (no "finished" keyword — sender= field has the task id)
	appendFile(t, path, `{"step_index":78,"source":"SYSTEM","type":"SYSTEM_MESSAGE","status":"DONE","content":"[Message] timestamp=2026-07-13T02:11:58Z sender=conv-id/task-68 priority=MESSAGE_PRIORITY_LOW content=Your scheduled timer was cancelled because you received another message."}`+"\n")
	// Planner response with no tool calls — should be idle since no running tasks
	appendFile(t, path, `{"step_index":79,"source":"MODEL","type":"PLANNER_RESPONSE","status":"DONE","content":"Build finished successfully"}`+"\n")

	w := &AntigravityWatcher{}
	c := &capture{}
	w.parseAntigravityLog(path, 0, c.cb())

	last := c.last()
	if last == nil {
		t.Fatal("expected a callback invocation")
	}
	if last[0] != "idle" {
		t.Errorf("expected idle after all tasks done, got %s (tool=%s details=%s)", last[0], last[1], last[2])
	}
}

func TestAntigravityBackgroundTaskRunning_Executing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "transcript.jsonl")
	writeFile(t, path, `{"step_index":0,"source":"USER_EXPLICIT","type":"USER_INPUT","status":"DONE","content":"build the project"}`+"\n")
	appendFile(t, path, `{"step_index":60,"source":"MODEL","type":"RUN_COMMAND","status":"RUNNING","content":"Tool is running as a background task with task id: conv-id/task-60\nTask Description: make build"}`+"\n")
	// Planner response with no tool calls — should be executing since task is still running
	appendFile(t, path, `{"step_index":61,"source":"MODEL","type":"PLANNER_RESPONSE","status":"DONE","content":"Build is running, waiting for completion"}`+"\n")

	w := &AntigravityWatcher{}
	c := &capture{}
	w.parseAntigravityLog(path, 0, c.cb())

	last := c.last()
	if last == nil {
		t.Fatal("expected a callback invocation")
	}
	if last[0] != "executing" {
		t.Errorf("expected executing while task running, got %s (tool=%s details=%s)", last[0], last[1], last[2])
	}
	if last[1] != "background_task" {
		t.Errorf("expected tool background_task, got %s", last[1])
	}
}

func TestAntigravityGenericStatusDone_RemovesTask(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "transcript.jsonl")
	writeFile(t, path, `{"step_index":0,"source":"USER_EXPLICIT","type":"USER_INPUT","status":"DONE","content":"build the project"}`+"\n")
	appendFile(t, path, `{"step_index":60,"source":"MODEL","type":"RUN_COMMAND","status":"RUNNING","content":"Tool is running as a background task with task id: conv-id/task-60\nTask Description: make build"}`+"\n")
	// GENERIC step reporting task status DONE
	appendFile(t, path, `{"step_index":118,"source":"MODEL","type":"GENERIC","status":"DONE","content":"Task: conv-id/task-60\nStatus: DONE\nLog: /path/to/log"}`+"\n")
	// Planner response with no tool calls — should be idle since task-60 is now DONE
	appendFile(t, path, `{"step_index":119,"source":"MODEL","type":"PLANNER_RESPONSE","status":"DONE","content":"Build finished"}`+"\n")

	w := &AntigravityWatcher{}
	c := &capture{}
	w.parseAntigravityLog(path, 0, c.cb())

	last := c.last()
	if last == nil {
		t.Fatal("expected a callback invocation")
	}
	if last[0] != "idle" {
		t.Errorf("expected idle after GENERIC Status: DONE, got %s (tool=%s details=%s)", last[0], last[1], last[2])
	}
}

func TestAntigravityAskQuestion_WaitingInput(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "transcript.jsonl")
	writeFile(t, path, `{"step_index":0,"source":"USER_EXPLICIT","type":"USER_INPUT","status":"DONE","content":"do something"}`+"\n")
	appendFile(t, path, `{"step_index":1,"source":"MODEL","type":"PLANNER_RESPONSE","status":"DONE","content":"asking a question","tool_calls":[{"name":"ASK_QUESTION","args":{}}]}`+"\n")

	w := &AntigravityWatcher{}
	c := &capture{}
	w.parseAntigravityLog(path, 0, c.cb())

	last := c.last()
	if last == nil {
		t.Fatal("expected a callback invocation")
	}
	if last[0] != "waiting_input" {
		t.Errorf("expected waiting_input for ASK_QUESTION, got %s (tool=%s)", last[0], last[1])
	}
}

// ----- Copilot tests -----

func TestCopilotAskUser_WaitingInput(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")
	writeFile(t, path, `{"type":"session.start","data":{"context":{"cwd":"/root/caw"}}}`+"\n")
	appendFile(t, path, `{"type":"user.message","data":{"content":"do something"}}`+"\n")
	appendFile(t, path, `{"type":"assistant.ask_user","data":{"question":"which option?"}}`+"\n")

	w := &CopilotWatcher{}
	c := &capture{}
	w.parseCopilotEvents(path, 0, c.cb())

	last := c.last()
	if last == nil {
		t.Fatal("expected a callback invocation")
	}
	if last[0] != "waiting_input" {
		t.Errorf("expected waiting_input, got %s (tool=%s)", last[0], last[1])
	}
	if last[1] != "ask_user" {
		t.Errorf("expected tool ask_user, got %s", last[1])
	}
}

// Ensure time import is used.
var _ = time.Second