package bios

import (
	"fmt"
	"strings"
)

// The graphics engine as the page runs it, executing the 3D class's methods the FIFO delivers into surfaces drawn
// through WebGL on the page's screen, the displayed surface scanned out every field
const graphicsEngineScript = `
function graphicsEngine(ctx) {
  const K = KELVIN;
  const screen = bridge.screen();
  const gl = screen === null ? null : screen.getContext("webgl2", { alpha: false, antialias: false, depth: false, stencil: false, preserveDrawingBuffer: true });
  if (screen !== null && gl === null) fault("the screen gives no WebGL2 context");
  const counts = { draws: 0, clears: 0, frames: 0, flips: 0 };
  const st = {
    surface: { x: 0, width: 0, y: 0, height: 0, format: 0, pitch: 0, zetaPitch: 0, color: 0, zeta: 0 },
    dma: new Map(),
    windowClipType: 0, windowClipH: new Uint32Array(KELVIN_WINDOW_CLIPS), windowClipV: new Uint32Array(KELVIN_WINDOW_CLIPS),
    clipMin: 0, clipMax: 0,
    arrays: [], attrib: new Float32Array(KELVIN_ATTRIBUTES * 4),
    gen: 0, constantsGen: 0, otherGen: 0, mode: 0, programLoad: 0, programStart: 0, program: new Uint32Array(KELVIN_PROGRAM_SLOTS * 4), constantLoad: 0, constants: new Float32Array(KELVIN_CONSTANTS * 4),
    textureMatrixEnable: new Uint32Array(KELVIN_STAGES), texgen: new Uint32Array(KELVIN_STAGES * 4), texgenViewModel: 0,
    lighting: 0, lightControl: 0, colorMaterial: 0, specularEnable: 0, lightMask: 0, twoSided: 0, normalization: 0, skinMode: 0, pointParamsEnable: 0,
    material: new Float32Array(4), sceneAmbient: new Float32Array(3), specularParams: new Float32Array(KELVIN_SPECULAR_WORDS), lights: new Float32Array(KELVIN_LIGHTS * KELVIN_LIGHT_STRIDE / 4), pointParams: new Float32Array(KELVIN_POINT_PARAMS),
    fog: { mode: 0, genMode: 0, params: new Float32Array(3) },
    textures: [], stagePrograms: 0, otherStageInput: 0, dotMapping: 0, clipPlaneMode: 0, shadowFunc: 0, colorKey: new Uint32Array(KELVIN_STAGES),
    combiner: { control: 0, alphaICW: new Uint32Array(KELVIN_COMBINERS), colorICW: new Uint32Array(KELVIN_COMBINERS), alphaOCW: new Uint32Array(KELVIN_COMBINERS), colorOCW: new Uint32Array(KELVIN_COMBINERS), factor0: new Uint32Array(KELVIN_COMBINERS), factor1: new Uint32Array(KELVIN_COMBINERS), finalCW0: 0, finalCW1: 0, fogFactor: new Uint32Array(2) },
    state: { alphaTest: 0, alphaFunc: 0x207, alphaRef: 0, blend: 0, sfactor: 1, dfactor: 0, blendEquation: 0x8006, blendColor: 0, cull: 0, cullFace: 0x405, frontFace: 0x900, depthTest: 0, depthFunc: 0x201, depthMask: 1, stencilTest: 0, stencilMask: 0xFF, stencilFunc: 0x207, stencilRef: 0, stencilFuncMask: 0xFF, stencilFail: 0x1E00, stencilZFail: 0x1E00, stencilZPass: 0x1E00, colorMask: 0x01010101, control0: 0, shadeMode: 0x1D01, lineWidth: 8, pointSize: 8, polyOffsetFill: 0, polyOffsetScale: 0, polyOffsetBias: 0, dither: 0, fog: 0, fogColor: 0 },
    clear: { color: 0, zstencil: 0, rectH: 0, rectV: 0 },
    draw: { primitive: 0, open: false, indices: new Uint32Array(1 << 16), count: 0, ranges: [], inline: new Float32Array(KELVIN_ATTRIBUTES * 4 * 64), inlineCount: 0, inlineWords: [] },
  };
  for (let i = 0; i < KELVIN_ATTRIBUTES; i++) { st.arrays.push({ offset: 0, context: 0, type: 2, size: 0, stride: 0 }); st.attrib[i * 4 + 3] = 1; }
  for (let i = 0; i < KELVIN_STAGES; i++) st.textures.push({ offset: 0, format: 0, address: 0, control0: 0, control1: 0, filter: 0, rect: 0, palette: 0, border: 0, bumpMatrix: new Float32Array(4), bumpScale: 0, bumpOffset: 0 });
  let shown = false;

  // ---- methods ----
  // Each ranged method by the kind of state it carries, so one table lookup finds its handler
  const KIND = new Uint8Array(0x2000 >> 2);
  const span = (first, bytes, kind) => { for (let m = first; m < first + bytes; m += 4) KIND[m >> 2] = kind; };
  span(K.transformProgram, 0x80, 1);
  span(K.transformConstant, 0x80, 2);
  span(K.vertexDataArrayOffset, 4 * KELVIN_ATTRIBUTES, 3);
  span(K.vertexDataArrayFormat, 4 * KELVIN_ATTRIBUTES, 4);
  span(K.vertexData2F, 8 * KELVIN_ATTRIBUTES, 5);
  span(K.vertexData4F, 16 * KELVIN_ATTRIBUTES, 6);
  span(K.vertexData2S, 4 * KELVIN_ATTRIBUTES, 7);
  span(K.vertexData4UB, 4 * KELVIN_ATTRIBUTES, 8);
  span(K.vertexData4S, 8 * KELVIN_ATTRIBUTES, 9);
  span(K.textureOffset, KELVIN_STAGES * KELVIN_STAGE_STRIDE, 10);
  span(K.combinerAlphaICW, 4 * KELVIN_COMBINERS, 11);
  span(K.combinerColorICW, 4 * KELVIN_COMBINERS, 12);
  span(K.combinerAlphaOCW, 4 * KELVIN_COMBINERS, 13);
  span(K.combinerColorOCW, 4 * KELVIN_COMBINERS, 14);
  span(K.combinerFactor0, 4 * KELVIN_COMBINERS, 15);
  span(K.combinerFactor1, 4 * KELVIN_COMBINERS, 16);
  span(K.specularFogFactor, 8, 17);
  span(K.colorKey, 4 * KELVIN_STAGES, 18);
  span(K.viewportOffset, 16, 19);
  span(K.viewportScale, 16, 20);
  span(K.compositeMatrix, 4 * KELVIN_MATRIX_WORDS, 21);
  span(K.projectionMatrix, 4 * KELVIN_MATRIX_WORDS, 22);
  span(K.modelViewMatrix, 4 * KELVIN_SKIN_MATRICES * KELVIN_MATRIX_WORDS, 23);
  span(K.inverseModelViewMatrix, 4 * KELVIN_SKIN_MATRICES * KELVIN_MATRIX_WORDS, 24);
  span(K.textureMatrix, KELVIN_STAGES * 4 * KELVIN_MATRIX_WORDS, 25);
  span(K.textureMatrixEnable, 4 * KELVIN_STAGES, 26);
  span(K.texgen, KELVIN_STAGES * KELVIN_TEXGEN_STRIDE, 27);
  span(K.texgenPlane, KELVIN_STAGES * KELVIN_TEXGEN_PLANE_STRIDE, 28);
  span(K.light, KELVIN_LIGHTS * KELVIN_LIGHT_STRIDE, 29);
  span(K.specularParams, 4 * KELVIN_SPECULAR_WORDS, 30);
  span(K.pointParams, 4 * KELVIN_POINT_PARAMS, 31);
  span(K.sceneAmbient, 12, 32);
  span(K.materialEmission, 12, 33);
  span(K.eyePosition, 16, 34);
  span(K.fogParams, 12, 35);
  span(K.fogPlane, 16, 36);
  span(K.windowClipHorizontal, 4 * KELVIN_WINDOW_CLIPS, 37);
  span(K.windowClipVertical, 4 * KELVIN_WINDOW_CLIPS, 38);
  span(K.backLight, KELVIN_LIGHTS * KELVIN_BACK_LIGHT_STRIDE, 39); span(K.backMaterialAlpha, K.backMaterialEmission + 12 - K.backMaterialAlpha, 39); span(K.specularParamsBack, 4 * KELVIN_SPECULAR_WORDS, 39);
  span(K.eyeVector, 12, 40); span(K.eyeDirection, 12, 40); for (const m of KELVIN_KEPT) KIND[m >> 2] = 40;
  function method(m, data) {
    switch (KIND[m >> 2]) {
    case 1: { st.gen++; const slot = ((m - K.transformProgram) >>> 2) & 3; if (st.programLoad >= KELVIN_PROGRAM_SLOTS) fault("vertex program slot " + st.programLoad + " lies past the program store"); st.program[st.programLoad * 4 + slot] = data; if (slot === 3) st.programLoad++; return; }
    case 2: { st.constantsGen++; const slot = ((m - K.transformConstant) >>> 2) & 3; if (st.constantLoad >= KELVIN_CONSTANTS) fault("vertex constant " + st.constantLoad + " lies past the constant store"); st.constants[st.constantLoad * 4 + slot] = fromBits(data); if (slot === 3) st.constantLoad++; return; }
    case 3: { const a = st.arrays[(m - K.vertexDataArrayOffset) >>> 2]; a.offset = data & 0x7FFFFFFF; a.context = data >>> 31; return; }
    case 4: { const a = st.arrays[(m - K.vertexDataArrayFormat) >>> 2]; a.type = data & 0xF; a.size = (data >>> 4) & 0xF; a.stride = data >>> 8; return; }
    case 5: { const i = (m - K.vertexData2F) >>> 3, part = ((m - K.vertexData2F) >>> 2) & 1; st.attrib[i * 4 + part] = fromBits(data); if (i === 0 && part === 1) { st.attrib[2] = 0; st.attrib[3] = 1; emitVertex(); } return; }
    case 6: { const i = (m - K.vertexData4F) >>> 4, part = ((m - K.vertexData4F) >>> 2) & 3; st.attrib[i * 4 + part] = fromBits(data); if (i === 0 && part === 3) emitVertex(); return; }
    case 7: { const i = (m - K.vertexData2S) >>> 2; st.attrib[i * 4] = (data << 16) >> 16; st.attrib[i * 4 + 1] = data >> 16; if (i === 0) { st.attrib[2] = 0; st.attrib[3] = 1; emitVertex(); } return; }
    case 8: { const i = (m - K.vertexData4UB) >>> 2; st.attrib[i * 4] = (data & 0xFF) / 255; st.attrib[i * 4 + 1] = ((data >>> 8) & 0xFF) / 255; st.attrib[i * 4 + 2] = ((data >>> 16) & 0xFF) / 255; st.attrib[i * 4 + 3] = (data >>> 24) / 255; if (i === 0) emitVertex(); return; }
    case 9: { const i = (m - K.vertexData4S) >>> 3, pair = ((m - K.vertexData4S) >>> 2) & 1; st.attrib[i * 4 + pair * 2] = (data << 16) >> 16; st.attrib[i * 4 + pair * 2 + 1] = data >> 16; if (i === 0 && pair === 1) emitVertex(); return; }
    case 10: {
      const stage = Math.floor((m - K.textureOffset) / KELVIN_STAGE_STRIDE), t = st.textures[stage], at = m - stage * KELVIN_STAGE_STRIDE;
      if (at >= K.textureBumpMatrix && at < K.textureBumpMatrix + 16) { st.otherGen++; t.bumpMatrix[(at - K.textureBumpMatrix) >>> 2] = fromBits(data); return; }
      switch (at) {
      case K.textureOffset: t.offset = data; return;
      case K.textureFormat: st.gen++; t.format = data; return;
      case K.textureAddress: t.address = data; return;
      case K.textureControl0: st.gen++; t.control0 = data; return;
      case K.textureControl1: t.control1 = data; return;
      case K.textureFilter: st.gen++; t.filter = data; return;
      case K.texturePalette: t.palette = data; return;
      case K.textureImageRect: t.rect = data; return;
      case K.textureBorderColor: t.border = data; return;
      case K.textureBumpScale: st.otherGen++; t.bumpScale = fromBits(data); return;
      case K.textureBumpOffset: st.otherGen++; t.bumpOffset = fromBits(data); return;
      }
      fault("texture stage " + stage + " method " + hex(m) + " with " + hex(data));
    }
    case 11: { st.gen++; st.combiner.alphaICW[(m - K.combinerAlphaICW) >>> 2] = data; return; }
    case 12: { st.gen++; st.combiner.colorICW[(m - K.combinerColorICW) >>> 2] = data; return; }
    case 13: { st.gen++; st.combiner.alphaOCW[(m - K.combinerAlphaOCW) >>> 2] = data; return; }
    case 14: { st.gen++; st.combiner.colorOCW[(m - K.combinerColorOCW) >>> 2] = data; return; }
    case 15: { st.otherGen++; st.combiner.factor0[(m - K.combinerFactor0) >>> 2] = data; return; }
    case 16: { st.otherGen++; st.combiner.factor1[(m - K.combinerFactor1) >>> 2] = data; return; }
    case 17: { st.otherGen++; st.combiner.fogFactor[(m - K.specularFogFactor) >>> 2] = data; return; }
    case 18: { st.otherGen++; st.colorKey[(m - K.colorKey) >>> 2] = data; return; }
    case 19: { st.constantsGen++; st.constants[KELVIN_XF_VIEWPORT_OFFSET * 4 + ((m - K.viewportOffset) >>> 2)] = fromBits(data); return; }
    case 20: { st.constantsGen++; st.constants[KELVIN_XF_VIEWPORT_SCALE * 4 + ((m - K.viewportScale) >>> 2)] = fromBits(data); return; }
    case 21: { st.constantsGen++; st.constants[KELVIN_XF_COMPOSITE * 4 + ((m - K.compositeMatrix) >>> 2)] = fromBits(data); return; }
    case 22: { st.constantsGen++; st.constants[KELVIN_XF_PROJECTION * 4 + ((m - K.projectionMatrix) >>> 2)] = fromBits(data); return; }
    case 23: { st.constantsGen++; st.constants[xfMatrixWord(KELVIN_XF_MODEL_VIEW, (m - K.modelViewMatrix) >>> 2)] = fromBits(data); return; }
    case 24: { st.constantsGen++; st.constants[xfMatrixWord(KELVIN_XF_INVERSE_MODEL_VIEW, (m - K.inverseModelViewMatrix) >>> 2)] = fromBits(data); return; }
    case 25: { st.constantsGen++; st.constants[xfMatrixWord(KELVIN_XF_TEXTURE_MATRIX, (m - K.textureMatrix) >>> 2)] = fromBits(data); return; }
    case 26: { st.gen++; st.textureMatrixEnable[(m - K.textureMatrixEnable) >>> 2] = data; return; }
    case 27: { st.gen++; st.texgen[(m - K.texgen) >>> 2] = data; return; }
    case 28: { st.constantsGen++; st.constants[xfMatrixWord(KELVIN_XF_TEXGEN_PLANE, (m - K.texgenPlane) >>> 2)] = fromBits(data); return; }
    case 29: { st.otherGen++; st.lights[(m - K.light) >>> 2] = fromBits(data); return; }
    case 30: { st.otherGen++; st.specularParams[(m - K.specularParams) >>> 2] = fromBits(data); return; }
    case 31: { st.otherGen++; st.pointParams[(m - K.pointParams) >>> 2] = fromBits(data); return; }
    case 32: { st.otherGen++; st.sceneAmbient[(m - K.sceneAmbient) >>> 2] = fromBits(data); return; }
    case 33: { st.otherGen++; st.material[(m - K.materialEmission) >>> 2] = fromBits(data); return; }
    case 34: { st.constantsGen++; st.constants[KELVIN_XF_EYE * 4 + ((m - K.eyePosition) >>> 2)] = fromBits(data); return; }
    case 35: { st.otherGen++; st.fog.params[(m - K.fogParams) >>> 2] = fromBits(data); return; }
    case 36: { st.constantsGen++; st.constants[KELVIN_XF_FOG_PLANE * 4 + ((m - K.fogPlane) >>> 2)] = fromBits(data); return; }
    case 37: { st.windowClipH[(m - K.windowClipHorizontal) >>> 2] = data; return; }
    case 38: { st.windowClipV[(m - K.windowClipVertical) >>> 2] = data; return; }
    // The back lights, back material and back specular curve feed two sided lighting only, which stops the page when enabled
    case 39: return;
    // The CPU folds the eye vector and direction into each light's half vector, and the unnamed methods are kept
    case 40: return;
    }
    switch (m) {
    case K.noOperation: case K.waitForIdle: case K.contextDMANotifies: case K.contextDMAState: case K.semaphoreOffset: case K.semaphoreRelease: case K.flipRead: case K.flipWrite: case K.flipModulo: case K.flipIncrement: case K.flipStall: case K.contextDMAReport: case K.contextDMASemaphore: case K.swathWidth: case K.programWriteEnable: return;
    case K.edgeFlagEnable: case K.invalidateVertexCache: case K.compressZBuffer: case K.shadowZSlopeThreshold: case K.antiAliasingControl: case K.flatShadeOp: case K.pointSmoothEnable: case K.lineSmoothEnable: case K.polySmoothEnable: case K.polyOffsetPointEnable: case K.polyOffsetLineEnable: case K.occludeZStencilEnable: case K.logicOp: return;
    case K.contextDMAA: case K.contextDMAB: case K.contextDMAColor: case K.contextDMAZeta: case K.contextDMAVertexA: case K.contextDMAVertexB: st.dma.set(m, data); return;
    case K.surfaceClipHorizontal: st.surface.x = data & 0xFFFF; st.surface.width = data >>> 16; return;
    case K.surfaceClipVertical: st.surface.y = data & 0xFFFF; st.surface.height = data >>> 16; return;
    case K.surfaceFormat: st.surface.format = data; return;
    case K.surfacePitch: st.surface.pitch = data & 0xFFFF; st.surface.zetaPitch = data >>> 16; return;
    case K.surfaceColorOffset: st.surface.color = data; return;
    case K.surfaceZetaOffset: st.surface.zeta = data; return;
    case K.combinerSpecularFogCW0: st.gen++; st.combiner.finalCW0 = data; return;
    case K.combinerSpecularFogCW1: st.gen++; st.combiner.finalCW1 = data; return;
    case K.control0: st.state.control0 = data; return;
    case K.lightControl: st.gen++; st.lightControl = data; return;
    case K.colorMaterial: st.gen++; st.colorMaterial = data; return;
    case K.fogMode: st.gen++; st.fog.mode = data; return;
    case K.fogGenMode: st.gen++; st.fog.genMode = data; return;
    case K.fogEnable: st.gen++; st.state.fog = data; return;
    case K.fogColor: st.otherGen++; st.state.fogColor = data; return;
    case K.windowClipType: st.windowClipType = data; return;
    case K.alphaTestEnable: st.gen++; st.state.alphaTest = data; return;
    case K.blendEnable: st.state.blend = data; return;
    case K.cullFaceEnable: st.state.cull = data; return;
    case K.depthTestEnable: st.state.depthTest = data; return;
    case K.ditherEnable: st.state.dither = data; return;
    case K.lightingEnable: st.gen++; st.lighting = data; return;
    case K.pointParamsEnable: st.gen++; st.pointParamsEnable = data; return;
    case K.skinMode: st.gen++; st.skinMode = data; return;
    case K.stencilTestEnable: st.state.stencilTest = data; return;
    case K.polyOffsetFillEnable: st.state.polyOffsetFill = data; return;
    case K.alphaFunc: st.gen++; st.state.alphaFunc = data; return;
    case K.alphaRef: st.otherGen++; st.state.alphaRef = data; return;
    case K.blendFuncSource: st.state.sfactor = data; return;
    case K.blendFuncDestination: st.state.dfactor = data; return;
    case K.blendColor: st.state.blendColor = data; return;
    case K.blendEquation: st.state.blendEquation = data; return;
    case K.depthFunc: st.state.depthFunc = data; return;
    case K.colorMask: st.state.colorMask = data; return;
    case K.depthMask: st.state.depthMask = data; return;
    case K.stencilMask: st.state.stencilMask = data; return;
    case K.stencilFunc: st.state.stencilFunc = data; return;
    case K.stencilFuncRef: st.state.stencilRef = data; return;
    case K.stencilFuncMask: st.state.stencilFuncMask = data; return;
    case K.stencilOpFail: st.state.stencilFail = data; return;
    case K.stencilOpZFail: st.state.stencilZFail = data; return;
    case K.stencilOpZPass: st.state.stencilZPass = data; return;
    case K.shadeMode: st.gen++; st.state.shadeMode = data; return;
    case K.lineWidth: st.state.lineWidth = data; return;
    case K.polyOffsetScale: st.state.polyOffsetScale = fromBits(data); return;
    case K.polyOffsetBias: st.state.polyOffsetBias = fromBits(data); return;
    case K.frontPolygonMode: case K.backPolygonMode: if (data !== 0x1B02) fault("polygon mode " + hex(data)); return;
    case K.clipMin: st.otherGen++; st.clipMin = fromBits(data); return;
    case K.clipMax: st.otherGen++; st.clipMax = fromBits(data); return;
    case K.cullFace: st.state.cullFace = data; return;
    case K.frontFace: st.state.frontFace = data; return;
    case K.normalizationEnable: st.gen++; st.normalization = data; return;
    case K.materialAlpha: st.otherGen++; st.material[3] = fromBits(data); return;
    case K.specularEnable: st.gen++; st.specularEnable = data; return;
    case K.lightEnableMask: st.gen++; st.lightMask = data; return;
    case K.pointSize: st.otherGen++; st.state.pointSize = data; return;
    case K.texgenViewModel: st.gen++; st.texgenViewModel = data; return;
    case K.polygonStippleEnable: if (data !== 0) fault("polygon stippling"); return;
    case K.twoSidedLighting: st.gen++; st.twoSided = data; return;
    case K.shaderClipPlaneMode: st.gen++; st.clipPlaneMode = data; return;
    case K.beginEnd: if (data !== 0) begin(data); else end(); return;
    case K.arrayElement16: pushIndex(data & 0xFFFF); pushIndex(data >>> 16); return;
    case K.arrayElement32: pushIndex(data); return;
    case K.drawArrays: st.draw.ranges.push(data & 0xFFFFFF, (data >>> 24) + 1); return;
    case K.inlineArray: st.draw.inlineWords.push(data); return;
    case K.zMinMaxControl: if (((data >>> 4) & 0xF) !== 0) fault("depth clamping"); return;
    case K.zStencilClearValue: st.clear.zstencil = data; return;
    case K.colorClearValue: st.clear.color = data; return;
    case K.clearRectHorizontal: st.clear.rectH = data; return;
    case K.clearRectVertical: st.clear.rectV = data; return;
    case K.clearSurface: clearSurface(data); return;
    case K.logicOpEnable: if (data !== 0) fault("logic operations on the colour buffer"); return;
    case K.combinerControl: st.otherGen++; st.gen++; st.combiner.control = data; return;
    case K.shadowDepthFunc: st.gen++; st.shadowFunc = data; return;
    case K.shaderStageProgram: st.gen++; st.stagePrograms = data; return;
    case K.shaderDotMapping: st.gen++; st.dotMapping = data; return;
    case K.shaderOtherStageInput: st.gen++; st.otherStageInput = data; return;
    case K.transformExecutionMode: st.gen++; st.mode = data; return;
    case K.transformProgramLoad: st.programLoad = data; return;
    case K.transformProgramStart: st.gen++; st.programStart = data; return;
    case K.transformConstantLoad: st.constantLoad = data; return;
    }
    fault("3D class method " + hex(m) + " with " + hex(data));
  }

  // ---- memory ----
  function xfMatrixWord(slot, word) { return (slot + (word >> 4) * KELVIN_XF_MATRIX_STRIDE) * 4 + (word & 15); }
  function checkError(what) { const e = gl.getError(); if (e !== 0) fault("WebGL reports error " + hex(e) + " after " + what); }
  function dmaBase(contextMethod) {
    const inst = st.dma.get(contextMethod);
    if (inst === undefined) fault("the 3D class has no memory object for method " + hex(contextMethod));
    return ctx.memoryObject(inst, "the 3D class's context for method " + hex(contextMethod)).base;
  }
  function bytesAt(at, n, what) { if (at + n > RAM_BYTES) fault(what + " at " + hex(at) + " runs " + n + " bytes past memory"); return ram.subarray(at, at + n); }

  // ---- surfaces ----
  const surfaces = new Map(), depthBuffers = new Map();
  function depthBufferFor(offset, width, height) {
    let d = depthBuffers.get(offset);
    if (d !== undefined && (d.width !== width || d.height !== height)) { for (const s of surfaces.values()) { const fb = s.fbs.get(d.tex); if (fb !== undefined) { gl.deleteFramebuffer(fb); s.fbs.delete(d.tex); if (s.fb === fb) s.fb = null; } } gl.deleteTexture(d.tex); depthBuffers.delete(offset); forgetTextures(); glState.fb = undefined; d = undefined; }
    if (d === undefined) {
      const tex = gl.createTexture();
      bindTexture(0, gl.TEXTURE_2D, tex);
      gl.texStorage2D(gl.TEXTURE_2D, 1, gl.DEPTH24_STENCIL8, width, height);
      d = { offset, width, height, tex, min: -1, mag: -1, wrapS: -1, wrapT: -1 };
      texParams(d, gl.TEXTURE_2D, gl.NEAREST, gl.NEAREST, gl.CLAMP_TO_EDGE, gl.CLAMP_TO_EDGE);
      depthBuffers.set(offset, d);
    }
    return d;
  }
  function surfaceFor(offset, width, height, format, zeta) {
    let s = surfaces.get(offset), created = false;
    if (s !== undefined && (s.width !== width || s.height !== height)) { for (const fb of s.fbs.values()) gl.deleteFramebuffer(fb); gl.deleteTexture(s.color); surfaces.delete(offset); forgetTextures(); glState.fb = undefined; s = undefined; }
    const depth = zeta !== 0 ? depthBufferFor(zeta, width, height) : null;
    if (s === undefined) {
      const color = gl.createTexture();
      bindTexture(0, gl.TEXTURE_2D, color);
      gl.texStorage2D(gl.TEXTURE_2D, 1, gl.RGBA8, width, height);
      s = { offset, width, height, format, zeta, fb: null, fbs: new Map(), color, depth: null, min: -1, mag: -1, wrapS: -1, wrapT: -1 };
      texParams(s, gl.TEXTURE_2D, gl.LINEAR, gl.LINEAR, gl.CLAMP_TO_EDGE, gl.CLAMP_TO_EDGE);
      surfaces.set(offset, s);
    }
    s.format = format;
    const depthTex = depth === null ? null : depth.tex;
    let fb = s.fbs.get(depthTex);
    if (fb === undefined) {
      fb = gl.createFramebuffer();
      bindFramebuffer(fb);
      gl.framebufferTexture2D(gl.FRAMEBUFFER, gl.COLOR_ATTACHMENT0, gl.TEXTURE_2D, s.color, 0);
      gl.framebufferTexture2D(gl.FRAMEBUFFER, gl.DEPTH_STENCIL_ATTACHMENT, gl.TEXTURE_2D, depthTex, 0);
      if (gl.checkFramebufferStatus(gl.FRAMEBUFFER) !== gl.FRAMEBUFFER_COMPLETE) fault("the surface at " + hex(offset) + " of " + width + "x" + height + " cannot be drawn into");
      s.fbs.set(depthTex, fb);
    }
    s.fb = fb;
    s.depth = depth;
    s.zeta = zeta;
    return s;
  }
  function target() {
    const su = st.surface;
    if (((su.format >>> 12) & 0xF) !== 0) fault("anti aliased surface format " + hex(su.format));
    let width = su.width, height = su.height;
    if (((su.format >>> 8) & 0xF) === 2) { width = 1 << ((su.format >>> 16) & 0xFF); height = 1 << (su.format >>> 24); }
    if (width === 0 || height === 0) fault("drawing into a surface with no size");
    if (su.x !== 0 || su.y !== 0) fault("a surface clip starting at " + su.x + "," + su.y);
    const color = su.format & 0xF;
    if (color < 1 || color > 8) fault("surface colour format " + color);
    return surfaceFor(su.color, width, height, su.format, su.zeta);
  }
  function surfaceOpaque(format) { const color = format & 0xF; return color !== 8 && color !== 6 && color !== 7; }
  function argb(v, out) { out[0] = ((v >>> 16) & 0xFF) / 255; out[1] = ((v >>> 8) & 0xFF) / 255; out[2] = (v & 0xFF) / 255; out[3] = (v >>> 24) / 255; return out; }
  function abgr(v, out) { out[0] = (v & 0xFF) / 255; out[1] = ((v >>> 8) & 0xFF) / 255; out[2] = ((v >>> 16) & 0xFF) / 255; out[3] = (v >>> 24) / 255; return out; }
  const color4 = new Float32Array(4);

  // ---- clears ----
  function clearSurface(flags) {
    counts.clears++;
    if (gl === null) return;
    const s = target();
    bindFramebuffer(s.fb);
    const x0 = st.clear.rectH & 0xFFFF, x1 = st.clear.rectH >>> 16, y0 = st.clear.rectV & 0xFFFF, y1 = st.clear.rectV >>> 16;
    scissor(true, x0, s.height - 1 - y1, x1 - x0 + 1, y1 - y0 + 1);
    let bits = 0;
    if ((flags & 0xF0) !== 0) { colorMask(((flags & 0x10) !== 0 ? 8 : 0) | ((flags & 0x20) !== 0 ? 4 : 0) | ((flags & 0x40) !== 0 ? 2 : 0) | ((flags & 0x80) !== 0 ? 1 : 0)); argb(st.clear.color, color4); gl.clearColor(color4[0], color4[1], color4[2], color4[3]); bits |= gl.COLOR_BUFFER_BIT; }
    const z16 = ((s.format >>> 4) & 0xF) === 1;
    if (s.depth !== null && (flags & 1) !== 0) { depthMask(true); gl.clearDepth(z16 ? (st.clear.zstencil & 0xFFFF) / 65535 : (st.clear.zstencil >>> 8) / 16777215); bits |= gl.DEPTH_BUFFER_BIT; }
    if (s.depth !== null && !z16 && (flags & 2) !== 0) { stencilMask(0xFF); gl.clearStencil(st.clear.zstencil & 0xFF); bits |= gl.STENCIL_BUFFER_BIT; }
    if (bits !== 0) gl.clear(bits);
  }
  // ---- draws ----
  function begin(primitive) { const d = st.draw; d.primitive = primitive; d.open = true; d.count = 0; d.ranges.length = 0; d.inlineCount = 0; d.inlineWords.length = 0; }
  function pushIndex(i) { const d = st.draw; if (d.count === d.indices.length) { const grown = new Uint32Array(d.indices.length * 2); grown.set(d.indices); d.indices = grown; } d.indices[d.count++] = i; }
  function emitVertex() {
    const d = st.draw;
    if (!d.open) return;
    const stride = KELVIN_ATTRIBUTES * 4;
    if ((d.inlineCount + 1) * stride > d.inline.length) { const grown = new Float32Array(d.inline.length * 2); grown.set(d.inline); d.inline = grown; }
    d.inline.set(st.attrib, d.inlineCount * stride);
    d.inlineCount++;
  }
  function end() {
    const d = st.draw;
    counts.draws++;
    if (gl !== null && (d.count > 0 || d.ranges.length > 0 || d.inlineCount > 0 || d.inlineWords.length > 0)) draw(d);
    d.open = false;
  }
  function glPrimitive(p) {
    switch (p) { case 1: return gl.POINTS; case 2: return gl.LINES; case 3: return gl.LINE_LOOP; case 4: return gl.LINE_STRIP; case 5: case 8: return gl.TRIANGLES; case 6: case 9: return gl.TRIANGLE_STRIP; case 7: case 10: return gl.TRIANGLE_FAN; }
    fault("primitive " + p);
  }
  function quadsToTriangles(indices) {
    const quads = indices.length >> 2, out = new Uint32Array(quads * 6);
    for (let q = 0; q < quads; q++) { const a = indices[q * 4], b = indices[q * 4 + 1], c = indices[q * 4 + 2], e = indices[q * 4 + 3]; out.set([a, b, c, a, c, e], q * 6); }
    return out;
  }
  const drawVAO = gl === null ? null : gl.createVertexArray(), blitVAO = gl === null ? null : gl.createVertexArray(), ARRAY_CACHE_LIMIT = 4096, ERROR_CHECK_FRAMES = 64;
  const arrayBuffers = [], indexBuffer = gl === null ? null : gl.createBuffer();
  if (gl !== null) for (let i = 0; i < KELVIN_ATTRIBUTES; i++) arrayBuffers.push(gl.createBuffer());
  function attributeBytes(a) { switch (a.type) { case 0: case 4: return a.size; case 1: case 5: return 2 * a.size; case 2: return 4 * a.size; case 6: return 4; } fault("vertex array type " + a.type); }
  function draw(d) {
    const s = target();
    bindFramebuffer(s.fb);
    bindVertexArray(drawVAO);
    viewport(s.width, s.height);
    applyState(s);
    const prog = linkedProgram();
    useProgram(prog.id);
    setUniforms(prog, s);
    bindTextures(prog);
    const mode = glPrimitive(d.primitive), quads = d.primitive === 8;
    if (d.inlineCount > 0) {
      bindInline(prog, d.inline.subarray(0, d.inlineCount * KELVIN_ATTRIBUTES * 4));
      if (quads) { const idx = new Uint32Array(d.inlineCount); for (let i = 0; i < idx.length; i++) idx[i] = i; drawIndices(mode, quadsToTriangles(idx)); } else gl.drawArrays(mode, 0, d.inlineCount);
    }
    if (d.inlineWords.length > 0) {
      const n = bindInlineArray(prog, d.inlineWords);
      if (quads) { const idx = new Uint32Array(n); for (let i = 0; i < idx.length; i++) idx[i] = i; drawIndices(mode, quadsToTriangles(idx)); } else gl.drawArrays(mode, 0, n);
    }
    if (d.count > 0 || d.ranges.length > 0) {
      let lo = 0xFFFFFFFF, hi = 0;
      for (let i = 0; i < d.count; i++) { const v = d.indices[i]; if (v < lo) lo = v; if (v > hi) hi = v; }
      for (let i = 0; i < d.ranges.length; i += 2) { if (d.ranges[i] < lo) lo = d.ranges[i]; if (d.ranges[i] + d.ranges[i + 1] - 1 > hi) hi = d.ranges[i] + d.ranges[i + 1] - 1; }
      bindArrays(prog, lo, hi);
      if (d.count > 0) {
        let idx = new Uint32Array(d.count);
        for (let i = 0; i < d.count; i++) idx[i] = d.indices[i] - lo;
        drawIndices(mode, quads ? quadsToTriangles(idx) : idx);
      }
      for (let i = 0; i < d.ranges.length; i += 2) {
        if (quads) { const idx = new Uint32Array(d.ranges[i + 1]); for (let k = 0; k < idx.length; k++) idx[k] = d.ranges[i] - lo + k; drawIndices(mode, quadsToTriangles(idx)); }
        else gl.drawArrays(mode, d.ranges[i] - lo, d.ranges[i + 1]);
      }
    }
  }
  function drawIndices(mode, idx) { if (!glState.indexBound) { glState.indexBound = true; gl.bindBuffer(gl.ELEMENT_ARRAY_BUFFER, indexBuffer); } gl.bufferData(gl.ELEMENT_ARRAY_BUFFER, idx, gl.STREAM_DRAW); gl.drawElements(mode, idx.length, gl.UNSIGNED_INT, 0); }
  function packedNormals(view, n, stride) {
    const out = new Float32Array(n * 3), words = new DataView(view.buffer, view.byteOffset, view.byteLength);
    for (let i = 0; i < n; i++) { const w = words.getUint32(i * stride, true); out[i * 3] = Math.max(((w << 21) >> 21) / 1023, -1); out[i * 3 + 1] = Math.max(((w << 10) >> 21) / 1023, -1); out[i * 3 + 2] = Math.max((w >> 22) / 511, -1); }
    return out;
  }
  // The vertex arrays bound from buffers kept by the pages they lie in, a buffer refilled only once its pages
  // were written, the attributes with no array holding their constants
  const arrayCache = new Map();
  function arrayBuffer(start, bytes, arr, n, view) {
    const key = start + ":" + bytes + ":" + arr.type + ":" + arr.stride, writes = writesOver(start, bytes);
    let e = arrayCache.get(key);
    if (e === undefined) {
      if (arrayCache.size >= ARRAY_CACHE_LIMIT) { for (const old of arrayCache.values()) gl.deleteBuffer(old.id); arrayCache.clear(); for (const slot of attribSlots) slot.buffer = null; }
      e = { id: gl.createBuffer(), writes: writes - 1 };
      arrayCache.set(key, e);
    }
    if (e.writes !== writes) {
      e.writes = writes;
      gl.bindBuffer(gl.ARRAY_BUFFER, e.id);
      let data = view;
      if (arr.type === 0) { data = new Uint8Array(view); for (let i = 0; i < n; i++) { const at = i * arr.stride, b = data[at]; data[at] = data[at + 2]; data[at + 2] = b; } }
      else if (arr.type === 6) data = packedNormals(view, n, arr.stride);
      gl.bufferData(gl.ARRAY_BUFFER, data, gl.STATIC_DRAW);
    }
    return e.id;
  }
  function bindArrays(prog, lo, hi) {
    for (let a = 0; a < KELVIN_ATTRIBUTES; a++) {
      const loc = prog.attribs[a];
      if (loc < 0) continue;
      const arr = st.arrays[a];
      if (arr.size === 0) { constantAttrib(loc, st.attrib[a * 4], st.attrib[a * 4 + 1], st.attrib[a * 4 + 2], st.attrib[a * 4 + 3]); continue; }
      const n = hi - lo + 1, start = dmaBase(arr.context !== 0 ? K.contextDMAVertexB : K.contextDMAVertexA) + arr.offset + lo * arr.stride, bytes = (n - 1) * arr.stride + attributeBytes(arr);
      const view = bytesAt(start, bytes, "vertex array " + a), id = arrayBuffer(start, bytes, arr, n, view);
      switch (arr.type) {
      case 0: pointer(loc, id, arr.size, gl.UNSIGNED_BYTE, true, arr.stride); break;
      case 1: pointer(loc, id, arr.size, gl.SHORT, true, arr.stride); break;
      case 2: pointer(loc, id, arr.size, gl.FLOAT, false, arr.stride); break;
      case 4: pointer(loc, id, arr.size, gl.UNSIGNED_BYTE, true, arr.stride); break;
      case 5: pointer(loc, id, arr.size, gl.SHORT, false, arr.stride); break;
      case 6: pointer(loc, id, 3, gl.FLOAT, false, 12); break;
      default: fault("vertex array type " + arr.type);
      }
    }
  }
  function bindInline(prog, floats) {
    const n = floats.length / (KELVIN_ATTRIBUTES * 4);
    for (let a = 0; a < KELVIN_ATTRIBUTES; a++) {
      const loc = prog.attribs[a];
      if (loc < 0) continue;
      const column = new Float32Array(n * 4);
      for (let v = 0; v < n; v++) column.set(floats.subarray(v * KELVIN_ATTRIBUTES * 4 + a * 4, v * KELVIN_ATTRIBUTES * 4 + a * 4 + 4), v * 4);
      gl.bindBuffer(gl.ARRAY_BUFFER, arrayBuffers[a]);
      gl.bufferData(gl.ARRAY_BUFFER, column, gl.STREAM_DRAW);
      pointer(loc, arrayBuffers[a], 4, gl.FLOAT, false, 0);
    }
  }
  function bindInlineArray(prog, words) {
    const bytes = new Uint8Array(new Uint32Array(words).buffer), view = new DataView(bytes.buffer);
    let stride = 0;
    for (const a of st.arrays) if (a.size !== 0) stride += attributeBytes(a);
    if (stride === 0) fault("an inline vertex array with no attribute enabled");
    const n = Math.floor(bytes.length / stride), floats = new Float32Array(n * KELVIN_ATTRIBUTES * 4);
    for (let v = 0; v < n; v++) {
      let at = v * stride;
      for (let a = 0; a < KELVIN_ATTRIBUTES; a++) {
        const arr = st.arrays[a], out = v * KELVIN_ATTRIBUTES * 4 + a * 4;
        floats[out + 3] = 1;
        if (arr.size === 0) { floats.set(st.attrib.subarray(a * 4, a * 4 + 4), out); continue; }
        switch (arr.type) {
        case 0: floats[out] = bytes[at + 2] / 255; floats[out + 1] = bytes[at + 1] / 255; floats[out + 2] = bytes[at] / 255; floats[out + 3] = bytes[at + 3] / 255; break;
        case 1: for (let c = 0; c < arr.size; c++) floats[out + c] = Math.max(view.getInt16(at + 2 * c, true) / 32767, -1); break;
        case 2: for (let c = 0; c < arr.size; c++) floats[out + c] = view.getFloat32(at + 4 * c, true); break;
        case 4: for (let c = 0; c < arr.size; c++) floats[out + c] = bytes[at + c] / 255; break;
        case 5: for (let c = 0; c < arr.size; c++) floats[out + c] = view.getInt16(at + 2 * c, true); break;
        case 6: { const w = view.getUint32(at, true); floats[out] = Math.max(((w << 21) >> 21) / 1023, -1); floats[out + 1] = Math.max(((w << 10) >> 21) / 1023, -1); floats[out + 2] = Math.max((w >> 22) / 511, -1); break; }
        }
        at += attributeBytes(arr);
      }
    }
    bindInline(prog, floats);
    return n;
  }

  // ---- state ----
  const blendFactors = new Set([0, 1, 0x300, 0x301, 0x302, 0x303, 0x304, 0x305, 0x306, 0x307, 0x308, 0x8001, 0x8002, 0x8003, 0x8004]);
  const compareFuncs = new Set([0x200, 0x201, 0x202, 0x203, 0x204, 0x205, 0x206, 0x207]);
  const stencilOps = new Set([0, 0x1E00, 0x1E01, 0x1E02, 0x1E03, 0x150A, 0x8507, 0x8508]);
  function blendEquation(v) { switch (v) { case 0x8006: case 0xF006: return gl.FUNC_ADD; case 0x8007: return gl.MIN; case 0x8008: return gl.MAX; case 0x800A: return gl.FUNC_SUBTRACT; case 0x800B: case 0xF005: return gl.FUNC_REVERSE_SUBTRACT; } fault("blend equation " + hex(v)); }
  // The GL state as last set, each draw setting only what changed, the texture units' bindings and each
  // attribute slot's array or constant kept alongside
  const glState = { caps: new Map(), blend: "", blendEq: -1, blendColor: -1, cullFace: -1, frontFace: -1, depthFunc: -1, depthMask: -1, stencilFunc: "", stencilOp: "", stencilMask: -1, colorMask: -1, scissor: "", polyOffset: "", lineWidth: -1, program: undefined, fb: undefined, vao: undefined, viewport: "", units: [], active: -1, indexBound: false };
  const attribSlots = [];
  for (let i = 0; i < KELVIN_ATTRIBUTES; i++) attribSlots.push({ enabled: false, buffer: null, size: 0, type: 0, normalized: false, stride: 0, x: NaN, y: NaN, z: NaN, w: NaN });
  function forgetTextures() { glState.units = []; glState.active = -1; }
  function capability(cap, on) { if (glState.caps.get(cap) === on) return; glState.caps.set(cap, on); if (on) gl.enable(cap); else gl.disable(cap); }
  function bindFramebuffer(fb) { if (glState.fb !== fb) { glState.fb = fb; gl.bindFramebuffer(gl.FRAMEBUFFER, fb); } }
  function bindVertexArray(vao) { if (glState.vao !== vao) { glState.vao = vao; gl.bindVertexArray(vao); } }
  function useProgram(id) { if (glState.program !== id) { glState.program = id; gl.useProgram(id); } }
  function viewport(w, h) { const key = w + "x" + h; if (glState.viewport !== key) { glState.viewport = key; gl.viewport(0, 0, w, h); } }
  function colorMask(bits) { if (glState.colorMask !== bits) { glState.colorMask = bits; gl.colorMask((bits & 8) !== 0, (bits & 4) !== 0, (bits & 2) !== 0, (bits & 1) !== 0); } }
  function depthMask(on) { if (glState.depthMask !== on) { glState.depthMask = on; gl.depthMask(on); } }
  function stencilMask(v) { if (glState.stencilMask !== v) { glState.stencilMask = v; gl.stencilMask(v); } }
  function scissor(on, x, y, w, h) { capability(gl.SCISSOR_TEST, on); if (!on) return; const key = x + "," + y + "," + w + "," + h; if (glState.scissor !== key) { glState.scissor = key; gl.scissor(x, y, w, h); } }
  function bindTexture(unit, target, id) {
    if (glState.active !== unit) { glState.active = unit; gl.activeTexture(gl.TEXTURE0 + unit); }
    let u = glState.units[unit];
    if (u === undefined) { u = { flat: undefined, cube: undefined }; glState.units[unit] = u; }
    const slot = target === gl.TEXTURE_2D ? "flat" : "cube";
    if (u[slot] !== id) { u[slot] = id; gl.bindTexture(target, id); }
  }
  function texParams(obj, target, min, mag, wrapS, wrapT) {
    if (obj.min !== min) { obj.min = min; gl.texParameteri(target, gl.TEXTURE_MIN_FILTER, min); }
    if (obj.mag !== mag) { obj.mag = mag; gl.texParameteri(target, gl.TEXTURE_MAG_FILTER, mag); }
    if (obj.wrapS !== wrapS) { obj.wrapS = wrapS; gl.texParameteri(target, gl.TEXTURE_WRAP_S, wrapS); }
    if (obj.wrapT !== wrapT) { obj.wrapT = wrapT; gl.texParameteri(target, gl.TEXTURE_WRAP_T, wrapT); }
  }
  function constantAttrib(loc, x, y, z, w) {
    const slot = attribSlots[loc];
    if (slot.enabled) { slot.enabled = false; gl.disableVertexAttribArray(loc); }
    if (slot.x !== x || slot.y !== y || slot.z !== z || slot.w !== w) { slot.x = x; slot.y = y; slot.z = z; slot.w = w; gl.vertexAttrib4f(loc, x, y, z, w); }
  }
  function pointer(loc, buffer, size, type, normalized, stride) {
    const slot = attribSlots[loc];
    if (!slot.enabled) { slot.enabled = true; gl.enableVertexAttribArray(loc); }
    if (slot.buffer === buffer && slot.size === size && slot.type === type && slot.normalized === normalized && slot.stride === stride) return;
    slot.buffer = buffer; slot.size = size; slot.type = type; slot.normalized = normalized; slot.stride = stride;
    gl.bindBuffer(gl.ARRAY_BUFFER, buffer);
    gl.vertexAttribPointer(loc, size, type, normalized, stride, 0);
  }
  function applyState(s) {
    const t = st.state;
    capability(gl.BLEND, t.blend !== 0);
    if (t.blend !== 0) {
      if (!blendFactors.has(t.sfactor) || !blendFactors.has(t.dfactor)) fault("blend factors " + hex(t.sfactor) + " " + hex(t.dfactor));
      const key = t.sfactor + ":" + t.dfactor;
      if (glState.blend !== key) { glState.blend = key; gl.blendFunc(t.sfactor, t.dfactor); }
      const eq = blendEquation(t.blendEquation);
      if (glState.blendEq !== eq) { glState.blendEq = eq; gl.blendEquation(eq); }
      if (glState.blendColor !== t.blendColor) { glState.blendColor = t.blendColor; argb(t.blendColor, color4); gl.blendColor(color4[0], color4[1], color4[2], color4[3]); }
    }
    capability(gl.CULL_FACE, t.cull !== 0);
    if (t.cull !== 0) {
      if (t.cullFace !== 0x404 && t.cullFace !== 0x405 && t.cullFace !== 0x408) fault("cull face " + hex(t.cullFace));
      if (glState.cullFace !== t.cullFace) { glState.cullFace = t.cullFace; gl.cullFace(t.cullFace); }
      if (glState.frontFace !== t.frontFace) { glState.frontFace = t.frontFace; gl.frontFace(t.frontFace === 0x900 ? gl.CCW : gl.CW); }
    }
    const depth = t.depthTest !== 0 && s.depth !== null;
    capability(gl.DEPTH_TEST, depth);
    if (depth) { if (!compareFuncs.has(t.depthFunc)) fault("depth function " + hex(t.depthFunc)); if (glState.depthFunc !== t.depthFunc) { glState.depthFunc = t.depthFunc; gl.depthFunc(t.depthFunc); } }
    depthMask(t.depthMask !== 0);
    const stencil = t.stencilTest !== 0 && s.depth !== null;
    capability(gl.STENCIL_TEST, stencil);
    if (stencil) {
      if (!compareFuncs.has(t.stencilFunc)) fault("stencil function " + hex(t.stencilFunc));
      if (!stencilOps.has(t.stencilFail) || !stencilOps.has(t.stencilZFail) || !stencilOps.has(t.stencilZPass)) fault("stencil operations");
      const fkey = t.stencilFunc + ":" + (t.stencilRef & 0xFF) + ":" + (t.stencilFuncMask & 0xFF);
      if (glState.stencilFunc !== fkey) { glState.stencilFunc = fkey; gl.stencilFunc(t.stencilFunc, t.stencilRef & 0xFF, t.stencilFuncMask & 0xFF); }
      const okey = t.stencilFail + ":" + t.stencilZFail + ":" + t.stencilZPass;
      if (glState.stencilOp !== okey) { glState.stencilOp = okey; gl.stencilOp(t.stencilFail, t.stencilZFail, t.stencilZPass); }
    }
    stencilMask((t.control0 & 1) !== 0 ? t.stencilMask & 0xFF : 0);
    colorMask(((t.colorMask & 0x10000) !== 0 ? 8 : 0) | ((t.colorMask & 0x100) !== 0 ? 4 : 0) | ((t.colorMask & 0x1) !== 0 ? 2 : 0) | ((t.colorMask & 0x1000000) !== 0 ? 1 : 0));
    const x0 = st.windowClipH[0] & 0xFFFF, x1 = Math.min(st.windowClipH[0] >>> 16, s.width), y0 = st.windowClipV[0] & 0xFFFF, y1 = Math.min(st.windowClipV[0] >>> 16, s.height);
    if (st.windowClipType !== 0) fault("exclusive window clips");
    scissor(x1 > x0 && y1 > y0, x0, s.height - y1, x1 - x0, y1 - y0);
    capability(gl.POLYGON_OFFSET_FILL, t.polyOffsetFill !== 0);
    if (t.polyOffsetFill !== 0) { const key = t.polyOffsetScale + ":" + t.polyOffsetBias; if (glState.polyOffset !== key) { glState.polyOffset = key; gl.polygonOffset(t.polyOffsetScale, t.polyOffsetBias); } }
    capability(gl.DITHER, t.dither !== 0);
    const width = Math.max(t.lineWidth / 8, 1);
    if (glState.lineWidth !== width) { glState.lineWidth = width; gl.lineWidth(width); }
  }
  // ---- programs ----
  const vertexShaders = new Map(), fragmentShaders = new Map(), programs = new Map();
  function compiled(type, source) {
    const sh = gl.createShader(type);
    gl.shaderSource(sh, source);
    gl.compileShader(sh);
    if (!gl.getShaderParameter(sh, gl.COMPILE_STATUS)) fault("a shader does not compile: " + gl.getShaderInfoLog(sh) + "\n" + source);
    return sh;
  }
  function vertexConfig() {
    const lctl = st.lightControl, cm = st.colorMaterial, lights = [];
    for (let i = 0; i < KELVIN_LIGHTS; i++) lights.push((st.lightMask >>> (2 * i)) & 3);
    const cfg = { lighting: st.lighting !== 0, lights, localEye: ((lctl >>> 16) & 1) !== 0, separateSpecular: (lctl & 1) !== 0, specularEnable: st.specularEnable !== 0, alphaFromMaterialSpecular: ((lctl >>> 17) & 1) !== 0, emissionSrc: cm & 3, ambientSrc: (cm >>> 2) & 3, diffuseSrc: (cm >>> 4) & 3, specularSrc: (cm >>> 6) & 3, normalization: st.normalization !== 0, skinning: st.skinMode, texgen: Array.from(st.texgen), texMatrix: Array.from(st.textureMatrixEnable).map((v) => v !== 0), infiniteViewer: st.texgenViewModel === 1, pointParams: st.pointParamsEnable !== 0, fog: st.state.fog !== 0 ? { mode: st.fog.mode, genMode: st.fog.genMode } : null };
    if (cfg.lighting && st.twoSided !== 0) fault("two sided lighting");
    return cfg;
  }
  function vertexShader() {
    const flat = st.state.shadeMode === 0x1D00, cfg = vertexConfig(), execution = st.mode & 3;
    let key, build;
    if (execution === 0) {
      key = "fixed:" + flat + ":" + JSON.stringify(cfg);
      build = () => vertexShaderSource(fixedFunctionSource(cfg), flat, true);
    } else if (execution === 2) {
      let words = "";
      for (let slot = st.programStart; slot < KELVIN_PROGRAM_SLOTS; slot++) { words += st.program[slot * 4 + 1].toString(16) + "." + st.program[slot * 4 + 2].toString(16) + "." + st.program[slot * 4 + 3].toString(16) + ","; if ((st.program[slot * 4 + 3] & 1) !== 0) break; }
      const tail = { pointParams: cfg.pointParams, specularEnable: cfg.specularEnable, alphaFromMaterialSpecular: cfg.alphaFromMaterialSpecular, fog: cfg.fog };
      key = "program:" + flat + ":" + JSON.stringify(tail) + ":" + words;
      build = () => vertexShaderSource(translateVertexProgram(st.program, st.programStart).body + programEpilogue + programTail(tail), flat, false);
    } else fault("transform execution mode " + hex(st.mode));
    let sh = vertexShaders.get(key);
    if (sh === undefined) { sh = { id: compiled(gl.VERTEX_SHADER, build()), key }; vertexShaders.set(key, sh); }
    return sh;
  }
  function fragmentShader(stages) {
    const c = st.combiner, count = c.control & 0xF;
    const k = { flat: st.state.shadeMode === 0x1D00, control: c.control, colorICW: Array.from(c.colorICW.subarray(0, count)), alphaICW: Array.from(c.alphaICW.subarray(0, count)), colorOCW: Array.from(c.colorOCW.subarray(0, count)), alphaOCW: Array.from(c.alphaOCW.subarray(0, count)), finalCW0: c.finalCW0, finalCW1: c.finalCW1, stages, shadowFunc: st.shadowFunc, dotMapping: st.dotMapping, otherStageInput: st.otherStageInput, clipPlaneMode: st.clipPlaneMode, alphaTest: st.state.alphaTest !== 0, alphaFunc: st.state.alphaFunc };
    const key = JSON.stringify(k);
    let sh = fragmentShaders.get(key);
    if (sh === undefined) { sh = { id: compiled(gl.FRAGMENT_SHADER, fragmentShaderSource(k)), key }; fragmentShaders.set(key, sh); }
    return sh;
  }
  let boundTextures = [], programGen = -1, lastProgram = null;
  const uniformNames = ["c", "surfaceSize", "clipRange", "pointSize", "lightCtx", "sceneAmbient", "materialEmission", "materialAlpha", "specularParams", "pointParams", "fogParams", "c0", "c1", "finalC0", "finalC1", "fogColor", "alphaRef", "tex0", "tex1", "tex2", "tex3", "texScale0", "texScale1", "texScale2", "texScale3", "bumpMat", "bumpScale", "bumpOffset", "colorKey"];
  function linkedProgram() {
    boundTextures = stageTextures();
    if (st.gen === programGen && lastProgram !== null) return lastProgram;
    programGen = st.gen;
    const vs = vertexShader(), fs = fragmentShader(boundTextures.map((t, i) => ({ mode: (st.stagePrograms >>> (5 * i)) & 0x1F, tex: t === null ? null : textureDescriptor(t) }))), key = vs.key + "|" + fs.key;
    let p = programs.get(key);
    if (p !== undefined) { lastProgram = p; return p; }
    const id = gl.createProgram();
    gl.attachShader(id, vs.id); gl.attachShader(id, fs.id);
    gl.linkProgram(id);
    if (!gl.getProgramParameter(id, gl.LINK_STATUS)) fault("a program does not link: " + gl.getProgramInfoLog(id));
    p = { id, attribs: [], u: {}, constantsGen: -1, otherGen: -1, surface: "", scale: [] };
    for (let a = 0; a < KELVIN_ATTRIBUTES; a++) p.attribs.push(gl.getAttribLocation(id, "v" + a));
    for (const name of uniformNames) p.u[name] = gl.getUniformLocation(id, name);
    useProgram(id);
    for (let i = 0; i < KELVIN_STAGES; i++) if (p.u["tex" + i] !== null) gl.uniform1i(p.u["tex" + i], i);
    programs.set(key, p);
    lastProgram = p;
    return p;
  }
  const factors = new Float32Array(KELVIN_COMBINERS * 4), factors1 = new Float32Array(KELVIN_COMBINERS * 4), bumpMats = new Float32Array(KELVIN_STAGES * 4), bumpScales = new Float32Array(KELVIN_STAGES), bumpOffsets = new Float32Array(KELVIN_STAGES);
  function setUniforms(p, s) {
    const u = p.u, c = st.combiner;
    if (u.c !== null && p.constantsGen !== st.constantsGen) { p.constantsGen = st.constantsGen; gl.uniform4fv(u.c, st.constants); }
    const surface = s.width + "x" + s.height;
    if (u.surfaceSize !== null && p.surface !== surface) { p.surface = surface; gl.uniform2f(u.surfaceSize, s.width, s.height); }
    if (p.otherGen === st.otherGen) return;
    p.otherGen = st.otherGen;
    if (u.clipRange !== null) gl.uniform2f(u.clipRange, st.clipMin, st.clipMax);
    if (u.pointSize !== null) gl.uniform1f(u.pointSize, st.state.pointSize / 8);
    if (u.lightCtx !== null) gl.uniform4fv(u.lightCtx, st.lights);
    if (u.sceneAmbient !== null) gl.uniform3fv(u.sceneAmbient, st.sceneAmbient);
    if (u.materialEmission !== null) gl.uniform3fv(u.materialEmission, st.material.subarray(0, 3));
    if (u.materialAlpha !== null) gl.uniform1f(u.materialAlpha, st.material[3]);
    if (u.specularParams !== null) gl.uniform3fv(u.specularParams, st.specularParams.subarray(0, 3));
    if (u.pointParams !== null) gl.uniform1fv(u.pointParams, st.pointParams);
    if (u.fogParams !== null) gl.uniform3fv(u.fogParams, st.fog.params);
    for (let i = 0; i < KELVIN_COMBINERS; i++) { argb(c.factor0[(c.control & 0x1000) !== 0 ? i : 0], color4); factors.set(color4, i * 4); argb(c.factor1[(c.control & 0x10000) !== 0 ? i : 0], color4); factors1.set(color4, i * 4); }
    if (u.c0 !== null) gl.uniform4fv(u.c0, factors);
    if (u.c1 !== null) gl.uniform4fv(u.c1, factors1);
    if (u.finalC0 !== null) { argb(c.fogFactor[0], color4); gl.uniform4fv(u.finalC0, color4); }
    if (u.finalC1 !== null) { argb(c.fogFactor[1], color4); gl.uniform4fv(u.finalC1, color4); }
    if (u.fogColor !== null) { abgr(st.state.fogColor, color4); gl.uniform4fv(u.fogColor, color4); }
    if (u.alphaRef !== null) gl.uniform1f(u.alphaRef, (st.state.alphaRef & 0xFF) / 255);
    for (let i = 0; i < KELVIN_STAGES; i++) { const t = st.textures[i]; bumpMats.set(t.bumpMatrix, i * 4); bumpScales[i] = t.bumpScale; bumpOffsets[i] = t.bumpOffset; }
    if (u.bumpMat !== null) gl.uniformMatrix2fv(u.bumpMat, false, bumpMats);
    if (u.bumpScale !== null) gl.uniform1fv(u.bumpScale, bumpScales);
    if (u.bumpOffset !== null) gl.uniform1fv(u.bumpOffset, bumpOffsets);
    if (u.colorKey !== null) gl.uniform1uiv(u.colorKey, st.colorKey);
  }
  // ---- textures ----
  const textures = new Map();
  const linearFormats = new Set([0x10, 0x11, 0x12, 0x13, 0x1B, 0x1C, 0x1D, 0x1E, 0x1F, 0x20, 0x2E, 0x2F, 0x30, 0x31, 0x35]);
  const depthFormats = new Map([[0x2C, { bits: 16, floating: false }], [0x2E, { bits: 24, floating: false }], [0x2F, { bits: 24, floating: true }], [0x30, { bits: 16, floating: false }], [0x31, { bits: 16, floating: true }]]);
  function stageTextures() {
    const out = [];
    for (let i = 0; i < KELVIN_STAGES; i++) {
      const program = (st.stagePrograms >>> (5 * i)) & 0x1F, t = st.textures[i];
      out.push(program === 0 || (t.control0 & 0x40000000) === 0 ? null : t);
    }
    return out;
  }
  function colorKeyMaskOf(color) { return color === 0x03 || color === 0x07 || color === 0x1C || color === 0x1E ? "0x00FFFFFFu" : "0xFFFFFFFFu"; }
  function textureDescriptor(t) {
    const color = (t.format >>> 8) & 0xFF, depth = depthFormats.get(color), min = (t.filter >>> 16) & 0xFF;
    return { cube: (t.format & 4) !== 0, depth: depth !== undefined, bits: depth === undefined ? 0 : depth.bits, floating: depth !== undefined && depth.floating, rect: linearFormats.has(color), smooth: min !== 1 && min !== 3 && min !== 5, conv: min === 7 ? (t.filter >>> 13) & 7 : 0, colorKeyMode: t.control0 & 3, colorKeyMask: colorKeyMaskOf(color), alphaKill: (t.control0 & 4) !== 0 };
  }
  function textureLevelBytes(color, width, height) {
    if (color === 0x0C) return ((width + 3) >> 2) * ((height + 3) >> 2) * 8;
    if (color === 0x0E || color === 0x0F) return ((width + 3) >> 2) * ((height + 3) >> 2) * 16;
    const bpp = formatBytes[color];
    if (bpp === undefined) fault("texture colour format " + hex(color));
    return width * height * bpp;
  }
  function writesOver(at, n) { let sum = 0; for (let p = at >>> 12, last = (at + n - 1) >>> 12; p <= last; p++) sum += PAGE_WRITES[p]; return sum; }
  function textureFor(i, t) {
    const fmt = t.format, color = (fmt >>> 8) & 0xFF, dim = (fmt >>> 4) & 0xF, cube = (fmt & 4) !== 0, rect = linearFormats.has(color), depth = depthFormats.get(color);
    if (dim !== 2) fault("texture dimensionality " + dim + " on stage " + i);
    if ((fmt & 8) === 0 && !rect && !cube) fault("a texture with border texels in its image on stage " + i);
    const base = dmaBase((fmt & 2) !== 0 ? K.contextDMAB : K.contextDMAA) + t.offset;
    const width = rect ? t.rect >>> 16 : 1 << ((fmt >>> 20) & 0xF), height = rect ? t.rect & 0xFFFF : 1 << ((fmt >>> 24) & 0xF), pitch = rect ? t.control1 >>> 16 : 0;
    const scaleXY = rect ? [1 / width, 1 / height] : [1, 1];
    if (depth !== undefined) {
      const d = depthBuffers.get(base);
      if (d === undefined) fault("the depth texture at " + hex(base) + " on stage " + i + " is no surface's depth buffer");
      if (d.width !== width || d.height !== height) fault("the depth texture on stage " + i + " is " + width + "x" + height + " over a " + d.width + "x" + d.height + " depth buffer");
      return { id: d.tex, target: gl.TEXTURE_2D, scale: [scaleXY[0], scaleXY[1], 1, 0], levels: 1, obj: d };
    }
    const levels = rect ? 1 : Math.max(1, (fmt >>> 16) & 0xF);
    const surf = surfaces.get(base);
    if (surf !== undefined && !cube && surf.width === width && surf.height === height) return { id: surf.color, target: gl.TEXTURE_2D, scale: [scaleXY[0], scaleXY[1], 1, surfaceOpaque(surf.format) ? 1 : 0], levels: 1, obj: surf };
    if (cube && rect) fault("a cube map in a linear format on stage " + i);
    let face = 0;
    for (let l = 0, w = width, h = height; l < levels; l++, w = Math.max(1, w >> 1), h = Math.max(1, h >> 1)) face += rect ? pitch * h : textureLevelBytes(color, w, h);
    if (cube) face = (face + KELVIN_CUBE_FACE_ALIGN - 1) & ~(KELVIN_CUBE_FACE_ALIGN - 1);
    const faces = cube ? KELVIN_CUBE_FACES : 1, target = cube ? gl.TEXTURE_CUBE_MAP : gl.TEXTURE_2D;
    const key = base + ":" + fmt + ":" + t.control1 + ":" + t.rect + ":" + t.palette;
    const view = bytesAt(base, face * faces, "texture on stage " + i);
    const palette = color === 0x0B ? dmaBase((t.palette & 1) !== 0 ? K.contextDMAB : K.contextDMAA) + (t.palette & 0xFFFFFFC0) : 0;
    const writes = writesOver(base, face * faces) + (color === 0x0B ? writesOver(palette, 256 * 4) : 0);
    let entry = textures.get(key);
    if (entry === undefined) { entry = { id: gl.createTexture(), writes: writes - 1, levels, target, min: -1, mag: -1, wrapS: -1, wrapT: -1 }; textures.set(key, entry); bindTexture(0, target, entry.id); gl.texParameteri(target, gl.TEXTURE_MAX_LEVEL, levels - 1); }
    if (entry.writes !== writes) {
      entry.writes = writes;
      bindTexture(0, target, entry.id);
      for (let f = 0; f < faces; f++) {
        let at = f * face;
        for (let l = 0, w = width, h = height; l < levels; l++, w = Math.max(1, w >> 1), h = Math.max(1, h >> 1)) {
          const n = rect ? pitch * h : textureLevelBytes(color, w, h);
          gl.texImage2D(cube ? gl.TEXTURE_CUBE_MAP_POSITIVE_X + f : gl.TEXTURE_2D, l, gl.RGBA8, w, h, 0, gl.RGBA, gl.UNSIGNED_BYTE, decodeTexture(color, view.subarray(at, at + n), w, h, rect, pitch, palette));
          at += n;
        }
      }
    }
    return { id: entry.id, target, scale: [scaleXY[0], scaleXY[1], 0, 0], levels, obj: entry };
  }
  function setScale(p, i, loc, a, b, c, d) {
    const k = p.scale[i];
    if (k !== undefined && k[0] === a && k[1] === b && k[2] === c && k[3] === d) return;
    p.scale[i] = [a, b, c, d];
    gl.uniform4f(loc, a, b, c, d);
  }
  function bindTextures(p) {
    for (let i = 0; i < KELVIN_STAGES; i++) {
      const loc = p.u["tex" + i], scaleLoc = p.u["texScale" + i], t = boundTextures[i];
      if (t === null || loc === null) { if (scaleLoc !== null) setScale(p, i, scaleLoc, 1, 1, 0, 0); continue; }
      const tex = textureFor(i, t), target = tex.target, desc = textureDescriptor(t);
      bindTexture(i, target, tex.id);
      if (desc.depth) texParams(tex.obj, target, gl.NEAREST, gl.NEAREST, gl.CLAMP_TO_EDGE, gl.CLAMP_TO_EDGE);
      else {
        const minFilter = (t.filter >>> 16) & 0xFF, magFilter = (t.filter >>> 24) & 0xF, mip = tex.levels > 1;
        const min = minFilter === 1 ? gl.NEAREST : minFilter === 2 || minFilter === 7 ? gl.LINEAR : minFilter === 3 ? (mip ? gl.NEAREST_MIPMAP_NEAREST : gl.NEAREST) : minFilter === 4 ? (mip ? gl.LINEAR_MIPMAP_NEAREST : gl.LINEAR) : minFilter === 5 ? (mip ? gl.NEAREST_MIPMAP_LINEAR : gl.NEAREST) : (mip ? gl.LINEAR_MIPMAP_LINEAR : gl.LINEAR);
        const wrap = (mode) => mode === 1 ? gl.REPEAT : mode === 2 ? gl.MIRRORED_REPEAT : gl.CLAMP_TO_EDGE;
        texParams(tex.obj, target, min, magFilter === 1 ? gl.NEAREST : gl.LINEAR, wrap(t.address & 0xF), wrap((t.address >>> 8) & 0xF));
      }
      if (scaleLoc !== null) setScale(p, i, scaleLoc, tex.scale[0], tex.scale[1], tex.scale[2], tex.scale[3]);
    }
  }
  // ---- scan out ----
  let blit = null;
  function present(start) {
    const s = surfaces.get(start >>> 0);
    if (gl === null || s === undefined) return;
    counts.frames++;
    if (!shown) { shown = true; bridge.shown(); }
    if (screen.width !== s.width || screen.height !== s.height) { screen.width = s.width; screen.height = s.height; }
    if (blit === null) {
      const vs = compiled(gl.VERTEX_SHADER, "#version 300 es\nin vec2 p;\nout vec2 t;\nvoid main() { t = p * 0.5 + 0.5; gl_Position = vec4(p, 0.0, 1.0); }\n");
      const fs = compiled(gl.FRAGMENT_SHADER, "#version 300 es\nprecision highp float;\nin vec2 t;\nuniform sampler2D tex;\nout vec4 o;\nvoid main() { o = vec4(texture(tex, t).rgb, 1.0); }\n");
      const id = gl.createProgram(); gl.attachShader(id, vs); gl.attachShader(id, fs); gl.linkProgram(id);
      if (!gl.getProgramParameter(id, gl.LINK_STATUS)) fault("the scan out program does not link: " + gl.getProgramInfoLog(id));
      const buffer = gl.createBuffer();
      bindVertexArray(blitVAO);
      gl.bindBuffer(gl.ARRAY_BUFFER, buffer);
      gl.bufferData(gl.ARRAY_BUFFER, new Float32Array([-1, -1, 1, -1, -1, 1, 1, 1]), gl.STATIC_DRAW);
      const pos = gl.getAttribLocation(id, "p");
      gl.enableVertexAttribArray(pos);
      gl.vertexAttribPointer(pos, 2, gl.FLOAT, false, 0, 0);
      useProgram(id);
      gl.uniform1i(gl.getUniformLocation(id, "tex"), 0);
      blit = { id };
    }
    bindFramebuffer(null);
    bindVertexArray(blitVAO);
    viewport(s.width, s.height);
    capability(gl.SCISSOR_TEST, false); capability(gl.BLEND, false); capability(gl.DEPTH_TEST, false); capability(gl.STENCIL_TEST, false); capability(gl.CULL_FACE, false);
    colorMask(15);
    useProgram(blit.id);
    bindTexture(0, gl.TEXTURE_2D, s.color);
    gl.drawArrays(gl.TRIANGLE_STRIP, 0, 4);
    if (counts.frames % ERROR_CHECK_FRAMES === 0) checkError("the frames' draws and scan out");
  }
  function status() { return counts.frames + " frames, " + counts.flips + " flips, " + counts.draws + " draws, " + counts.clears + " clears"; }
  function flip() { counts.flips++; }
  return { method, present, status, flip };
}
`

// The decoders turning a texture's bytes into RGBA pixels, the swizzled formats read through the bit masks the
// NV2A interleaves coordinates with, the block compressed formats through their colour endpoints and the linear
// formats row by row at their pitch, palette entries read where an indexed format names them
const textureDecoders = `
const formatBytes = { 0x00: 1, 0x01: 1, 0x02: 2, 0x03: 2, 0x04: 2, 0x05: 2, 0x06: 4, 0x07: 4, 0x0B: 1, 0x10: 2, 0x11: 2, 0x12: 4, 0x13: 1, 0x19: 1, 0x1A: 2, 0x1B: 1, 0x1C: 2, 0x1D: 2, 0x1E: 4, 0x1F: 1, 0x20: 2, 0x32: 2, 0x35: 2 };
function swizzleMasks(width, height) {
  let x = 0, y = 0, bit = 1;
  for (let i = 1; i < width || i < height; i <<= 1) { if (i < width) { x |= bit; bit <<= 1; } if (i < height) { y |= bit; bit <<= 1; } }
  return [x >>> 0, y >>> 0];
}
function spreadBits(v, mask) { let out = 0, src = 1; for (let dst = 1; dst <= mask && dst !== 0; dst <<= 1) { if ((mask & dst) !== 0) { if ((v & src) !== 0) out |= dst; src <<= 1; } } return out >>> 0; }
function swizzledIndex(width, height) {
  const [xMask, yMask] = swizzleMasks(width, height), out = new Uint32Array(width * height);
  for (let y = 0; y < height; y++) { const ys = spreadBits(y, yMask); for (let x = 0; x < width; x++) out[y * width + x] = (spreadBits(x, xMask) | ys) >>> 0; }
  return out;
}
function rgb565(c, out, at) { out[at] = ((c >>> 11) & 0x1F) * 255 / 31; out[at + 1] = ((c >>> 5) & 0x3F) * 255 / 63; out[at + 2] = (c & 0x1F) * 255 / 31; }
function decodeDXTColorBlock(data, at, out, bx, by, width, height, fourColor) {
  const c0 = data[at] | (data[at + 1] << 8), c1 = data[at + 2] | (data[at + 3] << 8), bits = data[at + 4] | (data[at + 5] << 8) | (data[at + 6] << 16) | (data[at + 7] << 24);
  const p = new Uint8Array(16);
  rgb565(c0, p, 0); p[3] = 255; rgb565(c1, p, 4); p[7] = 255;
  if (fourColor || c0 > c1) { for (let k = 0; k < 3; k++) { p[8 + k] = (2 * p[k] + p[4 + k]) / 3; p[12 + k] = (p[k] + 2 * p[4 + k]) / 3; } p[11] = 255; p[15] = 255; }
  else { for (let k = 0; k < 3; k++) { p[8 + k] = (p[k] + p[4 + k]) / 2; p[12 + k] = 0; } p[11] = 255; p[15] = 0; }
  for (let row = 0; row < 4; row++) { const py = by + row; if (py >= height) break; for (let col = 0; col < 4; col++) { const px = bx + col; if (px >= width) continue; const idx = (bits >>> (2 * (row * 4 + col))) & 3, o = (py * width + px) * 4; out[o] = p[idx * 4]; out[o + 1] = p[idx * 4 + 1]; out[o + 2] = p[idx * 4 + 2]; out[o + 3] = p[idx * 4 + 3]; } }
}
function decodeDXT(color, data, width, height) {
  const bw = (width + 3) >> 2, bh = (height + 3) >> 2, out = new Uint8Array(width * height * 4), block = color === 0x0C ? 8 : 16;
  for (let by = 0; by < bh; by++) for (let bx = 0; bx < bw; bx++) {
    const at = (by * bw + bx) * block;
    if (at + block > data.length) continue;
    if (color === 0x0C) { decodeDXTColorBlock(data, at, out, bx * 4, by * 4, width, height, false); continue; }
    decodeDXTColorBlock(data, at + 8, out, bx * 4, by * 4, width, height, true);
    if (color === 0x0E) {
      for (let row = 0; row < 4; row++) { const py = by * 4 + row; if (py >= height) break; const a = data[at + row * 2] | (data[at + row * 2 + 1] << 8); for (let col = 0; col < 4; col++) { const px = bx * 4 + col; if (px >= width) continue; const v = (a >>> (col * 4)) & 0xF; out[(py * width + px) * 4 + 3] = v | (v << 4); } }
    } else {
      const a0 = data[at], a1 = data[at + 1], alphas = [a0, a1];
      if (a0 > a1) for (let k = 1; k < 7; k++) alphas.push(((7 - k) * a0 + k * a1) / 7); else { for (let k = 1; k < 5; k++) alphas.push(((5 - k) * a0 + k * a1) / 5); alphas.push(0, 255); }
      const lo = data[at + 2] | (data[at + 3] << 8) | (data[at + 4] << 16), hi = data[at + 5] | (data[at + 6] << 8) | (data[at + 7] << 16);
      for (let i = 0; i < 16; i++) { const row = i >> 2, col = i & 3, py = by * 4 + row, px = bx * 4 + col; if (py >= height || px >= width) continue; const code = i < 8 ? (lo >>> (3 * i)) & 7 : (hi >>> (3 * (i - 8))) & 7; out[(py * width + px) * 4 + 3] = alphas[code]; }
    }
  }
  return out;
}
function decodeTexture(color, data, width, height, linear, pitch, paletteBase) {
  if (color === 0x0C || color === 0x0E || color === 0x0F) return decodeDXT(color, data, width, height);
  const bpp = formatBytes[color], out = new Uint8Array(width * height * 4), view = new DataView(data.buffer, data.byteOffset, data.byteLength);
  const index = linear ? null : swizzledIndex(width, height);
  let pal = null;
  if (color === 0x0B) pal = new DataView(ram.buffer, paletteBase, 256 * 4);
  for (let y = 0; y < height; y++) for (let x = 0; x < width; x++) {
    const src = linear ? y * pitch + x * bpp : index[y * width + x] * bpp, o = (y * width + x) * 4;
    if (src + bpp > data.byteLength) continue;
    switch (color) {
    case 0x00: case 0x13: out[o] = out[o + 1] = out[o + 2] = data[src]; out[o + 3] = 255; break;
    case 0x01: case 0x1B: out[o] = out[o + 1] = out[o + 2] = out[o + 3] = data[src]; break;
    case 0x19: case 0x1F: out[o] = out[o + 1] = out[o + 2] = 255; out[o + 3] = data[src]; break;
    case 0x1A: case 0x20: out[o] = out[o + 1] = out[o + 2] = data[src]; out[o + 3] = data[src + 1]; break;
    case 0x32: case 0x35: out[o] = out[o + 1] = out[o + 2] = data[src + 1]; out[o + 3] = 255; break;
    case 0x02: case 0x10: { const c = view.getUint16(src, true); out[o] = ((c >>> 10) & 0x1F) * 255 / 31; out[o + 1] = ((c >>> 5) & 0x1F) * 255 / 31; out[o + 2] = (c & 0x1F) * 255 / 31; out[o + 3] = (c & 0x8000) !== 0 ? 255 : 0; break; }
    case 0x03: case 0x1C: { const c = view.getUint16(src, true); out[o] = ((c >>> 10) & 0x1F) * 255 / 31; out[o + 1] = ((c >>> 5) & 0x1F) * 255 / 31; out[o + 2] = (c & 0x1F) * 255 / 31; out[o + 3] = 255; break; }
    case 0x04: case 0x1D: { const c = view.getUint16(src, true); out[o] = ((c >>> 8) & 0xF) * 17; out[o + 1] = ((c >>> 4) & 0xF) * 17; out[o + 2] = (c & 0xF) * 17; out[o + 3] = (c >>> 12) * 17; break; }
    case 0x05: case 0x11: rgb565(view.getUint16(src, true), out, o); out[o + 3] = 255; break;
    case 0x06: case 0x12: out[o] = data[src + 2]; out[o + 1] = data[src + 1]; out[o + 2] = data[src]; out[o + 3] = data[src + 3]; break;
    case 0x07: case 0x1E: out[o] = data[src + 2]; out[o + 1] = data[src + 1]; out[o + 2] = data[src]; out[o + 3] = 255; break;
    case 0x0B: { const c = pal.getUint32(data[src] * 4, true); out[o] = (c >>> 16) & 0xFF; out[o + 1] = (c >>> 8) & 0xFF; out[o + 2] = c & 0xFF; out[o + 3] = c >>> 24; break; }
    default: fault("texture colour format " + hex(color));
    }
  }
  return out;
}
`

// The 3D class's methods as the page names them, from the spec's table
func kelvinMethodTable() string {
	parts := make([]string, 0, len(kelvinMethods)+10)
	for _, m := range kelvinMethods {
		parts = append(parts, fmt.Sprintf("%s: 0x%X", m.name, m.value))
	}
	for _, m := range []struct {
		name  string
		value uint32
	}{{"flipRead", kelvinFlipRead}, {"flipWrite", kelvinFlipWrite}, {"flipModulo", kelvinFlipModulo}, {"flipIncrement", kelvinFlipIncrement}, {"flipStall", kelvinFlipStall}, {"semaphoreOffset", kelvinSemaphoreAt}, {"semaphoreRelease", kelvinSemaphoreWrite}, {"contextDMANotifies", kelvinContextDMANotifies}, {"contextDMAState", kelvinContextDMAState}, {"contextDMAReport", kelvinContextDMAReport}, {"contextDMASemaphore", kelvinSemaphoreDMA}} {
		parts = append(parts, fmt.Sprintf("%s: 0x%X", m.name, m.value))
	}
	return strings.Join(parts, ", ")
}

// The unnamed methods the engine keeps, as the page lists them
func kelvinKeptList() string {
	parts := make([]string, len(kelvinUnnamed))
	for i, m := range kelvinUnnamed {
		parts[i] = fmt.Sprintf("0x%X", m)
	}
	return strings.Join(parts, ", ")
}

// Writes the 3D class's method names, the engine's limits and layouts and the unnamed methods it keeps
func (e *emitter) writeGraphicsConstants() {
	e.js("const KELVIN = { %s };", kelvinMethodTable())
	e.js("const KELVIN_ATTRIBUTES = %d, KELVIN_PROGRAM_SLOTS = %d, KELVIN_CONSTANTS = %d, KELVIN_STAGES = %d, KELVIN_STAGE_STRIDE = 0x%X, KELVIN_MATRIX_WORDS = %d, KELVIN_COMBINERS = %d, KELVIN_WINDOW_CLIPS = %d, KELVIN_TEXGEN_STRIDE = 0x%X, KELVIN_TEXGEN_PLANE_STRIDE = 0x%X;", kelvinAttributes, kelvinProgramSlots, kelvinConstants, kelvinStages, kelvinStageStride, kelvinMatrixWords, kelvinCombiners, kelvinWindowClips, kelvinTexgenStride, kelvinTexgenPlaneStride)
	e.js("const KELVIN_LIGHTS = %d, KELVIN_LIGHT_STRIDE = 0x%X, KELVIN_BACK_LIGHT_STRIDE = 0x%X, KELVIN_SPECULAR_WORDS = %d, KELVIN_POINT_PARAMS = %d, KELVIN_SKIN_MATRICES = %d, KELVIN_CUBE_FACES = %d, KELVIN_CUBE_FACE_ALIGN = %d;", kelvinLights, kelvinLightStride, kelvinBackLightStride, kelvinSpecularWords, kelvinPointParams, kelvinSkinMatrices, kelvinCubeFaces, kelvinCubeFaceAlign)
	e.js("const KELVIN_XF_COMPOSITE = %d, KELVIN_XF_PROJECTION = %d, KELVIN_XF_MODEL_VIEW = %d, KELVIN_XF_INVERSE_MODEL_VIEW = %d, KELVIN_XF_EYE = %d, KELVIN_XF_FOG_PLANE = %d, KELVIN_XF_VIEWPORT_SCALE = %d, KELVIN_XF_VIEWPORT_OFFSET = %d, KELVIN_XF_TEXGEN_PLANE = %d, KELVIN_XF_TEXTURE_MATRIX = %d, KELVIN_XF_MATRIX_STRIDE = %d;", kelvinXFComposite, kelvinXFProjection, kelvinXFModelView, kelvinXFInverseMV, kelvinXFEye, kelvinXFFogPlane, kelvinXFViewportScale, kelvinXFViewportOffset, kelvinXFTexgenPlane, kelvinXFTextureMatrix, kelvinXFMatrixStride)
	e.js("const KELVIN_KEPT = new Set([%s]);", kelvinKeptList())
}

// Writes the graphics engine with the shader translators and texture decoders it draws with
func (e *emitter) writeGraphics() {
	e.js("// ---- the graphics engine ----")
	e.writeGraphicsConstants()
	e.js("let graphics = null;")
	e.js("function hwStatus() { return graphics === null ? \"\" : \", \" + graphics.status(); }")
	e.writeShaders()
	e.block(textureDecoders)
	e.block(graphicsEngineScript)
}
