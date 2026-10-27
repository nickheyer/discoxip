package xbe

import (
	"testing"

	"golang.org/x/arch/x86/x86asm"
)

func sseText() []chunk {
	return []chunk{
		lbl("roundFn"), abs32([]byte{0xF3, 0x0F, 0x10, 0x05}, "cHalf", 0), by(0xF3, 0x0F, 0x2D, 0xC0), by(0xF3, 0x0F, 0x2C, 0xC8),
		abs32([]byte{0xF3, 0x0F, 0x10, 0x0D}, "cThreeHalf", 0), by(0xF3, 0x0F, 0x2D, 0xD1),
		abs32([]byte{0xF3, 0x0F, 0x10, 0x15}, "cNeg", 0), by(0xF3, 0x0F, 0x2D, 0xDA),
		abs32([]byte{0x89, 0x0D}, "sout", 0), abs32([]byte{0x89, 0x15}, "sout", 4), abs32([]byte{0x89, 0x1D}, "sout", 8), ret(), pad(),
		lbl("packedFn"), abs32([]byte{0x0F, 0x28, 0x05}, "vecA", 0), abs32([]byte{0x0F, 0x28, 0x0D}, "vecB", 0), by(0x0F, 0x58, 0xC1), by(0x0F, 0x59, 0xC1), abs32([]byte{0x0F, 0x29, 0x05}, "sout", 0), ret(), pad(),
		lbl("shufFn"), abs32([]byte{0x0F, 0x28, 0x05}, "vecA", 0), abs32([]byte{0x0F, 0x28, 0x0D}, "vecB", 0), by(0x0F, 0xC6, 0xC1, 0x1B), abs32([]byte{0x0F, 0x29, 0x05}, "sout", 0), ret(), pad(),
		lbl("symFn"), by(0xF3, 0x0F, 0x58, 0xC1), by(0xF3, 0x0F, 0x5C, 0xC2), abs32([]byte{0xF3, 0x0F, 0x11, 0x05}, "sout", 0), ret(), pad(),
		lbl("logicFn"), abs32([]byte{0x0F, 0x28, 0x05}, "vecA", 0), abs32([]byte{0x0F, 0x57, 0x05}, "signMask", 0), abs32([]byte{0x0F, 0x29, 0x05}, "sout", 0), abs32([]byte{0x0F, 0x54, 0x05}, "absMask", 0), abs32([]byte{0x0F, 0x29, 0x05}, "sout2", 0), by(0x0F, 0x57, 0xC9), abs32([]byte{0x0F, 0x29, 0x0D}, "sout3", 0), ret(), pad(),
		lbl("noopFn"), by(0x0F, 0xAE, 0x54, 0x24, 0x04), abs32([]byte{0x0F, 0xAE, 0x1D}, "sout", 0), by(0x0F, 0x18, 0x08), by(0x0F, 0x18, 0x00), by(0x0F, 0xAE, 0xF8), by(0x0F, 0x77), ret(), pad(),
		lbl("cvtFn"), abs32([]byte{0xA1}, "cInt", 0), by(0xF3, 0x0F, 0x2A, 0xC0), abs32([]byte{0xF3, 0x0F, 0x11, 0x05}, "sout", 0), ret(), pad(),
		lbl("movdFn"), abs32([]byte{0xF3, 0x0F, 0x10, 0x05}, "cHalf", 0), by(0x66, 0x0F, 0x7E, 0xC0), ret(), pad(),
		lbl("mmxFn"), by(0x0F, 0x6E, 0xC0), by(0x0F, 0x6E, 0xC9), by(0x0F, 0x62, 0xC1), abs32([]byte{0x0F, 0x7F, 0x05}, "sout", 0), ret(), pad(),
		lbl("comissFn"), by(0x0F, 0x2F, 0xC1), ja8("comissAbove"), movEAXImm(1), ret(), lbl("comissAbove"), movEAXImm(2), ret(), pad(),
		lbl("unaryFn"), abs32([]byte{0xF3, 0x0F, 0x10, 0x05}, "cNine", 0), by(0xF3, 0x0F, 0x51, 0xC8), by(0xF3, 0x0F, 0x53, 0xD0), by(0xF3, 0x0F, 0x52, 0xD8),
		abs32([]byte{0xF3, 0x0F, 0x11, 0x0D}, "sout", 0), abs32([]byte{0xF3, 0x0F, 0x11, 0x15}, "sout", 4), abs32([]byte{0xF3, 0x0F, 0x11, 0x1D}, "sout", 8),
		by(0xF3, 0x0F, 0x5F, 0xCA), abs32([]byte{0xF3, 0x0F, 0x11, 0x0D}, "sout", 12), by(0xF3, 0x0F, 0x5D, 0xCA), abs32([]byte{0xF3, 0x0F, 0x11, 0x0D}, "sout2", 0), ret(), pad(),
		lbl("unpckFn"), abs32([]byte{0x0F, 0x28, 0x05}, "vecA", 0), abs32([]byte{0x0F, 0x28, 0x0D}, "vecB", 0), by(0x0F, 0x14, 0xC1), abs32([]byte{0x0F, 0x29, 0x05}, "sout", 0),
		abs32([]byte{0x0F, 0x28, 0x05}, "vecA", 0), by(0x0F, 0x15, 0xC1), abs32([]byte{0x0F, 0x29, 0x05}, "sout2", 0),
		abs32([]byte{0x0F, 0x28, 0x05}, "vecA", 0), by(0x0F, 0x16, 0xC1), abs32([]byte{0x0F, 0x29, 0x05}, "sout3", 0), ret(), pad(),
		lbl("movmskFn"), abs32([]byte{0x0F, 0x28, 0x05}, "vecMix", 0), by(0x0F, 0x50, 0xC0), ret(), pad(),
		lbl("cvtpsFn"), abs32([]byte{0x0F, 0x28, 0x0D}, "vecHalf", 0), by(0x0F, 0x2D, 0xC1), abs32([]byte{0x0F, 0x7F, 0x05}, "sout", 0), by(0x0F, 0x2C, 0xC1), abs32([]byte{0x0F, 0x7F, 0x05}, "sout2", 0), ret(), pad(),
	}
}

func sseImage(t *testing.T) *testImage {
	t.Helper()
	return buildImage(t, sseText(), []chunk{
		lbl("cHalf"), f32(2.5), lbl("cThreeHalf"), f32(3.5), lbl("cNeg"), f32(-2.5), lbl("cNine"), f32(9), lbl("cInt"), word(7),
		lbl("vecA"), f32(1), f32(2), f32(3), f32(4),
		lbl("vecB"), f32(10), f32(20), f32(30), f32(40),
		lbl("vecMix"), f32(-1), f32(2), f32(-3), f32(4),
		lbl("vecHalf"), f32(2.5), f32(3.5), f32(0), f32(0),
		lbl("signMask"), word(0x80000000), word(0x80000000), word(0x80000000), word(0x80000000),
		lbl("absMask"), word(0x7FFFFFFF), word(0x7FFFFFFF), word(0x7FFFFFFF), word(0x7FFFFFFF),
	}, []chunk{
		lbl("sout"), rep(16, 0), lbl("sout2"), rep(16, 0), lbl("sout3"), rep(16, 0),
	}, nil)
}

func expectLanes(t *testing.T, p Path, base int32, values ...Expr) {
	t.Helper()
	stores := effectsOf(p, "store")
	for i, v := range values {
		found := false
		for _, e := range stores {
			if e.Base == "" && e.Off == base+int32(4*i) {
				expectStore(t, e, "", base+int32(4*i), 4, v)
				found = true
			}
		}
		if !found {
			t.Errorf("no store to lane %d at %#x among %v", i, base, stores)
		}
	}
}

func TestSSEConversionsRound(t *testing.T) {
	ti := sseImage(t)
	p := onePath(t, runPaths(t, ti, "roundFn", noInit))
	exprEq(t, "nearest of 2.5", p.Return, IConst{2})
	expectLanes(t, p, int32(ti.at(t, "sout")), IConst{2}, IConst{4}, IConst{0xFFFFFFFE})
	p = onePath(t, runPaths(t, ti, "cvtFn", noInit))
	expectLanes(t, p, int32(ti.at(t, "sout")), FConst{7})
	p = onePath(t, runPaths(t, ti, "movdFn", noInit))
	exprEq(t, "float bits", p.Return, IConst{0x40200000})
	p = onePath(t, runPaths(t, ti, "cvtpsFn", noInit))
	expectLanes(t, p, int32(ti.at(t, "sout")), IConst{2}, IConst{4})
	expectLanes(t, p, int32(ti.at(t, "sout2")), IConst{2}, IConst{3})
}

func TestSSEPackedArithmetic(t *testing.T) {
	ti := sseImage(t)
	p := onePath(t, runPaths(t, ti, "packedFn", noInit))
	expectLanes(t, p, int32(ti.at(t, "sout")), FConst{110}, FConst{440}, FConst{990}, FConst{1760})
	p = onePath(t, runPaths(t, ti, "shufFn", noInit))
	expectLanes(t, p, int32(ti.at(t, "sout")), FConst{4}, FConst{3}, FConst{20}, FConst{10})
	p = onePath(t, runPaths(t, ti, "unpckFn", noInit))
	expectLanes(t, p, int32(ti.at(t, "sout")), FConst{1}, FConst{10}, FConst{2}, FConst{20})
	expectLanes(t, p, int32(ti.at(t, "sout2")), FConst{3}, FConst{30}, FConst{4}, FConst{40})
	expectLanes(t, p, int32(ti.at(t, "sout3")), FConst{1}, FConst{2}, FConst{10}, FConst{20})
	p = onePath(t, runPaths(t, ti, "unaryFn", noInit))
	ninth := FConst{float64(float32(1.0 / 9))}
	expectLanes(t, p, int32(ti.at(t, "sout")), FConst{3}, ninth, FConst{float64(float32(1.0 / 3))}, FConst{3})
	expectLanes(t, p, int32(ti.at(t, "sout2")), ninth)
	p = onePath(t, runPaths(t, ti, "movmskFn", noInit))
	exprEq(t, "sign mask", p.Return, IConst{5})
}

func TestSSELogicFoldsSignAndMagnitude(t *testing.T) {
	ti := sseImage(t)
	p := onePath(t, runPaths(t, ti, "logicFn", noInit))
	expectLanes(t, p, int32(ti.at(t, "sout")), FConst{-1}, FConst{-2}, FConst{-3}, FConst{-4})
	expectLanes(t, p, int32(ti.at(t, "sout2")), FConst{1}, FConst{2}, FConst{3}, FConst{4})
	expectLanes(t, p, int32(ti.at(t, "sout3")), FConst{0}, FConst{0}, FConst{0}, FConst{0})
	x := Sym{"x"}
	cases := []struct {
		op   string
		mask uint32
		want Expr
	}{
		{"xor", 0, x},
		{"xor", 0x80000000, Un{"fneg", x}},
		{"and", 0x7FFFFFFF, Un{"fabs", x}},
		{"and", 0, FConst{0}},
		{"and", 0xFFFFFFFF, x},
		{"or", 0, x},
		{"andn", 0, FConst{0}},
		{"or", 0x40000000, Un{"bits2f", Bin{"or", Un{"f2bits", x}, IConst{0x40000000}}}},
		{"andn", 0xFF, Un{"bits2f", Bin{"and", Un{"not", Un{"f2bits", x}}, IConst{0xFF}}}},
	}
	for _, tc := range cases {
		exprEq(t, tc.op, laneLogic(tc.op, x, IConst{tc.mask}), tc.want)
	}
	exprEq(t, "symbolic mask", laneLogic("and", x, Sym{"m"}), Un{"bits2f", Bin{"and", Un{"f2bits", x}, Sym{"m"}}})
}

func TestSSESymbolicLanesAndCompare(t *testing.T) {
	ti := sseImage(t)
	p := onePath(t, runPaths(t, ti, "symFn", noInit))
	expectLanes(t, p, int32(ti.at(t, "sout")), Un{"f32", Bin{"fsub", Bin{"fadd", Sym{"xmm0.0"}, Sym{"xmm1.0"}}, Sym{"xmm2.0"}}})
	p = onePath(t, runPaths(t, ti, "mmxFn", noInit))
	expectLanes(t, p, int32(ti.at(t, "sout")), Sym{"reg.EAX"}, Sym{"reg.ECX"})
	paths := runPaths(t, ti, "comissFn", noInit)
	if len(paths) != 2 {
		t.Fatalf("%d paths from the compare", len(paths))
	}
	exprEq(t, "not above guard", paths[0].Guards[0], FCmpSet{Sym{"xmm0.0"}, Sym{"xmm1.0"}, 1 | 2 | 8})
	exprEq(t, "not above return", paths[0].Return, IConst{1})
	exprEq(t, "above guard", paths[1].Guards[0], FCmpSet{Sym{"xmm0.0"}, Sym{"xmm1.0"}, 4})
	exprEq(t, "above return", paths[1].Return, IConst{2})
	if i, ok := xmmIndex(x86asm.X5); !ok || i != 5 {
		t.Errorf("xmm index %d %v", i, ok)
	}
	if _, ok := mmxIndex(x86asm.X5); ok {
		t.Errorf("a vector register is an mmx register")
	}
	if i, ok := mmxIndex(x86asm.M3); !ok || i != 3 {
		t.Errorf("mmx index %d %v", i, ok)
	}
}

func TestSSENoOps(t *testing.T) {
	ti := sseImage(t)
	p := onePath(t, runPaths(t, ti, "noopFn", noInit))
	if len(p.Effects) != 0 {
		t.Fatalf("no op instructions recorded %v", p.Effects)
	}
	if p.Return != nil {
		t.Errorf("an untouched accumulator returns %s", ExprString(p.Return))
	}
}
