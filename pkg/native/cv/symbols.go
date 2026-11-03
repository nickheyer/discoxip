package cv

import (
	"encoding/binary"
	"fmt"
	"sort"
)

const (
	sCompile    = 0x0001
	sEnd        = 0x0006
	sObjNameST  = 0x0009
	sRegisterST = 0x1001
	sConstantST = 0x1002
	sUDTST      = 0x1003
	sBPRel32ST  = 0x1006
	sLData32ST  = 0x1007
	sGData32ST  = 0x1008
	sLProc32ST  = 0x100A
	sGProc32ST  = 0x100B
	sRegRel32ST = 0x100D
	sObjName    = 0x1101
	sBlock32    = 0x1103
	sLabel32    = 0x1105
	sRegister   = 0x1106
	sConstant   = 0x1107
	sUDT        = 0x1108
	sBPRel32    = 0x110B
	sLData32    = 0x110C
	sGData32    = 0x110D
	sLProc32    = 0x110F
	sGProc32    = 0x1110
	sRegRel32   = 0x1111
	sCompile2   = 0x1116
	sCompile2ST = 0x1013
	sLProc32ID  = 0x1146
	sGProc32ID  = 0x1147

	subSymbols   = 0xF1
	subLines     = 0xF2
	subStrings   = 0xF3
	subChecksums = 0xF4
)

// Variable is a parameter or local, placed in a register or relative to the frame
type Variable struct {
	Name     string
	Type     uint32
	Offset   int32
	Register uint16
	Param    bool
}

// Proc is a routine with its type, placement, variables and source lines
type Proc struct {
	Name    string
	Type    uint32
	Segment uint16
	Offset  uint32
	Length  uint32
	Global  bool
	Vars    []Variable
	Lines   []Line
	File    string
}

// Line maps a code offset within a routine onto a source line
type Line struct {
	Offset uint32
	Line   uint32
}

// Data is a global or static variable
type Data struct {
	Name    string
	Type    uint32
	Segment uint16
	Offset  uint32
	Global  bool
}

// UDT names a type
type UDT struct {
	Name string
	Type uint32
}

// Constant is a named value
type Constant struct {
	Name  string
	Type  uint32
	Value int64
}

// Module is everything the symbol records of one object or compiland describe
type Module struct {
	Object    string
	Compiler  string
	Version   string
	Language  uint8
	Procs     []Proc
	Data      []Data
	UDTs      []UDT
	Constants []Constant
	files     map[uint32]string
	strings   []byte
	lines     []lineBlock
}

type lineBlock struct {
	segment uint16
	offset  uint32
	length  uint32
	file    string
	lines   []Line
}

// NewModule makes an empty module ready to take symbol sections
func NewModule() *Module {
	return &Module{files: map[uint32]string{}}
}

// AddSection reads one debug symbol section of an object, a zero length symbol subsection running to the end
func (m *Module) AddSection(section []byte) error {
	if len(section) < 4 {
		return fmt.Errorf("cv: %d bytes is too short for a symbol section", len(section))
	}
	if sig := binary.LittleEndian.Uint32(section); sig != 4 {
		return m.addRecords(section[4:], true)
	}
	return m.AddSubsections(section[4:])
}

// AddRecords reads a bare run of symbol records, names length prefixed when old
func (m *Module) AddRecords(data []byte, old bool) error {
	return m.addRecords(data, old)
}

// AddSubsections reads the subsections of a C13 debug stream, string tables and checksums before lines
func (m *Module) AddSubsections(data []byte) error {
	var later [][2][]byte
	for pos := 0; pos+8 <= len(data); {
		kind, n := binary.LittleEndian.Uint32(data[pos:]), int(binary.LittleEndian.Uint32(data[pos+4:]))
		if n == 0 && kind == subSymbols {
			n = len(data) - pos - 8
		}
		if pos+8+n > len(data) {
			return fmt.Errorf("cv: subsection %#x at %#x runs past the section", kind, pos)
		}
		body := data[pos+8 : pos+8+n]
		switch kind {
		case subStrings:
			m.strings = body
		case subChecksums:
			m.addChecksums(body)
		default:
			later = append(later, [2][]byte{{byte(kind)}, body})
		}
		pos += 8 + n + (-n)&3
	}
	for _, sub := range later {
		switch uint32(sub[0][0]) {
		case subSymbols:
			if err := m.addRecords(sub[1], false); err != nil {
				return err
			}
		case subLines:
			m.addLines(sub[1])
		}
	}
	return nil
}

func (m *Module) addChecksums(body []byte) {
	for pos := 0; pos+6 <= len(body); {
		nameOff := binary.LittleEndian.Uint32(body[pos:])
		n := int(body[pos+4])
		if int(nameOff) < len(m.strings) {
			m.files[uint32(pos)] = cstring(m.strings[nameOff:])
		}
		pos += 6 + n + (-(6 + n))&3
	}
}

func (m *Module) addLines(body []byte) {
	if len(body) < 12 {
		return
	}
	b := lineBlock{offset: binary.LittleEndian.Uint32(body), segment: binary.LittleEndian.Uint16(body[4:]), length: binary.LittleEndian.Uint32(body[8:])}
	for pos := 12; pos+12 <= len(body); {
		file, n, size := binary.LittleEndian.Uint32(body[pos:]), int(binary.LittleEndian.Uint32(body[pos+4:])), int(binary.LittleEndian.Uint32(body[pos+8:]))
		b.file = m.files[file]
		for i := 0; i < n && pos+12+8*i+8 <= len(body); i++ {
			off, ln := binary.LittleEndian.Uint32(body[pos+12+8*i:]), binary.LittleEndian.Uint32(body[pos+16+8*i:])
			b.lines = append(b.lines, Line{Offset: off, Line: ln & 0xFFFFFF})
		}
		if size < 12 {
			break
		}
		pos += size
	}
	m.lines = append(m.lines, b)
}

// AddC11Lines reads a source module line table, the format program databases kept before C13
func (m *Module) AddC11Lines(data []byte) {
	if len(data) < 4 {
		return
	}
	nfiles, nsegs := int(u16(data, 0)), int(u16(data, 2))
	for i := 0; i < nfiles; i++ {
		fileAt := int(u32(data, 4+4*i))
		if fileAt+4 > len(data) {
			return
		}
		fsegs := int(u16(data, fileAt))
		nameAt := fileAt + 4 + 4*fsegs + 8*fsegs
		fname := ""
		if nameAt < len(data) {
			fname = name(data, nameAt, true)
		}
		for j := 0; j < fsegs; j++ {
			lnAt := int(u32(data, fileAt+4+4*j))
			if lnAt+4 > len(data) {
				continue
			}
			seg, pairs := u16(data, lnAt), int(u16(data, lnAt+2))
			if lnAt+4+6*pairs > len(data) {
				continue
			}
			b := lineBlock{segment: seg, file: fname}
			for k := 0; k < pairs; k++ {
				off := u32(data, lnAt+4+4*k)
				ln := u16(data, lnAt+4+4*pairs+2*k)
				if k == 0 || off < b.offset {
					b.offset = off
				}
				if off > b.offset+b.length {
					b.length = off - b.offset
				}
				b.lines = append(b.lines, Line{Offset: off, Line: uint32(ln)})
			}
			b.length++
			for k := range b.lines {
				b.lines[k].Offset -= b.offset
			}
			m.lines = append(m.lines, b)
		}
	}
	_ = nsegs
}

// Reads a run of symbol records, attaching variables to the routine that encloses them
func (m *Module) addRecords(data []byte, st bool) error {
	var cur *Proc
	depth := 0
	for pos := 0; pos+4 <= len(data); {
		n := int(binary.LittleEndian.Uint16(data[pos:]))
		if n < 2 {
			return nil
		}
		if pos+2+n > len(data) {
			return fmt.Errorf("cv: symbol record at %#x runs past the section", pos)
		}
		kind := binary.LittleEndian.Uint16(data[pos+2:])
		rec := data[pos+4 : pos+2+n]
		pos += 2 + n
		oldName := st || kind < 0x1100
		switch kind {
		case sObjName, sObjNameST:
			m.Object = name(rec, 4, oldName)
		case sCompile:
			m.Language = rec[0]
			m.Compiler = name(rec, 4, true)
		case sCompile2, sCompile2ST:
			m.Language = rec[0]
			m.Version = fmt.Sprintf("%d.%02d.%d", u16(rec, 6), u16(rec, 8), u16(rec, 10))
			m.Compiler = name(rec, 18, oldName || kind == sCompile2ST)
		case sGProc32, sLProc32, sGProc32ST, sLProc32ST, sGProc32ID, sLProc32ID:
			p := Proc{Length: u32(rec, 12), Type: u32(rec, 24), Offset: u32(rec, 28), Segment: u16(rec, 32), Global: kind == sGProc32 || kind == sGProc32ST || kind == sGProc32ID}
			p.Name = name(rec, 35, oldName)
			m.Procs = append(m.Procs, p)
			cur = &m.Procs[len(m.Procs)-1]
			depth = 1
		case sBlock32:
			depth++
		case sEnd:
			if depth > 0 {
				depth--
			}
			if depth == 0 {
				cur = nil
			}
		case sBPRel32, sBPRel32ST:
			if cur != nil {
				cur.Vars = append(cur.Vars, Variable{Offset: int32(u32(rec, 0)), Type: u32(rec, 4), Name: name(rec, 8, oldName), Param: int32(u32(rec, 0)) > 0})
			}
		case sRegRel32, sRegRel32ST:
			if cur != nil {
				cur.Vars = append(cur.Vars, Variable{Offset: int32(u32(rec, 0)), Type: u32(rec, 4), Register: u16(rec, 8), Name: name(rec, 10, oldName), Param: int32(u32(rec, 0)) > 0})
			}
		case sRegister, sRegisterST:
			if cur != nil {
				cur.Vars = append(cur.Vars, Variable{Type: u32(rec, 0), Register: u16(rec, 4), Name: name(rec, 6, oldName)})
			}
		case sGData32, sLData32, sGData32ST, sLData32ST:
			m.Data = append(m.Data, Data{Type: u32(rec, 0), Offset: u32(rec, 4), Segment: u16(rec, 8), Name: name(rec, 10, oldName), Global: kind == sGData32 || kind == sGData32ST})
		case sUDT, sUDTST:
			m.UDTs = append(m.UDTs, UDT{Type: u32(rec, 0), Name: name(rec, 4, oldName)})
		case sConstant, sConstantST:
			v, k := numeric(rec, 4)
			m.Constants = append(m.Constants, Constant{Type: u32(rec, 0), Value: v, Name: name(rec, 4+k, oldName)})
		}
	}
	return nil
}

// Finish attaches line tables to the routines they cover and orders routines by placement
func (m *Module) Finish() {
	for i := range m.Procs {
		p := &m.Procs[i]
		for _, b := range m.lines {
			if b.segment != p.Segment || b.offset+b.length <= p.Offset || b.offset >= p.Offset+p.Length {
				continue
			}
			p.File = b.file
			for _, l := range b.lines {
				if abs := b.offset + l.Offset; abs >= p.Offset && abs < p.Offset+p.Length {
					p.Lines = append(p.Lines, Line{Offset: abs - p.Offset, Line: l.Line})
				}
			}
		}
		sort.Slice(p.Lines, func(a, b int) bool { return p.Lines[a].Offset < p.Lines[b].Offset })
	}
	sort.SliceStable(m.Procs, func(a, b int) bool {
		if m.Procs[a].Segment != m.Procs[b].Segment {
			return m.Procs[a].Segment < m.Procs[b].Segment
		}
		return m.Procs[a].Offset < m.Procs[b].Offset
	})
}

// Params lists a routine's parameters in declaration order
func (p Proc) Params() []Variable {
	var out []Variable
	for _, v := range p.Vars {
		if v.Param {
			out = append(out, v)
		}
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].Offset < out[b].Offset })
	return out
}

// Locals lists a routine's variables that are not parameters
func (p Proc) Locals() []Variable {
	var out []Variable
	for _, v := range p.Vars {
		if !v.Param {
			out = append(out, v)
		}
	}
	return out
}

// Span is the first and last source line a routine covers
func (p Proc) Span() (uint32, uint32) {
	lo, hi := uint32(0), uint32(0)
	for _, l := range p.Lines {
		if lo == 0 || l.Line < lo {
			lo = l.Line
		}
		if l.Line > hi {
			hi = l.Line
		}
	}
	return lo, hi
}
