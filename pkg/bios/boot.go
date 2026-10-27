package bios

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/nickheyer/discoxip/pkg/xbe"
	"golang.org/x/arch/x86/x86asm"
)

// Where the preldr leaves the decrypted 2BL, where the 2BL moves itself, and where the ROM leaves the stack
const (
	bldrLoadBase   = 0x00090000
	bldrRunBase    = 0x00400000
	romStackTop    = 0x0008F000
	romGDTBase     = 0xFFFFFFD8
	romGDTLimit    = 0x18
	xcodeStart     = 0x80
	xcodeExit      = 0xEE
	codeSegment    = 0x08
	dataSegment    = 0x10
	cr0Protected   = 0x00000011
	kernelPhysBase = 0x00010000
)

// XCode is one instruction of the init table the MCPX ROM interprets before the 2BL runs
type XCode struct {
	Op uint8  `json:"op"`
	A  uint32 `json:"a"`
	B  uint32 `json:"b"`
}

// MemoryRun is a file whose bytes the loader lays into memory at an address
type MemoryRun struct {
	At   uint32 `json:"at"`
	File string `json:"file"`
	Size int    `json:"size"`
}

// KernelLoad is the 2BL routine that decrypts and decompresses the kernel, which the loader answers with the bytes
// the build already produced
type KernelLoad struct {
	VA   uint32    `json:"va"`
	Pops uint32    `json:"pops"`
	Run  MemoryRun `json:"run"`
}

// Descriptor is a descriptor table pointer as the processor holds it
type Descriptor struct {
	Base  uint32 `json:"base"`
	Limit uint32 `json:"limit"`
}

// Boot is what the MCPX ROM and the preldr leave for the 2BL, and the point the loader answers the kernel load at
type Boot struct {
	XCodes   []XCode           `json:"xcodes"`
	Flash    MemoryRun         `json:"flash"`
	Bldr     MemoryRun         `json:"bldr"`
	Kernel   KernelLoad        `json:"kernel"`
	Entry    uint32            `json:"entry"`
	Regs     map[string]uint32 `json:"regs"`
	Segments map[string]uint32 `json:"segments"`
	GDT      Descriptor        `json:"gdt"`
	CR0      uint32            `json:"cr0"`
}

// The names the boot writes its memory runs under
const (
	FlashFile  = "flash.bin"
	BldrFile   = "bldr.bin"
	KernelFile = "kernel.bin"
	EEPROMFile = "eeprom.bin"
)

// XCodes decodes the init table from its start to its exit
func (b *Bank) XCodes() ([]XCode, error) {
	var out []XCode
	for off := xcodeStart; off+9 <= len(b.InitTable); off += 9 {
		x := XCode{Op: b.InitTable[off], A: binary.LittleEndian.Uint32(b.InitTable[off+1:]), B: binary.LittleEndian.Uint32(b.InitTable[off+5:])}
		out = append(out, x)
		if x.Op == xcodeExit {
			return out, nil
		}
	}
	return nil, fmt.Errorf("bios: the init table has no exit x-code")
}

// BldrImage lays the decrypted 2BL out at the address the preldr loads it and at the one it moves itself to
func (b *Bank) BldrImage() *xbe.Image {
	section := func(name string, base uint32) xbe.Section {
		data := make([]byte, len(b.Bldr))
		copy(data, b.Bldr)
		return xbe.Section{Name: name, VirtualAddr: base, VirtualSize: uint32(len(data)), RawSize: uint32(len(data)), Flags: 0x5, Data: data}
	}
	return xbe.NewImage(bldrLoadBase, b.Entry, nil, []xbe.Section{section("bldr0", bldrLoadBase), section("bldr1", bldrRunBase)})
}

// BldrRoots are the addresses the 2BL is entered at, its entry where the preldr leaves it and the same code where it
// runs after moving itself
func (b *Bank) BldrRoots() []uint32 {
	entry := b.Entry & 0xFFFF
	return []uint32{bldrLoadBase + entry, bldrRunBase + entry}
}

// Boot builds the state the loader starts the 2BL in, the kernel load answered at the routine the 2BL hands the
// kernel key to
func (b *Bank) Boot() (*Boot, error) {
	xcodes, err := b.XCodes()
	if err != nil {
		return nil, err
	}
	load, err := b.kernelLoad()
	if err != nil {
		return nil, err
	}
	boot := &Boot{
		XCodes: xcodes,
		Flash:  MemoryRun{At: 0, File: FlashFile, Size: len(b.Data)},
		Bldr:   MemoryRun{At: bldrLoadBase, File: BldrFile, Size: len(b.Bldr)},
		Kernel: KernelLoad{VA: load, Pops: 4, Run: MemoryRun{At: kernelPhysBase, File: KernelFile, Size: len(b.Kernel)}},
		Entry:  bldrLoadBase + b.Entry&0xFFFF,
		// The ROM hands the 2BL its own address in ebp and the stack it set below the 2BL
		Regs:     map[string]uint32{"EBP": bldrLoadBase, "ESP": romStackTop},
		Segments: map[string]uint32{"CS": codeSegment, "DS": dataSegment, "ES": dataSegment, "SS": dataSegment, "FS": 0, "GS": 0},
		GDT:      Descriptor{Base: romGDTBase, Limit: romGDTLimit},
		CR0:      cr0Protected,
	}
	return boot, nil
}

// The routine the 2BL calls with the kernel key to decrypt and decompress the kernel, found where the key's address
// is pushed straight before a call
func (b *Bank) kernelLoad() (uint32, error) {
	entryOff := int(b.Entry & 0xFFFF)
	keysPtr := binary.LittleEndian.Uint32(b.Bldr[entryOff-8:])
	kernelKey := bldrRunBase + keysPtr&0xFFFF + 2*keySize
	var pushed uint32
	for off := 0; off < len(b.Bldr); {
		in, err := x86asm.Decode(b.Bldr[off:], 32)
		if err != nil || in.Len == 0 {
			off++
			pushed = 0
			continue
		}
		va := uint32(bldrRunBase + off)
		switch {
		case in.Op == x86asm.PUSH && len(in.Args) > 0:
			if imm, ok := in.Args[0].(x86asm.Imm); ok {
				pushed = uint32(imm)
			} else {
				pushed = 0
			}
		case in.Op == x86asm.CALL && pushed == kernelKey:
			if rel, ok := in.Args[0].(x86asm.Rel); ok {
				return va + uint32(in.Len) + uint32(int32(rel)), nil
			}
			pushed = 0
		default:
			pushed = 0
		}
		off += in.Len
	}
	return 0, fmt.Errorf("bios: the 2BL never calls a routine with the kernel key at %08X", kernelKey)
}

// WriteBoot writes the machine lift, the boot state, the memory runs and the disk the loader boots from into a
// directory, the launch partition holding the dashboard directory given
func WriteBoot(b *Bank, dir, launchDir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	lifted, err := b.Machine()
	if err != nil {
		return err
	}
	mm := lifted.Model
	boot, err := b.Boot()
	if err != nil {
		return err
	}
	partitions, err := b.Partitions(mm, lifted.Kernel, lifted.Exports)
	if err != nil {
		return err
	}
	launch, err := launchPartition(lifted.Kernel, launchDir)
	if err != nil {
		return err
	}
	hdd, dvd := DefaultDrives()
	disk, diskData, err := BuildDisk(partitions, launch, launchDir, hdd, dvd)
	if err != nil {
		return err
	}
	if err := WriteDisk(disk, diskData, dir); err != nil {
		return err
	}
	eeprom, err := b.EEPROM(DefaultConsole())
	if err != nil {
		return err
	}
	files := map[string][]byte{FlashFile: b.Data, BldrFile: b.Bldr, KernelFile: b.Kernel, EEPROMFile: eeprom}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			return err
		}
	}
	for name, v := range map[string]any{"machine.json": mm, "boot.json": boot, "hardware.json": b.Hardware()} {
		data, err := json.Marshal(v)
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			return err
		}
	}
	fmt.Printf("boot: %d blocks, %d failed, %d expression nodes, kernel load at %08X\n", len(mm.Blocks), len(mm.Errors), len(mm.Exprs.Nodes), boot.Kernel.VA)
	for _, p := range partitions {
		fmt.Printf("  partition %d at sector %d, %d sectors\n", p.Number, p.Start, p.Length)
	}
	fmt.Printf("  disk of %d sectors, %d bytes of volume data in %d runs\n", disk.Sectors, len(diskData), len(disk.Runs))
	return nil
}

// Reads the partition number out of the kernel path naming an executable the launch directory holds
func launchPartition(kernel *xbe.Image, launchDir string) (uint32, error) {
	names, err := executableNames(launchDir)
	if err != nil {
		return 0, err
	}
	var paths []string
	for _, s := range kernelStrings(kernel) {
		lower := strings.ToLower(s)
		for _, name := range names {
			if strings.HasSuffix(lower, `\`+name) {
				paths = append(paths, s)
			}
		}
	}
	if len(paths) != 1 {
		return 0, fmt.Errorf("bios: %d kernel paths name an executable of %s (%v of %v)", len(paths), launchDir, paths, names)
	}
	lower := strings.ToLower(paths[0])
	at := strings.LastIndex(lower, strings.ToLower(partitionName))
	if at < 0 {
		return 0, fmt.Errorf("bios: the launch path %q names no %s", paths[0], partitionName)
	}
	digits := lower[at+len(partitionName):]
	digits = digits[:strings.IndexFunc(digits+`\`, func(r rune) bool { return r < '0' || r > '9' })]
	n, err := strconv.ParseUint(digits, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("bios: the launch path %q numbers no partition", paths[0])
	}
	return uint32(n), nil
}

// The lower cased names of the executables under a directory, told by the magic their first bytes carry
func executableNames(dir string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(dir, func(path string, e fs.DirEntry, err error) error {
		if err != nil || e.IsDir() {
			return err
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		magic := make([]byte, len(xbe.Magic))
		if n, _ := f.Read(magic); n == len(magic) && string(magic) == xbe.Magic {
			out = append(out, strings.ToLower(filepath.Base(path)))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("bios: %s holds no executable", dir)
	}
	return out, nil
}

// Every terminated run of printable bytes in the kernel image
func kernelStrings(img *xbe.Image) []string {
	var out []string
	for i := range img.Sections {
		data := img.Sections[i].Data
		start := -1
		for off, b := range data {
			printable := b >= ' ' && b <= '~'
			if printable && start < 0 {
				start = off
			}
			if !printable {
				if start >= 0 && b == 0 {
					out = append(out, string(data[start:off]))
				}
				start = -1
			}
		}
	}
	return out
}
