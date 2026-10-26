package xbe

import (
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"

	"golang.org/x/arch/x86/x86asm"
)

// LiftedFunction is a routine the runtime runs block by block, its loops being the jumps the executable makes.
type LiftedFunction struct {
	Entry  string                  `json:"entry"`
	Regs   []string                `json:"input_regs"`
	Params []string                `json:"params,omitempty"`
	Blocks map[string]*LiftedBlock `json:"blocks"`
	blocks map[string]*liftedBlock
}

// LiftedBlock is one straight run of instructions, ending where the routine jumps, branches or returns.
type LiftedBlock struct {
	VA      uint32         `json:"va"`
	Insns   int            `json:"n,omitempty"`
	Effects []EffectJSON   `json:"effects"`
	Regs    map[string]int `json:"regs"`
	SP      int32          `json:"sp"`
	ESP     int            `json:"esp,omitempty"`
	FPU     []int          `json:"fpu"`
	XMM     map[string]int `json:"xmm,omitempty"`
	MM      map[string]int `json:"mm,omitempty"`
	Flags   *LiftedFlags   `json:"flags,omitempty"`
	FCmp    *LiftedFCmp    `json:"fcmp,omitempty"`
	Term    LiftedTerm     `json:"term"`
}

// LiftedFlags are the operands of the last flag setting instruction, an index of -1 standing for none.
type LiftedFlags struct {
	Kind string `json:"kind,omitempty"`
	A    int    `json:"a"`
	B    int    `json:"b"`
	R    int    `json:"r"`
	CF   int    `json:"cf"`
	FswA int    `json:"fsw_a"`
	FswB int    `json:"fsw_b"`
}

// LiftedFCmp is the pending float comparison the status word reports.
type LiftedFCmp struct {
	A     int  `json:"a"`
	B     int  `json:"b"`
	AxFSW bool `json:"ax_fsw"`
}

// LiftedTerm is how a block ends: a jump, a branch, a switch or the routine's return, and in a machine lift a jump
// through a computed address, a halt, a software interrupt or an interrupt return.
type LiftedTerm struct {
	Kind    string   `json:"kind"`
	Target  string   `json:"target,omitempty"`
	Cond    int      `json:"cond,omitempty"`
	Taken   string   `json:"taken,omitempty"`
	Fall    string   `json:"fall,omitempty"`
	Index   int      `json:"index,omitempty"`
	Targets []string `json:"targets,omitempty"`
	Value   int      `json:"value,omitempty"`
	Vector  int      `json:"vector,omitempty"`
}

// liftedBlock is a block before its expressions are pooled.
type liftedBlock struct {
	va      uint32
	insns   int
	effects []Effect
	regs    [8]Expr
	segs    map[string]Expr
	sp      int32
	spExpr  Expr // the stack pointer where the block moved it out of the frame
	fpu     []Expr
	xmm     map[string]Expr
	mm      map[string]Expr
	fl      flagState
	fcmp    *fcmpRec
	axFSW   bool
	rest    int32
	term    *blockTerm
	succs   []string
}

// blockTerm is the symbolic end of a block as the executor reached it.
type blockTerm struct {
	kind    string
	target  uint32
	cond    Expr
	taken   uint32
	fall    uint32
	index   Expr
	targets []uint32
	value   Expr
	vector  int
	ret     uint32 // the return address a call pushed before the jump, for walking a routine past its calls
}

// blockRun is the block the executor is lifting, with the leaders that end it and the instructions it owns.
type blockRun struct {
	start   uint32
	leaders map[uint32]bool
	inside  map[uint32]bool
	started bool
	term    *blockTerm
}

// Steps one lifted block may take, a block being straight line code and the helpers it executes in place
const maxBlockSteps = 20000

// Blocks one lifted routine may hold, its flag carrying duplicates included
const maxLiftedBlocks = 4096

// Names of the general registers in the executor's order
var registerNames = [8]string{"EAX", "ECX", "EDX", "EBX", "ESP", "EBP", "ESI", "EDI"}

// Stack pointer a lifted routine starts with, its return slot there and its arguments above it
const liftedEntrySP = 0x10000

// blockEntry is what the executor knows of the machine where a block starts.
type blockEntry struct {
	esp      int32
	espMoved bool // the stack pointer left the frame, so the block starts with it as a value
	ebp      int32
	ebpKnown bool
	rest     int32   // where the stack settles between calls, the argument words of a call in progress below it
	regs     [8]Expr // registers every way in leaves holding the same constant or address
	pred     *liftedBlock
}

// Reports whether a register value holds across blocks, as constants, fixed addresses and kernel export pointers do
func blockInvariant(v Expr) bool {
	switch p := v.(type) {
	case IConst:
		return true
	case Ptr:
		return p.Base == "" || p.Base == "stack" || (len(p.Base) == 9 && p.Base[0] == 'g') || strings.HasPrefix(p.Base, "import:")
	}
	return false
}

// Narrows an entry to what it shares with another way in, reporting whether anything changed
func (in *blockEntry) meet(other blockEntry) bool {
	changed := false
	for i := range in.regs {
		if in.regs[i] != nil && in.regs[i] != other.regs[i] {
			in.regs[i] = nil
			changed = true
		}
	}
	// The arguments a join agrees on are the ones every way in placed, the rest level sitting at the fewest
	if other.rest < in.rest {
		in.rest = other.rest
		changed = true
	}
	return changed
}

// The instruction a block starts at and every jump target or fall through after a jump in the routine
func blockLeaders(d *Disassembly, fn *Function) (map[uint32]bool, map[uint32]bool) {
	leaders := map[uint32]bool{fn.EntryVA: true}
	inside := map[uint32]bool{}
	sec := d.sectionOf(fn.EntryVA)
	for i := range fn.Instructions {
		in := &fn.Instructions[i]
		inside[in.VA] = true
		op := in.Inst.Op
		if !isJump(op) && op != x86asm.RET {
			continue
		}
		leaders[in.VA+uint32(in.Inst.Len)] = true
		if t := resolveTarget(in); t != 0 {
			leaders[t] = true
		}
		if op == x86asm.JMP && sec != nil {
			if m, ok := in.Inst.Args[0].(x86asm.Mem); ok && m.Index != 0 && m.Scale == 4 && m.Base == 0 {
				for _, t := range d.jumpTableTargets(in, sec) {
					leaders[t] = true
				}
			}
		}
	}
	return leaders, inside
}

// Instructions that decide something from the flags an earlier instruction left
func consumesFlags(in *Instruction) bool {
	op := in.Inst.Op
	switch {
	case isConditionalJump(op) && op != x86asm.JCXZ && op != x86asm.JECXZ:
		return true
	case op >= x86asm.SETA && op <= x86asm.SETS:
		return true
	case op >= x86asm.CMOVA && op <= x86asm.CMOVS:
		return true
	}
	switch op {
	case x86asm.ADC, x86asm.SBB, x86asm.LAHF, x86asm.RCL, x86asm.RCR:
		return true
	}
	return false
}

// Instructions that leave flags the executor models
func producesFlags(in *Instruction) bool {
	switch in.Inst.Op {
	case x86asm.ADD, x86asm.SUB, x86asm.AND, x86asm.OR, x86asm.XOR, x86asm.ADC, x86asm.SBB, x86asm.INC, x86asm.DEC, x86asm.NEG,
		x86asm.CMP, x86asm.TEST, x86asm.SHL, x86asm.SHR, x86asm.SAR, x86asm.SHRD, x86asm.SHLD, x86asm.IMUL, x86asm.MUL, x86asm.SAHF,
		x86asm.COMISS, x86asm.UCOMISS, x86asm.FCOMI, x86asm.FCOMIP, x86asm.FUCOMI, x86asm.FUCOMIP, x86asm.CMPSB, x86asm.CMPSW, x86asm.CMPSD,
		x86asm.XADD, x86asm.CMPXCHG, x86asm.BSF, x86asm.BSR, x86asm.BT:
		return true
	}
	return false
}

// Instructions reading the status word a float compare left in ax
func consumesStatusWord(in *Instruction) bool {
	if in.Inst.Op == x86asm.SAHF {
		return true
	}
	if in.Inst.Op != x86asm.TEST || argCount(in) < 1 {
		return false
	}
	r, ok := in.Inst.Args[0].(x86asm.Reg)
	return ok && (r == x86asm.AH || r == x86asm.AX)
}

// Instructions that settle the float compare state before the status word is read
func producesStatusWord(in *Instruction) bool {
	switch in.Inst.Op {
	case x86asm.FNSTSW, x86asm.LAHF, x86asm.FCOM, x86asm.FCOMP, x86asm.FCOMPP, x86asm.FUCOM, x86asm.FUCOMP, x86asm.FUCOMPP, x86asm.FICOM, x86asm.FICOMP, x86asm.FTST, x86asm.CALL:
		return true
	}
	return false
}

// Reports whether a block decides on flags or a status word its predecessor left
func (fn *Function) needsEntryFlags(index map[uint32]int, leaders map[uint32]bool, va uint32) bool {
	i, ok := index[va]
	if !ok {
		return false
	}
	flags, fsw := true, true
	for ; i < len(fn.Instructions); i++ {
		in := &fn.Instructions[i]
		if in.VA != va && leaders[in.VA] {
			break
		}
		if flags && consumesFlags(in) {
			return true
		}
		if fsw && consumesStatusWord(in) {
			return true
		}
		if producesFlags(in) {
			flags = false
		}
		if producesStatusWord(in) {
			fsw = false
		}
		if !flags && !fsw {
			break
		}
		if isJump(in.Inst.Op) || in.Inst.Op == x86asm.RET {
			break
		}
	}
	return false
}

// The flags a block starts with when its predecessor's last flag setter decides its first branch
func entryFlags(pred *liftedBlock) (flagState, *fcmpRec, bool) {
	fl := flagState{kind: pred.fl.kind}
	if pred.fl.a != nil {
		fl.a = Sym{"flags.a"}
	}
	if pred.fl.b != nil {
		fl.b = Sym{"flags.b"}
	}
	if pred.fl.r != nil {
		fl.r = Sym{"flags.r"}
	}
	if pred.fl.cf != nil {
		fl.cf = Sym{"flags.cf"}
	}
	if pred.fl.fsw != nil {
		fl.fsw = &fcmpRec{a: Sym{"flags.fsw_a"}, b: Sym{"flags.fsw_b"}}
	}
	var fcmp *fcmpRec
	if pred.fcmp != nil {
		fcmp = &fcmpRec{a: Sym{"fcmp.a"}, b: Sym{"fcmp.b"}}
	}
	return fl, fcmp, pred.axFSW
}

// Lift runs a routine block by block with every input symbolic, keeping its control flow for the runtime to follow,
// the blocks lifted before a failure coming back beside the error
func (ex *Executor) Lift(entry uint32) (*LiftedFunction, error) {
	fn := ex.d.Functions[entry]
	if fn == nil {
		return nil, fmt.Errorf("lift %08X: not disassembled", entry)
	}
	leaders, inside := blockLeaders(ex.d, fn)
	index := fn.instructionIndex()
	lf := &LiftedFunction{Entry: blockKey(entry, nil), Regs: []string{}, Blocks: map[string]*LiftedBlock{}, blocks: map[string]*liftedBlock{}}
	for _, r := range ex.d.registerInputs(entry) {
		if i, ok := xmmIndex(r); ok {
			lf.Regs = append(lf.Regs, fmt.Sprintf("XMM%d", i))
		} else {
			lf.Regs = append(lf.Regs, r.String())
		}
	}
	entries := map[string]blockEntry{lf.Entry: {esp: liftedEntrySP, rest: liftedEntrySP}}
	queue := []string{lf.Entry}
	queued := map[string]bool{lf.Entry: true}
	lifts := 0
	for len(queue) > 0 {
		key := queue[0]
		queue = queue[1:]
		delete(queued, key)
		lifts++
		if len(lf.blocks) >= maxLiftedBlocks || lifts > 4*maxLiftedBlocks {
			return lf, fmt.Errorf("lift %08X: more than %d blocks", entry, maxLiftedBlocks)
		}
		in := entries[key]
		b, err := ex.liftBlock(fn, leaders, inside, blockKeyVA(key), in)
		if err != nil {
			return lf, fmt.Errorf("lift %08X block %s: %w", entry, key, err)
		}
		lf.blocks[key] = b
		exit := blockEntry{esp: b.sp, rest: b.rest, espMoved: b.spExpr != nil}
		if p, ok := b.regs[5].(Ptr); ok && p.Base == "stack" {
			exit.ebp, exit.ebpKnown = p.Off, true
		} else if in.ebpKnown && b.regs[5] == nil {
			exit.ebp, exit.ebpKnown = in.ebp, true
		}
		for i := range exit.regs {
			if i == 4 || i == 5 {
				continue
			}
			v := b.regs[i]
			if v == nil {
				v = in.regs[i]
			}
			if blockInvariant(v) {
				exit.regs[i] = v
			}
		}
		// A successor seen before is narrowed to what this way in shares with the others and lifted again if that changed
		link := func(va uint32) string {
			next := exit
			var succ string
			if fn.needsEntryFlags(index, leaders, va) {
				next.pred = b
				succ = blockKey(va, b)
			} else {
				succ = blockKey(va, nil)
			}
			prev, seen := entries[succ]
			// A block entered with the stack at another depth is lifted again for that way in
			if seen && (prev.esp != next.esp || prev.espMoved != next.espMoved || prev.ebpKnown != next.ebpKnown || prev.ebp != next.ebp) {
				depth := fmt.Sprintf("%X", uint32(next.esp))
				if next.espMoved {
					depth = "moved"
				}
				succ = fmt.Sprintf("%s~%s_%X", succ, depth, uint32(next.ebp))
				prev, seen = entries[succ]
			}
			if !seen {
				entries[succ] = next
			} else {
				if !prev.meet(next) {
					return succ
				}
				entries[succ] = prev
			}
			if !queued[succ] {
				queued[succ] = true
				queue = append(queue, succ)
			}
			return succ
		}
		t := b.term
		switch t.kind {
		case "trap", "halt", "noreturn":
		case "jump":
			b.succs = []string{link(t.target)}
		case "branch":
			b.succs = []string{link(t.taken), link(t.fall)}
		case "switch":
			for _, va := range t.targets {
				b.succs = append(b.succs, link(va))
			}
		}
		if err != nil {
			return lf, err
		}
	}
	return lf, nil
}

// The key a block is lifted under, its predecessor's key joined on when it reads that block's flags
func blockKey(va uint32, pred *liftedBlock) string {
	if pred == nil {
		return fmt.Sprintf("%08X", va)
	}
	return fmt.Sprintf("%08X@%08X", va, pred.va)
}

// The address a block key starts at
func blockKeyVA(key string) uint32 {
	var va uint32
	fmt.Sscanf(key, "%X", &va)
	return va
}

// Runs one block from a symbolic entry state and reads the state it leaves
func (ex *Executor) liftBlock(fn *Function, leaders, inside map[uint32]bool, va uint32, in blockEntry) (*liftedBlock, error) {
	s := &execState{mem: map[memKey]memVal{}, va: va, root: fn.EntryVA}
	s.regs = in.regs
	s.regs[4] = Ptr{"stack", in.esp}
	if in.espMoved {
		s.regs[4] = Sym{"reg.ESP"}
	}
	s.regs[5] = nil
	if in.ebpKnown {
		s.regs[5] = Ptr{"stack", in.ebp}
	}
	for i := 0; i < 8; i++ {
		s.fpu = append(s.fpu, Sym{fmt.Sprintf("fpu.st%d", i)})
	}
	if in.pred != nil {
		s.fl, s.fcmp, s.axFSW = entryFlags(in.pred)
	}
	s.rest, s.restBase = in.rest, "stack"
	s.entry = [][8]Expr{s.regs}
	ex.block = &blockRun{start: va, leaders: leaders, inside: inside}
	ex.paths = nil
	limit := ex.StepLimit
	ex.StepLimit = maxBlockSteps
	err := ex.run(s)
	ex.StepLimit = limit
	run := ex.block
	ex.block = nil
	if err != nil {
		return nil, err
	}
	if run.term == nil {
		return nil, fmt.Errorf("block %08X ends without a jump, branch or return", va)
	}
	b := &liftedBlock{va: va, effects: s.effects, term: run.term, fl: s.fl, fcmp: s.fcmp, axFSW: s.axFSW, rest: s.rest, xmm: map[string]Expr{}, mm: map[string]Expr{}, segs: map[string]Expr{}}
	for i, v := range s.segs {
		if v != nil {
			b.segs[segNames[i]] = v
		}
	}
	if sp, ok := s.regs[4].(Ptr); ok && sp.Base == "stack" {
		b.sp = sp.Off
	} else {
		b.spExpr = s.regs[4]
	}
	for i, v := range s.regs {
		if i == 4 || v == nil {
			continue
		}
		if _, isRet := v.(RetAddr); isRet {
			return nil, fmt.Errorf("block %08X leaves a return address in %s", va, registerNames[i])
		}
		b.regs[i] = v
	}
	for i := range s.fpu {
		b.fpu = append(b.fpu, s.fpu[i])
	}
	for i := range s.xmm {
		for lane := range s.xmm[i] {
			if s.xmm[i][lane] != nil {
				b.xmm[fmt.Sprintf("xmm%d.%d", i, lane)] = s.xmm[i][lane]
			}
		}
	}
	for i := range s.mm {
		for lane := range s.mm[i] {
			if s.mm[i][lane] != nil {
				b.mm[fmt.Sprintf("mm%d.%d", i, lane)] = s.mm[i][lane]
			}
		}
	}
	return b, nil
}

// Every effect a lifted routine's blocks record, in block key order
func (lf *LiftedFunction) allEffects() []Effect {
	var out []Effect
	for _, key := range slices.Sorted(maps.Keys(lf.blocks)) {
		out = append(out, lf.blocks[key].effects...)
	}
	return out
}

// The blocks of a lifted routine read as paths, each block's branch standing as a guard, for the analyses walking paths
func (lf *LiftedFunction) asPaths() []Path {
	var out []Path
	for _, key := range slices.Sorted(maps.Keys(lf.blocks)) {
		b := lf.blocks[key]
		p := Path{Effects: b.effects}
		switch b.term.kind {
		case "branch":
			p.Guards = []Expr{b.term.cond}
			p.GuardPos = []int{len(b.effects)}
		case "switch":
			p.Guards = []Expr{b.term.index}
			p.GuardPos = []int{len(b.effects)}
		case "return":
			p.Return = b.term.value
		}
		out = append(out, p)
	}
	return out
}

// Pools the expressions of every block so the routine serializes beside the path summaries
func (lf *LiftedFunction) serialize(pool *ExprPool) {
	keys := make([]string, 0, len(lf.blocks))
	for k := range lf.blocks {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	in := pool.interner()
	for _, key := range keys {
		lf.Blocks[key] = serializeBlock(in, lf.blocks[key])
	}
}

// Serializes one block with its expressions pooled through the interner given
func serializeBlock(in *interner, b *liftedBlock) *LiftedBlock {
	lb := &LiftedBlock{VA: b.va, Insns: b.insns, Effects: effectsJSON(in, b.effects), Regs: map[string]int{}, SP: b.sp, FPU: []int{}}
	if b.spExpr != nil {
		lb.ESP = in.add(b.spExpr)
	}
	for i, v := range b.regs {
		if v != nil {
			lb.Regs[registerNames[i]] = in.add(v)
		}
	}
	for name, v := range b.segs {
		lb.Regs[name] = in.add(v)
	}
	for _, v := range b.fpu {
		lb.FPU = append(lb.FPU, in.add(v))
	}
	if len(b.xmm) > 0 {
		lb.XMM = map[string]int{}
		for name, v := range b.xmm {
			lb.XMM[name] = in.add(v)
		}
	}
	if len(b.mm) > 0 {
		lb.MM = map[string]int{}
		for name, v := range b.mm {
			lb.MM[name] = in.add(v)
		}
	}
	lb.Flags = b.flagsJSON(in)
	if b.fcmp != nil {
		lb.FCmp = &LiftedFCmp{A: in.add(b.fcmp.a), B: in.add(b.fcmp.b), AxFSW: b.axFSW}
	}
	t := b.term
	lb.Term = LiftedTerm{Kind: t.kind}
	switch t.kind {
	case "jump", "hlt":
		lb.Term.Target = b.succs[0]
	case "int":
		lb.Term.Target, lb.Term.Vector = b.succs[0], t.vector
	case "branch":
		lb.Term.Cond, lb.Term.Taken, lb.Term.Fall = in.add(t.cond), b.succs[0], b.succs[1]
	case "switch":
		lb.Term.Index, lb.Term.Targets = in.add(t.index), b.succs
	case "return":
		lb.Term.Value = in.add(t.value)
	case "jumpind":
		lb.Term.Index = in.add(t.index)
	}
	return lb
}

// Pools an expression that may be absent, which is written as -1
func optional(in *interner, e Expr) int {
	if e == nil {
		return -1
	}
	return in.add(e)
}

// Serializes effects with their expressions as pool indices
func effectsJSON(in *interner, effects []Effect) []EffectJSON {
	out := []EffectJSON{}
	for _, e := range effects {
		ej := EffectJSON{Kind: e.Kind, VA: e.VA, Base: e.Base, Off: e.Off, Size: e.Size, Fn: e.Fn, Seq: e.Seq, Target: e.Target}
		if e.Value != nil {
			ej.Value = in.add(e.Value)
		}
		if e.Addr != nil {
			ej.Addr = in.add(e.Addr)
			ej.HasAddr = true
		}
		for _, a := range e.Args {
			ej.Args = append(ej.Args, in.add(a))
		}
		if e.This != nil {
			ej.This = in.add(e.This)
			ej.HasThis = true
		}
		for _, b := range e.Buffers {
			bj := BufferJSON{Index: b.Index, Elems: []int{}, Sizes: append([]int{}, b.Sizes...)}
			for _, el := range b.Elems {
				bj.Elems = append(bj.Elems, in.add(el))
			}
			ej.Buffers = append(ej.Buffers, bj)
		}
		out = append(out, ej)
	}
	return out
}

// Describes a lifted routine for the dump output, each node a block shares defined once as tN
func (lf *LiftedFunction) String() string {
	var sb strings.Builder
	for _, key := range slices.Sorted(maps.Keys(lf.blocks)) {
		b := lf.blocks[key]
		p := &graphPrinter{sb: &sb, refs: map[exprKey]int{}, names: map[exprKey]string{}}
		for _, e := range b.shown() {
			p.count(e)
		}
		if b.spExpr != nil {
			fmt.Fprintf(&sb, "block %s\n", key)
			p.line("ESP = %s", p.render(b.spExpr))
		} else {
			fmt.Fprintf(&sb, "block %s sp=%X\n", key, b.sp)
		}
		for _, e := range b.effects {
			p.line("%s", effectString(e, p.render))
		}
		for i, v := range b.regs {
			if v != nil {
				p.line("%s = %s", registerNames[i], p.render(v))
			}
		}
		for _, name := range segNames {
			if v, ok := b.segs[name]; ok {
				p.line("%s = %s", name, p.render(v))
			}
		}
		t := b.term
		switch t.kind {
		case "jump":
			p.line("jump %s", b.succs[0])
		case "branch":
			p.line("branch %s ? %s : %s", p.render(t.cond), b.succs[0], b.succs[1])
		case "switch":
			p.line("switch %s %v", p.render(t.index), b.succs)
		case "return":
			p.line("return %s", p.render(t.value))
		case "trap":
			p.line("trap")
		case "halt":
			p.line("halt")
		case "noreturn":
			p.line("noreturn")
		case "jumpind":
			p.line("jump to %s", p.render(t.index))
		case "hlt":
			p.line("hlt then %s", b.succs[0])
		case "int":
			p.line("int %d then %s", t.vector, b.succs[0])
		case "iret":
			p.line("iret")
		}
	}
	return sb.String()
}

// The expressions the dump prints for a block
func (b *liftedBlock) shown() []Expr {
	out := []Expr{b.spExpr}
	for _, e := range b.effects {
		out = append(out, e.Value, e.Addr)
		out = append(out, e.Args...)
	}
	out = append(out, b.regs[:]...)
	for _, name := range segNames {
		out = append(out, b.segs[name])
	}
	return append(out, b.term.cond, b.term.index, b.term.value)
}

// graphPrinter renders a block's expressions with every compound node the block shares named once
type graphPrinter struct {
	sb    *strings.Builder
	refs  map[exprKey]int
	names map[exprKey]string
}

// Counts the references each node receives, descending into a node on its first reference
func (p *graphPrinter) count(e Expr) {
	if e == nil {
		return
	}
	k := keyOf(e)
	p.refs[k]++
	if p.refs[k] == 1 {
		eachChild(e, p.count)
	}
}

// Renders an expression, a compound node referenced more than once defined on its first use and named after
func (p *graphPrinter) render(e Expr) string {
	if e == nil || !hasChildren(e) || p.refs[keyOf(e)] < 2 {
		return formatExpr(e, p.render)
	}
	k := keyOf(e)
	if name, ok := p.names[k]; ok {
		return name
	}
	body := formatExpr(e, p.render)
	name := fmt.Sprintf("t%d", len(p.names))
	p.names[k] = name
	p.line("%s = %s", name, body)
	return name
}

// Writes one indented line of the dump
func (p *graphPrinter) line(format string, args ...any) {
	fmt.Fprintf(p.sb, "  "+format+"\n", args...)
}

// Renders one effect in the dump notation with the expression renderer given
func effectString(e Effect, str func(Expr) string) string {
	switch e.Kind {
	case "store":
		if e.Addr != nil {
			return fmt.Sprintf("mem%d[%s] = %s", e.Size, str(e.Addr), str(e.Value))
		}
		return fmt.Sprintf("%s.%d@0x%X = %s", e.Base, e.Size, e.Off, str(e.Value))
	case "copy":
		return fmt.Sprintf("copy %s x%d from %s to %s", str(e.Value), e.Size, str(e.Args[0]), str(e.Addr))
	case "fill":
		return fmt.Sprintf("fill %s x%d with %s at %s", str(e.Args[0]), e.Size, str(e.Value), str(e.Addr))
	}
	args := make([]string, len(e.Args))
	for i, a := range e.Args {
		args[i] = str(a)
	}
	return fmt.Sprintf("%s#%d(%s)", e.Fn, e.Seq, strings.Join(args, ", "))
}
