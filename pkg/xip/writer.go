package xip

import (
	"encoding/binary"
	"fmt"
	"io"
	"sort"
	"strings"
)

// WriteEntry describes a file to include in a new XIP archive.
type WriteEntry struct {
	Name string   // path (forward slashes)
	Type FileType // FileTypeRegular or FileTypeMesh
	Size uint32
	Body io.Reader
}

// Writer creates a XIP archive. Call Add for every entry, then Flush.
type Writer struct {
	w       io.Writer
	entries []WriteEntry
}

func NewWriter(w io.Writer) *Writer {
	return &Writer{w: w}
}

// Add queues an entry to be written. Body is read during Flush.
func (w *Writer) Add(e WriteEntry) {
	w.entries = append(w.entries, e)
}

// Flush writes the complete XIP archive. Each entry's Body is consumed here.
func (w *Writer) Flush() error {
	// Sort entries by name for deterministic output
	sort.Slice(w.entries, func(i, j int) bool {
		return w.entries[i].Name < w.entries[j].Name
	})

	// Collect unique directory paths
	dirs := collectDirs(w.entries)

	// Total file records = dirs + files
	numFiles := uint16(len(dirs) + len(w.entries))
	numNames := uint16(len(dirs) + len(w.entries))

	// Build name block and fileName records
	type record struct {
		fd fileData
		fn fileName
	}
	var records []record
	var nameBlock []byte

	// Directory records first
	for _, d := range dirs {
		nameOff := uint16(len(nameBlock))
		nameBlock = appendString(nameBlock, d)
		records = append(records, record{
			fd: fileData{Type: uint32(FileTypeDir)},
			fn: fileName{DataIndex: uint16(len(records)), NameOffset: nameOff},
		})
	}

	// File records — offsets computed below
	fileStartIdx := len(records)
	for _, e := range w.entries {
		nameOff := uint16(len(nameBlock))
		nameBlock = appendString(nameBlock, e.Name)
		records = append(records, record{
			fd: fileData{Size: e.Size, Type: uint32(e.Type)},
			fn: fileName{DataIndex: uint16(len(records)), NameOffset: nameOff},
		})
	}

	// Compute data offset (header + fileData + fileName + nameBlock)
	headerSize := 16
	fileDataSize := int(numFiles) * 16
	fileNameSize := int(numNames) * 4
	dataOffset := uint32(headerSize + fileDataSize + fileNameSize + len(nameBlock))

	// Align data offset to 4 bytes
	if dataOffset%4 != 0 {
		pad := 4 - dataOffset%4
		nameBlock = append(nameBlock, make([]byte, pad)...)
		dataOffset += pad
	}

	// Compute per-file offsets and total data size
	var dataSize uint32
	for i, e := range w.entries {
		records[fileStartIdx+i].fd.Offset = dataSize
		dataSize += e.Size
	}

	// Write header
	h := Header{
		Magic:      magic,
		DataOffset: dataOffset,
		NumFiles:   numFiles,
		NumNames:   numNames,
		DataSize:   dataSize,
	}
	if err := binary.Write(w.w, binary.LittleEndian, &h); err != nil {
		return fmt.Errorf("xip: writing header: %w", err)
	}

	// Write fileData records
	for _, r := range records {
		if err := binary.Write(w.w, binary.LittleEndian, &r.fd); err != nil {
			return fmt.Errorf("xip: writing file data: %w", err)
		}
	}

	// Write fileName records
	for _, r := range records {
		if err := binary.Write(w.w, binary.LittleEndian, &r.fn); err != nil {
			return fmt.Errorf("xip: writing file name: %w", err)
		}
	}

	// Write name block
	if _, err := w.w.Write(nameBlock); err != nil {
		return fmt.Errorf("xip: writing name block: %w", err)
	}

	// Write file data bodies
	for _, e := range w.entries {
		n, err := io.Copy(w.w, e.Body)
		if err != nil {
			return fmt.Errorf("xip: writing %s: %w", e.Name, err)
		}
		if n != int64(e.Size) {
			return fmt.Errorf("xip: %s: expected %d bytes, got %d", e.Name, e.Size, n)
		}
	}

	return nil
}

// collectDirs returns sorted unique directory paths from entries.
func collectDirs(entries []WriteEntry) []string {
	seen := make(map[string]bool)
	for _, e := range entries {
		parts := strings.Split(e.Name, "/")
		for i := 1; i < len(parts); i++ {
			dir := strings.Join(parts[:i], "/")
			seen[dir] = true
		}
	}
	dirs := make([]string, 0, len(seen))
	for d := range seen {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)
	return dirs
}

// appendString appends a null-terminated string (with backslashes) to buf.
func appendString(buf []byte, s string) []byte {
	// Convert forward slashes to backslashes for XIP format
	s = strings.ReplaceAll(s, "/", "\\")
	buf = append(buf, []byte(s)...)
	buf = append(buf, 0)
	return buf
}
