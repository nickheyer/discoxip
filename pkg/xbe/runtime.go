package xbe

import (
	"encoding/binary"
	"fmt"
	"sort"

	"golang.org/x/arch/x86/x86asm"
)

// LiftExecutable lifts every block the executable's entry and code pointers reach, as the processor runs them,
// its kernel imports resolved to the exports of the kernel the page boots
func LiftExecutable(d *Disassembly, kd *Disassembly, exports map[uint32]uint32) (*MachineModel, error) {
	d.LearnImports(kd)
	rd, err := d.resolvedImports(exports)
	if err != nil {
		return nil, err
	}
	stage("runtime lift of the executable")
	mm, err := LiftMachineInto(rd, rd.runtimeRoots(), NewExprPool())
	if err != nil {
		return nil, err
	}
	stage("runtime lift done")
	return mm, nil
}

// The addresses the page may enter the executable at, its entry, every routine the sweep found and every word of
// the image or immediate of its code that lands on an instruction of an executable section, a word landing on
// zeroed bytes naming data and not code
func (d *Disassembly) runtimeRoots() []uint32 {
	seen := map[uint32]bool{}
	var out []uint32
	add := func(va uint32) {
		if !seen[va] && d.executableAt(va) && !zeroedAt(d.insnAt(va)) {
			seen[va] = true
			out = append(out, va)
		}
	}
	add(d.Image.EntryPoint)
	for va := range d.Functions {
		add(va)
	}
	for i := range d.Image.Sections {
		sec := &d.Image.Sections[i]
		for off := 0; off+4 <= len(sec.Data); off += 4 {
			add(binary.LittleEndian.Uint32(sec.Data[off:]))
		}
	}
	for _, in := range d.InsnByVA {
		for _, a := range in.Inst.Args {
			switch v := a.(type) {
			case x86asm.Imm:
				if in.Inst.Op != x86asm.INT && in.Inst.Op != x86asm.RET {
					add(uint32(v))
				}
			case x86asm.Mem:
				if v.Base == 0 && v.Index == 0 {
					add(uint32(v.Disp))
				}
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// Reports whether an instruction is encoded by nothing but zero bytes, as zeroed memory decodes
func zeroedAt(in *Instruction) bool {
	for _, b := range in.Raw {
		if b != 0 {
			return false
		}
	}
	return len(in.Raw) > 0
}

// CheckOperations reports an operation a lifted block calls that the page's processor does not emit
func (mm *MachineModel) CheckOperations() error {
	known := map[string]bool{}
	for _, name := range cpuOperationNames() {
		known[name] = true
	}
	for key, b := range mm.blocks {
		for _, e := range b.effects {
			if e.Kind != "call" {
				continue
			}
			name := e.Fn
			if len(name) > len(cpuCPUID) && name[:len(cpuCPUID)] == cpuCPUID {
				name = cpuCPUID[:len(cpuCPUID)-1]
			}
			if !known[name] {
				return fmt.Errorf("block %s calls %s, which the page's processor does not answer", key, e.Fn)
			}
		}
	}
	return nil
}
