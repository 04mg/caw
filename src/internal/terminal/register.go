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
)

type CreateRequest struct {
	Cwd string   `json:"cwd"`
	ID  string   `json:"id"`
	Cmd []string `json:"cmd,omitempty"`
}

type KillRequest struct {
	ID string `json:"id"`
}

func Register(mux *http.ServeMux, sessions map[string]*Session, sessionsMu *sync.RWMutex, upgrader *websocket.Upgrader) {
	mux.HandleFunc("/api/terminal/create", func(w http.ResponseWriter, r *http.Request) {
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

		ps, err := startPty(cwd, req.Cmd)
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
			onExit: func() {
				sessionsMu.Lock()
				delete(sessions, id)
				sessionsMu.Unlock()

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
								cmdRemove := exec.Command("git", "worktree", "remove", "--force", cleanCwd)
								cmdRemove.Dir = mainRepo
								_ = cmdRemove.Run()
							}
							_ = os.RemoveAll(cleanCwd)
						}()
					}
				}
			},
		}
		sessionsMu.Lock()
		sessions[id] = sess
		sessionsMu.Unlock()

		go sess.ReadLoop()

		httputil.WriteJSON(w, map[string]string{"id": id})
	})

	mux.HandleFunc("/ws/terminal/", func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Path[len("/ws/terminal/"):]
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
				sess.Pty.ptmx.Resize(int(colsF), int(rowsF))
			}
		}
	})

	mux.HandleFunc("/api/terminal/kill", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req KillRequest
		if err := httputil.ReadJSON(r, &req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		sessionsMu.Lock()
		sess, ok := sessions[req.ID]
		if ok {
			delete(sessions, req.ID)
		}
		sessionsMu.Unlock()
		if !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		sess.Pty.Kill()
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
