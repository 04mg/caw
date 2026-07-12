package terminal

type CreateRequest struct {
	Cwd string   `json:"cwd"`
	ID  string   `json:"id"`
	Cmd []string `json:"cmd,omitempty"`
}

type KillRequest struct {
	ID           string `json:"id"`
	DeleteBranch bool   `json:"deleteBranch"`
}

var (
	OnSessionStart func(id string, cmd []string, cwd string)
	OnSessionExit  func(id string)
	// OnPtyActivity is invoked (from ReadLoop) whenever new bytes are read
	// from the PTY. It receives the leaf/session id and the byte count. Used
	// by the agent status watcher to know when its agent process is producing
	// output, so it can correlate lazily-created internal sessions (e.g.
	// OpenCode sessions created on first user message) to the correct PTY
	// when multiple agents of the same type run in the same cwd.
	OnPtyActivity func(id string, n int)
)