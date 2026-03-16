package scene

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"path/filepath"
	"strings"

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
	Materials   []gltfMaterial   `json:"materials,omitempty"`
	Textures    []gltfTexture    `json:"textures,omitempty"`
	Images      []gltfImage      `json:"images,omitempty"`
	Samplers    []gltfSampler    `json:"samplers,omitempty"`
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
	Material   *int           `json:"material,omitempty"`
}

type gltfMaterial struct {
	Name                 string    `json:"name"`
	PBRMetallicRoughness *gltfPBR  `json:"pbrMetallicRoughness,omitempty"`
	EmissiveFactor       []float64 `json:"emissiveFactor,omitempty"`
	AlphaMode            string    `json:"alphaMode,omitempty"`
	DoubleSided          bool      `json:"doubleSided,omitempty"`
}

type gltfPBR struct {
	BaseColorFactor  []float64       `json:"baseColorFactor,omitempty"`
	BaseColorTexture *gltfTextureRef `json:"baseColorTexture,omitempty"`
	MetallicFactor   float64         `json:"metallicFactor"`
	RoughnessFactor  float64         `json:"roughnessFactor"`
}

type gltfTextureRef struct {
	Index int `json:"index"`
}

type gltfTexture struct {
	Sampler int `json:"sampler"`
	Source  int `json:"source"`
}

type gltfImage struct {
	BufferView int    `json:"bufferView"`
	MimeType   string `json:"mimeType"`
	Name       string `json:"name,omitempty"`
}

type gltfSampler struct {
	MagFilter int `json:"magFilter,omitempty"`
	MinFilter int `json:"minFilter,omitempty"`
	WrapS     int `json:"wrapS,omitempty"`
	WrapT     int `json:"wrapT,omitempty"`
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
	scene         *Scene
	root          gltfRoot
	binBuf        []byte
	meshIndex     map[string]int // mesh key (url+material) → glTF mesh index
	materialIndex map[string]int // material name → glTF material index
	textureIndex  map[string]int // texture name → glTF texture index
}

func newGLBBuilder(s *Scene) *glbBuilder {
	return &glbBuilder{
		scene:         s,
		meshIndex:     make(map[string]int),
		materialIndex: make(map[string]int),
		textureIndex:  make(map[string]int),
		root: gltfRoot{
			Asset: gltfAsset{Version: "2.0", Generator: "discoxip"},
		},
	}
}

// ExportGLB writes the scene as a binary glTF 2.0 (.glb) file.
func ExportGLB(w io.Writer, s *Scene) error {
	if len(s.Meshes) == 0 {
		return fmt.Errorf("scene: no meshes to export")
	}

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

	// Embed textures first so textureIndex is populated when materials are created
	b.embedTextures()

	var rootNodes []int
	for _, node := range s.XAP.Nodes() {
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

	for len(b.binBuf)%4 != 0 {
		b.binBuf = append(b.binBuf, 0)
	}

	if len(b.binBuf) > 0 {
		b.root.Buffers = []gltfBuffer{{ByteLength: len(b.binBuf)}}
	}

	return b.writeGLB(w)
}

func (b *glbBuilder) addNode(n *xap.Node) int {
	if n == nil {
		return -1
	}

	switch n.TypeName {
	case "Transform":
		return b.addTransformNode(n)
	case "Shape":
		return b.addShapeNode(n)
	case "Mesh":
		return b.addMeshRefNode(n)
	default:
		return b.addGenericNode(n)
	}
}

func (b *glbBuilder) addTransformNode(n *xap.Node) int {
	node := gltfNode{Name: n.DefName}

	translation := n.Floats("translation")
	if len(translation) >= 3 {
		node.Translation = translation[:3]
	}

	rotation := n.Floats("rotation")
	scaleOri := n.Floats("scaleOrientation")
	scale := n.Floats("scale")

	if len(scaleOri) >= 4 && len(scale) >= 3 {
		soQuat := axisAngleToQuat([4]float64{scaleOri[0], scaleOri[1], scaleOri[2], scaleOri[3]})
		soInvQuat := []float64{-soQuat[0], -soQuat[1], -soQuat[2], soQuat[3]}

		if len(rotation) >= 4 {
			node.Rotation = quatMul(axisAngleToQuat([4]float64{rotation[0], rotation[1], rotation[2], rotation[3]}), soQuat)
		} else {
			node.Rotation = soQuat
		}

		innerNode := gltfNode{
			Scale:    scale[:3],
			Rotation: soInvQuat,
		}

		for _, child := range n.Children {
			childIdx := b.addNode(child)
			if childIdx >= 0 {
				innerNode.Children = append(innerNode.Children, childIdx)
			}
		}

		innerIdx := len(b.root.Nodes)
		b.root.Nodes = append(b.root.Nodes, innerNode)
		node.Children = []int{innerIdx}
	} else {
		if len(rotation) >= 4 {
			node.Rotation = axisAngleToQuat([4]float64{rotation[0], rotation[1], rotation[2], rotation[3]})
		}
		if len(scale) >= 3 {
			node.Scale = scale[:3]
		}

		for _, child := range n.Children {
			childIdx := b.addNode(child)
			if childIdx >= 0 {
				node.Children = append(node.Children, childIdx)
			}
		}
	}

	if len(node.Children) == 0 && node.Mesh == nil &&
		node.Translation == nil && node.Rotation == nil && node.Scale == nil &&
		node.Name == "" {
		return -1
	}

	idx := len(b.root.Nodes)
	b.root.Nodes = append(b.root.Nodes, node)
	return idx
}

func (b *glbBuilder) addGenericNode(n *xap.Node) int {
	node := gltfNode{Name: n.DefName}
	if node.Name == "" {
		node.Name = n.TypeName
	}

	for _, child := range n.Children {
		childIdx := b.addNode(child)
		if childIdx >= 0 {
			node.Children = append(node.Children, childIdx)
		}
	}

	if len(node.Children) == 0 {
		return -1
	}

	idx := len(b.root.Nodes)
	b.root.Nodes = append(b.root.Nodes, node)
	return idx
}

func (b *glbBuilder) addShapeNode(n *xap.Node) int {
	// Find geometry (node-valued field)
	geomNode := n.ChildNode("geometry")
	if geomNode == nil || geomNode.TypeName != "Mesh" {
		return -1
	}

	meshURL := geomNode.String("url")
	if meshURL == "" {
		return -1
	}

	// Find appearance → material
	appNode := n.ChildNode("appearance")
	var matNode *xap.Node
	texURL := ""
	if appNode != nil {
		matNode = appNode.ChildNode("material")
		texNode := appNode.ChildNode("texture")
		if texNode != nil {
			texURL = texNode.String("url")
		}
	}

	meshIdx := b.ensureMeshWithMaterial(meshURL, geomNode.DefName, matNode, texURL)
	if meshIdx < 0 {
		return -1
	}

	node := gltfNode{
		Name: geomNode.DefName,
		Mesh: &meshIdx,
	}

	idx := len(b.root.Nodes)
	b.root.Nodes = append(b.root.Nodes, node)
	return idx
}

func (b *glbBuilder) addMeshRefNode(n *xap.Node) int {
	url := n.String("url")
	if url == "" {
		return -1
	}

	meshIdx := b.ensureMesh(url, n.DefName, "", "")
	if meshIdx < 0 {
		return -1
	}

	node := gltfNode{
		Name: n.DefName,
		Mesh: &meshIdx,
	}

	idx := len(b.root.Nodes)
	b.root.Nodes = append(b.root.Nodes, node)
	return idx
}

// ensureMesh returns the glTF mesh index for a URL+material, creating it if needed.
func (b *glbBuilder) ensureMesh(url, name, matName, texURL string) int {
	return b.ensureMeshWithMaterial(url, name, nil, texURL)
}

// ensureMeshWithMaterial returns the glTF mesh index, using full material data when available.
func (b *glbBuilder) ensureMeshWithMaterial(url, name string, matNode *xap.Node, texURL string) int {
	matName := ""
	if matNode != nil {
		matName = matNode.String("name")
	}
	key := url + "\x00" + matName + "\x00" + texURL
	if idx, ok := b.meshIndex[key]; ok {
		return idx
	}

	md, ok := b.scene.Meshes[url]
	if !ok || len(md.Vertices) == 0 {
		return -1
	}

	var matIdx *int
	if matNode != nil || texURL != "" {
		idx := b.ensureMaterialFromXAP(matNode, texURL)
		matIdx = &idx
	}

	meshIdx := b.buildMesh(md, name, matIdx)
	b.meshIndex[key] = meshIdx
	return meshIdx
}

// buildMesh adds vertex/index data to the binary buffer and creates
// glTF mesh, accessor, and buffer view entries.
func (b *glbBuilder) buildMesh(md *MeshData, name string, matIdx *int) int {
	if name == "" {
		name = md.Name
	}

	posOffset := len(b.binBuf)
	posData := encodePositions(md.Vertices)
	b.binBuf = append(b.binBuf, posData...)

	normOffset := len(b.binBuf)
	normData := encodeNormals(md.Vertices)
	b.binBuf = append(b.binBuf, normData...)

	uvOffset := len(b.binBuf)
	uvData := encodeUVs(md.Vertices)
	b.binBuf = append(b.binBuf, uvData...)

	// Check if any vertices have color data
	hasColors := false
	for _, v := range md.Vertices {
		if v.HasColor {
			hasColors = true
			break
		}
	}

	var colorOffset int
	var colorData []byte
	if hasColors {
		colorOffset = len(b.binBuf)
		colorData = encodeColors(md.Vertices)
		b.binBuf = append(b.binBuf, colorData...)
	}

	idxOffset := len(b.binBuf)
	idxData := encodeIndices(md.Indices)
	b.binBuf = append(b.binBuf, idxData...)

	for len(b.binBuf)%4 != 0 {
		b.binBuf = append(b.binBuf, 0)
	}

	minPos, maxPos := computeBounds(md.Vertices)

	bvBase := len(b.root.BufferViews)
	b.root.BufferViews = append(b.root.BufferViews,
		gltfBufferView{Buffer: 0, ByteOffset: posOffset, ByteLength: len(posData), Target: 34962},
		gltfBufferView{Buffer: 0, ByteOffset: normOffset, ByteLength: len(normData), Target: 34962},
		gltfBufferView{Buffer: 0, ByteOffset: uvOffset, ByteLength: len(uvData), Target: 34962},
	)

	if hasColors {
		b.root.BufferViews = append(b.root.BufferViews,
			gltfBufferView{Buffer: 0, ByteOffset: colorOffset, ByteLength: len(colorData), Target: 34962},
		)
	}

	idxBVIdx := len(b.root.BufferViews)
	b.root.BufferViews = append(b.root.BufferViews,
		gltfBufferView{Buffer: 0, ByteOffset: idxOffset, ByteLength: len(idxData), Target: 34963},
	)

	accBase := len(b.root.Accessors)
	b.root.Accessors = append(b.root.Accessors,
		gltfAccessor{
			BufferView: bvBase, ComponentType: 5126, Count: len(md.Vertices), Type: "VEC3",
			Min: []float64{float64(minPos[0]), float64(minPos[1]), float64(minPos[2])},
			Max: []float64{float64(maxPos[0]), float64(maxPos[1]), float64(maxPos[2])},
		},
		gltfAccessor{BufferView: bvBase + 1, ComponentType: 5126, Count: len(md.Vertices), Type: "VEC3"},
		gltfAccessor{BufferView: bvBase + 2, ComponentType: 5126, Count: len(md.Vertices), Type: "VEC2"},
	)

	attrs := map[string]int{
		"POSITION":   accBase,
		"NORMAL":     accBase + 1,
		"TEXCOORD_0": accBase + 2,
	}

	nextAcc := accBase + 3
	if hasColors {
		b.root.Accessors = append(b.root.Accessors,
			gltfAccessor{BufferView: bvBase + 3, ComponentType: 5126, Count: len(md.Vertices), Type: "VEC4"},
		)
		attrs["COLOR_0"] = nextAcc
		nextAcc++
	}

	b.root.Accessors = append(b.root.Accessors,
		gltfAccessor{BufferView: idxBVIdx, ComponentType: 5123, Count: len(md.Indices), Type: "SCALAR"},
	)
	idxAccessor := nextAcc

	meshIdx := len(b.root.Meshes)
	b.root.Meshes = append(b.root.Meshes, gltfMesh{
		Name: name,
		Primitives: []gltfPrimitive{{
			Attributes: attrs,
			Indices:    &idxAccessor,
			Material:   matIdx,
		}},
	})

	return meshIdx
}

// ensureMaterialFromXAP returns the glTF material index, creating it from XAP material data.
func (b *glbBuilder) ensureMaterialFromXAP(matNode *xap.Node, texURL string) int {
	name := ""
	if matNode != nil {
		name = matNode.String("name")
	}
	key := name + "\x00" + texURL
	if idx, ok := b.materialIndex[key]; ok {
		return idx
	}

	mat := gltfMaterial{
		Name:        name,
		DoubleSided: true,
	}

	pbr := &gltfPBR{
		MetallicFactor:  0.0,
		RoughnessFactor: 0.8,
	}

	alpha := 1.0
	if matNode != nil {
		if transparency, ok := matNode.Float("transparency"); ok {
			alpha = 1.0 - transparency
		}
	}

	hasDiffuse := false
	if matNode != nil {
		diffuse := matNode.Floats("diffuseColor")
		if len(diffuse) >= 3 {
			hasDiffuse = true
			pbr.BaseColorFactor = []float64{diffuse[0], diffuse[1], diffuse[2], alpha}
		}
	}
	if !hasDiffuse {
		pbr.BaseColorFactor = []float64{1.0, 1.0, 1.0, alpha}
	}

	if alpha < 1.0 {
		mat.AlphaMode = "BLEND"
	}

	if matNode != nil {
		if shininess, ok := matNode.Float("shininess"); ok && shininess > 0 {
			pbr.RoughnessFactor = 1.0 - shininess
		}
		emissive := matNode.Floats("emissiveColor")
		if len(emissive) >= 3 {
			mat.EmissiveFactor = emissive[:3]
		}
	}

	// Match texture
	texIdx := b.findTextureByURL(texURL)
	if texIdx >= 0 {
		pbr.BaseColorTexture = &gltfTextureRef{Index: texIdx}
		if !hasDiffuse {
			pbr.BaseColorFactor = []float64{1.0, 1.0, 1.0, alpha}
		}
	}

	mat.PBRMetallicRoughness = pbr

	idx := len(b.root.Materials)
	b.root.Materials = append(b.root.Materials, mat)
	b.materialIndex[key] = idx
	return idx
}

// findTextureByURL matches a texture URL from the XAP against discovered textures.
func (b *glbBuilder) findTextureByURL(texURL string) int {
	if texURL == "" {
		return -1
	}

	base := texURL
	if idx := strings.LastIndex(base, "/"); idx >= 0 {
		base = base[idx+1:]
	}
	if idx := strings.LastIndex(base, "\\"); idx >= 0 {
		base = base[idx+1:]
	}
	ext := filepath.Ext(base)
	if ext != "" {
		base = base[:len(base)-len(ext)]
	}
	baseLower := strings.ToLower(base)

	for texName, texIdx := range b.textureIndex {
		if strings.ToLower(texName) == baseLower {
			return texIdx
		}
	}

	return -1
}

// embedTextures adds all discovered textures to the GLB binary buffer.
func (b *glbBuilder) embedTextures() {
	if len(b.scene.Textures) == 0 {
		return
	}

	samplerIdx := len(b.root.Samplers)
	b.root.Samplers = append(b.root.Samplers, gltfSampler{
		MagFilter: 9729,
		MinFilter: 9987,
		WrapS:     10497,
		WrapT:     10497,
	})

	for _, tex := range b.scene.Textures {
		for len(b.binBuf)%4 != 0 {
			b.binBuf = append(b.binBuf, 0)
		}

		imgOffset := len(b.binBuf)
		b.binBuf = append(b.binBuf, tex.PNGData...)

		bvIdx := len(b.root.BufferViews)
		b.root.BufferViews = append(b.root.BufferViews, gltfBufferView{
			Buffer:     0,
			ByteOffset: imgOffset,
			ByteLength: len(tex.PNGData),
		})

		imgIdx := len(b.root.Images)
		b.root.Images = append(b.root.Images, gltfImage{
			BufferView: bvIdx,
			MimeType:   "image/png",
			Name:       tex.Name,
		})

		texIdx := len(b.root.Textures)
		b.root.Textures = append(b.root.Textures, gltfTexture{
			Sampler: samplerIdx,
			Source:  imgIdx,
		})

		b.textureIndex[tex.Name] = texIdx
	}
}

func (b *glbBuilder) writeGLB(w io.Writer) error {
	jsonData, err := json.Marshal(b.root)
	if err != nil {
		return fmt.Errorf("scene: encoding glTF JSON: %w", err)
	}

	for len(jsonData)%4 != 0 {
		jsonData = append(jsonData, ' ')
	}

	totalSize := 12 + 8 + len(jsonData)
	if len(b.binBuf) > 0 {
		totalSize += 8 + len(b.binBuf)
	}

	header := make([]byte, 12)
	copy(header[0:4], []byte("glTF"))
	binary.LittleEndian.PutUint32(header[4:8], 2)
	binary.LittleEndian.PutUint32(header[8:12], uint32(totalSize))
	if _, err := w.Write(header); err != nil {
		return err
	}

	chunkHeader := make([]byte, 8)
	binary.LittleEndian.PutUint32(chunkHeader[0:4], uint32(len(jsonData)))
	binary.LittleEndian.PutUint32(chunkHeader[4:8], 0x4E4F534A)
	if _, err := w.Write(chunkHeader); err != nil {
		return err
	}
	if _, err := w.Write(jsonData); err != nil {
		return err
	}

	if len(b.binBuf) > 0 {
		binary.LittleEndian.PutUint32(chunkHeader[0:4], uint32(len(b.binBuf)))
		binary.LittleEndian.PutUint32(chunkHeader[4:8], 0x004E4942)
		if _, err := w.Write(chunkHeader); err != nil {
			return err
		}
		if _, err := w.Write(b.binBuf); err != nil {
			return err
		}
	}

	return nil
}

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

func quatMul(a, b []float64) []float64 {
	return []float64{
		a[3]*b[0] + a[0]*b[3] + a[1]*b[2] - a[2]*b[1],
		a[3]*b[1] - a[0]*b[2] + a[1]*b[3] + a[2]*b[0],
		a[3]*b[2] + a[0]*b[1] - a[1]*b[0] + a[2]*b[3],
		a[3]*b[3] - a[0]*b[0] - a[1]*b[1] - a[2]*b[2],
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

func encodeColors(verts []buffer.Vertex) []byte {
	buf := make([]byte, len(verts)*16)
	for i, v := range verts {
		binary.LittleEndian.PutUint32(buf[i*16:], math.Float32bits(v.Color[0]))
		binary.LittleEndian.PutUint32(buf[i*16+4:], math.Float32bits(v.Color[1]))
		binary.LittleEndian.PutUint32(buf[i*16+8:], math.Float32bits(v.Color[2]))
		binary.LittleEndian.PutUint32(buf[i*16+12:], math.Float32bits(v.Color[3]))
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
