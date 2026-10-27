package xbe

import (
	"fmt"
	"strings"
	"testing"
)

const (
	ordAvSendTVEncoderOption = 2
	ordAvSetDisplayMode      = 3
	ordDbgLoadImageSymbols   = 7
	ordDbgPrint              = 8
	ordKdDebuggerEnabled     = 88
	ordKeGetCurrentThread    = 104
	ordKeInitializeInterrupt = 109
	ordKeSetEvent            = 145
	ordKeTickCount           = 156
)

// Lays out a kernel whose exports are routines, a data variable, a shared body, a late entry and a flag
func kernelTestImage(t *testing.T) (*Image, map[string]uint32) {
	t.Helper()
	a := newAsm(0x11000)
	a.label("entry")
	a.emit(0x55, 0x8B, 0xEC)
	a.rel32([]byte{0xE8}, "shared")
	a.rel32([]byte{0xE8}, "tail")
	a.rel32([]byte{0xE8}, "reader")
	a.emit(0x5D, 0xC3)
	a.align(16, 0xCC)
	a.label("lead")
	a.emit(0x33, 0xC0)
	a.label("getThread")
	a.emit(0xA1)
	a.u32(0x12004)
	a.emit(0xC3)
	a.align(16, 0xCC)
	a.label("setEvent")
	a.emit(0x55, 0x8B, 0xEC, 0x8B, 0x45, 0x08, 0xA3)
	a.u32(0x12008)
	a.emit(0x5D, 0xC2, 0x0C, 0x00)
	a.align(16, 0xCC)
	a.label("shared")
	a.emit(0x33, 0xC0, 0xC3)
	a.align(16, 0xCC)
	a.label("tail")
	a.emit(0xB8, 0x01, 0x00, 0x00, 0x00, 0x83, 0xC0, 0x02, 0xC3)
	a.align(16, 0xCC)
	a.label("reader")
	a.emit(0xA1)
	a.u32(0x12000)
	a.emit(0xA0)
	a.word("flag")
	a.emit(0xC3)
	a.align(16, 0xCC)
	a.label("flag")
	a.emit(0xB8, 0x5A, 0x00, 0x00, 0x00, 0xC3)
	a.align(16, 0xCC)
	a.label("dbgPrint")
	a.emit(0x8B, 0x44, 0x24, 0x04, 0xC3)
	a.align(16, 0xCC)
	a.label("initInterrupt")
	a.emit(0x55, 0x8B, 0xEC, 0x56, 0x57)
	a.abs32([]byte{0xBE}, "template")
	a.emit(0x8B, 0x7D, 0x08, 0x6A, 0x06, 0x59, 0xF3, 0xA5)
	a.emit(0x8B, 0x55, 0x08, 0x8B, 0x45, 0x0C)
	a.abs32Plus([]byte{0x89, 0x82}, "template", 2)
	a.emit(0x8B, 0x4D, 0x10)
	a.abs32Plus([]byte{0x89, 0x8A}, "template", 9)
	a.emit(0x5F, 0x5E, 0x5D, 0xC2, 0x0C, 0x00)
	a.align(16, 0xCC)
	a.label("copyOnly")
	a.abs32([]byte{0xBE}, "template")
	a.emit(0x8B, 0x7C, 0x24, 0x04, 0xB9, 0x06, 0x00, 0x00, 0x00, 0xF3, 0xA5, 0xC3)
	a.align(16, 0xCC)
	a.label("template")
	a.emit(0x50, 0xB8, 0xEF, 0xBE, 0xAD, 0xDE, 0xFF, 0xD0, 0xB9, 0x78, 0x56, 0x34, 0x12, 0x58, 0xCF)
	a.pad(9, 0xCC)
	a.label("templateEnd")
	a.align(16, 0xCC)
	a.label("zeros")
	a.pad(16, 0)
	a.label("end")
	data := words32(0x12345678, 0x00020000, 0, 0xCAFEBABE)
	late := newAsm(0x13000)
	late.label("lateEntry")
	late.emit(0xB8, 0x03, 0x00, 0x00, 0x00, 0xC3)
	late.align(16, 0xCC)
	a.labels["lateEntry"] = late.at("lateEntry")
	img := parseXBE(t, xbeSpec{
		entry: 0x11000,
		sections: []testSection{
			{name: ".text", va: 0x11000, flags: flagExecutable, data: a.bytes(), vsize: 0x1000},
			{name: ".data", va: 0x12000, flags: flagWritable, data: data, vsize: 0x100},
			{name: ".init", va: 0x13000, data: late.bytes(), vsize: 0x100},
		},
		libs: []LibVersion{},
	})
	return img, a.labels
}

func kernelExportsOf(at map[string]uint32) map[uint32]uint32 {
	return map[uint32]uint32{
		ordAvSendTVEncoderOption: at["shared"],
		ordAvSetDisplayMode:      at["shared"],
		ordDbgLoadImageSymbols:   at["lateEntry"],
		ordDbgPrint:              at["dbgPrint"],
		ordKdDebuggerEnabled:     at["flag"],
		ordKeGetCurrentThread:    at["getThread"],
		ordKeInitializeInterrupt: at["initInterrupt"],
		ordKeSetEvent:            at["setEvent"],
		ordKeTickCount:           0x12000,
	}
}

func kernelDisassembly(t *testing.T) (*Disassembly, map[string]uint32, map[uint32]uint32) {
	t.Helper()
	img, at := kernelTestImage(t)
	exports := kernelExportsOf(at)
	d, err := DisassembleKernel(img, exports)
	if err != nil {
		t.Fatalf("DisassembleKernel: %v", err)
	}
	return d, at, exports
}

func TestDisassembleKernelNames(t *testing.T) {
	d, at, _ := kernelDisassembly(t)
	if !d.kernel || len(d.Imports) != 0 {
		t.Fatalf("kernel flag %v imports %v", d.kernel, d.Imports)
	}
	want := map[string]struct {
		name     string
		argBytes int
		cdecl    bool
	}{
		"getThread":     {"KeGetCurrentThread", 0, false},
		"setEvent":      {"KeSetEvent", 12, false},
		"shared":        {"AvSendTVEncoderOption", 0, false},
		"lateEntry":     {"DbgLoadImageSymbols", 0, false},
		"dbgPrint":      {"DbgPrint", 0, true},
		"initInterrupt": {"KeInitializeInterrupt", 12, false},
	}
	for label, w := range want {
		fn := mustFunction(t, d, at[label])
		if fn.Name != w.name || fn.Symbol == nil || fn.Symbol.Name != w.name || fn.Symbol.Raw != w.name || fn.Symbol.Lib != "kernel" || fn.Symbol.VA != at[label] || fn.Symbol.ArgBytes != w.argBytes || fn.Symbol.Cdecl != w.cdecl {
			t.Errorf("%s named %q %+v", label, fn.Name, fn.Symbol)
		}
	}
	if alts := d.Functions[at["shared"]].Symbol.Alternates; !equalStrings(alts, []string{"AvSetDisplayMode"}) {
		t.Errorf("shared alternates %v", alts)
	}
	if d.Functions[at["flag"]] != nil {
		t.Error("export read as memory kept as a routine")
	}
	if d.Functions[0x12000] != nil {
		t.Error("data export became a routine")
	}
	if fn := mustFunction(t, d, at["lateEntry"]); !equalStrings(opsOf(fn), []string{"MOV", "RET"}) {
		t.Errorf("late entry %v", opsOf(fn))
	}
	if fn := mustFunction(t, d, at["tail"]); !equalStrings(opsOf(fn), []string{"MOV", "ADD", "RET"}) {
		t.Errorf("tail %v", opsOf(fn))
	}
	if fn := mustFunction(t, d, at["lead"]); len(fn.Instructions) != 1 {
		t.Errorf("code before an export runs into it %v", opsOf(fn))
	}
	if fn := mustFunction(t, d, at["entry"]); fn.Symbol != nil || !equalU32(fn.CallTargets, []uint32{at["shared"], at["tail"], at["reader"]}) {
		t.Errorf("entry %+v", fn)
	}
	if !equalU32(mustFunction(t, d, at["shared"]).CalledBy, []uint32{at["entry"]}) {
		t.Errorf("shared called by %x", d.Functions[at["shared"]].CalledBy)
	}
	setEvent := at["setEvent"]
	if got, want := d.Describe(setEvent), fmt.Sprintf("%08X KeSetEvent: 6 instructions through %08X, cleanup 12 bytes, returns true, rets map[RET 0xc:1]", setEvent, setEvent+12); got != want {
		t.Errorf("describe\n%s\n%s", got, want)
	}
}

func TestDisassembleKernelRejectsUnnamedExport(t *testing.T) {
	img, at := kernelTestImage(t)
	exports := kernelExportsOf(at)
	exports[367] = at["shared"]
	if _, err := DisassembleKernel(img, exports); err == nil || !strings.Contains(err.Error(), "367") {
		t.Fatalf("err %v", err)
	}
}

func TestDisassembleKernelSkipsImplausibleExports(t *testing.T) {
	img, at := kernelTestImage(t)
	exports := kernelExportsOf(at)
	exports[ordKeTickCount] = at["zeros"]
	d, err := DisassembleKernel(img, exports)
	if err != nil {
		t.Fatal(err)
	}
	if d.Functions[at["zeros"]] != nil {
		t.Error("zero bytes exported as a routine")
	}
	if d.Functions[at["getThread"]] == nil {
		t.Error("routine export lost")
	}
}

func TestDeriveKernel(t *testing.T) {
	d, at, exports := kernelDisassembly(t)
	km, err := DeriveKernel(d, exports)
	if err != nil {
		t.Fatal(err)
	}
	if km.Base != imgBase || km.Entry != at["entry"] {
		t.Fatalf("base %08X entry %08X", km.Base, km.Entry)
	}
	wantExports := map[string]uint32{}
	for ordinal, va := range exports {
		wantExports[KernelExportName(ordinal)] = va
	}
	if len(km.Exports) != len(wantExports) {
		t.Fatalf("exports %v", km.Exports)
	}
	for name, va := range wantExports {
		if km.Exports[name] != va {
			t.Errorf("export %s at %08X, want %08X", name, km.Exports[name], va)
		}
	}
	wantNames := map[uint32]string{at["getThread"]: "KeGetCurrentThread", at["setEvent"]: "KeSetEvent", at["shared"]: "AvSendTVEncoderOption", at["lateEntry"]: "DbgLoadImageSymbols", at["dbgPrint"]: "DbgPrint", at["initInterrupt"]: "KeInitializeInterrupt"}
	if len(km.Names) != len(wantNames) {
		t.Errorf("names %v", km.Names)
	}
	for va, name := range wantNames {
		if km.Names[va] != name {
			t.Errorf("%08X named %q, want %q", va, km.Names[va], name)
		}
	}
	if len(km.Errors) != 0 {
		t.Fatalf("errors %v", km.Errors)
	}
	for _, label := range []string{"entry", "getThread", "setEvent", "shared", "lateEntry", "dbgPrint", "initInterrupt", "tail", "reader"} {
		lf := km.Lifted[at[label]]
		if lf == nil {
			t.Errorf("%s not lifted", label)
			continue
		}
		if lf.Entry != blockKey(at[label], nil) || len(lf.blocks) == 0 || lf.blocks[lf.Entry] == nil {
			t.Errorf("%s lifted as %+v", label, lf)
		}
	}
	if len(km.Lifted) != 9 {
		t.Errorf("lifted %d routines", len(km.Lifted))
	}
	if term := km.Lifted[at["shared"]].blocks[blockKey(at["shared"], nil)].term; term == nil || term.kind != "return" {
		t.Errorf("shared terminator %+v", term)
	}
	for _, label := range []string{"flag", "lead", "template"} {
		if km.Lifted[at[label]] != nil {
			t.Errorf("%s lifted", label)
		}
	}
	if _, lifted := km.Lifted[0x12000]; lifted {
		t.Error("data export lifted")
	}
	if lf := km.Lifted[at["setEvent"]]; lf != nil && lf.Params != nil {
		t.Errorf("stack only routine has params %v", lf.Params)
	}
}

func TestDeriveKernelErrors(t *testing.T) {
	d, at, exports := kernelDisassembly(t)
	if _, err := DeriveKernel(d, map[uint32]uint32{367: at["shared"], 368: at["tail"]}); err == nil || !strings.Contains(err.Error(), "appears twice") {
		t.Fatalf("duplicate err %v", err)
	}
	delete(d.Functions, at["entry"])
	if _, err := DeriveKernel(d, exports); err == nil || !strings.Contains(err.Error(), "is no routine") {
		t.Fatalf("entry err %v", err)
	}
}
