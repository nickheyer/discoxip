package cli

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/nickheyer/discoxip/pkg/xip"
	"github.com/spf13/cobra"
)

var packOutput string

func init() {
	packCmd := &cobra.Command{
		Use:   "pack <directory>",
		Short: "Pack a directory into a XIP archive",
		Args:  cobra.ExactArgs(1),
		RunE:  runPack,
	}
	packCmd.Flags().StringVarP(&packOutput, "output", "o", "", "output file (required)")
	packCmd.MarkFlagRequired("output")
	rootCmd.AddCommand(packCmd)
}

func runPack(cmd *cobra.Command, args []string) error {
	srcDir := args[0]

	info, err := os.Stat(srcDir)
	if err != nil {
		return fmt.Errorf("pack: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("pack: %s is not a directory", srcDir)
	}

	// Collect files
	type fileEntry struct {
		path string // absolute path on disk
		rel  string // relative path for archive
		size int64
	}
	var files []fileEntry

	err = filepath.WalkDir(srcDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		fi, err := d.Info()
		if err != nil {
			return err
		}
		files = append(files, fileEntry{path: path, rel: rel, size: fi.Size()})
		return nil
	})
	if err != nil {
		return fmt.Errorf("pack: walking directory: %w", err)
	}

	if len(files) == 0 {
		return fmt.Errorf("pack: no files found in %s", srcDir)
	}

	// Create output file
	out, err := os.Create(packOutput)
	if err != nil {
		return fmt.Errorf("pack: %w", err)
	}
	defer out.Close()

	w := xip.NewWriter(out)

	for _, f := range files {
		fh, err := os.Open(f.path)
		if err != nil {
			return fmt.Errorf("pack: opening %s: %w", f.rel, err)
		}
		defer fh.Close()

		ft := xip.FileTypeRegular
		if strings.HasSuffix(strings.ToLower(f.rel), ".xm") {
			ft = xip.FileTypeMesh
		}

		w.Add(xip.WriteEntry{
			Name: f.rel,
			Type: ft,
			Size: uint32(f.size),
			Body: fh,
		})
	}

	if err := w.Flush(); err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "Packed %d files into %s\n", len(files), packOutput)
	return nil
}
