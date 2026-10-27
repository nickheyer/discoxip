package xbe

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func sampleSpec() xbeSpec {
	text := newAsm(0x00011000)
	text.emit(0x55, 0x8B, 0xEC, 0x5D, 0xC3)
	text.pad(11, 0xCC)
	data := append(thunkTable(145), wideBytes("Hello")...)
	data = append(data, words32(0x11223344, 0xDEADBEEF)...)
	return xbeSpec{
		entry:  0x00011000,
		kthunk: 0x00012000,
		sections: []testSection{
			{name: ".text", va: 0x00011000, flags: flagExecutable, data: text.bytes(), vsize: 0x40},
			{name: ".data", va: 0x00012000, flags: flagWritable, data: data, vsize: 0x100},
		},
		libs: []LibVersion{{Name: "D3D8", Major: 1, Minor: 0, Build: 4627}, {Name: "XAPILIB", Major: 1, Minor: 2, Build: 5558}},
		tls:  &TLSDirectory{Start: 0x13000, End: 0x13040, Index: 0x12080, Callbacks: 0, ZeroFill: 0x20},
	}
}

func TestParseHeaderAndSections(t *testing.T) {
	spec := sampleSpec()
	img := parseXBE(t, spec)
	if img.BaseAddr != imgBase || img.EntryPoint != 0x11000 || img.KernThunk != 0x12000 || img.HeadersSize != testHeaderSize {
		t.Fatalf("header fields %08X %08X %08X %X", img.BaseAddr, img.EntryPoint, img.KernThunk, img.HeadersSize)
	}
	if len(img.Sections) != 2 {
		t.Fatalf("sections %d", len(img.Sections))
	}
	text := img.Sections[0]
	if text.Name != ".text" || text.VirtualAddr != 0x11000 || text.VirtualSize != 0x40 || text.RawSize != 16 || text.Flags != flagExecutable || text.RawAddr != testHeaderSize {
		t.Fatalf("text section %+v", text)
	}
	if len(text.Data) != 0x40 || !bytes.Equal(text.Data[:5], []byte{0x55, 0x8B, 0xEC, 0x5D, 0xC3}) || !bytes.Equal(text.Data[16:], make([]byte, 0x30)) {
		t.Fatalf("text data %x", text.Data)
	}
	data := img.Sections[1]
	if data.Name != ".data" || data.Flags != flagWritable || data.RawSize != uint32(len(spec.sections[1].data)) || data.RawAddr != testHeaderSize+16 {
		t.Fatalf("data section %+v", data)
	}
	if len(img.Libs) != 2 || img.Libs[0] != (LibVersion{Name: "D3D8", Major: 1, Minor: 0, Build: 4627}) || img.Libs[1] != (LibVersion{Name: "XAPILIB", Major: 1, Minor: 2, Build: 5558}) {
		t.Fatalf("libs %+v", img.Libs)
	}
	if img.TLS == nil || *img.TLS != *spec.tls {
		t.Fatalf("tls %+v", img.TLS)
	}
}

func TestParseWithoutTLS(t *testing.T) {
	spec := sampleSpec()
	spec.tls = nil
	if img := parseXBE(t, spec); img.TLS != nil {
		t.Fatalf("tls %+v", img.TLS)
	}
}

func TestParseEntryScrambling(t *testing.T) {
	for _, kind := range []string{"retail", "debug", "sega"} {
		spec := sampleSpec()
		spec.kind = kind
		file := buildXBE(spec)
		stored := binary.LittleEndian.Uint32(file[0x128:])
		if stored == spec.entry {
			t.Fatalf("%s entry stored unscrambled", kind)
		}
		img, err := Parse(file)
		if err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		if img.EntryPoint != 0x11000 || img.KernThunk != 0x12000 {
			t.Fatalf("%s entry %08X thunk %08X", kind, img.EntryPoint, img.KernThunk)
		}
	}
}

func TestParseRejectsBadMagic(t *testing.T) {
	spec := sampleSpec()
	spec.magic = "XBEX"
	if _, err := Parse(buildXBE(spec)); err == nil || !strings.Contains(err.Error(), "magic") {
		t.Fatalf("err %v", err)
	}
	if _, err := Parse([]byte("XB")); err == nil {
		t.Fatal("short magic accepted")
	}
}

func TestParseRejectsTruncatedHeader(t *testing.T) {
	file := buildXBE(sampleSpec())
	if _, err := Parse(file[:0x100]); err == nil {
		t.Fatal("truncated header accepted")
	}
}

func TestParseRejectsTLSOutsideHeaders(t *testing.T) {
	file := buildXBE(sampleSpec())
	binary.LittleEndian.PutUint32(file[0x12C:], imgBase+uint32(len(file)))
	if _, err := Parse(file); err == nil || !strings.Contains(err.Error(), "TLS") {
		t.Fatalf("err %v", err)
	}
}

func TestParseRejectsSectionOutsideFile(t *testing.T) {
	file := buildXBE(sampleSpec())
	rawAddrField := 0x180 + 12
	binary.LittleEndian.PutUint32(file[rawAddrField:], uint32(len(file))+0x100)
	if _, err := Parse(file); err == nil {
		t.Fatal("section raw data outside the file accepted")
	}
}

func TestOpenReadsFile(t *testing.T) {
	spec := sampleSpec()
	path := filepath.Join(t.TempDir(), "default.xbe")
	if err := os.WriteFile(path, buildXBE(spec), 0o644); err != nil {
		t.Fatal(err)
	}
	img, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if img.EntryPoint != spec.entry || len(img.Sections) != 2 || img.Sections[1].Name != ".data" {
		t.Fatalf("image %+v", img)
	}
	if _, err := Open(filepath.Join(t.TempDir(), "missing.xbe")); err == nil {
		t.Fatal("missing file opened")
	}
}

func TestNewImageAndHeaderBytes(t *testing.T) {
	header := []byte("XBEHheader")
	secs := []Section{{Name: ".x", VirtualAddr: 0x20000, VirtualSize: 4, Data: words32(7)}}
	img := NewImage(0x1F000, 0x20000, header, secs)
	if img.BaseAddr != 0x1F000 || img.EntryPoint != 0x20000 || img.HeadersSize != 10 || !bytes.Equal(img.HeaderBytes(), header) {
		t.Fatalf("image %+v", img)
	}
	if v, ok := img.ReadU32(0x20000); !ok || v != 7 {
		t.Fatalf("read %08X %v", v, ok)
	}
	short := &Image{HeadersSize: 100, raw: []byte{1, 2, 3}}
	if !bytes.Equal(short.HeaderBytes(), []byte{1, 2, 3}) {
		t.Fatalf("header %x", short.HeaderBytes())
	}
	full := parseXBE(t, sampleSpec())
	if len(full.HeaderBytes()) != int(testHeaderSize) || string(full.HeaderBytes()[:4]) != Magic {
		t.Fatalf("header %d", len(full.HeaderBytes()))
	}
}

// Every section header gains a name reference count of one and nothing else changes
func TestLoadedHeader(t *testing.T) {
	img := parseXBE(t, sampleSpec())
	loaded, err := img.LoadedHeader()
	if err != nil {
		t.Fatal(err)
	}
	want := append([]byte(nil), img.HeaderBytes()...)
	for i := 0; i < 2; i++ {
		binary.LittleEndian.PutUint32(want[0x180+i*0x38+24:], 1)
	}
	if !bytes.Equal(loaded, want) {
		t.Fatal("loaded header differs from the expected patch")
	}
	if bytes.Equal(loaded, img.HeaderBytes()) {
		t.Fatal("loaded header unchanged")
	}
	clipped := NewImage(imgBase, img.EntryPoint, img.HeaderBytes()[:0x178], img.Sections)
	if _, err := clipped.LoadedHeader(); err == nil || !strings.Contains(err.Error(), "outside") {
		t.Fatalf("err %v", err)
	}
	if _, err := NewImage(imgBase, 0, []byte("XBEH"), nil).LoadedHeader(); err == nil {
		t.Fatal("short header accepted")
	}
}

func TestLibBuild(t *testing.T) {
	img := parseXBE(t, sampleSpec())
	if b := img.LibBuild("d3d8"); b != 4627 {
		t.Fatalf("d3d8 %d", b)
	}
	if b := img.LibBuild("XAPILIB"); b != 5558 {
		t.Fatalf("xapilib %d", b)
	}
	if b := img.LibBuild("D3D8LTCG"); b != 0 {
		t.Fatalf("absent %d", b)
	}
}

func TestMemoryLayout(t *testing.T) {
	spec := sampleSpec()
	spec.sections = []testSection{spec.sections[1], spec.sections[0]}
	img := parseXBE(t, spec)
	mem, raw, err := img.Memory()
	if err != nil {
		t.Fatal(err)
	}
	if mem.File != MemoryFile || mem.Base != imgBase || len(mem.Sections) != 3 {
		t.Fatalf("memory %+v", mem)
	}
	want := []ImageSection{
		{Name: "header", VA: imgBase, Size: testHeaderSize, Off: 0},
		{Name: ".text", VA: 0x11000, Size: 0x40, Off: testHeaderSize},
		{Name: ".data", VA: 0x12000, Size: 0x100, Off: testHeaderSize + 0x40, Writable: true},
	}
	for i, w := range want {
		if mem.Sections[i] != w {
			t.Fatalf("section %d %+v want %+v", i, mem.Sections[i], w)
		}
	}
	if len(raw) != int(testHeaderSize)+0x40+0x100 || string(raw[:4]) != Magic || raw[testHeaderSize] != 0x55 || !bytes.Equal(raw[testHeaderSize+0x40:testHeaderSize+0x40+28], spec.sections[0].data) {
		t.Fatalf("bytes %d", len(raw))
	}
}

func TestMemoryRejectsOverlap(t *testing.T) {
	img := parseXBE(t, sampleSpec())
	over := NewImage(imgBase, img.EntryPoint, make([]byte, 0x1004), img.Sections)
	if _, _, err := over.Memory(); err == nil || !strings.Contains(err.Error(), "inside section") {
		t.Fatalf("err %v", err)
	}
	claim := &Image{BaseAddr: imgBase, HeadersSize: 0x20, raw: make([]byte, 0x10)}
	if _, _, err := claim.Memory(); err == nil || !strings.Contains(err.Error(), "claims") {
		t.Fatalf("err %v", err)
	}
}

func TestReadsAtSectionBounds(t *testing.T) {
	img := parseXBE(t, sampleSpec())
	if v, ok := img.ReadU8(0x11000); !ok || v != 0x55 {
		t.Fatalf("u8 %02X %v", v, ok)
	}
	if v, ok := img.ReadU8(0x1103F); !ok || v != 0 {
		t.Fatalf("last byte %02X %v", v, ok)
	}
	if _, ok := img.ReadU8(0x11040); ok {
		t.Fatal("byte past the section read")
	}
	if _, ok := img.ReadU8(0x10FFF); ok {
		t.Fatal("byte before the section read")
	}
	if v, ok := img.ReadU16(0x11001); !ok || v != 0xEC8B {
		t.Fatalf("u16 %04X %v", v, ok)
	}
	if _, ok := img.ReadU16(0x1103F); ok {
		t.Fatal("u16 crossing the end read")
	}
	if v, ok := img.ReadU32(0x12014); !ok || v != 0x11223344 {
		t.Fatalf("u32 %08X %v", v, ok)
	}
	if v, ok := img.ReadU32(0x12018); !ok || v != 0xDEADBEEF {
		t.Fatalf("u32 %08X %v", v, ok)
	}
	if _, ok := img.ReadU32(0x1103E); ok {
		t.Fatal("u32 past the virtual end read")
	}
	if v, ok := img.ReadU32(0x120FC); !ok || v != 0 {
		t.Fatalf("padded u32 %08X %v", v, ok)
	}
	if _, ok := img.ReadU32(0x120FD); ok {
		t.Fatal("u32 crossing the virtual end read")
	}
	b, ok := img.ReadBytes(0x11000, 5)
	if !ok || !bytes.Equal(b, []byte{0x55, 0x8B, 0xEC, 0x5D, 0xC3}) {
		t.Fatalf("bytes %x %v", b, ok)
	}
	if b, ok := img.ReadBytes(0x11030, 16); !ok || !bytes.Equal(b, make([]byte, 16)) {
		t.Fatalf("padded bytes %x %v", b, ok)
	}
	if _, ok := img.ReadBytes(0x11030, 17); ok {
		t.Fatal("bytes crossing the end read")
	}
	b[0] = 0xFF
	if img.Sections[0].Data[0] != 0x55 {
		t.Fatal("ReadBytes aliases the section")
	}
}

func TestReadUTF16Strings(t *testing.T) {
	spec := sampleSpec()
	mixed := wideBytes("Ab中c")
	mixed = append(mixed, wideBytes("xyz")[:6]...)
	spec.sections = append(spec.sections, testSection{name: ".str", va: 0x14000, flags: 0, data: mixed})
	img := parseXBE(t, spec)
	if s := img.ReadUTF16(0x12008); s != "Hello" {
		t.Fatalf("ascii %q", s)
	}
	if s := img.ReadUTF16(0x14000); s != "Abc" {
		t.Fatalf("narrowed %q", s)
	}
	if s := img.ReadWideString(0x14000); s != "Ab中c" {
		t.Fatalf("wide %q", s)
	}
	if s := img.ReadWideString(0x1400A); s != "xyz" {
		t.Fatalf("unterminated %q", s)
	}
	if s := img.ReadUTF16(0x1400A); s != "xyz" {
		t.Fatalf("unterminated narrow %q", s)
	}
	if s := img.ReadWideString(0x15000); s != "" {
		t.Fatalf("outside %q", s)
	}
}

func TestFindSection(t *testing.T) {
	img := parseXBE(t, sampleSpec())
	sec := img.FindSection(".data")
	if sec == nil || sec != &img.Sections[1] {
		t.Fatalf("section %p", sec)
	}
	if img.FindSection(".bss") != nil {
		t.Fatal("missing section found")
	}
}

func TestReadCString(t *testing.T) {
	data := []byte("ab\x00cd")
	if s := readCString(data, 0); s != "ab" {
		t.Fatalf("%q", s)
	}
	if s := readCString(data, 3); s != "cd" {
		t.Fatalf("%q", s)
	}
	if s := readCString(data, 5); s != "" {
		t.Fatalf("%q", s)
	}
	if s := readCString(data, -1); s != "" {
		t.Fatalf("%q", s)
	}
}

func TestReaderAt(t *testing.T) {
	r := readerAt([]byte{1, 2, 3})
	buf := make([]byte, 2)
	if n, err := r.ReadAt(buf, 1); n != 2 || err != nil || buf[0] != 2 || buf[1] != 3 {
		t.Fatalf("%d %v %x", n, err, buf)
	}
	if n, err := r.ReadAt(buf, 2); n != 1 || err == nil {
		t.Fatalf("%d %v", n, err)
	}
	if n, err := r.ReadAt(buf, 3); n != 0 || err == nil {
		t.Fatalf("%d %v", n, err)
	}
}

// Words inside the raw bytes of a read only section are constant and the zero padding past them is not
func TestConstantGlobalPastRawSize(t *testing.T) {
	spec := sampleSpec()
	spec.sections[1].flags = 0
	img := parseXBE(t, spec)
	d := disassembleImage(t, img)
	ex := NewExecutor(d)
	if !ex.isConstantGlobal(0x12014, 4) {
		t.Fatal("initialized read only word is not constant")
	}
	if ex.isConstantGlobal(0x12080, 4) {
		t.Fatal("padding past the raw size is constant")
	}
	if ex.isConstantGlobal(0x1201A, 4) {
		t.Fatal("word crossing the raw size is constant")
	}
	if ex.isConstantGlobal(0x15000, 4) {
		t.Fatal("word outside every section is constant")
	}
}
