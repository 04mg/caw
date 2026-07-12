package git

import (
	"github.com/gin-gonic/gin"

	"github.com/04mg/caw/internal/httpx"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) Status(c *gin.Context) {
	path := c.Query("path")
	if path == "" {
		httpx.BadRequest(c, "path required")
		return
	}
	result, err := h.svc.Status(path)
	if err != nil {
		httpx.BadRequest(c, err.Error())
		return
	}
	httpx.OK(c, result)
}

func (h *Handler) Diff(c *gin.Context) {
	path := c.Query("path")
	if path == "" {
		httpx.BadRequest(c, "path required")
		return
	}
	content, err := h.svc.Diff(path)
	if err != nil {
		httpx.InternalErr(c, err)
		return
	}
	httpx.OK(c, ContentResponse{Content: content})
}

func (h *Handler) Original(c *gin.Context) {
	path := c.Query("path")
	if path == "" {
		httpx.BadRequest(c, "path required")
		return
	}
	content, err := h.svc.Original(path)
	if err != nil {
		if err == ErrNotGitRepo {
			httpx.BadRequest(c, err.Error())
			return
		}
		httpx.InternalErr(c, err)
		return
	}
	httpx.OK(c, ContentResponse{Content: content})
}

func (h *Handler) Ignored(c *gin.Context) {
	path := c.Query("path")
	if path == "" {
		httpx.BadRequest(c, "path required")
		return
	}
	result, err := h.svc.Ignored(path)
	if err != nil {
		httpx.BadRequest(c, err.Error())
		return
	}
	httpx.OK(c, result)
}

func Register(rg *gin.RouterGroup) {
	h := NewHandler(NewService())
	rg.GET("/git/statuses", h.Status)
	rg.GET("/git/diffs", h.Diff)
	rg.GET("/git/originals", h.Original)
	rg.GET("/git/ignored", h.Ignored)
}