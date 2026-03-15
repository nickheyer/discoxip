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

func init() {
	sceneCmd := &cobra.Command{
		Use:   "scene",
		Short: "Assemble and export full scenes",
	}

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

	sceneCmd.AddCommand(exportCmd, infoCmd)
	rootCmd.AddCommand(sceneCmd)
}

func runSceneExport(cmd *cobra.Command, args []string) error {
	s, err := scene.Load(args[0])
	if err != nil {
		return err
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

	fmt.Fprintf(os.Stderr, "Exported scene to %s\n", outPath)
	return nil
}

func runSceneInfo(cmd *cobra.Command, args []string) error {
	s, err := scene.Load(args[0])
	if err != nil {
		return err
	}

	fmt.Printf("Scene: %s\n", args[0])
	fmt.Printf("  Directory: %s\n", s.Dir)
	fmt.Printf("  XAP: %s\n", s.XAP)
	fmt.Printf("  Meshes resolved: %d\n", len(s.Meshes))

	for url, md := range s.Meshes {
		status := "no geometry"
		if len(md.Vertices) > 0 {
			status = fmt.Sprintf("%d vertices, %d indices", len(md.Vertices), len(md.Indices))
		}
		fmt.Printf("    %s: %s\n", url, status)
	}

	return nil
}
