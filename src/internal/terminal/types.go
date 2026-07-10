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
)