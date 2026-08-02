// Package pets implements upload, listing, serving and deletion of custom
// pet sprites stored locally by Caw. Pets downloaded from Petdex are served
// straight from the Petdex CDN; only user-uploaded sprites live here.
package pets

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

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

// DataDir returns the directory where uploaded pet sprites are stored,
// derived from the state database location (~/.caw by default).
func DataDir(store *state.Store) string {
	base := filepath.Dir(state.DefaultDBPath())
	return filepath.Join(base, "pets")
}

func spritePath(id string) string {
	return filepath.Join(id, "sprite.webp")
}

type Pet struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Kind          string `json:"kind"`
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
		pets = append(pets, Pet{
			ID:             e.Name(),
			Name:           displayName(e.Name()),
			Kind:           "custom",
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

	id := "custom:" + randomID()
	dir := filepath.Join(DataDir(store), id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		httpx.RespondInternalErr(w, err)
		return
	}
	if err := os.WriteFile(filepath.Join(dir, "sprite.webp"), data, 0o644); err != nil {
		httpx.RespondInternalErr(w, err)
		return
	}

	if name == "" {
		name = displayName(id)
	}
	httpx.RespondJSON(w, Pet{
		ID:             id,
		Name:           name,
		Kind:           "custom",
		SpritesheetURL: "/api/pets/" + id + "/sprite.webp",
	})
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
