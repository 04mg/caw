package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

//go:embed frontend/dist
var frontendFS embed.FS

//go:embed icon.txt
var iconTxt string

type TerminalSession struct {
	ID         string
	Pty        *ptySession
	Cwd        string
	mu         sync.Mutex
	conns      map[*websocket.Conn]bool
	scrollback []byte
}

var (
	sessions   = make(map[string]*TerminalSession)
	sessionsMu sync.RWMutex
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func main() {
	port := "8080"
	if p := os.Getenv("PORT"); p != "" {
		port = p
	}

	distFS, err := fs.Sub(frontendFS, "frontend/dist")
	if err != nil {
		log.Fatalf("embed sub: %v", err)
	}

	initState()

	mux := http.NewServeMux()
	mux.HandleFunc("/api/workspace/tree", handleFileTree)
	mux.HandleFunc("/api/workspace/list", handleListDir)
	mux.HandleFunc("/api/workspace/search", handleSearchDirs)
	mux.HandleFunc("/api/workspace/open", handleOpenDir)
	mux.HandleFunc("/api/terminal/create", handleTerminalCreate)
	mux.HandleFunc("/ws/terminal/", handleTerminalWS)
	mux.HandleFunc("/api/workspaces", handleWorkspaces)
	mux.HandleFunc("/ws/state", handleStateWS)
	mux.HandleFunc("/api/agents/available", handleAgentsAvailable)
	mux.Handle("/", http.FileServer(http.FS(distFS)))

	addr := ":" + port
	fmt.Print(strings.Replace(iconTxt, ":8080", addr, 1))
	log.Fatal(http.ListenAndServe(addr, mux))
}

func handleOpenDir(w http.ResponseWriter, r *http.Request) {
	dir := r.URL.Query().Get("path")
	if dir == "" {
		http.Error(w, "path required", http.StatusBadRequest)
		return
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	info, err := os.Stat(abs)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if !info.IsDir() {
		http.Error(w, "not a directory", http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]string{"path": abs})
}

type FileNode struct {
	Name     string     `json:"name"`
	Path     string     `json:"path"`
	IsDir    bool       `json:"isDir"`
	Children []FileNode `json:"children,omitempty"`
}

func handleFileTree(w http.ResponseWriter, r *http.Request) {
	root := r.URL.Query().Get("path")
	if root == "" {
		http.Error(w, "path required", http.StatusBadRequest)
		return
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	node, err := buildTree(abs, 2)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, node)
}

func buildTree(dir string, depth int) (FileNode, error) {
	info, err := os.Stat(dir)
	if err != nil {
		return FileNode{}, err
	}
	node := FileNode{
		Name:  filepath.Base(dir),
		Path:  dir,
		IsDir: info.IsDir(),
	}
	if !info.IsDir() || depth <= 0 {
		return node, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return node, nil
	}
	for _, e := range entries {
		name := e.Name()
		if name[0] == '.' {
			continue
		}
		child, err := buildTree(filepath.Join(dir, name), depth-1)
		if err != nil {
			continue
		}
		node.Children = append(node.Children, child)
	}
	return node, nil
}

type CreateTerminalRequest struct {
	Cwd string   `json:"cwd"`
	ID  string   `json:"id"`
	Cmd []string `json:"cmd,omitempty"`
}

func handleTerminalCreate(w http.ResponseWriter, r *http.Request) {
	var req CreateTerminalRequest
	if err := readJSON(r, &req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	cwd := req.Cwd
	if cwd == "" {
		cwd, _ = os.Getwd()
	}

	// If a client-supplied id is provided and a session already exists with
	// that id, reuse it (e.g. after a browser reload while the server kept
	// running). Otherwise create a fresh pty using the supplied id (when
	// present) so the client and server stay in sync.
	if req.ID != "" {
		sessionsMu.RLock()
		existing, ok := sessions[req.ID]
		sessionsMu.RUnlock()
		if ok {
			writeJSON(w, map[string]string{"id": existing.ID})
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

	sess := &TerminalSession{
		ID:         id,
		Pty:        ps,
		Cwd:        cwd,
		conns:      make(map[*websocket.Conn]bool),
		scrollback: []byte{},
	}
	sessionsMu.Lock()
	sessions[id] = sess
	sessionsMu.Unlock()

	go sess.readLoop()

	writeJSON(w, map[string]string{"id": id})
}

func handleTerminalWS(w http.ResponseWriter, r *http.Request) {
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
	sess.mu.Unlock()

	if len(sess.scrollback) > 0 {
		msg, _ := json.Marshal(map[string]interface{}{
			"type": "output",
			"data": string(sess.scrollback),
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
		var msg map[string]interface{}
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
}

func (s *TerminalSession) readLoop() {
	buf := make([]byte, 4096)
	for {
		n, err := s.Pty.ptmx.Read(buf)
		if n > 0 {
			data := buf[:n]
			s.mu.Lock()
			s.scrollback = append(s.scrollback, data...)
			if len(s.scrollback) > 256*1024 {
				s.scrollback = append([]byte(nil), s.scrollback[len(s.scrollback)-256*1024:]...)
			}
			for c := range s.conns {
				msg, _ := json.Marshal(map[string]interface{}{
					"type": "output",
					"data": string(data),
				})
				c.WriteMessage(websocket.TextMessage, msg)
			}
			s.mu.Unlock()
		}
		if err != nil {
			break
		}
	}
}

func handleListDir(w http.ResponseWriter, r *http.Request) {
	dir := r.URL.Query().Get("path")
	if dir == "" {
		dir = "/"
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	info, err := os.Stat(abs)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if !info.IsDir() {
		http.Error(w, "not a directory", http.StatusBadRequest)
		return
	}
	entries, err := os.ReadDir(abs)
	if err != nil {
		writeJSON(w, []FileNode{})
		return
	}
	var children []FileNode
	for _, e := range entries {
		name := e.Name()
		if name[0] == '.' && name != "." && name != ".." {
			continue
		}
		if !e.IsDir() {
			continue
		}
		children = append(children, FileNode{
			Name:  name,
			Path:  filepath.Join(abs, name),
			IsDir: true,
		})
	}
	writeJSON(w, children)
}

func handleSearchDirs(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	root := r.URL.Query().Get("root")
	if root == "" {
		root = "/"
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	info, err := os.Stat(abs)
	if err != nil || !info.IsDir() {
		writeJSON(w, []FileNode{})
		return
	}

	// Only list immediate subdirectories of root whose name contains q.
	// No recursive walk — keeps results relevant and bounded.
	entries, err := os.ReadDir(abs)
	if err != nil {
		writeJSON(w, []FileNode{})
		return
	}

	results := []FileNode{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if name[0] == '.' {
			continue
		}
		if q == "" || containsFold(name, q) {
			results = append(results, FileNode{
				Name:  name,
				Path:  filepath.Join(abs, name),
				IsDir: true,
			})
		}
	}

	writeJSON(w, results)
}

func containsFold(s, substr string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func readJSON(r *http.Request, v interface{}) error {
	body, err := readAll(r)
	if err != nil {
		return err
	}
	return json.Unmarshal(body, v)
}

func readAll(r *http.Request) ([]byte, error) {
	defer r.Body.Close()
	buf := make([]byte, 0, 4096)
	tmp := make([]byte, 4096)
	for {
		n, err := r.Body.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
		}
		if err != nil {
			break
		}
	}
	return buf, nil
}

type AgentInfo struct {
	ID    string   `json:"id"`
	Label string   `json:"label"`
	Cmd   []string `json:"cmd"`
}

func handleAgentsAvailable(w http.ResponseWriter, r *http.Request) {
	agentsList := []AgentInfo{
		{ID: "opencode", Label: "OpenCode", Cmd: []string{"opencode"}},
		{ID: "agy", Label: "agy", Cmd: []string{"agy"}},
		{ID: "claude", Label: "Claude Code", Cmd: []string{"claude"}},
	}
	available := []AgentInfo{}
	for _, a := range agentsList {
		if _, err := exec.LookPath(a.Cmd[0]); err == nil {
			available = append(available, a)
		}
	}
	writeJSON(w, available)
}

