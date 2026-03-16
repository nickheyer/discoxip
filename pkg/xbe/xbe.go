// Package xbe reads Xbox Executable (XBE) files.
//
// The XBE format is documented in the ghidra-xbe loader and the Xbox
// development community. This package parses the image header, section
// table, and kernel thunk imports, then provides virtual-address-based
// memory access for analysis of the loaded executable.
package xbe

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
)

// XOR keys for unscrambling the entry point and kernel thunk address.
// Retail XBEs use these constants; debug and Sega builds use different keys.
const (
	entryRetail  uint32 = 0xA8FC57AB
	entryDebug   uint32 = 0x94859D4B
	entrySega    uint32 = 0x40B5C16E
	kthunkRetail uint32 = 0x5B6D40B6
	kthunkDebug  uint32 = 0xEFB1F152
	kthunkSega   uint32 = 0x2290059D
)

// rawHeader is the on-disk XBE image header (first 0x178+ bytes).
type rawHeader struct {
	Magic              [4]byte  // "XBEH"
	Signature          [256]byte
	BaseAddr           uint32
	HeadersSize        uint32
	ImageSize          uint32
	ImageHeaderSize    uint32
	Timestamp          uint32
	CertificateAddr    uint32
	SectionCount       uint32
	SectionHeadersAddr uint32
	InitFlags          uint32
	EntryAddr          uint32 // XOR-scrambled
	TLSAddr            uint32
	PEStackCommit      uint32
	PEHeapReserve      uint32
	PEHeapCommit       uint32
	PEBaseAddr         uint32
	PEImageSize        uint32
	PEChecksum         uint32
	PETimestamp        uint32
	DebugPathnameAddr  uint32
	DebugFilenameAddr  uint32
	DebugUnicodeAddr   uint32
	KernThunkAddr      uint32 // XOR-scrambled
	ImportDirAddr      uint32
	LibVersionsCount   uint32
	LibVersionsAddr    uint32
	KernLibVersionAddr uint32
	XAPILibVersionAddr uint32
	LogoAddr           uint32
	LogoSize           uint32
}

// rawSection is the on-disk section header (0x38 bytes).
type rawSection struct {
	Flags                    uint32
	VirtualAddr              uint32
	VirtualSize              uint32
	RawAddr                  uint32
	RawSize                  uint32
	SectionNameAddr          uint32
	SectionNameRefCount      uint32
	HeadSharedPageRefCount   uint32
	TailSharedPageRefCount   uint32
	Digest                   [20]byte
}

// Section is a loaded XBE section with name and virtual address range.
type Section struct {
	Name        string
	VirtualAddr uint32
	VirtualSize uint32
	RawAddr     uint32
	RawSize     uint32
	Flags       uint32
	Data        []byte // loaded into virtual size (zero-padded)
}

// Image is a parsed XBE image with sections mapped by virtual address.
type Image struct {
	BaseAddr   uint32
	EntryPoint uint32 // unscrambled
	KernThunk  uint32 // unscrambled
	Sections   []Section
	raw        []byte // entire file contents for random access
}

// Open reads and parses an XBE file from disk.
func Open(path string) (*Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	data, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}

	return Parse(data)
}

// Parse parses an XBE from raw bytes.
func Parse(data []byte) (*Image, error) {
	if len(data) < 4 || string(data[:4]) != "XBEH" {
		return nil, fmt.Errorf("xbe: invalid magic (expected XBEH)")
	}

	var h rawHeader
	if err := binary.Read(
		io.NewSectionReader(readerAt(data), 0, int64(len(data))),
		binary.LittleEndian, &h,
	); err != nil {
		return nil, fmt.Errorf("xbe: reading header: %w", err)
	}

	img := &Image{
		BaseAddr: h.BaseAddr,
		raw:      data,
	}

	// Unscramble entry point and kernel thunk address.
	// Detection logic from ghidra-xbe XbeLoader.java.
	entry := h.EntryAddr
	kthunk := h.KernThunkAddr
	if entry&0xF0000000 == 0x40000000 {
		img.EntryPoint = entry ^ entrySega
		img.KernThunk = kthunk ^ kthunkSega
	} else if (entry^entryDebug) < 0x4000000 {
		img.EntryPoint = entry ^ entryDebug
		img.KernThunk = kthunk ^ kthunkDebug
	} else {
		img.EntryPoint = entry ^ entryRetail
		img.KernThunk = kthunk ^ kthunkRetail
	}

	// Read section headers.
	secOff := int(h.SectionHeadersAddr - h.BaseAddr)
	for i := 0; i < int(h.SectionCount); i++ {
		off := secOff + i*0x38
		if off+0x38 > len(data) {
			break
		}
		var sh rawSection
		if err := binary.Read(
			io.NewSectionReader(readerAt(data), int64(off), 0x38),
			binary.LittleEndian, &sh,
		); err != nil {
			return nil, fmt.Errorf("xbe: reading section %d: %w", i, err)
		}

		// Read section name from the name pointer.
		nameOff := int(sh.SectionNameAddr - h.BaseAddr)
		name := readCString(data, nameOff)

		// Load section data (raw), zero-extend to virtual size.
		secData := make([]byte, sh.VirtualSize)
		rawEnd := int(sh.RawAddr) + int(sh.RawSize)
		if rawEnd > len(data) {
			rawEnd = len(data)
		}
		if int(sh.RawAddr) < rawEnd {
			copy(secData, data[sh.RawAddr:rawEnd])
		}

		img.Sections = append(img.Sections, Section{
			Name:        name,
			VirtualAddr: sh.VirtualAddr,
			VirtualSize: sh.VirtualSize,
			RawAddr:     sh.RawAddr,
			RawSize:     sh.RawSize,
			Flags:       sh.Flags,
			Data:        secData,
		})
	}

	return img, nil
}

// ReadU8 reads a byte at a virtual address.
func (img *Image) ReadU8(va uint32) (byte, bool) {
	for i := range img.Sections {
		s := &img.Sections[i]
		if va >= s.VirtualAddr && va < s.VirtualAddr+s.VirtualSize {
			return s.Data[va-s.VirtualAddr], true
		}
	}
	return 0, false
}

// ReadU16 reads a little-endian uint16 at a virtual address.
func (img *Image) ReadU16(va uint32) (uint16, bool) {
	for i := range img.Sections {
		s := &img.Sections[i]
		off := va - s.VirtualAddr
		if va >= s.VirtualAddr && off+2 <= s.VirtualSize {
			return binary.LittleEndian.Uint16(s.Data[off:]), true
		}
	}
	return 0, false
}

// ReadU32 reads a little-endian uint32 at a virtual address.
func (img *Image) ReadU32(va uint32) (uint32, bool) {
	for i := range img.Sections {
		s := &img.Sections[i]
		off := va - s.VirtualAddr
		if va >= s.VirtualAddr && off+4 <= s.VirtualSize {
			return binary.LittleEndian.Uint32(s.Data[off:]), true
		}
	}
	return 0, false
}

// ReadBytes reads n bytes starting at a virtual address.
func (img *Image) ReadBytes(va uint32, n int) ([]byte, bool) {
	for i := range img.Sections {
		s := &img.Sections[i]
		off := va - s.VirtualAddr
		if va >= s.VirtualAddr && off+uint32(n) <= s.VirtualSize {
			out := make([]byte, n)
			copy(out, s.Data[off:off+uint32(n)])
			return out, true
		}
	}
	return nil, false
}

// ReadUTF16 reads a null-terminated UTF-16LE string at a virtual address.
func (img *Image) ReadUTF16(va uint32) string {
	var result []byte
	for addr := va; ; addr += 2 {
		lo, ok1 := img.ReadU8(addr)
		hi, ok2 := img.ReadU8(addr + 1)
		if !ok1 || !ok2 {
			break
		}
		if lo == 0 && hi == 0 {
			break
		}
		// Only handle BMP characters for material names (ASCII range).
		if hi == 0 {
			result = append(result, lo)
		}
	}
	return string(result)
}

// FindSection returns the section with the given name, or nil.
func (img *Image) FindSection(name string) *Section {
	for i := range img.Sections {
		if img.Sections[i].Name == name {
			return &img.Sections[i]
		}
	}
	return nil
}

// readCString reads a null-terminated ASCII string from a byte slice.
func readCString(data []byte, off int) string {
	if off < 0 || off >= len(data) {
		return ""
	}
	end := off
	for end < len(data) && data[end] != 0 {
		end++
	}
	return string(data[off:end])
}

// readerAt wraps a byte slice as an io.ReaderAt.
type readerAt []byte

func (r readerAt) ReadAt(p []byte, off int64) (int, error) {
	if off >= int64(len(r)) {
		return 0, io.EOF
	}
	n := copy(p, r[off:])
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}
