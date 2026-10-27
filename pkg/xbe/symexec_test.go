package xbe

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"golang.org/x/arch/x86/x86asm"
)

func execText() []chunk {
	return []chunk{
		lbl("leaf"), movEAXImm(42), ret(), pad(),
		lbl("espArgs"), by(0x8B, 0x44, 0x24, 0x04), by(0x03, 0x44, 0x24, 0x08), ret(), pad(),
		lbl("ebpArgs"), by(0x55), by(0x8B, 0xEC), by(0x8B, 0x45, 0x08), by(0x2B, 0x45, 0x0C), by(0x5D), ret(), pad(),
		lbl("getter"), by(0x8B, 0x41, 0x04), ret(), pad(),
		lbl("stdCaller"), by(0x6A, 0x01), by(0x6A, 0x02), call("std8"), ret(), pad(),
		lbl("std8"), by(0x8B, 0x44, 0x24, 0x04), by(0x03, 0x44, 0x24, 0x08), retn(8), pad(),
		lbl("fptrCaller"), by(0x6A, 0x01), by(0x6A, 0x02), abs32([]byte{0xFF, 0x15}, "fptr", 0), by(0x83, 0xC4, 0x08), ret(), pad(),
		lbl("setFptr"), abs32([]byte{0xA3}, "fptr", 0), ret(), pad(),
		lbl("branch"), by(0x83, 0x7C, 0x24, 0x04, 0x00), je8("branchZero"), movEAXImm(1), ret(), lbl("branchZero"), movEAXImm(2), ret(), pad(),
		lbl("loop"), by(0x8B, 0x4C, 0x24, 0x04), by(0x33, 0xC0), lbl("loopTop"), by(0x40), by(0x49), jne8("loopTop"), ret(), pad(),
		lbl("useResult"), call("helper"), by(0x8B, 0x40, 0x08), ret(), pad(),
		lbl("helper"), movEAXImm(7), ret(), pad(),
		lbl("vcall"), by(0x8B, 0x01), by(0x6A, 0x03), by(0xFF, 0x50, 0x08), ret(), pad(),
		lbl("useImport"), call("thunkFn"), ret(), pad(),
		lbl("thunkFn"), abs32([]byte{0xFF, 0x25}, "thunk.KeGetCurrentThread", 0), pad(),
		lbl("loadImport"), abs32([]byte{0xA1}, "thunk.KeGetCurrentThread", 0), ret(), pad(),
		lbl("tailImport"), abs32([]byte{0xFF, 0x25}, "thunk.KeStallExecutionProcessor", 0), pad(),
		lbl("storeGlobal"), abs32([]byte{0xA3}, "gvar", 0), abs32([]byte{0xA1}, "gvar", 0), ret(), pad(),
		lbl("objGlobal"), abs32([]byte{0xA1}, "gobj", 0), by(0x8B, 0x40, 0x04), ret(), pad(),
		lbl("localFrame"), by(0x55), by(0x8B, 0xEC), lbl("localPush"), by(0x51), by(0xC7, 0x45, 0xFC, 7, 0, 0, 0), by(0x8B, 0x01), by(0xFF, 0x50, 0x04), by(0x8B, 0xE5), by(0x5D), ret(), pad(),
		lbl("argPush"), by(0x6A, 0x09), by(0x8B, 0x01), by(0xFF, 0x50, 0x04), ret(), pad(),
		lbl("argArea"), by(0x56), by(0x5E), lbl("argAreaSub"), by(0x83, 0xEC, 0x08), by(0xC7, 0x04, 0x24, 1, 0, 0, 0), by(0xC7, 0x44, 0x24, 0x04, 2, 0, 0, 0), call("std8"), ret(), pad(),
		lbl("frameSub"), by(0x55), by(0x8B, 0xEC), lbl("frameSubInsn"), by(0x83, 0xEC, 0x08), by(0xC7, 0x45, 0xFC, 3, 0, 0, 0), by(0x8B, 0x45, 0xFC), by(0x8B, 0xE5), by(0x5D), ret(), pad(),
		lbl("probeCaller"), by(0x55), by(0x8B, 0xEC), movEAXImm(0x2000), call("probe"), by(0x8D, 0x04, 0x24), by(0x8B, 0xE5), by(0x5D), ret(), pad(),
		lbl("probe"), by(0x85, 0xC0), by(0x3D, 0x00, 0x10, 0x00, 0x00), by(0x87, 0xC4), ret(), pad(),
		lbl("sehCaller"), abs32([]byte{0x68}, "handler", 0), by(0x64, 0xFF, 0x35, 0, 0, 0, 0), by(0x64, 0x89, 0x25, 0, 0, 0, 0), by(0x6A, 0x01), by(0x8B, 0x01), by(0xFF, 0x50, 0x04), by(0x64, 0x8F, 0x05, 0, 0, 0, 0), by(0x83, 0xC4, 0x04), ret(), pad(),
		lbl("handler"), by(0x33, 0xC0), ret(), pad(),
		lbl("xadd"), movEAXImm(1), by(0xF0, 0x0F, 0xC1, 0x01), ret(), pad(),
		lbl("cmpxchg"), by(0x33, 0xC0), movEDXImm(1), by(0xF0, 0x0F, 0xB1, 0x11), ret(), pad(),
		lbl("switchFn"), by(0x8B, 0x44, 0x24, 0x04), abs32([]byte{0xFF, 0x24, 0x85}, "table", 0), lbl("case0"), movEAXImm(10), ret(), lbl("case1"), movEAXImm(20), ret(), lbl("case2"), movEAXImm(30), ret(), pad(),
		lbl("nullDeref"), by(0x8B, 0x44, 0x24, 0x04), by(0x85, 0xC0), je8("isNull"), by(0x8B, 0x00), ret(), lbl("isNull"), by(0x8B, 0x44, 0x24, 0x04), by(0x8B, 0x40, 0x04), ret(), pad(),
		lbl("nullLoad"), by(0xA1, 0x10, 0, 0, 0), ret(), pad(),
		lbl("outsideLoad"), by(0xA1, 0x00, 0x00, 0x03, 0x00), ret(), pad(),
		lbl("fmulArgs"), by(0xD9, 0x44, 0x24, 0x04), by(0xD8, 0x4C, 0x24, 0x08), abs32([]byte{0xD9, 0x1D}, "gout", 0), ret(), pad(),
		lbl("fpuTerms"), by(0xD8, 0xC1), abs32([]byte{0xD9, 0x1D}, "gout", 0), ret(), pad(),
		lbl("ftolCaller"), by(0xD9, 0x44, 0x24, 0x04), call("ftol"), ret(), pad(),
		lbl("ftol"), by(0x83, 0xEC, 0x08), by(0xDF, 0x3C, 0x24), by(0x8B, 0x04, 0x24), by(0x8B, 0x54, 0x24, 0x04), by(0x83, 0xC4, 0x08), ret(), pad(),
		lbl("fistGlobal"), by(0xD9, 0xE8), abs32([]byte{0xDB, 0x15}, "gout", 0), ret(), pad(),
		lbl("copySym"), by(0x8B, 0x4C, 0x24, 0x04), by(0xF3, 0xA5), ret(), pad(),
		lbl("fillSym"), by(0xF3, 0xAB), ret(), pad(),
		lbl("fillLong"), movECXImm(0x10001), by(0x33, 0xC0), by(0xF3, 0xAB), ret(), pad(),
		lbl("fillShort"), movECXImm(3), movEAXImm(7), by(0xF3, 0xAB), ret(), pad(),
		lbl("copyShort"), movECXImm(1), by(0xF3, 0xA5), ret(), pad(),
		lbl("spin"), jmp8("spin"), pad(),
		lbl("fourPaths"), by(0x8B, 0x44, 0x24, 0x04), by(0x85, 0xC0), je8("fourA"), lbl("fourA"), by(0x8B, 0x44, 0x24, 0x08), by(0x85, 0xC0), je8("fourB"), lbl("fourB"), ret(), pad(),
		lbl("retryCaller"), call("longHelper"), ret(), pad(),
		lbl("longHelper"), rep(10, 0x90), ret(), pad(),
	}
}

func execRData() []chunk {
	return []chunk{
		lbl("table"), wordAt("case0", 0), wordAt("case1", 0), wordAt("case2", 0), word(0),
		lbl("vt"), wordAt("leaf", 0), wordAt("helper", 0),
	}
}

func execData() []chunk {
	return []chunk{
		lbl("fptr"), word(0),
		lbl("gvar"), word(0),
		lbl("gobj"), word(0),
		lbl("gout"), word(0),
	}
}

var execImports = []string{"KeGetCurrentThread", "KeStallExecutionProcessor"}

func execImage(t *testing.T) *testImage {
	t.Helper()
	return buildImage(t, execText(), execRData(), execData(), execImports)
}

func withThis(s *execState) { s.regs[1] = Ptr{"this", 0} }

func withArgs(n int) func(*execState) {
	return func(s *execState) {
		args := make([]Expr, n)
		for i := range args {
			args[i] = Sym{"arg" + string(rune('0'+i))}
		}
		s.pushArgs(args...)
	}
}

func expectStore(t *testing.T, e Effect, base string, off int32, size int, value Expr) {
	t.Helper()
	if e.Kind != "store" || e.Base != base || e.Off != off || e.Size != size || e.Addr != nil {
		t.Fatalf("effect %s is not a %d byte store to %s+%#x", effectString(e, ExprString), size, base, off)
	}
	exprEq(t, "stored value", e.Value, value)
}

func TestRunLeafReturnsConstant(t *testing.T) {
	ti := execImage(t)
	p := onePath(t, runPaths(t, ti, "leaf", noInit))
	if len(p.Effects) != 0 || len(p.Guards) != 0 {
		t.Fatalf("leaf path has %d effects and %d guards", len(p.Effects), len(p.Guards))
	}
	exprEq(t, "return", p.Return, IConst{42})
}

func TestRunStackArgumentsNamed(t *testing.T) {
	ti := execImage(t)
	for label, want := range map[string]Expr{
		"espArgs": Bin{"add", Sym{"arg0"}, Sym{"arg1"}},
		"ebpArgs": Bin{"sub", Sym{"arg0"}, Sym{"arg1"}},
	} {
		ex := NewExecutor(ti.d)
		va := ti.at(t, label)
		paths, err := ex.Run(va, func(s *execState) { ex.seedArgs(s, va, "arg", 2) })
		if err != nil {
			t.Fatalf("%s: %v", label, err)
		}
		p := onePath(t, paths)
		exprEq(t, label+" return", p.Return, want)
		if len(p.Effects) != 0 {
			t.Errorf("%s folds its stack but recorded %d effects", label, len(p.Effects))
		}
	}
}

func TestRunThiscallReadsReceiver(t *testing.T) {
	ti := execImage(t)
	if regs := ti.d.liveInRegisters(ti.d.Functions[ti.at(t, "getter")]); len(regs) != 1 || regs[0].String() != "ECX" {
		t.Fatalf("getter reads %v on entry, want ECX", regs)
	}
	if inputs := ti.d.registerInputs(ti.at(t, "getter")); len(inputs) != 0 {
		t.Fatalf("the receiver register counts as an input %v", inputs)
	}
	p := onePath(t, runPaths(t, ti, "getter", withThis))
	exprEq(t, "return", p.Return, Load{Base: "this", Off: 4, Size: 4})
	p = onePath(t, runPaths(t, ti, "getter", noInit))
	exprEq(t, "unseeded return", p.Return, Load{Addr: Bin{"add", Sym{"reg.ECX"}, IConst{4}}, Size: 4})
}

func TestRunStdcallCleanup(t *testing.T) {
	ti := execImage(t)
	p := onePath(t, runPaths(t, ti, "stdCaller", noInit))
	exprEq(t, "inlined return", p.Return, IConst{3})
	if len(p.Effects) != 0 {
		t.Fatalf("inlined call recorded %d effects", len(p.Effects))
	}
	ex := NewExecutor(ti.d)
	ex.Opaque[ti.at(t, "std8")] = true
	paths, err := ex.Run(ti.at(t, "stdCaller"), noInit)
	if err != nil {
		t.Fatalf("opaque: %v", err)
	}
	p = onePath(t, paths)
	calls := callsNamed(p.Effects, subName(ti.at(t, "std8")))
	if len(calls) != 1 || len(calls[0].Args) != 2 {
		t.Fatalf("opaque call effects %v", p.Effects)
	}
	exprEq(t, "first argument", calls[0].Args[0], IConst{2})
	exprEq(t, "second argument", calls[0].Args[1], IConst{1})
	exprEq(t, "return", p.Return, CallResult{Fn: subName(ti.at(t, "std8")), Seq: 1})
}

// Checks the add after a pointer call fixes its arity and the pointer leads the arguments
func TestRunCallerCleanupFixesArity(t *testing.T) {
	ti := execImage(t)
	fptr := ti.at(t, "fptr")
	if clean := NewExecutor(ti.d).cleanupAfter(ti.d.Functions[ti.at(t, "fptrCaller")], ti.at(t, "fptrCaller")+4); clean != 8 {
		t.Fatalf("cleanup after the call is %d bytes, want 8", clean)
	}
	p := onePath(t, runPaths(t, ti, "fptrCaller", noInit))
	calls := callsNamed(p.Effects, pointerCallName)
	if len(calls) != 1 || len(calls[0].Args) != 3 {
		t.Fatalf("pointer call effects %v", p.Effects)
	}
	exprEq(t, "pointer", calls[0].Args[0], Load{Base: "", Off: int32(fptr), Size: 4})
	exprEq(t, "first argument", calls[0].Args[1], IConst{2})
	exprEq(t, "second argument", calls[0].Args[2], IConst{1})
	exprEq(t, "receiver", calls[0].This, Sym{"reg.ECX"})
}

func TestRunBranchForksPaths(t *testing.T) {
	ti := execImage(t)
	paths := runPaths(t, ti, "branch", withArgs(1))
	if len(paths) != 2 {
		t.Fatalf("%d paths, want 2", len(paths))
	}
	exprEq(t, "fall guard", paths[0].Guards[0], Cond{"ne", Sym{"arg0"}, IConst{0}})
	exprEq(t, "fall return", paths[0].Return, IConst{1})
	exprEq(t, "taken guard", paths[1].Guards[0], Cond{"eq", Sym{"arg0"}, IConst{0}})
	exprEq(t, "taken return", paths[1].Return, IConst{2})
	for _, p := range paths {
		if len(p.Guards) != 1 || !reflect.DeepEqual(p.GuardPos, []int{0}) {
			t.Errorf("guards %v at %v", p.Guards, p.GuardPos)
		}
	}
}

func decremented(n int) Expr {
	var e Expr = Sym{"arg0"}
	for i := 0; i < n; i++ {
		e = Bin{"sub", e, IConst{1}}
	}
	return e
}

// Checks a symbolic loop forks one exit per iteration and leaves after the unroll budget
func TestRunLoopUnrollBudget(t *testing.T) {
	ti := execImage(t)
	ex := NewExecutor(ti.d)
	paths, err := ex.Run(ti.at(t, "loop"), withArgs(1))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != maxLoopUnroll+1 {
		t.Fatalf("%d paths, want %d", len(paths), maxLoopUnroll+1)
	}
	if !ex.loopCut {
		t.Errorf("the loop was not cut")
	}
	for k, p := range paths {
		iterations := k + 1
		exprEq(t, "return", p.Return, IConst{uint32(iterations)})
		if len(p.Guards) != iterations {
			t.Fatalf("path %d has %d guards", k, len(p.Guards))
		}
		for i := 0; i < iterations-1; i++ {
			exprEq(t, "continue guard", p.Guards[i], Cond{"ne", decremented(i + 1), IConst{0}})
		}
		exprEq(t, "exit guard", p.Guards[iterations-1], Cond{"eq", decremented(iterations), IConst{0}})
	}
}

func TestRunCallResultBecomesObjectBase(t *testing.T) {
	ti := execImage(t)
	ex := NewExecutor(ti.d)
	helper := ti.at(t, "helper")
	ex.Opaque[helper] = true
	paths, err := ex.Run(ti.at(t, "useResult"), noInit)
	if err != nil {
		t.Fatal(err)
	}
	p := onePath(t, paths)
	calls := callsNamed(p.Effects, subName(helper))
	if len(calls) != 1 || calls[0].Seq != 1 || len(calls[0].Args) != 0 {
		t.Fatalf("call effects %v", p.Effects)
	}
	exprEq(t, "receiver", calls[0].This, Sym{"reg.ECX"})
	exprEq(t, "return", p.Return, Load{Base: "res1", Off: 8, Size: 4})
}

func TestRunVirtualCallNamesSlot(t *testing.T) {
	ti := execImage(t)
	fn := ti.d.Functions[ti.at(t, "vcall")]
	if vtable, known := ti.d.callReadsVtable(fn, ti.at(t, "vcall")+4, x86asm.EAX); !vtable || !known {
		t.Fatalf("vtable read %v known %v", vtable, known)
	}
	p := onePath(t, runPaths(t, ti, "vcall", withThis))
	calls := callsNamed(p.Effects, "vcall@8")
	if len(calls) != 1 || len(calls[0].Args) != 2 {
		t.Fatalf("call effects %v", p.Effects)
	}
	exprEq(t, "object", calls[0].Args[0], Ptr{"this", 0})
	exprEq(t, "pushed argument", calls[0].Args[1], IConst{3})
	exprEq(t, "return", p.Return, CallResult{Fn: "vcall@8", Seq: 1})
}

func TestRunImportThunk(t *testing.T) {
	ti := execImage(t)
	if name, ok := ti.d.importThunk(ti.d.Functions[ti.at(t, "thunkFn")]); !ok || name != "KeGetCurrentThread" {
		t.Fatalf("thunk names %q %v", name, ok)
	}
	p := onePath(t, runPaths(t, ti, "useImport", noInit))
	calls := callsNamed(p.Effects, "KeGetCurrentThread")
	if len(calls) != 1 || len(calls[0].Args) != 0 {
		t.Fatalf("call effects %v", p.Effects)
	}
	exprEq(t, "return", p.Return, CallResult{Fn: "KeGetCurrentThread", Seq: 1})
	p = onePath(t, runPaths(t, ti, "loadImport", noInit))
	exprEq(t, "loaded pointer", p.Return, Ptr{"import:KeGetCurrentThread", 0})
	p = onePath(t, runPaths(t, ti, "tailImport", withArgs(1)))
	if calls := callsNamed(p.Effects, "KeStallExecutionProcessor"); len(calls) != 1 {
		t.Fatalf("tail call effects %v", p.Effects)
	}
	exprEq(t, "tail return", p.Return, CallResult{Fn: "KeStallExecutionProcessor", Seq: 1})
}

func TestRunGlobalStoreAndObject(t *testing.T) {
	ti := execImage(t)
	gvar := ti.at(t, "gvar")
	p := onePath(t, runPaths(t, ti, "storeGlobal", noInit))
	if len(p.Effects) != 1 {
		t.Fatalf("effects %v", p.Effects)
	}
	expectStore(t, p.Effects[0], "", int32(gvar), 4, Sym{"reg.EAX"})
	exprEq(t, "read back", p.Return, Sym{"reg.EAX"})
	if got := effectString(p.Effects[0], ExprString); got != ".4@0x21010 = reg.EAX" {
		t.Errorf("effect renders as %q", got)
	}
	ex := NewExecutor(ti.d)
	gobj := ti.at(t, "gobj")
	ex.vtables[gobj] = ti.at(t, "vt")
	paths, err := ex.Run(ti.at(t, "objGlobal"), noInit)
	if err != nil {
		t.Fatal(err)
	}
	exprEq(t, "object field", onePath(t, paths).Return, Load{Base: "g00021014", Off: 4, Size: 4})
}

// Checks a push of ecx under a frame pointer reserves a local instead of passing an argument
func TestRunPushECXLocal(t *testing.T) {
	ti := execImage(t)
	ex := NewExecutor(ti.d)
	if !ex.savePush[ti.at(t, "localPush")] {
		t.Fatalf("the push of ecx is not a local")
	}
	if ex.savePush[ti.at(t, "argPush")] {
		t.Fatalf("the push of an immediate is a save")
	}
	paths, err := ex.Run(ti.at(t, "localFrame"), withThis)
	if err != nil {
		t.Fatal(err)
	}
	calls := callsNamed(onePath(t, paths).Effects, "vcall@4")
	if len(calls) != 1 || len(calls[0].Args) != 1 {
		t.Fatalf("framed call takes %v", calls)
	}
	exprEq(t, "object", calls[0].Args[0], Ptr{"this", 0})
	paths, err = ex.Run(ti.at(t, "argPush"), withThis)
	if err != nil {
		t.Fatal(err)
	}
	calls = callsNamed(onePath(t, paths).Effects, "vcall@4")
	if len(calls) != 1 || len(calls[0].Args) != 2 {
		t.Fatalf("pushed call takes %v", calls)
	}
	exprEq(t, "pushed argument", calls[0].Args[1], IConst{9})
}

func TestRunArgumentAreaVersusFrame(t *testing.T) {
	ti := execImage(t)
	ex := NewExecutor(ti.d)
	if ex.frameAlloc[ti.at(t, "argAreaSub")] {
		t.Errorf("an area the next call consumes counts as a frame")
	}
	if !ex.frameAlloc[ti.at(t, "frameSubInsn")] || !ex.frameSizes[ti.at(t, "frameSub")][8] {
		t.Errorf("the prologue reservation is not a frame of 8 bytes")
	}
	std8 := ti.at(t, "std8")
	ex.Opaque[std8] = true
	paths, err := ex.Run(ti.at(t, "argArea"), noInit)
	if err != nil {
		t.Fatal(err)
	}
	calls := callsNamed(onePath(t, paths).Effects, subName(std8))
	if len(calls) != 1 || len(calls[0].Args) != 2 {
		t.Fatalf("call effects %v", calls)
	}
	exprEq(t, "first stored argument", calls[0].Args[0], IConst{1})
	exprEq(t, "second stored argument", calls[0].Args[1], IConst{2})
	p := onePath(t, runPaths(t, ti, "frameSub", noInit))
	exprEq(t, "local read back", p.Return, IConst{3})
	if len(p.Effects) != 0 {
		t.Errorf("frame locals recorded %v", p.Effects)
	}
}

func TestRunStackProbeReservesFrame(t *testing.T) {
	ti := execImage(t)
	probe := ti.d.Functions[ti.at(t, "probe")]
	if !isStackProbe(probe) || probe.Symbol == nil || probe.Symbol.Name != "chkstk" {
		t.Fatalf("probe not recognized: %+v", probe.Symbol)
	}
	p := onePath(t, runPaths(t, ti, "probeCaller", noInit))
	exprEq(t, "stack pointer after the probe", p.Return, Ptr{"stack", 0x10000 - 8 - 0x2000})
}

func TestRunExceptionRegistrationResetsPushes(t *testing.T) {
	ti := execImage(t)
	p := onePath(t, runPaths(t, ti, "sehCaller", withThis))
	calls := callsNamed(p.Effects, "vcall@4")
	if len(calls) != 1 || len(calls[0].Args) != 2 {
		t.Fatalf("call effects %v", p.Effects)
	}
	exprEq(t, "argument after the registration", calls[0].Args[1], IConst{1})
	for _, e := range p.Effects {
		if e.Kind == "store" && threadLocalBase(e.Base) {
			t.Errorf("segment memory store recorded %s", effectString(e, ExprString))
		}
	}
	if len(effectsOf(p, "store")) != 0 {
		t.Errorf("stores recorded %v", effectsOf(p, "store"))
	}
}

func TestRunXaddAndCmpxchg(t *testing.T) {
	ti := execImage(t)
	p := onePath(t, runPaths(t, ti, "xadd", withThis))
	old := Load{Base: "this", Off: 0, Size: 4}
	exprEq(t, "xadd return", p.Return, old)
	stores := effectsOf(p, "store")
	if len(stores) != 1 {
		t.Fatalf("xadd stores %v", stores)
	}
	expectStore(t, stores[0], "this", 0, 4, Bin{"add", old, IConst{1}})
	p = onePath(t, runPaths(t, ti, "cmpxchg", withThis))
	same := Cond{"eq", IConst{0}, old}
	exprEq(t, "cmpxchg return", p.Return, Sel{same, IConst{0}, old})
	stores = effectsOf(p, "store")
	if len(stores) != 1 {
		t.Fatalf("cmpxchg stores %v", stores)
	}
	expectStore(t, stores[0], "this", 0, 4, Sel{same, IConst{1}, old})
}

func TestRunSwitchForksPerCase(t *testing.T) {
	ti := execImage(t)
	fn := ti.d.Functions[ti.at(t, "switchFn")]
	if targets := fn.Tables[ti.at(t, "switchFn")+4]; !reflect.DeepEqual(targets, []uint32{ti.at(t, "case0"), ti.at(t, "case1"), ti.at(t, "case2")}) {
		t.Fatalf("table targets %X", targets)
	}
	paths := runPaths(t, ti, "switchFn", withArgs(1))
	if len(paths) != 3 {
		t.Fatalf("%d paths, want 3", len(paths))
	}
	index := Sym{"arg0"}
	exprEq(t, "case 0 return", paths[0].Return, IConst{10})
	exprEq(t, "case 0 guard", paths[0].Guards[0], Cond{"eq", index, IConst{0}})
	exprEq(t, "case 1 return", paths[1].Return, IConst{20})
	exprEq(t, "case 1 guard", paths[1].Guards[1], Cond{"eq", index, IConst{1}})
	exprEq(t, "case 2 return", paths[2].Return, IConst{30})
	want := []Expr{Cond{"ne", index, IConst{0}}, Cond{"ne", index, IConst{1}}, Cond{"eq", index, IConst{2}}}
	for i, g := range want {
		exprEq(t, "last case guard", paths[2].Guards[i], g)
	}
}

func TestRunNullDereferenceDropsPath(t *testing.T) {
	ti := execImage(t)
	ex := NewExecutor(ti.d)
	paths, err := ex.Run(ti.at(t, "nullDeref"), withArgs(1))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 1 || ex.dropped != 1 {
		t.Fatalf("%d paths with %d dropped", len(paths), ex.dropped)
	}
	exprEq(t, "guard", paths[0].Guards[0], Cond{"ne", Sym{"arg0"}, IConst{0}})
	exprEq(t, "return", paths[0].Return, Load{Addr: Sym{"arg0"}, Size: 4})
	paths, err = ex.Run(ti.at(t, "nullLoad"), noInit)
	if err != nil || len(paths) != 0 || ex.dropped != 1 {
		t.Fatalf("null load gave %d paths, %d dropped, %v", len(paths), ex.dropped, err)
	}
	_, err = ex.Run(ti.at(t, "outsideLoad"), noInit)
	if err == nil || !strings.Contains(err.Error(), "reads 00030000, which the image loads no memory into") {
		t.Fatalf("load outside the image: %v", err)
	}
}

func TestRunX87Terms(t *testing.T) {
	ti := execImage(t)
	gout := int32(ti.at(t, "gout"))
	p := onePath(t, runPaths(t, ti, "fmulArgs", withArgs(2)))
	stores := effectsOf(p, "store")
	if len(stores) != 1 {
		t.Fatalf("stores %v", stores)
	}
	expectStore(t, stores[0], "", gout, 4, Un{"f32", Bin{"fmul", Un{"bits2f", Sym{"arg0"}}, Un{"bits2f", Sym{"arg1"}}}})
	p = onePath(t, runPaths(t, ti, "fpuTerms", noInit))
	stores = effectsOf(p, "store")
	if len(stores) != 1 {
		t.Fatalf("stores %v", stores)
	}
	expectStore(t, stores[0], "", gout, 4, Un{"f32", Bin{"fadd", Sym{"fpu.st0"}, Sym{"fpu.st1"}}})
	p = onePath(t, runPaths(t, ti, "fistGlobal", noInit))
	stores = effectsOf(p, "store")
	if len(stores) != 1 {
		t.Fatalf("stores %v", stores)
	}
	expectStore(t, stores[0], "", gout, 4, IConst{1})
}

func TestRunFtolConvention(t *testing.T) {
	ti := execImage(t)
	ex := NewExecutor(ti.d)
	if !isFtol(ti.d.Functions[ti.at(t, "ftol")]) || !ex.ftol[ti.at(t, "ftol")] {
		t.Fatalf("ftol not recognized")
	}
	paths, err := ex.Run(ti.at(t, "ftolCaller"), withArgs(1))
	if err != nil {
		t.Fatal(err)
	}
	p := onePath(t, paths)
	exprEq(t, "return", p.Return, Un{"f2i", Un{"bits2f", Sym{"arg0"}}})
	if len(p.Effects) != 0 {
		t.Errorf("effects %v", p.Effects)
	}
}

func withBuffers(s *execState) {
	s.regs[6] = Ptr{"src", 0}
	s.regs[7] = Ptr{"dst", 0}
}

func TestRunStringCopyAndFill(t *testing.T) {
	ti := execImage(t)
	p := onePath(t, runPaths(t, ti, "copySym", func(s *execState) { withBuffers(s); withArgs(1)(s) }))
	copies := effectsOf(p, "copy")
	if len(copies) != 1 || len(p.Effects) != 1 {
		t.Fatalf("effects %v", p.Effects)
	}
	c := copies[0]
	if c.Base != "dst" || c.Off != 0 || c.Size != 4 {
		t.Errorf("copy lands at %s+%d x%d", c.Base, c.Off, c.Size)
	}
	exprEq(t, "copy destination", c.Addr, Ptr{"dst", 0})
	exprEq(t, "copy count", c.Value, Sym{"arg0"})
	exprEq(t, "copy source", c.Args[0], Ptr{"src", 0})
	exprEq(t, "copy step", c.Args[1], IConst{4})
	if got := effectString(c, ExprString); got != "copy arg0 x4 from &src to &dst" {
		t.Errorf("copy renders as %q", got)
	}
	p = onePath(t, runPaths(t, ti, "fillSym", withBuffers))
	fills := effectsOf(p, "fill")
	if len(fills) != 1 || len(p.Effects) != 1 {
		t.Fatalf("effects %v", p.Effects)
	}
	exprEq(t, "fill destination", fills[0].Addr, Ptr{"dst", 0})
	exprEq(t, "fill value", fills[0].Value, Sym{"reg.EAX"})
	exprEq(t, "fill count", fills[0].Args[0], Sym{"reg.ECX"})
	if got := effectString(fills[0], ExprString); got != "fill reg.ECX x4 with reg.EAX at &dst" {
		t.Errorf("fill renders as %q", got)
	}
	p = onePath(t, runPaths(t, ti, "fillLong", withBuffers))
	fills = effectsOf(p, "fill")
	if len(fills) != 1 || len(p.Effects) != 1 {
		t.Fatalf("long fill effects %v", p.Effects)
	}
	exprEq(t, "long fill count", fills[0].Args[0], IConst{maxRepUnroll + 1})
	exprEq(t, "long fill value", fills[0].Value, IConst{0})
	p = onePath(t, runPaths(t, ti, "fillShort", withBuffers))
	stores := effectsOf(p, "store")
	if len(stores) != 3 || len(p.Effects) != 3 {
		t.Fatalf("short fill effects %v", p.Effects)
	}
	for i, e := range stores {
		expectStore(t, e, "dst", int32(4*i), 4, IConst{7})
	}
	p = onePath(t, runPaths(t, ti, "copyShort", withBuffers))
	stores = effectsOf(p, "store")
	if len(stores) != 1 || len(p.Effects) != 1 {
		t.Fatalf("short copy effects %v", p.Effects)
	}
	expectStore(t, stores[0], "dst", 0, 4, Load{Base: "src", Off: 0, Size: 4})
}

func TestRunStepAndPathBudgets(t *testing.T) {
	ti := execImage(t)
	ex := NewExecutor(ti.d)
	ex.StepLimit = 10
	_, err := ex.Run(ti.at(t, "spin"), noInit)
	var ee *ExecError
	if !errors.As(err, &ee) || !ee.Limit || ee.VA != ti.at(t, "spin") || ee.Err.Error() != "step limit exceeded" {
		t.Fatalf("spin: %v", err)
	}
	ex = NewExecutor(ti.d)
	paths, err := ex.Run(ti.at(t, "fourPaths"), withArgs(2))
	if err != nil || len(paths) != 4 {
		t.Fatalf("four paths: %d, %v", len(paths), err)
	}
	ex = NewExecutor(ti.d)
	ex.PathLimit = 2
	_, err = ex.Run(ti.at(t, "fourPaths"), withArgs(2))
	if !errors.As(err, &ee) || !ee.Limit || ee.Err.Error() != "path limit exceeded" {
		t.Fatalf("path limit: %v", err)
	}
}

// Checks a helper exhausting its frame budget is made opaque and the run retried
func TestRunWithRetriesOpaquesCallee(t *testing.T) {
	ti := execImage(t)
	ex := NewExecutor(ti.d)
	ex.FrameLimit = 5
	helper := ti.at(t, "longHelper")
	_, err := ex.Run(ti.at(t, "retryCaller"), noInit)
	var ee *ExecError
	if !errors.As(err, &ee) || !ee.Limit || !reflect.DeepEqual(ee.Frames, []uint32{helper}) || ee.Err.Error() != "frame step budget exceeded" {
		t.Fatalf("frame budget: %v", err)
	}
	ex = NewExecutor(ti.d)
	ex.FrameLimit = 5
	paths, partial, opaqued, err := ex.RunWithRetries(ti.at(t, "retryCaller"), noInit, 2)
	if err != nil || partial != nil || !reflect.DeepEqual(opaqued, []uint32{helper}) || !ex.Opaque[helper] {
		t.Fatalf("retries: %v %v %X", err, partial, opaqued)
	}
	if calls := callsNamed(onePath(t, paths).Effects, subName(helper)); len(calls) != 1 {
		t.Fatalf("retried run effects %v", paths[0].Effects)
	}
	ex = NewExecutor(ti.d)
	ex.FrameLimit = 5
	_, _, opaqued, err = ex.RunWithRetries(ti.at(t, "retryCaller"), noInit, 0)
	if err == nil || len(opaqued) != 0 {
		t.Fatalf("without retries: %v %X", err, opaqued)
	}
}

func TestExecErrorMessage(t *testing.T) {
	e := &ExecError{VA: 0x11000, Insn: "JMP .-2", Frames: []uint32{0x11010}, Err: errors.New("step limit exceeded")}
	if got := e.Error(); got != "symexec: 00011000 JMP .-2: step limit exceeded (frames [11010])" {
		t.Errorf("message %q", got)
	}
}

func learnText() []chunk {
	return []chunk{
		lbl("staticCaller"), abs32([]byte{0xB9}, "gstatic", 0), call("learnFn"), ret(), pad(),
		lbl("learnFn"), by(0x56), by(0x8B, 0xF1), call("alloc"), abs32([]byte{0xC7, 0x00}, "vt", 0), by(0x89, 0x06), abs32([]byte{0xA3}, "glearn", 0), by(0x5E), ret(), pad(),
		lbl("alloc"), movEAXImm(1), ret(), pad(),
		lbl("ctorCaller"), call("alloc"), by(0x8B, 0xC8), call("ctor"), abs32([]byte{0xA3}, "gctor", 0), ret(), pad(),
		lbl("ctor"), abs32([]byte{0xC7, 0x01}, "vt", 0), by(0x8B, 0xC1), ret(), pad(),
		lbl("nearCaller"), call("alloc"), by(0x8B, 0xC8), call("ctor"), rep(9, 0x90), abs32([]byte{0xA3}, "gnear", 0), ret(), pad(),
		lbl("farCaller"), call("alloc"), by(0x8B, 0xC8), call("ctor"), rep(10, 0x90), abs32([]byte{0xA3}, "gfar", 0), ret(), pad(),
		lbl("vtfn"), ret(), pad(),
	}
}

func learnImage(t *testing.T) *testImage {
	t.Helper()
	return buildImage(t, learnText(), []chunk{lbl("vt"), wordAt("vtfn", 0)}, []chunk{
		lbl("gstatic"), word(0), word(0), word(0), word(0),
		lbl("glearn"), word(0),
		lbl("gctor"), word(0),
		lbl("gnear"), word(0),
		lbl("gfar"), word(0),
	}, nil)
}

// Checks a constructor result stored to a global within ten instructions types the global
func TestResolveGlobalObjectsLookback(t *testing.T) {
	ti := learnImage(t)
	vt := ti.at(t, "vt")
	if !ti.d.isVtable(vt) {
		t.Fatalf("the vtable word does not point at a routine")
	}
	info, ok := ti.d.constructorInfo()[ti.at(t, "ctor")]
	if !ok || info.vtable != vt || info.this.Reg.String() != "ECX" || info.library {
		t.Fatalf("constructor info %+v %v", info, ok)
	}
	ex := NewExecutor(ti.d)
	for label, want := range map[string]bool{"gctor": true, "gnear": true, "gfar": false, "glearn": true, "gstatic": false} {
		got, held := ex.vtables[ti.at(t, label)]
		if held != want || (held && got != vt) {
			t.Errorf("%s typed %v with %08X, want typed %v", label, held, got, want)
		}
	}
}

func TestLearnGlobalObjectsHarvestsStores(t *testing.T) {
	ti := learnImage(t)
	ex := NewExecutor(ti.d)
	learnFn, gstatic := ti.at(t, "learnFn"), ti.at(t, "gstatic")
	if callers := ex.staticThisCallers(); callers[learnFn] != gstatic || ex.thisLoc[learnFn].Reg.String() != "ECX" {
		t.Fatalf("static callers %v at %v", callers, ex.thisLoc)
	}
	alloc := ti.at(t, "alloc")
	ex.Allocators[alloc] = true
	ex.LearnGlobalObjects([]uint32{alloc}, nil)
	if got := ex.vtables[gstatic]; got != ti.at(t, "vt") {
		t.Fatalf("the static object is typed %08X, want %08X", got, ti.at(t, "vt"))
	}
	if ex.Opaque[alloc] {
		t.Errorf("learning made the allocator opaque in the base executor")
	}
}

func TestNewExecutorSharedKeepsKnowledge(t *testing.T) {
	ti := learnImage(t)
	ex := NewExecutor(ti.d)
	ex.Opaque[1] = true
	sub := NewExecutorShared(ex)
	if sub.d != ex.d || sub.Opaque[1] || len(sub.Results) != 0 {
		t.Fatalf("shared executor copies per run state")
	}
	sub.vtables[7] = 9
	if ex.vtables[7] != 9 {
		t.Errorf("vtables are not shared")
	}
	if reflect.ValueOf(sub.savePush).Pointer() != reflect.ValueOf(ex.savePush).Pointer() || reflect.ValueOf(sub.frameAlloc).Pointer() != reflect.ValueOf(ex.frameAlloc).Pointer() || reflect.ValueOf(sub.written).Pointer() != reflect.ValueOf(ex.written).Pointer() {
		t.Errorf("prologue knowledge is not shared")
	}
	if reflect.ValueOf(sub.Allocators).Pointer() != reflect.ValueOf(ex.Allocators).Pointer() || reflect.ValueOf(sub.staticThis).Pointer() != reflect.ValueOf(ex.staticThis).Pointer() {
		t.Errorf("object knowledge is not shared")
	}
}
