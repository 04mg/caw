package terminal

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/04mg/caw/internal/state"
)

func TestPinClaudeSession(t *testing.T) {
	got := pinClaudeSession([]string{"claude", "--dangerously-skip-permissions"})
	if len(got) != 4 || got[2] != "--session-id" || ClaudeSessionIDFromPath("/x/"+got[3]+".jsonl") == "" {
		t.Fatalf("fresh launch not pinned: %v", got)
	}
	for _, cmd := range [][]string{
		{"claude", "--continue"},
		{"claude", "--resume", "15bf2d57-9a0d-48de-9953-9f34861a5241"},
		{"claude", "--session-id", "15bf2d57-9a0d-48de-9953-9f34861a5241"},
		{"claude", "-p", "hi"},
		{"claude", "mcp", "list"},
		{"codex"},
		{"bash"},
	} {
		if got := pinClaudeSession(cmd); !reflect.DeepEqual(got, cmd) {
			t.Errorf("pinClaudeSession(%v) = %v, want unchanged", cmd, got)
		}
	}
}

func TestResumeCmdForClaudeUsesExactSession(t *testing.T) {
	store := state.NewStore(filepath.Join(t.TempDir(), "state.db"))
	const leaf = "leaf-claude"
	base := []string{"claude", "--dangerously-skip-permissions"}
	if got := resumeCmdForAgent(store, leaf, base); !reflect.DeepEqual(got, base) {
		t.Fatalf("first launch must be untouched, got %v", got)
	}
	store.MarkAgentStarted(leaf, "claude", "/w")
	if got := resumeCmdForAgent(store, leaf, append([]string(nil), base...)); got[len(got)-1] != "--continue" {
		t.Fatalf("reopen without a bound transcript should --continue, got %v", got)
	}
	id := "15bf2d57-9a0d-48de-9953-9f34861a5241"
	store.SetExternalSessionID(leaf, "/home/u/.claude/projects/-w/"+id+".jsonl")
	want := append(append([]string(nil), base...), "--resume", id)
	if got := resumeCmdForAgent(store, leaf, append([]string(nil), base...)); !reflect.DeepEqual(got, want) {
		t.Fatalf("reopen = %v, want %v", got, want)
	}
}

func TestSanitizeChildEnvDropsNestedAgentMarkers(t *testing.T) {
	in := []string{"PATH=/bin", "CLAUDECODE=1", "CLAUDE_CODE_CHILD_SESSION=1", "CLAUDE_CODE_ENTRYPOINT=cli",
		"CLAUDE_CODE_OAUTH_TOKEN=tok", "HOME=/h"}
	want := []string{"PATH=/bin", "CLAUDE_CODE_OAUTH_TOKEN=tok", "HOME=/h"}
	if got := sanitizeChildEnv(in); !reflect.DeepEqual(got, want) {
		t.Fatalf("sanitizeChildEnv = %v, want %v", got, want)
	}
}
