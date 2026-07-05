package agent

import (
	"net/http"
	"os/exec"

	"github.com/04mg/caw/internal/httputil"
)

type Info struct {
	ID    string   `json:"id"`
	Label string   `json:"label"`
	Cmd   []string `json:"cmd"`
}

func Register(mux *http.ServeMux) {
	mux.HandleFunc("/api/agents/available", func(w http.ResponseWriter, r *http.Request) {
		agentsList := []Info{
			{ID: "opencode", Label: "OpenCode", Cmd: []string{"opencode"}},
			{ID: "agy", Label: "Antigravity", Cmd: []string{"agy"}},
			{ID: "claude", Label: "Claude Code", Cmd: []string{"claude"}},
		}
		available := []Info{}
		for _, a := range agentsList {
			if _, err := exec.LookPath(a.Cmd[0]); err == nil {
				available = append(available, a)
			}
		}
		httputil.WriteJSON(w, available)
	})
}
