package xbe

import (
	"encoding/binary"
	"fmt"
	"maps"
	"slices"

	"golang.org/x/arch/x86/x86asm"
)

// Scratch addresses a replay of one routine runs with and the blocks it may take
const (
	replayStack     = 0x7FFF0000
	replayReturn    = 0x7FFD0000
	replayBuffer    = 0x7FFC0000
	replayProcessor = 0x7FFA0000
	replayBlocks    = 1 << 24
)

// Bytes of the processor block a replay answers segment relative reads from
const replayProcessorSize = 0x1000

// The kernel routines serializing threads, which a single threaded replay answers at once
var synchronization = []string{"RtlEnterCriticalSection", "RtlEnterCriticalSectionAndRegion", "RtlLeaveCriticalSection", "RtlLeaveCriticalSectionAndRegion", "KeEnterCriticalRegion", "KeLeaveCriticalRegion"}

// Answers each synchronization export by popping the arguments the kernel's routine pops
func (d *Disassembly) skipSynchronization(run *MachineRun, kd *Disassembly) error {
	byName := map[string]*Function{}
	for _, fn := range kd.Functions {
		if fn.Symbol != nil {
			byName[fn.Symbol.Name] = fn
		}
	}
	for _, name := range synchronization {
		fn, ok := byName[name]
		if !ok {
			return fmt.Errorf("xbe: the kernel exports no %s", name)
		}
		words := kd.cleanupOf(fn) / 4
		run.Hooks[fn.EntryVA] = func(r *MachineRun) uint32 {
			ret := r.Pop()
			for range words {
				r.Pop()
			}
			r.Regs["EAX"] = 0
			return ret
		}
	}
	return nil
}

// Replays the executable's own section loader on each language section and reads the table it fills back through
// the routine that looks keys up in it
func (sm *SystemModel) deriveTranslations(d, kd *Disassembly, kernel *KernelInput, va uint32) error {
	fn := d.Functions[va]
	if fn == nil {
		return fmt.Errorf("xbe: translator SetLanguage %08X is not disassembled", va)
	}
	table, ok := scaledTableBase(fn, 4)
	if !ok {
		return fmt.Errorf("xbe: translator SetLanguage %08X indexes no section name table", va)
	}
	loader, slot, count, err := d.sectionLoader(va, table)
	if err != nil {
		return err
	}
	rd, err := d.resolvedImports(kernel.Exports)
	if err != nil {
		return err
	}
	mm, err := LiftMachineRooted(rd, []uint32{loader}, NewExprPool())
	if err != nil {
		return err
	}
	rd.Image = mergedImage(rd.Image, kernel.Image)
	for i := uint32(0); ; i++ {
		p, ok := d.Image.ReadU32(table + 4*i)
		if !ok || p == 0 {
			break
		}
		raw, ok := d.Image.ReadBytes(p, 64)
		if !ok {
			return fmt.Errorf("xbe: language %d section name at %08X lies outside the image", i, p)
		}
		name := readCString(raw, 0)
		sec := d.Image.FindSection(name)
		if sec == nil {
			return fmt.Errorf("xbe: language section %q is not in the image", name)
		}
		args := make([]uint32, count)
		args[slot] = p
		run, err := rd.replay(mm, kd, loader, args)
		if err != nil {
			return fmt.Errorf("xbe: language %q loader %08X: %w", name, loader, err)
		}
		pairs, err := rd.translationPairs(kd, run, sec)
		if err != nil {
			return fmt.Errorf("xbe: language %q: %w", name, err)
		}
		sm.Languages = append(sm.Languages, name)
		sm.Translations = append(sm.Translations, pairs)
	}
	if len(sm.Languages) == 0 {
		return fmt.Errorf("xbe: translator section table %08X names no languages", table)
	}
	return nil
}

// The routine the language setter hands a section name out of its name table, with the argument slot the name
// takes and the arguments the call carries
func (d *Disassembly) sectionLoader(va, table uint32) (uint32, int, int, error) {
	paths, ok := d.symbolicPaths(va)
	if !ok {
		return 0, 0, 0, fmt.Errorf("xbe: translator SetLanguage %08X does not run symbolically", va)
	}
	type site struct {
		callee      uint32
		slot, count int
	}
	found := map[site]bool{}
	for _, p := range paths {
		for _, e := range p.Effects {
			if e.Kind != "call" {
				continue
			}
			var callee uint32
			if !scanSub(e.Fn, &callee) {
				continue
			}
			for i, a := range e.Args {
				if l, isLoad := a.(Load); isLoad {
					if k, ok := entryOffset(l, table, 4); ok && k == 0 {
						found[site{callee, i, len(e.Args)}] = true
					}
				}
			}
		}
	}
	if len(found) != 1 {
		return 0, 0, 0, fmt.Errorf("xbe: translator SetLanguage %08X hands a section name to %d routines", va, len(found))
	}
	for st := range found {
		return st.callee, st.slot, st.count, nil
	}
	return 0, 0, 0, nil
}

// Runs a routine of the image concretely with the inputs given, kernel exports answering zero after popping the
// arguments the kernel's own routines pop
func (d *Disassembly) replay(mm *MachineModel, kd *Disassembly, entry uint32, args []uint32) (*MachineRun, error) {
	return d.replayInto(NewMachineRun(mm, d.Image, map[string]uint32{"ESP": replayStack}), kd, entry, args)
}

// A copy of the disassembly over an image whose import thunks hold the kernel export addresses, as the loader fills
// them before the executable starts
func (d *Disassembly) resolvedImports(exports map[uint32]uint32) (*Disassembly, error) {
	img := *d.Image
	img.Sections = append([]Section(nil), d.Image.Sections...)
	copied := map[int]bool{}
	for thunk := range d.Imports {
		raw, ok := d.Image.ReadU32(thunk)
		if !ok {
			return nil, fmt.Errorf("xbe: import thunk %08X lies outside the image", thunk)
		}
		ordinal := raw & 0x7FFFFFFF
		va, ok := exports[ordinal]
		if !ok {
			return nil, fmt.Errorf("xbe: the kernel exports no ordinal %d for the thunk at %08X", ordinal, thunk)
		}
		for i := range img.Sections {
			s := &img.Sections[i]
			if thunk < s.VirtualAddr || thunk+4 > s.VirtualAddr+s.VirtualSize {
				continue
			}
			if !copied[i] {
				s.Data = append([]byte(nil), s.Data...)
				copied[i] = true
			}
			binary.LittleEndian.PutUint32(s.Data[thunk-s.VirtualAddr:], va)
		}
	}
	rd := *d
	rd.Image = &img
	return &rd, nil
}

// The dashboard and the kernel laid out together, as memory holds them while the dashboard runs
func mergedImage(dash, kernel *Image) *Image {
	img := *dash
	img.Sections = append(append([]Section(nil), dash.Sections...), kernel.Sections...)
	return &img
}

// Lifts code the run reaches that no lift covered, from the kernel for its addresses and from the executable
// otherwise, so the images' own code answers every call
func (d *Disassembly) lendCode(run *MachineRun, kd *Disassembly) {
	run.Missing = func(r *MachineRun, va uint32) error {
		src := d
		if _, inKernel := kd.Image.ReadU8(va); inKernel {
			src = kd
		}
		if src.insnAt(va) == nil {
			return fmt.Errorf("%08X holds no code", va)
		}
		if debugTrace {
			name := ""
			if fn := src.Functions[va]; fn != nil && fn.Symbol != nil {
				name = fn.Symbol.Name
			}
			fmt.Printf("  replay: lifting %08X %s\n", va, name)
		}
		more, err := LiftMachineRooted(src, []uint32{va}, r.mm.Exprs)
		if err != nil {
			return err
		}
		return r.mm.Absorb(more)
	}
}

// Fills a scratch region with words naming their own address, so every list the kernel finds in it is empty and
// every pointer it reads lands back inside it
func (run *MachineRun) inertRegion(base, size uint32) {
	for off := uint32(0); off < size; off += 4 {
		run.WriteU32(base+off, base+off)
	}
}

// tableReader is a routine indexing a table with the scale it steps entries by and the words it reads of each
type tableReader struct {
	va    uint32
	scale uint32
	disps []uint32
}

// The unnamed routines indexing a table lying in an address range
func (d *Disassembly) tableReaders(lo, hi uint32) []tableReader {
	var out []tableReader
	for _, va := range d.functionVAs() {
		fn := d.Functions[va]
		if fn.Symbol != nil || d.inLibrary(va) {
			continue
		}
		disps := map[uint32]bool{}
		scale := uint32(0)
		for i := range fn.Instructions {
			for _, a := range fn.Instructions[i].Inst.Args {
				m, ok := a.(x86asm.Mem)
				if !ok || m.Index == 0 || uint32(m.Disp) < lo || uint32(m.Disp) > hi {
					continue
				}
				disps[uint32(m.Disp)] = true
				scale = uint32(m.Scale)
			}
		}
		if len(disps) > 0 {
			out = append(out, tableReader{va: va, scale: scale, disps: slices.Sorted(maps.Keys(disps))})
		}
	}
	return out
}

// Reads the key and value of every entry the loader stored into its table, the layout coming from the routine
// indexing the table and every pair checked by running that routine on the key
func (d *Disassembly) translationPairs(kd *Disassembly, run *MachineRun, sec *Section) (map[string]string, error) {
	lo, hi := sec.VirtualAddr, sec.VirtualAddr+sec.VirtualSize
	inSection := func(v uint32) bool { return v >= lo && v < hi }
	written := map[uint32]bool{}
	for addr := range run.Written() {
		if addr%4 == 0 && !inSection(addr) && (addr < replayBuffer || addr >= replayStack) && inSection(run.ReadU32(addr)) {
			written[addr] = true
		}
	}
	if len(written) == 0 {
		return nil, fmt.Errorf("the loader stored no pointer into the section")
	}
	addrs := slices.Sorted(maps.Keys(written))
	readers := d.tableReaders(addrs[0], addrs[len(addrs)-1])
	if len(readers) == 0 {
		return nil, fmt.Errorf("no routine indexes the table at %08X", addrs[0])
	}
	var results []map[string]string
	var failures []string
	for _, reader := range readers {
		pairs, err := d.readTable(kd, run, reader, inSection)
		if err != nil {
			failures = append(failures, err.Error())
			continue
		}
		results = append(results, pairs)
	}
	if len(results) != 1 {
		return nil, fmt.Errorf("%d routines read the table consistently (%v)", len(results), failures)
	}
	return results[0], nil
}

// Reads the table through one reader, the first entry word the key as keys precede values in the section, every
// entry confirmed by the reader answering the key with the value
func (d *Disassembly) readTable(kd *Disassembly, run *MachineRun, reader tableReader, inSection func(uint32) bool) (map[string]string, error) {
	if len(reader.disps) != 2 || reader.scale < 8 {
		return nil, fmt.Errorf("reader %08X reads %d words of %d byte entries", reader.va, len(reader.disps), reader.scale)
	}
	base, stride := reader.disps[0], reader.scale
	type entry struct{ key, value uint32 }
	var entries []entry
	for at := base; ; at += stride {
		a, b := run.ReadU32(at+reader.disps[0]-base), run.ReadU32(at+reader.disps[1]-base)
		if a == 0 && b == 0 {
			break
		}
		if !inSection(a) || !inSection(b) || a >= b {
			return nil, fmt.Errorf("reader %08X entry at %08X holds %08X and %08X", reader.va, at, a, b)
		}
		entries = append(entries, entry{a, b})
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("reader %08X finds no entry", reader.va)
	}
	mm, err := LiftMachineRooted(d, []uint32{reader.va}, NewExprPool())
	if err != nil {
		return nil, err
	}
	// The reader takes the key and the buffer it answers into among its inputs, in some pair of slots
	inputs := len(d.registerInputs(reader.va)) + d.stackInputs(d.Functions[reader.va])
	var pairs map[string]string
	orders := 0
	for keySlot := 0; keySlot < inputs; keySlot++ {
		for outSlot := 0; outSlot < inputs; outSlot++ {
			if keySlot == outSlot {
				continue
			}
			got := map[string]string{}
			consistent := true
			for _, e := range entries {
				args := make([]uint32, inputs)
				args[keySlot], args[outSlot] = e.key, replayBuffer
				sub := NewMachineRun(mm, d.Image, map[string]uint32{"ESP": replayStack})
				sub.Adopt(run)
				r, err := d.replayInto(sub, kd, reader.va, args)
				if err != nil {
					consistent = false
					break
				}
				key, value := run.WideString(e.key), run.WideString(e.value)
				if r.WideString(replayBuffer) != value {
					consistent = false
					break
				}
				if _, dup := got[key]; !dup {
					got[key] = value
				}
			}
			if consistent {
				pairs = got
				orders++
			}
		}
	}
	if orders != 1 {
		return nil, fmt.Errorf("reader %08X answers the keys in %d input orders", reader.va, orders)
	}
	return pairs, nil
}

// Runs a routine in a run already holding memory, the inputs placed as replay places them
func (d *Disassembly) replayInto(run *MachineRun, kd *Disassembly, entry uint32, args []uint32) (*MachineRun, error) {
	fn := d.Functions[entry]
	if fn == nil {
		return nil, fmt.Errorf("routine %08X is not disassembled", entry)
	}
	regs := d.registerInputs(entry)
	if len(regs) > len(args) {
		return nil, fmt.Errorf("routine %08X takes %d register inputs, %d arguments given", entry, len(regs), len(args))
	}
	header, err := d.Image.LoadedHeader()
	if err != nil {
		return nil, err
	}
	run.Preload(d.Image.BaseAddr, header)
	run.Segments["FS"] = replayProcessor
	run.inertRegion(replayProcessor, replayProcessorSize)
	d.lendCode(run, kd)
	if err := d.skipSynchronization(run, kd); err != nil {
		return nil, err
	}
	for i, r := range regs {
		idx, ok := regIndex[r]
		if !ok || idx >= len(registerNames) {
			return nil, fmt.Errorf("routine %08X takes an input in %s", entry, r)
		}
		run.Regs[registerNames[idx]] = args[i]
	}
	for i := len(args) - 1; i >= len(regs); i-- {
		run.Push(args[i])
	}
	run.Push(replayReturn)
	run.Stop[replayReturn] = true
	if _, err := run.Run(entry, replayBlocks); err != nil {
		return nil, err
	}
	if run.Failed != nil {
		return nil, run.Failed
	}
	return run, nil
}
