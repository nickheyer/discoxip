//go:build samples

package xbe

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// A writer the block scripts of a test land in one after another
type scriptBuffer struct{ bytes.Buffer }

func (b *scriptBuffer) Close() error { return nil }

// Lifts routines of the kernel image and writes the core and their blocks as one script
func liftedScript(t *testing.T, ti *testImage, perFile int, roots ...string) (string, *BlockTable) {
	t.Helper()
	mm := machineModel(t, ti, roots...)
	var script scriptBuffer
	if err := WriteCore(&script); err != nil {
		t.Fatal(err)
	}
	files := 0
	tbl, err := mm.WriteBlocks(func(int) (io.WriteCloser, error) { files++; return &script, nil }, perFile, "", "PLAIN", nil)
	if err != nil {
		t.Fatal(err)
	}
	if files != tbl.Files {
		t.Fatalf("%d scripts opened for %d written", files, tbl.Files)
	}
	return script.String(), tbl
}

// Runs a script under node and fails unless its last line is ok
func runScript(t *testing.T, name, script string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(node, path).CombinedOutput()
	if err != nil || !bytes.HasSuffix(bytes.TrimSpace(out), []byte("ok")) {
		t.Fatalf("node: %v\n%s", err, out)
	}
	return string(out)
}

// The harness running each lifted routine from a stack whose return address lands on a sentinel block
const blocksHarness = `
function hwAdvance() {}
function hwElapsedSeconds() { return 0; }
const TABLES = { counter_hz: 1 };
initMemory(0x20000);
const RETURN = 0x1F000;
PLAIN_BOOT.set(RETURN, () => YIELD);
function reset() { phaseBoot = false; EAX = ECX = EDX = EBX = EBP = ESI = EDI = 0; ESP = 0x10000; st32(ESP, RETURN); FK = 0; FA = FB = FR = FC = 0; IF = false; DF = false; ST = []; CW = 0x37F; }
function run(entry) { let f = go(entry); for (let i = 0; i < 100000 && f !== YIELD; i++) f = f(); if (f !== YIELD) throw new Error("the routine at " + hex(entry) + " did not return"); }
function expect(what, got, want) { if (got !== want) throw new Error(what + " is " + hex(got) + ", want " + hex(want)); console.log(what, hex(got)); }
`

// Every lifted routine must run under node as the processor runs it, registers, memory, flags and the stack
// moved alike, and the script must carry neither a copy of unchanged state nor a value assigned to itself
func TestWriteBlocksRuns(t *testing.T) {
	ti := kernelImage(t)
	script, tbl := liftedScript(t, ti, 0, "kEntry", "kStack", "kCount", "kDiv", "kCopy", "kFlagsJump", "kPopf", "kPushf", "kSeg", "kCmp", "kCondRoot")
	if tbl.Files != 1 {
		t.Fatalf("%d scripts for one file", tbl.Files)
	}
	entry := fmt.Sprintf("function B_%08X(){", ti.at(t, "kEntry"))
	at := strings.Index(script, entry)
	if at < 0 {
		t.Fatalf("no block for kEntry in\n%s", script)
	}
	block := script[at : at+strings.Index(script[at:], "\n}\n")]
	for _, unwanted := range []string{"ST = [", "CW = ", "stOf(", "ESP = ESP", "const t1 = EAX"} {
		if strings.Contains(block, unwanted) {
			t.Errorf("the entry block carries %q:\n%s", unwanted, block)
		}
	}
	if !strings.Contains(block, "EAX = 0x1;") {
		t.Errorf("the entry block does not set EAX to 1:\n%s", block)
	}
	labels := map[string]uint32{}
	for _, name := range []string{"kEntry", "kStack", "kCount", "kDiv", "kCopy", "kFlagsJump", "kPopf", "kPushf", "kSeg", "kCmp", "kCondRoot", "kvar"} {
		labels[name] = ti.at(t, name)
	}
	var at32 []string
	for name, va := range labels {
		at32 = append(at32, fmt.Sprintf("%s: 0x%X", name, va))
	}
	harness := blocksHarness + "const L = { " + strings.Join(at32, ", ") + " };\n" + `
reset(); run(L.kEntry); expect("kEntry EAX", EAX, 1); expect("kEntry ESP", ESP, 0x10004);
reset(); EAX = RETURN; EBP = 0x8000; ESI = 0x9000; run(L.kStack);
expect("kStack EBX", EBX, RETURN); expect("kStack ECX", ECX, RETURN); expect("kStack EDX", EDX, RETURN); expect("kStack [ESI]", ld32(0x9000), RETURN); expect("kStack [EBP-4]", ld32(0x7FFC), RETURN); expect("kStack ESP", ESP, 0x10000);
reset(); run(L.kCount); expect("kCount ECX", ECX, 0); expect("kCount ZF", condOf(0, FK, FA, FB, FR, FC, FFA, FFB), 1);
reset(); EAX = 17; EDX = 0; ECX = 5; run(L.kDiv); expect("kDiv EAX", EAX, 3); expect("kDiv EDX", EDX, 2);
reset(); st32(0x7000, 0xAAAAAAAA); st32(0x7004, 0xBBBBBBBB); st32(0x7008, 0xCCCCCCCC); ESI = 0x7000; EDI = 0x7100; ECX = 3; DF = true; run(L.kCopy);
expect("kCopy [EDI]", ld32(0x7104), 0xBBBBBBBB); expect("kCopy ESI", ESI, 0x700C); expect("kCopy EDI", EDI, 0x710C); expect("kCopy ECX", ECX, 0); expect("kCopy DF", DF ? 1 : 0, 0);
reset(); ECX = 7; run(L.kFlagsJump); expect("kFlagsJump taken", EAX, 2);
reset(); ECX = 6; run(L.kFlagsJump); expect("kFlagsJump fallen", EAX, 1);
reset(); EAX = 0x646; run(L.kPopf); expect("kPopf ZF", condOf(0, FK, FA, FB, FR, FC, FFA, FFB), 1); expect("kPopf IF", IF ? 1 : 0, 1); expect("kPopf DF", DF ? 1 : 0, 1); expect("kPopf ESP", ESP, 0x10004);
reset(); FK = 1; FA = 5; FB = 5; run(L.kPushf); expect("kPushf ZF bit", EAX & 0x40, 0x40); expect("kPushf CF bit", EAX & 1, 0);
reset(); BASE_FS = 0x3000; st32(0x3020, 0xFEEDFACE); run(L.kSeg); expect("kSeg EAX", EAX, 0xFEEDFACE);
reset(); ECX = 7; run(L.kCmp); expect("kCmp ZF", condOf(0, FK, FA, FB, FR, FC, FFA, FFB), 1); expect("kCmp JB", condOf(2, FK, FA, FB, FR, FC, FFA, FFB), 0);
reset(); FK = 1; FA = 1; FB = 2; run(L.kCondRoot); expect("kCondRoot fallen", EAX, 1);
reset(); FK = 1; FA = 2; FB = 2; run(L.kCondRoot); expect("kCondRoot taken", EAX, 2);
console.log("ok");
`
	runScript(t, "blocks.js", script+harness)
}

// Blocks spread over several scripts must each register in the table so the dispatcher finds them all
func TestWriteBlocksSplitsScripts(t *testing.T) {
	ti := kernelImage(t)
	script, tbl := liftedScript(t, ti, 2, "kEntry", "kStack", "kCount", "kDiv", "kCopy")
	if tbl.Files < 3 {
		t.Fatalf("%d blocks written in %d scripts of 2", tbl.Blocks, tbl.Files)
	}
	if n := strings.Count(script, "registerBlocks(PLAIN, ["); n != tbl.Files {
		t.Fatalf("%d registrations for %d scripts", n, tbl.Files)
	}
	harness := blocksHarness + fmt.Sprintf(`
const want = %d;
if (PLAIN.size !== want) throw new Error("the table holds " + PLAIN.size + " blocks, want " + want);
reset(); EAX = 17; EDX = 0; ECX = 5; run(0x%X); expect("kDiv EAX", EAX, 3); expect("kDiv EDX", EDX, 2);
console.log("ok");
`, len(mmPlainVAs(t, ti, "kEntry", "kStack", "kCount", "kDiv", "kCopy")), ti.at(t, "kDiv"))
	runScript(t, "split.js", script+harness)
}

// The plain block addresses of a lift of the labelled routines
func mmPlainVAs(t *testing.T, ti *testImage, roots ...string) []uint32 {
	t.Helper()
	return machineModel(t, ti, roots...).BlockVAs()
}

// A literal, a temporary or a call result needs no copy before the block's assignments, anything else does
func TestJSAtom(t *testing.T) {
	for _, s := range []string{"0x0", "0xFFFFFFFF", "t1", "t27", "c3", "NaN", "Infinity", "(-Infinity)", "1.5", "(-2)", "1e+21", "0"} {
		if !jsAtom(s) {
			t.Errorf("%q is no atom", s)
		}
	}
	for _, s := range []string{"EAX", "FA", "XMM[3]", "stOf(ST, 0)", "(t1 + 2)", "ld32(t1)", "t", "0x", "0xZZ", "(-)", "tx1", "-"} {
		if jsAtom(s) {
			t.Errorf("%q is an atom", s)
		}
	}
}
