package cli

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/nickheyer/discoxip/pkg/extract"
	"github.com/nickheyer/discoxip/pkg/web"
	"github.com/nickheyer/discoxip/pkg/xbe"
	"github.com/nickheyer/discoxip/pkg/xip"
	"github.com/spf13/cobra"
)

var buildOutputDir string

func init() {
	buildCmd := &cobra.Command{
		Use:   "build <source-dir>",
		Short: "Build complete web app from Xbox dashboard data",
		Long: `Takes a directory tree containing Xbox dashboard files (.xip archives,
.xbe executable, fonts, Audio/ directory) and produces a self-contained
Three.js web application.

This is the single entry point for the full pipeline:
  1. Find the dashboard executable and the archives it names
  2. Extract those archives (meshes, textures, scenes)
  3. Copy auxiliary data (audio, fonts)
  4. Generate the Three.js web app with everything derived from the executable

Example:
  discoxip build sample/5659 -o output/dashboard`,
		Args: cobra.ExactArgs(1),
		RunE: runBuild,
	}
	buildCmd.Flags().StringVarP(&buildOutputDir, "output", "o", "", "output directory (default: <source>/out)")
	rootCmd.AddCommand(buildCmd)
}

func runBuild(cmd *cobra.Command, args []string) error {
	srcDir := args[0]

	info, err := os.Stat(srcDir)
	if err != nil {
		return fmt.Errorf("source directory: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", srcDir)
	}

	outDir := buildOutputDir
	if outDir == "" {
		outDir = filepath.Join(srcDir, "out")
	}

	extractDir := filepath.Join(outDir, ".extracted")
	webDir := filepath.Join(outDir, "web")

	fmt.Fprintf(os.Stderr, "Building from %s → %s\n\n", srcDir, outDir)

	fmt.Fprintf(os.Stderr, "Step 1: Finding the dashboard executable...\n")
	xbePaths, err := findFiles(srcDir, ".xbe")
	if err != nil {
		return err
	}
	var dashboards []string
	var manifest []string
	var img *xbe.Image
	for _, path := range xbePaths {
		candidate, err := xbe.Open(path)
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		names := candidate.NamedFiles(".xip")
		if len(names) == 0 {
			fmt.Fprintf(os.Stderr, "  %s names no scene archives\n", relPath(srcDir, path))
			continue
		}
		dashboards = append(dashboards, path)
		manifest = names
		img = candidate
	}
	if len(dashboards) != 1 {
		return fmt.Errorf("%d executables under %s name scene archives, expected one: %v", len(dashboards), srcDir, dashboards)
	}
	xbePath := dashboards[0]
	fmt.Fprintf(os.Stderr, "  %s names %d archives\n", relPath(srcDir, xbePath), len(manifest))
	d, err := xbe.Disassemble(img)
	if err != nil {
		return fmt.Errorf("%s: %w", xbePath, err)
	}

	fmt.Fprintf(os.Stderr, "\nStep 2: Extracting the archives the executable names...\n")
	xipPaths, err := findFiles(srcDir, ".xip")
	if err != nil {
		return err
	}
	byName := map[string][]string{}
	for _, path := range xipPaths {
		key := strings.ToLower(filepath.Base(path))
		byName[key] = append(byName[key], path)
	}
	if err := os.MkdirAll(extractDir, 0o755); err != nil {
		return err
	}
	for _, name := range manifest {
		key := strings.ToLower(name)
		paths := byName[key]
		if len(paths) == 0 {
			return fmt.Errorf("archive %s named by %s is not under %s", name, filepath.Base(xbePath), srcDir)
		}
		if len(paths) > 1 {
			return fmt.Errorf("archive %s named by %s is present %d times under %s: %v", name, filepath.Base(xbePath), len(paths), srcDir, paths)
		}
		delete(byName, key)
		xipPath := paths[0]
		archiveName := strings.TrimSuffix(filepath.Base(xipPath), filepath.Ext(xipPath))
		fmt.Fprintf(os.Stderr, "  %s\n", relPath(srcDir, xipPath))
		r, err := xip.Open(xipPath)
		if err != nil {
			return fmt.Errorf("%s: %w", xipPath, err)
		}
		opts := extract.Options{OutputDir: extractDir, ArchiveName: archiveName}
		if err := extract.Archive(r, opts); err != nil {
			r.Close()
			return fmt.Errorf("%s: %w", xipPath, err)
		}
		r.Close()
	}
	for _, key := range sortedKeys(byName) {
		for _, path := range byName[key] {
			fmt.Fprintf(os.Stderr, "  %s is not a scene archive %s names\n", relPath(srcDir, path), filepath.Base(xbePath))
		}
	}

	fmt.Fprintf(os.Stderr, "\nStep 3: Copying auxiliary files...\n")
	copyAuxDirs(srcDir, extractDir)
	if err := copyFile(xbePath, filepath.Join(extractDir, filepath.Base(xbePath))); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "  Copied %s\n", filepath.Base(xbePath))
	if err := copyFonts(srcDir, extractDir, d); err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "\nStep 4: Building web application...\n")
	if err := web.Export(extractDir, srcDir, webDir, d); err != nil {
		return fmt.Errorf("web export: %w", err)
	}

	fmt.Fprintf(os.Stderr, "\nBuild complete.\n")
	fmt.Fprintf(os.Stderr, "  Web app: %s\n", webDir)
	fmt.Fprintf(os.Stderr, "  Run: cd %s && python3 -m http.server\n", webDir)
	return nil
}

// Copies each font file the tree holds, choosing among same named copies by the directory the code tries first
func copyFonts(srcDir, extractDir string, d *xbe.Disassembly) error {
	xtfPaths, err := findFiles(srcDir, ".xtf")
	if err != nil {
		return err
	}
	byName := map[string][]string{}
	for _, path := range xtfPaths {
		key := strings.ToLower(filepath.Base(path))
		byName[key] = append(byName[key], path)
	}
	preference := d.DirectoryPreference(".xtf")
	for _, key := range sortedKeys(byName) {
		paths := byName[key]
		chosen := ""
		if len(paths) == 1 {
			chosen = paths[0]
		}
		for _, dir := range preference {
			if chosen != "" {
				break
			}
			for _, path := range paths {
				if strings.ToLower(filepath.ToSlash(filepath.Dir(relPath(srcDir, path)))) == dir || (dir == "" && filepath.Dir(relPath(srcDir, path)) == ".") {
					chosen = path
					break
				}
			}
		}
		if chosen == "" {
			return fmt.Errorf("font %s is present %d times under %s and the executable tries none of their directories first: %v", key, len(paths), srcDir, paths)
		}
		if err := copyFile(chosen, filepath.Join(extractDir, filepath.Base(chosen))); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "  Copied %s\n", relPath(srcDir, chosen))
	}
	return nil
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o644)
}

func relPath(base, path string) string {
	rel, err := filepath.Rel(base, path)
	if err != nil {
		return path
	}
	return rel
}

func sortedKeys(m map[string][]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Finds every file with an extension anywhere under a directory, in path order
func findFiles(dir, ext string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(dir, func(path string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !e.IsDir() && strings.EqualFold(filepath.Ext(e.Name()), ext) {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}
