package state

import "time"

// CrashedSession is a persisted snapshot of an agent session that died
// unexpectedly (non-zero/signal exit, not a user kill). It lets the Kanban
// board keep showing a dismissable "crashed" card across Caw restarts —
// otherwise the in-memory statuses map would be empty after a restart and
// the user would have no indication that a run died.
//
// crashed_sessions(session_id PK, agent_id, cwd, title, tool, details,
//                  status, last_column, exit_code, exit_reason,
//                  started_at, ended_at, sequence)
//
//   - session_id  : the Caw terminal leaf UUID (same as AgentStatus.SessionID)
//   - agent_id    : agent identifier ("claude", "opencode", "codex", ...)
//   - cwd        : working directory the agent was launched in
//   - title      : cleaned first user prompt, for the card title line
//   - tool       : last tool the watcher reported before the crash
//   - details    : last assistant text excerpt before the crash
//   - status     : the live status the card was in just before the crash
//                  ("thinking", "executing", "waiting_input", "idle")
//   - last_column: Kanban column the card was in ("working", "needs_input",
//                  "idle") — precomputed so the UI doesn't have to re-derive it
//   - exit_code  : process exit code (negative when killed by a signal)
//   - exit_reason: short human-readable reason ("crashed", "signal", ...)
//   - started_at : when the session was first tracked
//   - ended_at   : when the crash was recorded
//   - sequence   : backend-assigned opening sequence, for stable ordering
type CrashedSession struct {
	SessionID  string
	AgentID    string
	Cwd        string
	Title      string
	Tool       string
	Details    string
	Status     string
	LastColumn string
	ExitCode   int
	ExitReason string
	StartedAt  time.Time
	EndedAt    time.Time
	Sequence   int64
}

// SaveCrashedSession upserts a crashed-session snapshot. Called from the
// agent package when a session transitions to "crashed". The row is
// removed by DeleteCrashedSession when the user dismisses the card, or by
// SaveCrashedSession overwriting it if the same session id is reused.
func (s *Store) SaveCrashedSession(c CrashedSession) {
	if c.SessionID == "" {
		return
	}
	s.Mu.Lock()
	defer s.Mu.Unlock()
	_, _ = s.db.Exec(
		`INSERT INTO crashed_sessions
		   (session_id, agent_id, cwd, title, tool, details, status,
		    last_column, exit_code, exit_reason, started_at, ended_at, sequence)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(session_id) DO UPDATE SET
		   agent_id=excluded.agent_id, cwd=excluded.cwd, title=excluded.title,
		   tool=excluded.tool, details=excluded.details, status=excluded.status,
		   last_column=excluded.last_column, exit_code=excluded.exit_code,
		   exit_reason=excluded.exit_reason, started_at=excluded.started_at,
		   ended_at=excluded.ended_at, sequence=excluded.sequence`,
		c.SessionID, c.AgentID, c.Cwd, c.Title, c.Tool, c.Details, c.Status,
		c.LastColumn, c.ExitCode, c.ExitReason, c.StartedAt.UTC(), c.EndedAt.UTC(), c.Sequence,
	)
}

// DeleteCrashedSession removes a crashed-session row. Called when the user
// dismisses a crashed card from the Kanban board.
func (s *Store) DeleteCrashedSession(sessionID string) {
	if sessionID == "" {
		return
	}
	s.Mu.Lock()
	defer s.Mu.Unlock()
	_, _ = s.db.Exec("DELETE FROM crashed_sessions WHERE session_id = ?", sessionID)
}

// ListCrashedSessions returns all persisted crashed-session snapshots, in
// stable sequence order. Called once at server startup to rehydrate the
// in-memory statuses map so crashed cards survive a Caw restart.
func (s *Store) ListCrashedSessions() []CrashedSession {
	s.Mu.RLock()
	defer s.Mu.RUnlock()
	rows, err := s.db.Query(
		`SELECT session_id, agent_id, cwd, title, tool, details, status,
		        last_column, exit_code, exit_reason, started_at, ended_at, sequence
		 FROM crashed_sessions ORDER BY sequence ASC`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []CrashedSession
	for rows.Next() {
		var c CrashedSession
		var startedAt, endedAt string
		if err := rows.Scan(&c.SessionID, &c.AgentID, &c.Cwd, &c.Title, &c.Tool,
			&c.Details, &c.Status, &c.LastColumn, &c.ExitCode, &c.ExitReason,
			&startedAt, &endedAt, &c.Sequence); err != nil {
			continue
		}
		c.StartedAt = parseTime(startedAt)
		c.EndedAt = parseTime(endedAt)
		out = append(out, c)
	}
	return out
}

// CountCrashedSessions returns the number of persisted crashed-session rows.
// Used in tests to assert persistence behavior without relying on ordering.
func (s *Store) CountCrashedSessions() int {
	s.Mu.RLock()
	defer s.Mu.RUnlock()
	var n int
	_ = s.db.QueryRow("SELECT COUNT(*) FROM crashed_sessions").Scan(&n)
	return n
}

func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		// Fall back to the SQLite default datetime format.
		t, err = time.Parse("2006-01-02 15:04:05", s)
		if err != nil {
			return time.Time{}
		}
	}
	return t
}