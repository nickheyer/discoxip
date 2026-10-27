package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestXAPInfo(t *testing.T) {
	path := writeFile(t, filepath.Join(t.TempDir(), "menu_default.xap"), []byte(xapSceneText))
	res := runCLI(t, "xap", "info", path)
	if res.err != nil {
		t.Fatal(res.err)
	}
	want := "XAP Scene: " + path + "\n" +
		"  Nodes:      10\n" +
		"  Mesh Refs:  2\n" +
		"    - box.xm\n" +
		"    - ball.xm\n" +
		"  Materials:  1\n" +
		"    - Chrome\n" +
		"  Warnings:   1\n" +
		"    - USE \"Missing\": no matching DEF found\n"
	if res.out != want {
		t.Fatalf("stdout\n%q\nwant\n%q", res.out, want)
	}
}

func TestXAPInfoWithoutWarnings(t *testing.T) {
	path := writeFile(t, filepath.Join(t.TempDir(), "plain.xap"), []byte("Transform { name \"x\" }\n"))
	res := runCLI(t, "xap", "info", path)
	if res.err != nil {
		t.Fatal(res.err)
	}
	want := "XAP Scene: " + path + "\n  Nodes:      1\n  Mesh Refs:  0\n  Materials:  0\n"
	if res.out != want {
		t.Fatalf("stdout\n%q\nwant\n%q", res.out, want)
	}
	res = runCLI(t, "xap", "info", filepath.Join(t.TempDir(), "absent.xap"))
	if res.err == nil || !os.IsNotExist(res.err) {
		t.Errorf("missing file error = %v", res.err)
	}
}

func TestXAPDump(t *testing.T) {
	path := writeFile(t, filepath.Join(t.TempDir(), "menu_default.xap"), []byte(xapSceneText))
	res := runCLI(t, "xap", "dump", path)
	if res.err != nil {
		t.Fatal(res.err)
	}
	shape := func(url string) string {
		return "    Shape {\n" +
			"      appearance Appearance {\n" +
			"        material MaxMaterial {\n" +
			"          name \"Chrome\"\n" +
			"        }\n" +
			"      }\n" +
			"      geometry Mesh {\n" +
			"        url \"" + url + "\"\n" +
			"      }\n" +
			"    }\n"
	}
	want := "DEF Root Transform {\n  children [\n" + shape("box.xm") + shape("ball.xm") + "  ]\n}\nUSE Missing\n"
	if res.out != want {
		t.Fatalf("stdout\n%s\nwant\n%s", res.out, want)
	}
}

func TestXAPDumpScriptsAndFields(t *testing.T) {
	text := "var n = 3;\nDEF T Transform {\n  translation 1 2.5 -3\n  visible TRUE\n  tags [ \"a\" 2 ]\n  function f() { go(); }\n}\n"
	path := writeFile(t, filepath.Join(t.TempDir(), "s.xap"), []byte(text))
	res := runCLI(t, "xap", "dump", path)
	if res.err != nil {
		t.Fatal(res.err)
	}
	want := "var n = 3;\n\n" +
		"DEF T Transform {\n" +
		"  translation 1 2.5 -3\n" +
		"  visible TRUE\n" +
		"  tags [ \"a\", 2 ]\n" +
		"  function f() { go(); }\n" +
		"}\n"
	if res.out != want {
		t.Fatalf("stdout\n%s\nwant\n%s", res.out, want)
	}
}

func meshDir(t *testing.T) (string, int) {
	t.Helper()
	dir := t.TempDir()
	text := []byte(`DEF Box Shape { geometry Mesh { url "box.xm" } }`)
	writeFile(t, filepath.Join(dir, "text.xm"), text)
	writeFile(t, filepath.Join(dir, "binary.xm"), []byte{1, 2, 3})
	writeFile(t, filepath.Join(dir, "empty.xm"), nil)
	writeFile(t, filepath.Join(dir, "zeros.xm"), make([]byte, 4))
	writeFile(t, filepath.Join(dir, "other.txt"), []byte("DEF Not Shape"))
	return dir, len(text)
}

func TestMeshInfoFile(t *testing.T) {
	dir, textLen := meshDir(t)
	path := filepath.Join(dir, "text.xm")
	res := runCLI(t, "mesh", "info", path)
	if res.err != nil {
		t.Fatal(res.err)
	}
	want := path + "\n  Type: text (VRML)\n" + fmt.Sprintf("  Size: %d bytes\n", textLen) + "  Nodes: 1\n  Mesh refs: [box.xm]\n"
	if res.out != want {
		t.Fatalf("stdout\n%q\nwant\n%q", res.out, want)
	}
}

func TestMeshInfoDirectory(t *testing.T) {
	dir, textLen := meshDir(t)
	res := runCLI(t, "mesh", "info", dir)
	if res.err != nil {
		t.Fatal(res.err)
	}
	want := filepath.Join(dir, "binary.xm") + "\n  Type: binary\n  Size: 3 bytes\n\n" +
		filepath.Join(dir, "empty.xm") + "\n  Type: empty\n  Size: 0 bytes\n\n" +
		filepath.Join(dir, "text.xm") + "\n  Type: text (VRML)\n" + fmt.Sprintf("  Size: %d bytes\n", textLen) + "  Nodes: 1\n  Mesh refs: [box.xm]\n\n" +
		filepath.Join(dir, "zeros.xm") + "\n  Type: empty\n  Size: 4 bytes\n\n"
	if res.out != want {
		t.Fatalf("stdout\n%q\nwant\n%q", res.out, want)
	}
}

func TestMeshInfoFileAndDirectory(t *testing.T) {
	dir, _ := meshDir(t)
	lone := writeFile(t, filepath.Join(t.TempDir(), "lone.xm"), []byte{9})
	res := runCLI(t, "mesh", "info", lone, dir)
	if res.err != nil {
		t.Fatal(res.err)
	}
	var heads []string
	for _, line := range strings.Split(res.out, "\n") {
		if line != "" && !strings.HasPrefix(line, " ") {
			heads = append(heads, line)
		}
	}
	want := []string{lone, filepath.Join(dir, "binary.xm"), filepath.Join(dir, "empty.xm"), filepath.Join(dir, "text.xm"), filepath.Join(dir, "zeros.xm")}
	if !reflect.DeepEqual(heads, want) {
		t.Fatalf("paths %q, want %q", heads, want)
	}
}

func TestMeshInfoErrors(t *testing.T) {
	empty := t.TempDir()
	res := runCLI(t, "mesh", "info", empty)
	if res.err == nil || res.err.Error() != "no .xm files found in "+empty {
		t.Errorf("empty directory error = %v", res.err)
	}
	res = runCLI(t, "mesh", "info", filepath.Join(empty, "absent.xm"))
	if res.err == nil || !os.IsNotExist(res.err) {
		t.Errorf("missing file error = %v", res.err)
	}
}

func TestBufferInfo(t *testing.T) {
	path := writeFile(t, filepath.Join(t.TempDir(), "~0.vb"), vbFile(3, 0x112, 20))
	res := runCLI(t, "buffer", "info", path)
	if res.err != nil {
		t.Fatal(res.err)
	}
	want := "Vertex Buffer: " + path + "\n  Vertex Count: 3\n  Format Code:  0x00000112\n  Stride:       20 bytes\n"
	if res.out != want {
		t.Fatalf("stdout\n%q\nwant\n%q", res.out, want)
	}
	empty := writeFile(t, filepath.Join(t.TempDir(), "empty.vb"), vbFile(0, 0xABCDEF01, 0))
	res = runCLI(t, "buffer", "info", empty)
	if res.err != nil {
		t.Fatal(res.err)
	}
	want = "Vertex Buffer: " + empty + "\n  Vertex Count: 0\n  Format Code:  0xABCDEF01\n  Stride:       0 bytes\n"
	if res.out != want {
		t.Fatalf("stdout\n%q\nwant\n%q", res.out, want)
	}
	short := writeFile(t, filepath.Join(t.TempDir(), "short.vb"), []byte{1, 2, 3})
	res = runCLI(t, "buffer", "info", short)
	if res.err == nil || res.err.Error() != "buffer: vertex buffer file is truncated" {
		t.Errorf("short file error = %v", res.err)
	}
}

func textureInfoText(path string) string {
	return path + "\n" +
		"  Dimensions:  4x4\n" +
		"  Format:      A8R8G8B8 (32 bpp)\n" +
		"  Compressed:  false\n" +
		"  Swizzled:    true\n" +
		"  Mip Levels:  1\n" +
		"  Header Size: 32 bytes\n" +
		"  Data Size:   64 bytes\n" +
		"  Format Reg:  0x02210629\n"
}

func TestTextureInfo(t *testing.T) {
	dir := t.TempDir()
	a := writeFile(t, filepath.Join(dir, "a.xbx"), xprTexture(4, texturePixels(4)))
	res := runCLI(t, "texture", "info", a)
	if res.err != nil {
		t.Fatal(res.err)
	}
	if res.out != textureInfoText(a) {
		t.Fatalf("stdout\n%q\nwant\n%q", res.out, textureInfoText(a))
	}
	bad := writeFile(t, filepath.Join(dir, "bad.xbx"), []byte("not a texture at all"))
	b := writeFile(t, filepath.Join(dir, "b.xbx"), xprTexture(4, texturePixels(4)))
	res = runCLI(t, "texture", "info", a, bad, b)
	if res.err != nil {
		t.Fatal(res.err)
	}
	if want := textureInfoText(a) + "\n" + textureInfoText(b) + "\n"; res.out != want {
		t.Fatalf("stdout\n%q\nwant\n%q", res.out, want)
	}
	if !strings.Contains(res.errOut, bad+": texture: invalid magic (expected XPR0)\n") {
		t.Errorf("stderr %q", res.errOut)
	}
}

func TestTextureExport(t *testing.T) {
	dir := t.TempDir()
	argb := texturePixels(4)
	path := writeFile(t, filepath.Join(dir, "a.xbx"), xprTexture(4, argb))
	res := runCLI(t, "texture", "export", path)
	if res.err != nil {
		t.Fatal(res.err)
	}
	checkPNG(t, filepath.Join(dir, "a.png"), 4, argb)
	if !strings.Contains(res.errOut, "Exported 4x4 A8R8G8B8 texture to "+filepath.Join(dir, "a.png")) {
		t.Errorf("stderr %q", res.errOut)
	}
	custom := filepath.Join(t.TempDir(), "custom.png")
	res = runCLI(t, "texture", "export", "-o", custom, path)
	if res.err != nil {
		t.Fatal(res.err)
	}
	checkPNG(t, custom, 4, argb)
	bad := writeFile(t, filepath.Join(dir, "bad.xbx"), []byte("not a texture at all"))
	res = runCLI(t, "texture", "export", bad)
	if res.err == nil || res.err.Error() != "texture: invalid magic (expected XPR0)" {
		t.Errorf("bad texture error = %v", res.err)
	}
}

func TestTextureExportAll(t *testing.T) {
	dir := t.TempDir()
	argb := texturePixels(4)
	writeFile(t, filepath.Join(dir, "a.xbx"), xprTexture(4, argb))
	writeFile(t, filepath.Join(dir, "b.xbx"), xprTexture(4, argb))
	writeFile(t, filepath.Join(dir, "bad.xbx"), []byte("not a texture at all"))
	writeFile(t, filepath.Join(dir, "c.txt"), []byte("text"))
	writeFile(t, filepath.Join(dir, "sub", "d.xbx"), xprTexture(4, argb))
	res := runCLI(t, "texture", "export-all", dir)
	if res.err != nil {
		t.Fatal(res.err)
	}
	want := "  a.xbx → a.png (4x4 A8R8G8B8)\n  b.xbx → b.png (4x4 A8R8G8B8)\n"
	if res.out != want {
		t.Fatalf("stdout\n%q\nwant\n%q", res.out, want)
	}
	if !strings.Contains(res.errOut, "skip bad.xbx: texture: invalid magic (expected XPR0)\n") || !strings.Contains(res.errOut, "Exported 2 textures\n") {
		t.Errorf("stderr %q", res.errOut)
	}
	checkPNG(t, filepath.Join(dir, "a.png"), 4, argb)
	checkPNG(t, filepath.Join(dir, "b.png"), 4, argb)
	for _, rel := range []string{"bad.png", "c.png", filepath.Join("sub", "d.png")} {
		if _, err := os.Stat(filepath.Join(dir, rel)); !os.IsNotExist(err) {
			t.Errorf("%s was written, stat err %v", rel, err)
		}
	}
	empty := t.TempDir()
	res = runCLI(t, "texture", "export-all", empty)
	if res.err == nil || res.err.Error() != "no XBX textures found in "+empty {
		t.Errorf("empty directory error = %v", res.err)
	}
}

func TestTextureExportAllUppercaseExtension(t *testing.T) {
	dir := t.TempDir()
	argb := texturePixels(4)
	writeFile(t, filepath.Join(dir, "b.XBX"), xprTexture(4, argb))
	res := runCLI(t, "texture", "export-all", dir)
	if res.err != nil {
		t.Fatal(res.err)
	}
	if want := "  b.XBX → b.png (4x4 A8R8G8B8)\n"; res.out != want {
		t.Errorf("stdout\n%q\nwant\n%q", res.out, want)
	}
	checkPNG(t, filepath.Join(dir, "b.png"), 4, argb)
}

func testFont() []byte {
	return xtfFont("TestFont", 32, [][2]uint16{{0x41, 2}, {0xC0, 1}}, []testGlyph{
		{metrics: [6]float32{10, 12, 1, 2, 11, 0}, indices: []uint16{0, 1, 2}, verts: [][2]float32{{0, 0}, {1, 0}, {0, 1}}},
		{metrics: [6]float32{5, 5, 0, 0, 9, 0}},
		{metrics: [6]float32{7, 9, 0.5, 1.5, 8, 0}, indices: []uint16{0, 1, 2, 0, 2, 3}, verts: [][2]float32{{0, 0}, {2, 0}, {2, 2}, {0, 2}}},
	})
}

func TestFontInfo(t *testing.T) {
	data := testFont()
	path := writeFile(t, filepath.Join(t.TempDir(), "Xbox.xtf"), data)
	res := runCLI(t, "font", "info", path)
	if res.err != nil {
		t.Fatal(res.err)
	}
	want := "XTF Font: " + path + "\n" +
		"  Name:         TestFont\n" +
		"  Cell Height:  32\n" +
		"  Glyph Count:  3\n" +
		"  Ranges:       2\n" +
		fmt.Sprintf("  File Size:    %d bytes\n", len(data)) +
		"\n" +
		"  Glyphs with mesh data: 2\n" +
		"\n" +
		"  Unicode Ranges:\n" +
		"    U+0041..U+0042 (2) Basic Latin\n" +
		"    U+00C0..U+00C0 (1) Latin-1 Supplement\n"
	if res.out != want {
		t.Fatalf("stdout\n%q\nwant\n%q", res.out, want)
	}
	bad := writeFile(t, filepath.Join(t.TempDir(), "bad.xtf"), make([]byte, 0x40))
	res = runCLI(t, "font", "info", bad)
	if res.err == nil || res.err.Error() != "font: invalid magic (expected XTF0)" {
		t.Errorf("bad font error = %v", res.err)
	}
}

func TestFontExport(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, filepath.Join(dir, "Xbox.xtf"), testFont())
	res := runCLI(t, "font", "export", path)
	if res.err != nil {
		t.Fatal(res.err)
	}
	glyph := func(kv ...interface{}) map[string]interface{} {
		m := map[string]interface{}{}
		for i := 0; i < len(kv); i += 2 {
			m[kv[i].(string)] = kv[i+1]
		}
		return m
	}
	want := map[string]interface{}{
		"name":   "TestFont",
		"height": 32.0,
		"glyphs": map[string]interface{}{
			"A": glyph("i", 0.0, "adv", 11.0, "bb", []interface{}{10.0, 12.0}, "org", []interface{}{1.0, 2.0},
				"idx", []interface{}{0.0, 1.0, 2.0}, "verts", []interface{}{0.0, 0.0, 1.0, 0.0, 0.0, 1.0}),
			"B": glyph("i", 1.0, "adv", 9.0),
			"À": glyph("i", 2.0, "adv", 8.0, "bb", []interface{}{7.0, 9.0}, "org", []interface{}{0.5, 1.5},
				"idx", []interface{}{0.0, 1.0, 2.0, 0.0, 2.0, 3.0}, "verts", []interface{}{0.0, 0.0, 2.0, 0.0, 2.0, 2.0, 0.0, 2.0}),
		},
	}
	var got interface{}
	if err := json.Unmarshal(readFile(t, filepath.Join(dir, "Xbox.json")), &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("json\n got %#v\nwant %#v", got, want)
	}
	if !strings.Contains(res.errOut, "Exported font JSON to "+filepath.Join(dir, "Xbox.json")) {
		t.Errorf("stderr %q", res.errOut)
	}
	custom := filepath.Join(t.TempDir(), "glyphs.json")
	res = runCLI(t, "font", "export", "-o", custom, path)
	if res.err != nil {
		t.Fatal(res.err)
	}
	var again interface{}
	if err := json.Unmarshal(readFile(t, custom), &again); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(again, want) {
		t.Fatalf("custom output json %#v", again)
	}
}
