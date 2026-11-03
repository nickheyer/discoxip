//go:build samples

package native

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

// The reference object dump recovers the prototype, the data table and the source lines of RtlRandom
func TestDumpObject(t *testing.T) {
	var out bytes.Buffer
	lib := filepath.Join("..", "..", "references", "xdk", "5659", "XDK", "xbox", "lib", "xapilib.lib")
	if err := Dump(&out, lib, "random.obj"); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"source   d:\\xboxfre-aug03\\private\\ntos\\rtl\\random.c",
		"compiler Microsoft (R) Optimizing Compiler 13.10.3077",
		"unsigned long RtlpRandomConstantVector[128];",
		"unsigned long __stdcall RtlRandom(unsigned long *Seed); // lines 112-123",
		"; line 113",
		"lea ecx, ptr [edx*4]    ; _RtlpRandomConstantVector",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("dump lacks %q", want)
		}
	}
}

// The kernel database dump names each module and recovers a module's routines with their lines and types
func TestDumpDatabase(t *testing.T) {
	pdb := filepath.Join("..", "..", "references", "xdk", "5659", "XDK", "xbox", "symbols", "Latest", "xboxkrnl.pdb")
	var list bytes.Buffer
	if err := Dump(&list, pdb, ""); err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(list.String(), "\n"); n != 328 {
		t.Fatalf("%d modules listed, want 328", n)
	}
	var out bytes.Buffer
	if err := Dump(&out, pdb, "psdelete.obj"); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"file     d:\\xbox-apr03\\private\\ntos\\ps\\psdelete.c",
		"void __stdcall PspReaper(struct _KDPC *Dpc, void *DeferredContext, void *SystemArgument1, void *SystemArgument2); // section 1 offset 0x1e4ff, 161 bytes, lines 57-109",
		"struct _LIST_ENTRY *Flink; /* 0x0 */",
		"}; /* 28 bytes */",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("dump lacks %q", want)
		}
	}
	old := filepath.Join("..", "..", "references", "xdk", "4627", "XDK", "xbox", "symbols", "4039", "xboxkrnl.pdb")
	out.Reset()
	if err := Dump(&out, old, "psdelete.obj"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "void __stdcall PsTerminateSystemThread(long ExitStatus); // section 1 offset 0x1f3dc, 357 bytes, lines ") {
		t.Errorf("old database dump lacks the routine:\n%s", out.String())
	}
}
