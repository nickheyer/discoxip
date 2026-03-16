package xbe

import (
	"encoding/base64"
	"fmt"
	"strings"

	"golang.org/x/arch/x86/x86asm"
)

func base64Encode(data []byte) string {
	return base64.StdEncoding.EncodeToString(data)
}

// TranspileToJS converts the entire disassembled XBE into a JavaScript module.
func TranspileToJS(d *Disassembly) string {
	var sb strings.Builder

	sb.WriteString("// Transpiled from xboxdash.xbe\n")
	sb.WriteString("// Auto-generated — do not edit\n\n")
	sb.WriteString(jsRuntime)
	sb.WriteString("\n\n")

	// Load XBE section data into virtual memory.
	// The transpiled code references virtual addresses for strings, globals, etc.
	sb.WriteString("// Initialize virtual memory with XBE section data\n")
	sb.WriteString("(function() {\n")
	for _, sec := range d.Image.Sections {
		if sec.RawSize == 0 {
			continue
		}
		// Encode section data as base64 and decode into mem
		sb.WriteString(fmt.Sprintf("  // Section %s: VA=0x%08X size=0x%X\n", sec.Name, sec.VirtualAddr, sec.VirtualSize))
		sb.WriteString(fmt.Sprintf("  { const d = atob('%s');\n", base64Encode(sec.Data[:sec.RawSize])))
		sb.WriteString(fmt.Sprintf("    for (let i = 0; i < d.length; i++) mem._u8[0x%X + i] = d.charCodeAt(i); }\n", sec.VirtualAddr))
	}
	sb.WriteString("})();\n\n")

	// Track emitted JS function names to avoid duplicates
	emittedNames := make(map[string]bool)

	// Functions whose transpiled bodies are replaced with JS implementations.
	// These are CRT/kernel functions that depend on Xbox boot state we can't replicate.
	crtOverrides := map[uint32]string{
		0x00055A60: "const _sz = stack[stack.length - 1] || 16; regs.eax = heap.alloc(_sz); console.log('[heap] alloc', _sz, '-> 0x' + regs.eax.toString(16)); return regs.eax;", // operator new
		0x000572A1: "regs.eax = heap.alloc(stack[stack.length - 1] || 16); return regs.eax;", // malloc
		0x000572C8: "regs.eax = heap.alloc(stack[stack.length - 1] || 16); return regs.eax;", // malloc wrapper
		0x000541F2: "regs.eax = heap.alloc(stack[stack.length - 1] || 16); return regs.eax;", // RtlAllocateHeap
		0x00054974: "regs.eax = 0; return regs.eax;",                                          // RtlFreeHeap
		0x000579F8: "return regs.eax;",                                                         // __SEH_prolog (no-op in JS)
		0x00057A26: "return regs.eax;",                                                         // __SEH_epilog (no-op in JS)
	}

	// Transpile each function with recovered control flow.
	// Deduplicate JS function names — append VA if name already emitted.
	for _, fn := range d.Functions {
		df := d.Decompile(fn)
		jsName := sanitizeJSName(df.Name)
		if emittedNames[jsName] {
			jsName = fmt.Sprintf("%s_%08X", jsName, df.EntryVA)
			df.Name = jsName
		}
		emittedNames[jsName] = true
		fn.jsName = jsName

		// Check if this function has a CRT override
		if body, ok := crtOverrides[fn.EntryVA]; ok {
			sb.WriteString(fmt.Sprintf("// 0x%08X (CRT override)\nfunction %s() {\n  %s\n}\n\n", fn.EntryVA, jsName, body))
			continue
		}

		structured := RecoverStructure(df.Blocks, df.BlockMap)
		js := emitFunction(d, df, structured)
		sb.WriteString(js)
		sb.WriteString("\n\n")
	}

	// Override Xbox CRT functions with JS implementations.
	// The transpiled heap allocator code (RtlAllocateHeap etc.) can't run because
	// the heap was never initialized by the Xbox boot sequence. Replace malloc/free
	// with JS implementations that allocate from the virtual memory ArrayBuffer.
	sb.WriteString(`
// Override malloc/new/free with working JS heap
function xbe_malloc() {
  // arg on stack: [esp+4] = size
  const size = mem.read32(regs.esp + 4) || stack[stack.length - 1] || 0;
  regs.eax = heap.alloc(size || 16);
  return regs.eax;
}
function xbe_free() {
  heap.free(0);
  regs.eax = 0;
  return regs.eax;
}
`)
	sb.WriteString("\n")

	// Export runtime state so the host can read memory, registers, etc.
	sb.WriteString("export { regs, mem, fpu, stack, flags, heap };\n\n")

	// Export function lookup table
	// Override CRT heap functions with JS implementations
	sb.WriteString("export const functions = new Map([\n")
	for _, fn := range d.Functions {
		sb.WriteString(fmt.Sprintf("  [0x%08X, %s],\n", fn.EntryVA, fn.jsName))
	}
	sb.WriteString("]);\n\n")

	// Replace CRT heap functions with JS implementations
	sb.WriteString("// Override CRT allocator functions\n")
	sb.WriteString("functions.set(0x00055A60, xbe_malloc); // operator new\n")
	sb.WriteString("functions.set(0x000572A1, xbe_malloc); // malloc\n")
	sb.WriteString("functions.set(0x000541F2, xbe_malloc); // RtlAllocateHeap\n")
	sb.WriteString("functions.set(0x000572C8, xbe_malloc); // malloc wrapper\n")
	sb.WriteString("functions.set(0x00054974, xbe_free);   // RtlFreeHeap\n")

	return sb.String()
}

func emitFunction(d *Disassembly, df *DecompiledFunc, body StructuredStmt) string {
	var sb strings.Builder

	name := df.Name
	if name == "" {
		name = fmt.Sprintf("sub_%08X", df.EntryVA)
	}
	jsName := sanitizeJSName(name)

	sb.WriteString(fmt.Sprintf("// 0x%08X\n", df.EntryVA))
	sb.WriteString(fmt.Sprintf("function %s() {\n", jsName))

	emitStmt(d, &sb, body, 1)

	sb.WriteString("}\n")
	return sb.String()
}

func emitStmt(d *Disassembly, sb *strings.Builder, stmt StructuredStmt, indent int) {
	prefix := strings.Repeat("  ", indent)

	switch s := stmt.(type) {
	case *SeqStmt:
		for _, child := range s.Stmts {
			emitStmt(d, sb, child, indent)
		}

	case *BlockStmt:
		for _, insn := range s.Insns {
			js := transpileInsn(d, &insn)
			if js != "" {
				sb.WriteString(prefix)
				sb.WriteString(js)
				sb.WriteString("\n")
			}
		}

	case *IfStmt:
		cond := jsCondition(s.CondInsn)
		if s.Negated {
			cond = negateCondition(cond)
		}
		if s.Else != nil {
			sb.WriteString(fmt.Sprintf("%sif (%s) {\n", prefix, cond))
			emitStmt(d, sb, s.Then, indent+1)
			sb.WriteString(fmt.Sprintf("%s} else {\n", prefix))
			emitStmt(d, sb, s.Else, indent+1)
			sb.WriteString(fmt.Sprintf("%s}\n", prefix))
		} else {
			sb.WriteString(fmt.Sprintf("%sif (%s) {\n", prefix, cond))
			emitStmt(d, sb, s.Then, indent+1)
			sb.WriteString(fmt.Sprintf("%s}\n", prefix))
		}

	case *WhileStmt:
		if s.CondInsn.Inst.Op != 0 {
			cond := jsCondition(s.CondInsn)
			if s.Negated {
				cond = negateCondition(cond)
			}
			sb.WriteString(fmt.Sprintf("%swhile (%s) {\n", prefix, cond))
		} else {
			sb.WriteString(fmt.Sprintf("%swhile (true) {\n", prefix))
		}
		emitStmt(d, sb, s.Body, indent+1)
		sb.WriteString(fmt.Sprintf("%s}\n", prefix))

	case *DoWhileStmt:
		sb.WriteString(fmt.Sprintf("%sdo {\n", prefix))
		emitStmt(d, sb, s.Body, indent+1)
		cond := jsCondition(s.CondInsn)
		sb.WriteString(fmt.Sprintf("%s} while (%s);\n", prefix, cond))

	case *BreakStmt:
		sb.WriteString(fmt.Sprintf("%sbreak;\n", prefix))

	case *ContinueStmt:
		sb.WriteString(fmt.Sprintf("%scontinue;\n", prefix))

	case *ReturnStmt:
		// Check for RET with stack cleanup
		if len(s.Insn.Inst.Args) > 0 {
			if imm, ok := s.Insn.Inst.Args[0].(x86asm.Imm); ok {
				_ = imm // stack cleanup amount — not needed in JS
			}
		}
		sb.WriteString(fmt.Sprintf("%sreturn regs.eax;\n", prefix))

	case *GotoStmt:
		sb.WriteString(fmt.Sprintf("%s/* goto 0x%08X */ // irreducible control flow\n", prefix, s.TargetVA))

	case *LabelStmt:
		sb.WriteString(fmt.Sprintf("%s// label 0x%08X\n", prefix, s.VA))
	}
}

func transpileInsn(d *Disassembly, insn *Instruction) string {
	op := insn.Inst.Op
	args := insn.Inst.Args

	switch op {
	case x86asm.NOP:
		return ""

	case x86asm.PUSH:
		if len(args) > 0 {
			val := jsOperand(d, args[0], insn)
			// For PUSH imm32, annotate string references as comments
			if imm, ok := args[0].(x86asm.Imm); ok {
				v := uint32(int64(imm))
				if str := d.Image.ReadUTF16(v); len(str) > 2 && len(str) < 200 {
					return fmt.Sprintf("stack.push(%s); // %q", val, str)
				}
			}
			return fmt.Sprintf("stack.push(%s);", val)
		}

	case x86asm.POP:
		if len(args) > 0 {
			return jsAssign(d, args[0], "stack.pop()", insn)
		}

	case x86asm.MOV:
		if len(args) >= 2 {
			src := jsOperand(d, args[1], insn)
			return jsAssign(d, args[0], src, insn)
		}

	case x86asm.LEA:
		if len(args) >= 2 {
			dst := jsOperand(d, args[0], insn)
			src := jsMemAddr(args[1].(x86asm.Mem))
			return fmt.Sprintf("%s = %s;", dst, src)
		}

	case x86asm.ADD:
		if len(args) >= 2 {
			dstR := jsOperand(d, args[0], insn)
			src := jsOperand(d, args[1], insn)
			return jsAssign(d, args[0], fmt.Sprintf("(%s + %s) | 0", dstR, src), insn)
		}

	case x86asm.SUB:
		if len(args) >= 2 {
			dstR := jsOperand(d, args[0], insn)
			src := jsOperand(d, args[1], insn)
			if dstR == "regs.esp" {
				return "" // stack alloc — handled by JS stack
			}
			return jsAssign(d, args[0], fmt.Sprintf("(%s - %s) | 0", dstR, src), insn)
		}

	case x86asm.XOR:
		if len(args) >= 2 {
			dstR := jsOperand(d, args[0], insn)
			src := jsOperand(d, args[1], insn)
			if dstR == src && !isMemArg(args[0]) {
				return jsAssign(d, args[0], "0", insn)
			}
			return jsAssign(d, args[0], fmt.Sprintf("%s ^ %s", dstR, src), insn)
		}

	case x86asm.AND:
		if len(args) >= 2 {
			dstR := jsOperand(d, args[0], insn)
			src := jsOperand(d, args[1], insn)
			return jsAssign(d, args[0], fmt.Sprintf("%s & %s", dstR, src), insn)
		}

	case x86asm.OR:
		if len(args) >= 2 {
			dstR := jsOperand(d, args[0], insn)
			src := jsOperand(d, args[1], insn)
			return jsAssign(d, args[0], fmt.Sprintf("%s | %s", dstR, src), insn)
		}

	case x86asm.NOT:
		if len(args) > 0 {
			a := jsOperand(d, args[0], insn)
			return jsAssign(d, args[0], fmt.Sprintf("~%s", a), insn)
		}

	case x86asm.NEG:
		if len(args) > 0 {
			a := jsOperand(d, args[0], insn)
			return jsAssign(d, args[0], fmt.Sprintf("(-%s) | 0", a), insn)
		}

	case x86asm.SHL:
		if len(args) >= 2 {
			dstR := jsOperand(d, args[0], insn)
			src := jsOperand(d, args[1], insn)
			return jsAssign(d, args[0], fmt.Sprintf("%s << %s", dstR, src), insn)
		}

	case x86asm.SHR:
		if len(args) >= 2 {
			dstR := jsOperand(d, args[0], insn)
			src := jsOperand(d, args[1], insn)
			return jsAssign(d, args[0], fmt.Sprintf("%s >>> %s", dstR, src), insn)
		}

	case x86asm.SAR:
		if len(args) >= 2 {
			dstR := jsOperand(d, args[0], insn)
			src := jsOperand(d, args[1], insn)
			return jsAssign(d, args[0], fmt.Sprintf("%s >> %s", dstR, src), insn)
		}

	case x86asm.MUL:
		if len(args) > 0 {
			src := jsOperand(d, args[0], insn)
			return fmt.Sprintf("{ const _r = (regs.eax >>> 0) * (%s >>> 0); regs.eax = _r | 0; regs.edx = (_r / 0x100000000) | 0; }", src)
		}

	case x86asm.IMUL:
		if len(args) == 1 {
			src := jsOperand(d, args[0], insn)
			return fmt.Sprintf("{ const _r = (regs.eax | 0) * (%s | 0); regs.eax = _r | 0; regs.edx = (_r / 0x100000000) | 0; }", src)
		}
		if len(args) >= 2 {
			src := jsOperand(d, args[1], insn)
			if len(args) >= 3 {
				imm := jsOperand(d, args[2], insn)
				return jsAssign(d, args[0], fmt.Sprintf("Math.imul(%s, %s)", src, imm), insn)
			}
			dstR := jsOperand(d, args[0], insn)
			return jsAssign(d, args[0], fmt.Sprintf("Math.imul(%s, %s)", dstR, src), insn)
		}

	case x86asm.DIV:
		if len(args) > 0 {
			src := jsOperand(d, args[0], insn)
			return fmt.Sprintf("{ const _d = %s >>> 0; regs.eax = ((regs.eax >>> 0) / _d) | 0; regs.edx = ((regs.eax >>> 0) %% _d) | 0; }", src)
		}

	case x86asm.IDIV:
		if len(args) > 0 {
			src := jsOperand(d, args[0], insn)
			return fmt.Sprintf("{ const _d = %s | 0; regs.eax = (regs.eax / _d) | 0; regs.edx = (regs.eax %% _d) | 0; }", src)
		}

	case x86asm.INC:
		if len(args) > 0 {
			a := jsOperand(d, args[0], insn)
			return jsAssign(d, args[0], fmt.Sprintf("(%s + 1) | 0", a), insn)
		}

	case x86asm.DEC:
		if len(args) > 0 {
			a := jsOperand(d, args[0], insn)
			return jsAssign(d, args[0], fmt.Sprintf("(%s - 1) | 0", a), insn)
		}

	case x86asm.CMP:
		if len(args) >= 2 {
			a := jsOperand(d, args[0], insn)
			b := jsOperand(d, args[1], insn)
			return fmt.Sprintf("flags = cmp(%s, %s);", a, b)
		}

	case x86asm.TEST:
		if len(args) >= 2 {
			a := jsOperand(d, args[0], insn)
			b := jsOperand(d, args[1], insn)
			return fmt.Sprintf("flags = test(%s, %s);", a, b)
		}

	case x86asm.CALL:
		target := resolveTarget(insn)
		name := resolveCallTarget(d, target, insn)
		return fmt.Sprintf("%s();", name)

	case x86asm.RET:
		return "" // handled by ReturnStmt in structured output

	case x86asm.LEAVE:
		return "regs.esp = regs.ebp; regs.ebp = stack.pop();"

	// Jumps are handled by control flow recovery — they should not appear here
	case x86asm.JMP, x86asm.JE, x86asm.JNE, x86asm.JL, x86asm.JLE, x86asm.JG, x86asm.JGE,
		x86asm.JB, x86asm.JBE, x86asm.JA, x86asm.JAE, x86asm.JS, x86asm.JNS,
		x86asm.JO, x86asm.JNO, x86asm.JP, x86asm.JNP:
		return "" // control flow handled structurally

	case x86asm.MOVZX:
		if len(args) >= 2 {
			src := jsOperand(d, args[1], insn)
			mask := "0xFFFF"
			if insn.Inst.MemBytes == 1 {
				mask = "0xFF"
			}
			return jsAssign(d, args[0], fmt.Sprintf("(%s) & %s", src, mask), insn)
		}

	case x86asm.MOVSX:
		if len(args) >= 2 {
			src := jsOperand(d, args[1], insn)
			shift := "16"
			if insn.Inst.MemBytes == 1 {
				shift = "24"
			}
			return jsAssign(d, args[0], fmt.Sprintf("(%s << %s) >> %s", src, shift, shift), insn)
		}

	case x86asm.CDQ:
		return "regs.edx = (regs.eax >> 31) | 0;"

	case x86asm.XCHG:
		if len(args) >= 2 {
			aRead := jsOperand(d, args[0], insn)
			bRead := jsOperand(d, args[1], insn)
			aWrite := jsAssign(d, args[0], "_t2", insn)
			bWrite := jsAssign(d, args[1], "_t1", insn)
			return fmt.Sprintf("{ const _t1 = %s; const _t2 = %s; %s %s }", aRead, bRead, bWrite, aWrite)
		}

	case x86asm.MOVSD:
		return "mem.write32(regs.edi, mem.read32(regs.esi)); regs.esi += 4; regs.edi += 4;"
	case x86asm.MOVSB:
		return "mem.write8(regs.edi, mem.read8(regs.esi)); regs.esi++; regs.edi++;"
	case x86asm.STOSD:
		return "mem.write32(regs.edi, regs.eax); regs.edi += 4;"
	case x86asm.STOSB:
		return "mem.write8(regs.edi, regs.eax & 0xFF); regs.edi++;"

	// FPU
	case x86asm.FLD:
		if len(args) > 0 {
			return fmt.Sprintf("fpu.push(%s);", jsOperand(d, args[0], insn))
		}
	case x86asm.FSTP:
		if len(args) > 0 {
			if isMemArg(args[0]) {
				addr := jsMemAddr(args[0].(x86asm.Mem))
				return fmt.Sprintf("mem.writeF32(%s, fpu.pop());", addr)
			}
			return fmt.Sprintf("%s = fpu.pop();", jsOperand(d, args[0], insn))
		}
	case x86asm.FST:
		if len(args) > 0 {
			if isMemArg(args[0]) {
				addr := jsMemAddr(args[0].(x86asm.Mem))
				return fmt.Sprintf("mem.writeF32(%s, fpu.top());", addr)
			}
			return fmt.Sprintf("%s = fpu.top();", jsOperand(d, args[0], insn))
		}
	case x86asm.FILD:
		if len(args) > 0 {
			return fmt.Sprintf("fpu.push(%s | 0);", jsOperand(d, args[0], insn))
		}
	case x86asm.FISTP:
		if len(args) > 0 {
			if isMemArg(args[0]) {
				addr := jsMemAddr(args[0].(x86asm.Mem))
				return fmt.Sprintf("mem.write32(%s, fpu.pop() | 0);", addr)
			}
			return fmt.Sprintf("%s = fpu.pop() | 0;", jsOperand(d, args[0], insn))
		}
	case x86asm.FADD:
		return "fpu.st[fpu.sp] += fpu.st[(fpu.sp + 1) & 7];"
	case x86asm.FADDP:
		return "fpu.st[(fpu.sp + 1) & 7] += fpu.st[fpu.sp]; fpu.sp = (fpu.sp + 1) & 7;"
	case x86asm.FSUB:
		return "fpu.st[fpu.sp] -= fpu.st[(fpu.sp + 1) & 7];"
	case x86asm.FSUBP:
		return "fpu.st[(fpu.sp + 1) & 7] -= fpu.st[fpu.sp]; fpu.sp = (fpu.sp + 1) & 7;"
	case x86asm.FMUL:
		return "fpu.st[fpu.sp] *= fpu.st[(fpu.sp + 1) & 7];"
	case x86asm.FMULP:
		return "fpu.st[(fpu.sp + 1) & 7] *= fpu.st[fpu.sp]; fpu.sp = (fpu.sp + 1) & 7;"
	case x86asm.FDIV:
		return "fpu.st[fpu.sp] /= fpu.st[(fpu.sp + 1) & 7];"
	case x86asm.FDIVP:
		return "fpu.st[(fpu.sp + 1) & 7] /= fpu.st[fpu.sp]; fpu.sp = (fpu.sp + 1) & 7;"
	case x86asm.FCHS:
		return "fpu.st[fpu.sp] = -fpu.st[fpu.sp];"
	case x86asm.FABS:
		return "fpu.st[fpu.sp] = Math.abs(fpu.st[fpu.sp]);"
	case x86asm.FSQRT:
		return "fpu.st[fpu.sp] = Math.sqrt(fpu.st[fpu.sp]);"
	case x86asm.FLDZ:
		return "fpu.push(0.0);"
	case x86asm.FLD1:
		return "fpu.push(1.0);"
	case x86asm.FXCH:
		return "{ const _t = fpu.st[fpu.sp]; fpu.st[fpu.sp] = fpu.st[(fpu.sp+1)&7]; fpu.st[(fpu.sp+1)&7] = _t; }"
	case x86asm.FNSTSW:
		return "regs.eax = (regs.eax & 0xFFFF0000) | fpu.statusWord();"
	case x86asm.FNSTCW:
		if len(args) > 0 {
			if isMemArg(args[0]) {
				addr := jsMemAddr(args[0].(x86asm.Mem))
				return fmt.Sprintf("mem.write16(%s, fpu.controlWord());", addr)
			}
			return fmt.Sprintf("%s = fpu.controlWord();", jsOperand(d, args[0], insn))
		}
	case x86asm.FLDCW:
		if len(args) > 0 {
			return fmt.Sprintf("fpu.setControlWord(%s);", jsOperand(d, args[0], insn))
		}
	case x86asm.SAHF:
		return "flags = sahf(regs.eax);"
	case x86asm.FCOM, x86asm.FCOMP, x86asm.FCOMPP:
		return "fpu.compare();"
	case x86asm.FSIN:
		return "fpu.st[fpu.sp] = Math.sin(fpu.st[fpu.sp]);"
	case x86asm.FCOS:
		return "fpu.st[fpu.sp] = Math.cos(fpu.st[fpu.sp]);"
	case x86asm.FPATAN:
		return "fpu.st[(fpu.sp+1)&7] = Math.atan2(fpu.st[(fpu.sp+1)&7], fpu.st[fpu.sp]); fpu.sp = (fpu.sp+1) & 7;"
	case x86asm.FLDPI:
		return "fpu.push(Math.PI);"
	}

	// Unhandled instruction — emit as comment with Intel syntax
	text := x86asm.IntelSyntax(insn.Inst, uint64(insn.VA), d.symbolLookup)
	return fmt.Sprintf("/* 0x%08X: %s */", insn.VA, text)
}

// jsCondition converts a conditional jump instruction to a JS boolean expression.
func jsCondition(insn Instruction) string {
	switch insn.Inst.Op {
	case x86asm.JE:
		return "flags.zf"
	case x86asm.JNE:
		return "!flags.zf"
	case x86asm.JL:
		return "flags.sf !== flags.of"
	case x86asm.JLE:
		return "flags.zf || flags.sf !== flags.of"
	case x86asm.JG:
		return "!flags.zf && flags.sf === flags.of"
	case x86asm.JGE:
		return "flags.sf === flags.of"
	case x86asm.JB:
		return "flags.cf"
	case x86asm.JBE:
		return "flags.cf || flags.zf"
	case x86asm.JA:
		return "!flags.cf && !flags.zf"
	case x86asm.JAE:
		return "!flags.cf"
	case x86asm.JS:
		return "flags.sf"
	case x86asm.JNS:
		return "!flags.sf"
	case x86asm.JO:
		return "flags.of"
	case x86asm.JNO:
		return "!flags.of"
	case x86asm.JP:
		return "flags.pf"
	case x86asm.JNP:
		return "!flags.pf"
	}
	return "true"
}

// negateCondition inverts a JS boolean expression.
func negateCondition(cond string) string {
	switch cond {
	case "flags.zf":
		return "!flags.zf"
	case "!flags.zf":
		return "flags.zf"
	case "flags.cf":
		return "!flags.cf"
	case "!flags.cf":
		return "flags.cf"
	case "flags.sf":
		return "!flags.sf"
	case "!flags.sf":
		return "flags.sf"
	case "flags.of":
		return "!flags.of"
	case "!flags.of":
		return "flags.of"
	case "flags.pf":
		return "!flags.pf"
	case "!flags.pf":
		return "flags.pf"
	case "flags.sf !== flags.of":
		return "flags.sf === flags.of"
	case "flags.sf === flags.of":
		return "flags.sf !== flags.of"
	case "flags.zf || flags.sf !== flags.of":
		return "!flags.zf && flags.sf === flags.of"
	case "!flags.zf && flags.sf === flags.of":
		return "flags.zf || flags.sf !== flags.of"
	case "flags.cf || flags.zf":
		return "!flags.cf && !flags.zf"
	case "!flags.cf && !flags.zf":
		return "flags.cf || flags.zf"
	}
	return "!(" + cond + ")"
}

// jsOperand converts an x86 operand to a JavaScript expression.
func jsOperand(d *Disassembly, arg x86asm.Arg, insn *Instruction) string {
	if arg == nil {
		return "0"
	}
	switch a := arg.(type) {
	case x86asm.Reg:
		return jsReg(a)
	case x86asm.Imm:
		v := uint32(int64(a))
		if v < 256 {
			return fmt.Sprintf("%d", v)
		}
		return fmt.Sprintf("0x%X", v)
	case x86asm.Mem:
		return jsMemRead(d, a, insn)
	case x86asm.Rel:
		target := insn.VA + uint32(insn.Inst.Len) + uint32(int32(a))
		return fmt.Sprintf("0x%X", target)
	}
	return "0"
}

func jsMemRead(d *Disassembly, m x86asm.Mem, insn *Instruction) string {
	addr := jsMemAddr(m)

	// Check for global import reference
	if m.Base == 0 && m.Index == 0 {
		va := uint32(m.Disp)
		if name, ok := d.Imports[va]; ok {
			return sanitizeJSName(name)
		}
	}

	// Determine access size
	size := insn.Inst.MemBytes
	switch size {
	case 1:
		return fmt.Sprintf("mem.read8(%s)", addr)
	case 2:
		return fmt.Sprintf("mem.read16(%s)", addr)
	case 8:
		return fmt.Sprintf("mem.readF64(%s)", addr)
	default:
		return fmt.Sprintf("mem.read32(%s)", addr)
	}
}

func isMemArg(arg x86asm.Arg) bool {
	_, ok := arg.(x86asm.Mem)
	return ok
}

// jsMemStore emits a memory write: mem.write{8,16,32}(addr, value)
func jsMemStore(d *Disassembly, m x86asm.Mem, value string, insn *Instruction) string {
	addr := jsMemAddr(m)
	size := insn.Inst.MemBytes
	switch size {
	case 1:
		return fmt.Sprintf("mem.write8(%s, %s);", addr, value)
	case 2:
		return fmt.Sprintf("mem.write16(%s, %s);", addr, value)
	default:
		return fmt.Sprintf("mem.write32(%s, %s);", addr, value)
	}
}

// jsAssign emits an assignment that handles register sub-parts and memory destinations.
func jsAssign(d *Disassembly, dst x86asm.Arg, expr string, insn *Instruction) string {
	if isMemArg(dst) {
		return jsMemStore(d, dst.(x86asm.Mem), expr, insn)
	}
	if reg, ok := dst.(x86asm.Reg); ok {
		// 8-bit low registers: merge into parent preserving upper bits
		switch reg {
		case x86asm.AL:
			return fmt.Sprintf("regs.eax = (regs.eax & 0xFFFFFF00) | ((%s) & 0xFF);", expr)
		case x86asm.CL:
			return fmt.Sprintf("regs.ecx = (regs.ecx & 0xFFFFFF00) | ((%s) & 0xFF);", expr)
		case x86asm.DL:
			return fmt.Sprintf("regs.edx = (regs.edx & 0xFFFFFF00) | ((%s) & 0xFF);", expr)
		case x86asm.BL:
			return fmt.Sprintf("regs.ebx = (regs.ebx & 0xFFFFFF00) | ((%s) & 0xFF);", expr)
		// 8-bit high registers
		case x86asm.AH:
			return fmt.Sprintf("regs.eax = (regs.eax & 0xFFFF00FF) | (((%s) & 0xFF) << 8);", expr)
		case x86asm.CH:
			return fmt.Sprintf("regs.ecx = (regs.ecx & 0xFFFF00FF) | (((%s) & 0xFF) << 8);", expr)
		case x86asm.DH:
			return fmt.Sprintf("regs.edx = (regs.edx & 0xFFFF00FF) | (((%s) & 0xFF) << 8);", expr)
		case x86asm.BH:
			return fmt.Sprintf("regs.ebx = (regs.ebx & 0xFFFF00FF) | (((%s) & 0xFF) << 8);", expr)
		// 16-bit registers
		case x86asm.AX:
			return fmt.Sprintf("regs.eax = (regs.eax & 0xFFFF0000) | ((%s) & 0xFFFF);", expr)
		case x86asm.CX:
			return fmt.Sprintf("regs.ecx = (regs.ecx & 0xFFFF0000) | ((%s) & 0xFFFF);", expr)
		case x86asm.DX:
			return fmt.Sprintf("regs.edx = (regs.edx & 0xFFFF0000) | ((%s) & 0xFFFF);", expr)
		case x86asm.BX:
			return fmt.Sprintf("regs.ebx = (regs.ebx & 0xFFFF0000) | ((%s) & 0xFFFF);", expr)
		}
	}
	return fmt.Sprintf("%s = %s;", jsOperand(d, dst, insn), expr)
}

func resolveCallTarget(d *Disassembly, target uint32, insn *Instruction) string {
	if target == 0 {
		// Indirect call — resolve the operand to a runtime function lookup
		if len(insn.Inst.Args) > 0 {
			switch arg := insn.Inst.Args[0].(type) {
			case x86asm.Mem:
				// CALL [mem] — read function pointer from memory, look up in function table
				va := uint32(arg.Disp)
				if name, ok := d.Imports[va]; ok {
					return sanitizeJSName(name)
				}
				return fmt.Sprintf("functions.get(mem.read32(%s))", jsMemAddr(arg))
			case x86asm.Reg:
				// CALL reg — register holds function pointer
				return fmt.Sprintf("functions.get(%s)", jsReg(arg))
			}
		}
		// Truly unknown indirect call — emit a runtime dispatch
		text := x86asm.IntelSyntax(insn.Inst, uint64(insn.VA), d.symbolLookup)
		return fmt.Sprintf("functions.get(0) /* unresolved: %s */", text)
	}

	if name, ok := d.Imports[target]; ok {
		return sanitizeJSName(name)
	}
	if fn, ok := d.Functions[target]; ok {
		if fn.jsName != "" {
			return fn.jsName
		}
		if fn.Name != "" {
			return sanitizeJSName(fn.Name)
		}
	}
	return fmt.Sprintf("sub_%08X", target)
}

func sanitizeJSName(name string) string {
	name = strings.ReplaceAll(name, "::", "_")
	name = strings.ReplaceAll(name, "/", "_")
	name = strings.ReplaceAll(name, " ", "_")
	name = strings.ReplaceAll(name, "#", "_")
	name = strings.ReplaceAll(name, "-", "_")
	name = strings.ReplaceAll(name, ".", "_")
	return name
}

func jsReg(r x86asm.Reg) string {
	switch r {
	case x86asm.EAX:
		return "regs.eax"
	case x86asm.ECX:
		return "regs.ecx"
	case x86asm.EDX:
		return "regs.edx"
	case x86asm.EBX:
		return "regs.ebx"
	case x86asm.ESP:
		return "regs.esp"
	case x86asm.EBP:
		return "regs.ebp"
	case x86asm.ESI:
		return "regs.esi"
	case x86asm.EDI:
		return "regs.edi"
	case x86asm.AL:
		return "(regs.eax & 0xFF)"
	case x86asm.AH:
		return "((regs.eax >> 8) & 0xFF)"
	case x86asm.CL:
		return "(regs.ecx & 0xFF)"
	case x86asm.CH:
		return "((regs.ecx >> 8) & 0xFF)"
	case x86asm.DL:
		return "(regs.edx & 0xFF)"
	case x86asm.DH:
		return "((regs.edx >> 8) & 0xFF)"
	case x86asm.BL:
		return "(regs.ebx & 0xFF)"
	case x86asm.BH:
		return "((regs.ebx >> 8) & 0xFF)"
	case x86asm.AX:
		return "(regs.eax & 0xFFFF)"
	case x86asm.CX:
		return "(regs.ecx & 0xFFFF)"
	case x86asm.DX:
		return "(regs.edx & 0xFFFF)"
	case x86asm.BX:
		return "(regs.ebx & 0xFFFF)"
	}
	return fmt.Sprintf("regs.%s", strings.ToLower(r.String()))
}

func jsMemAddr(m x86asm.Mem) string {
	var parts []string

	if m.Base != 0 {
		parts = append(parts, jsReg(m.Base))
	}
	if m.Index != 0 {
		idx := jsReg(m.Index)
		if m.Scale > 1 {
			parts = append(parts, fmt.Sprintf("(%s * %d)", idx, m.Scale))
		} else {
			parts = append(parts, idx)
		}
	}
	if m.Disp != 0 {
		if m.Disp > 0 {
			parts = append(parts, fmt.Sprintf("0x%X", m.Disp))
		} else {
			parts = append(parts, fmt.Sprintf("(-0x%X)", -m.Disp))
		}
	}

	if len(parts) == 0 {
		return "0"
	}
	if len(parts) == 1 {
		return parts[0]
	}
	return "(" + strings.Join(parts, " + ") + ")"
}

// jsRuntime is the JavaScript runtime support for transpiled x86 code.
const jsRuntime = `// x86 register state
const regs = { eax: 0, ecx: 0, edx: 0, ebx: 0, esp: 0, ebp: 0, esi: 0, edi: 0 };
const stack = [];
let flags = { zf: false, sf: false, cf: false, of: false, pf: false };

// Virtual memory
const mem = {
  _buf: new ArrayBuffer(16 * 1024 * 1024),
  _u8: null, _u16: null, _u32: null, _f32: null, _f64: null,
  init() {
    this._u8 = new Uint8Array(this._buf);
    this._u16 = new Uint16Array(this._buf);
    this._u32 = new Uint32Array(this._buf);
    this._f32 = new Float32Array(this._buf);
    this._f64 = new Float64Array(this._buf);
  },
  read8(addr) { return this._u8[addr] || 0; },
  read16(addr) { return this._u16[addr >> 1] || 0; },
  read32(addr) { return this._u32[addr >> 2] || 0; },
  readF32(addr) { return this._f32[addr >> 2] || 0; },
  readF64(addr) { return this._f64[addr >> 3] || 0; },
  write8(addr, v) { this._u8[addr] = v; },
  write16(addr, v) { this._u16[addr >> 1] = v; },
  write32(addr, v) { this._u32[addr >> 2] = v; },
  writeF32(addr, v) { this._f32[addr >> 2] = v; },
  writeF64(addr, v) { this._f64[addr >> 3] = v; },
  readUTF16(addr) {
    let s = '';
    for (let a = addr; ; a += 2) {
      const lo = this._u8[a] || 0;
      const hi = this._u8[a + 1] || 0;
      if (lo === 0 && hi === 0) break;
      if (hi === 0) s += String.fromCharCode(lo);
      else s += String.fromCharCode(lo | (hi << 8));
    }
    return s;
  },
};
mem.init();

// Heap allocator — provides malloc/free for transpiled Xbox code.
// Uses a simple bump allocator starting at 0x200000 (2MB mark).
// The Xbox dashboard allocates ~200KB of material/scene objects.
const heap = {
  base: 0x200000,
  ptr: 0x200000,
  alloc(size) {
    size = ((size + 15) & ~15) >>> 0; // 16-byte align
    const addr = this.ptr;
    this.ptr += size;
    return addr;
  },
  free(addr) { /* bump allocator doesn't free */ },
};

// x87 FPU
const fpu = {
  st: new Float64Array(8),
  sp: 0,
  push(v) { this.sp = (this.sp - 1) & 7; this.st[this.sp] = v; },
  pop() { const v = this.st[this.sp]; this.sp = (this.sp + 1) & 7; return v; },
  top() { return this.st[this.sp]; },
  statusWord() {
    const c0 = (this._cmp >> 0) & 1;
    const c2 = (this._cmp >> 2) & 1;
    const c3 = (this._cmp >> 3) & 1;
    return (c0 << 8) | (c2 << 10) | (c3 << 14);
  },
  controlWord() { return 0x037F; },
  setControlWord(v) {},
  _cmp: 0,
  compare() {
    const a = this.st[this.sp], b = this.st[(this.sp + 1) & 7];
    if (a > b) this._cmp = 0;
    else if (a < b) this._cmp = 1;
    else if (a === b) this._cmp = 8;
    else this._cmp = 7; // NaN
  },
};

function cmp(a, b) {
  const r = (a - b) | 0;
  return { zf: r === 0, sf: r < 0, cf: (a >>> 0) < (b >>> 0), of: false, pf: false };
}
function test(a, b) {
  const r = a & b;
  return { zf: r === 0, sf: (r & 0x80000000) !== 0, cf: false, of: false, pf: false };
}
function sahf(eax) {
  const ah = (eax >> 8) & 0xFF;
  return { zf: !!(ah & 0x40), sf: !!(ah & 0x80), cf: !!(ah & 1), of: false, pf: !!(ah & 4) };
}`
