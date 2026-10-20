package xbe

import (
	"fmt"
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
	KindCheck      uint32                `json:"kind_check"`
	MeshLoaded     uint32                `json:"mesh_loaded"`
	ShaderLookup   uint32                `json:"shader_lookup"`
	MaterialLookup uint32                `json:"material_lookup"`
	BufferVtable   uint32                `json:"buffer_vtable"`
	BufferDraw     uint32                `json:"buffer_draw"`
	BufferFields   map[string]int32      `json:"buffer_fields"`
	TextureField   int32                 `json:"texture_field"`
	LoadSlot       int32                 `json:"load_slot"`
	Sphere         SphereBuffer          `json:"sphere"`
	Functions      map[uint32][]PathJSON `json:"functions"`
	Strings        map[uint32]string     `json:"strings"`
	paths          map[uint32][]Path
}

// Finds the registrar as the function called with a name string, a size, a constructor and two tables
func (d *Disassembly) findRegistrar() (uint32, map[uint32][]registration, error) {
	counts := map[uint32]int{}
	sites := map[uint32][]registration{}
	for _, fn := range d.Functions {
		ins := fn.Instructions
		for i := range ins {
			if ins[i].Inst.Op != x86asm.CALL {
				continue
			}
			var pushes []uint32
			var desc uint32
			for j := i - 1; j >= 0 && j > i-9; j-- {
				in := &ins[j]
				if in.Inst.Op == x86asm.PUSH {
					imm, ok := in.Inst.Args[0].(x86asm.Imm)
					if !ok {
						break
					}
					pushes = append(pushes, uint32(imm))
				}
				if in.Inst.Op == x86asm.MOV {
					if r, ok := in.Inst.Args[0].(x86asm.Reg); ok && r == x86asm.ECX {
						if imm, ok := in.Inst.Args[1].(x86asm.Imm); ok {
							desc = uint32(imm)
						}
					}
				}
				if in.Inst.Op == x86asm.CALL {
					break
				}
			}
			if len(pushes) != 5 || desc == 0 {
				continue
			}
			name := d.Image.ReadUTF16(pushes[0])
			if name == "" || !isPrintable(name) || d.Functions[pushes[2]] == nil {
				continue
			}
			t := resolveTarget(&ins[i])
			counts[t]++
			sites[t] = append(sites[t], registration{name: name, size: pushes[1], ctor: pushes[2], parent: pushes[3], table: pushes[4], desc: desc})
		}
	}
	best, bestN := uint32(0), 0
	for t, n := range counts {
		if n > bestN {
			best, bestN = t, n
		}
	}
	if bestN < 8 {
		return 0, nil, fmt.Errorf("xbe: no node type registrar found (best candidate registers %d types)", bestN)
	}
	return best, sites, nil
}

type registration struct {
	name                            string
	size, ctor, parent, table, desc uint32
}

// Follows an allocation thunk to the constructor it tail jumps into
func (d *Disassembly) realCtor(va uint32) uint32 {
	for _, in := range d.Functions[va].Instructions {
		if in.Inst.Op != x86asm.JMP {
			continue
		}
		t := resolveTarget(&in)
		if t == 0 || t == va || d.InsnByVA[t] == nil {
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

// Reads the vtable entries that point at code
func (d *Disassembly) vtableSlots(vt uint32) []uint32 {
	var out []uint32
	for s := uint32(0); s < 64; s++ {
		v, ok := d.Image.ReadU32(vt + 4*s)
		if !ok || d.InsnByVA[v] == nil {
			break
		}
		out = append(out, v)
	}
	return out
}

// DeriveNodeTypes reads every registered scene node class with its field table and vtable.
func DeriveNodeTypes(d *Disassembly) (map[string]*NodeType, error) {
	registrar, sites, err := d.findRegistrar()
	if err != nil {
		return nil, err
	}
	byDesc := map[uint32]string{}
	types := map[string]*NodeType{}
	for _, r := range sites[registrar] {
		nt := &NodeType{Name: r.name, Desc: r.desc, Size: r.size, Ctor: d.realCtor(r.ctor), Fields: map[int32]NodeField{}, Init: map[int32]NodeInit{}}
		byDesc[r.desc] = r.name
		if r.table != 0 {
			for i := uint32(0); i < 64; i++ {
				off, _ := d.Image.ReadU32(r.table + 12 + 12*i)
				typ, _ := d.Image.ReadU32(r.table + 16 + 12*i)
				np, _ := d.Image.ReadU32(r.table + 20 + 12*i)
				if np == 0 {
					break
				}
				name := d.Image.ReadUTF16(np)
				if name == "" || !isPrintable(name) {
					return nil, fmt.Errorf("xbe: node type %s field %d has no name", r.name, i)
				}
				nt.Fields[int32(off)] = NodeField{Name: name, Type: typ}
			}
		}
		if fn := d.Functions[nt.Ctor]; fn != nil {
			nt.Vtable = ctorVtable(d, fn)
		}
		if nt.Vtable != 0 {
			nt.Slots = d.vtableSlots(nt.Vtable)
		}
		types[r.name] = nt
	}
	for _, r := range sites[registrar] {
		if r.parent != 0 {
			parent, ok := byDesc[r.parent]
			if !ok {
				return nil, fmt.Errorf("xbe: node type %s has unregistered parent descriptor %08X", r.name, r.parent)
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
	return types, nil
}

// Runs a constructor on a symbolic object and keeps the constants it stores into fields
func (d *Disassembly) ctorInits(ex *Executor, ctor uint32) (map[int32]NodeInit, error) {
	sub := NewExecutorShared(ex)
	paths, err := sub.Run(ctor, func(s *execState) {
		s.regs[1] = Ptr{"this", 0}
		s.pushArgs(Sym{"ctor.arg0"}, Sym{"ctor.arg1"}, Sym{"ctor.arg2"})
	})
	if err != nil {
		return nil, err
	}
	inits := map[int32]NodeInit{}
	for _, p := range paths {
		for _, e := range p.Effects {
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
	return inits, nil
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
	slot, ok := d.slotCalling(shape, rm.Accessor.Function)
	if !ok {
		return fmt.Errorf("xbe: no Shape method calls the format accessor %08X", rm.Accessor.Function)
	}
	dm.Slot = slot
	shapeDraw := shape.Slots[slot/4]
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
		case t == rm.Accessor.Function || t == rm.shaderLookup:
		case meshCallees[t]:
			dm.MeshLoaded = t
		case takesDescriptor(d, shapeDraw, t, descs):
			dm.KindCheck = t
		}
	}
	if dm.KindCheck == 0 || dm.MeshLoaded == 0 {
		return fmt.Errorf("xbe: shape draw %08X does not call a kind check and a mesh loaded check (kind %08X loaded %08X)", shapeDraw, dm.KindCheck, dm.MeshLoaded)
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
		return fmt.Errorf("xbe: material node draw %08X never looks its material up", matDraw)
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
			s.pushArgs(Sym{"arg0"}, Sym{"arg1"})
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
	// Constructor defaults stand in for fields the scene files omit on the classes the draw reads.
	for _, name := range append(chain, "Transform") {
		nt := types[name]
		inits, err := d.ctorInits(ex, nt.Ctor)
		if err != nil {
			return fmt.Errorf("xbe: node type %s constructor: %w", name, err)
		}
		nt.Init = inits
	}
	if err := summarize(bufferDraw); err != nil {
		return err
	}
	if err := summarize(rm.shaderLookup); err != nil {
		return err
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
				set := map[string]bool{}
				exprSyms(e.Args[1], set)
				for k := range set {
					var off int32
					if n, _ := fmt.Sscanf(k, "this@%X", &off); n == 1 {
						dm.BufferFields["count"] = off
					}
				}
			case e.Fn == fmt.Sprintf("sub_%08X", rm.shaderLookup) && len(e.Args) == 1:
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
	// The loaded check calls the class's load slot, and the sphere's load builds a buffer whose
	// format and stride are the last constants its builder stores into those fields.
	if err := summarize(dm.MeshLoaded); err != nil {
		return err
	}
	for _, p := range dm.paths[dm.MeshLoaded] {
		for _, e := range p.Effects {
			if e.Kind == "call" && strings.HasPrefix(e.Fn, "vcall@") {
				fmt.Sscanf(e.Fn, "vcall@%X", &dm.LoadSlot)
			}
		}
	}
	if dm.LoadSlot == 0 {
		return fmt.Errorf("xbe: mesh loaded check %08X makes no virtual load call", dm.MeshLoaded)
	}
	sphereLoad := types["Sphere"].Slots[dm.LoadSlot/4]
	for _, t := range d.plainCallees(sphereLoad) {
		format, stride := lastFieldConstants(d, t, dm.BufferFields["format"], dm.BufferFields["stride"])
		if format != 0 {
			dm.Sphere = SphereBuffer{Format: format, Stride: stride}
		}
	}
	if dm.Sphere.Format == 0 || dm.Sphere.Stride == 0 {
		return fmt.Errorf("xbe: sphere load %08X stores no buffer format and stride", sphereLoad)
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

// Reports whether a call site in caller passes a registered type descriptor to callee
func takesDescriptor(d *Disassembly, caller, callee uint32, descs map[uint32]bool) bool {
	ins := d.Functions[caller].Instructions
	for i := range ins {
		if ins[i].Inst.Op != x86asm.CALL || resolveTarget(&ins[i]) != callee {
			continue
		}
		for j := i - 1; j >= 0 && j > i-6; j-- {
			if ins[j].Inst.Op == x86asm.PUSH {
				if imm, ok := ins[j].Inst.Args[0].(x86asm.Imm); ok && descs[uint32(imm)] {
					return true
				}
			}
			if ins[j].Inst.Op == x86asm.CALL {
				break
			}
		}
	}
	return false
}

// Records every immediate argument of a path that addresses a wide string in the image
func (rm *RenderModel) collectStrings(d *Disassembly, p Path, out map[uint32]string) {
	var walk func(e Expr)
	walk = func(e Expr) {
		switch v := e.(type) {
		case CallResult:
			for _, a := range v.Args {
				if c, ok := isConst(a); ok {
					if s := d.Image.ReadUTF16(c); s != "" && isPrintable(s) {
						out[c] = s
					}
				}
				walk(a)
			}
		case Bin:
			walk(v.A)
			walk(v.B)
		case Un:
			walk(v.A)
		case Cond:
			walk(v.A)
			walk(v.B)
		case FCmpSet:
			walk(v.A)
			walk(v.B)
		case Not:
			walk(v.A)
		case Load:
			if v.Addr != nil {
				walk(v.Addr)
			}
		}
	}
	for _, g := range p.Guards {
		walk(g)
	}
	for _, e := range p.Effects {
		if e.Value != nil {
			walk(e.Value)
		}
		for _, a := range e.Args {
			if c, ok := isConst(a); ok {
				if s := d.Image.ReadUTF16(c); s != "" && isPrintable(s) {
					out[c] = s
				}
			}
			walk(a)
		}
	}
	walk(p.Return)
}
