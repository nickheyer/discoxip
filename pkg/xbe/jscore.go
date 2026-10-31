package xbe

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

// Pages the page's translation buffer covers and the slots of the two tables the processor walks
const (
	jsTLBEntries = 1 << (32 - pageShift)
)

// WriteCore writes the processor the translated blocks run on, its memory, descriptor tables, interrupt delivery,
// dispatcher and the operations the blocks call, the conditions and flags word built from the executor's rules
func WriteCore(w io.Writer) error {
	bw := bufio.NewWriterSize(w, 1<<20)
	g := &jsGen{w: bw, names: map[string]string{}, plain: map[uint32]string{}, routines: map[uint32]bool{}}
	g.writeState()
	g.writeMemory()
	g.writeSegments()
	g.writeInterrupts()
	g.writeDispatcher()
	g.writeRunLoop()
	g.writeArithmetic()
	g.writeProcessorOperations()
	if err := g.writeFlagDefinitions(); err != nil {
		return err
	}
	return bw.Flush()
}

// The registers, segments, flag record, float stack, vector lanes and control state of the one processor
func (g *jsGen) writeState() {
	fmt.Fprintf(g.w, "// ---- the processor ----\n")
	fmt.Fprintf(g.w, "let %s;\n", jsDeclare(registerNames[:], "0"))
	segs := make([]string, len(segNames))
	bases := make([]string, len(segNames))
	for i, name := range segNames {
		segs[i] = "SEG_" + name
		bases[i] = "BASE_" + name
	}
	fmt.Fprintf(g.w, "let %s;\nlet %s;\n", jsDeclare(segs, "0"), jsDeclare(bases, "0"))
	fmt.Fprintf(g.w, "let FK = 0, FA = 0, FB = 0, FR = 0, FC = 0, FFA = NaN, FFB = NaN, FCA = NaN, FCB = NaN;\n")
	fmt.Fprintf(g.w, "let ST = [], CW = 0x%X, DF = false, IF = false, SELF = 0;\n", fpuDefaultControlWord)
	fmt.Fprintf(g.w, "const XMM = new Float64Array(32), MMX = new Uint32Array(16), CR = new Uint32Array(8);\n")
	fmt.Fprintf(g.w, "let GDT_BASE = 0, GDT_LIMIT = 0, IDT_BASE = 0, IDT_LIMIT = 0, TR = 0;\n")
	fmt.Fprintf(g.w, "class GuestFault extends Error { constructor(message, va) { super(va === undefined ? message : message + \" at \" + hex(va)); this.va = va; } }\n")
	fmt.Fprintf(g.w, "function hex(v) { return \"0x\" + (v >>> 0).toString(16).toUpperCase().padStart(8, \"0\"); }\n")
	fmt.Fprintf(g.w, "function fault(message, va) { throw new GuestFault(message, va); }\n")
}

func jsDeclare(names []string, value string) string {
	parts := make([]string, len(names))
	for i, n := range names {
		parts[i] = n + " = " + value
	}
	return strings.Join(parts, ", ")
}

// Memory as the processor reaches it, RAM below the size the kernel counts, the devices above it, and every
// virtual address walked through the page tables once paging is on, the walk cached per page for RAM
func (g *jsGen) writeMemory() {
	fmt.Fprintf(g.w, "// ---- memory ----\n")
	fmt.Fprintf(g.w, "let RAM_BYTES = 0, ram = null, ramView = null, PAGE_WRITES = null;\n")
	fmt.Fprintf(g.w, "const TLB_R = new Int32Array(%d), TLB_W = new Int32Array(%d);\n", jsTLBEntries, jsTLBEntries)
	fmt.Fprintf(g.w, "function initMemory(bytes) { RAM_BYTES = bytes; ram = new Uint8Array(bytes); ramView = new DataView(ram.buffer); PAGE_WRITES = new Uint32Array(bytes >>> %d); }\n", pageShift)
	fmt.Fprintf(g.w, "function flushTLB() { TLB_R.fill(0); TLB_W.fill(0); }\n")
	fmt.Fprintf(g.w, "function physRead(pa, size) {\n")
	fmt.Fprintf(g.w, "if (pa + size <= RAM_BYTES) { switch (size) { case 1: return ram[pa]; case 2: return ramView.getUint16(pa, true); case 4: return ramView.getUint32(pa, true); } }\n")
	fmt.Fprintf(g.w, "return hwRead(pa >>> 0, size) >>> 0;\n}\n")
	fmt.Fprintf(g.w, "function physWrite(pa, size, v) {\n")
	fmt.Fprintf(g.w, "if (pa + size <= RAM_BYTES) { PAGE_WRITES[pa >>> %d]++; switch (size) { case 1: ram[pa] = v; return; case 2: ramView.setUint16(pa, v, true); return; case 4: ramView.setUint32(pa, v, true); return; } }\n", pageShift)
	fmt.Fprintf(g.w, "hwWrite(pa >>> 0, size, v >>> 0);\n}\n")
	pageMask := uint32(1<<pageShift - 1)
	largeMask := uint32(1<<largePageShift - 1)
	fmt.Fprintf(g.w, "// A virtual address through the tables the processor walks, the accessed and dirty bits set as it would\n")
	fmt.Fprintf(g.w, "function translate(va, write, quiet) {\n")
	fmt.Fprintf(g.w, "if ((CR[0] & 0x%X) === 0) return va >>> 0;\n", uint32(cr0Paging))
	fmt.Fprintf(g.w, "const dirAt = ((CR[3] & ~0x%X) + ((va >>> %d) << 2)) >>> 0;\n", pageMask, largePageShift)
	fmt.Fprintf(g.w, "const dir = physRead(dirAt, 4);\n")
	fmt.Fprintf(g.w, "if ((dir & 0x%X) === 0) { if (quiet) return -1; fault(\"no page directory entry for \" + hex(va) + readerContext()); }\n", pagePresent)
	fmt.Fprintf(g.w, "if ((dir & 0x%X) !== 0 && (CR[4] & 0x%X) !== 0) {\n", pageLarge, uint32(cr4PageSizeExtensions))
	fmt.Fprintf(g.w, "if (write && (dir & 0x%X) === 0 && (CR[0] & 0x%X) !== 0) { if (quiet) return -1; fault(\"write to a read only large page at \" + hex(va) + readerContext()); }\n", pageWritable, uint32(cr0WriteProtect))
	fmt.Fprintf(g.w, "const bits = (dir | 0x%X | (write ? 0x%X : 0)) >>> 0; if (bits !== dir) physWrite(dirAt, 4, bits);\n", pageAccessed, pageDirty)
	fmt.Fprintf(g.w, "return ((dir & ~0x%X) | (va & 0x%X)) >>> 0;\n}\n", largeMask, largeMask)
	fmt.Fprintf(g.w, "if ((dir & 0x%X) === 0) physWrite(dirAt, 4, (dir | 0x%X) >>> 0);\n", pageAccessed, pageAccessed)
	fmt.Fprintf(g.w, "const pteAt = ((dir & ~0x%X) + (((va >>> %d) & 0x%X) << 2)) >>> 0;\n", pageMask, pageShift, pageTableSlots-1)
	fmt.Fprintf(g.w, "const pte = physRead(pteAt, 4);\n")
	fmt.Fprintf(g.w, "if ((pte & 0x%X) === 0) { if (quiet) return -1; fault(\"no page table entry for \" + hex(va) + readerContext()); }\n", pagePresent)
	fmt.Fprintf(g.w, "if (write && (pte & 0x%X) === 0 && (CR[0] & 0x%X) !== 0) { if (quiet) return -1; fault(\"write to a read only page at \" + hex(va) + readerContext()); }\n", pageWritable, uint32(cr0WriteProtect))
	fmt.Fprintf(g.w, "const bits = (pte | 0x%X | (write ? 0x%X : 0)) >>> 0; if (bits !== pte) physWrite(pteAt, 4, bits);\n", pageAccessed, pageDirty)
	fmt.Fprintf(g.w, "return ((pte & ~0x%X) | (va & 0x%X)) >>> 0;\n}\n", pageMask, pageMask)
	fmt.Fprintf(g.w, "function mapped(va) { return translate(va, false, true) >= 0; }\n")
	fmt.Fprintf(g.w, "function ldSlow(a, size) {\n")
	fmt.Fprintf(g.w, "if ((a & 0x%X) + size > 0x%X) { let v = 0; for (let i = size - 1; i >= 0; i--) v = v * 256 + ld8((a + i) >>> 0); return v >>> 0; }\n", pageMask, pageMask+1)
	fmt.Fprintf(g.w, "const pa = translate(a, false, false);\n")
	fmt.Fprintf(g.w, "const base = (pa & ~0x%X) >>> 0;\n", pageMask)
	fmt.Fprintf(g.w, "if (base + 0x%X <= RAM_BYTES) { TLB_R[a >>> %d] = base + 1; return physRead(pa, size); }\n", pageMask+1, pageShift)
	fmt.Fprintf(g.w, "return physRead(pa, size);\n}\n")
	fmt.Fprintf(g.w, "function stSlow(a, size, v) {\n")
	fmt.Fprintf(g.w, "if ((a & 0x%X) + size > 0x%X) { for (let i = 0; i < size; i++) st8((a + i) >>> 0, (v >>> (8 * i)) & 0xFF); return; }\n", pageMask, pageMask+1)
	fmt.Fprintf(g.w, "const pa = translate(a, true, false);\n")
	fmt.Fprintf(g.w, "const base = (pa & ~0x%X) >>> 0;\n", pageMask)
	fmt.Fprintf(g.w, "if (base + 0x%X <= RAM_BYTES) { TLB_W[a >>> %d] = base + 1; TLB_R[a >>> %d] = base + 1; }\n", pageMask+1, pageShift, pageShift)
	fmt.Fprintf(g.w, "physWrite(pa, size, v);\n}\n")
	for _, size := range []int{1, 2, 4} {
		get, set := map[int]string{1: "", 2: "getUint16", 4: "getUint32"}[size], map[int]string{1: "", 2: "setUint16", 4: "setUint32"}[size]
		last := pageMask + 1 - uint32(size)
		if size == 1 {
			fmt.Fprintf(g.w, "function ld8(a) { const p = TLB_R[a >>> %d]; if (p !== 0) return ram[p - 1 + (a & 0x%X)]; return ldSlow(a, 1); }\n", pageShift, pageMask)
			fmt.Fprintf(g.w, "function st8(a, x) { const p = TLB_W[a >>> %d]; if (p !== 0) { ram[p - 1 + (a & 0x%X)] = x; PAGE_WRITES[(p - 1) >>> %d]++; return; } stSlow(a, 1, x); }\n", pageShift, pageMask, pageShift)
			continue
		}
		fmt.Fprintf(g.w, "function ld%d(a) { const p = TLB_R[a >>> %d], o = a & 0x%X; if (p !== 0 && o <= 0x%X) return ramView.%s(p - 1 + o, true); return ldSlow(a, %d); }\n", size*8, pageShift, pageMask, last, get, size)
		fmt.Fprintf(g.w, "function st%d(a, x) { const p = TLB_W[a >>> %d], o = a & 0x%X; if (p !== 0 && o <= 0x%X) { ramView.%s(p - 1 + o, x, true); PAGE_WRITES[(p - 1) >>> %d]++; return; } stSlow(a, %d, x); }\n", size*8, pageShift, pageMask, last, set, pageShift, size)
	}
	fmt.Fprintf(g.w, "function ldf64(a) { const p = TLB_R[a >>> %d], o = a & 0x%X; if (p !== 0 && o <= 0x%X) return ramView.getFloat64(p - 1 + o, true); return bits2d(ld32((a + 4) >>> 0), ld32(a)); }\n", pageShift, pageMask, pageMask-7)
	fmt.Fprintf(g.w, "function stf64(a, f) { const p = TLB_W[a >>> %d], o = a & 0x%X; if (p !== 0 && o <= 0x%X) { ramView.setFloat64(p - 1 + o, f, true); PAGE_WRITES[(p - 1) >>> %d]++; return; } F64[0] = f; st32(a, U32x2[0]); st32((a + 4) >>> 0, U32x2[1]); }\n", pageShift, pageMask, pageMask-7, pageShift)
	fmt.Fprintf(g.w, "function ldn(a, size) { switch (size) { case 1: return ld8(a); case 2: return ld16(a); } return ld32(a); }\n")
	fmt.Fprintf(g.w, "function stn(a, size, v) { switch (size) { case 1: st8(a, v); return; case 2: st16(a, v); return; } st32(a, v); }\n")
	fmt.Fprintf(g.w, "function memcopy(dst, src, count, size, step) { step = step | 0; for (let i = 0; i < count; i++) { stn(dst, size, ldn(src, size)); dst = (dst + step) >>> 0; src = (src + step) >>> 0; } }\n")
	fmt.Fprintf(g.w, "function memfill(dst, value, count, size, step) { step = step | 0; for (let i = 0; i < count; i++) { stn(dst, size, value); dst = (dst + step) >>> 0; } }\n")
	fmt.Fprintf(g.w, "function readBytes(a, n) { const out = new Uint8Array(n); for (let i = 0; i < n; i++) out[i] = ld8((a + i) >>> 0); return out; }\n")
	fmt.Fprintf(g.w, "function writeBytes(a, bytes) { for (let i = 0; i < bytes.length; i++) st8((a + i) >>> 0, bytes[i]); }\n")
	fmt.Fprintf(g.w, "function physWriteBytes(pa, bytes) { for (let i = 0; i < bytes.length; i++) physWrite((pa + i) >>> 0, 1, bytes[i]); }\n")
	fmt.Fprintf(g.w, "function arg(n) { return ld32((ESP + 4 + 4 * n) >>> 0); }\n")
	fmt.Fprintf(g.w, "function pop() { const v = ld32(ESP); ESP = (ESP + 4) >>> 0; return v; }\n")
	fmt.Fprintf(g.w, "function push(v) { ESP = (ESP - 4) >>> 0; st32(ESP, v >>> 0); }\n")
	fmt.Fprintf(g.w, "function readerContext() { return \" (ESP \" + hex(ESP) + \")\"; }\n")
}

// Segment selectors loaded with the base of their descriptor and the descriptor table registers
func (g *jsGen) writeSegments() {
	fmt.Fprintf(g.w, "// ---- segments and descriptor tables ----\n")
	fmt.Fprintf(g.w, "function descriptorBase(selector) {\n")
	fmt.Fprintf(g.w, "if ((selector & 0x%X) === 0) return 0;\n", selectorIndexMask)
	fmt.Fprintf(g.w, "if ((selector & 0x%X) + %d > GDT_LIMIT + 1) fault(\"selector \" + hex(selector) + \" outside the descriptor table\");\n", selectorIndexMask, descriptorBytes)
	fmt.Fprintf(g.w, "const at = (GDT_BASE + (selector & 0x%X)) >>> 0;\n", selectorIndexMask)
	fmt.Fprintf(g.w, "const lo = ld32(at), hi = ld32((at + 4) >>> 0);\n")
	fmt.Fprintf(g.w, "return ((lo >>> 16) | ((hi & 0xFF) << 16) | (hi & 0xFF000000)) >>> 0;\n}\n")
	for _, name := range segNames {
		fmt.Fprintf(g.w, "function setSeg_%s(v) { SEG_%s = v & 0xFFFF; BASE_%s = descriptorBase(SEG_%s); }\n", name, name, name, name)
	}
}

// Interrupts and traps delivered through the gates of the loaded table, each frame keeping the step it cut into
// and the flag record it left, the return through that frame continuing the step with the record restored
func (g *jsGen) writeInterrupts() {
	arithmetic := uint32(eflagsCarry | eflagsParity | eflagsZero | eflagsSign | eflagsOverflow)
	fmt.Fprintf(g.w, "// ---- interrupts ----\n")
	fmt.Fprintf(g.w, "let IRQ = -1, haltedAt = -1;\n")
	fmt.Fprintf(g.w, "const FRAMES = new Map();\n")
	fmt.Fprintf(g.w, "function flagsWord() { return flagsWordOf(FK, FA, FB, FR, FC, FFA, FFB); }\n")
	fmt.Fprintf(g.w, "function gate(vector, resume, fn) {\n")
	fmt.Fprintf(g.w, "if (%d * vector + %d > IDT_LIMIT + 1) fault(\"vector \" + vector + \" outside the interrupt table\");\n", descriptorBytes, descriptorBytes)
	fmt.Fprintf(g.w, "const at = (IDT_BASE + %d * vector) >>> 0;\n", descriptorBytes)
	fmt.Fprintf(g.w, "const lo = ld32(at), hi = ld32((at + 4) >>> 0);\n")
	fmt.Fprintf(g.w, "if ((hi & 0x%X) === 0) fault(\"vector \" + vector + \" has no present gate\");\n", gatePresentBit)
	fmt.Fprintf(g.w, "const type = hi & 0x%X;\n", gateTypeMask)
	fmt.Fprintf(g.w, "if (type !== 0x%X && type !== 0x%X) fault(\"vector \" + vector + \" has a gate of type \" + hex(type));\n", gateInterrupt, gateTrap)
	fmt.Fprintf(g.w, "const w = flagsWord(); push(w); push(SEG_CS); push(resume);\n")
	fmt.Fprintf(g.w, "FRAMES.set(ESP, { eip: resume, fn: fn, w: w, FK: FK, FA: FA, FB: FB, FR: FR, FC: FC, FFA: FFA, FFB: FFB });\n")
	fmt.Fprintf(g.w, "if (type === 0x%X) IF = false;\n", gateInterrupt)
	fmt.Fprintf(g.w, "setSeg_CS(lo >>> 16);\n")
	fmt.Fprintf(g.w, "return go(((lo & 0xFFFF) | (hi & 0xFFFF0000)) >>> 0);\n}\n")
	fmt.Fprintf(g.w, "function deliverInterrupt(resume, fn) { const vector = hwAcknowledge(); if (vector < 0) return fn !== undefined ? fn : go(resume); return gate(vector, resume, fn); }\n")
	fmt.Fprintf(g.w, "function trap(vector, next, va) { return gate(vector, next); }\n")
	fmt.Fprintf(g.w, "function iret(va) {\n")
	fmt.Fprintf(g.w, "const frame = FRAMES.get(ESP); FRAMES.delete(ESP);\n")
	fmt.Fprintf(g.w, "const eip = pop(), cs = pop(), w = pop(); setSeg_CS(cs);\n")
	fmt.Fprintf(g.w, "if (frame !== undefined && ((w ^ frame.w) & 0x%X) === 0) { IF = (w & 0x%X) !== 0; DF = (w & 0x%X) !== 0; FK = frame.FK; FA = frame.FA; FB = frame.FB; FR = frame.FR; FC = frame.FC; FFA = frame.FFA; FFB = frame.FFB; } else cpu_popfd(w);\n", arithmetic, eflagsInterrupt, eflagsDirection)
	fmt.Fprintf(g.w, "if (frame !== undefined && frame.fn !== undefined && frame.eip === eip) return frame.fn;\n")
	fmt.Fprintf(g.w, "return go(eip);\n}\n")
	fmt.Fprintf(g.w, "function halt(next) { if (!IF) fault(\"the processor halted with interrupts disabled\", next); haltedAt = next; return YIELD; }\n")
}

// The dispatcher mapping an address to the function of its block, through the hooks the page installs, the
// tables of the boot and of the executable and the templates the kernel copies and patches, the address of the
// block entered kept for the devices it reads
func (g *jsGen) writeDispatcher() {
	fmt.Fprintf(g.w, "// ---- the dispatcher ----\n")
	fmt.Fprintf(g.w, "const HOOKS = new Map(), LIFT_ERRORS = new Map(), TEMPLATES = [], PLAIN = new Map(), PLAIN_BOOT = new Map();\n")
	fmt.Fprintf(g.w, "function registerBlocks(table, pairs) { for (let i = 0; i < pairs.length; i += 2) table.set(pairs[i], pairs[i + 1]); }\n")
	fmt.Fprintf(g.w, "let phaseBoot = true, inExecutable = false, ticks = 0, traceCalls = false, AT = 0;\n")
	fmt.Fprintf(g.w, "const callTrail = [];\n")
	fmt.Fprintf(g.w, "function noteCall(va) { callTrail.push(va); if (callTrail.length > 64) callTrail.shift(); }\n")
	fmt.Fprintf(g.w, "function missing(va) { const why = LIFT_ERRORS.get(va); return () => fault(why === undefined ? \"no block lifted at \" + hex(va) : \"block at \" + hex(va) + \" was not lifted: \" + why); }\n")
	fmt.Fprintf(g.w, "function switchTo(index, targets, va) { if (index >= targets.length) fault(\"switch index \" + index + \" outside \" + targets.length + \" cases\", va); return go(targets[index]); }\n")
	fmt.Fprintf(g.w, "function templateMatches(t, self) {\n")
	fmt.Fprintf(g.w, "for (let i = 0; i < t.length; i++) { if (t.patched[i]) continue; if (!mapped((self + i) >>> 0) || ld8((self + i) >>> 0) !== t.bytes[i]) return false; }\nreturn true;\n}\n")
	fmt.Fprintf(g.w, "function templateAt(va) {\n")
	fmt.Fprintf(g.w, "for (const t of TEMPLATES) for (const [off, fn] of t.blocks) { const self = (va - off) >>> 0; if (templateMatches(t, self)) { SELF = self; return fn; } }\nreturn undefined;\n}\n")
	fmt.Fprintf(g.w, "function go(va) {\n")
	fmt.Fprintf(g.w, "AT = va;\n")
	fmt.Fprintf(g.w, "if (IF && IRQ >= 0) return deliverInterrupt(va);\n")
	fmt.Fprintf(g.w, "if ((++ticks & 0x3FF) === 0) hwAdvance();\n")
	fmt.Fprintf(g.w, "if (traceCalls) noteCall(va);\n")
	fmt.Fprintf(g.w, "const h = HOOKS.get(va); if (h !== undefined) return h;\n")
	fmt.Fprintf(g.w, "let f;\n")
	fmt.Fprintf(g.w, "if (!phaseBoot) { f = PLAIN.get(va); if (f !== undefined) { inExecutable = true; return f; } }\n")
	fmt.Fprintf(g.w, "f = PLAIN_BOOT.get(va); if (f !== undefined) return f;\n")
	fmt.Fprintf(g.w, "f = templateAt(va); if (f !== undefined) return f;\n")
	fmt.Fprintf(g.w, "return missing(va);\n}\n")
	fmt.Fprintf(g.w, "function enter(va, fn) { AT = va; if (IF && IRQ >= 0) return deliverInterrupt(va); if ((++ticks & 0x3FF) === 0) hwAdvance(); if (traceCalls) noteCall(va); const h = HOOKS.get(va); return h === undefined ? fn : h; }\n")
	fmt.Fprintf(g.w, "function step(va, fn) { AT = va; if (IF && IRQ >= 0) return deliverInterrupt(va, fn); if ((++ticks & 0x3FF) === 0) hwAdvance(); return fn; }\n")
	fmt.Fprintf(g.w, "function templateSelf(va) { return SELF; }\n")
	fmt.Fprintf(g.w, "function libcall(name, args, va) { fault(\"call of \" + name + \" inside a block\", va); }\n")
}

// Runs blocks until the processor halts, the budget ran out or a fault stopped it
func (g *jsGen) writeRunLoop() {
	fmt.Fprintf(g.w, "// ---- the run loop ----\n")
	fmt.Fprintf(g.w, "const YIELD = () => YIELD;\n")
	fmt.Fprintf(g.w, "let next = null, fatal = null, stepsRun = 0;\n")
	fmt.Fprintf(g.w, "function runSlice(budget) {\n")
	fmt.Fprintf(g.w, "if (fatal !== null) return \"fatal\";\n")
	fmt.Fprintf(g.w, "let f = next;\nlet i = 0;\n")
	fmt.Fprintf(g.w, "try {\n")
	fmt.Fprintf(g.w, "if (f === null) { if (!(IF && IRQ >= 0)) return \"halted\"; f = deliverInterrupt(haltedAt); haltedAt = -1; }\n")
	fmt.Fprintf(g.w, "for (; i < budget; i++) {\nf = f();\n")
	fmt.Fprintf(g.w, "if (f === YIELD) { next = null; stepsRun += i; return \"halted\"; }\n")
	fmt.Fprintf(g.w, "}\n")
	fmt.Fprintf(g.w, "} catch (e) { stepsRun += i; next = f; fatal = e; return \"fatal\"; }\n")
	fmt.Fprintf(g.w, "next = f; stepsRun += i; return \"budget\";\n}\n")
	fmt.Fprintf(g.w, "function stackTrace() {\nconst out = [];\nfor (let i = 0; i < 256 && out.length < 24; i++) { const at = (ESP + 4 * i) >>> 0; if (!mapped(at)) break; const v = ld32(at); if (PLAIN.has(v) || PLAIN_BOOT.has(v) || HOOKS.has(v)) out.push(hex(v) + \"@\" + hex(at)); }\nreturn out.join(\" \");\n}\n")
	fmt.Fprintf(g.w, "function stringAt(v) {\nif (!mapped(v) || !mapped((v + 64) >>> 0)) return \"\";\nlet narrow = \"\", wide = \"\";\nfor (let i = 0; i < 48; i++) { const c = ld8((v + i) >>> 0); if (c === 0) break; narrow += c >= 32 && c < 127 ? String.fromCharCode(c) : \".\"; }\nfor (let i = 0; i < 48; i++) { const c = ld16((v + 2 * i) >>> 0); if (c === 0) break; wide += c >= 32 && c < 127 ? String.fromCharCode(c) : \".\"; }\nreturn (narrow.length > 2 ? \" '\" + narrow + \"'\" : \"\") + (wide.length > 2 ? \" L'\" + wide + \"'\" : \"\");\n}\n")
	fmt.Fprintf(g.w, "function describeFault(e) {\n")
	fmt.Fprintf(g.w, "const blocks = (e.stack || \"\").match(/B[A-Z]*_[0-9A-F]{8}(_[0-9A-F]{8})?/g) || [];\n")
	fmt.Fprintf(g.w, "const lines = [(e instanceof GuestFault ? e.message : e.stack || String(e)) + (blocks.length > 0 ? \" in block \" + blocks[0] : \"\")];\n")
	fmt.Fprintf(g.w, "lines.push(\"stack \" + stackTrace());\n")
	fmt.Fprintf(g.w, "lines.push(\"registers \" + %s.map(([n, v]) => n + \"=\" + hex(v) + stringAt(v)).join(\" \"));\n", jsRegisterPairs())
	fmt.Fprintf(g.w, "lines.push(\"control \" + Array.from(CR).map(hex).join(\" \") + \" gdt \" + hex(GDT_BASE) + \" idt \" + hex(IDT_BASE) + \" if \" + IF + \" irq \" + IRQ);\n")
	fmt.Fprintf(g.w, "if (traceCalls) lines.push(\"calls \" + callTrail.map(hex).join(\" \"));\n")
	fmt.Fprintf(g.w, "return lines.join(\"\\n\");\n}\n")
}

func jsRegisterPairs() string {
	parts := make([]string, len(registerNames))
	for i, name := range registerNames {
		parts[i] = fmt.Sprintf("[%q, %s]", name, name)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// The arithmetic the blocks share, each the function of one operation of the lift's expressions
func (g *jsGen) writeArithmetic() {
	fmt.Fprintf(g.w, "// ---- arithmetic ----\n")
	fmt.Fprintf(g.w, "const F64 = new Float64Array(1), U32x2 = new Uint32Array(F64.buffer);\n")
	fmt.Fprintf(g.w, "const F32 = new Float32Array(1), U32x1 = new Uint32Array(F32.buffer);\n")
	fmt.Fprintf(g.w, "function fbits(f) { F32[0] = f; return U32x1[0]; }\n")
	fmt.Fprintf(g.w, "function fromBits(i) { U32x1[0] = i; return F32[0]; }\n")
	fmt.Fprintf(g.w, "function bits2d(hi, lo) { U32x2[0] = lo; U32x2[1] = hi; return F64[0]; }\n")
	fmt.Fprintf(g.w, "function i64f(hi, lo) { return (hi | 0) * 4294967296 + lo; }\n")
	fmt.Fprintf(g.w, "function udiv(a, b) { if (b === 0) fault(\"division by zero\"); return (a / b) >>> 0; }\n")
	fmt.Fprintf(g.w, "function udiv_rem(a, b) { if (b === 0) fault(\"division by zero\"); return (a %% b) >>> 0; }\n")
	fmt.Fprintf(g.w, "function idiv(a, b) { a |= 0; b |= 0; if (b === 0) fault(\"division by zero\"); if (a === -2147483648 && b === -1) fault(\"division overflow\"); return ((a / b) | 0) >>> 0; }\n")
	fmt.Fprintf(g.w, "function idiv_rem(a, b) { a |= 0; b |= 0; if (b === 0) fault(\"division by zero\"); if (a === -2147483648 && b === -1) fault(\"division overflow\"); return (a %% b) >>> 0; }\n")
	fmt.Fprintf(g.w, "function mulhi(a, b) { const ah = a >>> 16, al = a & 0xFFFF, bh = b >>> 16, bl = b & 0xFFFF; const ll = al * bl, lh = al * bh, hl = ah * bl, hh = ah * bh; const mid = (ll >>> 16) + (lh & 0xFFFF) + (hl & 0xFFFF); return (hh + (lh >>> 16) + (hl >>> 16) + (mid >>> 16)) >>> 0; }\n")
	fmt.Fprintf(g.w, "function imulhi(a, b) { let hi = mulhi(a, b); if ((a | 0) < 0) hi = (hi - b) >>> 0; if ((b | 0) < 0) hi = (hi - a) >>> 0; return hi >>> 0; }\n")
	fmt.Fprintf(g.w, "function udiv64(hi, lo, d) { if (d === 0) fault(\"division by zero\"); const n = (BigInt(hi) << 32n) | BigInt(lo); const q = n / BigInt(d); if (q > 0xFFFFFFFFn) fault(\"division overflow\"); return Number(q); }\n")
	fmt.Fprintf(g.w, "function udiv64_rem(hi, lo, d) { if (d === 0) fault(\"division by zero\"); const n = (BigInt(hi) << 32n) | BigInt(lo); if (n / BigInt(d) > 0xFFFFFFFFn) fault(\"division overflow\"); return Number(n %% BigInt(d)); }\n")
	fmt.Fprintf(g.w, "function idiv64(hi, lo, d) { if (d === 0) fault(\"division by zero\"); const n = BigInt.asIntN(64, (BigInt(hi) << 32n) | BigInt(lo)); const q = n / BigInt(d | 0); if (q > 2147483647n || q < -2147483648n) fault(\"division overflow\"); return Number(BigInt.asUintN(32, q)); }\n")
	fmt.Fprintf(g.w, "function idiv64_rem(hi, lo, d) { if (d === 0) fault(\"division by zero\"); const n = BigInt.asIntN(64, (BigInt(hi) << 32n) | BigInt(lo)); const q = n / BigInt(d | 0); if (q > 2147483647n || q < -2147483648n) fault(\"division overflow\"); return Number(BigInt.asUintN(32, n %% BigInt(d | 0))); }\n")
	fmt.Fprintf(g.w, "function bsf(a) { if (a === 0) return 0; return 31 - Math.clz32(a & -a); }\n")
	fmt.Fprintf(g.w, "function bsr(a) { if (a === 0) return 0; return 31 - Math.clz32(a); }\n")
	fmt.Fprintf(g.w, "function roundEven(f) { if (!Number.isFinite(f)) return f; const r = Math.round(f); if (Math.abs(f - Math.trunc(f)) === 0.5 && r %% 2 !== 0) return r - Math.sign(f); return r; }\n")
	fmt.Fprintf(g.w, "function frem(a, b) { if (b === 0 || !Number.isFinite(a) || Number.isNaN(b)) return NaN; if (!Number.isFinite(b)) return a; return a - b * roundEven(a / b); }\n")
	fmt.Fprintf(g.w, "function f2i(f) { if (Number.isNaN(f) || f >= 2147483648 || f < -2147483648) return 0x80000000; return (Math.trunc(f) | 0) >>> 0; }\n")
	fmt.Fprintf(g.w, "function f2irn(f) { return f2i(roundEven(f)); }\n")
	fmt.Fprintf(g.w, "function f2icw(f, cw) { switch ((cw >>> %d) & %d) { case %d: return f2i(Math.floor(f)); case %d: return f2i(Math.ceil(f)); case %d: return f2i(f); } return f2irn(f); }\n", fpuRoundingShift, fpuRoundingMask, fpuRoundDown, fpuRoundUp, fpuRoundTruncate)
	fmt.Fprintf(g.w, "function fcmpset(a, b, set) { let bit; if (Number.isNaN(a) || Number.isNaN(b)) bit = 8; else if (a < b) bit = 1; else if (a === b) bit = 2; else bit = 4; return (set & bit) !== 0 ? 1 : 0; }\n")
	fmt.Fprintf(g.w, "function stOf(stack, i) { return i < stack.length ? stack[i] : NaN; }\n")
	fmt.Fprintf(g.w, "function fswRegister(eax) { let w = 0; if (Number.isNaN(FCA) || Number.isNaN(FCB)) w |= 0x%X | 0x%X | 0x%X; else if (FCA < FCB) w |= 0x%X; else if (FCA === FCB) w |= 0x%X; return ((eax & 0xFFFF00FF) | w) >>> 0; }\n", fswC0, fswC2, fswC3, fswC0, fswC3)
	fmt.Fprintf(g.w, "function parity8(v) { v &= 0xFF; v ^= v >>> 4; v ^= v >>> 2; v ^= v >>> 1; return (v & 1) === 0 ? 1 : 0; }\n")
	fmt.Fprintf(g.w, "// An extended precision register read out of a save area, and written into one\n")
	fmt.Fprintf(g.w, "function readExtended(at) {\n")
	fmt.Fprintf(g.w, "const lo = ld32(at), hi = ld32((at + 4) >>> 0), se = ld16((at + 8) >>> 0);\n")
	fmt.Fprintf(g.w, "const mantissa = hi * 4294967296 + lo, exponent = se & 0x7FFF, sign = (se & 0x8000) !== 0 ? -1 : 1;\n")
	fmt.Fprintf(g.w, "if (exponent === 0 && mantissa === 0) return sign * 0;\n")
	fmt.Fprintf(g.w, "if (exponent === 0x7FFF) return (hi & 0x7FFFFFFF) === 0 && lo === 0 ? sign * Infinity : NaN;\n")
	fmt.Fprintf(g.w, "return sign * mantissa * Math.pow(2, exponent - %d - %d);\n}\n", extendedBias, extendedMantissa)
	fmt.Fprintf(g.w, "function writeExtended(at, f) {\n")
	fmt.Fprintf(g.w, "let se = 0, hi = 0, lo = 0;\n")
	fmt.Fprintf(g.w, "if (Number.isNaN(f)) { se = 0x7FFF; hi = 0xC0000000; }\n")
	fmt.Fprintf(g.w, "else if (!Number.isFinite(f)) { se = 0x7FFF | (f < 0 ? 0x8000 : 0); hi = 0x80000000; }\n")
	fmt.Fprintf(g.w, "else if (f !== 0) {\n")
	fmt.Fprintf(g.w, "F64[0] = f; const dhi = U32x2[1], dlo = U32x2[0]; let e = (dhi >>> 20) & 0x7FF; let m = (BigInt(dhi & 0xFFFFF) << 32n) | BigInt(dlo);\n")
	fmt.Fprintf(g.w, "if (e === 0) { while ((m & (1n << 52n)) === 0n) { m <<= 1n; e--; } e++; } else m |= 1n << 52n;\n")
	fmt.Fprintf(g.w, "m <<= 11n; se = (e - 1023 + %d) | (f < 0 ? 0x8000 : 0); hi = Number(m >> 32n) >>> 0; lo = Number(m & 0xFFFFFFFFn) >>> 0;\n}\n", extendedBias)
	fmt.Fprintf(g.w, "else if (Object.is(f, -0)) se = 0x8000;\n")
	fmt.Fprintf(g.w, "st32(at, lo); st32((at + 4) >>> 0, hi); st16((at + 8) >>> 0, se);\n}\n")
}

// The processor operations the blocks call, the hardware answering ports, registers and processor identity
func (g *jsGen) writeProcessorOperations() {
	fmt.Fprintf(g.w, "// ---- processor operations ----\n")
	fmt.Fprintf(g.w, "function %s() { IF = false; return 0; }\n", cpuCLI)
	fmt.Fprintf(g.w, "function %s() { IF = true; return 0; }\n", cpuSTI)
	fmt.Fprintf(g.w, "function %s() { DF = false; return 0; }\n", cpuCLD)
	fmt.Fprintf(g.w, "function %s() { DF = true; return 0; }\n", cpuSTD)
	fmt.Fprintf(g.w, "function %s() { CR[0] = (CR[0] & ~0x%X) >>> 0; return 0; }\n", cpuCLTS, uint32(cr0TaskSwitched))
	fmt.Fprintf(g.w, "function %s(kind, a, b, r, c) { if (kind === %d) return flagsWord(); return flagsWordOf(kind, a, b, r, c, FFA, FFB); }\n", cpuPushFD, flagKindCodes["machine"])
	fmt.Fprintf(g.w, "function %s(v) { IF = (v & 0x%X) !== 0; DF = (v & 0x%X) !== 0; FK = %d; FA = v >>> 0; return 0; }\n", cpuPopFD, eflagsInterrupt, eflagsDirection, flagKindCodes["eflags"])
	fmt.Fprintf(g.w, "function %s(v) { CW = v & 0xFFFF; return 0; }\n", cpuFLDCW)
	fmt.Fprintf(g.w, "function %s() { return CW; }\n", cpuFNSTCW)
	fmt.Fprintf(g.w, "function %s() { CW = 0x%X; ST = []; return 0; }\n", cpuFNINIT, fpuInitControlWord)
	fmt.Fprintf(g.w, "function %s() { return 0; }\n", cpuFNCLEX)
	fmt.Fprintf(g.w, "function tsc() { return Math.floor(hwElapsedSeconds() * TABLES.counter_hz); }\n")
	fmt.Fprintf(g.w, "function %s() { return tsc() %% 4294967296; }\n", cpuRDTSCLo)
	fmt.Fprintf(g.w, "function %s() { return Math.floor(tsc() / 4294967296) >>> 0; }\n", cpuRDTSCHi)
	fmt.Fprintf(g.w, "function %s(ptr, value, count, size, step, repne) {\nconst mask = size === 4 ? 0xFFFFFFFF : (1 << (8 * size)) - 1;\nlet n = 0;\nwhile (n < count) { const equal = ldn(ptr, size) === ((value & mask) >>> 0); ptr = (ptr + (step | 0)) >>> 0; n++; if (equal === (repne !== 0)) break; }\nreturn n;\n}\n", cpuSCAS)
	fmt.Fprintf(g.w, "function %s(a, b, count, size, step, repne) {\nlet n = 0;\nwhile (n < count) { const equal = ldn(a, size) === ldn(b, size); a = (a + (step | 0)) >>> 0; b = (b + (step | 0)) >>> 0; n++; if (equal === (repne !== 0)) break; }\nreturn n;\n}\n", cpuCMPS)
	fmt.Fprintf(g.w, "function %s(port, size, va) { return hwPortIn(port & 0xFFFF, size) >>> 0; }\n", portIn)
	fmt.Fprintf(g.w, "function %s(port, value, size, va) { hwPortOut(port & 0xFFFF, value >>> 0, size); return 0; }\n", portOut)
	fmt.Fprintf(g.w, "function %s(port, ptr, count, size, step, va) { for (let i = 0; i < count; i++) { stn(ptr, size, hwPortIn(port & 0xFFFF, size)); ptr = (ptr + (step | 0)) >>> 0; } return count; }\n", cpuINS)
	fmt.Fprintf(g.w, "function %s(port, ptr, count, size, step, va) { for (let i = 0; i < count; i++) { hwPortOut(port & 0xFFFF, ldn(ptr, size), size); ptr = (ptr + (step | 0)) >>> 0; } return count; }\n", cpuOUTS)
	fmt.Fprintf(g.w, "function %s(index, lo, hi, va) { hwWRMSR(index, lo, hi); return 0; }\n", cpuWRMSR)
	fmt.Fprintf(g.w, "function %s(index, va) { return hwRDMSR(index).lo >>> 0; }\n", cpuRDMSRLo)
	fmt.Fprintf(g.w, "function %s(index, va) { return hwRDMSR(index).hi >>> 0; }\n", cpuRDMSRHi)
	fmt.Fprintf(g.w, "function cpu_cpuid(register, leaf, sub, va) { return hwCPUID(leaf, sub)[register] >>> 0; }\n")
	fmt.Fprintf(g.w, "function %s(at, va) { GDT_LIMIT = ld16(at); GDT_BASE = ld32((at + 2) >>> 0); return 0; }\n", cpuLGDT)
	fmt.Fprintf(g.w, "function %s(at, va) { IDT_LIMIT = ld16(at); IDT_BASE = ld32((at + 2) >>> 0); return 0; }\n", cpuLIDT)
	fmt.Fprintf(g.w, "function %s(at, va) { st16(at, GDT_LIMIT); st32((at + 2) >>> 0, GDT_BASE); return 0; }\n", cpuSGDT)
	fmt.Fprintf(g.w, "function %s(at, va) { st16(at, IDT_LIMIT); st32((at + 2) >>> 0, IDT_BASE); return 0; }\n", cpuSIDT)
	fmt.Fprintf(g.w, "function %s(selector, va) { TR = selector & 0xFFFF; return 0; }\n", cpuLTR)
	fmt.Fprintf(g.w, "function %s(n, v, va) {\n", cpuWriteCR)
	fmt.Fprintf(g.w, "if (n === 0 && ((v ^ CR[0]) & (0x%X | 0x%X)) !== 0) flushTLB();\n", uint32(cr0Paging), uint32(cr0WriteProtect))
	fmt.Fprintf(g.w, "if (n === 3 || n === 4) flushTLB();\n")
	fmt.Fprintf(g.w, "CR[n] = v >>> 0;\nreturn 0;\n}\n")
	fmt.Fprintf(g.w, "function %s(n, va) { return CR[n]; }\n", cpuReadCR)
	fmt.Fprintf(g.w, "function %s(at, va) { TLB_R[at >>> %d] = 0; TLB_W[at >>> %d] = 0; return 0; }\n", cpuInvlpg, pageShift, pageShift)
	fmt.Fprintf(g.w, "function tagWord() { let tag = 0; for (let i = 0; i < 8; i++) tag |= (i < ST.length && !Number.isNaN(ST[i]) ? 0 : 3) << (2 * i); return tag; }\n")
	fmt.Fprintf(g.w, "function %s(at, va) {\nst16(at + %d, CW); st16(at + %d, fswRegister(0) & 0xFFFF);\n", cpuFXSave, fxsaveControl, fxsaveStatus)
	fmt.Fprintf(g.w, "let valid = 0; for (let i = 0; i < 8; i++) if (i < ST.length && !Number.isNaN(ST[i])) valid |= 1 << i; st8(at + %d, valid);\n", fxsaveTag)
	fmt.Fprintf(g.w, "for (let i = 0; i < 8; i++) writeExtended((at + %d + %d * i) >>> 0, stOf(ST, i));\n", fxsaveST, fxsaveSlot)
	fmt.Fprintf(g.w, "for (let i = 0; i < 8; i++) for (let lane = 0; lane < 4; lane++) st32((at + %d + %d * i + 4 * lane) >>> 0, fbits(XMM[4 * i + lane]));\nreturn 0;\n}\n", fxsaveXMM, fxsaveSlot)
	fmt.Fprintf(g.w, "function %s(at, va) {\nCW = ld16(at + %d);\n", cpuFXRstor, fxsaveControl)
	fmt.Fprintf(g.w, "for (let i = 0; i < 8; i++) for (let lane = 0; lane < 4; lane++) XMM[4 * i + lane] = fromBits(ld32((at + %d + %d * i + 4 * lane) >>> 0));\nreturn 0;\n}\n", fxsaveXMM, fxsaveSlot)
	fmt.Fprintf(g.w, "function %s(at, index) { if (at === 0) return NaN; const valid = ld8(at + %d); if ((valid & (1 << index)) === 0) return NaN; return readExtended((at + %d + %d * index) >>> 0); }\n", cpuFXRstorST, fxsaveTag, fxsaveST, fxsaveSlot)
	fmt.Fprintf(g.w, "function %s(at, va) {\nst32(at + %d, CW); st32(at + %d, fswRegister(0) & 0xFFFF); st32(at + %d, tagWord());\n", cpuFNSave, fsaveControl, fsaveStatus, fsaveTag)
	fmt.Fprintf(g.w, "for (let i = 0; i < 8; i++) writeExtended((at + %d + %d * i) >>> 0, stOf(ST, i));\nCW = 0x%X; ST = [];\nreturn 0;\n}\n", fsaveRegisters, fsaveRegisterSize, fpuInitControlWord)
	fmt.Fprintf(g.w, "function %s(at, va) { CW = ld32(at + %d) & 0xFFFF; return 0; }\n", cpuFRstor, fsaveControl)
	fmt.Fprintf(g.w, "function %s(at, index) { if (at === 0) return NaN; const tag = ld32(at + %d); if (((tag >>> (2 * index)) & 3) === 3) return NaN; return readExtended((at + %d + %d * index) >>> 0); }\n", cpuFRstorST, fsaveTag, fsaveRegisters, fsaveRegisterSize)
}
