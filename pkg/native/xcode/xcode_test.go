package xcode

import (
	"bytes"
	"strings"
	"testing"
)

const sample = `
dd 0xFF000009, 0xFF000008
space 120
xcode 0xF5, 0x08100000, 0x100
pci_write 0x80000884, 0x8001
mem_read 0xFD000000
and_or 0xFFFF, 0x1
start:
io_write 0xC008, 0x21
io_read 0xC000
jne 0x10, start
use_result 0x3, 0x8001
accum 0x0, 0xF
jmp done
done:
exit 0x806
align 16, code
db 0x01, 0x02, 0x03
asciz "Copyright (c) Microsoft Corporation."
space 9
ascii "end of table"
`

// Source assembles, disassembles and assembles again to the same bytes
func TestRoundTrip(t *testing.T) {
	bin, err := Assemble([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	if bin[0x80] != 0xF5 || bin[0x80+9] != 0x04 || bin[0x80+9*10] != 0xEE {
		t.Fatalf("records misplaced: % x", bin[0x80:0x80+9*11])
	}
	jne := bin[0x80+9*6:]
	if rel := int32(uint32(jne[5]) | uint32(jne[6])<<8 | uint32(jne[7])<<16 | uint32(jne[8])<<24); rel != -27 {
		t.Fatalf("jne offset %d, want -27", rel)
	}
	if pad := bin[0x80+9*11:]; !bytes.Equal(pad[:13], Pad(13)) {
		t.Fatalf("alignment pad % x", pad[:13])
	}
	src, err := Disassemble(bin)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"L_00A4:", "jne 0x10, L_00A4", "jmp L_00DA", "exit 0x806\n", "align 16, code", "db 0x01, 0x02, 0x03\n", "asciz \"Copyright (c) Microsoft Corporation.\"", "space 9", "ascii \"end of table\""} {
		if !strings.Contains(string(src), want) {
			t.Errorf("disassembly lacks %q:\n%s", want, src)
		}
	}
	again, err := Assemble(src)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(again, bin) {
		t.Fatalf("round trip differs\n%s", src)
	}
}

// Padding sequences chain seven byte pads before the remainder
func TestPad(t *testing.T) {
	if got := Pad(8); !bytes.Equal(got, []byte{0x8D, 0xA4, 0x24, 0, 0, 0, 0, 0x90}) {
		t.Fatalf("Pad(8) = % x", got)
	}
	if got := Pad(15); len(got) != 15 {
		t.Fatalf("Pad(15) has %d bytes", len(got))
	}
	if got := Pad(0); len(got) != 0 {
		t.Fatalf("Pad(0) has %d bytes", len(got))
	}
}

// Bad lines report their line number
func TestErrors(t *testing.T) {
	for _, src := range []string{"bogus 1", "jne 1", "jmp nowhere", "db 0x100", "exit 1, 2", "space", "align 0", "x:\nx:\n"} {
		if _, err := Assemble([]byte(src)); err == nil {
			t.Errorf("%q assembled", src)
		}
	}
	if _, err := Disassemble(make([]byte, 0x90)); err == nil {
		t.Error("a table without an exit record disassembled")
	}
}
