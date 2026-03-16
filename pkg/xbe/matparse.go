package xbe

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// DecodedMaterial is a material extracted from the decompiled CreateAllMaterials function.
type DecodedMaterial struct {
	Name      string    `json:"name"`
	Type      string    `json:"type"`                 // constructor variant: flatcolor, combiner, base, etc.
	R         uint8     `json:"r"`
	G         uint8     `json:"g"`
	B         uint8     `json:"b"`
	A         uint8     `json:"a"`
	Color1Raw uint32    `json:"color1_raw,omitempty"`  // packed ARGB for combiner types
	Color2Raw uint32    `json:"color2_raw,omitempty"`  // secondary combiner color
	ShaderCfg uint32    `json:"shader_cfg,omitempty"`  // shader/blend config
	Args      []uint32  `json:"args,omitempty"`        // raw constructor args for unhandled types
}

// ParseMaterialsFromDecompiled extracts all materials from the decompiled
// CreateAllMaterials() function text. This parses the pseudocode output
// from the decompiler rather than re-analyzing raw binary.
func ParseMaterialsFromDecompiled(decompiled string) []DecodedMaterial {
	// Find CreateAllMaterials function
	startMarker := "CMaxMaterial::CreateAllMaterials() {"
	startIdx := strings.Index(decompiled, startMarker)
	if startIdx < 0 {
		return nil
	}

	// Find the end (next function definition)
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

		// Track register assignments
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

		// Look for constructor calls
		cm := ctorRe.FindStringSubmatch(line)
		if cm == nil {
			continue
		}
		ctorType := cm[1]

		// Walk backwards to collect push arguments
		var pushes []uint32 // values in push order (furthest from name = index 0)
		var name string

		for j := i - 1; j >= 0 && j > i-20; j-- {
			pl := strings.TrimSpace(lines[j])

			// push("name")
			if nm := nameRe.FindStringSubmatch(pl); nm != nil {
				name = nm[1]
				continue
			}

			// push(imm decimal)
			if pm := immRe.FindStringSubmatch(pl); pm != nil {
				v, _ := strconv.ParseUint(pm[1], 10, 64)
				pushes = append([]uint32{uint32(v)}, pushes...)
				continue
			}

			// push(0xHEX)
			if pm := hexRe.FindStringSubmatch(pl); pm != nil {
				v, _ := strconv.ParseUint(pm[1], 16, 64)
				pushes = append([]uint32{uint32(v)}, pushes...)
				continue
			}

			// push(register)
			if pm := regPushRe.FindStringSubmatch(pl); pm != nil {
				regName := pm[1]
				if val, ok := regs[regName]; ok {
					pushes = append([]uint32{val}, pushes...)
					continue
				}
			}

			// ecx = eax; (skip)
			if strings.Contains(pl, "ecx = eax") {
				continue
			}

			// edx = N; (register assign before push(edx))
			if m := regAssignRe.FindStringSubmatch(pl); m != nil {
				if _, ok := regs[m[1]]; ok {
					v, _ := strconv.ParseUint(m[2], 10, 32)
					regs[m[1]] = uint32(v)
				}
				continue
			}

			// Stop at anything else
			break
		}

		if name == "" {
			continue
		}

		mat := DecodedMaterial{
			Name: name,
			Type: ctorType,
			Args: pushes,
		}

		// Decode by constructor type
		switch ctorType {
		case "flatcolor":
			// pushes order: [shader_cfg, A, B, G, R] (furthest from name first)
			if len(pushes) >= 5 {
				mat.R = uint8(pushes[4])
				mat.G = uint8(pushes[3])
				mat.B = uint8(pushes[2])
				mat.A = uint8(pushes[1])
				mat.ShaderCfg = pushes[0]
			}

		case "combiner":
			// pushes order: [shader_cfg, color2, color1] (furthest first)
			if len(pushes) >= 3 {
				color1 := pushes[2]
				color2 := pushes[1]
				mat.A = uint8(color1 >> 24)
				mat.R = uint8(color1 >> 16)
				mat.G = uint8(color1 >> 8)
				mat.B = uint8(color1)
				mat.Color1Raw = color1
				mat.Color2Raw = color2
				mat.ShaderCfg = pushes[0]
			}

		case "base":
			// Base materials have shader_cfg only, no color
			if len(pushes) >= 1 {
				mat.ShaderCfg = pushes[0]
			}
			mat.R = 255
			mat.G = 255
			mat.B = 255
			mat.A = 255

		case "innerwall":
			// Same as combiner layout
			if len(pushes) >= 3 {
				color1 := pushes[2]
				mat.A = uint8(color1 >> 24)
				mat.R = uint8(color1 >> 16)
				mat.G = uint8(color1 >> 8)
				mat.B = uint8(color1)
				mat.Color1Raw = color1
				mat.Color2Raw = pushes[1]
				mat.ShaderCfg = pushes[0]
			}

		case "chrome":
			// Chrome materials use combiner-like color args
			if len(pushes) >= 3 {
				color1 := pushes[2]
				mat.A = uint8(color1 >> 24)
				mat.R = uint8(color1 >> 16)
				mat.G = uint8(color1 >> 8)
				mat.B = uint8(color1)
				mat.Color1Raw = color1
				mat.Color2Raw = pushes[1]
				mat.ShaderCfg = pushes[0]
			}

		case "eggglow":
			if len(pushes) >= 3 {
				color1 := pushes[2]
				mat.A = uint8(color1 >> 24)
				mat.R = uint8(color1 >> 16)
				mat.G = uint8(color1 >> 8)
				mat.B = uint8(color1)
				mat.Color1Raw = color1
				mat.Color2Raw = pushes[1]
			}

		case "backing", "panel":
			// These have different argument structures
			// Keep args for now, apply defaults
			mat.R = 0
			mat.G = 0
			mat.B = 0
			mat.A = 128
		}

		materials = append(materials, mat)
	}

	return materials
}

// ExtractMaterialsFromXBE is the high-level entry point: load XBE,
// decompile it, parse the CreateAllMaterials function, return materials.
func ExtractMaterialsFromXBE(xbePath string) ([]DecodedMaterial, error) {
	img, err := Open(xbePath)
	if err != nil {
		return nil, fmt.Errorf("xbe: %w", err)
	}

	d, err := Disassemble(img)
	if err != nil {
		return nil, fmt.Errorf("xbe disassemble: %w", err)
	}

	// Decompile all functions
	funcs := d.DecompileAll()

	// Build the full decompiled text
	var sb strings.Builder
	for _, df := range funcs {
		sb.WriteString(df.Format())
		sb.WriteString("\n")
	}

	materials := ParseMaterialsFromDecompiled(sb.String())
	if len(materials) == 0 {
		return nil, fmt.Errorf("xbe: no materials found in decompiled output")
	}

	return materials, nil
}
