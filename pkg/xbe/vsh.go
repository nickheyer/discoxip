package xbe

import (
	"encoding/binary"
	"fmt"
	"strings"
)

// VertexProgram is a decoded NV2A vertex shader program.
type VertexProgram struct {
	VA           uint32          `json:"va"`
	Instructions []VSInstruction `json:"instructions"`
	Inputs       []int           `json:"inputs"`    // vertex attribute registers read
	Constants    []int           `json:"constants"` // constant registers read, in D3D numbering
	Outputs      []string        `json:"outputs"`
	GLSL         string          `json:"glsl"`
	Asm          string          `json:"asm"`
}

// VSOperand is one source operand of a vertex shader instruction.
type VSOperand struct {
	Kind    string `json:"kind"` // r v c
	Index   int    `json:"index"`
	Swizzle string `json:"swizzle"`
	Negate  bool   `json:"negate"`
}

// VSInstruction is one decoded 128 bit vertex shader instruction.
type VSInstruction struct {
	Slot    int        `json:"slot"`
	MAC     string     `json:"mac,omitempty"`
	ILU     string     `json:"ilu,omitempty"`
	A, B, C *VSOperand `json:"-"`
	MACDst  []string   `json:"mac_dst,omitempty"`
	ILUDst  []string   `json:"ilu_dst,omitempty"`
	Final   bool       `json:"final,omitempty"`
	Raw     [4]uint32  `json:"raw"`
}

var vsMacNames = []string{"NOP", "MOV", "MUL", "ADD", "MAD", "DP3", "DPH", "DP4", "DST", "MIN", "MAX", "SLT", "SGE", "ARL"}
var vsIluNames = []string{"NOP", "MOV", "RCP", "RCC", "RSQ", "EXP", "LOG", "LIT"}
var vsOutNames = []string{"oPos", "o1", "o2", "oD0", "oD1", "oFog", "oPts", "oB0", "oB1", "oT0", "oT1", "oT2", "oT3", "o13", "o14", "A0"}
var vsMacArgs = map[string]string{"MOV": "A", "MUL": "AB", "ADD": "AC", "MAD": "ABC", "DP3": "AB", "DPH": "AB", "DP4": "AB", "DST": "AB", "MIN": "AB", "MAX": "AB", "SLT": "AB", "SGE": "AB", "ARL": "A"}

// Field positions in the instruction subtokens, from the xemu NV2A shader decoder
type vsField struct{ word, bit, len uint }

var vsFields = map[string]vsField{
	"ILU": {1, 25, 3}, "MAC": {1, 21, 4}, "CONST": {1, 13, 8}, "V": {1, 9, 4},
	"A_NEG": {1, 8, 1}, "A_SWZ_X": {1, 6, 2}, "A_SWZ_Y": {1, 4, 2}, "A_SWZ_Z": {1, 2, 2}, "A_SWZ_W": {1, 0, 2}, "A_R": {2, 28, 4}, "A_MUX": {2, 26, 2},
	"B_NEG": {2, 25, 1}, "B_SWZ_X": {2, 23, 2}, "B_SWZ_Y": {2, 21, 2}, "B_SWZ_Z": {2, 19, 2}, "B_SWZ_W": {2, 17, 2}, "B_R": {2, 13, 4}, "B_MUX": {2, 11, 2},
	"C_NEG": {2, 10, 1}, "C_SWZ_X": {2, 8, 2}, "C_SWZ_Y": {2, 6, 2}, "C_SWZ_Z": {2, 4, 2}, "C_SWZ_W": {2, 2, 2}, "C_R_HIGH": {2, 0, 2}, "C_R_LOW": {3, 30, 2}, "C_MUX": {3, 28, 2},
	"OUT_MAC_MASK": {3, 24, 4}, "OUT_R": {3, 20, 4}, "OUT_ILU_MASK": {3, 16, 4}, "OUT_O_MASK": {3, 12, 4}, "OUT_ORB": {3, 11, 1}, "OUT_ADDRESS": {3, 3, 8}, "OUT_MUX": {3, 2, 1},
	"A0X": {3, 1, 1}, "FINAL": {3, 0, 1},
}

// The XVS header word carrying the instruction count in its upper half
const vshVersionXVS = 0x2078

func vsGet(t [4]uint32, name string) uint32 {
	f := vsFields[name]
	return (t[f.word] >> f.bit) & ((1 << f.len) - 1)
}

func vsMask(m uint32) string {
	out := ""
	for i, c := range "xyzw" {
		if m&(8>>i) != 0 {
			out += string(c)
		}
	}
	return out
}

func vsSwizzle(t [4]uint32, prefix string) string {
	s := ""
	for _, c := range []string{"X", "Y", "Z", "W"} {
		s += string("xyzw"[vsGet(t, prefix+"_SWZ_"+c)])
	}
	return s
}

func vsOperand(t [4]uint32, prefix string, rnum uint32) *VSOperand {
	mux := vsGet(t, prefix+"_MUX")
	op := &VSOperand{Swizzle: vsSwizzle(t, prefix), Negate: vsGet(t, prefix+"_NEG") != 0}
	switch mux {
	case 1:
		op.Kind, op.Index = "r", int(rnum)
	case 2:
		op.Kind, op.Index = "v", int(vsGet(t, "V"))
	case 3:
		op.Kind, op.Index = "c", int(vsGet(t, "CONST"))-96
	default:
		op.Kind = "?"
	}
	return op
}

// DecodeVertexProgram decodes an XVS program blob stored in the image.
func (img *Image) DecodeVertexProgram(va uint32) (*VertexProgram, error) {
	hdr, ok := img.ReadU32(va)
	if !ok || hdr&0xFFFF != vshVersionXVS {
		return nil, fmt.Errorf("vsh: no XVS header at %08X", va)
	}
	count := int(hdr >> 16)
	raw, ok := img.ReadBytes(va+8, count*16)
	if !ok {
		return nil, fmt.Errorf("vsh: program at %08X truncated", va)
	}
	prog := &VertexProgram{VA: va}
	inputs := map[int]bool{}
	consts := map[int]bool{}
	outputs := map[string]bool{}
	var asm []string
	for i := 0; i < count; i++ {
		var stored [4]uint32
		for w := 0; w < 4; w++ {
			stored[w] = binary.LittleEndian.Uint32(raw[i*16+w*4:])
		}
		// The image stores subtokens 1..3 first and the unused subtoken 0 last.
		t := [4]uint32{stored[3], stored[0], stored[1], stored[2]}
		ins := VSInstruction{Slot: i, Raw: stored, Final: vsGet(t, "FINAL") != 0}
		mac := vsMacNames[vsGet(t, "MAC")]
		ilu := vsIluNames[vsGet(t, "ILU")]
		cR := vsGet(t, "C_R_HIGH")<<2 | vsGet(t, "C_R_LOW")
		ins.A = vsOperand(t, "A", vsGet(t, "A_R"))
		ins.B = vsOperand(t, "B", vsGet(t, "B_R"))
		ins.C = vsOperand(t, "C", cR)
		outReg := int(vsGet(t, "OUT_R"))
		oMask := vsGet(t, "OUT_O_MASK")
		oAddr := vsGet(t, "OUT_ADDRESS") & 0xF
		useOut := func(ops []*VSOperand) {
			for _, o := range ops {
				switch o.Kind {
				case "v":
					inputs[o.Index] = true
				case "c":
					consts[o.Index] = true
				}
			}
		}
		if mac != "NOP" {
			ins.MAC = mac
			var args []*VSOperand
			for _, k := range vsMacArgs[mac] {
				switch k {
				case 'A':
					args = append(args, ins.A)
				case 'B':
					args = append(args, ins.B)
				case 'C':
					args = append(args, ins.C)
				}
			}
			useOut(args)
			if m := vsGet(t, "OUT_MAC_MASK"); m != 0 {
				ins.MACDst = append(ins.MACDst, fmt.Sprintf("R%d.%s", outReg, vsMask(m)))
			}
			if oMask != 0 && vsGet(t, "OUT_MUX") == 0 {
				if vsGet(t, "OUT_ORB") == 0 {
					return nil, fmt.Errorf("vsh: slot %d writes a constant register", i)
				}
				ins.MACDst = append(ins.MACDst, fmt.Sprintf("%s.%s", vsOutNames[oAddr], vsMask(oMask)))
				outputs[vsOutNames[oAddr]] = true
			}
			asm = append(asm, fmt.Sprintf("%s %s, %s", mac, strings.Join(ins.MACDst, "+"), joinOperands(args)))
		}
		if ilu != "NOP" {
			ins.ILU = ilu
			useOut([]*VSOperand{ins.C})
			dstReg := outReg
			if mac != "NOP" {
				dstReg = 1
			}
			if m := vsGet(t, "OUT_ILU_MASK"); m != 0 {
				ins.ILUDst = append(ins.ILUDst, fmt.Sprintf("R%d.%s", dstReg, vsMask(m)))
			}
			if oMask != 0 && vsGet(t, "OUT_MUX") == 1 {
				ins.ILUDst = append(ins.ILUDst, fmt.Sprintf("%s.%s", vsOutNames[oAddr], vsMask(oMask)))
				outputs[vsOutNames[oAddr]] = true
			}
			asm = append(asm, fmt.Sprintf("%s %s, %s", ilu, strings.Join(ins.ILUDst, "+"), operandString(ins.C)))
		}
		prog.Instructions = append(prog.Instructions, ins)
		if ins.Final {
			break
		}
	}
	if len(prog.Instructions) == 0 || !prog.Instructions[len(prog.Instructions)-1].Final {
		return nil, fmt.Errorf("vsh: program at %08X has no final instruction", va)
	}
	for k := range inputs {
		prog.Inputs = append(prog.Inputs, k)
	}
	for k := range consts {
		prog.Constants = append(prog.Constants, k)
	}
	sortInts(prog.Inputs)
	sortInts(prog.Constants)
	for k := range outputs {
		prog.Outputs = append(prog.Outputs, k)
	}
	sortStrings(prog.Outputs)
	prog.Asm = strings.Join(asm, "\n")
	prog.GLSL = prog.emitGLSL()
	return prog, nil
}

func joinOperands(ops []*VSOperand) string {
	parts := make([]string, len(ops))
	for i, o := range ops {
		parts[i] = operandString(o)
	}
	return strings.Join(parts, ", ")
}

func operandString(o *VSOperand) string {
	s := ""
	if o.Negate {
		s = "-"
	}
	switch o.Kind {
	case "r":
		s += fmt.Sprintf("R%d", o.Index)
	case "v":
		s += fmt.Sprintf("v%d", o.Index)
	case "c":
		s += fmt.Sprintf("c%d", o.Index)
	default:
		s += "?"
	}
	if o.Swizzle != "xyzw" {
		s += "." + o.Swizzle
	}
	return s
}

// GLSL expression for a source operand
func operandGLSL(o *VSOperand) string {
	var base string
	switch o.Kind {
	case "r":
		base = registerGLSL(o.Index)
	case "v":
		base = fmt.Sprintf("v%d", o.Index)
	case "c":
		if o.Index < 0 {
			base = fmt.Sprintf("cneg%d", -o.Index)
		} else {
			base = fmt.Sprintf("c[%d]", o.Index)
		}
	}
	s := base + "." + o.Swizzle
	if o.Negate {
		s = "(-" + s + ")"
	}
	return s
}

// Emits the program body as GLSL using the NV2A instruction semantics
func (p *VertexProgram) emitGLSL() string {
	var b strings.Builder
	b.WriteString("vec4 R0=vec4(0.0),R1=vec4(0.0),R2=vec4(0.0),R3=vec4(0.0),R4=vec4(0.0),R5=vec4(0.0),R6=vec4(0.0),R7=vec4(0.0),R8=vec4(0.0),R9=vec4(0.0),R10=vec4(0.0),R11=vec4(0.0);\n")
	b.WriteString("vec4 oPos=vec4(0.0),oD0=vec4(0.0),oD1=vec4(0.0),oFog=vec4(0.0),oPts=vec4(0.0),oB0=vec4(0.0),oB1=vec4(0.0),oT0=vec4(0.0),oT1=vec4(0.0),oT2=vec4(0.0),oT3=vec4(0.0);\n")
	b.WriteString("vec4 tmp; vec4 ilu;\n")
	for _, ins := range p.Instructions {
		if ins.MAC != "" {
			var expr string
			a, bb, c := operandGLSL(ins.A), operandGLSL(ins.B), operandGLSL(ins.C)
			switch ins.MAC {
			case "MOV":
				expr = a
			case "MUL":
				expr = fmt.Sprintf("(%s * %s)", a, bb)
			case "ADD":
				expr = fmt.Sprintf("(%s + %s)", a, c)
			case "MAD":
				expr = fmt.Sprintf("(%s * %s + %s)", a, bb, c)
			case "DP3":
				expr = fmt.Sprintf("vec4(dot(%s.xyz, %s.xyz))", a, bb)
			case "DPH":
				expr = fmt.Sprintf("vec4(dot(vec4(%s.xyz, 1.0), %s))", a, bb)
			case "DP4":
				expr = fmt.Sprintf("vec4(dot(%s, %s))", a, bb)
			case "DST":
				expr = fmt.Sprintf("vec4(1.0, %s.y * %s.y, %s.z, %s.w)", a, bb, a, bb)
			case "MIN":
				expr = fmt.Sprintf("min(%s, %s)", a, bb)
			case "MAX":
				expr = fmt.Sprintf("max(%s, %s)", a, bb)
			case "SLT":
				expr = fmt.Sprintf("vec4(lessThan(%s, %s))", a, bb)
			case "SGE":
				expr = fmt.Sprintf("vec4(greaterThanEqual(%s, %s))", a, bb)
			case "ARL":
				expr = fmt.Sprintf("vec4(floor(%s.x + 0.001))", a)
			}
			fmt.Fprintf(&b, "tmp = %s;\n", expr)
			for _, d := range ins.MACDst {
				writeMasked(&b, d, "tmp")
			}
		}
		if ins.ILU != "" {
			c := operandGLSL(ins.C)
			var expr string
			switch ins.ILU {
			case "MOV":
				expr = c
			case "RCP":
				expr = fmt.Sprintf("vec4(1.0 / %s.x)", c)
			case "RCC":
				expr = fmt.Sprintf("vec4(clamp(1.0 / %s.x, -1.84467441e19, 1.84467441e19))", c)
			case "RSQ":
				expr = fmt.Sprintf("vec4(inversesqrt(abs(%s.x)))", c)
			case "EXP":
				expr = fmt.Sprintf("vec4(exp2(floor(%s.x)), fract(%s.x), exp2(%s.x), 1.0)", c, c, c)
			case "LOG":
				expr = fmt.Sprintf("vec4(floor(log2(abs(%s.x))), abs(%s.x) / exp2(floor(log2(abs(%s.x)))), log2(abs(%s.x)), 1.0)", c, c, c, c)
			case "LIT":
				expr = fmt.Sprintf("vec4(1.0, max(%s.x, 0.0), (%s.x > 0.0) ? pow(max(%s.y, 0.0), clamp(%s.w, -127.9961, 127.9961)) : 0.0, 1.0)", c, c, c, c)
			}
			fmt.Fprintf(&b, "ilu = %s;\n", expr)
			for _, d := range ins.ILUDst {
				writeMasked(&b, d, "ilu")
			}
		}
	}
	return b.String()
}

func writeMasked(b *strings.Builder, dst, src string) {
	parts := strings.SplitN(dst, ".", 2)
	name := parts[0]
	if strings.HasPrefix(name, "R") {
		var idx int
		fmt.Sscanf(name, "R%d", &idx)
		name = registerGLSL(idx)
	}
	fmt.Fprintf(b, "%s.%s = %s.%s;\n", name, parts[1], src, parts[1])
}

// Names a temporary register, R12 being the readable mirror of the position output
func registerGLSL(idx int) string {
	if idx == 12 {
		return "oPos"
	}
	return fmt.Sprintf("R%d", idx)
}

// VertexDeclaration is a decoded D3DVSD token stream.
type VertexDeclaration struct {
	VA       uint32          `json:"va"`
	Stride   int             `json:"stride"`
	Elements []VertexElement `json:"elements"`
}

// VertexElement is one attribute of a vertex stream.
type VertexElement struct {
	Register int    `json:"register"`
	Type     string `json:"type"`
	TypeCode uint32 `json:"type_code"`
	Offset   int    `json:"offset"`
	Bytes    int    `json:"bytes"`
	Comps    int    `json:"comps"`
}

// DecodeVertexDeclaration decodes a single stream D3DVSD declaration.
func (img *Image) DecodeVertexDeclaration(va uint32) (*VertexDeclaration, error) {
	decl := &VertexDeclaration{VA: va}
	off := 0
	for i := 0; i < 64; i++ {
		tok, ok := img.ReadU32(va + uint32(4*i))
		if !ok {
			return nil, fmt.Errorf("vsd: declaration at %08X truncated", va)
		}
		if tok == 0xFFFFFFFF {
			decl.Stride = off
			return decl, nil
		}
		switch tok >> 29 {
		case 0:
			continue
		case 1:
			if tok&0xF != 0 {
				return nil, fmt.Errorf("vsd: declaration at %08X uses stream %d", va, tok&0xF)
			}
		case 2:
			if tok&(1<<28) != 0 {
				return nil, fmt.Errorf("vsd: declaration at %08X skips data", va)
			}
			code := (tok >> 16) & 0xFF
			dt, ok := vertexDataTypes[code]
			if !ok {
				return nil, fmt.Errorf("vsd: declaration at %08X has data type %02X", va, code)
			}
			decl.Elements = append(decl.Elements, VertexElement{Register: int(tok & 0x1F), Type: dt.Name, TypeCode: code, Offset: off, Bytes: dt.Bytes, Comps: dt.Comps})
			off += dt.Bytes
		default:
			return nil, fmt.Errorf("vsd: declaration at %08X has token type %d", va, tok>>29)
		}
	}
	return nil, fmt.Errorf("vsd: declaration at %08X has no end token", va)
}

func sortInts(a []int) {
	for i := 1; i < len(a); i++ {
		for j := i; j > 0 && a[j] < a[j-1]; j-- {
			a[j], a[j-1] = a[j-1], a[j]
		}
	}
}

func sortStrings(a []string) {
	for i := 1; i < len(a); i++ {
		for j := i; j > 0 && a[j] < a[j-1]; j-- {
			a[j], a[j-1] = a[j-1], a[j]
		}
	}
}
