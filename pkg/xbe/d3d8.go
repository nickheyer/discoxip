package xbe

import (
	"fmt"

	"golang.org/x/arch/x86/x86asm"
)

// D3D8 render state and texture stage state enumerations.
// These are the Xbox D3D8 SDK constants used by the dashboard.

// D3DRENDERSTATETYPE — values passed to SetRenderState
var D3DRenderStates = map[uint32]string{
	7:   "D3DRS_ZENABLE",
	8:   "D3DRS_FILLMODE",
	9:   "D3DRS_SHADEMODE",
	14:  "D3DRS_ZWRITEENABLE",
	15:  "D3DRS_ALPHATESTENABLE",
	19:  "D3DRS_SRCBLEND",
	20:  "D3DRS_DESTBLEND",
	22:  "D3DRS_CULLMODE",
	23:  "D3DRS_ZFUNC",
	24:  "D3DRS_ALPHAREF",
	25:  "D3DRS_ALPHAFUNC",
	26:  "D3DRS_DITHERENABLE",
	27:  "D3DRS_ALPHABLENDENABLE",
	28:  "D3DRS_FOGENABLE",
	29:  "D3DRS_SPECULARENABLE",
	34:  "D3DRS_FOGCOLOR",
	35:  "D3DRS_FOGTABLEMODE",
	36:  "D3DRS_FOGSTART",
	37:  "D3DRS_FOGEND",
	38:  "D3DRS_FOGDENSITY",
	48:  "D3DRS_RANGEFOGENABLE",
	52:  "D3DRS_STENCILENABLE",
	53:  "D3DRS_STENCILFAIL",
	54:  "D3DRS_STENCILZFAIL",
	55:  "D3DRS_STENCILPASS",
	56:  "D3DRS_STENCILFUNC",
	57:  "D3DRS_STENCILREF",
	58:  "D3DRS_STENCILMASK",
	59:  "D3DRS_STENCILWRITEMASK",
	60:  "D3DRS_TEXTUREFACTOR",
	128: "D3DRS_TEXTUREFACTOR", // Xbox-specific alias
	131: "D3DRS_SRCBLEND",      // Xbox-specific mapping
	132: "D3DRS_DESTBLEND",     // Xbox-specific mapping
	134: "D3DRS_BLENDOP",
	137: "D3DRS_COLORWRITEENABLE",
	154: "D3DRS_LIGHTING",
	155: "D3DRS_AMBIENT",
	171: "D3DRS_DIFFUSEMATERIALSOURCE",
	172: "D3DRS_SPECULARMATERIALSOURCE",
	173: "D3DRS_AMBIENTMATERIALSOURCE",
	174: "D3DRS_EMISSIVEMATERIALSOURCE",
	// Xbox-specific render states (D3DRS values above 128 are Xbox extensions)
	127: "D3DRS_MULTISAMPLEANTIALIAS",
	129: "D3DRS_EDGEANTIALIAS",
	130: "D3DRS_MULTISAMPLETYPE",
	133: "D3DRS_ZBIAS",
	135: "D3DRS_LOGICOP",
	136: "D3DRS_MULTISAMPLEMASK",
	138: "D3DRS_SWATHWIDTH",
	139: "D3DRS_POLYGONOFFSETZSLOPESCALE",
	140: "D3DRS_POLYGONOFFSETZOFFSET",
	141: "D3DRS_POINTOFFSETENABLE",
	142: "D3DRS_WIREFRAMEOFFSETENABLE",
	143: "D3DRS_SOLIDOFFSETENABLE",
	144: "D3DRS_DEPTHCLIPCONTROL",
	145: "D3DRS_STIPPLEENABLE",
	146: "D3DRS_SIMPLE_UNUSED8",
	147: "D3DRS_SIMPLE_UNUSED7",
	148: "D3DRS_SIMPLE_UNUSED6",
	149: "D3DRS_SIMPLE_UNUSED5",
	150: "D3DRS_SIMPLE_UNUSED4",
	151: "D3DRS_SIMPLE_UNUSED3",
	152: "D3DRS_SIMPLE_UNUSED2",
	153: "D3DRS_SIMPLE_UNUSED1",
}

// D3DTEXTURESTAGESTATETYPE — Xbox D3D8 SDK values (from cxbx XbD3D8Types.h).
// IMPORTANT: Xbox numbering differs from PC D3D8. Deferred states 0-21 are
// indexed directly into a 32-entry-per-stage table via SetTextureStageState.
var D3DTextureStageStates = map[uint32]string{
	// Deferred texture states (set via SetTextureStageState_Deferred)
	0:  "X_D3DTSS_ADDRESSU",
	1:  "X_D3DTSS_ADDRESSV",
	2:  "X_D3DTSS_ADDRESSW",
	3:  "X_D3DTSS_MAGFILTER",
	4:  "X_D3DTSS_MINFILTER",
	5:  "X_D3DTSS_MIPFILTER",
	6:  "X_D3DTSS_MIPMAPLODBIAS",
	7:  "X_D3DTSS_MAXMIPLEVEL",
	8:  "X_D3DTSS_MAXANISOTROPY",
	9:  "X_D3DTSS_COLORKEYOP",  // Xbox ext
	10: "X_D3DTSS_COLORSIGN",   // Xbox ext
	11: "X_D3DTSS_ALPHAKILL",   // Xbox ext
	12: "X_D3DTSS_COLOROP",
	13: "X_D3DTSS_COLORARG0",
	14: "X_D3DTSS_COLORARG1",
	15: "X_D3DTSS_COLORARG2",
	16: "X_D3DTSS_ALPHAOP",
	17: "X_D3DTSS_ALPHAARG0",
	18: "X_D3DTSS_ALPHAARG1",
	19: "X_D3DTSS_ALPHAARG2",
	20: "X_D3DTSS_RESULTARG",
	21: "X_D3DTSS_TEXTURETRANSFORMFLAGS",
	// Non-deferred texture states
	22: "X_D3DTSS_BUMPENVMAT00",
	23: "X_D3DTSS_BUMPENVMAT01",
	24: "X_D3DTSS_BUMPENVMAT11",
	25: "X_D3DTSS_BUMPENVMAT10",
	26: "X_D3DTSS_BUMPENVLSCALE",
	27: "X_D3DTSS_BUMPENVLOFFSET",
	28: "X_D3DTSS_TEXCOORDINDEX",
	29: "X_D3DTSS_BORDERCOLOR",
	30: "X_D3DTSS_COLORKEYCOLOR", // Xbox ext
	31: "X_D3DTSS_UNSUPPORTED",
}

// D3DTEXTUREOP — Xbox D3D8 SDK values (from cxbx XbD3D8Types.h).
// Used for X_D3DTSS_COLOROP and X_D3DTSS_ALPHAOP.
var D3DTextureOps = map[uint32]string{
	1:  "X_D3DTOP_DISABLE",
	2:  "X_D3DTOP_SELECTARG1",
	3:  "X_D3DTOP_SELECTARG2",
	4:  "X_D3DTOP_MODULATE",
	5:  "X_D3DTOP_MODULATE2X",
	6:  "X_D3DTOP_MODULATE4X",
	7:  "X_D3DTOP_ADD",
	8:  "X_D3DTOP_ADDSIGNED",
	9:  "X_D3DTOP_ADDSIGNED2X",
	10: "X_D3DTOP_SUBTRACT",
	11: "X_D3DTOP_ADDSMOOTH",
	12: "X_D3DTOP_BLENDDIFFUSEALPHA",
	13: "X_D3DTOP_BLENDCURRENTALPHA",
	14: "X_D3DTOP_BLENDTEXTUREALPHA",
	15: "X_D3DTOP_BLENDFACTORALPHA",
	16: "X_D3DTOP_BLENDTEXTUREALPHAPM",
	17: "X_D3DTOP_PREMODULATE",
	18: "X_D3DTOP_MODULATEALPHA_ADDCOLOR",
	19: "X_D3DTOP_MODULATECOLOR_ADDALPHA",
	20: "X_D3DTOP_MODULATEINVALPHA_ADDCOLOR",
	21: "X_D3DTOP_MODULATEINVCOLOR_ADDALPHA",
	22: "X_D3DTOP_DOTPRODUCT3",
	23: "X_D3DTOP_MULTIPLYADD",
	24: "X_D3DTOP_LERP",
	25: "X_D3DTOP_BUMPENVMAP",
	26: "X_D3DTOP_BUMPENVMAPLUMINANCE",
}

// D3DTA — Xbox texture argument flags (from cxbx XbPixelShader.cpp).
// Low 3 bits select the source, upper bits are modifiers.
var D3DTextureArgs = map[uint32]string{
	0: "X_D3DTA_DIFFUSE",
	1: "X_D3DTA_CURRENT",
	2: "X_D3DTA_TEXTURE",
	3: "X_D3DTA_TFACTOR",
	4: "X_D3DTA_SPECULAR",
	5: "X_D3DTA_TEMP",
}

// D3DTA modifier flags (OR'd with the source value)
const (
	D3DTA_COMPLEMENT      = 0x10 // 1 - arg
	D3DTA_ALPHAREPLICATE  = 0x20 // replicate alpha to all channels
)

// D3DBLEND — blend factor values
var D3DBlendFactors = map[uint32]string{
	0:  "D3DBLEND_ZERO",
	1:  "D3DBLEND_ONE",
	2:  "D3DBLEND_SRCCOLOR",
	3:  "D3DBLEND_INVSRCCOLOR",
	4:  "D3DBLEND_SRCALPHA",
	5:  "D3DBLEND_INVSRCALPHA",
	6:  "D3DBLEND_DESTALPHA",
	7:  "D3DBLEND_INVDESTALPHA",
	8:  "D3DBLEND_DESTCOLOR",
	9:  "D3DBLEND_INVDESTCOLOR",
	10: "D3DBLEND_SRCALPHASAT",
}

// NamedD3DFunc maps known Xbox D3D8 function addresses to their names.
// These are identified by their code patterns and location in the D3D section.
type NamedD3DFunc struct {
	Name     string
	ArgCount int // number of stack arguments (not counting this/ecx)
}

// IdentifyD3DFunctions analyzes the disassembly to identify and name
// known D3D8 functions based on their code patterns and section location.
func (d *Disassembly) IdentifyD3DFunctions() {
	// Identify functions in all library sections by code pattern matching.
	// Track used names to avoid duplicates — append VA suffix if a name is already taken.
	usedNames := make(map[string]bool)
	for _, fn := range d.Functions {
		if fn.Name != "" {
			usedNames[fn.Name] = true
		}
	}

	libSections := []struct {
		name     string
		identify func(*Function, uint32) string
	}{
		{"D3D", identifyD3DFunc},
		{"D3DX", identifyD3DXFunc},
		{"XGRPH", identifyXGRPHFunc},
		{"DSOUND", identifyDSOUNDFunc},
		{"XPP", identifyXPPFunc},
	}

	for _, lib := range libSections {
		sec := d.Image.FindSection(lib.name)
		if sec == nil {
			continue
		}
		for va, fn := range d.Functions {
			if fn.Name != "" {
				continue
			}
			if va >= sec.VirtualAddr && va < sec.VirtualAddr+sec.VirtualSize {
				if name := lib.identify(fn, va); name != "" {
					if usedNames[name] {
						name = fmt.Sprintf("%s_%08X", name, va)
					}
					fn.Name = name
					usedNames[name] = true
				}
			}
		}
	}

	// Name the key dashboard functions we identified from the material analysis
	knownFuncs := map[uint32]string{
		// Material system
		0x00042E05: "CMaxMaterial::CMaxMaterial_base",
		0x00042E31: "CMaxMaterial::CMaxMaterial_flatcolor",
		0x00042E65: "CMaxMaterial::CMaxMaterial_combiner",
		0x00042EA4: "CMaxMaterial::CMaxMaterial_wireframe",
		0x00042ED3: "CMaxMaterial::CMaxMaterial_chrome",
		0x00042FB1: "UnpackD3DCOLOR_to_float4",
		0x000433ED: "CMaxMaterial::CMaxMaterial_innerwall",
		0x0004341C: "CMaxMaterial::CMaxMaterial_numbered",
		0x000434FA: "CMaxMaterial::CMaxMaterial_orb",
		0x00043514: "CMaxMaterial::CMaxMaterial_panel",
		0x00043548: "CMaxMaterial::CMaxMaterial_reflect",
		0x00043567: "CMaxMaterial::CMaxMaterial_backing",
		0x0004359B: "CMaxMaterial::CMaxMaterial_eggpulse",
		0x000435B8: "CMaxMaterial::CMaxMaterial_eggglow",
		0x000435DC: "CMaxMaterial::CMaxMaterial_key",
		0x0004360D: "CMaxMaterial::CMaxMaterial_icon",
		0x0004386C: "CMaxMaterial::ApplyAlpha",
		0x00043889: "CMaxMaterial::Apply_flatcolor",
		0x00043936: "CMaxMaterial::Apply_combiner",
		0x000439D6: "CMaxMaterial::Apply_wireframe",
		0x00043AAB: "CMaxMaterial::Apply_chrome",
		0x0004496E: "CMaxMaterial::Apply_innerwall",
		0x00043434: "CMaxMaterial::Apply_numbered",
		0x00044A60: "CMaxMaterial::Apply_orb",
		0x00044B69: "CMaxMaterial::Apply_panel",
		0x00044C67: "CMaxMaterial::Apply_reflect",
		0x00044D22: "CMaxMaterial::Apply_backing",
		0x00044DED: "CMaxMaterial::Apply_eggpulse",
		0x00044E61: "CMaxMaterial::Apply_eggglow",
		0x00044EF1: "CMaxMaterial::Apply_key",
		0x00043646: "CMaxMaterial::Apply_icon",
		0x00043B30: "CMaxMaterial::CreateAllMaterials",
		0x00046660: "LookupTexture",
		0x00046912: "SetRegisterCombinerConstants",
		0x000460D5: "GetD3DDevice",
	}

	for va, name := range knownFuncs {
		if fn, ok := d.Functions[va]; ok {
			fn.Name = name
		} else {
			// Function not discovered by prologue/call heuristic (e.g. vtable targets).
			// Create it now by scanning forward from VA to RET.
			fn := &Function{EntryVA: va, Name: name}
			for addr := va; ; {
				insn, ok := d.InsnByVA[addr]
				if !ok {
					break
				}
				fn.Instructions = append(fn.Instructions, *insn)
				if insn.Inst.Op == x86asm.RET {
					break
				}
				addr += uint32(insn.Inst.Len)
			}
			if len(fn.Instructions) > 0 {
				last := fn.Instructions[len(fn.Instructions)-1]
				fn.Size = int(last.VA - va + uint32(last.Inst.Len))
				d.Functions[va] = fn
			}
		}
	}
}

// identifyD3DFunc tries to identify a D3D library function by its code pattern.
func identifyD3DFunc(fn *Function, _ uint32) string {
	if len(fn.Instructions) == 0 {
		return ""
	}

	// Pattern: writes to state table at fixed addresses
	for _, insn := range fn.Instructions {
		// Look for characteristic address patterns in D3D state functions
		if insn.Inst.Op == 0 {
			continue
		}
		for _, arg := range insn.Inst.Args {
			if arg == nil {
				continue
			}
			if mem, ok := arg.(x86asm.Mem); ok {
				disp := uint32(mem.Disp)
				switch {
				case disp == 0xBEB88:
					return "D3DDevice_SetTextureStageState_Deferred"
				case disp == 0xBED88:
					return "D3DDevice_SetRenderState_Simple"
				case disp == 0xBEF94:
					return "D3DDevice_SetTransform"
				case disp == 0xBF090:
					return "D3DDevice_SetTexture"
				}
			}
		}
	}

	return ""
}

// identifyXGRPHFunc identifies XGRPH (Xbox graphics helper) functions.
func identifyXGRPHFunc(fn *Function, _ uint32) string {
	if len(fn.Instructions) == 0 {
		return ""
	}
	for _, insn := range fn.Instructions {
		for _, arg := range insn.Inst.Args {
			if arg == nil {
				continue
			}
			if mem, ok := arg.(x86asm.Mem); ok {
				disp := uint32(mem.Disp)
				switch {
				case disp == 0xE67B0:
					return "XGRPH_GetDisplayMode"
				case disp == 0xE67B4:
					return "XGRPH_GetBackBuffer"
				}
			}
		}
	}
	return ""
}

// identifyDSOUNDFunc identifies DirectSound functions.
func identifyDSOUNDFunc(fn *Function, _ uint32) string {
	if len(fn.Instructions) == 0 {
		return ""
	}
	for _, insn := range fn.Instructions {
		for _, arg := range insn.Inst.Args {
			if arg == nil {
				continue
			}
			if mem, ok := arg.(x86asm.Mem); ok {
				disp := uint32(mem.Disp)
				switch {
				case disp == 0xE6440:
					return "DirectSound_CreateSoundBuffer"
				case disp == 0xE6448:
					return "DirectSound_GetCaps"
				}
			}
		}
	}
	return ""
}

// identifyXPPFunc identifies XPP (Xbox Platform/Presentation) functions.
func identifyXPPFunc(fn *Function, _ uint32) string {
	if len(fn.Instructions) == 0 {
		return ""
	}
	for _, insn := range fn.Instructions {
		for _, arg := range insn.Inst.Args {
			if arg == nil {
				continue
			}
			if mem, ok := arg.(x86asm.Mem); ok {
				disp := uint32(mem.Disp)
				switch {
				case disp == 0x110220:
					return "XPP_GetSystemTime"
				}
			}
		}
	}
	return ""
}

// identifyD3DXFunc identifies D3DX utility library functions.
func identifyD3DXFunc(fn *Function, _ uint32) string {
	if len(fn.Instructions) == 0 {
		return ""
	}

	for _, insn := range fn.Instructions {
		if insn.Inst.Op == 0 {
			continue
		}
		for _, arg := range insn.Inst.Args {
			if arg == nil {
				continue
			}
			if mem, ok := arg.(x86asm.Mem); ok {
				disp := uint32(mem.Disp)
				switch {
				case disp == 0xBEB50:
					return "D3DX_GetPushBuffer"
				case disp == 0xBEFC0:
					return "D3DX_GetDeviceState"
				}
			}
		}
	}

	return ""
}
