package scene

import (
	"bytes"
	"encoding/binary"
	"errors"
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
	"github.com/nickheyer/discoxip/pkg/xap"
)

const testFormat = 0x112

// The position, normal and texcoord layout every test pool is decoded through
func testLayouts() map[uint32]Layout {
	return map[uint32]Layout{testFormat: {
		Stride: 32,
		Elements: []buffer.Element{
			el(0, "FLOAT3", 0x32, 0, 12, 3),
			el(2, "FLOAT3", 0x32, 12, 12, 3),
			el(9, "FLOAT2", 0x22, 24, 8, 2),
		},
		Semantics: map[int]string{0: "position", 2: "normal", 9: "texcoord0"},
	}}
}

func poolPosition(i int) [3]float32 { return [3]float32{float32(i), float32(2 * i), float32(3 * i)} }

func poolUV(i int) [2]float32 { return [2]float32{0.5 * float32(i), 1} }

// Encodes count vertices of the test layout with positions, normals and UVs derived from the index
func poolVertexBytes(count int) []byte {
	out := make([]byte, 0, count*32)
	put := func(v float32) {
		var b [4]byte
		binary.LittleEndian.PutUint32(b[:], math.Float32bits(v))
		out = append(out, b[:]...)
	}
	for i := range count {
		p := poolPosition(i)
		put(p[0])
		put(p[1])
		put(p[2])
		put(0)
		put(0)
		put(1)
		uv := poolUV(i)
		put(uv[0])
		put(uv[1])
	}
	return out
}

func vbBytes(count uint32, format uint32, raw []byte) []byte {
	out := make([]byte, 8, 8+len(raw))
	binary.LittleEndian.PutUint32(out[0:], count)
	binary.LittleEndian.PutUint32(out[4:], format)
	return append(out, raw...)
}

func ibBytes(indices []uint16) []byte {
	out := make([]byte, 2*len(indices))
	for i, v := range indices {
		binary.LittleEndian.PutUint16(out[2*i:], v)
	}
	return out
}

func writeFile(t *testing.T, dir, name string, data []byte) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// Writes a pool's vertex and index files into dir under the given name
func writePool(t *testing.T, dir, name string, count int, indices []uint16) {
	t.Helper()
	writeFile(t, dir, name+".vb", vbBytes(uint32(count), testFormat, poolVertexBytes(count)))
	if indices != nil {
		writeFile(t, dir, name+".ib", ibBytes(indices))
	}
}

// Builds a 2x2 A8R8G8B8 XPR0 texture whose four pixels are the given NRGBA colors
func xpr2x2(pixels [4][4]byte) []byte {
	out := make([]byte, 32, 48)
	copy(out, "XPR0")
	binary.LittleEndian.PutUint32(out[4:], 48)
	binary.LittleEndian.PutUint32(out[8:], 32)
	binary.LittleEndian.PutUint16(out[14:], 4)
	binary.LittleEndian.PutUint32(out[24:], 0x06<<8|1<<16|1<<20|1<<24)
	for _, p := range pixels {
		out = append(out, p[2], p[1], p[0], p[3])
	}
	return out
}

var testPixels = [4][4]byte{{255, 0, 0, 255}, {0, 255, 0, 128}, {0, 0, 255, 0}, {10, 20, 30, 40}}

const mainScene = `DEF Root Transform {
	translation 1 2 3
	children [
		Shape {
			appearance Appearance {
				material Material {
					name "mat1"
					diffuseColor 0.5 0.25 1
					transparency 0.25
					shininess 0.4
					emissiveColor 0.1 0.2 0.3
				}
				texture ImageTexture {
					url "Textures\Tex1.XBX"
				}
			}
			geometry DEF Box Mesh {
				url "Meshes/box.xbg"
			}
		}
		Shape {
			geometry DEF Shared Mesh {
				url "Meshes/shared.xbg"
			}
		}
		Shape {
			geometry DEF Gone Mesh {
				url "Meshes/none.xbg"
			}
		}
	]
}
`

const mainManifest = `{
	"mainmenu:Meshes/box.xbg": {"pool": "mainmenu_pool0", "index_start": 0, "tri_count": 2, "archive": "mainmenu"},
	"other:Meshes/shared.xbg": {"pool": "mainmenu_pool0", "index_start": 6, "tri_count": 1, "archive": "other"}
}`

var mainIndices = []uint16{0, 1, 2, 2, 1, 3, 4, 5, 6}

// Lays out the scene directory every Load test starts from and returns the XAP path
func writeMainScene(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, dir, "mainmenu_default.xap", []byte(mainScene))
	writeFile(t, dir, "_meshes.json", []byte(mainManifest))
	writePool(t, dir, "mainmenu_pool0", 7, mainIndices)
	writeFile(t, dir, "tex1.xbx", xpr2x2(testPixels))
	writeFile(t, dir, "bad.xbx", []byte("NOPE"))
	writeFile(t, dir, "notes.txt", []byte("ignored"))
	return dir, filepath.Join(dir, "mainmenu_default.xap")
}

func assertFloats(t *testing.T, what string, got, want []float32) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s = %v, want %v", what, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("%s[%d] = %v, want %v", what, i, got[i], want[i])
		}
	}
}

// Lists the floats one semantic holds for test pool vertices first through last
func poolRange(semantic string, first, last int) []float32 {
	var out []float32
	for i := first; i <= last; i++ {
		switch semantic {
		case "position":
			p := poolPosition(i)
			out = append(out, p[0], p[1], p[2])
		case "normal":
			out = append(out, 0, 0, 1)
		case "texcoord0":
			uv := poolUV(i)
			out = append(out, uv[0], uv[1])
		}
	}
	return out
}

func TestLoad(t *testing.T) {
	dir, xapPath := writeMainScene(t)
	layouts := testLayouts()
	s, err := Load(xapPath, layouts)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if s.Dir != dir {
		t.Errorf("Dir = %q, want %q", s.Dir, dir)
	}
	if s.XAP == nil || len(s.XAP.MeshRefs()) != 3 {
		t.Fatalf("XAP = %v", s.XAP)
	}
	if !reflect.DeepEqual(s.Layouts, layouts) {
		t.Errorf("Layouts = %v, want %v", s.Layouts, layouts)
	}

	if len(s.Pools) != 1 {
		t.Fatalf("Pools = %v, want one", s.Pools)
	}
	pool := s.Pools[0]
	if pool.Name != "mainmenu_pool0" || pool.VB.Header.VertexCount != 7 || pool.VB.Header.FormatCode != testFormat || pool.VB.Stride != 32 {
		t.Errorf("pool = %s %+v", pool.Name, pool.VB.Header)
	}
	if pool.IB == nil || !reflect.DeepEqual(pool.IB.Indices, mainIndices) || pool.IB.TriangleCount != 3 {
		t.Errorf("pool IB = %+v", pool.IB)
	}
	if len(pool.Attributes) != 3 {
		t.Fatalf("pool attributes = %+v", pool.Attributes)
	}
	assertFloats(t, "pool positions", pool.Attributes[0].Data, poolRange("position", 0, 6))

	if len(s.Meshes) != 2 {
		t.Fatalf("Meshes = %v, want box and shared", s.Meshes)
	}
	box := s.Meshes["Meshes/box.xbg"]
	if box == nil {
		t.Fatal("box mesh not resolved through the archive prefixed manifest key")
	}
	if box.Name != "Meshes/box.xbg" || box.Format != testFormat || box.Count != 4 {
		t.Errorf("box = %+v", box)
	}
	if !reflect.DeepEqual(box.Indices, []uint16{0, 1, 2, 2, 1, 3}) {
		t.Errorf("box indices = %v", box.Indices)
	}
	if len(box.Attributes) != 3 {
		t.Fatalf("box attributes = %+v", box.Attributes)
	}
	assertFloats(t, "box positions", box.Attributes[0].Data, poolRange("position", 0, 3))
	assertFloats(t, "box normals", box.Attributes[1].Data, poolRange("normal", 0, 3))
	assertFloats(t, "box uvs", box.Attributes[2].Data, poolRange("texcoord0", 0, 3))
	for i, a := range box.Attributes {
		if a.Element != layouts[testFormat].Elements[i] {
			t.Errorf("box attribute %d element = %+v", i, a.Element)
		}
	}

	shared := s.Meshes["Meshes/shared.xbg"]
	if shared == nil {
		t.Fatal("shared mesh not resolved through another archive's manifest key")
	}
	if shared.Count != 3 || !reflect.DeepEqual(shared.Indices, []uint16{0, 1, 2}) {
		t.Errorf("shared = %+v", shared)
	}
	assertFloats(t, "shared positions", shared.Attributes[0].Data, poolRange("position", 4, 6))
	assertFloats(t, "shared uvs", shared.Attributes[2].Data, poolRange("texcoord0", 4, 6))

	if !reflect.DeepEqual(s.Missing, []string{"Meshes/none.xbg"}) {
		t.Errorf("Missing = %v, want the unpacked mesh only", s.Missing)
	}

	if len(s.Textures) != 1 {
		t.Fatalf("Textures = %v, want tex1 only", s.Textures)
	}
	tex := s.Textures[0]
	if tex.Name != "tex1" || tex.Width != 2 || tex.Height != 2 {
		t.Errorf("texture = %s %dx%d", tex.Name, tex.Width, tex.Height)
	}
	img, err := png.Decode(bytes.NewReader(tex.PNGData))
	if err != nil {
		t.Fatalf("texture PNG: %v", err)
	}
	nrgba, ok := img.(*image.NRGBA)
	if !ok {
		t.Fatalf("texture PNG decoded as %T, want NRGBA", img)
	}
	var wantPix []byte
	for _, p := range testPixels {
		wantPix = append(wantPix, p[:]...)
	}
	if !bytes.Equal(nrgba.Pix, wantPix) {
		t.Errorf("texture pixels = %v, want %v", nrgba.Pix, wantPix)
	}

	if len(s.Warnings) != 1 || !strings.Contains(s.Warnings[0], "texture bad.xbx") {
		t.Errorf("Warnings = %v, want one about bad.xbx", s.Warnings)
	}
}

func TestLoadBareManifestKey(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "scene.xap", []byte(`Shape { geometry Mesh { url "Meshes/bare.xbg" } }`))
	writeFile(t, dir, "_meshes.json", []byte(`{"Meshes/bare.xbg": {"pool": "p", "index_start": 3, "tri_count": 1}}`))
	writePool(t, dir, "p", 6, []uint16{0, 1, 2, 3, 4, 5})
	s, err := Load(filepath.Join(dir, "scene.xap"), testLayouts())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	md := s.Meshes["Meshes/bare.xbg"]
	if md == nil || md.Count != 3 || !reflect.DeepEqual(md.Indices, []uint16{0, 1, 2}) {
		t.Fatalf("mesh = %+v", md)
	}
	assertFloats(t, "positions", md.Attributes[0].Data, poolRange("position", 3, 5))
	if len(s.Missing) != 0 || len(s.Textures) != 0 {
		t.Errorf("Missing %v Textures %v, want none", s.Missing, s.Textures)
	}
}

func TestLoadArchiveKeyWinsOverOtherArchives(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "menu_default.xap", []byte(`Shape { geometry Mesh { url "m.xbg" } }`))
	writeFile(t, dir, "_meshes.json", []byte(`{
		"menu:m.xbg": {"pool": "p", "index_start": 0, "tri_count": 1},
		"a:m.xbg": {"pool": "p", "index_start": 3, "tri_count": 1},
		"b:m.xbg": {"pool": "p", "index_start": 3, "tri_count": 1}
	}`))
	writePool(t, dir, "p", 6, []uint16{0, 1, 2, 3, 4, 5})
	s, err := Load(filepath.Join(dir, "menu_default.xap"), testLayouts())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	md := s.Meshes["m.xbg"]
	if md == nil {
		t.Fatal("mesh not resolved")
	}
	assertFloats(t, "positions", md.Attributes[0].Data, poolRange("position", 0, 2))
}

func TestLoadRepeatedURLResolvedOnce(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "scene.xap", []byte(`Group { children [ Shape { geometry Mesh { url "m.xbg" } } Shape { geometry Mesh { url "m.xbg" } } ] }`))
	writeFile(t, dir, "_meshes.json", []byte(`{"m.xbg": {"pool": "p", "index_start": 0, "tri_count": 1}}`))
	writePool(t, dir, "p", 3, []uint16{0, 1, 2})
	s, err := Load(filepath.Join(dir, "scene.xap"), testLayouts())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(s.XAP.MeshRefs()) != 2 || len(s.Meshes) != 1 || s.Meshes["m.xbg"].Count != 3 {
		t.Errorf("refs %v meshes %v", s.XAP.MeshRefs(), s.Meshes)
	}
}

func TestLoadWithoutMeshRefs(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "scene.xap", []byte(`Transform { translation 0 0 0 }`))
	writeFile(t, dir, "_meshes.json", []byte(`{}`))
	writePool(t, dir, "p", 3, []uint16{0, 1, 2})
	writePool(t, dir, "empty", 0, nil)
	s, err := Load(filepath.Join(dir, "scene.xap"), testLayouts())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(s.Meshes) != 0 || len(s.Missing) != 0 {
		t.Errorf("Meshes %v Missing %v", s.Meshes, s.Missing)
	}
	if len(s.Pools) != 2 || s.Pools[0].Name != "empty" || s.Pools[1].Name != "p" {
		t.Fatalf("Pools = %v, want empty then p", s.Pools)
	}
	if s.Pools[0].IB != nil || s.Pools[0].Attributes != nil || s.Pools[0].VB.Header.VertexCount != 0 {
		t.Errorf("empty pool = %+v", s.Pools[0])
	}
}

func TestLoadErrors(t *testing.T) {
	cases := []struct {
		name  string
		setup func(t *testing.T, dir string)
		want  string
		is    error
	}{
		{"missing manifest", func(t *testing.T, dir string) {
			os.Remove(filepath.Join(dir, "_meshes.json"))
		}, "reading mesh manifest", fs.ErrNotExist},
		{"malformed manifest", func(t *testing.T, dir string) {
			writeFile(t, dir, "_meshes.json", []byte("{nope"))
		}, "parsing mesh manifest", nil},
		{"undeclared vertex format", func(t *testing.T, dir string) {
			writeFile(t, dir, "mainmenu_pool0.vb", vbBytes(7, 0x999, poolVertexBytes(7)))
		}, "mainmenu_pool0.vb uses vertex format 00000999 which the executable never declares", nil},
		{"truncated vertex buffer", func(t *testing.T, dir string) {
			writeFile(t, dir, "mainmenu_pool0.vb", []byte{1, 2, 3})
		}, "mainmenu_pool0.vb", buffer.ErrTruncatedVB},
		{"vertex buffer stride disagrees", func(t *testing.T, dir string) {
			writeFile(t, dir, "mainmenu_pool0.vb", vbBytes(7, testFormat, make([]byte, 7*16)))
		}, "declaration stride 32 does not match file stride 16", nil},
		{"pool not extracted", func(t *testing.T, dir string) {
			os.Remove(filepath.Join(dir, "mainmenu_pool0.vb"))
			os.Remove(filepath.Join(dir, "mainmenu_pool0.ib"))
		}, `mesh "Meshes/box.xbg" references pool "mainmenu_pool0" which was not extracted`, nil},
		{"pool without index buffer", func(t *testing.T, dir string) {
			os.Remove(filepath.Join(dir, "mainmenu_pool0.ib"))
		}, `mesh "Meshes/box.xbg" references pool "mainmenu_pool0" which has no decoded geometry`, nil},
		{"pool without vertices", func(t *testing.T, dir string) {
			writeFile(t, dir, "mainmenu_pool0.vb", vbBytes(0, testFormat, nil))
		}, "which has no decoded geometry", nil},
		{"index range beyond pool", func(t *testing.T, dir string) {
			writeFile(t, dir, "mainmenu_pool0.ib", ibBytes(mainIndices[:4]))
		}, `mesh "Meshes/box.xbg" index range [0,6) exceeds pool "mainmenu_pool0" with 4 indices`, nil},
		{"mesh packed in several archives", func(t *testing.T, dir string) {
			writeFile(t, dir, "_meshes.json", []byte(`{
				"a:Meshes/box.xbg": {"pool": "mainmenu_pool0", "index_start": 0, "tri_count": 1},
				"b:Meshes/box.xbg": {"pool": "mainmenu_pool0", "index_start": 0, "tri_count": 1}
			}`))
		}, `mesh "Meshes/box.xbg" is packed in several archives: [a:Meshes/box.xbg b:Meshes/box.xbg]`, nil},
		{"unparseable scene", func(t *testing.T, dir string) {
			writeFile(t, dir, "mainmenu_default.xap", []byte(`Transform { children 5 }`))
		}, "parsing XAP", xap.ErrUnexpectedToken},
		{"scene file missing", func(t *testing.T, dir string) {
			os.Remove(filepath.Join(dir, "mainmenu_default.xap"))
		}, "parsing XAP", fs.ErrNotExist},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir, xapPath := writeMainScene(t)
			c.setup(t, dir)
			s, err := Load(xapPath, testLayouts())
			if err == nil {
				t.Fatalf("Load succeeded with %v", s)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("err = %v, want one containing %q", err, c.want)
			}
			if c.is != nil && !errors.Is(err, c.is) {
				t.Errorf("err = %v does not wrap %v", err, c.is)
			}
			if s != nil {
				t.Errorf("scene = %+v, want nil", s)
			}
		})
	}
}

func TestLoadIndexRangeBeyondPoolIndexStart(t *testing.T) {
	dir, xapPath := writeMainScene(t)
	writeFile(t, dir, "_meshes.json", []byte(`{"mainmenu:Meshes/box.xbg": {"pool": "mainmenu_pool0", "index_start": 7, "tri_count": 1}}`))
	_, err := Load(xapPath, testLayouts())
	if err == nil || !strings.Contains(err.Error(), "index range [7,10) exceeds pool") {
		t.Errorf("err = %v", err)
	}
}

func TestDetectArchiveName(t *testing.T) {
	manifest := map[string]meshManifestEntry{
		"mainmenu:Meshes/a.xbg":       {},
		"settings_clock:Meshes/b.xbg": {},
		"settings:Meshes/c.xbg":       {},
		"bare.xbg":                    {},
	}
	cases := []struct {
		path string
		want string
	}{
		{"/x/mainmenu_default.xap", "mainmenu"},
		{"mainmenu_default.xap", "mainmenu"},
		{"settings_clock_default.xap", "settings_clock"},
		{"settings_audio_default.xap", "settings"},
		{"settings_default.xap", "settings"},
		{"other_default.xap", ""},
		{"mainmenu.xap", ""},
		{"_default.xap", ""},
		{"bare.xap", ""},
	}
	for _, c := range cases {
		if got := detectArchiveName(c.path, manifest); got != c.want {
			t.Errorf("detectArchiveName(%q) = %q, want %q", c.path, got, c.want)
		}
	}
}

func TestExtractSubMesh(t *testing.T) {
	pool := &BufferPool{Attributes: []buffer.Attribute{
		{Element: el(0, "FLOAT3", 0x32, 0, 12, 3), Data: poolRange("position", 0, 9)},
		{Element: el(9, "FLOAT2", 0x22, 24, 8, 2), Data: poolRange("texcoord0", 0, 9)},
	}}
	attrs, indices, count := extractSubMesh(pool, []uint16{3, 4, 5, 4, 5, 6})
	if count != 4 {
		t.Errorf("count = %d, want 4", count)
	}
	if !reflect.DeepEqual(indices, []uint16{0, 1, 2, 1, 2, 3}) {
		t.Errorf("indices = %v", indices)
	}
	if len(attrs) != 2 || attrs[0].Element != pool.Attributes[0].Element || attrs[1].Element != pool.Attributes[1].Element {
		t.Fatalf("attributes = %+v", attrs)
	}
	assertFloats(t, "positions", attrs[0].Data, poolRange("position", 3, 6))
	assertFloats(t, "uvs", attrs[1].Data, poolRange("texcoord0", 3, 6))
	attrs[0].Data[0] = 99
	if pool.Attributes[0].Data[9] == 99 {
		t.Error("sub mesh shares its float storage with the pool")
	}

	attrs, indices, count = extractSubMesh(pool, []uint16{7, 7, 7})
	if count != 1 || !reflect.DeepEqual(indices, []uint16{0, 0, 0}) {
		t.Errorf("single vertex sub mesh = %v, %d", indices, count)
	}
	assertFloats(t, "single position", attrs[0].Data, poolRange("position", 7, 7))

	attrs, indices, count = extractSubMesh(pool, nil)
	if attrs != nil || indices != nil || count != 0 {
		t.Errorf("empty sub mesh = %v, %v, %d", attrs, indices, count)
	}
}

func TestFindPool(t *testing.T) {
	s := &Scene{Pools: []*BufferPool{{Name: "a"}, {Name: "b"}}}
	if p := s.findPool("b"); p != s.Pools[1] {
		t.Errorf("findPool(b) = %v", p)
	}
	if p := s.findPool("c"); p != nil {
		t.Errorf("findPool(c) = %v, want nil", p)
	}
}

func testMesh(name string, count int) *MeshData {
	return &MeshData{
		Name:   name,
		Format: testFormat,
		Count:  count,
		Attributes: []buffer.Attribute{
			{Element: el(0, "FLOAT3", 0x32, 0, 12, 3), Data: poolRange("position", 0, count-1)},
			{Element: el(2, "FLOAT3", 0x32, 12, 12, 3), Data: poolRange("normal", 0, count-1)},
			{Element: el(9, "FLOAT2", 0x22, 24, 8, 2), Data: poolRange("texcoord0", 0, count-1)},
		},
	}
}

func TestMeshDataAttribute(t *testing.T) {
	md := testMesh("m", 3)
	layouts := testLayouts()
	for i, semantic := range []string{"position", "normal", "texcoord0"} {
		if a := md.Attribute(layouts, semantic); a != &md.Attributes[i] {
			t.Errorf("Attribute(%q) = %v, want attribute %d", semantic, a, i)
		}
	}
	if a := md.Attribute(layouts, "diffuse"); a != nil {
		t.Errorf("Attribute(diffuse) = %v, want nil", a)
	}
	if a := md.Attribute(map[uint32]Layout{}, "position"); a != nil {
		t.Errorf("Attribute with unknown format = %v, want nil", a)
	}
	md.Format = 0x999
	if a := md.Attribute(layouts, "position"); a != nil {
		t.Errorf("Attribute with undeclared format = %v, want nil", a)
	}
}

func TestMeshDataVertices(t *testing.T) {
	md := testMesh("m", 3)
	verts, err := md.Vertices(testLayouts())
	if err != nil {
		t.Fatalf("Vertices: %v", err)
	}
	if len(verts) != 3 {
		t.Fatalf("got %d vertices, want 3", len(verts))
	}
	for i, v := range verts {
		want := buffer.Vertex{Pos: poolPosition(i), Normal: [3]float32{0, 0, 1}, UV: poolUV(i)}
		if v != want {
			t.Errorf("vertex %d = %+v, want %+v", i, v, want)
		}
	}
}

func TestMeshDataVerticesPositionOnly(t *testing.T) {
	md := testMesh("m", 2)
	md.Attributes = md.Attributes[:1]
	verts, err := md.Vertices(testLayouts())
	if err != nil {
		t.Fatalf("Vertices: %v", err)
	}
	for i, v := range verts {
		if v != (buffer.Vertex{Pos: poolPosition(i)}) {
			t.Errorf("vertex %d = %+v, want position only", i, v)
		}
	}
}

func TestMeshDataVerticesFourComponentPosition(t *testing.T) {
	md := &MeshData{Name: "m", Format: 0x7, Count: 2, Attributes: []buffer.Attribute{
		{Element: el(0, "FLOAT4", 0x42, 0, 16, 4), Data: []float32{1, 2, 3, 4, 5, 6, 7, 8}},
	}}
	layouts := map[uint32]Layout{0x7: {Stride: 16, Elements: []buffer.Element{el(0, "FLOAT4", 0x42, 0, 16, 4)}, Semantics: map[int]string{0: "position"}}}
	verts, err := md.Vertices(layouts)
	if err != nil {
		t.Fatalf("Vertices: %v", err)
	}
	if verts[0].Pos != [3]float32{1, 2, 3} || verts[1].Pos != [3]float32{5, 6, 7} {
		t.Errorf("vertices = %+v", verts)
	}
}

func TestMeshDataVerticesWithoutPosition(t *testing.T) {
	md := testMesh("Meshes/m.xbg", 3)
	md.Attributes = md.Attributes[1:]
	verts, err := md.Vertices(testLayouts())
	if err == nil || !strings.Contains(err.Error(), `mesh "Meshes/m.xbg" has no position attribute`) {
		t.Errorf("Vertices = %v, %v", verts, err)
	}
	md = testMesh("m", 3)
	if _, err := md.Vertices(map[uint32]Layout{}); err == nil {
		t.Error("Vertices succeeded without a layout for the mesh format")
	}
}
