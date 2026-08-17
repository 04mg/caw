package state

import (
	"path/filepath"
	"testing"
)

// TestSetExternalSessionIDUpsert verifies that SetExternalSessionID persists
// the external session id even when no agent_sessions row exists for the leaf
// yet. Previously it used a plain UPDATE which silently did nothing when the
// row was missing, leaving a dangling binding that a reopen could not resume.
func TestSetExternalSessionIDUpsert(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(filepath.Join(dir, "state.db"))
	defer store.Close()

	// No prior MarkAgentStarted for this leaf — no agent_sessions row exists.
	const leafID = "leaf-no-row"
	const extID = "opencode-session-123"

	store.SetExternalSessionID(leafID, extID)

	got := store.GetExternalSessionID(leafID)
	if got != extID {
		t.Fatalf("GetExternalSessionID after upsert = %q, want %q", got, extID)
	}

	// Overwrite with a new value (mid-session /new or /resume).
	const extID2 = "opencode-session-456"
	store.SetExternalSessionID(leafID, extID2)
	if got := store.GetExternalSessionID(leafID); got != extID2 {
		t.Fatalf("GetExternalSessionID after overwrite = %q, want %q", got, extID2)
	}
}
