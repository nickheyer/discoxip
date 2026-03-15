package texture

import "encoding/binary"

// decodeDXT1 decompresses DXT1 data into RGBA pixels.
func decodeDXT1(data []byte, width, height int) []byte {
	bw := (width + 3) / 4
	bh := (height + 3) / 4
	out := make([]byte, width*height*4)

	for by := range bh {
		for bx := range bw {
			blockIdx := (by*bw + bx) * 8
			if blockIdx+8 > len(data) {
				continue
			}
			block := data[blockIdx : blockIdx+8]
			decodeDXT1Block(block, out, bx*4, by*4, width, height)
		}
	}
	return out
}

// decodeDXT3 decompresses DXT3 data into RGBA pixels.
func decodeDXT3(data []byte, width, height int) []byte {
	bw := (width + 3) / 4
	bh := (height + 3) / 4
	out := make([]byte, width*height*4)

	for by := range bh {
		for bx := range bw {
			blockIdx := (by*bw + bx) * 16
			if blockIdx+16 > len(data) {
				continue
			}
			alphaBlock := data[blockIdx : blockIdx+8]
			colorBlock := data[blockIdx+8 : blockIdx+16]

			// Decode color (same as DXT1 but always 4-color mode)
			decodeDXT1Block(colorBlock, out, bx*4, by*4, width, height)

			// Override alpha from explicit 4-bit alpha block
			for row := range 4 {
				py := by*4 + row
				if py >= height {
					break
				}
				alphaRow := binary.LittleEndian.Uint16(alphaBlock[row*2:])
				for col := range 4 {
					px := bx*4 + col
					if px >= width {
						continue
					}
					a := (alphaRow >> (col * 4)) & 0xF
					a = a | (a << 4) // expand 4-bit to 8-bit
					out[(py*width+px)*4+3] = byte(a)
				}
			}
		}
	}
	return out
}

// decodeDXT5 decompresses DXT5 data into RGBA pixels.
func decodeDXT5(data []byte, width, height int) []byte {
	bw := (width + 3) / 4
	bh := (height + 3) / 4
	out := make([]byte, width*height*4)

	for by := range bh {
		for bx := range bw {
			blockIdx := (by*bw + bx) * 16
			if blockIdx+16 > len(data) {
				continue
			}
			alphaBlock := data[blockIdx : blockIdx+8]
			colorBlock := data[blockIdx+8 : blockIdx+16]

			// Decode color
			decodeDXT1Block(colorBlock, out, bx*4, by*4, width, height)

			// Interpolated alpha
			a0 := alphaBlock[0]
			a1 := alphaBlock[1]

			var alphaTable [8]byte
			alphaTable[0] = a0
			alphaTable[1] = a1
			if a0 > a1 {
				alphaTable[2] = byte((6*int(a0) + 1*int(a1)) / 7)
				alphaTable[3] = byte((5*int(a0) + 2*int(a1)) / 7)
				alphaTable[4] = byte((4*int(a0) + 3*int(a1)) / 7)
				alphaTable[5] = byte((3*int(a0) + 4*int(a1)) / 7)
				alphaTable[6] = byte((2*int(a0) + 5*int(a1)) / 7)
				alphaTable[7] = byte((1*int(a0) + 6*int(a1)) / 7)
			} else {
				alphaTable[2] = byte((4*int(a0) + 1*int(a1)) / 5)
				alphaTable[3] = byte((3*int(a0) + 2*int(a1)) / 5)
				alphaTable[4] = byte((2*int(a0) + 3*int(a1)) / 5)
				alphaTable[5] = byte((1*int(a0) + 4*int(a1)) / 5)
				alphaTable[6] = 0
				alphaTable[7] = 255
			}

			// 48-bit alpha index block (6 bytes starting at offset 2)
			alphaBits := uint64(alphaBlock[2]) |
				uint64(alphaBlock[3])<<8 |
				uint64(alphaBlock[4])<<16 |
				uint64(alphaBlock[5])<<24 |
				uint64(alphaBlock[6])<<32 |
				uint64(alphaBlock[7])<<40

			for row := range 4 {
				py := by*4 + row
				if py >= height {
					break
				}
				for col := range 4 {
					px := bx*4 + col
					if px >= width {
						continue
					}
					idx := (alphaBits >> ((row*4 + col) * 3)) & 0x7
					out[(py*width+px)*4+3] = alphaTable[idx]
				}
			}
		}
	}
	return out
}

// decodeDXT1Block decodes a single DXT1 4x4 color block into the output buffer.
func decodeDXT1Block(block []byte, out []byte, bx, by, width, height int) {
	c0 := binary.LittleEndian.Uint16(block[0:2])
	c1 := binary.LittleEndian.Uint16(block[2:4])
	bits := binary.LittleEndian.Uint32(block[4:8])

	var colors [4][4]byte // [index][R,G,B,A]
	colors[0] = rgb565ToRGBA(c0)
	colors[1] = rgb565ToRGBA(c1)

	if c0 > c1 {
		// 4-color block
		colors[2] = [4]byte{
			byte((2*int(colors[0][0]) + int(colors[1][0])) / 3),
			byte((2*int(colors[0][1]) + int(colors[1][1])) / 3),
			byte((2*int(colors[0][2]) + int(colors[1][2])) / 3),
			255,
		}
		colors[3] = [4]byte{
			byte((int(colors[0][0]) + 2*int(colors[1][0])) / 3),
			byte((int(colors[0][1]) + 2*int(colors[1][1])) / 3),
			byte((int(colors[0][2]) + 2*int(colors[1][2])) / 3),
			255,
		}
	} else {
		// 3-color + transparent
		colors[2] = [4]byte{
			byte((int(colors[0][0]) + int(colors[1][0])) / 2),
			byte((int(colors[0][1]) + int(colors[1][1])) / 2),
			byte((int(colors[0][2]) + int(colors[1][2])) / 2),
			255,
		}
		colors[3] = [4]byte{0, 0, 0, 0}
	}

	for row := range 4 {
		py := by + row
		if py >= height {
			break
		}
		for col := range 4 {
			px := bx + col
			if px >= width {
				continue
			}
			idx := (bits >> (uint(row*4+col) * 2)) & 3
			c := colors[idx]
			off := (py*width + px) * 4
			out[off] = c[0]
			out[off+1] = c[1]
			out[off+2] = c[2]
			out[off+3] = c[3]
		}
	}
}

func rgb565ToRGBA(c uint16) [4]byte {
	r := byte(((c >> 11) & 0x1F) * 255 / 31)
	g := byte(((c >> 5) & 0x3F) * 255 / 63)
	b := byte((c & 0x1F) * 255 / 31)
	return [4]byte{r, g, b, 255}
}
