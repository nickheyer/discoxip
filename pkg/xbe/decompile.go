package xbe

import (
	"fmt"
	"sort"
	"strings"

	"golang.org/x/arch/x86/x86asm"
)

// BasicBlock is a sequence of instructions with a single entry and single exit.
type BasicBlock struct {
	StartVA uint32
	EndVA   uint32 // VA after last instruction
	Insns   []Instruction
	Succs   []uint32 // successor block start VAs
	Preds   []uint32 // predecessor block start VAs
}

// StackVar represents a local variable or argument on the stack frame.
type StackVar struct {
	Offset int    // EBP-relative offset (negative = local, positive = arg)
	Size   int    // bytes (1, 2, 4)
	Name   string // auto-generated name
}

// DecompiledFunc is a function with control flow and stack frame analysis.
type DecompiledFunc struct {
	EntryVA    uint32
	Name       string
	Blocks     []*BasicBlock
	BlockMap   map[uint32]*BasicBlock // startVA → block
	StackFrame []StackVar
	Args       int // number of arguments (from RET imm16 or analysis)
	Locals     int // bytes of local stack space (from SUB ESP, N)
	Stmts      []Stmt
}

// Stmt is a decompiled statement.
type Stmt struct {
	Indent int
	Text   string
}

// Decompile converts a disassembled function into structured pseudocode.
func (d *Disassembly) Decompile(fn *Function) *DecompiledFunc {
	df := &DecompiledFunc{
		EntryVA:  fn.EntryVA,
		Name:     fn.Name,
		BlockMap: make(map[uint32]*BasicBlock),
	}
	if df.Name == "" {
		df.Name = fmt.Sprintf("sub_%08X", fn.EntryVA)
	}

	// Step 1: Build basic blocks.
	df.buildBasicBlocks(fn)

	// Step 2: Analyze stack frame.
	df.analyzeStackFrame(fn)

	// Step 3: Generate pseudocode statements.
	df.generateStmts(d)

	return df
}

// buildBasicBlocks splits the function's instruction stream into basic blocks.
func (df *DecompiledFunc) buildBasicBlocks(fn *Function) {
	if len(fn.Instructions) == 0 {
		return
	}

	// Find block leaders: first instruction, jump targets, instructions after jumps.
	leaders := make(map[uint32]bool)
	leaders[fn.EntryVA] = true

	for i, insn := range fn.Instructions {
		op := insn.Inst.Op
		if isJump(op) || op == x86asm.CALL {
			target := resolveTarget(&fn.Instructions[i])
			if target != 0 {
				leaders[target] = true
			}
			// Instruction after jump/call is also a leader
			nextVA := insn.VA + uint32(insn.Inst.Len)
			leaders[nextVA] = true
		}
		if op == x86asm.RET {
			nextVA := insn.VA + uint32(insn.Inst.Len)
			leaders[nextVA] = true
		}
	}

	// Build blocks.
	var currentBlock *BasicBlock
	for _, insn := range fn.Instructions {
		if leaders[insn.VA] {
			if currentBlock != nil {
				currentBlock.EndVA = insn.VA
				df.Blocks = append(df.Blocks, currentBlock)
				df.BlockMap[currentBlock.StartVA] = currentBlock
			}
			currentBlock = &BasicBlock{StartVA: insn.VA}
		}
		if currentBlock != nil {
			currentBlock.Insns = append(currentBlock.Insns, insn)
		}
	}
	if currentBlock != nil && len(currentBlock.Insns) > 0 {
		last := currentBlock.Insns[len(currentBlock.Insns)-1]
		currentBlock.EndVA = last.VA + uint32(last.Inst.Len)
		df.Blocks = append(df.Blocks, currentBlock)
		df.BlockMap[currentBlock.StartVA] = currentBlock
	}

	// Build successor/predecessor edges.
	for _, block := range df.Blocks {
		if len(block.Insns) == 0 {
			continue
		}
		last := block.Insns[len(block.Insns)-1]
		op := last.Inst.Op

		if op == x86asm.RET {
			// No successors
			continue
		}

		if isConditionalJump(op) {
			// Two successors: fall-through and jump target
			fallThrough := last.VA + uint32(last.Inst.Len)
			target := resolveTarget(&last)
			block.Succs = append(block.Succs, fallThrough)
			if target != 0 {
				block.Succs = append(block.Succs, target)
			}
		} else if isUnconditionalJump(op) {
			target := resolveTarget(&last)
			if target != 0 {
				block.Succs = append(block.Succs, target)
			}
		} else {
			// Fall-through to next block
			block.Succs = append(block.Succs, block.EndVA)
		}
	}

	// Build predecessor lists.
	for _, block := range df.Blocks {
		for _, succVA := range block.Succs {
			if succ, ok := df.BlockMap[succVA]; ok {
				succ.Preds = append(succ.Preds, block.StartVA)
			}
		}
	}
}

// analyzeStackFrame examines the function prologue/body to determine
// stack frame layout (arguments, locals).
func (df *DecompiledFunc) analyzeStackFrame(fn *Function) {
	seen := make(map[int]bool)

	for _, insn := range fn.Instructions {
		// Detect SUB ESP, N (local space allocation)
		if insn.Inst.Op == x86asm.SUB {
			if dst, ok := insn.Inst.Args[0].(x86asm.Reg); ok && dst == x86asm.ESP {
				if imm, ok := insn.Inst.Args[1].(x86asm.Imm); ok {
					df.Locals = int(imm)
				}
			}
		}

		// Detect RET N (callee-cleaned arguments)
		if insn.Inst.Op == x86asm.RET {
			if len(insn.Inst.Args) > 0 {
				if imm, ok := insn.Inst.Args[0].(x86asm.Imm); ok {
					df.Args = int(imm) / 4
				}
			}
		}

		// Detect EBP-relative memory accesses for stack variables.
		for _, arg := range insn.Inst.Args {
			if mem, ok := arg.(x86asm.Mem); ok {
				if mem.Base == x86asm.EBP && mem.Index == 0 {
					offset := int(mem.Disp)
					if !seen[offset] {
						seen[offset] = true
						size := insn.Inst.MemBytes
						if size == 0 {
							size = 4
						}
						var name string
						if offset >= 8 {
							name = fmt.Sprintf("arg_%X", offset-8)
						} else if offset < 0 {
							name = fmt.Sprintf("var_%X", -offset)
						} else {
							name = fmt.Sprintf("ebp_%X", offset)
						}
						df.StackFrame = append(df.StackFrame, StackVar{
							Offset: offset,
							Size:   size,
							Name:   name,
						})
					}
				}
			}
		}
	}

	sort.Slice(df.StackFrame, func(i, j int) bool {
		return df.StackFrame[i].Offset < df.StackFrame[j].Offset
	})
}

// generateStmts produces pseudocode statements from the basic blocks.
func (df *DecompiledFunc) generateStmts(d *Disassembly) {
	// Function signature
	var argNames []string
	for i := 0; i < df.Args; i++ {
		argNames = append(argNames, fmt.Sprintf("arg%d", i))
	}
	sig := fmt.Sprintf("%s(%s)", df.Name, strings.Join(argNames, ", "))
	df.Stmts = append(df.Stmts, Stmt{0, sig + " {"})

	// Declare locals
	for _, sv := range df.StackFrame {
		if sv.Offset < 0 {
			typeName := "int"
			switch sv.Size {
			case 1:
				typeName = "byte"
			case 2:
				typeName = "short"
			}
			df.Stmts = append(df.Stmts, Stmt{1, fmt.Sprintf("%s %s;", typeName, sv.Name)})
		}
	}

	// Walk blocks in order and emit statements.
	for _, block := range df.Blocks {
		// Emit label if block has predecessors from jumps
		if len(block.Preds) > 0 && block.StartVA != df.EntryVA {
			df.Stmts = append(df.Stmts, Stmt{0, ""})
			df.Stmts = append(df.Stmts, Stmt{0, fmt.Sprintf("loc_%08X:", block.StartVA)})
		}

		for _, insn := range block.Insns {
			stmt := df.decompileInsn(d, &insn)
			if stmt != "" {
				df.Stmts = append(df.Stmts, Stmt{1, stmt})
			}
		}
	}

	df.Stmts = append(df.Stmts, Stmt{0, "}"})
}

// decompileInsn converts a single instruction to a pseudocode statement.
func (df *DecompiledFunc) decompileInsn(d *Disassembly, insn *Instruction) string {
	op := insn.Inst.Op
	args := insn.Inst.Args

	switch op {
	case x86asm.PUSH:
		if len(args) > 0 {
			return fmt.Sprintf("push(%s);", df.fmtArg(d, args[0], insn))
		}

	case x86asm.POP:
		if len(args) > 0 {
			return fmt.Sprintf("%s = pop();", df.fmtArg(d, args[0], insn))
		}

	case x86asm.MOV:
		if len(args) >= 2 {
			dst := df.fmtArg(d, args[0], insn)
			src := df.fmtArg(d, args[1], insn)
			return fmt.Sprintf("%s = %s;", dst, src)
		}

	case x86asm.LEA:
		if len(args) >= 2 {
			dst := df.fmtArg(d, args[0], insn)
			src := df.fmtArgAddr(d, args[1], insn)
			return fmt.Sprintf("%s = &%s;", dst, src)
		}

	case x86asm.ADD:
		if len(args) >= 2 {
			dst := df.fmtArg(d, args[0], insn)
			src := df.fmtArg(d, args[1], insn)
			return fmt.Sprintf("%s += %s;", dst, src)
		}

	case x86asm.SUB:
		if len(args) >= 2 {
			dst := df.fmtArg(d, args[0], insn)
			src := df.fmtArg(d, args[1], insn)
			if dst == "esp" {
				return fmt.Sprintf("// alloc %s bytes on stack", src)
			}
			return fmt.Sprintf("%s -= %s;", dst, src)
		}

	case x86asm.XOR:
		if len(args) >= 2 {
			dst := df.fmtArg(d, args[0], insn)
			src := df.fmtArg(d, args[1], insn)
			if dst == src {
				return fmt.Sprintf("%s = 0;", dst)
			}
			return fmt.Sprintf("%s ^= %s;", dst, src)
		}

	case x86asm.AND:
		if len(args) >= 2 {
			return fmt.Sprintf("%s &= %s;", df.fmtArg(d, args[0], insn), df.fmtArg(d, args[1], insn))
		}

	case x86asm.OR:
		if len(args) >= 2 {
			return fmt.Sprintf("%s |= %s;", df.fmtArg(d, args[0], insn), df.fmtArg(d, args[1], insn))
		}

	case x86asm.NOT:
		if len(args) > 0 {
			return fmt.Sprintf("%s = ~%s;", df.fmtArg(d, args[0], insn), df.fmtArg(d, args[0], insn))
		}

	case x86asm.NEG:
		if len(args) > 0 {
			return fmt.Sprintf("%s = -%s;", df.fmtArg(d, args[0], insn), df.fmtArg(d, args[0], insn))
		}

	case x86asm.SHL:
		if len(args) >= 2 {
			return fmt.Sprintf("%s <<= %s;", df.fmtArg(d, args[0], insn), df.fmtArg(d, args[1], insn))
		}

	case x86asm.SHR:
		if len(args) >= 2 {
			return fmt.Sprintf("%s >>= %s;", df.fmtArg(d, args[0], insn), df.fmtArg(d, args[1], insn))
		}

	case x86asm.SAR:
		if len(args) >= 2 {
			return fmt.Sprintf("%s = (signed)%s >> %s;", df.fmtArg(d, args[0], insn), df.fmtArg(d, args[0], insn), df.fmtArg(d, args[1], insn))
		}

	case x86asm.IMUL:
		if len(args) >= 2 {
			return fmt.Sprintf("%s *= %s;", df.fmtArg(d, args[0], insn), df.fmtArg(d, args[1], insn))
		}

	case x86asm.INC:
		if len(args) > 0 {
			return fmt.Sprintf("%s++;", df.fmtArg(d, args[0], insn))
		}

	case x86asm.DEC:
		if len(args) > 0 {
			return fmt.Sprintf("%s--;", df.fmtArg(d, args[0], insn))
		}

	case x86asm.CMP:
		if len(args) >= 2 {
			return fmt.Sprintf("// cmp %s, %s", df.fmtArg(d, args[0], insn), df.fmtArg(d, args[1], insn))
		}

	case x86asm.TEST:
		if len(args) >= 2 {
			return fmt.Sprintf("// test %s, %s", df.fmtArg(d, args[0], insn), df.fmtArg(d, args[1], insn))
		}

	case x86asm.CALL:
		target := resolveTarget(insn)
		name := df.resolveCallTarget(d, target, insn)
		// Check for indirect call through memory (vtable dispatch)
		if target == 0 && len(args) > 0 {
			if mem, ok := args[0].(x86asm.Mem); ok {
				return fmt.Sprintf("(*%s)(); // vtable call", df.fmtMemAddr(mem))
			}
		}
		return fmt.Sprintf("%s();", name)

	case x86asm.RET:
		if len(args) > 0 {
			if imm, ok := args[0].(x86asm.Imm); ok {
				return fmt.Sprintf("return; // cleanup %d bytes", int(imm))
			}
		}
		return "return;"

	case x86asm.NOP:
		return ""

	case x86asm.LEAVE:
		return "// leave (esp = ebp; pop ebp)"

	case x86asm.JMP:
		target := resolveTarget(insn)
		if target != 0 {
			return fmt.Sprintf("goto loc_%08X;", target)
		}
		return fmt.Sprintf("goto *%s;", df.fmtArg(d, args[0], insn))

	case x86asm.JE, x86asm.JNE, x86asm.JL, x86asm.JLE, x86asm.JG, x86asm.JGE,
		x86asm.JB, x86asm.JBE, x86asm.JA, x86asm.JAE, x86asm.JS, x86asm.JNS,
		x86asm.JO, x86asm.JNO, x86asm.JP, x86asm.JNP:
		target := resolveTarget(insn)
		cond := condName(op)
		return fmt.Sprintf("if (%s) goto loc_%08X;", cond, target)

	case x86asm.MOVZX:
		if len(args) >= 2 {
			return fmt.Sprintf("%s = (unsigned)%s;", df.fmtArg(d, args[0], insn), df.fmtArg(d, args[1], insn))
		}

	case x86asm.MOVSX:
		if len(args) >= 2 {
			return fmt.Sprintf("%s = (signed)%s;", df.fmtArg(d, args[0], insn), df.fmtArg(d, args[1], insn))
		}

	case x86asm.CDQ:
		return "edx = (eax >> 31); // sign-extend eax to edx:eax"

	case x86asm.XCHG:
		if len(args) >= 2 {
			a := df.fmtArg(d, args[0], insn)
			b := df.fmtArg(d, args[1], insn)
			return fmt.Sprintf("swap(%s, %s);", a, b)
		}

	case x86asm.MOVSD:
		return "*(int*)edi = *(int*)esi; esi += 4; edi += 4;"

	case x86asm.MOVSB:
		return "*edi = *esi; esi++; edi++;"

	case x86asm.STOSD:
		return "*(int*)edi = eax; edi += 4;"

	case x86asm.STOSB:
		return "*edi = al; edi++;"

	case x86asm.CMPSB:
		return "// cmpsb (compare *esi, *edi)"

	case x86asm.SCASB:
		return "// scasb (scan for al in *edi)"

	// FPU instructions
	case x86asm.FLD:
		return fmt.Sprintf("fpu_push(%s);", df.fmtArg(d, args[0], insn))
	case x86asm.FSTP:
		return fmt.Sprintf("%s = fpu_pop();", df.fmtArg(d, args[0], insn))
	case x86asm.FST:
		return fmt.Sprintf("%s = fpu_top();", df.fmtArg(d, args[0], insn))
	case x86asm.FILD:
		return fmt.Sprintf("fpu_push((float)%s);", df.fmtArg(d, args[0], insn))
	case x86asm.FISTP:
		return fmt.Sprintf("%s = (int)fpu_pop();", df.fmtArg(d, args[0], insn))
	case x86asm.FADD:
		return fmt.Sprintf("fpu_top() += %s;", df.fmtArgOrST(args))
	case x86asm.FADDP:
		return "st1 += st0; fpu_pop();"
	case x86asm.FSUB:
		return fmt.Sprintf("fpu_top() -= %s;", df.fmtArgOrST(args))
	case x86asm.FSUBP:
		return "st1 -= st0; fpu_pop();"
	case x86asm.FMUL:
		return fmt.Sprintf("fpu_top() *= %s;", df.fmtArgOrST(args))
	case x86asm.FMULP:
		return "st1 *= st0; fpu_pop();"
	case x86asm.FDIV:
		return fmt.Sprintf("fpu_top() /= %s;", df.fmtArgOrST(args))
	case x86asm.FDIVP:
		return "st1 /= st0; fpu_pop();"
	case x86asm.FCOM, x86asm.FCOMP, x86asm.FCOMPP:
		return fmt.Sprintf("// fcompare st0, %s", df.fmtArgOrST(args))
	case x86asm.FCHS:
		return "st0 = -st0;"
	case x86asm.FABS:
		return "st0 = fabs(st0);"
	case x86asm.FSQRT:
		return "st0 = sqrt(st0);"
	case x86asm.FLDZ:
		return "fpu_push(0.0);"
	case x86asm.FLD1:
		return "fpu_push(1.0);"
	case x86asm.FXCH:
		return "swap(st0, st1);"
	case x86asm.FNSTSW:
		return "ax = fpu_status();"
	case x86asm.FNSTCW:
		return fmt.Sprintf("%s = fpu_control();", df.fmtArg(d, args[0], insn))
	case x86asm.FLDCW:
		return fmt.Sprintf("fpu_set_control(%s);", df.fmtArg(d, args[0], insn))
	case x86asm.SAHF:
		return "flags = ah;"
	}

	// Fallback: emit as comment with Intel syntax
	text := x86asm.IntelSyntax(insn.Inst, uint64(insn.VA), d.symbolLookup)
	return fmt.Sprintf("/* %s */", text)
}

// fmtArg formats an instruction argument as a pseudocode expression.
func (df *DecompiledFunc) fmtArg(d *Disassembly, arg x86asm.Arg, insn *Instruction) string {
	if arg == nil {
		return "?"
	}
	switch a := arg.(type) {
	case x86asm.Reg:
		return strings.ToLower(a.String())
	case x86asm.Imm:
		val := int64(a)
		if val >= 0 && val < 256 {
			return fmt.Sprintf("%d", val)
		}
		uval := uint32(val)
		// Check if it's a string reference
		if str := d.Image.ReadUTF16(uval); len(str) > 2 && len(str) < 200 {
			return fmt.Sprintf("%q /* 0x%X */", str, uval)
		}
		return fmt.Sprintf("0x%X", uval)
	case x86asm.Mem:
		return df.fmtMem(d, a)
	case x86asm.Rel:
		target := insn.VA + uint32(insn.Inst.Len) + uint32(int32(a))
		return fmt.Sprintf("0x%X", target)
	}
	return fmt.Sprintf("%v", arg)
}

// fmtArgAddr formats a memory argument as an address expression (for LEA).
func (df *DecompiledFunc) fmtArgAddr(d *Disassembly, arg x86asm.Arg, insn *Instruction) string {
	if mem, ok := arg.(x86asm.Mem); ok {
		return df.fmtMemAddr(mem)
	}
	return df.fmtArg(d, arg, insn)
}

// fmtMem formats a memory operand as a dereference expression.
func (df *DecompiledFunc) fmtMem(d *Disassembly, m x86asm.Mem) string {
	addr := df.fmtMemAddr(m)

	// If it's a simple [imm32] global access, try to resolve
	if m.Base == 0 && m.Index == 0 {
		va := uint32(m.Disp)
		if name, ok := d.Imports[va]; ok {
			return name
		}
	}

	return fmt.Sprintf("*(%s)", addr)
}

// fmtMemAddr formats the address expression of a memory operand.
func (df *DecompiledFunc) fmtMemAddr(m x86asm.Mem) string {
	var parts []string

	if m.Base != 0 {
		base := strings.ToLower(m.Base.String())
		if m.Base == x86asm.EBP {
			// Map EBP-relative accesses to stack variable names
			off := int(m.Disp)
			for _, sv := range df.StackFrame {
				if sv.Offset == off {
					return sv.Name
				}
			}
		}
		parts = append(parts, base)
	}

	if m.Index != 0 {
		idx := strings.ToLower(m.Index.String())
		if m.Scale > 1 {
			parts = append(parts, fmt.Sprintf("%s*%d", idx, m.Scale))
		} else {
			parts = append(parts, idx)
		}
	}

	if m.Disp != 0 {
		if m.Base == x86asm.EBP {
			if m.Disp > 0 {
				parts = append(parts, fmt.Sprintf("+0x%X", m.Disp))
			} else {
				parts = append(parts, fmt.Sprintf("-0x%X", -m.Disp))
			}
		} else if len(parts) > 0 {
			if m.Disp > 0 {
				parts = append(parts, fmt.Sprintf("+0x%X", m.Disp))
			} else {
				parts = append(parts, fmt.Sprintf("-0x%X", -m.Disp))
			}
		} else {
			parts = append(parts, fmt.Sprintf("0x%X", uint32(m.Disp)))
		}
	}

	if len(parts) == 0 {
		return "0"
	}
	return strings.Join(parts, "")
}

// resolveCallTarget returns a human-readable name for a call target.
func (df *DecompiledFunc) resolveCallTarget(d *Disassembly, target uint32, insn *Instruction) string {
	if target == 0 {
		// Indirect call
		return fmt.Sprintf("(*%s)", df.fmtArg(d, insn.Inst.Args[0], insn))
	}

	// Check kernel imports
	if name, ok := d.Imports[target]; ok {
		return name
	}

	// Check named functions
	if fn, ok := d.Functions[target]; ok && fn.Name != "" {
		return fn.Name
	}

	// Check if it's an indirect call through a thunk
	// Pattern: CALL [addr] where addr is in the import table
	if len(insn.Inst.Args) > 0 {
		if mem, ok := insn.Inst.Args[0].(x86asm.Mem); ok {
			va := uint32(mem.Disp)
			if name, ok := d.Imports[va]; ok {
				return name
			}
		}
	}

	return fmt.Sprintf("sub_%08X", target)
}

func (df *DecompiledFunc) fmtArgOrST(args x86asm.Args) string {
	for _, a := range args {
		if a == nil {
			continue
		}
		if r, ok := a.(x86asm.Reg); ok {
			return strings.ToLower(r.String())
		}
	}
	return "st0"
}

func isJump(op x86asm.Op) bool {
	return isConditionalJump(op) || isUnconditionalJump(op)
}

func isConditionalJump(op x86asm.Op) bool {
	switch op {
	case x86asm.JE, x86asm.JNE, x86asm.JL, x86asm.JLE, x86asm.JG, x86asm.JGE,
		x86asm.JB, x86asm.JBE, x86asm.JA, x86asm.JAE, x86asm.JS, x86asm.JNS,
		x86asm.JO, x86asm.JNO, x86asm.JP, x86asm.JNP,
		x86asm.JCXZ, x86asm.JECXZ:
		return true
	}
	return false
}

func isUnconditionalJump(op x86asm.Op) bool {
	return op == x86asm.JMP
}

func condName(op x86asm.Op) string {
	switch op {
	case x86asm.JE:
		return "=="
	case x86asm.JNE:
		return "!="
	case x86asm.JL:
		return "< (signed)"
	case x86asm.JLE:
		return "<= (signed)"
	case x86asm.JG:
		return "> (signed)"
	case x86asm.JGE:
		return ">= (signed)"
	case x86asm.JB:
		return "< (unsigned)"
	case x86asm.JBE:
		return "<= (unsigned)"
	case x86asm.JA:
		return "> (unsigned)"
	case x86asm.JAE:
		return ">= (unsigned)"
	case x86asm.JS:
		return "sign"
	case x86asm.JNS:
		return "!sign"
	case x86asm.JO:
		return "overflow"
	case x86asm.JNO:
		return "!overflow"
	case x86asm.JP:
		return "parity"
	case x86asm.JNP:
		return "!parity"
	}
	return "?"
}

// Format outputs the decompiled function as text.
func (df *DecompiledFunc) Format() string {
	var sb strings.Builder
	for _, s := range df.Stmts {
		indent := strings.Repeat("    ", s.Indent)
		sb.WriteString(indent)
		sb.WriteString(s.Text)
		sb.WriteString("\n")
	}
	return sb.String()
}

// DecompileAll decompiles all functions and returns them sorted by VA.
func (d *Disassembly) DecompileAll() []*DecompiledFunc {
	var funcs []*DecompiledFunc
	for _, fn := range d.Functions {
		funcs = append(funcs, d.Decompile(fn))
	}
	sort.Slice(funcs, func(i, j int) bool { return funcs[i].EntryVA < funcs[j].EntryVA })
	return funcs
}
