package version

import (
	"net/http"

	"github.com/04mg/caw/internal/httpx"
	"github.com/04mg/caw/internal/update"
)

var Current = "dev"

func Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /version", handleGetVersion)
	mux.HandleFunc("GET /update/changelog", handleGetChangelog)
	mux.HandleFunc("POST /update/check", handleCheckUpdate)
	mux.HandleFunc("POST /update/apply", handleApplyUpdate)
}

func handleGetVersion(w http.ResponseWriter, r *http.Request) {
	httpx.RespondJSON(w, map[string]string{"version": Current})
}

func handleGetChangelog(w http.ResponseWriter, r *http.Request) {
	result, err := update.GetChangelog(Current)
	if err != nil {
		httpx.RespondInternalErr(w, err)
		return
	}
	httpx.RespondJSON(w, result)
}

func handleCheckUpdate(w http.ResponseWriter, r *http.Request) {
	result, err := update.Check(Current)
	if err != nil {
		httpx.RespondInternalErr(w, err)
		return
	}
	httpx.RespondJSON(w, result)
}

func handleApplyUpdate(w http.ResponseWriter, r *http.Request) {
	rel, err := update.Check(Current)
	if err != nil {
		httpx.RespondInternalErr(w, err)
		return
	}
	if !rel.UpdateAvailable {
		httpx.RespondJSON(w, map[string]any{
			"updated":       false,
			"latestVersion": rel.LatestVersion,
			"message":       "You're on the latest version.",
		})
		return
	}

	if err := update.Run(Current); err != nil {
		httpx.RespondInternalErr(w, err)
		return
	}

	httpx.RespondJSON(w, map[string]any{
		"updated":       true,
		"latestVersion": rel.LatestVersion,
		"message":       "Updated. Restart Caw to apply.",
	})
}
