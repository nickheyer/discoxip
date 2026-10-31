package bios

// Vertex program instruction fields by subtoken word, bit and width, operation names and output register names, from the xemu decoder
const vertexProgramTables = `
const vsFields = { ILU: [1, 25, 3], MAC: [1, 21, 4], CONST: [1, 13, 8], V: [1, 9, 4],
  A_NEG: [1, 8, 1], A_SWZ_X: [1, 6, 2], A_SWZ_Y: [1, 4, 2], A_SWZ_Z: [1, 2, 2], A_SWZ_W: [1, 0, 2], A_R: [2, 28, 4], A_MUX: [2, 26, 2],
  B_NEG: [2, 25, 1], B_SWZ_X: [2, 23, 2], B_SWZ_Y: [2, 21, 2], B_SWZ_Z: [2, 19, 2], B_SWZ_W: [2, 17, 2], B_R: [2, 13, 4], B_MUX: [2, 11, 2],
  C_NEG: [2, 10, 1], C_SWZ_X: [2, 8, 2], C_SWZ_Y: [2, 6, 2], C_SWZ_Z: [2, 4, 2], C_SWZ_W: [2, 2, 2], C_R_HIGH: [2, 0, 2], C_R_LOW: [3, 30, 2], C_MUX: [3, 28, 2],
  OUT_MAC_MASK: [3, 24, 4], OUT_R: [3, 20, 4], OUT_ILU_MASK: [3, 16, 4], OUT_O_MASK: [3, 12, 4], OUT_ORB: [3, 11, 1], OUT_ADDRESS: [3, 3, 8], OUT_MUX: [3, 2, 1], A0X: [3, 1, 1], FINAL: [3, 0, 1] };
const vsMac = ["NOP", "MOV", "MUL", "ADD", "MAD", "DP3", "DPH", "DP4", "DST", "MIN", "MAX", "SLT", "SGE", "ARL"];
const vsIlu = ["NOP", "MOV", "RCP", "RCC", "RSQ", "EXP", "LOG", "LIT"];
const vsOut = ["oPos", "o1", "o2", "oD0", "oD1", "oFog", "oPts", "oB0", "oB1", "oT0", "oT1", "oT2", "oT3", "o13", "o14", "A0"];
`

// A vertex program's instructions as a GLSL vertex shader body, each operation as the xemu translator writes it, screen space brought back to clip space
const vertexProgramTranslator = `
function vsGet(t, name) { const f = vsFields[name]; return (t[f[0]] >>> f[1]) & ((1 << f[2]) - 1); }
function vsMaskOf(m) { let s = ""; for (let i = 0; i < 4; i++) if ((m & (8 >> i)) !== 0) s += "xyzw"[i]; return s; }
function vsSwizzle(t, p) { let s = ""; for (const c of ["X", "Y", "Z", "W"]) s += "xyzw"[vsGet(t, p + "_SWZ_" + c)]; return s; }
function vsRegister(i) { return i === 12 ? "oPos" : "R" + i; }
function vsOperand(t, p, rnum, relative, slot) {
  let base;
  switch (vsGet(t, p + "_MUX")) {
  case 1: base = vsRegister(rnum); break;
  case 2: base = "v" + vsGet(t, "V"); break;
  case 3: base = relative ? "c[clampIndex(A0 + " + vsGet(t, "CONST") + ")]" : "c[" + vsGet(t, "CONST") + "]"; break;
  default: fault("vertex program slot " + slot + " reads operand " + p + " from no register");
  }
  const s = base + "." + vsSwizzle(t, p);
  return vsGet(t, p + "_NEG") !== 0 ? "(-" + s + ")" : s;
}
function vsWrite(dst, mask, src) {
  if (dst === "A0") return "A0 = int(floor(" + src + ".x + 0.001));\n";
  return dst + "." + mask + " = " + src + "." + mask + ";\n";
}
function translateVertexProgram(words, start) {
  let body = "";
  const inputs = new Set();
  for (let slot = start; slot < KELVIN_PROGRAM_SLOTS; slot++) {
    const t = [words[slot * 4], words[slot * 4 + 1], words[slot * 4 + 2], words[slot * 4 + 3]];
    const mac = vsMac[vsGet(t, "MAC")], ilu = vsIlu[vsGet(t, "ILU")], relative = vsGet(t, "A0X") !== 0;
    const a = vsOperand(t, "A", vsGet(t, "A_R"), relative, slot), b = vsOperand(t, "B", vsGet(t, "B_R"), relative, slot), c = vsOperand(t, "C", (vsGet(t, "C_R_HIGH") << 2) | vsGet(t, "C_R_LOW"), relative, slot);
    for (const p of ["A", "B", "C"]) if (vsGet(t, p + "_MUX") === 2) inputs.add(vsGet(t, "V"));
    const outReg = vsGet(t, "OUT_R"), oMask = vsMaskOf(vsGet(t, "OUT_O_MASK")), oName = vsOut[vsGet(t, "OUT_ADDRESS") & 0xF];
    if (mac !== "NOP") {
      let expr;
      switch (mac) {
      case "MOV": expr = a; break;
      case "MUL": expr = "(" + a + " * " + b + ")"; break;
      case "ADD": expr = "(" + a + " + " + c + ")"; break;
      case "MAD": expr = "(" + a + " * " + b + " + " + c + ")"; break;
      case "DP3": expr = "vec4(dot(" + a + ".xyz, " + b + ".xyz))"; break;
      case "DPH": expr = "vec4(dot(vec4(" + a + ".xyz, 1.0), " + b + "))"; break;
      case "DP4": expr = "vec4(dot(" + a + ", " + b + "))"; break;
      case "DST": expr = "vec4(1.0, " + a + ".y * " + b + ".y, " + a + ".z, " + b + ".w)"; break;
      case "MIN": expr = "min(" + a + ", " + b + ")"; break;
      case "MAX": expr = "max(" + a + ", " + b + ")"; break;
      case "SLT": expr = "vec4(lessThan(" + a + ", " + b + "))"; break;
      case "SGE": expr = "vec4(greaterThanEqual(" + a + ", " + b + "))"; break;
      case "ARL": expr = "vec4(floor(" + a + ".x + 0.001))"; break;
      }
      body += "tmp = " + expr + ";\n";
      const rMask = vsMaskOf(vsGet(t, "OUT_MAC_MASK"));
      if (rMask !== "") body += vsWrite(vsRegister(outReg), rMask, "tmp");
      if (oMask !== "" && vsGet(t, "OUT_MUX") === 0) {
        if (vsGet(t, "OUT_ORB") === 0) fault("vertex program slot " + slot + " writes a constant register");
        body += vsWrite(oName, oMask, "tmp");
      }
    }
    if (ilu !== "NOP") {
      let expr;
      switch (ilu) {
      case "MOV": expr = c; break;
      case "RCP": expr = "vec4(1.0 / " + c + ".x)"; break;
      case "RCC": expr = "vec4(clamp(1.0 / " + c + ".x, -1.84467441e19, 1.84467441e19))"; break;
      case "RSQ": expr = "vec4(inversesqrt(abs(" + c + ".x)))"; break;
      case "EXP": expr = "vec4(exp2(floor(" + c + ".x)), fract(" + c + ".x), exp2(" + c + ".x), 1.0)"; break;
      case "LOG": expr = "vec4(floor(log2(abs(" + c + ".x))), abs(" + c + ".x) / exp2(floor(log2(abs(" + c + ".x)))), log2(abs(" + c + ".x)), 1.0)"; break;
      case "LIT": expr = "vec4(1.0, max(" + c + ".x, 0.0), (" + c + ".x > 0.0) ? pow(max(" + c + ".y, 0.0), clamp(" + c + ".w, -127.9961, 127.9961)) : 0.0, 1.0)"; break;
      }
      body += "ilu = " + expr + ";\n";
      const rMask = vsMaskOf(vsGet(t, "OUT_ILU_MASK"));
      if (rMask !== "") body += vsWrite(vsRegister(mac !== "NOP" ? 1 : outReg), rMask, "ilu");
      if (oMask !== "" && vsGet(t, "OUT_MUX") === 1) body += vsWrite(oName, oMask, "ilu");
    }
    if (vsGet(t, "FINAL") !== 0) return { body, inputs };
  }
  fault("the vertex program starting at slot " + start + " has no final instruction");
}
const programEpilogue = "oPos.x = 2.0 * (oPos.x - surfaceSize.x * 0.5) / surfaceSize.x;\noPos.y = -2.0 * (oPos.y - surfaceSize.y * 0.5) / surfaceSize.y;\n" +
  "if (clipRange.y != clipRange.x) oPos.z = 2.0 * (oPos.z - clipRange.x) / (clipRange.y - clipRange.x) - 1.0; else oPos.z = 2.0 * oPos.z - 1.0;\n" +
  "if (oPos.w < 0.0) { oPos.xyz *= oPos.w; } else { oPos.w = 1.0; }\n";
function programTail(cfg) {
  let s = cfg.pointParams ? "" : "oPts.x = max(pointSize, 1.0);\n";
  if (!cfg.specularEnable) s += "oD1 = vec4(0.0, 0.0, 0.0, 1.0);\n";
  else if (!cfg.alphaFromMaterialSpecular) s += "oD1.a = 1.0;\n";
  return s + fogSource(cfg.fog, false);
}
function vertexShaderSource(body, flat, fixed) {
  let s = "#version 300 es\nprecision highp float;\n";
  for (let i = 0; i < KELVIN_ATTRIBUTES; i++) s += "in vec4 v" + i + ";\n";
  s += "uniform vec4 c[" + KELVIN_CONSTANTS + "];\nuniform vec2 surfaceSize;\nuniform vec2 clipRange;\nuniform float pointSize;\nuniform vec3 fogParams;\n";
  if (fixed) s += "uniform vec4 lightCtx[" + (KELVIN_LIGHTS * KELVIN_LIGHT_STRIDE / 16) + "];\nuniform vec3 sceneAmbient;\nuniform vec3 materialEmission;\nuniform float materialAlpha;\nuniform vec3 specularParams;\nuniform float pointParams[" + KELVIN_POINT_PARAMS + "];\n";
  s += (flat ? "flat " : "") + "out vec4 vD0;\n" + (flat ? "flat " : "") + "out vec4 vD1;\nout vec4 vFog;\nout vec4 vT0;\nout vec4 vT1;\nout vec4 vT2;\nout vec4 vT3;\n";
  s += "const float FLOAT_MAX = 3.402823466e38;\n";
  s += "int clampIndex(int i) { return clamp(i, 0, " + (KELVIN_CONSTANTS - 1) + "); }\n";
  s += "void main() {\nvec4 R0 = vec4(0.0), R1 = vec4(0.0), R2 = vec4(0.0), R3 = vec4(0.0), R4 = vec4(0.0), R5 = vec4(0.0), R6 = vec4(0.0), R7 = vec4(0.0), R8 = vec4(0.0), R9 = vec4(0.0), R10 = vec4(0.0), R11 = vec4(0.0);\n";
  s += "vec4 oPos = vec4(0.0, 0.0, 0.0, 1.0), o1 = vec4(0.0), o2 = vec4(0.0), oD0 = vec4(0.0, 0.0, 0.0, 1.0), oD1 = vec4(0.0, 0.0, 0.0, 1.0), oFog = vec4(0.0), oPts = vec4(0.0), oB0 = vec4(0.0), oB1 = vec4(0.0), oT0 = vec4(0.0, 0.0, 0.0, 1.0), oT1 = vec4(0.0, 0.0, 0.0, 1.0), oT2 = vec4(0.0, 0.0, 0.0, 1.0), oT3 = vec4(0.0, 0.0, 0.0, 1.0), o13 = vec4(0.0), o14 = vec4(0.0);\n";
  s += "vec4 tmp = vec4(0.0), ilu = vec4(0.0);\nint A0 = 0;\n" + body;
  s += "gl_Position = oPos;\nvD0 = clamp(oD0, 0.0, 1.0);\nvD1 = clamp(oD1, 0.0, 1.0);\nvFog = oFog;\nvT0 = oT0;\nvT1 = oT1;\nvT2 = oT2;\nvT3 = oT3;\ngl_PointSize = oPts.x;\n}\n";
  return s;
}
`

// The fixed function vertex pipeline as the xemu generator writes it, skinning, texgen, lighting, fog, position and point size
const fixedFunctionTranslator = `
function skinningOf(mode) {
  switch (mode) {
  case 0: return { mix: false, count: 0 };
  case 1: return { mix: true, count: 2 };
  case 2: return { mix: false, count: 2 };
  case 3: return { mix: true, count: 3 };
  case 4: return { mix: false, count: 3 };
  case 5: return { mix: true, count: 4 };
  case 6: return { mix: false, count: 4 };
  }
  fault("skinning mode " + mode);
}
function skinned(skin, type, output, input, matrix, swizzle) {
  if (skin.count === 0) return type + " " + output + " = (" + input + " * " + matrix + "[0])." + swizzle + ";\n";
  let s = type + " " + output + " = " + type + "(0.0);\n";
  if (skin.mix) {
    s += "{\nfloat weight_i;\nfloat weight_n = 1.0;\n";
    for (let i = 0; i < skin.count; i++) {
      s += i < skin.count - 1 ? "weight_i = v1." + "xyzw"[i] + ";\nweight_n -= weight_i;\n" : "weight_i = weight_n;\n";
      s += output + " += (" + input + " * " + matrix + "[" + i + "])." + swizzle + " * weight_i;\n";
    }
    return s + "}\n";
  }
  for (let i = 0; i < skin.count; i++) s += output + " += (" + input + " * " + matrix + "[" + i + "])." + swizzle + " * v1." + "xyzw"[i] + ";\n";
  return s;
}
function lightField(i, name) {
  const b = i * (KELVIN_LIGHT_STRIDE / 16), w = (k) => "lightCtx[" + (b + (k >> 2)) + "]." + "xyzw"[k & 3];
  const v3 = (k) => "vec3(" + w(k) + ", " + w(k + 1) + ", " + w(k + 2) + ")";
  switch (name) {
  case "ambient": return v3(0);
  case "diffuse": return v3(3);
  case "specular": return v3(6);
  case "range": return w(9);
  case "half": return v3(10);
  case "direction": return v3(13);
  case "falloff": return v3(16);
  case "spotDirection": return "vec4(" + w(19) + ", " + w(20) + ", " + w(21) + ", " + w(22) + ")";
  case "position": return v3(23);
  case "attenuation": return v3(26);
  }
  fault("light field " + name);
}
function xfMat(slot) { return "mat4(c[" + slot + "], c[" + (slot + 1) + "], c[" + (slot + 2) + "], c[" + (slot + 3) + "])"; }
function xfMats(slot) { const parts = []; for (let k = 0; k < KELVIN_SKIN_MATRICES; k++) parts.push(xfMat(slot + k * KELVIN_XF_MATRIX_STRIDE)); return "mat4[" + KELVIN_SKIN_MATRICES + "](" + parts.join(", ") + ")"; }
const fixedPosition = "vec4 viewportOffset = c[" + KELVIN_XF_VIEWPORT_OFFSET + "];\nfloat m11 = 0.5 * surfaceSize.x, m22 = -0.5 * surfaceSize.y;\n" +
  "oPos.x = p.x / m11 + p.w * (viewportOffset.x / m11 - 1.0);\noPos.y = p.y / m22 + p.w * (1.0 - viewportOffset.y / m22);\n" +
  "oPos.z = clipRange.y != clipRange.x ? 2.0 * (p.z - p.w * clipRange.x) / (clipRange.y - clipRange.x) - p.w : 2.0 * p.z - p.w;\noPos.w = p.w;\n";
function fixedFunctionSource(cfg) {
  const skin = skinningOf(cfg.skinning);
  let s = "mat4 compositeMat = " + xfMat(KELVIN_XF_COMPOSITE) + ";\nmat4 modelViewMat[" + KELVIN_SKIN_MATRICES + "] = " + xfMats(KELVIN_XF_MODEL_VIEW) + ";\nmat4 invModelViewMat[" + KELVIN_SKIN_MATRICES + "] = " + xfMats(KELVIN_XF_INVERSE_MODEL_VIEW) + ";\nvec4 eyePosition = c[" + KELVIN_XF_EYE + "];\n";
  s += skinned(skin, "vec4", "tPosition", "v0", "modelViewMat", "xyzw") + skinned(skin, "vec3", "tNormal", "vec4(v2.xyz, 0.0)", "invModelViewMat", "xyz");
  if (cfg.normalization) s += "tNormal = normalize(tNormal);\n";
  const viewer = cfg.infiniteViewer ? "vec3(0.0, 0.0, 1.0)" : "normalize(tPosition.xyz)";
  for (let i = 0; i < KELVIN_STAGES; i++) for (let j = 0; j < 4; j++) {
    const c = "xyzw"[j], out = "oT" + i + "." + c + " = ", plane = "c[" + (KELVIN_XF_TEXGEN_PLANE + i * KELVIN_XF_MATRIX_STRIDE + j) + "]", mode = cfg.texgen[i * 4 + j];
    switch (mode) {
    case 0: s += out + "v" + (9 + i) + "." + c + ";\n"; break;
    case 0x2400: s += out + "dot(" + plane + ", tPosition);\n"; break;
    case 0x2401: s += out + "dot(" + plane + ", v0);\n"; break;
    case 0x2402: if (j > 1) fault("sphere map generation of coordinate " + c + " on stage " + i); s += "{\nvec3 u = " + viewer + ";\nvec3 r = reflect(u, tNormal);\nfloat invM = 1.0 / (2.0 * length(r + vec3(0.0, 0.0, 1.0)));\n" + out + "r." + c + " * invM + 0.5;\n}\n"; break;
    case 0x8512: if (j > 2) fault("reflection map generation of coordinate " + c + " on stage " + i); s += "{\nvec3 u = " + viewer + ";\nvec3 r = reflect(u, tNormal);\n" + out + "r." + c + ";\n}\n"; break;
    case 0x8511: if (j > 2) fault("normal map generation of coordinate " + c + " on stage " + i); s += out + "tNormal." + c + ";\n"; break;
    default: fault("texture coordinate generation " + hex(mode) + " on stage " + i);
    }
  }
  for (let i = 0; i < KELVIN_STAGES; i++) if (cfg.texMatrix[i]) s += "oT" + i + " = oT" + i + " * " + xfMat(KELVIN_XF_TEXTURE_MATRIX + i * KELVIN_XF_MATRIX_STRIDE) + ";\n";
  if (!cfg.lighting) s += "oD0 = v3;\noD1 = v4;\n";
  else {
    const srcOf = (k) => k === 1 ? "v3.rgb" : "v4.rgb";
    const alpha = cfg.diffuseSrc === 0 ? "materialAlpha" : cfg.diffuseSrc === 2 ? "v4.a" : "v3.a";
    if (cfg.emissionSrc === 0 || cfg.ambientSrc === 0) {
      s += "oD0 = vec4(sceneAmbient, " + alpha + ");\n";
      const added = cfg.emissionSrc !== 0 ? srcOf(cfg.emissionSrc) : cfg.ambientSrc !== 0 ? srcOf(cfg.ambientSrc) : null;
      if (added !== null) s += "oD0.rgb += " + added + " * materialEmission;\n";
    } else s += "oD0 = vec4(" + srcOf(cfg.emissionSrc) + " + " + srcOf(cfg.ambientSrc) + " * materialEmission, " + alpha + ");\n";
    s += "oD1 = vec4(0.0, 0.0, 0.0, v4.a);\n";
    if (cfg.localEye) s += "vec3 VPeye = normalize(eyePosition.xyz / eyePosition.w - tPosition.xyz / tPosition.w);\n";
    const term = (src, light) => src === 0 ? light : srcOf(src) + " * " + light;
    for (let i = 0; i < KELVIN_LIGHTS; i++) {
      const type = cfg.lights[i], L = (name) => lightField(i, name);
      if (type === 0) continue;
      s += "{\n";
      if (type === 1) s += "{\nfloat attenuation = 1.0;\nvec3 lightDirection = normalize(" + L("direction") + ");\nfloat nDotVP = max(0.0, dot(tNormal, lightDirection));\n" + (cfg.localEye ? "float nDotHV = max(0.0, dot(tNormal, normalize(lightDirection + VPeye)));\n" : "float nDotHV = max(0.0, dot(tNormal, " + L("half") + "));\n");
      else {
        s += "vec3 tPos = tPosition.xyz / tPosition.w;\nvec3 VP = " + L("position") + " - tPos;\nfloat d = length(VP);\nif (d <= " + L("range") + ") {\nVP = normalize(VP);\nvec3 att = " + L("attenuation") + ";\nfloat attenuation = 1.0 / (att.x + att.y * d + att.z * d * d);\nvec3 halfVector = normalize(VP + " + (cfg.localEye ? "VPeye" : "vec3(0.0)") + ");\nfloat nDotVP = max(0.0, dot(tNormal, VP));\nfloat nDotHV = max(0.0, dot(tNormal, halfVector));\n";
        if (type === 3) s += "vec4 spotDir = " + L("spotDirection") + ";\nfloat sp = dot(spotDir.xyz, VP) + spotDir.w;\nif (sp <= 0.0) attenuation = 0.0;\nelse if (sp < 1.0) {\nvec3 spotFalloff = " + L("falloff") + ";\nfloat spotFactor = 0.0;\nif (sp > -spotFalloff.x) {\nfloat denom = spotFalloff.y * sp + spotFalloff.z;\nif (denom == 0.0) spotFactor = 1.0;\nelse if (denom > 0.0) spotFactor = clamp((sp + spotFalloff.x) / denom, 0.0, 1.0);\n}\nattenuation *= spotFactor;\n}\n";
      }
      s += "float pf = 0.0;\nif (nDotVP > 0.0 && nDotHV > -specularParams.x) {\nfloat denom = specularParams.y * nDotHV + specularParams.z;\nif (denom == 0.0) pf = 10000.0;\nelse if (denom > 0.0) pf = clamp((nDotHV + specularParams.x) / denom, 0.0, 10000.0);\n}\n";
      s += "vec3 lightAmbient = " + L("ambient") + " * attenuation;\nvec3 lightDiffuse = " + L("diffuse") + " * attenuation * nDotVP;\nvec3 lightSpecular = " + L("specular") + " * attenuation * pf;\n";
      s += "oD0.rgb += " + term(cfg.ambientSrc, "lightAmbient") + ";\noD0.rgb += " + term(cfg.diffuseSrc, "lightDiffuse") + ";\noD1.rgb += " + term(cfg.specularSrc, "lightSpecular") + ";\n}\n}\n";
    }
  }
  if (!cfg.specularEnable) s += "oD1 = vec4(0.0, 0.0, 0.0, 1.0);\n";
  else {
    if (!cfg.separateSpecular) s += (cfg.lighting ? "oD0.rgb += oD1.rgb;\n" : "") + "oD1 = v4;\n";
    if (!cfg.alphaFromMaterialSpecular) s += "oD1.a = 1.0;\n";
  }
  s += "vec4 p = " + (skin.count === 0 ? "v0" : "tPosition") + " * compositeMat;\n" + fixedPosition;
  if (cfg.pointParams) s += "float d_e = length(v0 * modelViewMat[0]);\nfloat ptMinSize = min(pointParams[7], 63.875);\nfloat ptMaxSize = min(pointParams[3] + ptMinSize, 63.875);\noPts.x = 1.0 / sqrt(pointParams[0] + pointParams[1] * d_e + pointParams[2] * d_e * d_e) + pointParams[6];\noPts.x = clamp(oPts.x * pointParams[3] + pointParams[7], ptMinSize, ptMaxSize);\n";
  else s += "oPts.x = max(pointSize, 1.0);\n";
  return s + fogSource(cfg.fog, true);
}
function fogSource(fog, fixed) {
  if (fog === null) return "oFog = vec4(1.0);\n";
  let s = "";
  if (!fixed) s += "float fogDistance = oFog.x;\n";
  else switch (fog.genMode) {
  case 0: s += "float fogDistance = clamp(v4.a, 0.0, 1.0);\n"; break;
  case 1: s += "float fogDistance = length(tPosition.xyz);\n"; break;
  case 2: s += "vec4 fogPlane = c[" + KELVIN_XF_FOG_PLANE + "];\nfloat fogDistance = dot(fogPlane.xyz, tPosition.xyz) + fogPlane.w;\n"; break;
  case 3: s += "vec4 fogPlane = c[" + KELVIN_XF_FOG_PLANE + "];\nfloat fogDistance = abs(dot(fogPlane.xyz, tPosition.xyz) + fogPlane.w);\n"; break;
  case 6: s += "float fogDistance = v5.x;\n"; break;
  default: fault("fog coordinate generation " + fog.genMode);
  }
  let infinite = "0.0", nan = "0.0";
  switch (fog.mode) {
  case 0x2601: case 0x804: infinite = "1.0"; nan = "1.0"; s += "float fogFactor = fogParams.x + fogDistance * fogParams.y - 1.0;\n"; break;
  case 0x800: infinite = "1.0"; nan = "1.0"; s += "float fogFactor = fogParams.x + exp2(fogDistance * fogParams.y * 16.0) - 1.5;\n"; break;
  case 0x802: s += "float fogFactor = fogParams.x + exp2(fogDistance * fogParams.y * 16.0) - 1.5;\n"; break;
  case 0x801: case 0x803: s += "float fogFactor = fogParams.x + exp2(-fogDistance * fogDistance * fogParams.y * fogParams.y * 32.0) - 1.5;\n"; break;
  default: fault("fog mode " + hex(fog.mode));
  }
  if (fog.mode === 0x804 || fog.mode === 0x802 || fog.mode === 0x803) s += "fogFactor = abs(fogFactor);\n";
  return s + "if (isinf(fogDistance)) oFog = vec4(" + infinite + ");\nelse oFog = clamp(vec4(isnan(fogFactor) ? " + nan + " : fogFactor), -FLOAT_MAX, FLOAT_MAX);\n";
}
`

// The register combiner state and the texture shader programs as a GLSL fragment shader, with the xemu texture shader semantics
const combinerTranslator = `
function combinerSource(reg, stage, final) {
  switch (reg) {
  case 0: return "vec4(0.0)";
  case 1: return final ? "finalC0" : "c0[" + stage + "]";
  case 2: return final ? "finalC1" : "c1[" + stage + "]";
  case 3: return "fog";
  case 4: return "v0";
  case 5: return "v1";
  case 8: return "t0";
  case 9: return "t1";
  case 10: return "t2";
  case 11: return "t3";
  case 12: return "r0";
  case 13: return "r1";
  case 14: if (final) return "sumV1R0"; break;
  case 15: if (final) return "ef"; break;
  }
  fault("combiner input register " + reg + (final ? " of the final combiner" : " of stage " + stage));
}
function combinerMapped(expr, mapping, lanes) {
  const one = lanes === 1 ? "1.0" : "vec3(1.0)", half = lanes === 1 ? "0.5" : "vec3(0.5)", zero = lanes === 1 ? "0.0" : "vec3(0.0)";
  switch (mapping) {
  case 0: return "max(" + expr + ", " + zero + ")";
  case 1: return "(" + one + " - clamp(" + expr + ", " + zero + ", " + one + "))";
  case 2: return "(2.0 * max(" + expr + ", " + zero + ") - " + one + ")";
  case 3: return "(" + one + " - 2.0 * max(" + expr + ", " + zero + "))";
  case 4: return "(max(" + expr + ", " + zero + ") - " + half + ")";
  case 5: return "(" + half + " - max(" + expr + ", " + zero + "))";
  case 6: return expr;
  }
  return "(-" + expr + ")";
}
function combinerInput(word, shift, stage, alphaPortion, final) {
  const v = (word >>> shift) & 0xFF, src = combinerSource(v & 0xF, stage, final), alpha = (v & 0x10) !== 0, mapping = v >>> 5;
  if (alphaPortion) return combinerMapped(alpha ? src + ".a" : src + ".b", mapping, 1);
  return combinerMapped(alpha ? "vec3(" + src + ".a)" : src + ".rgb", mapping, 3);
}
function combinerDestination(reg, stage) {
  switch (reg) {
  case 0: return null;
  case 4: return "v0";
  case 5: return "v1";
  case 8: return "t0";
  case 9: return "t1";
  case 10: return "t2";
  case 11: return "t3";
  case 12: return "r0";
  case 13: return "r1";
  }
  fault("combiner output register " + reg + " of stage " + stage);
}
function combinerScaled(expr, op, lanes) {
  const half = lanes === 1 ? "0.5" : "vec3(0.5)";
  switch (op) {
  case 0: return expr;
  case 1: return "(2.0 * " + expr + ")";
  case 2: return "(4.0 * " + expr + ")";
  case 3: return "(0.5 * " + expr + ")";
  case 4: return "(" + expr + " - " + half + ")";
  case 5: return "(2.0 * (" + expr + " - " + half + "))";
  }
  fault("combiner output operation " + op);
}
function combinerPortion(icw, ocw, stage, alphaPortion, muxTest) {
  const lanes = alphaPortion ? 1 : 3, type = alphaPortion ? "float" : "vec3", clampTo = alphaPortion ? "clamp(X, -1.0, 1.0)" : "clamp(X, vec3(-1.0), vec3(1.0))";
  const a = combinerInput(icw, 24, stage, alphaPortion, false), b = combinerInput(icw, 16, stage, alphaPortion, false), c = combinerInput(icw, 8, stage, alphaPortion, false), d = combinerInput(icw, 0, stage, alphaPortion, false);
  const cdDst = ocw & 0xF, abDst = (ocw >>> 4) & 0xF, sumDst = (ocw >>> 8) & 0xF, cdDot = (ocw >>> 12) & 1, abDot = (ocw >>> 13) & 1, mux = (ocw >>> 14) & 1, op = (ocw >>> 15) & 7, cdBlue = (ocw >>> 18) & 1, abBlue = (ocw >>> 19) & 1;
  const p = alphaPortion ? "a" : "c", sfx = p + stage;
  let s = type + " ab" + sfx + " = " + (abDot && !alphaPortion ? "vec3(dot(" + a + ", " + b + "))" : "(" + a + " * " + b + ")") + ";\n";
  s += type + " cd" + sfx + " = " + (cdDot && !alphaPortion ? "vec3(dot(" + c + ", " + d + "))" : "(" + c + " * " + d + ")") + ";\n";
  s += type + " sum" + sfx + " = " + (mux ? "(" + muxTest + " ? cd" + sfx + " : ab" + sfx + ")" : "(ab" + sfx + " + cd" + sfx + ")") + ";\n";
  const write = (dst, expr, blue) => {
    const name = combinerDestination(dst, stage);
    if (name === null) return "";
    const scaled = clampTo.replace("X", combinerScaled(expr, op, lanes));
    if (alphaPortion) return name + ".a = " + scaled + ";\n";
    return name + ".rgb = " + scaled + ";\n" + (blue ? name + ".a = " + name + ".b;\n" : "");
  };
  s += write(abDst, "ab" + sfx, abBlue) + write(cdDst, "cd" + sfx, cdBlue) + write(sumDst, "sum" + sfx, 0);
  return s;
}
function finalCombiner(cw0, cw1) {
  const r0 = (cw1 & 0x40) !== 0 ? "(vec3(1.0) - r0.rgb)" : "r0.rgb", v1 = (cw1 & 0x20) !== 0 ? "(vec3(1.0) - v1.rgb)" : "v1.rgb";
  const sum = (cw1 & 0x80) !== 0 ? "clamp(" + r0 + " + " + v1 + ", vec3(0.0), vec3(1.0))" : "(" + r0 + " + " + v1 + ")";
  let s = "vec4 sumV1R0 = vec4(" + sum + ", 1.0);\n";
  const e = combinerInput(cw1, 24, 0, false, true), f = combinerInput(cw1, 16, 0, false, true);
  s += "vec4 ef = vec4(" + e + " * " + f + ", 1.0);\n";
  const a = combinerInput(cw0, 24, 0, false, true), b = combinerInput(cw0, 16, 0, false, true), c = combinerInput(cw0, 8, 0, false, true), d = combinerInput(cw0, 0, 0, false, true);
  const g = combinerInput(cw1, 8, 0, true, true);
  s += "vec3 finalColor = " + a + " * " + b + " + (vec3(1.0) - " + a + ") * " + c + " + " + d + ";\nfloat finalAlpha = " + g + ";\n";
  return s;
}
const fragmentHelpers = "vec2 texXY(vec2 c, vec4 scale) { c *= scale.xy; return scale.z != 0.0 ? vec2(c.x, 1.0 - c.y) : c; }\n" +
  "vec2 textureCoord(vec4 st, vec4 scale) { return texXY(st.w != 0.0 ? st.xy / st.w : st.xy, scale); }\n" +
  "float sign1(float x) { x *= 255.0; return (x - 128.0) / 127.0; }\n" +
  "float sign2(float x) { x *= 255.0; return x >= 128.0 ? (x - 255.5) / 127.5 : (x + 0.5) / 127.5; }\n" +
  "float sign3(float x) { x *= 255.0; return x >= 128.0 ? (x - 256.0) / 127.0 : x / 127.0; }\n" +
  "vec3 dotmap_zero_to_one(vec4 col) { return col.rgb; }\n" +
  "vec3 dotmap_minus1_to_1_d3d(vec4 col) { return vec3(sign1(col.r), sign1(col.g), sign1(col.b)); }\n" +
  "vec3 dotmap_minus1_to_1_gl(vec4 col) { return vec3(sign2(col.r), sign2(col.g), sign2(col.b)); }\n" +
  "vec3 dotmap_minus1_to_1(vec4 col) { return vec3(sign3(col.r), sign3(col.g), sign3(col.b)); }\n" +
  "vec3 dotmap_hilo_1(vec4 col) { uint hi = (uint(col.a * 255.0) << 8) | uint(col.r * 255.0); uint lo = (uint(col.g * 255.0) << 8) | uint(col.b * 255.0); return vec3(float(hi) / 65535.0, float(lo) / 65535.0, 1.0); }\n" +
  "vec2 remapCubeTo2D(vec3 t) { vec3 a = abs(t); if (a.x > a.y && a.x > a.z) return (t.x > 0.0 ? vec2(-t.z, t.y) : vec2(t.z, t.y)) / a.x; if (a.y > a.x && a.y > a.z) return (t.y > 0.0 ? vec2(t.x, -t.z) : vec2(t.x, t.z)) / a.y; return (t.z > 0.0 ? vec2(t.x, t.y) : vec2(-t.x, t.y)) / a.z; }\n" +
  "vec3 remap2DToCube(vec3 t) { vec2 st = t.xy / t.z; return normalize(vec3(1.0, st.y, -st.x)); }\n" +
  "bool checkColorKey(vec4 texel, uint key, uint mask) { uvec4 c = uvec4(texel * 255.0 + 0.5); uint color = (c.a << 24) | (c.r << 16) | (c.g << 8) | c.b; return (color & mask) == (key & mask); }\n" +
  "const float gaussian3x3[9] = float[9](1.0 / 16.0, 2.0 / 16.0, 1.0 / 16.0, 2.0 / 16.0, 4.0 / 16.0, 2.0 / 16.0, 1.0 / 16.0, 2.0 / 16.0, 1.0 / 16.0);\n" +
  "const vec2 convolution3x3[9] = vec2[9](vec2(-1.0, -1.0), vec2(0.0, -1.0), vec2(1.0, -1.0), vec2(-1.0, 0.0), vec2(0.0, 0.0), vec2(1.0, 0.0), vec2(-1.0, 1.0), vec2(0.0, 1.0), vec2(1.0, 1.0));\n";
const dotFuncs = ["dotmap_zero_to_one", "dotmap_minus1_to_1_d3d", "dotmap_minus1_to_1_gl", "dotmap_minus1_to_1", "dotmap_hilo_1"];
function stageInput(k, i) { return i === 0 ? -1 : i === 1 ? 0 : (k.otherStageInput >>> (8 + 4 * i)) & 0xF; }
function dotMapOf(k, i) { const m = (k.dotMapping >>> (4 * (i - 1))) & 0xF; if (m >= dotFuncs.length) fault("dot product mapping " + m + " on stage " + i); return dotFuncs[m]; }
function shadowStage(i, k, compareZ) {
  const t = k.stages[i].tex, func = k.shadowFunc, tex = "tex" + i, pT = "pT" + i, scale = "texScale" + i;
  if (func === 0) return "vec4 t" + i + " = vec4(0.0);\n";
  if (func === 7) return "vec4 t" + i + " = vec4(1.0);\n";
  const op = ["", "<", "==", "<=", ">", "!=", ">="][func];
  const maxDepth = t.bits === 24 ? "16777215.0" : t.floating ? "511.9375" : "65535.0";
  let s = "vec4 t" + i + ";\n{\nvec2 c = textureCoord(" + pT + ", " + scale + ");\nfloat ref = " + (compareZ ? "clamp(" + pT + ".z / " + pT + ".w, 0.0, " + maxDepth + ")" : "0.0") + ";\n";
  if (t.smooth) {
    s += "vec2 size = vec2(textureSize(" + tex + ", 0));\nvec2 p = c * size - 0.5;\nvec2 f = fract(p);\nivec2 b = ivec2(floor(p));\nivec2 last = ivec2(size) - 1;\n";
    for (const [name, dx, dy] of [["s00", 0, 0], ["s10", 1, 0], ["s01", 0, 1], ["s11", 1, 1]]) s += "float " + name + " = texelFetch(" + tex + ", clamp(b + ivec2(" + dx + ", " + dy + "), ivec2(0), last), 0).r * " + maxDepth + " " + op + " ref ? 1.0 : 0.0;\n";
    s += "t" + i + " = vec4(mix(mix(s00, s10, f.x), mix(s01, s11, f.x), f.y));\n";
  } else s += "t" + i + " = vec4(texture(" + tex + ", c).r * " + maxDepth + " " + op + " ref ? 1.0 : 0.0);\n";
  return s + "}\n";
}
function textureStage(i, k) {
  const d = k.stages[i], mode = d.mode, t = d.tex, tex = "tex" + i, pT = "pT" + i, scale = "texScale" + i, input = stageInput(k, i);
  const none = "vec4 t" + i + " = vec4(0.0);\n", sample = (coord) => "vec4 t" + i + " = texture(" + tex + ", " + coord + ");\n", scaled = (xy) => "texXY(" + xy + ", " + scale + ")";
  const cubeOrFlat = (v) => sample(t.cube ? v : "remapCubeTo2D(" + v + ")");
  const needsInput = () => { if (input < 0) fault("texture stage program " + hex(mode) + " on stage 0 reads no earlier stage"); };
  const dotOf = () => { needsInput(); return "float dot" + i + " = dot(" + pT + ".xyz, " + dotMapOf(k, i) + "(t" + input + "));\n"; };
  const onStage = (allowed) => { if (!allowed) fault("texture stage program " + hex(mode) + " on stage " + i); };
  switch (mode) {
  case 0: return "vec4 t" + i + " = vec4(0.0, 0.0, 0.0, 1.0);\n";
  case 1: case 2:
    if (t === null) return none;
    if (t.depth) return shadowStage(i, k, mode === 2);
    if (t.cube) { if (mode === 2) fault("3D projective sampling of the cube map on stage " + i); return sample("remap2DToCube(" + pT + ".xyw)"); }
    if (t.conv === 0) return sample("textureCoord(" + pT + ", " + scale + ")");
    if (t.conv !== 2) fault("texture convolution kernel " + t.conv + " on stage " + i);
    return none + "{\nvec2 c = textureCoord(" + pT + ", " + scale + ");\nvec2 texel = 1.0 / vec2(textureSize(" + tex + ", 0));\nfor (int k = 0; k < 9; k++) t" + i + " += texture(" + tex + ", c + convolution3x3[k] * texel) * gaussian3x3[k];\n}\n";
  case 3: return t === null ? none : cubeOrFlat(pT + ".xyz");
  case 4: return "vec4 t" + i + " = clamp(" + pT + ", 0.0, 1.0);\n";
  case 5: { let s = none; for (let j = 0; j < 4; j++) s += "if (" + pT + "." + "xyzw"[j] + " " + (((k.clipPlaneMode >>> (4 * i + j)) & 1) !== 0 ? ">=" : "<") + " 0.0) discard;\n"; return s; }
  case 6: case 7: {
    onStage(i >= 1); needsInput();
    if (t === null) return none;
    if (t.cube) fault("bump environment mapping into the cube map on stage " + i);
    if (mode === 6) return "vec2 dsdt" + i + " = bumpMat[" + i + "] * vec2(sign3(t" + input + ".b), sign3(t" + input + ".g));\n" + sample(scaled(pT + ".xy + dsdt" + i));
    return "vec3 dsdtl" + i + " = vec3(sign3(t" + input + ".b), sign3(t" + input + ".g), t" + input + ".r);\ndsdtl" + i + ".st = bumpMat[" + i + "] * dsdtl" + i + ".st;\n" + sample(scaled(pT + ".xy + dsdtl" + i + ".st")) + "t" + i + " = t" + i + " * (bumpScale[" + i + "] * dsdtl" + i + ".p + bumpOffset[" + i + "]);\n";
  }
  case 9: { onStage(i >= 2); const s = dotOf() + "vec2 dotST" + i + " = vec2(dot" + (i - 1) + ", dot" + i + ");\n"; return s + (t === null ? none : sample(scaled("dotST" + i))); }
  case 0xB: { onStage(i === 2); const input3 = stageInput(k, 3); const s = dotOf() + "float dot2_n = dot(pT3.xyz, " + dotMapOf(k, 3) + "(t" + input3 + "));\nvec3 n_2 = vec3(dot1, dot2, dot2_n);\n"; return s + (t === null ? none : cubeOrFlat("n_2")); }
  case 0xC: { onStage(i === 3); const s = dotOf() + "vec3 n_3 = vec3(dot1, dot2, dot3);\nvec3 e_3 = vec3(pT1.w, pT2.w, pT3.w);\nvec3 rv_3 = 2.0 * n_3 * dot(n_3, e_3) / dot(n_3, n_3) - e_3;\n"; return s + (t === null ? none : cubeOrFlat("rv_3")); }
  case 0xD: { onStage(i === 3); const s = dotOf() + "vec3 dotSTR3 = vec3(dot1, dot2, dot3);\n"; if (t !== null && t.cube) fault("dot product 3D sampling of the cube map on stage 3"); return s + (t === null ? none : sample(scaled("dotSTR3.xy"))); }
  case 0xE: { onStage(i === 3); const s = dotOf() + "vec3 dotSTR3Cube = vec3(dot1, dot2, dot3);\n"; return s + (t === null ? none : cubeOrFlat("dotSTR3Cube")); }
  case 0xF: case 0x10: { onStage(i >= 1); needsInput(); if (t === null) return none; if (t.rect) fault("dependent sampling of the linear texture on stage " + i); if (t.cube) fault("dependent sampling of the cube map on stage " + i); return sample("t" + input + (mode === 0xF ? ".ar" : ".gb")); }
  case 0x11: onStage(i === 1 || i === 2); return dotOf() + none;
  }
  fault("texture stage program " + hex(mode) + " on stage " + i);
}
function stageSource(i, k) {
  const d = k.stages[i], t = d.tex;
  let s = textureStage(i, k);
  if (t === null || d.mode === 0 || d.mode === 4 || d.mode === 5 || d.mode === 0x11) return s;
  if (t.alphaKill) s += "if (t" + i + ".a == 0.0) discard;\n";
  if (t.colorKeyMode !== 0) s += "if (checkColorKey(t" + i + ", colorKey[" + i + "], " + t.colorKeyMask + ")) " + (t.colorKeyMode === 1 ? "discard;" : t.colorKeyMode === 2 ? "t" + i + ".a = 0.0;" : "t" + i + " = vec4(0.0);") + "\n";
  return s;
}
function fragmentShaderSource(k) {
  let s = "#version 300 es\nprecision highp float;\nprecision highp sampler2D;\nprecision highp samplerCube;\n";
  s += (k.flat ? "flat " : "") + "in vec4 vD0;\n" + (k.flat ? "flat " : "") + "in vec4 vD1;\nin vec4 vFog;\nin vec4 vT0;\nin vec4 vT1;\nin vec4 vT2;\nin vec4 vT3;\n";
  for (let i = 0; i < KELVIN_STAGES; i++) s += "uniform " + (k.stages[i].tex !== null && k.stages[i].tex.cube ? "samplerCube" : "sampler2D") + " tex" + i + ";\nuniform vec4 texScale" + i + ";\n";
  s += "uniform vec4 c0[" + KELVIN_COMBINERS + "];\nuniform vec4 c1[" + KELVIN_COMBINERS + "];\nuniform vec4 finalC0;\nuniform vec4 finalC1;\nuniform vec4 fogColor;\nuniform float alphaRef;\nuniform mat2 bumpMat[" + KELVIN_STAGES + "];\nuniform float bumpScale[" + KELVIN_STAGES + "];\nuniform float bumpOffset[" + KELVIN_STAGES + "];\nuniform uint colorKey[" + KELVIN_STAGES + "];\nout vec4 fragColor;\n";
  s += fragmentHelpers;
  s += "void main() {\nvec4 v0 = vD0, v1 = vD1, r0 = vec4(0.0, 0.0, 0.0, 1.0), r1 = vec4(0.0);\nvec4 fog = vec4(fogColor.rgb, clamp(vFog.x, 0.0, 1.0));\nvec4 pT0 = vT0, pT1 = vT1, pT2 = vT2, pT3 = vT3;\n";
  for (let i = 0; i < KELVIN_STAGES; i++) s += stageSource(i, k);
  const muxTest = (k.control & 0x100) !== 0 ? "r0.a >= 0.5" : "(uint(r0.a * 255.0) & 1u) == 1u";
  for (let i = 0; i < (k.control & 0xF); i++) s += combinerPortion(k.colorICW[i], k.colorOCW[i], i, false, muxTest) + combinerPortion(k.alphaICW[i], k.alphaOCW[i], i, true, muxTest);
  s += finalCombiner(k.finalCW0, k.finalCW1);
  if (k.alphaTest) {
    const ops = { 0x200: "false", 0x201: "finalAlpha < alphaRef", 0x202: "finalAlpha == alphaRef", 0x203: "finalAlpha <= alphaRef", 0x204: "finalAlpha > alphaRef", 0x205: "finalAlpha != alphaRef", 0x206: "finalAlpha >= alphaRef", 0x207: "true" };
    if (!(k.alphaFunc in ops)) fault("alpha test function " + hex(k.alphaFunc));
    s += "if (!(" + ops[k.alphaFunc] + ")) discard;\n";
  }
  s += "fragColor = vec4(clamp(finalColor, vec3(0.0), vec3(1.0)), clamp(finalAlpha, 0.0, 1.0));\n}\n";
  return s;
}
`

// Writes the shader translators the graphics engine builds its programs with
func (e *emitter) writeShaders() {
	e.js("// ---- shader translation ----")
	e.block(vertexProgramTables)
	e.block(vertexProgramTranslator)
	e.block(fixedFunctionTranslator)
	e.block(combinerTranslator)
}

// Writes a block of page script as it stands
func (e *emitter) block(text string) {
	e.w.WriteString(text)
	e.w.WriteString("\n")
}
