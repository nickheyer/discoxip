package xbe

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func liftText() []chunk {
	return []chunk{
		lbl("jumpFn"), movEAXImm(5), jmp8("jumpTarget"), pad(), lbl("jumpTarget"), abs32([]byte{0xA3}, "g1", 0), ret(), pad(),
		lbl("branchFn"), by(0x83, 0x7C, 0x24, 0x04, 0x00), je8("branchZero"), movEAXImm(1), ret(), lbl("branchZero"), movEAXImm(2), ret(), pad(),
		lbl("switchFn"), by(0x8B, 0x44, 0x24, 0x04), abs32([]byte{0xFF, 0x24, 0x85}, "table", 0), lbl("case0"), movEAXImm(10), ret(), lbl("case1"), movEAXImm(20), ret(), lbl("case2"), movEAXImm(30), ret(), pad(),
		lbl("trapFn"), by(0x85, 0xC0), jne8("trapOk"), lbl("trapInsn"), by(0xCC), lbl("trapOk"), ret(), pad(),
		lbl("haltFn"), by(0x6A, 0x00), abs32([]byte{0xFF, 0x15}, "thunk.KeBugCheck", 0), ret(), pad(),
		lbl("noreturnFn"), call("spinFn"), ret(), pad(),
		lbl("spinFn"), jmp8("spinFn"), pad(),
		lbl("flagsFn"), by(0x83, 0xF8, 0x00), jmp8("flagsNext"), pad(), lbl("flagsNext"), je8("flagsZero"), movEAXImm(1), ret(), lbl("flagsZero"), movEAXImm(2), ret(), pad(),
		lbl("fcmpFn"), by(0xD9, 0x44, 0x24, 0x04), by(0xD8, 0x5C, 0x24, 0x08), by(0xDF, 0xE0), jmp8("fcmpNext"), pad(), lbl("fcmpNext"), by(0xF6, 0xC4, 0x41), jne8("fcmpLess"), movEAXImm(1), ret(), lbl("fcmpLess"), movEAXImm(2), ret(), pad(),
		lbl("xmmFn"), by(0xF3, 0x0F, 0x58, 0xC1), abs32([]byte{0xF3, 0x0F, 0x11, 0x05}, "g1", 0), ret(), pad(),
		lbl("mmFn"), by(0x0F, 0x6E, 0xC0), abs32([]byte{0x0F, 0x7F, 0x05}, "g2", 0), ret(), pad(),
		lbl("invariantFn"), movEBXImm(5), by(0x8D, 0x74, 0x24, 0x08), abs32([]byte{0x8B, 0x3D}, "thunk.KeGetCurrentThread", 0), by(0x8B, 0x54, 0x24, 0x04), abs32([]byte{0x8B, 0x0D}, "gobj", 0), jmp8("invNext"), pad(),
		lbl("invNext"), abs32([]byte{0x89, 0x1D}, "g1", 0), abs32([]byte{0x89, 0x35}, "g2", 0), abs32([]byte{0x89, 0x3D}, "g3", 0), abs32([]byte{0x89, 0x15}, "g4", 0), abs32([]byte{0x89, 0x0D}, "g5", 0), ret(), pad(),
		lbl("sharedFn"), by(0x8B, 0x44, 0x24, 0x04), by(0x83, 0xC0, 0x03), abs32([]byte{0xA3}, "g1", 0), abs32([]byte{0xA3}, "g2", 0), ret(), pad(),
		lbl("callFn"), by(0x6A, 0x04), call("std4"), abs32([]byte{0xA3}, "g1", 0), ret(), pad(),
		lbl("std4"), by(0x8B, 0x44, 0x24, 0x04), retn(4), pad(),
		lbl("iretCaller"), call("iretFn"), ret(), pad(),
		lbl("iretFn"), by(0xCF), pad(),
		lbl("vtfn"), ret(), pad(),
	}
}

func liftImage(t *testing.T) *testImage {
	t.Helper()
	return buildImage(t, liftText(), []chunk{
		lbl("table"), wordAt("case0", 0), wordAt("case1", 0), wordAt("case2", 0), word(0),
		lbl("vt"), wordAt("vtfn", 0),
	}, []chunk{
		lbl("g1"), word(0), lbl("g2"), word(0), word(0), lbl("g3"), word(0), lbl("g4"), word(0), lbl("g5"), word(0), lbl("gobj"), word(0),
	}, []string{"KeGetCurrentThread", "KeBugCheck"})
}

func key(va uint32) string { return fmt.Sprintf("%08X", va) }

func TestLiftJumpAndReturn(t *testing.T) {
	ti := liftImage(t)
	lf := liftRoutine(t, ti, "jumpFn")
	entry, target := ti.at(t, "jumpFn"), ti.at(t, "jumpTarget")
	if lf.Entry != key(entry) || len(lf.blocks) != 2 || len(lf.Regs) != 0 {
		t.Fatalf("entry %s with %d blocks and inputs %v", lf.Entry, len(lf.blocks), lf.Regs)
	}
	a := lf.blocks[key(entry)]
	if a.term.kind != "jump" || a.term.target != target || !reflect.DeepEqual(a.succs, []string{key(target)}) || a.sp != liftedEntrySP {
		t.Fatalf("entry block ends with %+v at sp %X", a.term, a.sp)
	}
	exprEq(t, "entry EAX", a.regs[0], IConst{5})
	b := lf.blocks[key(target)]
	if b.term.kind != "return" || len(b.effects) != 1 {
		t.Fatalf("target block ends with %+v after %v", b.term, b.effects)
	}
	expectStore(t, b.effects[0], "", int32(ti.at(t, "g1")), 4, IConst{5})
	exprEq(t, "return", b.term.value, IConst{5})
	want := fmt.Sprintf("block %s sp=10000\n  EAX = 5\n  jump %s\nblock %s sp=10000\n  .4@0x%X = 5\n  EAX = 5\n  return 5\n", key(entry), key(target), key(target), ti.at(t, "g1"))
	if got := lf.String(); got != want {
		t.Errorf("dump\n%s\nwant\n%s", got, want)
	}
}

func TestLiftBranchAndSwitch(t *testing.T) {
	ti := liftImage(t)
	lf := liftRoutine(t, ti, "branchFn")
	entry := ti.at(t, "branchFn")
	a := lf.blocks[key(entry)]
	arg := Load{Base: "stack", Off: liftedEntrySP + 4, Size: 4}
	if a.term.kind != "branch" || a.term.taken != ti.at(t, "branchZero") || a.term.fall != entry+7 {
		t.Fatalf("branch block ends with %+v", a.term)
	}
	exprEq(t, "branch condition", a.term.cond, Cond{"eq", arg, IConst{0}})
	if !reflect.DeepEqual(a.succs, []string{key(ti.at(t, "branchZero")), key(entry + 7)}) {
		t.Errorf("successors %v", a.succs)
	}
	exprEq(t, "fall return", lf.blocks[key(entry+7)].term.value, IConst{1})
	exprEq(t, "taken return", lf.blocks[key(ti.at(t, "branchZero"))].term.value, IConst{2})
	pool := NewExprPool()
	lf.serialize(pool)
	lb := lf.Blocks[key(entry)]
	if lb.Term.Kind != "branch" || lb.Term.Taken != key(ti.at(t, "branchZero")) || lb.Term.Fall != key(entry+7) || lb.Term.Cond != pool.Add(Cond{"eq", arg, IConst{0}}) {
		t.Errorf("serialized branch %+v", lb.Term)
	}
	if lb.Flags == nil || lb.Flags.Kind != "cmp" || lb.Flags.A != pool.Add(arg) || lb.Flags.B != pool.Add(IConst{0}) || lb.Flags.CF != -1 || lb.Flags.FswA != -1 {
		t.Errorf("serialized flags %+v", lb.Flags)
	}
	lf = liftRoutine(t, ti, "switchFn")
	s := lf.blocks[key(ti.at(t, "switchFn"))]
	cases := []string{key(ti.at(t, "case0")), key(ti.at(t, "case1")), key(ti.at(t, "case2"))}
	if s.term.kind != "switch" || !reflect.DeepEqual(s.succs, cases) {
		t.Fatalf("switch block ends with %+v to %v", s.term, s.succs)
	}
	exprEq(t, "switch index", s.term.index, arg)
	for i, c := range cases {
		exprEq(t, c, lf.blocks[c].term.value, IConst{uint32(10 * (i + 1))})
	}
	lf.serialize(pool)
	if sb := lf.Blocks[key(ti.at(t, "switchFn"))]; sb.Term.Kind != "switch" || !reflect.DeepEqual(sb.Term.Targets, cases) || sb.Term.Index != pool.Add(arg) {
		t.Errorf("serialized switch %+v", sb.Term)
	}
}

func TestLiftTrapHaltNoreturn(t *testing.T) {
	ti := liftImage(t)
	lf := liftRoutine(t, ti, "trapFn")
	if b := lf.blocks[key(ti.at(t, "trapInsn"))]; b == nil || b.term.kind != "trap" || len(b.succs) != 0 {
		t.Fatalf("trap block %+v", b)
	}
	if b := lf.blocks[key(ti.at(t, "trapOk"))]; b == nil || b.term.kind != "return" {
		t.Fatalf("return block %+v", b)
	}
	lf = liftRoutine(t, ti, "haltFn")
	b := lf.blocks[key(ti.at(t, "haltFn"))]
	if b.term.kind != "halt" || len(lf.blocks) != 1 {
		t.Fatalf("halt block ends with %+v among %d blocks", b.term, len(lf.blocks))
	}
	calls := callsNamed(b.effects, "KeBugCheck")
	if len(calls) != 1 || len(calls[0].Args) != 1 {
		t.Fatalf("halt effects %v", b.effects)
	}
	exprEq(t, "bug check code", calls[0].Args[0], IConst{0})
	expectStore(t, b.effects[0], "stack", liftedEntrySP-4, 4, IConst{0})
	lf = liftRoutine(t, ti, "noreturnFn")
	b = lf.blocks[key(ti.at(t, "noreturnFn"))]
	if b.term.kind != "noreturn" || len(lf.blocks) != 1 {
		t.Fatalf("noreturn block ends with %+v among %d blocks", b.term, len(lf.blocks))
	}
	if calls := callsNamed(b.effects, subName(ti.at(t, "spinFn"))); len(calls) != 1 {
		t.Fatalf("noreturn effects %v", b.effects)
	}
	if !ti.d.neverReturns(ti.d.Functions[ti.at(t, "spinFn")]) || ti.d.neverReturns(ti.d.Functions[ti.at(t, "trapFn")]) {
		t.Errorf("never returns misjudged")
	}
}

func TestLiftEntryFlagsTerms(t *testing.T) {
	ti := liftImage(t)
	lf := liftRoutine(t, ti, "flagsFn")
	entry, next := ti.at(t, "flagsFn"), ti.at(t, "flagsNext")
	a := lf.blocks[key(entry)]
	if a.fl.kind != "cmp" || !reflect.DeepEqual(a.succs, []string{blockKey(next, a)}) {
		t.Fatalf("entry block flags %s successors %v", a.fl.kind, a.succs)
	}
	exprEq(t, "flags a", a.fl.a, Sym{"reg.EAX"})
	exprEq(t, "flags b", a.fl.b, IConst{0})
	b := lf.blocks[blockKey(next, a)]
	if b == nil || b.term.kind != "branch" {
		t.Fatalf("flag reading block %+v", b)
	}
	exprEq(t, "entry flag condition", b.term.cond, Cond{"eq", Sym{"flags.a"}, Sym{"flags.b"}})
	if _, plain := lf.blocks[key(next)]; plain {
		t.Errorf("the flag reading block was also lifted without its predecessor")
	}
	lf = liftRoutine(t, ti, "fcmpFn")
	entry, next = ti.at(t, "fcmpFn"), ti.at(t, "fcmpNext")
	a = lf.blocks[key(entry)]
	if a.fcmp == nil || !a.axFSW {
		t.Fatalf("compare block leaves fcmp %v ax %v", a.fcmp, a.axFSW)
	}
	exprEq(t, "compared left", a.fcmp.a, Un{"bits2f", Load{Base: "stack", Off: liftedEntrySP + 4, Size: 4}})
	exprEq(t, "compared right", a.fcmp.b, Un{"bits2f", Load{Base: "stack", Off: liftedEntrySP + 8, Size: 4}})
	exprEq(t, "status word register", a.regs[0], Sym{"fsw"})
	b = lf.blocks[blockKey(next, a)]
	if b == nil || b.term.kind != "branch" {
		t.Fatalf("status reading block %+v", b)
	}
	exprEq(t, "status condition", b.term.cond, FCmpSet{Sym{"fcmp.a"}, Sym{"fcmp.b"}, 1 | 2 | 8})
	pool := NewExprPool()
	lf.serialize(pool)
	if fc := lf.Blocks[key(entry)].FCmp; fc == nil || !fc.AxFSW || fc.A != pool.Add(a.fcmp.a) || fc.B != pool.Add(a.fcmp.b) {
		t.Errorf("serialized fcmp %+v", fc)
	}
}

func TestLiftVectorRegisterNames(t *testing.T) {
	ti := liftImage(t)
	lf := liftRoutine(t, ti, "xmmFn")
	if !reflect.DeepEqual(lf.Regs, []string{"XMM0", "XMM1"}) {
		t.Fatalf("inputs %v", lf.Regs)
	}
	b := lf.blocks[key(ti.at(t, "xmmFn"))]
	sum := Bin{"fadd", Sym{"xmm0.0"}, Sym{"xmm1.0"}}
	if len(b.xmm) != 1 {
		t.Fatalf("vector lanes %v", b.xmm)
	}
	exprEq(t, "xmm0.0", b.xmm["xmm0.0"], sum)
	if len(b.effects) != 1 {
		t.Fatalf("effects %v", b.effects)
	}
	expectStore(t, b.effects[0], "", int32(ti.at(t, "g1")), 4, Un{"f32", sum})
	lf = liftRoutine(t, ti, "mmFn")
	b = lf.blocks[key(ti.at(t, "mmFn"))]
	if len(b.mm) != 2 {
		t.Fatalf("mmx lanes %v", b.mm)
	}
	exprEq(t, "mm0.0", b.mm["mm0.0"], Sym{"reg.EAX"})
	exprEq(t, "mm0.1", b.mm["mm0.1"], IConst{0})
	if len(b.effects) != 2 {
		t.Fatalf("effects %v", b.effects)
	}
	expectStore(t, b.effects[0], "", int32(ti.at(t, "g2")), 4, Sym{"reg.EAX"})
	expectStore(t, b.effects[1], "", int32(ti.at(t, "g2"))+4, 4, IConst{0})
	pool := NewExprPool()
	lf.serialize(pool)
	if lb := lf.Blocks[key(ti.at(t, "mmFn"))]; lb.MM["mm0.0"] != pool.Add(Sym{"reg.EAX"}) || lb.MM["mm0.1"] != pool.Add(IConst{0}) || lb.XMM != nil {
		t.Errorf("serialized lanes %v %v", lb.MM, lb.XMM)
	}
}

// Checks only constants, stack addresses, typed globals and export pointers reach the next block
func TestLiftRegisterInvariants(t *testing.T) {
	ti := liftImage(t)
	ex := NewExecutor(ti.d)
	gobj := ti.at(t, "gobj")
	ex.vtables[gobj] = ti.at(t, "vt")
	lf, err := ex.Lift(ti.at(t, "invariantFn"))
	if err != nil {
		t.Fatal(err)
	}
	b := lf.blocks[key(ti.at(t, "invNext"))]
	if b == nil || len(b.effects) != 5 {
		t.Fatalf("successor block %+v", b)
	}
	expectStore(t, b.effects[0], "", int32(ti.at(t, "g1")), 4, IConst{5})
	expectStore(t, b.effects[1], "", int32(ti.at(t, "g2")), 4, Ptr{"stack", liftedEntrySP + 8})
	expectStore(t, b.effects[2], "", int32(ti.at(t, "g3")), 4, Ptr{"import:KeGetCurrentThread", 0})
	expectStore(t, b.effects[3], "", int32(ti.at(t, "g4")), 4, Sym{"reg.EDX"})
	expectStore(t, b.effects[4], "", int32(ti.at(t, "g5")), 4, Ptr{fmt.Sprintf("g%08X", gobj), 0})
	for _, v := range []Expr{IConst{1}, Ptr{"", 0x21000}, Ptr{"stack", 4}, Ptr{"g00021000", 8}, Ptr{"import:KeTickCount", 0}} {
		if !blockInvariant(v) {
			t.Errorf("%s does not hold across blocks", ExprString(v))
		}
	}
	for _, v := range []Expr{Sym{"reg.EDX"}, Load{Base: "stack", Off: 4, Size: 4}, Ptr{"this", 0}, Ptr{"res1", 0}, Bin{"add", IConst{1}, Sym{"x"}}, nil} {
		if blockInvariant(v) {
			t.Errorf("%s holds across blocks", ExprString(v))
		}
	}
}

func TestLiftDumpNamesSharedNodes(t *testing.T) {
	ti := liftImage(t)
	lf := liftRoutine(t, ti, "sharedFn")
	entry := ti.at(t, "sharedFn")
	want := fmt.Sprintf("block %s sp=10000\n  t0 = (stack.4@0x10004 add 3)\n  .4@0x%X = t0\n  .4@0x%X = t0\n  EAX = t0\n  return t0\n", key(entry), ti.at(t, "g1"), ti.at(t, "g2"))
	if got := lf.String(); got != want {
		t.Errorf("dump\n%s\nwant\n%s", got, want)
	}
	lf = liftRoutine(t, ti, "callFn")
	b := lf.blocks[key(entry)]
	if b != nil {
		t.Fatalf("call routine lifted the shared routine")
	}
	b = lf.blocks[key(ti.at(t, "callFn"))]
	std4 := ti.at(t, "std4")
	if calls := callsNamed(b.effects, subName(std4)); len(calls) != 1 || len(calls[0].Args) != 1 {
		t.Fatalf("call effects %v", b.effects)
	}
	want = fmt.Sprintf("block %s sp=10000\n  stack.4@0xFFFC = 4\n  %s#1(4)\n  t0 = %s#1(4)\n  .4@0x%X = t0\n  EAX = t0\n  return t0\n", key(ti.at(t, "callFn")), subName(std4), subName(std4), ti.at(t, "g1"))
	if got := lf.String(); got != want {
		t.Errorf("call dump\n%s\nwant\n%s", got, want)
	}
	if b.sp != liftedEntrySP {
		t.Errorf("stack settles at %X after the stdcall", b.sp)
	}
}

func TestLiftMachineTermsRender(t *testing.T) {
	cases := []struct {
		term  blockTerm
		succs []string
		want  string
	}{
		{blockTerm{kind: "jumpind", index: Sym{"reg.EAX"}}, nil, "jump to reg.EAX"},
		{blockTerm{kind: "hlt", target: 0x11002}, []string{"00011002"}, "hlt then 00011002"},
		{blockTerm{kind: "int", vector: 3, target: 0x11002}, []string{"00011002"}, "int 3 then 00011002"},
		{blockTerm{kind: "iret"}, nil, "iret"},
		{blockTerm{kind: "trap"}, nil, "trap"},
		{blockTerm{kind: "halt"}, nil, "halt"},
		{blockTerm{kind: "noreturn"}, nil, "noreturn"},
	}
	for _, tc := range cases {
		term := tc.term
		b := &liftedBlock{va: 0x11000, term: &term, succs: tc.succs, spExpr: Bin{"add", Sym{"reg.ESP"}, IConst{4}}}
		lf := &LiftedFunction{blocks: map[string]*liftedBlock{"00011000": b}, Blocks: map[string]*LiftedBlock{}}
		want := "block 00011000\n  ESP = (reg.ESP add 4)\n  " + tc.want + "\n"
		if got := lf.String(); got != want {
			t.Errorf("%s renders as %q, want %q", tc.term.kind, got, want)
		}
		pool := NewExprPool()
		lf.serialize(pool)
		lb := lf.Blocks["00011000"]
		if lb.Term.Kind != tc.term.kind || lb.ESP != pool.Add(b.spExpr) {
			t.Errorf("%s serialized as %+v", tc.term.kind, lb)
		}
		switch tc.term.kind {
		case "jumpind":
			if lb.Term.Index != pool.Add(Sym{"reg.EAX"}) {
				t.Errorf("jumpind index %d", lb.Term.Index)
			}
		case "hlt":
			if lb.Term.Target != "00011002" {
				t.Errorf("hlt target %q", lb.Term.Target)
			}
		case "int":
			if lb.Term.Target != "00011002" || lb.Term.Vector != 3 {
				t.Errorf("int target %q vector %d", lb.Term.Target, lb.Term.Vector)
			}
		}
	}
}

func TestLiftPathsAndEffects(t *testing.T) {
	ti := liftImage(t)
	lf := liftRoutine(t, ti, "branchFn")
	paths := lf.asPaths()
	if len(paths) != 3 {
		t.Fatalf("%d paths from 3 blocks", len(paths))
	}
	entryKey := key(ti.at(t, "branchFn"))
	keys := sortedKeys(map[string]bool{entryKey: true, key(ti.at(t, "branchFn") + 7): true, key(ti.at(t, "branchZero")): true})
	for i, k := range keys {
		b := lf.blocks[k]
		switch b.term.kind {
		case "branch":
			if len(paths[i].Guards) != 1 || !equalExprs(paths[i].Guards[0], b.term.cond) || paths[i].Return != nil {
				t.Errorf("branch path %+v", paths[i])
			}
		case "return":
			if len(paths[i].Guards) != 0 || !equalExprs(paths[i].Return, b.term.value) {
				t.Errorf("return path %+v", paths[i])
			}
		}
	}
	lf = liftRoutine(t, ti, "invariantFn")
	if all := lf.allEffects(); len(all) != 5 {
		t.Errorf("%d effects over the routine, want 5", len(all))
	}
	lf = liftRoutine(t, ti, "iretFn")
	b := lf.blocks[key(ti.at(t, "iretFn"))]
	if b.term.kind != "return" || len(callsNamed(b.effects, cpuIRETD)) != 1 {
		t.Errorf("interrupt return block %+v with %v", b.term, b.effects)
	}
}

func TestLiftBlockLimit(t *testing.T) {
	ti := buildImage(t, []chunk{lbl("blocks"), rep(maxLiftedBlocks+100, 0x90, 0xEB, 0x00), ret()}, nil, nil, nil)
	ex := NewExecutor(ti.d)
	lf, err := ex.Lift(ti.at(t, "blocks"))
	if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("more than %d blocks", maxLiftedBlocks)) {
		t.Fatalf("block limit: %v", err)
	}
	if lf == nil || len(lf.blocks) != maxLiftedBlocks {
		t.Fatalf("%d blocks lifted before the limit", len(lf.blocks))
	}
}

func TestLiftBlockStepBudget(t *testing.T) {
	ti := buildImage(t, []chunk{lbl("steps"), rep(maxBlockSteps+1, 0x90), ret()}, nil, nil, nil)
	ex := NewExecutor(ti.d)
	_, err := ex.Lift(ti.at(t, "steps"))
	if err == nil || !strings.Contains(err.Error(), "step limit exceeded") {
		t.Fatalf("step budget: %v", err)
	}
	if ex.StepLimit != 0 {
		t.Errorf("the lift left the step limit at %d", ex.StepLimit)
	}
	ti = buildImage(t, []chunk{lbl("fits"), rep(maxBlockSteps-1, 0x90), ret()}, nil, nil, nil)
	lf := liftRoutine(t, ti, "fits")
	if b := lf.blocks[key(ti.at(t, "fits"))]; b.term.kind != "return" {
		t.Errorf("a block within the budget ends with %+v", b.term)
	}
}

func TestLiftUnknownRoutine(t *testing.T) {
	ti := liftImage(t)
	if _, err := NewExecutor(ti.d).Lift(0x12345); err == nil || !strings.Contains(err.Error(), "not disassembled") {
		t.Errorf("unknown routine: %v", err)
	}
	if blockKeyVA("00011008@00011000") != 0x11008 || blockKeyVA("00011008~FFF0_0") != 0x11008 {
		t.Errorf("block key addresses misread")
	}
}
