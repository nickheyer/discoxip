package xbe

import (
	"fmt"
	"strings"
	"testing"

	"golang.org/x/arch/x86/x86asm"
)

// Lays out every routine shape the disassembly tests inspect, named by label, with its thunk table and data
func analysisImage(t *testing.T) (*Image, map[string]uint32) {
	t.Helper()
	a := newAsm(0x11000)
	a.label("entry")
	a.emit(0x55, 0x8B, 0xEC, 0x6A, 0x11)
	for _, f := range []string{"args", "switcher", "thunk", "probeUser"} {
		a.rel32([]byte{0xE8}, f)
	}
	a.label("importCall")
	a.emit(0xFF, 0x15)
	a.u32(0x12000)
	a.label("cookieCall")
	for _, f := range []string{"cookieCheck", "cdeclUser", "resync", "memcpy", "memset", "mathThunk", "powThunk", "bogusThunk", "eaxThunk", "noSwap", "oneSpill", "plainCore", "pre", "libFn", "spin", "flow", "loads", "live", "frames", "decomp"} {
		a.rel32([]byte{0xE8}, f)
	}
	a.emit(0x5D, 0xC3)
	a.align(16, 0xCC)
	a.label("args")
	a.emit(0x55, 0x8B, 0xEC, 0x8B, 0x45, 0x08, 0x8B, 0x4D, 0x0C, 0x03, 0xC1, 0x5D, 0xC2, 0x08, 0x00)
	a.align(16, 0xCC)
	a.label("switcher")
	a.emit(0x8B, 0x44, 0x24, 0x04, 0x83, 0xF8, 0x02)
	a.rel8(0x77, "default")
	a.label("switchJump")
	a.abs32([]byte{0xFF, 0x24, 0x85}, "table")
	a.label("case0")
	a.emit(0xB8, 0x0D, 0x0C, 0x0B, 0x0A, 0xC3)
	a.label("case1")
	a.emit(0xB8, 0x1D, 0x1C, 0x1B, 0x1A, 0xC3)
	a.label("case2")
	a.emit(0x33, 0xC0, 0xC3)
	a.label("default")
	a.emit(0xB8, 0xFF, 0xFF, 0xFF, 0xFF, 0xC3)
	a.align(4, 0xCC)
	a.label("table")
	a.word("case0")
	a.word("case1")
	a.word("case2")
	a.label("tableEnd")
	a.u32(0x12000)
	a.align(16, 0xCC)
	a.label("thunk")
	a.rel32([]byte{0xE9}, "target")
	a.align(16, 0xCC)
	a.label("target")
	a.emit(0x55, 0x8B, 0xEC, 0xB8, 0x4D, 0x3C, 0x2B, 0x1A, 0x5D, 0xC3)
	a.align(16, 0xCC)
	a.label("probeUser")
	a.emit(0x55, 0x8B, 0xEC, 0xB8, 0x00, 0x20, 0x00, 0x00)
	a.label("probeCall")
	a.rel32([]byte{0xE8}, "chkstk")
	a.emit(0x8B, 0x45, 0x08, 0xC9, 0xC2, 0x04, 0x00)
	a.align(16, 0xCC)
	a.label("chkstk")
	a.emit(0x85, 0xC0, 0x3D, 0x00, 0x10, 0x00, 0x00)
	a.rel8(0x72, "probeDone")
	a.emit(0x2D, 0x00, 0x10, 0x00, 0x00)
	a.label("probeDone")
	a.emit(0x94, 0xC3)
	a.align(16, 0xCC)
	a.label("cdeclUser")
	a.emit(0x6A, 0x01, 0x6A, 0x02)
	a.label("dbgCall")
	a.emit(0xFF, 0x15)
	a.u32(0x12008)
	a.emit(0x83, 0xC4, 0x08, 0x6A, 0x03)
	a.label("eventCall")
	a.emit(0xFF, 0x15)
	a.u32(0x12000)
	a.label("argsCall")
	a.rel32([]byte{0xE8}, "args")
	a.label("thunkCall")
	a.rel32([]byte{0xE8}, "thunk")
	a.emit(0xC3)
	a.align(16, 0xCC)
	a.label("cookieCheck")
	a.emit(0x3B, 0x0D)
	a.u32(0x12100)
	a.emit(0x75, 0x01, 0xC3, 0xCC)
	a.align(16, 0xCC)
	a.label("stray")
	a.emit(0x8E)
	a.label("resync")
	a.emit(0x55, 0x8B, 0xEC, 0x33, 0xC0, 0x5D, 0xC3)
	a.align(16, 0xCC)
	a.label("memcpy")
	a.emit(0x56, 0x57, 0x8B, 0x7C, 0x24, 0x0C, 0x8B, 0x74, 0x24, 0x10, 0x8B, 0x4C, 0x24, 0x14, 0x8B, 0xC7, 0x83, 0xE0, 0x03)
	a.label("alignJump")
	a.abs32([]byte{0xFF, 0x24, 0x85}, "alignTable")
	a.label("aligned")
	a.emit(0x8B, 0xD1, 0xC1, 0xE9, 0x02, 0xF3, 0xA5, 0x83, 0xE2, 0x03)
	a.label("tailJump")
	a.abs32([]byte{0xFF, 0x24, 0x95}, "tailTable")
	a.label("unaligned")
	a.emit(0xA4, 0x49)
	a.label("backJump")
	a.rel8(0xEB, "aligned")
	a.label("tail0")
	a.emit(0x5F, 0x5E, 0xC3)
	a.label("tail1")
	a.emit(0xA4, 0x5F, 0x5E, 0xC3)
	a.align(4, 0xCC)
	a.label("alignTable")
	a.word("aligned")
	a.word("unaligned")
	a.u32(0x12000)
	a.label("tailTable")
	a.word("tail0")
	a.word("tail1")
	a.u32(0x12000)
	a.align(16, 0xCC)
	a.label("memset")
	a.emit(0x57, 0x8B, 0x7C, 0x24, 0x08, 0x8B, 0x44, 0x24, 0x0C, 0x8B, 0x4C, 0x24, 0x10, 0x8B, 0xD7, 0x83, 0xE2, 0x03)
	a.abs32([]byte{0xFF, 0x24, 0x95}, "sAlignTable")
	a.label("sAligned")
	a.emit(0x8B, 0xD1, 0xC1, 0xE9, 0x02, 0xF3, 0xAB, 0x83, 0xE2, 0x03)
	a.abs32([]byte{0xFF, 0x24, 0x95}, "sTailTable")
	a.label("sUnaligned")
	a.emit(0xAA, 0x49)
	a.rel8(0xEB, "sAligned")
	a.label("sTail0")
	a.emit(0x5F, 0xC3)
	a.label("sTail1")
	a.emit(0xAA, 0x5F, 0xC3)
	a.align(4, 0xCC)
	a.label("sAlignTable")
	a.word("sAligned")
	a.word("sUnaligned")
	a.u32(0x12000)
	a.label("sTailTable")
	a.word("sTail0")
	a.word("sTail1")
	a.u32(0x12000)
	a.align(16, 0xCC)
	a.label("mathThunk")
	a.emit(0xBA)
	a.u32(0x12010)
	a.rel32([]byte{0xE9}, "mathCore")
	a.align(16, 0xCC)
	a.label("mathCore")
	a.emit(0x55, 0x8B, 0xEC, 0xDD, 0x45, 0x08, 0xD9, 0xFE, 0x5D, 0xC3)
	a.align(16, 0xCC)
	a.label("powThunk")
	a.emit(0x83, 0xEC, 0x10, 0xD9, 0xC9, 0xDD, 0x1C, 0x24, 0xDD, 0x5C, 0x24, 0x08)
	a.rel32([]byte{0xE8}, "powCore")
	a.emit(0x83, 0xC4, 0x10, 0xC3)
	a.align(16, 0xCC)
	a.label("powCore")
	a.emit(0x55, 0x8B, 0xEC, 0xD9, 0xF1, 0xD9, 0xFD, 0x5D, 0xC3)
	a.align(16, 0xCC)
	a.label("bogusThunk")
	a.emit(0xBA)
	a.u32(0x12020)
	a.rel32([]byte{0xE9}, "mathCore")
	a.align(16, 0xCC)
	a.label("eaxThunk")
	a.emit(0xB8)
	a.u32(0x12010)
	a.rel32([]byte{0xE9}, "mathCore")
	a.align(16, 0xCC)
	a.label("noSwap")
	a.emit(0x83, 0xEC, 0x10, 0xDD, 0x1C, 0x24, 0xDD, 0x5C, 0x24, 0x08)
	a.rel32([]byte{0xE8}, "powCore")
	a.emit(0x83, 0xC4, 0x10, 0xC3)
	a.align(16, 0xCC)
	a.label("oneSpill")
	a.emit(0x83, 0xEC, 0x10, 0xD9, 0xC9, 0xDD, 0x1C, 0x24)
	a.rel32([]byte{0xE8}, "powCore")
	a.emit(0x83, 0xC4, 0x10, 0xC3)
	a.align(16, 0xCC)
	a.label("plainCore")
	a.emit(0x83, 0xEC, 0x10, 0xD9, 0xC9, 0xDD, 0x1C, 0x24, 0xDD, 0x5C, 0x24, 0x08)
	a.rel32([]byte{0xE8}, "mathCore")
	a.emit(0x83, 0xC4, 0x10, 0xC3)
	a.align(16, 0xCC)
	a.label("pre")
	a.emit(0x33, 0xC0)
	a.label("prologue")
	a.emit(0x55, 0x8B, 0xEC, 0x5D, 0xC3)
	a.align(16, 0xCC)
	a.label("spin")
	a.emit(0xEB, 0xFE)
	a.align(16, 0xCC)
	a.label("flow")
	a.emit(0xBE)
	a.u32(0x12100)
	a.emit(0x8B, 0xFE, 0xB8, 0x00, 0x10, 0x01, 0x00, 0x85, 0xC9)
	a.rel8(0x74, "flowJoin")
	a.emit(0xBF)
	a.u32(0x12110)
	a.rel32([]byte{0xE8}, "libFn")
	a.label("flowJoin")
	a.emit(0x89, 0x37, 0xC3)
	a.align(16, 0xCC)
	a.label("loads")
	a.emit(0xA1)
	a.u32(0x12100)
	a.emit(0x8B, 0xC8, 0x8B, 0x15)
	a.u32(0x12104)
	a.emit(0x83, 0xC0, 0x01, 0xC3)
	a.align(16, 0xCC)
	a.label("live")
	a.emit(0x53, 0x56, 0x8B, 0x41, 0x04, 0x8B, 0x5A, 0x08, 0x50, 0x57)
	a.rel32([]byte{0xE8}, "libFn")
	a.emit(0x83, 0xC4, 0x08, 0x5E, 0x5B, 0xC3)
	a.align(16, 0xCC)
	a.label("frames")
	a.emit(0x55, 0x8B, 0xEC, 0x83, 0xEC, 0x10, 0x56, 0x8B, 0x45, 0x10, 0x8D, 0x65, 0xFC, 0x5E, 0x8B, 0xE5, 0x5D, 0xC3)
	a.align(16, 0xCC)
	a.label("decomp")
	a.emit(0x55, 0x8B, 0xEC, 0x83, 0xEC, 0x10, 0x8B, 0x45, 0x08, 0x89, 0x45, 0xFC, 0xC6, 0x45, 0xFF, 0x07, 0x66, 0xC7, 0x45, 0xFE, 0x34, 0x12)
	a.emit(0x68)
	a.u32(0x13000)
	a.emit(0x6A, 0x05, 0xFF, 0x15)
	a.u32(0x12000)
	a.label("decompArgsCall")
	a.rel32([]byte{0xE8}, "args")
	a.label("decompAfterArgs")
	a.emit(0x8B, 0x10, 0xFF, 0x52, 0x08)
	a.label("decompAfterVCall")
	a.emit(0x33, 0xC0, 0x3D, 0x00, 0x01, 0x00, 0x00)
	a.rel8(0x75, "decompSkip")
	a.label("decompLea")
	a.emit(0x8D, 0x45, 0xFC, 0x89, 0x45, 0xF8)
	a.label("decompSkip")
	a.emit(0xC9, 0xC2, 0x08, 0x00)
	a.align(16, 0xCC)
	a.label("gap")
	a.emit(0xB8, 0x5A, 0x5A, 0x00, 0x00, 0xC3)
	a.align(16, 0xCC)
	a.label("zeros")
	a.pad(16, 0)
	a.label("end")

	lib := newAsm(0x14000)
	lib.label("libFn")
	lib.emit(0xB8, 0x01, 0x00, 0x00, 0x00, 0xC3)
	lib.align(16, 0xCC)
	a.labels["libFn"] = lib.at("libFn")

	data := thunkTable(145, 367, 8)
	data = append(data, make([]byte, 0x10-len(data))...)
	data = append(data, append([]byte{0x01}, []byte("sin\x00")...)...)
	data = append(data, make([]byte, 0x20-len(data))...)
	data = append(data, append([]byte{0x01}, []byte("bogus\x00")...)...)
	data = append(data, make([]byte, 0x100-len(data))...)
	data = append(data, words32(0x5A5A5A5A, 0x12345678, 0, 0, 0xCAFEBABE)...)

	img := parseXBE(t, xbeSpec{
		entry:  0x11000,
		kthunk: 0x12000,
		sections: []testSection{
			{name: ".text", va: 0x11000, flags: flagExecutable, data: a.bytes(), vsize: 0x1000},
			{name: ".data", va: 0x12000, flags: flagWritable, data: data, vsize: 0x1000},
			{name: ".rdata", va: 0x13000, data: wideBytes("Hello"), vsize: 0x100},
			{name: ".lib", va: 0x14000, flags: flagExecutable, data: lib.bytes(), vsize: 0x100},
		},
	})
	return img, a.labels
}

// Disassembles the shared analysis image afresh for a test
func analysis(t *testing.T) (*Disassembly, map[string]uint32) {
	t.Helper()
	img, at := analysisImage(t)
	return disassembleImage(t, img), at
}

func TestResolveImports(t *testing.T) {
	d, _ := analysis(t)
	if len(d.Imports) != 2 || d.Imports[0x12000] != "KeSetEvent" || d.Imports[0x12008] != "DbgPrint" {
		t.Fatalf("imports %v", d.Imports)
	}
	if _, named := d.Imports[0x12004]; named {
		t.Fatal("unnamed ordinal imported")
	}
}

func TestFunctionDiscovery(t *testing.T) {
	d, at := analysis(t)
	for _, name := range []string{"entry", "args", "switcher", "thunk", "target", "probeUser", "chkstk", "cdeclUser", "cookieCheck", "resync", "memcpy", "memset", "mathThunk", "mathCore", "powThunk", "powCore", "bogusThunk", "eaxThunk", "noSwap", "oneSpill", "plainCore", "pre", "prologue", "spin", "flow", "loads", "live", "frames", "decomp", "gap", "libFn"} {
		if d.Functions[at[name]] == nil {
			t.Errorf("%s at %08X not discovered", name, at[name])
		}
	}
	for _, name := range []string{"case0", "case1", "case2", "default", "aligned", "unaligned", "tail0", "tail1", "sAligned", "sUnaligned", "sTail0", "sTail1", "stray", "zeros", "probeDone", "flowJoin", "decompSkip"} {
		if fn := d.Functions[at[name]]; fn != nil {
			t.Errorf("%s at %08X discovered as a function of %d instructions", name, at[name], len(fn.Instructions))
		}
	}
	for va := at["zeros"]; va < at["end"]; va++ {
		if d.Functions[va] != nil {
			t.Errorf("function inside the zero bytes at %08X", va)
		}
	}
	for va := range d.Functions {
		if va < 0x11000 || (va >= at["end"] && va < 0x14000) || va >= 0x14010 {
			t.Errorf("function at %08X outside the code", va)
		}
	}
}

func TestCallGraph(t *testing.T) {
	d, at := analysis(t)
	entry := mustFunction(t, d, at["entry"])
	want := []uint32{at["args"], at["switcher"], at["thunk"], at["probeUser"]}
	for _, f := range []string{"cookieCheck", "cdeclUser", "resync", "memcpy", "memset", "mathThunk", "powThunk", "bogusThunk", "eaxThunk", "noSwap", "oneSpill", "plainCore", "pre", "libFn", "spin", "flow", "loads", "live", "frames", "decomp"} {
		want = append(want, at[f])
	}
	if !equalU32(entry.CallTargets, want) {
		t.Fatalf("entry calls %x, want %x", entry.CallTargets, want)
	}
	if !equalU32(mustFunction(t, d, at["args"]).CalledBy, []uint32{at["entry"], at["cdeclUser"], at["decomp"]}) {
		t.Fatalf("args called by %x", d.Functions[at["args"]].CalledBy)
	}
	thunk := mustFunction(t, d, at["thunk"])
	if len(thunk.Instructions) != 1 || thunk.Size != 5 || !equalU32(thunk.CallTargets, []uint32{at["target"]}) {
		t.Fatalf("thunk %+v", thunk)
	}
	if !equalU32(mustFunction(t, d, at["target"]).CalledBy, []uint32{at["thunk"]}) {
		t.Fatalf("target called by %x", d.Functions[at["target"]].CalledBy)
	}
	if !equalU32(mustFunction(t, d, at["mathCore"]).CalledBy, []uint32{at["mathThunk"], at["bogusThunk"], at["eaxThunk"], at["plainCore"]}) {
		t.Fatalf("math core called by %x", d.Functions[at["mathCore"]].CalledBy)
	}
	if !equalU32(mustFunction(t, d, at["chkstk"]).CalledBy, []uint32{at["probeUser"]}) {
		t.Fatalf("probe called by %x", d.Functions[at["chkstk"]].CalledBy)
	}
	if !equalU32(mustFunction(t, d, at["libFn"]).CalledBy, []uint32{at["entry"], at["flow"], at["live"]}) {
		t.Fatalf("lib called by %x", d.Functions[at["libFn"]].CalledBy)
	}
}

func TestFunctionBodies(t *testing.T) {
	d, at := analysis(t)
	args := mustFunction(t, d, at["args"])
	if !equalStrings(opsOf(args), []string{"PUSH", "MOV", "MOV", "MOV", "ADD", "POP", "RET"}) || args.Size != 15 {
		t.Fatalf("args %v size %d", opsOf(args), args.Size)
	}
	switcher := mustFunction(t, d, at["switcher"])
	if len(switcher.Instructions) != 12 || switcher.Size != int(at["default"]+6-at["switcher"]) {
		t.Fatalf("switcher %v size %d", opsOf(switcher), switcher.Size)
	}
	if !equalU32(switcher.Tables[at["switchJump"]], []uint32{at["case0"], at["case1"], at["case2"]}) || len(switcher.Tables) != 1 {
		t.Fatalf("switch tables %x", switcher.Tables)
	}
	memcpy := mustFunction(t, d, at["memcpy"])
	if len(memcpy.Instructions) != 23 || len(memcpy.Tables) != 2 || !equalU32(memcpy.Tables[at["alignJump"]], []uint32{at["aligned"], at["unaligned"]}) || !equalU32(memcpy.Tables[at["tailJump"]], []uint32{at["tail0"], at["tail1"]}) {
		t.Fatalf("memcpy %v tables %x", opsOf(memcpy), memcpy.Tables)
	}
	if memcpy.Size != int(at["tail1"]+4-at["memcpy"]) {
		t.Fatalf("memcpy size %d", memcpy.Size)
	}
	resync := mustFunction(t, d, at["resync"])
	if !equalStrings(opsOf(resync), []string{"PUSH", "MOV", "XOR", "POP", "RET"}) {
		t.Fatalf("resync %v", opsOf(resync))
	}
	if !equalStrings(opsOf(mustFunction(t, d, at["pre"])), []string{"XOR", "PUSH", "MOV", "POP", "RET"}) || !equalStrings(opsOf(mustFunction(t, d, at["prologue"])), []string{"PUSH", "MOV", "POP", "RET"}) {
		t.Fatalf("fallthrough bodies %v %v", opsOf(d.Functions[at["pre"]]), opsOf(d.Functions[at["prologue"]]))
	}
	cookie := mustFunction(t, d, at["cookieCheck"])
	if !equalStrings(opsOf(cookie), []string{"CMP", "JNE", "RET", "INT"}) {
		t.Fatalf("cookie check %v", opsOf(cookie))
	}
	for _, in := range args.Instructions {
		if d.InsnByVA[in.VA] == nil || d.InsnByVA[in.VA].Inst.Op != in.Inst.Op || len(in.Raw) != in.Inst.Len {
			t.Fatalf("instruction %08X not indexed", in.VA)
		}
	}
}

func TestResynchronize(t *testing.T) {
	d, at := analysis(t)
	resync := at["resync"]
	if d.InsnByVA[at["stray"]] == nil || d.InsnByVA[at["stray"]].Inst.Op != x86asm.MOV || d.InsnByVA[at["stray"]].Inst.Len != 3 {
		t.Fatalf("stray decode %v", d.InsnByVA[at["stray"]])
	}
	for _, off := range []uint32{0, 1, 3, 5, 6} {
		if in := d.InsnByVA[resync+off]; in == nil {
			t.Errorf("no instruction at %08X", resync+off)
		}
	}
	if d.InsnByVA[resync+2] == nil || d.InsnByVA[resync+2].Inst.Op != x86asm.IN {
		t.Fatal("sweep garbage inside the routine removed")
	}
	if d.Functions[at["stray"]] != nil {
		t.Fatal("stray byte recovered as a function")
	}
}

func TestJumpTableTargets(t *testing.T) {
	d, at := analysis(t)
	sec := d.sectionOf(at["switcher"])
	jump := d.InsnByVA[at["switchJump"]]
	if got := d.jumpTableTargets(jump, sec); !equalU32(got, []uint32{at["case0"], at["case1"], at["case2"]}) {
		t.Fatalf("targets %x", got)
	}
	mem := jump.Inst.Args[0].(x86asm.Mem)
	variant := func(m x86asm.Mem) *Instruction {
		in := *jump
		in.Inst.Args = x86asm.Args{m}
		return &in
	}
	if got := d.jumpTableTargets(variant(x86asm.Mem{Base: mem.Base, Index: mem.Index, Scale: 1, Disp: mem.Disp}), sec); got != nil {
		t.Fatalf("scale 1 targets %x", got)
	}
	if got := d.jumpTableTargets(variant(x86asm.Mem{Index: mem.Index, Scale: 4, Disp: 0}), sec); got != nil {
		t.Fatalf("zero displacement targets %x", got)
	}
	if got := d.jumpTableTargets(variant(x86asm.Mem{Scale: 4, Disp: mem.Disp}), sec); got != nil {
		t.Fatalf("no index targets %x", got)
	}
	if got := d.jumpTableTargets(variant(x86asm.Mem{Index: mem.Index, Scale: 4, Disp: int64(at["tableEnd"])}), sec); got != nil {
		t.Fatalf("table starting outside the section %x", got)
	}
	if got := d.jumpTableTargets(variant(x86asm.Mem{Index: mem.Index, Scale: 4, Disp: int64(at["table"] + 4)}), sec); !equalU32(got, []uint32{at["case1"], at["case2"]}) {
		t.Fatalf("table from its second entry %x", got)
	}
	if got := d.jumpTableTargets(jump, d.sectionOf(0x14000)); got != nil {
		t.Fatalf("targets outside the jump's section %x", got)
	}
	if got := d.jumpTableTargets(d.InsnByVA[at["thunk"]], sec); got != nil {
		t.Fatalf("direct jump targets %x", got)
	}
}

func TestPlausibleStart(t *testing.T) {
	d, at := analysis(t)
	if fn := d.buildFunction(at["zeros"], map[uint32]bool{}); fn == nil || plausibleStart(fn) {
		t.Fatal("zero bytes plausible")
	}
	if fn := d.buildFunction(at["stray"], map[uint32]bool{}); fn == nil || plausibleStart(fn) {
		t.Fatal("segment register load plausible")
	}
	if !plausibleStart(mustFunction(t, d, at["gap"])) || !plausibleStart(mustFunction(t, d, at["args"])) {
		t.Fatal("real code implausible")
	}
	for _, seq := range [][]byte{{0x3F}, {0x37}, {0xD5, 0x0A}, {0x27}, {0xEC}, {0xF4}, {0xCF}, {0xC8, 0x00, 0x00, 0x00}, {0x1F}, {0xCE}, {0xD7}, {0x8E, 0xC0}, {0x07}} {
		if plausibleStart(functionOf(t, 0x11000, seq)) {
			t.Errorf("%x plausible", seq)
		}
	}
	if !plausibleStart(functionOf(t, 0x11000, []byte{0x8B, 0xC1}, []byte{0x58}, []byte{0x00, 0xC0})) {
		t.Fatal("add al to al implausible")
	}
	late := functionOf(t, 0x11000, []byte{0x90}, []byte{0x90}, []byte{0x90}, []byte{0x90}, []byte{0x90}, []byte{0x90}, []byte{0x90}, []byte{0x90}, []byte{0x3F})
	if !plausibleStart(late) {
		t.Fatal("ninth instruction judged")
	}
}

func TestSectionRoles(t *testing.T) {
	d, at := analysis(t)
	if len(d.libraries()) != 1 || !d.libraries()[d.sectionOf(at["entry"])] || d.inLibrary(at["libFn"]) || d.inLibrary(at["entry"]) || d.inLibrary(at["memcpy"]) {
		t.Fatalf("libraries before naming %v", d.libraries())
	}
	if !d.inPureData(0x12100) || d.inPureData(at["entry"]) || d.inPureData(0x13000) || d.inPureData(at["libFn"]) || d.inPureData(0x15000) {
		t.Fatal("pure data roles before naming")
	}
	lib := mustFunction(t, d, at["libFn"])
	lib.Symbol = &Symbol{VA: lib.EntryVA, Name: "LibRoutine"}
	d.librarySections = nil
	if !d.libraries()[d.sectionOf(at["libFn"])] || len(d.libraries()) != 2 {
		t.Fatalf("libraries after naming %v", d.libraries())
	}
	if !d.inLibrary(at["libFn"]) || !d.inLibrary(0x140F0) || d.inLibrary(at["entry"]) || d.inLibrary(0x12100) || d.inLibrary(0x15000) {
		t.Fatal("library roles after naming")
	}
	if d.inPureData(at["libFn"]) || !d.inPureData(0x12100) {
		t.Fatal("pure data roles after naming")
	}
	d.kernel = true
	if d.inLibrary(at["libFn"]) {
		t.Fatal("kernel image has a library section")
	}
}

func TestNameByUsage(t *testing.T) {
	d, at := analysis(t)
	d.conventions = map[uint32]callConvention{at["gap"]: {}}
	d.nameByUsage(at["gap"], "_HelperRoutine@12")
	fn := mustFunction(t, d, at["gap"])
	if fn.Name != "HelperRoutine" || fn.Symbol == nil || fn.Symbol.Lib != "usage" || fn.Symbol.VA != at["gap"] || fn.Symbol.ArgBytes != 12 {
		t.Fatalf("named %q %+v", fn.Name, fn.Symbol)
	}
	if _, kept := d.conventions[at["gap"]]; kept || d.librarySections != nil {
		t.Fatal("stale caches kept")
	}
	d.nameByUsage(at["gap"], "_Other")
	if fn.Name != "HelperRoutine" {
		t.Fatal("named function renamed")
	}
	d.nameByUsage(0x15000, "_Nowhere")
}

func TestDescribe(t *testing.T) {
	d, at := analysis(t)
	args := at["args"]
	if got, want := d.Describe(args), fmt.Sprintf("%08X : 7 instructions through %08X, cleanup 8 bytes, returns true, rets map[RET 0x8:1]", args, args+12); got != want {
		t.Fatalf("args\n%s\n%s", got, want)
	}
	cookie := at["cookieCheck"]
	if got, want := d.Describe(cookie), fmt.Sprintf("%08X _security_check_cookie: 4 instructions through %08X, cleanup 0 bytes, returns true, rets map[RET:1]", cookie, cookie+9); got != want {
		t.Fatalf("cookie\n%s\n%s", got, want)
	}
	spin := at["spin"]
	if got, want := d.Describe(spin), fmt.Sprintf("%08X : 1 instructions through %08X, cleanup 0 bytes, returns false, rets map[]", spin, spin); got != want {
		t.Fatalf("spin\n%s\n%s", got, want)
	}
	thunk := at["thunk"]
	if got, want := d.Describe(thunk), fmt.Sprintf("%08X : 1 instructions through %08X, cleanup 0 bytes, returns true, rets map[]", thunk, thunk); got != want {
		t.Fatalf("thunk\n%s\n%s", got, want)
	}
	if got := d.Describe(0x1FFFF); got != "0001FFFF is no routine" {
		t.Fatalf("missing %q", got)
	}
}

func TestFormatInsn(t *testing.T) {
	d, at := analysis(t)
	entry := at["entry"]
	cases := map[uint32]string{
		entry:               fmt.Sprintf("%08X  push ebp", entry),
		entry + 1:           fmt.Sprintf("%08X  mov ebp, esp", entry+1),
		entry + 3:           fmt.Sprintf("%08X  push 0x11", entry+3),
		entry + 5:           fmt.Sprintf("%08X  call 0x%x", entry+5, at["args"]),
		at["importCall"]:    fmt.Sprintf("%08X  call dword ptr [KeSetEvent]", at["importCall"]),
		at["cookieCall"]:    fmt.Sprintf("%08X  call _security_check_cookie", at["cookieCall"]),
		at["switchJump"]:    fmt.Sprintf("%08X  jmp dword ptr [eax*4+0x%x]", at["switchJump"], at["table"]),
		at["probeDone"]:     fmt.Sprintf("%08X  xchg esp, eax", at["probeDone"]),
		at["probeDone"] + 1: fmt.Sprintf("%08X  ret", at["probeDone"]+1),
	}
	for va, want := range cases {
		if got := d.FormatInsn(d.InsnByVA[va]); got != want {
			t.Errorf("%08X formats as %q, want %q", va, got, want)
		}
	}
}

func TestSymbolLookup(t *testing.T) {
	d, at := analysis(t)
	if name, base := d.symbolLookup(0x12000); name != "KeSetEvent" || base != 0x12000 {
		t.Errorf("import lookup %q %X", name, base)
	}
	if name, base := d.symbolLookup(uint64(at["cookieCheck"])); name != "_security_check_cookie" || base != uint64(at["cookieCheck"]) {
		t.Errorf("function lookup %q %X", name, base)
	}
	if name, base := d.symbolLookup(uint64(at["args"])); name != "" || base != 0 {
		t.Errorf("unnamed lookup %q %X", name, base)
	}
	if name, base := d.symbolLookup(0x15000); name != "" || base != 0 {
		t.Errorf("outside lookup %q %X", name, base)
	}
}

func TestResolveTarget(t *testing.T) {
	call := decodeAt(t, 0x11000, 0xE8, 0x10, 0x00, 0x00, 0x00)
	if resolveTarget(&call) != 0x11015 {
		t.Fatalf("relative %08X", resolveTarget(&call))
	}
	back := decodeAt(t, 0x11000, 0xEB, 0xFE)
	if resolveTarget(&back) != 0x11000 {
		t.Fatalf("backward %08X", resolveTarget(&back))
	}
	indirect := decodeAt(t, 0x11000, 0xFF, 0x15, 0x00, 0x20, 0x01, 0x00)
	if resolveTarget(&indirect) != 0 {
		t.Fatal("memory call resolved")
	}
	none := Instruction{Inst: x86asm.Inst{Op: x86asm.RET}}
	if resolveTarget(&none) != 0 {
		t.Fatal("return resolved")
	}
	imm := Instruction{Inst: x86asm.Inst{Op: x86asm.CALL, Args: x86asm.Args{x86asm.Imm(0x11234)}}}
	if resolveTarget(&imm) != 0x11234 {
		t.Fatal("immediate not resolved")
	}
}

func TestKernelExportName(t *testing.T) {
	if len(kernelExports) != 379 {
		t.Fatalf("table has %d entries", len(kernelExports))
	}
	named := map[uint32]string{1: "AvGetSavedDataAddress", 8: "DbgPrint", 14: "ExAllocatePool", 104: "KeGetCurrentThread", 109: "KeInitializeInterrupt", 145: "KeSetEvent", 156: "KeTickCount", 187: "NtClose", 320: "RtlZeroMemory", 366: "HalWriteSMCScratchRegister", 374: "MmDbgAllocateMemory", 378: "MmDbgWriteCheck"}
	for ordinal, want := range named {
		if got := KernelExportName(ordinal); got != want {
			t.Errorf("ordinal %d is %q, want %q", ordinal, got, want)
		}
	}
	for _, ordinal := range []uint32{0, 367, 370, 373, 379, 1000, 0x7FFFFFFF} {
		if got := KernelExportName(ordinal); got != "" {
			t.Errorf("ordinal %d is %q, want none", ordinal, got)
		}
	}
	seen := map[string]bool{}
	for _, name := range kernelExports {
		if name == "" {
			continue
		}
		if seen[name] {
			t.Errorf("%s exported twice", name)
		}
		seen[name] = true
	}
}

func TestIsBoundaryOp(t *testing.T) {
	for _, op := range []x86asm.Op{x86asm.RET, x86asm.JMP, x86asm.INT, x86asm.NOP, x86asm.HLT} {
		if !isBoundaryOp(op) {
			t.Errorf("%v not a boundary", op)
		}
	}
	for _, op := range []x86asm.Op{x86asm.CALL, x86asm.MOV, x86asm.JE, x86asm.PUSH} {
		if isBoundaryOp(op) {
			t.Errorf("%v a boundary", op)
		}
	}
}

func TestPointerTargets(t *testing.T) {
	d, at := analysis(t)
	targets := map[uint32]bool{}
	for _, va := range d.pointerTargets() {
		targets[va] = true
	}
	for _, name := range []string{"case0", "case1", "case2", "aligned", "unaligned", "tail0", "tail1", "sAligned", "sUnaligned", "sTail0", "sTail1", "entry"} {
		if !targets[at[name]] {
			t.Errorf("%s at %08X not collected", name, at[name])
		}
	}
	for _, name := range []string{"args", "switcher", "memcpy", "gap", "libFn"} {
		if targets[at[name]] {
			t.Errorf("%s at %08X collected without a stored pointer", name, at[name])
		}
	}
	for va := range targets {
		sec := d.sectionOf(va)
		if sec == nil || sec.Flags&flagExecutable == 0 || d.InsnByVA[va] == nil {
			t.Errorf("%08X collected outside the code", va)
		}
	}
}

func TestFunctionVAsSorted(t *testing.T) {
	d, _ := analysis(t)
	vas := d.functionVAs()
	if len(vas) != len(d.Functions) {
		t.Fatalf("%d of %d", len(vas), len(d.Functions))
	}
	for i := 1; i < len(vas); i++ {
		if vas[i] <= vas[i-1] {
			t.Fatalf("unsorted at %d", i)
		}
	}
	if vas[0] != 0x11000 {
		t.Fatalf("first %08X", vas[0])
	}
}

func TestDisassembleEmptySections(t *testing.T) {
	spec := sampleSpec()
	spec.sections = append(spec.sections, testSection{name: ".bss", va: 0x15000, flags: flagWritable, vsize: 0x100})
	d := disassembleImage(t, parseXBE(t, spec))
	for va := range d.InsnByVA {
		if va >= 0x15000 && va < 0x15100 {
			t.Fatalf("instruction decoded in the empty section at %08X", va)
		}
	}
	if len(d.Functions) != 1 || d.Functions[0x11000] == nil || !strings.HasPrefix(d.Describe(0x11000), "00011000 : 4 instructions") {
		t.Fatalf("functions %v", d.functionVAs())
	}
}
