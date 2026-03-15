package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/nickheyer/discoxip/pkg/mesh"
	"github.com/spf13/cobra"
)

func init() {
	meshCmd := &cobra.Command{
		Use:   "mesh",
		Short: "Inspect and export XM mesh files",
	}

	infoCmd := &cobra.Command{
		Use:   "info <file.xm> [file.xm...]",
		Short: "Display mesh file content type and summary",
		Args:  cobra.MinimumNArgs(1),
		RunE:  runMeshInfo,
	}

	exportAllCmd := &cobra.Command{
		Use:   "export-all <directory>",
		Short: "Batch info for all XM files in a directory",
		Args:  cobra.ExactArgs(1),
		RunE:  runMeshExportAll,
	}

	meshCmd.AddCommand(infoCmd, exportAllCmd)
	rootCmd.AddCommand(meshCmd)
}

func runMeshInfo(cmd *cobra.Command, args []string) error {
	for _, path := range args {
		m, err := mesh.Open(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", path, err)
			continue
		}

		fmt.Printf("%s\n", path)
		fmt.Printf("  Type: %s\n", m.Type)
		fmt.Printf("  Size: %d bytes\n", m.Size)

		switch m.Type {
		case mesh.ContentText:
			if m.Text != nil {
				fmt.Printf("  Nodes: %d\n", len(m.Text.NodeNames))
				if len(m.Text.MeshRefs) > 0 {
					fmt.Printf("  Mesh refs: %v\n", m.Text.MeshRefs)
				}
			}
		case mesh.ContentBinary:
			if m.Binary != nil {
				fmt.Printf("  Records: %d (4-byte each)\n", len(m.Binary.Records))
				// Count distinct tags
				tags := make(map[byte]int)
				for _, r := range m.Binary.Records {
					tags[r.Tag]++
				}
				fmt.Printf("  Tags:")
				for tag, count := range tags {
					fmt.Printf(" 0x%02X×%d", tag, count)
				}
				fmt.Println()
			}
		}

		if len(args) > 1 {
			fmt.Println()
		}
	}
	return nil
}

func runMeshExportAll(cmd *cobra.Command, args []string) error {
	dir := args[0]
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}

	var paths []string
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".xm" {
			paths = append(paths, filepath.Join(dir, e.Name()))
		}
	}

	if len(paths) == 0 {
		return fmt.Errorf("no .xm files found in %s", dir)
	}

	return runMeshInfo(cmd, paths)
}
