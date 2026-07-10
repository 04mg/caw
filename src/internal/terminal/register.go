package terminal

import (
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/04mg/caw/internal/httputil"
	"github.com/04mg/caw/internal/state"
)

type CreateRequest struct {
	Cwd string   `json:"cwd"`
	ID  string   `json:"id"`
	Cmd []string `json:"cmd,omitempty"`
}

type KillRequest struct {
	ID           string `json:"id"`
	DeleteBranch bool   `json:"deleteBranch"`
}

var (
	OnSessionStart func(id string, cmd []string, cwd string)
	OnSessionExit  func(id string)
)

func Register(mux *http.ServeMux, sessions map[string]*Session, sessionsMu *sync.RWMutex, upgrader *websocket.Upgrader, store *state.Store) {
	mux.HandleFunc("POST /api/terminals", func(w http.ResponseWriter, r *http.Request) {
		var req CreateRequest
		if err := httputil.ReadJSON(r, &req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		cwd := req.Cwd
		if cwd == "" {
			cwd, _ = os.Getwd()
		}

		if req.ID != "" {
			sessionsMu.RLock()
			existing, ok := sessions[req.ID]
			sessionsMu.RUnlock()
			if ok {
				httputil.WriteJSON(w, map[string]string{"id": existing.ID})
				return
			}
		}

		id := req.ID
		if id == "" {
			id = uuid.New().String()
		}

		// Reopen detection: if this leaf previously hosted an agent PTY in a
		// prior Caw process, mutate the launch command to pass a resume/continue
		// flag so the agent reconnects to its last internal session instead of
		// starting fresh. Plain shells and unknown binaries pass through.
		cmd := req.Cmd
		if store != nil && id != "" && len(cmd) > 0 {
			cmd = resumeCmdForAgent(store, id, cmd)
		}

		ps, err := startPty(cwd, cmd)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		sess := &Session{
			ID:         id,
			Pty:        ps,
			Cwd:        cwd,
			conns:      make(map[*websocket.Conn]bool),
			scrollback: []byte{},
			modes:      make(map[int]bool),
		}
		sess.onExit = func() {
			sessionsMu.Lock()
			delete(sessions, id)

			// Only clean up worktree if no other session still uses this cwd
			stillInUse := false
			for _, s := range sessions {
				if s.Cwd == cwd {
					stillInUse = true
					break
				}
			}
			doCleanup := !stillInUse
			sessionsMu.Unlock()

		if OnSessionExit != nil {
			OnSessionExit(id)
		}
		if !doCleanup {
			return
		}

			// Clean up Git worktree if cwd is an agent worktree
			home, err := os.UserHomeDir()
			if err == nil {
				worktreesBase := filepath.Clean(filepath.Join(home, ".caw", "worktrees"))
				cleanCwd := filepath.Clean(cwd)
				if strings.HasPrefix(cleanCwd, worktreesBase) && cleanCwd != worktreesBase {
					go func() {
						time.Sleep(500 * time.Millisecond)
						mainRepo := getMainRepoPath(cleanCwd)
						if mainRepo != "" {
							branchName := ""
							// Get branch name before removing worktree
							cmdBranch := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
							cmdBranch.Dir = cleanCwd
							branchOut, err := cmdBranch.Output()
							if err == nil {
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
						}
						_ = os.RemoveAll(cleanCwd)
					}()
				}
			}
		}
		sessionsMu.Lock()
		sessions[id] = sess
		sessionsMu.Unlock()

		// Record that an agent PTY was started for this leaf so a future Caw
		// process can detect the reopen case and pass a resume flag. Only
		// record known agents (not plain shells) so reopening a shell pane
		// doesn't get a spurious --continue appended.
		if store != nil && id != "" && len(req.Cmd) > 0 {
			if aid := agentBaseName(req.Cmd[0]); isKnownAgent(aid) {
				store.MarkAgentStarted(id, aid, cwd)
			}
		}

		if OnSessionStart != nil {
			OnSessionStart(id, req.Cmd, cwd)
		}

		go sess.ReadLoop()

		httputil.WriteJSON(w, map[string]string{"id": id})
	})

	mux.HandleFunc("/ws/terminals/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		sessionsMu.RLock()
		sess, ok := sessions[id]
		sessionsMu.RUnlock()
		if !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}

		sess.mu.Lock()
		sess.conns[c] = true
		// Copy scrollback under lock, strip alternate screen sequences
		// so that xterm.js stays in the normal buffer (with scrollback).
		scrollback := make([]byte, len(sess.scrollback))
		copy(scrollback, sess.scrollback)
		// Capture the current DEC private mode state so we can re-apply it
		// after the scrollback replay. A fresh xterm.js instance starts
		// with mouse tracking / bracketed paste OFF; if the running TUI
		// enabled them, we must resend the set sequences or clicks and
		// wheel scroll silently break.
		syncSeq := sess.syncMessage()
		// If the PTY already has a size, push it to the new client so it
		// fits its xterm.js to the shared size immediately.
		if sess.cols > 0 && sess.rows > 0 {
			msg, _ := json.Marshal(map[string]any{
				"type": "resize",
				"cols": sess.cols,
				"rows": sess.rows,
			})
			c.WriteMessage(websocket.TextMessage, msg)
		}
		sess.mu.Unlock()

		if len(scrollback) > 0 {
			stripped := stripAlternateScreen(scrollback)
			if len(stripped) > 0 {
				msg, _ := json.Marshal(map[string]any{
					"type": "output",
					"data": string(stripped),
				})
				c.WriteMessage(websocket.TextMessage, msg)
			}
		}

		// Re-apply the tracked DEC private modes (mouse tracking, SGR
		// mouse, bracketed paste, etc.) AFTER scrollback replay so the
		// fresh xterm.js client enters the same mode the running TUI
		// expects. Without this, clicks and wheel scroll break for
		// clients that attach after the TUI emitted the mode-set sequence.
		if syncSeq != "" {
			msg, _ := json.Marshal(map[string]any{
				"type": "output",
				"data": syncSeq,
			})
			c.WriteMessage(websocket.TextMessage, msg)
		}

		defer func() {
			sess.mu.Lock()
			delete(sess.conns, c)
			sess.mu.Unlock()
			c.Close()
		}()

		for {
			_, data, err := c.ReadMessage()
			if err != nil {
				return
			}
			var msg map[string]any
			if err := json.Unmarshal(data, &msg); err != nil {
				continue
			}
			switch msg["type"] {
			case "input":
				if s, ok := msg["data"].(string); ok {
					sess.Pty.ptmx.Write([]byte(s))
				}
			case "resize":
				colsF, okCols := msg["cols"].(float64)
				rowsF, okRows := msg["rows"].(float64)
				if !okCols || !okRows {
					continue
				}
				cols := int(colsF)
				rows := int(rowsF)
				if cols <= 0 || rows <= 0 {
					continue
				}
				sess.mu.Lock()
				sess.resizePTY(cols, rows)
				sess.mu.Unlock()
			}
		}
	})

	mux.HandleFunc("DELETE /api/terminals/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		deleteBranch := r.URL.Query().Get("deleteBranch") == "true"
		sessionsMu.Lock()
		sess, ok := sessions[id]
		if ok {
			if deleteBranch {
				sess.DeleteBranch = true
			}
			delete(sessions, id)
		}
		sessionsMu.Unlock()
		if !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		sess.Pty.Kill()
		// User explicitly closed this pane. Drop the persisted agent-session
		// marker so that reopening the same layout leaf (or a future Caw run
		// that reuses this leafId) starts a fresh agent instead of resuming a
		// session the user intended to discard.
		if store != nil {
			store.ClearAgentSession(id)
		}
		w.WriteHeader(http.StatusOK)
	})
}

func getMainRepoPath(worktreePath string) string {
	gitFile := filepath.Join(worktreePath, ".git")
	data, err := os.ReadFile(gitFile)
	if err != nil {
		return ""
	}
	content := string(data)
	if !strings.HasPrefix(content, "gitdir:") {
		return ""
	}
	gitdir := strings.TrimSpace(strings.TrimPrefix(content, "gitdir:"))

	// gitdir points to <main-repo>/.git/worktrees/<name>
	idx := strings.Index(gitdir, "/.git/worktrees/")
	if idx == -1 {
		idx = strings.Index(gitdir, "\\.git\\worktrees\\")
	}
	if idx != -1 {
		return filepath.Clean(gitdir[:idx])
	}
	return ""
}
