package xip

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func le16(b []byte, v uint16) []byte { return binary.LittleEndian.AppendUint16(b, v) }

func le32(b []byte, v uint32) []byte { return binary.LittleEndian.AppendUint32(b, v) }

func fixtureEntries() []WriteEntry {
	return []WriteEntry{
		{Name: "~0.vb", Type: FileTypeVB, Size: 8, Body: strings.NewReader("VBVBVBVB")},
		{Name: "tex/a.xbx", Type: FileTypeDir, Size: 3, Body: strings.NewReader("abc")},
		{Name: "b.xap", Type: FileTypeRegular, Size: 5, Body: strings.NewReader("hello")},
		{Name: "~0.ib", Type: FileTypeIB, Size: 6, Body: strings.NewReader("IBIBIB")},
	}
}

// Hand built image of the fixture with every offset computed from the record sizes
func fixtureBytes() []byte {
	b := []byte("XIP0")
	b = le32(b, 16+4*16+4*4+28)
	b = le16(b, 4)
	b = le16(b, 4)
	b = le32(b, 5+3+6+8)
	for _, fd := range [][4]uint32{
		{0, 5, 0, 0},
		{5, 3, 2, 0},
		{8, 6, 5, 0},
		{14, 8, 6, 0},
	} {
		for _, v := range fd {
			b = le32(b, v)
		}
	}
	for _, fn := range [][2]uint16{{0, 0}, {1, 6}, {2, 16}, {3, 22}} {
		b = le16(b, fn[0])
		b = le16(b, fn[1])
	}
	b = append(b, "b.xap\x00tex\\a.xbx\x00~0.ib\x00~0.vb\x00"...)
	return append(b, "helloabcIBIBIBVBVBVBVB"...)
}

var fixtureHeader = Header{Magic: magic, DataOffset: 124, NumFiles: 4, NumNames: 4, DataSize: 22}

var fixtureWant = []Entry{
	{Name: "b.xap", Offset: 0, Size: 5, Type: FileTypeRegular},
	{Name: "tex/a.xbx", Offset: 5, Size: 3, Type: FileTypeDir},
	{Name: "~0.ib", Offset: 8, Size: 6, Type: FileTypeIB},
	{Name: "~0.vb", Offset: 14, Size: 8, Type: FileTypeVB},
}

var fixtureBodies = map[string]string{
	"b.xap":     "hello",
	"tex/a.xbx": "abc",
	"~0.ib":     "IBIBIB",
	"~0.vb":     "VBVBVBVB",
}

func writeArchive(t *testing.T, entries []WriteEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := NewWriter(&buf)
	for _, e := range entries {
		w.Add(e)
	}
	if err := w.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	return buf.Bytes()
}

func readArchive(t *testing.T, buf []byte) *Reader {
	t.Helper()
	r, err := NewReader(bytes.NewReader(buf), int64(len(buf)))
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}
	return r
}

func readEntry(t *testing.T, r *Reader, name string) []byte {
	t.Helper()
	for _, e := range r.Entries() {
		if e.Name == name {
			data, err := io.ReadAll(r.OpenFile(e))
			if err != nil {
				t.Fatalf("reading %s: %v", name, err)
			}
			return data
		}
	}
	t.Fatalf("entry %q not found in %+v", name, r.Entries())
	return nil
}

func checkEntries(t *testing.T, got, want []Entry) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d entries, want %d\n got %+v\nwant %+v", len(got), len(want), got, want)
	}
	for i := range want {
		g, w := got[i], want[i]
		if g.Name != w.Name {
			t.Errorf("entry %d name %q, want %q", i, g.Name, w.Name)
		}
		if g.Type != w.Type {
			t.Errorf("entry %d (%s) type %d, want %d", i, w.Name, g.Type, w.Type)
		}
		if g.Offset != w.Offset {
			t.Errorf("entry %d (%s) offset %d, want %d", i, w.Name, g.Offset, w.Offset)
		}
		if g.Size != w.Size {
			t.Errorf("entry %d (%s) size %d, want %d", i, w.Name, g.Size, w.Size)
		}
		if g.Timestamp != w.Timestamp {
			t.Errorf("entry %d (%s) timestamp %d, want %d", i, w.Name, g.Timestamp, w.Timestamp)
		}
	}
}

func checkBodies(t *testing.T, r *Reader, want map[string]string) {
	t.Helper()
	for name, body := range want {
		if got := string(readEntry(t, r, name)); got != body {
			t.Errorf("%s body %q, want %q", name, got, body)
		}
	}
}

func checkFixture(t *testing.T, r *Reader) {
	t.Helper()
	if h := r.Header(); h != fixtureHeader {
		t.Errorf("header %+v, want %+v", h, fixtureHeader)
	}
	checkEntries(t, r.Entries(), fixtureWant)
	checkBodies(t, r, fixtureBodies)
}

type failWriter struct{ err error }

func (f failWriter) Write([]byte) (int, error) { return 0, f.err }

func TestFileTypeString(t *testing.T) {
	tests := []struct {
		t    FileType
		val  uint32
		want string
	}{
		{FileTypeRegular, 0, "file"},
		{FileTypeFile, 1, "file"},
		{FileTypeDir, 2, "resource"},
		{FileTypeMesh, 4, "mesh"},
		{FileTypeIB, 5, "ib"},
		{FileTypeVB, 6, "vb"},
		{3, 3, "unknown(3)"},
		{7, 7, "unknown(7)"},
		{0xFFFFFFFF, 4294967295, "unknown(4294967295)"},
	}
	for _, tc := range tests {
		if uint32(tc.t) != tc.val {
			t.Errorf("FileType %s has value %d, want %d", tc.want, uint32(tc.t), tc.val)
		}
		if got := tc.t.String(); got != tc.want {
			t.Errorf("FileType(%d).String() = %q, want %q", uint32(tc.t), got, tc.want)
		}
	}
}

func TestDecodeMeshEntry(t *testing.T) {
	tests := []struct {
		name string
		e    Entry
		want MeshMeta
	}{
		{"zero", Entry{Name: "m", Type: FileTypeMesh}, MeshMeta{}},
		{"pool 2 index 16", Entry{Offset: 2<<24 | 16, Size: 7}, MeshMeta{Pool: 2, IndexStart: 16, TriCount: 7}},
		{"pool 1 wide index", Entry{Offset: 1<<24 | 0x123456, Size: 99}, MeshMeta{Pool: 1, IndexStart: 0x123456, TriCount: 99}},
		{"max pool", Entry{Offset: 0xFF000000, Size: 1}, MeshMeta{Pool: 255, IndexStart: 0, TriCount: 1}},
		{"max index", Entry{Offset: 0x00FFFFFF}, MeshMeta{Pool: 0, IndexStart: 16777215}},
		{"all bits", Entry{Offset: 0xFFFFFFFF, Size: 0x7FFFFFFF}, MeshMeta{Pool: 255, IndexStart: 16777215, TriCount: 2147483647}},
	}
	for _, tc := range tests {
		if got := DecodeMeshEntry(tc.e); got != tc.want {
			t.Errorf("%s: DecodeMeshEntry(%+v) = %+v, want %+v", tc.name, tc.e, got, tc.want)
		}
	}
}

func TestWriterLayoutMatchesHandBuiltArchive(t *testing.T) {
	got := writeArchive(t, fixtureEntries())
	want := fixtureBytes()
	if !bytes.Equal(got, want) {
		t.Fatalf("writer output differs from hand built archive\n got % x\nwant % x", got, want)
	}
}

func TestReaderParsesHandBuiltArchive(t *testing.T) {
	checkFixture(t, readArchive(t, fixtureBytes()))
}

func TestWriterRoundTrip(t *testing.T) {
	checkFixture(t, readArchive(t, writeArchive(t, fixtureEntries())))
}

func TestWriterAlignsDataOffset(t *testing.T) {
	tests := []struct {
		name       string
		dataOffset uint32
	}{
		{"ab", 40},
		{"abc", 40},
		{"abcd", 44},
		{"a.xap", 44},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			buf := writeArchive(t, []WriteEntry{{Name: tc.name, Size: 3, Body: strings.NewReader("xyz")}})
			if got := binary.LittleEndian.Uint32(buf[4:8]); got != tc.dataOffset {
				t.Fatalf("data offset %d, want %d", got, tc.dataOffset)
			}
			if len(buf) != int(tc.dataOffset)+3 {
				t.Fatalf("archive length %d, want %d", len(buf), tc.dataOffset+3)
			}
			nameEnd := 36 + len(tc.name) + 1
			if got := string(buf[36:nameEnd]); got != tc.name+"\x00" {
				t.Errorf("name block %q, want %q", got, tc.name+"\x00")
			}
			for i, b := range buf[nameEnd:tc.dataOffset] {
				if b != 0 {
					t.Errorf("pad byte %d is %#x, want 0", i, b)
				}
			}
			if got := string(buf[tc.dataOffset:]); got != "xyz" {
				t.Errorf("data section %q, want %q", got, "xyz")
			}
			r := readArchive(t, buf)
			if got := r.Header().DataOffset; got != tc.dataOffset {
				t.Errorf("parsed data offset %d, want %d", got, tc.dataOffset)
			}
			checkEntries(t, r.Entries(), []Entry{{Name: tc.name, Size: 3}})
			checkBodies(t, r, map[string]string{tc.name: "xyz"})
		})
	}
}

func TestWriterKeepsNestedNames(t *testing.T) {
	buf := writeArchive(t, []WriteEntry{
		{Name: "a/d.txt", Size: 2, Body: strings.NewReader("dd")},
		{Name: "a/b/c.txt", Size: 1, Body: strings.NewReader("c")},
		{Name: "a/b/e.txt", Size: 3, Body: strings.NewReader("eee")},
	})
	if len(buf) != 110 {
		t.Fatalf("archive length %d, want 110", len(buf))
	}
	wantNames := "a\\b\\c.txt\x00a\\b\\e.txt\x00a\\d.txt\x00"
	if got := string(buf[76:104]); got != wantNames {
		t.Errorf("name block %q, want %q", got, wantNames)
	}
	if got := string(buf[104:]); got != "ceeedd" {
		t.Errorf("data section %q, want %q", got, "ceeedd")
	}
	r := readArchive(t, buf)
	wantHeader := Header{Magic: magic, DataOffset: 104, NumFiles: 3, NumNames: 3, DataSize: 6}
	if h := r.Header(); h != wantHeader {
		t.Errorf("header %+v, want %+v", h, wantHeader)
	}
	checkEntries(t, r.Entries(), []Entry{
		{Name: "a/b/c.txt", Offset: 0, Size: 1},
		{Name: "a/b/e.txt", Offset: 1, Size: 3},
		{Name: "a/d.txt", Offset: 4, Size: 2},
	})
	checkBodies(t, r, map[string]string{"a/b/c.txt": "c", "a/b/e.txt": "eee", "a/d.txt": "dd"})
}

func TestWriterBodySizeMismatch(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{"short", "abc", "expected 5 bytes, got 3"},
		{"long", "toolong", "expected 5 bytes, got 7"},
	}
	for _, tc := range tests {
		w := NewWriter(&bytes.Buffer{})
		w.Add(WriteEntry{Name: "x.bin", Size: 5, Body: strings.NewReader(tc.body)})
		err := w.Flush()
		if err == nil {
			t.Fatalf("%s: Flush succeeded, want error", tc.name)
		}
		if !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), "x.bin") {
			t.Errorf("%s: error %q, want it to name x.bin and say %q", tc.name, err, tc.want)
		}
	}
}

func TestWriterPropagatesWriteError(t *testing.T) {
	sentinel := errors.New("disk full")
	w := NewWriter(failWriter{sentinel})
	w.Add(WriteEntry{Name: "x.bin", Size: 1, Body: strings.NewReader("x")})
	if err := w.Flush(); !errors.Is(err, sentinel) {
		t.Fatalf("Flush error %v, want %v", err, sentinel)
	}
}

func TestEmptyArchiveRoundTrip(t *testing.T) {
	got := writeArchive(t, nil)
	want := le32(le16(le16(le32([]byte("XIP0"), 16), 0), 0), 0)
	if !bytes.Equal(got, want) {
		t.Fatalf("empty archive % x, want % x", got, want)
	}
	r := readArchive(t, got)
	if h := r.Header(); h != (Header{Magic: magic, DataOffset: 16}) {
		t.Errorf("header %+v, want data offset 16 and zero counts", h)
	}
	if n := len(r.Entries()); n != 0 {
		t.Errorf("%d entries, want 0", n)
	}
}

func TestOpenReadsArchiveFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.xip")
	if err := os.WriteFile(path, writeArchive(t, fixtureEntries()), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	checkFixture(t, r)
	if err := r.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := io.ReadAll(r.OpenFile(r.Entries()[1])); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("read after Close error %v, want %v", err, os.ErrClosed)
	}
}

func TestOpenErrors(t *testing.T) {
	dir := t.TempDir()
	badMagic := filepath.Join(dir, "bad.xip")
	if err := os.WriteFile(badMagic, append([]byte("XIP1"), make([]byte, 12)...), 0o644); err != nil {
		t.Fatal(err)
	}
	short := filepath.Join(dir, "short.xip")
	if err := os.WriteFile(short, []byte("XIP0"), 0o644); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		path string
		want error
	}{
		{"missing", filepath.Join(dir, "missing.xip"), os.ErrNotExist},
		{"bad magic", badMagic, ErrInvalidMagic},
		{"truncated", short, ErrTruncatedFile},
	}
	for _, tc := range tests {
		r, err := Open(tc.path)
		if !errors.Is(err, tc.want) {
			t.Errorf("%s: Open error %v, want %v", tc.name, err, tc.want)
		}
		if r != nil {
			t.Errorf("%s: Open returned reader %+v, want nil", tc.name, r)
		}
	}
}

func TestCloseWithoutFile(t *testing.T) {
	r := readArchive(t, fixtureBytes())
	if err := r.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	checkBodies(t, r, fixtureBodies)
}

func TestOpenFileIsRepeatable(t *testing.T) {
	r := readArchive(t, fixtureBytes())
	for i := range 3 {
		if got := string(readEntry(t, r, "tex/a.xbx")); got != "abc" {
			t.Errorf("read %d: %q, want %q", i, got, "abc")
		}
	}
}

func TestHandBuiltArchivePreservesMeshAndTimestamp(t *testing.T) {
	b := []byte("XIP0")
	b = le32(b, 16+2*16+2*4+4)
	b = le16(b, 2)
	b = le16(b, 2)
	b = le32(b, 2)
	for _, v := range []uint32{0x02000010, 7, 4, 0xDEADBEEF, 0, 2, 0, 7} {
		b = le32(b, v)
	}
	for _, v := range []uint16{1, 2, 0, 0} {
		b = le16(b, v)
	}
	b = append(b, "m\x00f\x00hi"...)
	r := readArchive(t, b)
	wantHeader := Header{Magic: magic, DataOffset: 60, NumFiles: 2, NumNames: 2, DataSize: 2}
	if h := r.Header(); h != wantHeader {
		t.Errorf("header %+v, want %+v", h, wantHeader)
	}
	checkEntries(t, r.Entries(), []Entry{
		{Name: "f", Offset: 0, Size: 2, Type: FileTypeRegular, Timestamp: 7},
		{Name: "m", Offset: 0x02000010, Size: 7, Type: FileTypeMesh, Timestamp: 0xDEADBEEF},
	})
	checkBodies(t, r, map[string]string{"f": "hi"})
	if got := DecodeMeshEntry(r.Entries()[1]); got != (MeshMeta{Pool: 2, IndexStart: 16, TriCount: 7}) {
		t.Errorf("mesh meta %+v, want pool 2 index 16 tris 7", got)
	}
}

func TestNewReaderErrors(t *testing.T) {
	tests := []struct {
		name   string
		mutate func([]byte) []byte
		want   error
	}{
		{"empty input", func(b []byte) []byte { return nil }, ErrTruncatedFile},
		{"header short by one byte", func(b []byte) []byte { return b[:15] }, ErrTruncatedFile},
		{"bad magic", func(b []byte) []byte { b[3] = '1'; return b }, ErrInvalidMagic},
		{"file records past data offset", func(b []byte) []byte { binary.LittleEndian.PutUint16(b[8:], 9); return b }, ErrTruncatedFile},
		{"name records past data offset", func(b []byte) []byte { binary.LittleEndian.PutUint16(b[10:], 40); return b }, ErrTruncatedFile},
		{"data offset past end", func(b []byte) []byte { binary.LittleEndian.PutUint32(b[4:], 147); return b }, ErrTruncatedFile},
		{"data size past end", func(b []byte) []byte { binary.LittleEndian.PutUint32(b[12:], 23); return b }, ErrTruncatedFile},
		{"body truncated", func(b []byte) []byte { return b[:145] }, ErrTruncatedFile},
		{"data index equals file count", func(b []byte) []byte { binary.LittleEndian.PutUint16(b[80:], 4); return b }, ErrInvalidIndex},
		{"name offset equals block size", func(b []byte) []byte { binary.LittleEndian.PutUint16(b[82:], 28); return b }, ErrInvalidOffset},
		{"name offset far past block", func(b []byte) []byte { binary.LittleEndian.PutUint16(b[82:], 0xFFFF); return b }, ErrInvalidOffset},
	}
	for _, tc := range tests {
		buf := tc.mutate(fixtureBytes())
		r, err := NewReader(bytes.NewReader(buf), int64(len(buf)))
		if !errors.Is(err, tc.want) {
			t.Errorf("%s: error %v, want %v", tc.name, err, tc.want)
		}
		if r != nil {
			t.Errorf("%s: reader %+v, want nil", tc.name, r)
		}
	}
}

func TestNewReaderAcceptsLastNameByte(t *testing.T) {
	buf := fixtureBytes()
	binary.LittleEndian.PutUint16(buf[82:], 27)
	r := readArchive(t, buf)
	want := slices.Clone(fixtureWant)
	want[0].Name = ""
	checkEntries(t, r.Entries(), want)
}

func TestExtractString(t *testing.T) {
	buf := []byte("ab\\c\x00xyz\x00tail")
	tests := []struct {
		off  int
		want string
	}{
		{0, "ab/c"},
		{2, "/c"},
		{4, ""},
		{5, "xyz"},
		{9, "tail"},
	}
	for _, tc := range tests {
		if got := extractString(buf, tc.off); got != tc.want {
			t.Errorf("extractString(off %d) = %q, want %q", tc.off, got, tc.want)
		}
	}
}

func TestAppendString(t *testing.T) {
	if got := string(appendString([]byte("p\x00"), "a/b/c")); got != "p\x00a\\b\\c\x00" {
		t.Errorf("appended %q, want %q", got, "p\x00a\\b\\c\x00")
	}
	if got := string(appendString(nil, "")); got != "\x00" {
		t.Errorf("appended %q, want %q", got, "\x00")
	}
}
