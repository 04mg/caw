package terminal

import (
	"bytes"
	"encoding/json"
	"sync"

	"github.com/gorilla/websocket"
)

type connDim struct {
	cols int
	rows int
}

type Session struct {
	ID           string
	Pty          *Pty
	Cwd          string
	DeleteBranch bool
	mu           sync.Mutex
	conns        map[*websocket.Conn]bool
	connDims     map[*websocket.Conn]connDim
	lastCols     int
	lastRows     int
	scrollback   []byte
	onExit       func()
}

// recomputeSize resizes the PTY to the smallest cols/rows across all
// connected clients so output stays readable for every viewer. A single
// PTY can only have one size; using the min guarantees no client gets
// output wrapped for a larger size than it can show.
// Must be called with s.mu held.
func (s *Session) recomputeSize() {
	if len(s.connDims) == 0 {
		return
	}
	minCols, minRows := -1, -1
	for _, d := range s.connDims {
		if d.cols <= 0 || d.rows <= 0 {
			continue
		}
		if minCols < 0 || d.cols < minCols {
			minCols = d.cols
		}
		if minRows < 0 || d.rows < minRows {
			minRows = d.rows
		}
	}
	if minCols <= 0 || minRows <= 0 {
		return
	}
	if s.lastCols == minCols && s.lastRows == minRows {
		return
	}
	s.lastCols = minCols
	s.lastRows = minRows
	s.Pty.ptmx.Resize(minCols, minRows)
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
