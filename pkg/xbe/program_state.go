package xbe

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Library routines the replay reads device state through, each understood by what its own body stores
const (
	programStateNotInline = "D3DDevice_SetRenderStateNotInline"
	programStateSimple    = "D3DDevice_SetRenderState_Simple"
	programStatePrefix    = "D3DDevice_SetRenderState_"
	programStageNotInline = "D3DDevice_SetTextureStageStateNotInline"
	programStagePrefix    = "D3DDevice_SetTextureState_"
	programConstantPrefix = "D3DDevice_SetVertexShaderConstant"
	programInputPrefix    = "in"
	programRegisterBytes  = 16
	programDeviceLibrary  = "D3D8"
)

// programStates is where the library shadows render and stage states and the tables it dispatches an index through
type programStates struct {
	d                                    *Disassembly
	shadow                               uint32 // render state values by index
	methods                              uint32 // method token of each simple state
	setters                              uint32 // routine of each complex state
	simpleEnd                            uint32 // first index past the simple states
	complexAt                            uint32 // first complex index
	count                                uint32 // render states the setter table ends at
	stages                               uint32 // texture stage state values, a stage apart
	stageBytes                           uint32 // bytes one stage's states take
	wrapper                              uint32 // the executable's own render state wrapper, where the library's dispatcher is inlined
	wrapperState, wrapperValue           string
	deferredBase, deferredLo, deferredHi uint32
	complexSetters                       map[uint32]complexSetter
	slots                                map[uint32]*slotRoles
	roles                                map[uint32]*setterRoles
	defBytes                             map[uint32]int
}

// slotRoles is how a setter addresses a shadow, by scaled row and index inputs or a fixed slot
type slotRoles struct {
	row, index string
	slot       uint32
	value      string
}

// setterRoles is a setter's register, bias, data, extent or counted unit, and method and value inputs
type setterRoles struct {
	index, data   string
	bias          uint32
	bytes         int
	count         string
	unit          int
	method, value string
}

// The complete lifted paths of a library routine, callees left as calls, stack inputs named in order
func (st *programStates) setterPaths(fn *Function, stackWords int) ([]Path, error) {
	sub := NewExecutorShared(st.d.baseExecutor())
	for _, t := range fn.CallTargets {
		sub.Opaque[t] = true
	}
	paths, _, _, err := sub.RunWithRetries(fn.EntryVA, func(s *execState) { sub.seedArgs(s, fn.EntryVA, programInputPrefix, stackWords) }, programLiftTries)
	if len(paths) == 0 {
		return nil, fmt.Errorf("xbe: lifting %s completes no path: %v", fn.Symbol.Name, err)
	}
	return paths, nil
}

// The input an expression is, or is a constant added to
func biasedInput(e Expr) (string, uint32, bool) {
	if b, ok := e.(Bin); ok && b.Op == "add" {
		if c, isConst := isConst(b.B); isConst {
			role, isInput := inputRole(b.A)
			return role, c, isInput
		}
	}
	role, ok := inputRole(e)
	return role, 0, ok
}

// The bytes a path reads through an input, by the furthest load it makes from it
func inputExtent(p Path, role string) int {
	extent := 0
	for _, e := range p.Effects {
		for _, v := range append([]Expr{e.Value, e.Addr}, e.Args...) {
			WalkExpr(v, func(n Expr) {
				ld, ok := n.(Load)
				if !ok {
					return
				}
				if s := loadShape(ld); s.isTable(role, 1) && s.off >= 0 {
					extent = max(extent, int(s.off)+ld.Size)
				}
			})
		}
	}
	return extent
}

// Every effect across a routine's lifted paths
func liftedEffects(paths []Path) []Effect {
	var out []Effect
	for _, p := range paths {
		out = append(out, p.Effects...)
	}
	return out
}

// The input a symbol names, a stack word by its index or a register by its name
func inputRole(e Expr) (string, bool) {
	s, ok := e.(Sym)
	if !ok {
		return "", false
	}
	if strings.HasPrefix(s.Name, "reg.") {
		return s.Name, true
	}
	if n, err := strconv.Atoi(strings.TrimPrefix(s.Name, programInputPrefix)); err == nil && strings.HasPrefix(s.Name, programInputPrefix) {
		return fmt.Sprintf("stack:%d", n), true
	}
	return "", false
}

// The value a call hands an input role, read from the register or the stack word it names
func (pr *programRun) inputValue(role string) uint32 {
	if strings.HasPrefix(role, "reg.") {
		return pr.run.Regs[strings.TrimPrefix(role, "reg.")]
	}
	n, _ := strconv.Atoi(strings.TrimPrefix(role, "stack:"))
	return pr.run.ReadU32(pr.run.Regs["ESP"] + 4 + 4*uint32(n))
}

// addrShape is an address split into its constant, the inputs it scales and whatever else it adds
type addrShape struct {
	off    int64
	scaled map[string]int64
	others []string
}

// Splits an address into the terms it adds
func shapeOf(e Expr) addrShape {
	s := addrShape{scaled: map[string]int64{}}
	var walk func(e Expr)
	walk = func(e Expr) {
		switch v := e.(type) {
		case Bin:
			switch v.Op {
			case "add":
				walk(v.A)
				walk(v.B)
				return
			case "mul", "shl":
				if c, isConst := isConst(v.B); isConst {
					if v.Op == "shl" {
						c = 1 << c
					}
					if inner := shapeOf(v.A); len(inner.others) == 0 {
						s.off += inner.off * int64(c)
						for role, scale := range inner.scaled {
							s.scaled[role] += scale * int64(c)
						}
						return
					}
				}
			}
		case IConst:
			s.off += int64(int32(v.V))
			return
		case Sym:
			if role, ok := inputRole(v); ok {
				s.scaled[role] += 1
				return
			}
		}
		s.others = append(s.others, ExprString(e))
	}
	walk(e)
	sort.Strings(s.others)
	return s
}

// The shape of a store's address, a store through a named base keeping that base as its other term
func storeShape(e Effect) addrShape {
	if e.Addr == nil {
		s := addrShape{off: int64(e.Off), scaled: map[string]int64{}}
		if e.Base != "" {
			if role, ok := inputRole(Sym{e.Base}); ok {
				s.scaled[role] = 1
			} else {
				s.others = []string{e.Base}
			}
		}
		return s
	}
	s := shapeOf(e.Addr)
	s.off += int64(e.Off)
	return s
}

// The shape of a load's address
func loadShape(ld Load) addrShape {
	if ld.Addr == nil {
		s := addrShape{off: int64(ld.Off), scaled: map[string]int64{}}
		if ld.Base != "" {
			if role, ok := inputRole(Sym{ld.Base}); ok {
				s.scaled[role] = 1
			} else {
				s.others = []string{ld.Base}
			}
		}
		return s
	}
	s := shapeOf(ld.Addr)
	s.off += int64(ld.Off)
	return s
}

// Reports whether a shape is a constant plus one input scaled as given and nothing else
func (s addrShape) isTable(role string, scale int64) bool {
	return len(s.others) == 0 && len(s.scaled) == 1 && s.scaled[role] == scale
}

// Derives the state shadows, the method and setter tables and the index bounds from the two dispatchers, the render
// state tables from the executable's own wrapper where the library inlines its dispatcher
func deriveStateTables(d *Disassembly) (*programStates, error) {
	st := &programStates{d: d, slots: map[uint32]*slotRoles{}, roles: map[uint32]*setterRoles{}, defBytes: map[uint32]int{}}
	build := int(d.Image.LibBuild(programDeviceLibrary))
	sections := RenderStateSections(build)
	st.simpleEnd, st.complexAt, st.count = sections[1], sections[2], uint32(len(RenderStateNames(build)))
	if fn := d.Functions[d.symbolVA(programStateNotInline)]; fn != nil {
		if err := st.readStateDispatcher(fn); err != nil {
			return nil, err
		}
	} else if err := st.readStateWrapper(); err != nil {
		return nil, err
	}
	if stage := d.Functions[d.symbolVA(programStageNotInline)]; stage != nil {
		paths, err := st.setterPaths(stage, 3)
		if err != nil {
			return nil, err
		}
		for _, e := range liftedEffects(paths) {
			role, ok := inputRole(e.Value)
			if e.Kind != "store" || e.Size != 4 || !ok || role != "stack:2" {
				continue
			}
			s := storeShape(e)
			if len(s.others) == 0 && len(s.scaled) == 2 && s.scaled["stack:1"] == 4 && s.scaled["stack:0"] > 4 {
				st.stages, st.stageBytes = uint32(s.off), uint32(s.scaled["stack:0"])
			}
		}
	}
	if st.stages == 0 {
		// The stage table the texture coordinate index setter writes is the same table where the dispatcher is inlined
		layout, err := d.deviceState()
		if err != nil {
			return nil, fmt.Errorf("xbe: %s stores its value through no stage and state scaled address and %w", programStageNotInline, err)
		}
		st.stages, st.stageBytes = layout.textures.base, layout.textures.stride
	}
	return st, nil
}

// Reads the shadow, the method table, the setter table and the index bounds off the library's render state dispatcher
func (st *programStates) readStateDispatcher(fn *Function) error {
	paths, err := st.setterPaths(fn, 2)
	if err != nil {
		return err
	}
	index, value := "stack:0", "stack:1"
	bounds := map[uint32]bool{}
	for _, p := range paths {
		for _, g := range p.Guards {
			WalkExpr(g, func(n Expr) {
				c, ok := n.(Cond)
				if !ok {
					return
				}
				if role, isInput := inputRole(c.A); isInput && role == index {
					if bound, isConst := isConst(c.B); isConst {
						bounds[bound] = true
					}
				}
			})
		}
		for _, e := range p.Effects {
			switch e.Kind {
			case "store":
				if role, ok := inputRole(e.Value); ok && role == value && e.Size == 4 {
					if s := storeShape(e); s.isTable(index, 4) {
						st.shadow = uint32(s.off)
					}
				}
			case "call":
				if e.Fn == programStateSimple {
					for _, a := range e.Args {
						if ld, isLoad := a.(Load); isLoad {
							if s := loadShape(ld); s.isTable(index, 4) {
								st.methods = uint32(s.off)
							}
						}
					}
				}
				if e.Fn == pointerCallName && len(e.Args) > 0 {
					if ld, isLoad := e.Args[0].(Load); isLoad {
						if s := loadShape(ld); s.isTable(index, 4) {
							st.setters = uint32(s.off)
						}
					}
				}
			}
		}
	}
	if st.shadow == 0 || st.methods == 0 || st.setters == 0 || len(bounds) != 2 {
		return fmt.Errorf("xbe: %s shadows states at %08X, reads methods at %08X, dispatches setters at %08X and tests its index against %d bounds", programStateNotInline, st.shadow, st.methods, st.setters, len(bounds))
	}
	st.simpleEnd, st.complexAt = 0, 0
	for b := range bounds {
		if st.simpleEnd == 0 || b < st.simpleEnd {
			st.simpleEnd = b
		}
		if b > st.complexAt {
			st.complexAt = b
		}
	}
	for st.count = st.complexAt; ; st.count++ {
		va, ok := st.d.Image.ReadU32(st.setters + 4*st.count)
		if !ok || st.d.Functions[va] == nil || !st.d.inLibrary(va) {
			break
		}
	}
	return nil
}

// Reads the method table, the deferred state array and the complex setters off the executable's own render state
// wrapper, the dispatcher the link time optimizer expanded into it
func (st *programStates) readStateWrapper() error {
	wrapper, tables, counts, err := st.d.renderStateWrapper()
	if err != nil {
		return err
	}
	_ = counts
	st.wrapper, st.methods = wrapper, tables[wrapper]
	state, value, err := st.d.wrapperInputs(wrapper)
	if err != nil {
		return err
	}
	st.wrapperState, st.wrapperValue = roleOf(state), roleOf(value)
	lo, hi, array, err := st.d.deferredStateRange(wrapper, state, value)
	if err != nil {
		return err
	}
	st.deferredBase, st.deferredLo, st.deferredHi = array, lo, hi
	st.complexSetters = map[uint32]complexSetter{}
	for va, index := range st.d.wrapperSetters(wrapper) {
		fn := st.d.Functions[va]
		role, err := st.d.setterValueRole(fn)
		if err != nil {
			return fmt.Errorf("xbe: %s sets render state %d: %w", fn.Symbol.Name, index, err)
		}
		st.complexSetters[va] = complexSetter{index: index, value: role}
	}
	return nil
}

// The input a setter of one state takes its value in, the last of its signature's parameters or the one register it
// reads on entry where its signature names none
func (d *Disassembly) setterValueRole(fn *Function) (string, error) {
	params, err := d.libraryParams(fn)
	if err != nil {
		return "", err
	}
	if len(params) > 0 {
		role := params[len(params)-1]
		switch {
		case strings.HasPrefix(role, "stack"):
			return "stack:" + strings.TrimPrefix(role, "stack"), nil
		case role == "":
			return "", fmt.Errorf("the value is passed nowhere the callers record")
		}
		return "reg." + role, nil
	}
	regs := d.liveInRegisters(fn)
	if len(regs) == 1 {
		return "reg." + regs[0].String(), nil
	}
	if stack := d.stackInputs(fn); stack == 1 && len(regs) == 0 {
		return "stack:0", nil
	}
	return "", fmt.Errorf("the routine reads %d registers and %d stack words on entry", len(regs), d.stackInputs(fn))
}

// complexSetter is a named setter of one render state and the input it takes the value in
type complexSetter struct {
	index uint32
	value string
}

// The input role of an argument location, a register by name or a stack word by its index
func roleOf(loc argLocation) string {
	if loc.Reg != 0 {
		return "reg." + loc.Reg.String()
	}
	return fmt.Sprintf("stack:%d", loc.Stack)
}

// How a setter addresses a shadow, inputs scaled by 4 and the row stride or a fixed slot
func (st *programStates) shadowRoles(fn *Function, shadow, rowBytes, words uint32) (*slotRoles, error) {
	if roles, done := st.slots[fn.EntryVA]; done {
		return roles, nil
	}
	paths, err := st.setterPaths(fn, 3)
	if err != nil {
		return nil, err
	}
	for _, e := range liftedEffects(paths) {
		value, ok := inputRole(e.Value)
		if e.Kind != "store" || e.Size != 4 || !ok {
			continue
		}
		s := storeShape(e)
		if len(s.others) != 0 || s.off < int64(shadow) || s.off >= int64(shadow)+4*int64(words) || (s.off-int64(shadow))%4 != 0 {
			continue
		}
		roles := &slotRoles{value: value, slot: uint32(s.off-int64(shadow)) / 4}
		for role, scale := range s.scaled {
			switch {
			case rowBytes != 0 && scale == int64(rowBytes):
				roles.row = role
			case scale == 4:
				roles.index = role
			default:
				roles = nil
			}
			if roles == nil {
				break
			}
		}
		if roles == nil || (rowBytes != 0 && roles.row == "") || (roles.index != "" && roles.slot != 0) {
			continue
		}
		st.slots[fn.EntryVA] = roles
		return roles, nil
	}
	return nil, fmt.Errorf("xbe: %s stores an input in no slot of the shadow at %08X", fn.Symbol.Name, shadow)
}

// The roles a two word setter's body reveals, the input pushed first being the method and the next its value
func (st *programStates) pairRoles(fn *Function) (*setterRoles, error) {
	if roles, done := st.roles[fn.EntryVA]; done {
		return roles, nil
	}
	paths, err := st.setterPaths(fn, 2)
	if err != nil {
		return nil, err
	}
	type slot struct {
		role string
		off  int64
	}
	byBase := map[string][]slot{}
	for _, e := range liftedEffects(paths) {
		role, ok := inputRole(e.Value)
		if e.Kind != "store" || e.Size != 4 || !ok {
			continue
		}
		s := storeShape(e)
		if len(s.scaled) != 0 {
			continue
		}
		key := strings.Join(s.others, "+")
		byBase[key] = append(byBase[key], slot{role, s.off})
	}
	for _, slots := range byBase {
		for _, a := range slots {
			for _, b := range slots {
				if b.off == a.off+4 && a.role != b.role {
					roles := &setterRoles{method: a.role, value: b.role}
					st.roles[fn.EntryVA] = roles
					return roles, nil
				}
			}
		}
	}
	return nil, fmt.Errorf("xbe: %s pushes no method and value pair out of its inputs", fn.Symbol.Name)
}

// A constant setter's register input and bias, its data input and the fixed extent or counted unit it reads
func (st *programStates) constantRoles(fn *Function) (*setterRoles, error) {
	if roles, done := st.roles[fn.EntryVA]; done {
		return roles, nil
	}
	paths, err := st.setterPaths(fn, 3)
	if err != nil {
		return nil, err
	}
	var roles *setterRoles
	for _, p := range paths {
		r := &setterRoles{}
		for _, e := range p.Effects {
			switch e.Kind {
			case "store":
				if role, bias, ok := biasedInput(e.Value); ok && e.Size == 4 {
					if r.index != "" && (r.index != role || r.bias != bias) {
						return nil, fmt.Errorf("xbe: %s pushes both %s and %s as a register number", fn.Symbol.Name, r.index, role)
					}
					r.index, r.bias = role, bias
				}
			case "copy":
				if len(e.Args) == 0 {
					continue
				}
				src, count := shapeOf(e.Args[0]), shapeOf(e.Value)
				if len(src.others) != 0 || len(src.scaled) != 1 || len(count.others) != 0 || len(count.scaled) != 1 {
					continue
				}
				for role := range src.scaled {
					r.data = role
				}
				for role, scale := range count.scaled {
					r.count, r.unit = role, int(scale)*e.Size
				}
			}
		}
		for _, role := range []string{"reg.ECX", "reg.EDX", "stack:0", "stack:1", "stack:2"} {
			if role == r.index || role == r.count {
				continue
			}
			if n := inputExtent(p, role); n > r.bytes {
				r.data, r.bytes = role, n
			}
		}
		// A path that reads no input hands the work to a helper, so it says nothing about the roles
		if r.index == "" && r.data == "" && r.count == "" {
			continue
		}
		switch {
		case roles == nil:
			roles = r
		case roles.index != r.index || roles.bias != r.bias || roles.data != r.data || (roles.count != "" && r.count != "" && (roles.count != r.count || roles.unit != r.unit)):
			return nil, fmt.Errorf("xbe: %s reads its inputs differently on two paths, %+v and %+v", fn.Symbol.Name, *roles, *r)
		default:
			roles.bytes = max(roles.bytes, r.bytes)
			if roles.count == "" {
				roles.count, roles.unit = r.count, r.unit
			}
		}
	}
	if roles == nil {
		return nil, fmt.Errorf("xbe: %s reads no input on any path", fn.Symbol.Name)
	}
	fixed := roles.count == "" && roles.bytes > 0 && roles.bytes%programRegisterBytes == 0
	counted := roles.count != "" && roles.unit > 0 && roles.unit%programRegisterBytes == 0
	if roles.index == "" || roles.data == "" || roles.index == roles.data || !(fixed || counted) {
		return nil, fmt.Errorf("xbe: %s pushes register %q and reads data through %q, %d bytes or %q units of %d, which places no constants", fn.Symbol.Name, roles.index, roles.data, roles.bytes, roles.count, roles.unit)
	}
	st.roles[fn.EntryVA] = roles
	return roles, nil
}

// The bytes a creation reads out of its first input, by the furthest load
func (st *programStates) definitionBytes(fn *Function) (int, error) {
	if n, done := st.defBytes[fn.EntryVA]; done {
		return n, nil
	}
	paths, err := st.setterPaths(fn, 2)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, p := range paths {
		n = max(n, inputExtent(p, "stack:0"))
	}
	if n == 0 || n%4 != 0 {
		return 0, fmt.Errorf("xbe: %s reads %d bytes of the definition it is handed", fn.Symbol.Name, n)
	}
	st.defBytes[fn.EntryVA] = n
	return n, nil
}

// Answers a vertex shader constant setter, the registers it loads read from where the program points it
func (pr *programRun) setConstants(fn *Function, cleanup int) (uint32, bool) {
	roles, err := pr.states.constantRoles(fn)
	if err != nil {
		pr.run.Failed = err
		return ReplayReturn, true
	}
	register, data := pr.inputValue(roles.index)+roles.bias, pr.inputValue(roles.data)
	registers := roles.bytes / programRegisterBytes
	if roles.count != "" {
		registers = int(pr.inputValue(roles.count)) * roles.unit / programRegisterBytes
	}
	for i := 0; i < registers; i++ {
		var words []uint32
		for w := 0; w < programRegisterBytes/4; w++ {
			words = append(words, pr.run.ReadU32(data+uint32(i*programRegisterBytes+4*w)))
		}
		pr.constants[register+uint32(i)] = words
	}
	return pr.ret(cleanup, 0), true
}

// Answers a render state setter by writing its shadow slot, the simple setter's index found in the method table
func (pr *programRun) setState(fn *Function, name string, cleanup int) (uint32, bool) {
	var index, value uint32
	if name == programStateSimple {
		roles, err := pr.states.pairRoles(fn)
		if err != nil {
			pr.run.Failed = err
			return ReplayReturn, true
		}
		method, found := pr.inputValue(roles.method), false
		value = pr.inputValue(roles.value)
		for i := uint32(0); i < pr.states.simpleEnd; i++ {
			if m, ok := pr.d.Image.ReadU32(pr.states.methods + 4*i); ok && m == method {
				index, found = i, true
			}
		}
		if !found {
			pr.run.Failed = fmt.Errorf("xbe: the program sets method %08X through %s, which the library's method table lacks", method, name)
			return ReplayReturn, true
		}
	} else {
		roles, err := pr.states.shadowRoles(fn, pr.states.shadow, 0, pr.states.count)
		if err != nil {
			pr.run.Failed = err
			return ReplayReturn, true
		}
		index, value = roles.slot, pr.inputValue(roles.value)
		if roles.index != "" {
			index = pr.inputValue(roles.index)
		}
	}
	pr.run.WriteU32(pr.states.shadow+4*index, value)
	return pr.ret(cleanup, 0), true
}

// Answers a texture stage state setter by writing the stage's shadow slot as the library's own routine would
func (pr *programRun) setStage(fn *Function, cleanup int) (uint32, bool) {
	roles, err := pr.states.shadowRoles(fn, pr.states.stages, pr.states.stageBytes, textureStages*pr.states.stageBytes/4)
	if err != nil {
		pr.run.Failed = err
		return ReplayReturn, true
	}
	stage, slot, value := pr.inputValue(roles.row), roles.slot, pr.inputValue(roles.value)
	if roles.index != "" {
		slot = pr.inputValue(roles.index)
	}
	pr.run.WriteU32(pr.states.stages+stage*pr.states.stageBytes+4*slot, value)
	return pr.ret(cleanup, 0), true
}

// The render states and texture stage states the program has set, read out of the shadows the library keeps
func (pr *programRun) stateSnapshot() (map[string]uint32, map[uint32]map[string]uint32) {
	names := RenderStateNames(pr.build)
	states := map[string]uint32{}
	for i := uint32(0); i < pr.states.count; i++ {
		at := pr.states.shadow + 4*i
		if !pr.run.Touched(at, 4) {
			continue
		}
		name, named := names[i]
		if !named {
			name = fmt.Sprintf("state%d", i)
		}
		states[name] = pr.run.ReadU32(at)
	}
	stageNames := TextureStageStateNames(pr.build)
	stages := map[uint32]map[string]uint32{}
	for s := uint32(0); s < textureStages; s++ {
		for t := uint32(0); t < pr.states.stageBytes/4; t++ {
			at := pr.states.stages + s*pr.states.stageBytes + 4*t
			if !pr.run.Touched(at, 4) {
				continue
			}
			name, named := stageNames[t]
			if !named {
				name = fmt.Sprintf("state%d", t)
			}
			if stages[s] == nil {
				stages[s] = map[string]uint32{}
			}
			stages[s][name] = pr.run.ReadU32(at)
		}
	}
	return states, stages
}

// A copy of the constants set so far
func (pr *programRun) constantSnapshot() map[uint32][]uint32 {
	out := make(map[uint32][]uint32, len(pr.constants))
	for k, v := range pr.constants {
		out[k] = append([]uint32(nil), v...)
	}
	return out
}
