package agent

import "errors"

var (
	ErrProjectPathRequired  = errors.New("projectPath required")
	ErrWorktreePathRequired = errors.New("worktreePath required")
)