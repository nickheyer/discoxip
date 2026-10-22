package xbe

import (
	"fmt"
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
		return CallResult{Fn: v.Fn, Args: args, Seq: v.Seq, This: mapExpr(v.This, f)}
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
	var out []Path
	for _, p := range paths {
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
			buffers[k] = ArgBuffer{Index: b.Index, Elems: elems}
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
