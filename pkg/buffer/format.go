package buffer

import "fmt"

// VertexFormat identifies the vertex element layout.
type VertexFormat uint32

const (
	// Stride 16: float32 X, int16×2 (packed normal?), float32 Z, int16×2 (packed UV?)
	FormatCompressed16 VertexFormat = 0x20000002
	// Stride 24: float32[3] pos + DWORD normal + float32[2] UV
	FormatStandard24 VertexFormat = 0x20000102
)

func (f VertexFormat) String() string {
	switch f {
	case FormatCompressed16:
		return "compressed16 (stride 16)"
	case FormatStandard24:
		return "standard24 (stride 24)"
	default:
		return fmt.Sprintf("unknown(0x%08X)", uint32(f))
	}
}

// Stride returns the per-vertex byte count for known formats.
// Returns 0 for unknown formats.
func (f VertexFormat) Stride() int {
	switch f {
	case FormatCompressed16:
		return 16
	case FormatStandard24:
		return 24
	default:
		return 0
	}
}

// Vertex is the decoded vertex representation, always expanded to full floats.
type Vertex struct {
	Pos    [3]float32
	Normal [3]float32
	UV     [2]float32
}
