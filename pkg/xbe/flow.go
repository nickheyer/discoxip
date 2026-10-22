package xbe

import (
	"golang.org/x/arch/x86/x86asm"
)

// Lists the instruction indices control can reach after an instruction of a function
func (fn *Function) successors(i int, index map[uint32]int) []int {
	in := &fn.Instructions[i]
	op := in.Inst.Op
	switch op {
	case x86asm.RET, x86asm.HLT, x86asm.INT:
		return nil
	}
	var out []int
	if isJump(op) {
		if t := resolveTarget(in); t != 0 {
			if j, ok := index[t]; ok {
				out = append(out, j)
			}
		}
		if isUnconditionalJump(op) {
			return out
		}
	}
	if j, ok := index[in.VA+uint32(in.Inst.Len)]; ok {
		out = append(out, j)
	}
	return out
}

func (fn *Function) instructionIndex() map[uint32]int {
	index := make(map[uint32]int, len(fn.Instructions))
	for i := range fn.Instructions {
		index[fn.Instructions[i].VA] = i
	}
	return index
}

// Computes the data addresses registers hold before each instruction, agreeing across every path reaching it
func (d *Disassembly) staticAddressFlow(fn *Function) []map[x86asm.Reg]uint32 {
	return d.staticAddressFlowFrom(fn, nil)
}

// Computes the same flow with the addresses the caller already knows the entry registers hold
func (d *Disassembly) staticAddressFlowFrom(fn *Function, seed map[x86asm.Reg]uint32) []map[x86asm.Reg]uint32 {
	index := fn.instructionIndex()
	states := make([]map[x86asm.Reg]uint32, len(fn.Instructions))
	entry, ok := index[fn.EntryVA]
	if !ok {
		return states
	}
	states[entry] = map[x86asm.Reg]uint32{}
	for r, a := range seed {
		states[entry][r] = a
	}
	work := []int{entry}
	for len(work) > 0 {
		i := work[len(work)-1]
		work = work[:len(work)-1]
		held := map[x86asm.Reg]uint32{}
		for r, a := range states[i] {
			held[r] = a
		}
		in := &fn.Instructions[i]
		if dst, src, ok := registerMove(in); ok {
			if a, ok := held[src]; ok {
				held[dst] = a
			} else {
				delete(held, dst)
			}
		} else if in.Inst.Op == x86asm.MOV && argCount(in) == 2 && isRegArg(in.Inst.Args[0]) {
			dst := wideRegister(in.Inst.Args[0].(x86asm.Reg))
			if imm, ok := in.Inst.Args[1].(x86asm.Imm); ok && d.isDataAddress(uint32(imm)) {
				held[dst] = uint32(imm)
			} else {
				delete(held, dst)
			}
		} else {
			for _, r := range d.writtenBy(in) {
				delete(held, r)
			}
		}
		for _, j := range fn.successors(i, index) {
			if states[j] == nil {
				states[j] = held
				work = append(work, j)
				continue
			}
			changed := false
			for r, a := range states[j] {
				if b, ok := held[r]; !ok || b != a {
					delete(states[j], r)
					changed = true
				}
			}
			if changed {
				work = append(work, j)
			}
		}
	}
	return states
}

func isRegArg(a x86asm.Arg) bool {
	_, ok := a.(x86asm.Reg)
	return ok
}

// Lists the argument registers some path of a function reads before any instruction on it writes them
func (d *Disassembly) liveInRegisters(fn *Function) []x86asm.Reg {
	if d.liveIns == nil {
		d.liveIns = map[uint32][]x86asm.Reg{}
	}
	if regs, done := d.liveIns[fn.EntryVA]; done {
		return regs
	}
	index := fn.instructionIndex()
	written := make([]map[x86asm.Reg]bool, len(fn.Instructions))
	live := map[x86asm.Reg]bool{}
	entry, ok := index[fn.EntryVA]
	if !ok {
		return nil
	}
	written[entry] = map[x86asm.Reg]bool{}
	prologue := map[int]bool{}
	for i := entry; i < len(fn.Instructions); i++ {
		in := &fn.Instructions[i]
		if in.Inst.Op == x86asm.PUSH {
			if r, ok := in.Inst.Args[0].(x86asm.Reg); ok && (r == x86asm.EBP || r == x86asm.EBX || r == x86asm.ESI || r == x86asm.EDI) {
				prologue[i] = true
				continue
			}
			break
		}
		switch in.Inst.Op {
		case x86asm.MOV, x86asm.LEA, x86asm.SUB, x86asm.AND, x86asm.XOR:
			continue
		}
		break
	}
	popped := map[x86asm.Reg]bool{}
	for i := range fn.Instructions {
		if in := &fn.Instructions[i]; in.Inst.Op == x86asm.POP {
			if r, ok := in.Inst.Args[0].(x86asm.Reg); ok {
				popped[wideRegister(r)] = true
			}
		}
	}
	work := []int{entry}
	for len(work) > 0 {
		i := work[len(work)-1]
		work = work[:len(work)-1]
		in := &fn.Instructions[i]
		// Pushing a still untouched register the function pops again later saves it rather than reading an argument.
		save := prologue[i]
		if in.Inst.Op == x86asm.PUSH {
			if r, ok := in.Inst.Args[0].(x86asm.Reg); ok && popped[wideRegister(r)] && !written[i][wideRegister(r)] {
				save = true
			}
		}
		for _, r := range registersRead(in, save) {
			if !written[i][r] {
				live[r] = true
			}
		}
		after := map[x86asm.Reg]bool{}
		for r := range written[i] {
			after[r] = true
		}
		for _, r := range d.writtenBy(in) {
			after[r] = true
		}
		for _, j := range fn.successors(i, index) {
			if written[j] == nil {
				written[j] = after
				work = append(work, j)
				continue
			}
			changed := false
			for r := range written[j] {
				if !after[r] {
					delete(written[j], r)
					changed = true
				}
			}
			if changed {
				work = append(work, j)
			}
		}
	}
	var out []x86asm.Reg
	for _, r := range argumentRegisters {
		if live[r] {
			out = append(out, r)
		}
	}
	d.liveIns[fn.EntryVA] = out
	return out
}

// Computes the global each register was loaded from before each instruction, agreeing across every path reaching it
func (d *Disassembly) globalLoadFlow(fn *Function) []map[x86asm.Reg]uint32 {
	index := fn.instructionIndex()
	states := make([]map[x86asm.Reg]uint32, len(fn.Instructions))
	entry, ok := index[fn.EntryVA]
	if !ok {
		return states
	}
	states[entry] = map[x86asm.Reg]uint32{}
	work := []int{entry}
	for len(work) > 0 {
		i := work[len(work)-1]
		work = work[:len(work)-1]
		held := map[x86asm.Reg]uint32{}
		for r, a := range states[i] {
			held[r] = a
		}
		in := &fn.Instructions[i]
		if dst, src, ok := registerMove(in); ok {
			if a, ok := held[src]; ok {
				held[dst] = a
			} else {
				delete(held, dst)
			}
		} else if in.Inst.Op == x86asm.MOV && argCount(in) == 2 && isRegArg(in.Inst.Args[0]) {
			dst := wideRegister(in.Inst.Args[0].(x86asm.Reg))
			if m, ok := in.Inst.Args[1].(x86asm.Mem); ok && m.Base == 0 && m.Index == 0 && d.isDataAddress(uint32(m.Disp)) {
				held[dst] = uint32(m.Disp)
			} else {
				delete(held, dst)
			}
		} else {
			for _, r := range d.writtenBy(in) {
				delete(held, r)
			}
		}
		for _, j := range fn.successors(i, index) {
			if states[j] == nil {
				states[j] = held
				work = append(work, j)
				continue
			}
			changed := false
			for r, a := range states[j] {
				if b, ok := held[r]; !ok || b != a {
					delete(states[j], r)
					changed = true
				}
			}
			if changed {
				work = append(work, j)
			}
		}
	}
	return states
}
