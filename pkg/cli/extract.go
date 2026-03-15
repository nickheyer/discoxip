package cli

import (
	"fmt"
	"os"

	"github.com/nickheyer/discoxip/pkg/extract"
	"github.com/nickheyer/discoxip/pkg/xip"
	"github.com/spf13/cobra"
)

var extractOpts extract.Options

func init() {
	extractCmd := &cobra.Command{
		Use:   "extract <file.xip>",
		Short: "Extract files from a XIP archive",
		Args:  cobra.ExactArgs(1),
		RunE:  runExtract,
	}
	extractCmd.Flags().StringVarP(&extractOpts.OutputDir, "output", "o", ".", "output directory")
	extractCmd.Flags().BoolVarP(&extractOpts.Verbose, "verbose", "v", false, "print each file extracted")
	extractCmd.Flags().BoolVar(&extractOpts.All, "all", false, "include mesh (type-4) entries")
	rootCmd.AddCommand(extractCmd)
}

func runExtract(cmd *cobra.Command, args []string) error {
	r, err := xip.Open(args[0])
	if err != nil {
		return err
	}
	defer r.Close()

	h := r.Header()
	fmt.Fprintf(os.Stderr, "Archive: %d files, %d names, data offset 0x%X, data size %d bytes\n",
		h.NumFiles, h.NumNames, h.DataOffset, h.DataSize)

	if err := extract.Archive(r, extractOpts); err != nil {
		return err
	}

	fmt.Fprintln(os.Stderr, "Done.")
	return nil
}
