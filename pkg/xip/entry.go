package xip

import "fmt"

type FileType uint32

const (
	FileTypeRegular FileType = 0 // regular file (XAP, etc.)
	FileTypeFile    FileType = 1 // regular file (alternate)
	FileTypeDir     FileType = 2 // texture / resource container (XBX textures use this type)
	FileTypeMesh    FileType = 4 // mesh sub-range reference
	FileTypeIB      FileType = 5 // index buffer pool
	FileTypeVB      FileType = 6 // vertex buffer pool
)

func (t FileType) String() string {
	switch t {
	case FileTypeRegular:
		return "file"
	case FileTypeFile:
		return "file"
	case FileTypeDir:
		return "resource"
	case FileTypeMesh:
		return "mesh"
	case FileTypeIB:
		return "ib"
	case FileTypeVB:
		return "vb"
	default:
		return fmt.Sprintf("unknown(%d)", uint32(t))
	}
}

type Entry struct {
	Name      string
	Offset    uint32 // raw offset (for mesh entries: upper byte = pool index, lower 24 bits = index start)
	Size      uint32 // byte size (for mesh entries: triangle count)
	Type      FileType
	Timestamp uint32
}

// MeshMeta holds decoded mesh sub-range metadata from a mesh-type XIP entry.
// For mesh entries, the Offset and Size fields encode pool and index range
// information rather than byte offsets into the data section.
type MeshMeta struct {
	Pool       int // buffer pool index (maps to ~0, ~1, ~2, etc.)
	IndexStart int // starting index offset in the pool's IB (in indices, not bytes)
	TriCount   int // triangle count (index count = TriCount * 3)
}

// DecodeMeshEntry extracts pool/index encoding from a mesh-type entry.
func DecodeMeshEntry(e Entry) MeshMeta {
	return MeshMeta{
		Pool:       int(e.Offset >> 24),
		IndexStart: int(e.Offset & 0x00FFFFFF),
		TriCount:   int(e.Size),
	}
}
