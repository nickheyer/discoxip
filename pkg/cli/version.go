package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Set w/ ldflags at build
var (
	Version = "dev"
	Commit  = "none"
)

func init() {
	versionCmd := &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("discoxip %s (commit %s)\n", Version, Commit)
		},
	}
	rootCmd.AddCommand(versionCmd)
}
