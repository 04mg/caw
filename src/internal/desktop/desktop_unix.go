//go:build !windows

package desktop

import (
	"fmt"
	"os/exec"
	"strconv"
)

// launchXpra starts an xpra seamless server on a virtual X display and
// returns the Session. The app is launched by xpra via --start-child, so
// Caw does not own the app process and appCmd is left nil; xpra's
// --exit-with-children ties the server lifetime to the child.
func launchXpra(id, cwd string, cmd []string, env []string, port int) (*Session, error) {
	display := allocDisplay()
	if display < 0 {
		return nil, fmt.Errorf("no free xpra display number")
	}

	// xpra invocation:
	//   xpra start :<display>
	//     --start-child=<cmd...>
	//     --bind-ws=127.0.0.1:<port>
	//     --exit-with-children=yes
	//     --terminate-children=yes
	//     --attach=no           (don't auto-attach a local client)
	//     --daemon=no           (stay in foreground so we own the process)
	//     --pulseaudio=yes     (start a private pulse server so the app's
	//                           audio is forwarded to the bundled client)
	//     --speaker=on          (enable speaker forwarding)
	//     --microphone=off      (we don't capture local audio)
	//     --notifications=no    (don't spawn a notification daemon)
	// We no longer pass --html=auto: the HTML5 client is bundled in Caw
	// itself (see src/frontend/.../desktop/xpra) and loads directly, so xpra
	// only needs to serve the WebSocket stream on the bound port.
	startChild := joinShellSafe(cmd)
	args := []string{
		"start", ":" + strconv.Itoa(display),
		"--start-child=" + startChild,
		"--bind-ws=127.0.0.1:" + strconv.Itoa(port),
		"--exit-with-children=yes",
		"--terminate-children=yes",
		"--attach=no",
		"--daemon=no",
		"--pulseaudio=yes",
		"--speaker=on",
		"--microphone=off",
		"--notifications=no",
		"--systemd-run=no",
		// Chrome-free embedding: no xpra-drawn window borders, no tray,
		// and the virtual display resizes to match the client pane. The
		// app window is configured to fill the pane by the bundled client
		// so it fills the pane immediately while still resizing correctly.
		"--border=off",
		"--tray=no",
		"--system-tray=no",
		"--resize-display=yes",
	}

	xc := exec.Command(xpraPath(), args...)
	xc.Dir = cwd
	xc.Env = env
	// Keep xpra's stderr for debugging; the server logs startup progress
	// there and Create waits for the WS port to accept before returning.
	xc.Stdout = nil
	xc.Stderr = nil
	if err := xc.Start(); err != nil {
		releaseDisplay(display)
		return nil, fmt.Errorf("start xpra: %w", err)
	}

	return &Session{
		ID:      id,
		Cwd:     cwd,
		Cmd:     cmd,
		Display: display,
		Port:    port,
		Spec:    ":" + strconv.Itoa(display),
		xpraCmd: xc,
	}, nil
}

// stopSessionImpl runs `xpra stop :<display>` to cleanly shut down the
// server. We use the control command rather than Process.Kill so xpra has a
// chance to terminate the start-child gracefully (and --exit-with-children
// cleans up the Xvfb).
func stopSessionImpl(sess *Session) error {
	exe := xpraPath()
	if exe == "" {
		return fmt.Errorf("xpra is not installed; cannot stop %s", sess.Spec)
	}
	out, err := exec.Command(exe, "stop", sess.Spec).CombinedOutput()
	if err != nil {
		return fmt.Errorf("xpra stop %s: %w: %s", sess.Spec, err, out)
	}
	return nil
}

// releaseSessionImpl returns the xpra display number to the allocator pool.
func releaseSessionImpl(sess *Session) {
	releaseDisplay(sess.Display)
}