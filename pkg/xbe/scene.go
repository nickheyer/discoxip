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
	Renderer    uint32                       `json:"renderer"`
	Camera      uint32                       `json:"camera"`
	MatrixStack uint32                       `json:"matrix_stack"`
	Frame       uint32                       `json:"frame"`
	Tick        uint32                       `json:"tick"`
	Init        uint32                       `json:"init"`
	MainLoad    uint32                       `json:"main_load"`
	CameraTick  uint32                       `json:"camera_tick"`
	TickSlot    int32                        `json:"tick_slot"`
	Group       GroupTraversal               `json:"group"`
	Natives     map[string]map[string]uint32 `json:"natives"`
	FireEvent   uint32                       `json:"fire_event"`
	ArrayElem   uint32                       `json:"array_element"`
	Present     uint32                       `json:"present"`
	Loader      uint32                       `json:"loader"`
	ImageLoad   []uint32                     `json:"image_load"`
	Unloader    uint32                       `json:"unloader"`
	InlineLoad  uint32                       `json:"inline_load"`
	PostLoad    uint32                       `json:"post_load"`
	SceneLoad   SceneLoadHelpers             `json:"scene_load"`
	ScriptTick  uint32                       `json:"script_tick"`
	TickHooks   []uint32                     `json:"tick_hooks"`
	TimerHooks  []uint32                     `json:"timer_hooks"`
	Timers      TimerList                    `json:"timers"`
	Control     uint32                       `json:"control_global"`
	Slots       map[string]SceneSlots        `json:"slots"`
	Errors      map[string]string            `json:"errors"`
}

// SceneSlots are the per class methods the runtime runs, zero where the class inherits none.
type SceneSlots struct {
	Draw uint32 `json:"draw"`
	Tick uint32 `json:"tick"`
	Hook uint32 `json:"hook"`
}

// SceneLoadHelpers are the scene tree loader's steps the runtime performs itself when it builds an inline.
type SceneLoadHelpers struct {
	Reset    uint32 `json:"reset"`
	Resolve  uint32 `json:"resolve"`
	Populate uint32 `json:"populate"`
	NewScope uint32 `json:"new_scope"`
	Attach   uint32 `json:"attach"`
}

// TimerList is the executable's list of field interpolations the tick advances.
type TimerList struct {
	Advance uint32 `json:"advance"`
	Remove  uint32 `json:"remove"`
	Step    uint32 `json:"step"`
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

// Classes whose draw, tick and field hook must summarize for the dashboard scenes to render
var requiredSceneClasses = []string{"Node", "Group", "Transform", "Level", "Inline", "Layer", "Waver", "HUD", "Background", "Viewpoint", "NavigationInfo", "Screen", "Shape"}

type sceneWork struct {
	va       uint32
	this     uint32
	required bool
	depth    int
}

// DeriveScene summarizes the frame loop, the camera, the node class draw and tick methods and the natives that move the scene.
func (rm *RenderModel) deriveScene(d *Disassembly, ms *MaterialSystem, types map[string]*NodeType, sm *SystemModel) error {
	ex := NewExecutor(d)
	callers := ex.staticThisCallers()
	sc := &SceneModel{Natives: map[string]map[string]uint32{}, Slots: map[string]SceneSlots{}, Errors: map[string]string{}}
	sc.Renderer = callers[rm.Clock.Via]
	if sc.Renderer == 0 {
		return fmt.Errorf("xbe: clock writer %08X is not called on a static object", rm.Clock.Via)
	}
	// The tick is the renderer method that advances the clock and then dispatches a virtual call to the scene.
	for _, fn := range slices.Sorted(maps.Keys(callers)) {
		this := callers[fn]
		if this != sc.Renderer {
			continue
		}
		if d.reaches(fn, "D3DXMatrixPerspectiveFovLH", 0) && d.reaches(fn, "D3DDevice_Clear", 1) {
			sc.Frame = fn
		}
		if _, err := indirectCallSlot(d, fn); err != nil {
			continue
		}
		if storesGlobal(ex, fn, this, rm.Clock.VA, 8) {
			sc.Tick = fn
		}
	}
	if sc.Tick == 0 {
		return fmt.Errorf("xbe: no renderer method advances the clock %08X and ticks the scene", rm.Clock.VA)
	}
	for fn := range d.Functions {
		if d.reaches(fn, "Direct3D_CreateDevice", 0) {
			if sc.Init != 0 {
				return fmt.Errorf("xbe: two routines create the device (%08X, %08X)", sc.Init, fn)
			}
			sc.Init = fn
		}
	}
	if sc.Frame == 0 || sc.Init == 0 {
		return fmt.Errorf("xbe: renderer %08X has no frame (%08X) or init (%08X) routine", sc.Renderer, sc.Frame, sc.Init)
	}
	// The startup names the main scene file to the renderer method that loads it.
	for _, startup := range d.Functions[sc.Init].CalledBy {
		for _, c := range d.plainCallees(startup) {
			if fn := d.Functions[c]; fn != nil && pushesWideStringSuffix(d, fn, ".xap") {
				sc.MainLoad = c
			}
		}
	}
	if sc.MainLoad == 0 {
		return fmt.Errorf("xbe: no startup routine around init %08X names the main scene file", sc.Init)
	}
	// Besides the camera the tick advances the script timers and the audio voices, which the runtime drives itself.
	for _, c := range d.plainCallees(sc.Tick) {
		if this := callers[c]; this != 0 && this != sc.Renderer {
			sc.Camera, sc.CameraTick = this, c
		} else {
			sc.TickHooks = append(sc.TickHooks, c)
		}
	}
	if sc.Camera == 0 {
		return fmt.Errorf("xbe: tick routine %08X advances no static camera object", sc.Tick)
	}
	if err := sc.deriveMatrixStack(rm); err != nil {
		return err
	}
	slot, err := indirectCallSlot(d, sc.Tick)
	if err != nil {
		return err
	}
	sc.TickSlot = slot
	group := types["Group"]
	if group == nil || len(group.Slots) <= int(rm.Draw.Slot/4) || len(group.Slots) <= int(sc.TickSlot/4) {
		return fmt.Errorf("xbe: Group is not registered with draw and tick slots")
	}
	if err := sc.deriveGroup(d, group.Slots[rm.Draw.Slot/4], group.Slots[sc.TickSlot/4], rm.Draw.Slot, sc.TickSlot); err != nil {
		return err
	}
	for _, class := range []string{"Level", "Transform"} {
		fns := sm.methodFunctions[class]
		if len(fns) == 0 {
			return fmt.Errorf("xbe: class %s registers no script natives", class)
		}
		sc.Natives[class] = fns
	}
	fire, err := stringCallee(d, sc.Natives["Level"]["GoTo"])
	if err != nil {
		return err
	}
	sc.FireEvent = fire
	if err := sc.deriveArchive(d, types, rm.Draw.Slot); err != nil {
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
	hooks := []uint32{rm.Draw.KindCheck, rm.Draw.MeshLoaded, rm.Accessor.Function, rm.shaderLookup, rm.Draw.MaterialLookup, rm.Wrapper, sc.Group.Draw, sc.Group.Tick, sc.ArrayElem, sc.FireEvent, sc.ScriptTick, sc.Loader, sc.Unloader, sc.SceneLoad.Reset, sc.SceneLoad.Resolve, sc.SceneLoad.Populate, sc.SceneLoad.NewScope, sc.SceneLoad.Attach}
	hooks = append(hooks, sc.TickHooks...)
	hooks = append(hooks, sc.ImageLoad...)
	hooks = append(hooks, d.plainCallees(rm.Draw.MeshLoaded)...)
	hooks = append(hooks, ms.Allocators...)
	hooks = append(hooks, ms.TextureProducers...)
	hooked := map[uint32]bool{}
	for _, h := range hooks {
		hooked[h] = true
	}
	for _, c := range d.plainCallees(sc.Frame) {
		if d.reaches(c, "D3DDevice_BlockUntilVerticalBlank", 1) {
			sc.Present = c
			hooked[c] = true
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
		if va == 0 || hooked[va] || queued[va] || ex.ftol[va] || depth > sceneCalleeDepth {
			return
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
		paths, err := sub.Run(w.va, func(s *execState) {
			if w.this != 0 {
				s.regs[1] = Ptr{"", int32(w.this)}
			} else {
				s.regs[1] = Ptr{"this", 0}
			}
			s.pushArgs(Sym{"arg0"}, Sym{"arg1"}, Sym{"arg2"}, Sym{"arg3"})
		})
		if err != nil {
			return fmt.Errorf("scene path %08X: %w", w.va, err)
		}
		if _, done := rm.Draw.paths[w.va]; !done {
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
	enqueue(sc.Frame, sc.Renderer, true, 0)
	enqueue(sc.Tick, sc.Renderer, true, 0)
	enqueue(sc.Init, sc.Renderer, true, 0)
	enqueue(sc.MainLoad, sc.Renderer, true, 0)
	enqueue(sc.CameraTick, sc.Camera, true, 0)
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
			enqueue(fns[name], 0, true, 0)
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
				var va uint32
				if e.Kind == "call" && scanSub(e.Fn, &va) {
					enqueue(va, 0, false, 0)
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
		if name == "Text" {
			continue
		}
		for _, va := range []uint32{slots.Draw, slots.Tick, slots.Hook} {
			enqueue(va, 0, required[name], 0)
		}
	}
	for len(queue) > 0 {
		w := queue[0]
		queue = queue[1:]
		if _, done := rm.Draw.paths[w.va]; !done {
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
	}
	// Helpers sharing the timer list globals the tick hooks walk belong to the script timer subsystem.
	timerGlobals := map[uint32]bool{}
	for _, va := range sc.TickHooks {
		for g := range absoluteGlobals(d, va) {
			timerGlobals[g] = true
		}
	}
	for va := range queued {
		if _, done := rm.Draw.paths[va]; done {
			continue
		}
		for g := range absoluteGlobals(d, va) {
			if timerGlobals[g] {
				sc.TimerHooks = append(sc.TimerHooks, va)
				delete(sc.Errors, fmt.Sprintf("%08X", va))
				break
			}
		}
	}
	sort.Slice(sc.TimerHooks, func(i, j int) bool { return sc.TimerHooks[i] < sc.TimerHooks[j] })
	candidates := make([]uint32, 0, len(queued))
	for va := range queued {
		candidates = append(candidates, va)
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i] < candidates[j] })
	if err := sc.deriveTimers(d, candidates); err != nil {
		return err
	}
	if err := summarize(sceneWork{va: sc.Timers.Step, required: true}); err != nil {
		return fmt.Errorf("xbe: timer step: %w", err)
	}
	if err := sc.deriveControl(rm, types, summarize); err != nil {
		return err
	}
	for _, paths := range rm.Draw.paths {
		for _, p := range paths {
			rm.collectStrings(d, p, rm.Draw.Strings)
		}
	}
	for _, nt := range types {
		for _, p := range nt.ctorPaths {
			rm.collectStrings(d, p, rm.Draw.Strings)
		}
	}
	rm.Scene = sc
	return nil
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
			if e.Kind == "call" && len(e.Args) == 1 && e.Args[0] == control && scanSub(e.Fn, &va) {
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

// The timer advance is the tick hook that steps each record of a global list through one callee, the remove
// helper the one unlinking the records of a node
func (sc *SceneModel) deriveTimers(d *Disassembly, candidates []uint32) error {
	for _, va := range sc.TickHooks {
		callees := d.plainCallees(va)
		if len(callees) != 1 {
			continue
		}
		head, next := listShape(d, va)
		if head == 0 || next < 0 {
			continue
		}
		sc.Timers = TimerList{Advance: va, Step: callees[0], Head: head, Next: next}
	}
	if sc.Timers.Advance == 0 {
		return fmt.Errorf("xbe: no tick hook walks a record list through a step routine")
	}
	for _, va := range candidates {
		if va == sc.Timers.Advance {
			continue
		}
		head, _ := listShape(d, va)
		if head != sc.Timers.Head {
			continue
		}
		for _, disp := range loadedDisplacements(d, va) {
			if disp != sc.Timers.Next && disp > 0 {
				sc.Timers.Remove, sc.Timers.Node = va, disp
			}
		}
	}
	if sc.Timers.Remove == 0 {
		return fmt.Errorf("xbe: no helper unlinks the records of a node from the timer list %08X", sc.Timers.Head)
	}
	return nil
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

// Displacements of register relative loads in a function
func loadedDisplacements(d *Disassembly, va uint32) []int32 {
	var out []int32
	fn := d.Functions[va]
	if fn == nil {
		return out
	}
	for i := range fn.Instructions {
		in := &fn.Instructions[i]
		if in.Inst.Op != x86asm.MOV || argCount(in) != 2 {
			continue
		}
		if m, ok := in.Inst.Args[1].(x86asm.Mem); ok && m.Base != 0 && m.Index == 0 {
			out = append(out, int32(m.Disp))
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
	for _, c := range d.plainCallees(goTo) {
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
	for _, c := range d.plainCallees(deactivate) {
		if c != sc.FireEvent {
			sc.Unloader = c
		}
	}
	if sc.Unloader == 0 {
		return fmt.Errorf("xbe: level deactivation %08X tail calls no archive unloader", deactivate)
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

// The matrix stack is the global object the node constant setup asks for its top, passed on the stack COM style
func (sc *SceneModel) deriveMatrixStack(rm *RenderModel) error {
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
			}
		}
	}
	return fmt.Errorf("xbe: node constant setup reads the matrix stack top from no global")
}

// Reports whether a method run on a static object stores a value of the size into a global on any path
func storesGlobal(ex *Executor, va, this, global uint32, size int) bool {
	sub := NewExecutorShared(ex)
	paths, partial, _, _ := sub.RunWithRetries(va, func(s *execState) { s.regs[1] = Ptr{"", int32(this)} }, 6)
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

// Returns the displacement of the one indirect register relative call in a function
func indirectCallSlot(d *Disassembly, va uint32) (int32, error) {
	fn := d.Functions[va]
	if fn == nil {
		return 0, fmt.Errorf("xbe: function %08X is not disassembled", va)
	}
	slot := int32(-1)
	for i := range fn.Instructions {
		in := &fn.Instructions[i]
		if in.Inst.Op != x86asm.CALL {
			continue
		}
		m, ok := in.Inst.Args[0].(x86asm.Mem)
		if !ok || m.Base == 0 || m.Index != 0 {
			continue
		}
		if slot >= 0 && slot != int32(m.Disp) {
			return 0, fmt.Errorf("xbe: function %08X makes virtual calls through slots %d and %d", va, slot, m.Disp)
		}
		slot = int32(m.Disp)
	}
	if slot < 0 {
		return 0, fmt.Errorf("xbe: function %08X makes no virtual call", va)
	}
	return slot, nil
}

// The group draw walks its child array through an element accessor, testing a byte of each child before its virtual draw
func (sc *SceneModel) deriveGroup(d *Disassembly, drawVA, tickVA uint32, drawSlot, tickSlot int32) error {
	fn := d.Functions[drawVA]
	if fn == nil {
		return fmt.Errorf("xbe: Group draw %08X is not disassembled", drawVA)
	}
	children, visible := int32(-1), int32(-1)
	called := int32(-1)
	for i := range fn.Instructions {
		in := &fn.Instructions[i]
		switch in.Inst.Op {
		case x86asm.LEA:
			if m, ok := in.Inst.Args[1].(x86asm.Mem); ok && m.Base == x86asm.ECX && m.Index == 0 {
				children = int32(m.Disp)
			}
		case x86asm.CMP:
			if m, ok := in.Inst.Args[0].(x86asm.Mem); ok && m.Base != 0 && m.Index == 0 {
				if imm, ok := in.Inst.Args[1].(x86asm.Imm); ok && imm == 0 {
					visible = int32(m.Disp)
				}
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
	}
	if children < 0 || visible < 0 || called != drawSlot || sc.ArrayElem == 0 {
		return fmt.Errorf("xbe: Group draw %08X walks no child array (children %d visible %d slot %d accessor %08X)", drawVA, children, visible, called, sc.ArrayElem)
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
