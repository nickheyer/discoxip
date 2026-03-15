package mesh

import (
	"encoding/binary"
	"image/color"
)

// BinaryMesh holds parsed binary XM mesh data.
// Binary XM files typically contain per-vertex RGBA color data.
// Each vertex gets a 4-byte color (R, G, B, A in byte order).
// Files filled with zeros represent meshes with no custom vertex colors.
type BinaryMesh struct {
	RawData      []byte
	VertexColors []color.RGBA // per-vertex colors (len = file size / 4)
	HeaderWords  []uint32     // first N uint32 values for diagnostics
	ColorCount   int          // number of color entries (= file size / 4)

	// Heuristic fields for non-color binary data
	VBPoolIndex int
	IBPoolIndex int
	VertexCount int
	IndexCount  int
}

// ParseBinary extracts structure from binary XM data.
// Detects whether the data is RGBA vertex colors or structured header data.
func ParseBinary(data []byte) *BinaryMesh {
	bm := &BinaryMesh{
		RawData:    data,
		ColorCount: len(data) / 4,
	}

	// Extract header as uint32 words for diagnostics
	maxWords := min(len(data)/4, 16)
	bm.HeaderWords = make([]uint32, maxWords)
	for i := range maxWords {
		bm.HeaderWords[i] = binary.LittleEndian.Uint32(data[i*4:])
	}

	// Check if this is RGBA vertex color data.
	// Heuristic: if >50% of 4-byte records have 0xFF as the last byte (alpha),
	// treat the whole file as per-vertex RGBA colors.
	if bm.ColorCount > 0 && isRGBAData(data) {
		bm.VertexColors = make([]color.RGBA, bm.ColorCount)
		for i := range bm.ColorCount {
			off := i * 4
			bm.VertexColors[i] = color.RGBA{
				R: data[off],
				G: data[off+1],
				B: data[off+2],
				A: data[off+3],
			}
		}
	}

	return bm
}

// isRGBAData checks if binary data looks like RGBA vertex color entries.
func isRGBAData(data []byte) bool {
	records := len(data) / 4
	if records == 0 {
		return false
	}

	ffCount := 0
	for i := 0; i < records; i++ {
		if data[i*4+3] == 0xFF {
			ffCount++
		}
	}
	return ffCount > records/2
}
