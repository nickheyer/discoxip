package xbe

import (
	"reflect"
	"strings"
	"testing"
)

func newRun(t *testing.T, ti *testImage, mm *MachineModel, regs map[string]uint32) *MachineRun {
	t.Helper()
	r := NewMachineRun(mm, ti.img, regs)
	r.memo = map[exprKey]uint32{}
	r.results = map[int]uint32{}
	return r
}

func evalOf(t *testing.T, r *MachineRun, e Expr) uint32 {
	t.Helper()
	r.memo = map[exprKey]uint32{}
	v, err := r.eval(e)
	if err != nil {
		t.Fatalf("eval %s: %v", ExprString(e), err)
	}
	return v
}

func evalFails(t *testing.T, r *MachineRun, e Expr, want string) {
	t.Helper()
	r.memo = map[exprKey]uint32{}
	if _, err := r.eval(e); err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("eval %s: %v, want %q", ExprString(e), err, want)
	}
}

func TestMachineRunRegistersStartAtZero(t *testing.T) {
	ti := kernelImage(t)
	r := NewMachineRun(machineModel(t, ti), ti.img, map[string]uint32{"EAX": 7})
	if len(r.Regs) != 8 || r.Regs["EAX"] != 7 {
		t.Fatalf("registers %v", r.Regs)
	}
	for _, name := range registerNames[1:] {
		if r.Regs[name] != 0 {
			t.Errorf("%s starts at %d", name, r.Regs[name])
		}
	}
	if r.IF || r.DF || r.tsc != 0 || len(r.Trail) != 0 {
		t.Errorf("processor state not clear")
	}
}

func TestMachineRunEvalNodes(t *testing.T) {
	ti := kernelImage(t)
	r := newRun(t, ti, machineModel(t, ti), map[string]uint32{"EAX": 0x80021000, "ECX": 0xFFFFFFFE})
	r.results[1] = 99
	r.WriteU32(0x80021000, 0x11223344)
	kvar := uint32(0x80021000)
	cases := []struct {
		e    Expr
		want uint32
	}{
		{IConst{5}, 5},
		{Sym{"reg.ECX"}, 0xFFFFFFFE},
		{Ptr{"", int32(kvar)}, 0x80021000},
		{Load{Base: "", Off: int32(kvar), Size: 4}, 0x11223344},
		{Load{Base: "", Off: int32(kvar), Size: 2}, 0x3344},
		{Load{Base: "", Off: int32(kvar + 1), Size: 1}, 0x33},
		{Load{Addr: Sym{"reg.EAX"}, Size: 4}, 0x11223344},
		{CallResult{Fn: "f", Seq: 1}, 99},
		{Bin{"add", IConst{7}, IConst{9}}, 16},
		{Bin{"sub", IConst{7}, IConst{9}}, 0xFFFFFFFE},
		{Bin{"mul", IConst{7}, IConst{9}}, 63},
		{Bin{"and", IConst{0xF0}, IConst{0x3C}}, 0x30},
		{Bin{"or", IConst{0xF0}, IConst{0x0F}}, 0xFF},
		{Bin{"xor", IConst{0xFF}, IConst{0x0F}}, 0xF0},
		{Bin{"shl", IConst{1}, IConst{35}}, 8},
		{Bin{"shr", IConst{0x80000000}, IConst{31}}, 1},
		{Bin{"sar", IConst{0x80000000}, IConst{31}}, 0xFFFFFFFF},
		{Bin{"udiv", IConst{17}, IConst{5}}, 3},
		{Bin{"udiv_rem", IConst{17}, IConst{5}}, 2},
		{Bin{"idiv", IConst{0xFFFFFFEF}, IConst{5}}, 0xFFFFFFFD},
		{Bin{"idiv_rem", IConst{0xFFFFFFEF}, IConst{5}}, 0xFFFFFFFE},
		{Bin{"mulhi", IConst{0x80000000}, IConst{4}}, 2},
		{Bin{"imulhi", IConst{0xFFFFFFFF}, IConst{4}}, 0xFFFFFFFF},
		{Un{"zx8", IConst{0x1234}}, 0x34},
		{Un{"zx16", IConst{0x12345678}}, 0x5678},
		{Un{"sx8", IConst{0x80}}, 0xFFFFFF80},
		{Un{"sx16", IConst{0x8000}}, 0xFFFF8000},
		{Un{"neg", IConst{1}}, 0xFFFFFFFF},
		{Un{"not", IConst{0}}, 0xFFFFFFFF},
		{Un{"bsf", IConst{0x50}}, 4},
		{Un{"bsr", IConst{0x50}}, 6},
		{Un{"bsf", IConst{0}}, 0},
		{Un{"bsr", IConst{0}}, 0},
		{Cond{"eq", IConst{3}, IConst{3}}, 1},
		{Cond{"ne", IConst{3}, IConst{3}}, 0},
		{Cond{"lt", IConst{0xFFFFFFFF}, IConst{0}}, 1},
		{Cond{"le", IConst{2}, IConst{2}}, 1},
		{Cond{"gt", IConst{0}, IConst{0xFFFFFFFF}}, 1},
		{Cond{"ge", IConst{0xFFFFFFFF}, IConst{0}}, 0},
		{Cond{"ult", IConst{0xFFFFFFFF}, IConst{0}}, 0},
		{Cond{"ule", IConst{5}, IConst{5}}, 1},
		{Cond{"ugt", IConst{0xFFFFFFFF}, IConst{0}}, 1},
		{Cond{"uge", IConst{0}, IConst{1}}, 0},
		{Not{IConst{0}}, 1},
		{Not{IConst{7}}, 0},
		{Sel{IConst{1}, IConst{10}, IConst{20}}, 10},
		{Sel{IConst{0}, IConst{10}, IConst{20}}, 20},
	}
	for _, tc := range cases {
		if got := evalOf(t, r, tc.e); got != tc.want {
			t.Errorf("%s = %#x, want %#x", ExprString(tc.e), got, tc.want)
		}
	}
	failures := []struct {
		e    Expr
		want string
	}{
		{nil, "undefined value"},
		{Ptr{"stack", 4}, "address in stack"},
		{Load{Base: "this", Off: 4, Size: 4}, "load from this"},
		{Load{Base: "", Off: int32(kvar), Size: 8}, "load of 8 bytes"},
		{CallResult{Fn: "g", Seq: 2}, "result of g before it ran"},
		{Bin{"udiv", IConst{1}, IConst{0}}, "division by zero"},
		{Bin{"idiv_rem", IConst{1}, IConst{0}}, "division by zero"},
		{Bin{"hilo", IConst{1}, IConst{0}}, "64 bit dividend outside a division"},
		{Bin{"fadd", IConst{1}, IConst{0}}, "operation fadd"},
		{Un{"i2f", IConst{1}}, "operation i2f"},
		{Cond{"odd", IConst{1}, IConst{0}}, "comparison odd"},
		{FConst{1}, "xbe.FConst value"},
		{Sym{"reg.XYZ"}, "register XYZ"},
		{Sym{"self"}, "own address outside a copy"},
		{Sym{"segbase.FS"}, "segment FS has no base"},
		{Sym{"other"}, "input other"},
	}
	for _, tc := range failures {
		evalFails(t, r, tc.e, tc.want)
	}
	shared := Expr(Sym{"reg.EAX"})
	r.memo = map[exprKey]uint32{}
	if v, _ := r.eval(shared); v != 0x80021000 {
		t.Fatalf("first read %#x", v)
	}
	r.Regs["EAX"] = 5
	if v, _ := r.eval(shared); v != 0x80021000 {
		t.Errorf("a node read twice in one block changed to %#x", v)
	}
	if v := evalOf(t, r, shared); v != 5 {
		t.Errorf("the next block reads %#x", v)
	}
}

func TestMachineRunWideDivide(t *testing.T) {
	ti := kernelImage(t)
	r := newRun(t, ti, machineModel(t, ti), nil)
	hilo := func(hi, lo uint32) Expr { return Bin{"hilo", IConst{hi}, IConst{lo}} }
	cases := []struct {
		e    Expr
		want uint32
	}{
		{Bin{"udiv64", hilo(0, 10), IConst{3}}, 3},
		{Bin{"udiv64_rem", hilo(0, 10), IConst{3}}, 1},
		{Bin{"udiv64", hilo(1, 0), IConst{1 << 31}}, 2},
		{Bin{"udiv64_rem", hilo(1, 5), IConst{1 << 31}}, 5},
		{Bin{"idiv64", hilo(0xFFFFFFFF, 0xFFFFFFF9), IConst{2}}, 0xFFFFFFFD},
		{Bin{"idiv64_rem", hilo(0xFFFFFFFF, 0xFFFFFFF9), IConst{2}}, 0xFFFFFFFF},
		{Bin{"idiv64", hilo(0, 7), IConst{0xFFFFFFFE}}, 0xFFFFFFFD},
	}
	for _, tc := range cases {
		if got := evalOf(t, r, tc.e); got != tc.want {
			t.Errorf("%s = %#x, want %#x", ExprString(tc.e), got, tc.want)
		}
	}
	evalFails(t, r, Bin{"udiv64", hilo(5, 0), IConst{1}}, "division overflow")
	evalFails(t, r, Bin{"idiv64", hilo(0, 0x80000000), IConst{1}}, "division overflow")
	evalFails(t, r, Bin{"udiv64", hilo(0, 1), IConst{0}}, "division by zero")
	evalFails(t, r, Bin{"udiv64", IConst{1}, IConst{1}}, "no hilo pair")
}

func TestMachineRunClockAndProcessorOps(t *testing.T) {
	ti := kernelImage(t)
	r := newRun(t, ti, machineModel(t, ti), nil)
	for i := uint32(1); i <= 3; i++ {
		if v, err := r.cpu(cpuRDTSCLo, nil); err != nil || v != i {
			t.Errorf("rdtsc %d read %d %v", i, v, err)
		}
	}
	if v, _ := r.cpu(cpuRDTSCHi, nil); v != 0 {
		t.Errorf("high clock word %d", v)
	}
	r.cpu(cpuSTI, nil)
	r.cpu(cpuSTD, nil)
	if !r.IF || !r.DF {
		t.Errorf("interrupt and direction flags not set")
	}
	if w, _ := r.cpu(cpuPushFD, nil); w != 2|0x200|0x400 {
		t.Errorf("flags word %#x", w)
	}
	r.cpu(cpuCLI, nil)
	r.cpu(cpuCLD, nil)
	if r.IF || r.DF {
		t.Errorf("interrupt and direction flags not clear")
	}
	r.cpu(cpuPopFD, []uint32{0x8D5})
	if r.IF || r.DF || r.fl.kind != "eflags" || r.fl.a != 0x8D5 {
		t.Errorf("popped flags %+v IF %v DF %v", r.fl, r.IF, r.DF)
	}
	if w := r.eflags(); w != 0x8D7 {
		t.Errorf("flags word after popfd %#x", w)
	}
	r.cpu(cpuPopFD, []uint32{0x600})
	if !r.IF || !r.DF {
		t.Errorf("popfd did not set the interrupt and direction flags")
	}
	if _, err := r.cpu(portOut, []uint32{0x80, 1, 1}); err == nil || !strings.Contains(err.Error(), "processor operation port_out") {
		t.Errorf("port write: %v", err)
	}
	if _, err := r.cpu(portIn, []uint32{0x80, 1}); err == nil || !strings.Contains(err.Error(), "processor operation port_in") {
		t.Errorf("port read: %v", err)
	}
	buf := uint32(0x80021000)
	r.Preload(buf, []byte{1, 2, 0x33, 4, 5, 6, 7, 8})
	if n, _ := r.cpu(cpuSCAS, []uint32{buf, 0x33, 8, 1, 1, 1}); n != 3 {
		t.Errorf("repne scasb compared %d elements, want 3", n)
	}
	if n, _ := r.cpu(cpuSCAS, []uint32{buf, 1, 8, 1, 1, 0}); n != 2 {
		t.Errorf("repe scasb compared %d elements, want 2", n)
	}
	if n, _ := r.cpu(cpuSCAS, []uint32{buf, 0x99, 8, 1, 1, 1}); n != 8 {
		t.Errorf("repne scasb without a match compared %d elements, want 8", n)
	}
	r.Preload(buf+16, []byte{1, 2, 9, 4})
	if n, _ := r.cpu(cpuCMPS, []uint32{buf, buf + 16, 4, 1, 1, 0}); n != 3 {
		t.Errorf("repe cmpsb compared %d elements, want 3", n)
	}
	if n, _ := r.cpu(cpuCMPS, []uint32{buf, buf + 16, 2, 1, 1, 0}); n != 2 {
		t.Errorf("repe cmpsb over equal bytes compared %d elements, want 2", n)
	}
	if n, _ := r.cpu(cpuCMPS, []uint32{buf, buf + 16, 4, 1, 1, 1}); n != 1 {
		t.Errorf("repne cmpsb compared %d elements, want 1", n)
	}
}

func TestMachineRunFlagsAndConditions(t *testing.T) {
	ti := kernelImage(t)
	r := newRun(t, ti, machineModel(t, ti), nil)
	check := func(fl runFlags, want map[string]bool) {
		t.Helper()
		r.fl = fl
		for op, taken := range want {
			got, err := r.condition(op)
			if err != nil || got != taken {
				t.Errorf("%s after %s: %v %v", op, fl.kind, got, err)
			}
		}
	}
	check(runFlags{kind: "cmp", a: 3, b: 5, r: 0xFFFFFFFE}, map[string]bool{"JE": false, "JNE": true, "JL": true, "JGE": false, "JLE": true, "JG": false, "JB": true, "JAE": false, "JBE": true, "JA": false, "JS": true, "JNS": false})
	check(runFlags{kind: "cmp", a: 5, b: 5, r: 0}, map[string]bool{"JE": true, "JLE": true, "JGE": true, "JBE": true, "JAE": true, "JA": false, "JB": false})
	check(runFlags{kind: "cmp", a: 0x80000000, b: 1, r: 0x7FFFFFFF}, map[string]bool{"JL": true, "JB": false, "JA": true, "JG": false})
	check(runFlags{kind: "alu", a: 1, b: 1, r: 0x80000000, c: 1}, map[string]bool{"JE": false, "JS": true, "JL": true, "JB": true, "JBE": true, "JA": false, "JAE": false, "JG": false, "JLE": true})
	check(runFlags{kind: "add", a: 0xFFFFFFFF, b: 1, r: 0}, map[string]bool{"JE": true, "JB": true, "JAE": false})
	check(runFlags{kind: "test", a: 3, b: 4, r: 0}, map[string]bool{"JE": true, "JB": false, "JA": false, "JBE": true})
	check(runFlags{kind: "eflags", a: 0x40}, map[string]bool{"JE": true, "JNE": false, "JB": false, "JAE": true, "JBE": true, "JA": false, "JL": false, "JGE": true, "JLE": true, "JG": false})
	check(runFlags{kind: "eflags", a: 0x80 | 0x800}, map[string]bool{"JL": false, "JGE": true, "JG": true})
	check(runFlags{kind: "eflags", a: 0x80}, map[string]bool{"JL": true, "JLE": true})
	r.fl = runFlags{kind: "eflags", a: 0x80}
	if _, err := r.condition("JS"); err == nil || !strings.Contains(err.Error(), "condition JS on a flags word") {
		t.Errorf("sign on a flags word: %v", err)
	}
	r.fl = runFlags{kind: "cmp"}
	if _, err := r.condition("JP"); err == nil || !strings.Contains(err.Error(), "condition JP after cmp flags") {
		t.Errorf("parity after a compare: %v", err)
	}
	r.fl = runFlags{kind: "cmp", a: 5, b: 5, r: 0}
	if w := r.eflags(); w != 2|0x40|4 {
		t.Errorf("flags word after an equal compare %#x", w)
	}
	r.fl = runFlags{kind: "add", a: 0xFFFFFFFF, b: 1, r: 0}
	if w := r.eflags(); w != 2|0x40|4|1 {
		t.Errorf("flags word after a wrapping add %#x", w)
	}
	r.fl = runFlags{kind: "alu", r: 0x80000001, c: 1}
	if w := r.eflags(); w != 2|0x80|1 {
		t.Errorf("flags word after a negative odd result %#x", w)
	}
	r.fl = runFlags{kind: "sub", a: 1, b: 2, r: 0xFFFFFFFF}
	if !r.carry() || r.eflags()&0x40 != 0 || r.eflags()&4 != 4 {
		t.Errorf("borrowing subtraction flags %#x", r.eflags())
	}
	r.fl = runFlags{kind: "cmp", a: 7, b: 7, r: 0}
	for sym, want := range map[string]uint32{"flags.a": 7, "flags.b": 7, "flags.r": 0, "flags.cf": 0, "cond.JE": 1, "cond.JNE": 0, "dfstep4": 4, "dfstep1": 1} {
		if got := evalOf(t, r, Sym{sym}); got != want {
			t.Errorf("%s = %d, want %d", sym, got, want)
		}
	}
	r.DF = true
	if got := evalOf(t, r, Sym{"dfstep4"}); got != 0xFFFFFFFC {
		t.Errorf("backward step %#x", got)
	}
	r.Segments["FS"] = 0x80021000
	if got := evalOf(t, r, Sym{"segbase.FS"}); got != 0x80021000 {
		t.Errorf("segment base %#x", got)
	}
}

func TestMachineRunMemory(t *testing.T) {
	ti := kernelImage(t)
	r := newRun(t, ti, machineModel(t, ti), map[string]uint32{"ESP": 0x80030000})
	entry := ti.at(t, "kEntry")
	if r.ReadU8(entry) != 0xB8 || r.ReadU32(entry) != 0x000001B8 {
		t.Errorf("image bytes read as %#x %#x", r.ReadU8(entry), r.ReadU32(entry))
	}
	if r.ReadU8(0x12345678) != 0 {
		t.Errorf("unmapped memory reads nonzero")
	}
	r.WriteU32(entry, 0xAABBCCDD)
	if r.ReadU32(entry) != 0xAABBCCDD || r.ReadU8(entry+3) != 0xAA || !reflect.DeepEqual(r.Bytes(entry, 6), []byte{0xDD, 0xCC, 0xBB, 0xAA, 0x00, 0xC3}) {
		t.Errorf("written bytes read back wrong")
	}
	r.Preload(0x80021000, []byte{'H', 0, 'i', 0, 0, 0})
	if r.WideString(0x80021000) != "Hi" || r.WideString(0x80021004) != "" {
		t.Errorf("wide strings %q %q", r.WideString(0x80021000), r.WideString(0x80021004))
	}
	written := r.Written()
	if len(written) != 10 || written[entry] != 0xDD || written[0x80021002] != 'i' {
		t.Errorf("written bytes %v", written)
	}
	written[entry] = 0
	if r.ReadU8(entry) != 0xDD {
		t.Errorf("the written map is not a copy")
	}
	r.Push(0x11223344)
	r.Push(0x55667788)
	if r.Regs["ESP"] != 0x80030000-8 || r.ReadU32(0x80030000-8) != 0x55667788 || r.ReadU32(0x80030000-4) != 0x11223344 {
		t.Errorf("pushes landed wrong")
	}
	if r.Pop() != 0x55667788 || r.Pop() != 0x11223344 || r.Regs["ESP"] != 0x80030000 {
		t.Errorf("pops read wrong")
	}
	other := newRun(t, ti, machineModel(t, ti), nil)
	other.Adopt(r)
	if other.ReadU32(entry) != 0xAABBCCDD || other.WideString(0x80021000) != "Hi" || len(other.Written()) != len(r.Written()) {
		t.Errorf("adopted memory differs")
	}
	other.WriteU32(entry, 1)
	if r.ReadU32(entry) != 0xAABBCCDD {
		t.Errorf("adopting shares memory")
	}
}

func TestMachineRunRoutine(t *testing.T) {
	ti := kernelImage(t)
	mm := machineModel(t, ti, "kWrite", "kLoop", "kBad")
	kWrite, kvar2 := ti.at(t, "kWrite"), ti.at(t, "kvar2")
	r := NewMachineRun(mm, ti.img, map[string]uint32{"ESP": 0x80030000})
	r.Push(0xDEAD0000)
	r.Stop[0xDEAD0000] = true
	next, err := r.Run(kWrite, 10)
	if err != nil || next != 0xDEAD0000 {
		t.Fatalf("run stopped at %X: %v", next, err)
	}
	if r.ReadU32(kvar2) != 0x1234 || r.Regs["ESP"] != 0x80030000 || !reflect.DeepEqual(r.Trail, []uint32{kWrite}) {
		t.Errorf("after the routine kvar2=%#x esp=%X trail %X", r.ReadU32(kvar2), r.Regs["ESP"], r.Trail)
	}
	if _, err := r.Run(ti.at(t, "kLoop"), 5); err == nil || !strings.Contains(err.Error(), "5 blocks without reaching a stop") {
		t.Errorf("loop: %v", err)
	}
	if _, err := r.Run(ti.at(t, "kBad"), 5); err == nil || !strings.Contains(err.Error(), "was not lifted") {
		t.Errorf("failed block: %v", err)
	}
	if _, err := r.Run(0x12345, 5); err == nil || !strings.Contains(err.Error(), "no block 00012345") {
		t.Errorf("unknown block: %v", err)
	}
}

// Checks a missing block is lifted on demand and a hooked routine is answered by the hook
func TestMachineRunMissingAndHooks(t *testing.T) {
	ti := kernelImage(t)
	mm := machineModel(t, ti, "kWrite")
	kCallRet, kWrite, kAfter, kvar2 := ti.at(t, "kCallRet"), ti.at(t, "kWrite"), ti.at(t, "kCallRetAfter"), ti.at(t, "kvar2")
	r := NewMachineRun(mm, ti.img, map[string]uint32{"ESP": 0x80030000})
	r.Push(0xDEAD0000)
	r.Stop[0xDEAD0000] = true
	if _, err := r.Run(kCallRet, 10); err == nil || !strings.Contains(err.Error(), "no block") {
		t.Fatalf("without a lifter: %v", err)
	}
	r = NewMachineRun(mm, ti.img, map[string]uint32{"ESP": 0x80030000})
	r.Push(0xDEAD0000)
	r.Stop[0xDEAD0000] = true
	lifted := 0
	r.Missing = func(r *MachineRun, va uint32) error {
		lifted++
		other, err := LiftMachineRooted(ti.d, []uint32{va}, mm.Exprs)
		if err != nil {
			return err
		}
		return mm.Absorb(other)
	}
	next, err := r.Run(kCallRet, 10)
	if err != nil || next != 0xDEAD0000 || lifted != 1 {
		t.Fatalf("with a lifter stopped at %X after %d lifts: %v", next, lifted, err)
	}
	if _, has := mm.blocks[key(kAfter)]; !has {
		t.Errorf("the on demand lift did not walk to the return block")
	}
	if !reflect.DeepEqual(r.Trail, []uint32{kCallRet, kWrite, kAfter}) || r.ReadU32(kvar2) != 0x1234 || r.Regs["ESP"] != 0x80030000 {
		t.Errorf("trail %X kvar2 %#x esp %X", r.Trail, r.ReadU32(kvar2), r.Regs["ESP"])
	}
	r = NewMachineRun(mm, ti.img, map[string]uint32{"ESP": 0x80030000})
	r.Push(0xDEAD0000)
	r.Stop[0xDEAD0000] = true
	r.Hooks[kWrite] = func(r *MachineRun) uint32 { return r.Pop() }
	next, err = r.Run(kCallRet, 10)
	if err != nil || next != 0xDEAD0000 || r.ReadU32(kvar2) != 0 || !reflect.DeepEqual(r.Trail, []uint32{kCallRet, kAfter}) {
		t.Errorf("hooked run stopped at %X with kvar2 %#x trail %X: %v", next, r.ReadU32(kvar2), r.Trail, err)
	}
	r = NewMachineRun(mm, ti.img, map[string]uint32{"ESP": 0x80030000})
	r.Stop[kWrite] = true
	if next, err := r.Run(kCallRet, 10); err != nil || next != kWrite || !reflect.DeepEqual(r.Trail, []uint32{kCallRet}) {
		t.Errorf("stopping at the callee: %X %v", next, err)
	}
}

func TestMachineRunConditionSymbols(t *testing.T) {
	ti := kernelImage(t)
	mm := machineModel(t, ti, "kCmp", "kCondRoot", "kFlagsJump")
	for ecx, want := range map[uint32]uint32{7: 2, 6: 1} {
		r := NewMachineRun(mm, ti.img, map[string]uint32{"ESP": 0x80030000, "ECX": ecx})
		r.Push(0xDEAD0000)
		r.Push(ti.at(t, "kCondRoot"))
		r.Stop[0xDEAD0000] = true
		if next, err := r.Run(ti.at(t, "kCmp"), 10); err != nil || next != 0xDEAD0000 {
			t.Fatalf("ecx %d: stopped at %X: %v", ecx, next, err)
		}
		if r.Regs["EAX"] != want || r.fl.kind != "cmp" || r.fl.a != ecx || r.fl.b != 7 {
			t.Errorf("ecx %d returned %d with flags %+v", ecx, r.Regs["EAX"], r.fl)
		}
		r = NewMachineRun(mm, ti.img, map[string]uint32{"ESP": 0x80030000, "ECX": ecx})
		r.Push(0xDEAD0000)
		r.Stop[0xDEAD0000] = true
		if next, err := r.Run(ti.at(t, "kFlagsJump"), 10); err != nil || next != 0xDEAD0000 {
			t.Fatalf("ecx %d through flags: stopped at %X: %v", ecx, next, err)
		}
		if r.Regs["EAX"] != want {
			t.Errorf("ecx %d through flags returned %d", ecx, r.Regs["EAX"])
		}
	}
}

func runRoutine(t *testing.T, ti *testImage, mm *MachineModel, label string, regs map[string]uint32, prepare func(r *MachineRun)) *MachineRun {
	t.Helper()
	if regs == nil {
		regs = map[string]uint32{}
	}
	regs["ESP"] = 0x80030000
	r := NewMachineRun(mm, ti.img, regs)
	if prepare != nil {
		prepare(r)
	}
	r.Push(0xDEAD0000)
	r.Stop[0xDEAD0000] = true
	if next, err := r.Run(ti.at(t, label), 10); err != nil || next != 0xDEAD0000 {
		t.Fatalf("%s stopped at %X: %v", label, next, err)
	}
	return r
}

func TestMachineRunStringEffects(t *testing.T) {
	ti := kernelImage(t)
	mm := machineModel(t, ti, "kCopy", "kFill", "kMovsd", "kMovsdCld", "kScas", "kCmps")
	src, dst := ti.at(t, "kbuf"), ti.at(t, "kbuf")+16
	payload := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}
	r := runRoutine(t, ti, mm, "kCopy", map[string]uint32{"ESI": src, "EDI": dst, "ECX": 3}, func(r *MachineRun) { r.Preload(src, payload) })
	if !reflect.DeepEqual(r.Bytes(dst, 12), payload) || r.Regs["ESI"] != src+12 || r.Regs["EDI"] != dst+12 || r.Regs["ECX"] != 0 || r.DF {
		t.Errorf("copy left %v esi %X edi %X ecx %d", r.Bytes(dst, 12), r.Regs["ESI"], r.Regs["EDI"], r.Regs["ECX"])
	}
	r = runRoutine(t, ti, mm, "kFill", map[string]uint32{"EAX": 0xAABBCCDD, "EDI": dst, "ECX": 2}, nil)
	if !reflect.DeepEqual(r.Bytes(dst, 9), []byte{0xDD, 0xCC, 0xBB, 0xAA, 0xDD, 0xCC, 0xBB, 0xAA, 0}) || r.Regs["EDI"] != dst+8 {
		t.Errorf("fill left %v edi %X", r.Bytes(dst, 9), r.Regs["EDI"])
	}
	b := blockOf(t, mm, ti.at(t, "kMovsd"))
	exprEq(t, "step before any direction", b.regs[6], Bin{"add", Sym{"reg.ESI"}, Sym{"dfstep4"}})
	exprEq(t, "fixed step after cld", blockOf(t, mm, ti.at(t, "kMovsdCld")).regs[6], Bin{"add", Sym{"reg.ESI"}, IConst{4}})
	r = runRoutine(t, ti, mm, "kMovsd", map[string]uint32{"ESI": src, "EDI": dst}, func(r *MachineRun) { r.DF = true })
	if r.Regs["ESI"] != src-4 || r.Regs["EDI"] != dst-4 {
		t.Errorf("backward move left esi %X edi %X", r.Regs["ESI"], r.Regs["EDI"])
	}
	r = runRoutine(t, ti, mm, "kMovsd", map[string]uint32{"ESI": src, "EDI": dst}, nil)
	if r.Regs["ESI"] != src+4 || r.Regs["EDI"] != dst+4 {
		t.Errorf("forward move left esi %X edi %X", r.Regs["ESI"], r.Regs["EDI"])
	}
	r = runRoutine(t, ti, mm, "kScas", map[string]uint32{"EAX": 0x33, "EDI": src, "ECX": 8}, func(r *MachineRun) { r.Preload(src, []byte{1, 2, 0x33, 4, 5, 6, 7, 8}) })
	if r.Regs["ECX"] != 5 || r.Regs["EDI"] != src+3 || r.fl.kind != "cmp" || r.fl.a != 0x33 || r.fl.b != 0x33 {
		t.Errorf("scan left ecx %d edi %X flags %+v", r.Regs["ECX"], r.Regs["EDI"], r.fl)
	}
	r = runRoutine(t, ti, mm, "kCmps", map[string]uint32{"ESI": src, "EDI": dst, "ECX": 4}, func(r *MachineRun) {
		r.Preload(src, []byte{1, 2, 3, 4})
		r.Preload(dst, []byte{1, 2, 9, 4})
	})
	if r.Regs["ECX"] != 1 || r.Regs["ESI"] != src+3 || r.Regs["EDI"] != dst+3 || r.fl.a != 3 || r.fl.b != 9 {
		t.Errorf("compare left ecx %d esi %X edi %X flags %+v", r.Regs["ECX"], r.Regs["ESI"], r.Regs["EDI"], r.fl)
	}
}

func TestMachineRunProcessorEffects(t *testing.T) {
	ti := kernelImage(t)
	mm := machineModel(t, ti, "kRdtsc", "kDiv", "kSeg", "kPort", "kCli", "kPushf", "kPopf")
	kvar, kvar2 := ti.at(t, "kvar"), ti.at(t, "kvar2")
	r := runRoutine(t, ti, mm, "kRdtsc", nil, nil)
	if r.ReadU32(kvar) != 1 || r.ReadU32(kvar2) != 2 || r.Regs["EDX"] != 0 {
		t.Errorf("clock reads %d then %d", r.ReadU32(kvar), r.ReadU32(kvar2))
	}
	b := blockOf(t, mm, ti.at(t, "kDiv"))
	exprEq(t, "quotient", b.regs[0], Bin{"udiv64", Bin{"hilo", Sym{"reg.EDX"}, Sym{"reg.EAX"}}, Sym{"reg.ECX"}})
	r = runRoutine(t, ti, mm, "kDiv", map[string]uint32{"EAX": 10, "EDX": 0, "ECX": 3}, nil)
	if r.Regs["EAX"] != 3 || r.Regs["EDX"] != 1 {
		t.Errorf("division gave %d remainder %d", r.Regs["EAX"], r.Regs["EDX"])
	}
	r = NewMachineRun(mm, ti.img, map[string]uint32{"ESP": 0x80030000, "EAX": 10, "EDX": 5, "ECX": 1})
	if _, err := r.Run(ti.at(t, "kDiv"), 10); err == nil || !strings.Contains(err.Error(), "division overflow") {
		t.Errorf("overflowing division: %v", err)
	}
	r = runRoutine(t, ti, mm, "kSeg", nil, func(r *MachineRun) {
		r.Segments["FS"] = 0x80021000
		r.WriteU32(0x80021020, 0x5A5A5A5A)
	})
	if r.Regs["EAX"] != 0x5A5A5A5A {
		t.Errorf("segment relative read %#x", r.Regs["EAX"])
	}
	r = NewMachineRun(mm, ti.img, map[string]uint32{"ESP": 0x80030000})
	if _, err := r.Run(ti.at(t, "kSeg"), 10); err == nil || !strings.Contains(err.Error(), "segment FS has no base") {
		t.Errorf("segment without a base: %v", err)
	}
	r = NewMachineRun(mm, ti.img, map[string]uint32{"ESP": 0x80030000})
	if _, err := r.Run(ti.at(t, "kPort"), 10); err == nil || !strings.Contains(err.Error(), "processor operation port_out") {
		t.Errorf("port write: %v", err)
	}
	r = runRoutine(t, ti, mm, "kCli", nil, func(r *MachineRun) { r.IF = true })
	if r.IF {
		t.Errorf("cli left interrupts enabled")
	}
	r = runRoutine(t, ti, mm, "kPushf", nil, func(r *MachineRun) { r.IF = true })
	if r.Regs["EAX"] != 0x202 {
		t.Errorf("pushed flags word %#x", r.Regs["EAX"])
	}
	r = runRoutine(t, ti, mm, "kPopf", map[string]uint32{"EAX": 0x602}, nil)
	if !r.IF || !r.DF || r.fl.kind != "eflags" || r.fl.a != 0x602 {
		t.Errorf("popped flags IF %v DF %v %+v", r.IF, r.DF, r.fl)
	}
}

func TestMachineRunEffectKinds(t *testing.T) {
	ti := kernelImage(t)
	r := newRun(t, ti, machineModel(t, ti), map[string]uint32{"EAX": 0x80021000})
	kvar := uint32(0x80021000)
	if err := r.effect(Effect{Kind: "store", Base: "this", Off: 4, Size: 4, Value: IConst{1}}); err == nil || !strings.Contains(err.Error(), "store to this") {
		t.Errorf("based store: %v", err)
	}
	if err := r.effect(Effect{Kind: "store", Off: int32(kvar), Size: 8, Value: IConst{1}}); err == nil || !strings.Contains(err.Error(), "store of 8 bytes") {
		t.Errorf("wide store: %v", err)
	}
	if err := r.effect(Effect{Kind: "store", Addr: Sym{"reg.EAX"}, Size: 2, Value: IConst{0x1234}}); err != nil || r.ReadU32(0x80021000) != 0x1234 {
		t.Errorf("store through a register: %v %#x", err, r.ReadU32(0x80021000))
	}
	if err := r.effect(Effect{Kind: "fill", Addr: IConst{0x80021010}, Size: 1, Value: IConst{0xAB}, Args: []Expr{IConst{3}, IConst{1}}}); err != nil || !reflect.DeepEqual(r.Bytes(0x80021010, 4), []byte{0xAB, 0xAB, 0xAB, 0}) {
		t.Errorf("fill: %v %v", err, r.Bytes(0x80021010, 4))
	}
	if err := r.effect(Effect{Kind: "copy", Addr: IConst{0x80021020}, Size: 1, Value: IConst{3}, Args: []Expr{IConst{0x80021010}, IConst{1}}}); err != nil || !reflect.DeepEqual(r.Bytes(0x80021020, 4), []byte{0xAB, 0xAB, 0xAB, 0}) {
		t.Errorf("copy: %v %v", err, r.Bytes(0x80021020, 4))
	}
	if err := r.effect(Effect{Kind: "call", Fn: cpuRDTSCLo, Seq: 3}); err != nil || r.results[3] != 1 {
		t.Errorf("call: %v %v", err, r.results)
	}
	if err := r.effect(Effect{Kind: "call", Fn: portOut, Seq: 4, Args: []Expr{IConst{0x80}, IConst{1}, IConst{1}}}); err == nil || !strings.Contains(err.Error(), "processor operation port_out") {
		t.Errorf("port call: %v", err)
	}
	if err := r.effect(Effect{Kind: "mystery"}); err == nil || !strings.Contains(err.Error(), "mystery effect") {
		t.Errorf("unknown kind: %v", err)
	}
}
