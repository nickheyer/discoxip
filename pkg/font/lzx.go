package font

import (
	"fmt"
)

// LZX stream constants, from the Microsoft LZX DELTA specification
const (
	lzxMinMatch          = 2
	lzxNumChars          = 256
	lzxPretreeSyms       = 20
	lzxAlignedSyms       = 8
	lzxLengthSyms        = 249
	lzxNumPrimaryLengths = 7
	lzxBlockVerbatim     = 1
	lzxBlockAligned      = 2
	lzxBlockUncompressed = 3
	lzxFrameSize         = 32768
	lzxMaxCodeLength     = 16
)

// lzxBits reads the stream as the format lays it out, sixteen bit little endian words consumed from the top bit
type lzxBits struct {
	src    []byte
	pos    int
	buffer uint32
	left   int
}

// Fills the buffer until it holds the bits asked for, a word at a time
func (b *lzxBits) ensure(n int) error {
	for b.left < n {
		if b.pos+2 > len(b.src) {
			return fmt.Errorf("lzx: input ends inside the stream")
		}
		word := uint32(b.src[b.pos]) | uint32(b.src[b.pos+1])<<8
		b.buffer |= word << (32 - 16 - b.left)
		b.left += 16
		b.pos += 2
	}
	return nil
}

func (b *lzxBits) peek(n int) uint32 {
	return b.buffer >> (32 - n)
}

func (b *lzxBits) remove(n int) {
	b.buffer <<= n
	b.left -= n
}

func (b *lzxBits) read(n int) (uint32, error) {
	if n == 0 {
		return 0, nil
	}
	if err := b.ensure(n); err != nil {
		return 0, err
	}
	v := b.peek(n)
	b.remove(n)
	return v, nil
}

// lzxTree is a canonical Huffman code, decoded by walking the code lengths from the shortest
type lzxTree struct {
	count   [lzxMaxCodeLength + 1]int
	symbols []int
}

// Builds the canonical code the lengths describe, codes assigned by rising length then rising symbol
func buildLZXTree(lengths []byte) (*lzxTree, error) {
	t := &lzxTree{}
	for _, l := range lengths {
		if int(l) > lzxMaxCodeLength {
			return nil, fmt.Errorf("lzx: code length %d", l)
		}
		t.count[l]++
	}
	t.count[0] = 0
	left := 1
	for l := 1; l <= lzxMaxCodeLength; l++ {
		left <<= 1
		left -= t.count[l]
		if left < 0 {
			return nil, fmt.Errorf("lzx: over subscribed code")
		}
	}
	var offs [lzxMaxCodeLength + 2]int
	for l := 1; l <= lzxMaxCodeLength; l++ {
		offs[l+1] = offs[l] + t.count[l]
	}
	t.symbols = make([]int, offs[lzxMaxCodeLength+1])
	for sym, l := range lengths {
		if l != 0 {
			t.symbols[offs[l]] = sym
			offs[l]++
		}
	}
	return t, nil
}

// Reads one symbol of the code off the stream
func (t *lzxTree) decode(b *lzxBits) (int, error) {
	code, first, index := 0, 0, 0
	for l := 1; l <= lzxMaxCodeLength; l++ {
		bit, err := b.read(1)
		if err != nil {
			return 0, err
		}
		code |= int(bit)
		count := t.count[l]
		if code-first < count {
			return t.symbols[index+code-first], nil
		}
		index += count
		first += count
		first <<= 1
		code <<= 1
	}
	return 0, fmt.Errorf("lzx: code outside the tree")
}

// lzxDecoder holds the state one stream carries between its blocks
type lzxDecoder struct {
	window        []byte
	windowPos     int
	r0, r1, r2    uint32
	mainLens      []byte
	lengthLens    []byte
	alignedLens   []byte
	mainTree      *lzxTree
	lengthTree    *lzxTree
	alignedTree   *lzxTree
	blockType     int
	blockLength   int
	blockRemain   int
	headerRead    bool
	intelFileSize int32
	intelStarted  bool
	intelCurPos   int32
	posnSlots     int
	extraBits     [51]int
	positionBase  [51]uint32
}

// Sets up a decoder for a window of the given number of bits
func newLZXDecoder(windowBits int) (*lzxDecoder, error) {
	if windowBits < 15 || windowBits > 21 {
		return nil, fmt.Errorf("lzx: window of %d bits", windowBits)
	}
	d := &lzxDecoder{window: make([]byte, 1<<windowBits), r0: 1, r1: 1, r2: 1}
	switch windowBits {
	case 21:
		d.posnSlots = 50
	case 20:
		d.posnSlots = 42
	default:
		d.posnSlots = windowBits << 1
	}
	d.mainLens = make([]byte, lzxNumChars+d.posnSlots*8)
	d.lengthLens = make([]byte, lzxLengthSyms)
	d.alignedLens = make([]byte, lzxAlignedSyms)
	for i := range d.extraBits {
		if i >= 4 {
			d.extraBits[i] = min(i/2-1, 17)
		}
	}
	base := uint32(0)
	for i := 0; i < 51; i++ {
		d.positionBase[i] = base
		base += 1 << d.extraBits[i]
	}
	return d, nil
}

// Reads a run of code lengths as deltas from the lengths the previous block left, through the pretree
func (d *lzxDecoder) readLengths(b *lzxBits, lens []byte, first, last int) error {
	pretreeLens := make([]byte, lzxPretreeSyms)
	for i := range pretreeLens {
		v, err := b.read(4)
		if err != nil {
			return err
		}
		pretreeLens[i] = byte(v)
	}
	pretree, err := buildLZXTree(pretreeLens)
	if err != nil {
		return err
	}
	for x := first; x < last; {
		z, err := pretree.decode(b)
		if err != nil {
			return err
		}
		switch {
		case z == 17:
			y, err := b.read(4)
			if err != nil {
				return err
			}
			for n := int(y) + 4; n > 0 && x < last; n-- {
				lens[x] = 0
				x++
			}
		case z == 18:
			y, err := b.read(5)
			if err != nil {
				return err
			}
			for n := int(y) + 20; n > 0 && x < last; n-- {
				lens[x] = 0
				x++
			}
		case z == 19:
			y, err := b.read(1)
			if err != nil {
				return err
			}
			z, err = pretree.decode(b)
			if err != nil {
				return err
			}
			v := int(lens[x]) - z
			if v < 0 {
				v += 17
			}
			for n := int(y) + 4; n > 0 && x < last; n-- {
				lens[x] = byte(v)
				x++
			}
		default:
			v := int(lens[x]) - z
			if v < 0 {
				v += 17
			}
			lens[x] = byte(v)
			x++
		}
	}
	return nil
}

// Reads the header of the next block and the trees it carries
func (d *lzxDecoder) startBlock(b *lzxBits) error {
	if d.blockType == lzxBlockUncompressed && d.blockLength&1 != 0 {
		if b.pos >= len(b.src) {
			return fmt.Errorf("lzx: input ends at the pad byte")
		}
		b.pos++
	}
	t, err := b.read(3)
	if err != nil {
		return err
	}
	d.blockType = int(t)
	hi, err := b.read(16)
	if err != nil {
		return err
	}
	lo, err := b.read(8)
	if err != nil {
		return err
	}
	d.blockLength = int(hi<<8 | lo)
	d.blockRemain = d.blockLength
	switch d.blockType {
	case lzxBlockAligned:
		for i := range d.alignedLens {
			v, err := b.read(3)
			if err != nil {
				return err
			}
			d.alignedLens[i] = byte(v)
		}
		if d.alignedTree, err = buildLZXTree(d.alignedLens); err != nil {
			return err
		}
		fallthrough
	case lzxBlockVerbatim:
		if err := d.readLengths(b, d.mainLens, 0, lzxNumChars); err != nil {
			return err
		}
		if err := d.readLengths(b, d.mainLens, lzxNumChars, len(d.mainLens)); err != nil {
			return err
		}
		if d.mainTree, err = buildLZXTree(d.mainLens); err != nil {
			return err
		}
		if d.mainLens[0xE8] != 0 {
			d.intelStarted = true
		}
		if err := d.readLengths(b, d.lengthLens, 0, lzxLengthSyms); err != nil {
			return err
		}
		if d.lengthTree, err = buildLZXTree(d.lengthLens); err != nil {
			return err
		}
	case lzxBlockUncompressed:
		d.intelStarted = true
		if b.left == 0 {
			if err := b.ensure(16); err != nil {
				return err
			}
		}
		b.left, b.buffer = 0, 0
		if b.pos+12 > len(b.src) {
			return fmt.Errorf("lzx: input ends inside the stored offsets")
		}
		word := func(i int) uint32 {
			return uint32(b.src[i]) | uint32(b.src[i+1])<<8 | uint32(b.src[i+2])<<16 | uint32(b.src[i+3])<<24
		}
		d.r0, d.r1, d.r2 = word(b.pos), word(b.pos+4), word(b.pos+8)
		b.pos += 12
	default:
		return fmt.Errorf("lzx: block type %d", d.blockType)
	}
	return nil
}

// Reads the offset of a match, the three most recent offsets standing in for the first three slots
func (d *lzxDecoder) matchOffset(b *lzxBits, slot int) (uint32, error) {
	switch {
	case slot == 0:
		return d.r0, nil
	case slot == 1:
		d.r1, d.r0 = d.r0, d.r1
		return d.r0, nil
	case slot == 2:
		d.r2, d.r0 = d.r0, d.r2
		return d.r0, nil
	}
	var offset uint32
	extra := 17
	if slot < 36 {
		extra = d.extraBits[slot]
	}
	switch {
	case d.blockType == lzxBlockVerbatim && slot == 3:
		offset = 1
	case d.blockType == lzxBlockVerbatim:
		bits, err := b.read(extra)
		if err != nil {
			return 0, err
		}
		offset = d.positionBase[slot] - 2 + bits
	case extra > 3:
		bits, err := b.read(extra - 3)
		if err != nil {
			return 0, err
		}
		aligned, err := d.alignedTree.decode(b)
		if err != nil {
			return 0, err
		}
		offset = d.positionBase[slot] - 2 + bits<<3 + uint32(aligned)
	case extra == 3:
		aligned, err := d.alignedTree.decode(b)
		if err != nil {
			return 0, err
		}
		offset = d.positionBase[slot] - 2 + uint32(aligned)
	case extra > 0:
		bits, err := b.read(extra)
		if err != nil {
			return 0, err
		}
		offset = d.positionBase[slot] - 2 + bits
	default:
		offset = 1
	}
	d.r2, d.r1, d.r0 = d.r1, d.r0, offset
	return offset, nil
}

// Decodes one frame of at most the given length into the window, returning the bytes it produced
func (d *lzxDecoder) decodeFrame(b *lzxBits, length int) ([]byte, error) {
	if !d.headerRead {
		flag, err := b.read(1)
		if err != nil {
			return nil, err
		}
		if flag != 0 {
			hi, err := b.read(16)
			if err != nil {
				return nil, err
			}
			lo, err := b.read(16)
			if err != nil {
				return nil, err
			}
			d.intelFileSize = int32(hi<<16 | lo)
		}
		d.headerRead = true
	}
	start := d.windowPos
	for d.windowPos-start < length {
		if d.blockRemain == 0 {
			if err := d.startBlock(b); err != nil {
				return nil, err
			}
		}
		run := d.blockRemain
		if remaining := length - (d.windowPos - start); run > remaining {
			run = remaining
		}
		if d.windowPos+run > len(d.window) {
			return nil, fmt.Errorf("lzx: output past the window")
		}
		d.blockRemain -= run
		if d.blockType == lzxBlockUncompressed {
			if b.pos+run > len(b.src) {
				return nil, fmt.Errorf("lzx: input ends inside a stored block")
			}
			copy(d.window[d.windowPos:], b.src[b.pos:b.pos+run])
			b.pos += run
			d.windowPos += run
			continue
		}
		for run > 0 {
			sym, err := d.mainTree.decode(b)
			if err != nil {
				return nil, err
			}
			if sym < lzxNumChars {
				d.window[d.windowPos] = byte(sym)
				d.windowPos++
				run--
				continue
			}
			sym -= lzxNumChars
			matchLength := sym & lzxNumPrimaryLengths
			if matchLength == lzxNumPrimaryLengths {
				footer, err := d.lengthTree.decode(b)
				if err != nil {
					return nil, err
				}
				matchLength += footer
			}
			matchLength += lzxMinMatch
			offset, err := d.matchOffset(b, sym>>3)
			if err != nil {
				return nil, err
			}
			if matchLength > run {
				return nil, fmt.Errorf("lzx: match of %d bytes past the block end", matchLength)
			}
			if uint32(d.windowPos) < offset {
				return nil, fmt.Errorf("lzx: match reaches %d bytes before the stream start", offset-uint32(d.windowPos))
			}
			src := d.windowPos - int(offset)
			for i := 0; i < matchLength; i++ {
				d.window[d.windowPos+i] = d.window[src+i]
			}
			d.windowPos += matchLength
			run -= matchLength
		}
	}
	out := d.window[start:d.windowPos]
	d.translateCalls(out)
	return out, nil
}

// Undoes the call target translation the compressor applied to x86 call instructions in a frame
func (d *lzxDecoder) translateCalls(frame []byte) {
	size := len(frame)
	if d.intelStarted && d.intelFileSize != 0 && size > 10 {
		curpos := d.intelCurPos
		for i := 0; i < size-10; i++ {
			if frame[i] != 0xE8 {
				continue
			}
			abs := int32(uint32(frame[i+1]) | uint32(frame[i+2])<<8 | uint32(frame[i+3])<<16 | uint32(frame[i+4])<<24)
			at := curpos + int32(i)
			if abs >= -at && abs < d.intelFileSize {
				rel := abs - at
				if abs < 0 {
					rel = abs + d.intelFileSize
				}
				frame[i+1], frame[i+2], frame[i+3], frame[i+4] = byte(rel), byte(rel>>8), byte(rel>>16), byte(rel>>24)
			}
			i += 4
		}
	}
	d.intelCurPos += int32(size)
}

// Decompresses one self contained LZX stream of a known length, as the decompression interface does per call
func lzxDecompress(src []byte, length, windowBits int) ([]byte, error) {
	d, err := newLZXDecoder(windowBits)
	if err != nil {
		return nil, err
	}
	b := &lzxBits{src: src}
	var out []byte
	for len(out) < length {
		frame := lzxFrameSize
		if remaining := length - len(out); frame > remaining {
			frame = remaining
		}
		part, err := d.decodeFrame(b, frame)
		if err != nil {
			return nil, err
		}
		out = append(out, part...)
	}
	return out, nil
}
