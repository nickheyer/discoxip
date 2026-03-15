package texture

// deswizzle converts NV2A Morton Z-order (swizzled) data to linear layout.
// Handles non-square textures correctly by building dimension-specific bit masks.
// The NV2A GPU interleaves X and Y coordinate bits, but when one dimension is
// smaller than the other, it stops interleaving that dimension's bits and assigns
// the remaining bit positions to the larger dimension.
func deswizzle(data []byte, width, height, bytesPerPixel int) []byte {
	out := make([]byte, width*height*bytesPerPixel)
	xMask, yMask := buildSwizzleMasks(width, height)

	for y := range height {
		for x := range width {
			srcIdx := int(fillPattern(x, xMask)|fillPattern(y, yMask)) * bytesPerPixel
			dstIdx := (y*width + x) * bytesPerPixel
			if srcIdx+bytesPerPixel <= len(data) && dstIdx+bytesPerPixel <= len(out) {
				copy(out[dstIdx:dstIdx+bytesPerPixel], data[srcIdx:srcIdx+bytesPerPixel])
			}
		}
	}
	return out
}

// buildSwizzleMasks creates bit masks for X and Y coordinates.
// Bits are allocated alternating between dimensions. When one dimension
// runs out of bits (i >= that dimension's size), remaining bit positions
// go to the other dimension. This matches the NV2A hardware swizzle for
// non-square textures.
func buildSwizzleMasks(width, height int) (xMask, yMask uint32) {
	bit := uint32(1)
	for i := 1; i < width || i < height; i <<= 1 {
		if i < width {
			xMask |= bit
			bit <<= 1
		}
		if i < height {
			yMask |= bit
			bit <<= 1
		}
	}
	return
}

// fillPattern distributes the bits of val into the bit positions set in mask.
// Example: val=0b111, mask=0b10101 -> result=0b10101 (val bits fill mask positions).
func fillPattern(val int, mask uint32) uint32 {
	result := uint32(0)
	srcBit := uint32(1)
	for dstBit := uint32(1); dstBit <= mask; dstBit <<= 1 {
		if mask&dstBit != 0 {
			if uint32(val)&srcBit != 0 {
				result |= dstBit
			}
			srcBit <<= 1
		}
	}
	return result
}
