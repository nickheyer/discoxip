// Maps the D3D device state the derived draw paths produce onto Three.js materials
// Vertex programs come from the executable's shader blobs, fixed function draws
// transform by the derived matrices, and the texture stages become fragment code.

import { matMul } from './derived.js';

// D3D texture argument selectors and modifiers
const TA_DIFFUSE = 0, TA_CURRENT = 1, TA_TEXTURE = 2, TA_TFACTOR = 3, TA_SPECULAR = 4, TA_TEMP = 5;
const TA_COMPLEMENT = 0x10, TA_ALPHAREPLICATE = 0x20;

// D3D texture operations by value
const TOP = { 1: 'DISABLE', 2: 'SELECTARG1', 3: 'SELECTARG2', 4: 'MODULATE', 5: 'MODULATE2X', 6: 'MODULATE4X', 7: 'ADD', 8: 'ADDSIGNED', 9: 'ADDSIGNED2X', 10: 'SUBTRACT', 11: 'ADDSMOOTH', 12: 'BLENDDIFFUSEALPHA', 13: 'BLENDCURRENTALPHA', 14: 'BLENDTEXTUREALPHA', 15: 'BLENDFACTORALPHA', 16: 'BLENDTEXTUREALPHAPM', 22: 'DOTPRODUCT3', 23: 'MULTIPLYADD', 24: 'LERP' };

// Xbox render state enums use OpenGL numbering
const GL_NEVER = 0x200, GL_ALWAYS = 0x207;

export class DevicePipeline {
  constructor(THREE, model, textures) {
    this.THREE = THREE;
    this.model = model;
    this.textures = textures;
    this.programs = model.programs;
    this.declarations = model.declarations;
    this.sources = new Map();
    this.blendFactors = {
      0: THREE.ZeroFactor, 1: THREE.OneFactor, 0x300: THREE.SrcColorFactor, 0x301: THREE.OneMinusSrcColorFactor,
      0x302: THREE.SrcAlphaFactor, 0x303: THREE.OneMinusSrcAlphaFactor, 0x304: THREE.DstAlphaFactor, 0x305: THREE.OneMinusDstAlphaFactor,
      0x306: THREE.DstColorFactor, 0x307: THREE.OneMinusDstColorFactor, 0x308: THREE.SrcAlphaSaturateFactor,
    };
    this.blendOps = { 0x8006: THREE.AddEquation, 0x800A: THREE.SubtractEquation, 0x800B: THREE.ReverseSubtractEquation, 0x8007: THREE.MinEquation, 0x8008: THREE.MaxEquation };
    this.depthFuncs = { 0x200: THREE.NeverDepth, 0x201: THREE.LessDepth, 0x202: THREE.EqualDepth, 0x203: THREE.LessEqualDepth, 0x204: THREE.GreaterDepth, 0x205: THREE.NotEqualDepth, 0x206: THREE.GreaterEqualDepth, 0x207: THREE.AlwaysDepth };
    this.wrapModes = { 1: THREE.RepeatWrapping, 2: THREE.MirroredRepeatWrapping, 3: THREE.ClampToEdgeWrapping, 4: THREE.ClampToEdgeWrapping, 5: THREE.ClampToEdgeWrapping };
  }

  // Makes the material a Three.js mesh keeps, updated from each frame's draw snapshot
  createMaterial() {
    const THREE = this.THREE;
    const material = new THREE.RawShaderMaterial({ uniforms: {}, vertexShader: 'void main(){gl_Position=vec4(0.0);}', fragmentShader: 'void main(){gl_FragColor=vec4(0.0);}', transparent: true });
    material._derivedKey = null;
    return material;
  }

  // Applies a draw snapshot to a material for a geometry whose attributes are keyed by byte offset
  update(material, snapshot, geometry, matrices, fixedFunction = false) {
    const THREE = this.THREE;
    const plan = this.plan(snapshot, geometry, fixedFunction);
    if (plan.key !== material._derivedKey) {
      const src = this.sources.get(plan.key) || this.compile(plan);
      this.sources.set(plan.key, src);
      material.vertexShader = src.vertex;
      material.fragmentShader = src.fragment;
      material.uniforms = {};
      for (const name of src.uniformNames) material.uniforms[name] = { value: null };
      material._derivedKey = plan.key;
      material.needsUpdate = true;
    }
    const u = material.uniforms;
    if (plan.program) {
      const regs = [];
      for (let i = 0; i < plan.constantCount; i++) {
        const c = snapshot.constants[i];
        regs.push(c ? new THREE.Vector4(c[0], c[1], c[2], c[3]) : new THREE.Vector4(0, 0, 0, 0));
      }
      u.c.value = regs;
    } else {
      const wvp = matMul(matMul(matrices.world, matrices.view), matrices.projection);
      u.u_wvp.value = new THREE.Matrix4().fromArray(wvp);
    }
    const tf = snapshot.rs.TEXTUREFACTOR >>> 0;
    u.u_tfactor.value = new THREE.Vector4(((tf >>> 16) & 255) / 255, ((tf >>> 8) & 255) / 255, (tf & 255) / 255, (tf >>> 24) / 255);
    for (const st of plan.stages) {
      if (st.texture) {
        const tex = this.resolveTexture(snapshot.textures[st.index], st);
        u['u_tex' + st.index].value = tex;
      }
      if (st.transform) {
        const m = snapshot.transforms[2 + st.index];
        u['u_texXf' + st.index].value = new THREE.Matrix4().fromArray(m);
      }
    }
    if (plan.alphaTest) u.u_alphaRef.value = (snapshot.rs.ALPHAREF & 255) / 255;
    this.applyRenderStates(material, snapshot.rs);
  }

  // Wrap modes come from the stage's address states, applied to the bound texture
  resolveTexture(handle, st) {
    const tex = this.textures.resolve(handle);
    if (!tex) throw new Error(`texture ${JSON.stringify(handle)} bound at stage ${st.index} is not loaded`);
    const THREE = this.THREE;
    const wrapU = this.wrapModes[st.addressU], wrapV = this.wrapModes[st.addressV];
    if (wrapU === undefined || wrapV === undefined) throw new Error(`stage ${st.index} address modes ${st.addressU}/${st.addressV} are not mapped`);
    if (tex.wrapS !== wrapU || tex.wrapT !== wrapV) {
      tex.wrapS = wrapU;
      tex.wrapT = wrapV;
      tex.needsUpdate = true;
    }
    tex.flipY = false;
    tex.colorSpace = THREE.SRGBColorSpace;
    return tex;
  }

  applyRenderStates(material, rs) {
    const THREE = this.THREE;
    if (rs.ALPHABLENDENABLE) {
      const src = this.blendFactors[rs.SRCBLEND], dst = this.blendFactors[rs.DESTBLEND];
      if (src === undefined || dst === undefined) throw new Error(`blend factors ${rs.SRCBLEND}/${rs.DESTBLEND} are not mapped`);
      const op = rs.BLENDOP === undefined ? THREE.AddEquation : this.blendOps[rs.BLENDOP];
      if (op === undefined) throw new Error(`blend operation ${rs.BLENDOP} is not mapped`);
      material.blending = THREE.CustomBlending;
      material.blendSrc = src;
      material.blendDst = dst;
      material.blendEquation = op;
      material.blendSrcAlpha = src;
      material.blendDstAlpha = dst;
      material.blendEquationAlpha = op;
    } else {
      material.blending = THREE.NoBlending;
    }
    material.depthTest = !!rs.ZENABLE;
    material.depthWrite = !!rs.ZWRITEENABLE;
    const zf = this.depthFuncs[rs.ZFUNC];
    if (zf === undefined) throw new Error(`depth function ${rs.ZFUNC} is not mapped`);
    material.depthFunc = zf;
    switch (rs.CULLMODE) {
      case 0: material.side = THREE.DoubleSide; break;
      case 0x900: material.side = THREE.BackSide; break;
      case 0x901: material.side = THREE.FrontSide; break;
      default: throw new Error(`cull mode ${rs.CULLMODE} is not mapped`);
    }
    material.colorWrite = (rs.COLORWRITEENABLE & 0x01010101) !== 0;
    switch (rs.FILLMODE) {
      case 0x1B02: material.wireframe = false; break;
      case 0x1B01: material.wireframe = true; break;
      default: throw new Error(`fill mode ${rs.FILLMODE} is not mapped`);
    }
    material.transparent = true;
  }

  // Decides the shader structure for a snapshot: the vertex path and the active stages
  plan(snapshot, geometry, fixedFunction) {
    const handle = fixedFunction ? geometry.format : snapshot.program;
    let program = null, declaration = null;
    if (handle && typeof handle === 'object' && handle.program !== undefined) {
      if (handle.program !== 0) {
        program = this.programs[hex8(handle.program)];
        if (!program) throw new Error(`vertex program ${hex8(handle.program)} was not decoded`);
        declaration = this.declarations[hex8(handle.declaration)];
        if (!declaration) throw new Error(`vertex declaration ${hex8(handle.declaration)} was not decoded`);
      }
    } else if (typeof handle === 'number') {
      if (handle !== geometry.format) throw new Error(`vertex shader handle 0x${handle.toString(16)} is neither a created shader nor the mesh format 0x${geometry.format.toString(16)}`);
    } else {
      throw new Error(`no vertex shader is set for the draw (${JSON.stringify(handle)})`);
    }
    const stages = [];
    for (let i = 0; i < 4; i++) {
      const s = snapshot.stages[i] || {};
      const colorOp = s.COLOROP === undefined ? 1 : s.COLOROP;
      if (colorOp === 1 && i > 0) break;
      const tci = s.TEXCOORDINDEX === undefined ? i : s.TEXCOORDINDEX;
      if (tci >>> 16) throw new Error(`stage ${i} texture coordinate generation 0x${tci.toString(16)} is not mapped`);
      const ttf = s.TEXTURETRANSFORMFLAGS === undefined ? 0 : s.TEXTURETRANSFORMFLAGS;
      if (ttf & 0xFF00) throw new Error(`stage ${i} texture transform flags 0x${ttf.toString(16)} are not mapped`);
      const st = {
        index: i, colorOp, alphaOp: s.ALPHAOP === undefined ? 1 : s.ALPHAOP,
        colorArg0: s.COLORARG0 === undefined ? TA_CURRENT : s.COLORARG0, colorArg1: s.COLORARG1 === undefined ? TA_TEXTURE : s.COLORARG1, colorArg2: s.COLORARG2 === undefined ? TA_CURRENT : s.COLORARG2,
        alphaArg0: s.ALPHAARG0 === undefined ? TA_CURRENT : s.ALPHAARG0, alphaArg1: s.ALPHAARG1 === undefined ? TA_TEXTURE : s.ALPHAARG1, alphaArg2: s.ALPHAARG2 === undefined ? TA_CURRENT : s.ALPHAARG2,
        resultArg: s.RESULTARG === undefined ? TA_CURRENT : s.RESULTARG,
        texcoord: tci & 0xFFFF, transform: ttf & 0xFF, texture: !!snapshot.textures[i],
        addressU: s.ADDRESSU === undefined ? 1 : s.ADDRESSU, addressV: s.ADDRESSV === undefined ? 1 : s.ADDRESSV,
      };
      stages.push(st);
      if (colorOp === 1) break;
    }
    const alphaTest = snapshot.rs.ALPHATESTENABLE ? (snapshot.rs.ALPHAFUNC === undefined ? GL_ALWAYS : snapshot.rs.ALPHAFUNC) : 0;
    const attributes = {};
    if (program) {
      for (const el of declaration.elements) {
        const a = geometry.byOffset[el.offset];
        if (!a) throw new Error(`declaration ${hex8(handle.declaration)} reads offset ${el.offset} which mesh format 0x${geometry.format.toString(16)} does not store`);
        if (a.type !== el.type) throw new Error(`declaration ${hex8(handle.declaration)} reads ${el.type} at offset ${el.offset} but the mesh stores ${a.type}`);
        attributes[el.register] = el.offset;
      }
    } else {
      for (const a of geometry.attributes) attributes[a.semantic] = a.offset;
      if (attributes.position === undefined) throw new Error(`mesh format 0x${geometry.format.toString(16)} has no position for fixed function drawing`);
    }
    let constantCount = 0;
    if (program) for (const c of program.constants) if (c >= 0 && c + 1 > constantCount) constantCount = c + 1;
    const key = JSON.stringify({ p: program ? program.va : 0, attributes, stages: stages.map((s) => [s.colorOp, s.alphaOp, s.colorArg0, s.colorArg1, s.colorArg2, s.alphaArg0, s.alphaArg1, s.alphaArg2, s.resultArg, s.texcoord, s.transform, s.texture]), alphaTest });
    return { key, program, declaration, stages, alphaTest, attributes, constantCount };
  }

  // Generates the GLSL pair for a plan
  compile(plan) {
    const uniformNames = ['u_tfactor'];
    let vertex = 'precision highp float;\n';
    const varyings = 'varying vec4 vD0; varying vec4 vD1; varying vec4 vT0; varying vec4 vT1; varying vec4 vT2; varying vec4 vT3;\n';
    if (plan.program) {
      const prog = plan.program;
      for (const [reg, off] of Object.entries(plan.attributes)) vertex += `attribute vec4 a_${off};\n#define v${reg} a_${off}\n`;
      for (const reg of prog.inputs) if (plan.attributes[reg] === undefined) vertex += `const vec4 v${reg} = vec4(0.0, 0.0, 0.0, 1.0);\n`;
      vertex += `uniform vec4 c[${Math.max(plan.constantCount, 1)}];\n`;
      uniformNames.push('c');
      // The viewport transform the program applies is undone so WebGL can apply its own.
      vertex += 'const vec4 cneg38 = vec4(1.0); const vec4 cneg37 = vec4(0.0);\n';
      vertex += varyings;
      vertex += 'void main() {\n' + prog.glsl + '\n';
      vertex += 'gl_Position = vec4((oPos.xyz - cneg37.xyz) / cneg38.xyz * oPos.w, oPos.w);\n';
      vertex += 'vD0 = oD0; vD1 = oD1; vT0 = oT0; vT1 = oT1; vT2 = oT2; vT3 = oT3;\n}\n';
    } else {
      const a = plan.attributes;
      vertex += `attribute vec3 a_${a.position};\n`;
      if (a.diffuse !== undefined) vertex += `attribute vec4 a_${a.diffuse};\n`;
      for (let i = 0; i < 4; i++) if (a['texcoord' + i] !== undefined) vertex += `attribute vec2 a_${a['texcoord' + i]};\n`;
      vertex += 'uniform mat4 u_wvp;\n';
      uniformNames.push('u_wvp');
      vertex += varyings;
      vertex += `void main() {\ngl_Position = u_wvp * vec4(a_${a.position}, 1.0);\n`;
      // A vertex attribute the stream does not supply reads the register's reset value.
      vertex += a.diffuse !== undefined ? `vD0 = a_${a.diffuse};\n` : 'vD0 = vec4(0.0, 0.0, 0.0, 1.0);\n';
      vertex += 'vD1 = vec4(0.0, 0.0, 0.0, 1.0);\n';
      for (let i = 0; i < 4; i++) vertex += a['texcoord' + i] !== undefined ? `vT${i} = vec4(a_${a['texcoord' + i]}, 0.0, 1.0);\n` : `vT${i} = vec4(0.0, 0.0, 0.0, 1.0);\n`;
      vertex += '}\n';
    }
    let fragment = 'precision mediump float;\n' + varyings + 'uniform vec4 u_tfactor;\n';
    for (const st of plan.stages) {
      if (st.texture) { fragment += `uniform sampler2D u_tex${st.index};\n`; uniformNames.push('u_tex' + st.index); }
      if (st.transform) { fragment += `uniform mat4 u_texXf${st.index};\n`; uniformNames.push('u_texXf' + st.index); }
    }
    if (plan.alphaTest) { fragment += 'uniform float u_alphaRef;\n'; uniformNames.push('u_alphaRef'); }
    fragment += 'void main() {\nvec4 diffuse = vD0; vec4 specular = vD1; vec4 current = diffuse; vec4 temp = vec4(0.0);\n';
    for (const st of plan.stages) {
      const i = st.index;
      if (st.colorOp === 1) {
        // A disabled first stage passes the vertex color through.
        break;
      }
      let uv = `vT${st.texcoord}.xy`;
      if (st.transform) uv = `(u_texXf${i} * vec4(vT${st.texcoord}.xy, 0.0, 1.0)).xy`;
      // An unbound texture stage reads as zero, the NV2A value for a disabled texture unit.
      fragment += st.texture ? `vec4 t${i} = texture2D(u_tex${i}, ${uv});\n` : `vec4 t${i} = vec4(0.0);\n`;
      const arg = (v) => {
        let e;
        switch (v & 7) {
          case TA_DIFFUSE: e = 'diffuse'; break;
          case TA_CURRENT: e = 'current'; break;
          case TA_TEXTURE: e = `t${i}`; break;
          case TA_TFACTOR: e = 'u_tfactor'; break;
          case TA_SPECULAR: e = 'specular'; break;
          case TA_TEMP: e = 'temp'; break;
          default: throw new Error(`stage ${i} texture argument ${v} is not mapped`);
        }
        if (v & TA_ALPHAREPLICATE) e = `${e}.aaaa`;
        if (v & TA_COMPLEMENT) e = `(vec4(1.0) - ${e})`;
        return e;
      };
      const op = (opValue, a0, a1, a2, sw) => {
        const name = TOP[opValue];
        const x = `${arg(a1)}.${sw}`, y = `${arg(a2)}.${sw}`, z = `${arg(a0)}.${sw}`;
        switch (name) {
          case 'SELECTARG1': return x;
          case 'SELECTARG2': return y;
          case 'MODULATE': return `(${x} * ${y})`;
          case 'MODULATE2X': return `(${x} * ${y} * 2.0)`;
          case 'MODULATE4X': return `(${x} * ${y} * 4.0)`;
          case 'ADD': return `(${x} + ${y})`;
          case 'ADDSIGNED': return `(${x} + ${y} - 0.5)`;
          case 'ADDSIGNED2X': return `((${x} + ${y} - 0.5) * 2.0)`;
          case 'SUBTRACT': return `(${x} - ${y})`;
          case 'ADDSMOOTH': return `(${x} + ${y} - ${x} * ${y})`;
          case 'BLENDDIFFUSEALPHA': return `mix(${y}, ${x}, diffuse.a)`;
          case 'BLENDCURRENTALPHA': return `mix(${y}, ${x}, current.a)`;
          case 'BLENDTEXTUREALPHA': return `mix(${y}, ${x}, t${i}.a)`;
          case 'BLENDFACTORALPHA': return `mix(${y}, ${x}, u_tfactor.a)`;
          case 'BLENDTEXTUREALPHAPM': return `(${x} + ${y} * (1.0 - t${i}.a))`;
          case 'DOTPRODUCT3': return `vec4(dot(${arg(a1)}.rgb - 0.5, ${arg(a2)}.rgb - 0.5) * 4.0).${sw}`;
          case 'MULTIPLYADD': return `(${z} + ${x} * ${y})`;
          case 'LERP': return `mix(${y}, ${x}, ${z})`;
          case 'DISABLE': return null;
        }
        throw new Error(`stage ${i} texture operation ${opValue} is not mapped`);
      };
      const color = op(st.colorOp, st.colorArg0, st.colorArg1, st.colorArg2, 'rgb');
      const alpha = op(st.alphaOp, st.alphaArg0, st.alphaArg1, st.alphaArg2, 'a');
      const dst = st.resultArg === TA_TEMP ? 'temp' : 'current';
      fragment += `{ vec3 rgb = ${color}; float a = ${alpha === null ? (i === 0 ? 'diffuse.a' : 'current.a') : alpha}; ${dst} = clamp(vec4(rgb, a), 0.0, 1.0); }\n`;
    }
    if (plan.alphaTest) fragment += alphaTestGLSL(plan.alphaTest);
    fragment += 'gl_FragColor = current;\n}\n';
    return { vertex, fragment, uniformNames };
  }
}

// Emits the alpha test for a GL comparison function against the reference uniform
function alphaTestGLSL(func) {
  switch (func) {
    case GL_NEVER: return 'discard;\n';
    case 0x201: return 'if (!(current.a < u_alphaRef)) discard;\n';
    case 0x202: return 'if (!(current.a == u_alphaRef)) discard;\n';
    case 0x203: return 'if (!(current.a <= u_alphaRef)) discard;\n';
    case 0x204: return 'if (!(current.a > u_alphaRef)) discard;\n';
    case 0x205: return 'if (!(current.a != u_alphaRef)) discard;\n';
    case 0x206: return 'if (!(current.a >= u_alphaRef)) discard;\n';
    case GL_ALWAYS: return '';
  }
  throw new Error(`alpha function ${func} is not mapped`);
}

function hex8(v) {
  return (v >>> 0).toString(16).toUpperCase().padStart(8, '0');
}

// Describes exported mesh geometry for the pipeline: attributes by semantic and by byte offset
export function describeGeometry(entry) {
  const byOffset = {};
  for (const a of entry.attributes) byOffset[a.offset] = a;
  return { format: entry.format, stride: entry.stride, attributes: entry.attributes, byOffset };
}
