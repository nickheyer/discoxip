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
		Short: "Export glyph data as JSON for web rendering",
		Args:  cobra.ExactArgs(1),
		RunE:  runFontExport,
	}
	exportCmd.Flags().StringVarP(&fontExportOutput, "output", "o", "", "output file (default: input with .json extension)")

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
	fmt.Printf("  Cell Height:  %d\n", f.Height)
	fmt.Printf("  Glyph Count:  %d\n", f.GlyphCount)
	fmt.Printf("  Ranges:       %d\n", f.RangeCount)
	fmt.Printf("  File Size:    %d bytes\n", f.FileSize)
	fmt.Println()

	// Count glyphs with mesh data
	meshCount := 0
	for _, g := range f.Glyphs {
		if len(g.Mesh.Indices) > 0 {
			meshCount++
		}
	}
	fmt.Printf("  Glyphs with mesh data: %d\n", meshCount)
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
		outPath = strings.TrimSuffix(args[0], filepath.Ext(args[0])) + ".json"
	}

	out, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer out.Close()

	if err := font.ExportJSON(out, f, 0x024F); err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "Exported font JSON to %s\n", outPath)
	return nil
}
