// Interpreter for the execution paths derived from the dashboard executable
// Runs the scene node draw methods, the material Apply methods and the device
// setup summarized in render.json against scene objects, producing D3D device state.

const f32 = Math.fround;
const u32 = (v) => v >>> 0;
const bitsBuf = new DataView(new ArrayBuffer(8));
const floatFromBits = (b) => { bitsBuf.setUint32(0, u32(b), true); return bitsBuf.getFloat32(0, true); };
const bitsFromFloat = (f) => { bitsBuf.setFloat32(0, f, true); return bitsBuf.getUint32(0, true); };
const hex8 = (v) => u32(v).toString(16).toUpperCase().padStart(8, '0');

// Rounds to nearest even like fistp under the default control word
function roundEven(v) {
  const r = Math.round(v);
  if (Math.abs(v % 1) === 0.5 && r % 2 !== 0) return r - 1;
  return r;
}

// Builds a stored value from little endian start bytes
function valueFromBytes(bytes) {
  for (let i = 0; i < 8; i++) bitsBuf.setUint8(i, i < bytes.length ? bytes[i] : 0);
  if (bytes.length === 8) return { f64: bitsBuf.getFloat64(0, true) };
  return { bits: bitsBuf.getUint32(0, true) };
}

export const IDENTITY = Float32Array.from([1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1]);

// Scene file field types as the executable's field tables number them
const FIELD_INT = 1, FIELD_BOOL = 2, FIELD_NUMBER = 3, FIELD_NODE = 9, FIELD_STRING = 10;

let nextObjectId = 1;

// An instance of a registered node class backed by a scene node
export class SceneObject {
  constructor(type, node) {
    this.id = nextObjectId++;
    this.type = type;
    this.node = node;
    this.slots = new Map();
  }
}

export const BUFFER_TYPE = '$buffer';

// Makes the loaded geometry object a mesh node points at, with the fields its draw reads
export function makeBuffer(model, geometry) {
  const obj = new SceneObject(BUFFER_TYPE, null);
  obj.geometry = geometry;
  const f = model.draw.buffer_fields;
  obj.slots.set(f.vertices, { vertices: geometry });
  obj.slots.set(f.indices, { indices: geometry });
  obj.slots.set(f.stride, geometry.stride);
  obj.slots.set(f.format, geometry.format);
  obj.slots.set(f.count, geometry.primitiveCount);
  return obj;
}

export function newState() {
  return { rs: {}, stages: {}, textures: {}, program: undefined, constants: {}, transforms: {}, stream: undefined, indices: undefined, draws: 0, returned: 0 };
}

export function cloneState(s) {
  const stages = {};
  for (const [k, v] of Object.entries(s.stages)) stages[k] = { ...v };
  return { rs: { ...s.rs }, stages, textures: { ...s.textures }, program: s.program, constants: { ...s.constants }, transforms: { ...s.transforms }, stream: s.stream, indices: s.indices, draws: s.draws, returned: s.returned };
}

export class DerivedModel {
  constructor(model) {
    this.model = model;
    this.nodes = model.exprs.nodes;
    this.materials = new Map();
    for (const m of model.materials) {
      this.materials.set(m.name, m);
      m._fields = new Map();
      for (const [off, b] of Object.entries(m.fields)) m._fields.set(Number(off), b);
    }
    this.globals = new Map();
    for (const [off, g] of Object.entries(model.globals)) this.globals.set(Number(off), valueFromBytes(g.init));
    this.clock = 0;
    this.hierarchyAlpha = 1;
    this.stateNames = model.render_state_names;
    this.stageNames = model.texture_stage_names;
    this.nvNames = model.nv_state_names;
    this.accessor = model.mesh_accessor;
    this.transforms = model.transforms;
    this.types = model.node_types;
    this.draw = model.draw;
    this.descToType = new Map();
    for (const t of Object.values(this.types)) this.descToType.set(t.desc, t.name);
    this.functions = new Map();
    for (const [va, paths] of Object.entries(this.draw.functions)) this.functions.set(Number(va), paths);
    this.hooks = new Map();
    this.onDraw = null;
    this.defaults = this.runPaths(model.render_state_defaults, this.newEnv(null), newState());
  }

  // Registers a runtime implementation for a draw method the model does not summarize
  hook(va, fn) {
    this.hooks.set(va, fn);
  }

  // Makes the executable's object for a scene node of a registered class
  newObject(typeName, node) {
    if (!this.types[typeName]) throw new Error(`node type ${typeName} is not registered by the executable`);
    return new SceneObject(typeName, node);
  }

  drawSlotFunction(typeName) {
    return this.typeSlot(typeName, this.draw.slot);
  }

  // Resolves a class vtable slot, inheriting down the class chain
  typeSlot(typeName, slot) {
    let t = this.types[typeName];
    if (!t) throw new Error(`node type ${typeName} is not registered by the executable`);
    while (t && t.slots.length <= slot / 4) t = this.types[t.parent];
    if (!t) throw new Error(`node type ${typeName} has no vtable slot 0x${slot.toString(16)}`);
    return t.slots[slot / 4];
  }

  // Reports whether a class descends from the class the descriptor names
  isKindOf(typeName, desc) {
    const target = this.descToType.get(desc);
    if (!target) throw new Error(`descriptor 0x${desc.toString(16)} names no registered node type`);
    for (let t = this.types[typeName]; t; t = this.types[t.parent]) {
      if (t.name === target) return true;
      if (!t.parent) break;
    }
    return false;
  }

  // Finds the scene file field a class declares at an object offset
  fieldAt(typeName, off) {
    for (let t = this.types[typeName]; t; t = this.types[t.parent]) {
      const f = t.fields[String(off)];
      if (f) return f;
      if (!t.parent) break;
    }
    return null;
  }

  ctorInit(typeName, off) {
    const t = this.types[typeName];
    return t ? t.init[String(off)] : undefined;
  }

  newEnv(thisObj, args = [], mat = null, node = null) {
    return { this: thisObj, args, mat, node, stack: new Map(), results: new Map(), mesh: null };
  }

  // Reads an object field: a runtime slot, a scene file field or the constructor default
  readField(obj, off, size) {
    if (!obj || !obj.slots) throw new Error(`field +0x${off.toString(16)} read on ${describe(obj)}`);
    if (obj.slots.has(off)) return obj.slots.get(off);
    if (obj.type === BUFFER_TYPE) throw new Error(`mesh buffer field +0x${off.toString(16)} is not one its draw reveals`);
    const f = this.fieldAt(obj.type, off);
    const v = f ? obj.node.derivedField(f.name, f.type) : undefined;
    if (v !== undefined) {
      switch (f.type) {
        case FIELD_NODE: return v ? v : 0;
        case FIELD_STRING: return { str: String(v) };
        case FIELD_BOOL: return v ? 1 : 0;
        case FIELD_INT: return u32(v);
        case FIELD_NUMBER: return size === 8 ? { f64: Number(v) } : bitsFromFloat(Number(v));
      }
      throw new Error(`field ${f.name} of ${obj.type} has scene type ${f.type} which the interpreter does not read`);
    }
    const init = this.ctorInit(obj.type, off);
    if (init) return init.size === 8 ? { f64: init.double || 0 } : u32(init.value);
    throw new Error(`${obj.type} object field +0x${off.toString(16)} is neither a scene field nor set by its constructor`);
  }

  writeField(obj, off, value) {
    obj.slots.set(off, value);
  }

  loadGlobal(off) {
    if (off === this.model.clock.va) return { f64: this.clock };
    if (off === this.model.hierarchy_alpha) return { bits: bitsFromFloat(this.hierarchyAlpha) };
    const g = this.globals.get(off);
    if (g === undefined) throw new Error(`global 0x${off.toString(16)} has no derived start value`);
    return g;
  }

  // Reads little endian bytes from a material object as an unsigned integer
  fieldInt(mat, off, size) {
    let v = 0;
    for (let i = size - 1; i >= 0; i--) {
      const b = mat._fields.get(off + i);
      if (b === undefined) throw new Error(`material ${mat.name} field +0x${(off + i).toString(16)} never written`);
      v = (v * 256) + b;
    }
    return u32(v);
  }

  storeMaterialField(mat, off, size, value) {
    if (typeof value === 'object' && value.f64 !== undefined) {
      bitsBuf.setFloat64(0, value.f64, true);
      for (let i = 0; i < 8; i++) mat._fields.set(off + i, bitsBuf.getUint8(i));
      return;
    }
    const bits = typeof value === 'object' ? value.bits : u32(value);
    for (let i = 0; i < size; i++) mat._fields.set(off + i, (bits >>> (8 * i)) & 0xFF);
  }

  // Evaluates a pooled expression node
  eval(idx, env) {
    const n = this.nodes[idx];
    switch (n.k) {
      case 'i': return u32(n.v);
      case 'f': return n.v !== undefined ? n.v : (n.s === 'NaN' ? NaN : (n.s === '+Inf' ? Infinity : -Infinity));
      case 'ptr':
        if (n.base === 'node') return env.node ? { obj: env.node, off: n.off } : 0;
        if (n.base === 'this') return { obj: env.this, off: n.off };
        if (n.base === '') return u32(n.off);
        if (n.base === 'stack') return { stack: n.off };
        return { ptr: n.base, off: n.off };
      case 'sym':
        if (n.name.startsWith('texture:')) return { texture: n.name };
        if (n.name.startsWith('arg')) return env.args[Number(n.name.slice(3))] ?? 0;
        if (n.name.startsWith('reg.') || n.name.startsWith('clobber.')) return 0;
        throw new Error(`unbound symbol ${n.name}`);
      case 'load': return this.evalLoad(n, env);
      case 'bin': return this.evalBin(n, env);
      case 'un': return this.evalUn(n, env);
      case 'cond': return this.evalCond(n, env) ? 1 : 0;
      case 'fcmp': return this.evalFcmp(n, env) ? 1 : 0;
      case 'not': return this.truthy(this.eval(n.a, env)) ? 0 : 1;
      case 'call': return this.evalCall(n, env);
      case 'block': return Float32Array.from(n.elems.flatMap((e) => this.floats(this.eval(e, env))));
      case 'elem': return this.vector(this.eval(n.a, env))[n.i];
      case 'retaddr': return 0;
      case 'undef': return 0;
    }
    throw new Error(`unknown expression kind ${n.k}`);
  }

  floats(v) {
    if (v instanceof Float32Array) return Array.from(v);
    return [this.scalar(v)];
  }

  scalar(v) {
    if (typeof v === 'number') return v;
    if (v && v.f64 !== undefined) return v.f64;
    if (v && v.bits !== undefined) return floatFromBits(v.bits);
    throw new Error(`expected a scalar, got ${describe(v)}`);
  }

  truthy(v) {
    return typeof v === 'number' ? v !== 0 : !!v;
  }

  // Integer view of a value: object handles compare by identity, void results carry nothing
  int(v) {
    if (typeof v === 'number') return u32(v);
    if (v && v.bits !== undefined) return v.bits;
    if (v && v.obj !== undefined) return u32(v.obj.id * 4096 + (v.off || 0));
    if (v && (v.ptr !== undefined || v.texture !== undefined || v.program !== undefined || v.str !== undefined || v.material !== undefined || v.vertices !== undefined || v.indices !== undefined || v.xapTexture !== undefined || v.stack !== undefined)) return 1;
    if (v && v.call !== undefined) return 0;
    throw new Error(`expected an integer, got ${describe(v)}`);
  }

  evalLoad(n, env) {
    n = n.off === undefined ? { ...n, off: 0 } : n;
    if (n.addr !== undefined) {
      const addr = this.eval(n.addr, env);
      if (addr && addr.obj !== undefined) return this.readField(addr.obj, addr.off, n.size);
      throw new Error(`load through ${describe(addr)} from ${this.show(n.addr)}`);
    }
    if (n.base === '') {
      const role = this.transforms[hex8(n.off)];
      if (role === 'view') return env.view;
      if (role === 'projection') return env.projection;
      const g = this.loadGlobal(n.off);
      if (n.size === 8) {
        if (g.f64 === undefined) throw new Error(`global 0x${n.off.toString(16)} holds an integer but is read as a double`);
        return { f64: g.f64 };
      }
      if (g.bits === undefined) {
        if (g.obj !== undefined || g.str !== undefined || g.material !== undefined) return g;
        throw new Error(`global 0x${n.off.toString(16)} holds a double but is read with size ${n.size}`);
      }
      return n.size === 4 ? g.bits : g.bits & ((1 << (8 * n.size)) - 1);
    }
    if (n.base === 'this') return this.readField(env.this, n.off, n.size);
    if (n.base === 'node') return this.readField(env.node, n.off, n.size);
    if (n.base === 'stack') {
      const v = env.stack.get(n.off);
      if (v === undefined) throw new Error(`uninitialized stack read at 0x${n.off.toString(16)}`);
      return v;
    }
    if (env.mat && n.base === env.mat.object) return this.fieldInt(env.mat, n.off, n.size);
    throw new Error(`load from ${n.base}`);
  }

  evalBin(n, env) {
    const op = n.op;
    if (op === 'fadd' || op === 'fsub' || op === 'fmul' || op === 'fdiv') {
      const a = this.scalar(this.eval(n.a, env)), b = this.scalar(this.eval(n.b, env));
      switch (op) {
        case 'fadd': return a + b;
        case 'fsub': return a - b;
        case 'fmul': return a * b;
        case 'fdiv': return a / b;
      }
    }
    const av = this.eval(n.a, env), bv = this.eval(n.b, env);
    // Pointer arithmetic on object handles keeps the object.
    if (op === 'add' && av && av.obj !== undefined && typeof bv === 'number') return { obj: av.obj, off: (av.off || 0) + (bv | 0) };
    if (op === 'add' && bv && bv.obj !== undefined && typeof av === 'number') return { obj: bv.obj, off: (bv.off || 0) + (av | 0) };
    const a = this.int(av), b = this.int(bv);
    switch (op) {
      case 'add': return u32(a + b);
      case 'sub': return u32(a - b);
      case 'mul': return u32(Math.imul(a, b));
      case 'and': return u32(a & b);
      case 'or': return u32(a | b);
      case 'xor': return u32(a ^ b);
      case 'shl': return u32(a << (b & 31));
      case 'shr': return u32(a >>> (b & 31));
      case 'sar': return u32((a | 0) >> (b & 31));
      case 'idiv': return u32(Math.trunc((a | 0) / (b | 0)));
      case 'idiv_rem': return u32((a | 0) % (b | 0));
      case 'udiv': return u32(Math.trunc(a / b));
      case 'udiv_rem': return u32(a % b);
    }
    throw new Error(`unknown binary op ${op}`);
  }

  evalUn(n, env) {
    const v = this.eval(n.a, env);
    switch (n.op) {
      case 'zx8': return this.int(v) & 0xFF;
      case 'zx16': return this.int(v) & 0xFFFF;
      case 'sx8': return u32((this.int(v) << 24) >> 24);
      case 'sx16': return u32((this.int(v) << 16) >> 16);
      case 'neg': return u32(-this.int(v));
      case 'not': return u32(~this.int(v));
      case 'i2f': return (this.int(v) | 0);
      case 'bits2f': return floatFromBits(this.int(v));
      case 'bits2d': return this.scalar(v);
      case 'f32': return f32(this.scalar(v));
      case 'f64': return this.scalar(v);
      case 'f2i': return u32(Math.trunc(this.scalar(v)));
      case 'f2i_rn': return u32(roundEven(this.scalar(v)));
      case 'f2bits': return bitsFromFloat(this.scalar(v));
      case 'fneg': return -this.scalar(v);
      case 'fabs': return Math.abs(this.scalar(v));
      case 'fsin': return Math.sin(this.scalar(v));
      case 'fcos': return Math.cos(this.scalar(v));
      case 'fsqrt': return Math.sqrt(this.scalar(v));
    }
    throw new Error(`unknown unary op ${n.op}`);
  }

  evalCond(n, env) {
    const ia = this.int(this.eval(n.a, env)), ib = this.int(this.eval(n.b, env));
    switch (n.op) {
      case 'eq': return ia === ib;
      case 'ne': return ia !== ib;
      case 'lt': return (ia | 0) < (ib | 0);
      case 'le': return (ia | 0) <= (ib | 0);
      case 'gt': return (ia | 0) > (ib | 0);
      case 'ge': return (ia | 0) >= (ib | 0);
      case 'ult': return ia < ib;
      case 'ule': return ia <= ib;
      case 'ugt': return ia > ib;
      case 'uge': return ia >= ib;
    }
    throw new Error(`unknown condition ${n.op}`);
  }

  evalFcmp(n, env) {
    const a = this.scalar(this.eval(n.a, env)), b = this.scalar(this.eval(n.b, env));
    let bit;
    if (Number.isNaN(a) || Number.isNaN(b)) bit = 8;
    else if (a < b) bit = 1;
    else if (a === b) bit = 2;
    else bit = 4;
    return (n.set & bit) !== 0;
  }

  // Values of calls: memoized per function run so a call executes once
  evalCall(n, env) {
    if (n.seq !== undefined && env.results.has(n.seq)) return env.results.get(n.seq);
    const v = this.performCall(n.fn, n.args || [], n.this, env, n.seq);
    if (n.seq !== undefined) env.results.set(n.seq, v);
    return v;
  }

  // Executes a call: accessors, derived helpers, node methods, math and device calls
  performCall(fn, argIdx, thisIdx, env, seq) {
    if (fn.startsWith('vcall@')) {
      const slot = parseInt(fn.slice(6), 16);
      if (slot === this.model.world_accessor_slot) return env.world;
      const target = this.eval(argIdx[0], env);
      const rest = argIdx.slice(1).map((a) => this.eval(a, env));
      return this.dispatch(slot, target, rest, env);
    }
    const dr = this.draw;
    if (fn.startsWith('sub_')) {
      const va = parseInt(fn.slice(4), 16);
      const self = () => this.object(thisIdx !== undefined ? this.eval(thisIdx, env) : undefined, fn);
      const args = argIdx.map((a) => this.eval(a, env));
      if (va === dr.kind_check) return this.isKindOf(self().type, this.int(args[0])) ? 1 : 0;
      if (va === dr.mesh_loaded) return this.readField(self(), this.accessor.offset, 4) ? 1 : 0;
      if (va === this.accessor.function) {
        const buffer = this.readField(self(), this.accessor.offset, 4);
        return buffer && buffer.obj ? this.dispatch(this.accessor.slot, buffer, [], env) : 0;
      }
      if (va === dr.material_lookup) {
        const name = args[0] && args[0].str !== undefined ? args[0].str : null;
        const m = name === null ? undefined : this.materials.get(name);
        return m ? { material: m } : 0;
      }
      if (this.functions.has(va)) {
        const thisVal = thisIdx !== undefined ? this.eval(thisIdx, env) : undefined;
        return this.run(va, thisVal && thisVal.obj ? thisVal.obj : null, args, env.state, env);
      }
      throw new Error(`helper ${fn} has no derived meaning`);
    }
    // Void device calls carry no value, and their arguments were applied as effects.
    if (fn.startsWith('D3DDevice_') && fn !== 'D3DDevice_CreateVertexShader') return { call: fn };
    const args = argIdx.map((a) => this.eval(a, env));
    switch (fn) {
      case 'wcscmp':
      case '_wcsicmp': {
        const a = this.string(args[0]), b = this.string(args[1]);
        const equal = fn === 'wcscmp' ? a === b : a.toLowerCase() === b.toLowerCase();
        return equal ? 0 : 1;
      }
      case 'D3DXMatrixMultiply': return matMul(this.matrix(args[1], env), this.matrix(args[2], env));
      case 'D3DXMatrixTranspose': return matTranspose(this.matrix(args[1], env));
      case 'D3DXMatrixInverse': return matInverse(this.matrix(args[2], env));
      case 'D3DXMatrixIdentity':
      case 'XGMatrixIdentity': return Float32Array.from(IDENTITY);
      case 'D3DXVec3TransformNormal': return vec3TransformNormal(this.vector(args[1]), this.matrix(args[2], env));
      case 'D3DXQuaternionNormalize': return vecNormalize(this.vector(args[1]));
      case 'D3DDevice_CreateVertexShader': return { program: this.int(args[1]), declaration: this.int(args[0]) };
    }
    throw new Error(`call to ${fn} has no runtime meaning`);
  }

  // Calls a virtual method on a scene object, a material object or a mesh buffer
  dispatch(slot, target, args, env) {
    if (target && target.obj !== undefined && target.obj.type === BUFFER_TYPE) {
      if (slot === this.accessor.slot) return target.obj.slots.get(this.draw.buffer_fields.format);
      if (slot === 0) return this.run(this.draw.buffer_draw, target.obj, args, env.state, env);
      throw new Error(`mesh buffer slot 0x${slot.toString(16)} has no derived meaning`);
    }
    if (target && target.obj !== undefined) {
      const va = this.typeSlot(target.obj.type, slot);
      return this.run(va, target.obj, args, env.state, env);
    }
    if (target && target.material !== undefined) {
      if (slot !== 0) throw new Error(`material objects expose only Apply at slot 0, not 0x${slot.toString(16)}`);
      const node = args[0] && args[0].obj ? args[0].obj : null;
      if (!node) throw new Error('material Apply called without its scene node');
      return this.applyMaterial(target.material, node, env);
    }
    throw new Error(`virtual call slot 0x${slot.toString(16)} on ${describe(target)}`);
  }

  object(v, what) {
    if (v && v.obj !== undefined) return v.obj;
    throw new Error(`${what} needs a scene object, got ${describe(v)}`);
  }

  string(v) {
    if (v && v.str !== undefined) return v.str;
    if (typeof v === 'number') {
      const s = this.draw.strings[String(v)];
      if (s !== undefined) return s;
    }
    throw new Error(`expected a string, got ${describe(v)}`);
  }

  // Resolves a matrix argument: a value, or a global the executable binds as view or projection
  matrix(v, env) {
    if (v instanceof Float32Array && v.length === 16) return v;
    if (typeof v === 'number') {
      const role = this.transforms[hex8(v)];
      if (role === 'view') return env.view;
      if (role === 'projection') return env.projection;
    }
    throw new Error(`matrix argument ${describe(v)} has no binding`);
  }

  vector(v) {
    if (v instanceof Float32Array) return v;
    throw new Error(`expected a vector, got ${describe(v)}`);
  }

  // Runs a summarized function or a runtime hook on an object with the shared device state
  run(va, thisObj, args, state, parent) {
    const hook = this.hooks.get(va);
    if (hook) return hook(thisObj, args, state, parent);
    const paths = this.functions.get(va);
    if (!paths) throw new Error(`function ${hex8(va)} is not summarized in the render model`);
    const env = this.newEnv(thisObj, args);
    env.state = state;
    env.world = parent.world; env.view = parent.view; env.projection = parent.projection; env.mesh = parent.mesh;
    this.runPaths(paths, env, state);
    return env.state.returned;
  }

  // Runs a material Apply method for a scene node holding the material
  applyMaterial(mat, node, parent) {
    const env = this.newEnv(null, [], mat, node);
    env.state = parent.state;
    env.world = parent.world; env.view = parent.view; env.projection = parent.projection; env.mesh = parent.mesh;
    this.runPaths(mat.apply, env, parent.state);
    return env.state.returned;
  }

  // Runs the first path whose guards hold and applies its effects to the state
  runPaths(paths, env, state) {
    env.state = state;
    for (const p of paths) {
      let ok = true;
      for (const g of p.guards) {
        if (!this.truthy(this.eval(g, env))) { ok = false; break; }
      }
      if (!ok) continue;
      for (const e of p.effects) this.applyEffect(e, env, state);
      state.returned = this.eval(p.return, env);
      return state;
    }
    throw new Error(`no execution path matches${env.mat ? ' for material ' + env.mat.name : ''}`);
  }

  applyEffect(e, env, state) {
    if (e.kind === 'store') {
      const value = this.eval(e.value, env);
      const base = e.base || '';
      if (base === '') {
        const off = e.off || 0;
        if (e.size === 8) this.globals.set(off, { f64: this.scalar(value) });
        else if (typeof value === 'object' && value.bits === undefined) this.globals.set(off, value);
        else this.globals.set(off, { bits: this.int(value) });
      } else if (base === 'this' || base === 'node') {
        const obj = base === 'this' ? env.this : env.node;
        if (!obj) throw new Error(`store to ${base} without an object`);
        this.writeField(obj, e.off || 0, e.size === 8 ? { f64: this.scalar(value) } : (typeof value === 'object' && value.bits === undefined ? value : this.int(value)));
      } else if (base === 'stack') {
        env.stack.set(e.off || 0, value);
      } else if (env.mat && base === env.mat.object) {
        this.storeMaterialField(env.mat, e.off || 0, e.size, typeof value === 'number' && e.size === 4 && Number.isInteger(value) ? value : (typeof value === 'number' ? { bits: bitsFromFloat(value) } : value));
      } else {
        throw new Error(`store to ${base} is not mapped`);
      }
      return;
    }
    const fn = e.fn;
    if (fn.startsWith('vcall@') || fn.startsWith('sub_') || fn === 'wcscmp' || fn === '_wcsicmp') {
      // Methods and helpers run once, their results being memoized by call sequence.
      if (!env.results.has(e.seq)) {
        const slotOrVa = fn.startsWith('vcall@') ? parseInt(fn.slice(6), 16) : parseInt(fn.slice(4), 16);
        if (fn.startsWith('vcall@') && slotOrVa === this.model.world_accessor_slot) return;
        if (fn.startsWith('sub_') && !this.isDerivedHelper(slotOrVa)) return;
        env.results.set(e.seq, this.performCall(fn, e.args || [], e.has_this ? e.this : undefined, env, e.seq));
      }
      return;
    }
    // Value producers were folded into the expressions that use their results.
    if (fn.startsWith('D3DX') || fn.startsWith('XG') || fn.startsWith('D3DDevice_Create')) return;
    if (fn.startsWith('D3DDevice_Draw')) {
      state.draws++;
      if (this.onDraw) this.onDraw(state, env);
      return;
    }
    const args = (e.args || []).map((a) => this.eval(a, env));
    if (fn === 'D3DDevice_SetTextureState_Deferred') {
      const stage = this.int(args[0]), st = this.int(args[1]), v = this.int(args[2]);
      const name = this.stageNames[String(st)];
      if (!name) throw new Error(`texture stage state ${st} has no name`);
      state.stages[stage] = state.stages[stage] || {};
      state.stages[stage][name] = v;
    } else if (fn === 'D3DDevice_SetTextureState_TexCoordIndex') {
      const stage = this.int(args[0]);
      state.stages[stage] = state.stages[stage] || {};
      state.stages[stage].TEXCOORDINDEX = this.int(args[1]);
    } else if (fn.startsWith('D3DDevice_SetRenderState_')) {
      const which = fn.slice('D3DDevice_SetRenderState_'.length);
      if (which === 'Simple') {
        const name = this.nvNames[String(this.int(args[0]) & 0x1FFF)];
        if (!name) throw new Error(`render state method 0x${this.int(args[0]).toString(16)} has no name`);
        state.rs[name] = this.int(args[1]);
      } else if (which === 'Deferred') {
        const name = this.stateNames[String(this.int(args[0]))];
        if (!name) throw new Error(`render state ${this.int(args[0])} has no name`);
        state.rs[name] = this.int(args[1]);
      } else {
        state.rs[which.toUpperCase()] = this.int(args[0]);
      }
    } else if (fn === 'D3DDevice_SetTexture') {
      const stage = this.int(args[0]);
      if (this.int(args[1]) === 0) delete state.textures[stage];
      else state.textures[stage] = args[1];
    } else if (fn === 'D3DDevice_SetVertexShader') {
      state.program = args[0];
    } else if (fn === 'D3DDevice_SetVertexShaderConstant') {
      const reg = this.int(args[0]), count = this.int(args[2]);
      const buf = e.buffers && e.buffers.find((b) => b.index === 1);
      if (!buf) throw new Error('vertex shader constant without a captured buffer');
      const elems = [];
      for (const x of buf.elems) {
        if (elems.length >= count * 4) break;
        elems.push(...this.floats(this.eval(x, env)));
      }
      if (elems.length < count * 4) throw new Error(`vertex shader constant ${reg} captured ${elems.length} of ${count * 4} floats`);
      for (let i = 0; i < count; i++) state.constants[reg + i] = Float32Array.from(elems.slice(i * 4, i * 4 + 4));
    } else if (fn === 'D3DDevice_SetTransform') {
      const which = this.int(args[0]);
      let m = args[1];
      if (!(m instanceof Float32Array)) {
        const buf = e.buffers && e.buffers.find((b) => b.index === 1);
        if (!buf) throw new Error(`transform ${which} has neither a matrix value nor a captured buffer`);
        const elems = buf.elems.flatMap((x) => this.floats(this.eval(x, env))).slice(0, 16);
        if (elems.length < 16) throw new Error(`transform ${which} captured ${elems.length} of 16 floats`);
        m = Float32Array.from(elems);
      }
      state.transforms[which] = m;
    } else if (fn === 'D3DDevice_GetTransform') {
      const which = this.int(args[0]);
      const m = state.transforms[which];
      if (!m) throw new Error(`transform ${which} read before it was set`);
      if (!args[1] || args[1].stack === undefined) throw new Error(`transform ${which} read into ${describe(args[1])}`);
      for (let i = 0; i < 16; i++) env.stack.set(args[1].stack + 4 * i, bitsFromFloat(m[i]));
    } else if (fn === 'D3DDevice_SetStreamSource') {
      state.stream = args[1];
    } else if (fn === 'D3DDevice_SetIndices') {
      state.indices = args[0];
    } else if (fn === 'D3DDevice_SetMaterial') {
      state.material = args[0];
    } else {
      throw new Error(`device call ${fn} is not mapped`);
    }
  }

  isDerivedHelper(va) {
    const dr = this.draw;
    return va === dr.kind_check || va === dr.mesh_loaded || va === this.accessor.function || va === dr.material_lookup || this.functions.has(va);
  }

  // Draws one shape node: the derived Shape draw method runs against the device state
  drawShape(shapeObj, state, matrices) {
    const env = this.newEnv(shapeObj);
    env.state = state;
    env.world = matrices.world; env.view = matrices.view; env.projection = matrices.projection;
    this.runPaths(this.functions.get(this.drawSlotFunction('Shape')), env, state);
  }

  // Applies the per node setup the transform draw loads before its children
  nodeSetup(state, matrices) {
    const env = this.newEnv(null);
    env.state = state;
    env.world = matrices.world; env.view = matrices.view; env.projection = matrices.projection;
    this.runPaths(this.model.node_constants, env, state);
  }

  show(idx) {
    return JSON.stringify(this.nodes[idx]);
  }
}

function describe(v) {
  if (v instanceof Float32Array) return `vec${v.length}`;
  if (v && v.obj !== undefined) return `${v.obj.type}#${v.obj.id}+${(v.off || 0).toString(16)}`;
  return JSON.stringify(v);
}

// Row major 4x4 products matching D3DXMatrixMultiply
export function matMul(a, b) {
  const out = new Float32Array(16);
  for (let r = 0; r < 4; r++) for (let c = 0; c < 4; c++) {
    let s = 0;
    for (let k = 0; k < 4; k++) s += a[r * 4 + k] * b[k * 4 + c];
    out[r * 4 + c] = s;
  }
  return out;
}

export function matTranspose(a) {
  const out = new Float32Array(16);
  for (let r = 0; r < 4; r++) for (let c = 0; c < 4; c++) out[c * 4 + r] = a[r * 4 + c];
  return out;
}

export function matInverse(m) {
  const inv = new Float64Array(16);
  inv[0] = m[5] * m[10] * m[15] - m[5] * m[11] * m[14] - m[9] * m[6] * m[15] + m[9] * m[7] * m[14] + m[13] * m[6] * m[11] - m[13] * m[7] * m[10];
  inv[4] = -m[4] * m[10] * m[15] + m[4] * m[11] * m[14] + m[8] * m[6] * m[15] - m[8] * m[7] * m[14] - m[12] * m[6] * m[11] + m[12] * m[7] * m[10];
  inv[8] = m[4] * m[9] * m[15] - m[4] * m[11] * m[13] - m[8] * m[5] * m[15] + m[8] * m[7] * m[13] + m[12] * m[5] * m[11] - m[12] * m[7] * m[9];
  inv[12] = -m[4] * m[9] * m[14] + m[4] * m[10] * m[13] + m[8] * m[5] * m[14] - m[8] * m[6] * m[13] - m[12] * m[5] * m[10] + m[12] * m[6] * m[9];
  inv[1] = -m[1] * m[10] * m[15] + m[1] * m[11] * m[14] + m[9] * m[2] * m[15] - m[9] * m[3] * m[14] - m[13] * m[2] * m[11] + m[13] * m[3] * m[10];
  inv[5] = m[0] * m[10] * m[15] - m[0] * m[11] * m[14] - m[8] * m[2] * m[15] + m[8] * m[3] * m[14] + m[12] * m[2] * m[11] - m[12] * m[3] * m[10];
  inv[9] = -m[0] * m[9] * m[15] + m[0] * m[11] * m[13] + m[8] * m[1] * m[15] - m[8] * m[3] * m[13] - m[12] * m[1] * m[11] + m[12] * m[3] * m[9];
  inv[13] = m[0] * m[9] * m[14] - m[0] * m[10] * m[13] - m[8] * m[1] * m[14] + m[8] * m[2] * m[13] + m[12] * m[1] * m[10] - m[12] * m[2] * m[9];
  inv[2] = m[1] * m[6] * m[15] - m[1] * m[7] * m[14] - m[5] * m[2] * m[15] + m[5] * m[3] * m[14] + m[13] * m[2] * m[7] - m[13] * m[3] * m[6];
  inv[6] = -m[0] * m[6] * m[15] + m[0] * m[7] * m[14] + m[4] * m[2] * m[15] - m[4] * m[3] * m[14] - m[12] * m[2] * m[7] + m[12] * m[3] * m[6];
  inv[10] = m[0] * m[5] * m[15] - m[0] * m[7] * m[13] - m[4] * m[1] * m[15] + m[4] * m[3] * m[13] + m[12] * m[1] * m[7] - m[12] * m[3] * m[5];
  inv[14] = -m[0] * m[5] * m[14] + m[0] * m[6] * m[13] + m[4] * m[1] * m[14] - m[4] * m[2] * m[13] - m[12] * m[1] * m[6] + m[12] * m[2] * m[5];
  inv[3] = -m[1] * m[6] * m[11] + m[1] * m[7] * m[10] + m[5] * m[2] * m[11] - m[5] * m[3] * m[10] - m[9] * m[2] * m[7] + m[9] * m[3] * m[6];
  inv[7] = m[0] * m[6] * m[11] - m[0] * m[7] * m[10] - m[4] * m[2] * m[11] + m[4] * m[3] * m[10] + m[8] * m[2] * m[7] - m[8] * m[3] * m[6];
  inv[11] = -m[0] * m[5] * m[11] + m[0] * m[7] * m[9] + m[4] * m[1] * m[11] - m[4] * m[3] * m[9] - m[8] * m[1] * m[7] + m[8] * m[3] * m[5];
  inv[15] = m[0] * m[5] * m[10] - m[0] * m[6] * m[9] - m[4] * m[1] * m[10] + m[4] * m[2] * m[9] + m[8] * m[1] * m[6] - m[8] * m[2] * m[5];
  const det = m[0] * inv[0] + m[1] * inv[4] + m[2] * inv[8] + m[3] * inv[12];
  const out = new Float32Array(16);
  for (let i = 0; i < 16; i++) out[i] = inv[i] / det;
  return out;
}

// Transforms a direction by the upper 3x3 of a row major matrix as a row vector
export function vec3TransformNormal(v, m) {
  const out = new Float32Array(4);
  for (let c = 0; c < 3; c++) out[c] = v[0] * m[c] + v[1] * m[4 + c] + v[2] * m[8 + c];
  out[3] = v.length > 3 ? v[3] : 0;
  return out;
}

export function vecNormalize(v) {
  let s = 0;
  for (let i = 0; i < 4 && i < v.length; i++) s += v[i] * v[i];
  const inv = s > 0 ? 1 / Math.sqrt(s) : 0;
  const out = new Float32Array(4);
  for (let i = 0; i < 4; i++) out[i] = (v[i] || 0) * inv;
  return out;
}
