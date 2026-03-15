package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nickheyer/discoxip/pkg/texture"
	"github.com/spf13/cobra"
)

var textureExportFormat string
var textureExportOutput string

func init() {
	textureCmd := &cobra.Command{
		Use:   "texture",
		Short: "Inspect and export XBX/XPR0 textures",
	}

	infoCmd := &cobra.Command{
		Use:   "info <file.xbx> [file.xbx...]",
		Short: "Display texture metadata",
		Args:  cobra.MinimumNArgs(1),
		RunE:  runTextureInfo,
	}

	exportCmd := &cobra.Command{
		Use:   "export <file.xbx>",
		Short: "Export texture to PNG",
		Args:  cobra.ExactArgs(1),
		RunE:  runTextureExport,
	}
	exportCmd.Flags().StringVarP(&textureExportFormat, "format", "f", "png", "output format (png)")
	exportCmd.Flags().StringVarP(&textureExportOutput, "output", "o", "", "output file (default: input with .png extension)")

	exportAllCmd := &cobra.Command{
		Use:   "export-all <directory>",
		Short: "Export all XBX textures in a directory to PNG",
		Args:  cobra.ExactArgs(1),
		RunE:  runTextureExportAll,
	}

	textureCmd.AddCommand(infoCmd, exportCmd, exportAllCmd)
	rootCmd.AddCommand(textureCmd)
}

func runTextureInfo(cmd *cobra.Command, args []string) error {
	for _, path := range args {
		tex, err := texture.OpenXPR(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", path, err)
			continue
		}

		info := tex.Info
		fmt.Printf("%s\n", path)
		fmt.Printf("  Dimensions:  %dx%d\n", info.Width, info.Height)
		fmt.Printf("  Format:      %s (%d bpp)\n", info.Format, info.Format.BitsPerPixel())
		fmt.Printf("  Compressed:  %v\n", info.Format.IsCompressed())
		if !info.Format.IsCompressed() {
			fmt.Printf("  Swizzled:    %v\n", info.Format.IsSwizzled())
		}
		fmt.Printf("  Mip Levels:  %d\n", info.MipLevels)
		fmt.Printf("  Header Size: %d bytes\n", info.HeaderSize)
		fmt.Printf("  Data Size:   %d bytes\n", info.DataSize)
		fmt.Printf("  Format Reg:  0x%08X\n", info.FormatReg)

		if len(args) > 1 {
			fmt.Println()
		}
	}
	return nil
}

func runTextureExport(cmd *cobra.Command, args []string) error {
	path := args[0]
	tex, err := texture.OpenXPR(path)
	if err != nil {
		return err
	}

	outPath := textureExportOutput
	if outPath == "" {
		outPath = strings.TrimSuffix(path, filepath.Ext(path)) + ".png"
	}

	f, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer f.Close()

	if err := texture.ExportPNG(f, tex); err != nil {
		return fmt.Errorf("exporting %s: %w", path, err)
	}

	fmt.Fprintf(os.Stderr, "Exported %dx%d %s texture to %s\n",
		tex.Info.Width, tex.Info.Height, tex.Info.Format, outPath)
	return nil
}

func runTextureExportAll(cmd *cobra.Command, args []string) error {
	dir := args[0]
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}

	var exported int
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(e.Name()))
		if ext != ".xbx" {
			continue
		}

		path := filepath.Join(dir, e.Name())
		tex, err := texture.OpenXPR(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "skip %s: %v\n", e.Name(), err)
			continue
		}

		outPath := filepath.Join(dir, strings.TrimSuffix(e.Name(), ext)+".png")
		f, err := os.Create(outPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "skip %s: %v\n", e.Name(), err)
			continue
		}

		if err := texture.ExportPNG(f, tex); err != nil {
			f.Close()
			fmt.Fprintf(os.Stderr, "skip %s: %v\n", e.Name(), err)
			continue
		}
		f.Close()

		fmt.Printf("  %s → %s (%dx%d %s)\n", e.Name(),
			strings.TrimSuffix(e.Name(), ext)+".png",
			tex.Info.Width, tex.Info.Height, tex.Info.Format)
		exported++
	}

	if exported == 0 {
		return fmt.Errorf("no XBX textures found in %s", dir)
	}
	fmt.Fprintf(os.Stderr, "Exported %d textures\n", exported)
	return nil
}
