package xbe

import (
	"golang.org/x/arch/x86/x86asm"
)

// mathIntrinsic is a C runtime float function and the expression operator standing for it.
type mathIntrinsic struct {
	op    string
	arity int
}

// C runtime float functions that take their arguments on the x87 stack, by the name their exception descriptor carries
var mathIntrinsics = map[string]mathIntrinsic{
	"sin": {"fsin", 1}, "cos": {"fcos", 1}, "tan": {"ftan", 1}, "sqrt": {"fsqrt", 1}, "floor": {"ffloor", 1}, "ceil": {"fceil", 1},
	"exp": {"fexp", 1}, "log": {"flog", 1}, "log10": {"flog10", 1}, "atan": {"fatan", 1}, "asin": {"fasin", 1}, "acos": {"facos", 1},
	"sinh": {"fsinh", 1}, "cosh": {"fcosh", 1}, "tanh": {"ftanh", 1}, "fabs": {"fabs", 1},
	"fmod": {"fmod", 2}, "pow": {"fpow", 2}, "atan2": {"fatan2", 2},
}

// Recognizes a runtime math thunk by the function name its descriptor names after the descriptor's leading byte
func (d *Disassembly) mathIntrinsic(target uint32) (mathIntrinsic, bool) {
	fn := d.Functions[target]
	if fn == nil || len(fn.Instructions) != 2 || fn.Instructions[0].Inst.Op != x86asm.MOV || fn.Instructions[1].Inst.Op != x86asm.JMP {
		return mathIntrinsic{}, false
	}
	r, ok := fn.Instructions[0].Inst.Args[0].(x86asm.Reg)
	imm, isImm := fn.Instructions[0].Inst.Args[1].(x86asm.Imm)
	if !ok || !isImm || r != x86asm.EDX {
		return mathIntrinsic{}, false
	}
	raw, ok := d.Image.ReadBytes(uint32(imm), 16)
	if !ok {
		return mathIntrinsic{}, false
	}
	m, ok := mathIntrinsics[readCString(raw[1:], 0)]
	return m, ok
}

// Recognizes the stack cookie check the optimizer calls on return from every guarded function
func (d *Disassembly) nameSecurityCheck() {
	for _, va := range d.functionVAs() {
		if d.Functions[va].Symbol != nil {
			continue
		}
		in := d.InsnByVA[va]
		if in == nil || in.Inst.Op != x86asm.CMP {
			continue
		}
		if r, ok := in.Inst.Args[0].(x86asm.Reg); !ok || r != x86asm.ECX {
			continue
		}
		if m, ok := in.Inst.Args[1].(x86asm.Mem); !ok || m.Base != 0 || m.Index != 0 || d.sectionOf(uint32(m.Disp)) == nil {
			continue
		}
		jump := d.InsnByVA[va+uint32(in.Inst.Len)]
		if jump == nil || !isConditionalJump(jump.Inst.Op) {
			continue
		}
		ret := d.InsnByVA[jump.VA+uint32(jump.Inst.Len)]
		if ret == nil || ret.Inst.Op != x86asm.RET {
			continue
		}
		d.nameByUsage(va, "__security_check_cookie")
	}
}

// Recognizes the stack probe the compiler calls to reserve a frame larger than a page, its size in eax
func isStackProbe(fn *Function) bool {
	if len(fn.Instructions) > 40 || fn.Instructions[0].VA != fn.EntryVA {
		return false
	}
	entry := &fn.Instructions[0]
	if entry.Inst.Op != x86asm.TEST || entry.Inst.Args[0] != x86asm.Arg(x86asm.EAX) || entry.Inst.Args[1] != x86asm.Arg(x86asm.EAX) {
		return false
	}
	swap, page := false, false
	for i := range fn.Instructions {
		in := &fn.Instructions[i]
		if in.Inst.Op == x86asm.XCHG && in.Inst.Args[0] == x86asm.Arg(x86asm.ESP) && in.Inst.Args[1] == x86asm.Arg(x86asm.EAX) {
			swap = true
		}
		if opRegImm(in, x86asm.CMP, x86asm.EAX, 0x1000) {
			page = true
		}
	}
	return swap && page
}

// Names the stack probe so the frames it reserves are not read as calls the dashboard makes
func (d *Disassembly) nameStackProbe() {
	for _, va := range d.functionVAs() {
		if fn := d.Functions[va]; fn.Symbol == nil && isStackProbe(fn) {
			d.nameByUsage(va, "_chkstk")
		}
	}
}
