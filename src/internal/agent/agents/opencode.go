package agents

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
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
	} `json:"state,omitempty"`
	Text string `json:"text,omitempty"`
}

// openCodeMessage mirrors the JSON blob stored in the message.data column.
type openCodeMessage struct {
	Role   string `json:"role"`
	Finish string `json:"finish,omitempty"`
	Parts  []struct {
		Type string `json:"type"`
		Text string `json:"text,omitempty"`
	} `json:"parts,omitempty"`
	Error *struct {
		Name string `json:"name"`
	} `json:"error,omitempty"`
}

func (w *OpenCodeWatcher) Watch(ctx context.Context, sessionID string, cwd string, resume bool, callback func(status, tool, details, title string), heartbeat func()) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	home, _ := os.UserHomeDir()
	dbPath := filepath.Join(home, ".local", "share", "opencode", "opencode.db")
	if _, err := os.Stat(dbPath); err != nil {
		appData := os.Getenv("APPDATA")
		if appData != "" {
			dbPath = filepath.Join(appData, "opencode", "opencode.db")
		}
	}

	walPath := dbPath + "-wal"

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
	// Re-bind bookkeeping for /new and /resume detection.
	var silentTicks int
	// lastBoundUpdated tracks the time_updated of the currently bound session
	// row. Because OpenCode stores all sessions in a single shared DB, the
	// db-changed detector fires on writes to ANY session. Comparing the bound
	// row's time_updated before and after a DB change lets us distinguish "our"
	// session (boundAdvanced) from a sibling session (otherSessionActive).
	var lastBoundUpdated int64
	var otherSessionActive bool

	// fsnotify-based immediate change notifier on the SQLite DB and WAL
	// files. Reacts to writes in ~tens of ms; the 2s ticker acts as a
	// fallback for missed events and drives heartbeat/re-bind.
	var notifyCh <-chan struct{}
	notifier, nerr := NewFileChangeNotifier()
	if nerr == nil {
		defer notifier.Close()
		notifyCh = notifier.Notify()
		// Watch the DB file; if it doesn't exist yet, the notifier will
		// watch the nearest existing ancestor dir and promote on create.
		notifier.Watch(dbPath)
	}

	defer func() {
		if openCodeSessionID != "" {
			UnclaimSession(agentID, cwd, openCodeSessionID)
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
		if openCodeSessionID == "" {
			if changed {
				openCodeSessionID = findUnclaimedOpenCodeSession(dbPath, cwd, watcherStart, agentID, resume)
				if openCodeSessionID != "" {
					silentTicks = 0
					lastBoundUpdated = openCodeSessionUpdated(dbPath, openCodeSessionID)
					agent.RecordExternalSession(sessionID, openCodeSessionID)
				}
			}
		}

		// Always re-parse while waiting for user input even if the DB
		// hasn't changed. This keeps lastActivity alive in the watchdog
		// so the idle-timeout never fires while a question is pending.
		if changed || lastReportedStatus == "waiting_input" {
			if openCodeSessionID != "" {
				// Determine whether this DB change advanced the bound session
				// or a sibling. All sessions share one SQLite DB, so a
				// modtime change alone is ambiguous — we must compare the
				// bound row's time_updated to disambiguate.
				boundAdvanced := false
				if changed {
					cur := openCodeSessionUpdated(dbPath, openCodeSessionID)
					if cur != lastBoundUpdated {
						boundAdvanced = true
						lastBoundUpdated = cur
					} else {
						// DB changed but the bound session didn't — another
						// session (e.g. one switched to via /session) got a
						// write. Mark it so the rebind pass can fire.
						otherSessionActive = true
					}
				}

				before := lastReportedStatus
				wrappedCallback := func(status, tool, details, title string) {
					lastReportedStatus = status
					callback(status, tool, details, title)
				}
				w.parseOpenCodeDB(dbPath, cwd, openCodeSessionID, wrappedCallback)
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

			// Mid-session re-bind for /new, /resume, and /session.
			// Fires when the bound session has been silent long enough OR
			// when the DB changed due to a sibling session (OpenCode stores
			// all sessions in a single SQLite DB, so writes to a session
			// switched to via /session would otherwise reset silentTicks
			// and prevent re-binding).
			//
			// Gated on PTY activity OR user focus: OpenCode keeps every
			// session for a cwd in one shared SQLite DB, so a DB modtime
			// change alone is ambiguous — it may come from a sibling agent
			// running in a different PTY. Only re-bind when *this* watcher's
			// PTY has produced output recently (indicating the agent process
			// in this PTY is the one that wrote, e.g. the user issued /new or
			// /resume here), OR when this pane currently has the user's focus
			// (the user issued /new or /resume here and is waiting for the
			// agent to start, which may not produce PTY bytes immediately).
			// Without this gate, when a sibling agent in the same cwd keeps
			// working after this one goes idle, the idle watcher would steal
			// the sibling's session and briefly flip back to "working" with
			// the sibling's task under this card.
			if openCodeSessionID != "" && (silentTicks >= rebindSilenceTicks || otherSessionActive) {
				focused := agent.IsPtyFocused(sessionID)
				lastPtyOut := agent.LastPtyActivity(sessionID)
				ptyRecent := time.Since(lastPtyOut) < 3*time.Second
				if ptyRecent || focused {
					newKey := findRebindOpenCodeSession(dbPath, cwd, agentID, openCodeSessionID)
					if newKey != "" && newKey != openCodeSessionID {
						if ClaimSession(agentID, cwd, newKey) {
							UnclaimSession(agentID, cwd, openCodeSessionID)
							openCodeSessionID = newKey
							lastReportedStatus = ""
							silentTicks = 0
							otherSessionActive = false
							lastBoundUpdated = openCodeSessionUpdated(dbPath, openCodeSessionID)
							agent.RecordExternalSession(sessionID, openCodeSessionID)
						}
					} else {
						otherSessionActive = false
					}
				} else {
					// PTY silent and not focused: the DB movement came from a
					// sibling, not from a /new or /resume issued here. Drop
					// the flag so we stop re-evaluating the re-bind every
					// tick while our own PTY stays quiet.
					otherSessionActive = false
				}
			}
		}
	}
}

// openCodeSessionUpdated returns the time_updated column of the given session
// row, or 0 if the query fails. Used by processState to tell whether a DB
// modtime change corresponds to the bound session (boundAdvanced) or a
// sibling session (otherSessionActive) in the shared SQLite database.
func openCodeSessionUpdated(dbPath, sid string) int64 {
	db, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro&_journal_mode=WAL")
	if err != nil {
		return 0
	}
	defer db.Close()
	var tu int64
	_ = db.QueryRow(`SELECT time_updated FROM session WHERE id = ?`, sid).Scan(&tu)
	return tu
}

// findUnclaimedOpenCodeSession enumerates candidate OpenCode sessions in the
// opencode.db (filtered by directory=cwd when possible) and returns the id of
// the earliest one (oldest first) that is not already claimed by another
// watcher of the same agent type+cwd.
//
// Subagent sessions (those with a non-empty parent_id) are always excluded:
// subagents run internally inside the parent's PTY and have their own message
// stream. If a parent watcher followed a subagent session, it would report the
// subagent's terminal "stop" message as "idle" — emitting a spurious finished
// notification — and then re-bind back to the parent once the parent resumes,
// producing the "idle for a second then working again" flicker.
//
// When multiple watchers compete for the same pool of sessions (because
// OpenCode creates sessions lazily on first user message, not at PTY start),
// PTY activity correlation is used to disambiguate: a watcher only claims a
// session if its PTY has produced output within a recent window, indicating
// the agent process in this PTY is the one that created the session. This
// prevents watcher 1 from claiming a session that was created by watcher 2's
// agent just because watcher 1 polled first.
//
// Recency handling differs from the file-based watchers (claude, codex, ...).
// Those filter by file mtime, which advances when a reattached old session
// receives a new message — so a /sessions reattach in a fresh agent launch is
// found naturally. OpenCode stores every session in a single shared SQLite DB,
// so there is no per-session file mtime; filtering by session.time_created
// (a row column set once at creation) would permanently exclude any session
// the user reattaches to via /sessions. Instead we use session.time_updated:
// a session whose time_updated advanced past the watcher start time was
// recently interacted with (the user sent a message or reattached via
// /sessions), so it is claimable. A stale old session that hasn't been touched
// since a previous Caw run has time_updated well before the watcher started and
// is skipped — this prevents a fresh OpenCode launch from spuriously binding
// to a leftover session just because the TUI rendered PTY bytes on startup.
func findUnclaimedOpenCodeSession(dbPath string, cwd string, watcherStart time.Time, agentID string, resume bool) string {
	db, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro&_journal_mode=WAL")
	if err != nil {
		return ""
	}
	defer db.Close()

	type row struct {
		id          string
		timeCreated int64
		timeUpdated int64
	}
	var candidates []row

	if cwd != "" {
		rows, qerr := db.Query(
			`SELECT id, time_created, time_updated FROM session WHERE directory = ? AND (parent_id IS NULL OR parent_id = '') ORDER BY time_created ASC`,
			cwd,
		)
		if qerr == nil {
			for rows.Next() {
				var r row
				rows.Scan(&r.id, &r.timeCreated, &r.timeUpdated)
				candidates = append(candidates, r)
			}
			rows.Close()
		}
	}
	if len(candidates) == 0 {
		// Only fall back to the unfiltered scan when we have no cwd to
		// narrow by. When a cwd IS set, an empty cwd-specific result means
		// no session has been created in this workspace yet — grabbing a
		// session from a different workspace here is exactly what would
		// associate two agents in different workspaces with the same
		// session. The watcher retries on the next DB change instead.
		if cwd == "" {
			rows, qerr := db.Query(
				`SELECT id, time_created, time_updated FROM session WHERE (parent_id IS NULL OR parent_id = '') ORDER BY time_created ASC`,
			)
			if qerr == nil {
				for rows.Next() {
					var r row
					rows.Scan(&r.id, &r.timeCreated, &r.timeUpdated)
					candidates = append(candidates, r)
				}
				rows.Close()
			}
		}
	}

	for _, r := range candidates {
		// On resume, skip the recency filter entirely — the session may
		// predate the watcher (the agent reattaches to an old session).
		// On fresh start:
		//   - A session created after the watcher launched is claimable with
		//     no extra gate (it was just created, most likely by this agent).
		//   - An older session is claimable only when it was recently
		//     updated (time_updated after watcherStart) — i.e. the user just
		//     interacted with it or reattached to it via /sessions. A stale
		//     old session that hasn't been touched since a previous Caw run
		//     is skipped, preventing a fresh OpenCode launch from spuriously
		//     binding to a leftover session just because the TUI rendered
		//     PTY bytes on startup. We deliberately do NOT gate on PTY
		//     activity alone: the OpenCode TUI emits control sequences on
		//     connect regardless of whether the user sent a message, so a
		//     PTY-activity gate would let every new instance claim the next
		//     stale session in the cwd and show its old title/status in Idle.
		if !resume {
			sessionCreated := time.UnixMilli(r.timeCreated)
			if !sessionCreated.After(watcherStart) {
				sessionUpdated := time.UnixMilli(r.timeUpdated)
				if !sessionUpdated.After(watcherStart) {
					continue
				}
			}
		}
		if ClaimSession(agentID, cwd, r.id) {
			return r.id
		}
	}
	return ""
}

// findRebindOpenCodeSession looks for a different OpenCode session in the same
// cwd whose time_updated is more recent than lastActivity. Used by the
// mid-session re-bind pass to detect /new and /resume. It does NOT claim the
// returned session; the caller is responsible for calling ClaimSession.
// Gates on PTY activity to ensure only the watcher whose PTY is producing
// output switches to the new session.
//
// Subagent sessions (parent_id != '') are excluded: a subagent launched by the
// current agent (e.g. via the "task" tool) shares the same directory and, while
// it runs, has a more recent time_updated than the blocked parent. Without this
// guard the parent watcher would re-bind to the subagent, report its terminal
// "stop" as "idle" (firing a false finished notification), then re-bind back to
// the parent once it resumes — the "idle for a second then working again"
// flicker. /new and /resume always create top-level sessions with no parent_id.
func findRebindOpenCodeSession(dbPath string, cwd string, agentID string, currentID string) string {
	db, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro&_journal_mode=WAL")
	if err != nil {
		return ""
	}
	defer db.Close()

	var currentUpdated int64
	_ = db.QueryRow(`SELECT time_updated FROM session WHERE id = ?`, currentID).Scan(&currentUpdated)

	var bestID string
	var bestTime int64

	if cwd != "" {
		rows, qerr := db.Query(
			`SELECT id, time_updated FROM session WHERE directory = ? AND (parent_id IS NULL OR parent_id = '') ORDER BY time_updated DESC`,
			cwd,
		)
		if qerr == nil {
			for rows.Next() {
				var id string
				var tu int64
				if rows.Scan(&id, &tu) != nil {
					continue
				}
				if id == currentID {
					continue
				}
				if tu > currentUpdated && tu > bestTime {
					bestID = id
					bestTime = tu
				}
			}
			rows.Close()
		}
	}
	return bestID
}

func (w *OpenCodeWatcher) parseOpenCodeDB(dbPath string, cwd string, openCodeSessionID string, callback func(status, tool, details, title string)) {
	// Open in read-only WAL mode to avoid interfering with the running OpenCode process.
	db, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro&_journal_mode=WAL")
	if err != nil {
		return
	}
	defer db.Close()

	var openCodeTitle string
	resolvedID := openCodeSessionID

	if resolvedID == "" {
		callback("idle", "", "", "")
		return
	}

	_ = db.QueryRow(
		`SELECT title FROM session WHERE id = ?`,
		resolvedID,
	).Scan(&openCodeTitle)

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

	// 1. Get the latest message in the session.
	var msgID string
	var msgDataJSON string
	err = db.QueryRow(
		`SELECT id, data FROM message WHERE session_id = ? ORDER BY time_created DESC LIMIT 1`,
		resolvedID,
	).Scan(&msgID, &msgDataJSON)
	if err != nil {
		callback("idle", "", "", sessionTitle)
		return
	}

	var msg openCodeMessage
	if err := json.Unmarshal([]byte(msgDataJSON), &msg); err != nil {
		callback("idle", "", "", sessionTitle)
		return
	}

	// 2. Query all parts associated with this latest message.
	rows, err := db.Query(
		`SELECT data FROM part WHERE message_id = ? ORDER BY time_created ASC`,
		msgID,
	)
	var parts []openCodePart
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var partData string
			if rows.Scan(&partData) == nil {
				var p openCodePart
				if json.Unmarshal([]byte(partData), &p) == nil {
					parts = append(parts, p)
				}
			}
		}
	}

	// 3. Determine status based on message role, finish reason, and part states.
	switch msg.Role {
	case "user":
		// User just sent a message — agent is thinking/preparing response.
		callback("thinking", "", "", sessionTitle)

	case "assistant":
		// Check for any currently running/pending tool calls.
		var hasQuestion bool
		var activeTool string
		var lastToolName string

		for _, p := range parts {
			if p.Type == "tool" {
				if p.Tool != "" {
					lastToolName = p.Tool
				}
				toolStatus := ""
				if p.State != nil {
					toolStatus = p.State.Status
				}
				if toolStatus == "running" || toolStatus == "pending" {
					if p.Tool == "question" {
						hasQuestion = true
					} else {
						activeTool = p.Tool
					}
				}
			}
		}

		if hasQuestion {
			callback("waiting_input", "question", "", sessionTitle)
			return
		}
		if activeTool != "" {
			callback("executing", activeTool, "", sessionTitle)
			return
		}

		// If no tool is actively running/pending but the turn expects more tool calls:
		if msg.Finish == "tool-calls" {
			if lastToolName != "" {
				callback("executing", lastToolName, "", sessionTitle)
			} else {
				callback("thinking", "", "", sessionTitle)
			}
			return
		}

		// Empty finish means the message row is still being written to — the
		// turn is in progress but the tool state hasn't been updated yet (or
		// a new step is about to start). Reporting "idle" here causes the
		// status to flash idle→executing repeatedly. Treat an unfinished
		// assistant message as "thinking" (actively working) instead.
		if msg.Finish == "" {
			if msg.Error != nil && msg.Error.Name == "MessageAbortedError" {
				callback("idle", "", "", sessionTitle)
				return
			}
			if lastToolName != "" {
				callback("executing", lastToolName, "", sessionTitle)
			} else {
				callback("thinking", "", "", sessionTitle)
			}
			return
		}

	// Otherwise, the assistant turn is complete (finish = "stop" or "completed").
	// Only the canonical "question" tool (handled above) signals waiting_input;
	// scanning the assistant text for confirmation keywords produces false
	// positives (e.g. planning prose that happens to contain "confirm" or
	// "approve"), so it has been removed.
	var textContent string
	for _, p := range parts {
		if p.Type == "text" {
			textContent = p.Text
		}
	}
	callback("idle", "", textContent, sessionTitle)

	default:
		callback("idle", "", "", sessionTitle)
	}
}
