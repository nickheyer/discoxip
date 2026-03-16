package cli

import (
	"fmt"
	"os"
	"path/filepath"
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
		Long: `Takes a directory containing Xbox dashboard files (.xip archives,
.xbe executable, Audio/ directory) and produces a self-contained
Three.js web application.

This is the single entry point for the full pipeline:
  1. Extract all .xip archives (meshes, textures, scenes)
  2. Copy auxiliary data (audio files)
  3. Decompile .xbe to extract material definitions
  4. Generate Three.js web app with all assets

Example:
  discoxip build sample/4304 -o output/dashboard`,
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

	// Intermediate extracted data goes into a temp dir inside output
	extractDir := filepath.Join(outDir, ".extracted")
	webDir := filepath.Join(outDir, "web")

	fmt.Fprintf(os.Stderr, "Building from %s → %s\n\n", srcDir, outDir)

	// ── Step 1: Extract all XIP archives ──
	fmt.Fprintf(os.Stderr, "Step 1: Extracting XIP archives...\n")

	xipFiles, err := findFiles(srcDir, ".xip")
	if err != nil {
		return err
	}
	if len(xipFiles) == 0 {
		return fmt.Errorf("no .xip files found in %s", srcDir)
	}
	fmt.Fprintf(os.Stderr, "  Found %d archive(s)\n", len(xipFiles))

	if err := os.MkdirAll(extractDir, 0o755); err != nil {
		return err
	}

	for _, xipPath := range xipFiles {
		archiveName := strings.TrimSuffix(filepath.Base(xipPath), filepath.Ext(xipPath))
		fmt.Fprintf(os.Stderr, "  %s\n", filepath.Base(xipPath))

		r, err := xip.Open(xipPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "    error: %v\n", err)
			continue
		}

		opts := extract.Options{
			OutputDir:   extractDir,
			ArchiveName: archiveName,
		}
		if err := extract.Archive(r, opts); err != nil {
			fmt.Fprintf(os.Stderr, "    error: %v\n", err)
		}
		r.Close()
	}

	// ── Step 2: Copy auxiliary files (Audio/, fonts, etc.) ──
	fmt.Fprintf(os.Stderr, "\nStep 2: Copying auxiliary files...\n")
	copyAuxDirs(srcDir, extractDir)

	// Copy XBE into extracted dir so web export can find it
	xbeFiles, _ := findFiles(srcDir, ".xbe")
	for _, xbePath := range xbeFiles {
		dst := filepath.Join(extractDir, filepath.Base(xbePath))
		data, err := os.ReadFile(xbePath)
		if err != nil {
			continue
		}
		os.WriteFile(dst, data, 0o644)
		fmt.Fprintf(os.Stderr, "  Copied %s\n", filepath.Base(xbePath))
	}

	// Copy font files
	xtfFiles, _ := findFiles(srcDir, ".xtf")
	for _, xtfPath := range xtfFiles {
		dst := filepath.Join(extractDir, filepath.Base(xtfPath))
		data, err := os.ReadFile(xtfPath)
		if err != nil {
			continue
		}
		os.WriteFile(dst, data, 0o644)
		fmt.Fprintf(os.Stderr, "  Copied %s\n", filepath.Base(xtfPath))
	}

	// ── Step 3: Build web app ──
	fmt.Fprintf(os.Stderr, "\nStep 3: Building web application...\n")

	if err := web.Export(extractDir, webDir); err != nil {
		return fmt.Errorf("web export: %w", err)
	}

	// ── Step 4: Decompile XBE ──
	if len(xbeFiles) > 0 {
		fmt.Fprintf(os.Stderr, "\nStep 4: Decompiling XBE...\n")

		xbeSrc := xbeFiles[0]
		xbeName := strings.TrimSuffix(filepath.Base(xbeSrc), filepath.Ext(filepath.Base(xbeSrc)))
		decompPath := filepath.Join(outDir, xbeName+".c")

		img, err := xbe.Open(xbeSrc)
		if err == nil {
			d, err := xbe.Disassemble(img)
			if err == nil {
				funcs := d.DecompileAll()
				f, err := os.Create(decompPath)
				if err == nil {
					fmt.Fprintf(f, "// Decompiled from %s\n", filepath.Base(xbeSrc))
					fmt.Fprintf(f, "// %d functions, %d instructions\n\n", len(funcs), len(d.InsnByVA))
					for _, df := range funcs {
						fmt.Fprintf(f, "\n// --- 0x%08X, %d blocks ---\n", df.EntryVA, len(df.Blocks))
						fmt.Fprint(f, df.Format())
					}
					f.Close()
					fmt.Fprintf(os.Stderr, "  %d functions → %s\n", len(funcs), decompPath)
				}
			} else {
				fmt.Fprintf(os.Stderr, "  warning: disassembly failed: %v\n", err)
			}
		} else {
			fmt.Fprintf(os.Stderr, "  warning: XBE load failed: %v\n", err)
		}
	}

	fmt.Fprintf(os.Stderr, "\nBuild complete.\n")
	fmt.Fprintf(os.Stderr, "  Web app: %s\n", webDir)
	fmt.Fprintf(os.Stderr, "  Run: cd %s && python3 -m http.server\n", webDir)

	return nil
}

// findFiles finds all files with a given extension in a directory (non-recursive).
func findFiles(dir, ext string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.EqualFold(filepath.Ext(e.Name()), ext) {
			files = append(files, filepath.Join(dir, e.Name()))
		}
	}
	return files, nil
}
