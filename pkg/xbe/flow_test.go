package xbe

import (
	"testing"

	"golang.org/x/arch/x86/x86asm"
)

func heldAddresses(states []map[x86asm.Reg]uint32, i int) map[x86asm.Reg]uint32 {
	out := map[x86asm.Reg]uint32{}
	for r, a := range states[i] {
		out[r] = a
	}
	return out
}

func sameHeld(got, want map[x86asm.Reg]uint32) bool {
	if len(got) != len(want) {
		return false
	}
	for r, a := range want {
		if got[r] != a {
			return false
		}
	}
	return true
}

func TestInstructionIndex(t *testing.T) {
	d, at := analysis(t)
	sw := mustFunction(t, d, at["switcher"])
	idx := sw.instructionIndex()
	if len(idx) != 12 || idx[at["switcher"]] != 0 || idx[at["switchJump"]] != 3 || idx[at["case0"]] != 4 || idx[at["default"]] != 10 {
		t.Fatalf("index %v", idx)
	}
	if _, present := idx[at["switcher"]+1]; present {
		t.Fatal("mid instruction address indexed")
	}
}

func TestSuccessors(t *testing.T) {
	d, at := analysis(t)
	succ := func(fn *Function, va uint32) []uint32 {
		idx := fn.instructionIndex()
		var out []uint32
		for _, j := range fn.successors(idx[va], idx) {
			out = append(out, fn.Instructions[j].VA)
		}
		return out
	}
	sw := mustFunction(t, d, at["switcher"])
	if got := succ(sw, at["switcher"]+4); !equalU32(got, []uint32{at["switcher"] + 7}) {
		t.Errorf("compare successors %x", got)
	}
	if got := succ(sw, at["switcher"]+7); !equalU32(got, []uint32{at["default"], at["switchJump"]}) {
		t.Errorf("conditional successors %x", got)
	}
	if got := succ(sw, at["switchJump"]); !equalU32(got, []uint32{at["case0"], at["case1"], at["case2"]}) {
		t.Errorf("table successors %x", got)
	}
	if got := succ(sw, at["case0"]+5); got != nil {
		t.Errorf("return successors %x", got)
	}
	entry := mustFunction(t, d, at["entry"])
	if got := succ(entry, at["entry"]+5); !equalU32(got, []uint32{at["entry"] + 10}) {
		t.Errorf("call successors %x", got)
	}
	memcpy := mustFunction(t, d, at["memcpy"])
	if got := succ(memcpy, at["backJump"]); !equalU32(got, []uint32{at["aligned"]}) {
		t.Errorf("unconditional successors %x", got)
	}
	cookie := mustFunction(t, d, at["cookieCheck"])
	if got := succ(cookie, at["cookieCheck"]+6); !equalU32(got, []uint32{at["cookieCheck"] + 9, at["cookieCheck"] + 8}) {
		t.Errorf("jne successors %x", got)
	}
	if got := succ(cookie, at["cookieCheck"]+9); got != nil {
		t.Errorf("trap successors %x", got)
	}
	spin := mustFunction(t, d, at["spin"])
	if got := succ(spin, at["spin"]); !equalU32(got, []uint32{at["spin"]}) {
		t.Errorf("self jump successors %x", got)
	}
}

func TestStaticAddressFlow(t *testing.T) {
	d, at := analysis(t)
	flow := mustFunction(t, d, at["flow"])
	if len(flow.Instructions) != 9 {
		t.Fatalf("flow %v", opsOf(flow))
	}
	states := d.staticAddressFlow(flow)
	want := []map[x86asm.Reg]uint32{
		{},
		{x86asm.ESI: 0x12100},
		{x86asm.ESI: 0x12100, x86asm.EDI: 0x12100},
		{x86asm.ESI: 0x12100, x86asm.EDI: 0x12100},
		{x86asm.ESI: 0x12100, x86asm.EDI: 0x12100},
		{x86asm.ESI: 0x12100, x86asm.EDI: 0x12100},
		{x86asm.ESI: 0x12100, x86asm.EDI: 0x12110},
		{x86asm.ESI: 0x12100},
		{x86asm.ESI: 0x12100},
	}
	for i, w := range want {
		if got := heldAddresses(states, i); !sameHeld(got, w) {
			t.Errorf("before %d (%v) held %v, want %v", i, flow.Instructions[i].Inst.Op, got, w)
		}
	}
	seeded := d.staticAddressFlowFrom(flow, map[x86asm.Reg]uint32{x86asm.EBX: 0x12104, x86asm.ECX: 0x12108})
	if got := heldAddresses(seeded, 7); !sameHeld(got, map[x86asm.Reg]uint32{x86asm.ESI: 0x12100, x86asm.EBX: 0x12104, x86asm.ECX: 0x12108}) {
		t.Errorf("seeded join held %v", got)
	}
	if got := heldAddresses(seeded, 5); !sameHeld(got, map[x86asm.Reg]uint32{x86asm.ESI: 0x12100, x86asm.EDI: 0x12100, x86asm.EBX: 0x12104, x86asm.ECX: 0x12108}) {
		t.Errorf("seeded branch held %v", got)
	}
	orphan := &Function{EntryVA: 0x15000, Instructions: flow.Instructions}
	if got := d.staticAddressFlow(orphan); len(got) != 9 || got[0] != nil {
		t.Fatal("entry outside the instructions computed a flow")
	}
}

func TestGlobalLoadFlow(t *testing.T) {
	d, at := analysis(t)
	loads := mustFunction(t, d, at["loads"])
	if !equalStrings(opsOf(loads), []string{"MOV", "MOV", "MOV", "ADD", "RET"}) {
		t.Fatalf("loads %v", opsOf(loads))
	}
	states := d.globalLoadFlow(loads)
	want := []map[x86asm.Reg]uint32{
		{},
		{x86asm.EAX: 0x12100},
		{x86asm.EAX: 0x12100, x86asm.ECX: 0x12100},
		{x86asm.EAX: 0x12100, x86asm.ECX: 0x12100, x86asm.EDX: 0x12104},
		{x86asm.ECX: 0x12100, x86asm.EDX: 0x12104},
	}
	for i, w := range want {
		if got := heldAddresses(states, i); !sameHeld(got, w) {
			t.Errorf("before %d held %v, want %v", i, got, w)
		}
	}
	flow := mustFunction(t, d, at["flow"])
	if got := heldAddresses(d.globalLoadFlow(flow), 1); len(got) != 0 {
		t.Errorf("immediate counted as a load %v", got)
	}
}

func TestLiveInRegisters(t *testing.T) {
	d, at := analysis(t)
	live := mustFunction(t, d, at["live"])
	if got := d.liveInRegisters(live); !equalRegs(got, []x86asm.Reg{x86asm.ECX, x86asm.EDX, x86asm.EDI}) {
		t.Fatalf("live in %v", got)
	}
	if got := d.liveInFloatRegisters(live); len(got) != 0 {
		t.Fatalf("float live in %v", got)
	}
	if got := d.liveInRegisters(live); !equalRegs(got, []x86asm.Reg{x86asm.ECX, x86asm.EDX, x86asm.EDI}) {
		t.Fatalf("cached live in %v", got)
	}
	for _, name := range []string{"args", "cdeclUser", "target", "switcher", "entry"} {
		if got := d.liveInRegisters(mustFunction(t, d, at[name])); len(got) != 0 {
			t.Errorf("%s live in %v", name, got)
		}
	}
	if got := d.liveInRegisters(mustFunction(t, d, at["memcpy"])); len(got) != 0 {
		t.Errorf("memcpy live in %v", got)
	}
	if got := d.liveInRegisters(mustFunction(t, d, at["chkstk"])); !equalRegs(got, []x86asm.Reg{x86asm.EAX}) {
		t.Errorf("probe live in %v", got)
	}
	if got := d.liveInRegisters(mustFunction(t, d, at["cookieCheck"])); !equalRegs(got, []x86asm.Reg{x86asm.ECX}) {
		t.Errorf("cookie check live in %v", got)
	}
}

func TestSavingPush(t *testing.T) {
	d, at := analysis(t)
	live := mustFunction(t, d, at["live"])
	base := at["live"]
	cases := map[uint32]bool{base: true, base + 1: true, base + 8: false, base + 9: false}
	for va, want := range cases {
		if got := d.savingPush(live, va); got != want {
			t.Errorf("push at %08X saving %v, want %v", va, got, want)
		}
	}
	args := mustFunction(t, d, at["args"])
	if !d.savingPush(args, at["args"]) {
		t.Error("frame pointer push not a save")
	}
	entry := mustFunction(t, d, at["entry"])
	if d.savingPush(entry, at["entry"]+3) {
		t.Error("immediate push a save")
	}
}

func equalRegs(a, b []x86asm.Reg) bool {
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

func TestStackInputs(t *testing.T) {
	d, at := analysis(t)
	cases := map[string]int{"args": 2, "switcher": 1, "memcpy": 3, "memset": 3, "probeUser": 1, "frames": 3, "cdeclUser": 0, "entry": 0, "target": 0, "decomp": 1}
	for name, want := range cases {
		if got := d.stackInputs(mustFunction(t, d, at[name])); got != want {
			t.Errorf("%s reads %d stack arguments, want %d", name, got, want)
		}
	}
	if got := d.stackInputs(mustFunction(t, d, at["args"])); got != 2 {
		t.Errorf("cached count %d", got)
	}
}

func TestFrameAfter(t *testing.T) {
	d, at := analysis(t)
	frames := mustFunction(t, d, at["frames"])
	if len(frames.Instructions) != 10 {
		t.Fatalf("frames %v", opsOf(frames))
	}
	type step struct {
		esp, ebp int32
		espKnown bool
		ebpKnown bool
		pushes   int32
	}
	want := []step{
		{4, 0, true, false, 1},
		{4, 4, true, true, 1},
		{20, 4, true, true, 1},
		{24, 4, true, true, 2},
		{24, 4, true, true, 2},
		{8, 4, true, true, 2},
		{4, 4, true, true, 2},
		{4, 4, true, true, 2},
		{0, 4, true, false, 2},
		{0, 4, true, false, 2},
	}
	f := stackFrame{espKnown: true}
	for i, w := range want {
		f = d.frameAfter(frames, i, f)
		if f.esp != w.esp || f.ebp != w.ebp || f.espKnown != w.espKnown || f.ebpKnown != w.ebpKnown || f.pushes != w.pushes {
			t.Errorf("after %d (%v) frame %+v, want %+v", i, frames.Instructions[i].Inst.Op, f, w)
		}
	}
	probe := mustFunction(t, d, at["probeUser"])
	f = stackFrame{espKnown: true}
	for i := range probe.Instructions {
		f = d.frameAfter(probe, i, f)
		if probe.Instructions[i].Inst.Op == x86asm.CALL && (f.esp != 4+0x2000 || f.pushes != 0) {
			t.Errorf("after the probe call frame %+v", f)
		}
	}
	if f.esp != 0 || f.ebpKnown {
		t.Errorf("probe user leaves %+v", f)
	}
	user := mustFunction(t, d, at["cdeclUser"])
	f = stackFrame{espKnown: true}
	var trace []int32
	for i := range user.Instructions {
		f = d.frameAfter(user, i, f)
		trace = append(trace, f.esp)
	}
	if want := []int32{4, 8, 8, 0, 4, 0, -8, -8, -8}; !equalI32(trace, want) {
		t.Errorf("cdecl user stack trace %v, want %v", trace, want)
	}
	single := func(seq []byte, in stackFrame) stackFrame {
		return d.frameAfter(functionOf(t, 0x11000, seq), 0, in)
	}
	if f := single([]byte{0xC9}, stackFrame{esp: 20, ebp: 4, espKnown: true, ebpKnown: true}); f.esp != 0 || !f.espKnown || f.ebpKnown {
		t.Errorf("leave %+v", f)
	}
	if f := single([]byte{0x83, 0xC4, 0x08}, stackFrame{esp: 12, espKnown: true}); f.esp != 4 {
		t.Errorf("add esp %+v", f)
	}
	if f := single([]byte{0x2B, 0xE0}, stackFrame{esp: 12, espKnown: true}); f.espKnown {
		t.Errorf("sub esp by register %+v", f)
	}
	if f := single([]byte{0x8B, 0xE0}, stackFrame{esp: 12, espKnown: true}); f.espKnown {
		t.Errorf("mov esp from register %+v", f)
	}
	if f := single([]byte{0x8D, 0x64, 0x24, 0x08}, stackFrame{esp: 12, espKnown: true}); f.esp != 4 {
		t.Errorf("lea esp %+v", f)
	}
	if f := single([]byte{0x8B, 0xE8}, stackFrame{esp: 12, ebp: 4, espKnown: true, ebpKnown: true}); f.ebpKnown {
		t.Errorf("mov ebp from register %+v", f)
	}
	if f := single([]byte{0x8B, 0xEC}, stackFrame{esp: 12, espKnown: false}); f.ebpKnown {
		t.Errorf("frame from unknown stack %+v", f)
	}
}

func equalI32(a, b []int32) bool {
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

func TestCallRemoves(t *testing.T) {
	d, at := analysis(t)
	user := mustFunction(t, d, at["cdeclUser"])
	idx := user.instructionIndex()
	if got := d.callRemoves(user, idx[at["dbgCall"]], 2); got != 0 {
		t.Errorf("variadic import removes %d", got)
	}
	if got := d.callRemoves(user, idx[at["eventCall"]], 1); got != 4 {
		t.Errorf("import removes %d", got)
	}
	if got := d.callRemoves(user, idx[at["eventCall"]], 3); got != 12 {
		t.Errorf("import removes %d for three pushes", got)
	}
	if got := d.callRemoves(user, idx[at["argsCall"]], 2); got != 8 {
		t.Errorf("callee cleanup removes %d", got)
	}
	if got := d.callRemoves(user, idx[at["thunkCall"]], 2); got != 0 {
		t.Errorf("thunk removes %d", got)
	}
	probe := mustFunction(t, d, at["probeUser"])
	if got := d.callRemoves(probe, probe.instructionIndex()[at["probeCall"]], 0); got != -0x2000 {
		t.Errorf("stack probe removes %d", got)
	}
	decomp := mustFunction(t, d, at["decomp"])
	if got := d.callRemoves(decomp, decomp.instructionIndex()[at["decompAfterArgs"]+2], 1); got != 0 {
		t.Errorf("virtual call removes %d", got)
	}
	jumper := functionOf(t, 0x11000, []byte{0xFF, 0x25, 0x08, 0x20, 0x01, 0x00})
	d.Functions[0x11000] = jumper
	caller := functionOf(t, 0x11100, []byte{0xE8, 0xFB, 0xFE, 0xFF, 0xFF})
	if got := d.callRemoves(caller, 0, 3); got != 0 {
		t.Errorf("variadic import thunk removes %d", got)
	}
	jumper.Instructions = functionOf(t, 0x11000, []byte{0xFF, 0x25, 0x00, 0x20, 0x01, 0x00}).Instructions
	if got := d.callRemoves(caller, 0, 3); got != 12 {
		t.Errorf("import thunk removes %d", got)
	}
}

func TestProbeFrameSize(t *testing.T) {
	d, at := analysis(t)
	probe := mustFunction(t, d, at["probeUser"])
	if got := probeFrameSize(probe, probe.instructionIndex()[at["probeCall"]]); got != 0x2000 {
		t.Errorf("probe frame %X", got)
	}
	user := mustFunction(t, d, at["cdeclUser"])
	if got := probeFrameSize(user, user.instructionIndex()[at["argsCall"]]); got != 0 {
		t.Errorf("frame without a size %X", got)
	}
	live := mustFunction(t, d, at["live"])
	if got := probeFrameSize(live, indexOfOp(t, live, x86asm.CALL, 0)); got != 0 {
		t.Errorf("frame loaded from memory %X", got)
	}
	if got := probeFrameSize(probe, 0); got != 0 {
		t.Errorf("frame before any load %X", got)
	}
}
