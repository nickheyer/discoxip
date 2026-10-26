package cli

import (
	"fmt"
	"path/filepath"

	"github.com/nickheyer/discoxip/pkg/scene"
	"github.com/nickheyer/discoxip/pkg/web"
	"github.com/nickheyer/discoxip/pkg/xbe"
	"github.com/spf13/cobra"
)

var webOutputDir string

func init() {
	webCmd := &cobra.Command{
		Use:   "web",
		Short: "Web application tools",
	}

	exportCmd := &cobra.Command{
		Use:   "export <extracted-dir>",
		Short: "Export extracted XIP data as a Three.js web application",
		Args:  cobra.ExactArgs(1),
		RunE:  runWebExport,
	}
	exportCmd.Flags().StringVarP(&webOutputDir, "output", "o", "", "output directory")
	addKernelFlags(exportCmd)

	webCmd.AddCommand(exportCmd)
	rootCmd.AddCommand(webCmd)
}

func runWebExport(cmd *cobra.Command, args []string) error {
	inputDir := args[0]
	outputDir := webOutputDir
	if outputDir == "" {
		outputDir = inputDir + "_web"
	}
	xbeFiles, err := filepath.Glob(filepath.Join(inputDir, "*.xbe"))
	if err != nil {
		return err
	}
	if len(xbeFiles) != 1 {
		return fmt.Errorf("web: expected one .xbe in %s, found %d", inputDir, len(xbeFiles))
	}
	d, err := disassemble(xbeFiles[0])
	if err != nil {
		return err
	}
	kernel, err := loadKernel()
	if err != nil {
		return err
	}
	return web.Export(inputDir, inputDir, outputDir, d, kernel)
}

// Opens and disassembles an executable
func disassemble(path string) (*xbe.Disassembly, error) {
	img, err := xbe.Open(path)
	if err != nil {
		return nil, err
	}
	return xbe.Disassemble(img)
}

// Derives the render model and vertex layouts of an executable at a path with the kernel the flags name
func loadLayouts(path string) (map[uint32]scene.Layout, *xbe.RenderModel, error) {
	d, err := disassemble(path)
	if err != nil {
		return nil, nil, err
	}
	kernel, err := loadKernel()
	if err != nil {
		return nil, nil, err
	}
	return scene.LoadLayouts(d, kernel)
}
