package bios

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"sort"

	"github.com/nickheyer/discoxip/pkg/xbe"
)

// Page is the console as the page boots it, every value read off the flash, the kernel and the disk the build made
type Page struct {
	Boot       *Boot
	Hardware   *Hardware
	Disk       *Disk
	Console    Console
	EEPROM     []byte
	ROM        []byte
	Flash      []byte
	Bldr       []byte
	DiskData   []byte
	Lifted     *Lifted
	Entry      uint32
	EEPROMPart uint8
	SMC        SMCRoles
	CPUID      map[uint32]map[string]uint32
	Polls      []PollAnswer
	MCPX       int
	layout     *deriver
}

// PollAnswer is the reading a polled register answers, passing the exit test of every poll of it, by the register's
// address or, where nothing fixes the address, by the block that spins reading it
type PollAnswer struct {
	Block   uint32 `json:"block"`
	Address uint32 `json:"address"`
	Size    int    `json:"size"`
	Value   uint32 `json:"value"`
}

// SMCRoles is the system management controller's bus address with the read bit clear and its tray command
type SMCRoles struct {
	Address uint8 `json:"address"`
	Tray    uint8 `json:"tray"`
}

// The kernel exports that address the system controller, each reading one command of it
const (
	exportTrayState = "HalReadSMCTrayState"
	exportSettings  = "ExQueryNonVolatileSetting"
)

// The names the page's data files are written under
const (
	ROMFile      = "mcpx.bin"
	DiskRunsDir  = "disk"
	TablesExport = "TABLES"
)

// DerivePage lifts the loader and the kernel, derives the hardware and lays out the disk the page boots from
func DerivePage(b *Bank, mcpx *MCPX, launchDir string, console Console) (*Page, error) {
	lifted, err := b.Machine()
	if err != nil {
		return nil, err
	}
	boot, err := b.Boot()
	if err != nil {
		return nil, err
	}
	partitions, err := b.Partitions(lifted.Model, lifted.Kernel, lifted.Exports)
	if err != nil {
		return nil, err
	}
	launch, err := launchPartition(lifted.Kernel, launchDir)
	if err != nil {
		return nil, err
	}
	hdd, dvd := DefaultDrives()
	disk, diskData, err := BuildDisk(partitions, launch, launchDir, hdd, dvd, console.HDDKey)
	if err != nil {
		return nil, err
	}
	eeprom, err := b.EEPROM(console)
	if err != nil {
		return nil, err
	}
	hardware, layout, err := deriveHardware(b, lifted)
	if err != nil {
		return nil, err
	}
	if err := flatImage(b); err != nil {
		return nil, err
	}
	page := &Page{Boot: boot, Hardware: hardware, Disk: disk, Console: console, EEPROM: eeprom, ROM: mcpx.Data, Flash: b.Data, Bldr: b.Bldr, DiskData: diskData, Lifted: lifted, Entry: lifted.Kernel.EntryPoint, MCPX: mcpx.Revision, layout: layout}
	if page.EEPROMPart, err = eepromPart(lifted); err != nil {
		return nil, err
	}
	if page.SMC, err = smcRoles(lifted); err != nil {
		return nil, err
	}
	if page.CPUID, err = cpuidAnswers(hardware.CPUID); err != nil {
		return nil, err
	}
	if page.Polls, err = pollAnswers(hardware.Polls); err != nil {
		return nil, err
	}
	return page, nil
}

// DeriveExecutable derives the register polls of the executable's blocks over the console's layout and answers
// them alongside the kernel's, and the lines the executable's drivers connect to the devices
func (p *Page) DeriveExecutable(d *xbe.Disassembly, mm *xbe.MachineModel) error {
	ex := p.layout.forExecutable(d, mm)
	polls, err := ex.derivePolls()
	if err != nil {
		return err
	}
	if p.Polls, err = pollAnswers(append(append([]Poll{}, p.Hardware.Polls...), polls...)); err != nil {
		return err
	}
	lines, err := ex.deriveDeviceLines(p.Hardware.PICs)
	if err != nil {
		return err
	}
	p.Hardware.Interrupts = append(p.Hardware.Interrupts, lines...)
	return nil
}

// The kernel's sections must sit in the file where the loader runs them, since the loader validates the headers
// in place and calls the entry without moving a section
func flatImage(b *Bank) error {
	pe, err := ParsePE(b.Kernel)
	if err != nil {
		return err
	}
	for _, s := range pe.Sections {
		if s.RawOffset != s.VirtualAddress && s.RawSize != 0 {
			return fmt.Errorf("bios: kernel section %s sits at file offset %#x and runs at %#x, so the loader would have to move it", s.Name, s.RawOffset, s.VirtualAddress)
		}
	}
	return nil
}

// The serial EEPROM's bus address as the kernel passes it, the read bit clear
func eepromPart(lifted *Lifted) (uint8, error) {
	slave, err := xbe.EEPROMSlave(lifted.KernelDis, lifted.Exports)
	if err != nil {
		return 0, err
	}
	return uint8(slave &^ 1), nil
}

// The system controller's bus address and tray command, read off the tray state export's one bus read
func smcRoles(lifted *Lifted) (SMCRoles, error) {
	var tray, read uint32
	for ordinal, va := range lifted.Exports {
		switch xbe.KernelExportName(ordinal) {
		case exportTrayState:
			tray = va
		case exportReadSMBus:
			read = va
		}
	}
	if tray == 0 || read == 0 {
		return SMCRoles{}, fmt.Errorf("bios: the kernel exports no %s and %s", exportTrayState, exportReadSMBus)
	}
	fn := lifted.KernelDis.Functions[tray]
	if fn == nil {
		return SMCRoles{}, fmt.Errorf("bios: %s is not disassembled", exportTrayState)
	}
	ex := xbe.NewExecutor(lifted.KernelDis)
	ex.Opaque[read] = true
	paths, _, err := ex.Paths(tray, liftRetries)
	if err != nil {
		return SMCRoles{}, fmt.Errorf("bios: lifting %s: %w", exportTrayState, err)
	}
	var roles []SMCRoles
	for _, p := range paths {
		for _, e := range p.Effects {
			if e.Kind != "call" || e.Fn != exportReadSMBus || len(e.Args) < 2 {
				continue
			}
			address, ok1 := xbe.ConstOf(e.Args[0])
			command, ok2 := xbe.ConstOf(e.Args[1])
			if !ok1 || !ok2 {
				return SMCRoles{}, fmt.Errorf("bios: %s reads the bus at %s command %s, which it computes", exportTrayState, xbe.ExprString(e.Args[0]), xbe.ExprString(e.Args[1]))
			}
			r := SMCRoles{Address: uint8(address &^ 1), Tray: uint8(command)}
			if len(roles) == 0 || roles[0] != r {
				roles = append(roles, r)
			}
		}
	}
	if len(roles) != 1 {
		return SMCRoles{}, fmt.Errorf("bios: %s reads %d different bus parts and commands", exportTrayState, len(roles))
	}
	return roles[0], nil
}

// The answers to each processor identification leaf that pass every test the boot applies to them
func cpuidAnswers(uses []CPUIDUse) (map[uint32]map[string]uint32, error) {
	out := map[uint32]map[string]uint32{}
	for _, u := range uses {
		regs := out[u.Leaf]
		if regs == nil {
			regs = map[string]uint32{"EAX": 0, "EBX": 0, "ECX": 0, "EDX": 0}
			out[u.Leaf] = regs
		}
		for _, t := range u.Tests {
			v, err := satisfy(regs[t.Register], t)
			if err != nil {
				return nil, fmt.Errorf("bios: leaf %#x %s: %w", u.Leaf, t.Register, err)
			}
			regs[t.Register] = v
		}
		for _, t := range u.Tests {
			if !passes(regs[t.Register], t) {
				return nil, fmt.Errorf("bios: leaf %#x %s cannot satisfy %s %#x against %#x with mask %#x alongside the leaf's other tests", u.Leaf, t.Register, t.Op, t.Value, regs[t.Register], t.And)
			}
		}
	}
	return out, nil
}

// The reading each polled register answers, changed in turn so that every poll's exit test passes, the polls of
// one address pooled and a poll fixing no address standing by its block
func pollAnswers(polls []Poll) ([]PollAnswer, error) {
	out := []PollAnswer{}
	test := func(p Poll) RegisterTest { return RegisterTest{And: p.And, Op: p.Op, Value: p.Value} }
	keyOf := func(p Poll) PollAnswer {
		if p.Address != 0 {
			return PollAnswer{Address: p.Address, Size: p.Size}
		}
		return PollAnswer{Block: p.Block, Size: p.Size}
	}
	for _, p := range polls {
		key := keyOf(p)
		i := slices.IndexFunc(out, func(a PollAnswer) bool { return a.Block == key.Block && a.Address == key.Address && a.Size == key.Size })
		if i < 0 {
			out = append(out, key)
			i = len(out) - 1
		}
		v, err := satisfy(out[i].Value, test(p))
		if err != nil {
			return nil, fmt.Errorf("bios: register %#x polled by block %08X of the %s: %w", p.Address, p.Block, p.Stage, err)
		}
		out[i].Value = v
	}
	for _, a := range out {
		for _, p := range polls {
			if key := keyOf(p); key.Block == a.Block && key.Address == a.Address && key.Size == a.Size && !passes(a.Value, test(p)) {
				return nil, fmt.Errorf("bios: register %#x polled by block %08X cannot satisfy %s %#x under mask %#x alongside its other polls", p.Address, p.Block, p.Op, p.Value, p.And)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Address != b.Address {
			return a.Address < b.Address
		}
		if a.Block != b.Block {
			return a.Block < b.Block
		}
		return a.Size < b.Size
	})
	return out, nil
}

// A value changed in the masked bits so that one test passes, the other bits kept
func satisfy(v uint32, t RegisterTest) (uint32, error) {
	if passes(v, t) {
		return v, nil
	}
	for _, candidate := range []uint32{t.Value, t.Value + 1, t.Value - 1, t.Value ^ t.And, t.Value + 1 + ^t.And, 0, t.And} {
		w := v&^t.And | candidate&t.And
		if passes(w, t) {
			return w, nil
		}
	}
	return 0, fmt.Errorf("no value passes %s %#x under mask %#x", t.Op, t.Value, t.And)
}

func passes(v uint32, t RegisterTest) bool {
	x := v & t.And
	var ok bool
	switch t.Op {
	case "eq", "not ne":
		ok = x == t.Value
	case "ne", "not eq":
		ok = x != t.Value
	case "ult", "not uge":
		ok = x < t.Value
	case "ule", "not ugt":
		ok = x <= t.Value
	case "ugt", "not ule":
		ok = x > t.Value
	case "uge", "not ult":
		ok = x >= t.Value
	case "lt", "not ge":
		ok = int32(x) < int32(t.Value)
	case "le", "not gt":
		ok = int32(x) <= int32(t.Value)
	case "gt", "not le":
		ok = int32(x) > int32(t.Value)
	case "ge", "not lt":
		ok = int32(x) >= int32(t.Value)
	}
	return ok
}

// pageTables is what the page reads of the console at load, beside the runtime's own tables
type pageTables struct {
	Boot       *Boot                        `json:"boot"`
	Hardware   *Hardware                    `json:"hardware"`
	Disk       *Disk                        `json:"disk"`
	Console    pageConsole                  `json:"console"`
	EEPROM     []int                        `json:"eeprom"`
	EEPROMPart uint8                        `json:"eeprom_part"`
	SMC        SMCRoles                     `json:"smc"`
	CPUID      map[uint32]map[string]uint32 `json:"cpuid"`
	Polls      []PollAnswer                 `json:"poll_answers"`
	CounterHz  uint32                       `json:"counter_hz"`
	Entry      uint32                       `json:"kernel_entry"`
	Files      map[string]string            `json:"files"`
	BlockFiles []string                     `json:"block_files"`
	ROMBase    uint32                       `json:"rom_base"`
	Answers    initAnswerTables             `json:"init_answers"`
	Tested     []uint32                     `json:"tested_memory"`
}

// The memory addresses the init table tests, in address order
func testedMemory(t *InitTable) []uint32 {
	out := []uint32{}
	if t == nil {
		return out
	}
	for addr := range t.memoryTests() {
		out = append(out, addr)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// initAnswerTables are the values the init table's tests pass unchanged, which the devices answer its reads with
type initAnswerTables struct {
	Port   map[uint32]uint32 `json:"port"`
	PCI    map[uint32]uint32 `json:"pci"`
	Memory map[uint32]uint32 `json:"memory"`
}

// The answers the init table's reads expect, by the space each read addresses
func initAnswerTablesOf(t *InitTable) initAnswerTables {
	out := initAnswerTables{Port: map[uint32]uint32{}, PCI: map[uint32]uint32{}, Memory: map[uint32]uint32{}}
	if t == nil {
		return out
	}
	tested := t.memoryTests()
	for _, r := range t.Reads {
		for _, test := range r.Tests {
			if test.Value&test.And|test.Or != test.Value {
				continue
			}
			switch r.Space {
			case SpacePort:
				out.Port[r.Address] = test.Value
			case SpacePCI:
				out.PCI[r.Address] = test.Value
			case SpaceMemory:
				if !tested[r.Address] {
					out.Memory[r.Address] = test.Value
				}
			}
		}
	}
	return out
}

// pageConsole is the console state the page answers the system controller and the southbridge's revision with,
// and whether a daughterboard hub carries its controller ports
type pageConsole struct {
	AVPack      uint32  `json:"av_pack"`
	Tray        uint32  `json:"tray"`
	FieldMs     float64 `json:"field_ms"`
	MCPRevision uint8   `json:"mcp_revision"`
	SMCVersion  string  `json:"smc_version"`
	USBHub      bool    `json:"usb_hub"`
}

// The configuration address of a function, as the address port takes it
func pciAddress(bus, device, function int) uint32 {
	return pciEnableBit | uint32(bus)<<16 | uint32(device)<<11 | uint32(function)<<8
}

// The time between the display's vertical blanks, a field of the standard the console's video setting names
func fieldPeriodMs(video uint32) (float64, error) {
	switch {
	case video&videoRefresh50 != 0:
		return 1000 / float64(fieldHz50), nil
	case video&videoRefresh60 != 0:
		return 1000 * float64(fieldHz60Divisor) / float64(fieldHz60Scaled), nil
	}
	return 0, fmt.Errorf("bios: the console's video standard %#x names no refresh", video)
}

// Files writes the data files the page loads beside its runtime and names them for the tables
func (p *Page) Files(write func(name string, data []byte) error) (map[string]string, error) {
	files := map[string]string{}
	for name, data := range map[string][]byte{FlashFile: p.Flash, BldrFile: p.Bldr, ROMFile: p.ROM} {
		if err := write(name, data); err != nil {
			return nil, err
		}
		files[name] = name
	}
	for i, r := range p.Disk.Runs {
		name := fmt.Sprintf("%s/%d.bin", DiskRunsDir, i)
		if err := write(name, p.DiskData[r.Offset:r.Offset+r.Size]); err != nil {
			return nil, err
		}
		files[fmt.Sprintf("run%d", i)] = name
	}
	return files, nil
}

// WriteTables writes the console's tables as the page's constant, the data files named as written and the scripts
// holding the translated blocks in the order the page loads them
func (p *Page) WriteTables(w io.Writer, files map[string]string, blockFiles []string) error {
	eeprom := make([]int, len(p.EEPROM))
	for i, b := range p.EEPROM {
		eeprom[i] = int(b)
	}
	fieldMs, err := fieldPeriodMs(p.Console.VideoStandard)
	if err != nil {
		return err
	}
	if p.MCPX < 0 || p.MCPX >= len(mcpxConsoles) {
		return fmt.Errorf("bios: no console carries MCPX ROM revision 1.%d", p.MCPX)
	}
	model := mcpxConsoles[p.MCPX]
	t := pageTables{Boot: p.Boot, Hardware: p.Hardware, Disk: p.Disk, Console: pageConsole{AVPack: p.Console.AVPack, Tray: smcTrayClosedNoMedia, FieldMs: fieldMs, MCPRevision: model.revision, SMCVersion: model.smcVersion, USBHub: model.daughterboard}, EEPROM: eeprom, EEPROMPart: p.EEPROMPart, SMC: p.SMC, CPUID: p.CPUID, Polls: p.Polls, CounterHz: p.Hardware.Timer.CounterHz, Entry: p.Entry, Files: files, BlockFiles: blockFiles, ROMBase: romBase, Answers: initAnswerTablesOf(p.Hardware.InitTable), Tested: testedMemory(p.Hardware.InitTable)}
	data, err := json.Marshal(t)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "const %s = %s;\n", TablesExport, data)
	return err
}

// The windows the page lays the devices out by, every base address register assignment in order with the
// function it belongs to, so the page decodes an address through the latest assignment below it
type pageWindow struct {
	Bus, Device, Function int
	Register              int
	Base                  uint32
	IO                    bool
	Extent                uint32
}

func (p *Page) windows() []pageWindow {
	var out []pageWindow
	for _, fn := range p.Hardware.PCI {
		for _, w := range fn.Windows {
			out = append(out, pageWindow{Bus: fn.Bus, Device: fn.Device, Function: fn.Function, Register: w.Register, Base: w.Base, IO: w.IO, Extent: w.Extent})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Base < out[j].Base })
	return out
}

// WriteHardware writes the console's hardware as the page answers it, each device from what the boot derived
func (p *Page) WriteHardware(w io.Writer) error {
	bw := bufio.NewWriterSize(w, 1<<20)
	e := &emitter{w: bw, page: p}
	e.writeTime()
	e.writeProcessorAnswers()
	e.writeAddressSpace()
	e.writeGraphics()
	e.writeGPU()
	e.writeDSP()
	e.writeAPU()
	e.writeACI()
	e.writePCI()
	e.writeTimer()
	e.writeControllers()
	e.writeSMBus()
	e.writeIDE()
	e.writeClock()
	e.writePorts()
	e.writeInitTable()
	e.writeBoot()
	e.writeNIC()
	e.writeUSB()
	e.writeDeviceDispatch()
	e.writeStall()
	return bw.Flush()
}

type emitter struct {
	w    *bufio.Writer
	page *Page
}

func (e *emitter) js(format string, args ...any) {
	fmt.Fprintf(e.w, format+"\n", args...)
}
