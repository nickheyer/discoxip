package xbe

import (
	"fmt"

	"golang.org/x/arch/x86/x86asm"
)

// The CRT routine that calls every static initializer before main
const crtInit = "_cinit"

// Most bytes a static initializer table may span, and the word the CRT skips beside zero
const (
	initTableLimit = 0x10000
	initTableSkip  = 0xFFFFFFFF
)

// Lists the static initializers in call order from the pointer tables the CRT init walks
func (d *Disassembly) staticInitializers() ([]uint32, error) {
	cinit := d.symbolVA(crtInit)
	if cinit == 0 {
		return nil, fmt.Errorf("xbe: no %s routine among the library functions", crtInit)
	}
	fn := d.Functions[cinit]
	var out []uint32
	tables := 0
	for i := range fn.Instructions {
		lo, hi, ok := d.initTableBounds(fn, i)
		if !ok {
			continue
		}
		tables++
		for va := lo; va < hi; va += 4 {
			p, ok := d.Image.ReadU32(va)
			if !ok {
				return nil, fmt.Errorf("xbe: initializer table %08X-%08X runs past the image at %08X", lo, hi, va)
			}
			if p == 0 || p == initTableSkip {
				continue
			}
			if d.Functions[p] == nil {
				return nil, fmt.Errorf("xbe: initializer table %08X-%08X names %08X, which is no routine", lo, hi, p)
			}
			out = append(out, p)
		}
	}
	if tables == 0 {
		return nil, fmt.Errorf("xbe: %s at %08X walks no initializer table", crtInit, cinit)
	}
	return out, nil
}

// The bounds of a table the init walks at an instruction, pushed at a call to the walker or compared before an inlined walk
func (d *Disassembly) initTableBounds(fn *Function, i int) (lo, hi uint32, ok bool) {
	in := &fn.Instructions[i]
	switch in.Inst.Op {
	case x86asm.CALL:
		args := d.siteArguments(fn, i)
		if len(args.stack) < 2 || !args.known[0] || !args.known[1] {
			return 0, 0, false
		}
		lo, hi = args.stack[0], args.stack[1]
	case x86asm.CMP:
		a, isRegA := in.Inst.Args[0].(x86asm.Reg)
		b, isRegB := in.Inst.Args[1].(x86asm.Reg)
		if !isRegA || !isRegB {
			return 0, 0, false
		}
		regs := d.siteArgumentsFrom(fn, i, nil).regs
		var knownA, knownB bool
		lo, knownA = regs[wideRegister(a)]
		hi, knownB = regs[wideRegister(b)]
		if !knownA || !knownB {
			return 0, 0, false
		}
	default:
		return 0, 0, false
	}
	if lo >= hi || hi-lo > initTableLimit || lo%4 != 0 || d.sectionOf(lo) == nil || d.sectionOf(hi-4) == nil {
		return 0, 0, false
	}
	return lo, hi, true
}

// Reports whether a function calls a target directly or through its own callees within a depth
func (d *Disassembly) callsWithin(va, target uint32, depth int) bool {
	fn := d.Functions[va]
	if fn == nil {
		return false
	}
	for _, c := range fn.CallTargets {
		if c == target || (depth > 0 && d.callsWithin(c, target, depth-1)) {
			return true
		}
	}
	return false
}
