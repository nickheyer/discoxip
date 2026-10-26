package xbe

import (
	"encoding/binary"
	"fmt"
	"maps"
	"slices"
	"sort"

	"golang.org/x/arch/x86/x86asm"
)

// Instruction is a decoded x86 instruction at a virtual address.
type Instruction struct {
	VA   uint32
	Inst x86asm.Inst
	Raw  []byte
}

// Function is a discovered function with its instructions and call targets.
type Function struct {
	EntryVA      uint32
	Name         string // resolved name (kernel import, symbol, or auto-generated)
	Symbol       *Symbol
	Instructions []Instruction
	CallTargets  []uint32            // VAs of functions this function calls
	CalledBy     []uint32            // VAs of functions that call this one
	Size         int                 // total bytes
	Tables       map[uint32][]uint32 // case targets of each indirect jump, by the jump's address
}

// Disassembly is the complete disassembled image.
type Disassembly struct {
	Image     *Image
	Functions map[uint32]*Function // entry VA → Function
	Imports   map[uint32]string    // thunk VA → kernel export name
	Symbols   []Symbol             // library functions identified by signature

	// All instructions indexed by VA for fast lookup
	InsnByVA map[uint32]*Instruction

	ctors           map[uint32]ctorInfo
	clobbers        map[uint32][]x86asm.Reg
	liveIns         map[uint32][]x86asm.Reg
	liveFloatIns    map[uint32][]x86asm.Reg
	savePushes      map[uint32]map[uint32]bool
	conventions     map[uint32]callConvention
	probe           *Executor
	librarySections map[*Section]bool
	resourceRef     map[uint32]resourceRef
	resources       *resourceCalls
	layout          *deviceLayout
	layoutErr       error
	layoutBusy      bool
	biases          map[uint32]int32
	stackIns        map[uint32]int
	boundaries      map[uint32]bool         // routines the image names from outside its code, which no routine runs into
	noReturn        map[uint32]bool         // routines known never to return to their caller
	kernel          bool                    // the image is the kernel, whose named routines lie among its own rather than in libraries
	endsAt          map[uint32]*Instruction // the instruction ending at each address, for the machine lift's entry check
}

// Disassemble performs full recursive disassembly of the XBE image.
// It starts from the entry point and all kernel thunk targets,
// recursively following CALL and JMP targets to discover all functions.
func Disassemble(img *Image) (*Disassembly, error) {
	d := &Disassembly{
		Image:     img,
		Functions: make(map[uint32]*Function),
		Imports:   make(map[uint32]string),
		InsnByVA:  make(map[uint32]*Instruction),
	}

	// Step 1: Resolve kernel imports from the thunk table.
	d.resolveImports()

	// Step 2: Disassemble ALL sections.
	for i := range img.Sections {
		sec := &img.Sections[i]
		if sec.RawSize == 0 {
			continue
		}
		d.linearSweep(sec)
	}
	d.resynchronize()

	// Step 3: Discover function boundaries across all code.
	d.discoverFunctions()

	// Step 4: Name XDK library functions by signature.
	if _, err := d.IdentifyLibraryFunctions(); err != nil {
		return nil, err
	}

	// Step 5 names the functions the executable identifies itself and the compiler helpers carrying no signature
	if err := d.nameXTLID(); err != nil {
		return nil, err
	}
	d.nameSecurityCheck()
	d.nameStackProbe()
	d.nameMemoryRoutines()

	// Step 6: Read the library bodies the optimizer expanded into the dashboard's code as the calls they replaced.
	d.scanResourceRefs()
	d.scanResourceCalls()

	return d, nil
}

// resolveImports reads the kernel thunk table and maps import addresses to names.
func (d *Disassembly) resolveImports() {
	addr := d.Image.KernThunk
	for {
		val, ok := d.Image.ReadU32(addr)
		if !ok || val == 0 {
			break
		}

		ordinal := val & 0x7FFFFFFF
		if int(ordinal) < len(kernelExports) && kernelExports[ordinal] != "" {
			d.Imports[addr] = kernelExports[ordinal]
		}
		addr += 4
	}
}

// linearSweep disassembles the entire .text section instruction by instruction.
func (d *Disassembly) linearSweep(text *Section) {
	data := text.Data
	va := text.VirtualAddr
	offset := 0

	for offset < len(data) {
		inst, err := x86asm.Decode(data[offset:], 32)
		if err != nil {
			// Invalid instruction — skip one byte
			offset++
			va++
			continue
		}

		raw := make([]byte, inst.Len)
		copy(raw, data[offset:offset+inst.Len])

		insn := &Instruction{
			VA:   va,
			Inst: inst,
			Raw:  raw,
		}
		d.InsnByVA[va] = insn

		offset += inst.Len
		va += uint32(inst.Len)
	}
}

// Decodes again from every call target the sweep passed over misaligned, data in a code section having
// desynchronized it, each target tried once since one that decodes to nothing stays missing
func (d *Disassembly) resynchronize() {
	tried := map[uint32]bool{}
	for {
		var missing []uint32
		for _, insn := range d.InsnByVA {
			if insn.Inst.Op != x86asm.CALL {
				continue
			}
			if t := resolveTarget(insn); t != 0 && !tried[t] && d.InsnByVA[t] == nil && d.sectionOf(t) != nil {
				missing = append(missing, t)
				tried[t] = true
			}
		}
		if len(missing) == 0 {
			return
		}
		sort.Slice(missing, func(i, j int) bool { return missing[i] < missing[j] })
		for _, va := range missing {
			d.decodeFrom(va)
		}
	}
}

// Decodes instructions from an address until reaching code already decoded or the end of the section
func (d *Disassembly) decodeFrom(va uint32) {
	sec := d.sectionOf(va)
	for va < sec.VirtualAddr+sec.VirtualSize && d.InsnByVA[va] == nil {
		inst, err := x86asm.Decode(sec.Data[va-sec.VirtualAddr:], 32)
		if err != nil {
			return
		}
		raw := make([]byte, inst.Len)
		copy(raw, sec.Data[va-sec.VirtualAddr:va-sec.VirtualAddr+uint32(inst.Len)])
		d.InsnByVA[va] = &Instruction{VA: va, Inst: inst, Raw: raw}
		if inst.Op == x86asm.RET || inst.Op == x86asm.JMP || inst.Op == x86asm.HLT || inst.Op == x86asm.INT {
			return
		}
		va += uint32(inst.Len)
	}
}

// discoverFunctions identifies function boundaries across all disassembled sections.
func (d *Disassembly) discoverFunctions() {
	// Collect all CALL targets as potential function entries.
	entries := make(map[uint32]bool)
	entries[d.Image.EntryPoint] = true

	for _, insn := range d.InsnByVA {
		if insn.Inst.Op == x86asm.CALL {
			target := resolveTarget(insn)
			if target != 0 && d.InsnByVA[target] != nil {
				entries[target] = true
			}
		}
	}

	// Also look for common function prologues as entry points.
	for va, insn := range d.InsnByVA {
		if insn.Inst.Op == x86asm.PUSH {
			if arg, ok := insn.Inst.Args[0].(x86asm.Reg); ok && arg == x86asm.EBP {
				if next, ok := d.InsnByVA[va+uint32(insn.Inst.Len)]; ok {
					if next.Inst.Op == x86asm.MOV {
						entries[va] = true
					}
				}
			}
		}
	}

	for _, va := range d.pointerTargets() {
		entries[va] = true
	}
	// The cases a jump table names belong to the routine switching through it, not to functions of their own
	for _, insn := range d.InsnByVA {
		if insn.Inst.Op != x86asm.JMP || resolveTarget(insn) != 0 {
			continue
		}
		if sec := d.sectionOf(insn.VA); sec != nil {
			for _, t := range d.jumpTableTargets(insn, sec) {
				delete(entries, t)
			}
		}
	}

	// A lone jump at an entry is a thunk, its target a function of its own, along the whole chain
	for added := true; added; {
		added = false
		for va := range entries {
			insn := d.InsnByVA[va]
			if insn == nil || insn.Inst.Op != x86asm.JMP {
				continue
			}
			t := resolveTarget(insn)
			if t == 0 || t == va || entries[t] || d.InsnByVA[t] == nil {
				continue
			}
			entries[t] = true
			added = true
		}
	}

	// Sort entries.
	sortedEntries := make([]uint32, 0, len(entries))
	for va := range entries {
		sortedEntries = append(sortedEntries, va)
	}
	sort.Slice(sortedEntries, func(i, j int) bool { return sortedEntries[i] < sortedEntries[j] })

	// Build functions by following control flow from each entry.
	for _, entry := range sortedEntries {
		fn := d.buildFunction(entry, entries)
		if fn != nil {
			d.Functions[entry] = fn
		}
	}

	d.recoverGaps(entries)

	// Build CalledBy cross-references.
	for _, callerVA := range d.functionVAs() {
		for _, target := range d.Functions[callerVA].CallTargets {
			if callee, ok := d.Functions[target]; ok {
				callee.CalledBy = append(callee.CalledBy, callerVA)
			}
		}
	}
}

// Lists function entry addresses in ascending order
func (d *Disassembly) functionVAs() []uint32 {
	return slices.Sorted(maps.Keys(d.Functions))
}

// Treats code left between discovered functions as further function entries
func (d *Disassembly) recoverGaps(entries map[uint32]bool) {
	for i := range d.Image.Sections {
		sec := &d.Image.Sections[i]
		if sec.Flags&0x4 == 0 {
			continue
		}
		covered := make(map[uint32]bool)
		for _, fn := range d.Functions {
			for _, insn := range fn.Instructions {
				for b := uint32(0); b < uint32(insn.Inst.Len); b++ {
					covered[insn.VA+b] = true
				}
			}
		}
		for va := sec.VirtualAddr; va < sec.VirtualAddr+sec.VirtualSize; {
			insn, ok := d.InsnByVA[va]
			if covered[va] || !ok || insn.Inst.Op == x86asm.INT || insn.Inst.Op == x86asm.NOP || insn.Inst.Len == 0 {
				va++
				continue
			}
			if _, exists := d.Functions[va]; exists {
				va++
				continue
			}
			fn := d.buildFunctionWithin(va, entries, covered)
			if fn == nil {
				va++
				continue
			}
			if !plausibleStart(fn) {
				// Data decoded as code is skipped whole rather than retried from every byte.
				for _, in := range fn.Instructions {
					for b := uint32(0); b < uint32(in.Inst.Len); b++ {
						covered[in.VA+b] = true
					}
				}
				va++
				continue
			}
			entries[va] = true
			d.Functions[va] = fn
			for _, in := range fn.Instructions {
				for b := uint32(0); b < uint32(in.Inst.Len); b++ {
					covered[in.VA+b] = true
				}
			}
		}
	}
}

// Collects every instruction reachable from entry through jumps and fallthrough
func (d *Disassembly) buildFunction(entry uint32, entries map[uint32]bool) *Function {
	return d.buildFunctionWithin(entry, entries, nil)
}

// Collects the instructions reachable from entry, never entering code another function already owns
func (d *Disassembly) buildFunctionWithin(entry uint32, entries map[uint32]bool, owned map[uint32]bool) *Function {
	sec := d.sectionOf(entry)
	if sec == nil {
		return nil
	}
	seen := make(map[uint32]bool)
	work := []uint32{entry}
	var callTargets []uint32
	tables := map[uint32][]uint32{}
	for len(work) > 0 {
		va := work[len(work)-1]
		work = work[:len(work)-1]
		for {
			// Running into a routine the image names ends the code a routine owns, as after a call that never returns
			if seen[va] || va < sec.VirtualAddr || va >= sec.VirtualAddr+sec.VirtualSize || (owned != nil && owned[va] && va != entry) || (va != entry && d.boundaries[va]) {
				break
			}
			insn, ok := d.InsnByVA[va]
			if !ok {
				break
			}
			seen[va] = true
			next := va + uint32(insn.Inst.Len)
			op := insn.Inst.Op
			if op == x86asm.CALL {
				if t := resolveTarget(insn); t != 0 {
					callTargets = append(callTargets, t)
				}
			}
			if op == x86asm.RET || op == x86asm.INT || op == x86asm.HLT {
				break
			}
			if isJump(op) {
				t := resolveTarget(insn)
				// A jump to the instruction after it only pads the code, so the routine carries on through it
				if t == next && op == x86asm.JMP {
					va = next
					continue
				}
				if t == 0 {
					if jts := d.jumpTableTargets(insn, sec); len(jts) > 0 {
						tables[va] = jts
						work = append(work, jts...)
					}
				} else if t != entry && !(entries[t] && op == x86asm.JMP) {
					work = append(work, t)
				} else if entries[t] && op == x86asm.JMP {
					// A jump into another function's entry is a tail call.
					callTargets = append(callTargets, t)
				}
				if isUnconditionalJump(op) {
					break
				}
			}
			va = next
		}
	}
	if len(seen) == 0 {
		return nil
	}
	fn := &Function{EntryVA: entry, CallTargets: callTargets, Tables: tables}
	vas := make([]uint32, 0, len(seen))
	for va := range seen {
		vas = append(vas, va)
	}
	sort.Slice(vas, func(i, j int) bool { return vas[i] < vas[j] })
	for _, va := range vas {
		fn.Instructions = append(fn.Instructions, *d.InsnByVA[va])
	}
	last := fn.Instructions[len(fn.Instructions)-1]
	fn.Size = int(last.VA - entry + uint32(last.Inst.Len))
	return fn
}

// Reads consecutive code pointers from an indirect jump's table until one leaves the section
func (d *Disassembly) jumpTableTargets(insn *Instruction, sec *Section) []uint32 {
	mem, ok := insn.Inst.Args[0].(x86asm.Mem)
	if !ok || mem.Scale != 4 || mem.Index == 0 || mem.Disp == 0 {
		return nil
	}
	var out []uint32
	for i := 0; i < 512; i++ {
		t, ok := d.Image.ReadU32(uint32(mem.Disp) + uint32(i)*4)
		if !ok || t < sec.VirtualAddr || t >= sec.VirtualAddr+sec.VirtualSize || d.InsnByVA[t] == nil {
			break
		}
		out = append(out, t)
	}
	return out
}

// Returns the section containing a virtual address or nil
func (d *Disassembly) sectionOf(va uint32) *Section {
	for i := range d.Image.Sections {
		s := &d.Image.Sections[i]
		if va >= s.VirtualAddr && va < s.VirtualAddr+s.VirtualSize {
			return s
		}
	}
	return nil
}

// Collects code addresses stored as data pointers that sit on a function boundary
func (d *Disassembly) pointerTargets() []uint32 {
	endsAt := make(map[uint32]*Instruction, len(d.InsnByVA))
	for _, insn := range d.InsnByVA {
		endsAt[insn.VA+uint32(insn.Inst.Len)] = insn
	}
	var out []uint32
	for i := range d.Image.Sections {
		sec := &d.Image.Sections[i]
		for off := 0; off+4 <= int(sec.RawSize); off += 4 {
			va := binary.LittleEndian.Uint32(sec.Data[off:])
			insn, ok := d.InsnByVA[va]
			if !ok || insn.Inst.Op == x86asm.INT || insn.Inst.Op == x86asm.NOP {
				continue
			}
			prev, hasPrev := endsAt[va]
			if hasPrev && !isBoundaryOp(prev.Inst.Op) {
				continue
			}
			out = append(out, va)
		}
	}
	return out
}

// Reports whether an instruction ending at an address lets a function start there
func isBoundaryOp(op x86asm.Op) bool {
	switch op {
	case x86asm.RET, x86asm.JMP, x86asm.INT, x86asm.NOP, x86asm.HLT:
		return true
	}
	return false
}

// resolveTarget extracts the absolute target address from a CALL or JMP instruction.
func resolveTarget(insn *Instruction) uint32 {
	if len(insn.Inst.Args) == 0 {
		return 0
	}
	switch arg := insn.Inst.Args[0].(type) {
	case x86asm.Rel:
		return insn.VA + uint32(insn.Inst.Len) + uint32(int32(arg))
	case x86asm.Imm:
		return uint32(arg)
	}
	return 0
}

// FormatInsn returns a human-readable string for an instruction.
func (d *Disassembly) FormatInsn(insn *Instruction) string {
	text := x86asm.IntelSyntax(insn.Inst, uint64(insn.VA), d.symbolLookup)
	return fmt.Sprintf("%08X  %s", insn.VA, text)
}

// symbolLookup resolves an address to a symbol name for the disassembler.
func (d *Disassembly) symbolLookup(addr uint64) (string, uint64) {
	va := uint32(addr)

	// Check kernel imports
	if name, ok := d.Imports[va]; ok {
		return name, 0
	}

	// Check function entries
	if fn, ok := d.Functions[va]; ok && fn.Name != "" {
		return fn.Name, 0
	}

	return "", 0
}

// kernel export ordinal table (from ghidra-xbe XbeLoader.java)
var kernelExports = [379]string{
	"",                                    // 0
	"AvGetSavedDataAddress",               // 1
	"AvSendTVEncoderOption",               // 2
	"AvSetDisplayMode",                    // 3
	"AvSetSavedDataAddress",               // 4
	"DbgBreakPoint",                       // 5
	"DbgBreakPointWithStatus",             // 6
	"DbgLoadImageSymbols",                 // 7
	"DbgPrint",                            // 8
	"HalReadSMCTrayState",                 // 9
	"DbgPrompt",                           // 10
	"DbgUnLoadImageSymbols",               // 11
	"ExAcquireReadWriteLockExclusive",     // 12
	"ExAcquireReadWriteLockShared",        // 13
	"ExAllocatePool",                      // 14
	"ExAllocatePoolWithTag",               // 15
	"ExEventObjectType",                   // 16
	"ExFreePool",                          // 17
	"ExInitializeReadWriteLock",           // 18
	"ExInterlockedAddLargeInteger",        // 19
	"ExInterlockedAddLargeStatistic",      // 20
	"ExInterlockedCompareExchange64",      // 21
	"ExMutantObjectType",                  // 22
	"ExQueryPoolBlockSize",                // 23
	"ExQueryNonVolatileSetting",           // 24
	"ExReadWriteRefurbInfo",               // 25
	"ExRaiseException",                    // 26
	"ExRaiseStatus",                       // 27
	"ExReleaseReadWriteLock",              // 28
	"ExSaveNonVolatileSetting",            // 29
	"ExSemaphoreObjectType",               // 30
	"ExTimerObjectType",                   // 31
	"ExfInterlockedInsertHeadList",        // 32
	"ExfInterlockedInsertTailList",        // 33
	"ExfInterlockedRemoveHeadList",        // 34
	"FscGetCacheSize",                     // 35
	"FscInvalidateIdleBlocks",             // 36
	"FscSetCacheSize",                     // 37
	"HalClearSoftwareInterrupt",           // 38
	"HalDisableSystemInterrupt",           // 39
	"HalDiskCachePartitionCount",          // 40
	"HalDiskModelNumber",                  // 41
	"HalDiskSerialNumber",                 // 42
	"HalEnableSystemInterrupt",            // 43
	"HalGetInterruptVector",               // 44
	"HalReadSMBusValue",                   // 45
	"HalReadWritePCISpace",                // 46
	"HalRegisterShutdownNotification",     // 47
	"HalRequestSoftwareInterrupt",         // 48
	"HalReturnToFirmware",                 // 49
	"HalWriteSMBusValue",                  // 50
	"InterlockedCompareExchange",          // 51
	"InterlockedDecrement",                // 52
	"InterlockedIncrement",                // 53
	"InterlockedExchange",                 // 54
	"InterlockedExchangeAdd",              // 55
	"InterlockedFlushSList",               // 56
	"InterlockedPopEntrySList",            // 57
	"InterlockedPushEntrySList",           // 58
	"IoAllocateIrp",                       // 59
	"IoBuildAsynchronousFsdRequest",       // 60
	"IoBuildDeviceIoControlRequest",       // 61
	"IoBuildSynchronousFsdRequest",        // 62
	"IoCheckShareAccess",                  // 63
	"IoCompletionObjectType",              // 64
	"IoCreateDevice",                      // 65
	"IoCreateFile",                        // 66
	"IoCreateSymbolicLink",                // 67
	"IoDeleteDevice",                      // 68
	"IoDeleteSymbolicLink",                // 69
	"IoDeviceObjectType",                  // 70
	"IoFileObjectType",                    // 71
	"IoFreeIrp",                           // 72
	"IoInitializeIrp",                     // 73
	"IoInvalidDeviceRequest",              // 74
	"IoQueryFileInformation",              // 75
	"IoQueryVolumeInformation",            // 76
	"IoQueueThreadIrp",                    // 77
	"IoRemoveShareAccess",                 // 78
	"IoSetIoCompletion",                   // 79
	"IoSetShareAccess",                    // 80
	"IoStartNextPacket",                   // 81
	"IoStartNextPacketByKey",              // 82
	"IoStartPacket",                       // 83
	"IoSynchronousDeviceIoControlRequest", // 84
	"IoSynchronousFsdRequest",             // 85
	"IofCallDriver",                       // 86
	"IofCompleteRequest",                  // 87
	"KdDebuggerEnabled",                   // 88
	"KdDebuggerNotPresent",                // 89
	"IoDismountVolume",                    // 90
	"IoDismountVolumeByName",              // 91
	"KeAlertResumeThread",                 // 92
	"KeAlertThread",                       // 93
	"KeBoostPriorityThread",               // 94
	"KeBugCheck",                          // 95
	"KeBugCheckEx",                        // 96
	"KeCancelTimer",                       // 97
	"KeConnectInterrupt",                  // 98
	"KeDelayExecutionThread",              // 99
	"KeDisconnectInterrupt",               // 100
	"KeEnterCriticalRegion",               // 101
	"MmGlobalData",                        // 102
	"KeGetCurrentIrql",                    // 103
	"KeGetCurrentThread",                  // 104
	"KeInitializeApc",                     // 105
	"KeInitializeDeviceQueue",             // 106
	"KeInitializeDpc",                     // 107
	"KeInitializeEvent",                   // 108
	"KeInitializeInterrupt",               // 109
	"KeInitializeMutant",                  // 110
	"KeInitializeQueue",                   // 111
	"KeInitializeSemaphore",               // 112
	"KeInitializeTimerEx",                 // 113
	"KeInsertByKeyDeviceQueue",            // 114
	"KeInsertDeviceQueue",                 // 115
	"KeInsertHeadQueue",                   // 116
	"KeInsertQueue",                       // 117
	"KeInsertQueueApc",                    // 118
	"KeInsertQueueDpc",                    // 119
	"KeInterruptTime",                     // 120
	"KeIsExecutingDpc",                    // 121
	"KeLeaveCriticalRegion",               // 122
	"KePulseEvent",                        // 123
	"KeQueryBasePriorityThread",           // 124
	"KeQueryInterruptTime",                // 125
	"KeQueryPerformanceCounter",           // 126
	"KeQueryPerformanceFrequency",         // 127
	"KeQuerySystemTime",                   // 128
	"KeRaiseIrqlToDpcLevel",               // 129
	"KeRaiseIrqlToSynchLevel",             // 130
	"KeReleaseMutant",                     // 131
	"KeReleaseSemaphore",                  // 132
	"KeRemoveByKeyDeviceQueue",            // 133
	"KeRemoveDeviceQueue",                 // 134
	"KeRemoveEntryDeviceQueue",            // 135
	"KeRemoveQueue",                       // 136
	"KeRemoveQueueDpc",                    // 137
	"KeResetEvent",                        // 138
	"KeRestoreFloatingPointState",         // 139
	"KeResumeThread",                      // 140
	"KeRundownQueue",                      // 141
	"KeSaveFloatingPointState",            // 142
	"KeSetBasePriorityThread",             // 143
	"KeSetDisableBoostThread",             // 144
	"KeSetEvent",                          // 145
	"KeSetEventBoostPriority",             // 146
	"KeSetPriorityProcess",                // 147
	"KeSetPriorityThread",                 // 148
	"KeSetTimer",                          // 149
	"KeSetTimerEx",                        // 150
	"KeStallExecutionProcessor",           // 151
	"KeSuspendThread",                     // 152
	"KeSynchronizeExecution",              // 153
	"KeSystemTime",                        // 154
	"KeTestAlertThread",                   // 155
	"KeTickCount",                         // 156
	"KeTimeIncrement",                     // 157
	"KeWaitForMultipleObjects",            // 158
	"KeWaitForSingleObject",               // 159
	"KfRaiseIrql",                         // 160
	"KfLowerIrql",                         // 161
	"KiBugCheckData",                      // 162
	"KiUnlockDispatcherDatabase",          // 163
	"LaunchDataPage",                      // 164
	"MmAllocateContiguousMemory",          // 165
	"MmAllocateContiguousMemoryEx",        // 166
	"MmAllocateSystemMemory",              // 167
	"MmClaimGpuInstanceMemory",            // 168
	"MmCreateKernelStack",                 // 169
	"MmDeleteKernelStack",                 // 170
	"MmFreeContiguousMemory",              // 171
	"MmFreeSystemMemory",                  // 172
	"MmGetPhysicalAddress",                // 173
	"MmIsAddressValid",                    // 174
	"MmLockUnlockBufferPages",             // 175
	"MmLockUnlockPhysicalPage",            // 176
	"MmMapIoSpace",                        // 177
	"MmPersistContiguousMemory",           // 178
	"MmQueryAddressProtect",               // 179
	"MmQueryAllocationSize",               // 180
	"MmQueryStatistics",                   // 181
	"MmSetAddressProtect",                 // 182
	"MmUnmapIoSpace",                      // 183
	"NtAllocateVirtualMemory",             // 184
	"NtCancelTimer",                       // 185
	"NtClearEvent",                        // 186
	"NtClose",                             // 187
	"NtCreateDirectoryObject",             // 188
	"NtCreateEvent",                       // 189
	"NtCreateFile",                        // 190
	"NtCreateIoCompletion",                // 191
	"NtCreateMutant",                      // 192
	"NtCreateSemaphore",                   // 193
	"NtCreateTimer",                       // 194
	"NtDeleteFile",                        // 195
	"NtDeviceIoControlFile",               // 196
	"NtDuplicateObject",                   // 197
	"NtFlushBuffersFile",                  // 198
	"NtFreeVirtualMemory",                 // 199
	"NtFsControlFile",                     // 200
	"NtOpenDirectoryObject",               // 201
	"NtOpenFile",                          // 202
	"NtOpenSymbolicLinkObject",            // 203
	"NtProtectVirtualMemory",              // 204
	"NtPulseEvent",                        // 205
	"NtQueueApcThread",                    // 206
	"NtQueryDirectoryFile",                // 207
	"NtQueryDirectoryObject",              // 208
	"NtQueryEvent",                        // 209
	"NtQueryFullAttributesFile",           // 210
	"NtQueryInformationFile",              // 211
	"NtQueryIoCompletion",                 // 212
	"NtQueryMutant",                       // 213
	"NtQuerySemaphore",                    // 214
	"NtQuerySymbolicLinkObject",           // 215
	"NtQueryTimer",                        // 216
	"NtQueryVirtualMemory",                // 217
	"NtQueryVolumeInformationFile",        // 218
	"NtReadFile",                          // 219
	"NtReadFileScatter",                   // 220
	"NtReleaseMutant",                     // 221
	"NtReleaseSemaphore",                  // 222
	"NtRemoveIoCompletion",                // 223
	"NtResumeThread",                      // 224
	"NtSetEvent",                          // 225
	"NtSetInformationFile",                // 226
	"NtSetIoCompletion",                   // 227
	"NtSetSystemTime",                     // 228
	"NtSetTimerEx",                        // 229
	"NtSignalAndWaitForSingleObjectEx",    // 230
	"NtSuspendThread",                     // 231
	"NtUserIoApcDispatcher",               // 232
	"NtWaitForSingleObject",               // 233
	"NtWaitForSingleObjectEx",             // 234
	"NtWaitForMultipleObjectsEx",          // 235
	"NtWriteFile",                         // 236
	"NtWriteFileGather",                   // 237
	"NtYieldExecution",                    // 238
	"ObCreateObject",                      // 239
	"ObDirectoryObjectType",               // 240
	"ObInsertObject",                      // 241
	"ObMakeTemporaryObject",               // 242
	"ObOpenObjectByName",                  // 243
	"ObOpenObjectByPointer",               // 244
	"ObpObjectHandleTable",                // 245
	"ObReferenceObjectByHandle",           // 246
	"ObReferenceObjectByName",             // 247
	"ObReferenceObjectByPointer",          // 248
	"ObSymbolicLinkObjectType",            // 249
	"ObfDereferenceObject",                // 250
	"ObfReferenceObject",                  // 251
	"PhyGetLinkState",                     // 252
	"PhyInitialize",                       // 253
	"PsCreateSystemThread",                // 254
	"PsCreateSystemThreadEx",              // 255
	"PsQueryStatistics",                   // 256
	"PsSetCreateThreadNotifyRoutine",      // 257
	"PsTerminateSystemThread",             // 258
	"PsThreadObjectType",                  // 259
	"RtlAnsiStringToUnicodeString",        // 260
	"RtlAppendStringToString",             // 261
	"RtlAppendUnicodeStringToString",      // 262
	"RtlAppendUnicodeToString",            // 263
	"RtlAssert",                           // 264
	"RtlCaptureContext",                   // 265
	"RtlCaptureStackBackTrace",            // 266
	"RtlCharToInteger",                    // 267
	"RtlCompareMemory",                    // 268
	"RtlCompareMemoryUlong",               // 269
	"RtlCompareString",                    // 270
	"RtlCompareUnicodeString",             // 271
	"RtlCopyString",                       // 272
	"RtlCopyUnicodeString",                // 273
	"RtlCreateUnicodeString",              // 274
	"RtlDowncaseUnicodeChar",              // 275
	"RtlDowncaseUnicodeString",            // 276
	"RtlEnterCriticalSection",             // 277
	"RtlEnterCriticalSectionAndRegion",    // 278
	"RtlEqualString",                      // 279
	"RtlEqualUnicodeString",               // 280
	"RtlExtendedIntegerMultiply",          // 281
	"RtlExtendedLargeIntegerDivide",       // 282
	"RtlExtendedMagicDivide",              // 283
	"RtlFillMemory",                       // 284
	"RtlFillMemoryUlong",                  // 285
	"RtlFreeAnsiString",                   // 286
	"RtlFreeUnicodeString",                // 287
	"RtlGetCallersAddress",                // 288
	"RtlInitAnsiString",                   // 289
	"RtlInitUnicodeString",                // 290
	"RtlInitializeCriticalSection",        // 291
	"RtlIntegerToChar",                    // 292
	"RtlIntegerToUnicodeString",           // 293
	"RtlLeaveCriticalSection",             // 294
	"RtlLeaveCriticalSectionAndRegion",    // 295
	"RtlLowerChar",                        // 296
	"RtlMapGenericMask",                   // 297
	"RtlMoveMemory",                       // 298
	"RtlMultiByteToUnicodeN",              // 299
	"RtlMultiByteToUnicodeSize",           // 300
	"RtlNtStatusToDosError",               // 301
	"RtlRaiseException",                   // 302
	"RtlRaiseStatus",                      // 303
	"RtlTimeFieldsToTime",                 // 304
	"RtlTimeToTimeFields",                 // 305
	"RtlTryEnterCriticalSection",          // 306
	"RtlUlongByteSwap",                    // 307
	"RtlUnicodeStringToAnsiString",        // 308
	"RtlUnicodeStringToInteger",           // 309
	"RtlUnicodeToMultiByteN",              // 310
	"RtlUnicodeToMultiByteSize",           // 311
	"RtlUnwind",                           // 312
	"RtlUpcaseUnicodeChar",                // 313
	"RtlUpcaseUnicodeString",              // 314
	"RtlUpcaseUnicodeToMultiByteN",        // 315
	"RtlUpperChar",                        // 316
	"RtlUpperString",                      // 317
	"RtlUshortByteSwap",                   // 318
	"RtlWalkFrameChain",                   // 319
	"RtlZeroMemory",                       // 320
	"XboxEEPROMKey",                       // 321
	"XboxHardwareInfo",                    // 322
	"XboxHDKey",                           // 323
	"XboxKrnlVersion",                     // 324
	"XboxSignatureKey",                    // 325
	"XeImageFileName",                     // 326
	"XeLoadSection",                       // 327
	"XeUnloadSection",                     // 328
	"READ_PORT_BUFFER_UCHAR",              // 329
	"READ_PORT_BUFFER_USHORT",             // 330
	"READ_PORT_BUFFER_ULONG",              // 331
	"WRITE_PORT_BUFFER_UCHAR",             // 332
	"WRITE_PORT_BUFFER_USHORT",            // 333
	"WRITE_PORT_BUFFER_ULONG",             // 334
	"XcSHAInit",                           // 335
	"XcSHAUpdate",                         // 336
	"XcSHAFinal",                          // 337
	"XcRC4Key",                            // 338
	"XcRC4Crypt",                          // 339
	"XcHMAC",                              // 340
	"XcPKEncPublic",                       // 341
	"XcPKDecPrivate",                      // 342
	"XcPKGetKeyLen",                       // 343
	"XcVerifyPKCS1Signature",              // 344
	"XcModExp",                            // 345
	"XcDESKeyParity",                      // 346
	"XcKeyTable",                          // 347
	"XcBlockCrypt",                        // 348
	"XcBlockCryptCBC",                     // 349
	"XcCryptService",                      // 350
	"XcUpdateCrypto",                      // 351
	"RtlRip",                              // 352
	"XboxLANKey",                          // 353
	"XboxAlternateSignatureKeys",          // 354
	"XePublicKeyData",                     // 355
	"HalBootSMCVideoMode",                 // 356
	"IdexChannelObject",                   // 357
	"HalIsResetOrShutdownPending",         // 358
	"IoMarkIrpMustComplete",               // 359
	"HalInitiateShutdown",                 // 360
	"RtlSnprintf",                         // 361
	"RtlSprintf",                          // 362
	"RtlVsnprintf",                        // 363
	"RtlVsprintf",                         // 364
	"HalEnableSecureTrayEject",            // 365
	"HalWriteSMCScratchRegister",          // 366
	"",                                    // 367
	"",                                    // 368
	"",                                    // 369
	"",                                    // 370
	"",                                    // 371
	"",                                    // 372
	"",                                    // 373
	"MmDbgAllocateMemory",                 // 374
	"MmDbgFreeMemory",                     // 375
	"MmDbgQueryAvailablePages",            // 376
	"MmDbgReleaseAddress",                 // 377
	"MmDbgWriteCheck",                     // 378
}

// Reports whether the first instructions of a recovered function look like compiler output rather than data
func plausibleStart(fn *Function) bool {
	for i := 0; i < len(fn.Instructions) && i < 8; i++ {
		switch fn.Instructions[i].Inst.Op {
		case x86asm.AAS, x86asm.AAA, x86asm.AAD, x86asm.AAM, x86asm.DAA, x86asm.DAS, x86asm.BOUND, x86asm.IRET, x86asm.IRETD, x86asm.ENTER,
			x86asm.IN, x86asm.OUT, x86asm.INSB, x86asm.INSD, x86asm.OUTSB, x86asm.OUTSD, x86asm.LODSB, x86asm.HLT, x86asm.ARPL, x86asm.LES, x86asm.LDS, x86asm.INTO, x86asm.XLATB:
			return false
		case x86asm.MOV, x86asm.POP:
			if r, ok := fn.Instructions[i].Inst.Args[0].(x86asm.Reg); ok && r >= x86asm.ES && r <= x86asm.GS {
				return false
			}
		case x86asm.ADD:
			// Zero bytes decode as add [eax], al, which no compiler emits.
			if m, ok := fn.Instructions[i].Inst.Args[0].(x86asm.Mem); ok && m.Base == x86asm.EAX && m.Disp == 0 && m.Index == 0 {
				if r, ok := fn.Instructions[i].Inst.Args[1].(x86asm.Reg); ok && r == x86asm.AL {
					return false
				}
			}
		}
	}
	return true
}

// Lists the sections holding signature identified library functions
func (d *Disassembly) libraries() map[*Section]bool {
	if d.librarySections == nil {
		d.librarySections = map[*Section]bool{}
		for fva, fn := range d.Functions {
			if fn.Symbol != nil {
				if s := d.sectionOf(fva); s != nil {
					d.librarySections[s] = true
				}
			}
		}
	}
	return d.librarySections
}

// Reports whether an address lies in a section holding signature identified library functions other than the entry section
func (d *Disassembly) inLibrary(va uint32) bool {
	if d.kernel {
		return false
	}
	sec := d.sectionOf(va)
	if sec == nil || sec == d.sectionOf(d.Image.EntryPoint) {
		return false
	}
	return d.libraries()[sec]
}

// Reports whether an address lies in a writable section holding neither the dashboard's code nor a library's
func (d *Disassembly) inPureData(va uint32) bool {
	sec := d.sectionOf(va)
	if sec == nil || sec.Flags&0x1 == 0 || sec == d.sectionOf(d.Image.EntryPoint) {
		return false
	}
	return !d.libraries()[sec]
}

// Names an unidentified library function after the XDK routine the dashboard's use of it reveals
func (d *Disassembly) nameByUsage(va uint32, name string) {
	fn := d.Functions[va]
	if fn == nil || fn.Symbol != nil {
		return
	}
	sym := decorateSymbol(name)
	sym.VA, sym.Lib = va, "usage"
	fn.Name, fn.Symbol = sym.Name, &sym
	delete(d.conventions, va)
	d.librarySections = nil
}
