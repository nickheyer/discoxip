package xbe

import (
	"fmt"

	"golang.org/x/arch/x86/x86asm"
)

func xmmIndex(r x86asm.Reg) (int, bool) {
	if r >= x86asm.X0 && r <= x86asm.X7 {
		return int(r - x86asm.X0), true
	}
	return 0, false
}

func mmxIndex(r x86asm.Reg) (int, bool) {
	if r >= x86asm.M0 && r <= x86asm.M7 {
		return int(r - x86asm.M0), true
	}
	return 0, false
}

func (s *execState) xmmLane(i, lane int) Expr {
	if v := s.xmm[i][lane]; v != nil {
		return v
	}
	return Sym{fmt.Sprintf("xmm%d.%d", i, lane)}
}

func (s *execState) mmLane(i, lane int) Expr {
	if v := s.mm[i][lane]; v != nil {
		return v
	}
	return Sym{fmt.Sprintf("mm%d.%d", i, lane)}
}

// Reads one float lane of a vector register or packed memory operand
func (ex *Executor) floatLane(s *execState, in *Instruction, a x86asm.Arg, lane int) Expr {
	switch v := a.(type) {
	case x86asm.Reg:
		if i, ok := xmmIndex(v); ok {
			return s.xmmLane(i, lane)
		}
	case x86asm.Mem:
		return ex.loadFloat(s, mkBin("add", ex.addr(s, v), IConst{uint32(4 * lane)}), 4)
	}
	panic(fmt.Sprintf("float lane of %T", a))
}

// Reads one lane of a vector register or packed memory operand as integer bits
func (ex *Executor) bitsLane(s *execState, in *Instruction, a x86asm.Arg, lane int) Expr {
	switch v := a.(type) {
	case x86asm.Reg:
		if i, ok := xmmIndex(v); ok {
			return mkUn("f2bits", s.xmmLane(i, lane))
		}
		if i, ok := mmxIndex(v); ok {
			return s.mmLane(i, lane)
		}
		return s.getReg(v)
	case x86asm.Mem:
		return ex.load(s, mkBin("add", ex.addr(s, v), IConst{uint32(4 * lane)}), 4)
	}
	panic(fmt.Sprintf("bits lane of %T", a))
}

func (ex *Executor) storeLane(s *execState, in *Instruction, m x86asm.Mem, lane int, v Expr) {
	ex.storeFloat(s, mkBin("add", ex.addr(s, m), IConst{uint32(4 * lane)}), 4, v, in.VA)
}

// Combines a float lane with an integer mask the way the packed logic instructions do
func laneLogic(op string, f Expr, mask Expr) Expr {
	if c, ok := isConst(mask); ok {
		switch {
		case op == "xor" && c == 0:
			return f
		case op == "xor" && c == 0x80000000:
			return mkUn("fneg", f)
		case op == "and" && c == 0x7FFFFFFF:
			return mkUn("fabs", f)
		case op == "and" && c == 0:
			return FConst{0}
		case op == "and" && c == 0xFFFFFFFF:
			return f
		case op == "or" && c == 0:
			return f
		case op == "andn" && c == 0:
			return FConst{0}
		}
	}
	bits := mkUn("f2bits", f)
	if op == "andn" {
		return mkUn("bits2f", mkBin("and", mkUn("not", bits), mask))
	}
	return mkUn("bits2f", mkBin(op, bits, mask))
}

// Executes one SSE or MMX instruction, reporting whether the opcode is one it models
func (ex *Executor) sseStep(s *execState, in *Instruction) (bool, error) {
	args := in.Inst.Args
	op := in.Inst.Op
	dstReg, _ := args[0].(x86asm.Reg)
	dstMem, dstIsMem := args[0].(x86asm.Mem)
	dx, dstXMM := xmmIndex(dstReg)
	dm, dstMMX := mmxIndex(dstReg)
	srcXMM := -1
	srcMMX := -1
	if len(args) > 1 {
		if r, ok := args[1].(x86asm.Reg); ok {
			if i, ok := xmmIndex(r); ok {
				srcXMM = i
			}
			if i, ok := mmxIndex(r); ok {
				srcMMX = i
			}
		}
	}
	packed := func(name string, lanes int) {
		for i := 0; i < lanes; i++ {
			s.xmm[dx][i] = mkFBin(name, s.xmmLane(dx, i), ex.floatLane(s, in, args[1], i))
		}
	}
	unary := func(f func(Expr) Expr, lanes int) {
		for i := 0; i < lanes; i++ {
			s.xmm[dx][i] = f(ex.floatLane(s, in, args[1], i))
		}
	}
	logic := func(name string) {
		for i := 0; i < 4; i++ {
			if name == "xor" && srcXMM == dx {
				s.xmm[dx][i] = FConst{0}
				continue
			}
			s.xmm[dx][i] = laneLogic(name, s.xmmLane(dx, i), ex.bitsLane(s, in, args[1], i))
		}
	}
	switch op {
	case x86asm.LDMXCSR, x86asm.STMXCSR, x86asm.PREFETCHT0, x86asm.PREFETCHNTA, x86asm.SFENCE, x86asm.EMMS:
		return true, nil
	case x86asm.MOVAPS, x86asm.MOVUPS, x86asm.MOVNTPS, x86asm.MOVDQA, x86asm.MOVDQU:
		if dstIsMem {
			for i := 0; i < 4; i++ {
				ex.storeLane(s, in, dstMem, i, s.xmmLane(srcXMM, i))
			}
			return true, nil
		}
		for i := 0; i < 4; i++ {
			s.xmm[dx][i] = ex.floatLane(s, in, args[1], i)
		}
		return true, nil
	case x86asm.MOVSS:
		switch {
		case dstIsMem:
			ex.storeLane(s, in, dstMem, 0, s.xmmLane(srcXMM, 0))
		case srcXMM >= 0:
			s.xmm[dx][0] = s.xmmLane(srcXMM, 0)
		default:
			s.xmm[dx] = [4]Expr{ex.floatLane(s, in, args[1], 0), FConst{0}, FConst{0}, FConst{0}}
		}
		return true, nil
	case x86asm.MOVSD_XMM, x86asm.MOVQ, x86asm.MOVLPS:
		switch {
		case dstIsMem && srcXMM >= 0:
			ex.storeLane(s, in, dstMem, 0, s.xmmLane(srcXMM, 0))
			ex.storeLane(s, in, dstMem, 1, s.xmmLane(srcXMM, 1))
		case dstIsMem && srcMMX >= 0:
			ex.store(s, ex.addr(s, dstMem), 4, s.mmLane(srcMMX, 0), in.VA)
			ex.store(s, mkBin("add", ex.addr(s, dstMem), IConst{4}), 4, s.mmLane(srcMMX, 1), in.VA)
		case dstXMM && srcXMM >= 0:
			s.xmm[dx][0], s.xmm[dx][1] = s.xmmLane(srcXMM, 0), s.xmmLane(srcXMM, 1)
		case dstXMM:
			s.xmm[dx][0], s.xmm[dx][1] = ex.floatLane(s, in, args[1], 0), ex.floatLane(s, in, args[1], 1)
			if op != x86asm.MOVLPS {
				s.xmm[dx][2], s.xmm[dx][3] = FConst{0}, FConst{0}
			}
		case dstMMX && srcMMX >= 0:
			s.mm[dm] = s.mm[srcMMX]
		case dstMMX:
			s.mm[dm][0], s.mm[dm][1] = ex.bitsLane(s, in, args[1], 0), ex.bitsLane(s, in, args[1], 1)
		default:
			return true, fmt.Errorf("unsupported vector move")
		}
		return true, nil
	case x86asm.MOVHPS:
		if dstIsMem {
			ex.storeLane(s, in, dstMem, 0, s.xmmLane(srcXMM, 2))
			ex.storeLane(s, in, dstMem, 1, s.xmmLane(srcXMM, 3))
			return true, nil
		}
		s.xmm[dx][2], s.xmm[dx][3] = ex.floatLane(s, in, args[1], 0), ex.floatLane(s, in, args[1], 1)
		return true, nil
	case x86asm.MOVLHPS:
		s.xmm[dx][2], s.xmm[dx][3] = s.xmmLane(srcXMM, 0), s.xmmLane(srcXMM, 1)
		return true, nil
	case x86asm.MOVHLPS:
		s.xmm[dx][0], s.xmm[dx][1] = s.xmmLane(srcXMM, 2), s.xmmLane(srcXMM, 3)
		return true, nil
	case x86asm.MOVD:
		switch {
		case dstXMM:
			s.xmm[dx] = [4]Expr{mkUn("bits2f", ex.bitsLane(s, in, args[1], 0)), FConst{0}, FConst{0}, FConst{0}}
		case dstMMX:
			s.mm[dm] = [2]Expr{ex.bitsLane(s, in, args[1], 0), IConst{0}}
		case srcXMM >= 0:
			ex.setOperand(s, in, args[0], 4, mkUn("f2bits", s.xmmLane(srcXMM, 0)))
		case srcMMX >= 0:
			ex.setOperand(s, in, args[0], 4, s.mmLane(srcMMX, 0))
		default:
			return true, fmt.Errorf("unsupported movd form")
		}
		return true, nil
	case x86asm.ADDPS:
		packed("fadd", 4)
	case x86asm.SUBPS:
		packed("fsub", 4)
	case x86asm.MULPS:
		packed("fmul", 4)
	case x86asm.DIVPS:
		packed("fdiv", 4)
	case x86asm.MINPS:
		packed("fmin", 4)
	case x86asm.MAXPS:
		packed("fmax", 4)
	case x86asm.ADDSS:
		packed("fadd", 1)
	case x86asm.SUBSS:
		packed("fsub", 1)
	case x86asm.MULSS:
		packed("fmul", 1)
	case x86asm.DIVSS:
		packed("fdiv", 1)
	case x86asm.MINSS:
		packed("fmin", 1)
	case x86asm.MAXSS:
		packed("fmax", 1)
	case x86asm.SQRTPS:
		unary(func(e Expr) Expr { return mkUn("fsqrt", e) }, 4)
	case x86asm.SQRTSS:
		unary(func(e Expr) Expr { return mkUn("fsqrt", e) }, 1)
	case x86asm.RSQRTPS:
		unary(func(e Expr) Expr { return mkFBin("fdiv", FConst{1}, mkUn("fsqrt", e)) }, 4)
	case x86asm.RSQRTSS:
		unary(func(e Expr) Expr { return mkFBin("fdiv", FConst{1}, mkUn("fsqrt", e)) }, 1)
	case x86asm.RCPPS:
		unary(func(e Expr) Expr { return mkFBin("fdiv", FConst{1}, e) }, 4)
	case x86asm.RCPSS:
		unary(func(e Expr) Expr { return mkFBin("fdiv", FConst{1}, e) }, 1)
	case x86asm.XORPS:
		logic("xor")
	case x86asm.ANDPS:
		logic("and")
	case x86asm.ORPS:
		logic("or")
	case x86asm.ANDNPS:
		logic("andn")
	case x86asm.SHUFPS:
		sel := uint8(args[2].(x86asm.Imm))
		d := s.xmm[dx]
		var src [4]Expr
		for i := 0; i < 4; i++ {
			d[i] = s.xmmLane(dx, i)
			src[i] = ex.floatLane(s, in, args[1], i)
		}
		s.xmm[dx] = [4]Expr{d[sel&3], d[(sel>>2)&3], src[(sel>>4)&3], src[(sel>>6)&3]}
	case x86asm.UNPCKLPS:
		s0, s1 := ex.floatLane(s, in, args[1], 0), ex.floatLane(s, in, args[1], 1)
		s.xmm[dx] = [4]Expr{s.xmmLane(dx, 0), s0, s.xmmLane(dx, 1), s1}
	case x86asm.UNPCKHPS:
		s2, s3 := ex.floatLane(s, in, args[1], 2), ex.floatLane(s, in, args[1], 3)
		s.xmm[dx] = [4]Expr{s.xmmLane(dx, 2), s2, s.xmmLane(dx, 3), s3}
	case x86asm.CVTSI2SS:
		s.xmm[dx][0] = mkUn("i2f", ex.operand(s, in, args[1], 4))
	case x86asm.CVTTSS2SI:
		ex.setOperand(s, in, args[0], 4, mkUn("f2i", ex.floatLane(s, in, args[1], 0)))
	case x86asm.CVTSS2SI:
		ex.setOperand(s, in, args[0], 4, mkUn("f2i_rn", ex.floatLane(s, in, args[1], 0)))
	case x86asm.CVTPS2PI, x86asm.CVTTPS2PI:
		conv := "f2i_rn"
		if op == x86asm.CVTTPS2PI {
			conv = "f2i"
		}
		s.mm[dm] = [2]Expr{mkUn(conv, ex.floatLane(s, in, args[1], 0)), mkUn(conv, ex.floatLane(s, in, args[1], 1))}
	case x86asm.CVTPI2PS:
		s.xmm[dx][0], s.xmm[dx][1] = mkUn("i2f", ex.bitsLane(s, in, args[1], 0)), mkUn("i2f", ex.bitsLane(s, in, args[1], 1))
	case x86asm.COMISS, x86asm.UCOMISS:
		s.fl = flagState{kind: "comiss", a: s.xmmLane(dx, 0), b: ex.floatLane(s, in, args[1], 0)}
	case x86asm.MOVMSKPS:
		var mask Expr = IConst{0}
		for i := 0; i < 4; i++ {
			bit := mkBin("shr", mkUn("f2bits", s.xmmLane(srcXMM, i)), IConst{31})
			mask = mkBin("or", mask, mkBin("shl", bit, IConst{uint32(i)}))
		}
		ex.setOperand(s, in, args[0], 4, mask)
	case x86asm.PSHUFW:
		// Each result word takes the source word the immediate's two bits for it select
		sel, _ := args[2].(x86asm.Imm)
		src := [2]Expr{ex.bitsLane(s, in, args[1], 0), ex.bitsLane(s, in, args[1], 1)}
		word := func(k int) Expr {
			if k%2 == 0 {
				return mkUn("zx16", src[k/2])
			}
			return mkBin("shr", src[k/2], IConst{16})
		}
		pick := func(i int) Expr { return word(int(sel>>(2*i)) & 3) }
		s.mm[dm] = [2]Expr{mkBin("or", pick(0), mkBin("shl", pick(1), IConst{16})), mkBin("or", pick(2), mkBin("shl", pick(3), IConst{16}))}
	case x86asm.PUNPCKLDQ, x86asm.PUNPCKHDQ:
		// The low or high doublewords of the two operands interleave
		half := 0
		if op == x86asm.PUNPCKHDQ {
			half = 1
		}
		switch {
		case dstMMX:
			s.mm[dm] = [2]Expr{s.mmLane(dm, half), ex.bitsLane(s, in, args[1], half)}
		case dstXMM:
			s.xmm[dx] = [4]Expr{s.xmmLane(dx, 2*half), ex.floatLane(s, in, args[1], 2*half), s.xmmLane(dx, 2*half+1), ex.floatLane(s, in, args[1], 2*half+1)}
		default:
			return false, nil
		}
	case x86asm.PUNPCKLWD:
		d0 := s.mmLane(dm, 0)
		s0 := ex.bitsLane(s, in, args[1], 0)
		lo := mkBin("or", mkUn("zx16", d0), mkBin("shl", mkUn("zx16", s0), IConst{16}))
		hi := mkBin("or", mkBin("shr", d0, IConst{16}), mkBin("and", s0, IConst{0xFFFF0000}))
		s.mm[dm] = [2]Expr{lo, hi}
	case x86asm.PXOR:
		if dstXMM && srcXMM == dx {
			s.xmm[dx] = [4]Expr{FConst{0}, FConst{0}, FConst{0}, FConst{0}}
		} else if dstMMX && srcMMX == dm {
			s.mm[dm] = [2]Expr{IConst{0}, IConst{0}}
		} else {
			return true, fmt.Errorf("unsupported pxor form")
		}
	default:
		return false, nil
	}
	return true, nil
}

// Resolves a jump after a packed scalar compare, which sets the flags from the float ordering
func (ex *Executor) comissCondition(s *execState, op x86asm.Op) Expr {
	a, b := s.fl.a, s.fl.b
	const lt, eq, gt, unord = 1, 2, 4, 8
	switch op {
	case x86asm.JB, x86asm.SETB:
		return mkFCmp(a, b, lt|unord)
	case x86asm.JBE, x86asm.SETBE:
		return mkFCmp(a, b, lt|eq|unord)
	case x86asm.JA, x86asm.SETA:
		return mkFCmp(a, b, gt)
	case x86asm.JAE, x86asm.SETAE:
		return mkFCmp(a, b, gt|eq)
	case x86asm.JE, x86asm.SETE:
		return mkFCmp(a, b, eq|unord)
	case x86asm.JNE, x86asm.SETNE:
		return mkFCmp(a, b, lt|gt)
	case x86asm.JP, x86asm.SETP:
		return mkFCmp(a, b, unord)
	case x86asm.JNP, x86asm.SETNP:
		return mkFCmp(a, b, lt|eq|gt)
	case x86asm.JS, x86asm.SETS:
		return IConst{0}
	case x86asm.JNS, x86asm.SETNS:
		return IConst{1}
	}
	panic(fmt.Sprintf("comiss condition %v", op))
}
