// Package xbe reads Xbox Executable (XBE) files.
//
// The XBE format is documented in the ghidra-xbe loader and the Xbox
// development community. This package parses the image header, section
// table, and kernel thunk imports, then provides virtual-address-based
// memory access for analysis of the loaded executable.
package xbe

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"unicode/utf16"
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

// Magic opens every executable image
const Magic = "XBEH"

// rawHeader is the on-disk XBE image header (first 0x178+ bytes).
type rawHeader struct {
	Magic              [4]byte
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
	Flags                  uint32
	VirtualAddr            uint32
	VirtualSize            uint32
	RawAddr                uint32
	RawSize                uint32
	SectionNameAddr        uint32
	SectionNameRefCount    uint32
	HeadSharedPageRefCount uint32
	TailSharedPageRefCount uint32
	Digest                 [20]byte
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

// LibVersion is one entry of the XBE library version table.
type LibVersion struct {
	Name  string
	Major uint16
	Minor uint16
	Build uint16
}

// Image is a parsed XBE image with sections mapped by virtual address.
type Image struct {
	BaseAddr    uint32
	EntryPoint  uint32 // unscrambled
	KernThunk   uint32 // unscrambled
	HeadersSize uint32
	Sections    []Section
	Libs        []LibVersion
	TLS         *TLSDirectory // the thread local storage the loader gives each thread, nil where the header names none
	raw         []byte        // entire file contents for random access
}

// TLSDirectory is the image's thread local storage directory, the template every thread's block starts as copied
// from Start to End and ZeroFill more bytes cleared after it, the index word at Index numbering the module's slot
type TLSDirectory struct {
	Start     uint32 `json:"start"`
	End       uint32 `json:"end"`
	Index     uint32 `json:"index"`
	Callbacks uint32 `json:"callbacks"`
	ZeroFill  uint32 `json:"zero_fill"`
}

// NewImage builds an image from sections already laid out in memory, the header being the bytes at its base
func NewImage(base, entry uint32, header []byte, sections []Section) *Image {
	return &Image{BaseAddr: base, EntryPoint: entry, HeadersSize: uint32(len(header)), Sections: sections, raw: header}
}

// HeaderBytes is the header the loader maps at the base address
func (img *Image) HeaderBytes() []byte {
	if int(img.HeadersSize) > len(img.raw) {
		return img.raw
	}
	return img.raw[:img.HeadersSize]
}

// LoadedHeader is the mapped header once every section has been loaded, each section counted as referenced once
func (img *Image) LoadedHeader() ([]byte, error) {
	out := append([]byte(nil), img.HeaderBytes()...)
	var h rawHeader
	if err := binary.Read(bytes.NewReader(out), binary.LittleEndian, &h); err != nil {
		return nil, fmt.Errorf("xbe: reading the header: %w", err)
	}
	refCount := uint32(binary.Size(rawSection{}.Flags) * 6)
	size := uint32(binary.Size(rawSection{}))
	for i := uint32(0); i < h.SectionCount; i++ {
		at := h.SectionHeadersAddr - img.BaseAddr + i*size + refCount
		if int(at)+4 > len(out) {
			return nil, fmt.Errorf("xbe: section header %d lies outside the mapped header", i)
		}
		binary.LittleEndian.PutUint32(out[at:], 1)
	}
	return out, nil
}

// Returns the XDK build number of the named library or 0 when absent
func (img *Image) LibBuild(name string) uint16 {
	for _, l := range img.Libs {
		if strings.EqualFold(l.Name, name) {
			return l.Build
		}
	}
	return 0
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
	if len(data) < len(Magic) || string(data[:len(Magic)]) != Magic {
		return nil, fmt.Errorf("xbe: invalid magic (expected %s)", Magic)
	}

	var h rawHeader
	if err := binary.Read(
		io.NewSectionReader(readerAt(data), 0, int64(len(data))),
		binary.LittleEndian, &h,
	); err != nil {
		return nil, fmt.Errorf("xbe: reading header: %w", err)
	}

	img := &Image{
		BaseAddr:    h.BaseAddr,
		HeadersSize: h.HeadersSize,
		raw:         data,
	}

	// Unscramble entry point and kernel thunk address.
	// Detection logic from ghidra-xbe XbeLoader.java.
	entry := h.EntryAddr
	kthunk := h.KernThunkAddr
	if entry&0xF0000000 == 0x40000000 {
		img.EntryPoint = entry ^ entrySega
		img.KernThunk = kthunk ^ kthunkSega
	} else if (entry ^ entryDebug) < 0x4000000 {
		img.EntryPoint = entry ^ entryDebug
		img.KernThunk = kthunk ^ kthunkDebug
	} else {
		img.EntryPoint = entry ^ entryRetail
		img.KernThunk = kthunk ^ kthunkRetail
	}

	if h.TLSAddr != 0 {
		off := int(h.TLSAddr - h.BaseAddr)
		if off < 0 || off+20 > len(data) {
			return nil, fmt.Errorf("xbe: TLS directory at %08X lies outside the headers", h.TLSAddr)
		}
		var t TLSDirectory
		if err := binary.Read(io.NewSectionReader(readerAt(data), int64(off), 20), binary.LittleEndian, &t); err != nil {
			return nil, fmt.Errorf("xbe: reading TLS directory: %w", err)
		}
		img.TLS = &t
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
			return nil, fmt.Errorf("xbe: section %s raw data at %X+%X lies past the %d byte file", name, sh.RawAddr, sh.RawSize, len(data))
		}
		copy(secData, data[sh.RawAddr:rawEnd])

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

	// Read the library version table.
	libOff := int(h.LibVersionsAddr - h.BaseAddr)
	for i := 0; i < int(h.LibVersionsCount); i++ {
		off := libOff + i*16
		if off < 0 || off+16 > len(data) {
			break
		}
		img.Libs = append(img.Libs, LibVersion{
			Name:  readCString(data[off:off+8], 0),
			Major: binary.LittleEndian.Uint16(data[off+8:]),
			Minor: binary.LittleEndian.Uint16(data[off+10:]),
			Build: binary.LittleEndian.Uint16(data[off+12:]),
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

// ReadWideString reads a null-terminated UTF-16LE string at a virtual address, keeping every character.
func (img *Image) ReadWideString(va uint32) string {
	var units []uint16
	for addr := va; ; addr += 2 {
		c, ok := img.ReadU16(addr)
		if !ok || c == 0 {
			break
		}
		units = append(units, c)
	}
	return string(utf16.Decode(units))
}

// MemoryFile is the name the exported startup memory is written under
const MemoryFile = "image.bin"

// ImageSection is where one loaded section sits in the startup memory and in the file holding it
type ImageSection struct {
	Name     string `json:"name"`
	VA       uint32 `json:"va"`
	Size     uint32 `json:"size"`
	Off      uint32 `json:"off"`
	Writable bool   `json:"writable"`
}

// ImageMemory is the loaded image the runtime reads as the memory the console starts the executable with
type ImageMemory struct {
	File     string         `json:"file"`
	Base     uint32         `json:"base"`
	Sections []ImageSection `json:"sections"`
}

// Lays every loaded section out in address order as the memory the console starts the executable with
func (img *Image) Memory() (ImageMemory, []byte, error) {
	order := make([]*Section, 0, len(img.Sections))
	for i := range img.Sections {
		order = append(order, &img.Sections[i])
	}
	sort.Slice(order, func(i, j int) bool { return order[i].VirtualAddr < order[j].VirtualAddr })
	mem := ImageMemory{File: MemoryFile, Base: img.BaseAddr}
	var bytes []byte
	// The loader maps the header at the base address, the certificate the executable reads its title from inside it
	if int(img.HeadersSize) > len(img.raw) {
		return mem, nil, fmt.Errorf("xbe: the header claims %d bytes of a %d byte file", img.HeadersSize, len(img.raw))
	}
	if len(order) > 0 && img.BaseAddr+img.HeadersSize > order[0].VirtualAddr {
		return mem, nil, fmt.Errorf("xbe: the header ends at %08X inside section %s at %08X", img.BaseAddr+img.HeadersSize, order[0].Name, order[0].VirtualAddr)
	}
	mem.Sections = append(mem.Sections, ImageSection{Name: "header", VA: img.BaseAddr, Size: img.HeadersSize, Off: 0, Writable: false})
	bytes = append(bytes, img.raw[:img.HeadersSize]...)
	for _, s := range order {
		mem.Sections = append(mem.Sections, ImageSection{Name: s.Name, VA: s.VirtualAddr, Size: s.VirtualSize, Off: uint32(len(bytes)), Writable: s.Flags&0x1 != 0})
		bytes = append(bytes, s.Data...)
	}
	return mem, bytes, nil
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
