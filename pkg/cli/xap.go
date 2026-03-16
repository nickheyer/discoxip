package cli

import (
	"fmt"
	"os"

	"github.com/nickheyer/discoxip/pkg/xap"
	"github.com/spf13/cobra"
)

func init() {
	xapCmd := &cobra.Command{
		Use:   "xap",
		Short: "Inspect and dump XAP scene scripts",
	}

	infoCmd := &cobra.Command{
		Use:   "info <file.xap>",
		Short: "Display scene graph summary",
		Args:  cobra.ExactArgs(1),
		RunE:  runXAPInfo,
	}

	dumpCmd := &cobra.Command{
		Use:   "dump <file.xap>",
		Short: "Pretty-print the scene graph",
		Args:  cobra.ExactArgs(1),
		RunE:  runXAPDump,
	}

	xapCmd.AddCommand(infoCmd, dumpCmd)
	rootCmd.AddCommand(xapCmd)
}

func runXAPInfo(cmd *cobra.Command, args []string) error {
	scene, err := xap.ParseFile(args[0])
	if err != nil {
		return err
	}

	fmt.Printf("XAP Scene: %s\n", args[0])
	fmt.Printf("  Nodes:      %d\n", scene.NodeCount())

	refs := scene.MeshRefs()
	fmt.Printf("  Mesh Refs:  %d\n", len(refs))
	for _, r := range refs {
		fmt.Printf("    - %s\n", r)
	}

	mats := scene.Materials()
	fmt.Printf("  Materials:  %d\n", len(mats))
	for _, m := range mats {
		fmt.Printf("    - %s\n", m)
	}

	if len(scene.Warnings) > 0 {
		fmt.Printf("  Warnings:   %d\n", len(scene.Warnings))
		for _, w := range scene.Warnings {
			fmt.Printf("    - %s\n", w)
		}
	}

	return nil
}

func runXAPDump(cmd *cobra.Command, args []string) error {
	scene, err := xap.ParseFile(args[0])
	if err != nil {
		return err
	}

	xap.PrettyPrint(os.Stdout, scene)
	return nil
}
