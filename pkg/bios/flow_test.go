package bios

import (
	"testing"
	"time"

	"github.com/nickheyer/discoxip/pkg/xbe"
)

// A loop rebuilding a register's large value every pass settles once the rebuilt value matches the one kept
func TestRoutineFlowSettlesLargeValues(t *testing.T) {
	var big xbe.Expr = xbe.Sym{Name: "reg.EBX"}
	for range 4096 {
		big = xbe.Bin{Op: "add", A: big, B: xbe.Sym{Name: "reg.EBX"}}
	}
	const entry, loop, exit = 0x1000, 0x1010, 0x1020
	dv := &deriver{flows: map[uint32]*routineFlow{}, blocks: map[uint32]xbe.BlockInfo{
		entry: {VA: entry, Kind: "jump", Target: loop, Regs: map[string]xbe.Expr{"reg.EAX": big}, ESP: xbe.Sym{Name: "reg.ESP"}},
		loop:  {VA: loop, Kind: "branch", Taken: loop, Fall: exit, Cond: xbe.Cond{Op: "ne", A: xbe.Sym{Name: "reg.ECX"}, B: xbe.IConst{V: 0}}, Regs: map[string]xbe.Expr{"reg.EAX": big}, ESP: xbe.Sym{Name: "reg.ESP"}},
	}}
	done := make(chan *routineFlow, 1)
	go func() { done <- dv.routineFlow(entry) }()
	select {
	case flow := <-done:
		if v := flow.states[loop].regs["reg.EAX"]; !xbe.EqualExprs(v, big) {
			t.Fatalf("loop entered with EAX %s", xbe.ExprString(v))
		}
		if v := flow.states[exit].regs["reg.EAX"]; !xbe.EqualExprs(v, big) {
			t.Fatalf("loop left with EAX %s", xbe.ExprString(v))
		}
	case <-time.After(30 * time.Second):
		t.Fatal("the flow of a loop rebuilding a large value did not settle")
	}
}

// A loop rotating and adding a value shares each pass's value many times over and still settles
func TestRoutineFlowJoinsSharedValues(t *testing.T) {
	round := func(x xbe.Expr) xbe.Expr {
		return xbe.Bin{Op: "add", A: xbe.Bin{Op: "or", A: xbe.Bin{Op: "shl", A: x, B: xbe.IConst{V: 7}}, B: xbe.Bin{Op: "shr", A: x, B: xbe.IConst{V: 25}}}, B: x}
	}
	var seeded xbe.Expr = xbe.Sym{Name: "reg.EBX"}
	for range 40 {
		seeded = round(seeded)
	}
	const entry, loop, exit = 0x2000, 0x2010, 0x2020
	dv := &deriver{flows: map[uint32]*routineFlow{}, blocks: map[uint32]xbe.BlockInfo{
		entry: {VA: entry, Kind: "jump", Target: loop, Regs: map[string]xbe.Expr{"reg.EAX": seeded}, ESP: xbe.Sym{Name: "reg.ESP"}},
		loop:  {VA: loop, Kind: "branch", Taken: loop, Fall: exit, Cond: xbe.Cond{Op: "ne", A: xbe.Sym{Name: "reg.ECX"}, B: xbe.IConst{V: 0}}, Regs: map[string]xbe.Expr{"reg.EAX": round(xbe.Sym{Name: "reg.EAX"})}, ESP: xbe.Sym{Name: "reg.ESP"}},
	}}
	done := make(chan *routineFlow, 1)
	go func() { done <- dv.routineFlow(entry) }()
	select {
	case flow := <-done:
		v := flow.states[loop].regs["reg.EAX"]
		if !mentionsLost(v) {
			t.Fatalf("loop entered with EAX %s, a value the loop itself changes", xbe.ExprString(v))
		}
		nodes := 0
		xbe.WalkExpr(v, func(xbe.Expr) { nodes++ })
		if nodes > 400 {
			t.Fatalf("loop entered with EAX of %d nodes", nodes)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("the flow of a loop sharing a rotated value did not settle")
	}
}
