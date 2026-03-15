package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nickheyer/discoxip/pkg/font"
	"github.com/spf13/cobra"
)

var fontExportOutput string
var fontExportWidth int

func init() {
	fontCmd := &cobra.Command{
		Use:   "font",
		Short: "Inspect and export XTF font files",
	}

	infoCmd := &cobra.Command{
		Use:   "info <file.xtf>",
		Short: "Display font metadata",
		Args:  cobra.ExactArgs(1),
		RunE:  runFontInfo,
	}

	exportCmd := &cobra.Command{
		Use:   "export <file.xtf>",
		Short: "Export glyph bitmap data as PNG atlas",
		Args:  cobra.ExactArgs(1),
		RunE:  runFontExport,
	}
	exportCmd.Flags().StringVarP(&fontExportOutput, "output", "o", "", "output file (default: input with .png extension)")
	exportCmd.Flags().IntVar(&fontExportWidth, "width", 256, "atlas width in pixels")

	fontCmd.AddCommand(infoCmd, exportCmd)
	rootCmd.AddCommand(fontCmd)
}

func runFontInfo(cmd *cobra.Command, args []string) error {
	f, err := font.Open(args[0])
	if err != nil {
		return err
	}

	fmt.Printf("XTF Font: %s\n", args[0])
	fmt.Printf("  Name:         %s\n", f.Name)
	fmt.Printf("  Glyph Count:  %d (header), %d (from ranges)\n", f.GlyphCount, f.TotalGlyphs())
	fmt.Printf("  Max Height:   %d\n", f.MaxHeight)
	fmt.Printf("  Ranges:       %d\n", len(f.Ranges))
	fmt.Printf("  Bitmap Offset: 0x%X\n", f.BitmapOffset)
	fmt.Printf("  File Size:    %d bytes\n", f.FileSize)
	fmt.Println()
	fmt.Println("  Unicode Ranges:")
	for _, b := range f.UnicodeBlocks() {
		fmt.Printf("    %s\n", b)
	}

	return nil
}

func runFontExport(cmd *cobra.Command, args []string) error {
	f, err := font.Open(args[0])
	if err != nil {
		return err
	}

	outPath := fontExportOutput
	if outPath == "" {
		outPath = strings.TrimSuffix(args[0], filepath.Ext(args[0])) + ".png"
	}

	out, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer out.Close()

	if err := font.ExportGlyphAtlas(out, f, fontExportWidth); err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "Exported glyph atlas to %s\n", outPath)
	return nil
}
