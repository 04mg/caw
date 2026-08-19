package desktop

import (
	"os/exec"
	"sync"
)

// xpraBinary is the name of the xpra executable looked up on PATH. Caw
// auto-provisions xpra on first launch (see EnsureInstalled) so this is
// normally on PATH after the first run; the LookPath gate keeps the
// desktop-app menu hidden on hosts where provisioning failed or was
// skipped (e.g. offline).
const xpraBinary = "xpra"

// xpraAvailable reports whether the xpra executable can be found on PATH.
// Used by the agent registry to filter desktop apps out of the New Tab menu
// on hosts that don't have xpra installed.
func xpraAvailable() bool {
	_, err := exec.LookPath(xpraBinary)
	return err == nil
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