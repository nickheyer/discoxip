package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/nickheyer/discoxip/pkg/bios"
	"github.com/nickheyer/discoxip/pkg/web"
	"github.com/nickheyer/discoxip/pkg/xbe"
	"github.com/spf13/cobra"
)

var xbeDisasmOutput string

func init() {
	xbeCmd = &cobra.Command{
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
	addKernelFlags(matsCmd)

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
	addKernelFlags(liftCmd)

	programCmd := &cobra.Command{
		Use:   "program <file.xbe>",
		Short: "Replay a standalone executable to its first frame and list what it loads",
		Args:  cobra.ExactArgs(1),
		RunE:  runXBEProgram,
	}
	programCmd.Flags().StringVarP(&xbeProgramOutput, "output", "o", "", "model file (default: none)")
	addKernelFlags(programCmd)

	xbeCmd.AddCommand(infoCmd, matsCmd, disasmCmd, symbolsCmd, renderCmd, liftCmd, programCmd)
	rootCmd.AddCommand(xbeCmd)
}

var xbeCmd *cobra.Command

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
	kernel, _, err := loadKernel()
	if err != nil {
		return err
	}
	kd, err := xbe.DisassembleKernel(kernel.Image, kernel.Exports)
	if err != nil {
		return err
	}
	d.LearnImports(kd)
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
	kernel, _, err := loadKernel()
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
	kernel, _, err := loadKernel()
	if err != nil {
		return err
	}
	kd, err := xbe.DisassembleKernel(kernel.Image, kernel.Exports)
	if err != nil {
		return err
	}
	d.LearnImports(kd)
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

var xbeProgramOutput string

// Replays an executable's startup and prints every section, texture, buffer, table, shader and draw it found
func runXBEProgram(cmd *cobra.Command, args []string) error {
	img, err := xbe.Open(args[0])
	if err != nil {
		return err
	}
	d, err := xbe.Disassemble(img)
	if err != nil {
		return err
	}
	kernel, _, err := loadKernel()
	if err != nil {
		return err
	}
	pm, err := xbe.DeriveProgram(d, kernel)
	if err != nil {
		return err
	}
	fmt.Printf("main %08X, launchers %X run from frames %v, %dx%d back buffer format %02X, sections %v\n", pm.Main, pm.Launchers, pm.RunFrames, pm.Width, pm.Height, pm.BackBuffer, pm.Sections)
	for _, t := range pm.Textures {
		fmt.Printf("texture %08X %-16s %4dx%-4d levels %2d format %02X cube %v usage %X\n", t.Object, t.Section, t.Width, t.Height, t.Levels, t.Format, t.Cube, t.Usage)
	}
	for _, b := range pm.Buffers {
		fmt.Printf("%-6s buffer %08X %-16s %d bytes\n", b.Kind, b.Object, b.Section, b.Size)
	}
	for _, t := range pm.Tables {
		fmt.Printf("table %08X %-16s offset %d, %d bytes\n", t.Address, t.Section, t.Offset, t.Size)
	}
	for _, s := range pm.Shaders {
		elements := 0
		if s.Declaration != nil {
			elements = len(s.Declaration.Elements)
		}
		fmt.Printf("shader %X declaration elements %d program %v\n", s.Handle, elements, s.Program != nil)
	}
	for _, s := range pm.Pixel {
		fmt.Printf("pixel shader %X definition %d words\n", s.Handle, len(s.Def))
	}
	for _, dr := range pm.Draws {
		if dr.Kind == "clear" {
			fmt.Printf("clear frame %d target %08X flags %X color %08X z %08X stencil %X rects %d\n", dr.Frame, dr.Target, dr.Flags, dr.Color, dr.Z, dr.Stencil, len(dr.Rects))
			continue
		}
		fmt.Printf("draw frame %d target %08X shader %X pixel %X stream %08X stride %d primitive %d start %d count %d indices %d textures %X constants %d states %d\n", dr.Frame, dr.Target, dr.Shader, dr.PixelShader, dr.Stream, dr.Stride, dr.Primitive, dr.Start, dr.Count, len(dr.Indices), dr.Textures, len(dr.Constants), len(dr.States))
	}
	if xbeProgramOutput != "" {
		data, err := json.MarshalIndent(pm, "", " ")
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(xbeProgramOutput), 0o755); err != nil {
			return err
		}
		return os.WriteFile(xbeProgramOutput, data, 0o644)
	}
	return nil
}

var xbeRuntimeBuild string

func init() {
	runtimeCmd := &cobra.Command{
		Use:   "runtime <file.xbe>",
		Short: "Translate the executable into the page's runtime inside a build's web directory",
		Args:  cobra.ExactArgs(1),
		RunE:  runXBERuntime,
	}
	runtimeCmd.Flags().StringVar(&xbeRuntimeBuild, "build", "", "build output directory holding .extracted and web (required)")
	runtimeCmd.MarkFlagRequired("build")
	addKernelFlags(runtimeCmd)
	xbeCmd.AddCommand(runtimeCmd)
}

// Rewrites a build's runtime, the console derived and the loader, the kernel and the executable lifted again
func runXBERuntime(cmd *cobra.Command, args []string) error {
	d, err := disassemble(args[0])
	if err != nil {
		return err
	}
	bank, mcpx, err := loadBoot()
	if err != nil {
		return err
	}
	webDir := filepath.Join(xbeRuntimeBuild, "web")
	return web.WriteRuntime(webDir, web.RuntimeInput{Dash: d, Bank: bank, MCPX: mcpx, Console: bios.DefaultConsole(), ConsoleDir: filepath.Dir(args[0])})
}
