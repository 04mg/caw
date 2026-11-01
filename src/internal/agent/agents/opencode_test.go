package agents

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/04mg/caw/internal/agent"
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
	claims = make(map[string]map[string]claim)
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

// TestParseOpenCodeDBFinishedShellCommandReportsIdle covers a "!" shell
// command (e.g. "!git status"): OpenCode runs it directly in the PTY and
// writes an assistant message holding only a bash tool part. The part
// transitions to "completed" and the message is finalized (time.completed
// set) but never receives a finish reason. The watcher must report "idle",
// not "executing bash", so the card doesn't stay stuck in Working forever.
func TestParseOpenCodeDBFinishedShellCommandReportsIdle(t *testing.T) {
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
		}{{"msg", "ses", now, `{"role":"assistant","time":{"created":` + fmt.Sprint(now) + `,"completed":` + fmt.Sprint(now) + `}}`}},
		[]struct {
			id            string
			messageID     string
			sessionID     string
			timeCreatedMs int64
			dataJSON      string
		}{
			{"p1", "msg", "ses", now, `{"type":"tool","tool":"bash","state":{"status":"completed","input":{"command":"git status"}}}`},
		},
	)
	defer cleanup()

	var status string
	(&OpenCodeWatcher{}).parseOpenCodeDB(dbPath, testCwd, "ses", func(s, tl, d, ti string) {
		status = s
	})
	if status != "idle" {
		t.Fatalf("status = %q, want idle", status)
	}
}

// TestParseOpenCodeDBRunningShellCommandReportsExecuting ensures a "!" shell
// command that is still running (bash tool part in state "running", message
// not yet finalized) still reports "executing bash".
func TestParseOpenCodeDBRunningShellCommandReportsExecuting(t *testing.T) {
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
		}{{"msg", "ses", now, `{"role":"assistant","time":{"created":` + fmt.Sprint(now) + `}}`}},
		[]struct {
			id            string
			messageID     string
			sessionID     string
			timeCreatedMs int64
			dataJSON      string
		}{
			{"p1", "msg", "ses", now, `{"type":"tool","tool":"bash","state":{"status":"running"}}`},
		},
	)
	defer cleanup()

	var status string
	(&OpenCodeWatcher{}).parseOpenCodeDB(dbPath, testCwd, "ses", func(s, tl, d, ti string) {
		status = s
	})
	if status != "executing" {
		t.Fatalf("status = %q, want executing", status)
	}
}

// TestParseOpenCodeDBMidTurnCompletedToolNotIdle guards the comment in the
// watcher about not reporting "idle" for a message row that is still being
// written: an LLM turn where a tool just completed but the message has not
// been finalized (no time.completed) must NOT report idle, otherwise the
// status would flash idle→executing. It should report "executing".
func TestParseOpenCodeDBMidTurnCompletedToolNotIdle(t *testing.T) {
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
			{"p1", "msg", "ses", now, `{"type":"step-start"}`},
			{"p2", "msg", "ses", now, `{"type":"tool","tool":"bash","state":{"status":"completed"}}`},
			{"p3", "msg", "ses", now, `{"type":"step-finish"}`},
		},
	)
	defer cleanup()

	var status string
	(&OpenCodeWatcher{}).parseOpenCodeDB(dbPath, testCwd, "ses", func(s, tl, d, ti string) {
		status = s
	})
	if status != "executing" {
		t.Fatalf("status = %q, want executing", status)
	}
}

func TestParseOpenCodeDBDismissedQuestionReportsInterrupted(t *testing.T) {
	// When the user dismisses an open "question" prompt (ESC), OpenCode marks
	// the question part as error ("The user dismissed this question") but
	// keeps the message finish at "tool-calls" and stops the turn — no new
	// rows are written until the user prompts again. The watcher must report
	// "interrupted" (not "executing", which would strand the card in Working).
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
		}{{"msg", "ses", now, `{"role":"assistant","finish":"tool-calls"}`}},
		[]struct {
			id            string
			messageID     string
			sessionID     string
			timeCreatedMs int64
			dataJSON      string
		}{
			{"p1", "msg", "ses", now, `{"type":"tool","tool":"question","state":{"status":"error","error":"The user dismissed this question"}}`},
		},
	)
	defer cleanup()

	var status, tool, details string
	(&OpenCodeWatcher{}).parseOpenCodeDB(dbPath, testCwd, "ses", func(s, tl, d, ti string) {
		status, tool, details = s, tl, d
	})
	if status != "interrupted" {
		t.Fatalf("status = %q, want interrupted", status)
	}
	if tool != "question" {
		t.Fatalf("tool = %q, want question", tool)
	}
	if !strings.Contains(details, "dismissed") {
		t.Fatalf("details = %q, want dismissed text", details)
	}
}

func TestParseOpenCodeDBDismissedQuestionNotToolFailed(t *testing.T) {
	// A dismissed question with finish="" must not fall into the tool_failed
	// branch (which also renders as Working); it is an interrupt, so the card
	// must leave Working just like the finish="tool-calls" variant.
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
			{"p1", "msg", "ses", now, `{"type":"tool","tool":"question","state":{"status":"error","error":"The user dismissed this question"}}`},
		},
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

func TestParseOpenCodeDBInvalidQuestionArgsNotInterrupted(t *testing.T) {
	// A question tool error caused by invalid arguments (schema mismatch) is
	// NOT a dismissal: the model receives the error and continues working.
	// The watcher must keep reporting tool_failed, not interrupted.
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
			{"p1", "msg", "ses", now, `{"type":"tool","tool":"question","state":{"status":"error","error":"The question tool was called with invalid arguments"}}`},
		},
	)
	defer cleanup()

	var status string
	(&OpenCodeWatcher{}).parseOpenCodeDB(dbPath, testCwd, "ses", func(s, tl, d, ti string) {
		status = s
	})
	if status != "tool_failed" {
		t.Fatalf("status = %q, want tool_failed", status)
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
	got := findUnclaimedOpenCodeSession(dbPath, testCwd, watcherStart, testAgent, false, "leaf-x")
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
	got := findUnclaimedOpenCodeSession(dbPath, testCwd, watcherStart, testAgent, false, "leaf-x")
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
	got := findUnclaimedOpenCodeSession(dbPath, testCwd, watcherStart, testAgent, false, "leaf-x")
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
	got := findUnclaimedOpenCodeSession(dbPath, testCwd, watcherStart, testAgent, false, "leaf-x")
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
	got := findUnclaimedOpenCodeSession(dbPath, testCwd, watcherStart, testAgent, false, "leaf-x")
	if got != "" {
		t.Fatalf("watcher in %q must not claim session from %q, got %q", testCwd, otherCwd, got)
	}
	// The session remains unclaimed and available to a watcher in its own cwd.
	got2 := findUnclaimedOpenCodeSession(dbPath, otherCwd, watcherStart, testAgent, false, "leaf-x")
	if got2 != newSession {
		t.Fatalf("watcher in %q should claim its own session, got %q", otherCwd, got2)
	}
}

// TestOpenCodeExactSessionClaimOwnership verifies that once a leaf claims its
// exact native session id, a sibling leaf sharing the same cwd cannot steal it,
// and that a persisted id is validated against existence + directory before it
// is claimed. This is the ownership guarantee that prevents two OpenCode panes
// in one workspace from swapping their bound sessions on reopen.
func TestOpenCodeExactSessionClaimOwnership(t *testing.T) {
	resetClaimRegistry()
	dbPath, cleanup := setupOpenCodeDB(t, []struct {
		id              string
		directory       string
		timeCreatedMs   int64
		timeUpdatedMs   int64
		parentID        string
	}{
		{oldSession, testCwd, oldTimeMs, oldTimeMs, ""},
		{newSession, testCwd, freshTimeMs(), freshTimeMs(), ""},
	})
	defer cleanup()

	// The exact id must exist in the same directory before it can be restored.
	if !openCodeSessionExists(dbPath, oldSession, testCwd) {
		t.Fatal("existing same-cwd session should be accepted")
	}
	if openCodeSessionExists(dbPath, oldSession, "/home/user/other") {
		t.Fatal("session in a different directory must be rejected")
	}
	if openCodeSessionExists(dbPath, "does-not-exist", testCwd) {
		t.Fatal("non-existent session must be rejected")
	}

	// A restored leaf binds its exact session.
	if !ClaimSessionForLeaf(testAgent, testCwd, oldSession, "leaf-restored") {
		t.Fatal("restored leaf should claim its exact session")
	}
	// A sibling leaf must NOT be able to steal the restored exact session.
	if ClaimSessionForLeaf(testAgent, testCwd, oldSession, "leaf-sibling") {
		t.Fatal("sibling leaf must not steal an owned exact session")
	}
	if ClaimedBy(testAgent, testCwd, oldSession) != "leaf-restored" {
		t.Fatal("ownership must report the restored leaf")
	}
}
// ---- Fixtures captured from a live OpenCode 1.18.35 session (paths scrubbed) ----

// ocMsgAPIError is the assistant message OpenCode wrote when the provider
// answered HTTP 400 (non-retryable): error set, time.completed set, no finish,
// no parts. The turn is over.
const ocMsgAPIError = `{"parentID":"msg_u","role":"assistant","mode":"build","agent":"build","path":{"cwd":"/home/user/project","root":"/home/user/project"},"cost":0,"tokens":{"input":0,"output":0,"reasoning":0,"cache":{"read":0,"write":0}},"modelID":"m","providerID":"p","time":{"created":1793559146012,"completed":1793559146557},"error":{"name":"APIError","data":{"message":"CAWTEST scripted failure","statusCode":400,"isRetryable":false}}}`

// ocMsgToolCalls is a finished step that requested tool calls (finish
// "tool-calls"); the next step's message follows once the LLM answers.
const ocMsgToolCalls = `{"parentID":"msg_u","role":"assistant","mode":"build","agent":"build","finish":"tool-calls","time":{"created":1793559107662,"completed":1793559108700}}`

// ocPartReadFailed is a read tool call that failed on its own (missing file).
const ocPartReadFailed = `{"type":"tool","tool":"read","callID":"call_1","state":{"status":"error","input":{"filePath":"/home/user/project/missing.txt"},"error":"File not found: /home/user/project/missing.txt","time":{"start":1793559092652,"end":1793559092712}}}`

// ocPartBashRejected is a bash call whose permission request the user
// rejected (ESC on the permission dialog). OpenCode stops the turn.
const ocPartBashRejected = `{"type":"tool","tool":"bash","callID":"call_2","state":{"status":"error","input":{"command":"touch perm2.txt"},"error":"The user rejected permission to use this specific tool call.","time":{"start":1793559650293,"end":1793559657864}}}`

// ocPartBashRunning is a bash call in progress (also what the DB shows while
// OpenCode's permission dialog for it is open).
const ocPartBashRunning = `{"type":"tool","tool":"bash","callID":"call_3","state":{"status":"running","input":{"command":"touch perm.txt"},"time":{"start":1793559596488}}}`

func mustOCMessage(t *testing.T, js string) openCodeMessage {
	t.Helper()
	var m openCodeMessage
	if err := json.Unmarshal([]byte(js), &m); err != nil {
		t.Fatalf("unmarshal message: %v", err)
	}
	return m
}

func mustOCParts(t *testing.T, js ...string) []openCodePart {
	t.Helper()
	var out []openCodePart
	for _, j := range js {
		var p openCodePart
		if err := json.Unmarshal([]byte(j), &p); err != nil {
			t.Fatalf("unmarshal part: %v", err)
		}
		out = append(out, p)
	}
	return out
}

// An API error ends the turn: it must not stay "thinking" forever (the old
// behavior) but report interrupted with the error text.
func TestDeriveOpenCodeStateAPIErrorEndsTurn(t *testing.T) {
	st := deriveOpenCodeState(mustOCMessage(t, ocMsgAPIError), nil)
	if st.status != "interrupted" {
		t.Fatalf("status = %q, want interrupted", st.status)
	}
	if st.details != "APIError: CAWTEST scripted failure" {
		t.Fatalf("details = %q", st.details)
	}
}

// A rejected permission stops the turn with finish "tool-calls"; the old code
// reported "executing bash" forever.
func TestDeriveOpenCodeStateRejectedPermissionIsInterrupted(t *testing.T) {
	st := deriveOpenCodeState(mustOCMessage(t, ocMsgToolCalls), mustOCParts(t, `{"type":"step-start"}`, ocPartBashRejected, `{"type":"step-finish","reason":"tool-calls"}`))
	if st.status != "interrupted" || st.tool != "bash" || !strings.Contains(st.details, "rejected permission") {
		t.Fatalf("got %+v, want interrupted bash with rejection text", st)
	}
}

// A tool that failed on its own inside a finished "tool-calls" step is
// surfaced as tool_failed while the agent processes the failure (the old code
// reported "executing read", so tool_failed was never shown).
func TestDeriveOpenCodeStateFailedToolInToolCallsStep(t *testing.T) {
	st := deriveOpenCodeState(mustOCMessage(t, ocMsgToolCalls), mustOCParts(t, `{"type":"step-start"}`, ocPartReadFailed, `{"type":"step-finish","reason":"tool-calls"}`))
	if st.status != "tool_failed" || st.tool != "read" || !strings.Contains(st.details, "File not found") {
		t.Fatalf("got %+v, want tool_failed read", st)
	}
}

// A running tool reports executing and exposes its start time, which scopes
// the PTY permission-dialog signal to this tool call.
func TestDeriveOpenCodeStateRunningToolStart(t *testing.T) {
	st := deriveOpenCodeState(mustOCMessage(t, `{"role":"assistant","time":{"created":1}}`), mustOCParts(t, `{"type":"step-start"}`, ocPartBashRunning))
	if st.status != "executing" || st.tool != "bash" || st.activeToolStart != 1793559596488 {
		t.Fatalf("got %+v", st)
	}
}

func TestOpenCodePermissionPending(t *testing.T) {
	const leaf = "oc-perm-leaf"
	defer agent.SetPtyPendingInputForTest(leaf, time.Time{})
	defer agent.SetPtyLineSubmitForTest(leaf, time.Time{})
	defer agent.SetPtyInterruptForTest(leaf, time.Time{})

	toolStart := time.Now().Add(-5 * time.Second)
	if openCodePermissionPending(leaf, toolStart.UnixMilli()) {
		t.Fatal("no dialog seen: must not be pending")
	}
	// Dialog seen before this tool started (an older prompt): not pending.
	agent.SetPtyPendingInputForTest(leaf, toolStart.Add(-time.Second))
	if openCodePermissionPending(leaf, toolStart.UnixMilli()) {
		t.Fatal("dialog from before the tool started must not count")
	}
	// Dialog seen after the tool started, unanswered: pending.
	agent.SetPtyPendingInputForTest(leaf, toolStart.Add(time.Second))
	if !openCodePermissionPending(leaf, toolStart.UnixMilli()) {
		t.Fatal("dialog after tool start must be pending")
	}
	// The user pressed Enter (allow): answered.
	agent.SetPtyLineSubmitForTest(leaf, toolStart.Add(2*time.Second))
	if openCodePermissionPending(leaf, toolStart.UnixMilli()) {
		t.Fatal("dialog answered with Enter must not be pending")
	}
	// A later ESC also answers (reject) a later dialog.
	agent.SetPtyPendingInputForTest(leaf, toolStart.Add(3*time.Second))
	agent.SetPtyInterruptForTest(leaf, toolStart.Add(4*time.Second))
	if openCodePermissionPending(leaf, toolStart.UnixMilli()) {
		t.Fatal("dialog answered with ESC must not be pending")
	}
}

func TestResolveOpenCodeDBPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("OPENCODE_DB", "")
	if got, want := resolveOpenCodeDBPath(), filepath.Join(home, ".local", "share", "opencode", "opencode.db"); got != want {
		t.Fatalf("default: got %q want %q", got, want)
	}

	xdg := t.TempDir()
	t.Setenv("XDG_DATA_HOME", xdg)
	if got, want := resolveOpenCodeDBPath(), filepath.Join(xdg, "opencode", "opencode.db"); got != want {
		t.Fatalf("XDG_DATA_HOME: got %q want %q", got, want)
	}

	// Non-release channels write opencode-<channel>.db.
	if err := os.MkdirAll(filepath.Join(xdg, "opencode"), 0o755); err != nil {
		t.Fatal(err)
	}
	ch := filepath.Join(xdg, "opencode", "opencode-dev.db")
	if err := os.WriteFile(ch, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := resolveOpenCodeDBPath(); got != ch {
		t.Fatalf("channel db: got %q want %q", got, ch)
	}
	// opencode.db wins when it exists.
	main := filepath.Join(xdg, "opencode", "opencode.db")
	if err := os.WriteFile(main, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := resolveOpenCodeDBPath(); got != main {
		t.Fatalf("main db: got %q want %q", got, main)
	}

	t.Setenv("OPENCODE_DB", "custom.db")
	if got, want := resolveOpenCodeDBPath(), filepath.Join(xdg, "opencode", "custom.db"); got != want {
		t.Fatalf("relative OPENCODE_DB: got %q want %q", got, want)
	}
	t.Setenv("OPENCODE_DB", "/abs/oc.db")
	if got := resolveOpenCodeDBPath(); got != "/abs/oc.db" {
		t.Fatalf("absolute OPENCODE_DB: got %q", got)
	}
}

// ocSessions/ocMessages/ocParts alias the anonymous fixture structs used by
// setupOpenCodeDBWithMessages.
type ocSession = struct {
	id            string
	directory     string
	timeCreatedMs int64
	timeUpdatedMs int64
	parentID      string
}
type ocMessage = struct {
	id            string
	sessionID     string
	timeCreatedMs int64
	dataJSON      string
}
type ocPart = struct {
	id            string
	messageID     string
	sessionID     string
	timeCreatedMs int64
	dataJSON      string
}

func registerOCLeaves(t *testing.T, leaves ...string) {
	t.Helper()
	for _, l := range leaves {
		agent.RegisterActiveSessionWithCwdForTest(l, testAgent, testCwd)
		agent.SetPtyLineSubmitForTest(l, time.Time{})
	}
	t.Cleanup(func() {
		for _, l := range leaves {
			agent.RegisterActiveSessionWithCwdForTest(l, "", "")
			agent.SetPtyLineSubmitForTest(l, time.Time{})
		}
	})
}

// Reproduces the live bug: an idle, never-prompted pane (leaf-idle) shares the
// cwd with a pane (leaf-busy) that just created a session with /new. The idle
// pane must not claim it; the submitting pane must.
func TestUnboundPaneDoesNotStealSiblingSession(t *testing.T) {
	resetClaimRegistry()
	registerOCLeaves(t, "leaf-idle", "leaf-busy")
	now := time.Now()
	dbPath, cleanup := setupOpenCodeDBWithMessages(t,
		[]ocSession{{newSession, testCwd, now.UnixMilli(), now.UnixMilli(), ""}},
		[]ocMessage{{"msg_u", newSession, now.UnixMilli(), `{"role":"user"}`}},
		[]ocPart{{"prt_t", "msg_u", newSession, now.UnixMilli(), `{"type":"text","text":"CAWTEST:shell:sleep 10"}`}},
	)
	defer cleanup()
	agent.SetPtyLineSubmitForTest("leaf-busy", now.Add(-60*time.Millisecond))

	watcherStart := now.Add(-time.Minute)
	if got := findUnclaimedOpenCodeSession(dbPath, testCwd, watcherStart, testAgent, false, "leaf-idle"); got != "" {
		t.Fatalf("idle pane claimed sibling's session %q", got)
	}
	if got := findUnclaimedOpenCodeSession(dbPath, testCwd, watcherStart, testAgent, false, "leaf-busy"); got != newSession {
		t.Fatalf("submitting pane got %q, want %q", got, newSession)
	}
}

// Near-simultaneous Enters (the user pressed Enter on an empty prompt in one
// pane right after submitting in another): the pane whose typed line matches
// the persisted prompt owns the session.
func TestSubmitOwnerPrefersMatchingPromptText(t *testing.T) {
	registerOCLeaves(t, "leaf-a", "leaf-b")
	now := time.Now()
	agent.SetPtyLineSubmitForTest("leaf-a", time.Time{})
	// leaf-a typed the prompt; leaf-b pressed Enter on an empty prompt later.
	agent.RecordPtyLineInputForTest("leaf-a", "CAWTEST:shell:sleep 4\r", now.Add(-60*time.Millisecond))
	agent.RecordPtyLineInputForTest("leaf-b", "\r", now.Add(-20*time.Millisecond))
	if got := agent.PtyLineSubmitOwner(testAgent, testCwd, now, "CAWTEST:shell:sleep 4"); got != "leaf-a" {
		t.Fatalf("owner = %q, want leaf-a (matching text)", got)
	}
	// Without the prompt text, the closest Enter wins.
	if got := agent.PtyLineSubmitOwner(testAgent, testCwd, now, ""); got != "leaf-b" {
		t.Fatalf("owner without text = %q, want leaf-b (closest Enter)", got)
	}
}

// A session whose prompt text is not written yet is left undecided rather than
// claimed by whichever pane pressed Enter last.
func TestFreshSessionWithoutPromptIsUndecided(t *testing.T) {
	resetClaimRegistry()
	registerOCLeaves(t, "leaf-x")
	now := time.Now()
	dbPath, cleanup := setupOpenCodeDBWithMessages(t,
		[]ocSession{{newSession, testCwd, now.UnixMilli(), now.UnixMilli(), ""}},
		[]ocMessage{{"msg_u", newSession, now.UnixMilli(), `{"role":"user"}`}},
		nil,
	)
	defer cleanup()
	agent.SetPtyLineSubmitForTest("leaf-x", now.Add(-50*time.Millisecond))
	if got := findUnclaimedOpenCodeSession(dbPath, testCwd, now.Add(-time.Minute), testAgent, false, "leaf-x"); got != "" {
		t.Fatalf("claimed %q before the prompt text landed", got)
	}
}

// Reproduces the live bug after a Caw restart: a focused, idle pane resumed on
// its own session must not re-bind to a more recently updated sibling session
// it did not submit to (the old focus/PTY gate did). It must re-bind to a
// session it did submit to (/new or /sessions + prompt).
func TestRebindRequiresOwnSubmit(t *testing.T) {
	resetClaimRegistry()
	registerOCLeaves(t, "leaf-me", "leaf-sib")
	agent.SetPtyFocusForTest("leaf-me", true)
	defer agent.SetPtyFocusForTest("leaf-me", false)
	now := time.Now()
	old := now.Add(-time.Minute).UnixMilli()
	dbPath, cleanup := setupOpenCodeDBWithMessages(t,
		[]ocSession{
			{oldSession, testCwd, old, old, ""},
			{"ses_sib", testCwd, old, now.UnixMilli(), ""},
		},
		[]ocMessage{
			{"m1", oldSession, old, `{"role":"user"}`},
			{"m2", "ses_sib", now.UnixMilli(), `{"role":"user"}`},
		},
		[]ocPart{
			{"p1", "m1", oldSession, old, `{"type":"text","text":"first"}`},
			{"p2", "m2", "ses_sib", now.UnixMilli(), `{"type":"text","text":"sibling prompt"}`},
		},
	)
	defer cleanup()

	if got := findRebindOpenCodeSession(dbPath, testCwd, testAgent, oldSession, "leaf-me"); got != "" {
		t.Fatalf("re-bound to unowned sibling session %q", got)
	}
	agent.RecordPtyLineInputForTest("leaf-sib", "sibling prompt\r", now.Add(-40*time.Millisecond))
	if got := findRebindOpenCodeSession(dbPath, testCwd, testAgent, oldSession, "leaf-me"); got != "" {
		t.Fatalf("re-bound to the sibling's session %q", got)
	}
	agent.SetPtyLineSubmitForTest("leaf-sib", time.Time{})
	agent.RecordPtyLineInputForTest("leaf-me", "sibling prompt\r", now.Add(-40*time.Millisecond))
	if got := findRebindOpenCodeSession(dbPath, testCwd, testAgent, oldSession, "leaf-me"); got != "ses_sib" {
		t.Fatalf("own /new session not re-bound: got %q", got)
	}
}
