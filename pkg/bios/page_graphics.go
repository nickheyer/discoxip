package bios

import (
	"fmt"
	"strings"
)

// The graphics engine as the page runs it, executing the 3D class's methods the FIFO delivers into surfaces drawn
// through WebGL, the 2D classes copying memory, every surface written back to the console's memory when the memory
// is read another way and reloaded from it when the processor or a copy wrote it, the displayed memory scanned out
// every field with the geometry the display controller's registers hold
const graphicsEngineScript = `
function graphicsEngine(ctx) {
  const K = KELVIN;
  const screen = bridge.screen();
  const gl = screen === null ? null : screen.getContext("webgl2", { alpha: false, antialias: false, depth: false, stencil: false, preserveDrawingBuffer: false });
  if (screen !== null && gl === null) fault("the screen gives no WebGL2 context");
  // A depth past the clip range is clamped as the 3D class leaves it, never clipped away
  const depthClamp = gl === null ? null : gl.getExtension("EXT_depth_clamp");
  if (gl !== null && depthClamp === null) fault("the screen's WebGL2 context has no EXT_depth_clamp");
  if (gl !== null) gl.enable(depthClamp.DEPTH_CLAMP_EXT);
  const counts = { draws: 0, clears: 0, frames: 0, flips: 0, copies: 0 };
  // Surfaces are drawn as many times the display's size as the screen shows it at, within what the context holds,
  // and written back to memory at the console's size
  const LARGEST_SURFACE = gl === null ? 0 : Math.min(gl.getParameter(gl.MAX_TEXTURE_SIZE), gl.getParameter(gl.MAX_RENDERBUFFER_SIZE), gl.getParameter(gl.MAX_VIEWPORT_DIMS)[0], gl.getParameter(gl.MAX_VIEWPORT_DIMS)[1]);
  let scale = 1;
  function wantedScale(mode) { const px = bridge.pixels(); if (mode.width === 0 || mode.height === 0) return scale; return Math.max(1, Math.ceil(Math.min(px.width / mode.width, px.height / mode.height))); }
  function scaleFor(width, height) { return Math.max(1, Math.min(scale, Math.floor(LARGEST_SURFACE / Math.max(width, height)))); }
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
      case K.textureAddress: st.gen++; t.address = data; return;
      case K.textureControl0: st.gen++; t.control0 = data; return;
      case K.textureControl1: t.control1 = data; return;
      case K.textureFilter: st.gen++; t.filter = data; return;
      case K.texturePalette: t.palette = data; return;
      case K.textureImageRect: t.rect = data; return;
      case K.textureBorderColor: st.otherGen++; t.border = data; return;
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
    case K.zMinMaxControl: return;
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
  function writesOver(at, n) { let sum = 0; for (let p = at >>> 12, last = (at + n - 1) >>> 12; p <= last; p++) sum += PAGE_WRITES[p]; return sum; }
  function pagesWritten(at, n) { for (let p = at >>> 12, last = (at + n - 1) >>> 12; p <= last; p++) PAGE_WRITES[p]++; }

  // ---- surfaces ----
  // A surface is drawn as a GL texture holding its pages until the processor, a device or the engine touches
  // them, when it is written back to memory, and reloaded from memory when the processor or a copy wrote its
  // pages since the GL copy was made
  const surfaces = new Map(), depthBuffers = new Map();
  surfaceFlush = (at, n) => flushOverlapping(at, n, null);
  function drawn(s) { if (s.glDirty) return; s.glDirty = true; surfaceHolds(s.offset, s.bytes); }
  function written(s) { s.glDirty = false; surfaceReleases(s.offset, s.bytes); for (const held of [surfaces, depthBuffers]) for (const o of held.values()) if (o !== s && o.glDirty && o.offset < s.offset + s.bytes && s.offset < o.offset + o.bytes) surfaceHolds(o.offset, o.bytes); }
  function surfaceBytesPerPixel(color) { const b = SURFACE_BYTES[color]; if (b === undefined) fault("surface colour format " + color); return b; }
  function dropFramebuffers(depth) { for (const s of surfaces.values()) { const fb = s.fbs.get(depth); if (fb !== undefined) { gl.deleteFramebuffer(fb); s.fbs.delete(depth); if (s.fb === fb) s.fb = null; } } glState.fb = undefined; }
  // A depth buffer holds and writes back its memory as a surface does, loaded from memory with each value
  // repeated over its block and written back through a framebuffer packing each block's centre depth into bytes,
  // the stencil bytes memory holds kept
  function depthBufferFor(offset, width, height, z16, k, swizzled, pitch) {
    const bpp = z16 ? 2 : 4, bytes = swizzled ? width * height * bpp : pitch * height;
    if (!swizzled && pitch < width * bpp) fault("the depth buffer at " + hex(offset) + " of " + width + " pixels has a pitch of " + pitch + " bytes");
    let d = depthBuffers.get(offset);
    if (d !== undefined && (d.width !== width || d.height !== height || d.z16 !== z16 || d.scale !== k)) { flushDepth(d); dropFramebuffers(d); gl.deleteTexture(d.tex); depthBuffers.delete(offset); forgetTextures(); d = undefined; }
    if (d === undefined) {
      const tex = gl.createTexture();
      bindTexture(0, gl.TEXTURE_2D, tex);
      gl.texStorage2D(gl.TEXTURE_2D, 1, z16 ? gl.DEPTH_COMPONENT16 : gl.DEPTH24_STENCIL8, width * k, height * k);
      d = { offset, width, height, z16, scale: k, swizzled, pitch, bytes, tex, glDirty: false, ramWrites: -1, min: -1, mag: -1, wrapS: -1, wrapT: -1 };
      texParams(d, gl.TEXTURE_2D, gl.NEAREST, gl.NEAREST, gl.CLAMP_TO_EDGE, gl.CLAMP_TO_EDGE);
      depthBuffers.set(offset, d);
    }
    if (d.swizzled !== swizzled || d.pitch !== pitch) { if (d.glDirty) surfaceReleases(d.offset, d.bytes); d.swizzled = swizzled; d.pitch = pitch; d.bytes = bytes; if (d.glDirty) surfaceHolds(d.offset, d.bytes); else d.ramWrites = writesOver(d.offset, d.bytes); }
    return d;
  }
  function depthCurrent(d) { if (writesOver(d.offset, d.bytes) !== d.ramWrites) uploadDepth(d); }
  function uploadDepth(d) {
    const bpp = d.z16 ? 2 : 4, k = d.scale, wide = d.width * k, src = bytesAt(d.offset, d.bytes, "the depth buffer"), view = new DataView(src.buffer, src.byteOffset, src.byteLength), swz = d.swizzled ? swizzleTable(d.width, d.height) : null;
    const out = d.z16 ? new Uint16Array(wide * d.height * k) : new Uint32Array(wide * d.height * k);
    for (let y = 0; y < d.height; y++) for (let x = 0; x < d.width; x++) {
      const at = swz === null ? y * d.pitch + x * bpp : swz[y * d.width + x] * bpp, v = d.z16 ? view.getUint16(at, true) : view.getUint32(at, true);
      for (let j = 0; j < k; j++) { const row = ((d.height - 1 - y) * k + j) * wide + x * k; for (let i = 0; i < k; i++) out[row + i] = v; }
    }
    bindTexture(0, gl.TEXTURE_2D, d.tex);
    gl.texSubImage2D(gl.TEXTURE_2D, 0, 0, 0, wide, d.height * k, d.z16 ? gl.DEPTH_COMPONENT : gl.DEPTH_STENCIL, d.z16 ? gl.UNSIGNED_SHORT : gl.UNSIGNED_INT_24_8, out);
    d.ramWrites = writesOver(d.offset, d.bytes);
    written(d);
  }
  function flushDepth(d) {
    if (!d.glDirty) return;
    sized(shrink, d.width, d.height, () => { if (shrink.fb === null) shrink.fb = gl.createFramebuffer(); bindFramebuffer(shrink.fb); gl.framebufferTexture2D(gl.FRAMEBUFFER, gl.COLOR_ATTACHMENT0, gl.TEXTURE_2D, shrink.tex, 0); });
    bindFramebuffer(shrink.fb);
    quadState();
    viewport(d.width, d.height);
    const p = depthProgram();
    useProgram(p.id);
    if (p.scale !== d.scale) { p.scale = d.scale; gl.uniform1i(p.k, d.scale); }
    bindTexture(0, gl.TEXTURE_2D, d.tex);
    gl.drawArrays(gl.TRIANGLE_STRIP, 0, 4);
    const read = new Uint8Array(d.width * d.height * 4);
    gl.readPixels(0, 0, d.width, d.height, gl.RGBA, gl.UNSIGNED_BYTE, read);
    const rgba = flipRows(read, d.width, d.height), bpp = d.z16 ? 2 : 4, dst = bytesAt(d.offset, d.bytes, "the depth buffer"), view = new DataView(dst.buffer, dst.byteOffset, dst.byteLength), swz = d.swizzled ? swizzleTable(d.width, d.height) : null;
    for (let y = 0; y < d.height; y++) for (let x = 0; x < d.width; x++) {
      const i = (y * d.width + x) * 4, z = rgba[i] | (rgba[i + 1] << 8) | (rgba[i + 2] << 16), at = swz === null ? y * d.pitch + x * bpp : swz[y * d.width + x] * bpp;
      if (d.z16) view.setUint16(at, z >>> 8, true); else view.setUint32(at, ((z << 8) | view.getUint8(at)) >>> 0, true);
    }
    pagesWritten(d.offset, d.bytes);
    d.ramWrites = writesOver(d.offset, d.bytes);
    written(d);
  }
  function dropSurface(s) {
    flushSurface(s);
    for (const fb of s.fbs.values()) gl.deleteFramebuffer(fb);
    gl.deleteTexture(s.color);
    surfaces.delete(s.offset);
    forgetTextures();
    glState.fb = undefined;
  }
  function surfaceFor(offset, width, height, format, pitch, zeta, zetaPitch) {
    const color = format & 0xF, bpp = surfaceBytesPerPixel(color), swizzled = ((format >>> 8) & 0xF) === 2, bytes = swizzled ? width * height * bpp : pitch * height;
    if (!swizzled && pitch < width * bpp) fault("the surface at " + hex(offset) + " of " + width + " pixels has a pitch of " + pitch + " bytes");
    let s = surfaces.get(offset);
    const k = scaleFor(width, height);
    if (s !== undefined && (s.width !== width || s.height !== height || s.bpp !== bpp || s.scale !== k)) { dropSurface(s); s = undefined; }
    // The same image declared in another layout keeps its drawing, memory taking the new layout when it is next written back
    if (s !== undefined && (s.swizzled !== swizzled || s.pitch !== pitch)) { if (s.glDirty) surfaceReleases(s.offset, s.bytes); s.swizzled = swizzled; s.pitch = pitch; s.bytes = bytes; if (s.glDirty) surfaceHolds(s.offset, s.bytes); else s.ramWrites = writesOver(s.offset, s.bytes); }
    const z16 = ((format >>> 4) & 0xF) === 1;
    const depth = zeta !== 0 ? depthBufferFor(zeta, width, height, z16, k, swizzled, zetaPitch) : null;
    if (s === undefined) {
      const tex = gl.createTexture();
      bindTexture(0, gl.TEXTURE_2D, tex);
      const levels = mipLevels(width * k, height * k);
      gl.texStorage2D(gl.TEXTURE_2D, levels, gl.RGBA8, width * k, height * k);
      s = { offset, width, height, scale: k, format, bpp, swizzled, pitch, bytes, zeta, fb: null, fbs: new Map(), color: tex, depth: null, glDirty: false, ramWrites: -1, min: -1, mag: -1, wrapS: -1, wrapT: -1, levels, changes: 0, mips: -1 };
      texParams(s, gl.TEXTURE_2D, gl.LINEAR, gl.LINEAR, gl.CLAMP_TO_EDGE, gl.CLAMP_TO_EDGE);
      surfaces.set(offset, s);
    }
    s.format = format;
    let fb = s.fbs.get(depth);
    if (fb === undefined) {
      fb = gl.createFramebuffer();
      bindFramebuffer(fb);
      gl.framebufferTexture2D(gl.FRAMEBUFFER, gl.COLOR_ATTACHMENT0, gl.TEXTURE_2D, s.color, 0);
      if (depth !== null) gl.framebufferTexture2D(gl.FRAMEBUFFER, depth.z16 ? gl.DEPTH_ATTACHMENT : gl.DEPTH_STENCIL_ATTACHMENT, gl.TEXTURE_2D, depth.tex, 0);
      if (gl.checkFramebufferStatus(gl.FRAMEBUFFER) !== gl.FRAMEBUFFER_COMPLETE) fault("the surface at " + hex(offset) + " of " + width + "x" + height + " cannot be drawn into");
      s.fbs.set(depth, fb);
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
    const s = surfaceFor(su.color, width, height, su.format, su.pitch, su.zeta, su.zetaPitch);
    flushOverlapping(s.offset, s.bytes, s);
    if (s.depth !== null) flushOverlapping(s.depth.offset, s.depth.bytes, s.depth);
    return s;
  }
  function ramNewer(s) { return writesOver(s.offset, s.bytes) !== s.ramWrites; }
  function mipLevels(width, height) { return Math.floor(Math.log2(Math.max(width, height))) + 1; }
  // The mip chain of a drawn surface regenerated from its drawing when it is sampled after a change
  function surfaceMips(s) { if (s.mips === s.changes) return; bindTexture(0, gl.TEXTURE_2D, s.color); gl.generateMipmap(gl.TEXTURE_2D); s.mips = s.changes; }
  // The surface's GL copy brought up to date with memory before its colour is partly written or read
  function colorCurrent(s) { if (ramNewer(s)) uploadSurface(s); }
  function flushOverlapping(at, n, except) {
    for (const s of surfaces.values()) if (s !== except && s.glDirty && s.offset < at + n && at < s.offset + s.bytes) flushSurface(s);
    for (const d of depthBuffers.values()) if (d !== except && d.glDirty && d.offset < at + n && at < d.offset + d.bytes) flushDepth(d);
  }
  const swizzleTables = new Map();
  function swizzleTable(width, height) { const key = width + "x" + height; let t = swizzleTables.get(key); if (t === undefined) { t = swizzledIndex(width, height); swizzleTables.set(key, t); } return t; }
  // A surface drawn larger than the console's is loaded from memory through a texture of the console's size drawn
  // over it, and written back through a framebuffer of the console's size each pixel of which averages its block
  const lift = { tex: null, width: 0, height: 0 }, shrink = { fb: null, tex: null, width: 0, height: 0 };
  function sized(obj, width, height, make) { if (obj.width === width && obj.height === height) return; if (obj.tex !== null) gl.deleteTexture(obj.tex); obj.tex = gl.createTexture(); bindTexture(0, gl.TEXTURE_2D, obj.tex); gl.texStorage2D(gl.TEXTURE_2D, 1, gl.RGBA8, width, height); gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR); gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.LINEAR); obj.width = width; obj.height = height; make(); }
  function quadState() { capability(gl.SCISSOR_TEST, false); capability(gl.BLEND, false); capability(gl.DEPTH_TEST, false); capability(gl.STENCIL_TEST, false); capability(gl.CULL_FACE, false); colorMask(15); bindVertexArray(quadVAO()); }
  function uploadSurface(s) {
    const rgba = flipRows(surfaceDecode(s.format & 0xF, bytesAt(s.offset, s.bytes, "the surface"), s.width, s.height, s.swizzled ? null : s.pitch, s.swizzled ? swizzleTable(s.width, s.height) : null), s.width, s.height);
    if (s.scale === 1) { bindTexture(0, gl.TEXTURE_2D, s.color); gl.texSubImage2D(gl.TEXTURE_2D, 0, 0, 0, s.width, s.height, gl.RGBA, gl.UNSIGNED_BYTE, rgba); }
    else {
      sized(lift, s.width, s.height, () => {});
      bindTexture(0, gl.TEXTURE_2D, lift.tex);
      gl.texSubImage2D(gl.TEXTURE_2D, 0, 0, 0, s.width, s.height, gl.RGBA, gl.UNSIGNED_BYTE, rgba);
      bindFramebuffer(s.fb);
      quadState();
      viewport(s.width * s.scale, s.height * s.scale);
      useProgram(copyProgram().id);
      gl.drawArrays(gl.TRIANGLE_STRIP, 0, 4);
    }
    s.ramWrites = writesOver(s.offset, s.bytes);
    s.changes++;
    written(s);
  }
  function flushSurface(s) {
    if (!s.glDirty) return;
    if (s.scale === 1) bindFramebuffer(s.fb);
    else {
      sized(shrink, s.width, s.height, () => { if (shrink.fb === null) shrink.fb = gl.createFramebuffer(); bindFramebuffer(shrink.fb); gl.framebufferTexture2D(gl.FRAMEBUFFER, gl.COLOR_ATTACHMENT0, gl.TEXTURE_2D, shrink.tex, 0); });
      bindFramebuffer(shrink.fb);
      quadState();
      viewport(s.width, s.height);
      const p = shrinkProgram();
      useProgram(p.id);
      if (p.scale !== s.scale) { p.scale = s.scale; gl.uniform1i(p.k, s.scale); }
      bindTexture(0, gl.TEXTURE_2D, s.color);
      gl.drawArrays(gl.TRIANGLE_STRIP, 0, 4);
    }
    const rgba = new Uint8Array(s.width * s.height * 4);
    gl.readPixels(0, 0, s.width, s.height, gl.RGBA, gl.UNSIGNED_BYTE, rgba);
    surfaceEncode(s.format & 0xF, flipRows(rgba, s.width, s.height), s.width, s.height, s.swizzled ? null : s.pitch, s.swizzled ? swizzleTable(s.width, s.height) : null, bytesAt(s.offset, s.bytes, "the surface"));
    pagesWritten(s.offset, s.bytes);
    s.ramWrites = writesOver(s.offset, s.bytes);
    written(s);
  }
  function flipRows(rgba, width, height) { const out = new Uint8Array(rgba.length), row = width * 4; for (let y = 0; y < height; y++) out.set(rgba.subarray(y * row, (y + 1) * row), (height - 1 - y) * row); return out; }
  function surfaceOpaque(format) { const color = format & 0xF; return color !== 8 && color !== 6 && color !== 7; }
  function argb(v, out) { out[0] = ((v >>> 16) & 0xFF) / 255; out[1] = ((v >>> 8) & 0xFF) / 255; out[2] = (v & 0xFF) / 255; out[3] = (v >>> 24) / 255; return out; }
  function abgr(v, out) { out[0] = (v & 0xFF) / 255; out[1] = ((v >>> 8) & 0xFF) / 255; out[2] = ((v >>> 16) & 0xFF) / 255; out[3] = (v >>> 24) / 255; return out; }
  const color4 = new Float32Array(4);

  // ---- clears ----
  function clearSurface(flags) {
    counts.clears++;
    if (gl === null) return;
    const s = target();
    const x0 = st.clear.rectH & 0xFFFF, x1 = st.clear.rectH >>> 16, y0 = st.clear.rectV & 0xFFFF, y1 = st.clear.rectV >>> 16, k = s.scale;
    let bits = 0;
    const everywhere = x0 === 0 && y0 === 0 && x1 >= s.width - 1 && y1 >= s.height - 1;
    if ((flags & 0xF0) !== 0) {
      if (!everywhere || (flags & 0xF0) !== 0xF0) colorCurrent(s); else s.ramWrites = writesOver(s.offset, s.bytes);
      drawn(s);
      s.changes++;
    }
    const z16 = ((s.format >>> 4) & 0xF) === 1;
    if (s.depth !== null && ((flags & 1) !== 0 || (!z16 && (flags & 2) !== 0))) {
      if (!everywhere || (flags & 1) === 0 || (!z16 && (flags & 2) === 0)) depthCurrent(s.depth); else s.depth.ramWrites = writesOver(s.depth.offset, s.depth.bytes);
      drawn(s.depth);
    }
    bindFramebuffer(s.fb);
    scissor(true, x0 * k, (s.height - 1 - y1) * k, (x1 - x0 + 1) * k, (y1 - y0 + 1) * k);
    if ((flags & 0xF0) !== 0) {
      colorMask(((flags & 0x10) !== 0 ? 8 : 0) | ((flags & 0x20) !== 0 ? 4 : 0) | ((flags & 0x40) !== 0 ? 2 : 0) | ((flags & 0x80) !== 0 ? 1 : 0)); argb(st.clear.color, color4); gl.clearColor(color4[0], color4[1], color4[2], color4[3]); bits |= gl.COLOR_BUFFER_BIT;
    }
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
  function colorMaskBits(mask) { return ((mask & 0x10000) !== 0 ? 8 : 0) | ((mask & 0x100) !== 0 ? 4 : 0) | ((mask & 0x1) !== 0 ? 2 : 0) | ((mask & 0x1000000) !== 0 ? 1 : 0); }
  // The memory a draw reads and writes is brought current before any of its GL state is set, since a surface
  // written back or loaded on the way sets state of its own
  function draw(d) {
    const s = target();
    if (colorMaskBits(st.state.colorMask) !== 0) { colorCurrent(s); drawn(s); s.changes++; }
    if (s.depth !== null && (st.state.depthTest !== 0 || st.state.stencilTest !== 0)) {
      depthCurrent(s.depth);
      if ((st.state.depthTest !== 0 && st.state.depthMask !== 0) || (st.state.stencilTest !== 0 && !s.depth.z16 && (st.state.control0 & 1) !== 0 && (st.state.stencilMask & 0xFF) !== 0)) drawn(s.depth);
    }
    const prog = linkedProgram();
    const stageTex = [];
    for (let i = 0; i < KELVIN_STAGES; i++) stageTex.push(boundTextures[i] !== null && prog.u["tex" + i] !== null ? textureFor(i, boundTextures[i]) : null);
    const indexed = d.count > 0 || d.ranges.length > 0;
    let lo = 0xFFFFFFFF, hi = 0;
    if (indexed) {
      for (let i = 0; i < d.count; i++) { const v = d.indices[i]; if (v < lo) lo = v; if (v > hi) hi = v; }
      for (let i = 0; i < d.ranges.length; i += 2) { if (d.ranges[i] < lo) lo = d.ranges[i]; if (d.ranges[i] + d.ranges[i + 1] - 1 > hi) hi = d.ranges[i] + d.ranges[i + 1] - 1; }
      for (let a = 0; a < KELVIN_ATTRIBUTES; a++) if (prog.attribs[a] >= 0 && st.arrays[a].size !== 0) { const r = arrayRange(st.arrays[a], lo, hi); flushOverlapping(r.start, r.bytes, null); }
    }
    bindFramebuffer(s.fb);
    bindVertexArray(drawVAO);
    viewport(s.width * s.scale, s.height * s.scale);
    applyState(s);
    useProgram(prog.id);
    setUniforms(prog, s);
    bindTextures(prog, stageTex);
    const mode = glPrimitive(d.primitive), quads = d.primitive === 8;
    if (d.inlineCount > 0) {
      bindInline(prog, d.inline.subarray(0, d.inlineCount * KELVIN_ATTRIBUTES * 4));
      if (quads) { const idx = new Uint32Array(d.inlineCount); for (let i = 0; i < idx.length; i++) idx[i] = i; drawIndices(mode, quadsToTriangles(idx)); } else gl.drawArrays(mode, 0, d.inlineCount);
    }
    if (d.inlineWords.length > 0) {
      const n = bindInlineArray(prog, d.inlineWords);
      if (quads) { const idx = new Uint32Array(n); for (let i = 0; i < idx.length; i++) idx[i] = i; drawIndices(mode, quadsToTriangles(idx)); } else gl.drawArrays(mode, 0, n);
    }
    if (indexed) {
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
  function arrayRange(arr, lo, hi) { const n = hi - lo + 1; return { n, start: dmaBase(arr.context !== 0 ? K.contextDMAVertexB : K.contextDMAVertexA) + arr.offset + lo * arr.stride, bytes: (n - 1) * arr.stride + attributeBytes(arr) }; }
  function bindArrays(prog, lo, hi) {
    for (let a = 0; a < KELVIN_ATTRIBUTES; a++) {
      const loc = prog.attribs[a];
      if (loc < 0) continue;
      const arr = st.arrays[a];
      if (arr.size === 0) { constantAttrib(loc, st.attrib[a * 4], st.attrib[a * 4 + 1], st.attrib[a * 4 + 2], st.attrib[a * 4 + 3]); continue; }
      const { n, start, bytes } = arrayRange(arr, lo, hi);
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
      if (t.frontFace !== 0x900 && t.frontFace !== 0x901) fault("front face " + hex(t.frontFace));
      // The vertex shaders flip y so the surface sits upright in GL and the hardware winding passes through unchanged
      if (glState.frontFace !== t.frontFace) { glState.frontFace = t.frontFace; gl.frontFace(t.frontFace); }
    }
    const depth = t.depthTest !== 0 && s.depth !== null;
    capability(gl.DEPTH_TEST, depth);
    if (depth) { if (!compareFuncs.has(t.depthFunc)) fault("depth function " + hex(t.depthFunc)); if (glState.depthFunc !== t.depthFunc) { glState.depthFunc = t.depthFunc; gl.depthFunc(t.depthFunc); } }
    depthMask(t.depthMask !== 0);
    const stencil = t.stencilTest !== 0 && s.depth !== null && !s.depth.z16;
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
    colorMask(colorMaskBits(t.colorMask));
    // The window clip's maxima are inclusive
    const x0 = st.windowClipH[0] & 0xFFFF, x1 = Math.min(st.windowClipH[0] >>> 16, s.width - 1), y0 = st.windowClipV[0] & 0xFFFF, y1 = Math.min(st.windowClipV[0] >>> 16, s.height - 1), k = s.scale;
    if (st.windowClipType !== 0) fault("exclusive window clips");
    scissor(x1 >= x0 && y1 >= y0, x0 * k, (s.height - 1 - y1) * k, (x1 - x0 + 1) * k, (y1 - y0 + 1) * k);
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
      build = () => vertexShaderSource(translateVertexProgram(st.program, st.programStart).body + programPerspective + programEpilogue + programTail(tail), flat, false);
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
  const uniformNames = ["c", "surfaceSize", "clipRange", "pointSize", "pixelScale", "lightCtx", "sceneAmbient", "materialEmission", "materialAlpha", "specularParams", "pointParams", "fogParams", "c0", "c1", "finalC0", "finalC1", "fogColor", "alphaRef", "tex0", "tex1", "tex2", "tex3", "texScale0", "texScale1", "texScale2", "texScale3", "texTexel0", "texTexel1", "texTexel2", "texTexel3", "bumpMat", "bumpScale", "bumpOffset", "colorKey", "texBorder"];
  function linkedProgram() {
    boundTextures = stageTextures();
    if (st.gen === programGen && lastProgram !== null) return lastProgram;
    programGen = st.gen;
    const vs = vertexShader(), fs = fragmentShader(boundTextures.map((t, i) => ({ mode: (st.stagePrograms >>> (5 * i)) & 0x1F, tex: t === null ? null : textureDescriptor(t, i) }))), key = vs.key + "|" + fs.key;
    let p = programs.get(key);
    if (p !== undefined) { lastProgram = p; return p; }
    const id = gl.createProgram();
    gl.attachShader(id, vs.id); gl.attachShader(id, fs.id);
    gl.linkProgram(id);
    if (!gl.getProgramParameter(id, gl.LINK_STATUS)) fault("a program does not link: " + gl.getProgramInfoLog(id));
    p = { id, attribs: [], u: {}, constantsGen: -1, otherGen: -1, surface: "", scale: [], texel: [] };
    for (let a = 0; a < KELVIN_ATTRIBUTES; a++) p.attribs.push(gl.getAttribLocation(id, "v" + a));
    for (const name of uniformNames) p.u[name] = gl.getUniformLocation(id, name);
    useProgram(id);
    for (let i = 0; i < KELVIN_STAGES; i++) if (p.u["tex" + i] !== null) gl.uniform1i(p.u["tex" + i], i);
    programs.set(key, p);
    lastProgram = p;
    return p;
  }
  const factors = new Float32Array(KELVIN_COMBINERS * 4), factors1 = new Float32Array(KELVIN_COMBINERS * 4), bumpMats = new Float32Array(KELVIN_STAGES * 4), bumpScales = new Float32Array(KELVIN_STAGES), bumpOffsets = new Float32Array(KELVIN_STAGES), borders = new Float32Array(KELVIN_STAGES * 4);
  function setUniforms(p, s) {
    const u = p.u, c = st.combiner;
    if (u.c !== null && p.constantsGen !== st.constantsGen) { p.constantsGen = st.constantsGen; gl.uniform4fv(u.c, st.constants); }
    const surface = s.width + "x" + s.height + "@" + s.scale;
    if (p.surface !== surface) {
      p.surface = surface;
      if (u.surfaceSize !== null) gl.uniform2f(u.surfaceSize, s.width, s.height);
      if (u.pixelScale !== null) gl.uniform1f(u.pixelScale, s.scale);
    }
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
    for (let i = 0; i < KELVIN_STAGES; i++) { const t = st.textures[i]; bumpMats.set(t.bumpMatrix, i * 4); bumpScales[i] = t.bumpScale; bumpOffsets[i] = t.bumpOffset; argb(t.border, color4); borders.set(color4, i * 4); }
    if (u.bumpMat !== null) gl.uniformMatrix2fv(u.bumpMat, false, bumpMats);
    if (u.bumpScale !== null) gl.uniform1fv(u.bumpScale, bumpScales);
    if (u.bumpOffset !== null) gl.uniform1fv(u.bumpOffset, bumpOffsets);
    if (u.colorKey !== null) gl.uniform1uiv(u.colorKey, st.colorKey);
    if (u.texBorder !== null) gl.uniform4fv(u.texBorder, borders);
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
  // The address modes, wrap 1, mirror 2, clamp to edge 3, border 4 and the OpenGL clamp 5 taken as the edge
  function addressMode(mode, axis, stage) { if (mode < 1 || mode > 5) fault("texture address mode " + mode + " on axis " + axis + " of stage " + stage); return mode; }
  function textureDescriptor(t, i) {
    const color = (t.format >>> 8) & 0xFF, depth = depthFormats.get(color), min = (t.filter >>> 16) & 0xFF, mag = (t.filter >>> 24) & 0xF;
    const borderS = addressMode(t.address & 0xF, "s", i) === 4, borderT = addressMode((t.address >>> 8) & 0xF, "t", i) === 4, smooth = min !== 1 && min !== 3 && min !== 5;
    if ((borderS || borderT) && (mag === 1) === smooth) fault("border texels with a nearest filter on one axis of the sampling and a linear filter on the other on stage " + i);
    if ((borderS || borderT) && !linearFormats.has(color) && ((t.format >>> 16) & 0xF) > 1) fault("border texels on the mipmapped texture of stage " + i);
    return { cube: (t.format & 4) !== 0, depth: depth !== undefined, bits: depth === undefined ? 0 : depth.bits, floating: depth !== undefined && depth.floating, rect: linearFormats.has(color), smooth, conv: min === 7 ? (t.filter >>> 13) & 7 : 0, colorKeyMode: t.control0 & 3, colorKeyMask: colorKeyMaskOf(color), alphaKill: (t.control0 & 4) !== 0, borderS, borderT };
  }
  function textureLevelBytes(color, width, height) {
    if (color === 0x0C) return ((width + 3) >> 2) * ((height + 3) >> 2) * 8;
    if (color === 0x0E || color === 0x0F) return ((width + 3) >> 2) * ((height + 3) >> 2) * 16;
    const bpp = formatBytes[color];
    if (bpp === undefined) fault("texture colour format " + hex(color));
    return width * height * bpp;
  }
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
      flushOverlapping(base, d.bytes, d);
      depthCurrent(d);
      return { id: d.tex, target: gl.TEXTURE_2D, scale: [scaleXY[0], scaleXY[1], 1, 0], texel: [1 / width, 1 / height], levels: 1, obj: d };
    }
    const levels = rect ? 1 : Math.max(1, (fmt >>> 16) & 0xF);
    const surf = surfaces.get(base);
    // The drawing is the texture when size and pixel size agree, linear over linear also matching pitch
    if (surf !== undefined && !cube && surf.width === width && surf.height === height && surf.bpp === formatBytes[color] && (!rect || surf.swizzled || surf.pitch === pitch)) { colorCurrent(surf); surfaceMips(surf); return { id: surf.color, target: gl.TEXTURE_2D, scale: [scaleXY[0], scaleXY[1], 1, surfaceOpaque(surf.format) ? 1 : 0], texel: [1 / width, 1 / height], levels: surf.levels, obj: surf }; }
    if (cube && rect) fault("a cube map in a linear format on stage " + i);
    let face = 0;
    for (let l = 0, w = width, h = height; l < levels; l++, w = Math.max(1, w >> 1), h = Math.max(1, h >> 1)) face += rect ? pitch * h : textureLevelBytes(color, w, h);
    if (cube) face = (face + KELVIN_CUBE_FACE_ALIGN - 1) & ~(KELVIN_CUBE_FACE_ALIGN - 1);
    const faces = cube ? KELVIN_CUBE_FACES : 1, target = cube ? gl.TEXTURE_CUBE_MAP : gl.TEXTURE_2D;
    const key = base + ":" + fmt + ":" + t.control1 + ":" + t.rect + ":" + t.palette;
    flushOverlapping(base, face * faces, null);
    const view = bytesAt(base, face * faces, "texture on stage " + i);
    const palette = color === 0x0B ? dmaBase((t.palette & 1) !== 0 ? K.contextDMAB : K.contextDMAA) + (t.palette & 0xFFFFFFC0) : 0;
    if (color === 0x0B) flushOverlapping(palette, 256 * 4, null);
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
    return { id: entry.id, target, scale: [scaleXY[0], scaleXY[1], 0, 0], texel: [1 / width, 1 / height], levels, obj: entry };
  }
  function setScale(p, i, loc, a, b, c, d) {
    const k = p.scale[i];
    if (k !== undefined && k[0] === a && k[1] === b && k[2] === c && k[3] === d) return;
    p.scale[i] = [a, b, c, d];
    gl.uniform4f(loc, a, b, c, d);
  }
  function wrapOf(mode) { return mode === 1 ? gl.REPEAT : mode === 2 ? gl.MIRRORED_REPEAT : gl.CLAMP_TO_EDGE; }
  function bindTextures(p, stageTex) {
    for (let i = 0; i < KELVIN_STAGES; i++) {
      const scaleLoc = p.u["texScale" + i], t = boundTextures[i], tex = stageTex[i];
      if (tex === null) { if (scaleLoc !== null) setScale(p, i, scaleLoc, 1, 1, 0, 0); continue; }
      const target = tex.target, desc = textureDescriptor(t, i);
      bindTexture(i, target, tex.id);
      if (desc.depth) texParams(tex.obj, target, gl.NEAREST, gl.NEAREST, gl.CLAMP_TO_EDGE, gl.CLAMP_TO_EDGE);
      else {
        const minFilter = (t.filter >>> 16) & 0xFF, magFilter = (t.filter >>> 24) & 0xF, mip = tex.levels > 1, drawnSurface = tex.obj.changes !== undefined;
        // A drawn surface is minified through its mip chain, the level at the scale's logarithm standing for the console's image
        const min = minFilter === 1 ? (drawnSurface ? gl.NEAREST_MIPMAP_NEAREST : gl.NEAREST) : minFilter === 2 || minFilter === 7 ? (drawnSurface ? gl.LINEAR_MIPMAP_LINEAR : gl.LINEAR) : minFilter === 3 ? (mip ? gl.NEAREST_MIPMAP_NEAREST : gl.NEAREST) : minFilter === 4 ? (mip ? gl.LINEAR_MIPMAP_NEAREST : gl.LINEAR) : minFilter === 5 ? (mip ? gl.NEAREST_MIPMAP_LINEAR : gl.NEAREST) : (mip ? gl.LINEAR_MIPMAP_LINEAR : gl.LINEAR);
        texParams(tex.obj, target, min, magFilter === 1 ? gl.NEAREST : gl.LINEAR, wrapOf(t.address & 0xF), wrapOf((t.address >>> 8) & 0xF));
      }
      if (scaleLoc !== null) setScale(p, i, scaleLoc, tex.scale[0], tex.scale[1], tex.scale[2], tex.scale[3]);
      const texelLoc = p.u["texTexel" + i];
      if (texelLoc !== null) { const k = p.texel[i]; if (k === undefined || k[0] !== tex.texel[0] || k[1] !== tex.texel[1]) { p.texel[i] = [tex.texel[0], tex.texel[1]]; gl.uniform2f(texelLoc, tex.texel[0], tex.texel[1]); } }
    }
  }

  // ---- the 2D classes ----
  // The 2D surfaces object names the source and destination memory objects, format, pitches and offsets the image
  // blit copies through, the memory copy object moves lines between two memory objects, every copy made in memory
  // after the surfaces drawn over either region were written back
  const surfaces2D = new Map(), blits = new Map(), copies = new Map();
  function stateOf(map, inst, make) { let s = map.get(inst); if (s === undefined) { s = make(); map.set(inst, s); } return s; }
  function method2D(cls, inst, m, data) {
    if (cls === SURFACES_2D_CLASS) {
      const s = stateOf(surfaces2D, inst, () => ({ source: 0, destin: 0, format: 0, pitchIn: 0, pitchOut: 0, offsetIn: 0, offsetOut: 0 }));
      switch (m) {
      case 0x180: return;
      case 0x184: s.source = data; return;
      case 0x188: s.destin = data; return;
      case 0x300: s.format = data; return;
      case 0x304: s.pitchIn = data & 0xFFFF; s.pitchOut = data >>> 16; return;
      case 0x308: s.offsetIn = data; return;
      case 0x30C: s.offsetOut = data; return;
      }
      fault("2D surfaces method " + hex(m) + " with " + hex(data));
    }
    if (cls === IMAGE_BLIT_CLASS) {
      const b = stateOf(blits, inst, () => ({ surfaces: 0, operation: 0, pointIn: 0, pointOut: 0 }));
      switch (m) {
      case 0x180: case 0x184: case 0x188: case 0x18C: case 0x190: case 0x194: case 0x198: return;
      case 0x19C: b.surfaces = data; return;
      case 0x2FC: b.operation = data; return;
      case 0x300: b.pointIn = data; return;
      case 0x304: b.pointOut = data; return;
      case 0x308: imageBlit(b, data); return;
      }
      fault("image blit method " + hex(m) + " with " + hex(data));
    }
    if (cls === MEMORY_COPY_CLASS) {
      const c = stateOf(copies, inst, () => ({ bufferIn: 0, bufferOut: 0, offsetIn: 0, offsetOut: 0, pitchIn: 0, pitchOut: 0, lineLength: 0, lineCount: 0, format: 0 }));
      switch (m) {
      case 0x180: return;
      case 0x184: c.bufferIn = data; return;
      case 0x188: c.bufferOut = data; return;
      case 0x30C: c.offsetIn = data; return;
      case 0x310: c.offsetOut = data; return;
      case 0x314: c.pitchIn = data; return;
      case 0x318: c.pitchOut = data; return;
      case 0x31C: c.lineLength = data; return;
      case 0x320: c.lineCount = data; return;
      case 0x324: c.format = data; return;
      case 0x328: memoryCopy(c); return;
      }
      fault("memory copy method " + hex(m) + " with " + hex(data));
    }
    fault("object class " + hex(cls) + " method " + hex(m));
  }
  function copyLines(src, pitchIn, dst, pitchOut, rowBytes, rows, what) {
    if (rows === 0 || rowBytes === 0) return;
    const srcSpan = (rows - 1) * pitchIn + rowBytes, dstSpan = (rows - 1) * pitchOut + rowBytes;
    bytesAt(src, srcSpan, what + "'s source"); bytesAt(dst, dstSpan, what + "'s destination");
    flushOverlapping(src, srcSpan, null); flushOverlapping(dst, dstSpan, null);
    const forward = dst <= src;
    for (let i = 0; i < rows; i++) { const y = forward ? i : rows - 1 - i, from = src + y * pitchIn; ram.copyWithin(dst + y * pitchOut, from, from + rowBytes); }
    pagesWritten(dst, dstSpan);
    counts.copies++;
  }
  function imageBlit(b, size) {
    const width = size & 0xFFFF, height = size >>> 16;
    if (width === 0 || height === 0) return;
    if (b.operation !== BLIT_SOURCE_COPY) fault("image blit operation " + hex(b.operation));
    const s = surfaces2D.get(b.surfaces);
    if (s === undefined) fault("the image blit names no 2D surfaces object");
    const bpp = SURFACES_2D_BYTES[s.format];
    if (bpp === undefined) fault("2D surfaces colour format " + hex(s.format));
    const src = ctx.memoryObject(s.source, "the 2D surfaces' source").base + s.offsetIn + (b.pointIn >>> 16) * s.pitchIn + (b.pointIn & 0xFFFF) * bpp;
    const dst = ctx.memoryObject(s.destin, "the 2D surfaces' destination").base + s.offsetOut + (b.pointOut >>> 16) * s.pitchOut + (b.pointOut & 0xFFFF) * bpp;
    copyLines(src, s.pitchIn, dst, s.pitchOut, width * bpp, height, "the image blit");
  }
  function memoryCopy(c) {
    if ((c.format & 0xFF) !== 1 || ((c.format >>> 8) & 0xFF) !== 1) fault("memory copy increments " + hex(c.format));
    const src = ctx.memoryObject(c.bufferIn, "the memory copy's source").base + c.offsetIn, dst = ctx.memoryObject(c.bufferOut, "the memory copy's destination").base + c.offsetOut;
    copyLines(src, c.pitchIn, dst, c.pitchOut, c.lineLength, c.lineCount, "the memory copy");
  }

  // ---- scan out ----
  // The display shows the memory its start register names with the geometry the CRTC registers hold, from the GL
  // surface drawn there when one matches it and from memory otherwise
  const display = { start: -1, mode: "", tex: null, writes: -1, width: 0, height: 0 };
  function displayTexture(start, mode) {
    const bytes = mode.pitch * mode.height, key = JSON.stringify(mode);
    flushOverlapping(start, bytes, null);
    const writes = writesOver(start, bytes);
    if (display.tex !== null && display.start === start && display.mode === key && display.writes === writes) return display.tex;
    sized(display, mode.width, mode.height, () => {});
    bindTexture(0, gl.TEXTURE_2D, display.tex);
    const color = mode.bpp === 4 ? 5 : mode.format565 ? 3 : 2;
    gl.texSubImage2D(gl.TEXTURE_2D, 0, 0, 0, mode.width, mode.height, gl.RGBA, gl.UNSIGNED_BYTE, flipRows(surfaceDecode(color, bytesAt(start, bytes, "the displayed memory"), mode.width, mode.height, mode.pitch, null), mode.width, mode.height));
    display.start = start; display.mode = key; display.writes = writes;
    return display.tex;
  }
  // The programs drawing one texture over the whole target, the scan out dropping alpha, the copy keeping it and
  // the shrink averaging each block of a larger surface into one pixel
  const quadVertex = "#version 300 es\nin vec2 p;\nout vec2 t;\nvoid main() { t = p * 0.5 + 0.5; gl_Position = vec4(p, 0.0, 1.0); }\n";
  const quadPrograms = new Map();
  function quadProgram(name, body) {
    let p = quadPrograms.get(name);
    if (p !== undefined) return p;
    const vs = compiled(gl.VERTEX_SHADER, quadVertex), fs = compiled(gl.FRAGMENT_SHADER, "#version 300 es\nprecision highp float;\nprecision highp sampler2D;\nin vec2 t;\nuniform sampler2D tex;\nuniform int k;\nout vec4 o;\nvoid main() { " + body + " }\n");
    const id = gl.createProgram(); gl.attachShader(id, vs); gl.attachShader(id, fs); gl.bindAttribLocation(id, 0, "p"); gl.linkProgram(id);
    if (!gl.getProgramParameter(id, gl.LINK_STATUS)) fault("the " + name + " program does not link: " + gl.getProgramInfoLog(id));
    useProgram(id);
    gl.uniform1i(gl.getUniformLocation(id, "tex"), 0);
    p = { id, k: gl.getUniformLocation(id, "k"), scale: -1 };
    quadPrograms.set(name, p);
    return p;
  }
  function scanOutProgram() { return quadProgram("scan out", "o = vec4(texture(tex, t).rgb, 1.0);"); }
  function copyProgram() { return quadProgram("copy", "o = texture(tex, t);"); }
  function depthProgram() { return quadProgram("depth", "ivec2 b = ivec2(gl_FragCoord.xy) * k + k / 2; uint v = uint(texelFetch(tex, b, 0).r * 16777215.0 + 0.5); o = vec4(float(v & 255u), float((v >> 8) & 255u), float(v >> 16), 0.0) / 255.0;"); }
  function shrinkProgram() { return quadProgram("shrink", "ivec2 b = ivec2(gl_FragCoord.xy) * k; vec4 sum = vec4(0.0); for (int y = 0; y < k; y++) for (int x = 0; x < k; x++) sum += texelFetch(tex, b + ivec2(x, y), 0); o = sum / float(k * k);"); }
  let quadReady = false;
  function quadVAO() {
    if (quadReady) return blitVAO;
    quadReady = true;
    bindVertexArray(blitVAO);
    const buffer = gl.createBuffer();
    gl.bindBuffer(gl.ARRAY_BUFFER, buffer);
    gl.bufferData(gl.ARRAY_BUFFER, new Float32Array([-1, -1, 1, -1, -1, 1, 1, 1]), gl.STATIC_DRAW);
    gl.enableVertexAttribArray(0);
    gl.vertexAttribPointer(0, 2, gl.FLOAT, false, 0, 0);
    return blitVAO;
  }
  function sizeScreen(width, height) { if (screen.width !== width || screen.height !== height) { screen.width = width; screen.height = height; } viewport(width, height); }
  // The image last scanned out, the canvas repainted only when the displayed image, its geometry or the canvas changed
  const presented = { start: -1, mode: "", tex: null, changes: -1, width: 0, height: 0 };
  function unchanged(start, key, tex, changes, width, height) { return presented.start === start && presented.mode === key && presented.tex === tex && presented.changes === changes && presented.width === width && presented.height === height && screen.width === width && screen.height === height; }
  function present(start) {
    if (gl === null) return;
    const mode = ctx.display();
    counts.frames++;
    if (!shown) { shown = true; bridge.shown(); }
    if (mode.on) scale = wantedScale(mode);
    const key = JSON.stringify(mode), width = mode.width * scale, height = mode.height * scale;
    if (!mode.on) {
      if (unchanged(-1, key, null, -1, width, height)) return;
      bindFramebuffer(null); quadState(); sizeScreen(width, height); gl.clearColor(0, 0, 0, 1); gl.clear(gl.COLOR_BUFFER_BIT);
      presented.start = -1; presented.mode = key; presented.tex = null; presented.changes = -1; presented.width = width; presented.height = height;
      return;
    }
    if (mode.bpp !== 4 && mode.bpp !== 2) fault("the display scans out " + mode.bpp + " bytes per pixel");
    const s = surfaces.get(start >>> 0);
    let tex, changes;
    if (s !== undefined && s.width === mode.width && s.height === mode.height && !s.swizzled && s.pitch === mode.pitch && s.bpp === mode.bpp) { colorCurrent(s); tex = s.color; changes = s.changes; }
    else { tex = displayTexture(start >>> 0, mode); changes = display.writes; }
    if (unchanged(start >>> 0, key, tex, changes, width, height)) return;
    bindFramebuffer(null);
    quadState();
    sizeScreen(width, height);
    useProgram(scanOutProgram().id);
    bindTexture(0, gl.TEXTURE_2D, tex);
    gl.drawArrays(gl.TRIANGLE_STRIP, 0, 4);
    presented.start = start >>> 0; presented.mode = key; presented.tex = tex; presented.changes = changes; presented.width = width; presented.height = height;
    if (counts.frames % ERROR_CHECK_FRAMES === 0) checkError("the frames' draws and scan out");
  }
  function status() { return counts.frames + " frames, " + counts.flips + " flips, " + counts.draws + " draws, " + counts.clears + " clears, " + counts.copies + " copies"; }
  function flip() { counts.flips++; }
  return { method, method2D, present, status, flip };
}
`

// The decoders turning a texture's bytes into RGBA pixels, the swizzled formats read through the bit masks the
// NV2A interleaves coordinates with, the block compressed formats through their colour endpoints and the linear
// formats row by row at their pitch, palette entries read where an indexed format names them, and the surface
// formats decoded into RGBA and encoded back into memory by their colour format
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
// The surface colour formats' bytes per pixel, X1R5G5B5 1 and 2, R5G6B5 3, X8R8G8B8 4 and 5, X1A7R8G8B8 6 and 7, A8R8G8B8 8, B8 9 and G8B8 10
const SURFACE_BYTES = { 1: 2, 2: 2, 3: 2, 4: 4, 5: 4, 6: 4, 7: 4, 8: 4, 9: 1, 10: 2 };
function surfaceDecode(color, data, width, height, pitch, swizzle) {
  const bpp = SURFACE_BYTES[color], out = new Uint8Array(width * height * 4), view = new DataView(data.buffer, data.byteOffset, data.byteLength);
  for (let y = 0; y < height; y++) for (let x = 0; x < width; x++) {
    const src = swizzle === null ? y * pitch + x * bpp : swizzle[y * width + x] * bpp, o = (y * width + x) * 4;
    switch (color) {
    case 1: case 2: { const c = view.getUint16(src, true); out[o] = ((c >>> 10) & 0x1F) * 255 / 31; out[o + 1] = ((c >>> 5) & 0x1F) * 255 / 31; out[o + 2] = (c & 0x1F) * 255 / 31; out[o + 3] = 255; break; }
    case 3: rgb565(view.getUint16(src, true), out, o); out[o + 3] = 255; break;
    case 4: case 5: out[o] = data[src + 2]; out[o + 1] = data[src + 1]; out[o + 2] = data[src]; out[o + 3] = 255; break;
    case 6: case 7: out[o] = data[src + 2]; out[o + 1] = data[src + 1]; out[o + 2] = data[src]; out[o + 3] = (data[src + 3] & 0x7F) * 255 / 127; break;
    case 8: out[o] = data[src + 2]; out[o + 1] = data[src + 1]; out[o + 2] = data[src]; out[o + 3] = data[src + 3]; break;
    case 9: out[o] = 0; out[o + 1] = 0; out[o + 2] = data[src]; out[o + 3] = 255; break;
    case 10: out[o] = 0; out[o + 1] = data[src + 1]; out[o + 2] = data[src]; out[o + 3] = 255; break;
    default: fault("surface colour format " + color);
    }
  }
  return out;
}
function surfaceEncode(color, rgba, width, height, pitch, swizzle, out) {
  const bpp = SURFACE_BYTES[color], view = new DataView(out.buffer, out.byteOffset, out.byteLength);
  for (let y = 0; y < height; y++) for (let x = 0; x < width; x++) {
    const dst = swizzle === null ? y * pitch + x * bpp : swizzle[y * width + x] * bpp, i = (y * width + x) * 4, r = rgba[i], g = rgba[i + 1], b = rgba[i + 2], a = rgba[i + 3];
    switch (color) {
    case 1: case 2: view.setUint16(dst, ((r >>> 3) << 10) | ((g >>> 3) << 5) | (b >>> 3) | (color === 2 ? 0x8000 : 0), true); break;
    case 3: view.setUint16(dst, ((r >>> 3) << 11) | ((g >>> 2) << 5) | (b >>> 3), true); break;
    case 4: case 5: out[dst] = b; out[dst + 1] = g; out[dst + 2] = r; out[dst + 3] = color === 5 ? 0xFF : 0; break;
    case 6: case 7: out[dst] = b; out[dst + 1] = g; out[dst + 2] = r; out[dst + 3] = (a >>> 1) | (color === 7 ? 0x80 : 0); break;
    case 8: out[dst] = b; out[dst + 1] = g; out[dst + 2] = r; out[dst + 3] = a; break;
    case 9: out[dst] = b; break;
    case 10: out[dst] = b; out[dst + 1] = g; break;
    default: fault("surface colour format " + color);
    }
  }
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

// The 2D surfaces class's colour formats by bytes per pixel, as a JavaScript object
func surfaces2DBytesJS() string {
	parts := make([]string, 0, len(surfaces2DBytes))
	for _, f := range surfaces2DFormats {
		parts = append(parts, fmt.Sprintf("0x%X: %d", f, surfaces2DBytes[f]))
	}
	return strings.Join(parts, ", ")
}

// Writes the 3D class's method names, the engine's limits and layouts, the unnamed methods it keeps and the 2D classes
func (e *emitter) writeGraphicsConstants() {
	e.js("const KELVIN = { %s };", kelvinMethodTable())
	e.js("const KELVIN_ATTRIBUTES = %d, KELVIN_PROGRAM_SLOTS = %d, KELVIN_CONSTANTS = %d, KELVIN_STAGES = %d, KELVIN_STAGE_STRIDE = 0x%X, KELVIN_MATRIX_WORDS = %d, KELVIN_COMBINERS = %d, KELVIN_WINDOW_CLIPS = %d, KELVIN_TEXGEN_STRIDE = 0x%X, KELVIN_TEXGEN_PLANE_STRIDE = 0x%X;", kelvinAttributes, kelvinProgramSlots, kelvinConstants, kelvinStages, kelvinStageStride, kelvinMatrixWords, kelvinCombiners, kelvinWindowClips, kelvinTexgenStride, kelvinTexgenPlaneStride)
	e.js("const KELVIN_LIGHTS = %d, KELVIN_LIGHT_STRIDE = 0x%X, KELVIN_BACK_LIGHT_STRIDE = 0x%X, KELVIN_SPECULAR_WORDS = %d, KELVIN_POINT_PARAMS = %d, KELVIN_SKIN_MATRICES = %d, KELVIN_CUBE_FACES = %d, KELVIN_CUBE_FACE_ALIGN = %d;", kelvinLights, kelvinLightStride, kelvinBackLightStride, kelvinSpecularWords, kelvinPointParams, kelvinSkinMatrices, kelvinCubeFaces, kelvinCubeFaceAlign)
	e.js("const KELVIN_XF_COMPOSITE = %d, KELVIN_XF_PROJECTION = %d, KELVIN_XF_MODEL_VIEW = %d, KELVIN_XF_INVERSE_MODEL_VIEW = %d, KELVIN_XF_EYE = %d, KELVIN_XF_FOG_PLANE = %d, KELVIN_XF_VIEWPORT_SCALE = %d, KELVIN_XF_VIEWPORT_OFFSET = %d, KELVIN_XF_TEXGEN_PLANE = %d, KELVIN_XF_TEXTURE_MATRIX = %d, KELVIN_XF_MATRIX_STRIDE = %d;", kelvinXFComposite, kelvinXFProjection, kelvinXFModelView, kelvinXFInverseMV, kelvinXFEye, kelvinXFFogPlane, kelvinXFViewportScale, kelvinXFViewportOffset, kelvinXFTexgenPlane, kelvinXFTextureMatrix, kelvinXFMatrixStride)
	e.js("const KELVIN_KEPT = new Set([%s]);", kelvinKeptList())
	e.js("const SURFACES_2D_CLASS = 0x%X, IMAGE_BLIT_CLASS = 0x%X, MEMORY_COPY_CLASS = 0x%X, BLIT_SOURCE_COPY = 0x%X;", surfaces2DClass, imageBlitClass, memoryCopyClass, blitSourceCopy)
	e.js("const SURFACES_2D_BYTES = { %s };", surfaces2DBytesJS())
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
