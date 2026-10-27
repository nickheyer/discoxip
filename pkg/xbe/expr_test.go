package xbe

import (
	"math"
	"reflect"
	"sort"
	"testing"
)

func TestExprStringNotation(t *testing.T) {
	c := Cond{"eq", Sym{"x"}, IConst{0}}
	cases := []struct {
		e    Expr
		want string
	}{
		{nil, "undef"},
		{IConst{5}, "5"},
		{IConst{1023}, "1023"},
		{IConst{1024}, "0x400"},
		{IConst{0xDEADBEEF}, "0xDEADBEEF"},
		{FConst{1.5}, "1.5"},
		{FConst{2}, "2"},
		{Sym{"arg0"}, "arg0"},
		{Ptr{"this", 0}, "&this"},
		{Ptr{"stack", 0x10}, "&stack+0x10"},
		{Load{Base: "this", Off: 4, Size: 4}, "this.4@0x4"},
		{Load{Base: "", Off: 0x21000, Size: 4}, "g4[0x21000]"},
		{Load{Addr: Sym{"p"}, Size: 2}, "mem2[p]"},
		{Bin{"add", IConst{1}, Sym{"x"}}, "(1 add x)"},
		{Un{"zx8", Sym{"x"}}, "zx8(x)"},
		{c, "(x eq 0)"},
		{FCmpSet{Sym{"a"}, Sym{"b"}, 1 | 8}, "fcmp(a, b)in{lt,unord}"},
		{FCmpSet{Sym{"a"}, Sym{"b"}, 2 | 4}, "fcmp(a, b)in{eq,gt}"},
		{Not{c}, "!(x eq 0)"},
		{CallResult{Fn: "sub_00011000", Seq: 3, Args: []Expr{IConst{1}, Sym{"y"}}}, "sub_00011000#3(1, y)"},
		{RetAddr{VA: 0x11005}, "retaddr(00011005)"},
		{Block{[]Expr{FConst{1}, FConst{2}}}, "{1, 2}"},
		{Elem{Sym{"m"}, 3}, "m[3]"},
		{Sel{c, Sym{"a"}, Sym{"b"}}, "((x eq 0) ? a : b)"},
	}
	for _, tc := range cases {
		if got := ExprString(tc.e); got != tc.want {
			t.Errorf("ExprString(%#v) = %q, want %q", tc.e, got, tc.want)
		}
	}
}

func TestFloatOperatorsFold(t *testing.T) {
	cases := []struct {
		op   string
		a, b float64
		want float64
	}{
		{"fadd", 1, 2, 3},
		{"fsub", 5, 2, 3},
		{"fmul", 3, 4, 12},
		{"fdiv", 9, 2, 4.5},
		{"fmin", 3, -1, -1},
		{"fmax", 3, -1, 3},
		{"fmod", 7, 3, 1},
		{"fpow", 2, 10, 1024},
		{"fatan2", 0, 1, 0},
		{"frem", 7, 3, 1},
	}
	if len(floatBinOps) != 10 {
		t.Fatalf("%d float operators, want 10", len(floatBinOps))
	}
	for _, tc := range cases {
		if !floatBinOps[tc.op] {
			t.Errorf("%s is not a float operator", tc.op)
		}
		got := mkFBin(tc.op, FConst{tc.a}, FConst{tc.b})
		if f, ok := got.(FConst); !ok || f.V != tc.want {
			t.Errorf("%s(%g, %g) = %s, want %g", tc.op, tc.a, tc.b, ExprString(got), tc.want)
		}
		sym := mkFBin(tc.op, Sym{"x"}, FConst{tc.b})
		if !reflect.DeepEqual(sym, Bin{tc.op, Sym{"x"}, FConst{tc.b}}) {
			t.Errorf("%s with a symbol = %s", tc.op, ExprString(sym))
		}
	}
}

func TestExprSymsNamesLoads(t *testing.T) {
	e := Bin{"add", Load{Base: "this", Off: 0x10, Size: 4}, Bin{"mul", Sym{"arg1"}, Load{Addr: Sym{"p"}, Size: 4}}}
	set := map[string]bool{}
	exprSyms(e, set)
	got := sortedKeys(set)
	want := []string{"arg1", "p", "this@10"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("syms = %v, want %v", got, want)
	}
}

func TestMkBinConstantFolding(t *testing.T) {
	a, b := uint32(0xF0000003), uint32(5)
	cases := map[string]uint32{
		"add":    a + b,
		"sub":    a - b,
		"mul":    a * b,
		"and":    a & b,
		"or":     a | b,
		"xor":    a ^ b,
		"shl":    a << 5,
		"shr":    a >> 5,
		"sar":    uint32(int32(a) >> 5),
		"imulhi": uint32((int64(int32(a)) * int64(int32(b))) >> 32),
		"mulhi":  uint32((uint64(a) * uint64(b)) >> 32),
	}
	for op, want := range cases {
		got := mkBin(op, IConst{a}, IConst{b})
		if c, ok := got.(IConst); !ok || c.V != want {
			t.Errorf("%s = %s, want %#x", op, ExprString(got), want)
		}
	}
	if got := mkBin("shl", IConst{1}, IConst{33}); got != Expr(IConst{2}) {
		t.Errorf("shl by 33 = %s, want 2", ExprString(got))
	}
	if got := mkBin("udiv", IConst{7}, IConst{2}); !reflect.DeepEqual(got, Bin{"udiv", IConst{7}, IConst{2}}) {
		t.Errorf("udiv folded to %s", ExprString(got))
	}
}

func TestMkBinIdentities(t *testing.T) {
	x := Sym{"x"}
	cases := []struct {
		name string
		got  Expr
		want Expr
	}{
		{"add 0", mkBin("add", x, IConst{0}), x},
		{"sub 0", mkBin("sub", x, IConst{0}), x},
		{"or 0", mkBin("or", x, IConst{0}), x},
		{"xor 0", mkBin("xor", x, IConst{0}), x},
		{"shl 0", mkBin("shl", x, IConst{0}), x},
		{"and 0", mkBin("and", x, IConst{0}), IConst{0}},
		{"and all", mkBin("and", x, IConst{0xFFFFFFFF}), x},
		{"mul 1", mkBin("mul", x, IConst{1}), x},
		{"and same", mkBin("and", x, x), x},
		{"or same", mkBin("or", x, x), x},
		{"ptr add", mkBin("add", Ptr{"this", 4}, IConst{8}), Ptr{"this", 12}},
		{"ptr sub", mkBin("sub", Ptr{"this", 4}, IConst{8}), Ptr{"this", -4}},
		{"const add ptr", mkBin("add", IConst{16}, Ptr{"stack", 4}), Ptr{"stack", 20}},
		{"shr over or", mkBin("shr", Bin{"or", x, Sym{"y"}}, IConst{3}), Bin{"or", Bin{"shr", x, IConst{3}}, Bin{"shr", Sym{"y"}, IConst{3}}}},
		{"shr over and", mkBin("shr", Bin{"and", x, IConst{0xFF00}}, IConst{8}), Bin{"and", Bin{"shr", x, IConst{8}}, IConst{0xFF}}},
		{"shr of shl", mkBin("shr", Bin{"shl", x, IConst{8}}, IConst{8}), Bin{"and", x, IConst{0x00FFFFFF}}},
		{"and zx8 ff", mkBin("and", Un{"zx8", x}, IConst{0xFF}), Un{"zx8", x}},
		{"and zx16 ffff", mkBin("and", Un{"zx16", x}, IConst{0x1FFFF}), Un{"zx16", x}},
		{"plain", mkBin("add", x, Sym{"y"}), Bin{"add", x, Sym{"y"}}},
	}
	for _, tc := range cases {
		if !reflect.DeepEqual(tc.got, tc.want) {
			t.Errorf("%s = %s, want %s", tc.name, ExprString(tc.got), ExprString(tc.want))
		}
	}
}

// Checks a call result added to becomes an object base, two results added staying integers
func TestCallResultEntersPointerArithmetic(t *testing.T) {
	r1 := CallResult{Fn: "sub_00011000", Seq: 1}
	r2 := CallResult{Fn: "sub_00011010", Seq: 2}
	cases := []struct {
		name string
		got  Expr
		want Expr
	}{
		{"result plus const", mkBin("add", r1, IConst{8}), Ptr{"res1", 8}},
		{"const plus result", mkBin("add", IConst{4}, r2), Ptr{"res2", 4}},
		{"result minus const", mkBin("sub", r1, IConst{4}), Ptr{"res1", -4}},
		{"result plus symbol", mkBin("add", r1, Sym{"i"}), Bin{"add", Ptr{"res1", 0}, Sym{"i"}}},
		{"two results", mkBin("add", r1, r2), Bin{"add", r1, r2}},
		{"result times const", mkBin("mul", r1, IConst{4}), Bin{"mul", r1, IConst{4}}},
	}
	for _, tc := range cases {
		if !reflect.DeepEqual(tc.got, tc.want) {
			t.Errorf("%s = %s, want %s", tc.name, ExprString(tc.got), ExprString(tc.want))
		}
	}
	resultPointers = false
	defer func() { resultPointers = true }()
	if got := mkBin("add", r1, IConst{8}); !reflect.DeepEqual(got, Bin{"add", r1, IConst{8}}) {
		t.Errorf("machine lift result plus const = %s", ExprString(got))
	}
}

func TestMkCondConstantFolding(t *testing.T) {
	cases := []struct {
		op   string
		a, b uint32
		want uint32
	}{
		{"eq", 3, 3, 1}, {"eq", 3, 4, 0},
		{"ne", 3, 4, 1}, {"ne", 3, 3, 0},
		{"lt", 0xFFFFFFFF, 0, 1}, {"lt", 0, 0xFFFFFFFF, 0},
		{"le", 2, 2, 1}, {"le", 3, 2, 0},
		{"gt", 0, 0xFFFFFFFF, 1}, {"gt", 2, 2, 0},
		{"ge", 2, 2, 1}, {"ge", 0xFFFFFFFF, 0, 0},
		{"ult", 0, 0xFFFFFFFF, 1}, {"ult", 0xFFFFFFFF, 0, 0},
		{"ule", 5, 5, 1}, {"ule", 6, 5, 0},
		{"ugt", 0xFFFFFFFF, 0, 1}, {"ugt", 0, 0xFFFFFFFF, 0},
		{"uge", 5, 5, 1}, {"uge", 0, 1, 0},
	}
	for _, tc := range cases {
		got := mkCond(tc.op, IConst{tc.a}, IConst{tc.b})
		if c, ok := got.(IConst); !ok || c.V != tc.want {
			t.Errorf("%s(%#x, %#x) = %s, want %d", tc.op, tc.a, tc.b, ExprString(got), tc.want)
		}
	}
	if got := mkCond("odd", IConst{1}, IConst{2}); !reflect.DeepEqual(got, Cond{"odd", IConst{1}, IConst{2}}) {
		t.Errorf("unknown op folded to %s", ExprString(got))
	}
	if got := mkCond("lt", Ptr{"this", 4}, Ptr{"this", 8}); got != Expr(IConst{1}) {
		t.Errorf("same base offsets = %s, want 1", ExprString(got))
	}
	if got := mkCond("eq", Ptr{"this", 4}, Ptr{"that", 4}); !reflect.DeepEqual(got, Cond{"eq", Ptr{"this", 4}, Ptr{"that", 4}}) {
		t.Errorf("different bases = %s", ExprString(got))
	}
}

// Checks a held object pointer is never null while this, results, empty bases and globals stay tests
func TestNullTestFolding(t *testing.T) {
	folded := []Ptr{{"stack", 8}, {"arg0obj", 4}, {"learn1", 0}, {"mat", 0}}
	for _, p := range folded {
		if got := mkCond("eq", p, IConst{0}); got != Expr(IConst{0}) {
			t.Errorf("%s eq 0 = %s, want 0", ExprString(p), ExprString(got))
		}
		if got := mkCond("ne", p, IConst{0}); got != Expr(IConst{1}) {
			t.Errorf("%s ne 0 = %s, want 1", ExprString(p), ExprString(got))
		}
		if got := mkCond("ult", p, IConst{0}); !reflect.DeepEqual(got, Cond{"ult", p, IConst{0}}) {
			t.Errorf("%s ult 0 = %s", ExprString(p), ExprString(got))
		}
	}
	kept := []Ptr{{"this", 0}, {"", 0x21000}, {"res3", 0}, {"g00021000", 0}}
	for _, p := range kept {
		if got := mkCond("eq", p, IConst{0}); !reflect.DeepEqual(got, Cond{"eq", p, IConst{0}}) {
			t.Errorf("%s eq 0 = %s, want a test", ExprString(p), ExprString(got))
		}
	}
	if got := mkCond("eq", Ptr{"stack", 8}, IConst{4}); !reflect.DeepEqual(got, Cond{"eq", Ptr{"stack", 8}, IConst{4}}) {
		t.Errorf("non null compare = %s", ExprString(got))
	}
}

func TestMkUnFolding(t *testing.T) {
	x := Sym{"x"}
	intCases := []struct {
		op   string
		in   uint32
		want uint32
	}{
		{"zx8", 0x1234, 0x34},
		{"zx16", 0x12345678, 0x5678},
		{"sx8", 0x80, 0xFFFFFF80},
		{"sx16", 0x8000, 0xFFFF8000},
		{"neg", 1, 0xFFFFFFFF},
		{"not", 0, 0xFFFFFFFF},
		{"bsf", 0x50, 4},
		{"bsr", 0x50, 6},
		{"bsf", 0, 0},
		{"bsr", 0, 31},
	}
	for _, tc := range intCases {
		if got := mkUn(tc.op, IConst{tc.in}); got != Expr(IConst{tc.want}) {
			t.Errorf("%s(%#x) = %s, want %#x", tc.op, tc.in, ExprString(got), tc.want)
		}
	}
	if got := mkUn("i2f", IConst{0xFFFFFFFE}); got != Expr(FConst{-2}) {
		t.Errorf("i2f = %s", ExprString(got))
	}
	if got := mkUn("bits2f", IConst{0x40200000}); got != Expr(FConst{2.5}) {
		t.Errorf("bits2f = %s", ExprString(got))
	}
	floatCases := []struct {
		op   string
		in   float64
		want float64
	}{
		{"fneg", 1.5, -1.5},
		{"fabs", -2, 2},
		{"fsqrt", 9, 3},
		{"ftrunc", -2.7, -2},
		{"fround", 2.5, 2},
		{"fround", 3.5, 4},
		{"ffloor", -0.5, -1},
		{"fceil", 0.5, 1},
		{"fexp", 0, 1},
		{"flog", 1, 0},
		{"flog10", 1000, 3},
		{"flog2", 8, 3},
		{"fsin", 0, 0},
		{"fcos", 0, 1},
		{"ftan", 0, 0},
		{"fatan", 0, 0},
		{"fasin", 0, 0},
		{"facos", 1, 0},
		{"fsinh", 0, 0},
		{"fcosh", 0, 1},
		{"ftanh", 0, 0},
		{"f32", 0.1, float64(float32(0.1))},
	}
	for _, tc := range floatCases {
		if got := mkUn(tc.op, FConst{tc.in}); got != Expr(FConst{tc.want}) {
			t.Errorf("%s(%g) = %s, want %g", tc.op, tc.in, ExprString(got), tc.want)
		}
	}
	if got := mkUn("f2i", FConst{-1.5}); got != Expr(IConst{0xFFFFFFFF}) {
		t.Errorf("f2i(-1.5) = %s", ExprString(got))
	}
	if got := mkUn("f2i_rn", FConst{2.5}); got != Expr(IConst{2}) {
		t.Errorf("f2i_rn(2.5) = %s", ExprString(got))
	}
	if got := mkUn("f2i_rn", FConst{-2.5}); got != Expr(IConst{0xFFFFFFFE}) {
		t.Errorf("f2i_rn(-2.5) = %s", ExprString(got))
	}
	if got := mkUn("f2bits", FConst{2.5}); got != Expr(IConst{0x40200000}) {
		t.Errorf("f2bits(2.5) = %s", ExprString(got))
	}
	structural := []struct {
		name string
		got  Expr
		want Expr
	}{
		{"zx8 of zx8", mkUn("zx8", Un{"zx8", x}), Un{"zx8", x}},
		{"zx16 of zx8", mkUn("zx16", Un{"zx8", x}), Un{"zx8", x}},
		{"zx16 of zx16", mkUn("zx16", Un{"zx16", x}), Un{"zx16", x}},
		{"bits2f of f2bits", mkUn("bits2f", Un{"f2bits", x}), x},
		{"f2bits of bits2f", mkUn("f2bits", Un{"bits2f", x}), x},
		{"zx8 over or", mkUn("zx8", Bin{"or", x, Sym{"y"}}), Bin{"or", Un{"zx8", x}, Un{"zx8", Sym{"y"}}}},
		{"zx8 of and high", mkUn("zx8", Bin{"and", x, IConst{0x100}}), IConst{0}},
		{"zx8 of and mixed", mkUn("zx8", Bin{"and", x, IConst{0x1F0}}), Bin{"and", Un{"zx8", x}, IConst{0xF0}}},
		{"zx8 of and syms", mkUn("zx8", Bin{"and", x, Sym{"y"}}), Bin{"and", Un{"zx8", x}, Un{"zx8", Sym{"y"}}}},
		{"zx8 of shl 8", mkUn("zx8", Bin{"shl", x, IConst{8}}), IConst{0}},
		{"zx16 of shl 8", mkUn("zx16", Bin{"shl", x, IConst{8}}), Un{"zx16", Bin{"shl", x, IConst{8}}}},
		{"zx16 of shl 16", mkUn("zx16", Bin{"shl", x, IConst{16}}), IConst{0}},
		{"plain", mkUn("neg", x), Un{"neg", x}},
	}
	for _, tc := range structural {
		if !reflect.DeepEqual(tc.got, tc.want) {
			t.Errorf("%s = %s, want %s", tc.name, ExprString(tc.got), ExprString(tc.want))
		}
	}
}

func TestMkSelAndMkNot(t *testing.T) {
	a, b := Sym{"a"}, Sym{"b"}
	c := Cond{"ult", a, b}
	if got := mkSel(IConst{1}, a, b); got != Expr(a) {
		t.Errorf("sel 1 = %s", ExprString(got))
	}
	if got := mkSel(IConst{0}, a, b); got != Expr(b) {
		t.Errorf("sel 0 = %s", ExprString(got))
	}
	if got := mkSel(c, a, a); got != Expr(a) {
		t.Errorf("sel same = %s", ExprString(got))
	}
	if got := mkSel(c, a, b); !reflect.DeepEqual(got, Sel{c, a, b}) {
		t.Errorf("sel = %s", ExprString(got))
	}
	if got := mkNot(IConst{5}); got != Expr(IConst{0}) {
		t.Errorf("not 5 = %s", ExprString(got))
	}
	if got := mkNot(IConst{0}); got != Expr(IConst{1}) {
		t.Errorf("not 0 = %s", ExprString(got))
	}
	if got := mkNot(Not{c}); !reflect.DeepEqual(got, c) {
		t.Errorf("not not = %s", ExprString(got))
	}
	if got := mkNot(FCmpSet{a, b, 1 | 2}); !reflect.DeepEqual(got, FCmpSet{a, b, 4 | 8}) {
		t.Errorf("not fcmp = %s", ExprString(got))
	}
	inverse := map[string]string{"eq": "ne", "ne": "eq", "lt": "ge", "ge": "lt", "le": "gt", "gt": "le", "ult": "uge", "uge": "ult", "ule": "ugt", "ugt": "ule"}
	for op, inv := range inverse {
		if got := mkNot(Cond{op, a, b}); !reflect.DeepEqual(got, Cond{inv, a, b}) {
			t.Errorf("not %s = %s, want %s", op, ExprString(got), inv)
		}
	}
	if got := mkNot(Bin{"and", a, b}); !reflect.DeepEqual(got, Not{Bin{"and", a, b}}) {
		t.Errorf("not of and = %s", ExprString(got))
	}
}

func TestMkFCmp(t *testing.T) {
	cases := []struct {
		a, b float64
		set  uint8
		want uint32
	}{
		{1, 2, 1, 1},
		{1, 2, 2 | 4, 0},
		{2, 2, 2, 1},
		{3, 2, 4, 1},
		{3, 2, 1 | 2, 0},
		{math.NaN(), 2, 8, 1},
		{math.NaN(), 2, 1 | 2 | 4, 0},
	}
	for _, tc := range cases {
		if got := mkFCmp(FConst{tc.a}, FConst{tc.b}, tc.set); got != Expr(IConst{tc.want}) {
			t.Errorf("fcmp(%g, %g) in %d = %s, want %d", tc.a, tc.b, tc.set, ExprString(got), tc.want)
		}
	}
	if got := mkFCmp(Sym{"a"}, FConst{1}, 1); !reflect.DeepEqual(got, FCmpSet{Sym{"a"}, FConst{1}, 1}) {
		t.Errorf("symbolic fcmp = %s", ExprString(got))
	}
}

func TestExprPoolDeduplicates(t *testing.T) {
	pool := NewExprPool()
	if len(pool.Nodes) != 1 || pool.Nodes[0]["k"] != "undef" {
		t.Fatalf("fresh pool holds %v", pool.Nodes)
	}
	if got := pool.Add(nil); got != 0 {
		t.Errorf("nil interned at %d, want 0", got)
	}
	five := pool.Add(IConst{5})
	if five != 1 || pool.Nodes[five]["k"] != "i" || pool.Nodes[five]["v"] != uint32(5) {
		t.Fatalf("IConst interned as %d %v", five, pool.Nodes[five])
	}
	if again := pool.Add(IConst{5}); again != five {
		t.Errorf("equal constant interned twice at %d and %d", five, again)
	}
	shared := Expr(Bin{"mul", Sym{"x"}, IConst{3}})
	sum := pool.Add(Bin{"add", shared, shared})
	node := pool.Nodes[sum]
	if node["k"] != "bin" || node["op"] != "add" || node["a"] != node["b"] {
		t.Fatalf("shared child interned as %v", node)
	}
	before := len(pool.Nodes)
	if pool.Add(Bin{"add", Bin{"mul", Sym{"x"}, IConst{3}}, Bin{"mul", Sym{"x"}, IConst{3}}}) != sum || len(pool.Nodes) != before {
		t.Errorf("structurally equal tree added new nodes")
	}
	load := pool.Add(Load{Base: "this", Off: 8, Size: 4, Epoch: 2})
	if n := pool.Nodes[load]; n["k"] != "load" || n["base"] != "this" || n["off"] != int32(8) || n["size"] != 4 || n["e"] != 2 {
		t.Errorf("load interned as %v", n)
	}
	plain := pool.Add(Load{Base: "this", Off: 8, Size: 4})
	if _, has := pool.Nodes[plain]["e"]; has || plain == load {
		t.Errorf("epoch zero load interned as %v at %d", pool.Nodes[plain], plain)
	}
	cr := pool.Add(CallResult{Fn: "f", Seq: 2, Args: []Expr{IConst{5}}, This: Sym{"t"}, Target: 0x11000})
	n := pool.Nodes[cr]
	if n["k"] != "call" || n["fn"] != "f" || n["seq"] != 2 || n["target"] != uint32(0x11000) || !reflect.DeepEqual(n["args"], []int{five}) {
		t.Errorf("call interned as %v", n)
	}
	if n := pool.Nodes[pool.Add(FConst{math.Inf(1)})]; n["s"] != "+Inf" {
		t.Errorf("infinity interned as %v", n)
	}
	if n := pool.Nodes[pool.Add(RetAddr{VA: 0x11010})]; n["k"] != "retaddr" || n["va"] != uint32(0x11010) {
		t.Errorf("return address interned as %v", n)
	}
	if n := pool.Nodes[pool.Add(Sel{Sym{"c"}, IConst{1}, IConst{2}})]; n["k"] != "sel" || n["c"] != pool.Add(Sym{"c"}) {
		t.Errorf("sel interned as %v", n)
	}
}

func chain(n int) Expr {
	var e Expr = Sym{"leaf"}
	for i := 0; i < n; i++ {
		e = Bin{"add", e, IConst{uint32(i)}}
	}
	return e
}

// Checks the structural compare gives up past its node budget while the memoized compare does not
func TestSameExprBudget(t *testing.T) {
	small := chain(100)
	if !sameExpr(small, chain(100)) {
		t.Errorf("equal small trees compare unequal")
	}
	if sameExpr(chain(100), chain(101)) {
		t.Errorf("different trees compare equal")
	}
	big := chain(sameExprBudget + 10)
	if sameExpr(big, chain(sameExprBudget+10)) {
		t.Errorf("trees past the budget compare equal")
	}
	if !sameExpr(big, big) {
		t.Errorf("one shared tree compares unequal")
	}
	if !equalExprs(big, chain(sameExprBudget+10)) {
		t.Errorf("equal big trees compare unequal")
	}
	if equalExprs(big, chain(sameExprBudget+11)) {
		t.Errorf("different big trees compare equal")
	}
}

func TestEqualExprsByNode(t *testing.T) {
	a, b := Sym{"a"}, Sym{"b"}
	equal := [][2]Expr{
		{nil, nil},
		{IConst{1}, IConst{1}},
		{FConst{1}, FConst{1}},
		{Ptr{"this", 4}, Ptr{"this", 4}},
		{RetAddr{VA: 1, Clean: 4}, RetAddr{VA: 1, Clean: 4}},
		{Load{Base: "x", Off: 4, Size: 2}, Load{Base: "x", Off: 4, Size: 2}},
		{Load{Addr: Bin{"add", a, IConst{4}}, Size: 4}, Load{Addr: Bin{"add", a, IConst{4}}, Size: 4}},
		{Bin{"add", a, b}, Bin{"add", a, b}},
		{Un{"neg", a}, Un{"neg", a}},
		{Cond{"eq", a, b}, Cond{"eq", a, b}},
		{FCmpSet{a, b, 3}, FCmpSet{a, b, 3}},
		{Not{a}, Not{a}},
		{Elem{a, 2}, Elem{a, 2}},
		{Sel{a, b, a}, Sel{a, b, a}},
		{Block{[]Expr{a, b}}, Block{[]Expr{a, b}}},
		{CallResult{Fn: "f", Seq: 1, Args: []Expr{a}}, CallResult{Fn: "f", Seq: 1, Args: []Expr{b}}},
	}
	for _, pair := range equal {
		if !equalExprs(pair[0], pair[1]) || !sameExpr(pair[0], pair[1]) {
			t.Errorf("%s and %s compare unequal", ExprString(pair[0]), ExprString(pair[1]))
		}
	}
	different := [][2]Expr{
		{nil, IConst{0}},
		{IConst{1}, IConst{2}},
		{IConst{1}, FConst{1}},
		{Ptr{"this", 4}, Ptr{"this", 8}},
		{Load{Base: "x", Off: 4, Size: 2}, Load{Base: "x", Off: 4, Size: 4}},
		{Load{Addr: a, Size: 4}, Load{Addr: b, Size: 4}},
		{Bin{"add", a, b}, Bin{"sub", a, b}},
		{Bin{"add", a, b}, Bin{"add", b, a}},
		{Un{"neg", a}, Un{"not", a}},
		{Cond{"eq", a, b}, Cond{"ne", a, b}},
		{FCmpSet{a, b, 3}, FCmpSet{a, b, 5}},
		{Not{a}, Not{b}},
		{Elem{a, 2}, Elem{a, 3}},
		{Sel{a, b, a}, Sel{a, a, b}},
		{Block{[]Expr{a, b}}, Block{[]Expr{a}}},
		{CallResult{Fn: "f", Seq: 1}, CallResult{Fn: "f", Seq: 2}},
		{CallResult{Fn: "f", Seq: 1}, CallResult{Fn: "g", Seq: 1}},
	}
	for _, pair := range different {
		if equalExprs(pair[0], pair[1]) || sameExpr(pair[0], pair[1]) {
			t.Errorf("%s and %s compare equal", ExprString(pair[0]), ExprString(pair[1]))
		}
	}
}

func TestExprDigestMatchesEquality(t *testing.T) {
	a := Bin{"add", Load{Base: "this", Off: 4, Size: 4}, Sel{Cond{"eq", Sym{"x"}, IConst{1}}, FConst{1}, Elem{Block{[]Expr{Sym{"y"}}}, 0}}}
	b := Bin{"add", Load{Base: "this", Off: 4, Size: 4}, Sel{Cond{"eq", Sym{"x"}, IConst{1}}, FConst{1}, Elem{Block{[]Expr{Sym{"y"}}}, 0}}}
	if exprDigest(a, digestMemo{}) != exprDigest(b, digestMemo{}) {
		t.Errorf("equal structures digest apart")
	}
	c := Bin{"add", Load{Base: "this", Off: 8, Size: 4}, Sel{Cond{"eq", Sym{"x"}, IConst{1}}, FConst{1}, Elem{Block{[]Expr{Sym{"y"}}}, 0}}}
	if exprDigest(a, digestMemo{}) == exprDigest(c, digestMemo{}) {
		t.Errorf("different offsets digest alike")
	}
	if exprDigest(CallResult{Fn: "f", Seq: 1, Args: []Expr{Sym{"p"}}}, digestMemo{}) != exprDigest(CallResult{Fn: "f", Seq: 1}, digestMemo{}) {
		t.Errorf("call results with the same sequence digest apart")
	}
	if exprDigest(nil, digestMemo{}) != 0 {
		t.Errorf("nil digests nonzero")
	}
	memo := digestMemo{}
	exprDigest(a, memo)
	if len(memo) != 10 {
		t.Errorf("memo holds %d nodes, want 10", len(memo))
	}
}

// Checks a shared graph counts as the tree it expands to against the path node budget
func TestPathTooWideCountsTreeNodes(t *testing.T) {
	var e Expr = Sym{"x"}
	for i := 0; i < 22; i++ {
		e = Bin{"add", e, e}
	}
	if !pathTooWide(Path{Return: e}) {
		t.Errorf("a tree of 2^23 nodes fits the budget")
	}
	var small Expr = Sym{"x"}
	for i := 0; i < 17; i++ {
		small = Bin{"add", small, small}
	}
	if pathTooWide(Path{Return: small, Guards: []Expr{small}, Effects: []Effect{{Kind: "store", Value: small, Addr: small}}}) {
		t.Errorf("four trees of 2^18 nodes exceed the budget")
	}
	budget := 100
	countNodes(Sel{Sym{"c"}, Block{[]Expr{Sym{"a"}, Elem{Sym{"b"}, 1}}}, CallResult{This: Sym{"t"}, Args: []Expr{Not{Cond{"eq", Sym{"p"}, FCmpSet{Sym{"q"}, Un{"neg", Load{Addr: Sym{"r"}}}, 1}}}}}}, &budget)
	if budget != 100-16 {
		t.Errorf("counted %d nodes, want 16", 100-budget)
	}
}

func TestEachChildAndWalk(t *testing.T) {
	shared := Expr(Sym{"s"})
	e := Sel{shared, Bin{"add", shared, Un{"neg", shared}}, Load{Addr: shared, Size: 4}}
	var visited []string
	walkExpr(e, map[exprKey]bool{}, func(x Expr) { visited = append(visited, ExprString(x)) })
	sort.Strings(visited)
	want := []string{"(s ? (s add neg(s)) : mem4[s])", "(s add neg(s))", "mem4[s]", "neg(s)", "s"}
	sort.Strings(want)
	if !reflect.DeepEqual(visited, want) {
		t.Errorf("walk visited %v, want %v", visited, want)
	}
	if hasChildren(Sym{"x"}) || hasChildren(IConst{1}) || hasChildren(Load{Base: "x"}) || !hasChildren(Load{Addr: Sym{"x"}}) || !hasChildren(Not{Sym{"x"}}) {
		t.Errorf("child detection wrong")
	}
}
