package desktop

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Session represents one running xpra server and the desktop app it
// forwards. It is the desktop equivalent of terminal.Session: a leaf id
// (shared with the layout tree), the xpra display number, the local TCP
// port the xpra WS server is bound to, and the start-child command.
type Session struct {
	ID      string
	Cwd     string
	Cmd     []string
	Display int
	Port    int
	// Spec is the xpra display/session specifier passed on the command line:
	// ":<display>" on Unix, "windows=<hwnd>" on Windows. Used to stop the
	// server and to identify the session in logs.
	Spec string
	// xpraCmd is the running xpra process. We keep it so Delete can stop
	// the server (which in turn kills the start-child via
	// --exit-with-children) and so ReconcileOrphans can detect orphans.
	xpraCmd *exec.Cmd
	// appCmd is the graphical application process. On Unix this is nil
	// (xpra's --start-child owns the app); on Windows Caw launches the app
	// itself and shadows its top-level window, so we own the process and
	// must kill it on Delete.
	appCmd *exec.Cmd
	// killed is set by Delete so OnSessionExit consumers can distinguish an
	// explicit kill from a process that died on its own.
	killed bool
}

// SessionManager owns the live desktop sessions, keyed by leaf id. It
// mirrors terminal.SessionManager: Create spawns an xpra server per leaf,
// Get/ReconcileOrphans handle lookup and cleanup. Sessions are removed
// when xpra exits (watched by a goroutine) or when Delete is called.
type SessionManager struct {
	mu       sync.RWMutex
	sessions map[string]*Session

	// reconcileMu/Timer coalesce rapid layout-state saves (drag/split/merge)
	// so a leaf that is momentarily absent isn't killed before the user
	// finishes the edit. Mirrors terminal.SessionManager.
	reconcileMu    sync.Mutex
	reconcileTimer *time.Timer
}

// reconcileDebounce is the grace period during which a layout-state save is
// held before orphan reconciliation runs.
const reconcileDebounce = 2 * time.Second

func NewSessionManager() *SessionManager {
	return &SessionManager{sessions: make(map[string]*Session)}
}

// Create starts an xpra server forwarding the given graphical command and
// returns the leaf id. The xpra WS server binds to 127.0.0.1 on a
// kernel-assigned free port; Caw reverse-proxies the browser to it. The
// start-child inherits the session's cwd and env.
func (m *SessionManager) Create(req CreateRequest) (string, error) {
	if !xpraAvailable() {
		return "", fmt.Errorf("xpra is not installed on this device; install it from https://xpra.org/ to use desktop apps")
	}
	cwd := req.Cwd
	if cwd == "" {
		cwd, _ = os.Getwd()
	}

	id := req.ID
	if id == "" {
		id = uuid.New().String()
	}

	// Idempotent: a session for this leaf already exists.
	m.mu.RLock()
	if existing, ok := m.sessions[id]; ok {
		m.mu.RUnlock()
		return existing.ID, nil
	}
	m.mu.RUnlock()

	if len(req.Cmd) == 0 {
		return "", fmt.Errorf("desktop session requires a start-child command")
	}

	port, err := freeTCPPort()
	if err != nil {
		return "", err
	}

	// Build the child env. xpra forwards these to the child via
	// --start-child; the env is passed as KEY=VALUE trailing args to the
	// start-child command itself, but xpra's --env flag applies to the
	// server. The simplest robust approach: set the env on the xpra process
	// and let --start-child inherit it.
	env := os.Environ()
	for _, kv := range req.Env {
		if len(kv) != 2 || kv[0] == "" {
			continue
		}
		env = append(env, kv[0]+"="+kv[1])
	}

	// Platform-specific launch. On Unix this runs `xpra start :<display>
	// --start-child=<cmd>` (seamless mode with a virtual X display). On
	// Windows seamless mode is unavailable (the Windows xpra build lacks the
	// X11 bindings), so the Windows variant instead launches the app
	// itself and runs `xpra shadow windows=<hwnd>` to forward just that
	// window. Both return a Session whose WS port we then wait on.
	sess, err := launchXpra(id, cwd, req.Cmd, env, port)
	if err != nil {
		return "", err
	}

	// Wait for xpra's WS port to accept connections before returning so the
	// frontend's iframe doesn't hit a closed port on first paint. Cap at
	// 20s; Windows shadow servers can take ~10s to initialise, while the
	// Unix case binds within a second or two.
	if err := waitForPort(port, 20*time.Second); err != nil {
		// xpra failed to bind; tear it down and report.
		_ = stopSession(sess)
		releaseSession(sess)
		return "", fmt.Errorf("xpra did not start: %w", err)
	}

	m.mu.Lock()
	m.sessions[id] = sess
	m.mu.Unlock()

	if OnSessionStart != nil {
		OnSessionStart(id, req.Cmd, cwd)
	}

	// Watch the xpra process; when it exits (app closed, crash, or kill),
	// remove the session and fire OnSessionExit. This is the desktop
	// equivalent of terminal.Session.ReadLoop's exit handling.
	go m.watchExit(sess)

	return id, nil
}

// watchExit blocks until the xpra process exits, then removes the session
// and fires OnSessionExit. Killed sessions (Delete was called) are
// already removed from the map by Delete, so this is a no-op for them.
func (m *SessionManager) watchExit(sess *Session) {
	cmd := sess.xpraCmd
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = cmd.Wait()

	m.mu.Lock()
	_, stillThere := m.sessions[sess.ID]
	if stillThere {
		delete(m.sessions, sess.ID)
	}
	m.mu.Unlock()

	releaseDisplay(sess.Display)

	if stillThere && OnSessionExit != nil {
		OnSessionExit(sess.ID, sess.killed)
	}
}

// Get returns the session for the given leaf id.
func (m *SessionManager) Get(id string) (*Session, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.sessions[id]
	return s, ok
}

// Delete stops the xpra server for the given leaf id, which in turn kills
// the start-child (--exit-with-children). Returns false if no session
// exists. The session is removed from the map immediately so a concurrent
// Create for the same leaf can proceed; the watchExit goroutine will
// observe the process exit and fire OnSessionExit with killed=true.
func (m *SessionManager) Delete(id string) bool {
	m.mu.Lock()
	sess, ok := m.sessions[id]
	if ok {
		sess.killed = true
		delete(m.sessions, id)
	}
	m.mu.Unlock()
	if !ok {
		return false
	}
	_ = stopSession(sess)
	releaseSession(sess)
	if OnSessionExit != nil {
		OnSessionExit(id, true)
	}
	return true
}

// scheduleReconcile arms (or resets) the debounce timer for orphan
// reconciliation, mirroring terminal.SessionManager.
func (m *SessionManager) scheduleReconcile(knownLeafIDs map[string]bool) {
	m.reconcileMu.Lock()
	defer m.reconcileMu.Unlock()
	if m.reconcileTimer != nil {
		m.reconcileTimer.Stop()
	}
	known := knownLeafIDs
	m.reconcileTimer = time.AfterFunc(reconcileDebounce, func() {
		m.doReconcileOrphans(known)
	})
}

// doReconcileOrphans kills any desktop session whose leaf id is no longer
// present in any workspace's layout. Unlike terminal sessions, desktop
// sessions have no "connected viewers" concept (the iframe is a plain HTTP
// client), so orphan == not-in-layout. Mirrors terminal's reconciler.
func (m *SessionManager) doReconcileOrphans(knownLeafIDs map[string]bool) {
	m.mu.RLock()
	var victims []string
	for id := range m.sessions {
		if !knownLeafIDs[id] {
			victims = append(victims, id)
		}
	}
	m.mu.RUnlock()
	for _, id := range victims {
		m.Delete(id)
	}
}

// ReconcileOrphans schedules a debounced reconciliation pass.
func (m *SessionManager) ReconcileOrphans(knownLeafIDs map[string]bool) {
	m.scheduleReconcile(knownLeafIDs)
}

// stopSession shuts down the xpra server for the session. It is the desktop
// equivalent of terminal.Pty.Kill. The mechanism is platform-specific: on
// Unix `xpra stop :<display>` cleanly stops the server (and
// --exit-with-children cleans up the Xvfb); on Windows, where `xpra stop`
// does not reliably resolve per-window shadow sessions, we kill the process
// tree of the xpra process we spawned (and the app process Caw launched).
func stopSession(sess *Session) error {
	return stopSessionImpl(sess)
}

// releaseSession releases any per-session resources (the display number on
// Unix; nothing on Windows).
func releaseSession(sess *Session) {
	releaseSessionImpl(sess)
}

// waitForPort polls the given TCP port until it accepts a connection or the
// timeout expires. Used by Create to block until xpra's WS server is ready.
func waitForPort(port int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 500*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("timeout waiting for port %d", port)
}

// joinShellSafe joins a command argv into a single string suitable for
// xpra's --start-child (which runs it through the shell). Simple
// space-join with shell quoting of args containing spaces or shell
// metacharacters.
func joinShellSafe(cmd []string) string {
	parts := make([]string, len(cmd))
	for i, a := range cmd {
		parts[i] = shellQuote(a)
	}
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += " "
		}
		out += p
	}
	return out
}

// shellQuote wraps a string in single quotes, escaping any embedded single
// quotes, so it can be safely embedded in a shell command line.
func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	// If it's a simple token with no shell metacharacters, leave it.
	if isShellSafe(s) {
		return s
	}
	// Otherwise single-quote it, escaping embedded single quotes.
	return "'" + stringsReplaceAll(s, "'", "'\"'\"'") + "'"
}

func isShellSafe(s string) bool {
	for _, r := range s {
		if r >= 'a' && r <= 'z' {
			continue
		}
		if r >= 'A' && r <= 'Z' {
			continue
		}
		if r >= '0' && r <= '9' {
			continue
		}
		switch r {
		case '/', '_', '-', '.', ':', '=', ',', '+', '@', '%':
			continue
		}
		return false
	}
	return true
}

// stringsReplaceAll is a tiny local replacement for strings.ReplaceAll to
// keep the import list in this file minimal.
func stringsReplaceAll(s, old, new string) string {
	out := ""
	for {
		i := indexOf(s, old)
		if i < 0 {
			out += s
			return out
		}
		out += s[:i] + new
		s = s[i+len(old):]
	}
}

func indexOf(s, sub string) int {
	if len(sub) == 0 {
		return 0
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// healthCheck pings the xpra WS server's HTTP endpoint for the session and
// reports whether it's still serving the HTML5 client. Used by the GET
// /api/desktop/{id} route so the frontend can detect a dead session.
func (s *Session) healthCheck() bool {
	url := "http://127.0.0.1:" + strconv.Itoa(s.Port) + "/"
	resp, err := httpClient.Get(url)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// proxyTarget returns the upstream URL for the reverse proxy. Kept on
// Session so the proxy doesn't need to re-resolve.
func (s *Session) proxyTarget() string {
	return "http://127.0.0.1:" + strconv.Itoa(s.Port)
}

// logf is a tiny convenience logger so the package doesn't pull in the
// global log package where it isn't already imported.
var _ = log.Printf