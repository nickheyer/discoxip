package xbe

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math/bits"
	"os"
	"slices"
	"sort"
	"strings"

	"github.com/nickheyer/discoxip/pkg/texture"
	"golang.org/x/arch/x86/x86asm"
)

// Where the program replay hands out heap and contiguous memory, the alignments it hands them out at, the first
// vertex shader handle it gives, the retries a lift of a kernel routine may take and the helpers it follows under one
const (
	programArena       = 0x40000000
	programHeapAlign   = 16
	programPageAlign   = 4096
	programObjectSize  = 0x20
	programHandleBase  = 0x10000
	programLiftTries   = 4
	programHelperDepth = 3
)

// The startup thread routine, the CRT initializer main follows, the allocator a launcher builds its application
// with, the virtual slots it then calls and the name the library gives the routines that flip the display
const (
	programStartup    = "mainXapiStartup"
	programCRTInit    = "_cinit"
	programAllocator  = "operator_new"
	programCreateSlot = "vcall@0"
	programRunSlot    = "vcall@4"
	programFlip       = "Flip"
)

// The library routine filling a program kept texture header, its argument roles, and the routine binding it to memory
const (
	programHeaderBuilder = "XGSetTextureHeader"
	programHeaderWidth   = 0
	programHeaderHeight  = 1
	programHeaderLevels  = 2
	programHeaderUsage   = 3
	programHeaderFormat  = 4
	programHeaderTexture = 6
	programHeaderOffset  = 7
	programHeaderPitch   = 8
	programRegister      = "D3DResource_Register"
)

// The routines binding what a draw uses, the frame operations the replay records and the kinds it records them as
const (
	programSetTexture        = "D3DDevice_SetTexture"
	programSetRenderTarget   = "D3DDevice_SetRenderTarget"
	programSetPixelShader    = "D3DDevice_SetPixelShader"
	programCreatePixelShader = "D3DDevice_CreatePixelShader"
	programClearCall         = "D3DDevice_Clear"
	programKindDraw          = "draw"
	programKindClear         = "clear"
	programRectWords         = 4
)

// The kernel routines reading the console's settings and the bus they read the EEPROM over
const (
	programSettings = "ExQueryNonVolatileSetting"
	programBusRead  = "HalReadSMBusValue"
	programBusWrite = "HalWriteSMBusValue"
)

// The section holding the library that drives the GPU, every routine of which the replay answers unless it is a
// leaf without loops that stores only through its arguments
var programDeviceSections = map[string]bool{"D3D": true}

// XAPI routines the replay answers, the heap because the startup that creates it never ran, the input devices and
// the clock because no hardware answers them
var programAPIHooks = map[string]string{
	"RtlAllocateHeap":   "alloc",
	"RtlFreeHeap":       "one",
	"RtlReAllocateHeap": "realloc",
	"RtlSizeHeap":       "size",
	"GetTickCount":      "tick",
	"XInitDevices":      "zero",
	"XGetDevices":       "zero",
	"XGetDeviceChanges": "zero",
	"XInputOpen":        "zero",
	"XInputClose":       "zero",
	"XInputGetState":    "zero",
	"XInputPoll":        "zero",
	"XInputSetState":    "zero",
	"XLaunchNewImage":   "leave",
	"XapiBootToDash":    "leave",
}

// Kernel exports the replay answers, section mapping from the header the loader filled, memory from the arena and
// the debugger and the scheduler at once
var programKernelHooks = map[string]string{
	"XeLoadSection":                "load",
	"XeUnloadSection":              "unload",
	"MmAllocateContiguousMemory":   "contiguous",
	"MmAllocateContiguousMemoryEx": "contiguous",
	"MmFreeContiguousMemory":       "zero",
	"MmPersistContiguousMemory":    "zero",
	"NtAllocateVirtualMemory":      "virtual",
	"NtFreeVirtualMemory":          "zero",
	"DbgPrint":                     "zero",
	"KeDelayExecutionThread":       "zero",
	"KeQueryPerformanceCounter":    "counter",
	"HalReturnToFirmware":          "leave",
	"HalReadSMBusValue":            "bus",
	"HalWriteSMBusValue":           "buswrite",
	"IoCreateSymbolicLink":         "zero",
	"IoDeleteSymbolicLink":         "zero",
	"ExAllocatePoolWithTag":        "contiguous",
	"ExAllocatePool":               "contiguous",
	"ExFreePool":                   "zero",
	"ExQueryPoolBlockSize":         "allocated",
	"MmQueryAllocationSize":        "allocated",
	"NtCreateFile":                 "notfound",
	"NtOpenFile":                   "notfound",
}

// The status the kernel answers an open of a path no file system holds with
const programNotFound = 0xC0000034

// The D3D object types the resource headers carry in their common word, and the resource types the texture
// creator is asked for
const (
	programTypeVertexBuffer = 0
	programTypeIndexBuffer  = 1
	programTypeTexture      = 4
	programTypeSurface      = 5
	programTypeShift        = 16
	programRTypeTexture     = 3
	programRTypeCube        = 5
)

// The dimensionality and cube bit of a texture format register, and the field positions the register packs
const (
	programFormatDim2D   = 2 << 4
	programFormatCube    = 1 << 2
	programFormatShift   = 8
	programFormatMips    = 16
	programFormatLog2U   = 20
	programFormatLog2V   = 24
	programCubeFaces     = 6
	programDescType      = 0x04
	programDescUsage     = 0x08
	programDescSize      = 0x0C
	programRectPitch     = 0
	programRectBits      = 4
	programPresentWidth  = 0x00
	programPresentHeight = 0x04
	programPresentFormat = 0x08
	programPresentFlags  = 0x28
	programPresentSize   = 0x34
	programModeWidth     = 0x00
	programModeHeight    = 0x04
	programModeRefresh   = 0x08
	programModeFlags     = 0x0C
	programModeFormat    = 0x10
	programModeRefreshHz = 60
	programHeaderName    = 0x14
	programResourceData  = 0x04
	programDeclEnd       = 0xFFFFFFFF
	programTickStep      = 16
	programCounterStep   = 1 << 12
)

// ProgramModel is what a standalone executable's startup hands the GPU and keeps in memory out of its own sections,
// read by replaying it from main to its first frame
type ProgramModel struct {
	Main       uint32                `json:"main"`
	Launchers  []uint32              `json:"launchers"`
	RunFrames  []int                 `json:"run_frames"`
	Width      uint32                `json:"width"`
	Height     uint32                `json:"height"`
	BackBuffer uint32                `json:"back_buffer_format"`
	Flags      uint32                `json:"present_flags"`
	Present    []uint32              `json:"present_parameters"`
	Sections   []string              `json:"sections"`
	Textures   []*ProgramTexture     `json:"textures"`
	Buffers    []*ProgramBuffer      `json:"buffers"`
	Tables     []*ProgramTable       `json:"tables"`
	Shaders    []*ProgramShader      `json:"shaders"`
	Pixel      []*ProgramPixelShader `json:"pixel_shaders"`
	Draws      []ProgramDraw         `json:"draws"`
}

// ProgramPixelShader is one pixel shader the program created, with the definition words it handed the library
type ProgramPixelShader struct {
	Handle uint32   `json:"handle"`
	Def    []uint32 `json:"def"`
}

// ProgramTexture is one texture the program created, with the pixels it wrote through locks, one face after another
// and each face's levels in order
type ProgramTexture struct {
	Object   uint32   `json:"object"`
	Launcher int      `json:"launcher"`
	Section  string   `json:"section"`
	Width    int      `json:"width"`
	Height   int      `json:"height"`
	Levels   int      `json:"levels"`
	Format   uint32   `json:"format"`
	Cube     bool     `json:"cube"`
	Usage    uint32   `json:"usage"`
	Faces    [][]byte `json:"-"`
	data     uint32
	face     int
	levels   []programLevel
	pending  bool
	screen   bool
}

// programLevel is one mip level's size and where it lies inside a face
type programLevel struct {
	width, height, bytes, offset int
}

// ProgramBuffer is one vertex or index buffer the program created, with the bytes it wrote into it
type ProgramBuffer struct {
	Object   uint32 `json:"object"`
	Launcher int    `json:"launcher"`
	Section  string `json:"section"`
	Kind     string `json:"kind"`
	Size     uint32 `json:"size"`
	Data     []byte `json:"-"`
	data     uint32
}

// ProgramTable is a heap block the program filled with bytes of a section it had loaded
type ProgramTable struct {
	Address  uint32 `json:"address"`
	Launcher int    `json:"launcher"`
	Section  string `json:"section"`
	Offset   int    `json:"offset"`
	Size     int    `json:"size"`
	Data     []byte `json:"-"`
}

// ProgramShader is one vertex shader the program created, its declaration and program decoded
type ProgramShader struct {
	Handle      uint32             `json:"handle"`
	Declaration *VertexDeclaration `json:"declaration"`
	Program     *VertexProgram     `json:"program"`
}

// ProgramDraw is one draw or clear before the first frame on the screen, with everything bound at the time
type ProgramDraw struct {
	Launcher      int                          `json:"launcher"`
	Frame         int                          `json:"frame"`
	Kind          string                       `json:"kind"`
	Target        uint32                       `json:"target"`
	Face          int                          `json:"face"`
	Level         int                          `json:"level"`
	Shader        uint32                       `json:"shader"`
	PixelShader   uint32                       `json:"pixel_shader"`
	Stream        uint32                       `json:"stream"`
	Stride        uint32                       `json:"stride"`
	Primitive     uint32                       `json:"primitive"`
	Start         uint32                       `json:"start"`
	Count         uint32                       `json:"count"`
	Indices       []uint16                     `json:"indices,omitempty"`
	Textures      []uint32                     `json:"textures"`
	Constants     map[uint32][]uint32          `json:"constants"`
	States        map[string]uint32            `json:"states"`
	TextureStates map[uint32]map[string]uint32 `json:"texture_states"`
	Flags         uint32                       `json:"flags,omitempty"`
	Color         uint32                       `json:"color,omitempty"`
	Z             uint32                       `json:"z,omitempty"`
	Stencil       uint32                       `json:"stencil,omitempty"`
	Rects         [][]uint32                   `json:"rects,omitempty"`
}

// programRun is the state the replay keeps beside the machine run
type programRun struct {
	d, kd      *Disassembly
	run        *MachineRun
	model      *ProgramModel
	eeprom     []byte
	eepromAt   uint32
	trace      bool
	answering  string
	sizes      map[uint32]uint32
	launcher   int
	kernel     *KernelInput
	arena      uint32
	tick       uint32
	counter    uint64
	loaded     []string
	textures   map[uint32]*ProgramTexture
	buffers    map[uint32]*ProgramBuffer
	surfaces   map[uint32]programSurface
	heap       []programHeapBlock
	shader     uint32
	pixel      uint32
	target     uint32
	stream     uint32
	stride     uint32
	indices    uint32
	bound      []uint32
	constants  map[uint32][]uint32
	states     *programStates
	build      int
	frames     int
	runReturn  uint32
	running    bool
	screen     bool
	shown      bool
	left       bool
	nextHandle uint32
}

// programSurface aliases one level of one face of a texture
type programSurface struct {
	tex   *ProgramTexture
	face  int
	level int
}

// programHeapBlock is one heap allocation with the section loaded when it was made
type programHeapBlock struct {
	addr, size uint32
	section    string
}

// DeriveProgram replays a standalone executable's applications, main's and every other launcher's, each from its
// launcher to its first frame, and reports what they load
func DeriveProgram(d *Disassembly, kernel *KernelInput) (*ProgramModel, error) {
	kd, err := DisassembleKernel(kernel.Image, kernel.Exports)
	if err != nil {
		return nil, err
	}
	d.LearnImports(kd)
	main, mainArgs, err := d.programMain()
	if err != nil {
		return nil, err
	}
	rd, err := d.resolvedImports(kernel.Exports)
	if err != nil {
		return nil, err
	}
	launchers, runCalls, err := rd.programLaunchers(main)
	if err != nil {
		return nil, err
	}
	mm, err := LiftMachineRooted(rd, launchers, NewExprPool())
	if err != nil {
		return nil, err
	}
	rd.Image = mergedImage(rd.Image, kernel.Image)
	pr := &programRun{d: rd, kd: kd, kernel: kernel, model: &ProgramModel{Main: main, Launchers: launchers, Sections: []string{}, Textures: []*ProgramTexture{}, Buffers: []*ProgramBuffer{}, Tables: []*ProgramTable{}, Shaders: []*ProgramShader{}, Pixel: []*ProgramPixelShader{}, Draws: []ProgramDraw{}}, arena: programArena, textures: map[uint32]*ProgramTexture{}, buffers: map[uint32]*ProgramBuffer{}, surfaces: map[uint32]programSurface{}, nextHandle: programHandleBase, sizes: map[uint32]uint32{}, trace: os.Getenv("DISCOXIP_PROGRAM_TRACE") != ""}
	pr.eeprom = kernel.EEPROM
	if pr.eepromAt, err = eepromSlave(kd, kernel); err != nil {
		return nil, err
	}
	pr.build = int(rd.Image.LibBuild(programDeviceLibrary))
	if pr.states, err = deriveStateTables(rd); err != nil {
		return nil, err
	}
	if rd.symbolVA(programSetTexture) == 0 {
		return nil, fmt.Errorf("xbe: no routine of the device library binds a texture to a stage")
	}
	for i, launcher := range launchers {
		call := rd.insnAt(runCalls[launcher])
		if call == nil {
			return nil, fmt.Errorf("xbe: launcher %08X runs its application from %08X, which holds no instruction", launcher, runCalls[launcher])
		}
		run := NewMachineRun(mm, rd.Image, map[string]uint32{"ESP": ReplayStack})
		pr.begin(run, i, call.VA+uint32(call.Inst.Len))
		args := mainArgs
		if launcher != main {
			args = rd.cleanupOf(rd.Functions[launcher]) / 4
		}
		if _, err := rd.replayInto(run, kd, launcher, make([]uint32, args)); err != nil {
			return nil, fmt.Errorf("xbe: replaying launcher %08X: %w", launcher, err)
		}
		if pr.left {
			return nil, fmt.Errorf("xbe: launcher %08X leaves the program before its first frame", launcher)
		}
		if !pr.shown {
			return nil, fmt.Errorf("xbe: launcher %08X returns after %d frames without its run drawing on the screen", launcher, pr.frames)
		}
		pr.collect()
	}
	return pr.model, nil
}

// Starts a launcher's run, the hooks installed, the state one run keeps cleared and the return from the call
// running the application marking where its run begins
func (pr *programRun) begin(run *MachineRun, launcher int, runReturn uint32) {
	pr.run, pr.launcher, pr.runReturn = run, launcher, runReturn
	pr.loaded, pr.heap = nil, nil
	pr.shader, pr.pixel, pr.target, pr.stream, pr.stride, pr.indices = 0, 0, 0, 0, 0, 0
	pr.bound = make([]uint32, textureStages)
	pr.constants = map[uint32][]uint32{}
	pr.frames, pr.running, pr.screen, pr.shown, pr.left = 0, false, false, false, false
	pr.installHooks(pr.kernel)
	run.IO = func(r *MachineRun, out bool, port, value, size uint32) uint32 {
		r.Advance()
		pr.counter += programCounterStep
		return uint32(pr.counter)
	}
	var names map[uint32]string
	if pr.trace {
		names = map[uint32]string{}
		for ordinal, va := range pr.kernel.Exports {
			names[va] = KernelExportName(ordinal)
		}
		for _, fn := range pr.d.Functions {
			if fn.Symbol != nil {
				names[fn.EntryVA] = fn.Symbol.Name
			}
		}
	}
	run.OnCall = func(r *MachineRun, target, ret uint32) {
		if ret == pr.runReturn && !pr.running {
			pr.running, pr.screen = true, false
			pr.model.RunFrames = append(pr.model.RunFrames, pr.frames)
		}
		if name, named := names[target]; named {
			fmt.Fprintf(os.Stderr, "program: %08X calls %s at %08X\n", ret, name, target)
		}
	}
}

// The application launchers, every unnamed routine in main's section that allocates an object, constructs it and
// calls its first two virtual slots in turn, main among them, each with the call running its application
func (d *Disassembly) programLaunchers(main uint32) ([]uint32, map[uint32]uint32, error) {
	alloc := d.symbolVA(programAllocator)
	if alloc == 0 {
		return nil, nil, fmt.Errorf("xbe: the image names no %s", programAllocator)
	}
	code := d.sectionOf(main)
	var out []uint32
	runs := map[uint32]uint32{}
	for va, fn := range d.Functions {
		if fn.Symbol != nil || d.sectionOf(va) != code || !slices.Contains(fn.CallTargets, alloc) {
			continue
		}
		ex := NewExecutor(d)
		for _, t := range fn.CallTargets {
			ex.Opaque[t] = true
		}
		paths, partial, err := ex.Paths(va, programLiftTries)
		if err != nil {
			return nil, nil, fmt.Errorf("xbe: lifting %08X, which allocates an object: %w", va, err)
		}
		if call, ok := launchCall(append(paths, Path{Effects: partial})); ok {
			out = append(out, va)
			runs[va] = call
		}
	}
	if !slices.Contains(out, main) {
		return nil, nil, fmt.Errorf("xbe: main %08X launches no application", main)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] == main || out[j] != main && out[i] < out[j] })
	return out, runs, nil
}

// The call running an application, where a path allocates an object, hands it to an unnamed constructor and
// calls its first two virtual slots in turn, a virtual call carrying its receiver as its first argument
func launchCall(paths []Path) (uint32, bool) {
	for _, p := range paths {
		var object, constructed *CallResult
		created := false
		for _, e := range p.Effects {
			if e.Kind != "call" {
				continue
			}
			if e.Fn == programAllocator {
				object, constructed, created = &CallResult{Fn: e.Fn, Seq: e.Seq}, nil, false
				continue
			}
			receiver := e.This
			if strings.HasPrefix(e.Fn, "vcall@") && len(e.Args) > 0 {
				receiver = e.Args[0]
			}
			switch {
			case object != nil && constructed == nil && strings.HasPrefix(e.Fn, "sub_") && resultOf(receiver, object):
				constructed = &CallResult{Fn: e.Fn, Seq: e.Seq}
			case constructed != nil && e.Fn == programCreateSlot && (resultOf(receiver, constructed) || resultOf(receiver, object)):
				created = true
			case created && e.Fn == programRunSlot && (resultOf(receiver, constructed) || resultOf(receiver, object)):
				return e.VA, true
			}
		}
	}
	return 0, false
}

// Reports whether an expression is the result of a recorded call
func resultOf(e Expr, call *CallResult) bool {
	r, ok := e.(CallResult)
	return ok && call != nil && r.Fn == call.Fn && r.Seq == call.Seq
}

// The bus address the kernel's settings query reads the EEPROM at, the one constant slave its paths and those of
// the unnamed helpers it calls hand the bus reader
// EEPROMSlave is the bus address the settings export reads the serial EEPROM at, as the kernel's own code passes it
func EEPROMSlave(kd *Disassembly, exports map[uint32]uint32) (uint32, error) {
	return eepromSlave(kd, &KernelInput{Exports: exports})
}

func eepromSlave(kd *Disassembly, kernel *KernelInput) (uint32, error) {
	var settings uint32
	for ordinal, va := range kernel.Exports {
		if KernelExportName(ordinal) == programSettings {
			settings = va
		}
	}
	if settings == 0 || kd.Functions[settings] == nil {
		return 0, fmt.Errorf("xbe: the kernel exports no %s routine", programSettings)
	}
	slaves := map[uint32]bool{}
	calls := map[string]bool{}
	seen := map[uint32]bool{}
	queue := []uint32{settings}
	for depth := 0; depth <= programHelperDepth && len(queue) > 0; depth++ {
		var next []uint32
		for _, routine := range queue {
			if seen[routine] {
				continue
			}
			seen[routine] = true
			ex := NewExecutor(kd)
			for va, fn := range kd.Functions {
				if fn.Symbol != nil && va != routine {
					ex.Opaque[va] = true
				}
			}
			paths, partial, err := ex.Paths(routine, programLiftTries)
			if err != nil {
				return 0, fmt.Errorf("xbe: lifting %08X under %s: %w", routine, programSettings, err)
			}
			paths = append(paths, Path{Effects: partial})
			for _, p := range paths {
				for _, e := range p.Effects {
					if e.Kind != "call" {
						continue
					}
					calls[e.Fn] = true
					var callee uint32
					if _, err := fmt.Sscanf(e.Fn, "sub_%08X", &callee); err == nil && kd.Functions[callee] != nil {
						next = append(next, callee)
					}
					if e.Fn != programBusRead || len(e.Args) == 0 {
						continue
					}
					if c, ok := isConst(e.Args[0]); ok {
						slaves[c] = true
					}
				}
			}
		}
		queue = next
	}
	if len(slaves) != 1 {
		names := make([]string, 0, len(calls))
		for name := range calls {
			names = append(names, name)
		}
		sort.Strings(names)
		return 0, fmt.Errorf("xbe: %s reads %d bus parts, expected the EEPROM alone, calling %v", programSettings, len(slaves), names)
	}
	for slave := range slaves {
		return slave, nil
	}
	return 0, nil
}

// main is the first unnamed routine the startup thread calls after the CRT initializer, with the words the startup
// pushes for it
func (d *Disassembly) programMain() (uint32, int, error) {
	startup := d.symbolVA(programStartup)
	if startup == 0 {
		return 0, 0, fmt.Errorf("xbe: the image names no %s routine", programStartup)
	}
	cinit := d.symbolVA(programCRTInit)
	if cinit == 0 {
		return 0, 0, fmt.Errorf("xbe: the image names no %s routine", programCRTInit)
	}
	seen, pushes := false, 0
	for _, in := range d.Functions[startup].Instructions {
		if in.Inst.Op == x86asm.PUSH {
			pushes++
		}
		if in.Inst.Op != x86asm.CALL {
			continue
		}
		t := resolveTarget(&in)
		if t == cinit {
			seen = true
			pushes = 0
			continue
		}
		if fn := d.Functions[t]; seen && fn != nil && fn.Symbol == nil {
			return t, pushes, nil
		}
		pushes = 0
	}
	return 0, 0, fmt.Errorf("xbe: %s calls no unnamed routine after %s", programStartup, programCRTInit)
}

// Hands out arena memory, remembering each block's size for the queries the kernel answers about its allocations
func (pr *programRun) alloc(size, align uint32) uint32 {
	pr.run.Advance()
	pr.arena = (pr.arena + align - 1) &^ (align - 1)
	at := pr.arena
	if size == 0 {
		size = align
	}
	pr.arena += size
	pr.sizes[at] = size
	return at
}

// The arguments a call left above its return address
func (pr *programRun) args(n int) []uint32 {
	out := make([]uint32, n)
	for i := range out {
		out[i] = pr.run.ReadU32(pr.run.Regs["ESP"] + 4 + 4*uint32(i))
	}
	return out
}

// Returns from a hooked routine the way its own return would, the result in EAX
func (pr *programRun) ret(cleanup int, result uint32) uint32 {
	r := pr.run
	if pr.trace {
		fmt.Fprintf(os.Stderr, "program: %s(%X) answered %X, %d bytes removed, back to %08X\n", pr.answering, pr.args(cleanup/4), result, cleanup, r.ReadU32(r.Regs["ESP"]))
	}
	ret := r.Pop()
	r.Regs["ESP"] += uint32(cleanup)
	r.Regs["EAX"] = result
	return ret
}

// Installs the hooks on the executable's library routines and the kernel's exports
func (pr *programRun) installHooks(kernel *KernelInput) error {
	byName := map[string][]*Function{}
	for _, fn := range pr.d.Functions {
		if fn.Symbol != nil {
			byName[fn.Symbol.Name] = append(byName[fn.Symbol.Name], fn)
		}
	}
	for _, fn := range pr.d.Functions {
		if fn.Symbol == nil {
			continue
		}
		if kind, ok := programAPIHooks[fn.Symbol.Name]; ok {
			pr.hookAPI(fn, kind)
		}
		if fn.Symbol.Name == programHeaderBuilder {
			pr.hookHeaderBuilder(fn)
		}
	}
	for _, fn := range pr.d.Functions {
		sec := pr.d.sectionOf(fn.EntryVA)
		if sec == nil || !programDeviceSections[sec.Name] {
			continue
		}
		name := ""
		if fn.Symbol != nil {
			if _, api := programAPIHooks[fn.Symbol.Name]; api {
				continue
			}
			name = fn.Symbol.Name
			if len(byName[name]) > 1 {
				name = fmt.Sprintf("%s/%d", name, pr.d.cleanupOf(fn))
			}
		}
		presents := name == "D3DDevice_Swap" || name == "D3DDevice_Present"
		for _, t := range fn.CallTargets {
			if callee := pr.d.Functions[t]; callee != nil && callee.Symbol != nil && strings.Contains(callee.Symbol.Name, programFlip) {
				presents = true
			}
		}
		pr.hookDevice(fn, name, presents)
	}
	ordinals := map[string]uint32{}
	for ordinal, va := range kernel.Exports {
		if name := KernelExportName(ordinal); name != "" {
			ordinals[name] = va
		}
	}
	for name, kind := range programKernelHooks {
		va, ok := ordinals[name]
		if !ok || kind == "" {
			continue
		}
		fn := pr.kd.Functions[va]
		if fn == nil {
			continue
		}
		pr.hookKernel(va, pr.kd.cleanupOf(fn), kind)
	}
	return nil
}

// Answers an XAPI routine, removing the stack bytes its own return removes
func (pr *programRun) hookAPI(fn *Function, kind string) {
	cleanup := pr.d.cleanupOf(fn)
	pr.run.Hooks[fn.EntryVA] = func(r *MachineRun) uint32 {
		pr.answering = fn.Symbol.Name
		switch kind {
		case "alloc":
			a := pr.args(3)
			at := pr.alloc(a[2], programHeapAlign)
			pr.heap = append(pr.heap, programHeapBlock{addr: at, size: a[2], section: pr.section()})
			return pr.ret(cleanup, at)
		case "realloc":
			a := pr.args(4)
			at := pr.alloc(a[3], programHeapAlign)
			for _, b := range pr.heap {
				if b.addr == a[2] {
					r.Preload(at, r.Bytes(b.addr, int(min(b.size, a[3]))))
				}
			}
			pr.heap = append(pr.heap, programHeapBlock{addr: at, size: a[3], section: pr.section()})
			return pr.ret(cleanup, at)
		case "size":
			a := pr.args(3)
			for _, b := range pr.heap {
				if b.addr == a[2] {
					return pr.ret(cleanup, b.size)
				}
			}
			return pr.ret(cleanup, 0)
		case "one":
			return pr.ret(cleanup, 1)
		case "tick":
			r.Advance()
			pr.tick += programTickStep
			return pr.ret(cleanup, pr.tick)
		case "leave":
			pr.left = true
			return ReplayReturn
		}
		return pr.ret(cleanup, 0)
	}
}

// Records the texture header the program has the library fill, then lets the library fill it
func (pr *programRun) hookHeaderBuilder(fn *Function) {
	pr.run.Hooks[fn.EntryVA] = func(r *MachineRun) uint32 {
		pr.answering = fn.Symbol.Name
		a := pr.args(programHeaderPitch + 1)
		if err := pr.headerTexture(a[programHeaderWidth], a[programHeaderHeight], a[programHeaderLevels], a[programHeaderUsage], a[programHeaderFormat], a[programHeaderTexture], a[programHeaderOffset], a[programHeaderPitch]); err != nil {
			r.Failed = err
			return ReplayReturn
		}
		return fn.EntryVA
	}
}

// Answers a kernel export
func (pr *programRun) hookKernel(va uint32, cleanup int, kind string) {
	pr.run.Hooks[va] = func(r *MachineRun) uint32 {
		pr.answering = kind
		switch kind {
		case "load":
			handle := pr.args(1)[0]
			pr.loaded = append(pr.loaded, pr.sectionName(handle))
			pr.model.Sections = appendMissing(pr.model.Sections, pr.sectionName(handle))
			return pr.ret(cleanup, 0)
		case "unload":
			handle := pr.args(1)[0]
			name := pr.sectionName(handle)
			for i := len(pr.loaded) - 1; i >= 0; i-- {
				if pr.loaded[i] == name {
					pr.loaded = append(pr.loaded[:i], pr.loaded[i+1:]...)
					break
				}
			}
			return pr.ret(cleanup, 0)
		case "contiguous":
			size := pr.args(1)[0]
			return pr.ret(cleanup, pr.alloc(size, programPageAlign))
		case "allocated":
			at := pr.args(1)[0]
			size, known := pr.sizes[at]
			if !known {
				r.Failed = fmt.Errorf("xbe: the program asks the size of %08X, which the replay never handed out", at)
				return ReplayReturn
			}
			return pr.ret(cleanup, size)
		case "virtual":
			a := pr.args(5)
			size := r.ReadU32(a[2])
			at := pr.alloc(size, programPageAlign)
			r.WriteU32(a[0], at)
			return pr.ret(cleanup, 0)
		case "counter":
			r.Advance()
			pr.counter += programCounterStep
			r.Regs["EDX"] = uint32(pr.counter >> 32)
			return pr.ret(cleanup, uint32(pr.counter))
		case "bus":
			a := pr.args(4)
			if a[0] != pr.eepromAt || int(a[1])+1 >= len(pr.eeprom) {
				r.Failed = fmt.Errorf("xbe: the program reads bus part %02X at %02X, which the replay has no answer for", a[0], a[1])
				return ReplayReturn
			}
			v := uint32(pr.eeprom[a[1]])
			if a[2] != 0 {
				v |= uint32(pr.eeprom[a[1]+1]) << 8
			}
			r.WriteU32(a[3], v)
			return pr.ret(cleanup, 0)
		case "buswrite":
			a := pr.args(4)
			r.Failed = fmt.Errorf("xbe: the program writes %X to bus part %02X at %02X, which the replay has no part for", a[3], a[0], a[1])
			return ReplayReturn
		case "notfound":
			return pr.ret(cleanup, programNotFound)
		case "leave":
			pr.left = true
			return ReplayReturn
		}
		return pr.ret(cleanup, 0)
	}
}

// The name of the section a header handle names, read out of the header the loader mapped
func (pr *programRun) sectionName(handle uint32) string {
	at := pr.run.ReadU32(handle + programHeaderName)
	var out []byte
	for i := uint32(0); i < 64; i++ {
		b := pr.run.ReadU8(at + i)
		if b == 0 {
			break
		}
		out = append(out, b)
	}
	return string(out)
}

// The section loaded last and still loaded, which a resource created now comes from
func (pr *programRun) section() string {
	if len(pr.loaded) == 0 {
		return ""
	}
	return pr.loaded[len(pr.loaded)-1]
}

func appendMissing(list []string, s string) []string {
	for _, have := range list {
		if have == s {
			return list
		}
	}
	return append(list, s)
}

// Answers a device routine, a present ending the run once the application's run drew on screen, a leaf running
// as it is
func (pr *programRun) hookDevice(fn *Function, name string, presents bool) {
	cleanup := pr.d.cleanupOf(fn)
	va := fn.EntryVA
	pr.run.Hooks[va] = func(r *MachineRun) uint32 {
		pr.answering = name
		if presents {
			pr.frames++
			if pr.running && pr.screen {
				pr.shown = true
				return ReplayReturn
			}
			pr.screen = false
			return pr.ret(cleanup, 0)
		}
		if next, answered := pr.answerDevice(fn, name, cleanup); answered {
			return next
		}
		if pr.d.leafRoutine(fn) {
			delete(r.Hooks, va)
			return va
		}
		return pr.ret(cleanup, 0)
	}
}

// Reports whether a routine calls nothing, never jumps backwards or out of itself and stores nowhere by absolute
// address
func (d *Disassembly) leafRoutine(fn *Function) bool {
	for _, in := range fn.Instructions {
		op := in.Inst.Op
		if op == x86asm.CALL || op == x86asm.IN || op == x86asm.OUT || op == x86asm.INT || op == x86asm.LOOP || op == x86asm.LOOPE || op == x86asm.LOOPNE {
			return false
		}
		if op == x86asm.JMP || isConditionalJump(op) {
			if t := resolveTarget(&in); t == 0 || t <= in.VA || t >= fn.EntryVA+uint32(fn.Size) {
				return false
			}
		}
		if len(in.Inst.Args) == 0 || !storesFirstOperand(in.Inst.Op) {
			continue
		}
		if m, ok := in.Inst.Args[0].(x86asm.Mem); ok && m.Base == 0 && m.Index == 0 {
			return false
		}
	}
	return true
}

// Reports whether an operation writes the memory its first operand names
func storesFirstOperand(op x86asm.Op) bool {
	switch op {
	case x86asm.MOV, x86asm.MOVSX, x86asm.MOVZX, x86asm.ADD, x86asm.SUB, x86asm.AND, x86asm.OR, x86asm.XOR, x86asm.INC, x86asm.DEC, x86asm.NEG, x86asm.NOT,
		x86asm.SHL, x86asm.SHR, x86asm.SAR, x86asm.ROL, x86asm.ROR, x86asm.RCL, x86asm.RCR, x86asm.ADC, x86asm.SBB, x86asm.XCHG, x86asm.XADD, x86asm.CMPXCHG,
		x86asm.FST, x86asm.FSTP, x86asm.FIST, x86asm.FISTP, x86asm.FNSTSW, x86asm.FNSTCW, x86asm.FNSAVE, x86asm.FXSAVE, x86asm.POP,
		x86asm.MOVD, x86asm.MOVQ, x86asm.MOVAPS, x86asm.MOVUPS, x86asm.MOVSS, x86asm.MOVLPS, x86asm.MOVHPS, x86asm.MOVNTPS, x86asm.MOVNTQ,
		x86asm.SETA, x86asm.SETAE, x86asm.SETB, x86asm.SETBE, x86asm.SETE, x86asm.SETG, x86asm.SETGE, x86asm.SETL, x86asm.SETLE, x86asm.SETNE, x86asm.SETNO, x86asm.SETNP, x86asm.SETNS, x86asm.SETO, x86asm.SETP, x86asm.SETS,
		x86asm.BTS, x86asm.BTR, x86asm.BTC, x86asm.SHLD, x86asm.SHRD, x86asm.IMUL, x86asm.MUL, x86asm.DIV, x86asm.IDIV:
		return true
	}
	return false
}

// Answers the D3D routines the replay models, a header binding routine running as it is once recorded
func (pr *programRun) answerDevice(fn *Function, name string, cleanup int) (uint32, bool) {
	r := pr.run
	switch {
	case strings.HasPrefix(name, programConstantPrefix):
		return pr.setConstants(fn, cleanup)
	case name == programStateNotInline || strings.HasPrefix(name, programStatePrefix):
		return pr.setState(fn, name, cleanup)
	case name == programStageNotInline || strings.HasPrefix(name, programStagePrefix):
		return pr.setStage(fn, cleanup)
	}
	switch name {
	case programRegister:
		a := pr.args(2)
		if tex := pr.textures[a[0]]; tex != nil && tex.pending {
			tex.data = a[1] + r.ReadU32(a[0]+programResourceData)
			tex.pending = false
		}
		return fn.EntryVA, true
	case programSetRenderTarget:
		pr.target = pr.args(2)[0]
		return pr.ret(cleanup, 0), true
	case programSetPixelShader:
		pr.pixel = pr.args(1)[0]
		return pr.ret(cleanup, 0), true
	case programClearCall:
		a := pr.args(6)
		clear := pr.frameOp(programKindClear)
		clear.Flags, clear.Color, clear.Z, clear.Stencil = a[2], a[3], a[4], a[5]
		for i := uint32(0); i < a[0]; i++ {
			var rect []uint32
			for w := uint32(0); w < programRectWords; w++ {
				rect = append(rect, r.ReadU32(a[1]+(i*programRectWords+w)*4))
			}
			clear.Rects = append(clear.Rects, rect)
		}
		pr.model.Draws = append(pr.model.Draws, clear)
		return pr.ret(cleanup, 0), true
	case "Direct3DCreate8":
		return pr.ret(cleanup, pr.alloc(programObjectSize, programHeapAlign)), true
	case "Direct3D_CreateDevice":
		a := pr.args(6)
		pr.model.Width = r.ReadU32(a[4] + programPresentWidth)
		pr.model.Height = r.ReadU32(a[4] + programPresentHeight)
		pr.model.BackBuffer = r.ReadU32(a[4] + programPresentFormat)
		pr.model.Flags = r.ReadU32(a[4] + programPresentFlags)
		pr.model.Present = nil
		for off := uint32(0); off < programPresentSize; off += 4 {
			pr.model.Present = append(pr.model.Present, r.ReadU32(a[4]+off))
		}
		r.WriteU32(a[5], pr.alloc(programObjectSize, programHeapAlign))
		return pr.ret(cleanup, 0), true
	case "D3DDevice_GetDisplayMode":
		a := pr.args(1)
		r.WriteU32(a[0]+programModeWidth, pr.model.Width)
		r.WriteU32(a[0]+programModeHeight, pr.model.Height)
		r.WriteU32(a[0]+programModeRefresh, programModeRefreshHz)
		r.WriteU32(a[0]+programModeFlags, pr.model.Flags)
		r.WriteU32(a[0]+programModeFormat, pr.model.BackBuffer)
		return pr.ret(cleanup, 0), true
	case "D3DDevice_GetBackBuffer2", "D3DDevice_GetDepthStencilSurface2", "D3DDevice_GetRenderTarget2":
		tex, err := pr.newTexture(pr.model.Width, pr.model.Height, 1, 0, pr.model.BackBuffer, false, "")
		if err != nil {
			r.Failed = err
			return ReplayReturn, true
		}
		tex.screen = true
		return pr.ret(cleanup, pr.newSurface(tex, 0, 0)), true
	case "D3DDevice_GetBackBuffer", "D3DDevice_GetDepthStencilSurface", "D3DDevice_GetRenderTarget":
		tex, err := pr.newTexture(pr.model.Width, pr.model.Height, 1, 0, pr.model.BackBuffer, false, "")
		if err != nil {
			r.Failed = err
			return ReplayReturn, true
		}
		tex.screen = true
		out := pr.args(cleanup / 4)
		r.WriteU32(out[len(out)-1], pr.newSurface(tex, 0, 0))
		return pr.ret(cleanup, 0), true
	case "D3DDevice_BlockUntilVerticalBlank":
		return pr.ret(cleanup, 0), true
	case "D3DDevice_CreateTexture2":
		a := pr.args(7)
		if a[6] != programRTypeTexture && a[6] != programRTypeCube || a[2] > 1 {
			r.Failed = fmt.Errorf("xbe: the program creates a resource of type %d and depth %d through the texture creator", a[6], a[2])
			return ReplayReturn, true
		}
		tex, err := pr.newTexture(a[0], a[1], a[3], a[4], a[5], a[6] == programRTypeCube, pr.section())
		if err != nil {
			r.Failed = err
			return ReplayReturn, true
		}
		return pr.ret(cleanup, tex.Object), true
	case "D3DDevice_CreateTexture":
		a := pr.args(7)
		tex, err := pr.newTexture(a[0], a[1], a[2], a[3], a[4], false, pr.section())
		if err != nil {
			r.Failed = err
			return ReplayReturn, true
		}
		r.WriteU32(a[6], tex.Object)
		return pr.ret(cleanup, 0), true
	case "D3DDevice_CreateCubeTexture":
		a := pr.args(6)
		tex, err := pr.newTexture(a[0], a[0], a[1], a[2], a[3], true, pr.section())
		if err != nil {
			r.Failed = err
			return ReplayReturn, true
		}
		r.WriteU32(a[5], tex.Object)
		return pr.ret(cleanup, 0), true
	case "D3DDevice_CreateVertexBuffer2", "D3DDevice_CreateIndexBuffer2":
		kind := "vertex"
		if strings.Contains(name, "Index") {
			kind = "index"
		}
		return pr.ret(cleanup, pr.newBuffer(pr.args(1)[0], kind).Object), true
	case "D3DDevice_CreateVertexBuffer", "D3DDevice_CreateIndexBuffer":
		kind := "vertex"
		if strings.Contains(name, "Index") {
			kind = "index"
		}
		a := pr.args(5)
		r.WriteU32(a[4], pr.newBuffer(a[0], kind).Object)
		return pr.ret(cleanup, 0), true
	case "D3DVertexBuffer_Lock2", "D3DIndexBuffer_Lock2":
		buf := pr.buffers[pr.args(1)[0]]
		if buf == nil {
			r.Failed = fmt.Errorf("xbe: the program locks a buffer it never created")
			return ReplayReturn, true
		}
		return pr.ret(cleanup, buf.data), true
	case "D3DVertexBuffer_Lock", "D3DIndexBuffer_Lock":
		a := pr.args(5)
		buf := pr.buffers[a[0]]
		if buf == nil {
			r.Failed = fmt.Errorf("xbe: the program locks a buffer it never created")
			return ReplayReturn, true
		}
		r.WriteU32(a[3], buf.data+a[1])
		return pr.ret(cleanup, 0), true
	case "D3DTexture_LockRect":
		a := pr.args(5)
		return pr.lock(a[0], 0, a[1], a[2], cleanup), true
	case "D3DCubeTexture_LockRect", "D3DDevice::AddRef/24":
		a := pr.args(6)
		return pr.lock(a[0], a[1], a[2], a[3], cleanup), true
	case "D3DSurface_LockRect":
		a := pr.args(4)
		s, ok := pr.surfaces[a[0]]
		if !ok {
			r.Failed = fmt.Errorf("xbe: the program locks a surface it never created")
			return ReplayReturn, true
		}
		return pr.lock(s.tex.Object, uint32(s.face), uint32(s.level), a[1], cleanup), true
	case "D3DTexture_GetLevelDesc", "D3DDevice::AddRef/12":
		a := pr.args(3)
		return pr.describe(a[0], a[1], a[2], cleanup), true
	case "D3DSurface_GetDesc":
		a := pr.args(2)
		s, ok := pr.surfaces[a[0]]
		if !ok {
			r.Failed = fmt.Errorf("xbe: the program describes a surface it never created")
			return ReplayReturn, true
		}
		return pr.describe(s.tex.Object, uint32(s.level), a[1], cleanup), true
	case "D3DTexture_GetSurfaceLevel2":
		a := pr.args(2)
		tex := pr.textures[a[0]]
		if tex == nil {
			r.Failed = fmt.Errorf("xbe: the program takes a level of a texture it never created")
			return ReplayReturn, true
		}
		return pr.ret(cleanup, pr.newSurface(tex, 0, int(a[1]))), true
	case "D3DTexture_GetSurfaceLevel":
		a := pr.args(3)
		tex := pr.textures[a[0]]
		if tex == nil {
			r.Failed = fmt.Errorf("xbe: the program takes a level of a texture it never created")
			return ReplayReturn, true
		}
		r.WriteU32(a[2], pr.newSurface(tex, 0, int(a[1])))
		return pr.ret(cleanup, 0), true
	case "D3DCubeTexture_GetCubeMapSurface2":
		a := pr.args(3)
		tex := pr.textures[a[0]]
		if tex == nil {
			r.Failed = fmt.Errorf("xbe: the program takes a face of a texture it never created")
			return ReplayReturn, true
		}
		return pr.ret(cleanup, pr.newSurface(tex, int(a[1]), int(a[2]))), true
	case "D3DResource_AddRef":
		return pr.ret(cleanup, 2), true
	case "D3DResource_Release", "D3DDevice::AddRef/0":
		return pr.ret(cleanup, 1), true
	case "D3D_AllocContiguousMemory":
		a := pr.args(2)
		align := a[1]
		if align < programHeapAlign {
			align = programHeapAlign
		}
		return pr.ret(cleanup, pr.alloc(a[0], align)), true
	case "D3DDevice_CreateVertexShader":
		a := pr.args(4)
		decl, err := pr.d.Image.DecodeVertexDeclaration(a[0])
		if err != nil {
			decl, err = pr.readDeclaration(a[0])
			if err != nil {
				r.Failed = err
				return ReplayReturn, true
			}
		}
		shader := &ProgramShader{Handle: pr.nextHandle, Declaration: decl}
		if a[1] != 0 {
			program, err := pr.readProgram(a[1])
			if err != nil {
				r.Failed = err
				return ReplayReturn, true
			}
			shader.Program = program
		}
		pr.nextHandle += 4
		pr.model.Shaders = append(pr.model.Shaders, shader)
		r.WriteU32(a[2], shader.Handle)
		return pr.ret(cleanup, 0), true
	case programCreatePixelShader:
		a := pr.args(2)
		n, err := pr.states.definitionBytes(fn)
		if err != nil {
			r.Failed = err
			return ReplayReturn, true
		}
		shader := &ProgramPixelShader{Handle: pr.nextHandle}
		for off := 0; off < n; off += 4 {
			shader.Def = append(shader.Def, r.ReadU32(a[0]+uint32(off)))
		}
		pr.model.Pixel = append(pr.model.Pixel, shader)
		r.WriteU32(a[1], shader.Handle)
		pr.nextHandle += 4
		return pr.ret(cleanup, 0), true
	case "D3DDevice_SetVertexShader":
		pr.shader = pr.args(1)[0]
		return pr.ret(cleanup, 0), true
	case "D3DDevice_SetStreamSource":
		a := pr.args(3)
		pr.stream, pr.stride = a[1], a[2]
		return pr.ret(cleanup, 0), true
	case "D3DDevice_SetIndices":
		pr.indices = pr.args(2)[0]
		return pr.ret(cleanup, 0), true
	case programSetTexture:
		a := pr.args(2)
		if a[0] < uint32(len(pr.bound)) {
			pr.bound[a[0]] = a[1]
		}
		return pr.ret(cleanup, 0), true
	case "D3DDevice_DrawVertices":
		a := pr.args(3)
		draw := pr.frameOp(programKindDraw)
		draw.Primitive, draw.Start, draw.Count = a[0], a[1], a[2]
		pr.model.Draws = append(pr.model.Draws, draw)
		return pr.ret(cleanup, 0), true
	case "D3DDevice_DrawIndexedVertices":
		a := pr.args(3)
		draw := pr.frameOp(programKindDraw)
		draw.Primitive, draw.Count = a[0], a[1]
		for i := uint32(0); i < a[1]; i++ {
			draw.Indices = append(draw.Indices, uint16(r.load(a[2]+2*i, 2)))
		}
		pr.model.Draws = append(pr.model.Draws, draw)
		return pr.ret(cleanup, 0), true
	}
	return 0, false
}

// Creates a texture object with a resource header the library's own readers decode and storage for every face and
// level, levels given as zero filling the chain down to one pixel
func (pr *programRun) newTexture(width, height, levels, usage, format uint32, cube bool, section string) (*ProgramTexture, error) {
	tex := &ProgramTexture{Launcher: pr.launcher, Section: section, Width: int(width), Height: int(height), Format: format, Cube: cube, Usage: usage}
	var err error
	if tex.levels, err = textureLevels(width, height, levels, format); err != nil {
		return nil, err
	}
	levels = uint32(len(tex.levels))
	tex.Levels = int(levels)
	off := tex.levels[len(tex.levels)-1].offset + tex.levels[len(tex.levels)-1].bytes
	tex.face = off
	faces := 1
	if cube {
		faces = programCubeFaces
	}
	tex.data = pr.alloc(uint32(off*faces), programPageAlign)
	tex.Object = pr.alloc(programObjectSize, programHeapAlign)
	r := pr.run
	r.WriteU32(tex.Object, 1|programTypeTexture<<programTypeShift)
	r.WriteU32(tex.Object+programResourceData, tex.data)
	r.WriteU32(tex.Object+8, 0)
	reg := programFormatDim2D | format<<programFormatShift | levels<<programFormatMips | uint32(bits.Len32(width)-1)<<programFormatLog2U | uint32(bits.Len32(height)-1)<<programFormatLog2V
	if cube {
		reg |= programFormatCube
	}
	r.WriteU32(tex.Object+12, reg)
	r.WriteU32(tex.Object+16, 0)
	pr.textures[tex.Object] = tex
	pr.model.Textures = append(pr.model.Textures, tex)
	return tex, nil
}

// A draw or clear with everything bound, its target the surface set, none or the back buffer being the screen
func (pr *programRun) frameOp(kind string) ProgramDraw {
	op := ProgramDraw{Launcher: pr.launcher, Frame: pr.frames, Kind: kind, Shader: pr.shader, PixelShader: pr.pixel, Stream: pr.stream, Stride: pr.stride, Textures: append([]uint32(nil), pr.bound...), Constants: pr.constantSnapshot()}
	op.States, op.TextureStates = pr.stateSnapshot()
	s, bound := pr.surfaces[pr.target]
	if bound {
		op.Target, op.Face, op.Level = s.tex.Object, s.face, s.level
	}
	if !bound || s.tex.screen {
		pr.screen = true
	}
	return op
}

// The size and place of each texture level, zero levels filling the chain down to one pixel
func textureLevels(width, height, levels, format uint32) ([]programLevel, error) {
	if levels == 0 {
		levels = uint32(bits.Len32(max(width, height)))
	}
	pf := texture.PixelFormat(format)
	if pf.BitsPerPixel() == 0 {
		return nil, fmt.Errorf("xbe: the program creates a %dx%d texture of format %02X, whose layout is unknown", width, height, format)
	}
	var out []programLevel
	off := 0
	w, h := int(width), int(height)
	for l := 0; l < int(levels); l++ {
		n := pf.DataSize(w, h)
		out = append(out, programLevel{width: w, height: h, bytes: n, offset: off})
		off += n
		w, h = max(w/2, 1), max(h/2, 1)
	}
	return out, nil
}

// Records a program kept texture header, a pitch laying one level out in rows, its data registered later
func (pr *programRun) headerTexture(width, height, levels, usage, format, object, offset, pitch uint32) error {
	tex := &ProgramTexture{Object: object, Launcher: pr.launcher, Section: pr.section(), Width: int(width), Height: int(height), Levels: int(levels), Format: format, Usage: usage, pending: true}
	if pitch != 0 {
		if levels != 1 {
			return fmt.Errorf("xbe: the program lays a %d level texture out with a pitch of %d bytes, which the replay cannot place", levels, pitch)
		}
		tex.levels = []programLevel{{width: int(width), height: int(height), bytes: int(pitch * height)}}
	} else {
		var err error
		if tex.levels, err = textureLevels(width, height, levels, format); err != nil {
			return err
		}
		tex.Levels = len(tex.levels)
	}
	last := tex.levels[len(tex.levels)-1]
	tex.face = last.offset + last.bytes
	tex.data = offset
	pr.textures[object] = tex
	pr.model.Textures = append(pr.model.Textures, tex)
	return nil
}

// Creates a vertex or index buffer object with its storage
func (pr *programRun) newBuffer(size uint32, kind string) *ProgramBuffer {
	buf := &ProgramBuffer{Launcher: pr.launcher, Section: pr.section(), Kind: kind, Size: size}
	buf.data = pr.alloc(size, programPageAlign)
	buf.Object = pr.alloc(programObjectSize, programHeapAlign)
	typ := uint32(programTypeVertexBuffer)
	if kind == "index" {
		typ = programTypeIndexBuffer
	}
	pr.run.WriteU32(buf.Object, 1|typ<<programTypeShift)
	pr.run.WriteU32(buf.Object+programResourceData, buf.data)
	pr.buffers[buf.Object] = buf
	pr.model.Buffers = append(pr.model.Buffers, buf)
	return buf
}

// Creates a surface object aliasing a level of a face of a texture
func (pr *programRun) newSurface(tex *ProgramTexture, face, level int) uint32 {
	at := pr.alloc(programObjectSize, programHeapAlign)
	pr.run.WriteU32(at, 1|programTypeSurface<<programTypeShift)
	if level < len(tex.levels) {
		pr.run.WriteU32(at+programResourceData, tex.data+uint32(face*tex.face+tex.levels[level].offset))
	}
	pr.surfaces[at] = programSurface{tex: tex, face: face, level: level}
	return at
}

// Answers a lock with the pitch and the pixels of a level of a face
func (pr *programRun) lock(object, face, level, rect uint32, cleanup int) uint32 {
	tex := pr.textures[object]
	if tex == nil {
		pr.run.Failed = fmt.Errorf("xbe: %s locks %08X, holding %08X %08X %08X %08X, which the replay never created (last %X)", pr.answering, object, pr.run.ReadU32(object), pr.run.ReadU32(object+4), pr.run.ReadU32(object+8), pr.run.ReadU32(object+12), pr.run.Trail)
		return ReplayReturn
	}
	if int(level) >= len(tex.levels) || int(face) >= programCubeFaces {
		pr.run.Failed = fmt.Errorf("xbe: %s locks level %d of face %d of %08X, which has %d levels (last %X)", pr.answering, level, face, object, len(tex.levels), pr.run.Trail)
		return ReplayReturn
	}
	if tex.pending {
		pr.run.Failed = fmt.Errorf("xbe: %s locks %08X, a header the program never registered over memory (last %X)", pr.answering, object, pr.run.Trail)
		return ReplayReturn
	}
	lv := tex.levels[level]
	pitch := lv.bytes / max(lv.height, 1)
	if texture.PixelFormat(tex.Format).IsCompressed() {
		pitch = lv.bytes / max((lv.height+3)/4, 1)
	}
	pr.run.WriteU32(rect+programRectPitch, uint32(pitch))
	pr.run.WriteU32(rect+programRectBits, tex.data+uint32(int(face)*tex.face+lv.offset))
	return pr.ret(cleanup, 0)
}

// Answers a level description
func (pr *programRun) describe(object, level, desc uint32, cleanup int) uint32 {
	tex := pr.textures[object]
	if tex == nil {
		pr.run.Failed = fmt.Errorf("xbe: %s describes %08X, holding %08X %08X %08X %08X, which the replay never created (last %X)", pr.answering, object, pr.run.ReadU32(object), pr.run.ReadU32(object+4), pr.run.ReadU32(object+8), pr.run.ReadU32(object+12), pr.run.Trail)
		return ReplayReturn
	}
	if int(level) >= len(tex.levels) {
		pr.run.Failed = fmt.Errorf("xbe: %s describes level %d of %08X, which has %d levels (last %X)", pr.answering, level, object, len(tex.levels), pr.run.Trail)
		return ReplayReturn
	}
	lv := tex.levels[level]
	r := pr.run
	r.WriteU32(desc+surfaceDescFormat, tex.Format)
	r.WriteU32(desc+programDescType, programTypeTexture)
	r.WriteU32(desc+programDescUsage, tex.Usage)
	r.WriteU32(desc+programDescSize, uint32(lv.bytes))
	r.WriteU32(desc+surfaceDescWidth, uint32(lv.width))
	r.WriteU32(desc+surfaceDescHeight, uint32(lv.height))
	return pr.ret(cleanup, 0)
}

// Decodes a vertex declaration the program built in memory rather than in its image
func (pr *programRun) readDeclaration(va uint32) (*VertexDeclaration, error) {
	var tokens []byte
	for i := uint32(0); i < 64; i++ {
		w := pr.run.ReadU32(va + 4*i)
		tokens = binary.LittleEndian.AppendUint32(tokens, w)
		if w == programDeclEnd {
			img := NewImage(va, 0, nil, []Section{{Name: "decl", VirtualAddr: va, VirtualSize: uint32(len(tokens)), RawSize: uint32(len(tokens)), Flags: 0x1, Data: tokens}})
			return img.DecodeVertexDeclaration(va)
		}
	}
	return nil, fmt.Errorf("xbe: the declaration at %08X has no end token", va)
}

// Decodes a vertex program from the run's memory
func (pr *programRun) readProgram(va uint32) (*VertexProgram, error) {
	if program, err := pr.d.Image.DecodeVertexProgram(va); err == nil {
		return program, nil
	}
	header := pr.run.Bytes(va, 8)
	count := int(binary.LittleEndian.Uint16(header[2:]))
	size := 8 + 16*count
	data := pr.run.Bytes(va, size)
	img := NewImage(va, 0, nil, []Section{{Name: "vsh", VirtualAddr: va, VirtualSize: uint32(size), RawSize: uint32(size), Flags: 0x1, Data: data}})
	return img.DecodeVertexProgram(va)
}

// Reads the bytes every resource holds after the run and the heap blocks filled out of a loaded section
func (pr *programRun) collect() {
	r := pr.run
	for _, tex := range pr.model.Textures {
		if tex.Launcher != pr.launcher {
			continue
		}
		faces := 1
		if tex.Cube {
			faces = programCubeFaces
		}
		for f := 0; f < faces; f++ {
			tex.Faces = append(tex.Faces, r.Bytes(tex.data+uint32(f*tex.face), tex.face))
		}
	}
	for _, buf := range pr.model.Buffers {
		if buf.Launcher == pr.launcher {
			buf.Data = r.Bytes(buf.data, int(buf.Size))
		}
	}
	for _, b := range pr.heap {
		if b.section == "" || b.size < programHeapAlign {
			continue
		}
		sec := pr.d.Image.FindSection(b.section)
		if sec == nil {
			continue
		}
		data := r.Bytes(b.addr, int(b.size))
		if off := bytes.Index(sec.Data, data); off >= 0 {
			pr.model.Tables = append(pr.model.Tables, &ProgramTable{Address: b.addr, Launcher: pr.launcher, Section: b.section, Offset: off, Size: int(b.size), Data: data})
		}
	}
	sort.Slice(pr.model.Tables, func(i, j int) bool { return pr.model.Tables[i].Address < pr.model.Tables[j].Address })
}
