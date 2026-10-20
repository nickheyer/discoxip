package xbe

// renderStateInfo is one row of the XDK render state table with the build that introduced it.
type renderStateInfo struct {
	Name    string
	Since   int
	Removed int
}

// Render state sections in XDK table order
const (
	rsSectionPS = iota
	rsSectionSimple
	rsSectionDeferred
	rsSectionComplex
)

// First table row of each section after the pixel shader states
var renderStateSectionStarts = map[string]int{"ZFUNC": rsSectionSimple, "FOGENABLE": rsSectionDeferred, "PSTEXTUREMODES": rsSectionComplex}

// XDK render states in cxbx-reloaded master order, versions verified against 3944..5933
var renderStateTable = []renderStateInfo{
	{"PSALPHAINPUTS0", 3424, 0}, {"PSALPHAINPUTS1", 3424, 0}, {"PSALPHAINPUTS2", 3424, 0}, {"PSALPHAINPUTS3", 3424, 0},
	{"PSALPHAINPUTS4", 3424, 0}, {"PSALPHAINPUTS5", 3424, 0}, {"PSALPHAINPUTS6", 3424, 0}, {"PSALPHAINPUTS7", 3424, 0},
	{"PSFINALCOMBINERINPUTSABCD", 3424, 0}, {"PSFINALCOMBINERINPUTSEFG", 3424, 0},
	{"PSCONSTANT0_0", 3424, 0}, {"PSCONSTANT0_1", 3424, 0}, {"PSCONSTANT0_2", 3424, 0}, {"PSCONSTANT0_3", 3424, 0},
	{"PSCONSTANT0_4", 3424, 0}, {"PSCONSTANT0_5", 3424, 0}, {"PSCONSTANT0_6", 3424, 0}, {"PSCONSTANT0_7", 3424, 0},
	{"PSCONSTANT1_0", 3424, 0}, {"PSCONSTANT1_1", 3424, 0}, {"PSCONSTANT1_2", 3424, 0}, {"PSCONSTANT1_3", 3424, 0},
	{"PSCONSTANT1_4", 3424, 0}, {"PSCONSTANT1_5", 3424, 0}, {"PSCONSTANT1_6", 3424, 0}, {"PSCONSTANT1_7", 3424, 0},
	{"PSALPHAOUTPUTS0", 3424, 0}, {"PSALPHAOUTPUTS1", 3424, 0}, {"PSALPHAOUTPUTS2", 3424, 0}, {"PSALPHAOUTPUTS3", 3424, 0},
	{"PSALPHAOUTPUTS4", 3424, 0}, {"PSALPHAOUTPUTS5", 3424, 0}, {"PSALPHAOUTPUTS6", 3424, 0}, {"PSALPHAOUTPUTS7", 3424, 0},
	{"PSRGBINPUTS0", 3424, 0}, {"PSRGBINPUTS1", 3424, 0}, {"PSRGBINPUTS2", 3424, 0}, {"PSRGBINPUTS3", 3424, 0},
	{"PSRGBINPUTS4", 3424, 0}, {"PSRGBINPUTS5", 3424, 0}, {"PSRGBINPUTS6", 3424, 0}, {"PSRGBINPUTS7", 3424, 0},
	{"PSCOMPAREMODE", 3424, 0}, {"PSFINALCOMBINERCONSTANT0", 3424, 0}, {"PSFINALCOMBINERCONSTANT1", 3424, 0},
	{"PSRGBOUTPUTS0", 3424, 0}, {"PSRGBOUTPUTS1", 3424, 0}, {"PSRGBOUTPUTS2", 3424, 0}, {"PSRGBOUTPUTS3", 3424, 0},
	{"PSRGBOUTPUTS4", 3424, 0}, {"PSRGBOUTPUTS5", 3424, 0}, {"PSRGBOUTPUTS6", 3424, 0}, {"PSRGBOUTPUTS7", 3424, 0},
	{"PSCOMBINERCOUNT", 3424, 0}, {"PSTEXTUREMODES_RESERVED", 3424, 0}, {"PSDOTMAPPING", 3424, 0}, {"PSINPUTTEXTURE", 3424, 0},
	{"ZFUNC", 3424, 0}, {"ALPHAFUNC", 3424, 0}, {"ALPHABLENDENABLE", 3424, 0}, {"ALPHATESTENABLE", 3424, 0}, {"ALPHAREF", 3424, 0},
	{"SRCBLEND", 3424, 0}, {"DESTBLEND", 3424, 0}, {"ZWRITEENABLE", 3424, 0}, {"DITHERENABLE", 3424, 0}, {"SHADEMODE", 3424, 0},
	{"COLORWRITEENABLE", 3424, 0}, {"STENCILZFAIL", 3424, 0}, {"STENCILPASS", 3424, 0}, {"STENCILFUNC", 3424, 0}, {"STENCILREF", 3424, 0},
	{"STENCILMASK", 3424, 0}, {"STENCILWRITEMASK", 3424, 0}, {"BLENDOP", 3424, 0}, {"BLENDCOLOR", 3424, 0}, {"SWATHWIDTH", 3424, 0},
	{"POLYGONOFFSETZSLOPESCALE", 3424, 0}, {"POLYGONOFFSETZOFFSET", 3424, 0}, {"POINTOFFSETENABLE", 3424, 0},
	{"WIREFRAMEOFFSETENABLE", 3424, 0}, {"SOLIDOFFSETENABLE", 3424, 0},
	{"DEPTHCLIPCONTROL", 4432, 0}, {"STIPPLEENABLE", 4627, 0},
	{"SIMPLE_UNUSED8", 4627, 0}, {"SIMPLE_UNUSED7", 4627, 0}, {"SIMPLE_UNUSED6", 4627, 0}, {"SIMPLE_UNUSED5", 4627, 0},
	{"SIMPLE_UNUSED4", 4627, 0}, {"SIMPLE_UNUSED3", 4627, 0}, {"SIMPLE_UNUSED2", 4627, 0}, {"SIMPLE_UNUSED1", 4627, 0},
	{"FOGENABLE", 3424, 0}, {"FOGTABLEMODE", 3424, 0}, {"FOGSTART", 3424, 0}, {"FOGEND", 3424, 0}, {"FOGDENSITY", 3424, 0},
	{"RANGEFOGENABLE", 3424, 0}, {"WRAP0", 3424, 0}, {"WRAP1", 3424, 0}, {"WRAP2", 3424, 0}, {"WRAP3", 3424, 0},
	{"LIGHTING", 3424, 0}, {"SPECULARENABLE", 3424, 0}, {"LOCALVIEWER", 3424, 0}, {"COLORVERTEX", 3424, 0},
	{"BACKSPECULARMATERIALSOURCE", 3424, 0}, {"BACKDIFFUSEMATERIALSOURCE", 3424, 0}, {"BACKAMBIENTMATERIALSOURCE", 3424, 0},
	{"BACKEMISSIVEMATERIALSOURCE", 3424, 0}, {"SPECULARMATERIALSOURCE", 3424, 0}, {"DIFFUSEMATERIALSOURCE", 3424, 0},
	{"AMBIENTMATERIALSOURCE", 3424, 0}, {"EMISSIVEMATERIALSOURCE", 3424, 0}, {"BACKAMBIENT", 3424, 0}, {"AMBIENT", 3424, 0},
	{"POINTSIZE", 3424, 0}, {"POINTSIZE_MIN", 3424, 0}, {"POINTSPRITEENABLE", 3424, 0}, {"POINTSCALEENABLE", 3424, 0},
	{"POINTSCALE_A", 3424, 0}, {"POINTSCALE_B", 3424, 0}, {"POINTSCALE_C", 3424, 0}, {"POINTSIZE_MAX", 3424, 0},
	{"PATCHEDGESTYLE", 3424, 0}, {"PATCHSEGMENTS", 3424, 0}, {"SWAPFILTER", 4034, 0}, {"PRESENTATIONINTERVAL", 4627, 0},
	{"DEFERRED_UNUSED8", 4627, 0}, {"DEFERRED_UNUSED7", 4627, 0}, {"DEFERRED_UNUSED6", 4627, 0}, {"DEFERRED_UNUSED5", 4627, 0},
	{"DEFERRED_UNUSED4", 4627, 0}, {"DEFERRED_UNUSED3", 4627, 0}, {"DEFERRED_UNUSED2", 4627, 0}, {"DEFERRED_UNUSED1", 4627, 0},
	{"PSTEXTUREMODES", 3424, 0}, {"VERTEXBLEND", 3424, 0}, {"FOGCOLOR", 3424, 0}, {"FILLMODE", 3424, 0}, {"BACKFILLMODE", 3424, 0},
	{"TWOSIDEDLIGHTING", 3424, 0}, {"NORMALIZENORMALS", 3424, 0}, {"ZENABLE", 3424, 0}, {"STENCILENABLE", 3424, 0},
	{"STENCILFAIL", 3424, 0}, {"FRONTFACE", 3424, 0}, {"CULLMODE", 3424, 0}, {"TEXTUREFACTOR", 3424, 0}, {"ZBIAS", 3424, 0},
	{"LOGICOP", 3424, 0}, {"EDGEANTIALIAS", 3424, 0}, {"MULTISAMPLEANTIALIAS", 3424, 0}, {"MULTISAMPLEMASK", 3424, 0},
	{"MULTISAMPLETYPE", 3424, 4034}, {"MULTISAMPLEMODE", 4034, 0}, {"MULTISAMPLERENDERTARGETMODE", 4034, 0},
	{"SHADOWFUNC", 3424, 0}, {"LINEWIDTH", 3424, 0}, {"SAMPLEALPHA", 4627, 0}, {"DXT1NOISEENABLE", 3424, 0},
	{"YUVENABLE", 3911, 0}, {"OCCLUSIONCULLENABLE", 3911, 0}, {"STENCILCULLENABLE", 3911, 0}, {"ROPZCMPALWAYSREAD", 3911, 0},
	{"ROPZREAD", 3911, 0}, {"DONOTCULLUNCOMPRESSED", 3911, 0},
}

// RenderStateNames returns the render state index to name map for an XDK build.
func RenderStateNames(build int) map[uint32]string {
	out := map[uint32]string{}
	idx := uint32(0)
	for _, rs := range renderStateTable {
		if rs.Since > build || (rs.Removed > 0 && build >= rs.Removed) {
			continue
		}
		out[idx] = rs.Name
		idx++
	}
	return out
}

// RenderStateSections returns the first index of the simple, deferred and complex sections for an XDK build.
func RenderStateSections(build int) [3]uint32 {
	var out [3]uint32
	idx := uint32(0)
	for _, rs := range renderStateTable {
		if rs.Since > build || (rs.Removed > 0 && build >= rs.Removed) {
			continue
		}
		if sec, ok := renderStateSectionStarts[rs.Name]; ok {
			out[sec-1] = idx
		}
		idx++
	}
	return out
}

// Texture stage states in the 4034 and later order, index is the state number
var textureStageNames = []string{
	"ADDRESSU", "ADDRESSV", "ADDRESSW", "MAGFILTER", "MINFILTER", "MIPFILTER", "MIPMAPLODBIAS", "MAXMIPLEVEL",
	"MAXANISOTROPY", "COLORKEYOP", "COLORSIGN", "ALPHAKILL", "COLOROP", "COLORARG0", "COLORARG1", "COLORARG2",
	"ALPHAOP", "ALPHAARG0", "ALPHAARG1", "ALPHAARG2", "RESULTARG", "TEXTURETRANSFORMFLAGS",
	"BUMPENVMAT00", "BUMPENVMAT01", "BUMPENVMAT11", "BUMPENVMAT10", "BUMPENVLSCALE", "BUMPENVLOFFSET",
	"TEXCOORDINDEX", "BORDERCOLOR", "COLORKEYCOLOR", "UNSUPPORTED",
}

// TextureStageStateNames returns the stage state index to name map for an XDK build.
// Builds up to 3948 number the op and arg states first and the sampler states after them.
func TextureStageStateNames(build int) map[uint32]string {
	out := map[uint32]string{}
	for i, name := range textureStageNames {
		idx := i
		if build <= 3948 {
			switch {
			case i >= 12 && i <= 21:
				idx = i - 12
			case i <= 11:
				idx = i + 10
			}
		}
		out[uint32(idx)] = name
	}
	return out
}

// Transform state numbers in Xbox D3D8
const (
	transformView       = 0
	transformProjection = 1
	transformWorld      = 6
)

// Vertex declaration data types in Xbox D3D8
var vertexDataTypes = map[uint32]struct {
	Name  string
	Bytes int
	Comps int
}{
	0x12: {"FLOAT1", 4, 1}, 0x22: {"FLOAT2", 8, 2}, 0x32: {"FLOAT3", 12, 3}, 0x42: {"FLOAT4", 16, 4},
	0x40: {"D3DCOLOR", 4, 4}, 0x25: {"SHORT2", 4, 2}, 0x45: {"SHORT4", 8, 4},
	0x11: {"NORMSHORT1", 2, 1}, 0x21: {"NORMSHORT2", 4, 2}, 0x31: {"NORMSHORT3", 6, 3}, 0x41: {"NORMSHORT4", 8, 4},
	0x16: {"NORMPACKED3", 4, 3}, 0x15: {"SHORT1", 2, 1}, 0x35: {"SHORT3", 6, 3},
	0x14: {"PBYTE1", 1, 1}, 0x24: {"PBYTE2", 2, 2}, 0x34: {"PBYTE3", 3, 3}, 0x44: {"PBYTE4", 4, 4},
	0x72: {"FLOAT2H", 12, 3}, 0x02: {"NONE", 0, 0},
}
