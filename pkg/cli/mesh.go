package cli

import (
	"fmt"
	"os"

	"github.com/nickheyer/discoxip/pkg/mesh"
	"github.com/spf13/cobra"
)

func init() {
	meshCmd := &cobra.Command{
		Use:   "mesh",
		Short: "Inspect XM mesh files",
	}

	infoCmd := &cobra.Command{
		Use:   "info <file.xm|directory> [...]",
		Short: "Display mesh file content type and summary",
		Args:  cobra.MinimumNArgs(1),
		RunE:  runMeshInfo,
	}

	meshCmd.AddCommand(infoCmd)
	rootCmd.AddCommand(meshCmd)
}

// Expands directory arguments into their .xm files
func expandMeshPaths(args []string) ([]string, error) {
	var paths []string
	for _, arg := range args {
		info, err := os.Stat(arg)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			paths = append(paths, arg)
			continue
		}
		found, err := findFiles(arg, ".xm")
		if err != nil {
			return nil, err
		}
		if len(found) == 0 {
			return nil, fmt.Errorf("no .xm files found in %s", arg)
		}
		paths = append(paths, found...)
	}
	return paths, nil
}

func runMeshInfo(cmd *cobra.Command, args []string) error {
	paths, err := expandMeshPaths(args)
	if err != nil {
		return err
	}

	for _, path := range paths {
		m, err := mesh.Open(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", path, err)
			continue
		}

		fmt.Printf("%s\n", path)
		fmt.Printf("  Type: %s\n", m.Type)
		fmt.Printf("  Size: %d bytes\n", m.Size)

		if m.Type == mesh.ContentText && m.Text != nil {
			fmt.Printf("  Nodes: %d\n", len(m.Text.NodeNames))
			if len(m.Text.MeshRefs) > 0 {
				fmt.Printf("  Mesh refs: %v\n", m.Text.MeshRefs)
			}
		}

		if len(paths) > 1 {
			fmt.Println()
		}
	}
	return nil
}
