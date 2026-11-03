// Package cv decodes CodeView debugging records, the types, symbols and line tables objects and program databases carry
package cv

import (
	"encoding/binary"
	"fmt"
	"strings"
)

const (
	lfModifier    = 0x1001
	lfPointer     = 0x1002
	lfArrayST     = 0x1003
	lfClassST     = 0x1004
	lfStructureST = 0x1005
	lfUnionST     = 0x1006
	lfEnumST      = 0x1007
	lfProcedure   = 0x1008
	lfMFunction   = 0x1009
	lfVTShape     = 0x000A
	lfArgList     = 0x1201
	lfFieldList   = 0x1203
	lfBitfield    = 0x1205
	lfMethodList  = 0x1206
	lfBClass      = 0x1400
	lfVBClass     = 0x1401
	lfIVBClass    = 0x1402
	lfIndex       = 0x1404
	lfEnumerateST = 0x0403
	lfMemberST    = 0x1405
	lfSTMemberST  = 0x1406
	lfMethodST    = 0x1407
	lfNestTypeST  = 0x1408
	lfVFuncTab    = 0x1409
	lfOneMethodST = 0x140B
	lfEnumerate   = 0x1502
	lfArray       = 0x1503
	lfClass       = 0x1504
	lfStructure   = 0x1505
	lfUnion       = 0x1506
	lfEnum        = 0x1507
	lfMember      = 0x150D
	lfSTMember    = 0x150E
	lfMethod      = 0x150F
	lfNestType    = 0x1510
	lfOneMethod   = 0x1511
	lfTypeServer2 = 0x1515

	FirstIndex = 0x1000
)

// Types is a table of type records addressed by index, Names giving typedef names to render aggregates by
type Types struct {
	records map[uint32][]byte
	kinds   map[uint32]uint16
	First   uint32
	Last    uint32
	Server  string
	Names   map[uint32]string
}

// Reads the type records of an object's debug type section
func ParseTypes(section []byte) (*Types, error) {
	if len(section) < 4 {
		return nil, fmt.Errorf("cv: %d bytes is too short for a type section", len(section))
	}
	if sig := binary.LittleEndian.Uint32(section); sig != 4 && sig != 2 && sig != 1 {
		return nil, fmt.Errorf("cv: type section signature %#x is not CodeView", sig)
	}
	t := &Types{records: map[uint32][]byte{}, kinds: map[uint32]uint16{}, First: FirstIndex, Last: FirstIndex}
	return t, t.read(section[4:], FirstIndex)
}

// Reads the type records of a program database's type stream
func ParseTPI(stream []byte) (*Types, error) {
	if len(stream) < 16 {
		return nil, fmt.Errorf("cv: %d bytes is too short for a type stream", len(stream))
	}
	version := binary.LittleEndian.Uint32(stream)
	hdr := binary.LittleEndian.Uint32(stream[4:])
	first := binary.LittleEndian.Uint32(stream[8:])
	if version < 19950410 || hdr > uint32(len(stream)) {
		return nil, fmt.Errorf("cv: type stream version %d with a %d byte header is not understood", version, hdr)
	}
	t := &Types{records: map[uint32][]byte{}, kinds: map[uint32]uint16{}, First: first, Last: first}
	return t, t.read(stream[hdr:], first)
}

func (t *Types) read(data []byte, index uint32) error {
	for pos := 0; pos+4 <= len(data); {
		n := int(binary.LittleEndian.Uint16(data[pos:]))
		if n < 2 || pos+2+n > len(data) {
			return fmt.Errorf("cv: type record %#x at %#x runs past the table", index, pos)
		}
		kind := binary.LittleEndian.Uint16(data[pos+2:])
		t.records[index] = data[pos+4 : pos+2+n]
		t.kinds[index] = kind
		if kind == lfTypeServer2 && len(t.records[index]) > 20 {
			t.Server = cstring(t.records[index][20:])
		}
		index++
		pos += 2 + n
	}
	t.Last = index
	return nil
}

// Count is the number of records in the table
func (t *Types) Count() int {
	return len(t.records)
}

// Kind returns a record's leaf kind, zero for a primitive or unknown index
func (t *Types) Kind(ti uint32) uint16 {
	return t.kinds[ti]
}

// Field is one member of a structure, union or enumeration
type Field struct {
	Name     string
	Type     uint32
	Offset   uint64
	Value    int64
	Bitfield bool
	Bits     uint8
	BitPos   uint8
	Static   bool
	Method   bool
	Base     bool
	VTable   bool
	Nested   bool
}

// Record is a decoded type
type Record struct {
	Kind       uint16
	Name       string
	Size       uint64
	Underlying uint32
	Element    uint32
	Return     uint32
	Class      uint32
	Fields     []Field
	Args       []uint32
	Count      int
	Forward    bool
	Const      bool
	Volatile   bool
	Call       uint8
	BitLen     uint8
	BitPos     uint8
	PtrSize    int
	FieldList  uint32
	Members    uint16
}

// Decode interprets the record at an index, primitives decoding to a record with no kind
func (t *Types) Decode(ti uint32) *Record {
	data, ok := t.records[ti]
	if !ok {
		return &Record{}
	}
	kind := t.kinds[ti]
	r := &Record{Kind: kind}
	st := func(off int) string {
		return name(data, off, stKind(kind))
	}
	switch kind {
	case lfModifier:
		r.Underlying = u32(data, 0)
		attr := u16(data, 4)
		r.Const, r.Volatile = attr&1 != 0, attr&2 != 0
	case lfPointer:
		r.Underlying = u32(data, 0)
		attr := u32(data, 4)
		r.PtrSize = int(attr >> 13 & 0x3F)
		if r.PtrSize == 0 {
			r.PtrSize = 4
		}
		r.Const, r.Volatile = attr&0x400 != 0, attr&0x200 != 0
	case lfProcedure:
		r.Return = u32(data, 0)
		r.Call = data[4]
		r.Count = int(u16(data, 6))
		r.Underlying = u32(data, 8)
	case lfMFunction:
		r.Return = u32(data, 0)
		r.Class = u32(data, 4)
		r.Call = data[12]
		r.Count = int(u16(data, 14))
		r.Underlying = u32(data, 16)
	case lfArgList, lfMethodList:
		n := int(u32(data, 0))
		for i := 0; i < n && 4+4*i+4 <= len(data); i++ {
			r.Args = append(r.Args, u32(data, 4+4*i))
		}
	case lfArray, lfArrayST:
		r.Element = u32(data, 0)
		r.Underlying = u32(data, 4)
		size, n := numeric(data, 8)
		r.Size = uint64(size)
		r.Name = st(8 + n)
	case lfStructure, lfClass, lfStructureST, lfClassST:
		r.Members = u16(data, 0)
		prop := u16(data, 2)
		r.Forward = prop&0x80 != 0
		r.FieldList = u32(data, 4)
		size, n := numeric(data, 16)
		r.Size = uint64(size)
		r.Name = st(16 + n)
	case lfUnion, lfUnionST:
		r.Members = u16(data, 0)
		prop := u16(data, 2)
		r.Forward = prop&0x80 != 0
		r.FieldList = u32(data, 4)
		size, n := numeric(data, 8)
		r.Size = uint64(size)
		r.Name = st(8 + n)
	case lfEnum, lfEnumST:
		r.Members = u16(data, 0)
		prop := u16(data, 2)
		r.Forward = prop&0x80 != 0
		r.Underlying = u32(data, 4)
		r.FieldList = u32(data, 8)
		r.Name = st(12)
	case lfBitfield:
		r.Underlying = u32(data, 0)
		r.BitLen, r.BitPos = data[4], data[5]
	case lfFieldList:
		r.Fields = t.fields(data)
	}
	return r
}

// Walks a field list, following continuation records
func (t *Types) fields(data []byte) []Field {
	var out []Field
	for pos := 0; pos+2 <= len(data); {
		if data[pos] >= 0xF0 {
			pos += int(data[pos] & 0x0F)
			continue
		}
		kind := u16(data, pos)
		p := pos + 2
		f := Field{}
		switch kind {
		case lfMember, lfMemberST:
			f.Type = u32(data, p+2)
			off, n := numeric(data, p+6)
			f.Offset = uint64(off)
			f.Name = name(data, p+6+n, stKind(kind))
			pos = p + 6 + n + nameLen(data, p+6+n, stKind(kind))
		case lfSTMember, lfSTMemberST:
			f.Type = u32(data, p+2)
			f.Static = true
			f.Name = name(data, p+6, stKind(kind))
			pos = p + 6 + nameLen(data, p+6, stKind(kind))
		case lfEnumerate, lfEnumerateST:
			v, n := numeric(data, p+2)
			f.Value = v
			f.Name = name(data, p+2+n, stKind(kind))
			pos = p + 2 + n + nameLen(data, p+2+n, stKind(kind))
		case lfBClass:
			f.Type = u32(data, p+2)
			_, n := numeric(data, p+6)
			f.Base = true
			pos = p + 6 + n
		case lfVBClass, lfIVBClass:
			f.Type = u32(data, p+2)
			_, n := numeric(data, p+10)
			_, m := numeric(data, p+10+n)
			f.Base = true
			pos = p + 10 + n + m
		case lfVFuncTab:
			f.Type = u32(data, p+2)
			f.VTable = true
			pos = p + 6
		case lfMethod, lfMethodST:
			f.Type = u32(data, p+2)
			f.Method = true
			f.Name = name(data, p+6, stKind(kind))
			pos = p + 6 + nameLen(data, p+6, stKind(kind))
		case lfOneMethod, lfOneMethodST:
			attr := u16(data, p)
			f.Type = u32(data, p+2)
			f.Method = true
			q := p + 6
			if mprop := attr >> 2 & 7; mprop == 4 || mprop == 6 {
				q += 4
			}
			f.Name = name(data, q, stKind(kind))
			pos = q + nameLen(data, q, stKind(kind))
		case lfNestType, lfNestTypeST:
			f.Type = u32(data, p+2)
			f.Nested = true
			f.Name = name(data, p+6, stKind(kind))
			pos = p + 6 + nameLen(data, p+6, stKind(kind))
		case lfIndex:
			return append(out, t.fields(t.records[u32(data, p+2)])...)
		default:
			return out
		}
		out = append(out, f)
	}
	return out
}

func stKind(kind uint16) bool {
	switch kind {
	case lfArrayST, lfClassST, lfStructureST, lfUnionST, lfEnumST, lfEnumerateST, lfMemberST, lfSTMemberST, lfMethodST, lfNestTypeST, lfOneMethodST:
		return true
	}
	return false
}

func u16(b []byte, off int) uint16 {
	if off+2 > len(b) {
		return 0
	}
	return binary.LittleEndian.Uint16(b[off:])
}

func u32(b []byte, off int) uint32 {
	if off+4 > len(b) {
		return 0
	}
	return binary.LittleEndian.Uint32(b[off:])
}

// Reads a numeric leaf, returning its value and the bytes it occupied
func numeric(b []byte, off int) (int64, int) {
	v := u16(b, off)
	if v < 0x8000 {
		return int64(v), 2
	}
	switch v {
	case 0x8000:
		return int64(int8(b[off+2])), 3
	case 0x8001:
		return int64(int16(u16(b, off+2))), 4
	case 0x8002:
		return int64(u16(b, off+2)), 4
	case 0x8003:
		return int64(int32(u32(b, off+2))), 6
	case 0x8004:
		return int64(u32(b, off+2)), 6
	case 0x8009, 0x800A:
		if off+10 <= len(b) {
			return int64(binary.LittleEndian.Uint64(b[off+2:])), 10
		}
	}
	return 0, 2
}

func cstring(b []byte) string {
	for i, c := range b {
		if c == 0 {
			return string(b[:i])
		}
	}
	return string(b)
}

// Reads a name, length prefixed in the older records and terminated in the newer
func name(b []byte, off int, st bool) string {
	if off >= len(b) {
		return ""
	}
	if st {
		n := int(b[off])
		if off+1+n > len(b) {
			n = len(b) - off - 1
		}
		return string(b[off+1 : off+1+n])
	}
	return cstring(b[off:])
}

func nameLen(b []byte, off int, st bool) int {
	if off >= len(b) {
		return 0
	}
	if st {
		return 1 + int(b[off])
	}
	return len(cstring(b[off:])) + 1
}

var primitives = map[uint32]string{
	0x0003: "void", 0x0008: "HRESULT",
	0x0010: "signed char", 0x0020: "unsigned char", 0x0070: "char", 0x0071: "wchar_t",
	0x0011: "short", 0x0021: "unsigned short", 0x0072: "short", 0x0073: "unsigned short",
	0x0012: "long", 0x0022: "unsigned long", 0x0074: "int", 0x0075: "unsigned int",
	0x0013: "__int64", 0x0023: "unsigned __int64", 0x0076: "__int64", 0x0077: "unsigned __int64",
	0x0040: "float", 0x0041: "double", 0x0042: "long double",
	0x0030: "bool", 0x0031: "bool", 0x0032: "bool", 0x0033: "bool",
}

var callNames = map[uint8]string{0: "__cdecl", 4: "__fastcall", 7: "__stdcall", 11: "__thiscall"}

// Name renders a type's own name, a struct, union or enum keyword prefixed
func (t *Types) Name(ti uint32) string {
	return t.Declare(ti, "")
}

// Declare renders a C declaration of a name with the given type
func (t *Types) Declare(ti uint32, decl string) string {
	if ti < FirstIndex {
		base, ok := primitives[ti&0xFF]
		if !ok {
			base = fmt.Sprintf("T_%04X", ti)
		}
		if ti&0x700 != 0 {
			decl = "*" + decl
		}
		return join(base, decl)
	}
	r := t.Decode(ti)
	switch r.Kind {
	case lfModifier:
		q := ""
		if r.Const {
			q += "const "
		}
		if r.Volatile {
			q += "volatile "
		}
		return q + t.Declare(r.Underlying, decl)
	case lfPointer:
		inner := "*" + decl
		if r.Const {
			inner = "* const " + decl
		}
		switch t.Kind(r.Underlying) {
		case lfProcedure, lfMFunction, lfArray, lfArrayST:
			inner = "(" + inner + ")"
		}
		return t.Declare(r.Underlying, inner)
	case lfArray, lfArrayST:
		count := ""
		if es := t.sizeOf(r.Element); es > 0 {
			count = fmt.Sprintf("%d", r.Size/es)
		}
		return t.Declare(r.Element, decl+"["+count+"]")
	case lfProcedure:
		return t.Declare(r.Return, join(callNames[r.Call], decl)+"("+t.argList(r.Underlying, r.Count)+")")
	case lfMFunction:
		return t.Declare(r.Return, join(callNames[r.Call], decl)+"("+t.argList(r.Underlying, r.Count)+")")
	case lfStructure, lfClass, lfStructureST, lfClassST, lfUnion, lfUnionST, lfEnum, lfEnumST:
		if anonymous(r.Name) {
			return join(t.inline(ti), decl)
		}
		if alias, ok := t.Names[t.Complete(ti)]; ok {
			return join(alias, decl)
		}
		return join(KindWord(r.Kind)+" "+r.Name, decl)
	case lfBitfield:
		return t.Declare(r.Underlying, decl)
	}
	return join(fmt.Sprintf("LF_%04X", r.Kind), decl)
}

func anonymous(name string) bool {
	return name == "" || strings.HasPrefix(name, "__unnamed") || strings.Contains(name, "<unnamed") || strings.Contains(name, "::")
}

// KindWord is the C keyword introducing an aggregate of the given leaf kind
func KindWord(kind uint16) string {
	switch kind {
	case lfUnion, lfUnionST:
		return "union"
	case lfEnum, lfEnumST:
		return "enum"
	}
	return "struct"
}

// Renders an anonymous aggregate in place, its members on one indented level deeper
func (t *Types) inline(ti uint32) string {
	r := t.Decode(t.Complete(ti))
	body := strings.ReplaceAll(t.Body(ti), "\n", "\n    ")
	return KindWord(r.Kind) + " " + body
}

// Body renders the braced member list of an aggregate, offsets noted after each member
func (t *Types) Body(ti uint32) string {
	ti = t.Complete(ti)
	r := t.Decode(ti)
	var w strings.Builder
	w.WriteString("{\n")
	switch r.Kind {
	case lfEnum, lfEnumST:
		for _, f := range t.Decode(r.FieldList).Fields {
			fmt.Fprintf(&w, "    %s = %d,\n", f.Name, f.Value)
		}
	default:
		for _, f := range t.Decode(r.FieldList).Fields {
			switch {
			case f.Base:
				fmt.Fprintf(&w, "    /* base */ %s;\n", t.Name(f.Type))
			case f.VTable:
				fmt.Fprintf(&w, "    /* vtable */ %s;\n", t.Name(f.Type))
			case f.Method:
				fmt.Fprintf(&w, "    /* method */ %s;\n", t.Declare(firstMethod(t, f.Type), f.Name))
			case f.Nested:
				fmt.Fprintf(&w, "    /* nested */ %s %s;\n", t.Name(f.Type), f.Name)
			case f.Static:
				fmt.Fprintf(&w, "    static %s;\n", t.Declare(f.Type, f.Name))
			default:
				decl := t.Declare(f.Type, f.Name)
				if b := t.Decode(f.Type); b.Kind == lfBitfield {
					decl += fmt.Sprintf(" : %d", b.BitLen)
				}
				fmt.Fprintf(&w, "    %s; /* %#x */\n", decl, f.Offset)
			}
		}
	}
	w.WriteString("}")
	return w.String()
}

// Embedded lists the named aggregates a type holds by value, through arrays and anonymous members
func (t *Types) Embedded(ti uint32) []uint32 {
	var out []uint32
	var walk func(uint32)
	walk = func(ti uint32) {
		if ti < FirstIndex {
			return
		}
		r := t.Decode(ti)
		switch r.Kind {
		case lfModifier, lfBitfield:
			walk(r.Underlying)
		case lfArray, lfArrayST:
			walk(r.Element)
		case lfStructure, lfClass, lfStructureST, lfClassST, lfUnion, lfUnionST, lfEnum, lfEnumST:
			full := t.Complete(ti)
			if anonymous(r.Name) {
				for _, f := range t.Decode(t.Decode(full).FieldList).Fields {
					walk(f.Type)
				}
				return
			}
			out = append(out, full)
		}
	}
	for _, f := range t.Decode(t.Decode(t.Complete(ti)).FieldList).Fields {
		if !f.Method && !f.Nested && !f.Static {
			walk(f.Type)
		}
	}
	return out
}

func (t *Types) argList(ti uint32, count int) string {
	if count == 0 {
		return "void"
	}
	args := t.Decode(ti).Args
	parts := make([]string, 0, len(args))
	for _, a := range args {
		if a == 0 {
			parts = append(parts, "...")
			continue
		}
		parts = append(parts, t.Name(a))
	}
	return strings.Join(parts, ", ")
}

// Args lists a procedure type's parameter types
func (t *Types) Args(ti uint32) []uint32 {
	r := t.Decode(ti)
	if r.Kind != lfProcedure && r.Kind != lfMFunction {
		return nil
	}
	return t.Decode(r.Underlying).Args
}

var primitiveSizes = map[uint32]uint64{
	0x08: 4, 0x10: 1, 0x20: 1, 0x70: 1, 0x71: 2, 0x11: 2, 0x21: 2, 0x72: 2, 0x73: 2,
	0x12: 4, 0x22: 4, 0x74: 4, 0x75: 4, 0x13: 8, 0x23: 8, 0x76: 8, 0x77: 8,
	0x40: 4, 0x41: 8, 0x42: 8, 0x30: 1, 0x31: 2, 0x32: 4, 0x33: 8,
}

func (t *Types) sizeOf(ti uint32) uint64 {
	if ti < FirstIndex {
		if ti&0x700 != 0 {
			return 4
		}
		return primitiveSizes[ti&0xFF]
	}
	r := t.Decode(ti)
	switch r.Kind {
	case lfModifier:
		return t.sizeOf(r.Underlying)
	case lfPointer:
		return uint64(r.PtrSize)
	case lfArray, lfArrayST, lfStructure, lfClass, lfStructureST, lfClassST, lfUnion, lfUnionST:
		if r.Forward {
			if full := t.Complete(ti); full != ti {
				return t.sizeOf(full)
			}
		}
		return r.Size
	case lfEnum, lfEnumST:
		return 4
	case lfBitfield:
		return t.sizeOf(r.Underlying)
	}
	return 0
}

// Complete finds the defining record for a forward reference by name, the index itself when none exists
func (t *Types) Complete(ti uint32) uint32 {
	r := t.Decode(ti)
	if !r.Forward {
		return ti
	}
	for i := t.First; i < t.Last; i++ {
		if t.kinds[i] == r.Kind || sameFamily(t.kinds[i], r.Kind) {
			if o := t.Decode(i); !o.Forward && o.Name == r.Name {
				return i
			}
		}
	}
	return ti
}

func sameFamily(a, b uint16) bool {
	family := func(k uint16) int {
		switch k {
		case lfStructure, lfClass, lfStructureST, lfClassST:
			return 1
		case lfUnion, lfUnionST:
			return 2
		case lfEnum, lfEnumST:
			return 3
		}
		return 0
	}
	return family(a) != 0 && family(a) == family(b)
}

// Define renders the full C definition of a structure, union or enumeration
func (t *Types) Define(ti uint32) string {
	ti = t.Complete(ti)
	r := t.Decode(ti)
	switch r.Kind {
	case lfStructure, lfClass, lfStructureST, lfClassST, lfUnion, lfUnionST:
		return fmt.Sprintf("%s %s %s; /* %d bytes */", KindWord(r.Kind), r.Name, t.Body(ti), r.Size)
	case lfEnum, lfEnumST:
		return fmt.Sprintf("enum %s %s;", r.Name, t.Body(ti))
	}
	return t.Name(ti)
}

func firstMethod(t *Types, ti uint32) uint32 {
	if t.Kind(ti) == lfMethodList {
		if args := t.Decode(ti).Args; len(args) >= 2 {
			return args[1]
		}
	}
	return ti
}

// Dependencies lists the structures, unions and enumerations a type refers to, in definition order
func (t *Types) Dependencies(ti uint32, seen map[uint32]bool) []uint32 {
	var out []uint32
	var walk func(uint32, bool)
	walk = func(ti uint32, deep bool) {
		if ti < FirstIndex {
			return
		}
		r := t.Decode(ti)
		switch r.Kind {
		case lfModifier, lfPointer, lfBitfield:
			walk(r.Underlying, r.Kind != lfPointer && deep)
		case lfArray, lfArrayST:
			walk(r.Element, deep)
		case lfProcedure, lfMFunction:
			walk(r.Return, false)
			for _, a := range t.Decode(r.Underlying).Args {
				walk(a, false)
			}
		case lfStructure, lfClass, lfStructureST, lfClassST, lfUnion, lfUnionST, lfEnum, lfEnumST:
			full := t.Complete(ti)
			if seen[full] {
				return
			}
			seen[full] = true
			for _, f := range t.Decode(t.Decode(full).FieldList).Fields {
				if !f.Method && !f.Nested {
					walk(f.Type, true)
				}
			}
			if !anonymous(r.Name) {
				out = append(out, full)
			}
		}
	}
	walk(ti, true)
	return out
}

func join(a, b string) string {
	if b == "" {
		return a
	}
	if a == "" {
		return b
	}
	return a + " " + b
}
