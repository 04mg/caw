package agent

// DesktopApp describes a graphical application that Caw can launch inside
// an xpra desktop session. It is the desktop equivalent of Info: an id, a
// human-readable label, and the command to launch as xpra's --start-child.
// The registry in ListDesktopApps filters by exec.LookPath for both the
// app binary and the xpra binary, so apps that aren't installed (or a host
// without xpra) simply don't appear in the New Tab menu.
type DesktopApp struct {
	ID    string   `json:"id"`
	Label string   `json:"label"`
	Cmd   []string `json:"cmd"`
	Env   [][]string `json:"env,omitempty"` // [key, value] pairs to inject into the start-child environment
}