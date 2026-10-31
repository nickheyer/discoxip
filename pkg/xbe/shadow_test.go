package xbe

import "testing"

// A word whose low byte comes from one open input and whose upper bytes from another, as a routine building a bus
// request leaves it in a register
func splitWord() (Expr, Expr, Expr) {
	lo := sizedLeaf(0, 4)
	hi := sizedLeaf(1, 4)
	word := Bin{Op: "or", A: Bin{Op: "and", A: hi, B: IConst{V: 0xFFFFFF00}}, B: Un{Op: "zx8", A: lo}}
	return word, lo, hi
}

func TestPruneBitsDropsOperandsOutsideTheMask(t *testing.T) {
	word, lo, _ := splitWord()
	p := pruneBits(word, 0xFF)
	leaves := openLeaves(p)
	if len(leaves) != 1 || leaves[0] != 0 {
		t.Fatalf("low byte of %s reads inputs %v, want only %s", ExprString(word), leaves, ExprString(lo))
	}
	for _, seeds := range []map[int]uint32{{0: 0x12345678, 1: 0xABCDEF01}, {0: 0, 1: 0xFFFFFFFF}, {0: 0xFF, 1: 0}} {
		want, _ := evalShadow(word, seeds, nil)
		got, err := evalShadow(p, seeds, nil)
		if err != nil || got != want&0xFF {
			t.Fatalf("pruned %s reads %X under %v, want %X (%v)", ExprString(p), got, seeds, want&0xFF, err)
		}
	}
	if sh := maskedShadow(word, 0xFF); len(openLeaves(sh)) != 1 {
		t.Fatalf("masked shadow %s keeps the high input", ExprString(sh))
	}
}

func TestPruneBitsKeepsShiftedAndWidenedInputs(t *testing.T) {
	word, _, hi := splitWord()
	if leaves := openLeaves(pruneBits(word, 0xFF00)); len(leaves) != 1 || leaves[0] != 1 {
		t.Fatalf("second byte reads %v, want only %s", leaves, ExprString(hi))
	}
	a := sizedLeaf(2, 1)
	shifted := Bin{Op: "shl", A: a, B: IConst{V: 8}}
	if sh := maskedShadow(shifted, 0xFF); sh != nil {
		t.Fatalf("low byte of %s is %s, want fixed", ExprString(shifted), ExprString(sh))
	}
	if sh := maskedShadow(shifted, 0xFF00); sh == nil || len(openLeaves(sh)) != 1 {
		t.Fatalf("second byte of %s lost its input", ExprString(shifted))
	}
	sx := Un{Op: "sx8", A: a}
	if sh := maskedShadow(sx, 0xFF000000); sh == nil {
		t.Fatal("the sign byte of a widened input is fixed")
	}
	if c := foldShadow(Bin{Op: "or", A: IConst{V: 0x100}, B: IConst{V: 1}}); c != (IConst{V: 0x101}) {
		t.Fatalf("fold gives %s", ExprString(c))
	}
}
