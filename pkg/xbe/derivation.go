package xbe

import (
	"fmt"
	"sort"

	"golang.org/x/arch/x86/x86asm"
)

// Paths runs a routine from its entry with every input symbolic, callees that exhaust their budget made opaque, an
// instruction the executor cannot express failing the run
func (ex *Executor) Paths(entry uint32, retries int) (paths []Path, partial []Effect, err error) {
	defer func() {
		if r := recover(); r != nil {
			paths, partial, err = nil, nil, fmt.Errorf("symexec: %08X: %v", entry, r)
		}
	}()
	paths, partial, _, err = ex.RunWithRetries(entry, func(*execState) {}, retries)
	return paths, partial, err
}

// RegisterInputs names the registers an unnamed routine reads before writing, in the order its callers fill them
func (d *Disassembly) RegisterInputs(va uint32) []string {
	var out []string
	for _, r := range d.registerInputs(va) {
		out = append(out, "reg."+r.String())
	}
	return out
}

// StackInputs counts the stack argument slots a routine reads above its return address
func (d *Disassembly) StackInputs(va uint32) int {
	fn := d.Functions[va]
	if fn == nil {
		return 0
	}
	return d.stackInputs(fn)
}

// FunctionAt reports the routine whose instructions include an address, the nearest entry below it where
// routines overlap
func (d *Disassembly) FunctionAt(va uint32) *Function {
	entries, span := d.sortedEntries()
	for i := sort.Search(len(entries), func(i int) bool { return entries[i] > va }) - 1; i >= 0 && entries[i]+span > va; i-- {
		if fn := d.Functions[entries[i]]; va < fn.EntryVA+uint32(fn.Size) {
			return fn
		}
	}
	return nil
}

// The routine entries in address order with the longest routine's size, rebuilt once routines were added
func (d *Disassembly) sortedEntries() ([]uint32, uint32) {
	if d.entries == nil || d.entriesOf != len(d.Functions) {
		d.entries, d.entrySpan, d.entriesOf = make([]uint32, 0, len(d.Functions)), 0, len(d.Functions)
		for va, fn := range d.Functions {
			d.entries = append(d.entries, va)
			d.entrySpan = max(d.entrySpan, uint32(fn.Size))
		}
		sort.Slice(d.entries, func(i, j int) bool { return d.entries[i] < d.entries[j] })
	}
	return d.entries, d.entrySpan
}

// SectionOf reports the section holding an address
func (d *Disassembly) SectionOf(va uint32) *Section {
	return d.sectionOf(va)
}

// CallTargets reports every address a call instruction of the image reaches directly
func (d *Disassembly) CallTargets() map[uint32]bool {
	out := map[uint32]bool{}
	for _, in := range d.InsnByVA {
		if in.Inst.Op == x86asm.CALL {
			if t := resolveTarget(in); t != 0 {
				out[t] = true
			}
		}
	}
	return out
}

// Cleanup reports the stack bytes a routine's returns remove beyond the return address
func (d *Disassembly) Cleanup(va uint32) int {
	fn := d.Functions[va]
	if fn == nil {
		return 0
	}
	return d.cleanupOf(fn)
}
