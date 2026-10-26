package bios

import (
	"encoding/binary"
	"fmt"
)

// The window the 2BL decompresses the kernel through and the chunk each framed block expands to
const (
	lzxWindowSize     = 128 * 1024
	lzxChunkSize      = 32 * 1024
	lzxMaxGrowth      = 6144
	lzxMinMatch       = 2
	lzxMaxMatch       = lzxMinMatch + 255
	lzxNumPrimaryLen  = 7
	lzxNumSecondary   = (lzxMaxMatch - lzxMinMatch + 1) - lzxNumPrimaryLen
	lzxAlignedBits    = 7
	lzxAlignedCount   = 8
	lzxMaxMainTree    = 700
	mainTreeTableBits = 10
	lenTreeTableBits  = 8
	preTreeTableBits  = 8
	preTreeCount      = 20
	e8FrameThreshold  = 32768
)

// Block types of the LZX stream
const (
	blockVerbatim     = 1
	blockAligned      = 2
	blockUncompressed = 3
)

var lzxExtraBits = [51]uint8{
	0, 0, 0, 0, 1, 1, 2, 2, 3, 3, 4, 4, 5, 5, 6, 6, 7, 7, 8, 8, 9, 9, 10, 10, 11, 11, 12, 12, 13, 13, 14, 14,
	15, 15, 16, 16, 17, 17, 17, 17, 17, 17, 17, 17, 17, 17, 17, 17, 17, 17, 17,
}

var lzxPositionBase = [51]int64{
	0, 1, 2, 3, 4, 6, 8, 12, 16, 24, 32, 48, 64, 96, 128, 192, 256, 384, 512, 768, 1024, 1536, 2048, 3072,
	4096, 6144, 8192, 12288, 16384, 24576, 32768, 49152, 65536, 98304, 131072, 196608, 262144, 393216, 524288, 655360,
	786432, 917504, 1048576, 1179648, 1310720, 1441792, 1572864, 1703936, 1835008, 1966080, 2097152,
}

// lzxDecoder holds the window and the Huffman trees across the framed blocks of one kernel image
type lzxDecoder struct {
	window        []byte
	windowMask    int64
	lastOffsets   [3]uint32
	positionSlots int
	mainTable     [1 << mainTreeTableBits]int16
	mainLen       [lzxMaxMainTree]uint8
	mainPrevLen   [lzxMaxMainTree]uint8
	mainLeftRight [lzxMaxMainTree * 4]int16
	lenTable      [1 << lenTreeTableBits]int16
	lenLen        [lzxNumSecondary]uint8
	lenPrevLen    [lzxNumSecondary]uint8
	lenLeftRight  [lzxNumSecondary * 4]int16
	alignedTable  [1 << lzxAlignedBits]uint8
	alignedLen    [lzxAlignedCount]uint8
	input         []byte
	inputPos      int
	inputEnd      int
	bitbuf        uint32
	bitcount      int
	pos           int64
	fileSize      uint32
	instrPos      uint32
	frames        uint32
	blockRemain   int64
	blockOriginal int64
	blockType     int
	newBlock      bool
	firstBlock    bool
	failed        bool
}

func newLzxDecoder() *lzxDecoder {
	d := &lzxDecoder{window: make([]byte, lzxWindowSize+lzxMaxMatch+4), windowMask: lzxWindowSize - 1}
	slots, start := 4, int64(4)
	for {
		start += int64(1) << lzxExtraBits[slots]
		slots++
		if start >= lzxWindowSize {
			break
		}
	}
	d.positionSlots = slots
	d.lastOffsets = [3]uint32{1, 1, 1}
	d.newBlock = true
	d.firstBlock = true
	return d
}

// Reads the framed blocks of a compressed kernel, each headed by its compressed and expanded sizes
func lzxDecompress(src []byte) ([]byte, error) {
	d := newLzxDecoder()
	var out []byte
	for off := 0; off < len(src); {
		if off+4 > len(src) {
			return nil, fmt.Errorf("lzx: block header at %d runs past the %d byte stream", off, len(src))
		}
		compressed := int(binary.LittleEndian.Uint16(src[off:]))
		expanded := int(binary.LittleEndian.Uint16(src[off+2:]))
		off += 4
		if off+compressed > len(src) {
			return nil, fmt.Errorf("lzx: block at %d claims %d bytes of the %d left", off, compressed, len(src)-off)
		}
		if compressed > lzxChunkSize+lzxMaxGrowth || expanded > lzxChunkSize {
			return nil, fmt.Errorf("lzx: block at %d is %d bytes expanding to %d", off, compressed, expanded)
		}
		chunk, err := d.decodeBlock(src[off:off+compressed], expanded)
		if err != nil {
			return nil, fmt.Errorf("lzx: block at %d: %w", off, err)
		}
		out = append(out, chunk...)
		off += compressed
	}
	return out, nil
}

// Decodes one framed block into the window and returns the bytes it expanded to, translated where the stream asks
func (d *lzxDecoder) decodeBlock(src []byte, expanded int) ([]byte, error) {
	// The decoder reads two bytes at a time, and the reference pads its input by four bytes past the data
	d.input = make([]byte, len(src)+4)
	copy(d.input, src)
	d.inputPos = 0
	d.inputEnd = len(src) + 4
	d.failed = false
	d.initBitbuf()
	total, err := d.decodeData(expanded)
	if err != nil {
		return nil, err
	}
	d.frames++
	at := d.pos
	if at == 0 {
		at = lzxWindowSize
	}
	out := make([]byte, total)
	copy(out, d.window[at-int64(total):at])
	if d.fileSize != 0 && d.frames-1 < e8FrameThreshold {
		d.translateE8(out)
	}
	return out, nil
}

func (d *lzxDecoder) initBitbuf() {
	if d.blockType == blockUncompressed {
		return
	}
	if d.inputPos+4 > d.inputEnd {
		return
	}
	p := d.input[d.inputPos:]
	d.bitbuf = (uint32(p[2]) | uint32(p[3])<<8) | (uint32(p[0])|uint32(p[1])<<8)<<16
	d.bitcount = 16
	d.inputPos += 4
}

// Shifts n bits out of the buffer and refills it from the input two bytes at a time
func (d *lzxDecoder) fill(n int) {
	d.bitbuf <<= uint(n)
	d.bitcount -= n
	for d.bitcount <= 0 {
		if d.inputPos >= d.inputEnd {
			d.failed = true
			return
		}
		d.bitbuf |= (uint32(d.input[d.inputPos]) | uint32(d.input[d.inputPos+1])<<8) << uint(-d.bitcount)
		d.inputPos += 2
		d.bitcount += 16
	}
}

func (d *lzxDecoder) getBits(n int) uint32 {
	v := d.bitbuf >> uint(32-n)
	d.fill(n)
	return v
}

// Builds a lookup table and the overflow tree for a canonical Huffman code, false where the lengths make none
func makeTable(nchar int, bitlen []uint8, tablebits int, table []int16, leftright []int16) bool {
	var count [17]uint32
	var weight [17]uint32
	var start [18]uint32
	for i := 0; i < nchar; i++ {
		count[bitlen[i]]++
	}
	for i := 1; i <= 16; i++ {
		start[i+1] = start[i] + (count[i] << uint(16-i))
	}
	if start[17] != 65536 {
		if start[17] == 0 {
			for i := range table[:1<<tablebits] {
				table[i] = 0
			}
			return true
		}
		return false
	}
	jutbits := 16 - tablebits
	i := 1
	for ; i <= tablebits; i++ {
		start[i] >>= uint(jutbits)
		weight[i] = 1 << uint(tablebits-i)
	}
	for ; i <= 16; i++ {
		weight[i] = 1 << uint(16-i)
	}
	i = int(start[tablebits+1] >> uint(jutbits))
	if i != 65536 {
		for k := i; k < 1<<tablebits; k++ {
			table[k] = 0
		}
	}
	avail := nchar
	for ch := 0; ch < nchar; ch++ {
		length := int(bitlen[ch])
		if length == 0 {
			continue
		}
		nextcode := start[length] + weight[length]
		if length <= tablebits {
			if nextcode > uint32(1)<<uint(tablebits) {
				return false
			}
			for k := start[length]; k < nextcode; k++ {
				table[k] = int16(ch)
			}
			start[length] = nextcode
			continue
		}
		k := start[length]
		start[length] = nextcode
		p := &table[k>>uint(jutbits)]
		depth := length - tablebits
		k <<= uint(tablebits)
		for depth > 0 {
			if *p == 0 {
				leftright[avail*2] = 0
				leftright[avail*2+1] = 0
				*p = int16(-avail)
				avail++
			}
			if int16(k) < 0 {
				p = &leftright[-int(*p)*2+1]
			} else {
				p = &leftright[-int(*p)*2]
			}
			k <<= 1
			depth--
		}
		*p = int16(ch)
	}
	return true
}

// Builds the eight entry aligned offset table, whose codes are at most seven bits
func makeAlignedTable(bitlen []uint8, table []uint8) bool {
	var count [17]uint16
	var weight [17]uint16
	var start [18]uint16
	for i := 0; i < 8; i++ {
		count[bitlen[i]]++
	}
	for i := 1; i <= 16; i++ {
		start[i+1] = start[i] + (count[i] << uint(16-i))
	}
	if start[17] != 0 {
		return false
	}
	i := 1
	for ; i <= 7; i++ {
		start[i] >>= 9
		weight[i] = 1 << uint(7-i)
	}
	for ; i <= 16; i++ {
		weight[i] = 1 << uint(16-i)
	}
	for k := range table {
		table[k] = 0
	}
	for ch := 0; ch < 8; ch++ {
		length := int(bitlen[ch])
		if length == 0 {
			continue
		}
		nextcode := start[length] + weight[length]
		if nextcode > 1<<7 {
			return false
		}
		for k := start[length]; k < nextcode; k++ {
			table[k] = uint8(ch)
		}
		start[length] = nextcode
	}
	return true
}

// Decodes one symbol of the pretree the path lengths are coded with
func (d *lzxDecoder) decodeSmall(table []int16, bitlen []uint8, leftright []int16) int {
	t := int(table[d.bitbuf>>(32-preTreeTableBits)])
	if t < 0 {
		mask := uint32(1) << (32 - 1 - preTreeTableBits)
		for t < 0 {
			t = -t
			if d.bitbuf&mask != 0 {
				t = int(leftright[2*t+1])
			} else {
				t = int(leftright[2*t])
			}
			mask >>= 1
		}
	}
	d.fill(int(bitlen[t]))
	return t
}

// Reads a tree's path lengths as differences from the previous block's, run length coded through a pretree
func (d *lzxDecoder) readRepTree(count int, lastlen, length []uint8) bool {
	var smallBitlen [24]uint8
	var smallTable [1 << preTreeTableBits]int16
	var leftright [2 * (2*24 + 241)]int16
	for i := 0; i < preTreeCount; i++ {
		smallBitlen[i] = uint8(d.getBits(4))
	}
	if d.failed {
		return false
	}
	if !makeTable(preTreeCount, smallBitlen[:], preTreeTableBits, smallTable[:], leftright[:]) {
		return false
	}
	for i := 0; i < count; i++ {
		t := d.decodeSmall(smallTable[:], smallBitlen[:], leftright[:])
		if d.failed {
			return false
		}
		switch t {
		case 17:
			run := int(d.getBits(4)) + 4
			if i+run >= count {
				run = count - i
			}
			for ; run > 0; run-- {
				length[i] = 0
				i++
			}
			i--
		case 18:
			run := int(d.getBits(5)) + 4 + 16
			if i+run >= count {
				run = count - i
			}
			for ; run > 0; run-- {
				length[i] = 0
				i++
			}
			i--
		case 19:
			run := int(d.getBits(1)) + 4
			if i+run >= count {
				run = count - i
			}
			t = d.decodeSmall(smallTable[:], smallBitlen[:], leftright[:])
			value := uint8((int(lastlen[i]) - t + 17) % 17)
			for ; run > 0; run-- {
				length[i] = value
				i++
			}
			i--
		default:
			length[i] = uint8((int(lastlen[i]) - t + 17) % 17)
		}
	}
	return !d.failed
}

func (d *lzxDecoder) readMainAndLengthTrees() bool {
	mainCount := 256 + d.positionSlots*(lzxNumPrimaryLen+1)
	if !d.readRepTree(256, d.mainPrevLen[:], d.mainLen[:]) {
		return false
	}
	if !d.readRepTree(d.positionSlots*(lzxNumPrimaryLen+1), d.mainPrevLen[256:], d.mainLen[256:]) {
		return false
	}
	if !makeTable(mainCount, d.mainLen[:], mainTreeTableBits, d.mainTable[:], d.mainLeftRight[:]) {
		return false
	}
	if !d.readRepTree(lzxNumSecondary, d.lenPrevLen[:], d.lenLen[:]) {
		return false
	}
	return makeTable(lzxNumSecondary, d.lenLen[:], lenTreeTableBits, d.lenTable[:], d.lenLeftRight[:])
}

func (d *lzxDecoder) readAlignedTree() bool {
	for i := 0; i < 8; i++ {
		d.alignedLen[i] = uint8(d.getBits(3))
	}
	if d.failed {
		return false
	}
	return makeAlignedTable(d.alignedLen[:], d.alignedTable[:])
}

// Decodes a main tree symbol, false when the input ran out under it
func (d *lzxDecoder) decodeMain() (int, bool) {
	c := int(d.mainTable[d.bitbuf>>(32-mainTreeTableBits)])
	if c < 0 {
		mask := uint32(1) << (32 - 1 - mainTreeTableBits)
		for c < 0 {
			c = -c
			if d.bitbuf&mask != 0 {
				c = int(d.mainLeftRight[c*2+1])
			} else {
				c = int(d.mainLeftRight[c*2])
			}
			mask >>= 1
		}
	}
	if d.inputPos >= d.inputEnd {
		return 0, false
	}
	n := int(d.mainLen[c])
	d.bitbuf <<= uint(n)
	d.bitcount -= n
	if d.bitcount <= 0 {
		d.bitbuf |= (uint32(d.input[d.inputPos]) | uint32(d.input[d.inputPos+1])<<8) << uint(-d.bitcount)
		d.inputPos += 2
		d.bitcount += 16
	}
	return c, true
}

func (d *lzxDecoder) decodeLength() int {
	m := int(d.lenTable[d.bitbuf>>(32-lenTreeTableBits)])
	if m < 0 {
		mask := uint32(1) << (32 - 1 - lenTreeTableBits)
		for m < 0 {
			m = -m
			if d.bitbuf&mask != 0 {
				m = int(d.lenLeftRight[m*2+1])
			} else {
				m = int(d.lenLeftRight[m*2])
			}
			mask >>= 1
		}
	}
	d.fill(int(d.lenLen[m]))
	return m + lzxNumPrimaryLen
}

func (d *lzxDecoder) decodeAligned() uint32 {
	j := d.alignedTable[d.bitbuf>>(32-lzxAlignedBits)]
	d.fill(int(d.alignedLen[j]))
	return uint32(j)
}

// Decodes literals and matches of a verbatim or aligned block into the window, the first 257 bytes mirrored past
// the window's end the way the reference keeps them for wrapped matches
func (d *lzxDecoder) decodeMatches(amount int64, aligned bool) error {
	pos := d.pos
	end := pos + amount
	for pos < end {
		c, ok := d.decodeMain()
		if !ok {
			return fmt.Errorf("input ran out inside a block")
		}
		c -= 256
		if c < 0 {
			d.window[pos] = byte(c)
			if pos < 257 {
				d.window[lzxWindowSize+pos] = byte(c)
			}
			pos++
			continue
		}
		matchLength := c & lzxNumPrimaryLen
		if matchLength == lzxNumPrimaryLen {
			matchLength = d.decodeLength()
		}
		m := c >> 3
		var matchPos uint32
		if m > 2 {
			switch {
			case aligned && lzxExtraBits[m] >= 3:
				var high uint32
				if lzxExtraBits[m]-3 > 0 {
					high = d.getBits(int(lzxExtraBits[m]) - 3)
				}
				matchPos = uint32(lzxPositionBase[m]-2) + high<<3
				matchPos += d.decodeAligned()
			case aligned:
				if lzxExtraBits[m] != 0 {
					matchPos = d.getBits(int(lzxExtraBits[m])) + uint32(lzxPositionBase[m]-2)
				} else {
					matchPos = uint32(lzxPositionBase[m] - 2)
					if pos < 257 {
						matchPos = 1
					}
				}
			case m > 3:
				matchPos = d.getBits(int(lzxExtraBits[m])) + uint32(lzxPositionBase[m]-2)
			default:
				matchPos = 1
			}
			d.lastOffsets[2] = d.lastOffsets[1]
			d.lastOffsets[1] = d.lastOffsets[0]
			d.lastOffsets[0] = matchPos
		} else {
			matchPos = d.lastOffsets[m]
			if m != 0 {
				d.lastOffsets[m] = d.lastOffsets[0]
				d.lastOffsets[0] = matchPos
			}
		}
		matchLength += 2
		for ; matchLength > 0; matchLength-- {
			d.window[pos] = d.window[(pos-int64(matchPos))&d.windowMask]
			if pos < 257 {
				d.window[lzxWindowSize+pos] = d.window[pos]
			}
			pos++
		}
	}
	d.pos = pos & d.windowMask
	return nil
}

// Copies the bytes of an uncompressed block into the window
func (d *lzxDecoder) decodeUncompressed(amount int64) error {
	pos := d.pos
	start := pos
	end := pos + amount
	for pos < end {
		if d.inputPos >= d.inputEnd {
			return fmt.Errorf("input ran out inside an uncompressed block")
		}
		d.window[pos] = d.input[d.inputPos]
		d.inputPos++
		pos++
	}
	mirror := end
	if mirror > 257 {
		mirror = 257
	}
	for ; start < mirror; start++ {
		d.window[lzxWindowSize+start] = d.window[start]
	}
	d.pos = pos & d.windowMask
	return nil
}

// Decodes the blocks a chunk spans, reading each block header as it is reached
func (d *lzxDecoder) decodeData(bytes int) (int, error) {
	total := 0
	remaining := int64(bytes)
	for remaining > 0 {
		if d.newBlock {
			if d.firstBlock {
				d.firstBlock = false
				if d.getBits(1) != 0 {
					high := d.getBits(16)
					low := d.getBits(16)
					d.fileSize = high<<16 | low
				} else {
					d.fileSize = 0
				}
			}
			if d.blockType == blockUncompressed {
				if d.blockOriginal&1 != 0 && d.inputPos < d.inputEnd {
					d.inputPos++
				}
				d.blockType = 0
				d.initBitbuf()
			}
			d.blockType = int(d.getBits(3))
			size := d.getBits(8)<<16 | d.getBits(8)<<8 | d.getBits(8)
			d.blockOriginal = int64(size)
			d.blockRemain = int64(size)
			switch d.blockType {
			case blockAligned:
				if !d.readAlignedTree() {
					return 0, fmt.Errorf("aligned offset tree is malformed")
				}
				fallthrough
			case blockVerbatim:
				copy(d.mainPrevLen[:], d.mainLen[:])
				copy(d.lenPrevLen[:], d.lenLen[:])
				if !d.readMainAndLengthTrees() {
					return 0, fmt.Errorf("main or length tree is malformed")
				}
			case blockUncompressed:
				d.inputPos -= 2
				if d.inputPos+4 >= d.inputEnd {
					return 0, fmt.Errorf("uncompressed block header runs past the input")
				}
				for i := 0; i < 3; i++ {
					d.lastOffsets[i] = binary.LittleEndian.Uint32(d.input[d.inputPos:])
					d.inputPos += 4
				}
			default:
				return 0, fmt.Errorf("block type %d", d.blockType)
			}
			d.newBlock = false
		}
		for d.blockRemain > 0 && remaining > 0 {
			amount := d.blockRemain
			if remaining < amount {
				amount = remaining
			}
			var err error
			switch d.blockType {
			case blockAligned:
				err = d.decodeMatches(amount, true)
			case blockVerbatim:
				err = d.decodeMatches(amount, false)
			case blockUncompressed:
				err = d.decodeUncompressed(amount)
			}
			if err != nil {
				return 0, err
			}
			if d.failed {
				return 0, fmt.Errorf("input ran out")
			}
			d.blockRemain -= amount
			remaining -= amount
			total += int(amount)
		}
		if d.blockRemain == 0 {
			d.newBlock = true
		}
		if remaining == 0 {
			d.initBitbuf()
		}
	}
	return total, nil
}

// Turns the absolute call targets the compressor wrote back into the relative ones the code holds
func (d *lzxDecoder) translateE8(mem []byte) {
	n := len(mem)
	if n <= 6 {
		d.instrPos += uint32(n)
		return
	}
	var saved [6]byte
	copy(saved[:], mem[n-6:])
	for i := n - 6; i < n; i++ {
		mem[i] = 0xE8
	}
	end := d.instrPos + uint32(n) - 10
	i := 0
	for {
		for mem[i] != 0xE8 {
			i++
			d.instrPos++
		}
		i++
		if d.instrPos >= end {
			break
		}
		absolute := binary.LittleEndian.Uint32(mem[i:])
		if absolute < d.fileSize {
			binary.LittleEndian.PutUint32(mem[i:], absolute-d.instrPos)
		} else if uint32(-int32(absolute)) <= d.instrPos {
			binary.LittleEndian.PutUint32(mem[i:], absolute+d.fileSize)
		}
		i += 4
		d.instrPos += 5
	}
	d.instrPos = end + 10
	copy(mem[n-6:], saved[:])
}
