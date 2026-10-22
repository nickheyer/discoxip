package xbe

import (
	"fmt"
	"sort"
	"strings"

	"golang.org/x/arch/x86/x86asm"
)

// argLocation is where a function takes one input, a register or a stack argument index, or nowhere.
type argLocation struct {
	Reg   x86asm.Reg
	Stack int
	None  bool
}

// Seeds one input with a value, the other stack arguments staying symbolic
func (l argLocation) seed(s *execState, v Expr, stackArgs int) {
	if l.Reg != 0 {
		s.regs[regIndex[l.Reg]] = v
		if stackArgs > 0 {
			s.pushArgs(symbolicArgs(stackArgs, -1, nil)...)
		}
		return
	}
	n := stackArgs
	if l.Stack >= n {
		n = l.Stack + 1
	}
	s.pushArgs(symbolicArgs(n, l.Stack, v)...)
}

func symbolicArgs(n, at int, v Expr) []Expr {
	args := make([]Expr, n)
	for i := range args {
		if i == at {
			args[i] = v
		} else {
			args[i] = Sym{fmt.Sprintf("arg%d", i)}
		}
	}
	return args
}

// shaderLookup is a function creating vertex shaders and the location it takes the vertex format in.
type shaderLookup struct {
	va     uint32
	format argLocation
	inputs int // stack arguments the function takes
}

// accessorSite is one place the dashboard reads a mesh's vertex format before looking a shader up.
type accessorSite struct {
	fn      uint32
	index   int // instruction index where the mesh register is read for the accessor
	meshReg x86asm.Reg
}

// Finds the functions creating vertex shaders and how the code feeds them the mesh vertex format
func (rm *RenderModel) deriveMeshAccessor(d *Disassembly) error {
	var lookups []uint32
	for _, va := range d.functionVAs() {
		fn := d.Functions[va]
		if fn.Symbol != nil {
			continue
		}
		for _, t := range fn.CallTargets {
			if c := d.Functions[t]; c != nil && c.Symbol != nil && createsVertexShader(c.Symbol.Name) {
				lookups = appendUnique(lookups, va)
			}
		}
	}
	if len(lookups) == 0 {
		return fmt.Errorf("xbe: no function creates vertex shaders")
	}
	isLookup := map[uint32]bool{}
	for _, va := range lookups {
		isLookup[va] = true
	}
	base := NewExecutor(d)
	shapes := map[uint32]*accessorProbe{}
	inputs := map[uint32]map[argLocation]int{}
	var acc *MeshAccessor
	for _, va := range d.functionVAs() {
		fn := d.Functions[va]
		if fn.Symbol != nil || isLookup[va] {
			continue
		}
		ins := fn.Instructions
		for i := range ins {
			if ins[i].Inst.Op != x86asm.CALL {
				continue
			}
			lookup := resolveTarget(&ins[i])
			if !isLookup[lookup] {
				continue
			}
			site, found, err := formatSource(d, base, shapes, fn, i, lookup)
			if err != nil {
				return err
			}
			if !found {
				continue
			}
			if acc == nil {
				acc = &site.acc
			} else if acc.Offset != site.acc.Offset || acc.Slot != site.acc.Slot || (acc.Function != 0 && site.acc.Function != 0 && acc.Function != site.acc.Function) {
				return fmt.Errorf("xbe: lookup %08X at %08X reads the format through %+v, elsewhere through %+v", lookup, ins[i].VA, site.acc, *acc)
			} else if acc.Function == 0 {
				acc.Function = site.acc.Function
			}
			if inputs[lookup] == nil {
				inputs[lookup] = map[argLocation]int{}
			}
			inputs[lookup][site.input]++
			rm.accessorSites = append(rm.accessorSites, accessorSite{fn: va, index: site.index, meshReg: site.meshReg})
		}
	}
	if acc == nil {
		return fmt.Errorf("xbe: no call of a shader lookup (%X) is fed by a mesh format accessor", lookups)
	}
	rm.Accessor = *acc
	for _, va := range lookups {
		locs := inputs[va]
		if len(locs) == 0 {
			continue
		}
		if len(locs) > 1 {
			return fmt.Errorf("xbe: shader lookup %08X takes its format in several places: %v", va, locs)
		}
		for loc := range locs {
			rm.lookups = append(rm.lookups, shaderLookup{va: va, format: loc, inputs: calleeCleanupOf(d.Functions[va]) / 4})
		}
	}
	sort.Slice(rm.lookups, func(i, j int) bool { return rm.lookups[i].va < rm.lookups[j].va })
	// A lookup taking nothing but the format, in a register or on the stack, serves the fixed function meshes
	for _, l := range rm.lookups {
		if l.inputs+len(d.liveInRegisters(d.Functions[l.va])) != 1 {
			continue
		}
		if rm.shaderLookup != 0 {
			return fmt.Errorf("xbe: two shader lookups take only a format (%08X, %08X)", rm.shaderLookup, l.va)
		}
		rm.shaderLookup = l.va
	}
	return nil
}

// accessorProbe is what running a candidate accessor function on a symbolic mesh revealed.
type accessorProbe struct {
	acc     *MeshAccessor
	meshReg x86asm.Reg
}

// formatSite is a lookup call with the accessor feeding it and where the lookup receives the format.
type formatSite struct {
	acc     MeshAccessor
	meshReg x86asm.Reg
	index   int // instruction reading the mesh register
	input   argLocation
}

// Finds the accessor whose result reaches a lookup call and the location the lookup takes it in
func formatSource(d *Disassembly, base *Executor, shapes map[uint32]*accessorProbe, fn *Function, i int, lookup uint32) (formatSite, bool, error) {
	ins := fn.Instructions
	for j := i - 1; j >= 0 && j >= i-24; j-- {
		op := ins[j].Inst.Op
		if op == x86asm.RET || isUnconditionalJump(op) {
			return formatSite{}, false, nil
		}
		if op != x86asm.CALL {
			continue
		}
		var site formatSite
		site.index = j
		if t := resolveTarget(&ins[j]); t != 0 {
			probe, ok := shapes[t]
			if !ok {
				p, err := accessorShape(d, base, t)
				if err != nil {
					return formatSite{}, false, err
				}
				probe = p
				shapes[t] = probe
			}
			if probe == nil || probe.acc == nil {
				return formatSite{}, false, nil
			}
			site.acc, site.meshReg = *probe.acc, probe.meshReg
		} else {
			acc, meshReg, use, ok := inlinedAccessor(ins, j)
			if !ok {
				return formatSite{}, false, nil
			}
			site.acc, site.meshReg, site.index = acc, meshReg, use
		}
		loc, ok := resultLocation(d, ins, j, i, lookup)
		if !ok {
			return formatSite{}, false, fmt.Errorf("xbe: accessor result at %08X does not reach the lookup call at %08X", ins[j].VA, ins[i].VA)
		}
		site.input = loc
		return site, true, nil
	}
	return formatSite{}, false, nil
}

// Follows eax from after a call to a later call, naming the register or stack slot it arrives in
func resultLocation(d *Disassembly, ins []Instruction, from, to int, callee uint32) (argLocation, bool) {
	holders := map[x86asm.Reg]bool{x86asm.EAX: true}
	pushedAt := -1
	pushes := 0
	for k := from + 1; k < to; k++ {
		in := &ins[k]
		if in.Inst.Op == x86asm.PUSH {
			if r, ok := in.Inst.Args[0].(x86asm.Reg); ok && holders[wideRegister(r)] {
				pushedAt = pushes
			}
			pushes++
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
		for _, r := range d.writtenBy(in) {
			delete(holders, r)
		}
	}
	live := map[x86asm.Reg]bool{}
	for _, r := range d.liveInRegisters(d.Functions[callee]) {
		live[r] = true
	}
	for _, r := range argumentRegisters {
		if holders[r] && live[r] {
			return argLocation{Reg: r}, true
		}
	}
	if pushedAt >= 0 {
		return argLocation{Stack: pushes - 1 - pushedAt}, true
	}
	return argLocation{}, false
}

// Recognizes a virtual call on an object loaded from a field of the mesh held in a register, returning the instruction reading the mesh
func inlinedAccessor(ins []Instruction, j int) (MeshAccessor, x86asm.Reg, int, bool) {
	m, ok := ins[j].Inst.Args[0].(x86asm.Mem)
	if !ok || m.Base == 0 || m.Index != 0 {
		return MeshAccessor{}, 0, 0, false
	}
	vtableReg := m.Base
	objReg := x86asm.Reg(0)
	for k := j - 1; k >= 0 && k >= j-6; k-- {
		in := &ins[k]
		// The object may move between registers after its load and before its vtable read
		if dst, src, ok := registerMove(in); ok {
			if dst == objReg {
				objReg = src
			}
			continue
		}
		if in.Inst.Op != x86asm.MOV || argCount(in) != 2 {
			continue
		}
		dst, ok := in.Inst.Args[0].(x86asm.Reg)
		src, isMem := in.Inst.Args[1].(x86asm.Mem)
		if !ok || !isMem || src.Index != 0 || src.Base == 0 {
			continue
		}
		if objReg == 0 {
			if dst == vtableReg && src.Disp == 0 {
				objReg = src.Base
			}
			continue
		}
		if dst == objReg {
			return MeshAccessor{Offset: int32(src.Disp), Slot: int32(m.Disp)}, src.Base, k, true
		}
	}
	return MeshAccessor{}, 0, 0, false
}

// Runs a candidate accessor on a symbolic mesh in each register it reads and finds the field and slot of its virtual call
func accessorShape(d *Disassembly, base *Executor, va uint32) (*accessorProbe, error) {
	fn := d.Functions[va]
	if fn == nil || fn.Symbol != nil {
		return nil, nil
	}
	ex := NewExecutorShared(base)
	ex.StepLimit = 20000
	ex.PathLimit = 64
	regs := d.liveInRegisters(fn)
	paths, err := ex.Run(va, func(s *execState) {
		for _, r := range regs {
			s.regs[regIndex[r]] = Ptr{"in." + r.String(), 0}
		}
	})
	if err != nil {
		return nil, nil
	}
	for _, p := range paths {
		for _, e := range p.Effects {
			if e.Kind != "call" || !strings.HasPrefix(e.Fn, "vcall@") || len(e.Args) != 1 {
				continue
			}
			var slot int32
			if n, _ := fmt.Sscanf(e.Fn, "vcall@%X", &slot); n != 1 {
				continue
			}
			l, ok := e.Args[0].(Load)
			if !ok || l.Addr != nil || !strings.HasPrefix(l.Base, "in.") {
				return nil, fmt.Errorf("xbe: format accessor %08X makes its virtual call on %s", va, ExprString(e.Args[0]))
			}
			for _, r := range regs {
				if "in."+r.String() == l.Base {
					return &accessorProbe{acc: &MeshAccessor{Function: va, Offset: l.Off, Slot: slot}, meshReg: r}, nil
				}
			}
		}
	}
	return nil, nil
}

// Finds the global that holds the mesh being drawn as the one most often loaded into the accessor's mesh register
func (rm *RenderModel) deriveCurrentMesh(d *Disassembly) error {
	found := map[uint32]int{}
	flows := map[uint32][]map[x86asm.Reg]uint32{}
	for _, site := range rm.accessorSites {
		states, ok := flows[site.fn]
		if !ok {
			states = d.globalLoadFlow(d.Functions[site.fn])
			flows[site.fn] = states
		}
		if states[site.index] == nil {
			continue
		}
		if g, ok := states[site.index][site.meshReg]; ok {
			found[g]++
		}
	}
	best, bestN := uint32(0), 0
	keys := make([]uint32, 0, len(found))
	for va := range found {
		keys = append(keys, va)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	for _, va := range keys {
		if n := found[va]; n > bestN {
			best, bestN = va, n
		}
	}
	if best == 0 {
		return fmt.Errorf("xbe: no global feeds the mesh format accessor at any of its %d sites", len(rm.accessorSites))
	}
	rm.Accessor.Global = best
	return nil
}

// Runs every shader lookup on each known vertex format, collecting the fixed function declarations and shader bindings they create
func (rm *RenderModel) deriveFixedFunction(d *Disassembly, ms *MaterialSystem) error {
	known := map[uint32]bool{}
	for _, f := range rm.Formats {
		known[f] = true
	}
	for _, l := range rm.lookups {
		// Running the lookup on a symbolic format exposes every format its tables compare against.
		probe := NewExecutor(d)
		ms.hideFrom(probe)
		probePaths, err := probe.Run(l.va, func(s *execState) { l.format.seed(s, Load{Base: "fmt", Off: 0, Size: 4}, l.inputs) })
		if err != nil {
			return fmt.Errorf("shader lookup %08X probe: %w", l.va, err)
		}
		for _, p := range probePaths {
			for _, g := range p.Guards {
				collectFormatConstants(g, known)
			}
		}
	}
	rm.Formats = rm.Formats[:0]
	for f := range known {
		rm.Formats = append(rm.Formats, f)
	}
	sort.Slice(rm.Formats, func(i, j int) bool { return rm.Formats[i] < rm.Formats[j] })
	for _, l := range rm.lookups {
		for _, f := range rm.Formats {
			ex := NewExecutor(d)
			ms.hideFrom(ex)
			paths, err := ex.Run(l.va, func(s *execState) { l.format.seed(s, IConst{f}, l.inputs) })
			if err != nil {
				return fmt.Errorf("shader lookup %08X for format %08X: %w", l.va, f, err)
			}
			for _, p := range paths {
				for _, e := range p.Effects {
					if e.Kind != "call" || e.Fn != "D3DDevice_CreateVertexShader" || len(e.Args) < 2 {
						continue
					}
					declVA, ok := constAddr(e.Args[0])
					if !ok {
						return fmt.Errorf("shader lookup %08X for format %08X passes a symbolic declaration %s", l.va, f, ExprString(e.Args[0]))
					}
					progVA, ok := constAddr(e.Args[1])
					if !ok {
						return fmt.Errorf("shader lookup %08X for format %08X passes a symbolic program %s", l.va, f, ExprString(e.Args[1]))
					}
					if err := rm.addDeclaration(d, declVA); err != nil {
						return err
					}
					if progVA == 0 {
						rm.addFixedFunction(FFDeclaration{Format: f, Declaration: declVA})
						continue
					}
					if err := rm.addProgram(d, progVA); err != nil {
						return err
					}
					rm.addShaderBinding(ShaderBinding{Format: f, Declaration: declVA, Program: progVA})
				}
			}
		}
	}
	return nil
}

func (rm *RenderModel) addFixedFunction(ff FFDeclaration) {
	for _, x := range rm.FixedFunction {
		if x == ff {
			return
		}
	}
	rm.FixedFunction = append(rm.FixedFunction, ff)
}
