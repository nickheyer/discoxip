package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/nickheyer/discoxip/pkg/xbe"
	"github.com/spf13/cobra"
)

var xbeDisasmOutput string

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
	disasmCmd.Flags().StringVarP(&xbeDisasmOutput, "output", "o", "", "output file (default: input with .c extension)")
	matsCmd.Flags().BoolVarP(&xbeMaterialsVerbose, "verbose", "v", false, "print every apply path")

	symbolsCmd := &cobra.Command{
		Use:   "symbols <file.xbe>",
		Short: "List XDK library functions identified by signature",
		Args:  cobra.ExactArgs(1),
		RunE:  runXBESymbols,
	}

	renderCmd := &cobra.Command{
		Use:   "render <file.xbe>",
		Short: "Derive the render model and write it as JSON",
		Args:  cobra.ExactArgs(1),
		RunE:  runXBERender,
	}
	renderCmd.Flags().StringVarP(&xbeRenderOutput, "output", "o", "", "output file (default: stdout)")
	addKernelFlags(renderCmd)

	liftCmd := &cobra.Command{
		Use:   "lift <file.xbe> <va>...",
		Short: "Lift routines block by block and print the blocks",
		Args:  cobra.MinimumNArgs(2),
		RunE:  runXBELift,
	}

	xbeCmd.AddCommand(infoCmd, matsCmd, disasmCmd, symbolsCmd, renderCmd, liftCmd)
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
	d, err := xbe.Disassemble(img)
	if err != nil {
		return err
	}
	ms, err := xbe.DeriveMaterials(d)
	if err != nil {
		return err
	}
	fmt.Printf("factory %08X allocator %08X constructors %d materials %d\n", ms.FactoryVA, ms.AllocatorVA, len(ms.Ctors), len(ms.Materials))
	for _, m := range ms.Materials {
		fmt.Printf("\n== %s (%s) ctor %08X vtable %08X apply %08X paths %d\n", m.Name, m.Object, m.CtorVA, m.VtableVA, m.ApplyVA, len(m.Apply))
		offs := make([]int, 0, len(m.Fields))
		for off := range m.Fields {
			offs = append(offs, int(off))
		}
		sort.Ints(offs)
		var parts []string
		for _, off := range offs {
			parts = append(parts, fmt.Sprintf("+%X=%02X", off, m.Fields[int32(off)]))
		}
		fmt.Printf("   fields: %s\n", strings.Join(parts, " "))
		fmt.Printf("   inputs: %s\n", strings.Join(m.Inputs, " "))
		if xbeMaterialsVerbose {
			for i, p := range m.Paths {
				var guards []string
				for _, g := range p.Guards {
					guards = append(guards, xbe.ExprString(g))
				}
				fmt.Printf("   path %d guards: %s\n", i, strings.Join(guards, " && "))
				for _, e := range p.Effects {
					if e.Kind == "call" {
						var args []string
						for _, a := range e.Args {
							args = append(args, xbe.ExprString(a))
						}
						fmt.Printf("      %08X call %s(%s)\n", e.VA, e.Fn, strings.Join(args, ", "))
					} else {
						fmt.Printf("      %08X %s+%X.%d = %s\n", e.VA, e.Base, e.Off, e.Size, xbe.ExprString(e.Value))
					}
				}
				fmt.Printf("      return %s\n", xbe.ExprString(p.Return))
			}
		}
	}
	return nil
}

var xbeMaterialsVerbose bool

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

	outPath := xbeDisasmOutput
	if outPath == "" {
		outPath = strings.TrimSuffix(args[0], filepath.Ext(args[0])) + ".c"
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return err
	}
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

func runXBESymbols(cmd *cobra.Command, args []string) error {
	img, err := xbe.Open(args[0])
	if err != nil {
		return err
	}
	for _, l := range img.Libs {
		fmt.Fprintf(os.Stderr, "  %-8s %d.%d.%d\n", l.Name, l.Major, l.Minor, l.Build)
	}
	d, err := xbe.Disassemble(img)
	if err != nil {
		return err
	}
	for _, s := range d.Symbols {
		fmt.Printf("%08X %-9s %4d %3d %s\n", s.VA, s.Lib, s.Version, s.ArgBytes, s.Name)
	}
	fmt.Fprintf(os.Stderr, "%d symbols\n", len(d.Symbols))
	return nil
}

var xbeRenderOutput string

func runXBERender(cmd *cobra.Command, args []string) error {
	img, err := xbe.Open(args[0])
	if err != nil {
		return err
	}
	d, err := xbe.Disassemble(img)
	if err != nil {
		return err
	}
	kernel, err := loadKernel()
	if err != nil {
		return err
	}
	rm, err := xbe.DeriveRender(d, kernel)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "xdk %d materials %d programs %d declarations %d formats %X\n", rm.XDK, len(rm.Materials), len(rm.Programs), len(rm.Declarations), rm.Formats)
	fmt.Fprintf(os.Stderr, "transforms %v clock %+v hierarchy alpha %08X accessor %+v wrapper %08X\n", rm.Transforms, rm.Clock, rm.HierarchyAlpha, rm.Accessor, rm.Wrapper)
	fmt.Fprintf(os.Stderr, "fixed function %+v\n", rm.FixedFunction)
	fmt.Fprintf(os.Stderr, "state names %v\n", rm.StateNames)
	fmt.Fprintf(os.Stderr, "defaults paths %d lifted routines %d\n", len(rm.Defaults), len(rm.Lifted))
	data, err := json.MarshalIndent(rm, "", " ")
	if err != nil {
		return err
	}
	if xbeRenderOutput == "" {
		_, err = os.Stdout.Write(data)
		return err
	}
	if err := os.MkdirAll(filepath.Dir(xbeRenderOutput), 0o755); err != nil {
		return err
	}
	return os.WriteFile(xbeRenderOutput, data, 0o644)
}

// Lifts each routine named by address and prints its blocks, effects, register outputs and terminators
func runXBELift(cmd *cobra.Command, args []string) error {
	img, err := xbe.Open(args[0])
	if err != nil {
		return err
	}
	d, err := xbe.Disassemble(img)
	if err != nil {
		return err
	}
	ex := xbe.NewExecutor(d)
	for _, a := range args[1:] {
		var va uint32
		if _, err := fmt.Sscanf(a, "%x", &va); err != nil {
			return fmt.Errorf("lift: %q is no address", a)
		}
		lf, err := ex.Lift(va)
		if err != nil {
			fmt.Printf("== %08X: %v\n", va, err)
		} else {
			fmt.Printf("== %08X\n", va)
		}
		if lf != nil {
			fmt.Print(lf)
		}
	}
	return nil
}
