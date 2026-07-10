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

type FileEventHub struct {
	mu        sync.Mutex
	watchers  map[string]*fsnotify.Watcher
	subs      map[string]map[*websocket.Conn]struct{}
	connRoots map[*websocket.Conn]map[string]struct{}
	extEvents chan pendingEvent
	done      chan struct{}
}

var defaultHub *FileEventHub
var hubOnce sync.Once

func getHub() *FileEventHub {
	hubOnce.Do(func() {
		defaultHub = &FileEventHub{
			watchers:  make(map[string]*fsnotify.Watcher),
			subs:      make(map[string]map[*websocket.Conn]struct{}),
			connRoots: make(map[*websocket.Conn]map[string]struct{}),
			extEvents: make(chan pendingEvent, 256),
			done:      make(chan struct{}),
		}
		go defaultHub.run()
	})
	return defaultHub
}

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

func (h *FileEventHub) Subscribe(conn *websocket.Conn, rootPath string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.subs[rootPath] == nil {
		h.subs[rootPath] = make(map[*websocket.Conn]struct{})
	}
	h.subs[rootPath][conn] = struct{}{}

	if h.connRoots[conn] == nil {
		h.connRoots[conn] = make(map[string]struct{})
	}
	h.connRoots[conn][rootPath] = struct{}{}

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

func (h *FileEventHub) Unsubscribe(conn *websocket.Conn, rootPath string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if m := h.subs[rootPath]; m != nil {
		delete(m, conn)
		if len(m) == 0 {
			delete(h.subs, rootPath)
			if w, ok := h.watchers[rootPath]; ok {
				w.Close()
				delete(h.watchers, rootPath)
			}
		}
	}

	if m := h.connRoots[conn]; m != nil {
		delete(m, rootPath)
		if len(m) == 0 {
			delete(h.connRoots, conn)
		}
	}
}

func (h *FileEventHub) UnsubscribeAll(conn *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()

	for rootPath := range h.connRoots[conn] {
		if m := h.subs[rootPath]; m != nil {
			delete(m, conn)
			if len(m) == 0 {
				delete(h.subs, rootPath)
				if w, ok := h.watchers[rootPath]; ok {
					w.Close()
					delete(h.watchers, rootPath)
				}
			}
		}
	}
	delete(h.connRoots, conn)
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
		h.extEvents <- pendingEvent{
			rootPath: targetRoot,
			event:    FileEvent{Type: eventType, Path: filePath, IsDir: isDir},
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

	h.extEvents <- pendingEvent{rootPath: rootPath, event: ev, fromFS: true}
}

func (h *FileEventHub) broadcast(rootPath string, event FileEvent) {
	msg, err := json.Marshal(event)
	if err != nil {
		return
	}

	h.mu.Lock()
	conns := make([]*websocket.Conn, 0, len(h.subs[rootPath]))
	for conn := range h.subs[rootPath] {
		conns = append(conns, conn)
	}
	h.mu.Unlock()

	for _, conn := range conns {
		conn.WriteMessage(websocket.TextMessage, msg)
	}
}
