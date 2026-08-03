// Package pets implements upload, listing, serving and deletion of custom
// pet sprites stored locally by Caw. Pets downloaded from Petdex are served
// straight from the Petdex CDN; only user-uploaded sprites live here.
package pets

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/04mg/caw/internal/httpx"
	"github.com/04mg/caw/internal/prefs"
	"github.com/04mg/caw/internal/state"
)

const (
	// frameW/frameH are the petdex atlas frame dimensions. Sprites are a
	// 8-column grid; the row count is either 9 (the classic atlas) or 11
	// (the extended atlas with extra states). Clean integer upscales are
	// also accepted.
	frameW      = 192
	frameH      = 208
	atlasCols   = 8
	atlasRowsV1 = 9
	atlasRowsV2 = 11
)

// Petdex's API and CDN do not send CORS headers, so the browser cannot read
// the manifest or download sprites directly. Caw proxies both through these
// same-origin endpoints instead.
const (
	manifestURL     = "https://petdex.dev/api/manifest"
	manifestMaxSize = 32 << 20
	spriteMaxSize   = 64 << 20
	manifestTTL     = 5 * time.Minute
)

var (
	petdexClient = &http.Client{Timeout: 30 * time.Second}
	manifestMu   sync.Mutex
	manifestBody []byte
	manifestAt   time.Time
)

// DataDir returns the directory where uploaded pet sprites are stored,
// derived from the state database location (~/.caw by default).
func DataDir(store *state.Store) string {
	base := filepath.Dir(state.DefaultDBPath())
	return filepath.Join(base, "pets")
}

func spritePath(id string) string {
	return filepath.Join(id, "sprite.webp")
}

// petMeta holds the user-visible metadata for a locally stored pet. It is
// persisted next to the sprite so listPets can return the original name (and
// the Petdex source slug for downloaded pets) instead of synthesizing one
// from the id.
type petMeta struct {
	Name   string `json:"name"`
	Source string `json:"source,omitempty"`
}

func readMeta(dir string) petMeta {
	data, err := os.ReadFile(filepath.Join(dir, "meta.json"))
	if err != nil {
		return petMeta{}
	}
	var m petMeta
	_ = json.Unmarshal(data, &m)
	return m
}

func writeMeta(dir string, m petMeta) error {
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "meta.json"), data, 0o644)
}

type Pet struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Kind           string `json:"kind"`
	Source         string `json:"source,omitempty"`
	SpritesheetURL string `json:"spritesheetUrl"`
}

func Register(api *http.ServeMux, store *state.Store) {
	_ = os.MkdirAll(DataDir(store), 0o755)
	api.HandleFunc("GET /pets", func(w http.ResponseWriter, r *http.Request) {
		httpx.RespondJSON(w, listPets(store))
	})
	api.HandleFunc("POST /pets", func(w http.ResponseWriter, r *http.Request) {
		handleUpload(w, r, store)
	})
	api.HandleFunc("GET /pets/{id}/sprite.webp", func(w http.ResponseWriter, r *http.Request) {
		handleSprite(w, r, store)
	})
	api.HandleFunc("DELETE /pets/{id}", func(w http.ResponseWriter, r *http.Request) {
		handleDelete(w, r, store)
	})
	api.HandleFunc("GET /pets/petdex-manifest", handlePetdexManifest)
	api.HandleFunc("GET /pets/proxy-sprite", handlePetdexSpriteProxy)
	api.HandleFunc("POST /pets/from-petdex", func(w http.ResponseWriter, r *http.Request) {
		handlePetdexDownload(w, r, store)
	})
}

func listPets(store *state.Store) []Pet {
	dir := DataDir(store)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return []Pet{}
	}
	pets := []Pet{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(dir, spritePath(e.Name()))); err != nil {
			continue
		}
		meta := readMeta(filepath.Join(dir, e.Name()))
		name := meta.Name
		if name == "" {
			name = displayName(e.Name())
		}
		pets = append(pets, Pet{
			ID:             e.Name(),
			Name:           name,
			Kind:           "custom",
			Source:         meta.Source,
			SpritesheetURL: "/api/pets/" + e.Name() + "/sprite.webp",
		})
	}
	return pets
}

func displayName(id string) string {
	// "custom:<uuid>" -> "Custom <first 8 hex chars>" for a readable label.
	if rest, ok := strings.CutPrefix(id, "custom:"); ok {
		return "Custom " + rest[:min(8, len(rest))]
	}
	return id
}

func handleUpload(w http.ResponseWriter, r *http.Request, store *state.Store) {
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		httpx.RespondBadRequest(w, "invalid multipart form")
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	source := strings.TrimSpace(r.FormValue("source"))
	file, _, err := r.FormFile("file")
	if err != nil {
		httpx.RespondBadRequest(w, "missing file field")
		return
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, 64<<20))
	if err != nil {
		httpx.RespondInternalErr(w, err)
		return
	}
	if len(data) < 32 {
		httpx.RespondBadRequest(w, "file too small to be a valid sprite")
		return
	}
	if err := validateSprite(data); err != nil {
		httpx.RespondBadRequest(w, err.Error())
		return
	}

	pet, err := saveSprite(store, data, name, source)
	if err != nil {
		httpx.RespondInternalErr(w, err)
		return
	}
	httpx.RespondJSON(w, pet)
}

// saveSprite writes a validated sprite plus its metadata as a custom pet and
// returns the created pet record.
func saveSprite(store *state.Store, data []byte, name, source string) (Pet, error) {
	id := "custom:" + randomID()
	dir := filepath.Join(DataDir(store), id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Pet{}, err
	}
	if err := os.WriteFile(filepath.Join(dir, "sprite.webp"), data, 0o644); err != nil {
		return Pet{}, err
	}
	if name == "" {
		name = displayName(id)
	}
	if err := writeMeta(dir, petMeta{Name: name, Source: source}); err != nil {
		return Pet{}, err
	}
	return Pet{
		ID:             id,
		Name:           name,
		Kind:           "custom",
		Source:         source,
		SpritesheetURL: "/api/pets/" + id + "/sprite.webp",
	}, nil
}

// handlePetdexManifest proxies the Petdex manifest through Caw so the browser
// never needs cross-origin access. The body is cached briefly in memory; the
// frontend additionally caches it in localStorage for a day.
func handlePetdexManifest(w http.ResponseWriter, r *http.Request) {
	manifestMu.Lock()
	defer manifestMu.Unlock()
	if manifestBody != nil && time.Since(manifestAt) < manifestTTL {
		httpx.RespondJSON(w, json.RawMessage(manifestBody))
		return
	}
	resp, err := petdexClient.Get(manifestURL)
	if err != nil {
		httpx.RespondInternal(w, "petdex manifest unreachable")
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		httpx.RespondInternal(w, fmt.Sprintf("petdex manifest returned %d", resp.StatusCode))
		return
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, manifestMaxSize))
	if err != nil {
		httpx.RespondInternalErr(w, err)
		return
	}
	if !json.Valid(body) {
		httpx.RespondInternal(w, "petdex manifest is invalid JSON")
		return
	}
	manifestBody = body
	manifestAt = time.Now()
	httpx.RespondJSON(w, json.RawMessage(body))
}

var petdexSlugRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,127}$`)

// handlePetdexSpriteProxy streams a Petdex spritesheet through Caw so the
// browser can render library thumbnails without touching the CDN (which
// sends no CORS headers). Only assets.petdex.dev is allowed, mirroring the
// download endpoint's host check.
func handlePetdexSpriteProxy(w http.ResponseWriter, r *http.Request) {
	u, err := url.Parse(r.URL.Query().Get("url"))
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host != "assets.petdex.dev" {
		httpx.RespondBadRequest(w, "invalid petdex sprite url")
		return
	}
	resp, err := petdexClient.Get(u.String())
	if err != nil {
		httpx.RespondInternal(w, "petdex sprite unreachable")
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		httpx.RespondBadRequest(w, fmt.Sprintf("petdex sprite returned %d", resp.StatusCode))
		return
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, spriteMaxSize))
	if err != nil {
		httpx.RespondInternalErr(w, err)
		return
	}
	if len(data) < 32 {
		httpx.RespondBadRequest(w, "file too small to be a valid sprite")
		return
	}
	if err := validateSprite(data); err != nil {
		httpx.RespondBadRequest(w, err.Error())
		return
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "" {
		w.Header().Set("Cache-Control", cc)
	} else {
		w.Header().Set("Cache-Control", "public, max-age=86400")
	}
	w.Header().Set("Content-Type", contentType(data))
	_, _ = w.Write(data)
}

// handlePetdexDownload downloads a Petdex spritesheet server-side (the CDN
// sends no CORS headers) and stores it as a local custom pet carrying the
// Petdex slug as its source.
func handlePetdexDownload(w http.ResponseWriter, r *http.Request, store *state.Store) {
	var req struct {
		Slug           string `json:"slug" validate:"required,max=128"`
		Name           string `json:"name"`
		SpritesheetURL string `json:"spritesheetUrl" validate:"required,url"`
	}
	if !httpx.BindRequest(w, r, &req) {
		return
	}
	req.Slug = strings.TrimSpace(req.Slug)
	if !petdexSlugRe.MatchString(req.Slug) {
		httpx.RespondBadRequest(w, "invalid petdex slug")
		return
	}
	u, err := url.Parse(req.SpritesheetURL)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host != "assets.petdex.dev" {
		httpx.RespondBadRequest(w, "invalid petdex sprite url")
		return
	}
	resp, err := petdexClient.Get(u.String())
	if err != nil {
		httpx.RespondInternal(w, "petdex sprite unreachable")
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		httpx.RespondBadRequest(w, fmt.Sprintf("petdex sprite returned %d", resp.StatusCode))
		return
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, spriteMaxSize))
	if err != nil {
		httpx.RespondInternalErr(w, err)
		return
	}
	if len(data) < 32 {
		httpx.RespondBadRequest(w, "file too small to be a valid sprite")
		return
	}
	if err := validateSprite(data); err != nil {
		httpx.RespondBadRequest(w, err.Error())
		return
	}
	pet, err := saveSprite(store, data, strings.TrimSpace(req.Name), req.Slug)
	if err != nil {
		httpx.RespondInternalErr(w, err)
		return
	}
	httpx.RespondJSON(w, pet)
}

func handleSprite(w http.ResponseWriter, r *http.Request, store *state.Store) {
	id := r.PathValue("id")
	if id == "" || strings.ContainsAny(id, `/\`) {
		httpx.RespondBadRequest(w, "invalid pet id")
		return
	}
	path := filepath.Join(DataDir(store), spritePath(id))
	data, err := os.ReadFile(path)
	if err != nil {
		httpx.RespondNotFound(w, "pet not found")
		return
	}
	w.Header().Set("Content-Type", contentType(data))
	w.Header().Set("Cache-Control", "public, max-age=3600")
	_, _ = w.Write(data)
}

func handleDelete(w http.ResponseWriter, r *http.Request, store *state.Store) {
	id := r.PathValue("id")
	if id == "" || strings.ContainsAny(id, `/\`) {
		httpx.RespondBadRequest(w, "invalid pet id")
		return
	}
	dir := filepath.Join(DataDir(store), id)
	if err := os.RemoveAll(dir); err != nil {
		httpx.RespondInternalErr(w, err)
		return
	}
	// Drop the slug from the shared roster and agent pins.
	prefs.PrunePets(store, func(slug string) bool { return slug != id })
	httpx.RespondJSON(w, map[string]bool{"ok": true})
}

func randomID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		log.Printf("pets: random id failed: %v", err)
	}
	return hex.EncodeToString(b)
}

func contentType(data []byte) string {
	if len(data) >= 8 && string(data[:8]) == "\x89PNG\r\n\x1a\n" {
		return "image/png"
	}
	return "image/webp"
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// validateSprite checks that the uploaded sprite is a PNG or WebP whose
// dimensions form a valid petdex atlas: 8 columns of 192x208 frames, with
// 9 or 11 rows (or a clean integer upscale of either).
func validateSprite(data []byte) error {
	width, height, err := imageDimensions(data)
	if err != nil {
		return err
	}
	if width < frameW*atlasCols {
		return fmt.Errorf("sprite too small: %dx%d (need at least %dx%d)", width, height, frameW*atlasCols, frameH*9)
	}
	k := width / (frameW * atlasCols)
	if width != frameW*atlasCols*k {
		return fmt.Errorf("invalid sprite width %d (must be a multiple of %d)", width, frameW*atlasCols)
	}
	if height != frameH*atlasRowsV1*k && height != frameH*atlasRowsV2*k {
		return fmt.Errorf("invalid sprite height %d (must be %d or %d, scaled by %d)", height, frameH*atlasRowsV1*k, frameH*atlasRowsV2*k, k)
	}
	return nil
}

// imageDimensions reads the pixel dimensions from the header of a PNG or
// WebP image without decoding the full file.
func imageDimensions(data []byte) (width, height int, err error) {
	if len(data) >= 24 && string(data[:8]) == "\x89PNG\r\n\x1a\n" {
		w := int(binary.BigEndian.Uint32(data[16:20]))
		h := int(binary.BigEndian.Uint32(data[20:24]))
		if w == 0 || h == 0 {
			return 0, 0, errors.New("invalid PNG dimensions")
		}
		return w, h, nil
	}
	if len(data) >= 30 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP" {
		switch string(data[12:16]) {
		case "VP8X":
			if len(data) < 30 {
				return 0, 0, errors.New("truncated VP8X header")
			}
			w := 1 + int(uint(data[24])|uint(data[25])<<8|uint(data[26])<<16)
			h := 1 + int(uint(data[27])|uint(data[28])<<8|uint(data[29])<<16)
			return w, h, nil
		case "VP8 ":
			if len(data) < 28 {
				return 0, 0, errors.New("truncated VP8 header")
			}
			w := 1 + int(uint(data[24])|uint(data[25])<<8&0x3fff)
			h := 1 + int(uint(data[26])|uint(data[27])<<8&0x3fff)
			return w, h, nil
		case "VP8L":
			if len(data) < 25 {
				return 0, 0, errors.New("truncated VP8L header")
			}
			bits := uint32(data[21]) | uint32(data[22])<<8 | uint32(data[23])<<16 | uint32(data[24])<<24
			w := 1 + int(bits&0x3fff)
			h := 1 + int(bits>>14&0x3fff)
			return w, h, nil
		}
	}
	return 0, 0, errors.New("unsupported image format (expected PNG or WebP)")
}
