package buffer

import (
	"encoding/binary"
	"fmt"
	"math"
)

// Element is one attribute of a declared vertex layout.
type Element struct {
	Register int    `json:"register"`
	Type     string `json:"type"`
	TypeCode uint32 `json:"type_code"`
	Offset   int    `json:"offset"`
	Bytes    int    `json:"bytes"`
	Comps    int    `json:"comps"`
}

// Attribute is one decoded per vertex attribute expanded to floats.
type Attribute struct {
	Element Element
	Data    []float32 // Comps values per vertex
}

// Vertex is the decoded vertex representation used by the glTF exporter.
type Vertex struct {
	Pos    [3]float32
	Normal [3]float32
	UV     [2]float32
}

// DecodeDeclared expands raw vertices through a declaration into float attributes.
func DecodeDeclared(raw []byte, count int, stride int, elements []Element) ([]Attribute, error) {
	out := make([]Attribute, len(elements))
	for ei, el := range elements {
		if el.Offset+el.Bytes > stride {
			return nil, fmt.Errorf("buffer: element at offset %d exceeds stride %d", el.Offset, stride)
		}
		attr := Attribute{Element: el, Data: make([]float32, count*el.Comps)}
		for v := 0; v < count; v++ {
			src := raw[v*stride+el.Offset:]
			dst := attr.Data[v*el.Comps : (v+1)*el.Comps]
			if err := decodeElement(el, src, dst); err != nil {
				return nil, err
			}
		}
		out[ei] = attr
	}
	return out, nil
}

// Expands one element of one vertex according to the Xbox D3DVSDT type
func decodeElement(el Element, src []byte, dst []float32) error {
	f32 := func(i int) float32 { return math.Float32frombits(binary.LittleEndian.Uint32(src[4*i:])) }
	switch el.TypeCode {
	case 0x12, 0x22, 0x32, 0x42:
		for i := range dst {
			dst[i] = f32(i)
		}
	case 0x72:
		dst[0], dst[1], dst[2] = f32(0), f32(1), f32(2)
	case 0x40:
		packed := binary.LittleEndian.Uint32(src)
		dst[0] = float32((packed>>16)&0xFF) / 255
		dst[1] = float32((packed>>8)&0xFF) / 255
		dst[2] = float32(packed&0xFF) / 255
		dst[3] = float32(packed>>24) / 255
	case 0x16:
		packed := binary.LittleEndian.Uint32(src)
		dst[0] = signedNorm(packed, 0, 11)
		dst[1] = signedNorm(packed, 11, 11)
		dst[2] = signedNorm(packed, 22, 10)
	case 0x25, 0x45, 0x15, 0x35:
		for i := range dst {
			dst[i] = float32(int16(binary.LittleEndian.Uint16(src[2*i:])))
		}
	case 0x11, 0x21, 0x31, 0x41:
		for i := range dst {
			dst[i] = float32(int16(binary.LittleEndian.Uint16(src[2*i:]))) / 32767
		}
	case 0x14, 0x24, 0x34, 0x44:
		for i := range dst {
			dst[i] = float32(src[i]) / 255
		}
	default:
		return fmt.Errorf("buffer: vertex data type %02X is not decodable", el.TypeCode)
	}
	return nil
}

// Extracts a signed normalized field of the given width from a packed word
func signedNorm(packed uint32, shift, bits uint) float32 {
	raw := int32(packed<<(32-shift-bits)) >> (32 - bits)
	max := float32(int32(1)<<(bits-1) - 1)
	v := float32(raw) / max
	if v < -1 {
		v = -1
	}
	return v
}
