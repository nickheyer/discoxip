package buffer

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
)

// IBReader reads a raw index buffer (uint16 little-endian, no header).
type IBReader struct {
	Indices       []uint16
	TriangleCount int
}

// ReadIB parses an index buffer from r with the given file size.
func ReadIB(r io.Reader, size int64) (*IBReader, error) {
	if size == 0 {
		return nil, ErrEmptyIB
	}
	if size%2 != 0 {
		return nil, ErrOddIB
	}

	count := int(size / 2)
	indices := make([]uint16, count)
	if err := binary.Read(r, binary.LittleEndian, indices); err != nil {
		return nil, fmt.Errorf("buffer: reading IB data: %w", err)
	}

	return &IBReader{
		Indices:       indices,
		TriangleCount: count / 3,
	}, nil
}

// OpenIB opens an IB file from disk.
func OpenIB(path string) (*IBReader, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}

	return ReadIB(f, fi.Size())
}
