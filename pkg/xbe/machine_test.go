package xbe

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"

	"golang.org/x/arch/x86/x86asm"
)

func kernelTextChunks() []chunk {
	return []chunk{
		lbl("kEntry"), movEAXImm(1), ret(), pad(),
		lbl("kThread"), abs32([]byte{0x68}, "kDyn", 0), abs32([]byte{0x8D, 0x05}, "kLea", 0), call("kCallee"), lbl("kAfterCall"), ret(), pad(),
		lbl("kDyn"), movEAXImm(2), ret(), pad(),
		lbl("kLea"), movEAXImm(3), ret(), pad(),
		lbl("kCallee"), abs32([]byte{0xA3}, "kvar", 0), ret(), pad(),
		lbl("kPtrTarget"), movEAXImm(4), ret(), pad(),
		lbl("kPadLea"), by(0x8D, 0x76, 0x00), lbl("kPadded1"), movEAXImm(5), ret(), pad(),
		lbl("kPadMov"), by(0x8B, 0xFF), lbl("kPadded2"), movEAXImm(6), ret(), pad(),
		lbl("kPadXchg"), by(0x87, 0xC9), lbl("kPadded3"), movEAXImm(7), ret(), pad(),
		lbl("kMidRef"), movEAXImm(1), lbl("kMid"), movEAXImm(8), ret(), pad(),
		lbl("kPushMid"), abs32([]byte{0x68}, "kMid", 0), ret(), pad(),
		lbl("kHlt"), by(0xF4), lbl("kAfterHlt"), movEAXImm(9), ret(), pad(),
		lbl("kInt"), by(0xCC), lbl("kAfterInt"), movEAXImm(10), ret(), pad(),
		lbl("kSti"), by(0xFB), lbl("kAfterSti"), movEAXImm(11), ret(), pad(),
		lbl("kPopfd"), by(0x9D), lbl("kAfterPopfd"), je8("kPopTaken"), movEAXImm(12), ret(), lbl("kPopTaken"), movEAXImm(13), ret(), pad(),
		lbl("kUd2"), by(0x0F, 0x0B), pad(),
		lbl("kMem"), abs32([]byte{0xA3}, "kvar", 0), abs32([]byte{0x8B, 0x1D}, "kvar", 0), by(0xA3, 0, 0, 0, 0xFD), by(0x8B, 0x0D, 0, 0, 0, 0xFD), ret(), pad(),
		lbl("kStack"), by(0x50), by(0x8B, 0x1C, 0x24), by(0x89, 0x45, 0xFC), by(0x8B, 0x4D, 0xFC), by(0x89, 0x06), by(0x8B, 0x16), ret(), pad(),
		lbl("kWrite"), abs32([]byte{0xC7, 0x05}, "kvar2", 0, 0x34, 0x12, 0, 0), ret(), pad(),
		lbl("kLoop"), jmp8("kLoop"), pad(),
		lbl("kJumpInd"), by(0xFF, 0xE0), pad(),
		lbl("kCallRet"), call("kWrite"), lbl("kCallRetAfter"), ret(), pad(),
		lbl("kReadTick"), abs32([]byte{0xA1}, "ktick", 0), ret(), pad(),
		lbl("kCmp"), by(0x83, 0xF9, 0x07), ret(), pad(),
		lbl("kCondRoot"), je8("kCondTaken"), movEAXImm(1), ret(), lbl("kCondTaken"), movEAXImm(2), ret(), pad(),
		lbl("kFlagsJump"), by(0x83, 0xF9, 0x07), jmp8("kFlagsNext"), pad(), lbl("kFlagsNext"), je8("kFlagsTaken"), movEAXImm(1), ret(), lbl("kFlagsTaken"), movEAXImm(2), ret(), pad(),
		lbl("kRdtsc"), by(0x0F, 0x31), abs32([]byte{0xA3}, "kvar", 0), by(0x0F, 0x31), abs32([]byte{0xA3}, "kvar2", 0), ret(), pad(),
		lbl("kCopy"), by(0xFC), by(0xF3, 0xA5), ret(), pad(),
		lbl("kFill"), by(0xFC), by(0xF3, 0xAB), ret(), pad(),
		lbl("kMovsd"), by(0xA5), ret(), pad(),
		lbl("kMovsdCld"), by(0xFC), by(0xA5), ret(), pad(),
		lbl("kDiv"), by(0xF7, 0xF1), ret(), pad(),
		lbl("kSeg"), by(0x64, 0xA1, 0x20, 0, 0, 0), ret(), pad(),
		lbl("kPort"), by(0xEE), ret(), pad(),
		lbl("kCli"), by(0xFA), ret(), pad(),
		lbl("kPushf"), by(0x9C), by(0x58), ret(), pad(),
		lbl("kPopf"), by(0x50), by(0x9D), ret(), pad(),
		lbl("kScas"), by(0xF2, 0xAE), ret(), pad(),
		lbl("kCmps"), by(0xF3, 0xA6), ret(), pad(),
		lbl("kBad"), by(0xC8, 0x00, 0x00, 0x00), ret(), pad(),
	}
}

func kernelDataChunks() []chunk {
	return []chunk{
		lbl("kptr"), wordAt("kPtrTarget", 0),
		lbl("kvar"), word(0),
		lbl("kvar2"), word(0),
		lbl("ktick"), word(0),
		lbl("kbuf"), rep(32, 0),
	}
}

func kernelImage(t *testing.T) *testImage {
	t.Helper()
	return buildKernelImage(t, kernelTextChunks(), kernelDataChunks(), map[uint32]string{104: "kThread", 156: "ktick"})
}

func sortedVAs(m map[uint32]bool) []uint32 {
	var out []uint32
	for va := range m {
		out = append(out, va)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func TestKernelDisassemblyExports(t *testing.T) {
	ti := kernelImage(t)
	fn := ti.d.Functions[ti.at(t, "kThread")]
	if fn == nil || fn.Symbol == nil || fn.Symbol.Name != "KeGetCurrentThread" || fn.Symbol.Lib != "kernel" || fn.Symbol.ArgBytes != 0 {
		t.Fatalf("export routine %+v", fn)
	}
	if _, isRoutine := ti.d.Functions[ti.at(t, "ktick")]; isRoutine {
		t.Errorf("a referenced export is a routine")
	}
	roots := KernelRoots(ti.d, ti.exports)
	want := []uint32{kernelText, ti.at(t, "kThread")}
	sort.Slice(roots, func(i, j int) bool { return roots[i] < roots[j] })
	if !reflect.DeepEqual(roots, want) {
		t.Errorf("roots %X, want %X", roots, want)
	}
}

// Checks immediates, absolute lea operands, call targets and code pointers become dynamic leaders
func TestMachineLeadersDynamic(t *testing.T) {
	ti := kernelImage(t)
	leaders, dynamic := MachineLeaders(ti.d, KernelRoots(ti.d, ti.exports))
	want := map[uint32]bool{}
	for _, label := range []string{"kEntry", "kThread", "kDyn", "kLea", "kCallee", "kAfterCall", "kPtrTarget"} {
		want[ti.at(t, label)] = true
	}
	if !reflect.DeepEqual(sortedVAs(dynamic), sortedVAs(want)) || !reflect.DeepEqual(sortedVAs(leaders), sortedVAs(want)) {
		t.Errorf("leaders %X dynamic %X, want %X", sortedVAs(leaders), sortedVAs(dynamic), sortedVAs(want))
	}
	_, dynamic = MachineLeaders(ti.d, []uint32{ti.at(t, "kPushMid")})
	if dynamic[ti.at(t, "kMid")] || !dynamic[ti.at(t, "kPushMid")] {
		t.Errorf("an implausible pushed address became a leader %X", sortedVAs(dynamic))
	}
	if !dynamic[ti.at(t, "kDyn")] {
		t.Errorf("the aligned immediate naming kDyn is not a code pointer %X", sortedVAs(dynamic))
	}
	mm, err := LiftMachine(ti.d, KernelRoots(ti.d, ti.exports))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(mm.Routines(), sortedVAs(want)) || len(mm.Errors) != 0 {
		t.Errorf("routines %X errors %v", mm.Routines(), mm.Errors)
	}
	if mm.Base != kernelBase || len(mm.Blocks) != len(want) {
		t.Errorf("model base %X with %d blocks", mm.Base, len(mm.Blocks))
	}
	rooted := machineModel(t, ti, "kEntry")
	if _, has := rooted.blocks[key(ti.at(t, "kPtrTarget"))]; has || len(rooted.blocks) != 1 {
		t.Errorf("a rooted lift followed code pointers %v", keysOf(rooted.blocks))
	}
	pool := NewExprPool()
	into, err := LiftMachineInto(ti.d, []uint32{ti.at(t, "kEntry")}, pool)
	if err != nil {
		t.Fatal(err)
	}
	pointed := []string{key(ti.at(t, "kEntry")), key(ti.at(t, "kDyn")), key(ti.at(t, "kPtrTarget"))}
	if !reflect.DeepEqual(keysOf(into.blocks), pointed) || into.Exprs != pool || len(pool.Nodes) < 2 {
		t.Errorf("a pooled lift of the entry gave %v with %d pooled nodes, want %v", keysOf(into.blocks), len(pool.Nodes), pointed)
	}
}

func TestMachinePaddingEntries(t *testing.T) {
	ti := kernelImage(t)
	for _, label := range []string{"kPadded1", "kPadded2", "kPadded3", "kDyn", "kEntry"} {
		if !ti.d.plausibleEntry(ti.at(t, label)) {
			t.Errorf("%s is not a plausible entry", label)
		}
	}
	for _, label := range []string{"kMid", "kptr"} {
		if ti.d.plausibleEntry(ti.at(t, label)) {
			t.Errorf("%s is a plausible entry", label)
		}
	}
	if ti.d.plausibleEntry(ti.at(t, "kEntry") + 1) {
		t.Errorf("the middle of an instruction is a plausible entry")
	}
	for _, label := range []string{"kPadLea", "kPadMov", "kPadXchg"} {
		if !isPadding(ti.d.InsnByVA[ti.at(t, label)]) {
			t.Errorf("%s is not padding", label)
		}
	}
	if isPadding(ti.d.InsnByVA[ti.at(t, "kMidRef")]) {
		t.Errorf("a load of an immediate is padding")
	}
	if !ti.d.executableAt(ti.at(t, "kEntry")) || ti.d.executableAt(ti.at(t, "kvar")) || ti.d.executableAt(0x12345678) {
		t.Errorf("executable addresses misjudged")
	}
}

func TestMachineBlockEnds(t *testing.T) {
	ti := kernelImage(t)
	mm := machineModel(t, ti, "kHlt", "kInt", "kSti", "kPopfd", "kUd2")
	if len(mm.Errors) != 0 {
		t.Fatalf("errors %v", mm.Errors)
	}
	hlt := blockOf(t, mm, ti.at(t, "kHlt"))
	if hlt.term.kind != "hlt" || hlt.term.target != ti.at(t, "kAfterHlt") || mm.Blocks[key(ti.at(t, "kHlt"))].Term.Target != key(ti.at(t, "kAfterHlt")) {
		t.Errorf("halt block ends with %+v", hlt.term)
	}
	in := blockOf(t, mm, ti.at(t, "kInt"))
	lb := mm.Blocks[key(ti.at(t, "kInt"))]
	if in.term.kind != "int" || in.term.vector != 3 || lb.Term.Kind != "int" || lb.Term.Vector != 3 || lb.Term.Target != key(ti.at(t, "kAfterInt")) {
		t.Errorf("interrupt block ends with %+v serialized %+v", in.term, lb.Term)
	}
	sti := blockOf(t, mm, ti.at(t, "kSti"))
	if sti.term.kind != "jump" || sti.term.target != ti.at(t, "kAfterSti") || len(callsNamed(sti.effects, cpuSTI)) != 1 {
		t.Errorf("sti block ends with %+v after %v", sti.term, sti.effects)
	}
	popfd := blockOf(t, mm, ti.at(t, "kPopfd"))
	if popfd.term.kind != "jump" || popfd.term.target != ti.at(t, "kAfterPopfd") || popfd.fl.kind != "eflags" {
		t.Errorf("popfd block ends with %+v flags %s", popfd.term, popfd.fl.kind)
	}
	word := Load{Addr: Sym{"reg.ESP"}, Size: 4}
	calls := callsNamed(popfd.effects, cpuPopFD)
	if len(calls) != 1 {
		t.Fatalf("popfd effects %v", popfd.effects)
	}
	exprEq(t, "popped word", calls[0].Args[0], word)
	exprEq(t, "flags word", popfd.fl.a, word)
	after := mm.blocks[blockKey(ti.at(t, "kAfterPopfd"), popfd)]
	if after == nil || after.term.kind != "branch" {
		t.Fatalf("block after popfd %+v among %v", after, keysOf(mm.blocks))
	}
	exprEq(t, "zero flag condition", after.term.cond, Cond{"ne", Bin{"and", Bin{"shr", Sym{"flags.a"}, IConst{6}}, IConst{1}}, IConst{0}})
	ud2 := blockOf(t, mm, ti.at(t, "kUd2"))
	if ud2.term.kind != "int" || ud2.term.vector != 6 || ud2.term.target != ti.at(t, "kUd2") {
		t.Errorf("undefined instruction ends with %+v", ud2.term)
	}
}

func TestMachineMemoryVersusDevice(t *testing.T) {
	ti := kernelImage(t)
	mm := machineModel(t, ti, "kMem")
	b := blockOf(t, mm, ti.at(t, "kMem"))
	exprEq(t, "memory read back", b.regs[3], Sym{"reg.EAX"})
	register := uint32(0xFD000000)
	device := Load{Base: "", Off: int32(register), Size: 4}
	exprEq(t, "device read", b.regs[1], device)
	if l, ok := b.regs[1].(Load); !ok || l.Epoch != 2 {
		t.Errorf("device read %s is not after both stores", ExprString(b.regs[1]))
	}
	stores := effectsOf(Path{Effects: b.effects}, "store")
	if len(stores) != 2 {
		t.Fatalf("stores %v", stores)
	}
	expectStore(t, stores[0], "", int32(ti.at(t, "kvar")), 4, Sym{"reg.EAX"})
	expectStore(t, stores[1], "", int32(register), 4, Sym{"reg.EAX"})
	for addr, want := range map[uint32]bool{0x80000000: true, 0x80021000: true, 0xBFFFFFFF: true, 0xC0000000: false, 0xFD000000: false, 0x7FFFFFFF: false} {
		if memoryAddress(IConst{addr}) != want {
			t.Errorf("%08X memory %v", addr, !want)
		}
	}
	if !memoryAddress(Bin{"sub", Sym{"reg.ESP"}, IConst{4}}) || !memoryAddress(Bin{"add", Sym{"reg.EBP"}, IConst{8}}) || memoryAddress(Sym{"reg.ESI"}) || memoryAddress(Bin{"add", Sym{"reg.EAX"}, IConst{0x80000000}}) {
		t.Errorf("stack bases misjudged")
	}
}

func TestMachineStackByBase(t *testing.T) {
	ti := kernelImage(t)
	mm := machineModel(t, ti, "kStack")
	b := blockOf(t, mm, ti.at(t, "kStack"))
	exprEq(t, "pushed word read back", b.regs[3], Sym{"reg.EAX"})
	exprEq(t, "frame local read back", b.regs[1], Sym{"reg.EAX"})
	exprEq(t, "other pointer read", b.regs[2], Load{Addr: Sym{"reg.ESI"}, Size: 4})
	exprEq(t, "stack pointer", b.spExpr, Bin{"add", Bin{"sub", Sym{"reg.ESP"}, IConst{4}}, IConst{4}})
	if b.term.kind != "jumpind" {
		t.Errorf("return ends with %+v", b.term)
	}
	stores := effectsOf(Path{Effects: b.effects}, "store")
	if len(stores) != 3 {
		t.Fatalf("stores %v", stores)
	}
	exprEq(t, "push address", stores[0].Addr, Bin{"sub", Sym{"reg.ESP"}, IConst{4}})
	exprEq(t, "frame address", stores[1].Addr, Bin{"add", Sym{"reg.EBP"}, IConst{0xFFFFFFFC}})
	exprEq(t, "pointer address", stores[2].Addr, Sym{"reg.ESI"})
	a, off := addressParts(Bin{"sub", Sym{"reg.ESP"}, IConst{4}})
	if !equalExprs(a, Sym{"reg.ESP"}) || off != -4 {
		t.Errorf("address parts %s %d", ExprString(a), off)
	}
	if mayAlias(IConst{0x100}, 4, IConst{0x104}, 4) || !mayAlias(IConst{0x100}, 4, IConst{0x103}, 1) || !mayAlias(Sym{"a"}, 4, Sym{"b"}, 4) || mayAlias(Bin{"add", Sym{"a"}, IConst{8}}, 4, Sym{"a"}, 8) {
		t.Errorf("aliasing misjudged")
	}
}

func TestMachineModelQueries(t *testing.T) {
	ti := kernelImage(t)
	mm := machineModel(t, ti, "kCallRet", "kWrite", "kLoop", "kJumpInd")
	kCallRet, kWrite, kAfter := ti.at(t, "kCallRet"), ti.at(t, "kWrite"), ti.at(t, "kCallRetAfter")
	if va, ok := mm.BlockVA(key(kWrite)); !ok || va != kWrite {
		t.Errorf("block address %X %v", va, ok)
	}
	if _, ok := mm.BlockVA("nothing"); ok {
		t.Errorf("an unknown key has an address")
	}
	if got := mm.BlocksStoring(0x1234); !reflect.DeepEqual(got, []uint32{kWrite}) {
		t.Errorf("blocks storing 0x1234 %X", got)
	}
	if !mm.CallsTo(kCallRet, kWrite) || mm.CallsTo(kCallRet, ti.at(t, "kLoop")) || mm.CallsTo(kWrite, kWrite) {
		t.Errorf("calls misjudged")
	}
	blocks, callees := mm.RoutineBlocks(kCallRet)
	if len(blocks) != 2 || blocks[0].va != kCallRet || blocks[1].va != kAfter || !reflect.DeepEqual(callees, []uint32{kWrite}) {
		t.Errorf("routine blocks %d callees %X", len(blocks), callees)
	}
	if entry, ok := mm.RoutineOf(kAfter, []uint32{kWrite, kCallRet}); !ok || entry != kCallRet {
		t.Errorf("routine of the return block %X %v", entry, ok)
	}
	if _, ok := mm.RoutineOf(0x12345, []uint32{kWrite, kCallRet}); ok {
		t.Errorf("an unknown block belongs to a routine")
	}
	want := []uint32{kCallRet, kAfter, kWrite, ti.at(t, "kLoop"), ti.at(t, "kJumpInd")}
	sort.Slice(want, func(i, j int) bool { return want[i] < want[j] })
	if !reflect.DeepEqual(mm.Routines(), want) {
		t.Errorf("routines %X, want %X", mm.Routines(), want)
	}
	exprEq(t, "computed jump", blockOf(t, mm, ti.at(t, "kJumpInd")).term.index, Sym{"reg.EAX"})
	if got := mm.Dump(kWrite, kWrite); !strings.HasPrefix(got, fmt.Sprintf("block %s\n  ESP = (reg.ESP add 4)\n", key(kWrite))) || !strings.HasSuffix(got, "  jump to mem4[reg.ESP]\n") || strings.Count(got, "\n") != 4 {
		t.Errorf("dump\n%s", got)
	}
	if got := mm.Dump(ti.at(t, "kJumpInd"), ti.at(t, "kJumpInd")); !strings.Contains(got, "  jump to reg.EAX\n") {
		t.Errorf("computed jump dump\n%s", got)
	}
	if got := mm.Dump(kWrite, ti.at(t, "kLoop")); strings.Count(got, "block ") != 2 {
		t.Errorf("ranged dump\n%s", got)
	}
	loop := blockOf(t, mm, ti.at(t, "kLoop"))
	if loop.term.kind != "jump" || loop.term.target != ti.at(t, "kLoop") || !reflect.DeepEqual(loop.succs, []string{key(ti.at(t, "kLoop"))}) {
		t.Errorf("loop block %+v", loop.term)
	}
}

// Checks a store to an identity mapped address prints as that address rather than a negative offset
func TestMachineDumpKernelStoreAddress(t *testing.T) {
	ti := kernelImage(t)
	mm := machineModel(t, ti, "kWrite")
	kWrite, kvar2 := ti.at(t, "kWrite"), ti.at(t, "kvar2")
	want := fmt.Sprintf("block %s\n  ESP = (reg.ESP add 4)\n  .4@0x%X = 0x1234\n  jump to mem4[reg.ESP]\n", key(kWrite), kvar2)
	if got := mm.Dump(kWrite, kWrite); got != want {
		t.Errorf("dump\n%s\nwant\n%s", got, want)
	}
	b := blockOf(t, mm, kWrite)
	if got := effectString(b.effects[0], ExprString); got != fmt.Sprintf(".4@0x%X = 0x1234", kvar2) {
		t.Errorf("store renders as %q", got)
	}
}

func TestMachineLiftErrors(t *testing.T) {
	ti := kernelImage(t)
	mm := machineModel(t, ti, "kBad")
	why, failed := mm.Errors[key(ti.at(t, "kBad"))]
	if !failed || !strings.Contains(why, "unsupported instruction") || len(mm.blocks) != 0 {
		t.Fatalf("errors %v blocks %v", mm.Errors, keysOf(mm.blocks))
	}
	if got := mm.Dump(ti.at(t, "kBad"), ti.at(t, "kBad")); !strings.HasPrefix(got, fmt.Sprintf("block %s failed: ", key(ti.at(t, "kBad")))) {
		t.Errorf("dump %q", got)
	}
	if got := mm.Dump(0, 0x100); got != "" {
		t.Errorf("dump of an empty range %q", got)
	}
}

func TestMachineMergeAndAbsorb(t *testing.T) {
	ti := kernelImage(t)
	pool := NewExprPool()
	a, err := LiftMachineRooted(ti.d, []uint32{ti.at(t, "kWrite")}, pool)
	if err != nil {
		t.Fatal(err)
	}
	b, err := LiftMachineRooted(ti.d, []uint32{ti.at(t, "kLoop"), ti.at(t, "kBad")}, pool)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Merge(b); err != nil {
		t.Fatal(err)
	}
	if len(a.blocks) != 2 || len(a.Blocks) != 2 || len(a.Errors) != 1 || len(a.Routines()) != 3 {
		t.Errorf("merged %v %v %X", keysOf(a.blocks), a.Errors, a.Routines())
	}
	if err := a.Merge(b); err == nil || !strings.Contains(err.Error(), "lifted from two images") {
		t.Errorf("merging the same block twice: %v", err)
	}
	apart, err := LiftMachineRooted(ti.d, []uint32{ti.at(t, "kJumpInd")}, NewExprPool())
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Merge(apart); err == nil || !strings.Contains(err.Error(), "pooled apart") {
		t.Errorf("merging apart pools: %v", err)
	}
	if err := a.Absorb(apart); err == nil {
		t.Errorf("absorbing apart pools succeeded")
	}
	c, err := LiftMachineRooted(ti.d, []uint32{ti.at(t, "kWrite"), ti.at(t, "kJumpInd")}, pool)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Absorb(c); err != nil {
		t.Fatal(err)
	}
	if len(a.blocks) != 3 || a.blocks[key(ti.at(t, "kWrite"))] == c.blocks[key(ti.at(t, "kWrite"))] || a.blocks[key(ti.at(t, "kJumpInd"))] != c.blocks[key(ti.at(t, "kJumpInd"))] {
		t.Errorf("absorbed %v", keysOf(a.blocks))
	}
	d, err := LiftMachineRooted(ti.d, []uint32{ti.at(t, "kBad")}, pool)
	if err != nil {
		t.Fatal(err)
	}
	d.Errors[key(ti.at(t, "kWrite"))] = "stale"
	if err := a.Absorb(d); err != nil || a.Errors[key(ti.at(t, "kWrite"))] != "" || len(a.Errors) != 1 {
		t.Errorf("absorbing errors over lifted blocks: %v %v", err, a.Errors)
	}
}

func TestMachineFlagsArgsAndConditions(t *testing.T) {
	ex := &Executor{}
	s := &execState{fl: flagState{kind: "cmp", a: Sym{"x"}, b: IConst{3}}}
	args := ex.flagsArgs(s)
	if len(args) != 5 || args[0] != Expr(IConst{1}) || !equalExprs(args[1], Sym{"x"}) || args[2] != Expr(IConst{3}) || args[3] != Expr(IConst{0}) || args[4] != Expr(IConst{0}) {
		t.Errorf("flag arguments %v", args)
	}
	s.fl = flagState{kind: "odd"}
	if got := ex.flagsArgs(s); got[0] != Expr(IConst{0}) {
		t.Errorf("unknown kind coded %s", ExprString(got[0]))
	}
	w := Sym{"w"}
	bit := func(n uint32) Expr {
		if n == 0 {
			return Bin{"and", w, IConst{1}}
		}
		return Bin{"and", Bin{"shr", w, IConst{n}}, IConst{1}}
	}
	set := func(e Expr) Expr { return Cond{"ne", e, IConst{0}} }
	cases := map[x86asm.Op]Expr{
		x86asm.JE:  set(bit(6)),
		x86asm.JNE: Cond{"eq", bit(6), IConst{0}},
		x86asm.JB:  set(bit(0)),
		x86asm.JAE: Cond{"eq", bit(0), IConst{0}},
		x86asm.JBE: set(Bin{"or", bit(0), bit(6)}),
		x86asm.JS:  set(bit(7)),
		x86asm.JP:  set(bit(2)),
		x86asm.JO:  set(bit(11)),
		x86asm.JL:  set(Bin{"xor", bit(7), bit(11)}),
		x86asm.JLE: set(Bin{"or", bit(6), Bin{"xor", bit(7), bit(11)}}),
	}
	for op, want := range cases {
		exprEq(t, op.String(), eflagsCondition(w, op), want)
	}
	exprEq(t, "JG", eflagsCondition(w, x86asm.JG), Cond{"eq", Bin{"or", bit(6), Bin{"xor", bit(7), bit(11)}}, IConst{0}})
	exprEq(t, "JGE", eflagsCondition(w, x86asm.JGE), Cond{"eq", Bin{"xor", bit(7), bit(11)}, IConst{0}})
	exprEq(t, "folded", eflagsCondition(IConst{0x40}, x86asm.JE), IConst{1})
	exprEq(t, "folded clear", eflagsCondition(IConst{0x40}, x86asm.JB), IConst{0})
}
