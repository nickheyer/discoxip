package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nickheyer/discoxip/pkg/xbe"
	"github.com/spf13/cobra"
)

func init() {
	xbeCmd := &cobra.Command{
		Use:   "xbe",
		Short: "Xbox executable analysis",
	}

	infoCmd := &cobra.Command{
		Use:   "info <file.xbe>",
		Short: "Display XBE header and section info",
		Args:  cobra.ExactArgs(1),
		RunE:  runXBEInfo,
	}

	matsCmd := &cobra.Command{
		Use:   "materials <file.xbe>",
		Short: "Extract CMaxMaterial definitions from XBE",
		Args:  cobra.ExactArgs(1),
		RunE:  runXBEMaterials,
	}

	disasmCmd := &cobra.Command{
		Use:   "disasm <file.xbe>",
		Short: "Full disassembly of XBE executable",
		Args:  cobra.ExactArgs(1),
		RunE:  runXBEDisasm,
	}

	xbeCmd.AddCommand(infoCmd, matsCmd, disasmCmd)
	rootCmd.AddCommand(xbeCmd)
}

func runXBEInfo(cmd *cobra.Command, args []string) error {
	img, err := xbe.Open(args[0])
	if err != nil {
		return err
	}

	fmt.Printf("XBE: %s\n", args[0])
	fmt.Printf("  Base Address:  0x%08X\n", img.BaseAddr)
	fmt.Printf("  Entry Point:   0x%08X\n", img.EntryPoint)
	fmt.Printf("  Kernel Thunk:  0x%08X\n", img.KernThunk)
	fmt.Printf("  Sections:      %d\n", len(img.Sections))

	for _, sec := range img.Sections {
		fmt.Printf("    %-20s VA=0x%08X VSize=0x%06X Raw=0x%06X RSize=0x%06X\n",
			sec.Name, sec.VirtualAddr, sec.VirtualSize, sec.RawAddr, sec.RawSize)
	}

	return nil
}

func runXBEMaterials(cmd *cobra.Command, args []string) error {
	img, err := xbe.Open(args[0])
	if err != nil {
		return err
	}

	materials, err := xbe.ExtractMaterials(img)
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "Extracted %d materials\n", len(materials))

	type matJSON struct {
		Name      string   `json:"name"`
		CtorAddr  string   `json:"ctor"`
		Color1    string   `json:"color1"`
		Color1R   uint8    `json:"color1_r"`
		Color1G   uint8    `json:"color1_g"`
		Color1B   uint8    `json:"color1_b"`
		Color1A   uint8    `json:"color1_a"`
		Color2    string   `json:"color2"`
		ShaderCfg string   `json:"shader_cfg"`
		StackArgs []uint32 `json:"stack_args,omitempty"`
	}

	var out []matJSON
	for _, m := range materials {
		a, r, g, b := xbe.ARGB(m.Color1)
		out = append(out, matJSON{
			Name:      m.Name,
			CtorAddr:  fmt.Sprintf("0x%08X", m.CtorType),
			Color1:    fmt.Sprintf("0x%08X", m.Color1),
			Color1R:   r,
			Color1G:   g,
			Color1B:   b,
			Color1A:   a,
			Color2:    fmt.Sprintf("0x%08X", m.Color2),
			ShaderCfg: fmt.Sprintf("0x%08X", m.ShaderCfg),
			StackArgs: m.StackArgs,
		})
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

func runXBEDisasm(cmd *cobra.Command, args []string) error {
	img, err := xbe.Open(args[0])
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "Disassembling %s...\n", args[0])

	d, err := xbe.Disassemble(img)
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "  Sections disassembled:\n")
	for _, sec := range img.Sections {
		fmt.Fprintf(os.Stderr, "    %-20s VA=0x%08X Size=0x%X\n", sec.Name, sec.VirtualAddr, sec.VirtualSize)
	}
	fmt.Fprintf(os.Stderr, "  %d instructions decoded\n", len(d.InsnByVA))
	fmt.Fprintf(os.Stderr, "  %d functions discovered\n", len(d.Functions))
	fmt.Fprintf(os.Stderr, "  %d kernel imports resolved\n", len(d.Imports))

	// Decompile all functions to pseudocode.
	fmt.Fprintf(os.Stderr, "Decompiling...\n")
	decompiledFuncs := d.DecompileAll()
	fmt.Fprintf(os.Stderr, "  %d functions decompiled\n", len(decompiledFuncs))

	// Write decompiled output next to input: xboxdash.xbe → xboxdash.c
	outPath := strings.TrimSuffix(args[0], filepath.Ext(args[0])) + ".c"
	f, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer f.Close()
	w := bufio.NewWriterSize(f, 256*1024)

	fmt.Fprintf(w, "// Decompiled from %s\n", filepath.Base(args[0]))
	fmt.Fprintf(w, "// %d functions, %d instructions, %d kernel imports\n\n",
		len(decompiledFuncs), len(d.InsnByVA), len(d.Imports))

	for _, df := range decompiledFuncs {
		fmt.Fprintf(w, "\n// --- 0x%08X, %d blocks ---\n", df.EntryVA, len(df.Blocks))
		fmt.Fprint(w, df.Format())
	}

	if err := w.Flush(); err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "  Written to %s\n", outPath)
	return nil
}
