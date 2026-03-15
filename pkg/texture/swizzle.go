package texture

// deswizzle converts NV2A Morton Z-order (swizzled) data to linear layout.
// bytesPerPixel is the pixel size (2 for R5G6B5, 4 for A8R8G8B8).
func deswizzle(data []byte, width, height, bytesPerPixel int) []byte {
	out := make([]byte, width*height*bytesPerPixel)

	for y := range height {
		for x := range width {
			srcIdx := mortonIndex(x, y) * bytesPerPixel
			dstIdx := (y*width + x) * bytesPerPixel
			if srcIdx+bytesPerPixel <= len(data) && dstIdx+bytesPerPixel <= len(out) {
				copy(out[dstIdx:dstIdx+bytesPerPixel], data[srcIdx:srcIdx+bytesPerPixel])
			}
		}
	}
	return out
}

// mortonIndex computes the Morton Z-order index for (x, y).
func mortonIndex(x, y int) int {
	return interleave(x) | (interleave(y) << 1)
}

// interleave spreads bits of v into even bit positions.
func interleave(v int) int {
	v = (v | (v << 8)) & 0x00FF00FF
	v = (v | (v << 4)) & 0x0F0F0F0F
	v = (v | (v << 2)) & 0x33333333
	v = (v | (v << 1)) & 0x55555555
	return v
}
