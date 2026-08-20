package agent

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/04mg/caw/internal/push"
	"github.com/04mg/caw/internal/state"
	"github.com/04mg/caw/internal/terminal"
	"github.com/04mg/caw/internal/ws"
	"github.com/gorilla/websocket"
)

// AgentStatus represents the current tracked state of an agent
type AgentStatus struct {
	SessionID string `json:"sessionId"`
	AgentID   string `json:"agentId"`
	Cwd       string `json:"cwd,omitempty"`
	// Status is the live state: "thinking", "executing", "waiting_input",
	// "idle", "unknown", "interrupted", "tool_failed". "unknown" means the
	// agent process exists but Caw cannot safely classify its lifecycle — e.g.
	// the stale watchdog could not confirm the agent is still working and the
	// pane is neither producing PTY output nor focused. It is NOT "idle": the
	// agent may still be working, just not observably. "interrupted" means the
	// user cancelled the in-flight turn (e.g. pressed ESC twice) — the agent is
	// no longer working but the card stays where it was with a red dot and no
	// push notification. "tool_failed" means the agent's last tool call
	// failed — the agent keeps running, but the failure is surfaced with a
	// red dot and the error text in Details (still in the working column).
	// When the session has terminated and is being kept on the board as a
	// dismissable card, Status is set to "crashed" and EndedAt/ExitCode/
	// ExitReason are populated.
	Status    string    `json:"status"`
	Tool      string    `json:"tool,omitempty"`
	Details   string    `json:"details,omitempty"`
	Title     string    `json:"title,omitempty"`
	Timestamp time.Time `json:"timestamp"`
	// Sequence is a monotonically increasing value assigned when a session is
	// first tracked. It reflects the order in which agents were opened and is
	// used to keep a stable ordering in the UI instead of relying on the map
	// iteration order or timestamps (which can reorder when re-fetching).
	Sequence int64 `json:"sequence"`
	// Terminal-session fields. Only populated when Status == "crashed" (i.e.
	// the agent process died unexpectedly). A clean (exit 0) or user-killed
	// session is removed from the statuses map and never carries these.
	EndedAt    *time.Time `json:"endedAt,omitempty"`
	ExitCode   *int       `json:"exitCode,omitempty"`
	ExitReason string     `json:"exitReason,omitempty"`
	// LastColumn records the column the card was in just before the crash
	// (one of "working", "needs_input", "idle") so the Kanban board can keep
	// the crashed card in the column the user last saw it in.
	LastColumn string `json:"lastColumn,omitempty"`
	// ExternalSessionID is the agent's own internal session id/path that the
	// watcher is currently bound to (OpenCode session row id, Codex rollout
	// UUID, Hermes session id, ...). Empty means the watcher has not bound to
	// a native session yet. Exposed to the "agent explain" endpoint for
	// diagnostics; the normal Kanban cards key off SessionID (the leaf id)
	// and do not surface this.
	ExternalSessionID string `json:"-"`
	// Source records the status authority behind the current status: "watcher"
	// for the per-agent transcript/DB watchers, "watchdog" for the stale-state
	// revert. Used by "agent explain".
	Source string `json:"-"`
}

// Event represents a WebSocket event message
type Event struct {
	Type      string    `json:"event"`
	SessionID string    `json:"sessionId"`
	AgentID   string    `json:"agentId"`
	Cwd       string    `json:"cwd,omitempty"`
	Status    string    `json:"status,omitempty"`
	Tool      string    `json:"tool,omitempty"`
	Details   string    `json:"details,omitempty"`
	Title     string    `json:"title,omitempty"`
	Timestamp time.Time `json:"timestamp"`
	Sequence  int64     `json:"sequence"`
	// Terminal-state fields. Populated for "agent_crashed" events.
	EndedAt    *time.Time `json:"endedAt,omitempty"`
	ExitCode   *int       `json:"exitCode,omitempty"`
	ExitReason string     `json:"exitReason,omitempty"`
	LastColumn string     `json:"lastColumn,omitempty"`
}

type watcherContext struct {
	cancel    context.CancelFunc
	agentId   string
	sessionId string
	cwd       string
	cmd       []string
	resume    bool
}

// StatusWatcher is the interface that each agent status provider must implement.
// The resume flag is true when the agent was launched with a resume/continue
// flag (e.g. --continue), meaning it reattaches to a pre-existing internal
// session. In that case watchers should look for the most recent session in
// the cwd regardless of when it was last updated, instead of only sessions
// started after the watcher launched.
//
// The heartbeat callback should be called on every poll iteration (even when
// nothing changed) to signal that the watcher is alive and the agent process
// is still running. This prevents the idle-timeout watchdog from falsely
// reverting the status to "idle" during long LLM response waits where the
// underlying transcript/DB file doesn't change for minutes.
type StatusWatcher interface {
	Watch(ctx context.Context, sessionID string, cwd string, resume bool, callback func(status, tool, details, title string), heartbeat func())
}

var (
	statuses       = make(map[string]AgentStatus)
	statusesMu     sync.RWMutex
	activeSessions = make(map[string]*watcherContext)
	activeSesMu    sync.Mutex
	statusMux      *ws.Multiplexer
	statusHub      = ws.NewHub()
	watchers       = make(map[string]StatusWatcher)
	watchersMu     sync.Mutex
	// externalSessions tracks the agent's own internal session id/path bound to
	// each leaf (OpenCode session row, Codex rollout UUID, ...). It lives
	// independently of statuses because a watcher binds a native session before
	// it necessarily produces a visible status update, and the explain endpoint
	// must report the binding even then. Updated by RecordExternalSession and
	// read by updateStatus (to preserve it) and ExplainStatuses.
	externalSessions   = make(map[string]string)
	externalSessionsMu sync.RWMutex
	// statusSeq is a monotonic counter used to assign a stable opening-order
	// sequence to each agent session. It is only mutated under statusesMu.
	statusSeq int64
	// pushStore is set by SetPushStore at server startup so that agent status
	// transitions can trigger web push notifications.
	pushStore *state.Store
	// stateStore is set by SetStateStore at server startup. It is the same
	// *state.Store as pushStore in practice, but is used by
	// RecordExternalSession to persist the agent's own internal session id
	// (OpenCode session row, Codex rollout UUID, ...) so a Caw reopen can
	// resume the exact session instead of the most recent one in the cwd.
	// Kept as a separate field so the resume-by-id code path doesn't depend
	// on push being configured.
	stateStore *state.Store
	// ptyActivity tracks the last time each leaf/session id received bytes
	// from its PTY. Updated via OnPtyActivity from terminal.ReadLoop and read
	// by watchers to correlate lazily-created internal agent sessions to the
	// correct PTY when multiple agents of the same type share a cwd.
	ptyActivity   = make(map[string]time.Time)
	ptyActivityMu sync.RWMutex
	// ptyFocus tracks which leaf/session id currently has the user's focus
	// (i.e. is the active terminal pane the user is typing into). Updated via
	// OnPtyFocus when the frontend reports a focus/blur event for a pane.
	// Read by the idle-timeout watchdog and the watchers' re-bind pass:
	//   - A focused PTY is exempt from the auto-revert-to-idle even when its
	//     output has gone quiet (the user may be mid-composition or reading).
	//   - A focused PTY is allowed to re-bind to a sibling internal session
	//     even with no recent PTY output (the user issued /new or /resume and
	//     is waiting for the agent to start, which may not produce PTY bytes
	//     immediately).
	// Conversely, an unfocused PTY is more conservative: it will not steal
	// a sibling's session and the idle-timeout watchdog may revert it.
	ptyFocus   = make(map[string]bool)
	ptyFocusMu sync.RWMutex
	// ptyInterrupt records the last time the user sent the interrupt key
	// sequence (Ctrl+C, byte 0x03) into each leaf/session id's PTY. Populated
	// by handlePtyInput and read by watchers via LastPtyInterrupt. Watchers
	// use it to detect a user-initiated interrupt that the agent's own
	// transcript/DB may not surface reliably (e.g. Hermes does not always
	// persist an interrupted turn to state.db).
	ptyInterrupt   = make(map[string]time.Time)
	ptyInterruptMu sync.RWMutex
	// ptyEscapePending delays a standalone ESC long enough to distinguish it
	// from the first byte of a fragmented terminal escape sequence.
	ptyEscapePending = make(map[string]*time.Timer)
)

// SetStatusMux wires the multiplexer into the agent package so that status
// transitions broadcast on the "agents" channel. Called once from
// server.New(). Falls back to the legacy statusHub when nil (legacy
// /ws/agents/statuses endpoint).
func SetStatusMux(m *ws.Multiplexer) { statusMux = m }

// SetPushStore wires the state store into the agent package so that status
// transitions can dispatch web push notifications. Called once from
// server.New().
func SetPushStore(s *state.Store) { pushStore = s }

// SetStateStore wires the state store used by RecordExternalSession. Called
// once from server.New(). In practice the same *state.Store as the push store.
func SetStateStore(s *state.Store) { stateStore = s }

// RecordExternalSession persists the agent's own internal session id for the
// given Caw leaf/session. Watchers call this once they bind to an agent
// session (OpenCode session row id, Codex rollout UUID, ...), and again on
// any mid-session re-bind caused by /new or /resume. On the next Caw reopen,
// resumeCmdForAgent reads this id back and launches the agent against the
// exact session instead of "--continue"/"--last", so multiple agent panes
// sharing a cwd each resume their own conversation.
func RecordExternalSession(sessionID, externalSessionID string) {
	if sessionID == "" || externalSessionID == "" {
		return
	}
	// Track the binding in-memory so the "agent explain" diagnostic can
	// report which native session each leaf is bound to, and updateStatus can
	// preserve it on the card entry. Kept separate from statuses so a binding
	// that happens before the first status broadcast is still captured.
	externalSessionsMu.Lock()
	externalSessions[sessionID] = externalSessionID
	externalSessionsMu.Unlock()

	if stateStore == nil {
		return
	}
	stateStore.SetExternalSessionID(sessionID, externalSessionID)
}

// BoundExternalSession returns the agent's own internal session id/path
// currently recorded for the given leaf, or "" if none has been bound yet.
func BoundExternalSession(sessionID string) string {
	externalSessionsMu.RLock()
	defer externalSessionsMu.RUnlock()
	return externalSessions[sessionID]
}

// PersistedExternalSession returns the agent's own internal session id/path
// persisted for the given leaf by a previous Caw process, or "" if none.
// Watchers call this at startup so a reopened pane resumes tracking the exact
// native session it was running (via resumeCmdForAgent) instead of re-running
// a heuristic candidate scan that can mis-associate a sibling session sharing
// the same cwd.
func PersistedExternalSession(leafID string) string {
	if stateStore == nil || leafID == "" {
		return ""
	}
	return stateStore.GetExternalSessionID(leafID)
}

// ReportAgentState applies a lifecycle/session report from an agent hook or
// plugin for an active terminal leaf. This is the additive "report-agent"
// authority (mirroring Herdr's pane.report_agent): official integrations can
// push semantic state and native session identity out-of-band, which is more
// robust than polling the agent's store. Reports only affect a leaf that is
// currently running an agent (verified against activeSessions), so a spoofed
// or stale report can never create a card or drive a session the platform no
// longer owns.
//
// status must be one of the known lifecycle states ("thinking", "executing",
// "waiting_input", "idle", "unknown", "interrupted", "tool_failed") or "" to
// report session identity only. externalSessionID, when non-empty, records the
// agent's own native session id/path so a reopen resumes the exact session.
// source labels the reporter (e.g. "opencode-plugin") for the explain endpoint.
func ReportAgentState(sessionID, agentID, status, tool, details, title, externalSessionID, source string) bool {
	activeSesMu.Lock()
	wCtx, active := activeSessions[sessionID]
	activeSesMu.Unlock()
	if !active {
		return false
	}
	if externalSessionID != "" {
		RecordExternalSession(sessionID, externalSessionID)
	}
	if status == "" {
		return true
	}
	switch status {
	case "thinking", "executing", "waiting_input", "idle", "unknown", "interrupted", "tool_failed":
	default:
		return false
	}
	updateStatus(sessionID, agentID, wCtx.cwd, status, tool, details, title, source)
	return true
}

func init() {
	terminal.OnSessionStart = handleSessionStart
	terminal.OnSessionExit = handleSessionExit
	terminal.OnPtyActivity = handlePtyActivity
	terminal.OnPtyInput = handlePtyInput
	terminal.OnPtyFocus = handlePtyFocus
}

// handlePtyInput detects when the user sends the interrupt key sequence
// (Ctrl+C or ESC) into a PTY and records it so watchers can detect a
// user-initiated interrupt that the agent's transcript/DB may not surface
// reliably. It does NOT transition status itself — that is left to each
// agent's watcher, which polls LastPtyInterrupt and applies the "interrupted"
// state while the turn is working.
//
// Previously this hook transitioned to "thinking" on any input; that was
// disabled to prevent non-agent PTY inputs (e.g. plain shell commands) from
// flipping the status. Recording only the explicit interrupt keys keeps the
// hook safe and narrow.
func handlePtyInput(id string, data string) {
	if strings.Contains(data, "\x03") {
		ptyInterruptMu.Lock()
		if timer := ptyEscapePending[id]; timer != nil {
			timer.Stop()
			delete(ptyEscapePending, id)
		}
		ptyInterrupt[id] = time.Now()
		ptyInterruptMu.Unlock()
		return
	}

	ptyInterruptMu.Lock()
	if data != "\x1b" {
		if timer := ptyEscapePending[id]; timer != nil {
			timer.Stop()
			delete(ptyEscapePending, id)
		}
		ptyInterruptMu.Unlock()
		return
	}
	if timer := ptyEscapePending[id]; timer != nil {
		timer.Stop()
	}
	ptyEscapePending[id] = time.AfterFunc(100*time.Millisecond, func() {
		ptyInterruptMu.Lock()
		if ptyEscapePending[id] != nil {
			delete(ptyEscapePending, id)
			ptyInterrupt[id] = time.Now()
		}
		ptyInterruptMu.Unlock()
	})
	ptyInterruptMu.Unlock()
}

// isInterruptInput reports whether the given PTY input bytes contain the user
// interrupt key sequence. TUI coding agents map two keys to interrupt/cancel
// during a busy turn:
//
//   - Ctrl+C, delivered as the 0x03 byte, and
//   - ESC, delivered as a standalone 0x1b byte (Claude Code's interrupt key).
//
// Only a standalone 0x1b counts as ESC: escape sequences (arrow keys,
// function keys, cursor-position reports) also begin with 0x1b but carry
// trailing bytes, and must not be treated as an interrupt.
// ExplainStatuses returns a diagnostic snapshot of every tracked agent session,
// including the evidence that produced the current status. Unlike ListStatuses
// (which only carries the visible card fields), each entry also exposes the
// bound native session id, the status authority source, and the leaf's PTY
// activity/focus/interrupt evidence. It is the "agent explain" endpoint's data
// source so misclassifications (wrong session bound to a leaf, false idle,
// stale state) can be diagnosed from concrete data.
func ExplainStatuses() []ExplainStatus {
	statusesMu.RLock()
	out := make([]ExplainStatus, 0, len(statuses))
	for _, s := range statuses {
		out = append(out, ExplainStatus{
			SessionID:         s.SessionID,
			AgentID:           s.AgentID,
			Cwd:               s.Cwd,
			Status:            s.Status,
			Tool:              s.Tool,
			Details:           s.Details,
			Title:             s.Title,
			Sequence:          s.Sequence,
			ExternalSessionID: BoundExternalSession(s.SessionID),
			Source:            s.Source,
			Timestamp:         s.Timestamp.UTC().Format(time.RFC3339),
		})
	}
	statusesMu.RUnlock()

	// Attach PTY evidence for each leaf outside the statuses lock to avoid
	// holding the global map lock while reading the smaller PTY maps.
	for i := range out {
		id := out[i].SessionID
		if p := LastPtyActivity(id); !p.IsZero() {
			out[i].LastPtyActivity = p.UTC().Format(time.RFC3339)
		}
		out[i].Focused = IsPtyFocused(id)
		if p := LastPtyInterrupt(id); !p.IsZero() {
			out[i].LastPtyInterrupt = p.UTC().Format(time.RFC3339)
		}
	}
	return out
}

func isInterruptInput(data string) bool {
	if strings.Contains(data, "\x03") {
		return true
	}
	return data == "\x1b"
}

// handlePtyFocus records whether the given leaf/session id currently has the
// user's focus. Called from terminal.HandleTerminalWS when the frontend sends
// a `focus`/`blur` message after the active pane changes. Also implicitly
// clears focus on any other pane: only one pane is focused at a time.
func handlePtyFocus(id string, focused bool) {
	ptyFocusMu.Lock()
	if focused {
		// Single-focus model: clear any previously focused pane so the
		// invariant "at most one entry is true" holds even if the frontend
		// ever forgets to send a blur before a focus on another pane.
		for k := range ptyFocus {
			if k != id {
				delete(ptyFocus, k)
			}
		}
		ptyFocus[id] = true
	} else {
		// Only delete if it was our entry — avoids clobbering a newer focus
		// that arrived between our blur and its processing.
		if ptyFocus[id] {
			delete(ptyFocus, id)
		}
	}
	ptyFocusMu.Unlock()
}

// handlePtyActivity records that the PTY for the given leaf/session id just
// produced output. Called from terminal.ReadLoop on every read.
func handlePtyActivity(id string, n int) {
	if n <= 0 {
		return
	}
	ptyActivityMu.Lock()
	ptyActivity[id] = time.Now()
	ptyActivityMu.Unlock()
}

// LastPtyActivity returns the timestamp of the most recent PTY output for the
// given leaf/session id, or the zero time if no activity has been recorded.
// Used by watchers to determine whether their agent process is currently
// producing output, which disambiguates which internal session belongs to
// which PTY when multiple agents of the same type share a cwd.
func LastPtyActivity(sessionID string) time.Time {
	ptyActivityMu.RLock()
	defer ptyActivityMu.RUnlock()
	return ptyActivity[sessionID]
}

// IsPtyFocused reports whether the given leaf/session id currently has the
// user's focus (i.e. is the active terminal pane the user is typing into).
// Used by watchers and the idle-timeout watchdog to bias the heuristics
// toward the pane the user is actually driving: a focused PTY is exempt
// from the auto-revert-to-idle and is allowed to re-bind to a sibling
// internal session even without recent PTY output.
func IsPtyFocused(sessionID string) bool {
	ptyFocusMu.RLock()
	defer ptyFocusMu.RUnlock()
	return ptyFocus[sessionID]
}

// LastPtyInterrupt returns the time the user most recently sent the interrupt
// key sequence (Ctrl+C) into the given leaf/session id's PTY, or the zero time
// if none has been recorded. Watchers poll this to detect a user-initiated
// interrupt that the agent's transcript/DB may not surface reliably (e.g.
// Hermes does not always persist an interrupted turn to state.db).
func LastPtyInterrupt(sessionID string) time.Time {
	ptyInterruptMu.RLock()
	defer ptyInterruptMu.RUnlock()
	return ptyInterrupt[sessionID]
}

// SetPtyActivityForTest records (or clears) the last-activity timestamp for a
// leaf/session id. It is intended only for tests that need to simulate PTY
// output without a real terminal; production code drives ptyActivity via
// terminal.OnPtyActivity (wired in init()).
func SetPtyActivityForTest(sessionID string, at time.Time) {
	ptyActivityMu.Lock()
	if at.IsZero() {
		delete(ptyActivity, sessionID)
	} else {
		ptyActivity[sessionID] = at
	}
	ptyActivityMu.Unlock()
}

// SetPtyFocusForTest records (or clears) whether a leaf/session id currently
// has the user's focus. It is intended only for tests; production code drives
// ptyFocus via terminal.OnPtyFocus (wired in init()).
func SetPtyFocusForTest(sessionID string, focused bool) {
	ptyFocusMu.Lock()
	if focused {
		for k := range ptyFocus {
			if k != sessionID {
				delete(ptyFocus, k)
			}
		}
		ptyFocus[sessionID] = true
	} else {
		delete(ptyFocus, sessionID)
	}
	ptyFocusMu.Unlock()
}

// SetPtyInterruptForTest records (or clears) the last-interrupt timestamp for
// a leaf/session id. It is intended only for tests; production code drives
// ptyInterrupt via terminal.OnPtyInput (wired in init()).
func SetPtyInterruptForTest(sessionID string, at time.Time) {
	ptyInterruptMu.Lock()
	if at.IsZero() {
		delete(ptyInterrupt, sessionID)
	} else {
		ptyInterrupt[sessionID] = at
	}
	ptyInterruptMu.Unlock()
}

// RegisterStatusWatcher allows status providers to register themselves
func RegisterStatusWatcher(agentID string, w StatusWatcher) {
	watchersMu.Lock()
	watchers[agentID] = w
	watchersMu.Unlock()
}

func StatusHub() *ws.Hub { return statusHub }

func marshalEvent(ev Event) ([]byte, error) { return json.Marshal(ev) }

func broadcastEvent(ev Event) {
	if statusMux != nil {
		statusMux.Broadcast("agents", ev)
		return
	}
	msg, err := marshalEvent(ev)
	if err != nil {
		return
	}
	statusHub.Broadcast(websocket.TextMessage, msg)
}

func updateStatus(sessionID, agentID, cwd, status, tool, details, title, source string) {
	// Strip markdown formatting from user-visible text so the Kanban card
	// Info line renders as clean plain text regardless of which agent
	// produced it. Titles are already cleaned by CleanPrompt upstream, but
	// details (assistant text excerpts) can contain **bold**, `code`, [links],
	// # headings, etc.
	details = StripMarkdown(details)
	title = StripMarkdown(title)

	statusesMu.Lock()
	prev, exists := statuses[sessionID]
	now := time.Now()
	// Once a session has transitioned to "crashed" it is terminal: ignore
	// any straggler status update from a watcher goroutine that hadn't yet
	// observed the process exit. This prevents a late "idle" callback from
	// resurrecting the card as a normal live card and hiding the crash.
	if exists && prev.Status == "crashed" {
		statusesMu.Unlock()
		return
	}
	if exists && prev.Status == status && prev.Tool == tool && prev.Details == details && prev.Title == title {
		statusesMu.Unlock()
		return // no change
	}

	seq := prev.Sequence
	if !exists {
		statusSeq++
		seq = statusSeq
	}

	// Preserve the bound native session id across status updates (set via
	// RecordExternalSession) and record the status authority so "agent
	// explain" can attribute the current state.
	extID := BoundExternalSession(sessionID)
	s := AgentStatus{
		SessionID:         sessionID,
		AgentID:           agentID,
		Cwd:               cwd,
		Status:            status,
		Tool:              tool,
		Details:           details,
		Title:             title,
		Timestamp:         now,
		Sequence:          seq,
		ExternalSessionID: extID,
		Source:            source,
	}
	statuses[sessionID] = s
	statusesMu.Unlock()

	broadcastEvent(Event{
		Type:      "agent_status",
		SessionID: sessionID,
		AgentID:   agentID,
		Cwd:       cwd,
		Status:    status,
		Tool:      tool,
		Details:   details,
		Title:     title,
		Timestamp: now,
		Sequence:  seq,
	})

	if pushStore != nil {
		switch status {
		case "waiting_input":
			push.CancelFinishedDebounced(sessionID)
			go push.Dispatch(pushStore, "needs_input", sessionID, agentID, title, "")
		case "thinking", "executing":
			push.CancelFinishedDebounced(sessionID)
		case "interrupted", "tool_failed":
			// The user interrupted the agent, or a tool call failed. Neither
			// is something the user needs a push notification for: an
			// interrupt is the user's own action, and a tool failure is
			// surfaced in the UI (red dot + error text) but the agent keeps
			// running. Cancel any pending "finished" notification so a
			// transition into these states from working doesn't fire one.
			push.CancelFinishedDebounced(sessionID)
		case "idle", "stopped", "unknown":
			// Suppress the "finished" notification when the agent was running
			// a background task or subagent — the agent is still working, it
			// just completed a sub-task. A "finished" notification here would
			// mislead the user.
			//   - "background_task": Antigravity's background tasks.
			//   - "task": OpenCode's subagent launcher. When the subagent
			//     finishes, the parent briefly transitions executing→idle
			//     before continuing, which would otherwise fire a spurious
			//     "finished" push notification.
			if exists && (prev.Status == "thinking" || prev.Status == "executing") && !isSubagentTool(prev.Tool) {
				push.DispatchFinishedDebounced(pushStore, sessionID, agentID, title, "")
			}
		}
	}
}

func handleSessionStart(id string, cmd []string, cwd string) {
	if len(cmd) == 0 {
		return
	}

	baseCmd := filepath.Base(cmd[0])
	baseCmd = strings.TrimSuffix(baseCmd, ".exe")

	var agentID string
	switch strings.ToLower(baseCmd) {
	case "claude":
		agentID = "claude"
	case "codex":
		agentID = "codex"
	case "copilot":
		agentID = "copilot"
	case "agy":
		agentID = "agy"
	case "opencode":
		agentID = "opencode"
	case "pi":
		agentID = "pi"
	case "omp":
		agentID = "omp"
	case "hermes":
		agentID = "hermes"
	case "command-code", "commandcode":
		agentID = "commandcode"
	case "fx":
		agentID = "fx"
	default:
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	wCtx := &watcherContext{
		cancel:    cancel,
		agentId:   agentID,
		sessionId: id,
		cwd:       cwd,
		cmd:       cmd,
		resume:    isResumeCmd(cmd),
	}

	activeSesMu.Lock()
	activeSessions[id] = wCtx
	activeSesMu.Unlock()

	broadcastEvent(Event{
		Type:      "agent_started",
		SessionID: id,
		AgentID:   agentID,
		Cwd:       cwd,
		Timestamp: time.Now(),
	})

	updateStatus(id, agentID, cwd, "idle", "", "", "", "watcher")

	go watchAgent(ctx, wCtx)
}

func handleSessionExit(id string, exitCode int, exitErr error, killed bool) {
	activeSesMu.Lock()
	wCtx, exists := activeSessions[id]
	if exists {
		delete(activeSessions, id)
	}
	activeSesMu.Unlock()

	if !exists {
		return
	}

	wCtx.cancel()
	push.CancelFinishedDebounced(id)

	// Decide how to finalize the session.
	//
	//   - killed (user clicked kill in the UI): remove immediately, just like
	//     a clean exit. The user explicitly chose to discard this run.
	//   - clean exit (exit code 0): remove. A finished run is not a crash.
	//   - anything else (non-zero exit, signal): transition to "crashed" and
	//     keep the card on the board with a red dot + reduced opacity in the
	//     column it was last in, persisted to SQLite so it survives Caw
	//     restarts. The user dismisses it explicitly.
	crashed := !killed && exitCode != 0

	statusesMu.Lock()
	prev, hadStatus := statuses[id]
	if !crashed {
		delete(statuses, id)
	} else {
		// Transition to "crashed" but keep the last live tool/details/title so
		// the card shows what the agent was last doing. Status itself is
		// overwritten to "crashed"; LastColumn records the column the card was
		// in so the UI can keep it there.
		now := time.Now()
		ec := exitCode
		st := AgentStatus{
			SessionID:  prev.SessionID,
			AgentID:    prev.AgentID,
			Cwd:        prev.Cwd,
			Status:     "crashed",
			Tool:       prev.Tool,
			Details:    prev.Details,
			Title:      prev.Title,
			Timestamp:  prev.Timestamp,
			Sequence:   prev.Sequence,
			EndedAt:    &now,
			ExitCode:   &ec,
			ExitReason: exitReason(exitCode, exitErr),
			LastColumn: lastColumnForStatus(prev.Status),
		}
		statuses[id] = st
	}
	statusesMu.Unlock()

	ptyFocusMu.Lock()
	delete(ptyFocus, id)
	ptyFocusMu.Unlock()

	if !crashed {
		// Clean exit or user kill: card simply leaves the board, as before.
		broadcastEvent(Event{
			Type:      "agent_stopped",
			SessionID: id,
			AgentID:   wCtx.agentId,
			Timestamp: time.Now(),
		})
		// Make sure a previously-persisted crashed row for this leaf is gone
		// (e.g. the user re-ran the agent in the same pane after dismissing a
		// crash, then exited cleanly).
		if stateStore != nil {
			stateStore.DeleteCrashedSession(id)
		}
		return
	}

	// Crashed: broadcast the new terminal event and persist the snapshot so
	// the card survives a Caw restart until the user dismisses it.
	statusesMu.RLock()
	cur := statuses[id]
	statusesMu.RUnlock()

	broadcastEvent(Event{
		Type:       "agent_crashed",
		SessionID:  id,
		AgentID:    cur.AgentID,
		Cwd:        cur.Cwd,
		Status:     cur.Status,
		Tool:       cur.Tool,
		Details:    cur.Details,
		Title:      cur.Title,
		Timestamp:  cur.Timestamp,
		Sequence:   cur.Sequence,
		EndedAt:    cur.EndedAt,
		ExitCode:   cur.ExitCode,
		ExitReason: cur.ExitReason,
		LastColumn: cur.LastColumn,
	})

	if stateStore != nil && hadStatus {
		stateStore.SaveCrashedSession(state.CrashedSession{
			SessionID:  cur.SessionID,
			AgentID:    cur.AgentID,
			Cwd:        cur.Cwd,
			Title:      cur.Title,
			Tool:       cur.Tool,
			Details:    cur.Details,
			Status:     prev.Status, // the live status it was in before the crash
			LastColumn: cur.LastColumn,
			ExitCode:   valInt(cur.ExitCode),
			ExitReason: cur.ExitReason,
			StartedAt:  cur.Timestamp,
			EndedAt:    valTime(cur.EndedAt),
			Sequence:   cur.Sequence,
		})
	}
}

// lastColumnForStatus maps a live agent status to the Kanban column id the
// card was in. Used to keep a crashed card in the column the user last saw
// it in rather than moving it on crash.
func lastColumnForStatus(liveStatus string) string {
	switch liveStatus {
	case "thinking", "executing", "tool_failed":
		// tool_failed stays in "working": the agent is still active, the
		// failure is just surfaced with a red dot.
		return "working"
	case "waiting_input":
		return "needs_input"
	default:
		// "idle", "unknown", and "interrupted": an interrupted turn is no
		// longer working, and an unknown/stale state cannot be confirmed as
		// working, so it sits in idle.
		return "idle"
	}
}

// exitReason produces a short human-readable reason for the exit, used as a
// sub-label on the crashed card. Negative exit codes mean the process was
// terminated by a signal (a crash); positive non-zero codes mean the agent
// exited with an error.
func exitReason(exitCode int, exitErr error) string {
	if exitErr != nil {
		return "crashed"
	}
	if exitCode < 0 {
		return "signal"
	}
	return "crashed"
}

func valInt(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

func valTime(p *time.Time) time.Time {
	if p == nil {
		return time.Time{}
	}
	return *p
}

// DismissCrashedSession removes a crashed card from the in-memory statuses
// map and from the persisted crashed_sessions table, and broadcasts an
// agent_stopped event so the frontend animates the card out. Called by the
// DELETE /agents/statuses/{id} dismiss endpoint when the user dismisses a
// crashed card. It is a no-op if the session isn't currently in the
// "crashed" state (e.g. the user is trying to dismiss a live card, which is
// not allowed).
func DismissCrashedSession(sessionID string) bool {
	statusesMu.Lock()
	s, exists := statuses[sessionID]
	if !exists || s.Status != "crashed" {
		statusesMu.Unlock()
		return false
	}
	delete(statuses, sessionID)
	statusesMu.Unlock()

	if stateStore != nil {
		stateStore.DeleteCrashedSession(sessionID)
	}

	broadcastEvent(Event{
		Type:      "agent_stopped",
		SessionID: sessionID,
		AgentID:   s.AgentID,
		Timestamp: time.Now(),
	})
	return true
}

// LoadCrashedSessions rehydrates the in-memory statuses map with any
// persisted crashed-session snapshots from the SQLite store. Called once
// at server startup (from server.New via a hook) so crashed cards survive a
// Caw restart and stay on the board until the user dismisses them. Sessions
// whose leaf id is currently running a live agent (i.e. also in
// activeSessions) are skipped — the live run takes precedence over the
// stale crashed row, which is also cleaned up.
func LoadCrashedSessions() {
	if stateStore == nil {
		return
	}
	rows := stateStore.ListCrashedSessions()
	if len(rows) == 0 {
		return
	}
	statusesMu.Lock()
	for _, c := range rows {
		// If a live agent is already running in this leaf (e.g. the user
		// reopened Caw and the auto-resume restarted the agent in the same
		// pane), drop the stale crashed row — the live run owns the card.
		activeSesMu.Lock()
		_, live := activeSessions[c.SessionID]
		activeSesMu.Unlock()
		if live {
			stateStore.DeleteCrashedSession(c.SessionID)
			continue
		}
		// Skip if a status already exists (e.g. a live card was created by
		// handleSessionStart in a race before we ran).
		if _, ok := statuses[c.SessionID]; ok {
			continue
		}
		ec := c.ExitCode
		endedAt := c.EndedAt
		statuses[c.SessionID] = AgentStatus{
			SessionID:  c.SessionID,
			AgentID:    c.AgentID,
			Cwd:        c.Cwd,
			Status:     "crashed",
			Tool:       c.Tool,
			Details:    c.Details,
			Title:      c.Title,
			Timestamp:  c.StartedAt,
			Sequence:   c.Sequence,
			EndedAt:    &endedAt,
			ExitCode:   &ec,
			ExitReason: c.ExitReason,
			LastColumn: c.LastColumn,
		}
		// Keep statusSeq ahead of any reloaded sequence so newly opened
		// sessions sort after the rehydrated crashed cards.
		if c.Sequence > statusSeq {
			statusSeq = c.Sequence
		}
	}
	statusesMu.Unlock()
}

// idleTimeout is the duration after which an agent stuck in a non-idle state
// with no new updates will be automatically reverted to idle. This prevents
// the KanbanBoard from showing "working" indefinitely if the agent crashes
// without triggering a clean session exit.
//
// Set to 2 minutes as a balance: LLM responses can take a minute or more
// for complex tool chains, but an interrupt that doesn't write to the
// transcript should be detected sooner than 5 minutes.
const idleTimeout = 2 * time.Minute

// ptyActivityWindow is the recency window used by the idle-timeout watchdog
// to decide whether recent PTY output should keep the agent in a "working"
// state. If the agent's PTY has produced bytes within this window, the
// watchdog skips the auto-revert to "idle" even when no transcript/DB writes
// have happened for a while (e.g. a long bash command streaming output).
// It is intentionally longer than the poll interval so brief output gaps
// during tool execution don't trigger a premature revert.
const ptyActivityWindow = 30 * time.Second

func watchAgent(ctx context.Context, wCtx *watcherContext) {
	watchersMu.Lock()
	watcher, ok := watchers[wCtx.agentId]
	watchersMu.Unlock()

	if !ok {
		return
	}

	// lastActivity tracks when the watcher last received an actual status change
	// from the agent log.
	lastActivity := time.Now()
	// lastHeartbeat tracks the last poll confirmation from the watcher.
	lastHeartbeat := time.Now()

	// Idle-timeout watchdog: if the agent hasn't emitted a status change in
	// idleTimeout seconds, or if the watcher itself has stopped calling the
	// heartbeat callback (indicating a crash), revert to idle.
	//
	// PTY activity exemption: a long-running tool call (e.g. a bash command
	// or a lengthy tool that streams output) may not produce any new
	// transcript/DB writes for minutes while it runs. The agent is still
	// actively working — its PTY keeps emitting output — so reverting to
	// "idle" here would be wrong. When the PTY has produced output within
	// the last ptyActivityWindow, we skip the revert entirely. This applies
	// to every agent (the watchdog is shared); the LLM-thinking phase after
	// a tool finishes does write to the transcript, so the 2-minute idle
	// timeout still catches genuinely stalled sessions.
	//
	// Focused PTY exemption: even when the PTY has been quiet (no output and
	// no transcript writes) for longer than the timeout, if the user is
	// currently focused on this pane we keep the last non-idle status. The
	// user may be mid-composition (typing a prompt that hasn't been
	// submitted), reading a long answer, or waiting for an answer that
	// doesn't stream to the transcript until it completes. Reverting a
	// pane the user is actively looking at to "idle" is more confusing
	// than keeping the previous status, and a genuinely-stalled focused
	// session will still be caught when the user eventually blurs it.
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if time.Since(lastHeartbeat) > 30*time.Second || time.Since(lastActivity) > idleTimeout {
					// If the PTY has produced output recently, the agent
					// process is still alive and actively running a tool —
					// don't revert to idle.
					lastPtyOut := LastPtyActivity(wCtx.sessionId)
					if !lastPtyOut.IsZero() && time.Since(lastPtyOut) < ptyActivityWindow {
						continue
					}
					// If this pane currently has the user's focus, don't
					// auto-revert: the user is actively interacting with it
					// and a spurious "idle" would be misleading.
					if IsPtyFocused(wCtx.sessionId) {
						continue
					}
					statusesMu.RLock()
					s, exists := statuses[wCtx.sessionId]
					statusesMu.RUnlock()
					// Do NOT auto-revert "waiting_input": the agent is blocked
					// waiting for the user to answer — this can take minutes.
					// Do NOT auto-revert "interrupted" or "tool_failed":
					// those are sticky UI states the user must see (red dot)
					// and they're not transient "working" states. Only
					// revert transient "working" states (thinking/executing)
					// that have stalled.
					if exists && s.Status != "idle" && s.Status != "stopped" && s.Status != "unknown" && s.Status != "waiting_input" && s.Status != "crashed" && s.Status != "interrupted" && s.Status != "tool_failed" {
						// A stale working state means the agent MAY still be
						// running, just not observably. Revert to "unknown"
						// rather than "idle" so the card does not falsely
						// claim the agent is finished and waiting for input.
						updateStatus(wCtx.sessionId, wCtx.agentId, wCtx.cwd, "unknown", "", "", s.Title, "watchdog")
					}
				}
			}
		}
	}()

	watcher.Watch(ctx, wCtx.sessionId, wCtx.cwd, wCtx.resume, func(status, tool, details, title string) {
		lastActivity = time.Now()
		lastHeartbeat = time.Now()
		updateStatus(wCtx.sessionId, wCtx.agentId, wCtx.cwd, status, tool, details, title, "watcher")
	}, func() {
		lastHeartbeat = time.Now()
	})
}

// isSubagentTool reports whether the given tool name represents a background
// task or subagent launcher. When such a tool was the agent's last action,
// transitioning to "idle" should NOT fire a "finished" push notification —
// the agent is still working, it just completed a sub-task.
func isSubagentTool(tool string) bool {
	switch tool {
	case "background_task", "task":
		return true
	}
	return false
}

// isResumeCmd reports whether the agent launch command contains a resume/
// continue flag, indicating the agent will reattach to a pre-existing
// internal session rather than starting a new one. This is set by
// terminal.resumeCmdForAgent on reopen.
//
// It recognizes every resume form across the supported agents:
//   - --continue / -c / --resume / --last (claude, pi, omp, copilot, agy)
//   - -s <id> / --session <id> (opencode exact-session resume)
//   - resume <id> / resume --last (codex subcommand)
func isResumeCmd(cmd []string) bool {
	for i, a := range cmd {
		switch a {
		case "--continue", "-c", "--resume", "--last":
			return true
		case "-s", "--session":
			// opencode -s <session-id> | opencode --session <session-id>
			if i+1 < len(cmd) && cmd[i+1] != "" {
				return true
			}
		case "resume":
			// codex resume <session-id> | codex resume --last
			return true
		}
	}
	return false
}
