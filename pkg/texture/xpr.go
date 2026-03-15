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

	// XPR0 file layout (single-texture resource):
	//   0x00: "XPR0" magic          (4 bytes)
	//   0x04: total file size       (4 bytes)
	//   0x08: header size           (4 bytes) — offset to pixel data
	//   0x0C: D3DResource.Common    (4 bytes) — refcount(16) + type(16)
	//   0x10: D3DTexture.Data       (4 bytes) — GPU data offset
	//   0x14: D3DTexture.Lock       (4 bytes) — unused at rest
	//   0x18: D3DTexture.Format     (4 bytes) — NV2A format register
	//   0x1C: D3DTexture.Size       (4 bytes) — NV2A size register
	//   [header_size]: pixel data starts
	//
	// We already read 16 bytes (XPR header + Common field as ResCount/ResType).
	// The format register is at file offset 0x18 = headerData[8:12].

	remainingHeader := int(h.HeaderSize) - 16
	if remainingHeader < 0 {
		return nil, ErrTruncatedFile
	}

	headerData := make([]byte, remainingHeader)
	if _, err := io.ReadFull(r, headerData); err != nil {
		return nil, fmt.Errorf("texture: reading header data: %w", err)
	}

	if len(headerData) < 12 {
		return nil, ErrTruncatedFile
	}

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

// decodeFormatRegister extracts texture info from the NV2A SET_TEXTURE_FORMAT register.
func decodeFormatRegister(reg uint32) TextureInfo {
	// NV2A SET_TEXTURE_FORMAT register bit layout:
	// [1:0]   = context DMA select
	// [2]     = cubemap enable
	// [3]     = border source
	// [7:4]   = dimensionality
	// [15:8]  = color format (D3DFMT_*)
	// [19:16] = mipmap levels
	// [23:20] = base size U (log2 width)
	// [27:24] = base size V (log2 height)
	// [31:28] = base size P (log2 depth, for 3D textures)
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
