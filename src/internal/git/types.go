package git

import "errors"

var ErrNotGitRepo = errors.New("not a git repository")

type ContentResponse struct {
	Content string `json:"content"`
}