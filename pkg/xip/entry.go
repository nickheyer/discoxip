package xip

import "fmt"

type FileType uint32

const (
	FileTypeRegular FileType = 1
	FileTypeDir     FileType = 2
	FileTypeMesh    FileType = 4
)

func (t FileType) String() string {
	switch t {
	case FileTypeRegular:
		return "file"
	case FileTypeDir:
		return "dir"
	case FileTypeMesh:
		return "mesh"
	default:
		return fmt.Sprintf("unknown(%d)", uint32(t))
	}
}

type Entry struct {
	Name      string
	Offset    uint32 // raw boffset
	Size      uint32
	Type      FileType
	Timestamp uint32
}
