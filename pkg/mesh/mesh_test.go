package mesh

import (
	"bytes"
	"encoding/binary"
	"errors"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"testing/iotest"
)

func rgbaBinary() []byte {
	return bytes.Repeat([]byte{'A', 'B', 'C', 0xff}, 64)
}

func vertexBinary() []byte {
	var buf bytes.Buffer
	for _, f := range []float32{1, -0.5, 0.25, 1, 1, 0, 0.75, 2, 3} {
		binary.Write(&buf, binary.LittleEndian, math.Float32bits(f))
	}
	return buf.Bytes()
}

func TestContentTypeString(t *testing.T) {
	cases := []struct {
		ct   ContentType
		want string
	}{
		{ContentEmpty, "empty"},
		{ContentText, "text (VRML)"},
		{ContentBinary, "binary"},
		{ContentType(3), "unknown"},
		{ContentType(-1), "unknown"},
	}
	for _, c := range cases {
		if got := c.ct.String(); got != c.want {
			t.Errorf("ContentType(%d).String() = %q want %q", int(c.ct), got, c.want)
		}
	}
}

func TestDetect(t *testing.T) {
	cases := []struct {
		name string
		data []byte
		size int64
		want ContentType
	}{
		{"zero size wins over text", []byte("Shape"), 0, ContentEmpty},
		{"all zeros", make([]byte, 64), 64, ContentEmpty},
		{"single zero", []byte{0}, 1, ContentEmpty},
		{"rgba printable bytes", rgbaBinary(), 256, ContentBinary},
		{"float vertices", vertexBinary(), 36, ContentBinary},
		{"one nonzero byte", []byte{0, 0, 7, 0}, 4, ContentBinary},
		{"def without space", []byte("DEFx"), 4, ContentBinary},
		{"url without space", []byte("url=a"), 5, ContentBinary},
		{"lowercase shape", []byte("shape"), 5, ContentBinary},
		{"full scene", []byte(sceneText), int64(len(sceneText)), ContentText},
	}
	for _, kw := range []string{"Shape", "Transform", "DEF ", "material", "appearance", "geometry", "children", "url "} {
		data := append([]byte{0, 0xff, 0x7f}, kw...)
		data = append(data, 1)
		cases = append(cases, struct {
			name string
			data []byte
			size int64
			want ContentType
		}{"keyword " + kw, data, int64(len(data)), ContentText})
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Detect(c.data, c.size); got != c.want {
				t.Fatalf("Detect = %v want %v", got, c.want)
			}
		})
	}
}

func TestRead(t *testing.T) {
	boom := errors.New("boom")

	got, err := Read(iotest.ErrReader(boom), 0)
	if err != nil {
		t.Fatalf("Read size 0 err = %v", err)
	}
	if want := (&Mesh{Type: ContentEmpty}); !reflect.DeepEqual(got, want) {
		t.Fatalf("Read size 0 = %+v want %+v", got, want)
	}

	if _, err := Read(iotest.ErrReader(boom), 8); !errors.Is(err, boom) {
		t.Fatalf("Read with failing reader err = %v want boom", err)
	}

	text := []byte(sceneText)
	got, err = Read(bytes.NewReader(text), int64(len(text)))
	if err != nil {
		t.Fatalf("Read text err = %v", err)
	}
	wantText := &Mesh{Type: ContentText, Size: int64(len(text)), Text: &TextMesh{Source: sceneText, NodeNames: sceneNames, MeshRefs: sceneRefs}}
	if !reflect.DeepEqual(got, wantText) {
		t.Fatalf("Read text = %+v want %+v", got, wantText)
	}

	bin := rgbaBinary()
	got, err = Read(bytes.NewReader(bin), int64(len(bin)))
	if err != nil {
		t.Fatalf("Read binary err = %v", err)
	}
	if want := (&Mesh{Type: ContentBinary, Size: int64(len(bin))}); !reflect.DeepEqual(got, want) {
		t.Fatalf("Read binary = %+v want %+v", got, want)
	}

	zeros := make([]byte, 32)
	got, err = Read(bytes.NewReader(zeros), 32)
	if err != nil {
		t.Fatalf("Read zeros err = %v", err)
	}
	if want := (&Mesh{Type: ContentEmpty, Size: 32}); !reflect.DeepEqual(got, want) {
		t.Fatalf("Read zeros = %+v want %+v", got, want)
	}
}

func TestReadShortDataReturnsTruncated(t *testing.T) {
	cases := []struct {
		name string
		data []byte
		size int64
	}{
		{"text shorter than size", []byte("Shape {"), 64},
		{"binary shorter than size", []byte{1, 2, 3}, 4},
		{"no data with positive size", nil, 16},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Read(bytes.NewReader(c.data), c.size)
			if !errors.Is(err, ErrTruncatedXM) {
				t.Fatalf("Read(%d bytes, size %d) err = %v want ErrTruncatedXM", len(c.data), c.size, err)
			}
		})
	}
}

func TestOpen(t *testing.T) {
	dir := t.TempDir()
	bin := rgbaBinary()
	cases := []struct {
		name string
		data []byte
		want *Mesh
	}{
		{"empty.xm", nil, &Mesh{Type: ContentEmpty}},
		{"zeros.xm", make([]byte, 32), &Mesh{Type: ContentEmpty, Size: 32}},
		{"text.xm", []byte(sceneText), &Mesh{Type: ContentText, Size: int64(len(sceneText)), Text: &TextMesh{Source: sceneText, NodeNames: sceneNames, MeshRefs: sceneRefs}}},
		{"binary.xm", bin, &Mesh{Type: ContentBinary, Size: int64(len(bin))}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := filepath.Join(dir, c.name)
			if err := os.WriteFile(path, c.data, 0o644); err != nil {
				t.Fatal(err)
			}
			got, err := Open(path)
			if err != nil {
				t.Fatalf("Open err = %v", err)
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("Open = %+v want %+v", got, c.want)
			}
		})
	}
}

func TestOpenMissing(t *testing.T) {
	_, err := Open(filepath.Join(t.TempDir(), "missing.xm"))
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Open missing err = %v want os.ErrNotExist", err)
	}
}
