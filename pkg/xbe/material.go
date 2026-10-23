package xbe

import (
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"
)

// MaterialObject is one material instance constructed by the dashboard at startup.
type MaterialObject struct {
	Name        string           `json:"name"`
	Object      string           `json:"object"`
	CtorVA      uint32           `json:"ctor_va"`
	VtableVA    uint32           `json:"vtable_va"`
	ApplyVA     uint32           `json:"apply_va"`
	Fields      map[int32]uint32 `json:"fields"`
	Apply       []PathJSON       `json:"apply"`
	Inputs      []string         `json:"inputs"`
	OpaqueCalls []uint32         `json:"opaque_calls,omitempty"`
	Paths       []Path           `json:"-"`
}

// PathJSON is the serialized form of one execution path with expressions as pool indices.
type PathJSON struct {
	Guards   []int        `json:"guards"`
	GuardPos []int        `json:"guard_pos"`
	Effects  []EffectJSON `json:"effects"`
	Return   int          `json:"return"`
}

// EffectJSON is the serialized form of one store or call.
type EffectJSON struct {
	Kind    string       `json:"kind"`
	VA      uint32       `json:"va"`
	Base    string       `json:"base,omitempty"`
	Off     int32        `json:"off,omitempty"`
	Size    int          `json:"size,omitempty"`
	Value   int          `json:"value,omitempty"`
	Addr    int          `json:"addr,omitempty"`
	HasAddr bool         `json:"has_addr,omitempty"`
	Fn      string       `json:"fn,omitempty"`
	Args    []int        `json:"args,omitempty"`
	This    int          `json:"this,omitempty"`
	HasThis bool         `json:"has_this,omitempty"`
	Seq     int          `json:"seq,omitempty"`
	Buffers []BufferJSON `json:"buffers,omitempty"`
}

// BufferJSON is the serialized snapshot of memory behind a pointer argument.
type BufferJSON struct {
	Index int   `json:"index"`
	Elems []int `json:"elems"`
}

func pathsJSON(pool *ExprPool, paths []Path) []PathJSON {
	out := []PathJSON{}
	for _, p := range paths {
		pj := PathJSON{Return: pool.Add(p.Return), Guards: []int{}, GuardPos: []int{}, Effects: []EffectJSON{}}
		for i, g := range p.Guards {
			pj.Guards = append(pj.Guards, pool.Add(g))
			pos := len(p.Effects)
			if i < len(p.GuardPos) {
				pos = p.GuardPos[i]
			}
			pj.GuardPos = append(pj.GuardPos, pos)
		}
		for _, e := range p.Effects {
			ej := EffectJSON{Kind: e.Kind, VA: e.VA, Base: e.Base, Off: e.Off, Size: e.Size, Fn: e.Fn, Seq: e.Seq}
			if e.Value != nil {
				ej.Value = pool.Add(e.Value)
			}
			if e.Addr != nil {
				ej.Addr = pool.Add(e.Addr)
				ej.HasAddr = true
			}
			for _, a := range e.Args {
				ej.Args = append(ej.Args, pool.Add(a))
			}
			if e.This != nil {
				ej.This = pool.Add(e.This)
				ej.HasThis = true
			}
			for _, b := range e.Buffers {
				bj := BufferJSON{Index: b.Index, Elems: []int{}}
				for _, el := range b.Elems {
					bj.Elems = append(bj.Elems, pool.Add(el))
				}
				ej.Buffers = append(ej.Buffers, bj)
			}
			pj.Effects = append(pj.Effects, ej)
		}
		out = append(out, pj)
	}
	return out
}

// MaterialSystem is everything derived about the dashboard material classes.
type MaterialSystem struct {
	FactoryVA        uint32
	AllocatorVA      uint32
	Ctors            map[uint32]uint32 // ctor VA to vtable VA
	Materials        []*MaterialObject
	TextureProducers []uint32 // dashboard functions that build textures at runtime
	Allocators       []uint32 // functions whose results become constructed objects
	Registry         *MaterialRegistry
}

// MaterialRegistry is the global array of material pointers the factory fills and the count beside it.
type MaterialRegistry struct {
	Base  uint32 `json:"base"`
	Count uint32 `json:"count"`
}

// Finds the array the factory stores each constructed object into, indexed by a counter global
func materialRegistry(paths []Path) *MaterialRegistry {
	counts := map[MaterialRegistry]int{}
	for _, e := range paths[0].Effects {
		if e.Kind != "store" || e.Addr == nil || e.Size != 4 {
			continue
		}
		if p, ok := e.Value.(Ptr); !ok || !strings.HasPrefix(p.Base, "obj") || p.Off != 0 {
			continue
		}
		b, ok := e.Addr.(Bin)
		if !ok || b.Op != "add" {
			continue
		}
		base, isBase := isConst(b.B)
		scaled, isScaled := b.A.(Bin)
		if !isBase {
			base, isBase = isConst(b.A)
			scaled, isScaled = b.B.(Bin)
		}
		if !isBase || !isScaled || scaled.Op != "mul" {
			continue
		}
		counter, ok := counterGlobal(scaled.A)
		if !ok {
			continue
		}
		counts[MaterialRegistry{Base: base, Count: counter}]++
	}
	var best *MaterialRegistry
	bestN := 0
	for r, n := range counts {
		if n > bestN {
			rr := r
			best, bestN = &rr, n
		}
	}
	return best
}

// Finds the global a counter expression counts up from, through any number of increments
func counterGlobal(e Expr) (uint32, bool) {
	for {
		switch v := e.(type) {
		case Load:
			if v.Base != "" || v.Addr != nil {
				return 0, false
			}
			return uint32(v.Off), true
		case Bin:
			if v.Op != "add" {
				return 0, false
			}
			if _, ok := isConst(v.B); ok {
				e = v.A
				continue
			}
			if _, ok := isConst(v.A); ok {
				e = v.B
				continue
			}
			return 0, false
		default:
			return 0, false
		}
	}
}

// Keeps the executor from inlining object allocators and texture producers
func (ms *MaterialSystem) hideFrom(ex *Executor) {
	ex.Opaque[ms.AllocatorVA] = true
	for _, va := range ms.Allocators {
		ex.Opaque[va] = true
	}
	for _, va := range ms.TextureProducers {
		ex.Opaque[va] = true
	}
}

// Finds dashboard functions that create and fill a texture
func (d *Disassembly) textureProducers() []uint32 {
	var out []uint32
	for va, fn := range d.Functions {
		if fn.Symbol != nil || d.inLibrary(va) {
			continue
		}
		creates := d.reaches(va, "D3DXCreateTexture", 1) || d.reaches(va, "D3DDevice_CreateTexture", 1)
		if creates && d.reaches(va, "XGSwizzleRect", 0) {
			out = append(out, va)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// Finds allocators as functions whose results become constructed objects at three or more sites
func (d *Disassembly) findAllocators() []uint32 {
	counts := map[uint32]int{}
	ctors := d.constructorInfo()
	for _, fn := range d.Functions {
		for _, c := range d.constructions(fn) {
			if ctors[c.ctor].library {
				continue
			}
			counts[c.alloc]++
		}
	}
	var out []uint32
	for va, n := range counts {
		if n >= 3 {
			out = append(out, va)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// Finds the factory as the function building the most named objects, with the allocator those objects come from
func (d *Disassembly) findMaterialFactory() (uint32, uint32, error) {
	bestVA, bestCount := uint32(0), 0
	var bestAlloc uint32
	for _, va := range d.functionVAs() {
		count := 0
		allocs := map[uint32]int{}
		for _, c := range d.constructions(d.Functions[va]) {
			if !c.named || d.constructorInfo()[c.ctor].library {
				continue
			}
			count++
			allocs[c.alloc]++
		}
		if count > bestCount {
			bestVA, bestCount = va, count
			bestAlloc = 0
			bestN := 0
			for _, a := range slices.Sorted(maps.Keys(allocs)) {
				if n := allocs[a]; n > bestN {
					bestAlloc, bestN = a, n
				}
			}
		}
	}
	if bestCount < 8 {
		return 0, 0, fmt.Errorf("xbe: no material factory found (best candidate constructs %d named objects)", bestCount)
	}
	return bestVA, bestAlloc, nil
}

// DeriveMaterials constructs every material symbolically and traces each Apply method.
func DeriveMaterials(d *Disassembly) (*MaterialSystem, error) {
	stage("constructors")
	ctors := d.findConstructors()
	stage("factory")
	factory, alloc, err := d.findMaterialFactory()
	if err != nil {
		return nil, err
	}
	ms := &MaterialSystem{FactoryVA: factory, AllocatorVA: alloc, Ctors: ctors}
	stage("allocators")
	allocators := d.findAllocators()
	ms.Allocators = allocators

	stage("executor")
	ex := NewExecutor(d)
	stage("run factory")
	ex.Opaque[alloc] = true
	ex.Allocators[alloc] = true
	for _, va := range allocators {
		ex.Opaque[va] = true
		ex.Allocators[va] = true
	}
	objCount := 0
	var objects []string
	ex.allocHook = func() Expr {
		objCount++
		name := fmt.Sprintf("obj%d", objCount)
		objects = append(objects, name)
		return Ptr{name, 0}
	}
	paths, err := ex.Run(factory, func(s *execState) {})
	if err != nil {
		return nil, fmt.Errorf("material factory: %w", err)
	}
	if len(paths) != 1 {
		return nil, fmt.Errorf("material factory %08X has %d execution paths, expected one", factory, len(paths))
	}
	stage(fmt.Sprintf("factory done with %d objects", len(objects)))
	ms.Registry = materialRegistry(paths)
	// Rebuild object memory from the recorded stores.
	objMem := map[string]map[int32]memVal{}
	for _, e := range paths[0].Effects {
		if e.Kind != "store" || !strings.HasPrefix(e.Base, "obj") {
			continue
		}
		if objMem[e.Base] == nil {
			objMem[e.Base] = map[int32]memVal{}
		}
		objMem[e.Base][e.Off] = memVal{e.Size, e.Value}
	}
	// The name field is the offset at which the most constructed objects hold a wide string pointer.
	nameCounts := map[int32]int{}
	for _, obj := range objects {
		if _, ok := objMem[obj][0]; !ok {
			continue
		}
		for off, mv := range objMem[obj] {
			if c, ok := isConst(mv.v); ok && mv.size == 4 && d.namesObject(c) {
				nameCounts[off]++
			}
		}
	}
	nameOff, nameN := int32(0), 0
	for _, off := range slices.Sorted(maps.Keys(nameCounts)) {
		if nameCounts[off] > nameN {
			nameOff, nameN = off, nameCounts[off]
		}
	}
	if nameN == 0 {
		return nil, fmt.Errorf("material factory %08X constructs no object holding a name", factory)
	}
	for _, obj := range objects {
		fields := objMem[obj]
		vt, ok := fields[0]
		if !ok {
			continue
		}
		vtVA, ok := isConst(vt.v)
		if !ok || !d.isVtable(vtVA) {
			return nil, fmt.Errorf("material %s has no concrete vtable", obj)
		}
		nameVal, ok := fields[nameOff]
		if !ok {
			return nil, fmt.Errorf("material %s has no name pointer at +%X", obj, nameOff)
		}
		nameVA, ok := isConst(nameVal.v)
		if !ok {
			return nil, fmt.Errorf("material %s has a symbolic name pointer", obj)
		}
		applyVA, _ := d.Image.ReadU32(vtVA)
		m := &MaterialObject{Name: d.Image.ReadUTF16(nameVA), Object: obj, VtableVA: vtVA, ApplyVA: applyVA, Fields: map[int32]uint32{}}
		for off, mv := range fields {
			if c, ok := isConst(mv.v); ok {
				for b := 0; b < mv.size; b++ {
					m.Fields[off+int32(b)] = (c >> (8 * b)) & 0xFF
				}
			} else if f, ok := mv.v.(FConst); ok && mv.size == 4 {
				bits := float32bits(f.V)
				for b := 0; b < 4; b++ {
					m.Fields[off+int32(b)] = (bits >> (8 * b)) & 0xFF
				}
			} else {
				return nil, fmt.Errorf("material %s field +%X is symbolic: %s", obj, off, ExprString(mv.v))
			}
		}
		for _, ctorVA := range slices.Sorted(maps.Keys(ctors)) {
			if ctors[ctorVA] == vtVA {
				m.CtorVA = ctorVA
			}
		}
		ms.Materials = append(ms.Materials, m)
	}
	sort.Slice(ms.Materials, func(i, j int) bool { return ms.Materials[i].Object < ms.Materials[j].Object })

	stage("texture producers")
	producers := d.textureProducers()
	ms.TextureProducers = producers
	stage("learn global objects")
	ex.LearnGlobalObjects(allocators, producers)
	for _, m := range ms.Materials {
		stage("material " + m.Name)
		fields := objMem[m.Object]
		ax := NewExecutorShared(ex)
		ax.Opaque[alloc] = true
		for _, va := range allocators {
			ax.Opaque[va] = true
		}
		for _, va := range producers {
			ax.Opaque[va] = true
			ax.Results[va] = Sym{fmt.Sprintf("texture:%08X", va)}
		}
		var paths []Path
		for attempt := 0; ; attempt++ {
			var err error
			paths, err = ax.Run(m.ApplyVA, func(s *execState) {
				s.regs[1] = Ptr{m.Object, 0}
				for off, mv := range fields {
					s.mem[memKey{m.Object, off}] = mv
				}
				s.pushArgs(Ptr{"node", 0})
			})
			if err == nil {
				break
			}
			ee, ok := err.(*ExecError)
			if !ok || !ee.Limit || len(ee.Frames) == 0 || attempt >= 8 {
				return nil, fmt.Errorf("material %q apply %08X: %w", m.Name, m.ApplyVA, err)
			}
			// The innermost inlined callee cannot be expanded, so keep it as an opaque call.
			inner := ee.Frames[len(ee.Frames)-1]
			ax.Opaque[inner] = true
			m.OpaqueCalls = append(m.OpaqueCalls, inner)
		}
		m.Inputs = pathInputs(paths)
		m.Paths = paths
	}
	return ms, nil
}

func float32bits(v float64) uint32 {
	return mathFloat32bits(float32(v))
}
