package xbe

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"testing"
	"unicode/utf16"

	"golang.org/x/arch/x86/x86asm"
)

const (
	imgBase        uint32 = 0x00010000
	testHeaderSize uint32 = 0x1000
	flagWritable   uint32 = 0x1
	flagExecutable uint32 = 0x4
)

type testSection struct {
	name  string
	va    uint32
	flags uint32
	data  []byte
	vsize uint32
}

type xbeSpec struct {
	entry    uint32
	kthunk   uint32
	kind     string
	sections []testSection
	libs     []LibVersion
	tls      *TLSDirectory
	headers  uint32
	magic    string
}

func defaultLibs() []LibVersion {
	return []LibVersion{{Name: "D3D8", Major: 1, Minor: 0, Build: 4627}}
}

// Lays the header, section table, names, library versions and raw section data out as an XBE file
func buildXBE(spec xbeSpec) []byte {
	if spec.headers == 0 {
		spec.headers = testHeaderSize
	}
	if spec.magic == "" {
		spec.magic = Magic
	}
	if spec.libs == nil {
		spec.libs = defaultLibs()
	}
	entryKey, thunkKey := entryRetail, kthunkRetail
	switch spec.kind {
	case "debug":
		entryKey, thunkKey = entryDebug, kthunkDebug
	case "sega":
		entryKey, thunkKey = entrySega, kthunkSega
	}
	header := make([]byte, spec.headers)
	sectionTable := uint32(0x180)
	namesAt := sectionTable + uint32(len(spec.sections))*0x38
	cursor := namesAt
	nameAddrs := make([]uint32, len(spec.sections))
	for i, s := range spec.sections {
		nameAddrs[i] = imgBase + cursor
		copy(header[cursor:], s.name)
		cursor += uint32(len(s.name)) + 1
	}
	cursor = (cursor + 15) &^ 15
	libsAt := cursor
	for _, l := range spec.libs {
		copy(header[cursor:], l.Name)
		binary.LittleEndian.PutUint16(header[cursor+8:], l.Major)
		binary.LittleEndian.PutUint16(header[cursor+10:], l.Minor)
		binary.LittleEndian.PutUint16(header[cursor+12:], l.Build)
		cursor += 16
	}
	tlsAt := uint32(0)
	if spec.tls != nil {
		tlsAt = cursor
		binary.LittleEndian.PutUint32(header[cursor:], spec.tls.Start)
		binary.LittleEndian.PutUint32(header[cursor+4:], spec.tls.End)
		binary.LittleEndian.PutUint32(header[cursor+8:], spec.tls.Index)
		binary.LittleEndian.PutUint32(header[cursor+12:], spec.tls.Callbacks)
		binary.LittleEndian.PutUint32(header[cursor+16:], spec.tls.ZeroFill)
		cursor += 20
	}
	file := append([]byte(nil), header...)
	for i, s := range spec.sections {
		vsize := s.vsize
		if vsize == 0 {
			vsize = uint32(len(s.data))
		}
		sh := rawSection{Flags: s.flags, VirtualAddr: s.va, VirtualSize: vsize, RawAddr: uint32(len(file)), RawSize: uint32(len(s.data)), SectionNameAddr: nameAddrs[i]}
		var buf bytes.Buffer
		binary.Write(&buf, binary.LittleEndian, sh)
		copy(file[sectionTable+uint32(i)*0x38:], buf.Bytes())
		file = append(file, s.data...)
		for len(file)%16 != 0 {
			file = append(file, 0)
		}
	}
	h := rawHeader{BaseAddr: imgBase, HeadersSize: spec.headers, ImageSize: uint32(len(file)), ImageHeaderSize: 0x178, SectionCount: uint32(len(spec.sections)), SectionHeadersAddr: imgBase + sectionTable, EntryAddr: spec.entry ^ entryKey, KernThunkAddr: spec.kthunk ^ thunkKey, LibVersionsCount: uint32(len(spec.libs)), LibVersionsAddr: imgBase + libsAt}
	copy(h.Magic[:], spec.magic)
	if spec.tls != nil {
		h.TLSAddr = imgBase + tlsAt
	}
	var hb bytes.Buffer
	binary.Write(&hb, binary.LittleEndian, h)
	copy(file, hb.Bytes())
	return file
}

func parseXBE(t *testing.T, spec xbeSpec) *Image {
	t.Helper()
	img, err := Parse(buildXBE(spec))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return img
}

func disassembleImage(t *testing.T, img *Image) *Disassembly {
	t.Helper()
	d, err := Disassemble(img)
	if err != nil {
		t.Fatalf("Disassemble: %v", err)
	}
	return d
}

type fixup struct {
	off    int
	label  string
	kind   byte
	addend uint32
}

type asm struct {
	base   uint32
	buf    []byte
	labels map[string]uint32
	fixups []fixup
}

func newAsm(base uint32) *asm {
	return &asm{base: base, labels: map[string]uint32{}}
}

func (a *asm) pc() uint32 {
	return a.base + uint32(len(a.buf))
}

func (a *asm) label(name string) {
	a.labels[name] = a.pc()
}

func (a *asm) emit(b ...byte) {
	a.buf = append(a.buf, b...)
}

func (a *asm) u32(v uint32) {
	a.buf = binary.LittleEndian.AppendUint32(a.buf, v)
}

func (a *asm) rel32(opcode []byte, label string) {
	a.emit(opcode...)
	a.fixups = append(a.fixups, fixup{off: len(a.buf), label: label, kind: 'r'})
	a.emit(0, 0, 0, 0)
}

func (a *asm) rel8(opcode byte, label string) {
	a.emit(opcode)
	a.fixups = append(a.fixups, fixup{off: len(a.buf), label: label, kind: 'b'})
	a.emit(0)
}

func (a *asm) abs32(prefix []byte, label string, suffix ...byte) {
	a.emit(prefix...)
	a.fixups = append(a.fixups, fixup{off: len(a.buf), label: label, kind: 'a'})
	a.emit(0, 0, 0, 0)
	a.emit(suffix...)
}

func (a *asm) abs32Plus(prefix []byte, label string, addend uint32) {
	a.emit(prefix...)
	a.fixups = append(a.fixups, fixup{off: len(a.buf), label: label, kind: 'a', addend: addend})
	a.emit(0, 0, 0, 0)
}

func (a *asm) word(label string) {
	a.fixups = append(a.fixups, fixup{off: len(a.buf), label: label, kind: 'a'})
	a.emit(0, 0, 0, 0)
}

func (a *asm) align(n int, fill byte) {
	for len(a.buf)%n != 0 {
		a.buf = append(a.buf, fill)
	}
}

func (a *asm) pad(n int, fill byte) {
	for i := 0; i < n; i++ {
		a.buf = append(a.buf, fill)
	}
}

func (a *asm) at(label string) uint32 {
	va, ok := a.labels[label]
	if !ok {
		panic(fmt.Sprintf("asm: no label %q", label))
	}
	return va
}

func (a *asm) bytes() []byte {
	out := append([]byte(nil), a.buf...)
	for _, f := range a.fixups {
		target := a.at(f.label) + f.addend
		switch f.kind {
		case 'r':
			next := a.base + uint32(f.off) + 4
			binary.LittleEndian.PutUint32(out[f.off:], target-next)
		case 'b':
			next := a.base + uint32(f.off) + 1
			out[f.off] = byte(int8(int32(target - next)))
		case 'a':
			binary.LittleEndian.PutUint32(out[f.off:], target)
		}
	}
	return out
}

func words32(vals ...uint32) []byte {
	var out []byte
	for _, v := range vals {
		out = binary.LittleEndian.AppendUint32(out, v)
	}
	return out
}

func wideBytes(s string) []byte {
	var out []byte
	for _, u := range utf16.Encode([]rune(s)) {
		out = binary.LittleEndian.AppendUint16(out, u)
	}
	return append(out, 0, 0)
}

// Builds the kernel thunk table of ordinals flagged with the import bit, zero terminated
func thunkTable(ordinals ...uint32) []byte {
	var out []byte
	for _, o := range ordinals {
		out = binary.LittleEndian.AppendUint32(out, o|0x80000000)
	}
	return append(out, 0, 0, 0, 0)
}

func mustFunction(t *testing.T, d *Disassembly, va uint32) *Function {
	t.Helper()
	fn := d.Functions[va]
	if fn == nil {
		t.Fatalf("no function at %08X, have %v", va, d.functionVAs())
	}
	return fn
}

func opsOf(fn *Function) []string {
	var out []string
	for _, in := range fn.Instructions {
		out = append(out, in.Inst.Op.String())
	}
	return out
}

func equalU32(a, b []uint32) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Decodes one hand assembled instruction at a virtual address
func decodeAt(t *testing.T, va uint32, b ...byte) Instruction {
	t.Helper()
	inst, err := x86asm.Decode(b, 32)
	if err != nil {
		t.Fatalf("decoding %x: %v", b, err)
	}
	return Instruction{VA: va, Inst: inst, Raw: append([]byte(nil), b[:inst.Len]...)}
}

func functionOf(t *testing.T, va uint32, seqs ...[]byte) *Function {
	t.Helper()
	fn := &Function{EntryVA: va, Tables: map[uint32][]uint32{}}
	for _, b := range seqs {
		in := decodeAt(t, va, b...)
		fn.Instructions = append(fn.Instructions, in)
		va += uint32(in.Inst.Len)
	}
	fn.Size = int(va - fn.EntryVA)
	return fn
}

func indexOfOp(t *testing.T, fn *Function, op x86asm.Op, nth int) int {
	t.Helper()
	for i := range fn.Instructions {
		if fn.Instructions[i].Inst.Op == op {
			if nth == 0 {
				return i
			}
			nth--
		}
	}
	t.Fatalf("no %v number %d in %08X", op, nth, fn.EntryVA)
	return -1
}
