package terminal

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/04mg/caw/internal/state"
)

// Pi-family agents resume the exact transcript recorded by the status watcher
// (several panes in one cwd would otherwise all reopen the latest session).
func TestResumeCmdForPiFamilyUsesExactTranscript(t *testing.T) {
	dir := t.TempDir()
	store := state.NewStore(filepath.Join(dir, "caw.db"))
	transcript := filepath.Join(dir, "2026-11-01T19-07-26-755Z_x.jsonl")
	if err := os.WriteFile(transcript, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	store.MarkAgentStarted("leaf-pi", "pi", dir)
	store.SetExternalSessionID("leaf-pi", transcript)
	if got, want := resumeCmdForAgent(store, "leaf-pi", []string{"pi"}), []string{"pi", "--session", transcript}; !reflect.DeepEqual(got, want) {
		t.Errorf("pi = %v, want %v", got, want)
	}

	store.MarkAgentStarted("leaf-omp", "omp", dir)
	store.SetExternalSessionID("leaf-omp", transcript)
	if got, want := resumeCmdForAgent(store, "leaf-omp", []string{"omp"}), []string{"omp", "--resume", transcript}; !reflect.DeepEqual(got, want) {
		t.Errorf("omp = %v, want %v", got, want)
	}

	// Missing transcript (deleted session) falls back to --continue.
	store.MarkAgentStarted("leaf-gone", "pi", dir)
	store.SetExternalSessionID("leaf-gone", filepath.Join(dir, "gone.jsonl"))
	if got, want := resumeCmdForAgent(store, "leaf-gone", []string{"pi"}), []string{"pi", "--continue"}; !reflect.DeepEqual(got, want) {
		t.Errorf("missing transcript = %v, want %v", got, want)
	}

	// Never started in this leaf: unchanged.
	if got := resumeCmdForAgent(store, "leaf-new", []string{"pi"}); !reflect.DeepEqual(got, []string{"pi"}) {
		t.Errorf("fresh leaf = %v", got)
	}
}

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
