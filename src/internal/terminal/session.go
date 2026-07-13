package terminal

import (
	"bytes"
	"encoding/json"
	"regexp"
	"sort"
	"strconv"
	"sync"

	"github.com/gorilla/websocket"
)

// syncModes are the DEC private modes whose current state must be re-applied
// to a freshly-attached xterm.js client. When a TUI (e.g. OpenCode) enables
// mouse tracking or bracketed paste, xterm.js stops handling clicks/wheel
// itself and forwards them to the app. A client that attaches after the
// mode-setting sequences were emitted (or whose scrollback replay had a
// sequence split by the 256KiB ring trim) ends up with the wrong mode state,
// so scrolling and clicking break intermittently.
//
// Alt-screen (47/1047/1048/1049) is handled separately by
// stripAlternateScreen and intentionally NOT re-applied, so scrollback replay
// renders into the normal buffer.
var syncModes = map[int]bool{
	1000: true, // X11 mouse tracking
	1002: true, // button-event mouse
	1003: true, // any-event mouse
	1004: true, // focus reporting
	1005: true, // UTF-8 mouse
	1006: true, // SGR mouse
	1015: true, // urxvt mouse
	1016: true, // pixel mouse
	2004: true, // bracketed paste
}

// connWriter wraps a gorilla websocket.Conn with a per-connection write
// mutex. gorilla/websocket does not allow concurrent writes to the same
// connection, and the terminal Session has multiple goroutines writing to
// the same conn (ReadLoop for PTY output, broadcastResize for resizes,
// HandleTerminalWS for scrollback replay). Serializing every WriteMessage
// call through this mutex prevents the "concurrent write to websocket
// connection" panic. This mirrors the existing ws.Client pattern in
// internal/ws/hub.go.
type connWriter struct {
	conn *websocket.Conn
	mu   sync.Mutex
}

func (w *connWriter) WriteMessage(msgType int, data []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.conn.WriteMessage(msgType, data)
}

func (w *connWriter) Close() error {
	return w.conn.Close()
}

// modeRe matches a single DEC private mode set/reset sequence, e.g.
// "\x1b[?1003h" (set) or "\x1b[?1000l" (reset). Multi-parameter forms such as
// "\x1b[?1000;1002h" are intentionally not matched; TUI apps (bubbletea,
// etc.) emit each mode as its own sequence.
var modeRe = regexp.MustCompile("\x1b\\[\\?(\\d+)([hl])")

type Session struct {
	ID           string
	Pty          *Pty
	Cwd          string
	DeleteBranch bool
	mu           sync.Mutex
	conns        map[*connWriter]bool
	cols         int
	rows         int
	scrollback   []byte
	// modes holds the last-set state of the DEC private modes listed in
	// syncModes, tracked incrementally from the live PTY stream. It is the
	// authoritative source used to re-sync a freshly-attached client.
	modes map[int]bool
	onExit       func()
}

// resizePTY resizes the single PTY to cols/rows and notifies every
// connected client of the new dimensions so each client can fit its
// local xterm.js to match. A single PTY has only one size; keeping all
// viewers at that size is what makes the terminal consistent across
// clients. Must be called with s.mu held.
func (s *Session) resizePTY(cols, rows int) {
	if cols <= 0 || rows <= 0 {
		return
	}
	if s.cols == cols && s.rows == rows && len(s.conns) > 0 {
		return
	}
	s.cols = cols
	s.rows = rows
	_ = s.Pty.ptmx.Resize(cols, rows)
	s.broadcastResize()
}

// broadcastResize sends the current PTY size to every connected client
// so each one can fit its local xterm.js to match. Must be called with s.mu held.
func (s *Session) broadcastResize() {
	if s.cols <= 0 || s.rows <= 0 {
		return
	}
	msg, _ := json.Marshal(map[string]any{
		"type": "resize",
		"cols": s.cols,
		"rows": s.rows,
	})
	for c := range s.conns {
		if err := c.WriteMessage(websocket.TextMessage, msg); err != nil {
			delete(s.conns, c)
		}
	}
}

// stripAlternateScreen removes the alternate screen toggle sequences
// (\x1b[?1049h and \x1b[?1049l) from the buffer, keeping only normal
// screen output so scrollback replay works correctly in xterm.js.
func stripAlternateScreen(data []byte) []byte {
	if !bytes.Contains(data, []byte("\x1b[?1049")) {
		return data
	}

	// Remove \x1b[?1049h (enter alt screen)
	data = bytes.ReplaceAll(data, []byte("\x1b[?1049h"), nil)
	// Remove \x1b[?1049l (leave alt screen)
	data = bytes.ReplaceAll(data, []byte("\x1b[?1049l"), nil)
	// Also remove \x1b[?1047h and \x1b[?1047l (alternate screen buffer)
	data = bytes.ReplaceAll(data, []byte("\x1b[?1047h"), nil)
	data = bytes.ReplaceAll(data, []byte("\x1b[?1047l"), nil)
	// Also remove \x1b[?1048h and \x1b[?1048l (save/restore cursor)
	data = bytes.ReplaceAll(data, []byte("\x1b[?1048h"), nil)
	data = bytes.ReplaceAll(data, []byte("\x1b[?1048l"), nil)
	return data
}

// stripSyncModes removes the DEC private mode set/reset sequences for the
// modes tracked in syncModes from a scrollback chunk. They are re-applied
// separately (and atomically) via syncMessage when a client attaches, so
// leaving them in the replayed scrollback would only risk double-application
// or — worse — corruption when the 256KiB ring trim splits a multi-byte
// sequence across two chunks. Returns the filtered bytes.
func stripSyncModes(data []byte) []byte {
	if !bytes.ContainsAny(data, "\x1b[?") {
		return data
	}
	return modeRe.ReplaceAllFunc(data, func(m []byte) []byte {
		sub := modeRe.FindSubmatch(m)
		if len(sub) < 3 {
			return m
		}
		n, err := strconv.Atoi(string(sub[1]))
		if err != nil {
			return m
		}
		if syncModes[n] {
			return nil
		}
		return m
	})
}

// updateModes parses the DEC private mode set/reset sequences in data and
// updates s.modes accordingly. Called for every live chunk read from the PTY
// so that s.modes always reflects what the running TUI last requested. Must
// be called with s.mu held.
func (s *Session) updateModes(data []byte) {
	if s.modes == nil {
		s.modes = make(map[int]bool)
	}
	if !bytes.ContainsAny(data, "\x1b[?") {
		return
	}
	for _, m := range modeRe.FindAllSubmatch(data, -1) {
		if len(m) < 3 {
			continue
		}
		n, err := strconv.Atoi(string(m[1]))
		if err != nil {
			continue
		}
		if !syncModes[n] {
			continue
		}
		s.modes[n] = string(m[2]) == "h"
	}
}

// syncMessage builds a single escape sequence that re-applies every tracked
// DEC private mode currently in the "set" state, e.g.
// "\x1b[?1003;1006;2004h". Sent to a freshly-attached client right after the
// scrollback replay so its xterm.js instance enters the same mouse/paste
// mode the running TUI expects. Must be called with s.mu held.
func (s *Session) syncMessage() string {
	if len(s.modes) == 0 {
		return ""
	}
	var set []int
	for n, on := range s.modes {
		if on {
			set = append(set, n)
		}
	}
	sort.Ints(set)
	if len(set) == 0 {
		return ""
	}
	var b []byte
	b = append(b, '\x1b', '[')
	b = append(b, '?')
	for i, n := range set {
		if i > 0 {
			b = append(b, ';')
		}
		b = strconv.AppendInt(b, int64(n), 10)
	}
	b = append(b, 'h')
	return string(b)
}

func (s *Session) ReadLoop() {
	// Monitor process exit and close the PTY to unblock Read.
	// On Windows, the ConPTY output pipe may not signal EOF when the
	// process exits, so we must explicitly break the Read.
	go func() {
		s.Pty.cmd.Wait()
		s.Pty.Close()
	}()

	buf := make([]byte, 16384)
	for {
		n, err := s.Pty.ptmx.Read(buf)
		if n > 0 {
			data := buf[:n]
			if OnPtyActivity != nil {
				OnPtyActivity(s.ID, n)
			}
			s.mu.Lock()
			// Track mode state from the raw stream before anything else:
			// this runs on every live chunk, so the 256KiB ring trim below
			// can never cause us to lose track of a mode the TUI set earlier.
			s.updateModes(data)
			// Persist scrollback with sync-mode sequences stripped, so a
			// later ring-trim split can never corrupt a multi-byte mode
			// sequence during replay. The current mode state is re-applied
			// separately and atomically via syncMessage on attach.
			stripped := stripSyncModes(data)
			s.scrollback = append(s.scrollback, stripped...)
			if len(s.scrollback) > 256*1024 {
				s.scrollback = append([]byte(nil), s.scrollback[len(s.scrollback)-256*1024:]...)
			}
			for c := range s.conns {
				msg, _ := json.Marshal(map[string]any{
					"type": "output",
					"data": string(data),
				})
				if err := c.WriteMessage(websocket.TextMessage, msg); err != nil {
					// Dead connection — remove it to avoid repeated failures.
					delete(s.conns, c)
				}
			}
			s.mu.Unlock()
		}
		if err != nil {
			break
		}
	}

	s.mu.Lock()
	for c := range s.conns {
		msg, _ := json.Marshal(map[string]any{"type": "exit"})
		c.WriteMessage(websocket.TextMessage, msg)
		c.Close()
	}
	s.mu.Unlock()

	if s.onExit != nil {
		s.onExit()
	}
}
