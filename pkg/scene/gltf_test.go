package scene

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/nickheyer/discoxip/pkg/buffer"
	"github.com/nickheyer/discoxip/pkg/xap"
)

type glbNode struct {
	Name        string    `json:"name"`
	Mesh        *int      `json:"mesh"`
	Children    []int     `json:"children"`
	Translation []float64 `json:"translation"`
	Rotation    []float64 `json:"rotation"`
	Scale       []float64 `json:"scale"`
}

type glbPBR struct {
	BaseColorFactor  []float64 `json:"baseColorFactor"`
	BaseColorTexture *struct {
		Index int `json:"index"`
	} `json:"baseColorTexture"`
	MetallicFactor  float64 `json:"metallicFactor"`
	RoughnessFactor float64 `json:"roughnessFactor"`
}

type glbMaterial struct {
	Name           string    `json:"name"`
	PBR            *glbPBR   `json:"pbrMetallicRoughness"`
	EmissiveFactor []float64 `json:"emissiveFactor"`
	AlphaMode      string    `json:"alphaMode"`
	DoubleSided    bool      `json:"doubleSided"`
}

type glbPrimitive struct {
	Attributes map[string]int `json:"attributes"`
	Indices    *int           `json:"indices"`
	Material   *int           `json:"material"`
}

type glbAccessor struct {
	BufferView    int       `json:"bufferView"`
	ByteOffset    int       `json:"byteOffset"`
	ComponentType int       `json:"componentType"`
	Count         int       `json:"count"`
	Type          string    `json:"type"`
	Max           []float64 `json:"max"`
	Min           []float64 `json:"min"`
}

type glbBufferView struct {
	Buffer     int `json:"buffer"`
	ByteOffset int `json:"byteOffset"`
	ByteLength int `json:"byteLength"`
	Target     int `json:"target"`
}

type glbDoc struct {
	Asset struct {
		Version   string `json:"version"`
		Generator string `json:"generator"`
	} `json:"asset"`
	Scene  int `json:"scene"`
	Scenes []struct {
		Nodes []int `json:"nodes"`
	} `json:"scenes"`
	Nodes  []glbNode `json:"nodes"`
	Meshes []struct {
		Name       string         `json:"name"`
		Primitives []glbPrimitive `json:"primitives"`
	} `json:"meshes"`
	Materials []glbMaterial `json:"materials"`
	Textures  []struct {
		Sampler int `json:"sampler"`
		Source  int `json:"source"`
	} `json:"textures"`
	Images []struct {
		BufferView int    `json:"bufferView"`
		MimeType   string `json:"mimeType"`
		Name       string `json:"name"`
	} `json:"images"`
	Samplers []struct {
		MagFilter int `json:"magFilter"`
		MinFilter int `json:"minFilter"`
		WrapS     int `json:"wrapS"`
		WrapT     int `json:"wrapT"`
	} `json:"samplers"`
	Accessors   []glbAccessor   `json:"accessors"`
	BufferViews []glbBufferView `json:"bufferViews"`
	Buffers     []struct {
		ByteLength int `json:"byteLength"`
	} `json:"buffers"`
}

// Splits a GLB into its decoded JSON document and binary chunk after checking the container framing
func parseGLB(t *testing.T, data []byte) (glbDoc, []byte) {
	t.Helper()
	if len(data) < 20 || string(data[0:4]) != "glTF" {
		t.Fatalf("output of %d bytes does not start with the glTF magic", len(data))
	}
	if v := binary.LittleEndian.Uint32(data[4:8]); v != 2 {
		t.Fatalf("version = %d, want 2", v)
	}
	if total := binary.LittleEndian.Uint32(data[8:12]); int(total) != len(data) {
		t.Fatalf("header length = %d, file is %d bytes", total, len(data))
	}
	jsonLen := int(binary.LittleEndian.Uint32(data[12:16]))
	if typ := binary.LittleEndian.Uint32(data[16:20]); typ != 0x4E4F534A {
		t.Fatalf("first chunk type = %08X, want JSON", typ)
	}
	if jsonLen%4 != 0 || 20+jsonLen > len(data) {
		t.Fatalf("JSON chunk length %d is unpadded or beyond the file", jsonLen)
	}
	jsonChunk := data[20 : 20+jsonLen]
	if trimmed := bytes.TrimRight(jsonChunk, " "); len(trimmed) == 0 || trimmed[len(trimmed)-1] != '}' {
		t.Fatalf("JSON chunk is not padded with spaces, ends %q", jsonChunk[len(jsonChunk)-4:])
	}
	var doc glbDoc
	if err := json.Unmarshal(jsonChunk, &doc); err != nil {
		t.Fatalf("JSON chunk: %v", err)
	}
	rest := data[20+jsonLen:]
	if len(rest) == 0 {
		return doc, nil
	}
	if len(rest) < 8 {
		t.Fatalf("trailing %d bytes after the JSON chunk", len(rest))
	}
	binLen := int(binary.LittleEndian.Uint32(rest[0:4]))
	if typ := binary.LittleEndian.Uint32(rest[4:8]); typ != 0x004E4942 {
		t.Fatalf("second chunk type = %08X, want BIN", typ)
	}
	if 8+binLen != len(rest) {
		t.Fatalf("BIN chunk length %d, %d bytes follow its header", binLen, len(rest)-8)
	}
	if binLen%4 != 0 {
		t.Fatalf("BIN chunk length %d is not 4 byte aligned", binLen)
	}
	return doc, rest[8:]
}

func exportGLB(t *testing.T, s *Scene) (glbDoc, []byte) {
	t.Helper()
	var buf bytes.Buffer
	if err := ExportGLB(&buf, s); err != nil {
		t.Fatalf("ExportGLB: %v", err)
	}
	return parseGLB(t, buf.Bytes())
}

func sceneFromXAP(t *testing.T, src string, meshes map[string]*MeshData, textures []*TextureData) *Scene {
	t.Helper()
	ast, err := xap.Parse(src)
	if err != nil {
		t.Fatalf("xap.Parse: %v", err)
	}
	return &Scene{XAP: ast, Meshes: meshes, Textures: textures, Layouts: testLayouts()}
}

func pad4(n int) int { return (n + 3) &^ 3 }

func pngBytes(t *testing.T, w, h int, c color.NRGBA) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < w*h; i++ {
		img.SetNRGBA(i%w, i/w, c)
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func floatsAt(bin []byte, offset, count int) []float32 {
	out := make([]float32, count)
	for i := range out {
		out[i] = math.Float32frombits(binary.LittleEndian.Uint32(bin[offset+4*i:]))
	}
	return out
}

func indicesAt(bin []byte, offset, count int) []uint16 {
	out := make([]uint16, count)
	for i := range out {
		out[i] = binary.LittleEndian.Uint16(bin[offset+2*i:])
	}
	return out
}

func assertVec(t *testing.T, what string, got, want []float64) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("%s = %v, want %v", what, got, want)
		return
	}
	for i := range want {
		if math.Abs(got[i]-want[i]) > 1e-9 {
			t.Errorf("%s = %v, want %v", what, got, want)
			return
		}
	}
}

func intp(v int) *int { return &v }

// Checks the four buffer views and accessors one mesh of count vertices adds starting at offset
func assertMeshBuffers(t *testing.T, doc glbDoc, bin []byte, bvBase, accBase, offset, count int, indices []uint16) int {
	t.Helper()
	posLen, uvLen, idxLen := count*12, count*8, len(indices)*2
	wantViews := []glbBufferView{
		{0, offset, posLen, 34962},
		{0, offset + posLen, posLen, 34962},
		{0, offset + 2*posLen, uvLen, 34962},
		{0, offset + 2*posLen + uvLen, idxLen, 34963},
	}
	for i, want := range wantViews {
		if got := doc.BufferViews[bvBase+i]; got != want {
			t.Errorf("bufferView %d = %+v, want %+v", bvBase+i, got, want)
		}
	}
	wantAcc := []glbAccessor{
		{BufferView: bvBase, ComponentType: 5126, Count: count, Type: "VEC3"},
		{BufferView: bvBase + 1, ComponentType: 5126, Count: count, Type: "VEC3"},
		{BufferView: bvBase + 2, ComponentType: 5126, Count: count, Type: "VEC2"},
		{BufferView: bvBase + 3, ComponentType: 5123, Count: len(indices), Type: "SCALAR"},
	}
	for i, want := range wantAcc {
		got := doc.Accessors[accBase+i]
		got.Min, got.Max = nil, nil
		if !reflect.DeepEqual(got, want) {
			t.Errorf("accessor %d = %+v, want %+v", accBase+i, got, want)
		}
	}
	if doc.Accessors[accBase+1].Min != nil || doc.Accessors[accBase+3].Max != nil {
		t.Errorf("only the position accessor carries bounds")
	}
	wantPos := poolRange("position", 0, count-1)
	if got := floatsAt(bin, offset, count*3); !reflect.DeepEqual(got, wantPos) {
		t.Errorf("positions in BIN = %v, want %v", got, wantPos)
	}
	wantNorm := poolRange("normal", 0, count-1)
	if got := floatsAt(bin, offset+posLen, count*3); !reflect.DeepEqual(got, wantNorm) {
		t.Errorf("normals in BIN = %v, want %v", got, wantNorm)
	}
	wantUV := poolRange("texcoord0", 0, count-1)
	if got := floatsAt(bin, offset+2*posLen, count*2); !reflect.DeepEqual(got, wantUV) {
		t.Errorf("uvs in BIN = %v, want %v", got, wantUV)
	}
	if got := indicesAt(bin, offset+2*posLen+uvLen, len(indices)); !reflect.DeepEqual(got, indices) {
		t.Errorf("indices in BIN = %v, want %v", got, indices)
	}
	return pad4(offset + 2*posLen + uvLen + idxLen)
}

func TestExportGLBShapesWithMaterialAndTexture(t *testing.T) {
	box := testMesh("Meshes/box.xbg", 4)
	box.Indices = []uint16{0, 1, 2, 2, 1, 3}
	shared := testMesh("Meshes/shared.xbg", 3)
	shared.Indices = []uint16{0, 1, 2}
	pngData := pngBytes(t, 2, 2, color.NRGBA{R: 255, G: 0, B: 0, A: 255})
	s := sceneFromXAP(t, mainScene,
		map[string]*MeshData{"Meshes/box.xbg": box, "Meshes/shared.xbg": shared},
		[]*TextureData{{Name: "tex1", PNGData: pngData, Width: 2, Height: 2}})
	doc, bin := exportGLB(t, s)

	if doc.Asset.Version != "2.0" || doc.Asset.Generator != "discoxip" {
		t.Errorf("asset = %+v", doc.Asset)
	}
	if doc.Scene != 0 || len(doc.Scenes) != 1 || !reflect.DeepEqual(doc.Scenes[0].Nodes, []int{2}) {
		t.Errorf("scenes = %+v, want the root transform as node 2", doc.Scenes)
	}
	wantNodes := []glbNode{
		{Name: "Box", Mesh: intp(0)},
		{Name: "Shared", Mesh: intp(1)},
		{Name: "Root", Translation: []float64{1, 2, 3}, Children: []int{0, 1}},
	}
	if !reflect.DeepEqual(doc.Nodes, wantNodes) {
		t.Errorf("nodes = %+v, want %+v", doc.Nodes, wantNodes)
	}

	if len(doc.Meshes) != 2 {
		t.Fatalf("meshes = %+v, want two", doc.Meshes)
	}
	wantBox := glbPrimitive{Attributes: map[string]int{"POSITION": 0, "NORMAL": 1, "TEXCOORD_0": 2}, Indices: intp(3), Material: intp(0)}
	if doc.Meshes[0].Name != "Box" || len(doc.Meshes[0].Primitives) != 1 || !reflect.DeepEqual(doc.Meshes[0].Primitives[0], wantBox) {
		t.Errorf("mesh 0 = %+v, want Box with %+v", doc.Meshes[0], wantBox)
	}
	wantShared := glbPrimitive{Attributes: map[string]int{"POSITION": 4, "NORMAL": 5, "TEXCOORD_0": 6}, Indices: intp(7)}
	if doc.Meshes[1].Name != "Shared" || !reflect.DeepEqual(doc.Meshes[1].Primitives[0], wantShared) {
		t.Errorf("mesh 1 = %+v, want Shared with %+v", doc.Meshes[1], wantShared)
	}

	if len(doc.Samplers) != 1 || doc.Samplers[0] != (struct {
		MagFilter int `json:"magFilter"`
		MinFilter int `json:"minFilter"`
		WrapS     int `json:"wrapS"`
		WrapT     int `json:"wrapT"`
	}{9729, 9987, 10497, 10497}) {
		t.Errorf("samplers = %+v", doc.Samplers)
	}
	if len(doc.Textures) != 1 || doc.Textures[0].Sampler != 0 || doc.Textures[0].Source != 0 {
		t.Errorf("textures = %+v", doc.Textures)
	}
	if len(doc.Images) != 1 || doc.Images[0].BufferView != 0 || doc.Images[0].MimeType != "image/png" || doc.Images[0].Name != "tex1" {
		t.Errorf("images = %+v", doc.Images)
	}
	imageView := glbBufferView{Buffer: 0, ByteOffset: 0, ByteLength: len(pngData)}
	if len(doc.BufferViews) != 9 || doc.BufferViews[0] != imageView {
		t.Fatalf("bufferViews = %+v, want nine starting with %+v", doc.BufferViews, imageView)
	}
	if !bytes.Equal(bin[:len(pngData)], pngData) {
		t.Errorf("BIN does not start with the PNG bytes")
	}

	if len(doc.Materials) != 1 {
		t.Fatalf("materials = %+v, want one", doc.Materials)
	}
	mat := doc.Materials[0]
	if mat.Name != "mat1" || !mat.DoubleSided || mat.AlphaMode != "BLEND" {
		t.Errorf("material = %+v", mat)
	}
	assertVec(t, "emissiveFactor", mat.EmissiveFactor, []float64{0.1, 0.2, 0.3})
	if mat.PBR == nil {
		t.Fatal("material has no pbrMetallicRoughness")
	}
	assertVec(t, "baseColorFactor", mat.PBR.BaseColorFactor, []float64{0.5, 0.25, 1, 0.75})
	if mat.PBR.MetallicFactor != 0 || math.Abs(mat.PBR.RoughnessFactor-0.6) > 1e-9 {
		t.Errorf("metallic %v roughness %v, want 0 and 0.6", mat.PBR.MetallicFactor, mat.PBR.RoughnessFactor)
	}
	if mat.PBR.BaseColorTexture == nil || mat.PBR.BaseColorTexture.Index != 0 {
		t.Errorf("baseColorTexture = %+v, want index 0", mat.PBR.BaseColorTexture)
	}

	if len(doc.Accessors) != 8 {
		t.Fatalf("accessors = %+v, want eight", doc.Accessors)
	}
	assertVec(t, "box min", doc.Accessors[0].Min, []float64{0, 0, 0})
	assertVec(t, "box max", doc.Accessors[0].Max, []float64{3, 6, 9})
	assertVec(t, "shared min", doc.Accessors[4].Min, []float64{0, 0, 0})
	assertVec(t, "shared max", doc.Accessors[4].Max, []float64{2, 4, 6})
	next := assertMeshBuffers(t, doc, bin, 1, 0, pad4(len(pngData)), 4, box.Indices)
	next = assertMeshBuffers(t, doc, bin, 5, 4, next, 3, shared.Indices)
	if len(doc.Buffers) != 1 || doc.Buffers[0].ByteLength != next || len(bin) != next {
		t.Errorf("buffers = %+v with %d BIN bytes, want %d", doc.Buffers, len(bin), next)
	}
	if next != pad4(len(pngData))+140+104 {
		t.Errorf("BIN length %d, want %d", next, pad4(len(pngData))+244)
	}
}

func TestExportGLBMeshNodeWithoutTextures(t *testing.T) {
	m := testMesh("m", 3)
	m.Indices = []uint16{2, 1, 0}
	s := sceneFromXAP(t, `DEF M Mesh { url "m" }`, map[string]*MeshData{"m": m}, nil)
	doc, bin := exportGLB(t, s)
	wantNodes := []glbNode{{Name: "M", Mesh: intp(0)}}
	if !reflect.DeepEqual(doc.Nodes, wantNodes) || !reflect.DeepEqual(doc.Scenes[0].Nodes, []int{0}) {
		t.Errorf("nodes = %+v scenes %+v", doc.Nodes, doc.Scenes)
	}
	if len(doc.Materials) != 0 || len(doc.Textures) != 0 || len(doc.Images) != 0 || len(doc.Samplers) != 0 {
		t.Errorf("materials %v textures %v images %v samplers %v, want none", doc.Materials, doc.Textures, doc.Images, doc.Samplers)
	}
	want := glbPrimitive{Attributes: map[string]int{"POSITION": 0, "NORMAL": 1, "TEXCOORD_0": 2}, Indices: intp(3)}
	if len(doc.Meshes) != 1 || doc.Meshes[0].Name != "M" || !reflect.DeepEqual(doc.Meshes[0].Primitives, []glbPrimitive{want}) {
		t.Errorf("meshes = %+v", doc.Meshes)
	}
	if len(doc.BufferViews) != 4 || len(doc.Accessors) != 4 {
		t.Fatalf("%d bufferViews and %d accessors, want 4 and 4", len(doc.BufferViews), len(doc.Accessors))
	}
	end := assertMeshBuffers(t, doc, bin, 0, 0, 0, 3, m.Indices)
	if end != 104 || len(bin) != 104 || doc.Buffers[0].ByteLength != 104 {
		t.Errorf("BIN is %d bytes, buffer says %d, want 104", len(bin), doc.Buffers[0].ByteLength)
	}
	if !bytes.Equal(bin[102:], []byte{0, 0}) {
		t.Errorf("BIN padding = %v, want zeros", bin[102:])
	}
}

func TestExportGLBMeshNameFallsBackToURL(t *testing.T) {
	m := testMesh("Meshes/m.xbg", 3)
	m.Indices = []uint16{0, 1, 2}
	s := sceneFromXAP(t, `Mesh { url "Meshes/m.xbg" }`, map[string]*MeshData{"Meshes/m.xbg": m}, nil)
	doc, _ := exportGLB(t, s)
	if doc.Meshes[0].Name != "Meshes/m.xbg" || doc.Nodes[0].Name != "" {
		t.Errorf("mesh name %q node name %q", doc.Meshes[0].Name, doc.Nodes[0].Name)
	}
}

func TestExportGLBTransforms(t *testing.T) {
	h := math.Sqrt(2) / 2
	shape := `Shape { geometry Mesh { url "m" } }`
	cases := []struct {
		name  string
		xap   string
		nodes []glbNode
		roots []int
	}{
		{"translation only", `DEF T Transform { translation 4 5 6 children [ ` + shape + ` ] }`,
			[]glbNode{{Mesh: intp(0)}, {Name: "T", Translation: []float64{4, 5, 6}, Children: []int{0}}}, []int{1}},
		{"rotation and scale", `Transform { rotation 0 0 1 1.5707963267948966 scale 2 3 4 children [ ` + shape + ` ] }`,
			[]glbNode{{Mesh: intp(0)}, {Rotation: []float64{0, 0, h, h}, Scale: []float64{2, 3, 4}, Children: []int{0}}}, []int{1}},
		{"rotation about y", `Transform { rotation 0 1 0 3.141592653589793 children [ ` + shape + ` ] }`,
			[]glbNode{{Mesh: intp(0)}, {Rotation: []float64{0, 1, 0, 0}, Children: []int{0}}}, []int{1}},
		{"scale orientation without scale", `Transform { rotation 0 0 1 1.5707963267948966 scaleOrientation 1 0 0 1 children [ ` + shape + ` ] }`,
			[]glbNode{{Mesh: intp(0)}, {Rotation: []float64{0, 0, h, h}, Children: []int{0}}}, []int{1}},
		{"scale orientation nests an inner node", `DEF T Transform { rotation 0 0 1 1.5707963267948966 scaleOrientation 0 0 1 1.5707963267948966 scale 2 3 4 children [ ` + shape + ` ] }`,
			[]glbNode{
				{Mesh: intp(0)},
				{Rotation: []float64{0, 0, -h, h}, Scale: []float64{2, 3, 4}, Children: []int{0}},
				{Name: "T", Rotation: []float64{0, 0, 1, 0}, Children: []int{1}},
			}, []int{2}},
		{"scale orientation alone", `Transform { scaleOrientation 0 1 0 3.141592653589793 scale 1 1 1 children [ ` + shape + ` ] }`,
			[]glbNode{
				{Mesh: intp(0)},
				{Rotation: []float64{0, -1, 0, 0}, Scale: []float64{1, 1, 1}, Children: []int{0}},
				{Rotation: []float64{0, 1, 0, 0}, Children: []int{1}},
			}, []int{2}},
		{"short vectors ignored", `DEF T Transform { translation 1 2 rotation 0 1 0 scale 2 children [ ` + shape + ` ] }`,
			[]glbNode{{Mesh: intp(0)}, {Name: "T", Children: []int{0}}}, []int{1}},
		{"named empty transform kept", `DEF T Transform { } ` + shape,
			[]glbNode{{Name: "T"}, {Mesh: intp(0)}}, []int{0, 1}},
		{"nested transforms", `Transform { translation 1 0 0 children [ Transform { translation 0 1 0 children [ ` + shape + ` ] } ] }`,
			[]glbNode{{Mesh: intp(0)}, {Translation: []float64{0, 1, 0}, Children: []int{0}}, {Translation: []float64{1, 0, 0}, Children: []int{1}}}, []int{2}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := testMesh("m", 3)
			m.Indices = []uint16{0, 1, 2}
			doc, _ := exportGLB(t, sceneFromXAP(t, c.xap, map[string]*MeshData{"m": m}, nil))
			if len(doc.Nodes) != len(c.nodes) {
				t.Fatalf("nodes = %+v, want %+v", doc.Nodes, c.nodes)
			}
			for i, want := range c.nodes {
				got := doc.Nodes[i]
				if got.Name != want.Name || !reflect.DeepEqual(got.Mesh, want.Mesh) || !reflect.DeepEqual(got.Children, want.Children) {
					t.Errorf("node %d = %+v, want %+v", i, got, want)
				}
				assertVec(t, "translation", got.Translation, want.Translation)
				assertVec(t, "rotation", got.Rotation, want.Rotation)
				assertVec(t, "scale", got.Scale, want.Scale)
			}
			if !reflect.DeepEqual(doc.Scenes[0].Nodes, c.roots) {
				t.Errorf("roots = %v, want %v", doc.Scenes[0].Nodes, c.roots)
			}
		})
	}
}

func TestExportGLBGenericAndEmptyNodes(t *testing.T) {
	m := testMesh("m", 3)
	m.Indices = []uint16{0, 1, 2}
	src := `Group { children [ Shape { geometry Mesh { url "m" } } ] }
Transform { }
Group { }
Level { name "x" }
DEF Named Level { children [ Shape { geometry Mesh { url "m" } } ] }
Shape { }
Shape { geometry Mesh { } }
Shape { geometry Box { } }
Mesh { }
`
	doc, _ := exportGLB(t, sceneFromXAP(t, src, map[string]*MeshData{"m": m}, nil))
	wantNodes := []glbNode{
		{Mesh: intp(0)},
		{Name: "Group", Children: []int{0}},
		{Mesh: intp(0)},
		{Name: "Named", Children: []int{2}},
	}
	if !reflect.DeepEqual(doc.Nodes, wantNodes) {
		t.Errorf("nodes = %+v, want %+v", doc.Nodes, wantNodes)
	}
	if !reflect.DeepEqual(doc.Scenes[0].Nodes, []int{1, 3}) {
		t.Errorf("roots = %v, want [1 3]", doc.Scenes[0].Nodes)
	}
	if len(doc.Meshes) != 1 {
		t.Errorf("meshes = %+v, want the shared mesh once", doc.Meshes)
	}
}

func TestExportGLBMeshReusePerMaterial(t *testing.T) {
	m := testMesh("m", 3)
	m.Indices = []uint16{0, 1, 2}
	src := `Group { children [
		Shape { geometry Mesh { url "m" } }
		Shape { geometry Mesh { url "m" } }
		Shape { appearance Appearance { material Material { name "red" diffuseColor 1 0 0 } } geometry Mesh { url "m" } }
		Shape { appearance Appearance { material Material { name "red" diffuseColor 1 0 0 } } geometry Mesh { url "m" } }
		Shape { appearance Appearance { material Material { name "red" } texture ImageTexture { url "t.xbx" } } geometry Mesh { url "m" } }
	] }`
	doc, bin := exportGLB(t, sceneFromXAP(t, src, map[string]*MeshData{"m": m}, nil))
	if len(doc.Nodes) != 6 {
		t.Fatalf("nodes = %+v, want six", doc.Nodes)
	}
	wantMesh := []int{0, 0, 1, 1, 2}
	for i, want := range wantMesh {
		if doc.Nodes[i].Mesh == nil || *doc.Nodes[i].Mesh != want {
			t.Errorf("node %d mesh = %v, want %d", i, doc.Nodes[i].Mesh, want)
		}
	}
	if len(doc.Meshes) != 3 || len(doc.Materials) != 2 {
		t.Fatalf("%d meshes and %d materials, want 3 and 2", len(doc.Meshes), len(doc.Materials))
	}
	if doc.Meshes[0].Primitives[0].Material != nil || *doc.Meshes[1].Primitives[0].Material != 0 || *doc.Meshes[2].Primitives[0].Material != 1 {
		t.Errorf("mesh materials = %v %v %v", doc.Meshes[0].Primitives[0].Material, doc.Meshes[1].Primitives[0].Material, doc.Meshes[2].Primitives[0].Material)
	}
	if len(bin) != 3*104 || doc.Buffers[0].ByteLength != 312 {
		t.Errorf("BIN is %d bytes, want 312", len(bin))
	}
	if doc.Materials[1].PBR.BaseColorTexture != nil {
		t.Errorf("material without a matching texture references texture %+v", doc.Materials[1].PBR.BaseColorTexture)
	}
}

func TestExportGLBMaterialDefaults(t *testing.T) {
	m := testMesh("m", 3)
	m.Indices = []uint16{0, 1, 2}
	cases := []struct {
		name       string
		appearance string
		want       glbMaterial
	}{
		{"texture without material", `Appearance { texture ImageTexture { url "none.xbx" } }`,
			glbMaterial{DoubleSided: true, PBR: &glbPBR{BaseColorFactor: []float64{1, 1, 1, 1}, MetallicFactor: 0, RoughnessFactor: 0.8}}},
		{"opaque material", `Appearance { material Material { name "flat" transparency 0 shininess 0 } }`,
			glbMaterial{Name: "flat", DoubleSided: true, PBR: &glbPBR{BaseColorFactor: []float64{1, 1, 1, 1}, MetallicFactor: 0, RoughnessFactor: 0.8}}},
		{"short diffuse ignored", `Appearance { material Material { name "two" diffuseColor 0.5 0.5 transparency 0.5 } }`,
			glbMaterial{Name: "two", DoubleSided: true, AlphaMode: "BLEND", PBR: &glbPBR{BaseColorFactor: []float64{1, 1, 1, 0.5}, MetallicFactor: 0, RoughnessFactor: 0.8}}},
		{"shiny material", `Appearance { material Material { name "shiny" diffuseColor 0.2 0.4 0.6 shininess 1 } }`,
			glbMaterial{Name: "shiny", DoubleSided: true, PBR: &glbPBR{BaseColorFactor: []float64{0.2, 0.4, 0.6, 1}, MetallicFactor: 0, RoughnessFactor: 0}}},
		{"emissive short ignored", `Appearance { material Material { name "e" emissiveColor 1 1 } }`,
			glbMaterial{Name: "e", DoubleSided: true, PBR: &glbPBR{BaseColorFactor: []float64{1, 1, 1, 1}, MetallicFactor: 0, RoughnessFactor: 0.8}}},
		{"material without name", `Appearance { material Material { diffuseColor 0 0 0 } }`,
			glbMaterial{DoubleSided: true, PBR: &glbPBR{BaseColorFactor: []float64{0, 0, 0, 1}, MetallicFactor: 0, RoughnessFactor: 0.8}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			src := `Shape { appearance ` + c.appearance + ` geometry Mesh { url "m" } }`
			doc, _ := exportGLB(t, sceneFromXAP(t, src, map[string]*MeshData{"m": m}, nil))
			if len(doc.Materials) != 1 {
				t.Fatalf("materials = %+v, want one", doc.Materials)
			}
			got := doc.Materials[0]
			if got.Name != c.want.Name || got.DoubleSided != c.want.DoubleSided || got.AlphaMode != c.want.AlphaMode || got.EmissiveFactor != nil {
				t.Errorf("material = %+v, want %+v", got, c.want)
			}
			if got.PBR == nil {
				t.Fatal("material has no pbrMetallicRoughness")
			}
			assertVec(t, "baseColorFactor", got.PBR.BaseColorFactor, c.want.PBR.BaseColorFactor)
			if got.PBR.MetallicFactor != 0 || math.Abs(got.PBR.RoughnessFactor-c.want.PBR.RoughnessFactor) > 1e-9 {
				t.Errorf("metallic %v roughness %v, want 0 and %v", got.PBR.MetallicFactor, got.PBR.RoughnessFactor, c.want.PBR.RoughnessFactor)
			}
			if got.PBR.BaseColorTexture != nil {
				t.Errorf("baseColorTexture = %+v, want none", got.PBR.BaseColorTexture)
			}
			if doc.Meshes[0].Primitives[0].Material == nil || *doc.Meshes[0].Primitives[0].Material != 0 {
				t.Errorf("primitive material = %v, want 0", doc.Meshes[0].Primitives[0].Material)
			}
		})
	}
}

func TestExportGLBShapeWithoutAppearanceHasNoMaterial(t *testing.T) {
	m := testMesh("m", 3)
	m.Indices = []uint16{0, 1, 2}
	src := `Shape { appearance Appearance { } geometry Mesh { url "m" } }`
	doc, _ := exportGLB(t, sceneFromXAP(t, src, map[string]*MeshData{"m": m}, nil))
	if len(doc.Materials) != 0 || doc.Meshes[0].Primitives[0].Material != nil {
		t.Errorf("materials = %+v primitive material %v, want none", doc.Materials, doc.Meshes[0].Primitives[0].Material)
	}
}

func TestExportGLBTextureMatching(t *testing.T) {
	m := testMesh("m", 3)
	m.Indices = []uint16{0, 1, 2}
	red := pngBytes(t, 1, 1, color.NRGBA{R: 255, A: 255})
	blue := pngBytes(t, 1, 2, color.NRGBA{B: 255, A: 255})
	textures := []*TextureData{{Name: "tex1", PNGData: red, Width: 1, Height: 1}, {Name: "Other", PNGData: blue, Width: 1, Height: 2}}
	src := `Group { children [
		Shape { appearance Appearance { texture ImageTexture { url "Textures\Tex1.XBX" } } geometry Mesh { url "m" } }
		Shape { appearance Appearance { texture ImageTexture { url "sub/dir/other.png" } } geometry Mesh { url "m" } }
		Shape { appearance Appearance { texture ImageTexture { url "tex1" } } geometry Mesh { url "m" } }
		Shape { appearance Appearance { texture ImageTexture { url "missing.xbx" } } geometry Mesh { url "m" } }
	] }`
	doc, bin := exportGLB(t, sceneFromXAP(t, src, map[string]*MeshData{"m": m}, textures))
	if len(doc.Images) != 2 || doc.Images[0].Name != "tex1" || doc.Images[1].Name != "Other" {
		t.Fatalf("images = %+v", doc.Images)
	}
	if len(doc.Textures) != 2 || doc.Textures[1].Source != 1 || doc.Textures[1].Sampler != 0 {
		t.Errorf("textures = %+v", doc.Textures)
	}
	redView, blueView := doc.BufferViews[doc.Images[0].BufferView], doc.BufferViews[doc.Images[1].BufferView]
	if redView != (glbBufferView{0, 0, len(red), 0}) || blueView != (glbBufferView{0, pad4(len(red)), len(blue), 0}) {
		t.Errorf("image views = %+v and %+v", redView, blueView)
	}
	if !bytes.Equal(bin[redView.ByteOffset:redView.ByteOffset+len(red)], red) || !bytes.Equal(bin[blueView.ByteOffset:blueView.ByteOffset+len(blue)], blue) {
		t.Errorf("BIN does not hold the PNG bytes at the image views")
	}
	if len(doc.Materials) != 4 {
		t.Fatalf("materials = %+v, want four", doc.Materials)
	}
	wantIndex := []*int{intp(0), intp(1), intp(0), nil}
	for i, want := range wantIndex {
		got := doc.Materials[i].PBR.BaseColorTexture
		switch {
		case want == nil && got != nil:
			t.Errorf("material %d references texture %d, want none", i, got.Index)
		case want != nil && (got == nil || got.Index != *want):
			t.Errorf("material %d texture = %+v, want %d", i, got, *want)
		}
	}
	if len(doc.Meshes) != 4 {
		t.Errorf("meshes = %d, want one per texture url", len(doc.Meshes))
	}
}

func TestExportGLBMeshDataAlignedAfterTextures(t *testing.T) {
	m := testMesh("m", 3)
	m.Indices = []uint16{0, 1, 2}
	textures := []*TextureData{{Name: "odd", PNGData: []byte{1, 2, 3, 4, 5, 6, 7}, Width: 1, Height: 1}}
	s := sceneFromXAP(t, `Mesh { url "m" }`, map[string]*MeshData{"m": m}, textures)
	doc, bin := exportGLB(t, s)
	if len(doc.BufferViews) != 5 || doc.BufferViews[0] != (glbBufferView{0, 0, 7, 0}) {
		t.Fatalf("bufferViews = %+v, want the image view then four mesh views", doc.BufferViews)
	}
	for i, bv := range doc.BufferViews[1:] {
		if bv.ByteOffset%4 != 0 {
			t.Errorf("mesh bufferView %d starts at byte %d, which is not 4 byte aligned", i+1, bv.ByteOffset)
		}
	}
	end := assertMeshBuffers(t, doc, bin, 1, 0, pad4(7), 3, m.Indices)
	if len(bin) != end || doc.Buffers[0].ByteLength != end {
		t.Errorf("BIN is %d bytes, buffer says %d, want %d", len(bin), doc.Buffers[0].ByteLength, end)
	}
}

func TestExportGLBErrors(t *testing.T) {
	m := testMesh("m", 3)
	m.Indices = []uint16{0, 1, 2}
	cases := []struct {
		name  string
		scene *Scene
		want  string
	}{
		{"no meshes", sceneFromXAP(t, `Shape { geometry Mesh { url "m" } }`, map[string]*MeshData{}, nil), "no meshes to export"},
		{"nil meshes", sceneFromXAP(t, `Shape { geometry Mesh { url "m" } }`, nil, nil), "no meshes to export"},
		{"no resolved geometry", sceneFromXAP(t, `Shape { geometry Mesh { url "m" } }`, map[string]*MeshData{"m": {Name: "m"}}, nil), "no resolved mesh data"},
		{"no scene nodes", sceneFromXAP(t, `Transform { }`, map[string]*MeshData{"m": m}, nil), "no nodes in scene graph"},
		{"mesh url unresolved", sceneFromXAP(t, `Shape { geometry Mesh { url "other" } }`, map[string]*MeshData{"m": m}, nil), "no nodes in scene graph"},
		{"mesh with no geometry skipped", sceneFromXAP(t, `Shape { geometry Mesh { url "empty" } }`, map[string]*MeshData{"m": m, "empty": {Name: "empty"}}, nil), "no nodes in scene graph"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := ExportGLB(&buf, c.scene)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want one containing %q", err, c.want)
			}
			if buf.Len() != 0 {
				t.Errorf("wrote %d bytes before failing", buf.Len())
			}
		})
	}
}

func TestExportGLBMeshWithoutPositionIsWarned(t *testing.T) {
	m := testMesh("Meshes/m.xbg", 3)
	m.Indices = []uint16{0, 1, 2}
	m.Attributes = m.Attributes[1:]
	s := sceneFromXAP(t, `Shape { geometry Mesh { url "Meshes/m.xbg" } }`, map[string]*MeshData{"Meshes/m.xbg": m}, nil)
	var buf bytes.Buffer
	err := ExportGLB(&buf, s)
	if err == nil || !strings.Contains(err.Error(), "no nodes in scene graph") {
		t.Fatalf("err = %v", err)
	}
	if len(s.Warnings) != 1 || !strings.Contains(s.Warnings[0], `mesh "Meshes/m.xbg" has no position attribute`) {
		t.Errorf("Warnings = %v", s.Warnings)
	}
}

type failingWriter struct{ err error }

func (f failingWriter) Write([]byte) (int, error) { return 0, f.err }

func TestExportGLBWriteError(t *testing.T) {
	m := testMesh("m", 3)
	m.Indices = []uint16{0, 1, 2}
	s := sceneFromXAP(t, `Mesh { url "m" }`, map[string]*MeshData{"m": m}, nil)
	wantErr := errors.New("disk full")
	if err := ExportGLB(failingWriter{err: wantErr}, s); !errors.Is(err, wantErr) {
		t.Errorf("err = %v, want %v", err, wantErr)
	}
}

func TestExportGLBFromLoadedScene(t *testing.T) {
	_, xapPath := writeMainScene(t)
	s, err := Load(xapPath, testLayouts())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	doc, bin := exportGLB(t, s)
	if len(doc.Nodes) != 3 || len(doc.Meshes) != 2 || len(doc.Materials) != 1 || len(doc.Images) != 1 {
		t.Fatalf("%d nodes %d meshes %d materials %d images", len(doc.Nodes), len(doc.Meshes), len(doc.Materials), len(doc.Images))
	}
	if doc.Materials[0].PBR.BaseColorTexture == nil || doc.Materials[0].PBR.BaseColorTexture.Index != 0 {
		t.Errorf("material texture = %+v, want the decoded tex1", doc.Materials[0].PBR.BaseColorTexture)
	}
	pngLen := len(s.Textures[0].PNGData)
	if !bytes.Equal(bin[:pngLen], s.Textures[0].PNGData) {
		t.Error("BIN does not start with the decoded texture PNG")
	}
	next := assertMeshBuffers(t, doc, bin, 1, 0, pad4(pngLen), 4, []uint16{0, 1, 2, 2, 1, 3})
	if got := floatsAt(bin, next, 9); !reflect.DeepEqual(got, poolRange("position", 4, 6)) {
		t.Errorf("shared positions = %v, want pool vertices 4 to 6", got)
	}
	if doc.Buffers[0].ByteLength != pad4(pngLen)+244 || len(bin) != pad4(pngLen)+244 {
		t.Errorf("BIN is %d bytes, want %d", len(bin), pad4(pngLen)+244)
	}
}

func TestAxisAngleToQuat(t *testing.T) {
	h := math.Sqrt(2) / 2
	cases := []struct {
		in   [4]float64
		want []float64
	}{
		{[4]float64{1, 0, 0, 0}, []float64{0, 0, 0, 1}},
		{[4]float64{0, 0, 1, math.Pi}, []float64{0, 0, 1, 0}},
		{[4]float64{0, 1, 0, math.Pi / 2}, []float64{0, h, 0, h}},
		{[4]float64{0, 0, 1, -math.Pi / 2}, []float64{0, 0, -h, h}},
		{[4]float64{2, 0, 0, math.Pi}, []float64{2, 0, 0, 0}},
	}
	for _, c := range cases {
		assertVec(t, "axisAngleToQuat", axisAngleToQuat(c.in), c.want)
	}
}

func TestQuatMul(t *testing.T) {
	h := math.Sqrt(2) / 2
	identity := []float64{0, 0, 0, 1}
	i, j, k := []float64{1, 0, 0, 0}, []float64{0, 1, 0, 0}, []float64{0, 0, 1, 0}
	z90 := []float64{0, 0, h, h}
	assertVec(t, "identity * z90", quatMul(identity, z90), z90)
	assertVec(t, "z90 * identity", quatMul(z90, identity), z90)
	assertVec(t, "z90 * z90", quatMul(z90, z90), k)
	assertVec(t, "i * j", quatMul(i, j), k)
	assertVec(t, "j * i", quatMul(j, i), []float64{0, 0, -1, 0})
	assertVec(t, "i * i", quatMul(i, i), []float64{0, 0, 0, -1})
}

func TestComputeBounds(t *testing.T) {
	minP, maxP := computeBounds(nil)
	if minP != [3]float32{} || maxP != [3]float32{} {
		t.Errorf("empty bounds = %v %v", minP, maxP)
	}
	verts := []buffer.Vertex{
		{Pos: [3]float32{1, -2, 3}},
		{Pos: [3]float32{-4, 5, 0}},
		{Pos: [3]float32{2, 2, -6}},
	}
	minP, maxP = computeBounds(verts)
	if minP != [3]float32{-4, -2, -6} || maxP != [3]float32{2, 5, 3} {
		t.Errorf("bounds = %v %v, want [-4 -2 -6] [2 5 3]", minP, maxP)
	}
	minP, maxP = computeBounds(verts[:1])
	if minP != verts[0].Pos || maxP != verts[0].Pos {
		t.Errorf("single vertex bounds = %v %v", minP, maxP)
	}
}

func TestEncodeBuffers(t *testing.T) {
	verts := []buffer.Vertex{
		{Pos: [3]float32{1, 2, 3}, Normal: [3]float32{0, 0, 1}, UV: [2]float32{0.5, -1}},
		{Pos: [3]float32{-1, 0, 0.25}, Normal: [3]float32{1, 0, 0}, UV: [2]float32{0, 1}},
	}
	pos := encodePositions(verts)
	if len(pos) != 24 || !reflect.DeepEqual(floatsAt(pos, 0, 6), []float32{1, 2, 3, -1, 0, 0.25}) {
		t.Errorf("positions = %v", floatsAt(pos, 0, len(pos)/4))
	}
	norm := encodeNormals(verts)
	if len(norm) != 24 || !reflect.DeepEqual(floatsAt(norm, 0, 6), []float32{0, 0, 1, 1, 0, 0}) {
		t.Errorf("normals = %v", floatsAt(norm, 0, len(norm)/4))
	}
	uv := encodeUVs(verts)
	if len(uv) != 16 || !reflect.DeepEqual(floatsAt(uv, 0, 4), []float32{0.5, -1, 0, 1}) {
		t.Errorf("uvs = %v", floatsAt(uv, 0, len(uv)/4))
	}
	idx := encodeIndices([]uint16{0, 0x1234, 0xFFFF})
	if !bytes.Equal(idx, []byte{0, 0, 0x34, 0x12, 0xFF, 0xFF}) {
		t.Errorf("indices = %v", idx)
	}
	if len(encodePositions(nil)) != 0 || len(encodeIndices(nil)) != 0 {
		t.Error("empty input encodes to bytes")
	}
}
