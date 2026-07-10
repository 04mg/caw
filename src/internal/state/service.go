package state

import "github.com/04mg/caw/internal/ws"

type Service struct {
	store *Store
	hub   *ws.Hub
}

func NewService(store *Store, hub *ws.Hub) *Service {
	return &Service{store: store, hub: hub}
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
	s.hub.BroadcastText(cur)
}

func (s *Service) broadcastExcept(exclude *ws.Client) {
	cur := s.store.Get()
	s.hub.BroadcastTextExcept(cur, exclude)
}