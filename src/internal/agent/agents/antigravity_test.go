package agents

import (
	"os"
	"path/filepath"
	"testing"
)

func writeAntigravityTranscript(t *testing.T, lines []string) string {
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

func TestAntigravityInterruptReportsInterrupted(t *testing.T) {
	// Antigravity surfaces a user interrupt as a SYSTEM_MESSAGE whose content
	// references the planner being "interrupted". The watcher reports
	// "interrupted" (not idle) for a red dot.
	lines := []string{
		`{"type":"USER_INPUT","content":"read /nonexistent/xyz.txt"}`,
		`{"type":"PLANNER_RESPONSE","content":"","tool_calls":[{"name":"view_file"}]}`,
		`{"type":"SYSTEM_MESSAGE","content":"The operation was interrupted by the user"}`,
	}
	p := writeAntigravityTranscript(t, lines)
	var status string
	(&AntigravityWatcher{}).parseAntigravityLog(p, 0, func(s, tl, d, ti string) {
		status = s
	})
	if status != "interrupted" {
		t.Fatalf("interrupt status = %q, want interrupted", status)
	}
}

func TestAntigravityToolFailureReportsToolFailed(t *testing.T) {
	// A tool step (RUN_COMMAND) whose content carries an error marker is a
	// failed tool call. The watcher surfaces it as tool_failed.
	lines := []string{
		`{"type":"USER_INPUT","content":"list files"}`,
		`{"type":"PLANNER_RESPONSE","content":"","tool_calls":[{"name":"run_command"}]}`,
		`{"type":"RUN_COMMAND","content":"Error: command failed: No such file or directory","status":"DONE"}`,
	}
	p := writeAntigravityTranscript(t, lines)
	var status, tool string
	(&AntigravityWatcher{}).parseAntigravityLog(p, 0, func(s, tl, d, ti string) {
		status, tool = s, tl
	})
	if status != "tool_failed" {
		t.Fatalf("tool failure status = %q, want tool_failed", status)
	}
	if tool != "run_command" {
		t.Fatalf("tool = %q, want run_command", tool)
	}
}

func TestAntigravityNormalIdleStillIdle(t *testing.T) {
	// A PLANNER_RESPONSE with no tool calls is a final answer → idle, even
	// though the watcher now scans the last step for error markers.
	lines := []string{
		`{"type":"USER_INPUT","content":"hi"}`,
		`{"type":"PLANNER_RESPONSE","content":"hello there","tool_calls":[]}`,
	}
	p := writeAntigravityTranscript(t, lines)
	var status string
	(&AntigravityWatcher{}).parseAntigravityLog(p, 0, func(s, tl, d, ti string) {
		status = s
	})
	if status != "idle" {
		t.Fatalf("final answer status = %q, want idle", status)
	}
}

func TestAntigravityNoBackgroundTasksClearsStaleTask(t *testing.T) {
	// Antigravity's manage_task command can report that its task list is empty
	// without first sending a completion message for each scheduled task.
	lines := []string{
		`{"type":"USER_INPUT","content":"build the project"}`,
		`{"type":"RUN_COMMAND","status":"RUNNING","content":"Tool is running as a background task with task id: session/task-32"}`,
		`{"type":"GENERIC","status":"RUNNING","content":"Tool is running as a background task with task id: session/task-34"}`,
		`{"type":"SYSTEM_MESSAGE","status":"DONE","content":"Task id \"session/task-32\" finished with result"}`,
		`{"type":"GENERIC","status":"DONE","content":"No background tasks are currently running."}`,
		`{"type":"PLANNER_RESPONSE","status":"DONE","content":"The build completed successfully.","tool_calls":[]}`,
	}
	p := writeAntigravityTranscript(t, lines)
	var status string
	(&AntigravityWatcher{}).parseAntigravityLog(p, 0, func(s, tl, d, ti string) {
		status = s
	})
	if status != "idle" {
		t.Fatalf("status = %q, want idle", status)
	}
}
