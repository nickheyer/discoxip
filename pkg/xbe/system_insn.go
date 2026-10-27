package xbe

import (
	"fmt"

	"golang.org/x/arch/x86/x86asm"
)

// Segment registers in the order the executor keeps them
var segIndex = map[x86asm.Reg]int{x86asm.ES: 0, x86asm.CS: 1, x86asm.SS: 2, x86asm.DS: 3, x86asm.FS: 4, x86asm.GS: 5}

var segNames = [6]string{"ES", "CS", "SS", "DS", "FS", "GS"}

// The calls the processor's own instructions become, which the runtime's processor model answers
const (
	cpuCLI      = "cpu_cli"
	cpuSTI      = "cpu_sti"
	cpuPushFD   = "cpu_pushfd"
	cpuPopFD    = "cpu_popfd"
	cpuIRETD    = "cpu_iretd"
	cpuINS      = "cpu_ins"
	cpuOUTS     = "cpu_outs"
	cpuSCAS     = "cpu_scas"
	cpuCMPS     = "cpu_cmps"
	cpuWRMSR    = "cpu_wrmsr"
	cpuRDMSRLo  = "cpu_rdmsr_lo"
	cpuRDMSRHi  = "cpu_rdmsr_hi"
	cpuRDTSCLo  = "cpu_rdtsc_lo"
	cpuRDTSCHi  = "cpu_rdtsc_hi"
	cpuCPUID    = "cpu_cpuid_"
	cpuFXSave   = "cpu_fxsave"
	cpuFXRstor  = "cpu_fxrstor"
	cpuFNSave   = "cpu_fnsave"
	cpuFRstor   = "cpu_frstor"
	cpuFRstorST = "cpu_frstor_st"
	cpuLGDT     = "cpu_lgdt"
	cpuLIDT     = "cpu_lidt"
	cpuLTR      = "cpu_ltr"
	cpuWriteCR  = "cpu_write_cr"
	cpuReadCR   = "cpu_read_cr"
	cpuInvlpg   = "cpu_invlpg"
	cpuCLD      = "cpu_cld"
	cpuSTD      = "cpu_std"
	cpuCLTS     = "cpu_clts"
	cpuFNSTCW   = "cpu_fnstcw"
	cpuFLDCW    = "cpu_fldcw"
	cpuFNINIT   = "cpu_fninit"
	cpuFNCLEX   = "cpu_fnclex"
)

// Reports whether a string instruction carries the repne prefix rather than rep or repe
func repnePrefix(in *Instruction) bool {
	for _, p := range in.Inst.Prefix {
		if p&0xFF == 0xF2 {
			return true
		}
	}
	return false
}

// Records a processor call with no result
func (ex *Executor) cpuEffect(s *execState, in *Instruction, name string, args ...Expr) {
	ex.seq++
	s.effects = append(s.effects, Effect{Kind: "call", VA: in.VA, Fn: name, Args: args, Seq: ex.seq})
}

// Records a processor call and returns the value it hands back
func (ex *Executor) cpuResult(s *execState, in *Instruction, name string, args ...Expr) Expr {
	ex.cpuEffect(s, in, name, args...)
	return CallResult{Fn: name, Args: args, Seq: ex.seq}
}

// Reports whether the stack pointer still points into the routine's own frame
func onFrame(s *execState) bool {
	p, ok := s.regs[4].(Ptr)
	return ok && p.Base == "stack"
}

// Pushes a word, into the frame while the stack pointer points there and into memory once it moved elsewhere
func (ex *Executor) pushWord(s *execState, in *Instruction, v Expr) {
	if onFrame(s) {
		s.push(v)
		// A lifted block leaves what it pushes in the frame for the blocks after it
		if ex.block != nil {
			s.effects = append(s.effects, Effect{Kind: "store", VA: in.VA, Base: "stack", Off: stackOff(s), Size: 4, Value: v})
		}
		return
	}
	esp := mkBin("sub", s.regs[4], IConst{4})
	s.regs[4] = esp
	ex.store(s, esp, 4, v, in.VA)
}

// The bytes a string instruction moves its pointer by, backwards when the direction flag is set
func (s *execState) stringStep(size int) Expr {
	if s.machineDF {
		return Sym{fmt.Sprintf("dfstep%d", size)}
	}
	if s.df {
		return IConst{uint32(-int32(size))}
	}
	return IConst{uint32(size)}
}

// Moves a string pointer register past the elements an instruction handled
func (s *execState) advance(r x86asm.Reg, count Expr, size int) {
	s.setReg(r, mkBin("add", s.getReg(r), mkBin("mul", count, s.stringStep(size))))
}

// Executes the system, flag, string and rotate instructions the dashboard never uses and the kernel does, reporting
// whether the instruction was one of them
func (ex *Executor) systemStep(s *execState, in *Instruction, next uint32) (done bool, handled bool, err error) {
	args := in.Inst.Args
	op := in.Inst.Op
	handled = true
	switch op {
	case x86asm.CLI:
		ex.cpuEffect(s, in, cpuCLI)
	case x86asm.STI:
		ex.cpuEffect(s, in, cpuSTI)
		// Enabling interrupts ends a machine block, so one pending is taken before the next instruction
		if ex.machine {
			ex.block.term = &blockTerm{kind: "jump", target: next}
			s.va = 0
			return true, true, nil
		}
	case x86asm.CLD:
		s.df, s.machineDF = false, false
		if ex.machine {
			ex.cpuEffect(s, in, cpuCLD)
		}
	case x86asm.STD:
		s.df, s.machineDF = true, false
		if ex.machine {
			ex.cpuEffect(s, in, cpuSTD)
		}
	case x86asm.CLTS:
		ex.cpuEffect(s, in, cpuCLTS)
	case x86asm.STC:
		s.fl.cf = IConst{1}
	case x86asm.CLC:
		s.fl.cf = IConst{0}
	case x86asm.CMC:
		cf, ok := carryOf(s.fl)
		if !ok {
			return false, true, fmt.Errorf("carry complement after %s flags", s.fl.kind)
		}
		s.fl.cf = mkBin("xor", cf, IConst{1})
	case x86asm.INVLPG:
		ex.cpuEffect(s, in, cpuInvlpg, ex.addr(s, args[0].(x86asm.Mem)))
	case x86asm.PAUSE:
	case x86asm.PUSHFD:
		if ex.machine {
			// The flags word is assembled by the machine from the record the last flag setter left
			ex.pushWord(s, in, ex.cpuResult(s, in, cpuPushFD, ex.flagsArgs(s)...))
			break
		}
		ex.pushWord(s, in, ex.cpuResult(s, in, cpuPushFD))
	case x86asm.POPFD:
		v := ex.pop(s)
		ex.cpuEffect(s, in, cpuPopFD, v)
		if ex.machine {
			s.fl = flagState{kind: "eflags", a: v}
			s.machineDF = true
			ex.block.term = &blockTerm{kind: "jump", target: next}
			s.va = 0
			return true, true, nil
		}
		// The arithmetic flags a popped word carries are never decided on by compiled code, so a use of them fails
		s.fl = flagState{kind: "alu", a: Sym{"unbound.popfd"}, b: IConst{0}, r: Sym{"unbound.popfd"}, cf: Sym{"unbound.popfd"}}
	case x86asm.PUSHAD:
		esp := s.regs[4]
		for _, r := range []x86asm.Reg{x86asm.EAX, x86asm.ECX, x86asm.EDX, x86asm.EBX} {
			ex.pushWord(s, in, s.getReg(r))
		}
		ex.pushWord(s, in, esp)
		for _, r := range []x86asm.Reg{x86asm.EBP, x86asm.ESI, x86asm.EDI} {
			ex.pushWord(s, in, s.getReg(r))
		}
	case x86asm.POPAD:
		for _, r := range []x86asm.Reg{x86asm.EDI, x86asm.ESI, x86asm.EBP} {
			s.setReg(r, ex.pop(s))
		}
		ex.pop(s)
		for _, r := range []x86asm.Reg{x86asm.EBX, x86asm.EDX, x86asm.ECX, x86asm.EAX} {
			s.setReg(r, ex.pop(s))
		}
	case x86asm.IRETD:
		if ex.block == nil || len(s.frames) > 0 {
			return false, true, fmt.Errorf("interrupt return outside a lifted routine")
		}
		if ex.machine {
			ex.block.term = &blockTerm{kind: "iret"}
			s.va = 0
			return true, true, nil
		}
		ex.cpuEffect(s, in, cpuIRETD)
		s.pending = nil
		s.pushes = 0
		ex.finish(s)
		return true, true, nil
	case x86asm.LOOP, x86asm.LOOPE, x86asm.LOOPNE:
		count := mkBin("sub", s.getReg(x86asm.ECX), IConst{1})
		s.setReg(x86asm.ECX, count)
		cond := mkCond("ne", count, IConst{0})
		switch op {
		case x86asm.LOOPE:
			cond = mkBin("and", cond, ex.condition(s, x86asm.JE))
		case x86asm.LOOPNE:
			cond = mkBin("and", cond, ex.condition(s, x86asm.JNE))
		}
		rel := args[0].(x86asm.Rel)
		taken := in.VA + uint32(in.Inst.Len) + uint32(int32(rel))
		done, err := ex.branch(s, cond, taken, next)
		return done, true, err
	case x86asm.INSB, x86asm.INSW, x86asm.INSD, x86asm.OUTSB, x86asm.OUTSW, x86asm.OUTSD:
		size := map[x86asm.Op]int{x86asm.INSB: 1, x86asm.INSW: 2, x86asm.INSD: 4, x86asm.OUTSB: 1, x86asm.OUTSW: 2, x86asm.OUTSD: 4}[op]
		port := s.getReg(x86asm.DX)
		in_ := op == x86asm.INSB || op == x86asm.INSW || op == x86asm.INSD
		ptrReg := x86asm.ESI
		if in_ {
			ptrReg = x86asm.EDI
		}
		var count Expr = IConst{1}
		if hasRep(in) {
			count = s.getReg(x86asm.ECX)
		}
		if in_ {
			ex.cpuEffect(s, in, cpuINS, port, s.getReg(ptrReg), count, IConst{uint32(size)}, s.stringStep(size))
		} else {
			ex.cpuEffect(s, in, cpuOUTS, port, s.getReg(ptrReg), count, IConst{uint32(size)}, s.stringStep(size))
		}
		s.advance(ptrReg, count, size)
		if hasRep(in) {
			s.setReg(x86asm.ECX, IConst{0})
		}
	case x86asm.LODSB, x86asm.LODSW, x86asm.LODSD:
		size := map[x86asm.Op]int{x86asm.LODSB: 1, x86asm.LODSW: 2, x86asm.LODSD: 4}[op]
		dst := map[x86asm.Op]x86asm.Reg{x86asm.LODSB: x86asm.AL, x86asm.LODSW: x86asm.AX, x86asm.LODSD: x86asm.EAX}[op]
		count := uint32(1)
		if hasRep(in) {
			c, ok := isConst(s.getReg(x86asm.ECX))
			if !ok || c > maxRepUnroll {
				return false, true, fmt.Errorf("rep lods with a symbolic or long count")
			}
			count = c
		}
		for i := uint32(0); i < count; i++ {
			src := s.getReg(x86asm.ESI)
			s.setReg(dst, ex.load(s, src, size))
			s.setReg(x86asm.ESI, mkBin("add", src, s.stringStep(size)))
		}
		if hasRep(in) {
			s.setReg(x86asm.ECX, IConst{0})
		}
	case x86asm.SCASB, x86asm.SCASW, x86asm.SCASD:
		size := map[x86asm.Op]int{x86asm.SCASB: 1, x86asm.SCASW: 2, x86asm.SCASD: 4}[op]
		acc := map[x86asm.Op]x86asm.Reg{x86asm.SCASB: x86asm.AL, x86asm.SCASW: x86asm.AX, x86asm.SCASD: x86asm.EAX}[op]
		value := s.getReg(acc)
		if !hasRep(in) {
			dst := s.getReg(x86asm.EDI)
			m := ex.load(s, dst, size)
			s.fl = narrowFlags(flagState{kind: "cmp", a: value, b: m, r: mkBin("sub", value, m)}, size)
			s.setReg(x86asm.EDI, mkBin("add", dst, s.stringStep(size)))
			break
		}
		// A repeated scan is left to the runtime, which reports how many elements it compared before stopping
		ptr, count := s.getReg(x86asm.EDI), s.getReg(x86asm.ECX)
		repne := IConst{0}
		if repnePrefix(in) {
			repne = IConst{1}
		}
		n := ex.cpuResult(s, in, cpuSCAS, ptr, value, count, IConst{uint32(size)}, s.stringStep(size), repne)
		s.setReg(x86asm.ECX, mkBin("sub", count, n))
		s.advance(x86asm.EDI, n, size)
		last := ex.load(s, mkBin("sub", s.getReg(x86asm.EDI), s.stringStep(size)), size)
		s.fl = narrowFlags(flagState{kind: "cmp", a: value, b: last, r: mkBin("sub", value, last)}, size)
	case x86asm.XLATB:
		s.setReg(x86asm.AL, ex.load(s, mkBin("add", s.getReg(x86asm.EBX), s.getReg(x86asm.AL)), 1))
	case x86asm.CMPXCHG8B:
		m := args[0].(x86asm.Mem)
		at := ex.addr(s, m)
		lo, hi := ex.load(s, at, 4), ex.load(s, mkBin("add", at, IConst{4}), 4)
		eax, edx := s.getReg(x86asm.EAX), s.getReg(x86asm.EDX)
		same := mkBin("and", mkCond("eq", eax, lo), mkCond("eq", edx, hi))
		ex.store(s, at, 4, mkSel(same, s.getReg(x86asm.EBX), lo), in.VA)
		ex.store(s, mkBin("add", at, IConst{4}), 4, mkSel(same, s.getReg(x86asm.ECX), hi), in.VA)
		s.setReg(x86asm.EAX, mkSel(same, eax, lo))
		s.setReg(x86asm.EDX, mkSel(same, edx, hi))
		s.fl = flagState{kind: "cmp", a: same, b: IConst{1}, r: mkBin("sub", same, IConst{1})}
	case x86asm.BT, x86asm.BTS, x86asm.BTR, x86asm.BTC:
		size := opSize(in, args[0])
		bits := uint32(8 * size)
		var base Expr
		var at Expr
		index := ex.operand(s, in, args[1], size)
		if m, isMem := args[0].(x86asm.Mem); isMem {
			at = ex.addr(s, m)
			if _, isImm := args[1].(x86asm.Imm); !isImm {
				// A register index reaches past the operand, the word it lands in being size bytes further per word
				words := mkBin("sar", index, IConst{uint32(3 + size/2)})
				at = mkBin("add", at, mkBin("mul", words, IConst{uint32(size)}))
			}
			base = ex.load(s, at, size)
		} else {
			base = ex.operand(s, in, args[0], size)
		}
		bit := mkBin("and", index, IConst{bits - 1})
		s.fl.cf = mkBin("and", mkBin("shr", base, bit), IConst{1})
		mask := mkBin("shl", IConst{1}, bit)
		var r Expr
		switch op {
		case x86asm.BTS:
			r = mkBin("or", base, mask)
		case x86asm.BTR:
			r = mkBin("and", base, mkUn("not", mask))
		case x86asm.BTC:
			r = mkBin("xor", base, mask)
		default:
			return false, true, nil
		}
		if at != nil {
			ex.store(s, at, size, maskSize(r, size), in.VA)
		} else {
			ex.setOperand(s, in, args[0], size, maskSize(r, size))
		}
	case x86asm.RCL, x86asm.RCR:
		size := opSize(in, args[0])
		bits := uint32(8 * size)
		a := ex.operand(s, in, args[0], size)
		var count Expr = IConst{1}
		if argCount(in) > 1 {
			count = mkBin("and", ex.operand(s, in, args[1], 1), IConst{31})
		}
		n, ok := isConst(count)
		if !ok {
			return false, true, fmt.Errorf("rotate through carry by a symbolic count")
		}
		cf, hasCF := carryOf(s.fl)
		if !hasCF {
			return false, true, fmt.Errorf("rotate through carry after %s flags", s.fl.kind)
		}
		for i := uint32(0); i < n%(bits+1); i++ {
			if op == x86asm.RCL {
				out := mkBin("and", mkBin("shr", a, IConst{bits - 1}), IConst{1})
				a = maskSize(mkBin("or", mkBin("shl", a, IConst{1}), cf), size)
				cf = out
			} else {
				out := mkBin("and", a, IConst{1})
				a = maskSize(mkBin("or", mkBin("shr", a, IConst{1}), mkBin("shl", cf, IConst{bits - 1})), size)
				cf = out
			}
		}
		s.fl = flagState{kind: "alu", a: a, b: count, r: a, cf: cf}
		ex.setOperand(s, in, args[0], size, a)
	case x86asm.WRMSR:
		ex.cpuEffect(s, in, cpuWRMSR, s.getReg(x86asm.ECX), s.getReg(x86asm.EAX), s.getReg(x86asm.EDX))
	case x86asm.RDMSR:
		msr := s.getReg(x86asm.ECX)
		s.setReg(x86asm.EAX, ex.cpuResult(s, in, cpuRDMSRLo, msr))
		s.setReg(x86asm.EDX, ex.cpuResult(s, in, cpuRDMSRHi, msr))
	case x86asm.RDTSC:
		s.setReg(x86asm.EAX, ex.cpuResult(s, in, cpuRDTSCLo))
		s.setReg(x86asm.EDX, ex.cpuResult(s, in, cpuRDTSCHi))
	case x86asm.CPUID:
		leaf, sub := s.getReg(x86asm.EAX), s.getReg(x86asm.ECX)
		for _, r := range []x86asm.Reg{x86asm.EAX, x86asm.EBX, x86asm.ECX, x86asm.EDX} {
			s.setReg(r, ex.cpuResult(s, in, cpuCPUID+r.String(), leaf, sub))
		}
	case x86asm.LGDT, x86asm.LIDT:
		name := cpuLGDT
		if op == x86asm.LIDT {
			name = cpuLIDT
		}
		ex.cpuEffect(s, in, name, ex.addr(s, args[0].(x86asm.Mem)))
	case x86asm.LTR:
		ex.cpuEffect(s, in, cpuLTR, ex.operand(s, in, args[0], 2))
	case x86asm.MOV:
		// A control register moves as a processor call, the register's number naming it
		if cr, ok := args[0].(x86asm.Reg); ok && cr >= x86asm.CR0 && cr <= x86asm.CR15 {
			ex.cpuEffect(s, in, cpuWriteCR, IConst{uint32(cr - x86asm.CR0)}, ex.operand(s, in, args[1], 4))
			return false, true, nil
		}
		if cr, ok := args[1].(x86asm.Reg); ok && cr >= x86asm.CR0 && cr <= x86asm.CR15 {
			ex.setOperand(s, in, args[0], 4, ex.cpuResult(s, in, cpuReadCR, IConst{uint32(cr - x86asm.CR0)}))
			return false, true, nil
		}
		return false, false, nil
	case x86asm.FXSAVE, x86asm.FNSAVE:
		name := cpuFXSave
		if op == x86asm.FNSAVE {
			name = cpuFNSave
		}
		ex.cpuEffect(s, in, name, ex.addr(s, args[0].(x86asm.Mem)))
		if op == x86asm.FNSAVE {
			s.fpu = nil
		}
	case x86asm.FXRSTOR, x86asm.FRSTOR:
		name := cpuFXRstor
		if op == x86asm.FRSTOR {
			name = cpuFRstor
		}
		at := ex.addr(s, args[0].(x86asm.Mem))
		ex.cpuEffect(s, in, name, at)
		// The stack comes back as the runtime restored it, one value a slot
		s.fpu = nil
		for i := 0; i < 8; i++ {
			s.fpu = append(s.fpu, ex.cpuResult(s, in, cpuFRstorST, at, IConst{uint32(i)}))
		}
	case x86asm.LJMP:
		// The far jump reloading the code segment after a new descriptor table lands at its offset
		seg, isSeg := args[0].(x86asm.Imm)
		off, isOff := args[1].(x86asm.Imm)
		if !isSeg || !isOff {
			return false, true, fmt.Errorf("far jump through memory")
		}
		s.setReg(x86asm.CS, IConst{uint32(seg)})
		target := uint32(off)
		if ex.machine {
			ex.block.term = &blockTerm{kind: "jump", target: target}
			s.va = 0
			return true, true, nil
		}
		if ex.block != nil && len(s.frames) == 0 && !ex.block.inside[target] {
			return false, true, fmt.Errorf("far jump out of the routine")
		}
		s.va = target
	case x86asm.AAD:
		// The ascii adjust before division joins the two digits in ax into the low byte by the base
		base := uint32(10)
		if argCount(in) > 0 {
			base = uint32(args[0].(x86asm.Imm)) & 0xFF
		}
		al := maskSize(mkBin("add", mkBin("mul", s.getReg(x86asm.AH), IConst{base}), s.getReg(x86asm.AL)), 1)
		s.setReg(x86asm.AL, al)
		s.setReg(x86asm.AH, IConst{0})
		s.fl = narrowFlags(flagState{kind: "alu", a: al, b: IConst{0}, r: al}, 1)
	case x86asm.AAM:
		// The ascii adjust splits the low byte into its quotient and remainder by the base
		base := uint32(10)
		if argCount(in) > 0 {
			base = uint32(args[0].(x86asm.Imm)) & 0xFF
		}
		al := s.getReg(x86asm.AL)
		s.setReg(x86asm.AH, mkBin("udiv", al, IConst{base}))
		s.setReg(x86asm.AL, mkBin("udiv_rem", al, IConst{base}))
		r := s.getReg(x86asm.AL)
		s.fl = narrowFlags(flagState{kind: "alu", a: r, b: IConst{0}, r: r}, 1)
	case x86asm.FCMOVB, x86asm.FCMOVE, x86asm.FCMOVBE, x86asm.FCMOVU, x86asm.FCMOVNB, x86asm.FCMOVNE, x86asm.FCMOVNBE, x86asm.FCMOVNU:
		jump := map[x86asm.Op]x86asm.Op{x86asm.FCMOVB: x86asm.JB, x86asm.FCMOVE: x86asm.JE, x86asm.FCMOVBE: x86asm.JBE, x86asm.FCMOVU: x86asm.JP, x86asm.FCMOVNB: x86asm.JAE, x86asm.FCMOVNE: x86asm.JNE, x86asm.FCMOVNBE: x86asm.JA, x86asm.FCMOVNU: x86asm.JNP}[op]
		src := stIndex(args[1].(x86asm.Reg))
		s.setSt(0, mkSel(ex.condition(s, jump), s.st(src), s.st(0)))
	default:
		return false, false, nil
	}
	return false, true, nil
}

// The overflow condition the last flag producer left, as a jump on overflow reads it
func (ex *Executor) overflow(s *execState) (Expr, error) {
	f := s.fl
	switch f.kind {
	case "machine":
		return Sym{"cond.JO"}, nil
	case "eflags":
		return mkBin("and", mkBin("shr", f.a, IConst{11}), IConst{1}), nil
	case "add":
		return mkBin("and", mkBin("shr", mkBin("and", mkBin("xor", f.a, f.r), mkBin("xor", f.b, f.r)), IConst{31}), IConst{1}), nil
	case "sub", "cmp":
		return mkBin("and", mkBin("shr", mkBin("and", mkBin("xor", f.a, f.b), mkBin("xor", f.a, f.r)), IConst{31}), IConst{1}), nil
	case "test", "and", "or", "xor":
		return IConst{0}, nil
	}
	return nil, fmt.Errorf("overflow flag after %s flags", f.kind)
}

// Multiplies the narrow accumulator by a byte or word, the product landing in ax or in dx:ax
func (ex *Executor) narrowMultiply(s *execState, in *Instruction, op x86asm.Op, size int) {
	b := ex.operand(s, in, in.Inst.Args[0], size)
	sx := map[int]string{1: "sx8", 2: "sx16"}[size]
	if size == 1 {
		a := s.getReg(x86asm.AL)
		if op == x86asm.IMUL {
			a, b = mkUn(sx, a), mkUn(sx, b)
		}
		r := mkBin("mul", a, b)
		s.setReg(x86asm.AX, r)
		var cf Expr = mkCond("ne", mkBin("shr", maskSize(r, 2), IConst{8}), IConst{0})
		if op == x86asm.IMUL {
			cf = mkCond("ne", mkUn("sx16", r), mkUn("sx8", r))
		}
		s.fl = flagState{kind: "alu", a: r, b: IConst{0}, r: r, cf: cf}
		return
	}
	a := s.getReg(x86asm.AX)
	if op == x86asm.IMUL {
		a, b = mkUn(sx, a), mkUn(sx, b)
	}
	r := mkBin("mul", a, b)
	s.setReg(x86asm.AX, r)
	s.setReg(x86asm.DX, mkBin("shr", r, IConst{16}))
	var cf Expr = mkCond("ne", mkBin("shr", r, IConst{16}), IConst{0})
	if op == x86asm.IMUL {
		cf = mkCond("ne", r, mkUn("sx16", r))
	}
	s.fl = flagState{kind: "alu", a: r, b: IConst{0}, r: r, cf: cf}
}

// Divides the narrow accumulator pair by a byte or word, the quotient and remainder landing where the width puts them
func (ex *Executor) narrowDivide(s *execState, in *Instruction, op x86asm.Op, size int) {
	divisor := ex.operand(s, in, in.Inst.Args[0], size)
	quotient, remainder := "udiv", "udiv_rem"
	if op == x86asm.IDIV {
		quotient, remainder = "idiv", "idiv_rem"
		divisor = mkUn(map[int]string{1: "sx8", 2: "sx16"}[size], divisor)
	}
	if size == 1 {
		dividend := s.getReg(x86asm.AX)
		if op == x86asm.IDIV {
			dividend = mkUn("sx16", dividend)
		}
		s.setReg(x86asm.AL, mkBin(quotient, dividend, divisor))
		s.setReg(x86asm.AH, mkBin(remainder, dividend, divisor))
		return
	}
	dividend := mkBin("or", mkBin("shl", s.getReg(x86asm.DX), IConst{16}), s.getReg(x86asm.AX))
	s.setReg(x86asm.AX, mkBin(quotient, dividend, divisor))
	s.setReg(x86asm.DX, mkBin(remainder, dividend, divisor))
}
