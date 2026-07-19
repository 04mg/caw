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
	agent.SetStatusMux(mux)
	s.gitSvc = gitSvc
	return s
}

func (s *Server) Engine() *gin.Engine {
	r := gin.New()
	httpx.InstallErrorMiddleware(r)

	api := r.Group("/api")
	{
		terminal.Register(api, s.store, &ws.TerminalUpgrader)
		state.RegisterHTTP(api, s.store, s.mux)
		workspace.Register(api)
		git.RegisterWithService(api, s.gitSvc)
		agent.Register(api)
		quota.Register(api, s.store)
		push.Register(api, s.store)
	}

	r.GET("/ws", func(c *gin.Context) {
		s.mux.HandleMuxWS(c.Writer, c.Request)
	})
	r.GET("/ws/state", func(c *gin.Context) {
		state.HandleStateWS(c.Writer, c.Request, s.store, s.hub)
	})
	r.GET("/ws/workspaces/files", gin.WrapF(workspace.HandleFilesWS))
	r.GET("/ws/agents/statuses", func(c *gin.Context) {
		agent.HandleStatusWS(c.Writer, c.Request, agent.StatusHub())
	})
	r.GET("/ws/terminals/:id", func(c *gin.Context) {
		terminal.HandleTerminalWS(c.Writer, c.Request, c.Param("id"), &ws.TerminalUpgrader)
	})

	r.NoRoute(func(c *gin.Context) {
		http.FileServer(http.FS(s.frontendFS)).ServeHTTP(c.Writer, c.Request)
	})

	return r
}

func (s *Server) ListenAndServe(host, port string) {
	addr := host + ":" + port
	fmt.Print(embed.IconTxt)
	fmt.Printf("\033[38;2;255;150;150m└─\033[0m \033[38;2;255;150;150mlistening on \033[1;4m%s\033[0m\n", addr)
	log.Fatal(http.ListenAndServe(addr, s.Engine()))
}