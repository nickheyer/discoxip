package scene

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/nickheyer/discoxip/pkg/buffer"
	"github.com/nickheyer/discoxip/pkg/xap"
)

// MeshData holds resolved vertex and index data for a mesh reference.
type MeshData struct {
	Name     string
	Vertices []buffer.Vertex
	Indices  []uint16
}

// Scene is a resolved scene graph with mesh data loaded.
type Scene struct {
	XAP    *xap.Scene
	Meshes map[string]*MeshData // keyed by mesh URL
	Dir    string               // base directory for resolving paths
}

// Load parses a XAP scene file and resolves mesh references from the same directory.
// It expects VB and IB files to be in the same directory as the XAP file.
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

	// Try to load VB/IB pools from the directory
	vbPools, ibPools := discoverBuffers(dir)

	// Resolve mesh references
	for _, url := range ast.MeshRefs() {
		if _, ok := s.Meshes[url]; ok {
			continue
		}

		// Try to find matching VB/IB data
		// The mesh references are .xm files, but the actual geometry
		// is in the VB/IB pools (numbered ~0, ~1, ~2, etc.)
		// For now, we link all meshes to the first available VB/IB pool
		md := &MeshData{Name: url}

		if len(vbPools) > 0 && len(ibPools) > 0 {
			// Use the first VB/IB pool as a shared geometry pool
			md.Vertices = vbPools[0].Vertices
			md.Indices = ibPools[0].Indices
		}

		s.Meshes[url] = md
	}

	return s, nil
}

func discoverBuffers(dir string) ([]*buffer.VBReader, []*buffer.IBReader) {
	var vbs []*buffer.VBReader
	var ibs []*buffer.IBReader

	// Look for ~N.vb and ~N.ib files
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil
	}

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := filepath.Ext(e.Name())
		path := filepath.Join(dir, e.Name())

		switch ext {
		case ".vb":
			vb, err := buffer.OpenVB(path)
			if err == nil && vb.Vertices != nil {
				vbs = append(vbs, vb)
			}
		case ".ib":
			ib, err := buffer.OpenIB(path)
			if err == nil {
				ibs = append(ibs, ib)
			}
		}
	}

	return vbs, ibs
}
