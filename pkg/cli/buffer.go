package cli

import (
	"encoding/hex"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/nickheyer/discoxip/pkg/buffer"
	"github.com/spf13/cobra"
)

var bufferDumpLimit int
var bufferExportFormat string
var bufferExportOutput string

func init() {
	bufferCmd := &cobra.Command{
		Use:   "buffer",
		Short: "Inspect and export vertex/index buffers",
	}

	// buffer info
	infoCmd := &cobra.Command{
		Use:   "info <file.vb>",
		Short: "Display vertex buffer header and format info",
		Args:  cobra.ExactArgs(1),
		RunE:  runBufferInfo,
	}

	// buffer dump
	dumpCmd := &cobra.Command{
		Use:   "dump <file.vb>",
		Short: "Dump decoded vertex data",
		Args:  cobra.ExactArgs(1),
		RunE:  runBufferDump,
	}
	dumpCmd.Flags().IntVar(&bufferDumpLimit, "limit", 0, "max vertices to print (0 = all)")

	// buffer export
	exportCmd := &cobra.Command{
		Use:   "export <file.vb> <file.ib>",
		Short: "Export vertex+index buffer pair to mesh format",
		Args:  cobra.ExactArgs(2),
		RunE:  runBufferExport,
	}
	exportCmd.Flags().StringVarP(&bufferExportFormat, "format", "f", "obj", "output format (obj)")
	exportCmd.Flags().StringVarP(&bufferExportOutput, "output", "o", "", "output file (default: stdout)")

	bufferCmd.AddCommand(infoCmd, dumpCmd, exportCmd)
	rootCmd.AddCommand(bufferCmd)
}

func runBufferInfo(cmd *cobra.Command, args []string) error {
	vb, err := buffer.OpenVB(args[0])
	if err != nil {
		return err
	}

	f := buffer.VertexFormat(vb.Header.FormatCode)
	fmt.Printf("Vertex Buffer: %s\n", args[0])
	fmt.Printf("  Vertex Count: %d\n", vb.Header.VertexCount)
	fmt.Printf("  Format Code:  0x%08X (%s)\n", vb.Header.FormatCode, f)
	fmt.Printf("  Stride:       %d bytes\n", vb.Stride)
	fmt.Printf("  Data Size:    %d bytes\n", len(vb.RawData))

	if vb.Vertices != nil {
		fmt.Printf("  Decoded:      yes (%d vertices)\n", len(vb.Vertices))
	} else if vb.Header.VertexCount > 0 {
		fmt.Printf("  Decoded:      no (unknown format)\n")
	}

	return nil
}

func runBufferDump(cmd *cobra.Command, args []string) error {
	vb, err := buffer.OpenVB(args[0])
	if err != nil {
		return err
	}

	if vb.Vertices != nil {
		w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(w, "IDX\tPOS_X\tPOS_Y\tPOS_Z\tNRM_X\tNRM_Y\tNRM_Z\tUV_U\tUV_V")
		limit := len(vb.Vertices)
		if bufferDumpLimit > 0 && bufferDumpLimit < limit {
			limit = bufferDumpLimit
		}
		for i := 0; i < limit; i++ {
			v := vb.Vertices[i]
			fmt.Fprintf(w, "%d\t%.4f\t%.4f\t%.4f\t%.4f\t%.4f\t%.4f\t%.4f\t%.4f\n",
				i, v.Pos[0], v.Pos[1], v.Pos[2],
				v.Normal[0], v.Normal[1], v.Normal[2],
				v.UV[0], v.UV[1])
		}
		if bufferDumpLimit > 0 && bufferDumpLimit < len(vb.Vertices) {
			fmt.Fprintf(w, "... (%d more vertices)\n", len(vb.Vertices)-bufferDumpLimit)
		}
		return w.Flush()
	}

	// Unknown format — raw hex dump per stride
	fmt.Printf("Unknown format 0x%08X — raw dump (stride %d):\n\n", vb.Header.FormatCode, vb.Stride)
	limit := int(vb.Header.VertexCount)
	if bufferDumpLimit > 0 && bufferDumpLimit < limit {
		limit = bufferDumpLimit
	}
	for i := 0; i < limit; i++ {
		off := i * vb.Stride
		end := off + vb.Stride
		if end > len(vb.RawData) {
			break
		}
		fmt.Printf("[%5d] %s\n", i, hex.EncodeToString(vb.RawData[off:end]))
	}
	return nil
}

func runBufferExport(cmd *cobra.Command, args []string) error {
	vb, err := buffer.OpenVB(args[0])
	if err != nil {
		return fmt.Errorf("reading VB: %w", err)
	}
	if vb.Vertices == nil {
		return fmt.Errorf("cannot export: unknown vertex format 0x%08X (use buffer dump for raw data)", vb.Header.FormatCode)
	}

	ib, err := buffer.OpenIB(args[1])
	if err != nil {
		return fmt.Errorf("reading IB: %w", err)
	}

	var w *os.File
	if bufferExportOutput != "" {
		w, err = os.Create(bufferExportOutput)
		if err != nil {
			return err
		}
		defer w.Close()
	} else {
		w = os.Stdout
	}

	switch bufferExportFormat {
	case "obj":
		if err := buffer.ExportOBJ(w, vb.Vertices, ib.Indices); err != nil {
			return fmt.Errorf("writing OBJ: %w", err)
		}
	default:
		return fmt.Errorf("unsupported format: %s (supported: obj)", bufferExportFormat)
	}

	if bufferExportOutput != "" {
		fmt.Fprintf(os.Stderr, "Exported %d vertices, %d triangles to %s\n",
			len(vb.Vertices), ib.TriangleCount, bufferExportOutput)
	}
	return nil
}
