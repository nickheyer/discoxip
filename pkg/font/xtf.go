package font

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
)

var xtfMagic = [4]byte{'X', 'T', 'F', '1'}

// GlyphRange describes a contiguous range of Unicode codepoints.
type GlyphRange struct {
	Start uint16
	Count uint16
}

// Font holds parsed XTF font metadata.
type Font struct {
	Name        string
	GlyphCount  int
	Ranges      []GlyphRange
	MaxHeight   int
	BitmapOffset int
	FileSize    int64
	RawData     []byte
}

// ReadXTF parses an XTF font file from r with given size.
func ReadXTF(r io.Reader, size int64) (*Font, error) {
	if size < 0x38 {
		return nil, ErrTruncatedFile
	}

	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("font: reading: %w", err)
	}

	// Check magic
	if data[0] != 'X' || data[1] != 'T' || data[2] != 'F' || data[3] != '1' {
		return nil, ErrInvalidMagic
	}

	// Parse font name (null-terminated, starting at offset 8)
	nameEnd := 8
	for nameEnd < 0x28 && data[nameEnd] != 0 {
		nameEnd++
	}
	name := string(data[8:nameEnd])

	// Glyph count at 0x28
	glyphCount := int(binary.LittleEndian.Uint32(data[0x28:]))

	// Bitmap data offset at 0x30
	bitmapOffset := int(binary.LittleEndian.Uint32(data[0x30:]))

	// Max glyph height at 0x34
	maxHeight := int(binary.LittleEndian.Uint32(data[0x34:]))

	// Parse glyph ranges starting at 0x38
	var ranges []GlyphRange
	totalGlyphs := 0
	off := 0x38
	for off+4 <= len(data) {
		start := binary.LittleEndian.Uint16(data[off:])
		count := binary.LittleEndian.Uint16(data[off+2:])
		if start == 0 && count == 0 {
			break
		}
		// Sanity check: don't read too many ranges
		if count == 0 || (start == 0 && totalGlyphs > 0) {
			break
		}
		ranges = append(ranges, GlyphRange{Start: start, Count: count})
		totalGlyphs += int(count)
		off += 4

		// Limit to reasonable number of ranges
		if len(ranges) > 256 {
			break
		}
	}

	return &Font{
		Name:         name,
		GlyphCount:   glyphCount,
		Ranges:       ranges,
		MaxHeight:    maxHeight,
		BitmapOffset: bitmapOffset,
		FileSize:     size,
		RawData:      data,
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

// TotalGlyphs returns the total glyph count from ranges.
func (f *Font) TotalGlyphs() int {
	total := 0
	for _, r := range f.Ranges {
		total += int(r.Count)
	}
	return total
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
