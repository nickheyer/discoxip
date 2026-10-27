package buffer

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Builds a vertex buffer file from its 8 byte pool header and raw vertex bytes
func vbFile(count, format uint32, raw []byte) []byte {
	out := make([]byte, 8, 8+len(raw))
	binary.LittleEndian.PutUint32(out[0:], count)
	binary.LittleEndian.PutUint32(out[4:], format)
	return append(out, raw...)
}

func TestReadVB(t *testing.T) {
	raw := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	file := vbFile(2, 0x80000112, raw)
	vb, err := ReadVB(bytes.NewReader(file), int64(len(file)))
	if err != nil {
		t.Fatalf("ReadVB: %v", err)
	}
	if vb.Header != (VBHeader{VertexCount: 2, FormatCode: 0x80000112}) {
		t.Errorf("Header = %+v", vb.Header)
	}
	if vb.Stride != 8 {
		t.Errorf("Stride = %d, want 8", vb.Stride)
	}
	if !bytes.Equal(vb.RawData, raw) {
		t.Errorf("RawData = %v, want %v", vb.RawData, raw)
	}
}

func TestReadVBSingleVertex(t *testing.T) {
	raw := make([]byte, 44)
	file := vbFile(1, 0x152, raw)
	vb, err := ReadVB(bytes.NewReader(file), int64(len(file)))
	if err != nil {
		t.Fatalf("ReadVB: %v", err)
	}
	if vb.Stride != 44 || len(vb.RawData) != 44 {
		t.Errorf("Stride %d RawData %d bytes, want 44 and 44", vb.Stride, len(vb.RawData))
	}
}

func TestReadVBZeroVertices(t *testing.T) {
	for _, trailing := range []int{0, 12} {
		file := vbFile(0, 0x112, make([]byte, trailing))
		vb, err := ReadVB(bytes.NewReader(file), int64(len(file)))
		if err != nil {
			t.Fatalf("ReadVB with %d trailing bytes: %v", trailing, err)
		}
		if vb.Header != (VBHeader{VertexCount: 0, FormatCode: 0x112}) || vb.Stride != 0 || vb.RawData != nil {
			t.Errorf("reader with %d trailing bytes = %+v, want header only", trailing, vb)
		}
	}
}

func TestReadVBErrors(t *testing.T) {
	good := vbFile(2, 0x112, make([]byte, 16))
	cases := []struct {
		name string
		file []byte
		size int64
		want error
	}{
		{"size below header", good, 7, ErrTruncatedVB},
		{"empty", nil, 0, ErrTruncatedVB},
		{"data not a multiple of count", vbFile(3, 0x112, make([]byte, 16)), 24, ErrBadStride},
		{"count above data size", vbFile(5, 0x112, make([]byte, 4)), 12, ErrBadStride},
		{"short header", good[:4], 24, io.ErrUnexpectedEOF},
		{"no header bytes", nil, 24, io.EOF},
		{"short data", good[:16], 24, io.ErrUnexpectedEOF},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			vb, err := ReadVB(bytes.NewReader(c.file), c.size)
			if !errors.Is(err, c.want) {
				t.Fatalf("err = %v, want %v", err, c.want)
			}
			if vb != nil {
				t.Errorf("reader = %+v, want nil", vb)
			}
		})
	}
}

func TestOpenVB(t *testing.T) {
	dir := t.TempDir()
	raw := append(f32s(1, 2, 3), f32s(4, 5, 6)...)
	path := filepath.Join(dir, "pool.vb")
	if err := os.WriteFile(path, vbFile(2, 0x12, raw), 0o644); err != nil {
		t.Fatal(err)
	}
	vb, err := OpenVB(path)
	if err != nil {
		t.Fatalf("OpenVB: %v", err)
	}
	if vb.Header.VertexCount != 2 || vb.Header.FormatCode != 0x12 || vb.Stride != 12 || !bytes.Equal(vb.RawData, raw) {
		t.Errorf("reader = %+v", vb)
	}

	short := filepath.Join(dir, "short.vb")
	if err := os.WriteFile(short, []byte{1, 2, 3}, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenVB(short); !errors.Is(err, ErrTruncatedVB) {
		t.Errorf("short file err = %v, want %v", err, ErrTruncatedVB)
	}

	if _, err := OpenVB(filepath.Join(dir, "missing.vb")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("missing file err = %v, want %v", err, fs.ErrNotExist)
	}
}

func TestVBReaderDecode(t *testing.T) {
	raw := append(append(f32s(1, 2), []byte{0x10, 0x20, 0x30, 0x40}...), append(f32s(3, 4), []byte{0xFF, 0xFF, 0xFF, 0xFF}...)...)
	file := vbFile(2, 0x52, raw)
	vb, err := ReadVB(bytes.NewReader(file), int64(len(file)))
	if err != nil {
		t.Fatalf("ReadVB: %v", err)
	}
	elements := []Element{
		{Register: 0, Type: "FLOAT2", TypeCode: 0x22, Offset: 0, Bytes: 8, Comps: 2},
		{Register: 3, Type: "D3DCOLOR", TypeCode: 0x40, Offset: 8, Bytes: 4, Comps: 4},
	}
	attrs, err := vb.Decode(elements, 12)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if len(attrs) != 2 {
		t.Fatalf("got %d attributes, want 2", len(attrs))
	}
	wantPos := []float32{1, 2, 3, 4}
	wantColor := []float32{float32(0x30) / 255, float32(0x20) / 255, float32(0x10) / 255, float32(0x40) / 255, 1, 1, 1, 1}
	for i, w := range wantPos {
		if attrs[0].Data[i] != w {
			t.Errorf("position float %d = %v, want %v", i, attrs[0].Data[i], w)
		}
	}
	for i, w := range wantColor {
		if attrs[1].Data[i] != w {
			t.Errorf("color float %d = %v, want %v", i, attrs[1].Data[i], w)
		}
	}
}

func TestVBReaderDecodeStrideMismatch(t *testing.T) {
	file := vbFile(2, 0x52, make([]byte, 24))
	vb, err := ReadVB(bytes.NewReader(file), int64(len(file)))
	if err != nil {
		t.Fatalf("ReadVB: %v", err)
	}
	elements := []Element{{Register: 0, Type: "FLOAT3", TypeCode: 0x32, Bytes: 12, Comps: 3}}
	attrs, err := vb.Decode(elements, 16)
	if err == nil || !strings.Contains(err.Error(), "stride 16") || !strings.Contains(err.Error(), "stride 12") || !strings.Contains(err.Error(), "00000052") {
		t.Errorf("err = %v, want a stride mismatch naming 16, 12 and format 00000052", err)
	}
	if attrs != nil {
		t.Errorf("attributes = %+v, want nil", attrs)
	}
}

func TestVBReaderDecodeZeroVertices(t *testing.T) {
	file := vbFile(0, 0x52, nil)
	vb, err := ReadVB(bytes.NewReader(file), int64(len(file)))
	if err != nil {
		t.Fatalf("ReadVB: %v", err)
	}
	elements := []Element{{Register: 0, Type: "FLOAT3", TypeCode: 0x32, Bytes: 12, Comps: 3}}
	attrs, err := vb.Decode(elements, 999)
	if attrs != nil || err != nil {
		t.Errorf("Decode = %v, %v, want nil, nil", attrs, err)
	}
}

func TestVBReaderDecodeElementError(t *testing.T) {
	file := vbFile(1, 0x52, make([]byte, 4))
	vb, err := ReadVB(bytes.NewReader(file), int64(len(file)))
	if err != nil {
		t.Fatalf("ReadVB: %v", err)
	}
	elements := []Element{{Register: 0, Type: "MYSTERY", TypeCode: 0x99, Bytes: 4, Comps: 1}}
	if _, err := vb.Decode(elements, 4); err == nil || !strings.Contains(err.Error(), "99") {
		t.Errorf("err = %v, want a not decodable error", err)
	}
}
