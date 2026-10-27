package texture

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

const testHeaderSize = 32

// Packs the NV2A SET_TEXTURE_FORMAT register from its color format, mip count and log2 dimensions
func formatRegister(format PixelFormat, mips, log2W, log2H int) uint32 {
	return uint32(format)<<8 | uint32(mips)<<16 | uint32(log2W)<<20 | uint32(log2H)<<24
}

// Builds a complete XPR0 file with a 32 byte header around the given pixel data
func xprFile(magic string, resType uint16, formatReg uint32, data []byte) []byte {
	out := make([]byte, testHeaderSize, testHeaderSize+len(data))
	copy(out[0:4], magic)
	binary.LittleEndian.PutUint32(out[4:8], uint32(testHeaderSize+len(data)))
	binary.LittleEndian.PutUint32(out[8:12], testHeaderSize)
	binary.LittleEndian.PutUint16(out[12:14], 1)
	binary.LittleEndian.PutUint16(out[14:16], resType)
	binary.LittleEndian.PutUint32(out[24:28], formatReg)
	return append(out, data...)
}

func TestDecodeFormatRegister(t *testing.T) {
	cases := []struct {
		reg           uint32
		format        PixelFormat
		mips          int
		width, height int
	}{
		{formatRegister(FormatDXT1, 1, 2, 2), FormatDXT1, 1, 4, 4},
		{formatRegister(FormatA8R8G8B8, 9, 8, 6), FormatA8R8G8B8, 9, 256, 64},
		{formatRegister(FormatR5G6B5, 0, 0, 0), FormatR5G6B5, 0, 1, 1},
		{formatRegister(FormatDXT5, 15, 10, 1), FormatDXT5, 15, 1024, 2},
		{0xF2600503, FormatR5G6B5, 0, 64, 4},
	}
	for _, c := range cases {
		info := decodeFormatRegister(c.reg)
		if info.Format != c.format || info.MipLevels != c.mips || info.Width != c.width || info.Height != c.height {
			t.Errorf("decodeFormatRegister(%08X) = %+v, want format %s mips %d %dx%d", c.reg, info, c.format, c.mips, c.width, c.height)
		}
		if info.FormatReg != c.reg {
			t.Errorf("decodeFormatRegister(%08X).FormatReg = %08X", c.reg, info.FormatReg)
		}
	}
}

func TestReadXPR(t *testing.T) {
	data := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	reg := formatRegister(FormatR5G6B5, 2, 2, 1)
	file := xprFile("XPR0", 4, reg, data)
	tex, err := ReadXPR(bytes.NewReader(file), int64(len(file)))
	if err != nil {
		t.Fatalf("ReadXPR: %v", err)
	}
	want := TextureInfo{Width: 4, Height: 2, Format: FormatR5G6B5, MipLevels: 2, HeaderSize: testHeaderSize, DataSize: len(data), FormatReg: reg}
	if tex.Info != want {
		t.Errorf("Info = %+v, want %+v", tex.Info, want)
	}
	if !bytes.Equal(tex.Data, data) {
		t.Errorf("Data = %v, want %v", tex.Data, data)
	}
}

func TestReadXPRLargerHeader(t *testing.T) {
	data := []byte{0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF, 0x11, 0x22}
	file := make([]byte, 64)
	copy(file, "XPR0")
	binary.LittleEndian.PutUint32(file[4:], uint32(64+len(data)))
	binary.LittleEndian.PutUint32(file[8:], 64)
	binary.LittleEndian.PutUint16(file[14:], 4)
	binary.LittleEndian.PutUint32(file[24:], formatRegister(FormatDXT1, 1, 2, 2))
	file = append(file, data...)
	tex, err := ReadXPR(bytes.NewReader(file), int64(len(file)))
	if err != nil {
		t.Fatalf("ReadXPR: %v", err)
	}
	if tex.Info.HeaderSize != 64 || tex.Info.DataSize != len(data) {
		t.Errorf("HeaderSize %d DataSize %d, want 64 and %d", tex.Info.HeaderSize, tex.Info.DataSize, len(data))
	}
	if !bytes.Equal(tex.Data, data) {
		t.Errorf("Data = %x, want %x", tex.Data, data)
	}
}

func TestReadXPRErrors(t *testing.T) {
	good := xprFile("XPR0", 4, formatRegister(FormatDXT1, 1, 2, 2), make([]byte, 8))
	headerSize := func(size uint32) []byte {
		f := append([]byte(nil), good...)
		binary.LittleEndian.PutUint32(f[8:12], size)
		return f
	}
	cases := []struct {
		name string
		file []byte
		size int64
		want error
	}{
		{"short size", good, 15, ErrTruncatedFile},
		{"empty", nil, 0, ErrTruncatedFile},
		{"bad magic", xprFile("XPR1", 4, formatRegister(FormatDXT1, 1, 2, 2), make([]byte, 8)), 40, ErrInvalidMagic},
		{"lowercase magic", xprFile("xpr0", 4, 0, make([]byte, 8)), 40, ErrInvalidMagic},
		{"wrong resource type", xprFile("XPR0", 3, formatRegister(FormatDXT1, 1, 2, 2), make([]byte, 8)), 40, ErrNoTexture},
		{"header below fixed part", headerSize(12), 40, ErrTruncatedFile},
		{"header without format register", headerSize(20), 40, ErrTruncatedFile},
		{"truncated header", good[:24], 24, io.ErrUnexpectedEOF},
		{"truncated pixel data", good[:36], 36, io.ErrUnexpectedEOF},
		{"header only reader", good[:10], 40, io.ErrUnexpectedEOF},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tex, err := ReadXPR(bytes.NewReader(c.file), c.size)
			if !errors.Is(err, c.want) {
				t.Fatalf("err = %v, want %v", err, c.want)
			}
			if tex != nil {
				t.Errorf("texture = %+v, want nil", tex)
			}
		})
	}
}

func TestReadXPRHeaderBeyondTotalSize(t *testing.T) {
	file := xprFile("XPR0", 4, formatRegister(FormatDXT1, 1, 2, 2), make([]byte, 8))
	binary.LittleEndian.PutUint32(file[4:8], 16)
	var tex *Texture
	var err error
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("ReadXPR panicked on a total size below the header size, %v", r)
			}
		}()
		tex, err = ReadXPR(bytes.NewReader(file), int64(len(file)))
	}()
	if !errors.Is(err, ErrTruncatedFile) {
		t.Errorf("err = %v, want %v", err, ErrTruncatedFile)
	}
	if tex != nil {
		t.Errorf("texture = %+v, want nil", tex)
	}
}

func TestOpenXPR(t *testing.T) {
	dir := t.TempDir()
	data := []byte{9, 8, 7, 6, 5, 4, 3, 2}
	reg := formatRegister(FormatDXT1, 1, 2, 2)
	path := filepath.Join(dir, "tex.xbx")
	if err := os.WriteFile(path, xprFile("XPR0", 4, reg, data), 0o644); err != nil {
		t.Fatal(err)
	}
	tex, err := OpenXPR(path)
	if err != nil {
		t.Fatalf("OpenXPR: %v", err)
	}
	if tex.Info.Width != 4 || tex.Info.Height != 4 || tex.Info.Format != FormatDXT1 || tex.Info.DataSize != 8 {
		t.Errorf("Info = %+v", tex.Info)
	}
	if !bytes.Equal(tex.Data, data) {
		t.Errorf("Data = %v, want %v", tex.Data, data)
	}

	bad := filepath.Join(dir, "bad.xbx")
	if err := os.WriteFile(bad, xprFile("NOPE", 4, reg, data), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenXPR(bad); !errors.Is(err, ErrInvalidMagic) {
		t.Errorf("bad magic err = %v, want %v", err, ErrInvalidMagic)
	}

	short := filepath.Join(dir, "short.xbx")
	if err := os.WriteFile(short, []byte("XPR0"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenXPR(short); !errors.Is(err, ErrTruncatedFile) {
		t.Errorf("short file err = %v, want %v", err, ErrTruncatedFile)
	}

	if _, err := OpenXPR(filepath.Join(dir, "missing.xbx")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("missing file err = %v, want %v", err, fs.ErrNotExist)
	}
}

func TestDecodeUnsupportedFormat(t *testing.T) {
	file := xprFile("XPR0", 4, formatRegister(PixelFormat(0x3A), 1, 2, 2), make([]byte, 64))
	tex, err := ReadXPR(bytes.NewReader(file), int64(len(file)))
	if err != nil {
		t.Fatalf("ReadXPR: %v", err)
	}
	if tex.Info.Format != PixelFormat(0x3A) {
		t.Fatalf("Format = %s", tex.Info.Format)
	}
	if img, err := Decode(tex); !errors.Is(err, ErrUnsupportedFormat) || img != nil {
		t.Errorf("Decode = %v, %v, want nil, %v", img, err, ErrUnsupportedFormat)
	}
	var buf bytes.Buffer
	if err := ExportPNG(&buf, tex); !errors.Is(err, ErrUnsupportedFormat) {
		t.Errorf("ExportPNG err = %v, want %v", err, ErrUnsupportedFormat)
	}
	if buf.Len() != 0 {
		t.Errorf("ExportPNG wrote %d bytes on failure", buf.Len())
	}
}
