package cli

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/nickheyer/discoxip/pkg/buffer"
	"github.com/nickheyer/discoxip/pkg/scene"
	"github.com/spf13/cobra"
)

var (
	bufferDumpLimit    int
	bufferExportOutput string
	bufferXBE          string
)

func init() {
	bufferCmd := &cobra.Command{
		Use:   "buffer",
		Short: "Inspect and export vertex/index buffers",
	}

	infoCmd := &cobra.Command{
		Use:   "info <file.vb>",
		Short: "Display vertex buffer header and stride",
		Args:  cobra.ExactArgs(1),
		RunE:  runBufferInfo,
	}

	dumpCmd := &cobra.Command{
		Use:   "dump <file.vb>",
		Short: "Dump vertex data decoded through the executable's declaration",
		Args:  cobra.ExactArgs(1),
		RunE:  runBufferDump,
	}
	dumpCmd.Flags().IntVar(&bufferDumpLimit, "limit", 0, "max vertices to print (0 = all)")
	dumpCmd.Flags().StringVar(&bufferXBE, "xbe", "", "dashboard executable declaring the vertex layouts (required)")
	dumpCmd.MarkFlagRequired("xbe")

	exportCmd := &cobra.Command{
		Use:   "export <file.vb> <file.ib>",
		Short: "Export a vertex and index buffer pair to Wavefront OBJ",
		Args:  cobra.ExactArgs(2),
		RunE:  runBufferExport,
	}
	exportCmd.Flags().StringVarP(&bufferExportOutput, "output", "o", "", "output file (default: stdout)")
	exportCmd.Flags().StringVar(&bufferXBE, "xbe", "", "dashboard executable declaring the vertex layouts (required)")
	exportCmd.MarkFlagRequired("xbe")

	bufferCmd.AddCommand(infoCmd, dumpCmd, exportCmd)
	rootCmd.AddCommand(bufferCmd)
}

func runBufferInfo(cmd *cobra.Command, args []string) error {
	vb, err := buffer.OpenVB(args[0])
	if err != nil {
		return err
	}
	fmt.Printf("Vertex Buffer: %s\n", args[0])
	fmt.Printf("  Vertex Count: %d\n", vb.Header.VertexCount)
	fmt.Printf("  Format Code:  0x%08X\n", vb.Header.FormatCode)
	fmt.Printf("  Stride:       %d bytes\n", vb.Stride)
	return nil
}

// Decodes a vertex buffer through the layout the executable binds to its format
func decodeBuffer(path string) (*buffer.VBReader, scene.Layout, []buffer.Attribute, error) {
	vb, err := buffer.OpenVB(path)
	if err != nil {
		return nil, scene.Layout{}, nil, err
	}
	layouts, _, err := loadLayouts(bufferXBE)
	if err != nil {
		return nil, scene.Layout{}, nil, err
	}
	layout, ok := layouts[vb.Header.FormatCode]
	if !ok {
		return nil, scene.Layout{}, nil, fmt.Errorf("format 0x%08X is not declared by %s", vb.Header.FormatCode, bufferXBE)
	}
	attrs, err := vb.Decode(layout.Elements, layout.Stride)
	if err != nil {
		return nil, scene.Layout{}, nil, err
	}
	return vb, layout, attrs, nil
}

func runBufferDump(cmd *cobra.Command, args []string) error {
	vb, layout, attrs, err := decodeBuffer(args[0])
	if err != nil {
		return err
	}
	w := bufio.NewWriter(os.Stdout)
	defer w.Flush()
	var heads []string
	for _, a := range attrs {
		heads = append(heads, fmt.Sprintf("%s(v%d %s)", layout.Semantics[a.Element.Register], a.Element.Register, a.Element.Type))
	}
	fmt.Fprintf(w, "format 0x%08X stride %d: %s\n", vb.Header.FormatCode, vb.Stride, strings.Join(heads, " "))
	count := int(vb.Header.VertexCount)
	limit := count
	if bufferDumpLimit > 0 && bufferDumpLimit < count {
		limit = bufferDumpLimit
	}
	for i := 0; i < limit; i++ {
		fmt.Fprintf(w, "%6d:", i)
		for _, a := range attrs {
			c := a.Element.Comps
			fmt.Fprintf(w, "  %v", a.Data[i*c:(i+1)*c])
		}
		fmt.Fprintln(w)
	}
	if limit < count {
		fmt.Fprintf(w, "... (%d more vertices)\n", count-limit)
	}
	return nil
}

func runBufferExport(cmd *cobra.Command, args []string) error {
	_, layout, attrs, err := decodeBuffer(args[0])
	if err != nil {
		return err
	}
	ib, err := buffer.OpenIB(args[1])
	if err != nil {
		return err
	}
	md := &scene.MeshData{Attributes: attrs, Indices: ib.Indices}
	for _, a := range attrs {
		md.Count = len(a.Data) / a.Element.Comps
	}
	verts, err := md.Vertices(map[uint32]scene.Layout{md.Format: layout})
	if err != nil {
		return err
	}
	out := os.Stdout
	if bufferExportOutput != "" {
		f, err := os.Create(bufferExportOutput)
		if err != nil {
			return err
		}
		defer f.Close()
		out = f
	}
	w := bufio.NewWriter(out)
	defer w.Flush()
	for _, v := range verts {
		fmt.Fprintf(w, "v %g %g %g\n", v.Pos[0], v.Pos[1], v.Pos[2])
	}
	for _, v := range verts {
		fmt.Fprintf(w, "vn %g %g %g\n", v.Normal[0], v.Normal[1], v.Normal[2])
	}
	for _, v := range verts {
		fmt.Fprintf(w, "vt %g %g\n", v.UV[0], v.UV[1])
	}
	for i := 0; i+2 < len(ib.Indices); i += 3 {
		a, b, c := ib.Indices[i]+1, ib.Indices[i+1]+1, ib.Indices[i+2]+1
		fmt.Fprintf(w, "f %d/%d/%d %d/%d/%d %d/%d/%d\n", a, a, a, b, b, b, c, c, c)
	}
	return nil
}
