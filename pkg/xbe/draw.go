package xbe

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"

	"golang.org/x/arch/x86/x86asm"
)

// NodeType is a scene node class the executable registers with its fields and virtual methods.
type NodeType struct {
	Name   string              `json:"name"`
	Desc   uint32              `json:"desc"`
	Parent string              `json:"parent"`
	Size   uint32              `json:"size"`
	Ctor   uint32              `json:"ctor"`
	Vtable uint32              `json:"vtable"`
	Fields map[int32]NodeField `json:"fields"`
	Slots  []uint32            `json:"slots"`
	Init   map[int32]NodeInit  `json:"init"`
	// Construct is every path of the constructor, run on each new object of the class
	Construct []PathJSON `json:"construct"`
	// InitError says why the constructor could not be run for its defaults.
	InitError string `json:"init_error,omitempty"`
	// OwnTexture marks a class whose load creates a device texture that its own routines fill
	OwnTexture bool `json:"own_texture,omitempty"`
	ctorPaths  []Path
	factory    bool // the constructor allocates its own object
}

// NodeField is one scene file field of a node class at its object offset.
type NodeField struct {
	Name string `json:"name"`
	Type uint32 `json:"type"`
}

// NodeInit is a constant a node constructor stores into a field.
type NodeInit struct {
	Size   int     `json:"size"`
	Value  uint32  `json:"value"`
	Double float64 `json:"double,omitempty"`
}

// SphereBuffer is the vertex layout the sphere node builds its mesh buffer with.
type SphereBuffer struct {
	Format uint32 `json:"format"`
	Stride uint32 `json:"stride"`
}

// DrawModel is the derived draw path from a shape node down to the indexed draw call.
type DrawModel struct {
	Slot           int32                 `json:"slot"`
	TextureSlot    int32                 `json:"texture_slot"`
	FieldSetSlot   int32                 `json:"field_set_slot"`
	KindCheck      uint32                `json:"kind_check"`
	MeshLoaded     uint32                `json:"mesh_loaded"`
	ShaderLookup   uint32                `json:"shader_lookup"`
	MaterialLookup uint32                `json:"material_lookup"`
	BufferVtable   uint32                `json:"buffer_vtable"`
	BufferDraw     uint32                `json:"buffer_draw"`
	BufferFields   map[string]int32      `json:"buffer_fields"`
	TextureField   int32                 `json:"texture_field"`
	IndexData      uint32                `json:"index_data"`
	LoadSlot       int32                 `json:"load_slot"`
	Sphere         SphereBuffer          `json:"sphere"`
	Functions      map[uint32][]PathJSON `json:"functions"`
	Strings        map[uint32]string     `json:"strings"`
	paths          map[uint32][]Path
}

// siteArgs are the constant arguments one call site passes, by stack slot and by register.
type siteArgs struct {
	stack []uint32
	known []bool
	regs  map[x86asm.Reg]uint32
}

// Evaluates the immediates a call site passes on the stack and in registers, tracking copies and zeroing within the function
func (d *Disassembly) siteArguments(fn *Function, i int) siteArgs {
	return d.siteArgumentsFrom(fn, i, nil)
}

// Evaluates a call site's arguments with the addresses some registers hold when the function starts
func (d *Disassembly) siteArgumentsFrom(fn *Function, i int, seed map[x86asm.Reg]uint32) siteArgs {
	ins := fn.Instructions
	regs := map[x86asm.Reg]uint32{}
	for r, a := range seed {
		regs[wideRegister(r)] = a
	}
	var pushes []uint32
	var known []bool
	for j := 0; j < i; j++ {
		in := &ins[j]
		switch in.Inst.Op {
		case x86asm.CALL, x86asm.RET, x86asm.JMP:
			pushes, known = nil, nil
			for _, r := range d.writtenBy(in) {
				delete(regs, r)
			}
			continue
		case x86asm.MOV:
			if dst, ok := in.Inst.Args[0].(x86asm.Reg); ok {
				if imm, ok := in.Inst.Args[1].(x86asm.Imm); ok {
					regs[wideRegister(dst)] = uint32(imm)
				} else if src, ok := in.Inst.Args[1].(x86asm.Reg); ok {
					if v, ok := regs[wideRegister(src)]; ok {
						regs[wideRegister(dst)] = v
					} else {
						delete(regs, wideRegister(dst))
					}
				} else {
					delete(regs, wideRegister(dst))
				}
			}
			continue
		case x86asm.XOR, x86asm.SUB:
			if a, ok := in.Inst.Args[0].(x86asm.Reg); ok {
				if b, ok := in.Inst.Args[1].(x86asm.Reg); ok && a == b {
					regs[wideRegister(a)] = 0
					continue
				}
			}
		case x86asm.PUSH:
			if imm, ok := in.Inst.Args[0].(x86asm.Imm); ok {
				pushes, known = append(pushes, uint32(imm)), append(known, true)
			} else if r, ok := in.Inst.Args[0].(x86asm.Reg); ok {
				v, ok := regs[wideRegister(r)]
				pushes, known = append(pushes, v), append(known, ok)
			} else {
				pushes, known = append(pushes, 0), append(known, false)
			}
			continue
		}
		for _, r := range d.writtenBy(in) {
			delete(regs, r)
		}
	}
	out := siteArgs{regs: regs}
	for k := len(pushes) - 1; k >= 0; k-- {
		out.stack = append(out.stack, pushes[k])
		out.known = append(out.known, known[k])
	}
	return out
}

// Finds the registrar as the function most often called with a class name, and how its call sites lay out the
// name, size, constructor, parent descriptor, field table and descriptor arguments
func (d *Disassembly) findRegistrar() (uint32, map[uint32][]registration, error) {
	type site struct {
		fn   *Function
		args siteArgs
	}
	sites := map[uint32][]site{}
	for _, va := range d.functionVAs() {
		fn := d.Functions[va]
		ins := fn.Instructions
		for i := range ins {
			if ins[i].Inst.Op != x86asm.CALL {
				continue
			}
			t := resolveTarget(&ins[i])
			if t == 0 || d.Functions[t] == nil {
				continue
			}
			args := d.siteArguments(fn, i)
			named, constructed := false, false
			classify := func(v uint32) {
				if d.namesObject(v) {
					named = true
				}
				if c := d.Functions[v]; c != nil && c.Symbol == nil {
					constructed = true
				}
			}
			for k, v := range args.stack {
				if args.known[k] {
					classify(v)
				}
			}
			for _, v := range args.regs {
				classify(v)
			}
			if named && constructed {
				sites[t] = append(sites[t], site{fn, args})
			}
		}
	}
	best, bestN := uint32(0), 0
	for _, t := range slices.Sorted(maps.Keys(sites)) {
		if n := len(sites[t]); n > bestN {
			best, bestN = t, n
		}
	}
	if bestN < 8 {
		return 0, nil, fmt.Errorf("xbe: no node type registrar found (best candidate registers %d types)", bestN)
	}
	// The argument slots every site fills: stack slots the callee cleans and registers holding immediates at all sites.
	stackArgs := calleeCleanupOf(d.Functions[best]) / 4
	var slots []argLocation
	for k := 0; k < stackArgs; k++ {
		slots = append(slots, argLocation{Stack: k})
	}
	for _, r := range d.liveInRegisters(d.Functions[best]) {
		slots = append(slots, argLocation{Reg: r})
	}
	value := func(st site, loc argLocation) (uint32, bool) {
		if loc.Reg != 0 {
			v, ok := st.args.regs[loc.Reg]
			return v, ok
		}
		if loc.Stack < len(st.args.stack) {
			return st.args.stack[loc.Stack], st.args.known[loc.Stack]
		}
		return 0, false
	}
	// Each slot is classified by what nearly every site passes in it, a stray value never settling the role.
	type slotStats struct {
		names, ctors, numbers, data, zero, known int
		distinct                                 map[uint32]bool
	}
	stats := map[argLocation]*slotStats{}
	for _, loc := range slots {
		st := &slotStats{distinct: map[uint32]bool{}}
		for _, site := range sites[best] {
			v, ok := value(site, loc)
			if !ok {
				continue
			}
			st.known++
			st.distinct[v] = true
			switch {
			case v == 0:
				st.zero++
			case d.sectionOf(v) == nil:
				st.numbers++
			case d.namesObject(v):
				st.names++
			case d.Functions[v] != nil && d.Functions[v].Symbol == nil:
				st.ctors++
			case d.isDataAddress(v):
				st.data++
			}
		}
		stats[loc] = st
	}
	mostly := func(n, known int) bool { return known > 0 && n*10 >= known*9 }
	roles := map[argLocation]string{}
	var pointers []argLocation
	for _, loc := range slots {
		st := stats[loc]
		switch {
		case mostly(st.names, st.known):
			roles[loc] = "name"
		case mostly(st.ctors+st.zero, st.known):
			roles[loc] = "ctor"
		case mostly(st.numbers, st.known):
			roles[loc] = "size"
		case mostly(st.data+st.zero, st.known):
			pointers = append(pointers, loc)
		}
	}
	// The parent slot names descriptors other sites pass in the descriptor slot, the field table slot names neither
	var links [][2]argLocation
	for _, p := range pointers {
		for _, q := range pointers {
			if p == q {
				continue
			}
			linked := true
			for v := range stats[p].distinct {
				if v != 0 && !stats[q].distinct[v] {
					linked = false
				}
			}
			if linked {
				links = append(links, [2]argLocation{p, q})
			}
		}
	}
	if len(links) != 1 {
		return 0, nil, fmt.Errorf("xbe: registrar %08X call sites link descriptors through %d pointer slot pairs", best, len(links))
	}
	roles[links[0][0]], roles[links[0][1]] = "parent", "desc"
	for _, loc := range pointers {
		if _, done := roles[loc]; !done {
			roles[loc] = "table"
		}
	}
	byRole := map[string]argLocation{}
	for loc, role := range roles {
		if prev, dup := byRole[role]; dup {
			return 0, nil, fmt.Errorf("xbe: registrar %08X takes its %s in both %s and %s", best, role, prev, loc)
		}
		byRole[role] = loc
	}
	for _, role := range []string{"name", "ctor", "size", "desc"} {
		if _, ok := byRole[role]; !ok {
			return 0, nil, fmt.Errorf("xbe: registrar %08X call sites reveal no %s argument (%v)", best, role, roles)
		}
	}
	out := map[uint32][]registration{}
	for _, st := range sites[best] {
		get := func(role string) uint32 {
			loc, ok := byRole[role]
			if !ok {
				return 0
			}
			v, _ := value(st, loc)
			return v
		}
		name := d.Image.ReadUTF16(get("name"))
		if name == "" || !isPrintable(name) || d.Functions[get("ctor")] == nil {
			continue
		}
		out[best] = append(out[best], registration{name: name, size: get("size"), ctor: get("ctor"), parent: get("parent"), table: get("table"), desc: get("desc")})
	}
	return best, out, nil
}

// registration is one node class the executable registers with its layout tables.
type registration struct {
	name                            string
	size, ctor, parent, table, desc uint32
}

// Follows an allocation thunk to the constructor it tail jumps into, a jump within the function being a branch
func (d *Disassembly) realCtor(va uint32) uint32 {
	fn := d.Functions[va]
	for _, in := range fn.Instructions {
		if in.Inst.Op != x86asm.JMP {
			continue
		}
		t := resolveTarget(&in)
		if t == 0 || t == va || d.InsnByVA[t] == nil || (t > va && t < va+uint32(fn.Size)) {
			continue
		}
		if d.Functions[t] == nil {
			if fn := d.buildFunction(t, map[uint32]bool{}); fn != nil {
				d.Functions[t] = fn
			}
		}
		if d.Functions[t] != nil {
			return t
		}
	}
	return va
}

// Reads the vtable entries that point at code, stopping where the next constructed vtable begins
func (d *Disassembly) vtableSlots(vt uint32) []uint32 {
	end := uint32(0)
	for _, info := range d.constructorInfo() {
		if info.vtable > vt && (end == 0 || info.vtable < end) {
			end = info.vtable
		}
	}
	var out []uint32
	for s := uint32(0); s < 64; s++ {
		addr := vt + 4*s
		if end != 0 && addr >= end {
			break
		}
		v, ok := d.Image.ReadU32(addr)
		if !ok || d.InsnByVA[v] == nil {
			break
		}
		out = append(out, v)
	}
	return out
}

// DeriveNodeTypes reads every registered scene node class with its field table and vtable and names the registrar
func DeriveNodeTypes(d *Disassembly) (map[string]*NodeType, uint32, error) {
	registrar, sites, err := d.findRegistrar()
	if err != nil {
		return nil, 0, err
	}
	byDesc := map[uint32]string{}
	types := map[string]*NodeType{}
	tables := map[string]uint32{}
	for _, r := range sites[registrar] {
		nt := &NodeType{Name: r.name, Desc: r.desc, Size: r.size, Ctor: d.realCtor(r.ctor), Fields: map[int32]NodeField{}, Init: map[int32]NodeInit{}}
		byDesc[r.desc] = r.name
		if r.table != 0 {
			tables[r.name] = r.table
		}
		if fn := d.Functions[nt.Ctor]; fn != nil {
			nt.Vtable, _ = ctorShape(d, fn)
			if nt.Vtable == 0 {
				// A factory allocates the object itself and installs the vtable on the allocation.
				for _, c := range d.constructions(fn) {
					if c.vtable == 0 {
						continue
					}
					if nt.Vtable != 0 && nt.Vtable != c.vtable {
						return nil, 0, fmt.Errorf("xbe: node type %s factory %08X installs vtables %08X and %08X", r.name, nt.Ctor, nt.Vtable, c.vtable)
					}
					nt.Vtable = c.vtable
					nt.factory = true
				}
			}
		}
		if nt.Vtable != 0 {
			nt.Slots = d.vtableSlots(nt.Vtable)
		}
		types[r.name] = nt
	}
	layout, err := d.fieldTableLayout(types, tables)
	if err != nil {
		return nil, 0, err
	}
	for name, table := range tables {
		if err := d.readFieldTable(types[name], table, layout); err != nil {
			return nil, 0, err
		}
	}
	for _, r := range sites[registrar] {
		if r.parent != 0 {
			parent, ok := byDesc[r.parent]
			if !ok {
				return nil, 0, fmt.Errorf("xbe: node type %s has unregistered parent descriptor %08X", r.name, r.parent)
			}
			types[r.name].Parent = parent
		}
	}
	// Inherit vtables and fields down the class chain where a constructor installs none of its own.
	for _, nt := range types {
		for p := nt; p != nil && nt.Vtable == 0; p = types[p.Parent] {
			if p.Vtable != 0 {
				nt.Vtable, nt.Slots = p.Vtable, p.Slots
			}
			if p.Parent == "" {
				break
			}
		}
	}
	return types, registrar, nil
}

// Runs a constructor on a symbolic object and keeps its paths and the constants it stores into fields, a factory
// receiving that object from its allocator
func (d *Disassembly) ctorInits(ex *Executor, nt *NodeType, allocators []uint32) (map[int32]NodeInit, []Path, error) {
	ctor := nt.Ctor
	sub := NewExecutorShared(ex)
	if nt.factory {
		sub.Allocators = map[uint32]bool{}
		for _, va := range allocators {
			sub.Opaque[va] = true
			sub.Allocators[va] = true
		}
		allocated := 0
		sub.allocHook = func() Expr {
			allocated++
			if allocated == 1 {
				return Ptr{"this", 0}
			}
			return Ptr{fmt.Sprintf("new%d", allocated), 0}
		}
	}
	var paths []Path
	// A helper the executor cannot follow, such as a string assignment, stays an opaque call.
	for attempt := 0; ; attempt++ {
		var err error
		paths, err = sub.Run(ctor, func(s *execState) {
			if !nt.factory {
				s.regs[1] = Ptr{"this", 0}
			}
			sub.seedArgs(s, ctor, "ctor.arg", 3)
		})
		if err == nil {
			break
		}
		ee, ok := err.(*ExecError)
		if !ok || len(ee.Frames) == 0 || attempt >= 8 {
			return nil, nil, err
		}
		sub.Opaque[ee.Frames[len(ee.Frames)-1]] = true
	}
	inits := map[int32]NodeInit{}
	for _, p := range paths {
		for _, e := range p.Effects {
			if e.Kind == "copy" && e.Base == "this" {
				return nil, nil, fmt.Errorf("constructor %08X copies an unknown length into the object at +%X", ctor, e.Off)
			}
			if e.Kind != "store" || e.Base != "this" {
				continue
			}
			c, ok := isConst(e.Value)
			if !ok {
				if f, isF := e.Value.(FConst); isF && e.Size == 4 {
					c, ok = float32bits(f.V), true
				} else if isF && e.Size == 8 {
					inits[e.Off] = NodeInit{Size: 8, Double: f.V}
					continue
				}
			}
			if !ok {
				delete(inits, e.Off)
				continue
			}
			inits[e.Off] = NodeInit{Size: e.Size, Value: c}
		}
	}
	return inits, paths, nil
}

// Finds the vtable slot of the type's method that calls the given helper directly
func (d *Disassembly) slotCalling(nt *NodeType, callee uint32) (int32, bool) {
	for i, va := range nt.Slots {
		fn := d.Functions[va]
		if fn == nil {
			continue
		}
		for _, t := range fn.CallTargets {
			if t == callee {
				return int32(4 * i), true
			}
		}
	}
	return 0, false
}

// Direct non library callees of a function
func (d *Disassembly) plainCallees(va uint32) []uint32 {
	var out []uint32
	seen := map[uint32]bool{}
	for _, t := range d.Functions[va].CallTargets {
		c := d.Functions[t]
		if c == nil || c.Symbol != nil || seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// Reports whether a function reaches a library symbol within two direct calls
func (d *Disassembly) reaches(va uint32, symbol string, depth int) bool {
	fn := d.Functions[va]
	if fn == nil {
		return false
	}
	for _, t := range fn.CallTargets {
		c := d.Functions[t]
		if c == nil {
			continue
		}
		if c.Symbol != nil && c.Symbol.Name == symbol {
			return true
		}
		if depth > 0 && c.Symbol == nil && d.reaches(t, symbol, depth-1) {
			return true
		}
	}
	return false
}

// DeriveDraw summarizes the virtual draw chain from a shape node through the appearance and
// material nodes down to the mesh buffer draw, naming every helper the chain relies on.
func (rm *RenderModel) deriveDraw(d *Disassembly, ms *MaterialSystem, types map[string]*NodeType) error {
	shape, appearance, mesh, matnode := types["Shape"], types["Appearance"], types["Mesh"], types["MaxMaterial"]
	for name, nt := range map[string]*NodeType{"Shape": shape, "Appearance": appearance, "Mesh": mesh, "MaxMaterial": matnode} {
		if nt == nil || nt.Vtable == 0 {
			return fmt.Errorf("xbe: node type %s is not registered with a vtable", name)
		}
	}
	dm := &DrawModel{Functions: map[uint32][]PathJSON{}, Strings: map[uint32]string{}, paths: map[uint32][]Path{}}
	// The draw slot is the one whose Shape method records the mesh being drawn in the current mesh global.
	slot, err := d.shapeDrawSlot(shape, rm.Accessor.Global)
	if err != nil {
		return err
	}
	if rm.Accessor.Function != 0 {
		if viaAccessor, ok := d.slotCalling(shape, rm.Accessor.Function); !ok || viaAccessor != slot {
			return fmt.Errorf("xbe: the Shape stores the current mesh in slot %d but calls the format accessor in slot %d", slot/4, viaAccessor/4)
		}
	}
	dm.Slot = slot
	shapeDraw := shape.Slots[slot/4]
	// The draw chain's shader lookup is the helper whose result the shape draw hands to the vertex shader setter.
	if lookup, ok := d.resultFeeding(shapeDraw, "D3DDevice_SetVertexShader"); ok {
		if rm.shaderLookup != 0 && rm.shaderLookup != lookup {
			return fmt.Errorf("xbe: the shape draw feeds the vertex shader from %08X but the format lookup is %08X", lookup, rm.shaderLookup)
		}
		rm.shaderLookup = lookup
	}
	dm.ShaderLookup = rm.shaderLookup
	descs := map[uint32]bool{}
	for _, nt := range types {
		descs[nt.Desc] = true
	}
	// The kind check takes a type descriptor, the loaded check is shared with the mesh draw.
	meshDraw := mesh.Slots[slot/4]
	meshCallees := map[uint32]bool{}
	for _, t := range d.plainCallees(meshDraw) {
		meshCallees[t] = true
	}
	for _, t := range d.plainCallees(shapeDraw) {
		switch {
		case (t == rm.Accessor.Function || t == rm.shaderLookup) && t != 0:
		case meshCallees[t]:
			dm.MeshLoaded = t
		case takesDescriptor(d, shapeDraw, t, descs):
			dm.KindCheck = t
		}
	}
	// With the kind check inlined, the shape draw compares a registered descriptor itself.
	if dm.KindCheck == 0 && !comparesDescriptor(d, shapeDraw, descs) {
		return fmt.Errorf("xbe: shape draw %08X neither calls a kind check nor compares a type descriptor", shapeDraw)
	}
	if dm.MeshLoaded == 0 {
		return fmt.Errorf("xbe: shape draw %08X does not call a mesh loaded check", shapeDraw)
	}
	matDraw := matnode.Slots[slot/4]
	for _, t := range d.plainCallees(matDraw) {
		if t != rm.Wrapper {
			if dm.MaterialLookup != 0 {
				return fmt.Errorf("xbe: material node draw %08X calls several helpers (%08X, %08X)", matDraw, dm.MaterialLookup, t)
			}
			dm.MaterialLookup = t
		}
	}
	if dm.MaterialLookup == 0 {
		// With the lookup inlined, the material node draw searches the registry the factory filled.
		if ms.Registry == nil || !searchesRegistry(d, matDraw, ms.Registry.Base) {
			return fmt.Errorf("xbe: material node draw %08X never looks its material up", matDraw)
		}
	}
	// The buffer object is constructed with the vtable whose first method issues the indexed draw.
	for _, vt := range d.findConstructors() {
		slots := d.vtableSlots(vt)
		if len(slots) <= int(rm.Accessor.Slot/4) {
			continue
		}
		if d.reaches(slots[0], "D3DDevice_DrawIndexedVertices", 1) {
			if dm.BufferVtable != 0 && dm.BufferVtable != vt {
				return fmt.Errorf("xbe: two buffer vtables issue indexed draws (%08X, %08X)", dm.BufferVtable, vt)
			}
			dm.BufferVtable = vt
		}
	}
	if dm.BufferVtable == 0 {
		return fmt.Errorf("xbe: no constructed object draws indexed vertices")
	}
	bufferDraw := d.vtableSlots(dm.BufferVtable)[0]
	dm.BufferDraw = bufferDraw

	ex := NewExecutor(d)
	// The loaded check's own callee reads the mesh file, which the runtime does itself.
	opaque := []uint32{dm.KindCheck, dm.MeshLoaded, rm.Accessor.Function, rm.shaderLookup, dm.MaterialLookup}
	opaque = append(opaque, d.plainCallees(dm.MeshLoaded)...)
	opaque = append(opaque, ms.Allocators...)
	opaque = append(opaque, ms.TextureProducers...)
	summarize := func(va uint32) error {
		if _, done := dm.paths[va]; done {
			return nil
		}
		sub := NewExecutorShared(ex)
		for _, o := range opaque {
			sub.Opaque[o] = true
		}
		paths, _, _, err := sub.RunWithRetries(va, func(s *execState) {
			s.regs[1] = Ptr{"this", 0}
			sub.seedArgs(s, va, "arg", 2)
		}, 8)
		if err != nil {
			return fmt.Errorf("draw path %08X: %w", va, err)
		}
		dm.paths[va] = paths
		dm.Functions[va] = pathsJSON(rm.Exprs, paths)
		return nil
	}
	// The texture slot is the virtual call whose result the appearance hands to SetTexture.
	appearanceDraw := appearance.Slots[slot/4]
	if err := summarize(appearanceDraw); err != nil {
		return err
	}
	// An unidentified library routine the appearance hands a virtual getter's result to is the texture setter.
	if setter, ok := usageOf(dm.paths[appearanceDraw], d, "D3DDevice_SetTexture"); ok {
		d.nameByUsage(setter, "D3DDevice_SetTexture")
		delete(dm.paths, appearanceDraw)
		if err := summarize(appearanceDraw); err != nil {
			return err
		}
	}
	for _, p := range dm.paths[appearanceDraw] {
		for _, e := range p.Effects {
			if e.Kind != "call" || e.Fn != "D3DDevice_SetTexture" || len(e.Args) != 2 {
				continue
			}
			if cr, ok := e.Args[1].(CallResult); ok && strings.HasPrefix(cr.Fn, "vcall@") {
				fmt.Sscanf(cr.Fn, "vcall@%X", &dm.TextureSlot)
			}
		}
	}
	if dm.TextureSlot == 0 {
		return fmt.Errorf("xbe: appearance draw %08X binds no texture from a virtual getter", appearanceDraw)
	}
	// Only the classes a shape draw can reach are summarized, the base class standing in for the rest.
	chain := []string{"Node", "Shape", "Appearance", "Material", "MaxMaterial", "Mesh", "Sphere", "Texture", "ImageTexture"}
	for _, name := range chain {
		nt := types[name]
		if nt == nil {
			return fmt.Errorf("xbe: node type %s is not registered", name)
		}
		if nt.Vtable == 0 {
			continue
		}
		for _, s := range []int32{slot, dm.TextureSlot} {
			if int(s/4) < len(nt.Slots) {
				if err := summarize(nt.Slots[s/4]); err != nil {
					return err
				}
			}
		}
	}
	// Constructor defaults stand in for fields the scene files omit, the draw chain's being required.
	required := map[string]bool{"Transform": true}
	for _, name := range chain {
		required[name] = true
	}
	for _, name := range slices.Sorted(maps.Keys(types)) {
		nt := types[name]
		if rootType(types, nt) != "Node" {
			continue
		}
		inits, paths, err := d.ctorInits(ex, nt, append([]uint32{ms.AllocatorVA}, ms.Allocators...))
		if err != nil {
			if required[name] {
				return fmt.Errorf("xbe: node type %s constructor: %w", name, err)
			}
			nt.InitError = err.Error()
			continue
		}
		nt.Init = inits
		nt.Construct = pathsJSON(rm.Exprs, paths)
		nt.ctorPaths = paths
	}
	// A scripted field write calls the class hook the material node overrides to drop its cached material
	fieldSlot, err := fieldSetSlot(summarize, dm, matnode, types[matnode.Parent])
	if err != nil {
		return err
	}
	dm.FieldSetSlot = fieldSlot
	for _, name := range chain {
		nt := types[name]
		if nt.Vtable == 0 {
			continue
		}
		if err := summarize(nt.Slots[fieldSlot/4]); err != nil {
			return err
		}
	}
	if err := summarize(bufferDraw); err != nil {
		return err
	}
	if rm.shaderLookup != 0 {
		if err := summarize(rm.shaderLookup); err != nil {
			return err
		}
	}
	// The buffer object's fields are the ones its draw hands to the stream, index and draw calls.
	dm.BufferFields = map[string]int32{}
	thisField := func(e Expr) (int32, bool) {
		l, ok := e.(Load)
		if !ok || l.Base != "this" || l.Addr != nil {
			return 0, false
		}
		return l.Off, true
	}
	for _, p := range dm.paths[bufferDraw] {
		for _, e := range p.Effects {
			if e.Kind != "call" {
				continue
			}
			switch {
			case e.Fn == "D3DDevice_SetStreamSource" && len(e.Args) == 3:
				if off, ok := thisField(e.Args[1]); ok {
					dm.BufferFields["vertices"] = off
				}
				if off, ok := thisField(e.Args[2]); ok {
					dm.BufferFields["stride"] = off
				}
			case e.Fn == "D3DDevice_SetIndices" && len(e.Args) == 2:
				if off, ok := thisField(e.Args[0]); ok {
					dm.BufferFields["indices"] = off
				}
			case e.Fn == "D3DDevice_DrawIndexedVertices" && len(e.Args) == 3:
				if off, ok := thisField(e.Args[0]); ok {
					dm.BufferFields["primitive"] = off
				}
				// The XDK's inlined indexed draw hands the device its own index data global, offset by the start index
				if global, ok := indexDataGlobal(e.Args[2]); ok {
					if dm.IndexData != 0 && dm.IndexData != global {
						return fmt.Errorf("xbe: buffer draw %08X draws through index data globals %08X and %08X", bufferDraw, dm.IndexData, global)
					}
					dm.IndexData = global
				}
				// The count derives from one field besides the primitive type that scales it.
				set := map[string]bool{}
				exprSyms(e.Args[1], set)
				var counts []int32
				for k := range set {
					var off int32
					if n, _ := fmt.Sscanf(k, "this@%X", &off); n == 1 && off != dm.BufferFields["primitive"] {
						counts = append(counts, off)
					}
				}
				if len(counts) != 1 {
					return fmt.Errorf("xbe: buffer draw %08X computes its vertex count from %d fields", bufferDraw, len(counts))
				}
				dm.BufferFields["count"] = counts[0]
			case rm.shaderLookup != 0 && e.Fn == fmt.Sprintf("sub_%08X", rm.shaderLookup) && len(e.Args) == 1:
				if off, ok := thisField(e.Args[0]); ok {
					dm.BufferFields["format"] = off
				}
			case e.Fn == "D3DDevice_SetVertexShader" && len(e.Args) == 1:
				// Without a fixed function lookup the buffer hands its format to the device as the shader itself.
				if off, ok := thisField(e.Args[0]); ok {
					dm.BufferFields["format"] = off
				}
			}
		}
	}
	for _, name := range []string{"vertices", "stride", "indices", "primitive", "count", "format"} {
		if _, ok := dm.BufferFields[name]; !ok {
			return fmt.Errorf("xbe: buffer draw %08X does not reveal the %s field", bufferDraw, name)
		}
	}
	if dm.IndexData == 0 {
		return fmt.Errorf("xbe: buffer draw %08X draws through no index data global", bufferDraw)
	}
	// The load slot is the virtual call on the mesh whose Sphere override stores a buffer format and stride
	if err := summarize(dm.MeshLoaded); err != nil {
		return err
	}
	meshField, ok := fieldStoredTo(dm.paths[shapeDraw], rm.Accessor.Global)
	if !ok {
		return fmt.Errorf("xbe: shape draw %08X stores no field of its own into the current mesh global %08X", shapeDraw, rm.Accessor.Global)
	}
	onMesh := map[int32]bool{}
	for _, p := range dm.paths[shapeDraw] {
		for _, s := range virtualSlotsOn(p, Load{Base: "this", Off: meshField, Size: 4}) {
			onMesh[s] = true
		}
	}
	for _, p := range dm.paths[dm.MeshLoaded] {
		for _, s := range virtualSlotsOn(p, Ptr{"this", 0}) {
			onMesh[s] = true
		}
	}
	sphere := types["Sphere"]
	for _, s := range slices.Sorted(maps.Keys(onMesh)) {
		if int(s/4) >= len(sphere.Slots) {
			continue
		}
		load := sphere.Slots[s/4]
		for _, t := range append([]uint32{load}, d.plainCallees(load)...) {
			format, stride := lastFieldConstants(d, t, dm.BufferFields["format"], dm.BufferFields["stride"])
			if format == 0 || stride == 0 {
				continue
			}
			if dm.LoadSlot != 0 && dm.LoadSlot != s {
				return fmt.Errorf("xbe: Sphere builds its buffer in both slot %d and slot %d", dm.LoadSlot/4, s/4)
			}
			dm.LoadSlot = s
			dm.Sphere = SphereBuffer{Format: format, Stride: stride}
		}
	}
	if dm.LoadSlot == 0 {
		return fmt.Errorf("xbe: no virtual call on the mesh (slots %v) builds the sphere buffer", slices.Sorted(maps.Keys(onMesh)))
	}
	// The image texture getter returns the field its loader fills with the device texture.
	imageGetter := types["ImageTexture"].Slots[dm.TextureSlot/4]
	for _, p := range dm.paths[imageGetter] {
		if l, ok := p.Return.(Load); ok && l.Base == "this" && l.Addr == nil {
			dm.TextureField = l.Off
		}
	}
	if dm.TextureField == 0 {
		return fmt.Errorf("xbe: image texture getter %08X does not return a field", imageGetter)
	}
	for _, paths := range dm.paths {
		for _, p := range paths {
			rm.collectStrings(d, p, dm.Strings)
		}
	}
	for _, m := range ms.Materials {
		for _, p := range m.Paths {
			rm.collectStrings(d, p, dm.Strings)
		}
	}
	rm.Draw = dm
	return nil
}

// The global an index pointer argument loads, offset or not
func indexDataGlobal(e Expr) (uint32, bool) {
	switch v := e.(type) {
	case Load:
		if v.Base == "" && v.Addr == nil && v.Size == 4 {
			return uint32(v.Off), true
		}
	case Bin:
		if v.Op == "add" {
			if g, ok := indexDataGlobal(v.A); ok {
				return g, true
			}
			return indexDataGlobal(v.B)
		}
	}
	return 0, false
}

// Finds the object field whose value the paths store into a global
func fieldStoredTo(paths []Path, global uint32) (int32, bool) {
	for _, p := range paths {
		for _, e := range p.Effects {
			if e.Kind != "store" || e.Base != "" || uint32(e.Off) != global {
				continue
			}
			if l, ok := e.Value.(Load); ok && l.Base == "this" && l.Addr == nil {
				return l.Off, true
			}
		}
	}
	return 0, false
}

// Lists the vtable slots a path calls on an object
func virtualSlotsOn(p Path, object Expr) []int32 {
	var out []int32
	for _, e := range p.Effects {
		if e.Kind != "call" || !strings.HasPrefix(e.Fn, "vcall@") || len(e.Args) == 0 || !sameExpr(e.Args[0], object) {
			continue
		}
		var slot int32
		if n, _ := fmt.Sscanf(e.Fn, "vcall@%X", &slot); n == 1 {
			out = append(out, slot)
		}
	}
	return out
}

// Follows the parent chain to the class a node type descends from
func rootType(types map[string]*NodeType, nt *NodeType) string {
	for nt.Parent != "" {
		parent := types[nt.Parent]
		if parent == nil {
			break
		}
		nt = parent
	}
	return nt.Name
}

// Returns the last immediates a function stores at two register relative offsets
func lastFieldConstants(d *Disassembly, va uint32, formatOff, strideOff int32) (uint32, uint32) {
	var format, stride uint32
	for _, in := range d.Functions[va].Instructions {
		if in.Inst.Op != x86asm.MOV {
			continue
		}
		m, ok := in.Inst.Args[0].(x86asm.Mem)
		if !ok || m.Base == 0 || m.Index != 0 {
			continue
		}
		imm, ok := in.Inst.Args[1].(x86asm.Imm)
		if !ok {
			continue
		}
		switch int32(m.Disp) {
		case formatOff:
			format = uint32(imm)
		case strideOff:
			stride = uint32(imm)
		}
	}
	return format, stride
}

// Reports whether a call site in caller passes a registered type descriptor to callee, on the stack or in a register
func takesDescriptor(d *Disassembly, caller, callee uint32, descs map[uint32]bool) bool {
	fn := d.Functions[caller]
	ins := fn.Instructions
	for i := range ins {
		if ins[i].Inst.Op != x86asm.CALL || resolveTarget(&ins[i]) != callee {
			continue
		}
		args := d.siteArguments(fn, i)
		for k, v := range args.stack {
			if args.known[k] && descs[v] {
				return true
			}
		}
		for _, v := range args.regs {
			if descs[v] {
				return true
			}
		}
	}
	return false
}

// Records every immediate argument of a path that addresses a wide string in the image, empty ones included
func (rm *RenderModel) collectStrings(d *Disassembly, p Path, out map[uint32]string) {
	seen := map[exprKey]bool{}
	calls := func(e Expr) {
		if c, ok := e.(CallResult); ok {
			for _, a := range c.Args {
				rm.collectString(d, a, out)
			}
		}
	}
	for _, g := range p.Guards {
		walkExpr(g, seen, calls)
	}
	for _, e := range p.Effects {
		if e.Value != nil {
			rm.collectString(d, e.Value, out)
			walkExpr(e.Value, seen, calls)
		}
		for _, a := range e.Args {
			rm.collectString(d, a, out)
			walkExpr(a, seen, calls)
		}
	}
	walkExpr(p.Return, seen, calls)
}

// Records a constant that addresses a wide string in the image, an empty one included
func (rm *RenderModel) collectString(d *Disassembly, e Expr, out map[uint32]string) {
	c, ok := isConst(e)
	if !ok {
		return
	}
	if s := d.Image.ReadUTF16(c); s != "" && isPrintable(s) {
		out[c] = s
	} else if w, ok := d.Image.ReadU16(c); ok && w == 0 && d.sectionOf(c) != nil {
		out[c] = ""
	}
}

// Finds the field write hook slot as the override comparing the descriptor word with own field offsets
func fieldSetSlot(summarize func(uint32) error, dm *DrawModel, nt, parent *NodeType) (int32, error) {
	own := map[uint32]bool{}
	for off := range nt.Fields {
		own[uint32(off)] = true
	}
	found := int32(-1)
	var failed []string
	for i, va := range nt.Slots {
		if parent != nil && i < len(parent.Slots) && parent.Slots[i] == va {
			continue
		}
		if err := summarize(va); err != nil {
			failed = append(failed, err.Error())
			continue
		}
		matches := false
		for _, p := range dm.paths[va] {
			for _, g := range p.Guards {
				if comparesArgWord(g, own) {
					matches = true
				}
			}
		}
		if !matches {
			continue
		}
		if found >= 0 {
			return 0, fmt.Errorf("xbe: node type %s compares field offsets in two methods (slots %d and %d)", nt.Name, found/4, i)
		}
		found = int32(i * 4)
	}
	if found < 0 {
		return 0, fmt.Errorf("xbe: no %s method compares a field descriptor against its fields (%s)", nt.Name, strings.Join(failed, "; "))
	}
	return found, nil
}

// Reports whether an expression compares the first word of the first argument with one of the offsets
func comparesArgWord(e Expr, offsets map[uint32]bool) bool {
	switch v := e.(type) {
	case Cond:
		if argWord(v.A) {
			if c, ok := isConst(v.B); ok && offsets[c] {
				return true
			}
		}
		if argWord(v.B) {
			if c, ok := isConst(v.A); ok && offsets[c] {
				return true
			}
		}
		return comparesArgWord(v.A, offsets) || comparesArgWord(v.B, offsets)
	case Not:
		return comparesArgWord(v.A, offsets)
	case Bin:
		return comparesArgWord(v.A, offsets) || comparesArgWord(v.B, offsets)
	case Un:
		return comparesArgWord(v.A, offsets)
	}
	return false
}

// Reports whether an expression loads the word the first argument points at
func argWord(e Expr) bool {
	l, ok := e.(Load)
	if !ok || l.Addr == nil || l.Off != 0 {
		return false
	}
	switch a := l.Addr.(type) {
	case Sym:
		return a.Name == "arg0"
	case Bin:
		s, ok := a.A.(Sym)
		c, isZero := isConst(a.B)
		return ok && s.Name == "arg0" && a.Op == "add" && isZero && c == 0
	}
	return false
}

// Finds the vtable slot whose Shape method stores the mesh it draws into the current mesh global
func (d *Disassembly) shapeDrawSlot(shape *NodeType, meshGlobal uint32) (int32, error) {
	var found []int32
	for i, va := range shape.Slots {
		fn := d.Functions[va]
		if fn == nil {
			continue
		}
		for j := range fn.Instructions {
			if storesAbsolute(&fn.Instructions[j], meshGlobal, 4) {
				found = append(found, int32(4*i))
				break
			}
		}
	}
	if len(found) != 1 {
		return 0, fmt.Errorf("xbe: %d Shape methods store the current mesh global %08X (%v)", len(found), meshGlobal, found)
	}
	return found[0], nil
}

// Reports whether a function compares a value against a registered type descriptor
func comparesDescriptor(d *Disassembly, va uint32, descs map[uint32]bool) bool {
	for _, in := range d.Functions[va].Instructions {
		if in.Inst.Op != x86asm.CMP || argCount(&in) != 2 {
			continue
		}
		if imm, ok := in.Inst.Args[1].(x86asm.Imm); ok && descs[uint32(imm)] {
			return true
		}
	}
	return false
}

// Finds the direct callee of a function whose result is pushed to a named library function right after it returns
func (d *Disassembly) resultFeeding(va uint32, symbol string) (uint32, bool) {
	ins := d.Functions[va].Instructions
	found := uint32(0)
	for i := range ins {
		if ins[i].Inst.Op != x86asm.CALL {
			continue
		}
		callee := d.Functions[resolveTarget(&ins[i])]
		if callee == nil || callee.Symbol == nil || callee.Symbol.Name != symbol {
			continue
		}
		for j := i - 1; j >= 0 && j > i-4; j-- {
			if ins[j].Inst.Op == x86asm.CALL {
				if t := resolveTarget(&ins[j]); t != 0 && d.Functions[t] != nil && d.Functions[t].Symbol == nil {
					found = t
				}
				break
			}
			if ins[j].Inst.Op != x86asm.PUSH {
				break
			}
			if r, ok := ins[j].Inst.Args[0].(x86asm.Reg); !ok || r != x86asm.EAX {
				break
			}
		}
	}
	return found, found != 0
}

// Reports whether a function hands the material registry to a library search routine
func searchesRegistry(d *Disassembly, va, base uint32) bool {
	fn := d.Functions[va]
	for i := range fn.Instructions {
		in := &fn.Instructions[i]
		if in.Inst.Op != x86asm.CALL {
			continue
		}
		callee := d.Functions[resolveTarget(in)]
		if callee == nil || callee.Symbol == nil {
			continue
		}
		args := d.siteArguments(fn, i)
		for k, v := range args.stack {
			if args.known[k] && v == base {
				return true
			}
		}
	}
	return false
}

// Finds the unnamed library routine the paths pass a virtual getter's result to, when the named routine is absent
// Reports whether a call takes the result of a virtual getter as one of its arguments
func bindsVirtualResult(e Effect) bool {
	for _, a := range e.Args {
		if cr, ok := a.(CallResult); ok && strings.HasPrefix(cr.Fn, "vcall@") {
			return true
		}
	}
	return false
}

func usageOf(paths []Path, d *Disassembly, name string) (uint32, bool) {
	found := uint32(0)
	for _, p := range paths {
		for _, e := range p.Effects {
			if e.Kind != "call" {
				continue
			}
			// A call already carrying the name needs no naming when it is the one binding the getter's result
			if e.Fn == name {
				if bindsVirtualResult(e) {
					return 0, false
				}
				continue
			}
			var va uint32
			if !scanSub(e.Fn, &va) || !d.inLibrary(va) {
				continue
			}
			for _, a := range e.Args {
				if cr, ok := a.(CallResult); ok && strings.HasPrefix(cr.Fn, "vcall@") {
					found = va
				}
			}
		}
	}
	return found, found != 0
}

// The vtable slot returning a class's field table and the byte layout of the table's records
type fieldTableLayout struct {
	slot   int32
	header uint32
	stride uint32
	name   uint32
	offset uint32
	typ    uint32
}

// Finds the one vtable slot whose accessor returns the field table every class registered
func (d *Disassembly) fieldTableSlot(types map[string]*NodeType, tables map[string]uint32) (int32, error) {
	slots := 0
	for _, nt := range types {
		slots = max(slots, len(nt.Slots))
	}
	var found []int32
	for s := 0; s < slots; s++ {
		matched, all := 0, true
		for name, table := range tables {
			nt := types[name]
			if nt.Vtable == 0 || s >= len(nt.Slots) {
				continue
			}
			base, accessor := d.methodTableBase(nt.Slots[s])
			if !accessor || base != table {
				all = false
				break
			}
			matched++
		}
		if all && matched > 0 {
			found = append(found, int32(4*s))
		}
	}
	if len(found) != 1 {
		return 0, fmt.Errorf("xbe: %d vtable slots return the registered field tables", len(found))
	}
	return found[0], nil
}

// Reports whether a routine calls through a vtable slot
func callsSlot(fn *Function, slot int32) bool {
	for i := range fn.Instructions {
		in := &fn.Instructions[i]
		if in.Inst.Op != x86asm.CALL {
			continue
		}
		if m, ok := in.Inst.Args[0].(x86asm.Mem); ok && m.Base != 0 && m.Index == 0 && int32(m.Disp) == slot {
			return true
		}
	}
	return false
}

// Reads record header, stride and name offset off the table walkers and picks the offset word no class repeats
func (d *Disassembly) fieldTableLayout(types map[string]*NodeType, tables map[string]uint32) (fieldTableLayout, error) {
	slot, err := d.fieldTableSlot(types, tables)
	if err != nil {
		return fieldTableLayout{}, err
	}
	ex := d.baseExecutor()
	var layout fieldTableLayout
	var walkers []uint32
	for _, va := range d.functionVAs() {
		fn := d.Functions[va]
		if fn.Symbol != nil || d.inLibrary(va) || !callsSlot(fn, slot) {
			continue
		}
		l, ok := d.walkerLayout(ex, va, slot)
		if !ok {
			continue
		}
		if len(walkers) > 0 && l != layout {
			return fieldTableLayout{}, fmt.Errorf("xbe: field table walkers %08X and %08X read records differently (%+v, %+v)", walkers[0], va, layout, l)
		}
		layout, walkers = l, append(walkers, va)
	}
	if len(walkers) == 0 {
		return fieldTableLayout{}, fmt.Errorf("xbe: no routine walks the field tables through vtable slot %d", slot/4)
	}
	words := layout.stride / 4
	var candidates []uint32
	for w := uint32(0); w < words; w++ {
		if w*4 != layout.name {
			candidates = append(candidates, w*4)
		}
	}
	unique := map[uint32]bool{}
	for _, c := range candidates {
		unique[c] = true
	}
	for name, table := range tables {
		seen := map[uint32]map[uint32]bool{}
		for _, c := range candidates {
			seen[c] = map[uint32]bool{}
		}
		for rec := table + layout.header; ; rec += layout.stride {
			np, ok := d.Image.ReadU32(rec + layout.name)
			if !ok {
				return fieldTableLayout{}, fmt.Errorf("xbe: node type %s field table runs past the image at %08X", name, rec)
			}
			if np == 0 {
				break
			}
			for _, c := range candidates {
				v, _ := d.Image.ReadU32(rec + c)
				if seen[c][v] {
					unique[c] = false
				}
				seen[c][v] = true
			}
		}
	}
	var offsets []uint32
	for _, c := range candidates {
		if unique[c] {
			offsets = append(offsets, c)
		}
	}
	if len(offsets) != 1 || len(candidates) != 2 {
		return fieldTableLayout{}, fmt.Errorf("xbe: field records of %d words hold %d words no class repeats, expected one offset word beside the type", words, len(offsets))
	}
	layout.offset = offsets[0]
	for _, c := range candidates {
		if c != layout.offset {
			layout.typ = c
		}
	}
	return layout, nil
}

// Runs a walker on a symbolic object and reads the name offsets compared and the records returned
func (d *Disassembly) walkerLayout(ex *Executor, va uint32, slot int32) (fieldTableLayout, bool) {
	fn := d.Functions[va]
	sub := NewExecutorShared(ex)
	sub.PathLimit, sub.StepLimit = 4096, 20000
	for _, c := range d.plainCallees(va) {
		sub.Opaque[c] = true
	}
	stackArgs := max(d.cleanupOf(fn)/4, d.stackInputs(fn))
	paths, err := sub.Run(va, func(s *execState) {
		s.regs[1] = Ptr{"this", 0}
		sub.seedArgs(s, va, "arg", stackArgs)
	})
	// The walk over every record of every ancestor outruns any path budget, the records found before that suffice
	var limit *ExecError
	if err != nil && (!errors.As(err, &limit) || !limit.Limit) {
		return fieldTableLayout{}, false
	}
	accessor := fmt.Sprintf("vcall@%X", slot)
	names := map[uint32]bool{}
	returns := map[uint32]bool{}
	for _, p := range paths {
		bases := map[string]bool{}
		for _, e := range p.Effects {
			receiver := e.This == Expr(Ptr{"this", 0}) || (len(e.Args) > 0 && e.Args[0] == Expr(Ptr{"this", 0}))
			if e.Kind == "call" && e.Fn == accessor && receiver {
				bases[fmt.Sprintf("res%d", e.Seq)] = true
			}
		}
		// A table is the accessor's result or the word a table starts with, which links its parent
		var isTable func(e Expr) bool
		isTable = func(e Expr) bool {
			switch v := e.(type) {
			case CallResult:
				return bases[fmt.Sprintf("res%d", v.Seq)]
			case Ptr:
				return v.Off == 0 && bases[v.Base]
			case Load:
				if v.Size != 4 {
					return false
				}
				if v.Addr == nil {
					return v.Off == 0 && bases[v.Base]
				}
				return isTable(v.Addr)
			}
			return false
		}
		// The byte offset into a table an expression reads or names
		tableOffset := func(e Expr) (uint32, bool) {
			switch v := e.(type) {
			case Ptr:
				if bases[v.Base] && v.Off > 0 {
					return uint32(v.Off), true
				}
			case Bin:
				if c, ok := isConst(v.B); ok && v.Op == "add" && c > 0 && isTable(v.A) {
					return c, true
				}
			}
			return 0, false
		}
		for _, e := range p.Effects {
			if e.Kind != "call" || e.Fn == accessor {
				continue
			}
			for _, a := range e.Args {
				l, ok := a.(Load)
				if !ok || l.Size != 4 {
					continue
				}
				if l.Addr == nil {
					if l.Off > 0 && bases[l.Base] {
						names[uint32(l.Off)] = true
					}
				} else if off, ok := tableOffset(l.Addr); ok {
					names[off] = true
				}
			}
		}
		if off, ok := tableOffset(asPtr(p.Return)); ok {
			returns[off] = true
		} else if off, ok := tableOffset(p.Return); ok {
			returns[off] = true
		}
	}
	if len(names) < 2 || len(returns) == 0 {
		return fieldTableLayout{}, false
	}
	nameOffs := slices.Sorted(maps.Keys(names))
	stride := nameOffs[1] - nameOffs[0]
	for i := 1; i < len(nameOffs); i++ {
		if nameOffs[i]-nameOffs[i-1] != stride {
			return fieldTableLayout{}, false
		}
	}
	header := slices.Min(slices.Collect(maps.Keys(returns)))
	for off := range returns {
		if (off-header)%stride != 0 {
			return fieldTableLayout{}, false
		}
	}
	if nameOffs[0] < header || nameOffs[0]-header >= stride || stride%4 != 0 {
		return fieldTableLayout{}, false
	}
	return fieldTableLayout{slot: slot, header: header, stride: stride, name: nameOffs[0] - header}, true
}

// Reads a class's fields out of its table, each record naming a field at an object offset with a type
func (d *Disassembly) readFieldTable(nt *NodeType, table uint32, layout fieldTableLayout) error {
	for rec := table + layout.header; ; rec += layout.stride {
		np, ok := d.Image.ReadU32(rec + layout.name)
		if !ok {
			return fmt.Errorf("xbe: node type %s field table runs past the image at %08X", nt.Name, rec)
		}
		if np == 0 {
			return nil
		}
		name := d.Image.ReadUTF16(np)
		if name == "" || !isPrintable(name) {
			return fmt.Errorf("xbe: node type %s field at %08X has no name", nt.Name, rec)
		}
		off, _ := d.Image.ReadU32(rec + layout.offset)
		typ, _ := d.Image.ReadU32(rec + layout.typ)
		nt.Fields[int32(off)] = NodeField{Name: name, Type: typ}
	}
}
