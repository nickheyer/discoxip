package xap

import "errors"

var (
	ErrUnexpectedEOF   = errors.New("xap: unexpected end of file")
	ErrUnexpectedToken = errors.New("xap: unexpected token")
)
