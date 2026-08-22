package desktop

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

// xpraBinary is the name of the xpra executable looked up on PATH. Caw
// does not install xpra itself; users install it on their device (see the
// Desktop settings section). The LookPath gate keeps the desktop-app menu
// hidden on hosts where xpra is not installed.
const xpraBinary = "xpra"

// windowsXpraDirs returns the directories where the Xpra Windows installer
// commonly places Xpra.exe. The installer does not always update PATH (and
// an already-running process never sees PATH changes), so LookPath alone
// misses valid installs on Windows.
func windowsXpraDirs() []string {
	dirs := []string{}
	for _, env := range []string{"ProgramFiles", "ProgramFiles(x86)"} {
		if v := os.Getenv(env); v != "" {
			dirs = append(dirs, v)
		}
	}
	if v := os.Getenv("LOCALAPPDATA"); v != "" {
		dirs = append(dirs, filepath.Join(v, "Programs"))
	} else if home, err := os.UserHomeDir(); err == nil && home != "" {
		dirs = append(dirs, filepath.Join(home, "AppData", "Local", "Programs"))
	}
	return dirs
}

// xpraPath resolves the absolute path of the xpra executable, returning ""
// when it cannot be found. On Windows it falls back to well-known install
// locations when the directory is missing from PATH.
func xpraPath() string {
	if p, err := exec.LookPath(xpraBinary); err == nil {
		return p
	}
	if runtime.GOOS != "windows" {
		return ""
	}
	for _, dir := range windowsXpraDirs() {
		candidate := filepath.Join(dir, "Xpra.exe")
		if st, err := os.Stat(candidate); err == nil && !st.IsDir() {
			return candidate
		}
	}
	return ""
}

// Available reports whether the xpra executable can be found on PATH (or,
// on Windows, in a well-known install location). Exported so other
// packages (e.g. the agent registry) share the same detection.
func Available() bool {
	return xpraPath() != ""
}

// xpraDebugInfo returns a human-readable trace of a detection attempt: the
// LookPath result, the running executable, and each Windows fallback
// location with its stat outcome. Used by the status endpoint to diagnose
// false "not installed" reports.
func xpraDebugInfo() string {
	var b strings.Builder
	if exe, err := os.Executable(); err == nil {
		fmt.Fprintf(&b, "exe=%s ", exe)
	}
	if p, err := exec.LookPath(xpraBinary); err == nil {
		fmt.Fprintf(&b, "lookpath=%s", p)
		return b.String()
	}
	b.WriteString("lookpath=none")
	if runtime.GOOS == "windows" {
		for _, dir := range windowsXpraDirs() {
			candidate := filepath.Join(dir, "Xpra.exe")
			if st, err := os.Stat(candidate); err == nil && !st.IsDir() {
				fmt.Fprintf(&b, " found=%s", candidate)
				return b.String()
			}
			fmt.Fprintf(&b, " miss=%s", candidate)
		}
	} else {
		b.WriteString(" (non-windows: no fallback locations)")
	}
	return b.String()
}

// xpraAvailable reports whether the xpra executable can be found. Used
// internally by the desktop package.
func xpraAvailable() bool {
	return Available()
}

// xpraVersion returns the xpra version string (e.g. "6.3.6") or an empty
// string if it cannot be determined.
func xpraVersion() string {
	exe := xpraPath()
	if exe == "" {
		return ""
	}
	out, err := exec.Command(exe, "--version").Output()
	if err != nil {
		return ""
	}
	fields := strings.Fields(string(out))
	for _, f := range fields {
		if f[0] >= '0' && f[0] <= '9' {
			return f
		}
	}
	return ""
}

// displayMu guards the xpra display-number allocator. Display numbers are
// xpra's :N virtual X server identifiers; they must be unique per running
// server on a host. We start at 100 to stay clear of the real X display
// (:0) and the common xpra proxy range (10-99).
var (
	displayMu    sync.Mutex
	nextDisplay  = 100
	allocatedDis = map[int]bool{}
)

// allocDisplay returns the next free xpra display number. It is released
// back to the pool by releaseDisplay when the session ends. The allocator
// walks upward from the last allocated number; xpra itself will reject a
// collision (the server fails to start and Create returns an error), so
// this is best-effort but cheap.
func allocDisplay() int {
	displayMu.Lock()
	defer displayMu.Unlock()
	for d := nextDisplay; d < 1000; d++ {
		if !allocatedDis[d] {
			allocatedDis[d] = true
			nextDisplay = d + 1
			return d
		}
	}
	for d := 100; d < 1000; d++ {
		if !allocatedDis[d] {
			allocatedDis[d] = true
			return d
		}
	}
	return -1
}

func releaseDisplay(d int) {
	displayMu.Lock()
	defer displayMu.Unlock()
	delete(allocatedDis, d)
}