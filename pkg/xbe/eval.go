package xbe

import (
	"encoding/binary"
	"fmt"
	"hash/fnv"
	"math"
	"math/bits"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"
)

// MachineRun executes the lifted blocks of a machine model in Go, for a derivation that reads what a routine of the
// image computes, every value an integer and memory starting as the image holds it
type MachineRun struct {
	mm       *MachineModel
	img      *Image
	Regs     map[string]uint32
	mem      runMemory
	fl       runFlags
	IF, DF   bool
	tsc      uint64
	steps    int
	results  map[int]uint32
	memo     map[exprKey]uint32
	fmemo    map[exprKey]float64
	Stop     map[uint32]bool                                                // addresses the run stops at before executing
	Hooks    map[uint32]func(r *MachineRun) uint32                          // routines the run answers itself, each returning where to continue
	Failed   error                                                          // what a hook could not answer
	Segments map[string]uint32                                              // the base the processor holds for each segment
	Missing  func(r *MachineRun, va uint32) error                           // lifts a block the run reaches that no lift covered
	Trail    []uint32                                                       // the last blocks the run entered
	IO       func(r *MachineRun, out bool, port, value, size uint32) uint32 // answers the ports the run touches
	CPU      func(r *MachineRun, fn string, args []uint32) (uint32, bool)   // answers processor operations before the run's own
	Overlays []Overlay                                                      // bytes read where nothing was written, a window repeating them
	OnCall   func(r *MachineRun, target, ret uint32)                        // told of every call a block makes before the callee runs
	Stored   func(r *MachineRun, addr uint32, size int, v uint32)           // told of every store once the bytes are written
	sh       *shadowState                                                   // the open values the run follows, where it follows any
	fpu      []float64                                                      // the floating point stack as the last block left it
	fcmp     [2]float64                                                     // the numbers the last float compare of a block compared
	cw       uint32                                                         // the x87 control word
	epoch    uint64                                                         // counts every change to memory or to the state a hook holds
	visits   runVisits                                                      // the epoch and processor state each block was last entered at
}

// runVisit is the epoch a block was last entered at and the processor state then, hashed once the epoch repeats
type runVisit struct {
	epoch  uint64
	state  uint64
	hashed bool
}

// Overlay is a window of memory answering reads with bytes repeated through it, as a flash chip mirrored over a bus
type Overlay struct {
	Base uint32
	Size uint32
	Data []byte
}

type runFlags struct {
	kind       string
	a, b, r, c uint32
	fa, fb     float64
}

// NewMachineRun prepares a run over a model's blocks with the stack pointer and the registers given
func NewMachineRun(mm *MachineModel, img *Image, regs map[string]uint32) *MachineRun {
	r := &MachineRun{mm: mm, img: img, Regs: map[string]uint32{}, mem: newRunMemory(), Stop: map[uint32]bool{}, Hooks: map[uint32]func(r *MachineRun) uint32{}, Segments: map[string]uint32{}, fpu: make([]float64, 8), cw: fpuDefaultControlWord}
	for _, name := range registerNames {
		r.Regs[name] = 0
	}
	for name, v := range regs {
		r.Regs[name] = v
	}
	return r
}

// Memory as the run sees it, bytes written in the run over the image's own

func (r *MachineRun) ReadU8(addr uint32) byte {
	if b, ok := r.mem.get(addr); ok {
		return b
	}
	for _, o := range r.Overlays {
		if addr >= o.Base && addr-o.Base < o.Size && len(o.Data) > 0 {
			return o.Data[(addr-o.Base)%uint32(len(o.Data))]
		}
	}
	b, _ := r.img.ReadU8(addr)
	return b
}

func (r *MachineRun) ReadU32(addr uint32) uint32 {
	return uint32(r.ReadU8(addr)) | uint32(r.ReadU8(addr+1))<<8 | uint32(r.ReadU8(addr+2))<<16 | uint32(r.ReadU8(addr+3))<<24
}

func (r *MachineRun) load(addr uint32, size int) uint32 {
	var v uint32
	for i := size - 1; i >= 0; i-- {
		v = v<<8 | uint32(r.ReadU8(addr+uint32(i)))
	}
	return v
}

func (r *MachineRun) WriteU32(addr uint32, v uint32) {
	r.store(addr, 4, v)
}

// Writes bytes, a byte that differs from what the address held advancing the epoch
func (r *MachineRun) store(addr uint32, size int, v uint32) {
	for i := 0; i < size; i++ {
		at, b := addr+uint32(i), byte(v>>(8*i))
		if had, written := r.mem.get(at); written {
			if had != b {
				r.epoch++
			}
		} else if r.ReadU8(at) != b {
			r.epoch++
		}
		r.mem.set(at, b)
	}
	if r.sh != nil {
		r.setShadow(addr, size, nil)
	}
	if r.Stored != nil {
		r.Stored(r, addr, size, v)
	}
}

// Advance counts a change to state a hook keeps outside the run, so the spin check sees it
func (r *MachineRun) Advance() {
	r.epoch++
}

// Run executes blocks from an address until a stop address, failing at a loop the run can never leave
func (r *MachineRun) Run(entry uint32) (uint32, error) {
	key := blockKey(entry, nil)
	r.visits = newRunVisits()
	for {
		if err := r.visit(key); err != nil {
			return 0, err
		}
		b := r.mm.blocks[key]
		if b == nil && r.Missing != nil && key == blockKey(blockKeyVA(key), nil) {
			if err := r.Missing(r, blockKeyVA(key)); err != nil {
				return 0, fmt.Errorf("machine run: block %s: %w", key, err)
			}
			b = r.mm.blocks[key]
		}
		if b == nil {
			if why, failed := r.mm.Errors[key]; failed {
				return 0, fmt.Errorf("machine run: block %s was not lifted: %s", key, why)
			}
			return 0, fmt.Errorf("machine run: no block %s", key)
		}
		r.Trail = append(r.Trail, b.va)
		if len(r.Trail) > 32 {
			r.Trail = r.Trail[1:]
		}
		next, err := r.block(b)
		if err != nil {
			return 0, fmt.Errorf("machine run: block %s: %w (after %X)", key, err, r.Trail)
		}
		if b.term.ret != 0 && r.OnCall != nil {
			r.OnCall(r, next, b.term.ret)
		}
		if r.Stop[next] {
			return next, nil
		}
		if hook, hooked := r.Hooks[next]; hooked {
			next = hook(r)
			if r.Stop[next] {
				return next, nil
			}
			key = blockKey(next, nil)
			continue
		}
		key = r.keyFor(b, next)
	}
}

// Records a block's entry, failing when nothing changed since the last entry that found the same processor state
func (r *MachineRun) visit(key string) error {
	last, seen := r.visits.get(key)
	if !seen || last.epoch != r.epoch {
		r.visits.set(key, runVisit{epoch: r.epoch})
		return nil
	}
	state := r.stateHash()
	if last.hashed && last.state == state {
		return fmt.Errorf("machine run: block %s is entered again with nothing changed since, a loop the run never leaves (last %X)", key, r.Trail)
	}
	r.visits.set(key, runVisit{epoch: r.epoch, state: state, hashed: true})
	return nil
}

// Hashes everything the next block's outcome depends on besides memory
func (r *MachineRun) stateHash() uint64 {
	h := fnv.New64a()
	var w [8]byte
	word := func(v uint64) {
		binary.LittleEndian.PutUint64(w[:], v)
		h.Write(w[:])
	}
	for _, name := range registerNames {
		word(uint64(r.Regs[name]))
	}
	segments := make([]string, 0, len(r.Segments))
	for name := range r.Segments {
		segments = append(segments, name)
	}
	sort.Strings(segments)
	for _, name := range segments {
		h.Write([]byte(name))
		word(uint64(r.Segments[name]))
	}
	h.Write([]byte(r.fl.kind))
	for _, v := range []uint32{r.fl.a, r.fl.b, r.fl.r, r.fl.c, r.cw} {
		word(uint64(v))
	}
	for _, f := range []float64{r.fl.fa, r.fl.fb} {
		word(math.Float64bits(f))
	}
	for _, f := range r.fpu {
		word(math.Float64bits(f))
	}
	flags := uint64(0)
	if r.IF {
		flags |= 1
	}
	if r.DF {
		flags |= 2
	}
	word(flags)
	word(r.tsc)
	word(uint64(len(r.fpu))<<32 | uint64(len(r.Overlays)))
	word(uint64(len(r.Hooks))<<32 | uint64(len(r.Stop)))
	return h.Sum64()
}

// The key of the block an address continues at, the lifted successor's key when the block links one there
func (r *MachineRun) keyFor(b *liftedBlock, va uint32) string {
	for _, succ := range b.succs {
		if blockKeyVA(succ) == va {
			return succ
		}
	}
	return blockKey(va, nil)
}

// Runs one block and returns the address it continues at
func (r *MachineRun) block(b *liftedBlock) (uint32, error) {
	if r.results == nil {
		r.results, r.memo, r.fmemo = map[int]uint32{}, map[exprKey]uint32{}, map[exprKey]float64{}
	} else {
		clear(r.results)
		clear(r.memo)
		clear(r.fmemo)
	}
	if r.sh != nil {
		r.sh.results = map[int]Expr{}
		r.sh.memo = map[exprKey]Expr{}
		r.sh.memoDone = map[exprKey]bool{}
		r.sh.branch = nil
	}
	loads := loadsByEpoch(b)
	for k, e := range b.effects {
		for _, l := range loads[k] {
			if err := r.preload(l); err != nil {
				return 0, err
			}
		}
		if err := r.effect(e); err != nil {
			return 0, err
		}
	}
	for epoch, ls := range loads {
		if epoch >= len(b.effects) {
			for _, l := range ls {
				if err := r.preload(l); err != nil {
					return 0, err
				}
			}
		}
	}
	out := map[string]uint32{}
	shOut := map[string]Expr{}
	for i, v := range b.regs {
		if v == nil || i == 4 {
			continue
		}
		x, err := r.eval(v)
		if err != nil {
			return 0, err
		}
		out[registerNames[i]] = x
		if r.sh != nil {
			shOut[registerNames[i]] = r.shadow(v)
		}
	}
	if b.spExpr != nil {
		x, err := r.eval(b.spExpr)
		if err != nil {
			return 0, err
		}
		out["ESP"] = x
	}
	var fpu []float64
	if len(b.fpu) > 0 && !stackUnchanged(b.fpu) {
		fpu = make([]float64, len(b.fpu))
		for i, e := range b.fpu {
			v, err := r.evalF(e)
			if err != nil {
				return 0, err
			}
			fpu[i] = v
		}
	}
	cw := r.cw
	if b.cw != nil {
		v, err := r.eval(b.cw)
		if err != nil {
			return 0, err
		}
		cw = v
	}
	var fl *runFlags
	if b.fl.kind == "fsw" && b.fl.fsw != nil {
		fa, err := r.evalF(b.fl.fsw.a)
		if err != nil {
			return 0, err
		}
		fb, err := r.evalF(b.fl.fsw.b)
		if err != nil {
			return 0, err
		}
		fl = &runFlags{kind: "fsw", fa: fa, fb: fb}
	} else if b.fl.kind == "comiss" {
		// A scalar compare leaves the ordering in the flags the way a status word carries it
		fa, err := r.evalF(b.fl.a)
		if err != nil {
			return 0, err
		}
		fb, err := r.evalF(b.fl.b)
		if err != nil {
			return 0, err
		}
		fl = &runFlags{kind: "fsw", fa: fa, fb: fb}
	} else if b.fl.kind != "" && b.fl.kind != "machine" {
		f := runFlags{kind: b.fl.kind}
		for _, p := range []struct {
			e   Expr
			dst *uint32
		}{{b.fl.a, &f.a}, {b.fl.b, &f.b}, {b.fl.r, &f.r}, {b.fl.cf, &f.c}} {
			if p.e != nil {
				x, err := r.eval(p.e)
				if err != nil {
					return 0, err
				}
				*p.dst = x
			}
		}
		fl = &f
	}
	var fcmp *[2]float64
	if b.fcmp != nil {
		fa, err := r.evalF(b.fcmp.a)
		if err != nil {
			return 0, err
		}
		fb, err := r.evalF(b.fcmp.b)
		if err != nil {
			return 0, err
		}
		fcmp = &[2]float64{fa, fb}
	}
	var shFl *shadowFlags
	if r.sh != nil && fl != nil {
		shFl = &shadowFlags{a: r.shadow(b.fl.a), b: r.shadow(b.fl.b), r: r.shadow(b.fl.r), c: r.shadow(b.fl.cf)}
	}
	t := b.term
	var next uint32
	switch t.kind {
	case "jump", "hlt":
		next = t.target
	case "branch":
		c, err := r.eval(t.cond)
		if err != nil {
			return 0, err
		}
		if c != 0 {
			next = t.taken
		} else {
			next = t.fall
		}
		if sh := r.shadow(t.cond); sh != nil {
			r.sh.branch = &openBranch{kind: "branch", cond: sh, outcome: c != 0, taken: t.taken, fall: t.fall, va: b.va}
		}
	case "switch":
		i, err := r.eval(t.index)
		if err != nil {
			return 0, err
		}
		if int(i) >= len(t.targets) {
			return 0, fmt.Errorf("switch index %d outside %d cases", i, len(t.targets))
		}
		next = t.targets[i]
		if sh := r.shadow(t.index); sh != nil {
			r.sh.branch = &openBranch{kind: "switch", cond: sh, index: i, targets: t.targets, va: b.va}
		}
	case "jumpind":
		a, err := r.eval(t.index)
		if err != nil {
			return 0, err
		}
		if r.shadow(t.index) != nil {
			return 0, fmt.Errorf("jump through an open address")
		}
		next = a
	default:
		return 0, fmt.Errorf("block ends with %s", t.kind)
	}
	for name, v := range out {
		r.Regs[name] = v
	}
	if r.sh != nil {
		for name, sh := range shOut {
			if sh == nil {
				delete(r.sh.regs, name)
			} else {
				r.sh.regs[name] = sh
			}
		}
		if shFl != nil {
			r.sh.fl = *shFl
		}
	}
	if fpu != nil {
		r.fpu = fpu
	}
	r.cw = cw
	if fl != nil {
		r.fl = *fl
	}
	if fcmp != nil {
		r.fcmp = *fcmp
	}
	return next, nil
}

// Reads a load at its epoch, an eight byte one as the double it is read as
func (r *MachineRun) preload(l Expr) error {
	if ld, ok := l.(Load); ok && ld.Size == 8 {
		_, err := r.loadDouble(ld)
		return err
	}
	_, err := r.eval(l)
	return err
}

// Reads eight bytes as a double, once per block at the load's epoch
func (r *MachineRun) loadDouble(ld Load) (float64, error) {
	k := keyOf(ld)
	if v, ok := r.fmemo[k]; ok {
		return v, nil
	}
	addr := uint32(ld.Off)
	if ld.Addr != nil {
		a, err := r.eval(ld.Addr)
		if err != nil {
			return 0, err
		}
		addr = a
	} else if ld.Base != "" {
		return 0, fmt.Errorf("load from %s", ld.Base)
	}
	v := math.Float64frombits(uint64(r.load(addr, 4)) | uint64(r.load(addr+4, 4))<<32)
	r.fmemo[k] = v
	return v, nil
}

// Every load a block makes, by the effect count it was recorded at
func loadsByEpoch(b *liftedBlock) map[int][]Expr {
	if b.loads != nil {
		return b.loads
	}
	out := map[int][]Expr{}
	seen := map[exprKey]bool{}
	var visit func(e Expr)
	visit = func(e Expr) {
		if e == nil {
			return
		}
		k := keyOf(e)
		if seen[k] {
			return
		}
		seen[k] = true
		eachChild(e, visit)
		if l, ok := e.(Load); ok {
			out[l.Epoch] = append(out[l.Epoch], e)
		}
	}
	for _, e := range b.emitted() {
		visit(e)
	}
	b.loads = out
	return out
}

func (r *MachineRun) effect(e Effect) error {
	switch e.Kind {
	case "store":
		addr := uint32(e.Off)
		if e.Addr != nil {
			var err error
			if addr, err = r.eval(e.Addr); err != nil {
				return err
			}
		} else if e.Base != "" {
			return fmt.Errorf("store to %s", e.Base)
		}
		if e.Size == 8 {
			f, err := r.evalF(e.Value)
			if err != nil {
				return err
			}
			bits := math.Float64bits(f)
			r.store(addr, 4, uint32(bits))
			r.store(addr+4, 4, uint32(bits>>32))
			return nil
		}
		if e.Size > 4 {
			return fmt.Errorf("store of %d bytes", e.Size)
		}
		v, err := r.eval(e.Value)
		if err != nil {
			return err
		}
		sh := r.shadow(e.Value)
		r.store(addr, e.Size, v)
		if sh != nil {
			r.setShadow(addr, e.Size, sh)
		}
	case "copy", "fill":
		dst, err := r.eval(e.Addr)
		if err != nil {
			return err
		}
		count, err := r.eval(e.Value)
		if err != nil {
			return err
		}
		step, err := r.eval(e.Args[1])
		if err != nil {
			return err
		}
		if e.Kind == "copy" {
			src, err := r.eval(e.Args[0])
			if err != nil {
				return err
			}
			srcShadow := r.shadow(e.Args[0])
			for i := uint32(0); i < count; i++ {
				var sh Expr
				if r.sh != nil {
					sh = r.loadShadow(src, e.Size, offsetShadow(srcShadow, i*step))
				}
				r.store(dst, e.Size, r.load(src, e.Size))
				if sh != nil {
					r.setShadow(dst, e.Size, sh)
				}
				dst += step
				src += step
			}
			return nil
		}
		value, err := r.eval(e.Args[0])
		if err != nil {
			return err
		}
		sh := r.shadow(e.Args[0])
		count, value = value, count
		for i := uint32(0); i < count; i++ {
			r.store(dst, e.Size, value)
			if sh != nil {
				r.setShadow(dst, e.Size, sh)
			}
			dst += step
		}
	case "call":
		args := make([]uint32, len(e.Args))
		for i, a := range e.Args {
			v, err := r.eval(a)
			if err != nil {
				return err
			}
			args[i] = v
		}
		v, err := r.cpu(e.Fn, args)
		if err != nil {
			return err
		}
		r.results[e.Seq] = v
		if r.sh != nil && e.Fn == portIn {
			r.sh.results[e.Seq] = r.sh.leaf(int(args[1]), v)
			r.epoch++
		}
	default:
		return fmt.Errorf("%s effect", e.Kind)
	}
	return nil
}

// An open address moved by an offset, nothing where the address is fixed
func offsetShadow(sh Expr, off uint32) Expr {
	if sh == nil || off == 0 {
		return sh
	}
	return Bin{Op: "add", A: sh, B: IConst{V: off}}
}

// The processor operations a derivation run answers
func (r *MachineRun) cpu(fn string, args []uint32) (uint32, error) {
	if r.CPU != nil {
		if v, answered := r.CPU(r, fn, args); answered {
			return v, nil
		}
	}
	switch fn {
	case cpuCLI:
		r.IF = false
	case cpuSTI:
		r.IF = true
	case cpuCLD:
		r.DF = false
	case cpuSTD:
		r.DF = true
	case cpuPushFD:
		return r.eflags(), nil
	case cpuPopFD:
		r.IF = args[0]&0x200 != 0
		r.DF = args[0]&0x400 != 0
		r.fl = runFlags{kind: "eflags", a: args[0]}
	case cpuRDTSCLo:
		r.tsc++
		return uint32(r.tsc), nil
	case cpuRDTSCHi:
		return uint32(r.tsc >> 32), nil
	case cpuFLDCW:
		r.cw = args[0]
	case cpuFNSTCW:
		return r.cw, nil
	case cpuFNINIT:
		r.cw = fpuInitControlWord
		for i := range r.fpu {
			r.fpu[i] = math.NaN()
		}
	case cpuFNCLEX:
	case cpuFRstorST, cpuFXRstorST:
		return 0, nil
	case cpuSCAS:
		// Scans memory for the accumulator, answering how many elements it compared before stopping
		ptr, value, count, size, step, repne := args[0], args[1], args[2], int(args[3]), args[4], args[5] != 0
		mask := uint32(1)<<(8*uint(size)) - 1
		if size == 4 {
			mask = 0xFFFFFFFF
		}
		n := uint32(0)
		for n < count {
			equal := r.load(ptr, size) == value&mask
			ptr += step
			n++
			if equal == repne {
				break
			}
		}
		return n, nil
	case cpuCMPS:
		// Compares two strings, answering how many elements it compared before stopping
		a, b, count, size, step, repne := args[0], args[1], args[2], int(args[3]), args[4], args[5] != 0
		n := uint32(0)
		for n < count {
			equal := r.load(a, size) == r.load(b, size)
			a += step
			b += step
			n++
			if equal == repne {
				break
			}
		}
		return n, nil
	case portOut, portIn, cpuOUTS, cpuINS:
		if r.IO == nil {
			return 0, fmt.Errorf("processor operation %s", fn)
		}
		return r.port(fn, args), nil
	default:
		return 0, fmt.Errorf("processor operation %s", fn)
	}
	return 0, nil
}

// Hands a port access to the run's hardware, a string instruction moving one element per step
func (r *MachineRun) port(fn string, args []uint32) uint32 {
	switch fn {
	case portOut:
		return r.IO(r, true, args[0], args[1], args[2])
	case portIn:
		return r.IO(r, false, args[0], 0, args[1])
	}
	port, ptr, count, size, step := args[0], args[1], args[2], int(args[3]), args[4]
	for n := uint32(0); n < count; n++ {
		if fn == cpuOUTS {
			r.IO(r, true, port, r.load(ptr, size), uint32(size))
		} else {
			r.store(ptr, size, r.IO(r, false, port, 0, uint32(size)))
		}
		ptr += step
	}
	return count
}

// The flags word the record stands for
func (r *MachineRun) eflags() uint32 {
	f := r.fl
	w := uint32(2)
	if r.IF {
		w |= 0x200
	}
	if r.DF {
		w |= 0x400
	}
	if f.kind == "eflags" {
		return f.a&^0x600 | w
	}
	if f.kind == "" {
		return w
	}
	if f.r == 0 {
		w |= 0x40
	}
	if f.r&0x80000000 != 0 {
		w |= 0x80
	}
	if bits.OnesCount8(uint8(f.r))%2 == 0 {
		w |= 4
	}
	if r.carry() {
		w |= 1
	}
	return w
}

func (r *MachineRun) carry() bool {
	f := r.fl
	switch f.kind {
	case "cmp", "sub":
		return f.a < f.b
	case "add":
		return f.r < f.a
	case "alu":
		return f.c != 0
	case "eflags":
		return f.a&1 != 0
	}
	return false
}

// A condition decided from the live record, as a block entered from anywhere decides it
func (r *MachineRun) condition(op string) (bool, error) {
	f := r.fl
	if f.kind == "fsw" {
		unordered := math.IsNaN(f.fa) || math.IsNaN(f.fb)
		zf, cf := f.fa == f.fb || unordered, f.fa < f.fb || unordered
		switch op {
		case "JE":
			return zf, nil
		case "JNE":
			return !zf, nil
		case "JB":
			return cf, nil
		case "JAE":
			return !cf, nil
		case "JBE":
			return cf || zf, nil
		case "JA":
			return !(cf || zf), nil
		case "JP":
			return unordered, nil
		case "JNP":
			return !unordered, nil
		}
		return false, fmt.Errorf("condition %s on a float compare", op)
	}
	if f.kind == "eflags" {
		w := f.a
		zf, cf, sf, of := w&0x40 != 0, w&1 != 0, w&0x80 != 0, w&0x800 != 0
		switch op {
		case "JE":
			return zf, nil
		case "JNE":
			return !zf, nil
		case "JB":
			return cf, nil
		case "JAE":
			return !cf, nil
		case "JBE":
			return cf || zf, nil
		case "JA":
			return !(cf || zf), nil
		case "JL":
			return sf != of, nil
		case "JGE":
			return sf == of, nil
		case "JLE":
			return zf || sf != of, nil
		case "JG":
			return !zf && sf == of, nil
		}
		return false, fmt.Errorf("condition %s on a flags word", op)
	}
	cmp := f.kind == "cmp" || f.kind == "sub"
	zero := f.r == 0
	if cmp {
		zero = f.a == f.b
	}
	sign := int32(f.r) < 0
	switch op {
	case "JE":
		return zero, nil
	case "JNE":
		return !zero, nil
	case "JL":
		if cmp {
			return int32(f.a) < int32(f.b), nil
		}
		return sign, nil
	case "JGE":
		if cmp {
			return int32(f.a) >= int32(f.b), nil
		}
		return !sign, nil
	case "JLE":
		if cmp {
			return int32(f.a) <= int32(f.b), nil
		}
		return int32(f.r) <= 0, nil
	case "JG":
		if cmp {
			return int32(f.a) > int32(f.b), nil
		}
		return int32(f.r) > 0, nil
	case "JB":
		if cmp {
			return f.a < f.b, nil
		}
		return r.carry(), nil
	case "JAE":
		if cmp {
			return f.a >= f.b, nil
		}
		return !r.carry(), nil
	case "JBE":
		if cmp {
			return f.a <= f.b, nil
		}
		return r.carry() || zero, nil
	case "JA":
		if cmp {
			return f.a > f.b, nil
		}
		return !(r.carry() || zero), nil
	case "JS":
		return sign, nil
	case "JNS":
		return !sign, nil
	}
	return false, fmt.Errorf("condition %s after %s flags", op, f.kind)
}

func (r *MachineRun) sym(name string) (uint32, error) {
	switch {
	case strings.HasPrefix(name, "reg."):
		v, ok := r.Regs[name[4:]]
		if !ok {
			return 0, fmt.Errorf("register %s", name[4:])
		}
		return v, nil
	case name == "flags.a":
		return r.fl.a, nil
	case name == "flags.b":
		return r.fl.b, nil
	case name == "flags.r":
		return r.fl.r, nil
	case name == "flags.cf":
		return r.fl.c, nil
	case strings.HasPrefix(name, "dfstep"):
		var n int32
		fmt.Sscanf(name[6:], "%d", &n)
		if r.DF {
			n = -n
		}
		return uint32(n), nil
	case name == "self":
		return 0, fmt.Errorf("a template copy's own address outside a copy")
	case name == "fpu.cw":
		return r.cw, nil
	case strings.HasPrefix(name, "segbase."):
		base, ok := r.Segments[name[8:]]
		if !ok {
			return 0, fmt.Errorf("segment %s has no base", name[8:])
		}
		return base, nil
	case strings.HasPrefix(name, "cond."):
		c, err := r.condition(name[5:])
		if err != nil {
			return 0, err
		}
		if c {
			return 1, nil
		}
		return 0, nil
	}
	return 0, fmt.Errorf("input %s", name)
}

// Evaluates an integer expression, each shared node once per block
func (r *MachineRun) eval(e Expr) (uint32, error) {
	if e == nil {
		return 0, fmt.Errorf("undefined value")
	}
	k := keyOf(e)
	if v, ok := r.memo[k]; ok {
		return v, nil
	}
	v, err := r.evalNode(e)
	if err != nil {
		return 0, err
	}
	r.memo[k] = v
	return v, nil
}

func (r *MachineRun) evalNode(e Expr) (uint32, error) {
	if isFloatExpr(e) {
		f, err := r.evalF(e)
		if err != nil {
			return 0, err
		}
		return math.Float32bits(float32(f)), nil
	}
	switch v := e.(type) {
	case IConst:
		return v.V, nil
	case FCmpSet:
		a, err := r.evalF(v.A)
		if err != nil {
			return 0, err
		}
		b, err := r.evalF(v.B)
		if err != nil {
			return 0, err
		}
		var bit uint8
		switch {
		case math.IsNaN(a) || math.IsNaN(b):
			bit = 8
		case a < b:
			bit = 1
		case a == b:
			bit = 2
		default:
			bit = 4
		}
		if v.Set&bit != 0 {
			return 1, nil
		}
		return 0, nil
	case Sym:
		return r.sym(v.Name)
	case Ptr:
		if v.Base != "" {
			return 0, fmt.Errorf("address in %s", v.Base)
		}
		return uint32(v.Off), nil
	case Load:
		if v.Size > 4 {
			return 0, fmt.Errorf("load of %d bytes", v.Size)
		}
		addr := uint32(v.Off)
		if v.Addr != nil {
			a, err := r.eval(v.Addr)
			if err != nil {
				return 0, err
			}
			addr = a
		} else if v.Base != "" {
			return 0, fmt.Errorf("load from %s", v.Base)
		}
		return r.load(addr, v.Size), nil
	case CallResult:
		x, ok := r.results[v.Seq]
		if !ok {
			return 0, fmt.Errorf("result of %s before it ran", v.Fn)
		}
		return x, nil
	case Bin:
		if strings.HasPrefix(v.Op, "udiv64") || strings.HasPrefix(v.Op, "idiv64") {
			return r.evalWideDivide(v)
		}
		if v.Op == "hilo" {
			return 0, fmt.Errorf("a 64 bit dividend outside a division")
		}
		if v.Op == "f2icw" {
			f, err := r.evalF(v.A)
			if err != nil {
				return 0, err
			}
			cw, err := r.eval(v.B)
			if err != nil {
				return 0, err
			}
			switch (cw >> fpuRoundingShift) & fpuRoundingMask {
			case fpuRoundDown:
				return uint32(int32(math.Floor(f))), nil
			case fpuRoundUp:
				return uint32(int32(math.Ceil(f))), nil
			case fpuRoundTruncate:
				return uint32(int32(f)), nil
			}
			return uint32(int32(math.RoundToEven(f))), nil
		}
		a, err := r.eval(v.A)
		if err != nil {
			return 0, err
		}
		b, err := r.eval(v.B)
		if err != nil {
			return 0, err
		}
		switch v.Op {
		case "add":
			return a + b, nil
		case "sub":
			return a - b, nil
		case "mul":
			return a * b, nil
		case "and":
			return a & b, nil
		case "or":
			return a | b, nil
		case "xor":
			return a ^ b, nil
		case "shl":
			return a << (b & 31), nil
		case "shr":
			return a >> (b & 31), nil
		case "sar":
			return uint32(int32(a) >> (b & 31)), nil
		case "udiv":
			if b == 0 {
				return 0, fmt.Errorf("division by zero")
			}
			return a / b, nil
		case "udiv_rem":
			if b == 0 {
				return 0, fmt.Errorf("division by zero")
			}
			return a % b, nil
		case "idiv":
			if b == 0 {
				return 0, fmt.Errorf("division by zero")
			}
			return uint32(int32(a) / int32(b)), nil
		case "idiv_rem":
			if b == 0 {
				return 0, fmt.Errorf("division by zero")
			}
			return uint32(int32(a) % int32(b)), nil
		case "mulhi":
			return uint32((uint64(a) * uint64(b)) >> 32), nil
		case "imulhi":
			return uint32((int64(int32(a)) * int64(int32(b))) >> 32), nil
		}
		return 0, fmt.Errorf("operation %s", v.Op)
	case Un:
		switch v.Op {
		case "f2bits", "f2i", "f2i_rn":
			f, err := r.evalF(v.A)
			if err != nil {
				return 0, err
			}
			switch v.Op {
			case "f2bits":
				return math.Float32bits(float32(f)), nil
			case "f2i":
				return uint32(int32(f)), nil
			}
			return uint32(int32(math.RoundToEven(f))), nil
		}
		a, err := r.eval(v.A)
		if err != nil {
			return 0, err
		}
		switch v.Op {
		case "zx8":
			return a & 0xFF, nil
		case "zx16":
			return a & 0xFFFF, nil
		case "parity":
			if bits.OnesCount8(uint8(a))%2 == 0 {
				return 1, nil
			}
			return 0, nil
		case "sx8":
			return uint32(int32(int8(a))), nil
		case "sx16":
			return uint32(int32(int16(a))), nil
		case "neg":
			return -a, nil
		case "not":
			return ^a, nil
		case "bsf":
			if a == 0 {
				return 0, nil
			}
			return uint32(bits.TrailingZeros32(a)), nil
		case "bsr":
			if a == 0 {
				return 0, nil
			}
			return uint32(31 - bits.LeadingZeros32(a)), nil
		}
		return 0, fmt.Errorf("operation %s", v.Op)
	case Cond:
		a, err := r.eval(v.A)
		if err != nil {
			return 0, err
		}
		b, err := r.eval(v.B)
		if err != nil {
			return 0, err
		}
		var c bool
		switch v.Op {
		case "eq":
			c = a == b
		case "ne":
			c = a != b
		case "lt":
			c = int32(a) < int32(b)
		case "le":
			c = int32(a) <= int32(b)
		case "gt":
			c = int32(a) > int32(b)
		case "ge":
			c = int32(a) >= int32(b)
		case "ult":
			c = a < b
		case "ule":
			c = a <= b
		case "ugt":
			c = a > b
		case "uge":
			c = a >= b
		default:
			return 0, fmt.Errorf("comparison %s", v.Op)
		}
		if c {
			return 1, nil
		}
		return 0, nil
	case Not:
		a, err := r.eval(v.A)
		if err != nil {
			return 0, err
		}
		if a == 0 {
			return 1, nil
		}
		return 0, nil
	case Sel:
		c, err := r.eval(v.C)
		if err != nil {
			return 0, err
		}
		if c != 0 {
			return r.eval(v.A)
		}
		return r.eval(v.B)
	}
	return 0, fmt.Errorf("%T value", e)
}

// Evaluates a floating point expression, a stack register read from the floating point stack, one below what the
// stack holds or popped off an empty stack being the indefinite value, and an eight byte load read as a double
func (r *MachineRun) evalF(e Expr) (float64, error) {
	switch v := e.(type) {
	case FConst:
		return v.V, nil
	case Sym:
		if v.Name == "fpu.empty" {
			return math.NaN(), nil
		}
		switch v.Name {
		case "flags.fsw_a":
			return r.fl.fa, nil
		case "flags.fsw_b":
			return r.fl.fb, nil
		case "fcmp.a":
			return r.fcmp[0], nil
		case "fcmp.b":
			return r.fcmp[1], nil
		}
		if strings.HasPrefix(v.Name, "fpu.st") {
			i, err := strconv.Atoi(v.Name[6:])
			if err != nil || i < 0 {
				return 0, fmt.Errorf("float register %s", v.Name)
			}
			if i >= len(r.fpu) {
				return math.NaN(), nil
			}
			return r.fpu[i], nil
		}
		return 0, fmt.Errorf("float input %s", v.Name)
	case Sel:
		c, err := r.eval(v.C)
		if err != nil {
			return 0, err
		}
		if c != 0 {
			return r.evalF(v.A)
		}
		return r.evalF(v.B)
	case CallResult:
		if v.Fn == cpuFRstorST || v.Fn == cpuFXRstorST {
			return r.restoredRegister(v)
		}
	case Bin:
		a, err := r.evalF(v.A)
		if err != nil {
			return 0, err
		}
		b, err := r.evalF(v.B)
		if err != nil {
			return 0, err
		}
		switch v.Op {
		case "fadd":
			return a + b, nil
		case "fsub":
			return a - b, nil
		case "fmul":
			return a * b, nil
		case "fdiv":
			return a / b, nil
		case "fmin":
			return math.Min(a, b), nil
		case "fmax":
			return math.Max(a, b), nil
		case "fmod":
			return math.Mod(a, b), nil
		case "fpow":
			return math.Pow(a, b), nil
		case "fatan2":
			return math.Atan2(a, b), nil
		case "frem":
			return math.Remainder(a, b), nil
		}
		return 0, fmt.Errorf("float operation %s", v.Op)
	case Un:
		switch v.Op {
		case "f64":
			ld, ok := v.A.(Load)
			if !ok || ld.Size != 8 {
				return r.evalF(v.A)
			}
			return r.loadDouble(ld)
		case "bits2f":
			a, err := r.eval(v.A)
			if err != nil {
				return 0, err
			}
			return float64(math.Float32frombits(a)), nil
		case "i2f":
			if pair, ok := v.A.(Bin); ok && pair.Op == "hilo" {
				hi, err := r.eval(pair.A)
				if err != nil {
					return 0, err
				}
				lo, err := r.eval(pair.B)
				if err != nil {
					return 0, err
				}
				return float64(int64(uint64(hi)<<32 | uint64(lo))), nil
			}
			a, err := r.eval(v.A)
			if err != nil {
				return 0, err
			}
			return float64(int32(a)), nil
		case "bits2d":
			if pair, ok := v.A.(Bin); ok && pair.Op == "hilo" {
				hi, err := r.eval(pair.A)
				if err != nil {
					return 0, err
				}
				lo, err := r.eval(pair.B)
				if err != nil {
					return 0, err
				}
				return math.Float64frombits(uint64(hi)<<32 | uint64(lo)), nil
			}
			if sym, ok := v.A.(Sym); ok && strings.HasPrefix(sym.Name, "fpu.") {
				return r.evalF(v.A)
			}
			return 0, fmt.Errorf("a double built from %s", ExprString(v.A))
		}
		a, err := r.evalF(v.A)
		if err != nil {
			return 0, err
		}
		switch v.Op {
		case "f32":
			return float64(float32(a)), nil
		case "fneg":
			return -a, nil
		case "fabs":
			return math.Abs(a), nil
		case "fsin":
			return math.Sin(a), nil
		case "fcos":
			return math.Cos(a), nil
		case "fsqrt":
			return math.Sqrt(a), nil
		case "ftan":
			return math.Tan(a), nil
		case "ffloor":
			return math.Floor(a), nil
		case "fceil":
			return math.Ceil(a), nil
		case "fexp":
			return math.Exp(a), nil
		case "flog":
			return math.Log(a), nil
		case "flog10":
			return math.Log10(a), nil
		case "flog2":
			return math.Log2(a), nil
		case "ftrunc":
			return math.Trunc(a), nil
		case "fround":
			return math.RoundToEven(a), nil
		case "fatan":
			return math.Atan(a), nil
		case "fasin":
			return math.Asin(a), nil
		case "facos":
			return math.Acos(a), nil
		case "fsinh":
			return math.Sinh(a), nil
		case "fcosh":
			return math.Cosh(a), nil
		case "ftanh":
			return math.Tanh(a), nil
		}
		return 0, fmt.Errorf("float operation %s", v.Op)
	case Elem:
		return 0, fmt.Errorf("vector element %d", v.Index)
	}
	a, err := r.eval(e)
	if err != nil {
		return 0, err
	}
	return float64(math.Float32frombits(a)), nil
}

// The value a floating point register takes from a restored save area, empty after an init, else the extended
// precision number the area holds for it
func (r *MachineRun) restoredRegister(v CallResult) (float64, error) {
	if len(v.Args) < 2 {
		return 0, fmt.Errorf("a restored register without a save area")
	}
	frame, err := r.eval(v.Args[0])
	if err != nil {
		return 0, err
	}
	index, err := r.eval(v.Args[1])
	if err != nil {
		return 0, err
	}
	if frame == 0 {
		return math.NaN(), nil
	}
	at := frame + fsaveRegisters + fsaveRegisterSize*index
	if v.Fn == cpuFXRstorST {
		if r.load(frame+fxsaveTag, 1)&(1<<index) == 0 {
			return math.NaN(), nil
		}
		at = frame + fxsaveST + fxsaveSlot*index
	} else if r.load(frame+fsaveTag, 4)>>(2*index)&3 == 3 {
		return math.NaN(), nil
	}
	mantissa := uint64(r.load(at, 4)) | uint64(r.load(at+4, 4))<<32
	se := r.load(at+8, 2)
	exponent := int(se & 0x7FFF)
	sign := 1.0
	if se&0x8000 != 0 {
		sign = -1
	}
	if exponent == 0 && mantissa == 0 {
		return math.Copysign(0, sign), nil
	}
	if exponent == 0x7FFF {
		if mantissa<<1 == 0 {
			return math.Inf(int(sign)), nil
		}
		return math.NaN(), nil
	}
	return sign * math.Ldexp(float64(mantissa), exponent-16383-63), nil
}

// Bytes the run holds at an address, written ones over the image's
func (r *MachineRun) Bytes(addr uint32, n int) []byte {
	out := make([]byte, n)
	for i := range out {
		out[i] = r.ReadU8(addr + uint32(i))
	}
	return out
}

// Pushes a word as a caller pushes an argument
func (r *MachineRun) Push(v uint32) {
	r.Regs["ESP"] -= 4
	r.store(r.Regs["ESP"], 4, v)
}

// Pops a word as a return does
func (r *MachineRun) Pop() uint32 {
	v := r.ReadU32(r.Regs["ESP"])
	r.Regs["ESP"] += 4
	return v
}

// Divides the 64 bit dividend a hilo pair carries by a 32 bit divisor, the quotient or remainder as the operation
// names, a quotient past 32 bits being the processor's divide error
func (r *MachineRun) evalWideDivide(v Bin) (uint32, error) {
	pair, ok := v.A.(Bin)
	if !ok || pair.Op != "hilo" {
		return 0, fmt.Errorf("%s of a dividend that is no hilo pair", v.Op)
	}
	hi, err := r.eval(pair.A)
	if err != nil {
		return 0, err
	}
	lo, err := r.eval(pair.B)
	if err != nil {
		return 0, err
	}
	d, err := r.eval(v.B)
	if err != nil {
		return 0, err
	}
	if d == 0 {
		return 0, fmt.Errorf("division by zero")
	}
	rem := strings.HasSuffix(v.Op, "_rem")
	if strings.HasPrefix(v.Op, "udiv64") {
		n := uint64(hi)<<32 | uint64(lo)
		q := n / uint64(d)
		if q > math.MaxUint32 {
			return 0, fmt.Errorf("division overflow")
		}
		if rem {
			return uint32(n % uint64(d)), nil
		}
		return uint32(q), nil
	}
	n := int64(uint64(hi)<<32 | uint64(lo))
	q := n / int64(int32(d))
	if q > math.MaxInt32 || q < math.MinInt32 {
		return 0, fmt.Errorf("division overflow")
	}
	if rem {
		return uint32(int32(n % int64(int32(d)))), nil
	}
	return uint32(int32(q)), nil
}

// Preload lays bytes into the run's memory before it starts
func (r *MachineRun) Preload(addr uint32, data []byte) {
	r.epoch++
	for i, b := range data {
		r.mem.set(addr+uint32(i), b)
	}
}

// Touched reports whether the run wrote any of a number of bytes at an address
func (r *MachineRun) Touched(addr uint32, n int) bool {
	for i := 0; i < n; i++ {
		if _, written := r.mem.get(addr + uint32(i)); written {
			return true
		}
	}
	return false
}

// Written is every byte the run wrote, by address
func (r *MachineRun) Written() map[uint32]byte {
	out := make(map[uint32]byte, r.mem.count())
	r.mem.each(func(addr uint32, b byte) { out[addr] = b })
	return out
}

// Adopt copies another run's memory over this run's
func (r *MachineRun) Adopt(o *MachineRun) {
	r.epoch++
	o.mem.each(func(addr uint32, b byte) { r.mem.set(addr, b) })
}

// Copies the run as it stands, hooks and image shared, so two ways on can each be followed
func (r *MachineRun) clone() *MachineRun {
	c := *r
	c.Regs = make(map[string]uint32, len(r.Regs))
	for k, v := range r.Regs {
		c.Regs[k] = v
	}
	c.mem = r.mem.clone()
	c.Segments = make(map[string]uint32, len(r.Segments))
	for k, v := range r.Segments {
		c.Segments[k] = v
	}
	c.results, c.memo, c.fmemo = map[int]uint32{}, map[exprKey]uint32{}, map[exprKey]float64{}
	c.fpu = append([]float64(nil), r.fpu...)
	c.Trail = append([]uint32(nil), r.Trail...)
	c.Overlays = append([]Overlay(nil), r.Overlays...)
	c.visits = r.visits.clone()
	if r.sh != nil {
		c.sh = r.sh.clone()
	}
	return &c
}

// WideString reads the terminated UTF-16 string at an address as the run holds it
func (r *MachineRun) WideString(addr uint32) string {
	var units []uint16
	for {
		u := uint16(r.ReadU8(addr)) | uint16(r.ReadU8(addr+1))<<8
		if u == 0 || len(units) > 1<<16 {
			break
		}
		units = append(units, u)
		addr += 2
	}
	return string(utf16.Decode(units))
}
