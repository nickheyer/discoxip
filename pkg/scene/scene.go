package scene

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/nickheyer/discoxip/pkg/buffer"
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
	Pool       int `json:"pool"`
	IndexStart int `json:"index_start"`
	TriCount   int `json:"tri_count"`
}

// Scene is a resolved scene graph with mesh data loaded.
type Scene struct {
	XAP      *xap.Scene
	Pools    []*BufferPool        // all discovered VB/IB pools
	Meshes   map[string]*MeshData // mesh URL → resolved geometry
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

	// Load mesh manifest (written by extract) for correct pool/sub-range mapping
	manifest := loadManifest(dir)

	// Resolve mesh references from the XAP scene graph.
	meshRefs := ast.MeshRefs()
	for _, url := range meshRefs {
		if _, ok := s.Meshes[url]; ok {
			continue
		}
		s.Meshes[url] = s.resolveMesh(url, manifest)
	}

	return s, nil
}

// resolveMesh attempts to find geometry for a mesh URL.
func (s *Scene) resolveMesh(url string, manifest map[string]meshManifestEntry) *MeshData {
	md := &MeshData{Name: url}

	// Strategy 1: Use mesh manifest (from XIP extraction) for exact pool/range mapping
	if entry, ok := manifest[url]; ok {
		poolName := fmt.Sprintf("~%d", entry.Pool)
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

	// Strategy 4 (fallback): Round-robin (last resort)
	if len(resolvedPools) > 0 {
		idx := len(s.Meshes) % len(resolvedPools)
		pool := resolvedPools[idx]
		md.Vertices = pool.MeshData.Vertices
		md.Indices = pool.MeshData.Indices
		s.Warnings = append(s.Warnings,
			fmt.Sprintf("mesh %q: assigned to pool %q (round-robin fallback)", url, pool.Name))
		return md
	}

	s.Warnings = append(s.Warnings, fmt.Sprintf("mesh %q: no VB/IB data found", url))
	return md
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
