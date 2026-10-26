package xbe

import (
	"fmt"
	"sort"

	"golang.org/x/arch/x86/x86asm"
)

// CodeTemplate is code the kernel copies elsewhere and patches before running it, the patched words read live
// from the copy when the template's blocks run there
type CodeTemplate struct {
	Base   uint32   `json:"base"`
	Length uint32   `json:"length"`
	Bytes  []byte   `json:"bytes"`
	Slots  []uint32 `json:"slots"`
}

// The name of the export that copies and patches the interrupt dispatch stub
const interruptInitializer = "KeInitializeInterrupt"

// DeriveInterruptTemplate reads the interrupt dispatch stub out of the routine that copies it into each interrupt
// object, the words that routine patches afterwards being the template's slots
func DeriveInterruptTemplate(d *Disassembly, exports map[uint32]uint32) (*CodeTemplate, error) {
	var init uint32
	for ordinal, va := range exports {
		if KernelExportName(ordinal) == interruptInitializer {
			init = va
		}
	}
	fn := d.Functions[init]
	if fn == nil {
		return nil, fmt.Errorf("xbe: the kernel has no %s routine", interruptInitializer)
	}
	known := map[x86asm.Reg]uint32{}
	var pushed []uint32
	var base, length uint32
	for i := range fn.Instructions {
		in := &fn.Instructions[i]
		args := in.Inst.Args
		switch in.Inst.Op {
		case x86asm.MOV:
			if r, ok := args[0].(x86asm.Reg); ok {
				switch v := args[1].(type) {
				case x86asm.Imm:
					known[r] = uint32(v)
				case x86asm.Reg:
					if x, ok := known[v]; ok {
						known[r] = x
					} else {
						delete(known, r)
					}
				default:
					delete(known, r)
				}
			}
		case x86asm.PUSH:
			if v, ok := args[0].(x86asm.Imm); ok {
				pushed = append(pushed, uint32(v))
			}
		case x86asm.POP:
			if r, ok := args[0].(x86asm.Reg); ok && len(pushed) > 0 {
				known[r] = pushed[len(pushed)-1]
				pushed = pushed[:len(pushed)-1]
			}
		case x86asm.MOVSD:
			if hasRep(in) {
				src, okSrc := known[x86asm.ESI]
				count, okCount := known[x86asm.ECX]
				if okSrc && okCount && d.executableAt(src) {
					base, length = src, count*4
				}
			}
		}
	}
	if base == 0 {
		return nil, fmt.Errorf("xbe: %s copies no code", interruptInitializer)
	}
	slots := map[uint32]bool{}
	for i := range fn.Instructions {
		in := &fn.Instructions[i]
		for _, a := range in.Inst.Args {
			m, ok := a.(x86asm.Mem)
			if !ok || m.Base == 0 {
				continue
			}
			disp := uint32(m.Disp)
			if disp >= base && disp < base+length {
				slots[disp-base] = true
			}
		}
	}
	if len(slots) == 0 {
		return nil, fmt.Errorf("xbe: %s patches nothing in the code it copies", interruptInitializer)
	}
	t := &CodeTemplate{Base: base, Length: length}
	for off := range slots {
		t.Slots = append(t.Slots, off)
	}
	sort.Slice(t.Slots, func(i, j int) bool { return t.Slots[i] < t.Slots[j] })
	for i := uint32(0); i < length; i++ {
		b, _ := d.Image.ReadU8(base + i)
		t.Bytes = append(t.Bytes, b)
	}
	return t, nil
}

// Reports whether an instruction's trailing word is a slot the copy patches, and which
func (t *CodeTemplate) patchedWord(in *Instruction) (uint32, bool) {
	if t == nil || in.VA < t.Base || in.VA >= t.Base+t.Length || in.Inst.Len < 4 {
		return 0, false
	}
	off := in.VA + uint32(in.Inst.Len) - 4 - t.Base
	for _, s := range t.Slots {
		if s == off {
			return off, true
		}
	}
	return 0, false
}

// The patched word read from the copy the template runs in
func (t *CodeTemplate) slotLoad(off uint32) Expr {
	return Load{Addr: mkBin("add", Sym{"self"}, IConst{off}), Size: 4}
}

// LiftTemplate lifts a template's code with its patched words read live, replacing the blocks lifted in place
func (mm *MachineModel) LiftTemplate(d *Disassembly, t *CodeTemplate) error {
	ex := NewExecutor(d)
	ex.machine = true
	ex.template = t
	resultPointers = false
	defer func() { resultPointers = true }()
	leaders := map[uint32]bool{t.Base: true}
	for off := uint32(0); off < t.Length; {
		in := d.insnAt(t.Base + off)
		if in == nil {
			break
		}
		if isJump(in.Inst.Op) || in.Inst.Op == x86asm.CALL {
			if next := t.Base + off + uint32(in.Inst.Len); next < t.Base+t.Length {
				leaders[next] = true
			}
			if target := resolveTarget(in); target >= t.Base && target < t.Base+t.Length {
				leaders[target] = true
			}
		}
		off += uint32(in.Inst.Len)
	}
	in := mm.Exprs.interner()
	for va := range leaders {
		b, err := ex.liftMachineBlock(leaders, va, blockEntry{espMoved: true}, true)
		if err != nil {
			return fmt.Errorf("xbe: template block %08X: %w", va, err)
		}
		switch b.term.kind {
		case "jump", "hlt", "int":
			b.succs = []string{blockKey(b.term.target, nil)}
		case "branch":
			b.succs = []string{blockKey(b.term.taken, nil), blockKey(b.term.fall, nil)}
		case "switch":
			for _, target := range b.term.targets {
				b.succs = append(b.succs, blockKey(target, nil))
			}
		}
		key := blockKey(va, nil)
		for k := range mm.blocks {
			if blockKeyVA(k) == va {
				delete(mm.blocks, k)
				delete(mm.Blocks, k)
			}
		}
		mm.blocks[key] = b
		mm.Blocks[key] = serializeBlock(in, b)
	}
	mm.Templates = append(mm.Templates, t)
	return nil
}
