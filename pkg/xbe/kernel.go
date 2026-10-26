package xbe

import (
	"fmt"
	"sort"
	"strings"

	"golang.org/x/arch/x86/x86asm"
)

// KernelExportName names a kernel export by its ordinal, empty where the table names none
func KernelExportName(ordinal uint32) string {
	if int(ordinal) < len(kernelExports) {
		return kernelExports[ordinal]
	}
	return ""
}

// KernelModel is the kernel lifted from its own image, its exports by name and every routine they reach
type KernelModel struct {
	Base    uint32                     `json:"base"`
	Entry   uint32                     `json:"entry"`
	Exports map[string]uint32          `json:"exports"`
	Errors  map[string]string          `json:"errors"`
	Lifted  map[uint32]*LiftedFunction `json:"-"`
	Names   map[uint32]string          `json:"-"`
}

// DisassembleKernel sweeps a kernel image, its exports and entry seeding the routines beside its calls
func DisassembleKernel(img *Image, exports map[uint32]uint32) (*Disassembly, error) {
	d := &Disassembly{
		Image:      img,
		Functions:  make(map[uint32]*Function),
		Imports:    make(map[uint32]string),
		InsnByVA:   make(map[uint32]*Instruction),
		boundaries: map[uint32]bool{},
		kernel:     true,
	}
	for ordinal, va := range exports {
		if KernelExportName(ordinal) == "" {
			return nil, fmt.Errorf("xbe: kernel export ordinal %d at %08X has no name", ordinal, va)
		}
		d.boundaries[va] = true
	}
	for i := range img.Sections {
		sec := &img.Sections[i]
		if sec.RawSize == 0 {
			continue
		}
		d.linearSweep(sec)
	}
	d.resynchronize()
	d.discoverFunctions()
	d.nameSecurityCheck()
	d.nameStackProbe()
	d.nameMemoryRoutines()
	// An export some routine reads or writes as memory is a variable, the others are routines, one that no call
	// reached being built from its own code
	referenced := map[uint32]bool{}
	for _, va := range d.functionVAs() {
		for i := range d.Functions[va].Instructions {
			for _, a := range d.Functions[va].Instructions[i].Inst.Args {
				if m, ok := a.(x86asm.Mem); ok && m.Base == 0 && m.Index == 0 {
					referenced[uint32(m.Disp)] = true
				}
			}
		}
	}
	known := map[uint32]bool{}
	for va := range d.Functions {
		known[va] = true
	}
	// Exports sharing one body take the lowest ordinal's name, the others standing as its alternates
	ordinals := make([]int, 0, len(exports))
	for ordinal := range exports {
		ordinals = append(ordinals, int(ordinal))
	}
	sort.Ints(ordinals)
	for _, ordinal := range ordinals {
		va := exports[uint32(ordinal)]
		if referenced[va] {
			delete(d.Functions, va)
			continue
		}
		fn := d.Functions[va]
		if fn == nil {
			if d.InsnByVA[va] == nil {
				continue
			}
			fn = d.buildFunction(va, known)
			if fn == nil || !plausibleStart(fn) {
				continue
			}
			d.Functions[va] = fn
			known[va] = true
			for _, t := range fn.CallTargets {
				if callee, ok := d.Functions[t]; ok {
					callee.CalledBy = append(callee.CalledBy, va)
				}
			}
		} else if !plausibleStart(fn) {
			delete(d.Functions, va)
			continue
		}
		name := KernelExportName(uint32(ordinal))
		if fn.Symbol != nil {
			fn.Symbol.Alternates = append(fn.Symbol.Alternates, name)
			continue
		}
		// The export's own return names the bytes it removes, the variable argument ones leaving that to the caller
		sym := Symbol{VA: va, Raw: name, Name: name, Lib: "kernel", ArgBytes: d.cleanupOf(fn), Cdecl: kernelCdecl[name]}
		fn.Name, fn.Symbol = name, &sym
	}
	return d, nil
}

// DeriveKernel lifts every export that is a routine and every routine those reach, block by block
func DeriveKernel(d *Disassembly, exports map[uint32]uint32) (*KernelModel, error) {
	km := &KernelModel{Base: d.Image.BaseAddr, Entry: d.Image.EntryPoint, Exports: map[string]uint32{}, Errors: map[string]string{}, Lifted: map[uint32]*LiftedFunction{}, Names: map[uint32]string{}}
	byName := map[string]uint32{}
	var roots []uint32
	for ordinal, va := range exports {
		name := KernelExportName(ordinal)
		if _, dup := km.Exports[name]; dup {
			return nil, fmt.Errorf("xbe: kernel export %s appears twice", name)
		}
		km.Exports[name] = va
		byName[name] = va
		if d.Functions[va] != nil {
			roots = append(roots, va)
		}
	}
	// The entry the 2BL jumps to starts the kernel, so it is lifted with the exports
	if d.Functions[d.Image.EntryPoint] == nil {
		return nil, fmt.Errorf("xbe: the kernel entry %08X is no routine", d.Image.EntryPoint)
	}
	roots = append(roots, d.Image.EntryPoint)
	sort.Slice(roots, func(i, j int) bool { return roots[i] < roots[j] })
	for _, va := range d.functionVAs() {
		if fn := d.Functions[va]; fn.Symbol != nil {
			km.Names[va] = fn.Symbol.Name
		}
	}
	ex := NewExecutor(d)
	seen := map[uint32]bool{}
	pending := append([]uint32{}, roots...)
	for _, va := range roots {
		seen[va] = true
	}
	visit := func(va uint32) {
		if seen[va] || d.Functions[va] == nil {
			return
		}
		seen[va] = true
		pending = append(pending, va)
	}
	for len(pending) > 0 {
		va := pending[0]
		pending = pending[1:]
		fn := d.Functions[va]
		lifter := NewExecutorShared(ex)
		lf, err := lifter.Lift(va)
		if err == nil && fn.Symbol != nil {
			lf.Params, err = d.libraryParams(fn)
		}
		if err != nil {
			key := fmt.Sprintf("%08X", va)
			if fn.Symbol != nil {
				key = fn.Symbol.Name
			}
			km.Errors[key] = err.Error()
			continue
		}
		km.Lifted[va] = lf
		for _, p := range lf.asPaths() {
			for _, e := range p.Effects {
				if e.Kind != "call" || strings.HasPrefix(e.Fn, "vcall@") || e.Fn == pointerCallName {
					continue
				}
				var sub uint32
				switch {
				case scanSub(e.Fn, &sub):
					visit(sub)
				case e.Target != 0:
					visit(e.Target)
				default:
					if t, ok := byName[e.Fn]; ok {
						visit(t)
					}
				}
			}
		}
	}
	return km, nil
}

// KernelInput is the kernel image as the 2BL leaves it in memory and its exports by ordinal
type KernelInput struct {
	Image   *Image
	Exports map[uint32]uint32
}

// Lifts the kernel and joins its routines, names, exports and memory to the model beside the dashboard's
func (rm *RenderModel) attachKernel(kernel *KernelInput) error {
	if kernel == nil {
		return fmt.Errorf("xbe: the render model needs the kernel image the dashboard runs on")
	}
	d, err := DisassembleKernel(kernel.Image, kernel.Exports)
	if err != nil {
		return err
	}
	km, err := DeriveKernel(d, kernel.Exports)
	if err != nil {
		return err
	}
	for va, lf := range km.Lifted {
		if _, taken := rm.Lifted[va]; taken {
			return fmt.Errorf("xbe: kernel routine %08X collides with a dashboard routine", va)
		}
		for _, p := range lf.asPaths() {
			rm.collectStrings(d, p, rm.Draw.Strings)
		}
		lf.serialize(rm.Exprs)
		rm.Lifted[va] = lf
	}
	for va, name := range km.Names {
		rm.Library[va] = name
	}
	mem, bytes, err := kernel.Image.Memory()
	if err != nil {
		return err
	}
	for _, s := range mem.Sections {
		s.Off += uint32(len(rm.memory))
		rm.Memory.Sections = append(rm.Memory.Sections, s)
	}
	rm.memory = append(rm.memory, bytes...)
	rm.Kernel = km
	return nil
}

// Describe reports a routine's extent, its return cleanup and whether it returns, for inspecting a lift
func (d *Disassembly) Describe(va uint32) string {
	fn := d.Functions[va]
	if fn == nil {
		return fmt.Sprintf("%08X is no routine", va)
	}
	last := fn.Instructions[len(fn.Instructions)-1]
	rets := map[string]int{}
	for i := range fn.Instructions {
		if in := &fn.Instructions[i]; in.Inst.Op == x86asm.RET {
			rets[in.Inst.String()]++
		}
	}
	return fmt.Sprintf("%08X %s: %d instructions through %08X, cleanup %d bytes, returns %v, rets %v", va, fn.Name, len(fn.Instructions), last.VA, d.cleanupOf(fn), !d.neverReturns(fn), rets)
}
