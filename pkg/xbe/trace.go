package xbe

// x86 register/operand tracer for extracting material construction arguments.
//
// This is NOT a full x86 disassembler. It traces a limited subset of
// instructions that appear in the CMaxMaterial creation function:
//   - MOV reg, imm32   (register initialization)
//   - XOR reg, reg     (register zeroing)
//   - PUSH imm8/imm32  (stack arguments)
//   - PUSH reg         (stack arguments from registers)
//   - CALL rel32       (constructor calls)
//   - PUSH <name VA>   (material name string reference)

// RegState tracks the current values of general-purpose registers.
type RegState struct {
	EAX, ECX, EDX, EBX uint32
	ESP, EBP, ESI, EDI uint32
}

// TracedCall represents a function call with its resolved arguments.
type TracedCall struct {
	CallAddr   uint32   // address of the CALL instruction
	TargetAddr uint32   // resolved call target
	Args       []uint32 // arguments in stack order (first pushed = last in slice)
}

// TraceBlock traces x86 code from startVA to endVA, tracking register
// state and recording CALL instructions with their stack arguments.
// Returns all calls found and the final register state.
func TraceBlock(img *Image, startVA, endVA uint32) ([]TracedCall, RegState) {
	var regs RegState
	var calls []TracedCall

	va := startVA
	for va < endVA {
		b, ok := img.ReadU8(va)
		if !ok {
			va++
			continue
		}

		switch {
		// MOV r32, imm32: B8+rd id
		case b >= 0xB8 && b <= 0xBF:
			imm, ok := img.ReadU32(va + 1)
			if !ok {
				va += 5
				continue
			}
			setReg(&regs, int(b-0xB8), imm)
			va += 5

		// XOR r32, r32: 33 /r (mod=11, same src/dst = zeroing)
		case b == 0x33:
			modrm, ok := img.ReadU8(va + 1)
			if !ok {
				va += 2
				continue
			}
			if modrm&0xC0 == 0xC0 {
				src := int(modrm & 7)
				dst := int((modrm >> 3) & 7)
				if src == dst {
					setReg(&regs, dst, 0)
				}
			}
			va += 2

		// PUSH imm8: 6A ib
		case b == 0x6A:
			ib, ok := img.ReadU8(va + 1)
			if !ok {
				va += 2
				continue
			}
			// Sign-extend to 32-bit
			var val uint32
			if ib > 127 {
				val = uint32(ib) | 0xFFFFFF00
			} else {
				val = uint32(ib)
			}
			_ = val // will be captured by subsequent CALL scanning
			va += 2

		// PUSH imm32: 68 id
		case b == 0x68:
			_, ok := img.ReadU32(va + 1)
			if !ok {
				va += 5
				continue
			}
			va += 5

		// PUSH r32: 50+rd
		case b >= 0x50 && b <= 0x57:
			va++

		// CALL rel32: E8 cd
		case b == 0xE8:
			rel, ok := img.ReadU32(va + 1)
			if !ok {
				va += 5
				continue
			}
			target := va + 5 + rel
			calls = append(calls, TracedCall{
				CallAddr:   va,
				TargetAddr: target,
			})
			va += 5

		default:
			va++
		}
	}

	return calls, regs
}

// TraceMaterialCreation traces the material creation function and extracts
// each material name → constructor call with register state at point of call.
//
// It scans for patterns of:
//   PUSH <material_name_VA>  (UTF-16 string in .text section)
//   ... (possible MOV ECX, ...)
//   CALL <constructor>
//
// Returns a map from material name to the register state at time of creation.
type MaterialCreation struct {
	Name       string
	CtorAddr   uint32   // constructor called
	Regs       RegState // register state at time of PUSH name
	StackArgs  []uint32 // immediate values pushed before the name
}

// TraceMaterials scans a VA range for material creation patterns.
// nameRangeStart/End define the VA range where UTF-16 material name strings live.
func TraceMaterials(img *Image, startVA, endVA, nameRangeStart, nameRangeEnd uint32) []MaterialCreation {
	var results []MaterialCreation
	var regs RegState

	va := startVA
	for va < endVA {
		b, ok := img.ReadU8(va)
		if !ok {
			va++
			continue
		}

		switch {
		// MOV r32, imm32
		case b >= 0xB8 && b <= 0xBF:
			imm, _ := img.ReadU32(va + 1)
			setReg(&regs, int(b-0xB8), imm)
			va += 5

		// XOR r32, r32 (zeroing)
		case b == 0x33:
			modrm, _ := img.ReadU8(va + 1)
			if modrm&0xC0 == 0xC0 {
				src := int(modrm & 7)
				dst := int((modrm >> 3) & 7)
				if src == dst {
					setReg(&regs, dst, 0)
				}
			}
			va += 2

		// PUSH imm32 — check if it's a material name VA
		case b == 0x68:
			imm, _ := img.ReadU32(va + 1)

			if imm >= nameRangeStart && imm < nameRangeEnd {
				// This is a material name push. Read the name.
				name := img.ReadUTF16(imm)
				if len(name) > 1 {
					// Collect stack args pushed before this name
					stackArgs := collectPrecedingPushes(img, va, startVA)

					// Find the CALL after this push (within ~20 bytes)
					ctorAddr := findNextCall(img, va+5, va+25)

					results = append(results, MaterialCreation{
						Name:      name,
						CtorAddr:  ctorAddr,
						Regs:      regs,
						StackArgs: stackArgs,
					})
				}
			}
			va += 5

		// PUSH r32
		case b >= 0x50 && b <= 0x57:
			va++

		// PUSH imm8
		case b == 0x6A:
			va += 2

		// CALL rel32
		case b == 0xE8:
			va += 5

		// Skip other instructions
		default:
			va += decodeInsnLength(img, va)
		}
	}

	return results
}

// collectPrecedingPushes walks backwards from pushAddr to collect
// immediate values pushed onto the stack before the material name push.
func collectPrecedingPushes(img *Image, pushAddr, limitAddr uint32) []uint32 {
	var args []uint32
	va := pushAddr - 1

	for va >= limitAddr && len(args) < 10 {
		b, ok := img.ReadU8(va)
		if !ok {
			break
		}

		switch {
		case b == 0x6A: // PUSH imm8
			ib, _ := img.ReadU8(va + 1)
			args = append([]uint32{uint32(ib)}, args...)
			va--

		case b == 0x68: // PUSH imm32
			// Need to check if 4 bytes before this are the immediate
			if va >= 4 {
				imm, _ := img.ReadU32(va + 1)
				args = append([]uint32{imm}, args...)
				va -= 4
			} else {
				return args
			}

		// PUSH reg: single byte
		case b >= 0x50 && b <= 0x57:
			// We can't know the register value at this point in backward scan.
			// Mark with a sentinel.
			args = append([]uint32{0xDEAD0000 | uint32(b-0x50)}, args...)
			va--

		default:
			// Not a push — stop scanning
			return args
		}
	}

	return args
}

// findNextCall scans forward from va for a CALL rel32 instruction.
func findNextCall(img *Image, startVA, endVA uint32) uint32 {
	for va := startVA; va < endVA; va++ {
		b, ok := img.ReadU8(va)
		if !ok {
			continue
		}
		if b == 0xE8 {
			rel, ok := img.ReadU32(va + 1)
			if ok {
				return va + 5 + rel
			}
		}
	}
	return 0
}

// setReg sets a register value by index (0=EAX .. 7=EDI).
func setReg(regs *RegState, idx int, val uint32) {
	switch idx {
	case 0:
		regs.EAX = val
	case 1:
		regs.ECX = val
	case 2:
		regs.EDX = val
	case 3:
		regs.EBX = val
	case 4:
		regs.ESP = val
	case 5:
		regs.EBP = val
	case 6:
		regs.ESI = val
	case 7:
		regs.EDI = val
	}
}

// getReg gets a register value by index.
func getReg(regs *RegState, idx int) uint32 {
	switch idx {
	case 0:
		return regs.EAX
	case 1:
		return regs.ECX
	case 2:
		return regs.EDX
	case 3:
		return regs.EBX
	case 4:
		return regs.ESP
	case 5:
		return regs.EBP
	case 6:
		return regs.ESI
	case 7:
		return regs.EDI
	}
	return 0
}

// decodeInsnLength returns a conservative instruction length estimate.
// This handles the most common x86 encodings seen in Xbox dashboard code.
func decodeInsnLength(img *Image, va uint32) uint32 {
	b, ok := img.ReadU8(va)
	if !ok {
		return 1
	}

	switch {
	// Single-byte instructions
	case b == 0x90: // NOP
		return 1
	case b == 0xC3: // RET
		return 1
	case b == 0xC9: // LEAVE
		return 1
	case b >= 0x50 && b <= 0x5F: // PUSH/POP r32
		return 1

	// Two-byte instructions
	case b == 0x33: // XOR r, r/m
		return 2
	case b == 0x3B: // CMP r, r/m
		return 2
	case b == 0x8B: // MOV r, r/m
		return modrm32Length(img, va+1) + 1
	case b == 0x89: // MOV r/m, r
		return modrm32Length(img, va+1) + 1
	case b == 0x85: // TEST r, r/m
		return 2
	case b == 0x32: // XOR r8, r/m8
		return 2

	// Immediate instructions
	case b == 0x6A: // PUSH imm8
		return 2
	case b == 0x68: // PUSH imm32
		return 5
	case b >= 0xB0 && b <= 0xB7: // MOV r8, imm8
		return 2
	case b >= 0xB8 && b <= 0xBF: // MOV r32, imm32
		return 5
	case b == 0xE8: // CALL rel32
		return 5
	case b == 0xE9: // JMP rel32
		return 5
	case b == 0xEB: // JMP rel8
		return 2

	// Conditional jumps (short)
	case b >= 0x70 && b <= 0x7F:
		return 2

	// MOV r/m, imm
	case b == 0xC7:
		return modrm32Length(img, va+1) + 1 + 4

	// RET imm16
	case b == 0xC2:
		return 3

	// Two-byte opcode prefix
	case b == 0x0F:
		b2, _ := img.ReadU8(va + 1)
		if b2 >= 0x80 && b2 <= 0x8F { // Jcc rel32
			return 6
		}
		return 2

	// MOV r8, [r/m]
	case b == 0x8A:
		return modrm32Length(img, va+1) + 1
	case b == 0x88: // MOV [r/m], r8
		return modrm32Length(img, va+1) + 1

	// LEA
	case b == 0x8D:
		return modrm32Length(img, va+1) + 1

	// FF group (CALL/JMP indirect, PUSH m, INC/DEC)
	case b == 0xFF:
		return modrm32Length(img, va+1) + 1

	default:
		return 1
	}
}

// modrm32Length returns the additional bytes consumed by a ModR/M byte
// and its SIB/displacement in 32-bit mode.
func modrm32Length(img *Image, va uint32) uint32 {
	modrm, ok := img.ReadU8(va)
	if !ok {
		return 1
	}

	mod := (modrm >> 6) & 3
	rm := modrm & 7

	length := uint32(1) // the modrm byte itself

	switch mod {
	case 0:
		if rm == 4 {
			length++ // SIB byte
			sib, _ := img.ReadU8(va + 1)
			if sib&7 == 5 {
				length += 4 // disp32
			}
		} else if rm == 5 {
			length += 4 // disp32
		}
	case 1:
		if rm == 4 {
			length++ // SIB
		}
		length++ // disp8
	case 2:
		if rm == 4 {
			length++ // SIB
		}
		length += 4 // disp32
	case 3:
		// register direct, no extra bytes
	}

	return length
}
