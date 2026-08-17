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

func (w *HermesWatcher) Watch(ctx context.Context, sessionID string, cwd string, resume bool, callback func(status, tool, details, title string), heartbeat func()) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	home, _ := os.UserHomeDir()
	dbPath := filepath.Join(home, ".hermes", "state.db")
	if _, err := os.Stat(dbPath); err != nil {
		appData := os.Getenv("APPDATA")
		if appData != "" {
			dbPath = filepath.Join(appData, "hermes", "state.db")
		}
	}

	walPath := dbPath + "-wal"

	const agentID = "hermes"
	// Hermes does not record cwd in its session rows for TUI chats (the
	// column is left empty), so the watcher cannot narrow the search by
	// workspace the way the OpenCode watcher does. Claims are therefore keyed
	// globally per agent — every Hermes pane shares one claim namespace so
	// two panes never bind the same internal session regardless of their
	// Caw-side cwd. We pass a fixed sentinel ("") as the claim cwd.
	const claimCwd = ""
	var lastDBMod time.Time
	var lastWALMod time.Time
	var lastReportedStatus string
	var hermesSessionID string
	// For a fresh start, only match sessions created after the watcher
	// started (a small negative offset tolerates the delay between PTY
	// launch and the session row insert) to avoid grabbing a sibling agent's
	// session. On resume, the session may predate the watcher so the recency
	// filter is skipped in findUnclaimedHermesSession.
	watcherStart := time.Now().Add(-10 * time.Second)
	// Re-bind bookkeeping for /new and --continue / --resume detection.
	var silentTicks int
	// lastBoundStarted tracks the started_at of the currently bound session
	// row. Because Hermes stores all sessions in a single shared DB, the
	// db-changed detector fires on writes to ANY session. Comparing the bound
	// row's started_at (set once at creation) doesn't advance, so we instead
	// track the latest message timestamp to tell whether the bound session
	// advanced or a sibling wrote.
	var lastBoundMsgTime float64
	var otherSessionActive bool
	// PTY-interrupt detection. The Hermes TUI maps Ctrl+C during a busy turn
	// to session.interrupt, but it does NOT reliably persist an "Operation
	// interrupted" message row: an interrupted turn usually leaves only a
	// finish_reason-less partial assistant row, or nothing new at all, so
	// transcript-based detection misses most interrupts and the card would
	// sit in "Working" until the idle-timeout reverts it to "Idle" minutes
	// later as if it had finished. We therefore also watch for the Ctrl+C
	// byte the user sends into the Hermes PTY and report "interrupted"
	// immediately while the bound turn is working. interruptApplied stays
	// sticky until the user submits a new prompt (the latest row becomes a
	// fresh user message), because the transcript may keep showing the
	// pre-interrupt working state for a while. interruptBoundaryID records
	// the messages.id at the moment the interrupt was applied so a sticky
	// interrupt is only cleared by a genuinely NEW user message (a higher id)
	// — not by the pre-interrupt user prompt row, which stays the latest row
	// for the entire interrupted turn (Hermes writes no new row on abort).
	var lastInterruptSeen time.Time
	var interruptApplied bool
	var interruptBoundaryID int64

	var notifyCh <-chan struct{}
	notifier, nerr := NewFileChangeNotifier()
	if nerr == nil {
		defer notifier.Close()
		notifyCh = notifier.Notify()
		notifier.Watch(dbPath)
	}

	defer func() {
		if hermesSessionID != "" {
			UnclaimSession(agentID, claimCwd, hermesSessionID)
		}
	}()

	dbChanged := func() bool {
		changed := false
		if info, err := os.Stat(dbPath); err == nil {
			if info.ModTime() != lastDBMod {
				changed = true
				lastDBMod = info.ModTime()
			}
		}
		if walInfo, err := os.Stat(walPath); err == nil {
			if walInfo.ModTime() != lastWALMod {
				changed = true
				lastWALMod = walInfo.ModTime()
			}
		}
		return changed
	}

	processState := func(changed bool) {
		if hermesSessionID == "" {
			if changed {
				// A reopened pane must resume tracking its exact prior session
				// (set by resumeCmdForAgent via `hermes --resume <id>`). Bind
				// to the persisted id first, never a heuristic scan over the
				// shared DB, so two panes never swap their bound sessions.
				hermesSessionID = bindHermesInitial(dbPath, watcherStart, agentID, resume, sessionID)
				if hermesSessionID != "" {
					silentTicks = 0
					lastBoundMsgTime = hermesLastMessageTime(dbPath, hermesSessionID)
					agent.RecordExternalSession(sessionID, hermesSessionID)
				}
			}
		}

		// Always re-parse while waiting for user input even if the DB
		// hasn't changed. This keeps lastActivity alive in the watchdog
		// so the idle-timeout never fires while a question is pending.
		if changed || lastReportedStatus == "waiting_input" {
			if hermesSessionID != "" {
				boundAdvanced := false
				if changed {
					cur := hermesLastMessageTime(dbPath, hermesSessionID)
					if cur != lastBoundMsgTime {
						boundAdvanced = true
						lastBoundMsgTime = cur
					} else {
						otherSessionActive = true
					}
				}

				// If a PTY interrupt is pending, keep the card in
				// "interrupted" regardless of what the transcript shows:
				// Hermes may persist the pre-interrupt working state
				// (e.g. a finish_reason-less partial assistant row) that
				// would otherwise read back as "thinking"/"executing".
				// The interrupt only clears once the user submits a NEW
				// prompt, which lands as a fresh user message whose id is
				// greater than interruptBoundaryID. Checking merely that
				// the latest role is "user" would clear the interrupt
				// immediately: during an interrupted turn the latest row
				// is still the user's pre-interrupt prompt (Hermes writes
				// no new row on abort).
				if interruptApplied && hermesLatestRole(dbPath, hermesSessionID) == "user" && hermesLastMessageID(dbPath, hermesSessionID) > interruptBoundaryID {
					interruptApplied = false
				}

				before := lastReportedStatus
				wrappedCallback := func(status, tool, details, title string) {
					if interruptApplied && (status == "thinking" || status == "executing" || status == "tool_failed") {
						status = "interrupted"
					}
					lastReportedStatus = status
					callback(status, tool, details, title)
				}
				w.parseHermesDB(dbPath, hermesSessionID, wrappedCallback)
				if before != lastReportedStatus || boundAdvanced {
					silentTicks = 0
					if boundAdvanced {
						otherSessionActive = false
					}
				} else {
					silentTicks++
				}
			} else {
				silentTicks++
			}
		} else {
			silentTicks++
		}
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-notifyCh:
			processState(dbChanged())
		case <-ticker.C:
			heartbeat()
			processState(dbChanged())

			// PTY-interrupt detection (see the interruptApplied comment
			// above): a fresh Ctrl+C while the bound turn is working
			// immediately flips the card to "interrupted" instead of waiting
			// for a transcript marker that Hermes may never write.
			if hermesSessionID != "" {
				last := agent.LastPtyInterrupt(sessionID)
				if last.After(lastInterruptSeen) {
					lastInterruptSeen = last
					if lastReportedStatus == "thinking" || lastReportedStatus == "executing" || lastReportedStatus == "tool_failed" {
						interruptApplied = true
						lastReportedStatus = "interrupted"
						interruptBoundaryID = hermesLastMessageID(dbPath, hermesSessionID)
						callback("interrupted", "", "", hermesSessionTitle(dbPath, hermesSessionID))
					}
				}
			}

			// Mid-session re-bind for /new, --continue, and --resume. Fires
			// when the bound session has been silent long enough OR when the
			// DB changed due to a sibling session (Hermes stores all sessions
			// in a single shared SQLite DB, so writes to a session switched
			// to via --resume would otherwise reset silentTicks and prevent
			// re-binding).
			//
			// Gated on PTY activity OR user focus, mirroring the OpenCode
			// watcher: only the watcher whose PTY is producing output (or
			// whose pane the user is currently driving) switches, so a sibling
			// Hermes in a different PTY writing to its own session can't make
			// this idle, unfocused watcher steal its session.
			if hermesSessionID != "" && (silentTicks >= rebindSilenceTicks || otherSessionActive) {
				focused := agent.IsPtyFocused(sessionID)
				lastPtyOut := agent.LastPtyActivity(sessionID)
				ptyRecent := time.Since(lastPtyOut) < 3*time.Second
				if ptyRecent || focused {
					newKey := findRebindHermesSession(dbPath, agentID, hermesSessionID)
					if newKey != "" && newKey != hermesSessionID {
						if ClaimSessionForLeaf(agentID, claimCwd, newKey, sessionID) {
							UnclaimSession(agentID, claimCwd, hermesSessionID)
							hermesSessionID = newKey
							lastReportedStatus = ""
							silentTicks = 0
							otherSessionActive = false
							lastBoundMsgTime = hermesLastMessageTime(dbPath, hermesSessionID)
							agent.RecordExternalSession(sessionID, hermesSessionID)
						}
					} else {
						otherSessionActive = false
					}
				} else {
					otherSessionActive = false
				}
			}
		}
	}
}

// hermesLastMessageTime returns the timestamp of the most recent message row
// for the given session, or 0 if the query fails. Used by processState to tell
// whether a DB modtime change corresponds to the bound session (boundAdvanced)
// or a sibling session (otherSessionActive) in the shared SQLite database.
func hermesLastMessageTime(dbPath, sid string) float64 {
	db, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro&_journal_mode=WAL")
	if err != nil {
		return 0
	}
	defer db.Close()
	var ts float64
	_ = db.QueryRow(`SELECT timestamp FROM messages WHERE session_id = ? ORDER BY id DESC LIMIT 1`, sid).Scan(&ts)
	return ts
}

// hermesLatestRole returns the role of the most recent message row for the
// given session, or "" when the query fails. Used by processState to clear a
// sticky PTY-interrupt once the user submits a new prompt (latest row = "user").
func hermesLatestRole(dbPath, sid string) string {
	db, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro&_journal_mode=WAL")
	if err != nil {
		return ""
	}
	defer db.Close()
	var role string
	_ = db.QueryRow(`SELECT role FROM messages WHERE session_id = ? ORDER BY id DESC LIMIT 1`, sid).Scan(&role)
	return role
}

// hermesLastMessageID returns the id of the most recent message row for the
// given session, or 0 if the query fails. Used to tell whether a "user" role
// row is a genuinely new prompt (id greater than interruptBoundaryID) rather
// than the pre-interrupt prompt row that stays the latest row for the whole
// interrupted turn.
func hermesLastMessageID(dbPath, sid string) int64 {
	db, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro&_journal_mode=WAL")
	if err != nil {
		return 0
	}
	defer db.Close()
	var id int64
	_ = db.QueryRow(`SELECT id FROM messages WHERE session_id = ? ORDER BY id DESC LIMIT 1`, sid).Scan(&id)
	return id
}

// hermesSessionTitle returns the display title for the given session: the
// auto-generated sessions.title when present, otherwise the first user message
// text via CleanPrompt.
func hermesSessionTitle(dbPath, sid string) string {
	db, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro&_journal_mode=WAL")
	if err != nil {
		return ""
	}
	defer db.Close()

	var sessionTitle string
	_ = db.QueryRow(`SELECT title FROM sessions WHERE id = ?`, sid).Scan(&sessionTitle)
	sessionTitle = CleanPrompt(sessionTitle)

	// Fallback: derive the title from the first user message. Hermes injects
	// long system-reminder blocks as user-role messages for skills like "plan";
	// CleanPrompt strips control sequences and trims, so the result is still
	// reasonable when the auto-title is empty.
	if sessionTitle == "" {
		var firstUser string
		_ = db.QueryRow(
			`SELECT content FROM messages WHERE session_id = ? AND role = 'user' ORDER BY id ASC LIMIT 1`,
			sid,
		).Scan(&firstUser)
		sessionTitle = CleanPrompt(firstUser)
	}
	return sessionTitle
}

// bindHermesInitial selects the initial Hermes session to bind for a leaf.
// It prefers the exact native session id persisted for the leaf by a previous
// Caw process (so a reopened pane resumes its own conversation), and only
// falls back to a candidate scan for a fresh launch. The exact id is validated
// to still be a live session before claiming.
func bindHermesInitial(dbPath string, watcherStart time.Time, agentID string, resume bool, leaf string) string {
	if exact := agent.PersistedExternalSession(leaf); exact != "" && hermesSessionLive(dbPath, exact) {
		if ClaimSessionForLeaf(agentID, "", exact, leaf) {
			return exact
		}
	}
	return findUnclaimedHermesSession(dbPath, watcherStart, agentID, resume)
}

// hermesSessionLive reports whether the given session id exists and has not
// ended, so a persisted id that no longer exists is not resurrected.
func hermesSessionLive(dbPath, sid string) bool {
	if sid == "" {
		return false
	}
	db, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro&_journal_mode=WAL")
	if err != nil {
		return false
	}
	defer db.Close()
	var ended sql.NullTime
	err = db.QueryRow(`SELECT ended_at FROM sessions WHERE id = ?`, sid).Scan(&ended)
	return err == nil && !ended.Valid
}

// findUnclaimedHermesSession enumerates candidate Hermes sessions in the
// state.db and returns the id of the earliest live one (ended_at IS NULL,
// ordered by started_at) that is not already claimed by another watcher.
//
// Hermes does not record the working directory on its session rows for TUI
// chats, so unlike the OpenCode watcher we cannot filter by cwd. Instead we
// rely on liveness (ended_at IS NULL) plus a recency gate on started_at: a
// fresh launch only claims sessions created after the watcher started, so a
// leftover live session from a previous Caw run is not grabbed. On resume
// (--continue / --resume) the recency filter is skipped because the
// reattached session may predate the watcher.
//
// Claims are global per agent (see claimCwd in Watch) so two Hermes panes —
// even in different cwds — never bind the same internal session.
func findUnclaimedHermesSession(dbPath string, watcherStart time.Time, agentID string, resume bool) string {
	db, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro&_journal_mode=WAL")
	if err != nil {
		return ""
	}
	defer db.Close()

	type row struct {
		id        string
		startedAt float64
	}
	var candidates []row

	rows, qerr := db.Query(
		`SELECT id, started_at FROM sessions WHERE ended_at IS NULL ORDER BY started_at ASC`,
	)
	if qerr == nil {
		for rows.Next() {
			var r row
			rows.Scan(&r.id, &r.startedAt)
			candidates = append(candidates, r)
		}
		rows.Close()
	}

	for _, r := range candidates {
		if !resume {
			started := time.Unix(0, int64(r.startedAt*float64(time.Second)))
			if !started.After(watcherStart) {
				continue
			}
		}
		if ClaimSession(agentID, "", r.id) {
			return r.id
		}
	}
	return ""
}

// findRebindHermesSession looks for a different live Hermes session whose
// started_at is more recent than the currently bound session's started_at.
// Used by the mid-session re-bind pass to detect /new, --continue, and
// --resume. It does NOT claim the returned session; the caller is responsible
// for calling ClaimSession. Gates on PTY activity to ensure only the watcher
// whose PTY is producing output switches to the new session.
func findRebindHermesSession(dbPath, agentID, currentID string) string {
	db, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro&_journal_mode=WAL")
	if err != nil {
		return ""
	}
	defer db.Close()

	var currentStarted float64
	_ = db.QueryRow(`SELECT started_at FROM sessions WHERE id = ?`, currentID).Scan(&currentStarted)

	var bestID string
	var bestStarted float64
	rows, qerr := db.Query(
		`SELECT id, started_at FROM sessions WHERE ended_at IS NULL ORDER BY started_at DESC`,
	)
	if qerr == nil {
		for rows.Next() {
			var id string
			var started float64
			if rows.Scan(&id, &started) != nil {
				continue
			}
			if id == currentID {
				continue
			}
			if started > currentStarted && started > bestStarted {
				bestID = id
				bestStarted = started
			}
		}
		rows.Close()
	}
	return bestID
}

// parseHermesDB reads the latest state of the bound Hermes session from the
// SQLite database and derives the Caw status from the most recent message row.
//
// Hermes writes messages to the messages table incrementally as a turn
// progresses, so polling the latest row yields live status transitions:
//
//   - role=user           → thinking (a new prompt just arrived)
//   - role=tool           → thinking (a tool just returned; the agent continues)
//   - role=assistant, finish_reason=tool_calls
//   - last tool is clarify (or any user-input tool) → waiting_input
//   - otherwise                                   → executing
//   - role=assistant, finish_reason=stop               → idle (turn complete)
//   - role=assistant, finish_reason="" (still generating)
//   - content starts with "Operation interrupted"  → idle (aborted)
//   - otherwise                                    → thinking
//
// The session title comes from sessions.title (Hermes auto-generates one),
// falling back to the first user message text via CleanPrompt.
func (w *HermesWatcher) parseHermesDB(dbPath, sid string, callback func(status, tool, details, title string)) {
	db, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro&_journal_mode=WAL")
	if err != nil {
		return
	}
	defer db.Close()

	if sid == "" {
		callback("idle", "", "", "")
		return
	}

	sessionTitle := hermesSessionTitle(dbPath, sid)

	var role, content, toolCallsJSON, finishReason, toolName string
	if err := db.QueryRow(
		`SELECT role, COALESCE(content,''), COALESCE(tool_calls,''), COALESCE(finish_reason,''), COALESCE(tool_name,'') FROM messages WHERE session_id = ? ORDER BY id DESC LIMIT 1`,
		sid,
	).Scan(&role, &content, &toolCallsJSON, &finishReason, &toolName); err != nil {
		callback("idle", "", "", sessionTitle)
		return
	}

	status, tool := hermesStatusForMessage(role, content, toolCallsJSON, finishReason)
	// For a failed tool result, report the tool name (from the tool_name
	// column) and the error text from the content JSON as the details.
	if status == "tool_failed" {
		errText := hermesToolErrorText(content)
		if tool == "" {
			tool = toolName
		}
		callback(status, tool, errText, sessionTitle)
		return
	}
	callback(status, tool, "", sessionTitle)
}

// hermesStatusForMessage derives the Caw working/idle state from a single
// Hermes message row. It returns an empty status string only when the row is
// not meaningful (shouldn't happen with the latest-row query, but kept
// defensive).
func hermesStatusForMessage(role, content, toolCallsJSON, finishReason string) (status, tool string) {
	roleLower := strings.ToLower(role)

	switch roleLower {
	case "user":
		return "thinking", ""
	case "tool":
		// A tool result was just posted — the agent will continue, so it is
		// thinking/working on the next step. If the result content JSON
		// carries an "error" field, the tool call failed; surface it as
		// tool_failed with the tool name (passed in via the caller) so the
		// UI shows a red dot and the error text.
		if hermesToolContentHasError(content) {
			return "tool_failed", ""
		}
		return "thinking", ""
	case "assistant", "agent":
		// An aborted turn (Ctrl+C during generation) is reported by Hermes as
		// an assistant message whose content begins with "Operation
		// interrupted" and which carries no finish_reason. Treat that as
		// "interrupted" (not idle) so the UI surfaces it with a red dot and
		// no push notification is sent.
		if strings.HasPrefix(strings.TrimSpace(content), "Operation interrupted") {
			return "interrupted", ""
		}
		finishLower := strings.ToLower(finishReason)
		if finishLower == "aborted" {
			return "interrupted", ""
		}
		if finishLower == "tool_calls" {
			toolName := lastHermesToolName(toolCallsJSON)
			if toolName == "" {
				return "thinking", ""
			}
			// User-input tools (clarify, ask, ...) block the agent until the
			// human answers. Without this the card would stay in Working
			// forever instead of moving to Needs Input.
			if isUserInputTool(strings.ToLower(toolName)) {
				return "waiting_input", toolName
			}
			return "executing", toolName
		}
		if finishLower == "stop" {
			return "idle", ""
		}
		// Empty finish_reason: the message row is still being written — the
		// turn is in progress. Reporting "idle" here would flash
		// idle→executing; treat an unfinished assistant message as thinking.
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

// hermesToolContentHasError reports whether a tool-role message's content
// JSON carries a non-empty "error" field. Hermes writes the tool result as a
// JSON object whose "error" key is populated when the tool call failed (e.g.
// "File not found: ..."), and absent/empty on success.
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
	return string(raw)
}
