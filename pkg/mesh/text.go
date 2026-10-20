package mesh

import (
	"regexp"
)

// TextMesh holds parsed VRML-like text content from an XM file.
type TextMesh struct {
	Source    string   // raw text content
	NodeNames []string // DEF-ed node names
	MeshRefs  []string // referenced mesh URLs
}

var (
	defPattern = regexp.MustCompile(`DEF\s+(\S+)`)
	urlPattern = regexp.MustCompile(`url\s+"([^"]+)"`)
)

// ParseText extracts structure from VRML text data.
func ParseText(data []byte) *TextMesh {
	src := string(data)
	tm := &TextMesh{Source: src}

	// Extract DEF names
	for _, m := range defPattern.FindAllStringSubmatch(src, -1) {
		tm.NodeNames = append(tm.NodeNames, m[1])
	}

	// Extract url references
	for _, m := range urlPattern.FindAllStringSubmatch(src, -1) {
		tm.MeshRefs = append(tm.MeshRefs, m[1])
	}

	return tm
}
