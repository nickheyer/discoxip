package texture

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
)

var xprMagic = [4]byte{'X', 'P', 'R', '0'}

// XPRHeader is the 16-byte XPR0 container header.
type XPRHeader struct {
	Magic      [4]byte
	TotalSize  uint32
	HeaderSize uint32
	ResCount   uint16
	ResType    uint16
}

// TextureInfo holds decoded texture metadata.
type TextureInfo struct {
	Width       int
	Height      int
	Format      PixelFormat
	MipLevels   int
	HeaderSize  int
	DataSize    int
	FormatReg   uint32 // raw NV2A format register
}

// Texture is a parsed XPR0 texture with metadata and raw data.
type Texture struct {
	Info TextureInfo
	Data []byte // raw pixel data (post-header)
}

// ReadXPR parses an XPR0 texture file from r with given size.
func ReadXPR(r io.Reader, size int64) (*Texture, error) {
	if size < 16 {
		return nil, ErrTruncatedFile
	}

	var h XPRHeader
	if err := binary.Read(r, binary.LittleEndian, &h); err != nil {
		return nil, fmt.Errorf("texture: reading header: %w", err)
	}

	if h.Magic != xprMagic {
		return nil, ErrInvalidMagic
	}

	if h.ResType != 4 {
		return nil, ErrNoTexture
	}

	// Read remaining header bytes to get to the format register
	// The format register is the last dword of the 16-byte resource descriptor
	// which starts right after the XPR header at byte 12
	// We already read 16 bytes (the XPR header itself contains the start of resource desc)
	// Actually: the XPR header is 12 bytes (magic+total+header), then 4 bytes res_count+type
	// Then there are per-resource entries. For a single texture, the format register
	// is at the end of the resource descriptor.

	// Read the rest of the header area
	remainingHeader := int(h.HeaderSize) - 16
	if remainingHeader < 0 {
		return nil, ErrTruncatedFile
	}

	headerData := make([]byte, remainingHeader)
	if _, err := io.ReadFull(r, headerData); err != nil {
		return nil, fmt.Errorf("texture: reading header data: %w", err)
	}

	// The format register is at the very end of the first resource descriptor
	// For single-resource XPR0, the resource descriptor follows the header
	// Based on analysis: the format register is at offset 12 from the resource start
	// Resource descriptor starts at byte 12 of the file (after magic+totalsize+headersize)
	// We already consumed bytes 12-15 as rescount+restype
	// The format register is the DWORD at file offset 0x1C (byte 28)
	// Which is headerData offset 12 (28 - 16 = 12)
	if len(headerData) < 12 {
		return nil, ErrTruncatedFile
	}

	// XPR0 layout:
	// 0x00: XPR0 magic
	// 0x04: total_size
	// 0x08: header_size
	// 0x0C: res_count(2) + res_type(2)
	// 0x10: GPU register 0 (unused)
	// 0x14: GPU register 1 (unused)
	// 0x18: NV2A format register
	// Format reg is at file offset 0x18 = headerData[8:12]
	formatReg := binary.LittleEndian.Uint32(headerData[8:12])

	info := decodeFormatRegister(formatReg)
	info.HeaderSize = int(h.HeaderSize)
	info.DataSize = int(h.TotalSize) - int(h.HeaderSize)

	// Read pixel data
	data := make([]byte, info.DataSize)
	if _, err := io.ReadFull(r, data); err != nil {
		return nil, fmt.Errorf("texture: reading pixel data: %w", err)
	}

	return &Texture{Info: info, Data: data}, nil
}

// OpenXPR opens an XPR0 texture file from disk.
func OpenXPR(path string) (*Texture, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}

	return ReadXPR(f, fi.Size())
}

// decodeFormatRegister extracts texture info from the NV2A format register.
func decodeFormatRegister(reg uint32) TextureInfo {
	// Bit layout (from analysis):
	// [7:0]   = context DMA + flags
	// [15:8]  = color format
	// [19:16] = mip levels
	// [23:20] = log2(width)
	// [27:24] = log2(height)
	colorFmt := PixelFormat((reg >> 8) & 0xFF)
	mipLevels := int((reg >> 16) & 0xF)
	log2W := int((reg >> 20) & 0xF)
	log2H := int((reg >> 24) & 0xF)

	return TextureInfo{
		Width:     1 << log2W,
		Height:    1 << log2H,
		Format:    colorFmt,
		MipLevels: mipLevels,
		FormatReg: reg,
	}
}
