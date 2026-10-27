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
//	0x00  magic "XTF0" or "XTF1"
//	0x04  uint32 cell height
//	0x08  char[32] font name (null-terminated)
//	0x28  GLYPHSET:
//	  0x28  uint32 cbThis       (size of serialized GLYPHSET)
//	  0x2C  uint32 flAccel      (flags)
//	  0x30  uint32 cGlyphsSupported
//	  0x34  uint32 cRanges
//	  0x38  WCRANGE[cRanges]    (uint16 wcLow, uint16 cGlyphs each)
//	XTF1 only, uint16[cGlyphsSupported] with the offset of each glyph's mesh inside its decompressed chunk
//	Per-glyph table of cGlyphsSupported entries
//	  each GLYPHMETRICSFLOAT (6 float32) + uint32 mesh location, the file offset of the mesh in XTF0
//	  and of the chunk holding it in XTF1
//	XTF1 chunk of uint16 compressed size, uint16 decompressed size and an LZX stream with a 128K window
//	Mesh data
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

	if data[0] != 'X' || data[1] != 'T' || data[2] != 'F' || (data[3] != '0' && data[3] != '1') {
		return nil, ErrInvalidMagic
	}
	chunked := data[3] == '1'

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

	// The chunked format places each glyph inside its chunk with a word between the ranges and the metrics
	var chunkOffsets []uint16
	if chunked {
		if off+glyphCount*2 > len(data) {
			return nil, ErrTruncatedFile
		}
		chunkOffsets = make([]uint16, glyphCount)
		for i := range chunkOffsets {
			chunkOffsets[i] = binary.LittleEndian.Uint16(data[off+2*i:])
		}
		off += glyphCount * 2
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
	chunks := map[uint32][]byte{}
	windowBits := 0
	if chunked {
		var err error
		if windowBits, err = xtfWindowBits(data, glyphs); err != nil {
			return nil, err
		}
	}
	for i := range glyphs {
		mesh, at := data, int(glyphs[i].meshOff)
		if chunked {
			chunk, err := xtfChunk(data, glyphs[i].meshOff, windowBits, chunks)
			if err != nil {
				return nil, fmt.Errorf("font: glyph %d: %w", i, err)
			}
			mesh, at = chunk, int(chunkOffsets[i])
		} else if at == 0 {
			continue
		}
		if at+4 > len(mesh) {
			return nil, fmt.Errorf("font: glyph %d mesh at %#x lies outside its %d bytes", i, at, len(mesh))
		}
		icount := int(binary.LittleEndian.Uint16(mesh[at:]))
		vcount := int(binary.LittleEndian.Uint16(mesh[at+2:]))
		if icount == 0 || vcount == 0 {
			continue
		}

		idxStart := at + 4
		idxEnd := idxStart + icount*2
		vertStart := idxEnd
		vertEnd := vertStart + vcount*8
		if vertEnd > len(mesh) {
			return nil, fmt.Errorf("font: glyph %d mesh of %d indices and %d vertices runs past its %d bytes", i, icount, vcount, len(mesh))
		}

		indices := make([]uint16, icount)
		for j := range icount {
			indices[j] = binary.LittleEndian.Uint16(mesh[idxStart+j*2:])
			if int(indices[j]) >= vcount {
				return nil, fmt.Errorf("font: glyph %d index %d names vertex %d of %d", i, j, indices[j], vcount)
			}
		}

		vertices := make([][2]float32, vcount)
		for j := range vcount {
			voff := vertStart + j*8
			vertices[j][0] = math.Float32frombits(binary.LittleEndian.Uint32(mesh[voff:]))
			vertices[j][1] = math.Float32frombits(binary.LittleEndian.Uint32(mesh[voff+4:]))
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

// Finds the one LZX window every chunk of the font decodes through, trying each window the format allows
func xtfWindowBits(data []byte, glyphs []Glyph) (int, error) {
	offsets := map[uint32]bool{}
	for i := range glyphs {
		offsets[glyphs[i].meshOff] = true
	}
	var fits []int
	for bits := lzxMinWindowBits; bits <= lzxMaxWindowBits; bits++ {
		chunks := map[uint32][]byte{}
		decodes := true
		for off := range offsets {
			if _, err := xtfChunk(data, off, bits, chunks); err != nil {
				decodes = false
				break
			}
		}
		if decodes {
			fits = append(fits, bits)
		}
	}
	if len(fits) != 1 {
		return 0, fmt.Errorf("font: %d LZX windows decode every chunk (%v)", len(fits), fits)
	}
	return fits[0], nil
}

// Decompresses the chunk at a file offset once, a sized header leading its LZX stream
func xtfChunk(data []byte, off uint32, windowBits int, chunks map[uint32][]byte) ([]byte, error) {
	if chunk, done := chunks[off]; done {
		return chunk, nil
	}
	if int(off)+4 > len(data) {
		return nil, fmt.Errorf("chunk at %#x lies outside the file", off)
	}
	compressed := int(binary.LittleEndian.Uint16(data[off:]))
	decompressed := int(binary.LittleEndian.Uint16(data[off+2:]))
	start := int(off) + 4
	if start+compressed > len(data) {
		return nil, fmt.Errorf("chunk at %#x of %d bytes runs past the file", off, compressed)
	}
	chunk, err := lzxDecompress(data[start:start+compressed], decompressed, windowBits)
	if err != nil {
		return nil, fmt.Errorf("chunk at %#x: %w", off, err)
	}
	if len(chunk) != decompressed {
		return nil, fmt.Errorf("chunk at %#x decodes to %d bytes, not %d", off, len(chunk), decompressed)
	}
	chunks[off] = chunk
	return chunk, nil
}
