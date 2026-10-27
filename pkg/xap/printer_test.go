package xap

import (
	"bytes"
	"reflect"
	"testing"
)

const printInput = `DEF root Transform {
  translation 1 2.5 -3
  name "hello"
  visible true
  flag FALSE
  mode ident
  list [ 1 "a" TRUE b ]
  material DEF m MaxMaterial { name "mat" }
  function f() { return 1 }
  children [
    Shape { geometry Mesh { url "a.xm" } }
    USE missing
    Bare
  ]
}
function top() { return 2 }`

const printWant = `DEF root Transform {
  translation 1 2.5 -3
  name "hello"
  visible TRUE
  flag FALSE
  mode ident
  list [ 1, "a", TRUE, b ]
  material DEF m MaxMaterial {
    name "mat"
  }
  function f() { return 1 }
  children [
    Shape {
      geometry Mesh {
        url "a.xm"
      }
    }
    USE missing
    Bare
  ]
}
function top() { return 2 }

`

const roundTripInput = "var speed = 1.5;\n" +
	"DEF cam Camera {\n" +
	"  position -1e-07 2.5e3 0.125\n" +
	"  target a~b\n" +
	"}\n" +
	"DEF grp Group {\n" +
	"  var count = 2;\n" +
	"  behavior { on_enter() }\n" +
	"  children [\n" +
	"    USE cam\n" +
	"    hud Transform {\n" +
	"      label \"caf\u00e9 \u2019\"\n" +
	"      children [\n" +
	"        DEF inner Shape { geometry Mesh { url \"m/a-b.xm\" } }\n" +
	"      ]\n" +
	"    }\n" +
	"  ]\n" +
	"}\n"

func TestPrettyPrint(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{name: "full scene", input: printInput, want: printWant},
		{
			name:  "inline node with scripts and children",
			input: `Shape { geometry DEF g Mesh { url "a" function f() { } children [ Sub ] } }`,
			want: "Shape {\n" +
				"  geometry DEF g Mesh {\n" +
				"    url \"a\"\n" +
				"    function f() { }\n" +
				"    children [\n" +
				"      Sub\n" +
				"    ]\n" +
				"  }\n" +
				"}\n",
		},
		{
			name:  "shared and unresolved use",
			input: "DEF a Shape { k 1 }\nUSE a\nUSE zz",
			want: "DEF a Shape {\n" +
				"  k 1\n" +
				"}\n" +
				"DEF a Shape {\n" +
				"  k 1\n" +
				"}\n" +
				"USE zz\n",
		},
		{
			name:  "var script in node",
			input: "T { var x = 1;\n n 2 }",
			want:  "T {\n  n 2\n  var x = 1;\n}\n",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var buf bytes.Buffer
			PrettyPrint(&buf, mustParse(t, c.input))
			if buf.String() != c.want {
				t.Fatalf("PrettyPrint\n got %q\nwant %q", buf.String(), c.want)
			}
		})
	}
}

func TestPrettyPrintConstructedValues(t *testing.T) {
	s := &Scene{Items: []SceneItem{
		{Kind: SNode},
		{Kind: SNode, Node: &Node{TypeName: "T", Fields: []Field{
			{Key: "k", Values: []Value{{Kind: VScript, Str: "a b"}}},
			{Key: "e"},
			{Key: "n", Values: []Value{{Kind: VNode}}},
			{Key: "arr", Values: []Value{arr()}},
			{Key: "nested", Values: []Value{arr(arr(num(1), num(2)), nodeVal(&Node{TypeName: "In", Fields: []Field{field("q", num(3))}}))}},
		}}},
		{Kind: SScript, Script: "function s() { }"},
	}}
	want := "T {\n" +
		"  k { a b }\n" +
		"  e \n" +
		"  n \n" +
		"  arr [  ]\n" +
		"  nested [ [ 1, 2 ], In {\n" +
		"    q 3\n" +
		"  } ]\n" +
		"}\n" +
		"function s() { }\n\n"
	var buf bytes.Buffer
	PrettyPrint(&buf, s)
	if buf.String() != want {
		t.Fatalf("PrettyPrint\n got %q\nwant %q", buf.String(), want)
	}
}

func TestPrettyPrintRoundTrip(t *testing.T) {
	for _, input := range []string{printInput, roundTripInput} {
		first := mustParse(t, input)
		var out1 bytes.Buffer
		PrettyPrint(&out1, first)
		second, err := Parse(out1.String())
		if err != nil {
			t.Fatalf("reparse of %q: %v", out1.String(), err)
		}
		assertDeepEqual(t, "items after round trip", second.Items, first.Items)
		assertDeepEqual(t, "defs after round trip", second.Defs, first.Defs)
		if !reflect.DeepEqual(second.Warnings, first.Warnings) {
			t.Fatalf("warnings after round trip %v want %v", second.Warnings, first.Warnings)
		}
		var out2 bytes.Buffer
		PrettyPrint(&out2, second)
		if out2.String() != out1.String() {
			t.Fatalf("second print differs\n got %q\nwant %q", out2.String(), out1.String())
		}
	}
}
