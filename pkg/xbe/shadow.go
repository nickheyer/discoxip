package xbe

import (
	"fmt"
	"math/bits"
	"sort"
	"strings"
)

// Sym name prefix of an open input, a value nothing in the run fixes
const openPrefix = "open:"

// Every value a run computed from open inputs as an expression over them, beside the concrete value under the seeds
type shadowState struct {
	regs     map[string]Expr
	mem      map[uint32]shadowByte
	fl       shadowFlags
	results  map[int]Expr
	memo     map[exprKey]Expr
	memoDone map[exprKey]bool
	seeds    map[int]uint32
	sizes    map[int]int
	origin   map[int]OpenOrigin
	guards   []openGuard
	next     int
	stored   map[uint32]bool
	device   func(addr uint32) bool
	branch   *openBranch
	failed   error
}

// shadowByte is byte k of an open value held in memory
type shadowByte struct {
	e Expr
	k int
}

// shadowFlags are the open operands of the flag record
type shadowFlags struct {
	a, b, r, c Expr
}

// OpenOrigin is the stored global or device an open value was read from
type OpenOrigin struct {
	Addr   uint32
	Size   int
	Device bool
}

// openBranch is a block's terminator that read an open value, with the way the run took it
type openBranch struct {
	kind    string
	cond    Expr
	outcome bool
	index   uint32
	taken   uint32
	fall    uint32
	targets []uint32
	va      uint32
}

func newShadowState(stored map[uint32]bool, device func(addr uint32) bool) *shadowState {
	return &shadowState{regs: map[string]Expr{}, mem: map[uint32]shadowByte{}, results: map[int]Expr{}, memo: map[exprKey]Expr{}, memoDone: map[exprKey]bool{}, seeds: map[int]uint32{}, sizes: map[int]int{}, origin: map[int]OpenOrigin{}, stored: stored, device: device}
}

func (s *shadowState) clone() *shadowState {
	c := &shadowState{regs: make(map[string]Expr, len(s.regs)), mem: make(map[uint32]shadowByte, len(s.mem)), fl: s.fl, results: map[int]Expr{}, memo: map[exprKey]Expr{}, memoDone: map[exprKey]bool{}, seeds: make(map[int]uint32, len(s.seeds)), sizes: make(map[int]int, len(s.sizes)), origin: make(map[int]OpenOrigin, len(s.origin)), next: s.next, stored: s.stored, device: s.device}
	for k, v := range s.regs {
		c.regs[k] = v
	}
	for k, v := range s.mem {
		c.mem[k] = v
	}
	for k, v := range s.seeds {
		c.seeds[k] = v
	}
	for k, v := range s.sizes {
		c.sizes[k] = v
	}
	for k, v := range s.origin {
		c.origin[k] = v
	}
	c.guards = append([]openGuard(nil), s.guards...)
	return c
}

// Allocates an open input of a width, seeded with a value
func (s *shadowState) leaf(size int, seed uint32) Expr {
	id := s.next
	s.next++
	s.seeds[id] = seed
	s.sizes[id] = size
	return sizedLeaf(id, size)
}

func sizedLeaf(id, size int) Expr {
	sym := Sym{Name: fmt.Sprintf("%s%d", openPrefix, id)}
	switch size {
	case 1:
		return Un{Op: "zx8", A: sym}
	case 2:
		return Un{Op: "zx16", A: sym}
	}
	return sym
}

func leafID(e Expr) (int, bool) {
	sym, ok := e.(Sym)
	if !ok || !strings.HasPrefix(sym.Name, openPrefix) {
		return 0, false
	}
	var id int
	if _, err := fmt.Sscanf(sym.Name[len(openPrefix):], "%d", &id); err != nil {
		return 0, false
	}
	return id, true
}

// The open inputs an expression reads, in order
func openLeaves(e Expr) []int {
	seen := map[int]bool{}
	var out []int
	WalkExpr(e, func(n Expr) {
		if id, ok := leafID(n); ok && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	})
	sort.Ints(out)
	return out
}

// Evaluates an expression over open inputs under seeds for them, loads read through the reader given
func evalShadow(e Expr, seeds map[int]uint32, read func(addr uint32, size int) (uint32, error)) (uint32, error) {
	memo := map[exprKey]uint32{}
	var ev func(e Expr) (uint32, error)
	wide := func(e Expr) (uint64, error) {
		pair, ok := e.(Bin)
		if !ok || pair.Op != "hilo" {
			v, err := ev(e)
			return uint64(v), err
		}
		hi, err := ev(pair.A)
		if err != nil {
			return 0, err
		}
		lo, err := ev(pair.B)
		if err != nil {
			return 0, err
		}
		return uint64(hi)<<32 | uint64(lo), nil
	}
	ev = func(e Expr) (uint32, error) {
		if e == nil {
			return 0, fmt.Errorf("undefined open value")
		}
		k := keyOf(e)
		if v, ok := memo[k]; ok {
			return v, nil
		}
		v, err := evalShadowNode(e, seeds, ev, wide, read)
		if err != nil {
			return 0, err
		}
		memo[k] = v
		return v, nil
	}
	return ev(e)
}

func evalShadowNode(e Expr, seeds map[int]uint32, ev func(Expr) (uint32, error), wide func(Expr) (uint64, error), read func(addr uint32, size int) (uint32, error)) (uint32, error) {
	switch v := e.(type) {
	case IConst:
		return v.V, nil
	case Load:
		if v.Addr == nil || v.Base != "" {
			return 0, fmt.Errorf("load %s in an open value", ExprString(v))
		}
		a, err := ev(v.Addr)
		if err != nil {
			return 0, err
		}
		return read(a, v.Size)
	case Sym:
		id, ok := leafID(v)
		if !ok {
			return 0, fmt.Errorf("input %s in an open value", v.Name)
		}
		return seeds[id], nil
	case Bin:
		if strings.HasPrefix(v.Op, "udiv64") || strings.HasPrefix(v.Op, "idiv64") {
			n, err := wide(v.A)
			if err != nil {
				return 0, err
			}
			d, err := ev(v.B)
			if err != nil {
				return 0, err
			}
			if d == 0 {
				return 0, fmt.Errorf("division by zero")
			}
			rem := strings.HasSuffix(v.Op, "_rem")
			if strings.HasPrefix(v.Op, "udiv64") {
				if rem {
					return uint32(n % uint64(d)), nil
				}
				return uint32(n / uint64(d)), nil
			}
			if rem {
				return uint32(int32(int64(n) % int64(int32(d)))), nil
			}
			return uint32(int32(int64(n) / int64(int32(d)))), nil
		}
		a, err := ev(v.A)
		if err != nil {
			return 0, err
		}
		b, err := ev(v.B)
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
		case "udiv", "udiv_rem", "idiv", "idiv_rem":
			if b == 0 {
				return 0, fmt.Errorf("division by zero")
			}
			switch v.Op {
			case "udiv":
				return a / b, nil
			case "udiv_rem":
				return a % b, nil
			case "idiv":
				return uint32(int32(a) / int32(b)), nil
			}
			return uint32(int32(a) % int32(b)), nil
		case "mulhi":
			return uint32((uint64(a) * uint64(b)) >> 32), nil
		case "imulhi":
			return uint32((int64(int32(a)) * int64(int32(b))) >> 32), nil
		}
		return 0, fmt.Errorf("operation %s in an open value", v.Op)
	case Un:
		a, err := ev(v.A)
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
		return 0, fmt.Errorf("operation %s in an open value", v.Op)
	case Cond:
		a, err := ev(v.A)
		if err != nil {
			return 0, err
		}
		b, err := ev(v.B)
		if err != nil {
			return 0, err
		}
		c, err := compareValues(v.Op, a, b)
		if err != nil {
			return 0, err
		}
		if c {
			return 1, nil
		}
		return 0, nil
	case Not:
		a, err := ev(v.A)
		if err != nil {
			return 0, err
		}
		if a == 0 {
			return 1, nil
		}
		return 0, nil
	case Sel:
		c, err := ev(v.C)
		if err != nil {
			return 0, err
		}
		if c != 0 {
			return ev(v.A)
		}
		return ev(v.B)
	}
	return 0, fmt.Errorf("%T in an open value", e)
}

// Decides an integer comparison by its operator name
func compareValues(op string, a, b uint32) (bool, error) {
	switch op {
	case "eq":
		return a == b, nil
	case "ne":
		return a != b, nil
	case "lt":
		return int32(a) < int32(b), nil
	case "le":
		return int32(a) <= int32(b), nil
	case "gt":
		return int32(a) > int32(b), nil
	case "ge":
		return int32(a) >= int32(b), nil
	case "ult":
		return a < b, nil
	case "ule":
		return a <= b, nil
	case "ugt":
		return a > b, nil
	case "uge":
		return a >= b, nil
	}
	return false, fmt.Errorf("comparison %s", op)
}

// The expression over open inputs a value depends on, nothing where fixed, each shared node once per block
func (r *MachineRun) shadow(e Expr) Expr {
	if e == nil || r.sh == nil {
		return nil
	}
	k := keyOf(e)
	if r.sh.memoDone[k] {
		return r.sh.memo[k]
	}
	v := r.shadowNode(e)
	r.sh.memo[k] = v
	r.sh.memoDone[k] = true
	return v
}

func (r *MachineRun) shadowNode(e Expr) Expr {
	if isFloatExpr(e) {
		return nil
	}
	switch v := e.(type) {
	case Sym:
		switch {
		case strings.HasPrefix(v.Name, "reg."):
			return r.sh.regs[v.Name[4:]]
		case v.Name == "flags.a":
			return r.sh.fl.a
		case v.Name == "flags.b":
			return r.sh.fl.b
		case v.Name == "flags.r":
			return r.sh.fl.r
		case v.Name == "flags.cf":
			return r.sh.fl.c
		case strings.HasPrefix(v.Name, "cond."):
			return r.shadowCondition(v.Name[5:])
		}
		return nil
	case Load:
		if v.Size > 4 {
			return nil
		}
		addr := uint32(v.Off)
		var addrShadow Expr
		if v.Addr != nil {
			addrShadow = r.shadow(v.Addr)
			a, err := r.eval(v.Addr)
			if err != nil {
				return nil
			}
			addr = a
		} else if v.Base != "" {
			return nil
		}
		return r.loadShadow(addr, v.Size, addrShadow)
	case CallResult:
		return r.sh.results[v.Seq]
	case Bin:
		if v.Op == "f2icw" {
			return nil
		}
		a, b := r.shadow(v.A), r.shadow(v.B)
		if a == nil && b == nil {
			return nil
		}
		if v.Op == "and" {
			if k, ok := v.B.(IConst); ok && a != nil {
				return maskedShadow(a, k.V)
			}
		}
		return canonical(Bin{Op: v.Op, A: r.orConst(v.A, a), B: r.orConst(v.B, b)})
	case Un:
		switch v.Op {
		case "f2bits", "f2i", "f2i_rn":
			return nil
		}
		a := r.shadow(v.A)
		if a == nil {
			return nil
		}
		switch v.Op {
		case "zx8":
			return maskedShadow(a, 0xFF)
		case "zx16":
			return maskedShadow(a, 0xFFFF)
		case "sx8":
			return foldShadow(Un{Op: v.Op, A: pruneBits(a, 0xFF)})
		case "sx16":
			return foldShadow(Un{Op: v.Op, A: pruneBits(a, 0xFFFF)})
		}
		return canonical(Un{Op: v.Op, A: a})
	case Cond:
		a, b := r.shadow(v.A), r.shadow(v.B)
		if a == nil && b == nil {
			return nil
		}
		sh := Cond{Op: v.Op, A: r.orConst(v.A, a), B: r.orConst(v.B, b)}
		if linearize(Bin{Op: "sub", A: sh.A, B: sh.B}).fixed() {
			return nil
		}
		return sh
	case Not:
		a := r.shadow(v.A)
		if a == nil {
			return nil
		}
		return Not{A: a}
	case Sel:
		c, a, b := r.shadow(v.C), r.shadow(v.A), r.shadow(v.B)
		if c == nil && a == nil && b == nil {
			return nil
		}
		return Sel{C: r.orConst(v.C, c), A: r.orConst(v.A, a), B: r.orConst(v.B, b)}
	}
	return nil
}

// A value as a constant plus atoms scaled by coefficients, any node but sums, differences, negations and constant
// scalings an atom, atoms told apart by the node held
type linearForm struct {
	k     uint32
	atoms []Expr
	coef  map[exprKey]uint32
}

// Writes a value as a linear form
func linearize(e Expr) *linearForm {
	l := &linearForm{coef: map[exprKey]uint32{}}
	l.add(e, 1)
	return l
}

func (l *linearForm) add(e Expr, scale uint32) {
	switch v := e.(type) {
	case IConst:
		l.k += v.V * scale
		return
	case Bin:
		switch v.Op {
		case "add":
			l.add(v.A, scale)
			l.add(v.B, scale)
			return
		case "sub":
			l.add(v.A, scale)
			l.add(v.B, -scale)
			return
		case "mul":
			if k, ok := v.B.(IConst); ok {
				l.add(v.A, scale*k.V)
				return
			}
			if k, ok := v.A.(IConst); ok {
				l.add(v.B, scale*k.V)
				return
			}
		case "shl":
			if k, ok := v.B.(IConst); ok {
				l.add(v.A, scale<<(k.V&31))
				return
			}
		}
	case Un:
		if v.Op == "neg" {
			l.add(v.A, -scale)
			return
		}
	}
	k := keyOf(e)
	if _, seen := l.coef[k]; !seen {
		l.atoms = append(l.atoms, e)
	}
	l.coef[k] += scale
}

// Reports whether every atom cancels, leaving a value nothing open decides
func (l *linearForm) fixed() bool {
	for _, c := range l.coef {
		if c != 0 {
			return false
		}
	}
	return true
}

// The value as its scaled atoms in the order met, added up with the constant
func (l *linearForm) expr() Expr {
	var out Expr
	for _, a := range l.atoms {
		var term Expr
		switch c := l.coef[keyOf(a)]; c {
		case 0:
			continue
		case 1:
			term = a
		case 0xFFFFFFFF:
			term = Un{Op: "neg", A: a}
		default:
			term = Bin{Op: "mul", A: a, B: IConst{V: c}}
		}
		if out == nil {
			out = term
		} else {
			out = Bin{Op: "add", A: out, B: term}
		}
	}
	if out == nil {
		return IConst{V: l.k}
	}
	if l.k != 0 {
		out = Bin{Op: "add", A: out, B: IConst{V: l.k}}
	}
	return out
}

// The bits a value over open inputs may hold set, every bit where the operation bounds none
func mayBits(e Expr) uint32 {
	switch v := e.(type) {
	case IConst:
		return v.V
	case Bin:
		switch v.Op {
		case "and":
			return mayBits(v.A) & mayBits(v.B)
		case "or", "xor":
			return mayBits(v.A) | mayBits(v.B)
		case "shl":
			if k, ok := v.B.(IConst); ok {
				return mayBits(v.A) << (k.V & 31)
			}
		case "shr":
			if k, ok := v.B.(IConst); ok {
				return mayBits(v.A) >> (k.V & 31)
			}
		case "mul":
			if k, ok := v.B.(IConst); ok && k.V != 0 && k.V&(k.V-1) == 0 {
				return mayBits(v.A) << uint(bits.TrailingZeros32(k.V))
			}
		}
	case Un:
		switch v.Op {
		case "zx8":
			return mayBits(v.A) & 0xFF
		case "zx16":
			return mayBits(v.A) & 0xFFFF
		case "parity":
			return 1
		}
	case Cond, Not:
		return 1
	case Sel:
		return mayBits(v.A) | mayBits(v.B)
	}
	return 0xFFFFFFFF
}

// A value cut to the bits of a mask, operands contributing nothing to those bits dropped, equal to the value on the
// bits of the mask alone
func pruneBits(e Expr, mask uint32) Expr {
	if mayBits(e)&mask == 0 {
		return IConst{}
	}
	switch v := e.(type) {
	case IConst:
		return IConst{V: v.V & mask}
	case Bin:
		switch v.Op {
		case "or", "xor":
			a, b := pruneBits(v.A, mask), pruneBits(v.B, mask)
			if isZeroConst(a) {
				return b
			}
			if isZeroConst(b) {
				return a
			}
			return foldShadow(Bin{Op: v.Op, A: a, B: b})
		case "and":
			if k, ok := v.B.(IConst); ok {
				return foldShadow(Bin{Op: "and", A: pruneBits(v.A, mask&k.V), B: k})
			}
			return foldShadow(Bin{Op: "and", A: pruneBits(v.A, mask), B: pruneBits(v.B, mask)})
		case "shl":
			if k, ok := v.B.(IConst); ok {
				return foldShadow(Bin{Op: "shl", A: pruneBits(v.A, mask>>(k.V&31)), B: k})
			}
		case "shr":
			if k, ok := v.B.(IConst); ok {
				return foldShadow(Bin{Op: "shr", A: pruneBits(v.A, mask<<(k.V&31)), B: k})
			}
		case "mul":
			if k, ok := v.B.(IConst); ok && k.V != 0 && k.V&(k.V-1) == 0 {
				return foldShadow(Bin{Op: "mul", A: pruneBits(v.A, mask>>uint(bits.TrailingZeros32(k.V))), B: k})
			}
		}
	case Un:
		switch v.Op {
		case "zx8":
			return foldShadow(Un{Op: "zx8", A: pruneBits(v.A, mask&0xFF)})
		case "zx16":
			return foldShadow(Un{Op: "zx16", A: pruneBits(v.A, mask&0xFFFF)})
		case "sx8":
			inner := mask & 0xFF
			if mask>>8 != 0 {
				inner |= 0x80
			}
			return foldShadow(Un{Op: "sx8", A: pruneBits(v.A, inner)})
		case "sx16":
			inner := mask & 0xFFFF
			if mask>>16 != 0 {
				inner |= 0x8000
			}
			return foldShadow(Un{Op: "sx16", A: pruneBits(v.A, inner)})
		}
	}
	return e
}

func isZeroConst(e Expr) bool {
	c, ok := e.(IConst)
	return ok && c.V == 0
}

// An operation on constants alone as its constant, anything else as it is
func foldShadow(e Expr) Expr {
	switch v := e.(type) {
	case Bin:
		if _, ok := v.A.(IConst); !ok {
			return e
		}
		if _, ok := v.B.(IConst); !ok {
			return e
		}
	case Un:
		if _, ok := v.A.(IConst); !ok {
			return e
		}
	default:
		return e
	}
	c, err := evalShadow(e, nil, nil)
	if err != nil {
		return e
	}
	return IConst{V: c}
}

// A value masked to some bits, pruned to what feeds them, nothing where that leaves a constant
func maskedShadow(e Expr, mask uint32) Expr {
	p := pruneBits(e, mask)
	if _, isConst := p.(IConst); isConst {
		return nil
	}
	if mask == 0xFFFFFFFF {
		return p
	}
	return Bin{Op: "and", A: p, B: IConst{V: mask}}
}

// A sum, difference, negation or constant scaling as its linear form, nothing where that is fixed, other nodes as they are
func canonical(e Expr) Expr {
	switch v := e.(type) {
	case Bin:
		_, constA := v.A.(IConst)
		_, constB := v.B.(IConst)
		switch v.Op {
		case "add", "sub":
		case "mul":
			if !constA && !constB {
				return e
			}
		case "shl":
			if !constB {
				return e
			}
		default:
			return e
		}
	case Un:
		if v.Op != "neg" {
			return e
		}
	default:
		return e
	}
	l := linearize(e)
	if l.fixed() {
		return nil
	}
	return l.expr()
}

// The shadow of a value where it has one, else the value the run holds as a constant
func (r *MachineRun) orConst(e Expr, sh Expr) Expr {
	if sh != nil {
		return sh
	}
	v, err := r.eval(e)
	if err != nil && r.sh.failed == nil {
		r.sh.failed = fmt.Errorf("fixing an operand beside an open value: %w", err)
	}
	return IConst{V: v}
}

// The condition a jump decides from the flag record, over the open operands the record holds
func (r *MachineRun) shadowCondition(op string) Expr {
	f, s := r.fl, r.sh.fl
	if s.a == nil && s.b == nil && s.r == nil && s.c == nil {
		return nil
	}
	if f.kind == "fsw" || f.kind == "eflags" {
		if r.sh.failed == nil {
			r.sh.failed = fmt.Errorf("condition %s on an open %s record", op, f.kind)
		}
		return nil
	}
	fixed := func(sh Expr, v uint32) Expr {
		if sh != nil {
			return sh
		}
		return IConst{V: v}
	}
	a, b, res, cf := fixed(s.a, f.a), fixed(s.b, f.b), fixed(s.r, f.r), fixed(s.c, f.c)
	cmp := f.kind == "cmp" || f.kind == "sub"
	var zero Expr = Cond{Op: "eq", A: res, B: IConst{}}
	if cmp {
		zero = Cond{Op: "eq", A: a, B: b}
	}
	var sign Expr = Cond{Op: "lt", A: res, B: IConst{}}
	var carry Expr = IConst{}
	switch f.kind {
	case "cmp", "sub":
		carry = Cond{Op: "ult", A: a, B: b}
	case "add":
		carry = Cond{Op: "ult", A: res, B: a}
	case "alu":
		carry = Cond{Op: "ne", A: cf, B: IConst{}}
	}
	signed := func(op string) Expr {
		if cmp {
			return Cond{Op: op, A: a, B: b}
		}
		return Cond{Op: op, A: res, B: IConst{}}
	}
	switch op {
	case "JE":
		return zero
	case "JNE":
		return Not{A: zero}
	case "JL":
		if cmp {
			return signed("lt")
		}
		return sign
	case "JGE":
		if cmp {
			return signed("ge")
		}
		return Not{A: sign}
	case "JLE":
		return signed("le")
	case "JG":
		return signed("gt")
	case "JB":
		if cmp {
			return Cond{Op: "ult", A: a, B: b}
		}
		return carry
	case "JAE":
		if cmp {
			return Cond{Op: "uge", A: a, B: b}
		}
		return Not{A: carry}
	case "JBE":
		if cmp {
			return Cond{Op: "ule", A: a, B: b}
		}
		return Bin{Op: "or", A: carry, B: zero}
	case "JA":
		if cmp {
			return Cond{Op: "ugt", A: a, B: b}
		}
		return Not{A: Bin{Op: "or", A: carry, B: zero}}
	case "JS":
		return sign
	case "JNS":
		return Not{A: sign}
	}
	if r.sh.failed == nil {
		r.sh.failed = fmt.Errorf("condition %s after %s flags on an open value", op, f.kind)
	}
	return nil
}

// What a load reads of open values, written bytes as written, open addresses loads over image data, devices fresh, stored bytes fresh once
func (r *MachineRun) loadShadow(addr uint32, size int, addrShadow Expr) Expr {
	if addrShadow != nil && r.imageOnly(addr, size) {
		return Load{Addr: addrShadow, Size: size}
	}
	var parts [4]Expr
	open, unwritten := false, 0
	for i := 0; i < size; i++ {
		a := addr + uint32(i)
		if sb, ok := r.sh.mem[a]; ok {
			parts[i] = sb.e
			open = true
			continue
		}
		if _, written := r.mem.get(a); !written {
			unwritten++
		}
	}
	fresh := unwritten > 0 && r.sh.device(addr) && !r.overlaid(addr)
	if !open && !fresh {
		if unwritten > 0 && r.storedAt(addr, size) {
			leaf := r.sh.leaf(size, r.load(addr, size))
			r.sh.origin[r.sh.next-1] = OpenOrigin{Addr: addr, Size: size}
			for i := 0; i < size; i++ {
				r.sh.mem[addr+uint32(i)] = shadowByte{e: leaf, k: i}
			}
			r.epoch++
			return leaf
		}
		return nil
	}
	if !open && unwritten == size {
		leaf := r.sh.leaf(size, r.load(addr, size))
		r.sh.origin[r.sh.next-1] = OpenOrigin{Addr: addr, Size: size, Device: true}
		return leaf
	}
	same := true
	for i := 0; i < size; i++ {
		sb, ok := r.sh.mem[addr+uint32(i)]
		if !ok || sb.k != i || (i > 0 && keyOf(sb.e) != keyOf(parts[0])) {
			same = false
		}
	}
	if same {
		if size == 4 {
			return parts[0]
		}
		return maskedShadow(parts[0], 1<<(8*uint(size))-1)
	}
	var out Expr
	for i := 0; i < size; i++ {
		a := addr + uint32(i)
		var part Expr
		if sb, ok := r.sh.mem[a]; ok {
			part = maskedShadow(Bin{Op: "shr", A: sb.e, B: IConst{V: uint32(8 * sb.k)}}, 0xFF)
			if part == nil {
				part = IConst{V: uint32(r.ReadU8(a))}
			}
		} else if _, written := r.mem.get(a); !written && fresh {
			part = r.sh.leaf(1, uint32(r.ReadU8(a)))
			r.sh.origin[r.sh.next-1] = OpenOrigin{Addr: a, Size: 1, Device: true}
		} else {
			part = IConst{V: uint32(r.ReadU8(a))}
		}
		if i > 0 {
			part = foldShadow(Bin{Op: "shl", A: part, B: IConst{V: uint32(8 * i)}})
		}
		if out == nil {
			out = part
		} else {
			out = foldShadow(Bin{Op: "or", A: out, B: part})
		}
	}
	if _, isConst := out.(IConst); isConst {
		return nil
	}
	return out
}

// Reports whether an overlay answers reads of the address, a window the run laid over data a device address maps
func (r *MachineRun) overlaid(addr uint32) bool {
	for _, o := range r.Overlays {
		if addr >= o.Base && addr-o.Base < o.Size && len(o.Data) > 0 {
			return true
		}
	}
	return false
}

// Reports whether the bytes still read from the image alone, neither written by the run, nor open, nor a device
// A load there through an open address stays a load, since another seed reads other image bytes
// A load anywhere else reads what the run holds, as a load kept would read its own store back
func (r *MachineRun) imageOnly(addr uint32, size int) bool {
	if r.sh.device(addr) && !r.overlaid(addr) {
		return false
	}
	for i := 0; i < size; i++ {
		a := addr + uint32(i)
		if _, open := r.sh.mem[a]; open {
			return false
		}
		if _, written := r.mem.get(a); written {
			return false
		}
		if r.sh.stored[a] {
			return false
		}
	}
	return true
}

// Reports whether the kernel stores into any of the bytes
func (r *MachineRun) storedAt(addr uint32, size int) bool {
	for i := 0; i < size; i++ {
		if r.sh.stored[addr+uint32(i)] {
			return true
		}
	}
	return false
}

// Marks bytes as holding an open value, or as fixed where the value has none
func (r *MachineRun) setShadow(addr uint32, size int, sh Expr) {
	for i := 0; i < size; i++ {
		if sh == nil {
			delete(r.sh.mem, addr+uint32(i))
		} else {
			r.sh.mem[addr+uint32(i)] = shadowByte{e: sh, k: i}
		}
	}
}

// Changes the seeds of open inputs and recomputes every value the run holds that depends on them
func (r *MachineRun) reseed(seeds map[int]uint32) error {
	for id, v := range seeds {
		r.sh.seeds[id] = v
	}
	memo := map[exprKey]uint32{}
	read := r.readUnder(r.sh.seeds)
	value := func(e Expr) (uint32, error) {
		k := keyOf(e)
		if v, ok := memo[k]; ok {
			return v, nil
		}
		v, err := evalShadow(e, r.sh.seeds, read)
		if err != nil {
			return 0, err
		}
		memo[k] = v
		return v, nil
	}
	for name, e := range r.sh.regs {
		v, err := value(e)
		if err != nil {
			return err
		}
		r.Regs[name] = v
	}
	for addr, sb := range r.sh.mem {
		v, err := value(sb.e)
		if err != nil {
			return err
		}
		r.mem.set(addr, byte(v>>(8*uint(sb.k))))
	}
	for _, p := range []struct {
		sh  Expr
		dst *uint32
	}{{r.sh.fl.a, &r.fl.a}, {r.sh.fl.b, &r.fl.b}, {r.sh.fl.r, &r.fl.r}, {r.sh.fl.c, &r.fl.c}} {
		if p.sh == nil {
			continue
		}
		v, err := value(p.sh)
		if err != nil {
			return err
		}
		*p.dst = v
	}
	r.epoch++
	return nil
}

// A reader of the run's memory under seeds, open bytes by their expressions
// A load kept in an open value read image bytes, so a byte it reaches holds an open value only once the run stored
// one there later, a value read back through the same load reading the byte as the run holds it
func (r *MachineRun) readUnder(seeds map[int]uint32) func(addr uint32, size int) (uint32, error) {
	reading := map[uint32]bool{}
	var read func(addr uint32, size int) (uint32, error)
	read = func(addr uint32, size int) (uint32, error) {
		var v uint32
		for i := size - 1; i >= 0; i-- {
			a := addr + uint32(i)
			b := uint32(r.ReadU8(a))
			if sb, ok := r.sh.mem[a]; ok && !reading[a] {
				reading[a] = true
				w, err := evalShadow(sb.e, seeds, read)
				delete(reading, a)
				if err != nil {
					return 0, err
				}
				b = (w >> (8 * uint(sb.k))) & 0xFF
			}
			v = v<<8 | b
		}
		return v, nil
	}
	return read
}

// Makes the run follow open values, the bytes given and device reads open, tests on them reported as open branches
func (r *MachineRun) OpenShadows(stored map[uint32]bool, device func(addr uint32) bool) {
	r.sh = newShadowState(stored, device)
}

// OpenAt is the expression over open inputs bytes hold, nothing where they are fixed
func (r *MachineRun) OpenAt(addr uint32, size int) Expr {
	return r.loadShadow(addr, size, nil)
}

// Pinned reports an open value's reading under the seeds and whether no seed satisfying the path's tests changes it
func (r *MachineRun) Pinned(sh Expr) (uint32, bool, error) {
	v, err := evalShadow(sh, r.sh.seeds, r.readUnder(r.sh.seeds))
	if err != nil {
		return 0, false, err
	}
	seeds, decided, err := r.seedsFor(Cond{Op: "ne", A: sh, B: IConst{V: v}}, true)
	if err != nil {
		return 0, false, err
	}
	return v, decided && seeds == nil, nil
}

// PushOpen pushes a word holding an open value
func (r *MachineRun) PushOpen() {
	r.Push(0)
	r.MarkOpen(r.Regs["ESP"], 4)
}

// OpenRegister makes a register hold an open value seeded with what it holds
func (r *MachineRun) OpenRegister(name string) {
	r.sh.regs[name] = r.sh.leaf(4, r.Regs[name])
	r.epoch++
}

// MarkOpen makes bytes hold an open value seeded with what they hold
func (r *MachineRun) MarkOpen(addr uint32, size int) {
	r.setShadow(addr, size, r.sh.leaf(size, r.load(addr, size)))
	r.epoch++
}

// The stored globals an open value in bytes came from, and whether from those alone
func (r *MachineRun) OpenGlobals(addr uint32, size int) ([]OpenOrigin, bool) {
	sh := r.loadShadow(addr, size, nil)
	if sh == nil {
		return nil, false
	}
	seen := map[OpenOrigin]bool{}
	var out []OpenOrigin
	all := true
	for _, id := range openLeaves(sh) {
		o, ok := r.sh.origin[id]
		if !ok || o.Device {
			all = false
			continue
		}
		if !seen[o] {
			seen[o] = true
			out = append(out, o)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Addr < out[j].Addr })
	return out, all
}

// OpenDevices lists the device addresses the open value at the bytes was read from
func (r *MachineRun) OpenDevices(addr uint32, size int) []OpenOrigin {
	sh := r.loadShadow(addr, size, nil)
	if sh == nil {
		return nil
	}
	seen := map[OpenOrigin]bool{}
	var out []OpenOrigin
	for _, id := range openLeaves(sh) {
		if o, ok := r.sh.origin[id]; ok && o.Device && !seen[o] {
			seen[o] = true
			out = append(out, o)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Addr < out[j].Addr })
	return out
}

// IsOpen reports whether any of the bytes holds an open value
func (r *MachineRun) IsOpen(addr uint32, size int) bool {
	for i := 0; i < size; i++ {
		if _, ok := r.sh.mem[addr+uint32(i)]; ok {
			return true
		}
	}
	return false
}
