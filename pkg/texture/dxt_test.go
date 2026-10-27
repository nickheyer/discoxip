package texture

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// Packs 5 bit red, 6 bit green and 5 bit blue into an RGB565 word
func rgb565(r, g, b uint16) uint16 {
	return r<<11 | g<<5 | b
}

// Expands an RGB565 word to 8 bit channels by scaling each field to 255
func expand565(c uint16) [3]byte {
	return [3]byte{
		byte(int(c>>11&0x1F) * 255 / 31),
		byte(int(c>>5&0x3F) * 255 / 63),
		byte(int(c&0x1F) * 255 / 31),
	}
}

// Derives the four entry DXT color palette by the two thirds or one half blends
func dxtPalette(c0, c1 uint16) [4][4]byte {
	p0, p1 := expand565(c0), expand565(c1)
	var p [4][4]byte
	p[0] = [4]byte{p0[0], p0[1], p0[2], 255}
	p[1] = [4]byte{p1[0], p1[1], p1[2], 255}
	if c0 > c1 {
		for i := range 3 {
			p[2][i] = byte((2*int(p0[i]) + int(p1[i])) / 3)
			p[3][i] = byte((int(p0[i]) + 2*int(p1[i])) / 3)
		}
		p[2][3], p[3][3] = 255, 255
		return p
	}
	for i := range 3 {
		p[2][i] = byte((int(p0[i]) + int(p1[i])) / 2)
	}
	p[2][3] = 255
	return p
}

// Derives the four entry palette DXT3 and DXT5 color blocks always use regardless of endpoint order
func dxtFourColorPalette(c0, c1 uint16) [4][4]byte {
	p0, p1 := expand565(c0), expand565(c1)
	var p [4][4]byte
	p[0] = [4]byte{p0[0], p0[1], p0[2], 255}
	p[1] = [4]byte{p1[0], p1[1], p1[2], 255}
	for i := range 3 {
		p[2][i] = byte((2*int(p0[i]) + int(p1[i])) / 3)
		p[3][i] = byte((int(p0[i]) + 2*int(p1[i])) / 3)
	}
	p[2][3], p[3][3] = 255, 255
	return p
}

// Derives the eight entry DXT5 alpha palette by the sevenths or fifths blends
func dxt5AlphaPalette(a0, a1 byte) [8]byte {
	p := [8]byte{a0, a1}
	if a0 > a1 {
		for i := 2; i < 8; i++ {
			p[i] = byte(((8-i)*int(a0) + (i-1)*int(a1)) / 7)
		}
		return p
	}
	for i := 2; i < 6; i++ {
		p[i] = byte(((6-i)*int(a0) + (i-1)*int(a1)) / 5)
	}
	p[6], p[7] = 0, 255
	return p
}

// Encodes one DXT1 color block from two endpoints and sixteen 2 bit indices in raster order
func dxt1Block(c0, c1 uint16, idx [16]uint8) []byte {
	block := make([]byte, 8)
	binary.LittleEndian.PutUint16(block[0:], c0)
	binary.LittleEndian.PutUint16(block[2:], c1)
	var bits uint32
	for i, v := range idx {
		bits |= uint32(v&3) << (2 * i)
	}
	binary.LittleEndian.PutUint32(block[4:], bits)
	return block
}

// Encodes the DXT3 alpha block from sixteen 4 bit alpha values in raster order
func dxt3AlphaBlock(alpha [16]uint8) []byte {
	block := make([]byte, 8)
	for row := range 4 {
		var word uint16
		for col := range 4 {
			word |= uint16(alpha[row*4+col]&0xF) << (4 * col)
		}
		binary.LittleEndian.PutUint16(block[row*2:], word)
	}
	return block
}

// Encodes the DXT5 alpha block from two endpoints and sixteen 3 bit indices in raster order
func dxt5AlphaBlock(a0, a1 byte, idx [16]uint8) []byte {
	block := make([]byte, 8)
	block[0], block[1] = a0, a1
	var bits uint64
	for i, v := range idx {
		bits |= uint64(v&7) << (3 * i)
	}
	for i := range 6 {
		block[2+i] = byte(bits >> (8 * i))
	}
	return block
}

// Writes the palette entry each index selects into a width by height NRGBA pixel array
func paintBlock(out []byte, width, height, bx, by int, palette [4][4]byte, idx [16]uint8) {
	for row := range 4 {
		for col := range 4 {
			x, y := bx+col, by+row
			if x >= width || y >= height {
				continue
			}
			copy(out[(y*width+x)*4:], palette[idx[row*4+col]][:])
		}
	}
}

var rasterIndices = [16]uint8{0, 1, 2, 3, 1, 2, 3, 0, 2, 3, 0, 1, 3, 0, 1, 2}

func TestExpand565(t *testing.T) {
	cases := []struct {
		c    uint16
		want [3]byte
	}{
		{0x0000, [3]byte{0, 0, 0}},
		{0xFFFF, [3]byte{255, 255, 255}},
		{0xF800, [3]byte{255, 0, 0}},
		{0x07E0, [3]byte{0, 255, 0}},
		{0x001F, [3]byte{0, 0, 255}},
		{rgb565(16, 32, 16), [3]byte{131, 129, 131}},
		{rgb565(1, 1, 1), [3]byte{8, 4, 8}},
	}
	for _, c := range cases {
		if got := expand565(c.c); got != c.want {
			t.Errorf("expand565(%04X) = %v, want %v", c.c, got, c.want)
		}
		rgba := rgb565ToRGBA(c.c)
		if rgba != [4]byte{c.want[0], c.want[1], c.want[2], 255} {
			t.Errorf("rgb565ToRGBA(%04X) = %v, want %v with alpha 255", c.c, rgba, c.want)
		}
	}
}

func TestDXTPaletteLiterals(t *testing.T) {
	four := dxtPalette(0xF800, 0x001F)
	wantFour := [4][4]byte{{255, 0, 0, 255}, {0, 0, 255, 255}, {170, 0, 85, 255}, {85, 0, 170, 255}}
	if four != wantFour {
		t.Errorf("four color palette = %v, want %v", four, wantFour)
	}
	three := dxtPalette(0x001F, 0xF800)
	wantThree := [4][4]byte{{0, 0, 255, 255}, {255, 0, 0, 255}, {127, 0, 127, 255}, {0, 0, 0, 0}}
	if three != wantThree {
		t.Errorf("three color palette = %v, want %v", three, wantThree)
	}
	alpha := dxt5AlphaPalette(200, 40)
	wantAlpha := [8]byte{200, 40, 177, 154, 131, 108, 85, 62}
	if alpha != wantAlpha {
		t.Errorf("seven step alpha palette = %v, want %v", alpha, wantAlpha)
	}
	alpha = dxt5AlphaPalette(40, 200)
	wantAlpha = [8]byte{40, 200, 72, 104, 136, 168, 0, 255}
	if alpha != wantAlpha {
		t.Errorf("five step alpha palette = %v, want %v", alpha, wantAlpha)
	}
}

func TestDecodeDXT1FourColor(t *testing.T) {
	c0, c1 := rgb565(31, 0, 0), rgb565(0, 0, 31)
	got := decodeDXT1(dxt1Block(c0, c1, rasterIndices), 4, 4)
	want := make([]byte, 64)
	paintBlock(want, 4, 4, 0, 0, dxtPalette(c0, c1), rasterIndices)
	if !bytes.Equal(got, want) {
		t.Errorf("decodeDXT1 = %v, want %v", got, want)
	}
}

func TestDecodeDXT1ThreeColor(t *testing.T) {
	cases := []struct {
		name   string
		c0, c1 uint16
	}{
		{"c0 below c1", rgb565(0, 0, 31), rgb565(31, 0, 0)},
		{"equal endpoints", rgb565(0, 63, 0), rgb565(0, 63, 0)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := decodeDXT1(dxt1Block(c.c0, c.c1, rasterIndices), 4, 4)
			want := make([]byte, 64)
			paintBlock(want, 4, 4, 0, 0, dxtPalette(c.c0, c.c1), rasterIndices)
			if !bytes.Equal(got, want) {
				t.Errorf("decodeDXT1 = %v, want %v", got, want)
			}
			if got[3*4+3] != 0 || got[3*4] != 0 {
				t.Errorf("index 3 pixel = %v, want transparent black", got[12:16])
			}
		})
	}
}

func TestDecodeDXT1MultipleBlocks(t *testing.T) {
	c0, c1 := rgb565(31, 0, 0), rgb565(0, 0, 31)
	d0, d1 := rgb565(0, 63, 0), rgb565(31, 63, 31)
	second := [16]uint8{3, 3, 2, 2, 1, 1, 0, 0, 3, 2, 1, 0, 0, 1, 2, 3}
	data := append(dxt1Block(c0, c1, rasterIndices), dxt1Block(d0, d1, second)...)
	got := decodeDXT1(data, 8, 4)
	want := make([]byte, 8*4*4)
	paintBlock(want, 8, 4, 0, 0, dxtPalette(c0, c1), rasterIndices)
	paintBlock(want, 8, 4, 4, 0, dxtPalette(d0, d1), second)
	if !bytes.Equal(got, want) {
		t.Errorf("decodeDXT1 8x4 = %v, want %v", got, want)
	}

	tall := append(dxt1Block(c0, c1, rasterIndices), dxt1Block(d0, d1, second)...)
	got = decodeDXT1(tall, 4, 8)
	want = make([]byte, 4*8*4)
	paintBlock(want, 4, 8, 0, 0, dxtPalette(c0, c1), rasterIndices)
	paintBlock(want, 4, 8, 0, 4, dxtPalette(d0, d1), second)
	if !bytes.Equal(got, want) {
		t.Errorf("decodeDXT1 4x8 = %v, want %v", got, want)
	}
}

func TestDecodeDXT1TruncatedData(t *testing.T) {
	c0, c1 := rgb565(31, 0, 0), rgb565(0, 0, 31)
	got := decodeDXT1(dxt1Block(c0, c1, rasterIndices), 8, 4)
	want := make([]byte, 8*4*4)
	paintBlock(want, 8, 4, 0, 0, dxtPalette(c0, c1), rasterIndices)
	if !bytes.Equal(got, want) {
		t.Errorf("decodeDXT1 with one of two blocks = %v, want %v", got, want)
	}
}

func TestDecodeDXT1SmallTexture(t *testing.T) {
	c0, c1 := rgb565(31, 0, 0), rgb565(0, 0, 31)
	got := decodeDXT1(dxt1Block(c0, c1, rasterIndices), 2, 2)
	want := make([]byte, 2*2*4)
	paintBlock(want, 2, 2, 0, 0, dxtPalette(c0, c1), rasterIndices)
	if !bytes.Equal(got, want) {
		t.Errorf("decodeDXT1 2x2 = %v, want %v", got, want)
	}
	got = decodeDXT1(dxt1Block(c0, c1, rasterIndices), 1, 1)
	if !bytes.Equal(got, []byte{255, 0, 0, 255}) {
		t.Errorf("decodeDXT1 1x1 = %v, want the first palette entry", got)
	}
}

func TestDecodeDXT3(t *testing.T) {
	var alpha [16]uint8
	for i := range alpha {
		alpha[i] = uint8(i)
	}
	c0, c1 := rgb565(31, 0, 0), rgb565(0, 0, 31)
	data := append(dxt3AlphaBlock(alpha), dxt1Block(c0, c1, rasterIndices)...)
	got := decodeDXT3(data, 4, 4)
	want := make([]byte, 64)
	paintBlock(want, 4, 4, 0, 0, dxtFourColorPalette(c0, c1), rasterIndices)
	for i := range 16 {
		want[i*4+3] = alpha[i] * 17
	}
	if !bytes.Equal(got, want) {
		t.Errorf("decodeDXT3 = %v, want %v", got, want)
	}
}

func TestDecodeDXT3ColorIgnoresEndpointOrder(t *testing.T) {
	var alpha [16]uint8
	for i := range alpha {
		alpha[i] = 0xF
	}
	c0, c1 := rgb565(0, 0, 31), rgb565(31, 0, 0)
	data := append(dxt3AlphaBlock(alpha), dxt1Block(c0, c1, rasterIndices)...)
	got := decodeDXT3(data, 4, 4)
	want := make([]byte, 64)
	paintBlock(want, 4, 4, 0, 0, dxtFourColorPalette(c0, c1), rasterIndices)
	for i := range 16 {
		want[i*4+3] = 255
	}
	if !bytes.Equal(got, want) {
		t.Errorf("decodeDXT3 with c0 below c1 = %v, want four color palette %v", got, want)
	}
}

func TestDecodeDXT3TwoBlocks(t *testing.T) {
	var opaque, clear [16]uint8
	for i := range opaque {
		opaque[i] = 0xF
		clear[i] = 0x0
	}
	c0, c1 := rgb565(31, 0, 0), rgb565(0, 0, 31)
	block := dxt1Block(c0, c1, rasterIndices)
	data := append(append(dxt3AlphaBlock(opaque), block...), append(dxt3AlphaBlock(clear), block...)...)
	got := decodeDXT3(data, 8, 4)
	want := make([]byte, 8*4*4)
	paintBlock(want, 8, 4, 0, 0, dxtFourColorPalette(c0, c1), rasterIndices)
	paintBlock(want, 8, 4, 4, 0, dxtFourColorPalette(c0, c1), rasterIndices)
	for y := range 4 {
		for x := 4; x < 8; x++ {
			want[(y*8+x)*4+3] = 0
		}
	}
	if !bytes.Equal(got, want) {
		t.Errorf("decodeDXT3 8x4 = %v, want %v", got, want)
	}
}

func TestDecodeDXT5(t *testing.T) {
	idx := [16]uint8{0, 1, 2, 3, 4, 5, 6, 7, 7, 6, 5, 4, 3, 2, 1, 0}
	c0, c1 := rgb565(31, 0, 0), rgb565(0, 0, 31)
	cases := []struct {
		name   string
		a0, a1 byte
	}{
		{"seven step", 200, 40},
		{"five step", 40, 200},
		{"equal endpoints", 90, 90},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			data := append(dxt5AlphaBlock(c.a0, c.a1, idx), dxt1Block(c0, c1, rasterIndices)...)
			got := decodeDXT5(data, 4, 4)
			want := make([]byte, 64)
			paintBlock(want, 4, 4, 0, 0, dxtFourColorPalette(c0, c1), rasterIndices)
			palette := dxt5AlphaPalette(c.a0, c.a1)
			for i := range 16 {
				want[i*4+3] = palette[idx[i]]
			}
			if !bytes.Equal(got, want) {
				t.Errorf("decodeDXT5 = %v, want %v", got, want)
			}
		})
	}
}

func TestDecodeDXT5ColorIgnoresEndpointOrder(t *testing.T) {
	idx := [16]uint8{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}
	c0, c1 := rgb565(0, 0, 31), rgb565(31, 0, 0)
	data := append(dxt5AlphaBlock(255, 0, idx), dxt1Block(c0, c1, rasterIndices)...)
	got := decodeDXT5(data, 4, 4)
	want := make([]byte, 64)
	paintBlock(want, 4, 4, 0, 0, dxtFourColorPalette(c0, c1), rasterIndices)
	for i := range 16 {
		want[i*4+3] = 255
	}
	if !bytes.Equal(got, want) {
		t.Errorf("decodeDXT5 with c0 below c1 = %v, want four color palette %v", got, want)
	}
}

func TestDecodeDXT5SmallTexture(t *testing.T) {
	idx := [16]uint8{1, 2, 3, 4, 5, 6, 7, 0, 0, 0, 0, 0, 0, 0, 0, 0}
	c0, c1 := rgb565(31, 0, 0), rgb565(0, 0, 31)
	data := append(dxt5AlphaBlock(200, 40, idx), dxt1Block(c0, c1, rasterIndices)...)
	got := decodeDXT5(data, 2, 2)
	want := make([]byte, 16)
	paintBlock(want, 2, 2, 0, 0, dxtFourColorPalette(c0, c1), rasterIndices)
	palette := dxt5AlphaPalette(200, 40)
	want[3], want[7], want[11], want[15] = palette[idx[0]], palette[idx[1]], palette[idx[4]], palette[idx[5]]
	if !bytes.Equal(got, want) {
		t.Errorf("decodeDXT5 2x2 = %v, want %v", got, want)
	}
}
