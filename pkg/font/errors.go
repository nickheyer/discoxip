package font

import "errors"

var (
	ErrInvalidMagic  = errors.New("font: invalid magic (expected XTF0)")
	ErrTruncatedFile = errors.New("font: file is truncated")
)
