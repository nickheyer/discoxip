package xip

import "errors"

var (
	ErrInvalidMagic  = errors.New("xip: invalid magic (expected XIP0)")
	ErrTruncatedFile = errors.New("xip: file is truncated")
	ErrInvalidIndex  = errors.New("xip: filename entry references invalid file index")
	ErrInvalidOffset = errors.New("xip: name offset out of range")
)
