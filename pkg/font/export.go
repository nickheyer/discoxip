package font

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
)

// ExportGlyphAtlas writes the font's glyph bitmaps as a PNG atlas.
// If per-glyph metrics were parsed, each glyph is laid out at its correct
// dimensions. Otherwise, falls back to a raw byte dump.
func ExportGlyphAtlas(w io.Writer, f *Font, atlasWidth int) error {
	if f.BitmapOffset >= len(f.RawData) {
		return fmt.Errorf("font: bitmap offset 0x%X beyond file end", f.BitmapOffset)
	}

	if atlasWidth <= 0 {
		atlasWidth = 256
	}

	bitmapData := f.RawData[f.BitmapOffset:]

	// If we have per-glyph metrics, do a proper atlas layout
	if len(f.Glyphs) > 0 {
		return exportMetricAtlas(w, f, bitmapData, atlasWidth)
	}

	// Fallback: render raw bitmap data as grayscale scanlines
	return exportRawAtlas(w, bitmapData, atlasWidth)
}

// exportMetricAtlas lays out glyphs using their parsed metric records.
func exportMetricAtlas(w io.Writer, f *Font, bitmapData []byte, atlasWidth int) error {
	// First pass: compute atlas height by packing glyphs left-to-right
	type placed struct {
		x, y  int
		glyph GlyphMetric
	}
	var placements []placed

	curX, curY := 0, 0
	rowHeight := 0
	padding := 1

	for _, g := range f.Glyphs {
		if g.Width <= 0 || g.Height <= 0 {
			continue
		}
		// Wrap to next row if needed
		if curX+g.Width > atlasWidth {
			curX = 0
			curY += rowHeight + padding
			rowHeight = 0
		}
		placements = append(placements, placed{x: curX, y: curY, glyph: g})
		curX += g.Width + padding
		if g.Height > rowHeight {
			rowHeight = g.Height
		}
	}
	atlasHeight := curY + rowHeight
	if atlasHeight <= 0 {
		atlasHeight = 1
	}

	img := image.NewGray(image.Rect(0, 0, atlasWidth, atlasHeight))

	// Second pass: blit each glyph into the atlas
	for _, p := range placements {
		g := p.glyph
		if g.BitmapOffset < 0 || g.BitmapOffset >= len(bitmapData) {
			continue
		}

		glyphSize := g.Width * g.Height
		if g.BitmapOffset+glyphSize > len(bitmapData) {
			// Truncated — render what we can
			glyphSize = len(bitmapData) - g.BitmapOffset
		}

		for py := range g.Height {
			for px := range g.Width {
				srcIdx := g.BitmapOffset + py*g.Width + px
				if srcIdx >= g.BitmapOffset+glyphSize {
					break
				}
				img.SetGray(p.x+px, p.y+py, color.Gray{Y: bitmapData[srcIdx]})
			}
		}
	}

	return png.Encode(w, img)
}

// exportRawAtlas renders raw bitmap bytes as a grayscale image.
// Used when glyph metrics are not available.
func exportRawAtlas(w io.Writer, bitmapData []byte, width int) error {
	height := (len(bitmapData) + width - 1) / width
	img := image.NewGray(image.Rect(0, 0, width, height))
	for i, b := range bitmapData {
		x := i % width
		y := i / width
		img.SetGray(x, y, color.Gray{Y: b})
	}
	return png.Encode(w, img)
}
