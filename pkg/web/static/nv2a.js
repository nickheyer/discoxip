// The graphics chip at its first window: the master interrupt routing, the frame timing of the display controller,
// the command FIFO running what the processor pushes through the objects it binds, and the engines reporting idle,
// every other register keeping what was written

import { MachineFault, u32, hex8 } from './machine.js';

const NV_PMC_BOOT_0 = 0x000000, NV_PMC_INTR_0 = 0x000100, NV_PMC_INTR_EN_0 = 0x000140;
const NV_PMC_INTR_PFIFO = 1 << 8, NV_PMC_INTR_PGRAPH = 1 << 12, NV_PMC_INTR_PCRTC = 1 << 24, NV_PMC_INTR_SOFTWARE = 1 << 31;
const NV_PMC_INTR_EN_HARDWARE = 1, NV_PMC_INTR_EN_SOFTWARE = 2;
const NV_PFB_CSTATUS = 0x10020C, NV_PFB_WBC = 0x100410, NV_PFB_WBC_FLUSH = 1 << 16;
const NV_PEXTDEV_BOOT_0 = 0x101000;
const NV_PTIMER_NUMERATOR = 0x009200, NV_PTIMER_DENOMINATOR = 0x009210, NV_PTIMER_TIME_0 = 0x009400, NV_PTIMER_TIME_1 = 0x009410;
const NV_PFIFO_DEBUG_0 = 0x002080, NV_PFIFO_INTR_0 = 0x002100, NV_PFIFO_INTR_EN_0 = 0x002140, NV_PFIFO_RAMHT = 0x002210, NV_PFIFO_RUNOUT_STATUS = 0x002400;
const NV_PFIFO_CACHE1_PUSH0 = 0x003200, NV_PFIFO_CACHE1_STATUS = 0x003214, NV_PFIFO_CACHE1_DMA_PUSH = 0x003220, NV_PFIFO_CACHE1_DMA_INSTANCE = 0x00322C;
const NV_PFIFO_CACHE1_DMA_PUT = 0x003240, NV_PFIFO_CACHE1_DMA_GET = 0x003244, NV_PFIFO_CACHE1_PULL0 = 0x003250;
const NV_PFIFO_RUNOUT_EMPTY = 0x10, NV_PFIFO_CACHE1_STATUS_EMPTY = 0x10, NV_PFIFO_CACHE1_DMA_PUSH_EMPTY = 0x100, NV_PFIFO_CACHE1_DMA_PUSH_SUSPENDED = 0x1000;
// The access bit the cache's push side, its pull side, the DMA pusher and the graphics engine's FIFO each carry
const NV_PFIFO_ACCESS = 1;
const NV_PGRAPH_INTR = 0x400100, NV_PGRAPH_NSOURCE = 0x400108, NV_PGRAPH_INTR_EN = 0x400140, NV_PGRAPH_STATUS = 0x400700;
const NV_PGRAPH_TRAPPED_ADDR = 0x400704, NV_PGRAPH_TRAPPED_DATA = 0x400708, NV_PGRAPH_SURFACE = 0x400710, NV_PGRAPH_INCREMENT = 0x40071C, NV_PGRAPH_FIFO = 0x400720;
const NV_PGRAPH_COLORCLEARVALUE = 0x40186C, NV_PGRAPH_ZSTENCILCLEARVALUE = 0x401A88;
const NV_PGRAPH_INTR_ERROR = 1 << 20, NV_PGRAPH_NSOURCE_NOTIFICATION = 1 << 0, NV_PGRAPH_INCREMENT_READ_3D = 1 << 1;
const SURFACE_READ_SHIFT = 8, SURFACE_WRITE_SHIFT = 16, SURFACE_MODULO_SHIFT = 24, SURFACE_FIELD = 7;
const NV_PCRTC_INTR_0 = 0x600100, NV_PCRTC_INTR_EN_0 = 0x600140, NV_PCRTC_RASTER = 0x600808;
const NV_PCRTC_INTR_VBLANK = 1;
const NV_PRMCIO_CRX = 0x6013D4, NV_PRMCIO_CR = 0x6013D5, NV_PRMCIO_INP0 = 0x6013DA, NV_PRMCIO_INP0_VBLANK = 0x08;
const NV_PRAMIN = 0x700000, NV_PRAMIN_SIZE = 0x100000;
// The user channels mirror the pusher's put and get pointers, one window per channel
const NV_USER = 0x800000, NV_USER_CHANNEL_SIZE = 0x10000, NV_USER_DMA_PUT = 0x40, NV_USER_DMA_GET = 0x44;
const GPU_IRQ = 3, FRAME_HZ = 60, FRAME_LINES = 525, VBLANK_LINES = 20;
// Pushbuffer command words
const CMD_JUMP_OLD = 0x20000000, CMD_CALL = 0x00000002, CMD_RETURN = 0x00020000, CMD_NON_INCREASING = 0x40000000;
// Object classes and the methods the command processor itself performs
const CLASS_DMA_FROM_MEMORY = 0x02, CLASS_DMA_TO_MEMORY = 0x03, CLASS_DMA_IN_MEMORY = 0x3D, CLASS_KELVIN = 0x97;
const METHOD_SET_OBJECT = 0x0000;
const NV097_NO_OPERATION = 0x0100, NV097_NOTIFY_WRITE_ONLY = 0x0104, NV097_WAIT_FOR_IDLE = 0x0110;
const NV097_SET_FLIP_READ = 0x0120, NV097_SET_FLIP_WRITE = 0x0124, NV097_SET_FLIP_MODULO = 0x0128, NV097_FLIP_INCREMENT_WRITE = 0x012C, NV097_FLIP_STALL = 0x0130;
const NV097_SET_CONTEXT_DMA_NOTIFIES = 0x0180, NV097_SET_CONTEXT_DMA_SEMAPHORE = 0x01A4;
const NV097_SET_SEMAPHORE_OFFSET = 0x1D6C, NV097_BACK_END_WRITE_SEMAPHORE_RELEASE = 0x1D70;
// The clear values land in engine registers the kernel reads back as the arguments of a software method
const NV097_SET_ZSTENCIL_CLEAR_VALUE = 0x1D8C, NV097_SET_COLOR_CLEAR_VALUE = 0x1D90;
// Why the pusher stopped short of the put pointer
const STALL_NONE = 0, STALL_TRAP = 1, STALL_FLIP = 2;

export class NV2A {
  constructor(bus) {
    this.bus = bus;
    this.regs = new Map();
    this.m = null;
    this.regs.set(NV_PMC_BOOT_0, bus.hardware.gpu.boot0);
    this.regs.set(NV_PEXTDEV_BOOT_0, bus.hardware.gpu.strap);
    this.regs.set(NV_PFB_CSTATUS, bus.hardware.memory);
    this.regs.set(NV_PTIMER_NUMERATOR, 1);
    this.regs.set(NV_PTIMER_DENOMINATOR, 1);
    this.crtcIndex = 0;
    this.crtc = new Uint8Array(256);
    this.ramin = new Uint8Array(NV_PRAMIN_SIZE);
    this.raminView = new DataView(this.ramin.buffer);
    this.pushed = 0;
    this.subchannels = new Array(8).fill(null);
    this.kelvin = { notifies: null, semaphore: null, semaphoreOffset: 0 };
    this.methods = null;
    // The pusher's position between runs: the words of a method header still to take and the call return stack
    this.pusher = { stall: STALL_NONE, header: null, calls: [] };
    this.frame = { deadline: Infinity, fire: (m) => this.vblank(m) };
  }

  attach(m) {
    this.m = m;
    this.frame.deadline = this.frameCycles;
    m.schedule(this.frame);
  }

  get frameCycles() { return this.bus.hardware.clock_hz / FRAME_HZ; }

  // The timer counts at the ratio programmed from the chip's own clock, taken as the processor's
  time() {
    const num = this.regs.get(NV_PTIMER_NUMERATOR) || 1, den = this.regs.get(NV_PTIMER_DENOMINATOR) || 1;
    return Math.floor(this.m.tsc * num / den);
  }

  // The line the display controller is scanning, counted through the frame from the machine clock
  raster() {
    return Math.floor((this.m.tsc % this.frameCycles) * FRAME_LINES / this.frameCycles);
  }

  inVblank() {
    return this.raster() >= FRAME_LINES - VBLANK_LINES;
  }

  // Each vertical blank raises the display controller's interrupt
  vblank(m) {
    this.regs.set(NV_PCRTC_INTR_0, (this.regs.get(NV_PCRTC_INTR_0) || 0) | NV_PCRTC_INTR_VBLANK);
    this.frame.deadline += this.frameCycles;
    if (this.frame.deadline <= m.tsc) this.frame.deadline = m.tsc + this.frameCycles;
    this.updateInterrupt();
  }

  // The master interrupt register gathers each engine's pending and enabled interrupts and drives the line
  updateInterrupt() {
    let pending = (this.regs.get(NV_PMC_INTR_0) || 0) & NV_PMC_INTR_SOFTWARE;
    if (((this.regs.get(NV_PCRTC_INTR_0) || 0) & (this.regs.get(NV_PCRTC_INTR_EN_0) || 0)) !== 0) pending |= NV_PMC_INTR_PCRTC;
    if (((this.regs.get(NV_PFIFO_INTR_0) || 0) & (this.regs.get(NV_PFIFO_INTR_EN_0) || 0)) !== 0) pending |= NV_PMC_INTR_PFIFO;
    if (((this.regs.get(NV_PGRAPH_INTR) || 0) & (this.regs.get(NV_PGRAPH_INTR_EN) || 0)) !== 0) pending |= NV_PMC_INTR_PGRAPH;
    this.regs.set(NV_PMC_INTR_0, pending >>> 0);
    const en = this.regs.get(NV_PMC_INTR_EN_0) || 0;
    const hardware = (pending & ~NV_PMC_INTR_SOFTWARE) !== 0 && (en & NV_PMC_INTR_EN_HARDWARE) !== 0;
    const software = (pending & NV_PMC_INTR_SOFTWARE) !== 0 && (en & NV_PMC_INTR_EN_SOFTWARE) !== 0;
    if (hardware || software) this.bus.raise(GPU_IRQ); else this.bus.lower(GPU_IRQ);
  }

  // Instance memory and the objects in it

  raminRead(off) {
    if (off + 4 > NV_PRAMIN_SIZE) throw new MachineFault(`instance memory read at ${hex8(off)}`);
    return this.raminView.getUint32(off, true);
  }

  // The instance a handle names, through the hash table the FIFO was given
  lookup(handle) {
    const ramht = this.regs.get(NV_PFIFO_RAMHT) || 0;
    const base = ((ramht >>> 4) & 0x1F) << 12;
    const bits = ((ramht >>> 16) & 3) + 9;
    const size = 1 << bits;
    let hash = 0;
    for (let h = handle >>> 0; h !== 0; h >>>= bits) hash ^= h & (size - 1);
    const at = base + hash * 8;
    if (this.raminRead(at) !== (handle >>> 0)) {
      for (let i = 0; i < size; i++) {
        if (this.raminRead(base + i * 8) === (handle >>> 0)) return this.raminRead(base + i * 8 + 4);
      }
      throw new MachineFault(`graphics handle ${hex8(handle)} is not in the hash table`);
    }
    return this.raminRead(at + 4);
  }

  // The memory window a direct memory access object describes, by the instance the handle names
  dmaObject(handle) {
    const entry = this.lookup(handle);
    const instance = (entry & 0xFFFF) << 4;
    const w0 = this.raminRead(instance), w1 = this.raminRead(instance + 4), w2 = this.raminRead(instance + 8);
    const cls = w0 & 0xFFF;
    if (cls !== CLASS_DMA_IN_MEMORY && cls !== CLASS_DMA_FROM_MEMORY && cls !== CLASS_DMA_TO_MEMORY) throw new MachineFault(`graphics handle ${hex8(handle)} names class ${cls.toString(16)}, not a memory window`);
    return { base: u32((w2 & 0xFFFFF000) | (w0 >>> 20)), limit: w1 >>> 0, target: (w0 >>> 16) & 3 };
  }

  // The class of the graphics object a handle names
  objectClass(handle) {
    const entry = this.lookup(handle);
    const instance = (entry & 0xFFFF) << 4;
    return this.raminRead(instance) & 0xFF;
  }

  // Memory the engines write through a window, into the processor's memory
  dmaWrite4(dma, offset, value) {
    if (offset + 4 > dma.limit + 1) throw new MachineFault(`graphics write at ${hex8(offset)} past a window of ${hex8(dma.limit + 1)} bytes`);
    this.m.physWrite4(u32(dma.base + offset), value >>> 0);
  }

  // The command FIFO

  // The pusher runs while the kernel has the cache's push and pull sides, the DMA pusher and the engine's FIFO
  // access enabled and nothing has stopped it short
  running() {
    const dmaPush = this.regs.get(NV_PFIFO_CACHE1_DMA_PUSH) || 0;
    if ((dmaPush & NV_PFIFO_ACCESS) === 0 || (dmaPush & NV_PFIFO_CACHE1_DMA_PUSH_SUSPENDED) !== 0) return false;
    for (const reg of [NV_PFIFO_CACHE1_PUSH0, NV_PFIFO_CACHE1_PULL0, NV_PGRAPH_FIFO]) if (((this.regs.get(reg) || 0) & NV_PFIFO_ACCESS) === 0) return false;
    return this.pusher.stall === STALL_NONE;
  }

  // Runs the words pushed between the get and put pointers through the objects bound to the subchannels, stopping
  // where a method traps to the kernel or stalls on a flip until the kernel lets it go on
  consumePushBuffer() {
    const p = this.pusher;
    if (!this.running()) return;
    const put = this.regs.get(NV_PFIFO_CACHE1_DMA_PUT) || 0;
    let get = this.regs.get(NV_PFIFO_CACHE1_DMA_GET) || 0;
    if (put === get && p.header === null) return;
    const instance = ((this.regs.get(NV_PFIFO_CACHE1_DMA_INSTANCE) || 0) & 0xFFFF) << 4;
    const w0 = this.raminRead(instance), w2 = this.raminRead(instance + 8);
    const base = u32((w2 & 0xFFFFF000) | (w0 >>> 20));
    for (let words = 0; get !== put || p.header !== null; words++) {
      if (words > 1 << 22) throw new MachineFault('the command FIFO never reaches the put pointer');
      if (p.header !== null) {
        // The rest of a header's methods, each word one method
        if (get === put) break;
        const h = p.header;
        const value = this.m.physRead4(u32(base + get));
        get = u32(get + 4);
        h.left--;
        if (h.left === 0) p.header = null;
        this.method(h.subchannel, h.method, value);
        h.method += h.step;
        this.pushed++;
        if (p.stall !== STALL_NONE) break;
        continue;
      }
      const word = this.m.physRead4(u32(base + get));
      get = u32(get + 4);
      if ((word & 0xE0000003) === CMD_JUMP_OLD) { get = word & 0x1FFFFFFC; continue; }
      if ((word & 3) === 1) { get = word & 0xFFFFFFFC; continue; }
      if ((word & 0xE0000003) === CMD_CALL) { p.calls.push(get); get = word & 0x1FFFFFFC; continue; }
      if (word === CMD_RETURN) { if (!p.calls.length) throw new MachineFault('return with no call in the command FIFO'); get = p.calls.pop(); continue; }
      if ((word & 0xE0030003) !== 0 && (word & 0xE0030003) !== CMD_NON_INCREASING) throw new MachineFault(`command word ${hex8(word)} in the FIFO`);
      const count = (word >>> 18) & 0x7FF;
      if (count === 0) continue;
      p.header = { left: count, subchannel: (word >>> 13) & 7, method: word & 0x1FFC, step: (word & CMD_NON_INCREASING) ? 0 : 4 };
    }
    this.regs.set(NV_PFIFO_CACHE1_DMA_GET, get);
  }

  // Lets the pusher go on after the kernel handled what stopped it
  resume(reason) {
    if (this.pusher.stall !== reason) return;
    this.pusher.stall = STALL_NONE;
    this.consumePushBuffer();
  }

  surfaceField(shift) { return ((this.regs.get(NV_PGRAPH_SURFACE) || 0) >>> shift) & SURFACE_FIELD; }

  setSurfaceField(shift, v) {
    const old = this.regs.get(NV_PGRAPH_SURFACE) || 0;
    this.regs.set(NV_PGRAPH_SURFACE, u32((old & ~(SURFACE_FIELD << shift)) | ((v & SURFACE_FIELD) << shift)));
  }

  // The engine stalls on a flip while the frame it writes next is the one being read
  flipStalled() {
    return this.surfaceField(SURFACE_READ_SHIFT) === this.surfaceField(SURFACE_WRITE_SHIFT);
  }

  // A method the hardware leaves to the kernel traps with its address and data as an error, the pusher stopping until
  // the kernel clears the interrupt
  trap(subchannel, method, value) {
    this.regs.set(NV_PGRAPH_TRAPPED_ADDR, u32((subchannel << 16) | method));
    this.regs.set(NV_PGRAPH_TRAPPED_DATA, value >>> 0);
    this.regs.set(NV_PGRAPH_NSOURCE, NV_PGRAPH_NSOURCE_NOTIFICATION);
    this.regs.set(NV_PGRAPH_INTR, u32((this.regs.get(NV_PGRAPH_INTR) || 0) | NV_PGRAPH_INTR_ERROR));
    this.pusher.stall = STALL_TRAP;
    this.updateInterrupt();
  }

  // One method on a subchannel: binding an object, a memory side effect the FIFO performs itself, or state for the
  // renderer to take
  method(subchannel, method, value) {
    if (method === METHOD_SET_OBJECT) {
      this.subchannels[subchannel] = { handle: value >>> 0, cls: this.objectClass(value) };
      return;
    }
    const bound = this.subchannels[subchannel];
    if (bound === null) throw new MachineFault(`method ${hex8(method)} on subchannel ${subchannel} with no object bound`);
    if (bound.cls === CLASS_KELVIN) {
      switch (method) {
        case NV097_NO_OPERATION:
          // A no operation with a parameter is a call into the kernel's own method handler
          if (value !== 0) this.trap(subchannel, method, value);
          return;
        case NV097_WAIT_FOR_IDLE: return;
        case NV097_SET_FLIP_READ: this.setSurfaceField(SURFACE_READ_SHIFT, value); return;
        case NV097_SET_FLIP_WRITE: this.setSurfaceField(SURFACE_WRITE_SHIFT, value); return;
        case NV097_SET_FLIP_MODULO: this.setSurfaceField(SURFACE_MODULO_SHIFT, value); return;
        case NV097_FLIP_INCREMENT_WRITE: {
          const modulo = this.surfaceField(SURFACE_MODULO_SHIFT) || 1;
          this.setSurfaceField(SURFACE_WRITE_SHIFT, (this.surfaceField(SURFACE_WRITE_SHIFT) + 1) % modulo);
          return;
        }
        case NV097_FLIP_STALL:
          if (this.flipStalled()) this.pusher.stall = STALL_FLIP;
          return;
        case NV097_SET_CONTEXT_DMA_NOTIFIES: this.kelvin.notifies = this.dmaObject(value); return;
        case NV097_SET_CONTEXT_DMA_SEMAPHORE: this.kelvin.semaphore = this.dmaObject(value); return;
        case NV097_SET_SEMAPHORE_OFFSET: this.kelvin.semaphoreOffset = value >>> 0; return;
        case NV097_BACK_END_WRITE_SEMAPHORE_RELEASE:
          if (this.kelvin.semaphore === null) throw new MachineFault('semaphore release with no semaphore window');
          this.dmaWrite4(this.kelvin.semaphore, this.kelvin.semaphoreOffset, value);
          return;
        case NV097_NOTIFY_WRITE_ONLY:
          // A notifier takes the timer and a zero status in its fourth word
          if (this.kelvin.notifies === null) throw new MachineFault('notify with no notifier window');
          this.dmaWrite4(this.kelvin.notifies, 0, u32(this.time() % 0x100000000));
          this.dmaWrite4(this.kelvin.notifies, 4, u32(Math.floor(this.time() / 0x100000000)));
          this.dmaWrite4(this.kelvin.notifies, 8, 0);
          this.dmaWrite4(this.kelvin.notifies, 12, 0);
          return;
        case NV097_SET_ZSTENCIL_CLEAR_VALUE: this.regs.set(NV_PGRAPH_ZSTENCILCLEARVALUE, value >>> 0); break;
        case NV097_SET_COLOR_CLEAR_VALUE: this.regs.set(NV_PGRAPH_COLORCLEARVALUE, value >>> 0); break;
      }
    }
    if (this.methods !== null) this.methods(bound.cls, method, value);
  }

  // Registers

  // The channel register a user window address stands for, or the address itself outside the user windows
  userAlias(reg) {
    if (reg < NV_USER) return reg;
    const inWindow = (reg - NV_USER) % NV_USER_CHANNEL_SIZE;
    if (inWindow === NV_USER_DMA_PUT) return NV_PFIFO_CACHE1_DMA_PUT;
    if (inWindow === NV_USER_DMA_GET) return NV_PFIFO_CACHE1_DMA_GET;
    return reg;
  }

  readRegister(reg) {
    reg = this.userAlias(reg);
    switch (reg) {
      case NV_PTIMER_TIME_0: return u32(this.time() % 0x100000000) & 0xFFFFFFE0;
      case NV_PTIMER_TIME_1: return u32(Math.floor(this.time() / 0x100000000)) & 0x1FFFFFFF;
      case NV_PCRTC_RASTER: return this.raster();
      case NV_PGRAPH_STATUS: return 0;
      case NV_PGRAPH_INCREMENT: return 0;
      case NV_PFIFO_DEBUG_0: return 0;
      case NV_PFIFO_RUNOUT_STATUS: return NV_PFIFO_RUNOUT_EMPTY;
      case NV_PFIFO_CACHE1_STATUS: return NV_PFIFO_CACHE1_STATUS_EMPTY;
      case NV_PFIFO_CACHE1_DMA_PUSH: return (this.regs.get(reg) || 0) | NV_PFIFO_CACHE1_DMA_PUSH_EMPTY;
      case NV_PFIFO_CACHE1_DMA_GET: this.consumePushBuffer(); return this.regs.get(reg) || 0;
    }
    const v = this.regs.get(reg);
    if (v === undefined) { this.bus.note(`gpu read of unset register ${hex8(reg)}`); return 0; }
    return v;
  }

  read(index, off, size) {
    if (index !== 0) throw new MachineFault(`graphics window ${index} read at ${hex8(off)}`);
    if (off === NV_PRMCIO_CRX && size === 1) return this.crtcIndex;
    if (off === NV_PRMCIO_CR && size === 1) return this.crtc[this.crtcIndex];
    if (off === NV_PRMCIO_INP0 && size === 1) return this.inVblank() ? NV_PRMCIO_INP0_VBLANK : 0;
    if (off >= NV_PRAMIN && off < NV_PRAMIN + NV_PRAMIN_SIZE) {
      const at = off - NV_PRAMIN;
      if (size === 4) return this.raminView.getUint32(at, true);
      if (size === 2) return this.raminView.getUint16(at, true);
      return this.ramin[at];
    }
    const reg = off & ~3;
    const v = this.readRegister(reg);
    if (size === 4) return v;
    return (v >>> (8 * (off & 3))) & (size === 1 ? 0xFF : 0xFFFF);
  }

  write(index, off, size, v) {
    if (index !== 0) throw new MachineFault(`graphics window ${index} write at ${hex8(off)}`);
    if (off === NV_PRMCIO_CRX && size === 1) { this.crtcIndex = v & 0xFF; return; }
    if (off === NV_PRMCIO_CR && size === 1) { this.crtc[this.crtcIndex] = v & 0xFF; return; }
    if (off >= NV_PRAMIN && off < NV_PRAMIN + NV_PRAMIN_SIZE) {
      const at = off - NV_PRAMIN;
      if (size === 4) this.raminView.setUint32(at, v >>> 0, true);
      else if (size === 2) this.raminView.setUint16(at, v & 0xFFFF, true);
      else this.ramin[at] = v & 0xFF;
      return;
    }
    const reg = this.userAlias(off & ~3);
    if (size !== 4) {
      const old = this.regs.get(reg) || 0;
      const shift = 8 * (off & 3), mask = size === 1 ? 0xFF : 0xFFFF;
      v = u32((old & ~(mask << shift)) | ((v & mask) << shift));
    }
    v >>>= 0;
    switch (reg) {
      // A write of ones clears the interrupt bits it names, the engine going on once its trap is taken
      case NV_PCRTC_INTR_0: case NV_PFIFO_INTR_0:
        this.regs.set(reg, u32((this.regs.get(reg) || 0) & ~v));
        this.updateInterrupt();
        return;
      case NV_PGRAPH_INTR:
        this.regs.set(reg, u32((this.regs.get(reg) || 0) & ~v));
        this.updateInterrupt();
        if (((this.regs.get(reg) || 0) & NV_PGRAPH_INTR_ERROR) === 0) this.resume(STALL_TRAP);
        return;
      case NV_PGRAPH_INCREMENT:
        // The kernel reads the next frame once it has shown one, which lets a stalled flip go on
        if (v & NV_PGRAPH_INCREMENT_READ_3D) {
          const modulo = this.surfaceField(SURFACE_MODULO_SHIFT) || 1;
          this.setSurfaceField(SURFACE_READ_SHIFT, (this.surfaceField(SURFACE_READ_SHIFT) + 1) % modulo);
          if (!this.flipStalled()) this.resume(STALL_FLIP);
        }
        return;
      // Enabling any side of the FIFO lets the pusher take what waits in the buffer
      case NV_PGRAPH_FIFO: case NV_PFIFO_CACHE1_PUSH0: case NV_PFIFO_CACHE1_PULL0: case NV_PFIFO_CACHE1_DMA_PUSH:
        this.regs.set(reg, v);
        this.consumePushBuffer();
        return;
      case NV_PMC_INTR_0:
        this.regs.set(reg, u32(((this.regs.get(reg) || 0) & ~v) | (v & NV_PMC_INTR_SOFTWARE)));
        this.updateInterrupt();
        return;
      case NV_PMC_INTR_EN_0: case NV_PCRTC_INTR_EN_0: case NV_PFIFO_INTR_EN_0: case NV_PGRAPH_INTR_EN:
        this.regs.set(reg, v);
        this.updateInterrupt();
        return;
      case NV_PFB_WBC:
        // The write back cache flush completes at once
        this.regs.set(reg, u32(v & ~NV_PFB_WBC_FLUSH));
        return;
      case NV_PFIFO_CACHE1_DMA_PUT:
        this.regs.set(reg, v);
        this.consumePushBuffer();
        return;
    }
    this.regs.set(reg, v);
    this.bus.note(`gpu write of ${hex8(v)} to ${hex8(reg)}`);
  }
}
