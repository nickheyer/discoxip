package xbe

import (
	"strings"
	"testing"

	"golang.org/x/arch/x86/x86asm"
)

const (
	startupTableA    = 0x12010
	startupTableAEnd = 0x1201C
	startupTableB    = 0x12020
	startupTableBEnd = 0x12028
	startupDesc      = 0x12040
	startupDesc2     = 0x12044
)

func startupImage(t *testing.T) (*Image, map[string]uint32) {
	t.Helper()
	a := newAsm(0x11000)
	a.label("entry")
	a.emit(0x55, 0x8B, 0xEC)
	for _, f := range []string{"cinit", "init1", "init2", "init3", "registrar", "atexit", "helper", "other", "badInit"} {
		a.rel32([]byte{0xE8}, f)
	}
	a.emit(0x5D, 0xC3)
	a.align(16, 0xCC)
	a.label("cinit")
	a.emit(0x55, 0x8B, 0xEC, 0x56, 0x57)
	for _, table := range []string{"A", "B"} {
		start, end := uint32(startupTableA), uint32(startupTableAEnd)
		if table == "B" {
			start, end = startupTableB, startupTableBEnd
		}
		a.emit(0xBE)
		a.u32(start)
		a.emit(0xBF)
		a.u32(end)
		a.label("loop" + table)
		a.emit(0x3B, 0xF7)
		a.rel8(0x73, "done"+table)
		a.emit(0x8B, 0x06, 0x85, 0xC0)
		a.rel8(0x74, "next"+table)
		a.emit(0xFF, 0xD0)
		a.label("next" + table)
		a.emit(0x83, 0xC6, 0x04)
		a.rel8(0xEB, "loop"+table)
		a.label("done" + table)
	}
	a.emit(0xB8, 0x00, 0x10, 0x01, 0x00, 0x5F, 0x5E, 0x5D, 0xC3)
	a.align(16, 0xCC)
	a.label("init1")
	a.abs32([]byte{0x68}, "other")
	a.rel32([]byte{0xE8}, "atexit")
	a.emit(0x83, 0xC4, 0x04)
	a.rel32([]byte{0xE8}, "registrar")
	a.emit(0xC3)
	a.align(16, 0xCC)
	a.label("init2")
	a.rel32([]byte{0xE8}, "helper")
	a.emit(0xC3)
	a.align(16, 0xCC)
	a.label("helper")
	a.rel32([]byte{0xE8}, "registrar")
	a.emit(0xC3)
	a.align(16, 0xCC)
	a.label("init3")
	a.emit(0xB8, 0x01, 0x00, 0x00, 0x00, 0xC3)
	a.align(16, 0xCC)
	a.label("registrar")
	a.emit(0xC7, 0x05)
	a.u32(startupDesc)
	a.u32(0x2A)
	a.emit(0xC3)
	a.align(16, 0xCC)
	a.label("atexit")
	a.emit(0xC3)
	a.align(16, 0xCC)
	a.label("other")
	a.emit(0xC3)
	a.align(16, 0xCC)
	a.label("badInit")
	a.rel32([]byte{0xE8}, "registrar")
	a.rel32([]byte{0xE8}, "other")
	a.emit(0xC3)
	a.align(16, 0xCC)
	data := thunkTable(145)
	data = append(data, make([]byte, 0x10-len(data))...)
	data = append(data, words32(a.at("init1"), 0, a.at("init2"), 0)...)
	data = append(data, words32(0xFFFFFFFF, a.at("init3"))...)
	data = append(data, make([]byte, 0x40-len(data))...)
	data = append(data, words32(0, 0, 0x77777777)...)
	img := parseXBE(t, xbeSpec{
		entry:  0x11000,
		kthunk: 0x12000,
		sections: []testSection{
			{name: ".text", va: 0x11000, flags: flagExecutable, data: a.bytes(), vsize: 0x400},
			{name: ".data", va: 0x12000, flags: flagWritable, data: data, vsize: 0x100},
			{name: ".rdata", va: 0x13000, data: words32(0, 0, 0, 0)},
		},
	})
	return img, a.labels
}

// Disassembles the startup image and names the runtime routines the way a signature would
func startupDisassembly(t *testing.T) (*Disassembly, map[string]uint32) {
	t.Helper()
	img, at := startupImage(t)
	d := disassembleImage(t, img)
	name := func(label, raw string) {
		fn := mustFunction(t, d, at[label])
		sym := decorateSymbol(raw)
		sym.VA, sym.Lib = fn.EntryVA, "libcmt"
		fn.Symbol, fn.Name = &sym, sym.Name
	}
	name("cinit", "__cinit")
	name("atexit", "_atexit")
	name("other", "_SomethingElse")
	return d, at
}

func startupModel(t *testing.T, d *Disassembly) *RenderModel {
	t.Helper()
	mem, raw, err := d.Image.Memory()
	if err != nil {
		t.Fatal(err)
	}
	return &RenderModel{Memory: mem, memory: raw}
}

func TestStaticInitializers(t *testing.T) {
	d, at := startupDisassembly(t)
	inits, err := d.staticInitializers()
	if err != nil {
		t.Fatal(err)
	}
	if !equalU32(inits, []uint32{at["init1"], at["init2"], at["init3"]}) {
		t.Fatalf("initializers %x", inits)
	}
}

func TestStaticInitializersErrors(t *testing.T) {
	img, _ := startupImage(t)
	bare := disassembleImage(t, img)
	if _, err := bare.staticInitializers(); err == nil || !strings.Contains(err.Error(), "names no _cinit") {
		t.Fatalf("unnamed err %v", err)
	}
	d, at := startupDisassembly(t)
	cinit := d.Functions[at["cinit"]]
	saved := append([]Instruction(nil), cinit.Instructions...)
	restore := func() { cinit.Instructions = append([]Instruction(nil), saved...) }

	cinit.Instructions = append(cinit.Instructions, decodeAt(t, 0x11000, 0xB8, 0x30, 0x20, 0x01, 0x00))
	if _, err := d.staticInitializers(); err == nil || !strings.Contains(err.Error(), "expected pairs") {
		t.Fatalf("odd bounds err %v", err)
	}
	restore()

	setBound := func(nth int, va uint32) {
		i := indexOfOp(t, cinit, x86asm.MOV, nth)
		cinit.Instructions[i].Inst.Args[1] = x86asm.Imm(va)
	}
	setBound(2, startupTableA+2)
	if _, err := d.staticInitializers(); err == nil || !strings.Contains(err.Error(), "whole number of pointers") {
		t.Fatalf("misaligned err %v", err)
	}
	restore()

	setBound(1, 0x120FC)
	setBound(2, 0x13000)
	if _, err := d.staticInitializers(); err == nil || !strings.Contains(err.Error(), "outside the image") {
		t.Fatalf("outside err %v", err)
	}
	restore()

	data := d.Image.FindSection(".data").Data
	entry := startupTableA + 4 - 0x12000
	saveWord := append([]byte(nil), data[entry:entry+4]...)
	copy(data[entry:], words32(at["init1"]+1))
	if _, err := d.staticInitializers(); err == nil || !strings.Contains(err.Error(), "is no function") {
		t.Fatalf("non function err %v", err)
	}
	copy(data[entry:], saveWord)

	for _, off := range []uint32{startupTableA - 0x12000, startupTableA + 8 - 0x12000, startupTableB + 4 - 0x12000} {
		copy(data[off:], words32(0))
	}
	if _, err := d.staticInitializers(); err == nil || !strings.Contains(err.Error(), "walks no initializer") {
		t.Fatalf("empty err %v", err)
	}
}

func TestCallsWithin(t *testing.T) {
	d, at := startupDisassembly(t)
	registrar := at["registrar"]
	if !d.callsWithin(at["init1"], registrar, 0) || !d.callsWithin(at["init1"], registrar, 3) {
		t.Error("direct call missed")
	}
	if d.callsWithin(at["init2"], registrar, 0) || !d.callsWithin(at["init2"], registrar, 1) {
		t.Error("nested call depth")
	}
	if d.callsWithin(at["init3"], registrar, 5) || d.callsWithin(at["init1"], at["other"], 5) {
		t.Error("library callee or absent call counted")
	}
	if got := d.plainCallees(at["init1"]); !equalU32(got, []uint32{registrar}) {
		t.Errorf("plain callees %x", got)
	}
}

func TestReplayRegistrations(t *testing.T) {
	d, at := startupDisassembly(t)
	rm := startupModel(t, d)
	types := map[string]*NodeType{"Thing": {Name: "Thing", Desc: startupDesc}}
	if err := rm.replayRegistrations(d, at["registrar"], types); err != nil {
		t.Fatal(err)
	}
	if v, err := rm.loadStartup(startupDesc, 4); err != nil || v != 0x2A {
		t.Fatalf("descriptor %08X %v", v, err)
	}
	if v, err := rm.loadStartup(startupDesc2, 4); err != nil || v != 0 {
		t.Fatalf("untouched word %08X %v", v, err)
	}
	if v, _ := d.Image.ReadU32(startupDesc); v != 0 {
		t.Fatal("replay wrote into the image")
	}
}

func TestReplayRegistrationsErrors(t *testing.T) {
	d, at := startupDisassembly(t)
	rm := startupModel(t, d)
	if err := rm.replayRegistrations(d, at["registrar"], map[string]*NodeType{"Ghost": {Desc: startupDesc2}}); err == nil || !strings.Contains(err.Error(), "no static initializer registers node type Ghost") {
		t.Fatalf("unregistered err %v", err)
	}
	if err := rm.replayRegistrations(d, at["registrar"], map[string]*NodeType{"Far": {Desc: 0x15000}}); err == nil || !strings.Contains(err.Error(), "outside the memory") {
		t.Fatalf("far err %v", err)
	}
	data := d.Image.FindSection(".data").Data
	copy(data[startupTableA+4-0x12000:], words32(at["badInit"]))
	err := rm.replayRegistrations(d, at["registrar"], map[string]*NodeType{"Thing": {Desc: startupDesc}})
	if err == nil || !strings.Contains(err.Error(), "calls SomethingElse") || !strings.Contains(err.Error(), "registration replay does not run") {
		t.Fatalf("library call err %v", err)
	}
	bare := disassembleImage(t, d.Image)
	if err := startupModel(t, bare).replayRegistrations(bare, at["registrar"], nil); err == nil || !strings.Contains(err.Error(), "_cinit") {
		t.Fatalf("unnamed err %v", err)
	}
}

func TestAbsoluteStore(t *testing.T) {
	if addr, ok := absoluteStore(Effect{Off: 0x12345}); !ok || addr != 0x12345 {
		t.Errorf("plain %X %v", addr, ok)
	}
	if _, ok := absoluteStore(Effect{Base: "stack", Off: 4}); ok {
		t.Error("stack store absolute")
	}
	if addr, ok := absoluteStore(Effect{Addr: IConst{0x1000}}); !ok || addr != 0x1000 {
		t.Errorf("constant address %X %v", addr, ok)
	}
	if addr, ok := absoluteStore(Effect{Addr: Ptr{Off: 0x2000}}); !ok || addr != 0x2000 {
		t.Errorf("pointer address %X %v", addr, ok)
	}
	if _, ok := absoluteStore(Effect{Addr: Sym{"x"}}); ok {
		t.Error("symbolic address absolute")
	}
}

func TestStartupMemory(t *testing.T) {
	d, _ := startupDisassembly(t)
	rm := startupModel(t, d)
	if v, err := rm.loadStartup(0x12048, 4); err != nil || v != 0x77777777 {
		t.Fatalf("word %08X %v", v, err)
	}
	if v, err := rm.loadStartup(0x12049, 2); err != nil || v != 0x7777 {
		t.Fatalf("half %04X %v", v, err)
	}
	if v, err := rm.loadStartup(0x1204B, 1); err != nil || v != 0x77 {
		t.Fatalf("byte %02X %v", v, err)
	}
	if _, err := rm.loadStartup(0x12048, 3); err == nil {
		t.Fatal("three byte load")
	}
	if _, err := rm.loadStartup(0x120FE, 4); err == nil {
		t.Fatal("load crossing the end")
	}
	if _, err := rm.loadStartup(0x15000, 4); err == nil {
		t.Fatal("load outside")
	}
	if err := rm.storeStartup(0x12050, 4, 0x11223344); err != nil {
		t.Fatal(err)
	}
	if err := rm.storeStartup(0x12054, 2, 0xABCD); err != nil {
		t.Fatal(err)
	}
	if err := rm.storeStartup(0x12056, 1, 0xEF); err != nil {
		t.Fatal(err)
	}
	if err := rm.storeStartup(0x12058, 3, 1); err == nil {
		t.Fatal("three byte store")
	}
	if err := rm.storeStartup(0x15000, 4, 1); err == nil {
		t.Fatal("store outside")
	}
	b, err := rm.startupBytes(0x12050, 8)
	if err != nil || !equalBytes(b, []byte{0x44, 0x33, 0x22, 0x11, 0xCD, 0xAB, 0xEF, 0x00}) {
		t.Fatalf("stored bytes %x %v", b, err)
	}
	if v, err := rm.loadStartup(0x11000, 1); err != nil || v != 0x55 {
		t.Fatalf("code byte %02X %v", v, err)
	}
	if v, err := rm.loadStartup(imgBase, 4); err != nil || v != 0x48454258 {
		t.Fatalf("header word %08X %v", v, err)
	}
	if v, err := rm.startupValue(IConst{7}); err != nil || v != 7 {
		t.Fatalf("constant value %d %v", v, err)
	}
	if v, err := rm.startupValue(Load{Off: 0x12048, Size: 4}); err != nil || v != 0x77777777 {
		t.Fatalf("loaded value %X %v", v, err)
	}
	if _, err := rm.startupValue(Sym{"x"}); err == nil || !strings.Contains(err.Error(), "not fixed at startup") {
		t.Fatalf("symbolic value err %v", err)
	}
	if _, err := rm.startupValue(Load{Base: "stack", Off: 4, Size: 4}); err == nil {
		t.Fatal("stack load valued")
	}
}

func equalBytes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
