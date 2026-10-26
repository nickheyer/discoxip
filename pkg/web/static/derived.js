
import { ShaContext, hmacSha1 } from './system.js';
import { ConsoleApi } from './console.js';
import { GpuRegisters } from './gpu.js';
import { udiv64, urem64, idiv64, irem64 } from './machine.js';

const FLOAT_BIN_OPS = new Set(['fadd', 'fsub', 'fmul', 'fdiv', 'fmin', 'fmax', 'fmod', 'fpow', 'fatan2', 'frem']);
// The 64 by 32 bit divisions, each taking the big integer a hilo pair makes of the high and low words
const WIDE_DIV_OPS = { udiv64: udiv64, udiv64_rem: urem64, idiv64: idiv64, idiv64_rem: irem64 };
const FLOAT_UN_OPS = new Set(['f32', 'f64', 'bits2f', 'bits2d', 'i2f', 'fneg', 'fabs', 'fsin', 'fcos', 'fsqrt', 'ftan', 'ffloor', 'fceil', 'fexp', 'flog', 'flog10', 'flog2', 'ftrunc', 'fround', 'fatan', 'fasin', 'facos', 'fsinh', 'fcosh', 'ftanh']);

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

// Functions the compiled expressions call by name
const EXPR_HELPERS = {
  f32, roundEven, bitsFromFloat, floatFromBits, rawLoad, rawStore, udiv64, urem64, idiv64, irem64,
  rethrow: (err) => { throw err; },
  bsf: (b) => (b === 0 ? 0 : 31 - Math.clz32(b & -b)),
  bsr: (b) => (b === 0 ? 0 : 31 - Math.clz32(b)),
};

export const IDENTITY = Float32Array.from([1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1]);

// Scene file field types as the executable's field tables number them
const FIELD_INT = 1, FIELD_BOOL = 2, FIELD_NUMBER = 3, FIELD_VEC3 = 5, FIELD_ROTATION = 6, FIELD_COLOR = 8, FIELD_NODE = 9, FIELD_STRING = 10;

// ID3DXMatrixStack methods by vtable offset, from the XDK D3DX interface
const STACK_POP = 0xC, STACK_PUSH = 0x10, STACK_IDENTITY = 0x14, STACK_LOAD = 0x18, STACK_MULT = 0x1C, STACK_MULT_LOCAL = 0x20;
const STACK_ROTATE_AXIS = 0x24, STACK_ROTATE_AXIS_LOCAL = 0x28, STACK_ROTATE_YPR = 0x2C, STACK_ROTATE_YPR_LOCAL = 0x30, STACK_SCALE = 0x34, STACK_SCALE_LOCAL = 0x38, STACK_TRANSLATE = 0x3C, STACK_TRANSLATE_LOCAL = 0x40, STACK_TOP = 0x44;
const STACK_METHODS = { Pop: STACK_POP, Push: STACK_PUSH, LoadIdentity: STACK_IDENTITY, LoadMatrix: STACK_LOAD, MultMatrix: STACK_MULT, MultMatrixLocal: STACK_MULT_LOCAL, RotateAxis: STACK_ROTATE_AXIS, RotateAxisLocal: STACK_ROTATE_AXIS_LOCAL, RotateYawPitchRoll: STACK_ROTATE_YPR, RotateYawPitchRollLocal: STACK_ROTATE_YPR_LOCAL, Scale: STACK_SCALE, ScaleLocal: STACK_SCALE_LOCAL, Translate: STACK_TRANSLATE, TranslateLocal: STACK_TRANSLATE_LOCAL, GetTop: STACK_TOP };

// Xbox D3D transform states
const TS_VIEW = 0, TS_PROJECTION = 1, TS_WORLD = 6;

let nextObjectId = 1;

// An instance of a registered node class backed by a scene node
export class SceneObject {
  constructor(type, node) {
    this.id = nextObjectId++;
    this.type = type;
    this.node = node;
    this.mem = new ByteStore();
  }
}

// Bytes a page of sparse memory holds
const PAGE_BYTES = 4096;
// Bytes of the widest word a global holds without being listed among the wide ones
const NARROW_WORD = 8;

const scratch = new DataView(new ArrayBuffer(8));
const scratchBytes = new Uint8Array(scratch.buffer);

// Pages given back by released stores, cleared over the bytes they were written in, for the next store to take
const PAGE_POOL = [];

// Takes a cleared page for a base, from the pool when it holds one
function acquirePage(base) {
  const p = PAGE_POOL.pop();
  if (p) { p.base = base; return p; }
  return { base, bytes: new Uint8Array(PAGE_BYTES), written: new Uint8Array(PAGE_BYTES), held: new Uint8Array(PAGE_BYTES), lo: PAGE_BYTES, hi: 0 };
}

// Clears a page over the range it was written in and returns it to the pool
function releasePage(p) {
  if (p.hi > p.lo) {
    p.bytes.fill(0, p.lo, p.hi);
    p.written.fill(0, p.lo, p.hi);
    p.held.fill(0, p.lo, p.hi);
  }
  p.lo = PAGE_BYTES;
  p.hi = 0;
  PAGE_POOL.push(p);
}

// Byte addressed memory kept in pages, the runtime handles stored into it standing beside the bytes they cover
export class ByteStore {
  constructor() {
    this.pages = new Map();
    this.handles = new Map();
    this.last = undefined;
    this.released = false;
    // Counts every write, so a value remembered for an unwritten range is dropped once anything writes
    this.writes = 0;
  }

  // Gives the pages back once the memory is finished with, after which any access is a fault
  release() {
    for (const p of this.pages.values()) releasePage(p);
    this.pages.clear();
    this.handles.clear();
    this.last = undefined;
    this.released = true;
  }

  pageOf(addr, make) {
    const last = this.last;
    if (last !== undefined && addr >= last.base && addr < last.base + PAGE_BYTES) return last;
    if (this.released) throw new Error(`access to a stack frame whose routine has returned`);
    const i = Math.floor(addr / PAGE_BYTES);
    let p = this.pages.get(i);
    if (!p && make) {
      p = acquirePage(i * PAGE_BYTES);
      this.pages.set(i, p);
    }
    if (p) this.last = p;
    return p;
  }

  // Reads a word whose bytes were all written and lie under no handle, or undefined where that does not hold
  loadWritten(off, size) {
    const h = this.handles.get(off);
    if (h !== undefined) return size === 4 ? h : (size < 4 ? narrowOf(h, size) : undefined);
    const p = this.pageHolding(off, size, false);
    if (!p || size > 8) return undefined;
    const i = off - p.base, w = p.written, held = p.held, b = p.bytes;
    for (let k = 0; k < size; k++) if (w[i + k] !== 1 || held[i + k] !== 0) return undefined;
    if (size === 4) return (b[i] | (b[i + 1] << 8) | (b[i + 2] << 16) | (b[i + 3] << 24)) >>> 0;
    if (size === 8) { for (let k = 0; k < 8; k++) scratchBytes[k] = b[i + k]; return { f64: scratch.getFloat64(0, true) }; }
    return size === 2 ? b[i] | (b[i + 1] << 8) : b[i];
  }

  // Marks a run of bytes as written, widening the range the page is cleared over when released
  touch(p, start, n) {
    this.writes++;
    if (start < p.lo) p.lo = start;
    if (start + n > p.hi) p.hi = start + n;
  }

  byteWritten(addr) {
    const p = this.pageOf(addr, false);
    return !!p && p.written[addr - p.base] === 1;
  }

  // A byte of the memory, zero where nothing was written
  byteAt(addr) {
    const p = this.pageOf(addr, false);
    return p ? p.bytes[addr - p.base] : 0;
  }

  // Visits a range one page at a time with the page or null, the index into it and the run length
  eachPage(off, size, make, visit) {
    for (let i = 0; i < size;) {
      const a = off + i, p = this.pageOf(a, make), start = a - (p ? p.base : Math.floor(a / PAGE_BYTES) * PAGE_BYTES), n = Math.min(PAGE_BYTES - start, size - i);
      if (visit(p, start, n, i) === false) return false;
      i += n;
    }
    return true;
  }

  // The page a range lies wholly inside, or null where it spans pages or no page exists yet
  pageHolding(off, size, make) {
    const p = this.pageOf(off, make);
    return p && off - p.base + size <= PAGE_BYTES ? p : null;
  }

  allWritten(off, size) {
    const p = this.pageHolding(off, size, false);
    if (p) {
      const start = off - p.base;
      for (let k = start; k < start + size; k++) if (p.written[k] !== 1) return false;
      return true;
    }
    return this.eachPage(off, size, false, (q, start, n) => {
      if (!q) return false;
      for (let k = start; k < start + n; k++) if (q.written[k] !== 1) return false;
      return true;
    });
  }

  anyWritten(off, size) {
    const p = this.pageHolding(off, size, false);
    if (p) {
      const start = off - p.base;
      for (let k = start; k < start + size; k++) if (p.written[k] === 1) return true;
      return false;
    }
    return !this.eachPage(off, size, false, (q, start, n) => {
      if (!q) return true;
      for (let k = start; k < start + n; k++) if (q.written[k] === 1) return false;
      return true;
    });
  }

  bytes(off, size) {
    const out = new Uint8Array(size);
    const p = this.pageHolding(off, size, false);
    if (p) { out.set(p.bytes.subarray(off - p.base, off - p.base + size)); return out; }
    this.eachPage(off, size, false, (q, start, n, i) => { if (q) out.set(q.bytes.subarray(start, start + n), i); });
    return out;
  }

  // Writes bytes in place of whatever handles they overlap
  setBytes(off, src) {
    this.dropHandles(off, src.length);
    const p = this.pageHolding(off, src.length, true);
    if (p) {
      const start = off - p.base;
      p.bytes.set(src, start);
      p.written.fill(1, start, start + src.length);
      this.touch(p, start, src.length);
      return;
    }
    this.eachPage(off, src.length, true, (q, start, n, i) => {
      q.bytes.set(src.subarray(i, i + n), start);
      q.written.fill(1, start, start + n);
      this.touch(q, start, n);
    });
  }

  // Writes a word of up to four bytes, in place of whatever handle it overlaps
  setWord(off, size, bits) {
    const p = this.pageHolding(off, size, true);
    if (!p) { scratch.setUint32(0, bits, true); this.setBytes(off, scratchBytes.subarray(0, size)); return; }
    const i = off - p.base, held = p.held;
    for (let k = 0; k < size; k++) if (held[i + k] !== 0) { this.dropHandles(off, size); break; }
    const b = p.bytes, w = p.written;
    b[i] = bits & 0xFF; w[i] = 1;
    if (size > 1) { b[i + 1] = (bits >>> 8) & 0xFF; w[i + 1] = 1; }
    if (size > 2) { b[i + 2] = (bits >>> 16) & 0xFF; b[i + 3] = bits >>> 24; w[i + 2] = 1; w[i + 3] = 1; }
    this.touch(p, i, size);
  }

  // The handles overlapping a range, by the word starts a short range holds or by walking a long one
  handlesOver(off, size) {
    const out = [];
    if (size + 3 < this.handles.size) {
      for (let at = off - 3; at < off + size; at++) if (this.handles.has(at)) out.push(at);
    } else {
      for (const at of this.handles.keys()) if (at < off + size && off < at + 4) out.push(at);
      out.sort((a, b) => a - b);
    }
    return out;
  }

  dropHandles(off, size) {
    if (!this.handles.size || !this.anyHeld(off, size)) return;
    for (const at of this.handlesOver(off, size)) {
      this.handles.delete(at);
      this.eachPage(at, 4, false, (p, start, n) => { if (p) p.held.fill(0, start, start + n); });
    }
  }

  // Reports whether any byte of a range lies under a handle's word
  anyHeld(off, size) {
    const p = this.pageHolding(off, size, false);
    if (p) {
      const start = off - p.base, held = p.held;
      for (let k = start; k < start + size; k++) if (held[k] !== 0) return true;
      return false;
    }
    return !this.eachPage(off, size, false, (q, start, n) => {
      if (!q) return true;
      for (let k = start; k < start + n; k++) if (q.held[k] !== 0) return false;
      return true;
    });
  }

  // The handle whose word overlaps a range, with the offset it sits at
  handleCovering(off, size) {
    if (!this.handles.size || !this.anyHeld(off, size)) return null;
    const over = this.handlesOver(off, size);
    return over.length ? { at: over[0], handle: this.handles.get(over[0]) } : null;
  }

  // Forgets a range, which then reads as never written
  clear(off, size) {
    this.writes++;
    this.dropHandles(off, size);
    this.eachPage(off, size, false, (p, start, n) => { if (p) p.written.fill(0, start, start + n); });
  }

  // Stores a word, double, float block or inline string as bytes, anything else as a handle over a word
  store(off, size, value) {
    if (value instanceof Float32Array) {
      this.setBytes(off, new Uint8Array(value.buffer, value.byteOffset, value.byteLength));
      return;
    }
    if (value && value.f64 !== undefined) {
      if (size === 8) scratch.setFloat64(0, value.f64, true); else scratch.setFloat32(0, value.f64, true);
      this.setBytes(off, scratchBytes.subarray(0, size === 8 ? 8 : 4));
      return;
    }
    if (value && value.str !== undefined && value.inline) {
      const b = new Uint8Array(2 * (value.str.length + 1));
      for (let i = 0; i < value.str.length; i++) { const c = value.str.charCodeAt(i); b[2 * i] = c & 0xFF; b[2 * i + 1] = c >>> 8; }
      this.setBytes(off, b);
      return;
    }
    if (typeof value === 'number' || (value && value.bits !== undefined)) {
      if (size <= 4) { this.setWord(off, size, valueBits(value)); return; }
      scratch.setUint32(0, valueBits(value), true);
      scratch.setUint32(4, 0, true);
      this.setBytes(off, scratchBytes.subarray(0, size));
      return;
    }
    if (value === undefined || value === null) throw new Error(`${value} stored at +0x${off.toString(16)}`);
    this.setBytes(off, ZERO_WORD);
    this.handles.set(off, value);
    this.eachPage(off, 4, true, (p, start, n) => { p.held.fill(1, start, start + n); this.touch(p, start, n); });
  }

  // Reads a handle stored exactly at an offset, else the bytes there, which read as zero where never written
  load(off, size, what) {
    if (this.handles.size === 0 && size <= 4) {
      const p = this.pageHolding(off, size, false);
      if (!p) return valueFromBytes(this.bytes(off, size), size);
      const b = p.bytes, i = off - p.base;
      if (size === 4) return (b[i] | (b[i + 1] << 8) | (b[i + 2] << 16) | (b[i + 3] << 24)) >>> 0;
      return size === 2 ? b[i] | (b[i + 1] << 8) : b[i];
    }
    const h = this.handles.get(off);
    if (h !== undefined) {
      if (size === 4) return h;
      if (size < 4) return narrowOf(h, size);
      throw new Error(`${what} +0x${off.toString(16)} of ${size} bytes starts at the ${describe(h)} handle`);
    }
    const over = this.handleCovering(off, size);
    if (over) throw new Error(`${what} +0x${off.toString(16)} read inside the ${describe(over.handle)} handle at +0x${over.at.toString(16)}`);
    if (size <= 4) {
      const p = this.pageHolding(off, size, false);
      if (!p) return valueFromBytes(this.bytes(off, size), size);
      const b = p.bytes, i = off - p.base;
      if (size === 4) return (b[i] | (b[i + 1] << 8) | (b[i + 2] << 16) | (b[i + 3] << 24)) >>> 0;
      return size === 2 ? b[i] | (b[i + 1] << 8) : b[i];
    }
    return valueFromBytes(this.bytes(off, size), size);
  }

  // A half word at an offset, or undefined where neither of its bytes was written
  unitAt(off) {
    if (!this.byteWritten(off) && !this.byteWritten(off + 1)) return undefined;
    return this.byteAt(off) | (this.byteAt(off + 1) << 8);
  }
}

// Memory the executable allocates while running, zeroed by the allocator and bounded by the size it asked for
export class FlatStore {
  constructor(size) {
    this.flat = true;
    this.size = size;
    this.data = new Uint8Array(size);
    this.held = new Uint8Array(size);
    this.handles = new Map();
  }

  // Takes the size a reallocation gives the block, keeping the bytes it held
  grow(size) {
    if (size > this.data.length) {
      const data = new Uint8Array(size), held = new Uint8Array(size);
      data.set(this.data);
      held.set(this.held);
      this.data = data;
      this.held = held;
    }
    this.size = size;
  }

  release() {}

  inside(off, size) {
    return off >= 0 && off + size <= this.size;
  }

  byteWritten(addr) {
    return addr >= 0 && addr < this.size;
  }

  byteAt(addr) {
    return this.byteWritten(addr) ? this.data[addr] : 0;
  }

  allWritten(off, size) {
    return this.inside(off, size);
  }

  anyWritten(off, size) {
    return off < this.size && off + size > 0;
  }

  bytes(off, size) {
    const out = new Uint8Array(size);
    for (let i = 0; i < size; i++) if (this.byteWritten(off + i)) out[i] = this.data[off + i];
    return out;
  }

  outside(off, size, what) {
    return new Error(`${what} of ${size} bytes at +0x${off.toString(16)} outside the ${this.size} bytes of the block`);
  }

  setBytes(off, src) {
    if (!this.inside(off, src.length)) throw this.outside(off, src.length, 'write');
    this.dropHandles(off, src.length);
    this.data.set(src, off);
  }

  setWord(off, size, bits) {
    if (!this.inside(off, size)) throw this.outside(off, size, 'write');
    const held = this.held;
    for (let k = 0; k < size; k++) if (held[off + k] !== 0) { this.dropHandles(off, size); break; }
    const b = this.data;
    b[off] = bits & 0xFF;
    if (size > 1) b[off + 1] = (bits >>> 8) & 0xFF;
    if (size > 2) { b[off + 2] = (bits >>> 16) & 0xFF; b[off + 3] = bits >>> 24; }
  }

  anyHeld(off, size) {
    const held = this.held;
    for (let k = Math.max(off, 0), e = Math.min(off + size, this.size); k < e; k++) if (held[k] !== 0) return true;
    return false;
  }

  handlesOver(off, size) {
    const out = [];
    for (let at = off - 3; at < off + size; at++) if (this.handles.has(at)) out.push(at);
    return out;
  }

  dropHandles(off, size) {
    if (!this.handles.size || !this.anyHeld(off, size)) return;
    for (const at of this.handlesOver(off, size)) {
      this.handles.delete(at);
      this.held.fill(0, at, Math.min(at + 4, this.size));
    }
  }

  handleCovering(off, size) {
    if (!this.handles.size || !this.anyHeld(off, size)) return null;
    const over = this.handlesOver(off, size);
    return over.length ? { at: over[0], handle: this.handles.get(over[0]) } : null;
  }

  // Forgets a range, which then reads as the zeros the allocator gave it
  clear(off, size) {
    this.dropHandles(off, size);
    for (let k = Math.max(off, 0), e = Math.min(off + size, this.size); k < e; k++) this.data[k] = 0;
  }

  store(off, size, value) {
    if (value instanceof Float32Array) {
      this.setBytes(off, new Uint8Array(value.buffer, value.byteOffset, value.byteLength));
      return;
    }
    if (value && value.f64 !== undefined) {
      if (size === 8) scratch.setFloat64(0, value.f64, true); else scratch.setFloat32(0, value.f64, true);
      this.setBytes(off, scratchBytes.subarray(0, size === 8 ? 8 : 4));
      return;
    }
    if (value && value.str !== undefined && value.inline) {
      const b = new Uint8Array(2 * (value.str.length + 1));
      for (let i = 0; i < value.str.length; i++) { const c = value.str.charCodeAt(i); b[2 * i] = c & 0xFF; b[2 * i + 1] = c >>> 8; }
      this.setBytes(off, b);
      return;
    }
    if (typeof value === 'number' || (value && value.bits !== undefined)) {
      if (size <= 4) { this.setWord(off, size, valueBits(value)); return; }
      scratch.setUint32(0, valueBits(value), true);
      scratch.setUint32(4, 0, true);
      this.setBytes(off, scratchBytes.subarray(0, size));
      return;
    }
    if (value === undefined || value === null) throw new Error(`${value} stored at +0x${off.toString(16)}`);
    this.setBytes(off, ZERO_WORD);
    this.handles.set(off, value);
    this.held.fill(1, off, off + 4);
  }

  load(off, size, what) {
    const h = this.handles.get(off);
    if (h !== undefined) {
      if (size === 4) return h;
      if (size < 4) return narrowOf(h, size);
      throw new Error(`${what} +0x${off.toString(16)} of ${size} bytes starts at the ${describe(h)} handle`);
    }
    const over = this.handleCovering(off, size);
    if (over) throw new Error(`${what} +0x${off.toString(16)} read inside the ${describe(over.handle)} handle at +0x${over.at.toString(16)}`);
    if (!this.inside(off, size)) throw this.outside(off, size, what);
    const b = this.data;
    if (size === 4) return (b[off] | (b[off + 1] << 8) | (b[off + 2] << 16) | (b[off + 3] << 24)) >>> 0;
    if (size < 4) return size === 2 ? b[off] | (b[off + 1] << 8) : b[off];
    return valueFromBytes(b.subarray(off, off + size), size);
  }

  loadWritten(off, size) {
    const h = this.handles.get(off);
    if (h !== undefined) return size === 4 ? h : (size < 4 ? narrowOf(h, size) : undefined);
    if (size > 4 || !this.inside(off, size)) return undefined;
    const held = this.held, b = this.data;
    for (let k = 0; k < size; k++) if (held[off + k] !== 0) return undefined;
    if (size === 4) return (b[off] | (b[off + 1] << 8) | (b[off + 2] << 16) | (b[off + 3] << 24)) >>> 0;
    return size === 2 ? b[off] | (b[off + 1] << 8) : b[off];
  }

  unitAt(off) {
    if (!this.byteWritten(off) && !this.byteWritten(off + 1)) return undefined;
    return this.byteAt(off) | (this.byteAt(off + 1) << 8);
  }
}

export const BUFFER_TYPE = '$buffer';
// The loader's archive record, whose fields beyond the ones a finished load sets read as zero
export const ARCHIVE_TYPE = '$archive';
// Memory the executable allocates while running, read and written by field offset
export const HEAP_TYPE = '$heap';

// The descriptor and value a scripted field write hands to the class field hook
const FIELD_TYPE = '$field', VALUE_TYPE = '$value';
// A script value the runtime's engine holds, converted for natives by the dispatcher's own virtual calls
export const SCRIPT_VALUE_TYPE = '$script';

// The name a folded counted array index carries, the accessor call the plain build makes
export const ARRAY_ELEMENT = 'array_element';

// Characters the executable's own path buffers hold, past which a global is no string
const WIDE_STRING_LIMIT = 0x800;

// Stack pointer a lifted routine starts with, its return slot there and its arguments above it
const LIFTED_ENTRY_SP = 0x10000;
// Blocks one lifted routine may run before it is taken for a loop that never ends
const LIFTED_BLOCK_LIMIT = 1 << 24;
// Effects and blocks a frame may run before it is given up as stuck
const FRAME_WORK_BUDGET = 8000000;
// Characters of code one compiled lifted routine may take, past which the routine is interpreted instead
const CODE_BUDGET = 4000000;
// Marks a routine whose compiled form outgrew the budget, which runs by interpretation
const INTERPRETED = Symbol('interpreted');

// Xbox D3D primitive type of the triangle lists the mesh exporter writes
const PT_TRIANGLELIST = 5;

// Where the kernel's processor control region keeps its own address, its processor block's and that block itself
const KPCR_SELF = 0x1C, KPCR_PRCB = 0x20, KPCR_PRCB_DATA = 0x28;
// Where the thread information block keeps the stack base, which the XAPI keeps its thread local storage pointer
// array at, and where the kernel's thread block keeps its thread local storage and the handle naming the thread
const TIB_STACK_BASE = 0x4, KTHREAD_TLS_DATA = 0x28, ETHREAD_UNIQUE_THREAD = 0x12C;
// The handle the runtime names the thread the dashboard runs on by
const MAIN_THREAD_ID = 1;

// D3DXCreateMeshFVF option asking for 32 bit indices
const D3DXMESH_32BIT = 1;

// Flexible vertex format bits that size a vertex
const FVF_XYZ = 0x002, FVF_XYZRHW = 0x004, FVF_NORMAL = 0x010, FVF_DIFFUSE = 0x040, FVF_SPECULAR = 0x080, FVF_TEXCOUNT_SHIFT = 8, FVF_TEXCOUNT_MASK = 0xF00;

let nextRawId = 1;

// Bytes the executable addresses directly, such as a locked buffer or a surface, read and written in place
export class RawMemory {
  constructor(size, bytes = null) {
    this.id = nextRawId++;
    this.bytes = bytes || new Uint8Array(size);
    this.view = new DataView(this.bytes.buffer, this.bytes.byteOffset, this.bytes.byteLength);
    this.version = 0;
    // The range of bytes written since the memory was last read into a geometry
    this.dirtyLo = 0;
    this.dirtyHi = this.bytes.length;
  }

  // Widens the written range over a store
  dirty(off, size) {
    if (off < this.dirtyLo) this.dirtyLo = off;
    if (off + size > this.dirtyHi) this.dirtyHi = off + size;
  }
}

// The console's physical memory, reached from the processor at each physical address with the alias bit the image's
// own resource routines set, the kernel handing out contiguous runs of its pages
export class PhysicalMemory {
  constructor(resource, bytes, reserved) {
    this.alias = resource ? resource.alias >>> 0 : null;
    this.bytes = bytes;
    // The top pages the kernel keeps for itself and the GPU, which no contiguous allocation reaches
    this.reserved = reserved;
    this.store = new ByteStore();
    this.runs = [];
  }

  // Reports whether an address lies in the window the alias bit opens onto physical memory
  window(addr) {
    return this.alias !== null && addr >= this.alias && addr < this.alias + this.bytes;
  }

  // The physical address an address in the window reaches
  physical(addr) {
    return addr - this.alias;
  }

  // Takes the highest run of pages within the bounds that the alignment allows, the way the kernel searches down from
  // the highest acceptable page, and hands back its address in the window or zero when no run fits
  allocate(size, lowest, highest, alignment) {
    if (this.alias === null) throw new Error('the image carries no resource routine reaching physical memory');
    const pages = Math.ceil(Math.max(size, 1) / PAGE_BYTES) * PAGE_BYTES;
    const align = Math.max(alignment, PAGE_BYTES);
    const top = Math.min(highest + 1, this.bytes - this.reserved);
    for (let base = Math.floor((top - pages) / align) * align; base >= lowest; base -= align) {
      if (this.runs.some((r) => base < r.base + r.size && r.base < base + pages)) continue;
      this.runs.push({ base, size: pages });
      return u32(this.alias + base);
    }
    return 0;
  }

  // Gives back the run starting at an address
  free(addr) {
    const base = this.physical(addr);
    const i = this.runs.findIndex((r) => r.base === base);
    if (i < 0) throw new Error(`0x${addr.toString(16)} starts no run of physical memory the kernel handed out`);
    this.runs.splice(i, 1);
  }
}

// Bytes one vertex of a flexible vertex format takes
export function fvfStride(fvf) {
  let stride = 0;
  if (fvf & FVF_XYZRHW) stride += 16;
  else if (fvf & FVF_XYZ) stride += 12;
  if (fvf & FVF_NORMAL) stride += 12;
  if (fvf & FVF_DIFFUSE) stride += 4;
  if (fvf & FVF_SPECULAR) stride += 4;
  stride += 8 * ((fvf & FVF_TEXCOUNT_MASK) >> FVF_TEXCOUNT_SHIFT);
  return stride;
}

// Reads a byte, half word, word, double or float block out of raw memory
function rawLoad(raw, off, size) {
  if (off < 0 || off + size > raw.bytes.length) throw new Error(`read of ${size} bytes at +0x${off.toString(16)} outside the ${raw.bytes.length} bytes of raw#${raw.id}`);
  const v = raw.view;
  if (size === 1) return v.getUint8(off);
  if (size === 2) return v.getUint16(off, true);
  if (size === 4) return v.getUint32(off, true);
  if (size === 8) return { f64: v.getFloat64(off, true) };
  const out = new Float32Array(size / 4);
  for (let i = 0; i < out.length; i++) out[i] = v.getFloat32(off + 4 * i, true);
  return out;
}

// Writes a word, double or float block into raw memory, which holds bytes and never handles
function rawStore(raw, off, size, value) {
  if (off < 0 || off + size > raw.bytes.length) throw new Error(`write of ${size} bytes at +0x${off.toString(16)} outside the ${raw.bytes.length} bytes of raw#${raw.id}`);
  const v = raw.view;
  raw.version++;
  raw.dirty(off, value instanceof Float32Array ? value.length * 4 : size);
  if (value instanceof Float32Array) {
    for (let i = 0; i < size / 4; i++) v.setFloat32(off + 4 * i, value[i] || 0, true);
    return;
  }
  if (value && value.f64 !== undefined) {
    if (size === 8) v.setFloat64(off, value.f64, true); else v.setFloat32(off, value.f64, true);
    return;
  }
  const bits = valueBits(value);
  if (size === 1) v.setUint8(off, bits & 0xFF);
  else if (size === 2) v.setUint16(off, bits & 0xFFFF, true);
  else if (size === 4) v.setUint32(off, bits, true);
  else if (size === 8) { v.setUint32(off, bits, true); v.setUint32(off + 4, 0, true); }
  else throw new Error(`write of ${size} bytes of a word into raw memory`);
}

// Makes the loaded geometry object a mesh node points at, with the fields its draw reads
export function makeBuffer(model, geometry) {
  const obj = new SceneObject(BUFFER_TYPE, null);
  obj.geometry = geometry;
  const f = model.draw.buffer_fields;
  obj.mem.store(f.vertices, 4, { vertices: geometry });
  obj.mem.store(f.indices, 4, { ib: geometry.indexBuffer });
  obj.mem.store(f.stride, 4, geometry.stride);
  obj.mem.store(f.format, 4, geometry.format);
  obj.mem.store(f.count, 4, geometry.primitiveCount);
  obj.mem.store(f.primitive, 4, PT_TRIANGLELIST);
  return obj;
}

export function newState() {
  return { rs: {}, stages: {}, textures: {}, palettes: {}, program: undefined, constants: {}, transforms: {}, stream: undefined, indices: undefined, draws: 0, returned: 0, material: undefined, clear: null };
}

export function cloneState(s) {
  const stages = {};
  for (const [k, v] of Object.entries(s.stages)) stages[k] = { ...v };
  return { rs: { ...s.rs }, stages, textures: { ...s.textures }, palettes: { ...s.palettes }, program: s.program, constants: { ...s.constants }, transforms: { ...s.transforms }, stream: s.stream, indices: s.indices, draws: s.draws, returned: s.returned, material: s.material, clear: s.clear };
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
export class VertexBuffer extends RawMemory {
  constructor(size, fvf) {
    super(size);
    this.fvf = fvf;
  }
}

// An index buffer the executable fills through a lock or the exporter wrote, its indices two or four bytes each
export class IndexBuffer extends RawMemory {
  constructor(size, indexBytes = 2, bytes = null) {
    super(size, bytes);
    this.indexBytes = indexBytes;
  }
}

// A mesh D3DX builds for the executable, its vertices and indices behind the ID3DXBaseMesh methods
export class D3DXMesh {
  constructor(numFaces, numVertices, options, fvf) {
    this.numFaces = numFaces;
    this.numVertices = numVertices;
    this.options = options;
    this.fvf = fvf;
    this.vb = new VertexBuffer(numVertices * fvfStride(fvf), fvf);
    this.ib = new IndexBuffer(numFaces * 3 * ((options & D3DXMESH_32BIT) ? 4 : 2), (options & D3DXMESH_32BIT) ? 4 : 2);
  }
}

// ID3DXBaseMesh methods by vtable offset
const MESH_ADDREF = 0x4, MESH_RELEASE = 0x8, MESH_DRAW_SUBSET = 0xC, MESH_NUM_FACES = 0x10, MESH_NUM_VERTICES = 0x14, MESH_FVF = 0x18, MESH_OPTIONS = 0x20;
const MESH_GET_VB = 0x30, MESH_GET_IB = 0x34, MESH_LOCK_VB = 0x38, MESH_UNLOCK_VB = 0x3C, MESH_LOCK_IB = 0x40, MESH_UNLOCK_IB = 0x44;

// Values the running path overwrote, by address and in order, each with the index of the effect that overwrote
// it, so a load reads the memory as it stood after the effects recorded before it
// A handle read narrower than its word: the leading characters of a string it names, else the handle itself
function narrowOf(v, size) {
  if (v && v.str !== undefined && size < 4) return bitsAt(v, 0);
  return v;
}

// A load read when its effect ran but which failed, thrown again where the value is used
class EarlyError {
  constructor(err) { this.err = err; }
}
EXPR_HELPERS.EarlyError = EarlyError;

// Zero bytes a handle's word holds under it
const ZERO_WORD = new Uint8Array(4);

// The memory the console starts the executable with, the loaded image under the words the paths write
class GlobalMemory {
  constructor(memory, image) {
    this.sections = memory.sections.map((s) => ({ name: s.name, va: s.va, end: s.va + s.size, off: s.off, writable: !!s.writable }));
    const last = this.sections[this.sections.length - 1];
    if (!last || image.length < last.off + (last.end - last.va)) throw new Error(`startup memory of ${image.length} bytes is shorter than the ${this.sections.length} sections laid out in it`);
    this.image = image;
    this.words = new Map();
    // Words wider than a double are few and listed apart, so a lookup scans the starts of narrow words only
    this.wide = new Map();
    // The loader zero fills the pages it lays each section in, so the tail of a section's last page is memory too
    for (const s of this.sections) s.pageEnd = Math.ceil(s.end / PAGE_BYTES) * PAGE_BYTES;
    // The physical memory the kernel hands out and the GPU's registers, which the derived model attaches
    this.physical = null;
    this.gpu = null;
  }

  // The loaded section a range lies in, or null where the image maps no memory
  sectionOf(addr, size) {
    let lo = 0, hi = this.sections.length - 1;
    while (lo <= hi) {
      const mid = (lo + hi) >> 1, s = this.sections[mid];
      if (addr < s.va) hi = mid - 1;
      else if (addr >= s.end) lo = mid + 1;
      else return addr + size <= s.end ? s : null;
    }
    return null;
  }

  // Reports whether the image, physical memory or the GPU's registers map the address
  mapped(addr) {
    addr >>>= 0;
    return this.pageOf(addr) !== null || this.physical.window(addr) || this.gpu.claims(addr);
  }

  // The section whose pages hold an address, the zero filled tail of its last page included
  pageOf(addr) {
    let lo = 0, hi = this.sections.length - 1;
    while (lo <= hi) {
      const mid = (lo + hi) >> 1, s = this.sections[mid];
      if (addr < s.va) hi = mid - 1;
      else if (addr >= s.pageEnd) lo = mid + 1;
      else return s;
    }
    return null;
  }

  // The section a name the executable loads by names, or null where the image has none
  sectionNamed(name) {
    return this.sections.find((s) => s.name === name) || null;
  }

  // A section by the handle the section routines hand out, which is its index past zero
  sectionByHandle(handle) {
    const s = this.sections[handle - 1];
    if (!s) throw new Error(`section handle ${handle} names no section of the image`);
    return s;
  }

  // The wide string a global holds, or null where the memory holds no terminated printable text
  wideString(addr) {
    if (!this.mapped(addr + 1)) return null;
    let out = '';
    for (let a = addr; this.mapped(a + 1) && out.length <= WIDE_STRING_LIMIT; a += 2) {
      const unit = this.load(a, 2);
      if (unit === 0) return out;
      if (unit < 0x20 && unit !== 9 && unit !== 10 && unit !== 13) return null;
      out += String.fromCharCode(unit);
    }
    return null;
  }

  // Finds the stored word covering an address
  covering(addr) {
    for (let delta = 0; delta < NARROW_WORD; delta++) {
      const w = this.words.get(addr - delta);
      if (w && addr < addr - delta + w.size) return { off: addr - delta, w };
    }
    for (const [off, w] of this.wide) if (addr >= off && addr < off + w.size) return { off, w };
    return null;
  }

  // The image bytes of a range no stored word overlaps
  imageRange(off, size) {
    for (let a = off - (NARROW_WORD - 1); a < off + size; a++) {
      const w = this.words.get(a);
      if (w && a + w.size > off) return null;
    }
    for (const [o, w] of this.wide) if (o < off + size && off < o + w.size) return null;
    const s = this.sectionOf(off, size);
    return s === null ? null : this.image.subarray(s.off + off - s.va, s.off + off - s.va + size);
  }

  byteAt(addr) {
    addr >>>= 0;
    if (this.physical.window(addr)) return this.physical.store.byteAt(this.physical.physical(addr));
    if (this.gpu.claims(addr)) return this.gpu.read(addr, 1);
    const c = this.covering(addr);
    if (c) return wordByte(c.w, addr - c.off);
    const s = this.sectionOf(addr, 1);
    if (s !== null) return this.image[s.off + addr - s.va];
    if (!this.mapped(addr)) throw new Error(`0x${addr.toString(16)} is outside the memory the image loads into`);
    return 0;
  }

  // An address arrives as the word the executable holds, which a high one can carry as a negative offset
  load(off, size) {
    off >>>= 0;
    if (this.physical.window(off)) return this.physical.store.load(this.physical.physical(off), size, 'physical memory');
    if (this.gpu.claims(off)) return this.gpu.read(off, size);
    const exact = this.words.get(off);
    if (exact && exact.size === size) return exact.value;
    const direct = this.imageRange(off, size);
    if (direct !== null) return valueFromBytes(direct, size);
    const bytes = new Uint8Array(size);
    for (let i = 0; i < size; i++) bytes[i] = this.byteAt(off + i);
    return valueFromBytes(bytes, size);
  }

  store(off, size, value) {
    off >>>= 0;
    if (this.physical.window(off)) {
      this.physical.store.store(this.physical.physical(off), size, value);
      return;
    }
    if (this.gpu.claims(off)) {
      this.gpu.write(off, size, valueBits(value));
      return;
    }
    const c = this.covering(off);
    if (c && c.w.value instanceof Float32Array && size === 4 && (off - c.off) % 4 === 0 && off + 4 <= c.off + c.w.size) {
      c.w.value[(off - c.off) / 4] = floatFromBits(valueBits(value));
      return;
    }
    const hit = [];
    for (let delta = -(NARROW_WORD - 1); delta < size; delta++) {
      const k = off + delta, w = this.words.get(k);
      if (w && w.size <= NARROW_WORD && k + w.size > off) hit.push([k, w]);
    }
    for (const [k, w] of this.wide) if (k < off + size && off < k + w.size) hit.push([k, w]);
    for (const [k, w] of hit) {
      if (k === off && w.size === size) continue;
      this.words.delete(k);
      this.wide.delete(k);
      if (k < off || k + w.size > off + size) {
        for (let b = 0; b < w.size; b++) {
          const bo = k + b;
          if (bo >= off && bo < off + size) continue;
          this.words.set(bo, { size: 1, value: wordByte(w, b) });
        }
      }
    }
    const word = { size, value };
    this.words.set(off, word);
    if (size > NARROW_WORD) this.wide.set(off, word);
  }
}

// The bits of a stored value from a byte offset into it, that byte lowest
function bitsAt(v, delta) {
  if (v instanceof Float32Array) {
    const word = bitsFromFloat(v[delta >> 2]);
    return (delta & 3) === 0 ? word : word >>> (8 * (delta & 3));
  }
  if (v && v.f64 !== undefined) {
    bitsBuf.setFloat64(0, v.f64, true);
    let out = 0;
    for (let i = 3; i >= 0; i--) out = out * 256 + (delta + i < 8 ? bitsBuf.getUint8(delta + i) : 0);
    return u32(out);
  }
  if (v && v.str !== undefined) {
    let out = 0;
    for (let i = 3; i >= 0; i--) {
      const b = delta + i, unit = (b >> 1) < v.str.length ? v.str.charCodeAt(b >> 1) : 0;
      out = out * 256 + ((b & 1) ? (unit >>> 8) & 0xFF : unit & 0xFF);
    }
    return u32(out);
  }
  return delta === 0 ? valueBits(v) : valueBits(v) >>> (8 * delta);
}

// One byte of a stored word, block, double, string or byte snapshot
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
  if (typeof v === 'object' && v !== null && v.str !== undefined) return bitsAt(v, i) & 0xFF;
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
  constructor(model, host, image) {
    this.model = model;
    this.host = host;
    this.nodes = model.exprs.nodes;
    this.compiled = new Array(this.nodes.length);
    this.verify = false;
    this.interpret = false;
    this.lookupCaches = new Map();
    this.materials = new Map();
    for (const m of model.materials) {
      this.materials.set(m.name, m);
      m._fields = new Map();
      for (const [off, b] of Object.entries(m.fields)) m._fields.set(Number(off), b);
    }
    this.memory = new GlobalMemory(model.memory, image);
    this.consoleApi = new ConsoleApi(host.console);
    this.consoleApi.d = this;
    this.memory.physical = new PhysicalMemory(model.resource, this.consoleApi.memoryBytes(), this.consoleApi.reservedTopBytes());
    this.memory.gpu = new GpuRegisters(model.gpu);
    // The processor control region the fs segment addresses, its exception chain ending where the loader left it and
    // the words the kernel keeps its own address and its processor block's in pointing back into it
    this.fs = new ByteStore();
    const pcr = new SceneObject(HEAP_TYPE, null);
    pcr.mem = this.fs;
    this.fs.store(0, 4, 0xFFFFFFFF);
    this.fs.store(KPCR_SELF, 4, { obj: pcr, off: 0 });
    this.fs.store(KPCR_PRCB, 4, { obj: pcr, off: KPCR_PRCB_DATA });
    // The thread the dashboard runs on, which the processor block names, its thread local storage the block the
    // image's TLS directory describes, copied from the template and zero filled after it, and the pointer array the
    // XAPI keeps at the stack base naming that block in the module's slot
    const thread = new SceneObject(HEAP_TYPE, null);
    thread.mem.store(ETHREAD_UNIQUE_THREAD, 4, MAIN_THREAD_ID);
    this.fs.store(KPCR_PRCB_DATA, 4, { obj: thread, off: 0 });
    if (model.tls) {
      const tls = new SceneObject(HEAP_TYPE, null);
      for (let i = 0; i < model.tls.end - model.tls.start; i++) tls.mem.store(i, 1, this.memory.load(model.tls.start + i, 1));
      thread.mem.store(KTHREAD_TLS_DATA, 4, { obj: tls, off: 0 });
      const slots = new SceneObject(HEAP_TYPE, null);
      slots.mem.store(this.memory.load(model.tls.index, 4) * 4, 4, { obj: tls, off: 0 });
      this.fs.store(TIB_STACK_BASE, 4, { obj: slots, off: 0 });
    }
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
    this.lifted = new Map();
    for (const [va, fn] of Object.entries(model.lifted || {})) this.lifted.set(Number(va), fn);
    // The routines the image names by address, and by name where the name is the routine's alone
    this.libraryNames = new Map();
    this.libraryAddresses = new Map();
    const shared = new Set();
    for (const [va, name] of Object.entries(model.library)) {
      this.libraryNames.set(Number(va), name);
      if (this.libraryAddresses.has(name)) shared.add(name);
      this.libraryAddresses.set(name, Number(va));
    }
    for (const name of shared) this.libraryAddresses.delete(name);
    this.hooks = new Map();
    this.onDraw = null;
    this.traced = new Set();
    this.loadMemo = new Map();
    this.helperVA = new Map();
    this.work = 0;
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
    try {
      this.runPaths(t.construct, env, this.host.device);
    } finally {
      env.stack.release();
    }
    if (!node || !node._fields) return;
    for (let c = t; c; c = this.types[c.parent]) {
      for (const [off, f] of Object.entries(c.fields)) {
        if (node._fields[f.name] === undefined) continue;
        obj.mem.clear(Number(off), fieldSize(f.type));
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
    const cache = this.offsetCache(typeName, 'typeSlot');
    const hit = cache.get(slot);
    if (hit !== undefined) return hit;
    let t = this.types[typeName];
    if (!t) throw new Error(`node type ${typeName} is not registered by the executable`);
    while (t && t.slots.length <= slot / 4) t = this.types[t.parent];
    if (!t) throw new Error(`node type ${typeName} has no vtable slot 0x${slot.toString(16)}`);
    cache.set(slot, t.slots[slot / 4]);
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
    const cache = this.offsetCache(typeName, 'fieldAt');
    let f = cache.get(off);
    if (f !== undefined) return f;
    f = null;
    for (let t = this.types[typeName]; t; t = this.types[t.parent]) {
      const found = t.fields[String(off)];
      if (found) { f = found; break; }
      if (!t.parent) break;
    }
    cache.set(off, f);
    return f;
  }

  // A per class map memoizing a lookup by object offset
  offsetCache(typeName, what) {
    let byType = this.lookupCaches.get(what);
    if (!byType) { byType = new Map(); this.lookupCaches.set(what, byType); }
    let cache = byType.get(typeName);
    if (!cache) { cache = new Map(); byType.set(typeName, cache); }
    return cache;
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
    if (!t) return undefined;
    const cache = this.offsetCache(typeName, 'ctorInit');
    let init = cache.get(off);
    if (init !== undefined) return init === null ? undefined : init;
    init = t.init[String(off)];
    if (t.init_error && !init) throw new Error(`${typeName} constructor defaults were not derived: ${t.init_error}`);
    cache.set(off, init === undefined ? null : init);
    return init;
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
    return this.summarized(this.typeSlot(typeName, this.draw.field_set_slot));
  }

  // Reports whether a routine is carried in the model, as paths or as lifted blocks
  summarized(va) {
    return this.functions.has(va) || this.lifted.has(va);
  }

  // Runs the class hook a field write calls, returning whether the generic store follows
  setField(obj, field, value, state) {
    const va = this.typeSlot(obj.type, this.draw.field_set_slot);
    const desc = new SceneObject(FIELD_TYPE, null);
    desc.mem.store(0, 4, u32(field.off));
    desc.mem.store(4, 4, u32(field.type));
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
    return { this: thisObj, args, mat, node, stack: new ByteStore(), results: new Map(), mesh: null, loads: new Map(), parent: null };
  }

  // The object this points at in a run: a scene object, a static address or a stack buffer, offset
  thisPointer(env, off) {
    const t = env.this;
    // A routine entered with no object holds a null in the register, which only a use through it refuses
    if (t === null || t === undefined) return u32(off);
    if (typeof t === 'number') return u32(t + off);
    if (t instanceof SceneObject) return { obj: t, off };
    if (t.obj !== undefined) return { obj: t.obj, off: (t.off || 0) + off };
    if (t.stack !== undefined) return { stack: t.stack + off, frame: t.frame };
    if (t.res !== undefined) return { res: t.res, off: (t.off || 0) + off };
    if (t.raw !== undefined) return { raw: t.raw, off: (t.off || 0) + off };
    if (t.str !== undefined) return { str: t.str, chars: (t.chars || 0) + off / 2 };
    throw new Error(`this is ${describe(t)}`);
  }

  // Reads a word or block at an offset from this
  loadThis(env, off, size) {
    return this.loadLive(env, this.thisPointer(env, off), size);
  }

  // Reads through a resolved address as the memory stands now
  loadLive(env, p, size) {
    if (typeof p === 'number') return this.memory.load(p, size);
    if (p.obj !== undefined) return this.readField(p.obj, p.off || 0, size);
    if (p.stack !== undefined) return this.stackLoad(p.frame || env, p.stack, size);
    if (p.str !== undefined) return this.stringWord(p, size);
    if (p.raw !== undefined) return rawLoad(p.raw, p.off || 0, size);
    return this.bufferLoad(env, p.res, p.off || 0, size);
  }

  // Reads an object field as bytes, a handle stored there, or what the object held before any write
  readField(obj, off, size) {
    if (typeof obj === 'number') return this.memory.load(obj + off, size);
    if (!obj || !obj.mem) throw new Error(`field +0x${off.toString(16)} read on ${describe(obj)}`);
    if (obj.type === VALUE_TYPE) return obj.read(off, size);
    const mem = obj.mem, node = obj.node;
    // A field nothing wrote keeps the value it falls back to until the object or its scene fields change
    const memo = obj.fallbackMemo;
    if (memo !== undefined && memo.writes === mem.writes && memo.version === node._fieldsVersion) {
      const hit = memo.values.get(off * 256 + size);
      if (hit !== undefined) return hit;
    }
    const written = mem.loadWritten(off, size);
    if (written !== undefined) return written;
    if (!mem.anyWritten(off, size)) {
      const v = this.fallbackField(obj, off, size);
      if (node && !(v instanceof Float32Array)) {
        let m = obj.fallbackMemo;
        if (m === undefined || m.writes !== mem.writes || m.version !== node._fieldsVersion) m = obj.fallbackMemo = { writes: mem.writes, version: node._fieldsVersion, values: new Map() };
        m.values.set(off * 256 + size, v);
      }
      return v;
    }
    if (mem.allWritten(off, size) || mem.handles.has(off)) return mem.load(off, size, `${obj.type} field`);
    const bytes = mem.bytes(off, size);
    for (let i = 0; i < size; i++) if (!mem.byteWritten(off + i)) bytes[i] = this.fallbackByte(obj, off + i);
    const over = mem.handleCovering(off, size);
    if (over) throw new Error(`${obj.type} field +0x${off.toString(16)} read inside the ${describe(over.handle)} handle at +0x${over.at.toString(16)}`);
    return valueFromBytes(bytes, size);
  }

  // What an object holds where nothing wrote: a scene file field, a constructor default or the allocator's zeros
  fallbackField(obj, off, size) {
    if (obj.type === BUFFER_TYPE) throw new Error(`mesh buffer field +0x${off.toString(16)} is not one its draw reveals`);
    if (obj.type === ARCHIVE_TYPE) return size === 8 ? { f64: 0 } : (size > 8 ? new Float32Array(size / 4) : 0);
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
    if (size > 8) return this.readWords(obj, off, size);
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
    // Allocated memory nothing wrote yet reads as the zeroed pages the allocator starts from
    if (obj.type === HEAP_TYPE && (obj.size === undefined || off + size <= obj.size)) return size === 8 ? { f64: 0 } : 0;
    throw new Error(`${obj.type} object field +0x${off.toString(16)} is outside the object${obj.size !== undefined ? ' of ' + obj.size + ' bytes' : ''}`);
  }

  // One byte of what an object holds where nothing wrote, taken from the field or default covering it
  fallbackByte(obj, addr) {
    const t = this.types[obj.type];
    if (t) {
      const exact = this.fieldAt(obj.type, addr);
      const f = exact ? { off: addr, name: exact.name, type: exact.type } : this.fieldSpanning(obj.type, addr);
      if (f && obj.node && obj.node.derivedField(f.name, f.type) !== undefined) return wordByte({ value: this.fallbackField(obj, f.off, fieldSize(f.type)) }, addr - f.off);
      for (let d = 0; d < 8; d++) {
        const init = t.init[String(addr - d)];
        if (init && d < init.size) return wordByte({ value: init.size === 8 ? { f64: init.double || 0 } : u32(init.value) }, d);
      }
      if (addr < t.size) return 0;
    }
    if (obj.type === HEAP_TYPE && (obj.size === undefined || addr < obj.size)) return 0;
    if (obj.type === ARCHIVE_TYPE) return 0;
    throw new Error(`${obj.type} object byte +0x${addr.toString(16)} is outside the object`);
  }

  // A word inside a double the constructor stores
  ctorInitSpanning(typeName, off) {
    const t = this.types[typeName];
    if (!t) return undefined;
    const init = t.init[String(off - 4)];
    if (init && init.size === 8) { bitsBuf.setFloat64(0, init.double || 0, true); return bitsBuf.getUint32(4, true); }
    return undefined;
  }

  writeField(obj, off, value, size = 4) {
    if (typeof obj === 'number') { this.memory.store(obj + off, value instanceof Float32Array ? value.length * 4 : (value && value.f64 !== undefined ? 8 : size), value); return; }
    obj.mem.store(off, size, value);
  }

  // Stores a value into a stack frame
  stackStore(frame, off, size, value) {
    frame.stack.store(off, size, value);
  }

  // Evaluates a pooled expression node through the function compiled for it, checked against its interpretation
  // when the model verifies its compiler
  eval(idx, env) {
    if (this.interpret) return this.evalNode(idx, env);
    const f = this.compiled[idx];
    if (this.verify) {
      const a = (f === undefined ? this.compileExpr(idx) : f)(env);
      const b = this.evalNode(idx, env);
      if (!sameValue(a, b)) throw new Error(`compiled expression ${idx} gave ${describe(a)} where its interpretation gives ${describe(b)}: ${this.show(idx)}`);
      return a;
    }
    return (f === undefined ? this.compileExpr(idx) : f)(env);
  }

  // Compiles a pooled expression into a function of the run's environment, each node becoming the statements
  // its interpretation runs, with subexpressions shared where no call sits between their uses
  compileExpr(root) {
    const emitter = this.exprEmitter();
    const body = [];
    const result = emitter.emit(root, body);
    body.push(`return ${result};`);
    const make = new Function('d', 'N', 'F', `return function expr${root}(env) {\n${body.join('\n')}\n};`);
    const fn = make(this, this.nodes, EXPR_HELPERS);
    this.compiled[root] = fn;
    return fn;
  }

  // Makes an emitter writing the statements evaluating pooled expressions, each node becoming what its
  // interpretation runs, a subexpression evaluating once however many times the expression uses it
  exprEmitter(useEnvLoads = true) {
    const nodes = this.nodes;
    const callMemo = new Map();
    let preread = null;
    const hasCall = (idx) => {
      let found = callMemo.get(idx);
      if (found !== undefined) return found;
      const n = nodes[idx];
      found = n.k === 'call';
      if (!found) for (const key of ['a', 'b', 'c', 'addr', 'this']) if (n[key] !== undefined && hasCall(n[key])) { found = true; break; }
      if (!found) for (const key of ['args', 'elems']) if (n[key]) for (const x of n[key]) if (hasCall(x)) { found = true; break; }
      callMemo.set(idx, found);
      return found;
    };
    let temps = 0;
    let share = false;
    let scopes = [];
    const lookup = (idx) => { for (let i = scopes.length - 1; i >= 0; i--) { const t = scopes[i].get(idx); if (t !== undefined) return t; } return undefined; };
    const num = (v) => (Number.isNaN(v) ? 'NaN' : (v === Infinity ? 'Infinity' : (v === -Infinity ? '-Infinity' : (Object.is(v, -0) ? '-0' : String(v)))));
    const emitNode = (idx, out) => {
      const found = share ? lookup(idx) : undefined;
      if (found !== undefined) return found;
      const n = nodes[idx];
      let expr;
      const temp = (code) => { const t = 't' + temps++; out.push(`var ${t} = ${code};`); return t; };
      // A literal operand is held in a temp where the code reads a property off it
      const ref = (x) => (/^[-0-9]|^NaN$|^Infinity$/.test(x) ? temp(x) : x);
      const intOf = (t) => `(typeof ${t} === 'number' ? ${t} >>> 0 : d.intOf(${t}, env))`;
      const scalarOf = (t) => `(typeof ${t} === 'number' ? ${t} : d.scalar(${t}))`;
      switch (n.k) {
        case 'i': expr = String(u32(n.v)); break;
        case 'f': expr = n.v !== undefined ? num(n.v) : (n.s === 'NaN' ? 'NaN' : (n.s === '+Inf' ? 'Infinity' : '-Infinity')); break;
        case 'ptr': {
          const off = n.off || 0;
          if (n.base === 'node') expr = temp(`(env.node ? { obj: env.node, off: ${off} } : 0)`);
          else if (n.base === 'this') expr = temp(`d.thisPointer(env, ${off})`);
          else if (n.base === '') expr = String(u32(off));
          else if (n.base === 'stack') expr = temp(`{ stack: ${off}, frame: env }`);
          else if (n.base.startsWith('new')) expr = temp(`{ obj: d.allocatedObject(env, ${JSON.stringify(n.base)}), off: ${off} }`);
          else if (n.base.startsWith('res')) expr = temp(`d.resultPointer(${Number(n.base.slice(3))}, ${off}, env)`);
          else if (n.base.startsWith('g')) expr = temp(`d.globalPointer(env, ${JSON.stringify(n.base)}, ${off})`);
          else if (n.base.startsWith('import:')) expr = String(u32(this.exportAddress(n.base.slice(7)) + off));
          else expr = temp(`{ ptr: ${JSON.stringify(n.base)}, off: ${off} }`);
          break;
        }
        case 'sym': {
          const name = n.name;
          if (name.startsWith('texture:')) expr = temp(`{ texture: ${JSON.stringify(name)} }`);
          else if (name.startsWith('arg')) expr = temp(`(env.args[${Number(name.slice(3))}] ?? 0)`);
          else if (name.startsWith('ctor.arg')) expr = temp(`(env.args[${Number(name.slice(8))}] ?? 0)`);
          else if (name.startsWith('reg.')) expr = temp(`(env.regs !== undefined ? (env.regs.${name.slice(4)} ?? 0) : 0)`);
          else if (name.startsWith('fpu.st')) expr = temp(`(env.regs !== undefined ? (env.fpu[${Number(name.slice(6))}] ?? 0) : 0)`);
          else if (name.startsWith('xmm')) expr = temp(`(env.regs !== undefined ? (env.xmm[${JSON.stringify(name)}] ?? 0) : 0)`);
          else if (name.startsWith('mm')) expr = temp(`(env.regs !== undefined ? (env.mm[${JSON.stringify(name)}] ?? 0) : 0)`);
          else if (name.startsWith('flags.')) expr = temp(`(env.regs !== undefined ? (env.flags.${name.slice(6)} ?? 0) : 0)`);
          else if (name.startsWith('fcmp.')) expr = temp(`(env.regs !== undefined && env.fcmp ? env.fcmp.${name.slice(5)} : 0)`);
          else if (name.startsWith('clobber.') || name.startsWith('fpu.') || name === 'ftol.hi' || name === 'fsw') expr = '0';
          else expr = temp(`d.unbound(${JSON.stringify(name)})`);
          break;
        }
        case 'load': {
          // A load read early in its block stands as the local holding it, or the error reading it raised
          const pre = preread === null ? undefined : preread.get(idx);
          if (pre !== undefined) { expr = temp(`${pre.e} === undefined ? ${pre.v} : F.rethrow(${pre.e})`); break; }
          const narrow = (code) => (n.size < 4 ? `(typeof ${code} === 'number' ? ${code} & ${n.size === 1 ? '0xFF' : '0xFFFF'} : ${code})` : code);
          const t = 't' + temps++;
          if (n.addr === undefined && n.size <= 4 && (n.base === 'stack' || n.base === 'this')) {
            // A stack slot or a field of this reads straight from the frame or the object
            const live = n.base === 'stack' ? `env.stack.load(${n.off || 0}, ${n.size}, 'stack')` : `d.loadThis(env, ${n.off || 0}, ${n.size})`;
            if (useEnvLoads) out.push(`var ${t} = env.loads.get(${idx});`, `if (${t} === undefined) { const l = ${live}; ${t} = ${narrow('l')}; } else if (${t} instanceof F.EarlyError) throw ${t}.err;`);
            else out.push(`var l${t} = ${live};`, `var ${t} = ${narrow('l' + t)};`);
            expr = t;
            break;
          }
          if (n.addr === undefined || n.size > 4) {
            if (useEnvLoads) { expr = temp(`d.loadNode(${idx}, env)`); break; }
            expr = temp(`d.evalLoad(N[${idx}], env)`);
            break;
          }
          // A load through a computed address reads raw or allocated memory in place, anything else through the model
          const inner = [];
          scopes.push(new Map());
          let a = emitNode(n.addr, inner);
          if (/^[-0-9]|^NaN$|^Infinity$/.test(a)) { const t2 = 't' + temps++; inner.push(`var ${t2} = ${a};`); a = t2; }
          scopes.pop();
          const live = `(${a} !== null && typeof ${a} === 'object' && ${a}.raw !== undefined) ? F.rawLoad(${a}.raw, ${a}.off || 0, ${n.size}) : ((${a} !== null && typeof ${a} === 'object' && ${a}.obj !== undefined && ${a}.obj.mem.flat === true ? ${a}.obj.mem.loadWritten(${a}.off || 0, ${n.size}) : undefined) ?? d.loadAddress(${a}, N[${idx}], env))`;
          if (useEnvLoads) out.push(`var ${t} = env.loads.get(${idx});`, `if (${t} === undefined) { ${inner.join(' ')} ${t} = ${live}; } else if (${t} instanceof F.EarlyError) throw ${t}.err;`);
          else out.push(`var ${t};`, `{ ${inner.join(' ')} ${t} = ${live}; }`);
          expr = t;
          break;
        }
        case 'bin': {
          let a = emitNode(n.a, out), b = emitNode(n.b, out);
          const op = n.op;
          if (FLOAT_BIN_OPS.has(op)) {
            const x = temp(scalarOf(a)), y = temp(scalarOf(b));
            switch (op) {
              case 'fadd': expr = temp(`${x} + ${y}`); break;
              case 'fsub': expr = temp(`${x} - ${y}`); break;
              case 'fmul': expr = temp(`${x} * ${y}`); break;
              case 'fdiv': expr = temp(`${x} / ${y}`); break;
              case 'fmin': expr = temp(`Math.min(${x}, ${y})`); break;
              case 'fmax': expr = temp(`Math.max(${x}, ${y})`); break;
              case 'fmod': expr = temp(`${x} % ${y}`); break;
              case 'fpow': expr = temp(`Math.pow(${x}, ${y})`); break;
              case 'fatan2': expr = temp(`Math.atan2(${x}, ${y})`); break;
              case 'frem': expr = temp(`${x} - ${y} * F.roundEven(${x} / ${y})`); break;
            }
            break;
          }
          if (op === 'add' || op === 'sub') {
            const sign = op === 'add' ? '+' : '-';
            if (/^[-0-9]|^NaN$|^Infinity$/.test(a) && /^[-0-9]|^NaN$|^Infinity$/.test(b)) { expr = temp(`(${a} ${sign} ${b}) >>> 0`); break; }
            a = ref(a); b = ref(b);
            expr = temp(`(typeof ${a} === 'number' && typeof ${b} === 'number') ? (${a} ${sign} ${b}) >>> 0 : ((typeof ${b} === 'number' && ${a} !== null && typeof ${a} === 'object' && ${a}.raw !== undefined) ? { raw: ${a}.raw, off: ((${a}.off || 0) ${sign} ${b}) | 0 } : ((typeof ${b} === 'number' && ${a} !== null && typeof ${a} === 'object' && ${a}.obj !== undefined) ? { obj: ${a}.obj, off: ((${a}.off || 0) ${sign} ${b}) | 0 } : d.pointerArith(${JSON.stringify(op)}, ${a}, ${b}, env)))`);
            break;
          }
          if (op === 'hilo') { const x = temp(intOf(a)), y = temp(intOf(b)); expr = temp(`(BigInt(${x}) << 32n) | BigInt(${y})`); break; }
          if (op in WIDE_DIV_OPS) { const y = temp(intOf(b)); expr = temp(`F.${WIDE_DIV_OPS[op].name}(${a}, ${y})`); break; }
          const x = temp(intOf(a)), y = temp(intOf(b));
          switch (op) {
            case 'mul': expr = temp(`Math.imul(${x}, ${y}) >>> 0`); break;
            case 'and': expr = temp(`(${x} & ${y}) >>> 0`); break;
            case 'or': expr = temp(`(${x} | ${y}) >>> 0`); break;
            case 'xor': expr = temp(`(${x} ^ ${y}) >>> 0`); break;
            case 'shl': expr = temp(`(${x} << (${y} & 31)) >>> 0`); break;
            case 'shr': expr = temp(`${x} >>> (${y} & 31)`); break;
            case 'sar': expr = temp(`((${x} | 0) >> (${y} & 31)) >>> 0`); break;
            case 'idiv': expr = temp(`Math.trunc((${x} | 0) / (${y} | 0)) >>> 0`); break;
            case 'idiv_rem': expr = temp(`((${x} | 0) % (${y} | 0)) >>> 0`); break;
            case 'udiv': expr = temp(`Math.trunc(${x} / ${y}) >>> 0`); break;
            case 'udiv_rem': expr = temp(`(${x} % ${y}) >>> 0`); break;
            case 'imulhi': expr = temp(`Number((BigInt(${x} | 0) * BigInt(${y} | 0)) >> 32n) >>> 0`); break;
            case 'mulhi': expr = temp(`Number((BigInt(${x}) * BigInt(${y})) >> 32n) >>> 0`); break;
            default: throw new Error(`unknown binary op ${op}`);
          }
          break;
        }
        case 'un': {
          const a = emitNode(n.a, out);
          switch (n.op) {
            case 'zx8': expr = temp(`${intOf(a)} & 0xFF`); break;
            case 'zx16': expr = temp(`${intOf(a)} & 0xFFFF`); break;
            case 'sx8': expr = temp(`((${intOf(a)} << 24) >> 24) >>> 0`); break;
            case 'sx16': expr = temp(`((${intOf(a)} << 16) >> 16) >>> 0`); break;
            case 'neg': expr = temp(`(-${intOf(a)}) >>> 0`); break;
            case 'not': expr = temp(`(~${intOf(a)}) >>> 0`); break;
            case 'i2f': expr = temp(`${intOf(a)} | 0`); break;
            case 'bits2f': expr = temp(`d.bits2f(${a})`); break;
            case 'bits2d': expr = temp(scalarOf(a)); break;
            case 'f32': expr = temp(`F.f32(${scalarOf(a)})`); break;
            case 'f64': expr = temp(scalarOf(a)); break;
            case 'f2i': expr = temp(`Math.trunc(${scalarOf(a)}) >>> 0`); break;
            case 'f2i_rn': expr = temp(`F.roundEven(${scalarOf(a)}) >>> 0`); break;
            case 'f2bits': expr = temp(`d.f2bits(${a})`); break;
            case 'fneg': expr = temp(`-${scalarOf(a)}`); break;
            case 'fabs': expr = temp(`Math.abs(${scalarOf(a)})`); break;
            case 'fsin': expr = temp(`Math.sin(${scalarOf(a)})`); break;
            case 'fcos': expr = temp(`Math.cos(${scalarOf(a)})`); break;
            case 'fsqrt': expr = temp(`Math.sqrt(${scalarOf(a)})`); break;
            case 'ftan': expr = temp(`Math.tan(${scalarOf(a)})`); break;
            case 'ffloor': expr = temp(`Math.floor(${scalarOf(a)})`); break;
            case 'fceil': expr = temp(`Math.ceil(${scalarOf(a)})`); break;
            case 'fexp': expr = temp(`Math.exp(${scalarOf(a)})`); break;
            case 'flog': expr = temp(`Math.log(${scalarOf(a)})`); break;
            case 'flog10': expr = temp(`Math.log10(${scalarOf(a)})`); break;
            case 'flog2': expr = temp(`Math.log2(${scalarOf(a)})`); break;
            case 'ftrunc': expr = temp(`Math.trunc(${scalarOf(a)})`); break;
            case 'fround': expr = temp(`F.roundEven(${scalarOf(a)})`); break;
            case 'fatan': expr = temp(`Math.atan(${scalarOf(a)})`); break;
            case 'fasin': expr = temp(`Math.asin(${scalarOf(a)})`); break;
            case 'facos': expr = temp(`Math.acos(${scalarOf(a)})`); break;
            case 'fsinh': expr = temp(`Math.sinh(${scalarOf(a)})`); break;
            case 'fcosh': expr = temp(`Math.cosh(${scalarOf(a)})`); break;
            case 'ftanh': expr = temp(`Math.tanh(${scalarOf(a)})`); break;
            case 'bsf': expr = temp(`F.bsf(${intOf(a)})`); break;
            case 'bsr': expr = temp(`F.bsr(${intOf(a)})`); break;
            default: throw new Error(`unknown unary op ${n.op}`);
          }
          break;
        }
        case 'cond': {
          const a = emitNode(n.a, out), b = emitNode(n.b, out);
          let fast;
          switch (n.op) {
            case 'eq': fast = `(${a} >>> 0) === (${b} >>> 0)`; break;
            case 'ne': fast = `(${a} >>> 0) !== (${b} >>> 0)`; break;
            case 'lt': fast = `(${a} | 0) < (${b} | 0)`; break;
            case 'le': fast = `(${a} | 0) <= (${b} | 0)`; break;
            case 'gt': fast = `(${a} | 0) > (${b} | 0)`; break;
            case 'ge': fast = `(${a} | 0) >= (${b} | 0)`; break;
            case 'ult': fast = `(${a} >>> 0) < (${b} >>> 0)`; break;
            case 'ule': fast = `(${a} >>> 0) <= (${b} >>> 0)`; break;
            case 'ugt': fast = `(${a} >>> 0) > (${b} >>> 0)`; break;
            case 'uge': fast = `(${a} >>> 0) >= (${b} >>> 0)`; break;
            default: throw new Error(`unknown condition ${n.op}`);
          }
          expr = temp(`(typeof ${a} === 'number' && typeof ${b} === 'number') ? (${fast} ? 1 : 0) : (d.compare(${JSON.stringify(n.op)}, ${a}, ${b}) ? 1 : 0)`);
          break;
        }
        case 'fcmp': {
          const a = emitNode(n.a, out), b = emitNode(n.b, out);
          const x = temp(scalarOf(a)), y = temp(scalarOf(b));
          expr = temp(`((((${x} !== ${x} || ${y} !== ${y}) ? 8 : (${x} < ${y} ? 1 : (${x} === ${y} ? 2 : 4))) & ${n.set}) !== 0 ? 1 : 0)`);
          break;
        }
        case 'not': {
          const a = emitNode(n.a, out);
          expr = temp(`(typeof ${a} === 'number' ? ${a} !== 0 : !!${a}) ? 0 : 1`);
          break;
        }
        case 'call': expr = temp(`d.evalCall(N[${idx}], env)`); break;
        case 'block': {
          const elems = n.elems.map((e) => emitNode(e, out));
          expr = temp(`d.blockOf([${elems.join(', ')}])`);
          break;
        }
        case 'elem': {
          const a = emitNode(n.a, out);
          expr = temp(`d.vector(${a}, 64, env)[${n.i}]`);
          break;
        }
        case 'sel': {
          const c = emitNode(n.c, out);
          const t = 't' + temps++;
          out.push(`var ${t};`);
          const taken = [], other = [];
          scopes.push(new Map());
          const va = emitNode(n.a, taken);
          scopes.pop();
          scopes.push(new Map());
          const vb = emitNode(n.b, other);
          scopes.pop();
          out.push(`if (typeof ${c} === 'number' ? ${c} !== 0 : !!${c}) { ${taken.join(' ')} ${t} = ${va}; } else { ${other.join(' ')} ${t} = ${vb}; }`);
          expr = t;
          break;
        }
        case 'retaddr': case 'undef': expr = '0'; break;
        default: throw new Error(`unknown expression kind ${n.k}`);
      }
      if (share) scopes[scopes.length - 1].set(idx, expr);
      return expr;
    };
    // Emits the statements evaluating a root expression into a statement list and returns the expression of its value,
    // a node used twice evaluating once since a load after a call carries its own epoch and a call runs once per sequence
    const emit = (root, out) => {
      share = true;
      scopes = [new Map()];
      return emitNode(root, out);
    };
    // Emits a root sharing the subexpressions every root emitted since the scope began, which run in one function
    const emitShared = (root, out) => {
      share = true;
      return emitNode(root, out);
    };
    // Begins the scope of one function, past which nothing emitted before is shared
    const newScope = () => { scopes = [new Map()]; };
    // Sets the loads a block has read early, by pool index, to the locals holding them
    const setPreread = (map) => { preread = map; };
    return { emit, emitShared, newScope, hasCall, setPreread, nextTemp: () => 't' + temps++ };
  }

  // Compiles a lifted routine into one function running its blocks, each block's effects, register results and
  // ending as straight statements, the machine state staying in the environment the expressions read
  compileLifted(va, fn) {
    const emitter = this.exprEmitter(false);
    const blocks = Object.keys(fn.blocks);
    const effectTables = [];
    const lines = [];
    lines.push(`let key = ${JSON.stringify(fn.entry)}, steps = 0;`);
    lines.push(`const regs = env.regs;`);
    lines.push(`try { for (;;) {`);
    lines.push(`if (++steps >= ${LIFTED_BLOCK_LIMIT} || ++d.work > ${FRAME_WORK_BUDGET}) d.liftedOverrun(${va}, key, steps);`);
    lines.push(`switch (key) {`);
    for (const key of blocks) {
      const b = fn.blocks[key];
      if (!b._marked) this.annotateBlock(b);
      const table = effectTables.length;
      effectTables.push(b.effects);
      // A block making no call keeps its early loads in locals alone, one with calls records them for the
      // expressions the calls evaluate as well
      const exprs = [];
      for (const e of b.effects) { for (const x of effectExprs(e)) exprs.push(x); }
      for (const idx of Object.values(b.regs)) exprs.push(idx);
      if (b.esp !== undefined) exprs.push(b.esp);
      for (const idx of b.fpu) exprs.push(idx);
      for (const idx of Object.values(b.xmm || {})) exprs.push(idx);
      for (const idx of Object.values(b.mm || {})) exprs.push(idx);
      for (const idx of Object.values(b.flags || {})) if (idx >= 0) exprs.push(idx);
      if (b.fcmp) exprs.push(b.fcmp.a, b.fcmp.b);
      for (const k of ['cond', 'index', 'value']) if (b.term[k] !== undefined) exprs.push(b.term[k]);
      const calls = b.effects.some((e) => e.kind === 'call') || exprs.some((idx) => emitter.hasCall(idx));
      const preread = new Map();
      emitter.setPreread(preread);
      lines.push(`case ${JSON.stringify(key)}: {`);
      if (calls) lines.push(`env.results.clear(); env.loads.clear();`);
      const typed = (v, size, floatTyped) => {
        if (size === 8) return `if (typeof ${v} === 'number') ${v} = { f64: ${v} }; else if (${v} !== null && typeof ${v} === 'object' && ${v}.bits !== undefined) ${v} = { f64: F.floatFromBits(${v}.bits) };`;
        if (floatTyped) return `if (typeof ${v} === 'number') ${v} = { bits: F.bitsFromFloat(${v}) };`;
        return `if (typeof ${v} === 'number' && !Number.isInteger(${v})) ${v} = { bits: F.bitsFromFloat(${v}) };`;
      };
      b.effects.forEach((e, k) => {
        if (e._early) {
          for (const idx of e._early) {
            if (preread.has(idx)) continue;
            const body = [];
            const value = emitter.emit(idx, body);
            const v = emitter.nextTemp(), err = emitter.nextTemp();
            lines.push(`let ${v}, ${err};`);
            lines.push(`try { ${body.join(' ')} ${v} = ${value} instanceof Float32Array ? ${value}.slice() : ${value}; } catch (e) { ${err} = e; }`);
            if (calls) lines.push(`env.loads.set(${idx}, ${err} === undefined ? ${v} : new F.EarlyError(${err}));`);
            preread.set(idx, { v, e: err });
          }
        }
        if (e.kind === 'store' && e.has_addr && e.size <= 4) {
          const body = [];
          const v = emitter.emit(e.value, body);
          lines.push(`{ ${body.join(' ')} let v = ${v};`);
          lines.push(typed('v', e.size, this.isFloatExpr(e.value)));
          const addr = [];
          const a = emitter.emit(e.addr, addr);
          lines.push(`${addr.join(' ')} const a = ${a};`);
          lines.push(`if (typeof v === 'number' && a !== null && typeof a === 'object' && a.raw !== undefined) F.rawStore(a.raw, a.off || 0, ${e.size}, v);`);
          lines.push(`else if (typeof v === 'number' && a !== null && typeof a === 'object' && a.obj !== undefined && a.obj.mem.flat === true) a.obj.mem.setWord(a.off || 0, ${e.size}, v);`);
          lines.push(`else d.storeThrough(a, ${e.size}, v, env); }`);
        } else if (e.kind === 'store' && !e.has_addr && e.base === 'stack') {
          const body = [];
          const v = emitter.emit(e.value, body);
          lines.push(`{ ${body.join(' ')} let v = ${v};`);
          lines.push(typed('v', e.size, this.isFloatExpr(e.value)));
          lines.push(`env.stack.store(${e.off || 0}, ${e.size}, v); }`);
        } else if (e.kind === 'copy' || e.kind === 'fill') {
          lines.push(`try { d.runOfElements(E[${table}][${k}], env); } catch (err) { d.effectError(err, E[${table}][${k}]); }`);
        } else {
          lines.push(`d.applyEffectAt(E[${table}][${k}], env, state);`);
        }
      });
      const assigns = [];
      const after = [];
      for (const [r, idx] of Object.entries(b.regs)) {
        const body = [];
        const v = emitter.emit(idx, body);
        lines.push(...body);
        const t = `n${r}`;
        lines.push(`const ${t} = ${v};`);
        assigns.push(`regs.${r} = ${t};`);
      }
      const fpu = b.fpu.map((idx, i) => { const body = []; const v = emitter.emit(idx, body); lines.push(...body); lines.push(`const nf${i} = ${v};`); return `nf${i}`; });
      after.push(`env.fpu = [${fpu.join(', ')}];`);
      let xi = 0;
      for (const [name, idx] of Object.entries(b.xmm || {})) { const body = []; const v = emitter.emit(idx, body); lines.push(...body); lines.push(`const nx${xi} = ${v};`); after.push(`env.xmm[${JSON.stringify(name)}] = nx${xi};`); xi++; }
      let mi = 0;
      for (const [name, idx] of Object.entries(b.mm || {})) { const body = []; const v = emitter.emit(idx, body); lines.push(...body); lines.push(`const nm${mi} = ${v};`); after.push(`env.mm[${JSON.stringify(name)}] = nm${mi};`); mi++; }
      const flagParts = [];
      for (const [name, idx] of Object.entries(b.flags || {})) { if (typeof idx !== 'number') { flagParts.push(`${name}: ${JSON.stringify(idx)}`); continue; } if (idx < 0) continue; const body = []; const v = emitter.emit(idx, body); lines.push(...body); lines.push(`const nfl_${name} = ${v};`); flagParts.push(`${name}: nfl_${name}`); }
      after.push(`env.flags = { ${flagParts.join(', ')} };`);
      if (b.fcmp) {
        const ba = [], bb = [];
        const fa = emitter.emit(b.fcmp.a, ba), fb = emitter.emit(b.fcmp.b, bb);
        lines.push(...ba, `const nfca = ${fa};`, ...bb, `const nfcb = ${fb};`);
        after.push(`env.fcmp = { a: nfca, b: nfcb };`);
      } else after.push(`env.fcmp = null;`);
      const t = b.term;
      let to;
      switch (t.kind) {
        case 'return': { const body = []; const v = emitter.emit(t.value, body); lines.push(...body, `state.returned = ${v}; return;`); break; }
        case 'trap': lines.push(`throw new Error('the executable traps here');`); break;
        case 'noreturn': lines.push(`throw new Error('the routine called last in this block never returns');`); break;
        case 'halt': lines.push(`state.returned = 0; return;`); break;
        case 'jump': to = JSON.stringify(t.target); break;
        case 'branch': { const body = []; const v = emitter.emit(t.cond, body); lines.push(...body, `const cond = ${v};`); to = `((typeof cond === 'number' ? cond !== 0 : !!cond) ? ${JSON.stringify(t.taken)} : ${JSON.stringify(t.fall)})`; break; }
        case 'switch': { const body = []; const v = emitter.emit(t.index, body); lines.push(...body, `const index = d.int(${v});`, `if (index >= ${t.targets.length}) throw new Error('switch index ' + index + ' outside its ${t.targets.length} cases');`); to = `T[${table}][index]`; break; }
        default: throw new Error(`block ends with ${t.kind}`);
      }
      if (to !== undefined) {
        lines.push(`const to = ${to};`);
        if (b.esp !== undefined) {
          const body = [];
          const v = emitter.emit(b.esp, body);
          lines.push(...body, `const nESP = ${v};`, ...assigns, `regs.ESP = nESP;`, ...after);
        } else lines.push(...assigns, `regs.ESP = { stack: ${b.sp}, frame: env };`, ...after);
        lines.push(`if (trace) d.traceBlock(${va}, key, to, env);`);
        lines.push(`key = to; break;`);
      }
      lines.push(`}`);
    }
    lines.push(`default: throw new Error('no block ' + key);`);
    lines.push(`} } } catch (err) { if (!err.block) { err.block = key; err.message = 'block ' + key + ': ' + err.message; } throw err; }`);
    let size = 0;
    for (const l of lines) size += l.length;
    if (size > CODE_BUDGET) { fn._compiled = INTERPRETED; return INTERPRETED; }
    const targets = blocks.map((key) => fn.blocks[key].term.targets || null);
    const make = new Function('d', 'N', 'F', 'E', 'T', 'trace', `return function lifted${hex8(va)}(env, state) {\n${lines.join('\n')}\n};`);
    const compiled = make(this, this.nodes, EXPR_HELPERS, effectTables, targets, this.traced.has('blocks:' + hex8(va)));
    fn._compiled = compiled;
    return compiled;
  }

  // Reports a lifted routine that ran past its block or work budget
  liftedOverrun(va, key, steps) {
    if (steps >= LIFTED_BLOCK_LIMIT) throw new Error(`ran ${steps} blocks without returning`);
    throw new Error(`the frame ran past ${FRAME_WORK_BUDGET} steps in ${hex8(va)} at block ${key} after ${steps} blocks of its own`);
  }

  traceBlock(va, key, to, env) {
    console.log(`[block] ${hex8(va)} ${key} -> ${to} regs ${Object.entries(env.regs).map(([r, x]) => r + '=' + describe(x)).join(' ')}`);
  }

  // Reads through an evaluated address the way a load expression does, narrowing to the bytes loaded
  loadAddress(addr, n, env) {
    let v;
    if (addr && (addr.matrixStack !== undefined || addr.vb !== undefined || addr.ib !== undefined || addr.d3dxMesh !== undefined)) v = addr;
    else if (addr && typeof addr.ptr === 'string' && addr.ptr.startsWith('import:')) v = this.memory.load(u32(this.exportAddress(addr.ptr.slice(7)) + (addr.off || 0)), n.size);
    else if (typeof addr === 'number' || (addr && (addr.obj !== undefined || addr.raw !== undefined || addr.stack !== undefined || addr.res !== undefined || addr.str !== undefined))) v = this.loadLive(env, addr, n.size);
    else throw new Error(`load through ${describe(addr)} from ${this.show(n.addr)}`);
    if (typeof v === 'number' && n.size < 4) return v & (n.size === 1 ? 0xFF : 0xFFFF);
    return v;
  }

  // The value an unbound symbol would take, which no run supplies
  unbound(name) {
    throw new Error(`unbound symbol ${name}`);
  }

  // A load as of the effect that read it early, else as the memory stands now
  loadNode(idx, env) {
    const early = env.loads.get(idx);
    if (early === undefined) return this.evalLoad(this.nodes[idx], env);
    if (early instanceof EarlyError) throw early.err;
    return early;
  }

  // A pointer into the result of a call, the object or the address it handed back when it did
  resultPointer(seq, off, env) {
    const r = env.results.get(seq);
    if (r && r.obj !== undefined) return { obj: r.obj, off: (r.off || 0) + off };
    if (typeof r === 'number') return u32(r + off);
    return { res: seq, off };
  }

  // The integer of a value that may point into a call's numeric result
  intOf(v, env) {
    return this.int(this.resultNumber(v, env));
  }

  // A pointer reinterpreted as float bits and back is the same word, so the handle passes through
  bits2f(v) {
    return isHandle(v) || (v && v.sum !== undefined) ? v : floatFromBits(this.int(v));
  }

  f2bits(v) {
    return isHandle(v) || (v && v.sum !== undefined) ? v : bitsFromFloat(this.scalar(v));
  }

  // Adds or subtracts values of which at least one is no plain number
  pointerArith(op, av, bv, env) {
    if (typeof bv === 'number' && av !== null && typeof av === 'object') {
      const moved = this.movedPointer(av, op === 'add' ? bv : -bv, env);
      if (moved !== undefined) return moved;
    } else if (op === 'add' && typeof av === 'number' && bv !== null && typeof bv === 'object') {
      const moved = this.movedPointer(bv, av, env);
      if (moved !== undefined) return moved;
    }
    if (this.pointerTerms(av, env) || this.pointerTerms(bv, env)) return this.pointerSum(op, av, bv, env);
    const a = this.int(this.resultNumber(av, env)), b = this.int(this.resultNumber(bv, env));
    return op === 'add' ? u32(a + b) : u32(a - b);
  }

  // A float block from the values its elements evaluate to
  blockOf(elems) {
    return Float32Array.from(elems.flatMap((e) => this.floats(e)));
  }

  // Evaluates a pooled expression node by interpretation, the compiled form standing on this
  evalNode(idx, env) {
    const n = this.nodes[idx];
    switch (n.k) {
      case 'i': return u32(n.v);
      case 'f': return n.v !== undefined ? n.v : (n.s === 'NaN' ? NaN : (n.s === '+Inf' ? Infinity : -Infinity));
      case 'ptr':
        if (n.base === 'node') return env.node ? { obj: env.node, off: n.off } : 0;
        if (n.base === 'this') return this.thisPointer(env, n.off);
        if (n.base === '') return u32(n.off);
        if (n.base === 'stack') return { stack: n.off, frame: env };
        if (n.base.startsWith('new')) return { obj: this.allocatedObject(env, n.base), off: n.off };
        if (n.base.startsWith('res')) {
          const r = env.results.get(Number(n.base.slice(3)));
          if (r && r.obj !== undefined) return { obj: r.obj, off: (r.off || 0) + n.off };
          return { res: Number(n.base.slice(3)), off: n.off };
        }
        if (n.base.startsWith('g')) return this.globalPointer(env, n.base, n.off);
        if (n.base.startsWith('import:')) return u32(this.exportAddress(n.base.slice(7)) + n.off);
        return { ptr: n.base, off: n.off };
      case 'sym':
        if (n.name.startsWith('texture:')) return { texture: n.name };
        if (n.name.startsWith('arg')) return env.args[Number(n.name.slice(3))] ?? 0;
        if (n.name.startsWith('ctor.arg')) return env.args[Number(n.name.slice(8))] ?? 0;
        if (env.regs !== undefined) {
          // A lifted block reads the machine state its predecessor left
          if (n.name.startsWith('reg.')) return env.regs[n.name.slice(4)] ?? 0;
          if (n.name.startsWith('fpu.st')) return env.fpu[Number(n.name.slice(6))] ?? 0;
          if (n.name.startsWith('xmm')) return env.xmm[n.name] ?? 0;
          if (n.name.startsWith('mm')) return env.mm[n.name] ?? 0;
          if (n.name.startsWith('flags.')) return env.flags[n.name.slice(6)] ?? 0;
          if (n.name.startsWith('fcmp.')) return env.fcmp ? env.fcmp[n.name.slice(5)] : 0;
        }
        if (n.name.startsWith('reg.') || n.name.startsWith('clobber.') || n.name.startsWith('fpu.') || n.name.startsWith('xmm') || n.name.startsWith('mm') || n.name === 'ftol.hi' || n.name === 'fsw') return 0;
        throw new Error(`unbound symbol ${n.name}`);
      case 'load': {
        const early = env.loads.get(idx);
        if (early === undefined) return this.evalLoad(n, env);
        if (early instanceof EarlyError) throw early.err;
        return early;
      }
      case 'bin': return this.evalBin(n, env);
      case 'un': return this.evalUn(n, env);
      case 'cond': return this.evalCond(n, env) ? 1 : 0;
      case 'fcmp': return this.evalFcmp(n, env) ? 1 : 0;
      case 'not': return this.truthy(this.eval(n.a, env)) ? 0 : 1;
      case 'call': return this.evalCall(n, env);
      case 'block': return Float32Array.from(n.elems.flatMap((e) => this.floats(this.eval(e, env))));
      case 'elem': return this.vector(this.eval(n.a, env), 64, env)[n.i];
      case 'sel': return this.truthy(this.eval(n.c, env)) ? this.eval(n.a, env) : this.eval(n.b, env);
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

  // Arguments as a routine of the executable receives them, a single precision value tagged with its bits
  bitsArgs(args, argIdx) {
    if (!argIdx) return args;
    return args.map((v, i) => (typeof v === 'number' && argIdx[i] !== undefined && this.isFloatExpr(argIdx[i]) ? { bits: bitsFromFloat(v) } : v));
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
    if (v && v.sum !== undefined) return u32([...v.sum.values()].reduce((n, t) => n + t.coef * this.int(basePointer(t.base, 0)), v.off));
    if (v && v.raw !== undefined) return u32(0x40000000 + v.raw.id * 0x100000 + (v.off || 0));
    if (v && v.vb !== undefined) return u32(0x40000000 + v.vb.id * 0x100000);
    if (v && v.ib !== undefined) return u32(0x40000000 + v.ib.id * 0x100000);
    if (v && v.d3dxMesh !== undefined) return u32(0x40000000 + v.d3dxMesh.vb.id * 0x100000 + 0x80000);
    // A float block read as a word is the bits of its first element, the way the executable copies it
    if (v instanceof Float32Array) return bitsFromFloat(v[0]);
    if (v && (v.ptr !== undefined || v.texture !== undefined || v.program !== undefined || v.str !== undefined || v.material !== undefined || v.vertices !== undefined || v.indices !== undefined || v.xapTexture !== undefined || v.stack !== undefined || v.matrixStack !== undefined || v.res !== undefined)) return 1;
    if (v && v.call !== undefined) return 0;
    throw new Error(`expected an integer, got ${describe(v)}`);
  }

  evalLoad(n, env) {
    const v = this.evalLoadAt(n, env);
    // A narrow load reads only the bits the executable loaded
    if (typeof v === 'number' && n.size < 4) return v & (n.size === 1 ? 0xFF : 0xFFFF);
    return v;
  }

  evalLoadAt(n, env) {
    if (n.addr !== undefined) {
      const addr = this.eval(n.addr, env);
      if (addr && (addr.matrixStack !== undefined || addr.vb !== undefined || addr.ib !== undefined || addr.d3dxMesh !== undefined)) return addr;
      if (addr && typeof addr.ptr === 'string' && addr.ptr.startsWith('import:')) return this.memory.load(u32(this.exportAddress(addr.ptr.slice(7)) + (addr.off || 0)), n.size);
      if (typeof addr === 'number' || (addr && (addr.obj !== undefined || addr.raw !== undefined || addr.stack !== undefined || addr.res !== undefined || addr.str !== undefined))) return this.loadLive(env, addr, n.size);
      throw new Error(`load through ${describe(addr)} from ${this.show(n.addr)}`);
    }
    const off = n.off || 0;
    if (n.base === '') return this.memory.load(off, n.size);
    if (n.base === 'this') return this.loadThis(env, off, n.size);
    if (n.base === 'node') {
      if (!env.node) throw new Error('load from node without an object');
      return this.readField(env.node, off, n.size);
    }
    if (n.base === 'stack') return this.stackLoad(env, off, n.size);
    if (n.base === 'fs') return this.fs.load(off, n.size, 'fs');
    if (n.base.startsWith('new')) return this.readField(this.allocatedObject(env, n.base), off, n.size);
    if (n.base.startsWith('res')) return this.bufferLoad(env, Number(n.base.slice(3)), off, n.size);
    if (n.base.startsWith('import:')) return this.memory.load(u32(this.exportAddress(n.base.slice(7)) + off), n.size);
    if (n.base.startsWith('g')) {
      const p = this.globalPointer(env, n.base, off);
      if (p && (p.matrixStack !== undefined || p.vb !== undefined || p.ib !== undefined || p.d3dxMesh !== undefined)) return p;
      return this.loadLive(env, p, n.size);
    }
    if (env.mat && n.base === env.mat.object) return this.fieldInt(env.mat, off, n.size);
    throw new Error(`load from ${n.base}`);
  }

  // The address a number of bytes past a pointer, whatever kind of memory it names
  offsetPointer(p, delta) {
    if (typeof p === 'number') return u32(p + delta);
    if (p && p.obj !== undefined) return { obj: p.obj, off: (p.off || 0) + delta };
    if (p && p.stack !== undefined) return { stack: p.stack + delta, frame: p.frame };
    if (p && p.res !== undefined) return { res: p.res, off: (p.off || 0) + delta };
    if (p && p.str !== undefined) return { str: p.str, chars: (p.chars || 0) + delta / 2 };
    if (p && p.raw !== undefined) return { raw: p.raw, off: (p.off || 0) + delta };
    throw new Error(`no address ${delta} bytes past ${describe(p)}`);
  }

  // Exchanges two runs of bytes, which is how the library sort moves its elements
  swapBytes(a, b, size, env) {
    for (let i = 0; i < size; i++) {
      const pa = this.offsetPointer(a, i), pb = this.offsetPointer(b, i);
      const va = this.loadLive(env, pa, 1), vb = this.loadLive(env, pb, 1);
      this.storeThrough(pa, 1, vb, env);
      this.storeThrough(pb, 1, va, env);
    }
  }

  // A block of allocated memory, bounded by the byte size the allocation asked for
  heapObject(size) {
    const obj = new SceneObject(HEAP_TYPE, null);
    if (typeof size === 'number') { obj.size = size; obj.mem = new FlatStore(size); }
    return { obj, off: 0 };
  }

  // An object a constructor allocated besides the one it builds, the same one for the whole run that made it
  allocatedObject(env, base) {
    if (!env.allocated) env.allocated = new Map();
    if (!env.allocated.has(base)) env.allocated.set(base, new SceneObject(HEAP_TYPE, null));
    return env.allocated.get(base);
  }

  // The object a typed global pointer names: the handle the global held as of the load, offset into it
  globalPointer(env, base, off) {
    const addr = parseInt(base.slice(1), 16);
    const v = this.memory.load(addr, 4);
    if (typeof v === 'number') return u32(v + off);
    if (v && v.obj !== undefined) return { obj: v.obj, off: (v.off || 0) + off };
    if (v && v.res !== undefined) return { res: v.res, off: (v.off || 0) + off };
    if (v && v.stack !== undefined) return { stack: v.stack + off, frame: v.frame };
    if (v && v.raw !== undefined) return { raw: v.raw, off: (v.off || 0) + off };
    if (off === 0 && v && typeof v === 'object') return v;
    throw new Error(`global 0x${addr.toString(16)} holds ${describe(v)} which cannot be offset by ${off}`);
  }

  // Reads a word or a float block from the stack frame of the running function, memory it never wrote reading as zero
  stackLoad(env, off, size) {
    return env.stack.load(off, size, 'stack');
  }

  // Reads from the memory a lock handed back or an allocation returned
  bufferLoad(env, seq, off, size) {
    const r = env.results.get(seq);
    if (r && r.obj !== undefined) return this.readField(r.obj, (r.off || 0) + off, size);
    // A call handing back an address is read through, the memory it names being the image's own
    if (typeof r === 'number') return this.memory.load(u32(r + off), size);
    if (r && r.raw !== undefined) return rawLoad(r.raw, (r.off || 0) + off, size);
    throw new Error(`load from the result of call ${seq} which is ${describe(r)} rather than memory`);
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
        case 'frem': return a - b * roundEven(a / b);
      }
    }
    const av = this.eval(n.a, env), bv = this.eval(n.b, env);
    if (op === 'add' || op === 'sub') {
      // A pointer moved by a number keeps its base, the term algebra being for sums of pointers
      if (typeof bv === 'number' && av !== null && typeof av === 'object') {
        const moved = this.movedPointer(av, op === 'add' ? bv : -bv, env);
        if (moved !== undefined) return moved;
      } else if (op === 'add' && typeof av === 'number' && bv !== null && typeof bv === 'object') {
        const moved = this.movedPointer(bv, av, env);
        if (moved !== undefined) return moved;
      }
      if (this.pointerTerms(av, env) || this.pointerTerms(bv, env)) return this.pointerSum(op, av, bv, env);
    }
    if (op in WIDE_DIV_OPS) {
      if (typeof av !== 'bigint') throw new Error(`${op} of a dividend that is no hilo pair`);
      return WIDE_DIV_OPS[op](av, this.int(this.resultNumber(bv, env)));
    }
    const a = this.int(this.resultNumber(av, env)), b = this.int(this.resultNumber(bv, env));
    if (op === 'hilo') return (BigInt(a) << 32n) | BigInt(b);
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

  // A simple pointer moved by a number of bytes, or undefined where the value is no such pointer
  movedPointer(v, delta, env) {
    if (v.obj !== undefined) return { obj: v.obj, off: ((v.off || 0) + delta) | 0 };
    if (v.stack !== undefined) return { stack: (v.stack + delta) | 0, frame: v.frame };
    if (v.raw !== undefined) return { raw: v.raw, off: ((v.off || 0) + delta) | 0 };
    if (v.res !== undefined && typeof env.results.get(v.res) !== 'number') return { res: v.res, off: ((v.off || 0) + delta) | 0 };
    return undefined;
  }

  // The memory bases a pointer adds up, each with its multiplicity, and its offset, or null for a plain number
  pointerTerms(v, env) {
    if (!v || typeof v !== 'object') return null;
    if (v.sum !== undefined) return { terms: new Map([...v.sum].map(([k, t]) => [k, { ...t }])), off: v.off };
    if (v.obj !== undefined) return { terms: new Map([[v.obj, { coef: 1, base: { obj: v.obj } }]]), off: v.off || 0 };
    if (v.stack !== undefined) return { terms: new Map([[v.frame || env, { coef: 1, base: { stack: 0, frame: v.frame } }]]), off: v.stack };
    if (v.res !== undefined && typeof env.results.get(v.res) !== 'number') return { terms: new Map([['res' + v.res, { coef: 1, base: { res: v.res } }]]), off: v.off || 0 };
    if (v.raw !== undefined) return { terms: new Map([[v.raw, { coef: 1, base: { raw: v.raw } }]]), off: v.off || 0 };
    // A string value stands for the memory holding its characters, a pointer into it counting code units
    if (v.str !== undefined) return { terms: new Map([['str:' + v.str, { coef: 1, base: { str: v.str } }]]), off: 2 * (v.chars || 0) };
    return null;
  }

  // Adds or subtracts values naming memory, a base subtracted out cancelling once it is added back
  pointerSum(op, av, bv, env) {
    const terms = new Map();
    let off = 0;
    for (const [v, s] of [[av, 1], [bv, op === 'add' ? 1 : -1]]) {
      const p = this.pointerTerms(v, env);
      if (!p) { off += s * this.int(this.resultNumber(v, env)); continue; }
      off += s * p.off;
      for (const [k, t] of p.terms) {
        const e = terms.get(k);
        if (e) e.coef += s * t.coef; else terms.set(k, { coef: s * t.coef, base: t.base });
      }
    }
    for (const [k, t] of terms) if (t.coef === 0) terms.delete(k);
    if (terms.size === 0) return u32(off);
    if (terms.size === 1) {
      const [t] = terms.values();
      if (t.coef === 1) return basePointer(t.base, off | 0);
    }
    return { sum: terms, off: off | 0 };
  }

  // A pointer into the result of a call that returned a number is that number offset, as the arithmetic reads it
  resultNumber(v, env) {
    if (!v || v.res === undefined) return v;
    const r = env.results.get(v.res);
    return typeof r === 'number' ? u32(r + (v.off || 0)) : v;
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
      // A pointer reinterpreted as float bits and back is the same word, so the handle passes through
      case 'bits2f': return isHandle(v) || (v && v.sum !== undefined) ? v : floatFromBits(this.int(v));
      case 'bits2d': return this.scalar(v);
      case 'f32': return f32(this.scalar(v));
      case 'f64': return this.scalar(v);
      case 'f2i': return u32(Math.trunc(this.scalar(v)));
      case 'f2i_rn': return u32(roundEven(this.scalar(v)));
      case 'f2bits': return isHandle(v) || (v && v.sum !== undefined) ? v : bitsFromFloat(this.scalar(v));
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
      case 'bsf': { const b = this.int(v); return b === 0 ? 0 : 31 - Math.clz32(b & -b); }
      case 'bsr': { const b = this.int(v); return b === 0 ? 0 : 31 - Math.clz32(b); }
      case 'flog2': return Math.log2(this.scalar(v));
      case 'ftrunc': return Math.trunc(this.scalar(v));
      case 'fround': return roundEven(this.scalar(v));
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
    return this.compare(n.op, this.eval(n.a, env), this.eval(n.b, env));
  }

  // Compares two evaluated values the way the executable's flags would decide
  compare(op, av, bv) {
    const n = { op };
    // Handles compare equal only to themselves, and never to a number other than zero.
    if (isHandle(av) || isHandle(bv)) {
      const same = isHandle(av) && isHandle(bv) ? sameHandle(av, bv) : false;
      const zero = (isHandle(av) && typeof bv === 'number' && bv === 0) || (isHandle(bv) && typeof av === 'number' && av === 0);
      if (n.op === 'eq') return same;
      if (n.op === 'ne') return !same;
      if (zero) { const eq = false; switch (n.op) { case 'lt': case 'le': return isHandle(bv) ? true : eq; case 'gt': case 'ge': return isHandle(av) ? true : eq; } }
    }
    // A block stands for the memory holding it, which is never the null pointer
    if ((av instanceof Float32Array && bv === 0) || (bv instanceof Float32Array && av === 0)) {
      if (n.op === 'eq') return false;
      if (n.op === 'ne') return true;
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
    const v = this.performCall(n.fn, n.args || [], n.this, env, n.seq, n.target);
    if (n.seq !== undefined) env.results.set(n.seq, v);
    return v;
  }

  // Executes a call: accessors, derived helpers, node methods, math and device calls
  performCall(fn, argIdx, thisIdx, env, seq, target) {
    if (fn.startsWith('vcall@')) {
      let slot = this.helperVA.get(fn);
      if (slot === undefined) { slot = parseInt(fn.slice(6), 16); this.helperVA.set(fn, slot); }
      const args = new Array(argIdx.length);
      let stackArg, meshArg;
      for (let i = 0; i < argIdx.length; i++) {
        const a = this.eval(argIdx[i], env);
        args[i] = a;
        if (a !== null && typeof a === 'object') {
          if (stackArg === undefined && a.matrixStack !== undefined) stackArg = a;
          if (meshArg === undefined && a.d3dxMesh !== undefined) meshArg = a;
        }
      }
      if (stackArg) return this.matrixStackCall(slot, stackArg.matrixStack, args, argIdx, env);
      if (meshArg) {
        let last = 0;
        for (let i = 0; i < args.length; i++) if (args[i] && args[i].d3dxMesh === meshArg.d3dxMesh) last = i;
        return this.d3dxMeshCall(slot, meshArg.d3dxMesh, args.slice(last + 1), env);
      }
      if (slot === this.model.world_accessor_slot) throw new Error('world matrix accessor called on no matrix stack');
      return this.dispatch(slot, args[0], args.slice(1), env, argIdx.slice(1));
    }
    if (fn === 'fptr') {
      // A call through a function pointer the executable keeps in memory runs the routine it points at
      const args = argIdx.map((a) => this.eval(a, env));
      const va = this.int(args[0]);
      // A pointer to a routine the image names runs as the named call it is, through the runtime's stand-in for it
      const name = this.libraryNames.get(va);
      if (name !== undefined && !this.hooks.has(va)) return this.namedCall(name, argIdx.slice(1), thisIdx, env, seq, va);
      if (!this.summarized(va) && !this.hooks.has(va)) throw new Error(`function pointer ${hex8(va)} names no summarized routine`);
      const rest = this.hooks.has(va) ? args.slice(1) : this.bitsArgs(args.slice(1), argIdx.slice(1));
      return this.run(va, thisIdx === undefined ? null : this.eval(thisIdx, env), rest, env.state, env);
    }
    if (fn.startsWith('CD3DXMatrixStack::')) {
      // The stack's methods reached through a typed global pointer carry their names
      const slot = STACK_METHODS[fn.slice('CD3DXMatrixStack::'.length)];
      if (slot === undefined) throw new Error(`${fn} is no matrix stack method`);
      const args = argIdx.map((a) => this.eval(a, env));
      const stackArg = args.find((a) => a && a.matrixStack !== undefined);
      if (!stackArg) throw new Error(`${fn} called on ${describe(args[0])} which holds no matrix stack`);
      return this.matrixStackCall(slot, stackArg.matrixStack, args, argIdx, env);
    }
    const dr = this.draw;
    if (fn.startsWith('sub_')) {
      let va = this.helperVA.get(fn);
      if (va === undefined) { va = parseInt(fn.slice(4), 16); this.helperVA.set(fn, va); }
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
      if (this.summarized(va)) return this.run(va, thisVal === undefined ? null : thisVal, this.bitsArgs(args, argIdx), env.state, env);
      if (this.scene.errors[hex8(va)]) throw new Error(`routine ${hex8(va)} is not summarized: ${this.scene.errors[hex8(va)]}`);
      throw new Error(`helper ${fn} has no derived meaning`);
    }
    // A device call read as a value is made the way the path makes it as an effect
    const cls = callClass(fn);
    if (cls === CALL_DEVICE || cls === CALL_DRAW) return this.namedCall(fn, argIdx, thisIdx, env, seq, target);
    const args = argIdx.map((a) => this.eval(a, env));
    if (this.traced.has(fn)) console.log(`[trace] ${fn}(${args.map((a) => this.describeArg(a)).join(', ')})`);
    switch (fn) {
      // The element accessor the optimizer expanded at its call sites, folded back by the derivation
      case ARRAY_ELEMENT: {
        const hook = this.hooks.get(ARRAY_ELEMENT);
        if (!hook) throw new Error('no array element accessor is registered');
        return hook(thisIdx === undefined ? undefined : this.eval(thisIdx, env), args, env.state, env);
      }
      case 'qsort': {
        // The library sort, ordering an array in memory by the routine the caller hands it
        const count = this.int(args[1]), size = this.int(args[2]);
        const compare = this.int(args[3]);
        if (!this.summarized(compare)) throw new Error(`qsort orders by ${hex8(compare)} which is not summarized`);
        const at = (i) => this.offsetPointer(args[0], i * size);
        const order = (a, b) => (this.int(this.run(compare, null, [a, b], env.state, env)) | 0);
        for (let i = 1; i < count; i++) {
          for (let j = i; j > 0 && order(at(j - 1), at(j)) > 0; j--) this.swapBytes(at(j - 1), at(j), size, env);
        }
        return 0;
      }
      case 'bsearch': {
        // The material registry answers by name, any other table by halving over the routine the caller hands
        const registry = this.model.material_registry;
        if (registry && typeof args[1] === 'number' && args[1] === registry.base) {
          const name = this.string(args[0]);
          const m = this.materials.get(name);
          if (!m) return 0;
          const slot = new SceneObject(HEAP_TYPE, null);
          slot.mem.store(0, 4, { material: m });
          return { obj: slot, off: 0 };
        }
        const count = this.int(args[2]), size = this.int(args[3]), compare = this.int(args[4]);
        if (!this.summarized(compare)) throw new Error(`bsearch orders by ${hex8(compare)} which is not summarized`);
        let lo = 0, hi = count;
        while (lo < hi) {
          const mid = (lo + hi) >>> 1;
          const at = this.offsetPointer(args[1], mid * size);
          const order = this.int(this.run(compare, null, [args[0], at], env.state, env)) | 0;
          if (order === 0) return at;
          if (order < 0) hi = mid; else lo = mid + 1;
        }
        return 0;
      }
      case 'wcscmp':
      case '_wcsicmp': {
        const a = this.string(args[0]), b = this.string(args[1]);
        const equal = fn === 'wcscmp' ? a === b : a.toLowerCase() === b.toLowerCase();
        return equal ? 0 : 1;
      }
      case 'wcslen': return this.string(args[0]).length;
      case 'lstrcmpW': { const a = this.string(args[0]), b = this.string(args[1]); return a === b ? 0 : (a < b ? u32(-1) : 1); }
      case 'lstrcmpiW': { const a = this.string(args[0]).toLowerCase(), b = this.string(args[1]).toLowerCase(); return a === b ? 0 : (a < b ? u32(-1) : 1); }
      case 'lstrlenW': return this.string(args[0]).length;
      case 'lstrcpyW': { this.storeString(args[0], this.string(args[1]), env); return args[0]; }
      case 'lstrcatW': { this.storeString(args[0], this.string(args[0]) + this.string(args[1]), env); return args[0]; }
      case 'lstrcmpA': { const a = this.ansiString(args[0], env), b = this.ansiString(args[1], env); return a === b ? 0 : (a < b ? u32(-1) : 1); }
      case 'lstrcmpiA': { const a = this.ansiString(args[0], env).toLowerCase(), b = this.ansiString(args[1], env).toLowerCase(); return a === b ? 0 : (a < b ? u32(-1) : 1); }
      case 'lstrlenA': return this.ansiString(args[0], env).length;
      case 'wcsncat': { this.storeString(args[0], this.string(args[0]) + this.string(args[1]).slice(0, this.int(args[2])), env); return args[0]; }
      case '_wtoi': return u32(parseInt(this.string(args[0]), 10) | 0);
      case '_wtol': return u32(parseInt(this.string(args[0]), 10) | 0);
      case '_itow': case '_ltow': {
        const radix = this.int(args[2]);
        const n = radix === 10 ? (this.int(args[0]) | 0) : this.int(args[0]);
        this.storeString(args[1], n.toString(radix), env);
        return args[1];
      }
      case 'wcsncmp': {
        const n = this.int(args[2]);
        const a = this.string(args[0]).slice(0, n), b = this.string(args[1]).slice(0, n);
        return a === b ? 0 : (a < b ? u32(-1) : 1);
      }
      case '_wcsnicmp': {
        const n = this.int(args[2]);
        const a = this.string(args[0]).slice(0, n).toLowerCase(), b = this.string(args[1]).slice(0, n).toLowerCase();
        return a === b ? 0 : (a < b ? u32(-1) : 1);
      }
      case 'swprintf': {
        const s = this.formatWide(this.string(args[1]), args.slice(2), env);
        this.storeString(args[0], s, env);
        return s.length;
      }
      case '_snwprintf': {
        const s = this.formatWide(this.string(args[2]), args.slice(3), env).slice(0, this.int(args[1]));
        this.storeString(args[0], s, env);
        return s.length;
      }
      case 'GetLocalTime': {
        // The console clock, written as the SYSTEMTIME words
        const d = new Date(this.host.console.now());
        const words = [d.getFullYear(), d.getMonth() + 1, d.getDay(), d.getDate(), d.getHours(), d.getMinutes(), d.getSeconds(), d.getMilliseconds()];
        words.forEach((w, i) => this.storeWord16(this.offsetPointer(args[0], 2 * i), w, env));
        return 0;
      }
      // The image maps every section from the start, so loading one hands back where it lies and freeing one succeeds
      case 'XLoadSectionA': case 'XFreeSectionA': {
        const s = this.memory.sectionNamed(this.ansiString(args[0], env));
        return s ? u32(s.va) : 0;
      }
      case 'XGetSectionHandleA': {
        const name = this.ansiString(args[0], env);
        const i = this.memory.sections.findIndex((s) => s.name === name);
        return i < 0 ? u32(-1) : i + 1;
      }
      case 'XLoadSectionByHandle': return u32(this.memory.sectionByHandle(this.int(args[0])).va);
      case 'XFreeSectionByHandle': this.memory.sectionByHandle(this.int(args[0])); return 1;
      case 'XGetSectionSize': { const s = this.memory.sectionByHandle(this.int(args[0])); return u32(s.end - s.va); }
      case 'memcpy': {
        // The C runtime copy, moved a word at a time where the length allows so handles survive the move
        const n = this.int(args[2]);
        const size = n % 4 === 0 ? 4 : 1;
        for (let i = 0; i < n; i += size) this.storeThrough(this.offsetPointer(args[0], i), size, this.loadLive(env, this.offsetPointer(args[1], i), size), env);
        return args[0];
      }
      case 'memset': {
        const n = this.int(args[2]), byte = this.int(args[1]) & 0xFF;
        const size = n % 4 === 0 ? 4 : 1;
        const value = size === 4 ? u32(byte * 0x01010101) : byte;
        for (let i = 0; i < n; i += size) this.storeThrough(this.offsetPointer(args[0], i), size, value, env);
        return args[0];
      }
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
      case 'D3DXCreateMeshFVF': {
        // The mesh is handed back through the last argument, its buffers sized by the counts and format
        const mesh = new D3DXMesh(this.int(args[0]), this.int(args[1]), this.int(args[2]), this.int(args[3]));
        this.storeThrough(args[5], 4, { d3dxMesh: mesh }, env);
        return 0;
      }
      case 'D3DXComputeBoundingBox': {
        // The extent of the positions of a vertex run laid out by its flexible vertex format
        const count = this.int(args[1]), stride = fvfStride(this.int(args[2]));
        const min = Float32Array.from([Infinity, Infinity, Infinity]), max = Float32Array.from([-Infinity, -Infinity, -Infinity]);
        for (let i = 0; i < count; i++) {
          const p = this.vector(this.offsetPointer(args[0], i * stride), 12, env);
          for (let k = 0; k < 3; k++) { if (p[k] < min[k]) min[k] = p[k]; if (p[k] > max[k]) max[k] = p[k]; }
        }
        this.storeThrough(args[3], 12, min, env);
        this.storeThrough(args[4], 12, max, env);
        return 0;
      }
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
      case 'D3DXQuaternionNormalize':
      case 'D3DXVec4Normalize': return vecNormalize(this.vector(args[1], 16, env));
      case 'D3DXVec3CatmullRom': return vecCatmullRom(this.vector(args[1], 12, env), this.vector(args[2], 12, env), this.vector(args[3], 12, env), this.vector(args[4], 12, env), this.floatArg(argIdx[5], env), 3);
      case 'D3DXVec2CatmullRom': return vecCatmullRom(this.vector(args[1], 8, env), this.vector(args[2], 8, env), this.vector(args[3], 8, env), this.vector(args[4], 8, env), this.floatArg(argIdx[5], env), 2);
      case 'D3DXQuaternionRotationAxis': return quatRotationAxis(this.vector(args[1], 12, env), this.floatArg(argIdx[2], env));
      case 'D3DXQuaternionSlerp': return quatSlerp(this.vector(args[1], 16, env), this.vector(args[2], 16, env), this.floatArg(argIdx[3], env));
      case 'D3DXQuaternionMultiply': return quatMultiply(this.vector(args[1], 16, env), this.vector(args[2], 16, env));
      case 'D3DXQuaternionRotationYawPitchRoll': return quatYawPitchRoll(this.floatArg(argIdx[1], env), this.floatArg(argIdx[2], env), this.floatArg(argIdx[3], env));
      case 'XGVec3Lerp': return vec3Lerp(this.vector(args[1], 12, env), this.vector(args[2], 12, env), this.floatArg(argIdx[3], env));
      case 'D3DXCreateMatrixStack': return { matrixStack: new MatrixStack() };
      case 'operator_new':
      case 'operator_new_array':
      case 'malloc': return this.heapObject(this.int(args[0]));
      case 'calloc': return this.heapObject(this.int(args[0]) * this.int(args[1]));
      case 'GlobalAlloc':
      case 'LocalAlloc': return this.heapObject(this.int(args[1]));
      case 'HeapAlloc': return this.heapObject(this.int(args[2]));
      case 'GlobalLock': return args[0];
      case 'GlobalUnlock': return 1;
      case 'GetProcessHeap': return 1;
      case 'GlobalFree': case 'LocalFree': case 'HeapFree': return 0;
      // A block grown in place keeps its identity and takes the new size
      case 'realloc': {
        if (this.int(args[0]) === 0) return this.heapObject(this.int(args[1]));
        if (args[0] && args[0].obj !== undefined && args[0].obj.type === HEAP_TYPE) {
          args[0].obj.size = this.int(args[1]);
          if (args[0].obj.mem.flat) args[0].obj.mem.grow(args[0].obj.size);
        }
        return args[0];
      }
      case 'operator_delete':
      case 'operator_delete_array':
      case 'free': return 0;
      // The graphics buffer constructor initializes the object it is called on and hands it back
      case 'XGRAPHICS::?0Buffer': return thisIdx !== undefined ? this.eval(thisIdx, env) : 0;
      // The vector constructor iterators run the element constructor over each element in place and hand the array back
      case 'YGXPAXIHP6EPAX0::?_H': case 'YGXPAXIHP6EX0::?_L': {
        const size = this.int(args[1]), count = this.int(args[2]), ctor = this.int(args[3]);
        if (!this.summarized(ctor) && !this.hooks.has(ctor)) throw new Error(`element constructor ${hex8(ctor)} is not summarized`);
        for (let i = 0; i < count; i++) this.run(ctor, this.offsetPointer(args[0], i * size), [], env.state, env);
        return args[0];
      }
      // Destructors registered for the end of the program never run, the page being the program's end
      case 'atexit': return 0;
      case 'HalReadSMCTrayState': this.storeThrough(args[0], 4, u32(this.host.console.trayState()), env); return 0;
      case 'XGetDevices': return u32(this.host.console.devices(this.int(args[0])));
      case 'CreateEventA':
      case 'CreateThread': return { ptr: fn, off: seq };
      case 'D3DDevice_CreateVertexShader': return { program: this.int(args[1]), declaration: this.int(args[0]) };
      case 'D3DDevice_CreateVertexBuffer': return { vb: this.host.createVertexBuffer(this.int(args[0]), this.int(args[2])) };
      // The vertex format of a buffer the executable makes this way comes from the shader the draw sets
      case 'D3DDevice_CreateVertexBuffer2': return { vb: this.host.createVertexBuffer(this.int(args[0]), 0) };
      case 'D3DVertexBuffer_Lock':
      case 'D3DVertexBuffer_Lock2': {
        const vb = args[0] && args[0].vb ? args[0].vb : null;
        if (!vb) throw new Error(`lock of ${describe(args[0])} which is no vertex buffer`);
        return { raw: vb, off: 0 };
      }
      // The compiler's own frame check, which passes on a frame the runtime never corrupts
      case '_security_check_cookie': return 0;
      // The runtime keeps every resource it hands out alive, so a reference taken on one is the only one
      case 'D3DResource_AddRef': return 1;
      case 'D3DResource_Release': return 0;
      case 'XNetRandom': {
        // The network stack's random bytes come from the host
        const count = this.int(args[1]);
        const bytes = this.host.console.randomBytes(count);
        for (let i = 0; i < count; i++) this.storeThrough(this.offsetPointer(args[0], i), 1, bytes[i], env);
        return 0;
      }
      // The kernel's hashing, kept as a running context in the word the caller hands it
      case 'XcSHAInit': this.storeThrough(args[0], 4, { sha: new ShaContext() }, env); return 0;
      case 'XcSHAUpdate': this.shaContext(args[0], env).update(this.readBytes(args[1], this.int(args[2]), env)); return 0;
      case 'XcSHAFinal': this.storeBytes(args[1], this.shaContext(args[0], env).digest(), env); return 0;
      case 'XcHMAC': {
        const key = this.readBytes(args[0], this.int(args[1]), env);
        const data = new Uint8Array([...this.readBytes(args[2], this.int(args[3]), env), ...this.readBytes(args[4], this.int(args[5]), env)]);
        this.storeBytes(args[6], hmacSha1(key, data), env);
        return 0;
      }
      case 'XcVerifyPKCS1Signature': return u32(this.host.console.verifySignature());
      case 'XGetVideoStandard': return u32(this.host.console.videoStandard());
      case 'XGetAVPack': return u32(this.host.console.avPack());
      case 'HalReturnToFirmware': this.host.console.reboot(); return 0;
      // The console runs the dashboard on one thread, so locks and exception filters have nothing to do
      case 'SetUnhandledExceptionFilter': return 0;
      case 'InitializeCriticalSection': case 'EnterCriticalSection': case 'LeaveCriticalSection': case 'DeleteCriticalSection':
      case 'RtlInitializeCriticalSection': case 'RtlEnterCriticalSection': case 'RtlLeaveCriticalSection': return 0;
      case 'CreateMutexA': case 'CreateSemaphoreA': case 'CreateEventW': return { ptr: fn, off: seq };
      case 'SetEvent': case 'ResetEvent': case 'ReleaseMutex': case 'ReleaseSemaphore': case 'SetThreadPriority': return 1;
      case 'GetCurrentThread': return u32(0xFFFFFFFE);
      case 'GetCurrentThreadId': case 'GetCurrentProcessId': return 1;
      case 'Sleep': case 'SwitchToThread': return 0;
      case 'QueryPerformanceFrequency': {
        this.storeThrough(args[0], 4, this.host.console.performanceFrequency() >>> 0, env);
        this.storeThrough(this.offsetPointer(args[0], 4), 4, 0, env);
        return 1;
      }
      case 'QueryPerformanceCounter': {
        const count = this.host.console.performanceCounter();
        this.storeThrough(args[0], 4, u32(count % 0x100000000), env);
        this.storeThrough(this.offsetPointer(args[0], 4), 4, u32(Math.floor(count / 0x100000000)), env);
        return 1;
      }
      case 'XGetLaunchInfo': {
        // The console was started from its own dashboard, so no launch data arrives
        this.storeThrough(args[0], 4, 0, env);
        return 0;
      }
      case 'XInitDevices': case 'XSetProcessQuantumLength': return 0;
      // The console never powers itself down, so the idle timer the dashboard resets has nothing to count
      case 'XAutoPowerDownResetTimer': return 0;
      case 'RtlInitAnsiString': {
        // The counted string takes the length of the characters it is pointed at, with room for their terminator
        const length = this.ansiString(args[1], env).length;
        this.storeThrough(args[0], 2, length, env);
        this.storeThrough(this.offsetPointer(args[0], 2), 2, length + 1, env);
        this.storeThrough(this.offsetPointer(args[0], 4), 4, args[1], env);
        return 0;
      }
      // No volume is ever mounted on the console's drive, so there is nothing to dismount
      case 'IoDismountVolumeByName': return 0;
    }
    const served = this.consoleApi.call(fn, args, argIdx, env);
    if (served !== undefined) return served;
    // A library routine the runtime stands in for nothing runs out of the image, unless the runtime hooked it by address
    const bodyVA = this.imageRoutine(fn, target);
    if (bodyVA !== undefined && !this.hooks.has(bodyVA)) return this.runImageRoutine(fn, args, argIdx, thisIdx === undefined ? undefined : this.eval(thisIdx, env), env, bodyVA);
    if (fn.startsWith('D3DDevice_') || fn.startsWith('D3D::')) return { call: fn };
    throw new Error(`call to ${fn} has no runtime meaning${this.scene.errors[fn] ? ': ' + this.scene.errors[fn] : ''}`);
  }

  // The address a kernel export lives at in the kernel image, the thunk naming it by that name
  exportAddress(name) {
    const va = this.model.kernel.exports[name];
    if (va === undefined) throw new Error(`the kernel exports no ${name}`);
    return va >>> 0;
  }

  // The address of the library routine a call reaches whose body the model carries, or undefined where it carries none
  imageRoutine(fn, target) {
    const va = target ?? this.libraryAddresses.get(fn) ?? this.model.kernel.exports[fn];
    return va !== undefined && this.summarized(va) ? va : undefined;
  }

  // Makes a call to a routine the image names the way a call naming it in a path is made
  namedCall(fn, argIdx, thisIdx, env, seq, target) {
    const cls = callClass(fn);
    if (cls !== CALL_DRAW && cls !== CALL_DEVICE) return this.performCall(fn, argIdx, thisIdx, env, seq, target);
    const args = argIdx.map((a) => this.eval(a, env));
    if (cls === CALL_DRAW) {
      this.deviceDraw(fn, args, env, env.state);
      return 0;
    }
    if (this.deviceCall(fn, args, { args: argIdx, seq }, env, env.state)) return 0;
    if (this.imageRoutine(fn, target) === undefined) throw new Error(`device call ${fn} is not mapped${this.scene.errors[fn] ? ': ' + this.scene.errors[fn] : ''}`);
    return this.runImageRoutine(fn, args, argIdx, thisIdx === undefined ? undefined : this.eval(thisIdx, env), env, target);
  }

  // Runs the image's own body of a library routine, its arguments placed where its callers' convention puts them
  runImageRoutine(fn, args, argIdx, thisVal, env, target) {
    const va = target ?? this.libraryAddresses.get(fn);
    if (va === undefined) throw new Error(`the image carries no body of ${fn}${this.scene.errors[fn] ? ': ' + this.scene.errors[fn] : ''}`);
    return this.run(va, thisVal === undefined ? null : thisVal, argIdx ? this.bitsArgs(args, argIdx) : args, env.state, env);
  }

  // Runs a library routine of the image for the runtime itself, on a work budget of its own rather than the frame's
  callImage(fn, args, state) {
    const va = this.libraryAddresses.get(fn);
    if (va === undefined) throw new Error(`the image carries no body of ${fn}${this.scene.errors[fn] ? ': ' + this.scene.errors[fn] : ''}`);
    const work = this.work;
    this.work = 0;
    try {
      return this.run(va, null, args, state, null);
    } finally {
      this.work = work;
    }
  }

  // Writes a string the executable copies into a global, an object field or a stack buffer, a pointer into
  // an existing string replacing its tail
  storeString(dst, s, env) {
    if (dst && dst.chars !== undefined) {
      const head = this.string({ ...dst, chars: 0 }).slice(0, dst.chars);
      this.storeString({ ...dst, chars: undefined }, head + s, env);
      return;
    }
    // The characters and the terminator overwrite whatever the span held, as the copy does on the console
    const span = 2 * (s.length + 1);
    if (typeof dst === 'number') { this.memory.store(dst, span, { str: s }); return; }
    if (dst && (dst.obj !== undefined || dst.stack !== undefined)) {
      this.storeThrough(dst, span, { str: s, inline: true }, env);
      return;
    }
    if (dst && dst.raw !== undefined) {
      for (let i = 0; i <= s.length; i++) this.storeThrough(this.offsetPointer(dst, 2 * i), 2, i < s.length ? s.charCodeAt(i) : 0, env);
      return;
    }
    throw new Error(`string copied into ${describe(dst)}`);
  }

  // Runs an ID3DXBaseMesh method on a mesh D3DX built, the arguments being those after the mesh itself
  d3dxMeshCall(slot, mesh, rest, env) {
    const takes = (n) => { if (rest.length < n) throw new Error(`D3DX mesh method at slot 0x${slot.toString(16)} called with ${rest.length} of ${n} arguments`); };
    switch (slot) {
      case MESH_ADDREF: return 1;
      case MESH_RELEASE: return 0;
      case MESH_DRAW_SUBSET: {
        // D3DX draws the whole mesh from its own buffers, which it binds as the stream and the indices first
        takes(1);
        this.deviceCall('D3DDevice_SetVertexShader', [mesh.fvf], null, env, env.state);
        this.deviceCall('D3DDevice_SetStreamSource', [0, { vb: mesh.vb }, fvfStride(mesh.fvf)], null, env, env.state);
        this.deviceCall('D3DDevice_SetIndices', [{ ib: mesh.ib }, 0], null, env, env.state);
        this.deviceDraw('D3DDevice_DrawIndexedVertices', [PT_TRIANGLELIST, mesh.numFaces * 3, { raw: mesh.ib, off: 0 }], env, env.state);
        return 0;
      }
      case MESH_NUM_FACES: return mesh.numFaces;
      case MESH_NUM_VERTICES: return mesh.numVertices;
      case MESH_FVF: return mesh.fvf;
      case MESH_OPTIONS: return mesh.options;
      case MESH_GET_VB: takes(1); this.storeThrough(rest[0], 4, { vb: mesh.vb }, env); return 0;
      case MESH_GET_IB: takes(1); this.storeThrough(rest[0], 4, { ib: mesh.ib }, env); return 0;
      case MESH_LOCK_VB: takes(2); this.storeThrough(rest[1], 4, { raw: mesh.vb, off: 0 }, env); return 0;
      case MESH_UNLOCK_VB: return 0;
      case MESH_LOCK_IB: takes(2); this.storeThrough(rest[1], 4, { raw: mesh.ib, off: 0 }, env); return 0;
      case MESH_UNLOCK_IB: return 0;
    }
    throw new Error(`D3DX mesh method at slot 0x${slot.toString(16)} has no runtime meaning`);
  }

  // Runs an ID3DXMatrixStack method on the executable's stack, the float arguments following the stack itself
  matrixStackCall(slot, stack, args, argIdx, env) {
    const matrixArg = () => {
      const m = args.find((a) => a instanceof Float32Array || (typeof a === 'number' && this.memory.mapped(a)) || (a && a.matrixStack === undefined && (a.obj !== undefined || a.stack !== undefined)));
      if (m === undefined) throw new Error(`matrix stack slot 0x${slot.toString(16)} called without a matrix`);
      return this.matrix(m, env);
    };
    // The floats follow the stack itself, which a COM style call names both in the register and as the pushed this
    let first = 0;
    for (let i = 0; i < args.length; i++) if (args[i] && args[i].matrixStack === stack) first = i + 1;
    const floats = (n) => {
      if (args.length < first + n) throw new Error(`matrix stack slot 0x${slot.toString(16)} called with ${args.length - first} of ${n} floats`);
      return Array.from({ length: n }, (_, i) => this.floatArg(argIdx[first + i], env));
    };
    switch (slot) {
      case STACK_POP: stack.pop(); return { call: 'pop' };
      case STACK_PUSH: stack.push(); return { call: 'push' };
      case STACK_IDENTITY: stack.identity(); return { call: 'identity' };
      case STACK_LOAD: stack.load(matrixArg()); return { call: 'load' };
      case STACK_MULT: stack.mult(matrixArg()); return { call: 'mult' };
      case STACK_MULT_LOCAL: stack.multLocal(matrixArg()); return { call: 'multlocal' };
      case STACK_ROTATE_AXIS: { const [angle] = floats(1); stack.mult(matRotationQuaternion(quatRotationAxis(this.vector(args[first], 12, env), angle))); return { call: 'rotateaxis' }; }
      case STACK_ROTATE_AXIS_LOCAL: { const [angle] = floats(1); stack.multLocal(matRotationQuaternion(quatRotationAxis(this.vector(args[first], 12, env), angle))); return { call: 'rotateaxislocal' }; }
      case STACK_ROTATE_YPR: { const [yaw, pitch, roll] = floats(3); stack.mult(matRotationQuaternion(quatYawPitchRoll(yaw, pitch, roll))); return { call: 'rotateypr' }; }
      case STACK_ROTATE_YPR_LOCAL: { const [yaw, pitch, roll] = floats(3); stack.multLocal(matRotationQuaternion(quatYawPitchRoll(yaw, pitch, roll))); return { call: 'rotateyprlocal' }; }
      case STACK_SCALE: { const [x, y, z] = floats(3); stack.mult(matScaling(x, y, z)); return { call: 'scale' }; }
      case STACK_SCALE_LOCAL: { const [x, y, z] = floats(3); stack.multLocal(matScaling(x, y, z)); return { call: 'scalelocal' }; }
      case STACK_TRANSLATE: { const [x, y, z] = floats(3); stack.mult(matTranslation(x, y, z)); return { call: 'translate' }; }
      case STACK_TRANSLATE_LOCAL: { const [x, y, z] = floats(3); stack.multLocal(matTranslation(x, y, z)); return { call: 'translatelocal' }; }
      case STACK_TOP: return stack.top;
    }
    throw new Error(`matrix stack slot 0x${slot.toString(16)} has no runtime meaning`);
  }

  // Converts a runtime script value the way the engine's dispatcher asks, into a boxed number or string
  scriptValueCall(slot, obj, env) {
    const s = this.scene.script, v = obj.scriptValue;
    if (slot === s.release) return 0;
    // A plain value resolves to itself, which ends the dispatcher's walk through references
    if (slot === s.resolve) return { obj, off: 0 };
    if (slot === s.to_number || slot === s.to_string) {
      const box = new SceneObject(SCRIPT_VALUE_TYPE, null);
      box.scriptValue = v;
      // The box starts with the vtable its class constructor stores, which the dispatcher loads before a call
      box.mem.store(0, 4, slot === s.to_number ? s.number_vtable : s.string_vtable);
      if (slot === s.to_number) box.mem.store(s.number_field, 4, bitsFromFloat(Number(v)));
      else box.mem.store(s.string_field, 4, { str: String(v) });
      return { obj: box, off: 0 };
    }
    throw new Error(`script value method at slot 0x${slot.toString(16)} is none the dispatcher asks for`);
  }

  // Calls a virtual method on a scene object, a material object or a mesh buffer
  dispatch(slot, target, args, env, argIdx) {
    const routineArgs = (va) => (this.hooks.has(va) ? args : this.bitsArgs(args, argIdx));
    if (target && target.obj !== undefined && target.obj.type === SCRIPT_VALUE_TYPE) return this.scriptValueCall(slot, target.obj, env);
    if (target && target.obj !== undefined && target.obj.type === BUFFER_TYPE) {
      if (slot === this.accessor.slot) return this.readField(target.obj, this.draw.buffer_fields.format, 4);
      if (slot === 0) return this.run(this.draw.buffer_draw, target.obj, routineArgs(this.draw.buffer_draw), env.state, env);
      throw new Error(`mesh buffer slot 0x${slot.toString(16)} has no derived meaning`);
    }
    if (target && target.obj !== undefined && this.types[target.obj.type]) {
      const va = this.typeSlot(target.obj.type, slot);
      return this.run(va, target, routineArgs(va), env.state, env);
    }
    // An object the runtime stands in for serves its virtual methods itself
    if (target && target.obj !== undefined && target.obj.serve) return target.obj.serve(slot, args, env);
    if (target && target.obj !== undefined) {
      // Any other object dispatches through the vtable its constructor stored, read off the image
      const vt = this.readField(target.obj, target.off || 0, 4);
      if (typeof vt !== 'number' || !this.memory.mapped(vt + slot)) throw new Error(`virtual call slot 0x${slot.toString(16)} on ${describe(target)} which holds no vtable`);
      const va = this.memory.load(vt + slot, 4);
      if (!this.summarized(va) && !this.hooks.has(va)) throw new Error(`vtable 0x${vt.toString(16)} slot 0x${slot.toString(16)} is ${hex8(va)} which is not summarized`);
      return this.run(va, target, routineArgs(va), env.state, env);
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
    const s = this.stringOrNull(v);
    if (s === null) throw new Error(`expected a string, got ${describe(v)}`);
    return s;
  }

  // The bytes a path reads through a string pointer, the characters it holds being sixteen bits each
  stringWord(v, size) {
    const bits = bitsAt({ str: v.str }, Math.round(2 * (v.chars || 0)));
    return size >= 4 ? bits : bits & (size === 1 ? 0xFF : 0xFFFF);
  }

  // A traced argument as text where it plainly holds a string, else as the value it is
  describeArg(a) {
    let held = null;
    if (a && a.str !== undefined) held = a.str;
    else if (typeof a === 'number' && a !== 0) held = this.draw.strings[String(a)] ?? this.memory.wideString(a);
    else if (a && (a.stack !== undefined || a.obj !== undefined)) { try { held = this.stringOrNull(a); } catch (_) { held = null; } }
    return held !== null ? JSON.stringify(held) : describe(a);
  }

  // The running hash the kernel keeps in a context word
  shaContext(ctx, env) {
    const held = this.loadLive(env, ctx, 4);
    if (!held || held.sha === undefined) throw new Error(`${describe(ctx)} holds no hash context`);
    return held.sha;
  }

  // Bytes the executable holds in memory from a pointer, as a library routine reads them
  readBytes(v, count, env) {
    if (v && v.obj !== undefined && v.obj.type !== VALUE_TYPE && v.obj.mem) return this.objectBytes(v.obj, v.off || 0, count);
    if (v && v.raw !== undefined) {
      const off = v.off || 0;
      if (off < 0 || off + count > v.raw.bytes.length) throw new Error(`read of ${count} bytes at +0x${off.toString(16)} outside the ${v.raw.bytes.length} bytes of raw#${v.raw.id}`);
      return v.raw.bytes.slice(off, off + count);
    }
    const out = new Uint8Array(count);
    for (let i = 0; i < count; i++) out[i] = this.int(this.loadLive(env, this.offsetPointer(v, i), 1)) & 0xFF;
    return out;
  }

  // The bytes of an object range, the bytes never written reading as what the object held before
  objectBytes(obj, off, count) {
    const mem = obj.mem;
    const over = mem.handleCovering(off, count);
    if (over) throw new Error(`${obj.type} bytes +0x${off.toString(16)} read inside the ${describe(over.handle)} handle at +0x${over.at.toString(16)}`);
    const out = mem.bytes(off, count);
    if (mem.allWritten(off, count)) return out;
    for (let i = 0; i < count; i++) if (!mem.byteWritten(off + i)) out[i] = this.fallbackByte(obj, off + i);
    return out;
  }

  // The characters of a byte string the executable built in memory, read until its terminator
  ansiString(v, env) {
    let out = '';
    for (let i = 0; i < 1024; i++) {
      const b = this.int(this.loadLive(env, this.offsetPointer(v, i), 1)) & 0xFF;
      if (b === 0) return out;
      out += String.fromCharCode(b);
    }
    throw new Error(`byte string at ${describe(v)} runs past 1024 characters`);
  }

  // Writes a byte string with its terminator through a pointer, as a library routine does, for later loads to read
  storeAnsi(v, s, env) {
    for (let i = 0; i <= s.length; i++) this.storeThrough(this.offsetPointer(v, i), 1, i < s.length ? s.charCodeAt(i) & 0xFF : 0, env);
  }

  // Writes bytes through a pointer, as one block into an object or raw memory, else word by word
  storeBytes(v, bytes, env) {
    if (v && v.obj !== undefined && v.obj.type !== VALUE_TYPE && v.obj.mem && !v.obj.mem.handleCovering(v.off || 0, bytes.length)) {
      const off = v.off || 0;
      v.obj.mem.setBytes(off, bytes);
      return;
    }
    if (v && v.raw !== undefined) {
      const off = v.off || 0;
      if (off < 0 || off + bytes.length > v.raw.bytes.length) throw new Error(`write of ${bytes.length} bytes at +0x${off.toString(16)} outside the ${v.raw.bytes.length} bytes of raw#${v.raw.id}`);
      v.raw.bytes.set(bytes, off);
      v.raw.version++;
      v.raw.dirty(off, bytes.length);
      return;
    }
    let i = 0;
    for (; i + 4 <= bytes.length; i += 4) this.storeThrough(this.offsetPointer(v, i), 4, u32(bytes[i] | (bytes[i + 1] << 8) | (bytes[i + 2] << 16) | (bytes[i + 3] << 24)), env);
    for (; i < bytes.length; i++) this.storeThrough(this.offsetPointer(v, i), 1, bytes[i], env);
  }

  // Writes a half word through a pointer
  storeWord16(v, value, env) {
    this.storeThrough(v, 2, value & 0xFFFF, env);
  }

  // Expands the conversions of a byte format string with its arguments the way the C runtime does
  formatAnsi(format, argValues, env) {
    return this.formatWide(format, argValues, env, true);
  }

  // Expands the conversions of a wide format string with its arguments the way the C runtime does, the plain
  // string conversion reading bytes when the format itself is bytes
  formatWide(format, argValues, env, ansi = false) {
    let next = 0;
    const take = () => {
      if (next >= argValues.length) throw new Error(`format ${JSON.stringify(format)} reads past its ${argValues.length} arguments`);
      return argValues[next++];
    };
    return format.replace(/%([-+ 0#]*)(\d+|\*)?(?:\.(\d+|\*))?(l|h|ll|I64)?([diouxXcsCS%])/g, (m, flags, width, precision, length, kind) => {
      if (kind === '%') return '%';
      if (width === '*') width = String(this.int(take()) | 0);
      if (precision === '*') precision = String(this.int(take()) | 0);
      let s;
      switch (kind) {
        case 'd': case 'i': s = String(this.int(take()) | 0); break;
        case 'u': s = String(this.int(take())); break;
        case 'x': s = this.int(take()).toString(16); break;
        case 'X': s = this.int(take()).toString(16).toUpperCase(); break;
        case 'o': s = this.int(take()).toString(8); break;
        case 'c': case 'C': s = String.fromCharCode(this.int(take()) & 0xFFFF); break;
        case 's': case 'S': { const p = take(); s = (kind === 's') === ansi ? this.ansiString(p, env) : this.string(p); break; }
      }
      if (precision !== undefined && kind !== 's' && kind !== 'S') s = s.replace(/^-?/, (sign) => sign + '0'.repeat(Math.max(0, Number(precision) - (s.length - sign.length))));
      if (precision !== undefined && (kind === 's' || kind === 'S')) s = s.slice(0, Number(precision));
      if (width !== undefined && s.length < Number(width)) {
        const pad = flags.includes('0') && !flags.includes('-') && kind !== 's' && kind !== 'S' ? '0' : ' ';
        s = flags.includes('-') ? s + ' '.repeat(Number(width) - s.length) : (pad === '0' ? s.replace(/^-?/, (sign) => sign + pad.repeat(Number(width) - s.length)) : pad.repeat(Number(width) - s.length) + s);
      }
      return s;
    });
  }

  // The characters a value holds, or null when it holds no string
  stringOrNull(v) {
    if (v && v.chars !== undefined) {
      // A pointer landing between two code units reads the bytes from there on as the characters
      if (!Number.isInteger(v.chars)) return this.assembleWide((k) => this.stringWord({ str: v.str, chars: v.chars + k }, 2));
      const whole = this.stringOrNull({ ...v, chars: undefined });
      return whole === null ? null : whole.slice(v.chars);
    }
    if (v && v.str !== undefined) return v.str;
    if (v && v.raw !== undefined) {
      const off = v.off || 0;
      return this.assembleWide((k) => (off + 2 * k + 2 <= v.raw.bytes.length ? v.raw.view.getUint16(off + 2 * k, true) : undefined));
    }
    if (v && v.stack !== undefined && v.frame) {
      const held = v.frame.stack.handles.get(v.stack);
      if (held && held.str !== undefined) return held.str;
      // Frame memory nothing wrote holds zeros, so a string read there is empty rather than absent
      return this.assembleWide((k) => v.frame.stack.unitAt(v.stack + 2 * k) ?? 0);
    }
    if (typeof v === 'number') {
      const s = this.draw.strings[String(v)];
      if (s !== undefined) return s;
      const g = this.memory.words.get(v);
      if (g && g.value && g.value.str !== undefined) return g.value.str;
      return this.memory.wideString(v);
    }
    if (v && v.obj !== undefined) {
      const off = v.off || 0;
      const held = v.obj.mem.handles.get(off);
      if (held && held.str !== undefined) return held.str;
      if (!v.obj.mem.anyWritten(off, 2)) {
        const f = this.readField(v.obj, off, 4);
        if (f && f.str !== undefined) return f.str;
      }
      return this.assembleWide((k) => v.obj.mem.unitAt(off + 2 * k));
    }
    return null;
  }

  // Reads a wide string half word by half word until its terminator, null where the memory holds none
  assembleWide(unitAt) {
    let out = '';
    for (let k = 0; k <= WIDE_STRING_LIMIT; k++) {
      const w = unitAt(k);
      if (w === undefined) return k === 0 ? null : out;
      const unit = u32(w) & 0xFFFF;
      if (unit === 0) return out;
      out += String.fromCharCode(unit);
    }
    return null;
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
    if (v && v.raw !== undefined) return rawLoad(v.raw, v.off || 0, 64);
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
    if (v && v.raw !== undefined) {
      const out = new Float32Array(size / 4);
      for (let i = 0; i < out.length; i++) out[i] = v.raw.view.getFloat32((v.off || 0) + 4 * i, true);
      return out;
    }
    throw new Error(`expected a vector, got ${describe(v)}`);
  }

  // Assembles consecutive float words of an object into a vector
  readWords(obj, off, size) {
    const out = new Float32Array(size / 4);
    for (let i = 0; i < out.length; i++) out[i] = floatFromBits(valueBits(this.readField(obj, off + 4 * i, 4)));
    return out;
  }

  // Runs a summarized function or a runtime hook on an object with the shared device state, logging the calls
  // of the routines being traced with their arguments and results
  run(va, thisObj, args, state, parent) {
    if (this.traced.has(va)) {
      const result = this.runUntraced(va, thisObj, args, state, parent);
      console.log(`[trace] ${hex8(va)}(${describe(thisObj)}; ${args.map(describe).join(', ')}) = ${describe(result)}`);
      return result;
    }
    return this.runUntraced(va, thisObj, args, state, parent);
  }

  runUntraced(va, thisObj, args, state, parent) {
    const hook = this.hooks.get(va);
    if (hook) return hook(thisObj, args, state, parent);
    const paths = this.functions.get(va);
    const lifted = this.lifted.get(va);
    if (!paths && !lifted) {
      const why = this.scene.errors[hex8(va)];
      throw new Error(`function ${hex8(va)} is not summarized in the render model${why ? ': ' + why : ''}`);
    }
    const env = this.newEnv(thisObj, args);
    env.state = state;
    env.mesh = parent ? parent.mesh : null;
    env.parent = parent || null;
    try {
      if (lifted) this.runLifted(va, lifted, env, thisObj, args, state);
      else this.runPaths(paths, env, state);
    } catch (err) {
      if (!err.routine) { err.routine = hex8(va); err.message = `${hex8(va)}: ${err.message}`; err.callers = []; }
      else if (err.callers.length < 12) { err.callers.push(hex8(va)); err.message += ` < ${hex8(va)}`; }
      throw err;
    } finally {
      // The frame's pages go back to the pool as the routine returns, the way its stack is reused on the console
      env.stack.release();
    }
    return env.state.returned;
  }

  // Runs a lifted routine block by block, carrying the registers, x87 stack, vector lanes and flags between blocks
  runLifted(va, fn, env, thisObj, args, state) {
    env.regs = { EAX: 0, ECX: thisObj instanceof SceneObject ? { obj: thisObj, off: 0 } : (thisObj ?? 0), EDX: 0, EBX: 0, ESP: { stack: LIFTED_ENTRY_SP, frame: env }, EBP: 0, ESI: 0, EDI: 0 };
    env.fpu = [];
    env.xmm = {};
    env.mm = {};
    env.flags = {};
    env.fcmp = null;
    let next = 0, slot = 0;
    if (fn.params) {
      // A library routine takes each argument where its callers' convention places it, the rest following on the stack
      for (; next < fn.params.length && next < args.length; next++) {
        const at = fn.params[next], v = args[next] ?? 0;
        if (at === '') continue;
        if (at.startsWith('stack')) {
          const k = Number(at.slice(5));
          env.stack.store(LIFTED_ENTRY_SP + 4 + 4 * k, 4, v);
          slot = Math.max(slot, k + 1);
        } else if (at.startsWith('XMM')) env.xmm['xmm' + at.slice(3) + '.0'] = v;
        else env.regs[at] = v;
      }
    } else {
      for (const r of fn.input_regs) {
        if (r.startsWith('XMM')) env.xmm['xmm' + r.slice(3) + '.0'] = args[next++] ?? 0;
        else env.regs[r] = args[next++] ?? 0;
      }
    }
    for (; next < args.length; slot++) env.stack.store(LIFTED_ENTRY_SP + 4 + 4 * slot, 4, args[next++] ?? 0);
    const compiled = this.interpret ? INTERPRETED : (fn._compiled || this.compileLifted(va, fn));
    if (compiled !== INTERPRETED) { compiled(env, state); return; }
    this.interpretLifted(va, fn, env, state);
  }

  // Runs a lifted routine block by block by interpretation, carrying the registers, x87 stack, vector lanes and
  // flags between blocks
  interpretLifted(va, fn, env, state) {
    let key = fn.entry;
    const traceBlocks = this.traced.has('blocks:' + hex8(va));
    for (let steps = 0; ; steps++) {
      if (steps >= LIFTED_BLOCK_LIMIT) throw new Error(`ran ${steps} blocks without returning`);
      if (++this.work > FRAME_WORK_BUDGET) throw new Error(`the frame ran past ${FRAME_WORK_BUDGET} steps in ${hex8(va)} at block ${key} after ${steps} blocks of its own`);
      const b = fn.blocks[key];
      if (!b) throw new Error(`no block ${key}`);
      env.results = new Map();
      env.loads = new Map();
      if (!b._marked) this.annotateBlock(b);
      try {
        this.applyEffects(b.effects, env);
        const regs = {};
        for (const [r, idx] of Object.entries(b.regs)) regs[r] = this.eval(idx, env);
        const fpu = b.fpu.map((idx) => this.eval(idx, env));
        const xmm = {};
        for (const [name, idx] of Object.entries(b.xmm || {})) xmm[name] = this.eval(idx, env);
        const mm = {};
        for (const [name, idx] of Object.entries(b.mm || {})) mm[name] = this.eval(idx, env);
        const flags = {};
        for (const [name, idx] of Object.entries(b.flags || {})) if (typeof idx !== 'number') flags[name] = idx; else if (idx >= 0) flags[name] = this.eval(idx, env);
        const fcmp = b.fcmp ? { a: this.eval(b.fcmp.a, env), b: this.eval(b.fcmp.b, env) } : null;
        const t = b.term;
        let to;
        switch (t.kind) {
          case 'return': state.returned = this.eval(t.value, env); return;
          case 'trap': throw new Error('the executable traps here');
          case 'noreturn': throw new Error('the routine called last in this block never returns');
          case 'halt': state.returned = 0; return;
          case 'jump': to = t.target; break;
          case 'branch': to = this.truthy(this.eval(t.cond, env)) ? t.taken : t.fall; break;
          case 'switch': {
            const i = this.int(this.eval(t.index, env));
            if (i >= t.targets.length) throw new Error(`switch index ${i} outside its ${t.targets.length} cases`);
            to = t.targets[i];
            break;
          }
          default: throw new Error(`block ends with ${t.kind}`);
        }
        if (traceBlocks) this.traceBlock(va, key, to, env);
        const esp = b.esp !== undefined ? this.eval(b.esp, env) : { stack: b.sp, frame: env };
        Object.assign(env.regs, regs);
        env.regs.ESP = esp;
        env.fpu = fpu;
        Object.assign(env.xmm, xmm);
        Object.assign(env.mm, mm);
        env.flags = flags;
        env.fcmp = fcmp;
        key = to;
      } catch (err) {
        if (!err.block) { err.block = key; err.message = `block ${key}: ${err.message}`; }
        throw err;
      }
    }
  }

  // Runs a material Apply method for a scene node holding the material
  applyMaterial(mat, node, parent) {
    const env = this.newEnv(null, [], mat, node);
    env.state = parent.state;
    env.mesh = parent.mesh;
    env.parent = parent;
    try {
      this.runPaths(mat.apply, env, parent.state);
    } finally {
      env.stack.release();
    }
    return env.state.returned;
  }

  // Runs the first path whose guards hold, applying effects in program order as the guards are decided
  runPaths(paths, env, state) {
    env.state = state;
    if (!paths._trie) {
      paths._trie = buildTrie(paths);
      this.annotateTrie(paths._trie, []);
    }
    const compiled = this.interpret ? INTERPRETED : (paths._compiled || this.compilePaths(paths));
    if (compiled !== INTERPRETED) return compiled(env, state);
    const p = this.matchPath(paths._trie, env);
    if (!p) throw new Error(`no execution path matches${env.mat ? ' for material ' + env.mat.name : ''}`);
    state.returned = this.eval(p.return, env);
    return state;
  }

  // Compiles a path trie into one function per node, a guarded child compiling the first time its guard holds
  compilePaths(paths) {
    const emitter = this.exprEmitter(true);
    const effects = [];
    const guards = [];
    const nodes = [];
    const emitEffects = (lines, list) => {
      for (const e of list) {
        const k = effects.length;
        effects.push(e);
        lines.push(`if (++d.work > ${FRAME_WORK_BUDGET}) d.budgetExceeded();`);
        if (e._early) lines.push(`d.readEarly(E[${k}]._early, env);`);
        if (e.kind === 'store' && ((e.has_addr && e.size <= 4) || (!e.has_addr && (e.base === 'stack' || e.base === 'this')))) {
          const body = [];
          const v = emitter.emitShared(e.value, body);
          lines.push(`try { ${body.join(' ')} let v = ${v};`);
          if (e.size === 8) lines.push(`if (typeof v === 'number') v = { f64: v }; else if (v !== null && typeof v === 'object' && v.bits !== undefined) v = { f64: F.floatFromBits(v.bits) };`);
          else if (this.isFloatExpr(e.value)) lines.push(`if (typeof v === 'number') v = { bits: F.bitsFromFloat(v) };`);
          else lines.push(`if (typeof v === 'number' && !Number.isInteger(v)) v = { bits: F.bitsFromFloat(v) };`);
          if (e.has_addr) {
            const addr = [];
            const a = emitter.emitShared(e.addr, addr);
            lines.push(`${addr.join(' ')} const a = ${a};`);
            lines.push(`if (typeof v === 'number' && a !== null && typeof a === 'object' && a.raw !== undefined) F.rawStore(a.raw, a.off || 0, ${e.size}, v);`);
            lines.push(`else if (typeof v === 'number' && a !== null && typeof a === 'object' && a.obj !== undefined && a.obj.mem.flat === true) a.obj.mem.setWord(a.off || 0, ${e.size}, v);`);
            lines.push(`else d.storeThrough(a, ${e.size}, v, env);`);
          } else if (e.base === 'stack') {
            lines.push(`env.stack.store(${e.off || 0}, ${e.size}, v);`);
          } else {
            lines.push(`d.storeThrough(d.thisPointer(env, ${e.off || 0}), ${e.size}, v, env);`);
          }
          lines.push(`} catch (err) { d.effectError(err, E[${k}]); }`);
        } else if (e.kind === 'copy' || e.kind === 'fill') {
          lines.push(`try { d.runOfElements(E[${k}], env); } catch (err) { d.effectError(err, E[${k}]); }`);
        } else {
          lines.push(`d.applyEffectAt(E[${k}], env, state);`);
        }
      }
    };
    // Emits a node's effects and its items up to the unguarded path, each guarded child a call into its own function
    const emitNode = (lines, node) => {
      emitEffects(lines, node.effects);
      for (const item of node.items) {
        emitEffects(lines, item.effects);
        if (item.path) {
          const body = [];
          const v = emitter.emitShared(item.path.return, body);
          for (const l of body) lines.push(l);
          lines.push(`state.returned = ${v}; return state;`);
          return;
        }
        const g = guards.length;
        guards.push(item.guard);
        const c = nodes.length;
        nodes.push(pending(item.node, c));
        const body = [];
        const v = emitter.emitShared(item.guard, body);
        lines.push(`{ let g; try { ${body.join(' ')} g = ${v}; } catch (err) { d.guardError(err, G[${g}]); }`);
        lines.push(`if (typeof g === 'number' ? g !== 0 : !!g) { const r = C[${c}](env, state); if (r !== undefined) return r; } }`);
      }
      lines.push(`return undefined;`);
    };
    const compileNode = (node, c) => {
      const lines = [];
      emitter.newScope();
      emitNode(lines, node);
      const make = new Function('d', 'N', 'F', 'E', 'G', 'C', `return function paths${c}(env, state) {\n${lines.join('\n')}\n};`);
      return make(this, this.nodes, EXPR_HELPERS, effects, guards, nodes);
    };
    // A node stands as a function compiling it on its first run, which then takes its place
    const pending = (node, c) => (env, state) => {
      const f = compileNode(node, c);
      nodes[c] = f;
      return f(env, state);
    };
    nodes.push(pending(paths._trie, 0));
    const compiled = (env, state) => {
      const r = nodes[0](env, state);
      if (r === undefined) throw new Error('no execution path matches' + (env.mat ? ' for material ' + env.mat.name : ''));
      return r;
    };
    paths._compiled = compiled;
    return compiled;
  }

  budgetExceeded() {
    throw new Error(`the frame ran past ${FRAME_WORK_BUDGET} steps`);
  }

  // Applies one effect with the routine's work budget and the context an error in it names
  applyEffectAt(e, env, state) {
    try {
      this.applyEffect(e, env, state);
    } catch (err) {
      this.effectError(err, e);
    }
  }

  // Names the effect an error happened in and throws it on
  effectError(err, e) {
    if (!err.effect) { err.effect = e; err.message = `${err.message} (at ${hex8(e.va)} ${e.kind} ${e.fn || (e.base || '') + '+0x' + (e.off || 0).toString(16)})`; }
    throw err;
  }

  // Names the guard an error happened in and throws it on
  guardError(err, guard) {
    if (!err.guard) { err.guard = guard; err.message = `${err.message} (in guard ${this.show(guard)})`; }
    throw err;
  }

  // Walks the guard trie in path order, the effects before each guard applied first
  matchPath(node, env) {
    this.applyEffects(node.effects, env);
    for (const item of node.items) {
      this.applyEffects(item.effects, env);
      if (item.path) return item.path;
      let holds;
      try {
        holds = this.truthy(this.eval(item.guard, env));
      } catch (err) {
        if (!err.guard) { err.guard = item.guard; err.message = `${err.message} (in guard ${this.show(item.guard)})`; }
        throw err;
      }
      if (!holds) continue;
      const found = this.matchPath(item.node, env);
      if (found) return found;
    }
    return null;
  }

  // Applies effects in order, reading the loads taken as of each one just before it runs
  applyEffects(effects, env) {
    for (const e of effects) {
      if (++this.work > FRAME_WORK_BUDGET) throw new Error(`the frame ran past ${FRAME_WORK_BUDGET} steps`);
      if (e._early) this.readEarly(e._early, env);
      try {
        this.applyEffect(e, env, env.state);
      } catch (err) {
        if (!err.effect) { err.effect = e; err.message = `${err.message} (at ${hex8(e.va)} ${e.kind} ${e.fn || (e.base || '') + '+0x' + (e.off || 0).toString(16)})`; }
        throw err;
      }
    }
  }

  // Reads the loads later expressions take as of this effect, before it changes the memory they read
  readEarly(idxs, env) {
    for (const idx of idxs) {
      if (env.loads.has(idx)) continue;
      try {
        const v = this.evalLoad(this.nodes[idx], env);
        env.loads.set(idx, v instanceof Float32Array ? v.slice() : v);
      } catch (err) {
        env.loads.set(idx, new EarlyError(err));
      }
    }
  }

  // The loads an expression holds, by pool index, nested address loads included
  loadsIn(idx) {
    let found = this.loadMemo.get(idx);
    if (found) return found;
    const n = this.nodes[idx];
    const set = new Set();
    if (n.k === 'load') set.add(idx);
    for (const key of ['a', 'b', 'c', 'addr', 'this']) if (n[key] !== undefined) for (const x of this.loadsIn(n[key])) set.add(x);
    for (const key of ['args', 'elems']) if (n[key]) for (const x of n[key]) for (const y of this.loadsIn(x)) set.add(y);
    found = [...set];
    this.loadMemo.set(idx, found);
    return found;
  }

  // Marks a load on the effect it reads the memory as of when a later expression uses it
  markEarly(route, exprIdx, position) {
    for (const idx of this.loadsIn(exprIdx)) {
      const e = this.nodes[idx].e || 0;
      if (e >= position) continue;
      const at = route[e];
      if (!at._early) at._early = new Set();
      at._early.add(idx);
    }
  }

  // Marks the effects along a path's route with the loads its later expressions, guards and return take as of them
  annotatePath(p, route) {
    route.forEach((eff, k) => { for (const x of effectExprs(eff)) this.markEarly(route, x, k); });
    (p.guards || []).forEach((g, i) => this.markEarly(route, g, p.guard_pos[i]));
    if (p.return !== undefined) this.markEarly(route, p.return, route.length);
  }

  // Walks a path trie to each path, marking the effects it runs with the loads read as of them
  annotateTrie(node, route) {
    const here = route.concat(node.effects);
    for (const item of node.items) {
      const next = here.concat(item.effects);
      if (item.path) this.annotatePath(item.path, next);
      else this.annotateTrie(item.node, next);
    }
  }

  // Marks a lifted block's effects with the loads its later effects and its ending expressions take as of them
  annotateBlock(b) {
    const route = b.effects;
    route.forEach((eff, k) => { for (const x of effectExprs(eff)) this.markEarly(route, x, k); });
    const end = route.length;
    for (const idx of Object.values(b.regs)) this.markEarly(route, idx, end);
    if (b.esp !== undefined) this.markEarly(route, b.esp, end);
    for (const idx of b.fpu) this.markEarly(route, idx, end);
    for (const idx of Object.values(b.xmm || {})) this.markEarly(route, idx, end);
    for (const idx of Object.values(b.mm || {})) this.markEarly(route, idx, end);
    for (const idx of Object.values(b.flags || {})) if (idx >= 0) this.markEarly(route, idx, end);
    if (b.fcmp) { this.markEarly(route, b.fcmp.a, end); this.markEarly(route, b.fcmp.b, end); }
    for (const key of ['cond', 'index', 'value']) if (b.term[key] !== undefined) this.markEarly(route, b.term[key], end);
    b._marked = true;
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
        this.storeThrough(addr, e.size, value, env);
        return;
      }
      if (base === '') {
        this.memory.store(off, e.size, this.storable(value, e.size));
      } else if (base === 'this') {
        const p = this.thisPointer(env, off);
        this.storeThrough(p, e.size, value, env);
      } else if (base === 'node') {
        if (!env.node) throw new Error('store to node without an object');
        this.writeField(env.node, off, e.size === 8 ? { f64: this.scalar(value) } : (typeof value === 'object' && value !== null && value.bits === undefined ? value : this.int(value)), e.size);
      } else if (base === 'stack') {
        this.stackStore(env, off, e.size, value);
      } else if (base === 'fs') {
        this.fs.store(off, e.size, value);
      } else if (base.startsWith('res')) {
        this.bufferStore(env, Number(base.slice(3)), off, e.size, value);
      } else if (base.startsWith('g')) {
        const p = this.globalPointer(env, base, off);
        if (typeof p === 'number') {
          this.memory.store(p, e.size, this.storable(value, e.size));
        } else if (p.obj !== undefined) {
          this.writeField(p.obj, p.off, e.size === 8 ? { f64: this.scalar(value) } : (typeof value === 'object' && value !== null && value.bits === undefined ? value : this.int(value)), e.size);
        } else if (p.stack !== undefined) {
          this.stackStore(p.frame || env, p.stack, e.size, value);
        } else if (p.res !== undefined) {
          this.bufferStore(env, p.res, p.off, e.size, value);
        } else {
          throw new Error(`store through ${describe(p)} held by global ${base}`);
        }
      } else if (base.startsWith('new')) {
        const obj = this.allocatedObject(env, base);
        this.writeField(obj, off, e.size === 8 ? { f64: this.scalar(value) } : (typeof value === 'object' && value !== null && value.bits === undefined ? value : this.int(value)), e.size);
      } else if (env.mat && base === env.mat.object) {
        this.storeMaterialField(env.mat, off, e.size, e.size === 8 && typeof value === 'number' ? { f64: value } : value);
      } else {
        throw new Error(`store to ${base} is not mapped`);
      }
      return;
    }
    if (e.kind === 'copy' || e.kind === 'fill') { this.runOfElements(e, env); return; }
    const fn = e.fn;
    this.materializeBuffers(e, env);
    let cls = e._cls;
    if (cls === undefined) cls = e._cls = callClass(fn);
    // Value producers run where the path called them, a block they hand back landing where the pointer they
    // were given points, so the loads after the call read what the routine left
    if (cls === CALL_PRODUCER) {
      if (!env.results.has(e.seq)) env.results.set(e.seq, this.performCall(fn, e.args || [], e.has_this ? e.this : undefined, env, e.seq, e.target));
      const first = (e.args || [])[0];
      const dst = first === undefined ? undefined : this.eval(first, env);
      const v = env.results.get(e.seq);
      if (v instanceof Float32Array && dst && (dst.stack !== undefined || dst.obj !== undefined || dst.res !== undefined || dst.raw !== undefined)) this.storeThrough(dst, v.length * 4, v, env);
      return;
    }
    if (cls === CALL_ROUTINE) {
      // Methods, helpers and library calls run once, their results being memoized by call sequence.
      if (!env.results.has(e.seq)) env.results.set(e.seq, this.performCall(fn, e.args || [], e.has_this ? e.this : undefined, env, e.seq, e.target));
      return;
    }
    const args = (e.args || []).map((a) => this.eval(a, env));
    // A device call hands back nothing, which a path reading its result afterwards takes as success
    if (cls === CALL_DRAW) {
      this.deviceDraw(fn, args, env, state);
      if (!env.results.has(e.seq)) env.results.set(e.seq, 0);
      return;
    }
    if (this.deviceCall(fn, args, e, env, state)) {
      if (!env.results.has(e.seq)) env.results.set(e.seq, 0);
      return;
    }
    // A device routine the runtime stands in for nothing runs out of the image
    if (this.imageRoutine(fn, e.target) === undefined) throw new Error(`device call ${fn} is not mapped${this.scene.errors[fn] ? ': ' + this.scene.errors[fn] : ''}`);
    if (!env.results.has(e.seq)) env.results.set(e.seq, this.runImageRoutine(fn, args, e.args || [], e.has_this ? this.eval(e.this, env) : undefined, env, e.target));
  }

  // Issues a draw to the host with the primitive and the vertex range or the index run the call names
  deviceDraw(fn, args, env, state) {
    state.draws++;
    if (!this.onDraw) throw new Error('draw without a draw handler');
    if (fn === 'D3DDevice_DrawVertices') this.onDraw(state, env, { primitive: this.int(args[0]), start: this.int(args[1]), count: this.int(args[2]) });
    else if (fn === 'D3DDevice_DrawIndexedVertices') this.onDraw(state, env, { primitive: this.int(args[0]), count: this.int(args[1]), indices: args[2] });
    else throw new Error(`draw call ${fn} is not mapped`);
  }

  // The index buffer a handle names
  indexBuffer(v) {
    if (v && v.ib !== undefined) return v.ib;
    throw new Error(`${describe(v)} is no index buffer`);
  }

  // Applies a device call with its evaluated arguments to the device state
  deviceCall(fn, args, e, env, state) {
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
    } else if (fn === 'D3DDevice_SetPalette') {
      const stage = this.int(args[0]);
      if (this.int(args[1]) === 0) delete state.palettes[stage];
      else state.palettes[stage] = args[1];
    } else if (fn === 'D3DDevice_SetVertexShader') {
      state.program = args[0];
    } else if (fn.startsWith('D3DDevice_SetVertexShaderConstant')) {
      // The optimizer's own setters carry the register count in their name, the plain one takes it as an argument
      const rows = fn.slice('D3DDevice_SetVertexShaderConstant'.length);
      if (rows !== '' && !/^[0-9]+$/.test(rows)) throw new Error(`device call ${fn} is not mapped`);
      const reg = this.int(args[0]), count = rows === '' ? this.int(args[2]) : Number(rows);
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
      for (let i = 0; i < 16; i++) this.storeThrough(this.offsetPointer(args[1], 4 * i), 4, bitsFromFloat(m[i]), env);
      // The matrix the device handed back is what the path's later reads of the call stand for
      env.results.set(e.seq, Float32Array.from(m));
    } else if (fn === 'D3DDevice_SetStreamSource') {
      state.stream = args[1];
      state.streamStride = this.int(args[2]);
    } else if (fn === 'D3DDevice_SetIndices') {
      state.indices = args[0];
      state.baseVertex = this.int(args[1]);
      // The XDK keeps the index data pointer in a global of its own, which its inlined indexed draw offsets by the start index
      if (!this.draw.index_data) throw new Error('the model names no index data global');
      this.memory.store(this.draw.index_data, 4, this.int(args[0]) === 0 ? 0 : { raw: this.indexBuffer(args[0]), off: 0 });
    } else if (fn === 'D3DDevice_SetMaterial') {
      state.material = args[0];
    } else if (fn === 'D3DDevice_Swap') {
      this.host.onPresent(state);
    } else if (fn === 'D3DDevice_Clear') {
      state.clear = { flags: this.int(args[2]), color: this.int(args[3]) };
      this.host.onClear(state);
    } else if (fn === 'D3DDevice_Reset' || fn === 'D3DDevice_SetFlickerFilter' || fn === 'D3DDevice_SetSoftDisplayFilter' || fn === 'D3DDevice_BlockUntilVerticalBlank' || fn === 'D3DDevice_SetVerticalBlankCallback' || fn === 'D3DDevice_SetViewport' || fn === 'D3DDevice_SetGammaRamp' || fn === 'D3DDevice_PersistDisplay' || fn === 'D3DDevice_SetBackBufferScale' || fn === 'D3DDevice_SetScreenSpaceOffset' || fn === 'D3DDevice_SetOverscanColor') {
      return true;
    } else {
      return false;
    }
    return true;
  }

  // Copies or fills the run of elements a repeated string instruction moved, element by element through the
  // pointers, the loads after it reading what it left
  runOfElements(e, env) {
    const dst = this.eval(e.addr, env);
    const size = e.size;
    // The step is the element size, negative where the direction flag ran the instruction backwards
    const step = this.int(this.eval(e.args[1], env)) | 0;
    if (e.kind === 'copy') {
      const src = this.eval(e.args[0], env);
      const count = this.int(this.eval(e.value, env));
      for (let i = 0; i < count; i++) this.storeThrough(this.offsetPointer(dst, i * step), size, this.loadLive(env, this.offsetPointer(src, i * step), size), env);
      return;
    }
    const value = this.eval(e.value, env);
    const count = this.int(this.eval(e.args[0], env));
    for (let i = 0; i < count; i++) this.storeThrough(this.offsetPointer(dst, i * step), size, value, env);
  }

  // Writes the stack memory a call's pointer arguments refer to, which the path kept as expressions
  materializeBuffers(e, env) {
    for (const b of e.buffers || []) {
      const held = b.index < 0 ? e.this : (e.args || [])[b.index];
      const p = this.eval(held, env);
      if (!p || p.stack === undefined) continue;
      const stack = (p.frame || env).stack;
      let off = p.stack;
      for (let i = 0; i < b.elems.length; i++) {
        const x = b.elems[i];
        const v = this.eval(x, env);
        if (v instanceof Float32Array) { stack.store(off, v.length * 4, v); off += v.length * 4; continue; }
        const size = b.sizes[i];
        if (size === undefined) throw new Error(`buffer element ${i} of call ${e.fn} carries no size`);
        if (size < 4) stack.store(off, size, this.int(v) & (size === 1 ? 0xFF : 0xFFFF));
        else stack.store(off, size, typeof v === 'number' && this.isFloatExpr(x) ? bitsFromFloat(v) : v);
        off += size;
      }
    }
  }

  // Stores through a pointer the path computed, an object field, a global, a stack slot or locked memory
  storeThrough(addr, size, value, env) {
    if (addr && addr.obj !== undefined) {
      const bytes = value instanceof Float32Array || (value && (value.inline || value.f64 !== undefined));
      this.writeField(addr.obj, addr.off || 0, bytes ? value : (size === 8 ? { f64: this.scalar(value) } : (typeof value === 'object' && value !== null && value.bits === undefined ? value : this.int(value))), size);
      return;
    }
    if (typeof addr === 'number') { this.memory.store(addr, size, this.storable(value, size)); return; }
    if (addr && addr.stack !== undefined) { this.stackStore(addr.frame || env, addr.stack, size, value); return; }
    if (addr && addr.res !== undefined) { this.bufferStore(env, addr.res, addr.off || 0, size, value); return; }
    if (addr && addr.raw !== undefined) { rawStore(addr.raw, addr.off || 0, size, value); return; }
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
    if (r && r.obj !== undefined) { this.writeField(r.obj, (r.off || 0) + off, size === 8 ? { f64: this.scalar(value) } : (typeof value === 'object' && value !== null && value.bits === undefined ? value : this.int(value)), size); return; }
    if (typeof r === 'number') { this.memory.store(u32(r + off), size, this.storable(value, size)); return; }
    if (r && r.raw !== undefined) { rawStore(r.raw, (r.off || 0) + off, size, value); return; }
    throw new Error(`store into the result of call ${seq} which is ${describe(r)} rather than memory`);
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
      case 'un': return FLOAT_UN_OPS.has(n.op);
      case 'bin': return FLOAT_BIN_OPS.has(n.op);
      case 'sel': return this.isFloatExpr(n.a) && this.isFloatExpr(n.b);
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
      const before = p.effects.slice(from, pos);
      from = pos;
      let next = null;
      for (const item of node.items) if (item.guard === g) { next = item.node; break; }
      if (!next) { next = { items: [], effects: [] }; node.items.push({ guard: g, effects: before, node: next }); }
      node = next;
    }
    node.items.push({ path: p, effects: p.effects.slice(from) });
  }
  hoistEffects(root);
  return root;
}

// Classes of call effect, a value producer, a routine run once, a device draw or another device call
const CALL_PRODUCER = 1, CALL_ROUTINE = 2, CALL_DRAW = 3, CALL_DEVICE = 4;


// The class a call name falls in, decided once per effect
function callClass(fn) {
  if (fn.startsWith('D3DX') || fn.startsWith('XG') || fn.startsWith('D3DDevice_Create') || fn === 'Direct3DCreate8' || fn.startsWith('D3D::') || fn === 'D3DDevice_GetBackBuffer' || fn === 'D3DResource_Release') return CALL_PRODUCER;
  if (fn.startsWith('vcall@') || fn.startsWith('sub_') || !fn.startsWith('D3DDevice_')) return CALL_ROUTINE;
  return fn.startsWith('D3DDevice_Draw') ? CALL_DRAW : CALL_DEVICE;
}

// The expressions an effect evaluates, its value, address, receiver, arguments and buffer elements
function effectExprs(e) {
  const out = [];
  if (e.value !== undefined) out.push(e.value);
  if (e.has_addr) out.push(e.addr);
  if (e.has_this) out.push(e.this);
  for (const a of e.args || []) out.push(a);
  for (const b of e.buffers || []) for (const x of b.elems) out.push(x);
  return out;
}

// Lifts the effects every item of a node shares onto the node, leaving each item the ones only it runs
function hoistEffects(node) {
  let shared = node.items.length ? node.items[0].effects.length : 0;
  for (const item of node.items) shared = Math.min(shared, item.effects.length);
  node.effects = node.items.length ? node.items[0].effects.slice(0, shared) : [];
  for (const item of node.items) {
    item.effects = item.effects.slice(shared);
    if (item.node) hoistEffects(item.node);
  }
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
  return v !== null && typeof v === 'object' && !(v instanceof Float32Array) && v.bits === undefined && v.f64 === undefined && v.sum === undefined && !(v.str !== undefined && v.inline);
}

// A pointer of a base's kind at an offset into it
function basePointer(base, off) {
  if (base.obj !== undefined) return { obj: base.obj, off };
  if (base.stack !== undefined) return { stack: off, frame: base.frame };
  if (base.str !== undefined) return { str: base.str, chars: off / 2 };
  if (base.raw !== undefined) return { raw: base.raw, off };
  return { res: base.res, off };
}

// Reports whether two evaluated values are the same number, block or handle
function sameValue(a, b) {
  if (typeof a === 'number' || typeof b === 'number') return typeof a === typeof b && (Object.is(a, b) || a === b);
  if (a instanceof Float32Array || b instanceof Float32Array) return a instanceof Float32Array && b instanceof Float32Array && a.length === b.length && a.every((v, i) => Object.is(v, b[i]) || v === b[i]);
  if (a === b) return true;
  if (!a || !b) return false;
  if (a.bits !== undefined || b.bits !== undefined) return a.bits === b.bits;
  if (a.f64 !== undefined || b.f64 !== undefined) return a.f64 === b.f64 || (Number.isNaN(a.f64) && Number.isNaN(b.f64));
  return describe(a) === describe(b);
}

function sameHandle(a, b) {
  if (a.obj !== undefined || b.obj !== undefined) return a.obj === b.obj && (a.off || 0) === (b.off || 0);
  if (a.raw !== undefined || b.raw !== undefined) return a.raw === b.raw && (a.off || 0) === (b.off || 0);
  if (a.ib !== undefined || b.ib !== undefined) return a.ib === b.ib;
  if (a.d3dxMesh !== undefined || b.d3dxMesh !== undefined) return a.d3dxMesh === b.d3dxMesh;
  if (a.material !== undefined || b.material !== undefined) return a.material === b.material;
  if (a.vb !== undefined || b.vb !== undefined) return a.vb === b.vb;
  if (a.matrixStack !== undefined || b.matrixStack !== undefined) return a.matrixStack === b.matrixStack;
  if (a.texture !== undefined || b.texture !== undefined) return a.texture === b.texture;
  if (a.str !== undefined || b.str !== undefined) return a.str === b.str && (a.chars || 0) === (b.chars || 0);
  return a === b;
}

function describe(v) {
  if (v instanceof Float32Array) return `vec${v.length}`;
  if (v && v.sum !== undefined) return `${[...v.sum.values()].map((t) => `${t.coef > 0 ? '+' : ''}${t.coef}*${describe(basePointer(t.base, 0))}`).join('')}${v.off < 0 ? '-' : '+'}0x${Math.abs(v.off).toString(16)}`;
  if (v && v.obj !== undefined) return `${v.obj.type}#${v.obj.id}+${(v.off || 0).toString(16)}`;
  if (v && v.raw !== undefined) return `raw#${v.raw.id}+0x${(v.off || 0).toString(16)}`;
  if (v && v.vb !== undefined) return `vb#${v.vb.id}`;
  if (v && v.ib !== undefined) return `ib#${v.ib.id}`;
  if (v && v.d3dxMesh !== undefined) return `d3dxmesh#${v.d3dxMesh.vb.id}`;
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

// D3DXVec3CatmullRom, the spline through four points at a weight between the middle two
export function vecCatmullRom(p0, p1, p2, p3, s, comps) {
  const out = new Float32Array(comps);
  for (let i = 0; i < comps; i++) {
    out[i] = 0.5 * (2 * p1[i] + (p2[i] - p0[i]) * s + (2 * p0[i] - 5 * p1[i] + 4 * p2[i] - p3[i]) * s * s + (p3[i] - p0[i] + 3 * (p1[i] - p2[i])) * s * s * s);
  }
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
