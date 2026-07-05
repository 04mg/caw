package server

import (
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"strings"
	"sync"

	"github.com/gorilla/websocket"
	"github.com/04mg/caw/internal/agent"
	"github.com/04mg/caw/internal/embed"
	"github.com/04mg/caw/internal/git"
	"github.com/04mg/caw/internal/state"
	"github.com/04mg/caw/internal/terminal"
	"github.com/04mg/caw/internal/workspace"
)

type Server struct {
	sessions   map[string]*terminal.Session
	sessionsMu sync.RWMutex
	upgrader   websocket.Upgrader
	store      *state.Store
	frontendFS fs.FS
}

func New() *Server {
	frontendFS, err := fs.Sub(embed.FrontendFS, "frontend/dist")
	if err != nil {
		log.Fatalf("embed sub: %v", err)
	}

	return &Server{
		sessions:   make(map[string]*terminal.Session),
		upgrader:   websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }},
		store:      state.NewStore(state.DefaultStatePath()),
		frontendFS: frontendFS,
	}
}

func (s *Server) ListenAndServe(port string) {
	mux := http.NewServeMux()

	terminal.Register(mux, s.sessions, &s.sessionsMu, &s.upgrader)
	state.RegisterHTTP(mux, s.store)
	state.RegisterWS(mux, s.store)
	workspace.Register(mux)
	git.Register(mux)
	agent.Register(mux)

	mux.Handle("/", http.FileServer(http.FS(s.frontendFS)))

	addr := ":" + port
	fmt.Print(strings.Replace(embed.IconTxt, ":8080", addr, 1))
	log.Fatal(http.ListenAndServe(addr, mux))
}
