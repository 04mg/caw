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

	"github.com/04mg/caw/internal/httpx"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

type pathResponse struct {
	Path string `json:"path"`
}

type statusResponse struct {
	Status string `json:"status"`
}

func (h *Handler) OpenDir(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Query().Get("path")
	if p == "" {
		httpx.RespondBadRequest(w, "path required")
		return
	}
	abs, err := h.svc.OpenDir(p)
	if err != nil {
		httpx.RespondBadRequest(w, err.Error())
		return
	}
	httpx.RespondJSON(w, pathResponse{Path: abs})
}

func (h *Handler) FileTree(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Query().Get("path")
	if p == "" {
		httpx.RespondBadRequest(w, "path required")
		return
	}
	node, err := h.svc.FileTree(p)
	if err != nil {
		httpx.RespondInternalErr(w, err)
		return
	}
	httpx.RespondJSON(w, node)
}

func (h *Handler) Contents(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("dirs_only") == "true" {
		h.listDir(w, r)
		return
	}
	h.listAll(w, r)
}

func (h *Handler) listDir(w http.ResponseWriter, r *http.Request) {
	children, err := h.svc.ListDir(r.URL.Query().Get("path"))
	if err != nil {
		httpx.RespondBadRequest(w, err.Error())
		return
	}
	httpx.RespondJSON(w, children)
}

func (h *Handler) listAll(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Query().Get("path")
	if p == "" {
		httpx.RespondBadRequest(w, "path required")
		return
	}
	children, err := h.svc.ListAll(p)
	if err != nil {
		httpx.RespondBadRequest(w, err.Error())
		return
	}
	httpx.RespondJSON(w, children)
}

func (h *Handler) SearchDirs(w http.ResponseWriter, r *http.Request) {
	results, err := h.svc.SearchDirs(r.URL.Query().Get("q"), r.URL.Query().Get("root"))
	if err != nil {
		httpx.RespondBadRequest(w, err.Error())
		return
	}
	httpx.RespondJSON(w, results)
}

func (h *Handler) Files(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	path := r.URL.Query().Get("path")
	if q != "" {
		results, err := h.svc.SearchAll(q, r.URL.Query().Get("root"))
		if err != nil {
			httpx.RespondBadRequest(w, err.Error())
			return
		}
		httpx.RespondJSON(w, results)
		return
	}
	if path != "" {
		if r.URL.Query().Get("download") == "true" {
			h.fileDownload(w, r)
			return
		}
		h.fileRead(w, r)
		return
	}
	httpx.RespondBadRequest(w, "query parameter q or path required")
}

func (h *Handler) fileRead(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Query().Get("path")
	resp, err := h.svc.ReadFile(p)
	if err != nil {
		httpx.RespondBadRequest(w, err.Error())
		return
	}
	httpx.RespondJSON(w, resp)
}

func (h *Handler) fileDownload(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Query().Get("path")
	abs, err := filepath.Abs(p)
	if err != nil {
		httpx.RespondBadRequest(w, err.Error())
		return
	}
	info, err := os.Stat(abs)
	if err != nil {
		httpx.RespondNotFound(w, err.Error())
		return
	}
	if !info.IsDir() {
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filepath.Base(abs)))
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", fmt.Sprintf("%d", info.Size()))
		http.ServeFile(w, r, abs)
		return
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
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
			_, err = zw.Create(base + "/" + rel + "/")
			return err
		}
		f, err := zw.Create(base + "/" + rel)
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
		zw.Close()
		httpx.RespondInternalErr(w, err)
		return
	}
	if err := zw.Close(); err != nil {
		httpx.RespondInternalErr(w, err)
		return
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.zip"`, base))
	w.Header().Set("Content-Type", "application/zip")
	w.WriteHeader(http.StatusOK)
	w.Write(buf.Bytes())
}

func (h *Handler) FileWrite(w http.ResponseWriter, r *http.Request) {
	var req WriteRequest
	if !httpx.BindRequest(w, r, &req) {
		return
	}
	if req.Path == "" {
		httpx.RespondBadRequest(w, "path required")
		return
	}
	if err := h.svc.WriteFile(req); err != nil {
		httpx.RespondInternalErr(w, err)
		return
	}
	httpx.RespondJSON(w, statusResponse{Status: "ok"})
}

func (h *Handler) FileDelete(w http.ResponseWriter, r *http.Request) {
	var req DeleteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Path == "" {
		req.Path = r.URL.Query().Get("path")
	}
	if req.Path == "" {
		httpx.RespondBadRequest(w, "path required")
		return
	}
	if err := h.svc.Delete(req.Path); err != nil {
		httpx.RespondInternalErr(w, err)
		return
	}
	httpx.RespondJSON(w, statusResponse{Status: "ok"})
}

func (h *Handler) FileRename(w http.ResponseWriter, r *http.Request) {
	var req RenameRequest
	if !httpx.BindRequest(w, r, &req) {
		return
	}
	if req.OldPath == "" || req.NewPath == "" {
		httpx.RespondBadRequest(w, "oldPath and newPath required")
		return
	}
	if err := h.svc.Rename(req); err != nil {
		httpx.RespondInternalErr(w, err)
		return
	}
	httpx.RespondJSON(w, statusResponse{Status: "ok"})
}

func (h *Handler) FileCreateDispatch(w http.ResponseWriter, r *http.Request) {
	contentType := r.Header.Get("Content-Type")
	if strings.HasPrefix(contentType, "multipart/form-data") {
		h.fileUpload(w, r)
		return
	}
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		httpx.RespondBadRequest(w, err.Error())
		return
	}
	r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

	var data map[string]any
	if err := json.Unmarshal(bodyBytes, &data); err != nil {
		httpx.RespondBadRequest(w, err.Error())
		return
	}

	if _, ok := data["destPath"]; ok {
		h.fileCopy(w, r)
		return
	}
	if _, ok := data["targetDir"]; ok {
		h.filePaste(w, r)
		return
	}
	h.fileCreate(w, r)
}

func (h *Handler) fileUpload(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		httpx.RespondBadRequest(w, err.Error())
		return
	}
	targetDir := r.PostFormValue("targetDir")
	if targetDir == "" {
		httpx.RespondBadRequest(w, "targetDir required")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		httpx.RespondBadRequest(w, err.Error())
		return
	}
	defer file.Close()
	content, err := io.ReadAll(file)
	if err != nil {
		httpx.RespondInternalErr(w, err)
		return
	}
	if err := h.svc.Upload(targetDir, header.Filename, content); err != nil {
		httpx.RespondInternalErr(w, err)
		return
	}
	httpx.RespondJSON(w, statusResponse{Status: "ok"})
}

func (h *Handler) fileCopy(w http.ResponseWriter, r *http.Request) {
	var req CopyRequest
	if !httpx.BindRequest(w, r, &req) {
		return
	}
	if req.SourcePath == "" || req.DestPath == "" {
		httpx.RespondBadRequest(w, "sourcePath and destPath required")
		return
	}
	if err := h.svc.Copy(req); err != nil {
		httpx.RespondInternalErr(w, err)
		return
	}
	httpx.RespondJSON(w, statusResponse{Status: "ok"})
}

func (h *Handler) fileCreate(w http.ResponseWriter, r *http.Request) {
	var req CreateRequest
	if !httpx.BindRequest(w, r, &req) {
		return
	}
	if req.Path == "" || req.Type == "" {
		httpx.RespondBadRequest(w, "path and type required")
		return
	}
	if err := h.svc.Create(req); err != nil {
		httpx.RespondInternalErr(w, err)
		return
	}
	httpx.RespondJSON(w, statusResponse{Status: "ok"})
}

func (h *Handler) filePaste(w http.ResponseWriter, r *http.Request) {
	var req PasteRequest
	if !httpx.BindRequest(w, r, &req) {
		return
	}
	if req.SourcePath == "" || req.TargetDir == "" {
		httpx.RespondBadRequest(w, "sourcePath and targetDir required")
		return
	}
	if err := h.svc.Paste(req); err != nil {
		httpx.RespondBadRequest(w, err.Error())
		return
	}
	httpx.RespondJSON(w, statusResponse{Status: "ok"})
}

func (h *Handler) History(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Action string `json:"action"`
	}
	if !httpx.BindRequest(w, r, &req) {
		return
	}
	switch req.Action {
	case "undo":
		if err := h.svc.Undo(); err != nil {
			httpx.RespondBadRequest(w, err.Error())
			return
		}
	case "redo":
		if err := h.svc.Redo(); err != nil {
			httpx.RespondBadRequest(w, err.Error())
			return
		}
	default:
		httpx.RespondBadRequest(w, "invalid action: must be 'undo' or 'redo'")
		return
	}
	httpx.RespondJSON(w, statusResponse{Status: "ok"})
}

func Register(mux *http.ServeMux) {
	h := NewHandler(NewService())
	mux.HandleFunc("GET /workspaces/details", h.OpenDir)
	mux.HandleFunc("GET /workspaces/trees", h.FileTree)
	mux.HandleFunc("GET /workspaces/contents", h.Contents)
	mux.HandleFunc("GET /workspaces/directories", h.SearchDirs)
	mux.HandleFunc("GET /workspaces/files", h.Files)
	mux.HandleFunc("PUT /workspaces/files", h.FileWrite)
	mux.HandleFunc("DELETE /workspaces/files", h.FileDelete)
	mux.HandleFunc("PATCH /workspaces/files", h.FileRename)
	mux.HandleFunc("POST /workspaces/files", h.FileCreateDispatch)
	mux.HandleFunc("POST /workspaces/history", h.History)
}