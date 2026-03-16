package xbe

import "golang.org/x/arch/x86/x86asm"

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

// D3DTEXTURESTAGESTATETYPE — values passed to SetTextureStageState
var D3DTextureStageStates = map[uint32]string{
	0:  "D3DTSS_COLOROP",
	1:  "D3DTSS_COLORARG0",
	2:  "D3DTSS_COLORARG1",
	3:  "D3DTSS_COLORARG2",
	4:  "D3DTSS_ALPHAOP",
	5:  "D3DTSS_ALPHAARG0",
	6:  "D3DTSS_ALPHAARG1",
	7:  "D3DTSS_ALPHAARG2",
	8:  "D3DTSS_RESULTARG",
	9:  "D3DTSS_TEXTURETRANSFORMFLAGS",
	10: "D3DTSS_BUMPENVMAT00",
	11: "D3DTSS_BUMPENVMAT01",
	12: "D3DTSS_BUMPENVMAT11",
	13: "D3DTSS_BUMPENVMAT10",
	14: "D3DTSS_BUMPENVLSCALE",
	15: "D3DTSS_BUMPENVLOFFSET",
	// Note: Xbox D3D8 uses a different numbering than desktop D3D8.
	// The mapping here is for the Xbox SDK-specific indices used by
	// SetTextureStageState in the dashboard XBE.
}

// D3DTEXTUREOP — values for D3DTSS_COLOROP / D3DTSS_ALPHAOP
var D3DTextureOps = map[uint32]string{
	0:  "D3DTOP_DISABLE",
	1:  "D3DTOP_SELECTARG1",
	2:  "D3DTOP_SELECTARG2",
	3:  "D3DTOP_MODULATE",
	4:  "D3DTOP_MODULATE2X",
	5:  "D3DTOP_MODULATE4X",
	6:  "D3DTOP_ADD",
	7:  "D3DTOP_ADDSIGNED",
	8:  "D3DTOP_ADDSIGNED2X",
	9:  "D3DTOP_SUBTRACT",
	10: "D3DTOP_ADDSMOOTH",
	11: "D3DTOP_BLENDDIFFUSEALPHA",
	12: "D3DTOP_BLENDCURRENTALPHA",
	13: "D3DTOP_BLENDTEXTUREALPHA",
	14: "D3DTOP_BLENDFACTORALPHA",
	15: "D3DTOP_BLENDTEXTUREALPHAPM",
	16: "D3DTOP_PREMODULATE",
	17: "D3DTOP_MODULATEALPHA_ADDCOLOR",
	18: "D3DTOP_MODULATECOLOR_ADDALPHA",
	19: "D3DTOP_MODULATEINVALPHA_ADDCOLOR",
	20: "D3DTOP_MODULATEINVCOLOR_ADDALPHA",
	21: "D3DTOP_DOTPRODUCT3",
	22: "D3DTOP_MULTIPLYADD",
	23: "D3DTOP_LERP",
	24: "D3DTOP_BUMPENVMAP",
	25: "D3DTOP_BUMPENVMAPLUMINANCE",
}

// D3DTA — texture argument flags for D3DTSS_COLORARG / D3DTSS_ALPHAARG
var D3DTextureArgs = map[uint32]string{
	0: "D3DTA_DIFFUSE",
	1: "D3DTA_CURRENT",
	2: "D3DTA_TEXTURE",
	3: "D3DTA_TFACTOR",
	4: "D3DTA_SPECULAR",
	5: "D3DTA_TEMP",
}

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
	// Name functions in the D3D section by their behavior patterns.
	d3d := d.Image.FindSection("D3D")
	d3dx := d.Image.FindSection("D3DX")

	for va, fn := range d.Functions {
		// Already named (kernel import)
		if fn.Name != "" {
			continue
		}

		// Functions in D3D section
		if d3d != nil && va >= d3d.VirtualAddr && va < d3d.VirtualAddr+d3d.VirtualSize {
			fn.Name = identifyD3DFunc(fn, va)
		}

		// Functions in D3DX section
		if d3dx != nil && va >= d3dx.VirtualAddr && va < d3dx.VirtualAddr+d3dx.VirtualSize {
			fn.Name = identifyD3DXFunc(fn, va)
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
		0x000439D6: "CMaxMaterial::Apply_combiner2",
		0x00043AAB: "CMaxMaterial::Apply_combiner3",
		0x00043B30: "CMaxMaterial::CreateAllMaterials",
		0x00046660: "LookupTexture",
		0x00046912: "SetRegisterCombinerConstants",
		0x000460D5: "GetD3DDevice",
	}

	for va, name := range knownFuncs {
		if fn, ok := d.Functions[va]; ok {
			fn.Name = name
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

func identifyD3DXFunc(_ *Function, _ uint32) string {
	return ""
}
