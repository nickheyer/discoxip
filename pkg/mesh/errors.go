package mesh

import "errors"

var (
	ErrEmptyMesh   = errors.New("mesh: file is empty")
	ErrTruncatedXM = errors.New("mesh: file is truncated")
)
