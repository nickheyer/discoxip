package xbe

import (
	"strings"
	"testing"
)

func TestLoadXTLID(t *testing.T) {
	names, err := loadXTLID()
	if err != nil {
		t.Fatal(err)
	}
	if len(names) < 1000 {
		t.Fatalf("only %d identities", len(names))
	}
	for id, want := range map[uint32]string{0x90001: "D3DXBoxBoundProbe", 0x90002: "D3DXCheckCubeTextureRequirements", 0x10032: "XGetDevices"} {
		if names[id] != want {
			t.Errorf("identity %X is %q, want %q", id, names[id], want)
		}
	}
	if name, ok := names[0]; ok {
		t.Errorf("identity 0 named %q", name)
	}
}

func xtlidImage(t *testing.T, table []byte) (*Image, map[string]uint32) {
	t.Helper()
	code := newAsm(0x11000)
	code.label("entry")
	code.rel32([]byte{0xE8}, "probe")
	code.rel32([]byte{0xE8}, "other")
	code.emit(0xC3)
	code.align(16, 0xCC)
	code.label("probe")
	code.emit(0xB8, 0x01, 0x00, 0x09, 0x00, 0xC3)
	code.align(16, 0xCC)
	code.label("other")
	code.emit(0xB8, 0x02, 0x00, 0x09, 0x00, 0xC3)
	code.align(16, 0xCC)
	img := parseXBE(t, xbeSpec{
		entry:  0x11000,
		kthunk: 0x12000,
		sections: []testSection{
			{name: ".text", va: 0x11000, flags: flagExecutable, data: code.bytes(), vsize: 0x100},
			{name: ".data", va: 0x12000, flags: flagWritable, data: thunkTable(145), vsize: 0x100},
			{name: xtlidSection, va: 0x13000, data: table},
		},
	})
	return img, code.labels
}

func TestNameXTLID(t *testing.T) {
	table := words32(0x90001, 0x11010, 0, 0x11020, 0x90002, 0x11400)
	img, at := xtlidImage(t, table)
	d := disassembleImage(t, img)
	probe := mustFunction(t, d, at["probe"])
	if probe.Name != "D3DXBoxBoundProbe" || probe.Symbol == nil || probe.Symbol.Name != "D3DXBoxBoundProbe" || probe.Symbol.Raw != "D3DXBoxBoundProbe" || probe.Symbol.Lib != "xtlid" || probe.Symbol.VA != at["probe"] {
		t.Fatalf("probe %q %+v", probe.Name, probe.Symbol)
	}
	if other := mustFunction(t, d, at["other"]); other.Symbol != nil || other.Name != "" {
		t.Fatalf("other %q %+v", other.Name, other.Symbol)
	}
	if !d.libraries()[d.sectionOf(at["probe"])] {
		t.Fatal("identity named function does not mark its section as a library")
	}
}

// An identity names a function over whatever a signature guessed for it, keeping the signature's library
func TestNameXTLIDOverridesSignature(t *testing.T) {
	img, at := xtlidImage(t, words32(0x90001, 0x11010))
	d := disassembleImage(t, img)
	probe := mustFunction(t, d, at["probe"])
	probe.Symbol = &Symbol{VA: at["probe"], Raw: "_Guess@4", Name: "Guess", Lib: "d3d8", ArgBytes: 4, Alternates: []string{"_Other@4"}}
	probe.Name = "Guess"
	d.conventions = map[uint32]callConvention{at["probe"]: {}}
	if err := d.nameXTLID(); err != nil {
		t.Fatal(err)
	}
	if probe.Name != "D3DXBoxBoundProbe" || probe.Symbol.Name != "D3DXBoxBoundProbe" || probe.Symbol.Raw != "D3DXBoxBoundProbe" || probe.Symbol.Alternates != nil || probe.Symbol.Lib != "d3d8" || probe.Symbol.ArgBytes != 4 {
		t.Fatalf("probe %q %+v", probe.Name, probe.Symbol)
	}
	if _, kept := d.conventions[at["probe"]]; kept {
		t.Fatal("stale convention kept")
	}
}

func TestNameXTLIDRejectsUnknownIdentity(t *testing.T) {
	img, _ := xtlidImage(t, words32(0xDEAD0001, 0x11010))
	if _, err := Disassemble(img); err == nil || !strings.Contains(err.Error(), "DEAD0001") || !strings.Contains(err.Error(), "identity database") {
		t.Fatalf("err %v", err)
	}
}

func TestNameXTLIDWithoutSection(t *testing.T) {
	spec := sampleSpec()
	d := disassembleImage(t, parseXBE(t, spec))
	if err := d.nameXTLID(); err != nil {
		t.Fatal(err)
	}
}
