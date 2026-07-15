package git

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/04mg/caw/internal/ws"
	"github.com/04mg/caw/internal/workspace"
)

// gitStatusEvent is the payload broadcast on the "git" channel. It mirrors
// the two REST responses (/api/git/statuses and /api/git/ignored) so the
// frontend can apply both in one shot.
type gitStatusEvent struct {
	Type    string              `json:"type"` // "git-status"
	Path    string              `json:"path"` // repo root path the snapshot is for
	Statuses map[string]string   `json:"statuses"`
	Ignored  map[string]bool      `json:"ignored"`
}

// statusHub manages "git" channel subscriptions and re-broadcasts git status
// snapshots whenever the working tree changes. It listens to FileEventHub so
// that any file create/modify/delete (including external git operations that
// touch the index) triggers a debounced recompute + broadcast.
type statusHub struct {
	mux  *ws.Multiplexer
	svc  *Service

	mu       sync.Mutex
	subPaths map[string]int // repoPath -> subscriber count
}

var (
	statusHubOnce sync.Once
	defaultStatusHub *statusHub
)

func getStatusHub() *statusHub {
	statusHubOnce.Do(func() {
		defaultStatusHub = &statusHub{subPaths: make(map[string]int)}
	})
	return defaultStatusHub
}

// RegisterMuxChannel wires the "git" channel into the multiplexer. On
// subscribe the client sends {type:"subscribe", path:"<repo>"} and receives
// the current status snapshot; on unsubscribe the repo path is released.
// A background goroutine listens to FileEventHub events and, per subscribed
// repo, debounces a recompute that broadcasts a fresh snapshot to all
// subscribers of that repo.
func RegisterMuxChannel(mux *ws.Multiplexer, svc *Service) {
	h := getStatusHub()
	h.mux = mux
	h.svc = svc

	mux.HandleChannel("git",
		nil,
		func(c *ws.MuxClient) {
			h.mu.Lock()
			for path := range h.subPaths {
				// nothing per-client to clean; paths tracked globally
				_ = path
			}
			h.mu.Unlock()
		},
		func(c *ws.MuxClient, data []byte) {
			var msg struct {
				Type string `json:"type"`
				Path string `json:"path"`
			}
			if err := json.Unmarshal(data, &msg); err != nil {
				return
			}
			switch msg.Type {
			case "subscribe":
				if msg.Path == "" {
					return
				}
				h.addSub(msg.Path)
				// Send current snapshot immediately.
				h.sendSnapshot(c, msg.Path)
			case "unsubscribe":
				if msg.Path == "" {
					return
				}
				h.removeSub(msg.Path)
			}
		},
	)

	// Listen to file events and trigger debounced recompute + broadcast for
	// any subscribed repo whose tree changed.
	go h.watchFileEvents()
}

func (h *statusHub) addSub(path string) {
	h.mu.Lock()
	h.subPaths[path] = h.subPaths[path] + 1
	count := h.subPaths[path]
	h.mu.Unlock()
	if count == 1 {
		workspace.GetHub() // ensure hub exists
	}
}

func (h *statusHub) removeSub(path string) {
	h.mu.Lock()
	if n, ok := h.subPaths[path]; ok {
		if n <= 1 {
			delete(h.subPaths, path)
		} else {
			h.subPaths[path] = n - 1
		}
	}
	h.mu.Unlock()
}

func (h *statusHub) isSubscribed(path string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.subPaths[path] > 0
}

func (h *statusHub) sendSnapshot(c *ws.MuxClient, repoPath string) {
	statuses, ignored, err := h.svc.StatusAndIgnored(repoPath)
	if err != nil {
		return
	}
	_ = c.Send("git", gitStatusEvent{
		Type:     "git-status",
		Path:     repoPath,
		Statuses: statuses,
		Ignored:  ignored,
	})
}

func (h *statusHub) broadcastSnapshot(repoPath string) {
	if !h.isSubscribed(repoPath) {
		return
	}
	h.svc.InvalidateStatusCache()
	statuses, ignored, err := h.svc.StatusAndIgnored(repoPath)
	if err != nil {
		return
	}
	h.mux.Broadcast("git", gitStatusEvent{
		Type:     "git-status",
		Path:     repoPath,
		Statuses: statuses,
		Ignored:  ignored,
	})
}

// watchFileEvents listens to the FileEventHub and, for each event whose root
// path is currently subscribed on the "git" channel, schedules a debounced
// git status recompute + broadcast.
func (h *statusHub) watchFileEvents() {
	timers := make(map[string]*time.Timer)
	var timersMu sync.Mutex

	schedule := func(repoPath string) {
		timersMu.Lock()
		defer timersMu.Unlock()
		if t, ok := timers[repoPath]; ok {
			t.Stop()
		}
		timers[repoPath] = time.AfterFunc(500*time.Millisecond, func() {
			timersMu.Lock()
			delete(timers, repoPath)
			timersMu.Unlock()
			h.broadcastSnapshot(repoPath)
		})
	}

	_ = workspace.GetHub().OnEvent(func(rootPath string, _ workspace.FileEvent) {
		if h.isSubscribed(rootPath) {
			schedule(rootPath)
		} else {
			// The event root may be a worktree whose repo lives at a different
			// path; check subscribed paths that contain the event root.
			h.mu.Lock()
			paths := make([]string, 0, len(h.subPaths))
			for p := range h.subPaths {
				paths = append(paths, p)
			}
			h.mu.Unlock()
			for _, p := range paths {
				if p == rootPath {
					schedule(p)
				}
			}
		}
	})
}