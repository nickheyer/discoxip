package xbe

import (
	"fmt"
	"maps"
	"os"
	"slices"
	"sort"
	"strings"
	"time"

	"golang.org/x/arch/x86/x86asm"
)

// DeviceObject is the device global, the static device it points at and the device fields holding its surfaces
type DeviceObject struct {
	Global   uint32   `json:"global"`
	Object   uint32   `json:"object"`
	Surfaces []uint32 `json:"surfaces"`
}

// The static device the XDK's creation stores into the device global, found by the store of its address
func (d *Disassembly) deviceObject(global uint32) (uint32, error) {
	object := uint32(0)
	for _, va := range d.functionVAs() {
		fn := d.Functions[va]
		for i := range fn.Instructions {
			in := &fn.Instructions[i]
			if in.Inst.Op != x86asm.MOV || argCount(in) != 2 {
				continue
			}
			m, isMem := in.Inst.Args[0].(x86asm.Mem)
			imm, isImm := in.Inst.Args[1].(x86asm.Imm)
			if !isMem || !isImm || m.Base != 0 || m.Index != 0 || uint32(m.Disp) != global || !d.isDataAddress(uint32(imm)) {
				continue
			}
			if object != 0 && object != uint32(imm) {
				return 0, fmt.Errorf("xbe: the device global %08X is stored both %08X and %08X", global, object, uint32(imm))
			}
			object = uint32(imm)
		}
	}
	if object == 0 {
		return 0, fmt.Errorf("xbe: nothing stores a static object into the device global %08X", global)
	}
	return object, nil
}

// Routines reading a description off a surface
var surfaceDescribers = map[string]bool{"PixelJar::Get2DSurfaceDesc": true, "D3DSurface_GetDesc": true, "D3DTexture_GetLevelDesc": true}

// The device fields the init hands to a surface description getter, the back buffer among them
func (rm *RenderModel) deviceSurfaces(layout *deviceLayout) ([]uint32, error) {
	fields := map[uint32]bool{}
	for _, p := range rm.Draw.paths[rm.Scene.Init] {
		for _, e := range p.Effects {
			if e.Kind != "call" || !surfaceDescribers[e.Fn] {
				continue
			}
			for _, a := range e.Args {
				l, ok := a.(Load)
				if !ok || l.Size != 4 {
					continue
				}
				b, isAdd := l.Addr.(Bin)
				if !isAdd || b.Op != "add" {
					continue
				}
				global, isLoad := b.A.(Load)
				off, isOff := isConst(b.B)
				if isLoad && isOff && global.Addr == nil && global.Base == "" && uint32(global.Off) == layout.device {
					fields[off] = true
				}
			}
		}
	}
	if !fields[layout.backBuffer] {
		return nil, fmt.Errorf("xbe: the init %08X describes the device surfaces at %v, not the back buffer at +%X", rm.Scene.Init, slices.Sorted(maps.Keys(fields)), layout.backBuffer)
	}
	return slices.Sorted(maps.Keys(fields)), nil
}

// RenderModel is everything the web runtime needs that was derived from the executable.
type RenderModel struct {
	XDK            int                           `json:"xdk"`
	Materials      []*MaterialObject             `json:"materials"`
	Programs       map[string]*VertexProgram     `json:"programs"`
	Declarations   map[string]*VertexDeclaration `json:"declarations"`
	FixedFunction  []FFDeclaration               `json:"fixed_function"`
	Transforms     map[string]string             `json:"transforms"`
	Device         DeviceObject                  `json:"device"`
	StateNames     map[uint32]string             `json:"render_state_names"`
	StageNames     map[uint32]string             `json:"texture_stage_names"`
	Defaults       []PathJSON                    `json:"render_state_defaults"`
	Clock          *ClockGlobal                  `json:"clock"`
	HierarchyAlpha uint32                        `json:"hierarchy_alpha"`
	Formats        []uint32                      `json:"vertex_formats"`
	Accessor       MeshAccessor                  `json:"mesh_accessor"`
	Wrapper        uint32                        `json:"render_state_wrapper"`
	Exprs          *ExprPool                     `json:"exprs"`
	Textures       []*ProducedTexture            `json:"textures"`
	Vertex         map[int]string                `json:"vertex_semantics"`
	ShaderTable    []ShaderBinding               `json:"shader_table"`
	NVStateNames   map[uint32]string             `json:"nv_state_names"`
	WorldSlot      int32                         `json:"world_accessor_slot"`
	Memory         ImageMemory                   `json:"memory"`
	Types          map[string]*NodeType          `json:"node_types"`
	Draw           *DrawModel                    `json:"draw"`
	Lifted         map[uint32]*LiftedFunction    `json:"lifted"`
	Library        map[uint32]string             `json:"library"`
	Resource       *ResourceLayout               `json:"resource"`
	SurfaceDesc    SurfaceDescLayout             `json:"surface_desc"`
	GPU            uint32                        `json:"gpu"`
	System         *SystemModel                  `json:"system"`
	Scene          *SceneModel                   `json:"scene"`
	Allocators     []uint32                      `json:"allocators"`
	Registry       *MaterialRegistry             `json:"material_registry"`
	Kernel         *KernelModel                  `json:"kernel"`
	TLS            *TLSDirectory                 `json:"tls"`
	memory         []byte
	defaultPaths   []Path
	nodePaths      []Path // paths of the routines loading the node constants from the matrix stack top
	shaderLookup   uint32
	lookups        []shaderLookup
	accessorSites  []accessorSite
	wrapperState   argLocation
	wrapperValue   argLocation
	matrixStack    uint32
	stateCounts    map[uint32]int
	setterStates   map[uint32]uint32 // XDK setter each complex state dispatches to, by its address
}

// MeshAccessor is how the draw path reaches the vertex format of the mesh being drawn.
type MeshAccessor struct {
	Function uint32 `json:"function"` // dashboard function wrapping the virtual call
	Global   uint32 `json:"global"`   // global holding the mesh object pointer
	Offset   int32  `json:"offset"`   // mesh field holding the object the call is made on
	Slot     int32  `json:"slot"`     // vtable slot returning the format
}

// ShaderBinding is a vertex program and declaration a material path creates for a format.
type ShaderBinding struct {
	Format      uint32 `json:"format"`
	Declaration uint32 `json:"declaration"`
	Program     uint32 `json:"program"`
}

// FFDeclaration is the fixed function declaration the draw path selects for a vertex format.
type FFDeclaration struct {
	Format      uint32 `json:"format"`
	Declaration uint32 `json:"declaration"`
}

// ClockGlobal describes the seconds clock the dashboard animates with.
type ClockGlobal struct {
	VA    uint32  `json:"va"`
	Scale float64 `json:"scale"`
	Via   uint32  `json:"writer"`
}

var stageTrace = os.Getenv("DISCOXIP_STAGE_TRACE") != ""

var stageStart = time.Now()

// Prints a derivation stage name with the seconds elapsed when stage tracing is on
func stage(name string) {
	if stageTrace {
		fmt.Fprintf(os.Stderr, "stage: %s (%.1fs)\n", name, time.Since(stageStart).Seconds())
	}
}

// DeriveRender runs every derivation and assembles the render model, the kernel lifted beside the dashboard.
func DeriveRender(d *Disassembly, kernel *KernelInput) (*RenderModel, error) {
	build := int(d.Image.LibBuild("D3D8"))
	rm := &RenderModel{XDK: build, TLS: d.Image.TLS, Programs: map[string]*VertexProgram{}, Declarations: map[string]*VertexDeclaration{}, Transforms: map[string]string{}, Exprs: NewExprPool(), Lifted: map[uint32]*LiftedFunction{}}
	rm.StageNames = TextureStageStateNames(build)
	// The render state wrapper comes first so the state tables it writes are known before any path is summarized.
	stage("render states")
	if err := rm.deriveRenderStates(d); err != nil {
		return nil, err
	}
	stage("materials")
	ms, err := DeriveMaterials(d)
	if err != nil {
		return nil, err
	}
	rm.Materials, rm.Allocators, rm.Registry = ms.Materials, append([]uint32{ms.AllocatorVA}, ms.Allocators...), ms.Registry
	for _, m := range ms.Materials {
		m.Apply = pathsJSON(rm.Exprs, m.Paths)
	}

	// Shader programs and declarations referenced by any material path.
	formats := map[uint32]bool{}
	for _, m := range ms.Materials {
		for _, p := range m.Paths {
			for _, e := range p.Effects {
				if e.Kind == "call" && e.Fn == "D3DDevice_CreateVertexShader" && len(e.Args) >= 2 {
					declVA, ok1 := constAddr(e.Args[0])
					progVA, ok2 := constAddr(e.Args[1])
					if !ok1 || !ok2 {
						return nil, fmt.Errorf("material %q creates a shader from symbolic pointers", m.Name)
					}
					if err := rm.addDeclaration(d, declVA); err != nil {
						return nil, err
					}
					if progVA != 0 {
						if err := rm.addProgram(d, progVA); err != nil {
							return nil, err
						}
					}
					if f, ok := boundFormat(p.Guards); ok {
						rm.addShaderBinding(ShaderBinding{Format: f, Declaration: declVA, Program: progVA})
					}
				}
			}
			for _, g := range p.Guards {
				collectFormatConstants(g, formats)
			}
		}
	}
	for f := range formats {
		rm.Formats = append(rm.Formats, f)
	}
	sort.Slice(rm.Formats, func(i, j int) bool { return rm.Formats[i] < rm.Formats[j] })

	stage("mesh accessor")
	if err := rm.deriveMeshAccessor(d); err != nil {
		return nil, err
	}

	stage("fixed function")
	if err := rm.deriveFixedFunction(d, ms); err != nil {
		return nil, err
	}
	stage("transforms")
	if err := rm.deriveTransforms(d); err != nil {
		return nil, err
	}
	stage("render state defaults")
	if err := rm.deriveRenderDefaults(d, ms); err != nil {
		return nil, err
	}
	stage("constant loaders")
	if err := rm.deriveConstantLoaders(d, ms); err != nil {
		return nil, err
	}
	stage("clock")
	if err := rm.deriveClock(d); err != nil {
		return nil, err
	}
	stage("hierarchy alpha")
	if err := rm.deriveHierarchyAlpha(d, ms); err != nil {
		return nil, err
	}
	referenced := map[uint32]bool{}
	for _, m := range ms.Materials {
		for _, in := range m.Inputs {
			var va uint32
			if n, _ := fmt.Sscanf(in, "texture:%X", &va); n == 1 {
				referenced[va] = true
			}
		}
	}
	var producers []uint32
	for _, va := range ms.TextureProducers {
		if referenced[va] {
			producers = append(producers, va)
		}
	}
	stage("textures")
	textures, err := DeriveTextures(d, producers, ms.Allocators)
	if err != nil {
		return nil, err
	}
	rm.Textures = textures
	rm.Vertex = fixedFunctionRegisters
	stage("current mesh")
	if err := rm.deriveCurrentMesh(d); err != nil {
		return nil, err
	}
	stage("node types")
	types, registrar, err := DeriveNodeTypes(d)
	if err != nil {
		return nil, err
	}
	rm.Types = types
	stage("draw")
	if err := rm.deriveDraw(d, ms, types); err != nil {
		return nil, err
	}
	if kernel == nil {
		return nil, fmt.Errorf("xbe: the render model needs the kernel image the dashboard runs on")
	}
	stage("kernel disassembly")
	kd, err := DisassembleKernel(kernel.Image, kernel.Exports)
	if err != nil {
		return nil, err
	}
	stage("system")
	sm, err := DeriveSystem(d, kd, kernel, types, rm.Draw.Slot)
	if err != nil {
		return nil, err
	}
	rm.System = sm
	stage("scene")
	if err := rm.deriveScene(d, ms, types, sm, registrar); err != nil {
		return nil, err
	}
	// The device layout comes from the transform setter, which only the builds writing the device directly reveal
	if layout, err := d.deviceState(); err == nil {
		surfaces, err := rm.deviceSurfaces(layout)
		if err != nil {
			return nil, err
		}
		object, err := d.deviceObject(layout.device)
		if err != nil {
			return nil, err
		}
		rm.Device = DeviceObject{Global: layout.device, Object: object, Surfaces: surfaces}
	}
	stage("memory")
	if err := rm.deriveMemory(d); err != nil {
		return nil, err
	}
	stage("kernel")
	if err := rm.attachKernel(kd, kernel); err != nil {
		return nil, err
	}
	stage("registrations")
	if err := rm.replayRegistrations(d, registrar, types); err != nil {
		return nil, err
	}
	return rm, nil
}

// The startup memory bytes the model's sections index
func (rm *RenderModel) MemoryBytes() []byte {
	return rm.memory
}

// Maps the loaded image as the memory the runtime starts the executable with
func (rm *RenderModel) deriveMemory(d *Disassembly) error {
	var err error
	rm.Memory, rm.memory, err = d.Image.Memory()
	if err != nil {
		return err
	}
	if len(rm.memory) == 0 {
		return fmt.Errorf("xbe: the image maps no memory for the runtime to start from")
	}
	return nil
}

// Reports whether an instruction stores through an absolute address overlapping a global range
func storesAbsolute(insn *Instruction, va uint32, size int) bool {
	if argCount(insn) == 0 {
		return false
	}
	m, ok := insn.Inst.Args[0].(x86asm.Mem)
	if !ok || m.Base != 0 || m.Index != 0 {
		return false
	}
	switch insn.Inst.Op {
	case x86asm.CMP, x86asm.TEST, x86asm.PUSH, x86asm.FLD, x86asm.FILD, x86asm.FCOM, x86asm.FCOMP, x86asm.FICOM, x86asm.FICOMP,
		x86asm.FADD, x86asm.FSUB, x86asm.FSUBR, x86asm.FMUL, x86asm.FDIV, x86asm.FDIVR, x86asm.FIADD, x86asm.FISUB, x86asm.FISUBR, x86asm.FIMUL, x86asm.FIDIV, x86asm.FIDIVR,
		x86asm.JMP, x86asm.CALL, x86asm.LEA:
		return false
	}
	w := insn.Inst.MemBytes
	if w == 0 {
		w = 4
	}
	start := uint32(m.Disp)
	return start < va+uint32(size) && start+uint32(w) > va
}

// Vertex register meanings for fixed function declarations in Xbox D3D8
var fixedFunctionRegisters = map[int]string{0: "position", 1: "blendweight", 2: "normal", 3: "diffuse", 4: "specular", 5: "fog", 6: "pointsize", 7: "backdiffuse", 8: "backspecular", 9: "texcoord0", 10: "texcoord1", 11: "texcoord2", 12: "texcoord3"}

// Returns the vertex format a path's guards pin the accessor result to
func boundFormat(guards []Expr) (uint32, bool) {
	for _, g := range guards {
		c, ok := g.(Cond)
		if !ok || c.Op != "eq" {
			continue
		}
		if cr, ok := c.B.(CallResult); ok && strings.HasPrefix(cr.Fn, "vcall@") {
			if v, ok := isConst(c.A); ok {
				return v, true
			}
		}
		if cr, ok := c.A.(CallResult); ok && strings.HasPrefix(cr.Fn, "vcall@") {
			if v, ok := isConst(c.B); ok {
				return v, true
			}
		}
	}
	return 0, false
}

func (rm *RenderModel) addShaderBinding(b ShaderBinding) {
	for _, x := range rm.ShaderTable {
		if x == b {
			return
		}
	}
	rm.ShaderTable = append(rm.ShaderTable, b)
}

// Reads an address from an immediate or an absolute pointer expression
func constAddr(e Expr) (uint32, bool) {
	if c, ok := isConst(e); ok {
		return c, true
	}
	if p, ok := e.(Ptr); ok && p.Base == "" {
		return uint32(p.Off), true
	}
	return 0, false
}

func appendUnique(list []uint32, v uint32) []uint32 {
	for _, x := range list {
		if x == v {
			return list
		}
	}
	return append(list, v)
}

func (rm *RenderModel) addProgram(d *Disassembly, va uint32) error {
	key := fmt.Sprintf("%08X", va)
	if _, ok := rm.Programs[key]; ok {
		return nil
	}
	p, err := d.Image.DecodeVertexProgram(va)
	if err != nil {
		return err
	}
	rm.Programs[key] = p
	return nil
}

func (rm *RenderModel) addDeclaration(d *Disassembly, va uint32) error {
	key := fmt.Sprintf("%08X", va)
	if _, ok := rm.Declarations[key]; ok {
		return nil
	}
	decl, err := d.Image.DecodeVertexDeclaration(va)
	if err != nil {
		return err
	}
	rm.Declarations[key] = decl
	return nil
}

// Collects constants compared against the vertex format accessor in a guard
func collectFormatConstants(g Expr, out map[uint32]bool) {
	c, ok := g.(Cond)
	if !ok {
		return
	}
	var constant Expr
	var other Expr
	if _, isC := isConst(c.A); isC {
		constant, other = c.A, c.B
	} else if _, isC := isConst(c.B); isC {
		constant, other = c.B, c.A
	} else {
		return
	}
	if cr, ok := other.(CallResult); ok && strings.HasPrefix(cr.Fn, "vcall@") {
		v, _ := isConst(constant)
		out[v] = true
	}
	if l, ok := other.(Load); ok && l.Base == "fmt" {
		v, _ := isConst(constant)
		out[v] = true
	}
}

// Binds the view and projection matrix globals from the SetTransform calls the dashboard's functions make
func (rm *RenderModel) deriveTransforms(d *Disassembly) error {
	names := map[uint32]string{transformView: "view", transformProjection: "projection"}
	ex := NewExecutor(d)
	callers := ex.staticThisCallers()
	states := map[uint32]map[uint32]int{}
	for _, va := range d.functionVAs() {
		fn := d.Functions[va]
		if fn.Symbol != nil {
			continue
		}
		calls := false
		for _, t := range fn.CallTargets {
			if c := d.Functions[t]; c != nil && c.Symbol != nil && c.Symbol.Name == "D3DDevice_SetTransform" {
				calls = true
			}
		}
		if !calls {
			continue
		}
		sub := NewExecutorShared(ex)
		sub.StepLimit = 30000
		sub.PathLimit = 128
		this := callers[va]
		paths, partial, _, _ := sub.RunWithRetries(va, func(s *execState) {
			if this != 0 {
				ex.seedThis(s, va, this, 3)
			} else {
				s.regs[1] = Ptr{"this", 0}
				sub.seedArgs(s, va, "arg", 3)
			}
		}, 6)
		var effects []Effect
		for _, p := range paths {
			effects = append(effects, p.Effects...)
		}
		effects = append(effects, partial...)
		for _, e := range effects {
			if e.Kind != "call" || e.Fn != "D3DDevice_SetTransform" || len(e.Args) != 2 {
				continue
			}
			state, ok1 := isConst(e.Args[0])
			// The world matrix comes from the matrix stack top through a virtual accessor on a global object.
			if cr, isCall := e.Args[1].(CallResult); ok1 && state == transformWorld && isCall && strings.HasPrefix(cr.Fn, "vcall@") {
				var slot int32
				if n, _ := fmt.Sscanf(cr.Fn, "vcall@%X", &slot); n == 1 {
					if rm.WorldSlot != 0 && rm.WorldSlot != slot {
						return fmt.Errorf("xbe: the world transform comes from accessor slots %d and %d", rm.WorldSlot, slot)
					}
					rm.WorldSlot = slot
					for _, a := range cr.Args {
						if l, ok := a.(Load); ok && l.Base == "" && l.Addr == nil && l.Size == 4 {
							rm.matrixStack = uint32(l.Off)
						} else if va, ok := globalObjectAddress(a); ok {
							rm.matrixStack = va
						}
					}
				}
				continue
			}
			matrix, ok2 := constAddr(e.Args[1])
			if !ok1 || !ok2 || !d.inPureData(matrix) {
				continue
			}
			if states[matrix] == nil {
				states[matrix] = map[uint32]int{}
			}
			states[matrix][state]++
		}
	}
	for _, matrix := range slices.Sorted(maps.Keys(states)) {
		// A matrix handed to the device for several transforms is a constant, not one the camera computes.
		if len(states[matrix]) != 1 {
			continue
		}
		for state := range states[matrix] {
			name, ok := names[state]
			if !ok {
				continue
			}
			for prev, prevName := range rm.Transforms {
				if prevName == name {
					return fmt.Errorf("xbe: both %s and %08X are handed to the device as the %s matrix", prev, matrix, name)
				}
			}
			rm.Transforms[fmt.Sprintf("%08X", matrix)] = name
		}
	}
	if len(rm.Transforms) == 0 {
		return fmt.Errorf("xbe: no view or projection global is handed to the device")
	}
	return nil
}

// Derives render state names from the wrapper's method table and the startup defaults
func (rm *RenderModel) deriveRenderStates(d *Disassembly) error {
	// Candidates call the render state setters of the XDK, the wrapper among them mapping a state index to a method through a table.
	counts := map[uint32]int{}
	tables := map[uint32]uint32{}
	for _, va := range d.functionVAs() {
		fn := d.Functions[va]
		if fn.Symbol != nil {
			continue
		}
		n := 0
		for _, t := range fn.CallTargets {
			if c := d.Functions[t]; c != nil && c.Symbol != nil && strings.HasPrefix(c.Symbol.Name, "D3DDevice_SetRenderState_") {
				n++
			}
		}
		if n == 0 {
			continue
		}
		counts[va] = n
		if table := methodTableLoad(d, fn); table != 0 {
			tables[va] = table
		}
	}
	wrapper := uint32(0)
	for _, va := range slices.Sorted(maps.Keys(tables)) {
		if wrapper == 0 || counts[va] > counts[wrapper] {
			wrapper = va
		}
	}
	if wrapper == 0 {
		return fmt.Errorf("xbe: no render state wrapper indexes a method table")
	}
	rm.Wrapper = wrapper
	table := tables[wrapper]
	names := map[uint32]string{}
	methods := map[uint32]uint32{}
	for idx := uint32(0); idx < 256; idx++ {
		v, ok := d.Image.ReadU32(table + idx*4)
		if !ok {
			break
		}
		if v>>18 == 1 {
			methods[idx] = v & 0x1FFF
		}
		if name, ok := NV097StateName(v); ok && v>>18 == 1 {
			names[idx] = name
		}
	}
	// Complex states: the wrapper tests the state for equality before calling a named function.
	cross := RenderStateNames(rm.XDK)
	ins := d.Functions[wrapper].Instructions
	lastCmp := int64(-1)
	equality := false
	for i := range ins {
		in := &ins[i]
		switch in.Inst.Op {
		case x86asm.CMP:
			if imm, ok := in.Inst.Args[1].(x86asm.Imm); ok {
				lastCmp = int64(imm)
				equality = false
			}
		case x86asm.JNE, x86asm.JE:
			equality = true
		case x86asm.JGE, x86asm.JL, x86asm.JG, x86asm.JLE, x86asm.JA, x86asm.JB, x86asm.JAE, x86asm.JBE:
			equality = false
		case x86asm.CALL:
			if lastCmp < 0 || !equality {
				continue
			}
			callee := d.Functions[resolveTarget(in)]
			// Setters whose bytes several states share are identified under collision names.
			if callee == nil || callee.Symbol == nil || !strings.Contains(callee.Symbol.Name, "SetRenderState") {
				continue
			}
			switch callee.Symbol.Name {
			case "D3DDevice_SetRenderState_Simple", "D3DDevice_SetRenderState_Deferred", "D3DDevice_SetRenderStateNotInline":
				continue
			}
			names[uint32(lastCmp)] = stateNameFromSymbol(callee.Symbol, cross[uint32(lastCmp)])
			if rm.setterStates == nil {
				rm.setterStates = map[uint32]uint32{}
			}
			rm.setterStates[resolveTarget(in)] = uint32(lastCmp)
		}
	}
	// A setter name several functions share came from an ambiguous signature, and the dispatch index names it instead.
	shared := map[string]int{}
	for _, fn := range d.Functions {
		if fn.Symbol != nil {
			shared[fn.Symbol.Name]++
		}
	}
	for idx, name := range names {
		ref, ok := cross[idx]
		if !ok {
			continue
		}
		if !sameStateName(ref, name) && shared["D3DDevice_SetRenderState_"+name] < 2 {
			return fmt.Errorf("xbe: render state %d is %s in the binary but %s in the XDK %d table", idx, name, ref, rm.XDK)
		}
		names[idx] = ref
	}
	for _, va := range slices.Sorted(maps.Keys(rm.setterStates)) {
		fn := d.Functions[va]
		if shared[fn.Symbol.Name] < 2 && !strings.HasPrefix(fn.Symbol.Name, "Collisions_") {
			continue
		}
		name, ok := names[rm.setterStates[va]]
		if !ok {
			return fmt.Errorf("xbe: the setter %08X of render state %d has no name", va, rm.setterStates[va])
		}
		fn.Symbol.Name = "D3DDevice_SetRenderState_" + name
		fn.Name = fn.Symbol.Name
	}
	state, value, err := d.wrapperInputs(wrapper)
	if err != nil {
		return err
	}
	rm.wrapperState, rm.wrapperValue = state, value
	// Deferred states: the wrapper hands one contiguous index range to the deferred setter,
	// and that range must coincide with the deferred section of the XDK table.
	lo, hi, array, err := d.deferredStateRange(wrapper, state, value)
	if err != nil {
		return err
	}
	if array != 0 {
		layout, err := d.deviceState()
		if err != nil {
			return err
		}
		layout.renders = &stateArray{base: array, count: uint32(len(cross))}
	}
	sections := RenderStateSections(rm.XDK)
	if lo != sections[1] || hi != sections[2] {
		return fmt.Errorf("xbe: wrapper defers render states %d..%d but the XDK %d table defers %d..%d", lo, hi-1, rm.XDK, sections[1], sections[2]-1)
	}
	for idx := lo; idx < hi; idx++ {
		if prev, taken := names[idx]; taken {
			return fmt.Errorf("xbe: render state %d is dispatched as %s and also deferred", idx, prev)
		}
		names[idx] = cross[idx]
	}
	// Every simple state the wrapper pushes as a single method takes its XDK name, and a method the hardware names leave
	// out is known by it
	for idx := sections[0]; idx < sections[1]; idx++ {
		if _, pushed := methods[idx]; pushed && names[idx] == "" && cross[idx] != "" {
			names[idx] = cross[idx]
		}
	}
	rm.NVStateNames = maps.Clone(nv097StateNames)
	for idx, m := range methods {
		if name, ok := names[idx]; ok && rm.NVStateNames[m] == "" {
			rm.NVStateNames[m] = name
		}
	}
	rm.StateNames = names
	rm.stateCounts = counts
	return nil
}

// Runs the function setting the render state defaults at startup
func (rm *RenderModel) deriveRenderDefaults(d *Disassembly, ms *MaterialSystem) error {
	wrapper, counts := rm.Wrapper, rm.stateCounts
	// Defaults: the function setting the most states, through the wrapper or the XDK setters it inlined.
	best, bestN := uint32(0), 0
	for _, va := range d.functionVAs() {
		if va == wrapper {
			continue
		}
		fn := d.Functions[va]
		n := counts[va]
		for _, t := range fn.CallTargets {
			if t == wrapper {
				n++
			}
		}
		if n > bestN {
			best, bestN = va, n
		}
	}
	if best == 0 {
		return fmt.Errorf("xbe: no render state default initializer found")
	}
	ex := NewExecutor(d)
	for _, va := range ms.TextureProducers {
		ex.Opaque[va] = true
	}
	thisVA := ex.staticThisCallers()[best]
	paths, err := ex.Run(best, func(s *execState) {
		if thisVA != 0 {
			ex.seedThis(s, best, thisVA, 0)
		} else {
			s.regs[1] = Ptr{"this", 0}
		}
	})
	if err != nil {
		return fmt.Errorf("render state defaults %08X: %w", best, err)
	}
	rm.Defaults = pathsJSON(rm.Exprs, paths)
	rm.defaultPaths = paths
	return nil
}

// Finds the table a function indexes into ecx right before calling the simple render state setter
func methodTableLoad(d *Disassembly, fn *Function) uint32 {
	ins := fn.Instructions
	for i := range ins {
		if ins[i].Inst.Op != x86asm.CALL {
			continue
		}
		callee := d.Functions[resolveTarget(&ins[i])]
		if callee == nil || callee.Symbol == nil || callee.Symbol.Name != "D3DDevice_SetRenderState_Simple" {
			continue
		}
		for j := i - 1; j >= 0 && j > i-8; j-- {
			in := &ins[j]
			if in.Inst.Op != x86asm.MOV || argCount(in) != 2 {
				continue
			}
			dst, ok := in.Inst.Args[0].(x86asm.Reg)
			if !ok || dst != x86asm.ECX {
				continue
			}
			if m, ok := in.Inst.Args[1].(x86asm.Mem); ok && (m.Base != 0 || m.Index != 0) && d.sectionOf(uint32(m.Disp)) != nil {
				return uint32(m.Disp)
			}
			break
		}
	}
	return 0
}

// Locates the state index and value inputs of the wrapper, the index being the input it scales into its tables
func (d *Disassembly) wrapperInputs(wrapper uint32) (argLocation, argLocation, error) {
	fn := d.Functions[wrapper]
	regs := d.liveInRegisters(fn)
	roles, err := d.inputRoles(fn, regs, calleeCleanupOf(fn)/4)
	if err != nil {
		return argLocation{}, argLocation{}, err
	}
	var state, value argLocation
	found := 0
	for loc, r := range roles {
		if r.scaled {
			state = loc
			found++
		}
	}
	if found != 1 {
		return argLocation{}, argLocation{}, fmt.Errorf("xbe: render state wrapper %08X scales %d of its inputs into a table, expected one (%v)", wrapper, found, roles)
	}
	found = 0
	for loc := range roles {
		if loc != state {
			value = loc
			found++
		}
	}
	if found != 1 {
		return argLocation{}, argLocation{}, fmt.Errorf("xbe: render state wrapper %08X takes %d inputs besides the state, expected one (%v)", wrapper, found, roles)
	}
	return state, value, nil
}

// Runs the wrapper on a symbolic state and reads the bounds guarding its deferred setter call
func (d *Disassembly) deferredStateRange(wrapper uint32, stateLoc, valueLoc argLocation) (uint32, uint32, uint32, error) {
	ex := NewExecutor(d)
	state := Sym{"rs.state"}
	paths, err := ex.Run(wrapper, func(s *execState) {
		stackArgs := calleeCleanupOf(d.Functions[wrapper]) / 4
		args := symbolicArgs(stackArgs, -1, nil)
		if stateLoc.Reg != 0 {
			s.regs[regIndex[stateLoc.Reg]] = state
		} else {
			args[stateLoc.Stack] = state
		}
		if valueLoc.Reg != 0 {
			s.regs[regIndex[valueLoc.Reg]] = Sym{"rs.value"}
		} else {
			args[valueLoc.Stack] = Sym{"rs.value"}
		}
		s.pushArgs(args...)
	})
	if err != nil {
		return 0, 0, 0, fmt.Errorf("render state wrapper %08X: %w", wrapper, err)
	}
	for _, p := range paths {
		// The deferred path hands the state to the XDK's deferred setter or, when that is inlined, records it in a table
		// indexed by the state without any setter call.
		deferred, setter, indexed := false, false, false
		array := uint32(0)
		for _, e := range p.Effects {
			if e.Kind == "call" && e.Fn == "D3DDevice_SetRenderState_Deferred" {
				deferred = true
			}
			if e.Kind == "call" && strings.HasPrefix(e.Fn, "D3DDevice_SetRenderState") {
				setter = true
			}
			if e.Kind == "store" && e.Addr != nil && e.Value == Expr(Sym{"rs.value"}) {
				set := map[string]bool{}
				exprSyms(e.Addr, set)
				if set[state.Name] {
					indexed = true
					// The value lands in the XDK's own state table indexed by the state, the last such table being the XDK's.
					if b, ok := e.Addr.(Bin); ok && b.Op == "add" {
						if c, ok := isConst(b.B); ok {
							array = c
						} else if c, ok := isConst(b.A); ok {
							array = c
						}
					}
				}
			}
		}
		if !deferred && (setter || !indexed) {
			continue
		}
		if deferred {
			// The XDK's own setter keeps its table, so the stores here are the dashboard's copy.
			array = 0
		}
		lo, hi := uint32(0), uint32(0)
		for _, g := range p.Guards {
			c, ok := g.(Cond)
			if !ok || c.A != state {
				continue
			}
			bound, ok := isConst(c.B)
			if !ok {
				continue
			}
			switch c.Op {
			case "ge", "uge":
				lo = bound
			case "gt", "ugt":
				lo = bound + 1
			case "lt", "ult":
				hi = bound
			case "le", "ule":
				hi = bound + 1
			}
		}
		if lo == 0 || hi <= lo {
			return 0, 0, 0, fmt.Errorf("render state wrapper %08X defers an unbounded range %d..%d", wrapper, lo, hi)
		}
		return lo, hi, array, nil
	}
	return 0, 0, 0, fmt.Errorf("render state wrapper %08X never calls the deferred setter", wrapper)
}

// Compares a binary derived state name with the XDK table name ignoring case and underscores
func sameStateName(ref, name string) bool {
	norm := func(x string) string { return strings.ToUpper(strings.ReplaceAll(x, "_", "")) }
	return norm(ref) == norm(name)
}

// Picks the state name from a dispatch symbol, resolving byte identical collisions with the XDK table
func stateNameFromSymbol(sym *Symbol, ref string) string {
	candidates := []string{sym.Name}
	for _, alt := range sym.Alternates {
		candidates = append(candidates, decorateSymbol(alt).Name)
	}
	for _, c := range candidates {
		n := strings.TrimPrefix(c, "D3DDevice_SetRenderState_")
		if ref != "" && sameStateName(ref, n) {
			return n
		}
	}
	if strings.HasPrefix(sym.Name, "Collisions_") && ref != "" {
		return ref
	}
	return strings.TrimPrefix(sym.Name, "D3DDevice_SetRenderState_")
}

// Runs every routine that loads the per node vertex shader constants, which read the matrix stack top
func (rm *RenderModel) deriveConstantLoaders(d *Disassembly, ms *MaterialSystem) error {
	// They are the functions whose paths load four rows into vertex shader constant register zero.
	probe := NewExecutor(d)
	ms.hideFrom(probe)
	targets := map[uint32]bool{}
	for _, va := range d.functionVAs() {
		fn := d.Functions[va]
		if fn.Symbol != nil {
			continue
		}
		calls := false
		for _, t := range fn.CallTargets {
			if c := d.Functions[t]; c != nil && c.Symbol != nil && strings.HasPrefix(c.Symbol.Name, "D3DDevice_SetVertexShaderConstant") {
				calls = true
			}
		}
		if !calls {
			continue
		}
		sub := NewExecutorShared(probe)
		sub.StepLimit = 30000
		sub.PathLimit = 128
		paths, partial, _, _ := sub.RunWithRetries(va, func(s *execState) {}, 6)
		var effects []Effect
		for _, p := range paths {
			effects = append(effects, p.Effects...)
		}
		effects = append(effects, partial...)
		for _, e := range effects {
			if e.Kind != "call" || !strings.HasPrefix(e.Fn, "D3DDevice_SetVertexShaderConstant") || len(e.Args) < 2 {
				continue
			}
			register, ok := isConst(e.Args[0])
			if !ok || register != 0 {
				continue
			}
			rows := uint32(0)
			if len(e.Args) >= 3 {
				rows, _ = isConst(e.Args[2])
			} else {
				fmt.Sscanf(strings.TrimPrefix(e.Fn, "D3DDevice_SetVertexShaderConstant"), "%d", &rows)
			}
			if rows == 4 {
				targets[va] = true
			}
		}
	}
	if len(targets) == 0 {
		return fmt.Errorf("xbe: no node constant loader found")
	}
	ex := NewExecutor(d)
	ms.hideFrom(ex)
	var failures []string
	for _, target := range slices.Sorted(maps.Keys(targets)) {
		paths, err := ex.Run(target, func(s *execState) {})
		if err != nil {
			failures = append(failures, fmt.Sprintf("%08X: %v", target, err))
			continue
		}
		rm.nodePaths = append(rm.nodePaths, paths...)
	}
	if len(rm.nodePaths) == 0 {
		return fmt.Errorf("xbe: no node constant loader runs to completion (%s)", strings.Join(failures, "; "))
	}
	if rm.WorldSlot == 0 {
		return fmt.Errorf("xbe: no transform setting routine takes the world matrix from a virtual accessor")
	}
	return nil
}

// Finds the seconds clock as the double a tick function derives from KeTickCount
func (rm *RenderModel) deriveClock(d *Disassembly) error {
	ex := NewExecutor(d)
	callers := ex.staticThisCallers()
	for _, fnVA := range slices.Sorted(maps.Keys(callers)) {
		thisVA := callers[fnVA]
		fn := d.Functions[fnVA]
		if fn == nil || len(fn.Instructions) > 400 {
			continue
		}
		readsTick := false
		for _, t := range fn.CallTargets {
			if c := d.Functions[t]; c != nil {
				for _, in := range c.Instructions {
					if m, ok := in.Inst.Args[1].(x86asm.Mem); ok && m.Base == 0 && d.Imports[uint32(m.Disp)] == "KeTickCount" {
						readsTick = true
					}
				}
			}
		}
		for _, in := range fn.Instructions {
			if m, ok := in.Inst.Args[1].(x86asm.Mem); ok && m.Base == 0 && d.Imports[uint32(m.Disp)] == "KeTickCount" {
				readsTick = true
			}
		}
		if !readsTick {
			continue
		}
		sub := NewExecutorShared(ex)
		paths, partial, _, _ := sub.RunWithRetries(fnVA, func(s *execState) { ex.seedThis(s, fnVA, thisVA, 0) }, 6)
		var effects []Effect
		for _, p := range paths {
			effects = append(effects, p.Effects...)
		}
		effects = append(effects, partial...)
		for _, e := range effects {
			if e.Kind != "store" || e.Base != "" || e.Size != 8 {
				continue
			}
			scale, ok := tickScale(e.Value)
			if !ok {
				continue
			}
			rm.Clock = &ClockGlobal{VA: uint32(e.Off), Scale: scale, Via: fnVA}
			return nil
		}
	}
	return fmt.Errorf("xbe: no tick count derived clock found")
}

// Extracts the multiplier applied to the tick count in a clock expression
func tickScale(e Expr) (float64, bool) {
	switch v := e.(type) {
	case Bin:
		if v.Op == "fmul" {
			if c, ok := isFConst(v.B); ok && mentionsTick(v.A) {
				return c, true
			}
			if c, ok := isFConst(v.A); ok && mentionsTick(v.B) {
				return c, true
			}
		}
		if v.Op == "fadd" || v.Op == "fsub" {
			if s, ok := tickScale(v.A); ok {
				return s, true
			}
		}
	case Un:
		return tickScale(v.A)
	}
	return 0, false
}

func mentionsTick(e Expr) bool {
	switch v := e.(type) {
	case CallResult:
		if strings.Contains(v.Fn, "TickCount") {
			return true
		}
		for _, a := range v.Args {
			if mentionsTick(a) {
				return true
			}
		}
	case Load:
		return strings.Contains(v.Base, "KeTickCount")
	case Bin:
		return mentionsTick(v.A) || mentionsTick(v.B)
	case Un:
		return mentionsTick(v.A)
	}
	return false
}

// Finds the alpha global the scene traversal scales down the node hierarchy
func (rm *RenderModel) deriveHierarchyAlpha(d *Disassembly, ms *MaterialSystem) error {
	// It is a float global read by material Apply methods, initialized to 1.0 in the image,
	// and multiplied in place by a float stored elsewhere in some function.
	counts := map[uint32]int{}
	for _, m := range ms.Materials {
		set := map[string]bool{}
		for _, p := range m.Paths {
			for _, e := range p.Effects {
				for _, a := range e.Args {
					exprSyms(a, set)
				}
				if e.Value != nil {
					exprSyms(e.Value, set)
				}
			}
		}
		for k := range set {
			var va uint32
			if n, _ := fmt.Sscanf(k, "@%X", &va); n == 1 {
				counts[va]++
			}
		}
	}
	for _, va := range slices.Sorted(maps.Keys(counts)) {
		v, ok := d.Image.ReadU32(va)
		if !ok || v != 0x3F800000 {
			continue
		}
		for _, insn := range d.InsnByVA {
			if insn.Inst.Op == x86asm.FMUL {
				if m, ok := insn.Inst.Args[len(insn.Inst.Args)-1].(x86asm.Mem); ok && m.Base == 0 && uint32(m.Disp) == va {
					rm.HierarchyAlpha = va
					return nil
				}
				for _, a := range insn.Inst.Args {
					if m, ok := a.(x86asm.Mem); ok && m.Base == 0 && uint32(m.Disp) == va {
						rm.HierarchyAlpha = va
						return nil
					}
				}
			}
		}
	}
	return fmt.Errorf("xbe: no hierarchy alpha global found")
}
