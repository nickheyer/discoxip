package xbe

import (
	"fmt"
	"slices"
	"strings"

	"golang.org/x/arch/x86/x86asm"
)

// XDK routines the runtime's GPU runs from the image to read the textures the executable creates
var gpuRoutines = []string{"D3DTexture_GetLevelDesc", "D3DTexture_GetSurfaceLevel", "D3DSurface_GetDesc", "D3DResource_Release", "XGBytesPerPixelFromFormat", "XGIsSwizzledFormat", "XGUnswizzleRect"}

// The miniport routine mapping the GPU's registers and the routine finding a texture level's data
const mapRegisters, findSurface = "CMiniport::MapRegisters", "PixelJar::FindSurfaceWithinTexture"

// Where a D3D resource header keeps its data and the bit reaching that data from the processor
type ResourceLayout struct {
	Data  int32  `json:"data"`
	Alias uint32 `json:"alias"`
}

// Where a surface description holds the format, width and height a texture reader needs
type SurfaceDescLayout struct {
	Format int32 `json:"format"`
	Width  int32 `json:"width"`
	Height int32 `json:"height"`
}

// Lifts every library routine the runtime's routines reach so it runs the image's code wherever it has no stand-in
func (rm *RenderModel) deriveLibrary(d *Disassembly, ex *Executor, sc *SceneModel, types map[string]*NodeType, hooked map[uint32]bool) {
	named := map[string]uint32{}
	ambiguous := map[string]bool{}
	for _, va := range d.functionVAs() {
		fn := d.Functions[va]
		if fn.Symbol == nil {
			continue
		}
		if prev, dup := named[fn.Symbol.Name]; dup && prev != va {
			ambiguous[fn.Symbol.Name] = true
			continue
		}
		named[fn.Symbol.Name] = va
	}
	// Every named routine is known by name, so a pointer call reaching one runs as a named call
	rm.Library = map[uint32]string{}
	for _, va := range d.functionVAs() {
		if fn := d.Functions[va]; fn.Symbol != nil {
			rm.Library[va] = fn.Symbol.Name
		}
	}
	var pending []uint32
	seen := map[uint32]bool{}
	nameOf := map[uint32]string{}
	visitName := func(name string) {
		va, ok := named[name]
		if !ok || seen[va] || hooked[va] {
			return
		}
		seen[va] = true
		if ambiguous[name] {
			sc.Errors[name] = fmt.Sprintf("%s names more than one routine of the image", name)
			return
		}
		if !rm.summarized(va) {
			nameOf[va] = name
			pending = append(pending, va)
		}
	}
	visitSub := func(va uint32) {
		if seen[va] || hooked[va] || rm.summarized(va) || d.Functions[va] == nil {
			return
		}
		seen[va] = true
		pending = append(pending, va)
	}
	scan := func(paths []Path, helpers bool) {
		for _, p := range paths {
			for _, e := range p.Effects {
				if e.Kind != "call" || strings.HasPrefix(e.Fn, "vcall@") || e.Fn == pointerCallName {
					continue
				}
				var va uint32
				if scanSub(e.Fn, &va) {
					if helpers {
						visitSub(va)
					}
					continue
				}
				// A named call carries its target, which tells apart routines the image gives one name
				if e.Target != 0 {
					visitSub(e.Target)
					continue
				}
				visitName(e.Fn)
			}
		}
	}
	for _, paths := range rm.Draw.paths {
		scan(paths, false)
	}
	for _, lf := range rm.Lifted {
		scan(lf.asPaths(), false)
	}
	for _, nt := range types {
		scan(nt.ctorPaths, false)
	}
	for _, m := range rm.Materials {
		scan(m.Paths, false)
	}
	scan(rm.defaultPaths, false)
	for _, name := range gpuRoutines {
		visitName(name)
	}
	for len(pending) > 0 {
		va := pending[0]
		pending = pending[1:]
		fn := d.Functions[va]
		lifter := NewExecutorShared(ex)
		for h := range hooked {
			lifter.Opaque[h] = true
		}
		lf, err := lifter.Lift(va)
		if err == nil && (fn.Symbol != nil || d.inLibrary(va)) {
			lf.Params, err = d.libraryParams(fn)
		}
		if err != nil {
			sc.Errors[fmt.Sprintf("%08X", va)] = err.Error()
			if name, ok := nameOf[va]; ok {
				sc.Errors[name] = err.Error()
			}
			continue
		}
		rm.Lifted[va] = lf
		scan(lf.asPaths(), true)
	}
}

// Where a library routine takes each argument its callers record, failing on a register no caller passes
func (d *Disassembly) libraryParams(fn *Function) ([]string, error) {
	conv, err := d.convention(fn)
	if err != nil {
		return nil, err
	}
	carried := map[x86asm.Reg]bool{}
	var params []string
	switch {
	case conv.known:
		for _, loc := range conv.params {
			switch {
			case loc.None:
				params = append(params, "")
			case loc.Reg != 0:
				params = append(params, registerParam(loc.Reg))
				carried[loc.Reg] = true
			default:
				params = append(params, fmt.Sprintf("stack%d", loc.Stack))
			}
		}
	case fn.Symbol != nil && fn.Symbol.FastCall:
		params = []string{"ECX", "EDX"}
		carried[x86asm.ECX], carried[x86asm.EDX] = true, true
	default:
		// The stack arguments come in order and the caller records ecx as the receiver
		carried[x86asm.ECX] = true
	}
	for _, r := range append(append([]x86asm.Reg{}, d.liveInRegisters(fn)...), d.liveInFloatRegisters(fn)...) {
		if !carried[r] {
			return nil, fmt.Errorf("%s at %08X reads %s on entry, which no caller passes", fn.Name, fn.EntryVA, r)
		}
	}
	return params, nil
}

// The name a register argument takes in the lifted routine's parameters
func registerParam(r x86asm.Reg) string {
	if i, ok := xmmIndex(r); ok {
		return fmt.Sprintf("XMM%d", i)
	}
	return r.String()
}

// Reports whether a named library routine takes a receiver in ecx beside its signature's arguments
func (d *Disassembly) readsReceiver(fn *Function) bool {
	if fn.Symbol == nil || fn.Symbol.FastCall {
		return false
	}
	for _, r := range d.liveInRegisters(fn) {
		if r == x86asm.ECX {
			return true
		}
	}
	return false
}

// Reports whether a class's load creates a device texture no image loader of the runtime fills
func (d *Disassembly) createsOwnTexture(nt *NodeType, loadSlot, textureSlot int32, imageLoad []uint32) bool {
	if int(loadSlot/4) >= len(nt.Slots) || int(textureSlot/4) >= len(nt.Slots) {
		return false
	}
	load, texture := nt.Slots[loadSlot/4], nt.Slots[textureSlot/4]
	creates := false
	for name := range textureCreations {
		creates = creates || d.reachesSymbol(load, name, map[uint32]bool{})
	}
	if !creates {
		return false
	}
	for _, c := range d.plainCallees(texture) {
		if slices.Contains(imageLoad, c) {
			return false
		}
	}
	return true
}

// Reports whether a routine calls a library symbol directly or through unnamed routines
func (d *Disassembly) reachesSymbol(va uint32, symbol string, seen map[uint32]bool) bool {
	fn := d.Functions[va]
	if fn == nil || seen[va] {
		return false
	}
	seen[va] = true
	for _, t := range fn.CallTargets {
		c := d.Functions[t]
		if c == nil {
			continue
		}
		if c.Symbol != nil && c.Symbol.Name == symbol {
			return true
		}
		if c.Symbol == nil && d.reachesSymbol(t, symbol, seen) {
			return true
		}
	}
	return false
}

// The address the executable's miniport maps the GPU's registers at, read off its register mapping
func (d *Disassembly) gpuRegisterBase() (uint32, error) {
	va := d.symbolVA(mapRegisters)
	if va == 0 {
		return 0, fmt.Errorf("xbe: the miniport's register mapping %s is not identified", mapRegisters)
	}
	for _, in := range d.Functions[va].Instructions {
		if in.Inst.Op != x86asm.MOV || argCount(&in) != 2 || in.Inst.MemBytes != 4 {
			continue
		}
		m, isMem := in.Inst.Args[0].(x86asm.Mem)
		v, isImm := in.Inst.Args[1].(x86asm.Imm)
		if isMem && isImm && m.Base != 0 && m.Index == 0 {
			return uint32(v), nil
		}
	}
	return 0, fmt.Errorf("xbe: %s at %08X stores no register base", mapRegisters, va)
}

// The data word and alias bit the surface lookup reads a texture's data through
func (d *Disassembly) resourceLayout() (*ResourceLayout, error) {
	va := d.symbolVA(findSurface)
	if va == 0 {
		return nil, fmt.Errorf("xbe: the surface lookup %s is not identified", findSurface)
	}
	routines := []uint32{va}
	routines = append(routines, d.Functions[va].CallTargets...)
	for _, r := range routines {
		fn := d.Functions[r]
		if fn == nil {
			continue
		}
		loaded := map[x86asm.Reg]int32{}
		for i := range fn.Instructions {
			in := &fn.Instructions[i]
			switch in.Inst.Op {
			case x86asm.MOV:
				dst, isReg := in.Inst.Args[0].(x86asm.Reg)
				m, isMem := in.Inst.Args[1].(x86asm.Mem)
				if isReg && isMem && m.Base != 0 && m.Base != x86asm.ESP && m.Index == 0 && in.Inst.MemBytes == 4 {
					loaded[dst] = int32(m.Disp)
				} else if isReg {
					delete(loaded, dst)
				}
			case x86asm.OR:
				dst, isReg := in.Inst.Args[0].(x86asm.Reg)
				v, isImm := in.Inst.Args[1].(x86asm.Imm)
				if off, held := loaded[dst]; isReg && isImm && held && uint32(v)&0x80000000 != 0 {
					return &ResourceLayout{Data: off, Alias: uint32(v)}, nil
				}
			}
		}
	}
	return nil, fmt.Errorf("xbe: %s at %08X reaches no resource data through an alias bit", findSurface, va)
}
