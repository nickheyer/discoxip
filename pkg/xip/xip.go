package xip

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"strings"
)

var magic = [4]byte{'X', 'I', 'P', '0'}

// On-disk XIP header (16 bytes, little-endian)
type Header struct {
	Magic      [4]byte
	DataOffset uint32
	NumFiles   uint16
	NumNames   uint16
	DataSize   uint32
}

// On-disk per-file record (16 bytes)
type fileData struct {
	Offset    uint32
	Size      uint32
	Type      uint32
	Timestamp uint32
}

// On-disk name-index record (4 bytes)
type fileName struct {
	DataIndex  uint16
	NameOffset uint16
}

// Random access to entries in a XIP
type Reader struct {
	ra      io.ReaderAt
	closer  io.Closer // non-nil only when created via Open
	header  Header
	entries []Entry
}

// Opens a XIP archive
func Open(path string) (*Reader, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	fi, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	r, err := NewReader(f, fi.Size())
	if err != nil {
		f.Close()
		return nil, err
	}
	r.closer = f
	return r, nil
}

func NewReader(ra io.ReaderAt, size int64) (*Reader, error) {
	const headerSize = 16

	if size < headerSize {
		return nil, ErrTruncatedFile
	}

	// Read header
	var h Header
	if err := binary.Read(io.NewSectionReader(ra, 0, headerSize), binary.LittleEndian, &h); err != nil {
		return nil, fmt.Errorf("xip: reading header: %w", err)
	}
	if h.Magic != magic {
		return nil, ErrInvalidMagic
	}

	// Section sizes
	fileDataSize := int64(h.NumFiles) * 16
	fileNameSize := int64(h.NumNames) * 4
	metaEnd := headerSize + fileDataSize + fileNameSize
	nameBlockSize := int64(h.DataOffset) - metaEnd
	if nameBlockSize < 0 || int64(h.DataOffset)+int64(h.DataSize) > size {
		return nil, ErrTruncatedFile
	}

	// File data records
	fds := make([]fileData, h.NumFiles)
	if err := binary.Read(
		io.NewSectionReader(ra, headerSize, fileDataSize),
		binary.LittleEndian, fds,
	); err != nil {
		return nil, fmt.Errorf("xip: reading file data: %w", err)
	}

	// File name records
	fns := make([]fileName, h.NumNames)
	if err := binary.Read(
		io.NewSectionReader(ra, headerSize+fileDataSize, fileNameSize),
		binary.LittleEndian, fns,
	); err != nil {
		return nil, fmt.Errorf("xip: reading file names: %w", err)
	}

	// Name string block
	nameBlock := make([]byte, nameBlockSize)
	if _, err := ra.ReadAt(nameBlock, metaEnd); err != nil {
		return nil, fmt.Errorf("xip: reading name block: %w", err)
	}

	// Resolve data and name from file name record
	entries := make([]Entry, len(fns))
	for i, fn := range fns {
		if int(fn.DataIndex) >= len(fds) {
			return nil, ErrInvalidIndex
		}
		if int(fn.NameOffset) >= len(nameBlock) {
			return nil, ErrInvalidOffset
		}

		// Pick null term from name block
		name := extractString(nameBlock, int(fn.NameOffset))
		fd := fds[fn.DataIndex]

		entries[i] = Entry{
			Name:      name,
			Offset:    fd.Offset,
			Size:      fd.Size,
			Type:      FileType(fd.Type),
			Timestamp: fd.Timestamp,
		}
	}

	return &Reader{
		ra:      ra,
		header:  h,
		entries: entries,
	}, nil
}

// Parsed archive header
func (r *Reader) Header() Header {
	return r.header
}

// Resolved list of entries
func (r *Reader) Entries() []Entry {
	return r.entries
}

func (r *Reader) OpenFile(e Entry) io.Reader {
	return io.NewSectionReader(r.ra, int64(r.header.DataOffset)+int64(e.Offset), int64(e.Size))
}

func (r *Reader) Close() error {
	if r.closer != nil {
		return r.closer.Close()
	}
	return nil
}

// Get null terminate str from buffer starting at offset
func extractString(buf []byte, off int) string {
	end := off
	for end < len(buf) && buf[end] != 0 {
		end++
	}
	s := string(buf[off:end])
	// lol microsoft slashes
	return strings.ReplaceAll(s, "\\", "/")
}
