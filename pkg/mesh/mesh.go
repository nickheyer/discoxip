package mesh

import (
	"io"
	"os"
)

// ContentType classifies what an XM file contains.
type ContentType int

const (
	ContentEmpty  ContentType = iota // all zeros or zero-length
	ContentText                      // ASCII VRML scene text
	ContentBinary                    // binary packed records
)

func (c ContentType) String() string {
	switch c {
	case ContentEmpty:
		return "empty"
	case ContentText:
		return "text (VRML)"
	case ContentBinary:
		return "binary"
	default:
		return "unknown"
	}
}

// Mesh is the parsed result of an XM file.
type Mesh struct {
	Type     ContentType
	Size     int64
	Text     *TextMesh
	Binary   *BinaryMesh
}

// Detect determines the content type from the first bytes of data.
func Detect(data []byte, size int64) ContentType {
	if size == 0 {
		return ContentEmpty
	}

	// Check if all zeros
	allZero := true
	for _, b := range data {
		if b != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		return ContentEmpty
	}

	// Check if printable ASCII (VRML text)
	printable := 0
	for _, b := range data {
		if (b >= 0x20 && b <= 0x7E) || b == '\r' || b == '\n' || b == '\t' {
			printable++
		}
	}
	// If >90% printable, treat as text
	if len(data) > 0 && float64(printable)/float64(len(data)) > 0.9 {
		return ContentText
	}

	return ContentBinary
}

// Read parses an XM file from r with the given size.
func Read(r io.Reader, size int64) (*Mesh, error) {
	if size == 0 {
		return &Mesh{Type: ContentEmpty, Size: 0}, nil
	}

	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}

	ct := Detect(data, size)
	m := &Mesh{Type: ct, Size: size}

	switch ct {
	case ContentText:
		m.Text = ParseText(data)
	case ContentBinary:
		m.Binary = ParseBinary(data)
	}

	return m, nil
}

// Open reads an XM file from disk.
func Open(path string) (*Mesh, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}

	return Read(f, fi.Size())
}
