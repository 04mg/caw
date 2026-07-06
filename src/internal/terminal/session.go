package terminal

import (
	"bytes"
	"encoding/json"
	"sync"

	"github.com/gorilla/websocket"
)

type Session struct {
	ID           string
	Pty          *Pty
	Cwd          string
	DeleteBranch bool
	mu           sync.Mutex
	conns        map[*websocket.Conn]bool
	cols         int
	rows         int
	scrollback   []byte
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

func (s *Session) ReadLoop() {
	// Monitor process exit and close the PTY to unblock Read.
	// On Windows, the ConPTY output pipe may not signal EOF when the
	// process exits, so we must explicitly break the Read.
	go func() {
		s.Pty.cmd.Wait()
		s.Pty.Close()
	}()

	buf := make([]byte, 4096)
	for {
		n, err := s.Pty.ptmx.Read(buf)
		if n > 0 {
			data := buf[:n]
			s.mu.Lock()
			s.scrollback = append(s.scrollback, data...)
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
