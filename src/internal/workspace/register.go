package workspace

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/04mg/caw/internal/httpx"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) OpenDir(c *gin.Context) {
	path := c.Query("path")
	if path == "" {
		httpx.BadRequest(c, "path required")
		return
	}
	abs, err := h.svc.OpenDir(path)
	if err != nil {
		httpx.BadRequest(c, err.Error())
		return
	}
	httpx.OK(c, map[string]string{"path": abs})
}

func (h *Handler) FileTree(c *gin.Context) {
	path := c.Query("path")
	if path == "" {
		httpx.BadRequest(c, "path required")
		return
	}
	node, err := h.svc.FileTree(path)
	if err != nil {
		httpx.InternalErr(c, err)
		return
	}
	httpx.OK(c, node)
}

func (h *Handler) Contents(c *gin.Context) {
	if c.Query("dirs_only") == "true" {
		h.listDir(c)
		return
	}
	h.listAll(c)
}

func (h *Handler) listDir(c *gin.Context) {
	children, err := h.svc.ListDir(c.Query("path"))
	if err != nil {
		httpx.BadRequest(c, err.Error())
		return
	}
	httpx.OK(c, children)
}

func (h *Handler) listAll(c *gin.Context) {
	path := c.Query("path")
	if path == "" {
		httpx.BadRequest(c, "path required")
		return
	}
	children, err := h.svc.ListAll(path)
	if err != nil {
		httpx.BadRequest(c, err.Error())
		return
	}
	httpx.OK(c, children)
}

func (h *Handler) SearchDirs(c *gin.Context) {
	results, err := h.svc.SearchDirs(c.Query("q"), c.Query("root"))
	if err != nil {
		httpx.BadRequest(c, err.Error())
		return
	}
	httpx.OK(c, results)
}

func (h *Handler) Files(c *gin.Context) {
	q := c.Query("q")
	path := c.Query("path")
	if q != "" {
		results, err := h.svc.SearchAll(q, c.Query("root"))
		if err != nil {
			httpx.BadRequest(c, err.Error())
			return
		}
		httpx.OK(c, results)
		return
	}
	if path != "" {
		if c.Query("download") == "true" {
			h.fileDownload(c)
			return
		}
		h.fileRead(c)
		return
	}
	httpx.BadRequest(c, "query parameter q or path required")
}

func (h *Handler) fileRead(c *gin.Context) {
	path := c.Query("path")
	resp, err := h.svc.ReadFile(path)
	if err != nil {
		httpx.BadRequest(c, err.Error())
		return
	}
	httpx.OK(c, resp)
}

func (h *Handler) fileDownload(c *gin.Context) {
	path := c.Query("path")
	abs, err := filepath.Abs(path)
	if err != nil {
		httpx.BadRequest(c, err.Error())
		return
	}
	info, err := os.Stat(abs)
	if err != nil {
		httpx.NotFound(c, err.Error())
		return
	}
	if !info.IsDir() {
		c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filepath.Base(abs)))
		c.Header("Content-Type", "application/octet-stream")
		c.Header("Content-Length", fmt.Sprintf("%d", info.Size()))
		c.File(abs)
		return
	}
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	base := filepath.Base(abs)
	err = filepath.Walk(abs, func(p string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(filepath.Dir(abs), p)
		if err != nil {
			return err
		}
		if fi.IsDir() {
			_, err = w.Create(base + "/" + rel + "/")
			return err
		}
		f, err := w.Create(base + "/" + rel)
		if err != nil {
			return err
		}
		src, err := os.Open(p)
		if err != nil {
			return err
		}
		defer src.Close()
		_, err = io.Copy(f, src)
		return err
	})
	if err != nil {
		w.Close()
		httpx.InternalErr(c, err)
		return
	}
	if err := w.Close(); err != nil {
		httpx.InternalErr(c, err)
		return
	}
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.zip"`, base))
	c.Header("Content-Type", "application/zip")
	c.Data(http.StatusOK, "application/zip", buf.Bytes())
}

func (h *Handler) FileWrite(c *gin.Context) {
	var req WriteRequest
	if !httpx.Bind(c, &req) {
		return
	}
	if req.Path == "" {
		httpx.BadRequest(c, "path required")
		return
	}
	if err := h.svc.WriteFile(req); err != nil {
		httpx.InternalErr(c, err)
		return
	}
	httpx.OK(c, map[string]string{"status": "ok"})
}

func (h *Handler) FileDelete(c *gin.Context) {
	var req DeleteRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.Path == "" {
		req.Path = c.Query("path")
	}
	if req.Path == "" {
		httpx.BadRequest(c, "path required")
		return
	}
	if err := h.svc.Delete(req.Path); err != nil {
		httpx.InternalErr(c, err)
		return
	}
	httpx.OK(c, map[string]string{"status": "ok"})
}

func (h *Handler) FileRename(c *gin.Context) {
	var req RenameRequest
	if !httpx.Bind(c, &req) {
		return
	}
	if req.OldPath == "" || req.NewPath == "" {
		httpx.BadRequest(c, "oldPath and newPath required")
		return
	}
	if err := h.svc.Rename(req); err != nil {
		httpx.InternalErr(c, err)
		return
	}
	httpx.OK(c, map[string]string{"status": "ok"})
}

func (h *Handler) FileCreateDispatch(c *gin.Context) {
	contentType := c.GetHeader("Content-Type")
	if strings.HasPrefix(contentType, "multipart/form-data") {
		h.fileUpload(c)
		return
	}
	bodyBytes, err := io.ReadAll(c.Request.Body)
	if err != nil {
		httpx.BadRequest(c, err.Error())
		return
	}
	c.Request.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

	var data map[string]any
	if err := json.Unmarshal(bodyBytes, &data); err != nil {
		httpx.BadRequest(c, err.Error())
		return
	}

	if _, ok := data["destPath"]; ok {
		h.fileCopy(c)
		return
	}
	if _, ok := data["targetDir"]; ok {
		h.filePaste(c)
		return
	}
	h.fileCreate(c)
}

func (h *Handler) fileUpload(c *gin.Context) {
	if err := c.Request.ParseMultipartForm(32 << 20); err != nil {
		httpx.BadRequest(c, err.Error())
		return
	}
	targetDir := c.PostForm("targetDir")
	if targetDir == "" {
		httpx.BadRequest(c, "targetDir required")
		return
	}
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		httpx.BadRequest(c, err.Error())
		return
	}
	defer file.Close()
	content, err := io.ReadAll(file)
	if err != nil {
		httpx.InternalErr(c, err)
		return
	}
	if err := h.svc.Upload(targetDir, header.Filename, content); err != nil {
		httpx.InternalErr(c, err)
		return
	}
	httpx.OK(c, map[string]string{"status": "ok"})
}

func (h *Handler) fileCopy(c *gin.Context) {
	var req CopyRequest
	if !httpx.Bind(c, &req) {
		return
	}
	if req.SourcePath == "" || req.DestPath == "" {
		httpx.BadRequest(c, "sourcePath and destPath required")
		return
	}
	if err := h.svc.Copy(req); err != nil {
		httpx.InternalErr(c, err)
		return
	}
	httpx.OK(c, map[string]string{"status": "ok"})
}

func (h *Handler) fileCreate(c *gin.Context) {
	var req CreateRequest
	if !httpx.Bind(c, &req) {
		return
	}
	if req.Path == "" || req.Type == "" {
		httpx.BadRequest(c, "path and type required")
		return
	}
	if err := h.svc.Create(req); err != nil {
		httpx.InternalErr(c, err)
		return
	}
	httpx.OK(c, map[string]string{"status": "ok"})
}

func (h *Handler) filePaste(c *gin.Context) {
	var req PasteRequest
	if !httpx.Bind(c, &req) {
		return
	}
	if req.SourcePath == "" || req.TargetDir == "" {
		httpx.BadRequest(c, "sourcePath and targetDir required")
		return
	}
	if err := h.svc.Paste(req); err != nil {
		httpx.BadRequest(c, err.Error())
		return
	}
	httpx.OK(c, map[string]string{"status": "ok"})
}

func (h *Handler) History(c *gin.Context) {
	var req struct {
		Action string `json:"action"`
	}
	if !httpx.Bind(c, &req) {
		return
	}
	switch req.Action {
	case "undo":
		if err := h.svc.Undo(); err != nil {
			httpx.BadRequest(c, err.Error())
			return
		}
	case "redo":
		if err := h.svc.Redo(); err != nil {
			httpx.BadRequest(c, err.Error())
			return
		}
	default:
		httpx.BadRequest(c, "invalid action: must be 'undo' or 'redo'")
		return
	}
	httpx.OK(c, map[string]string{"status": "ok"})
}

func Register(rg *gin.RouterGroup) {
	h := NewHandler(NewService())
	rg.GET("/workspaces/details", h.OpenDir)
	rg.GET("/workspaces/trees", h.FileTree)
	rg.GET("/workspaces/contents", h.Contents)
	rg.GET("/workspaces/directories", h.SearchDirs)
	rg.GET("/workspaces/files", h.Files)
	rg.PUT("/workspaces/files", h.FileWrite)
	rg.DELETE("/workspaces/files", h.FileDelete)
	rg.PATCH("/workspaces/files", h.FileRename)
	rg.POST("/workspaces/files", h.FileCreateDispatch)
	rg.POST("/workspaces/history", h.History)
}