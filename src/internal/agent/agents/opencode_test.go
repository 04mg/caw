package agents

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
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
// Computed per call rather than at package init: tests that run long (e.g. a
// watcher test polling on a multi-second tick) must not age a package-level
// constant out of the fresh window, or an unrelated later test would see its
// "fresh" session as stale and fail to claim it.
func freshTimeMs() int64 { return time.Now().UnixMilli() }

// resetClaimRegistry clears any leftover claims between tests so they don't
// interfere with each other.
func resetClaimRegistry() {
	claimsMu.Lock()
	claims = make(map[string]map[string]bool)
	claimsMu.Unlock()
}

// setupOpenCodeDBWithMessages builds a temp opencode.db with the session,
// message, and part tables (the subset the watcher queries), inserts the given
// sessions, messages, and parts, and returns the db path + cleanup.
//
// A message is (id, sessionID, timeCreatedMs, dataJSON). A part is
// (id, messageID, sessionID, timeCreatedMs, dataJSON). The session schema
// matches setupOpenCodeDB so findUnclaimedOpenCodeSession still works.
func setupOpenCodeDBWithMessages(t *testing.T, sessions []struct {
	id              string
	directory       string
	timeCreatedMs   int64
	timeUpdatedMs   int64
	parentID        string
}, messages []struct {
	id            string
	sessionID     string
	timeCreatedMs int64
	dataJSON      string
}, parts []struct {
	id            string
	messageID     string
	sessionID     string
	timeCreatedMs int64
	dataJSON      string
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
	_, err = db.Exec(`CREATE TABLE message (
		id text PRIMARY KEY,
		session_id text NOT NULL,
		time_created integer NOT NULL,
		time_updated integer NOT NULL,
		data text NOT NULL
	)`)
	if err != nil {
		db.Close()
		t.Fatalf("create message table: %v", err)
	}
	_, err = db.Exec(`CREATE TABLE part (
		id text PRIMARY KEY,
		message_id text NOT NULL,
		session_id text NOT NULL,
		time_created integer NOT NULL,
		time_updated integer NOT NULL,
		data text NOT NULL
	)`)
	if err != nil {
		db.Close()
		t.Fatalf("create part table: %v", err)
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
	for _, m := range messages {
		if _, err := db.Exec(
			`INSERT INTO message (id, session_id, time_created, time_updated, data) VALUES (?, ?, ?, ?, ?)`,
			m.id, m.sessionID, m.timeCreatedMs, m.timeCreatedMs, m.dataJSON,
		); err != nil {
			db.Close()
			t.Fatalf("insert message %q: %v", m.id, err)
		}
	}
	for _, p := range parts {
		if _, err := db.Exec(
			`INSERT INTO part (id, message_id, session_id, time_created, time_updated, data) VALUES (?, ?, ?, ?, ?, ?)`,
			p.id, p.messageID, p.sessionID, p.timeCreatedMs, p.timeCreatedMs, p.dataJSON,
		); err != nil {
			db.Close()
			t.Fatalf("insert part %q: %v", p.id, err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}
	return dbPath, func() { _ = os.RemoveAll(dir) }
}

func TestParseOpenCodeDBToolFailureReportsToolFailed(t *testing.T) {
	// An assistant message whose latest tool part has state.status "error"
	// is a failed tool call. The watcher reports tool_failed with the tool
	// name and the state.error text.
	now := time.Now().UnixMilli()
	dbPath, cleanup := setupOpenCodeDBWithMessages(t,
		[]struct {
			id              string
			directory       string
			timeCreatedMs   int64
			timeUpdatedMs   int64
			parentID        string
		}{{"ses", testCwd, now, now, ""}},
		[]struct {
			id            string
			sessionID     string
			timeCreatedMs int64
			dataJSON      string
		}{{"msg", "ses", now, `{"role":"assistant","finish":""}`}},
		[]struct {
			id            string
			messageID     string
			sessionID     string
			timeCreatedMs int64
			dataJSON      string
		}{
			{"p1", "msg", "ses", now, `{"type":"tool","tool":"read","state":{"status":"error","error":"ENOENT: no such file"}}`},
		},
	)
	defer cleanup()

	var status, tool, details string
	(&OpenCodeWatcher{}).parseOpenCodeDB(dbPath, testCwd, "ses", func(s, tl, d, ti string) {
		status, tool, details = s, tl, d
	})
	if status != "tool_failed" {
		t.Fatalf("status = %q, want tool_failed", status)
	}
	if tool != "read" {
		t.Fatalf("tool = %q, want read", tool)
	}
	if !strings.Contains(details, "ENOENT") {
		t.Fatalf("details = %q, want ENOENT text", details)
	}
}

func TestParseOpenCodeDBInterruptedReportsInterrupted(t *testing.T) {
	// An assistant message with finish="" and error.name "MessageAbortedError"
	// means the user aborted the turn. The watcher reports "interrupted".
	now := time.Now().UnixMilli()
	dbPath, cleanup := setupOpenCodeDBWithMessages(t,
		[]struct {
			id              string
			directory       string
			timeCreatedMs   int64
			timeUpdatedMs   int64
			parentID        string
		}{{"ses", testCwd, now, now, ""}},
		[]struct {
			id            string
			sessionID     string
			timeCreatedMs int64
			dataJSON      string
		}{{"msg", "ses", now, `{"role":"assistant","finish":"","error":{"name":"MessageAbortedError"}}`}},
		nil,
	)
	defer cleanup()

	var status string
	(&OpenCodeWatcher{}).parseOpenCodeDB(dbPath, testCwd, "ses", func(s, tl, d, ti string) {
		status = s
	})
	if status != "interrupted" {
		t.Fatalf("status = %q, want interrupted", status)
	}
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
		{newSession, testCwd, freshTimeMs(), freshTimeMs(), ""},
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
		{newSession, testCwd, freshTimeMs(), freshTimeMs(), ""},
	})
	defer cleanup()

	watcherStart := time.Now().Add(-10 * time.Second)
	got := findUnclaimedOpenCodeSession(dbPath, testCwd, watcherStart, testAgent, false)
	if got != newSession {
		t.Fatalf("expected %q (skip claimed %q), got %q", newSession, oldSession, got)
	}
}

// TestOpenCodeSessionNotLeakedAcrossWorkspaces verifies that a watcher whose
// cwd has no matching session rows does NOT fall back to claiming a session
// from a different workspace. Two OpenCode agents in different workspaces
// must never bind the same internal session row — that was the root cause of
// both Kanban cards showing the same session title.
func TestOpenCodeSessionNotLeakedAcrossWorkspaces(t *testing.T) {
	resetClaimRegistry()

	otherCwd := "/home/user/other-project"
	dbPath, cleanup := setupOpenCodeDB(t, []struct {
		id              string
		directory       string
		timeCreatedMs   int64
		timeUpdatedMs   int64
		parentID        string
	}{
		// A fresh session that lives in a *different* workspace.
		{newSession, otherCwd, freshTimeMs(), freshTimeMs(), ""},
	})
	defer cleanup()

	watcherStart := time.Now().Add(-10 * time.Second)
	// Watcher running in testCwd finds no sessions there; it must NOT
	// claim the session that belongs to otherCwd.
	got := findUnclaimedOpenCodeSession(dbPath, testCwd, watcherStart, testAgent, false)
	if got != "" {
		t.Fatalf("watcher in %q must not claim session from %q, got %q", testCwd, otherCwd, got)
	}
	// The session remains unclaimed and available to a watcher in its own cwd.
	got2 := findUnclaimedOpenCodeSession(dbPath, otherCwd, watcherStart, testAgent, false)
	if got2 != newSession {
		t.Fatalf("watcher in %q should claim its own session, got %q", otherCwd, got2)
	}
}