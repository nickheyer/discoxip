package xbe

import "testing"

// Checks an address splits into its constant and the inputs it scales, a multiplied sum scaling every term
func TestShapeOf(t *testing.T) {
	s := shapeOf(Bin{"add", IConst{0x3B4F0}, Bin{"mul", Bin{"add", Bin{"shl", Sym{"in0"}, IConst{5}}, Sym{"in1"}}, IConst{4}}})
	if s.off != 0x3B4F0 || len(s.others) != 0 || s.scaled["stack:0"] != 128 || s.scaled["stack:1"] != 4 {
		t.Errorf("stage address %+v", s)
	}
	s = shapeOf(Bin{"add", Bin{"shl", Sym{"reg.ECX"}, IConst{4}}, IConst{0x394F8}})
	if s.off != 0x394F8 || !s.isTable("reg.ECX", 16) {
		t.Errorf("register address %+v", s)
	}
	s = shapeOf(Bin{"add", Load{Off: 0x388A0, Size: 4}, IConst{8}})
	if s.off != 8 || len(s.scaled) != 0 || len(s.others) != 1 || s.isTable("stack:0", 4) {
		t.Errorf("pointer address %+v", s)
	}
	s = shapeOf(Bin{"mul", Bin{"add", Load{Off: 0x388A0, Size: 4}, Sym{"in0"}}, IConst{4}})
	if len(s.others) != 1 || len(s.scaled) != 0 {
		t.Errorf("scaled pointer %+v", s)
	}
}

// Checks inputs are named by their stack word or register, with a constant added seen through
func TestInputRoles(t *testing.T) {
	for e, want := range map[Expr]string{Sym{"in3"}: "stack:3", Sym{"reg.EDX"}: "reg.EDX"} {
		if role, ok := inputRole(e); !ok || role != want {
			t.Errorf("%s names %q %v", ExprString(e), role, ok)
		}
	}
	for _, e := range []Expr{IConst{3}, Sym{"g00012345"}, Sym{"input"}, Load{Off: 4, Size: 4}} {
		if role, ok := inputRole(e); ok {
			t.Errorf("%s names %q", ExprString(e), role)
		}
	}
	if role, bias, ok := biasedInput(Bin{"add", Sym{"in0"}, IConst{96}}); !ok || role != "stack:0" || bias != 96 {
		t.Errorf("biased input %q %d %v", role, bias, ok)
	}
	if role, bias, ok := biasedInput(Sym{"reg.ECX"}); !ok || role != "reg.ECX" || bias != 0 {
		t.Errorf("plain input %q %d %v", role, bias, ok)
	}
	if _, _, ok := biasedInput(Bin{"add", Load{Off: 8, Size: 4}, IConst{96}}); ok {
		t.Errorf("a biased load is an input")
	}
}

// Checks the furthest load through an input sizes what a path reads from it
func TestInputExtent(t *testing.T) {
	put := Load{Off: 0x388A0, Size: 4}
	p := Path{Effects: []Effect{
		{Kind: "store", Addr: Bin{"add", put, IConst{4}}, Size: 4, Value: Load{Addr: Sym{"in0"}, Size: 4}},
		{Kind: "store", Addr: Bin{"add", put, IConst{8}}, Size: 4, Value: Load{Addr: Bin{"add", Bin{"add", Sym{"in0"}, IConst{4}}, IConst{4}}, Size: 8}},
		{Kind: "store", Addr: put, Size: 4, Value: Load{Addr: Bin{"add", Sym{"in1"}, IConst{60}}, Size: 4}},
		{Kind: "store", Addr: put, Size: 4, Value: Load{Base: "reg.EDX", Off: 12, Size: 4}},
	}}
	for role, want := range map[string]int{"stack:0": 16, "stack:1": 64, "reg.EDX": 16, "reg.ECX": 0} {
		if n := inputExtent(p, role); n != want {
			t.Errorf("%s extent %d, want %d", role, n, want)
		}
	}
}

// Checks a named base store keeps the base as an other term and a table store folds its offset
func TestStoreShape(t *testing.T) {
	s := storeShape(Effect{Kind: "store", Base: "res1", Off: 0xC, Size: 4})
	if s.off != 0xC || len(s.others) != 1 || s.others[0] != "res1" || len(s.scaled) != 0 {
		t.Errorf("named base %+v", s)
	}
	s = storeShape(Effect{Kind: "store", Base: "?", Addr: Bin{"add", IConst{0x3B6F0}, Bin{"mul", Sym{"in0"}, IConst{4}}}, Size: 4})
	if !s.isTable("stack:0", 4) || s.off != 0x3B6F0 {
		t.Errorf("table store %+v", s)
	}
	s = storeShape(Effect{Kind: "store", Base: "reg.EDX", Off: 8, Size: 4})
	if !s.isTable("reg.EDX", 1) || s.off != 8 {
		t.Errorf("register base %+v", s)
	}
}
