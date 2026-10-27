package texture

import "testing"

func TestPixelFormatProperties(t *testing.T) {
	cases := []struct {
		format     PixelFormat
		name       string
		bpp        int
		compressed bool
		swizzled   bool
	}{
		{FormatR5G6B5, "R5G6B5", 16, false, true},
		{FormatA8R8G8B8, "A8R8G8B8", 32, false, true},
		{FormatX8R8G8B8, "X8R8G8B8", 32, false, true},
		{FormatDXT1, "DXT1", 4, true, false},
		{FormatDXT3, "DXT3", 8, true, false},
		{FormatDXT5, "DXT5", 8, true, false},
		{PixelFormat(0x00), "unknown(0x00)", 0, false, false},
		{PixelFormat(0x3A), "unknown(0x3A)", 0, false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.format.String(); got != c.name {
				t.Errorf("String() = %q, want %q", got, c.name)
			}
			if got := c.format.BitsPerPixel(); got != c.bpp {
				t.Errorf("BitsPerPixel() = %d, want %d", got, c.bpp)
			}
			if got := c.format.IsCompressed(); got != c.compressed {
				t.Errorf("IsCompressed() = %v, want %v", got, c.compressed)
			}
			if got := c.format.IsSwizzled(); got != c.swizzled {
				t.Errorf("IsSwizzled() = %v, want %v", got, c.swizzled)
			}
		})
	}
}

func TestPixelFormatDataSize(t *testing.T) {
	cases := []struct {
		format        PixelFormat
		width, height int
		want          int
	}{
		{FormatR5G6B5, 8, 4, 64},
		{FormatR5G6B5, 1, 1, 2},
		{FormatA8R8G8B8, 8, 4, 128},
		{FormatA8R8G8B8, 256, 256, 262144},
		{FormatX8R8G8B8, 2, 2, 16},
		{FormatDXT1, 4, 4, 8},
		{FormatDXT1, 8, 4, 16},
		{FormatDXT1, 1, 1, 8},
		{FormatDXT1, 5, 5, 32},
		{FormatDXT1, 64, 32, 1024},
		{FormatDXT3, 4, 4, 16},
		{FormatDXT3, 8, 4, 32},
		{FormatDXT3, 2, 2, 16},
		{FormatDXT5, 4, 4, 16},
		{FormatDXT5, 16, 16, 256},
		{PixelFormat(0x00), 16, 16, 0},
	}
	for _, c := range cases {
		if got := c.format.DataSize(c.width, c.height); got != c.want {
			t.Errorf("%s.DataSize(%d, %d) = %d, want %d", c.format, c.width, c.height, got, c.want)
		}
	}
}
