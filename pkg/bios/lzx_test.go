package bios

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestNewLzxDecoder(t *testing.T) {
	d := newLzxDecoder()
	if d.positionSlots != lzxTestSlots {
		t.Fatalf("position slots = %d, want %d", d.positionSlots, lzxTestSlots)
	}
	if d.lastOffsets != [3]uint32{1, 1, 1} {
		t.Fatalf("initial offsets = %v", d.lastOffsets)
	}
	if d.windowMask != 128*1024-1 {
		t.Fatalf("window mask = %#x", d.windowMask)
	}
	if !d.newBlock || !d.firstBlock {
		t.Fatalf("decoder does not start at a first new block")
	}
}

func TestLzxSlotTablesMatchDecoder(t *testing.T) {
	base, extra := lzxSlotTables()
	for s := 0; s < lzxTestSlots; s++ {
		if int64(base[s]) != lzxPositionBase[s] || uint8(extra[s]) != lzxExtraBits[s] {
			t.Fatalf("slot %d: base %d extra %d, decoder has %d and %d", s, base[s], extra[s], lzxPositionBase[s], lzxExtraBits[s])
		}
	}
}

func TestLzxDecompressEmpty(t *testing.T) {
	out, err := lzxDecompress(nil)
	if err != nil || len(out) != 0 {
		t.Fatalf("empty stream gave %d bytes, %v", len(out), err)
	}
}

func TestLzxDecompressVerbatim(t *testing.T) {
	items := append(lits("hello world "), match(12, 12))
	items = append(items, lits("!!")...)
	items = append(items, repMatch(3, 0), match(2, 2), repMatch(4, 1), repMatch(5, 2), lit('z'))
	want := "hello world " + "hello world " + "!!" + "llo" + "lo" + "orld" + "ddddd" + "z"
	if itemsSize(items) != len(want) {
		t.Fatalf("items expand to %d bytes, want %d", itemsSize(items), len(want))
	}
	got, err := lzxDecompress(lzxFrame(verbatimBlock(items), len(want)))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestLzxDecompressSkewedTrees(t *testing.T) {
	want := "abcdefghijkl" + "lkjihgfedcba" + "aaaa"
	main, length := zeroTrees()
	for i := 0; i < 12; i++ {
		main['a'+i] = uint8(min(i+1, 11))
	}
	pre := make([]uint8, 20)
	for sym, l := range map[int]uint8{18: 1, 16: 2, 15: 3, 14: 4, 13: 5, 12: 6, 11: 7, 10: 8, 9: 9, 8: 10, 7: 11, 6: 11} {
		pre[sym] = l
	}
	w := &bitWriter{}
	writeE8Header(w, 0)
	writeBlockHeader(w, blockVerbatim, len(want))
	zeroMain, zeroLen := zeroTrees()
	writeTrees(w, main, length, zeroMain, zeroLen, [3][]uint8{pre, nil, nil})
	writeItems(w, lits(want), main, length, nil)
	w.flush()
	got, err := lzxDecompress(lzxFrame(w.out, len(want)))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestLzxDecompressAligned(t *testing.T) {
	var items []lzxItem
	var want []byte
	for i := 0; i < 60; i++ {
		items = append(items, lit(byte(i)))
		want = append(want, byte(i))
	}
	items = append(items, match(4, 50), match(2, 3))
	want = append(want, 10, 11, 12, 13, 11, 12)
	aligned := []uint8{3, 3, 3, 3, 3, 3, 3, 3}
	w := &bitWriter{}
	writeE8Header(w, 0)
	writeBlockHeader(w, blockAligned, len(want))
	for _, l := range aligned {
		w.writeBits(uint32(l), 3)
	}
	main, length := lzxTreesFor(items)
	zeroMain, zeroLen := zeroTrees()
	writeTrees(w, main, length, zeroMain, zeroLen, [3][]uint8{})
	writeItems(w, items, main, length, aligned)
	w.flush()
	got, err := lzxDecompress(lzxFrame(w.out, len(want)))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("got %x, want %x", got, want)
	}
}

func TestLzxDecompressTwoVerbatimBlocks(t *testing.T) {
	first, second := lits("abcd"), lits("abcdefgh")
	want := "abcd" + "abcdefgh"
	w := &bitWriter{}
	writeE8Header(w, 0)
	writeBlockHeader(w, blockVerbatim, len(first))
	main1, len1 := lzxTreesFor(first)
	zeroMain, zeroLen := zeroTrees()
	writeTrees(w, main1, len1, zeroMain, zeroLen, [3][]uint8{})
	writeItems(w, first, main1, len1, nil)
	writeBlockHeader(w, blockVerbatim, len(second))
	main2, len2 := lzxTreesFor(second)
	writeTrees(w, main2, len2, main1, len1, [3][]uint8{})
	writeItems(w, second, main2, len2, nil)
	w.flush()
	got, err := lzxDecompress(lzxFrame(w.out, len(want)))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestLzxDecompressUncompressedThenVerbatim(t *testing.T) {
	want := "ABCDEFG" + "H" + "FGHF" + "HF"
	w := &bitWriter{}
	writeE8Header(w, 0)
	writeUncompressedBlock(w, []byte("ABCDEFG"), [3]uint32{3, 5, 7})
	items := []lzxItem{lit('H'), repMatch(4, 0), repMatch(2, 1)}
	writeBlockHeader(w, blockVerbatim, itemsSize(items))
	main, length := lzxTreesFor(items)
	zeroMain, zeroLen := zeroTrees()
	writeTrees(w, main, length, zeroMain, zeroLen, [3][]uint8{})
	writeItems(w, items, main, length, nil)
	w.flush()
	got, err := lzxDecompress(lzxFrame(w.out, len(want)))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestLzxStoreRoundTrip(t *testing.T) {
	data := make([]byte, 70001)
	for i := range data {
		data[i] = byte(i*7 + i>>8)
	}
	src := lzxStore(data)
	if len(src) != 3*(4+4+12)+70001+1 {
		t.Fatalf("stored stream is %d bytes", len(src))
	}
	got, err := lzxDecompress(src)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, data) {
		t.Fatalf("round trip differs at %d", bytes.Compare(got, data))
	}
}

func TestLzxDecompressWindowWrap(t *testing.T) {
	const literals = 128 * 1024
	const distance = 131000
	want := make([]byte, literals+100)
	x := uint32(12345)
	var items []lzxItem
	for i := 0; i < literals; i++ {
		x = x*1103515245 + 12345
		want[i] = byte(x >> 16)
		items = append(items, lit(want[i]))
	}
	items = append(items, match(100, distance))
	for i := 0; i < 100; i++ {
		want[literals+i] = want[literals+i-distance]
	}
	main, length := lzxTreesFor(items)
	zeroMain, zeroLen := zeroTrees()
	var src []byte
	for off := 0; off < len(items); off += lzxTestFrame {
		w := &bitWriter{}
		if off == 0 {
			writeE8Header(w, 0)
			writeBlockHeader(w, blockVerbatim, len(want))
			writeTrees(w, main, length, zeroMain, zeroLen, [3][]uint8{})
		}
		frame := items[off:min(off+lzxTestFrame, len(items))]
		writeItems(w, frame, main, length, nil)
		w.flush()
		src = append(src, lzxFrame(w.out, itemsSize(frame))...)
	}
	got, err := lzxDecompress(src)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("got %d bytes, equal prefix %d", len(got), equalPrefix(got, want))
	}
}

func equalPrefix(a, b []byte) int {
	n := 0
	for n < len(a) && n < len(b) && a[n] == b[n] {
		n++
	}
	return n
}

func TestLzxDecompressE8(t *testing.T) {
	const fileSize = 0x10000
	chunk := bytes.Repeat([]byte{0x90}, 40)
	for at, abs := range map[int]uint32{5: 0x1234, 12: 0xFFFFFFF8, 19: 0x20000, 24: 0xFFFFFFE0, 29: 0x100, 35: 1} {
		chunk[at] = 0xE8
		binary.LittleEndian.PutUint32(chunk[at+1:], abs)
	}
	want := append([]byte(nil), chunk...)
	binary.LittleEndian.PutUint32(want[6:], 0x1234-5)
	binary.LittleEndian.PutUint32(want[13:], 0xFFF8)
	binary.LittleEndian.PutUint32(want[30:], 0x100-29)
	second := bytes.Repeat([]byte{0x90}, 20)
	second[2] = 0xE8
	binary.LittleEndian.PutUint32(second[3:], 0x50)
	wantSecond := append([]byte(nil), second...)
	binary.LittleEndian.PutUint32(wantSecond[3:], 0x50-42)
	third := []byte{0xE8, 0x34, 0x12, 0, 0, 0x90}
	fourth := bytes.Repeat([]byte{0x90}, 20)
	fourth[0] = 0xE8
	binary.LittleEndian.PutUint32(fourth[1:], 0x100)
	wantFourth := append([]byte(nil), fourth...)
	binary.LittleEndian.PutUint32(wantFourth[1:], 0x100-66)
	var src []byte
	for i, data := range [][]byte{chunk, second, third, fourth} {
		w := &bitWriter{}
		if i == 0 {
			writeE8Header(w, fileSize)
		}
		writeUncompressedBlock(w, data, [3]uint32{1, 1, 1})
		src = append(src, lzxFrame(w.out, len(data))...)
	}
	got, err := lzxDecompress(src)
	if err != nil {
		t.Fatal(err)
	}
	wantAll := append(append(append(want, wantSecond...), third...), wantFourth...)
	if !bytes.Equal(got, wantAll) {
		t.Fatalf("got  %x\nwant %x", got, wantAll)
	}
}
