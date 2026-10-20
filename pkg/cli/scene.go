package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nickheyer/discoxip/pkg/scene"
	"github.com/spf13/cobra"
)

var sceneExportOutput string
var sceneExportFormat string
var sceneBuildOutput string
var sceneXBE string

// Loads the vertex layouts the dashboard executable declares
func sceneLayouts() (map[uint32]scene.Layout, error) {
	layouts, _, err := scene.LoadLayouts(sceneXBE)
	return layouts, err
}

func init() {
	sceneCmd := &cobra.Command{
		Use:   "scene",
		Short: "Assemble and export full scenes",
	}

	buildCmd := &cobra.Command{
		Use:   "build <directory>",
		Short: "Extract all XIP archives and export every scene to GLB",
		Long:  "Recursively find all .xip files, extract them, then export every XAP scene that contains geometry.",
		Args:  cobra.ExactArgs(1),
		RunE:  runSceneBuild,
	}
	buildCmd.Flags().StringVarP(&sceneBuildOutput, "output", "o", "", "output directory (default: build/<source>)")

	exportCmd := &cobra.Command{
		Use:   "export <file.xap>",
		Short: "Export scene to glTF/GLB",
		Long:  "Parse XAP scene graph, resolve mesh references and VB/IB pools from the same directory, and export as GLB.",
		Args:  cobra.ExactArgs(1),
		RunE:  runSceneExport,
	}
	exportCmd.Flags().StringVarP(&sceneExportOutput, "output", "o", "", "output file (default: input with .glb extension)")
	exportCmd.Flags().StringVarP(&sceneExportFormat, "format", "f", "glb", "output format (glb)")

	infoCmd := &cobra.Command{
		Use:   "info <file.xap>",
		Short: "Show scene assembly info (resolved meshes, buffers found)",
		Args:  cobra.ExactArgs(1),
		RunE:  runSceneInfo,
	}

	sceneCmd.PersistentFlags().StringVar(&sceneXBE, "xbe", "", "dashboard executable declaring the vertex layouts (required)")
	sceneCmd.MarkPersistentFlagRequired("xbe")
	sceneCmd.AddCommand(buildCmd, exportCmd, infoCmd)
	rootCmd.AddCommand(sceneCmd)
}

func runSceneBuild(cmd *cobra.Command, args []string) error {
	layouts, err := sceneLayouts()
	if err != nil {
		return err
	}
	srcDir := args[0]
	outDir := sceneBuildOutput
	if outDir == "" {
		outDir = filepath.Join("build", srcDir)
	}

	// Step 1: Extract all XIPs recursively
	extractRecursive = true
	extractOpts.OutputDir = outDir
	if err := extractDir(srcDir); err != nil {
		return err
	}

	// Step 2: Find all XAP files in output and export those with geometry
	exported := 0
	err = filepath.WalkDir(outDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if strings.ToLower(filepath.Ext(d.Name())) != ".xap" {
			return nil
		}

		s, err := scene.Load(path, layouts)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  %s: %v\n", path, err)
			return nil
		}

		if len(s.Meshes) == 0 {
			return nil
		}

		hasGeometry := false
		for _, md := range s.Meshes {
			if md.Count > 0 {
				hasGeometry = true
				break
			}
		}
		if !hasGeometry {
			return nil
		}

		glbPath := strings.TrimSuffix(path, filepath.Ext(path)) + ".glb"
		f, err := os.Create(glbPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  %s: %v\n", glbPath, err)
			return nil
		}
		defer f.Close()

		if err := scene.ExportGLB(f, s); err != nil {
			fmt.Fprintf(os.Stderr, "  %s: %v\n", glbPath, err)
			return nil
		}

		fmt.Fprintf(os.Stderr, "  Exported %d meshes → %s\n", len(s.Meshes), glbPath)
		exported++
		return nil
	})
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "\n%d scene(s) exported to %s\n", exported, outDir)
	return nil
}

func runSceneExport(cmd *cobra.Command, args []string) error {
	layouts, err := sceneLayouts()
	if err != nil {
		return err
	}
	s, err := scene.Load(args[0], layouts)
	if err != nil {
		return err
	}

	for _, w := range s.Warnings {
		fmt.Fprintf(os.Stderr, "  warning: %s\n", w)
	}

	outPath := sceneExportOutput
	if outPath == "" {
		outPath = strings.TrimSuffix(args[0], filepath.Ext(args[0])) + ".glb"
	}

	f, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer f.Close()

	if err := scene.ExportGLB(f, s); err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "Exported scene (%d meshes) to %s\n", len(s.Meshes), outPath)
	return nil
}

func runSceneInfo(cmd *cobra.Command, args []string) error {
	layouts, err := sceneLayouts()
	if err != nil {
		return err
	}
	s, err := scene.Load(args[0], layouts)
	if err != nil {
		return err
	}

	fmt.Printf("Scene: %s\n", args[0])
	fmt.Printf("  Directory: %s\n", s.Dir)
	fmt.Printf("  XAP: %s\n", s.XAP.GoString())

	fmt.Printf("  Buffer pools: %d\n", len(s.Pools))
	for _, pool := range s.Pools {
		verts := int(pool.VB.Header.VertexCount)
		idxCount := 0
		if pool.IB != nil {
			idxCount = len(pool.IB.Indices)
		}
		fmt.Printf("    %s: %d verts (format 0x%08X, stride %d), %d indices, %d attributes\n",
			pool.Name, verts, pool.VB.Header.FormatCode, pool.VB.Stride, idxCount, len(pool.Attributes))
	}

	fmt.Printf("  Textures: %d\n", len(s.Textures))
	for _, tex := range s.Textures {
		fmt.Printf("    %s: %dx%d (%d bytes PNG)\n", tex.Name, tex.Width, tex.Height, len(tex.PNGData))
	}

	fmt.Printf("  Mesh refs: %d\n", len(s.Meshes))
	for url, md := range s.Meshes {
		status := "no geometry"
		if md.Count > 0 {
			status = fmt.Sprintf("%d vertices, %d indices", md.Count, len(md.Indices))
		}
		fmt.Printf("    %s: %s\n", url, status)
	}

	if len(s.Warnings) > 0 {
		fmt.Printf("  Warnings:\n")
		for _, w := range s.Warnings {
			fmt.Printf("    - %s\n", w)
		}
	}

	return nil
}
