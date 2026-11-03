// Package pdb reads Microsoft program databases, both the 2.00 and the 7.00 container
package pdb

import (
	"bytes"
	"encoding/binary"
	"fmt"

	"github.com/nickheyer/discoxip/pkg/native/cv"
)

// Module is one compiland the linker recorded
type Module struct {
	Name     string
	Object   string
	Stream   int
	Files    []string
	Section  uint16
	Offset   uint32
	Size     uint32
	SymBytes int
	C11Bytes int
	C13Bytes int
}

// File is an opened program database
type File struct {
	Streams [][]byte
	Old     bool
	Modules []Module
	symrec  int
}

// Open parses the container and the debug information header
func Open(data []byte) (*File, error) {
	f := &File{}
	var err error
	switch {
	case bytes.HasPrefix(data, []byte("Microsoft C/C++ program database 2.00")):
		f.Old = true
		err = f.readOld(data)
	case bytes.HasPrefix(data, []byte("Microsoft C/C++ MSF 7.00")):
		err = f.readNew(data)
	default:
		return nil, fmt.Errorf("pdb: not a program database")
	}
	if err != nil {
		return nil, err
	}
	return f, f.readDBI()
}

func (f *File) readOld(data []byte) error {
	page := int(binary.LittleEndian.Uint32(data[0x2C:]))
	rootSize := int(binary.LittleEndian.Uint32(data[0x34:]))
	n := (rootSize + page - 1) / page
	var root []byte
	for i := 0; i < n; i++ {
		p := int(binary.LittleEndian.Uint16(data[0x3C+2*i:]))
		if (p+1)*page > len(data) {
			return fmt.Errorf("pdb: root page %d lies outside the file", p)
		}
		root = append(root, data[p*page:(p+1)*page]...)
	}
	root = root[:rootSize]
	count := int(binary.LittleEndian.Uint16(root))
	pos := 4 + 8*count
	for i := 0; i < count; i++ {
		size := binary.LittleEndian.Uint32(root[4+8*i:])
		if size == 0xFFFFFFFF {
			f.Streams = append(f.Streams, nil)
			continue
		}
		pages := (int(size) + page - 1) / page
		var s []byte
		for j := 0; j < pages; j++ {
			p := int(binary.LittleEndian.Uint16(root[pos:]))
			pos += 2
			if (p+1)*page > len(data) {
				return fmt.Errorf("pdb: stream %d page %d lies outside the file", i, p)
			}
			s = append(s, data[p*page:(p+1)*page]...)
		}
		f.Streams = append(f.Streams, s[:size])
	}
	return nil
}

func (f *File) readNew(data []byte) error {
	page := int(binary.LittleEndian.Uint32(data[0x20:]))
	dirBytes := int(binary.LittleEndian.Uint32(data[0x2C:]))
	mapAt := int(binary.LittleEndian.Uint32(data[0x34:]))
	n := (dirBytes + page - 1) / page
	var root []byte
	for i := 0; i < n; i++ {
		p := int(binary.LittleEndian.Uint32(data[mapAt*page+4*i:]))
		if (p+1)*page > len(data) {
			return fmt.Errorf("pdb: directory page %d lies outside the file", p)
		}
		root = append(root, data[p*page:(p+1)*page]...)
	}
	root = root[:dirBytes]
	count := int(binary.LittleEndian.Uint32(root))
	pos := 4 + 4*count
	for i := 0; i < count; i++ {
		size := binary.LittleEndian.Uint32(root[4+4*i:])
		if size == 0xFFFFFFFF {
			f.Streams = append(f.Streams, nil)
			continue
		}
		pages := (int(size) + page - 1) / page
		var s []byte
		for j := 0; j < pages; j++ {
			p := int(binary.LittleEndian.Uint32(root[pos:]))
			pos += 4
			if (p+1)*page > len(data) {
				return fmt.Errorf("pdb: stream %d page %d lies outside the file", i, p)
			}
			s = append(s, data[p*page:(p+1)*page]...)
		}
		f.Streams = append(f.Streams, s[:size])
	}
	return nil
}

// Reads the debug information stream's module list and file tables
func (f *File) readDBI() error {
	if len(f.Streams) < 4 || len(f.Streams[3]) < 64 {
		return fmt.Errorf("pdb: no debug information stream")
	}
	dbi := f.Streams[3]
	if int32(binary.LittleEndian.Uint32(dbi)) != -1 {
		return fmt.Errorf("pdb: debug information header signature %#x is not understood", binary.LittleEndian.Uint32(dbi))
	}
	f.symrec = int(binary.LittleEndian.Uint16(dbi[20:]))
	cbModi := int(binary.LittleEndian.Uint32(dbi[24:]))
	cbSC := int(binary.LittleEndian.Uint32(dbi[28:]))
	cbSecMap := int(binary.LittleEndian.Uint32(dbi[32:]))
	cbFileInfo := int(binary.LittleEndian.Uint32(dbi[36:]))
	if 64+cbModi+cbSC+cbSecMap+cbFileInfo > len(dbi) {
		return fmt.Errorf("pdb: debug information tables run past the stream")
	}
	modi := dbi[64 : 64+cbModi]
	for pos := 0; pos+64 <= len(modi); {
		m := Module{Section: binary.LittleEndian.Uint16(modi[pos+4:]), Offset: binary.LittleEndian.Uint32(modi[pos+8:]), Size: binary.LittleEndian.Uint32(modi[pos+12:]), Stream: int(binary.LittleEndian.Uint16(modi[pos+34:]))}
		m.SymBytes, m.C11Bytes, m.C13Bytes = int(binary.LittleEndian.Uint32(modi[pos+36:])), int(binary.LittleEndian.Uint32(modi[pos+40:])), int(binary.LittleEndian.Uint32(modi[pos+44:]))
		name, p := cstr(modi, pos+64)
		obj, p := cstr(modi, p)
		m.Name, m.Object = name, obj
		f.Modules = append(f.Modules, m)
		pos = (p + 3) &^ 3
	}
	fi := dbi[64+cbModi+cbSC+cbSecMap : 64+cbModi+cbSC+cbSecMap+cbFileInfo]
	if len(fi) < 4 {
		return nil
	}
	nmods := int(binary.LittleEndian.Uint16(fi))
	counts := make([]int, nmods)
	total := 0
	for i := range counts {
		counts[i] = int(binary.LittleEndian.Uint16(fi[4+2*nmods+2*i:]))
		total += counts[i]
	}
	offs := fi[4+4*nmods:]
	names := fi[4+4*nmods+4*total:]
	k := 0
	for i, c := range counts {
		for j := 0; j < c && i < len(f.Modules); j++ {
			o := int(binary.LittleEndian.Uint32(offs[4*k:]))
			k++
			if o >= len(names) {
				continue
			}
			if f.Old {
				n := int(names[o])
				f.Modules[i].Files = append(f.Modules[i].Files, string(names[o+1:o+1+n]))
			} else {
				s, _ := cstr(names, o)
				f.Modules[i].Files = append(f.Modules[i].Files, s)
			}
		}
	}
	return nil
}

func cstr(b []byte, off int) (string, int) {
	end := bytes.IndexByte(b[off:], 0)
	if end < 0 {
		return string(b[off:]), len(b)
	}
	return string(b[off : off+end]), off + end + 1
}

// Types parses the type stream
func (f *File) Types() (*cv.Types, error) {
	if len(f.Streams) < 3 {
		return nil, fmt.Errorf("pdb: no type stream")
	}
	return cv.ParseTPI(f.Streams[2])
}

// Symbols parses a module's symbol stream
func (f *File) Symbols(m Module) (*cv.Module, error) {
	mod := cv.NewModule()
	if m.Stream <= 0 || m.Stream >= len(f.Streams) || len(f.Streams[m.Stream]) < 4 {
		mod.Finish()
		return mod, nil
	}
	s := f.Streams[m.Stream]
	if m.SymBytes > len(s) || m.SymBytes+m.C11Bytes+m.C13Bytes > len(s) {
		return nil, fmt.Errorf("pdb: module %s symbol stream is shorter than its tables", m.Name)
	}
	syms := s[:m.SymBytes]
	if m.SymBytes == 0 {
		syms = s
	}
	if err := mod.AddRecords(syms[4:], binary.LittleEndian.Uint32(syms) != 4); err != nil {
		return nil, fmt.Errorf("pdb: module %s: %v", m.Name, err)
	}
	if m.C11Bytes > 0 {
		mod.AddC11Lines(s[m.SymBytes : m.SymBytes+m.C11Bytes])
	}
	if m.C13Bytes > 0 {
		if err := mod.AddSubsections(s[m.SymBytes+m.C11Bytes : m.SymBytes+m.C11Bytes+m.C13Bytes]); err != nil {
			return nil, fmt.Errorf("pdb: module %s lines: %v", m.Name, err)
		}
	}
	mod.Finish()
	return mod, nil
}

// Globals parses the symbol record stream, every public and global the linker kept
func (f *File) Globals() (*cv.Module, error) {
	mod := cv.NewModule()
	if f.symrec <= 0 || f.symrec >= len(f.Streams) {
		return nil, fmt.Errorf("pdb: no symbol record stream")
	}
	if err := mod.AddRecords(f.Streams[f.symrec], f.Old); err != nil {
		return nil, err
	}
	mod.Finish()
	return mod, nil
}
