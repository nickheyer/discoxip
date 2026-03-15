package scene

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math"

	"github.com/nickheyer/discoxip/pkg/buffer"
	"github.com/nickheyer/discoxip/pkg/xap"
)

// glTF 2.0 JSON structures
type gltfRoot struct {
	Asset       gltfAsset        `json:"asset"`
	Scene       int              `json:"scene"`
	Scenes      []gltfScene      `json:"scenes"`
	Nodes       []gltfNode       `json:"nodes"`
	Meshes      []gltfMesh       `json:"meshes,omitempty"`
	Accessors   []gltfAccessor   `json:"accessors,omitempty"`
	BufferViews []gltfBufferView `json:"bufferViews,omitempty"`
	Buffers     []gltfBuffer     `json:"buffers,omitempty"`
}

type gltfAsset struct {
	Version   string `json:"version"`
	Generator string `json:"generator"`
}

type gltfScene struct {
	Nodes []int `json:"nodes"`
}

type gltfNode struct {
	Name        string    `json:"name,omitempty"`
	Mesh        *int      `json:"mesh,omitempty"`
	Children    []int     `json:"children,omitempty"`
	Translation []float64 `json:"translation,omitempty"`
	Rotation    []float64 `json:"rotation,omitempty"`
	Scale       []float64 `json:"scale,omitempty"`
}

type gltfMesh struct {
	Name       string          `json:"name"`
	Primitives []gltfPrimitive `json:"primitives"`
}

type gltfPrimitive struct {
	Attributes map[string]int `json:"attributes"`
	Indices    *int           `json:"indices,omitempty"`
}

type gltfAccessor struct {
	BufferView    int       `json:"bufferView"`
	ByteOffset    int       `json:"byteOffset"`
	ComponentType int       `json:"componentType"`
	Count         int       `json:"count"`
	Type          string    `json:"type"`
	Max           []float64 `json:"max,omitempty"`
	Min           []float64 `json:"min,omitempty"`
}

type gltfBufferView struct {
	Buffer     int `json:"buffer"`
	ByteOffset int `json:"byteOffset"`
	ByteLength int `json:"byteLength"`
	Target     int `json:"target,omitempty"`
}

type gltfBuffer struct {
	ByteLength int `json:"byteLength"`
}

// glbBuilder accumulates glTF nodes, meshes, and binary data while walking the scene graph.
type glbBuilder struct {
	scene      *Scene
	root       gltfRoot
	binBuf     []byte
	meshIndex  map[string]int // mesh URL → glTF mesh index (dedup)
}

func newGLBBuilder(s *Scene) *glbBuilder {
	return &glbBuilder{
		scene:     s,
		meshIndex: make(map[string]int),
		root: gltfRoot{
			Asset: gltfAsset{Version: "2.0", Generator: "discoxip"},
		},
	}
}

// ExportGLB writes the scene as a binary glTF 2.0 (.glb) file.
// Walks the full XAP scene graph, preserving transform hierarchy and
// exporting all resolved meshes.
func ExportGLB(w io.Writer, s *Scene) error {
	if len(s.Meshes) == 0 {
		return fmt.Errorf("scene: no meshes to export")
	}

	// Check that at least some meshes have geometry
	hasGeometry := false
	for _, md := range s.Meshes {
		if len(md.Vertices) > 0 {
			hasGeometry = true
			break
		}
	}
	if !hasGeometry {
		return fmt.Errorf("scene: no resolved mesh data")
	}

	b := newGLBBuilder(s)

	// Walk the XAP scene graph and build glTF nodes
	var rootNodes []int
	for _, node := range s.XAP.Nodes {
		idx := b.addNode(node)
		if idx >= 0 {
			rootNodes = append(rootNodes, idx)
		}
	}

	if len(rootNodes) == 0 {
		return fmt.Errorf("scene: no nodes in scene graph")
	}

	b.root.Scene = 0
	b.root.Scenes = []gltfScene{{Nodes: rootNodes}}

	// Pad binary buffer to 4-byte alignment
	for len(b.binBuf)%4 != 0 {
		b.binBuf = append(b.binBuf, 0)
	}

	if len(b.binBuf) > 0 {
		b.root.Buffers = []gltfBuffer{{ByteLength: len(b.binBuf)}}
	}

	return b.writeGLB(w)
}

// addNode recursively converts an XAP node into glTF node(s).
// Returns the glTF node index, or -1 if the node produces nothing.
func (b *glbBuilder) addNode(n xap.Node) int {
	switch v := n.(type) {
	case *xap.Transform:
		return b.addTransformNode(v)
	case *xap.Shape:
		return b.addShapeNode(v)
	case *xap.MeshRef:
		return b.addMeshRefNode(v)
	default:
		return -1
	}
}

func (b *glbBuilder) addTransformNode(tf *xap.Transform) int {
	node := gltfNode{Name: tf.Name}

	// Convert VRML transform properties to glTF
	if tf.HasTranslation {
		node.Translation = tf.Translation[:]
	}
	if tf.HasRotation {
		// VRML rotation: axis(3) + angle(1) → glTF quaternion [x, y, z, w]
		node.Rotation = axisAngleToQuat(tf.Rotation)
	}
	if tf.HasScale {
		node.Scale = tf.Scale[:]
	}

	// Process children
	for _, child := range tf.Children {
		childIdx := b.addNode(child)
		if childIdx >= 0 {
			node.Children = append(node.Children, childIdx)
		}
	}

	// Only emit the node if it has children or transforms
	if len(node.Children) == 0 && node.Mesh == nil &&
		node.Translation == nil && node.Rotation == nil && node.Scale == nil {
		return -1
	}

	idx := len(b.root.Nodes)
	b.root.Nodes = append(b.root.Nodes, node)
	return idx
}

func (b *glbBuilder) addShapeNode(shape *xap.Shape) int {
	if shape.Geometry == nil || shape.Geometry.URL == "" {
		return -1
	}

	meshIdx := b.ensureMesh(shape.Geometry.URL, shape.Geometry.Name)
	if meshIdx < 0 {
		return -1
	}

	node := gltfNode{
		Name: shape.Geometry.Name,
		Mesh: &meshIdx,
	}

	idx := len(b.root.Nodes)
	b.root.Nodes = append(b.root.Nodes, node)
	return idx
}

func (b *glbBuilder) addMeshRefNode(ref *xap.MeshRef) int {
	if ref.URL == "" {
		return -1
	}

	meshIdx := b.ensureMesh(ref.URL, ref.Name)
	if meshIdx < 0 {
		return -1
	}

	node := gltfNode{
		Name: ref.Name,
		Mesh: &meshIdx,
	}

	idx := len(b.root.Nodes)
	b.root.Nodes = append(b.root.Nodes, node)
	return idx
}

// ensureMesh returns the glTF mesh index for a URL, creating it if needed.
func (b *glbBuilder) ensureMesh(url, name string) int {
	if idx, ok := b.meshIndex[url]; ok {
		return idx
	}

	md, ok := b.scene.Meshes[url]
	if !ok || len(md.Vertices) == 0 {
		return -1
	}

	meshIdx := b.buildMesh(md, name)
	b.meshIndex[url] = meshIdx
	return meshIdx
}

// buildMesh adds vertex/index data to the binary buffer and creates
// glTF mesh, accessor, and buffer view entries.
func (b *glbBuilder) buildMesh(md *MeshData, name string) int {
	if name == "" {
		name = md.Name
	}

	// Encode positions
	posOffset := len(b.binBuf)
	posData := encodePositions(md.Vertices)
	b.binBuf = append(b.binBuf, posData...)

	// Encode normals
	normOffset := len(b.binBuf)
	normData := encodeNormals(md.Vertices)
	b.binBuf = append(b.binBuf, normData...)

	// Encode UVs
	uvOffset := len(b.binBuf)
	uvData := encodeUVs(md.Vertices)
	b.binBuf = append(b.binBuf, uvData...)

	// Encode indices
	idxOffset := len(b.binBuf)
	idxData := encodeIndices(md.Indices)
	b.binBuf = append(b.binBuf, idxData...)

	// Align to 4 bytes after each mesh's data
	for len(b.binBuf)%4 != 0 {
		b.binBuf = append(b.binBuf, 0)
	}

	// Compute bounds
	minPos, maxPos := computeBounds(md.Vertices)

	// Buffer views
	bvBase := len(b.root.BufferViews)
	b.root.BufferViews = append(b.root.BufferViews,
		gltfBufferView{Buffer: 0, ByteOffset: posOffset, ByteLength: len(posData), Target: 34962},
		gltfBufferView{Buffer: 0, ByteOffset: normOffset, ByteLength: len(normData), Target: 34962},
		gltfBufferView{Buffer: 0, ByteOffset: uvOffset, ByteLength: len(uvData), Target: 34962},
		gltfBufferView{Buffer: 0, ByteOffset: idxOffset, ByteLength: len(idxData), Target: 34963},
	)

	// Accessors
	accBase := len(b.root.Accessors)
	b.root.Accessors = append(b.root.Accessors,
		gltfAccessor{
			BufferView: bvBase, ComponentType: 5126, Count: len(md.Vertices), Type: "VEC3",
			Min: []float64{float64(minPos[0]), float64(minPos[1]), float64(minPos[2])},
			Max: []float64{float64(maxPos[0]), float64(maxPos[1]), float64(maxPos[2])},
		},
		gltfAccessor{BufferView: bvBase + 1, ComponentType: 5126, Count: len(md.Vertices), Type: "VEC3"},
		gltfAccessor{BufferView: bvBase + 2, ComponentType: 5126, Count: len(md.Vertices), Type: "VEC2"},
		gltfAccessor{BufferView: bvBase + 3, ComponentType: 5123, Count: len(md.Indices), Type: "SCALAR"},
	)

	// Mesh primitive
	idxAccessor := accBase + 3
	meshIdx := len(b.root.Meshes)
	b.root.Meshes = append(b.root.Meshes, gltfMesh{
		Name: name,
		Primitives: []gltfPrimitive{{
			Attributes: map[string]int{
				"POSITION":   accBase,
				"NORMAL":     accBase + 1,
				"TEXCOORD_0": accBase + 2,
			},
			Indices: &idxAccessor,
		}},
	})

	return meshIdx
}

func (b *glbBuilder) writeGLB(w io.Writer) error {
	jsonData, err := json.Marshal(b.root)
	if err != nil {
		return fmt.Errorf("scene: encoding glTF JSON: %w", err)
	}

	// Pad JSON to 4-byte alignment with spaces
	for len(jsonData)%4 != 0 {
		jsonData = append(jsonData, ' ')
	}

	// GLB header (12 bytes) + JSON chunk (8 + data) + BIN chunk (8 + data)
	totalSize := 12 + 8 + len(jsonData)
	if len(b.binBuf) > 0 {
		totalSize += 8 + len(b.binBuf)
	}

	// GLB header
	header := make([]byte, 12)
	copy(header[0:4], []byte("glTF"))
	binary.LittleEndian.PutUint32(header[4:8], 2)
	binary.LittleEndian.PutUint32(header[8:12], uint32(totalSize))
	if _, err := w.Write(header); err != nil {
		return err
	}

	// JSON chunk
	chunkHeader := make([]byte, 8)
	binary.LittleEndian.PutUint32(chunkHeader[0:4], uint32(len(jsonData)))
	binary.LittleEndian.PutUint32(chunkHeader[4:8], 0x4E4F534A) // "JSON"
	if _, err := w.Write(chunkHeader); err != nil {
		return err
	}
	if _, err := w.Write(jsonData); err != nil {
		return err
	}

	// Binary chunk (only if there's data)
	if len(b.binBuf) > 0 {
		binary.LittleEndian.PutUint32(chunkHeader[0:4], uint32(len(b.binBuf)))
		binary.LittleEndian.PutUint32(chunkHeader[4:8], 0x004E4942) // "BIN\0"
		if _, err := w.Write(chunkHeader); err != nil {
			return err
		}
		if _, err := w.Write(b.binBuf); err != nil {
			return err
		}
	}

	return nil
}

// axisAngleToQuat converts VRML axis-angle rotation [ax, ay, az, angle]
// to glTF quaternion [qx, qy, qz, qw].
func axisAngleToQuat(r [4]float64) []float64 {
	halfAngle := r[3] / 2.0
	s := math.Sin(halfAngle)
	return []float64{
		r[0] * s,
		r[1] * s,
		r[2] * s,
		math.Cos(halfAngle),
	}
}

func encodePositions(verts []buffer.Vertex) []byte {
	buf := make([]byte, len(verts)*12)
	for i, v := range verts {
		binary.LittleEndian.PutUint32(buf[i*12:], math.Float32bits(v.Pos[0]))
		binary.LittleEndian.PutUint32(buf[i*12+4:], math.Float32bits(v.Pos[1]))
		binary.LittleEndian.PutUint32(buf[i*12+8:], math.Float32bits(v.Pos[2]))
	}
	return buf
}

func encodeNormals(verts []buffer.Vertex) []byte {
	buf := make([]byte, len(verts)*12)
	for i, v := range verts {
		binary.LittleEndian.PutUint32(buf[i*12:], math.Float32bits(v.Normal[0]))
		binary.LittleEndian.PutUint32(buf[i*12+4:], math.Float32bits(v.Normal[1]))
		binary.LittleEndian.PutUint32(buf[i*12+8:], math.Float32bits(v.Normal[2]))
	}
	return buf
}

func encodeUVs(verts []buffer.Vertex) []byte {
	buf := make([]byte, len(verts)*8)
	for i, v := range verts {
		binary.LittleEndian.PutUint32(buf[i*8:], math.Float32bits(v.UV[0]))
		binary.LittleEndian.PutUint32(buf[i*8+4:], math.Float32bits(v.UV[1]))
	}
	return buf
}

func encodeIndices(indices []uint16) []byte {
	buf := make([]byte, len(indices)*2)
	for i, idx := range indices {
		binary.LittleEndian.PutUint16(buf[i*2:], idx)
	}
	return buf
}

func computeBounds(verts []buffer.Vertex) ([3]float32, [3]float32) {
	if len(verts) == 0 {
		return [3]float32{}, [3]float32{}
	}
	minP := verts[0].Pos
	maxP := verts[0].Pos
	for _, v := range verts[1:] {
		for i := range 3 {
			if v.Pos[i] < minP[i] {
				minP[i] = v.Pos[i]
			}
			if v.Pos[i] > maxP[i] {
				maxP[i] = v.Pos[i]
			}
		}
	}
	return minP, maxP
}
