package buffer

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
)

const vbHeaderSize = 8

// VBHeader is the 8-byte vertex buffer file header.
type VBHeader struct {
	VertexCount uint32
	FormatCode  uint32
}

// VBReader holds a vertex buffer file's header and raw vertex data.
type VBReader struct {
	Header  VBHeader
	Stride  int
	RawData []byte
}

// ReadVB parses a vertex buffer from r with the given file size.
func ReadVB(r io.Reader, size int64) (*VBReader, error) {
	if size < vbHeaderSize {
		return nil, ErrTruncatedVB
	}
	var h VBHeader
	if err := binary.Read(r, binary.LittleEndian, &h); err != nil {
		return nil, fmt.Errorf("buffer: reading VB header: %w", err)
	}
	dataSize := size - vbHeaderSize
	if h.VertexCount == 0 {
		return &VBReader{Header: h}, nil
	}
	stride := int(dataSize) / int(h.VertexCount)
	if int(dataSize)%int(h.VertexCount) != 0 {
		return nil, ErrBadStride
	}
	raw := make([]byte, dataSize)
	if _, err := io.ReadFull(r, raw); err != nil {
		return nil, fmt.Errorf("buffer: reading VB data: %w", err)
	}
	return &VBReader{Header: h, Stride: stride, RawData: raw}, nil
}

// OpenVB opens a VB file from disk.
func OpenVB(path string) (*VBReader, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	return ReadVB(f, fi.Size())
}

// Decode expands the raw vertices through a declaration whose stride must match the file
func (vb *VBReader) Decode(elements []Element, declStride int) ([]Attribute, error) {
	if vb.Header.VertexCount == 0 {
		return nil, nil
	}
	if declStride != vb.Stride {
		return nil, fmt.Errorf("buffer: declaration stride %d does not match file stride %d for format %08X", declStride, vb.Stride, vb.Header.FormatCode)
	}
	return DecodeDeclared(vb.RawData, int(vb.Header.VertexCount), vb.Stride, elements)
}
