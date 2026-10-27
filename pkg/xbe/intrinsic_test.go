package xbe

import (
	"testing"

	"golang.org/x/arch/x86/x86asm"
)

func TestMathIntrinsic(t *testing.T) {
	d, at := analysis(t)
	if m, ok := d.mathIntrinsic(at["mathThunk"]); !ok || m != (mathIntrinsic{"fsin", 1}) {
		t.Errorf("sin thunk %+v %v", m, ok)
	}
	if m, ok := d.mathIntrinsic(at["powThunk"]); !ok || m != (mathIntrinsic{"fpow", 2}) {
		t.Errorf("pow thunk %+v %v", m, ok)
	}
	for _, name := range []string{"bogusThunk", "eaxThunk", "mathCore", "thunk", "noSwap", "oneSpill", "plainCore", "args"} {
		if m, ok := d.mathIntrinsic(at[name]); ok {
			t.Errorf("%s recognized as %+v", name, m)
		}
	}
	if _, ok := d.mathIntrinsic(0x15000); ok {
		t.Error("missing routine recognized")
	}
	if len(mathIntrinsics) != 19 || mathIntrinsics["atan2"] != (mathIntrinsic{"fatan2", 2}) || mathIntrinsics["sqrt"] != (mathIntrinsic{"fsqrt", 1}) {
		t.Errorf("intrinsic table %v", mathIntrinsics)
	}
}

func TestPowThunk(t *testing.T) {
	d, at := analysis(t)
	if !d.powThunk(mustFunction(t, d, at["powThunk"])) {
		t.Error("power thunk not recognized")
	}
	for _, name := range []string{"noSwap", "oneSpill", "plainCore", "mathThunk", "powCore"} {
		if d.powThunk(mustFunction(t, d, at[name])) {
			t.Errorf("%s recognized as the power thunk", name)
		}
	}
	long := mustFunction(t, d, at["powThunk"])
	padded := &Function{EntryVA: long.EntryVA, Instructions: append([]Instruction(nil), long.Instructions...)}
	for len(padded.Instructions) <= 12 {
		padded.Instructions = append(padded.Instructions, decodeAt(t, 0x11000, 0x90))
	}
	if d.powThunk(padded) {
		t.Error("long routine recognized as the power thunk")
	}
}

func TestNameMemoryRoutines(t *testing.T) {
	d, at := analysis(t)
	memcpy := mustFunction(t, d, at["memcpy"])
	if memcpy.Name != "memcpy" || memcpy.Symbol == nil || memcpy.Symbol.Name != "memcpy" || memcpy.Symbol.Lib != "usage" || !memcpy.Symbol.Cdecl || memcpy.Symbol.Raw != "_memcpy" {
		t.Errorf("memcpy %q %+v", memcpy.Name, memcpy.Symbol)
	}
	memset := mustFunction(t, d, at["memset"])
	if memset.Name != "memset" || memset.Symbol == nil || memset.Symbol.Lib != "usage" {
		t.Errorf("memset %q %+v", memset.Name, memset.Symbol)
	}
	for _, name := range []string{"switcher", "args", "entry", "probeUser"} {
		if fn := mustFunction(t, d, at[name]); fn.Symbol != nil && (fn.Symbol.Name == "memcpy" || fn.Symbol.Name == "memset") {
			t.Errorf("%s named %s", name, fn.Symbol.Name)
		}
	}
}

func memoryShape(t *testing.T, repeat []byte, tables int, call bool, extra ...[]byte) *Function {
	t.Helper()
	seqs := [][]byte{repeat}
	for i := 0; i < tables; i++ {
		seqs = append(seqs, []byte{0xFF, 0x24, 0x8D, 0x00, 0x20, 0x01, 0x00})
	}
	if call {
		seqs = append(seqs, []byte{0xE8, 0x00, 0x00, 0x00, 0x00})
	}
	seqs = append(seqs, extra...)
	seqs = append(seqs, []byte{0xC3})
	return functionOf(t, 0x11000, seqs...)
}

func TestNameMemoryRoutinesShapes(t *testing.T) {
	cases := []struct {
		fn   *Function
		name string
	}{
		{memoryShape(t, []byte{0xF3, 0xA5}, 2, false), "memcpy"},
		{memoryShape(t, []byte{0xF3, 0xAB}, 3, false), "memset"},
		{memoryShape(t, []byte{0xF3, 0xA5}, 2, true), ""},
		{memoryShape(t, []byte{0xF3, 0xA5}, 1, false), ""},
		{memoryShape(t, []byte{0xA5}, 2, false), ""},
		{memoryShape(t, []byte{0xF3, 0xA5}, 2, false, []byte{0xF3, 0xAB}), ""},
		{memoryShape(t, []byte{0xF3, 0xAB}, 2, false, []byte{0xFF, 0x24, 0x95, 0x00, 0x20, 0x01, 0x00}), "memset"},
	}
	for i, c := range cases {
		d := &Disassembly{Functions: map[uint32]*Function{0x11000: c.fn}}
		d.nameMemoryRoutines()
		got := ""
		if c.fn.Symbol != nil {
			got = c.fn.Symbol.Name
		}
		if got != c.name || c.fn.Name != c.name {
			t.Errorf("shape %d named %q, want %q", i, got, c.name)
		}
	}
	long := memoryShape(t, []byte{0xF3, 0xA5}, 2, false)
	for len(long.Instructions) <= 400 {
		long.Instructions = append(long.Instructions, decodeAt(t, 0x11000, 0x90))
	}
	d := &Disassembly{Functions: map[uint32]*Function{0x11000: long}}
	d.nameMemoryRoutines()
	if long.Symbol != nil {
		t.Error("long routine named")
	}
	named := memoryShape(t, []byte{0xF3, 0xA5}, 2, false)
	named.Symbol = &Symbol{Name: "Known"}
	d = &Disassembly{Functions: map[uint32]*Function{0x11000: named}}
	d.nameMemoryRoutines()
	if named.Symbol.Name != "Known" {
		t.Error("named routine renamed")
	}
}

func TestNameSecurityCheck(t *testing.T) {
	d, at := analysis(t)
	cookie := mustFunction(t, d, at["cookieCheck"])
	if cookie.Name != "_security_check_cookie" || cookie.Symbol == nil || cookie.Symbol.Lib != "usage" || !cookie.Symbol.Cdecl || cookie.Symbol.Raw != "__security_check_cookie" {
		t.Errorf("cookie check %q %+v", cookie.Name, cookie.Symbol)
	}
	for _, name := range []string{"args", "switcher", "chkstk", "live"} {
		if fn := mustFunction(t, d, at[name]); fn.Symbol != nil && fn.Symbol.Name == "_security_check_cookie" {
			t.Errorf("%s named as the cookie check", name)
		}
	}
}

func TestNameSecurityCheckShapes(t *testing.T) {
	img, _ := analysisImage(t)
	shape := func(seqs ...[]byte) *Disassembly {
		fn := functionOf(t, 0x11000, seqs...)
		d := &Disassembly{Image: img, Functions: map[uint32]*Function{0x11000: fn}, InsnByVA: map[uint32]*Instruction{}}
		for i := range fn.Instructions {
			d.InsnByVA[fn.Instructions[i].VA] = &fn.Instructions[i]
		}
		return d
	}
	cmpCookie := []byte{0x3B, 0x0D, 0x00, 0x21, 0x01, 0x00}
	cases := []struct {
		d    *Disassembly
		want bool
	}{
		{shape(cmpCookie, []byte{0x75, 0x01}, []byte{0xC3}), true},
		{shape(cmpCookie, []byte{0x74, 0x01}, []byte{0xC3}), true},
		{shape([]byte{0x3B, 0x05, 0x00, 0x21, 0x01, 0x00}, []byte{0x75, 0x01}, []byte{0xC3}), false},
		{shape([]byte{0x3B, 0x0D, 0x00, 0x00, 0x05, 0x00}, []byte{0x75, 0x01}, []byte{0xC3}), false},
		{shape([]byte{0x3B, 0x4D, 0x08}, []byte{0x75, 0x01}, []byte{0xC3}), false},
		{shape(cmpCookie, []byte{0xEB, 0x01}, []byte{0xC3}), false},
		{shape(cmpCookie, []byte{0x75, 0x01}, []byte{0x90}), false},
		{shape([]byte{0x85, 0xC9}, []byte{0x75, 0x01}, []byte{0xC3}), false},
		{shape(cmpCookie, []byte{0xC3}), false},
	}
	for i, c := range cases {
		c.d.nameSecurityCheck()
		fn := c.d.Functions[0x11000]
		if got := fn.Symbol != nil; got != c.want {
			t.Errorf("shape %d named %v, want %v", i, got, c.want)
		}
	}
	named := shape(cmpCookie, []byte{0x75, 0x01}, []byte{0xC3})
	named.Functions[0x11000].Symbol = &Symbol{Name: "Known"}
	named.nameSecurityCheck()
	if named.Functions[0x11000].Symbol.Name != "Known" {
		t.Error("named routine renamed")
	}
}

func TestIsStackProbe(t *testing.T) {
	d, at := analysis(t)
	if !isStackProbe(mustFunction(t, d, at["chkstk"])) {
		t.Error("probe not recognized")
	}
	for _, name := range []string{"args", "probeUser", "entry", "spin"} {
		if isStackProbe(mustFunction(t, d, at[name])) {
			t.Errorf("%s recognized as the probe", name)
		}
	}
	test, cmp, xchg, sub := []byte{0x85, 0xC0}, []byte{0x3D, 0x00, 0x10, 0x00, 0x00}, []byte{0x94}, []byte{0x2D, 0x00, 0x10, 0x00, 0x00}
	cases := []struct {
		fn   *Function
		want bool
	}{
		{functionOf(t, 0x11000, test, cmp, xchg, []byte{0xC3}), true},
		{functionOf(t, 0x11000, test, xchg, cmp, sub, []byte{0xC3}), true},
		{functionOf(t, 0x11000, cmp, test, xchg, []byte{0xC3}), false},
		{functionOf(t, 0x11000, test, cmp, []byte{0xC3}), false},
		{functionOf(t, 0x11000, test, xchg, []byte{0xC3}), false},
		{functionOf(t, 0x11000, test, []byte{0x3D, 0x00, 0x20, 0x00, 0x00}, xchg, []byte{0xC3}), false},
		{functionOf(t, 0x11000, []byte{0x85, 0xC9}, cmp, xchg, []byte{0xC3}), false},
		{functionOf(t, 0x11000, test, cmp, []byte{0x91}, []byte{0xC3}), false},
	}
	for i, c := range cases {
		if got := isStackProbe(c.fn); got != c.want {
			t.Errorf("shape %d probe %v, want %v", i, got, c.want)
		}
	}
	long := functionOf(t, 0x11000, test, cmp, xchg)
	for len(long.Instructions) <= 40 {
		long.Instructions = append(long.Instructions, decodeAt(t, 0x11000, 0x90))
	}
	if isStackProbe(long) {
		t.Error("long routine recognized as the probe")
	}
	shifted := functionOf(t, 0x11000, test, cmp, xchg)
	shifted.EntryVA = 0x11001
	if isStackProbe(shifted) {
		t.Error("routine entered mid instruction recognized")
	}
}

func TestNameStackProbe(t *testing.T) {
	d, at := analysis(t)
	probe := mustFunction(t, d, at["chkstk"])
	if probe.Name != "chkstk" || probe.Symbol == nil || probe.Symbol.Name != "chkstk" || probe.Symbol.Lib != "usage" || probe.Symbol.Raw != "_chkstk" || !probe.Symbol.Cdecl {
		t.Errorf("probe %q %+v", probe.Name, probe.Symbol)
	}
	if user := mustFunction(t, d, at["probeUser"]); user.Symbol != nil {
		t.Errorf("probe user named %+v", user.Symbol)
	}
	ex := NewExecutor(d)
	if !ex.probe[at["chkstk"]] || ex.probe[at["probeUser"]] {
		t.Errorf("executor probes %v", ex.probe)
	}
	fn := functionOf(t, 0x11000, []byte{0x85, 0xC0}, []byte{0x3D, 0x00, 0x10, 0x00, 0x00}, []byte{0x94}, []byte{0xC3})
	bare := &Disassembly{Functions: map[uint32]*Function{0x11000: fn}}
	bare.nameStackProbe()
	if fn.Name != "chkstk" {
		t.Errorf("bare probe named %q", fn.Name)
	}
	if ops := opsOf(fn); ops[2] != x86asm.XCHG.String() {
		t.Errorf("ops %v", ops)
	}
}
