package xbe

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	"golang.org/x/arch/x86/x86asm"
)

func systemKernelText() []chunk {
	return []chunk{
		lbl("kSys"), by(0xFA), by(0x0F, 0x06), by(0x0F, 0x01, 0x38), by(0xFD), by(0xFC), by(0x6C), by(0x6E), by(0xF2, 0xAE), by(0xF3, 0xA6),
		by(0x0F, 0x01, 0x10), by(0x0F, 0x01, 0x18), by(0x0F, 0x00, 0xD8), by(0x0F, 0x22, 0xC0), by(0x0F, 0xAE, 0x00), by(0x0F, 0xAE, 0x08), by(0xDD, 0x30), by(0xDD, 0x20),
		by(0xD9, 0x38), by(0xD9, 0x28), by(0xDB, 0xE3), by(0xDB, 0xE2), by(0x0F, 0x30), by(0x0F, 0x20, 0xC0), by(0x0F, 0x32), by(0x0F, 0x31), by(0x0F, 0xA2),
		by(0x9C), by(0x9D), lbl("kSysSti"), by(0xFB), lbl("kSysRet"), ret(), pad(),
		lbl("kStr"), by(0xA4), by(0x66, 0xAB), ret(), pad(),
		lbl("kIret"), by(0xCF), pad(),
	}
}

func systemText() []chunk {
	return []chunk{
		lbl("popfFn"), by(0x9C), by(0x9D), je8("popfTaken"), movEAXImm(1), ret(), lbl("popfTaken"), movEAXImm(2), ret(), pad(),
		lbl("cmovFn"), by(0x83, 0xF8, 0x01), by(0x0F, 0x44, 0xC1), ret(), pad(),
		lbl("stcFn"), by(0xF9), by(0x83, 0xD0, 0x00), ret(), pad(),
		lbl("clcFn"), by(0xF8), by(0x83, 0xD0, 0x00), ret(), pad(),
		lbl("cmcFn"), by(0xF9), by(0xF5), by(0x83, 0xD0, 0x00), ret(), pad(),
		lbl("pushadFn"), by(0x60), by(0x8B, 0x44, 0x24, 0x0C), by(0x83, 0xC4, 0x20), ret(), pad(),
		lbl("popadFn"), by(0x60), movEAXImm(7), by(0x61), ret(), pad(),
		lbl("loopFn"), movECXImm(2), lbl("loopTop"), rel8(0xE2, "loopTop"), abs32([]byte{0x89, 0x0D}, "gsys", 0), ret(), pad(),
		lbl("btFn"), movEAXImm(5), by(0x0F, 0xBA, 0xE0, 0x02), by(0x0F, 0x92, 0xC0), ret(), pad(),
		lbl("btsFn"), movEAXImm(5), by(0x0F, 0xBA, 0xE8, 0x01), ret(), pad(),
		lbl("rclFn"), movEAXImm(0x80000001), by(0xF9), by(0xD1, 0xD0), ret(), pad(),
		lbl("cx8Fn"), by(0x0F, 0xC7, 0x0E), ret(), pad(),
		lbl("xlatFn"), by(0xD7), ret(), pad(),
		lbl("inFn"), by(0xEC), ret(), pad(),
		lbl("outFn"), by(0xEE), ret(), pad(),
		lbl("fcmovFn"), by(0x83, 0xF8, 0x01), by(0xDA, 0xC1), abs32([]byte{0xD9, 0x1D}, "gsys", 0), ret(), pad(),
		lbl("lodsFn"), by(0xAD), ret(), pad(),
		lbl("rdtscFn"), by(0x0F, 0x31), ret(), pad(),
		lbl("crFn"), by(0x0F, 0x20, 0xC0), ret(), pad(),
		lbl("sysLiftCaller"), call("sysLift"), ret(), pad(),
		lbl("sysLift"), by(0xFA), by(0xFB), by(0xFC), by(0xFD), by(0xCF), pad(),
		lbl("ljmpFn"), by(0x85, 0xC0), jne8("ljmpTarget"), abs32([]byte{0xEA}, "ljmpTarget", 0, 0x08, 0x00), lbl("ljmpTarget"), movEAXImm(9), ret(), pad(),
		lbl("ljmpOut"), abs32([]byte{0xEA}, "ljmpFn", 0, 0x08, 0x00), pad(),
		lbl("cpuidFn"), by(0x0F, 0xA2), ret(), pad(),
	}
}

func systemImage(t *testing.T) *testImage {
	t.Helper()
	return buildImage(t, systemText(), nil, []chunk{lbl("gsys"), word(0)}, nil)
}

// Checks every processor instruction the kernel uses records the call named for it
func TestSystemInstructionCallNames(t *testing.T) {
	ti := buildKernelImage(t, systemKernelText(), []chunk{lbl("ksdata"), rep(16, 0)}, map[uint32]string{104: "kSys"})
	mm := machineModel(t, ti, "kSys")
	if len(mm.Errors) != 0 {
		t.Fatalf("errors %v", mm.Errors)
	}
	blocks, _ := mm.RoutineBlocks(ti.at(t, "kSys"))
	if len(blocks) != 3 || blocks[1].va != ti.at(t, "kSysSti") || blocks[2].va != ti.at(t, "kSysRet") {
		t.Fatalf("%d blocks over the routine", len(blocks))
	}
	names := map[string]int{}
	for _, b := range blocks {
		for _, e := range b.effects {
			if e.Kind == "call" {
				names[e.Fn]++
			}
		}
	}
	want := map[string]int{
		cpuCLI: 1, cpuCLTS: 1, cpuInvlpg: 1, cpuCLD: 1, cpuSTD: 1, cpuINS: 1, cpuOUTS: 1, cpuSCAS: 1, cpuCMPS: 1,
		cpuWRMSR: 1, cpuRDMSRLo: 1, cpuRDMSRHi: 1, cpuRDTSCLo: 1, cpuRDTSCHi: 1,
		cpuCPUID + "EAX": 1, cpuCPUID + "EBX": 1, cpuCPUID + "ECX": 1, cpuCPUID + "EDX": 1,
		cpuFXSave: 1, cpuFXRstor: 1, cpuFNSave: 1, cpuFRstor: 1, cpuFRstorST: 24,
		cpuLGDT: 1, cpuLIDT: 1, cpuLTR: 1, cpuWriteCR: 1, cpuReadCR: 1,
		cpuFNSTCW: 1, cpuFLDCW: 1, cpuFNINIT: 1, cpuFNCLEX: 1, cpuPushFD: 1, cpuPopFD: 1, cpuSTI: 1,
	}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("recorded calls %v\nwant %v", names, want)
	}
	constants := []string{cpuCLI, cpuSTI, cpuPushFD, cpuPopFD, cpuIRETD, cpuINS, cpuOUTS, cpuSCAS, cpuCMPS, cpuWRMSR, cpuRDMSRLo, cpuRDMSRHi, cpuRDTSCLo, cpuRDTSCHi, cpuCPUID, cpuFXSave, cpuFXRstor, cpuFNSave, cpuFRstor, cpuFRstorST, cpuLGDT, cpuLIDT, cpuLTR, cpuWriteCR, cpuReadCR, cpuInvlpg, cpuCLD, cpuSTD, cpuCLTS, cpuFNSTCW, cpuFLDCW, cpuFNINIT, cpuFNCLEX}
	seen := map[string]bool{}
	for _, c := range constants {
		if !strings.HasPrefix(c, "cpu_") || seen[c] {
			t.Errorf("processor call %q is not a distinct cpu_ name", c)
		}
		seen[c] = true
	}
	if len(seen) != 33 {
		t.Errorf("%d processor call names, want 33", len(seen))
	}
	first := blocks[0]
	for _, e := range first.effects {
		switch e.Fn {
		case cpuWriteCR:
			exprEq(t, "control register number", e.Args[0], IConst{0})
			exprEq(t, "control register value", e.Args[1], Sym{"reg.EAX"})
		case cpuLTR:
			exprEq(t, "task register", e.Args[0], Un{"zx16", Sym{"reg.EAX"}})
		case cpuINS:
			exprEq(t, "port", e.Args[0], Un{"zx16", Sym{"reg.EDX"}})
			exprEq(t, "buffer", e.Args[1], Sym{"reg.EDI"})
			exprEq(t, "count", e.Args[2], IConst{1})
			exprEq(t, "size", e.Args[3], IConst{1})
			exprEq(t, "step", e.Args[4], IConst{1})
		case cpuSCAS:
			exprEq(t, "repne", e.Args[5], IConst{1})
		case cpuCMPS:
			exprEq(t, "repe", e.Args[5], IConst{0})
		case cpuPushFD:
			exprEq(t, "flags kind after the string compare", e.Args[0], IConst{flagKindCodes["cmp"]})
		case cpuReadCR:
			exprEq(t, "read control register number", e.Args[0], IConst{0})
		}
	}
	if len(first.fpu) != 8 {
		t.Fatalf("x87 stack holds %d values", len(first.fpu))
	}
	for i, v := range first.fpu {
		cr, ok := v.(CallResult)
		if !ok || cr.Fn != cpuFRstorST || !equalExprs(cr.Args[1], IConst{uint32(i)}) {
			t.Errorf("st%d restored as %s", i, ExprString(v))
		}
	}
	str := blockOf(t, machineModel(t, ti, "kStr"), ti.at(t, "kStr"))
	exprEq(t, "byte step source", str.regs[6], Bin{"add", Sym{"reg.ESI"}, Sym{"dfstep1"}})
	exprEq(t, "byte then word step", str.regs[7], Bin{"add", Bin{"add", Sym{"reg.EDI"}, Sym{"dfstep1"}}, Sym{"dfstep2"}})
	iret := blockOf(t, machineModel(t, ti, "kIret"), ti.at(t, "kIret"))
	if iret.term.kind != "iret" || len(iret.effects) != 0 {
		t.Errorf("interrupt return block %+v with %v", iret.term, iret.effects)
	}
}

func TestSystemFlagInstructionsOutsideMachine(t *testing.T) {
	ti := systemImage(t)
	paths := runPaths(t, ti, "popfFn", noInit)
	if len(paths) != 2 {
		t.Fatalf("%d paths after popfd", len(paths))
	}
	unbound := Sym{"unbound.popfd"}
	exprEq(t, "fall guard", paths[0].Guards[0], Cond{"ne", unbound, IConst{0}})
	exprEq(t, "taken guard", paths[1].Guards[0], Cond{"eq", unbound, IConst{0}})
	calls := callsNamed(paths[0].Effects, cpuPopFD)
	if len(callsNamed(paths[0].Effects, cpuPushFD)) != 1 || len(calls) != 1 || len(paths[0].Effects) != 2 {
		t.Fatalf("effects %v", paths[0].Effects)
	}
	exprEq(t, "popped word", calls[0].Args[0], CallResult{Fn: cpuPushFD, Seq: 1})
	p := onePath(t, runPaths(t, ti, "stcFn", noInit))
	exprEq(t, "add with carry set", p.Return, Bin{"add", Sym{"reg.EAX"}, IConst{1}})
	p = onePath(t, runPaths(t, ti, "clcFn", noInit))
	exprEq(t, "add with carry clear", p.Return, Sym{"reg.EAX"})
	p = onePath(t, runPaths(t, ti, "cmcFn", noInit))
	exprEq(t, "add with carry complemented", p.Return, Sym{"reg.EAX"})
	p = onePath(t, runPaths(t, ti, "btFn", noInit))
	exprEq(t, "bit test into a byte", p.Return, IConst{1})
	p = onePath(t, runPaths(t, ti, "btsFn", noInit))
	exprEq(t, "bit set", p.Return, IConst{7})
	p = onePath(t, runPaths(t, ti, "rclFn", noInit))
	exprEq(t, "rotate through carry", p.Return, IConst{3})
	p = onePath(t, runPaths(t, ti, "loopFn", noInit))
	stores := effectsOf(p, "store")
	if len(stores) != 1 {
		t.Fatalf("loop effects %v", p.Effects)
	}
	expectStore(t, stores[0], "", int32(ti.at(t, "gsys")), 4, IConst{0})
}

func TestSystemConditionalMoveUsesJumpCondition(t *testing.T) {
	ti := systemImage(t)
	p := onePath(t, runPaths(t, ti, "cmovFn", noInit))
	exprEq(t, "return", p.Return, Sel{Cond{"eq", Sym{"reg.EAX"}, IConst{1}}, Sym{"reg.ECX"}, Sym{"reg.EAX"}})
	want := map[x86asm.Op]x86asm.Op{
		x86asm.CMOVA: x86asm.JA, x86asm.CMOVAE: x86asm.JAE, x86asm.CMOVB: x86asm.JB, x86asm.CMOVBE: x86asm.JBE,
		x86asm.CMOVE: x86asm.JE, x86asm.CMOVNE: x86asm.JNE, x86asm.CMOVG: x86asm.JG, x86asm.CMOVGE: x86asm.JGE,
		x86asm.CMOVL: x86asm.JL, x86asm.CMOVLE: x86asm.JLE, x86asm.CMOVS: x86asm.JS, x86asm.CMOVNS: x86asm.JNS,
		x86asm.CMOVP: x86asm.JP, x86asm.CMOVNP: x86asm.JNP, x86asm.CMOVO: x86asm.JO, x86asm.CMOVNO: x86asm.JNO,
	}
	if !reflect.DeepEqual(cmovJump, want) {
		t.Errorf("conditional move table %v", cmovJump)
	}
	sets := map[x86asm.Op]string{x86asm.SETE: "JE", x86asm.SETNE: "JNE", x86asm.SETL: "JL", x86asm.SETGE: "JGE", x86asm.SETLE: "JLE", x86asm.SETG: "JG", x86asm.SETB: "JB", x86asm.SETAE: "JAE", x86asm.SETBE: "JBE", x86asm.SETA: "JA", x86asm.SETS: "JS", x86asm.SETNS: "JNS", x86asm.SETP: "JP", x86asm.SETNP: "JNP", x86asm.SETO: "JO", x86asm.SETNO: "JNO", x86asm.JE: "JE", x86asm.JNE: "JNE"}
	for op, name := range sets {
		if got := jumpName(op); got != name {
			t.Errorf("%v names %s, want %s", op, got, name)
		}
	}
	p = onePath(t, runPaths(t, ti, "fcmovFn", noInit))
	stores := effectsOf(p, "store")
	if len(stores) != 1 {
		t.Fatalf("effects %v", p.Effects)
	}
	expectStore(t, stores[0], "", int32(ti.at(t, "gsys")), 4, Un{"f32", Sel{Cond{"ult", Sym{"reg.EAX"}, IConst{1}}, Sym{"fpu.st1"}, Sym{"fpu.st0"}}})
}

func TestSystemStackAndStringInstructions(t *testing.T) {
	ti := systemImage(t)
	p := onePath(t, runPaths(t, ti, "pushadFn", noInit))
	exprEq(t, "saved stack pointer", p.Return, Ptr{"stack", 0x10000 - 4})
	p = onePath(t, runPaths(t, ti, "popadFn", noInit))
	exprEq(t, "restored accumulator", p.Return, Sym{"reg.EAX"})
	p = onePath(t, runPaths(t, ti, "cx8Fn", func(s *execState) { s.regs[6] = Ptr{"q", 0} }))
	lo, hi := Load{Base: "q", Off: 0, Size: 4}, Load{Base: "q", Off: 4, Size: 4}
	same := Bin{"and", Cond{"eq", Sym{"reg.EAX"}, lo}, Cond{"eq", Sym{"reg.EDX"}, hi}}
	stores := effectsOf(p, "store")
	if len(stores) != 2 {
		t.Fatalf("stores %v", stores)
	}
	expectStore(t, stores[0], "q", 0, 4, Sel{same, Sym{"reg.EBX"}, lo})
	expectStore(t, stores[1], "q", 4, 4, Sel{same, Sym{"reg.ECX"}, hi})
	exprEq(t, "accumulator after exchange", p.Return, Sel{same, Sym{"reg.EAX"}, lo})
	p = onePath(t, runPaths(t, ti, "xlatFn", func(s *execState) {
		s.regs[3] = Ptr{"tbl", 0}
		s.regs[0] = IConst{3}
	}))
	exprEq(t, "translated byte", p.Return, Bin{"or", IConst{0}, Un{"zx8", Load{Base: "tbl", Off: 3, Size: 1}}})
	p = onePath(t, runPaths(t, ti, "lodsFn", withBuffers))
	exprEq(t, "loaded word", p.Return, Load{Base: "src", Off: 0, Size: 4})
	p = onePath(t, runPaths(t, ti, "inFn", noInit))
	calls := callsNamed(p.Effects, portIn)
	if len(calls) != 1 || len(calls[0].Args) != 2 {
		t.Fatalf("port read effects %v", p.Effects)
	}
	exprEq(t, "read port", calls[0].Args[0], Un{"zx16", Sym{"reg.EDX"}})
	exprEq(t, "read size", calls[0].Args[1], IConst{1})
	exprEq(t, "read result", p.Return, Bin{"or", Bin{"and", Sym{"reg.EAX"}, IConst{0xFFFFFF00}}, Un{"zx8", CallResult{Fn: portIn, Seq: 1}}})
	p = onePath(t, runPaths(t, ti, "outFn", noInit))
	calls = callsNamed(p.Effects, portOut)
	if len(calls) != 1 || len(calls[0].Args) != 3 {
		t.Fatalf("port write effects %v", p.Effects)
	}
	exprEq(t, "written port", calls[0].Args[0], Un{"zx16", Sym{"reg.EDX"}})
	exprEq(t, "written byte", calls[0].Args[1], Un{"zx8", Sym{"reg.EAX"}})
	exprEq(t, "written size", calls[0].Args[2], IConst{1})
	p = onePath(t, runPaths(t, ti, "rdtscFn", noInit))
	exprEq(t, "clock low word", p.Return, CallResult{Fn: cpuRDTSCLo, Seq: 1})
	if len(callsNamed(p.Effects, cpuRDTSCHi)) != 1 {
		t.Errorf("clock effects %v", p.Effects)
	}
	p = onePath(t, runPaths(t, ti, "crFn", noInit))
	exprEq(t, "control register", p.Return, CallResult{Fn: cpuReadCR, Seq: 1})
	p = onePath(t, runPaths(t, ti, "cpuidFn", noInit))
	var names []string
	for _, e := range p.Effects {
		names = append(names, e.Fn)
	}
	sort.Strings(names)
	if !reflect.DeepEqual(names, []string{"cpu_cpuid_EAX", "cpu_cpuid_EBX", "cpu_cpuid_ECX", "cpu_cpuid_EDX"}) {
		t.Errorf("cpuid effects %v", names)
	}
}

func TestSystemInterruptReturnAndFarJump(t *testing.T) {
	ti := systemImage(t)
	lf := liftRoutine(t, ti, "sysLift")
	b := lf.blocks[key(ti.at(t, "sysLift"))]
	var names []string
	for _, e := range b.effects {
		names = append(names, e.Fn)
	}
	if !reflect.DeepEqual(names, []string{cpuCLI, cpuSTI, cpuIRETD}) || b.term.kind != "return" {
		t.Errorf("lifted system block %v ends with %+v", names, b.term)
	}
	if _, err := NewExecutor(ti.d).Run(ti.at(t, "sysLift"), noInit); err == nil || !strings.Contains(err.Error(), "interrupt return outside a lifted routine") {
		t.Errorf("interrupt return on a path: %v", err)
	}
	p := onePath(t, runPaths(t, ti, "ljmpFn", func(s *execState) { s.regs[0] = IConst{0} }))
	exprEq(t, "far jump return", p.Return, IConst{9})
	lf = liftRoutine(t, ti, "ljmpFn")
	far := lf.blocks[key(ti.at(t, "ljmpFn")+4)]
	if far == nil || far.term.kind != "jump" || far.term.target != ti.at(t, "ljmpTarget") {
		t.Fatalf("far jump block %+v among %v", far, keysOf(lf.blocks))
	}
	exprEq(t, "code segment", far.segs["CS"], IConst{8})
	if _, err := NewExecutor(ti.d).Lift(ti.at(t, "ljmpOut")); err == nil || !strings.Contains(err.Error(), "far jump out of the routine") {
		t.Errorf("far jump out: %v", err)
	}
}
