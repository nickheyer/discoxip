package texture

import (
	"bytes"
	"testing"
)

// Interleaves the bits of x and y into the Morton address NV2A stores the pixel at
func mortonIndex(x, y, width, height int) int {
	idx, out, xb, yb := 0, 0, 0, 0
	for 1<<xb < width || 1<<yb < height {
		if 1<<xb < width {
			idx |= (x >> xb & 1) << out
			xb++
			out++
		}
		if 1<<yb < height {
			idx |= (y >> yb & 1) << out
			yb++
			out++
		}
	}
	return idx
}

// Places each linear pixel at its Morton address so deswizzling recovers the linear image
func swizzlePixels(linear []byte, width, height, bpp int) []byte {
	out := make([]byte, len(linear))
	for y := range height {
		for x := range width {
			src := (y*width + x) * bpp
			dst := mortonIndex(x, y, width, height) * bpp
			copy(out[dst:dst+bpp], linear[src:src+bpp])
		}
	}
	return out
}

func TestMortonIndexIsBijective(t *testing.T) {
	for _, dim := range [][2]int{{1, 1}, {4, 4}, {8, 2}, {2, 8}, {16, 4}, {1, 8}} {
		w, h := dim[0], dim[1]
		seen := make([]bool, w*h)
		for y := range h {
			for x := range w {
				i := mortonIndex(x, y, w, h)
				if i < 0 || i >= w*h || seen[i] {
					t.Fatalf("%dx%d pixel (%d,%d) maps to %d, which is out of range or already taken", w, h, x, y, i)
				}
				seen[i] = true
			}
		}
	}
}

func TestBuildSwizzleMasks(t *testing.T) {
	cases := []struct {
		width, height int
		xMask, yMask  uint32
	}{
		{1, 1, 0, 0},
		{2, 1, 0b1, 0},
		{1, 2, 0, 0b1},
		{4, 4, 0b0101, 0b1010},
		{8, 2, 0b1101, 0b0010},
		{2, 8, 0b0001, 0b1110},
		{16, 4, 0b110101, 0b001010},
		{256, 256, 0x5555, 0xAAAA},
	}
	for _, c := range cases {
		x, y := buildSwizzleMasks(c.width, c.height)
		if x != c.xMask || y != c.yMask {
			t.Errorf("buildSwizzleMasks(%d, %d) = %b, %b, want %b, %b", c.width, c.height, x, y, c.xMask, c.yMask)
		}
	}
}

func TestFillPattern(t *testing.T) {
	cases := []struct {
		val  int
		mask uint32
		want uint32
	}{
		{0, 0b10101, 0},
		{0b111, 0b10101, 0b10101},
		{0b101, 0b10101, 0b10001},
		{0b11, 0b1101, 0b101},
		{0b100, 0b1101, 0b1000},
		{5, 0, 0},
		{7, 0b111, 0b111},
	}
	for _, c := range cases {
		if got := fillPattern(c.val, c.mask); got != c.want {
			t.Errorf("fillPattern(%b, %b) = %b, want %b", c.val, c.mask, got, c.want)
		}
	}
}

func TestDeswizzle(t *testing.T) {
	for _, dim := range [][2]int{{1, 1}, {4, 4}, {8, 2}, {2, 8}, {8, 8}, {16, 4}} {
		w, h := dim[0], dim[1]
		for _, bpp := range []int{1, 2, 4} {
			linear := make([]byte, w*h*bpp)
			for i := range linear {
				linear[i] = byte(i*7 + 3)
			}
			got := deswizzle(swizzlePixels(linear, w, h, bpp), w, h, bpp)
			if !bytes.Equal(got, linear) {
				t.Errorf("%dx%d bpp %d: deswizzle = %v, want %v", w, h, bpp, got, linear)
			}
		}
	}
}

func TestDeswizzleHandWrittenOrder(t *testing.T) {
	swizzled := []byte{0, 1, 4, 5, 2, 3, 6, 7, 8, 9, 12, 13, 10, 11, 14, 15}
	want := []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}
	if got := deswizzle(swizzled, 4, 4, 1); !bytes.Equal(got, want) {
		t.Errorf("deswizzle 4x4 = %v, want %v", got, want)
	}
}

func TestDeswizzleShortInput(t *testing.T) {
	got := deswizzle([]byte{0xAA, 0xBB}, 2, 2, 1)
	want := []byte{0xAA, 0xBB, 0, 0}
	if !bytes.Equal(got, want) {
		t.Errorf("deswizzle short = %v, want %v", got, want)
	}
}
