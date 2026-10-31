package bios

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"slices"
	"sort"
	"strings"

	"github.com/nickheyer/discoxip/pkg/xbe"
)

// The stages of the boot that touch the hardware, in order
const (
	StageInit       = "init"
	StageBldr       = "bldr"
	StageKernel     = "kernel"
	StageExecutable = "executable"
)

// Kernel routines and variables the derivation reads by their export names
const (
	exportStall          = "KeStallExecutionProcessor"
	exportCounter        = "KeQueryPerformanceCounter"
	exportFrequency      = "KeQueryPerformanceFrequency"
	exportTimeIncrement  = "KeTimeIncrement"
	exportStatistics     = "MmQueryStatistics"
	exportReadSMBus      = "HalReadSMBusValue"
	exportWriteSMBus     = "HalWriteSMBusValue"
	exportVector         = "HalGetInterruptVector"
	symbolPCISpace       = "HalReadWritePCISpace"
	statisticsPagesField = 4
)

// Processor and bus facts the derivation decodes the boot's accesses with
const (
	pageSize         = 4096
	pageDirectoryPS  = 0x80
	pageDirectoryVA  = 22
	mtrrPhysBase0    = 0x200
	mtrrPhysMask0    = 0x201
	mtrrTypeMask     = 0xFF
	mtrrWriteProtect = 5
	mtrrValid        = 0x800
	pitCommandPort   = 0x43
	pitChannel0Port  = 0x40
	picInitBit       = 0x10
	picSingleBit     = 0x02
	picICW4Bit       = 0x01
	pciAddressPort   = 0xCF8
	pciDataPort      = 0xCFC
	pciEnableBit     = 0x80000000
	pciFirstBAR      = 0x10
	pciLastBAR       = 0x24
	pciBridgeBuses   = 0x18
	pciBARIOBit      = 0x1
	pciConfigSize    = 256
	hundredNsPerSec  = 10000000
	gatePresent      = 0x8000
)

// Hardware is the console as the init table, the 2BL and the kernel find it, every value read from what they
// program, probe, compare and connect
type Hardware struct {
	Memory     uint32         `json:"memory"`
	Stall      Stall          `json:"stall"`
	Timer      Timer          `json:"timer"`
	PICs       []PIC          `json:"pics"`
	CPUID      []CPUIDUse     `json:"cpuid"`
	MSRs       []MSRAccess    `json:"msrs"`
	PCI        []PCIFunction  `json:"pci"`
	Ports      []PortUse      `json:"ports"`
	Registers  []RegisterUse  `json:"registers"`
	Polls      []Poll         `json:"polls"`
	SMBus      SMBus          `json:"smbus"`
	Interrupts []Interrupt    `json:"interrupts"`
	Table      InterruptTable `json:"interrupt_table"`
	Paging     Paging         `json:"paging"`
	Flash      []uint32       `json:"flash_windows"`
	InitTable  *InitTable     `json:"init_table"`
}

// Stall is the stall export, the stack slot of the microseconds it takes, the loops it spins per microsecond
// and the bytes its return removes beyond the return address
type Stall struct {
	Entry    uint32 `json:"entry"`
	Argument int    `json:"argument"`
	Scale    uint32 `json:"loops_per_microsecond"`
	Cleanup  int    `json:"cleanup"`
}

// Timer is the interval timer the kernel programs for its clock and the counter it reports time by
type Timer struct {
	Divisor       uint32 `json:"divisor"`
	TimeIncrement uint32 `json:"time_increment"`
	Hz            uint32 `json:"hz"`
	CounterPort   uint32 `json:"counter_port"`
	CounterHz     uint32 `json:"counter_hz"`
}

// PIC is one interrupt controller as the kernel initializes it through its command and data ports
type PIC struct {
	Command    uint32 `json:"command_port"`
	Data       uint32 `json:"data_port"`
	ICW1       uint8  `json:"icw1"`
	VectorBase uint8  `json:"vector_base"`
	Cascade    uint8  `json:"cascade"`
	ICW4       uint8  `json:"icw4"`
	Mask       uint8  `json:"mask"`
}

// CPUIDUse is one leaf the boot executes and the tests it applies to the answer
type CPUIDUse struct {
	Stage string         `json:"stage"`
	Leaf  uint32         `json:"leaf"`
	Tests []RegisterTest `json:"tests"`
}

// RegisterTest is a comparison the code makes against a masked answer register
type RegisterTest struct {
	Register string `json:"register"`
	And      uint32 `json:"and"`
	Op       string `json:"op"`
	Value    uint32 `json:"value"`
}

// MSRAccess is a model specific register the boot writes or reads, a write setting the bits given and keeping
// the bits named from the register's own reading
type MSRAccess struct {
	Stage    string `json:"stage"`
	Index    uint32 `json:"index"`
	Write    bool   `json:"write"`
	Low      uint32 `json:"low"`
	High     uint32 `json:"high"`
	KeepLow  uint32 `json:"keep_low,omitempty"`
	KeepHigh uint32 `json:"keep_high,omitempty"`
}

// PCIFunction is one function on a bus, its configuration space as the boot writes and reads it and the windows its
// base address registers open
type PCIFunction struct {
	Bus       int            `json:"bus"`
	Device    int            `json:"device"`
	Function  int            `json:"function"`
	Config    []ConfigAccess `json:"config"`
	Secondary int            `json:"secondary_bus,omitempty"`
	Windows   []Window       `json:"windows"`
}

// ConfigAccess is one configuration register access, a write's value fixed or carried from what was read
type ConfigAccess struct {
	Stage    string `json:"stage"`
	Register int    `json:"register"`
	Size     int    `json:"size"`
	Write    bool   `json:"write"`
	Value    uint32 `json:"value"`
	Known    bool   `json:"known"`
}

// Window is one base address register assignment with the accesses the code makes through it
type Window struct {
	Register int      `json:"register"`
	Stage    string   `json:"stage"`
	Base     uint32   `json:"base"`
	IO       bool     `json:"io"`
	Extent   uint32   `json:"extent"`
	Accesses []Access `json:"accesses"`
}

// Access is one offset of a window with how the code reads and writes it
type Access struct {
	Offset uint32   `json:"offset"`
	Size   int      `json:"size"`
	Reads  bool     `json:"reads"`
	Writes []uint32 `json:"writes"`
	Stages []string `json:"stages"`
}

// PortUse is an I/O port outside every window with how the code reads and writes it
type PortUse struct {
	Port   uint32   `json:"port"`
	Size   int      `json:"size"`
	Reads  bool     `json:"reads"`
	Writes []uint32 `json:"writes"`
	Stages []string `json:"stages"`
}

// RegisterUse is a memory mapped register outside every window with how the code reads and writes it
type RegisterUse struct {
	Address uint32   `json:"address"`
	Size    int      `json:"size"`
	Reads   bool     `json:"reads"`
	Writes  []uint32 `json:"writes"`
	Stages  []string `json:"stages"`
}

// Poll is a loop a stage spins in on a memory reading, a branch on the reading leading back to itself, with the
// masked compare against a constant the loop exits on, the address zero where nothing fixes it, the device then
// answering the block's repeated reads of a register
type Poll struct {
	Stage   string `json:"stage"`
	Block   uint32 `json:"block"`
	Address uint32 `json:"address"`
	Size    int    `json:"size"`
	And     uint32 `json:"and"`
	Op      string `json:"op"`
	Value   uint32 `json:"value"`
}

// SMBus is the system management bus controller, its register roles as the kernel drives it and the parts the boot
// addresses on it
type SMBus struct {
	Bus      int          `json:"bus"`
	Device   int          `json:"device"`
	Function int          `json:"function"`
	Base     uint32       `json:"base"`
	Status   uint32       `json:"status_port"`
	Control  uint32       `json:"control_port"`
	Address  uint32       `json:"address_port"`
	Data     uint32       `json:"data_port"`
	Command  uint32       `json:"command_port"`
	Done     uint32       `json:"done_status"`
	Slaves   []SMBusSlave `json:"slaves"`
}

// SMBusSlave is one part on the bus with the commands the boot sends it by constant
type SMBusSlave struct {
	Address  uint8          `json:"address"`
	Commands []SMBusCommand `json:"commands"`
}

// SMBusCommand is one register of a part with how the boot reads and writes it
type SMBusCommand struct {
	Command uint8    `json:"command"`
	Word    bool     `json:"word"`
	Reads   bool     `json:"reads"`
	Writes  []uint32 `json:"writes"`
	Stages  []string `json:"stages"`
}

// Interrupt is one line the kernel serves, through a gate its table holds from the start or a connection it makes,
// with the ports and registers the handler reaches
type Interrupt struct {
	IRQ       int      `json:"irq"`
	Vector    int      `json:"vector"`
	Handler   uint32   `json:"handler"`
	Static    bool     `json:"static"`
	Ports     []uint32 `json:"ports"`
	Registers []uint32 `json:"registers"`
}

// InterruptTable is the descriptor table the kernel loads, with the handler each gate holds
type InterruptTable struct {
	Base     uint32   `json:"base"`
	Limit    uint32   `json:"limit"`
	Handlers []uint32 `json:"handlers"`
}

// Paging is the address space the 2BL builds for the kernel, the directory it loads and the window the directory
// maps itself into
type Paging struct {
	Directory uint32 `json:"directory"`
	SelfMap   uint32 `json:"self_map"`
}

// access is one touch of a port or register by one stage
type access struct {
	stage string
	port  bool
	addr  uint32
	size  int
	write bool
	value uint32
	known bool
	order int
}

// barAssignment is one write of a base address register
type barAssignment struct {
	stage    string
	order    int
	fn       pciKey
	register int
	base     uint32
	io       bool
}

// pciKey identifies a function on a bus
type pciKey struct {
	bus, device, function int
}

// deriver holds what the sub derivations share
type deriver struct {
	bank       *Bank
	lifted     *Lifted
	kernel     *lifter
	exports    map[string]uint32
	atOnce     map[uint32]func(*xbe.MachineRun) uint32
	memory     uint32
	accesses   []access
	bars       []barAssignment
	configs    map[pciKey][]ConfigAccess
	initTable  *InitTable
	bldrTrace  *trace
	bldrRun    *xbe.MachineRun
	replayed   map[uint32]bool
	blocks     map[uint32]xbe.BlockInfo
	routineOf  map[uint32]uint32
	flows      map[uint32]*routineFlow
	selfMap    uint32
	directory  uint32
	flash      []uint32
	rom        []romRange
	stores     map[uint32]bool
	exportName map[uint32]string
	extents    map[uint32]uint32
	stage      string
}

// Prepares a derivation over a bank's init table and its lifted 2BL and kernel, the kernel's blocks indexed by
// address and by the lifted routine holding each
func newDeriver(b *Bank, lifted *Lifted) (*deriver, error) {
	codes, err := b.XCodes()
	if err != nil {
		return nil, err
	}
	table, err := RunInitTable(codes)
	if err != nil {
		return nil, err
	}
	atOnce, err := xbe.AtOnceHooks(lifted.KernelDis)
	if err != nil {
		return nil, err
	}
	writes := map[string][]xbe.OutBuffer{symbolPCISpace: {{Arg: 3, Bytes: pciConfigSize, Count: 4, Unless: 5}}}
	dv := &deriver{atOnce: atOnce, bank: b, lifted: lifted, kernel: newLifter(lifted.KernelDis, lifted.Model, writes), exports: map[string]uint32{}, exportName: map[uint32]string{}, configs: map[pciKey][]ConfigAccess{}, initTable: table, replayed: map[uint32]bool{}, flows: map[uint32]*routineFlow{}}
	dv.blocks, dv.routineOf = indexBlocks(lifted.Model)
	for ordinal, va := range lifted.Exports {
		dv.exports[xbe.KernelExportName(ordinal)] = va
		dv.exportName[va] = xbe.KernelExportName(ordinal)
	}
	return dv, nil
}

// A model's blocks by address, the first lift of each, and the lifted routine holding each block
func indexBlocks(mm *xbe.MachineModel) (map[uint32]xbe.BlockInfo, map[uint32]uint32) {
	blocks, routineOf := map[uint32]xbe.BlockInfo{}, map[uint32]uint32{}
	returns := map[uint32]bool{}
	mm.EachBlock(func(b xbe.BlockInfo) {
		if _, taken := blocks[b.VA]; !taken {
			blocks[b.VA] = b
		}
		if b.Ret != 0 {
			returns[b.Ret] = true
		}
	})
	for _, entry := range mm.Routines() {
		if returns[entry] {
			continue
		}
		for _, va := range mm.RoutineBlockVAs(entry) {
			if _, taken := routineOf[va]; !taken {
				routineOf[va] = entry
			}
		}
	}
	return blocks, routineOf
}

// A derivation over the executable's blocks with the console's layout this derivation read off the kernel
func (dv *deriver) forExecutable(d *xbe.Disassembly, mm *xbe.MachineModel) *deriver {
	ex := &deriver{bank: dv.bank, lifted: &Lifted{Model: mm, Kernel: d.Image, KernelDis: d}, kernel: newLifter(d, mm, nil), exports: dv.exports, exportName: dv.exportName, memory: dv.memory, accesses: dv.accesses, bars: dv.bars, flows: map[uint32]*routineFlow{}, selfMap: dv.selfMap, directory: dv.directory, flash: dv.flash, rom: dv.rom, stage: StageExecutable}
	ex.blocks, ex.routineOf = indexBlocks(mm)
	return ex
}

// DeriveHardware reads the console out of the init table, the 2BL and the kernel
func DeriveHardware(b *Bank, lifted *Lifted) (*Hardware, error) {
	hw, _, err := deriveHardware(b, lifted)
	return hw, err
}

// Reads the console out of the init table, the 2BL and the kernel, handing back the derivation for the executable's
// polls to share
func deriveHardware(b *Bank, lifted *Lifted) (*Hardware, *deriver, error) {
	dv, err := newDeriver(b, lifted)
	if err != nil {
		return nil, nil, err
	}
	hw := &Hardware{InitTable: dv.initTable}
	if hw.Memory, err = dv.deriveMemory(); err != nil {
		return nil, nil, err
	}
	dv.memory = hw.Memory
	stageTrace("memory")
	if hw.Stall, err = dv.deriveStall(); err != nil {
		return nil, nil, err
	}
	stageTrace("stall")
	if hw.Timer, err = dv.deriveTimer(); err != nil {
		return nil, nil, err
	}
	stageTrace("timer")
	if hw.PICs, err = dv.derivePICs(); err != nil {
		return nil, nil, err
	}
	stageTrace("pics")
	if err := dv.replayBldr(); err != nil {
		return nil, nil, err
	}
	stageTrace("bldr")
	hw.Paging = Paging{Directory: dv.directory, SelfMap: dv.selfMap}
	if hw.CPUID, hw.MSRs, err = dv.deriveProcessor(); err != nil {
		return nil, nil, err
	}
	stageTrace("processor")
	if hw.Flash, err = dv.deriveFlash(); err != nil {
		return nil, nil, err
	}
	dv.collectInitAccesses()
	dv.collectBldrAccesses()
	stageTrace("bldr accesses")
	if err := dv.collectBlockAccesses(); err != nil {
		return nil, nil, err
	}
	stageTrace("block accesses")
	if hw.Polls, err = dv.derivePolls(); err != nil {
		return nil, nil, err
	}
	stageTrace("polls")
	if err := dv.collectPCISpace(); err != nil {
		return nil, nil, err
	}
	stageTrace("configuration space")
	hw.PCI, hw.Ports, hw.Registers = dv.layout()
	if hw.SMBus, err = dv.deriveSMBus(hw.PCI); err != nil {
		return nil, nil, err
	}
	stageTrace("smbus")
	if hw.Table, err = dv.deriveTable(); err != nil {
		return nil, nil, err
	}
	if hw.Interrupts, err = dv.deriveInterrupts(hw.PICs, hw.Table); err != nil {
		return nil, nil, err
	}
	stageTrace("interrupts")
	return hw, dv, nil
}

// Reports a finished stage and the heap in use when DISCOXIP_HARDWARE_TRACE is set
func stageTrace(stage string) {
	if os.Getenv("DISCOXIP_HARDWARE_TRACE") == "" {
		return
	}
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	fmt.Fprintf(os.Stderr, "hardware: %s, %d MB in use\n", stage, ms.HeapAlloc>>20)
}

// The address of an export by name
func (dv *deriver) export(name string) (uint32, error) {
	va, ok := dv.exports[name]
	if !ok {
		return 0, fmt.Errorf("bios: the kernel exports no %s", name)
	}
	return va, nil
}

// trace records what a concrete replay does to the hardware
type trace struct {
	stage   string
	order   int
	ports   []access
	msrs    []MSRAccess
	cpuid   []CPUIDUse
	crs     map[uint32]uint32
	answers map[uint32]uint32
	tsc     uint64
}

// Attaches the trace's hooks to a run, port reads answered as the init table expects them where it tested the port
func (t *trace) attach(r *xbe.MachineRun) {
	r.IO = func(r *xbe.MachineRun, out bool, port, value, size uint32) uint32 {
		t.order++
		a := access{stage: t.stage, port: true, addr: port, size: int(size), write: out, value: value, known: true, order: t.order}
		t.ports = append(t.ports, a)
		if out {
			return 0
		}
		return t.answers[port]
	}
	r.CPU = func(r *xbe.MachineRun, fn string, args []uint32) (uint32, bool) {
		switch {
		case fn == "cpu_wrmsr":
			t.msrs = append(t.msrs, MSRAccess{Stage: t.stage, Index: args[0], Write: true, Low: args[1], High: args[2]})
			return 0, true
		case fn == "cpu_rdmsr_lo" || fn == "cpu_rdmsr_hi":
			if fn == "cpu_rdmsr_lo" {
				t.msrs = append(t.msrs, MSRAccess{Stage: t.stage, Index: args[0]})
			}
			return 0, true
		case fn == "cpu_rdtsc_lo":
			t.tsc += 1 << 20
			return uint32(t.tsc), true
		case fn == "cpu_rdtsc_hi":
			return uint32(t.tsc >> 32), true
		case fn == "cpu_write_cr":
			t.crs[args[0]] = args[1]
			return 0, true
		case fn == "cpu_read_cr":
			return t.crs[args[0]], true
		case strings.HasPrefix(fn, "cpu_cpuid_"):
			if strings.HasSuffix(fn, "EAX") {
				t.cpuid = append(t.cpuid, CPUIDUse{Stage: t.stage, Leaf: args[0], Tests: []RegisterTest{}})
			}
			return 0, true
		case fn == "cpu_lgdt" || fn == "cpu_lidt" || fn == "cpu_ltr" || fn == "cpu_invlpg" || fn == "cpu_fninit" || fn == "cpu_fnclex" || fn == "cpu_fldcw" || fn == "cpu_fnstcw" || fn == "cpu_clts":
			return 0, true
		}
		return 0, false
	}
}

// The run a kernel replay starts in, with image header, the 2BL's paging, scratch stack and at once hooks
func (dv *deriver) kernelRun(t *trace) *xbe.MachineRun {
	run := xbe.NewMachineRun(dv.lifted.Model, dv.lifted.Kernel, map[string]uint32{"ESP": xbe.ReplayStack})
	run.Preload(dv.lifted.Kernel.BaseAddr, dv.lifted.Kernel.HeaderBytes())
	dv.mapPaging(run)
	run.Push(xbe.ReplayReturn)
	run.Stop[xbe.ReplayReturn] = true
	for va, hook := range dv.atOnce {
		run.Hooks[va] = hook
	}
	t.attach(run)
	return run
}

// Replays a kernel routine that takes no arguments, reporting what stops it
func (dv *deriver) replay(t *trace, entry uint32) (*xbe.MachineRun, error) {
	run := dv.kernelRun(t)
	if _, err := run.Run(entry); err != nil {
		return nil, fmt.Errorf("bios: replaying %08X: %w", entry, err)
	}
	return run, nil
}

// The routines of the machine lift that a call instruction of the kernel reaches, in address order
func (dv *deriver) calledRoutines() []uint32 {
	targets := dv.lifted.KernelDis.CallTargets()
	var out []uint32
	for _, entry := range dv.lifted.Model.Routines() {
		if targets[entry] {
			out = append(out, entry)
		}
	}
	return out
}

// Keeps the port trace of a replayed kernel routine and marks its blocks as covered by it
func (dv *deriver) replayedPorts(entry uint32, t *trace) {
	for _, va := range dv.lifted.Model.RoutineBlockVAs(entry) {
		dv.replayed[va] = true
	}
	dv.collectPortTrace(t.ports)
}

// The status the init table waits for on a port, the value its test passes unchanged, which a replay answers
// reads of that port with
func (dv *deriver) initAnswers() map[uint32]uint32 {
	answers := map[uint32]uint32{}
	for _, r := range dv.initTable.Reads {
		if r.Space != SpacePort {
			continue
		}
		for _, test := range r.Tests {
			if test.Value&test.And|test.Or == test.Value {
				answers[r.Address] = test.Value
			}
		}
	}
	return answers
}

func (dv *deriver) newTrace(stage string) *trace {
	return &trace{stage: stage, crs: map[uint32]uint32{}, answers: dv.initAnswers()}
}

// RAM is what the kernel's page frame initialization counts, replayed from the routines that count the pages the
// memory statistics report
func (dv *deriver) deriveMemory() (uint32, error) {
	stats, err := dv.export(exportStatistics)
	if err != nil {
		return 0, err
	}
	var global uint32
	for _, e := range dv.kernel.effects(stats) {
		if e.Kind != "store" || e.Size != 4 || e.Addr == nil {
			continue
		}
		bin, ok := e.Addr.(xbe.Bin)
		if !ok || bin.Op != "add" {
			continue
		}
		if off, isConst := xbe.ConstOf(bin.B); !isConst || off != statisticsPagesField {
			continue
		}
		if _, isInput := inputName(bin.A); !isInput {
			continue
		}
		ld, isLoad := e.Value.(xbe.Load)
		if !isLoad || ld.Base != "" || ld.Addr != nil || ld.Size != 4 {
			return 0, fmt.Errorf("bios: %s reports the page count from %s, not a global", exportStatistics, xbe.ExprString(e.Value))
		}
		global = uint32(ld.Off)
	}
	if global == 0 {
		return 0, fmt.Errorf("bios: %s stores no global at offset %d of its statistics", exportStatistics, statisticsPagesField)
	}
	d := dv.lifted.KernelDis
	counting := map[uint32]bool{}
	for _, site := range dv.lifted.Model.StoresAt(global, 4) {
		bin, isBin := site.Value.(xbe.Bin)
		if !isBin || bin.Op != "add" || !mentions(bin.A, xbe.Load{Off: int32(global), Size: 4}) {
			continue
		}
		if fn := d.FunctionAt(site.VA); fn != nil {
			counting[fn.EntryVA] = true
		}
	}
	var roots []uint32
	for entry := range counting {
		called := false
		for other := range counting {
			for _, callee := range d.Functions[other].CallTargets {
				if callee == entry {
					called = true
				}
			}
		}
		if !called {
			roots = append(roots, entry)
		}
	}
	sort.Slice(roots, func(i, j int) bool { return roots[i] < roots[j] })
	if len(roots) == 0 {
		return 0, fmt.Errorf("bios: no kernel routine counts pages into %08X", global)
	}
	t := dv.newTrace(StageKernel)
	run := dv.kernelRun(t)
	for _, root := range roots {
		run.Push(xbe.ReplayReturn)
		if _, err := run.Run(root); err != nil {
			return 0, fmt.Errorf("bios: replaying the page frame initialization at %08X: %w", root, err)
		}
	}
	pages := run.ReadU32(global)
	if pages == 0 {
		return 0, fmt.Errorf("bios: the page frame initialization counted no pages")
	}
	return pages * pageSize, nil
}

// The stall is the export spinning the loops its microseconds multiply by, a constant or the global holding one,
// the microseconds the stack slot the multiply reads and the cleanup what the export's return removes
func (dv *deriver) deriveStall() (Stall, error) {
	stall, err := dv.export(exportStall)
	if err != nil {
		return Stall{}, err
	}
	var scale uint32
	found, argument := false, -1
	visit := func(n xbe.Expr) {
		bin, ok := n.(xbe.Bin)
		if !ok || bin.Op != "mul" {
			return
		}
		for _, side := range [][2]xbe.Expr{{bin.A, bin.B}, {bin.B, bin.A}} {
			name, isInput := inputName(side[1])
			if !isInput {
				continue
			}
			if c, isConst := xbe.ConstOf(side[0]); isConst {
				scale, found = c, true
			}
			if ld, isLoad := side[0].(xbe.Load); isLoad && ld.Base == "" && ld.Addr == nil && ld.Size == 4 {
				scale, found = dv.lifted.Kernel.ReadU32(uint32(ld.Off))
			}
			if found {
				fmt.Sscanf(name, "stack:%d", &argument)
			}
		}
	}
	for _, p := range dv.kernel.run(stall) {
		for _, g := range p.Guards {
			xbe.WalkExpr(g, visit)
		}
		for _, e := range p.Effects {
			for _, a := range e.Args {
				xbe.WalkExpr(a, visit)
			}
			xbe.WalkExpr(e.Value, visit)
		}
	}
	if !found || scale == 0 {
		return Stall{}, fmt.Errorf("bios: %s multiplies its microseconds by nothing fixed", exportStall)
	}
	if argument < 0 {
		return Stall{}, fmt.Errorf("bios: %s multiplies no stack slot by its scale", exportStall)
	}
	cleanup := dv.lifted.KernelDis.Cleanup(stall)
	if cleanup < 4*(argument+1) {
		return Stall{}, fmt.Errorf("bios: %s removes %d bytes on return, fewer than its microseconds slot %d needs", exportStall, cleanup, argument)
	}
	return Stall{Entry: stall, Argument: argument, Scale: scale, Cleanup: cleanup}, nil
}

// The timer is the divisor the kernel programs into the interval timer, the time it adds per tick, and the port
// and rate of the counter it reports
func (dv *deriver) deriveTimer() (Timer, error) {
	mm := dv.lifted.Model
	var programmer uint32
	for _, entry := range dv.calledRoutines() {
		for _, e := range mm.RoutineEffects(entry) {
			if e.Kind == "call" && e.Fn == "port_out" {
				if port, ok := xbe.ConstOf(e.Args[0]); ok && port == pitCommandPort {
					programmer = entry
				}
			}
		}
	}
	if programmer == 0 {
		return Timer{}, fmt.Errorf("bios: no kernel routine programs the interval timer")
	}
	t := dv.newTrace(StageKernel)
	if _, err := dv.replay(t, programmer); err != nil {
		return Timer{}, err
	}
	dv.replayedPorts(programmer, t)
	var bytes []uint32
	for _, a := range t.ports {
		if a.write && a.addr == pitChannel0Port {
			bytes = append(bytes, a.value&0xFF)
		}
	}
	if len(bytes) != 2 {
		return Timer{}, fmt.Errorf("bios: the interval timer routine writes %d bytes of divisor", len(bytes))
	}
	timer := Timer{Divisor: bytes[0] | bytes[1]<<8}
	increment, err := dv.export(exportTimeIncrement)
	if err != nil {
		return Timer{}, err
	}
	if timer.TimeIncrement, _ = dv.lifted.Kernel.ReadU32(increment); timer.TimeIncrement == 0 {
		return Timer{}, fmt.Errorf("bios: %s is zero", exportTimeIncrement)
	}
	timer.Hz = uint32(uint64(timer.Divisor) * hundredNsPerSec / uint64(timer.TimeIncrement))
	frequency, err := dv.export(exportFrequency)
	if err != nil {
		return Timer{}, err
	}
	for _, p := range dv.kernel.run(frequency) {
		if c, ok := xbe.ConstOf(p.Return); ok {
			timer.CounterHz = c
		}
	}
	if timer.CounterHz == 0 {
		return Timer{}, fmt.Errorf("bios: %s returns no constant", exportFrequency)
	}
	counter, err := dv.export(exportCounter)
	if err != nil {
		return Timer{}, err
	}
	for _, e := range dv.kernel.effects(counter) {
		if e.Kind == "call" && e.Fn == "port_in" {
			if port, ok := xbe.ConstOf(e.Args[0]); ok {
				timer.CounterPort = port
			}
		}
	}
	if timer.CounterPort == 0 {
		return Timer{}, fmt.Errorf("bios: %s reads no fixed port", exportCounter)
	}
	return timer, nil
}

// The interrupt controllers are read off the port writes of the kernel routine that programs them through a table
// with string output, an initialization word opening each controller's sequence
func (dv *deriver) derivePICs() ([]PIC, error) {
	mm := dv.lifted.Model
	var pics []PIC
	for _, entry := range dv.calledRoutines() {
		strings := false
		for _, e := range mm.RoutineEffects(entry) {
			if e.Kind == "call" && e.Fn == "cpu_outs" {
				strings = true
			}
		}
		if !strings {
			continue
		}
		t := dv.newTrace(StageKernel)
		if _, err := dv.replay(t, entry); err != nil {
			continue
		}
		found := parsePICs(t.ports)
		if len(found) == 0 {
			continue
		}
		dv.replayedPorts(entry, t)
		for _, pic := range found {
			known := false
			for _, have := range pics {
				known = known || have == pic
			}
			if !known {
				pics = append(pics, pic)
			}
		}
	}
	if len(pics) == 0 {
		return nil, fmt.Errorf("bios: no kernel routine initializes an interrupt controller")
	}
	sort.Slice(pics, func(i, j int) bool { return pics[i].Command < pics[j].Command })
	return pics, nil
}

// Reads the initialization sequences out of a port trace, each opened by a word with the initialization bit on
// the command port and continued on the port above it
func parsePICs(ports []access) []PIC {
	var out []PIC
	for i := 0; i < len(ports); i++ {
		a := ports[i]
		if !a.write || a.value&picInitBit == 0 {
			continue
		}
		pic := PIC{Command: a.addr, Data: a.addr + 1, ICW1: uint8(a.value)}
		want := 1
		if pic.ICW1&picSingleBit == 0 {
			want++
		}
		if pic.ICW1&picICW4Bit != 0 {
			want++
		}
		var words []uint8
		j := i + 1
		for ; j < len(ports) && len(words) < want+1; j++ {
			if ports[j].write && ports[j].addr == pic.Data {
				words = append(words, uint8(ports[j].value))
			}
		}
		if len(words) < want {
			continue
		}
		pic.VectorBase = words[0]
		rest := words[1:]
		if pic.ICW1&picSingleBit == 0 {
			pic.Cascade, rest = rest[0], rest[1:]
		}
		if pic.ICW1&picICW4Bit != 0 {
			pic.ICW4, rest = rest[0], rest[1:]
		}
		if len(rest) > 0 {
			pic.Mask = rest[0]
		}
		out = append(out, pic)
		i = j - 1
	}
	return out
}

// Replays the 2BL from its entry to the kernel load, as the loader runs it, tracing the hardware it touches
func (dv *deriver) replayBldr() error {
	boot, err := dv.bank.Boot()
	if err != nil {
		return err
	}
	t := dv.newTrace(StageBldr)
	run := xbe.NewMachineRun(dv.lifted.Model, dv.lifted.Kernel, boot.Regs)
	for name, v := range boot.Segments {
		run.Segments[name] = v
	}
	t.attach(run)
	run.Stop[boot.Kernel.VA] = true
	run.Preload(boot.Flash.At, dv.bank.Data)
	cpu := run.CPU
	run.CPU = func(r *xbe.MachineRun, fn string, args []uint32) (uint32, bool) {
		v, answered := cpu(r, fn, args)
		if fn == "cpu_write_cr" && args[0] == 3 {
			dv.directory = args[1]
			dv.flash = dv.flashWindows(r, t)
			for _, window := range dv.flash {
				r.Overlays = append(r.Overlays, xbe.Overlay{Base: window, Size: 1 << pageDirectoryVA, Data: dv.bank.Data})
			}
		}
		return v, answered
	}
	if _, err := run.Run(boot.Entry); err != nil {
		return fmt.Errorf("bios: replaying the 2BL: %w", err)
	}
	dv.bldrTrace, dv.bldrRun = t, run
	for i := uint32(0); i < pageSize/4; i++ {
		entry := run.ReadU32(dv.directory + 4*i)
		if entry&1 != 0 && entry&pageDirectoryPS == 0 && entry&^0xFFF == dv.directory {
			dv.selfMap = i << pageDirectoryVA
		}
	}
	if dv.selfMap == 0 {
		return fmt.Errorf("bios: the 2BL's page directory at %08X maps no window onto itself", dv.directory)
	}
	return nil
}

// The large pages of the directory a run built that hold the range the 2BL marked write protected
func (dv *deriver) flashWindows(run *xbe.MachineRun, t *trace) []uint32 {
	var protected []uint32
	dv.rom = nil
	for _, m := range t.msrs {
		if !m.Write || m.Index < mtrrPhysBase0 || (m.Index-mtrrPhysBase0)%2 != 0 || m.Low&mtrrTypeMask != mtrrWriteProtect {
			continue
		}
		base := m.Low &^ uint32(0xFFF)
		protected = append(protected, base)
		for _, mask := range t.msrs {
			if mask.Write && mask.Index == m.Index+1 {
				dv.rom = append(dv.rom, romRange{base: base, size: ^mask.Low&^uint32(0xFFF) + 0x1000})
			}
		}
	}
	var out []uint32
	for i := uint32(0); i < pageSize/4; i++ {
		entry := run.ReadU32(dv.directory + 4*i)
		if entry&1 == 0 || entry&pageDirectoryPS == 0 {
			continue
		}
		base := entry &^ (1<<pageDirectoryVA - 1)
		for _, p := range protected {
			if p >= base && p-base < 1<<pageDirectoryVA {
				out = append(out, base)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// The flash windows are the large pages the 2BL mapped over the range it marked write protected
func (dv *deriver) deriveFlash() ([]uint32, error) {
	if len(dv.flash) == 0 {
		return nil, fmt.Errorf("bios: the 2BL maps no page over a write protected range")
	}
	return dv.flash, nil
}

// The processor features the boot executes and the registers it programs, from the 2BL's replay and the kernel's
// blocks, an argument the block leaves open resolved through the lift of its routine
func (dv *deriver) deriveProcessor() ([]CPUIDUse, []MSRAccess, error) {
	cpuid := []CPUIDUse{}
	msrs := []MSRAccess{}
	seenMSR := map[MSRAccess]bool{}
	addMSR := func(m MSRAccess) {
		if !seenMSR[m] {
			seenMSR[m] = true
			msrs = append(msrs, m)
		}
	}
	for _, m := range dv.bldrTrace.msrs {
		addMSR(m)
	}
	cpuid = append(cpuid, dv.bldrTrace.cpuid...)
	var failed error
	dv.lifted.Model.EachBlock(func(b xbe.BlockInfo) {
		if dv.stageOf(b.VA) != StageKernel || !dv.kernelCode(b.VA) || failed != nil {
			return
		}
		for _, e := range b.Effects {
			if e.Kind != "call" {
				continue
			}
			switch {
			case e.Fn == "cpu_wrmsr":
				for _, args := range dv.resolvedArgs(b.VA, e, 3) {
					m, err := dv.msrWrite(args)
					if err != nil {
						failed = fmt.Errorf("bios: the register write at %08X: %w", e.VA, err)
						return
					}
					addMSR(m)
				}
			case e.Fn == "cpu_rdmsr_lo":
				for _, args := range dv.resolvedArgs(b.VA, e, 1) {
					index, ok := xbe.ConstOf(args[0])
					if !ok {
						failed = fmt.Errorf("bios: the register read at %08X names register %s, which no caller fixes", e.VA, xbe.ExprString(args[0]))
						return
					}
					addMSR(MSRAccess{Stage: StageKernel, Index: index})
				}
			case e.Fn == "cpu_cpuid_EAX":
				leaf, ok := xbe.ConstOf(e.Args[0])
				if !ok {
					failed = fmt.Errorf("bios: block %08X identifies the processor with leaf %s", b.VA, xbe.ExprString(e.Args[0]))
					return
				}
				use := CPUIDUse{Stage: StageKernel, Leaf: leaf, Tests: []RegisterTest{}}
				for _, reg := range []string{"EAX", "EBX", "ECX", "EDX"} {
					result := xbe.CallResult{Fn: "cpu_cpuid_" + reg, Args: e.Args, Seq: e.Seq + indexOf(reg)}
					use.Tests = append(use.Tests, registerTests(b.Cond, result, reg)...)
				}
				cpuid = append(cpuid, use)
			}
		}
	})
	if failed != nil {
		return nil, nil, failed
	}
	return mergeCPUID(cpuid), msrs, nil
}

// The arguments of a block's call as every caller fixes them, the block's own values where it fixes them, else the
// lift of its routine resolved through the callers
func (dv *deriver) resolvedArgs(va uint32, e xbe.Effect, n int) [][]xbe.Expr {
	own := make([]xbe.Expr, n)
	open := false
	state := dv.blockState(va)
	for i := range own {
		if i < len(e.Args) {
			own[i] = dv.inState(e.Args[i], state)
		} else {
			own[i] = xbe.IConst{}
		}
		if _, ok := xbe.ConstOf(own[i]); !ok {
			open = true
		}
	}
	if !open {
		return [][]xbe.Expr{own}
	}
	routine, effects := dv.kernel.effectsAt(e.VA, e.Fn)
	if len(effects) == 0 {
		return [][]xbe.Expr{own}
	}
	var out, unresolved [][]xbe.Expr
	for _, pe := range effects {
		for _, b := range append([]binding{{}}, dv.kernel.bindings(routine, resolveDepth)...) {
			args := make([]xbe.Expr, n)
			open := false
			for i := range args {
				if i < len(pe.Args) {
					args[i] = dv.kernel.globals(substitute(pe.Args[i], b))
				} else {
					args[i] = xbe.IConst{}
				}
				open = open || readsInputs(args[i])
			}
			if open {
				unresolved = append(unresolved, args)
			} else {
				out = append(out, args)
			}
		}
	}
	if len(out) == 0 {
		return unresolved
	}
	return out
}

// A model specific register write from its resolved arguments, the value fixed or the register's reading masked
func (dv *deriver) msrWrite(args []xbe.Expr) (MSRAccess, error) {
	index, ok := xbe.ConstOf(args[0])
	if !ok {
		return MSRAccess{}, fmt.Errorf("names register %s, which no caller fixes", xbe.ExprString(args[0]))
	}
	m := MSRAccess{Stage: StageKernel, Index: index, Write: true}
	lowSeq := -1
	if c, isConst := xbe.ConstOf(args[1]); isConst {
		m.Low = c
	} else {
		base, and, or := readMask(args[1])
		seq, reading := dv.kernel.readingOf(base, "cpu_rdmsr_lo", index, -1)
		if !reading {
			return MSRAccess{}, fmt.Errorf("writes register %#x with %s, which no caller fixes", index, xbe.ExprString(args[1]))
		}
		m.KeepLow, m.Low, lowSeq = and, or, seq
	}
	if c, isConst := xbe.ConstOf(args[2]); isConst {
		m.High = c
	} else {
		base, and, or := readMask(args[2])
		if _, reading := dv.kernel.readingOf(base, "cpu_rdmsr_hi", index, lowSeq); !reading {
			return MSRAccess{}, fmt.Errorf("writes register %#x with %s above, which no caller fixes", index, xbe.ExprString(args[2]))
		}
		m.KeepHigh, m.High = and, or
	}
	return m, nil
}

// Splits an expression into what it masks and the and and or masks applied
func readMask(e xbe.Expr) (base xbe.Expr, and, or uint32) {
	and = 0xFFFFFFFF
	for {
		bin, ok := e.(xbe.Bin)
		if !ok {
			return e, and, or
		}
		c, isConst := xbe.ConstOf(bin.B)
		if !isConst {
			return e, and, or
		}
		switch bin.Op {
		case "and":
			and &= c
			or &= c
		case "or":
			or |= c
		default:
			return e, and, or
		}
		e = bin.A
	}
}

// Reports whether an expression is a half of a register's reading, read in place or through a helper that only
// reads the register its caller names, the helper's call sequence reported so its other half is known by it
func (l *lifter) readingOf(e xbe.Expr, fn string, index uint32, lowSeq int) (int, bool) {
	switch v := e.(type) {
	case xbe.Sym:
		return lowSeq, lowSeq >= 0 && v.Name == fmt.Sprintf("clobber.edx#%d", lowSeq)
	case xbe.CallResult:
		if v.Fn == fn {
			idx, ok := xbe.ConstOf(v.Args[0])
			return v.Seq, ok && idx == index
		}
		var callee uint32
		if !scanSub(v.Fn, &callee) {
			return 0, false
		}
		b := l.bindResult(callee, v)
		paths := l.run(callee)
		if len(paths) == 0 {
			return 0, false
		}
		for _, p := range paths {
			for _, pe := range p.Effects {
				if pe.Kind != "call" || pe.Fn != "cpu_rdmsr_lo" && pe.Fn != "cpu_rdmsr_hi" {
					return 0, false
				}
				if idx, ok := xbe.ConstOf(fold(substitute(pe.Args[0], b))); !ok || idx != index {
					return 0, false
				}
			}
			r, ok := p.Return.(xbe.CallResult)
			if !ok || r.Fn != "cpu_rdmsr_lo" {
				return 0, false
			}
		}
		return v.Seq, true
	}
	return 0, false
}

// The inputs a call hands a routine, read off the call's result rather than its effect
func (l *lifter) bindResult(callee uint32, r xbe.CallResult) binding {
	return l.bind(callee, xbe.Effect{Kind: "call", Fn: r.Fn, Args: r.Args, This: r.This, Target: r.Target})
}

// The stage a lifted block belongs to, the kernel's image or the 2BL
func (dv *deriver) stageOf(va uint32) string {
	if dv.stage != "" {
		return dv.stage
	}
	if dv.lifted.KernelDis.SectionOf(va) != nil {
		return StageKernel
	}
	return StageBldr
}

// Reports whether a kernel block lies in a routine the disassembly knows, itself or through the lifted routine
// holding it, a block lifted from a data word that only looked like a code address lying in none
func (dv *deriver) kernelCode(va uint32) bool {
	d := dv.lifted.KernelDis
	if d.FunctionAt(va) != nil {
		return true
	}
	entry, ok := dv.routineOf[va]
	return ok && d.FunctionAt(entry) != nil
}

// Position of an answer register in the order the lift records a cpuid's results
func indexOf(reg string) int {
	return map[string]int{"EAX": 0, "EBX": 1, "ECX": 2, "EDX": 3}[reg]
}

// The tests a condition applies to a cpuid answer register, a masked compare against a constant
func registerTests(g xbe.Expr, result xbe.CallResult, reg string) []RegisterTest {
	if g == nil {
		return nil
	}
	negate := false
	if n, ok := g.(xbe.Not); ok {
		g, negate = n.A, true
	}
	cond, ok := g.(xbe.Cond)
	if !ok {
		return nil
	}
	value, ok := xbe.ConstOf(cond.B)
	if !ok {
		return nil
	}
	mask := uint32(0xFFFFFFFF)
	subject := cond.A
	if bin, isBin := subject.(xbe.Bin); isBin && bin.Op == "and" {
		if m, isConst := xbe.ConstOf(bin.B); isConst {
			mask, subject = m, bin.A
		}
	}
	if !sameResult(subject, result) {
		return nil
	}
	op := cond.Op
	if negate {
		op = "not " + op
	}
	return []RegisterTest{{Register: reg, And: mask, Op: op, Value: value}}
}

// Reports whether an expression is the answer register of a cpuid, by the call it came from
func sameResult(e xbe.Expr, result xbe.CallResult) bool {
	r, ok := e.(xbe.CallResult)
	return ok && r.Fn == result.Fn && r.Seq == result.Seq
}

// Joins the uses of one leaf by one stage, their tests pooled
func mergeCPUID(uses []CPUIDUse) []CPUIDUse {
	var out []CPUIDUse
	for _, u := range uses {
		merged := false
		for i := range out {
			if out[i].Stage == u.Stage && out[i].Leaf == u.Leaf {
				for _, t := range u.Tests {
					if !containsTestOf(out[i].Tests, t) {
						out[i].Tests = append(out[i].Tests, t)
					}
				}
				merged = true
			}
		}
		if !merged {
			out = append(out, u)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Stage != out[j].Stage {
			return out[i].Stage == StageBldr
		}
		return out[i].Leaf < out[j].Leaf
	})
	return out
}

func containsTestOf(tests []RegisterTest, t RegisterTest) bool {
	for _, have := range tests {
		if have == t {
			return true
		}
	}
	return false
}

// Records a port or register access
func (dv *deriver) touch(a access) {
	a.order = len(dv.accesses)
	dv.accesses = append(dv.accesses, a)
}

// Records a configuration space access decoded from its address port value and data port access
func (dv *deriver) config(stage string, address uint32, dataPort uint32, size int, write bool, value uint32, known bool) {
	if address&pciEnableBit == 0 {
		return
	}
	key := pciKey{int(address >> 16 & 0xFF), int(address >> 11 & 0x1F), int(address >> 8 & 7)}
	register := int(address&0xFC) + int(dataPort-pciDataPort)
	c := ConfigAccess{Stage: stage, Register: register, Size: size, Write: write, Value: value, Known: known || !write}
	if !c.Known {
		c.Value = 0
	}
	for _, have := range dv.configs[key] {
		if have == c {
			return
		}
	}
	dv.configs[key] = append(dv.configs[key], c)
	if write && known && size == 4 && register >= pciFirstBAR && register <= pciLastBAR {
		base := value &^ 0xF
		io := value&pciBARIOBit != 0
		if io {
			base = value &^ 0x3
		}
		dv.bars = append(dv.bars, barAssignment{stage: stage, order: len(dv.accesses), fn: key, register: register, base: base, io: io})
	}
}

// The init table's accesses in table order, the memory it tests left out as RAM
func (dv *deriver) collectInitAccesses() {
	type event struct {
		at    int
		read  *InitRead
		write *InitWrite
	}
	var events []event
	for i := range dv.initTable.Reads {
		events = append(events, event{at: dv.initTable.Reads[i].At, read: &dv.initTable.Reads[i]})
	}
	for i := range dv.initTable.Writes {
		events = append(events, event{at: dv.initTable.Writes[i].At, write: &dv.initTable.Writes[i]})
	}
	sort.SliceStable(events, func(i, j int) bool { return events[i].at < events[j].at })
	tested := dv.initTable.memoryTests()
	for _, ev := range events {
		if r := ev.read; r != nil {
			switch r.Space {
			case SpacePort:
				dv.touch(access{stage: StageInit, port: true, addr: r.Address, size: 1})
			case SpaceMemory:
				if at, device := dv.deviceAddress(nil, "", int32(r.Address)); device && !tested[r.Address] {
					dv.touch(access{stage: StageInit, addr: at, size: 4})
				}
			case SpacePCI:
				dv.config(StageInit, r.Address, pciDataPort, 4, false, 0, true)
			}
			continue
		}
		w := ev.write
		switch w.Space {
		case SpacePort:
			dv.touch(access{stage: StageInit, port: true, addr: w.Address, size: 1, write: true, value: w.Value, known: w.Read < 0})
		case SpaceMemory:
			if at, device := dv.deviceAddress(nil, "", int32(w.Address)); device && !tested[w.Address] {
				dv.touch(access{stage: StageInit, addr: at, size: 4, write: true, value: w.Value, known: w.Read < 0})
			}
		case SpacePCI:
			dv.config(StageInit, w.Address, pciDataPort, 4, true, w.Value, w.Read < 0)
		}
	}
}

// The 2BL's port accesses from its replay, configuration accesses paired through the address port, and the registers
// it wrote read back out of the run's memory
func (dv *deriver) collectBldrAccesses() {
	dv.collectPortTrace(dv.bldrTrace.ports)
	written := dv.bldrRun.Written()
	addrs := make([]uint32, 0, len(written))
	for at := range written {
		addrs = append(addrs, at)
	}
	sort.Slice(addrs, func(i, j int) bool { return addrs[i] < addrs[j] })
	for i := 0; i < len(addrs); {
		at := addrs[i]
		size := 1
		for size < 4 && i+size < len(addrs) && addrs[i+size] == at+uint32(size) && (at+uint32(size))%4 != 0 {
			size++
		}
		if _, device := dv.deviceAddress(nil, "", int32(at)); device {
			var value uint32
			for k := 0; k < size; k++ {
				value |= uint32(written[at+uint32(k)]) << (8 * uint(k))
			}
			dv.touch(access{stage: StageBldr, addr: at, size: size, write: true, value: value, known: true})
		}
		i += size
	}
}

// Records a port trace, the data port accesses of configuration space decoded by the address written before them
func (dv *deriver) collectPortTrace(ports []access) {
	var address uint32
	for _, a := range ports {
		if a.write && a.addr == pciAddressPort && a.size == 4 {
			address = a.value
			continue
		}
		if a.addr >= pciDataPort && a.addr < pciDataPort+4 {
			dv.config(a.stage, address, a.addr, a.size, a.write, a.value, a.known)
			continue
		}
		dv.touch(a)
	}
}

// Reports whether a store or load address is a fixed device register, outside RAM, the kernel's page tables and
// the flash
func (dv *deriver) deviceAddress(addr xbe.Expr, base string, off int32) (uint32, bool) {
	at, ok := fixedAddress(addr, base, off)
	if !ok {
		return 0, false
	}
	if at < dv.memory || at >= 0x80000000 && at-0x80000000 < dv.memory {
		return 0, false
	}
	if at >= dv.selfMap && at-dv.selfMap < 1<<pageDirectoryVA {
		return 0, false
	}
	for _, window := range dv.flash {
		if at >= window && at-window < 1<<pageDirectoryVA {
			return 0, false
		}
	}
	return at, true
}

// The address a load or store fixes, by constant or as a global
func fixedAddress(addr xbe.Expr, base string, off int32) (uint32, bool) {
	if addr != nil {
		return xbe.ConstOf(fold(addr))
	}
	if base == "" {
		return uint32(off), true
	}
	return 0, false
}

// Blocks a walk from a branch's target visits before the target counts as not leading back to the branch, the
// depth the stores fixing an object field a poll's address is indexed by are searched to, and the entries a table
// indexed by a value nothing fixes may span
const (
	pollBodyBlocks   = 256
	pollFieldDepth   = 3
	pollTableEntries = 64
)

// The polls are the branches of the 2BL's, the kernel's or the executable's blocks whose one target leads back to them on a memory
// reading outside RAM, the reading and the compare resolved by the branch's flow state, each kept with the exit
// test for every address the reading may have or with the block alone where nothing fixes the address, each block's
// first lift alone counting, a branch both targets lead back to or one reading through a value the loop itself
// changes being a loop over something else
func (dv *deriver) derivePolls() ([]Poll, error) {
	out := []Poll{}
	var failed error
	dv.lifted.Model.EachBlock(func(b xbe.BlockInfo) {
		if b.Kind != "branch" || b.Taken == b.Fall {
			return
		}
		if first := dv.blocks[b.VA]; first.Taken != b.Taken || first.Fall != b.Fall || !xbe.SameExpr(first.Cond, b.Cond) {
			return
		}
		stage := dv.stageOf(b.VA)
		if stage != StageBldr && !dv.kernelCode(b.VA) {
			return
		}
		loop, back := dv.loopTarget(b)
		if back != 1 {
			return
		}
		_, routine := dv.flowOf(b.VA)
		cond := dv.inState(b.Cond, dv.blockState(b.VA))
		ld, p, ok := pollTest(cond, loop == b.Taken)
		if !ok {
			loads, err := dv.deviceLoads(cond, routine)
			if err != nil {
				failed = errors.Join(failed, fmt.Errorf("bios: block %08X loops on %s %w", b.VA, xbe.ExprStringShort(cond), err))
			} else if len(loads) > 0 {
				failed = errors.Join(failed, fmt.Errorf("bios: block %08X spins on register %s until %s, a test the derivation cannot fix", b.VA, xbe.ExprStringShort(loads[0].load), xbe.ExprStringShort(cond)))
			}
			return
		}
		if mentionsLost(ld.Addr) {
			return
		}
		if at, fixed := fixedAddress(ld.Addr, ld.Base, ld.Off); fixed {
			if _, isDevice := dv.deviceAddress(nil, "", int32(at)); !isDevice {
				return
			}
		}
		addresses := dv.registerAddresses(ld, routine)
		if len(addresses) == 0 && dv.namesDevice(ld.Addr) {
			failed = errors.Join(failed, fmt.Errorf("bios: block %08X spins reading %s, which the derivation cannot place", b.VA, xbe.ExprStringShort(ld)))
			return
		}
		p.Stage, p.Block = stage, b.VA
		if len(addresses) == 0 {
			out = append(out, p)
			return
		}
		for _, at := range addresses {
			p.Address = at
			if !slices.Contains(out, p) {
				out = append(out, p)
			}
		}
	})
	return out, failed
}

// deviceLoad is a reading of a device register in an expression with every address it may read
type deviceLoad struct {
	load      xbe.Load
	addresses []uint32
}

// The device registers an expression reads, one entry per set of addresses a load resolves to, a load through a
// value the loop itself changes left out, a load naming a device address by constant that resolves to none reported
func (dv *deriver) deviceLoads(e xbe.Expr, routine uint32) ([]deviceLoad, error) {
	var out []deviceLoad
	var failed error
	xbe.WalkExpr(e, func(n xbe.Expr) {
		ld, ok := n.(xbe.Load)
		if !ok || failed != nil || mentionsLost(ld.Addr) {
			return
		}
		addresses := dv.registerAddresses(ld, routine)
		if len(addresses) == 0 {
			if dv.namesDevice(ld.Addr) {
				failed = fmt.Errorf("reading %s, which the derivation cannot place", xbe.ExprString(ld))
			}
			return
		}
		for _, have := range out {
			if have.load.Size == ld.Size && slices.Equal(have.addresses, addresses) {
				return
			}
		}
		out = append(out, deviceLoad{load: ld, addresses: addresses})
	})
	return out, failed
}

// The device registers a load may read, in address order, a load whose address names no device constant reading none
func (dv *deriver) registerAddresses(ld xbe.Load, routine uint32) []uint32 {
	var candidates []uint32
	if at, ok := fixedAddress(ld.Addr, ld.Base, ld.Off); ok {
		candidates = []uint32{at}
	} else if dv.namesDevice(ld.Addr) {
		candidates = dv.valuesOf(ld.Addr, routine, pollFieldDepth)
		if len(candidates) == 0 {
			candidates = dv.tableSpan(ld.Addr)
		}
	}
	var out []uint32
	for _, c := range candidates {
		if at, isDevice := dv.deviceAddress(nil, "", int32(c)); isDevice {
			out = appendUnique(out, at)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// Reports whether an address expression names a device by constant, a device address inside a memory window a base
// address register opened
func (dv *deriver) namesDevice(addr xbe.Expr) bool {
	found := false
	if addr == nil {
		return false
	}
	xbe.WalkExpr(addr, func(n xbe.Expr) {
		if c, ok := n.(xbe.IConst); ok && dv.inWindow(c.V) {
			if _, isDevice := dv.deviceAddress(nil, "", int32(c.V)); isDevice {
				found = true
			}
		}
	})
	return found
}

// Reports whether an address lies within the span of a memory window a base address register opened
func (dv *deriver) inWindow(at uint32) bool {
	_, ok := dv.windowBase(at)
	return ok
}

// The base of the memory window an address falls in, the greatest base address register assignment below it within
// a window's span
func (dv *deriver) windowBase(at uint32) (uint32, bool) {
	var base uint32
	found := false
	for _, bar := range dv.bars {
		if !bar.io && at >= bar.base && at-bar.base < memoryWindowBytes && (!found || bar.base > base) {
			base, found = bar.base, true
		}
	}
	return base, found
}

// Reports whether an address lies within the reach of the constant accesses into its memory window
func (dv *deriver) inExtent(at uint32) bool {
	base, ok := dv.windowBase(at)
	if !ok {
		return false
	}
	if dv.extents == nil {
		dv.extents = map[uint32]uint32{}
		for _, a := range dv.accesses {
			if a.port {
				continue
			}
			if b, inside := dv.windowBase(a.addr); inside && a.addr+uint32(a.size)-b > dv.extents[b] {
				dv.extents[b] = a.addr + uint32(a.size) - b
			}
		}
	}
	return at-base < dv.extents[base]
}

// The addresses an offset from a word table indexed by a value nothing fixes may form, the table's leading entries
// whose addresses stay within the reach of the constant accesses into the window
func (dv *deriver) tableSpan(addr xbe.Expr) []uint32 {
	inner, off := splitOffset(addr)
	ld, ok := inner.(xbe.Load)
	if !ok || ld.Addr == nil || ld.Size != 4 {
		return nil
	}
	table, _, ok := tableIndex(ld.Addr)
	if !ok {
		return nil
	}
	var out []uint32
	for i := uint32(0); i < pollTableEntries; i++ {
		w, read := dv.lifted.Kernel.ReadU32(table + 4*i)
		if !read || !dv.inExtent(w+off) {
			break
		}
		out = appendUnique(out, w+off)
	}
	return out
}

// The constants an expression may take, an offset from an inner value's, a word of an image table indexed by an
// inner value, or what the kernel stores into a field of an object the routine is handed
func (dv *deriver) valuesOf(e xbe.Expr, routine uint32, depth int) []uint32 {
	if c, ok := xbe.ConstOf(e); ok {
		return []uint32{c}
	}
	switch v := e.(type) {
	case xbe.Bin:
		if v.Op != "add" && v.Op != "sub" {
			return nil
		}
		c, ok := xbe.ConstOf(v.B)
		inner := v.A
		if !ok && v.Op == "add" {
			c, ok = xbe.ConstOf(v.A)
			inner = v.B
		}
		if !ok {
			return nil
		}
		var out []uint32
		for _, x := range dv.valuesOf(inner, routine, depth) {
			if v.Op == "sub" {
				out = appendUnique(out, x-c)
			} else {
				out = appendUnique(out, x+c)
			}
		}
		return out
	case xbe.Load:
		if v.Addr == nil || v.Size != 4 {
			return nil
		}
		if table, index, ok := tableIndex(v.Addr); ok {
			var out []uint32
			for _, i := range dv.valuesOf(index, routine, depth) {
				if w, read := dv.lifted.Kernel.ReadU32(table + 4*i); read {
					out = appendUnique(out, w)
				}
				for _, w := range dv.kernel.globalValues(table+4*i, 4) {
					out = appendUnique(out, w)
				}
			}
			return out
		}
		if base, off := splitOffset(v.Addr); readsInputs(base) {
			return dv.kernel.fieldValues(routine, base, off, depth)
		}
	}
	return nil
}

// A word table indexed by a value scaled to words, the table's address and the index
func tableIndex(addr xbe.Expr) (uint32, xbe.Expr, bool) {
	bin, ok := addr.(xbe.Bin)
	if !ok || bin.Op != "add" {
		return 0, nil, false
	}
	table, ok := xbe.ConstOf(bin.B)
	scaled := bin.A
	if !ok {
		table, ok = xbe.ConstOf(bin.A)
		scaled = bin.B
	}
	s, isBin := scaled.(xbe.Bin)
	if !ok || !isBin {
		return 0, nil, false
	}
	if c, isConst := xbe.ConstOf(s.B); isConst && (s.Op == "mul" && c == 4 || s.Op == "shl" && c == 2) {
		return table, s.A, true
	}
	if c, isConst := xbe.ConstOf(s.A); isConst && s.Op == "mul" && c == 4 {
		return table, s.B, true
	}
	return 0, nil, false
}

// The target of a branch leading back to it and how many do, the branch itself where it is a target, else a target
// from which the branch is reached again through blocks that write no device register
func (dv *deriver) loopTarget(b xbe.BlockInfo) (uint32, int) {
	for _, t := range []uint32{b.Taken, b.Fall} {
		if t == b.VA {
			return t, 1
		}
	}
	var loop uint32
	back := 0
	for _, t := range []uint32{b.Taken, b.Fall} {
		if dv.leadsBack(t, b.VA) {
			loop, back = t, back+1
		}
	}
	return loop, back
}

// Reports whether a block leads back to another through blocks that write no device register, the walk following
// jumps, branches, switches and the returns of calls
func (dv *deriver) leadsBack(from, to uint32) bool {
	seen := map[uint32]bool{}
	queue := []uint32{from}
	for len(queue) > 0 && len(seen) < pollBodyBlocks {
		va := queue[0]
		queue = queue[1:]
		if va == to {
			return true
		}
		b, ok := dv.blocks[va]
		if seen[va] || !ok || dv.storesDevice(b) {
			continue
		}
		seen[va] = true
		succs, _ := blockSuccessors(b)
		queue = append(queue, succs...)
	}
	return false
}

// Reports whether a block stores to a device register it names by constant
func (dv *deriver) storesDevice(b xbe.BlockInfo) bool {
	for _, e := range b.Effects {
		if e.Kind != "store" {
			continue
		}
		if _, ok := dv.deviceAddress(e.Addr, e.Base, e.Off); ok {
			return true
		}
	}
	return false
}

// The test a poll exits on and the reading it tests, the reading cut to a mask and compared against a constant, an
// extension or an or of bits outside the mask dropped where the outcome cannot change, a shift right folded into
// the mask and the constant, a bound nothing fixes replaced by the most it can hold where the loop exits above it,
// the compare negated where the loop runs while it holds
func pollTest(cond xbe.Expr, runsWhileTrue bool) (xbe.Load, Poll, bool) {
	negate := runsWhileTrue
	if n, ok := cond.(xbe.Not); ok {
		cond, negate = n.A, !negate
	}
	c, ok := cond.(xbe.Cond)
	if !ok {
		return xbe.Load{}, Poll{}, false
	}
	op := c.Op
	value, ok := xbe.ConstOf(c.B)
	if !ok {
		exit := op
		if negate {
			exit = "not " + exit
		}
		if exit != "uge" && exit != "not ult" && exit != "ugt" && exit != "not ule" {
			return xbe.Load{}, Poll{}, false
		}
		if value, ok = mostOf(c.B); !ok {
			return xbe.Load{}, Poll{}, false
		}
	}
	mask, subject := uint32(0xFFFFFFFF), c.A
	for {
		switch v := subject.(type) {
		case xbe.Bin:
			switch v.Op {
			case "shr":
				by, isConst := xbe.ConstOf(v.B)
				if !isConst || by == 0 || by >= 32 || value>>(32-by) != 0 || !unsignedOp(op) {
					return xbe.Load{}, Poll{}, false
				}
				mask, value, subject = mask<<by, value<<by, v.A
			case "and":
				m, isConst := xbe.ConstOf(v.B)
				other := v.A
				if !isConst {
					m, isConst = xbe.ConstOf(v.A)
					other = v.B
				}
				if !isConst {
					return xbe.Load{}, Poll{}, false
				}
				mask, subject = mask&m, other
			case "or":
				a, okA := bitsOf(v.A)
				b, okB := bitsOf(v.B)
				switch {
				case okA && a&mask == 0:
					subject = v.B
				case okB && b&mask == 0:
					subject = v.A
				default:
					return xbe.Load{}, Poll{}, false
				}
			default:
				return xbe.Load{}, Poll{}, false
			}
		case xbe.Un:
			low, signed, isExtension := extensionOf(v.Op)
			if !isExtension {
				return xbe.Load{}, Poll{}, false
			}
			if signed && mask&^low != 0 {
				if mask != 0xFFFFFFFF || op != "eq" && op != "ne" || value != signExtend(value, low) {
					return xbe.Load{}, Poll{}, false
				}
				value &= low
			}
			mask, subject = mask&low, v.A
		case xbe.Load:
			if negate {
				op = "not " + op
			}
			return v, Poll{Size: v.Size, And: mask, Op: op, Value: value}, true
		default:
			return xbe.Load{}, Poll{}, false
		}
	}
}

// Reports whether a compare is unsigned or an equality, the compares a shift of both sides keeps
func unsignedOp(op string) bool {
	switch op {
	case "eq", "ne", "ult", "ule", "ugt", "uge":
		return true
	}
	return false
}

// The most a value can hold where its width bounds it below the word, a constant's own, a load's or extension's
// width, the sum, product or shift of what its parts hold, a mask's bound, a value the word does not bound none
func mostOf(e xbe.Expr) (uint32, bool) {
	const word = uint32(0xFFFFFFFF)
	if c, ok := xbe.ConstOf(e); ok {
		return c, true
	}
	switch v := e.(type) {
	case xbe.Load:
		if v.Size < 4 {
			return 1<<(8*uint(v.Size)) - 1, true
		}
	case xbe.Un:
		if low, signed, ok := extensionOf(v.Op); ok && !signed {
			return low, true
		}
	case xbe.Bin:
		a, okA := mostOf(v.A)
		b, okB := mostOf(v.B)
		switch v.Op {
		case "add":
			if sum := uint64(a) + uint64(b); okA && okB && sum < uint64(word) {
				return uint32(sum), true
			}
		case "mul":
			if product := uint64(a) * uint64(b); okA && okB && product < uint64(word) {
				return uint32(product), true
			}
		case "shl":
			if c, ok := xbe.ConstOf(v.B); ok && okA && c < 32 && a>>(32-c) == 0 && a<<c != word {
				return a << c, true
			}
		case "shr":
			if c, ok := xbe.ConstOf(v.B); ok && c > 0 && c < 32 {
				if !okA {
					a = word
				}
				return a >> c, true
			}
		case "and":
			if okA || okB {
				if !okA {
					a = word
				}
				if !okB {
					b = word
				}
				return min(a, b), true
			}
		case "or":
			if okA && okB && onesThrough(a)|onesThrough(b) != word {
				return onesThrough(a) | onesThrough(b), true
			}
		}
	}
	return word, false
}

// All ones up to the highest bit set in a value
func onesThrough(v uint32) uint32 {
	for bit := uint32(1); bit != 0 && bit <= v; bit <<= 1 {
		v |= bit - 1
	}
	return v
}

// The bits a value may hold, a constant's own, the mask a value is cut to, the width a zero extension keeps or the
// union of what an or joins
func bitsOf(e xbe.Expr) (uint32, bool) {
	if m, ok := maskOf(e); ok {
		return m, true
	}
	switch v := e.(type) {
	case xbe.Un:
		if low, signed, ok := extensionOf(v.Op); ok && !signed {
			return low, true
		}
	case xbe.Bin:
		if v.Op == "or" {
			a, okA := bitsOf(v.A)
			b, okB := bitsOf(v.B)
			if okA && okB {
				return a | b, true
			}
		}
	}
	return 0, false
}

// The bits an extension keeps and whether it copies the sign, for the zero and sign extensions
func extensionOf(op string) (low uint32, signed, ok bool) {
	switch op {
	case "zx8":
		return 0xFF, false, true
	case "zx16":
		return 0xFFFF, false, true
	case "sx8":
		return 0xFF, true, true
	case "sx16":
		return 0xFFFF, true, true
	}
	return 0, false, false
}

// A value's low bits sign extended, the sign the highest bit kept
func signExtend(v, low uint32) uint32 {
	if v&((low+1)>>1) != 0 {
		return v | ^low
	}
	return v & low
}

// The registers and ports every lifted block of the kernel and the 2BL names by constant, a port a kernel block
// leaves open resolved through the callers of its routine, and configuration space read off the routines that
// write the address port
func (dv *deriver) collectBlockAccesses() error {
	var failed error
	configRoutines := map[uint32]bool{}
	dv.lifted.Model.EachBlock(func(b xbe.BlockInfo) {
		if failed != nil {
			return
		}
		stage := dv.stageOf(b.VA)
		if stage == StageKernel && !dv.kernelCode(b.VA) {
			if os.Getenv("DISCOXIP_HARDWARE_TRACE") != "" {
				fmt.Fprintf(os.Stderr, "hardware: block %08X lies in no routine of the kernel\n", b.VA)
			}
			return
		}
		for _, e := range b.Effects {
			switch e.Kind {
			case "store":
				if at, ok := dv.deviceAddress(e.Addr, e.Base, e.Off); ok {
					v, known := xbe.ConstOf(fold(e.Value))
					dv.touch(access{stage: stage, addr: at, size: e.Size, write: true, value: v, known: known})
				}
				dv.collectLoads(stage, e.Value)
			case "call":
				for _, a := range e.Args {
					dv.collectLoads(stage, a)
				}
				if e.Fn != "port_in" && e.Fn != "port_out" || stage != StageKernel || dv.replayed[b.VA] {
					continue
				}
				if err := dv.blockPort(b.VA, e, configRoutines); err != nil {
					failed = err
				}
			}
		}
		dv.collectLoads(stage, b.Cond)
	})
	if failed != nil {
		return failed
	}
	routines := make([]uint32, 0, len(configRoutines))
	for entry := range configRoutines {
		routines = append(routines, entry)
	}
	sort.Slice(routines, func(i, j int) bool { return routines[i] < routines[j] })
	for _, entry := range routines {
		if err := dv.collectConfigPaths(entry); err != nil {
			return err
		}
	}
	return nil
}

// Records the loads of device registers inside an expression
func (dv *deriver) collectLoads(stage string, e xbe.Expr) {
	if e == nil {
		return
	}
	xbe.WalkExpr(e, func(n xbe.Expr) {
		ld, ok := n.(xbe.Load)
		if !ok {
			return
		}
		if at, isDevice := dv.deviceAddress(ld.Addr, ld.Base, ld.Off); isDevice {
			dv.touch(access{stage: stage, addr: at, size: ld.Size})
		}
	})
}

// Records one port access of a kernel block, the configuration ports left to their routine's paths and a port
// the block leaves open resolved through the callers of the routine
func (dv *deriver) blockPort(va uint32, e xbe.Effect, configRoutines map[uint32]bool) error {
	write := e.Fn == "port_out"
	state := dv.blockState(va)
	port, portKnown := xbe.ConstOf(dv.inState(e.Args[0], state))
	var value, size uint32
	known := true
	if write {
		value, known = xbe.ConstOf(dv.inState(e.Args[1], state))
		size, _ = xbe.ConstOf(e.Args[2])
	} else {
		size, _ = xbe.ConstOf(e.Args[1])
	}
	if portKnown && (port == pciAddressPort || port >= pciDataPort && port < pciDataPort+4) {
		if fn := dv.lifted.KernelDis.FunctionAt(e.VA); fn != nil {
			configRoutines[fn.EntryVA] = true
		}
		return nil
	}
	if portKnown && (!write || known) {
		dv.touch(access{stage: StageKernel, port: true, addr: port, size: int(size), write: write, value: value, known: known})
		return nil
	}
	routine, effects := dv.kernel.effectsAt(e.VA, e.Fn)
	if len(effects) == 0 {
		if portKnown {
			dv.touch(access{stage: StageKernel, port: true, addr: port, size: int(size), write: write, known: false})
			return nil
		}
		return fmt.Errorf("bios: the port access at %08X reaches %s and the lift of its routine %08X records no %s: %v", e.VA, xbe.ExprString(e.Args[0]), routine, e.Fn, dv.kernel.errs[routine])
	}
	for _, pe := range effects {
		for _, b := range append([]binding{{}}, dv.kernel.bindings(routine, resolveDepth)...) {
			p, ok := xbe.ConstOf(dv.kernel.globals(substitute(pe.Args[0], b)))
			if !ok {
				continue
			}
			if p == pciAddressPort || p >= pciDataPort && p < pciDataPort+4 {
				configRoutines[routine] = true
				continue
			}
			a := access{stage: StageKernel, port: true, addr: p, size: int(size), write: write}
			if write {
				v := dv.kernel.globals(substitute(pe.Args[1], b))
				if readsInputs(v) {
					continue
				}
				a.value, a.known = xbe.ConstOf(v)
			}
			dv.touch(a)
		}
	}
	return nil
}

// Reads the configuration space accesses of a kernel routine off its paths, the data port decoded by the address
// written before it on the same path, an address the routine takes as an input resolved through its callers
func (dv *deriver) collectConfigPaths(entry uint32) error {
	l := dv.kernel
	paths := l.run(entry)
	if len(paths) == 0 {
		return fmt.Errorf("bios: the routine %08X writing the configuration address port does not lift: %v", entry, l.errs[entry])
	}
	for _, p := range paths {
		bindings := []binding{{}}
		for _, e := range p.Effects {
			if e.Kind == "call" && (e.Fn == "port_in" || e.Fn == "port_out") && (readsInputs(e.Args[0]) || len(e.Args) > 2 && readsInputs(e.Args[1])) {
				bindings = l.bindings(entry, resolveDepth)
				break
			}
		}
		for _, b := range bindings {
			var address uint32
			for _, e := range p.Effects {
				if e.Kind != "call" || e.Fn != "port_in" && e.Fn != "port_out" {
					continue
				}
				port, ok := xbe.ConstOf(l.globals(substitute(e.Args[0], b)))
				if !ok {
					continue
				}
				write := e.Fn == "port_out"
				var value, size uint32
				known := true
				if write {
					value, known = xbe.ConstOf(l.globals(substitute(e.Args[1], b)))
					size, _ = xbe.ConstOf(e.Args[2])
				} else {
					size, _ = xbe.ConstOf(e.Args[1])
				}
				if write && port == pciAddressPort && known && size == 4 {
					address = value
					continue
				}
				if port >= pciDataPort && port < pciDataPort+4 {
					dv.config(StageKernel, address, port, int(size), write, value, known)
				}
			}
		}
	}
	return nil
}

// flowState is what is known where a block of a routine is entered: the registers, the stack pointer's offset from
// the routine's entry and the stack slots, each value built from constants, globals and the entry stack pointer
type flowState struct {
	regs  map[string]xbe.Expr
	esp   int32
	slots map[int32]xbe.Expr
	stack bool
}

// The registers the processor keeps across a call
var calleeSaved = map[string]bool{"reg.EBX": true, "reg.ESI": true, "reg.EDI": true, "reg.EBP": true}

// The registers a routine is entered with, each holding its own input until a block writes it
var entryRegisters = []string{"reg.EAX", "reg.EBX", "reg.ECX", "reg.EDX", "reg.ESI", "reg.EDI", "reg.EBP"}

// The state a routine is entered in, every register its own input and the stack pointer at the return address
func entryState() flowState {
	in := flowState{regs: map[string]xbe.Expr{}, slots: map[int32]xbe.Expr{}, stack: true}
	for _, r := range entryRegisters {
		in.regs[r] = xbe.Sym{Name: r}
	}
	return in
}

// The flow of the routine holding a block, with the routine's entry
func (dv *deriver) flowOf(va uint32) (*routineFlow, uint32) {
	entry, ok := dv.routineOf[va]
	if !ok {
		fn := dv.lifted.KernelDis.FunctionAt(va)
		if fn == nil {
			return nil, 0
		}
		entry = fn.EntryVA
	}
	flow, done := dv.flows[entry]
	if !done {
		flow = dv.routineFlow(entry)
		dv.flows[entry] = flow
	}
	return flow, entry
}

// The state at the entry of the block at an address, flowed from the entry of the lifted routine holding it
func (dv *deriver) blockState(va uint32) flowState {
	flow, _ := dv.flowOf(va)
	if flow == nil {
		return flowState{}
	}
	return flow.states[va]
}

// Rewrites an expression of a block with what is known at its entry, the registers and the slots it reads
// substituted, a register nothing fixes marked lost, the stack pointer made relative to the routine's entry and
// operations between constants folded, in one pass up the expression
func (dv *deriver) inState(e xbe.Expr, in flowState) xbe.Expr {
	return dv.inStateWith(e, in, xbe.NewFoldMemo())
}

// Rewrites an expression into entry terms through a memo shared with the other expressions rewritten under the
// same state
func (dv *deriver) inStateWith(e xbe.Expr, in flowState, memo *xbe.FoldMemo) xbe.Expr {
	if e == nil {
		return nil
	}
	return memo.Fold(e, func(n xbe.Expr) (xbe.Expr, bool) { return dv.entryNode(n, in) })
}

// One node of a block's expression in entry terms once its children are, a register substituted or lost, a stack
// slot read as what the state holds there and an operation between constants folded
func (dv *deriver) entryNode(n xbe.Expr, in flowState) (xbe.Expr, bool) {
	switch v := n.(type) {
	case xbe.Sym:
		if v.Name == "reg.ESP" {
			if !in.stack {
				return nil, false
			}
			return xbe.Bin{Op: "add", A: v, B: xbe.IConst{V: uint32(in.esp)}}, true
		}
		if value, known := in.regs[v.Name]; known {
			return value, true
		}
		if strings.HasPrefix(v.Name, "reg.") {
			return xbe.Sym{Name: "lost." + v.Name}, true
		}
		return nil, false
	case xbe.Load:
		if v.Addr == nil || v.Size != 4 || !in.stack {
			return nil, false
		}
		if k, ok := relESP(v.Addr); ok {
			if value, known := in.slots[k]; known {
				return value, true
			}
		}
		return nil, false
	}
	return foldNode(n)
}

// The offset an expression names from the routine's entry stack pointer
func relESP(e xbe.Expr) (int32, bool) {
	switch v := e.(type) {
	case xbe.Sym:
		return 0, v.Name == "reg.ESP"
	case xbe.Bin:
		if v.Op != "add" && v.Op != "sub" {
			return 0, false
		}
		c, ok := xbe.ConstOf(v.B)
		base := v.A
		if !ok && v.Op == "add" {
			c, ok = xbe.ConstOf(v.A)
			base = v.B
		}
		if !ok {
			return 0, false
		}
		inner, ok := relESP(base)
		if !ok {
			return 0, false
		}
		if v.Op == "sub" {
			return inner - int32(c), true
		}
		return inner + int32(c), true
	}
	return 0, false
}

// Reports whether a value depends on nothing but constants, the memory it loads through such values, the routine's
// register inputs, registers a call left unknown and the entry stack pointer
func settled(e xbe.Expr) bool {
	ok := true
	xbe.WalkExpr(e, func(n xbe.Expr) {
		switch v := n.(type) {
		case xbe.Sym:
			if !strings.HasPrefix(v.Name, "reg.") && !strings.HasPrefix(v.Name, "lost.") {
				ok = false
			}
		case xbe.CallResult, xbe.Ptr:
			ok = false
		case xbe.Load:
			if v.Base != "" {
				ok = false
			}
		}
	})
	return ok
}

// The state a block leaves, its registers, stores and stack pointer applied to the state it entered with
func (dv *deriver) exitState(b xbe.BlockInfo, in flowState) (flowState, bool) {
	out := flowState{regs: map[string]xbe.Expr{}, esp: in.esp, slots: map[int32]xbe.Expr{}, stack: in.stack}
	for name, v := range in.regs {
		out.regs[name] = v
	}
	for k, v := range in.slots {
		out.slots[k] = v
	}
	view := dv.viewOf(b, in)
	for name, e := range b.Regs {
		if v := dv.inBlock(e, in, b, view); settled(v) {
			out.regs[name] = v
		} else {
			delete(out.regs, name)
		}
	}
	for i, e := range b.Effects {
		s := view.slots[i]
		if !s.ok {
			continue
		}
		if e.Size != 4 {
			delete(out.slots, s.at&^3)
			continue
		}
		if v := dv.inBlock(e.Value, in, b, view); settled(v) {
			out.slots[s.at] = v
		} else {
			delete(out.slots, s.at)
		}
	}
	if !in.stack {
		return out, false
	}
	k, ok := relESP(dv.inState(b.ESP, in))
	if !ok {
		if os.Getenv("DISCOXIP_HARDWARE_TRACE") != "" {
			fmt.Fprintf(os.Stderr, "hardware: block %08X leaves the stack pointer at %s, entered at %d\n", b.VA, xbe.ExprString(dv.inState(b.ESP, in)), in.esp)
		}
		out.stack, out.slots = false, map[int32]xbe.Expr{}
		return out, false
	}
	out.esp = k
	return out, true
}

// storeSlot is where a store of a block lands relative to the routine's entry stack pointer under one entry state
type storeSlot struct {
	at   int32
	size int
	ok   bool
}

// blockView is a block seen under one entry state, the slot each store lands in by effect index, the values of
// the stores a load of the block read, each rewritten once, and the nodes rewritten so far, shared by every
// expression of the block since they share their chains of addresses
type blockView struct {
	slots  []storeSlot
	stored map[int]xbe.Expr
	memo   *xbe.FoldMemo
}

// The slots the stores of a block land in under an entry state, none where the stack pointer is unknown
func (dv *deriver) viewOf(b xbe.BlockInfo, in flowState) *blockView {
	view := &blockView{slots: make([]storeSlot, len(b.Effects)), stored: map[int]xbe.Expr{}, memo: xbe.NewFoldMemo()}
	if !in.stack {
		return view
	}
	addresses := xbe.NewFoldMemo()
	for i, e := range b.Effects {
		if e.Kind != "store" || e.Addr == nil {
			continue
		}
		if at, ok := relESP(dv.inStateWith(e.Addr, in, addresses)); ok {
			view.slots[i] = storeSlot{at: at, size: e.Size, ok: true}
		}
	}
	return view
}

// Rewrites a block's expression with what is known at its entry, a load of a stack slot the block itself stored
// before the load reading that store, a slot a store partly overwrote staying a read of memory
func (dv *deriver) inBlock(e xbe.Expr, in flowState, b xbe.BlockInfo, view *blockView) xbe.Expr {
	if e == nil || !in.stack {
		return dv.inState(e, in)
	}
	return view.memo.Fold(e, func(n xbe.Expr) (xbe.Expr, bool) {
		ld, ok := n.(xbe.Load)
		if !ok || ld.Addr == nil || ld.Size != 4 {
			return dv.entryNode(n, in)
		}
		k, ok := relESP(ld.Addr)
		if !ok {
			return nil, false
		}
		for i := min(ld.Epoch, len(b.Effects)) - 1; i >= 0; i-- {
			s := view.slots[i]
			if !s.ok || s.at+int32(s.size) <= k || s.at >= k+4 {
				continue
			}
			if s.at != k || s.size != 4 {
				return ld, true
			}
			v, done := view.stored[i]
			if !done {
				v = dv.inBlock(b.Effects[i].Value, in, b, view)
				view.stored[i] = v
			}
			return v, true
		}
		return dv.entryNode(n, in)
	})
}

// routineFlow is what is known at the entry of every block of a routine and the blocks each is entered from
type routineFlow struct {
	states map[uint32]flowState
	preds  map[uint32][]uint32
}

// The blocks a block continues at, and whether it continues there after a call returns
func blockSuccessors(b xbe.BlockInfo) ([]uint32, bool) {
	switch b.Kind {
	case "jump", "hlt", "int":
		if b.Ret != 0 {
			return []uint32{b.Ret}, true
		}
		return []uint32{b.Target}, false
	case "jumpind":
		if b.Ret != 0 {
			return []uint32{b.Ret}, true
		}
		return nil, false
	case "branch":
		return []uint32{b.Taken, b.Fall}, false
	case "switch":
		return b.Targets, false
	}
	return nil, false
}

// The state a block hands a successor, only the saved registers and the slots above what the callee removes
// surviving a call, no slot surviving a call through a pointer since what that callee removes is unknown
func (dv *deriver) flowOut(b xbe.BlockInfo, out flowState, afterCall bool) flowState {
	next := flowState{regs: map[string]xbe.Expr{}, esp: out.esp, slots: map[int32]xbe.Expr{}, stack: out.stack}
	for name, v := range out.regs {
		if !afterCall || calleeSaved[name] {
			next.regs[name] = v
		}
	}
	if afterCall && out.stack {
		next.esp = out.esp + 4 + int32(dv.lifted.KernelDis.Cleanup(b.Target))
	}
	for k, v := range out.slots {
		if k >= next.esp && (!afterCall || b.Target != 0) {
			next.slots[k] = v
		}
	}
	return next
}

// The states at the entry of every block of a routine, flowed from its entry through branches and over calls, a
// register or slot the paths meeting at a block disagree on forgotten there
func (dv *deriver) routineFlow(entry uint32) *routineFlow {
	flow := &routineFlow{states: map[uint32]flowState{entry: entryState()}, preds: map[uint32][]uint32{}}
	queue := []uint32{entry}
	for len(queue) > 0 {
		va := queue[0]
		queue = queue[1:]
		b, ok := dv.blocks[va]
		if !ok {
			continue
		}
		out, _ := dv.exitState(b, flow.states[va])
		succs, afterCall := blockSuccessors(b)
		for _, succ := range succs {
			if !slices.Contains(flow.preds[succ], va) {
				flow.preds[succ] = append(flow.preds[succ], va)
			}
			next := dv.flowOut(b, out, afterCall)
			have, seen := flow.states[succ]
			if !seen {
				flow.states[succ] = next
				queue = append(queue, succ)
				continue
			}
			changed := false
			for name, v := range have.regs {
				w, kept := next.regs[name]
				if !kept {
					delete(have.regs, name)
					changed = true
					continue
				}
				if j := fold(join(v, w)); !xbe.EqualExprs(j, v) {
					have.regs[name] = j
					changed = true
				}
			}
			if have.stack && (!next.stack || have.esp != next.esp) {
				have.stack, have.slots, changed = false, map[int32]xbe.Expr{}, true
			}
			for k, v := range have.slots {
				w, kept := next.slots[k]
				if !kept {
					delete(have.slots, k)
					changed = true
					continue
				}
				if j := fold(join(v, w)); !xbe.EqualExprs(j, v) {
					have.slots[k] = j
					changed = true
				}
			}
			flow.states[succ] = have
			if changed {
				queue = append(queue, succ)
			}
		}
	}
	return flow
}

// The value two paths agree on where they meet, differing parts lost, masked values widened to the union of their masks
func join(a, b xbe.Expr) xbe.Expr {
	if xbe.EqualExprs(a, b) {
		return a
	}
	return joinWithin(a, b, map[[2]xbe.ExprKey]xbe.Expr{})
}

// Joins one node pair once however often the trees share it
func joinWithin(a, b xbe.Expr, seen map[[2]xbe.ExprKey]xbe.Expr) xbe.Expr {
	pair := [2]xbe.ExprKey{xbe.KeyOf(a), xbe.KeyOf(b)}
	if pair[0] == pair[1] {
		return a
	}
	if j, done := seen[pair]; done {
		return j
	}
	j := joinNode(a, b, seen)
	seen[pair] = j
	return j
}

// One node of a join, lost absorbing, equal leaves kept, masks widened, matching operations joined child by child
func joinNode(a, b xbe.Expr, seen map[[2]xbe.ExprKey]xbe.Expr) xbe.Expr {
	if s, ok := a.(xbe.Sym); ok && strings.HasPrefix(s.Name, "lost.") {
		return a
	}
	if s, ok := b.(xbe.Sym); ok && strings.HasPrefix(s.Name, "lost.") {
		return b
	}
	switch a.(type) {
	case xbe.IConst, xbe.FConst, xbe.Sym, xbe.Ptr, xbe.RetAddr:
		if a == b {
			return a
		}
	}
	if ma, cutA := maskOf(a); cutA {
		if mb, cutB := maskOf(b); cutB {
			return xbe.Bin{Op: "and", A: xbe.Sym{Name: "lost.join"}, B: xbe.IConst{V: ma | mb}}
		}
	}
	switch x := a.(type) {
	case xbe.Bin:
		if y, ok := b.(xbe.Bin); ok && x.Op == y.Op {
			ja, jb := joinWithin(x.A, y.A, seen), joinWithin(x.B, y.B, seen)
			if xbe.KeyOf(ja) == xbe.KeyOf(x.A) && xbe.KeyOf(jb) == xbe.KeyOf(x.B) {
				return a
			}
			return xbe.Bin{Op: x.Op, A: ja, B: jb}
		}
	case xbe.Un:
		if y, ok := b.(xbe.Un); ok && x.Op == y.Op {
			ja := joinWithin(x.A, y.A, seen)
			if xbe.KeyOf(ja) == xbe.KeyOf(x.A) {
				return a
			}
			return xbe.Un{Op: x.Op, A: ja}
		}
	case xbe.Load:
		if y, ok := b.(xbe.Load); ok && x.Base == y.Base && x.Size == y.Size && x.Addr != nil && y.Addr != nil {
			ja := joinWithin(x.Addr, y.Addr, seen)
			if xbe.KeyOf(ja) == xbe.KeyOf(x.Addr) && x.Off == y.Off && x.Epoch == y.Epoch {
				return a
			}
			return xbe.Load{Base: x.Base, Size: x.Size, Addr: ja, Epoch: x.Epoch}
		}
	}
	return xbe.Sym{Name: "lost.join"}
}

// The bits a value may hold, a constant's own or the mask a value is cut to
func maskOf(e xbe.Expr) (uint32, bool) {
	if c, ok := xbe.ConstOf(e); ok {
		return c, true
	}
	if bin, ok := e.(xbe.Bin); ok && bin.Op == "and" {
		if m, ok := xbe.ConstOf(bin.B); ok {
			return m, true
		}
		if m, ok := xbe.ConstOf(bin.A); ok {
			return m, true
		}
	}
	return 0, false
}

// The states a block may be entered in, each predecessor's state flowed into it apart, to a depth, the merged state
// past that depth or where nothing enters it
func (dv *deriver) statesInto(flow *routineFlow, va uint32, depth int) []flowState {
	merged := flow.states[va]
	if depth == 0 || len(flow.preds[va]) == 0 {
		return []flowState{merged}
	}
	var out []flowState
	for _, p := range flow.preds[va] {
		pb, ok := dv.blocks[p]
		if !ok {
			continue
		}
		for _, in := range dv.statesInto(flow, p, depth-1) {
			exit, _ := dv.exitState(pb, in)
			out = append(out, dv.flowOut(pb, exit, pb.Ret == va))
		}
	}
	if len(out) == 0 {
		return []flowState{merged}
	}
	return out
}

// Names the inputs a flow expression reads as the lift names them, a slot above the return address a stack input
func liftInputs(e xbe.Expr) xbe.Expr {
	return xbe.MapExpr(e, func(n xbe.Expr) (xbe.Expr, bool) {
		ld, ok := n.(xbe.Load)
		if !ok || ld.Addr == nil {
			return nil, false
		}
		k, rel := relESP(ld.Addr)
		if !rel || k < 4 {
			return nil, false
		}
		return xbe.Load{Base: "stack", Off: liftArgBase + k - 4, Size: ld.Size}, true
	})
}

// The arguments a call block leaves above its return address when entered in a state, nil where the state fixes
// none, and whether it fixes them all
func (dv *deriver) pushedArgs(b xbe.BlockInfo, in flowState, n int) ([]xbe.Expr, bool) {
	fromEntry := in.stack
	if !in.stack {
		in = flowState{regs: in.regs, slots: map[int32]xbe.Expr{}, stack: true}
	}
	exit, ok := dv.exitState(b, in)
	args := make([]xbe.Expr, n)
	complete := ok
	for i := range args {
		v, known := exit.slots[exit.esp+4+4*int32(i)]
		if !ok || !known {
			complete = false
			continue
		}
		if mentionsLost(v) {
			complete = false
		}
		if fromEntry {
			v = liftInputs(v)
		}
		args[i] = v
	}
	return args, complete
}

// Reports whether a value holds a register a call lost or paths disagreed on
func mentionsLost(e xbe.Expr) bool {
	found := false
	xbe.WalkExpr(e, func(n xbe.Expr) {
		if s, ok := n.(xbe.Sym); ok && strings.HasPrefix(s.Name, "lost.") {
			found = true
		}
	})
	return found
}

// The argument tuples a call block pushes, one from the state its paths agree on, else one from each path's own
// state to the split depth, the tuples pushed alike kept once
func (dv *deriver) callArgs(b xbe.BlockInfo, n int) [][]xbe.Expr {
	flow, _ := dv.flowOf(b.VA)
	if flow == nil {
		args, _ := dv.pushedArgs(b, flowState{}, n)
		return [][]xbe.Expr{args}
	}
	if args, complete := dv.pushedArgs(b, flow.states[b.VA], n); complete {
		return [][]xbe.Expr{args}
	}
	var out [][]xbe.Expr
	seen := map[string]bool{}
	for _, in := range dv.statesInto(flow, b.VA, splitDepth) {
		args, _ := dv.pushedArgs(b, in, n)
		var key strings.Builder
		for _, a := range args {
			key.WriteString(xbe.ExprString(a))
			key.WriteString(";")
		}
		if !seen[key.String()] {
			seen[key.String()] = true
			out = append(out, args)
		}
	}
	return out
}

// callSite is one call a lifted block makes to a routine, with the arguments the block pushes and what the lift
// of the calling routine records for the same instruction
type callSite struct {
	block   uint32
	ret     uint32
	routine uint32
	args    []xbe.Expr
	paths   []xbe.Effect
}

// The calls the kernel's or the executable's blocks make to a routine, one site per argument tuple the block's
// paths push above the return address, with what the calling routine's lift records for the same instruction
func (dv *deriver) callSites(target uint32, n int, names ...string) []callSite {
	var out []callSite
	dv.lifted.Model.EachBlock(func(b xbe.BlockInfo) {
		stage := dv.stageOf(b.VA)
		if b.Kind != "jump" || b.Target != target || b.Ret == 0 || (stage != StageKernel && stage != StageExecutable) || !dv.kernelCode(b.VA) {
			return
		}
		var routine uint32
		var paths []xbe.Effect
		if fn := dv.lifted.KernelDis.FunctionAt(b.VA); fn != nil {
			routine = fn.EntryVA
			for _, e := range dv.kernel.effects(fn.EntryVA) {
				if callReaches(e, target, names...) && e.VA < b.Ret && b.Ret-e.VA <= 7 {
					paths = append(paths, e)
				}
			}
		}
		for _, args := range dv.callArgs(b, n) {
			if args[0] == nil && os.Getenv("DISCOXIP_HARDWARE_TRACE") != "" {
				fmt.Fprintf(os.Stderr, "hardware: call block %08X of routine %08X leaves argument 0 open\n", b.VA, routine)
			}
			out = append(out, callSite{block: b.VA, ret: b.Ret, routine: routine, args: args, paths: paths})
		}
	})
	return out
}

// The argument tuples a call site resolves to, the calling routine's lift of the call under each binding of the
// routine's inputs, else what the block pushed under each, the routine's own inputs standing where nothing calls
// it, the tuples resolving alike kept once
func (dv *deriver) argTuples(site callSite, n int) [][]xbe.Expr {
	l := dv.kernel
	var sources [][]xbe.Expr
	for _, pe := range site.paths {
		args := make([]xbe.Expr, n)
		copy(args, pe.Args)
		sources = append(sources, args)
	}
	if len(sources) == 0 {
		args := make([]xbe.Expr, n)
		copy(args, site.args)
		sources = append(sources, args)
	}
	bindings := l.bindings(site.routine, resolveDepth)
	if len(bindings) == 0 {
		bindings = []binding{{}}
	}
	seen := map[string]bool{}
	var out [][]xbe.Expr
	for _, args := range sources {
		for _, b := range bindings {
			tuple := make([]xbe.Expr, n)
			var key strings.Builder
			for i, a := range args {
				if a != nil {
					tuple[i] = l.globals(substitute(a, b))
				}
				key.WriteString(xbe.ExprString(tuple[i]))
				key.WriteString(";")
			}
			if !seen[key.String()] {
				seen[key.String()] = true
				out = append(out, tuple)
			}
		}
	}
	return out
}

// The argument expression of a call site as the calling routine's lift sees it, else as the block pushed it
func (site callSite) arg(i int) xbe.Expr {
	for _, pe := range site.paths {
		if i < len(pe.Args) {
			return pe.Args[i]
		}
	}
	if i < len(site.args) && site.args[i] != nil {
		return site.args[i]
	}
	return nil
}

// pciCall is one configuration space access as a call site resolves it
type pciCall struct {
	bus, slot, register, length, write uint32
}

// Replays the kernel's configuration space routine with the arguments one caller passes, once per tuple the site
// resolves to, the buffer it writes from taken from the call, so its address and data port traffic is read exactly
func (dv *deriver) replayPCISpace(routine uint32, site callSite) error {
	var calls []pciCall
	openArg := -1
	var openTuple []xbe.Expr
	for _, tuple := range dv.argTuples(site, 6) {
		var c pciCall
		fields := []*uint32{&c.bus, &c.slot, &c.register, nil, &c.length, &c.write}
		resolved := true
		for i, f := range fields {
			if f == nil {
				continue
			}
			v, ok := xbe.ConstOf(tuple[i])
			if !ok {
				if openArg < 0 {
					openArg, openTuple = i, tuple
				}
				resolved = false
				break
			}
			*f = v
		}
		if resolved && !slices.Contains(calls, c) {
			calls = append(calls, c)
		}
	}
	if len(calls) == 0 {
		return fmt.Errorf("bios: block %08X calls %s with argument %d unresolved: %s", site.block, symbolPCISpace, openArg, xbe.ExprString(openTuple[openArg]))
	}
	var data []byte
	var fixed []bool
	for _, c := range calls {
		if c.write == 0 {
			continue
		}
		image, constant, fresh, err := dv.configImage(routine, site)
		if err != nil {
			return err
		}
		data, fixed = image, make([]bool, len(image))
		for k := range fixed {
			fixed[k] = constant[k] && fresh[k]
		}
	}
	for _, c := range calls {
		if os.Getenv("DISCOXIP_HARDWARE_TRACE") != "" {
			fmt.Fprintf(os.Stderr, "hardware: block %08X of %08X reaches configuration space of %d:%d register %#x, %d bytes, write %d, image of %d bytes with %d fixed\n", site.block, site.routine, c.bus, c.slot, c.register, c.length, c.write, len(data), countTrue(fixed))
		}
		t := dv.newTrace(StageKernel)
		run := dv.kernelRun(t)
		run.Preload(xbe.ReplayBuffer, make([]byte, c.length))
		run.Preload(xbe.ReplayBuffer, data)
		for _, v := range []uint32{c.write, c.length, xbe.ReplayBuffer, c.register, c.slot, c.bus} {
			run.Push(v)
		}
		run.Push(xbe.ReplayReturn)
		if _, err := run.Run(routine); err != nil {
			return fmt.Errorf("bios: replaying %s for block %08X: %w", symbolPCISpace, site.block, err)
		}
		markCarried(t.ports, c.register, fixed)
		dv.collectPortTrace(t.ports)
	}
	return nil
}

// Marks the data port writes of a configuration routine's traffic unknown where the image byte each carries is not fixed
func markCarried(ports []access, register uint32, fixed []bool) {
	var address uint32
	for i := range ports {
		a := &ports[i]
		if a.write && a.addr == pciAddressPort && a.size == 4 {
			address = a.value
			continue
		}
		if !a.write || a.addr < pciDataPort || a.addr >= pciDataPort+4 {
			continue
		}
		offset := int(address&0xFC) + int(a.addr-pciDataPort) - int(register)
		for k := 0; k < a.size; k++ {
			if offset+k < 0 || offset+k >= len(fixed) || !fixed[offset+k] {
				a.known = false
			}
		}
	}
}

func countTrue(flags []bool) int {
	n := 0
	for _, f := range flags {
		if f {
			n++
		}
	}
	return n
}

// Replays a routine that takes no input through to its return, the image each configuration write it makes
// carries marked by the lift of its site, reporting whether the run completed
func (dv *deriver) replayPCIRoutine(pciSpace, routine uint32, byReturn map[uint32]callSite) (bool, error) {
	type visit struct {
		order int
		call  pciCall
		fresh []bool
	}
	t := dv.newTrace(StageKernel)
	run := dv.kernelRun(t)
	var visits []visit
	var failed error
	run.OnCall = func(r *xbe.MachineRun, target, ret uint32) {
		if target != pciSpace || failed != nil {
			return
		}
		site, known := byReturn[ret]
		if !known {
			failed = fmt.Errorf("bios: %08X calls %s returning to %08X, where no lifted block calls it", routine, symbolPCISpace, ret)
			return
		}
		args := make([]uint32, 6)
		for i := range args {
			args[i] = r.ReadU32(r.Regs["ESP"] + 4*uint32(i+1))
		}
		v := visit{order: t.order, call: pciCall{bus: args[0], slot: args[1], register: args[2], length: args[4], write: args[5]}}
		if v.call.write != 0 {
			var err error
			if _, _, v.fresh, err = dv.configImage(pciSpace, site); err != nil {
				failed = err
				return
			}
		}
		if os.Getenv("DISCOXIP_HARDWARE_TRACE") != "" {
			fmt.Fprintf(os.Stderr, "hardware: %08X reaches configuration space of %d:%d register %#x, %d bytes, write %d, from block %08X whose image has %d of %d bytes fresh\n", routine, v.call.bus, v.call.slot, v.call.register, v.call.length, v.call.write, site.block, countTrue(v.fresh), len(v.fresh))
		}
		visits = append(visits, v)
	}
	if _, err := run.Run(routine); err != nil {
		return false, nil
	}
	if failed != nil {
		return false, failed
	}
	for i, v := range visits {
		end := len(t.ports)
		if i+1 < len(visits) {
			end = visits[i+1].order
		}
		markCarried(t.ports[v.order:end], v.call.register, v.fresh)
	}
	dv.collectPortTrace(t.ports)
	return true, nil
}

// The image a call site hands the configuration routine, the lift's snapshot of its buffer at the call, a byte
// constant when a fixed value lies there and fresh when what lies there was built from nothing the buffer carried
func (dv *deriver) configImage(routine uint32, site callSite) (data []byte, constant, fresh []bool, err error) {
	at := map[uint32]bool{}
	for _, pe := range site.paths {
		at[pe.VA] = true
	}
	for _, p := range dv.kernel.run(site.routine) {
		for _, e := range p.Effects {
			if e.Kind != "call" || !at[e.VA] || len(e.Args) < 6 {
				continue
			}
			ptr, isPtr := e.Args[3].(xbe.Ptr)
			if !isPtr {
				return nil, nil, nil, fmt.Errorf("bios: block %08X hands %s a buffer at %s, which the lift cannot place", site.block, symbolPCISpace, xbe.ExprString(e.Args[3]))
			}
			carries := func(v xbe.Expr) bool {
				found := false
				xbe.WalkExpr(v, func(n xbe.Expr) {
					switch ld := n.(type) {
					case xbe.Load:
						if ld.Addr == nil && ld.Base == ptr.Base && ld.Off >= ptr.Off && int64(ld.Off)-int64(ptr.Off) < pciConfigSize {
							found = true
						}
					case xbe.Ptr:
						if ld.Base == ptr.Base && ld.Off >= ptr.Off && int64(ld.Off)-int64(ptr.Off) < pciConfigSize {
							found = true
						}
					}
				})
				return found
			}
			for _, buf := range e.Buffers {
				if buf.Index != 3 || buf.Offs == nil {
					continue
				}
				data, constant, fresh = make([]byte, pciConfigSize), make([]bool, pciConfigSize), make([]bool, pciConfigSize)
				for k, el := range buf.Elems {
					v, isConst := xbe.ConstOf(el)
					isFresh := !carries(el)
					for b := 0; b < buf.Sizes[k] && int(buf.Offs[k])+b < pciConfigSize; b++ {
						data[int(buf.Offs[k])+b] = byte(v >> (8 * uint(b)))
						constant[int(buf.Offs[k])+b], fresh[int(buf.Offs[k])+b] = isConst, isFresh
					}
				}
				return data, constant, fresh, nil
			}
			return nil, nil, nil, fmt.Errorf("bios: the lift of %08X records no image of the buffer block %08X hands %s", site.routine, site.block, symbolPCISpace)
		}
	}
	return nil, nil, nil, fmt.Errorf("bios: the lift of %08X records no call of %s at block %08X", site.routine, symbolPCISpace, site.block)
}

// The configuration space accesses the kernel makes through its signature named routine, a calling routine that
// takes no input and runs through replayed whole, and every other call replayed with the arguments it resolves to
func (dv *deriver) collectPCISpace() error {
	var pciSpace uint32
	for _, fn := range dv.lifted.KernelDis.Functions {
		if fn.Symbol != nil && fn.Symbol.Name == symbolPCISpace {
			pciSpace = fn.EntryVA
		}
	}
	if pciSpace == 0 {
		return fmt.Errorf("bios: the kernel's %s is not identified", symbolPCISpace)
	}
	sites := dv.callSites(pciSpace, 6, symbolPCISpace)
	byReturn := map[uint32]callSite{}
	for _, site := range sites {
		byReturn[site.ret] = site
	}
	whole := map[uint32]bool{}
	for _, site := range sites {
		if site.routine != 0 && dv.inputFree(site.routine) {
			ran, tried := whole[site.routine]
			if !tried {
				var err error
				if ran, err = dv.replayPCIRoutine(pciSpace, site.routine, byReturn); err != nil {
					return err
				}
				whole[site.routine] = ran
			}
			if ran {
				continue
			}
		}
		if err := dv.replayPCISpace(pciSpace, site); err != nil {
			return err
		}
	}
	return nil
}

// Reports whether a routine reads no input, so a replay from its entry runs it as its callers do
func (dv *deriver) inputFree(routine uint32) bool {
	l := dv.kernel
	if len(l.d.RegisterInputs(routine)) > 0 {
		return false
	}
	paths := l.run(routine)
	if len(paths) == 0 || l.errs[routine] != nil {
		return false
	}
	for _, p := range paths {
		for _, e := range p.Effects {
			if readsInputs(e.Value) || readsInputs(e.Addr) {
				return false
			}
			for _, a := range e.Args {
				if readsInputs(a) {
					return false
				}
			}
		}
		for _, g := range p.Guards {
			if readsInputs(g) {
				return false
			}
		}
		if readsInputs(p.Return) {
			return false
		}
	}
	return true
}

func (dv *deriver) layout() ([]PCIFunction, []PortUse, []RegisterUse) {
	keys := make([]pciKey, 0, len(dv.configs))
	for k := range dv.configs {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := keys[i], keys[j]
		if a.bus != b.bus {
			return a.bus < b.bus
		}
		if a.device != b.device {
			return a.device < b.device
		}
		return a.function < b.function
	})
	functions := map[pciKey]*PCIFunction{}
	out := make([]PCIFunction, 0, len(keys))
	for _, k := range keys {
		fn := PCIFunction{Bus: k.bus, Device: k.device, Function: k.function, Config: dv.configs[k], Windows: []Window{}}
		for _, c := range fn.Config {
			if c.Write && c.Register == pciBridgeBuses && c.Size == 4 {
				secondary := int(c.Value >> 8 & 0xFF)
				for _, other := range keys {
					if other.bus == secondary && secondary != k.bus {
						fn.Secondary = secondary
					}
				}
			}
		}
		out = append(out, fn)
		functions[k] = &out[len(out)-1]
	}
	windows := make([]*Window, len(dv.bars))
	for _, bar := range dv.bars {
		fn := functions[bar.fn]
		if fn.Secondary != 0 && bar.register > pciFirstBAR+4 {
			continue
		}
		fn.Windows = append(fn.Windows, Window{Register: bar.register, Stage: bar.stage, Base: bar.base, IO: bar.io, Accesses: []Access{}})
	}
	for i, bar := range dv.bars {
		fn := functions[bar.fn]
		for w := range fn.Windows {
			if fn.Windows[w].Register == bar.register && fn.Windows[w].Stage == bar.stage && fn.Windows[w].Base == bar.base {
				windows[i] = &fn.Windows[w]
			}
		}
	}
	ports := map[uint32]*PortUse{}
	registers := map[uint32]*RegisterUse{}
	for _, a := range dv.accesses {
		if w := dv.windowOf(a, windows); w != nil {
			addAccess(w, a)
			continue
		}
		if a.port {
			u := ports[a.addr]
			if u == nil {
				u = &PortUse{Port: a.addr, Size: a.size, Writes: []uint32{}}
				ports[a.addr] = u
			}
			u.Reads = u.Reads || !a.write
			if a.write && a.known {
				u.Writes = appendUnique(u.Writes, a.value)
			}
			u.Stages = appendStage(u.Stages, a.stage)
			continue
		}
		u := registers[a.addr]
		if u == nil {
			u = &RegisterUse{Address: a.addr, Size: a.size, Writes: []uint32{}}
			registers[a.addr] = u
		}
		u.Reads = u.Reads || !a.write
		if a.write && a.known {
			u.Writes = appendUnique(u.Writes, a.value)
		}
		u.Stages = appendStage(u.Stages, a.stage)
	}
	portList := []PortUse{}
	for _, u := range ports {
		portList = append(portList, *u)
	}
	sort.Slice(portList, func(i, j int) bool { return portList[i].Port < portList[j].Port })
	registerList := []RegisterUse{}
	for _, u := range registers {
		registerList = append(registerList, *u)
	}
	sort.Slice(registerList, func(i, j int) bool { return registerList[i].Address < registerList[j].Address })
	for _, w := range windows {
		if w == nil {
			continue
		}
		sort.Slice(w.Accesses, func(i, j int) bool { return w.Accesses[i].Offset < w.Accesses[j].Offset })
		for _, a := range w.Accesses {
			if end := a.Offset + uint32(a.Size); end > w.Extent {
				w.Extent = end
			}
		}
	}
	return out, portList, registerList
}

// The window an access falls in, the one whose assignment was the latest of the right space below the address
// when the access happened, the init table's assignments counting in table order and the later stages' all before them
func (dv *deriver) windowOf(a access, windows []*Window) *Window {
	var best *Window
	var bestOrder int
	bestStage := ""
	for i, bar := range dv.bars {
		if bar.io != a.port || a.addr < bar.base {
			continue
		}
		if a.stage == StageInit && (bar.stage != StageInit || bar.order > a.order) {
			continue
		}
		if a.stage != StageInit && bar.stage == StageInit && dv.reassigned(bar) {
			continue
		}
		w := windows[i]
		if w == nil {
			continue
		}
		if best == nil || bar.base > best.Base || bar.base == best.Base && (stageRank(bar.stage) > stageRank(bestStage) || bar.order > bestOrder) {
			best, bestOrder, bestStage = w, bar.order, bar.stage
		}
	}
	return best
}

// Reports whether a later stage assigned the same base address register again
func (dv *deriver) reassigned(bar barAssignment) bool {
	for _, other := range dv.bars {
		if other.fn == bar.fn && other.register == bar.register && stageRank(other.stage) > stageRank(bar.stage) {
			return true
		}
	}
	return false
}

func stageRank(stage string) int {
	return map[string]int{StageInit: 0, StageBldr: 1, StageKernel: 2}[stage]
}

// Merges an access into a window at its offset
func addAccess(w *Window, a access) {
	offset := a.addr - w.Base
	for i := range w.Accesses {
		have := &w.Accesses[i]
		if have.Offset == offset && have.Size == a.size {
			have.Reads = have.Reads || !a.write
			if a.write && a.known {
				have.Writes = appendUnique(have.Writes, a.value)
			}
			have.Stages = appendStage(have.Stages, a.stage)
			return
		}
	}
	acc := Access{Offset: offset, Size: a.size, Reads: !a.write, Writes: []uint32{}, Stages: []string{a.stage}}
	if a.write && a.known {
		acc.Writes = []uint32{a.value}
	}
	w.Accesses = append(w.Accesses, acc)
}

func appendUnique(list []uint32, v uint32) []uint32 {
	for _, have := range list {
		if have == v {
			return list
		}
	}
	return append(list, v)
}

func appendStage(list []string, stage string) []string {
	for _, have := range list {
		if have == stage {
			return list
		}
	}
	list = append(list, stage)
	sort.Slice(list, func(i, j int) bool { return stageRank(list[i]) < stageRank(list[j]) })
	return list
}

// The bus controller by the ports its routines write, and every part the init table, 2BL and kernel calls address
func (dv *deriver) deriveSMBus(functions []PCIFunction) (SMBus, error) {
	read, err := dv.export(exportReadSMBus)
	if err != nil {
		return SMBus{}, err
	}
	write, err := dv.export(exportWriteSMBus)
	if err != nil {
		return SMBus{}, err
	}
	bus := SMBus{Slaves: []SMBusSlave{}}
	l := dv.kernel
	slave := xbe.Load{Base: "stack", Off: liftArgBase, Size: 4}
	command := xbe.Load{Base: "stack", Off: liftArgBase + 4, Size: 4}
	value := xbe.Load{Base: "stack", Off: liftArgBase + 12, Size: 4}
	var outs []uint32
	for _, routine := range []uint32{read, write} {
		for _, pe := range l.portEffects(routine, binding{}, 2) {
			port, ok := xbe.ConstOf(pe.port)
			if !ok {
				continue
			}
			switch {
			case pe.out && mentionsInput(pe.value, slave):
				bus.Address = port
			case pe.out && mentionsInput(pe.value, command):
				bus.Command = port
			case pe.out && routine == write && mentionsInput(pe.value, value):
				bus.Data = port
			case !pe.out && pe.acked:
				bus.Status = port
			case !pe.out && pe.stored:
				bus.Data = port
			case pe.out && !mentionsPortRead(pe.value):
				outs = appendUnique(outs, port)
			}
		}
	}
	for _, port := range outs {
		if port != bus.Address && port != bus.Command && port != bus.Data && port != bus.Status {
			bus.Control = port
		}
	}
	for name, port := range map[string]uint32{"address": bus.Address, "command": bus.Command, "data": bus.Data, "status": bus.Status, "control": bus.Control} {
		if port == 0 {
			return SMBus{}, fmt.Errorf("bios: the kernel's bus routines reveal no %s port", name)
		}
	}
	bus.Done = dv.initAnswers()[bus.Status]
	if bus.Done == 0 {
		return SMBus{}, fmt.Errorf("bios: the init table waits for no status on port %04X", bus.Status)
	}
	found := false
	for _, fn := range functions {
		for _, w := range fn.Windows {
			if w.IO && bus.Address >= w.Base && bus.Address-w.Base < 0x100 {
				bus.Bus, bus.Device, bus.Function, bus.Base = fn.Bus, fn.Device, fn.Function, w.Base
				found = true
			}
		}
	}
	if !found {
		return SMBus{}, fmt.Errorf("bios: no base address register opens a window over port %04X", bus.Address)
	}
	slaves := map[uint8]map[uint8]*SMBusCommand{}
	addPart := func(address uint8) {
		if slaves[address] == nil {
			slaves[address] = map[uint8]*SMBusCommand{}
		}
	}
	add := func(stage string, address, cmd uint8, word, reads, known bool, v uint32) {
		addPart(address)
		c := slaves[address][cmd]
		if c == nil {
			c = &SMBusCommand{Command: cmd, Writes: []uint32{}}
			slaves[address][cmd] = c
		}
		c.Word = c.Word || word
		c.Reads = c.Reads || reads
		if !reads && known {
			c.Writes = appendUnique(c.Writes, v)
		}
		c.Stages = appendStage(c.Stages, stage)
	}
	type kernelCall struct {
		reads     bool
		addresses []uint32
		command   uint32
		word      uint32
		value     uint32
		fixed     bool
	}
	calls := map[uint32][]kernelCall{}
	addressed := map[uint32][]uint32{}
	explore := map[uint32]bool{}
	for _, routine := range []uint32{read, write} {
		for _, site := range dv.callSites(routine, 4, exportReadSMBus, exportWriteSMBus) {
			if site.routine == 0 {
				return SMBus{}, fmt.Errorf("bios: block %08X addresses the bus from no routine", site.block)
			}
			if explore[site.routine] {
				continue
			}
			for _, tuple := range dv.argTuples(site, 4) {
				addresses := l.loadedGlobal(tuple[0])
				if a, ok := xbe.ConstOf(tuple[0]); ok {
					addresses = []uint32{a}
				}
				if len(addresses) == 0 {
					if os.Getenv("DISCOXIP_HARDWARE_TRACE") != "" {
						fmt.Fprintf(os.Stderr, "hardware: block %08X of %08X addresses part %s, which it computes, so its routine is explored\n", site.block, site.routine, xbe.ExprString(tuple[0]))
					}
					explore[site.routine] = true
					break
				}
				cmd, cmdFixed := xbe.ConstOf(tuple[1])
				word, wordFixed := xbe.ConstOf(tuple[2])
				if !cmdFixed || !wordFixed {
					for _, address := range addresses {
						addressed[site.routine] = appendUnique(addressed[site.routine], address&^1)
					}
					continue
				}
				value, valueFixed := xbe.ConstOf(tuple[3])
				calls[site.routine] = append(calls[site.routine], kernelCall{reads: routine == read, addresses: addresses, command: cmd, word: word, value: value, fixed: routine == write && valueFixed})
			}
		}
	}
	resolved := make([]uint32, 0, len(calls)+len(addressed))
	for _, routines := range []map[uint32]bool{keysOf(calls), keysOf(addressed)} {
		for entry := range routines {
			if !explore[entry] && !slices.Contains(resolved, entry) {
				resolved = append(resolved, entry)
			}
		}
	}
	sort.Slice(resolved, func(i, j int) bool { return resolved[i] < resolved[j] })
	for _, entry := range resolved {
		for _, part := range addressed[entry] {
			addPart(uint8(part))
		}
		for _, c := range calls[entry] {
			for _, address := range c.addresses {
				add(StageKernel, uint8(address&^1), uint8(c.command), c.word != 0, c.reads, c.fixed, c.value)
			}
		}
	}
	roots, err := dv.busRoots(explore)
	if err != nil {
		return SMBus{}, err
	}
	for _, root := range roots {
		if err := dv.exploreBus(root, read, write, func(c busCall) {
			add(StageKernel, c.slave, c.command, c.word, c.reads, c.valueFixed, c.value)
		}); err != nil {
			return SMBus{}, err
		}
	}
	for _, stage := range []string{StageInit, StageBldr} {
		var sequence []access
		for _, a := range dv.accesses {
			if a.stage == stage && a.port && a.addr >= bus.Base && a.addr-bus.Base < 0x100 {
				sequence = append(sequence, a)
			}
		}
		for _, tx := range busTransactions(sequence, bus) {
			add(stage, tx.address&^1, tx.command, tx.word, tx.address&1 != 0, tx.known, tx.value)
		}
	}
	addresses := make([]int, 0, len(slaves))
	for a := range slaves {
		addresses = append(addresses, int(a))
	}
	sort.Ints(addresses)
	for _, a := range addresses {
		s := SMBusSlave{Address: uint8(a), Commands: []SMBusCommand{}}
		cmds := make([]int, 0, len(slaves[uint8(a)]))
		for c := range slaves[uint8(a)] {
			cmds = append(cmds, int(c))
		}
		sort.Ints(cmds)
		for _, c := range cmds {
			s.Commands = append(s.Commands, *slaves[uint8(a)][uint8(c)])
		}
		bus.Slaves = append(bus.Slaves, s)
	}
	return bus, nil
}

// The keys of a map by routine address, as a set
func keysOf[V any](m map[uint32]V) map[uint32]bool {
	out := map[uint32]bool{}
	for k := range m {
		out[k] = true
	}
	return out
}

// The constants a global holds when the low byte of an expression is a load of it, widened, masked to the byte or
// ored with bits above it
func (l *lifter) loadedGlobal(e xbe.Expr) []uint32 {
	for {
		switch v := e.(type) {
		case xbe.Un:
			e = v.A
			continue
		case xbe.Bin:
			if v.Op == "or" {
				if c, ok := xbe.ConstOf(v.A); ok && c&0xFF == 0 || highBytesOnly(v.A) {
					e = v.B
					continue
				}
				if c, ok := xbe.ConstOf(v.B); ok && c&0xFF == 0 || highBytesOnly(v.B) {
					e = v.A
					continue
				}
			}
			if v.Op == "and" {
				if c, ok := xbe.ConstOf(v.B); ok && c&0xFF == 0xFF {
					e = v.A
					continue
				}
			}
		case xbe.Load:
			if addr, global := globalAt(v); global {
				var out []uint32
				for _, g := range l.globalValues(addr, v.Size) {
					out = appendUnique(out, g&0xFF)
				}
				return out
			}
		}
		return nil
	}
}

// Reports whether an expression keeps only bits above its low byte
func highBytesOnly(e xbe.Expr) bool {
	bin, ok := e.(xbe.Bin)
	if !ok || bin.Op != "and" {
		return false
	}
	c, isConst := xbe.ConstOf(bin.B)
	return isConst && c&0xFF == 0
}

type transaction struct {
	address uint8
	command uint8
	value   uint32
	known   bool
	word    bool
}

// Reads the transactions out of the port accesses of one stage inside the controller's window, each starting at a
// write of the address port and ending at the write of the control port
func busTransactions(sequence []access, bus SMBus) []transaction {
	var out []transaction
	var cur *transaction
	for _, a := range sequence {
		if !a.write {
			continue
		}
		switch a.addr {
		case bus.Address:
			cur = &transaction{address: uint8(a.value), known: true}
		case bus.Command:
			if cur != nil {
				cur.command = uint8(a.value)
			}
		case bus.Data:
			if cur != nil {
				cur.value, cur.known, cur.word = a.value, a.known, a.size == 2
			}
		case bus.Control:
			if cur != nil {
				out = append(out, *cur)
				cur = &transaction{address: cur.address, known: true}
			}
		}
	}
	return out
}

// portEffect is a port access a routine makes with its inputs substituted, and how the value read is used, kept,
// tested or written back to the same port
type portEffect struct {
	port    xbe.Expr
	value   xbe.Expr
	out     bool
	stored  bool
	guarded bool
	acked   bool
}

// The port accesses a routine and the helpers it calls make, the helpers' inputs bound to what the routine passes
func (l *lifter) portEffects(entry uint32, b binding, depth int) []portEffect {
	var out []portEffect
	for _, p := range l.run(entry) {
		for _, e := range p.Effects {
			if e.Kind != "call" {
				continue
			}
			switch e.Fn {
			case "port_out":
				out = append(out, portEffect{port: substitute(e.Args[0], b), value: substitute(e.Args[1], b), out: true})
			case "port_in":
				result := xbe.CallResult{Fn: e.Fn, Args: e.Args, Seq: e.Seq}
				pe := portEffect{port: substitute(e.Args[0], b)}
				for _, other := range p.Effects {
					if other.Kind == "store" && mentions(other.Value, result) {
						pe.stored = true
					}
					if other.Kind == "call" && other.Fn == "port_out" && xbe.SameExpr(other.Args[0], e.Args[0]) && mentions(other.Args[1], result) {
						pe.acked = true
					}
				}
				for _, g := range p.Guards {
					if mentions(g, result) {
						pe.guarded = true
					}
				}
				if mentions(p.Return, result) {
					pe.stored = true
				}
				out = append(out, pe)
			default:
				if depth == 0 {
					continue
				}
				var callee uint32
				if scanSub(e.Fn, &callee) || e.Target != 0 {
					if callee == 0 {
						callee = e.Target
					}
					inner := l.bind(callee, e)
					for k, v := range inner {
						inner[k] = substitute(v, b)
					}
					out = append(out, l.portEffects(callee, inner, depth-1)...)
				}
			}
		}
	}
	return out
}

// Parses the address out of an unnamed callee's name
func scanSub(name string, va *uint32) bool {
	_, err := fmt.Sscanf(name, "sub_%08X", va)
	return err == nil && strings.HasPrefix(name, "sub_")
}

// Reports whether an expression reads a slot of its routine's inputs, the slot narrowed or not
func mentionsInput(e xbe.Expr, input xbe.Load) bool {
	found := false
	xbe.WalkExpr(e, func(n xbe.Expr) {
		if ld, ok := n.(xbe.Load); ok && ld.Base == input.Base && ld.Addr == nil && ld.Off == input.Off {
			found = true
		}
	})
	return found
}

// Reports whether an expression is built from a port reading
func mentionsPortRead(e xbe.Expr) bool {
	found := false
	xbe.WalkExpr(e, func(n xbe.Expr) {
		if r, ok := n.(xbe.CallResult); ok && r.Fn == "port_in" {
			found = true
		}
	})
	return found
}

// The interrupt table is the one the kernel loads, its limit and base stored beside the load, its gates read from
// the image
func (dv *deriver) deriveTable() (InterruptTable, error) {
	var table InterruptTable
	found := false
	dv.lifted.Model.EachBlock(func(b xbe.BlockInfo) {
		if dv.stageOf(b.VA) != StageKernel || !dv.kernelCode(b.VA) {
			return
		}
		for _, e := range b.Effects {
			if e.Kind != "call" || e.Fn != "cpu_lidt" {
				continue
			}
			for _, st := range b.Effects {
				if st.Kind != "store" || st.Addr == nil {
					continue
				}
				if c, ok := xbe.ConstOf(st.Value); ok && st.Size == 2 && xbe.SameExpr(st.Addr, e.Args[0]) {
					table.Limit = c
				}
				if c, ok := xbe.ConstOf(st.Value); ok && st.Size == 4 && xbe.SameExpr(st.Addr, xbe.Bin{Op: "add", A: e.Args[0], B: xbe.IConst{V: 2}}) {
					table.Base = c
					found = true
				}
			}
		}
	})
	if !found {
		return table, fmt.Errorf("bios: the kernel loads no interrupt table from a fixed address")
	}
	mm := dv.lifted.Model
	for v := uint32(0); v*8 < table.Limit; v++ {
		handler, ok := dv.lifted.Kernel.ReadU32(table.Base + 8*v)
		attr, ok2 := dv.lifted.Kernel.ReadU32(table.Base + 8*v + 4)
		if !ok || !ok2 {
			return table, fmt.Errorf("bios: the interrupt table at %08X lies outside the kernel", table.Base)
		}
		if attr&gatePresent == 0 {
			handler = 0
		}
		var low, high []uint32
		for _, st := range mm.StoresAt(table.Base+8*v, 2) {
			if c, isConst := xbe.ConstOf(st.Value); isConst {
				low = appendUnique(low, c)
			}
		}
		for _, st := range mm.StoresAt(table.Base+8*v+6, 2) {
			if c, isConst := xbe.ConstOf(st.Value); isConst {
				high = appendUnique(high, c)
			}
		}
		if len(low) == 1 && len(high) == 1 {
			handler = high[0]<<16 | low[0]&0xFFFF
		} else if len(low) > 0 || len(high) > 0 {
			return table, fmt.Errorf("bios: the kernel patches gate %#x with %d low and %d high halves", v, len(low), len(high))
		}
		table.Handlers = append(table.Handlers, handler)
	}
	return table, nil
}

// The interrupts are the lines the kernel serves, from the gates its table holds that reach a device and from the
// connections it makes, each handler's reach read off its blocks and the object the connection hands it
func (dv *deriver) deriveInterrupts(pics []PIC, table InterruptTable) ([]Interrupt, error) {
	connect, err := dv.export(xbe.InterruptInitializer)
	if err != nil {
		return nil, err
	}
	out := []Interrupt{}
	for i, pic := range pics {
		for line := 0; line < 8; line++ {
			vector := int(pic.VectorBase) + line
			if vector >= len(table.Handlers) || table.Handlers[vector] == 0 {
				continue
			}
			handler := table.Handlers[vector]
			if err := dv.lifted.Model.LiftRoutine(dv.lifted.KernelDis, handler); err != nil {
				return nil, fmt.Errorf("bios: lifting the handler of gate %#x: %w", vector, err)
			}
			ports, registers := dv.routineReach(handler)
			if len(ports) == 0 && len(registers) == 0 {
				continue
			}
			out = append(out, Interrupt{IRQ: 8*i + line, Vector: vector, Handler: handler, Static: true, Ports: ports, Registers: registers})
		}
	}
	connected := map[[2]uint32]bool{}
	for _, site := range dv.callSites(connect, 7, xbe.InterruptInitializer) {
		lines := map[uint32][]int{}
		var handlers []uint32
		for _, tuple := range dv.argTuples(site, 7) {
			handler, ok := xbe.ConstOf(tuple[1])
			if !ok {
				continue
			}
			if !slices.Contains(handlers, handler) {
				handlers = append(handlers, handler)
			}
			vector := tuple[3]
			level := false
			if r, isCall := vector.(xbe.CallResult); isCall && r.Fn == exportVector && len(r.Args) > 0 {
				vector, level = r.Args[0], true
			}
			v, isConst := xbe.ConstOf(vector)
			if !isConst {
				continue
			}
			if level {
				if !slices.Contains(lines[handler], int(v)) {
					lines[handler] = append(lines[handler], int(v))
				}
				continue
			}
			for i, pic := range pics {
				if v >= uint32(pic.VectorBase) && v-uint32(pic.VectorBase) < 8 && !slices.Contains(lines[handler], int(v-uint32(pic.VectorBase))+8*i) {
					lines[handler] = append(lines[handler], int(v-uint32(pic.VectorBase))+8*i)
				}
			}
		}
		if len(handlers) == 0 {
			return nil, fmt.Errorf("bios: block %08X connects an interrupt to no fixed handler: %s", site.block, xbe.ExprString(site.arg(1)))
		}
		for _, handler := range handlers {
			if len(lines[handler]) == 0 {
				return nil, fmt.Errorf("bios: block %08X connects handler %08X to no line the controllers serve: %s", site.block, handler, xbe.ExprString(site.arg(3)))
			}
			ports, registers, err := dv.handlerReach(handler, site)
			if err != nil {
				return nil, err
			}
			for _, irq := range lines[handler] {
				if connected[[2]uint32{uint32(irq), handler}] {
					continue
				}
				connected[[2]uint32{uint32(irq), handler}] = true
				vector := 0
				if irq/8 < len(pics) {
					vector = int(pics[irq/8].VectorBase) + irq%8
				}
				out = append(out, Interrupt{IRQ: irq, Vector: vector, Handler: handler, Ports: ports, Registers: registers})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].IRQ < out[j].IRQ })
	return out, nil
}

// The ports and registers a handler reaches, by constant in its blocks and those of the routines it calls, and
// through the fields of the context object the connection hands it
func (dv *deriver) handlerReach(handler uint32, site callSite) ([]uint32, []uint32, error) {
	ports, registers := dv.routineReach(handler)
	l := dv.kernel
	paths := l.run(handler)
	if len(paths) == 0 {
		return nil, nil, fmt.Errorf("bios: the handler %08X does not lift: %v", handler, l.errs[handler])
	}
	contextArg := xbe.Load{Base: "stack", Off: liftArgBase + 4, Size: 4}
	context := site.arg(2)
	var failed error
	visit := func(addr xbe.Expr) {
		if failed != nil || addr == nil {
			return
		}
		base, off := splitOffset(addr)
		field, isLoad := base.(xbe.Load)
		if !isLoad || field.Addr == nil || field.Size != 4 {
			return
		}
		fieldBase, fieldOff := splitOffset(field.Addr)
		if !xbe.SameExpr(fieldBase, contextArg) {
			return
		}
		values := l.fieldValues(site.routine, context, fieldOff, 3)
		if len(values) == 0 {
			failed = fmt.Errorf("bios: the handler %08X reaches through field %d of its context, which block %08X hands it as %s, and nothing fixes that field", handler, fieldOff, site.block, xbe.ExprString(context))
			return
		}
		for _, v := range values {
			if at, ok := dv.deviceAddress(nil, "", int32(v+off)); ok {
				registers = appendUnique(registers, at)
			}
		}
	}
	for _, p := range paths {
		for _, e := range p.Effects {
			if e.Kind == "store" {
				visit(e.Addr)
			}
			for _, x := range append([]xbe.Expr{e.Value}, e.Args...) {
				xbe.WalkExpr(x, func(n xbe.Expr) {
					if ld, ok := n.(xbe.Load); ok {
						visit(ld.Addr)
					}
				})
			}
		}
		for _, g := range p.Guards {
			xbe.WalkExpr(g, func(n xbe.Expr) {
				if ld, ok := n.(xbe.Load); ok {
					visit(ld.Addr)
				}
			})
		}
	}
	if failed != nil {
		return nil, nil, failed
	}
	sort.Slice(ports, func(i, j int) bool { return ports[i] < ports[j] })
	sort.Slice(registers, func(i, j int) bool { return registers[i] < registers[j] })
	return ports, registers, nil
}

// The ports and registers a routine's blocks and those of the routines it calls name by constant
func (dv *deriver) routineReach(entry uint32) ([]uint32, []uint32) {
	mm := dv.lifted.Model
	ports, registers := []uint32{}, []uint32{}
	for _, routine := range append([]uint32{entry}, mm.RoutineCallees(entry)...) {
		for _, e := range mm.RoutineEffects(routine) {
			if e.Kind == "call" && (e.Fn == "port_in" || e.Fn == "port_out") {
				if port, ok := xbe.ConstOf(fold(e.Args[0])); ok {
					ports = appendUnique(ports, port)
				}
			}
			if e.Kind == "store" {
				if at, ok := dv.deviceAddress(e.Addr, e.Base, e.Off); ok {
					registers = appendUnique(registers, at)
				}
			}
			for _, x := range append([]xbe.Expr{e.Value}, e.Args...) {
				registers = dv.loadedRegisters(registers, x)
			}
		}
	}
	sort.Slice(ports, func(i, j int) bool { return ports[i] < ports[j] })
	sort.Slice(registers, func(i, j int) bool { return registers[i] < registers[j] })
	return ports, registers
}

// Adds the device registers an expression loads by constant
func (dv *deriver) loadedRegisters(registers []uint32, e xbe.Expr) []uint32 {
	if e == nil {
		return registers
	}
	xbe.WalkExpr(e, func(n xbe.Expr) {
		if ld, ok := n.(xbe.Load); ok {
			if at, isDevice := dv.deviceAddress(ld.Addr, ld.Base, ld.Off); isDevice {
				registers = appendUnique(registers, at)
			}
		}
	})
	return registers
}

func splitOffset(e xbe.Expr) (xbe.Expr, uint32) {
	if bin, ok := e.(xbe.Bin); ok && bin.Op == "add" {
		if c, isConst := xbe.ConstOf(bin.B); isConst {
			return bin.A, c
		}
		if c, isConst := xbe.ConstOf(bin.A); isConst {
			return bin.B, c
		}
	}
	return e, 0
}

// The constants a routine, the helpers it hands the object to, or its callers store into a field of an object
func (l *lifter) fieldValues(routine uint32, object xbe.Expr, off uint32, depth int) []uint32 {
	if depth == 0 {
		return nil
	}
	var out []uint32
	if c, ok := xbe.ConstOf(object); ok {
		return l.globalValues(c+off, 4)
	}
	for _, p := range l.run(routine) {
		for _, e := range p.Effects {
			if e.Kind == "store" && e.Addr != nil && e.Size == 4 {
				base, k := splitOffset(e.Addr)
				if k == off && xbe.SameExpr(base, object) {
					if v, ok := xbe.ConstOf(e.Value); ok {
						out = appendUnique(out, v)
					}
				}
			}
			if e.Kind != "call" {
				continue
			}
			var callee uint32
			if !scanSub(e.Fn, &callee) {
				callee = e.Target
			}
			if callee == 0 || l.d.Functions[callee] == nil {
				continue
			}
			b := l.bind(callee, e)
			for name, v := range b {
				if !xbe.SameExpr(v, object) {
					continue
				}
				var inner xbe.Expr
				if strings.HasPrefix(name, "stack:") {
					var i int32
					fmt.Sscanf(name, "stack:%d", &i)
					inner = xbe.Load{Base: "stack", Off: liftArgBase + 4*i, Size: 4}
				} else {
					inner = xbe.Sym{Name: name}
				}
				for _, v := range l.fieldValues(callee, inner, off, depth-1) {
					out = appendUnique(out, v)
				}
			}
		}
	}
	if readsInputs(object) {
		for _, site := range l.callsTo(routine) {
			b := l.bind(routine, site.effect)
			for _, v := range l.fieldValues(site.caller, substitute(object, b), off, depth-1) {
				out = appendUnique(out, v)
			}
		}
	}
	return out
}

// The lines the executable's drivers connect, each the level a routine hands the vector export while naming by
// constant an address inside a device window, itself or in a routine it calls, kept as a connection to the
// window's base
func (dv *deriver) deriveDeviceLines(pics []PIC) ([]Interrupt, error) {
	vector, err := dv.export(exportVector)
	if err != nil {
		return nil, err
	}
	out := []Interrupt{}
	for _, site := range dv.callSites(vector, 2, exportVector) {
		if site.routine == 0 {
			return nil, fmt.Errorf("bios: block %08X of the executable asks for a vector from no routine", site.block)
		}
		var levels []uint32
		for _, tuple := range dv.argTuples(site, 2) {
			level, ok := xbe.ConstOf(tuple[0])
			if !ok {
				return nil, fmt.Errorf("bios: block %08X of the executable asks for the vector of a level it computes: %s", site.block, xbe.ExprString(tuple[0]))
			}
			levels = appendUnique(levels, level)
		}
		for _, level := range levels {
			for _, base := range dv.windowsNamed(site.routine) {
				if slices.ContainsFunc(out, func(i Interrupt) bool { return i.IRQ == int(level) && i.Registers[0] == base }) {
					continue
				}
				vectorOf := 0
				if int(level)/8 < len(pics) {
					vectorOf = int(pics[level/8].VectorBase) + int(level%8)
				}
				out = append(out, Interrupt{IRQ: int(level), Vector: vectorOf, Ports: []uint32{}, Registers: []uint32{base}})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].IRQ < out[j].IRQ || out[i].IRQ == out[j].IRQ && out[i].Registers[0] < out[j].Registers[0]
	})
	return out, nil
}

// The bases of the device windows a routine or a routine it calls names by constant in its effects
func (dv *deriver) windowsNamed(entry uint32) []uint32 {
	mm := dv.lifted.Model
	var out []uint32
	for _, routine := range append([]uint32{entry}, mm.RoutineCallees(entry)...) {
		for _, e := range mm.RoutineEffects(routine) {
			for _, x := range append([]xbe.Expr{e.Value, e.Addr}, e.Args...) {
				if x == nil {
					continue
				}
				xbe.WalkExpr(x, func(n xbe.Expr) {
					c, ok := xbe.ConstOf(n)
					if !ok {
						return
					}
					if _, isDevice := dv.deviceAddress(nil, "", int32(c)); !isDevice {
						return
					}
					if base, inWindow := dv.windowBase(c); inWindow {
						out = appendUnique(out, base)
					}
				})
			}
		}
	}
	return out
}
