package workspace

import "errors"

var (
	ErrNotDir  = errors.New("not a directory")
	ErrReadDir = errors.New("cannot read directory")
)