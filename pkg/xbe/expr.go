package xbe

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"math"
	"math/bits"
	"sort"
	"strings"
	"unsafe"
)

// Expr is a value computed by symbolically executed x86 code.
type Expr interface {
	exprTag()
}

// IConst is a 32 bit integer value.
type IConst struct{ V uint32 }

// FConst is a floating point value.
type FConst struct{ V float64 }

// Sym is a named runtime input such as the node pointer or the clock.
type Sym struct{ Name string }

// Ptr is an address formed from a base object plus a constant offset.
type Ptr struct {
	Base string
	Off  int32
}

// Load reads memory at a symbolic location, as the memory stood after the effects recorded before it.
type Load struct {
	Base  string
	Off   int32
	Size  int
	Addr  Expr // set when the address itself is symbolic
	Epoch int  // effects recorded before the load, which the runtime reads the memory as of
}

// Bin is a binary integer or float operation.
type Bin struct {
	Op   string
	A, B Expr
}

// Un is a unary operation or conversion.
type Un struct {
	Op string
	A  Expr
}

// Cond is a boolean predicate on two values.
type Cond struct {
	Op   string
	A, B Expr
}

// FCmpSet is a float comparison that holds when the outcome is in the set.
type FCmpSet struct {
	A, B Expr
	Set  uint8 // bit 1 less, bit 2 equal, bit 4 greater, bit 8 unordered
}

// Not negates a predicate.
type Not struct{ A Expr }

// Block is a vector or matrix value assembled from scalar elements.
type Block struct{ Elems []Expr }

// Elem is one float element of a vector or matrix value.
type Elem struct {
	A     Expr
	Index int
}

// Sel is one of two values chosen by a predicate, the conditional move's result.
type Sel struct {
	C, A, B Expr
}

// CallResult is the value returned by an opaque call.
type CallResult struct {
	Fn     string
	Args   []Expr
	Seq    int
	This   Expr   // ecx at the call for helpers of unknown convention
	Target uint32 // the routine a named call reaches, where the image carries it
}

func (IConst) exprTag()     {}
func (FConst) exprTag()     {}
func (Sym) exprTag()        {}
func (Ptr) exprTag()        {}
func (Load) exprTag()       {}
func (Bin) exprTag()        {}
func (Un) exprTag()         {}
func (Cond) exprTag()       {}
func (FCmpSet) exprTag()    {}
func (Not) exprTag()        {}
func (CallResult) exprTag() {}
func (Block) exprTag()      {}
func (Elem) exprTag()       {}
func (Sel) exprTag()        {}

// exprKey identifies the box an interface value holds, the same wherever one node is shared
type exprKey struct{ typ, data unsafe.Pointer }

// The identity of a node, read from the two words of the interface value
func keyOf(e Expr) exprKey {
	w := (*[2]unsafe.Pointer)(unsafe.Pointer(&e))
	return exprKey{w[0], w[1]}
}

// Calls f on each node an expression is built from
func eachChild(e Expr, f func(Expr)) {
	switch v := e.(type) {
	case Load:
		if v.Addr != nil {
			f(v.Addr)
		}
	case Bin:
		f(v.A)
		f(v.B)
	case Un:
		f(v.A)
	case Cond:
		f(v.A)
		f(v.B)
	case FCmpSet:
		f(v.A)
		f(v.B)
	case Not:
		f(v.A)
	case CallResult:
		for _, a := range v.Args {
			f(a)
		}
		if v.This != nil {
			f(v.This)
		}
	case Block:
		for _, a := range v.Elems {
			f(a)
		}
	case Elem:
		f(v.A)
	case Sel:
		f(v.C)
		f(v.A)
		f(v.B)
	}
}

// Reports whether an expression is built from other nodes
func hasChildren(e Expr) bool {
	found := false
	eachChild(e, func(Expr) { found = true })
	return found
}

// Calls visit once for every node reachable from an expression, nodes met before through seen skipped
func walkExpr(e Expr, seen map[exprKey]bool, visit func(Expr)) {
	if e == nil {
		return
	}
	k := keyOf(e)
	if seen[k] {
		return
	}
	seen[k] = true
	visit(e)
	eachChild(e, func(c Expr) { walkExpr(c, seen, visit) })
}

// Serializes one node as a JSON object, its children replaced by the indices child hands back
func exprNode(e Expr, child func(Expr) int) map[string]any {
	switch v := e.(type) {
	case nil:
		return map[string]any{"k": "undef"}
	case IConst:
		return map[string]any{"k": "i", "v": v.V}
	case FConst:
		if math.IsNaN(v.V) || math.IsInf(v.V, 0) {
			return map[string]any{"k": "f", "s": fmt.Sprint(v.V)}
		}
		return map[string]any{"k": "f", "v": v.V}
	case Sym:
		return map[string]any{"k": "sym", "name": v.Name}
	case Ptr:
		return map[string]any{"k": "ptr", "base": v.Base, "off": v.Off}
	case Load:
		m := map[string]any{"k": "load", "base": v.Base, "off": v.Off, "size": v.Size}
		if v.Addr != nil {
			m["addr"] = child(v.Addr)
		}
		if v.Epoch > 0 {
			m["e"] = v.Epoch
		}
		return m
	case Bin:
		return map[string]any{"k": "bin", "op": v.Op, "a": child(v.A), "b": child(v.B)}
	case Un:
		return map[string]any{"k": "un", "op": v.Op, "a": child(v.A)}
	case Cond:
		return map[string]any{"k": "cond", "op": v.Op, "a": child(v.A), "b": child(v.B)}
	case FCmpSet:
		return map[string]any{"k": "fcmp", "a": child(v.A), "b": child(v.B), "set": v.Set}
	case Not:
		return map[string]any{"k": "not", "a": child(v.A)}
	case CallResult:
		args := make([]int, len(v.Args))
		for i, a := range v.Args {
			args[i] = child(a)
		}
		m := map[string]any{"k": "call", "fn": v.Fn, "args": args, "seq": v.Seq}
		if v.This != nil {
			m["this"] = child(v.This)
		}
		if v.Target != 0 {
			m["target"] = v.Target
		}
		return m
	case RetAddr:
		return map[string]any{"k": "retaddr", "va": v.VA}
	case Block:
		elems := make([]int, len(v.Elems))
		for i, a := range v.Elems {
			elems[i] = child(a)
		}
		return map[string]any{"k": "block", "elems": elems}
	case Elem:
		return map[string]any{"k": "elem", "a": child(v.A), "i": v.Index}
	case Sel:
		return map[string]any{"k": "sel", "c": child(v.C), "a": child(v.A), "b": child(v.B)}
	}
	panic(fmt.Sprintf("exprNode: %T", e))
}

// ExprPool deduplicates expression graphs into an indexed table for compact JSON
type ExprPool struct {
	Nodes []map[string]any `json:"nodes"`
	index map[string]int
}

// NewExprPool makes a pool whose index zero holds the undefined value, so an omitted index names no expression.
func NewExprPool() *ExprPool {
	pool := &ExprPool{index: map[string]int{}}
	pool.Add(nil)
	return pool
}

// Add interns an expression and returns its node index, each node it shares interned once
func (pool *ExprPool) Add(e Expr) int {
	return pool.interner().add(e)
}

// interner remembers the index of every node one structure's walk interned, so a shared node is interned once
type interner struct {
	pool *ExprPool
	memo map[exprKey]int
}

// Starts the walk interning one structure's expressions
func (pool *ExprPool) interner() *interner {
	return &interner{pool: pool, memo: map[exprKey]int{}}
}

// Interns a node after its children
func (in *interner) add(e Expr) int {
	if e == nil {
		return in.pool.addNode(exprNode(nil, nil))
	}
	k := keyOf(e)
	if idx, ok := in.memo[k]; ok {
		return idx
	}
	idx := in.pool.addNode(exprNode(e, in.add))
	in.memo[k] = idx
	return idx
}

func (pool *ExprPool) addNode(node map[string]any) int {
	b, _ := json.Marshal(node)
	key := string(b)
	if idx, ok := pool.index[key]; ok {
		return idx
	}
	pool.Nodes = append(pool.Nodes, node)
	pool.index[key] = len(pool.Nodes) - 1
	return len(pool.Nodes) - 1
}

// String renders an expression in C like notation for the dump output.
func ExprString(e Expr) string {
	return formatExpr(e, ExprString)
}

// Renders one node in the dump notation, child handing back the text of each node it is built from
func formatExpr(e Expr, child func(Expr) string) string {
	switch v := e.(type) {
	case nil:
		return "undef"
	case IConst:
		if v.V < 1024 {
			return fmt.Sprintf("%d", v.V)
		}
		return fmt.Sprintf("0x%X", v.V)
	case FConst:
		return fmt.Sprintf("%g", v.V)
	case Sym:
		return v.Name
	case Ptr:
		if v.Off == 0 {
			return "&" + v.Base
		}
		return fmt.Sprintf("&%s+0x%X", v.Base, v.Off)
	case Load:
		if v.Addr != nil {
			return fmt.Sprintf("mem%d[%s]", v.Size, child(v.Addr))
		}
		if v.Base == "" {
			return fmt.Sprintf("g%d[0x%X]", v.Size, uint32(v.Off))
		}
		return fmt.Sprintf("%s.%d@0x%X", v.Base, v.Size, v.Off)
	case Bin:
		return fmt.Sprintf("(%s %s %s)", child(v.A), v.Op, child(v.B))
	case Un:
		return fmt.Sprintf("%s(%s)", v.Op, child(v.A))
	case Cond:
		return fmt.Sprintf("(%s %s %s)", child(v.A), v.Op, child(v.B))
	case FCmpSet:
		var parts []string
		for _, p := range []struct {
			bit  uint8
			name string
		}{{1, "lt"}, {2, "eq"}, {4, "gt"}, {8, "unord"}} {
			if v.Set&p.bit != 0 {
				parts = append(parts, p.name)
			}
		}
		return fmt.Sprintf("fcmp(%s, %s)in{%s}", child(v.A), child(v.B), strings.Join(parts, ","))
	case Not:
		return "!" + child(v.A)
	case CallResult:
		args := make([]string, len(v.Args))
		for i, a := range v.Args {
			args[i] = child(a)
		}
		return fmt.Sprintf("%s#%d(%s)", v.Fn, v.Seq, strings.Join(args, ", "))
	case RetAddr:
		return fmt.Sprintf("retaddr(%08X)", v.VA)
	case Block:
		parts := make([]string, len(v.Elems))
		for i, a := range v.Elems {
			parts[i] = child(a)
		}
		return "{" + strings.Join(parts, ", ") + "}"
	case Elem:
		return fmt.Sprintf("%s[%d]", child(v.A), v.Index)
	case Sel:
		return fmt.Sprintf("(%s ? %s : %s)", child(v.C), child(v.A), child(v.B))
	}
	return fmt.Sprintf("%T", e)
}

// Whether a call result added to is taken as the base of an object, which only the path summaries of the dashboard do
var resultPointers = true

func isConst(e Expr) (uint32, bool) {
	c, ok := e.(IConst)
	return c.V, ok
}

func isFConst(e Expr) (float64, bool) {
	c, ok := e.(FConst)
	return c.V, ok
}

// Builds an integer operation, folding constants and pointer offsets
func mkBin(op string, a, b Expr) Expr {
	if av, ok := isConst(a); ok {
		if bv, ok := isConst(b); ok {
			switch op {
			case "add":
				return IConst{av + bv}
			case "sub":
				return IConst{av - bv}
			case "mul":
				return IConst{av * bv}
			case "and":
				return IConst{av & bv}
			case "or":
				return IConst{av | bv}
			case "xor":
				return IConst{av ^ bv}
			case "shl":
				return IConst{av << (bv & 31)}
			case "shr":
				return IConst{av >> (bv & 31)}
			case "sar":
				return IConst{uint32(int32(av) >> (bv & 31))}
			case "imulhi":
				return IConst{uint32((int64(int32(av)) * int64(int32(bv))) >> 32)}
			case "mulhi":
				return IConst{uint32((uint64(av) * uint64(bv)) >> 32)}
			}
		}
	}
	// An opaque call result entering pointer arithmetic is an object base, two results added being plain integers,
	// except in a machine lift where every value is the number it is
	_, aResult := a.(CallResult)
	_, bResult := b.(CallResult)
	if cr, ok := a.(CallResult); ok && !bResult && (op == "add" || op == "sub") && resultPointers {
		a = Ptr{fmt.Sprintf("res%d", cr.Seq), 0}
	}
	if cr, ok := b.(CallResult); ok && !aResult && op == "add" && resultPointers {
		b = Ptr{fmt.Sprintf("res%d", cr.Seq), 0}
	}
	if p, ok := a.(Ptr); ok {
		if bv, ok := isConst(b); ok {
			switch op {
			case "add":
				return Ptr{p.Base, p.Off + int32(bv)}
			case "sub":
				return Ptr{p.Base, p.Off - int32(bv)}
			}
		}
	}
	if p, ok := b.(Ptr); ok && op == "add" {
		if av, ok := isConst(a); ok {
			return Ptr{p.Base, p.Off + int32(av)}
		}
	}
	if bv, ok := isConst(b); ok {
		switch {
		case (op == "add" || op == "sub" || op == "or" || op == "xor" || op == "shl" || op == "shr" || op == "sar") && bv == 0:
			return a
		case op == "and" && bv == 0:
			return IConst{0}
		case op == "and" && bv == 0xFFFFFFFF:
			return a
		case op == "mul" && bv == 1:
			return a
		}
	}
	if (op == "and" || op == "or") && sameExpr(a, b) {
		return a
	}
	if op == "shr" {
		if k, ok := isConst(b); ok {
			if inner, ok := a.(Bin); ok {
				switch inner.Op {
				case "or", "xor":
					return mkBin(inner.Op, mkBin("shr", inner.A, b), mkBin("shr", inner.B, b))
				case "and":
					if c, ok := isConst(inner.B); ok {
						return mkBin("and", mkBin("shr", inner.A, b), IConst{c >> k})
					}
				case "shl":
					if k2, ok := isConst(inner.B); ok && k2 == k {
						return mkBin("and", inner.A, IConst{0xFFFFFFFF >> k})
					}
				}
			}
		}
	}
	if op == "and" {
		if c, ok := isConst(b); ok {
			if u, ok := a.(Un); ok && (u.Op == "zx8" && c&0xFF == 0xFF || u.Op == "zx16" && c&0xFFFF == 0xFFFF) {
				return a
			}
		}
	}
	if op == "and" {
		if u, ok := a.(Un); ok && u.Op == "zx8" {
			if bv, ok := isConst(b); ok && bv&0xFF == 0xFF {
				return a
			}
		}
	}
	return Bin{op, a, b}
}

// Float operations with two operands
var floatBinOps = map[string]bool{"fadd": true, "fsub": true, "fmul": true, "fdiv": true, "fmin": true, "fmax": true, "fmod": true, "fpow": true, "fatan2": true, "frem": true}

// Builds a selection between two values, folding a decided predicate and a choice between equals
func mkSel(c, a, b Expr) Expr {
	if v, ok := isConst(c); ok {
		if v != 0 {
			return a
		}
		return b
	}
	if sameExpr(a, b) {
		return a
	}
	return Sel{c, a, b}
}

// Builds a float operation, folding constants
func mkFBin(op string, a, b Expr) Expr {
	if av, ok := isFConst(a); ok {
		if bv, ok := isFConst(b); ok {
			switch op {
			case "fadd":
				return FConst{av + bv}
			case "fsub":
				return FConst{av - bv}
			case "fmul":
				return FConst{av * bv}
			case "fdiv":
				return FConst{av / bv}
			case "fmin":
				return FConst{math.Min(av, bv)}
			case "fmax":
				return FConst{math.Max(av, bv)}
			case "fmod":
				return FConst{math.Mod(av, bv)}
			case "fpow":
				return FConst{math.Pow(av, bv)}
			case "fatan2":
				return FConst{math.Atan2(av, bv)}
			case "frem":
				return FConst{math.Remainder(av, bv)}
			}
		}
	}
	return Bin{op, a, b}
}

func mkUn(op string, a Expr) Expr {
	if av, ok := isConst(a); ok {
		switch op {
		case "zx8":
			return IConst{av & 0xFF}
		case "zx16":
			return IConst{av & 0xFFFF}
		case "sx8":
			return IConst{uint32(int32(int8(av)))}
		case "sx16":
			return IConst{uint32(int32(int16(av)))}
		case "neg":
			return IConst{-av}
		case "not":
			return IConst{^av}
		case "i2f":
			return FConst{float64(int32(av))}
		case "bits2f":
			return FConst{float64(math.Float32frombits(av))}
		case "bsf":
			return IConst{uint32(bits.TrailingZeros32(av)) & 31}
		case "bsr":
			return IConst{uint32(31-bits.LeadingZeros32(av)) & 31}
		}
	}
	if av, ok := isFConst(a); ok {
		switch op {
		case "fneg":
			return FConst{-av}
		case "fabs":
			return FConst{math.Abs(av)}
		case "fsin":
			return FConst{math.Sin(av)}
		case "fcos":
			return FConst{math.Cos(av)}
		case "fsqrt":
			return FConst{math.Sqrt(av)}
		case "ftan":
			return FConst{math.Tan(av)}
		case "ffloor":
			return FConst{math.Floor(av)}
		case "fceil":
			return FConst{math.Ceil(av)}
		case "fexp":
			return FConst{math.Exp(av)}
		case "flog":
			return FConst{math.Log(av)}
		case "flog10":
			return FConst{math.Log10(av)}
		case "flog2":
			return FConst{math.Log2(av)}
		case "ftrunc":
			return FConst{math.Trunc(av)}
		case "fround":
			return FConst{math.RoundToEven(av)}
		case "fatan":
			return FConst{math.Atan(av)}
		case "fasin":
			return FConst{math.Asin(av)}
		case "facos":
			return FConst{math.Acos(av)}
		case "fsinh":
			return FConst{math.Sinh(av)}
		case "fcosh":
			return FConst{math.Cosh(av)}
		case "ftanh":
			return FConst{math.Tanh(av)}
		case "f2i":
			return IConst{uint32(int32(av))}
		case "f2i_rn":
			return IConst{uint32(int32(math.RoundToEven(av)))}
		case "f32":
			return FConst{float64(float32(av))}
		case "f2bits":
			return IConst{math.Float32bits(float32(av))}
		}
	}
	if u, ok := a.(Un); ok && (op == "zx8" && u.Op == "zx8" || op == "zx16" && (u.Op == "zx16" || u.Op == "zx8")) {
		return a
	}
	if u, ok := a.(Un); ok && (op == "bits2f" && u.Op == "f2bits" || op == "f2bits" && u.Op == "bits2f") {
		return u.A
	}
	if op == "zx8" || op == "zx16" {
		mask := uint32(0xFF)
		if op == "zx16" {
			mask = 0xFFFF
		}
		if b, ok := a.(Bin); ok {
			switch b.Op {
			case "or", "xor":
				return mkBin(b.Op, mkUn(op, b.A), mkUn(op, b.B))
			case "and":
				if c, ok := isConst(b.B); ok {
					if c&mask == 0 {
						return IConst{0}
					}
					return mkBin("and", mkUn(op, b.A), IConst{c & mask})
				}
				return mkBin("and", mkUn(op, b.A), mkUn(op, b.B))
			case "shl":
				if c, ok := isConst(b.B); ok && c >= 8 && (op == "zx8" || c >= 16) {
					return IConst{0}
				}
			}
		}
	}
	return Un{op, a}
}

// Builds a predicate, folding when both sides are constant
func mkCond(op string, a, b Expr) Expr {
	av, aok := isConst(a)
	bv, bok := isConst(b)
	if aok && bok {
		var r bool
		switch op {
		case "eq":
			r = av == bv
		case "ne":
			r = av != bv
		case "lt":
			r = int32(av) < int32(bv)
		case "le":
			r = int32(av) <= int32(bv)
		case "gt":
			r = int32(av) > int32(bv)
		case "ge":
			r = int32(av) >= int32(bv)
		case "ult":
			r = av < bv
		case "ule":
			r = av <= bv
		case "ugt":
			r = av > bv
		case "uge":
			r = av >= bv
		default:
			return Cond{op, a, b}
		}
		if r {
			return IConst{1}
		}
		return IConst{0}
	}
	if pa, ok := a.(Ptr); ok {
		if pb, ok := b.(Ptr); ok && pa.Base == pb.Base {
			return mkCond(op, IConst{uint32(pa.Off)}, IConst{uint32(pb.Off)})
		}
		// A pointer to an object the path holds is never null, where a global holds one only once set, the receiver is
		// whatever the caller passed and a call result is an integer until the call runs, so those stay tests
		if _, held := globalObjectAddress(Ptr{pa.Base, 0}); bok && bv == 0 && pa.Base != "" && pa.Base != "this" && !held && !strings.HasPrefix(pa.Base, "res") {
			switch op {
			case "eq":
				return IConst{0}
			case "ne":
				return IConst{1}
			}
		}
	}
	return Cond{op, a, b}
}

// Evaluates a float comparison set when both operands are constant
func mkFCmp(a, b Expr, set uint8) Expr {
	av, aok := isFConst(a)
	bv, bok := isFConst(b)
	if aok && bok {
		var bit uint8
		switch {
		case math.IsNaN(av) || math.IsNaN(bv):
			bit = 8
		case av < bv:
			bit = 1
		case av == bv:
			bit = 2
		default:
			bit = 4
		}
		if set&bit != 0 {
			return IConst{1}
		}
		return IConst{0}
	}
	return FCmpSet{a, b, set}
}

func mkNot(c Expr) Expr {
	if v, ok := isConst(c); ok {
		if v != 0 {
			return IConst{0}
		}
		return IConst{1}
	}
	if n, ok := c.(Not); ok {
		return n.A
	}
	if f, ok := c.(FCmpSet); ok {
		return FCmpSet{f.A, f.B, ^f.Set & 15}
	}
	if cc, ok := c.(Cond); ok {
		inv := map[string]string{"eq": "ne", "ne": "eq", "lt": "ge", "ge": "lt", "le": "gt", "gt": "le", "ult": "uge", "uge": "ult", "ule": "ugt", "ugt": "ule"}
		if o, ok := inv[cc.Op]; ok {
			return Cond{o, cc.A, cc.B}
		}
	}
	return Not{c}
}

// Collects the names of every symbolic input an expression depends on, each shared node visited once
func exprSyms(e Expr, out map[string]bool) {
	walkExpr(e, map[exprKey]bool{}, func(x Expr) {
		switch v := x.(type) {
		case Sym:
			out[v.Name] = true
		case Load:
			if v.Addr == nil {
				out[fmt.Sprintf("%s@%X", v.Base, v.Off)] = true
			}
		}
	})
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Nodes a structural comparison visits before giving up on shared or deep expression trees
const sameExprBudget = 512

// Reports whether two expressions are structurally identical, false when they are too large to compare
func sameExpr(a, b Expr) bool {
	budget := sameExprBudget
	return sameExprWithin(a, b, &budget)
}

func sameExprWithin(a, b Expr, budget *int) bool {
	if keyOf(a) == keyOf(b) {
		return true
	}
	*budget--
	if *budget < 0 {
		return false
	}
	return sameNode(a, b, func(x, y Expr) bool { return sameExprWithin(x, y, budget) })
}

// Reports whether two expressions are the same structure however large, each pair of shared nodes compared once
func equalExprs(a, b Expr) bool {
	return equalExprsWithin(a, b, map[[2]exprKey]bool{})
}

func equalExprsWithin(a, b Expr, seen map[[2]exprKey]bool) bool {
	pair := [2]exprKey{keyOf(a), keyOf(b)}
	if pair[0] == pair[1] {
		return true
	}
	if eq, ok := seen[pair]; ok {
		return eq
	}
	eq := sameNode(a, b, func(x, y Expr) bool { return equalExprsWithin(x, y, seen) })
	seen[pair] = eq
	return eq
}

// Compares two nodes field by field, same deciding each pair of children
func sameNode(a, b Expr, same func(x, y Expr) bool) bool {
	switch x := a.(type) {
	case nil:
		return b == nil
	case IConst, FConst, Sym, Ptr, RetAddr:
		return a == b
	case Load:
		y, ok := b.(Load)
		return ok && x.Base == y.Base && x.Off == y.Off && x.Size == y.Size && same(x.Addr, y.Addr)
	case Bin:
		y, ok := b.(Bin)
		return ok && x.Op == y.Op && same(x.A, y.A) && same(x.B, y.B)
	case Un:
		y, ok := b.(Un)
		return ok && x.Op == y.Op && same(x.A, y.A)
	case Cond:
		y, ok := b.(Cond)
		return ok && x.Op == y.Op && same(x.A, y.A) && same(x.B, y.B)
	case FCmpSet:
		y, ok := b.(FCmpSet)
		return ok && x.Set == y.Set && same(x.A, y.A) && same(x.B, y.B)
	case Not:
		y, ok := b.(Not)
		return ok && same(x.A, y.A)
	case Elem:
		y, ok := b.(Elem)
		return ok && x.Index == y.Index && same(x.A, y.A)
	case Sel:
		y, ok := b.(Sel)
		return ok && same(x.C, y.C) && same(x.A, y.A) && same(x.B, y.B)
	case Block:
		y, ok := b.(Block)
		if !ok || len(x.Elems) != len(y.Elems) {
			return false
		}
		for i := range x.Elems {
			if !same(x.Elems[i], y.Elems[i]) {
				return false
			}
		}
		return true
	case CallResult:
		y, ok := b.(CallResult)
		return ok && x.Seq == y.Seq && x.Fn == y.Fn
	}
	return false
}

// Digests of the nodes one walk met, each shared node digested once
type digestMemo map[exprKey]uint64

// A digest of an expression's structure, equal for equal structures and blind to what equalExprs ignores
func exprDigest(e Expr, memo digestMemo) uint64 {
	if e == nil {
		return 0
	}
	k := keyOf(e)
	if d, ok := memo[k]; ok {
		return d
	}
	h := fnv.New64a()
	var word [8]byte
	num := func(x uint64) {
		binary.LittleEndian.PutUint64(word[:], x)
		h.Write(word[:])
	}
	text := func(s string) {
		h.Write([]byte(s))
		h.Write([]byte{0})
	}
	switch v := e.(type) {
	case IConst:
		text("i")
		num(uint64(v.V))
	case FConst:
		text("f")
		num(math.Float64bits(v.V))
	case Sym:
		text("s")
		text(v.Name)
	case Ptr:
		text("p")
		text(v.Base)
		num(uint64(uint32(v.Off)))
	case RetAddr:
		text("r")
		num(uint64(v.VA))
		num(uint64(v.Clean))
	case Load:
		text("l")
		text(v.Base)
		num(uint64(uint32(v.Off)))
		num(uint64(v.Size))
		num(exprDigest(v.Addr, memo))
	case Bin:
		text("b")
		text(v.Op)
		num(exprDigest(v.A, memo))
		num(exprDigest(v.B, memo))
	case Un:
		text("u")
		text(v.Op)
		num(exprDigest(v.A, memo))
	case Cond:
		text("c")
		text(v.Op)
		num(exprDigest(v.A, memo))
		num(exprDigest(v.B, memo))
	case FCmpSet:
		text("m")
		num(uint64(v.Set))
		num(exprDigest(v.A, memo))
		num(exprDigest(v.B, memo))
	case Not:
		text("n")
		num(exprDigest(v.A, memo))
	case CallResult:
		text("k")
		text(v.Fn)
		num(uint64(v.Seq))
	case Block:
		text("B")
		num(uint64(len(v.Elems)))
		for _, x := range v.Elems {
			num(exprDigest(x, memo))
		}
	case Elem:
		text("e")
		num(uint64(v.Index))
		num(exprDigest(v.A, memo))
	case Sel:
		text("S")
		num(exprDigest(v.C, memo))
		num(exprDigest(v.A, memo))
		num(exprDigest(v.B, memo))
	default:
		panic(fmt.Sprintf("exprDigest: %T", e))
	}
	d := h.Sum64()
	memo[k] = d
	return d
}

// Tree nodes one path's expressions may expand to, past which the pool cannot carry them
const maxPathNodes = 1 << 21

// Counts the tree nodes of an expression against a budget, stopping once it runs out
func countNodes(e Expr, budget *int) {
	if *budget <= 0 {
		return
	}
	*budget--
	switch v := e.(type) {
	case Load:
		countNodes(v.Addr, budget)
	case Bin:
		countNodes(v.A, budget)
		countNodes(v.B, budget)
	case Un:
		countNodes(v.A, budget)
	case Cond:
		countNodes(v.A, budget)
		countNodes(v.B, budget)
	case FCmpSet:
		countNodes(v.A, budget)
		countNodes(v.B, budget)
	case Not:
		countNodes(v.A, budget)
	case Elem:
		countNodes(v.A, budget)
	case Sel:
		countNodes(v.C, budget)
		countNodes(v.A, budget)
		countNodes(v.B, budget)
	case Block:
		for _, x := range v.Elems {
			countNodes(x, budget)
		}
	case CallResult:
		countNodes(v.This, budget)
		for _, a := range v.Args {
			countNodes(a, budget)
		}
	}
}

// Reports whether a path's expressions expand to more tree nodes than the pool carries
func pathTooWide(p Path) bool {
	budget := maxPathNodes
	for _, g := range p.Guards {
		countNodes(g, &budget)
	}
	for _, e := range p.Effects {
		countNodes(e.Value, &budget)
		countNodes(e.Addr, &budget)
		countNodes(e.This, &budget)
		for _, a := range e.Args {
			countNodes(a, &budget)
		}
		for _, b := range e.Buffers {
			for _, x := range b.Elems {
				countNodes(x, &budget)
			}
		}
	}
	countNodes(p.Return, &budget)
	return budget <= 0
}
