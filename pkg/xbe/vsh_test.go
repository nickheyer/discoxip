package xbe

import (
	"strings"
	"testing"
)

type vsPos struct{ word, bit, len uint }

var vsTestFields = map[string]vsPos{
	"ILU": {1, 25, 3}, "MAC": {1, 21, 4}, "CONST": {1, 13, 8}, "V": {1, 9, 4},
	"A_NEG": {1, 8, 1}, "A_SWZ_X": {1, 6, 2}, "A_SWZ_Y": {1, 4, 2}, "A_SWZ_Z": {1, 2, 2}, "A_SWZ_W": {1, 0, 2}, "A_R": {2, 28, 4}, "A_MUX": {2, 26, 2},
	"B_NEG": {2, 25, 1}, "B_SWZ_X": {2, 23, 2}, "B_SWZ_Y": {2, 21, 2}, "B_SWZ_Z": {2, 19, 2}, "B_SWZ_W": {2, 17, 2}, "B_R": {2, 13, 4}, "B_MUX": {2, 11, 2},
	"C_NEG": {2, 10, 1}, "C_SWZ_X": {2, 8, 2}, "C_SWZ_Y": {2, 6, 2}, "C_SWZ_Z": {2, 4, 2}, "C_SWZ_W": {2, 2, 2}, "C_R_HIGH": {2, 0, 2}, "C_R_LOW": {3, 30, 2}, "C_MUX": {3, 28, 2},
	"OUT_MAC_MASK": {3, 24, 4}, "OUT_R": {3, 20, 4}, "OUT_ILU_MASK": {3, 16, 4}, "OUT_O_MASK": {3, 12, 4}, "OUT_ORB": {3, 11, 1}, "OUT_ADDRESS": {3, 3, 8}, "OUT_MUX": {3, 2, 1},
	"A0X": {3, 1, 1}, "FINAL": {3, 0, 1},
}

const (
	vsMacMOV = 1
	vsMacMAD = 4
	vsMacDP4 = 7
	vsMacARL = 13
	vsIluRCP = 2
	vsIluRSQ = 4
	vsMuxR   = 1
	vsMuxV   = 2
	vsMuxC   = 3
)

type vsEnc struct{ t [4]uint32 }

func (e *vsEnc) set(name string, v uint32) {
	f, ok := vsTestFields[name]
	if !ok {
		panic("vsh test: no field " + name)
	}
	e.t[f.word] |= (v & ((1 << f.len) - 1)) << f.bit
}

func (e *vsEnc) swizzle(prefix, swz string) {
	for i, c := range []string{"X", "Y", "Z", "W"} {
		e.set(prefix+"_SWZ_"+c, uint32(strings.IndexByte("xyzw", swz[i])))
	}
}

func (e *vsEnc) operand(prefix string, mux, index uint32, swz string, neg bool) {
	e.set(prefix+"_MUX", mux)
	e.swizzle(prefix, swz)
	if neg {
		e.set(prefix+"_NEG", 1)
	}
	switch {
	case mux == vsMuxV:
		e.set("V", index)
	case mux == vsMuxC:
		e.set("CONST", index+96)
	case prefix == "C":
		e.set("C_R_HIGH", index>>2)
		e.set("C_R_LOW", index&3)
	default:
		e.set(prefix+"_R", index)
	}
}

func (e *vsEnc) output(mask, address uint32, ilu bool) {
	e.set("OUT_O_MASK", mask)
	e.set("OUT_ORB", 1)
	e.set("OUT_ADDRESS", address)
	if ilu {
		e.set("OUT_MUX", 1)
	}
}

// Stores subtokens one to three first and the unused subtoken zero last, as the image does
func (e *vsEnc) bytes() []byte {
	return words32(e.t[1], e.t[2], e.t[3], e.t[0])
}

func xvsBlob(count int, instructions ...*vsEnc) []byte {
	out := words32(uint32(vshVersionXVS)|uint32(count)<<16, 0)
	for _, in := range instructions {
		out = append(out, in.bytes()...)
	}
	return out
}

func sampleProgram() []*vsEnc {
	mad := &vsEnc{}
	mad.set("MAC", vsMacMAD)
	mad.operand("A", vsMuxV, 0, "xyzw", false)
	mad.operand("B", vsMuxC, 4, "xyzw", false)
	mad.operand("C", vsMuxR, 2, "wzyx", true)
	mad.set("OUT_MAC_MASK", 0xE)
	mad.set("OUT_R", 1)
	mad.set("ILU", vsIluRSQ)
	mad.set("OUT_ILU_MASK", 0x1)

	dp4 := &vsEnc{}
	dp4.set("MAC", vsMacDP4)
	dp4.operand("A", vsMuxV, 0, "xyzw", false)
	dp4.operand("B", vsMuxC, 0, "xyzw", false)
	dp4.output(0x8, 0, false)

	arl := &vsEnc{}
	arl.set("MAC", vsMacARL)
	arl.operand("A", vsMuxV, 1, "xxxx", false)
	arl.output(0x8, 15, false)

	rcp := &vsEnc{}
	rcp.set("ILU", vsIluRCP)
	negative := int32(-6)
	rcp.operand("C", vsMuxC, uint32(negative), "xyzw", false)
	rcp.set("OUT_R", 3)
	rcp.set("OUT_ILU_MASK", 0x8)
	rcp.output(0x1, 5, true)

	mirror := &vsEnc{}
	mirror.set("MAC", vsMacMOV)
	mirror.operand("A", vsMuxR, 12, "xyzw", false)
	mirror.output(0xC, 9, false)

	mov := &vsEnc{}
	mov.set("MAC", vsMacMOV)
	mov.operand("A", vsMuxV, 3, "xyzw", false)
	mov.output(0xF, 3, false)
	mov.set("FINAL", 1)
	return []*vsEnc{mad, dp4, arl, rcp, mirror, mov}
}

func programImage(t *testing.T, blob []byte) *Image {
	t.Helper()
	spec := sampleSpec()
	spec.sections = append(spec.sections, testSection{name: ".vsh", va: 0x13000, data: blob, vsize: 0x200})
	return parseXBE(t, spec)
}

func TestDecodeVertexProgramFields(t *testing.T) {
	ins := sampleProgram()
	img := programImage(t, xvsBlob(7, ins...))
	prog, err := img.DecodeVertexProgram(0x13000)
	if err != nil {
		t.Fatal(err)
	}
	if prog.VA != 0x13000 || len(prog.Instructions) != 6 {
		t.Fatalf("program %08X with %d instructions", prog.VA, len(prog.Instructions))
	}
	mad := prog.Instructions[0]
	if mad.Slot != 0 || mad.MAC != "MAD" || mad.ILU != "RSQ" || mad.Final {
		t.Fatalf("slot 0 %+v", mad)
	}
	if *mad.A != (VSOperand{Kind: "v", Index: 0, Swizzle: "xyzw"}) || *mad.B != (VSOperand{Kind: "c", Index: 4, Swizzle: "xyzw"}) || *mad.C != (VSOperand{Kind: "r", Index: 2, Swizzle: "wzyx", Negate: true}) {
		t.Fatalf("slot 0 operands %+v %+v %+v", mad.A, mad.B, mad.C)
	}
	if !equalStrings(mad.MACDst, []string{"R1.xyz"}) || !equalStrings(mad.ILUDst, []string{"R1.w"}) {
		t.Fatalf("slot 0 destinations %v %v", mad.MACDst, mad.ILUDst)
	}
	stored := ins[0].bytes()
	for w := 0; w < 4; w++ {
		if mad.Raw[w] != le32ToU32(stored[w*4:]) {
			t.Fatalf("slot 0 raw word %d %08X", w, mad.Raw[w])
		}
	}
	dp4 := prog.Instructions[1]
	if dp4.MAC != "DP4" || dp4.ILU != "" || !equalStrings(dp4.MACDst, []string{"oPos.x"}) || dp4.ILUDst != nil || *dp4.B != (VSOperand{Kind: "c", Index: 0, Swizzle: "xyzw"}) {
		t.Fatalf("slot 1 %+v", dp4)
	}
	arl := prog.Instructions[2]
	if arl.MAC != "ARL" || !equalStrings(arl.MACDst, []string{"A0.x"}) || *arl.A != (VSOperand{Kind: "v", Index: 1, Swizzle: "xxxx"}) {
		t.Fatalf("slot 2 %+v", arl)
	}
	rcp := prog.Instructions[3]
	if rcp.MAC != "" || rcp.ILU != "RCP" || rcp.MACDst != nil || !equalStrings(rcp.ILUDst, []string{"R3.x", "oFog.w"}) || *rcp.C != (VSOperand{Kind: "c", Index: -6, Swizzle: "xyzw"}) {
		t.Fatalf("slot 3 %+v", rcp)
	}
	mirror := prog.Instructions[4]
	if mirror.MAC != "MOV" || *mirror.A != (VSOperand{Kind: "r", Index: 12, Swizzle: "xyzw"}) || !equalStrings(mirror.MACDst, []string{"oT0.xy"}) {
		t.Fatalf("slot 4 %+v", mirror)
	}
	mov := prog.Instructions[5]
	if mov.MAC != "MOV" || !mov.Final || !equalStrings(mov.MACDst, []string{"oD0.xyzw"}) || *mov.A != (VSOperand{Kind: "v", Index: 3, Swizzle: "xyzw"}) {
		t.Fatalf("slot 5 %+v", mov)
	}
	if !equalInts(prog.Inputs, []int{0, 1, 3}) || !equalInts(prog.Constants, []int{-6, 0, 4}) || !equalStrings(prog.Outputs, []string{"A0", "oD0", "oFog", "oPos", "oT0"}) {
		t.Fatalf("uses %v %v %v", prog.Inputs, prog.Constants, prog.Outputs)
	}
	wantAsm := []string{"MAD R1.xyz, v0, c4, -R2.wzyx", "RSQ R1.w, -R2.wzyx", "DP4 oPos.x, v0, c0", "ARL A0.x, v1.xxxx", "RCP R3.x+oFog.w, c-6", "MOV oT0.xy, R12", "MOV oD0.xyzw, v3"}
	if !equalStrings(strings.Split(prog.Asm, "\n"), wantAsm) {
		t.Fatalf("asm\n%s", prog.Asm)
	}
}

func TestDecodeVertexProgramGLSL(t *testing.T) {
	img := programImage(t, xvsBlob(6, sampleProgram()...))
	prog, err := img.DecodeVertexProgram(0x13000)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(prog.GLSL, "\n"), "\n")
	if len(lines) < 3 || !strings.HasPrefix(lines[0], "vec4 R0=vec4(0.0),") || !strings.HasPrefix(lines[1], "vec4 oPos=vec4(0.0),") || lines[2] != "vec4 tmp; vec4 ilu;" {
		t.Fatalf("glsl header %q", lines[:3])
	}
	want := []string{
		"tmp = (v0.xyzw * c[4].xyzw + (-R2.wzyx));",
		"R1.xyz = tmp.xyz;",
		"ilu = vec4(inversesqrt(abs((-R2.wzyx).x)));",
		"R1.w = ilu.w;",
		"tmp = vec4(dot(v0.xyzw, c[0].xyzw));",
		"oPos.x = tmp.x;",
		"tmp = vec4(floor(v1.xxxx.x + 0.001));",
		"A0.x = tmp.x;",
		"ilu = vec4(1.0 / cneg6.xyzw.x);",
		"R3.x = ilu.x;",
		"oFog.w = ilu.w;",
		"tmp = oPos.xyzw;",
		"oT0.xy = tmp.xy;",
		"tmp = v3.xyzw;",
		"oD0.xyzw = tmp.xyzw;",
	}
	if !equalStrings(lines[3:], want) {
		t.Fatalf("glsl body\n%s", strings.Join(lines[3:], "\n"))
	}
}

func TestDecodeVertexProgramEveryOperation(t *testing.T) {
	macs := map[int]string{2: "(R1.xyzw * R2.xyzw)", 3: "(R1.xyzw + R3.xyzw)", 5: "vec4(dot(R1.xyzw.xyz, R2.xyzw.xyz))", 6: "vec4(dot(vec4(R1.xyzw.xyz, 1.0), R2.xyzw))", 8: "vec4(1.0, R1.xyzw.y * R2.xyzw.y, R1.xyzw.z, R2.xyzw.w)", 9: "min(R1.xyzw, R2.xyzw)", 10: "max(R1.xyzw, R2.xyzw)", 11: "vec4(lessThan(R1.xyzw, R2.xyzw))", 12: "vec4(greaterThanEqual(R1.xyzw, R2.xyzw))"}
	names := map[int]string{2: "MUL", 3: "ADD", 5: "DP3", 6: "DPH", 8: "DST", 9: "MIN", 10: "MAX", 11: "SLT", 12: "SGE"}
	for code, expr := range macs {
		e := &vsEnc{}
		e.set("MAC", uint32(code))
		e.operand("A", vsMuxR, 1, "xyzw", false)
		e.operand("B", vsMuxR, 2, "xyzw", false)
		e.operand("C", vsMuxR, 3, "xyzw", false)
		e.set("OUT_MAC_MASK", 0xF)
		e.set("OUT_R", 4)
		e.set("FINAL", 1)
		prog, err := programImage(t, xvsBlob(1, e)).DecodeVertexProgram(0x13000)
		if err != nil {
			t.Fatalf("mac %d: %v", code, err)
		}
		if prog.Instructions[0].MAC != names[code] || !strings.Contains(prog.GLSL, "tmp = "+expr+";\nR4.xyzw = tmp.xyzw;\n") {
			t.Errorf("mac %d decodes as %s with\n%s", code, prog.Instructions[0].MAC, prog.GLSL)
		}
	}
	ilus := map[int]string{1: "R5.xyzw", 3: "vec4(clamp(1.0 / R5.xyzw.x, -1.84467441e19, 1.84467441e19))", 5: "vec4(exp2(floor(R5.xyzw.x)), fract(R5.xyzw.x), exp2(R5.xyzw.x), 1.0)", 6: "vec4(floor(log2(abs(R5.xyzw.x))), abs(R5.xyzw.x) / exp2(floor(log2(abs(R5.xyzw.x)))), log2(abs(R5.xyzw.x)), 1.0)", 7: "vec4(1.0, max(R5.xyzw.x, 0.0), (R5.xyzw.x > 0.0) ? pow(max(R5.xyzw.y, 0.0), clamp(R5.xyzw.w, -127.9961, 127.9961)) : 0.0, 1.0)"}
	iluNames := map[int]string{1: "MOV", 3: "RCC", 5: "EXP", 6: "LOG", 7: "LIT"}
	for code, expr := range ilus {
		e := &vsEnc{}
		e.set("ILU", uint32(code))
		e.operand("C", vsMuxR, 5, "xyzw", false)
		e.set("OUT_ILU_MASK", 0xF)
		e.set("OUT_R", 6)
		e.set("FINAL", 1)
		prog, err := programImage(t, xvsBlob(1, e)).DecodeVertexProgram(0x13000)
		if err != nil {
			t.Fatalf("ilu %d: %v", code, err)
		}
		if prog.Instructions[0].ILU != iluNames[code] || !strings.Contains(prog.GLSL, "ilu = "+expr+";\nR6.xyzw = ilu.xyzw;\n") {
			t.Errorf("ilu %d decodes as %s with\n%s", code, prog.Instructions[0].ILU, prog.GLSL)
		}
	}
}

func TestDecodeVertexProgramErrors(t *testing.T) {
	ins := sampleProgram()
	if _, err := programImage(t, xvsBlob(6, ins...)).DecodeVertexProgram(0x13004); err == nil || !strings.Contains(err.Error(), "no XVS header") {
		t.Fatalf("header err %v", err)
	}
	if _, err := programImage(t, xvsBlob(5, ins[:5]...)).DecodeVertexProgram(0x13000); err == nil || !strings.Contains(err.Error(), "no final instruction") {
		t.Fatalf("final err %v", err)
	}
	if _, err := programImage(t, xvsBlob(0)).DecodeVertexProgram(0x13000); err == nil || !strings.Contains(err.Error(), "no final instruction") {
		t.Fatalf("empty err %v", err)
	}
	if _, err := programImage(t, xvsBlob(40, ins...)).DecodeVertexProgram(0x13000); err == nil || !strings.Contains(err.Error(), "truncated") {
		t.Fatalf("truncated err %v", err)
	}
	bad := &vsEnc{}
	bad.set("MAC", vsMacMOV)
	bad.operand("A", vsMuxV, 0, "xyzw", false)
	bad.set("OUT_O_MASK", 0xF)
	bad.set("OUT_ADDRESS", 3)
	bad.set("FINAL", 1)
	if _, err := programImage(t, xvsBlob(1, bad)).DecodeVertexProgram(0x13000); err == nil || !strings.Contains(err.Error(), "writes a constant register") {
		t.Fatalf("constant err %v", err)
	}
}

func TestOperandStrings(t *testing.T) {
	cases := []struct {
		op   VSOperand
		text string
		glsl string
	}{
		{VSOperand{Kind: "r", Index: 3, Swizzle: "xyzw"}, "R3", "R3.xyzw"},
		{VSOperand{Kind: "r", Index: 12, Swizzle: "xxxx", Negate: true}, "-R12.xxxx", "(-oPos.xxxx)"},
		{VSOperand{Kind: "v", Index: 2, Swizzle: "zzzz"}, "v2.zzzz", "v2.zzzz"},
		{VSOperand{Kind: "c", Index: 7, Swizzle: "xyzw"}, "c7", "c[7].xyzw"},
		{VSOperand{Kind: "c", Index: -2, Swizzle: "wwww", Negate: true}, "-c-2.wwww", "(-cneg2.wwww)"},
		{VSOperand{Kind: "?", Swizzle: "xyzw"}, "?", ".xyzw"},
	}
	for _, c := range cases {
		o := c.op
		if got := operandString(&o); got != c.text {
			t.Errorf("%+v text %q, want %q", c.op, got, c.text)
		}
		if got := operandGLSL(&o); got != c.glsl {
			t.Errorf("%+v glsl %q, want %q", c.op, got, c.glsl)
		}
	}
	if vsMask(0xF) != "xyzw" || vsMask(0x9) != "xw" || vsMask(0) != "" {
		t.Fatalf("masks %q %q %q", vsMask(0xF), vsMask(0x9), vsMask(0))
	}
	if registerGLSL(12) != "oPos" || registerGLSL(0) != "R0" {
		t.Fatal("register names")
	}
	var b strings.Builder
	writeMasked(&b, "R12.xy", "tmp")
	writeMasked(&b, "oD1.w", "ilu")
	if b.String() != "oPos.xy = tmp.xy;\noD1.w = ilu.w;\n" {
		t.Fatalf("masked writes %q", b.String())
	}
}

func TestSortHelpers(t *testing.T) {
	ints := []int{5, -1, 3, 3, 0}
	sortInts(ints)
	if !equalInts(ints, []int{-1, 0, 3, 3, 5}) {
		t.Fatalf("ints %v", ints)
	}
	strs := []string{"oPos", "A0", "oD0"}
	sortStrings(strs)
	if !equalStrings(strs, []string{"A0", "oD0", "oPos"}) {
		t.Fatalf("strings %v", strs)
	}
}

func vsdStream(stream uint32) uint32 { return 1<<29 | stream }

func vsdData(code, register uint32) uint32 { return 2<<29 | code<<16 | register }

func declImage(t *testing.T, tokens ...uint32) *Image {
	t.Helper()
	spec := sampleSpec()
	spec.sections = append(spec.sections, testSection{name: ".vsd", va: 0x14000, data: words32(tokens...)})
	return parseXBE(t, spec)
}

func TestDecodeVertexDeclarationLayout(t *testing.T) {
	img := declImage(t, 0, vsdStream(0), vsdData(0x32, 0), vsdData(0x40, 3), vsdData(0x22, 9), 0xFFFFFFFF)
	decl, err := img.DecodeVertexDeclaration(0x14000)
	if err != nil {
		t.Fatal(err)
	}
	if decl.VA != 0x14000 || decl.Stride != 24 || len(decl.Elements) != 3 {
		t.Fatalf("declaration %+v", decl)
	}
	want := []VertexElement{
		{Register: 0, Type: "FLOAT3", TypeCode: 0x32, Offset: 0, Bytes: 12, Comps: 3},
		{Register: 3, Type: "D3DCOLOR", TypeCode: 0x40, Offset: 12, Bytes: 4, Comps: 4},
		{Register: 9, Type: "FLOAT2", TypeCode: 0x22, Offset: 16, Bytes: 8, Comps: 2},
	}
	for i, w := range want {
		if decl.Elements[i] != w {
			t.Errorf("element %d %+v, want %+v", i, decl.Elements[i], w)
		}
	}
}

func TestDecodeVertexDeclarationEveryType(t *testing.T) {
	cases := []struct {
		code  uint32
		name  string
		bytes int
		comps int
	}{
		{0x12, "FLOAT1", 4, 1}, {0x22, "FLOAT2", 8, 2}, {0x32, "FLOAT3", 12, 3}, {0x42, "FLOAT4", 16, 4},
		{0x40, "D3DCOLOR", 4, 4}, {0x25, "SHORT2", 4, 2}, {0x45, "SHORT4", 8, 4},
		{0x11, "NORMSHORT1", 2, 1}, {0x21, "NORMSHORT2", 4, 2}, {0x31, "NORMSHORT3", 6, 3}, {0x41, "NORMSHORT4", 8, 4},
		{0x16, "NORMPACKED3", 4, 3}, {0x15, "SHORT1", 2, 1}, {0x35, "SHORT3", 6, 3},
		{0x14, "PBYTE1", 1, 1}, {0x24, "PBYTE2", 2, 2}, {0x34, "PBYTE3", 3, 3}, {0x44, "PBYTE4", 4, 4},
		{0x72, "FLOAT2H", 12, 3}, {0x02, "NONE", 0, 0},
	}
	for _, c := range cases {
		img := declImage(t, vsdStream(0), vsdData(c.code, 5), vsdData(0x12, 6), 0xFFFFFFFF)
		decl, err := img.DecodeVertexDeclaration(0x14000)
		if err != nil {
			t.Errorf("type %02X: %v", c.code, err)
			continue
		}
		want := VertexElement{Register: 5, Type: c.name, TypeCode: c.code, Offset: 0, Bytes: c.bytes, Comps: c.comps}
		if len(decl.Elements) != 2 || decl.Elements[0] != want || decl.Elements[1].Offset != c.bytes || decl.Stride != c.bytes+4 {
			t.Errorf("type %02X decodes to %+v stride %d", c.code, decl.Elements, decl.Stride)
		}
	}
}

func TestDecodeVertexDeclarationErrors(t *testing.T) {
	cases := map[string][]uint32{
		"uses stream 1":    {vsdStream(1), vsdData(0x12, 0), 0xFFFFFFFF},
		"skips data":       {vsdStream(0), vsdData(0x12, 0) | 1<<28, 0xFFFFFFFF},
		"has data type 99": {vsdStream(0), vsdData(0x99, 0), 0xFFFFFFFF},
		"has token type 3": {vsdStream(0), 3 << 29, 0xFFFFFFFF},
		"has no end token": make([]uint32, 70),
		"truncated":        {vsdStream(0), vsdData(0x12, 0)},
	}
	for want, tokens := range cases {
		_, err := declImage(t, tokens...).DecodeVertexDeclaration(0x14000)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: err %v", want, err)
		}
	}
}

func le32ToU32(b []byte) uint32 {
	return uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24
}

func equalInts(a, b []int) bool {
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
