//go:build windows

package desktop

import (
	"fmt"
	"os/exec"
	"strconv"
	"syscall"
	"time"
	"unsafe"
)

var (
	user32                       = syscall.NewLazyDLL("user32.dll")
	procEnumWindows              = user32.NewProc("EnumWindows")
	procIsWindowVisible          = user32.NewProc("IsWindowVisible")
	procGetWindowThreadProcessId = user32.NewProc("GetWindowThreadProcessId")
)

const (
	// GW_OWNER = 4
	gwOwner = 4
)

// launchXpra starts a per-window xpra shadow server on Windows.
//
// The MS Windows xpra build does not include the X11 bindings, so the
// seamless `xpra start :<display>` mode used on Unix is unavailable
// ("seamless requires a build with the X11 bindings"). The only server
// mode that works on Windows is `shadow`, which mirrors an existing
// desktop. xpra 4.4+ added "name based window shadowing" (issue #3476):
// `xpra shadow windows=<match>` shadows only the top-level windows whose
// titles match a regex (or, when the match value is an integer, the
// window with that HWND).
//
// Caw therefore launches the graphical app itself (rather than relying
// on xpra's --start-child, which cannot target a specific window),
// waits for its first visible top-level (non-owned) window, and then
// runs `xpra shadow windows=<hwnd>` to forward just that window. The
// hwnd is a filename-safe integer, which matters because xpra uses the
// display argument verbatim as the session-directory name (so regex
// metacharacters like * ? | are unusable there). --exit-with-windows=yes
// stops the shadow server when the matched window disappears, mirroring
// the Unix --exit-with-children lifecycle.
func launchXpra(id, cwd string, cmd []string, env []string, port int) (*Session, error) {
	if len(cmd) == 0 {
		return nil, fmt.Errorf("desktop session requires a command")
	}

	exePath, err := exec.LookPath(cmd[0])
	if err != nil {
		// exec.LookPath on Windows searches PATH and the app dir; if the
		// user configured a bare name like "notepad" it resolves here.
		return nil, fmt.Errorf("lookup %s: %w", cmd[0], err)
	}

	// Launch the application ourselves. We detach it from xpra (there is
	// no xpra child relationship on Windows) and capture its PID so we can
	// find its top-level window and clean it up on Delete.
	appCmd := exec.Command(exePath, cmd[1:]...)
	appCmd.Dir = cwd
	appCmd.Env = env
	if err := appCmd.Start(); err != nil {
		return nil, fmt.Errorf("start application %s: %w", cmd[0], err)
	}

	// Wait for the app to create a visible top-level window. We poll up to
	// 20s; most GUI apps show a window within a second or two.
	hwnd, err := waitForTopLevelWindow(uint32(appCmd.Process.Pid), 20*time.Second)
	if err != nil {
		_ = killProcessTree(appCmd.Process.Pid)
		return nil, err
	}

	args := []string{
		"shadow", "windows=" + strconv.FormatUint(uint64(hwnd), 10),
		"--bind-ws=127.0.0.1:" + strconv.Itoa(port),
		"--html=auto",
		"--attach=no",
		"--daemon=no",
		"--pulseaudio=no",
		"--notifications=no",
		"--systemd-run=no",
		"--tray=no",
		"--system-tray=no",
		// When the shadowed window disappears (the app is closed or
		// crashes) the shadow server exits on its own, which watchExit
		// observes the same way it observes a Unix xpra exit.
		"--exit-with-windows=yes",
	}

	xc := exec.Command(xpraPath(), args...)
	xc.Dir = cwd
	xc.Env = env
	xc.Stdout = nil
	xc.Stderr = nil
	if err := xc.Start(); err != nil {
		_ = killProcessTree(appCmd.Process.Pid)
		return nil, fmt.Errorf("start xpra: %w", err)
	}

	return &Session{
		ID:      id,
		Cwd:     cwd,
		Cmd:     cmd,
		Port:    port,
		Spec:    "windows=" + strconv.FormatUint(uint64(hwnd), 10),
		xpraCmd: xc,
		appCmd:  appCmd,
	}, nil
}

// stopSessionImpl kills the xpra shadow server process tree and the
// application process tree. `xpra stop <spec>` does not reliably resolve
// per-window shadow sessions on Windows (it hangs), and since we spawn
// xpra with --daemon=no we own the process, so a tree kill is the robust
// shutdown path. taskkill /T /F recursively kills descendants, which also
// terminates any helpers xpra spawned.
func stopSessionImpl(sess *Session) error {
	var firstErr error
	if sess.xpraCmd != nil && sess.xpraCmd.Process != nil {
		if err := killProcessTree(sess.xpraCmd.Process.Pid); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if sess.appCmd != nil && sess.appCmd.Process != nil {
		if err := killProcessTree(sess.appCmd.Process.Pid); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// releaseSessionImpl is a no-op on Windows: there is no display-number
// allocator (the session is identified by the window hwnd, which xpra
// manages).
func releaseSessionImpl(sess *Session) {}

// killProcessTree runs `taskkill /PID <pid> /T /F` to kill a process and all
// of its descendants. This is the Windows equivalent of killing a process
// group. We use taskkill rather than Process.Kill so children (e.g. the Xvfb
// or helper processes xpra spawns, or sub-processes the app spawns) are
// cleaned up rather than orphaned.
func killProcessTree(pid int) error {
	return exec.Command("taskkill", "/PID", strconv.Itoa(pid), "/T", "/F").Run()
}

// waitForTopLevelWindow polls EnumWindows for a visible, unowned top-level
// window belonging to the given PID. "Unowned" (GetWindow(GW_OWNER)==0)
// filters out transient windows and toolwindows that some apps create
// before their real main window. Returns an error if no window appears
// before the timeout.
func waitForTopLevelWindow(pid uint32, timeout time.Duration) (uintptr, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if hwnd := findTopLevelWindow(pid); hwnd != 0 {
			return hwnd, nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return 0, fmt.Errorf("application (pid %d) did not create a visible window within %s", pid, timeout)
}

// findTopLevelWindow enumerates all top-level windows and returns the first
// visible one owned by the given PID that has a non-empty title or is a
// main window (GW_OWNER == 0). The title check is relaxed: some apps briefly
// create untitled visible windows; requiring a title would miss fast-launching
// apps. We instead rely on visibility and ownership.
func findTopLevelWindow(pid uint32) uintptr {
	var found uintptr
	cb := syscall.NewCallback(func(hwnd uintptr, lparam uintptr) uintptr {
		if !isWindowVisible(hwnd) {
			return 1 // continue
		}
		var wpid uint32
		procGetWindowThreadProcessId.Call(hwnd, uintptr(unsafe.Pointer(&wpid)))
		if wpid != pid {
			return 1
		}
		// Skip windows that are owned by another top-level window
		// (transients, dialogs owned by the main window). We want the
		// main window itself.
		if getOwner(hwnd) != 0 {
			return 1
		}
		found = hwnd
		return 0 // stop
	})
	procEnumWindows.Call(cb, 0)
	return found
}

func isWindowVisible(hwnd uintptr) bool {
	ret, _, _ := procIsWindowVisible.Call(hwnd)
	return ret != 0
}

func getOwner(hwnd uintptr) uintptr {
	// GetWindow is not in the lazy proc list above; load it on demand to
	// keep the var block tidy. We only call it here.
	gw := user32.NewProc("GetWindow")
	owner, _, _ := gw.Call(hwnd, gwOwner)
	return owner
}