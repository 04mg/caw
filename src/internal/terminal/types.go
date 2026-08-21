package terminal

type CreateRequest struct {
	Cwd string     `json:"cwd"`
	ID  string     `json:"id"`
	Cmd []string   `json:"cmd,omitempty"`
	Env [][]string `json:"env,omitempty"` // [key, value] pairs injected into the PTY environment
}

type KillRequest struct {
	ID           string `json:"id"`
	DeleteBranch bool   `json:"deleteBranch"`
}

var (
	OnSessionStart func(id string, cmd []string, cwd string)
	// OnSessionExit is invoked once the PTY process has fully exited and the
	// ReadLoop has terminated. exitCode is the process exit code (a negative
	// value means the process was terminated by a signal); exitErr is any
	// error returned by cmd.Wait(); killed is true when the exit was
	// initiated by the user via SessionManager.Delete (so consumers can
	// distinguish an explicit kill from a crash that happened to be signalled).
	OnSessionExit func(id string, exitCode int, exitErr error, killed bool)
	// OnPtyActivity is invoked (from ReadLoop) whenever new bytes are read
	// from the PTY. It receives the leaf/session id and the byte count. Used
	// by the agent status watcher to know when its agent process is producing
	// output, so it can correlate lazily-created internal sessions (e.g.
	// OpenCode sessions created on first user message) to the correct PTY
	// when multiple agents of the same type run in the same cwd.
	OnPtyActivity func(id string, n int)
	OnPtyInput    func(id string, data string)
	// OnPtyOutput is invoked (from ReadLoop) with the raw output chunk every
	// time bytes are read from the PTY. Unlike OnPtyActivity (which only
	// carries the byte count), it exposes the content so the agent status
	// layer can detect TUI-rendered interactive prompts (e.g. Fx's question
	// and permission screens) that are never written to the agent's on-disk
	// transcript while they are pending.
	OnPtyOutput func(id string, data string)
	// OnPtyFocus is invoked when a terminal pane gains or loses the user's
	// focus. The focused flag is true when the pane becomes the active pane
	// the user is interacting with, false when it loses that status. Used by
	// the agent status watcher to make the idle-timeout and re-bind heuristics
	// aware of which terminal the user is currently looking at, so a focused
	// agent that the user is typing into is never falsely reverted to idle and
	// so re-binds are biased toward the pane the user is actually driving.
	OnPtyFocus func(id string, focused bool)
)