
const FLOAT_BIN_OPS = new Set(['fadd', 'fsub', 'fmul', 'fdiv', 'fmin', 'fmax', 'fmod', 'fpow', 'fatan2']);

// Interpreter for the execution paths derived from the dashboard executable
// Runs the frame loop, the node draw and tick methods, the material Apply methods,
// the script natives and the device setup summarized in render.json against scene
// objects and the executable's globals, producing D3D device state and draw calls.

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

export const IDENTITY = Float32Array.from([1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1]);

// Scene file field types as the executable's field tables number them
const FIELD_INT = 1, FIELD_BOOL = 2, FIELD_NUMBER = 3, FIELD_VEC3 = 5, FIELD_ROTATION = 6, FIELD_COLOR = 8, FIELD_NODE = 9, FIELD_STRING = 10;

// ID3DXMatrixStack methods by vtable offset, from the XDK D3DX interface
const STACK_POP = 0xC, STACK_PUSH = 0x10, STACK_IDENTITY = 0x14, STACK_LOAD = 0x18, STACK_MULT = 0x1C, STACK_MULT_LOCAL = 0x20, STACK_TOP = 0x44;

// Xbox D3D transform states
const TS_VIEW = 0, TS_PROJECTION = 1, TS_WORLD = 6;

// D3DSURFACE_DESC words the init reads back from the back buffer, in the Xbox layout
const SURFACE_DESC_WIDTH = 0x14, SURFACE_DESC_HEIGHT = 0x18;

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
// The loader's archive record, whose fields beyond the ones a finished load sets read as zero
export const ARCHIVE_TYPE = '$archive';
// Memory the executable allocates while running, read and written by field offset
export const HEAP_TYPE = '$heap';
// The descriptor and value a scripted field write hands to the class field hook
const FIELD_TYPE = '$field', VALUE_TYPE = '$value';

// The name a folded counted array index carries, the accessor call the plain build makes
export const ARRAY_ELEMENT = 'array_element';

// D3D primitive type of the triangle lists the mesh exporter writes
const PT_TRIANGLELIST = 4;

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
  obj.slots.set(f.primitive, PT_TRIANGLELIST);
  return obj;
}

export function newState() {
  return { rs: {}, stages: {}, textures: {}, program: undefined, constants: {}, transforms: {}, stream: undefined, indices: undefined, draws: 0, returned: 0, material: undefined, clear: null };
}

export function cloneState(s) {
  const stages = {};
  for (const [k, v] of Object.entries(s.stages)) stages[k] = { ...v };
  return { rs: { ...s.rs }, stages, textures: { ...s.textures }, program: s.program, constants: { ...s.constants }, transforms: { ...s.transforms }, stream: s.stream, indices: s.indices, draws: s.draws, returned: s.returned, material: s.material, clear: s.clear };
}

// The D3DX matrix stack the executable composes world matrices on
export class MatrixStack {
  constructor() {
    this.frames = [Float32Array.from(IDENTITY)];
  }
  get top() { return this.frames[this.frames.length - 1]; }
  push() { this.frames.push(Float32Array.from(this.top)); }
  pop() { if (this.frames.length > 1) this.frames.pop(); }
  identity() { this.frames[this.frames.length - 1] = Float32Array.from(IDENTITY); }
  load(m) { this.frames[this.frames.length - 1] = Float32Array.from(m); }
  mult(m) { this.frames[this.frames.length - 1] = matMul(this.top, m); }
  multLocal(m) { this.frames[this.frames.length - 1] = matMul(m, this.top); }
}

// A vertex buffer the executable creates and fills through a lock
export class VertexBuffer {
  constructor(size, fvf) {
    this.bytes = new Uint8Array(size);
    this.view = new DataView(this.bytes.buffer);
    this.fvf = fvf;
    this.version = 0;
  }
}

// Values the running path overwrote, so loads later in the same path still read what the path began with
class EntryValues {
  constructor() {
    this.mem = new Map();
    this.objs = new Map();
    this.stacks = new Map();
    this.buffers = new Map();
    this.mats = new Map();
  }
}

const UNSET = Symbol('unset');

// The first word of a block stored at a slot read as a single word
function wordOf(v) {
  if (v instanceof Float32Array) return bitsFromFloat(v[0]);
  if (v && v.f64 !== undefined) { bitsBuf.setFloat64(0, v.f64, true); return bitsBuf.getUint32(0, true); }
  return v;
}

// Copies a block so a remembered value survives in place writes to the live one
function keep(v) {
  return v instanceof Float32Array ? v.slice() : v;
}

// The executable's writable globals as the derived paths read and write them, image bytes underneath
class GlobalMemory {
  constructor(model) {
    this.image = new Map();
    for (const [off, g] of Object.entries(model.globals)) {
      const base = Number(off);
      for (let i = 0; i < g.init.length; i++) this.image.set(base + i, g.init[i]);
    }
    this.words = new Map();
  }

  // Finds the stored word covering an address
  covering(addr) {
    for (let delta = 0; delta < 64; delta++) {
      const w = this.words.get(addr - delta);
      if (w && addr < addr - delta + w.size) return { off: addr - delta, w };
    }
    return null;
  }

  byteAt(addr) {
    const c = this.covering(addr);
    if (c) return wordByte(c.w, addr - c.off);
    const b = this.image.get(addr);
    if (b === undefined) throw new Error(`global 0x${addr.toString(16)} has no derived start value`);
    return b;
  }

  load(off, size) {
    const exact = this.words.get(off);
    if (exact && exact.size === size) return exact.value;
    const bytes = new Uint8Array(size);
    for (let i = 0; i < size; i++) bytes[i] = this.byteAt(off + i);
    return valueFromBytes(bytes, size);
  }

  store(off, size, value) {
    const c = this.covering(off);
    if (c && c.w.value instanceof Float32Array && size === 4 && (off - c.off) % 4 === 0 && off + 4 <= c.off + c.w.size) {
      c.w.value[(off - c.off) / 4] = floatFromBits(valueBits(value));
      return;
    }
    for (let delta = -63; delta < size; delta++) {
      const k = off + delta;
      const w = this.words.get(k);
      if (!w || k + w.size <= off || (k === off && w.size === size)) continue;
      this.words.delete(k);
      if (k < off || k + w.size > off + size) {
        for (let b = 0; b < w.size; b++) {
          const bo = k + b;
          if (bo >= off && bo < off + size) continue;
          this.words.set(bo, { size: 1, value: wordByte(w, b) });
        }
      }
    }
    this.words.set(off, { size, value });
  }
}

// One byte of a stored word
function wordByte(w, i) {
  const v = w.value;
  if (v instanceof Float32Array) {
    bitsBuf.setFloat32(0, v[(i / 4) | 0], true);
    return bitsBuf.getUint8(i % 4);
  }
  if (typeof v === 'object' && v !== null && v.f64 !== undefined) {
    bitsBuf.setFloat64(0, v.f64, true);
    return bitsBuf.getUint8(i);
  }
  if (typeof v === 'object' && v !== null && v.bits === undefined) throw new Error(`byte ${i} of a ${describe(v)} global`);
  bitsBuf.setUint32(0, valueBits(v), true);
  return i < 4 ? bitsBuf.getUint8(i) : 0;
}

// Builds a value from little endian bytes: an integer, a double or a float block
function valueFromBytes(bytes, size) {
  if (size <= 4) {
    let v = 0;
    for (let i = size - 1; i >= 0; i--) v = v * 256 + bytes[i];
    return u32(v);
  }
  if (size === 8) {
    for (let i = 0; i < 8; i++) bitsBuf.setUint8(i, bytes[i]);
    return { f64: bitsBuf.getFloat64(0, true) };
  }
  const out = new Float32Array(size / 4);
  for (let i = 0; i < out.length; i++) {
    for (let b = 0; b < 4; b++) bitsBuf.setUint8(b, bytes[4 * i + b]);
    out[i] = bitsBuf.getFloat32(0, true);
  }
  return out;
}

function valueBits(v) {
  if (typeof v === 'number') return u32(v);
  if (v && v.bits !== undefined) return u32(v.bits);
  if (v && v.f64 !== undefined) return bitsFromFloat(v.f64);
  throw new Error(`expected a word, got ${describe(v)}`);
}

export class DerivedModel {
  constructor(model, host) {
    this.model = model;
    this.host = host;
    this.nodes = model.exprs.nodes;
    this.materials = new Map();
    for (const m of model.materials) {
      this.materials.set(m.name, m);
      m._fields = new Map();
      for (const [off, b] of Object.entries(m.fields)) m._fields.set(Number(off), b);
    }
    this.memory = new GlobalMemory(model);
    this.hierarchyAlpha = 1;
    this.stateNames = model.render_state_names;
    this.stageNames = model.texture_stage_names;
    this.nvNames = model.nv_state_names;
    this.accessor = model.mesh_accessor;
    this.types = model.node_types;
    this.draw = model.draw;
    this.scene = model.scene;
    this.descToType = new Map();
    for (const t of Object.values(this.types)) this.descToType.set(t.desc, t.name);
    this.functions = new Map();
    for (const [va, paths] of Object.entries(this.draw.functions)) this.functions.set(Number(va), paths);
    this.hooks = new Map();
    this.onDraw = null;
    this.frameStart = performance.now();
    this.defaults = this.runPaths(model.render_state_defaults, this.newEnv(null), newState());
  }

  // Registers a runtime implementation for a routine the model keeps as a call
  hook(va, fn) {
    this.hooks.set(va, fn);
  }

  // Makes the executable's object for a scene node of a registered class
  newObject(typeName, node) {
    const t = this.types[typeName];
    if (!t) throw new Error(`node type ${typeName} is not registered by the executable`);
    const obj = new SceneObject(typeName, node);
    this.construct(t, obj, node);
    return obj;
  }

  // Runs the class constructor on a new object, the scene file's own field values then standing over its defaults
  construct(t, obj, node) {
    if (!t.construct || !t.construct.length) return;
    const env = this.newEnv(obj, [0, 0, 0]);
    env.state = this.host.device;
    this.runPaths(t.construct, env, this.host.device);
    if (!node || !node._fields) return;
    for (let c = t; c; c = this.types[c.parent]) {
      for (const [off, f] of Object.entries(c.fields)) {
        if (node._fields[f.name] === undefined) continue;
        for (let o = Number(off); o < Number(off) + fieldSize(f.type); o++) obj.slots.delete(o);
      }
      if (!c.parent) break;
    }
  }

  drawSlotFunction(typeName) {
    return this.typeSlot(typeName, this.draw.slot);
  }

  tickSlotFunction(typeName) {
    return this.typeSlot(typeName, this.scene.tick_slot);
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

  // Finds the field whose storage spans an object offset
  fieldSpanning(typeName, off) {
    for (let t = this.types[typeName]; t; t = this.types[t.parent]) {
      for (const [start, f] of Object.entries(t.fields)) {
        const s = Number(start);
        if (off > s && off < s + fieldSize(f.type)) return { off: s, name: f.name, type: f.type };
      }
      if (!t.parent) break;
    }
    return null;
  }

  ctorInit(typeName, off) {
    const t = this.types[typeName];
    if (t && t.init_error && !t.init[String(off)]) throw new Error(`${typeName} constructor defaults were not derived: ${t.init_error}`);
    return t ? t.init[String(off)] : undefined;
  }

  // Finds a scene file field of a class by name, inheriting down the class chain
  fieldByName(typeName, name) {
    for (let t = this.types[typeName]; t; t = this.types[t.parent]) {
      for (const [off, f] of Object.entries(t.fields)) if (f.name === name) return { off: Number(off), name, type: f.type };
      if (!t.parent) break;
    }
    return null;
  }

  // Reads the value a class constructor gives a scene field the scene file omits
  defaultField(typeName, name) {
    const f = this.fieldByName(typeName, name);
    if (!f) throw new Error(`node type ${typeName} has no field ${name}`);
    const init = this.ctorInit(typeName, f.off);
    if (!init) throw new Error(`${typeName} constructor stores no default for ${name}`);
    if (f.type === FIELD_NUMBER) return init.size === 8 ? (init.double || 0) : floatFromBits(init.value);
    return u32(init.value);
  }

  // Reports whether the hook a field write calls on the class is summarized
  hasFieldHook(typeName) {
    return this.functions.has(this.typeSlot(typeName, this.draw.field_set_slot));
  }

  // Runs the class hook a field write calls, returning whether the generic store follows
  setField(obj, field, value, state) {
    const va = this.typeSlot(obj.type, this.draw.field_set_slot);
    const desc = new SceneObject(FIELD_TYPE, null);
    desc.slots.set(0, u32(field.off));
    desc.slots.set(4, u32(field.type));
    const val = new SceneObject(VALUE_TYPE, null);
    val.read = (off, size) => {
      if (field.type === FIELD_STRING) { if (off !== 0) throw new Error(`string field value word +0x${off.toString(16)} read by the hook`); return { str: String(value) }; }
      if (field.type === FIELD_NUMBER) { if (off !== 0) throw new Error(`number field value word +0x${off.toString(16)} read by the hook`); return size === 8 ? { f64: Number(value) } : bitsFromFloat(Number(value)); }
      if (field.type === FIELD_BOOL) return value ? 1 : 0;
      if (field.type === FIELD_INT) return u32(Number(value));
      if (field.type === FIELD_VEC3 || field.type === FIELD_ROTATION || field.type === FIELD_COLOR) {
        const comps = fieldSize(field.type) / 4;
        if (off % 4 !== 0 || off / 4 >= comps) throw new Error(`field ${field.name} value word +0x${off.toString(16)} is outside its ${comps} components`);
        if (size === 4) return bitsFromFloat(Number(value[off / 4]));
        return Float32Array.from(Array.from({ length: size / 4 }, (_, i) => Number(value[off / 4 + i] || 0)));
      }
      throw new Error(`field ${field.name} has scene type ${field.type} which the hook cannot be handed`);
    };
    const parent = this.newEnv(null);
    parent.state = state;
    return (this.int(this.run(va, obj, [{ obj: desc, off: 0 }, { obj: val, off: 0 }], state, parent)) & 0xFF) !== 0;
  }

  // Applies a render state write by state index the way the deferred wrapper does
  setRenderState(state, index, value) {
    const name = this.stateNames[String(index)];
    if (!name) throw new Error(`render state ${index} has no name`);
    state.rs[name] = u32(value);
  }

  newEnv(thisObj, args = [], mat = null, node = null) {
    return { this: thisObj, args, mat, node, stack: new Map(), results: new Map(), mesh: null, entry: new EntryValues() };
  }

  // The object this points at in a run: a scene object, a static address or a stack buffer, offset
  thisPointer(env, off) {
    const t = env.this;
    if (t === null || t === undefined) throw new Error('this used without an object');
    if (typeof t === 'number') return u32(t + off);
    if (t instanceof SceneObject) return { obj: t, off };
    if (t.obj !== undefined) return { obj: t.obj, off: (t.off || 0) + off };
    if (t.stack !== undefined) return { stack: t.stack + off, frame: t.frame };
    if (t.res !== undefined) return { res: t.res, off: (t.off || 0) + off };
    throw new Error(`this is ${describe(t)}`);
  }

  // Reads a word or block at an offset from this
  loadThis(env, off, size) {
    return this.loadAt(env, this.thisPointer(env, off), size);
  }

  // Reads through a resolved address as the running path saw it on entry
  loadAt(env, p, size) {
    if (typeof p === 'number') return this.entryGlobal(env, p, size);
    if (p.obj !== undefined) return this.entryField(env, p.obj, p.off || 0, size);
    if (p.stack !== undefined) return this.entryStack(env, p.frame || env, p.stack, size);
    return this.entryBuffer(env, p.res, p.off || 0, size);
  }

  // Remembers what a resolved address holds before the running path overwrites it
  noteAt(env, p, size) {
    if (typeof p === 'number') this.noteGlobal(env, p, size);
    else if (p && p.obj !== undefined) this.noteField(env, p.obj, p.off || 0, size);
    else if (p && p.stack !== undefined) this.noteStack(env, p.frame || env, p.stack);
    else if (p && p.res !== undefined) this.noteBuffer(env, p.res, p.off || 0, size);
  }

  noteGlobal(env, addr, size) {
    if (env.entry.mem.has(addr)) return;
    let value;
    try { value = keep(this.memory.load(addr, size)); } catch (_) { value = UNSET; }
    env.entry.mem.set(addr, { size, value });
  }

  noteField(env, obj, off, size) {
    if (typeof obj === 'number') { this.noteGlobal(env, obj + off, size); return; }
    let slots = env.entry.objs.get(obj);
    if (!slots) { slots = new Map(); env.entry.objs.set(obj, slots); }
    if (slots.has(off)) return;
    let value;
    try { value = keep(this.readField(obj, off, size)); } catch (_) { value = UNSET; }
    slots.set(off, { size, value });
  }

  noteStack(env, frame, off) {
    let slots = env.entry.stacks.get(frame);
    if (!slots) { slots = new Map(); env.entry.stacks.set(frame, slots); }
    if (!slots.has(off)) slots.set(off, frame.stack.has(off) ? keep(frame.stack.get(off)) : UNSET);
  }

  noteBuffer(env, seq, off, size) {
    const r = env.results.get(seq);
    if (r && r.obj !== undefined) { this.noteField(env, r.obj, (r.off || 0) + off, size); return; }
    let slots = env.entry.buffers.get(seq);
    if (!slots) { slots = new Map(); env.entry.buffers.set(seq, slots); }
    if (slots.has(off)) return;
    let value;
    try { value = keep(this.bufferLoad(env, seq, off, size)); } catch (_) { value = UNSET; }
    slots.set(off, { size, value });
  }

  noteMaterial(env, mat, off, size) {
    if (env.entry.mats.has(off)) return;
    let value;
    try { value = this.fieldInt(mat, off, size); } catch (_) { value = UNSET; }
    env.entry.mats.set(off, { size, value });
  }

  // A remembered word or block covering a range, or the bytes it shares with remembered ones
  entryOverlap(slots, off, size, liveByte) {
    let overlaps = false;
    for (const [o, s] of slots) {
      if (o < off + size && off < o + s.size) overlaps = true;
    }
    if (!overlaps) return undefined;
    const bytes = new Uint8Array(size);
    for (let i = 0; i < size; i++) {
      const a = off + i;
      let b;
      for (const [o, s] of slots) {
        if (a >= o && a < o + s.size && s.value !== UNSET) { b = wordByte({ value: s.value }, a - o); break; }
      }
      bytes[i] = b === undefined ? liveByte(a) : b;
    }
    return valueFromBytes(bytes, size);
  }

  entryGlobal(env, addr, size) {
    const saved = env.entry.mem.get(addr);
    if (saved && saved.size === size && saved.value !== UNSET) return keep(saved.value);
    if (env.entry.mem.size) {
      const v = this.entryOverlap(env.entry.mem, addr, size, (a) => this.memory.byteAt(a));
      if (v !== undefined) return v;
    }
    return this.memory.load(addr, size);
  }

  entryField(env, obj, off, size) {
    if (typeof obj === 'number') return this.entryGlobal(env, obj + off, size);
    const slots = env.entry.objs.get(obj);
    if (!slots) return this.readField(obj, off, size);
    const saved = slots.get(off);
    if (saved && saved.size === size && saved.value !== UNSET) return keep(saved.value);
    const v = this.entryOverlap(slots, off, size, (a) => this.fieldByte(obj, a));
    return v === undefined ? this.readField(obj, off, size) : v;
  }

  // One live byte of an object field through the word or block holding it
  fieldByte(obj, addr) {
    for (let delta = 0; delta < 64; delta++) {
      const off = addr - delta;
      if (!obj.slots.has(off)) continue;
      const v = obj.slots.get(off);
      const size = v instanceof Float32Array ? v.length * 4 : (v && v.f64 !== undefined ? 8 : 4);
      if (addr < off + size) return wordByte({ value: v }, delta);
    }
    const base = addr & ~3;
    return wordByte({ value: this.readField(obj, base, 4) }, addr - base);
  }

  entryStack(env, frame, off, size) {
    const slots = env.entry.stacks.get(frame);
    if (!slots) return this.stackLoad(frame, off, size);
    if (size <= 4) {
      const saved = slots.get(off);
      return saved !== undefined && saved !== UNSET ? wordOf(saved) : this.stackLoad(frame, off, size);
    }
    const saved = slots.get(off);
    if (saved instanceof Float32Array && saved.length * 4 >= size) return saved.length * 4 === size ? saved.slice() : saved.slice(0, size / 4);
    if (saved && saved.f64 !== undefined && size === 8) return saved;
    const out = new Float32Array(size / 4);
    for (let i = 0; i < out.length; i++) {
      const w = slots.has(off + 4 * i) && slots.get(off + 4 * i) !== UNSET ? slots.get(off + 4 * i) : this.stackLoad(frame, off + 4 * i, 4);
      out[i] = typeof w === 'number' ? floatFromBits(w) : (w instanceof Float32Array ? w[0] : this.scalar(w));
    }
    return out;
  }

  entryBuffer(env, seq, off, size) {
    const r = env.results.get(seq);
    if (r && r.obj !== undefined) return this.entryField(env, r.obj, (r.off || 0) + off, size);
    const slots = env.entry.buffers.get(seq);
    const saved = slots && slots.get(off);
    if (saved && saved.size === size && saved.value !== UNSET) return keep(saved.value);
    return this.bufferLoad(env, seq, off, size);
  }

  entryMaterial(env, mat, off, size) {
    const saved = env.entry.mats.get(off);
    if (saved && saved.size === size && saved.value !== UNSET) return saved.value;
    return this.fieldInt(mat, off, size);
  }

  // Reads an object field: a runtime slot, a scene file field or the constructor default
  readField(obj, off, size) {
    if (typeof obj === 'number') return this.memory.load(obj + off, size);
    if (!obj || !obj.slots) throw new Error(`field +0x${off.toString(16)} read on ${describe(obj)}`);
    if (obj.type === VALUE_TYPE) return obj.read(off, size);
    if (size > 8) return this.readBlock(obj, off, size);
    if (obj.slots.has(off)) {
      const v = obj.slots.get(off);
      if (v instanceof Float32Array && size <= 4) return bitsFromFloat(v[0]);
      return v;
    }
    const inner = this.slotSpanning(obj, off);
    if (inner) {
      const v = inner.value;
      if (v instanceof Float32Array && size === 4 && (off - inner.off) % 4 === 0) return bitsFromFloat(v[(off - inner.off) / 4]);
      if (v && v.f64 !== undefined && size === 4) { bitsBuf.setFloat64(0, v.f64, true); return bitsBuf.getUint32(off - inner.off, true); }
      throw new Error(`${obj.type} field +0x${off.toString(16)} read inside a ${describe(v)} slot`);
    }
    if (obj.type === BUFFER_TYPE) throw new Error(`mesh buffer field +0x${off.toString(16)} is not one its draw reveals`);
    if (obj.type === ARCHIVE_TYPE) return size === 8 ? { f64: 0 } : 0;
    if (obj.type === FIELD_TYPE) throw new Error(`field descriptor word +0x${off.toString(16)} is not one the hook may read`);
    const f = this.fieldAt(obj.type, off);
    const v = f ? obj.node.derivedField(f.name, f.type) : undefined;
    if (v !== undefined) {
      switch (f.type) {
        case FIELD_NODE: return v ? v : 0;
        case FIELD_STRING: return { str: String(v) };
        case FIELD_BOOL: return v ? 1 : 0;
        case FIELD_INT: return u32(v);
        case FIELD_NUMBER: return size === 8 ? { f64: Number(v) } : bitsFromFloat(Number(v));
        case FIELD_VEC3: case FIELD_ROTATION: case FIELD_COLOR:
          if (size === 4) return bitsFromFloat(Number(v[0]));
          return Float32Array.from(Array.from({ length: size / 4 }, (_, i) => Number(v[i] || 0)));
      }
      throw new Error(`field ${f.name} of ${obj.type} has scene type ${f.type} which the interpreter does not read`);
    }
    const span = this.fieldSpanning(obj.type, off);
    if (span) {
      const sv = obj.node.derivedField(span.name, span.type);
      if (sv !== undefined && size === 4) return bitsFromFloat(Number(sv[(off - span.off) / 4]));
    }
    const init = this.ctorInit(obj.type, off);
    if (init) return init.size === 8 ? { f64: init.double || 0 } : u32(init.value);
    if (size === 4 && this.ctorInitSpanning(obj.type, off) !== undefined) return this.ctorInitSpanning(obj.type, off);
    // Object memory the constructor never writes starts zeroed by the allocator.
    if (this.types[obj.type] && off < this.types[obj.type].size) return size === 8 ? { f64: 0 } : 0;
    throw new Error(`${obj.type} object field +0x${off.toString(16)} is outside the object`);
  }

  // Reads a vector or matrix at an object offset: a block slot, a vector scene field or the words making it up
  readBlock(obj, off, size) {
    const slot = obj.slots.get(off);
    if (slot instanceof Float32Array && slot.length * 4 >= size) return slot.length * 4 === size ? slot : slot.slice(0, size / 4);
    if (slot === undefined) {
      const f = this.fieldAt(obj.type, off);
      if (f && (f.type === FIELD_VEC3 || f.type === FIELD_ROTATION || f.type === FIELD_COLOR) && fieldSize(f.type) >= size) {
        const v = obj.node.derivedField(f.name, f.type);
        if (v !== undefined) return Float32Array.from(Array.from({ length: size / 4 }, (_, i) => Number(v[i] || 0)));
      }
    }
    return this.readWords(obj, off, size);
  }

  // A word inside a double the constructor stores
  ctorInitSpanning(typeName, off) {
    const t = this.types[typeName];
    if (!t) return undefined;
    const init = t.init[String(off - 4)];
    if (init && init.size === 8) { bitsBuf.setFloat64(0, init.double || 0, true); return bitsBuf.getUint32(4, true); }
    return undefined;
  }

  // Finds a runtime slot whose value spans an object offset
  slotSpanning(obj, off) {
    for (let delta = 4; delta < 64; delta += 4) {
      const v = obj.slots.get(off - delta);
      if (v === undefined) continue;
      const size = v instanceof Float32Array ? v.length * 4 : (v && v.f64 !== undefined ? 8 : 4);
      if (off < off - delta + size) return { off: off - delta, value: v };
    }
    return null;
  }

  writeField(obj, off, value) {
    if (typeof obj === 'number') { this.memory.store(obj + off, value instanceof Float32Array ? value.length * 4 : (value && value.f64 !== undefined ? 8 : 4), value); return; }
    const inner = this.slotSpanning(obj, off);
    if (inner && inner.value instanceof Float32Array && (off - inner.off) % 4 === 0 && !(value instanceof Float32Array)) {
      inner.value[(off - inner.off) / 4] = floatFromBits(valueBits(value));
      return;
    }
    obj.slots.set(off, value);
  }

  // Evaluates a pooled expression node
  eval(idx, env) {
    const n = this.nodes[idx];
    switch (n.k) {
      case 'i': return u32(n.v);
      case 'f': return n.v !== undefined ? n.v : (n.s === 'NaN' ? NaN : (n.s === '+Inf' ? Infinity : -Infinity));
      case 'ptr':
        if (n.base === 'node') return env.node ? { obj: env.node, off: n.off } : 0;
        if (n.base === 'this') return this.thisPointer(env, n.off);
        if (n.base === '') return u32(n.off);
        if (n.base === 'stack') return { stack: n.off, frame: env };
        if (n.base.startsWith('res')) {
          const r = env.results.get(Number(n.base.slice(3)));
          if (r && r.obj !== undefined) return { obj: r.obj, off: (r.off || 0) + n.off };
          return { res: Number(n.base.slice(3)), off: n.off };
        }
        if (n.base.startsWith('g')) return this.globalPointer(env, n.base, n.off);
        return { ptr: n.base, off: n.off };
      case 'sym':
        if (n.name.startsWith('texture:')) return { texture: n.name };
        if (n.name.startsWith('arg')) return env.args[Number(n.name.slice(3))] ?? 0;
        if (n.name.startsWith('ctor.arg')) return env.args[Number(n.name.slice(8))] ?? 0;
        if (n.name.startsWith('reg.') || n.name.startsWith('clobber.') || n.name.startsWith('fpu.') || n.name.startsWith('xmm') || n.name.startsWith('mm') || n.name === 'ftol.hi' || n.name === 'fsw') return 0;
        throw new Error(`unbound symbol ${n.name}`);
      case 'load': return this.evalLoad(n, env);
      case 'bin': return this.evalBin(n, env);
      case 'un': return this.evalUn(n, env);
      case 'cond': return this.evalCond(n, env) ? 1 : 0;
      case 'fcmp': return this.evalFcmp(n, env) ? 1 : 0;
      case 'not': return this.truthy(this.eval(n.a, env)) ? 0 : 1;
      case 'call': return this.evalCall(n, env);
      case 'block': return Float32Array.from(n.elems.flatMap((e) => this.floats(this.eval(e, env))));
      case 'elem': return this.vector(this.eval(n.a, env), 64, env)[n.i];
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

  // A float argument an XDK call received: a typed float, a double or the bits of a float
  floatArg(idx, env) {
    const v = this.eval(idx, env);
    if (typeof v === 'number') return this.isFloatExpr(idx) ? v : floatFromBits(v);
    return this.scalar(v);
  }

  truthy(v) {
    return typeof v === 'number' ? v !== 0 : !!v;
  }

  // Integer view of a value: object handles compare by identity, void results carry nothing
  int(v) {
    if (typeof v === 'number') return u32(v);
    if (v && v.bits !== undefined) return v.bits;
    if (v && v.f64 !== undefined) return u32(Math.trunc(v.f64));
    if (v && v.obj !== undefined) return u32(v.obj.id * 4096 + (v.off || 0));
    if (v && v.vb !== undefined) return u32(0x40000000 + v.vb.id);
    if (v && (v.ptr !== undefined || v.texture !== undefined || v.program !== undefined || v.str !== undefined || v.material !== undefined || v.vertices !== undefined || v.indices !== undefined || v.xapTexture !== undefined || v.stack !== undefined || v.matrixStack !== undefined || v.buffer !== undefined || v.res !== undefined || v.device !== undefined)) return 1;
    if (v && v.call !== undefined) return 0;
    if (v instanceof Float32Array) return 1;
    throw new Error(`expected an integer, got ${describe(v)}`);
  }

  evalLoad(n, env) {
    n = n.off === undefined ? { ...n, off: 0 } : n;
    if (n.addr !== undefined) {
      const addr = this.eval(n.addr, env);
      if (addr && addr.obj !== undefined) return this.entryField(env, addr.obj, addr.off, n.size);
      if (addr && addr.matrixStack !== undefined) return addr;
      if (addr && addr.vb !== undefined) return addr;
      if (addr && addr.device !== undefined) return addr;
      if (typeof addr === 'number') return this.entryGlobal(env, addr, n.size);
      if (addr && addr.stack !== undefined) return this.entryStack(env, addr.frame || env, addr.stack, n.size);
      if (addr && addr.res !== undefined) return this.entryBuffer(env, addr.res, addr.off, n.size);
      if (addr && typeof addr.ptr === 'string' && addr.ptr.startsWith('import:')) return u32(this.host.console.kernelVariable(addr.ptr.slice(7), addr.off || 0, n.size));
      throw new Error(`load through ${describe(addr)} from ${this.show(n.addr)}`);
    }
    if (n.base === '') return this.entryGlobal(env, n.off, n.size);
    if (n.base === 'this') return this.loadThis(env, n.off, n.size);
    if (n.base === 'node') return this.entryField(env, env.node, n.off, n.size);
    if (n.base === 'stack') return this.entryStack(env, env, n.off, n.size);
    if (n.base.startsWith('res')) return this.entryBuffer(env, Number(n.base.slice(3)), n.off, n.size);
    if (n.base.startsWith('import:')) return u32(this.host.console.kernelVariable(n.base.slice(7), n.off, n.size));
    if (n.base.startsWith('g')) {
      const p = this.globalPointer(env, n.base, n.off);
      if (p && (p.matrixStack !== undefined || p.vb !== undefined || p.device !== undefined)) return p;
      return this.loadAt(env, p, n.size);
    }
    if (env.mat && n.base === env.mat.object) return this.entryMaterial(env, env.mat, n.off, n.size);
    throw new Error(`load from ${n.base}`);
  }

  // The object a typed global pointer names: the handle the global held when the path started, offset into it
  globalPointer(env, base, off) {
    const addr = parseInt(base.slice(1), 16);
    const v = this.entryGlobal(env, addr, 4);
    if (typeof v === 'number') return u32(v + off);
    if (v && v.obj !== undefined) return { obj: v.obj, off: (v.off || 0) + off };
    if (v && v.res !== undefined) return { res: v.res, off: (v.off || 0) + off };
    if (v && v.stack !== undefined) return { stack: v.stack + off, frame: v.frame };
    if (off === 0 && v && typeof v === 'object') return v;
    throw new Error(`global 0x${addr.toString(16)} holds ${describe(v)} which cannot be offset by ${off}`);
  }

  // Reads a word or a float block from the stack frame of the running function
  stackLoad(env, off, size) {
    if (size <= 4) {
      const v = env.stack.get(off);
      if (v === undefined) {
        const span = this.stackSpanning(env, off);
        if (span !== undefined) return span;
        throw new Error(`uninitialized stack read at 0x${off.toString(16)}`);
      }
      return wordOf(v);
    }
    const exact = env.stack.get(off);
    if (exact instanceof Float32Array && exact.length * 4 >= size) return exact.length * 4 === size ? exact : exact.slice(0, size / 4);
    if (exact && exact.f64 !== undefined && size === 8) return exact;
    const out = new Float32Array(size / 4);
    for (let i = 0; i < out.length; i++) {
      const v = env.stack.get(off + 4 * i);
      if (v === undefined) {
        const span = this.stackSpanning(env, off + 4 * i);
        if (span === undefined) { if (i < 4) throw new Error(`uninitialized stack read at 0x${(off + 4 * i).toString(16)}`); break; }
        out[i] = floatFromBits(valueBits(span));
        continue;
      }
      out[i] = typeof v === 'number' ? floatFromBits(v) : (v instanceof Float32Array ? v[0] : this.scalar(v));
    }
    return out;
  }

  // A word inside a block or double stored on the stack
  stackSpanning(env, off) {
    for (let delta = 4; delta < 64; delta += 4) {
      const v = env.stack.get(off - delta);
      if (v === undefined) continue;
      if (v instanceof Float32Array && delta / 4 < v.length) return bitsFromFloat(v[delta / 4]);
      if (v && v.f64 !== undefined && delta === 4) { bitsBuf.setFloat64(0, v.f64, true); return bitsBuf.getUint32(4, true); }
    }
    return undefined;
  }

  // Reads from the memory a lock handed back or an allocation returned
  bufferLoad(env, seq, off, size) {
    const r = env.results.get(seq);
    if (r && r.obj !== undefined) return this.readField(r.obj, (r.off || 0) + off, size);
    if (!r || !r.buffer) throw new Error(`load from the result of call ${seq} which is no locked buffer`);
    const view = r.buffer.view;
    if (size === 4) return view.getUint32(off, true);
    if (size === 1) return view.getUint8(off);
    if (size === 2) return view.getUint16(off, true);
    throw new Error(`locked buffer read of ${size} bytes`);
  }

  evalBin(n, env) {
    const op = n.op;
    if (FLOAT_BIN_OPS.has(op)) {
      const a = this.scalar(this.eval(n.a, env)), b = this.scalar(this.eval(n.b, env));
      switch (op) {
        case 'fadd': return a + b;
        case 'fsub': return a - b;
        case 'fmul': return a * b;
        case 'fdiv': return a / b;
        case 'fmin': return Math.min(a, b);
        case 'fmax': return Math.max(a, b);
        case 'fmod': return a % b;
        case 'fpow': return Math.pow(a, b);
        case 'fatan2': return Math.atan2(a, b);
      }
    }
    const av = this.eval(n.a, env), bv = this.eval(n.b, env);
    // Pointer arithmetic on handles keeps the handle.
    if ((op === 'add' || op === 'sub') && av && av.obj !== undefined && typeof bv === 'number') return { obj: av.obj, off: (av.off || 0) + (op === 'add' ? (bv | 0) : -(bv | 0)) };
    if (op === 'add' && bv && bv.obj !== undefined && typeof av === 'number') return { obj: bv.obj, off: (bv.off || 0) + (av | 0) };
    if ((op === 'add' || op === 'sub') && av && av.stack !== undefined && typeof bv === 'number') return { stack: av.stack + (op === 'add' ? (bv | 0) : -(bv | 0)), frame: av.frame };
    if ((op === 'add' || op === 'sub') && av && av.res !== undefined && typeof bv === 'number') return { res: av.res, off: (av.off || 0) + (op === 'add' ? (bv | 0) : -(bv | 0)) };
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
      case 'imulhi': return u32(Number((BigInt(a | 0) * BigInt(b | 0)) >> 32n));
      case 'mulhi': return u32(Number((BigInt(a) * BigInt(b)) >> 32n));
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
      case 'ftan': return Math.tan(this.scalar(v));
      case 'ffloor': return Math.floor(this.scalar(v));
      case 'fceil': return Math.ceil(this.scalar(v));
      case 'fexp': return Math.exp(this.scalar(v));
      case 'flog': return Math.log(this.scalar(v));
      case 'flog10': return Math.log10(this.scalar(v));
      case 'fatan': return Math.atan(this.scalar(v));
      case 'fasin': return Math.asin(this.scalar(v));
      case 'facos': return Math.acos(this.scalar(v));
      case 'fsinh': return Math.sinh(this.scalar(v));
      case 'fcosh': return Math.cosh(this.scalar(v));
      case 'ftanh': return Math.tanh(this.scalar(v));
    }
    throw new Error(`unknown unary op ${n.op}`);
  }

  evalCond(n, env) {
    const av = this.eval(n.a, env), bv = this.eval(n.b, env);
    // Handles compare equal only to themselves, and never to a number other than zero.
    if (isHandle(av) || isHandle(bv)) {
      const same = isHandle(av) && isHandle(bv) ? sameHandle(av, bv) : false;
      const zero = (isHandle(av) && typeof bv === 'number' && bv === 0) || (isHandle(bv) && typeof av === 'number' && av === 0);
      if (n.op === 'eq') return same;
      if (n.op === 'ne') return !same;
      if (zero) { const eq = false; switch (n.op) { case 'lt': case 'le': return isHandle(bv) ? true : eq; case 'gt': case 'ge': return isHandle(av) ? true : eq; } }
    }
    const ia = this.int(av), ib = this.int(bv);
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
      const args = argIdx.map((a) => this.eval(a, env));
      const stackArg = args.find((a) => a && a.matrixStack !== undefined);
      if (stackArg) return this.matrixStackCall(slot, stackArg.matrixStack, args, env);
      if (slot === this.model.world_accessor_slot) throw new Error('world matrix accessor called on no matrix stack');
      return this.dispatch(slot, args[0], args.slice(1), env);
    }
    const dr = this.draw;
    if (fn.startsWith('sub_')) {
      const va = parseInt(fn.slice(4), 16);
      const thisVal = thisIdx !== undefined ? this.eval(thisIdx, env) : undefined;
      const self = () => this.object(thisVal, fn);
      const args = argIdx.map((a) => this.eval(a, env));
      const hook = this.hooks.get(va);
      if (hook) return hook(thisVal && thisVal.obj ? thisVal.obj : thisVal, args, env.state, env);
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
      if (va === this.model.render_state_wrapper) {
        this.setRenderState(env.state, this.int(args[0]), this.int(args[1]));
        return { call: fn };
      }
      if (this.functions.has(va)) return this.run(va, thisVal === undefined ? null : thisVal, args, env.state, env);
      if (this.scene.errors[hex8(va)]) throw new Error(`routine ${hex8(va)} is not summarized: ${this.scene.errors[hex8(va)]}`);
      throw new Error(`helper ${fn} has no derived meaning`);
    }
    const args = argIdx.map((a) => this.eval(a, env));
    switch (fn) {
      // The element accessor the optimizer expanded at its call sites, folded back by the derivation
      case ARRAY_ELEMENT: {
        const hook = this.hooks.get(ARRAY_ELEMENT);
        if (!hook) throw new Error('no array element accessor is registered');
        return hook(thisIdx === undefined ? undefined : this.eval(thisIdx, env), args, env.state, env);
      }
      case 'bsearch': {
        // The material node searches the registry the factory filled, by the material name it holds.
        const registry = this.model.material_registry;
        const base = this.int(args[1]);
        if (!registry || base !== registry.base) throw new Error(`bsearch over 0x${base.toString(16)} is not the material registry`);
        const name = this.string(args[0]);
        const m = this.materials.get(name);
        if (!m) return 0;
        const slot = new SceneObject(HEAP_TYPE, null);
        slot.slots.set(0, { material: m });
        return { obj: slot, off: 0 };
      }
      case 'wcscmp':
      case '_wcsicmp': {
        const a = this.string(args[0]), b = this.string(args[1]);
        const equal = fn === 'wcscmp' ? a === b : a.toLowerCase() === b.toLowerCase();
        return equal ? 0 : 1;
      }
      case 'wcslen': return this.string(args[0]).length;
      case 'wcschr': {
        const s = this.string(args[0]);
        const i = s.indexOf(String.fromCharCode(this.int(args[1])));
        return i < 0 ? 0 : { ...args[0], chars: (args[0].chars || 0) + i };
      }
      case 'wcsstr': {
        const s = this.string(args[0]), i = s.indexOf(this.string(args[1]));
        return i < 0 ? 0 : { ...args[0], chars: (args[0].chars || 0) + i };
      }
      case 'wcscat': {
        this.storeString(args[0], this.string(args[0]) + this.string(args[1]), env);
        return args[0];
      }
      case 'wcsncpy': {
        this.storeString(args[0], this.string(args[1]).slice(0, this.int(args[2])), env);
        return args[0];
      }
      case '_wcslwr': {
        this.storeString(args[0], this.string(args[0]).toLowerCase(), env);
        return args[0];
      }
      case '_wcsupr': {
        this.storeString(args[0], this.string(args[0]).toUpperCase(), env);
        return args[0];
      }
      case 'towlower': return this.int(args[0]) < 0x10000 ? String.fromCharCode(this.int(args[0])).toLowerCase().charCodeAt(0) : this.int(args[0]);
      case 'towupper': return this.int(args[0]) < 0x10000 ? String.fromCharCode(this.int(args[0])).toUpperCase().charCodeAt(0) : this.int(args[0]);
      case 'wcscpy': {
        const s = this.string(args[1]);
        this.storeString(args[0], s, env);
        return args[0];
      }
      case 'wcsrchr': {
        const s = this.string(args[0]);
        const i = s.lastIndexOf(String.fromCharCode(this.int(args[1])));
        if (i < 0) return 0;
        // A pointer into the string keeps the holder so a later copy overwrites the tail in place.
        return { ...args[0], chars: (args[0].chars || 0) + i };
      }
      case 'GetTickCount': return u32(Math.floor(performance.now() - this.frameStart + this.host.tickBase));
      case 'D3DXMatrixMultiply': return matMul(this.matrix(args[1], env), this.matrix(args[2], env));
      case 'D3DXMatrixTranspose': return matTranspose(this.matrix(args[1], env));
      case 'D3DXMatrixInverse': return matInverse(this.matrix(args[2], env));
      case 'D3DXMatrixIdentity':
      case 'XGMatrixIdentity': return Float32Array.from(IDENTITY);
      case 'D3DXMatrixScaling': return matScaling(this.floatArg(argIdx[1], env), this.floatArg(argIdx[2], env), this.floatArg(argIdx[3], env));
      case 'D3DXMatrixTranslation': return matTranslation(this.floatArg(argIdx[1], env), this.floatArg(argIdx[2], env), this.floatArg(argIdx[3], env));
      case 'D3DXMatrixRotationQuaternion': return matRotationQuaternion(this.vector(args[1], 16, env));
      case 'D3DXMatrixRotationAxis': return matRotationQuaternion(quatRotationAxis(this.vector(args[1], 12, env), this.floatArg(argIdx[2], env)));
      case 'D3DXMatrixAffineTransformation': return matAffine(this.floatArg(argIdx[1], env), this.int(args[2]) === 0 ? null : this.vector(args[2], 12, env), this.int(args[3]) === 0 ? null : this.vector(args[3], 16, env), this.int(args[4]) === 0 ? null : this.vector(args[4], 12, env));
      case 'D3DXMatrixPerspectiveFovLH': return matPerspectiveFovLH(this.floatArg(argIdx[1], env), this.floatArg(argIdx[2], env), this.floatArg(argIdx[3], env), this.floatArg(argIdx[4], env));
      case 'D3DXMatrixOrthoLH': return matOrthoLH(this.floatArg(argIdx[1], env), this.floatArg(argIdx[2], env), this.floatArg(argIdx[3], env), this.floatArg(argIdx[4], env));
      case 'D3DXVec3TransformNormal': return vec3TransformNormal(this.vector(args[1], 12, env), this.matrix(args[2], env));
      case 'D3DXQuaternionNormalize': return vecNormalize(this.vector(args[1], 16, env));
      case 'D3DXQuaternionRotationAxis': return quatRotationAxis(this.vector(args[1], 12, env), this.floatArg(argIdx[2], env));
      case 'D3DXQuaternionSlerp': return quatSlerp(this.vector(args[1], 16, env), this.vector(args[2], 16, env), this.floatArg(argIdx[3], env));
      case 'D3DXQuaternionMultiply': return quatMultiply(this.vector(args[1], 16, env), this.vector(args[2], 16, env));
      case 'D3DXQuaternionRotationYawPitchRoll': return quatYawPitchRoll(this.floatArg(argIdx[1], env), this.floatArg(argIdx[2], env), this.floatArg(argIdx[3], env));
      case 'XGVec3Lerp': return vec3Lerp(this.vector(args[1], 12, env), this.vector(args[2], 12, env), this.floatArg(argIdx[3], env));
      case 'D3DXCreateMatrixStack': return { matrixStack: new MatrixStack() };
      case 'operator_new':
      case 'operator_new_array': return { obj: new SceneObject(HEAP_TYPE, null), off: 0 };
      case 'operator_delete':
      case 'operator_delete_array': return 0;
      // The graphics buffer constructor initializes the object it is called on and hands it back
      case 'XGRAPHICS::?0Buffer': return thisIdx !== undefined ? this.eval(thisIdx, env) : 0;
      // The vector constructor iterator builds the array elements in place and hands the array back
      case 'YGXPAXIHP6EPAX0::?_H': return args[0];
      case 'HalReadSMCTrayState': this.storeThrough(args[0], 4, u32(this.host.console.trayState()), env); return 0;
      case 'XGetDevices': return u32(this.host.console.devices(this.int(args[0])));
      case 'CreateEventA':
      case 'CreateThread': return { ptr: fn, off: seq };
      case 'D3DDevice_CreateVertexShader': return { program: this.int(args[1]), declaration: this.int(args[0]) };
      case 'D3DDevice_CreateVertexBuffer': return { vb: this.host.createVertexBuffer(this.int(args[0]), this.int(args[2])) };
      case 'D3DVertexBuffer_Lock': {
        const vb = args[0] && args[0].vb ? args[0].vb : null;
        if (!vb) throw new Error(`lock of ${describe(args[0])} which is no vertex buffer`);
        vb.version++;
        return { buffer: vb };
      }
      case 'Direct3DCreate8': return { device: 'd3d' };
      case 'Direct3D_CreateDevice': return 0;
      case 'D3DDevice_GetBackBuffer': return { device: 'backbuffer' };
      case 'D3DSurface_GetDesc': {
        if (!args[1] || args[1].stack === undefined) throw new Error(`surface description read into ${describe(args[1])}`);
        (args[1].frame || env).stack.set(args[1].stack + SURFACE_DESC_WIDTH, this.host.backBufferWidth(env.state));
        (args[1].frame || env).stack.set(args[1].stack + SURFACE_DESC_HEIGHT, this.host.backBufferHeight(env.state));
        return 0;
      }
      case 'D3DResource_Release': return 0;
      case 'XGetVideoFlags': return u32(this.host.console.videoFlags());
      case 'XGetVideoStandard': return u32(this.host.console.videoStandard());
      case 'XQueryValue': return u32(this.host.console.queryValue(this.int(args[0]), args[1], args[2], env));
      case 'XGetAVPack': return u32(this.host.console.avPack());
    }
    if (fn.startsWith('D3DDevice_') || fn.startsWith('D3D::')) return { call: fn };
    throw new Error(`call to ${fn} has no runtime meaning`);
  }

  // Writes a string the executable copies into a global, an object field or a stack buffer, a pointer into
  // an existing string replacing its tail
  storeString(dst, s, env) {
    if (dst && dst.chars !== undefined) {
      const head = this.string({ ...dst, chars: 0 }).slice(0, dst.chars);
      this.storeString({ ...dst, chars: undefined }, head + s, env);
      return;
    }
    if (typeof dst === 'number') { this.memory.store(dst, 4, { str: s }); return; }
    if (dst && dst.obj !== undefined) { this.writeField(dst.obj, dst.off, { str: s }); return; }
    if (dst && dst.stack !== undefined) { (dst.frame || env).stack.set(dst.stack, { str: s }); return; }
    throw new Error(`string copied into ${describe(dst)}`);
  }

  // Runs an ID3DXMatrixStack method on the executable's stack
  matrixStackCall(slot, stack, args, env) {
    const matrixArg = () => {
      const m = args.find((a) => a instanceof Float32Array || (typeof a === 'number' && a > 0x10000 && this.model.globals[String(a)] !== undefined) || (a && a.matrixStack === undefined && (a.obj !== undefined || a.stack !== undefined)));
      if (m === undefined) throw new Error(`matrix stack slot 0x${slot.toString(16)} called without a matrix`);
      return this.matrix(m, env);
    };
    switch (slot) {
      case STACK_POP: stack.pop(); return { call: 'pop' };
      case STACK_PUSH: stack.push(); return { call: 'push' };
      case STACK_IDENTITY: stack.identity(); return { call: 'identity' };
      case STACK_LOAD: stack.load(matrixArg()); return { call: 'load' };
      case STACK_MULT: stack.mult(matrixArg()); return { call: 'mult' };
      case STACK_MULT_LOCAL: stack.multLocal(matrixArg()); return { call: 'multlocal' };
      case STACK_TOP: return stack.top;
    }
    throw new Error(`matrix stack slot 0x${slot.toString(16)} has no runtime meaning`);
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
      return this.run(va, target, args, env.state, env);
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
    if (v && v.chars !== undefined) return this.string({ ...v, chars: undefined }).slice(v.chars);
    if (v && v.str !== undefined) return v.str;
    if (v && v.stack !== undefined) {
      const held = v.frame ? v.frame.stack.get(v.stack) : undefined;
      if (held && held.str !== undefined) return held.str;
    }
    if (typeof v === 'number') {
      const s = this.draw.strings[String(v)];
      if (s !== undefined) return s;
      const g = this.memory.words.get(v);
      if (g && g.value && g.value.str !== undefined) return g.value.str;
    }
    if (v && v.obj !== undefined) {
      const f = this.readField(v.obj, v.off, 4);
      if (f && f.str !== undefined) return f.str;
    }
    throw new Error(`expected a string, got ${describe(v)}`);
  }

  // Resolves a matrix argument: a value, a global, an object field or a stack buffer
  matrix(v, env) {
    if (v instanceof Float32Array && v.length === 16) return v;
    if (typeof v === 'number') {
      const m = this.memory.load(v, 64);
      if (m instanceof Float32Array) return m;
    }
    if (v && v.obj !== undefined) {
      const m = this.readField(v.obj, v.off, 64);
      if (m instanceof Float32Array) return m;
    }
    if (v && v.stack !== undefined) return this.stackLoad(v.frame || env, v.stack, 64);
    throw new Error(`matrix argument ${describe(v)} has no binding`);
  }

  // Resolves a vector argument of a byte size: a value, an object field, a global or a stack buffer
  vector(v, size = 16, env = null) {
    if (v instanceof Float32Array) return v;
    if (v && v.obj !== undefined) {
      const r = this.readField(v.obj, v.off || 0, size);
      if (r instanceof Float32Array) return r;
      if (typeof r === 'number') return this.vector(this.readWords(v.obj, v.off || 0, size), size, env);
    }
    if (typeof v === 'number') {
      const r = this.memory.load(v, size);
      if (r instanceof Float32Array) return r;
    }
    if (v && v.stack !== undefined) return this.stackLoad(v.frame || env, v.stack, size);
    throw new Error(`expected a vector, got ${describe(v)}`);
  }

  // Assembles consecutive float words of an object into a vector
  readWords(obj, off, size) {
    const out = new Float32Array(size / 4);
    for (let i = 0; i < out.length; i++) out[i] = floatFromBits(valueBits(this.readField(obj, off + 4 * i, 4)));
    return out;
  }

  // Runs a summarized function or a runtime hook on an object with the shared device state
  run(va, thisObj, args, state, parent) {
    const hook = this.hooks.get(va);
    if (hook) return hook(thisObj, args, state, parent);
    const paths = this.functions.get(va);
    if (!paths) {
      const why = this.scene.errors[hex8(va)];
      throw new Error(`function ${hex8(va)} is not summarized in the render model${why ? ': ' + why : ''}`);
    }
    const env = this.newEnv(thisObj, args);
    env.state = state;
    env.mesh = parent ? parent.mesh : null;
    try {
      this.runPaths(paths, env, state);
    } catch (err) {
      if (!err.routine) { err.routine = hex8(va); err.message = `${hex8(va)}: ${err.message}`; }
      throw err;
    }
    return env.state.returned;
  }

  // Runs a material Apply method for a scene node holding the material
  applyMaterial(mat, node, parent) {
    const env = this.newEnv(null, [], mat, node);
    env.state = parent.state;
    env.mesh = parent.mesh;
    this.runPaths(mat.apply, env, parent.state);
    return env.state.returned;
  }

  // Runs the first path whose guards hold, applying effects in program order as the guards are decided
  runPaths(paths, env, state) {
    env.state = state;
    if (!paths._trie) paths._trie = buildTrie(paths);
    const p = this.matchPath(paths._trie, env);
    if (!p) throw new Error(`no execution path matches${env.mat ? ' for material ' + env.mat.name : ''}`);
    state.returned = this.eval(p.return, env);
    return state;
  }

  // Walks the guard trie in path order, the effects before each guard applied first
  matchPath(node, env) {
    this.applyEffects(node.effects, env);
    for (const item of node.items) {
      if (item.path) return item.path;
      if (!this.truthy(this.eval(item.guard, env))) continue;
      const found = this.matchPath(item.node, env);
      if (found) return found;
    }
    return null;
  }

  applyEffects(effects, env) {
    for (const e of effects) {
      try {
        this.applyEffect(e, env, env.state);
      } catch (err) {
        if (!err.effect) { err.effect = e; err.message = `${err.message} (at ${hex8(e.va)} ${e.kind} ${e.fn || (e.base || '') + '+0x' + (e.off || 0).toString(16)})`; }
        throw err;
      }
    }
  }

  applyEffect(e, env, state) {
    if (e.kind === 'store') {
      let value = this.eval(e.value, env);
      // A single precision result is stored as its bit pattern, the expression or a fraction typing it
      if (typeof value === 'number' && e.size !== 8 && (!Number.isInteger(value) || this.isFloatExpr(e.value))) value = { bits: bitsFromFloat(value) };
      if (typeof value === 'number' && e.size === 8) value = { f64: value };
      if (typeof value === 'object' && value !== null && value.bits !== undefined && e.size === 8) value = { f64: floatFromBits(value.bits) };
      const base = e.base || '';
      const off = e.off || 0;
      if (e.has_addr) {
        const addr = this.eval(e.addr, env);
        this.noteAt(env, addr, e.size);
        this.storeThrough(addr, e.size, value, env);
        return;
      }
      if (base === '') {
        this.noteGlobal(env, off, e.size);
        this.memory.store(off, e.size, this.storable(value, e.size));
      } else if (base === 'this') {
        const p = this.thisPointer(env, off);
        this.noteAt(env, p, e.size);
        this.storeThrough(p, e.size, value, env);
      } else if (base === 'node') {
        if (!env.node) throw new Error('store to node without an object');
        this.noteField(env, env.node, off, e.size);
        this.writeField(env.node, off, e.size === 8 ? { f64: this.scalar(value) } : (typeof value === 'object' && value !== null && value.bits === undefined ? value : this.int(value)));
      } else if (base === 'stack') {
        this.noteStack(env, env, off);
        env.stack.set(off, value);
      } else if (base.startsWith('res')) {
        this.noteBuffer(env, Number(base.slice(3)), off, e.size);
        this.bufferStore(env, Number(base.slice(3)), off, e.size, value);
      } else if (base.startsWith('g')) {
        const p = this.globalPointer(env, base, off);
        if (typeof p === 'number') {
          this.noteGlobal(env, p, e.size);
          this.memory.store(p, e.size, this.storable(value, e.size));
        } else if (p.obj !== undefined) {
          this.noteField(env, p.obj, p.off, e.size);
          this.writeField(p.obj, p.off, e.size === 8 ? { f64: this.scalar(value) } : (typeof value === 'object' && value !== null && value.bits === undefined ? value : this.int(value)));
        } else if (p.stack !== undefined) {
          this.noteStack(env, p.frame || env, p.stack);
          (p.frame || env).stack.set(p.stack, value);
        } else if (p.res !== undefined) {
          this.noteBuffer(env, p.res, p.off, e.size);
          this.bufferStore(env, p.res, p.off, e.size, value);
        } else {
          throw new Error(`store through ${describe(p)} held by global ${base}`);
        }
      } else if (env.mat && base === env.mat.object) {
        this.noteMaterial(env, env.mat, off, e.size);
        this.storeMaterialField(env.mat, off, e.size, e.size === 8 && typeof value === 'number' ? { f64: value } : value);
      } else {
        throw new Error(`store to ${base} is not mapped`);
      }
      return;
    }
    if (e.kind === 'copy') throw new Error(`copy of unknown length into ${e.base}+0x${(e.off || 0).toString(16)} is not interpreted`);
    const fn = e.fn;
    this.materializeBuffers(e, env);
    // Value producers were folded into the expressions that use their results.
    if (fn.startsWith('D3DX') || fn.startsWith('XG') || fn.startsWith('D3DDevice_Create') || fn.startsWith('Direct3D') || fn.startsWith('D3D::') || fn === 'D3DDevice_GetBackBuffer' || fn === 'D3DResource_Release') return;
    if (fn.startsWith('vcall@') || fn.startsWith('sub_') || !fn.startsWith('D3DDevice_')) {
      // Methods, helpers and library calls run once, their results being memoized by call sequence.
      if (!env.results.has(e.seq)) env.results.set(e.seq, this.performCall(fn, e.args || [], e.has_this ? e.this : undefined, env, e.seq));
      return;
    }
    if (fn.startsWith('D3DDevice_Draw')) {
      state.draws++;
      if (!this.onDraw) throw new Error('draw without a draw handler');
      if (fn === 'D3DDevice_DrawVertices') {
        const args = (e.args || []).map((a) => this.eval(a, env));
        this.onDraw(state, env, { primitive: this.int(args[0]), start: this.int(args[1]), count: this.int(args[2]) });
      } else {
        this.onDraw(state, env, null);
      }
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
        if (typeof m === 'number' || (m && m.obj !== undefined) || (m && m.stack !== undefined)) m = this.matrix(m, env);
        else {
          const buf = e.buffers && e.buffers.find((b) => b.index === 1);
          if (!buf) throw new Error(`transform ${which} has neither a matrix value nor a captured buffer`);
          const elems = buf.elems.flatMap((x) => this.floats(this.eval(x, env))).slice(0, 16);
          if (elems.length < 16) throw new Error(`transform ${which} captured ${elems.length} of 16 floats`);
          m = Float32Array.from(elems);
        }
      }
      state.transforms[which] = Float32Array.from(m);
    } else if (fn === 'D3DDevice_GetTransform') {
      const which = this.int(args[0]);
      const m = state.transforms[which];
      if (!m) throw new Error(`transform ${which} read before it was set`);
      if (!args[1] || args[1].stack === undefined) throw new Error(`transform ${which} read into ${describe(args[1])}`);
      for (let i = 0; i < 16; i++) (args[1].frame || env).stack.set(args[1].stack + 4 * i, bitsFromFloat(m[i]));
    } else if (fn === 'D3DDevice_SetStreamSource') {
      state.stream = args[1];
      state.streamStride = this.int(args[2]);
    } else if (fn === 'D3DDevice_SetIndices') {
      state.indices = args[0];
    } else if (fn === 'D3DDevice_SetMaterial') {
      state.material = args[0];
    } else if (fn === 'D3DDevice_Swap') {
      this.host.onPresent(state);
    } else if (fn === 'D3DDevice_Clear') {
      state.clear = { flags: this.int(args[2]), color: this.int(args[3]) };
      this.host.onClear(state);
    } else if (fn === 'D3DDevice_Reset' || fn === 'D3DDevice_SetFlickerFilter' || fn === 'D3DDevice_SetSoftDisplayFilter' || fn === 'D3DDevice_BlockUntilVerticalBlank' || fn === 'D3DDevice_SetVerticalBlankCallback' || fn === 'D3DDevice_SetViewport' || fn === 'D3DDevice_SetGammaRamp' || fn === 'D3DDevice_PersistDisplay' || fn === 'D3DDevice_SetBackBufferScale' || fn === 'D3DDevice_SetScreenSpaceOffset' || fn === 'D3DDevice_SetOverscanColor') {
      return;
    } else {
      throw new Error(`device call ${fn} is not mapped`);
    }
  }

  // Writes the stack memory a call's pointer arguments refer to, which the path kept as expressions
  materializeBuffers(e, env) {
    for (const b of e.buffers || []) {
      const p = this.eval((e.args || [])[b.index], env);
      if (!p || p.stack === undefined) continue;
      const stack = (p.frame || env).stack;
      let off = p.stack;
      for (const x of b.elems) {
        const v = this.eval(x, env);
        if (v instanceof Float32Array) { stack.set(off, v); off += v.length * 4; continue; }
        stack.set(off, typeof v === 'number' && this.isFloatExpr(x) ? bitsFromFloat(v) : v);
        off += (v && v.f64 !== undefined) ? 8 : 4;
      }
    }
  }

  // Stores through a pointer the path computed: an object field, a global, a stack slot or locked memory
  storeThrough(addr, size, value, env) {
    if (addr && addr.obj !== undefined) {
      this.writeField(addr.obj, addr.off || 0, size === 8 ? { f64: this.scalar(value) } : (typeof value === 'object' && value !== null && value.bits === undefined ? value : this.int(value)));
      return;
    }
    if (typeof addr === 'number') { this.memory.store(addr, size, this.storable(value, size)); return; }
    if (addr && addr.stack !== undefined) { (addr.frame || env).stack.set(addr.stack, value); return; }
    if (addr && addr.res !== undefined) { this.bufferStore(env, addr.res, addr.off || 0, size, value); return; }
    throw new Error(`store through ${describe(addr)}`);
  }

  // A value in the form the global store keeps: words, doubles, blocks and handles
  storable(value, size) {
    if (value instanceof Float32Array) return value;
    if (typeof value === 'object' && value !== null && value.bits !== undefined && size !== 8) return value.bits;
    return value;
  }

  // Writes into the memory a lock handed back or an allocation returned
  bufferStore(env, seq, off, size, value) {
    const r = env.results.get(seq);
    if (r && r.obj !== undefined) { this.writeField(r.obj, (r.off || 0) + off, size === 8 ? { f64: this.scalar(value) } : (typeof value === 'object' && value !== null && value.bits === undefined ? value : this.int(value))); return; }
    if (!r || !r.buffer) throw new Error(`store into the result of call ${seq} which is no locked buffer`);
    const view = r.buffer.view;
    if (size === 4) view.setUint32(off, valueBits(value), true);
    else if (size === 1) view.setUint8(off, valueBits(value) & 0xFF);
    else if (size === 2) view.setUint16(off, valueBits(value) & 0xFFFF, true);
    else if (size === 8 && value && value.f64 !== undefined) view.setFloat64(off, value.f64, true);
    else throw new Error(`locked buffer write of ${size} bytes`);
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
    if (typeof value === 'object' && value !== null && value.f64 !== undefined) {
      bitsBuf.setFloat64(0, value.f64, true);
      for (let i = 0; i < 8; i++) mat._fields.set(off + i, bitsBuf.getUint8(i));
      return;
    }
    const bits = typeof value === 'object' && value !== null ? value.bits : u32(value);
    for (let i = 0; i < size; i++) mat._fields.set(off + i, (bits >>> (8 * i)) & 0xFF);
  }

  // Reports whether a pooled expression produces a floating point value
  isFloatExpr(idx) {
    const n = this.nodes[idx];
    switch (n.k) {
      case 'f': return true;
      case 'elem': return true;
      case 'un': return ['f32', 'f64', 'bits2f', 'bits2d', 'i2f', 'fneg', 'fabs', 'fsin', 'fcos', 'fsqrt'].includes(n.op);
      case 'bin': return ['fadd', 'fsub', 'fmul', 'fdiv'].includes(n.op);
      case 'load': return n.size === 8;
    }
    return false;
  }

  show(idx) {
    return JSON.stringify(this.nodes[idx]);
  }
}

// Shares the guard prefixes of a function's paths so matching one evaluates each guard once, each trie node
// holding the effects its paths recorded between the guard leading to it and the next
function buildTrie(paths) {
  const root = { items: [], effects: [] };
  for (const p of paths) {
    let node = root;
    let from = 0;
    for (let i = 0; i < p.guards.length; i++) {
      const g = p.guards[i], pos = p.guard_pos[i];
      node.effects = p.effects.slice(from, pos);
      from = pos;
      let next = null;
      for (const item of node.items) if (item.guard === g) { next = item.node; break; }
      if (!next) { next = { items: [], effects: [] }; node.items.push({ guard: g, node: next }); }
      node = next;
    }
    node.effects = p.effects.slice(from);
    node.items.push({ path: p });
  }
  return root;
}

// Byte size of a scene field type
function fieldSize(type) {
  switch (type) {
    case FIELD_VEC3: case FIELD_COLOR: return 12;
    case FIELD_ROTATION: return 16;
    case FIELD_NUMBER: case FIELD_INT: case FIELD_NODE: case FIELD_STRING: return 4;
    case FIELD_BOOL: return 1;
  }
  return 4;
}

function isHandle(v) {
  return v !== null && typeof v === 'object' && !(v instanceof Float32Array) && v.bits === undefined && v.f64 === undefined;
}

function sameHandle(a, b) {
  if (a.obj !== undefined || b.obj !== undefined) return a.obj === b.obj && (a.off || 0) === (b.off || 0);
  if (a.material !== undefined || b.material !== undefined) return a.material === b.material;
  if (a.vb !== undefined || b.vb !== undefined) return a.vb === b.vb;
  if (a.matrixStack !== undefined || b.matrixStack !== undefined) return a.matrixStack === b.matrixStack;
  if (a.texture !== undefined || b.texture !== undefined) return a.texture === b.texture;
  if (a.str !== undefined || b.str !== undefined) return a.str === b.str;
  return a === b;
}

function describe(v) {
  if (v instanceof Float32Array) return `vec${v.length}`;
  if (v && v.obj !== undefined) return `${v.obj.type}#${v.obj.id}+${(v.off || 0).toString(16)}`;
  if (v && v.stack !== undefined) return `stack+0x${v.stack.toString(16)}${v.chars !== undefined ? '[' + v.chars + ']' : ''}`;
  if (v && v.scope !== undefined) return `scope of ${v.scope.type}#${v.scope.id}`;
  return JSON.stringify(v, (k, x) => (x instanceof SceneObject ? `${x.type}#${x.id}` : x));
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

export function matScaling(sx, sy, sz) {
  const out = Float32Array.from(IDENTITY);
  out[0] = sx; out[5] = sy; out[10] = sz;
  return out;
}

export function matTranslation(x, y, z) {
  const out = Float32Array.from(IDENTITY);
  out[12] = x; out[13] = y; out[14] = z;
  return out;
}

// D3DXMatrixRotationQuaternion
export function matRotationQuaternion(q) {
  const [x, y, z, w] = q;
  const out = Float32Array.from(IDENTITY);
  out[0] = 1 - 2 * (y * y + z * z); out[1] = 2 * (x * y + z * w); out[2] = 2 * (x * z - y * w);
  out[4] = 2 * (x * y - z * w); out[5] = 1 - 2 * (x * x + z * z); out[6] = 2 * (y * z + x * w);
  out[8] = 2 * (x * z + y * w); out[9] = 2 * (y * z - x * w); out[10] = 1 - 2 * (x * x + y * y);
  return out;
}

// D3DXMatrixAffineTransformation with the rotation about an optional center
export function matAffine(scaling, center, rotation, translation) {
  let m = matScaling(scaling, scaling, scaling);
  if (rotation) {
    if (center) m = matMul(m, matTranslation(-center[0], -center[1], -center[2]));
    m = matMul(m, matRotationQuaternion(rotation));
    if (center) m = matMul(m, matTranslation(center[0], center[1], center[2]));
  }
  if (translation) m = matMul(m, matTranslation(translation[0], translation[1], translation[2]));
  return m;
}

// D3DXMatrixPerspectiveFovLH
export function matPerspectiveFovLH(fovy, aspect, zn, zf) {
  const yScale = 1 / Math.tan(fovy / 2), xScale = yScale / aspect;
  const out = new Float32Array(16);
  out[0] = xScale; out[5] = yScale; out[10] = zf / (zf - zn); out[11] = 1; out[14] = -zn * zf / (zf - zn);
  return out;
}

// D3DXMatrixOrthoLH
export function matOrthoLH(w, h, zn, zf) {
  const out = new Float32Array(16);
  out[0] = 2 / w; out[5] = 2 / h; out[10] = 1 / (zf - zn); out[14] = -zn / (zf - zn); out[15] = 1;
  return out;
}

// D3DXQuaternionRotationAxis, the axis normalized first
export function quatRotationAxis(axis, angle) {
  const n = vecNormalize(Float32Array.from([axis[0], axis[1], axis[2], 0]));
  const s = Math.sin(angle / 2);
  return Float32Array.from([n[0] * s, n[1] * s, n[2] * s, Math.cos(angle / 2)]);
}

// D3DXQuaternionSlerp
export function quatSlerp(a, b, t) {
  let dot = a[0] * b[0] + a[1] * b[1] + a[2] * b[2] + a[3] * b[3];
  let sign = 1;
  if (dot < 0) { dot = -dot; sign = -1; }
  let wa, wb;
  if (1 - dot < 0.001) {
    wa = 1 - t; wb = t;
  } else {
    const theta = Math.acos(dot), s = Math.sin(theta);
    wa = Math.sin((1 - t) * theta) / s; wb = Math.sin(t * theta) / s;
  }
  wb *= sign;
  return Float32Array.from([wa * a[0] + wb * b[0], wa * a[1] + wb * b[1], wa * a[2] + wb * b[2], wa * a[3] + wb * b[3]]);
}

// D3DXQuaternionMultiply, the rotation q1 followed by q2
export function quatMultiply(q1, q2) {
  return Float32Array.from([
    q2[3] * q1[0] + q2[0] * q1[3] + q2[1] * q1[2] - q2[2] * q1[1],
    q2[3] * q1[1] - q2[0] * q1[2] + q2[1] * q1[3] + q2[2] * q1[0],
    q2[3] * q1[2] + q2[0] * q1[1] - q2[1] * q1[0] + q2[2] * q1[3],
    q2[3] * q1[3] - q2[0] * q1[0] - q2[1] * q1[1] - q2[2] * q1[2],
  ]);
}

// D3DXQuaternionRotationYawPitchRoll
export function quatYawPitchRoll(yaw, pitch, roll) {
  const sy = Math.sin(yaw / 2), cy = Math.cos(yaw / 2), sp = Math.sin(pitch / 2), cp = Math.cos(pitch / 2), sr = Math.sin(roll / 2), cr = Math.cos(roll / 2);
  return Float32Array.from([cy * sp * cr + sy * cp * sr, sy * cp * cr - cy * sp * sr, cy * cp * sr - sy * sp * cr, cy * cp * cr + sy * sp * sr]);
}

export function vec3Lerp(a, b, t) {
  return Float32Array.from([a[0] + t * (b[0] - a[0]), a[1] + t * (b[1] - a[1]), a[2] + t * (b[2] - a[2])]);
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

export { floatFromBits, bitsFromFloat, TS_VIEW, TS_PROJECTION, TS_WORLD };
