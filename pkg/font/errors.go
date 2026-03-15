package font

import "errors"

var (
	ErrInvalidMagic  = errors.New("font: invalid magic (expected XTF1)")
	ErrTruncatedFile = errors.New("font: file is truncated")
)
