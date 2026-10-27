package xap

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"
)

func num(f float64) Value   { return Value{Kind: VNumber, Num: f} }
func str(s string) Value    { return Value{Kind: VString, Str: s} }
func ident(s string) Value  { return Value{Kind: VIdent, Str: s} }
func boolean(b bool) Value  { return Value{Kind: VBool, Bool: b} }
func nodeVal(n *Node) Value { return Value{Kind: VNode, Node: n} }
func arr(vs ...Value) Value { return Value{Kind: VArray, Array: vs} }

func field(key string, vs ...Value) Field { return Field{Key: key, Values: vs} }

func nodeItems(nodes ...*Node) []SceneItem {
	items := make([]SceneItem, len(nodes))
	for i, n := range nodes {
		items[i] = SceneItem{Kind: SNode, Node: n}
	}
	return items
}

func mustParse(t *testing.T, input string) *Scene {
	t.Helper()
	s, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse(%q): %v", input, err)
	}
	return s
}

func jsonOf(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(b)
}

func assertDeepEqual(t *testing.T, what string, got, want any) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s\n got %s\nwant %s", what, jsonOf(t, got), jsonOf(t, want))
	}
}

func assertDefs(t *testing.T, s *Scene, want map[string]*Node) {
	t.Helper()
	if len(s.Defs) != len(want) {
		t.Fatalf("Defs = %v want keys of %v", s.Defs, want)
	}
	for k, n := range want {
		if s.Defs[k] != n {
			t.Fatalf("Defs[%q] = %p want %p", k, s.Defs[k], n)
		}
	}
}

func TestParseEmptyInput(t *testing.T) {
	for _, input := range []string{"", " \t\n", "# only a comment\n// and another\n/* and a block */"} {
		s := mustParse(t, input)
		if len(s.Items) != 0 || s.Warnings != nil || s.Defs == nil || len(s.Defs) != 0 {
			t.Fatalf("Parse(%q) items %s warnings %v defs %v", input, jsonOf(t, s.Items), s.Warnings, s.Defs)
		}
	}
}

func TestParseNodeFieldValues(t *testing.T) {
	s := mustParse(t, `DEF root Transform {
  translation 1 2.5 -3
  name "hello world"
  visible true
  hidden FALSE
  mode wire-frame.v2/x~1
  list [ 1 "a" TRUE false b DEF e Shape { } USE e 2e1 ]
  material DEF m MaxMaterial { name "mat" }
  geometry Mesh { url "a.xm" }
  bare Shape
  ref USE m
}`)
	e := &Node{TypeName: "Shape", DefName: "e"}
	m := &Node{TypeName: "MaxMaterial", DefName: "m", Fields: []Field{field("name", str("mat"))}}
	root := &Node{
		TypeName: "Transform",
		DefName:  "root",
		Fields: []Field{
			field("translation", num(1), num(2.5), num(-3)),
			field("name", str("hello world")),
			field("visible", boolean(true)),
			field("hidden", boolean(false)),
			field("mode", ident("wire-frame.v2/x~1")),
			field("list", arr(num(1), str("a"), boolean(true), boolean(false), ident("b"), nodeVal(e), nodeVal(e), num(20))),
			field("material", nodeVal(m)),
			field("geometry", nodeVal(&Node{TypeName: "Mesh", Fields: []Field{field("url", str("a.xm"))}})),
			field("bare", ident("Shape")),
			field("ref", nodeVal(m)),
		},
	}
	assertDeepEqual(t, "items", s.Items, nodeItems(root))
	assertDeepEqual(t, "warnings", s.Warnings, []string(nil))

	got := s.Items[0].Node
	gotM := got.Fields[6].Values[0].Node
	gotE := got.Fields[5].Values[0].Array[5].Node
	if got.Fields[9].Values[0].Node != gotM {
		t.Fatalf("field USE m produced a copy of the DEF node")
	}
	if got.Fields[5].Values[0].Array[6].Node != gotE {
		t.Fatalf("array USE e produced a copy of the DEF node")
	}
	assertDefs(t, s, map[string]*Node{"root": got, "e": gotE, "m": gotM})
}

func TestParseFieldValueForms(t *testing.T) {
	cases := []struct {
		name string
		text string
		want []Value
	}{
		{"integers", "1 2 3", []Value{num(1), num(2), num(3)}},
		{"signed decimals", "-1.5 +2 7.", []Value{num(-1.5), num(2), num(7)}},
		{"exponents", "1e3 2E-2 -3.25e+1", []Value{num(1000), num(0.02), num(-32.5)}},
		{"zero", "0", []Value{num(0)}},
		{"lower true", "true", []Value{boolean(true)}},
		{"upper true", "TRUE", []Value{boolean(true)}},
		{"lower false", "false", []Value{boolean(false)}},
		{"upper false", "FALSE", []Value{boolean(false)}},
		{"identifier", "foo-bar.baz/qux~1", []Value{ident("foo-bar.baz/qux~1")}},
		{"upper identifier", "Shape", []Value{ident("Shape")}},
		{"string", `"a b"`, []Value{str("a b")}},
		{"string keeps backslash", `"a\b"`, []Value{str(`a\b`)}},
		{"string keeps hash and slashes", `"a#b//c"`, []Value{str("a#b//c")}},
		{"empty string", `""`, []Value{str("")}},
		{"empty array", "[ ]", []Value{arr()}},
		{"array skips punctuation", "[ 1, 2 ; ( 3 ) ]", []Value{arr(num(1), num(2), num(3))}},
		{"no value", "", nil},
		{"inline node", "Inner { k 1 }", []Value{nodeVal(&Node{TypeName: "Inner", Fields: []Field{field("k", num(1))}})}},
		{"inline def node", "DEF d Inner { }", []Value{nodeVal(&Node{TypeName: "Inner", DefName: "d"})}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := mustParse(t, "T { v "+c.text+" }")
			assertDeepEqual(t, "fields", s.Items[0].Node.Fields, []Field{field("v", c.want...)})
			assertDeepEqual(t, "warnings", s.Warnings, []string(nil))
		})
	}
}

func TestParseComments(t *testing.T) {
	s := mustParse(t, `# vrml comment { not a node }
// c comment DEF x
/* block
   comment } */
DEF a Shape { # trailing
  url "s#not-comment" // after
  /* mid */ n 1
}`)
	want := nodeItems(&Node{TypeName: "Shape", DefName: "a", Fields: []Field{
		field("url", str("s#not-comment")),
		field("n", num(1)),
	}})
	assertDeepEqual(t, "items", s.Items, want)
	assertDeepEqual(t, "warnings", s.Warnings, []string(nil))
	assertDefs(t, s, map[string]*Node{"a": s.Items[0].Node})
}

func TestParseTopLevelNodes(t *testing.T) {
	s := mustParse(t, "Shape Transform { x 1 } DEF n Group { } DEF bare Light")
	want := nodeItems(
		&Node{TypeName: "Shape"},
		&Node{TypeName: "Transform", Fields: []Field{field("x", num(1))}},
		&Node{TypeName: "Group", DefName: "n"},
		&Node{TypeName: "Light", DefName: "bare"},
	)
	assertDeepEqual(t, "items", s.Items, want)
	assertDefs(t, s, map[string]*Node{"n": s.Items[2].Node, "bare": s.Items[3].Node})
}

func TestParseChildrenArray(t *testing.T) {
	s := mustParse(t, `Group {
  children [
    DEF a Shape { k 1 }
    USE a
    MUheader Transform { t 2 }
    hud Panel { }
    Plain { p 3 }
    Bare
    junk 7 "s"
    DEF last Shape
  ]
}`)
	a := &Node{TypeName: "Shape", DefName: "a", Fields: []Field{field("k", num(1))}}
	want := nodeItems(&Node{TypeName: "Group", Children: []*Node{
		a,
		a,
		{TypeName: "Transform", DefName: "MUheader", Fields: []Field{field("t", num(2))}},
		{TypeName: "Panel", DefName: "hud"},
		{TypeName: "Plain", Fields: []Field{field("p", num(3))}},
		{TypeName: "Bare"},
		{TypeName: "Shape", DefName: "last"},
	}})
	assertDeepEqual(t, "items", s.Items, want)
	assertDeepEqual(t, "warnings", s.Warnings, []string(nil))
	kids := s.Items[0].Node.Children
	if kids[0] != kids[1] {
		t.Fatalf("USE a in children produced a copy of the DEF node")
	}
	assertDefs(t, s, map[string]*Node{"a": kids[0], "MUheader": kids[2], "hud": kids[3], "last": kids[6]})
}

func TestParseBodyChildren(t *testing.T) {
	s := mustParse(t, `Group {
  DEF a Shape { }
  USE a
  USE nobody
  Inner { x 1 }
  lower { y 2 }
  children [ Tail ]
  children [ Tail2 ]
}`)
	a := &Node{TypeName: "Shape", DefName: "a"}
	want := nodeItems(&Node{TypeName: "Group", Children: []*Node{
		a,
		a,
		{TypeName: "USE", DefName: "nobody"},
		{TypeName: "Inner", Fields: []Field{field("x", num(1))}},
		{TypeName: "lower", Fields: []Field{field("y", num(2))}},
		{TypeName: "Tail"},
		{TypeName: "Tail2"},
	}})
	assertDeepEqual(t, "items", s.Items, want)
	assertDeepEqual(t, "warnings", s.Warnings, []string{`USE "nobody": no matching DEF found`})
	kids := s.Items[0].Node.Children
	if kids[0] != kids[1] {
		t.Fatalf("USE a in body produced a copy of the DEF node")
	}
}

func TestParseUSEResolution(t *testing.T) {
	s := mustParse(t, `DEF a Shape { k 1 }
USE a
USE zz
T { f USE a g USE zz h [ USE a USE zz ] }`)
	a := &Node{TypeName: "Shape", DefName: "a", Fields: []Field{field("k", num(1))}}
	want := nodeItems(
		a,
		a,
		&Node{TypeName: "USE", DefName: "zz"},
		&Node{TypeName: "T", Fields: []Field{
			field("f", nodeVal(a)),
			field("g", ident("zz")),
			field("h", arr(nodeVal(a), nodeVal(&Node{TypeName: "USE", DefName: "zz"}))),
		}},
	)
	assertDeepEqual(t, "items", s.Items, want)
	assertDeepEqual(t, "warnings", s.Warnings, []string{
		`USE "zz": no matching DEF found`,
		`USE "zz": no matching DEF found`,
		`USE "zz": no matching DEF found`,
	})
	def := s.Items[0].Node
	tn := s.Items[3].Node
	if s.Items[1].Node != def || tn.Fields[0].Values[0].Node != def || tn.Fields[2].Values[0].Array[0].Node != def {
		t.Fatalf("USE a resolved to a copy instead of the DEF node")
	}
	assertDefs(t, s, map[string]*Node{"a": def})
}

const topFunc = "function top(a, b) {\n  if (a) { return \"}\"; } // } comment\n  /* { */ # {\n  return b;\n}"

func TestParseScripts(t *testing.T) {
	input := topFunc + "\nvar speed = 1.5;\nvar noSemi = 2\nDEF n Node {\n" +
		"  function inner() { return { x: 1 }; }\n" +
		"  behavior { on(x) { go(\"{\") } }\n" +
		"  var count = 3;\n" +
		"  k 1\n}\n"
	s := mustParse(t, input)
	want := []SceneItem{
		{Kind: SScript, Script: topFunc},
		{Kind: SScript, Script: "var speed = 1.5;"},
		{Kind: SScript, Script: "var noSemi = 2"},
		{Kind: SNode, Node: &Node{
			TypeName: "Node",
			DefName:  "n",
			Fields:   []Field{field("k", num(1))},
			Scripts: []string{
				"function inner() { return { x: 1 }; }",
				"behavior { on(x) { go(\"{\") } }",
				"var count = 3;",
			},
		}},
	}
	assertDeepEqual(t, "items", s.Items, want)
	assertDeepEqual(t, "warnings", s.Warnings, []string(nil))
}

func TestParseStrayTokens(t *testing.T) {
	s := mustParse(t, `} 5 "s" ) Shape`)
	assertDeepEqual(t, "items", s.Items, nodeItems(&Node{TypeName: "Shape"}))
	assertDeepEqual(t, "warnings", s.Warnings, []string{
		`skipping stray token "}" at pos 0`,
		`skipping stray token "5" at pos 2`,
		`skipping stray token "s" at pos 4`,
		`skipping stray token ")" at pos 8`,
	})

	s = mustParse(t, `T { 5 "s" ( ) ] x 1 }`)
	assertDeepEqual(t, "items", s.Items, nodeItems(&Node{TypeName: "T", Fields: []Field{field("x", num(1))}}))
	assertDeepEqual(t, "warnings", s.Warnings, []string(nil))

	input := `T { v - w 2 }`
	s = mustParse(t, input)
	assertDeepEqual(t, "items", s.Items, nodeItems(&Node{TypeName: "T", Fields: []Field{field("v"), field("w", num(2))}}))
	assertDeepEqual(t, "warnings", s.Warnings, []string{fmt.Sprintf(`bad number "-" at pos %d`, strings.Index(input, "-"))})
}

func TestParseUnexpectedToken(t *testing.T) {
	cases := []struct {
		input  string
		bad    string
		hasPos bool
	}{
		{`DEF 5 Shape { }`, "5", true},
		{`Shape { children 5 }`, "5", true},
		{`Group { children [ DEF ] }`, "]", true},
		{`T { f DEF 5 }`, "5", true},
		{`T { list [ DEF 5 ] }`, "5", true},
		{`USE "x"`, "x", false},
		{`T { USE 5 }`, "5", false},
		{`T { children [ USE { ] }`, "{", false},
		{`T { list [ USE ( ] }`, "(", false},
	}
	for _, c := range cases {
		t.Run(c.input, func(t *testing.T) {
			_, err := Parse(c.input)
			if !errors.Is(err, ErrUnexpectedToken) {
				t.Fatalf("error = %v want ErrUnexpectedToken", err)
			}
			want := fmt.Sprintf("got %q", c.bad)
			if c.hasPos {
				want += fmt.Sprintf(" at pos %d", strings.Index(c.input, c.bad))
			}
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("error %q does not contain %q", err.Error(), want)
			}
		})
	}
}

func parseRecovering(t *testing.T, input string) (err error) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Parse(%q) panicked: %v", input, r)
		}
	}()
	_, err = Parse(input)
	return err
}

func TestParseUnclosedScriptBodyReturnsUnexpectedEOF(t *testing.T) {
	for _, input := range []string{
		"function f() {",
		"Shape { behavior { a",
		"function f() { if (x) { }",
	} {
		t.Run(input, func(t *testing.T) {
			err := parseRecovering(t, input)
			if !errors.Is(err, ErrUnexpectedEOF) {
				t.Fatalf("Parse(%q) error = %v want ErrUnexpectedEOF", input, err)
			}
		})
	}
}

func TestParseUnclosedConstructReturnsUnexpectedEOF(t *testing.T) {
	for _, input := range []string{
		"Shape {",
		"DEF a Shape { k 1",
		"Shape { children [ Inner { } ",
		"Shape { list [ 1 2",
		`Shape { name "abc }`,
	} {
		t.Run(input, func(t *testing.T) {
			_, err := Parse(input)
			if !errors.Is(err, ErrUnexpectedEOF) {
				t.Fatalf("Parse(%q) error = %v want ErrUnexpectedEOF", input, err)
			}
		})
	}
}

func TestParseFileDecodesWindows1252(t *testing.T) {
	path := filepath.Join(t.TempDir(), "scene.xap")
	raw := []byte("DEF t Text {\n  string \"caf\xe9 \x93q\x94 \x96 \x80\"\n  # \x99 comment\n  n 1\n}\n")
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := ParseFile(path)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	want := nodeItems(&Node{TypeName: "Text", DefName: "t", Fields: []Field{
		field("string", str("caf\u00e9 \u201cq\u201d \u2013 \u20ac")),
		field("n", num(1)),
	}})
	assertDeepEqual(t, "items", s.Items, want)
	assertDeepEqual(t, "warnings", s.Warnings, []string(nil))
}

func TestParseFileMissing(t *testing.T) {
	_, err := ParseFile(filepath.Join(t.TempDir(), "missing.xap"))
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("error = %v want os.ErrNotExist", err)
	}
}

func TestDecodeWindows1252(t *testing.T) {
	high := []rune("\u20ac\u0081\u201a\u0192\u201e\u2026\u2020\u2021\u02c6\u2030\u0160\u2039\u0152\u008d\u017d\u008f" +
		"\u0090\u2018\u2019\u201c\u201d\u2022\u2013\u2014\u02dc\u2122\u0161\u203a\u0153\u009d\u017e\u0178")
	data := make([]byte, 256)
	want := make([]rune, 256)
	for i := range data {
		data[i] = byte(i)
		switch {
		case i < 0x80:
			want[i] = rune(i)
		case i < 0xa0:
			want[i] = high[i-0x80]
		default:
			want[i] = rune(i)
		}
	}
	got := decodeWindows1252(data)
	if utf8.RuneCountInString(got) != 256 {
		t.Fatalf("decoded rune count = %d want 256", utf8.RuneCountInString(got))
	}
	if got != string(want) {
		t.Fatalf("decoded\n got %q\nwant %q", got, string(want))
	}
	if got := decodeWindows1252(nil); got != "" {
		t.Fatalf("decoded nil = %q want empty", got)
	}
}

func TestScanMatchingBrace(t *testing.T) {
	cases := []struct {
		input string
		open  int
		want  int
	}{
		{"{}", 0, 1},
		{"{a{b}c}", 0, 6},
		{`{ "}" }`, 0, 6},
		{"{ // }\n}", 0, 7},
		{"{ /* } */ }", 0, 10},
		{"{ # }\n}", 0, 6},
		{"{ a / b }", 0, 8},
		{"x {y} {z}", 6, 8},
		{"{ab", 0, 3},
		{`{ "ab`, 0, 5},
	}
	for _, c := range cases {
		p := &parser{input: c.input}
		if got := p.scanMatchingBrace(c.open); got != c.want {
			t.Errorf("scanMatchingBrace(%q, %d) = %d want %d", c.input, c.open, got, c.want)
		}
	}
}

func TestCaptureBlockVerbatim(t *testing.T) {
	input := "{ x 1 } tail"
	p := &parser{tokens: lex(input), input: input}
	if got := p.captureBlockVerbatim(); got != "x 1" {
		t.Fatalf("captureBlockVerbatim = %q want %q", got, "x 1")
	}
	if p.peek().val != "tail" {
		t.Fatalf("next token after block = %q want tail", p.peek().val)
	}

	p = &parser{tokens: lex("{}"), input: "{}"}
	if got := p.captureBlockVerbatim(); got != "" {
		t.Fatalf("empty block = %q want empty", got)
	}
	if p.peek().typ != tokEOF {
		t.Fatalf("empty block left token %q unconsumed", p.peek().val)
	}

	p = &parser{tokens: lex("x"), input: "x"}
	if got := p.captureBlockVerbatim(); got != "" || p.peek().val != "x" {
		t.Fatalf("non-block = %q next %q want empty and x", got, p.peek().val)
	}
}

func TestPosAhead(t *testing.T) {
	p := &parser{tokens: lex("a b c")}
	for i, want := range []string{"a", "b", "c"} {
		if got := p.posAhead(i); got.typ != tokIdent || got.val != want {
			t.Errorf("posAhead(%d) = %+v want ident %q", i, got, want)
		}
	}
	for _, i := range []int{3, 4, 100} {
		if got := p.posAhead(i); got.typ != tokEOF {
			t.Errorf("posAhead(%d) = %+v want EOF", i, got)
		}
	}
}

func TestIsUpperStart(t *testing.T) {
	for _, c := range []struct {
		s    string
		want bool
	}{
		{"", false}, {"a", false}, {"A", true}, {"Zed", true}, {"_X", false}, {"9A", false}, {"\u00c9", false},
	} {
		if got := isUpperStart(c.s); got != c.want {
			t.Errorf("isUpperStart(%q) = %v want %v", c.s, got, c.want)
		}
	}
}
