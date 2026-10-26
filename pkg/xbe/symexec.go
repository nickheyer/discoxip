package xbe

import (
	"fmt"
	"maps"
	"math"
	"os"
	"slices"
	"sort"
	"strings"

	"golang.org/x/arch/x86/x86asm"
)

func mathFloat32bits(f float32) uint32 { return math.Float32bits(f) }

// Effect is one observable effect recorded along an execution path.
type Effect struct {
	Kind    string      `json:"kind"` // store or call
	VA      uint32      `json:"va"`
	Base    string      `json:"base,omitempty"`
	Addr    Expr        `json:"-"` // symbolic address of a store through a pointer the path computed
	Off     int32       `json:"off,omitempty"`
	Size    int         `json:"size,omitempty"`
	Value   Expr        `json:"-"`
	Fn      string      `json:"fn,omitempty"`
	Args    []Expr      `json:"-"`
	This    Expr        `json:"-"` // ecx at the call for helpers of unknown convention
	Seq     int         `json:"seq,omitempty"`
	Target  uint32      `json:"target,omitempty"` // the routine a named call reaches, where the image carries it
	Buffers []ArgBuffer `json:"-"`
}

// Buffer index of the receiver register, which carries a stack buffer in the optimizer's own convention
const thisBufferIndex = -1

// ArgBuffer snapshots stack memory a call argument points at, each element with the size it was stored as.
type ArgBuffer struct {
	Index int
	Elems []Expr
	Sizes []int
}

// Stored elements a stack buffer snapshot may hold, the path buffers being the longest
const stackBufferElems = 1024

// Path is one complete execution of a function under a set of guards.
type Path struct {
	Guards   []Expr
	GuardPos []int // effects recorded before each guard was taken
	Effects  []Effect
	Return   Expr
}

// RetAddr marks a return address pushed by an inlined call.
type RetAddr struct {
	VA    uint32
	Clean int32
}

func (RetAddr) exprTag() {}

type memKey struct {
	base string
	off  int32
}

type memVal struct {
	size  int
	v     Expr
	frame bool   // a saved register or return address, which no library routine writes over
	va    uint32 // the instruction that stored it
}

type flagState struct {
	kind string // cmp test alu fsw none
	a, b Expr
	r    Expr
	cf   Expr // the carry out where the operation leaves one the kind alone does not give
	fsw  *fcmpRec
}

// The carry the last flag setting operation left
func carryOf(fl flagState) (Expr, bool) {
	if fl.cf != nil {
		return fl.cf, true
	}
	switch fl.kind {
	case "cmp", "sub":
		return mkCond("ult", fl.a, fl.b), true
	case "add":
		return mkCond("ult", fl.r, fl.a), true
	case "and", "or", "xor", "test":
		return IConst{0}, true
	}
	return nil, false
}

type fcmpRec struct{ a, b Expr }

// Sign extends the operands and result of a narrow operation so the signed and unsigned conditions read them right
func narrowFlags(fl flagState, size int) flagState {
	if size >= 4 {
		return fl
	}
	sx := map[int]string{1: "sx8", 2: "sx16"}[size]
	if fl.a != nil {
		fl.a = mkUn(sx, fl.a)
	}
	if fl.b != nil {
		fl.b = mkUn(sx, fl.b)
	}
	if fl.r != nil {
		fl.r = mkUn(sx, fl.r)
	}
	return fl
}

// ExecError reports where symbolic execution gave up.
type ExecError struct {
	VA      uint32
	Insn    string
	Frames  []uint32
	Limit   bool
	Err     error
	Effects []Effect // effects recorded on the failing path
}

func (e *ExecError) Error() string {
	return fmt.Sprintf("symexec: %08X %s: %v (frames %X)", e.VA, e.Insn, e.Err, e.Frames)
}

var debugTrace = os.Getenv("DISCOXIP_TRACE") != ""

var debugNoVResolve = os.Getenv("DISCOXIP_NO_VRESOLVE") != ""

var debugNoKeep = os.Getenv("DISCOXIP_NO_KEEP") != ""

// Execution limits that stop runaway analysis
const (
	maxSteps      = 60000
	maxPaths      = 512
	maxInlineSize = 4000
	maxFrameSteps = 2500
	maxLoopUnroll = 4
	maxRepUnroll  = 1 << 16
	maxRepCompare = 1 << 16
)

type execState struct {
	regs       [8]Expr
	xmm        [8][4]Expr
	mm         [8][2]Expr
	mem        map[memKey]memVal
	fpu        []Expr
	fl         flagState
	fcmp       *fcmpRec
	axFSW      bool
	va         uint32
	guards     []Expr
	guardPos   []int
	effects    []Effect
	steps      int
	stack      []RetAddr
	root       uint32    // entry of the function the path started in
	frames     []uint32  // entry addresses of inlined callees
	fsteps     []int     // instructions executed inside each inlined frame
	entry      [][8]Expr // register values at entry of the outer function and each inlined frame
	pushes     int       // argument pushes since the last call boundary
	rest       int32     // stack offset above the outstanding argument words, where the stack settles between calls
	lastSave   bool      // the instruction just run pushed a register to save it
	pending    *pendingCall
	bound      map[string]Expr // atomic symbolic values fixed by earlier guards
	boundLoads map[string]Load // memory reads behind those values, freed when the memory changes
	loops      map[uint32]int  // backward symbolic branches taken per jump on this path
	nulled     bool            // a pointer a guard fixed to zero was dereferenced, which the hardware cannot do
}

// pendingCall remembers the arguments popped for a call of unknown arity so a later
// call can still reach the ones that were pushed for it in advance.
type pendingCall struct {
	shadow []Expr // candidate arguments, nearest the call first
}

// Executor runs x86 code symbolically over the disassembled image.
type Executor struct {
	d            *Disassembly
	seq          int
	paths        []Path
	vtables      map[uint32]uint32 // global object pointer VA to vtable VA
	symbols      map[uint32]*Symbol
	nameOf       map[uint32]string
	Opaque       map[uint32]bool // functions never inlined
	ftol         map[uint32]bool
	probe        map[uint32]bool // stack probes that reserve the frame their caller sizes in eax
	errCount     int
	dropped      int                     // paths left unsummarized because they reach through a null pointer
	allocHook    func() Expr             // fresh object pointer for the allocator when set
	written      map[uint32]bool         // global bytes some instruction stores to directly
	Results      map[uint32]Expr         // fixed return values for opaque functions
	Allocators   map[uint32]bool         // functions whose results are fresh objects
	savePush     map[uint32]bool         // push instructions that save callee owned registers
	frameAlloc   map[uint32]bool         // prologue subtractions reserving the locals of a frame
	frameSizes   map[uint32]map[int]bool // bytes each function's frame reservations take, by function
	StepLimit    int                     // per path instruction budget, zero means the default
	FrameLimit   int                     // per inlined frame budget, negative disables the budget
	PathLimit    int                     // paths one run may enumerate, zero means the default
	staticThis   map[uint32]uint32       // functions invoked on a static object, by address
	thisLoc      map[uint32]argLocation  // where each such function takes its object
	callTarget   uint32                  // library function the opaque call being recorded resolves to
	callReceiver bool                    // the opaque call being recorded takes its receiver in ecx
	callee       uint32                  // the routine the named call being recorded reaches
	// ImageCreations models the creations the runtime runs from the image as handing back their object
	ImageCreations bool
	arrayIdx       map[uint32]arrayIndex // counted array indexes the optimizer inlined, by their first instruction
	Stop           map[uint32]bool       // addresses a path ends at, for a routine the runtime enters and leaves mid function
	block          *blockRun             // the basic block being lifted, nil while paths are summarized
	loopCut        bool                  // a loop left after its unroll budget, so the paths miss its longer runs
	pointer        Expr                  // the function pointer the call being recorded goes through
	tail           bool                  // the call being recorded is a jump leaving the return address under the arguments
	tailClean      uint32                // stack bytes the callee of a tail jump removes, which its return then applies
}

// NewExecutor prepares an executor with symbol and vtable knowledge.
func NewExecutor(d *Disassembly) *Executor {
	ex := &Executor{d: d, vtables: map[uint32]uint32{}, symbols: map[uint32]*Symbol{}, nameOf: map[uint32]string{}, Opaque: map[uint32]bool{}, ftol: map[uint32]bool{}, probe: map[uint32]bool{}, Results: map[uint32]Expr{}, Allocators: map[uint32]bool{}}
	for va, fn := range d.Functions {
		if fn.Symbol != nil {
			ex.symbols[va] = fn.Symbol
		}
		if fn.Name != "" {
			ex.nameOf[va] = fn.Name
		}
		if isFtol(fn) {
			ex.ftol[va] = true
		}
		if isStackProbe(fn) {
			ex.probe[va] = true
		}
	}
	stage("resolve global objects")
	ex.resolveGlobalObjects()
	stage("scan written globals")
	ex.scanWrittenGlobals()
	ex.scanPrologues()
	ex.scanFrameAllocations()
	ex.scanArrayIndexes()
	stage("executor ready")
	return ex
}

// Records every counted array index the optimizer inlined, so each runs as the accessor call it expands
func (ex *Executor) scanArrayIndexes() {
	ex.arrayIdx = map[uint32]arrayIndex{}
	for va, fn := range ex.d.Functions {
		if fn.Symbol != nil || ex.d.inLibrary(va) {
			continue
		}
		for start, a := range inlinedArrayIndexes(fn) {
			ex.arrayIdx[start] = a
		}
	}
}

// Runs an inlined index as the element accessor call, leaving the element in the register the load reads into
func (ex *Executor) arrayElement(s *execState, in *Instruction, a arrayIndex) {
	ptr := asPtr(ex.addr(s, x86asm.Mem{Base: a.base, Disp: int64(a.off)}))
	index := s.getReg(a.index)
	ex.seq++
	res := CallResult{Fn: arrayElementCall, Args: []Expr{index}, Seq: ex.seq, This: ptr}
	s.effects = append(s.effects, Effect{Kind: "call", VA: in.VA, Fn: arrayElementCall, Args: []Expr{index}, Seq: ex.seq, This: ptr})
	s.setReg(a.temp, Sym{fmt.Sprintf("clobber.%s#%d", a.temp, ex.seq)})
	s.setReg(a.dest, res)
	s.va = a.exit
}

// Runs an expanded reference count helper as the call it was expanded from, after the stores scheduled into it
func (ex *Executor) resourceCount(s *execState, in *Instruction, r resourceRef) error {
	for _, va := range r.carry {
		carried, ok := ex.d.InsnByVA[va]
		if !ok {
			return fmt.Errorf("instruction %08X scheduled into the reference count is gone", va)
		}
		if _, err := ex.step(s, carried, va+uint32(carried.Inst.Len)); err != nil {
			return err
		}
	}
	obj := s.getReg(r.base)
	ex.seq++
	args := []Expr{obj}
	s.effects = append(s.effects, Effect{Kind: "call", VA: in.VA, Fn: r.fn, Args: args, Seq: ex.seq})
	for _, w := range r.regs {
		s.setReg(w, Sym{fmt.Sprintf("clobber.%s#%d", w, ex.seq)})
	}
	s.setReg(x86asm.EAX, CallResult{Fn: r.fn, Args: args, Seq: ex.seq})
	s.va = r.exit
	return nil
}

// Records the register saving pushes and local allocations in every function prologue, reading each only as far
// as the code it owns
func (ex *Executor) scanPrologues() {
	ex.savePush = map[uint32]bool{}
	ex.frameAlloc = map[uint32]bool{}
	ex.frameSizes = map[uint32]map[int]bool{}
	owner := map[uint32]uint32{}
	for _, va := range ex.d.functionVAs() {
		for i := range ex.d.Functions[va].Instructions {
			in := &ex.d.Functions[va].Instructions[i]
			if _, taken := owner[in.VA]; !taken {
				owner[in.VA] = va
			}
		}
	}
	for _, va := range ex.d.functionVAs() {
		framed := false
		for cur := va; owner[cur] == va; {
			in := ex.d.InsnByVA[cur]
			if in == nil {
				break
			}
			switch in.Inst.Op {
			case x86asm.PUSH:
				// A push of ecx under a fresh frame pointer reserves a local, the compiler's short form of the allocation
				if r, ok := in.Inst.Args[0].(x86asm.Reg); ok && (r == x86asm.EBP || r == x86asm.EBX || r == x86asm.ESI || r == x86asm.EDI || (r == x86asm.ECX && framed)) {
					ex.savePush[cur] = true
					cur += uint32(in.Inst.Len)
					continue
				}
			case x86asm.MOV, x86asm.LEA, x86asm.SUB, x86asm.AND, x86asm.XOR, x86asm.FLD, x86asm.FLD1, x86asm.FLDZ, x86asm.FSTP:
				if dst, src, ok := registerMove(in); ok && dst == x86asm.EBP && src == x86asm.ESP {
					framed = true
				}
				if r, ok := in.Inst.Args[0].(x86asm.Reg); ok && r == x86asm.ESP && (in.Inst.Op == x86asm.SUB || in.Inst.Op == x86asm.AND) {
					ex.frameAlloc[cur] = true
					if imm, isImm := in.Inst.Args[1].(x86asm.Imm); isImm && in.Inst.Op == x86asm.SUB {
						ex.noteFrameSize(va, int(imm))
					}
				}
				cur += uint32(in.Inst.Len)
				continue
			}
			break
		}
	}
}

// Instructions a frame allocation is followed by before the call that would consume it as an argument area
const allocScanLimit = 40

// Marks every stack pointer subtraction that reserves locals rather than an argument area, an argument area being
// one the next call consumes exactly with the pushes made after it
func (ex *Executor) scanFrameAllocations() {
	for _, va := range ex.d.functionVAs() {
		fn := ex.d.Functions[va]
		for i := range fn.Instructions {
			in := &fn.Instructions[i]
			if in.Inst.Op != x86asm.SUB || argCount(in) != 2 {
				continue
			}
			r, isReg := in.Inst.Args[0].(x86asm.Reg)
			imm, isImm := in.Inst.Args[1].(x86asm.Imm)
			if !isReg || r != x86asm.ESP || !isImm || ex.frameAlloc[in.VA] {
				continue
			}
			if !ex.consumedByNextCall(fn, i, int(imm)/4) {
				ex.frameAlloc[in.VA] = true
				ex.noteFrameSize(va, int(imm))
			}
		}
	}
}

// Records bytes a function reserves for its frame, which its epilogue releases in one addition
func (ex *Executor) noteFrameSize(fn uint32, bytes int) {
	if ex.frameSizes[fn] == nil {
		ex.frameSizes[fn] = map[int]bool{}
	}
	ex.frameSizes[fn][bytes] = true
}

// Reports whether the straight line code after a stack reservation reaches a call whose arity is exactly the
// reserved words plus the pushes made on the way
func (ex *Executor) consumedByNextCall(fn *Function, i int, words int) bool {
	pushes := 0
	for j := i + 1; j < len(fn.Instructions) && j <= i+allocScanLimit; j++ {
		in := &fn.Instructions[j]
		switch {
		case in.Inst.Op == x86asm.PUSH:
			pushes++
		case in.Inst.Op == x86asm.CALL:
			arity := ex.d.callArity(resolveTarget(in))
			return arity >= 0 && arity == words+pushes
		case isJump(in.Inst.Op) || in.Inst.Op == x86asm.RET || in.Inst.Op == x86asm.POP:
			return false
		}
	}
	return false
}

// Argument bytes the caller removes after a call, read off the add or pops that follow it past instructions
// leaving the stack pointer alone, -1 where none do, the release of the function's own frame not counting
func (ex *Executor) cleanupAfter(fn *Function, callVA uint32) int {
	index := fn.instructionIndex()
	i, ok := index[callVA]
	if !ok {
		return -1
	}
	bytes := 0
	found := func() int {
		if bytes > 0 {
			return bytes
		}
		return -1
	}
	for j := i + 1; j < len(fn.Instructions); j++ {
		in := &fn.Instructions[j]
		switch {
		case in.Inst.Op == x86asm.ADD && argCount(in) == 2 && isStackPointer(in.Inst.Args[0]):
			if imm, isImm := in.Inst.Args[1].(x86asm.Imm); isImm && !ex.frameSizes[fn.EntryVA][int(imm)] {
				return bytes + int(imm)
			}
			return found()
		case in.Inst.Op == x86asm.POP:
			if r, isReg := in.Inst.Args[0].(x86asm.Reg); isReg && (r == x86asm.ECX || r == x86asm.EDX || r == x86asm.EAX) {
				bytes += 4
				continue
			}
			return found()
		case leavesStackPointer(in):
			continue
		}
		return found()
	}
	return found()
}

// Reports whether an operand is the stack pointer register
func isStackPointer(a x86asm.Arg) bool {
	r, ok := a.(x86asm.Reg)
	return ok && r == x86asm.ESP
}

// Reports whether an instruction neither moves the stack pointer nor leaves the straight line code
func leavesStackPointer(in *Instruction) bool {
	switch in.Inst.Op {
	case x86asm.PUSH, x86asm.POP, x86asm.CALL, x86asm.RET, x86asm.LEAVE, x86asm.ENTER:
		return false
	}
	if isJump(in.Inst.Op) {
		return false
	}
	return argCount(in) == 0 || !isStackPointer(in.Inst.Args[0])
}

// Moves the rest level with the stack pointer: a saved register, a reserved frame or a finished call settle it
// where the stack stands, and it never stays below the stack pointer
func (ex *Executor) settleRest(s *execState, in *Instruction) {
	p, ok := s.regs[4].(Ptr)
	if !ok || p.Base != "stack" {
		return
	}
	switch {
	case in.Inst.Op == x86asm.PUSH && s.lastSave:
		s.rest = p.Off
	case (in.Inst.Op == x86asm.SUB || in.Inst.Op == x86asm.AND) && ex.frameAlloc[in.VA]:
		s.rest = p.Off
	case in.Inst.Op == x86asm.CALL && len(s.frames) == 0:
		s.rest = p.Off
	}
	if p.Off > s.rest {
		s.rest = p.Off
	}
}

// Bytes of argument words standing between the stack pointer and the rest level
func (s *execState) outstanding() uint32 {
	p, ok := s.regs[4].(Ptr)
	if !ok || p.Base != "stack" || s.rest <= p.Off {
		return 0
	}
	return uint32(s.rest - p.Off)
}

// Marks every global byte that any instruction stores to through an absolute address
func (ex *Executor) scanWrittenGlobals() {
	ex.written = map[uint32]bool{}
	for _, insn := range ex.d.InsnByVA {
		if argCount(insn) == 0 {
			continue
		}
		m, ok := insn.Inst.Args[0].(x86asm.Mem)
		if !ok || m.Base != 0 || m.Index != 0 {
			continue
		}
		switch insn.Inst.Op {
		case x86asm.CMP, x86asm.TEST, x86asm.PUSH, x86asm.FLD, x86asm.FILD, x86asm.FCOM, x86asm.FCOMP, x86asm.FICOM, x86asm.FICOMP,
			x86asm.FADD, x86asm.FSUB, x86asm.FSUBR, x86asm.FMUL, x86asm.FDIV, x86asm.FDIVR, x86asm.FIADD, x86asm.FISUB, x86asm.FISUBR, x86asm.FIMUL, x86asm.FIDIV, x86asm.FIDIVR,
			x86asm.JMP, x86asm.CALL, x86asm.LEA:
			continue
		}
		size := insn.Inst.MemBytes
		if size == 0 {
			size = 4
		}
		for b := 0; b < size; b++ {
			ex.written[uint32(m.Disp)+uint32(b)] = true
		}
	}
}

// Reports whether a global range is initialized image data that no instruction stores to
func (ex *Executor) isConstantGlobal(va uint32, size int) bool {
	sec := ex.d.sectionOf(va)
	if sec == nil || va+uint32(size) > sec.VirtualAddr+sec.RawSize {
		return false
	}
	if sec.Flags&0x1 == 0 {
		return true
	}
	for b := uint32(0); b < uint32(size); b++ {
		if ex.written[va+b] {
			return false
		}
	}
	return true
}

// Recognizes the CRT float to int helper by its fistp into a stack slot returned in eax
func isFtol(fn *Function) bool {
	sawFist := false
	for _, in := range fn.Instructions {
		switch in.Inst.Op {
		case x86asm.FISTP, x86asm.FIST:
			sawFist = true
		case x86asm.CALL:
			return false
		}
	}
	if !sawFist || len(fn.Instructions) > 80 {
		return false
	}
	for _, in := range fn.Instructions {
		if in.Inst.Op == x86asm.RET {
			return true
		}
	}
	return false
}

// Finds functions invoked with a static object address in a register they read, keeping that register
func (ex *Executor) staticThisCallers() map[uint32]uint32 {
	if ex.staticThis != nil {
		return ex.staticThis
	}
	ex.staticThis = map[uint32]uint32{}
	ex.thisLoc = map[uint32]argLocation{}
	code := ex.d.sectionOf(ex.d.Image.EntryPoint)
	// An optimized build hands the object on in a register, so each round reveals the callees of the last
	for round, found := 0, true; found && round < staticThisRounds; round++ {
		found = ex.scanStaticThis(code)
	}
	return ex.staticThis
}

// Rounds of propagation through routines that pass their object on in the register they received it in
const staticThisRounds = 8

// Records the static object each routine runs on, reporting whether this round learned anything new
func (ex *Executor) scanStaticThis(code *Section) bool {
	found := false
	record := func(t, addr uint32, loc argLocation) {
		if a, seen := ex.staticThis[t]; !seen || a != addr || ex.thisLoc[t] != loc {
			found = true
		}
		ex.staticThis[t], ex.thisLoc[t] = addr, loc
	}
	for _, va := range ex.d.functionVAs() {
		fn := ex.d.Functions[va]
		seed := map[x86asm.Reg]uint32{}
		if addr, ok := ex.staticThis[va]; ok {
			if loc := ex.thisLoc[va]; loc.Reg != 0 {
				seed[loc.Reg] = addr
			}
		}
		states := ex.d.staticAddressFlowFrom(fn, seed)
		for i := range fn.Instructions {
			in := &fn.Instructions[i]
			if in.Inst.Op != x86asm.CALL || states[i] == nil {
				continue
			}
			t := resolveTarget(in)
			callee := ex.d.Functions[t]
			// Only the dashboard's own unnamed code holds methods of its static objects.
			if callee == nil || t == 0 || callee.Symbol != nil || ex.d.sectionOf(t) != code {
				continue
			}
			chosen := x86asm.Reg(0)
			for _, r := range ex.d.liveInRegisters(callee) {
				if _, ok := states[i][r]; !ok {
					continue
				}
				if chosen == 0 || r == x86asm.ECX {
					chosen = r
				}
			}
			if chosen != 0 {
				record(t, states[i][chosen], argLocation{Reg: chosen})
				continue
			}
			// With no register carrying it, the object may be the first stack argument, pushed right before the call.
			if i > 0 && calleeCleanupOf(callee) > 0 {
				prev := &fn.Instructions[i-1]
				if prev.Inst.Op == x86asm.PUSH {
					if imm, ok := prev.Inst.Args[0].(x86asm.Imm); ok && ex.isDataAddress(uint32(imm)) {
						record(t, uint32(imm), argLocation{Stack: 0})
					}
				}
			}
		}
	}
	return found
}

// Seeds the static object where the function takes it and numbers the other inputs, registers before the stack
func (ex *Executor) seedThis(s *execState, fn, addr uint32, stackArgs int) {
	ex.staticThisCallers()
	loc, known := ex.thisLoc[fn]
	if callee := ex.d.Functions[fn]; callee != nil && calleeCleanupOf(callee)/4 > stackArgs {
		stackArgs = calleeCleanupOf(callee) / 4
	}
	this := Expr(Ptr{"", int32(addr)})
	n := 0
	next := func() Expr {
		e := Sym{fmt.Sprintf("arg%d", n)}
		n++
		return e
	}
	thisReg := x86asm.ECX
	if known && loc.Reg != 0 {
		thisReg = loc.Reg
	}
	for _, r := range ex.d.registerInputs(fn) {
		v := next()
		if r == thisReg {
			v = this
		}
		s.setInput(r, v)
	}
	if !known || loc.Reg != 0 {
		s.regs[regIndex[thisReg]] = this
	}
	count := stackArgs
	if known && loc.Reg == 0 && loc.Stack >= count {
		count = loc.Stack + 1
	}
	args := make([]Expr, count)
	for i := range args {
		args[i] = next()
	}
	if known && loc.Reg == 0 {
		args[loc.Stack] = this
	}
	s.pushArgs(args...)
}

// Reports whether an address lies in a writable data section
func (ex *Executor) isDataAddress(va uint32) bool {
	sec := ex.d.sectionOf(va)
	return sec != nil && sec.Flags&0x1 != 0
}

// Maps globals holding heap objects to the vtable their constructor installs
func (ex *Executor) resolveGlobalObjects() {
	staticThis := ex.staticThisCallers()
	for _, fnVA := range ex.d.functionVAs() {
		for _, c := range ex.d.constructions(ex.d.Functions[fnVA]) {
			if c.vtable == 0 {
				continue
			}
			for _, g := range c.globals {
				ex.vtables[g] = c.vtable
			}
		}
		ins := ex.d.Functions[fnVA].Instructions
		bases := map[x86asm.Reg]uint32{}
		if t, ok := staticThis[fnVA]; ok && ex.thisLoc[fnVA].Reg != 0 {
			bases[ex.thisLoc[fnVA].Reg] = t
		}
		var pushed []uint32
		for i, in := range ins {
			// A library call constructing an object through an out pointer types the address pushed for it.
			switch in.Inst.Op {
			case x86asm.PUSH:
				addr := uint32(0)
				if imm, ok := in.Inst.Args[0].(x86asm.Imm); ok {
					addr = uint32(imm)
				} else if r, ok := in.Inst.Args[0].(x86asm.Reg); ok {
					addr = bases[r]
				}
				pushed = append(pushed, addr)
			case x86asm.CALL:
				if vt, ok := ex.d.outObjectVtable(resolveTarget(&in)); ok {
					idx := outParams[ex.d.Functions[resolveTarget(&in)].Symbol.Name]
					if idx < len(pushed) {
						if addr := pushed[len(pushed)-1-idx]; addr != 0 && ex.isDataAddress(addr) {
							ex.vtables[addr] = vt
						}
					}
				}
				pushed = nil
				delete(bases, x86asm.EAX)
				delete(bases, x86asm.ECX)
				delete(bases, x86asm.EDX)
			case x86asm.LEA:
				if dst, ok := in.Inst.Args[0].(x86asm.Reg); ok {
					if m, ok := in.Inst.Args[1].(x86asm.Mem); ok && m.Index == 0 && bases[m.Base] != 0 {
						bases[dst] = bases[m.Base] + uint32(m.Disp)
					} else {
						delete(bases, dst)
					}
				}
			}
			if in.Inst.Op != x86asm.MOV || argCount(&in) < 2 {
				continue
			}
			if dst, ok := in.Inst.Args[0].(x86asm.Reg); ok {
				if imm, ok := in.Inst.Args[1].(x86asm.Imm); ok && ex.isDataAddress(uint32(imm)) {
					bases[dst] = uint32(imm)
				} else if src, ok := in.Inst.Args[1].(x86asm.Reg); ok && bases[src] != 0 {
					bases[dst] = bases[src]
				} else {
					delete(bases, dst)
				}
				continue
			}
			mem, ok := in.Inst.Args[0].(x86asm.Mem)
			if !ok || mem.Index != 0 {
				continue
			}
			globalVA := uint32(mem.Disp)
			if mem.Base != 0 {
				base, known := bases[mem.Base]
				if !known {
					continue
				}
				globalVA = base + uint32(mem.Disp)
			}
			src, ok := in.Inst.Args[1].(x86asm.Reg)
			if !ok {
				continue
			}
			holders := map[x86asm.Reg]bool{x86asm.EAX: true}
			for j := i - 1; j >= 0 && j >= i-10; j-- {
				jn := &ins[j]
				if jn.Inst.Op == x86asm.MOV && argCount(jn) == 2 {
					if dst, ok := jn.Inst.Args[0].(x86asm.Reg); ok {
						if from, ok := jn.Inst.Args[1].(x86asm.Reg); ok && from == x86asm.EAX {
							holders[dst] = true
						}
					}
				}
				if jn.Inst.Op != x86asm.CALL {
					continue
				}
				if !holders[src] {
					break
				}
				if info, ok := ex.d.constructorInfo()[resolveTarget(jn)]; ok {
					ex.vtables[globalVA] = info.vtable
				}
				break
			}
		}
	}
}

// LearnGlobalObjects runs the functions invoked on static objects and records which
// globals receive freshly constructed objects, binding those globals to vtables.
func (ex *Executor) LearnGlobalObjects(allocators []uint32, opaque []uint32) {
	callers := ex.staticThisCallers()
	seen := map[uint32]bool{}
	for _, fnVA := range slices.Sorted(maps.Keys(callers)) {
		thisVA := callers[fnVA]
		if seen[fnVA] {
			continue
		}
		seen[fnVA] = true
		stage(fmt.Sprintf("learn %08X on %08X in %v", fnVA, thisVA, ex.thisLoc[fnVA]))
		sub := NewExecutorShared(ex)
		for _, va := range allocators {
			sub.Opaque[va] = true
		}
		for _, va := range opaque {
			sub.Opaque[va] = true
		}
		count := 0
		sub.allocHook = func() Expr {
			count++
			return Ptr{fmt.Sprintf("learn%d", count), 0}
		}
		for attempt := 0; attempt < 6; attempt++ {
			paths, err := sub.Run(fnVA, func(s *execState) {
				ex.seedThis(s, fnVA, thisVA, 0)
			})
			harvest := func(effects []Effect) {
				objVt := map[string]uint32{}
				for _, e := range effects {
					if e.Kind == "store" && e.Off == 0 && e.Size == 4 && strings.HasPrefix(e.Base, "learn") {
						if c, ok := isConst(e.Value); ok && ex.d.isVtable(c) {
							objVt[e.Base] = c
						}
					}
				}
				for _, e := range effects {
					if e.Kind == "store" && e.Base == "" && e.Size == 4 {
						if p, ok := e.Value.(Ptr); ok && p.Off == 0 {
							if vt, ok := objVt[p.Base]; ok {
								ex.vtables[uint32(e.Off)] = vt
							}
						}
					}
				}
			}
			for _, pth := range paths {
				harvest(pth.Effects)
			}
			if err == nil {
				break
			}
			ee, ok := err.(*ExecError)
			if !ok {
				break
			}
			harvest(ee.Effects)
			if !ee.Limit || len(ee.Frames) == 0 {
				break
			}
			sub.Opaque[ee.Frames[len(ee.Frames)-1]] = true
		}
	}
}

// RunWithRetries executes a function, marking callees that exhaust their budget opaque and
// retrying, and returns the completed paths plus any effects of a final failing path.
func (ex *Executor) RunWithRetries(entry uint32, init func(s *execState), retries int) ([]Path, []Effect, []uint32, error) {
	var opaqued []uint32
	for attempt := 0; ; attempt++ {
		paths, err := ex.Run(entry, init)
		if err == nil {
			return paths, nil, opaqued, nil
		}
		ee, ok := err.(*ExecError)
		if !ok || !ee.Limit || len(ee.Frames) == 0 || attempt >= retries {
			var partial []Effect
			if ok {
				partial = ee.Effects
			}
			return paths, partial, opaqued, err
		}
		inner := ee.Frames[len(ee.Frames)-1]
		ex.Opaque[inner] = true
		opaqued = append(opaqued, inner)
	}
}

// NewExecutorShared makes an executor reusing another one's precomputed knowledge.
func NewExecutorShared(base *Executor) *Executor {
	return &Executor{d: base.d, arrayIdx: base.arrayIdx, Stop: base.Stop, vtables: base.vtables, symbols: base.symbols, nameOf: base.nameOf, Opaque: map[uint32]bool{}, ftol: base.ftol, probe: base.probe, Results: map[uint32]Expr{}, written: base.written, Allocators: base.Allocators, savePush: base.savePush, frameAlloc: base.frameAlloc, frameSizes: base.frameSizes, staticThis: base.staticThis, thisLoc: base.thisLoc}
}

// Reports whether a data pointer starts a table of code addresses
func (d *Disassembly) isVtable(va uint32) bool {
	v, ok := d.Image.ReadU32(va)
	if !ok {
		return false
	}
	_, isFn := d.Functions[v]
	return isFn
}

var regIndex = map[x86asm.Reg]int{
	x86asm.EAX: 0, x86asm.ECX: 1, x86asm.EDX: 2, x86asm.EBX: 3, x86asm.ESP: 4, x86asm.EBP: 5, x86asm.ESI: 6, x86asm.EDI: 7,
	x86asm.AX: 0, x86asm.CX: 1, x86asm.DX: 2, x86asm.BX: 3, x86asm.SP: 4, x86asm.BP: 5, x86asm.SI: 6, x86asm.DI: 7,
	x86asm.AL: 0, x86asm.CL: 1, x86asm.DL: 2, x86asm.BL: 3, x86asm.AH: 0, x86asm.CH: 1, x86asm.DH: 2, x86asm.BH: 3,
}

func regSize(r x86asm.Reg) (size int, high bool) {
	switch r {
	case x86asm.AL, x86asm.CL, x86asm.DL, x86asm.BL:
		return 1, false
	case x86asm.AH, x86asm.CH, x86asm.DH, x86asm.BH:
		return 1, true
	case x86asm.AX, x86asm.CX, x86asm.DX, x86asm.BX, x86asm.SP, x86asm.BP, x86asm.SI, x86asm.DI:
		return 2, false
	}
	return 4, false
}

func (s *execState) clone() *execState {
	n := *s
	if s.pending != nil {
		pc := *s.pending
		n.pending = &pc
	}
	n.bound = make(map[string]Expr, len(s.bound))
	for k, v := range s.bound {
		n.bound[k] = v
	}
	n.boundLoads = make(map[string]Load, len(s.boundLoads))
	for k, v := range s.boundLoads {
		n.boundLoads[k] = v
	}
	n.loops = make(map[uint32]int, len(s.loops))
	for k, v := range s.loops {
		n.loops[k] = v
	}
	n.mem = make(map[memKey]memVal, len(s.mem))
	for k, v := range s.mem {
		n.mem[k] = v
	}
	n.fpu = append([]Expr(nil), s.fpu...)
	n.guards = append([]Expr(nil), s.guards...)
	n.guardPos = append([]int(nil), s.guardPos...)
	n.effects = append([]Effect(nil), s.effects...)
	n.stack = append([]RetAddr(nil), s.stack...)
	n.frames = append([]uint32(nil), s.frames...)
	n.fsteps = append([]int(nil), s.fsteps...)
	n.entry = append([][8]Expr(nil), s.entry...)
	return &n
}

func (s *execState) getReg(r x86asm.Reg) Expr {
	idx, ok := regIndex[r]
	if !ok {
		panic(fmt.Sprintf("unsupported register %v", r))
	}
	v := s.regs[idx]
	if v == nil {
		v = Sym{"reg." + wideRegister(r).String()}
	}
	size, high := regSize(r)
	switch {
	case size == 4:
		return v
	case size == 2:
		return mkUn("zx16", v)
	case high:
		return mkUn("zx8", mkBin("shr", v, IConst{8}))
	default:
		return mkUn("zx8", v)
	}
}

func (s *execState) setReg(r x86asm.Reg, v Expr) {
	idx := regIndex[r]
	size, high := regSize(r)
	old := s.regs[idx]
	if old == nil {
		old = Sym{"reg." + wideRegister(r).String()}
	}
	switch {
	case size == 4:
		s.regs[idx] = v
	case size == 2:
		s.regs[idx] = mkBin("or", mkBin("and", old, IConst{0xFFFF0000}), mkUn("zx16", v))
	case high:
		s.regs[idx] = mkBin("or", mkBin("and", old, IConst{0xFFFF00FF}), mkBin("shl", mkUn("zx8", v), IConst{8}))
	default:
		s.regs[idx] = mkBin("or", mkBin("and", old, IConst{0xFFFFFF00}), mkUn("zx8", v))
	}
	if r == x86asm.AX || r == x86asm.EAX || r == x86asm.AH || r == x86asm.AL {
		s.axFSW = false
	}
}

// Computes the address expression of a memory operand
func (ex *Executor) addr(s *execState, m x86asm.Mem) Expr {
	// Thread local memory, which the compiler links its exception records into, is its own region
	if m.Segment != 0 && m.Base == 0 && m.Index == 0 {
		return Ptr{strings.ToLower(m.Segment.String()), int32(m.Disp)}
	}
	// A displacement inside the image that a call result indexes is a table, not a field of what it returned
	if m.Base != 0 && m.Index == 0 && ex.d.sectionOf(uint32(m.Disp)) != nil && indexesTable(s.getReg(m.Base)) {
		return Bin{"add", IConst{uint32(m.Disp)}, s.getReg(m.Base)}
	}
	var e Expr = IConst{uint32(m.Disp)}
	if m.Base != 0 {
		e = mkBin("add", s.getReg(m.Base), e)
	}
	if m.Index != 0 {
		idx := mkBin("mul", s.getReg(m.Index), IConst{uint32(m.Scale)})
		e = mkBin("add", e, idx)
	}
	return e
}

// Turns an absolute integer address or an opaque call result into a pointer value for memory access
// Reports whether a value is a call result, which indexes a table rather than pointing at one
func indexesTable(v Expr) bool {
	switch p := v.(type) {
	case CallResult:
		return true
	case Ptr:
		return strings.HasPrefix(p.Base, "res")
	}
	return false
}

// The address a memory access reads, a call result added to an image address being a table it indexes
func (ex *Executor) asAddress(a Expr) Expr {
	p := asPtr(a)
	q, ok := p.(Ptr)
	if ok && strings.HasPrefix(q.Base, "res") && ex.d.sectionOf(uint32(q.Off)) != nil {
		return a
	}
	return p
}

func asPtr(a Expr) Expr {
	switch v := a.(type) {
	case IConst:
		return Ptr{"", int32(v.V)}
	case CallResult:
		return Ptr{fmt.Sprintf("res%d", v.Seq), 0}
	case Bin:
		if v.Op == "add" {
			if cr, ok := v.A.(CallResult); ok {
				if c, ok := isConst(v.B); ok {
					return Ptr{fmt.Sprintf("res%d", cr.Seq), int32(c)}
				}
			}
			if cr, ok := v.B.(CallResult); ok {
				if c, ok := isConst(v.A); ok {
					return Ptr{fmt.Sprintf("res%d", cr.Seq), int32(c)}
				}
			}
		}
	}
	return a
}

func (ex *Executor) store(s *execState, a Expr, size int, v Expr, va uint32) {
	a = ex.asAddress(a)
	p, ok := a.(Ptr)
	if ok && p.Base == "" && uint32(p.Off) < ex.d.Image.BaseAddr {
		s.nulled = true
	}
	if !ok {
		s.effects = append(s.effects, Effect{Kind: "store", VA: va, Base: "?", Addr: a, Size: size, Value: v})
		return
	}
	s.unbind(p.Base, int64(p.Off), int64(p.Off)+int64(size))
	// Overwrite overlapping entries, which can start up to 63 bytes before this store.
	for delta := int32(-63); delta < int32(size); delta++ {
		k := memKey{p.Base, p.Off + delta}
		mv, ok := s.mem[k]
		if !ok || k.off+int32(mv.size) <= p.Off || (k.off == p.Off && mv.size == size) {
			continue
		}
		delete(s.mem, k)
		if k.off < p.Off || k.off+int32(mv.size) > p.Off+int32(size) {
			if mv.size > 8 {
				// Partial overlap of a vector or matrix: keep the untouched elements.
				for b := int32(0); b < int32(mv.size); b += 4 {
					bo := k.off + b
					if bo+4 > p.Off && bo < p.Off+int32(size) {
						continue
					}
					s.mem[memKey{k.base, bo}] = memVal{size: 4, v: Elem{mv.v, int(b / 4)}, va: mv.va}
				}
			} else {
				// Partial overlap: split into bytes.
				for b := int32(0); b < int32(mv.size); b++ {
					bo := k.off + b
					if bo >= p.Off && bo < p.Off+int32(size) {
						continue
					}
					s.mem[memKey{k.base, bo}] = memVal{size: 1, v: byteOf(mv.v, int(b), mv.size), va: mv.va}
				}
			}
		}
	}
	s.mem[memKey{p.Base, p.Off}] = memVal{size: size, v: v, va: va}
	if p.Base == "" && !ex.d.layoutBusy && ex.stateStore(s, uint32(p.Off), size, v, va) {
		return
	}
	// A path folds its own stack away, a lifted block leaves its frame for the blocks after it to read
	if (p.Base != "stack" || ex.block != nil) && !threadLocalBase(p.Base) {
		s.effects = append(s.effects, Effect{Kind: "store", VA: va, Base: p.Base, Off: p.Off, Size: size, Value: v})
	}
}

// Stores the folded frame locals a forgotten range covers ahead of the call for loads after it
func (ex *Executor) spillForgotten(s *execState, base string, start, end int64) {
	if ex.block != nil {
		return
	}
	var keys []memKey
	for k, mv := range s.mem {
		if k.base == base && !mv.frame && int64(k.off)+int64(mv.size) > start && int64(k.off) < end {
			keys = append(keys, k)
		}
	}
	if len(keys) == 0 {
		return
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].off < keys[j].off })
	at := len(s.effects)
	for i := len(s.effects) - 1; i >= 0; i-- {
		if s.effects[i].Kind == "call" && s.effects[i].Seq == ex.seq {
			at = i
			break
		}
	}
	var spills []Effect
	for _, k := range keys {
		mv := s.mem[k]
		spills = append(spills, Effect{Kind: "store", VA: mv.va, Base: base, Off: k.off, Size: mv.size, Value: mv.v})
	}
	s.effects = append(s.effects[:at], append(spills, s.effects[at:]...)...)
}

func byteOf(v Expr, i int, size int) Expr {
	if f, ok := v.(FConst); ok {
		if size == 4 {
			v = IConst{math.Float32bits(float32(f.V))}
		} else {
			bits := math.Float64bits(f.V)
			if i < 4 {
				v = IConst{uint32(bits)}
			} else {
				v = IConst{uint32(bits >> 32)}
				i -= 4
			}
		}
	}
	return mkUn("zx8", mkBin("shr", v, IConst{uint32(8 * i)}))
}

// Reads an integer value of the given size at an address
func (ex *Executor) load(s *execState, a Expr, size int) Expr {
	a = ex.asAddress(a)
	p, ok := a.(Ptr)
	if !ok {
		if v, stored := s.storedThrough(a, size); stored {
			return v
		}
		return Load{Addr: a, Size: size, Epoch: len(s.effects)}
	}
	if mv, ok := s.mem[memKey{p.Base, p.Off}]; ok && mv.size == size {
		if f, isF := mv.v.(FConst); isF {
			if size == 4 {
				return IConst{math.Float32bits(float32(f.V))}
			}
			return IConst{uint32(math.Float64bits(f.V))}
		}
		return mv.v
	}
	// An aligned word inside a vector or matrix value is that value's element.
	if size == 4 {
		for delta := int32(4); delta < 64; delta += 4 {
			k := memKey{p.Base, p.Off - delta}
			if mv, ok := s.mem[k]; ok && mv.size > 8 && p.Off+4 <= k.off+int32(mv.size) {
				return Elem{mv.v, int(delta / 4)}
			}
		}
	}
	// Assemble from overlapping entries byte by byte when any exist.
	var parts []Expr
	anyHit := false
	for b := int32(0); b < int32(size); b++ {
		bo := p.Off + b
		var be Expr
		found := false
		for delta := int32(0); delta < 64 && !found; delta++ {
			k := memKey{p.Base, bo - delta}
			if mv, ok := s.mem[k]; ok && bo < k.off+int32(mv.size) {
				be = byteOf(mv.v, int(bo-k.off), mv.size)
				found = true
			}
		}
		if !found {
			be = byteOf(ex.loadBacking(s, Ptr{p.Base, bo}, 1), 0, 1)
		} else {
			anyHit = true
		}
		parts = append(parts, be)
	}
	if !anyHit {
		return ex.loadBacking(s, p, size)
	}
	var out Expr = IConst{0}
	for i, be := range parts {
		out = mkBin("or", out, mkBin("shl", be, IConst{uint32(8 * i)}))
	}
	return out
}

// Reads memory never written on this path from the image or as a symbolic input a guard may have fixed
func (ex *Executor) loadBacking(s *execState, p Ptr, size int) Expr {
	if p.Base == "" {
		va := uint32(p.Off)
		if va < ex.d.Image.BaseAddr {
			s.nulled = true
		}
		if size == 4 {
			if _, ok := ex.vtables[va]; ok {
				return Ptr{fmt.Sprintf("g%08X", va), 0}
			}
			if name, ok := ex.d.Imports[va]; ok {
				return Ptr{"import:" + name, 0}
			}
		}
		if ex.isConstantGlobal(va, size) {
			switch size {
			case 1:
				v, _ := ex.d.Image.ReadU8(va)
				return IConst{uint32(v)}
			case 2:
				v, _ := ex.d.Image.ReadU16(va)
				return IConst{uint32(v)}
			case 4:
				v, _ := ex.d.Image.ReadU32(va)
				return IConst{v}
			}
		}
	}
	return s.substitute(Load{Base: p.Base, Off: p.Off, Size: size, Epoch: len(s.effects)})
}

// Reports whether a memory region is the thread local storage a segment register addresses
func threadLocalBase(base string) bool {
	switch base {
	case "fs", "gs", "es", "ds", "cs", "ss":
		return true
	}
	return false
}

// Reports whether an expression already carries a floating point value
func isFloatExpr(e Expr) bool {
	switch v := e.(type) {
	case FConst, Elem:
		return true
	case Un:
		switch v.Op {
		case "f64", "f32", "bits2f", "bits2d", "i2f", "fneg", "fabs", "fsin", "fcos", "fsqrt", "ftan", "ffloor", "fceil", "fexp", "flog", "flog10", "flog2", "ftrunc", "fround", "fatan", "fasin", "facos", "fsinh", "fcosh", "ftanh":
			return true
		}
	case Bin:
		return floatBinOps[v.Op]
	case Sel:
		return isFloatExpr(v.A) && isFloatExpr(v.B)
	}
	return false
}

func (ex *Executor) loadFloat(s *execState, a Expr, size int) Expr {
	a = ex.asAddress(a)
	p, ok := a.(Ptr)
	if ok {
		if mv, ok := s.mem[memKey{p.Base, p.Off}]; ok && mv.size == size {
			if isFloatExpr(mv.v) {
				return mv.v
			}
			if size == 4 {
				return mkUn("bits2f", mv.v)
			}
			return Un{"bits2d", mv.v}
		}
		if p.Base == "" {
			if ex.isConstantGlobal(uint32(p.Off), size) {
				if size == 4 {
					v, _ := ex.d.Image.ReadU32(uint32(p.Off))
					return FConst{float64(math.Float32frombits(v))}
				}
				lo, _ := ex.d.Image.ReadU32(uint32(p.Off))
				hi, _ := ex.d.Image.ReadU32(uint32(p.Off) + 4)
				return FConst{math.Float64frombits(uint64(hi)<<32 | uint64(lo))}
			}
		}
		if size == 4 {
			w := ex.load(s, a, 4)
			if isFloatExpr(w) {
				return w
			}
			return mkUn("bits2f", w)
		}
		return Un{"f64", Load{Base: p.Base, Off: p.Off, Size: 8, Epoch: len(s.effects)}}
	}
	if v, stored := s.storedThrough(a, size); stored {
		if isFloatExpr(v) {
			return v
		}
		if size == 4 {
			return mkUn("bits2f", v)
		}
		return Un{"bits2d", v}
	}
	if size == 4 {
		return mkUn("bits2f", Load{Addr: a, Size: 4, Epoch: len(s.effects)})
	}
	return Un{"f64", Load{Addr: a, Size: size, Epoch: len(s.effects)}}
}

// The value the path last stored through a symbolic address, which a later load of the same address reads back
func (s *execState) storedThrough(a Expr, size int) (Expr, bool) {
	for i := len(s.effects) - 1; i >= 0; i-- {
		e := &s.effects[i]
		if e.Kind != "store" || e.Addr == nil || e.Size != size {
			continue
		}
		if sameExpr(e.Addr, a) {
			return e.Value, true
		}
	}
	return nil, false
}

func (ex *Executor) storeFloat(s *execState, a Expr, size int, v Expr, va uint32) {
	if size == 4 {
		v = mkUn("f32", v)
	}
	ex.store(s, a, size, v, va)
}

// Pushes a value, a return address marking the slot as one of the frame
func (s *execState) push(v Expr) {
	s.regs[4] = mkBin("sub", s.regs[4], IConst{4})
	_, isRet := v.(RetAddr)
	s.mem[memKey{stackBase(s), stackOff(s)}] = memVal{size: 4, v: v, frame: isRet}
}

func stackBase(s *execState) string { return s.regs[4].(Ptr).Base }
func stackOff(s *execState) int32   { return s.regs[4].(Ptr).Off }

func (ex *Executor) pop(s *execState) Expr {
	v := ex.load(s, s.regs[4], 4)
	s.regs[4] = mkBin("add", s.regs[4], IConst{4})
	return v
}

func (ex *Executor) operand(s *execState, in *Instruction, a x86asm.Arg, size int) Expr {
	return s.substitute(ex.rawOperand(s, in, a, size))
}

func (ex *Executor) rawOperand(s *execState, in *Instruction, a x86asm.Arg, size int) Expr {
	switch v := a.(type) {
	case x86asm.Reg:
		return s.getReg(v)
	case x86asm.Imm:
		switch size {
		case 1:
			return IConst{uint32(uint8(v))}
		case 2:
			return IConst{uint32(uint16(v))}
		}
		return IConst{uint32(int32(v))}
	case x86asm.Mem:
		return ex.load(s, ex.addr(s, v), size)
	case x86asm.Rel:
		return IConst{in.VA + uint32(in.Inst.Len) + uint32(int32(v))}
	}
	panic(fmt.Sprintf("operand %T", a))
}

func opSize(in *Instruction, a x86asm.Arg) int {
	if r, ok := a.(x86asm.Reg); ok {
		sz, _ := regSize(r)
		return sz
	}
	if in.Inst.MemBytes != 0 {
		return in.Inst.MemBytes
	}
	if in.Inst.DataSize == 16 {
		return 2
	}
	return 4
}

func (ex *Executor) setOperand(s *execState, in *Instruction, a x86asm.Arg, size int, v Expr) {
	switch t := a.(type) {
	case x86asm.Reg:
		s.setReg(t, v)
	case x86asm.Mem:
		ex.store(s, ex.addr(s, t), size, v, in.VA)
	default:
		panic("setOperand")
	}
}

func maskSize(v Expr, size int) Expr {
	switch size {
	case 1:
		return mkUn("zx8", v)
	case 2:
		return mkUn("zx16", v)
	}
	return v
}

// Run executes a function symbolically from entry with prepared registers and memory.
func (ex *Executor) Run(entry uint32, init func(s *execState)) ([]Path, error) {
	s := &execState{mem: map[memKey]memVal{}, va: entry, root: entry}
	s.regs[4] = Ptr{"stack", 0x10000}
	s.regs[5] = Sym{"reg.EBP"}
	init(s)
	s.push(RetAddr{VA: 0, Clean: 0})
	s.rest = stackOff(s)
	s.entry = [][8]Expr{s.regs}
	ex.paths, ex.dropped = nil, 0
	if err := ex.run(s); err != nil {
		return ex.paths, err
	}
	for i, p := range ex.paths {
		if pathTooWide(p) {
			return ex.paths, &ExecError{VA: entry, Limit: true, Err: fmt.Errorf("path %d expands past %d expression nodes", i, maxPathNodes)}
		}
		if va, outside := ex.d.outsideImage(p); outside {
			return ex.paths, &ExecError{VA: entry, Err: fmt.Errorf("path %d reads %08X, which the image loads no memory into", i, va)}
		}
	}
	// Dashboard code reads as the XDK calls it makes, the ones the link time optimizer inlined included
	if fn := ex.d.Functions[entry]; fn != nil && fn.Symbol == nil && !ex.d.inLibrary(entry) {
		paths, err := ex.foldInlinedCreations(ex.paths)
		if err != nil {
			return ex.paths, err
		}
		return ex.foldInlinedResources(paths)
	}
	return ex.paths, nil
}

// Lists the argument registers besides ecx an unnamed dashboard function reads, which its callers fill before the stack
func (d *Disassembly) registerInputs(va uint32) []x86asm.Reg {
	fn := d.Functions[va]
	if fn == nil || fn.Symbol != nil || d.inLibrary(va) {
		return nil
	}
	var out []x86asm.Reg
	for _, r := range d.liveInRegisters(fn) {
		if r != x86asm.ECX {
			out = append(out, r)
		}
	}
	return append(out, d.liveInFloatRegisters(fn)...)
}

// Sets a register input, a vector register taking the float in its first lane
func (s *execState) setInput(r x86asm.Reg, v Expr) {
	if i, ok := xmmIndex(r); ok {
		s.xmm[i][0] = v
		return
	}
	s.regs[regIndex[r]] = v
}

// Reads a register input the way a callee receives it, a vector register by the float in its first lane
func (s *execState) input(r x86asm.Reg) Expr {
	if i, ok := xmmIndex(r); ok {
		return s.xmmLane(i, 0)
	}
	return s.getReg(r)
}

// Seeds a function's inputs with numbered symbols, its register inputs taking the leading numbers before the stack
func (ex *Executor) seedArgs(s *execState, va uint32, prefix string, stackArgs int) {
	n := 0
	next := func() Expr {
		e := Sym{fmt.Sprintf("%s%d", prefix, n)}
		n++
		return e
	}
	for _, r := range ex.d.registerInputs(va) {
		s.setInput(r, next())
	}
	args := make([]Expr, stackArgs)
	for i := range args {
		args[i] = next()
	}
	s.pushArgs(args...)
}

// Reports whether a push saves a callee saved register still at its entry value rather than passing an input,
// a lifted block reading that off the routine's own flow since every register looks untouched where it starts
func (ex *Executor) savesRegister(s *execState, in *Instruction, a x86asm.Arg, v Expr) bool {
	r, ok := a.(x86asm.Reg)
	if !ok || (r != x86asm.EBP && r != x86asm.EBX && r != x86asm.ESI && r != x86asm.EDI) || len(s.entry) == 0 {
		return false
	}
	current := s.root
	if len(s.frames) > 0 {
		current = s.frames[len(s.frames)-1]
	}
	if ex.block != nil && len(s.frames) == 0 {
		return ex.d.savingPush(ex.d.Functions[current], in.VA)
	}
	if slices.Contains(ex.d.registerInputs(current), r) {
		return false
	}
	at := s.entry[len(s.entry)-1][regIndex[r]]
	if at == nil {
		at = Sym{"reg." + r.String()}
	}
	return ExprString(at) == ExprString(v)
}

// Pushes stack arguments in order after init so arg0 is nearest the return slot
func (s *execState) pushArgs(args ...Expr) {
	for i := len(args) - 1; i >= 0; i-- {
		s.push(args[i])
	}
}

func (ex *Executor) run(s *execState) error {
	for {
		limit := maxSteps
		if ex.StepLimit > 0 {
			limit = ex.StepLimit
		}
		if s.steps > limit {
			return &ExecError{VA: s.va, Frames: s.frames, Limit: true, Err: fmt.Errorf("step limit exceeded"), Effects: s.effects}
		}
		pathLimit := maxPaths
		if ex.PathLimit > 0 {
			pathLimit = ex.PathLimit
		}
		if len(ex.paths)+ex.dropped > pathLimit {
			return &ExecError{VA: s.va, Frames: s.frames, Limit: true, Err: fmt.Errorf("path limit exceeded"), Effects: s.effects}
		}
		if ex.Stop[s.va] && len(s.frames) == 0 {
			ex.finish(s)
			return nil
		}
		if b := ex.block; b != nil && len(s.frames) == 0 && b.started && (b.leaders[s.va] || s.va == b.start) {
			b.term = &blockTerm{kind: "jump", target: s.va}
			return nil
		}
		in, ok := ex.d.InsnByVA[s.va]
		if !ok {
			return &ExecError{VA: s.va, Frames: s.frames, Err: fmt.Errorf("no instruction"), Effects: s.effects}
		}
		if ex.block != nil {
			ex.block.started = true
		}
		s.steps++
		frameLimit := maxFrameSteps
		if ex.FrameLimit < 0 {
			frameLimit = 1 << 30
		} else if ex.FrameLimit > 0 {
			frameLimit = ex.FrameLimit
		}
		for i := range s.fsteps {
			s.fsteps[i]++
			if s.fsteps[i] > frameLimit {
				return &ExecError{VA: s.va, Frames: s.frames[:i+1], Limit: true, Err: fmt.Errorf("frame step budget exceeded"), Effects: s.effects}
			}
		}
		if debugTrace && (s.steps%5000 == 0 || s.steps < 400) {
			fmt.Printf("  trace: %08X %-28s esp=%s pushes=%d pending=%v\n", in.VA, in.Inst.String(), ExprString(s.regs[4]), s.pushes, s.pending != nil)
		}
		if a, ok := ex.arrayIdx[s.va]; ok {
			ex.arrayElement(s, in, a)
			continue
		}
		if r, ok := ex.d.resourceRef[s.va]; ok {
			if err := ex.resourceCount(s, in, r); err != nil {
				return &ExecError{VA: in.VA, Insn: in.Inst.String(), Frames: s.frames, Err: err, Effects: s.effects}
			}
			continue
		}
		next := s.va + uint32(in.Inst.Len)
		s.lastSave = false
		done, err := ex.step(s, in, next)
		if err != nil {
			if ee, ok := err.(*ExecError); ok {
				return ee
			}
			return &ExecError{VA: in.VA, Insn: in.Inst.String(), Frames: s.frames, Err: err, Effects: s.effects}
		}
		// A routine realigning or sizing its stack by a value leaves no frame offset for the next push to land at
		if _, framed := s.regs[4].(Ptr); !framed {
			return &ExecError{VA: in.VA, Insn: in.Inst.String(), Frames: s.frames, Err: fmt.Errorf("the stack pointer becomes %s", ExprString(s.regs[4])), Effects: s.effects}
		}
		ex.settleRest(s, in)
		if ex.block != nil && ex.block.term != nil && (ex.block.term.kind == "trap" || ex.block.term.kind == "halt") {
			return nil
		}
		// The dashboard never runs a path that reaches through a null pointer, so it is left unsummarized
		if s.nulled && ex.block == nil {
			ex.dropped++
			return nil
		}
		if done {
			return nil
		}
	}
}

// Calls the function a jump lands in with the return address kept under its arguments, then returns
func (ex *Executor) tailCall(s *execState, in *Instruction) (bool, error) {
	ex.tail = true
	err := ex.call(s, in, 0)
	ex.tail = false
	if err != nil {
		return false, err
	}
	if s.va != 0 {
		return false, nil
	}
	clean := ex.tailClean
	ex.tailClean = 0
	return ex.doReturn(s, int32(clean))
}

// Argument bytes a tail jump hands a callee named without its arity, as many words as its body reads
func (ex *Executor) tailArgBytes(target uint32, argBytes int) int {
	landing := ex.d.Functions[ex.d.thunkTarget(target)]
	if landing == nil {
		return argBytes
	}
	return 4 * ex.d.stackInputs(landing)
}

// Pops the return address and either resumes the caller or finishes the path
func (ex *Executor) doReturn(s *execState, clean int32) (bool, error) {
	s.pending = nil
	s.pushes = 0
	if ex.block != nil && len(s.frames) == 0 {
		ex.finish(s)
		return true, nil
	}
	rv := ex.pop(s)
	ra, ok := rv.(RetAddr)
	if !ok {
		return false, fmt.Errorf("return address lost: %s", ExprString(rv))
	}
	s.regs[4] = mkBin("add", s.regs[4], IConst{uint32(clean)})
	if ra.VA == 0 {
		ex.finish(s)
		return true, nil
	}
	if len(s.frames) > 0 {
		s.frames = s.frames[:len(s.frames)-1]
		s.fsteps = s.fsteps[:len(s.fsteps)-1]
		s.entry = s.entry[:len(s.entry)-1]
	}
	s.va = ra.VA
	return false, nil
}

// Finishes a path when the outermost return executes, a lifted block ending in its return instead
func (ex *Executor) finish(s *execState) {
	if ex.block != nil {
		// A block returning what it entered with hands back the accumulator as the runtime holds it
		ex.block.term = &blockTerm{kind: "return", value: s.getReg(x86asm.EAX)}
		return
	}
	ex.paths = append(ex.paths, Path{Guards: s.guards, GuardPos: s.guardPos, Effects: s.effects, Return: s.regs[0]})
}

// Forks on a symbolic condition and continues both outcomes, a loop leaving after a few symbolic
// iterations with its exit condition recorded as a guard
func (ex *Executor) branch(s *execState, cond Expr, taken, fall uint32) (bool, error) {
	if c, ok := isConst(cond); ok {
		if c != 0 {
			s.va = taken
		} else {
			s.va = fall
		}
		return false, nil
	}
	if ex.block != nil {
		if len(s.frames) > 0 {
			return false, fmt.Errorf("inlined helper branches inside a lifted block")
		}
		ex.block.term = &blockTerm{kind: "branch", cond: cond, taken: taken, fall: fall}
		return true, nil
	}
	if taken < s.va {
		if s.loops == nil {
			s.loops = map[uint32]int{}
		}
		s.loops[s.va]++
		if s.loops[s.va] > maxLoopUnroll {
			ex.loopCut = true
			s.guards = append(s.guards, mkNot(cond))
			s.guardPos = append(s.guardPos, len(s.effects))
			s.bindFrom(mkNot(cond))
			s.va = fall
			return false, nil
		}
	}
	alt := s.clone()
	alt.guards = append(alt.guards, mkNot(cond))
	alt.guardPos = append(alt.guardPos, len(alt.effects))
	alt.bindFrom(mkNot(cond))
	alt.va = fall
	if err := ex.run(alt); err != nil {
		return true, err
	}
	s.guards = append(s.guards, cond)
	s.guardPos = append(s.guardPos, len(s.effects))
	s.bindFrom(cond)
	s.va = taken
	return false, nil
}

// Continues at the table entry a constant index selects, or forks a path per entry under a guard on a symbolic index
func (ex *Executor) switchJump(s *execState, index Expr, targets []uint32) (bool, error) {
	if c, ok := isConst(index); ok {
		if int(c) >= len(targets) {
			return false, fmt.Errorf("switch index %d outside its %d entry table", c, len(targets))
		}
		s.va = targets[c]
		return false, nil
	}
	if ex.block != nil {
		if len(s.frames) > 0 {
			return false, fmt.Errorf("inlined helper switches inside a lifted block")
		}
		ex.block.term = &blockTerm{kind: "switch", index: index, targets: targets}
		return true, nil
	}
	for k := 0; k+1 < len(targets); k++ {
		alt := s.clone()
		cond := mkCond("eq", index, IConst{uint32(k)})
		alt.guards = append(alt.guards, cond)
		alt.guardPos = append(alt.guardPos, len(alt.effects))
		alt.bindFrom(cond)
		alt.va = targets[k]
		if err := ex.run(alt); err != nil {
			return true, err
		}
		s.guards = append(s.guards, mkNot(cond))
		s.guardPos = append(s.guardPos, len(s.effects))
	}
	last := len(targets) - 1
	cond := mkCond("eq", index, IConst{uint32(last)})
	s.guards = append(s.guards, cond)
	s.guardPos = append(s.guardPos, len(s.effects))
	s.bindFrom(cond)
	s.va = targets[last]
	return false, nil
}

// Fixes an atomic value when a guard pins it to a constant
func (s *execState) bindFrom(cond Expr) {
	c, ok := cond.(Cond)
	if !ok || c.Op != "eq" {
		return
	}
	atom, value := c.A, c.B
	if _, isC := isConst(value); !isC {
		atom, value = c.B, c.A
	}
	if _, isC := isConst(value); !isC || !isAtomic(atom) {
		return
	}
	if s.bound == nil {
		s.bound = map[string]Expr{}
	}
	key := ExprString(atom)
	s.bound[key] = value
	if l, ok := atom.(Load); ok && l.Addr == nil {
		if s.boundLoads == nil {
			s.boundLoads = map[string]Load{}
		}
		s.boundLoads[key] = l
	}
}

// Frees the values guards pinned to loads of memory a write or a call makes unknown again
func (s *execState) unbind(base string, start, end int64) {
	for k, l := range s.boundLoads {
		if l.Base == base && int64(l.Off)+int64(l.Size) > start && int64(l.Off) < end {
			delete(s.bound, k)
			delete(s.boundLoads, k)
		}
	}
}

func isAtomic(e Expr) bool {
	switch e.(type) {
	case Sym, Load, CallResult:
		return true
	}
	return false
}

// Replaces bound atomic values inside an expression
func (s *execState) substitute(e Expr) Expr {
	if len(s.bound) == 0 || e == nil {
		return e
	}
	if isAtomic(e) {
		if v, ok := s.bound[ExprString(e)]; ok {
			return v
		}
	}
	switch v := e.(type) {
	case Bin:
		if strings.HasPrefix(v.Op, "f") {
			return mkFBin(v.Op, s.substitute(v.A), s.substitute(v.B))
		}
		return mkBin(v.Op, s.substitute(v.A), s.substitute(v.B))
	case Un:
		return mkUn(v.Op, s.substitute(v.A))
	}
	return e
}

// Counts the populated operands of an instruction
func argCount(in *Instruction) int {
	n := 0
	for _, a := range in.Inst.Args {
		if a != nil {
			n++
		}
	}
	return n
}

func (ex *Executor) step(s *execState, in *Instruction, next uint32) (bool, error) {
	args := in.Inst.Args
	nargs := argCount(in)
	op := in.Inst.Op
	s.va = next
	switch op {
	case x86asm.NOP, x86asm.FWAIT, x86asm.FLDCW, x86asm.FNSTCW, x86asm.FNCLEX, x86asm.FNINIT:
		return false, nil
	case x86asm.WBINVD:
		// Writing the caches back leaves memory holding what the stores already put there
		return false, nil
	case x86asm.INT, x86asm.HLT, x86asm.UD2:
		// A path that traps, such as a failed stack cookie check, never completes and is abandoned.
		if ex.block != nil && len(s.frames) == 0 {
			ex.block.term = &blockTerm{kind: "trap"}
			s.va = 0
		}
		return true, nil
	case x86asm.MOV, x86asm.MOVZX, x86asm.MOVSX:
		size := opSize(in, args[1])
		v := ex.operand(s, in, args[1], size)
		if op == x86asm.MOVSX {
			if size == 1 {
				v = mkUn("sx8", v)
			} else {
				v = mkUn("sx16", v)
			}
		}
		if op != x86asm.MOV {
			size = opSize(in, args[0])
		}
		// Linking the stack into the exception chain marks everything pushed so far as the registration record
		if m, ok := args[0].(x86asm.Mem); ok && m.Segment == x86asm.FS && m.Disp == 0 {
			if r, ok := args[1].(x86asm.Reg); ok && r == x86asm.ESP {
				s.pushes = 0
			}
		}
		ex.setOperand(s, in, args[0], opSize(in, args[0]), maskSize(v, opSize(in, args[0])))
		_ = size
	case x86asm.LEA:
		ex.setOperand(s, in, args[0], 4, ex.addr(s, args[1].(x86asm.Mem)))
	case x86asm.PUSH:
		v := ex.operand(s, in, args[0], 4)
		s.push(v)
		// A lifted block leaves what it pushes in the frame for the blocks after it
		if ex.block != nil {
			s.effects = append(s.effects, Effect{Kind: "store", VA: in.VA, Base: "stack", Off: stackOff(s), Size: 4, Value: v})
		}
		if !ex.savePush[in.VA] && !ex.savesRegister(s, in, args[0], v) {
			s.pushes++
		} else {
			s.lastSave = true
			// A saved register bounds the locals below it, the push of ecx being a local itself
			if r, isReg := args[0].(x86asm.Reg); isReg && r != x86asm.ECX {
				key := memKey{stackBase(s), stackOff(s)}
				mv := s.mem[key]
				mv.frame = true
				s.mem[key] = mv
			}
		}
	case x86asm.POP:
		v := ex.pop(s)
		ex.setOperand(s, in, args[0], 4, v)
	case x86asm.LEAVE:
		s.regs[4] = s.regs[5]
		s.regs[5] = ex.pop(s)
	case x86asm.ADD, x86asm.SUB, x86asm.AND, x86asm.OR, x86asm.XOR, x86asm.ADC, x86asm.SBB:
		size := opSize(in, args[0])
		a := ex.operand(s, in, args[0], size)
		b := ex.operand(s, in, args[1], size)
		if op == x86asm.ADC || op == x86asm.SBB {
			cf, ok := carryOf(s.fl)
			if !ok {
				return false, fmt.Errorf("carry arithmetic after %s flags", s.fl.kind)
			}
			// sbb r, r materializes the borrow alone, as all ones or zero
			if op == x86asm.SBB && sameArg(args[0], args[1]) {
				r := maskSize(mkUn("neg", cf), size)
				s.fl = narrowFlags(flagState{kind: "alu", a: r, b: IConst{0}, r: r}, size)
				ex.setOperand(s, in, args[0], size, r)
				return false, nil
			}
			name := "add"
			if op == x86asm.SBB {
				name = "sub"
			}
			r := maskSize(mkBin(name, mkBin(name, a, b), cf), size)
			s.fl = narrowFlags(flagState{kind: "alu", a: a, b: b, r: r}, size)
			ex.setOperand(s, in, args[0], size, r)
			return false, nil
		}
		name := map[x86asm.Op]string{x86asm.ADD: "add", x86asm.SUB: "sub", x86asm.AND: "and", x86asm.OR: "or", x86asm.XOR: "xor"}[op]
		var r Expr
		if op == x86asm.XOR && sameArg(args[0], args[1]) {
			r = IConst{0}
		} else {
			r = maskSize(mkBin(name, a, b), size)
		}
		s.fl = narrowFlags(flagState{kind: name, a: a, b: b, r: r}, size)
		ex.setOperand(s, in, args[0], size, r)
	case x86asm.INC, x86asm.DEC, x86asm.NEG, x86asm.NOT:
		size := opSize(in, args[0])
		a := ex.operand(s, in, args[0], size)
		var r Expr
		switch op {
		case x86asm.INC:
			r = mkBin("add", a, IConst{1})
		case x86asm.DEC:
			r = mkBin("sub", a, IConst{1})
		case x86asm.NEG:
			r = mkUn("neg", a)
		default:
			r = mkUn("not", a)
		}
		r = maskSize(r, size)
		if op != x86asm.NOT {
			cf, _ := carryOf(s.fl)
			if op == x86asm.NEG {
				cf = mkCond("ne", a, IConst{0})
			}
			s.fl = narrowFlags(flagState{kind: "alu", a: a, b: IConst{0}, r: r, cf: cf}, size)
		}
		ex.setOperand(s, in, args[0], size, r)
	case x86asm.SHL, x86asm.SHR, x86asm.SAR:
		size := opSize(in, args[0])
		a := ex.operand(s, in, args[0], size)
		var b Expr = IConst{1}
		if nargs > 1 {
			b = ex.operand(s, in, args[1], 1)
		}
		name := map[x86asm.Op]string{x86asm.SHL: "shl", x86asm.SHR: "shr", x86asm.SAR: "sar"}[op]
		if name == "sar" && size < 4 {
			a = mkUn(map[int]string{1: "sx8", 2: "sx16"}[size], a)
		}
		r := maskSize(mkBin(name, a, b), size)
		// The carry is the last bit shifted out, which a following adc shifts back in
		var cf Expr
		if name == "shl" {
			cf = mkBin("and", mkBin("shr", a, mkBin("sub", IConst{uint32(8 * size)}, b)), IConst{1})
		} else {
			cf = mkBin("and", mkBin("shr", a, mkBin("sub", b, IConst{1})), IConst{1})
		}
		s.fl = narrowFlags(flagState{kind: "alu", a: a, b: b, r: r, cf: cf}, size)
		ex.setOperand(s, in, args[0], size, r)
	case x86asm.IMUL, x86asm.MUL:
		size := opSize(in, args[0])
		switch nargs {
		case 1:
			// The widening product lands in edx:eax.
			if size != 4 {
				return false, fmt.Errorf("narrow widening multiply unsupported")
			}
			a := s.getReg(x86asm.EAX)
			b := ex.operand(s, in, args[0], 4)
			hi := "imulhi"
			if op == x86asm.MUL {
				hi = "mulhi"
			}
			s.setReg(x86asm.EDX, mkBin(hi, a, b))
			s.setReg(x86asm.EAX, mkBin("mul", a, b))
		case 2:
			a := ex.operand(s, in, args[0], size)
			b := ex.operand(s, in, args[1], size)
			ex.setOperand(s, in, args[0], size, mkBin("mul", a, b))
		default:
			a := ex.operand(s, in, args[1], size)
			b := ex.operand(s, in, args[2], size)
			ex.setOperand(s, in, args[0], size, mkBin("mul", a, b))
		}
	case x86asm.CMOVA, x86asm.CMOVAE, x86asm.CMOVB, x86asm.CMOVBE, x86asm.CMOVE, x86asm.CMOVG, x86asm.CMOVGE, x86asm.CMOVL, x86asm.CMOVLE, x86asm.CMOVNE, x86asm.CMOVNS, x86asm.CMOVS, x86asm.CMOVP, x86asm.CMOVNP:
		size := opSize(in, args[0])
		kept := ex.operand(s, in, args[0], size)
		moved := ex.operand(s, in, args[1], size)
		ex.setOperand(s, in, args[0], size, mkSel(ex.condition(s, cmovJump[op]), moved, kept))
	case x86asm.SHRD, x86asm.SHLD:
		size := opSize(in, args[0])
		if size != 4 {
			return false, fmt.Errorf("narrow double shift unsupported")
		}
		dst := ex.operand(s, in, args[0], 4)
		src := ex.operand(s, in, args[1], 4)
		count := mkBin("and", ex.operand(s, in, args[2], 1), IConst{31})
		rest := mkBin("sub", IConst{32}, count)
		var r Expr
		if op == x86asm.SHRD {
			r = mkBin("or", mkBin("shr", dst, count), mkBin("shl", src, rest))
		} else {
			r = mkBin("or", mkBin("shl", dst, count), mkBin("shr", src, rest))
		}
		r = mkSel(mkCond("eq", count, IConst{0}), dst, r)
		var cf Expr
		if op == x86asm.SHRD {
			cf = mkBin("and", mkBin("shr", dst, mkBin("sub", count, IConst{1})), IConst{1})
		} else {
			cf = mkBin("and", mkBin("shr", dst, rest), IConst{1})
		}
		s.fl = flagState{kind: "alu", a: dst, b: count, r: r, cf: cf}
		ex.setOperand(s, in, args[0], 4, r)
	case x86asm.BSWAP:
		a := ex.operand(s, in, args[0], 4)
		r := mkBin("or", mkBin("or", mkBin("shl", a, IConst{24}), mkBin("shl", mkBin("and", a, IConst{0xFF00}), IConst{8})), mkBin("or", mkBin("and", mkBin("shr", a, IConst{8}), IConst{0xFF00}), mkBin("shr", a, IConst{24})))
		ex.setOperand(s, in, args[0], 4, r)
	case x86asm.ROL, x86asm.ROR:
		size := opSize(in, args[0])
		a := ex.operand(s, in, args[0], size)
		var count Expr = IConst{1}
		if nargs > 1 {
			count = ex.operand(s, in, args[1], 1)
		}
		bits := uint32(8 * size)
		count = mkBin("and", count, IConst{bits - 1})
		if _, isConst := isConst(count); !isConst && size != 4 {
			return false, fmt.Errorf("narrow rotate by a symbolic count unsupported")
		}
		rest := mkBin("sub", IConst{bits}, count)
		var r Expr
		if op == x86asm.ROL {
			r = mkBin("or", mkBin("shl", a, count), mkBin("shr", a, rest))
		} else {
			r = mkBin("or", mkBin("shr", a, count), mkBin("shl", a, rest))
		}
		r = maskSize(r, size)
		// The carry is the bit rotated around the end, the lowest after a left turn and the highest after a right one
		var cf Expr
		if op == x86asm.ROL {
			cf = mkBin("and", r, IConst{1})
		} else {
			cf = mkBin("and", mkBin("shr", r, IConst{bits - 1}), IConst{1})
		}
		s.fl = narrowFlags(flagState{kind: "alu", a: a, b: count, r: r, cf: cf}, size)
		ex.setOperand(s, in, args[0], size, r)
	case x86asm.BSF, x86asm.BSR:
		size := opSize(in, args[1])
		src := ex.operand(s, in, args[1], size)
		name := "bsf"
		if op == x86asm.BSR {
			name = "bsr"
		}
		// The zero flag reports a source without any set bit
		s.fl = flagState{kind: "cmp", a: src, b: IConst{0}, r: src}
		ex.setOperand(s, in, args[0], size, mkUn(name, src))
	case x86asm.CWDE:
		s.setReg(x86asm.EAX, mkUn("sx16", s.getReg(x86asm.EAX)))
	case x86asm.CBW:
		s.setReg(x86asm.AX, mkUn("sx8", s.getReg(x86asm.EAX)))
	case x86asm.CWD:
		s.setReg(x86asm.DX, mkBin("sar", mkUn("sx16", s.getReg(x86asm.EAX)), IConst{15}))
	case x86asm.TEST:
		size := opSize(in, args[0])
		a := ex.operand(s, in, args[0], size)
		b := ex.operand(s, in, args[1], size)
		s.fl = narrowFlags(flagState{kind: "test", a: a, b: b, r: mkBin("and", a, b)}, size)
		if r, ok := args[0].(x86asm.Reg); ok && (r == x86asm.AH || r == x86asm.AX) && s.axFSW && s.fcmp != nil {
			s.fl.kind = "fsw"
			s.fl.fsw = s.fcmp
		}
	case x86asm.CMP:
		size := opSize(in, args[0])
		a := ex.operand(s, in, args[0], size)
		b := ex.operand(s, in, args[1], size)
		if size < 4 {
			sx := map[int]string{1: "sx8", 2: "sx16"}[size]
			s.fl = flagState{kind: "cmp", a: mkUn(sx, a), b: mkUn(sx, b), r: mkBin("sub", a, b)}
		} else {
			s.fl = flagState{kind: "cmp", a: a, b: b, r: mkBin("sub", a, b)}
		}
	case x86asm.SAHF:
		if s.fcmp != nil {
			s.fl = flagState{kind: "fsw", fsw: s.fcmp}
			s.fl.a = IConst{0x45}
		}
	case x86asm.LAHF:
		if s.fl.kind == "comiss" {
			// The ordering flags land in ah exactly where fnstsw puts the condition codes.
			s.fcmp = &fcmpRec{a: s.fl.a, b: s.fl.b}
			s.regs[0] = Sym{"fsw"}
			s.axFSW = true
			return false, nil
		}
		if s.fl.kind == "" {
			return false, fmt.Errorf("lahf without flags")
		}
		zf := mkBin("shl", mkCond("eq", s.fl.r, IConst{0}), IConst{6})
		sf := mkBin("shl", mkCond("lt", s.fl.r, IConst{0}), IConst{7})
		var cf Expr = IConst{0}
		if s.fl.kind == "cmp" || s.fl.kind == "sub" {
			cf = mkCond("ult", s.fl.a, s.fl.b)
		}
		ah := mkBin("or", mkBin("or", zf, sf), mkBin("or", cf, IConst{2}))
		s.setReg(x86asm.AH, ah)
	case x86asm.IDIV, x86asm.DIV:
		size := opSize(in, args[0])
		if size != 4 {
			return false, fmt.Errorf("narrow division unsupported")
		}
		divisor := ex.operand(s, in, args[0], 4)
		lo := s.getReg(x86asm.EAX)
		hi := s.getReg(x86asm.EDX)
		lv, lok := isConst(lo)
		hv, hok := isConst(hi)
		dv, dok := isConst(divisor)
		if lok && hok && dok && dv != 0 {
			if op == x86asm.IDIV {
				n := int64(uint64(hv)<<32 | uint64(lv))
				s.setReg(x86asm.EAX, IConst{uint32(n / int64(int32(dv)))})
				s.setReg(x86asm.EDX, IConst{uint32(n % int64(int32(dv)))})
			} else {
				n := uint64(hv)<<32 | uint64(lv)
				s.setReg(x86asm.EAX, IConst{uint32(n / uint64(dv))})
				s.setReg(x86asm.EDX, IConst{uint32(n % uint64(dv))})
			}
		} else {
			name := "udiv"
			if op == x86asm.IDIV {
				name = "idiv"
			}
			s.setReg(x86asm.EAX, Bin{name, lo, divisor})
			s.setReg(x86asm.EDX, Bin{name + "_rem", lo, divisor})
		}
	case x86asm.CDQ:
		s.setReg(x86asm.EDX, mkBin("sar", s.getReg(x86asm.EAX), IConst{31}))
	case x86asm.XCHG:
		a := ex.operand(s, in, args[0], 4)
		b := ex.operand(s, in, args[1], 4)
		ex.setOperand(s, in, args[0], 4, b)
		ex.setOperand(s, in, args[1], 4, a)
	case x86asm.XADD:
		// The exchanging add of one thread, the console's others never racing it here
		size := opSize(in, args[0])
		a := ex.operand(s, in, args[0], size)
		b := ex.operand(s, in, args[1], size)
		r := maskSize(mkBin("add", a, b), size)
		s.fl = narrowFlags(flagState{kind: "add", a: a, b: b, r: r}, size)
		ex.setOperand(s, in, args[1], size, a)
		ex.setOperand(s, in, args[0], size, r)
	case x86asm.CMPXCHG:
		// The compare and exchange of one thread, swapping when the accumulator matches
		size := opSize(in, args[0])
		dst := ex.operand(s, in, args[0], size)
		src := ex.operand(s, in, args[1], size)
		acc := maskSize(s.getReg(x86asm.EAX), size)
		same := mkCond("eq", acc, dst)
		s.fl = narrowFlags(flagState{kind: "cmp", a: acc, b: dst, r: mkBin("sub", acc, dst)}, size)
		ex.setOperand(s, in, args[0], size, mkSel(same, src, dst))
		if size == 4 {
			s.setReg(x86asm.EAX, mkSel(same, acc, dst))
		} else {
			s.setReg(x86asm.EAX, mkBin("or", mkBin("and", s.getReg(x86asm.EAX), IConst{^uint32(0) << (8 * uint32(size))}), mkSel(same, acc, dst)))
		}
	case x86asm.SETE, x86asm.SETNE, x86asm.SETL, x86asm.SETGE, x86asm.SETLE, x86asm.SETG, x86asm.SETB, x86asm.SETAE, x86asm.SETBE, x86asm.SETA, x86asm.SETS, x86asm.SETNS, x86asm.SETP, x86asm.SETNP:
		c := ex.condition(s, op)
		ex.setOperand(s, in, args[0], 1, c)
	case x86asm.JMP:
		if rel, ok := args[0].(x86asm.Rel); ok {
			target := in.VA + uint32(in.Inst.Len) + uint32(int32(rel))
			if !ex.Opaque[target] && !(ex.block != nil && len(s.frames) == 0 && !ex.block.inside[target]) {
				s.va = target
				return false, nil
			}
			return ex.tailCall(s, in)
		}
		// A jump through a table of code addresses is a switch, forked over every case the index may select.
		if m, ok := args[0].(x86asm.Mem); ok && m.Index != 0 && m.Scale == 4 && m.Base == 0 {
			if sec := ex.d.sectionOf(in.VA); sec != nil {
				if targets := ex.d.jumpTableTargets(in, sec); len(targets) > 0 {
					return ex.switchJump(s, s.getReg(m.Index), targets)
				}
			}
		}
		return ex.tailCall(s, in)
	case x86asm.JE, x86asm.JNE, x86asm.JL, x86asm.JGE, x86asm.JLE, x86asm.JG, x86asm.JB, x86asm.JAE, x86asm.JBE, x86asm.JA, x86asm.JS, x86asm.JNS, x86asm.JP, x86asm.JNP:
		rel := args[0].(x86asm.Rel)
		taken := in.VA + uint32(in.Inst.Len) + uint32(int32(rel))
		return ex.branch(s, ex.condition(s, op), taken, next)
	case x86asm.JECXZ:
		rel := args[0].(x86asm.Rel)
		taken := in.VA + uint32(in.Inst.Len) + uint32(int32(rel))
		return ex.branch(s, mkCond("eq", s.getReg(x86asm.ECX), IConst{0}), taken, next)
	case x86asm.CALL:
		return false, ex.call(s, in, next)
	case x86asm.RET:
		clean := int32(0)
		if nargs > 0 {
			clean = int32(args[0].(x86asm.Imm))
		}
		return ex.doReturn(s, clean)
	case x86asm.STOSD, x86asm.STOSB, x86asm.STOSW:
		size := map[x86asm.Op]int{x86asm.STOSD: 4, x86asm.STOSB: 1, x86asm.STOSW: 2}[op]
		src := map[x86asm.Op]x86asm.Reg{x86asm.STOSD: x86asm.EAX, x86asm.STOSB: x86asm.AL, x86asm.STOSW: x86asm.AX}[op]
		count := uint32(1)
		if hasRep(in) {
			c, ok := isConst(s.getReg(x86asm.ECX))
			if !ok || c > maxRepUnroll {
				// A fill of unknown or long length is kept as an effect the runtime performs, the pointer moving past it.
				dst := s.getReg(x86asm.EDI)
				n := s.getReg(x86asm.ECX)
				s.effects = append(s.effects, Effect{Kind: "fill", VA: in.VA, Addr: dst, Size: size, Value: s.getReg(src), Args: []Expr{n}})
				s.setReg(x86asm.EDI, mkBin("add", dst, mkBin("mul", n, IConst{uint32(size)})))
				s.setReg(x86asm.ECX, IConst{0})
				break
			}
			count = c
		}
		v := s.getReg(src)
		for i := uint32(0); i < count; i++ {
			dst := s.getReg(x86asm.EDI)
			ex.store(s, dst, size, v, in.VA)
			s.setReg(x86asm.EDI, mkBin("add", dst, IConst{uint32(size)}))
		}
		if hasRep(in) {
			s.setReg(x86asm.ECX, IConst{0})
		}
	case x86asm.CMPSD, x86asm.CMPSB, x86asm.CMPSW:
		size := map[x86asm.Op]int{x86asm.CMPSD: 4, x86asm.CMPSB: 1, x86asm.CMPSW: 2}[op]
		bytes := size
		if hasRep(in) {
			c, ok := isConst(s.getReg(x86asm.ECX))
			if !ok {
				return false, fmt.Errorf("rep cmps with symbolic count")
			}
			if c > maxRepCompare {
				return false, fmt.Errorf("rep cmps count %d exceeds the unroll limit", c)
			}
			bytes = int(c) * size
		}
		a := ex.loadBlock(s, s.getReg(x86asm.ESI), bytes)
		b := ex.loadBlock(s, s.getReg(x86asm.EDI), bytes)
		s.fl = flagState{kind: "cmp", a: a, b: b, r: mkBin("sub", a, b)}
		// The compare stops at the first word that differs, so where it leaves the pointers follows the data
		if hasRep(in) {
			ex.seq++
			for _, r := range []x86asm.Reg{x86asm.ESI, x86asm.EDI, x86asm.ECX} {
				s.setReg(r, Sym{fmt.Sprintf("clobber.%s#%d", r, ex.seq)})
			}
		}
	case x86asm.MOVSD, x86asm.MOVSB, x86asm.MOVSW:
		size := map[x86asm.Op]int{x86asm.MOVSD: 4, x86asm.MOVSB: 1, x86asm.MOVSW: 2}[op]
		if hasRep(in) {
			count := s.getReg(x86asm.ECX)
			cnt, ok := isConst(count)
			if copied, isCopy := ex.transformCopyAt(s, in, cnt, ok, size); isCopy {
				if copied {
					break
				}
			}
			if !ok || cnt > maxRepUnroll {
				// A copy of unknown or long length is kept as an effect the runtime performs, the pointers moving past it.
				dst, src := s.getReg(x86asm.EDI), s.getReg(x86asm.ESI)
				base := "?"
				off := int32(0)
				if p, isPtr := asPtr(dst).(Ptr); isPtr {
					base, off = p.Base, p.Off
				}
				s.effects = append(s.effects, Effect{Kind: "copy", VA: in.VA, Base: base, Off: off, Addr: dst, Size: size, Value: count, Args: []Expr{src}})
				advance := mkBin("mul", count, IConst{uint32(size)})
				s.setReg(x86asm.EDI, mkBin("add", dst, advance))
				s.setReg(x86asm.ESI, mkBin("add", src, advance))
				s.setReg(x86asm.ECX, IConst{0})
				break
			}
			for i := uint32(0); i < cnt; i++ {
				ex.movs(s, in, size)
			}
			s.setReg(x86asm.ECX, IConst{0})
		} else {
			ex.movs(s, in, size)
		}
	case x86asm.IN, x86asm.OUT:
		// A port access reaches the console's hardware, kept as a call the runtime's hardware answers
		data, port := in.Inst.Args[0], in.Inst.Args[1]
		if op == x86asm.OUT {
			data, port = port, data
		}
		r, ok := data.(x86asm.Reg)
		if !ok {
			return false, fmt.Errorf("port access through %v", data)
		}
		size, _ := regSize(r)
		ex.seq++
		at := ex.operand(s, in, port, 2)
		if op == x86asm.OUT {
			s.effects = append(s.effects, Effect{Kind: "call", VA: in.VA, Fn: portOut, Args: []Expr{at, ex.operand(s, in, data, size), IConst{uint32(size)}}, Seq: ex.seq})
			break
		}
		args := []Expr{at, IConst{uint32(size)}}
		s.effects = append(s.effects, Effect{Kind: "call", VA: in.VA, Fn: portIn, Args: args, Seq: ex.seq})
		ex.setOperand(s, in, data, size, CallResult{Fn: portIn, Args: args, Seq: ex.seq})
	default:
		if isFPU(op) {
			return false, ex.fpuStep(s, in)
		}
		if handled, err := ex.sseStep(s, in); handled {
			return false, err
		}
		return false, fmt.Errorf("unsupported instruction")
	}
	return false, nil
}

// The calls a port read and a port write become
const portIn, portOut = "port_in", "port_out"

// The name a call to a routine is recorded under, its address where the signature database could only give it a name
// several routines share
func calleeName(fn *Function) string {
	if fn.Symbol == nil || strings.HasPrefix(fn.Symbol.Name, "Collisions_") {
		return fmt.Sprintf("sub_%08X", fn.EntryVA)
	}
	return fn.Symbol.Name
}

// Reads a matrix copy straight into or out of the device's transform array as the inlined transform setter or
// getter, reporting whether the copy was one and whether it was taken that way
func (ex *Executor) transformCopyAt(s *execState, in *Instruction, cnt uint32, known bool, size int) (bool, bool) {
	if !known {
		return false, false
	}
	if state, ok := ex.transformCopy(s, s.getReg(x86asm.EDI), cnt*uint32(size)); ok {
		src := s.getReg(x86asm.ESI)
		ex.seq++
		s.effects = append(s.effects, Effect{Kind: "call", VA: in.VA, Fn: "D3DDevice_SetTransform", Args: []Expr{IConst{state}, src}, Seq: ex.seq})
		advance := IConst{cnt * uint32(size)}
		s.setReg(x86asm.EDI, mkBin("add", s.getReg(x86asm.EDI), advance))
		s.setReg(x86asm.ESI, mkBin("add", src, advance))
		s.setReg(x86asm.ECX, IConst{0})
		return true, true
	}
	if state, ok := ex.transformCopy(s, s.getReg(x86asm.ESI), cnt*uint32(size)); ok {
		dst := s.getReg(x86asm.EDI)
		ex.seq++
		s.effects = append(s.effects, Effect{Kind: "call", VA: in.VA, Fn: "D3DDevice_GetTransform", Args: []Expr{IConst{state}, dst}, Seq: ex.seq})
		if p, ok := asPtr(dst).(Ptr); ok {
			ex.store(s, p, int(cnt*uint32(size)), CallResult{Fn: "D3DDevice_GetTransform", Args: []Expr{IConst{state}}, Seq: ex.seq}, in.VA)
		}
		advance := IConst{cnt * uint32(size)}
		s.setReg(x86asm.EDI, mkBin("add", dst, advance))
		s.setReg(x86asm.ESI, mkBin("add", s.getReg(x86asm.ESI), advance))
		s.setReg(x86asm.ECX, IConst{0})
		return true, true
	}
	return false, false
}

func hasRep(in *Instruction) bool {
	for _, p := range in.Inst.Prefix {
		if p&0xFF == 0xF3 || p&0xFF == 0xF2 {
			return true
		}
	}
	return false
}

func sameArg(a, b x86asm.Arg) bool {
	ra, ok1 := a.(x86asm.Reg)
	rb, ok2 := b.(x86asm.Reg)
	return ok1 && ok2 && ra == rb
}

func (ex *Executor) movs(s *execState, in *Instruction, size int) {
	src := s.getReg(x86asm.ESI)
	dst := s.getReg(x86asm.EDI)
	v := ex.load(s, src, size)
	ex.store(s, dst, size, v, in.VA)
	s.setReg(x86asm.ESI, mkBin("add", src, IConst{uint32(size)}))
	s.setReg(x86asm.EDI, mkBin("add", dst, IConst{uint32(size)}))
}

// Builds the predicate a conditional jump tests from the last flag producer
func (ex *Executor) condition(s *execState, op x86asm.Op) Expr {
	f := s.fl
	if f.kind == "fsw" {
		return ex.fswCondition(s, op)
	}
	if f.kind == "comiss" {
		return ex.comissCondition(s, op)
	}
	zero := func() Expr { return mkCond("eq", f.r, IConst{0}) }
	sign := func() Expr { return mkCond("lt", f.r, IConst{0}) }
	// The unsigned conditions of anything but a compare read the carry the instruction left, as a flag of zero or one
	carry := func() (Expr, bool) {
		if f.kind == "cmp" || f.kind == "sub" {
			return nil, false
		}
		cf, ok := carryOf(f)
		if !ok {
			return nil, false
		}
		return mkCond("ne", cf, IConst{0}), true
	}
	switch op {
	case x86asm.JE, x86asm.SETE:
		if f.kind == "cmp" {
			return mkCond("eq", f.a, f.b)
		}
		return zero()
	case x86asm.JNE, x86asm.SETNE:
		if f.kind == "cmp" {
			return mkCond("ne", f.a, f.b)
		}
		return mkNot(zero())
	case x86asm.JL, x86asm.SETL:
		if f.kind == "cmp" || f.kind == "sub" {
			return mkCond("lt", f.a, f.b)
		}
		return sign()
	case x86asm.JGE, x86asm.SETGE:
		if f.kind == "cmp" || f.kind == "sub" {
			return mkCond("ge", f.a, f.b)
		}
		return mkNot(sign())
	case x86asm.JLE, x86asm.SETLE:
		if f.kind == "cmp" || f.kind == "sub" {
			return mkCond("le", f.a, f.b)
		}
		return mkCond("le", f.r, IConst{0})
	case x86asm.JG, x86asm.SETG:
		if f.kind == "cmp" || f.kind == "sub" {
			return mkCond("gt", f.a, f.b)
		}
		return mkCond("gt", f.r, IConst{0})
	case x86asm.JB, x86asm.SETB:
		if cf, ok := carry(); ok {
			return cf
		}
		return mkCond("ult", f.a, f.b)
	case x86asm.JAE, x86asm.SETAE:
		if cf, ok := carry(); ok {
			return mkNot(cf)
		}
		return mkCond("uge", f.a, f.b)
	case x86asm.JBE, x86asm.SETBE:
		if cf, ok := carry(); ok {
			return mkBin("or", cf, zero())
		}
		return mkCond("ule", f.a, f.b)
	case x86asm.JA, x86asm.SETA:
		if cf, ok := carry(); ok {
			return mkNot(mkBin("or", cf, zero()))
		}
		return mkCond("ugt", f.a, f.b)
	case x86asm.JS, x86asm.SETS:
		return sign()
	case x86asm.JNS, x86asm.SETNS:
		return mkNot(sign())
	}
	panic(fmt.Sprintf("condition %v", op))
}

// Resolves a jump after fnstsw/test ah by enumerating float compare outcomes
func (ex *Executor) fswCondition(s *execState, op x86asm.Op) Expr {
	f := s.fl
	mask, _ := isConst(f.b)
	if f.a != nil {
		if m, ok := isConst(f.a); ok && f.b == nil {
			mask = m
		}
	}
	if r, ok := f.b.(IConst); ok {
		mask = r.V
	}
	var set uint8
	outcomes := []struct {
		bit uint8
		c0  uint32
		c2  uint32
		c3  uint32
	}{{1, 1, 0, 0}, {2, 0, 0, 1}, {4, 0, 0, 0}, {8, 1, 1, 1}}
	for _, o := range outcomes {
		ah := o.c0 | o.c2<<2 | o.c3<<6
		r := ah & mask
		zf := r == 0
		pf := parity8(r)
		sf := r&0x80 != 0
		taken := false
		switch op {
		case x86asm.JE, x86asm.SETE:
			taken = zf
		case x86asm.JNE, x86asm.SETNE:
			taken = !zf
		case x86asm.JP, x86asm.SETP:
			taken = pf
		case x86asm.JNP, x86asm.SETNP:
			taken = !pf
		case x86asm.JS, x86asm.SETS:
			taken = sf
		case x86asm.JNS, x86asm.SETNS:
			taken = !sf
		case x86asm.JBE, x86asm.SETBE, x86asm.JA, x86asm.SETA, x86asm.JB, x86asm.SETB, x86asm.JAE, x86asm.SETAE:
			// After sahf the flags come straight from the status word.
			cf := o.c0 != 0
			zfs := o.c3 != 0
			switch op {
			case x86asm.JBE, x86asm.SETBE:
				taken = cf || zfs
			case x86asm.JA, x86asm.SETA:
				taken = !cf && !zfs
			case x86asm.JB, x86asm.SETB:
				taken = cf
			default:
				taken = !cf
			}
		default:
			panic(fmt.Sprintf("fsw condition %v", op))
		}
		if taken {
			set |= o.bit
		}
	}
	return mkFCmp(f.fsw.a, f.fsw.b, set)
}

func parity8(v uint32) bool {
	n := 0
	for i := 0; i < 8; i++ {
		if v>>i&1 != 0 {
			n++
		}
	}
	return n%2 == 0
}

// Handles direct, import, library and inlined calls
func (ex *Executor) call(s *execState, in *Instruction, next uint32) error {
	ex.callee = 0
	nargs := argCount(in)
	_ = nargs
	var target uint32
	var obj Expr
	slot := int32(-1)
	switch a := in.Inst.Args[0].(type) {
	case x86asm.Rel:
		target = in.VA + uint32(in.Inst.Len) + uint32(int32(a))
	case x86asm.Mem:
		addrE := asPtr(ex.addr(s, a))
		if p, ok := addrE.(Ptr); ok && p.Base == "" {
			if name, ok := ex.d.Imports[uint32(p.Off)]; ok {
				return ex.importCall(s, in, next, name)
			}
			v := ex.load(s, addrE, 4)
			if c, ok := isConst(v); ok {
				target = c
			} else if a.Base != 0 {
				obj = s.getReg(a.Base)
				slot = int32(a.Disp)
			}
		} else if p, ok := addrE.(Ptr); ok && p.Base == "stack" {
			// A call through a function pointer kept in a local goes where the pointer points
			v := ex.load(s, addrE, 4)
			if c, ok := isConst(v); ok {
				target = c
			} else {
				return ex.pointerCall(s, in, next, v)
			}
		} else if a.Base != 0 && a.Index == 0 {
			obj = s.getReg(a.Base)
			slot = int32(a.Disp)
			if vt := ex.vtableFor(s, obj); vt != 0 {
				if fp, ok := ex.d.Image.ReadU32(vt + uint32(slot)); ok {
					target = fp
				}
			}
			// A call through a register holding anything but an object's first word goes where the pointer stored
			// at that offset points, a virtual call reading its vtable from the object first
			if target == 0 {
				if vtable, known := ex.d.callReadsVtable(ex.d.Functions[s.currentFunction()], in.VA, a.Base); known && !vtable {
					return ex.pointerCall(s, in, next, ex.load(s, addrE, 4))
				}
			}
		} else if a.Index != 0 {
			// A call through a table entry the routine indexes goes where the entry points
			return ex.pointerCall(s, in, next, ex.load(s, addrE, 4))
		}
	case x86asm.Reg:
		v := s.getReg(a)
		if c, ok := isConst(v); ok {
			target = c
		}
		if p, ok := v.(Ptr); ok && strings.HasPrefix(p.Base, "import:") {
			return ex.importCall(s, in, next, strings.TrimPrefix(p.Base, "import:"))
		}
		if target == 0 {
			return ex.pointerCall(s, in, next, v)
		}
	}
	if target == 0 {
		if obj != nil {
			return ex.opaqueCall(s, in, next, fmt.Sprintf("vcall@%X", slot), -1, false)
		}
		return fmt.Errorf("unresolved call target")
	}
	if name, ok := ex.d.Imports[target]; ok {
		return ex.importCall(s, in, next, name)
	}
	fn, ok := ex.d.Functions[target]
	if !ok {
		// Virtual table entries only reached through calls are discovered on demand.
		if ex.d.InsnByVA[target] == nil {
			return fmt.Errorf("call to undiscovered function %08X", target)
		}
		fn = ex.d.buildFunction(target, map[uint32]bool{})
		if fn == nil {
			return fmt.Errorf("call to undiscovered function %08X", target)
		}
		ex.d.Functions[target] = fn
	}
	// A thunk jumping straight to an import is that import, its arguments still on the caller's stack
	if name, ok := ex.d.importThunk(fn); ok {
		return ex.importCall(s, in, next, name)
	}
	if ex.probe[target] {
		// The probe reserves the frame its caller sized in eax, leaving the return address on top of it
		size, ok := isConst(s.getReg(x86asm.EAX))
		if !ok {
			return fmt.Errorf("stack probe at %08X reserves a frame of no fixed size", in.VA)
		}
		s.pushes = 0
		s.regs[4] = mkBin("add", s.regs[4], IConst{-size})
		s.va = next
		return nil
	}
	if ex.ftol[target] {
		s.pushes = 0
		v := s.fpuPop()
		s.setReg(x86asm.EAX, mkUn("f2i", v))
		s.setReg(x86asm.EDX, Sym{"ftol.hi"})
		return nil
	}
	if m, ok := ex.d.mathIntrinsic(target); ok {
		// The runtime math thunks take their operands on the x87 stack, the first argument below the top.
		s.pushes = 0
		s.pending = nil
		y := s.fpuPop()
		if m.arity == 1 {
			s.fpuPush(mkUn(m.op, y))
		} else {
			x := s.fpuPop()
			s.fpuPush(mkFBin(m.op, x, y))
		}
		s.setReg(x86asm.EAX, Sym{fmt.Sprintf("clobber.eax#%d", ex.seq)})
		s.setReg(x86asm.ECX, Sym{fmt.Sprintf("clobber.ecx#%d", ex.seq)})
		s.setReg(x86asm.EDX, Sym{fmt.Sprintf("clobber.edx#%d", ex.seq)})
		ex.seq++
		return nil
	}
	// A C runtime math function linked as a library routine takes its doubles on the stack and answers on the x87 stack
	if fn.Symbol != nil && fn.Symbol.Lib == "libcmt" {
		if m, ok := mathIntrinsics[fn.Symbol.Name]; ok {
			s.pushes = 0
			s.pending = nil
			x := ex.load(s, s.regs[4], 8)
			if m.arity == 1 {
				s.fpuPush(mkUn(m.op, x))
			} else {
				y := ex.load(s, mkBin("add", s.regs[4], IConst{8}), 8)
				s.fpuPush(mkFBin(m.op, x, y))
			}
			s.setReg(x86asm.EAX, Sym{fmt.Sprintf("clobber.eax#%d", ex.seq)})
			s.setReg(x86asm.ECX, Sym{fmt.Sprintf("clobber.ecx#%d", ex.seq)})
			s.setReg(x86asm.EDX, Sym{fmt.Sprintf("clobber.edx#%d", ex.seq)})
			ex.seq++
			return nil
		}
	}
	if sym := fn.Symbol; sym != nil && obj != nil {
		// A library method reached through a vtable stays a virtual call, its arity now known.
		argBytes := sym.ArgBytes
		if argBytes < 0 && !sym.FastCall {
			if clean := ex.d.cleanupOf(fn); clean > 0 || !sym.Cdecl {
				argBytes = clean
			}
		}
		return ex.opaqueCall(s, in, next, fmt.Sprintf("vcall@%X", slot), argBytes, false)
	}
	if ex.Opaque[target] || (ex.block != nil && !(fn.Symbol != nil && inlineSymbols[fn.Symbol.Name])) {
		// A helper that traps as soon as it runs ends the block, the executable never taking that way
		if ex.block != nil && len(s.frames) == 0 && ex.d.trapsOnEntry(fn) {
			ex.block.term = &blockTerm{kind: "trap"}
			s.va = 0
			return nil
		}
		name := fmt.Sprintf("sub_%08X", target)
		argBytes := ex.d.cleanupOf(fn)
		fast := false
		cdecl := false
		if fn.Symbol != nil {
			name, argBytes, fast, cdecl = calleeName(fn), fn.Symbol.ArgBytes, fn.Symbol.FastCall, fn.Symbol.Cdecl
			// A symbol named without its arity whose body cleans the stack is called the way its body says
			if argBytes < 0 && !fast {
				if clean := ex.d.cleanupOf(fn); clean > 0 {
					argBytes, cdecl = clean, false
				}
			}
		}
		// A tail jump hands the callee the routine's own arguments, which its body counts where its name does not
		if ex.tail && argBytes < 0 && !fast {
			argBytes = ex.tailArgBytes(target, argBytes)
		}
		eax, ecx, edx := s.regs[0], s.regs[1], s.regs[2]
		if fn.Symbol != nil {
			ex.callee = target
		}
		if fn.Symbol != nil || (ex.block != nil && ex.d.inLibrary(target)) {
			if done, err := ex.conventionCall(s, in, next, fn); err != nil {
				return err
			} else if done {
				ex.keepUnwritten(s, target, eax, ecx, edx)
				return nil
			}
		}
		if fn.Symbol == nil {
			ex.callTarget = target
		}
		ex.callReceiver = ex.d.readsReceiver(fn)
		if err := ex.opaqueCall(s, in, next, name, argBytes, fast, cdecl); err != nil {
			return err
		}
		ex.keepUnwritten(s, target, eax, ecx, edx)
		if r, ok := ex.Results[target]; ok {
			s.setReg(x86asm.EAX, r)
		} else if ex.allocHook != nil && ex.Allocators[target] {
			s.setReg(x86asm.EAX, ex.allocHook())
		}
		return nil
	}
	if sym := fn.Symbol; sym != nil {
		if inlineSymbols[sym.Name] {
			if next != 0 {
				s.push(RetAddr{VA: next})
			}
			s.pending = nil
			s.pushes = 0
			s.frames = append(s.frames, target)
			s.fsteps = append(s.fsteps, 0)
			s.entry = append(s.entry, s.regs)
			s.va = target
			return nil
		}
		argBytes := sym.ArgBytes
		cdecl := sym.Cdecl
		if argBytes < 0 && !sym.FastCall {
			// The callee's own ret immediate fixes the arity of a symbol named without it.
			if clean := ex.d.cleanupOf(fn); clean > 0 {
				argBytes, cdecl = clean, false
			}
		}
		// A tail jump hands the callee the routine's own arguments, which its body counts where its name does not
		if ex.tail && argBytes < 0 && !sym.FastCall {
			argBytes = ex.tailArgBytes(target, argBytes)
		}
		eax, ecx, edx := s.regs[0], s.regs[1], s.regs[2]
		ex.callee = target
		if done, err := ex.conventionCall(s, in, next, fn); err != nil {
			return err
		} else if done {
			ex.keepUnwritten(s, target, eax, ecx, edx)
			return nil
		}
		ex.callTarget = target
		ex.callReceiver = ex.d.readsReceiver(fn)
		if err := ex.opaqueCall(s, in, next, calleeName(fn), argBytes, sym.FastCall, cdecl); err != nil {
			return err
		}
		ex.keepUnwritten(s, target, eax, ecx, edx)
		return nil
	}
	if ex.d.inLibrary(target) {
		// Unnamed library code stays a call whose register inputs its body reveals, never inlined into dashboard paths.
		eax, ecx, edx := s.regs[0], s.regs[1], s.regs[2]
		if done, err := ex.conventionCall(s, in, next, fn); err != nil {
			return err
		} else if done {
			ex.keepUnwritten(s, target, eax, ecx, edx)
			return nil
		}
	}
	if len(fn.Instructions) > maxInlineSize {
		return fmt.Errorf("callee %08X too large to inline", target)
	}
	if next != 0 {
		s.push(RetAddr{VA: next})
	}
	if s.pending != nil {
		// The pending call keeps whatever it did not consume below this frame.
		s.pending = nil
	}
	s.pushes = 0
	s.frames = append(s.frames, target)
	s.fsteps = append(s.fsteps, 0)
	s.entry = append(s.entry, s.regs)
	s.va = target
	return nil
}

// The call effect name standing for a call through a function pointer, the pointer being its first argument
const pointerCallName = "fptr"

// The function whose code the state is executing, the innermost inlined frame or the routine itself
func (s *execState) currentFunction() uint32 {
	if len(s.frames) > 0 {
		return s.frames[len(s.frames)-1]
	}
	return s.root
}

// Instructions the look back from a call walks before giving up on finding what loaded its base register
const vtableScanLimit = 96

// Reports whether the register a call goes through was last loaded from the first word of an object, the way a
// virtual call reads its vtable, and whether the straight line code before the call shows what loaded it at all
func (d *Disassembly) callReadsVtable(fn *Function, callVA uint32, base x86asm.Reg) (vtable, known bool) {
	if fn == nil {
		return false, false
	}
	index := fn.instructionIndex()
	i, ok := index[callVA]
	if !ok {
		return false, false
	}
	base = wideRegister(base)
	for j := i - 1; j >= 0 && i-j <= vtableScanLimit; j-- {
		in := &fn.Instructions[j]
		// A return or an unconditional jump never falls through to the instruction after it
		if in.Inst.Op == x86asm.RET || in.Inst.Op == x86asm.JMP {
			return false, false
		}
		if !slices.Contains(writtenRegisters(in), base) {
			continue
		}
		if in.Inst.Op == x86asm.MOV {
			if m, isMem := in.Inst.Args[1].(x86asm.Mem); isMem && m.Base != 0 && m.Index == 0 && m.Disp == 0 && m.Segment == 0 {
				return true, true
			}
		}
		return false, true
	}
	return false, false
}

// Records a call through a function pointer the path holds symbolically, the runtime following the pointer, the
// caller's own cleanup after the call fixing the arity of a routine the caller cleans up after
func (ex *Executor) pointerCall(s *execState, in *Instruction, next uint32, fp Expr) error {
	ex.pointer = fp
	if fn := ex.d.Functions[s.currentFunction()]; fn != nil {
		if clean := ex.cleanupAfter(fn, in.VA); clean >= 0 {
			return ex.opaqueCall(s, in, next, pointerCallName, clean, false, true)
		}
	}
	return ex.opaqueCall(s, in, next, pointerCallName, -1, false)
}

// Kernel exports taking a variable argument list, which their callers clean up after
var kernelCdecl = map[string]bool{"DbgPrint": true, "RtlSprintf": true, "RtlSnprintf": true, "RtlVsprintf": true, "RtlVsnprintf": true}

// Kernel exports that never return to their caller
var kernelNoReturn = map[string]bool{"HalReturnToFirmware": true, "KeBugCheck": true, "KeBugCheckEx": true, "RtlRaiseException": true, "RtlRaiseStatus": true}

// Records a kernel export call with everything pushed for it, the caller cleaning up only a variable argument list,
// a lifted block ending at an export that never comes back
func (ex *Executor) importCall(s *execState, in *Instruction, next uint32, name string) error {
	if err := ex.opaqueCall(s, in, next, name, -1, false, kernelCdecl[name]); err != nil {
		return err
	}
	if kernelNoReturn[name] && ex.block != nil && len(s.frames) == 0 {
		ex.block.term = &blockTerm{kind: "halt"}
		s.va = 0
	}
	return nil
}

// The function a chain of single jump thunks lands in, the function itself when it is no thunk
func (d *Disassembly) thunkTarget(va uint32) uint32 {
	for hops := 0; hops < 8; hops++ {
		fn := d.Functions[va]
		if fn == nil || len(fn.Instructions) != 1 || fn.Instructions[0].Inst.Op != x86asm.JMP {
			return va
		}
		if _, isRel := fn.Instructions[0].Inst.Args[0].(x86asm.Rel); !isRel {
			return va
		}
		t := resolveTarget(&fn.Instructions[0])
		if t == 0 || t == va {
			return va
		}
		if d.Functions[t] == nil {
			if d.InsnByVA[t] == nil {
				return va
			}
			if built := d.buildFunction(t, map[uint32]bool{}); built != nil {
				d.Functions[t] = built
			} else {
				return va
			}
		}
		va = t
	}
	return va
}

// Stack bytes a function removes on return, read off its return or off the function its last jump lands in
func (d *Disassembly) cleanupOf(fn *Function) int {
	for hops := 0; fn != nil && hops < 8; hops++ {
		for i := len(fn.Instructions) - 1; i >= 0; i-- {
			in := &fn.Instructions[i]
			if in.Inst.Op != x86asm.RET {
				continue
			}
			if argCount(in) > 0 {
				return int(in.Inst.Args[0].(x86asm.Imm))
			}
			return 0
		}
		if len(fn.Instructions) == 0 {
			return 0
		}
		last := &fn.Instructions[len(fn.Instructions)-1]
		if last.Inst.Op != x86asm.JMP {
			return 0
		}
		if _, isRel := last.Inst.Args[0].(x86asm.Rel); !isRel {
			return 0
		}
		t := resolveTarget(last)
		if t == 0 || t == fn.EntryVA || d.InsnByVA[t] == nil {
			return 0
		}
		if d.Functions[t] == nil {
			if built := d.buildFunction(t, map[uint32]bool{}); built != nil {
				d.Functions[t] = built
			}
		}
		fn = d.Functions[t]
	}
	return 0
}

// Argument words a direct call target removes from the stack itself, -1 where neither its name nor its return says
func (d *Disassembly) callArity(target uint32) int {
	if _, isImport := d.Imports[target]; isImport {
		return -1
	}
	fn := d.Functions[target]
	if fn == nil {
		return -1
	}
	if _, ok := d.importThunk(fn); ok {
		return -1
	}
	if sym := fn.Symbol; sym != nil {
		if sym.FastCall {
			return -1
		}
		if sym.ArgBytes >= 0 {
			return sym.ArgBytes / 4
		}
	}
	if clean := d.cleanupOf(fn); clean > 0 {
		return clean / 4
	}
	return -1
}

// Instructions the look past a call walks on each way on before giving up on finding the next call
const demandScanLimit = 400

// Argument words the calls after a call of unknown arity take from the ones standing when it is made, read off
// every way on until the next call, each call placing what it pushes itself first
func (d *Disassembly) demandAfter(fn *Function, callVA uint32) int {
	index := fn.instructionIndex()
	start, ok := index[callVA]
	if !ok || start+1 >= len(fn.Instructions) {
		return 0
	}
	type point struct {
		i     int
		delta int
	}
	visited := map[int]bool{}
	work := []point{{start + 1, 0}}
	demand := 0
	for steps := 0; len(work) > 0 && steps < demandScanLimit; steps++ {
		p := work[len(work)-1]
		work = work[:len(work)-1]
		if visited[p.i] {
			continue
		}
		visited[p.i] = true
		in := &fn.Instructions[p.i]
		delta := p.delta
		esp := func() bool {
			r, ok := in.Inst.Args[0].(x86asm.Reg)
			return ok && r == x86asm.ESP
		}
		imm := func() int {
			if v, ok := in.Inst.Args[1].(x86asm.Imm); ok {
				return int(v)
			}
			return 0
		}
		switch in.Inst.Op {
		case x86asm.PUSH:
			delta++
		case x86asm.POP:
			delta--
		case x86asm.SUB:
			if esp() {
				delta += imm() / 4
			}
		case x86asm.ADD:
			if esp() {
				delta -= imm() / 4
			}
		case x86asm.LEAVE, x86asm.RET:
			continue
		case x86asm.MOV:
			if esp() {
				continue
			}
		case x86asm.CALL:
			t := resolveTarget(in)
			if t == 0 {
				continue
			}
			if arity := d.callArity(t); arity > delta && arity-delta > demand {
				demand = arity - delta
			}
			continue
		case x86asm.JMP:
			if t := resolveTarget(in); t != 0 {
				if j, ok := index[t]; ok {
					work = append(work, point{j, delta})
				}
			}
			continue
		}
		// Popping below where the call left the stack is the epilogue, past which no call reads those words
		if delta < 0 {
			continue
		}
		if isConditionalJump(in.Inst.Op) {
			if t := resolveTarget(in); t != 0 {
				if j, ok := index[t]; ok {
					work = append(work, point{j, delta})
				}
			}
		}
		if p.i+1 < len(fn.Instructions) {
			work = append(work, point{p.i + 1, delta})
		}
	}
	return demand
}

// Reports whether a function traps as soon as it is entered, following only the jumps it takes unconditionally
func (d *Disassembly) trapsOnEntry(fn *Function) bool {
	va := fn.EntryVA
	for hops := 0; hops < 8; hops++ {
		in := d.InsnByVA[va]
		if in == nil {
			return false
		}
		switch in.Inst.Op {
		case x86asm.INT, x86asm.UD2, x86asm.HLT:
			return true
		case x86asm.JMP:
			if _, isRel := in.Inst.Args[0].(x86asm.Rel); !isRel {
				return false
			}
			va = resolveTarget(in)
		default:
			return false
		}
	}
	return false
}

// Names the import a single jump function forwards to
func (d *Disassembly) importThunk(fn *Function) (string, bool) {
	if len(fn.Instructions) != 1 || fn.Instructions[0].Inst.Op != x86asm.JMP {
		return "", false
	}
	m, ok := fn.Instructions[0].Inst.Args[0].(x86asm.Mem)
	if !ok || m.Base != 0 || m.Index != 0 {
		return "", false
	}
	name, ok := d.Imports[uint32(m.Disp)]
	return name, ok
}

// Lists every expression a path holds, in its guards, effects, argument buffers and return
func pathExprs(p Path) []Expr {
	out := append([]Expr{}, p.Guards...)
	for _, e := range p.Effects {
		out = append(out, e.Value, e.Addr, e.This)
		out = append(out, e.Args...)
		for _, b := range e.Buffers {
			out = append(out, b.Elems...)
		}
	}
	return append(out, p.Return)
}

// Restores the caller saved registers a known callee never writes after an opaque call clobbered them, the
// accumulator among them where the callee returns nothing
func (ex *Executor) keepUnwritten(s *execState, target uint32, eax, ecx, edx Expr) {
	if debugNoKeep {
		return
	}
	writes := map[x86asm.Reg]bool{}
	for _, r := range ex.d.callClobbers(target) {
		writes[r] = true
	}
	if !writes[x86asm.EAX] && eax != nil {
		s.regs[0] = eax
	}
	if !writes[x86asm.ECX] {
		s.regs[1] = ecx
	}
	if !writes[x86asm.EDX] {
		s.regs[2] = edx
	}
}

// Looks up a vtable for an object pointer loaded from a known global, or for the vtable word read from such an object
func (ex *Executor) vtableFor(s *execState, obj Expr) uint32 {
	if l, ok := obj.(Load); ok && l.Base == "" && l.Addr == nil {
		return ex.vtables[uint32(l.Off)]
	}
	if l, ok := obj.(Load); ok && !debugNoVResolve && l.Addr == nil && l.Off == 0 && l.Size == 4 && len(l.Base) == 9 && l.Base[0] == 'g' {
		var va uint32
		if _, err := fmt.Sscanf(l.Base[1:], "%08X", &va); err == nil {
			return ex.vtables[va]
		}
	}
	if p, ok := obj.(Ptr); ok && p.Off == 0 && len(p.Base) == 9 && p.Base[0] == 'g' {
		var va uint32
		if _, err := fmt.Sscanf(p.Base[1:], "%08X", &va); err == nil {
			if vt, ok := ex.vtables[va]; ok {
				return vt
			}
		}
	}
	if p, ok := obj.(Ptr); ok {
		if mv, ok := s.mem[memKey{p.Base, p.Off}]; ok {
			if c, ok := isConst(mv.v); ok && ex.d.isVtable(c) {
				return c
			}
		}
	}
	return 0
}

func calleeCleanupOf(fn *Function) int {
	for i := len(fn.Instructions) - 1; i >= 0; i-- {
		in := fn.Instructions[i]
		if in.Inst.Op == x86asm.RET {
			if argCount(&in) > 0 {
				return int(in.Inst.Args[0].(x86asm.Imm))
			}
			return 0
		}
	}
	return 0
}

// outBuffer is memory a library routine writes through a pointer argument.
type outBuffer struct {
	arg   int // the pointer argument
	bytes int // bytes written where the count argument is unknown or absent
	count int // the argument counting the elements written, -1 where the type fixes the size
	unit  int // bytes per counted element
}

// Bytes a wide string buffer a library routine fills may hold
const wideBuffer = 0x1000

// Bytes the whole EEPROM holds, the most a query of one of its values writes
const eepromSize = 256

// Bytes the 32 binary digits of a converted integer and their terminator take as wide characters
const itowBuffer = 66

// Memory the library routines write through their pointer arguments, which the executor forgets so the loads
// after the call read what the routine left
var outBuffers = map[string][]outBuffer{
	"ReadFile": {{1, 1 << 20, 2, 1}, {3, 4, -1, 0}}, "GetFileSize": {{1, 4, -1, 0}}, "SetFilePointer": {{2, 4, -1, 0}},
	"FindFirstFileA": {{1, 320, -1, 0}}, "FindNextFileA": {{1, 320, -1, 0}}, "GetLocalTime": {{0, 16, -1, 0}}, "GetSystemTime": {{0, 16, -1, 0}},
	"QueryPerformanceCounter": {{0, 8, -1, 0}}, "QueryPerformanceFrequency": {{0, 8, -1, 0}}, "XGetLaunchInfo": {{0, 4, -1, 0}, {1, 3072, -1, 0}},
	"XcSHAInit": {{0, 116, -1, 0}}, "XcSHAUpdate": {{0, 116, -1, 0}}, "XcSHAFinal": {{0, 116, -1, 0}, {1, 20, -1, 0}}, "XcHMAC": {{6, 20, -1, 0}},
	"XQueryValue": {{1, 4, -1, 0}, {2, eepromSize, 3, 1}, {4, 4, -1, 0}},
	"wcscpy":      {{0, wideBuffer, -1, 0}}, "wcscat": {{0, wideBuffer, -1, 0}}, "wcsncpy": {{0, wideBuffer, 2, 2}}, "wcsncat": {{0, wideBuffer, 2, 2}},
	"swprintf": {{0, wideBuffer, -1, 0}}, "_snwprintf": {{0, wideBuffer, 1, 2}}, "_wcslwr": {{0, wideBuffer, -1, 0}}, "_wcsupr": {{0, wideBuffer, -1, 0}},
	"lstrcpyW": {{0, wideBuffer, -1, 0}}, "lstrcatW": {{0, wideBuffer, -1, 0}}, "_itow": {{1, itowBuffer, -1, 0}}, "_ltow": {{1, itowBuffer, -1, 0}},
	"memcpy": {{0, 1 << 20, 2, 1}}, "memset": {{0, 1 << 20, 2, 1}}, "XNetRandom": {{0, 1 << 16, 1, 1}},
}

// Bytes a routine writes through one pointer argument of a call, its count argument deciding where it is known
func (ob outBuffer) written(args []Expr) int {
	if ob.count < 0 || ob.count >= len(args) {
		return ob.bytes
	}
	if n, known := isConst(args[ob.count]); known {
		return int(n) * ob.unit
	}
	return ob.bytes
}

// Drops what the path knows of the memory a library routine writes from a pointer, never past the saved
// registers or return address of the frame a stack buffer lies in
func (ex *Executor) forget(s *execState, p Ptr, bytes int) {
	start, end := int64(p.Off), int64(p.Off)+int64(bytes)
	if p.Base == "stack" {
		for k, mv := range s.mem {
			if k.base == p.Base && mv.frame && int64(k.off) >= start && int64(k.off) < end {
				end = int64(k.off)
			}
		}
		ex.spillForgotten(s, p.Base, start, end)
	}
	s.unbind(p.Base, start, end)
	for k, mv := range s.mem {
		if k.base == p.Base && int64(k.off)+int64(mv.size) > start && int64(k.off) < end {
			delete(s.mem, k)
		}
	}
}

// XDK functions that return a handle through a pointer argument
var outParams = map[string]int{"D3DDevice_CreateVertexShader": 2, "D3DDevice_CreatePixelShader": 1, "D3DDevice_CreateVertexBuffer": 4, "D3DVertexBuffer_Lock": 3, "D3DXCreateMatrixStack": 1}

// Creations the runtime runs from the image, which hand back their object through a pointer argument only in the
// derivations made at build time
var imageOutParams = map[string]int{"D3DXCreateTexture": 7}

// D3DSURFACE_DESC words in the Xbox layout
const surfaceDescFormat, surfaceDescWidth, surfaceDescHeight = 0, 0x14, 0x18

// The size and format arguments of each texture creation, as the description of the texture it made reports them
var textureCreations = map[string][3]int{"D3DXCreateTexture": {1, 2, 5}, "D3DDevice_CreateTexture": {0, 1, 4}}

// Fills a surface description read off a texture the path created, from the arguments of its creation
func (ex *Executor) describeSurface(s *execState, name string, args []Expr, va uint32) {
	switch name {
	case "PixelJar::Get2DSurfaceDesc", "D3DSurface_GetDesc", "D3DTexture_GetLevelDesc":
	default:
		return
	}
	var creation *CallResult
	var desc Expr
	for _, a := range args {
		if cr, ok := a.(CallResult); ok {
			if _, made := textureCreations[cr.Fn]; made && creation == nil {
				c := cr
				creation = &c
				continue
			}
		}
		if p, ok := asPtr(a).(Ptr); ok && desc == nil && p.Base != "" {
			desc = p
		}
	}
	if creation == nil || desc == nil {
		return
	}
	slots := textureCreations[creation.Fn]
	if len(creation.Args) <= slots[2] {
		return
	}
	p := desc.(Ptr)
	ex.store(s, Ptr{p.Base, p.Off + surfaceDescWidth}, 4, creation.Args[slots[0]], va)
	ex.store(s, Ptr{p.Base, p.Off + surfaceDescHeight}, 4, creation.Args[slots[1]], va)
	ex.store(s, Ptr{p.Base, p.Off + surfaceDescFormat}, 4, creation.Args[slots[2]], va)
}

// Library helpers whose bodies are pure arithmetic and are executed instead of summarized
var inlineSymbols = map[string]bool{"?0D3DXCOLOR": true}

// Byte sizes of the buffers D3DX math functions read and write through pointers
func d3dxBufferSize(name string) int {
	name = strings.TrimPrefix(strings.TrimPrefix(name, "D3DX"), "XG")
	switch {
	case strings.HasPrefix(name, "Matrix"):
		return 64
	case strings.HasPrefix(name, "Vec4"), strings.HasPrefix(name, "Quaternion"), strings.HasPrefix(name, "Plane"), strings.HasPrefix(name, "Color"):
		return 16
	case strings.HasPrefix(name, "Vec3"):
		return 12
	case strings.HasPrefix(name, "Vec2"):
		return 8
	}
	return 0
}

// Reads a whole buffer as one value, falling back to a symbolic block load
func (ex *Executor) loadBlock(s *execState, a Expr, size int) Expr {
	a = asPtr(a)
	p, ok := a.(Ptr)
	if !ok {
		if v, stored := s.storedThrough(a, size); stored {
			return v
		}
		return Load{Addr: a, Size: size, Epoch: len(s.effects)}
	}
	if mv, ok := s.mem[memKey{p.Base, p.Off}]; ok && mv.size >= size && mv.size > 4 {
		return mv.v
	}
	// Assemble from scalar and nested block entries when the range is fully present.
	var elems []Expr
	for off := int32(0); off < int32(size); {
		mv, ok := s.mem[memKey{p.Base, p.Off + off}]
		if !ok || mv.size < 4 || off+int32(mv.size) > int32(size) {
			elems = nil
			break
		}
		switch {
		case mv.size > 4 || isFloatExpr(mv.v):
			elems = append(elems, mv.v)
		case isElem(mv.v):
			elems = append(elems, mv.v)
		default:
			elems = append(elems, mkUn("bits2f", mv.v))
		}
		off += int32(mv.size)
	}
	if elems != nil {
		return Block{Elems: elems}
	}
	// A range some store on this path only partly covers is read word by word, the rest as entry values
	if ex.writtenWithin(s, p, size) {
		for off := int32(0); off < int32(size); off += 4 {
			w := ex.load(s, Ptr{p.Base, p.Off + off}, 4)
			if !isFloatExpr(w) && !isElem(w) {
				w = mkUn("bits2f", w)
			}
			elems = append(elems, w)
		}
		return Block{Elems: elems}
	}
	if p.Base == "" && ex.isConstantGlobal(uint32(p.Off), size) {
		for off := int32(0); off < int32(size); off += 4 {
			v, _ := ex.d.Image.ReadU32(uint32(p.Off + off))
			elems = append(elems, FConst{float64(math.Float32frombits(v))})
		}
		return Block{Elems: elems}
	}
	return Load{Base: p.Base, Off: p.Off, Size: size, Epoch: len(s.effects)}
}

// Reports whether any store on this path touches bytes of a range
func (ex *Executor) writtenWithin(s *execState, p Ptr, size int) bool {
	for k, mv := range s.mem {
		if k.base == p.Base && k.off < p.Off+int32(size) && p.Off < k.off+int32(mv.size) {
			return true
		}
	}
	return false
}

// Records a call to code outside the analysis with its arguments
func (ex *Executor) opaqueCall(s *execState, in *Instruction, next uint32, name string, argBytes int, fastcall bool, cdecl ...bool) error {
	callerCleans := len(cdecl) > 0 && cdecl[0]
	target := ex.callTarget
	ex.callTarget = 0
	receiver := ex.callReceiver
	ex.callReceiver = false
	tail := ex.tail
	ex.tail = false
	base := uint32(0)
	if tail {
		base = 4
	}
	var args []Expr
	if fastcall {
		args = append(args, s.getReg(x86asm.ECX), s.getReg(x86asm.EDX))
		argBytes -= 8
	}
	n := 0
	if argBytes > 0 {
		n = argBytes / 4
	}
	// Arguments pushed before an earlier call of unknown arity are still reachable through its shadow.
	k := s.pushes
	missing := 0
	if s.pending != nil && argBytes >= 0 && k < n {
		missing = n - k
		if missing > len(s.pending.shadow) {
			missing = len(s.pending.shadow)
		}
	}
	for i := 0; i < n; i++ {
		if i >= k && s.pending != nil && missing > 0 {
			j := len(s.pending.shadow) - missing + (i - k)
			if j >= 0 && j < len(s.pending.shadow) {
				args = append(args, s.pending.shadow[j])
				continue
			}
		}
		args = append(args, ex.load(s, mkBin("add", s.regs[4], IConst{base + uint32(4*i)}), 4))
	}
	var this Expr
	if !fastcall && strings.HasPrefix(name, "sub_") {
		this = s.getReg(x86asm.ECX)
		landing := ex.d.thunkTarget(target)
		// A helper the caller cleans up after still reads the arguments it was pushed
		if argBytes == 0 && target != 0 && ex.d.Functions[landing] != nil {
			for i := 0; i < ex.d.stackInputs(ex.d.Functions[landing]); i++ {
				args = append(args, ex.load(s, mkBin("add", s.regs[4], IConst{base + uint32(4*i)}), 4))
			}
		}
		// A dashboard function compiled to read argument registers takes them before its stack arguments
		var leading []Expr
		for _, r := range ex.d.registerInputs(landing) {
			leading = append(leading, s.input(r))
		}
		args = append(leading, args...)
	}
	// A call through a pointer the dashboard keeps reaches a method, which takes its object in ecx
	if name == pointerCallName {
		this = s.getReg(x86asm.ECX)
	}
	// A library routine reading ecx on entry takes its receiver there
	if receiver {
		this = s.getReg(x86asm.ECX)
	}
	// A lifted block counts the words standing between the stack pointer and the rest level as the arguments
	// of a call whose arity is unknown, which covers arguments stored into a reserved area as well as pushed ones
	words := s.pushes
	if ex.block != nil {
		words = int(s.outstanding()/4) - ex.d.demandAfter(ex.d.Functions[s.root], in.VA)
		if words < 0 {
			words = 0
		}
		// A call the caller cleans up after takes exactly what that cleanup removes
		if callerCleans && argBytes < 0 {
			if clean := ex.cleanupAfter(ex.d.Functions[s.root], in.VA); clean >= 0 {
				words = clean / 4
			}
		}
	}
	if name == pointerCallName {
		args = append([]Expr{ex.pointer}, args...)
		ex.pointer = nil
	}
	if (len(name) > 6 && name[:6] == "vcall@") || name == pointerCallName {
		if name != pointerCallName {
			args = append([]Expr{s.getReg(x86asm.ECX)}, args...)
		}
		if argBytes < 0 {
			for i := 0; i < words && i < 8; i++ {
				v := ex.load(s, mkBin("add", s.regs[4], IConst{base + uint32(4*i)}), 4)
				if _, isRet := v.(RetAddr); isRet {
					break
				}
				args = append(args, v)
			}
		}
	} else if argBytes < 0 {
		for i := 0; i < words && i < 8; i++ {
			v := ex.load(s, mkBin("add", s.regs[4], IConst{base + uint32(4*i)}), 4)
			if _, isRet := v.(RetAddr); isRet {
				break
			}
			args = append(args, v)
		}
	}
	s.pending = nil
	popBytes := uint32(0)
	switch {
	case callerCleans:
		// The caller removes what it pushed, so the callee leaves the stack where the call found it
	case argBytes > 0 && missing > 0:
		popBytes = uint32(4 * k)
	case argBytes > 0:
		popBytes = uint32(argBytes)
	case argBytes < 0 && !callerCleans && ex.block != nil:
		popBytes = uint32(4 * words)
	case argBytes < 0 && !callerCleans:
		// Unknown stdcall arity: the callee removed everything pushed for it.
		var shadow []Expr
		for i := 0; i < s.pushes; i++ {
			shadow = append(shadow, ex.load(s, mkBin("add", s.regs[4], IConst{uint32(4 * i)}), 4))
		}
		s.pending = &pendingCall{shadow: shadow}
		popBytes = uint32(4 * s.pushes)
	}
	s.pushes = 0
	if tail {
		ex.tailClean = popBytes
		popBytes = 0
	}
	if target != 0 {
		if err := ex.normalizeConstantRegister(target, name, args); err != nil {
			return err
		}
	}
	if err := ex.recordCall(s, in, next, name, args, this, popBytes); err != nil {
		return err
	}
	if strings.HasPrefix(name, "sub_") || strings.HasPrefix(name, "vcall@") || name == pointerCallName {
		ex.forgetStackPointees(s, this, args)
	}
	return nil
}

// Bytes a dashboard routine may write past a stack buffer it receives, the frame's saved registers bounding it
const stackPointeeExtent = 1 << 30

// Drops what the path knows of each stack buffer a dashboard routine receives a pointer to
func (ex *Executor) forgetStackPointees(s *execState, this Expr, args []Expr) {
	for _, a := range append([]Expr{this}, args...) {
		if p, ok := a.(Ptr); ok && p.Base == "stack" {
			ex.forget(s, p, stackPointeeExtent)
		}
	}
}

// Records a library call whose parameter locations were derived from its body
func (ex *Executor) conventionCall(s *execState, in *Instruction, next uint32, fn *Function) (bool, error) {
	conv, err := ex.d.convention(fn)
	if err != nil {
		return true, err
	}
	if !conv.known {
		return false, nil
	}
	args := make([]Expr, len(conv.params))
	for i, loc := range conv.params {
		switch {
		case loc.None:
			args[i] = nil
		case loc.Reg != 0:
			args[i] = s.getReg(loc.Reg)
		default:
			args[i] = ex.load(s, mkBin("add", s.regs[4], IConst{uint32(4 * loc.Stack)}), 4)
		}
	}
	s.pending = nil
	s.pushes = 0
	name := calleeName(fn)
	if err := ex.normalizeConstantRegister(fn.EntryVA, name, args); err != nil {
		return true, err
	}
	return true, ex.recordCall(s, in, next, name, args, nil, uint32(conv.stackBytes))
}

// Vertex shader constant registers the XDK API numbers from, its setters adding this to reach the hardware index
const apiConstantBias = 96

// Rewrites the register a constant setter receives into the API numbering its own bias implies
func (ex *Executor) normalizeConstantRegister(target uint32, name string, args []Expr) error {
	if !strings.HasPrefix(name, "D3DDevice_SetVertexShaderConstant") || len(args) == 0 {
		return nil
	}
	fn := ex.d.Functions[target]
	if fn == nil || fn.Symbol == nil {
		return nil
	}
	bias, err := ex.d.constantBias(fn)
	if err != nil {
		return err
	}
	if delta := bias - apiConstantBias; delta != 0 {
		args[0] = mkBin("add", args[0], IConst{uint32(delta)})
	}
	return nil
}

// Records a call effect with its arguments, the result and the stack the callee removed
func (ex *Executor) recordCall(s *execState, in *Instruction, next uint32, name string, args []Expr, this Expr, popBytes uint32) error {
	ex.seq++
	callee := ex.callee
	ex.callee = 0
	var ret Expr
	bufSize := d3dxBufferSize(name)
	if bufSize > 0 && len(args) > 0 {
		// Pointer arguments become the values they point at and the output buffer receives the result.
		vals := make([]Expr, len(args))
		for i, a := range args {
			if c, ok := isConst(a); ok && ex.d.sectionOf(c) != nil {
				a = Ptr{"", int32(c)}
				args[i] = a
			}
			if p, ok := a.(Ptr); ok && (i > 0 || !d3dxWritesArg0(name)) {
				vals[i] = ex.loadBlock(s, p, argBufferSize(name, i, bufSize))
			} else {
				vals[i] = a
			}
		}
		res := CallResult{Fn: name, Args: vals, Seq: ex.seq, Target: callee}
		if p, ok := args[0].(Ptr); ok && d3dxWritesArg0(name) {
			ex.store(s, p, bufSize, res, in.VA)
		}
		s.effects = append(s.effects, Effect{Kind: "call", VA: in.VA, Fn: name, Args: vals, Seq: ex.seq, Target: callee})
		ret = args[0]
	} else {
		res := CallResult{Fn: name, Args: args, Seq: ex.seq, This: this, Target: callee}
		s.effects = append(s.effects, Effect{Kind: "call", VA: in.VA, Fn: name, Args: args, Seq: ex.seq, This: this, Target: callee})
		ret = res
		outIdx, ok := outParams[name]
		if !ok && ex.ImageCreations {
			outIdx, ok = imageOutParams[name]
		}
		if ok && outIdx < len(args) {
			if p, ok := asPtr(args[outIdx]).(Ptr); ok {
				ex.store(s, p, 4, res, in.VA)
			}
		}
		for _, ob := range outBuffers[name] {
			if ob.arg < len(args) {
				if p, ok := asPtr(args[ob.arg]).(Ptr); ok {
					ex.forget(s, p, ob.written(args))
				}
			}
		}
		ex.describeSurface(s, name, args, in.VA)
		for i, a := range args {
			if p, ok := a.(Ptr); ok && p.Base == "stack" {
				elems, sizes := ex.stackElems(s, p, stackBufferElems)
				s.effects[len(s.effects)-1].Buffers = append(s.effects[len(s.effects)-1].Buffers, ArgBuffer{Index: i, Elems: elems, Sizes: sizes})
			}
		}
		// A helper reading the buffer it was handed in a register reads what the path left there
		if p, ok := this.(Ptr); ok && p.Base == "stack" {
			if elems, sizes := ex.stackElems(s, p, stackBufferElems); len(elems) > 0 {
				s.effects[len(s.effects)-1].Buffers = append(s.effects[len(s.effects)-1].Buffers, ArgBuffer{Index: thisBufferIndex, Elems: elems, Sizes: sizes})
			}
		}
	}
	s.setReg(x86asm.EAX, ret)
	s.setReg(x86asm.ECX, Sym{fmt.Sprintf("clobber.ecx#%d", ex.seq)})
	s.setReg(x86asm.EDX, Sym{fmt.Sprintf("clobber.edx#%d", ex.seq)})
	if popBytes > 0 {
		s.regs[4] = mkBin("add", s.regs[4], IConst{popBytes})
	}
	s.va = next
	return nil
}

func isElem(e Expr) bool {
	_, ok := e.(Elem)
	return ok
}

// Reports whether a D3DX function writes its result through the first argument
func d3dxWritesArg0(name string) bool {
	switch name {
	case "D3DXMatrixDeterminant", "D3DXVec3Dot", "D3DXVec3Length", "D3DXVec3LengthSq", "D3DXVec4Dot", "D3DXQuaternionDot", "D3DXVec2Dot":
		return false
	}
	return true
}

// Buffer size of a D3DX pointer argument, which differs from the result size for mixed calls
func argBufferSize(name string, i int, result int) int {
	switch name {
	case "D3DXVec3TransformNormal", "D3DXVec3TransformCoord", "D3DXVec3Transform":
		if i == 2 {
			return 64
		}
		return 12
	case "D3DXMatrixInverse":
		if i == 1 {
			return 4
		}
	case "D3DXMatrixAffineTransformation":
		switch i {
		case 2, 4:
			return 12
		case 3:
			return 16
		}
	case "D3DXMatrixRotationAxis", "D3DXQuaternionRotationAxis":
		if i == 1 {
			return 12
		}
	case "D3DXMatrixRotationQuaternion":
		if i == 1 {
			return 16
		}
	}
	return result
}

// Reads consecutive stack entries starting at a pointer, blocks included, for call argument snapshots
func (ex *Executor) stackElems(s *execState, p Ptr, max int) ([]Expr, []int) {
	var out []Expr
	var sizes []int
	off := int32(0)
	for len(out) < max {
		mv, ok := s.mem[memKey{p.Base, p.Off + off}]
		if !ok {
			break
		}
		if _, isRet := mv.v.(RetAddr); isRet {
			break
		}
		if mv.size == 4 && !isFloatExpr(mv.v) && !isElem(mv.v) {
			out = append(out, mkUn("bits2f", mv.v))
		} else {
			out = append(out, mv.v)
		}
		sizes = append(sizes, mv.size)
		off += int32(mv.size)
	}
	return out, sizes
}

func isFPU(op x86asm.Op) bool {
	return op >= x86asm.F2XM1 && op <= x86asm.FYL2XP1
}

func (s *execState) fpuPush(v Expr) {
	s.fpu = append([]Expr{v}, s.fpu...)
	if len(s.fpu) > 8 {
		s.fpu = s.fpu[:8]
	}
}

func (s *execState) fpuPop() Expr {
	if len(s.fpu) == 0 {
		return Sym{"fpu.empty"}
	}
	v := s.fpu[0]
	s.fpu = s.fpu[1:]
	return v
}

func (s *execState) st(i int) Expr {
	if i < len(s.fpu) {
		return s.fpu[i]
	}
	return Sym{fmt.Sprintf("fpu.st%d", i)}
}

func (s *execState) setSt(i int, v Expr) {
	for len(s.fpu) <= i {
		s.fpu = append(s.fpu, Sym{fmt.Sprintf("fpu.st%d", len(s.fpu))})
	}
	s.fpu[i] = v
}

func stIndex(r x86asm.Reg) int { return int(r - x86asm.F0) }

// Executes one x87 instruction
func (ex *Executor) fpuStep(s *execState, in *Instruction) error {
	args := in.Inst.Args
	nargs := argCount(in)
	op := in.Inst.Op
	memOperand := func() (Expr, int, bool) {
		for _, a := range args {
			if m, ok := a.(x86asm.Mem); ok {
				return ex.addr(s, m), in.Inst.MemBytes, true
			}
		}
		return nil, 0, false
	}
	loadF := func() Expr {
		a, size, ok := memOperand()
		if !ok {
			return nil
		}
		return ex.loadFloat(s, a, size)
	}
	loadI := func() Expr {
		a, size, _ := memOperand()
		v := ex.load(s, a, size)
		if size == 2 {
			v = mkUn("sx16", v)
		}
		return mkUn("i2f", v)
	}
	binop := func(name string, reverse bool, pop bool) {
		var dst int
		var a, b Expr
		if _, _, isMem := memOperand(); isMem {
			dst = 0
			a, b = s.st(0), loadF()
		} else if nargs == 2 {
			d := stIndex(args[0].(x86asm.Reg))
			sidx := stIndex(args[1].(x86asm.Reg))
			dst = d
			a, b = s.st(d), s.st(sidx)
		} else {
			dst = 1
			a, b = s.st(1), s.st(0)
		}
		if reverse {
			a, b = b, a
		}
		s.setSt(dst, mkFBin(name, a, b))
		if pop {
			s.fpuPop()
		}
	}
	ibinop := func(name string, reverse bool) {
		a, b := s.st(0), loadI()
		if reverse {
			a, b = b, a
		}
		s.setSt(0, mkFBin(name, a, b))
	}
	switch op {
	case x86asm.FLD:
		if r, ok := args[0].(x86asm.Reg); ok {
			s.fpuPush(s.st(stIndex(r)))
		} else {
			s.fpuPush(loadF())
		}
	case x86asm.FLD1:
		s.fpuPush(FConst{1})
	case x86asm.FLDZ:
		s.fpuPush(FConst{0})
	case x86asm.FLDPI:
		s.fpuPush(FConst{math.Pi})
	case x86asm.FILD:
		s.fpuPush(loadI())
	case x86asm.FST, x86asm.FSTP:
		if r, ok := args[0].(x86asm.Reg); ok {
			s.setSt(stIndex(r), s.st(0))
		} else {
			a, size, _ := memOperand()
			ex.storeFloat(s, a, size, s.st(0), in.VA)
		}
		if op == x86asm.FSTP {
			s.fpuPop()
		}
	case x86asm.FIST, x86asm.FISTP:
		a, size, _ := memOperand()
		ex.store(s, a, size, mkUn("f2i_rn", s.st(0)), in.VA)
		if op == x86asm.FISTP {
			s.fpuPop()
		}
	case x86asm.FADD:
		binop("fadd", false, false)
	case x86asm.FADDP:
		binop("fadd", false, true)
	case x86asm.FSUB:
		binop("fsub", false, false)
	case x86asm.FSUBP:
		binop("fsub", false, true)
	case x86asm.FSUBR:
		binop("fsub", true, false)
	case x86asm.FSUBRP:
		binop("fsub", true, true)
	case x86asm.FMUL:
		binop("fmul", false, false)
	case x86asm.FMULP:
		binop("fmul", false, true)
	case x86asm.FDIV:
		binop("fdiv", false, false)
	case x86asm.FDIVP:
		binop("fdiv", false, true)
	case x86asm.FDIVR:
		binop("fdiv", true, false)
	case x86asm.FDIVRP:
		binop("fdiv", true, true)
	case x86asm.FIADD:
		ibinop("fadd", false)
	case x86asm.FISUB:
		ibinop("fsub", false)
	case x86asm.FISUBR:
		ibinop("fsub", true)
	case x86asm.FIMUL:
		ibinop("fmul", false)
	case x86asm.FIDIV:
		ibinop("fdiv", false)
	case x86asm.FIDIVR:
		ibinop("fdiv", true)
	case x86asm.FCHS:
		s.setSt(0, mkUn("fneg", s.st(0)))
	case x86asm.FABS:
		s.setSt(0, mkUn("fabs", s.st(0)))
	case x86asm.FSIN:
		s.setSt(0, mkUn("fsin", s.st(0)))
	case x86asm.FCOS:
		s.setSt(0, mkUn("fcos", s.st(0)))
	case x86asm.FSQRT:
		s.setSt(0, mkUn("fsqrt", s.st(0)))
	case x86asm.FXCH:
		i := 1
		if nargs > 0 {
			i = stIndex(args[0].(x86asm.Reg))
		}
		a, b := s.st(0), s.st(i)
		s.setSt(0, b)
		s.setSt(i, a)
	case x86asm.FCOM, x86asm.FCOMP, x86asm.FCOMPP, x86asm.FUCOM, x86asm.FUCOMP, x86asm.FUCOMPP, x86asm.FICOM, x86asm.FICOMP:
		var b Expr
		switch {
		case op == x86asm.FICOM || op == x86asm.FICOMP:
			b = loadI()
		case nargs == 0:
			b = s.st(1)
		default:
			if r, ok := args[nargs-1].(x86asm.Reg); ok {
				b = s.st(stIndex(r))
			} else {
				b = loadF()
			}
		}
		s.fcmp = &fcmpRec{a: s.st(0), b: b}
		switch op {
		case x86asm.FCOMP, x86asm.FUCOMP, x86asm.FICOMP:
			s.fpuPop()
		case x86asm.FCOMPP, x86asm.FUCOMPP:
			s.fpuPop()
			s.fpuPop()
		}
	case x86asm.FCOMI, x86asm.FCOMIP, x86asm.FUCOMI, x86asm.FUCOMIP:
		i := 1
		if nargs > 1 {
			i = stIndex(args[1].(x86asm.Reg))
		} else if nargs == 1 {
			i = stIndex(args[0].(x86asm.Reg))
		}
		s.fl = flagState{kind: "comiss", a: s.st(0), b: s.st(i)}
		if op == x86asm.FCOMIP || op == x86asm.FUCOMIP {
			s.fpuPop()
		}
	case x86asm.FNSTSW:
		s.axFSW = s.fcmp != nil
		s.regs[0] = Sym{"fsw"}
	case x86asm.FTST:
		s.fcmp = &fcmpRec{a: s.st(0), b: FConst{0}}
	case x86asm.FPATAN:
		y, x := s.st(1), s.st(0)
		s.fpuPop()
		s.setSt(0, mkFBin("fatan2", y, x))
	case x86asm.FYL2X:
		y, x := s.st(1), s.st(0)
		s.fpuPop()
		s.setSt(0, mkFBin("fmul", y, mkUn("flog2", x)))
	case x86asm.FYL2XP1:
		y, x := s.st(1), s.st(0)
		s.fpuPop()
		s.setSt(0, mkFBin("fmul", y, mkUn("flog2", mkFBin("fadd", x, FConst{1}))))
	case x86asm.F2XM1:
		s.setSt(0, mkFBin("fsub", mkFBin("fpow", FConst{2}, s.st(0)), FConst{1}))
	case x86asm.FSCALE:
		s.setSt(0, mkFBin("fmul", s.st(0), mkFBin("fpow", FConst{2}, mkUn("ftrunc", s.st(1)))))
	case x86asm.FRNDINT:
		s.setSt(0, mkUn("fround", s.st(0)))
	case x86asm.FPREM:
		s.setSt(0, mkFBin("fmod", s.st(0), s.st(1)))
	case x86asm.FPREM1:
		s.setSt(0, mkFBin("frem", s.st(0), s.st(1)))
	case x86asm.FSINCOS:
		x := s.st(0)
		s.setSt(0, mkUn("fsin", x))
		s.fpuPush(mkUn("fcos", x))
	case x86asm.FPTAN:
		s.setSt(0, mkUn("ftan", s.st(0)))
		s.fpuPush(FConst{1})
	case x86asm.FLDLN2:
		s.fpuPush(FConst{math.Ln2})
	case x86asm.FLDLG2:
		s.fpuPush(FConst{math.Log10(2)})
	case x86asm.FLDL2E:
		s.fpuPush(FConst{math.Log2E})
	case x86asm.FLDL2T:
		s.fpuPush(FConst{math.Log2(10)})
	case x86asm.FISTTP:
		a, size, _ := memOperand()
		ex.store(s, a, size, mkUn("f2i", s.st(0)), in.VA)
		s.fpuPop()
	case x86asm.FFREE:
	case x86asm.FFREEP:
		s.fpuPop()
	default:
		return fmt.Errorf("unsupported x87 instruction")
	}
	return nil
}

// The jump each conditional move takes its predicate from
var cmovJump = map[x86asm.Op]x86asm.Op{
	x86asm.CMOVA: x86asm.JA, x86asm.CMOVAE: x86asm.JAE, x86asm.CMOVB: x86asm.JB, x86asm.CMOVBE: x86asm.JBE,
	x86asm.CMOVE: x86asm.JE, x86asm.CMOVNE: x86asm.JNE, x86asm.CMOVG: x86asm.JG, x86asm.CMOVGE: x86asm.JGE,
	x86asm.CMOVL: x86asm.JL, x86asm.CMOVLE: x86asm.JLE, x86asm.CMOVS: x86asm.JS, x86asm.CMOVNS: x86asm.JNS,
	x86asm.CMOVP: x86asm.JP, x86asm.CMOVNP: x86asm.JNP,
}

// Collects every symbolic input referenced by a set of paths
func pathInputs(paths []Path) []string {
	set := map[string]bool{}
	for _, p := range paths {
		for _, g := range p.Guards {
			exprSyms(g, set)
		}
		for _, st := range p.Effects {
			if st.Value != nil {
				exprSyms(st.Value, set)
			}
			for _, a := range st.Args {
				exprSyms(a, set)
			}
		}
	}
	keys := sortedKeys(set)
	sort.Strings(keys)
	return keys
}
