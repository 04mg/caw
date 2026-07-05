package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sync"
)

type LayoutNode struct {
	Type        string       `json:"type"`
	ID          string       `json:"id,omitempty"`
	Cwd         string       `json:"cwd,omitempty"`
	Cmd         []string     `json:"cmd,omitempty"`
	AgentID     string       `json:"agentId,omitempty"`
	Orientation string       `json:"orientation,omitempty"`
	Children    []LayoutNode `json:"children,omitempty"`
	Sizes       []float64    `json:"sizes,omitempty"`
	FilePath    string       `json:"filePath,omitempty"`
	IsDiff      bool         `json:"isDiff,omitempty"`
}

type TabLayout struct {
	ID     string     `json:"id"`
	Name   string     `json:"name"`
	Layout LayoutNode `json:"layout"`
}

type Workspace struct {
	ID              string      `json:"id"`
	Path            string      `json:"path"`
	Name            string      `json:"name"`
	Emoji           string      `json:"emoji,omitempty"`
	Layouts         []TabLayout `json:"layouts"`
	ActiveTabIndex  int         `json:"activeTabIndex"`
	ActivePaneID    string      `json:"activePaneId"`
}

type AppState struct {
	Workspaces        []Workspace `json:"workspaces"`
	ActiveWorkspaceID string      `json:"activeWorkspaceId"`
}

var (
	appState     AppState
	appStateMu   sync.RWMutex
	statePath    string
)

func initState() {
	dir, err := os.UserConfigDir()
	if dir == "" || err != nil {
		dir, _ = os.Getwd()
	}
	statePath = filepath.Join(dir, "caw", "state.json")
	_ = os.MkdirAll(filepath.Dir(statePath), 0o755)
	loadStateFile()
}

func loadStateFile() {
	appStateMu.Lock()
	defer appStateMu.Unlock()
	b, err := os.ReadFile(statePath)
	if err != nil {
		return
	}
	_ = json.Unmarshal(b, &appState)
	if appState.Workspaces == nil {
		appState.Workspaces = []Workspace{}
	}
}

func saveStateFile() {
	appStateMu.RLock()
	defer appStateMu.RUnlock()
	b, err := json.MarshalIndent(appState, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(statePath, b, 0o644)
}

func getState() AppState {
	appStateMu.RLock()
	defer appStateMu.RUnlock()
	return AppState{
		Workspaces:        append([]Workspace(nil), appState.Workspaces...),
		ActiveWorkspaceID: appState.ActiveWorkspaceID,
	}
}

func setState(s AppState) {
	appStateMu.Lock()
	appState = s
	if appState.Workspaces == nil {
		appState.Workspaces = []Workspace{}
	}
	appStateMu.Unlock()
	saveStateFile()
}

func handleWorkspaces(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		handleGetWorkspaces(w, r)
	case http.MethodPost:
		handlePostWorkspaces(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func handleGetWorkspaces(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, getState())
}

func handlePostWorkspaces(w http.ResponseWriter, r *http.Request) {
	var s AppState
	if err := readJSON(r, &s); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if s.Workspaces == nil {
		s.Workspaces = []Workspace{}
	}
	setState(s)
	broadcastStateToAll()
	writeJSON(w, map[string]bool{"ok": true})
}