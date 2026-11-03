// Package coff reads Microsoft object files and the library archives that hold them
package coff

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"strconv"
	"strings"
)

const (
	FlagCOMDAT    = 0x00001000
	ClassExternal = 2
	ClassStatic   = 3
	ClassFile     = 103
	TypeFunction  = 0x20
)

// Member is one object held by a library archive
type Member struct {
	Name string
	Data []byte
}

// Section carries a section's bytes, the relocations applied within them and its line number table
type Section struct {
	Name   string
	Flags  uint32
	Data   []byte
	Relocs []Reloc
	Lines  []LineNumber
}

// LineNumber maps a section offset onto a line counted from the start of its routine
type LineNumber struct {
	Offset uint32
	Line   uint16
	Symbol int
}

// Reloc names the symbol patched into a section at an offset
type Reloc struct {
	Offset uint32
	Type   uint16
	Symbol string
}

// Symbol is one entry of the symbol table, a function begin record keeping its source line
type Symbol struct {
	Name    string
	Value   uint32
	Section int16
	Type    uint16
	Class   uint8
	Line    uint16
}

// Object is a parsed object file
type Object struct {
	Machine  uint16
	Sections []Section
	Symbols  []Symbol
}

// Lists the objects of a library, long names resolved through the names member
func ReadArchive(data []byte) ([]Member, error) {
	if !bytes.HasPrefix(data, []byte("!<arch>\n")) {
		return nil, fmt.Errorf("coff: not an archive")
	}
	var members []Member
	var longnames []byte
	for pos := 8; pos+60 <= len(data); {
		name := strings.TrimRight(string(data[pos:pos+16]), " ")
		size, err := strconv.Atoi(strings.TrimSpace(string(data[pos+48 : pos+58])))
		if err != nil || pos+60+size > len(data) {
			return nil, fmt.Errorf("coff: bad archive member header at %#x", pos)
		}
		body := data[pos+60 : pos+60+size]
		switch {
		case name == "//":
			longnames = body
		case name == "/":
		case strings.HasPrefix(name, "/"):
			off, err := strconv.Atoi(name[1:])
			if err != nil || off >= len(longnames) {
				return nil, fmt.Errorf("coff: bad long name %q", name)
			}
			end := bytes.IndexByte(longnames[off:], 0)
			if end < 0 {
				end = len(longnames) - off
			}
			members = append(members, Member{Name: string(longnames[off : off+end]), Data: body})
		default:
			members = append(members, Member{Name: strings.TrimSuffix(name, "/"), Data: body})
		}
		pos += 60 + size + size&1
	}
	return members, nil
}

// Finds an archive member by its full recorded name or its base name
func Find(members []Member, name string) (Member, bool) {
	want := strings.ToLower(strings.ReplaceAll(name, "\\", "/"))
	for _, m := range members {
		have := strings.ToLower(strings.ReplaceAll(m.Name, "\\", "/"))
		if have == want || strings.HasSuffix(have, "/"+want) {
			return m, true
		}
	}
	return Member{}, false
}

// Parses an object file's sections, relocations and symbols
func Parse(data []byte) (*Object, error) {
	if len(data) < 20 {
		return nil, fmt.Errorf("coff: %d bytes is too short for a header", len(data))
	}
	machine := binary.LittleEndian.Uint16(data)
	nsec := int(binary.LittleEndian.Uint16(data[2:]))
	symat := int(binary.LittleEndian.Uint32(data[8:]))
	nsym := int(binary.LittleEndian.Uint32(data[12:]))
	optsize := int(binary.LittleEndian.Uint16(data[16:]))
	if symat+18*nsym+4 > len(data) || 20+optsize+40*nsec > len(data) {
		return nil, fmt.Errorf("coff: tables lie outside the %d byte file", len(data))
	}
	strtab := data[symat+18*nsym:]
	name := func(raw []byte) string {
		if binary.LittleEndian.Uint32(raw) == 0 {
			off := int(binary.LittleEndian.Uint32(raw[4:]))
			if off < len(strtab) {
				if end := bytes.IndexByte(strtab[off:], 0); end >= 0 {
					return string(strtab[off : off+end])
				}
			}
			return ""
		}
		return string(bytes.TrimRight(raw, "\x00"))
	}
	o := &Object{Machine: machine}
	names := make([]string, nsym)
	for i := 0; i < nsym; {
		rec := data[symat+18*i:]
		s := Symbol{Name: name(rec[:8]), Value: binary.LittleEndian.Uint32(rec[8:]), Section: int16(binary.LittleEndian.Uint16(rec[12:])), Type: binary.LittleEndian.Uint16(rec[14:]), Class: rec[16]}
		if aux := int(rec[17]); s.Class == ClassFile && aux > 0 && 18+18*aux <= len(rec) {
			s.Name = string(bytes.TrimRight(rec[18:18+18*aux], "\x00"))
		} else if s.Name == ".bf" && aux > 0 && 36 <= len(rec) {
			s.Line = binary.LittleEndian.Uint16(rec[22:])
		}
		names[i] = s.Name
		o.Symbols = append(o.Symbols, s)
		i += 1 + int(rec[17])
	}
	for i := 0; i < nsec; i++ {
		h := data[20+optsize+40*i:]
		sec := Section{Flags: binary.LittleEndian.Uint32(h[36:])}
		sec.Name = string(bytes.TrimRight(h[:8], "\x00"))
		if strings.HasPrefix(sec.Name, "/") {
			if off, err := strconv.Atoi(sec.Name[1:]); err == nil && off < len(strtab) {
				if end := bytes.IndexByte(strtab[off:], 0); end >= 0 {
					sec.Name = string(strtab[off : off+end])
				}
			}
		}
		size, at := int(binary.LittleEndian.Uint32(h[16:])), int(binary.LittleEndian.Uint32(h[20:]))
		if at != 0 {
			if at+size > len(data) {
				return nil, fmt.Errorf("coff: section %s data lies outside the file", sec.Name)
			}
			sec.Data = data[at : at+size]
		} else {
			sec.Data = make([]byte, size)
		}
		relat, nrel := int(binary.LittleEndian.Uint32(h[24:])), int(binary.LittleEndian.Uint16(h[32:]))
		if relat+10*nrel > len(data) {
			return nil, fmt.Errorf("coff: section %s relocations lie outside the file", sec.Name)
		}
		for r := 0; r < nrel; r++ {
			rec := data[relat+10*r:]
			idx := int(binary.LittleEndian.Uint32(rec[4:]))
			if idx >= nsym {
				return nil, fmt.Errorf("coff: section %s relocation names symbol %d of %d", sec.Name, idx, nsym)
			}
			sec.Relocs = append(sec.Relocs, Reloc{Offset: binary.LittleEndian.Uint32(rec), Type: binary.LittleEndian.Uint16(rec[8:]), Symbol: names[idx]})
		}
		linat, nlin := int(binary.LittleEndian.Uint32(h[28:])), int(binary.LittleEndian.Uint16(h[34:]))
		if linat+6*nlin > len(data) {
			return nil, fmt.Errorf("coff: section %s line numbers lie outside the file", sec.Name)
		}
		for l := 0; l < nlin; l++ {
			rec := data[linat+6*l:]
			ln := LineNumber{Line: binary.LittleEndian.Uint16(rec[4:]), Symbol: -1}
			if ln.Line == 0 {
				ln.Symbol = int(binary.LittleEndian.Uint32(rec))
			} else {
				ln.Offset = binary.LittleEndian.Uint32(rec)
			}
			sec.Lines = append(sec.Lines, ln)
		}
		o.Sections = append(o.Sections, sec)
	}
	return o, nil
}

// Reports whether a section only carries debugging or linker directives
func (s Section) Debug() bool {
	return strings.HasPrefix(s.Name, ".debug$") || s.Name == ".drectve"
}

// Lists the defined functions with the section index and byte range each occupies
func (o *Object) Functions() []Function {
	var fns []Function
	for _, s := range o.Symbols {
		if s.Type != TypeFunction || s.Section <= 0 || int(s.Section) > len(o.Sections) || (s.Class != ClassExternal && s.Class != ClassStatic) {
			continue
		}
		end := uint32(len(o.Sections[s.Section-1].Data))
		for _, t := range o.Symbols {
			if t.Section == s.Section && t.Value > s.Value && t.Value < end && (t.Class == ClassExternal || t.Class == ClassStatic) && t.Type == TypeFunction {
				end = t.Value
			}
		}
		fns = append(fns, Function{Name: s.Name, Section: int(s.Section) - 1, Start: s.Value, End: end})
	}
	return fns
}

// Function is a defined routine's place in its object
type Function struct {
	Name    string
	Section int
	Start   uint32
	End     uint32
}

// Returns the function's bytes and the relocations within them, offsets made relative to its start
func (o *Object) Body(f Function) ([]byte, []Reloc) {
	sec := o.Sections[f.Section]
	var relocs []Reloc
	for _, r := range sec.Relocs {
		if r.Offset >= f.Start && r.Offset < f.End {
			relocs = append(relocs, Reloc{Offset: r.Offset - f.Start, Type: r.Type, Symbol: r.Symbol})
		}
	}
	return sec.Data[f.Start:f.End], relocs
}
