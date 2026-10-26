// The NV2A as the runtime stands in for it: its registers hold what the executable writes them, and its command
// channel consumes every command pushed to it as soon as the put pointer moves

// Bytes of register space the GPU decodes from the base the executable's miniport maps it at
const REGISTER_SPACE = 0x1000000;
// Get pointers standing where their put pointers do, since the GPU drains what it is fed at once: the user area's own
// and the fifo cache's
const DRAINED = new Map([[0x800044, 0x800040], [0x3244, 0x3240]]);
// Bits the GPU clears as soon as the work they start is done: the write combining flush and the push engine's busy state
const SELF_CLEARING = new Map([[0x100410, 0x10000], [0x3220, 0x10]]);
// Bits reporting an empty fifo, which the drained GPU always has: the cache's and the runout's low marks
const ALWAYS_SET = new Map([[0x3214, 0x10], [0x2400, 0x10]]);
// Interrupt status of the master control, bus, fifo, video, timer, graphics and display engines, which report the
// events the GPU raised and clear the bits written to them, so they read clear while it raises none
const INTERRUPT_STATUS = new Set([0x000100, 0x001100, 0x002100, 0x008100, 0x009100, 0x400100, 0x600100]);

export class GpuRegisters {
  constructor(base) {
    this.base = base >>> 0;
    this.words = new Map();
  }

  // Reports whether an address falls in the GPU's register space
  claims(addr) {
    return this.base !== 0 && addr >= this.base && addr < this.base + REGISTER_SPACE;
  }

  // Reads a register as the GPU reports it once all the work it was given is done
  read(addr, size) {
    const off = addr - this.base;
    if (size === 4 && (off & 3) !== 0) throw new Error(`GPU register read of 4 bytes at unaligned +0x${off.toString(16)}`);
    const reg = off & ~3, at = DRAINED.get(reg) ?? reg;
    if (INTERRUPT_STATUS.has(reg)) return 0;
    const word = (((this.words.get(at) ?? 0) & ~(SELF_CLEARING.get(reg) ?? 0)) | (ALWAYS_SET.get(reg) ?? 0)) >>> 0;
    if (size === 4) return word;
    return (word >>> (8 * (off & 3))) & (size === 1 ? 0xFF : 0xFFFF);
  }

  // Writes the bytes of a register a store covers
  write(addr, size, bits) {
    const off = addr - this.base, at = off & ~3, shift = 8 * (off & 3);
    if (size === 4 && shift !== 0) throw new Error(`GPU register write of 4 bytes at unaligned +0x${off.toString(16)}`);
    if (size === 4) {
      this.words.set(at, bits >>> 0);
      return;
    }
    const mask = ((size === 1 ? 0xFF : 0xFFFF) << shift) >>> 0;
    const old = this.words.get(at) ?? 0;
    this.words.set(at, ((old & ~mask) | ((bits << shift) & mask)) >>> 0);
  }
}
