package xbe

import (
	"reflect"
	"strings"
	"testing"

	"golang.org/x/arch/x86/x86asm"
)

func conventionText() []chunk {
	return []chunk{
		lbl("helperRegs"), by(0x8B, 0xC2), by(0x03, 0xC6), by(0x03, 0x44, 0x24, 0x04), by(0x03, 0x44, 0x24, 0x08), retn(8), pad(),
		lbl("cdeclRegs"), by(0x8B, 0xC1), by(0x03, 0x44, 0x24, 0x04), ret(), pad(),
		lbl("noRegs"), by(0x8B, 0x44, 0x24, 0x04), retn(4), pad(),
		lbl("canonicalMatch"), by(0x8B, 0xC1), retn(4), pad(),
		lbl("thiscall"), by(0x8B, 0x01), ret(), pad(),
		lbl("oneReg"), by(0x8B, 0xC2), ret(), pad(),
		lbl("unlisted"), by(0x8B, 0xC2), by(0x03, 0x44, 0x24, 0x04), retn(4), pad(),
		lbl("declared"), abs32([]byte{0x8B, 0x0C, 0x85}, "tableC", 0), by(0x8B, 0x12), by(0x03, 0xCA), abs32([]byte{0x89, 0x0D}, "gres", 0), ret(), pad(),
		lbl("ambiguous"), by(0x8B, 0x00), by(0x8B, 0x12), by(0x03, 0xC2), abs32([]byte{0xA3}, "gres", 0), ret(), pad(),
		lbl("tooFew"), by(0x8B, 0x02), abs32([]byte{0xA3}, "gres", 0), ret(), pad(),
		lbl("fastReadsEAX"), by(0x03, 0xC1), ret(), pad(),
	}
}

func conventionImage(t *testing.T) *testImage {
	t.Helper()
	return buildImage(t, conventionText(), []chunk{lbl("tableC"), word(1), word(2), word(3), word(4)}, []chunk{lbl("gres"), word(0)}, nil)
}

func named(ti *testImage, t *testing.T, label, name string, cdecl, fast bool) *Function {
	t.Helper()
	fn := ti.d.Functions[ti.at(t, label)]
	fn.Symbol = &Symbol{VA: fn.EntryVA, Raw: name, Name: name, ArgBytes: -1, Cdecl: cdecl, FastCall: fast}
	fn.Name = name
	return fn
}

func reg(r x86asm.Reg) argLocation { return argLocation{Reg: r} }
func stack(i int) argLocation      { return argLocation{Stack: i} }

func TestConventionRules(t *testing.T) {
	ti := conventionImage(t)
	conv, err := ti.d.convention(ti.d.Functions[ti.at(t, "helperRegs")])
	if err != nil || !conv.known || conv.stackBytes != 8 || !reflect.DeepEqual(conv.params, []argLocation{reg(x86asm.EDX), reg(x86asm.ESI), stack(0), stack(1)}) {
		t.Errorf("unnamed helper %+v %v", conv, err)
	}
	conv, err = ti.d.convention(named(ti, t, "cdeclRegs", "Widget::Scale", true, false))
	if err != nil || !conv.known || conv.stackBytes != 0 || !reflect.DeepEqual(conv.params, []argLocation{reg(x86asm.ECX), stack(0)}) {
		t.Errorf("cdecl reading registers %+v %v", conv, err)
	}
	conv, err = ti.d.convention(named(ti, t, "noRegs", "D3DDevice_SetTexture", false, false))
	if err != nil || conv.known || conv.stackBytes != 4 || len(conv.params) != 0 {
		t.Errorf("no live registers %+v %v", conv, err)
	}
	conv, err = ti.d.convention(named(ti, t, "canonicalMatch", "D3DDevice_SetRenderState_ZEnable", false, false))
	if err != nil || conv.known || conv.stackBytes != 4 {
		t.Errorf("canonical size matching the cleanup %+v %v", conv, err)
	}
	conv, err = ti.d.convention(named(ti, t, "thiscall", "Widget::Get", false, false))
	if err != nil || conv.known || conv.stackBytes != 0 {
		t.Errorf("thiscall %+v %v", conv, err)
	}
	conv, err = ti.d.convention(named(ti, t, "oneReg", "D3DDevice_SetRenderState_ZEnable", false, false))
	if err != nil || !conv.known || !reflect.DeepEqual(conv.params, []argLocation{reg(x86asm.EDX)}) {
		t.Errorf("one register for the one canonical parameter %+v %v", conv, err)
	}
	conv, err = ti.d.convention(named(ti, t, "unlisted", "Widget::Set", false, false))
	if err != nil || !conv.known || conv.stackBytes != 4 || !reflect.DeepEqual(conv.params, []argLocation{reg(x86asm.EDX), stack(0)}) {
		t.Errorf("unlisted name %+v %v", conv, err)
	}
	conv, err = ti.d.convention(named(ti, t, "declared", "D3DDevice_SetTexture", false, false))
	if err != nil || !conv.known || !reflect.DeepEqual(conv.params, []argLocation{reg(x86asm.EAX), reg(x86asm.EDX)}) {
		t.Errorf("declared parameters %+v %v", conv, err)
	}
	again, err := ti.d.convention(ti.d.Functions[ti.at(t, "declared")])
	if err != nil || !reflect.DeepEqual(again, conv) {
		t.Errorf("convention not cached %+v %v", again, err)
	}
	_, err = ti.d.convention(named(ti, t, "ambiguous", "D3DDevice_SetTexture", false, false))
	if err == nil || !strings.Contains(err.Error(), "no input of D3DDevice_SetTexture fits each declared parameter") {
		t.Errorf("two pointers for an index and a pointer: %v", err)
	}
	_, err = ti.d.convention(named(ti, t, "tooFew", "D3DDevice_SetTexture", false, false))
	if err == nil || !strings.Contains(err.Error(), "declares 2 parameters but takes 1 inputs") {
		t.Errorf("one input for two parameters: %v", err)
	}
	conv, err = ti.d.convention(named(ti, t, "fastReadsEAX", "Widget::Fast", false, true))
	if err != nil || conv.known {
		t.Errorf("fastcall %+v %v", conv, err)
	}
}

func TestLibraryParams(t *testing.T) {
	ti := conventionImage(t)
	params, err := ti.d.libraryParams(ti.d.Functions[ti.at(t, "helperRegs")])
	if err != nil || !reflect.DeepEqual(params, []string{"EDX", "ESI", "stack0", "stack1"}) {
		t.Errorf("helper params %v %v", params, err)
	}
	params, err = ti.d.libraryParams(named(ti, t, "thiscall", "Widget::Get", false, false))
	if err != nil || len(params) != 0 {
		t.Errorf("thiscall params %v %v", params, err)
	}
	_, err = ti.d.libraryParams(named(ti, t, "fastReadsEAX", "Widget::Fast", false, true))
	if err == nil || !strings.Contains(err.Error(), "reads EAX on entry, which no caller passes") {
		t.Errorf("fastcall reading eax: %v", err)
	}
	named(ti, t, "noRegs", "Widget::Plain", true, false)
	fn := ti.d.Functions[ti.at(t, "noRegs")]
	fn.Symbol.FastCall = true
	params, err = ti.d.libraryParams(fn)
	if err != nil || !reflect.DeepEqual(params, []string{"ECX", "EDX"}) {
		t.Errorf("fastcall params %v %v", params, err)
	}
	if registerParam(x86asm.X2) != "XMM2" || registerParam(x86asm.ESI) != "ESI" {
		t.Errorf("register parameter names wrong")
	}
	if ti.d.readsReceiver(ti.d.Functions[ti.at(t, "thiscall")]) != true || ti.d.readsReceiver(ti.d.Functions[ti.at(t, "helperRegs")]) {
		t.Errorf("receiver reads misjudged")
	}
}

func TestDeclaredParameterKinds(t *testing.T) {
	if len(xdkParameters) != 18 {
		t.Errorf("%d declared routines, want 18", len(xdkParameters))
	}
	want := map[string][]paramKind{
		"D3DDevice_SetTexture":         {kindIndex, kindPointer},
		"D3DDevice_SetTransform":       {kindIndex, kindPointer},
		"D3DDevice_Swap":               {kindFlags},
		"D3DDevice_DrawVertices":       {kindStored, kindCombined, kindCount},
		"D3DDevice_CreateVertexShader": {kindPointer, kindPointer, kindOut, kindFlags},
		"Direct3D_CreateDevice":        {kindIgnored, kindIgnored, kindIgnored, kindFlags, kindPointer, kindOut},
	}
	for name, kinds := range want {
		if !reflect.DeepEqual(xdkParameters[name], kinds) {
			t.Errorf("%s declares %v, want %v", name, xdkParameters[name], kinds)
		}
	}
	if len(kindNames) != 8 || kindNames[kindIgnored] != "ignored" || kindNames[kindFlags] != "flags" {
		t.Errorf("kind names %v", kindNames)
	}
}

func TestRolePriorityAndSatisfaction(t *testing.T) {
	kinds := []struct {
		r    roleSet
		want paramKind
	}{
		{roleSet{}, kindIgnored},
		{roleSet{storeBase: true, loadBase: true, scaled: true, stored: true}, kindOut},
		{roleSet{loadBase: true, scaled: true, stored: true}, kindPointer},
		{roleSet{scaled: true, stored: true, compared: true}, kindIndex},
		{roleSet{stored: true}, kindStored},
		{roleSet{stored: true, combined: true}, kindStored},
		{roleSet{tested: true}, kindFlags},
		{roleSet{tested: true, compared: true}, kindFlags},
		{roleSet{tested: true, combined: true}, kindCombined},
		{roleSet{compared: true}, kindCount},
		{roleSet{stored: true, compared: true}, kindCount},
		{roleSet{combined: true}, kindCombined},
		{roleSet{forwarded: true}, kindIgnored},
	}
	for _, tc := range kinds {
		if got := tc.r.kind(); got != tc.want {
			t.Errorf("%+v kinds as %s, want %s", tc.r, kindNames[got], kindNames[tc.want])
		}
	}
	supports := []struct {
		r    roleSet
		k    paramKind
		want bool
	}{
		{roleSet{scaled: true}, kindIndex, true},
		{roleSet{loadBase: true}, kindIndex, false},
		{roleSet{loadBase: true}, kindPointer, true},
		{roleSet{forwarded: true}, kindPointer, true},
		{roleSet{storeBase: true}, kindPointer, false},
		{roleSet{storeBase: true}, kindOut, true},
		{roleSet{stored: true}, kindStored, true},
		{roleSet{combined: true}, kindStored, false},
		{roleSet{stored: true}, kindCombined, true},
		{roleSet{combined: true}, kindCombined, true},
		{roleSet{compared: true}, kindCount, true},
		{roleSet{tested: true}, kindCount, false},
		{roleSet{tested: true}, kindFlags, true},
		{roleSet{compared: true}, kindFlags, true},
		{roleSet{stored: true}, kindFlags, false},
		{roleSet{scaled: true, loadBase: true, storeBase: true, stored: true, combined: true, compared: true, tested: true, forwarded: true}, kindIgnored, false},
	}
	for _, tc := range supports {
		if got := tc.r.supports(tc.k); got != tc.want {
			t.Errorf("%+v supports %s = %v", tc.r, kindNames[tc.k], got)
		}
	}
}

func TestMatchParametersResolution(t *testing.T) {
	eax, edx, s0 := reg(x86asm.EAX), reg(x86asm.EDX), stack(0)
	got, err := matchParameters("f", []paramKind{kindIndex, kindPointer}, map[argLocation]roleSet{eax: {scaled: true}, edx: {loadBase: true}})
	if err != nil || !reflect.DeepEqual(got, []argLocation{eax, edx}) {
		t.Errorf("unique solution %v %v", got, err)
	}
	got, err = matchParameters("f", []paramKind{kindCount, kindFlags}, map[argLocation]roleSet{eax: {compared: true}, edx: {tested: true, compared: true}})
	if err != nil || !reflect.DeepEqual(got, []argLocation{eax, edx}) {
		t.Errorf("unique exact solution %v %v", got, err)
	}
	got, err = matchParameters("f", []paramKind{kindPointer, kindPointer}, map[argLocation]roleSet{s0: {loadBase: true}, eax: {loadBase: true}})
	if err != nil || !reflect.DeepEqual(got, []argLocation{eax, s0}) {
		t.Errorf("unique ordered solution %v %v", got, err)
	}
	got, err = matchParameters("f", []paramKind{kindIgnored, kindPointer}, map[argLocation]roleSet{edx: {loadBase: true}})
	if err != nil || !reflect.DeepEqual(got, []argLocation{{None: true}, edx}) {
		t.Errorf("ignored parameter %v %v", got, err)
	}
	_, err = matchParameters("f", []paramKind{kindStored, kindCombined}, map[argLocation]roleSet{eax: {stored: true}, edx: {stored: true}})
	if err == nil || !strings.Contains(err.Error(), "2 assignments of the inputs of f fit its declared parameters") {
		t.Errorf("ambiguous: %v", err)
	}
	_, err = matchParameters("f", []paramKind{kindIndex, kindPointer}, map[argLocation]roleSet{eax: {loadBase: true}, edx: {loadBase: true}})
	if err == nil || !strings.Contains(err.Error(), "no input of f fits each declared parameter (EAX=pointer EDX=pointer)") {
		t.Errorf("unsatisfiable: %v", err)
	}
	_, err = matchParameters("f", []paramKind{kindIndex, kindPointer}, map[argLocation]roleSet{eax: {scaled: true}})
	if err == nil || !strings.Contains(err.Error(), "f declares 2 parameters but takes 1 inputs (EAX=index)") {
		t.Errorf("count mismatch: %v", err)
	}
	orders := []struct {
		sol  []argLocation
		want bool
	}{
		{[]argLocation{eax, s0, stack(1)}, true},
		{[]argLocation{s0, eax}, false},
		{[]argLocation{stack(1), s0}, false},
		{[]argLocation{{None: true}, s0}, true},
		{[]argLocation{s0, s0}, false},
		{[]argLocation{eax, edx}, true},
		{nil, true},
	}
	for _, tc := range orders {
		if got := passedInOrder(tc.sol); got != tc.want {
			t.Errorf("%v in order %v", tc.sol, got)
		}
	}
	if eax.String() != "EAX" || stack(2).String() != "stack2" {
		t.Errorf("location names %s %s", eax, stack(2))
	}
}

// Checks the signature database sizes leave out the link time optimized library
func TestCanonicalArgBytesExcludesLTCG(t *testing.T) {
	if n, ok := canonicalArgBytes("D3DDevice_SetTexture"); !ok || n != 8 {
		t.Errorf("SetTexture canonical %d %v", n, ok)
	}
	if n, ok := canonicalArgBytes("D3DDevice_SetRenderState_ZEnable"); !ok || n != 4 {
		t.Errorf("ZEnable canonical %d %v", n, ok)
	}
	if _, ok := canonicalArgBytes("NoSuchRoutine"); ok {
		t.Errorf("an unknown name has a canonical size")
	}
	pats, err := loadPatterns()
	if err != nil {
		t.Fatal(err)
	}
	inLTCG := false
	for _, p := range pats {
		if sym := decorateSymbol(p.Symbol); sym.Name == "Collisions_SetRenderState_A" {
			if p.Lib != "d3d8ltcg" {
				t.Fatalf("Collisions_SetRenderState_A also appears in %s", p.Lib)
			}
			if sym.ArgBytes != 4 {
				t.Fatalf("Collisions_SetRenderState_A declares %d bytes", sym.ArgBytes)
			}
			inLTCG = true
		}
	}
	if !inLTCG {
		t.Fatalf("the optimized library carries no Collisions_SetRenderState_A")
	}
	if n, ok := canonicalArgBytes("Collisions_SetRenderState_A"); ok {
		t.Errorf("an optimized library size %d counted as canonical", n)
	}
}

func TestInputRolesAndAddressBases(t *testing.T) {
	ti := conventionImage(t)
	fn := named(ti, t, "declared", "D3DDevice_SetTexture", false, false)
	roles, err := ti.d.inputRoles(fn, []x86asm.Reg{x86asm.EAX, x86asm.EDX}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if r := roles[reg(x86asm.EAX)]; !r.scaled || r.loadBase || !r.combined || r.kind() != kindIndex {
		t.Errorf("index input roles %+v", r)
	}
	if r := roles[reg(x86asm.EDX)]; !r.loadBase || r.scaled || r.kind() != kindPointer {
		t.Errorf("pointer input roles %+v", r)
	}
	names := map[string]argLocation{"in.EAX": reg(x86asm.EAX), "in.EDX": reg(x86asm.EDX), "in.stack0": stack(0)}
	table := IConst{ti.at(t, "tableC")}
	bases, indexes := addressBases(ti.d, Bin{"add", table, Sym{"in.EAX"}}, names)
	if len(bases) != 0 || !reflect.DeepEqual(indexes, []argLocation{reg(x86asm.EAX)}) {
		t.Errorf("table index %v %v", bases, indexes)
	}
	bases, indexes = addressBases(ti.d, Bin{"add", Sym{"in.EDX"}, IConst{8}}, names)
	if !reflect.DeepEqual(bases, []argLocation{reg(x86asm.EDX)}) || len(indexes) != 0 {
		t.Errorf("pointer offset %v %v", bases, indexes)
	}
	bases, indexes = addressBases(ti.d, Bin{"add", Un{"zx16", Sym{"in.stack0"}}, Bin{"or", table, Sym{"in.EAX"}}}, names)
	if !reflect.DeepEqual(bases, []argLocation{stack(0)}) || !reflect.DeepEqual(indexes, []argLocation{reg(x86asm.EAX)}) {
		t.Errorf("mixed address %v %v", bases, indexes)
	}
	walked := map[argLocation]roleSet{reg(x86asm.EAX): {}, reg(x86asm.EDX): {}, stack(0): {}}
	walkInputUses(ti.d, Sel{Cond{"eq", Bin{"and", Sym{"in.stack0"}, IConst{4}}, IConst{0}}, Load{Addr: Bin{"add", Sym{"in.EDX"}, Bin{"shl", Sym{"in.EAX"}, IConst{2}}}, Size: 4}, IConst{0}}, names, walked)
	if r := walked[stack(0)]; !r.tested || r.scaled {
		t.Errorf("tested input %+v", r)
	}
	if r := walked[reg(x86asm.EDX)]; !r.loadBase {
		t.Errorf("load base %+v", r)
	}
	if r := walked[reg(x86asm.EAX)]; !r.scaled || r.loadBase {
		t.Errorf("shifted index %+v", r)
	}
	if !isInputSym(Sym{"in.EAX"}, names) || isInputSym(Sym{"other"}, names) || isInputSym(IConst{1}, names) {
		t.Errorf("input symbols misjudged")
	}
}
