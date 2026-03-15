package mesh

import (
	"bytes"
	"io"
	"os"
)

// ContentType classifies what an XM file contains.
type ContentType int

const (
	ContentEmpty  ContentType = iota // all zeros or zero-length
	ContentText                      // ASCII VRML scene text
	ContentBinary                    // binary vertex color data or other binary
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
	Type   ContentType
	Size   int64
	Text   *TextMesh
	Binary *BinaryMesh
}

// vrmlKeywords are tokens that reliably identify VRML text content.
// RGBA vertex color data often has >90% printable bytes (because the 0xFF
// alpha byte and color values 0x20-0x7E are "printable"), so we must check
// for actual VRML structure rather than relying on byte-range heuristics.
var vrmlKeywords = [][]byte{
	[]byte("Shape"),
	[]byte("Transform"),
	[]byte("DEF "),
	[]byte("material"),
	[]byte("appearance"),
	[]byte("geometry"),
	[]byte("children"),
	[]byte("url "),
}

// Detect determines the content type from the data.
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

	// Check for VRML keywords — this is more reliable than byte-range analysis
	// because RGBA color data often passes the "90% printable" test.
	for _, kw := range vrmlKeywords {
		if bytes.Contains(data, kw) {
			return ContentText
		}
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
