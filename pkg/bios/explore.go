package bios

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/nickheyer/discoxip/pkg/xbe"
)

// Kernel routines that halt the machine, ending every path that reaches them
var haltExports = []string{"KeBugCheck", "KeBugCheckEx"}

// romRange is a physical range the 2BL marked write protected, where the flash answers reads
type romRange struct {
	base, size uint32
}

// busCall is one bus access a replayed kernel routine made
type busCall struct {
	reads      bool
	slave      uint8
	command    uint8
	word       bool
	value      uint32
	valueFixed bool
}

// The bytes the kernel stores into, by fixed address or from a store's address constant to its writable section's end
func (dv *deriver) fixedStores() map[uint32]bool {
	if dv.stores != nil {
		return dv.stores
	}
	dv.stores = map[uint32]bool{}
	img := dv.lifted.Kernel
	mark := func(from, to uint32) {
		for at := from; at < to; at++ {
			dv.stores[at] = true
		}
	}
	dv.lifted.Model.EachBlock(func(b xbe.BlockInfo) {
		if !dv.kernelCode(b.VA) {
			return
		}
		for _, e := range b.Effects {
			if e.Kind != "store" {
				continue
			}
			if at, ok := xbe.FixedAddress(e.Base, e.Addr, e.Off); ok {
				mark(at, at+uint32(e.Size))
				continue
			}
			xbe.WalkExpr(e.Addr, func(n xbe.Expr) {
				c, ok := n.(xbe.IConst)
				if !ok {
					return
				}
				for i := range img.Sections {
					s := &img.Sections[i]
					if s.Writable() && c.V >= s.VirtualAddr && c.V < s.VirtualAddr+s.VirtualSize {
						mark(c.V, s.VirtualAddr+s.VirtualSize)
					}
				}
			})
		}
	})
	return dv.stores
}

// Reports whether an address reads from a device, outside RAM, its mirror, the self map, the flash and the replay scratch
func (dv *deriver) isDevice(at uint32) bool {
	if at < dv.memory || at >= 0x80000000 && at-0x80000000 < dv.memory || xbe.ReplayArea(at) {
		return false
	}
	if at >= dv.selfMap && at-dv.selfMap < 1<<pageDirectoryVA {
		return false
	}
	for _, window := range dv.flash {
		if at >= window && at-window < 1<<pageDirectoryVA {
			return false
		}
	}
	return true
}

// The addresses the page directory is reached at, where the 2BL loaded it, its mirror and the self map
func (dv *deriver) directoryAliases() []uint32 {
	return []uint32{dv.directory, 0x80000000 | dv.directory, dv.selfMap + (dv.selfMap>>pageDirectoryVA)*pageSize}
}

// Lays the 2BL's directory and flash windows into a run, pages mapped over the protected range reading the flash
// The kernel maps its own windows over the flash by storing a large page entry into the directory, counting the
// mappings so a later caller finds the window in place, so an entry a fixed store writes is laid in from the start
// and one a run computes as it is stored
func (dv *deriver) mapPaging(run *xbe.MachineRun) {
	if dv.bldrRun == nil {
		return
	}
	directory := dv.bldrRun.Bytes(dv.directory, pageSize)
	for _, alias := range dv.directoryAliases() {
		run.Preload(alias, directory)
	}
	for _, window := range dv.flash {
		run.Overlays = append(run.Overlays, xbe.Overlay{Base: window, Size: 1 << pageDirectoryVA, Data: dv.bank.Data})
	}
	for _, e := range dv.kernelWindows() {
		dv.mapEntry(run, e.at, e.entry)
	}
	run.Stored = func(r *xbe.MachineRun, addr uint32, size int, v uint32) {
		if size == 4 {
			dv.mapEntry(r, addr, v)
		}
	}
}

// Lays the window of a large page entry stored into the directory over a run, where the frame reads the flash
func (dv *deriver) mapEntry(run *xbe.MachineRun, addr, entry uint32) {
	if entry&1 == 0 || entry&pageDirectoryPS == 0 {
		return
	}
	for _, alias := range dv.directoryAliases() {
		if addr < alias || addr-alias >= pageSize {
			continue
		}
		index := (addr - alias) / 4
		frame := entry &^ (1<<pageDirectoryVA - 1)
		if off, ok := dv.flashOffset(frame); ok {
			data := append(append([]byte{}, dv.bank.Data[off:]...), dv.bank.Data[:off]...)
			run.Overlays = append(run.Overlays, xbe.Overlay{Base: index << pageDirectoryVA, Size: 1 << pageDirectoryVA, Data: data})
		}
	}
}

// The directory entries the kernel's code stores as constants at fixed addresses, in address order
func (dv *deriver) kernelWindows() []directoryStore {
	var out []directoryStore
	seen := map[directoryStore]bool{}
	dv.lifted.Model.EachBlock(func(b xbe.BlockInfo) {
		if !dv.kernelCode(b.VA) {
			return
		}
		for _, e := range b.Effects {
			if e.Kind != "store" || e.Size != 4 {
				continue
			}
			at, fixed := xbe.FixedAddress(e.Base, e.Addr, e.Off)
			v, constant := xbe.ConstOf(e.Value)
			if !fixed || !constant {
				continue
			}
			for _, alias := range dv.directoryAliases() {
				if at >= alias && at-alias < pageSize && !seen[directoryStore{at, v}] {
					seen[directoryStore{at, v}] = true
					out = append(out, directoryStore{at, v})
				}
			}
		}
	})
	sort.Slice(out, func(i, j int) bool {
		return out[i].at < out[j].at || out[i].at == out[j].at && out[i].entry < out[j].entry
	})
	return out
}

// directoryStore is a constant the kernel stores into the page directory at a fixed address
type directoryStore struct {
	at, entry uint32
}

// The offset into the bank a frame of the flash reads from, the flash the range the 2BL write protected and the
// windows it mapped over that range, the bank repeating across both
func (dv *deriver) flashOffset(frame uint32) (int, bool) {
	for _, rom := range dv.rom {
		if frame >= rom.base && frame-rom.base < rom.size {
			return int(frame-rom.base) % len(dv.bank.Data), true
		}
	}
	for _, window := range dv.flash {
		if frame >= window && frame-window < 1<<pageDirectoryVA {
			return int(frame-window) % len(dv.bank.Data), true
		}
	}
	return 0, false
}

// The routines an exploration reads bus accesses from, exports and input free routines themselves, others through their callers
func (dv *deriver) busRoots(explore map[uint32]bool) ([]uint32, error) {
	d := dv.lifted.KernelDis
	roots := map[uint32]bool{}
	seen := map[uint32]bool{}
	queue := make([]uint32, 0, len(explore))
	for entry := range explore {
		queue = append(queue, entry)
	}
	sort.Slice(queue, func(i, j int) bool { return queue[i] < queue[j] })
	for len(queue) > 0 {
		entry := queue[0]
		queue = queue[1:]
		if seen[entry] {
			continue
		}
		seen[entry] = true
		fn := d.Functions[entry]
		if fn == nil {
			return nil, fmt.Errorf("bios: %08X addresses the bus from no routine", entry)
		}
		if dv.exportName[entry] != "" || len(d.RegisterInputs(entry)) == 0 && d.StackInputs(entry) == 0 && d.Cleanup(entry) == 0 {
			roots[entry] = true
			continue
		}
		if owner, ok := dv.routineOf[entry]; ok && owner != entry {
			queue = append(queue, owner)
			continue
		}
		if len(fn.CalledBy) == 0 {
			return nil, fmt.Errorf("bios: %08X addresses the bus with inputs nothing in the kernel hands it", entry)
		}
		queue = append(queue, fn.CalledBy...)
	}
	out := make([]uint32, 0, len(roots))
	for entry := range roots {
		out = append(out, entry)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out, nil
}

// The routines a root reaches by call, the ones a replay answers itself left out
func (dv *deriver) callTree(root uint32, answered map[uint32]bool) map[uint32]bool {
	d := dv.lifted.KernelDis
	tree := map[uint32]bool{}
	queue := []uint32{root}
	for len(queue) > 0 {
		entry := queue[0]
		queue = queue[1:]
		if tree[entry] || answered[entry] {
			continue
		}
		tree[entry] = true
		if fn := d.Functions[entry]; fn != nil {
			queue = append(queue, fn.CallTargets...)
		}
	}
	return tree
}

// The globals the lifts of a tree of routines load
func (dv *deriver) loadedGlobals(tree map[uint32]bool) map[globalKey]bool {
	l := dv.kernel
	out := map[globalKey]bool{}
	collect := func(e xbe.Expr) {
		xbe.WalkExpr(e, func(n xbe.Expr) {
			if ld, ok := n.(xbe.Load); ok {
				if addr, global := globalAt(ld); global {
					out[globalKey{addr, ld.Size}] = true
				}
			}
		})
	}
	entries := make([]uint32, 0, len(tree))
	for entry := range tree {
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i] < entries[j] })
	for _, entry := range entries {
		for _, p := range l.run(entry) {
			for _, g := range p.Guards {
				collect(g)
			}
			for _, e := range p.Effects {
				collect(e.Value)
				collect(e.Addr)
				for _, a := range e.Args {
					collect(a)
				}
			}
		}
	}
	return out
}

// The routines storing into any byte of a global, by the machine lift's routines
func (dv *deriver) storers(g globalKey) []uint32 {
	seen := map[uint32]bool{}
	var out []uint32
	dv.lifted.Model.EachBlock(func(b xbe.BlockInfo) {
		for _, e := range b.Effects {
			if e.Kind != "store" {
				continue
			}
			at, ok := xbe.FixedAddress(e.Base, e.Addr, e.Off)
			if !ok || at >= g.addr+uint32(g.size) || at+uint32(e.Size) <= g.addr {
				continue
			}
			routine, owned := dv.routineOf[b.VA]
			if owned && dv.kernelCode(b.VA) && !seen[routine] {
				seen[routine] = true
				out = append(out, routine)
			}
		}
	})
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// Routines run before a root so the globals it addresses the bus by hold their stores, readers after storers
func (dv *deriver) preludes(root uint32, globals []globalKey, answered map[uint32]bool) ([]uint32, error) {
	tree := dv.callTree(root, answered)
	var storers []uint32
	seen := map[uint32]bool{}
	for _, g := range globals {
		found := false
		for _, s := range dv.storers(g) {
			if tree[s] {
				continue
			}
			found = true
			if !seen[s] {
				seen[s] = true
				storers = append(storers, s)
			}
		}
		if !found {
			return nil, fmt.Errorf("bios: %08X reads %08X into a bus part or command, which no routine outside it stores", root, g.addr)
		}
	}
	sort.Slice(storers, func(i, j int) bool { return storers[i] < storers[j] })
	stored := map[uint32]map[globalKey]bool{}
	reads := map[uint32]map[globalKey]bool{}
	for _, s := range storers {
		stored[s] = map[globalKey]bool{}
		reads[s] = dv.loadedGlobals(dv.callTree(s, answered))
	}
	dv.lifted.Model.EachBlock(func(b xbe.BlockInfo) {
		routine, owned := dv.routineOf[b.VA]
		if !owned || stored[routine] == nil {
			return
		}
		for _, e := range b.Effects {
			if e.Kind != "store" {
				continue
			}
			if at, ok := xbe.FixedAddress(e.Base, e.Addr, e.Off); ok {
				stored[routine][globalKey{at, e.Size}] = true
			}
		}
	})
	sort.SliceStable(storers, func(i, j int) bool {
		a, b := storers[i], storers[j]
		for g := range reads[b] {
			if stored[a][g] {
				return true
			}
		}
		for g := range reads[a] {
			if stored[b][g] {
				return false
			}
		}
		return a < b
	})
	return storers, nil
}

// openGlobalsError reports a bus call whose part, command or width was read from stored globals alone
type openGlobalsError struct {
	ret     uint32
	globals []globalKey
	what    []string
}

func (e *openGlobalsError) Error() string {
	return fmt.Sprintf("bios: the call returning to %08X addresses the bus at %s by globals nothing run before fixes", e.ret, strings.Join(e.what, ", "))
}

// Pushes a routine's frame as a caller does, every input an open value
func (dv *deriver) enterOpen(r *xbe.MachineRun, entry uint32) {
	d := dv.lifted.KernelDis
	for _, reg := range d.RegisterInputs(entry) {
		r.Regs[reg[4:]] = 0
		r.OpenRegister(reg[4:])
	}
	for range max(d.Cleanup(entry)/4, d.StackInputs(entry)) {
		r.PushOpen()
	}
	r.Push(xbe.ReplayReturn)
}

// Explores a root after the storers of the globals its calls address the bus by, found by exploring and retrying
func (dv *deriver) exploreBus(root uint32, read, write uint32, record func(busCall)) error {
	answered := map[uint32]bool{read: true, write: true}
	for va := range dv.atOnce {
		answered[va] = true
	}
	halts := map[uint32]bool{}
	for _, name := range haltExports {
		va, err := dv.export(name)
		if err != nil {
			return err
		}
		halts[va] = true
		answered[va] = true
	}
	// The part, command and width are bytes to the kernel, the value a word, an argument open only where the path's
	// tests leave a byte the kernel reads free to vary
	hook := func(reads bool) func(r *xbe.MachineRun) uint32 {
		return func(r *xbe.MachineRun) uint32 {
			esp := r.Regs["ESP"]
			var open [4]bool
			var globals []globalKey
			var what []string
			fixed := true
			for i := range open {
				at, size := esp+4+4*uint32(i), 1
				if i == 3 {
					size = 4
				}
				sh := r.OpenAt(at, size)
				if sh == nil {
					continue
				}
				_, pinned, err := r.Pinned(sh)
				if err != nil {
					r.Failed = err
					return r.Pop()
				}
				open[i] = !pinned
				if open[i] && i < 3 {
					origins, all := r.OpenGlobals(at, size)
					fixed = fixed && all
					desc := fmt.Sprintf("%s %s", [3]string{"part", "command", "width"}[i], xbe.ExprString(sh))
					for _, o := range r.OpenDevices(at, size) {
						desc += fmt.Sprintf(", reading the device at %08X", o.Addr)
					}
					what = append(what, desc)
					for _, o := range origins {
						globals = append(globals, globalKey{o.Addr, o.Size})
					}
				}
			}
			ret := r.Pop()
			slave, command, word, arg := r.Pop(), r.Pop(), r.Pop(), r.Pop()
			if open[0] || open[1] || open[2] {
				if fixed {
					r.Failed = &openGlobalsError{ret: ret, globals: globals, what: what}
				} else {
					r.Failed = fmt.Errorf("bios: the call returning to %08X addresses the bus at %s, which the path leaves open", ret, strings.Join(what, ", "))
				}
				return ret
			}
			if reads {
				size := 1
				if uint8(word) != 0 {
					size = 2
				}
				r.WriteU32(arg, 0)
				r.MarkOpen(arg, size)
			}
			record(busCall{reads: reads, slave: uint8(slave &^ 1), command: uint8(command), word: uint8(word) != 0, value: arg, valueFixed: !reads && !open[3]})
			r.Regs["EAX"] = 0
			r.OpenRegister("EAX")
			return ret
		}
	}
	var globals []globalKey
	known := map[globalKey]bool{}
	for {
		preludes, err := dv.preludes(root, globals, answered)
		if err != nil {
			return err
		}
		ex := &xbe.Explorer{Model: dv.lifted.Model, Halts: halts, Steps: xbe.ExploreSteps, Paths: xbe.ExplorePaths, Enter: dv.enterOpen}
		if os.Getenv("DISCOXIP_HARDWARE_TRACE") != "" {
			fmt.Fprintf(os.Stderr, "hardware: exploring %08X after %X\n", root, preludes)
			ex.Progress = func(steps, paths, open int) {
				fmt.Fprintf(os.Stderr, "hardware: exploring %08X after %X, %d paths in %d blocks so far, %d open\n", root, preludes, paths, steps, open)
			}
		}
		ex.Prepare = func() *xbe.MachineRun {
			run := dv.kernelRun(dv.newTrace(StageKernel))
			run.OpenShadows(dv.fixedStores(), dv.isDevice)
			run.Hooks[read] = hook(true)
			run.Hooks[write] = hook(false)
			for va := range halts {
				run.Stop[va] = true
			}
			return run
		}
		entries := append(append([]uint32{}, preludes...), root)
		err = ex.Explore(entries)
		var open *openGlobalsError
		if errors.As(err, &open) {
			added := false
			for _, g := range open.globals {
				if !known[g] {
					known[g] = true
					globals = append(globals, g)
					added = true
				}
			}
			if added {
				if os.Getenv("DISCOXIP_HARDWARE_TRACE") != "" {
					names := make([]string, 0, len(open.globals))
					for _, g := range open.globals {
						names = append(names, fmt.Sprintf("%08X", g.addr))
					}
					fmt.Fprintf(os.Stderr, "hardware: %08X addresses the bus by globals %v returning to %08X, %s, their storers run first\n", root, names, open.ret, strings.Join(open.what, ", "))
				}
				continue
			}
		}
		if err != nil {
			return fmt.Errorf("bios: exploring %08X after %X: %w", root, preludes, err)
		}
		if os.Getenv("DISCOXIP_HARDWARE_TRACE") != "" {
			steps, paths, most := ex.Stats()
			fmt.Fprintf(os.Stderr, "hardware: explored %08X after %X over %d paths in %d blocks, %d open at most\n", root, preludes, paths, steps, most)
		}
		return nil
	}
}
