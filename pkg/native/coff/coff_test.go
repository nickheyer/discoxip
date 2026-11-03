package coff

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// Builds an object with one text section holding two routines and a relocation to an external
func sample() []byte {
	text := []byte{0x8B, 0xC1, 0xC3, 0xE8, 0x00, 0x00, 0x00, 0x00, 0xC3}
	syms := []struct {
		name  string
		value uint32
		sec   int16
		typ   uint16
		class uint8
	}{
		{".text", 0, 1, 0, ClassStatic}, {"_first", 0, 1, TypeFunction, ClassExternal}, {"_second", 3, 1, TypeFunction, ClassExternal}, {"_target", 0, 0, TypeFunction, ClassExternal}, {"d:\\src\\a.c", 0, -2, 0, ClassFile},
	}
	var b bytes.Buffer
	hdr := make([]byte, 20)
	binary.LittleEndian.PutUint16(hdr, 0x14C)
	binary.LittleEndian.PutUint16(hdr[2:], 1)
	symat := uint32(20 + 40 + len(text) + 10)
	binary.LittleEndian.PutUint32(hdr[8:], symat)
	binary.LittleEndian.PutUint32(hdr[12:], uint32(len(syms)))
	b.Write(hdr)
	sec := make([]byte, 40)
	copy(sec, ".text")
	binary.LittleEndian.PutUint32(sec[16:], uint32(len(text)))
	binary.LittleEndian.PutUint32(sec[20:], 60)
	binary.LittleEndian.PutUint32(sec[24:], uint32(60+len(text)))
	binary.LittleEndian.PutUint16(sec[32:], 1)
	binary.LittleEndian.PutUint32(sec[36:], 0x60500020)
	b.Write(sec)
	b.Write(text)
	rel := make([]byte, 10)
	binary.LittleEndian.PutUint32(rel, 4)
	binary.LittleEndian.PutUint32(rel[4:], 3)
	binary.LittleEndian.PutUint16(rel[8:], 0x14)
	b.Write(rel)
	strtab := []byte{0, 0, 0, 0}
	for _, s := range syms {
		rec := make([]byte, 18)
		if len(s.name) <= 8 {
			copy(rec, s.name)
		} else {
			binary.LittleEndian.PutUint32(rec[4:], uint32(len(strtab)))
			strtab = append(append(strtab, s.name...), 0)
		}
		binary.LittleEndian.PutUint32(rec[8:], s.value)
		binary.LittleEndian.PutUint16(rec[12:], uint16(s.sec))
		binary.LittleEndian.PutUint16(rec[14:], s.typ)
		rec[16] = s.class
		b.Write(rec)
	}
	binary.LittleEndian.PutUint32(strtab, uint32(len(strtab)))
	b.Write(strtab)
	return b.Bytes()
}

// Sections, relocations, long symbol names and routine extents come back from a parsed object
func TestParse(t *testing.T) {
	o, err := Parse(sample())
	if err != nil {
		t.Fatal(err)
	}
	if len(o.Sections) != 1 || o.Sections[0].Name != ".text" || len(o.Sections[0].Data) != 9 || o.Sections[0].Flags != 0x60500020 {
		t.Fatalf("sections %+v", o.Sections)
	}
	if rs := o.Sections[0].Relocs; len(rs) != 1 || rs[0] != (Reloc{Offset: 4, Type: 0x14, Symbol: "_target"}) {
		t.Fatalf("relocs %+v", rs)
	}
	if o.Symbols[4].Name != "d:\\src\\a.c" || o.Symbols[4].Class != ClassFile {
		t.Fatalf("long name %+v", o.Symbols[4])
	}
	fns := o.Functions()
	if len(fns) != 2 || fns[0] != (Function{Name: "_first", Section: 0, Start: 0, End: 3}) || fns[1] != (Function{Name: "_second", Section: 0, Start: 3, End: 9}) {
		t.Fatalf("functions %+v", fns)
	}
	body, relocs := o.Body(fns[1])
	if !bytes.Equal(body, []byte{0xE8, 0, 0, 0, 0, 0xC3}) || len(relocs) != 1 || relocs[0].Offset != 1 {
		t.Fatalf("body % x relocs %+v", body, relocs)
	}
}

// Archive members come back with long names resolved and are found by base name
func TestArchive(t *testing.T) {
	obj := sample()
	var b bytes.Buffer
	b.WriteString("!<arch>\n")
	write := func(name string, data []byte) {
		hdr := []byte(name + "                ")[:16]
		size := []byte("          ")
		copy(size, []byte(itoa(len(data))))
		b.Write(hdr)
		b.WriteString("0           0     0     644     ")
		b.Write(size)
		b.WriteString("`\n")
		b.Write(data)
		if len(data)%2 == 1 {
			b.WriteByte('\n')
		}
	}
	write("/", []byte{0, 0, 0, 0})
	write("//", []byte("obj\\i386\\random.obj\x00"))
	write("/0", obj)
	write("short.obj/", obj)
	members, err := ReadArchive(b.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if len(members) != 2 || members[0].Name != "obj\\i386\\random.obj" || members[1].Name != "short.obj" {
		t.Fatalf("members %+v", members)
	}
	if m, ok := Find(members, "random.obj"); !ok || !bytes.Equal(m.Data, obj) {
		t.Fatal("random.obj not found by base name")
	}
	if _, ok := Find(members, "other.obj"); ok {
		t.Fatal("other.obj found")
	}
	if _, err := ReadArchive([]byte("nope")); err == nil {
		t.Fatal("non archive read")
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var s []byte
	for n > 0 {
		s = append([]byte{byte('0' + n%10)}, s...)
		n /= 10
	}
	return string(s)
}
