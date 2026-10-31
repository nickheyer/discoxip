package bios

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/nickheyer/discoxip/pkg/xbe"
)

// Depth the resolver follows callers to when an argument is an input of the routine it is used in, depth the flow
// follows a call block's predecessors apart when their states disagree, and the budget one routine's lift may spend
const (
	resolveDepth = 2
	splitDepth   = 3
	liftSteps    = xbe.DeriveSteps
	liftPaths    = xbe.DerivePaths
	liftDepth    = 64
	liftRetries  = 4
)

// The first stack argument of a lift sits at the entry stack pointer, the return address pushed below
const liftArgBase = xbe.LiftedEntrySP

// lifter runs routines of one image symbolically once each and resolves the inputs their effects depend on through
// the arguments their callers pass
type lifter struct {
	d        *xbe.Disassembly
	mm       *xbe.MachineModel
	writes   map[string][]xbe.OutBuffer
	paths    map[uint32][]xbe.Path
	errs     map[uint32]error
	calls    map[uint32][]liftCall
	bound    map[bindingKey][]binding
	globalsV map[globalKey][]uint32
	varies   map[globalKey]bool
}

// bindingKey names the bindings of a routine resolved to a depth
type bindingKey struct {
	entry uint32
	depth int
}

// globalKey names a global by address and width
type globalKey struct {
	addr uint32
	size int
}

func newLifter(d *xbe.Disassembly, mm *xbe.MachineModel, writes map[string][]xbe.OutBuffer) *lifter {
	return &lifter{d: d, mm: mm, writes: writes, paths: map[uint32][]xbe.Path{}, errs: map[uint32]error{}, calls: map[uint32][]liftCall{}, bound: map[bindingKey][]binding{}, globalsV: map[globalKey][]uint32{}, varies: map[globalKey]bool{}}
}

// The paths of a routine with every callee left as a call, a path that ran out of budget contributing the effects
// it recorded
func (l *lifter) run(entry uint32) []xbe.Path {
	if paths, done := l.paths[entry]; done {
		return paths
	}
	l.paths[entry] = nil
	ex := xbe.NewExecutor(l.d)
	ex.OutBuffers = l.writes
	ex.StepLimit = liftSteps
	ex.PathLimit = liftPaths
	ex.DepthLimit = liftDepth
	if fn := l.d.Functions[entry]; fn != nil {
		for _, t := range fn.CallTargets {
			ex.Opaque[t] = true
		}
	}
	started := time.Now()
	paths, partial, err := ex.Paths(entry, liftRetries)
	if err != nil {
		l.errs[entry] = err
	}
	if err != nil {
		stageTrace(fmt.Sprintf("lifted %08X in %.1fs, %d paths, %v", entry, time.Since(started).Seconds(), len(paths), err))
	} else {
		stageTrace(fmt.Sprintf("lifted %08X in %.1fs, %d paths", entry, time.Since(started).Seconds(), len(paths)))
	}
	if len(partial) > 0 {
		paths = append(paths, xbe.Path{Effects: partial})
	}
	l.paths[entry] = paths
	return paths
}

// The effects the lift of the routine holding an instruction recorded at that instruction, with the routine
func (l *lifter) effectsAt(va uint32, fn string) (uint32, []xbe.Effect) {
	f := l.d.FunctionAt(va)
	if f == nil {
		return 0, nil
	}
	var out []xbe.Effect
	for _, e := range l.effects(f.EntryVA) {
		if e.VA == va && e.Fn == fn {
			out = append(out, e)
		}
	}
	return f.EntryVA, out
}

// Every effect of a routine across its paths
func (l *lifter) effects(entry uint32) []xbe.Effect {
	var out []xbe.Effect
	for _, p := range l.run(entry) {
		out = append(out, p.Effects...)
	}
	return out
}

// liftCall is one call a routine's lift records, with the routine it sits in
type liftCall struct {
	caller uint32
	effect xbe.Effect
}

// Reports whether a call effect reaches the routine at an address, by its name or its target
func callReaches(e xbe.Effect, callee uint32, names ...string) bool {
	if e.Kind != "call" {
		return false
	}
	if e.Target == callee || e.Fn == fmt.Sprintf("sub_%08X", callee) {
		return true
	}
	for _, n := range names {
		if e.Fn == n {
			return true
		}
	}
	return false
}

// The calls the routines calling a routine make to it
func (l *lifter) callsTo(callee uint32, names ...string) []liftCall {
	if sites, done := l.calls[callee]; done && len(names) == 0 {
		return sites
	}
	fn := l.d.Functions[callee]
	if fn == nil {
		return nil
	}
	callers := append([]uint32{}, fn.CalledBy...)
	sort.Slice(callers, func(i, j int) bool { return callers[i] < callers[j] })
	var out []liftCall
	for _, c := range callers {
		for _, e := range l.effects(c) {
			if callReaches(e, callee, names...) {
				out = append(out, liftCall{caller: c, effect: e})
			}
		}
	}
	if len(names) == 0 {
		l.calls[callee] = out
	}
	return out
}

// binding maps the inputs of a routine, registers by name and stack slots by index, to what one caller passes
type binding map[string]xbe.Expr

// The inputs a call hands a routine, registers in the order the callee reads them, then the stack slots
func (l *lifter) bind(callee uint32, e xbe.Effect) binding {
	b := binding{}
	regs := l.d.RegisterInputs(callee)
	if fn := l.d.Functions[callee]; fn != nil && fn.Symbol != nil {
		regs = nil
	}
	for i, a := range e.Args {
		if i < len(regs) {
			b[regs[i]] = a
		} else {
			b[fmt.Sprintf("stack:%d", i-len(regs))] = a
		}
	}
	if e.This != nil {
		b["reg.ECX"] = e.This
	}
	return b
}

// Names the input an expression node reads, a register or a stack slot, or nothing
func inputName(e xbe.Expr) (string, bool) {
	switch v := e.(type) {
	case xbe.Sym:
		if strings.HasPrefix(v.Name, "reg.") {
			return v.Name, true
		}
	case xbe.Load:
		if v.Base == "stack" && v.Addr == nil && v.Off >= liftArgBase && (v.Off-liftArgBase)%4 == 0 {
			return fmt.Sprintf("stack:%d", (v.Off-liftArgBase)/4), true
		}
	}
	return "", false
}

// Rewrites an expression with a caller's bindings in place of the inputs it reads, narrowing a slot to the bytes
// read, constants folded in the same pass
func substitute(e xbe.Expr, b binding) xbe.Expr {
	return xbe.FoldExpr(e, func(n xbe.Expr) (xbe.Expr, bool) {
		name, ok := inputName(n)
		if !ok {
			return foldNode(n)
		}
		v, bound := b[name]
		if !bound {
			return foldNode(n)
		}
		if l, isLoad := n.(xbe.Load); isLoad && l.Size < 4 {
			return fold(xbe.Bin{Op: "and", A: v, B: xbe.IConst{V: 1<<(8*uint(l.Size)) - 1}}), true
		}
		return fold(v), true
	})
}

// Nodes the folds remember before forgetting them all, bounding what the memory holds for them
const foldMemoNodes = 1 << 20

// The folded form of every node folded so far, shared so a chain folded one node longer at a time costs each node once
var folds = xbe.NewFoldMemo()

// Folds the integer operations between constants an expression is built from, bottom up in one pass
func fold(e xbe.Expr) xbe.Expr {
	switch e.(type) {
	case xbe.IConst, xbe.FConst, xbe.Sym, xbe.Ptr:
		return e
	}
	if folds.Len() > foldMemoNodes {
		folds.Reset()
	}
	return folds.Fold(e, foldNode)
}

// Folds one node whose operands are constants
func foldNode(n xbe.Expr) (xbe.Expr, bool) {
	switch v := n.(type) {
	case xbe.Bin:
		a, aok := xbe.ConstOf(v.A)
		b, bok := xbe.ConstOf(v.B)
		if !aok || !bok {
			return nil, false
		}
		switch v.Op {
		case "add":
			return xbe.IConst{V: a + b}, true
		case "sub":
			return xbe.IConst{V: a - b}, true
		case "and":
			return xbe.IConst{V: a & b}, true
		case "or":
			return xbe.IConst{V: a | b}, true
		case "xor":
			return xbe.IConst{V: a ^ b}, true
		case "shl":
			return xbe.IConst{V: a << (b & 31)}, true
		case "shr":
			return xbe.IConst{V: a >> (b & 31)}, true
		case "mul":
			return xbe.IConst{V: a * b}, true
		}
	case xbe.Un:
		a, ok := xbe.ConstOf(v.A)
		if !ok {
			return nil, false
		}
		switch v.Op {
		case "zx8":
			return xbe.IConst{V: a & 0xFF}, true
		case "sx8":
			return xbe.IConst{V: uint32(int32(int8(a)))}, true
		case "zx16":
			return xbe.IConst{V: a & 0xFFFF}, true
		case "sx16":
			return xbe.IConst{V: uint32(int32(int16(a)))}, true
		case "not":
			return xbe.IConst{V: ^a}, true
		}
	}
	return nil, false
}

// Reports whether an expression reads any input of its routine
func readsInputs(e xbe.Expr) bool {
	found := false
	xbe.WalkExpr(e, func(n xbe.Expr) {
		if _, ok := inputName(n); ok {
			found = true
		}
	})
	return found
}

// Reports whether an expression contains a node
func mentions(e, node xbe.Expr) bool {
	found := false
	xbe.WalkExpr(e, func(n xbe.Expr) {
		if xbe.SameExpr(n, node) {
			found = true
		}
	})
	return found
}

// The bindings reaching a routine from its callers, each caller's inputs resolved through its own callers in turn,
// the same binding from several callers kept once
func (l *lifter) bindings(entry uint32, depth int) []binding {
	if depth == 0 {
		return nil
	}
	key := bindingKey{entry, depth}
	if out, done := l.bound[key]; done {
		return out
	}
	l.bound[key] = nil
	seen := map[string]bool{}
	var out []binding
	add := func(b binding) {
		k := b.key()
		if !seen[k] {
			seen[k] = true
			out = append(out, b)
		}
	}
	for _, site := range l.callsTo(entry) {
		b := l.bind(entry, site.effect)
		open := false
		for _, v := range b {
			if readsInputs(v) {
				open = true
			}
		}
		if !open {
			add(b)
			continue
		}
		for _, outer := range l.bindings(site.caller, depth-1) {
			inner := binding{}
			for k, v := range b {
				inner[k] = substitute(v, outer)
			}
			add(inner)
		}
	}
	l.bound[key] = out
	return out
}

// A text naming a binding, the same for two bindings handing the same values to the same inputs
func (b binding) key() string {
	names := make([]string, 0, len(b))
	for name := range b {
		names = append(names, name)
	}
	sort.Strings(names)
	var sb strings.Builder
	for _, name := range names {
		sb.WriteString(name)
		sb.WriteString("=")
		sb.WriteString(xbe.ExprString(b[name]))
		sb.WriteString(";")
	}
	return sb.String()
}

// The constants an expression of a routine takes, its inputs resolved through the callers and its global loads
// through the values the kernel stores there
func (l *lifter) resolve(entry uint32, e xbe.Expr, depth int) []uint32 {
	e = l.globals(fold(e))
	if c, ok := xbe.ConstOf(e); ok {
		return []uint32{c}
	}
	if !readsInputs(e) {
		return nil
	}
	seen := map[uint32]bool{}
	var out []uint32
	for _, b := range l.bindings(entry, depth) {
		if c, ok := xbe.ConstOf(l.globals(substitute(e, b))); ok && !seen[c] {
			seen[c] = true
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// The address a load of a global reads
func globalAt(ld xbe.Load) (uint32, bool) {
	return xbe.FixedAddress(ld.Base, ld.Addr, ld.Off)
}

// Replaces a load of a global the kernel stores exactly one constant into with that constant
func (l *lifter) globals(e xbe.Expr) xbe.Expr {
	if l.mm == nil {
		return e
	}
	return xbe.FoldExpr(e, func(n xbe.Expr) (xbe.Expr, bool) {
		ld, ok := n.(xbe.Load)
		if !ok {
			return foldNode(n)
		}
		addr, global := globalAt(ld)
		if !global {
			return foldNode(n)
		}
		values := l.globalValues(addr, ld.Size)
		if len(values) != 1 || l.globalVaries(addr, ld.Size) {
			return foldNode(n)
		}
		return xbe.IConst{V: values[0]}, true
	})
}

// The constants a global holds at any point, by the stores the kernel makes into it
func (l *lifter) globalValues(addr uint32, size int) []uint32 {
	key := globalKey{addr, size}
	if out, done := l.globalsV[key]; done {
		return out
	}
	seen := map[uint32]bool{}
	out := []uint32{}
	for _, v := range l.mm.StoredValues(addr, size) {
		if c, ok := xbe.ConstOf(v); ok && !seen[c] {
			seen[c] = true
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	l.globalsV[key] = out
	return out
}

// Reports whether any store into a global leaves a value no constant fixes
func (l *lifter) globalVaries(addr uint32, size int) bool {
	key := globalKey{addr, size}
	if _, done := l.varies[key]; !done {
		l.varies[key] = false
		for _, v := range l.mm.StoredValues(addr, size) {
			if _, ok := xbe.ConstOf(v); !ok {
				l.varies[key] = true
			}
		}
	}
	return l.varies[key]
}
