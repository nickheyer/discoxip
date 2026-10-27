package xbe

import (
	"reflect"
	"testing"

	"golang.org/x/arch/x86/x86asm"
)

func constructText() []chunk {
	return []chunk{
		lbl("ctorECX"), abs32([]byte{0xC7, 0x01}, "vt", 0), ret(), pad(),
		lbl("ctorEAX"), abs32([]byte{0xC7, 0x00}, "vt", 0), ret(), pad(),
		lbl("ctorEDX"), abs32([]byte{0xC7, 0x02}, "vt", 0), ret(), pad(),
		lbl("ctorEBX"), abs32([]byte{0xC7, 0x03}, "vt", 0), ret(), pad(),
		lbl("ctorESI"), abs32([]byte{0xC7, 0x06}, "vt", 0), ret(), pad(),
		lbl("ctorEDI"), abs32([]byte{0xC7, 0x07}, "vt", 0), ret(), pad(),
		lbl("ctorStackESP"), by(0x56), by(0x8B, 0x74, 0x24, 0x08), abs32([]byte{0xC7, 0x06}, "vt", 0), by(0x5E), ret(), pad(),
		lbl("ctorStackEBP"), by(0x55), by(0x8B, 0xEC), by(0x8B, 0x45, 0x0C), abs32([]byte{0xC7, 0x00}, "vt", 0), by(0x5D), ret(), pad(),
		lbl("ctorMoved"), by(0x8B, 0xC1), abs32([]byte{0xC7, 0x00}, "vt", 0), ret(), pad(),
		lbl("notCtor"), by(0xC7, 0x01, 5, 0, 0, 0), ret(), pad(),
		lbl("ctorClobbered"), movECXImm(0), abs32([]byte{0xC7, 0x01}, "vt", 0), ret(), pad(),
		lbl("alloc"), movEAXImm(1), ret(), pad(),
		lbl("factory"), call("alloc"), abs32([]byte{0xC7, 0x00}, "vt", 0), abs32([]byte{0xA3}, "gA", 0), ret(), pad(),
		lbl("factoryCtor"), call("alloc"), by(0x8B, 0xC8), call("ctorECX"), abs32([]byte{0xA3}, "gB", 0), ret(), pad(),
		lbl("factoryStack"), call("alloc"), by(0x50), call("ctorStackESP"), abs32([]byte{0xA3}, "gC", 0), ret(), pad(),
		lbl("factoryNamed"), call("alloc"), abs32([]byte{0x68}, "nameStr", 0), by(0x8B, 0xC8), call("ctorECX"), ret(), pad(),
		lbl("factoryNamedStore"), call("alloc"), by(0x8B, 0xC8), abs32([]byte{0xC7, 0x41, 0x08}, "nameStr", 0), abs32([]byte{0xC7, 0x01}, "vt", 0), ret(), pad(),
		lbl("factoryUnnamed"), call("alloc"), by(0x8B, 0xC8), abs32([]byte{0xC7, 0x41, 0x08}, "badStr", 0), abs32([]byte{0xC7, 0x01}, "vt", 0), ret(), pad(),
		lbl("factoryJump"), call("alloc"), jmp8("factoryInit"), lbl("factoryFail"), by(0x33, 0xC0), ret(), lbl("factoryInit"), abs32([]byte{0xC7, 0x00}, "vt", 0), ret(), pad(),
		lbl("factoryBranch"), call("alloc"), by(0x85, 0xC0), jne8("factoryBranchInit"), by(0x33, 0xC0), ret(), lbl("factoryBranchInit"), abs32([]byte{0xC7, 0x00}, "vt", 0), ret(), pad(),
		lbl("factoryNear"), call("alloc"), rep(95, 0x90), abs32([]byte{0xC7, 0x00}, "vt", 0), ret(), pad(),
		lbl("factoryFar"), call("alloc"), rep(96, 0x90), abs32([]byte{0xC7, 0x00}, "vt", 0), ret(), pad(),
		lbl("factoryCookie"), call("alloc"), abs32([]byte{0x8B, 0x0D}, "gcookie", 0), call("securityCheck"), abs32([]byte{0xC7, 0x00}, "vt", 0), ret(), pad(),
		lbl("factoryClobber"), call("alloc"), call("writesEAX"), abs32([]byte{0xC7, 0x00}, "vt", 0), ret(), pad(),
		lbl("securityCheck"), abs32([]byte{0x3B, 0x0D}, "gcookie", 0), jne8("cookieFail"), ret(), lbl("cookieFail"), by(0xCC), pad(),
		lbl("writesEAX"), movEAXImm(2), ret(), pad(),
		lbl("floatFn"), by(0xF3, 0x0F, 0x58, 0xD3), ret(), pad(),
		lbl("floatHigh"), by(0xF3, 0x0F, 0x58, 0xE5), ret(), pad(),
		lbl("vtfn"), ret(), pad(),
	}
}

func constructImage(t *testing.T) *testImage {
	t.Helper()
	return buildImage(t, constructText(), []chunk{
		lbl("vt"), wordAt("vtfn", 0),
		lbl("nameStr"), wide("Widget"),
		lbl("badStr"), wide("\x01x"),
		lbl("emptyStr"), wide(""),
	}, []chunk{
		lbl("gA"), word(0), lbl("gB"), word(0), lbl("gC"), word(0), lbl("gcookie"), word(0),
	}, nil)
}

func TestConstructorShapes(t *testing.T) {
	ti := constructImage(t)
	vt := ti.at(t, "vt")
	cases := map[string]argLocation{
		"ctorEAX": {Reg: x86asm.EAX}, "ctorECX": {Reg: x86asm.ECX}, "ctorEDX": {Reg: x86asm.EDX},
		"ctorEBX": {Reg: x86asm.EBX}, "ctorESI": {Reg: x86asm.ESI}, "ctorEDI": {Reg: x86asm.EDI},
		"ctorStackESP": {Stack: 0}, "ctorStackEBP": {Stack: 1}, "ctorMoved": {Reg: x86asm.ECX},
	}
	for label, want := range cases {
		got, this := ctorShape(ti.d, ti.d.Functions[ti.at(t, label)])
		if got != vt || this != want {
			t.Errorf("%s installs %08X into %v, want %08X into %v", label, got, this, vt, want)
		}
	}
	for _, label := range []string{"notCtor", "ctorClobbered", "alloc", "factory"} {
		if got, this := ctorShape(ti.d, ti.d.Functions[ti.at(t, label)]); got != 0 || this != (argLocation{}) {
			t.Errorf("%s is a constructor of %08X %v", label, got, this)
		}
	}
	infos := ti.d.constructorInfo()
	if len(infos) != len(cases) {
		t.Errorf("%d constructors found, want %d", len(infos), len(cases))
	}
	for label, want := range cases {
		info := infos[ti.at(t, label)]
		if info.vtable != vt || info.this != want || info.library {
			t.Errorf("%s info %+v", label, info)
		}
	}
	found := ti.d.findConstructors()
	if len(found) != len(cases) || found[ti.at(t, "ctorECX")] != vt {
		t.Errorf("found %v", found)
	}
	if !reflect.DeepEqual(argumentRegisters, []x86asm.Reg{x86asm.EAX, x86asm.ECX, x86asm.EDX, x86asm.EBX, x86asm.ESI, x86asm.EDI}) {
		t.Errorf("argument registers %v", argumentRegisters)
	}
	if !reflect.DeepEqual(floatArgumentRegisters, []x86asm.Reg{x86asm.X0, x86asm.X1, x86asm.X2, x86asm.X3}) {
		t.Errorf("float argument registers %v", floatArgumentRegisters)
	}
	if got := ti.d.liveInFloatRegisters(ti.d.Functions[ti.at(t, "floatFn")]); !reflect.DeepEqual(got, []x86asm.Reg{x86asm.X2, x86asm.X3}) {
		t.Errorf("float inputs %v", got)
	}
	if got := ti.d.liveInFloatRegisters(ti.d.Functions[ti.at(t, "floatHigh")]); len(got) != 0 {
		t.Errorf("high vector registers count as inputs %v", got)
	}
}

func TestStackArgumentSlots(t *testing.T) {
	cases := []struct {
		mem   x86asm.Mem
		depth int
		frame bool
		slot  int
		ok    bool
	}{
		{x86asm.Mem{Base: x86asm.ESP, Disp: 8}, 4, false, 0, true},
		{x86asm.Mem{Base: x86asm.ESP, Disp: 12}, 4, false, 1, true},
		{x86asm.Mem{Base: x86asm.ESP, Disp: 4}, 0, false, 0, true},
		{x86asm.Mem{Base: x86asm.ESP, Disp: 4}, 4, false, 0, false},
		{x86asm.Mem{Base: x86asm.ESP, Disp: 10}, 4, false, 0, false},
		{x86asm.Mem{Base: x86asm.EBP, Disp: 8}, 0, true, 0, true},
		{x86asm.Mem{Base: x86asm.EBP, Disp: 12}, 8, true, 1, true},
		{x86asm.Mem{Base: x86asm.EBP, Disp: 12}, 0, false, 0, false},
		{x86asm.Mem{Base: x86asm.EBP, Disp: 4}, 0, true, 0, false},
		{x86asm.Mem{Base: x86asm.ESP, Index: x86asm.EAX, Scale: 4, Disp: 8}, 4, false, 0, false},
		{x86asm.Mem{Base: x86asm.ECX, Disp: 8}, 4, true, 0, false},
	}
	for _, tc := range cases {
		slot, ok := stackArgumentSlot(tc.mem, tc.depth, tc.frame)
		if ok != tc.ok || slot != tc.slot {
			t.Errorf("%v at depth %d frame %v = %d %v, want %d %v", tc.mem, tc.depth, tc.frame, slot, ok, tc.slot, tc.ok)
		}
	}
}

func constructionOf(t *testing.T, ti *testImage, label string) (construction, bool) {
	t.Helper()
	var found []construction
	for _, c := range ti.d.constructions(ti.d.Functions[ti.at(t, label)]) {
		if c.alloc == ti.at(t, "alloc") {
			found = append(found, c)
		}
	}
	if len(found) > 1 {
		t.Fatalf("%s builds %d objects from the allocator", label, len(found))
	}
	if len(found) == 0 {
		return construction{}, false
	}
	return found[0], true
}

func TestConstructionsFromAllocations(t *testing.T) {
	ti := constructImage(t)
	vt, alloc := ti.at(t, "vt"), ti.at(t, "alloc")
	c, ok := constructionOf(t, ti, "factory")
	if !ok || c.alloc != alloc || c.ctor != 0 || c.vtable != vt || c.named || !reflect.DeepEqual(c.globals, []uint32{ti.at(t, "gA")}) {
		t.Errorf("factory %+v %v", c, ok)
	}
	c, ok = constructionOf(t, ti, "factoryCtor")
	if !ok || c.ctor != ti.at(t, "ctorECX") || c.vtable != vt || !reflect.DeepEqual(c.globals, []uint32{ti.at(t, "gB")}) {
		t.Errorf("factory through a constructor %+v %v", c, ok)
	}
	c, ok = constructionOf(t, ti, "factoryStack")
	if !ok || c.ctor != ti.at(t, "ctorStackESP") || c.vtable != vt || !reflect.DeepEqual(c.globals, []uint32{ti.at(t, "gC")}) {
		t.Errorf("factory through a stack constructor %+v %v", c, ok)
	}
	c, ok = constructionOf(t, ti, "factoryNamed")
	if !ok || c.ctor != ti.at(t, "ctorECX") || !c.named {
		t.Errorf("factory pushing a name %+v %v", c, ok)
	}
	c, ok = constructionOf(t, ti, "factoryNamedStore")
	if !ok || c.vtable != vt || !c.named || c.ctor != 0 {
		t.Errorf("factory storing a name %+v %v", c, ok)
	}
	c, ok = constructionOf(t, ti, "factoryUnnamed")
	if !ok || c.vtable != vt || c.named {
		t.Errorf("factory storing an unprintable string %+v %v", c, ok)
	}
	c, ok = constructionOf(t, ti, "factoryJump")
	if !ok || c.vtable != vt || c.alloc != alloc {
		t.Errorf("factory jumping past its failure branch %+v %v", c, ok)
	}
	if c, ok := constructionOf(t, ti, "factoryBranch"); ok {
		t.Errorf("a failure branch cleared the object yet %+v was found", c)
	}
	if cs := ti.d.constructions(ti.d.Functions[ti.at(t, "factoryClobber")]); len(cs) != 1 || cs[0].alloc != ti.at(t, "writesEAX") || cs[0].vtable != vt {
		t.Errorf("the clobbering call stands as the allocation %+v", cs)
	}
	c, ok = constructionOf(t, ti, "factoryNear")
	if !ok || c.vtable != vt {
		t.Errorf("vtable stored at the 96th step %+v %v", c, ok)
	}
	if c, ok := constructionOf(t, ti, "factoryFar"); ok {
		t.Errorf("vtable stored past the 96th step was found %+v", c)
	}
	c, ok = constructionOf(t, ti, "factoryCookie")
	if !ok || c.vtable != vt {
		t.Errorf("object survives the cookie check %+v %v", c, ok)
	}
	if c, ok := constructionOf(t, ti, "factoryClobber"); ok {
		t.Errorf("object survived a call writing eax %+v", c)
	}
	check := ti.d.Functions[ti.at(t, "securityCheck")]
	if check.Symbol == nil || check.Symbol.Name != "_security_check_cookie" || ti.d.callClobbers(ti.at(t, "securityCheck")) != nil {
		t.Errorf("cookie check %+v clobbers %v", check.Symbol, ti.d.callClobbers(ti.at(t, "securityCheck")))
	}
	if got := ti.d.callClobbers(ti.at(t, "writesEAX")); !reflect.DeepEqual(got, []x86asm.Reg{x86asm.EAX}) {
		t.Errorf("helper clobbers %v", got)
	}
	if got := ti.d.callClobbers(0x12345); !reflect.DeepEqual(got, callerSaved) {
		t.Errorf("unknown target clobbers %v", got)
	}
	if !ti.d.namesObject(ti.at(t, "nameStr")) || ti.d.namesObject(ti.at(t, "badStr")) || ti.d.namesObject(ti.at(t, "emptyStr")) {
		t.Errorf("object names misjudged")
	}
	if !ti.d.isDataAddress(ti.at(t, "gA")) || ti.d.isDataAddress(ti.at(t, "vt")) || ti.d.isDataAddress(ti.at(t, "alloc")) {
		t.Errorf("data addresses misjudged")
	}
}

func decode(t *testing.T, b ...byte) *Instruction {
	t.Helper()
	inst, err := x86asm.Decode(b, 32)
	if err != nil {
		t.Fatalf("decode % X: %v", b, err)
	}
	return &Instruction{VA: 0x11000, Inst: inst, Raw: b}
}

func TestRegisterReadsAndWrites(t *testing.T) {
	regs := func(rs ...x86asm.Reg) []x86asm.Reg { return rs }
	reads := []struct {
		name string
		in   *Instruction
		save bool
		want []x86asm.Reg
	}{
		{"xor eax,eax", decode(t, 0x33, 0xC0), false, nil},
		{"mov eax,eax", decode(t, 0x8B, 0xC0), false, nil},
		{"push ebp saved", decode(t, 0x55), true, nil},
		{"push ebp", decode(t, 0x55), false, regs(x86asm.EBP)},
		{"cdq", decode(t, 0x99), false, regs(x86asm.EAX)},
		{"div ecx", decode(t, 0xF7, 0xF1), false, regs(x86asm.EAX, x86asm.EDX, x86asm.ECX)},
		{"movsd", decode(t, 0xA5), false, regs(x86asm.ECX, x86asm.ESI, x86asm.EDI)},
		{"stosd", decode(t, 0xAB), false, regs(x86asm.EAX, x86asm.ECX, x86asm.EDI)},
		{"lodsd", decode(t, 0xAD), false, regs(x86asm.ESI)},
		{"imul eax,ecx", decode(t, 0x0F, 0xAF, 0xC1), false, regs(x86asm.EAX, x86asm.ECX)},
		{"imul eax,ecx,10", decode(t, 0x6B, 0xC1, 0x0A), false, regs(x86asm.ECX)},
		{"imul ecx", decode(t, 0xF7, 0xE9), false, regs(x86asm.EAX, x86asm.ECX)},
		{"or eax,-1", decode(t, 0x83, 0xC8, 0xFF), false, nil},
		{"and eax,0", decode(t, 0x83, 0xE0, 0x00), false, nil},
		{"and eax,1", decode(t, 0x83, 0xE0, 0x01), false, regs(x86asm.EAX)},
		{"xorps xmm0,xmm0", decode(t, 0x0F, 0x57, 0xC0), false, nil},
		{"addss xmm0,xmm1", decode(t, 0xF3, 0x0F, 0x58, 0xC1), false, regs(x86asm.X0, x86asm.X1)},
		{"movss xmm0,xmm1", decode(t, 0xF3, 0x0F, 0x10, 0xC1), false, regs(x86asm.X1)},
		{"lea eax,[ecx+edx*4]", decode(t, 0x8D, 0x04, 0x91), false, regs(x86asm.ECX, x86asm.EDX)},
		{"mov al,[ecx]", decode(t, 0x8A, 0x01), false, regs(x86asm.ECX)},
		{"add al,cl", decode(t, 0x02, 0xC1), false, regs(x86asm.EAX, x86asm.ECX)},
		{"sub ecx,ecx", decode(t, 0x2B, 0xC9), false, nil},
		{"setb al", decode(t, 0x0F, 0x92, 0xC0), false, nil},
		{"pop ebx", decode(t, 0x5B), false, nil},
	}
	for _, tc := range reads {
		if got := registersRead(tc.in, tc.save); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s reads %v, want %v", tc.name, got, tc.want)
		}
	}
	writes := []struct {
		name string
		in   *Instruction
		want []x86asm.Reg
	}{
		{"push eax", decode(t, 0x50), nil},
		{"cmp eax,1", decode(t, 0x83, 0xF8, 0x01), nil},
		{"fld [eax]", decode(t, 0xD9, 0x00), nil},
		{"call", decode(t, 0xE8, 0, 0, 0, 0), callerSaved},
		{"cdq", decode(t, 0x99), regs(x86asm.EDX)},
		{"mul ecx", decode(t, 0xF7, 0xE1), regs(x86asm.EAX, x86asm.EDX)},
		{"imul ecx", decode(t, 0xF7, 0xE9), regs(x86asm.EAX, x86asm.EDX)},
		{"imul eax,ecx", decode(t, 0x0F, 0xAF, 0xC1), regs(x86asm.EAX)},
		{"xchg eax,ecx", decode(t, 0x87, 0xC8), regs(x86asm.EAX, x86asm.ECX)},
		{"movsd", decode(t, 0xA5), regs(x86asm.EAX, x86asm.ECX, x86asm.ESI, x86asm.EDI)},
		{"fnstsw ax", decode(t, 0xDF, 0xE0), regs(x86asm.EAX)},
		{"mov al,1", decode(t, 0xB0, 0x01), regs(x86asm.EAX)},
		{"mov [eax],ecx", decode(t, 0x89, 0x08), nil},
	}
	for _, tc := range writes {
		if got := writtenRegisters(tc.in); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s writes %v, want %v", tc.name, got, tc.want)
		}
	}
	if dst, src, ok := registerMove(decode(t, 0x8B, 0xC8)); !ok || dst != x86asm.ECX || src != x86asm.EAX {
		t.Errorf("mov ecx,eax read as %v %v %v", dst, src, ok)
	}
	if _, _, ok := registerMove(decode(t, 0x8A, 0xC8)); ok {
		t.Errorf("a byte move is a register move")
	}
	if _, _, ok := registerMove(decode(t, 0x8B, 0x01)); ok {
		t.Errorf("a load is a register move")
	}
	for r, want := range map[x86asm.Reg]x86asm.Reg{x86asm.AL: x86asm.EAX, x86asm.AH: x86asm.EAX, x86asm.CX: x86asm.ECX, x86asm.SP: x86asm.ESP, x86asm.BH: x86asm.EBX, x86asm.EDI: x86asm.EDI, x86asm.X3: x86asm.X3, x86asm.FS: x86asm.FS} {
		if got := wideRegister(r); got != want {
			t.Errorf("%v widens to %v, want %v", r, got, want)
		}
	}
	if !reflect.DeepEqual(callerSaved, append([]x86asm.Reg{x86asm.EAX, x86asm.ECX, x86asm.EDX}, vectorRegisters...)) || len(vectorRegisters) != 8 {
		t.Errorf("caller saved registers %v", callerSaved)
	}
}
