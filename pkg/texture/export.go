package texture

import (
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
)

// Decode converts raw texture data into an RGBA image.
func Decode(tex *Texture) (*image.RGBA, error) {
	info := tex.Info
	w, h := info.Width, info.Height

	switch info.Format {
	case FormatDXT1:
		pixels := decodeDXT1(tex.Data, w, h)
		return pixelsToImage(pixels, w, h), nil

	case FormatDXT3:
		pixels := decodeDXT3(tex.Data, w, h)
		return pixelsToImage(pixels, w, h), nil

	case FormatDXT5:
		pixels := decodeDXT5(tex.Data, w, h)
		return pixelsToImage(pixels, w, h), nil

	case FormatA8R8G8B8, FormatX8R8G8B8:
		linear := deswizzle(tex.Data, w, h, 4)
		return decodeARGB32(linear, w, h, info.Format == FormatA8R8G8B8), nil

	case FormatR5G6B5:
		linear := deswizzle(tex.Data, w, h, 2)
		return decodeRGB565(linear, w, h), nil

	default:
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedFormat, info.Format)
	}
}

// ExportPNG decodes a texture and writes it as PNG.
func ExportPNG(w io.Writer, tex *Texture) error {
	img, err := Decode(tex)
	if err != nil {
		return err
	}
	return png.Encode(w, img)
}

func pixelsToImage(pixels []byte, w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	copy(img.Pix, pixels)
	return img
}

func decodeARGB32(data []byte, w, h int, hasAlpha bool) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < w*h; i++ {
		off := i * 4
		if off+4 > len(data) {
			break
		}
		// D3DFMT_A8R8G8B8 in little-endian memory: B, G, R, A
		b := data[off]
		g := data[off+1]
		r := data[off+2]
		a := data[off+3]
		if !hasAlpha {
			a = 255
		}
		img.SetRGBA(i%w, i/w, color.RGBA{R: r, G: g, B: b, A: a})
	}
	return img
}

func decodeRGB565(data []byte, w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < w*h; i++ {
		off := i * 2
		if off+2 > len(data) {
			break
		}
		c := binary.LittleEndian.Uint16(data[off:])
		rgba := rgb565ToRGBA(c)
		img.SetRGBA(i%w, i/w, color.RGBA{R: rgba[0], G: rgba[1], B: rgba[2], A: rgba[3]})
	}
	return img
}
