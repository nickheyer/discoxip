package cli

import (
	"github.com/nickheyer/discoxip/pkg/web"
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

	webCmd.AddCommand(exportCmd)
	rootCmd.AddCommand(webCmd)
}

func runWebExport(cmd *cobra.Command, args []string) error {
	inputDir := args[0]
	outputDir := webOutputDir
	if outputDir == "" {
		outputDir = inputDir + "_web"
	}
	return web.Export(inputDir, outputDir)
}
