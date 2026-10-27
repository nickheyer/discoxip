package texture

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/png"
	"testing"
)

// Builds a width by height texture whose pixel (x, y) has channel values derived from its coordinates
func argbTexture(format PixelFormat, width, height int) (*Texture, []byte) {
	linear := make([]byte, width*height*4)
	want := make([]byte, width*height*4)
	for y := range height {
		for x := range width {
			r, g, b, a := byte(10*x+1), byte(10*y+2), byte(x+y), byte(100+4*x+y)
			i := (y*width + x) * 4
			linear[i], linear[i+1], linear[i+2], linear[i+3] = b, g, r, a
			if format == FormatX8R8G8B8 {
				a = 255
			}
			want[i], want[i+1], want[i+2], want[i+3] = r, g, b, a
		}
	}
	tex := &Texture{Info: TextureInfo{Width: width, Height: height, Format: format}, Data: swizzlePixels(linear, width, height, 4)}
	return tex, want
}

func TestDecodeA8R8G8B8(t *testing.T) {
	for _, dim := range [][2]int{{4, 4}, {8, 2}, {1, 1}} {
		tex, want := argbTexture(FormatA8R8G8B8, dim[0], dim[1])
		img, err := Decode(tex)
		if err != nil {
			t.Fatalf("Decode: %v", err)
		}
		if img.Rect != image.Rect(0, 0, dim[0], dim[1]) {
			t.Errorf("bounds = %v", img.Rect)
		}
		if !bytes.Equal(img.Pix, want) {
			t.Errorf("%dx%d Pix = %v, want %v", dim[0], dim[1], img.Pix, want)
		}
	}
}

func TestDecodeX8R8G8B8(t *testing.T) {
	tex, want := argbTexture(FormatX8R8G8B8, 4, 4)
	img, err := Decode(tex)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if !bytes.Equal(img.Pix, want) {
		t.Errorf("Pix = %v, want %v", img.Pix, want)
	}
	for i := 3; i < len(img.Pix); i += 4 {
		if img.Pix[i] != 255 {
			t.Fatalf("alpha at %d = %d, want 255", i, img.Pix[i])
		}
	}
}

func TestDecodeR5G6B5(t *testing.T) {
	width, height := 4, 2
	linear := make([]byte, width*height*2)
	want := make([]byte, width*height*4)
	for y := range height {
		for x := range width {
			c := rgb565(uint16(x*8), uint16(y*30+x), uint16(31-x*5))
			binary.LittleEndian.PutUint16(linear[(y*width+x)*2:], c)
			rgb := expand565(c)
			copy(want[(y*width+x)*4:], []byte{rgb[0], rgb[1], rgb[2], 255})
		}
	}
	tex := &Texture{Info: TextureInfo{Width: width, Height: height, Format: FormatR5G6B5}, Data: swizzlePixels(linear, width, height, 2)}
	img, err := Decode(tex)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if !bytes.Equal(img.Pix, want) {
		t.Errorf("Pix = %v, want %v", img.Pix, want)
	}
}

func TestDecodeR5G6B5Literal(t *testing.T) {
	tex := &Texture{Info: TextureInfo{Width: 2, Height: 1, Format: FormatR5G6B5}, Data: []byte{0x00, 0xF8, 0x1F, 0x00}}
	img, err := Decode(tex)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	want := []byte{255, 0, 0, 255, 0, 0, 255, 255}
	if !bytes.Equal(img.Pix, want) {
		t.Errorf("Pix = %v, want %v", img.Pix, want)
	}
}

func TestDecodeCompressedThroughTexture(t *testing.T) {
	c0, c1 := rgb565(31, 0, 0), rgb565(0, 0, 31)
	block := dxt1Block(c0, c1, rasterIndices)
	want := make([]byte, 64)
	paintBlock(want, 4, 4, 0, 0, dxtPalette(c0, c1), rasterIndices)

	dxt1 := &Texture{Info: TextureInfo{Width: 4, Height: 4, Format: FormatDXT1}, Data: block}
	img, err := Decode(dxt1)
	if err != nil {
		t.Fatalf("Decode DXT1: %v", err)
	}
	if !bytes.Equal(img.Pix, want) {
		t.Errorf("DXT1 Pix = %v, want %v", img.Pix, want)
	}

	var alpha [16]uint8
	for i := range alpha {
		alpha[i] = uint8(15 - i)
	}
	dxt3 := &Texture{Info: TextureInfo{Width: 4, Height: 4, Format: FormatDXT3}, Data: append(dxt3AlphaBlock(alpha), block...)}
	img, err = Decode(dxt3)
	if err != nil {
		t.Fatalf("Decode DXT3: %v", err)
	}
	for i := range 16 {
		want[i*4+3] = alpha[i] * 17
	}
	if !bytes.Equal(img.Pix, want) {
		t.Errorf("DXT3 Pix = %v, want %v", img.Pix, want)
	}

	idx := [16]uint8{7, 6, 5, 4, 3, 2, 1, 0, 0, 1, 2, 3, 4, 5, 6, 7}
	dxt5 := &Texture{Info: TextureInfo{Width: 4, Height: 4, Format: FormatDXT5}, Data: append(dxt5AlphaBlock(16, 240, idx), block...)}
	img, err = Decode(dxt5)
	if err != nil {
		t.Fatalf("Decode DXT5: %v", err)
	}
	palette := dxt5AlphaPalette(16, 240)
	for i := range 16 {
		want[i*4+3] = palette[idx[i]]
	}
	if !bytes.Equal(img.Pix, want) {
		t.Errorf("DXT5 Pix = %v, want %v", img.Pix, want)
	}
}

func TestDecodeFromXPRFile(t *testing.T) {
	tex, want := argbTexture(FormatA8R8G8B8, 4, 4)
	file := xprFile("XPR0", 4, formatRegister(FormatA8R8G8B8, 1, 2, 2), tex.Data)
	parsed, err := ReadXPR(bytes.NewReader(file), int64(len(file)))
	if err != nil {
		t.Fatalf("ReadXPR: %v", err)
	}
	img, err := Decode(parsed)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if !bytes.Equal(img.Pix, want) {
		t.Errorf("Pix = %v, want %v", img.Pix, want)
	}
}

func TestDecodeIgnoresMipData(t *testing.T) {
	tex, want := argbTexture(FormatA8R8G8B8, 4, 4)
	tex.Info.MipLevels = 3
	tex.Data = append(tex.Data, bytes.Repeat([]byte{0xEE}, 16+4)...)
	img, err := Decode(tex)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if !bytes.Equal(img.Pix, want) {
		t.Errorf("Pix = %v, want %v", img.Pix, want)
	}
}

func TestPixelsToImage(t *testing.T) {
	pixels := []byte{1, 2, 3, 4, 5, 6, 7, 8}
	img := pixelsToImage(pixels, 2, 1)
	if img.Rect != image.Rect(0, 0, 2, 1) || img.Stride != 8 {
		t.Errorf("image geometry = %v stride %d", img.Rect, img.Stride)
	}
	if !bytes.Equal(img.Pix, pixels) {
		t.Errorf("Pix = %v, want %v", img.Pix, pixels)
	}
	if got := img.NRGBAAt(1, 0); got != (color.NRGBA{R: 5, G: 6, B: 7, A: 8}) {
		t.Errorf("pixel (1,0) = %v", got)
	}
}

func TestExportPNG(t *testing.T) {
	c0, c1 := rgb565(31, 0, 0), rgb565(0, 0, 31)
	opaque := make([]byte, 64)
	paintBlock(opaque, 4, 4, 0, 0, dxtPalette(c0, c1), rasterIndices)
	argb, argbWant := argbTexture(FormatA8R8G8B8, 8, 2)
	cases := []struct {
		name string
		tex  *Texture
		want []byte
	}{
		{"DXT1", &Texture{Info: TextureInfo{Width: 4, Height: 4, Format: FormatDXT1}, Data: dxt1Block(c0, c1, rasterIndices)}, opaque},
		{"A8R8G8B8", argb, argbWant},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := ExportPNG(&buf, c.tex); err != nil {
				t.Fatalf("ExportPNG: %v", err)
			}
			if !bytes.HasPrefix(buf.Bytes(), []byte("\x89PNG\r\n\x1a\n")) {
				t.Fatalf("output does not start with the PNG signature")
			}
			decoded, err := png.Decode(bytes.NewReader(buf.Bytes()))
			if err != nil {
				t.Fatalf("png.Decode: %v", err)
			}
			w, h := c.tex.Info.Width, c.tex.Info.Height
			if decoded.Bounds() != image.Rect(0, 0, w, h) {
				t.Fatalf("bounds = %v, want %dx%d", decoded.Bounds(), w, h)
			}
			for y := range h {
				for x := range w {
					got := color.NRGBAModel.Convert(decoded.At(x, y)).(color.NRGBA)
					i := (y*w + x) * 4
					want := color.NRGBA{R: c.want[i], G: c.want[i+1], B: c.want[i+2], A: c.want[i+3]}
					if got != want {
						t.Errorf("pixel (%d,%d) = %v, want %v", x, y, got, want)
					}
				}
			}
		})
	}
}

type failingWriter struct{ err error }

func (f failingWriter) Write([]byte) (int, error) { return 0, f.err }

func TestExportPNGWriteError(t *testing.T) {
	tex, _ := argbTexture(FormatA8R8G8B8, 4, 4)
	wantErr := errors.New("disk full")
	if err := ExportPNG(failingWriter{err: wantErr}, tex); !errors.Is(err, wantErr) {
		t.Fatalf("ExportPNG err = %v, want %v", err, wantErr)
	}
}
