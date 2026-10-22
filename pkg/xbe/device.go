package xbe

import (
	"fmt"
)

// deviceLayout is where the XDK keeps device state the dashboard's optimized code writes directly.
type deviceLayout struct {
	device    uint32 // global holding the device pointer
	transform uint32 // offset of the transform matrix array in the device
	stride    uint32 // bytes per transform matrix
	textures  *stateArray
	renders   *stateArray
}

// stateArray is a global table of device state words, indexed by stage and state or by state alone.
type stateArray struct {
	base   uint32
	stride uint32 // bytes per stage, zero for a single row
	count  uint32 // words per row
	rows   uint32
}

// Reports the row and word a constant address falls on inside the array
func (a *stateArray) locate(va uint32) (row, word uint32, ok bool) {
	if a == nil || va < a.base {
		return 0, 0, false
	}
	off := va - a.base
	if a.stride == 0 {
		if off%4 != 0 || off/4 >= a.count {
			return 0, 0, false
		}
		return 0, off / 4, true
	}
	row, rem := off/a.stride, off%a.stride
	if row >= a.rows || rem%4 != 0 || rem/4 >= a.count {
		return 0, 0, false
	}
	return row, rem / 4, true
}

// Derives the transform array layout by running the XDK transform setter on a symbolic state and matrix
func (d *Disassembly) deviceState() (*deviceLayout, error) {
	if d.layout != nil || d.layoutErr != nil {
		return d.layout, d.layoutErr
	}
	// Running the setter itself copies into the array, which must not be recognized while the layout is unknown.
	if d.layoutBusy {
		return nil, fmt.Errorf("xbe: device layout is being derived")
	}
	d.layoutBusy = true
	defer func() { d.layoutBusy = false }()
	var setter *Function
	for _, fn := range d.Functions {
		if fn.Symbol != nil && fn.Symbol.Name == "D3DDevice_SetTransform" {
			setter = fn
		}
	}
	if setter == nil {
		d.layoutErr = fmt.Errorf("xbe: the XDK transform setter is not identified")
		return nil, d.layoutErr
	}
	conv, err := d.convention(setter)
	if err != nil {
		d.layoutErr = err
		return nil, err
	}
	ex := NewExecutorShared(d.baseExecutor())
	ex.StepLimit = 20000
	ex.PathLimit = 64
	for _, t := range setter.CallTargets {
		ex.Opaque[t] = true
	}
	state := Sym{"in.state"}
	paths, partial, _, _ := ex.RunWithRetries(setter.EntryVA, func(s *execState) {
		params := conv.params
		if !conv.known {
			params = []argLocation{{Stack: 0}, {Stack: 1}}
		}
		args := symbolicArgs(conv.stackBytes/4, -1, nil)
		for i, loc := range params {
			var v Expr = state
			if i == 1 {
				v = Ptr{"mat", 0}
			}
			if loc.Reg != 0 {
				s.regs[regIndex[loc.Reg]] = v
			} else {
				args[loc.Stack] = v
			}
		}
		s.pushArgs(args...)
	}, 4)
	var effects []Effect
	for _, p := range paths {
		effects = append(effects, p.Effects...)
	}
	effects = append(effects, partial...)
	layout := &deviceLayout{}
	offsets := map[uint32]bool{}
	for _, e := range effects {
		if e.Kind != "store" || e.Addr == nil || e.Size != 4 {
			continue
		}
		l, ok := e.Value.(Load)
		if !ok || l.Base != "mat" {
			continue
		}
		device, off, stride, ok := transformAddress(e.Addr, state)
		if !ok {
			continue
		}
		if layout.device != 0 && (layout.device != device || layout.stride != stride) {
			d.layoutErr = fmt.Errorf("xbe: transform setter stores through several device layouts")
			return nil, d.layoutErr
		}
		layout.device, layout.stride = device, stride
		offsets[off-uint32(l.Off)] = true
	}
	if len(offsets) != 1 {
		d.layoutErr = fmt.Errorf("xbe: transform setter copies the matrix to %d distinct device offsets", len(offsets))
		return nil, d.layoutErr
	}
	for off := range offsets {
		layout.transform = off
	}
	textures, err := d.textureStateArray()
	if err != nil {
		d.layoutErr = err
		return nil, err
	}
	layout.textures = textures
	d.layout = layout
	return layout, nil
}

// Texture stages the hardware offers
const textureStages = 4

// Derives the texture stage state table from the XDK setter that writes the texture coordinate index of a stage
func (d *Disassembly) textureStateArray() (*stateArray, error) {
	var setter *Function
	for _, fn := range d.Functions {
		if fn.Symbol != nil && fn.Symbol.Name == "D3DDevice_SetTextureState_TexCoordIndex" {
			setter = fn
		}
	}
	if setter == nil {
		return nil, fmt.Errorf("xbe: the XDK texture coordinate index setter is not identified")
	}
	conv, err := d.convention(setter)
	if err != nil {
		return nil, err
	}
	stage, value := Sym{"in.stage"}, Sym{"in.value"}
	ex := NewExecutorShared(d.baseExecutor())
	ex.StepLimit = 20000
	ex.PathLimit = 64
	for _, t := range setter.CallTargets {
		ex.Opaque[t] = true
	}
	paths, partial, _, _ := ex.RunWithRetries(setter.EntryVA, func(s *execState) {
		params := conv.params
		if !conv.known {
			params = []argLocation{{Stack: 0}, {Stack: 1}}
		}
		args := symbolicArgs(conv.stackBytes/4, -1, nil)
		for i, loc := range params {
			var v Expr = stage
			if i == 1 {
				v = value
			}
			if loc.Reg != 0 {
				s.regs[regIndex[loc.Reg]] = v
			} else if !loc.None {
				args[loc.Stack] = v
			}
		}
		s.pushArgs(args...)
	}, 4)
	var effects []Effect
	for _, p := range paths {
		effects = append(effects, p.Effects...)
	}
	effects = append(effects, partial...)
	index, ok := textureStateIndex(int(d.Image.LibBuild("D3D8")), "TEXCOORDINDEX")
	if !ok {
		return nil, fmt.Errorf("xbe: the XDK %d texture stage table names no TEXCOORDINDEX state", d.Image.LibBuild("D3D8"))
	}
	for _, e := range effects {
		if e.Kind != "store" || e.Addr == nil || e.Size != 4 || e.Value != Expr(value) {
			continue
		}
		b, ok := e.Addr.(Bin)
		if !ok || b.Op != "add" {
			continue
		}
		c, isC := isConst(b.B)
		scaled, isBin := b.A.(Bin)
		if !isC {
			c, isC = isConst(b.A)
			scaled, isBin = b.B.(Bin)
		}
		if !isC || !isBin || scaled.A != Expr(stage) {
			continue
		}
		k, isK := isConst(scaled.B)
		if !isK {
			continue
		}
		stride := k
		if scaled.Op == "shl" {
			stride = 1 << k
		} else if scaled.Op != "mul" {
			continue
		}
		return &stateArray{base: c - 4*index, stride: stride, count: stride / 4, rows: textureStages}, nil
	}
	return nil, fmt.Errorf("xbe: the texture coordinate index setter stores its value into no stage indexed table")
}

// Finds the index of a texture stage state by name in the XDK table of a build
func textureStateIndex(build int, name string) (uint32, bool) {
	for idx, n := range TextureStageStateNames(build) {
		if n == name {
			return idx, true
		}
	}
	return 0, false
}

// Translates a store into a device state table into the deferred setter the dashboard inlined, if it lands in one
func (ex *Executor) stateStore(s *execState, va uint32, size int, v Expr, insnVA uint32) bool {
	layout, err := ex.d.deviceState()
	if err != nil || layout == nil || size != 4 {
		return false
	}
	if stage, state, ok := layout.textures.locate(va); ok {
		ex.seq++
		s.effects = append(s.effects, Effect{Kind: "call", VA: insnVA, Fn: "D3DDevice_SetTextureState_Deferred", Args: []Expr{IConst{stage}, IConst{state}, v}, Seq: ex.seq})
		return true
	}
	if _, state, ok := layout.renders.locate(va); ok {
		ex.seq++
		s.effects = append(s.effects, Effect{Kind: "call", VA: insnVA, Fn: "D3DDevice_SetRenderState_Deferred", Args: []Expr{IConst{state}, v}, Seq: ex.seq})
		return true
	}
	return false
}

// Splits a device transform element address into the device global, the constant offset and the state's scale
func transformAddress(addr Expr, state Sym) (device, off, stride uint32, ok bool) {
	var terms []Expr
	var flatten func(e Expr)
	flatten = func(e Expr) {
		if b, isBin := e.(Bin); isBin && b.Op == "add" {
			flatten(b.A)
			flatten(b.B)
			return
		}
		terms = append(terms, e)
	}
	flatten(addr)
	for _, t := range terms {
		switch v := t.(type) {
		case IConst:
			off += v.V
		case Load:
			if v.Base == "" && v.Addr == nil && v.Size == 4 && device == 0 {
				device = uint32(v.Off)
				continue
			}
			return 0, 0, 0, false
		case Ptr:
			// A device pointer whose class is known arrives as a typed object pointer.
			if g, ok := globalObjectAddress(Ptr{v.Base, 0}); ok && device == 0 {
				device = g
				off += uint32(v.Off)
				continue
			}
			return 0, 0, 0, false
		case Bin:
			c, isC := isConst(v.B)
			if !isC || v.A != Expr(state) {
				return 0, 0, 0, false
			}
			switch v.Op {
			case "shl":
				stride = 1 << c
			case "mul":
				stride = c
			default:
				return 0, 0, 0, false
			}
		default:
			return 0, 0, 0, false
		}
	}
	return device, off, stride, device != 0 && stride != 0
}

// Recognizes a copy into one of the device's transform matrices, returning the transform state it sets
func (ex *Executor) transformCopy(s *execState, dst Expr, bytes uint32) (uint32, bool) {
	layout, err := ex.d.deviceState()
	if err != nil || layout == nil || bytes != layout.stride {
		return 0, false
	}
	var terms []Expr
	var flatten func(e Expr)
	flatten = func(e Expr) {
		if b, isBin := e.(Bin); isBin && b.Op == "add" {
			flatten(b.A)
			flatten(b.B)
			return
		}
		terms = append(terms, e)
	}
	flatten(dst)
	var off uint32
	device := false
	for _, t := range terms {
		switch v := t.(type) {
		case IConst:
			off += v.V
		case Load:
			if v.Base == "" && v.Addr == nil && v.Size == 4 && uint32(v.Off) == layout.device {
				device = true
				continue
			}
			return 0, false
		case Ptr:
			if g, ok := globalObjectAddress(Ptr{v.Base, 0}); ok && g == layout.device {
				device = true
				off += uint32(v.Off)
				continue
			}
			return 0, false
		default:
			return 0, false
		}
	}
	if !device || off < layout.transform || (off-layout.transform)%layout.stride != 0 {
		return 0, false
	}
	return (off - layout.transform) / layout.stride, true
}

// Derives the bias a vertex shader constant setter adds to its register before loading the hardware constant
func (d *Disassembly) constantBias(fn *Function) (int32, error) {
	if d.biases == nil {
		d.biases = map[uint32]int32{}
	}
	if b, done := d.biases[fn.EntryVA]; done {
		return b, nil
	}
	conv, err := d.convention(fn)
	if err != nil {
		return 0, err
	}
	ex := NewExecutorShared(d.baseExecutor())
	ex.StepLimit = 20000
	ex.PathLimit = 64
	for _, t := range fn.CallTargets {
		ex.Opaque[t] = true
	}
	reg := Sym{"in.register"}
	paths, partial, _, _ := ex.RunWithRetries(fn.EntryVA, func(s *execState) {
		params := conv.params
		if !conv.known {
			if fn.Symbol.FastCall {
				s.regs[1] = reg
				s.regs[2] = Ptr{"data", 0}
				s.pushArgs(symbolicArgs(conv.stackBytes/4, -1, nil)...)
				return
			}
			params = []argLocation{{Stack: 0}, {Stack: 1}, {Stack: 2}}
		}
		args := symbolicArgs(conv.stackBytes/4, -1, nil)
		for i, loc := range params {
			// Any further parameter is a row count, kept concrete so the copy loop runs.
			var v Expr = IConst{4}
			switch i {
			case 0:
				v = reg
			case 1:
				v = Ptr{"data", 0}
			}
			if loc.Reg != 0 {
				s.regs[regIndex[loc.Reg]] = v
			} else if loc.Stack < len(args) {
				args[loc.Stack] = v
			}
		}
		s.pushArgs(args...)
	}, 4)
	var effects []Effect
	for _, p := range paths {
		effects = append(effects, p.Effects...)
	}
	effects = append(effects, partial...)
	biases := map[int32]bool{}
	for _, e := range effects {
		if e.Kind != "store" || e.Value == nil {
			continue
		}
		if e.Value == Expr(reg) {
			biases[0] = true
		}
		if b, ok := e.Value.(Bin); ok && b.Op == "add" && b.A == Expr(reg) {
			if c, ok := isConst(b.B); ok {
				biases[int32(c)] = true
			}
		}
	}
	if len(biases) != 1 {
		return 0, fmt.Errorf("xbe: %s stores its register with %d distinct biases", fn.Symbol.Name, len(biases))
	}
	for b := range biases {
		d.biases[fn.EntryVA] = b
		return b, nil
	}
	return 0, nil
}
