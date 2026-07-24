package agents

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// setupOpenCodeDB creates a temporary opencode.db with the subset of the
// session schema the watcher queries, inserts the given sessions, and returns
// the db path plus a cleanup function.
//
// Each session is described by (id, directory, timeCreatedMs, timeUpdatedMs,
// parentID). A zero parentID inserts NULL.
func setupOpenCodeDB(t *testing.T, sessions []struct {
	id              string
	directory       string
	timeCreatedMs   int64
	timeUpdatedMs   int64
	parentID        string
}) (string, func()) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "opencode.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	_, err = db.Exec(`CREATE TABLE session (
		id text PRIMARY KEY,
		directory text NOT NULL,
		parent_id text,
		title text NOT NULL,
		time_created integer NOT NULL,
		time_updated integer NOT NULL
	)`)
	if err != nil {
		db.Close()
		t.Fatalf("create session table: %v", err)
	}
	for _, s := range sessions {
		if s.parentID == "" {
			_, err = db.Exec(
				`INSERT INTO session (id, directory, parent_id, title, time_created, time_updated) VALUES (?, ?, NULL, '', ?, ?)`,
				s.id, s.directory, s.timeCreatedMs, s.timeUpdatedMs,
			)
		} else {
			_, err = db.Exec(
				`INSERT INTO session (id, directory, parent_id, title, time_created, time_updated) VALUES (?, ?, ?, '', ?, ?)`,
				s.id, s.directory, s.parentID, s.timeCreatedMs, s.timeUpdatedMs,
			)
		}
		if err != nil {
			db.Close()
			t.Fatalf("insert session %q: %v", s.id, err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}
	return dbPath, func() { _ = os.RemoveAll(dir) }
}

const (
	testCwd   = "/home/user/project"
	testAgent = "opencode"
	oldSession = "ses_old"
	newSession = "ses_new"
)

// oldTimeMs is a creation time well before any watcher starts.
var oldTimeMs = time.Now().Add(-5 * time.Minute).UnixMilli()

// freshTimeMs is a creation time at "now" (after watcherStart by definition).
var freshTimeMs = time.Now().UnixMilli()

// resetClaimRegistry clears any leftover claims between tests so they don't
// interfere with each other.
func resetClaimRegistry() {
	claimsMu.Lock()
	claims = make(map[string]map[string]bool)
	claimsMu.Unlock()
}

// TestOldSessionClaimedWhenRecentlyUpdated covers the /sessions reattach case:
// the user reattaches to a pre-existing old session inside a fresh agent
// launch, and the session's time_updated has just advanced (the reattach
// counts as an update). The old session must be claimable.
func TestOldSessionClaimedWhenRecentlyUpdated(t *testing.T) {
	resetClaimRegistry()

	dbPath, cleanup := setupOpenCodeDB(t, []struct {
		id              string
		directory       string
		timeCreatedMs   int64
		timeUpdatedMs   int64
		parentID        string
	}{
		{oldSession, testCwd, oldTimeMs, time.Now().UnixMilli(), ""},
	})
	defer cleanup()

	watcherStart := time.Now().Add(-10 * time.Second)
	got := findUnclaimedOpenCodeSession(dbPath, testCwd, watcherStart, testAgent, false)
	if got != oldSession {
		t.Fatalf("expected %q, got %q", oldSession, got)
	}
}

// TestOldSessionSkippedWhenStale ensures a fresh OpenCode launch does NOT
// claim a pre-existing old session that hasn't been touched since a previous
// Caw run (time_updated before the watcher started). This is the core fix for
// the bug where every new OpenCode instance spuriously bound to the next
// leftover session and showed its old title/status in Idle.
func TestOldSessionSkippedWhenStale(t *testing.T) {
	resetClaimRegistry()

	dbPath, cleanup := setupOpenCodeDB(t, []struct {
		id              string
		directory       string
		timeCreatedMs   int64
		timeUpdatedMs   int64
		parentID        string
	}{
		{oldSession, testCwd, oldTimeMs, oldTimeMs, ""},
	})
	defer cleanup()

	watcherStart := time.Now().Add(-10 * time.Second)
	got := findUnclaimedOpenCodeSession(dbPath, testCwd, watcherStart, testAgent, false)
	if got != "" {
		t.Fatalf("expected empty (skip stale old session on fresh launch), got %q", got)
	}
}

// TestFreshSessionClaimedRegardlessOfPtyState preserves the existing behavior
// for /new: a freshly created session (time_created after watcherStart) is
// claimable with no extra gate.
func TestFreshSessionClaimedRegardlessOfPtyState(t *testing.T) {
	resetClaimRegistry()

	dbPath, cleanup := setupOpenCodeDB(t, []struct {
		id              string
		directory       string
		timeCreatedMs   int64
		timeUpdatedMs   int64
		parentID        string
	}{
		{newSession, testCwd, freshTimeMs, freshTimeMs, ""},
	})
	defer cleanup()

	watcherStart := time.Now().Add(-10 * time.Second)
	got := findUnclaimedOpenCodeSession(dbPath, testCwd, watcherStart, testAgent, false)
	if got != newSession {
		t.Fatalf("expected %q, got %q", newSession, got)
	}
}

// TestAlreadyClaimedSessionSkipped verifies the claim registry prevents two
// watchers from binding the same session: a session already claimed by
// another watcher is skipped, and the next unclaimed candidate is returned.
func TestAlreadyClaimedSessionSkipped(t *testing.T) {
	resetClaimRegistry()

	// Pre-claim oldSession as if another watcher owns it.
	if !ClaimSession(testAgent, testCwd, oldSession) {
		t.Fatal("pre-claim failed")
	}

	dbPath, cleanup := setupOpenCodeDB(t, []struct {
		id              string
		directory       string
		timeCreatedMs   int64
		timeUpdatedMs   int64
		parentID        string
	}{
		{oldSession, testCwd, oldTimeMs, time.Now().UnixMilli(), ""},
		{newSession, testCwd, freshTimeMs, freshTimeMs, ""},
	})
	defer cleanup()

	watcherStart := time.Now().Add(-10 * time.Second)
	got := findUnclaimedOpenCodeSession(dbPath, testCwd, watcherStart, testAgent, false)
	if got != newSession {
		t.Fatalf("expected %q (skip claimed %q), got %q", newSession, oldSession, got)
	}
}