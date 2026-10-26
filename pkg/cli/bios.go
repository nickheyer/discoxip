package cli

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/nickheyer/discoxip/pkg/bios"
	"github.com/nickheyer/discoxip/pkg/xbe"
	"github.com/spf13/cobra"
)

var biosMCPX, biosOutput, biosLiftVA, biosLaunch string

func init() {
	biosCmd := &cobra.Command{
		Use:   "bios",
		Short: "Flash image tools",
	}
	infoCmd := &cobra.Command{
		Use:   "info <flash.bin>",
		Short: "Decrypt the 2BL and describe the kernel the image carries",
		Args:  cobra.ExactArgs(1),
		RunE:  runBIOSInfo,
	}
	extractCmd := &cobra.Command{
		Use:   "extract <flash.bin>",
		Short: "Write the 2BL, the kernel, its data section, the init table and the keys",
		Args:  cobra.ExactArgs(1),
		RunE:  runBIOSExtract,
	}
	for _, c := range []*cobra.Command{infoCmd, extractCmd} {
		c.Flags().StringVar(&biosMCPX, "mcpx", "", "MCPX ROM dump holding the 2BL key (required)")
	}
	liftCmd := &cobra.Command{
		Use:   "lift <flash.bin>",
		Short: "Disassemble the kernel and lift its exports, reporting what fails",
		Args:  cobra.ExactArgs(1),
		RunE:  runBIOSLift,
	}
	liftCmd.Flags().StringVar(&biosMCPX, "mcpx", "", "MCPX ROM dump holding the 2BL key (required)")
	extractCmd.Flags().StringVarP(&biosOutput, "output", "o", "", "output directory (default: <flash>.out)")
	liftCmd.Flags().StringVar(&biosLiftVA, "va", "", "lift one routine at this hex address and print its blocks")
	machineCmd := &cobra.Command{
		Use:   "machine <flash.bin>",
		Short: "Lift the kernel as the processor runs it and report every block that fails",
		Args:  cobra.ExactArgs(1),
		RunE:  runBIOSMachine,
	}
	machineCmd.Flags().StringVar(&biosMCPX, "mcpx", "", "MCPX ROM dump holding the 2BL key (required)")
	machineCmd.Flags().StringVar(&biosLiftVA, "va", "", "print the blocks in a hex address range, as lo-hi")
	bootCmd := &cobra.Command{
		Use:   "boot <flash.bin>",
		Short: "Write the machine lift of the 2BL and kernel with the state the ROM leaves them in",
		Args:  cobra.ExactArgs(1),
		RunE:  runBIOSBoot,
	}
	bootCmd.Flags().StringVar(&biosMCPX, "mcpx", "", "MCPX ROM dump holding the 2BL key (required)")
	bootCmd.Flags().StringVarP(&biosOutput, "output", "o", "", "output directory (required)")
	bootCmd.Flags().StringVar(&biosLaunch, "launch", "", "directory holding the dashboard the launch partition carries (required)")
	bootCmd.MarkFlagRequired("output")
	bootCmd.MarkFlagRequired("launch")
	biosCmd.AddCommand(infoCmd, extractCmd, liftCmd, machineCmd, bootCmd)
	rootCmd.AddCommand(biosCmd)
}

// Opens the bank and decrypts it with the MCPX ROM named by the flag, naming the revision needed otherwise
func openBIOS(path string) (*bios.Bank, error) {
	b, err := bios.Open(path)
	if err != nil {
		return nil, err
	}
	if biosMCPX == "" {
		return nil, fmt.Errorf("bios: %s boots on MCPX 1.%d, pass that ROM dump with --mcpx", path, b.MCPXRevision())
	}
	mcpx, err := bios.LoadMCPX(biosMCPX)
	if err != nil {
		return nil, err
	}
	if err := b.Decrypt(mcpx); err != nil {
		return nil, err
	}
	return b, nil
}

func runBIOSInfo(cmd *cobra.Command, args []string) error {
	b, err := openBIOS(args[0])
	if err != nil {
		return err
	}
	pe, err := bios.ParsePE(b.Kernel)
	if err != nil {
		return err
	}
	fmt.Printf("Flash: %s\n", args[0])
	fmt.Printf("  MCPX:         1.%d\n", b.MCPXRevision())
	fmt.Printf("  2BL entry:    %08X\n", b.Entry)
	fmt.Printf("  init table:   %d bytes\n", b.Params.InitTableSize)
	fmt.Printf("  kernel:       %d bytes compressed, %d bytes decompressed\n", b.Params.KernelSize, len(b.Kernel))
	fmt.Printf("  kernel data:  %d bytes\n", b.Params.KernelDataSize)
	fmt.Printf("  eeprom key:   %s\n", hex.EncodeToString(b.Keys.EEPROM[:]))
	fmt.Printf("  cert key:     %s\n", hex.EncodeToString(b.Keys.Cert[:]))
	fmt.Printf("  kernel key:   %s\n", hex.EncodeToString(b.Keys.Kernel[:]))
	fmt.Printf("Kernel:\n")
	fmt.Printf("  built:        %s\n", time.Unix(int64(pe.TimeDateStamp), 0).UTC().Format(time.RFC1123))
	fmt.Printf("  image base:   %08X\n", pe.ImageBase)
	fmt.Printf("  entry point:  %08X\n", pe.ImageBase+pe.EntryPoint)
	fmt.Printf("  image size:   %d bytes\n", pe.SizeOfImage)
	exports, err := pe.Exports()
	if err != nil {
		return err
	}
	fmt.Printf("  exports:      %d\n", len(exports))
	for _, s := range pe.Sections {
		fmt.Printf("  section %-8s va %08X size %6d raw %6d at %6X flags %08X\n", s.Name, pe.ImageBase+s.VirtualAddress, s.VirtualSize, s.RawSize, s.RawOffset, s.Characteristics)
	}
	return nil
}

func runBIOSExtract(cmd *cobra.Command, args []string) error {
	b, err := openBIOS(args[0])
	if err != nil {
		return err
	}
	dir := biosOutput
	if dir == "" {
		dir = args[0] + ".out"
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	files := map[string][]byte{
		"2bl.bin":        b.Bldr,
		"kernel.exe":     b.Kernel,
		"kerneldata.bin": b.KernelData,
		"inittbl.bin":    b.InitTable,
		"eeprom.key":     b.Keys.EEPROM[:],
		"cert.key":       b.Keys.Cert[:],
		"kernel.key":     b.Keys.Kernel[:],
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			return err
		}
	}
	fmt.Printf("Extracted %d files to %s\n", len(files), dir)
	return nil
}

func runBIOSLift(cmd *cobra.Command, args []string) error {
	b, err := openBIOS(args[0])
	if err != nil {
		return err
	}
	img, exports, err := b.Image()
	if err != nil {
		return err
	}
	d, err := xbe.DisassembleKernel(img, exports)
	if err != nil {
		return err
	}
	if biosLiftVA != "" {
		var va uint32
		if _, err := fmt.Sscanf(biosLiftVA, "%x", &va); err != nil {
			return fmt.Errorf("lift: %q is no address", biosLiftVA)
		}
		fmt.Println(d.Describe(va))
		lf, err := xbe.NewExecutor(d).Lift(va)
		if err != nil {
			fmt.Printf("== %08X: %v\n", va, err)
		}
		if lf != nil {
			fmt.Print(lf)
		}
		return nil
	}
	routines := 0
	for _, va := range exports {
		if d.Functions[va] != nil {
			routines++
		}
	}
	fmt.Printf("kernel: %d functions, %d exports of which %d are routines\n", len(d.Functions), len(exports), routines)
	km, err := xbe.DeriveKernel(d, exports)
	if err != nil {
		return err
	}
	fmt.Printf("lifted %d routines, %d failed\n", len(km.Lifted), len(km.Errors))
	names := make([]string, 0, len(km.Errors))
	for name := range km.Errors {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		fmt.Printf("  %-32s %s\n", name, km.Errors[name])
	}
	return nil
}

var kernelBIOS, kernelMCPX string

// Adds the flags naming the flash image and MCPX ROM a derivation lifts the kernel from
func addKernelFlags(c *cobra.Command) {
	c.Flags().StringVar(&kernelBIOS, "bios", "", "flash image holding the kernel the dashboard runs on (required)")
	c.Flags().StringVar(&kernelMCPX, "mcpx", "", "MCPX ROM dump holding the 2BL key (required)")
	c.MarkFlagRequired("bios")
	c.MarkFlagRequired("mcpx")
}

// Decrypts the flash image the flags name and lays its kernel out for the derivation
func loadKernel() (*xbe.KernelInput, error) {
	b, err := bios.Open(kernelBIOS)
	if err != nil {
		return nil, err
	}
	mcpx, err := bios.LoadMCPX(kernelMCPX)
	if err != nil {
		return nil, err
	}
	if err := b.Decrypt(mcpx); err != nil {
		return nil, err
	}
	img, exports, err := b.Image()
	if err != nil {
		return nil, err
	}
	return &xbe.KernelInput{Image: img, Exports: exports}, nil
}

func runBIOSMachine(cmd *cobra.Command, args []string) error {
	b, err := openBIOS(args[0])
	if err != nil {
		return err
	}
	img, exports, err := b.Image()
	if err != nil {
		return err
	}
	d, err := xbe.DisassembleKernel(img, exports)
	if err != nil {
		return err
	}
	started := time.Now()
	mm, err := xbe.LiftMachine(d, xbe.KernelRoots(d, exports))
	if err != nil {
		return err
	}
	fmt.Printf("machine: %d blocks, %d failed, %d expression nodes, in %.1fs\n", len(mm.Blocks), len(mm.Errors), len(mm.Exprs.Nodes), time.Since(started).Seconds())
	if biosLiftVA != "" {
		var lo, hi uint32
		if _, err := fmt.Sscanf(biosLiftVA, "%x-%x", &lo, &hi); err != nil {
			if _, err := fmt.Sscanf(biosLiftVA, "%x", &lo); err != nil {
				return fmt.Errorf("machine: %q is no address range", biosLiftVA)
			}
			hi = lo
		}
		fmt.Print(mm.Dump(lo, hi))
		return nil
	}
	keys := make([]string, 0, len(mm.Errors))
	for key := range mm.Errors {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		fmt.Printf("  %-18s %s\n", key, mm.Errors[key])
	}
	return nil
}

func runBIOSBoot(cmd *cobra.Command, args []string) error {
	b, err := openBIOS(args[0])
	if err != nil {
		return err
	}
	return bios.WriteBoot(b, biosOutput, biosLaunch)
}
