//go:build samples

package bios

import (
	"bytes"
	"encoding/binary"
	"path/filepath"
	"testing"
)

// The digest and the composed plaintext image of each reference bank come back exactly as the flash holds them
func TestComposeRoundTrip(t *testing.T) {
	root := filepath.Join("..", "..")
	for _, s := range []struct{ flash, mcpx string }{{"4034_orig.bin", "mcpx-1.0.bin"}, {"5713_orig.bin", "mcpx-1.1.bin"}} {
		b, err := Open(filepath.Join(root, "kernels", s.flash))
		if err != nil {
			t.Fatal(err)
		}
		key, err := LoadMCPX(filepath.Join(root, "kernels", s.mcpx))
		if err != nil {
			t.Fatal(err)
		}
		if err := b.Decrypt(key); err != nil {
			t.Fatal(err)
		}
		if d := b.BootDigest(); d != b.Params.Digest {
			t.Errorf("%s: digest %x, params hold %x", s.flash, d, b.Params.Digest)
		}
		l := LayoutFor(b.MCPXRevision())
		ref := b.Bldr
		var preldr, nonce, header []byte
		entry := b.Entry - bldrBase
		var reenter uint32
		bldr := ref[:l.Overlay.Start]
		if b.Preldr {
			preldr = ref[l.PreldrAt : l.PreldrAt+l.PreldrSize]
			nonce = ref[l.NonceAt:]
			header = ref[:16]
			bldr = ref[16:l.RecordAt]
			reenter = binary.LittleEndian.Uint32(ref[l.RecordAt+4:])
			bldr = append(append([]byte{}, header...), bldr...)
		} else {
			bldr = ref[4:l.Overlay.Start]
			bldr = append(make([]byte, 4), bldr...)
		}
		img, err := b.Compose(bldr, preldr, ref[l.SignatureAt:l.SignatureAt+signatureSize], nonce, header, entry, reenter)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(img, ref) {
			n := 0
			for i := range img {
				if img[i] != ref[i] {
					if n < 8 {
						t.Logf("%s: byte %#x is %02x, composed %02x", s.flash, i, ref[i], img[i])
					}
					n++
				}
			}
			t.Errorf("%s: %d bytes differ", s.flash, n)
		}
	}
}
