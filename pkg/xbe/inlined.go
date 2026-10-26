package xbe

import (
	"fmt"
	"slices"
	"strings"

	"golang.org/x/arch/x86/x86asm"
)

// XDK parsers the link time optimizer leaves as calls where it inlined the vertex shader creation around them
const declarationParser = "D3D::ParseDeclarationStream"
const programParser = "D3D::ParseProgram"

// Reports whether a callee creates a vertex shader, directly or as the declaration parse of its inlined body
func createsVertexShader(name string) bool {
	return name == "D3DDevice_CreateVertexShader" || name == declarationParser
}

// Rebuilds an expression top down, each node the function maps replacing its whole subtree
func mapExpr(e Expr, f func(Expr) (Expr, bool)) Expr {
	if e == nil {
		return nil
	}
	if r, ok := f(e); ok {
		return r
	}
	switch v := e.(type) {
	case Load:
		if v.Addr != nil {
			v.Addr = mapExpr(v.Addr, f)
		}
		return v
	case Bin:
		return Bin{v.Op, mapExpr(v.A, f), mapExpr(v.B, f)}
	case Un:
		return Un{v.Op, mapExpr(v.A, f)}
	case Cond:
		return Cond{v.Op, mapExpr(v.A, f), mapExpr(v.B, f)}
	case FCmpSet:
		return FCmpSet{mapExpr(v.A, f), mapExpr(v.B, f), v.Set}
	case Not:
		return Not{mapExpr(v.A, f)}
	case Elem:
		return Elem{mapExpr(v.A, f), v.Index}
	case Sel:
		return Sel{mapExpr(v.C, f), mapExpr(v.A, f), mapExpr(v.B, f)}
	case Block:
		elems := make([]Expr, len(v.Elems))
		for i, x := range v.Elems {
			elems[i] = mapExpr(x, f)
		}
		return Block{elems}
	case CallResult:
		args := make([]Expr, len(v.Args))
		for i, a := range v.Args {
			args[i] = mapExpr(a, f)
		}
		return CallResult{Fn: v.Fn, Args: args, Seq: v.Seq, This: mapExpr(v.This, f), Target: v.Target}
	}
	return e
}

// Reads the call sequence of an allocation result, as the result itself or a pointer to its start
func allocationOf(e Expr) (int, bool) {
	switch v := e.(type) {
	case CallResult:
		return v.Seq, true
	case Ptr:
		var seq int
		if v.Off == 0 {
			if n, _ := fmt.Sscanf(v.Base, "res%d", &seq); n == 1 && v.Base == fmt.Sprintf("res%d", seq) {
				return seq, true
			}
		}
	}
	return 0, false
}

// Reports whether any expression mentions one of the allocations, by result or by memory inside it
func mentionsAllocation(exprs []Expr, seqs map[int]bool) bool {
	found := false
	for _, e := range exprs {
		mapExpr(e, func(x Expr) (Expr, bool) {
			switch v := x.(type) {
			case CallResult:
				if seqs[v.Seq] {
					found = true
				}
			case Ptr:
				found = found || allocationBase(v.Base, seqs)
			case Load:
				found = found || allocationBase(v.Base, seqs)
			}
			return nil, false
		})
	}
	return found
}

func allocationBase(base string, seqs map[int]bool) bool {
	var seq int
	n, _ := fmt.Sscanf(base, "res%d", &seq)
	return n == 1 && base == fmt.Sprintf("res%d", seq) && seqs[seq]
}

// Folds inlined vertex shader creations back into the XDK call, the filled allocation standing for the handle
func (ex *Executor) foldInlinedCreations(paths []Path) ([]Path, error) {
	// An allocation one path fills with a shader is the same one its sibling paths test for failure
	created := map[int]bool{}
	for _, p := range paths {
		for _, e := range p.Effects {
			if e.Kind != "call" || e.Fn != declarationParser {
				continue
			}
			for _, a := range e.Args {
				if s, ok := allocationOf(a); ok {
					created[s] = true
				}
			}
		}
	}
	isCreated := func(e Expr) bool {
		s, ok := allocationOf(e)
		return ok && created[s]
	}
	var out []Path
	for _, p := range paths {
		// The XDK's own out of memory branch is a path the console never runs
		if emptyHanded(p, isCreated) {
			continue
		}
		folded, keep, err := ex.foldCreations(p)
		if err != nil {
			return nil, err
		}
		if keep {
			out = append(out, folded)
		}
	}
	return out, nil
}

// Reports whether a path holds an allocation another path fills to zero
func emptyHanded(p Path, isCreated func(Expr) bool) bool {
	for _, g := range p.Guards {
		if zero, tests := handleTest(g, isCreated); tests && zero {
			return true
		}
	}
	return false
}

func (ex *Executor) foldCreations(p Path) (Path, bool, error) {
	imageAddress := func(args []Expr) (Expr, bool) {
		for _, a := range args {
			if c, ok := constAddr(a); ok && ex.d.sectionOf(c) != nil {
				return IConst{c}, true
			}
		}
		return nil, false
	}
	handles := map[int]CallResult{}
	creationVA := map[int]uint32{}
	for _, e := range p.Effects {
		if e.Kind != "call" || e.Fn != declarationParser {
			continue
		}
		seq := -1
		for _, a := range e.Args {
			if s, ok := allocationOf(a); ok && seq < 0 {
				seq = s
			}
		}
		decl, hasDecl := imageAddress(e.Args)
		if seq < 0 || !hasDecl {
			return Path{}, false, fmt.Errorf("xbe: inlined vertex shader creation at %08X parses no declaration in the image into an allocation", e.VA)
		}
		one := map[int]bool{seq: true}
		prog := Expr(IConst{0})
		for _, pe := range p.Effects {
			if pe.Kind != "call" || pe.Fn != programParser || !mentionsAllocation(pe.Args, one) {
				continue
			}
			if pv, ok := imageAddress(pe.Args); ok {
				prog = pv
			} else {
				return Path{}, false, fmt.Errorf("xbe: inlined vertex shader creation at %08X parses no program in the image", pe.VA)
			}
		}
		handles[seq] = CallResult{Fn: "D3DDevice_CreateVertexShader", Args: []Expr{decl, prog}, Seq: seq}
		creationVA[seq] = e.VA
	}
	if len(handles) == 0 {
		return p, true, nil
	}
	seqs := map[int]bool{}
	for seq := range handles {
		seqs[seq] = true
	}
	// The tagged pointer the body returns and the bare allocation both read as the created handle
	subst := func(e Expr) (Expr, bool) {
		if b, ok := e.(Bin); ok && b.Op == "or" {
			for _, side := range [][2]Expr{{b.A, b.B}, {b.B, b.A}} {
				if c, ok := isConst(side[1]); ok && c == 1 {
					if seq, ok := allocationOf(side[0]); ok && seqs[seq] {
						return handles[seq], true
					}
				}
			}
		}
		if seq, ok := allocationOf(e); ok && seqs[seq] {
			return handles[seq], true
		}
		return nil, false
	}
	isHandle := func(e Expr) bool {
		cr, ok := e.(CallResult)
		return ok && cr.Fn == "D3DDevice_CreateVertexShader" && seqs[cr.Seq]
	}
	out := Path{Return: mapExpr(p.Return, subst)}
	kept := make([]int, len(p.Effects)+1)
	for i, e := range p.Effects {
		kept[i] = len(out.Effects)
		switch {
		case e.Kind == "call" && seqs[e.Seq]:
			h := handles[e.Seq]
			out.Effects = append(out.Effects, Effect{Kind: "call", VA: creationVA[e.Seq], Fn: h.Fn, Args: h.Args, Seq: h.Seq})
			continue
		case e.Kind == "call" && strings.HasPrefix(e.Fn, "D3D::") && mentionsAllocation(e.Args, seqs):
			continue
		case e.Kind != "call" && (allocationBase(e.Base, seqs) || (e.Addr != nil && mentionsAllocation([]Expr{e.Addr}, seqs))):
			continue
		}
		e.Value = mapExpr(e.Value, subst)
		e.Addr = mapExpr(e.Addr, subst)
		e.This = mapExpr(e.This, subst)
		args := make([]Expr, len(e.Args))
		for k, a := range e.Args {
			args[k] = mapExpr(a, subst)
		}
		e.Args = args
		buffers := make([]ArgBuffer, len(e.Buffers))
		for k, b := range e.Buffers {
			elems := make([]Expr, len(b.Elems))
			for j, x := range b.Elems {
				elems[j] = mapExpr(x, subst)
			}
			buffers[k] = ArgBuffer{Index: b.Index, Elems: elems, Sizes: b.Sizes}
		}
		e.Buffers = buffers
		out.Effects = append(out.Effects, e)
	}
	kept[len(p.Effects)] = len(out.Effects)
	for i, g := range p.Guards {
		g = mapExpr(g, subst)
		// The body tests its allocation, the failing branch being the XDK's own out of memory path
		if zero, tests := handleTest(g, isHandle); tests {
			if zero {
				return Path{}, false, nil
			}
			continue
		}
		pos := len(p.Effects)
		if i < len(p.GuardPos) {
			pos = p.GuardPos[i]
		}
		out.Guards = append(out.Guards, g)
		out.GuardPos = append(out.GuardPos, kept[pos])
	}
	return out, true, nil
}

// Reads a guard comparing a created handle with zero, reporting whether it holds the handle to be zero
func handleTest(g Expr, isHandle func(Expr) bool) (zero bool, tests bool) {
	negated := false
	if n, ok := g.(Not); ok {
		g, negated = n.A, true
	}
	c, ok := g.(Cond)
	if !ok {
		return false, false
	}
	other := c.B
	if !isHandle(c.A) {
		other = c.A
		if !isHandle(c.B) {
			return false, false
		}
	}
	if v, ok := isConst(other); !ok || v != 0 {
		return false, false
	}
	switch c.Op {
	case "eq":
		zero = true
	case "ne":
		zero = false
	default:
		return false, false
	}
	return zero != negated, true
}

// The counted array element accessor, a call in plain builds and inline code where the optimizer expanded it
const arrayElementCall = "array_element"

// arrayIndex is one inlined counted array index, reading the element at an index of the array in an object
type arrayIndex struct {
	exit  uint32
	dest  x86asm.Reg
	base  x86asm.Reg
	off   int32
	index x86asm.Reg
	temp  x86asm.Reg
}

// Reads the element load of an inlined index, with the instruction that took the array pointer from the object
func elementLoad(fn *Function, i int) (arrayIndex, int, bool) {
	in := &fn.Instructions[i]
	if in.Inst.Op != x86asm.MOV || in.Inst.MemBytes != 4 || argCount(in) != 2 {
		return arrayIndex{}, 0, false
	}
	dest, isReg := in.Inst.Args[0].(x86asm.Reg)
	m, isMem := in.Inst.Args[1].(x86asm.Mem)
	if !isReg || !isMem || m.Scale != 4 || m.Index == 0 || m.Base == 0 || m.Disp != 0 || m.Segment != 0 {
		return arrayIndex{}, 0, false
	}
	for j := i - 1; j >= 0 && j > i-8; j-- {
		p := &fn.Instructions[j]
		if p.Inst.Op == x86asm.CALL || isJump(p.Inst.Op) {
			break
		}
		r, isReg := p.Inst.Args[0].(x86asm.Reg)
		if !isReg || wideRegister(r) != wideRegister(m.Base) {
			continue
		}
		pm, isMem := p.Inst.Args[1].(x86asm.Mem)
		if p.Inst.Op != x86asm.MOV || !isMem || pm.Base == 0 || pm.Index != 0 || pm.Disp < 8 || p.Inst.MemBytes != 4 {
			break
		}
		if wideRegister(pm.Base) == wideRegister(dest) {
			break
		}
		a := arrayIndex{dest: dest, base: pm.Base, off: int32(pm.Disp) - 8, index: m.Index, temp: m.Base}
		return a, j, true
	}
	return arrayIndex{}, 0, false
}

// Reads the bounds test above an element load, whose every failing branch leaves the element zero
func boundsTest(fn *Function, index map[uint32]int, i, load int, a arrayIndex) (uint32, arrayIndex, bool) {
	if i+1 >= len(fn.Instructions) || fn.Instructions[i+1].Inst.Op != x86asm.JMP {
		return 0, a, false
	}
	exit := resolveTarget(&fn.Instructions[i+1])
	null, start := -1, 0
	for j := load - 1; j >= 1; j -= 2 {
		jump := &fn.Instructions[j]
		if !isConditionalJump(jump.Inst.Op) {
			break
		}
		k, ok := index[resolveTarget(jump)]
		if !ok || (null >= 0 && k != null) {
			break
		}
		if op := fn.Instructions[j-1].Inst.Op; op != x86asm.CMP && op != x86asm.TEST {
			break
		}
		null, start = k, j-1
	}
	if null < 0 || null+1 >= len(fn.Instructions) {
		return 0, a, false
	}
	zero := &fn.Instructions[null]
	r, isReg := zero.Inst.Args[0].(x86asm.Reg)
	if zero.Inst.Op != x86asm.XOR || !isReg || r != a.dest || zero.Inst.Args[1] != x86asm.Arg(a.dest) {
		return 0, a, false
	}
	if fn.Instructions[null+1].VA != exit {
		return 0, a, false
	}
	a.exit = exit
	return fn.Instructions[start].VA, a, true
}

// Finds the counted array indexes the optimizer inlined in a function, by the instruction each one begins at
func inlinedArrayIndexes(fn *Function) map[uint32]arrayIndex {
	var out map[uint32]arrayIndex
	index := fn.instructionIndex()
	for i := range fn.Instructions {
		a, load, ok := elementLoad(fn, i)
		if !ok {
			continue
		}
		start, a, ok := boundsTest(fn, index, i, load, a)
		if !ok {
			continue
		}
		if out == nil {
			out = map[uint32]arrayIndex{}
		}
		out[start] = a
	}
	return out
}

// Bits of the XDK resource header the reference count helpers test before following its cached data pointer
const (
	resourceRefMask  = 0xffff
	resourceTypeMask = 0x70000
	resourceCached   = 0x50000
	resourceBusyMask = 0x780000
)

// resourceRef is one reference count helper the optimizer expanded inline, standing for the call it replaced
type resourceRef struct {
	fn    string
	exit  uint32
	base  x86asm.Reg
	regs  []x86asm.Reg
	carry []uint32 // instructions the scheduler placed inside the body, which run ahead of the call
}

// patternWalk reads a fixed instruction pattern, carrying the unrelated instructions scheduled between its steps
type patternWalk struct {
	fn     *Function
	index  map[uint32]int
	i      int
	first  int
	carry  []int
	closed bool // a branch of the pattern has been read, after which nothing may be carried past it
	ok     bool
}

// Reports whether an instruction may run ahead of the folded call, touching neither the flags nor the pattern
func (w *patternWalk) carries(in *Instruction, deps []x86asm.Reg) bool {
	if in.Inst.Op != x86asm.MOV && in.Inst.Op != x86asm.LEA {
		return false
	}
	for _, r := range writtenRegisters(in) {
		if slices.Contains(deps, wideRegister(r)) || r == x86asm.ESP {
			return false
		}
	}
	m, isMem := in.Inst.Args[0].(x86asm.Mem)
	return !isMem || (!slices.Contains(deps, m.Base) && !slices.Contains(deps, m.Index))
}

// Reads the next instruction of the pattern, carrying anything unrelated the scheduler placed before it
func (w *patternWalk) expect(deps []x86asm.Reg, match func(*Instruction) bool) *Instruction {
	for w.ok && w.i < len(w.fn.Instructions) {
		in := &w.fn.Instructions[w.i]
		if match(in) {
			w.i++
			return in
		}
		if w.closed || !w.carries(in, deps) {
			break
		}
		w.carry = append(w.carry, w.i)
		w.i++
	}
	w.ok = false
	return nil
}

// Reads a conditional jump of the pattern, after which the scheduler may move nothing across
func (w *patternWalk) expectJump(deps []x86asm.Reg) int {
	in := w.expect(deps, func(in *Instruction) bool { return isConditionalJump(in.Inst.Op) })
	if in == nil {
		return -1
	}
	w.closed = true
	target, known := w.index[resolveTarget(in)]
	if !known {
		w.ok = false
		return -1
	}
	return target
}

// Reads the instruction at an index of the pattern without consuming it
func (w *patternWalk) at(i int) *Instruction {
	if !w.ok || i < 0 || i >= len(w.fn.Instructions) {
		w.ok = false
		return nil
	}
	return &w.fn.Instructions[i]
}

// Reports whether an instruction applies an immediate to a register
func opRegImm(in *Instruction, op x86asm.Op, r x86asm.Reg, imm uint32) bool {
	if in.Inst.Op != op || argCount(in) != 2 {
		return false
	}
	a, isReg := in.Inst.Args[0].(x86asm.Reg)
	v, isImm := in.Inst.Args[1].(x86asm.Imm)
	return isReg && isImm && a == r && uint32(v) == imm
}

// Reports whether an instruction loads a word of the object a register points at
func opRegLoad(in *Instruction, dst, base x86asm.Reg) bool {
	if in.Inst.Op != x86asm.MOV || argCount(in) != 2 || in.Inst.MemBytes != 4 {
		return false
	}
	a, isReg := in.Inst.Args[0].(x86asm.Reg)
	m, isMem := in.Inst.Args[1].(x86asm.Mem)
	return isReg && isMem && a == dst && m.Base == base && m.Index == 0 && m.Segment == 0
}

// Reports whether an instruction compares a register with a value, as an immediate or through another register
func opCompare(in *Instruction, op x86asm.Op, r x86asm.Reg, imm uint32) bool {
	if opRegImm(in, op, r, imm) {
		return true
	}
	a, isReg := in.Inst.Args[0].(x86asm.Reg)
	_, other := in.Inst.Args[1].(x86asm.Reg)
	return in.Inst.Op == op && argCount(in) == 2 && isReg && other && a == r
}

// Reads the guarded recursion both helpers share, where a cached resource hands its data pointer to the helper,
// the join being the step past it or negative for the first branch of the body to name it
func (w *patternWalk) cachedDataCall(v, base x86asm.Reg, join int) (uint32, int) {
	deps := []x86asm.Reg{v, base}
	w.expect(deps, func(in *Instruction) bool { return opRegImm(in, x86asm.AND, v, resourceTypeMask) })
	w.expect(deps, func(in *Instruction) bool { return opRegImm(in, x86asm.CMP, v, resourceCached) })
	to := w.expectJump(deps)
	if join < 0 {
		join = to
	}
	if to != join {
		w.ok = false
	}
	w.expect(deps, func(in *Instruction) bool { return opRegLoad(in, v, base) })
	w.expect(deps, func(in *Instruction) bool {
		return opCompare(in, x86asm.CMP, v, 0) || (in.Inst.Op == x86asm.TEST && in.Inst.Args[0] == x86asm.Arg(v) && in.Inst.Args[1] == x86asm.Arg(v))
	})
	if to := w.expectJump(deps); to != join {
		w.ok = false
	}
	w.expect(deps, func(in *Instruction) bool { return in.Inst.Op == x86asm.PUSH && in.Inst.Args[0] == x86asm.Arg(v) })
	call := w.expect(deps, func(in *Instruction) bool { return in.Inst.Op == x86asm.CALL && resolveTarget(in) != 0 })
	if !w.ok || w.i != join {
		w.ok = false
		return 0, join
	}
	return resolveTarget(call), join
}

// Reads the reference taking helper: the cached recursion, then the count raised in place
func matchResourceAddRef(fn *Function, index map[uint32]int, i int) (resourceRef, int, uint32, bool) {
	w := &patternWalk{fn: fn, index: index, i: i, first: i, ok: true}
	in := w.at(i)
	v, isReg := in.Inst.Args[0].(x86asm.Reg)
	m, isMem := in.Inst.Args[1].(x86asm.Mem)
	if !w.ok || in.Inst.Op != x86asm.MOV || !isReg || !isMem || m.Base == 0 || m.Index != 0 || m.Disp != 0 || in.Inst.MemBytes != 4 {
		return resourceRef{}, 0, 0, false
	}
	base, deps := m.Base, []x86asm.Reg{v, m.Base}
	w.i++
	w.expect(deps, func(in *Instruction) bool { return opRegImm(in, x86asm.TEST, v, resourceRefMask) })
	join := w.expectJump(deps)
	target, _ := w.cachedDataCall(v, base, join)
	bump := w.at(join)
	if !w.ok {
		return resourceRef{}, 0, 0, false
	}
	bm, isMem := bump.Inst.Args[0].(x86asm.Mem)
	if bump.Inst.Op != x86asm.INC || !isMem || bm.Base != base || bm.Index != 0 || bm.Disp != 0 {
		return resourceRef{}, 0, 0, false
	}
	return resourceRef{exit: bump.VA + uint32(bump.Inst.Len), base: base, carry: w.carried()}, join, target, true
}

// Reads the reference dropping helper: the last reference frees the cached data and destroys the resource
func matchResourceRelease(fn *Function, index map[uint32]int, i int) (resourceRef, int, uint32, bool) {
	w := &patternWalk{fn: fn, index: index, i: i, first: i, ok: true}
	in := w.at(i)
	v, isReg := in.Inst.Args[0].(x86asm.Reg)
	m, isMem := in.Inst.Args[1].(x86asm.Mem)
	if !w.ok || in.Inst.Op != x86asm.MOV || !isReg || !isMem || m.Base == 0 || m.Index != 0 || m.Disp != 0 || in.Inst.MemBytes != 4 {
		return resourceRef{}, 0, 0, false
	}
	base := m.Base
	w.i++
	count := x86asm.Reg(0)
	w.expect([]x86asm.Reg{v, base}, func(in *Instruction) bool {
		r, isReg := in.Inst.Args[0].(x86asm.Reg)
		if in.Inst.Op != x86asm.MOV || !isReg || in.Inst.Args[1] != x86asm.Arg(v) {
			return false
		}
		count = r
		return true
	})
	deps := []x86asm.Reg{v, base, count}
	w.expect(deps, func(in *Instruction) bool { return opRegImm(in, x86asm.AND, count, resourceRefMask) })
	w.expect(deps, func(in *Instruction) bool { return opCompare(in, x86asm.CMP, count, 1) })
	last := w.expectJump(deps)
	target, join := w.cachedDataCall(v, base, -1)
	w.expect(deps, func(in *Instruction) bool { return opRegLoad(in, v, base) })
	w.expect(deps, func(in *Instruction) bool { return opRegImm(in, x86asm.TEST, v, resourceBusyMask) })
	if to := w.expectJump(deps); to != last {
		w.ok = false
	}
	w.expect(deps, func(in *Instruction) bool { return in.Inst.Op == x86asm.CALL && resolveTarget(in) != 0 })
	done := -1
	if leave := w.expect(deps, func(in *Instruction) bool { return in.Inst.Op == x86asm.JMP }); leave != nil {
		done = index[resolveTarget(leave)]
	}
	drop, store := w.at(last), w.at(last+1)
	if !w.ok || join < 0 || w.i != last || done != last+2 {
		return resourceRef{}, 0, 0, false
	}
	sm, isMem := store.Inst.Args[0].(x86asm.Mem)
	if drop.Inst.Op != x86asm.DEC || drop.Inst.Args[0] != x86asm.Arg(v) || !isMem {
		return resourceRef{}, 0, 0, false
	}
	if store.Inst.Op != x86asm.MOV || sm.Base != base || sm.Index != 0 || sm.Disp != 0 {
		return resourceRef{}, 0, 0, false
	}
	return resourceRef{exit: fn.Instructions[done].VA, base: base, carry: w.carried()}, done - 1, target, true
}

// The addresses of the instructions the scheduler placed inside the body, in the order they run
func (w *patternWalk) carried() []uint32 {
	var out []uint32
	for _, i := range w.carry {
		out = append(out, w.fn.Instructions[i].VA)
	}
	return out
}

// Reports whether a named helper hands a cached resource's data pointer back to itself, as both count helpers do
func (d *Disassembly) selfCachedRecursion(target uint32) bool {
	fn := d.Functions[target]
	if fn == nil || fn.Symbol == nil {
		return false
	}
	index := fn.instructionIndex()
	for i := range fn.Instructions {
		v, isReg := fn.Instructions[i].Inst.Args[0].(x86asm.Reg)
		if !isReg || !opRegImm(&fn.Instructions[i], x86asm.AND, v, resourceTypeMask) || i+3 >= len(fn.Instructions) {
			continue
		}
		m, isMem := fn.Instructions[i+3].Inst.Args[1].(x86asm.Mem)
		if !isMem || m.Base == 0 {
			continue
		}
		w := &patternWalk{fn: fn, index: index, i: i, first: i, ok: true}
		if callee, _ := w.cachedDataCall(v, m.Base, -1); w.ok && callee == target {
			return true
		}
	}
	return false
}

// Records every reference count helper the optimizer expanded into the dashboard's own code
func (d *Disassembly) scanResourceRefs() {
	d.resourceRef = map[uint32]resourceRef{}
	for _, va := range d.functionVAs() {
		fn := d.Functions[va]
		if fn.Symbol != nil || d.inLibrary(va) {
			continue
		}
		index := fn.instructionIndex()
		for i := range fn.Instructions {
			r, last, target, ok := matchResourceAddRef(fn, index, i)
			if !ok {
				r, last, target, ok = matchResourceRelease(fn, index, i)
			}
			if !ok || !d.selfCachedRecursion(target) {
				continue
			}
			r.fn = d.Functions[target].Symbol.Name
			for j := i; j <= last; j++ {
				for _, w := range d.writtenBy(&fn.Instructions[j]) {
					if w != x86asm.ESP && !slices.Contains(r.regs, w) {
						r.regs = append(r.regs, w)
					}
				}
			}
			d.resourceRef[fn.Instructions[i].VA] = r
		}
	}
}
