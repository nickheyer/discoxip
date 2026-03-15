package extract

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/nickheyer/discoxip/pkg/xip"
)

const bufSize = 256 * 1024

// MeshManifestEntry holds the per-mesh pool and index range info written to _meshes.json.
type MeshManifestEntry struct {
	Pool       int `json:"pool"`
	IndexStart int `json:"index_start"`
	TriCount   int `json:"tri_count"`
}

type Options struct {
	OutputDir string
	Verbose   bool
	All       bool // include meshes (kept for backward compat, no longer writes .xm files)
}

// Extracts all (possible) entries from xip reader
func Archive(r *xip.Reader, opts Options) error {
	if opts.OutputDir == "" {
		opts.OutputDir = "."
	}

	absOut, err := filepath.Abs(opts.OutputDir)
	if err != nil {
		return fmt.Errorf("extract: resolving output dir: %w", err)
	}

	// Collect mesh metadata from mesh-type entries (they have no file data to extract)
	manifest := make(map[string]MeshManifestEntry)

	for _, e := range r.Entries() {
		if e.Type == xip.FileTypeDir {
			continue
		}

		// Mesh entries encode pool/index metadata, not file data.
		// Collect metadata but do not extract — the Offset field is not a byte offset.
		if e.Type == xip.FileTypeMesh {
			meta := xip.DecodeMeshEntry(e)
			manifest[e.Name] = MeshManifestEntry{
				Pool:       meta.Pool,
				IndexStart: meta.IndexStart,
				TriCount:   meta.TriCount,
			}
			if opts.Verbose {
				fmt.Printf("  %s → pool ~%d, index %d, %d tris\n", e.Name, meta.Pool, meta.IndexStart, meta.TriCount)
			}
			continue
		}

		safe, err := sanitizePath(absOut, e.Name)
		if err != nil {
			return fmt.Errorf("extract: %s: %w", e.Name, err)
		}

		if err := extractFile(r, e, safe, opts.Verbose); err != nil {
			return err
		}
	}

	// Write mesh manifest
	if len(manifest) > 0 {
		if err := writeManifest(absOut, manifest, opts.Verbose); err != nil {
			return err
		}
	}

	return nil
}

func writeManifest(dir string, manifest map[string]MeshManifestEntry, verbose bool) error {
	path := filepath.Join(dir, "_meshes.json")
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("extract: encoding mesh manifest: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("extract: writing mesh manifest: %w", err)
	}
	if verbose {
		fmt.Printf("  _meshes.json (%d mesh entries)\n", len(manifest))
	}
	return nil
}

func extractFile(r *xip.Reader, e xip.Entry, dest string, verbose bool) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return fmt.Errorf("extract: mkdir %s: %w", filepath.Dir(dest), err)
	}

	f, err := os.Create(dest)
	if err != nil {
		return fmt.Errorf("extract: create %s: %w", dest, err)
	}
	defer f.Close()

	w := bufio.NewWriterSize(f, bufSize)
	if _, err := io.Copy(w, r.OpenFile(e)); err != nil {
		return fmt.Errorf("extract: writing %s: %w", e.Name, err)
	}
	if err := w.Flush(); err != nil {
		return fmt.Errorf("extract: flushing %s: %w", e.Name, err)
	}

	if verbose {
		fmt.Printf("  %s (%d bytes)\n", e.Name, e.Size)
	}
	return nil
}

// stop name escape from target dir
func sanitizePath(base, name string) (string, error) {
	// Microsoft slashes
	name = filepath.FromSlash(name)

	if filepath.IsAbs(name) {
		return "", fmt.Errorf("absolute path rejected: %s", name)
	}
	if slices.Contains(strings.Split(name, string(filepath.Separator)), "..") {
		return "", fmt.Errorf("path traversal rejected: %s", name)
	}

	joined := filepath.Join(base, name)

	// Resolved path is under base
	rel, err := filepath.Rel(base, joined)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("path escapes output directory: %s", name)
	}

	return joined, nil
}
