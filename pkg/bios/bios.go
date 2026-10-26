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
	bankSize         = 0x40000
	mcpxSize         = 0x200
	bldrSize         = 0x6000
	bldrBase         = 0x00090000
	preldrBlockSize  = 0x2A00
	preldrParamsSize = 0x80
	preldrNonceSize  = 0x10
	keySize          = 16
	bootParamsSize   = 36
	bootSignature    = 0x7854794A
)

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

// Decrypt unlocks the 2BL with the MCPX key, reads its parameters and keys, and extracts the kernel
func (b *Bank) Decrypt(mcpx *MCPX) error {
	if mcpx.Revision != b.MCPXRevision() {
		return fmt.Errorf("bios: the bank boots on MCPX 1.%d and the ROM given is MCPX 1.%d", b.MCPXRevision(), mcpx.Revision)
	}
	bldr := b.Bldr
	preldr := bldr[bldrSize-preldrBlockSize:]
	paramsAt := bldrSize - bootParamsSize
	if b.Preldr {
		// The preldr's key is the SHA1 of the secret, the preldr's nonce and the secret xored with 0x5C
		h := sha1.New()
		h.Write(mcpx.Key)
		h.Write(preldr[preldrBlockSize-preldrNonceSize:])
		xored := make([]byte, keySize)
		for i := range xored {
			xored[i] = mcpx.Key[i] ^ 0x5C
		}
		h.Write(xored)
		c, err := rc4.NewCipher(h.Sum(nil))
		if err != nil {
			return err
		}
		// The 2BL is decrypted around the preldr, whose block stays as the flash holds it
		c.XORKeyStream(bldr[:bldrSize-preldrBlockSize], bldr[:bldrSize-preldrBlockSize])
		skip := make([]byte, preldrBlockSize-preldrParamsSize)
		c.XORKeyStream(skip, skip)
		tail := bldr[bldrSize-preldrParamsSize:]
		c.XORKeyStream(tail, tail)
		// The first 16 bytes were zeroed at build, the entry coming back from the preldr's entry record
		entryOff := binary.LittleEndian.Uint32(bldr[bldrSize-preldrBlockSize-8:])
		if entryOff > bldrSize {
			return fmt.Errorf("bios: the preldr names a 2BL entry at %#x past the 2BL", entryOff)
		}
		for i := 0; i < 16; i++ {
			bldr[i] = 0
		}
		binary.LittleEndian.PutUint32(bldr, bldrBase+entryOff)
		// The nonce took the last 16 bytes, so the boot params sit 16 bytes earlier
		paramsAt -= preldrNonceSize
	} else {
		c, err := rc4.NewCipher(mcpx.Key)
		if err != nil {
			return err
		}
		c.XORKeyStream(bldr, bldr)
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
	b.Entry = binary.LittleEndian.Uint32(bldr)
	entryOff := int(b.Entry & 0xFFFF)
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
