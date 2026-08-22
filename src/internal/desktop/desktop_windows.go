//go:build windows

package desktop

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
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

	// Resolve the executable for the CreateProcess fast path. Skip the
	// lookup for shell-activated (UWP) commands — they go through
	// `cmd /c start` and don't need a resolved exePath.
	var exePath string
	if !needsShellExecute(cmd) {
		var err error
		exePath, err = exec.LookPath(cmd[0])
		if err != nil {
			// exec.LookPath on Windows searches PATH and the app dir; if
			// the user configured a bare name like "notepad" it resolves
			// here.
			return nil, fmt.Errorf("lookup %s: %w", cmd[0], err)
		}
	}

	// Snapshot the visible top-level windows that already exist so we can
	// detect NEW windows the app creates, even when the launched PID is
	// not the one that owns the window (UWP apps launched via explorer.exe
	// shell:AppsFolder\..., single-instance apps that delegate to an
	// existing process, or apps that relaunch themselves elevated via
	// UAC — all break the launched-PID → window-owner relationship).
	existing := snapshotVisibleWindows()

	// Launch the application. Most apps resolve via exec.LookPath and
	// CreateProcess (fast path, gives us a PID). UWP/store apps launched
	// via "explorer.exe shell:AppsFolder\<AUMID>" are special: running
	// explorer.exe through CreateProcess just opens an Explorer window
	// instead of activating the app — UWP activation MUST go through the
	// shell (ShellExecute). Route any command containing "shell:" through
	// `cmd /c start` (which uses ShellExecute internally); we can't track
	// the PID, but the new-window detection below handles that.
	var appCmd *exec.Cmd
	if needsShellExecute(cmd) {
		// `cmd /c start "" "<args>..."` invokes ShellExecuteEx, which is
		// the only way to activate UWP apps. The empty title ("") is
		// required so the first quoted arg isn't treated as a title.
		appCmd = exec.Command("cmd", "/c", "start", "", strings.Join(cmd, " "))
	} else {
		appCmd = exec.Command(exePath, cmd[1:]...)
	}
	appCmd.Dir = cwd
	appCmd.Env = env
	if err := appCmd.Start(); err != nil {
		return nil, fmt.Errorf("start application %s: %w", cmd[0], err)
	}

	// Wait for the app to create a visible top-level window. Try the
	// launched PID first (fast path for most apps), then fall back to any
	// new visible top-level window. Allow 45s for heavy Electron/UWP apps.
	hwnd, err := waitForTopLevelWindow(uint32(appCmd.Process.Pid), 45*time.Second, existing)
	if err != nil {
		_ = killProcessTree(appCmd.Process.Pid)
		return nil, err
	}

	args := []string{
		"shadow", "windows=" + strconv.FormatUint(uint64(hwnd), 10),
		"--bind-ws=127.0.0.1:" + strconv.Itoa(port),
		// The HTML5 client is bundled in Caw (see src/frontend/.../desktop
		// /xpra) and loads directly, so xpra only needs to serve the WS
		// stream — no --html=auto.
		"--attach=no",
		"--daemon=no",
		// Forward the app's audio to the bundled client.
		"--speaker=on",
		"--microphone=off",
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

// waitForTopLevelWindow polls for a visible, unowned top-level window.
// It first tries windows owned by the launched PID (fast path), then
// falls back to any NEW visible top-level window that appeared since the
// pre-launch snapshot (handles UWP/single-instance/UAC apps where the
// window is owned by a different PID than the one we launched). Returns
// an error if no window appears before the timeout.
func waitForTopLevelWindow(pid uint32, timeout time.Duration, existing map[uintptr]bool) (uintptr, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if hwnd := findTopLevelWindow(pid); hwnd != 0 {
			return hwnd, nil
		}
		if hwnd := findNewTopLevelWindow(existing); hwnd != 0 {
			return hwnd, nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return 0, fmt.Errorf("application (pid %d) did not create a visible window within %s", pid, timeout)
}

// snapshotVisibleWindows returns the set of currently visible top-level
// window handles. Used as a pre-launch baseline so we can detect windows
// the app creates even when they are owned by a different PID.
func snapshotVisibleWindows() map[uintptr]bool {
	set := make(map[uintptr]bool)
	cb := syscall.NewCallback(func(hwnd uintptr, lparam uintptr) uintptr {
		if isWindowVisible(hwnd) && getOwner(hwnd) == 0 {
			set[hwnd] = true
		}
		return 1
	})
	procEnumWindows.Call(cb, 0)
	return set
}

// findNewTopLevelWindow enumerates all visible, unowned top-level windows
// and returns the first one NOT in the pre-launch snapshot. This catches
// apps whose window is owned by a process other than the one we launched
// (UWP apps, single-instance delegates, UAC-elevated relaunches).
func findNewTopLevelWindow(existing map[uintptr]bool) uintptr {
	var found uintptr
	cb := syscall.NewCallback(func(hwnd uintptr, lparam uintptr) uintptr {
		if !isWindowVisible(hwnd) {
			return 1
		}
		if getOwner(hwnd) != 0 {
			return 1
		}
		if existing[hwnd] {
			return 1
		}
		found = hwnd
		return 0
	})
	procEnumWindows.Call(cb, 0)
	return found
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

// needsShellExecute reports whether the command should be launched via
// ShellExecute (cmd /c start) rather than CreateProcess. UWP/store apps
// activated via "explorer.exe shell:AppsFolder\<AUMID>" REQUIRE the shell
// path: CreateProcess on explorer.exe just opens an Explorer window.
func needsShellExecute(cmd []string) bool {
	for _, arg := range cmd {
		if strings.Contains(arg, "shell:") {
			return true
		}
	}
	return false
}