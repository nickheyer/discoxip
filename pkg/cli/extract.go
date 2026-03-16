package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nickheyer/discoxip/pkg/extract"
	"github.com/nickheyer/discoxip/pkg/xip"
	"github.com/spf13/cobra"
)

var extractOpts extract.Options
var extractRecursive bool

func init() {
	extractCmd := &cobra.Command{
		Use:   "extract [path]",
		Short: "Extract files from XIP archives",
		Long: `Extract files from a XIP archive or walk a directory for archives.

If path is a .xip file, extract it directly.
If path is a directory, find and extract all .xip files into a shared
output directory. Pool files are prefixed with the archive name to
prevent collisions, and mesh manifests are merged across archives.
Use -r to search subdirectories recursively.
Defaults to the current directory if no path is given.`,
		Args: cobra.MaximumNArgs(1),
		RunE: runExtract,
	}
	extractCmd.Flags().StringVarP(&extractOpts.OutputDir, "output", "o", "", "output directory (default: <source_dir>/out)")
	extractCmd.Flags().BoolVarP(&extractOpts.Verbose, "verbose", "v", false, "print each file extracted")
	extractCmd.Flags().BoolVarP(&extractRecursive, "recursive", "r", false, "walk subdirectories when given a directory")
	rootCmd.AddCommand(extractCmd)
}

func runExtract(cmd *cobra.Command, args []string) error {
	target := "."
	if len(args) > 0 {
		target = args[0]
	}

	info, err := os.Stat(target)
	if err != nil {
		return err
	}

	if !info.IsDir() {
		outDir := extractOpts.OutputDir
		if outDir == "" {
			outDir = filepath.Join(filepath.Dir(target), "out")
		}
		return extractOne(target, outDir)
	}

	return extractDir(target)
}

// extractDir finds .xip files and extracts them to a shared output directory.
// Pool files are prefixed with the archive name to prevent collisions, and
// mesh manifests are merged across all archives.
func extractDir(root string) error {
	// Group archives by their parent directory.
	groups := make(map[string][]string)

	if extractRecursive {
		err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.IsDir() && strings.ToLower(filepath.Ext(d.Name())) == ".xip" {
				dir := filepath.Dir(path)
				groups[dir] = append(groups[dir], path)
			}
			return nil
		})
		if err != nil {
			return fmt.Errorf("walking %s: %w", root, err)
		}
	} else {
		entries, err := os.ReadDir(root)
		if err != nil {
			return fmt.Errorf("reading %s: %w", root, err)
		}
		for _, e := range entries {
			if !e.IsDir() && strings.ToLower(filepath.Ext(e.Name())) == ".xip" {
				groups[root] = append(groups[root], filepath.Join(root, e.Name()))
			}
		}
	}

	total := 0
	for _, archives := range groups {
		total += len(archives)
	}
	if total == 0 {
		return fmt.Errorf("no .xip files found in %s", root)
	}

	fmt.Fprintf(os.Stderr, "Found %d archive(s) in %d location(s)\n", total, len(groups))

	for dir, archives := range groups {
		outDir := extractOpts.OutputDir
		if outDir == "" {
			outDir = filepath.Join(dir, "out")
		} else if len(groups) > 1 {
			rel, err := filepath.Rel(root, dir)
			if err != nil {
				rel = filepath.Base(dir)
			}
			outDir = filepath.Join(outDir, rel)
		}

		fmt.Fprintf(os.Stderr, "\n%s → %s (%d archives)\n", dir, outDir, len(archives))

		for _, path := range archives {
			archiveName := strings.TrimSuffix(filepath.Base(path), filepath.Ext(filepath.Base(path)))
			fmt.Fprintf(os.Stderr, "  %s\n", filepath.Base(path))
			if err := extractOneWithPrefix(path, outDir, archiveName); err != nil {
				fmt.Fprintf(os.Stderr, "    error: %v\n", err)
			}
		}

		// Copy auxiliary directories (Audio/, etc.) that exist alongside XIP files
		copyAuxDirs(dir, outDir)
	}

	return nil
}

// copyAuxDirs copies auxiliary data directories (e.g., Audio/) from the source
// alongside XIP files to the output directory. These directories contain assets
// referenced by XAP scripts (WAV audio files, etc.) but not packed into XIP archives.
func copyAuxDirs(srcDir, outDir string) {
	auxDirs := []string{"Audio"}
	for _, name := range auxDirs {
		src := filepath.Join(srcDir, name)
		info, err := os.Stat(src)
		if err != nil || !info.IsDir() {
			continue
		}

		count := 0
		filepath.Walk(src, func(path string, fi os.FileInfo, err error) error {
			if err != nil || fi.IsDir() {
				return nil
			}
			rel, err := filepath.Rel(srcDir, path)
			if err != nil {
				return nil
			}
			dst := filepath.Join(outDir, rel)
			os.MkdirAll(filepath.Dir(dst), 0o755)
			data, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			if err := os.WriteFile(dst, data, 0o644); err != nil {
				return nil
			}
			count++
			return nil
		})
		if count > 0 {
			fmt.Fprintf(os.Stderr, "  Copied %d files from %s/\n", count, name)
		}
	}
}

// extractOne extracts a single .xip archive to the given output directory.
func extractOne(xipPath, outDir string) error {
	return extractOneWithPrefix(xipPath, outDir, "")
}

// extractOneWithPrefix extracts a .xip archive with an optional archive name
// prefix for pool files (to prevent collisions in shared output directories).
func extractOneWithPrefix(xipPath, outDir, archiveName string) error {
	r, err := xip.Open(xipPath)
	if err != nil {
		return err
	}
	defer r.Close()

	h := r.Header()
	fmt.Fprintf(os.Stderr, "    %d files, data offset 0x%X, data size %d bytes\n",
		h.NumNames, h.DataOffset, h.DataSize)

	opts := extractOpts
	opts.OutputDir = outDir
	opts.ArchiveName = archiveName

	if err := extract.Archive(r, opts); err != nil {
		return err
	}

	return nil
}
