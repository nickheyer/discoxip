package xbe

import (
	"golang.org/x/arch/x86/x86asm"
)

// ctorInfo is the vtable a constructor installs and the register or stack slot its object arrives in
type ctorInfo struct {
	vtable  uint32
	this    argLocation
	library bool // the constructor is a library function identified by signature
}

// construction is one object a function builds, by a constructor call or by stores after an allocation.
type construction struct {
	alloc   uint32
	ctor    uint32
	vtable  uint32
	named   bool
	globals []uint32 // absolute addresses the object pointer is stored to
}

var argumentRegisters = []x86asm.Reg{x86asm.EAX, x86asm.ECX, x86asm.EDX, x86asm.EBX, x86asm.ESI, x86asm.EDI}

// Maps a register of any width to the 32 bit register it belongs to
func wideRegister(r x86asm.Reg) x86asm.Reg {
	idx, ok := regIndex[r]
	if !ok {
		return r
	}
	return []x86asm.Reg{x86asm.EAX, x86asm.ECX, x86asm.EDX, x86asm.EBX, x86asm.ESP, x86asm.EBP, x86asm.ESI, x86asm.EDI}[idx]
}

// Lists the registers an instruction writes besides a plain register to register move
func writtenRegisters(in *Instruction) []x86asm.Reg {
	var out []x86asm.Reg
	switch in.Inst.Op {
	case x86asm.PUSH, x86asm.CMP, x86asm.TEST, x86asm.RET, x86asm.JMP, x86asm.NOP, x86asm.FLD, x86asm.FILD, x86asm.FST, x86asm.FSTP, x86asm.FIST, x86asm.FISTP,
		x86asm.FCOM, x86asm.FCOMP, x86asm.FCOMPP, x86asm.FUCOM, x86asm.FUCOMP, x86asm.FUCOMPP, x86asm.FICOM, x86asm.FICOMP,
		x86asm.FADD, x86asm.FSUB, x86asm.FSUBR, x86asm.FMUL, x86asm.FDIV, x86asm.FDIVR, x86asm.FIADD, x86asm.FISUB, x86asm.FISUBR, x86asm.FIMUL, x86asm.FIDIV, x86asm.FIDIVR,
		x86asm.FADDP, x86asm.FSUBP, x86asm.FSUBRP, x86asm.FMULP, x86asm.FDIVP, x86asm.FDIVRP, x86asm.FXCH, x86asm.FCHS, x86asm.FABS, x86asm.FLD1, x86asm.FLDZ, x86asm.FLDCW, x86asm.FNSTCW:
		return nil
	case x86asm.CALL:
		return []x86asm.Reg{x86asm.EAX, x86asm.ECX, x86asm.EDX}
	case x86asm.CDQ, x86asm.CWD:
		return []x86asm.Reg{x86asm.EDX}
	case x86asm.DIV, x86asm.IDIV, x86asm.MUL:
		return []x86asm.Reg{x86asm.EAX, x86asm.EDX}
	case x86asm.MOVSB, x86asm.MOVSW, x86asm.MOVSD, x86asm.STOSB, x86asm.STOSW, x86asm.STOSD, x86asm.LODSB, x86asm.LODSW, x86asm.LODSD:
		return []x86asm.Reg{x86asm.EAX, x86asm.ECX, x86asm.ESI, x86asm.EDI}
	case x86asm.XCHG:
		for _, a := range in.Inst.Args {
			if r, ok := a.(x86asm.Reg); ok {
				out = append(out, wideRegister(r))
			}
		}
		return out
	case x86asm.IMUL:
		if argCount(in) == 1 {
			return []x86asm.Reg{x86asm.EAX, x86asm.EDX}
		}
	case x86asm.FNSTSW:
		return []x86asm.Reg{x86asm.EAX}
	}
	if len(in.Inst.Args) > 0 {
		if r, ok := in.Inst.Args[0].(x86asm.Reg); ok {
			out = append(out, wideRegister(r))
		}
	}
	return out
}

// Reports whether an instruction copies one register to another
func registerMove(in *Instruction) (dst, src x86asm.Reg, ok bool) {
	if in.Inst.Op != x86asm.MOV || argCount(in) != 2 {
		return 0, 0, false
	}
	d, ok1 := in.Inst.Args[0].(x86asm.Reg)
	s, ok2 := in.Inst.Args[1].(x86asm.Reg)
	if !ok1 || !ok2 || wideRegister(d) != d || wideRegister(s) != s {
		return 0, 0, false
	}
	return d, s, true
}

var callerSaved = []x86asm.Reg{x86asm.EAX, x86asm.ECX, x86asm.EDX}

// Lists the caller saved registers a call target writes, itself or through the functions it calls
func (d *Disassembly) callClobbers(target uint32) []x86asm.Reg {
	fn, ok := d.Functions[target]
	if !ok || fn.Symbol != nil {
		return callerSaved
	}
	if d.clobbers == nil {
		d.clobbers = map[uint32][]x86asm.Reg{}
	}
	if c, done := d.clobbers[target]; done {
		return c
	}
	d.clobbers[target] = nil
	set := map[x86asm.Reg]bool{}
	for i := range fn.Instructions {
		in := &fn.Instructions[i]
		var written []x86asm.Reg
		if in.Inst.Op == x86asm.CALL {
			written = d.callClobbers(resolveTarget(in))
		} else {
			written = writtenRegisters(in)
		}
		for _, r := range written {
			set[r] = true
		}
	}
	var out []x86asm.Reg
	for _, r := range callerSaved {
		if set[r] {
			out = append(out, r)
		}
	}
	d.clobbers[target] = out
	return out
}

// Lists the registers an instruction writes, a call writing what its target writes
func (d *Disassembly) writtenBy(in *Instruction) []x86asm.Reg {
	if in.Inst.Op == x86asm.CALL {
		return d.callClobbers(resolveTarget(in))
	}
	return writtenRegisters(in)
}

// Returns the vtable a constructor stores at its object's start and the register or stack slot the object arrives in
func ctorShape(d *Disassembly, fn *Function) (uint32, argLocation) {
	entry := map[x86asm.Reg]argLocation{}
	for _, r := range argumentRegisters {
		entry[r] = argLocation{Reg: r}
	}
	depth, frame := 0, false
	for i := range fn.Instructions {
		in := &fn.Instructions[i]
		if dst, src, ok := registerMove(in); ok {
			if dst == x86asm.EBP && src == x86asm.ESP {
				frame = true
				continue
			}
			if from, held := entry[src]; held {
				entry[dst] = from
			} else {
				delete(entry, dst)
			}
			continue
		}
		switch in.Inst.Op {
		case x86asm.PUSH:
			depth += 4
		case x86asm.POP:
			depth -= 4
		case x86asm.SUB, x86asm.ADD:
			if r, ok := in.Inst.Args[0].(x86asm.Reg); ok && r == x86asm.ESP {
				if imm, ok := in.Inst.Args[1].(x86asm.Imm); ok {
					if in.Inst.Op == x86asm.SUB {
						depth += int(imm)
					} else {
						depth -= int(imm)
					}
				}
			}
		}
		if in.Inst.Op == x86asm.MOV && argCount(in) == 2 {
			if mem, ok := in.Inst.Args[0].(x86asm.Mem); ok {
				if from, held := entry[mem.Base]; held && mem.Index == 0 && mem.Disp == 0 {
					if imm, ok := in.Inst.Args[1].(x86asm.Imm); ok && d.isVtable(uint32(imm)) {
						return uint32(imm), from
					}
				}
				continue
			}
			if dst, ok := in.Inst.Args[0].(x86asm.Reg); ok {
				if mem, ok := in.Inst.Args[1].(x86asm.Mem); ok {
					if slot, ok := stackArgumentSlot(mem, depth, frame); ok {
						entry[wideRegister(dst)] = argLocation{Stack: slot}
						continue
					}
				}
			}
		}
		for _, r := range d.writtenBy(in) {
			delete(entry, r)
		}
		if len(entry) == 0 {
			break
		}
	}
	return 0, argLocation{}
}

// Reads the stack argument index a memory operand addresses through esp at the push depth or through ebp
func stackArgumentSlot(mem x86asm.Mem, depth int, frame bool) (int, bool) {
	if mem.Index != 0 {
		return 0, false
	}
	var off int64
	switch {
	case mem.Base == x86asm.ESP:
		off = mem.Disp - int64(depth) - 4
	case mem.Base == x86asm.EBP && frame:
		off = mem.Disp - 8
	default:
		return 0, false
	}
	if off < 0 || off%4 != 0 {
		return 0, false
	}
	return int(off / 4), true
}

// Finds every constructor with the vtable it installs and the register it takes its object in
func (d *Disassembly) constructorInfo() map[uint32]ctorInfo {
	if d.ctors != nil {
		return d.ctors
	}
	d.ctors = map[uint32]ctorInfo{}
	for va, fn := range d.Functions {
		if vt, this := ctorShape(d, fn); vt != 0 {
			d.ctors[va] = ctorInfo{vtable: vt, this: this, library: fn.Symbol != nil}
		}
	}
	return d.ctors
}

// Finds the dashboard's constructors as functions that install a vtable into their incoming object
func (d *Disassembly) findConstructors() map[uint32]uint32 {
	out := map[uint32]uint32{}
	for va, info := range d.constructorInfo() {
		if !info.library {
			out[va] = info.vtable
		}
	}
	return out
}

// Returns the vtable of the object a library function hands back through its out pointer argument
func (d *Disassembly) outObjectVtable(target uint32) (uint32, bool) {
	fn := d.Functions[target]
	if fn == nil || fn.Symbol == nil {
		return 0, false
	}
	if _, ok := outParams[fn.Symbol.Name]; !ok {
		return 0, false
	}
	vt := uint32(0)
	for _, c := range d.constructions(fn) {
		if c.vtable == 0 || c.vtable == vt {
			continue
		}
		if vt != 0 {
			return 0, false
		}
		vt = c.vtable
	}
	return vt, vt != 0
}

// Lists the objects a function builds from the results of its calls
func (d *Disassembly) constructions(fn *Function) []construction {
	ctors := d.constructorInfo()
	ins := fn.Instructions
	var out []construction
	for i := range ins {
		if ins[i].Inst.Op != x86asm.CALL {
			continue
		}
		alloc := resolveTarget(&ins[i])
		if alloc == 0 {
			continue
		}
		holders := map[x86asm.Reg]bool{x86asm.EAX: true}
		c := construction{alloc: alloc}
		index := fn.instructionIndex()
		visited := map[int]bool{}
		steps := 0
		// Whether each push since the last call handed the object on, the latest last
		var pushed []bool
		for j := i + 1; j < len(ins) && steps < 96; j++ {
			steps++
			if visited[j] {
				break
			}
			visited[j] = true
			in := &ins[j]
			op := in.Inst.Op
			// A factory jumps past its failure branch to the code initializing the object.
			if op == x86asm.JMP {
				if k, ok := index[resolveTarget(in)]; ok && k > i {
					j = k - 1
					continue
				}
			}
			if op == x86asm.CALL {
				if info, ok := ctors[resolveTarget(in)]; ok && c.ctor == 0 && c.vtable == 0 {
					slot := len(pushed) - 1 - info.this.Stack
					if (info.this.Reg != 0 && holders[info.this.Reg]) || (info.this.Reg == 0 && slot >= 0 && pushed[slot]) {
						c.ctor, c.vtable = resolveTarget(in), info.vtable
					}
				}
				pushed = nil
				for _, r := range d.callClobbers(resolveTarget(in)) {
					delete(holders, r)
				}
				if len(holders) == 0 {
					break
				}
				continue
			}
			if op == x86asm.RET || op == x86asm.JMP {
				break
			}
			if op == x86asm.PUSH {
				if imm, ok := in.Inst.Args[0].(x86asm.Imm); ok && d.namesObject(uint32(imm)) {
					c.named = true
				}
				r, isReg := in.Inst.Args[0].(x86asm.Reg)
				pushed = append(pushed, isReg && holders[wideRegister(r)])
				continue
			}
			if dst, src, ok := registerMove(in); ok {
				if holders[src] {
					holders[dst] = true
				} else {
					delete(holders, dst)
				}
				continue
			}
			if op == x86asm.MOV && argCount(in) == 2 {
				if mem, ok := in.Inst.Args[0].(x86asm.Mem); ok {
					if holders[mem.Base] && mem.Index == 0 {
						if imm, ok := in.Inst.Args[1].(x86asm.Imm); ok {
							switch {
							case mem.Disp == 0 && c.ctor == 0 && d.isVtable(uint32(imm)):
								c.vtable = uint32(imm)
							case d.namesObject(uint32(imm)):
								c.named = true
							}
						}
					}
					if src, ok := in.Inst.Args[1].(x86asm.Reg); ok && holders[src] && mem.Base == 0 && mem.Index == 0 && d.isDataAddress(uint32(mem.Disp)) {
						c.globals = append(c.globals, uint32(mem.Disp))
					}
					continue
				}
			}
			for _, r := range writtenRegisters(in) {
				delete(holders, r)
			}
			if len(holders) == 0 {
				break
			}
		}
		if c.ctor != 0 || c.vtable != 0 {
			out = append(out, c)
		}
	}
	return out
}

// Reports whether an address holds a printable wide string
func (d *Disassembly) namesObject(va uint32) bool {
	s := d.Image.ReadUTF16(va)
	return s != "" && isPrintable(s)
}

// Reports whether an address lies in a writable data section
func (d *Disassembly) isDataAddress(va uint32) bool {
	sec := d.sectionOf(va)
	return sec != nil && sec.Flags&0x1 != 0
}

// Lists the registers an instruction reads, a prologue save of a callee owned register reading nothing
func registersRead(in *Instruction, prologueSave bool) []x86asm.Reg {
	var out []x86asm.Reg
	add := func(r x86asm.Reg) {
		if _, ok := regIndex[r]; ok {
			out = append(out, wideRegister(r))
		}
	}
	switch in.Inst.Op {
	case x86asm.CDQ, x86asm.CWD:
		return []x86asm.Reg{x86asm.EAX}
	case x86asm.DIV, x86asm.IDIV, x86asm.MUL:
		out = append(out, x86asm.EAX, x86asm.EDX)
	case x86asm.MOVSB, x86asm.MOVSW, x86asm.MOVSD:
		return []x86asm.Reg{x86asm.ECX, x86asm.ESI, x86asm.EDI}
	case x86asm.STOSB, x86asm.STOSW, x86asm.STOSD:
		return []x86asm.Reg{x86asm.EAX, x86asm.ECX, x86asm.EDI}
	case x86asm.LODSB, x86asm.LODSW, x86asm.LODSD:
		return []x86asm.Reg{x86asm.ESI}
	case x86asm.PUSH:
		if prologueSave {
			return nil
		}
	case x86asm.IMUL:
		if argCount(in) == 1 {
			out = append(out, x86asm.EAX)
		}
	case x86asm.XOR, x86asm.SUB, x86asm.SBB:
		// Clearing a register with itself reads nothing.
		if !readsFirstOperand(in) {
			return nil
		}
	}
	for i, a := range in.Inst.Args {
		switch v := a.(type) {
		case x86asm.Reg:
			if i == 0 && !readsFirstOperand(in) {
				continue
			}
			add(v)
		case x86asm.Mem:
			if v.Base != 0 {
				add(v.Base)
			}
			if v.Index != 0 {
				add(v.Index)
			}
		}
	}
	return out
}

// Reports whether an instruction reads its destination operand before writing it
func readsFirstOperand(in *Instruction) bool {
	switch in.Inst.Op {
	case x86asm.MOV, x86asm.MOVZX, x86asm.MOVSX, x86asm.LEA, x86asm.POP, x86asm.SETE, x86asm.SETNE, x86asm.SETL, x86asm.SETGE, x86asm.SETLE, x86asm.SETG, x86asm.SETB, x86asm.SETAE, x86asm.SETBE, x86asm.SETA, x86asm.SETS, x86asm.SETNS, x86asm.SETP, x86asm.SETNP,
		x86asm.MOVD, x86asm.MOVSS, x86asm.MOVAPS, x86asm.MOVUPS, x86asm.CVTSI2SS, x86asm.CVTTSS2SI, x86asm.CVTSS2SI, x86asm.FNSTSW, x86asm.LAHF:
		return false
	case x86asm.XOR, x86asm.SUB, x86asm.SBB:
		if argCount(in) == 2 {
			if a, ok := in.Inst.Args[0].(x86asm.Reg); ok {
				if b, ok := in.Inst.Args[1].(x86asm.Reg); ok && a == b {
					return false
				}
			}
		}
	case x86asm.IMUL:
		return argCount(in) < 3
	}
	return true
}
