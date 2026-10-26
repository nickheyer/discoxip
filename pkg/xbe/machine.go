package xbe

import (
	"encoding/binary"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"

	"golang.org/x/arch/x86/x86asm"
)

// MachineModel is an image lifted as the processor runs it, every block keyed by its address and the stack held in memory
type MachineModel struct {
	Base      uint32                  `json:"base"`
	Blocks    map[string]*LiftedBlock `json:"blocks"`
	Errors    map[string]string       `json:"errors"`
	Exprs     *ExprPool               `json:"exprs"`
	Templates []*CodeTemplate         `json:"templates"`
	blocks    map[string]*liftedBlock
	routines  map[uint32]bool
}

// leaderSet is every address a block may start at, the ones entered through a return, a pointer or an export marked
// as entered with nothing known about the registers, and the walk that finds more from each
type leaderSet struct {
	d       *Disassembly
	leaders map[uint32]bool
	dynamic map[uint32]bool
	queue   []uint32
}

func newLeaderSet(d *Disassembly) *leaderSet {
	return &leaderSet{d: d, leaders: map[uint32]bool{}, dynamic: map[uint32]bool{}}
}

func (ls *leaderSet) add(va uint32, dyn bool) {
	if ls.d.insnAt(va) == nil {
		return
	}
	if dyn {
		ls.dynamic[va] = true
	}
	if !ls.leaders[va] {
		ls.leaders[va] = true
		ls.queue = append(ls.queue, va)
	}
}

// Walks the code from every leader not walked yet, each jump, call and code pointer met adding a leader
func (ls *leaderSet) walk() {
	d := ls.d
	for len(ls.queue) > 0 {
		va := ls.queue[0]
		ls.queue = ls.queue[1:]
		for {
			in := d.insnAt(va)
			if in == nil {
				break
			}
			next := va + uint32(in.Inst.Len)
			op := in.Inst.Op
			for _, a := range in.Inst.Args {
				switch v := a.(type) {
				case x86asm.Imm:
					if op != x86asm.INT && op != x86asm.RET && op != x86asm.LRET && d.plausibleEntry(uint32(v)) {
						ls.add(uint32(v), true)
					}
				case x86asm.Mem:
					if op == x86asm.LEA && v.Base == 0 && v.Index == 0 && d.plausibleEntry(uint32(v.Disp)) {
						ls.add(uint32(v.Disp), true)
					}
				}
			}
			switch {
			case op == x86asm.CALL:
				if t := resolveTarget(in); t != 0 {
					ls.add(t, true)
				}
				ls.add(next, true)
			case op == x86asm.LJMP:
				if off, ok := in.Inst.Args[1].(x86asm.Imm); ok {
					ls.add(uint32(off), false)
				}
			case isConditionalJump(op) || op == x86asm.LOOP || op == x86asm.LOOPE || op == x86asm.LOOPNE:
				if t := resolveTarget(in); t != 0 {
					ls.add(t, false)
				}
				ls.add(next, false)
			case op == x86asm.JMP:
				if t := resolveTarget(in); t != 0 {
					ls.add(t, false)
				} else if sec := d.sectionOf(in.VA); sec != nil {
					for _, t := range d.jumpTableTargets(in, sec) {
						ls.add(t, false)
					}
				}
			case op == x86asm.HLT || op == x86asm.INT || op == x86asm.STI || op == x86asm.POPFD:
				ls.add(next, false)
			}
			if op == x86asm.RET || op == x86asm.LRET || op == x86asm.JMP || op == x86asm.LJMP || op == x86asm.IRETD || op == x86asm.UD2 || op == x86asm.CALL || op == x86asm.HLT || op == x86asm.INT || op == x86asm.STI || op == x86asm.POPFD {
				break
			}
			if ls.leaders[next] {
				break
			}
			va = next
		}
	}
}

// machineLeaders collects the leaders the roots and the image's code pointers reach
func (d *Disassembly) machineLeaders(roots []uint32) *leaderSet {
	ls := newLeaderSet(d)
	for _, va := range roots {
		ls.add(va, true)
	}
	for _, va := range d.codePointers() {
		if d.plausibleEntry(va) {
			ls.add(va, true)
		}
	}
	ls.walk()
	return ls
}

// Reports whether an address lies in a section the processor executes and starts an instruction there
func (d *Disassembly) executableAt(va uint32) bool {
	sec := d.sectionOf(va)
	return sec != nil && sec.Flags&0x4 != 0 && d.insnAt(va) != nil
}

// Every aligned word of the image that holds the address of a routine in an executable section, a word beside
// another code address counting as a table entry whatever precedes the routine
func (d *Disassembly) codePointers() []uint32 {
	var out []uint32
	for i := range d.Image.Sections {
		sec := &d.Image.Sections[i]
		for off := 0; off+4 <= len(sec.Data); off += 4 {
			va := binary.LittleEndian.Uint32(sec.Data[off:])
			if !d.executableAt(va) {
				continue
			}
			if d.plausibleEntry(va) {
				out = append(out, va)
				continue
			}
			before := off >= 4 && d.executableAt(binary.LittleEndian.Uint32(sec.Data[off-4:]))
			after := off+8 <= len(sec.Data) && d.executableAt(binary.LittleEndian.Uint32(sec.Data[off+4:]))
			if before || after {
				out = append(out, va)
			}
		}
	}
	return out
}

// The instruction at an address, decoded from the image where the sweep left none there
func (d *Disassembly) insnAt(va uint32) *Instruction {
	if in := d.InsnByVA[va]; in != nil {
		return in
	}
	sec := d.sectionOf(va)
	if sec == nil || sec.Flags&0x4 == 0 {
		return nil
	}
	inst, err := x86asm.Decode(sec.Data[va-sec.VirtualAddr:], 32)
	if err != nil || inst.Len == 0 {
		return nil
	}
	raw := make([]byte, inst.Len)
	copy(raw, sec.Data[va-sec.VirtualAddr:va-sec.VirtualAddr+uint32(inst.Len)])
	in := &Instruction{VA: va, Inst: inst, Raw: raw}
	d.InsnByVA[va] = in
	return in
}

// Reports whether a block decides on flags left by whichever block ran before it
func (d *Disassembly) machineNeedsEntryFlags(leaders map[uint32]bool, va uint32) bool {
	for at := va; ; {
		in := d.insnAt(at)
		if in == nil || (at != va && leaders[at]) {
			return false
		}
		if consumesFlags(in) || consumesStatusWord(in) {
			return true
		}
		if producesFlags(in) || producesStatusWord(in) {
			return false
		}
		if isJump(in.Inst.Op) || in.Inst.Op == x86asm.RET || in.Inst.Op == x86asm.CALL {
			return false
		}
		at += uint32(in.Inst.Len)
	}
}

// LiftMachine lifts every block the roots reach, a block ending at each jump, call, return or halt, so the loader runs
// the image with the processor's own stack and control flow
func LiftMachine(d *Disassembly, roots []uint32) (*MachineModel, error) {
	return LiftMachineInto(d, roots, NewExprPool())
}

// LiftMachineInto lifts an image the way LiftMachine does, pooling its expressions with those of other images
func LiftMachineInto(d *Disassembly, roots []uint32, pool *ExprPool) (*MachineModel, error) {
	ex := NewExecutor(d)
	ex.machine = true
	resultPointers = false
	defer func() { resultPointers = true }()
	ls := d.machineLeaders(roots)
	leaders, dynamic := ls.leaders, ls.dynamic
	mm := &MachineModel{Base: d.Image.BaseAddr, Blocks: map[string]*LiftedBlock{}, Errors: map[string]string{}, Exprs: pool, Templates: []*CodeTemplate{}, blocks: map[string]*liftedBlock{}, routines: map[uint32]bool{}}
	entries := map[string]blockEntry{}
	queued := map[string]bool{}
	var queue []string
	enqueue := func(key string) {
		if !queued[key] {
			queued[key] = true
			queue = append(queue, key)
		}
	}
	// A leader deciding on flags at entry is lifted per predecessor, unless it is entered from anywhere and reads them live
	seeded := map[uint32]bool{}
	seed := func() {
		vas := make([]int, 0, len(leaders))
		for va := range leaders {
			if !seeded[va] {
				vas = append(vas, int(va))
			}
		}
		sort.Ints(vas)
		for _, va := range vas {
			seeded[uint32(va)] = true
			if d.machineNeedsEntryFlags(leaders, uint32(va)) && !dynamic[uint32(va)] {
				continue
			}
			key := blockKey(uint32(va), nil)
			if _, seen := entries[key]; !seen {
				entries[key] = blockEntry{espMoved: true}
			}
			enqueue(key)
		}
	}
	seed()
	for len(queue) > 0 {
		key := queue[0]
		queue = queue[1:]
		delete(queued, key)
		in := entries[key]
		va := blockKeyVA(key)
		b, err := ex.liftMachineBlock(leaders, va, in, dynamic[va] && in.pred == nil)
		if err != nil {
			mm.Errors[key] = err.Error()
			continue
		}
		mm.blocks[key] = b
		exit := blockEntry{espMoved: true}
		for i := range exit.regs {
			if i == 4 {
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
		// A successor entered through a return or a pointer as well takes nothing from this way in, and one the walk
		// never reached is walked from now, the leaders it finds joining the lift
		link := func(target uint32) string {
			if !leaders[target] {
				ls.add(target, false)
				ls.walk()
				seed()
			}
			next := exit
			if dynamic[target] {
				next = blockEntry{espMoved: true}
			}
			succ := blockKey(target, nil)
			if d.machineNeedsEntryFlags(leaders, target) {
				next.pred = b
				succ = blockKey(target, b)
			}
			prev, seen := entries[succ]
			if !seen {
				entries[succ] = next
				enqueue(succ)
				return succ
			}
			if prev.meet(next) {
				entries[succ] = prev
				enqueue(succ)
			}
			return succ
		}
		t := b.term
		switch t.kind {
		case "jump", "hlt", "int":
			b.succs = []string{link(t.target)}
		case "branch":
			b.succs = []string{link(t.taken), link(t.fall)}
		case "switch":
			for _, target := range t.targets {
				b.succs = append(b.succs, link(target))
			}
		}
	}
	for va := range dynamic {
		mm.routines[va] = true
	}
	mm.serialize()
	return mm, nil
}

// Runs one block from an entry holding the stack pointer as a value, the flags read live when nothing static set them
func (ex *Executor) liftMachineBlock(leaders map[uint32]bool, va uint32, in blockEntry, live bool) (*liftedBlock, error) {
	s := &execState{mem: map[memKey]memVal{}, va: va, root: va}
	s.regs = in.regs
	s.regs[4] = Sym{"reg.ESP"}
	for i := 0; i < 8; i++ {
		s.fpu = append(s.fpu, Sym{fmt.Sprintf("fpu.st%d", i)})
	}
	if in.pred != nil {
		s.fl, s.fcmp, s.axFSW = entryFlags(in.pred)
	} else if live {
		s.fl = flagState{kind: "machine", a: Sym{"flags.a"}, b: Sym{"flags.b"}, r: Sym{"flags.r"}, cf: Sym{"flags.cf"}}
	}
	s.rest, s.restBase = 0, ""
	s.machineDF = true
	s.entry = [][8]Expr{s.regs}
	ex.block = &blockRun{start: va, leaders: leaders}
	ex.paths = nil
	limit := ex.StepLimit
	ex.StepLimit = maxBlockSteps
	err := ex.runRecovering(s)
	ex.StepLimit = limit
	run := ex.block
	ex.block = nil
	if err != nil {
		return nil, err
	}
	if run.term == nil {
		return nil, fmt.Errorf("block %08X ends without a jump, branch or return", va)
	}
	b := &liftedBlock{va: va, insns: s.steps, effects: s.effects, term: run.term, fl: s.fl, fcmp: s.fcmp, axFSW: s.axFSW, xmm: map[string]Expr{}, mm: map[string]Expr{}, segs: map[string]Expr{}}
	for i, v := range s.segs {
		if v != nil {
			b.segs[segNames[i]] = v
		}
	}
	b.spExpr = s.regs[4]
	for i, v := range s.regs {
		if i == 4 || v == nil {
			continue
		}
		b.regs[i] = v
	}
	b.fpu = append(b.fpu, s.fpu...)
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

// Merge takes the blocks and errors of another model lifted into the same pool
func (mm *MachineModel) Merge(other *MachineModel) error {
	if other.Exprs != mm.Exprs {
		return fmt.Errorf("xbe: machine models pooled apart cannot merge")
	}
	for key, b := range other.Blocks {
		if _, taken := mm.Blocks[key]; taken {
			return fmt.Errorf("xbe: machine block %s lifted from two images", key)
		}
		mm.Blocks[key] = b
	}
	for key, b := range other.blocks {
		mm.blocks[key] = b
	}
	for va := range other.routines {
		mm.routines[va] = true
	}
	for key, err := range other.Errors {
		mm.Errors[key] = err
	}
	mm.Templates = append(mm.Templates, other.Templates...)
	return nil
}

// Pools the expressions of every block into the model's own table
func (mm *MachineModel) serialize() {
	in := mm.Exprs.interner()
	for _, key := range slices.Sorted(maps.Keys(mm.blocks)) {
		mm.Blocks[key] = serializeBlock(in, mm.blocks[key])
	}
}

// The flag record a block leaves, its kind named so a block entered from anywhere reads it live
func (b *liftedBlock) flagsJSON(in *interner) *LiftedFlags {
	// A block leaving the live record it entered with leaves the machine's record alone
	if b.fl.kind == "" || b.fl.kind == "machine" {
		return nil
	}
	fl := &LiftedFlags{Kind: b.fl.kind, A: optional(in, b.fl.a), B: optional(in, b.fl.b), R: optional(in, b.fl.r), CF: optional(in, b.fl.cf), FswA: -1, FswB: -1}
	if b.fl.fsw != nil {
		fl.FswA, fl.FswB = in.add(b.fl.fsw.a), in.add(b.fl.fsw.b)
	}
	return fl
}

// Describes the blocks of a machine model whose addresses fall in a range, in the dump notation
func (mm *MachineModel) Dump(lo, hi uint32) string {
	var sb strings.Builder
	for _, key := range slices.Sorted(maps.Keys(mm.blocks)) {
		b := mm.blocks[key]
		if b.va < lo || b.va > hi {
			continue
		}
		lf := &LiftedFunction{blocks: map[string]*liftedBlock{key: b}}
		sb.WriteString(lf.String())
	}
	for _, key := range slices.Sorted(maps.Keys(mm.Errors)) {
		va := blockKeyVA(key)
		if va >= lo && va <= hi {
			fmt.Fprintf(&sb, "block %s failed: %s\n", key, mm.Errors[key])
		}
	}
	return sb.String()
}

// Codes the flag record kinds carry into the machine, which assembles a flags word from them
var flagKindCodes = map[string]uint32{"cmp": 1, "sub": 2, "add": 3, "test": 4, "and": 4, "or": 4, "xor": 4, "alu": 5, "eflags": 6, "machine": 7}

// The operands of the flag record as call arguments, the kind first and absent operands as zero
func (ex *Executor) flagsArgs(s *execState) []Expr {
	f := s.fl
	code, known := flagKindCodes[f.kind]
	if !known {
		code = 0
	}
	args := []Expr{IConst{code}}
	for _, e := range []Expr{f.a, f.b, f.r, f.cf} {
		if e == nil {
			e = IConst{0}
		}
		args = append(args, e)
	}
	return args
}

// The condition a jump decides from a flags word, by the bits the processor keeps there
func eflagsCondition(w Expr, op x86asm.Op) Expr {
	bit := func(n uint32) Expr { return mkBin("and", mkBin("shr", w, IConst{n}), IConst{1}) }
	cf, pf, zf, sf, of := bit(0), bit(2), bit(6), bit(7), bit(11)
	set := func(e Expr) Expr { return mkCond("ne", e, IConst{0}) }
	switch op {
	case x86asm.JE, x86asm.SETE:
		return set(zf)
	case x86asm.JNE, x86asm.SETNE:
		return mkNot(set(zf))
	case x86asm.JB, x86asm.SETB:
		return set(cf)
	case x86asm.JAE, x86asm.SETAE:
		return mkNot(set(cf))
	case x86asm.JBE, x86asm.SETBE:
		return set(mkBin("or", cf, zf))
	case x86asm.JA, x86asm.SETA:
		return mkNot(set(mkBin("or", cf, zf)))
	case x86asm.JS, x86asm.SETS:
		return set(sf)
	case x86asm.JNS, x86asm.SETNS:
		return mkNot(set(sf))
	case x86asm.JP, x86asm.SETP:
		return set(pf)
	case x86asm.JNP, x86asm.SETNP:
		return mkNot(set(pf))
	case x86asm.JO, x86asm.SETO:
		return set(of)
	case x86asm.JNO, x86asm.SETNO:
		return mkNot(set(of))
	case x86asm.JL, x86asm.SETL:
		return set(mkBin("xor", sf, of))
	case x86asm.JGE, x86asm.SETGE:
		return mkNot(set(mkBin("xor", sf, of)))
	case x86asm.JLE, x86asm.SETLE:
		return set(mkBin("or", zf, mkBin("xor", sf, of)))
	case x86asm.JG, x86asm.SETG:
		return mkNot(set(mkBin("or", zf, mkBin("xor", sf, of))))
	}
	panic(fmt.Sprintf("flags word condition %v", op))
}

// Ends a machine block at a call, the return address pushed and the block jumping where the call goes
func (ex *Executor) machineCall(s *execState, in *Instruction, next uint32) (bool, error) {
	target, dynamic, err := ex.machineTarget(s, in)
	if err != nil {
		return false, err
	}
	ex.pushWord(s, in, IConst{next})
	if dynamic != nil {
		ex.block.term = &blockTerm{kind: "jumpind", index: dynamic, ret: next}
	} else {
		ex.block.term = &blockTerm{kind: "jump", target: target, ret: next}
	}
	s.va = 0
	return true, nil
}

// RoutineBlocks collects the blocks of the routine starting at an address, following its branches and stepping over
// its calls to their return addresses, and reports the routines it calls
func (mm *MachineModel) RoutineBlocks(entry uint32) (blocks []*liftedBlock, callees []uint32) {
	seen := map[uint32]bool{}
	queue := []uint32{entry}
	calls := map[uint32]bool{}
	for len(queue) > 0 {
		va := queue[0]
		queue = queue[1:]
		if seen[va] {
			continue
		}
		seen[va] = true
		b := mm.blocks[blockKey(va, nil)]
		if b == nil {
			for key, cand := range mm.blocks {
				if cand.va == va && strings.Contains(key, "@") {
					b = cand
					break
				}
			}
		}
		if b == nil {
			continue
		}
		blocks = append(blocks, b)
		t := b.term
		if t.ret != 0 {
			if t.kind == "jump" && !calls[t.target] {
				calls[t.target] = true
				callees = append(callees, t.target)
			}
			queue = append(queue, t.ret)
			continue
		}
		switch t.kind {
		case "jump", "hlt", "int":
			queue = append(queue, t.target)
		case "branch":
			queue = append(queue, t.taken, t.fall)
		case "switch":
			queue = append(queue, t.targets...)
		}
	}
	return blocks, callees
}

// Blocks reports the lifted blocks of the model by key, for derivations reading them
func (mm *MachineModel) BlockVA(key string) (uint32, bool) {
	b := mm.blocks[key]
	if b == nil {
		return 0, false
	}
	return b.va, true
}

// StoresConstant reports whether a block pushes or stores the value given anywhere
func (b *liftedBlock) StoresConstant(v uint32) bool {
	for _, e := range b.effects {
		if e.Kind != "store" {
			continue
		}
		if c, ok := isConst(e.Value); ok && c == v {
			return true
		}
	}
	return false
}

// CallsTo reports whether a routine's blocks jump straight to an address
func (mm *MachineModel) CallsTo(entry, target uint32) bool {
	blocks, callees := mm.RoutineBlocks(entry)
	for _, c := range callees {
		if c == target {
			return true
		}
	}
	for _, b := range blocks {
		if b.term.kind == "jump" && b.term.target == target {
			return true
		}
	}
	return false
}

// BlocksStoring finds the addresses of the blocks storing a constant, the routines referencing a datum
func (mm *MachineModel) BlocksStoring(v uint32) []uint32 {
	var out []uint32
	for _, b := range mm.blocks {
		if b.StoresConstant(v) {
			out = append(out, b.va)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// RoutineOf finds the routine whose blocks include the block at an address, among the routines given
func (mm *MachineModel) RoutineOf(va uint32, routines []uint32) (uint32, bool) {
	for _, entry := range routines {
		blocks, _ := mm.RoutineBlocks(entry)
		for _, b := range blocks {
			if b.va == va {
				return entry, true
			}
		}
	}
	return 0, false
}

// Routines are the addresses the lift entered as routines, through calls, pointers and exports
func (mm *MachineModel) Routines() []uint32 {
	out := make([]uint32, 0, len(mm.routines))
	for va := range mm.routines {
		out = append(out, va)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// Ends a machine block at a jump, a table of cases becoming a switch and a computed address a jump through it, a
// jump whose displacement the copy of a template patches going where the patched word points from the copy
func (ex *Executor) machineJump(s *execState, in *Instruction) (bool, error) {
	if off, patched := ex.template.patchedWord(in); patched {
		if _, isRel := in.Inst.Args[0].(x86asm.Rel); isRel {
			end := mkBin("add", Sym{"self"}, IConst{in.VA + uint32(in.Inst.Len) - ex.template.Base})
			ex.block.term = &blockTerm{kind: "jumpind", index: mkBin("add", end, ex.template.slotLoad(off))}
			s.va = 0
			return true, nil
		}
	}
	if m, ok := in.Inst.Args[0].(x86asm.Mem); ok && m.Index != 0 && m.Scale == 4 && m.Base == 0 {
		if sec := ex.d.sectionOf(in.VA); sec != nil {
			if targets := ex.d.jumpTableTargets(in, sec); len(targets) > 0 {
				return ex.switchJump(s, s.getReg(m.Index), targets)
			}
		}
	}
	target, dynamic, err := ex.machineTarget(s, in)
	if err != nil {
		return false, err
	}
	if dynamic != nil {
		ex.block.term = &blockTerm{kind: "jumpind", index: dynamic}
	} else {
		ex.block.term = &blockTerm{kind: "jump", target: target}
	}
	s.va = 0
	return true, nil
}

// Where a call or jump goes, as an address when the instruction fixes it and as the expression it computes otherwise
func (ex *Executor) machineTarget(s *execState, in *Instruction) (uint32, Expr, error) {
	var v Expr
	switch a := in.Inst.Args[0].(type) {
	case x86asm.Rel:
		return in.VA + uint32(in.Inst.Len) + uint32(int32(a)), nil, nil
	case x86asm.Imm:
		return uint32(a), nil, nil
	case x86asm.Mem:
		v = ex.load(s, ex.addr(s, a), 4)
	case x86asm.Reg:
		v = s.getReg(a)
	default:
		return 0, nil, fmt.Errorf("control transfer through %v", in.Inst.Args[0])
	}
	if c, ok := isConst(v); ok {
		return c, nil, nil
	}
	return 0, v, nil
}

// Ends a machine block at a return, the address popped and the stack raised past the bytes the return removes
func (ex *Executor) machineReturn(s *execState, clean int32, far bool) (bool, error) {
	target := ex.pop(s)
	if far {
		s.setReg(x86asm.CS, ex.pop(s))
	}
	if clean != 0 {
		s.regs[4] = mkBin("add", s.regs[4], IConst{uint32(clean)})
	}
	if c, ok := isConst(target); ok {
		ex.block.term = &blockTerm{kind: "jump", target: c}
	} else {
		ex.block.term = &blockTerm{kind: "jumpind", index: target}
	}
	s.va = 0
	return true, nil
}

// Ends a machine block at a halt or a software interrupt, which resumes at the next instruction, or at an undefined
// instruction, which faults at the instruction itself
func (ex *Executor) machineTrap(s *execState, in *Instruction, next uint32) (bool, error) {
	switch in.Inst.Op {
	case x86asm.HLT:
		ex.block.term = &blockTerm{kind: "hlt", target: next}
	case x86asm.INT:
		ex.block.term = &blockTerm{kind: "int", vector: int(in.Inst.Args[0].(x86asm.Imm)), target: next}
	default:
		ex.block.term = &blockTerm{kind: "int", vector: 6, target: in.VA}
	}
	s.va = 0
	return true, nil
}

// Moves the x87 control word between memory and the machine, and resets the unit on its init instructions
func (ex *Executor) machineControlWord(s *execState, in *Instruction) (bool, error) {
	switch in.Inst.Op {
	case x86asm.FNSTCW:
		ex.store(s, ex.addr(s, in.Inst.Args[0].(x86asm.Mem)), 2, ex.cpuResult(s, in, cpuFNSTCW), in.VA)
	case x86asm.FLDCW:
		ex.cpuEffect(s, in, cpuFLDCW, ex.load(s, ex.addr(s, in.Inst.Args[0].(x86asm.Mem)), 2))
	case x86asm.FNINIT:
		ex.cpuEffect(s, in, cpuFNINIT)
		s.fpu = nil
		for i := 0; i < 8; i++ {
			s.fpu = append(s.fpu, ex.cpuResult(s, in, cpuFRstorST, IConst{0}, IConst{uint32(i)}))
		}
	case x86asm.FNCLEX:
		ex.cpuEffect(s, in, cpuFNCLEX)
	}
	return false, nil
}

// Drops what the block knows of memory at fixed addresses a store through a computed address may overwrite
func (ex *Executor) forgetAliased(s *execState, a Expr, size int) {
	for k, mv := range s.mem {
		if k.base == "" && mayAlias(a, size, IConst{uint32(k.off)}, mv.size) {
			delete(s.mem, k)
		}
	}
}

// Reports whether a machine address is memory and not a device register: the stack, which the stack pointer or
// the frame pointer addresses, and the kernel's own identity mapped memory
func memoryAddress(a Expr) bool {
	base, off := addressParts(a)
	if base == nil {
		return off >= 0x80000000 && off < 0xC0000000
	}
	if sym, ok := base.(Sym); ok {
		return sym.Name == "reg.ESP" || sym.Name == "reg.EBP"
	}
	return false
}

// The value the latest store through the same address left, unless a store that may overlap it came after, and only
// where the address is memory, since a device register answers reads with its own state
func (s *execState) machineStoredThrough(a Expr, size int) (Expr, bool) {
	if !memoryAddress(a) {
		return nil, false
	}
	for i := len(s.effects) - 1; i >= 0; i-- {
		e := &s.effects[i]
		switch e.Kind {
		case "store":
			var at Expr = e.Addr
			if at == nil {
				if e.Base != "" {
					continue
				}
				at = IConst{uint32(e.Off)}
			}
			if e.Size == size && sameExpr(at, a) {
				return e.Value, true
			}
			if mayAlias(at, e.Size, a, size) {
				return nil, false
			}
		case "copy", "fill", "call":
			if e.Kind == "call" && (strings.HasPrefix(e.Fn, "cpu_") && e.Fn != cpuINS && e.Fn != cpuFXRstor && e.Fn != cpuFRstor && e.Fn != cpuFXSave && e.Fn != cpuFNSave || e.Fn == portOut) {
				continue
			}
			return nil, false
		}
	}
	return nil, false
}

// Splits an address into the expression it offsets and the offset, a fixed address offsetting nothing
func addressParts(a Expr) (Expr, int64) {
	switch v := a.(type) {
	case IConst:
		return nil, int64(v.V)
	case Bin:
		if v.Op == "add" {
			if c, ok := isConst(v.B); ok {
				return v.A, int64(int32(c))
			}
			if c, ok := isConst(v.A); ok {
				return v.B, int64(int32(c))
			}
		}
		if v.Op == "sub" {
			if c, ok := isConst(v.B); ok {
				return v.A, -int64(int32(c))
			}
		}
	}
	return a, 0
}

// Reports whether two accesses may touch the same bytes, which only two offsets from one base or two fixed
// addresses can rule out
func mayAlias(a Expr, sizeA int, b Expr, sizeB int) bool {
	baseA, offA := addressParts(a)
	baseB, offB := addressParts(b)
	if baseA == nil && baseB == nil || baseA != nil && baseB != nil && sameExpr(baseA, baseB) {
		return offA < offB+int64(sizeB) && offB < offA+int64(sizeA)
	}
	return true
}

// Runs a block, an instruction the executor cannot express ending the block with that as its error
func (ex *Executor) runRecovering(s *execState) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%08X: %v", s.va, r)
		}
	}()
	return ex.run(s)
}

// MachineLeaders reports the block starts a machine lift of an image would use, for inspecting the discovery
func MachineLeaders(d *Disassembly, roots []uint32) (leaders, dynamic map[uint32]bool) {
	ls := d.machineLeaders(roots)
	return ls.leaders, ls.dynamic
}

// Reports whether a code address the image holds as a pointer starts a routine, the instruction before it ending one
func (d *Disassembly) plausibleEntry(va uint32) bool {
	if !d.executableAt(va) {
		return false
	}
	if d.endsAt == nil {
		d.endsAt = map[uint32]*Instruction{}
		for _, in := range d.InsnByVA {
			d.endsAt[in.VA+uint32(in.Inst.Len)] = in
		}
	}
	prev, ok := d.endsAt[va]
	if !ok {
		sec := d.sectionOf(va)
		return sec != nil && sec.VirtualAddr == va
	}
	return isBoundaryOp(prev.Inst.Op) || isPadding(prev)
}

// Reports whether an instruction is the padding the linker puts before an aligned routine
func isPadding(in *Instruction) bool {
	args := in.Inst.Args
	switch in.Inst.Op {
	case x86asm.LEA:
		m, ok := args[1].(x86asm.Mem)
		return ok && m.Disp == 0 && m.Index == 0 && x86asm.Reg(m.Base) == args[0].(x86asm.Reg)
	case x86asm.MOV, x86asm.XCHG:
		a, aok := args[0].(x86asm.Reg)
		b, bok := args[1].(x86asm.Reg)
		return aok && bok && a == b
	}
	return false
}

// KernelRoots are the addresses the kernel is entered at from outside its own code, its entry and its routine exports
func KernelRoots(d *Disassembly, exports map[uint32]uint32) []uint32 {
	roots := []uint32{d.Image.EntryPoint}
	for _, va := range exports {
		if d.executableAt(va) {
			roots = append(roots, va)
		}
	}
	return roots
}
