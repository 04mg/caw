package state

import "time"

// AgentSession record tracks whether a given terminal leaf already had an
// agent PTY started for it in a previous Caw process. Caw persists layout
// metadata (cwd, cmd, agentId, branches) to SQLite, but the live PTY process
// is volatile — when Caw quits, all child PTYs die. On reopen, the frontend
// re-issues /api/terminal/create with the same leafId; the backend would
// normally spawn a fresh agent process that loses the agent's internal
// session state (conversation history, context, etc.).
//
// By recording that a leaf previously hosted a running agent, the backend
// can detect the reopen case and mutate the agent's launch command to pass a
// resume/continue flag so the agent reattaches its last session instead of
// starting a new one.
//
// agent_sessions(leaf_id PK, agent_id, cwd, started_at)
//   - leaf_id   : the Caw terminal leaf UUID (frontend-assigned, persisted in layout_nodes.id)
//   - agent_id  : the agent identifier ("claude", "opencode", "codex", ...)
//   - cwd       : working directory the agent was launched in (mainly for diagnostics)
//   - started_at: wall-clock timestamp of the first PTY start for this leaf

// MarkAgentStarted records that an agent PTY was started for the given leaf.
// Upsert: if a row already exists for leafID it is overwritten, keeping the
// earliest started_at would be misleading for a reopened leaf, so we always
// stamp the current time.
func (s *Store) MarkAgentStarted(leafID, agentID, cwd string) {
	if leafID == "" {
		return
	}
	s.Mu.Lock()
	defer s.Mu.Unlock()
	_, _ = s.db.Exec(
		`INSERT INTO agent_sessions (leaf_id, agent_id, cwd, started_at) VALUES (?, ?, ?, ?)
		 ON CONFLICT(leaf_id) DO UPDATE SET agent_id = excluded.agent_id, cwd = excluded.cwd, started_at = excluded.started_at`,
		leafID, agentID, cwd, time.Now().UTC(),
	)
}

// WasAgentStarted reports whether a previous Caw process started an agent
// PTY for this leaf. Used by /api/terminal/create to decide whether to
// append a resume flag to the agent's launch command.
func (s *Store) WasAgentStarted(leafID string) bool {
	if leafID == "" {
		return false
	}
	s.Mu.RLock()
	defer s.Mu.RUnlock()
	var v int
	err := s.db.QueryRow("SELECT 1 FROM agent_sessions WHERE leaf_id = ?", leafID).Scan(&v)
	return err == nil
}

// GetAgentSession returns the persisted agent session info for a leaf, if any.
func (s *Store) GetAgentSession(leafID string) (agentID, cwd string, ok bool) {
	if leafID == "" {
		return "", "", false
	}
	s.Mu.RLock()
	defer s.Mu.RUnlock()
	err := s.db.QueryRow("SELECT agent_id, cwd FROM agent_sessions WHERE leaf_id = ?", leafID).Scan(&agentID, &cwd)
	if err != nil {
		return "", "", false
	}
	return agentID, cwd, true
}

// GetExternalSessionID returns the agent's own internal session id previously
// recorded for this leaf (e.g. the OpenCode session row id or the Codex
// rollout UUID), or "" if none is recorded. Used by resumeCmdForAgent to
// reattach to the exact session that was running before Caw quit, instead of
// always resuming the most recent one in the cwd — which matters when multiple
// agent panes share the same worktree.
func (s *Store) GetExternalSessionID(leafID string) string {
	if leafID == "" {
		return ""
	}
	s.Mu.RLock()
	defer s.Mu.RUnlock()
	var sid string
	err := s.db.QueryRow("SELECT external_session_id FROM agent_sessions WHERE leaf_id = ?", leafID).Scan(&sid)
	if err != nil {
		return ""
	}
	return sid
}

// SetExternalSessionID records the agent's own internal session id for a leaf.
// Called by the status watcher once it binds to an agent session (OpenCode
// session row, Codex rollout file, etc.). Subsequent calls overwrite the
// value, so a mid-session /new or /resume that rebinds to a different agent
// session updates the record and the next Caw reopen resumes the right one.
//
// The write is an upsert: a plain UPDATE silently does nothing when no
// agent_sessions row exists for the leaf (e.g. a watcher bound a native
// session before MarkAgentStarted ran, or the row was cleared), leaving a
// dangling binding that a reopen cannot resume. INSERT OR REPLACE guarantees
// the external id is always persisted keyed by leaf.
func (s *Store) SetExternalSessionID(leafID, externalSessionID string) {
	if leafID == "" || externalSessionID == "" {
		return
	}
	s.Mu.Lock()
	defer s.Mu.Unlock()
	_, _ = s.db.Exec(
		`INSERT INTO agent_sessions (leaf_id, external_session_id) VALUES (?, ?)
		 ON CONFLICT(leaf_id) DO UPDATE SET external_session_id = excluded.external_session_id`,
		leafID, externalSessionID,
	)
}

// ClearAgentSession removes the agent-session record for a leaf. Called when
// the user explicitly closes the terminal pane (kill), so that reopening the
// same layout leaf would start a fresh agent rather than resuming a session
// the user intended to discard.
func (s *Store) ClearAgentSession(leafID string) {
	if leafID == "" {
		return
	}
	s.Mu.Lock()
	defer s.Mu.Unlock()
	_, _ = s.db.Exec("DELETE FROM agent_sessions WHERE leaf_id = ?", leafID)
}