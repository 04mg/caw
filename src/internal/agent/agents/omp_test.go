package agents

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestEncodeOmpSessionDirHomeRelative(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skip("no home dir")
	}
	if resolved, err := filepath.EvalSymlinks(home); err == nil {
		home = resolved
	}

	got := encodeOmpSessionDir(filepath.Join(home, "Documents", "projects", "evm_cuda"))
	want := "-Documents-projects-evm_cuda"
	if got != want {
		t.Fatalf("home-relative encode = %q, want %q", got, want)
	}

	got = encodeOmpSessionDir(home)
	if got != "-" {
		t.Fatalf("home itself encode = %q, want %q", got, "-")
	}
}

func TestEncodeOmpSessionDirTempRelative(t *testing.T) {
	tmp := filepath.Clean(os.TempDir())
	if resolved, err := filepath.EvalSymlinks(tmp); err == nil {
		tmp = resolved
	}

	got := encodeOmpSessionDir(tmp)
	if got != "-tmp-" {
		t.Fatalf("temp root encode = %q, want %q", got, "-tmp-")
	}

	got = encodeOmpSessionDir(filepath.Join(tmp, "caw-agent-test"))
	if got != "-tmp-caw-agent-test" {
		t.Fatalf("temp child encode = %q, want %q", got, "-tmp-caw-agent-test")
	}
}

func TestEncodeOmpSessionDirLegacyAbsolute(t *testing.T) {
	// Outside home and outside os.TempDir. On macOS /tmp resolves to
	// /private/tmp and is encoded as the legacy absolute form.
	path := "/private/tmp"
	if runtime.GOOS == "windows" {
		t.Skip("legacy absolute encoding checked on unix paths")
	}
	if _, err := os.Stat(path); err != nil {
		path = "/var/empty-caw-omp-test-path"
	}

	got := encodeOmpSessionDir(path)
	if !strings.HasPrefix(got, "--") || !strings.HasSuffix(got, "--") {
		t.Fatalf("legacy encode = %q, want --...-- form", got)
	}
	if strings.Contains(got, "/") || strings.Contains(got, "\\") {
		t.Fatalf("legacy encode still contains separators: %q", got)
	}
}

func TestIsTopLevelOmpSession(t *testing.T) {
	root := filepath.Join(string(os.PathSeparator), "Users", "dev", ".omp", "agent", "sessions")
	top := filepath.Join(root, "-Documents-demo", "2026-07-25T00-00-00Z_abc.jsonl")
	nested := filepath.Join(root, "-Documents-demo", "2026-07-25T00-00-00Z_abc", "SubAgent.jsonl")
	loose := filepath.Join(root, "orphan.jsonl")

	if !isTopLevelOmpSession(root, top) {
		t.Fatalf("expected top-level session %q", top)
	}
	if isTopLevelOmpSession(root, nested) {
		t.Fatalf("nested subagent session should be rejected: %q", nested)
	}
	if isTopLevelOmpSession(root, loose) {
		t.Fatalf("file directly under sessions root should be rejected: %q", loose)
	}
}

func TestParsePiFormatTitleLine(t *testing.T) {
	title, ok := parsePiFormatTitleLine(`{"type":"title","v":1,"title":"Add omp support to caw"}`)
	if !ok || title != "Add omp support to caw" {
		t.Fatalf("title parse = (%q, %v)", title, ok)
	}
	if _, ok := parsePiFormatTitleLine(`{"type":"session","id":"x"}`); ok {
		t.Fatal("session header should not parse as title")
	}
}

func TestPiStatusForMessageAskIsWaitingInput(t *testing.T) {
	status, tool, _ := piStatusForMessage(PiMessage{
		Role:       "assistant",
		StopReason: "toolUse",
		Content: []PiBlock{
			{Type: "toolCall", Name: "ask"},
		},
	})
	if status != "waiting_input" || tool != "ask" {
		t.Fatalf("ask tool status = (%q, %q), want (waiting_input, ask)", status, tool)
	}
}

func TestPiStatusForMessageReadIsExecuting(t *testing.T) {
	status, tool, _ := piStatusForMessage(PiMessage{
		Role:       "assistant",
		StopReason: "toolUse",
		Content: []PiBlock{
			{Type: "toolCall", Name: "read"},
		},
	})
	if status != "executing" || tool != "read" {
		t.Fatalf("read tool status = (%q, %q), want (executing, read)", status, tool)
	}
}

func TestPiStatusForMessageDeveloperIsThinking(t *testing.T) {
	// omp's harness injects a "developer" role system-reminder between a
	// text-only assistant message (stopReason:stop) and a follow-up ask tool
	// call. Treating developer as thinking prevents the preceding idle from
	// firing a spurious "finished" push before the ask arrives.
	status, tool, _ := piStatusForMessage(PiMessage{Role: "developer"})
	if status != "thinking" || tool != "" {
		t.Fatalf("developer status = (%q, %q), want (thinking, \"\")", status, tool)
	}
}

func TestIsUserInputToolIncludesAsk(t *testing.T) {
	if !isUserInputTool("ask") {
		t.Fatal("ask should be treated as a user-input tool")
	}
	if isUserInputTool("bash") {
		t.Fatal("bash should not be a user-input tool")
	}
}

func TestPiStatusForMessageAbortedIsInterrupted(t *testing.T) {
	// An assistant message with stopReason "aborted" means the user cancelled
	// the turn. The watcher reports "interrupted" (not idle) for a red dot.
	status, _, _ := piStatusForMessage(PiMessage{Role: "assistant", StopReason: "aborted"})
	if status != "interrupted" {
		t.Fatalf("aborted status = %q, want interrupted", status)
	}
	status, _, _ = piStatusForMessage(PiMessage{Role: "assistant", ErrorMessage: "turn aborted by user"})
	if status != "interrupted" {
		t.Fatalf("errorMessage aborted status = %q, want interrupted", status)
	}
}

func TestPiStatusForMessageToolFailureIsToolFailed(t *testing.T) {
	// A toolResult message with isError:true carries the tool name and an
	// error text block. The watcher reports tool_failed with both.
	status, tool, details := piStatusForMessage(PiMessage{
		Role:     "toolResult",
		IsError:  true,
		ToolName: "read",
		Content: []PiBlock{
			{Type: "text", Text: "ENOENT: no such file or directory, access '/nonexistent/xyz.txt'"},
		},
	})
	if status != "tool_failed" {
		t.Fatalf("tool failure status = %q, want tool_failed", status)
	}
	if tool != "read" {
		t.Fatalf("tool = %q, want read", tool)
	}
	if !strings.Contains(details, "ENOENT") {
		t.Fatalf("details = %q, want ENOENT text", details)
	}
}

func TestPiStatusForMessageToolSuccessIsThinking(t *testing.T) {
	status, _, _ := piStatusForMessage(PiMessage{
		Role:     "toolResult",
		ToolName: "read",
		Content:  []PiBlock{{Type: "text", Text: "file contents here"}},
	})
	if status != "thinking" {
		t.Fatalf("tool success status = %q, want thinking", status)
	}
}
