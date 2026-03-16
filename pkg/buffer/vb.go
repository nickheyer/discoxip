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

// decodeCompressed16 decodes a 16-byte per-vertex format.
//
// Layout (verified by data analysis):
//
//	Offset 0:  float32       — position X
//	Offset 4:  int16 SHORT2N — normal X (divide by 32767 for [-1,1])
//	Offset 6:  int16 SHORT2N — normal Y (divide by 32767 for [-1,1])
//	Offset 8:  float32       — position Z
//	Offset 12: uint16 USHORT2N — texture U coordinate [0,1]
//	Offset 14: uint16 USHORT2N — texture V coordinate [0,1]
//
// Only two position axes are stored (X, Z). The Y axis is zero in local space;
// the scene graph transforms handle 3D placement. Normal Z is derived from
// the unit normal constraint: nz = sqrt(1 - nx² - ny²).
func decodeCompressed16(data []byte, count int) ([]Vertex, error) {
	verts := make([]Vertex, count)
	for i := range count {
		off := i * 16

		// Position: X from first float, Z from second float, Y = 0
		verts[i].Pos[0] = math.Float32frombits(binary.LittleEndian.Uint32(data[off:]))
		verts[i].Pos[1] = 0
		verts[i].Pos[2] = math.Float32frombits(binary.LittleEndian.Uint32(data[off+8:]))

		// Normal: XY from SHORT2N, Z derived from unit length
		nx := float32(int16(binary.LittleEndian.Uint16(data[off+4:]))) / 32767.0
		ny := float32(int16(binary.LittleEndian.Uint16(data[off+6:]))) / 32767.0
		sq := nx*nx + ny*ny
		nz := float32(0)
		if sq < 1.0 {
			nz = float32(math.Sqrt(float64(1.0 - sq)))
		}
		verts[i].Normal[0] = nx
		verts[i].Normal[1] = ny
		verts[i].Normal[2] = nz

		// UV: USHORT2N normalized to [0,1]
		u := binary.LittleEndian.Uint16(data[off+12:])
		v := binary.LittleEndian.Uint16(data[off+14:])
		verts[i].UV[0] = float32(u) / 65535.0
		verts[i].UV[1] = float32(v) / 65535.0
	}
	return verts, nil
}
