package buffer

import (
	"encoding/binary"
	"math"
	"strings"
	"testing"
)

// Encodes float32 values little endian
func f32s(vals ...float32) []byte {
	out := make([]byte, 4*len(vals))
	for i, v := range vals {
		binary.LittleEndian.PutUint32(out[4*i:], math.Float32bits(v))
	}
	return out
}

// Encodes int16 values little endian
func i16s(vals ...int16) []byte {
	out := make([]byte, 2*len(vals))
	for i, v := range vals {
		binary.LittleEndian.PutUint16(out[2*i:], uint16(v))
	}
	return out
}

// Encodes one uint32 little endian
func u32(v uint32) []byte {
	out := make([]byte, 4)
	binary.LittleEndian.PutUint32(out, v)
	return out
}

// Packs three two's complement fields of 11, 11 and 10 bits into the NORMPACKED3 word
func normPacked3(x, y, z int) uint32 {
	return uint32(x&0x7FF) | uint32(y&0x7FF)<<11 | uint32(z&0x3FF)<<22
}

var vertexTypes = []struct {
	code  uint32
	name  string
	bytes int
	comps int
	src   []byte
	want  []float32
}{
	{0x12, "FLOAT1", 4, 1, f32s(1.5), []float32{1.5}},
	{0x22, "FLOAT2", 8, 2, f32s(1.5, -2), []float32{1.5, -2}},
	{0x32, "FLOAT3", 12, 3, f32s(1.5, -2, 0.25), []float32{1.5, -2, 0.25}},
	{0x42, "FLOAT4", 16, 4, f32s(1.5, -2, 0.25, 1e10), []float32{1.5, -2, 0.25, 1e10}},
	{0x72, "FLOAT2H", 12, 3, f32s(0.25, 0.5, 1), []float32{0.25, 0.5, 1}},
	{0x40, "D3DCOLOR", 4, 4, []byte{0xDD, 0xCC, 0xBB, 0xAA}, []float32{float32(0xBB) / 255, float32(0xCC) / 255, float32(0xDD) / 255, float32(0xAA) / 255}},
	{0x40, "D3DCOLOR", 4, 4, []byte{0xFF, 0x00, 0x00, 0xFF}, []float32{0, 0, 1, 1}},
	{0x16, "NORMPACKED3", 4, 3, u32(normPacked3(511, -1024, -256)), []float32{float32(511) / 1023, -1, float32(-256) / 511}},
	{0x16, "NORMPACKED3", 4, 3, u32(normPacked3(-512, 1023, 511)), []float32{float32(-512) / 1023, 1, 1}},
	{0x16, "NORMPACKED3", 4, 3, u32(normPacked3(0, -1, -512)), []float32{0, float32(-1) / 1023, -1}},
	{0x25, "SHORT2", 4, 2, i16s(-5, 300), []float32{-5, 300}},
	{0x45, "SHORT4", 8, 4, i16s(-5, 300, 32767, -32768), []float32{-5, 300, 32767, -32768}},
	{0x15, "SHORT1", 2, 1, i16s(-32768), []float32{-32768}},
	{0x35, "SHORT3", 6, 3, i16s(1, 2, 3), []float32{1, 2, 3}},
	{0x11, "NORMSHORT1", 2, 1, i16s(32767), []float32{1}},
	{0x21, "NORMSHORT2", 4, 2, i16s(-32767, 16384), []float32{-1, float32(16384) / 32767}},
	{0x31, "NORMSHORT3", 6, 3, i16s(0, 1, -1), []float32{0, float32(1) / 32767, float32(-1) / 32767}},
	{0x41, "NORMSHORT4", 8, 4, i16s(32767, -32767, 0, 100), []float32{1, -1, 0, float32(100) / 32767}},
	{0x14, "PBYTE1", 1, 1, []byte{255}, []float32{1}},
	{0x24, "PBYTE2", 2, 2, []byte{0, 128}, []float32{0, float32(128) / 255}},
	{0x34, "PBYTE3", 3, 3, []byte{1, 2, 3}, []float32{float32(1) / 255, float32(2) / 255, float32(3) / 255}},
	{0x44, "PBYTE4", 4, 4, []byte{255, 0, 51, 204}, []float32{1, 0, float32(51) / 255, float32(204) / 255}},
}

func TestDecodeDeclaredEveryType(t *testing.T) {
	for _, c := range vertexTypes {
		t.Run(c.name, func(t *testing.T) {
			if len(c.src) != c.bytes {
				t.Fatalf("test source has %d bytes, type is %d bytes wide", len(c.src), c.bytes)
			}
			second := make([]byte, len(c.src))
			for i := range c.src {
				second[i] = c.src[i] ^ 0xFF
			}
			raw := append(append([]byte(nil), second...), c.src...)
			el := Element{Register: 7, Type: c.name, TypeCode: c.code, Offset: 0, Bytes: c.bytes, Comps: c.comps}
			attrs, err := DecodeDeclared(raw, 2, c.bytes, []Element{el})
			if err != nil {
				t.Fatalf("DecodeDeclared: %v", err)
			}
			if len(attrs) != 1 || attrs[0].Element != el {
				t.Fatalf("attributes = %+v", attrs)
			}
			if len(attrs[0].Data) != 2*c.comps {
				t.Fatalf("Data has %d floats, want %d", len(attrs[0].Data), 2*c.comps)
			}
			got := attrs[0].Data[c.comps:]
			for i := range c.want {
				if got[i] != c.want[i] {
					t.Errorf("component %d = %v, want %v", i, got[i], c.want[i])
				}
			}
		})
	}
}

func TestSignedNorm(t *testing.T) {
	cases := []struct {
		packed      uint32
		shift, bits uint
		want        float32
	}{
		{0x3FF, 0, 11, float32(1023) / 1023},
		{0x400, 0, 11, -1},
		{0x7FF, 0, 11, float32(-1) / 1023},
		{0x1FF << 22, 22, 10, 1},
		{0x200 << 22, 22, 10, -1},
		{0x001 << 11, 11, 11, float32(1) / 1023},
		{0xFFFFFFFF, 11, 11, float32(-1) / 1023},
	}
	for _, c := range cases {
		if got := signedNorm(c.packed, c.shift, c.bits); got != c.want {
			t.Errorf("signedNorm(%08X, %d, %d) = %v, want %v", c.packed, c.shift, c.bits, got, c.want)
		}
	}
}

func TestDecodeDeclaredSeveralElements(t *testing.T) {
	elements := []Element{
		{Register: 0, Type: "FLOAT3", TypeCode: 0x32, Offset: 0, Bytes: 12, Comps: 3},
		{Register: 3, Type: "D3DCOLOR", TypeCode: 0x40, Offset: 12, Bytes: 4, Comps: 4},
		{Register: 9, Type: "SHORT2", TypeCode: 0x25, Offset: 16, Bytes: 4, Comps: 2},
		{Register: 10, Type: "PBYTE4", TypeCode: 0x44, Offset: 20, Bytes: 4, Comps: 4},
	}
	var raw []byte
	raw = append(raw, f32s(1, 2, 3)...)
	raw = append(raw, 0x10, 0x20, 0x30, 0x40)
	raw = append(raw, i16s(-7, 9)...)
	raw = append(raw, 0, 51, 102, 255)
	raw = append(raw, f32s(-1, -2, -3)...)
	raw = append(raw, 0xFF, 0xFF, 0xFF, 0x00)
	raw = append(raw, i16s(1000, -1000)...)
	raw = append(raw, 255, 204, 153, 0)
	raw = append(raw, f32s(0.5, 0.25, 0.125)...)
	raw = append(raw, 0x00, 0x00, 0x00, 0xFF)
	raw = append(raw, i16s(0, 0)...)
	raw = append(raw, 1, 2, 3, 4)
	if len(raw) != 72 {
		t.Fatalf("raw is %d bytes, want 72", len(raw))
	}
	attrs, err := DecodeDeclared(raw, 3, 24, elements)
	if err != nil {
		t.Fatalf("DecodeDeclared: %v", err)
	}
	want := [][]float32{
		{1, 2, 3, -1, -2, -3, 0.5, 0.25, 0.125},
		{float32(0x30) / 255, float32(0x20) / 255, float32(0x10) / 255, float32(0x40) / 255, 1, 1, 1, 0, 0, 0, 0, 1},
		{-7, 9, 1000, -1000, 0, 0},
		{0, float32(51) / 255, float32(102) / 255, 1, 1, float32(204) / 255, float32(153) / 255, 0, float32(1) / 255, float32(2) / 255, float32(3) / 255, float32(4) / 255},
	}
	if len(attrs) != len(elements) {
		t.Fatalf("got %d attributes, want %d", len(attrs), len(elements))
	}
	for i, a := range attrs {
		if a.Element != elements[i] {
			t.Errorf("attribute %d element = %+v, want %+v", i, a.Element, elements[i])
		}
		if len(a.Data) != len(want[i]) {
			t.Fatalf("attribute %d has %d floats, want %d", i, len(a.Data), len(want[i]))
		}
		for j := range want[i] {
			if a.Data[j] != want[i][j] {
				t.Errorf("attribute %d float %d = %v, want %v", i, j, a.Data[j], want[i][j])
			}
		}
	}
}

func TestDecodeDeclaredZeroVertices(t *testing.T) {
	el := Element{Register: 0, Type: "FLOAT3", TypeCode: 0x32, Bytes: 12, Comps: 3}
	attrs, err := DecodeDeclared(nil, 0, 12, []Element{el})
	if err != nil {
		t.Fatalf("DecodeDeclared: %v", err)
	}
	if len(attrs) != 1 || len(attrs[0].Data) != 0 || attrs[0].Element != el {
		t.Errorf("attributes = %+v", attrs)
	}
}

func TestDecodeDeclaredNoElements(t *testing.T) {
	attrs, err := DecodeDeclared(make([]byte, 24), 2, 12, nil)
	if err != nil || len(attrs) != 0 {
		t.Errorf("DecodeDeclared = %v, %v, want empty and nil", attrs, err)
	}
}

func TestDecodeDeclaredElementExceedsStride(t *testing.T) {
	elements := []Element{
		{Register: 0, Type: "FLOAT3", TypeCode: 0x32, Offset: 0, Bytes: 12, Comps: 3},
		{Register: 9, Type: "FLOAT2", TypeCode: 0x22, Offset: 12, Bytes: 8, Comps: 2},
	}
	attrs, err := DecodeDeclared(make([]byte, 32), 2, 16, elements)
	if err == nil || !strings.Contains(err.Error(), "exceeds stride 16") {
		t.Errorf("err = %v, want an exceeds stride error", err)
	}
	if attrs != nil {
		t.Errorf("attributes = %+v, want nil", attrs)
	}
}

func TestDecodeDeclaredUnknownType(t *testing.T) {
	el := Element{Register: 0, Type: "MYSTERY", TypeCode: 0x99, Offset: 0, Bytes: 4, Comps: 1}
	attrs, err := DecodeDeclared(make([]byte, 4), 1, 4, []Element{el})
	if err == nil || !strings.Contains(err.Error(), "99") {
		t.Errorf("err = %v, want a not decodable error naming type 99", err)
	}
	if attrs != nil {
		t.Errorf("attributes = %+v, want nil", attrs)
	}
	for _, code := range []uint32{0x02, 0x00, 0x52, 0x13} {
		el.TypeCode = code
		if _, err := DecodeDeclared(make([]byte, 4), 1, 4, []Element{el}); err == nil {
			t.Errorf("type %02X decoded without error", code)
		}
	}
}
