package buffer

import "errors"

var (
	ErrTruncatedVB    = errors.New("buffer: vertex buffer file is truncated")
	ErrBadStride      = errors.New("buffer: data size is not a multiple of computed stride")
	ErrUnknownFormat  = errors.New("buffer: unknown vertex format")
	ErrEmptyIB        = errors.New("buffer: index buffer is empty")
	ErrOddIB          = errors.New("buffer: index buffer size is not a multiple of 2")
	ErrIndexOutOfRange = errors.New("buffer: index references vertex beyond vertex count")
)
