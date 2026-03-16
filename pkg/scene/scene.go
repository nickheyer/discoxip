package scene

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/nickheyer/discoxip/pkg/buffer"
	"github.com/nickheyer/discoxip/pkg/mesh"
	"github.com/nickheyer/discoxip/pkg/texture"
	"github.com/nickheyer/discoxip/pkg/xap"
)

// MeshData holds resolved vertex and index data for a mesh reference.
type MeshData struct {
	Name     string
	Vertices []buffer.Vertex
	Indices  []uint16
}

// BufferPool is a named VB/IB pair discovered in the scene directory.
type BufferPool struct {
	Name     string
	VB       *buffer.VBReader
	IB       *buffer.IBReader
	MeshData *MeshData // resolved geometry (nil if VB format unknown)
}

// meshManifestEntry mirrors extract.MeshManifestEntry for JSON decoding.
type meshManifestEntry struct {
	Pool       string `json:"pool"`
	IndexStart int    `json:"index_start"`
	TriCount   int    `json:"tri_count"`
	Archive    string `json:"archive,omitempty"`
}

// TextureData holds a decoded texture ready for embedding in glTF.
type TextureData struct {
	Name    string // base filename without extension
	PNGData []byte // encoded PNG bytes
	Width   int
	Height  int
}

// Scene is a resolved scene graph with mesh data loaded.
type Scene struct {
	XAP      *xap.Scene
	Pools    []*BufferPool        // all discovered VB/IB pools
	Meshes   map[string]*MeshData // mesh URL → resolved geometry
	Textures []*TextureData       // all discovered and decoded textures
	Dir      string               // base directory for resolving paths
	Warnings []string
}

// Load parses a XAP scene file and resolves mesh references from the same directory.
func Load(xapPath string) (*Scene, error) {
	ast, err := xap.ParseFile(xapPath)
	if err != nil {
		return nil, fmt.Errorf("scene: parsing XAP: %w", err)
	}

	dir := filepath.Dir(xapPath)
	s := &Scene{
		XAP:    ast,
		Meshes: make(map[string]*MeshData),
		Dir:    dir,
	}

	// Propagate parser warnings
	s.Warnings = append(s.Warnings, ast.Warnings...)

	// Discover all VB/IB pools
	s.Pools = discoverPools(dir)
	for _, pool := range s.Pools {
		if pool.VB.Vertices != nil && pool.IB != nil {
			pool.MeshData = &MeshData{
				Name:     pool.Name,
				Vertices: pool.VB.Vertices,
				Indices:  pool.IB.Indices,
			}
		}
	}

	// Discover and decode textures
	s.Textures = discoverTextures(dir, &s.Warnings)

	// Load mesh manifest (written by extract) for correct pool/sub-range mapping
	manifest := loadManifest(dir)

	// Detect archive name from XAP filename.
	// Multi-archive extraction prefixes XAPs: "mainmenu5_default.xap" → archive "mainmenu5".
	// Manifest keys are then "mainmenu5:meshname.xm".
	archiveName := detectArchiveName(xapPath, manifest)

	// Resolve mesh references from the XAP scene graph.
	meshRefs := ast.MeshRefs()
	for _, url := range meshRefs {
		if _, ok := s.Meshes[url]; ok {
			continue
		}
		s.Meshes[url] = s.resolveMesh(url, manifest, archiveName)
	}




	// Apply vertex colors from .xm files to resolved meshes
	s.applyVertexColors()

	return s, nil
}

// detectArchiveName tries to determine which archive a XAP came from by
// checking if the manifest has entries prefixed with "archiveName:".
// Returns empty string if no archive prefix is detected (single-archive mode).
func detectArchiveName(xapPath string, manifest map[string]meshManifestEntry) string {
	base := filepath.Base(xapPath)
	ext := filepath.Ext(base)
	name := strings.TrimSuffix(base, ext)

	// Try splitting "mainmenu5_default" → archiveName="mainmenu5"
	// Look for the longest prefix that matches manifest archive names.
	for i := len(name) - 1; i > 0; i-- {
		if name[i] == '_' {
			candidate := name[:i]
			prefix := candidate + ":"
			for k := range manifest {
				if strings.HasPrefix(k, prefix) {
					return candidate
				}
			}
		}
	}

	// No prefix — check if manifest uses unprefixed keys (single-archive mode)
	return ""
}

// resolveMesh attempts to find geometry for a mesh URL.
func (s *Scene) resolveMesh(url string, manifest map[string]meshManifestEntry, archiveName string) *MeshData {
	md := &MeshData{Name: url}

	// Strategy 1: Use mesh manifest (from XIP extraction) for exact pool/range mapping.
	// Try archive-prefixed key first, then plain key.
	entry, ok := meshManifestEntry{}, false
	if archiveName != "" {
		entry, ok = manifest[archiveName+":"+url]
	}
	if !ok {
		entry, ok = manifest[url]
	}
	if ok {
		poolName := entry.Pool
		pool := s.findPool(poolName)
		if pool != nil && pool.VB.Vertices != nil && pool.IB != nil {
			indexCount := entry.TriCount * 3
			indexEnd := entry.IndexStart + indexCount
			if indexEnd <= len(pool.IB.Indices) {
				subIndices := pool.IB.Indices[entry.IndexStart:indexEnd]
				md.Vertices, md.Indices = extractSubMesh(pool.VB.Vertices, subIndices)
				return md
			}
			s.Warnings = append(s.Warnings,
				fmt.Sprintf("mesh %q: index range [%d,%d) exceeds pool %q IB (%d indices)",
					url, entry.IndexStart, indexEnd, poolName, len(pool.IB.Indices)))
		} else {
			s.Warnings = append(s.Warnings,
				fmt.Sprintf("mesh %q: manifest references pool %q but pool not found or not decoded", url, poolName))
		}
	}

	// Strategy 2 (fallback): Direct name match
	base := strings.TrimSuffix(url, filepath.Ext(url))
	for _, pool := range s.Pools {
		if pool.Name == base && pool.MeshData != nil {
			md.Vertices = pool.MeshData.Vertices
			md.Indices = pool.MeshData.Indices
			return md
		}
	}

	// Strategy 3 (fallback): Single pool
	var resolvedPools []*BufferPool
	for _, pool := range s.Pools {
		if pool.MeshData != nil {
			resolvedPools = append(resolvedPools, pool)
		}
	}
	if len(resolvedPools) == 1 {
		pool := resolvedPools[0]
		md.Vertices = pool.MeshData.Vertices
		md.Indices = pool.MeshData.Indices
		return md
	}

	// No match found — return empty geometry rather than guessing wrong
	s.Warnings = append(s.Warnings,
		fmt.Sprintf("mesh %q: no matching pool found (%d pools available)", url, len(resolvedPools)))
	return md
}

// applyVertexColors loads .xm files from the scene directory and
// merges their RGBA vertex colors into the corresponding resolved meshes.
func (s *Scene) applyVertexColors() {
	for url, md := range s.Meshes {
		if len(md.Vertices) == 0 {
			continue
		}

		// Look for a matching .xm file in the scene directory
		xmPath := filepath.Join(s.Dir, url)
		xm, err := mesh.Open(xmPath)
		if err != nil {
			continue // no .xm file or unreadable — not an error
		}

		if xm.Binary == nil || len(xm.Binary.VertexColors) == 0 {
			continue
		}

		colors := xm.Binary.VertexColors
		if len(colors) < len(md.Vertices) {
			s.Warnings = append(s.Warnings,
				fmt.Sprintf("mesh %q: .xm has %d vertex colors but mesh has %d vertices",
					url, len(colors), len(md.Vertices)))
			continue
		}

		for i := range md.Vertices {
			c := colors[i]
			md.Vertices[i].Color = [4]float32{
				float32(c.R) / 255.0,
				float32(c.G) / 255.0,
				float32(c.B) / 255.0,
				float32(c.A) / 255.0,
			}
			md.Vertices[i].HasColor = true
		}
	}
}

// findPool returns the pool with the given name, or nil.
func (s *Scene) findPool(name string) *BufferPool {
	for _, pool := range s.Pools {
		if pool.Name == name {
			return pool
		}
	}
	return nil
}

// extractSubMesh extracts vertices referenced by a sub-range of indices,
// returning compacted vertices and remapped indices.
func extractSubMesh(poolVerts []buffer.Vertex, indices []uint16) ([]buffer.Vertex, []uint16) {
	if len(indices) == 0 {
		return nil, nil
	}

	// Find min/max to check if vertices are contiguous
	minIdx, maxIdx := indices[0], indices[0]
	for _, idx := range indices[1:] {
		if idx < minIdx {
			minIdx = idx
		}
		if idx > maxIdx {
			maxIdx = idx
		}
	}

	// Bounds check
	if int(maxIdx) >= len(poolVerts) {
		return nil, nil
	}

	// Extract the contiguous vertex range and remap indices
	verts := make([]buffer.Vertex, maxIdx-minIdx+1)
	copy(verts, poolVerts[minIdx:maxIdx+1])

	remapped := make([]uint16, len(indices))
	for i, idx := range indices {
		remapped[i] = idx - minIdx
	}

	return verts, remapped
}

// loadManifest reads _meshes.json from the scene directory.
// Returns nil map if not found.
func loadManifest(dir string) map[string]meshManifestEntry {
	path := filepath.Join(dir, "_meshes.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	var manifest map[string]meshManifestEntry
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil
	}
	return manifest
}

// discoverTextures finds and decodes .xbx texture files in the directory.
func discoverTextures(dir string, warnings *[]string) []*TextureData {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	var textures []*TextureData
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if strings.ToLower(filepath.Ext(e.Name())) != ".xbx" {
			continue
		}
		path := filepath.Join(dir, e.Name())
		tex, err := texture.OpenXPR(path)
		if err != nil {
			*warnings = append(*warnings, fmt.Sprintf("texture %q: %v", e.Name(), err))
			continue
		}

		img, err := texture.Decode(tex)
		if err != nil {
			*warnings = append(*warnings, fmt.Sprintf("texture %q: decode: %v", e.Name(), err))
			continue
		}

		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err != nil {
			*warnings = append(*warnings, fmt.Sprintf("texture %q: png encode: %v", e.Name(), err))
			continue
		}

		baseName := strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))
		textures = append(textures, &TextureData{
			Name:    baseName,
			PNGData: buf.Bytes(),
			Width:   tex.Info.Width,
			Height:  tex.Info.Height,
		})
	}

	sort.Slice(textures, func(i, j int) bool {
		return textures[i].Name < textures[j].Name
	})

	return textures
}

// discoverPools finds VB/IB file pairs in the directory.
// Files are paired by base name (e.g., "~0.vb" + "~0.ib" = pool "~0").
func discoverPools(dir string) []*BufferPool {
	vbs := make(map[string]*buffer.VBReader)
	ibs := make(map[string]*buffer.IBReader)

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := filepath.Ext(e.Name())
		baseName := strings.TrimSuffix(e.Name(), ext)
		path := filepath.Join(dir, e.Name())

		switch ext {
		case ".vb":
			vb, err := buffer.OpenVB(path)
			if err == nil {
				vbs[baseName] = vb
			}
		case ".ib":
			ib, err := buffer.OpenIB(path)
			if err == nil {
				ibs[baseName] = ib
			}
		}
	}

	// Match VB/IB pairs by base name
	var pools []*BufferPool
	for name, vb := range vbs {
		pool := &BufferPool{Name: name, VB: vb}
		if ib, ok := ibs[name]; ok {
			pool.IB = ib
		}
		pools = append(pools, pool)
	}

	// Sort by name for deterministic ordering
	sort.Slice(pools, func(i, j int) bool {
		return pools[i].Name < pools[j].Name
	})

	return pools
}
