package bios

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/nickheyer/discoxip/pkg/xbe"
)

// Names the kernel's own routines are known by in the derivation
const (
	partitionName        = "Partition"
	ioCreateDeviceExport = "IoCreateDevice"
	cachePartitionExport = "HalDiskCachePartitionCount"
	fixedPartitions      = 2
	ataSectorSize        = 512
)

// Partition is one of the hard disk's partitions as the kernel creates its device, in sectors
type Partition struct {
	Number uint32 `json:"number"`
	Start  uint64 `json:"start"`
	Length uint64 `json:"length"`
}

// DiskRun is a span of the disk image held in the data file, by sector
type DiskRun struct {
	Sector uint64 `json:"sector"`
	Offset uint64 `json:"offset"`
	Size   uint64 `json:"size"`
}

// Disk is the hard disk and the DVD drive the kernel finds on its IDE channel
type Disk struct {
	Sectors    uint64      `json:"sectors"`
	Partitions []Partition `json:"partitions"`
	Runs       []DiskRun   `json:"runs"`
	File       string      `json:"file"`
	Identify   []uint16    `json:"identify"`
	DVD        []uint16    `json:"dvd_identify"`
}

// The names the disk is written under
const (
	DiskFile      = "disk.bin"
	DiskIndexFile = "disk.json"
)

// Identity is what the drives report to an identify command
type Identity struct {
	Model    string
	Serial   string
	Firmware string
}

// The drives the build puts on the channel
func DefaultDrives() (hdd, dvd Identity) {
	return Identity{Model: "DISCOXIP VIRTUAL HARD DISK", Serial: "DISCOXIP000001", Firmware: "1.0"},
		Identity{Model: "DISCOXIP VIRTUAL DVD-ROM", Serial: "DISCOXIP000002", Firmware: "1.0"}
}

// Partitions derives the hard disk's partitions by running the kernel's own partition device routine for every
// number the kernel creates one for, the device creation answered with an object the routine fills in
func (b *Bank) Partitions(mm *xbe.MachineModel, img *xbe.Image, exports map[uint32]uint32) ([]Partition, error) {
	byName := map[string]uint32{}
	for ordinal, va := range exports {
		byName[xbe.KernelExportName(ordinal)] = va
	}
	ioCreateDevice, ok := byName[ioCreateDeviceExport]
	if !ok {
		return nil, fmt.Errorf("bios: the kernel exports no %s", ioCreateDeviceExport)
	}
	cacheCountVA, ok := byName[cachePartitionExport]
	if !ok {
		return nil, fmt.Errorf("bios: the kernel exports no %s", cachePartitionExport)
	}
	cacheCount, ok := img.ReadU32(cacheCountVA)
	if !ok {
		return nil, fmt.Errorf("bios: %s lies outside the kernel image", cachePartitionExport)
	}
	creator, err := partitionCreator(mm, img, ioCreateDevice)
	if err != nil {
		return nil, err
	}
	var out []Partition
	for n := uint32(1); n <= fixedPartitions+cacheCount; n++ {
		p, err := runPartitionCreator(mm, img, creator, ioCreateDevice, n)
		if err != nil {
			return nil, fmt.Errorf("bios: partition %d: %w", n, err)
		}
		out = append(out, p)
	}
	return out, nil
}

// The routine creating a partition device by number, called by the parse routine that reads the number out of a
// name after the partition prefix, and itself creating a device object
func partitionCreator(mm *xbe.MachineModel, img *xbe.Image, ioCreateDevice uint32) (uint32, error) {
	prefix := findString(img, partitionName)
	if prefix == 0 {
		return 0, fmt.Errorf("bios: the kernel holds no %q", partitionName)
	}
	// The counted string naming the prefix holds its length, its size and the buffer's address
	var counted []byte
	counted = binary.LittleEndian.AppendUint16(counted, uint16(len(partitionName)))
	counted = binary.LittleEndian.AppendUint16(counted, uint16(len(partitionName)+1))
	counted = binary.LittleEndian.AppendUint32(counted, prefix)
	stringVA := findBytes(img, counted)
	if stringVA == 0 {
		return 0, fmt.Errorf("bios: the kernel holds no counted string for %q", partitionName)
	}
	routines := mm.Routines()
	for _, blockVA := range mm.BlocksStoring(stringVA) {
		parser, ok := mm.RoutineOf(blockVA, routines)
		if !ok {
			continue
		}
		_, callees := mm.RoutineBlocks(parser)
		for _, callee := range callees {
			if mm.CallsTo(callee, ioCreateDevice) {
				return callee, nil
			}
		}
	}
	return 0, fmt.Errorf("bios: no routine parsing %q creates a device", partitionName)
}

// Runs the creator for one partition number and reads the offset, length and start the routine stores in the
// device extension it fills, the start identified as the sector whose byte offset is the offset stored
func runPartitionCreator(mm *xbe.MachineModel, img *xbe.Image, creator, ioCreateDevice, n uint32) (Partition, error) {
	const scratchStack = 0x7FFF0000
	const deviceObject = 0x7FFE0000
	const deviceExtension = 0x7FFE1000
	const returnTo = 0x7FFD0000
	run := xbe.NewMachineRun(mm, img, map[string]uint32{"ESP": scratchStack})
	run.Push(n)
	run.Push(returnTo)
	run.Stop[returnTo] = true
	extensionAt := uint32(0)
	run.Hooks[ioCreateDevice] = func(r *xbe.MachineRun) uint32 {
		// The kernel's device creation hands back an object whose extension pointer the caller fills through
		ret := r.Pop()
		args := make([]uint32, 6)
		for i := range args {
			args[i] = r.Pop()
		}
		r.WriteU32(deviceObject+0x18, deviceExtension)
		r.WriteU32(args[5], deviceObject)
		extensionAt = deviceExtension
		r.Regs["EAX"] = 0
		return ret
	}
	if _, err := run.Run(creator, 100000); err != nil {
		return Partition{}, err
	}
	if extensionAt == 0 {
		return Partition{}, fmt.Errorf("the creator never created a device")
	}
	words := make([]uint32, 9)
	for i := range words {
		words[i] = run.ReadU32(extensionAt + uint32(4*i))
	}
	if words[0] != deviceObject {
		return Partition{}, fmt.Errorf("the extension does not start with its device")
	}
	pair := func(i int) uint64 { return uint64(words[i]) | uint64(words[i+1])<<32 }
	for i := 1; i+1 < len(words); i++ {
		offset := pair(i)
		if offset == 0 || offset%ataSectorSize != 0 {
			continue
		}
		start := offset / ataSectorSize
		found := false
		for j := 1; j < len(words); j++ {
			if j != i && j != i+1 && uint64(words[j]) == start {
				found = true
			}
		}
		if !found {
			continue
		}
		for k := 1; k+1 < len(words); k++ {
			if k == i || k == i+1 || k == i-1 {
				continue
			}
			length := pair(k)
			if length != 0 && length%ataSectorSize == 0 && length != offset {
				return Partition{Number: n, Start: start, Length: length / ataSectorSize}, nil
			}
		}
	}
	return Partition{}, fmt.Errorf("the extension holds no offset, length and start")
}

// The address of a terminated string in the image, zero where it holds none
func findString(img *xbe.Image, s string) uint32 {
	return findBytes(img, append([]byte(s), 0))
}

func findBytes(img *xbe.Image, needle []byte) uint32 {
	for i := range img.Sections {
		sec := &img.Sections[i]
		if at := bytes.Index(sec.Data, needle); at >= 0 {
			return sec.VirtualAddr + uint32(at)
		}
	}
	return 0
}

// BuildDisk lays the partitions out as volumes, the launch partition holding the directory given and the others
// empty, and sizes the disk to hold them
func BuildDisk(partitions []Partition, launch uint32, dir string, hdd, dvd Identity) (*Disk, []byte, error) {
	var sectors uint64
	for _, p := range partitions {
		if end := p.Start + p.Length; end > sectors {
			sectors = end
		}
	}
	disk := &Disk{Sectors: sectors, Partitions: partitions, File: DiskFile}
	var data []byte
	for _, p := range partitions {
		tree := EmptyTree()
		if p.Number == launch {
			var err error
			if tree, err = ReadTree(dir); err != nil {
				return nil, nil, err
			}
		}
		runs, err := FormatFATX(p.Length, tree, p.Number)
		if err != nil {
			return nil, nil, fmt.Errorf("bios: partition %d: %w", p.Number, err)
		}
		for _, r := range runs {
			at := p.Start*ataSectorSize + r.Offset
			if at%ataSectorSize != 0 {
				return nil, nil, fmt.Errorf("bios: partition %d holds a run off a sector boundary", p.Number)
			}
			size := (uint64(len(r.Data)) + ataSectorSize - 1) / ataSectorSize * ataSectorSize
			padded := make([]byte, size)
			copy(padded, r.Data)
			disk.Runs = append(disk.Runs, DiskRun{Sector: at / ataSectorSize, Offset: uint64(len(data)), Size: size})
			data = append(data, padded...)
		}
	}
	sort.Slice(disk.Runs, func(i, j int) bool { return disk.Runs[i].Sector < disk.Runs[j].Sector })
	disk.Identify = ataIdentify(hdd, sectors)
	disk.DVD = atapiIdentify(dvd)
	return disk, data, nil
}

// The identify words of a hard disk of the size given, addressed by logical block, moving data by direct memory
// access, with no security feature set so the kernel neither unlocks nor locks it
func ataIdentify(id Identity, sectors uint64) []uint16 {
	w := make([]uint16, 256)
	w[0] = 0x0040
	cylinders := sectors / (16 * 63)
	if cylinders > 16383 {
		cylinders = 16383
	}
	w[1], w[3], w[6] = uint16(cylinders), 16, 63
	ataString(w[10:20], id.Serial)
	ataString(w[23:27], id.Firmware)
	ataString(w[27:47], id.Model)
	w[47] = 0x8010
	w[49] = 0x0F00
	w[50] = 0x4000
	w[51], w[52] = 0x0200, 0x0200
	w[53] = 0x0007
	w[54], w[55], w[56] = uint16(cylinders), 16, 63
	current := cylinders * 16 * 63
	w[57], w[58] = uint16(current), uint16(current>>16)
	w[59] = 0x0110
	lba := sectors
	if lba > 0x0FFFFFFF {
		lba = 0x0FFFFFFF
	}
	w[60], w[61] = uint16(lba), uint16(lba>>16)
	w[63] = 0x0407
	w[64] = 0x0003
	w[65], w[66], w[67], w[68] = 120, 120, 120, 120
	w[80] = 0x001E
	w[81] = 0x0017
	w[82] = 0x4020
	w[83] = 0x4000
	w[84] = 0x4000
	w[85] = 0x0020
	w[86] = 0
	w[87] = 0x4000
	w[88] = 0x0407
	w[93] = 0x6001
	w[128] = 0
	w[255] = 0xA5A5
	return w
}

// The identify words of the DVD drive, a removable packet device with no media in it
func atapiIdentify(id Identity) []uint16 {
	w := make([]uint16, 256)
	w[0] = 0x85C0
	ataString(w[10:20], id.Serial)
	ataString(w[23:27], id.Firmware)
	ataString(w[27:47], id.Model)
	w[49] = 0x0F00
	w[51], w[52] = 0x0200, 0x0200
	w[53] = 0x0007
	w[63] = 0x0407
	w[64] = 0x0003
	w[65], w[66], w[67], w[68] = 120, 120, 120, 120
	w[80] = 0x001E
	w[82] = 0x4010
	w[83] = 0x4000
	w[84] = 0x4000
	w[85] = 0x0010
	w[87] = 0x4000
	w[88] = 0x0407
	w[255] = 0xA5A5
	return w
}

// Writes a string into identify words, each pair of characters swapped and the rest padded with spaces
func ataString(words []uint16, s string) {
	b := make([]byte, len(words)*2)
	for i := range b {
		b[i] = ' '
	}
	copy(b, s)
	for i := range words {
		words[i] = uint16(b[2*i])<<8 | uint16(b[2*i+1])
	}
}

// WriteDisk writes the disk's index and data into a directory
func WriteDisk(disk *Disk, data []byte, dir string) error {
	index, err := json.Marshal(disk)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, DiskIndexFile), index, 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, DiskFile), data, 0o644)
}
