//go:build samples

package native

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nickheyer/discoxip/pkg/bios"
)

// Every boot row rebuilds its reference byte for byte
func TestBootRows(t *testing.T) {
	root := filepath.Join("..", "..")
	work := t.TempDir()
	sets := []struct{ rom, flash, inittbl string }{
		{"mcpx10", "4034_orig.bin", "inittbl4034.xc"},
		{"mcpx11", "5713_orig.bin", "inittbl5713.xc"},
	}
	for _, s := range sets {
		mcpx := filepath.Join(root, "kernels", map[string]string{"mcpx10": "mcpx-1.0.bin", "mcpx11": "mcpx-1.1.bin"}[s.rom])
		r := Verify(Entry{Kind: KindROM, File: filepath.Join(root, "native", "boot", "mcpx", s.rom+".asm"), Reference: mcpx}, work)
		if !r.Exact {
			t.Errorf("%s: %d of %d bytes, %s", s.rom, r.Matched, r.Total, r.Error)
		}
		b, err := bios.Open(filepath.Join(root, "kernels", s.flash))
		if err != nil {
			t.Fatal(err)
		}
		key, err := bios.LoadMCPX(mcpx)
		if err != nil {
			t.Fatal(err)
		}
		if err := b.Decrypt(key); err != nil {
			t.Fatal(err)
		}
		ref := filepath.Join(work, s.flash+".inittbl")
		if err := os.WriteFile(ref, b.InitTable, 0o644); err != nil {
			t.Fatal(err)
		}
		r = Verify(Entry{Kind: KindInitTable, File: filepath.Join(root, "native", "boot", "inittbl", s.inittbl), Reference: ref}, work)
		if !r.Exact {
			t.Errorf("%s: %d of %d bytes, %s", s.inittbl, r.Matched, r.Total, r.Error)
		}
	}
}

// A library object compiled with the XDK compiler equals the member the XDK library holds
func TestLibraryObject(t *testing.T) {
	root := filepath.Join("..", "..")
	os.Setenv("WINEPREFIX", filepath.Join(root, "build", "wine"))
	r := Verify(Entry{
		Kind:      KindObject,
		File:      filepath.Join(root, "native", "private", "ntos", "rtl", "random.c"),
		Reference: filepath.Join(root, "references", "xdk", "5659", "XDK", "xbox", "lib", "xapilib.lib"),
		Member:    "obj\\i386\\random.obj",
		CL:        filepath.Join(root, "references", "xdk", "5659", "XDK", "xbox", "bin", "vc71", "CL.Exe"),
		Flags:     []string{"/Oxs", "/Gy", "/GF", "/Zl", "/Z7", "/I" + filepath.Join(root, "native", "private", "ntos", "rtl")},
	}, t.TempDir())
	if !r.Exact || r.Matched != 1 || r.Total != 1 {
		t.Fatalf("random.c: %+v", r)
	}
}

// A missing source and an unknown kind report errors rather than matches
func TestVerifyErrors(t *testing.T) {
	ref := filepath.Join(t.TempDir(), "ref.bin")
	os.WriteFile(ref, []byte{1, 2, 3}, 0o644)
	if r := Verify(Entry{Kind: KindROM, File: "nowhere.asm", Reference: ref}, t.TempDir()); r.Error != "no source" || r.Total != 3 {
		t.Errorf("missing source: %+v", r)
	}
	if r := Verify(Entry{Kind: "mystery", File: ref, Reference: ref}, t.TempDir()); r.Error == "" {
		t.Errorf("unknown kind verified: %+v", r)
	}
}
