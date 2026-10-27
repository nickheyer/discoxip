package xap

import (
	"reflect"
	"testing"
)

func TestNodeAccessors(t *testing.T) {
	s := mustParse(t, `T {
  s "str"
  i ident
  n 1.5
  v 1 2 3
  b TRUE
  c Inner { k 1 }
  dup 1
  dup 2
}`)
	n := s.Items[0].Node

	for _, c := range []struct{ key, want string }{
		{"s", "str"}, {"i", "ident"}, {"n", ""}, {"b", ""}, {"c", ""}, {"missing", ""},
	} {
		if got := n.String(c.key); got != c.want {
			t.Errorf("String(%q) = %q want %q", c.key, got, c.want)
		}
	}

	for _, c := range []struct {
		key  string
		want float64
		ok   bool
	}{
		{"n", 1.5, true}, {"v", 1, true}, {"s", 0, false}, {"b", 0, false}, {"missing", 0, false},
	} {
		got, ok := n.Float(c.key)
		if got != c.want || ok != c.ok {
			t.Errorf("Float(%q) = %v, %v want %v, %v", c.key, got, ok, c.want, c.ok)
		}
	}

	for _, c := range []struct {
		key  string
		want []float64
	}{
		{"v", []float64{1, 2, 3}}, {"n", []float64{1.5}}, {"s", nil}, {"missing", nil},
	} {
		if got := n.Floats(c.key); !reflect.DeepEqual(got, c.want) {
			t.Errorf("Floats(%q) = %v want %v", c.key, got, c.want)
		}
	}

	wantChild := &Node{TypeName: "Inner", Fields: []Field{field("k", num(1))}}
	if got := n.ChildNode("c"); !reflect.DeepEqual(got, wantChild) {
		t.Errorf("ChildNode(c) = %s want %s", jsonOf(t, got), jsonOf(t, wantChild))
	}
	for _, key := range []string{"s", "v", "missing"} {
		if got := n.ChildNode(key); got != nil {
			t.Errorf("ChildNode(%q) = %s want nil", key, jsonOf(t, got))
		}
	}

	if got := n.GetField("dup"); got != &n.Fields[6] || !reflect.DeepEqual(got.Values, []Value{num(1)}) {
		t.Errorf("GetField(dup) = %+v want pointer to first dup field", got)
	}
	if got := n.GetField("missing"); got != nil {
		t.Errorf("GetField(missing) = %+v want nil", got)
	}
}

func TestNodeAccessorsSkipNonNumbersAndEmptyFields(t *testing.T) {
	n := &Node{Fields: []Field{
		{Key: "mixed", Values: []Value{num(1), str("x"), num(2), boolean(true)}},
		{Key: "empty"},
	}}
	if got := n.Floats("mixed"); !reflect.DeepEqual(got, []float64{1, 2}) {
		t.Errorf("Floats(mixed) = %v want [1 2]", got)
	}
	if got := n.String("empty"); got != "" {
		t.Errorf("String(empty) = %q want empty", got)
	}
	if got, ok := n.Float("empty"); got != 0 || ok {
		t.Errorf("Float(empty) = %v, %v want 0, false", got, ok)
	}
	if got := n.ChildNode("empty"); got != nil {
		t.Errorf("ChildNode(empty) = %+v want nil", got)
	}
	if got := n.Floats("empty"); got != nil {
		t.Errorf("Floats(empty) = %v want nil", got)
	}
}

func TestSceneNodesSkipsScriptsAndNilNodes(t *testing.T) {
	a := &Node{TypeName: "A"}
	b := &Node{TypeName: "B"}
	s := &Scene{Items: []SceneItem{
		{Kind: SNode, Node: a},
		{Kind: SScript, Script: "var x = 1;"},
		{Kind: SNode},
		{Kind: SNode, Node: b},
	}}
	got := s.Nodes()
	if len(got) != 2 || got[0] != a || got[1] != b {
		t.Fatalf("Nodes() = %s want [A B]", jsonOf(t, got))
	}
	if got := (&Scene{}).Nodes(); got != nil {
		t.Fatalf("empty Nodes() = %v want nil", got)
	}
}

func TestNodeCount(t *testing.T) {
	s := mustParse(t, `DEF a T { children [ U { } V ] material M { } list [ DEF x W { } 5 ] }
USE a
function f() { }
Bare`)
	if got := s.NodeCount(); got != 11 {
		t.Fatalf("NodeCount() = %d want 11", got)
	}
	empty := &Scene{Items: []SceneItem{{Kind: SNode}, {Kind: SScript, Script: "var y = 2;"}}}
	if got := empty.NodeCount(); got != 0 {
		t.Fatalf("NodeCount() of nodeless scene = %d want 0", got)
	}
}

func TestMeshRefs(t *testing.T) {
	s := mustParse(t, `Shape { geometry Mesh { url "a.xm" } }
DEF t Transform { children [ Shape { geometry Mesh { url "b.xm" } } Mesh { url c.xm } ] }
Mesh { url "a.xm" }
Mesh { url "" }
Mesh { }
T { list [ DEF d Mesh { url "d.xm" } ] }
Other { url "not-a-mesh.xm" }`)
	want := []string{"a.xm", "b.xm", "c.xm", "a.xm", "d.xm"}
	if got := s.MeshRefs(); !reflect.DeepEqual(got, want) {
		t.Fatalf("MeshRefs() = %v want %v", got, want)
	}
	if got := mustParse(t, "").MeshRefs(); got != nil {
		t.Fatalf("empty MeshRefs() = %v want nil", got)
	}
}

func TestMaterials(t *testing.T) {
	s := mustParse(t, `Shape { appearance Appearance { material MaxMaterial { name "red" } } }
Shape { appearance Appearance { material Material { name "red" } } }
Group { children [ Shape { appearance Appearance { material Material { name "blue" } } } ] }
Material { name "" }
Material { }
T { mats [ DEF g MaxMaterial { name "green" } ] }
Other { name "ignored" }`)
	want := []string{"red", "blue", "green"}
	if got := s.Materials(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Materials() = %v want %v", got, want)
	}
	if got := mustParse(t, "").Materials(); got != nil {
		t.Fatalf("empty Materials() = %v want nil", got)
	}
}

func TestGoString(t *testing.T) {
	s := mustParse(t, `Shape { appearance Appearance { material Material { name "red" } } geometry Mesh { url "a.xm" } }
Mesh { url "a.xm" }`)
	if got, want := s.GoString(), "5 nodes, 2 mesh refs, 1 materials"; got != want {
		t.Fatalf("GoString() = %q want %q", got, want)
	}
	if got, want := mustParse(t, "").GoString(), "0 nodes, 0 mesh refs, 0 materials"; got != want {
		t.Fatalf("empty GoString() = %q want %q", got, want)
	}
}
