package desktop

// CreateRequest is the body of POST /api/desktop. It mirrors the terminal
// package's CreateRequest: a leaf id, a working directory, and the command
// to launch as the xpra --start-child. The command runs inside the xpra
// server's virtual display, so it must be a graphical X11 application.
type CreateRequest struct {
	Cwd string     `json:"cwd"`
	ID  string     `json:"id"`
	Cmd []string   `json:"cmd,omitempty"`
	Env [][]string `json:"env,omitempty"` // [key, value] pairs injected into the start-child environment
}

// KillRequest is the body of DELETE /api/desktop/{id}. It is intentionally
// empty today (the id is in the path) but kept for symmetry with the
// terminal package so future per-kill options can be added without a route
// change.
type KillRequest struct {
	ID           string `json:"id"`
	DeleteBranch bool   `json:"deleteBranch"`
}

var (
	// OnSessionStart is invoked once the xpra server has successfully bound
	// its WebSocket port and the start-child has been launched. Consumers
	// (agent status, push notifications) can use it to track desktop app
	// sessions the same way they track terminal sessions.
	OnSessionStart func(id string, cmd []string, cwd string)
	// OnSessionExit is invoked once the xpra server has been stopped and
	// the session removed from the manager. killed is true when the exit
	// was initiated by the user via SessionManager.Delete.
	OnSessionExit func(id string, killed bool)
)