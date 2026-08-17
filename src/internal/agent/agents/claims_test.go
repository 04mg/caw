package agents

import "testing"

func resetClaims() {
	claimsMu.Lock()
	claims = make(map[string]map[string]claim)
	claimsMu.Unlock()
}

// TestClaimSessionForLeafRefusesSteal verifies the ownership guarantee: a
// native session already claimed by one leaf cannot be claimed by a different
// leaf. This is the core fix for two OpenCode panes in one cwd swapping their
// bound sessions (and thus their statuses/titles).
func TestClaimSessionForLeafRefusesSteal(t *testing.T) {
	resetClaims()
	const agentID = "opencode"
	const cwd = "/home/user/project"
	const key = "session-a"

	if !ClaimSessionForLeaf(agentID, cwd, key, "leaf-1") {
		t.Fatal("first leaf should win the claim")
	}
	// A second, different leaf must NOT be able to steal the session.
	if ClaimSessionForLeaf(agentID, cwd, key, "leaf-2") {
		t.Fatal("a different leaf must not steal an owned session")
	}
	// The owning leaf may re-claim its own session (idempotent, covers rebind).
	if !ClaimSessionForLeaf(agentID, cwd, key, "leaf-1") {
		t.Fatal("the owning leaf should be able to re-claim its own session")
	}
}

// TestClaimSessionForLeafAllowsDistinctKeys verifies two leaves in the same
// group can each own a distinct session without interference.
func TestClaimSessionForLeafAllowsDistinctKeys(t *testing.T) {
	resetClaims()
	const agentID = "opencode"
	const cwd = "/home/user/project"

	if !ClaimSessionForLeaf(agentID, cwd, "session-a", "leaf-1") {
		t.Fatal("leaf-1 should claim session-a")
	}
	if !ClaimSessionForLeaf(agentID, cwd, "session-b", "leaf-2") {
		t.Fatal("leaf-2 should claim session-b")
	}
}

// TestClaimSessionLegacyStillWorks ensures the leaf-less claim API preserves
// its original behavior (returns false when already claimed).
func TestClaimSessionLegacyStillWorks(t *testing.T) {
	resetClaims()
	const agentID = "opencode"
	const cwd = "/home/user/project"
	const key = "session-a"

	if !ClaimSession(agentID, cwd, key) {
		t.Fatal("first claim should succeed")
	}
	if ClaimSession(agentID, cwd, key) {
		t.Fatal("second claim of same key should fail")
	}
}

// TestClaimedByReportsOwner verifies the ownership registry reports which leaf
// owns a claim, and returns "" for unclaimed keys.
func TestClaimedByReportsOwner(t *testing.T) {
	resetClaims()
	const agentID = "opencode"
	const cwd = "/home/user/project"

	if got := ClaimedBy(agentID, cwd, "session-a"); got != "" {
		t.Fatalf("unclaimed key should report no owner, got %q", got)
	}
	if !ClaimSessionForLeaf(agentID, cwd, "session-a", "leaf-1") {
		t.Fatal("first leaf should win the claim")
	}
	if got := ClaimedBy(agentID, cwd, "session-a"); got != "leaf-1" {
		t.Fatalf("expected owner leaf-1, got %q", got)
	}
}
