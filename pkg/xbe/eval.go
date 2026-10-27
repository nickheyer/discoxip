package xbe

import (
	"fmt"
	"math"
	"math/bits"
	"strings"
	"unicode/utf16"
)

// MachineRun executes the lifted blocks of a machine model in Go, for a derivation that reads what a routine of the
// image computes, every value an integer and memory starting as the image holds it
type MachineRun struct {
	mm       *MachineModel
	img      *Image
	Regs     map[string]uint32
	mem      map[uint32]byte
	fl       runFlags
	IF, DF   bool
	tsc      uint64
	steps    int
	results  map[int]uint32
	memo     map[exprKey]uint32
	Stop     map[uint32]bool                       // addresses the run stops at before executing
	Hooks    map[uint32]func(r *MachineRun) uint32 // routines the run answers itself, each returning where to continue
	Failed   error                                 // what a hook could not answer
	Segments map[string]uint32                     // the base the processor holds for each segment
	Missing  func(r *MachineRun, va uint32) error  // lifts a block the run reaches that no lift covered
	Trail    []uint32                              // the last blocks the run entered
}

type runFlags struct {
	kind       string
	a, b, r, c uint32
}

// NewMachineRun prepares a run over a model's blocks with the stack pointer and the registers given
func NewMachineRun(mm *MachineModel, img *Image, regs map[string]uint32) *MachineRun {
	r := &MachineRun{mm: mm, img: img, Regs: map[string]uint32{}, mem: map[uint32]byte{}, Stop: map[uint32]bool{}, Hooks: map[uint32]func(r *MachineRun) uint32{}, Segments: map[string]uint32{}}
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
	if b, ok := r.mem[addr]; ok {
		return b
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

func (r *MachineRun) store(addr uint32, size int, v uint32) {
	for i := 0; i < size; i++ {
		r.mem[addr+uint32(i)] = byte(v >> (8 * i))
	}
}

// Run executes blocks from an address until one of the stop addresses is reached, reporting which
func (r *MachineRun) Run(entry uint32, maxBlocks int) (uint32, error) {
	key := blockKey(entry, nil)
	for n := 0; ; n++ {
		if n >= maxBlocks {
			return 0, fmt.Errorf("machine run: %d blocks without reaching a stop", maxBlocks)
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
	r.results = map[int]uint32{}
	r.memo = map[exprKey]uint32{}
	loads := loadsByEpoch(b)
	for k, e := range b.effects {
		for _, l := range loads[k] {
			if _, err := r.eval(l); err != nil {
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
				if _, err := r.eval(l); err != nil {
					return 0, err
				}
			}
		}
	}
	out := map[string]uint32{}
	for i, v := range b.regs {
		if v == nil || i == 4 {
			continue
		}
		x, err := r.eval(v)
		if err != nil {
			return 0, err
		}
		out[registerNames[i]] = x
	}
	if b.spExpr != nil {
		x, err := r.eval(b.spExpr)
		if err != nil {
			return 0, err
		}
		out["ESP"] = x
	}
	var fl *runFlags
	if b.fl.kind != "" && b.fl.kind != "machine" {
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
	case "switch":
		i, err := r.eval(t.index)
		if err != nil {
			return 0, err
		}
		if int(i) >= len(t.targets) {
			return 0, fmt.Errorf("switch index %d outside %d cases", i, len(t.targets))
		}
		next = t.targets[i]
	case "jumpind":
		a, err := r.eval(t.index)
		if err != nil {
			return 0, err
		}
		next = a
	default:
		return 0, fmt.Errorf("block ends with %s", t.kind)
	}
	for name, v := range out {
		r.Regs[name] = v
	}
	if fl != nil {
		r.fl = *fl
	}
	return next, nil
}

// Every load a block makes, by the effect count it was recorded at
func loadsByEpoch(b *liftedBlock) map[int][]Expr {
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
	for _, e := range b.shown() {
		visit(e)
	}
	return out
}

func (r *MachineRun) effect(e Effect) error {
	switch e.Kind {
	case "store":
		v, err := r.eval(e.Value)
		if err != nil {
			return err
		}
		addr := uint32(e.Off)
		if e.Addr != nil {
			if addr, err = r.eval(e.Addr); err != nil {
				return err
			}
		} else if e.Base != "" {
			return fmt.Errorf("store to %s", e.Base)
		}
		if e.Size > 4 {
			return fmt.Errorf("store of %d bytes", e.Size)
		}
		r.store(addr, e.Size, v)
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
			for i := uint32(0); i < count; i++ {
				r.store(dst, e.Size, r.load(src, e.Size))
				dst += step
				src += step
			}
			return nil
		}
		value, err := r.eval(e.Args[0])
		if err != nil {
			return err
		}
		count, value = value, count
		for i := uint32(0); i < count; i++ {
			r.store(dst, e.Size, value)
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
	default:
		return fmt.Errorf("%s effect", e.Kind)
	}
	return nil
}

// The processor operations a derivation run answers
func (r *MachineRun) cpu(fn string, args []uint32) (uint32, error) {
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
	default:
		return 0, fmt.Errorf("processor operation %s", fn)
	}
	return 0, nil
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
	switch v := e.(type) {
	case IConst:
		return v.V, nil
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
		a, err := r.eval(v.A)
		if err != nil {
			return 0, err
		}
		switch v.Op {
		case "zx8":
			return a & 0xFF, nil
		case "zx16":
			return a & 0xFFFF, nil
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
	for i, b := range data {
		r.mem[addr+uint32(i)] = b
	}
}

// Written is every byte the run wrote, by address
func (r *MachineRun) Written() map[uint32]byte {
	out := make(map[uint32]byte, len(r.mem))
	for k, v := range r.mem {
		out[k] = v
	}
	return out
}

// Adopt copies another run's memory over this run's
func (r *MachineRun) Adopt(o *MachineRun) {
	for k, v := range o.mem {
		r.mem[k] = v
	}
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
