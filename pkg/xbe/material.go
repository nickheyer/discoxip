package xbe

import (
	"fmt"
	"sort"
	"strings"

	"golang.org/x/arch/x86/x86asm"
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
	Guards  []int        `json:"guards"`
	Effects []EffectJSON `json:"effects"`
	Return  int          `json:"return"`
}

// EffectJSON is the serialized form of one store or call.
type EffectJSON struct {
	Kind    string       `json:"kind"`
	VA      uint32       `json:"va"`
	Base    string       `json:"base,omitempty"`
	Off     int32        `json:"off,omitempty"`
	Size    int          `json:"size,omitempty"`
	Value   int          `json:"value,omitempty"`
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
		pj := PathJSON{Return: pool.Add(p.Return), Guards: []int{}, Effects: []EffectJSON{}}
		for _, g := range p.Guards {
			pj.Guards = append(pj.Guards, pool.Add(g))
		}
		for _, e := range p.Effects {
			ej := EffectJSON{Kind: e.Kind, VA: e.VA, Base: e.Base, Off: e.Off, Size: e.Size, Fn: e.Fn, Seq: e.Seq}
			if e.Value != nil {
				ej.Value = pool.Add(e.Value)
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
}

// Finds dashboard functions that create and fill a texture
func (d *Disassembly) textureProducers() []uint32 {
	var out []uint32
	for va, fn := range d.Functions {
		if fn.Symbol != nil {
			continue
		}
		locks := false
		for _, t := range fn.CallTargets {
			if callee, ok := d.Functions[t]; ok && callee.Symbol != nil && callee.Symbol.Name == "D3DTexture_LockRect" {
				locks = true
			}
		}
		if locks {
			out = append(out, va)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// Finds constructors as functions that install a vtable into this+0
func (d *Disassembly) findConstructors() map[uint32]uint32 {
	out := map[uint32]uint32{}
	for va, fn := range d.Functions {
		if fn.Symbol != nil {
			continue
		}
		if vt := ctorVtable(d, fn); vt != 0 {
			out[va] = vt
		}
	}
	return out
}

// Finds allocators as functions whose result becomes this for a constructor at three or more sites
func (d *Disassembly) findAllocators(ctors map[uint32]uint32) []uint32 {
	counts := map[uint32]int{}
	for _, fn := range d.Functions {
		ins := fn.Instructions
		for i := range ins {
			if ins[i].Inst.Op != x86asm.CALL {
				continue
			}
			if _, isCtor := ctors[resolveTarget(&ins[i])]; !isCtor {
				continue
			}
			thisFromEAX := false
			for j := i - 1; j >= 0 && j >= i-12; j-- {
				jn := &ins[j]
				if jn.Inst.Op == x86asm.MOV && argCount(jn) == 2 {
					if dst, ok := jn.Inst.Args[0].(x86asm.Reg); ok && dst == x86asm.ECX {
						if src, ok := jn.Inst.Args[1].(x86asm.Reg); ok && src == x86asm.EAX {
							thisFromEAX = true
						}
					}
				}
				if jn.Inst.Op == x86asm.CALL {
					if t := resolveTarget(jn); t != 0 && thisFromEAX {
						counts[t]++
					}
					break
				}
			}
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

// Finds the factory as the function calling the most constructors with a string argument
func (d *Disassembly) findMaterialFactory(ctors map[uint32]uint32) (uint32, uint32, error) {
	bestVA, bestCount := uint32(0), 0
	var bestAlloc uint32
	for va, fn := range d.Functions {
		ins := fn.Instructions
		count := 0
		allocs := map[uint32]int{}
		for i, in := range ins {
			if in.Inst.Op != x86asm.CALL {
				continue
			}
			t := resolveTarget(&in)
			if _, ok := ctors[t]; !ok {
				continue
			}
			hasName := false
			for j := i - 1; j >= 0 && j >= i-12; j-- {
				if ins[j].Inst.Op == x86asm.CALL {
					if prev := resolveTarget(&ins[j]); prev != 0 {
						allocs[prev]++
					}
					break
				}
				if ins[j].Inst.Op == x86asm.PUSH {
					if imm, ok := ins[j].Inst.Args[0].(x86asm.Imm); ok && len(d.Image.ReadUTF16(uint32(imm))) > 0 {
						hasName = true
					}
				}
			}
			if hasName {
				count++
			}
		}
		if count > bestCount {
			bestVA, bestCount = va, count
			bestAlloc = 0
			bestN := 0
			for a, n := range allocs {
				if n > bestN {
					bestAlloc, bestN = a, n
				}
			}
		}
	}
	if bestCount < 8 {
		return 0, 0, fmt.Errorf("xbe: no material factory found (best candidate constructs %d named objects)", bestCount)
	}
	if bestAlloc == 0 {
		return 0, 0, fmt.Errorf("xbe: material factory %08X has no allocator call before its constructors", bestVA)
	}
	return bestVA, bestAlloc, nil
}

// DeriveMaterials constructs every material symbolically and traces each Apply method.
func DeriveMaterials(d *Disassembly) (*MaterialSystem, error) {
	ctors := d.findConstructors()
	factory, alloc, err := d.findMaterialFactory(ctors)
	if err != nil {
		return nil, err
	}
	ms := &MaterialSystem{FactoryVA: factory, AllocatorVA: alloc, Ctors: ctors}
	allocators := d.findAllocators(ctors)
	ms.Allocators = allocators

	ex := NewExecutor(d)
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
		nameVal, ok := fields[4]
		if !ok {
			return nil, fmt.Errorf("material %s has no name pointer at +4", obj)
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
		for ctorVA, cvt := range ctors {
			if cvt == vtVA {
				m.CtorVA = ctorVA
			}
		}
		ms.Materials = append(ms.Materials, m)
	}
	sort.Slice(ms.Materials, func(i, j int) bool { return ms.Materials[i].Object < ms.Materials[j].Object })

	producers := d.textureProducers()
	ms.TextureProducers = producers
	ex.LearnGlobalObjects(allocators, producers)
	for _, m := range ms.Materials {
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
