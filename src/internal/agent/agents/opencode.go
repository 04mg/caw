package agents

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/04mg/caw/internal/agent"
)

type OpenCodeWatcher struct{}

func init() {
	agent.RegisterStatusWatcher("opencode", &OpenCodeWatcher{})
}

// openCodePart mirrors the JSON blob stored in the part.data column.
type openCodePart struct {
	Type  string `json:"type"`
	Tool  string `json:"tool,omitempty"`
	State *struct {
		Status string `json:"status"`
		// Error holds the error message when a tool call failed
		// (state.status == "error"), e.g. "The user dismissed this question".
		Error string `json:"error,omitempty"`
		Time  struct {
			Start int64 `json:"start,omitempty"`
		} `json:"time,omitempty"`
	} `json:"state,omitempty"`
	Text string `json:"text,omitempty"`
}

// openCodeMessage mirrors the JSON blob stored in the message.data column.
type openCodeMessage struct {
	Role   string `json:"role"`
	Finish string `json:"finish,omitempty"`
	Time   struct {
		// Completed is set (nonzero) once the message row is fully written.
		// A "!" shell command message is finalized without a finish reason,
		// so it can be told apart from a mid-flight LLM turn by this field.
		Completed int64 `json:"completed,omitempty"`
	} `json:"time,omitempty"`
	Parts []struct {
		Type string `json:"type"`
		Text string `json:"text,omitempty"`
	} `json:"parts,omitempty"`
	Error *openCodeMessageError `json:"error,omitempty"`
}

// openCodeMessageError is message.data.error: set when the turn was aborted
// (MessageAbortedError) or failed (APIError, ProviderAuthError, ...).
type openCodeMessageError struct {
	Name string `json:"name"`
	Data struct {
		Message string `json:"message,omitempty"`
	} `json:"data,omitempty"`
}

// text renders the error for the card's details line.
func (e *openCodeMessageError) text() string {
	if e == nil {
		return ""
	}
	if e.Data.Message == "" {
		return e.Name
	}
	if e.Name == "" {
		return e.Data.Message
	}
	return e.Name + ": " + e.Data.Message
}

// resolveOpenCodeDBPath mirrors how OpenCode 1.x picks its SQLite database:
//
//   - the data dir is $XDG_DATA_HOME/opencode (xdg-basedir), falling back to
//     ~/.local/share/opencode;
//   - OPENCODE_DB overrides the file (absolute, or relative to the data dir);
//   - release channels (latest/beta/prod) use opencode.db, other channels use
//     opencode-<channel>.db, so when opencode.db is missing the most recently
//     modified opencode-*.db in the data dir is used.
//
// Caw's PTY children inherit Caw's environment, so resolving with Caw's env
// finds the same file the agent writes. %APPDATA% is kept as a last resort.
func resolveOpenCodeDBPath() string {
	dataHome := os.Getenv("XDG_DATA_HOME")
	if dataHome == "" || !filepath.IsAbs(dataHome) {
		home, _ := os.UserHomeDir()
		dataHome = filepath.Join(home, ".local", "share")
	}
	dataDir := filepath.Join(dataHome, "opencode")
	if v := os.Getenv("OPENCODE_DB"); v != "" && v != ":memory:" {
		if filepath.IsAbs(v) {
			return v
		}
		return filepath.Join(dataDir, v)
	}
	def := filepath.Join(dataDir, "opencode.db")
	if _, err := os.Stat(def); err == nil {
		return def
	}
	if matches, _ := filepath.Glob(filepath.Join(dataDir, "opencode-*.db")); len(matches) > 0 {
		best, bestMod := "", time.Time{}
		for _, m := range matches {
			if info, err := os.Stat(m); err == nil && info.ModTime().After(bestMod) {
				best, bestMod = m, info.ModTime()
			}
		}
		if best != "" {
			return best
		}
	}
	if appData := os.Getenv("APPDATA"); appData != "" {
		if p := filepath.Join(appData, "opencode", "opencode.db"); fileExists(p) {
			return p
		}
	}
	return def
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// openCodePermissionPending reports whether the leaf's PTY showed OpenCode's
// permission dialog after the given running tool started and the user has not
// answered (Enter / click / ESC) since. OpenCode keeps pending permission
// requests in memory only: the tool part simply stays "running" in the DB, so
// the TUI is the only signal that the agent is blocked on the user.
func openCodePermissionPending(leaf string, toolStartMs int64) bool {
	seen := agent.LastPtyPendingInput(leaf)
	if seen.IsZero() {
		return false
	}
	if toolStartMs > 0 && seen.Before(time.UnixMilli(toolStartMs)) {
		return false
	}
	return seen.After(agent.LastPtyAnswer(leaf))
}

func (w *OpenCodeWatcher) Watch(ctx context.Context, sessionID string, cwd string, resume bool, callback func(status, tool, details, title string), heartbeat func()) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	dbPath := resolveOpenCodeDBPath()

	const agentID = "opencode"
	var lastDBMod time.Time
	var lastWALMod time.Time
	var lastReportedStatus string
	var openCodeSessionID string
	// For a fresh start, only match sessions created after the watcher
	// started (no negative offset) to avoid grabbing a sibling agent's
	// session. On resume, the session may predate the watcher so we skip
	// the recency filter entirely in findUnclaimedOpenCodeSession.
	watcherStart := time.Now().Add(-10 * time.Second)

	// fsnotify-based immediate change notifiers. OpenCode runs SQLite in WAL
	// mode, so a write only touches opencode.db-wal until the next checkpoint:
	// watching the main file alone left the watcher on the 2s fallback poll.
	// Watch both; the ticker still covers missed events and drives heartbeat.
	var notifyCh, walNotifyCh <-chan struct{}
	watchFiles := func() {}
	if notifier, err := NewFileChangeNotifier(); err == nil {
		defer notifier.Close()
		notifyCh = notifier.Notify()
		if walNotifier, err := NewFileChangeNotifier(); err == nil {
			defer walNotifier.Close()
			walNotifyCh = walNotifier.Notify()
			watchFiles = func() {
				notifier.Watch(dbPath)
				walNotifier.Watch(dbPath + "-wal")
			}
		} else {
			watchFiles = func() { notifier.Watch(dbPath) }
		}
		watchFiles()
	}

	defer func() {
		if openCodeSessionID != "" {
			UnclaimSession(agentID, cwd, openCodeSessionID)
		}
	}()

	dbChanged := func() bool {
		// The DB may not exist yet (first ever OpenCode run) or may be
		// created under a channel-specific name; re-resolve until it exists.
		if !fileExists(dbPath) {
			if p := resolveOpenCodeDBPath(); p != dbPath {
				dbPath = p
				watchFiles()
			}
		}
		changed := false
		if info, err := os.Stat(dbPath); err == nil {
			if info.ModTime() != lastDBMod {
				changed = true
				lastDBMod = info.ModTime()
			}
		}
		if walInfo, err := os.Stat(dbPath + "-wal"); err == nil {
			if walInfo.ModTime() != lastWALMod {
				changed = true
				lastWALMod = walInfo.ModTime()
			}
		}
		return changed
	}

	bind := func(id string) {
		openCodeSessionID = id
		lastReportedStatus = ""
		agent.RecordExternalSession(sessionID, id)
	}

	// pendingProbe is true when a tick should re-read the DB even though it
	// did not change: while waiting for input (keeps the watchdog's
	// lastActivity alive and notices the answer), and while a tool runs and
	// the PTY just showed a permission dialog (only visible in the TUI).
	var runningToolStart int64
	pendingProbe := func() bool {
		switch lastReportedStatus {
		case "waiting_input":
			return true
		case "executing":
			return openCodePermissionPending(sessionID, runningToolStart)
		}
		return false
	}

	processState := func(changed bool) {
		if openCodeSessionID == "" {
			// A reopened pane must resume tracking its exact prior session
			// (set by resumeCmdForAgent via `opencode -s <id>`). Bind to it
			// first, never a heuristic scan over the shared DB, so two panes
			// sharing a cwd each follow their own conversation.
			if !changed {
				return
			}
			id := bindOpenCodeInitial(dbPath, cwd, watcherStart, agentID, resume, sessionID)
			if id == "" {
				return
			}
			bind(id)
		} else if changed {
			// Mid-session re-bind for /new, /sessions and /resume. OpenCode
			// keeps every session of the workspace in one shared DB, so a
			// session that just received a user message is only ours when
			// THIS pane submitted it (PtySubmitOwner); otherwise it belongs
			// to a sibling pane (or an external opencode process) and must
			// not be followed.
			if newKey := findRebindOpenCodeSession(dbPath, cwd, agentID, openCodeSessionID, sessionID); newKey != "" {
				if ClaimSessionForLeaf(agentID, cwd, newKey, sessionID) {
					UnclaimSession(agentID, cwd, openCodeSessionID)
					bind(newKey)
				}
			}
		}

		if !changed && !pendingProbe() {
			return
		}
		st, ok := w.readOpenCodeState(dbPath, openCodeSessionID)
		if !ok {
			return
		}
		runningToolStart = st.activeToolStart
		if st.status == "executing" && openCodePermissionPending(sessionID, st.activeToolStart) {
			st.status = "waiting_input"
			st.details = "Permission required"
		}
		lastReportedStatus = st.status
		callback(st.status, st.tool, st.details, st.title)
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-notifyCh:
			processState(dbChanged())
		case <-walNotifyCh:
			processState(dbChanged())
		case <-ticker.C:
			heartbeat()
			processState(dbChanged())
		}
	}
}

// bindOpenCodeInitial selects the initial OpenCode session to bind for a leaf.
// It prefers the exact native session id persisted for the leaf by a previous
// Caw process (so a reopened pane resumes its own conversation even when a
// sibling OpenCode shares the same cwd), and only falls back to a candidate
// scan for a fresh launch. The exact id is validated to still exist and belong
// to the same directory before claiming.
func bindOpenCodeInitial(dbPath, cwd string, watcherStart time.Time, agentID string, resume bool, leaf string) string {
	if exact := agent.PersistedExternalSession(leaf); exact != "" && openCodeSessionExists(dbPath, exact, cwd) {
		if ClaimSessionForLeaf(agentID, cwd, exact, leaf) {
			return exact
		}
	}
	return findUnclaimedOpenCodeSession(dbPath, cwd, watcherStart, agentID, resume, leaf)
}

// openCodeSessionExists reports whether the given session id exists in the DB
// and (when cwd is set) belongs to the same directory, so a persisted id that
// no longer exists (or was moved to another workspace) is not resurrected.
func openCodeSessionExists(dbPath, sid, cwd string) bool {
	if sid == "" {
		return false
	}
	db, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro&_journal_mode=WAL")
	if err != nil {
		return false
	}
	defer db.Close()
	var directory string
	err = db.QueryRow(`SELECT COALESCE(directory,'') FROM session WHERE id = ?`, sid).Scan(&directory)
	if err != nil {
		return false
	}
	if cwd != "" && directory != cwd {
		return false
	}
	return true
}

// openCodeCandidate is a top-level OpenCode session of the watcher's cwd.
// activity is the time (ms) of its latest user message — the write a pane's
// Enter produces — or its creation time when it has no user message yet.
type openCodeCandidate struct {
	id          string
	timeCreated int64
	timeUpdated int64
	activity    int64
}

// openCodeLastUserMessage returns the time_created (ms) and prompt text of the
// latest user message in the session, or 0 when it has none.
func openCodeLastUserMessage(db *sql.DB, sid string) (int64, string) {
	var id string
	var created int64
	if err := db.QueryRow(
		`SELECT id, time_created FROM message WHERE session_id = ? AND json_extract(data,'$.role') = 'user' ORDER BY time_created DESC LIMIT 1`,
		sid,
	).Scan(&id, &created); err != nil {
		return 0, ""
	}
	var text sql.NullString
	_ = db.QueryRow(
		`SELECT json_extract(data,'$.text') FROM part WHERE message_id = ? AND json_extract(data,'$.type') = 'text' ORDER BY time_created ASC LIMIT 1`,
		id,
	).Scan(&text)
	return created, text.String
}

// openCodeActivity is the activity used for submit correlation: the time and
// prompt of the latest user message, else the session creation time.
func openCodeActivity(db *sql.DB, sid string, timeCreated int64) (int64, string) {
	if t, text := openCodeLastUserMessage(db, sid); t > 0 {
		return t, text
	}
	return timeCreated, ""
}

// openCodePromptSettle is how long a just-written session/user message whose
// prompt text is not readable yet is left undecided. OpenCode writes the
// session row, the user message and its text part in separate statements;
// deciding ownership before the text lands would fall back to "closest Enter",
// which a sibling pane pressing Enter at the same moment can win.
const openCodePromptSettle = 3 * time.Second

// openCodeSubmitOwner returns the Caw pane (leaf) that submitted the
// candidate's latest user message, or "". undecided is true when a pane did
// submit around then but the message is too fresh to judge (its prompt text is
// not written yet); the caller should retry on the next DB change instead of
// claiming or skipping for good.
func openCodeSubmitOwner(agentID, cwd string, activityMs int64, prompt string) (owner string, undecided bool) {
	at := time.UnixMilli(activityMs)
	owner = agent.PtySubmitOwner(agentID, cwd, at, prompt)
	if owner != "" && prompt == "" && time.Since(at) < openCodePromptSettle {
		// Some pane pressed Enter around then, but without the prompt text
		// the closest Enter may be the wrong pane: wait for the text part.
		return "", true
	}
	return owner, false
}

// listOpenCodeCandidates returns the top-level sessions of cwd (subagent
// sessions, which have a parent_id, are never bound: they run inside the
// parent's PTY), optionally only those updated at/after minUpdatedMs,
// ordered by orderBy.
func listOpenCodeCandidates(db *sql.DB, cwd string, minUpdatedMs int64, orderBy string) []openCodeCandidate {
	query := `SELECT id, time_created, time_updated FROM session WHERE (parent_id IS NULL OR parent_id = '') AND time_updated >= ?`
	args := []any{minUpdatedMs}
	if cwd != "" {
		query += ` AND directory = ?`
		args = append(args, cwd)
	}
	rows, err := db.Query(query+` ORDER BY `+orderBy, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []openCodeCandidate
	for rows.Next() {
		var c openCodeCandidate
		if rows.Scan(&c.id, &c.timeCreated, &c.timeUpdated) == nil {
			out = append(out, c)
		}
	}
	return out
}

// findUnclaimedOpenCodeSession picks the OpenCode session a not-yet-bound
// pane should follow, and claims it for leaf.
//
// OpenCode creates its session row lazily, on the first prompt, in a SQLite DB
// shared by every OpenCode process. When several panes run OpenCode in the same
// cwd, "a new session appeared" says nothing about which pane created it: the
// old heuristic let whichever watcher polled first claim it, so an idle,
// never-prompted pane could steal a sibling's /new session and show its work
// (and fire its notifications) while the real pane stayed idle.
//
// The session's user message is written milliseconds after the Enter that
// produced it, so ownership is decided by agent.PtySubmitOwner:
//  1. a candidate whose latest user message this pane submitted is claimed
//     (most recent first);
//  2. a candidate another pane submitted is never claimed;
//  3. only when no pane submitted around the candidate's activity (e.g.
//     `opencode --prompt`, or a session created outside Caw) the legacy
//     recency rules apply: on resume (`--continue`) the most recently updated
//     session; on a fresh launch a session created after the watcher started,
//     or an older one updated since (reattached via /sessions). A stale
//     session untouched since the watcher started is never claimed, so a fresh
//     launch does not adopt a leftover conversation.
func findUnclaimedOpenCodeSession(dbPath string, cwd string, watcherStart time.Time, agentID string, resume bool, leaf string) string {
	db, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro&_journal_mode=WAL")
	if err != nil {
		return ""
	}
	defer db.Close()

	// Only fall back to an unfiltered (all directories) scan when there is no
	// cwd to narrow by: grabbing a session from another workspace would bind
	// two agents in different workspaces to the same session.
	var minUpdated int64
	order := `time_created ASC`
	if resume {
		order = `time_updated DESC`
	} else {
		minUpdated = watcherStart.UnixMilli()
	}
	candidates := listOpenCodeCandidates(db, cwd, minUpdated, order)

	type scored struct {
		openCodeCandidate
		owner string
	}
	var mine []scored
	var orphans []scored
	for _, c := range candidates {
		if by := ClaimedBy(agentID, cwd, c.id); by != "" && by != leaf {
			continue
		}
		var prompt string
		c.activity, prompt = openCodeActivity(db, c.id, c.timeCreated)
		owner, undecided := openCodeSubmitOwner(agentID, cwd, c.activity, prompt)
		if undecided {
			continue
		}
		switch owner {
		case leaf:
			mine = append(mine, scored{c, owner})
		case "":
			orphans = append(orphans, scored{c, owner})
		}
	}
	// 1. Sessions this pane submitted to, most recent activity first.
	for len(mine) > 0 {
		best := 0
		for i := range mine {
			if mine[i].activity > mine[best].activity {
				best = i
			}
		}
		if ClaimSessionForLeaf(agentID, cwd, mine[best].id, leaf) {
			return mine[best].id
		}
		mine = append(mine[:best], mine[best+1:]...)
	}
	// 3. Legacy recency rules for sessions no pane is known to own.
	for _, c := range orphans {
		if !resume {
			if !time.UnixMilli(c.timeCreated).After(watcherStart) && !time.UnixMilli(c.timeUpdated).After(watcherStart) {
				continue
			}
		}
		if ClaimSessionForLeaf(agentID, cwd, c.id, leaf) {
			return c.id
		}
	}
	return ""
}

// findRebindOpenCodeSession detects /new, /sessions and /resume inside a
// running OpenCode pane: it returns a different top-level session of the cwd
// that received a user message more recently than the bound session AND that
// THIS pane submitted (agent.PtySubmitOwner), or "". It does not claim; the
// caller does.
//
// The submit correlation replaces the old "PTY recently active or pane
// focused" gate, which re-bound a focused idle pane to whatever session of
// the cwd was updated last — e.g. a sibling pane's conversation, or after a
// Caw restart a stale session another pane had used — so the card followed
// the wrong conversation and missed its own turns.
//
// Subagent sessions (parent_id set) are excluded: a subagent launched by the
// task tool shares the cwd and is updated while the parent waits; following
// it would report its terminal "stop" as idle (false finished push).
func findRebindOpenCodeSession(dbPath string, cwd string, agentID string, currentID string, leaf string) string {
	db, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro&_journal_mode=WAL")
	if err != nil {
		return ""
	}
	defer db.Close()

	var curCreated int64
	_ = db.QueryRow(`SELECT time_created FROM session WHERE id = ?`, currentID).Scan(&curCreated)
	curActivity, _ := openCodeActivity(db, currentID, curCreated)

	var bestID string
	var bestActivity int64
	for _, c := range listOpenCodeCandidates(db, cwd, curActivity, `time_updated DESC`) {
		if c.id == currentID {
			continue
		}
		if by := ClaimedBy(agentID, cwd, c.id); by != "" && by != leaf {
			continue
		}
		act, prompt := openCodeActivity(db, c.id, c.timeCreated)
		if act <= curActivity || act <= bestActivity {
			continue
		}
		if owner, undecided := openCodeSubmitOwner(agentID, cwd, act, prompt); undecided || owner != leaf {
			continue
		}
		bestID, bestActivity = c.id, act
	}
	return bestID
}

// openCodeState is the status derived from the bound session's rows.
type openCodeState struct {
	status, tool, details, title string
	// activeToolStart is the state.time.start (ms) of the running tool part,
	// 0 when none. Used to scope the PTY permission-dialog signal to the tool
	// call that is currently blocked.
	activeToolStart int64
}

func (w *OpenCodeWatcher) parseOpenCodeDB(dbPath string, cwd string, openCodeSessionID string, callback func(status, tool, details, title string)) {
	if openCodeSessionID == "" {
		callback("idle", "", "", "")
		return
	}
	if st, ok := w.readOpenCodeState(dbPath, openCodeSessionID); ok {
		callback(st.status, st.tool, st.details, st.title)
	}
}

// readOpenCodeState derives the lifecycle status of an OpenCode session from
// its latest message and that message's parts.
func (w *OpenCodeWatcher) readOpenCodeState(dbPath string, resolvedID string) (openCodeState, bool) {
	// Open in read-only WAL mode to avoid interfering with the running OpenCode process.
	db, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro&_journal_mode=WAL")
	if err != nil {
		return openCodeState{}, false
	}
	defer db.Close()

	var openCodeTitle string
	_ = db.QueryRow(`SELECT title FROM session WHERE id = ?`, resolvedID).Scan(&openCodeTitle)

	// Retrieve the user's prompt. OpenCode stores the initial prompt as the
	// first 'text' part in the session. session_input exists in the schema but
	// is not reliably populated in all versions, so we prefer the part table.
	var userPrompt string
	var firstTextPartData string
	if err := db.QueryRow(
		`SELECT data FROM part WHERE session_id = ? AND json_extract(data,'$.type') = 'text' ORDER BY time_created ASC LIMIT 1`,
		resolvedID,
	).Scan(&firstTextPartData); err == nil && firstTextPartData != "" {
		var p openCodePart
		if json.Unmarshal([]byte(firstTextPartData), &p) == nil && p.Text != "" {
			userPrompt = p.Text
		}
	}
	// Fallback: session_input table (populated in some OpenCode versions).
	if userPrompt == "" {
		_ = db.QueryRow(
			`SELECT prompt FROM session_input WHERE session_id = ? ORDER BY time_created ASC LIMIT 1`,
			resolvedID,
		).Scan(&userPrompt)
	}
	userPrompt = CleanPrompt(userPrompt)

	sessionTitle := openCodeTitle
	if sessionTitle == "" {
		sessionTitle = userPrompt
	}
	idle := openCodeState{status: "idle", title: sessionTitle}

	// 1. Get the latest message in the session.
	var msgID string
	var msgDataJSON string
	err = db.QueryRow(
		`SELECT id, data FROM message WHERE session_id = ? ORDER BY time_created DESC LIMIT 1`,
		resolvedID,
	).Scan(&msgID, &msgDataJSON)
	if err != nil {
		return idle, true
	}

	var msg openCodeMessage
	if err := json.Unmarshal([]byte(msgDataJSON), &msg); err != nil {
		return idle, true
	}

	// 2. Query all parts associated with this latest message.
	var parts []openCodePart
	if rows, err := db.Query(`SELECT data FROM part WHERE message_id = ? ORDER BY time_created ASC`, msgID); err == nil {
		for rows.Next() {
			var partData string
			if rows.Scan(&partData) == nil {
				var p openCodePart
				if json.Unmarshal([]byte(partData), &p) == nil {
					parts = append(parts, p)
				}
			}
		}
		rows.Close()
	}

	st := deriveOpenCodeState(msg, parts)
	st.title = sessionTitle
	return st, true
}

// deriveOpenCodeState maps the latest message of a session (and its parts) to
// a lifecycle status. Title is filled in by the caller.
func deriveOpenCodeState(msg openCodeMessage, parts []openCodePart) openCodeState {
	switch msg.Role {
	case "user":
		// User just sent a message — agent is thinking/preparing response.
		return openCodeState{status: "thinking"}
	case "assistant":
	default:
		return openCodeState{status: "idle"}
	}

	var hasQuestion bool
	var activeTool string
	var activeToolStart int64
	var lastToolName string
	// failedTool/failedErr track the most recent tool part whose state is
	// "error".
	var failedTool string
	var failedErr string
	for _, p := range parts {
		if p.Type != "tool" {
			continue
		}
		if p.Tool != "" {
			lastToolName = p.Tool
		}
		toolStatus := ""
		if p.State != nil {
			toolStatus = p.State.Status
		}
		switch toolStatus {
		case "running", "pending":
			if p.Tool == "question" {
				hasQuestion = true
			} else {
				activeTool = p.Tool
				if p.State.Time.Start > 0 {
					activeToolStart = p.State.Time.Start
				}
			}
		case "error":
			failedTool = p.Tool
			failedErr = p.State.Error
		}
	}

	if hasQuestion {
		return openCodeState{status: "waiting_input", tool: "question"}
	}
	if activeTool != "" {
		return openCodeState{status: "executing", tool: activeTool, activeToolStart: activeToolStart}
	}

	// The user aborted the turn (ESC twice). Report "interrupted" (not idle)
	// so the UI surfaces it with a red dot and no push is sent.
	if msg.Error != nil && msg.Error.Name == "MessageAbortedError" {
		return openCodeState{status: "interrupted", tool: failedTool}
	}
	// The user dismissed an open "question" prompt or rejected a permission
	// request. OpenCode marks the tool part as error ("The user dismissed this
	// question" / "The user rejected permission to use this specific tool
	// call.") and stops the turn — no new rows are written until the user
	// prompts again. The message finish stays "tool-calls", so the generic
	// branches below would leave the card stuck in Working forever.
	if failedTool != "" && isUserRejectedToolError(failedTool, failedErr) {
		if failedErr == "" {
			failedErr = "question dismissed"
		}
		return openCodeState{status: "interrupted", tool: failedTool, details: failedErr}
	}
	// The turn ended with a provider/API error (e.g. HTTP 400, auth, context
	// overflow): OpenCode finalizes the assistant message with an error and no
	// finish reason and stops. Without this the empty-finish branch below
	// reported "thinking" forever. The agent is no longer working, so report
	// "interrupted" (idle column, red dot) with the error text.
	if msg.Error != nil && msg.Finish == "" && msg.Time.Completed != 0 {
		return openCodeState{status: "interrupted", tool: failedTool, details: msg.Error.text()}
	}
	// A tool call failed and the agent is about to process the failure (the
	// step finished with "tool-calls", or is still being written): surface it
	// with a red dot while the turn keeps running.
	if failedTool != "" && (msg.Finish == "" || msg.Finish == "tool-calls") {
		if failedErr == "" {
			failedErr = "tool call failed"
		}
		return openCodeState{status: "tool_failed", tool: failedTool, details: failedErr}
	}

	// If no tool is actively running/pending but the turn expects more tool calls:
	if msg.Finish == "tool-calls" {
		if lastToolName != "" {
			return openCodeState{status: "executing", tool: lastToolName}
		}
		return openCodeState{status: "thinking"}
	}

	// Empty finish means the message row is still being written to — the
	// turn is in progress but the tool state hasn't been updated yet (or a new
	// step is about to start). Reporting "idle" here causes the status to
	// flash idle→executing repeatedly, so treat it as working.
	if msg.Finish == "" {
		// A shell command executed with "!" (e.g. "!git status") is written
		// by OpenCode as an assistant message with a single bash tool part and
		// no finish reason: the command runs directly in the PTY, not through
		// the model, so the message is finalized (time.completed set) without
		// finish. A mid-flight LLM turn never has time.completed set, so this
		// only matches finished shell commands.
		if msg.Time.Completed != 0 && lastToolName != "" {
			return openCodeState{status: "idle"}
		}
		if lastToolName != "" {
			return openCodeState{status: "executing", tool: lastToolName}
		}
		return openCodeState{status: "thinking"}
	}

	// Otherwise, the assistant turn is complete (finish = "stop" or other).
	// Only the canonical "question" tool (handled above) signals waiting_input;
	// scanning the assistant text for confirmation keywords produces false
	// positives, so it is not done.
	var textContent string
	for _, p := range parts {
		if p.Type == "text" {
			textContent = p.Text
		}
	}
	return openCodeState{status: "idle", details: textContent}
}

// isUserRejectedToolError reports whether a failed tool part was cancelled by
// the user rather than failing on its own:
//   - "question" dismissed with ESC ("The user dismissed this question"), or
//     an equivalent "aborted" wording;
//   - any tool whose permission request the user rejected ("The user rejected
//     permission to use this specific tool call.").
//
// Deliberately narrow: an invalid-arguments schema error keeps the model
// working, so it must not be treated as a cancellation.
func isUserRejectedToolError(tool, err string) bool {
	lower := strings.ToLower(err)
	if strings.Contains(lower, "rejected permission") {
		return true
	}
	return tool == "question" && isDismissedQuestionError(err)
}

// isDismissedQuestionError reports whether the error on a "question" tool
// part means the user cancelled the question rather than answered it.
// OpenCode writes "The user dismissed this question" when the user escapes
// the prompt; "aborted" covers the equivalent user-initiated cancellation
// wording on other agents.
func isDismissedQuestionError(err string) bool {
	lower := strings.ToLower(err)
	return strings.Contains(lower, "dismissed") || strings.Contains(lower, "aborted")
}
