//go:build samples

package bios

import (
	"bufio"
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/nickheyer/discoxip/pkg/xbe"
)

// Translates a dashboard vertex program, the kernel's fixed function states and the texture shader programs under node
const translatorHarness = `
const words = new Uint32Array(KELVIN_PROGRAM_SLOTS * 4);
words.set([0x00000000, 0x00EC001B, 0x0836186C, 0x20708800, 0x00000000, 0x00ED401B, 0x0836186C, 0x28200FF8, 0x00000000, 0x00ACA61B, 0x0836186C, 0x28300FF8, 0x00000000, 0x00EC201B, 0x0836186C, 0x20704800, 0x00000000, 0x00ED601B, 0x0836186C, 0x24200FF8, 0x00000000, 0x00ACC61B, 0x0836186C, 0x24300FF8, 0x00000000, 0x00EC401B, 0x0836186C, 0x20702800, 0x00000000, 0x00ED801B, 0x0836186C, 0x22200FF8, 0x00000000, 0x00ACE61B, 0x0836186C, 0x22300FF8, 0x00000000, 0x00EC601B, 0x0836186C, 0x20701800, 0x00000000, 0x00A0001B, 0x3436686C, 0x21400FF8, 0x00000000, 0x002E001B, 0x0C36106C, 0x2FA00FF8, 0x00000000, 0x08A0001B, 0x24364BFD, 0x11510FF8, 0x00000000, 0x0647401B, 0xC4361BFF, 0x1078E800, 0x00000000, 0x0800001B, 0x083613FD, 0x50610FF8, 0x00000000, 0x0040001B, 0x35FE286C, 0x2E700FF8, 0x00000000, 0x0040001B, 0x25FEC86C, 0x2E800FF8, 0x00000000, 0x00A0001B, 0x7437086C, 0x21900FF8, 0x00000000, 0x0087601B, 0xC400286C, 0x3070E800, 0x00000000, 0x014000FF, 0x97FF286C, 0x21B00FF8, 0x00000000, 0x008DE01B, 0xA5FF686C, 0x3070F819]);
const program = translateVertexProgram(words, 0);
const expect = (what, ok) => { if (!ok) throw new Error(what); console.log(what); };
expect("the program reads position and normal attributes", program.inputs.has(0) && program.inputs.has(3));
expect("the program writes the position output a component at a time", program.body.includes("oPos.x = tmp.x;") && program.body.includes("oPos.w = tmp.w;"));
expect("the program writes the diffuse colour", program.body.includes("oD0."));
expect("the program holds 21 instructions", program.body.split("tmp = ").length + program.body.split("ilu = ").length - 2 >= 21);
const tail = { pointParams: false, specularEnable: true, alphaFromMaterialSpecular: true, fog: null };
const vs = vertexShaderSource(program.body + programEpilogue + programTail(tail), false, false);
expect("the vertex shader declares every attribute and the constant store", vs.includes("in vec4 v15;") && vs.includes("uniform vec4 c[" + KELVIN_CONSTANTS + "];"));
expect("the vertex shader hands the position on", vs.includes("gl_Position = oPos;"));
expect("the vertex shader brings screen space back to clip space", vs.includes("oPos.x = 2.0 * (oPos.x - surfaceSize.x * 0.5) / surfaceSize.x;"));
expect("a program without fog leaves the fog input opaque", vs.includes("oFog = vec4(1.0);"));
const unlit = { lighting: true, lights: [0, 0, 0, 0, 0, 0, 0, 0], localEye: false, separateSpecular: true, specularEnable: true, alphaFromMaterialSpecular: false, emissionSrc: 0, ambientSrc: 0, diffuseSrc: 0, specularSrc: 0, normalization: false, skinning: 0, texgen: new Array(16).fill(0), texMatrix: [false, false, false, false], infiniteViewer: false, pointParams: false, fog: null };
const fixed = vertexShaderSource(fixedFunctionSource(unlit), true, true);
expect("lighting with no light leaves the scene ambient and the material alpha", fixed.includes("oD0 = vec4(sceneAmbient, materialAlpha);") && fixed.includes("flat out vec4 vD0;"));
expect("the fixed function shader transforms by the composite matrix in the transform context", fixed.includes("mat4 compositeMat = mat4(c[0], c[1], c[2], c[3]);") && fixed.includes("vec4 p = v0 * compositeMat;") && fixed.includes("oD1.a = 1.0;"));
const lit = Object.assign({}, unlit, { lights: [1, 2, 3, 0, 0, 0, 0, 0], localEye: true, diffuseSrc: 1, texgen: [0x8512, 0x8512, 0x8512, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0], texMatrix: [true, false, false, false], normalization: true, skinning: 2, fog: { mode: 0x2601, genMode: 1 } });
const litSource = fixedFunctionSource(lit);
expect("an infinite light reads its direction from the light context", litSource.includes("normalize(vec3(lightCtx[3].y, lightCtx[3].z, lightCtx[3].w))"));
expect("a local light reads its position and attenuation", litSource.includes("vec3(lightCtx[13].w, lightCtx[14].x, lightCtx[14].y) - tPos") && litSource.includes("vec3 att = vec3(lightCtx[14].z, lightCtx[14].w, lightCtx[15].x);"));
expect("a spot light applies the falloff", litSource.includes("vec3 spotFalloff = vec3(lightCtx[20].x, lightCtx[20].y, lightCtx[20].z);") && litSource.includes("attenuation *= spotFactor;"));
expect("the local eye and the specular curve shape the highlights", litSource.includes("VPeye") && litSource.includes("specularParams.y * nDotHV + specularParams.z"));
expect("the vertex diffuse colour scales the light", litSource.includes("oD0.rgb += v3.rgb * lightDiffuse;"));
expect("reflection map generation and the texture matrix shape the coordinates", litSource.includes("reflect(u, tNormal)") && litSource.includes("oT0 = oT0 * mat4(c[68], c[69], c[70], c[71]);"));
expect("skinning blends two matrices by the weights", litSource.includes("(v0 * modelViewMat[1]).xyzw * v1.y") && litSource.includes("tNormal = normalize(tNormal);") && litSource.includes("vec4 p = tPosition * compositeMat;"));
expect("radial linear fog is generated", litSource.includes("float fogDistance = length(tPosition.xyz);") && litSource.includes("fogParams.x + fogDistance * fogParams.y - 1.0"));
const flat2D = { cube: false, depth: false, bits: 0, floating: false, rect: true, smooth: true, conv: 0, colorKeyMode: 0, colorKeyMask: "0xFFFFFFFFu", alphaKill: false };
const cube = Object.assign({}, flat2D, { cube: true, rect: false });
const depth16 = Object.assign({}, flat2D, { depth: true, bits: 16 });
const base = { flat: false, control: 1, colorICW: [0x04200000], alphaICW: [0x14200000], colorOCW: [0xC00], alphaOCW: [0xC00], finalCW0: 0xC, finalCW1: 0x1C80, stages: [{ mode: 1, tex: flat2D }, { mode: 0, tex: null }, { mode: 0, tex: null }, { mode: 0, tex: null }], shadowFunc: 0, dotMapping: 0, otherStageInput: 0, clipPlaneMode: 0, alphaTest: true, alphaFunc: 0x204 };
const fs = fragmentShaderSource(base);
expect("stage 0 samples its texture", fs.includes("vec4 t0 = texture(tex0, textureCoord(pT0, texScale0));"));
expect("stage 1 holds no texture", fs.includes("vec4 t1 = vec4(0.0, 0.0, 0.0, 1.0);"));
expect("the colour combiner multiplies the diffuse colour by one into spare 0", fs.includes("vec3 abc0 = (max(v0.rgb, vec3(0.0)) * (vec3(1.0) - clamp(vec4(0.0).rgb, vec3(0.0), vec3(1.0))));") && fs.includes("r0.rgb = clamp(sumc0, vec3(-1.0), vec3(1.0));"));
expect("the alpha combiner takes the diffuse alpha and the blue of a colour input", fs.includes("float aba0 = (max(v0.a, 0.0) * (1.0 - clamp(vec4(0.0).b, 0.0, 1.0)));"));
expect("the final combiner passes spare 0 through", fs.includes("vec3 finalColor = max(vec4(0.0).rgb, vec3(0.0)) * max(vec4(0.0).rgb, vec3(0.0)) + (vec3(1.0) - max(vec4(0.0).rgb, vec3(0.0))) * max(vec4(0.0).rgb, vec3(0.0)) + max(r0.rgb, vec3(0.0));") && fs.includes("float finalAlpha = max(r0.a, 0.0);"));
expect("the alpha test discards what fails", fs.includes("if (!(finalAlpha > alphaRef)) discard;"));
const bootStage = fragmentShaderSource(Object.assign({}, base, { control: 0x11108, colorICW: [0xC149C14A, 0, 0, 0, 0, 0, 0, 0], alphaICW: [0xD159D15A, 0, 0, 0, 0, 0, 0, 0], colorOCW: [0xC30CD, 0, 0, 0, 0, 0, 0, 0], alphaOCW: [0, 0, 0, 0, 0, 0, 0, 0], stages: [{ mode: 0, tex: null }, { mode: 3, tex: cube }, { mode: 3, tex: cube }, { mode: 0, tex: null }] }));
expect("a dot product writes its colour and copies blue into alpha", bootStage.includes("r0.rgb = clamp(abc0, vec3(-1.0), vec3(1.0));\nr0.a = r0.b;") && bootStage.includes("r1.rgb = clamp(cdc0, vec3(-1.0), vec3(1.0));\nr1.a = r1.b;") && bootStage.includes("vec3 abc0 = vec3(dot("));
expect("the mux reads the low bit of spare 0 alpha unless the control word says the high bit", bootStage.includes("(uint(r0.a * 255.0) & 1u) == 1u") === false && fragmentShaderSource(Object.assign({}, base, { control: 0x11, colorOCW: [0x4C00] })).includes("(uint(r0.a * 255.0) & 1u) == 1u ? cdc0 : abc0"));
const boot = fragmentShaderSource(Object.assign({}, base, { stages: [{ mode: 0, tex: null }, { mode: 3, tex: cube }, { mode: 3, tex: cube }, { mode: 2, tex: depth16 }], shadowFunc: 4 }));
expect("cube map stages sample cube textures", boot.includes("uniform samplerCube tex1;") && boot.includes("vec4 t1 = texture(tex1, pT1.xyz);"));
expect("the shadow map compares the filtered depth against the projected coordinate", boot.includes("float ref = clamp(pT3.z / pT3.w, 0.0, 65535.0);") && boot.includes("texelFetch(tex3, clamp(b + ivec2(1, 1), ivec2(0), last), 0).r * 65535.0 > ref"));
const filtered = fragmentShaderSource(Object.assign({}, base, { stages: [{ mode: 1, tex: Object.assign({}, flat2D, { conv: 2 }) }, { mode: 4, tex: null }, { mode: 0, tex: null }, { mode: 0, tex: null }] }));
expect("the gaussian convolution sums nine taps", filtered.includes("t0 += texture(tex0, c + convolution3x3[k] * texel) * gaussian3x3[k];"));
expect("a pass through stage clamps its coordinates", filtered.includes("vec4 t1 = clamp(pT1, 0.0, 1.0);"));
const bumped = fragmentShaderSource(Object.assign({}, base, { stages: [{ mode: 1, tex: flat2D }, { mode: 6, tex: Object.assign({}, flat2D, { rect: false, alphaKill: true }) }, { mode: 0x11, tex: null }, { mode: 0xC, tex: cube }] }));
expect("bump environment mapping perturbs by the signed input texel", bumped.includes("vec2 dsdt1 = bumpMat[1] * vec2(sign3(t0.b), sign3(t0.g));") && bumped.includes("if (t1.a == 0.0) discard;"));
expect("the specular reflection stage reflects through the dot products", bumped.includes("float dot2 = dot(pT2.xyz, dotmap_zero_to_one(t0));") && bumped.includes("vec4 t3 = texture(tex3, rv_3);"));
let stopped = null;
try { fragmentShaderSource(Object.assign({}, base, { stages: [{ mode: 1, tex: flat2D }, { mode: 1, tex: flat2D }, { mode: 8, tex: flat2D }, { mode: 0, tex: null }] })); } catch (e) { stopped = e.message; }
expect("a texture shader program the engine has no model of stops the page", stopped === "texture stage program 0x00000008 on stage 2");
console.log("ok");
`

// The translators must produce the shader text the engine compiles for every state the samples set
func TestGraphicsTranslators(t *testing.T) {
	var buf bytes.Buffer
	if err := xbe.WriteCore(&buf); err != nil {
		t.Fatal(err)
	}
	bw := bufio.NewWriter(&buf)
	e := &emitter{w: bw}
	e.writeGraphicsConstants()
	e.writeShaders()
	if err := bw.Flush(); err != nil {
		t.Fatal(err)
	}
	buf.WriteString(translatorHarness)
	path := filepath.Join(t.TempDir(), "translators.js")
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(node, path).CombinedOutput()
	if err != nil || !bytes.HasSuffix(bytes.TrimSpace(out), []byte("ok")) {
		t.Fatalf("node: %v\n%s", err, out)
	}
}
