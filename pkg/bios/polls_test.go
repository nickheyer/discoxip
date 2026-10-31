package bios

import (
	"testing"

	"github.com/nickheyer/discoxip/pkg/xbe"
)

// The poll tests read off the loop shapes the kernels spin in, each answered by a reading that exits the loop
func TestPollTests(t *testing.T) {
	load := func(at uint32, size int) xbe.Expr { return xbe.Load{Off: int32(at), Size: size} }
	cases := []struct {
		name          string
		cond          xbe.Expr
		register      uint32
		runsWhileTrue bool
		want          Poll
		answer        uint32
	}{
		{"queue room", xbe.Cond{Op: "ult", A: load(0xFE820010, 4), B: xbe.IConst{V: 4}}, 0xFE820010, true, Poll{Address: 0xFE820010, Size: 4, And: 0xFFFFFFFF, Op: "not ult", Value: 4}, 4},
		{"semaphore clear", xbe.Cond{Op: "ne", A: xbe.Un{Op: "sx8", A: xbe.Bin{Op: "and", A: load(0xFEC00134, 1), B: xbe.IConst{V: 1}}}, B: xbe.IConst{V: 0}}, 0xFEC00134, true, Poll{Address: 0xFEC00134, Size: 1, And: 1, Op: "not ne", Value: 0}, 0},
		{"ready bit", xbe.Cond{Op: "eq", A: xbe.Bin{Op: "and", A: load(0xFEC00130, 4), B: xbe.IConst{V: 256}}, B: xbe.IConst{V: 0}}, 0xFEC00130, true, Poll{Address: 0xFEC00130, Size: 4, And: 256, Op: "not eq", Value: 0}, 256},
		{"blank bit", xbe.Not{A: xbe.Cond{Op: "eq", A: xbe.Un{Op: "sx8", A: xbe.Bin{Op: "and", A: load(0xFD600100, 1), B: xbe.IConst{V: 1}}}, B: xbe.IConst{V: 0}}}, 0xFD600100, false, Poll{Address: 0xFD600100, Size: 1, And: 1, Op: "not eq", Value: 0}, 1},
		{"shifted free space against a count nothing fixes", xbe.Cond{Op: "ult", A: xbe.Bin{Op: "shr", A: load(0xFE820010, 4), B: xbe.IConst{V: 2}}, B: xbe.Bin{Op: "add", A: xbe.Load{Addr: xbe.Bin{Op: "add", A: xbe.Sym{Name: "reg.ECX"}, B: xbe.IConst{V: 100}}, Size: 1}, B: xbe.Load{Addr: xbe.Bin{Op: "add", A: xbe.Sym{Name: "reg.ECX"}, B: xbe.IConst{V: 100}}, Size: 1}}}, 0xFE820010, true, Poll{Address: 0xFE820010, Size: 4, And: 0xFFFFFFFC, Op: "not ult", Value: 2040}, 2040},
		{"shifted field compared", xbe.Cond{Op: "eq", A: xbe.Bin{Op: "and", A: xbe.Bin{Op: "shr", A: load(0xFD600100, 4), B: xbe.IConst{V: 8}}, B: xbe.IConst{V: 0xFF}}, B: xbe.IConst{V: 3}}, 0xFD600100, true, Poll{Address: 0xFD600100, Size: 4, And: 0xFF00, Op: "not eq", Value: 0x300}, 0},
		{"reset bit kept in a register", xbe.Cond{Op: "ne", A: xbe.Un{Op: "sx8", A: xbe.Un{Op: "zx8", A: xbe.Bin{Op: "or", A: xbe.Bin{Op: "and", A: xbe.Sym{Name: "reg.ECX"}, B: xbe.IConst{V: 0xFFFFFF00}}, B: xbe.Bin{Op: "and", A: xbe.Bin{Op: "or", A: xbe.IConst{V: 0}, B: xbe.Un{Op: "zx8", A: load(0xFEC0011B, 1)}}, B: xbe.IConst{V: 2}}}}}, B: xbe.IConst{V: 0}}, 0xFEC0011B, true, Poll{Address: 0xFEC0011B, Size: 1, And: 2, Op: "not ne", Value: 0}, 0},
	}
	for _, c := range cases {
		ld, got, ok := pollTest(c.cond, c.runsWhileTrue)
		got.Address = uint32(ld.Off)
		if !ok || got != c.want {
			t.Errorf("%s: got %+v %v, want %+v", c.name, got, ok, c.want)
			continue
		}
		answers, err := pollAnswers([]Poll{got})
		if err != nil || len(answers) != 1 || answers[0].Value != c.answer || answers[0].Size != c.want.Size {
			t.Errorf("%s: answers %+v %v, want %#x", c.name, answers, err, c.answer)
		}
	}
	for _, cond := range []xbe.Expr{
		xbe.Cond{Op: "ult", A: load(0xFE820010, 4), B: xbe.Sym{Name: "lost.reg.EDX"}},
		xbe.Cond{Op: "ult", A: xbe.Un{Op: "sx8", A: load(0xFE820010, 1)}, B: xbe.IConst{V: 4}},
		xbe.Cond{Op: "eq", A: xbe.Bin{Op: "or", A: load(0xFE820010, 4), B: xbe.IConst{V: 1}}, B: xbe.IConst{V: 1}},
		xbe.Cond{Op: "eq", A: load(0xFE820010, 4), B: xbe.Load{Addr: xbe.Sym{Name: "reg.ECX"}, Size: 4}},
		xbe.Cond{Op: "ugt", A: xbe.Bin{Op: "shr", A: load(0xFE820010, 4), B: xbe.IConst{V: 2}}, B: xbe.Load{Addr: xbe.Sym{Name: "reg.ECX"}, Size: 1}},
		xbe.Cond{Op: "lt", A: xbe.Bin{Op: "shr", A: load(0xFE820010, 4), B: xbe.IConst{V: 2}}, B: xbe.IConst{V: 4}},
	} {
		if _, p, ok := pollTest(cond, true); ok {
			t.Errorf("%s fixed to %+v", xbe.ExprString(cond), p)
		}
	}
}

// Two polls of one register combine into a reading passing both, and a contradiction is reported
func TestPollAnswersCombine(t *testing.T) {
	polls := []Poll{
		{Address: 0xFEC00130, Size: 4, And: 256, Op: "not eq", Value: 0},
		{Address: 0xFEC00130, Size: 4, And: 512, Op: "not eq", Value: 0},
		{Address: 0xFE820010, Size: 4, And: 0xFFFFFFFF, Op: "not ult", Value: 4},
		{Block: 0x80041DB0, Size: 4, And: 0x10000, Op: "not ne", Value: 0},
	}
	answers, err := pollAnswers(polls)
	if err != nil || len(answers) != 3 || answers[0].Block != 0x80041DB0 || answers[0].Value != 0 || answers[1].Address != 0xFE820010 || answers[1].Value != 4 || answers[2].Value != 768 {
		t.Fatalf("answers %+v %v", answers, err)
	}
	if _, err := pollAnswers([]Poll{{Address: 1, Size: 4, And: 1, Op: "eq", Value: 1}, {Address: 1, Size: 4, And: 1, Op: "eq", Value: 0}}); err == nil {
		t.Error("contradicting polls answered")
	}
}
