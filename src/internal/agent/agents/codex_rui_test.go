package agents

import (
	"os"
	"path/filepath"
	"testing"
	"fmt"
)

func TestCodexRequestUserInputLastEntry_WaitingInput(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	// Simulate a codex session where request_user_input is the last function_call
	// and there is NO function_call_output yet (user hasn't responded)
	f, _ := os.Create(path)
	f.WriteString(`{"type":"response_item","payload":{"type":"user_message","message":"which option?"}}` + "\n")
	f.WriteString(`{"type":"response_item","payload":{"type":"function_call","name":"request_user_input","arguments":"{}","call_id":"call_1"}}` + "\n")
	f.Close()

	w := &CodexWatcher{}
	c := &capture{}
	w.parseCodexLog(path, 0, c.cb())

	last := c.last()
	if last == nil {
		t.Fatal("expected a callback invocation")
	}
	fmt.Printf("Codex request_user_input (no output): status=%s tool=%s\n", last[0], last[1])
	if last[0] != "waiting_input" {
		t.Errorf("expected waiting_input, got %s (tool=%s)", last[0], last[1])
	}
	if last[1] != "request_user_input" {
		t.Errorf("expected tool request_user_input, got %s", last[1])
	}
}

func TestCodexRequestUserInputWithOutput_Continues(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	// After the user responds, the function_call_output is written and the
	// agent continues with another function_call (exec_command)
	f, _ := os.Create(path)
	f.WriteString(`{"type":"response_item","payload":{"type":"user_message","message":"which option?"}}` + "\n")
	f.WriteString(`{"type":"response_item","payload":{"type":"function_call","name":"request_user_input","arguments":"{}","call_id":"call_1"}}` + "\n")
	f.WriteString(`{"type":"response_item","payload":{"type":"function_call_output","call_id":"call_1","output":"{\"answers\":{}}"}}` + "\n")
	f.WriteString(`{"type":"response_item","payload":{"type":"function_call","name":"exec_command","arguments":"{}"}}` + "\n")
	f.Close()

	w := &CodexWatcher{}
	c := &capture{}
	w.parseCodexLog(path, 0, c.cb())

	last := c.last()
	if last == nil {
		t.Fatal("expected a callback invocation")
	}
	fmt.Printf("Codex after request_user_input responded: status=%s tool=%s\n", last[0], last[1])
	if last[0] != "executing" {
		t.Errorf("expected executing after user responded, got %s (tool=%s)", last[0], last[1])
	}
	if last[1] != "exec_command" {
		t.Errorf("expected tool exec_command, got %s", last[1])
	}
}
