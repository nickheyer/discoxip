package web

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"image"
	"image/png"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/nickheyer/discoxip/pkg/buffer"
	"github.com/nickheyer/discoxip/pkg/scene"
	"github.com/nickheyer/discoxip/pkg/xap"
	"github.com/nickheyer/discoxip/pkg/xbe"
)

func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, body := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func parseScene(t *testing.T, text string) *xap.Scene {
	t.Helper()
	s, err := xap.Parse(text)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestConsoleName(t *testing.T) {
	tests := []struct{ in, want string }{
		{`Fonts\\Xbox.xtf`, `fonts\xbox.xtf`},
		{"%sBeep.wav", "beep.wav"},
		{`y:\Fonts\Xbox.xtf`, `fonts\xbox.xtf`},
		{`\Device\Harddisk0\Partition2\config.ini`, `device\harddisk0\partition2\config.ini`},
		{"Sounds/Music/intro.wav", `sounds\music\intro.wav`},
		{`\\\\Fonts\\\\x.xtf`, `fonts\x.xtf`},
		{`%s\\Device\\CdRom0\\default.xbe`, `device\cdrom0\default.xbe`},
		{`A/B\\C`, `a\b\c`},
		{`c:\a:b\c`, `b\c`},
		{"", ""},
		{"%s", ""},
		{"%S", ""},
		{"x%s", "x%s"},
		{`D:\`, ""},
		{`\\`, ""},
	}
	for _, tc := range tests {
		if got := consoleName(tc.in); got != tc.want {
			t.Errorf("consoleName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestConsoleSplit(t *testing.T) {
	tests := []struct{ in, dir, base string }{
		{`fonts\xbox.xtf`, "fonts", "xbox.xtf"},
		{"xbox.xtf", "", "xbox.xtf"},
		{`a\b\c`, `a\b`, "c"},
		{`a\`, "a", ""},
		{"", "", ""},
	}
	for _, tc := range tests {
		dir, base := consoleSplit(tc.in)
		if dir != tc.dir || base != tc.base {
			t.Errorf("consoleSplit(%q) = (%q, %q), want (%q, %q)", tc.in, dir, base, tc.dir, tc.base)
		}
	}
}

func TestConsoleNamed(t *testing.T) {
	named := map[string]bool{
		`fonts\xbox.xtf`:                         true,
		"beep.wav":                               true,
		`device\harddisk0\partition2\config.ini`: true,
	}
	patterns := map[string]bool{"*.xtf": true, `sounds\*.wav`: true, "*": true, "****": true, "*.*": true, "*.ini": true}
	dirs := map[string]map[string]bool{
		".xtf": {"fonts": true},
		".wav": {"": true},
		".ini": {`device\harddisk0\partition2`: true},
	}
	tests := []struct {
		path string
		want bool
	}{
		{`fonts\xbox.xtf`, true},
		{`sounds\beep.wav`, true},
		{"beep.wav", true},
		{"config.ini", true},
		{"xbox.xtf", true},
		{`fonts\xarial.xtf`, true},
		{`ui\fonts\big.xtf`, true},
		{`other\loose.xtf`, false},
		{`sounds\click.wav`, true},
		{`menu\sounds\click.wav`, true},
		{`other\noise.wav`, false},
		{`other\skip.ini`, false},
		{"extra.ini", false},
		{"stray.c", false},
		{"readme.txt", false},
		{`partition2\config.ini`, true},
		{`harddisk0\partition2\config.ini`, true},
		{`disk\config.ini`, false},
	}
	for _, tc := range tests {
		if got := consoleNamed(tc.path, named, patterns, dirs); got != tc.want {
			t.Errorf("consoleNamed(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

func TestConsoleFiles(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"Fonts/Xbox.xtf":         "xtf1",
		"Fonts/Xarial.xtf":       "xtf2",
		"Sounds/Beep.wav":        "wav1",
		"Sounds/click.wav":       "wav2",
		"Sounds/Music/intro.wav": "wav3",
		"Sounds/Music/extra.wav": "wav4",
		"config.ini":             "ini1",
		"extra.ini":              "ini2",
		"other/skip.ini":         "ini3",
		"other/loose.xtf":        "xtf3",
		"other/noise.wav":        "wav5",
		"other/readme.txt":       "txt",
		"stray.c":                "int main() {}",
	})
	names := []string{
		`Fonts\\Xbox.xtf`,
		`y:\Fonts\Xbox.xtf`,
		"%sBeep.wav",
		"Sounds/Beep.WAV",
		`Music\intro.wav`,
		`\Device\Harddisk0\Partition2\config.ini`,
		"boot.ini",
		"*.xtf",
		"*.ini",
		`Sounds\*.wav`,
		`music\*.wav`,
		"*",
		"****",
		"*.*",
		"",
		"%s",
	}
	got, err := consoleFiles(root, names)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"config.ini",
		"extra.ini",
		filepath.Join("Fonts", "Xarial.xtf"),
		filepath.Join("Fonts", "Xbox.xtf"),
		filepath.Join("Sounds", "Beep.wav"),
		filepath.Join("Sounds", "click.wav"),
		filepath.Join("Sounds", "Music", "extra.wav"),
		filepath.Join("Sounds", "Music", "intro.wav"),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("consoleFiles = %q, want %q", got, want)
	}
	var spelled []string
	for _, rel := range got {
		spelled = append(spelled, strings.ToLower(strings.ReplaceAll(filepath.ToSlash(rel), "/", `\`)))
	}
	wantSpelled := []string{
		"config.ini",
		"extra.ini",
		`fonts\xarial.xtf`,
		`fonts\xbox.xtf`,
		`sounds\beep.wav`,
		`sounds\click.wav`,
		`sounds\music\extra.wav`,
		`sounds\music\intro.wav`,
	}
	if !reflect.DeepEqual(spelled, wantSpelled) {
		t.Fatalf("config spelling = %q, want %q", spelled, wantSpelled)
	}
}

func TestConsoleFilesWildcardsWithoutExtensionAdmitNothing(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{"stray.c": "c", "a/b.txt": "t", "c.xtf": "x"})
	got, err := consoleFiles(root, []string{"*", "****", "*.*", "?", "*.", "a\\*"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("wildcards without an extension listed %q", got)
	}
}

func TestConsoleFilesMissingDirectory(t *testing.T) {
	_, err := consoleFiles(filepath.Join(t.TempDir(), "absent"), []string{"a.xtf"})
	if err == nil || !strings.HasPrefix(err.Error(), "web: listing console files: ") {
		t.Fatalf("error = %v", err)
	}
}

const sceneText = `function init() { load("intro.wav"); say("hi"); }
var music = "theme.wav";
DEF Root Transform {
  name "root"
  translation 1 2.5 -3
  visible TRUE
  mode fast
  tags [ "alpha" 1 "beta" ]
  empty [ ]
  material Material { name "steel" }
  children [
    Shape {
      geometry Mesh { url "box.xm" }
    }
  ]
  behavior { play("click.wav") }
  function tick() { show("tick"); }
}
`

func TestSceneStrings(t *testing.T) {
	got := sceneStrings(parseScene(t, sceneText))
	want := []string{"intro.wav", "hi", "theme.wav", "root", "alpha", "beta", "steel", "box.xm", "click.wav", "tick"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("sceneStrings = %q, want %q", got, want)
	}
}

func TestSceneStringsHandBuilt(t *testing.T) {
	s := &xap.Scene{Items: []xap.SceneItem{
		{Kind: xap.SNode, Node: nil},
		{Kind: xap.SNode, Node: &xap.Node{
			TypeName: "Transform",
			Fields: []xap.Field{
				{Key: "onclick", Values: []xap.Value{{Kind: xap.VScript, Str: `play("one.wav") stop("two.wav")`}}},
				{Key: "inner", Values: []xap.Value{{Kind: xap.VNode, Node: nil}}},
				{Key: "list", Values: []xap.Value{{Kind: xap.VArray, Array: []xap.Value{
					{Kind: xap.VNode, Node: &xap.Node{Fields: []xap.Field{{Key: "url", Values: []xap.Value{{Kind: xap.VString, Str: "deep.xm"}}}}}},
					{Kind: xap.VIdent, Str: "ignored"},
				}}}},
			},
			Children: []*xap.Node{{Scripts: []string{`function f() { a("three.wav") }`}}},
		}},
		{Kind: xap.SScript, Script: `var x = "four";`},
	}}
	got := sceneStrings(s)
	want := []string{"one.wav", "two.wav", "deep.xm", "three.wav", "four"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("sceneStrings = %q, want %q", got, want)
	}
}

func TestQuotedStrings(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{`foo("a.xtf", "b")`, []string{"a.xtf", "b"}},
		{`x "a" y "b" "c"`, []string{"a", "b", "c"}},
		{`""`, []string{""}},
		{`"abc`, nil},
		{`abc"`, nil},
		{"no quotes", nil},
		{"", nil},
		{`"one" "two`, []string{"one"}},
	}
	for _, tc := range tests {
		if got := quotedStrings(tc.in); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("quotedStrings(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestBuildConfig(t *testing.T) {
	files := []string{
		"/x/mainmenu5_default.xap",
		"/x/default_default.xap",
		"/x/default_music2.xap",
		"/x/Settings_Clock_default.xap",
		"/x/solo.xap",
	}
	cfg := buildConfig(files)
	if cfg.Entry != "default_default" {
		t.Errorf("entry %q, want default_default", cfg.Entry)
	}
	wantScenes := []string{"mainmenu5_default", "default_default", "default_music2", "Settings_Clock_default", "solo"}
	if !reflect.DeepEqual(cfg.Scenes, wantScenes) {
		t.Errorf("scenes %q, want %q", cfg.Scenes, wantScenes)
	}
	wantArchives := map[string]string{
		"mainmenu5":      "mainmenu5_default",
		"default":        "default_default",
		"Settings_Clock": "Settings_Clock_default",
		"settings_clock": "Settings_Clock_default",
		"solo":           "solo",
	}
	if !reflect.DeepEqual(cfg.Archives, wantArchives) {
		t.Errorf("archives %v, want %v", cfg.Archives, wantArchives)
	}
	wantInlines := map[string]string{"music2.xap": "default_music2", "Music2.xap": "default_music2"}
	if !reflect.DeepEqual(cfg.Inlines, wantInlines) {
		t.Errorf("inlines %v, want %v", cfg.Inlines, wantInlines)
	}
	if cfg.Files != nil || cfg.Missing != nil || cfg.Font != 0 {
		t.Errorf("files %v missing %v font %d, want none", cfg.Files, cfg.Missing, cfg.Font)
	}
}

func TestBuildConfigEntryFallsBackToFirst(t *testing.T) {
	cfg := buildConfig([]string{"/a/b_default.xap", "/a/c.xap"})
	if cfg.Entry != "b_default" {
		t.Errorf("entry %q, want b_default", cfg.Entry)
	}
	empty := buildConfig(nil)
	if empty.Entry != "" || len(empty.Scenes) != 0 || len(empty.Archives) != 0 || len(empty.Inlines) != 0 {
		t.Errorf("empty config = %+v", empty)
	}
}

func TestSplitArchiveScene(t *testing.T) {
	tests := []struct {
		in      string
		archive string
		scene   string
	}{
		{"mainmenu5_default", "mainmenu5", "default"},
		{"settings_Clock_default", "settings_Clock", "default"},
		{"default_music2", "default", "music2"},
		{"solo", "solo", "default"},
		{"_x", "", "x"},
		{"x_", "x", ""},
		{"", "", "default"},
	}
	for _, tc := range tests {
		got := splitArchiveScene(tc.in)
		if got[0] != tc.archive || got[1] != tc.scene {
			t.Errorf("splitArchiveScene(%q) = %q, want [%q %q]", tc.in, got, tc.archive, tc.scene)
		}
		if name := extractArchiveName(tc.in); name != tc.archive {
			t.Errorf("extractArchiveName(%q) = %q, want %q", tc.in, name, tc.archive)
		}
	}
}

func TestCapitalize(t *testing.T) {
	tests := []struct{ in, want string }{
		{"", ""},
		{"a", "A"},
		{"music2", "Music2"},
		{"Already", "Already"},
		{"1x", "1x"},
		{"ab cd", "Ab cd"},
	}
	for _, tc := range tests {
		if got := capitalize(tc.in); got != tc.want {
			t.Errorf("capitalize(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func marshalGeneric(t *testing.T, v interface{}) interface{} {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var out interface{}
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestSerializeScene(t *testing.T) {
	got := marshalGeneric(t, serializeScene(parseScene(t, sceneText)))
	obj := func(kv ...interface{}) map[string]interface{} {
		m := map[string]interface{}{}
		for i := 0; i < len(kv); i += 2 {
			m[kv[i].(string)] = kv[i+1]
		}
		return m
	}
	want := obj("items", []interface{}{
		obj("kind", "script", "script", `function init() { load("intro.wav"); say("hi"); }`),
		obj("kind", "script", "script", `var music = "theme.wav";`),
		obj("kind", "node", "node", obj(
			"type", "Transform",
			"def", "Root",
			"fields", obj(
				"name", "root",
				"translation", []interface{}{1.0, 2.5, -3.0},
				"visible", true,
				"mode", "fast",
				"tags", []interface{}{"alpha", 1.0, "beta"},
				"empty", nil,
				"material", obj("_node", obj("type", "Material", "fields", obj("name", "steel"))),
			),
			"children", []interface{}{
				obj("type", "Shape", "fields", obj("geometry", obj("_node", obj("type", "Mesh", "fields", obj("url", "box.xm"))))),
			},
			"scripts", []interface{}{`behavior { play("click.wav") }`, `function tick() { show("tick"); }`},
		)),
	})
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("scene JSON\n got %#v\nwant %#v", got, want)
	}
}

func TestSerializeNode(t *testing.T) {
	if serializeNode(nil) != nil {
		t.Fatal("nil node serialized to a value")
	}
	bare := marshalGeneric(t, serializeNode(&xap.Node{TypeName: "Shape"}))
	if !reflect.DeepEqual(bare, map[string]interface{}{"type": "Shape"}) {
		t.Fatalf("bare node = %#v", bare)
	}
	nested := marshalGeneric(t, serializeNode(&xap.Node{
		TypeName: "Group",
		DefName:  "G",
		Children: []*xap.Node{{TypeName: "A"}, {TypeName: "B", Scripts: []string{"var q = 1;"}}},
	}))
	want := map[string]interface{}{
		"type": "Group",
		"def":  "G",
		"children": []interface{}{
			map[string]interface{}{"type": "A"},
			map[string]interface{}{"type": "B", "scripts": []interface{}{"var q = 1;"}},
		},
	}
	if !reflect.DeepEqual(nested, want) {
		t.Fatalf("nested node = %#v, want %#v", nested, want)
	}
}

func TestSerializeFieldValues(t *testing.T) {
	num := func(f float64) xap.Value { return xap.Value{Kind: xap.VNumber, Num: f} }
	str := func(s string) xap.Value { return xap.Value{Kind: xap.VString, Str: s} }
	tests := []struct {
		name   string
		values []xap.Value
		want   interface{}
	}{
		{"none", nil, nil},
		{"single", []xap.Value{str("a")}, "a"},
		{"vector", []xap.Value{num(1), num(2), num(3)}, []float64{1, 2, 3}},
		{"mixed", []xap.Value{num(1), str("a"), {Kind: xap.VBool, Bool: false}}, []interface{}{1.0, "a", false}},
	}
	for _, tc := range tests {
		if got := serializeFieldValues(tc.values); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: serializeFieldValues = %#v, want %#v", tc.name, got, tc.want)
		}
	}
}

func TestSerializeValue(t *testing.T) {
	inner := &xap.Node{TypeName: "Mesh", Fields: []xap.Field{{Key: "url", Values: []xap.Value{{Kind: xap.VString, Str: "a.xm"}}}}}
	tests := []struct {
		name string
		in   xap.Value
		want interface{}
	}{
		{"number", xap.Value{Kind: xap.VNumber, Num: 2.5}, 2.5},
		{"string", xap.Value{Kind: xap.VString, Str: "s"}, "s"},
		{"bool", xap.Value{Kind: xap.VBool, Bool: true}, true},
		{"ident", xap.Value{Kind: xap.VIdent, Str: "id"}, "id"},
		{"script", xap.Value{Kind: xap.VScript, Str: "x()"}, map[string]interface{}{"_script": "x()"}},
		{"node", xap.Value{Kind: xap.VNode, Node: inner}, map[string]interface{}{"_node": serializeNode(inner)}},
		{"nil node", xap.Value{Kind: xap.VNode}, nil},
		{"array", xap.Value{Kind: xap.VArray, Array: []xap.Value{{Kind: xap.VNumber, Num: 1}, {Kind: xap.VString, Str: "b"}}}, []interface{}{1.0, "b"}},
		{"empty array", xap.Value{Kind: xap.VArray}, []interface{}(nil)},
		{"unknown", xap.Value{Kind: xap.ValueKind(99)}, nil},
	}
	for _, tc := range tests {
		if got := serializeValue(tc.in); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: serializeValue = %#v, want %#v", tc.name, got, tc.want)
		}
	}
}

func TestExportMeshBinary(t *testing.T) {
	md := &scene.MeshData{
		Name:   "box.xm",
		Format: 0x12345678,
		Count:  3,
		Attributes: []buffer.Attribute{
			{Element: buffer.Element{Register: 0, Type: "FLOAT3", Offset: 0, Comps: 3}, Data: []float32{0, 1, 2, 3, 4, 5, 6, 7, 8}},
			{Element: buffer.Element{Register: 9, Type: "FLOAT2", Offset: 12, Comps: 2}, Data: []float32{0.5, -1.5, 2.25, 3, 4, 5}},
		},
		Indices: []uint16{0, 1, 2, 2, 1, 0},
	}
	layout := scene.Layout{Stride: 20, Semantics: map[int]string{0: "position", 9: "texcoord0"}}
	path := filepath.Join(t.TempDir(), "menu_box.bin")
	entry, err := exportMeshBinary(path, md, layout)
	if err != nil {
		t.Fatal(err)
	}
	want := meshEntry{
		File: "menu_box.bin", VertexCount: 3, IndexCount: 6, Format: 0x12345678, Stride: 20,
		Attributes: []meshAttribute{
			{Register: 0, Semantic: "position", Type: "FLOAT3", Offset: 0, Comps: 3},
			{Register: 9, Semantic: "texcoord0", Type: "FLOAT2", Offset: 12, Comps: 2},
		},
	}
	if !reflect.DeepEqual(entry, want) {
		t.Fatalf("entry %+v, want %+v", entry, want)
	}
	var expect []byte
	for _, v := range append(append([]float32{}, md.Attributes[0].Data...), md.Attributes[1].Data...) {
		expect = binary.LittleEndian.AppendUint32(expect, math.Float32bits(v))
	}
	for _, i := range md.Indices {
		expect = binary.LittleEndian.AppendUint16(expect, i)
	}
	if got := readFile(t, path); !bytes.Equal(got, expect) {
		t.Fatalf("file % x, want % x", got, expect)
	}
	if _, err := exportMeshBinary(filepath.Join(t.TempDir(), "missing", "x.bin"), md, layout); err == nil {
		t.Fatal("writing into a missing directory succeeded")
	}
}

func TestFontVersion(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
		want  int
	}{
		{"absent", nil, 0},
		{"fonts dir", map[string]string{"fonts/Xbox.xtf": "XTF1rest"}, 1},
		{"root", map[string]string{"Xbox.xtf": "XTF2rest"}, 2},
		{"fonts dir first", map[string]string{"fonts/Xbox.xtf": "XTF1", "Xbox.xtf": "XTF2"}, 1},
		{"short magic", map[string]string{"fonts/Xbox.xtf": "XT"}, 0},
		{"short then root", map[string]string{"fonts/Xbox.xtf": "XT", "Xbox.xtf": "XTF3"}, 3},
		{"version zero", map[string]string{"Xbox.xtf": "XTF0"}, 0},
	}
	for _, tc := range tests {
		dir := t.TempDir()
		writeTree(t, dir, tc.files)
		if got := fontVersion(dir); got != tc.want {
			t.Errorf("%s: fontVersion = %d, want %d", tc.name, got, tc.want)
		}
	}
}

func TestCopyConsoleFiles(t *testing.T) {
	src := t.TempDir()
	writeTree(t, src, map[string]string{
		"Fonts/Xbox.xtf":  "one",
		"FONTS/Big.XTF":   "two",
		"Sounds/beep.wav": "three",
		"Xarial.xtf":      "four",
	})
	dst := filepath.Join(t.TempDir(), "files")
	files := []string{
		filepath.Join("Fonts", "Xbox.xtf"),
		filepath.Join("Sounds", "beep.wav"),
		filepath.Join("FONTS", "Big.XTF"),
		"Xarial.xtf",
	}
	count, err := copyConsoleFiles(src, dst, files)
	if err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Fatalf("count %d, want 3", count)
	}
	for rel, body := range map[string]string{"fonts/xbox.xtf": "one", "fonts/big.xtf": "two", "xarial.xtf": "four"} {
		if got := string(readFile(t, filepath.Join(dst, rel))); got != body {
			t.Errorf("%s holds %q, want %q", rel, got, body)
		}
	}
	if _, err := os.Stat(filepath.Join(dst, "sounds", "beep.wav")); !os.IsNotExist(err) {
		t.Errorf("a wav file was copied, stat err %v", err)
	}
	if _, err := copyConsoleFiles(src, dst, []string{"gone.xtf"}); err == nil || !strings.HasPrefix(err.Error(), "web: copying console files: ") {
		t.Errorf("missing file error = %v", err)
	}
}

func TestCopyTextures(t *testing.T) {
	src := t.TempDir()
	writeTree(t, src, map[string]string{
		"a.png":     "png-a",
		"b.PNG":     "png-b",
		"c.xbx":     "xbx",
		"d.txt":     "txt",
		"sub/e.png": "nested",
	})
	if err := os.Mkdir(filepath.Join(src, "f.png"), 0o755); err != nil {
		t.Fatal(err)
	}
	dst := t.TempDir()
	if got := copyTextures(src, dst); got != 2 {
		t.Fatalf("count %d, want 2", got)
	}
	entries, err := os.ReadDir(dst)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if !reflect.DeepEqual(names, []string{"a.png", "b.PNG"}) {
		t.Fatalf("copied %q", names)
	}
	if got := string(readFile(t, filepath.Join(dst, "b.PNG"))); got != "png-b" {
		t.Fatalf("b.PNG holds %q", got)
	}
	if got := copyTextures(filepath.Join(src, "absent"), dst); got != 0 {
		t.Fatalf("absent source copied %d", got)
	}
}

type imaState struct {
	pred int32
	idx  int
}

// Encodes one sample to a nibble and returns the value the decoder reconstructs for it
func (s *imaState) encode(sample int16) (byte, int16) {
	step := int32(imaStepTable[s.idx])
	delta := int32(sample) - s.pred
	var nibble byte
	if delta < 0 {
		nibble = 8
		delta = -delta
	}
	if delta >= step {
		nibble |= 4
		delta -= step
	}
	if delta >= step>>1 {
		nibble |= 2
		delta -= step >> 1
	}
	if delta >= step>>2 {
		nibble |= 1
	}
	diff := step >> 3
	if nibble&4 != 0 {
		diff += step
	}
	if nibble&2 != 0 {
		diff += step >> 1
	}
	if nibble&1 != 0 {
		diff += step >> 2
	}
	if nibble&8 != 0 {
		s.pred -= diff
	} else {
		s.pred += diff
	}
	if s.pred > 32767 {
		s.pred = 32767
	}
	if s.pred < -32768 {
		s.pred = -32768
	}
	s.idx += imaIndexTable[nibble]
	if s.idx < 0 {
		s.idx = 0
	}
	if s.idx > 88 {
		s.idx = 88
	}
	return nibble, int16(s.pred)
}

// Packs one block of per channel samples and returns it with the samples the decoder yields per channel
func adpcmBlock(samples [][]int16, preds []int16, idxs []int) ([]byte, [][]int16) {
	channels := len(samples)
	var block []byte
	states := make([]imaState, channels)
	for ch := range channels {
		block = binary.LittleEndian.AppendUint16(block, uint16(preds[ch]))
		block = append(block, byte(idxs[ch]), 0)
		states[ch] = imaState{pred: int32(preds[ch]), idx: idxs[ch]}
	}
	decoded := make([][]int16, channels)
	for g := 0; g < len(samples[0])/8; g++ {
		for ch := range channels {
			for i := 0; i < 4; i++ {
				lo, dlo := states[ch].encode(samples[ch][g*8+2*i])
				hi, dhi := states[ch].encode(samples[ch][g*8+2*i+1])
				block = append(block, lo|hi<<4)
				decoded[ch] = append(decoded[ch], dlo, dhi)
			}
		}
	}
	return block, decoded
}

func riffChunk(id string, body []byte) []byte {
	out := binary.LittleEndian.AppendUint32([]byte(id), uint32(len(body)))
	out = append(out, body...)
	if len(body)%2 == 1 {
		out = append(out, 0)
	}
	return out
}

func wavFile(chunks ...[]byte) []byte {
	body := []byte("WAVE")
	for _, c := range chunks {
		body = append(body, c...)
	}
	return riffChunk("RIFF", body)
}

func fmtChunk(format, channels uint16, rate uint32, blockAlign, bits uint16, extra []byte) []byte {
	b := binary.LittleEndian.AppendUint16(nil, format)
	b = binary.LittleEndian.AppendUint16(b, channels)
	b = binary.LittleEndian.AppendUint32(b, rate)
	b = binary.LittleEndian.AppendUint32(b, rate*uint32(blockAlign))
	b = binary.LittleEndian.AppendUint16(b, blockAlign)
	b = binary.LittleEndian.AppendUint16(b, bits)
	return riffChunk("fmt ", append(b, extra...))
}

func pcmWav(channels uint16, rate uint32, samples []int16) []byte {
	var data []byte
	for _, s := range samples {
		data = binary.LittleEndian.AppendUint16(data, uint16(s))
	}
	return wavFile(fmtChunk(1, channels, rate, channels*2, 16, nil), riffChunk("data", data))
}

func interleave(blocks ...[][]int16) []int16 {
	var out []int16
	for _, chans := range blocks {
		for i := range chans[0] {
			for _, ch := range chans {
				out = append(out, ch[i])
			}
		}
	}
	return out
}

func TestIMATables(t *testing.T) {
	if imaStepTable[0] != 7 || imaStepTable[88] != 32767 {
		t.Fatalf("step table ends %d..%d, want 7..32767", imaStepTable[0], imaStepTable[88])
	}
	for i := 1; i < len(imaStepTable); i++ {
		if imaStepTable[i] <= imaStepTable[i-1] {
			t.Fatalf("step table is not rising at %d", i)
		}
	}
	want := [16]int{-1, -1, -1, -1, 2, 4, 6, 8, -1, -1, -1, -1, 2, 4, 6, 8}
	if imaIndexTable != want {
		t.Fatalf("index table %v, want %v", imaIndexTable, want)
	}
	for _, tc := range []struct{ in, want int }{{-5, 0}, {0, 0}, {40, 40}, {88, 88}, {89, 88}, {1000, 88}} {
		if got := clampIndex(tc.in); got != tc.want {
			t.Errorf("clampIndex(%d) = %d, want %d", tc.in, got, tc.want)
		}
	}
	for _, tc := range []struct {
		in   int32
		want int16
	}{{-40000, -32768}, {-32768, -32768}, {-1, -1}, {0, 0}, {32767, 32767}, {32768, 32767}, {100000, 32767}} {
		if got := clampSample(tc.in); got != tc.want {
			t.Errorf("clampSample(%d) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestDecodeXboxADPCMStereo(t *testing.T) {
	left := []int16{0, 500, 1200, 2500, 4000, 3000, 1000, -500, -2000, -4000, -3000, -1000, 0, 800, 1600, 2400}
	right := make([]int16, len(left))
	for i, v := range left {
		right[i] = 100 - v/2
	}
	block1, dec1 := adpcmBlock([][]int16{left, right}, []int16{0, 100}, []int{0, 3})
	left2 := make([]int16, len(left))
	right2 := make([]int16, len(left))
	for i := range left {
		left2[i] = left[len(left)-1-i] * 3
		right2[i] = right[i] - 3000
	}
	block2, dec2 := adpcmBlock([][]int16{left2, right2}, []int16{dec1[0][15], dec1[1][15]}, []int{10, 20})
	data := append(append(block1, block2...), 0xAA, 0xBB, 0xCC)
	in := wavFile(
		fmtChunk(0x69, 2, 22050, 24, 4, []byte{2, 0, 16, 0}),
		riffChunk("junk", []byte{1, 2, 3}),
		riffChunk("data", data),
	)
	got, err := decodeXboxADPCM(in)
	if err != nil {
		t.Fatal(err)
	}
	want := pcmWav(2, 22050, interleave(dec1, dec2))
	if !bytes.Equal(got, want) {
		t.Fatalf("decoded\n got % x\nwant % x", got, want)
	}
}

func TestDecodeXboxADPCMMonoSaturates(t *testing.T) {
	samples := []int16{32000, 32767, 32767, -32768, -32768, 0, 0, 0, 5, -5, 32767, 32767, 32767, -32768, 0, 1}
	block, dec := adpcmBlock([][]int16{samples}, []int16{-20}, []int{88})
	in := wavFile(fmtChunk(0x69, 1, 8000, 12, 4, []byte{2, 0, 16, 0}), riffChunk("data", block))
	got, err := decodeXboxADPCM(in)
	if err != nil {
		t.Fatal(err)
	}
	if want := pcmWav(1, 8000, dec[0]); !bytes.Equal(got, want) {
		t.Fatalf("decoded\n got % x\nwant % x", got, want)
	}
	saturated := false
	for _, s := range dec[0] {
		if s == 32767 || s == -32768 {
			saturated = true
		}
	}
	if !saturated {
		t.Fatalf("samples %v never reached the clamp", dec[0])
	}
}

func TestDecodeXboxADPCMPassesOtherFormatsThrough(t *testing.T) {
	in := pcmWav(1, 8000, []int16{1, -2, 3})
	got, err := decodeXboxADPCM(in)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, in) {
		t.Fatalf("PCM input was altered")
	}
}

func TestDecodeXboxADPCMErrors(t *testing.T) {
	filler := riffChunk("junk", make([]byte, 40))
	tests := []struct {
		name string
		in   []byte
		want string
	}{
		{"short", []byte("RIFF"), "not a WAV file"},
		{"wrong riff", append([]byte("RIFX"), wavFile(filler)[4:]...), "not a WAV file"},
		{"wrong wave", append(wavFile(filler)[:8:8], append([]byte("WAVX"), filler...)...), "not a WAV file"},
		{"no chunks", wavFile(filler), "missing fmt or data chunk"},
		{"only fmt", wavFile(fmtChunk(0x69, 2, 22050, 24, 4, nil), filler), "missing fmt or data chunk"},
		{"only data", wavFile(riffChunk("data", make([]byte, 24)), filler), "missing fmt or data chunk"},
		{"zero channels", wavFile(fmtChunk(0x69, 0, 22050, 24, 4, nil), riffChunk("data", make([]byte, 24))), "invalid ADPCM params: ch=0 blockAlign=24"},
		{"zero block", wavFile(fmtChunk(0x69, 2, 22050, 0, 4, nil), riffChunk("data", make([]byte, 24))), "invalid ADPCM params: ch=2 blockAlign=0"},
	}
	for _, tc := range tests {
		got, err := decodeXboxADPCM(tc.in)
		if err == nil || err.Error() != tc.want {
			t.Errorf("%s: error %v, want %q", tc.name, err, tc.want)
		}
		if got != nil {
			t.Errorf("%s: returned %d bytes with the error", tc.name, len(got))
		}
	}
}

func TestCopyAudio(t *testing.T) {
	samples := []int16{10, 20, 30, 40, 50, 60, 70, 80}
	block, dec := adpcmBlock([][]int16{samples}, []int16{10}, []int{0})
	adpcm := wavFile(fmtChunk(0x69, 1, 8000, 8, 4, []byte{2, 0, 8, 0}), riffChunk("data", block))
	pcm := pcmWav(2, 44100, []int16{1, 2, 3, 4})
	garbage := []byte("RIFF not really a wave file at all, just bytes that end in .wav")
	src := t.TempDir()
	writeTree(t, src, map[string]string{
		"Audio/a.wav":     string(adpcm),
		"Audio/sub/b.WAV": string(pcm),
		"Audio/c.txt":     "text",
		"Audio/d.wav":     string(garbage),
		"Other/e.wav":     string(pcm),
	})
	dst := t.TempDir()
	if got := copyAudio(src, dst); got != 3 {
		t.Fatalf("count %d, want 3", got)
	}
	if got, want := readFile(t, filepath.Join(dst, "Audio", "a.wav")), pcmWav(1, 8000, dec[0]); !bytes.Equal(got, want) {
		t.Errorf("a.wav\n got % x\nwant % x", got, want)
	}
	if got := readFile(t, filepath.Join(dst, "Audio", "sub", "b.WAV")); !bytes.Equal(got, pcm) {
		t.Errorf("b.WAV was altered")
	}
	if got := readFile(t, filepath.Join(dst, "Audio", "d.wav")); !bytes.Equal(got, garbage) {
		t.Errorf("d.wav was altered")
	}
	for _, rel := range []string{"Audio/c.txt", "Other/e.wav"} {
		if _, err := os.Stat(filepath.Join(dst, filepath.FromSlash(rel))); !os.IsNotExist(err) {
			t.Errorf("%s was copied, stat err %v", rel, err)
		}
	}
	if got := copyAudio(t.TempDir(), dst); got != 0 {
		t.Fatalf("source without Audio copied %d", got)
	}
}

func TestWriteRenderModel(t *testing.T) {
	model := &xbe.RenderModel{
		Memory: xbe.ImageMemory{File: "image.bin", Base: 0x10000},
		Textures: []*xbe.ProducedTexture{{
			VA: 0x1234, Width: 2, Height: 2, Format: 6,
			Pixels: []uint32{0xFF112233, 0x80445566, 0x00778899, 0xFFAABBCC},
		}},
	}
	dataDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dataDir, "textures"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeRenderModel(model, dataDir); err != nil {
		t.Fatal(err)
	}
	var render struct {
		Memory struct {
			File string `json:"file"`
			Base uint32 `json:"base"`
		} `json:"memory"`
		Textures []struct {
			VA     uint32 `json:"va"`
			Width  int    `json:"width"`
			Height int    `json:"height"`
			Format uint32 `json:"format"`
		} `json:"textures"`
	}
	if err := json.Unmarshal(readFile(t, filepath.Join(dataDir, "render.json")), &render); err != nil {
		t.Fatal(err)
	}
	if render.Memory.File != "image.bin" || render.Memory.Base != 0x10000 {
		t.Errorf("memory %+v", render.Memory)
	}
	if len(render.Textures) != 1 || render.Textures[0].VA != 0x1234 || render.Textures[0].Width != 2 || render.Textures[0].Height != 2 || render.Textures[0].Format != 6 {
		t.Errorf("textures %+v", render.Textures)
	}
	if got := readFile(t, filepath.Join(dataDir, "image.bin")); len(got) != len(model.MemoryBytes()) {
		t.Errorf("image.bin holds %d bytes, want %d", len(got), len(model.MemoryBytes()))
	}
	f, err := os.Open(filepath.Join(dataDir, "textures", "proc_00001234.png"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	decoded, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	img, ok := decoded.(*image.NRGBA)
	if !ok {
		t.Fatalf("decoded image is %T, want NRGBA", decoded)
	}
	if img.Bounds().Dx() != 2 || img.Bounds().Dy() != 2 {
		t.Fatalf("image bounds %v", img.Bounds())
	}
	want := []byte{0x11, 0x22, 0x33, 0xFF, 0x44, 0x55, 0x66, 0x80, 0x77, 0x88, 0x99, 0x00, 0xAA, 0xBB, 0xCC, 0xFF}
	if !bytes.Equal(img.Pix, want) {
		t.Errorf("pixels % x, want % x", img.Pix, want)
	}
}

func TestWriteRenderModelMissingTextureDir(t *testing.T) {
	model := &xbe.RenderModel{
		Memory:   xbe.ImageMemory{File: "image.bin"},
		Textures: []*xbe.ProducedTexture{{VA: 1, Width: 1, Height: 1, Pixels: []uint32{0}}},
	}
	if err := writeRenderModel(model, t.TempDir()); err == nil {
		t.Fatal("writing a texture without its directory succeeded")
	}
}

func TestSanitizeFilename(t *testing.T) {
	tests := []struct{ in, want string }{
		{"", ""},
		{"plain.bin", "plain.bin"},
		{`a/b\c d~e`, "a_b_c_d_e"},
		{"menu_~0/box", "menu__0_box"},
		{"//", "__"},
	}
	for _, tc := range tests {
		if got := sanitizeFilename(tc.in); got != tc.want {
			t.Errorf("sanitizeFilename(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestStaticFiles(t *testing.T) {
	entries, err := fs.ReadDir(staticFiles, "static")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "index.html" || entries[0].IsDir() {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("embedded files %q, want index.html alone", names)
	}
	data, err := staticFiles.ReadFile("static/index.html")
	if err != nil {
		t.Fatal(err)
	}
	page := string(data)
	if !strings.HasPrefix(page, "<!DOCTYPE html>") {
		t.Errorf("index.html starts %q", page[:min(len(page), 20)])
	}
	for _, want := range []string{
		"<title>Xbox Dashboard</title>",
		`<script type="module" src="runtime.js"></script>`,
		`"three": "https://cdn.jsdelivr.net/npm/three@0.170.0/build/three.module.js"`,
		`<div id="loading">Loading Xbox Dashboard...</div>`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("index.html lacks %q", want)
		}
	}
}
