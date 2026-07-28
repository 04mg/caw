package terminal

import (
	"testing"
	"time"
)

// TestReconcileOrphansKillsUnknownLeaf creates sessions, then calls
// doReconcileOrphans with a leaf set that excludes one session. The
// excluded session (with no viewers) should be killed via Delete; the
// included session should survive.
func TestReconcileOrphansKillsUnknownLeaf(t *testing.T) {
	mgr := NewSessionManager(nil, nil)

	// Start two shell sessions — one that stays in the layout, one that
	// is orphaned.
	keepID, err := mgr.Create(CreateRequest{
		Cwd: t.TempDir(),
		Cmd: []string{"sleep", "30"},
	})
	if err != nil {
		t.Fatalf("create keep session: %v", err)
	}
	orphanID, err := mgr.Create(CreateRequest{
		Cwd: t.TempDir(),
		Cmd: []string{"sleep", "30"},
	})
	if err != nil {
		t.Fatalf("create orphan session: %v", err)
	}

	// Both sessions should exist.
	if _, ok := mgr.Get(keepID); !ok {
		t.Fatal("keep session missing before reconcile")
	}
	if _, ok := mgr.Get(orphanID); !ok {
		t.Fatal("orphan session missing before reconcile")
	}

	// Reconcile with only keepID in the known set. Neither session has
	// viewers, so orphanID should be killed.
	mgr.doReconcileOrphans(map[string]bool{keepID: true})

	// Give the kill + onExit goroutine a moment to run.
	deadline := time.Now().Add(2 * time.Second)
	for {
		_, orphanOK := mgr.Get(orphanID)
		if !orphanOK {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("orphan session still alive after reconcile")
		}
		time.Sleep(50 * time.Millisecond)
	}

	if _, ok := mgr.Get(keepID); !ok {
		t.Fatal("keep session was killed, should have survived")
	}

	// Cleanup.
	mgr.Delete(keepID, false)
}

// TestReconcileOrphansSparesSessionWithViewers verifies that a session
// with connected WebSocket viewers is NOT killed even if its leaf is
// absent from the layout. This protects the multi-client scenario where
// another browser is actively viewing the pane.
func TestReconcileOrphansSparesSessionWithViewers(t *testing.T) {
	mgr := NewSessionManager(nil, nil)

	orphanID, err := mgr.Create(CreateRequest{
		Cwd: t.TempDir(),
		Cmd: []string{"sleep", "30"},
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	defer mgr.Delete(orphanID, false)

	// Simulate a connected viewer by adding a fake connWriter to the
	// session's conns map.
	sess, ok := mgr.Get(orphanID)
	if !ok {
		t.Fatal("session missing")
	}
	cw := &connWriter{}
	sess.mu.Lock()
	sess.conns[cw] = true
	sess.mu.Unlock()

	// Reconcile with an empty known-leaf set — the session is orphaned
	// but has a viewer, so it must survive.
	mgr.doReconcileOrphans(map[string]bool{})

	if _, ok := mgr.Get(orphanID); !ok {
		t.Fatal("session with viewers was killed")
	}

	// Cleanup.
	sess.mu.Lock()
	delete(sess.conns, cw)
	sess.mu.Unlock()
}

// TestReconcileOrphansEmptySetKillsAll verifies that an empty known-leaf
// set kills all viewerless sessions.
func TestReconcileOrphansEmptySetKillsAll(t *testing.T) {
	mgr := NewSessionManager(nil, nil)

	id, err := mgr.Create(CreateRequest{
		Cwd: t.TempDir(),
		Cmd: []string{"sleep", "30"},
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	mgr.doReconcileOrphans(map[string]bool{})

	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, ok := mgr.Get(id); !ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("session still alive after reconcile with empty set")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// TestScheduleReconcileDebounces verifies that rapid successive calls to
// scheduleReconcile only result in a single reconciliation pass using the
// last known-leaf set.
func TestScheduleReconcileDebounces(t *testing.T) {
	mgr := NewSessionManager(nil, nil)

	keepID, err := mgr.Create(CreateRequest{
		Cwd: t.TempDir(),
		Cmd: []string{"sleep", "30"},
	})
	if err != nil {
		t.Fatalf("create keep: %v", err)
	}
	orphanID, err := mgr.Create(CreateRequest{
		Cwd: t.TempDir(),
		Cmd: []string{"sleep", "30"},
	})
	if err != nil {
		t.Fatalf("create orphan: %v", err)
	}
	defer mgr.Delete(keepID, false)

	// Fire three rapid reconcile calls with different leaf sets. The
	// final one includes keepID but not orphanID.
	mgr.scheduleReconcile(map[string]bool{keepID: true, orphanID: true})
	mgr.scheduleReconcile(map[string]bool{keepID: true, orphanID: true})
	mgr.scheduleReconcile(map[string]bool{keepID: true})

	// Wait for the debounce + kill to settle.
	deadline := time.Now().Add(reconcileDebounce + 2*time.Second)
	for {
		_, ok := mgr.Get(orphanID)
		if !ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("orphan still alive after debounce window")
		}
		time.Sleep(100 * time.Millisecond)
	}

	if _, ok := mgr.Get(keepID); !ok {
		t.Fatal("keep session killed after debounced reconcile")
	}
}
