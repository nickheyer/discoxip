package xbe

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/arch/x86/x86asm"
)

// DecodedMaterial is a material extracted from the XBE.
// It contains both the raw binary data AND resolved color/blend values.
// The resolved values are computed by the Go decompiler from the binary —
// the JavaScript runtime just reads them directly.
type DecodedMaterial struct {
	Name         string       `json:"name"`
	Type         string       `json:"type"`                  // constructor variant name
	ShaderCfg    uint32       `json:"shader_cfg"`            // shader configuration flags
	R            uint8        `json:"r"`                     // resolved diffuse red (0-255)
	G            uint8        `json:"g"`                     // resolved diffuse green (0-255)
	B            uint8        `json:"b"`                     // resolved diffuse blue (0-255)
	A            uint8        `json:"a"`                     // resolved alpha (0-255)
	Color1       uint32       `json:"color1"`                // primary packed ARGB (from ctor or Apply writes)
	Color2       uint32       `json:"color2"`                // secondary packed ARGB
	TextureIndex uint32       `json:"texture_index"`         // material texture (0=none, 1=gradient, 2=chrome, 3=cellwall)
	TextureFile  string       `json:"texture_file,omitempty"` // resolved texture filename (e.g. "steel.png")
	KeyMode1     uint8        `json:"key_mode1,omitempty"`   // key mode byte 1 (this+0x220)
	KeyMode2     uint8        `json:"key_mode2,omitempty"`   // key mode byte 2 (this+0x221)
	CtorVA       uint32       `json:"ctor_va"`               // constructor address
	VtableVA     uint32       `json:"vtable_va"`             // vtable pointer set by constructor
	ApplyVA      uint32       `json:"apply_va"`              // Apply method address (vtable slot 0)
	CtorArgs     []uint32     `json:"ctor_args,omitempty"`   // raw constructor arguments (reversed push order)
	Apply        []D3DCall    `json:"apply"`                 // D3D calls made by Apply method
	InitWrites   []MemWrite   `json:"init_writes,omitempty"` // immediate writes to this+offset in Apply
	D3DState     *D3DState    `json:"d3d_state,omitempty"`   // resolved D3D render state from Apply trace
}

// D3DState holds the resolved D3D render state extracted from a material's
// Apply method trace. These are derived from SetRenderState and
// SetTextureStageState calls in the XBE binary, mapped using the Xbox D3D8
// enum values from cxbx-reloaded.
type D3DState struct {
	AlphaBlendEnable bool   `json:"alpha_blend_enable"`
	SrcBlend         uint32 `json:"src_blend"`                    // Xbox blend factor (0x000=ZERO, 0x001=ONE, 0x302=SRCALPHA, etc.)
	DstBlend         uint32 `json:"dst_blend"`                    // Xbox blend factor
	SrcBlendName     string `json:"src_blend_name,omitempty"`     // resolved name (e.g. "D3DBLEND_SRCALPHA")
	DstBlendName     string `json:"dst_blend_name,omitempty"`     // resolved name
	ZWriteEnable     *bool  `json:"z_write_enable,omitempty"`     // nil = not set by Apply
	UsesCombiner     bool   `json:"uses_combiner"`                // true if Apply calls SetRegisterCombinerConstants
	UsesTextureFactor bool  `json:"uses_texture_factor"`          // true if Apply sets D3DRS_TEXTUREFACTOR
}

// D3DCall is a D3D API call extracted from the Apply method.
type D3DCall struct {
	Function string   `json:"fn"`              // resolved function name
	TargetVA uint32   `json:"target_va"`       // call target address
	Args     []uint32 `json:"args,omitempty"`  // immediate arguments (from preceding pushes)
	ECX      int32    `json:"ecx"`   // ECX value at call site (-1 = unknown)
	EDX      int32    `json:"edx"`   // EDX value at call site (-1 = unknown)
}

// MemWrite is an immediate value written to a this+offset field in the Apply method.
type MemWrite struct {
	Offset int32  `json:"offset"` // offset from this pointer (esi/ecx typically)
	Value  uint32 `json:"value"`  // immediate value written
}

// ParseMaterialsFromDecompiled extracts material names and constructor args
// from the decompiled CreateAllMaterials() function text.
func ParseMaterialsFromDecompiled(decompiled string) []DecodedMaterial {
	startMarker := "CMaxMaterial::CreateAllMaterials() {"
	startIdx := strings.Index(decompiled, startMarker)
	if startIdx < 0 {
		return nil
	}

	endIdx := strings.Index(decompiled[startIdx+len(startMarker):], "\n// ---")
	var funcText string
	if endIdx >= 0 {
		funcText = decompiled[startIdx : startIdx+len(startMarker)+endIdx]
	} else {
		funcText = decompiled[startIdx:]
	}

	lines := strings.Split(funcText, "\n")

	ctorRe := regexp.MustCompile(`CMaxMaterial::CMaxMaterial_(\w+)\(\);`)
	nameRe := regexp.MustCompile(`push\("([^"]*)"`)
	immRe := regexp.MustCompile(`^push\((\d+)\);$`)
	hexRe := regexp.MustCompile(`^push\(0x([0-9A-Fa-f]+)\);$`)
	regAssignRe := regexp.MustCompile(`^(\w+) = (\d+);$`)
	regHexRe := regexp.MustCompile(`^(\w+) = 0x([0-9A-Fa-f]+);$`)
	regPushRe := regexp.MustCompile(`^push\((\w+)\);$`)

	regs := map[string]uint32{
		"esi": 0, "edi": 0, "ebx": 0, "ebp": 0, "edx": 0,
	}

	var materials []DecodedMaterial

	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])

		if m := regAssignRe.FindStringSubmatch(line); m != nil {
			if _, ok := regs[m[1]]; ok {
				v, _ := strconv.ParseUint(m[2], 10, 32)
				regs[m[1]] = uint32(v)
			}
		}
		if m := regHexRe.FindStringSubmatch(line); m != nil {
			if _, ok := regs[m[1]]; ok {
				v, _ := strconv.ParseUint(m[2], 16, 32)
				regs[m[1]] = uint32(v)
			}
		}
		if strings.HasSuffix(line, "++;") {
			reg := strings.TrimSuffix(line, "++;")
			if _, ok := regs[reg]; ok {
				regs[reg]++
			}
		}
		if strings.Contains(line, " = 0;") && !strings.Contains(line, "*(") {
			parts := strings.Fields(line)
			if len(parts) >= 3 {
				if _, ok := regs[parts[0]]; ok {
					regs[parts[0]] = 0
				}
			}
		}

		cm := ctorRe.FindStringSubmatch(line)
		if cm == nil {
			continue
		}
		ctorType := cm[1]

		var pushes []uint32
		var name string

		for j := i - 1; j >= 0 && j > i-20; j-- {
			pl := strings.TrimSpace(lines[j])

			if nm := nameRe.FindStringSubmatch(pl); nm != nil {
				name = nm[1]
				continue
			}
			if pm := immRe.FindStringSubmatch(pl); pm != nil {
				v, _ := strconv.ParseUint(pm[1], 10, 64)
				pushes = append([]uint32{uint32(v)}, pushes...)
				continue
			}
			if pm := hexRe.FindStringSubmatch(pl); pm != nil {
				v, _ := strconv.ParseUint(pm[1], 16, 64)
				pushes = append([]uint32{uint32(v)}, pushes...)
				continue
			}
			if pm := regPushRe.FindStringSubmatch(pl); pm != nil {
				regName := pm[1]
				if val, ok := regs[regName]; ok {
					pushes = append([]uint32{val}, pushes...)
					continue
				}
			}
			if strings.Contains(pl, "ecx = eax") {
				continue
			}
			if m := regAssignRe.FindStringSubmatch(pl); m != nil {
				if _, ok := regs[m[1]]; ok {
					v, _ := strconv.ParseUint(m[2], 10, 32)
					regs[m[1]] = uint32(v)
				}
				continue
			}
			break
		}

		if name == "" {
			continue
		}

		materials = append(materials, DecodedMaterial{
			Name:     name,
			Type:     ctorType,
			CtorArgs: pushes,
		})
	}

	return materials
}

// scanCtorFields extracts member initializations from a constructor's x86
// instruction stream. It detects two patterns:
//
//  1. MOV [this+offset], imm — immediate member writes (vtable, textureIndex, etc.)
//  2. MOV reg, [stack+N] then MOV [this+offset], reg — arg-to-member writes
//
// The byte vs dword write size determines how the value is stored (e.g. offset
// 0x0C as a dword is Color1 but as individual bytes is R/G/B/A at 0xC-0xF).
//
// For arg-based writes, the function tracks ESP deltas through PUSH/POP/CALL
// to correctly resolve stack-relative arg references in both EBP-frame and
// ESP-relative constructors.
//
// When the constructor delegates to a parent CMaxMaterial constructor (e.g.
// eggpulse→combiner), scanCtorFields resolves the pushed values and recurses
// into the parent with the forwarded args.
func scanCtorFields(d *Disassembly, fn *Function, mat *DecodedMaterial) {
	// Reverse ctor_args to get source-code argument order (arg1, arg2, ...).
	// arg0 = name is stripped from ctor_args by the parser.
	reversed := make([]uint32, len(mat.CtorArgs))
	for i, v := range mat.CtorArgs {
		reversed[len(mat.CtorArgs)-1-i] = v
	}
	scanCtorWithArgs(d, fn, mat, reversed)
}

// scanCtorWithArgs is the core constructor scanner. args is the constructor's
// arguments in source-code order (arg1, arg2, ..., excluding arg0/name).
func scanCtorWithArgs(d *Disassembly, fn *Function, mat *DecodedMaterial, args []uint32) {
	// Track which registers are potential `this` pointers.
	thisRegs := map[x86asm.Reg]bool{x86asm.ECX: true}

	// Track which registers hold which constructor arg (1-based; 0 = name, skipped).
	type argInfo struct {
		index  int  // 1-based arg index
		isByte bool // loaded as 8-bit register (AL, DL, etc.)
	}
	argInReg := map[x86asm.Reg]argInfo{}

	// Track register values for resolving PUSH reg when delegating to parent.
	regVal := map[x86asm.Reg]uint32{}
	regKnown := map[x86asm.Reg]bool{}

	// Track pushed values for resolving parent constructor delegation.
	// Each entry is the resolved value pushed onto the stack. pushStack[0] is
	// the most recently pushed value (closest to CALL = leftmost arg = name).
	var pushStack []uint32
	var pushKnown []bool

	hasEBPFrame := false
	espDelta := int32(0)
	insns := fn.Instructions

	for i := range insns {
		insn := &insns[i]

		// Detect EBP frame: MOV EBP, ESP
		if insn.Inst.Op == x86asm.MOV && len(insn.Inst.Args) >= 2 {
			if dst, ok := insn.Inst.Args[0].(x86asm.Reg); ok && dst == x86asm.EBP {
				if src, ok := insn.Inst.Args[1].(x86asm.Reg); ok && src == x86asm.ESP {
					hasEBPFrame = true
				}
			}
		}

		// Track `this` pointer copies: MOV reg, ECX
		if insn.Inst.Op == x86asm.MOV && len(insn.Inst.Args) >= 2 {
			if dst, ok := insn.Inst.Args[0].(x86asm.Reg); ok {
				if src, ok := insn.Inst.Args[1].(x86asm.Reg); ok {
					if thisRegs[src] && !is8BitReg(dst) {
						thisRegs[dst] = true
					}
				}
			}
		}

		// Track register values for PUSH resolution
		if insn.Inst.Op == x86asm.MOV && len(insn.Inst.Args) >= 2 {
			if dst, ok := insn.Inst.Args[0].(x86asm.Reg); ok && !is8BitReg(dst) {
				full := fullReg(dst)
				if imm, ok := insn.Inst.Args[1].(x86asm.Imm); ok {
					regVal[full] = uint32(imm)
					regKnown[full] = true
				} else if src, ok := insn.Inst.Args[1].(x86asm.Mem); ok && src.Index == 0 {
					// Arg load — resolve to ctor_args value
					argIdx := resolveArgIdx(src, hasEBPFrame, espDelta)
					if argIdx > 0 && argIdx-1 < len(args) {
						regVal[full] = args[argIdx-1]
						regKnown[full] = true
					}
				}
			}
		}
		if insn.Inst.Op == x86asm.XOR && len(insn.Inst.Args) >= 2 {
			if dst, ok := insn.Inst.Args[0].(x86asm.Reg); ok {
				if src, ok := insn.Inst.Args[1].(x86asm.Reg); ok && dst == src {
					full := fullReg(dst)
					regVal[full] = 0
					regKnown[full] = true
				}
			}
		}

		// Track ESP delta and pushed values.
		switch insn.Inst.Op {
		case x86asm.PUSH:
			espDelta -= 4
			val, known := resolvePushValue(insn, args, hasEBPFrame, espDelta+4, regVal, regKnown)
			pushStack = append([]uint32{val}, pushStack...)
			pushKnown = append([]bool{known}, pushKnown...)
		case x86asm.POP:
			espDelta += 4
			if len(pushStack) > 0 {
				pushStack = pushStack[1:]
				pushKnown = pushKnown[1:]
			}
		case x86asm.SUB:
			if dst, ok := insn.Inst.Args[0].(x86asm.Reg); ok && dst == x86asm.ESP {
				if imm, ok := insn.Inst.Args[1].(x86asm.Imm); ok {
					espDelta -= int32(imm)
				}
			}
		case x86asm.ADD:
			if dst, ok := insn.Inst.Args[0].(x86asm.Reg); ok && dst == x86asm.ESP {
				if imm, ok := insn.Inst.Args[1].(x86asm.Imm); ok {
					espDelta += int32(imm)
				}
			}
		case x86asm.CALL:
			target := resolveTarget(insn)
			callee, isKnown := d.Functions[target]
			cleanup := int32(0)
			if isKnown {
				cleanup = calleeCleanup(callee)
			}

			// If calling a parent CMaxMaterial constructor, resolve pushed args
			// and recurse. The top N entries of pushStack are the parent's args.
			if isKnown && strings.HasPrefix(callee.Name, "CMaxMaterial::CMaxMaterial_") {
				nArgs := int(cleanup / 4)
				var parentArgs []uint32
				// pushStack[0] = name (closest to CALL), [1] = arg1, etc.
				// Strip name (index 0), take the rest as parent args in source order.
				if nArgs > 1 && len(pushStack) >= nArgs {
					for j := 1; j < nArgs; j++ {
						if pushKnown[j] {
							parentArgs = append(parentArgs, pushStack[j])
						} else {
							parentArgs = append(parentArgs, 0)
						}
					}
				}

				// Save fields the child may have set directly.
				savedVtable := mat.VtableVA
				savedTexIdx := mat.TextureIndex
				savedColor1 := mat.Color1
				savedColor2 := mat.Color2

				scanCtorWithArgs(d, callee, mat, parentArgs)

				// Child's direct writes take precedence over parent's.
				if savedVtable != 0 {
					mat.VtableVA = savedVtable
				}
				if savedTexIdx != 0 {
					mat.TextureIndex = savedTexIdx
				}
				if savedColor1 != 0 {
					mat.Color1 = savedColor1
				}
				if savedColor2 != 0 {
					mat.Color2 = savedColor2
				}
			}

			// CALL pushes return address; callee's RET N cleans up.
			espDelta -= 4
			espDelta += 4 + cleanup
			// Remove cleaned-up entries from pushStack
			nCleaned := int(cleanup / 4)
			if nCleaned <= len(pushStack) {
				pushStack = pushStack[nCleaned:]
				pushKnown = pushKnown[nCleaned:]
			} else {
				pushStack = nil
				pushKnown = nil
			}
		}

		if insn.Inst.Op != x86asm.MOV || len(insn.Inst.Args) < 2 {
			continue
		}

		// Pattern: MOV reg, [EBP+N] or [ESP+N] — load constructor arg from stack
		if dst, ok := insn.Inst.Args[0].(x86asm.Reg); ok {
			if src, ok := insn.Inst.Args[1].(x86asm.Mem); ok && src.Index == 0 {
				argIdx := resolveArgIdx(src, hasEBPFrame, espDelta)
				if argIdx > 0 { // >0: skip arg0 (name)
					full := fullReg(dst)
					argInReg[full] = argInfo{index: argIdx, isByte: is8BitReg(dst)}
				}
			}
		}

		// Pattern: MOV [this+offset], reg — store arg to member field
		if mem, ok := insn.Inst.Args[0].(x86asm.Mem); ok {
			if src, ok := insn.Inst.Args[1].(x86asm.Reg); ok {
				if !thisRegs[mem.Base] || mem.Index != 0 {
					continue
				}
				full := fullReg(src)
				if info, ok := argInReg[full]; ok && info.index > 0 {
					ctorIdx := info.index - 1
					if ctorIdx < len(args) {
						applyMemberWrite(mat, mem.Disp, args[ctorIdx], info.isByte)
					}
				}
			}

			// Pattern: MOV [this+offset], imm — immediate member write
			if imm, ok := insn.Inst.Args[1].(x86asm.Imm); ok {
				if thisRegs[mem.Base] && mem.Index == 0 {
					applyMemberWrite(mat, mem.Disp, uint32(imm), false)
				}
			}
		}
	}
}

// resolveArgIdx computes the constructor arg index (0-based, where 0 = name)
// from a stack memory reference.
func resolveArgIdx(mem x86asm.Mem, hasEBPFrame bool, espDelta int32) int {
	switch mem.Base {
	case x86asm.EBP:
		if hasEBPFrame && mem.Disp >= 8 {
			return int(mem.Disp-8) / 4
		}
	case x86asm.ESP:
		entryRel := espDelta + int32(mem.Disp)
		if entryRel >= 4 {
			return int(entryRel-4) / 4
		}
	}
	return -1
}

// resolvePushValue resolves the value being pushed onto the stack.
// espDelta should be the delta BEFORE this push instruction decremented it.
func resolvePushValue(insn *Instruction, args []uint32, hasEBPFrame bool, espDelta int32,
	regVal map[x86asm.Reg]uint32, regKnown map[x86asm.Reg]bool) (uint32, bool) {
	if len(insn.Inst.Args) == 0 {
		return 0, false
	}
	switch arg := insn.Inst.Args[0].(type) {
	case x86asm.Imm:
		return uint32(arg), true
	case x86asm.Reg:
		full := fullReg(arg)
		if regKnown[full] {
			return regVal[full], true
		}
		return 0, false
	case x86asm.Mem:
		if arg.Index == 0 {
			argIdx := resolveArgIdx(arg, hasEBPFrame, espDelta)
			if argIdx > 0 && argIdx-1 < len(args) {
				return args[argIdx-1], true
			}
		}
		return 0, false
	}
	return 0, false
}

// applyMemberWrite maps a this+offset write to the appropriate DecodedMaterial
// field. The CMaxMaterial struct layout offsets are derived from the
// constructor instruction patterns in the XBE binary.
func applyMemberWrite(mat *DecodedMaterial, disp int64, value uint32, isByte bool) {
	switch disp {
	case 0x00:
		mat.VtableVA = value
	case 0x08:
		mat.ShaderCfg = value
	case 0x0C:
		if isByte {
			// D3DCOLOR 0xAARRGGBB on little-endian x86: byte at offset 0x0C = B (LSB)
			mat.B = uint8(value)
		} else if mat.Color1 == 0 {
			mat.Color1 = value
		}
	case 0x0D:
		mat.G = uint8(value)
	case 0x0E:
		// byte at offset 0x0E = R
		mat.R = uint8(value)
	case 0x0F:
		mat.A = uint8(value)
	case 0x10:
		mat.Color2 = value
	case 0x14:
		mat.TextureIndex = value
	case 0x220:
		mat.KeyMode1 = uint8(value)
	case 0x221:
		mat.KeyMode2 = uint8(value)
	}
}

// calleeCleanup returns the number of bytes a function cleans from the stack
// on return (from RET N instruction). Returns 0 for cdecl-style functions.
func calleeCleanup(fn *Function) int32 {
	for i := len(fn.Instructions) - 1; i >= 0; i-- {
		insn := fn.Instructions[i]
		if insn.Inst.Op == x86asm.RET && len(insn.Inst.Args) > 0 {
			if imm, ok := insn.Inst.Args[0].(x86asm.Imm); ok {
				return int32(imm)
			}
		}
	}
	return 0
}

// fullReg maps 8-bit and 16-bit registers to their 32-bit parent.
func fullReg(r x86asm.Reg) x86asm.Reg {
	switch r {
	case x86asm.AL, x86asm.AH, x86asm.AX:
		return x86asm.EAX
	case x86asm.BL, x86asm.BH, x86asm.BX:
		return x86asm.EBX
	case x86asm.CL, x86asm.CH, x86asm.CX:
		return x86asm.ECX
	case x86asm.DL, x86asm.DH, x86asm.DX:
		return x86asm.EDX
	case x86asm.SI:
		return x86asm.ESI
	case x86asm.DI:
		return x86asm.EDI
	default:
		return r
	}
}

// is8BitReg returns true if the register is an 8-bit register (AL, AH, etc.)
func is8BitReg(r x86asm.Reg) bool {
	switch r {
	case x86asm.AL, x86asm.AH, x86asm.BL, x86asm.BH,
		x86asm.CL, x86asm.CH, x86asm.DL, x86asm.DH:
		return true
	}
	return false
}

// TraceApplyMethods resolves each material's vtable and traces its Apply
// method to extract the D3D state programming directly from the binary.
func TraceApplyMethods(img *Image, d *Disassembly, materials []DecodedMaterial) {
	for i := range materials {
		mat := &materials[i]

		// Find the constructor function to determine the vtable pointer.
		ctorName := "CMaxMaterial::CMaxMaterial_" + mat.Type
		var ctorFn *Function
		for _, fn := range d.Functions {
			if fn.Name == ctorName {
				ctorFn = fn
				break
			}
		}
		if ctorFn == nil {
			continue
		}
		mat.CtorVA = ctorFn.EntryVA

		// Scan constructor instructions for MOV [reg+disp], imm32 patterns:
		//   disp=0x00: vtable pointer
		//   disp=0x0C: color1 (for reflect, which hardcodes this+0xC = 4)
		//   disp=0x14: textureIndex
		// Also follow CALL targets to parent constructors (e.g. eggpulse → combiner)
		// to extract fields set by the parent.
		scanCtorFields(d, ctorFn, mat)

		if mat.VtableVA == 0 {
			continue
		}

		// Read Apply method address from vtable slot 0.
		applyVA, ok := img.ReadU32(mat.VtableVA)
		if !ok {
			continue
		}
		mat.ApplyVA = applyVA

		// Find the Apply function in the disassembly.
		applyFn, ok := d.Functions[applyVA]
		if !ok {
			continue
		}

		// Trace the Apply function to extract D3D calls and immediate memory writes.
		mat.Apply, mat.InitWrites = traceApplyFunction(d, applyFn)
	}
}

// traceApplyFunction traces an Apply method's instructions to extract:
// 1. All CALL instructions with their preceding push arguments AND register values (D3D API calls)
// 2. All MOV [esi/ecx+offset], imm32 instructions (this->field = value)
//
// For SetTextureStageState_Deferred, the calling convention is:
//   ECX = texture stage number (0-3)
//   EDX = state type index (0-31, Xbox D3DTSS enum)
//   stack arg0 = value
// For SetRenderState wrappers (sub_0002989C):
//   stack args = (value, state_type) or (state_type) with value in register
func traceApplyFunction(d *Disassembly, fn *Function) ([]D3DCall, []MemWrite) {
	var calls []D3DCall
	var writes []MemWrite

	insns := fn.Instructions

	// Track register values forward through the function.
	// Values are int32 where -1 = unknown.
	regs := map[x86asm.Reg]int32{
		x86asm.ECX: -1,
		x86asm.EDX: -1,
		x86asm.EAX: -1,
		x86asm.ESI: -1,
		x86asm.EDI: -1,
		x86asm.EBX: -1,
	}

	for i := range insns {
		insn := &insns[i]

		// Track register assignments (forward pass).
		// IMPORTANT: CALL clobbering is handled AFTER extraction below,
		// so ECX/EDX are captured at their pre-call values.
		switch insn.Inst.Op {
		case x86asm.MOV:
			if len(insn.Inst.Args) >= 2 {
				if dst, ok := insn.Inst.Args[0].(x86asm.Reg); ok {
					full := fullReg(dst)
					if _, tracked := regs[full]; tracked {
						switch src := insn.Inst.Args[1].(type) {
						case x86asm.Imm:
							regs[full] = int32(src)
						case x86asm.Reg:
							if v, ok2 := regs[fullReg(src)]; ok2 {
								regs[full] = v
							} else {
								regs[full] = -1
							}
						default:
							regs[full] = -1
						}
					}
				}
			}
		case x86asm.XOR:
			if len(insn.Inst.Args) >= 2 {
				if dst, ok := insn.Inst.Args[0].(x86asm.Reg); ok {
					if src, ok := insn.Inst.Args[1].(x86asm.Reg); ok && dst == src {
						full := fullReg(dst)
						if _, tracked := regs[full]; tracked {
							regs[full] = 0
						}
					}
				}
			}
		case x86asm.POP:
			// POP reg — resolve from matching PUSH walking backward.
			if dst, ok := insn.Inst.Args[0].(x86asm.Reg); ok {
				full := fullReg(dst)
				if _, tracked := regs[full]; tracked {
					regs[full] = -1
					for j := i - 1; j >= 0; j-- {
						prev := insns[j]
						if prev.Inst.Op == x86asm.PUSH {
							if imm, ok := prev.Inst.Args[0].(x86asm.Imm); ok {
								regs[full] = int32(imm)
							} else if reg, ok := prev.Inst.Args[0].(x86asm.Reg); ok {
								if v, ok2 := regs[fullReg(reg)]; ok2 {
									regs[full] = v
								}
							}
							break
						}
						if prev.Inst.Op == x86asm.CALL || prev.Inst.Op == x86asm.RET {
							break
						}
					}
				}
			}
		case x86asm.INC:
			if len(insn.Inst.Args) > 0 {
				if dst, ok := insn.Inst.Args[0].(x86asm.Reg); ok {
					full := fullReg(dst)
					if v, ok2 := regs[full]; ok2 && v >= 0 {
						regs[full] = v + 1
					}
				}
			}
		case x86asm.DEC:
			if len(insn.Inst.Args) > 0 {
				if dst, ok := insn.Inst.Args[0].(x86asm.Reg); ok {
					full := fullReg(dst)
					if v, ok2 := regs[full]; ok2 && v >= 0 {
						regs[full] = v - 1
					}
				}
			}
		case x86asm.SHL:
			if len(insn.Inst.Args) >= 2 {
				if dst, ok := insn.Inst.Args[0].(x86asm.Reg); ok {
					if imm, ok := insn.Inst.Args[1].(x86asm.Imm); ok {
						if v, ok2 := regs[dst]; ok2 && v >= 0 {
							regs[dst] = v << uint(imm)
						}
					}
				}
			}
		case x86asm.ADD:
			if len(insn.Inst.Args) >= 2 {
				if dst, ok := insn.Inst.Args[0].(x86asm.Reg); ok {
					full := fullReg(dst)
					if imm, ok := insn.Inst.Args[1].(x86asm.Imm); ok {
						if v, ok2 := regs[full]; ok2 && v >= 0 {
							regs[full] = v + int32(imm)
						}
					}
					if src, ok := insn.Inst.Args[1].(x86asm.Reg); ok {
						if dv, ok2 := regs[full]; ok2 {
							if sv, ok3 := regs[fullReg(src)]; ok3 && dv >= 0 && sv >= 0 {
								regs[full] = dv + sv
							} else {
								regs[full] = -1
							}
						}
					}
				}
			}
		}

		// Extract MOV [this+offset], imm32 writes
		if insn.Inst.Op == x86asm.MOV && len(insn.Inst.Args) >= 2 {
			if mem, ok := insn.Inst.Args[0].(x86asm.Mem); ok {
				if imm, ok := insn.Inst.Args[1].(x86asm.Imm); ok {
					if mem.Base == x86asm.ESI || mem.Base == x86asm.ECX || mem.Base == x86asm.EDI || mem.Base == x86asm.EBX {
						writes = append(writes, MemWrite{
							Offset: int32(mem.Disp),
							Value:  uint32(imm),
						})
					}
				}
			}
		}

		// Extract OR reg, imm32
		if insn.Inst.Op == x86asm.OR && len(insn.Inst.Args) >= 2 {
			if _, ok := insn.Inst.Args[0].(x86asm.Reg); ok {
				if imm, ok := insn.Inst.Args[1].(x86asm.Imm); ok {
					writes = append(writes, MemWrite{
						Offset: -1,
						Value:  uint32(imm),
					})
				}
			}
		}

		// Extract CALL instructions with register state and stack args.
		// Register values are captured BEFORE clobbering.
		if insn.Inst.Op == x86asm.CALL {
			target := resolveTarget(&insns[i])

			fnName := fmt.Sprintf("sub_%08X", target)
			if target != 0 {
				if callee, ok := d.Functions[target]; ok && callee.Name != "" {
					fnName = callee.Name
				}
				if name, ok := d.Imports[target]; ok {
					fnName = name
				}
			}

			// Collect stack args by walking backward from the CALL.
			// Uses PUSH/POP balancing to skip register-load pairs
			// (PUSH imm; POP reg patterns used by the compiler to load registers).
			var args []uint32
			popBalance := 0 // unmatched POPs seen walking backward
			for j := i - 1; j >= 0; j-- {
				prev := insns[j]
				switch prev.Inst.Op {
				case x86asm.PUSH:
					if popBalance > 0 {
						// This PUSH was consumed by a later POP (register load)
						popBalance--
						continue
					}
					// Actual stack arg — resolve its value
					if imm, ok := prev.Inst.Args[0].(x86asm.Imm); ok {
						args = append([]uint32{uint32(imm)}, args...)
					} else if reg, ok := prev.Inst.Args[0].(x86asm.Reg); ok {
						if v, ok2 := regs[fullReg(reg)]; ok2 && v >= 0 {
							args = append([]uint32{uint32(v)}, args...)
						}
						// Unknown reg value — skip (don't include garbage)
					}
					// Unknown source (memory ref) — skip
				case x86asm.POP:
					popBalance++
				case x86asm.CALL, x86asm.RET:
					// Stop at previous call boundary
					goto argsDone
				default:
					// Skip register-setup instructions (MOV, XOR, INC, DEC,
					// ADD, SUB, SHL, LEA, CMP, TEST, JCC, etc.)
					continue
				}
			}
		argsDone:

			calls = append(calls, D3DCall{
				Function: fnName,
				TargetVA: target,
				Args:     args,
				ECX:      regs[x86asm.ECX],
				EDX:      regs[x86asm.EDX],
			})

			// Clobber caller-saved registers AFTER capture
			regs[x86asm.EAX] = -1
			regs[x86asm.ECX] = -1
			regs[x86asm.EDX] = -1
		}
	}

	return calls, writes
}

// textureIndexFiles maps material texture indices to their extracted filenames.
// Derived from the XBE's texture loading code: LookupTexture(this+0x14) binds
// the texture at the given index. Index 1 is a GPU-generated gradient (no file).
var textureIndexFiles = map[uint32]string{
	2: "steel.png",
	3: "cellwall.png",
}

// ResolveD3DState processes a material's Apply trace to extract the D3D render
// state configuration. It decodes SetRenderState calls (sub_0002989C in the
// original trace) using the Xbox D3D8 enum values, and detects combiner vs
// texture-factor rendering paths.
//
// The calling convention for SetRenderState wrapper: args[-1]=State, args[-2]=Value
// (reversed push order in the trace).
func ResolveD3DState(mat *DecodedMaterial) {
	state := &D3DState{}

	for _, call := range mat.Apply {
		switch {
		case strings.Contains(call.Function, "SetRenderState"):
			if len(call.Args) >= 2 {
				// args = [Value, State] due to reversed push collection
				value := call.Args[0]
				stateID := call.Args[len(call.Args)-1]
				resolveRenderState(state, stateID, value)
			} else if len(call.Args) == 1 {
				// Single-arg: the resolved push is the state ID (immediate),
				// the value was in an unresolved register.
				stateID := call.Args[0]
				if name, ok := D3DRenderStates[stateID]; ok {
					if name == "X_D3DRS_TEXTUREFACTOR" {
						state.UsesTextureFactor = true
					}
				}
			}

		case call.Function == "SetRegisterCombinerConstants":
			state.UsesCombiner = true

		case call.Function == "LookupTexture":
			// Texture binding — textureIndex already extracted from constructor
		}
	}

	// Resolve texture file from index
	if f, ok := textureIndexFiles[mat.TextureIndex]; ok {
		mat.TextureFile = f
	}

	// Resolve blend factor names
	if name, ok := D3DBlendFactors[state.SrcBlend]; ok {
		state.SrcBlendName = name
	}
	if name, ok := D3DBlendFactors[state.DstBlend]; ok {
		state.DstBlendName = name
	}

	mat.D3DState = state
}

// resolveRenderState maps a single SetRenderState(stateID, value) call
// to the D3DState struct fields.
func resolveRenderState(state *D3DState, stateID, value uint32) {
	name := D3DRenderStates[stateID]
	switch name {
	case "X_D3DRS_ALPHABLENDENABLE":
		state.AlphaBlendEnable = value != 0
	case "X_D3DRS_SRCBLEND":
		state.SrcBlend = value
	case "X_D3DRS_DESTBLEND":
		state.DstBlend = value
	case "X_D3DRS_ZWRITEENABLE":
		v := value != 0
		state.ZWriteEnable = &v
	case "X_D3DRS_TEXTUREFACTOR":
		state.UsesTextureFactor = true
	}
}

// ExtractMaterialsFromXBE is the high-level entry point: load XBE,
// decompile it, parse CreateAllMaterials, trace Apply methods.
func ExtractMaterialsFromXBE(xbePath string) ([]DecodedMaterial, error) {
	img, err := Open(xbePath)
	if err != nil {
		return nil, fmt.Errorf("xbe: %w", err)
	}

	d, err := Disassemble(img)
	if err != nil {
		return nil, fmt.Errorf("xbe disassemble: %w", err)
	}

	// Decompile all functions to get pseudocode for CreateAllMaterials parsing
	funcs := d.DecompileAll()
	var sb strings.Builder
	for _, df := range funcs {
		sb.WriteString(df.Format())
		sb.WriteString("\n")
	}

	// Step 1: Parse constructor calls from CreateAllMaterials
	materials := ParseMaterialsFromDecompiled(sb.String())
	if len(materials) == 0 {
		return nil, fmt.Errorf("xbe: no materials found in decompiled output")
	}

	// Step 2: Trace each material's Apply method from the binary
	TraceApplyMethods(img, d, materials)

	// Step 3: Resolve D3D state from Apply traces
	for i := range materials {
		ResolveD3DState(&materials[i])
	}

	return materials, nil
}
