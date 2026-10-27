package buffer

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestPrimitiveTypeValues(t *testing.T) {
	if PrimTriangleList != 0 {
		t.Errorf("PrimTriangleList = %d, want 0", PrimTriangleList)
	}
	if PrimTriangleStrip != 1 {
		t.Errorf("PrimTriangleStrip = %d, want 1", PrimTriangleStrip)
	}
	var p PrimitiveType
	if p != PrimTriangleList {
		t.Errorf("zero PrimitiveType = %d, want PrimTriangleList", p)
	}
}

func TestReadIB(t *testing.T) {
	cases := []struct {
		name    string
		indices []uint16
		tris    int
	}{
		{"two triangles", []uint16{0, 1, 2, 2, 1, 3}, 2},
		{"partial triangle", []uint16{5, 6, 7, 8}, 1},
		{"one index", []uint16{0xFFFF}, 0},
		{"three indices", []uint16{300, 200, 100}, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			file := u16s(c.indices)
			ib, err := ReadIB(bytes.NewReader(file), int64(len(file)))
			if err != nil {
				t.Fatalf("ReadIB: %v", err)
			}
			if len(ib.Indices) != len(c.indices) {
				t.Fatalf("Indices = %v, want %v", ib.Indices, c.indices)
			}
			for i := range c.indices {
				if ib.Indices[i] != c.indices[i] {
					t.Errorf("index %d = %d, want %d", i, ib.Indices[i], c.indices[i])
				}
			}
			if ib.TriangleCount != c.tris {
				t.Errorf("TriangleCount = %d, want %d", ib.TriangleCount, c.tris)
			}
		})
	}
}

// Encodes uint16 indices little endian
func u16s(vals []uint16) []byte {
	out := make([]byte, 2*len(vals))
	for i, v := range vals {
		out[2*i] = byte(v)
		out[2*i+1] = byte(v >> 8)
	}
	return out
}

func TestReadIBErrors(t *testing.T) {
	cases := []struct {
		name string
		file []byte
		size int64
		want error
	}{
		{"empty", nil, 0, ErrEmptyIB},
		{"odd size", []byte{1, 2, 3}, 3, ErrOddIB},
		{"odd size one byte", []byte{1}, 1, ErrOddIB},
		{"short reader", []byte{1, 2, 3, 4}, 8, io.ErrUnexpectedEOF},
		{"empty reader", nil, 4, io.EOF},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ib, err := ReadIB(bytes.NewReader(c.file), c.size)
			if !errors.Is(err, c.want) {
				t.Fatalf("err = %v, want %v", err, c.want)
			}
			if ib != nil {
				t.Errorf("reader = %+v, want nil", ib)
			}
		})
	}
}

func TestOpenIB(t *testing.T) {
	dir := t.TempDir()
	indices := []uint16{3, 4, 5, 5, 4, 6, 7, 8, 9}
	path := filepath.Join(dir, "pool.ib")
	if err := os.WriteFile(path, u16s(indices), 0o644); err != nil {
		t.Fatal(err)
	}
	ib, err := OpenIB(path)
	if err != nil {
		t.Fatalf("OpenIB: %v", err)
	}
	if len(ib.Indices) != 9 || ib.TriangleCount != 3 {
		t.Fatalf("reader = %+v", ib)
	}
	for i := range indices {
		if ib.Indices[i] != indices[i] {
			t.Errorf("index %d = %d, want %d", i, ib.Indices[i], indices[i])
		}
	}

	empty := filepath.Join(dir, "empty.ib")
	if err := os.WriteFile(empty, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenIB(empty); !errors.Is(err, ErrEmptyIB) {
		t.Errorf("empty file err = %v, want %v", err, ErrEmptyIB)
	}

	odd := filepath.Join(dir, "odd.ib")
	if err := os.WriteFile(odd, []byte{1, 2, 3}, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenIB(odd); !errors.Is(err, ErrOddIB) {
		t.Errorf("odd file err = %v, want %v", err, ErrOddIB)
	}

	if _, err := OpenIB(filepath.Join(dir, "missing.ib")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("missing file err = %v, want %v", err, fs.ErrNotExist)
	}
}
