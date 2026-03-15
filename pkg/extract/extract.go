package extract

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/nickheyer/discoxip/pkg/xip"
)

const bufSize = 256 * 1024

type Options struct {
	OutputDir string
	Verbose   bool
	All       bool // include meshes
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

	for _, e := range r.Entries() {
		if e.Type == xip.FileTypeDir {
			continue
		}
		if e.Type == xip.FileTypeMesh && !opts.All {
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
