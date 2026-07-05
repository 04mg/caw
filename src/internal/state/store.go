package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

type Store struct {
	Mu     sync.RWMutex
	state  AppState
	path   string
}

func NewStore(statePath string) *Store {
	s := &Store{path: statePath}
	s.load()
	if s.state.Workspaces == nil {
		s.state.Workspaces = []Workspace{}
	}
	return s
}

func (s *Store) load() {
	b, err := os.ReadFile(s.path)
	if err != nil {
		return
	}
	_ = json.Unmarshal(b, &s.state)
	if s.state.Workspaces == nil {
		s.state.Workspaces = []Workspace{}
	}
}

func (s *Store) save() {
	s.Mu.RLock()
	defer s.Mu.RUnlock()
	b, err := json.MarshalIndent(s.state, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(s.path, b, 0o644)
}

func (s *Store) Get() AppState {
	s.Mu.RLock()
	defer s.Mu.RUnlock()
	return AppState{
		Workspaces:        append([]Workspace(nil), s.state.Workspaces...),
		ActiveWorkspaceID: s.state.ActiveWorkspaceID,
	}
}

func (s *Store) Set(as AppState) {
	s.Mu.Lock()
	s.state = as
	if s.state.Workspaces == nil {
		s.state.Workspaces = []Workspace{}
	}
	s.Mu.Unlock()
	s.save()
}

func DefaultStatePath() string {
	dir, err := os.UserConfigDir()
	if dir == "" || err != nil {
		dir, _ = os.Getwd()
	}
	p := filepath.Join(dir, "caw", "state.json")
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	return p
}
