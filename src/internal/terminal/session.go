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

// altScreenModes are the DEC private modes that switch the terminal to the
// alternate screen buffer. A TUI sets one of these on startup and clears it
// on exit; the running program is "on the alt screen" while any one is set.
// Used by updateModes to maintain s.altScreen so a reconnecting client can
// be driven into the same buffer the TUI is drawing to.
var altScreenModes = map[int]bool{
	47:   true, // alternate screen buffer
	1047: true, // alternate screen buffer (xterm)
	1049: true, // save cursor + switch to alternate screen
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

	// Per-connection terminal dimensions reported by the viewer's xterm.js.
	// Used to compute the smallest viewer and per-viewer padding.
	cols int
	rows int
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
	// pendingResizes counts WebSocket connections that have opened but
	// haven't yet completed their first resize (and thus aren't in conns).
	// This prevents resizePTY from treating a connection as the only viewer
	// when another connection is still waiting for its first resize message.
	pendingResizes int
	cols           int
	rows           int
	scrollback     []byte
	// modes holds the last-set state of the DEC private modes listed in
	// syncModes, tracked incrementally from the live PTY stream. It is the
	// authoritative source used to re-sync a freshly-attached client.
	modes map[int]bool
	// altScreen tracks whether the running program is currently displaying
	// the alternate screen (DEC 1049 / 47 / 1047 set). Tracked incrementally
	// from the live PTY stream so that a reconnecting client can be driven
	// into the same buffer the TUI is drawing to. Without it, scrollback
	// replay strips the alt-screen toggles and writes the TUI's frame into
	// the normal buffer, so the live incremental updates the TUI sends
	// (which assume the alt screen) land in the wrong buffer — leaving
	// regions such as a sidebar unrendered — and the redraw history that
	// accumulates in the normal buffer surfaces as scroll artifacts.
	altScreen bool
	// killed is set when the user explicitly kills this session via
	// SessionManager.Delete, so the onExit path can tell an explicit kill
	// apart from a crash that happened to be signalled. It is read once by
	// buildOnExit's closure, then cleared.
	killed bool
	// exitInfo carries the process exit result collected in ReadLoop's exit
	// goroutine so it is available to onExit. It is written before s.Pty.Close()
	// unblocks the read loop and read in the onExit closure; both accesses
	// happen on the same ReadLoop goroutine (the exit goroutine writes, then
	// Close unblocks Read which returns err, then onExit is called) so no
	// additional synchronization is needed.
	exitInfo exitInfo
	// exitReady is closed by the exit goroutine once exitInfo has been
	// populated. The main ReadLoop waits on it before calling onExit, so
	// onExit always sees the real exit code even when the kernel's own EOF
	// (from the child dying and closing the slave PTY) unblocks Read before
	// the exit goroutine has finished cmd.Wait(). Without this, an
	// externally-killed process (e.g. pkill -SEGV) would race: Read returns
	// EOF, onExit runs with a zeroed exitInfo, and the crash is missed.
	exitReady chan struct{}
	onExit   func()
}

// exitInfo captures the outcome of the PTY process: its exit code (negative
// when killed by a signal), and the error returned by cmd.Wait() (which is
// typically nil for a clean exit and non-nil for a crash/signal).
type exitInfo struct {
	code int
	err  error
}

// resizePTY handles a viewer resize request. For a single viewer the PTY is
// resized directly to that viewer's dimensions. With multiple viewers the
// PTY is sized to the smallest viewer (min cols×rows across all connected
// viewers) so every viewer can render the full PTY content; viewers larger
// than the PTY receive adapted output via a VirtualScreen and are told to
// pad the grid (padCols/padRows) to center it within their panel.
//
// Each viewer maintains its own dimensions; the PTY is re-driven to the
// current min whenever the set of viewers or their sizes changes.
// Must be called with s.mu held.
func (s *Session) resizePTY(cols, rows int, source *connWriter) {
	if cols <= 0 || rows <= 0 {
		return
	}
	source.cols = cols
	source.rows = rows

	// Single viewer — resize the PTY directly. The frontend already fit
	// its local xterm.js to the new dimensions and sent us the size; we
	// only need to drive the PTY. Echoing a resize back here would be
	// redundant and can race with the user's next resize: a stale echo
	// arriving after a further shrink/grow snaps xterm.js back to the old
	// size and reflows the buffer, corrupting scrollback. So, unlike the
	// multi-viewer path, we do NOT broadcastResize here.
	totalViewers := len(s.conns) + s.pendingResizes
	if totalViewers <= 1 {
		if s.cols == cols && s.rows == rows {
			return
		}
		s.cols = cols
		s.rows = rows
		_ = s.Pty.ptmx.Resize(cols, rows)
		return
	}

	// Multiple viewers — PTY tracks the smallest viewer so the content
	// fits every client. Every viewer renders the SAME PTY-sized grid;
	// larger viewers center it via cell-based padding (padCols/padRows),
	// so no VirtualScreen upscaling is needed — raw PTY output is already
	// at PTY dimensions and renders correctly at that size.
	// Include the source viewer in the min computation: a freshly-connected
	// viewer reports its dimensions here before sendScrollback registers it
	// in s.conns (it is still counted in pendingResizes). Without this, the
	// min would be drawn only from the already-registered (e.g. desktop)
	// viewers and the PTY would be driven to the desktop size instead of
	// the smallest (mobile) viewer — the very bug this path exists to fix.
	minCols, minRows := s.smallestViewerSize(source)
	if minCols <= 0 || minRows <= 0 {
		// No fully-sized viewer yet; keep the PTY as-is.
		return
	}
	if s.cols != minCols || s.rows != minRows {
		s.cols = minCols
		s.rows = minRows
		_ = s.Pty.ptmx.Resize(minCols, minRows)
	}
	s.broadcastResize(source)
}

// smallestViewerSize returns the minimum cols and rows across all
// registered connections that have reported dimensions. Must be called
// with s.mu held.
//
// extra is an optional viewer (e.g. a freshly-connected one that has
// reported its dimensions via resizePTY but hasn't been registered in
// s.conns yet — it is still counted in pendingResizes). Including it
// ensures the PTY is driven to the true smallest viewer on the very first
// resize, rather than waiting until sendScrollback registers the
// connection.
func (s *Session) smallestViewerSize(extra *connWriter) (int, int) {
	minCols, minRows := 0, 0
	consider := func(c *connWriter) {
		if c.cols <= 0 || c.rows <= 0 {
			return
		}
		if minCols == 0 || c.cols < minCols {
			minCols = c.cols
		}
		if minRows == 0 || c.rows < minRows {
			minRows = c.rows
		}
	}
	for c := range s.conns {
		consider(c)
	}
	if extra != nil {
		consider(extra)
	}
	return minCols, minRows
}

// recomputeResize re-evaluates the PTY size after the set of viewers
// changes (e.g. a viewer disconnects). With one viewer left the PTY is
// resized directly to it; with several it is re-driven to the smallest.
// Must be called with s.mu held.
func (s *Session) recomputeResize() {
	totalViewers := len(s.conns) + s.pendingResizes
	if totalViewers <= 0 {
		return
	}
	if totalViewers == 1 {
		for c := range s.conns {
			if c.cols <= 0 || c.rows <= 0 {
				return
			}
			if s.cols == c.cols && s.rows == c.rows {
				// PTY already matches the remaining viewer. Still
				// broadcast so the viewer clears any padding a
				// previous multi-viewer layout applied.
				s.broadcastResize(nil)
				return
			}
			s.cols = c.cols
			s.rows = c.rows
			_ = s.Pty.ptmx.Resize(c.cols, c.rows)
			s.broadcastResize(nil)
			return
		}
		return
	}
	minCols, minRows := s.smallestViewerSize(nil)
	if minCols <= 0 || minRows <= 0 {
		return
	}
	if s.cols != minCols || s.rows != minRows {
		s.cols = minCols
		s.rows = minRows
		_ = s.Pty.ptmx.Resize(minCols, minRows)
	}
	s.broadcastResize(nil)
}

// broadcastResize tells each connected client how big its local xterm.js
// grid should be, plus cell-based padding (padCols/padRows) so the grid is
// centered within a panel that is larger than the PTY. The PTY is sized to
// the smallest viewer, so every viewer renders the SAME PTY-sized grid:
//   - the smallest viewer receives cols/rows == PTY size and zero padding;
//   - larger viewers ALSO receive cols/rows == PTY size, plus positive
//     padding so xterm.js renders the PTY-sized grid centered inside their
//     larger panel. No VirtualScreen upscaling is needed — raw PTY output
//     is already at PTY dimensions and renders correctly at that size.
//
// extra is an optional viewer that has reported dimensions but isn't in
// s.conns yet (e.g. a freshly-connected client whose resizePTY ran before
// sendScrollback registered it). It still needs its resize/padding message
// so its frontend learns the padCols/padRows to center the grid. If extra
// is already in s.conns it is skipped to avoid a duplicate send.
//
// Must be called with s.mu held.
func (s *Session) broadcastResize(extra *connWriter) {
	if s.cols <= 0 || s.rows <= 0 {
		return
	}
	sent := make(map[*connWriter]bool, len(s.conns)+1)
	send := func(c *connWriter) {
		if sent[c] {
			return
		}
		sent[c] = true
		viewCols, viewRows := c.cols, c.rows
		if viewCols <= 0 || viewRows <= 0 {
			viewCols, viewRows = s.cols, s.rows
		}
		padCols := viewCols - s.cols
		padRows := viewRows - s.rows
		if padCols < 0 {
			padCols = 0
		}
		if padRows < 0 {
			padRows = 0
		}
		msg, _ := json.Marshal(map[string]any{
			"type":    "resize",
			"cols":    s.cols,
			"rows":    s.rows,
			"padCols": padCols,
			"padRows": padRows,
		})
		if err := c.WriteMessage(websocket.TextMessage, msg); err != nil {
			delete(s.conns, c)
		}
	}
	for c := range s.conns {
		send(c)
	}
	if extra != nil {
		send(extra)
	}
}

// stripAlternateScreen removes the alternate screen toggle sequences
// (\x1b[?1049h and \x1b[?1049l) from the buffer, keeping only normal
// screen output so scrollback replay works correctly in xterm.js.
func stripAlternateScreen(data []byte) []byte {
	if !bytes.Contains(data, []byte("\x1b[?1049")) &&
		!bytes.Contains(data, []byte("\x1b[?1047")) &&
		!bytes.Contains(data, []byte("\x1b[?47")) &&
		!bytes.Contains(data, []byte("\x1b[?1048")) {
		return data
	}

	// Remove \x1b[?1049h (enter alt screen)
	data = bytes.ReplaceAll(data, []byte("\x1b[?1049h"), nil)
	// Remove \x1b[?1049l (leave alt screen)
	data = bytes.ReplaceAll(data, []byte("\x1b[?1049l"), nil)
	// Also remove \x1b[?1047h and \x1b[?1047l (alternate screen buffer)
	data = bytes.ReplaceAll(data, []byte("\x1b[?1047h"), nil)
	data = bytes.ReplaceAll(data, []byte("\x1b[?1047l"), nil)
	// Also remove \x1b[?47h and \x1b[?47l (alternate screen buffer, original)
	data = bytes.ReplaceAll(data, []byte("\x1b[?47h"), nil)
	data = bytes.ReplaceAll(data, []byte("\x1b[?47l"), nil)
	// Also remove \x1b[?1048h and \x1b[?1048l (save/restore cursor)
	data = bytes.ReplaceAll(data, []byte("\x1b[?1048h"), nil)
	data = bytes.ReplaceAll(data, []byte("\x1b[?1048l"), nil)
	return data
}

// altScreenEnterSeqs are the sequences a program emits to switch to the
// alternate screen. Used by currentAltScreenFrame to locate the start of
// the frame the running TUI is currently drawing.
var altScreenEnterSeqs = [][]byte{
	[]byte("\x1b[?1049h"),
	[]byte("\x1b[?1047h"),
	[]byte("\x1b[?47h"),
}

// currentAltScreenFrame returns the tail of scrollback beginning at the last
// alternate-screen-enter sequence, i.e. the bytes that drew the frame the
// running TUI is currently displaying. Returns nil when the buffer contains
// no enter sequence (e.g. the 256KiB ring trim already evicted it), in which
// case the caller should fall back to replaying the full stripped buffer.
func currentAltScreenFrame(scrollback []byte) []byte {
	last := -1
	for _, seq := range altScreenEnterSeqs {
		if i := bytes.LastIndex(scrollback, seq); i > last {
			last = i
		}
	}
	if last < 0 {
		return nil
	}
	return scrollback[last:]
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
		set := string(m[2]) == "h"
		if altScreenModes[n] {
			// Entering an alt-screen mode puts the program on the alt
			// screen; leaving any of them returns it to the normal
			// buffer. A TUI uses a single mode consistently, so tracking
			// the union is safe in practice.
			s.altScreen = set
		}
		if !syncModes[n] {
			continue
		}
		s.modes[n] = set
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
		waitErr := s.Pty.cmd.Wait()
		// Capture the exit code for the onExit callback. ProcessState is
		// populated by Wait() when the process has exited. A negative exit
		// code means the process was terminated by a signal (a crash); a
		// zero code means a clean exit.
		code := 0
		if ps := s.Pty.cmd.ProcessState; ps != nil {
			code = ps.ExitCode()
		} else if waitErr != nil {
			code = 1
		}
		s.exitInfo = exitInfo{code: code, err: waitErr}
		// Signal that exitInfo is populated BEFORE closing the PTY, so the
		// main ReadLoop (which may have already gotten EOF from the kernel
		// when the child died) can wait on exitReady and read the real exit
		// code rather than a zeroed default.
		close(s.exitReady)
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

	// Wait for the exit goroutine to populate exitInfo before calling
	// onExit. On Linux the kernel closes the slave PTY when the child dies,
	// so Read() returns EOF and we reach here before the exit goroutine has
	// necessarily finished cmd.Wait(). Waiting on exitReady guarantees
	// onExit sees the real exit code (e.g. -1 for a signal) instead of the
	// zero default, which is what distinguishes a crash from a clean exit.
	<-s.exitReady

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
