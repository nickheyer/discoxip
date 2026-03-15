package font

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
)

// ExportGlyphAtlas writes a visual representation of the glyph bitmap data as PNG.
// This is a best-effort decode — the exact bitmap format is not fully documented.
func ExportGlyphAtlas(w io.Writer, f *Font, width int) error {
	if f.BitmapOffset >= len(f.RawData) {
		return fmt.Errorf("font: bitmap offset 0x%X beyond file end", f.BitmapOffset)
	}

	bitmapData := f.RawData[f.BitmapOffset:]
	if width <= 0 {
		width = 256
	}

	// Render raw bitmap data as grayscale — each byte is a pixel
	height := (len(bitmapData) + width - 1) / width

	img := image.NewGray(image.Rect(0, 0, width, height))
	for i, b := range bitmapData {
		x := i % width
		y := i / width
		img.SetGray(x, y, color.Gray{Y: b})
	}

	return png.Encode(w, img)
}
