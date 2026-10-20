package font

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
)

// GlyphRange describes a contiguous range of Unicode codepoints.
type GlyphRange struct {
	Start uint16
	Count uint16
}

// GlyphMetrics holds GLYPHMETRICSFLOAT data for a glyph.
type GlyphMetrics struct {
	BlackBoxX float32 // width of bounding box
	BlackBoxY float32 // height of bounding box
	OriginX   float32 // x origin
	OriginY   float32 // y origin
	AdvanceX  float32 // horizontal advance
	AdvanceY  float32 // vertical advance (usually 0)
}

// GlyphMesh holds the pre-triangulated outline for a glyph.
type GlyphMesh struct {
	Indices  []uint16
	Vertices [][2]float32
}

// Glyph combines metrics and mesh data for a single glyph.
type Glyph struct {
	Metrics GlyphMetrics
	Mesh    GlyphMesh
	meshOff uint32 // absolute file offset to mesh data (internal)
}

// Font holds a parsed XTF vector font.
type Font struct {
	Name       string
	Height     int // cell height in pixels (from header 0x04)
	GlyphCount int // total supported glyphs
	RangeCount int
	Ranges     []GlyphRange
	Glyphs     []Glyph
	FileSize   int64
}

// ReadXTF parses an XTF vector font file.
//
// XTF layout:
//
//	0x00  magic "XTF0"
//	0x04  uint32 name buffer size (always 32)
//	0x08  char[32] font name (null-terminated)
//	0x28  GLYPHSET:
//	  0x28  uint32 cbThis       (size of serialized GLYPHSET)
//	  0x2C  uint32 flAccel      (flags)
//	  0x30  uint32 cGlyphsSupported
//	  0x34  uint32 cRanges
//	  0x38  WCRANGE[cRanges]    (uint16 wcLow, uint16 cGlyphs each)
//	After GLYPHSET: per-glyph table (cGlyphsSupported entries)
//	  each: GLYPHMETRICSFLOAT (6 float32) + uint32 mesh offset
//	Mesh data at absolute offsets:
//	  uint16 indexCount, uint16 vertexCount
//	  uint16[indexCount] indices (triangle list)
//	  float32[vertexCount*2] vertices (x,y pairs)
func ReadXTF(r io.Reader, size int64) (*Font, error) {
	if size < 0x38 {
		return nil, ErrTruncatedFile
	}

	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("font: reading: %w", err)
	}

	if data[0] != 'X' || data[1] != 'T' || data[2] != 'F' || data[3] != '0' {
		return nil, ErrInvalidMagic
	}

	height := int(binary.LittleEndian.Uint32(data[0x04:]))

	nameEnd := 8
	for nameEnd < 0x28 && data[nameEnd] != 0 {
		nameEnd++
	}
	name := string(data[8:nameEnd])

	// GLYPHSET at 0x28
	glyphCount := int(binary.LittleEndian.Uint32(data[0x30:]))
	rangeCount := int(binary.LittleEndian.Uint32(data[0x34:]))

	if rangeCount < 0 || rangeCount > 100000 {
		return nil, fmt.Errorf("font: invalid range count %d", rangeCount)
	}

	// Parse ranges at 0x38
	ranges := make([]GlyphRange, rangeCount)
	off := 0x38
	totalFromRanges := 0
	for i := range rangeCount {
		if off+4 > len(data) {
			return nil, ErrTruncatedFile
		}
		ranges[i].Start = binary.LittleEndian.Uint16(data[off:])
		ranges[i].Count = binary.LittleEndian.Uint16(data[off+2:])
		totalFromRanges += int(ranges[i].Count)
		off += 4
	}

	if totalFromRanges != glyphCount {
		return nil, fmt.Errorf("font: range glyph sum %d != header count %d", totalFromRanges, glyphCount)
	}

	// Per-glyph table starts right after ranges
	glyphTableOff := off
	const recordSize = 28 // 6 floats (GLYPHMETRICSFLOAT) + 1 uint32 (mesh offset)
	glyphTableEnd := glyphTableOff + glyphCount*recordSize
	if glyphTableEnd > len(data) {
		return nil, ErrTruncatedFile
	}

	glyphs := make([]Glyph, glyphCount)
	for i := range glyphCount {
		rec := data[glyphTableOff+i*recordSize:]
		glyphs[i].Metrics.BlackBoxX = math.Float32frombits(binary.LittleEndian.Uint32(rec[0:]))
		glyphs[i].Metrics.BlackBoxY = math.Float32frombits(binary.LittleEndian.Uint32(rec[4:]))
		glyphs[i].Metrics.OriginX = math.Float32frombits(binary.LittleEndian.Uint32(rec[8:]))
		glyphs[i].Metrics.OriginY = math.Float32frombits(binary.LittleEndian.Uint32(rec[12:]))
		glyphs[i].Metrics.AdvanceX = math.Float32frombits(binary.LittleEndian.Uint32(rec[16:]))
		glyphs[i].Metrics.AdvanceY = math.Float32frombits(binary.LittleEndian.Uint32(rec[20:]))
		glyphs[i].meshOff = binary.LittleEndian.Uint32(rec[24:])
	}

	// Parse mesh data for each glyph
	for i := range glyphs {
		moff := int(glyphs[i].meshOff)
		if moff == 0 || moff+4 > len(data) {
			continue
		}
		icount := int(binary.LittleEndian.Uint16(data[moff:]))
		vcount := int(binary.LittleEndian.Uint16(data[moff+2:]))
		if icount == 0 || vcount == 0 {
			continue
		}

		idxStart := moff + 4
		idxEnd := idxStart + icount*2
		vertStart := idxEnd
		vertEnd := vertStart + vcount*8
		if vertEnd > len(data) {
			continue
		}

		indices := make([]uint16, icount)
		for j := range icount {
			indices[j] = binary.LittleEndian.Uint16(data[idxStart+j*2:])
		}

		vertices := make([][2]float32, vcount)
		for j := range vcount {
			voff := vertStart + j*8
			vertices[j][0] = math.Float32frombits(binary.LittleEndian.Uint32(data[voff:]))
			vertices[j][1] = math.Float32frombits(binary.LittleEndian.Uint32(data[voff+4:]))
		}

		glyphs[i].Mesh.Indices = indices
		glyphs[i].Mesh.Vertices = vertices
	}

	return &Font{
		Name:       name,
		Height:     height,
		GlyphCount: glyphCount,
		RangeCount: rangeCount,
		Ranges:     ranges,
		Glyphs:     glyphs,
		FileSize:   size,
	}, nil
}

// Open reads an XTF font file from disk.
func Open(path string) (*Font, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}

	return ReadXTF(f, fi.Size())
}

// UnicodeBlocks returns a summary of which Unicode blocks are covered.
func (f *Font) UnicodeBlocks() []string {
	var blocks []string
	for _, r := range f.Ranges {
		end := r.Start + r.Count - 1
		block := unicodeBlockName(r.Start)
		blocks = append(blocks, fmt.Sprintf("U+%04X..U+%04X (%d) %s", r.Start, end, r.Count, block))
	}
	return blocks
}

func unicodeBlockName(cp uint16) string {
	switch {
	case cp < 0x0080:
		return "Basic Latin"
	case cp < 0x0100:
		return "Latin-1 Supplement"
	case cp < 0x0180:
		return "Latin Extended-A"
	case cp < 0x0250:
		return "Latin Extended-B"
	case cp < 0x0300:
		return "Spacing Modifiers"
	case cp >= 0x2000 && cp < 0x2070:
		return "General Punctuation"
	case cp >= 0x20A0 && cp < 0x20D0:
		return "Currency Symbols"
	case cp >= 0x2100 && cp < 0x2150:
		return "Letterlike Symbols"
	case cp >= 0x2190 && cp < 0x2200:
		return "Arrows"
	case cp >= 0x2200 && cp < 0x2300:
		return "Mathematical Operators"
	case cp >= 0x2500 && cp < 0x2580:
		return "Box Drawing"
	case cp >= 0x25A0 && cp < 0x2600:
		return "Geometric Shapes"
	case cp >= 0x3000 && cp < 0x3040:
		return "CJK Symbols"
	case cp >= 0x3040 && cp < 0x30A0:
		return "Hiragana"
	case cp >= 0x30A0 && cp < 0x3100:
		return "Katakana"
	case cp >= 0x3100 && cp < 0x3130:
		return "Bopomofo"
	case cp >= 0x3130 && cp < 0x3190:
		return "Hangul Compatibility Jamo"
	case cp >= 0x3190 && cp < 0x31A0:
		return "Kanbun"
	case cp >= 0x3200 && cp < 0x3300:
		return "Enclosed CJK"
	case cp >= 0x3300 && cp < 0x3400:
		return "CJK Compatibility"
	case cp >= 0x4E00 && cp < 0xA000:
		return "CJK Unified Ideographs"
	case cp >= 0xAC00 && cp < 0xD7B0:
		return "Hangul Syllables"
	case cp >= 0xF900 && cp < 0xFB00:
		return "CJK Compatibility Ideographs"
	case cp >= 0xFF00 && cp < 0xFF60:
		return "Fullwidth Forms"
	case cp >= 0xFF60 && cp < 0xFFE0:
		return "Halfwidth Forms"
	case cp >= 0x1100 && cp < 0x1200:
		return "Hangul Jamo"
	case cp >= 0xF000 && cp < 0xF100:
		return "Private Use"
	case cp >= 0xFB00 && cp < 0xFB07:
		return "Alphabetic Presentation"
	default:
		return ""
	}
}
