package terminal

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"

	"github.com/04mg/caw/internal/state"
)

type SessionManager struct {
	sessions   map[string]*Session
	sessionsMu sync.RWMutex
	upgrader   *websocket.Upgrader
	store      *state.Store
}

func NewSessionManager(store *state.Store, upgrader *websocket.Upgrader) *SessionManager {
	return &SessionManager{
		sessions: make(map[string]*Session),
		upgrader: upgrader,
		store:    store,
	}
}

func (m *SessionManager) Create(req CreateRequest) (string, error) {
	cwd := req.Cwd
	if cwd == "" {
		cwd, _ = os.Getwd()
	}

	if req.ID != "" {
		m.sessionsMu.RLock()
		existing, ok := m.sessions[req.ID]
		m.sessionsMu.RUnlock()
		if ok {
			return existing.ID, nil
		}
	}

	id := req.ID
	if id == "" {
		id = uuid.New().String()
	}

	cmd := req.Cmd
	if m.store != nil && id != "" && len(cmd) > 0 {
		cmd = resumeCmdForAgent(m.store, id, cmd)
	}

	ps, err := startPty(cwd, cmd, req.Env)
	if err != nil {
		return "", err
	}

	sess := &Session{
		ID:         id,
		Pty:        ps,
		Cwd:        cwd,
		conns:      make(map[*connWriter]bool),
		scrollback: []byte{},
		modes:      make(map[int]bool),
	}
	sess.onExit = m.buildOnExit(sess, cwd, id)

	m.sessionsMu.Lock()
	m.sessions[id] = sess
	m.sessionsMu.Unlock()

	if m.store != nil && id != "" && len(req.Cmd) > 0 {
		if aid := agentBaseName(req.Cmd[0]); isKnownAgent(aid) {
			m.store.MarkAgentStarted(id, aid, cwd)
		}
	}

	if OnSessionStart != nil {
		OnSessionStart(id, cmd, cwd)
	}

	go sess.ReadLoop()

	return id, nil
}

func (m *SessionManager) buildOnExit(sess *Session, cwd, id string) func() {
	return func() {
		// Capture the exit result before deleting the session. The exit
		// goroutine in ReadLoop writes sess.exitInfo before s.Pty.Close()
		// unblocks the read loop, so by the time onExit runs the info is
		// already populated. killed is set by Delete before issuing the kill.
		exitCode := sess.exitInfo.code
		exitErr := sess.exitInfo.err
		killed := sess.killed

		m.sessionsMu.Lock()
		delete(m.sessions, id)

		stillInUse := false
		for _, s := range m.sessions {
			if s.Cwd == cwd {
				stillInUse = true
				break
			}
		}
		doCleanup := !stillInUse
		m.sessionsMu.Unlock()

		if OnSessionExit != nil {
			OnSessionExit(id, exitCode, exitErr, killed)
		}
		if !doCleanup {
			return
		}

		m.maybeCleanupWorktree(sess, cwd)
	}
}

func (m *SessionManager) maybeCleanupWorktree(sess *Session, cwd string) {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	worktreesBase := filepath.Clean(filepath.Join(home, ".caw", "worktrees"))
	cleanCwd := filepath.Clean(cwd)
	if !strings.HasPrefix(cleanCwd, worktreesBase) || cleanCwd == worktreesBase {
		return
	}
	go func() {
		time.Sleep(500 * time.Millisecond)
		mainRepo := getMainRepoPath(cleanCwd)
		if mainRepo == "" {
			return
		}
		branchName := ""
		cmdBranch := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
		cmdBranch.Dir = cleanCwd
		if branchOut, err := cmdBranch.Output(); err == nil {
			branchName = strings.TrimSpace(string(branchOut))
		}

		cmdRemove := exec.Command("git", "worktree", "remove", "--force", cleanCwd)
		cmdRemove.Dir = mainRepo
		_ = cmdRemove.Run()

		if sess.DeleteBranch && branchName != "" && branchName != "HEAD" {
			cmdDel := exec.Command("git", "branch", "-D", branchName)
			cmdDel.Dir = mainRepo
			_ = cmdDel.Run()
		}
		_ = os.RemoveAll(cleanCwd)
	}()
}

func (m *SessionManager) Get(id string) (*Session, bool) {
	m.sessionsMu.RLock()
	defer m.sessionsMu.RUnlock()
	sess, ok := m.sessions[id]
	return sess, ok
}

func (m *SessionManager) Delete(id string, deleteBranch bool) bool {
	m.sessionsMu.Lock()
	sess, ok := m.sessions[id]
	if ok {
		if deleteBranch {
			sess.DeleteBranch = true
		}
		// Mark the session as user-killed so the onExit handler can
		// distinguish an explicit kill (which should NOT be treated as a
		// crash on the Kanban board) from a process that died on its own.
		sess.killed = true
		delete(m.sessions, id)
	}
	m.sessionsMu.Unlock()
	if !ok {
		return false
	}
	sess.Pty.Kill()
	if m.store != nil {
		m.store.ClearAgentSession(id)
	}
	return true
}

func (m *SessionManager) Upgrader() *websocket.Upgrader { return m.upgrader }