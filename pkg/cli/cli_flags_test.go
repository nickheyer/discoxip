package cli

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func TestRequiredFlags(t *testing.T) {
	dir := t.TempDir()
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"build", []string{"build", dir}, `required flag(s) "bios", "mcpx" not set`},
		{"build with bios", []string{"build", dir, "--bios", "f"}, `required flag(s) "mcpx" not set`},
		{"build with mcpx", []string{"build", "--mcpx", "f", dir}, `required flag(s) "bios" not set`},
		{"web export", []string{"web", "export", dir}, `required flag(s) "bios", "mcpx" not set`},
		{"web export with mcpx", []string{"web", "export", "--mcpx", "f", dir}, `required flag(s) "bios" not set`},
		{"scene info", []string{"scene", "info", "x.xap"}, `required flag(s) "bios", "mcpx", "xbe" not set`},
		{"scene export", []string{"scene", "export", "x.xap"}, `required flag(s) "bios", "mcpx", "xbe" not set`},
		{"scene build", []string{"scene", "build", dir}, `required flag(s) "bios", "mcpx", "xbe" not set`},
		{"scene info with xbe and bios", []string{"scene", "info", "--xbe", "a", "--bios", "b", "x.xap"}, `required flag(s) "mcpx" not set`},
		{"xbe render", []string{"xbe", "render", "x.xbe"}, `required flag(s) "bios", "mcpx" not set`},
		{"xbe render with bios", []string{"xbe", "render", "--bios", "f", "x.xbe"}, `required flag(s) "mcpx" not set`},
		{"buffer dump", []string{"buffer", "dump", "x.vb"}, `required flag(s) "bios", "mcpx", "xbe" not set`},
		{"buffer dump with xbe", []string{"buffer", "dump", "--xbe", "a", "x.vb"}, `required flag(s) "bios", "mcpx" not set`},
		{"buffer export", []string{"buffer", "export", "x.vb", "x.ib"}, `required flag(s) "bios", "mcpx", "xbe" not set`},
		{"buffer export with xbe and mcpx", []string{"buffer", "export", "--xbe", "a", "--mcpx", "m", "x.vb", "x.ib"}, `required flag(s) "bios" not set`},
		{"bios boot", []string{"bios", "boot", "flash.bin"}, `required flag(s) "launch", "output" not set`},
		{"bios boot with output", []string{"bios", "boot", "-o", dir, "flash.bin"}, `required flag(s) "launch" not set`},
	}
	for _, tc := range tests {
		res := runCLI(t, tc.args...)
		if res.err == nil || res.err.Error() != tc.want {
			t.Errorf("%s: error %v, want %q", tc.name, res.err, tc.want)
		}
	}
}

func TestXBEInfo(t *testing.T) {
	base := uint32(0x00010000)
	data := xbeImage(base, base+0x1000, base+0x2000, []xbeSection{
		{name: ".text", va: base + 0x1000, vsize: 0x100, raw: 0x1000, rsize: 0x80, flags: 0x6},
		{name: ".data", va: base + 0x2000, vsize: 0x40, raw: 0x1080, rsize: 0x40, flags: 0x3},
	}, 0x1100)
	path := writeFile(t, filepath.Join(t.TempDir(), "default.xbe"), data)
	res := runCLI(t, "xbe", "info", path)
	if res.err != nil {
		t.Fatal(res.err)
	}
	pad := strings.Repeat(" ", 16)
	want := "XBE: " + path + "\n" +
		"  Base Address:  0x00010000\n" +
		"  Entry Point:   0x00011000\n" +
		"  Kernel Thunk:  0x00012000\n" +
		"  Sections:      2\n" +
		"    .text" + pad + "VA=0x00011000 VSize=0x000100 Raw=0x001000 RSize=0x000080\n" +
		"    .data" + pad + "VA=0x00012000 VSize=0x000040 Raw=0x001080 RSize=0x000040\n"
	if res.out != want {
		t.Fatalf("stdout\n%q\nwant\n%q", res.out, want)
	}
	bad := writeFile(t, filepath.Join(t.TempDir(), "bad.xbe"), []byte("XBEX and then nothing"))
	res = runCLI(t, "xbe", "info", bad)
	if res.err == nil || res.err.Error() != "xbe: invalid magic (expected XBEH)" {
		t.Errorf("bad image error = %v", res.err)
	}
}

func TestBIOSCommandsNameTheRevisionWithoutMCPX(t *testing.T) {
	for _, revision := range []int{0, 1} {
		path := writeFile(t, filepath.Join(t.TempDir(), fmt.Sprintf("flash%d.bin", revision)), flashBank(revision))
		want := fmt.Sprintf("bios: %s boots on MCPX 1.%d, pass that ROM dump with --mcpx", path, revision)
		for _, sub := range []string{"info", "extract", "lift", "machine"} {
			res := runCLI(t, "bios", sub, path)
			if res.err == nil || res.err.Error() != want {
				t.Errorf("bios %s on revision %d: error %v, want %q", sub, revision, res.err, want)
			}
		}
	}
}

func TestBIOSInfoRejectsOtherSizesAndROMs(t *testing.T) {
	short := writeFile(t, filepath.Join(t.TempDir(), "short.bin"), make([]byte, 100))
	res := runCLI(t, "bios", "info", short)
	if want := fmt.Sprintf("bios: %s is 100 bytes, a flash image is 256 KB, 512 KB or 1 MB", short); res.err == nil || res.err.Error() != want {
		t.Errorf("short flash error %v, want %q", res.err, want)
	}
	flash := writeFile(t, filepath.Join(t.TempDir(), "flash.bin"), flashBank(0))
	rom := writeFile(t, filepath.Join(t.TempDir(), "mcpx.bin"), []byte{1, 2, 3})
	res = runCLI(t, "bios", "info", "--mcpx", rom, flash)
	if want := fmt.Sprintf("bios: %s is 3 bytes, an MCPX ROM is 512", rom); res.err == nil || res.err.Error() != want {
		t.Errorf("short ROM error %v, want %q", res.err, want)
	}
}
