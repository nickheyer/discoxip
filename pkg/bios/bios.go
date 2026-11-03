package bios

import (
	"bytes"
	"crypto/rc4"
	"crypto/sha1"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"os"
)

// Sizes and places of the parts of one 256 KB flash bank
const (
	bankSize        = 0x40000
	mcpxSize        = 0x200
	bldrSize        = 0x6000
	bldrBase        = 0x00090000
	preldrBlockSize = 0x2A00
	preldrBase      = 0x100000000 - mcpxSize - preldrBlockSize
	preldrNonceSize = 0x10
	keySize         = 16
	bootParamsSize  = 36
	bootSignature   = 0x7854794A
	signatureSize   = 0x100
	signatureAt     = 0x5E80
	overlayAt       = 0x5800
	overlayHeader10 = 12
	overlayHeader11 = 20
	bldrRecordAt    = bldrSize - preldrBlockSize - 8
)

// Overlay is the plaintext the build tool wrote over the encrypted 2BL, the signature and its header among it
type Overlay struct {
	Start, End int
	Nonce      bool
}

// The overlay each MCPX revision's flash carries, the second loader's block for the 1.1 scheme
func OverlayFor(rev int) Overlay {
	if rev == 0 {
		return Overlay{Start: overlayAt, End: signatureAt + signatureSize + overlayHeader10}
	}
	return Overlay{Start: bldrSize - preldrBlockSize, End: signatureAt + signatureSize + overlayHeader11, Nonce: true}
}

// BootParams are the sizes at the end of the 2BL that place the kernel and its data
type BootParams struct {
	KernelDataSize uint32
	InitTableSize  uint32
	Signature      uint32
	KernelSize     uint32
	Digest         [20]byte
}

// Keys are the RC4 keys the 2BL carries, eeprom and certificate for the kernel and the kernel's own
type Keys struct {
	EEPROM [keySize]byte
	Cert   [keySize]byte
	Kernel [keySize]byte
}

// MCPX is the 512 byte secret ROM, which holds the key the 2BL is encrypted with
type MCPX struct {
	Data     []byte
	Revision int
	Key      []byte
}

// SHA1 digests of the MCPX ROM revisions and where each keeps its 2BL key
var mcpxRevisions = []struct {
	sha1 string
	rev  int
	key  int
}{
	{"5d270675b54eb8071b480e42d22a3015ac211cef", 0, 0x1A5},
	{"6c875f17f773aaec51eb434068bb6c657c4343c0", 1, 0x19C},
}

// LoadMCPX reads a 512 byte MCPX ROM dump and identifies its revision by digest
func LoadMCPX(path string) (*MCPX, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(data) != mcpxSize {
		return nil, fmt.Errorf("bios: %s is %d bytes, an MCPX ROM is %d", path, len(data), mcpxSize)
	}
	digest := sha1.Sum(data)
	sum := hex.EncodeToString(digest[:])
	for _, r := range mcpxRevisions {
		if r.sha1 == sum {
			return &MCPX{Data: data, Revision: r.rev, Key: data[r.key : r.key+keySize]}, nil
		}
	}
	return nil, fmt.Errorf("bios: %s has digest %s, which is no known MCPX ROM", path, sum)
}

// Bank is one 256 KB flash image, its bootloader decrypted and its kernel extracted once a key is given
type Bank struct {
	Data       []byte
	Bldr       []byte
	Preldr     bool
	Entry      uint32
	Params     BootParams
	Keys       Keys
	InitTable  []byte
	KernelData []byte
	Compressed []byte
	Kernel     []byte
}

// Open reads a flash image of one, two or four identical banks and locates the parts of the first
func Open(path string) (*Bank, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(data) != bankSize && len(data) != 2*bankSize && len(data) != 4*bankSize {
		return nil, fmt.Errorf("bios: %s is %d bytes, a flash image is 256 KB, 512 KB or 1 MB", path, len(data))
	}
	for off := bankSize; off < len(data); off += bankSize {
		if !bytes.Equal(data[:bankSize], data[off:off+bankSize]) {
			return nil, fmt.Errorf("bios: %s holds a bank at %#x that differs from its first", path, off)
		}
	}
	return OpenBank(data[:bankSize])
}

// OpenBank locates the 2BL of one bank and reports whether a preldr sits inside it
func OpenBank(data []byte) (*Bank, error) {
	if len(data) != bankSize {
		return nil, fmt.Errorf("bios: a bank is %d bytes, got %d", bankSize, len(data))
	}
	b := &Bank{Data: data, Bldr: make([]byte, bldrSize)}
	copy(b.Bldr, data[bankSize-bldrSize-mcpxSize:bankSize-mcpxSize])
	b.Preldr = b.Bldr[bldrSize-preldrBlockSize] == 0xE9
	return b, nil
}

// The MCPX revision whose ROM boots the bank, the preldr marking the second
func (b *Bank) MCPXRevision() int {
	if b.Preldr {
		return 1
	}
	return 0
}

// PreldrKey derives the RC4 key of a 1.1 bank, the SHA1 of the ROM secret, the nonce and the secret xored with 0x5C
func PreldrKey(secret, nonce []byte) []byte {
	h := sha1.New()
	h.Write(secret)
	h.Write(nonce)
	xored := make([]byte, len(secret))
	for i := range xored {
		xored[i] = secret[i] ^ 0x5C
	}
	h.Write(xored)
	return h.Sum(nil)
}

// Decrypt unlocks the 2BL with the MCPX key, reads its parameters and keys, and extracts the kernel
func (b *Bank) Decrypt(mcpx *MCPX) error {
	if mcpx.Revision != b.MCPXRevision() {
		return fmt.Errorf("bios: the bank boots on MCPX 1.%d and the ROM given is MCPX 1.%d", b.MCPXRevision(), mcpx.Revision)
	}
	bldr := b.Bldr
	raw := make([]byte, bldrSize)
	copy(raw, bldr)
	paramsAt := bldrSize - bootParamsSize
	var key []byte
	if b.Preldr {
		key = PreldrKey(mcpx.Key, raw[bldrSize-preldrNonceSize:])
		paramsAt -= preldrNonceSize
	} else {
		key = mcpx.Key
	}
	c, err := rc4.NewCipher(key)
	if err != nil {
		return err
	}
	// The whole 2BL was one RC4 stream, the overlay and nonce written over the ciphertext afterwards
	c.XORKeyStream(bldr, bldr)
	ov := OverlayFor(b.MCPXRevision())
	copy(bldr[ov.Start:ov.End], raw[ov.Start:ov.End])
	if ov.Nonce {
		copy(bldr[bldrSize-preldrNonceSize:], raw[bldrSize-preldrNonceSize:])
	}
	p := bldr[paramsAt:]
	b.Params = BootParams{
		KernelDataSize: binary.LittleEndian.Uint32(p[0:]),
		InitTableSize:  binary.LittleEndian.Uint32(p[4:]),
		Signature:      binary.LittleEndian.Uint32(p[8:]),
		KernelSize:     binary.LittleEndian.Uint32(p[12:]),
	}
	copy(b.Params.Digest[:], p[16:36])
	if b.Params.Signature != bootSignature {
		return fmt.Errorf("bios: the decrypted 2BL carries signature %08X, not %08X, so the MCPX key does not fit this bank", b.Params.Signature, uint32(bootSignature))
	}
	if b.Params.KernelSize > bankSize || b.Params.KernelDataSize > bankSize || b.Params.InitTableSize > bankSize {
		return fmt.Errorf("bios: the 2BL names a %d byte kernel, %d bytes of kernel data and a %d byte init table in a %d byte bank", b.Params.KernelSize, b.Params.KernelDataSize, b.Params.InitTableSize, bankSize)
	}
	if b.Preldr {
		// The 1.1 scheme keeps the entry offset in a record at the end of the 2BL rather than a header pointer
		b.Entry = bldrBase + binary.LittleEndian.Uint32(bldr[bldrRecordAt:])
	} else {
		b.Entry = binary.LittleEndian.Uint32(bldr)
	}
	entryOff := int(b.Entry - bldrBase)
	if entryOff < 8 || entryOff > bldrSize {
		return fmt.Errorf("bios: the 2BL entry %08X lies outside the 2BL", b.Entry)
	}
	keysPtr := binary.LittleEndian.Uint32(bldr[entryOff-8:])
	keysOff := int(keysPtr & 0xFFFF)
	if keysOff+3*keySize > bldrSize {
		return fmt.Errorf("bios: the 2BL keys at %08X lie outside the 2BL", keysPtr)
	}
	copy(b.Keys.EEPROM[:], bldr[keysOff:])
	copy(b.Keys.Cert[:], bldr[keysOff+keySize:])
	copy(b.Keys.Kernel[:], bldr[keysOff+2*keySize:])
	bldrAt := bankSize - bldrSize - mcpxSize
	dataAt := bldrAt - int(b.Params.KernelDataSize)
	kernelAt := dataAt - int(b.Params.KernelSize)
	if kernelAt < int(b.Params.InitTableSize) {
		return fmt.Errorf("bios: the kernel at %#x overlaps the %d byte init table", kernelAt, b.Params.InitTableSize)
	}
	b.InitTable = b.Data[:b.Params.InitTableSize]
	b.KernelData = b.Data[dataAt:bldrAt]
	b.Compressed = make([]byte, b.Params.KernelSize)
	copy(b.Compressed, b.Data[kernelAt:dataAt])
	// A kernel key of all zero or all one bits marks a kernel stored in the clear
	zero, ones := true, true
	for _, k := range b.Keys.Kernel {
		zero = zero && k == 0
		ones = ones && k == 0xFF
	}
	if !zero && !ones {
		c, err := rc4.NewCipher(b.Keys.Kernel[:])
		if err != nil {
			return err
		}
		c.XORKeyStream(b.Compressed, b.Compressed)
	}
	kernel, err := lzxDecompress(b.Compressed)
	if err != nil {
		return fmt.Errorf("bios: decompressing the kernel: %w", err)
	}
	if len(kernel) < 0x40 || kernel[0] != 'M' || kernel[1] != 'Z' {
		return fmt.Errorf("bios: the decompressed kernel does not start with an MZ header")
	}
	b.Kernel = kernel
	return nil
}

// BldrFlash is where the 2BL sits in the address space, just under the MCPX ROM
const BldrFlash = 0x100000000 - mcpxSize - bldrSize

// Layout places the plaintext fields of a 2BL image for one MCPX revision
type Layout struct {
	Size        int
	ParamsAt    int
	RecordAt    int
	SignatureAt int
	HeaderAt    int
	NonceAt     int
	Overlay     Overlay
	PreldrAt    int
	PreldrSize  int
}

// LayoutFor describes the 2BL image of a bank booted by the given MCPX revision
func LayoutFor(rev int) Layout {
	l := Layout{Size: bldrSize, ParamsAt: bldrSize - bootParamsSize, SignatureAt: signatureAt, HeaderAt: signatureAt + signatureSize, Overlay: OverlayFor(rev)}
	if rev == 1 {
		l.ParamsAt -= preldrNonceSize
		l.RecordAt = bldrRecordAt
		l.NonceAt = bldrSize - preldrNonceSize
		l.PreldrAt = bldrSize - preldrBlockSize
		l.PreldrSize = signatureAt - l.PreldrAt
	}
	return l
}

// KernelRaw returns the compressed kernel as the flash stores it, encrypted when the bank carries a key
func (b *Bank) KernelRaw() []byte {
	bldrAt := bankSize - bldrSize - mcpxSize
	dataAt := bldrAt - int(b.Params.KernelDataSize)
	return b.Data[dataAt-int(b.Params.KernelSize) : dataAt]
}

// BootDigest computes the digest the 2BL checks the kernel against, a keyed double SHA1 over every flash part for 1.0 and a plain SHA1 of kernel and data for 1.1
func (b *Bank) BootDigest() [20]byte {
	var out [20]byte
	var size [4]byte
	h := sha1.New()
	if b.MCPXRevision() == 0 {
		h.Write(b.Keys.Kernel[:])
		binary.LittleEndian.PutUint32(size[:], b.Params.KernelSize)
		h.Write(size[:])
		h.Write(b.KernelRaw())
		binary.LittleEndian.PutUint32(size[:], b.Params.KernelDataSize)
		h.Write(size[:])
		h.Write(b.KernelData)
		binary.LittleEndian.PutUint32(size[:], b.Params.InitTableSize)
		h.Write(size[:])
		h.Write(b.InitTable)
		inner := h.Sum(nil)
		h = sha1.New()
		h.Write(b.Keys.Kernel[:])
		h.Write(inner)
	} else {
		h.Write(b.KernelRaw())
		h.Write(b.KernelData)
	}
	copy(out[:], h.Sum(nil))
	return out
}

// Compose lays a rebuilt 2BL, and for 1.1 banks its preldr, into the plaintext image the flash encrypts, writing the fields the build tool fills in
func (b *Bank) Compose(bldr, preldr, signature, nonce, header []byte, entry, reenter uint32) ([]byte, error) {
	rev := b.MCPXRevision()
	l := LayoutFor(rev)
	img := make([]byte, l.Size)
	if len(bldr) > l.Overlay.Start {
		return nil, fmt.Errorf("bios: the 2BL is %d bytes and runs into the overlay at %#x", len(bldr), l.Overlay.Start)
	}
	copy(img, bldr)
	if len(signature) != signatureSize {
		return nil, fmt.Errorf("bios: the signature is %d bytes, not %d", len(signature), signatureSize)
	}
	copy(img[l.SignatureAt:], signature)
	hdr := img[l.HeaderAt:]
	binary.LittleEndian.PutUint32(hdr, 0xFFF00000)
	binary.LittleEndian.PutUint32(hdr[4:], b.Params.InitTableSize)
	if rev == 0 {
		binary.LittleEndian.PutUint32(img, bldrBase+entry)
		binary.LittleEndian.PutUint32(hdr[8:], BldrFlash-b.Params.KernelDataSize-b.Params.KernelSize)
	} else {
		if len(header) != 16 {
			return nil, fmt.Errorf("bios: the 2BL header is %d bytes, not 16", len(header))
		}
		copy(img, header)
		if len(preldr) > l.PreldrSize {
			return nil, fmt.Errorf("bios: the preldr is %d bytes, more than the %d its block holds", len(preldr), l.PreldrSize)
		}
		copy(img[l.PreldrAt:], preldr)
		binary.LittleEndian.PutUint32(img[l.RecordAt:], entry)
		binary.LittleEndian.PutUint32(img[l.RecordAt+4:], reenter)
		binary.LittleEndian.PutUint32(hdr[8:], BldrFlash)
		binary.LittleEndian.PutUint32(hdr[12:], BldrFlash)
		binary.LittleEndian.PutUint32(hdr[16:], 0x00400000)
		if len(nonce) != preldrNonceSize {
			return nil, fmt.Errorf("bios: the nonce is %d bytes, not %d", len(nonce), preldrNonceSize)
		}
		copy(img[l.NonceAt:], nonce)
	}
	p := img[l.ParamsAt:]
	binary.LittleEndian.PutUint32(p, b.Params.KernelDataSize)
	binary.LittleEndian.PutUint32(p[4:], b.Params.InitTableSize)
	binary.LittleEndian.PutUint32(p[8:], bootSignature)
	binary.LittleEndian.PutUint32(p[12:], b.Params.KernelSize)
	digest := b.BootDigest()
	copy(p[16:], digest[:])
	return img, nil
}
