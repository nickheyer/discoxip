package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/nickheyer/discoxip/pkg/xip"
)

func TestVersion(t *testing.T) {
	oldVersion, oldCommit := Version, Commit
	Version, Commit = "1.2.3", "abc123"
	t.Cleanup(func() { Version, Commit = oldVersion, oldCommit })
	res := runCLI(t, "version")
	if res.err != nil {
		t.Fatal(res.err)
	}
	if res.out != "discoxip 1.2.3 (commit abc123)\n" {
		t.Fatalf("stdout %q", res.out)
	}
}

func TestInfoListsArchive(t *testing.T) {
	xpr := xprTexture(4, texturePixels(4))
	data := archiveBytes(t, []xip.WriteEntry{
		entry("scene.xap", xip.FileTypeRegular, []byte("hello")),
		entry("~0.vb", xip.FileTypeVB, vbFile(2, 0x112, 4)),
		entry("tex/a.xbx", xip.FileTypeDir, xpr),
		entry("~0.ib", xip.FileTypeIB, ibFile(0, 1, 2)),
		entry("box.xm", xip.FileTypeMesh, nil),
	}, map[string]meshPatch{"box.xm": {pool: 1, indexStart: 16, tris: 7}})
	path := writeFile(t, filepath.Join(t.TempDir(), "menu.xip"), data)
	res := runCLI(t, "info", path)
	if res.err != nil {
		t.Fatal(res.err)
	}
	lines := strings.Split(res.out, "\n")
	wantHead := []string{
		"XIP Archive: " + path,
		"  Files:       5",
		"  Names:       5",
		"  Data Offset: 0x9C",
		fmt.Sprintf("  Data Size:   %d bytes", 5+len(xpr)+6+16),
		"",
	}
	if len(lines) < len(wantHead) || !reflect.DeepEqual(lines[:len(wantHead)], wantHead) {
		t.Fatalf("header lines %q, want %q", lines, wantHead)
	}
	wantRows := [][]string{
		{"TYPE", "SIZE", "OFFSET", "NAME"},
		{"----", "----", "------", "----"},
		{"mesh", "pool=~1", "idx=16", "tris=7", "box.xm"},
		{"file", "5", "0x0", "scene.xap"},
		{"resource", fmt.Sprint(len(xpr)), "0x5", "tex/a.xbx"},
		{"ib", "6", fmt.Sprintf("0x%X", 5+len(xpr)), "~0.ib"},
		{"vb", "16", fmt.Sprintf("0x%X", 11+len(xpr)), "~0.vb"},
	}
	var rows [][]string
	for _, line := range lines[len(wantHead):] {
		if line != "" {
			rows = append(rows, strings.Fields(line))
		}
	}
	if !reflect.DeepEqual(rows, wantRows) {
		t.Fatalf("rows %q, want %q", rows, wantRows)
	}
}

func TestInfoErrors(t *testing.T) {
	res := runCLI(t, "info")
	if res.err == nil || res.err.Error() != "accepts 1 arg(s), received 0" {
		t.Errorf("no argument error = %v", res.err)
	}
	path := writeFile(t, filepath.Join(t.TempDir(), "bad.xip"), bytes.Repeat([]byte("x"), 32))
	res = runCLI(t, "info", path)
	if res.err != xip.ErrInvalidMagic {
		t.Errorf("bad archive error = %v", res.err)
	}
}

type manifestEntry struct {
	Pool       string `json:"pool"`
	IndexStart int    `json:"index_start"`
	TriCount   int    `json:"tri_count"`
	Archive    string `json:"archive"`
}

func readManifest(t *testing.T, path string) map[string]manifestEntry {
	t.Helper()
	var m map[string]manifestEntry
	if err := json.Unmarshal(readFile(t, path), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func checkPNG(t *testing.T, path string, size int, argb []uint32) {
	t.Helper()
	f, err := os.Open(path)
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
		t.Fatalf("%s decodes to %T, want NRGBA", path, decoded)
	}
	if img.Bounds().Dx() != size || img.Bounds().Dy() != size {
		t.Fatalf("%s is %v, want %dx%d", path, img.Bounds(), size, size)
	}
	if want := nrgbaPixels(argb); !bytes.Equal(img.Pix, want) {
		t.Fatalf("%s pixels % x, want % x", path, img.Pix, want)
	}
}

// Writes a flat archive of a scene, a texture, one buffer pool and a mesh record to the path
func flatArchive(t *testing.T, path string) []byte {
	t.Helper()
	xpr := xprTexture(4, texturePixels(4))
	data := archiveBytes(t, []xip.WriteEntry{
		entry("scene.xap", xip.FileTypeRegular, []byte("hello")),
		entry("tex.xbx", xip.FileTypeDir, xpr),
		entry("~0.vb", xip.FileTypeVB, vbFile(2, 0x112, 4)),
		entry("~0.ib", xip.FileTypeIB, ibFile(0, 1, 2)),
		entry("box.xm", xip.FileTypeMesh, nil),
	}, map[string]meshPatch{"box.xm": {pool: 1, indexStart: 16, tris: 7}})
	writeFile(t, path, data)
	return xpr
}

func checkExtracted(t *testing.T, dir, prefix string, xpr []byte) {
	t.Helper()
	pool := "~0"
	scene := "scene.xap"
	if prefix != "" {
		pool = "~" + prefix + "_0"
		scene = prefix + "_scene.xap"
	}
	for name, want := range map[string][]byte{
		scene:        []byte("hello"),
		"tex.xbx":    xpr,
		pool + ".vb": vbFile(2, 0x112, 4),
		pool + ".ib": ibFile(0, 1, 2),
	} {
		if got := readFile(t, filepath.Join(dir, name)); !bytes.Equal(got, want) {
			t.Errorf("%s holds % x, want % x", name, got, want)
		}
	}
	checkPNG(t, filepath.Join(dir, "tex.png"), 4, texturePixels(4))
	key := "box.xm"
	if prefix != "" {
		key = prefix + ":box.xm"
	}
	meshPool := "~1"
	if prefix != "" {
		meshPool = "~" + prefix + "_1"
	}
	want := map[string]manifestEntry{key: {Pool: meshPool, IndexStart: 16, TriCount: 7, Archive: prefix}}
	if got := readManifest(t, filepath.Join(dir, "_meshes.json")); !reflect.DeepEqual(got, want) {
		t.Errorf("manifest %+v, want %+v", got, want)
	}
	if _, err := os.Stat(filepath.Join(dir, "box.xm")); !os.IsNotExist(err) {
		t.Errorf("mesh record was written as a file, stat err %v", err)
	}
}

func TestExtractFileToDefaultDirectory(t *testing.T) {
	root := t.TempDir()
	xpr := flatArchive(t, filepath.Join(root, "menu.xip"))
	res := runCLI(t, "extract", filepath.Join(root, "menu.xip"))
	if res.err != nil {
		t.Fatal(res.err)
	}
	if res.out != "" {
		t.Errorf("stdout %q without the verbose flag", res.out)
	}
	checkExtracted(t, filepath.Join(root, "out"), "", xpr)
}

func TestExtractFileVerboseToOutput(t *testing.T) {
	root := t.TempDir()
	xpr := flatArchive(t, filepath.Join(root, "menu.xip"))
	out := filepath.Join(t.TempDir(), "custom")
	res := runCLI(t, "extract", "-v", "-o", out, filepath.Join(root, "menu.xip"))
	if res.err != nil {
		t.Fatal(res.err)
	}
	want := "  box.xm → pool ~1, index 16, 7 tris\n" +
		"  scene.xap (5 bytes)\n" +
		fmt.Sprintf("  tex.xbx (%d bytes)\n", len(xpr)) +
		"  tex.xbx → tex.png (4x4 A8R8G8B8)\n" +
		"  ~0.ib (6 bytes)\n" +
		"  ~0.vb (16 bytes)\n" +
		"  _meshes.json (1 mesh entries total)\n"
	if res.out != want {
		t.Errorf("stdout\n%q\nwant\n%q", res.out, want)
	}
	checkExtracted(t, out, "", xpr)
	if _, err := os.Stat(filepath.Join(root, "out")); !os.IsNotExist(err) {
		t.Errorf("default output directory was created, stat err %v", err)
	}
}

func TestExtractDirectoryPrefixesArchives(t *testing.T) {
	root := t.TempDir()
	xpr := flatArchive(t, filepath.Join(root, "menu.xip"))
	flatArchive(t, filepath.Join(root, "sub", "panel.xip"))
	writeFile(t, filepath.Join(root, "Audio", "x.wav"), []byte("wave"))
	writeFile(t, filepath.Join(root, "notes.txt"), []byte("n"))
	res := runCLI(t, "extract", root)
	if res.err != nil {
		t.Fatal(res.err)
	}
	checkExtracted(t, filepath.Join(root, "out"), "menu", xpr)
	if got := readFile(t, filepath.Join(root, "out", "Audio", "x.wav")); string(got) != "wave" {
		t.Errorf("Audio/x.wav holds %q", got)
	}
	if _, err := os.Stat(filepath.Join(root, "sub", "out")); !os.IsNotExist(err) {
		t.Errorf("subdirectory was extracted without the recursive flag, stat err %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "out", "notes.txt")); !os.IsNotExist(err) {
		t.Errorf("a loose file was copied, stat err %v", err)
	}
}

func TestExtractDirectoryRecursive(t *testing.T) {
	root := t.TempDir()
	xpr := flatArchive(t, filepath.Join(root, "menu.xip"))
	flatArchive(t, filepath.Join(root, "sub", "panel.xip"))
	writeFile(t, filepath.Join(root, "sub", "Audio", "y.wav"), []byte("sub wave"))
	out := filepath.Join(t.TempDir(), "out")
	res := runCLI(t, "extract", "-r", "-o", out, root)
	if res.err != nil {
		t.Fatal(res.err)
	}
	checkExtracted(t, out, "menu", xpr)
	checkExtracted(t, filepath.Join(out, "sub"), "panel", xpr)
	if got := readFile(t, filepath.Join(out, "sub", "Audio", "y.wav")); string(got) != "sub wave" {
		t.Errorf("sub/Audio/y.wav holds %q", got)
	}
	for _, rel := range []string{"out", filepath.Join("sub", "out")} {
		if _, err := os.Stat(filepath.Join(root, rel)); !os.IsNotExist(err) {
			t.Errorf("%s was created beside the source, stat err %v", rel, err)
		}
	}
}

func TestExtractRecursiveSingleGroupUsesOutputDirectly(t *testing.T) {
	root := t.TempDir()
	xpr := flatArchive(t, filepath.Join(root, "deep", "menu.xip"))
	out := filepath.Join(t.TempDir(), "out")
	res := runCLI(t, "extract", "-r", "-o", out, root)
	if res.err != nil {
		t.Fatal(res.err)
	}
	checkExtracted(t, out, "menu", xpr)
}

func TestExtractErrors(t *testing.T) {
	root := t.TempDir()
	res := runCLI(t, "extract", root)
	if res.err == nil || res.err.Error() != "no .xip files found in "+root {
		t.Errorf("empty directory error = %v", res.err)
	}
	res = runCLI(t, "extract", filepath.Join(root, "absent.xip"))
	if res.err == nil || !os.IsNotExist(res.err) {
		t.Errorf("missing file error = %v", res.err)
	}
}

func TestPackRoundTrip(t *testing.T) {
	src := t.TempDir()
	files := map[string][]byte{
		"a.xap":          []byte("AAA"),
		"~0.vb":          []byte("VB1"),
		"~0.ib":          []byte("IB"),
		"tex/t.xbx":      []byte("T"),
		"tex/notes.txt":  []byte("n"),
		"Deep/Sub/x.XBX": []byte("deep"),
	}
	for name, body := range files {
		writeFile(t, filepath.Join(src, filepath.FromSlash(name)), body)
	}
	out := filepath.Join(t.TempDir(), "packed.xip")
	res := runCLI(t, "pack", "-o", out, src)
	if res.err != nil {
		t.Fatal(res.err)
	}
	if !strings.Contains(res.errOut, "Packed 6 files into "+out) {
		t.Errorf("stderr %q", res.errOut)
	}
	r, err := xip.Open(out)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	type want struct {
		name string
		typ  xip.FileType
		size uint32
	}
	wants := []want{
		{"Deep/Sub/x.XBX", 2, 4},
		{"a.xap", 0, 3},
		{"tex/notes.txt", 0, 1},
		{"tex/t.xbx", 2, 1},
		{"~0.ib", 5, 2},
		{"~0.vb", 6, 3},
	}
	entries := r.Entries()
	if len(entries) != len(wants) {
		t.Fatalf("entries %+v, want %d", entries, len(wants))
	}
	for i, w := range wants {
		e := entries[i]
		if e.Name != w.name || e.Type != w.typ || e.Size != w.size {
			t.Errorf("entry %d = %+v, want %+v", i, e, w)
		}
		if body, ok := files[w.name]; ok {
			got, err := io.ReadAll(r.OpenFile(e))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, body) {
				t.Errorf("%s holds %q, want %q", w.name, got, body)
			}
		}
	}
	if h := r.Header(); h.NumFiles != 6 || h.NumNames != 6 || h.DataSize != 14 {
		t.Errorf("header %+v", h)
	}
}

func TestPackErrors(t *testing.T) {
	src := t.TempDir()
	res := runCLI(t, "pack", src)
	if res.err == nil || res.err.Error() != `required flag(s) "output" not set` {
		t.Errorf("missing output error = %v", res.err)
	}
	out := filepath.Join(t.TempDir(), "x.xip")
	res = runCLI(t, "pack", "-o", out, src)
	if res.err == nil || res.err.Error() != "pack: no files found in "+src {
		t.Errorf("empty directory error = %v", res.err)
	}
	file := writeFile(t, filepath.Join(src, "f.txt"), []byte("f"))
	res = runCLI(t, "pack", "-o", out, file)
	if res.err == nil || res.err.Error() != "pack: "+file+" is not a directory" {
		t.Errorf("file source error = %v", res.err)
	}
}

func TestFileTypeForExt(t *testing.T) {
	tests := []struct {
		ext  string
		want xip.FileType
	}{
		{".vb", 6},
		{".VB", 6},
		{".ib", 5},
		{".Ib", 5},
		{".xbx", 2},
		{".XBX", 2},
		{".xap", 0},
		{".xm", 0},
		{"", 0},
		{".txt", 0},
	}
	for _, tc := range tests {
		if got := fileTypeForExt(tc.ext); got != tc.want {
			t.Errorf("fileTypeForExt(%q) = %d, want %d", tc.ext, got, tc.want)
		}
	}
}
