package xbe

import (
	"bytes"
	"strings"
	"testing"

	"golang.org/x/arch/x86/x86asm"
)

func TestDeriveInterruptTemplate(t *testing.T) {
	d, at, exports := kernelDisassembly(t)
	tpl, err := DeriveInterruptTemplate(d, exports)
	if err != nil {
		t.Fatal(err)
	}
	if tpl.Base != at["template"] || tpl.Length != 24 {
		t.Fatalf("template %08X length %d", tpl.Base, tpl.Length)
	}
	if !equalU32(tpl.Slots, []uint32{2, 9}) {
		t.Fatalf("slots %v", tpl.Slots)
	}
	want := append([]byte{0x50, 0xB8, 0xEF, 0xBE, 0xAD, 0xDE, 0xFF, 0xD0, 0xB9, 0x78, 0x56, 0x34, 0x12, 0x58, 0xCF}, bytes.Repeat([]byte{0xCC}, 9)...)
	if !bytes.Equal(tpl.Bytes, want) {
		t.Fatalf("bytes %x", tpl.Bytes)
	}
}

func TestDeriveInterruptTemplateErrors(t *testing.T) {
	d, at, exports := kernelDisassembly(t)
	delete(exports, ordKeInitializeInterrupt)
	if _, err := DeriveInterruptTemplate(d, exports); err == nil || !strings.Contains(err.Error(), "no KeInitializeInterrupt routine") {
		t.Fatalf("missing err %v", err)
	}
	if _, err := DeriveInterruptTemplate(d, map[uint32]uint32{ordKeInitializeInterrupt: at["shared"]}); err == nil || !strings.Contains(err.Error(), "copies no code") {
		t.Fatalf("no copy err %v", err)
	}
	if d.Functions[at["copyOnly"]] == nil {
		t.Fatal("copy only routine not discovered")
	}
	if _, err := DeriveInterruptTemplate(d, map[uint32]uint32{ordKeInitializeInterrupt: at["copyOnly"]}); err == nil || !strings.Contains(err.Error(), "patches nothing") {
		t.Fatalf("no patch err %v", err)
	}
	if _, err := DeriveInterruptTemplate(d, map[uint32]uint32{ordKeInitializeInterrupt: 0x15000}); err == nil {
		t.Fatal("unknown routine accepted")
	}
}

func TestPatchedWord(t *testing.T) {
	_, at, _ := kernelDisassembly(t)
	base := at["template"]
	tpl := &CodeTemplate{Base: base, Length: 24, Slots: []uint32{2, 9}}
	cases := []struct {
		in   Instruction
		off  uint32
		want bool
	}{
		{decodeAt(t, base+1, 0xB8, 0xEF, 0xBE, 0xAD, 0xDE), 2, true},
		{decodeAt(t, base+8, 0xB9, 0x78, 0x56, 0x34, 0x12), 9, true},
		{decodeAt(t, base, 0x50), 0, false},
		{decodeAt(t, base+6, 0xFF, 0xD0), 0, false},
		{decodeAt(t, base+2, 0xB8, 0xEF, 0xBE, 0xAD, 0xDE), 0, false},
		{decodeAt(t, base+20, 0xB8, 0xEF, 0xBE, 0xAD, 0xDE), 0, false},
		{decodeAt(t, base-5, 0xB8, 0xEF, 0xBE, 0xAD, 0xDE), 0, false},
		{decodeAt(t, base+24, 0xB8, 0xEF, 0xBE, 0xAD, 0xDE), 0, false},
	}
	for i, c := range cases {
		off, ok := tpl.patchedWord(&c.in)
		if ok != c.want || off != c.off {
			t.Errorf("case %d at %08X gives %d %v, want %d %v", i, c.in.VA, off, ok, c.off, c.want)
		}
	}
	var none *CodeTemplate
	if _, ok := none.patchedWord(&cases[0].in); ok {
		t.Error("nil template patches")
	}
}

func TestSlotLoad(t *testing.T) {
	tpl := &CodeTemplate{Base: 0x11000, Length: 24, Slots: []uint32{2}}
	l, ok := tpl.slotLoad(2).(Load)
	if !ok || l.Size != 4 || l.Base != "" {
		t.Fatalf("slot load %#v", tpl.slotLoad(2))
	}
	b, ok := l.Addr.(Bin)
	if !ok || b.Op != "add" || b.A != (Sym{"self"}) || b.B != (IConst{2}) {
		t.Fatalf("slot address %#v", l.Addr)
	}
}

func TestTemplateCopyTracking(t *testing.T) {
	d, at, exports := kernelDisassembly(t)
	fn := d.Functions[at["initInterrupt"]]
	var movs, patches int
	for i := range fn.Instructions {
		in := &fn.Instructions[i]
		if in.Inst.Op == x86asm.MOVSD && hasRep(in) {
			movs++
		}
		if m, ok := in.Inst.Args[0].(x86asm.Mem); ok && m.Base == x86asm.EDX {
			patches++
		}
	}
	if movs != 1 || patches != 2 {
		t.Fatalf("copies %d patches %d", movs, patches)
	}
	tpl, err := DeriveInterruptTemplate(d, exports)
	if err != nil {
		t.Fatal(err)
	}
	if !d.executableAt(tpl.Base) || d.executableAt(0x12000) {
		t.Fatal("template placement")
	}
}
