package state

import (
	"encoding/json"

	"github.com/04mg/caw/internal/ws"
)

type Service struct {
	store *Store
	mux   *ws.Multiplexer
}

func NewService(store *Store, mux *ws.Multiplexer) *Service {
	return &Service{store: store, mux: mux}
}

func (s *Service) Get() AppState {
	return s.store.Get()
}

func (s *Service) Set(as AppState) {
	if as.Workspaces == nil {
		as.Workspaces = []Workspace{}
	}
	s.store.Set(as)
	s.broadcast()
}

func (s *Service) broadcast() {
	cur := s.store.Get()
	curJSON, _ := json.Marshal(cur)
	lastStateMu.Lock()
	if string(curJSON) == string(lastStateJSON) {
		lastStateMu.Unlock()
		return
	}
	lastStateJSON = curJSON
	lastStateMu.Unlock()
	s.mux.Broadcast("state", cur)
}

func (s *Service) broadcastExcept(exclude *ws.MuxClient) {
	cur := s.store.Get()
	s.mux.BroadcastExcept("state", cur, exclude)
}