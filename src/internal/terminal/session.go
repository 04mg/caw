package terminal

import (
	"encoding/json"
	"sync"

	"github.com/gorilla/websocket"
)

type Session struct {
	ID         string
	Pty        *Pty
	Cwd        string
	mu         sync.Mutex
	conns      map[*websocket.Conn]bool
	scrollback []byte
	onExit     func()
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
				c.WriteMessage(websocket.TextMessage, msg)
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
