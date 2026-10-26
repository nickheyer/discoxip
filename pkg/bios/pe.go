package bios

import (
	"encoding/binary"
	"fmt"

	"github.com/nickheyer/discoxip/pkg/xbe"
)

// PESection is one section of the kernel image as its header lays it out
type PESection struct {
	Name            string
	VirtualAddress  uint32
	VirtualSize     uint32
	RawOffset       uint32
	RawSize         uint32
	Characteristics uint32
}

// PE is the kernel's portable executable header, read from the decompressed image
type PE struct {
	Machine        uint16
	TimeDateStamp  uint32
	ImageBase      uint32
	EntryPoint     uint32
	SizeOfImage    uint32
	SizeOfHeaders  uint32
	ExportRVA      uint32
	ExportSize     uint32
	Sections       []PESection
	Characteristic uint16
	Image          []byte
}

// ParsePE reads the headers of a 32 bit image
func ParsePE(image []byte) (*PE, error) {
	if len(image) < 0x40 || image[0] != 'M' || image[1] != 'Z' {
		return nil, fmt.Errorf("pe: no MZ header")
	}
	off := int(binary.LittleEndian.Uint32(image[0x3C:]))
	if off+24 > len(image) || string(image[off:off+4]) != "PE\x00\x00" {
		return nil, fmt.Errorf("pe: no PE signature at %#x", off)
	}
	coff := image[off+4:]
	pe := &PE{Machine: binary.LittleEndian.Uint16(coff), TimeDateStamp: binary.LittleEndian.Uint32(coff[4:]), Characteristic: binary.LittleEndian.Uint16(coff[18:]), Image: image}
	sections := int(binary.LittleEndian.Uint16(coff[2:]))
	optSize := int(binary.LittleEndian.Uint16(coff[16:]))
	opt := image[off+24:]
	if optSize < 96 || len(opt) < optSize {
		return nil, fmt.Errorf("pe: optional header of %d bytes", optSize)
	}
	if binary.LittleEndian.Uint16(opt) != 0x10B {
		return nil, fmt.Errorf("pe: optional header magic %04X is not a 32 bit image", binary.LittleEndian.Uint16(opt))
	}
	pe.EntryPoint = binary.LittleEndian.Uint32(opt[16:])
	pe.ImageBase = binary.LittleEndian.Uint32(opt[28:])
	pe.SizeOfImage = binary.LittleEndian.Uint32(opt[56:])
	pe.SizeOfHeaders = binary.LittleEndian.Uint32(opt[60:])
	if dirs := binary.LittleEndian.Uint32(opt[92:]); dirs > 0 && optSize >= 104 {
		pe.ExportRVA = binary.LittleEndian.Uint32(opt[96:])
		pe.ExportSize = binary.LittleEndian.Uint32(opt[100:])
	}
	at := off + 24 + optSize
	for i := 0; i < sections; i++ {
		if at+40 > len(image) {
			return nil, fmt.Errorf("pe: section header %d runs past the image", i)
		}
		h := image[at:]
		name := h[:8]
		for len(name) > 0 && name[len(name)-1] == 0 {
			name = name[:len(name)-1]
		}
		pe.Sections = append(pe.Sections, PESection{
			Name:            string(name),
			VirtualSize:     binary.LittleEndian.Uint32(h[8:]),
			VirtualAddress:  binary.LittleEndian.Uint32(h[12:]),
			RawSize:         binary.LittleEndian.Uint32(h[16:]),
			RawOffset:       binary.LittleEndian.Uint32(h[20:]),
			Characteristics: binary.LittleEndian.Uint32(h[36:]),
		})
		at += 40
	}
	return pe, nil
}

// Export is one entry of the kernel's export table, by the ordinal the thunks name it with
type Export struct {
	Ordinal uint32
	RVA     uint32
}

// Exports reads the export table, the kernel naming its exports by ordinal alone
func (pe *PE) Exports() ([]Export, error) {
	if pe.ExportRVA == 0 {
		return nil, fmt.Errorf("pe: the image has no export table")
	}
	dir, ok := pe.bytesAt(pe.ExportRVA, 40)
	if !ok {
		return nil, fmt.Errorf("pe: export directory at %#x lies outside the image", pe.ExportRVA)
	}
	base := binary.LittleEndian.Uint32(dir[16:])
	count := binary.LittleEndian.Uint32(dir[20:])
	funcs := binary.LittleEndian.Uint32(dir[28:])
	table, ok := pe.bytesAt(funcs, int(count)*4)
	if !ok {
		return nil, fmt.Errorf("pe: export address table at %#x lies outside the image", funcs)
	}
	var out []Export
	for i := uint32(0); i < count; i++ {
		rva := binary.LittleEndian.Uint32(table[4*i:])
		if rva != 0 {
			out = append(out, Export{Ordinal: base + i, RVA: rva})
		}
	}
	return out, nil
}

// The bytes at an RVA, through the section holding it or straight from an image laid out as loaded
func (pe *PE) bytesAt(rva uint32, n int) ([]byte, bool) {
	for _, s := range pe.Sections {
		if rva >= s.VirtualAddress && rva+uint32(n) <= s.VirtualAddress+s.RawSize {
			off := int(s.RawOffset + rva - s.VirtualAddress)
			if off+n <= len(pe.Image) {
				return pe.Image[off : off+n], true
			}
		}
	}
	if int(rva)+n <= len(pe.Image) {
		return pe.Image[rva : int(rva)+n], true
	}
	return nil, false
}

// XBEImage lays the kernel out as the loaded image the disassembler takes, each section at its virtual address
func (pe *PE) XBEImage() *xbe.Image {
	headers := int(pe.SizeOfHeaders)
	if headers > len(pe.Image) {
		headers = len(pe.Image)
	}
	var sections []xbe.Section
	for _, s := range pe.Sections {
		// The sections sit back to back at their raw sizes, which the padding past the virtual size fills out
		size := max(s.RawSize, s.VirtualSize)
		data := make([]byte, size)
		end := min(int(s.RawOffset)+int(s.RawSize), len(pe.Image))
		if int(s.RawOffset) < end {
			copy(data, pe.Image[s.RawOffset:end])
		}
		// Executable and writable sections take the flag bits the XBE loader reads
		flags := uint32(0)
		if s.Characteristics&0x80000000 != 0 {
			flags |= 0x1
		}
		if s.Characteristics&0x20000000 != 0 {
			flags |= 0x4
		}
		sections = append(sections, xbe.Section{Name: s.Name, VirtualAddr: pe.ImageBase + s.VirtualAddress, VirtualSize: size, RawAddr: s.RawOffset, RawSize: s.RawSize, Flags: flags, Data: data})
	}
	return xbe.NewImage(pe.ImageBase, pe.ImageBase+pe.EntryPoint, pe.Image[:headers], sections)
}
