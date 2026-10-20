package xbe

import (
	"fmt"
	"math"
	"os"
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
	Off     int32       `json:"off,omitempty"`
	Size    int         `json:"size,omitempty"`
	Value   Expr        `json:"-"`
	Fn      string      `json:"fn,omitempty"`
	Args    []Expr      `json:"-"`
	This    Expr        `json:"-"` // ecx at the call for helpers of unknown convention
	Seq     int         `json:"seq,omitempty"`
	Buffers []ArgBuffer `json:"-"`
}

// ArgBuffer snapshots stack memory a call argument points at.
type ArgBuffer struct {
	Index int
	Elems []Expr
}

// Path is one complete execution of a function under a set of guards.
type Path struct {
	Guards  []Expr
	Effects []Effect
	Return  Expr
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
	size int
	v    Expr
}

type flagState struct {
	kind string // cmp test alu fsw none
	a, b Expr
	r    Expr
	fsw  *fcmpRec
}

type fcmpRec struct{ a, b Expr }

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

// Execution limits that stop runaway analysis
const (
	maxSteps      = 60000
	maxPaths      = 512
	maxInlineSize = 4000
	maxFrameSteps = 2500
)

type execState struct {
	regs    [8]Expr
	mem     map[memKey]memVal
	fpu     []Expr
	fl      flagState
	fcmp    *fcmpRec
	axFSW   bool
	va      uint32
	guards  []Expr
	effects []Effect
	steps   int
	stack   []RetAddr
	frames  []uint32  // entry addresses of inlined callees
	fsteps  []int     // instructions executed inside each inlined frame
	entry   [][8]Expr // register values at entry of the outer function and each inlined frame
	pushes  int       // argument pushes since the last call boundary
	pending *pendingCall
	bound   map[string]Expr // atomic symbolic values fixed by earlier guards
}

// pendingCall remembers the arguments popped for a call of unknown arity so a later
// call can still reach the ones that were pushed for it in advance.
type pendingCall struct {
	shadow []Expr // candidate arguments, nearest the call first
}

// Executor runs x86 code symbolically over the disassembled image.
type Executor struct {
	d          *Disassembly
	seq        int
	paths      []Path
	vtables    map[uint32]uint32 // global object pointer VA to vtable VA
	symbols    map[uint32]*Symbol
	nameOf     map[uint32]string
	Opaque     map[uint32]bool // functions never inlined
	ftol       map[uint32]bool
	errCount   int
	allocHook  func() Expr     // fresh object pointer for the allocator when set
	written    map[uint32]bool // global bytes some instruction stores to directly
	Results    map[uint32]Expr // fixed return values for opaque functions
	Allocators map[uint32]bool // functions whose results are fresh objects
	savePush   map[uint32]bool // push instructions that save callee owned registers
	StepLimit  int             // per path instruction budget, zero means the default
	FrameLimit int             // per inlined frame budget, negative disables the budget
}

// NewExecutor prepares an executor with symbol and vtable knowledge.
func NewExecutor(d *Disassembly) *Executor {
	ex := &Executor{d: d, vtables: map[uint32]uint32{}, symbols: map[uint32]*Symbol{}, nameOf: map[uint32]string{}, Opaque: map[uint32]bool{}, ftol: map[uint32]bool{}, Results: map[uint32]Expr{}, Allocators: map[uint32]bool{}}
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
	}
	ex.resolveGlobalObjects()
	ex.scanWrittenGlobals()
	ex.scanPrologues()
	return ex
}

// Records the register saving pushes in every function prologue
func (ex *Executor) scanPrologues() {
	ex.savePush = map[uint32]bool{}
	for _, fn := range ex.d.Functions {
		for i := range fn.Instructions {
			in := &fn.Instructions[i]
			switch in.Inst.Op {
			case x86asm.PUSH:
				if r, ok := in.Inst.Args[0].(x86asm.Reg); ok && (r == x86asm.EBP || r == x86asm.EBX || r == x86asm.ESI || r == x86asm.EDI) {
					ex.savePush[in.VA] = true
					continue
				}
			case x86asm.MOV, x86asm.LEA, x86asm.SUB, x86asm.AND, x86asm.XOR, x86asm.FLD, x86asm.FLD1, x86asm.FLDZ, x86asm.FSTP:
				continue
			}
			break
		}
	}
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

// Finds functions invoked with a static object address in ecx
func (ex *Executor) staticThisCallers() map[uint32]uint32 {
	out := map[uint32]uint32{}
	for _, fn := range ex.d.Functions {
		ins := fn.Instructions
		var ecxImm uint32
		for i := range ins {
			in := &ins[i]
			if in.Inst.Op == x86asm.MOV && argCount(in) == 2 {
				if dst, ok := in.Inst.Args[0].(x86asm.Reg); ok && dst == x86asm.ECX {
					if imm, ok := in.Inst.Args[1].(x86asm.Imm); ok && ex.isDataAddress(uint32(imm)) {
						ecxImm = uint32(imm)
					} else if src, ok := in.Inst.Args[1].(x86asm.Reg); ok && src == x86asm.ESI && i > 0 {
						// mov ecx, esi after mov esi, imm keeps the static address
						ecxImm = staticRegImm(ins, i, x86asm.ESI, ex)
					} else {
						ecxImm = 0
					}
				}
			}
			if in.Inst.Op == x86asm.CALL && ecxImm != 0 {
				if t := resolveTarget(in); t != 0 {
					out[t] = ecxImm
				}
				ecxImm = 0
			}
		}
	}
	return out
}

// Returns the immediate most recently moved into a register before index i or 0
func staticRegImm(ins []Instruction, i int, r x86asm.Reg, ex *Executor) uint32 {
	for j := i - 1; j >= 0 && j > i-16; j-- {
		in := &ins[j]
		if in.Inst.Op == x86asm.MOV && argCount(in) == 2 {
			if dst, ok := in.Inst.Args[0].(x86asm.Reg); ok && dst == r {
				if imm, ok := in.Inst.Args[1].(x86asm.Imm); ok && ex.isDataAddress(uint32(imm)) {
					return uint32(imm)
				}
				return 0
			}
		}
	}
	return 0
}

// Reports whether an address lies in a writable data section
func (ex *Executor) isDataAddress(va uint32) bool {
	sec := ex.d.sectionOf(va)
	return sec != nil && sec.Flags&0x1 != 0
}

// Maps globals holding heap objects to the vtable their constructor installs
func (ex *Executor) resolveGlobalObjects() {
	staticThis := ex.staticThisCallers()
	for fnVA, fn := range ex.d.Functions {
		ins := fn.Instructions
		bases := map[x86asm.Reg]uint32{}
		if t, ok := staticThis[fnVA]; ok {
			bases[x86asm.ECX] = t
		}
		for i, in := range ins {
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
				t := resolveTarget(jn)
				if ctor, ok := ex.d.Functions[t]; ok {
					if vt := ctorVtable(ex.d, ctor); vt != 0 {
						ex.vtables[globalVA] = vt
					}
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
	for fnVA, thisVA := range callers {
		if seen[fnVA] {
			continue
		}
		seen[fnVA] = true
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
				s.regs[1] = Ptr{"", int32(thisVA)}
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
	return &Executor{d: base.d, vtables: base.vtables, symbols: base.symbols, nameOf: base.nameOf, Opaque: map[uint32]bool{}, ftol: base.ftol, Results: map[uint32]Expr{}, written: base.written, Allocators: base.Allocators, savePush: base.savePush}
}

// Returns the vtable a constructor stores at this+0 or 0
func ctorVtable(d *Disassembly, fn *Function) uint32 {
	thisRegs := map[x86asm.Reg]bool{x86asm.ECX: true}
	for _, in := range fn.Instructions {
		if in.Inst.Op == x86asm.MOV && argCount(&in) == 2 {
			if dst, ok := in.Inst.Args[0].(x86asm.Reg); ok {
				if src, ok := in.Inst.Args[1].(x86asm.Reg); ok && thisRegs[src] {
					thisRegs[dst] = true
				}
			}
			if mem, ok := in.Inst.Args[0].(x86asm.Mem); ok && thisRegs[mem.Base] && mem.Index == 0 && mem.Disp == 0 {
				if imm, ok := in.Inst.Args[1].(x86asm.Imm); ok {
					if d.isVtable(uint32(imm)) {
						return uint32(imm)
					}
				}
			}
		}
	}
	return 0
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
	n.mem = make(map[memKey]memVal, len(s.mem))
	for k, v := range s.mem {
		n.mem[k] = v
	}
	n.fpu = append([]Expr(nil), s.fpu...)
	n.guards = append([]Expr(nil), s.guards...)
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
		v = Sym{"reg." + r.String()}
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
		old = Sym{"reg." + r.String()}
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
	a = asPtr(a)
	p, ok := a.(Ptr)
	if !ok {
		s.effects = append(s.effects, Effect{Kind: "store", VA: va, Base: "?", Size: size, Value: v})
		return
	}
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
					s.mem[memKey{k.base, bo}] = memVal{4, Elem{mv.v, int(b / 4)}}
				}
			} else {
				// Partial overlap: split into bytes.
				for b := int32(0); b < int32(mv.size); b++ {
					bo := k.off + b
					if bo >= p.Off && bo < p.Off+int32(size) {
						continue
					}
					s.mem[memKey{k.base, bo}] = memVal{1, byteOf(mv.v, int(b), mv.size)}
				}
			}
		}
	}
	s.mem[memKey{p.Base, p.Off}] = memVal{size, v}
	if p.Base != "stack" {
		s.effects = append(s.effects, Effect{Kind: "store", VA: va, Base: p.Base, Off: p.Off, Size: size, Value: v})
	}
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
	a = asPtr(a)
	p, ok := a.(Ptr)
	if !ok {
		return Load{Addr: a, Size: size}
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

// Reads memory never written on this path from the image or as a symbolic input
func (ex *Executor) loadBacking(s *execState, p Ptr, size int) Expr {
	if p.Base == "" {
		va := uint32(p.Off)
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
	return Load{Base: p.Base, Off: p.Off, Size: size}
}

// Reports whether an expression already carries a floating point value
func isFloatExpr(e Expr) bool {
	switch v := e.(type) {
	case FConst:
		return true
	case Un:
		switch v.Op {
		case "f64", "f32", "bits2f", "bits2d", "i2f", "fneg", "fabs", "fsin", "fcos", "fsqrt":
			return true
		}
	case Bin:
		return v.Op == "fadd" || v.Op == "fsub" || v.Op == "fmul" || v.Op == "fdiv"
	}
	return false
}

func (ex *Executor) loadFloat(s *execState, a Expr, size int) Expr {
	a = asPtr(a)
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
			return mkUn("bits2f", ex.load(s, a, 4))
		}
		return Un{"f64", Load{Base: p.Base, Off: p.Off, Size: 8}}
	}
	return Un{"f64", Load{Addr: a, Size: size}}
}

func (ex *Executor) storeFloat(s *execState, a Expr, size int, v Expr, va uint32) {
	if size == 4 {
		v = mkUn("f32", v)
	}
	ex.store(s, a, size, v, va)
}

func (s *execState) push(v Expr) {
	s.regs[4] = mkBin("sub", s.regs[4], IConst{4})
	s.mem[memKey{stackBase(s), stackOff(s)}] = memVal{4, v}
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
	s := &execState{mem: map[memKey]memVal{}, va: entry}
	s.regs[4] = Ptr{"stack", 0x10000}
	s.regs[5] = Sym{"reg.EBP"}
	init(s)
	s.push(RetAddr{VA: 0, Clean: 0})
	s.entry = [][8]Expr{s.regs}
	ex.paths = nil
	err := ex.run(s)
	return ex.paths, err
}

// Reports whether a push preserves a callee saved register still holding its frame entry value
func (s *execState) savesRegister(a x86asm.Arg, v Expr) bool {
	r, ok := a.(x86asm.Reg)
	if !ok || (r != x86asm.EBP && r != x86asm.EBX && r != x86asm.ESI && r != x86asm.EDI) || len(s.entry) == 0 {
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
		if len(ex.paths) > maxPaths {
			return &ExecError{VA: s.va, Frames: s.frames, Limit: true, Err: fmt.Errorf("path limit exceeded"), Effects: s.effects}
		}
		in, ok := ex.d.InsnByVA[s.va]
		if !ok {
			return &ExecError{VA: s.va, Frames: s.frames, Err: fmt.Errorf("no instruction"), Effects: s.effects}
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
		next := s.va + uint32(in.Inst.Len)
		done, err := ex.step(s, in, next)
		if err != nil {
			if ee, ok := err.(*ExecError); ok {
				return ee
			}
			return &ExecError{VA: in.VA, Insn: in.Inst.String(), Frames: s.frames, Err: err, Effects: s.effects}
		}
		if done {
			return nil
		}
	}
}

// Pops the return address and either resumes the caller or finishes the path
func (ex *Executor) doReturn(s *execState, clean int32) (bool, error) {
	s.pending = nil
	s.pushes = 0
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

// Finishes a path when the outermost return executes
func (ex *Executor) finish(s *execState) {
	ex.paths = append(ex.paths, Path{Guards: s.guards, Effects: s.effects, Return: s.regs[0]})
}

// Forks on a symbolic condition and continues both outcomes
func (ex *Executor) branch(s *execState, cond Expr, taken, fall uint32) (bool, error) {
	if c, ok := isConst(cond); ok {
		if c != 0 {
			s.va = taken
		} else {
			s.va = fall
		}
		return false, nil
	}
	alt := s.clone()
	alt.guards = append(alt.guards, mkNot(cond))
	alt.bindFrom(mkNot(cond))
	alt.va = fall
	if err := ex.run(alt); err != nil {
		return true, err
	}
	s.guards = append(s.guards, cond)
	s.bindFrom(cond)
	s.va = taken
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
	s.bound[ExprString(atom)] = value
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
		ex.setOperand(s, in, args[0], opSize(in, args[0]), maskSize(v, opSize(in, args[0])))
		_ = size
	case x86asm.LEA:
		ex.setOperand(s, in, args[0], 4, ex.addr(s, args[1].(x86asm.Mem)))
	case x86asm.PUSH:
		v := ex.operand(s, in, args[0], 4)
		s.push(v)
		if !ex.savePush[in.VA] && !s.savesRegister(args[0], v) {
			s.pushes++
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
		if op == x86asm.SBB && sameArg(args[0], args[1]) && (s.fl.kind == "cmp" || s.fl.kind == "sub") {
			// sbb r, r after a compare materializes the borrow as all ones or zero.
			r := maskSize(mkUn("neg", mkCond("ult", s.fl.a, s.fl.b)), size)
			s.fl = flagState{kind: "alu", a: r, b: IConst{0}, r: r}
			ex.setOperand(s, in, args[0], size, r)
			return false, nil
		}
		if op == x86asm.ADC || op == x86asm.SBB {
			return false, fmt.Errorf("carry arithmetic unsupported")
		}
		name := map[x86asm.Op]string{x86asm.ADD: "add", x86asm.SUB: "sub", x86asm.AND: "and", x86asm.OR: "or", x86asm.XOR: "xor"}[op]
		var r Expr
		if op == x86asm.XOR && sameArg(args[0], args[1]) {
			r = IConst{0}
		} else {
			r = maskSize(mkBin(name, a, b), size)
		}
		s.fl = flagState{kind: name, a: a, b: b, r: r}
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
			s.fl = flagState{kind: "alu", a: a, b: IConst{0}, r: r}
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
		s.fl = flagState{kind: "alu", a: a, b: b, r: r}
		ex.setOperand(s, in, args[0], size, r)
	case x86asm.IMUL:
		size := opSize(in, args[0])
		switch nargs {
		case 1:
			return false, fmt.Errorf("one operand imul unsupported")
		case 2:
			a := ex.operand(s, in, args[0], size)
			b := ex.operand(s, in, args[1], size)
			ex.setOperand(s, in, args[0], size, mkBin("mul", a, b))
		default:
			a := ex.operand(s, in, args[1], size)
			b := ex.operand(s, in, args[2], size)
			ex.setOperand(s, in, args[0], size, mkBin("mul", a, b))
		}
	case x86asm.TEST:
		size := opSize(in, args[0])
		a := ex.operand(s, in, args[0], size)
		b := ex.operand(s, in, args[1], size)
		s.fl = flagState{kind: "test", a: a, b: b, r: mkBin("and", a, b)}
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
	case x86asm.SETE, x86asm.SETNE, x86asm.SETL, x86asm.SETGE, x86asm.SETLE, x86asm.SETG, x86asm.SETB, x86asm.SETAE, x86asm.SETBE, x86asm.SETA, x86asm.SETS, x86asm.SETNS, x86asm.SETP, x86asm.SETNP:
		c := ex.condition(s, op)
		ex.setOperand(s, in, args[0], 1, c)
	case x86asm.JMP:
		if rel, ok := args[0].(x86asm.Rel); ok {
			s.va = in.VA + uint32(in.Inst.Len) + uint32(int32(rel))
			return false, nil
		}
		// An indirect jump is a tail call followed by a return to the caller.
		if err := ex.call(s, in, 0); err != nil {
			return false, err
		}
		if s.va != 0 {
			return false, nil
		}
		return ex.doReturn(s, 0)
	case x86asm.JE, x86asm.JNE, x86asm.JL, x86asm.JGE, x86asm.JLE, x86asm.JG, x86asm.JB, x86asm.JAE, x86asm.JBE, x86asm.JA, x86asm.JS, x86asm.JNS, x86asm.JP, x86asm.JNP:
		rel := args[0].(x86asm.Rel)
		taken := in.VA + uint32(in.Inst.Len) + uint32(int32(rel))
		return ex.branch(s, ex.condition(s, op), taken, next)
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
			if !ok {
				return false, fmt.Errorf("rep stos with symbolic count")
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
	case x86asm.MOVSD, x86asm.MOVSB, x86asm.MOVSW:
		size := map[x86asm.Op]int{x86asm.MOVSD: 4, x86asm.MOVSB: 1, x86asm.MOVSW: 2}[op]
		if hasRep(in) {
			cnt, ok := isConst(s.getReg(x86asm.ECX))
			if !ok {
				return false, fmt.Errorf("rep movs with symbolic count")
			}
			for i := uint32(0); i < cnt; i++ {
				ex.movs(s, in, size)
			}
			s.setReg(x86asm.ECX, IConst{0})
		} else {
			ex.movs(s, in, size)
		}
	default:
		if isFPU(op) {
			return false, ex.fpuStep(s, in)
		}
		return false, fmt.Errorf("unsupported instruction")
	}
	return false, nil
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
	zero := func() Expr { return mkCond("eq", f.r, IConst{0}) }
	sign := func() Expr { return mkCond("lt", f.r, IConst{0}) }
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
		return mkCond("ult", f.a, f.b)
	case x86asm.JAE, x86asm.SETAE:
		return mkCond("uge", f.a, f.b)
	case x86asm.JBE, x86asm.SETBE:
		return mkCond("ule", f.a, f.b)
	case x86asm.JA, x86asm.SETA:
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
				return ex.opaqueCall(s, in, next, name, -1, false)
			}
			v := ex.load(s, addrE, 4)
			if c, ok := isConst(v); ok {
				target = c
			} else if a.Base != 0 {
				obj = s.getReg(a.Base)
				slot = int32(a.Disp)
			}
		} else if a.Base != 0 && a.Index == 0 {
			obj = s.getReg(a.Base)
			slot = int32(a.Disp)
			if vt := ex.vtableFor(s, obj); vt != 0 {
				if fp, ok := ex.d.Image.ReadU32(vt + uint32(slot)); ok {
					target = fp
				}
			}
		}
	case x86asm.Reg:
		v := s.getReg(a)
		if c, ok := isConst(v); ok {
			target = c
		}
		if p, ok := v.(Ptr); ok && strings.HasPrefix(p.Base, "import:") {
			return ex.opaqueCall(s, in, next, strings.TrimPrefix(p.Base, "import:"), -1, false)
		}
	}
	if target == 0 {
		if obj != nil {
			return ex.opaqueCall(s, in, next, fmt.Sprintf("vcall@%X", slot), -1, false)
		}
		return fmt.Errorf("unresolved call target")
	}
	if name, ok := ex.d.Imports[target]; ok {
		return ex.opaqueCall(s, in, next, name, -1, false)
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
	if ex.ftol[target] {
		s.pushes = 0
		v := s.fpuPop()
		s.setReg(x86asm.EAX, mkUn("f2i", v))
		s.setReg(x86asm.EDX, Sym{"ftol.hi"})
		return nil
	}
	if ex.Opaque[target] {
		name := fmt.Sprintf("sub_%08X", target)
		argBytes := calleeCleanupOf(fn)
		fast := false
		cdecl := false
		if fn.Symbol != nil {
			name, argBytes, fast, cdecl = fn.Symbol.Name, fn.Symbol.ArgBytes, fn.Symbol.FastCall, fn.Symbol.Cdecl
		}
		if err := ex.opaqueCall(s, in, next, name, argBytes, fast, cdecl); err != nil {
			return err
		}
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
		if argBytes < 0 && !sym.Cdecl && !sym.FastCall {
			// The callee's own ret immediate fixes the arity of a stdcall symbol.
			if clean := calleeCleanupOf(fn); clean > 0 {
				argBytes = clean
			}
		}
		return ex.opaqueCall(s, in, next, sym.Name, argBytes, sym.FastCall, sym.Cdecl)
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

// Looks up a vtable for an object pointer loaded from a known global
func (ex *Executor) vtableFor(s *execState, obj Expr) uint32 {
	if l, ok := obj.(Load); ok && l.Base == "" && l.Addr == nil {
		return ex.vtables[uint32(l.Off)]
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

// XDK functions that return a handle through a pointer argument
var outParams = map[string]int{"D3DDevice_CreateVertexShader": 2, "D3DDevice_CreateTexture": 6, "D3DDevice_CreatePixelShader": 1, "D3DDevice_CreateVertexBuffer": 3}

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
		return Load{Addr: a, Size: size}
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
	if p.Base == "" && ex.isConstantGlobal(uint32(p.Off), size) {
		for off := int32(0); off < int32(size); off += 4 {
			v, _ := ex.d.Image.ReadU32(uint32(p.Off + off))
			elems = append(elems, FConst{float64(math.Float32frombits(v))})
		}
		return Block{Elems: elems}
	}
	return Load{Base: p.Base, Off: p.Off, Size: size}
}

// Records a call to code outside the analysis with its arguments
func (ex *Executor) opaqueCall(s *execState, in *Instruction, next uint32, name string, argBytes int, fastcall bool, cdecl ...bool) error {
	callerCleans := len(cdecl) > 0 && cdecl[0]
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
		args = append(args, ex.load(s, mkBin("add", s.regs[4], IConst{uint32(4 * i)}), 4))
	}
	var this Expr
	if !fastcall && strings.HasPrefix(name, "sub_") {
		this = s.getReg(x86asm.ECX)
	}
	if argBytes < 0 && len(name) > 6 && name[:6] == "vcall@" {
		args = append(args, s.getReg(x86asm.ECX))
		for i := 0; i < s.pushes && i < 8; i++ {
			v := ex.load(s, mkBin("add", s.regs[4], IConst{uint32(4 * i)}), 4)
			if _, isRet := v.(RetAddr); isRet {
				break
			}
			args = append(args, v)
		}
	} else if argBytes < 0 {
		for i := 0; i < s.pushes && i < 8; i++ {
			v := ex.load(s, mkBin("add", s.regs[4], IConst{uint32(4 * i)}), 4)
			if _, isRet := v.(RetAddr); isRet {
				break
			}
			args = append(args, v)
		}
	}
	s.pending = nil
	popBytes := uint32(0)
	switch {
	case argBytes > 0 && missing > 0:
		popBytes = uint32(4 * k)
	case argBytes > 0:
		popBytes = uint32(argBytes)
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
	ex.seq++
	var ret Expr
	bufSize := d3dxBufferSize(name)
	if bufSize > 0 && len(args) > 0 {
		// Pointer arguments become the values they point at and the output buffer receives the result.
		vals := make([]Expr, len(args))
		for i, a := range args {
			if c, ok := isConst(a); ok && ex.isDataAddress(c) {
				a = Ptr{"", int32(c)}
				args[i] = a
			}
			if p, ok := a.(Ptr); ok && (i > 0 || !d3dxWritesArg0(name)) {
				vals[i] = ex.loadBlock(s, p, argBufferSize(name, i, bufSize))
			} else {
				vals[i] = a
			}
		}
		res := CallResult{Fn: name, Args: vals, Seq: ex.seq}
		if p, ok := args[0].(Ptr); ok && d3dxWritesArg0(name) {
			ex.store(s, p, bufSize, res, in.VA)
		}
		s.effects = append(s.effects, Effect{Kind: "call", VA: in.VA, Fn: name, Args: vals, Seq: ex.seq})
		ret = args[0]
	} else {
		res := CallResult{Fn: name, Args: args, Seq: ex.seq, This: this}
		s.effects = append(s.effects, Effect{Kind: "call", VA: in.VA, Fn: name, Args: args, Seq: ex.seq, This: this})
		ret = res
		if outIdx, ok := outParams[name]; ok && outIdx < len(args) {
			if p, ok := asPtr(args[outIdx]).(Ptr); ok {
				ex.store(s, p, 4, res, in.VA)
			}
		}
		for i, a := range args {
			if p, ok := a.(Ptr); ok && p.Base == "stack" {
				s.effects[len(s.effects)-1].Buffers = append(s.effects[len(s.effects)-1].Buffers, ArgBuffer{Index: i, Elems: ex.stackElems(s, p, 16)})
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
	}
	return result
}

// Reads consecutive stack entries starting at a pointer, blocks included, for call argument snapshots
func (ex *Executor) stackElems(s *execState, p Ptr, max int) []Expr {
	var out []Expr
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
		off += int32(mv.size)
	}
	return out
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
	case x86asm.FNSTSW:
		s.axFSW = s.fcmp != nil
		s.regs[0] = Sym{"fsw"}
	case x86asm.FTST:
		s.fcmp = &fcmpRec{a: s.st(0), b: FConst{0}}
	default:
		return fmt.Errorf("unsupported x87 instruction")
	}
	return nil
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
