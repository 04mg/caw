package server

import (
	"fmt"
	"io/fs"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/04mg/caw/internal/agent"
	_ "github.com/04mg/caw/internal/agent/agents"
	"github.com/04mg/caw/internal/embed"
	"github.com/04mg/caw/internal/git"
	"github.com/04mg/caw/internal/httpx"
	"github.com/04mg/caw/internal/push"
	"github.com/04mg/caw/internal/quota"
	_ "github.com/04mg/caw/internal/quota/providers"
	"github.com/04mg/caw/internal/state"
	"github.com/04mg/caw/internal/terminal"
	"github.com/04mg/caw/internal/workspace"
	"github.com/04mg/caw/internal/ws"
)

type Server struct {
	store      *state.Store
	frontendFS fs.FS
	hub        *ws.Hub
	mux        *ws.Multiplexer
	gitSvc     *git.Service
}

func New() *Server {
	gin.SetMode(gin.ReleaseMode)

	frontendFS, err := fs.Sub(embed.FrontendFS, "frontend/dist")
	if err != nil {
		log.Fatalf("embed sub: %v", err)
	}

	mux := ws.NewMultiplexer()

	s := &Server{
		frontendFS: frontendFS,
		store:      state.NewStore(state.DefaultDBPath()),
		hub:        ws.NewHub(),
		mux:        mux,
	}
	state.RegisterMuxChannel(mux, s.store)
	agent.RegisterMuxChannel(mux)
	workspace.RegisterMuxChannel(mux)
	gitSvc := git.NewService()
	git.RegisterMuxChannel(mux, gitSvc)
	push.EnsureVAPIDKeys(s.store)
	agent.SetPushStore(s.store)
	agent.SetStateStore(s.store)
	agent.SetStatusMux(mux)
	s.gitSvc = gitSvc
	return s
}

func (s *Server) Handler() http.Handler {
	api := http.NewServeMux()
	git.RegisterWithService(api, s.gitSvc)
	quota.Register(api, s.store)
	push.Register(api, s.store)

	httpx.RegisterAPIGin(api, func(rg *gin.RouterGroup) {
		terminal.Register(rg, s.store, &ws.TerminalUpgrader)
		state.RegisterHTTP(rg, s.store, s.mux)
		workspace.Register(rg)
		agent.Register(rg)
	})

	mux := http.NewServeMux()
	mux.Handle("/api/", http.StripPrefix("/api", api))

	mux.HandleFunc("GET /ws", s.mux.HandleMuxWS)
	mux.HandleFunc("GET /ws/state", func(w http.ResponseWriter, r *http.Request) {
		state.HandleStateWS(w, r, s.store, s.hub)
	})
	mux.HandleFunc("GET /ws/workspaces/files", workspace.HandleFilesWS)
	mux.HandleFunc("GET /ws/agents/statuses", func(w http.ResponseWriter, r *http.Request) {
		agent.HandleStatusWS(w, r, agent.StatusHub())
	})
	mux.HandleFunc("GET /ws/terminals/{id}", func(w http.ResponseWriter, r *http.Request) {
		terminal.HandleTerminalWS(w, r, r.PathValue("id"), &ws.TerminalUpgrader)
	})

	mux.HandleFunc("/", httpx.SPAHandler(s.frontendFS))

	return httpx.Chain(mux, httpx.Recovery, httpx.Logger)
}

func (s *Server) ListenAndServe(host, port string) {
	addr := host + ":" + port
	fmt.Print(embed.IconTxt)
	fmt.Printf("\033[38;2;255;150;150m└─\033[0m \033[38;2;255;150;150mlistening on \033[1;4m%s\033[0m\n", addr)
	log.Fatal(http.ListenAndServe(addr, s.Handler()))
}