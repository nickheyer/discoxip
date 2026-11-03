package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/nickheyer/discoxip/pkg/native"
	"github.com/nickheyer/discoxip/pkg/native/xcode"
	"github.com/spf13/cobra"
)

var nativeOutput, nativeReport, nativeWork string

func init() {
	nativeCmd := &cobra.Command{
		Use:   "native",
		Short: "Reference source tools, assembling and verifying the native tree against the originals",
	}
	xcodeCmd := &cobra.Command{
		Use:   "xcode",
		Short: "Init table x-code assembler",
	}
	asmCmd := &cobra.Command{
		Use:   "asm <table.xc>",
		Short: "Assemble init table source into the bytes the flash carries",
		Args:  cobra.ExactArgs(1),
		RunE:  runXcodeAsm,
	}
	disasmCmd := &cobra.Command{
		Use:   "disasm <inittbl.bin>",
		Short: "Render a flash init table as source that assembles back to the same bytes",
		Args:  cobra.ExactArgs(1),
		RunE:  runXcodeDisasm,
	}
	for _, c := range []*cobra.Command{asmCmd, disasmCmd} {
		c.Flags().StringVarP(&nativeOutput, "output", "o", "", "output file (default: input with the extension swapped)")
	}
	verifyCmd := &cobra.Command{
		Use:          "verify <manifest.json>",
		Short:        "Rebuild every source the manifest lists and compare it with its reference bytes",
		Args:         cobra.ExactArgs(1),
		RunE:         runNativeVerify,
		SilenceUsage: true,
	}
	verifyCmd.Flags().StringVarP(&nativeReport, "report", "o", "", "write the results as JSON to this file")
	verifyCmd.Flags().StringVar(&nativeWork, "work", "build/native/verify", "directory for assembler and compiler intermediates")
	dumpCmd := &cobra.Command{
		Use:   "dump <object.obj | library.lib member.obj>",
		Short: "Print a reference object's source path, prototypes, types, data, line numbers and code",
		Args:  cobra.RangeArgs(1, 2),
		RunE:  runNativeDump,
	}
	headersCmd := &cobra.Command{
		Use:   "headers <xboxkrnl.pdb | library.lib>...",
		Short: "Write private headers holding every type, typedef, routine and global the inputs describe",
		Args:  cobra.MinimumNArgs(1),
		RunE:  runNativeHeaders,
	}
	headersCmd.Flags().StringVarP(&nativeOutput, "output", "o", "native/private/ntos/inc", "directory the headers are written to")
	xcodeCmd.AddCommand(asmCmd, disasmCmd)
	nativeCmd.AddCommand(xcodeCmd, verifyCmd, dumpCmd, headersCmd)
	rootCmd.AddCommand(nativeCmd)
}

// Verifies the manifest's entries, printing one line each and failing when any is not byte exact
func runNativeVerify(cmd *cobra.Command, args []string) error {
	data, err := os.ReadFile(args[0])
	if err != nil {
		return err
	}
	var entries []native.Entry
	if err := json.Unmarshal(data, &entries); err != nil {
		return fmt.Errorf("%s: %v", args[0], err)
	}
	results := native.VerifyAll(entries, nativeWork)
	if nativeReport != "" {
		out, err := json.MarshalIndent(results, "", "  ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(nativeReport, out, 0o644); err != nil {
			return err
		}
	}
	short := 0
	for _, r := range results {
		switch {
		case r.Error != "":
			fmt.Printf("  --  %-50s %s\n", r.File, strings.SplitN(r.Error, "\n", 2)[0])
			short++
		case r.Exact:
			fmt.Printf("100%%  %-50s %d %s\n", r.File, r.Total, r.Unit)
		default:
			fmt.Printf("%3d%%  %-50s %d of %d %s\n", r.Matched*100/max(r.Total, 1), r.File, r.Matched, r.Total, r.Unit)
			if r.Detail != "" {
				fmt.Printf("      %s\n", r.Detail)
			}
			short++
		}
	}
	if short > 0 {
		return fmt.Errorf("%d of %d entries do not rebuild byte for byte", short, len(results))
	}
	return nil
}

// Returns the output flag or the input path with its extension replaced
func nativeOut(in, ext string) string {
	if nativeOutput != "" {
		return nativeOutput
	}
	if i := strings.LastIndexByte(in, '.'); i > strings.LastIndexByte(in, '/') {
		in = in[:i]
	}
	return in + ext
}

func runXcodeAsm(cmd *cobra.Command, args []string) error {
	src, err := os.ReadFile(args[0])
	if err != nil {
		return err
	}
	bin, err := xcode.Assemble(src)
	if err != nil {
		return err
	}
	out := nativeOut(args[0], ".bin")
	if err := os.WriteFile(out, bin, 0o644); err != nil {
		return err
	}
	fmt.Printf("%s: %d bytes\n", out, len(bin))
	return nil
}

func runXcodeDisasm(cmd *cobra.Command, args []string) error {
	bin, err := os.ReadFile(args[0])
	if err != nil {
		return err
	}
	src, err := xcode.Disassemble(bin)
	if err != nil {
		return err
	}
	out := nativeOut(args[0], ".xc")
	if err := os.WriteFile(out, src, 0o644); err != nil {
		return err
	}
	fmt.Printf("%s: %d lines\n", out, strings.Count(string(src), "\n"))
	return nil
}

func runNativeDump(cmd *cobra.Command, args []string) error {
	member := ""
	if len(args) == 2 {
		member = args[1]
	}
	return native.Dump(os.Stdout, args[0], member)
}

// Collects every input into one set of headers, listing definitions that disagree between inputs
func runNativeHeaders(cmd *cobra.Command, args []string) error {
	u := native.NewUniverse()
	for _, in := range args {
		var err error
		if strings.HasSuffix(strings.ToLower(in), ".pdb") {
			err = u.AddDatabase(in)
		} else {
			err = u.AddLibrary(in)
		}
		if err != nil {
			return err
		}
	}
	if err := u.Write(nativeOutput); err != nil {
		return err
	}
	for _, c := range u.Conflicts {
		fmt.Println("conflict:", c)
	}
	fmt.Printf("%s: %d types, %d typedefs, %d routines, %d globals, %d conflicts\n", nativeOutput, len(u.Defs), len(u.Typedefs), len(u.Routines), len(u.Globals), len(u.Conflicts))
	return nil
}
