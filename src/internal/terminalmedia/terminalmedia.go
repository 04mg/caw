package terminalmedia

import (
	"bytes"
	"database/sql"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/04mg/caw/internal/httpx"
	"github.com/04mg/caw/internal/state"
)

const (
	routeBase            = "/terminal/background-assets"
	maxImageBytes  int64 = 32 << 20
	maxVideoBytes  int64 = 150 << 20
	maxUploadBytes       = maxVideoBytes
)

type Handler struct {
	store *state.Store
}

type Asset struct {
	ID          string `json:"id"`
	Filename    string `json:"filename"`
	Kind        string `json:"kind"`
	ContentType string `json:"contentType"`
	SizeBytes   int64  `json:"sizeBytes"`
	CreatedAt   string `json:"createdAt"`
	ContentURL  string `json:"contentUrl"`
}

type mediaConfig struct {
	Kind        string
	ContentType string
	StoredExt   string
	AllowedExts map[string]bool
	MaxBytes    int64
}

type requestError struct {
	status  int
	message string
}

func (e *requestError) Error() string { return e.message }

var (
	errMissingFile = &requestError{status: http.StatusBadRequest, message: "missing file field"}
	errLocalOnly   = &requestError{status: http.StatusForbidden, message: "terminal background assets are only available to local requests"}
)

var (
	pngConfig = mediaConfig{
		Kind:        "image",
		ContentType: "image/png",
		StoredExt:   ".png",
		AllowedExts: map[string]bool{".png": true},
		MaxBytes:    maxImageBytes,
	}
	jpegConfig = mediaConfig{
		Kind:        "image",
		ContentType: "image/jpeg",
		StoredExt:   ".jpg",
		AllowedExts: map[string]bool{".jpg": true, ".jpeg": true},
		MaxBytes:    maxImageBytes,
	}
	gifConfig = mediaConfig{
		Kind:        "image",
		ContentType: "image/gif",
		StoredExt:   ".gif",
		AllowedExts: map[string]bool{".gif": true},
		MaxBytes:    maxImageBytes,
	}
	webpConfig = mediaConfig{
		Kind:        "image",
		ContentType: "image/webp",
		StoredExt:   ".webp",
		AllowedExts: map[string]bool{".webp": true},
		MaxBytes:    maxImageBytes,
	}
	mp4Config = mediaConfig{
		Kind:        "video",
		ContentType: "video/mp4",
		StoredExt:   ".mp4",
		AllowedExts: map[string]bool{".mp4": true},
		MaxBytes:    maxVideoBytes,
	}
	webmConfig = mediaConfig{
		Kind:        "video",
		ContentType: "video/webm",
		StoredExt:   ".webm",
		AllowedExts: map[string]bool{".webm": true},
		MaxBytes:    maxVideoBytes,
	}
)

func Register(mux *http.ServeMux, store *state.Store) {
	_ = os.MkdirAll(DataDir(), 0o755)
	h := &Handler{store: store}
	mux.HandleFunc("GET "+routeBase, h.List)
	mux.HandleFunc("POST "+routeBase, h.Upload)
	mux.HandleFunc("GET "+routeBase+"/{id}", h.Get)
	mux.HandleFunc("GET "+routeBase+"/{id}/content", h.Content)
	mux.HandleFunc("DELETE "+routeBase+"/{id}", h.Delete)
}

func DataDir() string {
	return filepath.Join(filepath.Dir(state.DefaultDBPath()), "terminal-background-assets")
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	if !authorizeLocalRequest(w, r) {
		return
	}
	assets, err := h.store.ListTerminalBackgroundAssets()
	if err != nil {
		httpx.RespondInternalErr(w, err)
		return
	}
	httpx.RespondJSON(w, toAPIAssets(assets))
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	if !authorizeLocalRequest(w, r) {
		return
	}
	asset, err := h.store.GetTerminalBackgroundAsset(strings.TrimSpace(r.PathValue("id")))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			httpx.RespondNotFound(w, "asset not found")
			return
		}
		httpx.RespondInternalErr(w, err)
		return
	}
	httpx.RespondJSON(w, toAPIAsset(asset))
}

func (h *Handler) Upload(w http.ResponseWriter, r *http.Request) {
	if !authorizeLocalRequest(w, r) {
		return
	}

	asset, err := h.storeUploadedAsset(w, r)
	if err != nil {
		respondRequestError(w, err)
		return
	}
	httpx.RespondCreated(w, toAPIAsset(asset))
}

func (h *Handler) Content(w http.ResponseWriter, r *http.Request) {
	if !authorizeLocalRequest(w, r) {
		return
	}

	asset, err := h.store.GetTerminalBackgroundAsset(strings.TrimSpace(r.PathValue("id")))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			httpx.RespondNotFound(w, "asset not found")
			return
		}
		httpx.RespondInternalErr(w, err)
		return
	}

	path := assetPath(asset)
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			httpx.RespondNotFound(w, "asset content not found")
			return
		}
		httpx.RespondInternalErr(w, err)
		return
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		httpx.RespondInternalErr(w, err)
		return
	}

	w.Header().Set("Content-Type", asset.ContentType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	if contentDisposition := mime.FormatMediaType("inline", map[string]string{"filename": asset.Filename}); contentDisposition != "" {
		w.Header().Set("Content-Disposition", contentDisposition)
	}
	http.ServeContent(w, r, asset.Filename, info.ModTime(), f)
}

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	if !authorizeLocalRequest(w, r) {
		return
	}

	id := strings.TrimSpace(r.PathValue("id"))
	asset, err := h.store.GetTerminalBackgroundAsset(id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			httpx.RespondNotFound(w, "asset not found")
			return
		}
		httpx.RespondInternalErr(w, err)
		return
	}

	if err := os.Remove(assetPath(asset)); err != nil && !errors.Is(err, os.ErrNotExist) {
		httpx.RespondInternalErr(w, err)
		return
	}
	deleted, err := h.store.DeleteTerminalBackgroundAsset(id)
	if err != nil {
		httpx.RespondInternalErr(w, err)
		return
	}
	if !deleted {
		httpx.RespondNotFound(w, "asset not found")
		return
	}
	httpx.RespondNoContent(w)
}

func (h *Handler) storeUploadedAsset(w http.ResponseWriter, r *http.Request) (state.TerminalBackgroundAsset, error) {
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes+(8<<20))

	reader, err := r.MultipartReader()
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			return state.TerminalBackgroundAsset{}, &requestError{status: http.StatusRequestEntityTooLarge, message: "upload too large"}
		}
		return state.TerminalBackgroundAsset{}, &requestError{status: http.StatusBadRequest, message: "expected multipart/form-data upload"}
	}

	var asset state.TerminalBackgroundAsset
	var seenFile bool
	cleanup := func() {
		if asset.ID == "" {
			return
		}
		_ = os.Remove(assetPath(asset))
		_, _ = h.store.DeleteTerminalBackgroundAsset(asset.ID)
		asset = state.TerminalBackgroundAsset{}
	}

	for {
		part, err := reader.NextPart()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			var maxBytesErr *http.MaxBytesError
			if errors.As(err, &maxBytesErr) {
				cleanup()
				return state.TerminalBackgroundAsset{}, &requestError{status: http.StatusRequestEntityTooLarge, message: "upload too large"}
			}
			cleanup()
			return state.TerminalBackgroundAsset{}, &requestError{status: http.StatusBadRequest, message: "invalid multipart upload"}
		}

		if part.FormName() != "file" {
			part.Close()
			continue
		}
		if seenFile {
			part.Close()
			cleanup()
			return state.TerminalBackgroundAsset{}, &requestError{status: http.StatusBadRequest, message: "only one file may be uploaded at a time"}
		}
		seenFile = true
		asset, err = h.saveUploadedPart(part)
		part.Close()
		if err != nil {
			return state.TerminalBackgroundAsset{}, err
		}
	}

	if !seenFile {
		return state.TerminalBackgroundAsset{}, errMissingFile
	}
	return asset, nil
}

func (h *Handler) saveUploadedPart(part *multipart.Part) (asset state.TerminalBackgroundAsset, err error) {
	filename, uploadedExt, err := validateFilename(part.FileName())
	if err != nil {
		return state.TerminalBackgroundAsset{}, err
	}
	if err := os.MkdirAll(DataDir(), 0o755); err != nil {
		return state.TerminalBackgroundAsset{}, err
	}

	id := uuid.NewString()
	tmpPath := filepath.Join(DataDir(), id+".upload")
	dst, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o644)
	if err != nil {
		return state.TerminalBackgroundAsset{}, err
	}
	defer func() {
		_ = dst.Close()
		if err != nil {
			_ = os.Remove(tmpPath)
		}
	}()

	header := make([]byte, 0, 512)
	buf := make([]byte, 32*1024)
	var size int64

	for {
		n, readErr := part.Read(buf)
		if n > 0 {
			size += int64(n)
			if size > maxUploadBytes {
				return state.TerminalBackgroundAsset{}, &requestError{status: http.StatusRequestEntityTooLarge, message: "upload too large"}
			}
			if len(header) < 512 {
				take := 512 - len(header)
				if take > n {
					take = n
				}
				header = append(header, buf[:take]...)
			}
			if _, writeErr := dst.Write(buf[:n]); writeErr != nil {
				return state.TerminalBackgroundAsset{}, writeErr
			}
		}
		if readErr == nil {
			continue
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		var maxBytesErr *http.MaxBytesError
		if errors.As(readErr, &maxBytesErr) {
			return state.TerminalBackgroundAsset{}, &requestError{status: http.StatusRequestEntityTooLarge, message: "upload too large"}
		}
		return state.TerminalBackgroundAsset{}, readErr
	}

	if size == 0 {
		return state.TerminalBackgroundAsset{}, &requestError{status: http.StatusBadRequest, message: "uploaded file is empty"}
	}

	cfg, err := detectMedia(header)
	if err != nil {
		return state.TerminalBackgroundAsset{}, err
	}
	if !cfg.AllowedExts[uploadedExt] {
		return state.TerminalBackgroundAsset{}, &requestError{status: http.StatusBadRequest, message: "file extension does not match uploaded media type"}
	}
	if size > cfg.MaxBytes {
		return state.TerminalBackgroundAsset{}, &requestError{status: http.StatusRequestEntityTooLarge, message: sizeLimitMessage(cfg)}
	}

	asset = state.TerminalBackgroundAsset{
		ID:          id,
		Filename:    filename,
		MediaKind:   cfg.Kind,
		ContentType: cfg.ContentType,
		FileExt:     cfg.StoredExt,
		SizeBytes:   size,
		CreatedAt:   time.Now().UTC().Format(time.RFC3339),
	}

	finalPath := assetPath(asset)
	if err := os.Rename(tmpPath, finalPath); err != nil {
		return state.TerminalBackgroundAsset{}, err
	}
	if err := h.store.PutTerminalBackgroundAsset(asset); err != nil {
		_ = os.Remove(finalPath)
		return state.TerminalBackgroundAsset{}, err
	}
	return asset, nil
}

func assetPath(asset state.TerminalBackgroundAsset) string {
	return filepath.Join(DataDir(), asset.ID+asset.FileExt)
}

func toAPIAssets(assets []state.TerminalBackgroundAsset) []Asset {
	out := make([]Asset, 0, len(assets))
	for _, asset := range assets {
		out = append(out, toAPIAsset(asset))
	}
	return out
}

func toAPIAsset(asset state.TerminalBackgroundAsset) Asset {
	return Asset{
		ID:          asset.ID,
		Filename:    asset.Filename,
		Kind:        asset.MediaKind,
		ContentType: asset.ContentType,
		SizeBytes:   asset.SizeBytes,
		CreatedAt:   asset.CreatedAt,
		ContentURL:  "/api" + routeBase + "/" + asset.ID + "/content",
	}
}

func authorizeLocalRequest(w http.ResponseWriter, r *http.Request) bool {
	if err := validateLocalRequest(r); err != nil {
		respondRequestError(w, err)
		return false
	}
	return true
}

func validateLocalRequest(r *http.Request) error {
	if !isLoopbackRequest(r.RemoteAddr) {
		return errLocalOnly
	}
	if !isLocalHostHeader(r.Host) {
		return errLocalOnly
	}
	if err := validateSameOrigin(r); err != nil {
		return err
	}
	return nil
}

func validateSameOrigin(r *http.Request) error {
	if value := strings.TrimSpace(r.Header.Get("Origin")); value != "" {
		if !matchesRequestHost(value, r.Host) {
			return &requestError{status: http.StatusForbidden, message: "origin is not allowed"}
		}
		return nil
	}

	if ref := strings.TrimSpace(r.Referer()); ref != "" {
		if !matchesRequestHost(ref, r.Host) {
			return &requestError{status: http.StatusForbidden, message: "referer is not allowed"}
		}
		return nil
	}

	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		return nil
	}
	return &requestError{status: http.StatusForbidden, message: "same-origin browser request required"}
}

func matchesRequestHost(rawURL, requestHost string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	if u.Host == "" {
		return false
	}
	return strings.EqualFold(normalizeHostPort(u.Host), normalizeHostPort(requestHost))
}

func normalizeHostPort(hostport string) string {
	hostport = strings.TrimSpace(strings.ToLower(hostport))
	if hostport == "" {
		return ""
	}
	host, port, err := net.SplitHostPort(hostport)
	if err == nil {
		return net.JoinHostPort(host, port)
	}
	if strings.Count(hostport, ":") > 1 && !strings.HasPrefix(hostport, "[") {
		return hostport
	}
	return hostport
}

func isLoopbackRequest(remoteAddr string) bool {
	if remoteAddr == "" {
		return false
	}
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func isLocalHostHeader(hostport string) bool {
	hostport = strings.TrimSpace(hostport)
	if hostport == "" {
		return false
	}
	host, _, err := net.SplitHostPort(hostport)
	if err != nil {
		host = hostport
	}
	host = strings.Trim(host, "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func validateFilename(raw string) (string, string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", &requestError{status: http.StatusBadRequest, message: "uploaded file must have a filename"}
	}
	if filepath.Base(raw) != raw || strings.Contains(raw, "\\") {
		return "", "", &requestError{status: http.StatusBadRequest, message: "filename must not include directories"}
	}
	if !utf8.ValidString(raw) {
		return "", "", &requestError{status: http.StatusBadRequest, message: "filename must be valid UTF-8"}
	}
	if len(raw) > 128 {
		return "", "", &requestError{status: http.StatusBadRequest, message: "filename is too long"}
	}
	for _, r := range raw {
		if r == utf8.RuneError || unicode.IsControl(r) || r == '/' || r == '\\' || r == 0 {
			return "", "", &requestError{status: http.StatusBadRequest, message: "filename contains invalid characters"}
		}
		if !(unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune(" ._()-", r)) {
			return "", "", &requestError{status: http.StatusBadRequest, message: "filename contains unsupported characters"}
		}
	}

	ext := strings.ToLower(filepath.Ext(raw))
	base := strings.TrimSpace(strings.TrimSuffix(raw, filepath.Ext(raw)))
	if base == "" || ext == "" {
		return "", "", &requestError{status: http.StatusBadRequest, message: "filename must include a supported extension"}
	}
	return raw, ext, nil
}

func detectMedia(header []byte) (mediaConfig, error) {
	switch {
	case len(header) >= 8 && bytes.Equal(header[:8], []byte("\x89PNG\r\n\x1a\n")):
		return pngConfig, nil
	case len(header) >= 3 && header[0] == 0xff && header[1] == 0xd8 && header[2] == 0xff:
		return jpegConfig, nil
	case len(header) >= 6 && (string(header[:6]) == "GIF87a" || string(header[:6]) == "GIF89a"):
		return gifConfig, nil
	case len(header) >= 12 && string(header[:4]) == "RIFF" && string(header[8:12]) == "WEBP":
		return webpConfig, nil
	case len(header) >= 12 && string(header[4:8]) == "ftyp":
		return mp4Config, nil
	case len(header) >= 4 && bytes.Equal(header[:4], []byte{0x1a, 0x45, 0xdf, 0xa3}):
		return webmConfig, nil
	default:
		return mediaConfig{}, &requestError{status: http.StatusBadRequest, message: "unsupported media type; allowed formats are PNG, JPEG, GIF, WebP, MP4, and WebM"}
	}
}

func sizeLimitMessage(cfg mediaConfig) string {
	if cfg.Kind == "video" {
		return "video files must be 150 MiB or smaller"
	}
	return "image files must be 32 MiB or smaller"
}

func respondRequestError(w http.ResponseWriter, err error) {
	var reqErr *requestError
	if errors.As(err, &reqErr) {
		code := "bad_request"
		if reqErr.status == http.StatusForbidden {
			code = "forbidden"
		}
		httpx.WriteError(w, reqErr.status, code, reqErr.message)
		return
	}
	var maxBytesErr *http.MaxBytesError
	if errors.As(err, &maxBytesErr) {
		httpx.WriteError(w, http.StatusRequestEntityTooLarge, "bad_request", "upload too large")
		return
	}
	httpx.RespondInternalErr(w, err)
}
