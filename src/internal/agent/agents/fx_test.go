package agents

import (
	"os"
	"path/filepath"
	"testing"
)

// TestFxParseLogAskUserQuestionMapsToWaitingInput guards the core mapping:
// a recovery checkpoint whose last tool call is ask_user_question must be
// reported as waiting_input (the "needs input" Kanban column).
func TestFxParseLogAskUserQuestionMapsToWaitingInput(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")
	line := `{"kind":"recovery_checkpoint_set","seq":1,"timestamp_ms":1,"payload":{"checkpoint":{"version":1,"turn_id":1,"execution":{"schema_version":3,"tool_steps":[{"tool_calls":[{"id":"c1","name":"ask_user_question","arguments_json":"{}"}],"tool_results":[]}]}}}}` + "\n"
	if err := os.WriteFile(path, []byte(line), 0o600); err != nil {
		t.Fatalf("write events: %v", err)
	}

	var gotStatus, gotTool string
	w := &FxWatcher{}
	w.parseFxLog(path, 0, func(status, tool, details, title string) {
		gotStatus = status
		gotTool = tool
	})

	if gotStatus != "waiting_input" {
		t.Fatalf("status = %q, want waiting_input", gotStatus)
	}
	if gotTool != "ask_user_question" {
		t.Fatalf("tool = %q, want ask_user_question", gotTool)
	}
}

// TestFxParseLogPausedMapsToWaitingInput verifies the paused-recovery
// checkpoint (provider exhausted retries) also maps to waiting_input.
func TestFxParseLogPausedMapsToWaitingInput(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")
	line := `{"kind":"recovery_checkpoint_set","seq":1,"timestamp_ms":1,"payload":{"checkpoint":{"version":1,"turn_id":1,"action":"paused","execution":{"schema_version":3,"tool_steps":[{"tool_calls":[{"id":"c1","name":"terminal","arguments_json":"{}"}],"tool_results":[]}]}}}}` + "\n"
	if err := os.WriteFile(path, []byte(line), 0o600); err != nil {
		t.Fatalf("write events: %v", err)
	}

	var gotStatus string
	w := &FxWatcher{}
	w.parseFxLog(path, 0, func(status, tool, details, title string) {
		gotStatus = status
	})

	if gotStatus != "waiting_input" {
		t.Fatalf("status = %q, want waiting_input", gotStatus)
	}
}
