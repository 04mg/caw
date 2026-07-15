package workspace

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/gorilla/websocket"
)

type FileEvent struct {
	Type  string `json:"type"`
	Path  string `json:"path"`
	IsDir bool   `json:"isDir,omitempty"`
}

type pendingEvent struct {
	rootPath string
	event    FileEvent
	fromFS   bool
}

type debounceFlush struct {
	rootPath string
	event    FileEvent
}

// Subscriber is the minimal interface FileEventHub needs from a
// connected client. Both the legacy connClient wrapper and the
// ws.MuxClient satisfy it, so the hub can broadcast to either.
type Subscriber interface {
	WriteMessage(msgType int, data []byte) error
}

// connClient wraps a gorilla websocket.Conn with a per-connection write
// mutex. gorilla/websocket does not allow concurrent writes to the same
// connection, and the FileEventHub has multiple goroutines that can write
// to the same conn (the run loop for debounced broadcasts, EmitEvent for
// external events, and cleanup). Serializing every WriteMessage call
// through this mutex prevents the "concurrent write to websocket
// connection" panic. This mirrors the ws.Client pattern used by the state
// and agent status hubs.
type connClient struct {
	conn *websocket.Conn
	mu   sync.Mutex
}

func (c *connClient) WriteMessage(msgType int, data []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.conn.WriteMessage(msgType, data)
}

func (c *connClient) Close() error { return c.conn.Close() }

type subKey struct {
	id Subscriber
}

type FileEventHub struct {
	mu         sync.Mutex
	watchers   map[string]*fsnotify.Watcher
	subs       map[string]map[Subscriber]struct{}
	connRoots  map[Subscriber]map[string]struct{}
	extEvents  chan pendingEvent
	done       chan struct{}
	droppedEvents int64

	// onEventListeners holds callbacks invoked (synchronously) with each
	// FileEvent that is broadcast to subscribers. This lets other packages
	// (e.g. git status) react to file changes without coupling to the hub's
	// broadcast internals. Listeners must not block.
	eventMu       sync.RWMutex
	onEventListeners []func(rootPath string, event FileEvent)
}

var defaultHub *FileEventHub
var hubOnce sync.Once

func getHub() *FileEventHub {
	hubOnce.Do(func() {
		defaultHub = &FileEventHub{
			watchers:  make(map[string]*fsnotify.Watcher),
			subs:      make(map[string]map[Subscriber]struct{}),
			connRoots: make(map[Subscriber]map[string]struct{}),
			extEvents: make(chan pendingEvent, 256),
			done:      make(chan struct{}),
		}
		go defaultHub.run()
	})
	return defaultHub
}

// GetHub returns the process-wide FileEventHub, creating it on first use.
// Other packages can use it to register event listeners (see OnEvent)
// without importing the hub's broadcast internals.
func GetHub() *FileEventHub { return getHub() }

func (h *FileEventHub) run() {
	debounceTimers := make(map[string]*time.Timer)
	debounceEvs := make(map[string]*FileEvent)
	flushCh := make(chan debounceFlush, 256)

	for {
		select {
		case pe := <-h.extEvents:
			if pe.fromFS {
				root := pe.rootPath
				if t, ok := debounceTimers[root]; ok {
					t.Stop()
				}
				ev := pe.event
				debounceEvs[root] = &ev
				debounceTimers[root] = time.AfterFunc(150*time.Millisecond, func() {
					flushCh <- debounceFlush{rootPath: root, event: ev}
				})
			} else {
				h.broadcast(pe.rootPath, pe.event)
			}

		case flush := <-flushCh:
			delete(debounceTimers, flush.rootPath)
			delete(debounceEvs, flush.rootPath)
			h.broadcast(flush.rootPath, flush.event)

		case <-h.done:
			return
		}
	}
}

func (h *FileEventHub) Subscribe(sub Subscriber, rootPath string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.subs[rootPath] == nil {
		h.subs[rootPath] = make(map[Subscriber]struct{})
	}
	h.subs[rootPath][sub] = struct{}{}

	if h.connRoots[sub] == nil {
		h.connRoots[sub] = make(map[string]struct{})
	}
	h.connRoots[sub][rootPath] = struct{}{}

	if _, ok := h.watchers[rootPath]; !ok {
		w, err := fsnotify.NewWatcher()
		if err != nil {
			log.Printf("fsnotify NewWatcher for %s: %v", rootPath, err)
			return
		}
		filepath.Walk(rootPath, func(path string, fi os.FileInfo, err error) error {
			if err != nil || !fi.IsDir() {
				return err
			}
			if shouldIgnoreDir(fi.Name()) {
				return filepath.SkipDir
			}
			w.Add(path)
			return nil
		})
		h.watchers[rootPath] = w
		go h.watchPath(rootPath, w)
	}
}

func (h *FileEventHub) Unsubscribe(sub Subscriber, rootPath string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if m := h.subs[rootPath]; m != nil {
		delete(m, sub)
		if len(m) == 0 {
			delete(h.subs, rootPath)
			if w, ok := h.watchers[rootPath]; ok {
				w.Close()
				delete(h.watchers, rootPath)
			}
		}
	}

	if m := h.connRoots[sub]; m != nil {
		delete(m, rootPath)
		if len(m) == 0 {
			delete(h.connRoots, sub)
		}
	}
}

func (h *FileEventHub) UnsubscribeAll(sub Subscriber) {
	h.mu.Lock()
	defer h.mu.Unlock()

	for rootPath := range h.connRoots[sub] {
		if m := h.subs[rootPath]; m != nil {
			delete(m, sub)
			if len(m) == 0 {
				delete(h.subs, rootPath)
				if w, ok := h.watchers[rootPath]; ok {
					w.Close()
					delete(h.watchers, rootPath)
				}
			}
		}
	}
	delete(h.connRoots, sub)
}

func (h *FileEventHub) EmitEvent(filePath string, eventType string, isDir bool) {
	h.mu.Lock()
	var targetRoot string
	for rootPath := range h.subs {
		if strings.HasPrefix(filePath, rootPath) {
			targetRoot = rootPath
			break
		}
	}
	h.mu.Unlock()

	if targetRoot != "" {
		h.trySend(pendingEvent{
			rootPath: targetRoot,
			event:    FileEvent{Type: eventType, Path: filePath, IsDir: isDir},
		})
	}
}

// trySend sends pe to the extEvents channel without blocking. If the
// channel buffer is full (e.g. during an npm install or git checkout
// generating thousands of FS events in quick succession), the event is
// dropped and droppedEvents is incremented. This prevents the fsnotify
// watcher goroutine or the HTTP handler from blocking on a full channel,
// which would stall all file-tree notifications for that root until the
// consumer drains the buffer. The debounce timer in the run loop already
// coalesces bursts, so dropping intermediate events is safe — the final
// flush carries the last-seen event for each root.
func (h *FileEventHub) trySend(pe pendingEvent) {
	select {
	case h.extEvents <- pe:
	default:
		h.droppedEvents++
		if h.droppedEvents%1000 == 1 {
			log.Printf("file-event-hub: dropped %d events (buffer full)", h.droppedEvents)
		}
	}
}

func (h *FileEventHub) watchPath(rootPath string, w *fsnotify.Watcher) {
	defer func() {
		h.mu.Lock()
		if h.watchers[rootPath] == w {
			w.Close()
			delete(h.watchers, rootPath)
		}
		h.mu.Unlock()
	}()

	for {
		select {
		case event, ok := <-w.Events:
			if !ok {
				return
			}
			h.handleFSEvent(rootPath, event, w)
		case err, ok := <-w.Errors:
			if !ok {
				return
			}
			log.Printf("fsnotify error for %s: %v", rootPath, err)
		case <-h.done:
			return
		}
	}
}

func shouldIgnoreDir(name string) bool {
	if strings.HasPrefix(name, ".") {
		return true
	}
	if name == "node_modules" {
		return true
	}
	return false
}

func shouldIgnoreEvent(path string) bool {
	name := filepath.Base(path)

	if strings.HasPrefix(name, ".") {
		return true
	}

	if strings.HasSuffix(name, "~") {
		return true
	}

	ext := filepath.Ext(name)
	if ext == ".db" || strings.HasSuffix(name, ".db-wal") || strings.HasSuffix(name, ".db-shm") || strings.HasSuffix(name, ".db-journal") {
		return true
	}

	if strings.HasSuffix(name, ".swp") || strings.HasSuffix(name, ".swx") || strings.HasPrefix(name, ".#") {
		return true
	}

	return false
}

func (h *FileEventHub) handleFSEvent(rootPath string, event fsnotify.Event, w *fsnotify.Watcher) {
	path := event.Name

	if shouldIgnoreEvent(path) {
		return
	}

	if event.Has(fsnotify.Create) {
		if fi, err := os.Stat(path); err == nil && fi.IsDir() {
			if shouldIgnoreDir(fi.Name()) {
				return
			}
			w.Add(path)
			filepath.Walk(path, func(subPath string, subFi os.FileInfo, err error) error {
				if err != nil || !subFi.IsDir() || subPath == path {
					return err
				}
				if shouldIgnoreDir(subFi.Name()) {
					return filepath.SkipDir
				}
				w.Add(subPath)
				return nil
			})
		}
	}

	ev := FileEvent{}
	switch {
	case event.Has(fsnotify.Create):
		ev.Type = "file-created"
	case event.Has(fsnotify.Write):
		ev.Type = "file-modified"
	case event.Has(fsnotify.Remove) || event.Has(fsnotify.Rename):
		ev.Type = "file-deleted"
	default:
		return
	}
	ev.Path = path

	if event.Has(fsnotify.Write) {
		ev.IsDir = false
	} else if fi, err := os.Stat(path); err == nil {
		ev.IsDir = fi.IsDir()
	}

	h.trySend(pendingEvent{rootPath: rootPath, event: ev, fromFS: true})
}

// OnEvent registers a callback invoked whenever a FileEvent is broadcast
// to subscribers. The returned function removes the listener. Callers must
// not block in the callback; the hub dispatches it under a read lock.
func (h *FileEventHub) OnEvent(fn func(rootPath string, event FileEvent)) func() {
	h.eventMu.Lock()
	defer h.eventMu.Unlock()
	h.onEventListeners = append(h.onEventListeners, fn)
	idx := len(h.onEventListeners) - 1
	return func() {
		h.eventMu.Lock()
		defer h.eventMu.Unlock()
		if idx < len(h.onEventListeners) {
			h.onEventListeners[idx] = nil
		}
	}
}

func (h *FileEventHub) broadcast(rootPath string, event FileEvent) {
	msg, err := json.Marshal(event)
	if err != nil {
		return
	}

	h.mu.Lock()
	subs := make([]Subscriber, 0, len(h.subs[rootPath]))
	for sub := range h.subs[rootPath] {
		subs = append(subs, sub)
	}
	h.mu.Unlock()

	for _, sub := range subs {
		sub.WriteMessage(websocket.TextMessage, msg)
	}

	// Notify out-of-band listeners (e.g. git status recompute) without
	// holding the subscriber lock. Listeners must be non-blocking.
	h.eventMu.RLock()
	for _, fn := range h.onEventListeners {
		if fn != nil {
			fn(rootPath, event)
		}
	}
	h.eventMu.RUnlock()
}
