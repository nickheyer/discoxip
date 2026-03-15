package scene

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math"

	"github.com/nickheyer/discoxip/pkg/buffer"
)

// glTF 2.0 JSON structures
type gltfRoot struct {
	Asset       gltfAsset        `json:"asset"`
	Scene       int              `json:"scene"`
	Scenes      []gltfScene      `json:"scenes"`
	Nodes       []gltfNode       `json:"nodes"`
	Meshes      []gltfMesh       `json:"meshes"`
	Accessors   []gltfAccessor   `json:"accessors"`
	BufferViews []gltfBufferView `json:"bufferViews"`
	Buffers     []gltfBuffer     `json:"buffers"`
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
	Name       string           `json:"name"`
	Primitives []gltfPrimitive  `json:"primitives"`
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

// ExportGLB writes the scene as a binary glTF 2.0 (.glb) file.
func ExportGLB(w io.Writer, s *Scene) error {
	if len(s.Meshes) == 0 {
		return fmt.Errorf("scene: no meshes to export")
	}

	// Collect all unique mesh data
	var meshDatas []*MeshData
	for _, md := range s.Meshes {
		if len(md.Vertices) > 0 {
			meshDatas = append(meshDatas, md)
		}
	}

	if len(meshDatas) == 0 {
		return fmt.Errorf("scene: no resolved mesh data")
	}

	// For now, export the first mesh with data
	md := meshDatas[0]
	return exportSingleMeshGLB(w, md)
}

func exportSingleMeshGLB(w io.Writer, md *MeshData) error {
	// Build binary buffer: positions + normals + UVs + indices
	var binBuf []byte
	posOffset := len(binBuf)
	posData := encodePositions(md.Vertices)
	binBuf = append(binBuf, posData...)

	normOffset := len(binBuf)
	normData := encodeNormals(md.Vertices)
	binBuf = append(binBuf, normData...)

	uvOffset := len(binBuf)
	uvData := encodeUVs(md.Vertices)
	binBuf = append(binBuf, uvData...)

	idxOffset := len(binBuf)
	idxData := encodeIndices(md.Indices)
	binBuf = append(binBuf, idxData...)

	// Pad binary buffer to 4-byte alignment
	for len(binBuf)%4 != 0 {
		binBuf = append(binBuf, 0)
	}

	// Compute bounds
	minPos, maxPos := computeBounds(md.Vertices)

	// Build glTF JSON
	indicesAccessor := 3
	meshIdx := 0

	root := gltfRoot{
		Asset: gltfAsset{Version: "2.0", Generator: "discoxip"},
		Scene: 0,
		Scenes: []gltfScene{{Nodes: []int{0}}},
		Nodes: []gltfNode{{Name: md.Name, Mesh: &meshIdx}},
		Meshes: []gltfMesh{{
			Name: md.Name,
			Primitives: []gltfPrimitive{{
				Attributes: map[string]int{
					"POSITION": 0,
					"NORMAL":   1,
					"TEXCOORD_0": 2,
				},
				Indices: &indicesAccessor,
			}},
		}},
		BufferViews: []gltfBufferView{
			{Buffer: 0, ByteOffset: posOffset, ByteLength: len(posData), Target: 34962},
			{Buffer: 0, ByteOffset: normOffset, ByteLength: len(normData), Target: 34962},
			{Buffer: 0, ByteOffset: uvOffset, ByteLength: len(uvData), Target: 34962},
			{Buffer: 0, ByteOffset: idxOffset, ByteLength: len(idxData), Target: 34963},
		},
		Accessors: []gltfAccessor{
			{BufferView: 0, ComponentType: 5126, Count: len(md.Vertices), Type: "VEC3",
				Min: []float64{float64(minPos[0]), float64(minPos[1]), float64(minPos[2])},
				Max: []float64{float64(maxPos[0]), float64(maxPos[1]), float64(maxPos[2])}},
			{BufferView: 1, ComponentType: 5126, Count: len(md.Vertices), Type: "VEC3"},
			{BufferView: 2, ComponentType: 5126, Count: len(md.Vertices), Type: "VEC2"},
			{BufferView: 3, ComponentType: 5123, Count: len(md.Indices), Type: "SCALAR"},
		},
		Buffers: []gltfBuffer{{ByteLength: len(binBuf)}},
	}

	jsonData, err := json.Marshal(root)
	if err != nil {
		return fmt.Errorf("scene: encoding glTF JSON: %w", err)
	}

	// Pad JSON to 4-byte alignment
	for len(jsonData)%4 != 0 {
		jsonData = append(jsonData, ' ')
	}

	// Write GLB header
	totalSize := 12 + 8 + len(jsonData) + 8 + len(binBuf)
	header := make([]byte, 12)
	copy(header[0:4], []byte("glTF"))
	binary.LittleEndian.PutUint32(header[4:8], 2) // version
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

	// Binary chunk
	binary.LittleEndian.PutUint32(chunkHeader[0:4], uint32(len(binBuf)))
	binary.LittleEndian.PutUint32(chunkHeader[4:8], 0x004E4942) // "BIN\0"
	if _, err := w.Write(chunkHeader); err != nil {
		return err
	}
	if _, err := w.Write(binBuf); err != nil {
		return err
	}

	return nil
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
