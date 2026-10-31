//go:build samples

package xbe

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"golang.org/x/arch/x86/x86asm"
)

// The emitted core must parse as a module before any page runs it
func TestCoreSyntax(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteCore(&buf); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "core.mjs")
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(node, "--check", path).CombinedOutput()
	if err != nil {
		t.Fatalf("node --check: %v\n%s", err, out)
	}
}

// Every condition the executor defines for a live record must build for every live kind, the overflow after the
// operations the processor leaves it undefined for being the one rule the executor has none for
func TestLiveConditions(t *testing.T) {
	ex := &Executor{}
	for _, k := range liveFlagKinds {
		for name, op := range jsConditionOps {
			if _, err := liveCondition(ex, k.kind, op); err != nil && k.kind != "comiss" && k.kind != "eflags" && !(k.kind == "alu" && (op == x86asm.JO || op == x86asm.JNO)) {
				t.Errorf("%s after %s flags: %v", name, k.kind, err)
			}
		}
		if _, err := liveFlagsWord(ex, k.kind); err != nil {
			t.Errorf("flags word after %s flags: %v", k.kind, err)
		}
	}
}

// The harness a node run appends to the core, delivering interrupts over a compare record with and without a step
// to continue, returning untouched, with the zero flag changed and with the return address moved
const interruptReturnHarness = `
function hwAdvance() {}
initMemory(0x10000);
GDT_BASE = 0x1000; GDT_LIMIT = 0xFF; IDT_BASE = 0x2000; IDT_LIMIT = 0x7FF;
const handler = 0x5000, resumeAt = 0x6000, vector = 0x30;
st32(IDT_BASE + 8 * vector, ((8 << 16) | (handler & 0xFFFF)) >>> 0);
st32(IDT_BASE + 8 * vector + 4, ((handler & 0xFFFF0000) | 0x8E00) >>> 0);
let seen = null;
const generic = () => { seen = { by: "generic", FK, FA, FB, IF, ge: condOf(7, FK, FA, FB, FR, FC, FFA, FFB) }; return YIELD; };
const specialized = () => { seen = { by: "step", FK, FA, FB, IF, ge: condOf(7, FK, FA, FB, FR, FC, FFA, FFB) }; return YIELD; };
PLAIN_BOOT.set(resumeAt, generic);
function run(f) { for (let i = 0; i < 16 && f !== YIELD; i++) f = f(); }
function arm() { ESP = 0x8000; IF = true; FK = 1; FA = 0xFFFFFFFF; FB = 0; FR = 0xFFFFFFFF; FC = 0; seen = null; }
function expect(what, want) { for (const k in want) if (seen === null || seen[k] !== want[k]) throw new Error(what + ": " + JSON.stringify(seen)); console.log(what, JSON.stringify(seen)); }
PLAIN_BOOT.set(handler, () => iret(handler));
arm(); run(gate(vector, resumeAt));
expect("plain return", { by: "generic", FK: 1, FA: 0xFFFFFFFF, FB: 0, IF: true, ge: 0 });
arm(); run(gate(vector, resumeAt, specialized));
expect("step continued", { by: "step", FK: 1, FA: 0xFFFFFFFF, ge: 0 });
arm(); run(gate(vector, resumeAt));
expect("step not kept", { by: "generic", FK: 1 });
if (FRAMES.size !== 0) throw new Error("frames kept " + FRAMES.size);
PLAIN_BOOT.set(handler, () => { st32((ESP + 8) >>> 0, (ld32((ESP + 8) >>> 0) ^ 0x40) >>> 0); return iret(handler); });
arm(); run(gate(vector, resumeAt, specialized));
expect("changed word", { by: "step", FK: 6, IF: true });
PLAIN_BOOT.set(handler, () => { st32(ESP, resumeAt + 4); return iret(handler); });
PLAIN_BOOT.set(resumeAt + 4, () => { seen = { by: "moved" }; return YIELD; });
arm(); run(gate(vector, resumeAt, specialized));
expect("moved return", { by: "moved" });
console.log("ok");
`

// A return from an interrupt continues the step it cut into with the record restored, takes the word when the
// handler changed its bits, goes where a moved return address points and keeps nothing for a later entry
func TestInterruptReturnRestoresFlags(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteCore(&buf); err != nil {
		t.Fatal(err)
	}
	buf.WriteString(interruptReturnHarness)
	path := filepath.Join(t.TempDir(), "iret.mjs")
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
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
}
