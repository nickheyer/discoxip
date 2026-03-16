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

	"github.com/nickheyer/discoxip/pkg/texture"
	"github.com/nickheyer/discoxip/pkg/xip"
)

const bufSize = 256 * 1024

// MeshManifestEntry holds the per-mesh pool and index range info written to _meshes.json.
type MeshManifestEntry struct {
	Pool       string `json:"pool"`
	IndexStart int    `json:"index_start"`
	TriCount   int    `json:"tri_count"`
	Archive    string `json:"archive,omitempty"` // source archive name (for multi-archive extraction)
}

type Options struct {
	OutputDir   string
	Verbose     bool
	All         bool   // include meshes (kept for backward compat, no longer writes .xm files)
	ArchiveName string // prefix for pool files to avoid collisions in shared output dirs
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
		// Mesh entries encode pool/index metadata, not file data.
		// Collect metadata but do not extract — the Offset field is not a byte offset.
		if e.Type == xip.FileTypeMesh {
			meta := xip.DecodeMeshEntry(e)
			poolName := poolFileName(opts.ArchiveName, meta.Pool)
			// Key includes archive name to avoid collisions when the same
			// mesh name appears in multiple archives (e.g. shared UI meshes).
			manifestKey := e.Name
			if opts.ArchiveName != "" {
				manifestKey = opts.ArchiveName + ":" + e.Name
			}
			manifest[manifestKey] = MeshManifestEntry{
				Pool:       poolName,
				IndexStart: meta.IndexStart,
				TriCount:   meta.TriCount,
				Archive:    opts.ArchiveName,
			}
			if opts.Verbose {
				fmt.Printf("  %s → pool %s, index %d, %d tris\n", e.Name, poolName, meta.IndexStart, meta.TriCount)
			}
			continue
		}

		name := e.Name
		if opts.ArchiveName != "" {
			// Rename pool files to include archive prefix to avoid collisions
			if e.Type == xip.FileTypeVB || e.Type == xip.FileTypeIB {
				name = renamePoolFile(opts.ArchiveName, name)
			}
			// Prefix XAP files so each archive's scene is accessible
			ext := strings.ToLower(filepath.Ext(name))
			if ext == ".xap" {
				name = opts.ArchiveName + "_" + name
			}
		}

		safe, err := sanitizePath(absOut, name)
		if err != nil {
			return fmt.Errorf("extract: %s: %w", e.Name, err)
		}

		if err := extractFile(r, e, safe, opts.Verbose); err != nil {
			return err
		}
	}

	// Merge mesh manifest with any existing one (from other archives in the same dir)
	if len(manifest) > 0 {
		if err := mergeManifest(absOut, manifest, opts.Verbose); err != nil {
			return err
		}
	}

	return nil
}

// poolFileName returns the on-disk pool name for a given archive and pool index.
// When archiveName is set, returns "~<archive>_<idx>" to avoid collisions.
// Otherwise returns "~<idx>" for backward compatibility with single-archive extraction.
func poolFileName(archiveName string, poolIdx int) string {
	if archiveName == "" {
		return fmt.Sprintf("~%d", poolIdx)
	}
	return fmt.Sprintf("~%s_%d", archiveName, poolIdx)
}

// renamePoolFile prefixes a pool filename (e.g. "~0.vb" → "~mainmenu5_0.vb").
func renamePoolFile(archiveName, name string) string {
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	if strings.HasPrefix(base, "~") {
		return "~" + archiveName + "_" + base[1:] + ext
	}
	return archiveName + "_" + name
}

// mergeManifest loads any existing _meshes.json, merges new entries, and writes it back.
func mergeManifest(dir string, manifest map[string]MeshManifestEntry, verbose bool) error {
	path := filepath.Join(dir, "_meshes.json")

	// Load existing manifest if present
	existing := make(map[string]MeshManifestEntry)
	if data, err := os.ReadFile(path); err == nil {
		json.Unmarshal(data, &existing) // ignore parse errors on corrupt files
	}

	// Merge new entries (new entries win on collision)
	for k, v := range manifest {
		existing[k] = v
	}

	data, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return fmt.Errorf("extract: encoding mesh manifest: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("extract: writing mesh manifest: %w", err)
	}
	if verbose {
		fmt.Printf("  _meshes.json (%d mesh entries total)\n", len(existing))
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

	// Auto-convert .xbx textures to .png alongside the raw file
	if strings.ToLower(filepath.Ext(dest)) == ".xbx" {
		convertTextureToPNG(dest, verbose)
	}

	return nil
}

// convertTextureToPNG decodes an XBX texture and writes a PNG next to it.
func convertTextureToPNG(xbxPath string, verbose bool) {
	tex, err := texture.OpenXPR(xbxPath)
	if err != nil {
		if verbose {
			fmt.Fprintf(os.Stderr, "  warning: %s: %v\n", filepath.Base(xbxPath), err)
		}
		return
	}

	pngPath := strings.TrimSuffix(xbxPath, filepath.Ext(xbxPath)) + ".png"
	f, err := os.Create(pngPath)
	if err != nil {
		if verbose {
			fmt.Fprintf(os.Stderr, "  warning: creating %s: %v\n", filepath.Base(pngPath), err)
		}
		return
	}
	defer f.Close()

	if err := texture.ExportPNG(f, tex); err != nil {
		if verbose {
			fmt.Fprintf(os.Stderr, "  warning: encoding %s: %v\n", filepath.Base(pngPath), err)
		}
		return
	}

	if verbose {
		fmt.Printf("  %s → %s (%dx%d %s)\n",
			filepath.Base(xbxPath), filepath.Base(pngPath),
			tex.Info.Width, tex.Info.Height, tex.Info.Format)
	}
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
