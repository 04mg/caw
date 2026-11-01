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
