package bios

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// The FATX volume layout the kernel's file system driver reads
const (
	fatxMagic             = "FATX"
	fatxSuperblockSize    = 0x1000
	fatxSectorSize        = 512
	fatxSectorsPerCluster = 32
	fatxRootCluster       = 1
	fatxEntrySize         = 64
	fatxNameSize          = 42
	fatxNameFree          = 0xFF
	fatxAttrDirectory     = 0x10
	fatxAttrArchive       = 0x20
	fatxFAT16Limit        = 0xFFF0
	fatxEndOfChain16      = 0xFFFF
	fatxEndOfChain32      = 0xFFFFFFFF
	fatxPage              = 0x1000
)

// Run is a span of bytes at an offset into a volume or a disk
type Run struct {
	Offset uint64
	Data   []byte
}

// fatxEntry is one file or directory as the volume holds it
type fatxEntry struct {
	name     string
	dir      bool
	data     []byte
	children []*fatxEntry
	modified time.Time
	cluster  uint32
}

// FormatFATX lays a directory tree out as a FATX volume of the sector count given, as runs from the volume's start
func FormatFATX(sectors uint64, root *fatxEntry, volumeID uint32) ([]Run, error) {
	clusterBytes := uint64(fatxSectorsPerCluster * fatxSectorSize)
	clusters := uint32(sectors * fatxSectorSize / clusterBytes)
	entrySize := uint64(2)
	if clusters >= fatxFAT16Limit {
		entrySize = 4
	}
	fatBytes := (uint64(clusters)*entrySize + fatxPage - 1) / fatxPage * fatxPage
	dataStart := uint64(fatxSuperblockSize) + fatBytes
	if dataStart+clusterBytes > sectors*fatxSectorSize {
		return nil, fmt.Errorf("fatx: %d sectors hold no data cluster", sectors)
	}
	fat := make([]byte, fatBytes)
	setFAT := func(cluster, value uint32) {
		if entrySize == 2 {
			binary.LittleEndian.PutUint16(fat[cluster*2:], uint16(value))
		} else {
			binary.LittleEndian.PutUint32(fat[cluster*4:], value)
		}
	}
	endOfChain := uint32(fatxEndOfChain16)
	if entrySize == 4 {
		endOfChain = fatxEndOfChain32
	}
	setFAT(0, endOfChain)
	next := uint32(fatxRootCluster)
	var runs []Run
	// Allocates a chain of clusters long enough for a byte count and hands back its first cluster
	reserve := func(size uint64) (uint32, error) {
		count := uint32((size + clusterBytes - 1) / clusterBytes)
		if count == 0 {
			count = 1
		}
		first := next
		if uint64(first)+uint64(count) > uint64(clusters) {
			return 0, fmt.Errorf("fatx: the volume of %d clusters is full", clusters)
		}
		for i := uint32(0); i < count; i++ {
			c := first + i
			if i+1 < count {
				setFAT(c, c+1)
			} else {
				setFAT(c, endOfChain)
			}
		}
		next += count
		return first, nil
	}
	emit := func(first uint32, data []byte) {
		if len(data) > 0 {
			runs = append(runs, Run{Offset: dataStart + uint64(first-1)*clusterBytes, Data: data})
		}
	}
	var layout func(e *fatxEntry) error
	layout = func(e *fatxEntry) error {
		// A directory takes its clusters before its children so the root lands first, its table written once the
		// children's clusters are known
		tableBytes := uint64(len(e.children)+1) * fatxEntrySize
		first, err := reserve(tableBytes)
		if err != nil {
			return err
		}
		e.cluster = first
		for _, c := range e.children {
			if c.dir {
				if err := layout(c); err != nil {
					return err
				}
			} else {
				at, err := reserve(uint64(len(c.data)))
				if err != nil {
					return err
				}
				c.cluster = at
				emit(at, c.data)
			}
		}
		padded := (tableBytes + clusterBytes - 1) / clusterBytes * clusterBytes
		table := make([]byte, padded)
		for i := range table {
			table[i] = fatxNameFree
		}
		at := 0
		for _, c := range e.children {
			copy(table[at:], fatxDirent(c))
			at += fatxEntrySize
		}
		emit(first, table)
		return nil
	}
	if err := layout(root); err != nil {
		return nil, err
	}
	if root.cluster != fatxRootCluster {
		return nil, fmt.Errorf("fatx: the root directory landed in cluster %d", root.cluster)
	}
	super := make([]byte, fatxSuperblockSize)
	copy(super, fatxMagic)
	binary.LittleEndian.PutUint32(super[4:], volumeID)
	binary.LittleEndian.PutUint32(super[8:], fatxSectorsPerCluster)
	binary.LittleEndian.PutUint32(super[12:], fatxRootCluster)
	for i := 16; i < fatxSuperblockSize; i++ {
		super[i] = 0xFF
	}
	runs = append([]Run{{Offset: 0, Data: super}, {Offset: fatxSuperblockSize, Data: fat}}, runs...)
	sort.Slice(runs, func(i, j int) bool { return runs[i].Offset < runs[j].Offset })
	return runs, nil
}

// One directory entry, the name padded with the free byte and the times in the file system's own encoding
func fatxDirent(e *fatxEntry) []byte {
	out := make([]byte, fatxEntrySize)
	out[0] = byte(len(e.name))
	if e.dir {
		out[1] = fatxAttrDirectory
	} else {
		out[1] = fatxAttrArchive
	}
	for i := 0; i < fatxNameSize; i++ {
		out[2+i] = fatxNameFree
	}
	copy(out[2:2+fatxNameSize], e.name)
	binary.LittleEndian.PutUint32(out[44:], e.cluster)
	if !e.dir {
		binary.LittleEndian.PutUint32(out[48:], uint32(len(e.data)))
	}
	t, d := fatxTime(e.modified)
	for _, off := range []int{52, 56, 60} {
		binary.LittleEndian.PutUint16(out[off:], t)
		binary.LittleEndian.PutUint16(out[off+2:], d)
	}
	return out
}

// A time as the file system packs it, seconds halved and years counted from 1980
func fatxTime(at time.Time) (uint16, uint16) {
	at = at.UTC()
	year := at.Year()
	if year < 1980 {
		year = 1980
	}
	t := uint16(at.Hour())<<11 | uint16(at.Minute())<<5 | uint16(at.Second()/2)
	d := uint16(year-1980)<<9 | uint16(at.Month())<<5 | uint16(at.Day())
	return t, d
}

// ReadTree reads a directory from disk as the tree a volume is formatted from, every name within the file system's limit
func ReadTree(dir string) (*fatxEntry, error) {
	root := &fatxEntry{name: "", dir: true}
	var walk func(path string, e *fatxEntry) error
	walk = func(path string, e *fatxEntry) error {
		entries, err := os.ReadDir(path)
		if err != nil {
			return err
		}
		sort.Slice(entries, func(i, j int) bool { return strings.ToLower(entries[i].Name()) < strings.ToLower(entries[j].Name()) })
		for _, de := range entries {
			name := de.Name()
			if len(name) > fatxNameSize {
				return fmt.Errorf("fatx: %q is longer than the %d characters a name may have", filepath.Join(path, name), fatxNameSize)
			}
			info, err := de.Info()
			if err != nil {
				return err
			}
			child := &fatxEntry{name: name, dir: de.IsDir(), modified: info.ModTime()}
			if de.IsDir() {
				if err := walk(filepath.Join(path, name), child); err != nil {
					return err
				}
			} else {
				data, err := os.ReadFile(filepath.Join(path, name))
				if err != nil {
					return err
				}
				child.data = data
			}
			e.children = append(e.children, child)
		}
		return nil
	}
	if err := walk(dir, root); err != nil {
		return nil, err
	}
	return root, nil
}

// EmptyTree is the tree of a volume holding nothing
func EmptyTree() *fatxEntry {
	return &fatxEntry{name: "", dir: true}
}
