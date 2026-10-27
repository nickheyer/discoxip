package bios

import (
	"encoding/binary"
	"math/bits"
)

const (
	lzxTestSlots       = 34
	lzxTestMainSymbols = 256 + lzxTestSlots*8
	lzxTestLenSymbols  = 249
	lzxTestFrame       = 32768
)

// bitWriter packs bits most significant first into little endian 16 bit words
type bitWriter struct {
	out  []byte
	acc  uint32
	nacc int
}

func (w *bitWriter) writeBits(v uint32, n int) {
	for n > 0 {
		take := min(n, 16-w.nacc)
		w.acc = w.acc<<uint(take) | (v>>uint(n-take))&(1<<uint(take)-1)
		w.nacc += take
		n -= take
		if w.nacc == 16 {
			w.out = binary.LittleEndian.AppendUint16(w.out, uint16(w.acc))
			w.acc, w.nacc = 0, 0
		}
	}
}

// Pads the pending word with zero bits
func (w *bitWriter) flush() {
	if w.nacc > 0 {
		w.writeBits(0, 16-w.nacc)
	}
}

// Pads one to sixteen zero bits so the next byte starts a word
func (w *bitWriter) pad16() {
	if w.nacc == 0 {
		w.writeBits(0, 16)
	} else {
		w.flush()
	}
}

// Appends raw bytes plus one pad byte after an odd count
func (w *bitWriter) writeRaw(data []byte) {
	w.out = append(w.out, data...)
	if len(data)%2 == 1 {
		w.out = append(w.out, 0)
	}
}

// Position slot bases and extra bit counts of a 128 KB window
func lzxSlotTables() (base, extra [lzxTestSlots]int) {
	for s := 4; s < lzxTestSlots; s++ {
		extra[s] = s/2 - 1
	}
	for s := 1; s < lzxTestSlots; s++ {
		base[s] = base[s-1] + 1<<uint(extra[s-1])
	}
	return base, extra
}

type huffCode struct {
	code uint32
	n    int
}

// Canonical codes with shorter lengths first and symbols ascending within a length
func canonicalCodes(lengths []uint8) []huffCode {
	var count [17]uint32
	for _, l := range lengths {
		count[l]++
	}
	count[0] = 0
	var next [17]uint32
	code := uint32(0)
	for l := 1; l <= 16; l++ {
		code = (code + count[l-1]) << 1
		next[l] = code
	}
	out := make([]huffCode, len(lengths))
	for i, l := range lengths {
		if l != 0 {
			out[i] = huffCode{next[l], int(l)}
			next[l]++
		}
	}
	return out
}

// Complete code lengths over the symbols used, the first few one bit shorter
func balancedLengths(n int, used []int) []uint8 {
	out := make([]uint8, n)
	if len(used) == 0 {
		return out
	}
	if len(used) == 1 {
		used = append(used, (used[0]+1)%n)
	}
	d := bits.Len(uint(len(used) - 1))
	short := 1<<uint(d) - len(used)
	for i, s := range used {
		out[s] = uint8(d)
		if i < short {
			out[s] = uint8(d - 1)
		}
	}
	return out
}

func usedSymbols(seen []bool) []int {
	var out []int
	for s, ok := range seen {
		if ok {
			out = append(out, s)
		}
	}
	return out
}

type lzxItem struct {
	lit    byte
	length int
	offset int
	rep    int
}

func lit(b byte) lzxItem { return lzxItem{lit: b} }

func lits(s string) []lzxItem {
	var out []lzxItem
	for i := 0; i < len(s); i++ {
		out = append(out, lit(s[i]))
	}
	return out
}

func match(length, offset int) lzxItem { return lzxItem{length: length, offset: offset, rep: -1} }

func repMatch(length, slot int) lzxItem { return lzxItem{length: length, rep: slot} }

func itemsSize(items []lzxItem) int {
	n := 0
	for _, it := range items {
		n += max(it.length, 1)
	}
	return n
}

// The main symbol, length symbol, slot and extra bits of a match
func matchSymbols(it lzxItem) (sym, lenSym, slot, extra int) {
	base, _ := lzxSlotTables()
	header := it.length - 2
	lenSym = -1
	if header >= 7 {
		lenSym = it.length - 9
		header = 7
	}
	slot = it.rep
	if it.rep < 0 {
		formatted := it.offset + 2
		for slot = 0; slot+1 < lzxTestSlots && base[slot+1] <= formatted; slot++ {
		}
		extra = formatted - base[slot]
	}
	return 256 + slot*8 + header, lenSym, slot, extra
}

func lzxTreesFor(items []lzxItem) (main, length []uint8) {
	mainSeen := make([]bool, lzxTestMainSymbols)
	lenSeen := make([]bool, lzxTestLenSymbols)
	for _, it := range items {
		if it.length == 0 {
			mainSeen[it.lit] = true
			continue
		}
		sym, lenSym, _, _ := matchSymbols(it)
		mainSeen[sym] = true
		if lenSym >= 0 {
			lenSeen[lenSym] = true
		}
	}
	return balancedLengths(lzxTestMainSymbols, usedSymbols(mainSeen)), balancedLengths(lzxTestLenSymbols, usedSymbols(lenSeen))
}

// Writes path lengths as pretree coded differences from the previous ones
func writePathLengths(w *bitWriter, lengths, prev, pre []uint8) {
	type item struct{ sym, extra, nextra, sym2 int }
	var items []item
	for i := 0; i < len(lengths); {
		run := 1
		for i+run < len(lengths) && lengths[i+run] == lengths[i] {
			run++
		}
		diff := (int(prev[i]) - int(lengths[i]) + 17) % 17
		switch {
		case lengths[i] == 0 && run >= 20:
			run = min(run, 51)
			items = append(items, item{18, run - 20, 5, -1})
		case lengths[i] == 0 && run >= 4:
			items = append(items, item{17, run - 4, 4, -1})
		case run >= 4:
			run = min(run, 5)
			items = append(items, item{19, run - 4, 1, diff})
		default:
			run = 1
			items = append(items, item{diff, 0, 0, -1})
		}
		i += run
	}
	if pre == nil {
		seen := make([]bool, 20)
		for _, it := range items {
			seen[it.sym] = true
			if it.sym2 >= 0 {
				seen[it.sym2] = true
			}
		}
		pre = balancedLengths(20, usedSymbols(seen))
	}
	for _, l := range pre {
		w.writeBits(uint32(l), 4)
	}
	codes := canonicalCodes(pre)
	for _, it := range items {
		w.writeBits(codes[it.sym].code, codes[it.sym].n)
		if it.nextra > 0 {
			w.writeBits(uint32(it.extra), it.nextra)
		}
		if it.sym2 >= 0 {
			w.writeBits(codes[it.sym2].code, codes[it.sym2].n)
		}
	}
}

func writeTrees(w *bitWriter, main, length, prevMain, prevLength []uint8, pre [3][]uint8) {
	writePathLengths(w, main[:256], prevMain[:256], pre[0])
	writePathLengths(w, main[256:], prevMain[256:], pre[1])
	writePathLengths(w, length, prevLength, pre[2])
}

// Writes literals and matches, aligned offsets through the aligned tree when one is given
func writeItems(w *bitWriter, items []lzxItem, main, length, aligned []uint8) {
	mainCodes, lenCodes, alignedCodes := canonicalCodes(main), canonicalCodes(length), canonicalCodes(aligned)
	_, extraBits := lzxSlotTables()
	for _, it := range items {
		if it.length == 0 {
			w.writeBits(mainCodes[it.lit].code, mainCodes[it.lit].n)
			continue
		}
		sym, lenSym, slot, extra := matchSymbols(it)
		w.writeBits(mainCodes[sym].code, mainCodes[sym].n)
		if lenSym >= 0 {
			w.writeBits(lenCodes[lenSym].code, lenCodes[lenSym].n)
		}
		n := extraBits[slot]
		switch {
		case aligned != nil && n >= 3:
			if n > 3 {
				w.writeBits(uint32(extra>>3), n-3)
			}
			w.writeBits(alignedCodes[extra&7].code, alignedCodes[extra&7].n)
		case n > 0:
			w.writeBits(uint32(extra), n)
		}
	}
}

func writeE8Header(w *bitWriter, fileSize uint32) {
	if fileSize == 0 {
		w.writeBits(0, 1)
		return
	}
	w.writeBits(1, 1)
	w.writeBits(fileSize>>16, 16)
	w.writeBits(fileSize&0xFFFF, 16)
}

func writeBlockHeader(w *bitWriter, typ, size int) {
	w.writeBits(uint32(typ), 3)
	w.writeBits(uint32(size), 24)
}

func writeUncompressedBlock(w *bitWriter, data []byte, offsets [3]uint32) {
	writeBlockHeader(w, blockUncompressed, len(data))
	w.pad16()
	for _, r := range offsets {
		w.out = binary.LittleEndian.AppendUint32(w.out, r)
	}
	w.writeRaw(data)
}

func zeroTrees() (main, length []uint8) {
	return make([]uint8, lzxTestMainSymbols), make([]uint8, lzxTestLenSymbols)
}

// One verbatim block holding the items, headed as the first block of a stream
func verbatimBlock(items []lzxItem) []byte {
	w := &bitWriter{}
	writeE8Header(w, 0)
	writeBlockHeader(w, blockVerbatim, itemsSize(items))
	main, length := lzxTreesFor(items)
	zeroMain, zeroLen := zeroTrees()
	writeTrees(w, main, length, zeroMain, zeroLen, [3][]uint8{})
	writeItems(w, items, main, length, nil)
	w.flush()
	return w.out
}

func lzxFrame(data []byte, expanded int) []byte {
	out := binary.LittleEndian.AppendUint16(nil, uint16(len(data)))
	out = binary.LittleEndian.AppendUint16(out, uint16(expanded))
	return append(out, data...)
}

// Frames of one uncompressed block each holding the data as the decoder expects it
func lzxStore(data []byte) []byte {
	var out []byte
	for off := 0; off < len(data); off += lzxTestFrame {
		chunk := data[off:min(off+lzxTestFrame, len(data))]
		w := &bitWriter{}
		if off == 0 {
			writeE8Header(w, 0)
		}
		writeUncompressedBlock(w, chunk, [3]uint32{1, 1, 1})
		out = append(out, lzxFrame(w.out, len(chunk))...)
	}
	return out
}
