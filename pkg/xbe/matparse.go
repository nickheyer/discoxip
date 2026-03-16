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
	Name       string       `json:"name"`
	Type       string       `json:"type"`                // constructor variant name
	R          uint8        `json:"r"`                   // resolved diffuse red (0-255)
	G          uint8        `json:"g"`                   // resolved diffuse green (0-255)
	B          uint8        `json:"b"`                   // resolved diffuse blue (0-255)
	A          uint8        `json:"a"`                   // resolved alpha (0-255)
	Color1     uint32       `json:"color1"`              // primary packed ARGB (from ctor or Apply writes)
	Color2     uint32       `json:"color2"`              // secondary packed ARGB
	CtorVA     uint32       `json:"ctor_va"`             // constructor address
	VtableVA   uint32       `json:"vtable_va"`           // vtable pointer set by constructor
	ApplyVA    uint32       `json:"apply_va"`            // Apply method address (vtable slot 0)
	CtorArgs   []uint32     `json:"ctor_args,omitempty"` // raw constructor arguments
	Apply      []D3DCall    `json:"apply"`               // D3D calls made by Apply method
	InitWrites []MemWrite   `json:"init_writes,omitempty"` // immediate writes to this+offset in Apply
}

// D3DCall is a D3D API call extracted from the Apply method.
type D3DCall struct {
	Function string   `json:"fn"`             // resolved function name
	TargetVA uint32   `json:"target_va"`      // call target address
	Args     []uint32 `json:"args,omitempty"` // immediate arguments (from preceding pushes)
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

		// The constructor writes the vtable pointer via MOV [reg], imm32.
		// Scan constructor instructions for this pattern.
		for _, insn := range ctorFn.Instructions {
			if insn.Inst.Op == x86asm.MOV && len(insn.Inst.Args) >= 2 {
				if mem, ok := insn.Inst.Args[0].(x86asm.Mem); ok {
					if imm, ok := insn.Inst.Args[1].(x86asm.Imm); ok {
						// MOV [reg], imm32 where reg is this pointer and disp is 0
						if mem.Disp == 0 && (mem.Base == x86asm.ESI || mem.Base == x86asm.EDX || mem.Base == x86asm.EAX || mem.Base == x86asm.ECX) {
							mat.VtableVA = uint32(imm)
						}
					}
				}
			}
		}

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
// 1. All CALL instructions with their preceding push arguments (D3D API calls)
// 2. All MOV [esi/ecx+offset], imm32 instructions (this->field = value)
func traceApplyFunction(d *Disassembly, fn *Function) ([]D3DCall, []MemWrite) {
	var calls []D3DCall
	var writes []MemWrite

	insns := fn.Instructions

	for i, insn := range insns {
		// Extract MOV [this+offset], imm32 writes
		if insn.Inst.Op == x86asm.MOV && len(insn.Inst.Args) >= 2 {
			if mem, ok := insn.Inst.Args[0].(x86asm.Mem); ok {
				if imm, ok := insn.Inst.Args[1].(x86asm.Imm); ok {
					// Writing an immediate to a this-relative offset
					if mem.Base == x86asm.ESI || mem.Base == x86asm.ECX || mem.Base == x86asm.EDI || mem.Base == x86asm.EBX {
						writes = append(writes, MemWrite{
							Offset: int32(mem.Disp),
							Value:  uint32(imm),
						})
					}
				}
			}
		}

		// Extract OR [this+offset], imm32 (used to set color components)
		if insn.Inst.Op == x86asm.OR && len(insn.Inst.Args) >= 2 {
			if _, ok := insn.Inst.Args[0].(x86asm.Reg); ok {
				if imm, ok := insn.Inst.Args[1].(x86asm.Imm); ok {
					// OR eax, imm32 — often used to combine alpha with color
					writes = append(writes, MemWrite{
						Offset: -1, // register, not memory
						Value:  uint32(imm),
					})
				}
			}
		}

		// Extract CALL instructions with preceding push arguments
		if insn.Inst.Op == x86asm.CALL {
			target := resolveTarget(&insns[i])

			// Resolve function name
			fnName := fmt.Sprintf("sub_%08X", target)
			if target != 0 {
				if callee, ok := d.Functions[target]; ok && callee.Name != "" {
					fnName = callee.Name
				}
				if name, ok := d.Imports[target]; ok {
					fnName = name
				}
			}

			// Collect immediate push arguments before this call
			var args []uint32
			for j := i - 1; j >= 0; j-- {
				prev := insns[j]
				if prev.Inst.Op == x86asm.PUSH {
					if imm, ok := prev.Inst.Args[0].(x86asm.Imm); ok {
						args = append([]uint32{uint32(imm)}, args...)
						continue
					}
				}
				// Stop at non-push instructions (but skip POP used for mov edx, pop pattern)
				if prev.Inst.Op != x86asm.POP {
					break
				}
			}

			calls = append(calls, D3DCall{
				Function: fnName,
				TargetVA: target,
				Args:     args,
			})
		}
	}

	return calls, writes
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

	return materials, nil
}
