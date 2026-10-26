// The processor the kernel runs on: the lifted blocks of the 2BL and the kernel run over physical memory through
// the page tables the kernel builds, with the stack, the interrupt frames and every thread's context in that memory

const u32 = (v) => v >>> 0;
const hex8 = (v) => u32(v).toString(16).toUpperCase().padStart(8, '0');
const bitsBuf = new DataView(new ArrayBuffer(8));
const floatFromBits = (b) => { bitsBuf.setUint32(0, u32(b), true); return bitsBuf.getFloat32(0, true); };
const bitsFromFloat = (f) => { bitsBuf.setFloat32(0, f, true); return bitsBuf.getUint32(0, true); };
function roundEven(v) {
  const r = Math.round(v);
  return Math.abs(v % 1) === 0.5 && r % 2 !== 0 ? r - 1 : r;
}
function bsf(v) { if (v === 0) return 0; let i = 0; while (((v >>> i) & 1) === 0) i++; return i; }
function bsr(v) { if (v === 0) return 0; return 31 - Math.clz32(v); }
function parity(v) { let p = 1; for (let b = v & 0xFF; b; b >>= 1) p ^= b & 1; return p; }

const FLOAT_BIN = new Set(['fadd', 'fsub', 'fmul', 'fdiv', 'fmin', 'fmax', 'fmod', 'fpow', 'fatan2', 'frem']);
const FLOAT_UN = new Set(['f32', 'f64', 'bits2f', 'bits2d', 'i2f', 'fneg', 'fabs', 'fsin', 'fcos', 'fsqrt', 'ftan', 'ffloor', 'fceil', 'fexp', 'flog', 'flog10', 'flog2', 'ftrunc', 'fround', 'fatan', 'fasin', 'facos', 'fsinh', 'fcosh', 'ftanh']);

// Flag record kinds as the lifter numbers them for the flags word
const FLAG_KINDS = ['', 'cmp', 'sub', 'add', 'logic', 'alu', 'eflags', 'machine'];
const EFLAGS_CF = 1, EFLAGS_PF = 4, EFLAGS_ZF = 0x40, EFLAGS_SF = 0x80, EFLAGS_IF = 0x200, EFLAGS_DF = 0x400, EFLAGS_OF = 0x800;
const CR0_PG = 0x80000000;
const PAGE_SHIFT = 12, PAGE_SIZE = 4096;
// Sentinel return addresses a call made from outside the machine comes back to, one per nesting level
const SENTINEL_BASE = 0xFFFFFF00;

// The exception vectors that push an error code
const ERROR_CODE_VECTORS = new Set([8, 10, 11, 12, 13, 14, 17]);

export class MachineFault extends Error {
  constructor(message, m) {
    super(message);
    this.state = m ? m.describe() : '';
  }
}

export class Machine {
  constructor(model, hardware, bus) {
    this.blocks = model.blocks;
    this.errors = model.errors;
    this.nodes = model.exprs.nodes;
    // Code the kernel copies and patches runs from the copy, its blocks reading the copy through this address
    this.templates = (model.templates || []).map((t) => ({ ...t, bytes: Uint8Array.from(atob(t.bytes), (c) => c.charCodeAt(0)) }));
    this.self = 0;
    this.hardware = hardware;
    this.ramSize = hardware.memory;
    this.ram = new Uint8Array(this.ramSize);
    this.dv = new DataView(this.ram.buffer);
    this.bus = bus;
    this.regs = { EAX: 0, ECX: 0, EDX: 0, EBX: 0, ESP: 0, EBP: 0, ESI: 0, EDI: 0 };
    this.segs = { CS: 0, DS: 0, ES: 0, SS: 0, FS: 0, GS: 0 };
    this.segbase = { FS: 0, GS: 0 };
    this.cr = [0, 0, 0, 0, 0];
    this.gdtr = { base: 0, limit: 0 };
    this.idtr = { base: 0, limit: 0 };
    this.tr = 0;
    this.msrs = new Map();
    this.IF = false;
    this.DF = false;
    this.fl = { kind: '', a: 0, b: 0, r: 0, cf: 0, fswA: 0, fswB: 0 };
    this.fcmp = null;
    this.fpu = [0, 0, 0, 0, 0, 0, 0, 0];
    this.fcw = 0x037F;
    this.xmm = Array.from({ length: 8 }, () => [0, 0, 0, 0]);
    this.mm = Array.from({ length: 8 }, () => [0, 0]);
    this.tsc = 0;
    this.cyclesPerInsn = hardware.cycles_per_instruction;
    this.nextEvent = Infinity;
    this.timers = [];
    this.irq = false;
    this.halted = false;
    this.compiled = new Map();
    this.hooks = new Map();
    this.sentinels = 0;
    this.stopAt = null;
    this.trace = null;
    this.blocksRun = 0;
    this.tlb = new Int32Array(1 << 20);
    this.tlbFlags = new Uint8Array(1 << 20);
    this.key = null;
    this.watch = null;
    bus.attach(this);
  }

  // Everything the machine shows when it fails, for the error it raises
  describe() {
    const r = this.regs;
    return `at ${this.key} eax=${hex8(r.EAX)} ecx=${hex8(r.ECX)} edx=${hex8(r.EDX)} ebx=${hex8(r.EBX)} esp=${hex8(r.ESP)} ebp=${hex8(r.EBP)} esi=${hex8(r.ESI)} edi=${hex8(r.EDI)} if=${this.IF ? 1 : 0} cr0=${hex8(this.cr[0])} cr3=${hex8(this.cr[3])} tsc=${this.tsc} blocks=${this.blocksRun}`;
  }

  fault(message) {
    throw new MachineFault(`${message} ${this.describe()}`, null);
  }

  // Paging and physical access

  get paging() { return (this.cr[0] & CR0_PG) !== 0; }

  flushTLB() {
    this.tlb.fill(0);
    this.tlbFlags.fill(0);
  }

  // The physical address a virtual address reaches, through the page tables in memory while paging is on, and the
  // alias of memory a device window opens where the physical address lies past memory
  translate(va, write) {
    va >>>= 0;
    if (!this.paging) return this.aliased(va);
    const page = va >>> PAGE_SHIFT;
    const hit = this.tlb[page];
    if (hit !== 0 && (!write || (this.tlbFlags[page] & 2) !== 0)) return ((hit - 1) * PAGE_SIZE + (va & 0xFFF)) >>> 0;
    const pdeAddr = ((this.cr[3] & 0xFFFFF000) >>> 0) + (va >>> 22) * 4;
    const pde = this.physRead4(pdeAddr);
    if ((pde & 1) === 0) this.pageFault(va, write, false);
    let pa, pteAddr = 0, pte = 0;
    if (pde & 0x80) {
      pa = ((pde & 0xFFC00000) | (va & 0x3FFFFF)) >>> 0;
      if ((pde & 0x20) === 0 || (write && (pde & 0x40) === 0)) this.physWrite4(pdeAddr, pde | 0x20 | (write ? 0x40 : 0));
    } else {
      pteAddr = ((pde & 0xFFFFF000) >>> 0) + ((va >>> 12) & 0x3FF) * 4;
      pte = this.physRead4(pteAddr);
      if ((pte & 1) === 0) this.pageFault(va, write, false);
      pa = ((pte & 0xFFFFF000) | (va & 0xFFF)) >>> 0;
      if ((pde & 0x20) === 0) this.physWrite4(pdeAddr, pde | 0x20);
      if ((pte & 0x20) === 0 || (write && (pte & 0x40) === 0)) this.physWrite4(pteAddr, pte | 0x20 | (write ? 0x40 : 0));
    }
    const writable = pde & 0x80 ? (pde & 2) !== 0 : (pte & 2) !== 0;
    if (write && !writable) this.pageFault(va, write, true);
    const target = this.aliased(pa);
    this.tlb[page] = (target >>> PAGE_SHIFT) + 1;
    // A translation is kept only once its dirty bit is set, so a later write sets the bit through a miss
    this.tlbFlags[page] = (writable && (write || (pde & 0x80 ? pde : pte) & 0x40) ? 2 : 0) | 1;
    return (target & 0xFFFFF000 | (va & 0xFFF)) >>> 0;
  }

  // A physical address past memory that a device window aliases onto memory comes back inside memory
  aliased(pa) {
    if (pa < this.ramSize) return pa;
    const alias = this.bus.ramAlias(pa);
    return alias < 0 ? pa : alias;
  }

  pageFault(va, write, present) {
    this.fault(`page fault ${write ? 'writing' : 'reading'} ${hex8(va)}${present ? ' (protection)' : ' (not present)'} cr3=${hex8(this.cr[3])}`);
  }

  physRead4(pa) {
    pa = this.aliased(pa >>> 0);
    if (pa + 4 <= this.ramSize) return this.dv.getUint32(pa, true);
    return this.bus.read(pa, 4);
  }

  physWrite4(pa, v) {
    pa = this.aliased(pa >>> 0);
    if (pa + 4 <= this.ramSize) { this.dv.setUint32(pa, v >>> 0, true); return; }
    this.bus.write(pa, 4, v >>> 0);
  }

  // Loads and stores through virtual addresses, an access crossing a page or reaching a device going byte by byte

  // A device access names the virtual address beside the physical one when it fails
  deviceRead(va, pa, size) {
    try {
      return this.bus.read(pa, size);
    } catch (err) {
      err.message = `${err.message} (virtual ${hex8(va)})`;
      throw err;
    }
  }

  deviceWrite(va, pa, size, v) {
    try {
      this.bus.write(pa, size, v);
    } catch (err) {
      err.message = `${err.message} (virtual ${hex8(va)})`;
      throw err;
    }
  }

  ld1(va) {
    const pa = this.translate(va, false);
    return pa < this.ramSize ? this.ram[pa] : this.deviceRead(va, pa, 1);
  }

  ld2(va) {
    if ((va & 0xFFF) <= 0xFFE) {
      const pa = this.translate(va, false);
      if (pa + 2 <= this.ramSize) return this.dv.getUint16(pa, true);
      return this.deviceRead(va, pa, 2);
    }
    return (this.ld1(va) | (this.ld1(va + 1) << 8)) >>> 0;
  }

  ld4(va) {
    if ((va & 0xFFF) <= 0xFFC) {
      const pa = this.translate(va, false);
      if (pa + 4 <= this.ramSize) return this.dv.getUint32(pa, true);
      return this.deviceRead(va, pa, 4);
    }
    return (this.ld2(va) | (this.ld2(va + 2) << 16)) >>> 0;
  }

  ldf64(va) {
    if ((va & 0xFFF) <= 0xFF8) {
      const pa = this.translate(va, false);
      if (pa + 8 <= this.ramSize) return this.dv.getFloat64(pa, true);
    }
    bitsBuf.setUint32(0, this.ld4(va), true);
    bitsBuf.setUint32(4, this.ld4(va + 4), true);
    return bitsBuf.getFloat64(0, true);
  }

  // A run of single precision lanes, as the vector instructions read memory
  ldvec(va, size) {
    const out = new Array(size / 4);
    for (let i = 0; i < out.length; i++) out[i] = floatFromBits(this.ld4(va + 4 * i));
    return out;
  }

  st1(va, v) {
    const pa = this.translate(va, true);
    if (this.watch !== null && this.watch.has(pa & ~3)) this.watch.get(pa & ~3)(this, va, v & 0xFF);
    if (pa < this.ramSize) this.ram[pa] = v & 0xFF; else this.deviceWrite(va, pa, 1, v & 0xFF);
  }

  st2(va, v) {
    if ((va & 0xFFF) <= 0xFFE) {
      const pa = this.translate(va, true);
      if (this.watch !== null && this.watch.has(pa & ~3)) this.watch.get(pa & ~3)(this, va, v & 0xFFFF);
      if (pa + 2 <= this.ramSize) { this.dv.setUint16(pa, v & 0xFFFF, true); return; }
      this.deviceWrite(va, pa, 2, v & 0xFFFF);
      return;
    }
    this.st1(va, v);
    this.st1(va + 1, v >>> 8);
  }

  st4(va, v) {
    if ((va & 0xFFF) <= 0xFFC) {
      const pa = this.translate(va, true);
      if (this.watch !== null && this.watch.has(pa)) this.watch.get(pa)(this, va, v >>> 0);
      if (pa + 4 <= this.ramSize) { this.dv.setUint32(pa, v >>> 0, true); return; }
      this.deviceWrite(va, pa, 4, v >>> 0);
      return;
    }
    this.st2(va, v);
    this.st2(va + 2, v >>> 16);
  }

  stf64(va, f) {
    bitsBuf.setFloat64(0, f, true);
    this.st4(va, bitsBuf.getUint32(0, true));
    this.st4(va + 4, bitsBuf.getUint32(4, true));
  }

  stvec(va, lanes) {
    for (let i = 0; i < lanes.length; i++) this.st4(va + 4 * i, bitsFromFloat(lanes[i]));
  }

  ld(va, size) {
    switch (size) {
      case 1: return this.ld1(va);
      case 2: return this.ld2(va);
      case 4: return this.ld4(va);
    }
    this.fault(`load of ${size} bytes`);
  }

  st(va, size, v) {
    switch (size) {
      case 1: this.st1(va, v); return;
      case 2: this.st2(va, v); return;
      case 4: this.st4(va, v); return;
    }
    this.fault(`store of ${size} bytes`);
  }

  // The repeated string moves, each element moved in turn so an overlapping copy behaves as the processor's does
  copy(dst, src, count, size, step) {
    const s = step | 0;
    for (let i = 0; i < count; i++) {
      this.st(dst, size, this.ld(src, size));
      dst = u32(dst + s);
      src = u32(src + s);
    }
  }

  fill(dst, value, count, size, step) {
    const s = step | 0;
    if (size === 4 && s === 4 && count > 0 && (dst & 3) === 0) {
      // A forward word fill of memory goes a page at a time
      let left = count;
      while (left > 0) {
        const n = Math.min(left, (PAGE_SIZE - (dst & 0xFFF)) >>> 2);
        const pa = this.translate(dst, true);
        if (pa + 4 * n > this.ramSize) { this.st4(dst, value); dst = u32(dst + 4); left--; continue; }
        if (value === 0) this.ram.fill(0, pa, pa + 4 * n);
        else for (let i = 0; i < n; i++) this.dv.setUint32(pa + 4 * i, value, true);
        dst = u32(dst + 4 * n);
        left -= n;
      }
      return;
    }
    for (let i = 0; i < count; i++) {
      this.st(dst, size, value);
      dst = u32(dst + s);
    }
  }

  // The segment base a selector names, from the descriptor table the processor holds
  descriptorBase(sel) {
    if ((sel & 0xFFF8) === 0) return 0;
    const at = this.gdtr.base + (sel & 0xFFF8);
    if ((sel & 0xFFF8) + 7 > this.gdtr.limit) this.fault(`selector ${sel.toString(16)} outside the descriptor table`);
    return u32(this.ld2(at + 2) | (this.ld1(at + 4) << 16) | (this.ld1(at + 7) << 24));
  }

  setSegment(name, sel) {
    this.segs[name] = sel & 0xFFFF;
    if (name === 'FS' || name === 'GS') this.segbase[name] = this.descriptorBase(sel & 0xFFFF);
  }

  // Flags

  // The flags word the record stands for, with the interrupt and direction flags the machine keeps
  eflags() {
    const f = this.fl;
    let w = 2 | (this.IF ? EFLAGS_IF : 0) | (this.DF ? EFLAGS_DF : 0);
    if (f.kind === 'eflags') return u32((f.a & ~(EFLAGS_IF | EFLAGS_DF)) | w);
    if (f.kind === '') return w;
    const r = f.r >>> 0;
    if (r === 0) w |= EFLAGS_ZF;
    if (r & 0x80000000) w |= EFLAGS_SF;
    if (parity(r)) w |= EFLAGS_PF;
    if (this.carry()) w |= EFLAGS_CF;
    if (this.overflow()) w |= EFLAGS_OF;
    return u32(w);
  }

  // Takes a flags word as the record, the interrupt and direction flags with it
  setEflags(w) {
    this.IF = (w & EFLAGS_IF) !== 0;
    this.DF = (w & EFLAGS_DF) !== 0;
    this.fl = { kind: 'eflags', a: u32(w), b: 0, r: 0, cf: 0, fswA: 0, fswB: 0 };
  }

  carry() {
    const f = this.fl;
    switch (f.kind) {
      case 'cmp': case 'sub': return (f.a >>> 0) < (f.b >>> 0);
      case 'add': return (f.r >>> 0) < (f.a >>> 0);
      case 'test': case 'and': case 'or': case 'xor': case 'logic': return false;
      case 'alu': return f.cf !== 0;
      case 'eflags': return (f.a & EFLAGS_CF) !== 0;
      case 'fsw': { const a = f.fswA, b = f.fswB; return a !== a || b !== b || a < b; }
    }
    this.fault(`carry after ${f.kind || 'no'} flags`);
  }

  overflow() {
    const f = this.fl;
    switch (f.kind) {
      case 'add': return ((((f.a ^ f.r) & (f.b ^ f.r)) >>> 31) & 1) === 1;
      case 'sub': case 'cmp': return ((((f.a ^ f.b) & (f.a ^ f.r)) >>> 31) & 1) === 1;
      case 'test': case 'and': case 'or': case 'xor': case 'logic': case 'alu': case 'fsw': return false;
      case 'eflags': return (f.a & EFLAGS_OF) !== 0;
    }
    this.fault(`overflow after ${f.kind || 'no'} flags`);
  }

  // A condition a block entered from anywhere decides from the record the last flag setter left
  condition(op) {
    const f = this.fl;
    if (f.kind === 'eflags') {
      const w = f.a;
      const zf = (w & EFLAGS_ZF) !== 0, cf = (w & EFLAGS_CF) !== 0, sf = (w & EFLAGS_SF) !== 0, of = (w & EFLAGS_OF) !== 0, pf = (w & EFLAGS_PF) !== 0;
      switch (op) {
        case 'JE': return zf; case 'JNE': return !zf; case 'JB': return cf; case 'JAE': return !cf;
        case 'JBE': return cf || zf; case 'JA': return !(cf || zf); case 'JS': return sf; case 'JNS': return !sf;
        case 'JP': return pf; case 'JNP': return !pf; case 'JO': return of; case 'JNO': return !of;
        case 'JL': return sf !== of; case 'JGE': return sf === of; case 'JLE': return zf || sf !== of; case 'JG': return !zf && sf === of;
      }
      this.fault(`condition ${op}`);
    }
    if (f.kind === '' || f.kind === 'machine') this.fault(`condition ${op} with no flags`);
    const cmp = f.kind === 'cmp' || f.kind === 'sub';
    const a = f.a | 0, b = f.b | 0, r = f.r | 0;
    const zero = cmp ? (f.a >>> 0) === (f.b >>> 0) : r === 0;
    const sign = r < 0;
    switch (op) {
      case 'JE': return zero;
      case 'JNE': return !zero;
      case 'JL': return cmp ? a < b : sign;
      case 'JGE': return cmp ? a >= b : !sign;
      case 'JLE': return cmp ? a <= b : r <= 0;
      case 'JG': return cmp ? a > b : r > 0;
      case 'JB': return cmp ? (f.a >>> 0) < (f.b >>> 0) : this.carry();
      case 'JAE': return cmp ? (f.a >>> 0) >= (f.b >>> 0) : !this.carry();
      case 'JBE': return cmp ? (f.a >>> 0) <= (f.b >>> 0) : this.carry() || zero;
      case 'JA': return cmp ? (f.a >>> 0) > (f.b >>> 0) : !(this.carry() || zero);
      case 'JS': return sign;
      case 'JNS': return !sign;
      case 'JP': return parity(r >>> 0) === 1;
      case 'JNP': return parity(r >>> 0) === 0;
      case 'JO': return this.overflow();
      case 'JNO': return !this.overflow();
    }
    this.fault(`condition ${op}`);
  }

  // The x87 status word the last compare left, its condition codes set for less, unordered and equal
  statusWord() {
    const f = this.fcmp;
    if (f === null) return 0;
    const a = f.a, b = f.b;
    if (a !== a || b !== b) return 0x400;
    return a < b ? 0x100 : (a === b ? 0x4000 : 0);
  }

  // The flags word a pushfd leaves, built from the record the lifter handed it
  flagsWord(kind, a, b, r, cf) {
    const name = FLAG_KINDS[kind];
    if (name === 'machine' || name === undefined) return this.eflags();
    const saved = this.fl;
    this.fl = { kind: name, a, b, r, cf, fswA: 0, fswB: 0 };
    const w = this.eflags();
    this.fl = saved;
    return w;
  }

  // Processor operations the blocks call

  cpu(fn, args, seq) {
    switch (fn) {
      case 'cpu_cli': this.IF = false; return 0;
      case 'cpu_sti': this.IF = true; return 0;
      case 'cpu_cld': this.DF = false; return 0;
      case 'cpu_std': this.DF = true; return 0;
      case 'cpu_clts': this.cr[0] &= ~8; return 0;
      case 'cpu_pushfd': return this.flagsWord(args[0], args[1], args[2], args[3], args[4]);
      case 'cpu_popfd': this.setEflags(args[0]); return 0;
      case 'cpu_invlpg': { const page = args[0] >>> PAGE_SHIFT; this.tlb[page] = 0; this.tlbFlags[page] = 0; return 0; }
      case 'cpu_wrmsr': this.msrs.set(args[0] >>> 0, [args[1] >>> 0, args[2] >>> 0]); return 0;
      case 'cpu_rdmsr_lo': return this.msr(args[0])[0];
      case 'cpu_rdmsr_hi': return this.msr(args[0])[1];
      case 'cpu_rdtsc_lo': return u32(this.tsc % 0x100000000);
      case 'cpu_rdtsc_hi': return u32(Math.floor(this.tsc / 0x100000000));
      case 'cpu_cpuid_EAX': case 'cpu_cpuid_EBX': case 'cpu_cpuid_ECX': case 'cpu_cpuid_EDX': return this.cpuid(args[0], fn.slice(10));
      case 'cpu_lgdt': this.gdtr = { limit: this.ld2(args[0]), base: this.ld4(args[0] + 2) }; return 0;
      case 'cpu_lidt': this.idtr = { limit: this.ld2(args[0]), base: this.ld4(args[0] + 2) }; return 0;
      case 'cpu_ltr': this.tr = args[0] & 0xFFFF; return 0;
      case 'cpu_write_cr': this.writeCR(args[0], args[1]); return 0;
      case 'cpu_read_cr': return this.cr[args[0]] >>> 0;
      case 'cpu_fxsave': this.fxsave(args[0]); return 0;
      case 'cpu_fxrstor': this.fxrstor(args[0]); return 0;
      case 'cpu_fnsave': this.fnsave(args[0]); return 0;
      case 'cpu_frstor': this.frstor(args[0]); return 0;
      case 'cpu_frstor_st': return this.fpu[args[1]];
      case 'cpu_fnstcw': return this.fcw;
      case 'cpu_fldcw': this.fcw = args[0] & 0xFFFF; return 0;
      case 'cpu_fninit': this.fcw = 0x037F; this.fpu.fill(0); return 0;
      case 'cpu_fnclex': return 0;
      case 'port_in': return this.bus.portIn(args[0] & 0xFFFF, args[1]);
      case 'port_out': this.bus.portOut(args[0] & 0xFFFF, args[1], args[2]); return 0;
      case 'cpu_ins': case 'cpu_outs': return this.stringIO(fn === 'cpu_ins', args);
      case 'cpu_scas': return this.scan(args);
      case 'cpu_cmps': return this.compareStrings(args);
    }
    this.fault(`processor operation ${fn}`);
  }

  msr(index) {
    const v = this.msrs.get(index >>> 0);
    if (v === undefined) return [0, 0];
    return v;
  }

  cpuid(leaf, reg) {
    const row = this.hardware.cpuid[String(leaf >>> 0)];
    if (!row) this.fault(`cpuid leaf ${hex8(leaf)}`);
    return row[reg] >>> 0;
  }

  writeCR(n, v) {
    v >>>= 0;
    if (n === 0 || n === 3 || n === 4) this.flushTLB();
    this.cr[n] = v;
  }

  // The x87 and vector state saved and restored as the thread switch keeps it, each register as its double
  fxsave(at) {
    this.st2(at, this.fcw);
    this.st2(at + 2, 0);
    for (let i = 0; i < 8; i++) this.stf64(at + 32 + 16 * i, this.fpu[i]);
    for (let i = 0; i < 8; i++) for (let k = 0; k < 4; k++) this.st4(at + 160 + 16 * i + 4 * k, bitsFromFloat(this.xmm[i][k]));
  }

  fxrstor(at) {
    this.fcw = this.ld2(at);
    for (let i = 0; i < 8; i++) this.fpu[i] = this.ldf64(at + 32 + 16 * i);
    for (let i = 0; i < 8; i++) for (let k = 0; k < 4; k++) this.xmm[i][k] = floatFromBits(this.ld4(at + 160 + 16 * i + 4 * k));
  }

  fnsave(at) {
    this.st4(at, this.fcw);
    for (let i = 0; i < 8; i++) this.stf64(at + 28 + 10 * i, this.fpu[i]);
    this.fcw = 0x037F;
    this.fpu.fill(0);
  }

  frstor(at) {
    this.fcw = this.ld4(at) & 0xFFFF;
    for (let i = 0; i < 8; i++) this.fpu[i] = this.ldf64(at + 28 + 10 * i);
  }

  stringIO(input, args) {
    const [port, ptr, count, size, step] = args;
    let p = ptr >>> 0;
    for (let i = 0; i < count; i++) {
      if (input) this.st(p, size, this.bus.portIn(port & 0xFFFF, size));
      else this.bus.portOut(port & 0xFFFF, this.ld(p, size), size);
      p = u32(p + (step | 0));
    }
    return 0;
  }

  // Elements a repeated scan passes before it stops, the count exhausted or the match decided by the prefix
  scan(args) {
    const [ptr, value, count, size, step, repne] = args;
    const mask = size === 4 ? 0xFFFFFFFF : (size === 2 ? 0xFFFF : 0xFF);
    let p = ptr >>> 0;
    for (let i = 0; i < count; i++) {
      const equal = ((this.ld(p, size) ^ value) & mask) === 0;
      p = u32(p + (step | 0));
      if (repne ? equal : !equal) return i + 1;
    }
    return count >>> 0;
  }

  compareStrings(args) {
    const [src, dst, count, size, step, repne] = args;
    const mask = size === 4 ? 0xFFFFFFFF : (size === 2 ? 0xFFFF : 0xFF);
    let s = src >>> 0, d = dst >>> 0;
    for (let i = 0; i < count; i++) {
      const equal = ((this.ld(s, size) ^ this.ld(d, size)) & mask) === 0;
      s = u32(s + (step | 0));
      d = u32(d + (step | 0));
      if (repne ? equal : !equal) return i + 1;
    }
    return count >>> 0;
  }

  // Control flow

  // Registers a routine the loader answers itself, the function performing its return
  hook(va, fn) {
    this.hooks.set(va >>> 0, fn);
    this.compiled.clear();
  }

  push(v) {
    this.regs.ESP = u32(this.regs.ESP - 4);
    this.st4(this.regs.ESP, v);
  }

  pop() {
    const v = this.ld4(this.regs.ESP);
    this.regs.ESP = u32(this.regs.ESP + 4);
    return v;
  }

  // Returns from a hooked routine, popping the return address and the bytes the routine removes
  ret(pops = 0) {
    const to = this.pop();
    this.regs.ESP = u32(this.regs.ESP + pops);
    return this.dispatch(to);
  }

  // The block an address lands in, a hooked address running its replacement instead
  dispatch(addr) {
    addr >>>= 0;
    const hook = this.hooks.get(addr);
    if (hook !== undefined) return hook(this);
    if (addr >= SENTINEL_BASE) {
      this.stopAt = addr;
      return null;
    }
    const key = hex8(addr);
    if (this.blocks[key] === undefined) {
      const copied = this.templateAt(addr);
      if (copied !== null) {
        this.self = addr;
        return hex8(copied.base);
      }
      const why = this.errors[key];
      this.fault(why ? `block ${key} was not lifted: ${why}` : `no block at ${key}`);
    }
    return key;
  }

  // The template whose bytes lie at an address, the patched slots left out of the comparison
  templateAt(addr) {
    for (const t of this.templates) {
      let matches = true;
      for (let i = 0; i < t.length && matches; i++) {
        if (t.slots.some((s) => i >= s && i < s + 4)) continue;
        if (this.ld1(addr + i) !== t.bytes[i]) matches = false;
      }
      if (matches) return t;
    }
    return null;
  }

  // A halt waits for the next interrupt, the clock running forward to the event that raises it
  halt(next) {
    this.halted = true;
    return next;
  }

  // A software interrupt or a fault enters the handler the interrupt table names, the address given as the return
  softint(vector, returnTo) {
    this.regs.EIP = returnTo;
    return this.deliver(vector, false);
  }

  // Enters the handler of a vector with the flags, the code segment and the return address pushed, an exception
  // raised by the processor pushing its error code as well
  deliver(vector, external, errorCode = 0) {
    const at = this.idtr.base + vector * 8;
    if (vector * 8 + 7 > this.idtr.limit) this.fault(`interrupt ${vector} outside the interrupt table`);
    const lo = this.ld2(at), sel = this.ld2(at + 2), flags = this.ld1(at + 5), hi = this.ld2(at + 6);
    if ((flags & 0x80) === 0) this.fault(`interrupt ${vector} names no present gate`);
    const type = flags & 0x1F;
    const handler = u32(lo | (hi << 16));
    this.push(this.eflags());
    this.push(this.segs.CS);
    this.push(this.regs.EIP);
    if (!external && ERROR_CODE_VECTORS.has(vector)) this.push(errorCode);
    if (type === 0x0E || type === 0x06) this.IF = false;
    this.segs.CS = sel;
    this.halted = false;
    return this.dispatch(handler);
  }

  iret() {
    const eip = this.pop();
    this.segs.CS = this.pop() & 0xFFFF;
    this.setEflags(this.pop());
    return this.dispatch(eip);
  }

  // Timers and interrupts

  // Registers a device deadline on the machine clock
  schedule(timer) {
    this.timers.push(timer);
    this.rescheduleEvents();
  }

  rescheduleEvents() {
    let next = Infinity;
    for (const t of this.timers) if (t.deadline < next) next = t.deadline;
    this.nextEvent = next;
  }

  runTimers() {
    for (const t of this.timers) if (this.tsc >= t.deadline) t.fire(this);
    this.rescheduleEvents();
  }

  // Raises or lowers the interrupt request line the controller drives into the processor
  setIRQ(pending) {
    this.irq = pending;
  }

  // Running

  // Runs blocks from a key until the machine stops at a sentinel return, the clock advancing with each block and
  // interrupts taken between blocks
  run(key) {
    this.key = key;
    this.stopAt = null;
    for (;;) {
      // An interrupt is taken before a block that starts afresh, never before one deciding on its predecessor's
      // flags, so the return lands on a block the lift keys by its address alone
      if (this.irq && this.IF && key.indexOf('@') < 0) {
        const vector = this.bus.acknowledge();
        if (vector >= 0) {
          this.regs.EIP = this.blocks[key].va;
          key = this.deliver(vector, true);
          this.key = key;
          continue;
        }
      }
      if (this.halted) {
        // Nothing runs until a device raises an interrupt, so the clock jumps to the next event
        if (this.nextEvent === Infinity) this.fault('halted with no event pending and interrupts ' + (this.IF ? 'enabled' : 'disabled'));
        this.tsc = Math.max(this.tsc, this.nextEvent);
        this.runTimers();
        if (!this.irq) continue;
        this.halted = false;
        continue;
      }
      let fn = this.compiled.get(key);
      if (fn === undefined) fn = this.compile(key);
      this.blocksRun++;
      if (this.trace !== null) this.trace(this, key);
      const next = fn(this);
      if (this.tsc >= this.nextEvent) this.runTimers();
      if (next === null) return;
      key = next;
      this.key = key;
    }
  }

  // Calls a routine from outside the machine, the arguments pushed as a caller pushes them, and runs until it returns
  call(va, args = [], regs = {}) {
    const esp = this.regs.ESP;
    for (let i = args.length - 1; i >= 0; i--) this.push(args[i]);
    const sentinel = SENTINEL_BASE + 4 * this.sentinels++;
    this.push(sentinel);
    for (const [name, v] of Object.entries(regs)) this.regs[name] = v >>> 0;
    try {
      this.run(this.dispatch(va));
      if (this.stopAt !== sentinel) this.fault(`returned to ${hex8(this.stopAt)} instead of the caller`);
    } finally {
      this.sentinels--;
    }
    this.regs.ESP = esp;
    return this.regs.EAX;
  }

  // Compiling

  compile(key) {
    const b = this.blocks[key];
    if (b === undefined) {
      const why = this.errors[key];
      this.fault(why ? `block ${key} was not lifted: ${why}` : `no block ${key}`);
    }
    const c = new BlockCompiler(this, key, b);
    const fn = c.compile();
    this.compiled.set(key, fn);
    return fn;
  }

  isFloat(idx) {
    const n = this.nodes[idx];
    switch (n.k) {
      case 'f': case 'elem': return true;
      case 'un': return FLOAT_UN.has(n.op);
      case 'bin': return FLOAT_BIN.has(n.op);
      case 'sel': return this.isFloat(n.a) && this.isFloat(n.b);
      case 'load': return n.size === 8;
    }
    return false;
  }
}

// Compiles one block to a function over the machine, every effect applied in order and each load read at the point
// in that order the lifter recorded it
class BlockCompiler {
  constructor(m, key, b) {
    this.m = m;
    this.nodes = m.nodes;
    this.key = key;
    this.b = b;
    this.lines = [];
    this.names = new Map();
    this.temps = 0;
    this.epoch = 0;
    this.loadsAt = new Map();
    this.collectLoads();
  }

  // Groups every load the block's expressions make by the effect count it was recorded at
  collectLoads() {
    const seen = new Set();
    const b = this.b;
    const visit = (idx) => {
      if (idx === undefined || seen.has(idx)) return;
      seen.add(idx);
      const n = this.nodes[idx];
      for (const k of ['a', 'b', 'c', 'addr']) visit(n[k]);
      for (const k of ['args', 'elems']) if (n[k]) for (const x of n[k]) visit(x);
      if (n.k === 'load') {
        const e = n.e || 0;
        if (!this.loadsAt.has(e)) this.loadsAt.set(e, []);
        this.loadsAt.get(e).push(idx);
      }
    };
    for (const e of b.effects) {
      visit(e.value); visit(e.addr);
      for (const a of e.args || []) visit(a);
    }
    for (const idx of Object.values(b.regs)) visit(idx);
    if (b.esp !== undefined) visit(b.esp);
    for (const idx of b.fpu) visit(idx);
    for (const idx of Object.values(b.xmm || {})) visit(idx);
    for (const idx of Object.values(b.mm || {})) visit(idx);
    for (const idx of Object.values(b.flags || {})) if (typeof idx === 'number' && idx >= 0) visit(idx);
    if (b.fcmp) { visit(b.fcmp.a); visit(b.fcmp.b); }
    for (const k of ['cond', 'index', 'value']) if (b.term[k] !== undefined) visit(b.term[k]);
  }

  temp(code) {
    const t = 't' + this.temps++;
    this.lines.push(`const ${t} = ${code};`);
    return t;
  }

  // Emits the loads recorded at an effect count, before that effect runs
  emitLoads(epoch) {
    const loads = this.loadsAt.get(epoch);
    if (!loads) return;
    for (const idx of loads) this.emit(idx);
  }

  emit(idx) {
    const found = this.names.get(idx);
    if (found !== undefined) return found;
    const n = this.nodes[idx];
    let expr;
    const int = (x) => `(${x} >>> 0)`;
    switch (n.k) {
      case 'i': expr = String(u32(n.v)); break;
      case 'f': expr = n.v !== undefined ? String(n.v) : (n.s === 'NaN' ? 'NaN' : (n.s === '+Inf' ? 'Infinity' : '-Infinity')); break;
      case 'ptr':
        if (n.base !== '') throw new MachineFault(`block ${this.key} addresses ${n.base}`);
        expr = String(u32(n.off || 0));
        break;
      // A register or flag read lands in a temp, since the block's outputs are assigned after every value is computed
      case 'sym': expr = this.temp(this.sym(n.name)); break;
      case 'load': {
        const e = n.e || 0;
        if (e > this.epoch) throw new MachineFault(`block ${this.key} reads memory at effect ${e} before effect ${this.epoch} runs`);
        let addr;
        if (n.addr !== undefined) addr = this.emit(n.addr);
        else if (n.base === '') addr = String(u32(n.off || 0));
        else throw new MachineFault(`block ${this.key} loads from ${n.base}`);
        if (n.size === 8) expr = this.temp(`m.ldf64(${addr})`);
        else if (n.size > 8) expr = this.temp(`m.ldvec(${addr}, ${n.size})`);
        else expr = this.temp(`m.ld${n.size}(${addr})`);
        break;
      }
      case 'bin': {
        const a = this.emit(n.a), b = this.emit(n.b);
        const op = n.op;
        if (FLOAT_BIN.has(op)) {
          switch (op) {
            case 'fadd': expr = this.temp(`${a} + ${b}`); break;
            case 'fsub': expr = this.temp(`${a} - ${b}`); break;
            case 'fmul': expr = this.temp(`${a} * ${b}`); break;
            case 'fdiv': expr = this.temp(`${a} / ${b}`); break;
            case 'fmin': expr = this.temp(`Math.min(${a}, ${b})`); break;
            case 'fmax': expr = this.temp(`Math.max(${a}, ${b})`); break;
            case 'fmod': expr = this.temp(`${a} % ${b}`); break;
            case 'fpow': expr = this.temp(`Math.pow(${a}, ${b})`); break;
            case 'fatan2': expr = this.temp(`Math.atan2(${a}, ${b})`); break;
            case 'frem': expr = this.temp(`${a} - ${b} * F.roundEven(${a} / ${b})`); break;
          }
          break;
        }
        switch (op) {
          case 'add': expr = this.temp(`(${a} + ${b}) >>> 0`); break;
          case 'sub': expr = this.temp(`(${a} - ${b}) >>> 0`); break;
          case 'mul': expr = this.temp(`Math.imul(${a}, ${b}) >>> 0`); break;
          case 'and': expr = this.temp(`(${a} & ${b}) >>> 0`); break;
          case 'or': expr = this.temp(`(${a} | ${b}) >>> 0`); break;
          case 'xor': expr = this.temp(`(${a} ^ ${b}) >>> 0`); break;
          case 'shl': expr = this.temp(`(${a} << (${b} & 31)) >>> 0`); break;
          case 'shr': expr = this.temp(`${int(a)} >>> (${b} & 31)`); break;
          case 'sar': expr = this.temp(`((${a} | 0) >> (${b} & 31)) >>> 0`); break;
          case 'idiv': expr = this.temp(`F.idiv(${a}, ${b})`); break;
          case 'idiv_rem': expr = this.temp(`F.irem(${a}, ${b})`); break;
          case 'udiv': expr = this.temp(`F.udiv(${a}, ${b})`); break;
          case 'udiv_rem': expr = this.temp(`F.urem(${a}, ${b})`); break;
          case 'imulhi': expr = this.temp(`Number((BigInt(${a} | 0) * BigInt(${b} | 0)) >> 32n) >>> 0`); break;
          case 'mulhi': expr = this.temp(`Number((BigInt(${int(a)}) * BigInt(${int(b)})) >> 32n) >>> 0`); break;
          // A 64 bit dividend is the high and low words as one big integer, which only the wide divisions take
          case 'hilo': expr = this.temp(`(BigInt(${int(a)}) << 32n) | BigInt(${int(b)})`); break;
          case 'udiv64': expr = this.temp(`F.udiv64(${a}, ${b})`); break;
          case 'udiv64_rem': expr = this.temp(`F.urem64(${a}, ${b})`); break;
          case 'idiv64': expr = this.temp(`F.idiv64(${a}, ${b})`); break;
          case 'idiv64_rem': expr = this.temp(`F.irem64(${a}, ${b})`); break;
          default: throw new MachineFault(`block ${this.key} uses ${op}`);
        }
        break;
      }
      case 'un': {
        const a = this.emit(n.a);
        switch (n.op) {
          case 'zx8': expr = this.temp(`${a} & 0xFF`); break;
          case 'zx16': expr = this.temp(`${a} & 0xFFFF`); break;
          case 'sx8': expr = this.temp(`((${a} << 24) >> 24) >>> 0`); break;
          case 'sx16': expr = this.temp(`((${a} << 16) >> 16) >>> 0`); break;
          case 'neg': expr = this.temp(`(-${a}) >>> 0`); break;
          case 'not': expr = this.temp(`(~${a}) >>> 0`); break;
          case 'i2f': expr = this.temp(`${a} | 0`); break;
          case 'bits2f': expr = this.temp(`F.floatFromBits(${a})`); break;
          case 'bits2d': expr = this.temp(`F.doubleFromBits(${a})`); break;
          case 'f32': expr = this.temp(`Math.fround(${a})`); break;
          case 'f64': expr = a; break;
          case 'f2i': expr = this.temp(`F.f2i(${a})`); break;
          case 'f2i_rn': expr = this.temp(`F.f2i(F.roundEven(${a}))`); break;
          case 'f2bits': expr = this.temp(`F.bitsFromFloat(${a})`); break;
          case 'fneg': expr = this.temp(`-${a}`); break;
          case 'fabs': expr = this.temp(`Math.abs(${a})`); break;
          case 'fsin': expr = this.temp(`Math.sin(${a})`); break;
          case 'fcos': expr = this.temp(`Math.cos(${a})`); break;
          case 'fsqrt': expr = this.temp(`Math.sqrt(${a})`); break;
          case 'ftan': expr = this.temp(`Math.tan(${a})`); break;
          case 'ffloor': expr = this.temp(`Math.floor(${a})`); break;
          case 'fceil': expr = this.temp(`Math.ceil(${a})`); break;
          case 'fexp': expr = this.temp(`Math.exp(${a})`); break;
          case 'flog': expr = this.temp(`Math.log(${a})`); break;
          case 'flog10': expr = this.temp(`Math.log10(${a})`); break;
          case 'flog2': expr = this.temp(`Math.log2(${a})`); break;
          case 'ftrunc': expr = this.temp(`Math.trunc(${a})`); break;
          case 'fround': expr = this.temp(`F.roundEven(${a})`); break;
          case 'fatan': expr = this.temp(`Math.atan(${a})`); break;
          case 'fasin': expr = this.temp(`Math.asin(${a})`); break;
          case 'facos': expr = this.temp(`Math.acos(${a})`); break;
          case 'fsinh': expr = this.temp(`Math.sinh(${a})`); break;
          case 'fcosh': expr = this.temp(`Math.cosh(${a})`); break;
          case 'ftanh': expr = this.temp(`Math.tanh(${a})`); break;
          case 'bsf': expr = this.temp(`F.bsf(${a})`); break;
          case 'bsr': expr = this.temp(`F.bsr(${a})`); break;
          default: throw new MachineFault(`block ${this.key} uses ${n.op}`);
        }
        break;
      }
      case 'cond': {
        const a = this.emit(n.a), b = this.emit(n.b);
        let c;
        switch (n.op) {
          case 'eq': c = `${int(a)} === ${int(b)}`; break;
          case 'ne': c = `${int(a)} !== ${int(b)}`; break;
          case 'lt': c = `(${a} | 0) < (${b} | 0)`; break;
          case 'le': c = `(${a} | 0) <= (${b} | 0)`; break;
          case 'gt': c = `(${a} | 0) > (${b} | 0)`; break;
          case 'ge': c = `(${a} | 0) >= (${b} | 0)`; break;
          case 'ult': c = `${int(a)} < ${int(b)}`; break;
          case 'ule': c = `${int(a)} <= ${int(b)}`; break;
          case 'ugt': c = `${int(a)} > ${int(b)}`; break;
          case 'uge': c = `${int(a)} >= ${int(b)}`; break;
          default: throw new MachineFault(`block ${this.key} compares with ${n.op}`);
        }
        expr = this.temp(`${c} ? 1 : 0`);
        break;
      }
      case 'fcmp': {
        const a = this.emit(n.a), b = this.emit(n.b);
        expr = this.temp(`((((${a} !== ${a} || ${b} !== ${b}) ? 8 : (${a} < ${b} ? 1 : (${a} === ${b} ? 2 : 4))) & ${n.set}) !== 0 ? 1 : 0)`);
        break;
      }
      case 'not': { const a = this.emit(n.a); expr = this.temp(`${a} ? 0 : 1`); break; }
      case 'sel': {
        const c = this.emit(n.c), a = this.emit(n.a), b = this.emit(n.b);
        expr = this.temp(`${c} ? ${a} : ${b}`);
        break;
      }
      case 'call': expr = `r${n.seq}`; if (!this.names.has('call' + n.seq)) throw new MachineFault(`block ${this.key} reads the result of call ${n.seq} (${n.fn}) before it runs`); break;
      case 'block': { const elems = n.elems.map((e) => this.emit(e)); expr = this.temp(`[${elems.join(', ')}]`); break; }
      case 'elem': { const a = this.emit(n.a); expr = this.temp(`${a}[${n.i}]`); break; }
      case 'undef': throw new MachineFault(`block ${this.key} holds a value the lifter never defined`);
      default: throw new MachineFault(`block ${this.key} holds a ${n.k} node`);
    }
    this.names.set(idx, expr);
    return expr;
  }

  sym(name) {
    if (name.startsWith('reg.')) {
      const r = name.slice(4);
      if (r in this.m.regs) return `R.${r}`;
      if (r in this.m.segs) return `m.segs.${r}`;
    }
    if (name.startsWith('segbase.')) return `m.segbase.${name.slice(8)}`;
    if (name === 'self') return 'm.self';
    if (name === 'fsw') return 'm.statusWord()';
    if (name.startsWith('fpu.st')) return `m.fpu[${Number(name.slice(6))}]`;
    if (name.startsWith('xmm')) { const [r, lane] = name.slice(3).split('.'); return `m.xmm[${Number(r)}][${Number(lane)}]`; }
    if (name.startsWith('mm')) { const [r, lane] = name.slice(2).split('.'); return `m.mm[${Number(r)}][${Number(lane)}]`; }
    if (name === 'flags.a') return 'm.fl.a';
    if (name === 'flags.b') return 'm.fl.b';
    if (name === 'flags.r') return 'm.fl.r';
    if (name === 'flags.cf') return 'm.fl.cf';
    if (name === 'flags.fsw_a') return 'm.fl.fswA';
    if (name === 'flags.fsw_b') return 'm.fl.fswB';
    if (name === 'fcmp.a') return 'm.fcmp.a';
    if (name === 'fcmp.b') return 'm.fcmp.b';
    if (name.startsWith('dfstep')) { const n = Number(name.slice(6)); return `(m.DF ? ${u32(-n)} : ${n})`; }
    if (name.startsWith('cond.')) return `(m.condition(${JSON.stringify(name.slice(5))}) ? 1 : 0)`;
    throw new MachineFault(`block ${this.key} reads ${name}`);
  }

  effect(e, k) {
    this.epoch = k;
    this.emitLoads(k);
    if (e.kind === 'store') {
      const v = this.emit(e.value);
      const addr = e.has_addr ? this.emit(e.addr) : String(u32(e.off || 0));
      if (!e.has_addr && (e.base || '') !== '') throw new MachineFault(`block ${this.key} stores to ${e.base}`);
      if (e.size === 8) this.lines.push(`m.stf64(${addr}, ${v});`);
      else if (e.size > 8) this.lines.push(`m.stvec(${addr}, ${v});`);
      else if (e.size === 4 && this.m.isFloat(e.value)) this.lines.push(`m.st4(${addr}, F.bitsFromFloat(${v}));`);
      else this.lines.push(`m.st${e.size}(${addr}, ${v});`);
      return;
    }
    if (e.kind === 'copy') {
      const dst = this.emit(e.addr), src = this.emit(e.args[0]), count = this.emit(e.value), step = this.emit(e.args[1]);
      this.lines.push(`m.copy(${dst}, ${src}, ${count}, ${e.size}, ${step});`);
      return;
    }
    if (e.kind === 'fill') {
      const dst = this.emit(e.addr), value = this.emit(e.value), count = this.emit(e.args[0]), step = this.emit(e.args[1]);
      this.lines.push(`m.fill(${dst}, ${value}, ${count}, ${e.size}, ${step});`);
      return;
    }
    if (e.kind === 'call') {
      const args = (e.args || []).map((a) => this.emit(a));
      this.names.set('call' + e.seq, true);
      this.lines.push(`const r${e.seq} = m.cpu(${JSON.stringify(e.fn)}, [${args.join(', ')}], ${e.seq});`);
      return;
    }
    throw new MachineFault(`block ${this.key} has a ${e.kind} effect`);
  }

  compile() {
    const b = this.b;
    const L = this.lines;
    L.push(`const R = m.regs;`);
    L.push(`m.tsc += ${(b.n || 1) * this.m.cyclesPerInsn};`);
    b.effects.forEach((e, k) => this.effect(e, k));
    this.epoch = b.effects.length;
    for (const [epoch] of this.loadsAt) if (epoch >= this.epoch) this.emitLoads(epoch);
    this.epoch = Infinity;
    // Every result is computed before any register changes, since the expressions read the state the block entered with
    const assigns = [];
    for (const [r, idx] of Object.entries(b.regs)) {
      const v = this.emit(idx);
      if (r in this.m.regs) assigns.push(`R.${r} = ${v};`);
      else assigns.push(`m.setSegment(${JSON.stringify(r)}, ${v});`);
    }
    if (b.esp !== undefined) { const v = this.emit(b.esp); assigns.push(`R.ESP = ${v};`); }
    if (b.fpu && b.fpu.length) {
      const vals = b.fpu.map((idx) => this.emit(idx));
      assigns.push(`m.fpu = [${vals.join(', ')}];`);
    }
    for (const [name, idx] of Object.entries(b.xmm || {})) {
      const [r, lane] = name.slice(3).split('.');
      const v = this.emit(idx);
      assigns.push(`m.xmm[${Number(r)}][${Number(lane)}] = ${v};`);
    }
    for (const [name, idx] of Object.entries(b.mm || {})) {
      const [r, lane] = name.slice(2).split('.');
      const v = this.emit(idx);
      assigns.push(`m.mm[${Number(r)}][${Number(lane)}] = ${v};`);
    }
    if (b.flags) {
      const f = b.flags;
      const part = (idx) => (typeof idx === 'number' && idx >= 0 ? this.emit(idx) : '0');
      const a = part(f.a), bb = part(f.b), r = part(f.r), cf = part(f.cf), fa = part(f.fsw_a), fb = part(f.fsw_b);
      assigns.push(`m.fl = { kind: ${JSON.stringify(f.kind || '')}, a: ${a}, b: ${bb}, r: ${r}, cf: ${cf}, fswA: ${fa}, fswB: ${fb} };`);
    }
    if (b.fcmp) {
      const a = this.emit(b.fcmp.a), bb = this.emit(b.fcmp.b);
      assigns.push(`m.fcmp = { a: ${a}, b: ${bb} };`);
    }
    const t = b.term;
    let tail;
    const target = (key) => {
      const va = this.m.blocks[key] ? this.m.blocks[key].va : parseInt(key, 16);
      if (this.m.hooks.has(va >>> 0)) return `m.dispatch(${va >>> 0})`;
      return JSON.stringify(key);
    };
    switch (t.kind) {
      case 'jump': tail = `return ${target(t.target)};`; break;
      case 'branch': { const c = this.emit(t.cond); tail = `return ${c} ? ${target(t.taken)} : ${target(t.fall)};`; break; }
      case 'switch': { const i = this.emit(t.index); tail = `{ const i = ${i}; if (i >= ${t.targets.length}) m.fault('switch index ' + i + ' outside ${t.targets.length} cases'); return T[i]; }`; break; }
      case 'jumpind': { const a = this.emit(t.index); tail = `return m.dispatch(${a});`; break; }
      case 'hlt': tail = `return m.halt(${target(t.target)});`; break;
      case 'int': tail = `return m.softint(${t.vector}, ${this.m.blocks[t.target] ? this.m.blocks[t.target].va : parseInt(t.target, 16)});`; break;
      case 'iret': tail = `return m.iret();`; break;
      default: throw new MachineFault(`block ${this.key} ends with ${t.kind}`);
    }
    L.push(...assigns, tail);
    const targets = t.kind === 'switch' ? t.targets.map((k) => (this.m.hooks.has(this.m.blocks[k].va) ? null : k)) : null;
    const body = L.join('\n');
    let make;
    try {
      make = new Function('m', 'F', 'T', `return function block_${this.key.replace(/[^0-9A-Za-z]/g, '_')}(m) {\n${body}\n};`);
    } catch (err) {
      throw new MachineFault(`block ${this.key} does not compile: ${err.message}\n${body}`);
    }
    return make(this.m, HELPERS, targets);
  }
}

// The 64 by 32 bit divisions, a quotient past 32 bits being the processor's divide error
function udiv64(n, d) {
  const y = BigInt(d >>> 0);
  if (y === 0n) throw new MachineFault('division by zero');
  const q = n / y;
  if (q > 0xFFFFFFFFn) throw new MachineFault('division overflow');
  return Number(q);
}
function urem64(n, d) {
  udiv64(n, d);
  return Number(n % BigInt(d >>> 0));
}
function idiv64(n, d) {
  const x = BigInt.asIntN(64, n), y = BigInt(d | 0);
  if (y === 0n) throw new MachineFault('division by zero');
  const q = x / y;
  if (q > 0x7FFFFFFFn || q < -0x80000000n) throw new MachineFault('division overflow');
  return Number(BigInt.asUintN(32, q));
}
function irem64(n, d) {
  idiv64(n, d);
  return Number(BigInt.asUintN(32, BigInt.asIntN(64, n) % BigInt(d | 0)));
}

const HELPERS = {
  floatFromBits,
  bitsFromFloat,
  doubleFromBits: (bits) => { bitsBuf.setUint32(0, u32(bits), true); bitsBuf.setUint32(4, 0, true); return bitsBuf.getFloat64(0, true); },
  roundEven,
  f2i: (f) => (Number.isFinite(f) && Math.abs(f) < 0x80000000 ? (Math.trunc(f) | 0) >>> 0 : 0x80000000),
  idiv: (a, b) => { const x = a | 0, y = b | 0; if (y === 0) throw new MachineFault('division by zero'); return (Math.trunc(x / y) | 0) >>> 0; },
  irem: (a, b) => { const x = a | 0, y = b | 0; if (y === 0) throw new MachineFault('division by zero'); return (x % y) >>> 0; },
  udiv: (a, b) => { const x = a >>> 0, y = b >>> 0; if (y === 0) throw new MachineFault('division by zero'); return Math.trunc(x / y) >>> 0; },
  urem: (a, b) => { const x = a >>> 0, y = b >>> 0; if (y === 0) throw new MachineFault('division by zero'); return (x % y) >>> 0; },
  udiv64,
  urem64,
  idiv64,
  irem64,
  bsf,
  bsr,
};

export { u32, hex8, floatFromBits, bitsFromFloat, udiv64, urem64, idiv64, irem64 };
