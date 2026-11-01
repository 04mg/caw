package agents

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/04mg/caw/internal/agent"
)

// HermesWatcher tracks a Hermes Agent (Nous Research `hermes`) pane.
//
// Hermes keeps every session of every process in one shared SQLite database
// (~/.hermes/state.db, WAL mode): a `sessions` row per conversation and a
// `messages` row per user / assistant / tool message, appended incrementally
// while a turn runs. The watcher binds each Caw leaf to exactly one session
// row and derives the card status from that session's latest message row.
//
// Two things are not visible in state.db and come from elsewhere:
//   - A turn that dies on an API error persists nothing after the user's
//     prompt, so the transcript keeps reading as "thinking". Hermes logs the
//     failure to ~/.hermes/logs/agent.log, tagged with the session id; the
//     watcher tails that log (see hermesLogTail).
//   - Ctrl+C mid-turn: Hermes writes an "Operation interrupted" assistant row
//     most of the time, but not when the turn is aborted before anything was
//     persisted, so the Ctrl+C byte sent into the PTY is also honoured.
type HermesWatcher struct{}

func init() {
	agent.RegisterStatusWatcher("hermes", &HermesWatcher{})
}

// hermesToolCall mirrors one entry of the JSON blob stored in the
// messages.tool_calls column for an assistant turn that requested tools.
type hermesToolCall struct {
	Function struct {
		Name string `json:"name"`
	} `json:"function"`
}

const (
	hermesAgentID = "hermes"
	// Hermes sessions created by /new carry no cwd until later, so claims
	// can't be keyed by workspace: every Hermes pane shares one claim
	// namespace and two panes can never bind the same session row.
	hermesClaimCwd = ""
	// hermesPollInterval is the fallback poll period. fsnotify on state.db
	// and its WAL makes most transitions near-instant; the ticker covers
	// missed events, PTY Ctrl+C detection and the log tail.
	hermesPollInterval = time.Second
	// hermesBindGate is how recent this leaf's PTY output must be for the
	// watcher to claim an unowned session row. A session row appears when
	// the user submits a prompt, which makes that pane (and only that pane)
	// echo and render; an idle sibling pane in the same cwd stays silent.
	hermesBindGate = 5 * time.Second
)

// hermesHomeDir returns Hermes' data directory: $HERMES_HOME when set (Caw's
// environment is inherited by the agent), else ~/.hermes, else %APPDATA%\hermes.
func hermesHomeDir() string {
	if h := os.Getenv("HERMES_HOME"); h != "" {
		return h
	}
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".hermes")
	if _, err := os.Stat(dir); err != nil {
		if appData := os.Getenv("APPDATA"); appData != "" {
			return filepath.Join(appData, "hermes")
		}
	}
	return dir
}

// openHermesDB opens state.db read-only. database/sql connects lazily, so a
// DB that does not exist yet simply makes every query fail until it appears.
func openHermesDB(dbPath string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro&_journal_mode=WAL")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	return db, nil
}

// hermesWatchState is the per-pane binding and status state. It is driven by
// step(), which is called on every DB/WAL change notification and poll tick.
type hermesWatchState struct {
	db      *sql.DB
	leaf    string
	cwd     string
	resume  bool
	started time.Time // watcher start; fresh launches only bind newer rows
	// persisted is the native session id recorded for this leaf by a
	// previous Caw run (the id resumeCmdForAgent passed to --resume).
	persisted string

	// gate reports whether this leaf may claim an unowned session row right
	// now (recent PTY output or focus). Replaceable in tests.
	gate func() bool
	// lastCtrlC returns the last Ctrl+C sent into this leaf's PTY.
	lastCtrlC func() time.Time

	sid        string
	boundAt    float64 // unix seconds when sid was bound
	lastState  string  // last status passed to the callback
	lastReport hermesReport
	// staleRowID is the latest message row of a resumed session at bind
	// time; an unfinished turn in it belongs to the previous process.
	staleRowID int64

	// Sticky PTY interrupt (see applyInterrupt).
	lastCtrlCSeen       time.Time
	interruptApplied    bool
	interruptBoundaryID int64

	logTail *hermesLogTail
	cols    hermesColumns
}

// hermesColumns records optional schema columns. Older Hermes releases have
// neither sessions.cwd nor sessions.parent_session_id.
type hermesColumns struct {
	checked bool
	cwd     bool
	parent  bool
}

func (w *HermesWatcher) Watch(ctx context.Context, sessionID string, cwd string, resume bool, callback func(status, tool, details, title string), heartbeat func()) {
	home := hermesHomeDir()
	dbPath := filepath.Join(home, "state.db")

	db, err := openHermesDB(dbPath)
	if err != nil {
		return
	}
	defer db.Close()

	st := &hermesWatchState{
		db:     db,
		leaf:   sessionID,
		cwd:    cwd,
		resume: resume,
		// A small negative offset tolerates the delay between the PTY launch
		// and the watcher start.
		started:   time.Now().Add(-10 * time.Second),
		persisted: agent.PersistedExternalSession(sessionID),
		gate: func() bool {
			return agent.IsPtyFocused(sessionID) || time.Since(agent.LastPtyActivity(sessionID)) < hermesBindGate
		},
		lastCtrlC: func() time.Time { return agent.LastPtyCtrlC(sessionID) },
		logTail:   newHermesLogTail(filepath.Join(home, "logs", "agent.log")),
	}
	// Ignore Ctrl+C presses that happened before this watcher started.
	st.lastCtrlCSeen = st.lastCtrlC()
	defer func() {
		if st.sid != "" {
			UnclaimSession(hermesAgentID, hermesClaimCwd, st.sid)
		}
	}()

	// Hermes runs SQLite in WAL mode: commits append to state.db-wal and only
	// reach state.db on checkpoint, so both files are watched.
	var dbNotify, walNotify <-chan struct{}
	if n, err := NewFileChangeNotifier(); err == nil {
		defer n.Close()
		n.Watch(dbPath)
		dbNotify = n.Notify()
	}
	if n, err := NewFileChangeNotifier(); err == nil {
		defer n.Close()
		n.Watch(dbPath + "-wal")
		walNotify = n.Notify()
	}

	ticker := time.NewTicker(hermesPollInterval)
	defer ticker.Stop()

	st.step(callback)
	for {
		select {
		case <-ctx.Done():
			return
		case <-dbNotify:
			st.step(callback)
		case <-walNotify:
			st.step(callback)
		case <-ticker.C:
			heartbeat()
			st.step(callback)
		}
	}
}

// step runs one bind / rebind / classify pass and reports the status.
func (st *hermesWatchState) step(callback func(status, tool, details, title string)) {
	st.checkColumns()
	st.logTail.poll()

	if st.sid == "" {
		sid := st.bindInitial()
		if sid == "" {
			return
		}
		st.bind(sid)
		if st.resume {
			// A resumed session may end in a turn the previous process never
			// finished (Ctrl+C during the model wait persists only the
			// prompt, a crash leaves a tool call). This process starts idle,
			// so that row must not read as working.
			if row, ok := hermesLatestRow(st.db, sid); ok {
				st.staleRowID = row.ID
			}
		}
	} else if next := st.successor(); next != "" {
		UnclaimSession(hermesAgentID, hermesClaimCwd, st.sid)
		st.bind(next)
	}

	row, ok := hermesLatestRow(st.db, st.sid)
	title := hermesSessionTitleDB(st.db, st.sid)
	var status, tool, details string
	if !ok {
		// Bound but no message yet (e.g. right after /new).
		status = "idle"
	} else {
		status, tool, details = hermesStatusForRow(row)
		if row.ID == st.staleRowID && hermesWorking(status) {
			status, tool, details = "idle", "", ""
		}
		if status == "thinking" {
			// The turn died on an API error: nothing is persisted after the
			// prompt / tool result, only the log knows the turn is over.
			if ev, ok := st.logTail.lastError(st.sid); ok && ev.at >= row.Timestamp {
				status, tool, details = "idle", "", "API error: "+ev.msg
			}
		}
	}

	status = st.applyInterrupt(status, row)
	if status == "" {
		return
	}
	if status == "interrupted" {
		tool, details = "", ""
	}
	report := hermesReport{status, tool, details, title}
	// Only report changes, so the shared watchdog can still notice a working
	// state that went stale. waiting_input is re-reported every pass to keep
	// the watchdog's lastActivity fresh while the user takes their time.
	if report == st.lastReport && status != "waiting_input" {
		return
	}
	st.lastReport = report
	st.lastState = status
	callback(status, tool, details, title)
}

type hermesReport struct{ status, tool, details, title string }

// bind claims sid for this leaf and resets per-session state.
func (st *hermesWatchState) bind(sid string) {
	st.sid = sid
	st.boundAt = float64(time.Now().UnixNano()) / 1e9
	st.lastState = ""
	st.lastReport = hermesReport{}
	st.interruptApplied = false
	st.interruptBoundaryID = 0
	st.staleRowID = 0
	agent.RecordExternalSession(st.leaf, sid)
}

func hermesWorking(status string) bool {
	switch status {
	case "thinking", "executing", "tool_failed", "waiting_input":
		return true
	}
	return false
}

// applyInterrupt honours a Ctrl+C sent into the PTY while the bound turn is
// working. Hermes only cancels a turn on Ctrl+C (ESC is not an interrupt key
// in either the classic CLI or the Ink TUI) and may persist nothing for an
// aborted turn, so the card would otherwise keep reading "thinking" from the
// pre-interrupt prompt row. The interrupt stays sticky until a NEW user row
// (id above the boundary recorded at interrupt time) arrives.
func (st *hermesWatchState) applyInterrupt(status string, row hermesRow) string {
	if last := st.lastCtrlC(); last.After(st.lastCtrlCSeen) {
		st.lastCtrlCSeen = last
		if hermesWorking(st.lastState) {
			st.interruptApplied = true
			st.interruptBoundaryID = row.ID
		}
	}
	if st.interruptApplied && strings.EqualFold(row.Role, "user") && row.ID > st.interruptBoundaryID {
		st.interruptApplied = false
	}
	if st.interruptApplied && hermesWorking(status) {
		return "interrupted"
	}
	return status
}

func (st *hermesWatchState) checkColumns() {
	if st.cols.checked {
		return
	}
	rows, err := st.db.Query(`PRAGMA table_info(sessions)`)
	if err != nil {
		return
	}
	defer rows.Close()
	found := false
	for rows.Next() {
		var cid int
		var name, typ string
		var notnull, pk int
		var dflt sql.NullString
		if rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk) != nil {
			continue
		}
		found = true
		switch name {
		case "cwd":
			st.cols.cwd = true
		case "parent_session_id":
			st.cols.parent = true
		}
	}
	st.cols.checked = found
}

// hermesSession is the subset of a sessions row the binder looks at.
type hermesSession struct {
	ID        string
	StartedAt float64
	EndedAt   sql.NullFloat64
	EndReason string
	Cwd       string
	Parent    string
	LastMsgAt float64
}

func (st *hermesWatchState) sessions(where string, args ...any) []hermesSession {
	cwdCol, parentCol := "''", "''"
	if st.cols.cwd {
		cwdCol = "COALESCE(s.cwd,'')"
	}
	if st.cols.parent {
		parentCol = "COALESCE(s.parent_session_id,'')"
	}
	q := `SELECT s.id, s.started_at, s.ended_at, COALESCE(s.end_reason,''), ` + cwdCol + `, ` + parentCol + `,
		COALESCE((SELECT MAX(m.timestamp) FROM messages m WHERE m.session_id = s.id), 0)
		FROM sessions s`
	if where != "" {
		q += " WHERE " + where
	}
	rows, err := st.db.Query(q, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []hermesSession
	for rows.Next() {
		var s hermesSession
		if rows.Scan(&s.ID, &s.StartedAt, &s.EndedAt, &s.EndReason, &s.Cwd, &s.Parent, &s.LastMsgAt) == nil {
			out = append(out, s)
		}
	}
	return out
}

// sameCwd reports whether a session's recorded cwd is compatible with the
// pane's cwd. An empty cwd (sessions created by /new, older Hermes) matches.
func (st *hermesWatchState) sameCwd(cwd string) bool {
	if cwd == "" || st.cwd == "" {
		return true
	}
	return hermesCleanPath(cwd) == hermesCleanPath(st.cwd)
}

func hermesCleanPath(p string) string {
	p = filepath.Clean(p)
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
}

// free reports whether no other leaf holds the claim for sid.
func (st *hermesWatchState) free(sid string) bool {
	owner := ClaimedBy(hermesAgentID, hermesClaimCwd, sid)
	return owner == "" || owner == st.leaf
}

func (st *hermesWatchState) claim(sid string) bool {
	return ClaimSessionForLeaf(hermesAgentID, hermesClaimCwd, sid, st.leaf)
}

// bindInitial picks the session row for a pane that is not bound yet.
//
//   - Reopened pane (`hermes --resume <id>` from resumeCmdForAgent): bind the
//     persisted exact id as soon as its row exists, even while it still
//     carries the ended_at of the previous run — Hermes clears it a moment
//     later when it reopens the session. Falling back to a scan here is what
//     made two reopened panes swap sessions.
//   - Otherwise only claim while this pane's PTY is active or focused (the
//     user just submitted a prompt here). Fresh launches take the oldest live
//     top-level session created after the watcher started; `--continue`
//     takes the most recently active live one.
func (st *hermesWatchState) bindInitial() string {
	if st.resume {
		if exact := st.persisted; exact != "" {
			if len(st.sessions("s.id = ?", exact)) == 1 {
				if st.claim(exact) {
					return exact
				}
			}
			return ""
		}
	}
	if !st.gate() {
		return ""
	}
	cands := st.sessions("s.ended_at IS NULL")
	var best *hermesSession
	for i := range cands {
		c := &cands[i]
		if c.Parent != "" || !st.free(c.ID) || !st.sameCwd(c.Cwd) {
			continue
		}
		if st.resume {
			if best == nil || hermesActivity(*c) > hermesActivity(*best) {
				best = c
			}
			continue
		}
		if time.Unix(0, int64(c.StartedAt*1e9)).Before(st.started) {
			continue
		}
		if best == nil || c.StartedAt < best.StartedAt {
			best = c
		}
	}
	if best != nil && st.claim(best.ID) {
		return best.ID
	}
	return ""
}

func hermesActivity(s hermesSession) float64 {
	if s.LastMsgAt > s.StartedAt {
		return s.LastMsgAt
	}
	return s.StartedAt
}

// successor detects an in-process session switch. Hermes always ends the
// session it leaves — /new ("new_session"), /resume ("resumed_other"),
// /branch ("branched"), context compression ("compression") — so the watcher
// only looks for a new row once its bound session was ended after binding,
// and never because a sibling pane happened to be busier. Candidates, in
// order:
//  1. a live child row (parent_session_id = bound): compression, /branch;
//  2. a live row created right when the bound one ended: /new;
//  3. for any other switch (/resume <older session>), the live row that gets
//     the next message, provided this pane's PTY is active or focused.
func (st *hermesWatchState) successor() string {
	cur := st.sessions("s.id = ?", st.sid)
	if len(cur) != 1 || !cur[0].EndedAt.Valid || cur[0].EndedAt.Float64 < st.boundAt {
		return ""
	}
	ended := cur[0].EndedAt.Float64
	switch cur[0].EndReason {
	case "cli_close", "agent_close":
		// The process is exiting; there is nothing to follow.
		return ""
	}
	cands := st.sessions("s.ended_at IS NULL AND s.id != ?", st.sid)
	pick := func(match func(c hermesSession) bool, better func(a, b hermesSession) bool) string {
		var best *hermesSession
		for i := range cands {
			c := cands[i]
			if !st.free(c.ID) || !match(c) {
				continue
			}
			if best == nil || better(c, *best) {
				best = &cands[i]
			}
		}
		if best != nil && st.claim(best.ID) {
			return best.ID
		}
		return ""
	}
	if id := pick(func(c hermesSession) bool { return c.Parent == st.sid },
		func(a, b hermesSession) bool { return a.StartedAt > b.StartedAt }); id != "" {
		return id
	}
	if id := pick(func(c hermesSession) bool {
		return c.Parent == "" && st.sameCwd(c.Cwd) && c.StartedAt >= ended-2 && c.StartedAt <= ended+10
	}, func(a, b hermesSession) bool { return a.StartedAt < b.StartedAt }); id != "" {
		return id
	}
	if !st.gate() {
		return ""
	}
	return pick(func(c hermesSession) bool {
		return c.Parent == "" && st.sameCwd(c.Cwd) && c.LastMsgAt > ended
	}, func(a, b hermesSession) bool { return a.LastMsgAt > b.LastMsgAt })
}

// hermesRow is the latest messages row of a session.
type hermesRow struct {
	ID           int64
	Role         string
	Content      string
	ToolCalls    string
	FinishReason string
	ToolName     string
	Timestamp    float64
}

func hermesLatestRow(db *sql.DB, sid string) (hermesRow, bool) {
	var r hermesRow
	err := db.QueryRow(
		`SELECT id, role, COALESCE(content,''), COALESCE(tool_calls,''), COALESCE(finish_reason,''), COALESCE(tool_name,''), timestamp
		 FROM messages WHERE session_id = ? ORDER BY id DESC LIMIT 1`, sid,
	).Scan(&r.ID, &r.Role, &r.Content, &r.ToolCalls, &r.FinishReason, &r.ToolName, &r.Timestamp)
	return r, err == nil
}

// hermesSessionTitleDB returns the display title for the given session: the
// auto-generated sessions.title when present, otherwise the first user message
// text via CleanPrompt.
func hermesSessionTitleDB(db *sql.DB, sid string) string {
	var title sql.NullString
	_ = db.QueryRow(`SELECT title FROM sessions WHERE id = ?`, sid).Scan(&title)
	t := CleanPrompt(title.String)
	if t == "" {
		var firstUser sql.NullString
		_ = db.QueryRow(
			`SELECT content FROM messages WHERE session_id = ? AND role = 'user' ORDER BY id ASC LIMIT 1`, sid,
		).Scan(&firstUser)
		t = CleanPrompt(firstUser.String)
	}
	return t
}

// hermesSessionTitle is hermesSessionTitleDB for a database path.
func hermesSessionTitle(dbPath, sid string) string {
	db, err := openHermesDB(dbPath)
	if err != nil {
		return ""
	}
	defer db.Close()
	return hermesSessionTitleDB(db, sid)
}

// hermesStatusForRow derives the card status from the latest message row:
//
//   - role=user                        → thinking (a prompt just arrived)
//   - role=tool, failed result         → tool_failed (tool name + error text)
//   - role=tool                        → thinking (the agent continues)
//   - assistant, finish_reason=tool_calls
//   - last tool is clarify (or another user-input tool) → waiting_input
//   - otherwise                                          → executing
//   - assistant, finish_reason=stop    → idle (turn complete, answer excerpt)
//   - assistant "Operation interrupted…" / aborted → interrupted
//   - assistant without finish_reason  → thinking
func hermesStatusForRow(r hermesRow) (status, tool, details string) {
	if strings.EqualFold(r.Role, "tool") {
		if errText, failed := hermesToolFailure(r.ToolName, r.Content); failed {
			return "tool_failed", r.ToolName, errText
		}
		return "thinking", "", ""
	}
	status, tool = hermesStatusForMessage(r.Role, r.Content, r.ToolCalls, r.FinishReason)
	if status == "idle" {
		details = CleanPrompt(r.Content)
	}
	return status, tool, details
}

// hermesStatusForMessage derives the Caw working/idle state from a single
// Hermes message row (see hermesStatusForRow). It returns an empty status for
// unknown roles. Tool rows are classified as thinking or tool_failed based on
// a generic "error" field; hermesStatusForRow refines that per tool.
func hermesStatusForMessage(role, content, toolCallsJSON, finishReason string) (status, tool string) {
	switch strings.ToLower(role) {
	case "user":
		return "thinking", ""
	case "tool":
		if hermesToolContentHasError(content) {
			return "tool_failed", ""
		}
		return "thinking", ""
	case "assistant", "agent":
		// Ctrl+C mid-turn: Hermes persists "Operation interrupted." with no
		// finish_reason.
		if strings.HasPrefix(strings.TrimSpace(content), "Operation interrupted") {
			return "interrupted", ""
		}
		switch strings.ToLower(finishReason) {
		case "aborted":
			return "interrupted", ""
		case "tool_calls":
			toolName := lastHermesToolName(toolCallsJSON)
			if toolName == "" {
				return "thinking", ""
			}
			// User-input tools (clarify, ...) block the agent until the human
			// answers.
			if isUserInputTool(strings.ToLower(toolName)) {
				return "waiting_input", toolName
			}
			return "executing", toolName
		case "stop":
			return "idle", ""
		}
		// No finish_reason: the turn is still in progress.
		return "thinking", ""
	}
	return "", ""
}

// lastHermesToolName parses the messages.tool_calls JSON array (OpenAI
// function-call format) and returns the name of the last tool call, or "" when
// the blob is empty or unparseable.
func lastHermesToolName(toolCallsJSON string) string {
	if toolCallsJSON == "" {
		return ""
	}
	var calls []hermesToolCall
	if err := json.Unmarshal([]byte(toolCallsJSON), &calls); err != nil {
		return ""
	}
	if len(calls) == 0 {
		return ""
	}
	return calls[len(calls)-1].Function.Name
}

// hermesToolFailure mirrors Hermes' own failure classification
// (agent/display.py _detect_tool_failure): the terminal tool failed when its
// exit_code is non-zero; any other tool failed when its JSON result carries a
// non-empty "error". It returns the text to show on the card.
func hermesToolFailure(toolName, content string) (string, bool) {
	if toolName == "terminal" {
		var res struct {
			ExitCode *int            `json:"exit_code"`
			Error    json.RawMessage `json:"error"`
		}
		if json.Unmarshal([]byte(strings.TrimSpace(content)), &res) != nil || res.ExitCode == nil || *res.ExitCode == 0 {
			return "", false
		}
		if errText := hermesToolErrorText(content); errText != "" {
			return errText, true
		}
		return fmt.Sprintf("exit code %d", *res.ExitCode), true
	}
	errText := hermesToolErrorText(content)
	return errText, errText != ""
}

// hermesToolContentHasError reports whether a tool-role message's content
// JSON carries a non-empty "error" field.
func hermesToolContentHasError(content string) bool {
	return hermesToolErrorText(content) != ""
}

// hermesToolErrorText extracts the "error" string from a tool-role message's
// content JSON, or "" when there is no error field or the content isn't JSON.
func hermesToolErrorText(content string) string {
	content = strings.TrimSpace(content)
	if content == "" || content[0] != '{' {
		return ""
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal([]byte(content), &obj) != nil {
		return ""
	}
	raw, ok := obj["error"]
	if !ok {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	if string(raw) == "null" {
		return ""
	}
	return string(raw)
}

// ----- agent.log tail ------------------------------------------------------

// hermesLogEvent is a turn-ending error logged for a session.
type hermesLogEvent struct {
	at  float64 // unix seconds
	msg string
}

// hermesLogTail follows ~/.hermes/logs/agent.log. Hermes tags every record
// emitted while a session is active with "[<session id>]", and logs each
// failure that ends a turn without persisting a reply (non-retryable client
// error, retries exhausted, context overflow, ...) at ERROR level from
// agent.conversation_loop, e.g.:
//
//	2026-11-01 18:51:21,543 ERROR [20261101_184802_7d73cd] agent.conversation_loop: Non-retryable client error: ...
//
// Only data appended after the watcher started is read.
type hermesLogTail struct {
	path    string
	offset  int64
	partial string
	errors  map[string]hermesLogEvent
}

func newHermesLogTail(path string) *hermesLogTail {
	t := &hermesLogTail{path: path, errors: make(map[string]hermesLogEvent)}
	if info, err := os.Stat(path); err == nil {
		t.offset = info.Size()
	}
	return t
}

var hermesLogLine = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}),(\d{3}) (ERROR|CRITICAL) \[([^\]\s]+)\] agent\.conversation_loop: (.*)$`)

// poll reads newly appended log data.
func (t *hermesLogTail) poll() {
	if t == nil {
		return
	}
	info, err := os.Stat(t.path)
	if err != nil {
		return
	}
	if info.Size() < t.offset {
		// Rotated (RotatingFileHandler renames agent.log and starts a new
		// one) or truncated: start over.
		t.offset = 0
		t.partial = ""
	}
	if info.Size() == t.offset {
		return
	}
	f, err := os.Open(t.path)
	if err != nil {
		return
	}
	defer f.Close()
	if _, err := f.Seek(t.offset, io.SeekStart); err != nil {
		return
	}
	// Bound a single read so a huge backlog can't stall the watcher.
	data, err := io.ReadAll(io.LimitReader(f, 4<<20))
	if err != nil {
		return
	}
	t.offset += int64(len(data))
	t.consume(string(data))
}

func (t *hermesLogTail) consume(chunk string) {
	text := t.partial + chunk
	last := strings.LastIndexByte(text, '\n')
	if last < 0 {
		t.partial = text
		return
	}
	t.partial = text[last+1:]
	sc := bufio.NewScanner(strings.NewReader(text[:last]))
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	for sc.Scan() {
		t.consumeLine(sc.Text())
	}
}

func (t *hermesLogTail) consumeLine(line string) {
	m := hermesLogLine.FindStringSubmatch(line)
	if m == nil {
		return
	}
	at, err := time.ParseInLocation("2006-01-02 15:04:05", m[1], time.Local)
	if err != nil {
		return
	}
	var ms int
	fmt.Sscanf(m[2], "%d", &ms)
	sec := float64(at.Unix()) + float64(ms)/1000
	msg := strings.TrimSpace(m[5])
	if len(msg) > 160 {
		msg = msg[:160] + "…"
	}
	t.errors[m[4]] = hermesLogEvent{at: sec, msg: msg}
}

// lastError returns the most recent turn-ending error logged for sid.
func (t *hermesLogTail) lastError(sid string) (hermesLogEvent, bool) {
	if t == nil {
		return hermesLogEvent{}, false
	}
	ev, ok := t.errors[sid]
	return ev, ok
}
