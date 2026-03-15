package buffer

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
)

// PrimitiveType indicates how index data should be interpreted.
type PrimitiveType int

const (
	PrimTriangleList  PrimitiveType = iota // every 3 indices form a triangle
	PrimTriangleStrip                      // sliding window of 3 indices
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

// StripToTriangles converts triangle strip indices to a triangle list,
// correctly alternating winding order for even/odd triangles and
// skipping degenerate triangles (used as strip restarts).
func StripToTriangles(indices []uint16) []uint16 {
	if len(indices) < 3 {
		return nil
	}

	tris := make([]uint16, 0, (len(indices)-2)*3)
	for i := 2; i < len(indices); i++ {
		i0, i1, i2 := indices[i-2], indices[i-1], indices[i]
		// Skip degenerate triangles (strip restart markers)
		if i0 == i1 || i1 == i2 || i0 == i2 {
			continue
		}
		if i%2 == 0 {
			tris = append(tris, i0, i1, i2)
		} else {
			// Flip winding for odd triangles to maintain consistent face orientation
			tris = append(tris, i1, i0, i2)
		}
	}
	return tris
}
