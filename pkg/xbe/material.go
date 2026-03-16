package xbe

import (
	"fmt"
	"strings"
)

// Material represents a decoded CMaxMaterial from the XBE.
type Material struct {
	Name      string   // UTF-16 material name (e.g. "FlatSrfc/PodParts")
	CtorType  uint32   // constructor address (identifies material class variant)
	Color1    uint32   // primary color/combiner constant (D3DCOLOR ARGB)
	Color2    uint32   // secondary color/combiner constant
	ShaderCfg uint32   // shader/combiner configuration value
	StackArgs []uint32 // raw immediate args pushed before name
	RegEDI    uint32   // EDI at point of creation
	RegEBX    uint32   // EBX at point of creation
	RegESI    uint32   // ESI at point of creation
	RegEBP    uint32   // EBP at point of creation
}

// ARGB extracts A, R, G, B components from a D3DCOLOR uint32.
func ARGB(color uint32) (a, r, g, b uint8) {
	return uint8(color >> 24), uint8(color >> 16), uint8(color >> 8), uint8(color)
}

// ExtractMaterials finds and decodes all CMaxMaterial instances from an XBE.
//
// The algorithm:
//  1. Locate the material name string table (UTF-16LE strings in .text section)
//  2. Find the function that creates all materials by searching for PUSH <name_va>
//     patterns referencing those strings
//  3. Trace the function, tracking register state at each material creation point
//  4. Decode constructor arguments to extract color/shader data
func ExtractMaterials(img *Image) ([]Material, error) {
	text := img.FindSection(".text")
	if text == nil {
		return nil, fmt.Errorf("xbe: no .text section found")
	}

	// Step 1: Find the "CMaxMaterial" UTF-16 string to locate the name table region.
	cmmVA := findUTF16String(img, text, "CMaxMaterial")
	if cmmVA == 0 {
		return nil, fmt.Errorf("xbe: CMaxMaterial string not found")
	}

	// Step 2: Find the material name string table boundaries.
	// The names are UTF-16LE strings that start ~0x100 bytes after CMaxMaterial.
	// There's a gap of vtable/code data between the class name and the material names.
	// Use a wide search window and validate strings by checking they're referenced
	// by PUSH imm32 instructions in the code.
	nameStart, nameEnd := findNameTableByReference(img, text, cmmVA)
	if nameStart == 0 {
		return nil, fmt.Errorf("xbe: material name table not found")
	}

	// Step 3: Find the material creation function by looking for code that
	// references material name VAs via PUSH imm32.
	funcStart, funcEnd := findMaterialCreationFunc(img, text, nameStart, nameEnd)
	if funcStart == 0 {
		return nil, fmt.Errorf("xbe: material creation function not found")
	}

	// Step 4: Trace the function and extract material data.
	creations := TraceMaterials(img, funcStart, funcEnd, nameStart, nameEnd)

	var materials []Material
	for _, mc := range creations {
		mat := Material{
			Name:      mc.Name,
			CtorType:  mc.CtorAddr,
			RegEDI:    mc.Regs.EDI,
			RegEBX:    mc.Regs.EBX,
			RegESI:    mc.Regs.ESI,
			RegEBP:    mc.Regs.EBP,
			StackArgs: mc.StackArgs,
		}

		// Decode based on constructor type.
		// The constructor at the most common address stores:
		//   this+0x0C = EDI (primary color as D3DCOLOR)
		//   this+0x10 = EBX (secondary color/combiner)
		//   this+0x08 = ESI (shader configuration)
		mat.Color1 = mc.Regs.EDI
		mat.Color2 = mc.Regs.EBX
		mat.ShaderCfg = mc.Regs.ESI

		materials = append(materials, mat)
	}

	return materials, nil
}

// findUTF16String searches for a UTF-16LE encoded string in a section.
// Returns the VA where the string starts, or 0 if not found.
func findUTF16String(img *Image, sec *Section, target string) uint32 {
	// Encode target as UTF-16LE
	encoded := make([]byte, len(target)*2+2)
	for i, ch := range target {
		encoded[i*2] = byte(ch)
		encoded[i*2+1] = 0
	}
	// Null terminator
	encoded[len(target)*2] = 0
	encoded[len(target)*2+1] = 0

	// Search section data
	searchBytes := encoded[:len(target)*2] // don't require null term in search
	for i := 0; i <= len(sec.Data)-len(searchBytes); i++ {
		match := true
		for j, b := range searchBytes {
			if sec.Data[i+j] != b {
				match = false
				break
			}
		}
		if match {
			return sec.VirtualAddr + uint32(i)
		}
	}
	return 0
}

// findNameTableBounds finds the start and end VA of the material name string table.
// Material names are consecutive UTF-16LE null-terminated strings in the .text section.
// They begin after the CMaxMaterial class name string and span ~2.5KB.
func findNameTableBounds(img *Image, cmmVA uint32, text *Section) (uint32, uint32) {
	textEnd := text.VirtualAddr + text.VirtualSize

	// The material names start AFTER CMaxMaterial and its vtable pointers.
	// Skip past CMaxMaterial string + null + vtable data to find the first
	// material name. Scan forward looking for a sequence of valid UTF-16 strings.
	searchStart := cmmVA

	// Find the first valid material-like string after CMaxMaterial.
	// Material names are >2 chars, ASCII-printable, often containing
	// uppercase letters, slashes, or numbers.
	start := uint32(0)
	va := searchStart
	for va < textEnd-4 && va < searchStart+0x400 {
		ch, ok := img.ReadU16(va)
		if !ok {
			va += 2
			continue
		}
		// Look for start of a UTF-16 string (printable ASCII char)
		if ch >= 0x20 && ch < 0x7F {
			s := img.ReadUTF16(va)
			// Material names are at least 3 chars and contain letters
			if len(s) >= 3 && containsLetter(s) {
				if start == 0 {
					start = va
				}
			}
			// Skip past this string + null terminator
			va += uint32(len(s)*2) + 2
			continue
		}
		va += 2
	}

	if start == 0 {
		start = cmmVA
	}

	// Now scan forward from start to find all material name strings.
	// The table ends when we hit non-string data (code or other structures).
	end := start
	va = start
	consecutiveNonString := 0
	for va < textEnd-4 && va < start+0x2000 {
		ch, ok := img.ReadU16(va)
		if !ok {
			break
		}
		if ch >= 0x20 && ch < 0x7F {
			s := img.ReadUTF16(va)
			if len(s) >= 2 {
				end = va + uint32(len(s)*2) + 2
				consecutiveNonString = 0
			}
			va += uint32(len(s)*2) + 2
			continue
		}
		if ch == 0 {
			va += 2
			consecutiveNonString++
			// Allow small gaps between strings (alignment padding)
			if consecutiveNonString > 4 {
				break
			}
			continue
		}
		// Non-string, non-null data
		consecutiveNonString++
		if consecutiveNonString > 6 {
			break
		}
		va += 2
	}

	return start, end
}

// findNameTableByReference finds the material name table by scanning the .text
// section for UTF-16 strings near CMaxMaterial that are referenced by PUSH imm32
// instructions. This avoids the problem of non-string data gaps in the string table.
func findNameTableByReference(img *Image, text *Section, cmmVA uint32) (uint32, uint32) {
	textStart := text.VirtualAddr
	textEnd := text.VirtualAddr + text.VirtualSize

	// Scan a 4KB region after CMaxMaterial for UTF-16 strings.
	var candidates []struct {
		va   uint32
		name string
	}

	searchStart := cmmVA
	searchEnd := cmmVA + 0x1000
	if searchEnd > textEnd {
		searchEnd = textEnd
	}

	va := searchStart
	for va < searchEnd-4 {
		ch, ok := img.ReadU16(va)
		if !ok {
			va += 2
			continue
		}
		if ch >= 0x20 && ch < 0x7F {
			s := img.ReadUTF16(va)
			if len(s) >= 3 && containsLetter(s) {
				candidates = append(candidates, struct {
					va   uint32
					name string
				}{va, s})
			}
			va += uint32(len(s)*2) + 2
			continue
		}
		va += 2
	}

	if len(candidates) == 0 {
		return 0, 0
	}

	// Now verify which candidates are actually referenced by PUSH imm32 in code.
	referenced := make(map[uint32]bool)
	for codeVA := textStart; codeVA < textEnd-5; codeVA++ {
		b, ok := img.ReadU8(codeVA)
		if !ok {
			continue
		}
		if b == 0x68 { // PUSH imm32
			imm, ok := img.ReadU32(codeVA + 1)
			if !ok {
				continue
			}
			referenced[imm] = true
		}
	}

	// Filter candidates to only those referenced by PUSH instructions.
	var minVA, maxVA uint32
	count := 0
	for _, c := range candidates {
		if referenced[c.va] {
			if count == 0 || c.va < minVA {
				minVA = c.va
			}
			endVA := c.va + uint32(len(c.name)*2) + 2
			if endVA > maxVA {
				maxVA = endVA
			}
			count++
		}
	}

	if count < 5 {
		return 0, 0
	}

	return minVA, maxVA
}

func containsLetter(s string) bool {
	for _, ch := range s {
		if (ch >= 'A' && ch <= 'Z') || (ch >= 'a' && ch <= 'z') {
			return true
		}
	}
	return false
}

// findMaterialCreationFunc locates the function that creates all materials.
// It searches for the densest cluster of PUSH instructions referencing
// material name VAs.
func findMaterialCreationFunc(img *Image, text *Section, nameStart, nameEnd uint32) (uint32, uint32) {
	textStart := text.VirtualAddr
	textEnd := text.VirtualAddr + text.VirtualSize

	// Find all PUSH imm32 instructions where the immediate is a name VA.
	type pushRef struct {
		va  uint32
		imm uint32
	}
	var refs []pushRef

	for va := textStart; va < textEnd-5; va++ {
		b, ok := img.ReadU8(va)
		if !ok {
			continue
		}
		if b == 0x68 { // PUSH imm32
			imm, ok := img.ReadU32(va + 1)
			if !ok {
				continue
			}
			if imm >= nameStart && imm < nameEnd {
				// Verify it's actually pointing to a valid string
				name := img.ReadUTF16(imm)
				if len(name) > 1 && !strings.ContainsRune(name, 0xFFFD) {
					refs = append(refs, pushRef{va, imm})
				}
			}
		}
	}

	if len(refs) < 5 {
		return 0, 0
	}

	// Find the tightest cluster of references — that's our creation function.
	// Use a sliding window to find the region with most references.
	bestStart := refs[0].va
	bestEnd := refs[len(refs)-1].va + 5
	bestCount := 0

	windowSize := uint32(0x2000) // 8KB window
	for _, ref := range refs {
		winStart := ref.va
		winEnd := winStart + windowSize
		count := 0
		for _, r := range refs {
			if r.va >= winStart && r.va < winEnd {
				count++
			}
		}
		if count > bestCount {
			bestCount = count
			bestStart = winStart
			bestEnd = winEnd
		}
	}

	// Expand slightly to include the function prologue/epilogue.
	// Scan backward from bestStart to find function prologue (PUSH EBP; MOV EBP,ESP or similar).
	for va := bestStart; va > bestStart-0x200 && va > textStart; va-- {
		b0, _ := img.ReadU8(va)
		b1, _ := img.ReadU8(va + 1)
		b2, _ := img.ReadU8(va + 2)
		// Common function prologues
		if (b0 == 0x55 && b1 == 0x8B && b2 == 0xEC) || // push ebp; mov ebp, esp
			(b0 == 0x55 && b1 == 0x56 && b2 == 0x57) { // push ebp; push esi; push edi
			bestStart = va
			break
		}
		// Also check for push esi; push edi; push imm8 (common in this function)
		if b0 == 0x55 && b1 == 0x56 {
			bestStart = va
			break
		}
	}

	return bestStart, bestEnd
}
