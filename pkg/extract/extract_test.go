package extract

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/nickheyer/discoxip/pkg/xip"
)

type rec struct {
	name string
	typ  uint32
	off  uint32
	size uint32
	body []byte
}

func fileRec(name string, typ uint32, body string) rec {
	return rec{name: name, typ: typ, body: []byte(body)}
}

func meshRec(name string, pool, start, tris int) rec {
	return rec{name: name, typ: 4, off: uint32(pool)<<24 | uint32(start), size: uint32(tris)}
}

// Lays out header, 16 byte records, 4 byte name records, padded name block and bodies in order
func buildArchive(recs []rec) []byte {
	var tbl, idx, names, data []byte
	for i, r := range recs {
		off, size := r.off, r.size
		if r.body != nil {
			off, size = uint32(len(data)), uint32(len(r.body))
			data = append(data, r.body...)
		}
		for _, v := range []uint32{off, size, r.typ, 0} {
			tbl = binary.LittleEndian.AppendUint32(tbl, v)
		}
		idx = binary.LittleEndian.AppendUint16(idx, uint16(i))
		idx = binary.LittleEndian.AppendUint16(idx, uint16(len(names)))
		names = append(names, strings.ReplaceAll(r.name, "/", "\\")...)
		names = append(names, 0)
	}
	for len(names)%4 != 0 {
		names = append(names, 0)
	}
	out := []byte("XIP0")
	out = binary.LittleEndian.AppendUint32(out, uint32(16+len(tbl)+len(idx)+len(names)))
	out = binary.LittleEndian.AppendUint16(out, uint16(len(recs)))
	out = binary.LittleEndian.AppendUint16(out, uint16(len(recs)))
	out = binary.LittleEndian.AppendUint32(out, uint32(len(data)))
	out = append(out, tbl...)
	out = append(out, idx...)
	out = append(out, names...)
	return append(out, data...)
}

func openArchive(t *testing.T, recs []rec) *xip.Reader {
	t.Helper()
	return openBytes(t, buildArchive(recs))
}

func openBytes(t *testing.T, buf []byte) *xip.Reader {
	t.Helper()
	r, err := xip.NewReader(bytes.NewReader(buf), int64(len(buf)))
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}
	return r
}

var texturePixels = []color.NRGBA{
	{R: 255, A: 255},
	{G: 255, A: 255},
	{B: 255, A: 255},
	{R: 255, G: 255, B: 255, A: 128},
}

// Encodes the 2x2 pixel table as an XPR0 A8R8G8B8 texture behind a 32 byte header
func xprTexture() string {
	b := []byte("XPR0")
	b = binary.LittleEndian.AppendUint32(b, 32+16)
	b = binary.LittleEndian.AppendUint32(b, 32)
	b = binary.LittleEndian.AppendUint16(b, 1)
	b = binary.LittleEndian.AppendUint16(b, 4)
	b = binary.LittleEndian.AppendUint32(b, 0)
	b = binary.LittleEndian.AppendUint32(b, 0)
	b = binary.LittleEndian.AppendUint32(b, 0x06<<8|1<<16|1<<20|1<<24)
	b = binary.LittleEndian.AppendUint32(b, 0)
	for _, p := range texturePixels {
		b = append(b, p.B, p.G, p.R, p.A)
	}
	return string(b)
}

func checkPNG(t *testing.T, path string) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open png: %v", err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	if img.Bounds() != image.Rect(0, 0, 2, 2) {
		t.Fatalf("%s bounds %v, want 2x2", path, img.Bounds())
	}
	for i, want := range texturePixels {
		got := color.NRGBAModel.Convert(img.At(i%2, i/2)).(color.NRGBA)
		if got != want {
			t.Errorf("%s pixel %d = %+v, want %+v", path, i, got, want)
		}
	}
}

func listFiles(t *testing.T, dir string) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		files = append(files, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", dir, err)
	}
	slices.Sort(files)
	return files
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func readManifest(t *testing.T, dir string) map[string]MeshManifestEntry {
	t.Helper()
	var m map[string]MeshManifestEntry
	raw := readFile(t, filepath.Join(dir, "_meshes.json"))
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("manifest %q: %v", raw, err)
	}
	return m
}

func checkFiles(t *testing.T, dir string, want []string) {
	t.Helper()
	if got := listFiles(t, dir); !slices.Equal(got, want) {
		t.Errorf("files %v, want %v", got, want)
	}
}

func checkContents(t *testing.T, dir string, want map[string]string) {
	t.Helper()
	for name, body := range want {
		if got := readFile(t, filepath.Join(dir, name)); got != body {
			t.Errorf("%s = %q, want %q", name, got, body)
		}
	}
}

func checkManifest(t *testing.T, dir string, want map[string]MeshManifestEntry) {
	t.Helper()
	if got := readManifest(t, dir); !maps.Equal(got, want) {
		t.Errorf("manifest %+v, want %+v", got, want)
	}
}

func captureOutput(t *testing.T, fn func()) (string, string) {
	t.Helper()
	oldOut, oldErr := os.Stdout, os.Stderr
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	var outBuf, errBuf bytes.Buffer
	done := make(chan struct{}, 2)
	go func() { io.Copy(&outBuf, outR); done <- struct{}{} }()
	go func() { io.Copy(&errBuf, errR); done <- struct{}{} }()
	os.Stdout, os.Stderr = outW, errW
	fn()
	os.Stdout, os.Stderr = oldOut, oldErr
	outW.Close()
	errW.Close()
	<-done
	<-done
	return outBuf.String(), errBuf.String()
}

func TestArchiveExtractsEntriesByType(t *testing.T) {
	r := openArchive(t, []rec{
		fileRec("scene.xap", 0, "scene"),
		fileRec("notes.txt", 1, "notes"),
		fileRec("blob.bin", 3, "blob"),
		fileRec("tex/a.xbx", 2, xprTexture()),
		fileRec("bad.xbx", 2, "nope"),
		fileRec("~0.vb", 6, "vertices"),
		fileRec("~1.ib", 5, "indices"),
		meshRec("meshA", 0, 16, 7),
		meshRec("meshB", 1, 0x123456, 99),
	})
	out := t.TempDir()
	if err := Archive(r, Options{OutputDir: out}); err != nil {
		t.Fatalf("Archive: %v", err)
	}
	checkFiles(t, out, []string{
		"_meshes.json", "bad.xbx", "blob.bin", "notes.txt", "scene.xap",
		"tex/a.png", "tex/a.xbx", "~0.vb", "~1.ib",
	})
	checkContents(t, out, map[string]string{
		"scene.xap": "scene",
		"notes.txt": "notes",
		"blob.bin":  "blob",
		"tex/a.xbx": xprTexture(),
		"bad.xbx":   "nope",
		"~0.vb":     "vertices",
		"~1.ib":     "indices",
	})
	checkPNG(t, filepath.Join(out, "tex", "a.png"))
	checkManifest(t, out, map[string]MeshManifestEntry{
		"meshA": {Pool: "~0", IndexStart: 16, TriCount: 7},
		"meshB": {Pool: "~1", IndexStart: 0x123456, TriCount: 99},
	})
}

func TestArchiveManifestJSON(t *testing.T) {
	tests := []struct {
		name    string
		archive string
		want    string
	}{
		{"plain", "", `{
  "meshA": {
    "pool": "~0",
    "index_start": 16,
    "tri_count": 7
  }
}`},
		{"prefixed", "lobby", `{
  "lobby:meshA": {
    "pool": "~lobby_0",
    "index_start": 16,
    "tri_count": 7,
    "archive": "lobby"
  }
}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out := t.TempDir()
			r := openArchive(t, []rec{meshRec("meshA", 0, 16, 7)})
			if err := Archive(r, Options{OutputDir: out, ArchiveName: tc.archive}); err != nil {
				t.Fatalf("Archive: %v", err)
			}
			checkFiles(t, out, []string{"_meshes.json"})
			if got := readFile(t, filepath.Join(out, "_meshes.json")); got != tc.want {
				t.Errorf("manifest\n%s\nwant\n%s", got, tc.want)
			}
		})
	}
}

func TestArchivePrefixesNamesWithArchiveName(t *testing.T) {
	r := openArchive(t, []rec{
		fileRec("scene.xap", 0, "scene"),
		fileRec("Menu.XAP", 0, "menu"),
		fileRec("~0.vb", 6, "vertices"),
		fileRec("~1.ib", 5, "indices"),
		fileRec("tex/a.xbx", 2, "nope"),
		fileRec("notes.txt", 1, "notes"),
		meshRec("meshA", 1, 16, 7),
	})
	out := t.TempDir()
	if err := Archive(r, Options{OutputDir: out, ArchiveName: "lobby"}); err != nil {
		t.Fatalf("Archive: %v", err)
	}
	checkFiles(t, out, []string{
		"_meshes.json", "lobby_Menu.XAP", "lobby_scene.xap", "notes.txt",
		"tex/a.xbx", "~lobby_0.vb", "~lobby_1.ib",
	})
	checkContents(t, out, map[string]string{
		"lobby_scene.xap": "scene",
		"lobby_Menu.XAP":  "menu",
		"~lobby_0.vb":     "vertices",
		"~lobby_1.ib":     "indices",
		"tex/a.xbx":       "nope",
		"notes.txt":       "notes",
	})
	want := map[string]MeshManifestEntry{
		"lobby:meshA": {Pool: "~lobby_1", IndexStart: 16, TriCount: 7, Archive: "lobby"},
	}
	checkManifest(t, out, want)
	if got := readFile(t, filepath.Join(out, want["lobby:meshA"].Pool+".ib")); got != "indices" {
		t.Errorf("manifest pool file holds %q, want %q", got, "indices")
	}
}

func TestArchiveMergesManifestsAcrossArchives(t *testing.T) {
	out := t.TempDir()
	a := openArchive(t, []rec{
		fileRec("scene.xap", 0, "A"),
		fileRec("~0.vb", 6, "va"),
		meshRec("m1", 0, 10, 3),
		meshRec("m2", 1, 20, 4),
	})
	b := openArchive(t, []rec{
		fileRec("scene.xap", 0, "B"),
		fileRec("~0.vb", 6, "vb"),
		meshRec("m2", 0, 5, 6),
		meshRec("m3", 2, 0, 1),
	})
	if err := Archive(a, Options{OutputDir: out, ArchiveName: "a"}); err != nil {
		t.Fatalf("Archive a: %v", err)
	}
	if err := Archive(b, Options{OutputDir: out, ArchiveName: "b"}); err != nil {
		t.Fatalf("Archive b: %v", err)
	}
	checkFiles(t, out, []string{"_meshes.json", "a_scene.xap", "b_scene.xap", "~a_0.vb", "~b_0.vb"})
	checkContents(t, out, map[string]string{
		"a_scene.xap": "A",
		"b_scene.xap": "B",
		"~a_0.vb":     "va",
		"~b_0.vb":     "vb",
	})
	checkManifest(t, out, map[string]MeshManifestEntry{
		"a:m1": {Pool: "~a_0", IndexStart: 10, TriCount: 3, Archive: "a"},
		"a:m2": {Pool: "~a_1", IndexStart: 20, TriCount: 4, Archive: "a"},
		"b:m2": {Pool: "~b_0", IndexStart: 5, TriCount: 6, Archive: "b"},
		"b:m3": {Pool: "~b_2", IndexStart: 0, TriCount: 1, Archive: "b"},
	})
}

func TestArchiveManifestCollisionKeepsNewest(t *testing.T) {
	out := t.TempDir()
	first := openArchive(t, []rec{meshRec("m", 0, 1, 2), meshRec("only_first", 3, 4, 5)})
	second := openArchive(t, []rec{meshRec("m", 1, 3, 4)})
	if err := Archive(first, Options{OutputDir: out}); err != nil {
		t.Fatalf("Archive first: %v", err)
	}
	if err := Archive(second, Options{OutputDir: out}); err != nil {
		t.Fatalf("Archive second: %v", err)
	}
	checkManifest(t, out, map[string]MeshManifestEntry{
		"m":          {Pool: "~1", IndexStart: 3, TriCount: 4},
		"only_first": {Pool: "~3", IndexStart: 4, TriCount: 5},
	})
}

func TestArchiveWithoutMeshesLeavesManifestAlone(t *testing.T) {
	r := openArchive(t, []rec{fileRec("a.txt", 0, "a")})
	out := t.TempDir()
	custom := "{\"keep\": {\"pool\": \"~9\"}}\n"
	if err := os.WriteFile(filepath.Join(out, "_meshes.json"), []byte(custom), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Archive(r, Options{OutputDir: out}); err != nil {
		t.Fatalf("Archive: %v", err)
	}
	checkFiles(t, out, []string{"_meshes.json", "a.txt"})
	if got := readFile(t, filepath.Join(out, "_meshes.json")); got != custom {
		t.Errorf("manifest rewritten to %q, want %q", got, custom)
	}
	fresh := t.TempDir()
	if err := Archive(r, Options{OutputDir: fresh}); err != nil {
		t.Fatalf("Archive: %v", err)
	}
	checkFiles(t, fresh, []string{"a.txt"})
}

func TestArchiveReplacesCorruptManifest(t *testing.T) {
	out := t.TempDir()
	if err := os.WriteFile(filepath.Join(out, "_meshes.json"), []byte("not json{"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := openArchive(t, []rec{meshRec("m", 0, 1, 2)})
	if err := Archive(r, Options{OutputDir: out}); err != nil {
		t.Fatalf("Archive: %v", err)
	}
	checkManifest(t, out, map[string]MeshManifestEntry{"m": {Pool: "~0", IndexStart: 1, TriCount: 2}})
}

func TestArchiveResolvesOutputDirAgainstCwd(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	r := openArchive(t, []rec{fileRec("a.txt", 0, "a"), meshRec("m", 0, 1, 2)})
	if err := Archive(r, Options{}); err != nil {
		t.Fatalf("Archive: %v", err)
	}
	checkFiles(t, dir, []string{"_meshes.json", "a.txt"})
	checkContents(t, dir, map[string]string{"a.txt": "a"})
	if err := Archive(r, Options{OutputDir: "nested/out"}); err != nil {
		t.Fatalf("Archive: %v", err)
	}
	nested := filepath.Join(dir, "nested", "out")
	checkFiles(t, nested, []string{"_meshes.json", "a.txt"})
	checkManifest(t, nested, map[string]MeshManifestEntry{"m": {Pool: "~0", IndexStart: 1, TriCount: 2}})
}

func TestArchiveRejectsUnsafeNames(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{"../evil.txt", "path traversal rejected"},
		{"a/../../evil.txt", "path traversal rejected"},
		{"/abs/evil.txt", "absolute path rejected"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := openArchive(t, []rec{fileRec(tc.name, 0, "x")})
			out := t.TempDir()
			err := Archive(r, Options{OutputDir: out})
			if err == nil {
				t.Fatalf("Archive accepted %q", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), tc.name) {
				t.Errorf("error %q, want it to name %q and say %q", err, tc.name, tc.want)
			}
			checkFiles(t, out, nil)
			checkFiles(t, filepath.Dir(out), nil)
		})
	}
}

func TestArchiveAllOptionHasNoEffect(t *testing.T) {
	recs := []rec{fileRec("scene.xap", 0, "scene"), fileRec("~0.vb", 6, "v"), meshRec("m", 0, 1, 2)}
	for _, all := range []bool{false, true} {
		out := t.TempDir()
		if err := Archive(openArchive(t, recs), Options{OutputDir: out, All: all}); err != nil {
			t.Fatalf("All=%v: Archive: %v", all, err)
		}
		checkFiles(t, out, []string{"_meshes.json", "scene.xap", "~0.vb"})
		checkContents(t, out, map[string]string{"scene.xap": "scene", "~0.vb": "v"})
		checkManifest(t, out, map[string]MeshManifestEntry{"m": {Pool: "~0", IndexStart: 1, TriCount: 2}})
	}
}

func TestArchiveVerboseOutput(t *testing.T) {
	recs := []rec{
		fileRec("scene.xap", 0, "scene"),
		meshRec("meshA", 1, 16, 7),
		fileRec("bad.xbx", 2, "nope"),
		fileRec("tex/a.xbx", 2, xprTexture()),
	}
	var err error
	out := t.TempDir()
	stdout, stderr := captureOutput(t, func() {
		err = Archive(openArchive(t, recs), Options{OutputDir: out, ArchiveName: "v", Verbose: true})
	})
	if err != nil {
		t.Fatalf("Archive: %v", err)
	}
	wantOut := "  scene.xap (5 bytes)\n" +
		"  meshA \u2192 pool ~v_1, index 16, 7 tris\n" +
		"  bad.xbx (4 bytes)\n" +
		"  tex/a.xbx (48 bytes)\n" +
		"  a.xbx \u2192 a.png (2x2 A8R8G8B8)\n" +
		"  _meshes.json (1 mesh entries total)\n"
	if stdout != wantOut {
		t.Errorf("stdout\n%q\nwant\n%q", stdout, wantOut)
	}
	wantErr := "  warning: bad.xbx: texture: file is truncated\n"
	if stderr != wantErr {
		t.Errorf("stderr %q, want %q", stderr, wantErr)
	}
	quiet := t.TempDir()
	stdout, stderr = captureOutput(t, func() {
		err = Archive(openArchive(t, recs), Options{OutputDir: quiet, ArchiveName: "v"})
	})
	if err != nil {
		t.Fatalf("Archive: %v", err)
	}
	if stdout != "" || stderr != "" {
		t.Errorf("quiet run printed stdout %q stderr %q", stdout, stderr)
	}
}

func TestArchiveExtractsWriterArchive(t *testing.T) {
	var buf bytes.Buffer
	w := xip.NewWriter(&buf)
	w.Add(xip.WriteEntry{Name: "scene.xap", Type: xip.FileTypeRegular, Size: 5, Body: strings.NewReader("scene")})
	w.Add(xip.WriteEntry{Name: "~0.vb", Type: xip.FileTypeVB, Size: 8, Body: strings.NewReader("VBVBVBVB")})
	w.Add(xip.WriteEntry{Name: "~0.ib", Type: xip.FileTypeIB, Size: 6, Body: strings.NewReader("IBIBIB")})
	w.Add(xip.WriteEntry{Name: "a.xbx", Type: xip.FileTypeDir, Size: 48, Body: strings.NewReader(xprTexture())})
	if err := w.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	out := t.TempDir()
	if err := Archive(openBytes(t, buf.Bytes()), Options{OutputDir: out, ArchiveName: "pack"}); err != nil {
		t.Fatalf("Archive: %v", err)
	}
	checkFiles(t, out, []string{"a.png", "a.xbx", "pack_scene.xap", "~pack_0.ib", "~pack_0.vb"})
	checkContents(t, out, map[string]string{
		"a.xbx":          xprTexture(),
		"pack_scene.xap": "scene",
		"~pack_0.ib":     "IBIBIB",
		"~pack_0.vb":     "VBVBVBVB",
	})
	checkPNG(t, filepath.Join(out, "a.png"))
}

func TestArchiveExtractsWriterArchiveWithDirectories(t *testing.T) {
	var buf bytes.Buffer
	w := xip.NewWriter(&buf)
	w.Add(xip.WriteEntry{Name: "tex/a.bin", Type: xip.FileTypeRegular, Size: 3, Body: strings.NewReader("aaa")})
	w.Add(xip.WriteEntry{Name: "b.bin", Type: xip.FileTypeRegular, Size: 2, Body: strings.NewReader("bb")})
	if err := w.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	out := t.TempDir()
	if err := Archive(openBytes(t, buf.Bytes()), Options{OutputDir: out}); err != nil {
		t.Fatalf("Archive: %v", err)
	}
	checkFiles(t, out, []string{"b.bin", "tex/a.bin"})
	checkContents(t, out, map[string]string{"b.bin": "bb", "tex/a.bin": "aaa"})
}

func TestPoolFileName(t *testing.T) {
	tests := []struct {
		archive string
		pool    int
		want    string
	}{
		{"", 0, "~0"},
		{"", 12, "~12"},
		{"lobby", 1, "~lobby_1"},
		{"menu", 0, "~menu_0"},
	}
	for _, tc := range tests {
		if got := poolFileName(tc.archive, tc.pool); got != tc.want {
			t.Errorf("poolFileName(%q, %d) = %q, want %q", tc.archive, tc.pool, got, tc.want)
		}
	}
}

func TestRenamePoolFile(t *testing.T) {
	tests := []struct {
		archive string
		name    string
		want    string
	}{
		{"lobby", "~0.vb", "~lobby_0.vb"},
		{"lobby", "~12.ib", "~lobby_12.ib"},
		{"lobby", "pool.vb", "lobby_pool.vb"},
		{"x", "~0", "~x_0"},
	}
	for _, tc := range tests {
		if got := renamePoolFile(tc.archive, tc.name); got != tc.want {
			t.Errorf("renamePoolFile(%q, %q) = %q, want %q", tc.archive, tc.name, got, tc.want)
		}
	}
}

func TestSanitizePath(t *testing.T) {
	base := t.TempDir()
	tests := []struct {
		name    string
		want    string
		errText string
	}{
		{"a.txt", filepath.Join(base, "a.txt"), ""},
		{"dir/sub/a.txt", filepath.Join(base, "dir", "sub", "a.txt"), ""},
		{"./a.txt", filepath.Join(base, "a.txt"), ""},
		{"../a.txt", "", "path traversal rejected"},
		{"dir/../../a.txt", "", "path traversal rejected"},
		{"/abs/a.txt", "", "absolute path rejected"},
	}
	for _, tc := range tests {
		got, err := sanitizePath(base, tc.name)
		if tc.errText == "" {
			if err != nil {
				t.Errorf("sanitizePath(%q) error %v", tc.name, err)
			}
			if got != tc.want {
				t.Errorf("sanitizePath(%q) = %q, want %q", tc.name, got, tc.want)
			}
		} else {
			if err == nil || !strings.Contains(err.Error(), tc.errText) {
				t.Errorf("sanitizePath(%q) error %v, want %q", tc.name, err, tc.errText)
			}
			if got != "" {
				t.Errorf("sanitizePath(%q) = %q, want empty", tc.name, got)
			}
		}
	}
}

func TestMergeManifest(t *testing.T) {
	dir := t.TempDir()
	first := map[string]MeshManifestEntry{
		"keep":   {Pool: "~0", IndexStart: 1, TriCount: 2},
		"shared": {Pool: "~0", IndexStart: 3, TriCount: 4},
	}
	if err := mergeManifest(dir, first, false); err != nil {
		t.Fatalf("mergeManifest: %v", err)
	}
	checkManifest(t, dir, first)
	second := map[string]MeshManifestEntry{
		"shared": {Pool: "~x_1", IndexStart: 5, TriCount: 6, Archive: "x"},
		"new":    {Pool: "~x_2", IndexStart: 7, TriCount: 8, Archive: "x"},
	}
	if err := mergeManifest(dir, second, false); err != nil {
		t.Fatalf("mergeManifest: %v", err)
	}
	checkManifest(t, dir, map[string]MeshManifestEntry{
		"keep":   first["keep"],
		"shared": second["shared"],
		"new":    second["new"],
	})
	checkFiles(t, dir, []string{"_meshes.json"})
}

func TestExtractFileCreatesParentDirs(t *testing.T) {
	r := openArchive(t, []rec{fileRec("deep.bin", 0, "payload"), fileRec("t.xbx", 2, xprTexture())})
	dir := t.TempDir()
	dest := filepath.Join(dir, "x", "y", "deep.bin")
	if err := extractFile(r, r.Entries()[0], dest, false); err != nil {
		t.Fatalf("extractFile: %v", err)
	}
	if got := readFile(t, dest); got != "payload" {
		t.Errorf("extracted %q, want %q", got, "payload")
	}
	texDest := filepath.Join(dir, "img", "t.xbx")
	if err := extractFile(r, r.Entries()[1], texDest, false); err != nil {
		t.Fatalf("extractFile: %v", err)
	}
	checkFiles(t, dir, []string{"img/t.png", "img/t.xbx", "x/y/deep.bin"})
	checkPNG(t, filepath.Join(dir, "img", "t.png"))
}

func TestConvertTextureToPNG(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "good.xbx")
	if err := os.WriteFile(good, []byte(xprTexture()), 0o644); err != nil {
		t.Fatal(err)
	}
	convertTextureToPNG(good, false)
	checkPNG(t, filepath.Join(dir, "good.png"))
	upper := filepath.Join(dir, "UP.XBX")
	if err := os.WriteFile(upper, []byte(xprTexture()), 0o644); err != nil {
		t.Fatal(err)
	}
	convertTextureToPNG(upper, false)
	checkPNG(t, filepath.Join(dir, "UP.png"))
	bad := filepath.Join(dir, "bad.xbx")
	if err := os.WriteFile(bad, []byte("nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	convertTextureToPNG(bad, false)
	if _, err := os.Stat(filepath.Join(dir, "bad.png")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("bad.png stat error %v, want not exist", err)
	}
	checkFiles(t, dir, []string{"UP.XBX", "UP.png", "bad.xbx", "good.png", "good.xbx"})
}
