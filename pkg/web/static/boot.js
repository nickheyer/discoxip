// Boots the console the way its ROM does: the x-codes run against the devices, the decrypted 2BL lies where the
// preldr leaves it, and the machine starts at the 2BL's entry with the kernel load answered by the build's bytes

import { Machine, MachineFault, u32, hex8 } from './machine.js';
import { Bus } from './devices.js';

const PCI_CONFIG_ADDRESS = 0xCF8, PCI_CONFIG_DATA = 0xCFC;
// The x-code opcodes the ROM interprets
const XCODE_PEEK = 0x02, XCODE_POKE = 0x03, XCODE_POKEPCI = 0x04, XCODE_PEEKPCI = 0x05, XCODE_ANDOR = 0x06, XCODE_CHAIN = 0x07;
const XCODE_BNE = 0x08, XCODE_BRA = 0x09, XCODE_RESULT = 0x10, XCODE_OUTB = 0x11, XCODE_INB = 0x12, XCODE_EXIT = 0xEE;
const XCODE_BYTES = 9, XCODE_PEEK_MASK = 0x0FFFFFFF;
// The ROM keeps the south bridge from hiding itself through a configuration write of this register
const ROM_HIDE_ADDRESS = 0x80000880, ROM_HIDE_BIT = 2;

// Runs the init table as the ROM does, the accumulator and result registers starting clear
export function runXCodes(xcodes, m, bus) {
  let acc = 0, result = 0, pc = 0;
  for (let steps = 0; ; steps++) {
    if (pc < 0 || pc >= xcodes.length) throw new MachineFault(`x-code ${pc} lies outside the init table`);
    if (steps > 100000) throw new MachineFault(`the init table never exits, spinning at x-code ${pc} with ${hex8(acc)} in the accumulator`);
    const x = xcodes[pc];
    let op = x.op, a = x.a >>> 0, b = x.b >>> 0;
    if (op === XCODE_CHAIN) { op = a & 0xFF; a = b; b = acc; }
    switch (op) {
      case XCODE_PEEK: acc = m.physRead4(a & XCODE_PEEK_MASK); break;
      case XCODE_POKE: m.physWrite4(a, b); break;
      case XCODE_POKEPCI:
        if (a === ROM_HIDE_ADDRESS) b = u32(b & ~ROM_HIDE_BIT);
        bus.portOut(PCI_CONFIG_ADDRESS, a, 4);
        bus.portOut(PCI_CONFIG_DATA, b, 4);
        break;
      case XCODE_PEEKPCI:
        bus.portOut(PCI_CONFIG_ADDRESS, a, 4);
        acc = bus.portIn(PCI_CONFIG_DATA, 4);
        break;
      case XCODE_ANDOR: acc = u32((acc & a) | b); break;
      case XCODE_BNE: if (acc !== a) pc += (b | 0) / XCODE_BYTES; break;
      case XCODE_BRA: pc += (b | 0) / XCODE_BYTES; break;
      case XCODE_RESULT: result = u32((result & a) | b); acc = result; break;
      case XCODE_OUTB: bus.portOut(a & 0xFFFF, b & 0xFF, 1); break;
      case XCODE_INB: acc = bus.portIn(a & 0xFFFF, 1); break;
      case XCODE_EXIT: return;
      default: break;
    }
    pc++;
  }
}

// Assembles the machine from the build's model, boot state, hardware table and memory runs
export function assemble({ model, boot, hardware, files, now }) {
  const bus = new Bus(hardware, { flash: files.flash, eeprom: files.eeprom, disk: files.disk, now });
  const m = new Machine(model, hardware, bus);
  runXCodes(boot.xcodes, m, bus);
  if (files.bldr.length !== boot.bldr.size) throw new MachineFault(`the 2BL run holds ${files.bldr.length} bytes, the boot names ${boot.bldr.size}`);
  m.ram.set(files.bldr, boot.bldr.at);
  // The kernel load routine lands the decrypted and decompressed kernel where the 2BL puts it and returns
  m.hook(boot.kernel.va, (mm) => {
    const pa = mm.translate(boot.kernel.run.at, true);
    mm.ram.set(files.kernel, pa);
    return mm.ret(boot.kernel.pops);
  });
  for (const [name, v] of Object.entries(boot.regs)) m.regs[name] = v >>> 0;
  m.gdtr = { base: boot.gdt.base >>> 0, limit: boot.gdt.limit };
  m.cr[0] = boot.cr0 >>> 0;
  for (const [name, sel] of Object.entries(boot.segments)) m.segs[name] = sel;
  return { m, bus };
}

// Where the machine starts
export function entryKey(m, boot) {
  return m.dispatch(boot.entry);
}

export { Machine, MachineFault, Bus, u32, hex8 };
