package scene

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/nickheyer/discoxip/pkg/buffer"
	"github.com/nickheyer/discoxip/pkg/texture"
	"github.com/nickheyer/discoxip/pkg/xap"
)

// errMeshMissing marks a mesh URL that no extracted archive packs
var errMeshMissing = errors.New("packed in no extracted archive")

// Layout is the vertex declaration the executable binds to a vertex buffer format code.
type Layout struct {
	Stride    int
	Elements  []buffer.Element
	Semantics map[int]string // register to fixed function meaning
}

// MeshData holds resolved vertex attributes and indices for a mesh reference.
type MeshData struct {
	Name       string
	Format     uint32
	Count      int
	Attributes []buffer.Attribute
	Indices    []uint16
}

// BufferPool is a named VB/IB pair discovered in the scene directory.
type BufferPool struct {
	Name       string
	VB         *buffer.VBReader
	IB         *buffer.IBReader
	Attributes []buffer.Attribute
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
	Meshes   map[string]*MeshData // mesh URL to resolved geometry
	Textures []*TextureData       // all discovered and decoded textures
	Dir      string               // base directory for resolving paths
	Layouts  map[uint32]Layout
	Missing  []string // mesh URLs no extracted archive contains
	Warnings []string
}

// Load parses a XAP scene file and resolves every mesh reference through the manifest.
func Load(xapPath string, layouts map[uint32]Layout) (*Scene, error) {
	ast, err := xap.ParseFile(xapPath)
	if err != nil {
		return nil, fmt.Errorf("scene: parsing XAP: %w", err)
	}
	dir := filepath.Dir(xapPath)
	s := &Scene{XAP: ast, Meshes: make(map[string]*MeshData), Dir: dir, Layouts: layouts}
	s.Warnings = append(s.Warnings, ast.Warnings...)

	pools, err := discoverPools(dir, layouts)
	if err != nil {
		return nil, err
	}
	s.Pools = pools
	s.Textures = discoverTextures(dir, &s.Warnings)

	manifest, err := loadManifest(dir)
	if err != nil {
		return nil, err
	}
	archiveName := detectArchiveName(xapPath, manifest)
	for _, url := range ast.MeshRefs() {
		if _, ok := s.Meshes[url]; ok {
			continue
		}
		md, err := s.resolveMesh(url, manifest, archiveName)
		if err != nil {
			if errors.Is(err, errMeshMissing) {
				s.Missing = append(s.Missing, url)
				continue
			}
			return nil, err
		}
		s.Meshes[url] = md
	}
	return s, nil
}

// detectArchiveName determines which archive a XAP came from by matching manifest prefixes.
func detectArchiveName(xapPath string, manifest map[string]meshManifestEntry) string {
	base := filepath.Base(xapPath)
	name := strings.TrimSuffix(base, filepath.Ext(base))
	for i := len(name) - 1; i > 0; i-- {
		if name[i] != '_' {
			continue
		}
		prefix := name[:i] + ":"
		for k := range manifest {
			if strings.HasPrefix(k, prefix) {
				return name[:i]
			}
		}
	}
	return ""
}

// resolveMesh finds the pool sub range the manifest recorded for a mesh URL.
func (s *Scene) resolveMesh(url string, manifest map[string]meshManifestEntry, archiveName string) (*MeshData, error) {
	entry, ok := meshManifestEntry{}, false
	if archiveName != "" {
		entry, ok = manifest[archiveName+":"+url]
	}
	if !ok {
		entry, ok = manifest[url]
	}
	if !ok {
		// Scenes reference meshes packed in other archives by the same relative URL.
		var matches []string
		for k := range manifest {
			if strings.HasSuffix(k, ":"+url) {
				matches = append(matches, k)
			}
		}
		sort.Strings(matches)
		switch len(matches) {
		case 0:
			return nil, fmt.Errorf("scene: mesh %q: %w", url, errMeshMissing)
		case 1:
			entry = manifest[matches[0]]
		default:
			return nil, fmt.Errorf("scene: mesh %q is packed in several archives: %v", url, matches)
		}
	}
	pool := s.findPool(entry.Pool)
	if pool == nil {
		return nil, fmt.Errorf("scene: mesh %q references pool %q which was not extracted", url, entry.Pool)
	}
	if pool.IB == nil || pool.Attributes == nil {
		return nil, fmt.Errorf("scene: mesh %q references pool %q which has no decoded geometry", url, entry.Pool)
	}
	indexCount := entry.TriCount * 3
	indexEnd := entry.IndexStart + indexCount
	if indexEnd > len(pool.IB.Indices) {
		return nil, fmt.Errorf("scene: mesh %q index range [%d,%d) exceeds pool %q with %d indices", url, entry.IndexStart, indexEnd, entry.Pool, len(pool.IB.Indices))
	}
	md := &MeshData{Name: url, Format: pool.VB.Header.FormatCode}
	md.Attributes, md.Indices, md.Count = extractSubMesh(pool, pool.IB.Indices[entry.IndexStart:indexEnd])
	return md, nil
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

// extractSubMesh copies the vertex range an index sub range touches and remaps the indices
func extractSubMesh(pool *BufferPool, indices []uint16) ([]buffer.Attribute, []uint16, int) {
	if len(indices) == 0 {
		return nil, nil, 0
	}
	minIdx, maxIdx := indices[0], indices[0]
	for _, idx := range indices[1:] {
		if idx < minIdx {
			minIdx = idx
		}
		if idx > maxIdx {
			maxIdx = idx
		}
	}
	count := int(maxIdx-minIdx) + 1
	attrs := make([]buffer.Attribute, len(pool.Attributes))
	for i, a := range pool.Attributes {
		c := a.Element.Comps
		attrs[i] = buffer.Attribute{Element: a.Element, Data: append([]float32(nil), a.Data[int(minIdx)*c:(int(maxIdx)+1)*c]...)}
	}
	remapped := make([]uint16, len(indices))
	for i, idx := range indices {
		remapped[i] = idx - minIdx
	}
	return attrs, remapped, count
}

// Attribute returns the decoded attribute bound to a fixed function meaning or nil
func (md *MeshData) Attribute(layouts map[uint32]Layout, semantic string) *buffer.Attribute {
	layout, ok := layouts[md.Format]
	if !ok {
		return nil
	}
	for i := range md.Attributes {
		if layout.Semantics[md.Attributes[i].Element.Register] == semantic {
			return &md.Attributes[i]
		}
	}
	return nil
}

// Vertices expands the fixed function position, normal and texcoord attributes
func (md *MeshData) Vertices(layouts map[uint32]Layout) ([]buffer.Vertex, error) {
	pos := md.Attribute(layouts, "position")
	if pos == nil {
		return nil, fmt.Errorf("scene: mesh %q has no position attribute", md.Name)
	}
	norm := md.Attribute(layouts, "normal")
	uv := md.Attribute(layouts, "texcoord0")
	verts := make([]buffer.Vertex, md.Count)
	for i := range verts {
		copy(verts[i].Pos[:], pos.Data[i*pos.Element.Comps:])
		if norm != nil {
			copy(verts[i].Normal[:], norm.Data[i*norm.Element.Comps:])
		}
		if uv != nil {
			copy(verts[i].UV[:], uv.Data[i*uv.Element.Comps:])
		}
	}
	return verts, nil
}

// loadManifest reads _meshes.json from the scene directory.
func loadManifest(dir string) (map[string]meshManifestEntry, error) {
	path := filepath.Join(dir, "_meshes.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("scene: reading mesh manifest: %w", err)
	}
	var manifest map[string]meshManifestEntry
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("scene: parsing mesh manifest: %w", err)
	}
	return manifest, nil
}

// discoverTextures finds all .xbx files in the directory and decodes them to PNG.
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
		name := e.Name()
		ext := strings.ToLower(filepath.Ext(name))
		if ext != ".xbx" {
			continue
		}
		base := strings.TrimSuffix(name, filepath.Ext(name))
		tex, err := texture.OpenXPR(filepath.Join(dir, name))
		if err != nil {
			*warnings = append(*warnings, fmt.Sprintf("texture %s: %v", name, err))
			continue
		}
		var buf bytes.Buffer
		if err := texture.ExportPNG(&buf, tex); err != nil {
			*warnings = append(*warnings, fmt.Sprintf("texture %s: encoding PNG: %v", name, err))
			continue
		}
		cfg, err := png.DecodeConfig(bytes.NewReader(buf.Bytes()))
		if err != nil {
			*warnings = append(*warnings, fmt.Sprintf("texture %s: reading PNG header: %v", name, err))
			continue
		}
		textures = append(textures, &TextureData{Name: base, PNGData: buf.Bytes(), Width: cfg.Width, Height: cfg.Height})
	}
	sort.Slice(textures, func(i, j int) bool { return textures[i].Name < textures[j].Name })
	return textures
}

// discoverPools finds every VB/IB pair and decodes the vertices through the executable's layout
func discoverPools(dir string, layouts map[uint32]Layout) ([]*BufferPool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var pools []*BufferPool
	for _, e := range entries {
		if e.IsDir() || strings.ToLower(filepath.Ext(e.Name())) != ".vb" {
			continue
		}
		base := strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))
		vb, err := buffer.OpenVB(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("scene: %s: %w", e.Name(), err)
		}
		pool := &BufferPool{Name: base, VB: vb}
		if ib, err := buffer.OpenIB(filepath.Join(dir, base+".ib")); err == nil {
			pool.IB = ib
		}
		if vb.Header.VertexCount > 0 {
			layout, ok := layouts[vb.Header.FormatCode]
			if !ok {
				return nil, fmt.Errorf("scene: %s uses vertex format %08X which the executable never declares", e.Name(), vb.Header.FormatCode)
			}
			attrs, err := vb.Decode(layout.Elements, layout.Stride)
			if err != nil {
				return nil, fmt.Errorf("scene: %s: %w", e.Name(), err)
			}
			pool.Attributes = attrs
		}
		pools = append(pools, pool)
	}
	sort.Slice(pools, func(i, j int) bool { return pools[i].Name < pools[j].Name })
	return pools, nil
}
