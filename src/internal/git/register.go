package git

import (
	"net/http"

	"github.com/04mg/caw/internal/httpx"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) Status(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Query().Get("path")
	if p == "" {
		httpx.RespondBadRequest(w, "path required")
		return
	}
	result, err := h.svc.Status(p)
	if err != nil {
		httpx.RespondBadRequest(w, err.Error())
		return
	}
	httpx.RespondJSON(w, result)
}

func (h *Handler) Diff(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Query().Get("path")
	if p == "" {
		httpx.RespondBadRequest(w, "path required")
		return
	}
	content, err := h.svc.Diff(p)
	if err != nil {
		httpx.RespondInternalErr(w, err)
		return
	}
	httpx.RespondJSON(w, ContentResponse{Content: content})
}

func (h *Handler) Original(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Query().Get("path")
	if p == "" {
		httpx.RespondBadRequest(w, "path required")
		return
	}
	content, err := h.svc.Original(p)
	if err != nil {
		if err == ErrNotGitRepo {
			httpx.RespondBadRequest(w, err.Error())
			return
		}
		httpx.RespondInternalErr(w, err)
		return
	}
	httpx.RespondJSON(w, ContentResponse{Content: content})
}

func (h *Handler) Ignored(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Query().Get("path")
	if p == "" {
		httpx.RespondBadRequest(w, "path required")
		return
	}
	result, err := h.svc.Ignored(p)
	if err != nil {
		httpx.RespondBadRequest(w, err.Error())
		return
	}
	httpx.RespondJSON(w, result)
}

func Register(mux *http.ServeMux) {
	svc := NewService()
	h := NewHandler(svc)
	mux.HandleFunc("GET /git/statuses", h.Status)
	mux.HandleFunc("GET /git/diffs", h.Diff)
	mux.HandleFunc("GET /git/originals", h.Original)
	mux.HandleFunc("GET /git/ignored", h.Ignored)
}

func RegisterWithService(mux *http.ServeMux, svc *Service) {
	h := NewHandler(svc)
	mux.HandleFunc("GET /git/statuses", h.Status)
	mux.HandleFunc("GET /git/diffs", h.Diff)
	mux.HandleFunc("GET /git/originals", h.Original)
	mux.HandleFunc("GET /git/ignored", h.Ignored)
}