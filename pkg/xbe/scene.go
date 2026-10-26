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

// SceneModel is the frame loop, camera, node traversal and script natives the executable runs around the draws.
type SceneModel struct {
	Renderer     uint32                       `json:"renderer"`
	Camera       uint32                       `json:"camera"`
	MatrixStack  uint32                       `json:"matrix_stack"`
	Frame        uint32                       `json:"frame"`
	Tick         uint32                       `json:"tick"`
	Init         uint32                       `json:"init"`
	Disk         uint32                       `json:"disk"`
	Initializers []uint32                     `json:"initializers"`
	MainLoad     uint32                       `json:"main_load"`
	MainLoadArgs []string                     `json:"main_load_args"`
	CameraTick   uint32                       `json:"camera_tick"`
	TickSlot     int32                        `json:"tick_slot"`
	Group        GroupTraversal               `json:"group"`
	Natives      map[string]map[string]uint32 `json:"natives"`
	FireEvent    uint32                       `json:"fire_event"`
	ArrayElem    uint32                       `json:"array_element"`
	Present      uint32                       `json:"present"`
	Loader       uint32                       `json:"loader"`
	ImageLoad    []uint32                     `json:"image_load"`
	Unloader     uint32                       `json:"unloader"`
	InlineLoad   uint32                       `json:"inline_load"`
	PostLoad     uint32                       `json:"post_load"`
	SceneLoad    SceneLoadHelpers             `json:"scene_load"`
	ScriptTick   uint32                       `json:"script_tick"`
	TickHooks    []uint32                     `json:"tick_hooks"`
	Input        []uint32                     `json:"input"`
	Timers       TimerList                    `json:"timers"`
	Control      uint32                       `json:"control_global"`
	Script       ScriptCalls                  `json:"script"`
	Slots        map[string]SceneSlots        `json:"slots"`
	Errors       map[string]string            `json:"errors"`
}

// ScriptCalls is the engine's native dispatcher, the conversions it asks of argument values and the value objects
// it takes back from natives.
type ScriptCalls struct {
	Call         uint32 `json:"call"`
	Resolve      int32  `json:"resolve"`
	ToNumber     int32  `json:"to_number"`
	ToString     int32  `json:"to_string"`
	Release      int32  `json:"release"`
	NumberField  int32  `json:"number_field"`
	StringField  int32  `json:"string_field"`
	NumberVtable uint32 `json:"number_vtable"`
	StringVtable uint32 `json:"string_vtable"`
}

// SceneSlots are the per class methods the runtime runs, zero where the class inherits none.
type SceneSlots struct {
	Draw uint32 `json:"draw"`
	Tick uint32 `json:"tick"`
	Hook uint32 `json:"hook"`
}

// SceneLoadHelpers are the scene tree loader's steps the runtime performs itself when it builds an inline.
type SceneLoadHelpers struct {
	Reset    uint32   `json:"reset"`
	Resolve  uint32   `json:"resolve"`
	Populate uint32   `json:"populate"`
	NewScope uint32   `json:"new_scope"`
	Attach   uint32   `json:"attach"`
	Join     PathJoin `json:"join"`
}

// PathJoin is the helper joining a path onto a base path, its inputs numbered as the runtime receives them,
// the receiver register first, then the argument registers, then the stack
type PathJoin struct {
	VA   uint32 `json:"va"`
	Src  int    `json:"src"`
	Base int    `json:"base"`
	Out  int    `json:"out"`
}

// TimerList is the executable's list of field interpolations the tick advances.
type TimerList struct {
	Advance uint32 `json:"advance"`
	Remove  uint32 `json:"remove"`
	Walk    uint32 `json:"walk"`
	Head    uint32 `json:"head"`
	Next    int32  `json:"next"`
	Node    int32  `json:"node"`
}

// GroupTraversal is how a group walks its children array when drawing and ticking.
type GroupTraversal struct {
	Children int32  `json:"children"`
	Visible  int32  `json:"visible"`
	Draw     uint32 `json:"draw"`
	Tick     uint32 `json:"tick"`
}

var sceneTrace = os.Getenv("DISCOXIP_SCENE_TRACE") != ""

// Callee levels below a scene root that are summarized along with it
const sceneCalleeDepth = 2

// Effects the paths of one routine may carry before it runs block by block instead
const maxPathEffects = 262144

// Reports whether a routine's own instructions name a kernel export through its thunk
func (d *Disassembly) usesImport(va uint32, name string) bool {
	fn := d.Functions[va]
	if fn == nil {
		return false
	}
	for i := range fn.Instructions {
		for _, a := range fn.Instructions[i].Inst.Args {
			if m, ok := a.(x86asm.Mem); ok && m.Base == 0 && m.Index == 0 && d.Imports[uint32(m.Disp)] == name {
				return true
			}
		}
	}
	return false
}

// Reports whether a routine calls one callee before it first calls another
func callsBefore(fn *Function, first, second uint32) bool {
	for i := range fn.Instructions {
		in := &fn.Instructions[i]
		if in.Inst.Op != x86asm.CALL {
			continue
		}
		switch resolveTarget(in) {
		case first:
			return true
		case second:
			return false
		}
	}
	return false
}

// Effects every path of a routine carries, added up
func pathEffects(paths []Path) int {
	n := 0
	for _, p := range paths {
		n += len(p.Effects)
	}
	return n
}

// The XDK routines through which the dashboard reaches the controllers, which the runtime drives itself
var inputRoutines = map[string]bool{"XGetDevices": true, "XGetDeviceChanges": true, "XInputOpen": true, "XInputClose": true, "XInputGetState": true, "XInputPoll": true, "XInputGetCapabilities": true}

// The routines calling the controller routines directly, less those writing a global, which belong to the scene
func (d *Disassembly) inputLayer() []uint32 {
	var out []uint32
	for _, va := range d.functionVAs() {
		fn := d.Functions[va]
		if fn.Symbol != nil || d.inLibrary(va) || d.storesAbsoluteGlobal(fn) {
			continue
		}
		for _, t := range fn.CallTargets {
			if c := d.Functions[t]; c != nil && c.Symbol != nil && inputRoutines[c.Symbol.Name] {
				out = append(out, va)
				break
			}
		}
	}
	return out
}

// Reports whether a function stores into a writable global through its address alone
func (d *Disassembly) storesAbsoluteGlobal(fn *Function) bool {
	for i := range fn.Instructions {
		in := &fn.Instructions[i]
		switch in.Inst.Op {
		case x86asm.CMP, x86asm.TEST, x86asm.PUSH, x86asm.CALL, x86asm.JMP, x86asm.LEA, x86asm.FLD, x86asm.FILD, x86asm.FCOM, x86asm.FCOMP:
			continue
		}
		if argCount(in) == 0 {
			continue
		}
		m, ok := in.Inst.Args[0].(x86asm.Mem)
		if ok && m.Base == 0 && m.Index == 0 && d.isDataAddress(uint32(m.Disp)) {
			return true
		}
	}
	return false
}

// Classes whose draw, tick and field hook must summarize for the dashboard scenes to render
var requiredSceneClasses = []string{"Node", "Group", "Transform", "Level", "Inline", "Layer", "Waver", "HUD", "Background", "Viewpoint", "NavigationInfo", "Screen", "Shape"}

type sceneWork struct {
	va       uint32
	this     uint32
	required bool
	depth    int
}

// DeriveScene summarizes the frame loop, the camera, the node class draw and tick methods and the natives that move the scene.
func (rm *RenderModel) deriveScene(d *Disassembly, ms *MaterialSystem, types map[string]*NodeType, sm *SystemModel, registrar uint32) error {
	ex := NewExecutor(d)
	callers := ex.staticThisCallers()
	sc := &SceneModel{Natives: map[string]map[string]uint32{}, Slots: map[string]SceneSlots{}, Errors: map[string]string{}, Initializers: []uint32{}}
	sc.Renderer = callers[rm.Clock.Via]
	if sc.Renderer == 0 {
		return fmt.Errorf("xbe: clock writer %08X is not called on a static object", rm.Clock.Via)
	}
	// The tick advances the clock and dispatches a virtual call to the scene, from a renderer method or a plain routine
	writers := map[uint32]bool{}
	for _, fn := range slices.Sorted(maps.Keys(callers)) {
		this := callers[fn]
		if this != sc.Renderer {
			continue
		}
		if d.reaches(fn, "D3DXMatrixPerspectiveFovLH", 0) && d.reaches(fn, "D3DDevice_Clear", 1) {
			sc.Frame = fn
		}
		if storesGlobal(ex, fn, this, rm.Clock.VA, 8) {
			writers[fn] = true
		}
	}
	for va, fn := range d.Functions {
		if fn.Symbol != nil || d.inLibrary(va) {
			continue
		}
		for i := range fn.Instructions {
			if storesAbsolute(&fn.Instructions[i], rm.Clock.VA, 8) {
				writers[va] = true
			}
		}
	}
	group := types["Group"]
	if group == nil || len(group.Slots) <= int(rm.Draw.Slot/4) {
		return fmt.Errorf("xbe: Group is not registered with a draw slot")
	}
	// The scene dispatch is the one slot a group hands on to its children unchanged, which the draw slot is not
	for _, fn := range slices.Sorted(maps.Keys(writers)) {
		for _, slot := range indirectCallSlots(d, fn) {
			if slot == rm.Draw.Slot || slot < 0 || int(slot/4) >= len(group.Slots) {
				continue
			}
			if s, err := indirectCallSlot(d, group.Slots[slot/4]); err != nil || s != slot {
				continue
			}
			if sc.Tick != 0 {
				return fmt.Errorf("xbe: both %08X slot %d and %08X slot %d advance the clock %08X and tick the scene", sc.Tick, sc.TickSlot, fn, slot, rm.Clock.VA)
			}
			sc.Tick, sc.TickSlot = fn, slot
		}
	}
	if sc.Tick == 0 {
		return fmt.Errorf("xbe: no routine advances the clock %08X and ticks the scene (writers %X)", rm.Clock.VA, slices.Sorted(maps.Keys(writers)))
	}
	// Overlapping entries recovered from pointer tables also reach the device creation, but nothing calls them
	for fn, f := range d.Functions {
		if len(f.CalledBy) > 0 && d.reaches(fn, "Direct3D_CreateDevice", 0) {
			if sc.Init != 0 {
				return fmt.Errorf("xbe: two routines create the device (%08X, %08X)", sc.Init, fn)
			}
			sc.Init = fn
		}
	}
	if sc.Frame == 0 || sc.Init == 0 {
		return fmt.Errorf("xbe: renderer %08X has no frame (%08X) or init (%08X) routine", sc.Renderer, sc.Frame, sc.Init)
	}
	// The startup names the main scene file to the renderer method that loads it, handing it the renderer again
	// or only the buffer for the file's path, as its call site shows
	for _, startup := range d.Functions[sc.Init].CalledBy {
		for _, c := range d.plainCallees(startup) {
			if fn := d.Functions[c]; fn != nil && pushesWideStringSuffix(d, fn, ".xap") {
				sc.MainLoad = c
				seed := map[x86asm.Reg]uint32{}
				if addr := callers[startup]; addr != 0 && ex.thisLoc[startup].Reg != 0 {
					seed[ex.thisLoc[startup].Reg] = addr
				}
				sc.MainLoadArgs = mainLoadArgs(d, d.Functions[startup], c, sc.Renderer, seed)
			}
			// The startup links the drive letters its paths name before the init runs, through a routine of its own
			if d.usesImport(c, "IoCreateSymbolicLink") {
				if sc.Disk != 0 && sc.Disk != c {
					return fmt.Errorf("xbe: startup routines link the drives in two routines (%08X, %08X)", sc.Disk, c)
				}
				if !callsBefore(d.Functions[startup], c, sc.Init) {
					return fmt.Errorf("xbe: startup %08X links the drives in %08X only after the init %08X", startup, c, sc.Init)
				}
				sc.Disk = c
			}
		}
	}
	if sc.MainLoad == 0 {
		return fmt.Errorf("xbe: no startup routine around init %08X names the main scene file", sc.Init)
	}
	if sc.Disk == 0 {
		return fmt.Errorf("xbe: no startup routine around init %08X links the drive letters", sc.Init)
	}
	// Where the optimizer merged the frame loop into the tick, the tick is the loop body and the draw ends it
	if slices.Contains(d.Functions[sc.Tick].CallTargets, sc.Frame) {
		entry, stop, err := loopBody(d, sc.Tick, sc.Frame)
		if err != nil {
			return err
		}
		if d.Functions[entry] == nil {
			d.Functions[entry] = d.buildFunction(entry, nil)
		}
		sc.Tick = entry
		ex.Stop = map[uint32]bool{stop: true}
	}
	// The camera is the static object the tick advances by the clock, the other callees being the script timers,
	// the audio voices and the input, which the runtime drives itself.
	for _, c := range d.plainCallees(sc.Tick) {
		this := callers[c]
		if c == sc.Frame {
			continue
		}
		if this != 0 && this != sc.Renderer && absoluteGlobals(d, c)[rm.Clock.VA] {
			if sc.Camera != 0 {
				return fmt.Errorf("xbe: tick routine %08X advances the clock into two static objects (%08X, %08X)", sc.Tick, sc.CameraTick, c)
			}
			sc.Camera, sc.CameraTick = this, c
			continue
		}
		sc.TickHooks = append(sc.TickHooks, c)
	}
	if sc.Camera == 0 {
		return fmt.Errorf("xbe: tick routine %08X advances no static camera object", sc.Tick)
	}
	if err := sc.deriveMatrixStack(rm); err != nil {
		return err
	}
	if err := sc.deriveGroup(d, group.Slots[rm.Draw.Slot/4], group.Slots[sc.TickSlot/4], rm.Draw.Slot, sc.TickSlot); err != nil {
		return err
	}
	// Every class runs its natives out of the executable, the scene graph classes being required to summarize
	for class, fns := range sm.methodFunctions {
		if len(fns) > 0 {
			sc.Natives[class] = fns
		}
	}
	requiredNatives := map[string]bool{"Level": true, "Transform": true, "Background": true}
	if len(sc.Natives["Level"]) == 0 || len(sc.Natives["Transform"]) == 0 {
		return fmt.Errorf("xbe: the Level and Transform classes register no script natives")
	}
	fire, err := stringCallee(d, sc.Natives["Level"]["GoTo"])
	if err != nil {
		return err
	}
	sc.FireEvent = fire
	if err := sc.deriveArchive(d, types, rm.Draw.Slot); err != nil {
		return err
	}
	if err := sc.derivePathJoin(d, ex); err != nil {
		return err
	}

	// The base node tick runs the script engine, which the runtime drives itself.
	node := types["Node"]
	if node == nil || len(node.Slots) <= int(sc.TickSlot/4) {
		return fmt.Errorf("xbe: Node is not registered with a tick slot")
	}
	sc.ScriptTick = node.Slots[sc.TickSlot/4]
	// The image texture getter loads its file through helpers the runtime's own texture loading stands in for.
	image := types["ImageTexture"]
	if image == nil || len(image.Slots) <= int(rm.Draw.TextureSlot/4) {
		return fmt.Errorf("xbe: ImageTexture is not registered with a texture getter")
	}
	sc.ImageLoad = d.plainCallees(image.Slots[rm.Draw.TextureSlot/4])
	sc.Input = d.inputLayer()
	hooks := []uint32{rm.Draw.KindCheck, rm.Draw.MeshLoaded, rm.Accessor.Function, rm.shaderLookup, rm.Draw.MaterialLookup, rm.Wrapper, sc.Group.Draw, sc.Group.Tick, sc.ArrayElem, sc.FireEvent, sc.ScriptTick, sc.Loader, sc.Unloader, sc.SceneLoad.Reset, sc.SceneLoad.Join.VA, sc.SceneLoad.Populate, sc.SceneLoad.NewScope, sc.SceneLoad.Attach}
	hooks = append(hooks, sc.TickHooks...)
	hooks = append(hooks, sc.Input...)
	hooks = append(hooks, sc.ImageLoad...)
	hooks = append(hooks, d.plainCallees(rm.Draw.MeshLoaded)...)
	hooks = append(hooks, ms.Allocators...)
	hooks = append(hooks, ms.TextureProducers...)
	hooked := map[uint32]bool{}
	for _, h := range hooks {
		hooked[h] = true
	}
	// The frame presents through the XDK swap, which carries a signature in some builds and none in others
	for _, c := range d.plainCallees(sc.Frame) {
		if d.reaches(c, "D3DDevice_BlockUntilVerticalBlank", 1) {
			sc.Present = c
			hooked[c] = true
		}
	}
	for _, t := range d.Functions[sc.Frame].CallTargets {
		if fn := d.Functions[t]; fn != nil && fn.Symbol != nil && fn.Symbol.Name == "D3DDevice_Swap" {
			sc.Present = t
		}
	}
	if sc.Present == 0 {
		return fmt.Errorf("xbe: frame routine %08X presents through no callee", sc.Frame)
	}
	// Every function is summarized on its own with its callees kept as calls, the callees following in turn.
	var queue []sceneWork
	queued := map[uint32]bool{}
	staticCallees := map[uint32]uint32{}
	enqueue := func(va, this uint32, required bool, depth int) {
		if va == 0 || hooked[va] || queued[va] || ex.ftol[va] {
			return
		}
		// A math thunk runs as the operator it stands for, so it is never summarized
		if _, isMath := d.mathIntrinsic(va); isMath {
			return
		}
		// A method only a vtable names is disassembled when it is first queued
		if d.Functions[va] == nil {
			fn := d.buildFunction(va, map[uint32]bool{})
			if fn == nil {
				return
			}
			d.Functions[va] = fn
		}
		// A callee past the depth the chain is followed on the full budget is summarized on the small one
		if depth > sceneCalleeDepth {
			required = false
		}
		queued[va] = true
		queue = append(queue, sceneWork{va, this, required, depth})
	}
	summarize := func(w sceneWork) error {
		started := time.Now()
		if sceneTrace {
			fmt.Fprintf(os.Stderr, "scene %08X start\n", w.va)
		}
		defer func() {
			if sceneTrace {
				fmt.Fprintf(os.Stderr, "scene %08X depth %d required %v: %d insns %v\n", w.va, w.depth, w.required, len(d.Functions[w.va].Instructions), time.Since(started))
			}
		}()
		sub := NewExecutorShared(ex)
		// Routines the runtime enters directly and the required chain get the full path budget, other callees a small one.
		sub.PathLimit = 4096
		if w.depth > 0 && !w.required {
			sub.PathLimit = 256
		}
		if !w.required {
			sub.StepLimit = 20000
		}
		for h := range hooked {
			sub.Opaque[h] = true
		}
		for _, c := range d.plainCallees(w.va) {
			sub.Opaque[c] = true
		}
		// The routine sees as many stack arguments as its cleanup or its reads show, four at the least
		stackArgs := 4
		if n := d.cleanupOf(d.Functions[w.va]) / 4; n > stackArgs {
			stackArgs = n
		}
		if n := d.stackInputs(d.Functions[w.va]); n > stackArgs {
			stackArgs = n
		}
		paths, err := sub.Run(w.va, func(s *execState) {
			if w.this != 0 {
				ex.seedThis(s, w.va, w.this, stackArgs)
			} else {
				s.regs[1] = Ptr{"this", 0}
				sub.seedArgs(s, w.va, "arg", stackArgs)
			}
		})
		// Paths that left a loop early cover only its shortest runs, so such a routine runs block by block instead
		if err == nil && sub.loopCut && !w.required {
			err = fmt.Errorf("the paths leave a loop after %d turns", maxLoopUnroll)
		}
		// Paths multiplying through a loop repeat its body, so a summary past the budget runs block by block instead
		if n := pathEffects(paths); err == nil && n > maxPathEffects {
			err = fmt.Errorf("the paths carry %d effects, more than %d", n, maxPathEffects)
		}
		if err != nil {
			// A routine the paths cannot carry, its loops among them, runs block by block instead
			lifter := NewExecutorShared(ex)
			for h := range hooked {
				lifter.Opaque[h] = true
			}
			lifted, lerr := lifter.Lift(w.va)
			if lerr != nil {
				return fmt.Errorf("scene path %08X: %w; %v", w.va, err, lerr)
			}
			if sceneTrace {
				fmt.Fprintf(os.Stderr, "scene %08X lifted into %d blocks after: %v\n", w.va, len(lifted.blocks), err)
			}
			rm.Lifted[w.va] = lifted
			paths = lifted.asPaths()
		} else if _, done := rm.Draw.paths[w.va]; !done {
			rm.Draw.paths[w.va] = paths
			rm.Draw.Functions[w.va] = pathsJSON(rm.Exprs, paths)
		}
		// A helper the static object calls on itself is summarized on that object, its fields being globals.
		if w.this != 0 {
			for _, p := range paths {
				for _, e := range p.Effects {
					if e.Kind != "call" || !strings.HasPrefix(e.Fn, "sub_") {
						continue
					}
					if tp, ok := e.This.(Ptr); ok && tp.Base == "" && uint32(tp.Off) == w.this {
						var va uint32
						fmt.Sscanf(e.Fn, "sub_%X", &va)
						staticCallees[va] = w.this
					}
				}
			}
		}
		return nil
	}
	// The engine reaches the natives through a Node method, so every Node method is summarized
	for _, va := range node.Slots {
		enqueue(va, 0, false, 0)
	}
	enqueue(sc.Frame, sc.Renderer, true, 0)
	enqueue(sc.Tick, sc.Renderer, true, 0)
	enqueue(sc.Init, sc.Renderer, true, 0)
	enqueue(sc.Disk, 0, true, 0)
	enqueue(sc.MainLoad, sc.Renderer, true, 0)
	enqueue(sc.CameraTick, sc.Camera, true, 0)
	// The dashboard's own static initializers besides the class registrations run in the runtime before the
	// renderer starts, the C runtime's own being the runtime's business as the rest of that library is
	inits, err := d.staticInitializers()
	if err != nil {
		return err
	}
	for _, va := range inits {
		if d.callsWithin(va, registrar, 1) || d.Functions[va].Symbol != nil {
			continue
		}
		sc.Initializers = append(sc.Initializers, va)
		enqueue(va, 0, false, 0)
	}
	classes := make([]string, 0, len(sc.Natives))
	for class := range sc.Natives {
		classes = append(classes, class)
	}
	sort.Strings(classes)
	for _, class := range classes {
		fns := sc.Natives[class]
		names := make([]string, 0, len(fns))
		for name := range fns {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			enqueue(fns[name], 0, requiredNatives[class], 0)
		}
	}
	required := map[string]bool{}
	for _, c := range requiredSceneClasses {
		required[c] = true
	}
	names := make([]string, 0, len(types))
	for name := range types {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		nt := types[name]
		// The helpers a constructor keeps as calls run when the runtime constructs the class
		for _, p := range nt.Construct {
			for _, e := range p.Effects {
				if e.Kind != "call" {
					continue
				}
				var va uint32
				if scanSub(e.Fn, &va) {
					enqueue(va, 0, false, 0)
				}
			}
		}
		// A routine the constructor hands a library call as a callback is run by the runtime as well
		for _, p := range nt.ctorPaths {
			for _, e := range p.Effects {
				if e.Kind != "call" {
					continue
				}
				for _, arg := range e.Args {
					if c, ok := isConst(arg); ok && d.Functions[c] != nil && d.Functions[c].Symbol == nil && !d.inLibrary(c) {
						enqueue(c, 0, false, 0)
					}
				}
			}
		}
		if rootType(types, nt) != "Node" || nt.Vtable == 0 {
			continue
		}
		slots := SceneSlots{}
		if int(rm.Draw.Slot/4) < len(nt.Slots) {
			slots.Draw = nt.Slots[rm.Draw.Slot/4]
		}
		if int(sc.TickSlot/4) < len(nt.Slots) {
			slots.Tick = nt.Slots[sc.TickSlot/4]
		}
		if int(rm.Draw.FieldSetSlot/4) < len(nt.Slots) {
			slots.Hook = nt.Slots[rm.Draw.FieldSetSlot/4]
		}
		sc.Slots[name] = slots
		for _, va := range []uint32{slots.Draw, slots.Tick, slots.Hook} {
			enqueue(va, 0, required[name], 0)
		}
	}
	// A slot the paths dispatch on runs the implementation every class gives it, so each one is summarized too
	dispatched := map[uint32]bool{}
	enqueueSlot := func(slot uint32, depth int) {
		if slot%4 != 0 || dispatched[slot] {
			return
		}
		dispatched[slot] = true
		for _, name := range names {
			nt := types[name]
			if rootType(types, nt) != "Node" || nt.Vtable == 0 || int(slot/4) >= len(nt.Slots) {
				continue
			}
			enqueue(nt.Slots[slot/4], 0, false, depth)
		}
	}
	drain := func() error {
		for len(queue) > 0 {
			w := queue[0]
			queue = queue[1:]
			if !rm.summarized(w.va) {
				if err := summarize(w); err != nil {
					if w.required {
						return fmt.Errorf("xbe: %w", err)
					}
					sc.Errors[fmt.Sprintf("%08X", w.va)] = err.Error()
					continue
				}
			}
			for _, c := range d.plainCallees(w.va) {
				enqueue(c, staticCallees[c], w.required, w.depth+1)
			}
			// A routine the dashboard hands a library call as a callback is run by the runtime as well, and so is
			// every method of a class the routine builds an object of
			for _, p := range rm.pathsOf(w.va) {
				for _, e := range p.Effects {
					if e.Kind == "store" && e.Size == 4 {
						if c, ok := isConst(e.Value); ok && d.isVtable(c) {
							for _, m := range d.vtableSlots(c) {
								enqueue(m, 0, false, w.depth+1)
							}
						}
					}
					if e.Kind != "call" {
						continue
					}
					var slot uint32
					if n, _ := fmt.Sscanf(e.Fn, "vcall@%X", &slot); n == 1 {
						enqueueSlot(slot, w.depth+1)
					}
					for _, a := range append([]Expr{e.Value}, e.Args...) {
						c, ok := isConst(a)
						if !ok || d.Functions[c] == nil || d.Functions[c].Symbol != nil || d.inLibrary(c) {
							continue
						}
						enqueue(c, staticCallees[c], w.required, w.depth+1)
					}
				}
			}
		}
		return nil
	}
	if err := drain(); err != nil {
		return err
	}
	candidates := make([]uint32, 0, len(queued))
	for va := range queued {
		candidates = append(candidates, va)
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i] < candidates[j] })
	if err := sc.deriveTimers(d, ex, candidates); err != nil {
		return err
	}
	enqueue(sc.Timers.Walk, 0, true, 0)
	if err := drain(); err != nil {
		return err
	}
	if err := sc.deriveControl(rm, types, summarize); err != nil {
		return err
	}
	if err := sc.deriveScriptCalls(rm, d, node); err != nil {
		return err
	}
	// A class whose load creates a device texture its own routines fill runs that load from the image
	for _, nt := range types {
		nt.OwnTexture = d.createsOwnTexture(nt, rm.Draw.LoadSlot, rm.Draw.TextureSlot, sc.ImageLoad)
	}
	rm.deriveLibrary(d, ex, sc, types, hooked)
	resource, err := d.resourceLayout()
	if err != nil {
		return err
	}
	rm.Resource = resource
	rm.SurfaceDesc = SurfaceDescLayout{Format: surfaceDescFormat, Width: surfaceDescWidth, Height: surfaceDescHeight}
	gpu, err := d.gpuRegisterBase()
	if err != nil {
		return err
	}
	rm.GPU = gpu
	for _, paths := range rm.Draw.paths {
		for _, p := range paths {
			rm.collectStrings(d, p, rm.Draw.Strings)
		}
	}
	for _, lf := range rm.Lifted {
		for _, p := range lf.asPaths() {
			rm.collectStrings(d, p, rm.Draw.Strings)
		}
		lf.serialize(rm.Exprs)
	}
	for _, nt := range types {
		for _, p := range nt.ctorPaths {
			rm.collectStrings(d, p, rm.Draw.Strings)
		}
	}
	rm.Scene = sc
	return nil
}

// Reports whether a routine is carried in the model, as paths or as lifted blocks
func (rm *RenderModel) summarized(va uint32) bool {
	_, asPaths := rm.Draw.paths[va]
	_, asBlocks := rm.Lifted[va]
	return asPaths || asBlocks
}

// The paths of a routine, a lifted routine's blocks read as paths
func (rm *RenderModel) pathsOf(va uint32) []Path {
	if lf, ok := rm.Lifted[va]; ok {
		return lf.asPaths()
	}
	return rm.Draw.paths[va]
}

// The dispatcher is the Node method switching on a method record's signature code around a call through the
// record's pointer, its argument conversions being the virtual calls it reads a field of, the number value being
// the one object it builds and the string value the one the natives build
func (sc *SceneModel) deriveScriptCalls(rm *RenderModel, d *Disassembly, node *NodeType) error {
	for _, va := range node.Slots {
		lf, ok := rm.Lifted[va]
		if !ok {
			continue
		}
		switched, pointer := false, false
		for _, b := range lf.blocks {
			if b.term.kind == "switch" && len(b.term.targets) >= 8 {
				switched = true
			}
			for _, e := range b.effects {
				if e.Kind == "call" && e.Fn == pointerCallName {
					pointer = true
				}
			}
		}
		if !switched || !pointer {
			continue
		}
		if sc.Script.Call != 0 {
			return fmt.Errorf("xbe: Node methods %08X and %08X both dispatch script natives", sc.Script.Call, va)
		}
		sc.Script.Call = va
	}
	if sc.Script.Call == 0 {
		return fmt.Errorf("xbe: no Node method dispatches script natives")
	}
	effects := rm.Lifted[sc.Script.Call].allEffects()
	// The conversions are the virtual calls made on more than one value, a field of their results being read as a
	// float for the number and handed on for the string, the release reading nothing back
	slotOf := map[int]int32{}
	calls := map[int32]int{}
	for _, e := range effects {
		var slot int32
		if e.Kind == "call" && len(e.Fn) > 6 && e.Fn[:6] == "vcall@" {
			if n, _ := fmt.Sscanf(e.Fn, "vcall@%X", &slot); n == 1 {
				slotOf[e.Seq] = slot
				calls[slot]++
			}
		}
	}
	fields := map[int32]map[int32]bool{}
	floated := map[int32]bool{}
	for _, e := range rm.Lifted[sc.Script.Call].asPaths() {
		for _, x := range pathExprs(e) {
			mapExpr(x, func(e Expr) (Expr, bool) {
				if u, ok := e.(Un); ok && u.Op == "bits2f" {
					if l, ok := u.A.(Load); ok {
						if slot, ok := resultSlot(l, slotOf); ok {
							floated[slot] = true
						}
					}
				}
				if l, ok := e.(Load); ok {
					if slot, ok := resultSlot(l, slotOf); ok {
						if fields[slot] == nil {
							fields[slot] = map[int32]bool{}
						}
						fields[slot][l.Off] = true
					}
				}
				return nil, false
			})
		}
	}
	// A value is resolved through the slot whose result is compared with the value it was called on, in the
	// dispatcher itself or in the helper it hands each value to
	resolvers := map[int32]bool{}
	for _, va := range append([]uint32{sc.Script.Call}, d.plainCallees(sc.Script.Call)...) {
		for _, slot := range comparedSlots(rm.pathsOf(va)) {
			resolvers[slot] = true
		}
	}
	if len(resolvers) != 1 {
		return fmt.Errorf("xbe: the script dispatcher %08X resolves values through %d slots (%X)", sc.Script.Call, len(resolvers), slices.Sorted(maps.Keys(resolvers)))
	}
	sc.Script.Resolve = slices.Sorted(maps.Keys(resolvers))[0]
	for _, slot := range slices.Sorted(maps.Keys(calls)) {
		if slot == sc.Script.Resolve || calls[slot] < 2 {
			continue
		}
		read := slices.Sorted(maps.Keys(fields[slot]))
		switch {
		case len(read) == 0 && sc.Script.Release == 0:
			sc.Script.Release = slot
		case len(read) == 1 && floated[slot] && sc.Script.ToNumber == 0:
			sc.Script.ToNumber, sc.Script.NumberField = slot, read[0]
		case len(read) == 1 && !floated[slot] && sc.Script.ToString == 0:
			sc.Script.ToString, sc.Script.StringField = slot, read[0]
		default:
			return fmt.Errorf("xbe: the script dispatcher %08X converts values through slot %X in no known way (fields %v)", sc.Script.Call, slot, read)
		}
	}
	if sc.Script.Release == 0 || sc.Script.ToNumber == 0 || sc.Script.ToString == 0 {
		return fmt.Errorf("xbe: the script dispatcher %08X converts values through slots %X, %X and %X, expected a number, a string and a release", sc.Script.Call, sc.Script.ToNumber, sc.Script.ToString, sc.Script.Release)
	}
	// The number value is the one object the dispatcher builds, itself or through a helper, its vtable the one stored
	vtables := map[uint32]bool{}
	for _, e := range effects {
		if v, ok := storedVtable(d, e); ok {
			vtables[v] = true
		}
	}
	for _, c := range d.plainCallees(sc.Script.Call) {
		for _, p := range rm.pathsOf(c) {
			for _, e := range p.Effects {
				if v, ok := storedVtable(d, e); ok {
					vtables[v] = true
				}
			}
		}
	}
	if len(vtables) != 1 {
		return fmt.Errorf("xbe: the script dispatcher %08X builds values of %d classes (%X)", sc.Script.Call, len(vtables), slices.Sorted(maps.Keys(vtables)))
	}
	sc.Script.NumberVtable = slices.Sorted(maps.Keys(vtables))[0]
	// A native returning a string hands the object it allocated to the string value constructor, which keeps the
	// characters it copies in the field the dispatcher reads them from
	for _, fns := range sc.Natives {
		for _, va := range fns {
			for _, p := range rm.pathsOf(va) {
				for _, e := range p.Effects {
					var ctor uint32
					if e.Kind != "call" || !scanSub(e.Fn, &ctor) || !takesNewObject(e) {
						continue
					}
					vtable, field, ok := valueConstructor(rm.pathsOf(ctor), "operator_new_array")
					if !ok || !d.isVtable(vtable) || field != sc.Script.StringField {
						continue
					}
					if sc.Script.StringVtable != 0 && sc.Script.StringVtable != vtable {
						return fmt.Errorf("xbe: natives box strings as both %08X and %08X", sc.Script.StringVtable, vtable)
					}
					sc.Script.StringVtable = vtable
				}
			}
		}
	}
	if sc.Script.StringVtable == 0 {
		return fmt.Errorf("xbe: no native boxes a string through a constructor filling field +%X", sc.Script.StringField)
	}
	return nil
}

// The virtual call slots whose results a routine's guards compare with another value, each one reading nothing back
func comparedSlots(paths []Path) []int32 {
	slots := map[int32]bool{}
	for _, p := range paths {
		for _, g := range p.Guards {
			c, ok := g.(Cond)
			if !ok || (c.Op != "eq" && c.Op != "ne") {
				continue
			}
			for _, side := range []Expr{c.A, c.B} {
				var slot int32
				if cr, ok := side.(CallResult); ok && len(cr.Args) == 1 {
					if n, _ := fmt.Sscanf(cr.Fn, "vcall@%X", &slot); n == 1 {
						slots[slot] = true
					}
				}
			}
		}
	}
	return slices.Sorted(maps.Keys(slots))
}

// The conversion slot a load of a virtual call's result belongs to
func resultSlot(l Load, slotOf map[int]int32) (int32, bool) {
	var seq int
	if l.Addr != nil || !strings.HasPrefix(l.Base, "res") {
		return 0, false
	}
	if n, _ := fmt.Sscanf(l.Base, "res%d", &seq); n != 1 {
		return 0, false
	}
	slot, ok := slotOf[seq]
	return slot, ok
}

// Reports whether a call takes the object a plain allocation handed back, as its receiver or an argument
func takesNewObject(e Effect) bool {
	for _, a := range append([]Expr{e.This}, e.Args...) {
		if cr, ok := a.(CallResult); ok && cr.Fn == "operator_new" {
			return true
		}
	}
	return false
}

// The vtable a store effect writes into the start of an object, if it writes one
func storedVtable(d *Disassembly, e Effect) (uint32, bool) {
	if e.Kind != "store" || e.Size != 4 {
		return 0, false
	}
	c, ok := isConst(e.Value)
	if !ok || !d.isVtable(c) {
		return 0, false
	}
	return c, true
}

// Reports whether any path calls a library routine by name
func callsLibrary(paths []Path, name string) bool {
	for _, p := range paths {
		for _, e := range p.Effects {
			if e.Kind == "call" && e.Fn == name {
				return true
			}
		}
	}
	return false
}

// Reads the vtable a constructor stores at its object's start and the field it fills from an allocation
func valueConstructor(paths []Path, filledFrom string) (uint32, int32, bool) {
	var vtable uint32
	field, found := int32(-1), false
	for _, p := range paths {
		for _, e := range p.Effects {
			if e.Kind != "store" {
				continue
			}
			// The object arrives as an argument or as the receiver, a store into it being relative to either
			var base Expr = Sym{"this"}
			off := e.Off
			if e.Addr != nil {
				base, off = e.Addr, 0
				if b, ok := e.Addr.(Bin); ok && b.Op == "add" {
					if c, ok := isConst(b.B); ok {
						base, off = b.A, int32(c)
					}
				}
				if p, isThis := base.(Ptr); isThis && p.Base == "this" {
					base, off = Sym{"this"}, off+p.Off
				}
			} else if e.Base != "this" {
				continue
			}
			if _, isArg := base.(Sym); !isArg {
				continue
			}
			if c, ok := isConst(e.Value); ok && off == 0 && e.Size == 4 {
				vtable = c
				continue
			}
			if cr, ok := e.Value.(CallResult); ok && cr.Fn == filledFrom {
				field, found = off, true
			}
		}
	}
	return vtable, field, vtable != 0 && found
}

// The input target is the global the binder the Level tick hands its control field to stores that node in
func (sc *SceneModel) deriveControl(rm *RenderModel, types map[string]*NodeType, summarize func(sceneWork) error) error {
	off, ok := fieldOffset(types, "Level", "control")
	if !ok {
		return fmt.Errorf("xbe: Level registers no control field")
	}
	tick := sc.Slots["Level"].Tick
	control := Expr(Load{Base: "this", Off: off, Size: 4})
	helpers := map[uint32]bool{}
	for _, p := range rm.Draw.paths[tick] {
		for _, e := range p.Effects {
			var va uint32
			if e.Kind == "call" && len(e.Args) == 1 && sameExpr(e.Args[0], control) && scanSub(e.Fn, &va) {
				helpers[va] = true
			}
		}
	}
	if len(helpers) != 1 {
		return fmt.Errorf("xbe: Level tick %08X hands its control to %d helpers", tick, len(helpers))
	}
	helper := slices.Sorted(maps.Keys(helpers))[0]
	globals := map[uint32]bool{}
	binders := map[uint32]bool{}
	for _, p := range rm.Draw.paths[helper] {
		for _, e := range p.Effects {
			var va uint32
			switch {
			case e.Kind == "store" && e.Base == "" && e.Size == 4 && e.Value == Expr(Sym{"arg0"}):
				globals[uint32(e.Off)] = true
			case e.Kind == "call" && e.This == Expr(Sym{"arg0"}) && scanSub(e.Fn, &va):
				binders[va] = true
			}
		}
	}
	for _, va := range slices.Sorted(maps.Keys(binders)) {
		if _, done := rm.Draw.paths[va]; !done {
			if err := summarize(sceneWork{va: va, required: true}); err != nil {
				return fmt.Errorf("xbe: control binder: %w", err)
			}
		}
		for _, p := range rm.Draw.paths[va] {
			for _, e := range p.Effects {
				if e.Kind == "store" && e.Base == "" && e.Size == 4 && e.Value == Expr(Ptr{"this", 0}) {
					globals[uint32(e.Off)] = true
				}
			}
		}
	}
	if len(globals) != 1 {
		return fmt.Errorf("xbe: control helper %08X binds the control into %d globals", helper, len(globals))
	}
	sc.Control = slices.Sorted(maps.Keys(globals))[0]
	return nil
}

// The roles of the stack arguments a startup hands the main load, the renderer where it passes its own address
// and the path buffer elsewhere
func mainLoadArgs(d *Disassembly, startup *Function, mainLoad, renderer uint32, seed map[x86asm.Reg]uint32) []string {
	roles := []string{}
	for i := range startup.Instructions {
		in := &startup.Instructions[i]
		if in.Inst.Op != x86asm.CALL || resolveTarget(in) != mainLoad {
			continue
		}
		args := d.siteArgumentsFrom(startup, i, seed)
		for k, v := range args.stack {
			if args.known[k] && v == renderer {
				roles = append(roles, "renderer")
			} else {
				roles = append(roles, "buffer")
			}
		}
		break
	}
	return roles
}

// Reads the address out of a plain helper call name
func scanSub(fn string, va *uint32) bool {
	n, _ := fmt.Sscanf(fn, "sub_%X", va)
	return n == 1
}

// Finds a scene file field of a class by name, inheriting down the class chain
func fieldOffset(types map[string]*NodeType, class, name string) (int32, bool) {
	for nt := types[class]; nt != nil; nt = types[nt.Parent] {
		for off, f := range nt.Fields {
			if f.Name == name {
				return off, true
			}
		}
		if nt.Parent == "" {
			break
		}
	}
	return 0, false
}

// The timer advance is the tick hook that walks a global record list and frees the records it finishes
func (sc *SceneModel) deriveTimers(d *Disassembly, ex *Executor, candidates []uint32) error {
	for _, va := range sc.TickHooks {
		head, next := listShape(d, va)
		if head == 0 || next < 0 || !freesRecord(d, va) {
			continue
		}
		if sc.Timers.Advance != 0 {
			return fmt.Errorf("xbe: tick hooks %08X and %08X both walk a record list they free from", sc.Timers.Advance, va)
		}
		sc.Timers = TimerList{Advance: va, Head: head, Next: next}
	}
	if sc.Timers.Advance == 0 {
		return fmt.Errorf("xbe: no tick hook walks a record list it frees from")
	}
	entry, stop, err := walkBody(d, sc.Timers.Advance)
	if err != nil {
		return err
	}
	if d.Functions[entry] == nil {
		if d.Functions[entry] = d.buildFunction(entry, nil); d.Functions[entry] == nil {
			return fmt.Errorf("xbe: the walk body of %08X at %08X is not disassembled", sc.Timers.Advance, entry)
		}
	}
	if ex.Stop == nil {
		ex.Stop = map[uint32]bool{}
	}
	ex.Stop[stop] = true
	sc.Timers.Walk = entry
	// The remove helper is the other walk of that list, the field it tests being the node a record writes into
	for _, va := range candidates {
		head, _ := listShape(d, va)
		if va == sc.Timers.Advance || head != sc.Timers.Head || !freesRecord(d, va) {
			continue
		}
		var fields []int32
		for _, disp := range fieldDisplacements(d, va) {
			if disp != sc.Timers.Next && disp > 0 {
				fields = append(fields, disp)
			}
		}
		// A walk of the list reading several fields of its own is some other pass over it, not the unlink
		if len(fields) != 1 {
			continue
		}
		if sc.Timers.Remove != 0 {
			return fmt.Errorf("xbe: helpers %08X and %08X both unlink records of the timer list %08X", sc.Timers.Remove, va, sc.Timers.Head)
		}
		sc.Timers.Remove, sc.Timers.Node = va, fields[0]
	}
	if sc.Timers.Remove == 0 {
		return fmt.Errorf("xbe: no helper unlinks the records of a node from the timer list %08X", sc.Timers.Head)
	}
	return nil
}

// Reports whether a walk ends the life of the records it visits
func freesRecord(d *Disassembly, va uint32) bool {
	fn := d.Functions[va]
	if fn == nil {
		return false
	}
	for _, t := range fn.CallTargets {
		if c := d.Functions[t]; c != nil && c.Symbol != nil && c.Symbol.Name == "operator_delete" {
			return true
		}
	}
	return false
}

// The pass a list walk makes over one record, between the load its back edge returns to and the link test
func walkBody(d *Disassembly, va uint32) (uint32, uint32, error) {
	fn := d.Functions[va]
	if fn == nil {
		return 0, 0, fmt.Errorf("xbe: list walk %08X is not disassembled", va)
	}
	targets := map[uint32]bool{}
	back := -1
	for i := range fn.Instructions {
		in := &fn.Instructions[i]
		t := resolveTarget(in)
		if !isJump(in.Inst.Op) || t == 0 {
			continue
		}
		targets[t] = true
		if t >= in.VA {
			continue
		}
		if back >= 0 {
			return 0, 0, fmt.Errorf("xbe: list walk %08X loops back from both %08X and %08X", va, fn.Instructions[back].VA, in.VA)
		}
		back = i
	}
	if back < 0 {
		return 0, 0, fmt.Errorf("xbe: list walk %08X runs no loop over its records", va)
	}
	i := back
	for i > 0 && !targets[fn.Instructions[i].VA] {
		p := &fn.Instructions[i-1]
		if isJump(p.Inst.Op) || p.Inst.Op == x86asm.CALL || p.Inst.Op == x86asm.RET {
			break
		}
		i--
	}
	entry, stop := resolveTarget(&fn.Instructions[back]), fn.Instructions[i].VA
	if stop <= entry || !targets[stop] {
		return 0, 0, fmt.Errorf("xbe: list walk %08X tests its link at %08X, which is not a join below its body at %08X", va, stop, entry)
	}
	return entry, stop, nil
}

// The global a list walk starts from and the record field it follows to the next record
func listShape(d *Disassembly, va uint32) (uint32, int32) {
	head, next := uint32(0), int32(-1)
	fn := d.Functions[va]
	if fn == nil {
		return 0, -1
	}
	for i := range fn.Instructions {
		in := &fn.Instructions[i]
		if in.Inst.Op == x86asm.MOV && argCount(in) == 2 {
			if imm, ok := in.Inst.Args[1].(x86asm.Imm); ok {
				if sec := d.sectionOf(uint32(imm)); sec != nil && sec.Flags&0x1 != 0 {
					head = uint32(imm)
				}
			}
		}
		if in.Inst.Op == x86asm.LEA {
			if m, ok := in.Inst.Args[1].(x86asm.Mem); ok && m.Base != 0 && m.Index == 0 && m.Disp > 0 {
				next = int32(m.Disp)
			}
		}
	}
	return head, next
}

// The distinct offsets of the object fields a function addresses, leaving out its own frame
func fieldDisplacements(d *Disassembly, va uint32) []int32 {
	var out []int32
	fn := d.Functions[va]
	if fn == nil {
		return out
	}
	for i := range fn.Instructions {
		for _, a := range fn.Instructions[i].Inst.Args {
			m, ok := a.(x86asm.Mem)
			if !ok || m.Base == 0 || m.Base == x86asm.ESP || m.Base == x86asm.EBP || m.Index != 0 || m.Segment != 0 {
				continue
			}
			if !slices.Contains(out, int32(m.Disp)) {
				out = append(out, int32(m.Disp))
			}
		}
	}
	return out
}

// The absolute data addresses a function's instructions touch
func absoluteGlobals(d *Disassembly, va uint32) map[uint32]bool {
	out := map[uint32]bool{}
	fn := d.Functions[va]
	if fn == nil {
		return out
	}
	for i := range fn.Instructions {
		in := &fn.Instructions[i]
		for _, a := range in.Inst.Args {
			switch v := a.(type) {
			case x86asm.Mem:
				if v.Base == 0 && v.Index == 0 && d.sectionOf(uint32(v.Disp)) != nil && d.sectionOf(uint32(v.Disp)).Flags&0x1 != 0 {
					out[uint32(v.Disp)] = true
				}
			case x86asm.Imm:
				if in.Inst.Op == x86asm.MOV || in.Inst.Op == x86asm.PUSH {
					if sec := d.sectionOf(uint32(v)); sec != nil && sec.Flags&0x1 != 0 {
						out[uint32(v)] = true
					}
				}
			}
		}
	}
	return out
}

// The archive loader is the helper both the inline draw and the level switch call, the unloader the one the level
// deactivation tail jumps into after firing its event
func (sc *SceneModel) deriveArchive(d *Disassembly, types map[string]*NodeType, drawSlot int32) error {
	goTo := sc.Natives["Level"]["GoTo"]
	var levelSwitch, deactivate uint32
	// The switch fires the deactivation through a helper, or itself where the optimizer expanded that helper into it
	for _, c := range d.plainCallees(goTo) {
		if fn := d.Functions[c]; fn != nil && pushesWideString(d, fn, "OnDeactivate") {
			levelSwitch, deactivate = c, c
		}
		for _, cc := range d.plainCallees(c) {
			if fn := d.Functions[cc]; fn != nil && pushesWideString(d, fn, "OnDeactivate") {
				levelSwitch, deactivate = c, cc
			}
		}
	}
	if levelSwitch == 0 {
		return fmt.Errorf("xbe: Level.GoTo %08X calls no level switch helper that deactivates the current level", goTo)
	}
	inline := types["Inline"]
	if inline == nil || len(inline.Slots) <= int(drawSlot/4) {
		return fmt.Errorf("xbe: Inline is not registered with a draw slot")
	}
	inlineCallees := map[uint32]bool{}
	for _, c := range d.plainCallees(inline.Slots[drawSlot/4]) {
		inlineCallees[c] = true
		for _, cc := range d.plainCallees(c) {
			inlineCallees[cc] = true
		}
	}
	for _, c := range d.plainCallees(levelSwitch) {
		if inlineCallees[c] {
			sc.Loader = c
		}
	}
	if sc.Loader == 0 {
		return fmt.Errorf("xbe: level switch %08X shares no archive loader with the inline draw", levelSwitch)
	}
	unloaders := 0
	for _, c := range d.plainCallees(deactivate) {
		if c != sc.FireEvent && c != sc.Loader {
			sc.Unloader, unloaders = c, unloaders+1
		}
	}
	if unloaders != 1 {
		return fmt.Errorf("xbe: level deactivation %08X calls %d archive unloaders", deactivate, unloaders)
	}
	// The inline load names the archive by extension and its post load step fires the scene's initialize.
	for _, c := range d.plainCallees(inline.Slots[drawSlot/4]) {
		if fn := d.Functions[c]; fn != nil && pushesWideString(d, fn, ".xip") {
			sc.InlineLoad = c
		}
	}
	if sc.InlineLoad == 0 {
		return fmt.Errorf("xbe: Inline draw calls no archive load step")
	}
	for _, c := range d.plainCallees(sc.InlineLoad) {
		if fn := d.Functions[c]; fn != nil && pushesWideString(d, fn, "initialize") {
			sc.PostLoad = c
		}
	}
	if sc.PostLoad == 0 {
		return fmt.Errorf("xbe: inline load %08X calls no post load step", sc.InlineLoad)
	}
	var loadSteps, postSteps []uint32
	for _, c := range calleesInOrder(d, sc.InlineLoad) {
		if c != sc.Loader && c != sc.PostLoad {
			loadSteps = append(loadSteps, c)
		}
	}
	for _, c := range calleesInOrder(d, sc.PostLoad) {
		if c != sc.FireEvent {
			postSteps = append(postSteps, c)
		}
	}
	if len(loadSteps) != 2 || len(postSteps) != 3 {
		return fmt.Errorf("xbe: inline load %08X has %d helper steps and post load %08X has %d, expected 2 and 3", sc.InlineLoad, len(loadSteps), sc.PostLoad, len(postSteps))
	}
	sc.SceneLoad = SceneLoadHelpers{Reset: loadSteps[0], Resolve: loadSteps[1], Populate: postSteps[0], NewScope: postSteps[1], Attach: postSteps[2]}
	return nil
}

// The join is the helper the resolver calls twice, its inputs placed by which one each string call reads or fills
func (sc *SceneModel) derivePathJoin(d *Disassembly, ex *Executor) error {
	resolve := d.Functions[sc.SceneLoad.Resolve]
	calls := map[uint32]int{}
	for i := range resolve.Instructions {
		in := &resolve.Instructions[i]
		if in.Inst.Op != x86asm.CALL {
			continue
		}
		if t := resolveTarget(in); t != 0 && d.Functions[t] != nil && d.Functions[t].Symbol == nil {
			calls[t]++
		}
	}
	join := uint32(0)
	for _, va := range slices.Sorted(maps.Keys(calls)) {
		if calls[va] != 2 {
			continue
		}
		if join != 0 {
			return fmt.Errorf("xbe: resolver %08X calls both %08X and %08X twice", sc.SceneLoad.Resolve, join, va)
		}
		join = va
	}
	if join == 0 {
		return fmt.Errorf("xbe: resolver %08X calls no helper twice", sc.SceneLoad.Resolve)
	}
	regs := d.registerInputs(join)
	stackCount := d.stackInputs(d.Functions[join])
	inputs := 1 + len(regs) + stackCount
	sub := NewExecutorShared(ex)
	sub.PathLimit = 256
	sub.StepLimit = 20000
	for _, c := range d.plainCallees(join) {
		sub.Opaque[c] = true
	}
	paths, partial, _, _ := sub.RunWithRetries(join, func(s *execState) {
		s.regs[1] = Sym{"in0"}
		for i, r := range regs {
			s.setInput(r, Sym{fmt.Sprintf("in%d", 1+i)})
		}
		args := make([]Expr, stackCount)
		for i := range args {
			args[i] = Sym{fmt.Sprintf("in%d", 1+len(regs)+i)}
		}
		s.pushArgs(args...)
	}, 0)
	var effects []Effect
	for _, p := range paths {
		effects = append(effects, p.Effects...)
	}
	effects = append(effects, partial...)
	inputOf := func(e Expr) int {
		var n int
		if s, ok := e.(Sym); ok {
			if c, _ := fmt.Sscanf(s.Name, "in%d", &n); c == 1 {
				return n
			}
		}
		return -1
	}
	src, base, out := -1, -1, -1
	for _, e := range effects {
		if e.Kind != "call" || len(e.Args) < 2 {
			continue
		}
		first := inputOf(e.Args[0])
		switch {
		case e.Fn == "wcschr" && first >= 0 && src < 0:
			src = first
		case e.Fn == "wcschr" && first >= 0 && first != src && base < 0:
			base = first
		case e.Fn == "wcscpy" && first >= 0 && out < 0 && inputOf(e.Args[1]) >= 0:
			out = first
		}
	}
	if src < 0 || base < 0 || out < 0 || src == base || src == out || base == out || src >= inputs || base >= inputs || out >= inputs {
		return fmt.Errorf("xbe: join helper %08X reads its path, base and output in inputs %d, %d and %d of %d", join, src, base, out, inputs)
	}
	sc.SceneLoad.Join = PathJoin{VA: join, Src: src, Base: base, Out: out}
	return nil
}

// Direct non library callees of a function in call order, each once
func calleesInOrder(d *Disassembly, va uint32) []uint32 {
	var out []uint32
	seen := map[uint32]bool{}
	for i := range d.Functions[va].Instructions {
		in := &d.Functions[va].Instructions[i]
		if in.Inst.Op != x86asm.CALL {
			continue
		}
		t := resolveTarget(in)
		c := d.Functions[t]
		if t == 0 || c == nil || c.Symbol != nil || seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	return out
}

// Reports whether a function pushes the address of a wide string ending with a suffix
func pushesWideStringSuffix(d *Disassembly, fn *Function, suffix string) bool {
	for i := range fn.Instructions {
		in := &fn.Instructions[i]
		if in.Inst.Op != x86asm.PUSH {
			continue
		}
		if imm, ok := in.Inst.Args[0].(x86asm.Imm); ok {
			if s := d.Image.ReadWideString(uint32(imm)); s != "" && isPrintable(s) && strings.HasSuffix(strings.ToLower(s), suffix) {
				return true
			}
		}
	}
	return false
}

// Reports whether a function pushes the address of a given wide string
func pushesWideString(d *Disassembly, fn *Function, text string) bool {
	for i := range fn.Instructions {
		in := &fn.Instructions[i]
		if in.Inst.Op != x86asm.PUSH {
			continue
		}
		if imm, ok := in.Inst.Args[0].(x86asm.Imm); ok && d.Image.ReadWideString(uint32(imm)) == text {
			return true
		}
	}
	return false
}

// The matrix stack is the global object the world transform setup asks for its top, passed on the stack COM style
func (sc *SceneModel) deriveMatrixStack(rm *RenderModel) error {
	if rm.matrixStack != 0 {
		sc.MatrixStack = rm.matrixStack
		return nil
	}
	for _, p := range rm.nodePaths {
		for _, e := range p.Effects {
			if e.Kind != "call" || e.Fn != fmt.Sprintf("vcall@%X", rm.WorldSlot) {
				continue
			}
			for _, a := range e.Args {
				if l, ok := a.(Load); ok && l.Base == "" && l.Addr == nil && l.Size == 4 {
					sc.MatrixStack = uint32(l.Off)
					return nil
				}
				if va, ok := globalObjectAddress(a); ok {
					sc.MatrixStack = va
					return nil
				}
			}
		}
	}
	return fmt.Errorf("xbe: node constant setup reads the matrix stack top from no global")
}

// Reports whether a method run on a static object stores a value of the size into a global on any path
func storesGlobal(ex *Executor, va, this, global uint32, size int) bool {
	sub := NewExecutorShared(ex)
	paths, partial, _, _ := sub.RunWithRetries(va, func(s *execState) { ex.seedThis(s, va, this, 0) }, 6)
	var effects []Effect
	for _, p := range paths {
		effects = append(effects, p.Effects...)
	}
	effects = append(effects, partial...)
	for _, e := range effects {
		if e.Kind == "store" && e.Base == "" && uint32(e.Off) == global && e.Size == size {
			return true
		}
	}
	return false
}

// Lists the ascending displacements of the register relative indirect calls a function makes
func indirectCallSlots(d *Disassembly, va uint32) []int32 {
	fn := d.Functions[va]
	if fn == nil {
		return nil
	}
	slots := map[int32]bool{}
	for i := range fn.Instructions {
		in := &fn.Instructions[i]
		if in.Inst.Op != x86asm.CALL {
			continue
		}
		m, ok := in.Inst.Args[0].(x86asm.Mem)
		if !ok || m.Base == 0 || m.Index != 0 {
			continue
		}
		slots[int32(m.Disp)] = true
	}
	return slices.Sorted(maps.Keys(slots))
}

// The loop a merged frame routine runs each frame, from the setup above its back edge to the draw that ends it
func loopBody(d *Disassembly, va, frame uint32) (uint32, uint32, error) {
	fn := d.Functions[va]
	forward := map[uint32]bool{}
	var back, stop uint32
	for i := range fn.Instructions {
		in := &fn.Instructions[i]
		if in.Inst.Op == x86asm.CALL && resolveTarget(in) == frame {
			stop = in.VA
		}
		t := resolveTarget(in)
		if !isJump(in.Inst.Op) || t == 0 {
			continue
		}
		switch {
		case t > in.VA:
			forward[t] = true
		case isUnconditionalJump(in.Inst.Op):
			back = t
		}
	}
	if stop == 0 || back == 0 {
		return 0, 0, fmt.Errorf("xbe: merged frame routine %08X runs no loop around its draw %08X", va, frame)
	}
	i := fn.instructionIndex()[back]
	for i > 0 && !forward[fn.Instructions[i].VA] {
		p := &fn.Instructions[i-1]
		if isJump(p.Inst.Op) || p.Inst.Op == x86asm.CALL || p.Inst.Op == x86asm.RET {
			break
		}
		i--
	}
	return fn.Instructions[i].VA, stop, nil
}

// Returns the displacement of the one indirect register relative call in a function
func indirectCallSlot(d *Disassembly, va uint32) (int32, error) {
	if d.Functions[va] == nil {
		return 0, fmt.Errorf("xbe: function %08X is not disassembled", va)
	}
	slots := indirectCallSlots(d, va)
	if len(slots) == 0 {
		return 0, fmt.Errorf("xbe: function %08X makes no virtual call", va)
	}
	if len(slots) > 1 {
		return 0, fmt.Errorf("xbe: function %08X makes virtual calls through slots %d and %d", va, slots[0], slots[1])
	}
	return slots[0], nil
}

// The group draw walks its child array through an element accessor, testing a byte of each child before its virtual draw
func (sc *SceneModel) deriveGroup(d *Disassembly, drawVA, tickVA uint32, drawSlot, tickSlot int32) error {
	fn := d.Functions[drawVA]
	if fn == nil {
		return fmt.Errorf("xbe: Group draw %08X is not disassembled", drawVA)
	}
	children, visible := int32(-1), int32(-1)
	called := int32(-1)
	inlined := inlinedArrayIndexes(fn)
	for _, a := range inlined {
		children = a.off
	}
	for i := range fn.Instructions {
		in := &fn.Instructions[i]
		switch in.Inst.Op {
		case x86asm.LEA:
			if m, ok := in.Inst.Args[1].(x86asm.Mem); ok && m.Base == x86asm.ECX && m.Index == 0 {
				children = int32(m.Disp)
			}
		case x86asm.CALL:
			if m, ok := in.Inst.Args[0].(x86asm.Mem); ok && m.Base != 0 && m.Index == 0 {
				called = int32(m.Disp)
			} else if t := resolveTarget(in); t != 0 {
				if c := d.Functions[t]; c != nil && c.Symbol == nil {
					sc.ArrayElem = t
				}
			}
		}
		// The visible flag is the single byte of a child the draw reads before dispatching to it
		if in.Inst.MemBytes == 1 {
			for _, arg := range in.Inst.Args[:argCount(in)] {
				if m, ok := arg.(x86asm.Mem); ok && m.Base != 0 && m.Index == 0 {
					visible = int32(m.Disp)
				}
			}
		}
	}
	if sc.ArrayElem == 0 && len(inlined) == 0 {
		return fmt.Errorf("xbe: Group draw %08X neither calls nor inlines the child array accessor", drawVA)
	}
	if children < 0 || visible < 0 || called != drawSlot {
		return fmt.Errorf("xbe: Group draw %08X walks no child array (children %d visible %d slot %d)", drawVA, children, visible, called)
	}
	tickCall, err := indirectCallSlot(d, tickVA)
	if err != nil {
		return err
	}
	if tickCall != tickSlot {
		return fmt.Errorf("xbe: Group tick %08X calls slot %d, the frame tick calls slot %d", tickVA, tickCall, tickSlot)
	}
	sc.Group = GroupTraversal{Children: children, Visible: visible, Draw: drawVA, Tick: tickVA}
	return nil
}

// Finds the callee of a function that is handed a wide string pushed immediately before the call
func stringCallee(d *Disassembly, va uint32) (uint32, error) {
	fn := d.Functions[va]
	if fn == nil {
		return 0, fmt.Errorf("xbe: native %08X is not disassembled", va)
	}
	ins := fn.Instructions
	for i := range ins {
		if ins[i].Inst.Op != x86asm.CALL {
			continue
		}
		for j := i - 1; j >= 0 && j > i-6; j-- {
			if ins[j].Inst.Op == x86asm.CALL {
				break
			}
			if ins[j].Inst.Op != x86asm.PUSH {
				continue
			}
			imm, ok := ins[j].Inst.Args[0].(x86asm.Imm)
			if !ok {
				continue
			}
			if s := d.Image.ReadWideString(uint32(imm)); strings.HasPrefix(s, "On") {
				if t := resolveTarget(&ins[i]); t != 0 {
					return t, nil
				}
			}
		}
	}
	return 0, fmt.Errorf("xbe: native %08X fires no named script event", va)
}

// Reads the global a typed object pointer was loaded from
func globalObjectAddress(e Expr) (uint32, bool) {
	p, ok := e.(Ptr)
	if !ok || p.Off != 0 || len(p.Base) != 9 || p.Base[0] != 'g' {
		return 0, false
	}
	var va uint32
	if _, err := fmt.Sscanf(p.Base[1:], "%08X", &va); err != nil {
		return 0, false
	}
	return va, true
}
