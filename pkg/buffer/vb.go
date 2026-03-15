package buffer

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
)

const vbHeaderSize = 8

// VBHeader is the 8-byte vertex buffer file header.
type VBHeader struct {
	VertexCount uint32
	FormatCode  uint32
}

// VBReader reads and decodes a vertex buffer file.
type VBReader struct {
	Header   VBHeader
	Stride   int
	Vertices []Vertex
	RawData  []byte // raw vertex data (post-header)
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

	vbr := &VBReader{
		Header:  h,
		Stride:  stride,
		RawData: raw,
	}

	// Decode known formats
	format := VertexFormat(h.FormatCode)
	switch format {
	case FormatStandard24:
		verts, err := decodeStandard24(raw, int(h.VertexCount))
		if err != nil {
			return nil, err
		}
		vbr.Vertices = verts
	case FormatCompressed16:
		verts, err := decodeCompressed16(raw, int(h.VertexCount))
		if err != nil {
			return nil, err
		}
		vbr.Vertices = verts
	}

	return vbr, nil
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

// decodeStandard24: float32[3] pos + DWORD packed normal + float32[2] UV
func decodeStandard24(data []byte, count int) ([]Vertex, error) {
	verts := make([]Vertex, count)
	for i := range count {
		off := i * 24
		verts[i].Pos[0] = math.Float32frombits(binary.LittleEndian.Uint32(data[off:]))
		verts[i].Pos[1] = math.Float32frombits(binary.LittleEndian.Uint32(data[off+4:]))
		verts[i].Pos[2] = math.Float32frombits(binary.LittleEndian.Uint32(data[off+8:]))

		// Packed normal as D3DCOLOR: XYZW in bytes (X=R, Y=G, Z=B)
		packed := binary.LittleEndian.Uint32(data[off+12:])
		verts[i].Normal[0] = float32(int8(packed&0xFF)) / 127.0
		verts[i].Normal[1] = float32(int8((packed>>8)&0xFF)) / 127.0
		verts[i].Normal[2] = float32(int8((packed>>16)&0xFF)) / 127.0

		verts[i].UV[0] = math.Float32frombits(binary.LittleEndian.Uint32(data[off+16:]))
		verts[i].UV[1] = math.Float32frombits(binary.LittleEndian.Uint32(data[off+20:]))
	}
	return verts, nil
}

// decodeCompressed16: float32 X, int16×2, float32 Z, int16×2
// Partial decode — positions from float32 fields, packed int16 pairs normalized
func decodeCompressed16(data []byte, count int) ([]Vertex, error) {
	verts := make([]Vertex, count)
	for i := range count {
		off := i * 16
		verts[i].Pos[0] = math.Float32frombits(binary.LittleEndian.Uint32(data[off:]))

		// Two packed int16 values
		s0 := int16(binary.LittleEndian.Uint16(data[off+4:]))
		s1 := int16(binary.LittleEndian.Uint16(data[off+6:]))
		verts[i].Pos[1] = float32(s0) / 32767.0
		verts[i].Normal[0] = float32(s1) / 32767.0

		verts[i].Pos[2] = math.Float32frombits(binary.LittleEndian.Uint32(data[off+8:]))

		s2 := int16(binary.LittleEndian.Uint16(data[off+12:]))
		s3 := int16(binary.LittleEndian.Uint16(data[off+14:]))
		verts[i].Normal[1] = float32(s2) / 32767.0
		verts[i].Normal[2] = float32(s3) / 32767.0
	}
	return verts, nil
}
