package texture

import "fmt"

// PixelFormat identifies the NV2A texture color format.
type PixelFormat uint8

const (
	FormatR5G6B5   PixelFormat = 0x05
	FormatA8R8G8B8 PixelFormat = 0x06
	FormatX8R8G8B8 PixelFormat = 0x07
	FormatDXT1     PixelFormat = 0x0C
	FormatDXT3     PixelFormat = 0x0E
	FormatDXT5     PixelFormat = 0x0F
)

func (f PixelFormat) String() string {
	switch f {
	case FormatR5G6B5:
		return "R5G6B5"
	case FormatA8R8G8B8:
		return "A8R8G8B8"
	case FormatX8R8G8B8:
		return "X8R8G8B8"
	case FormatDXT1:
		return "DXT1"
	case FormatDXT3:
		return "DXT3"
	case FormatDXT5:
		return "DXT5"
	default:
		return fmt.Sprintf("unknown(0x%02X)", uint8(f))
	}
}

// BitsPerPixel returns the bits per pixel (for DXT, this is the average).
func (f PixelFormat) BitsPerPixel() int {
	switch f {
	case FormatDXT1:
		return 4 // 0.5 bytes/pixel
	case FormatDXT3, FormatDXT5:
		return 8 // 1 byte/pixel
	case FormatR5G6B5:
		return 16
	case FormatA8R8G8B8, FormatX8R8G8B8:
		return 32
	default:
		return 0
	}
}

// IsCompressed returns true for DXT block-compressed formats.
func (f PixelFormat) IsCompressed() bool {
	switch f {
	case FormatDXT1, FormatDXT3, FormatDXT5:
		return true
	default:
		return false
	}
}

// IsSwizzled returns true for uncompressed formats (NV2A Morton Z-order).
func (f PixelFormat) IsSwizzled() bool {
	return !f.IsCompressed() && f.BitsPerPixel() > 0
}

// DataSize returns the expected data size in bytes for given dimensions.
func (f PixelFormat) DataSize(width, height int) int {
	bpp := f.BitsPerPixel()
	if bpp == 0 {
		return 0
	}
	if f.IsCompressed() {
		// DXT blocks are 4x4
		bw := (width + 3) / 4
		bh := (height + 3) / 4
		blockSize := 8 // DXT1
		if f == FormatDXT3 || f == FormatDXT5 {
			blockSize = 16
		}
		return bw * bh * blockSize
	}
	return width * height * bpp / 8
}
