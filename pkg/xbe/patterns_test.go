package xbe

import (
	"fmt"
	"strings"
	"sync"
	"testing"
)

// FLIRT crc16 of the IDA pattern tools, init 0xFFFF and polynomial 0x8408, written apart from the package's copy
func refCRC16(data []byte) uint16 {
	crc := uint16(0xFFFF)
	for _, b := range data {
		crc ^= uint16(b)
		for i := 0; i < 8; i++ {
			if crc&1 != 0 {
				crc = (crc >> 1) ^ 0x8408
			} else {
				crc >>= 1
			}
		}
	}
	crc = ^crc
	return crc<<8 | crc>>8
}

func hexMask(b []byte, wildcards int) string {
	s := strings.ToUpper(fmt.Sprintf("%x", b))
	return s + strings.Repeat("..", wildcards)
}

// Installs a hand built signature set for the test and restores the embedded one afterwards
func withPatternSet(t *testing.T, ps []Pattern) {
	t.Helper()
	real, err := loadPatterns()
	if err != nil {
		t.Fatalf("loading the embedded patterns: %v", err)
	}
	patternSet, patternErr = ps, nil
	t.Cleanup(func() {
		patternSet, patternErr = real, nil
		patternOnce = sync.Once{}
		patternOnce.Do(func() {})
	})
}

func TestFlirtCRC16(t *testing.T) {
	if flirtCRC16(nil) != 0 {
		t.Fatalf("empty crc %04X", flirtCRC16(nil))
	}
	for _, data := range [][]byte{{0x00}, {0xFF}, []byte("123456789"), {0x5E, 0xC2, 0x04, 0x00, 0xC7, 0x40, 0x04, 0x01}, make([]byte, 256)} {
		if got, want := flirtCRC16(data), refCRC16(data); got != want {
			t.Errorf("crc of %x is %04X, want %04X", data, got, want)
		}
	}
	if flirtCRC16([]byte{1, 2, 3}) == flirtCRC16([]byte{1, 2, 4}) {
		t.Fatal("crc does not distinguish a changed byte")
	}
}

func TestParseHexMask(t *testing.T) {
	if got := parseHexMask("55..8BEC"); len(got) != 4 || got[0] != 0x55 || got[1] != -1 || got[2] != 0x8B || got[3] != 0xEC {
		t.Fatalf("mask %v", got)
	}
	if got := parseHexMask(""); got == nil || len(got) != 0 {
		t.Fatalf("empty mask %v", got)
	}
	if parseHexMask("558") != nil || parseHexMask("5G") != nil || parseHexMask("5.") != nil {
		t.Fatal("malformed masks parsed")
	}
}

func TestParseDxbxPattern(t *testing.T) {
	prefix := hexMask([]byte{0x56, 0x8B, 0x35}, 4) + hexMask([]byte{0x56, 0xE8}, 4) + strings.Repeat("00", 19)
	line := "_D3DDevice_SetRenderState_CullMode@4 3911 d3d8 0070 " + prefix + " 05 B217 .......5EC2 ^0003D ?g_pDevice@D3D@@3PAVCDevice@1@A ^0009R _XMETAL_StartPush@4 :0010 ignored ^0025D _D3D__RenderState+01FC"
	p, err := parseDxbxPattern(line)
	if err != nil {
		t.Fatal(err)
	}
	if p.Symbol != "_D3DDevice_SetRenderState_CullMode@4" || p.Version != 3911 || p.Lib != "d3d8" || p.Length != 0x70 || p.CRCLen != 5 || p.CRC != 0xB217 {
		t.Fatalf("pattern %+v", p)
	}
	if len(p.Prefix) != 32 || p.Prefix[0] != 0x56 || p.Prefix[3] != -1 || p.Prefix[6] != -1 || p.Prefix[7] != 0x56 || p.Prefix[8] != 0xE8 || p.Prefix[9] != -1 || p.Prefix[13] != 0 {
		t.Fatalf("prefix %v", p.Prefix)
	}
	if p.Tail != nil {
		t.Fatalf("odd tail parsed as %v", p.Tail)
	}
	want := []PatternRef{{0x3, "?g_pDevice@D3D@@3PAVCDevice@1@A"}, {0x9, "_XMETAL_StartPush@4"}, {0x25, "_D3D__RenderState+01FC"}}
	if len(p.Refs) != 3 || p.Refs[0] != want[0] || p.Refs[1] != want[1] || p.Refs[2] != want[2] {
		t.Fatalf("refs %v", p.Refs)
	}
	withTail, err := parseDxbxPattern("_memcpy 4627 libcmt 0040 " + prefix + " 04 1234 ..5E5BC3 ^0010R _memset")
	if err != nil {
		t.Fatal(err)
	}
	if len(withTail.Tail) != 4 || withTail.Tail[0] != -1 || withTail.Tail[1] != 0x5E || withTail.Tail[3] != 0xC3 || len(withTail.Refs) != 1 || withTail.Refs[0] != (PatternRef{0x10, "_memset"}) {
		t.Fatalf("tail %v refs %v", withTail.Tail, withTail.Refs)
	}
	if _, err := parseDxbxPattern("_short 4627 libcmt 0040 55 00"); err == nil {
		t.Fatal("short line parsed")
	}
	if _, err := parseDxbxPattern("_bad 4627 libcmt 0040 558BEC 00 0000"); err != errMalformed {
		t.Fatalf("short prefix err %v", err)
	}
	if _, err := parseDxbxPattern("_bad x 4627 libcmt 0040 " + prefix + " 00 0000"); err == nil {
		t.Fatal("bad version parsed")
	}
	if _, err := parseDxbxPattern("_bad 4627 libcmt zz " + prefix + " 00 0000"); err == nil {
		t.Fatal("bad length parsed")
	}
	if _, err := parseDxbxPattern("_bad 4627 libcmt 0040 " + prefix + " zz 0000"); err == nil {
		t.Fatal("bad crc length parsed")
	}
}

func TestParseIDAPattern(t *testing.T) {
	prefix := hexMask([]byte{0x81, 0xEC, 0x5C, 0x02, 0x00, 0x00, 0x53, 0x55, 0x56, 0x8B, 0x35}, 4) + strings.Repeat("00", 17)
	p, err := parseIDAPattern(prefix + " 06 ABCD 0045 :0000 ?AntiAliasedCopy@D3D@@YGXPBUtagRECT@@@Z :0010 ?Second@D3D@@YGXXZ ^0010R ?MakeSpace@D3D@@YGPCKXZ 5E5BC3..")
	if err != nil {
		t.Fatal(err)
	}
	if p.Symbol != "?AntiAliasedCopy@D3D@@YGXPBUtagRECT@@@Z" || p.Lib != "d3d8ltcg" || p.Version != 0 || p.Length != 0x45 || p.CRCLen != 6 || p.CRC != 0xABCD {
		t.Fatalf("pattern %+v", p)
	}
	if len(p.Refs) != 1 || p.Refs[0] != (PatternRef{0x10, "?MakeSpace@D3D@@YGPCKXZ"}) {
		t.Fatalf("refs %v", p.Refs)
	}
	if len(p.Tail) != 4 || p.Tail[0] != 0x5E || p.Tail[2] != 0xC3 || p.Tail[3] != -1 {
		t.Fatalf("tail %v", p.Tail)
	}
	if p.Prefix[11] != -1 || p.Prefix[15] != 0 {
		t.Fatalf("prefix %v", p.Prefix)
	}
	if _, err := parseIDAPattern(prefix + " 00 0000 0000"); err == nil {
		t.Fatal("line without a symbol parsed")
	}
	if _, err := parseIDAPattern("55 00 0000 0000 :0000 _x"); err != errMalformed {
		t.Fatalf("short prefix err %v", err)
	}
	if _, err := parseIDAPattern(prefix + " 00"); err == nil {
		t.Fatal("short line parsed")
	}
}

func TestParseRef(t *testing.T) {
	if r := parseRef("^001ER", "_XMemAlloc@8"); r != (PatternRef{0x1E, "_XMemAlloc@8"}) {
		t.Fatalf("%+v", r)
	}
	if r := parseRef("^0025D", "_D3D__RenderState+01FC"); r != (PatternRef{0x25, "_D3D__RenderState+01FC"}) {
		t.Fatalf("%+v", r)
	}
	if r := parseRef("^0010", "x"); r.Off != 0x10 {
		t.Fatalf("%+v", r)
	}
}

func crcPatternCode() []byte {
	code := []byte{0x55, 0x8B, 0xEC, 0x83, 0xEC, 0x10}
	code = append(code, 0xC7, 0x45, 0xFC, 0x11, 0x11, 0x11, 0x11)
	code = append(code, 0xC7, 0x45, 0xF8, 0x22, 0x22, 0x22, 0x22)
	code = append(code, 0xC7, 0x45, 0xF4, 0x33, 0x33, 0x33, 0x33)
	code = append(code, 0x8B, 0x45, 0xFC, 0x03, 0x45, 0xF8, 0x33, 0x45, 0xF4, 0xC9, 0xC3)
	return append(code, 0xCC, 0xCC)
}

func crcPatternLine() string {
	code := crcPatternCode()
	return fmt.Sprintf("_CrcChecked 4627 libcmt 0026 %s 06 %04X CCCC", hexMask(code[:32], 0), refCRC16(code[32:38]))
}

func TestPatternMatches(t *testing.T) {
	code := crcPatternCode()
	p, err := parseDxbxPattern(crcPatternLine())
	if err != nil {
		t.Fatal(err)
	}
	if p.CRC != refCRC16(code[32:38]) || p.significant() != 40 {
		t.Fatalf("pattern %+v significant %d", p, p.significant())
	}
	if !p.matches(code) {
		t.Fatal("exact code does not match")
	}
	if !p.matches(append(append([]byte(nil), code...), 0x90, 0x90)) {
		t.Fatal("longer code does not match")
	}
	if p.matches(code[:39]) {
		t.Fatal("code short of the tail matches")
	}
	if p.matches(code[:35]) {
		t.Fatal("code short of the crc region matches")
	}
	changed := append([]byte(nil), code...)
	changed[9] = 0x12
	if p.matches(changed) {
		t.Fatal("changed prefix byte matches")
	}
	changed = append([]byte(nil), code...)
	changed[34] ^= 1
	if p.matches(changed) {
		t.Fatal("changed crc byte matches")
	}
	changed = append([]byte(nil), code...)
	changed[38] = 0x90
	if p.matches(changed) {
		t.Fatal("changed tail byte matches")
	}
	wild, err := parseDxbxPattern("_Wild 4627 libcmt 0026 " + hexMask(code[:6], 26) + " 00 0000")
	if err != nil {
		t.Fatal(err)
	}
	changed = append([]byte(nil), code...)
	for i := 6; i < 32; i++ {
		changed[i] ^= 0xFF
	}
	if !wild.matches(changed) || wild.significant() != 6 {
		t.Fatalf("wildcards %v significant %d", wild.matches(changed), wild.significant())
	}
	if wild.matches(code[:20]) {
		t.Fatal("code shorter than the prefix matches")
	}
}

func TestDecorateSymbol(t *testing.T) {
	cases := []struct {
		raw  string
		want Symbol
	}{
		{"_memcpy", Symbol{Name: "memcpy", ArgBytes: -1, Cdecl: true}},
		{"__cinit", Symbol{Name: "_cinit", ArgBytes: -1, Cdecl: true}},
		{"_XMemAlloc@8", Symbol{Name: "XMemAlloc", ArgBytes: 8}},
		{"_Func@abc", Symbol{Name: "Func@abc", ArgBytes: -1, Cdecl: true}},
		{"@D3DDevice_SetRenderState_Simple@8", Symbol{Name: "D3DDevice_SetRenderState_Simple", ArgBytes: 8, FastCall: true}},
		{"@Fast", Symbol{Name: "Fast", ArgBytes: -1, FastCall: true}},
		{"??2@YAPAXI@Z", Symbol{Name: "operator_new", ArgBytes: -1, Cdecl: true}},
		{"??3@YAXPAX@Z", Symbol{Name: "operator_delete", ArgBytes: -1, Cdecl: true}},
		{"??_U@YAPAXI@Z", Symbol{Name: "operator_new_array", ArgBytes: -1, Cdecl: true}},
		{"??_V@YAXPAX@Z", Symbol{Name: "operator_delete_array", ArgBytes: -1, Cdecl: true}},
		{"?swprintf@@YAHPA_WIPB_WZZ", Symbol{Name: "_snwprintf", ArgBytes: -1, Cdecl: true}},
		{"?MakeSpace@D3D@@YGPCKXZ", Symbol{Name: "D3D::MakeSpace", ArgBytes: -1}},
		{"?XMalloc@@YAJPAPAXI@Z", Symbol{Name: "XMalloc", ArgBytes: -1, Cdecl: true}},
		{"?BindToChannel@CMiniport@D3D@@QAEHPAUOBJECTINFO@12@@Z", Symbol{Name: "CMiniport::BindToChannel", ArgBytes: -1}},
		{"?Member@Klass@@QAAXXZ", Symbol{Name: "Klass::Member", ArgBytes: -1, Cdecl: true}},
		{"?Static@Klass@@SAXXZ", Symbol{Name: "Klass::Static", ArgBytes: -1, Cdecl: true}},
		{"D3DXBoxBoundProbe", Symbol{Name: "D3DXBoxBoundProbe", ArgBytes: -1}},
	}
	for _, c := range cases {
		got := decorateSymbol(c.raw)
		want := c.want
		want.Raw = c.raw
		if got.Raw != want.Raw || got.Name != want.Name || got.ArgBytes != want.ArgBytes || got.FastCall != want.FastCall || got.Cdecl != want.Cdecl {
			t.Errorf("%s decorates to %+v, want %+v", c.raw, got, want)
		}
	}
}

func TestMangledIsCdecl(t *testing.T) {
	cases := map[string]bool{"?f@@YAXXZ": true, "?f@@YGXXZ": false, "?f@@YIXXZ": false, "?m@K@@QAEXXZ": false, "?m@K@@QAAXXZ": true, "?m@K@@AEAXXZ": true, "?m@K@@IAGXXZ": false, "?g@@3HA": false, "plain": false}
	for raw, want := range cases {
		if got := mangledIsCdecl(raw); got != want {
			t.Errorf("%s cdecl %v, want %v", raw, got, want)
		}
	}
}

func libraryImage(t *testing.T, libs []LibVersion) *Image {
	t.Helper()
	a := newAsm(0x11000)
	a.label("entry")
	for _, f := range []string{"strong", "caller", "pusher", "jumper", "ltcg", "crc"} {
		a.rel32([]byte{0xE8}, f)
	}
	a.emit(0xC3)
	a.align(16, 0xCC)
	a.label("strong")
	a.emit(0x55, 0x8B, 0xEC, 0xB8, 0xCD, 0xAB, 0x34, 0x12, 0x5D, 0xC2, 0x08, 0x00)
	a.align(16, 0xCC)
	a.label("caller")
	a.rel32([]byte{0xE8}, "strong")
	a.emit(0xC3)
	a.align(16, 0xCC)
	a.label("pusher")
	a.abs32([]byte{0x68}, "strong", 0xC3)
	a.align(16, 0xCC)
	a.label("jumper")
	a.emit(0xFF, 0x25)
	a.u32(0x12000)
	a.align(16, 0xCC)
	a.label("ltcg")
	a.emit(0x8B, 0x44, 0x24, 0x04, 0x05, 0x57, 0x7E, 0x00, 0x00, 0xC2, 0x04, 0x00)
	a.align(16, 0xCC)
	a.label("crc")
	a.emit(crcPatternCode()...)
	a.align(16, 0xCC)
	return parseXBE(t, xbeSpec{
		entry:  0x11000,
		kthunk: 0x12000,
		sections: []testSection{
			{name: ".text", va: 0x11000, flags: flagExecutable, data: a.bytes(), vsize: 0x400},
			{name: ".data", va: 0x12000, flags: flagWritable, data: thunkTable(145), vsize: 0x100},
		},
		libs: libs,
	})
}

func testPatternSet(t *testing.T) []Pattern {
	t.Helper()
	strong := hexMask([]byte{0x55, 0x8B, 0xEC, 0xB8, 0xCD, 0xAB, 0x34, 0x12, 0x5D, 0xC2, 0x08, 0x00}, 20)
	lines := []string{
		"_StrongOne@8 4627 d3d8 000C " + strong + " 00 0000",
		"_OlderOne@8 4361 d3d8 000C " + strong + " 00 0000",
		"_AlsoStrong@8 4627 d3d8 000C " + strong + " 00 0000",
		"_WeakCaller 4627 xapilib 0006 E8........C3" + strings.Repeat("..", 26) + " 00 0000 ^0001R _AlsoStrong@8",
		"_WeakPusher 4627 xapilib 0006 68........C3" + strings.Repeat("..", 26) + " 00 0000 ^0001R _AlsoStrong@8",
		"_ImportJumper 4627 xapilib 0006 FF25........" + strings.Repeat("..", 26) + " 00 0000 ^0002R KeSetEvent",
		crcPatternLine(),
	}
	var ps []Pattern
	for _, l := range lines {
		p, err := parseDxbxPattern(l)
		if err != nil {
			t.Fatalf("%s: %v", l, err)
		}
		ps = append(ps, p)
	}
	ltcg, err := parseIDAPattern(hexMask([]byte{0x8B, 0x44, 0x24, 0x04, 0x05, 0x57, 0x7E, 0x00, 0x00, 0xC2, 0x04, 0x00}, 20) + " 00 0000 000C :0000 ?LtcgOnly@D3D@@YGXXZ")
	if err != nil {
		t.Fatal(err)
	}
	return append(ps, ltcg)
}

func symbolNames(d *Disassembly) map[uint32]string {
	out := map[uint32]string{}
	for va, fn := range d.Functions {
		if fn.Symbol != nil {
			out[va] = fn.Symbol.Name
		}
	}
	return out
}

func TestIdentifyLibraryFunctionsExactBuild(t *testing.T) {
	withPatternSet(t, testPatternSet(t))
	img := libraryImage(t, []LibVersion{{Name: "D3D8", Build: 4627}})
	d := disassembleImage(t, img)
	syms, err := d.IdentifyLibraryFunctions()
	if err != nil {
		t.Fatal(err)
	}
	want := map[uint32]string{0x11020: "AlsoStrong", 0x11030: "WeakCaller", 0x11040: "WeakPusher", 0x11050: "ImportJumper", 0x11070: "CrcChecked"}
	got := symbolNames(d)
	if len(got) != len(want) {
		t.Fatalf("symbols %v, want %v", got, want)
	}
	for va, name := range want {
		if got[va] != name {
			t.Errorf("%08X named %q, want %q", va, got[va], name)
		}
	}
	if len(syms) != 5 || syms[0].VA != 0x11020 || syms[4].VA != 0x11070 || len(d.Symbols) != 5 {
		t.Fatalf("returned %+v", syms)
	}
	strong := d.Functions[0x11020].Symbol
	if strong.Raw != "_AlsoStrong@8" || strong.Version != 4627 || strong.Lib != "d3d8" || strong.ArgBytes != 8 || !equalStrings(strong.Alternates, []string{"_StrongOne@8"}) || d.Functions[0x11020].Name != "AlsoStrong" {
		t.Fatalf("strong symbol %+v", strong)
	}
	if caller := d.Functions[0x11030].Symbol; caller.Lib != "xapilib" || !caller.Cdecl || caller.ArgBytes != -1 {
		t.Fatalf("caller symbol %+v", caller)
	}
	if d.Functions[0x11060].Symbol != nil {
		t.Fatalf("ltcg function named %+v without an ltcg library", d.Functions[0x11060].Symbol)
	}
}

func TestIdentifyLibraryFunctionsNearestBuild(t *testing.T) {
	withPatternSet(t, testPatternSet(t))
	d := disassembleImage(t, libraryImage(t, []LibVersion{{Name: "D3D8", Build: 4400}}))
	strong := d.Functions[0x11020].Symbol
	if strong == nil || strong.Name != "OlderOne" || strong.Version != 4361 || len(strong.Alternates) != 0 {
		t.Fatalf("strong symbol %+v", strong)
	}
	if d.Functions[0x11030].Symbol != nil || d.Functions[0x11040].Symbol != nil || d.Functions[0x11050].Symbol == nil || d.Functions[0x11070].Symbol == nil {
		t.Fatalf("weak symbols %v", symbolNames(d))
	}
	far := disassembleImage(t, libraryImage(t, []LibVersion{{Name: "D3D8", Build: 4500}}))
	if sym := far.Functions[0x11020].Symbol; sym == nil || sym.Name != "AlsoStrong" || !equalStrings(sym.Alternates, []string{"_StrongOne@8"}) {
		t.Fatalf("equidistant symbol %+v", sym)
	}
}

func TestIdentifyLibraryFunctionsLTCG(t *testing.T) {
	withPatternSet(t, testPatternSet(t))
	d := disassembleImage(t, libraryImage(t, []LibVersion{{Name: "D3D8", Build: 4627}, {Name: "D3D8LTCG", Build: 4627}}))
	sym := d.Functions[0x11060].Symbol
	if sym == nil || sym.Name != "D3D::LtcgOnly" || sym.Lib != "d3d8ltcg" || sym.Version != 0 || sym.Raw != "?LtcgOnly@D3D@@YGXXZ" {
		t.Fatalf("ltcg symbol %+v", sym)
	}
	only := disassembleImage(t, libraryImage(t, []LibVersion{{Name: "D3D8LTCG", Build: 4361}}))
	if sym := only.Functions[0x11020].Symbol; sym == nil || sym.Name != "OlderOne" {
		t.Fatalf("ltcg build symbol %+v", sym)
	}
	if sym := only.Functions[0x11060].Symbol; sym == nil || sym.Name != "D3D::LtcgOnly" {
		t.Fatalf("ltcg only symbol %+v", sym)
	}
}

func TestIdentifyLibraryFunctionsNeedsD3D8(t *testing.T) {
	img := libraryImage(t, []LibVersion{{Name: "XAPILIB", Build: 4627}})
	d := &Disassembly{Image: img, Functions: map[uint32]*Function{}, Imports: map[uint32]string{}}
	if _, err := d.IdentifyLibraryFunctions(); err == nil || !strings.Contains(err.Error(), "no D3D8") {
		t.Fatalf("err %v", err)
	}
	if _, err := Disassemble(img); err == nil {
		t.Fatal("image without a D3D8 version disassembled")
	}
}

func TestRefsResolve(t *testing.T) {
	withPatternSet(t, testPatternSet(t))
	d := disassembleImage(t, libraryImage(t, []LibVersion{{Name: "D3D8", Build: 4627}}))
	if !d.refsResolve(0x11030, &Pattern{Refs: []PatternRef{{1, "_AlsoStrong@8"}}}) {
		t.Fatal("relative reference to the strong symbol does not resolve")
	}
	if d.refsResolve(0x11030, &Pattern{Refs: []PatternRef{{1, "_Other@8"}}}) {
		t.Fatal("reference to another name resolves")
	}
	if !d.refsResolve(0x11040, &Pattern{Refs: []PatternRef{{1, "_AlsoStrong@8"}}}) {
		t.Fatal("absolute reference does not resolve")
	}
	if !d.refsResolve(0x11050, &Pattern{Refs: []PatternRef{{2, "KeSetEvent"}}}) {
		t.Fatal("import reference does not resolve")
	}
	if d.refsResolve(0x11050, &Pattern{}) {
		t.Fatal("pattern without references resolves")
	}
	if d.refsResolve(0x113FE, &Pattern{Refs: []PatternRef{{1, "_AlsoStrong@8"}}}) {
		t.Fatal("reference past the section resolves")
	}
}

func TestEmbeddedPatternsLoad(t *testing.T) {
	pats, err := loadPatterns()
	if err != nil {
		t.Fatal(err)
	}
	if len(pats) < 60000 {
		t.Fatalf("only %d patterns", len(pats))
	}
	dxbx, err := readPatternFile("patterns/dxbx.pat.gz")
	if err != nil {
		t.Fatal(err)
	}
	ltcg, err := readPatternFile("patterns/d3d8ltcg.pat.gz")
	if err != nil {
		t.Fatal(err)
	}
	if len(dxbx)+len(ltcg) != len(pats) || len(ltcg) < 1000 {
		t.Fatalf("file counts %d %d of %d", len(dxbx), len(ltcg), len(pats))
	}
	if _, err := readPatternFile("patterns/missing.pat.gz"); err == nil {
		t.Fatal("missing file read")
	}
	var cull, ltcgFlip *Pattern
	libs := map[string]bool{}
	for i := range pats {
		p := &pats[i]
		libs[p.Lib] = true
		if p.Symbol == "_D3DDevice_SetRenderState_CullMode@4" && p.Version == 3911 && cull == nil {
			cull = p
		}
		if p.Symbol == "?AliasedFlip@D3D@@YGXXZ" && p.Lib == "d3d8ltcg" && ltcgFlip == nil {
			ltcgFlip = p
		}
		if len(p.Prefix) != 32 {
			t.Fatalf("pattern %s has a %d byte prefix", p.Symbol, len(p.Prefix))
		}
	}
	for _, lib := range []string{"d3d8", "d3dx8", "xapilib", "libcmt", "xgraphics", "d3d8ltcg"} {
		if !libs[lib] {
			t.Errorf("no %s patterns", lib)
		}
	}
	if cull == nil || cull.Lib != "d3d8" || cull.Length != 0x70 || cull.CRCLen != 5 || cull.CRC != 0xB217 || cull.Prefix[0] != 0x56 || cull.Prefix[3] != -1 {
		t.Fatalf("cull mode pattern %+v", cull)
	}
	if len(cull.Refs) < 2 || cull.Refs[0] != (PatternRef{0x3, "?g_pDevice@D3D@@3PAVCDevice@1@A"}) || cull.Refs[1] != (PatternRef{0x9, "_XMETAL_StartPush@4"}) {
		t.Fatalf("cull mode refs %v", cull.Refs)
	}
	if ltcgFlip == nil || ltcgFlip.Lib != "d3d8ltcg" || ltcgFlip.Version != 0 || ltcgFlip.Prefix[0] != 0x56 || ltcgFlip.Prefix[3] != -1 {
		t.Fatalf("ltcg pattern %+v", ltcgFlip)
	}
}
