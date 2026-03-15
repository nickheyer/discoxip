package texture

import "errors"

var (
	ErrInvalidMagic      = errors.New("texture: invalid magic (expected XPR0)")
	ErrTruncatedFile     = errors.New("texture: file is truncated")
	ErrUnsupportedFormat = errors.New("texture: unsupported pixel format")
	ErrNoTexture         = errors.New("texture: no texture resource found")
)
