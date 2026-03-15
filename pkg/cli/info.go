package cli

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/nickheyer/discoxip/pkg/xip"
	"github.com/spf13/cobra"
)

func init() {
	infoCmd := &cobra.Command{
		Use:   "info <file.xip>",
		Short: "Display archive metadata and file listing",
		Args:  cobra.ExactArgs(1),
		RunE:  runInfo,
	}
	rootCmd.AddCommand(infoCmd)
}

func runInfo(cmd *cobra.Command, args []string) error {
	r, err := xip.Open(args[0])
	if err != nil {
		return err
	}
	defer r.Close()

	h := r.Header()
	fmt.Printf("XIP Archive: %s\n", args[0])
	fmt.Printf("  Files:       %d\n", h.NumFiles)
	fmt.Printf("  Names:       %d\n", h.NumNames)
	fmt.Printf("  Data Offset: 0x%X\n", h.DataOffset)
	fmt.Printf("  Data Size:   %d bytes\n", h.DataSize)
	fmt.Println()

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "TYPE\tSIZE\tOFFSET\tNAME")
	fmt.Fprintln(w, "----\t----\t------\t----")
	for _, e := range r.Entries() {
		fmt.Fprintf(w, "%s\t%d\t0x%X\t%s\n", e.Type, e.Size, e.Offset, e.Name)
	}
	return w.Flush()
}
