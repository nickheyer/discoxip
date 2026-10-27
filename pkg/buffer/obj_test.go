package buffer

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

var objVerts = []Vertex{
	{Pos: [3]float32{1, 2, 3}},
	{Pos: [3]float32{-1, 0.5, 0}},
	{Pos: [3]float32{0, 0, 1}},
}

var objNormals = [][3]float32{{0, 0, 1}, {0, 1, 0}, {1, 0, 0}}

var objUVs = [][2]float32{{0, 0}, {1, 0}, {0.5, 1}}

var objPositionLines = []string{
	"v 1.000000 2.000000 3.000000",
	"v -1.000000 0.500000 0.000000",
	"v 0.000000 0.000000 1.000000",
}

var objNormalLines = []string{
	"vn 0.000000 0.000000 1.000000",
	"vn 0.000000 1.000000 0.000000",
	"vn 1.000000 0.000000 0.000000",
}

var objUVLines = []string{
	"vt 0.000000 0.000000",
	"vt 1.000000 0.000000",
	"vt 0.500000 1.000000",
}

// Copies the shared vertices and attaches normals or UVs on request
func objVertices(withNormals, withUVs bool) []Vertex {
	verts := append([]Vertex(nil), objVerts...)
	for i := range verts {
		if withNormals {
			verts[i].Normal = objNormals[i]
		}
		if withUVs {
			verts[i].UV = objUVs[i]
		}
	}
	return verts
}

// Runs the exporter and splits its output into lines without the trailing newline
func objLines(t *testing.T, verts []Vertex, indices []uint16) []string {
	t.Helper()
	var buf bytes.Buffer
	if err := ExportOBJ(&buf, verts, indices); err != nil {
		t.Fatalf("ExportOBJ: %v", err)
	}
	out := buf.String()
	if out != "" && !strings.HasSuffix(out, "\n") {
		t.Fatalf("output does not end with a newline, %q", out)
	}
	return strings.Split(strings.TrimSuffix(out, "\n"), "\n")
}

func assertLines(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d lines, want %d\ngot:\n%s\nwant:\n%s", len(got), len(want), strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d = %q, want %q", i+1, got[i], want[i])
		}
	}
}

func join(parts ...[]string) []string {
	var out []string
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

func TestExportOBJPositionsOnly(t *testing.T) {
	got := objLines(t, objVertices(false, false), []uint16{0, 1, 2})
	assertLines(t, got, join(objPositionLines, []string{"f 1 2 3"}))
}

func TestExportOBJWithNormals(t *testing.T) {
	got := objLines(t, objVertices(true, false), []uint16{0, 1, 2, 2, 1, 0})
	assertLines(t, got, join(objPositionLines, objNormalLines, []string{"f 1//1 2//2 3//3", "f 3//3 2//2 1//1"}))
}

func TestExportOBJWithUVs(t *testing.T) {
	got := objLines(t, objVertices(false, true), []uint16{2, 0, 1})
	assertLines(t, got, join(objPositionLines, objUVLines, []string{"f 3/3 1/1 2/2"}))
}

func TestExportOBJWithNormalsAndUVs(t *testing.T) {
	got := objLines(t, objVertices(true, true), []uint16{0, 1, 2})
	assertLines(t, got, join(objPositionLines, objNormalLines, objUVLines, []string{"f 1/1/1 2/2/2 3/3/3"}))
}

func TestExportOBJSingleNonZeroAttributeEnablesSection(t *testing.T) {
	verts := objVertices(false, false)
	verts[2].Normal = [3]float32{0, 0, -1}
	verts[1].UV = [2]float32{0, 0.25}
	got := objLines(t, verts, []uint16{0, 1, 2})
	want := join(objPositionLines,
		[]string{"vn 0.000000 0.000000 0.000000", "vn 0.000000 0.000000 0.000000", "vn 0.000000 0.000000 -1.000000"},
		[]string{"vt 0.000000 0.000000", "vt 0.000000 0.250000", "vt 0.000000 0.000000"},
		[]string{"f 1/1/1 2/2/2 3/3/3"})
	assertLines(t, got, want)
}

func TestExportOBJIgnoresTrailingIndices(t *testing.T) {
	got := objLines(t, objVertices(false, false), []uint16{0, 1, 2, 1, 2})
	assertLines(t, got, join(objPositionLines, []string{"f 1 2 3"}))
	got = objLines(t, objVertices(false, false), []uint16{1})
	assertLines(t, got, objPositionLines)
	got = objLines(t, objVertices(false, false), nil)
	assertLines(t, got, objPositionLines)
}

func TestExportOBJEmpty(t *testing.T) {
	var buf bytes.Buffer
	if err := ExportOBJ(&buf, nil, nil); err != nil {
		t.Fatalf("ExportOBJ: %v", err)
	}
	if buf.Len() != 0 {
		t.Errorf("output = %q, want empty", buf.String())
	}
}

type failingWriter struct{ err error }

func (f failingWriter) Write([]byte) (int, error) { return 0, f.err }

func TestExportOBJWriteError(t *testing.T) {
	wantErr := errors.New("disk full")
	cases := []struct {
		name  string
		verts []Vertex
	}{
		{"positions", objVertices(false, false)},
		{"normals", objVertices(true, false)},
		{"uvs", objVertices(false, true)},
	}
	for _, c := range cases {
		if err := ExportOBJ(failingWriter{err: wantErr}, c.verts, []uint16{0, 1, 2}); !errors.Is(err, wantErr) {
			t.Errorf("%s: err = %v, want %v", c.name, err, wantErr)
		}
	}
	if err := ExportOBJ(failingWriter{err: wantErr}, nil, []uint16{0, 1, 2}); !errors.Is(err, wantErr) {
		t.Errorf("faces only: err = %v, want %v", err, wantErr)
	}
}
