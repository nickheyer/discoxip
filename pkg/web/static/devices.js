// The hardware under the kernel: the buses and devices answering the port, configuration and memory accesses the
// lifted code makes, each with the registers and behaviour of the part the console carries

import { MachineFault, u32, hex8 } from './machine.js';
import { IDE } from './ide.js';
import { NV2A } from './nv2a.js';
import { APU } from './apu.js';

const PCI_CONFIG_ADDRESS = 0xCF8, PCI_CONFIG_DATA = 0xCFC;
const CONFIG_COMMAND = 0x04, CONFIG_BAR0 = 0x10, CONFIG_BRIDGE_BUSES = 0x18, CONFIG_INTERRUPT_LINE = 0x3C;
const COMMAND_IO = 1, COMMAND_MEMORY = 2;
// Where the south bridge keeps the bit that hides the boot ROM and the base of its power management block
const LPC_ROM_HIDE = 0x80, LPC_ROM_HIDDEN = 2, LPC_ACPI_BASE = 0x84;

// One function on the bus, its configuration space built from the hardware table and its windows from its registers
class PCIFunction {
  constructor(desc, bus) {
    this.desc = desc;
    this.bus = bus;
    this.config = new Uint8Array(256);
    this.dv = new DataView(this.config.buffer);
    this.dv.setUint16(0, desc.vendor, true);
    this.dv.setUint16(2, desc.id, true);
    this.config[8] = desc.revision;
    this.config[9] = desc.class & 0xFF;
    this.dv.setUint16(10, desc.class >>> 8, true);
    this.config[14] = desc.header_type;
    if (desc.header_type === 0) this.dv.setUint32(0x2C, desc.subsystem, true);
    this.config[CONFIG_INTERRUPT_LINE] = desc.irq;
    this.config[CONFIG_INTERRUPT_LINE + 1] = desc.pin;
    if (desc.bridge) {
      this.config[CONFIG_BRIDGE_BUSES] = desc.bus;
      this.config[CONFIG_BRIDGE_BUSES + 1] = desc.bridge.secondary;
      this.config[CONFIG_BRIDGE_BUSES + 2] = desc.bridge.secondary;
    }
    this.bars = new Map();
    for (const b of desc.bars || []) {
      this.bars.set(b.index, b);
      this.dv.setUint32(CONFIG_BAR0 + 4 * b.index, b.io ? 1 : (b.prefetch ? 8 : 0), true);
    }
    this.device = null;
  }

  get name() { return this.desc.name; }

  read(off, size) {
    switch (size) {
      case 1: return this.config[off];
      case 2: return this.dv.getUint16(off & ~1, true);
      default: return this.dv.getUint32(off & ~3, true);
    }
  }

  write(off, size, v) {
    const word = off & ~3;
    const old = this.dv.getUint32(word, true);
    let merged;
    if (size === 4) merged = v >>> 0;
    else {
      const shift = 8 * (off & 3), mask = size === 1 ? 0xFF : 0xFFFF;
      merged = u32((old & ~(mask << shift)) | ((v & mask) << shift));
    }
    if (word >= CONFIG_BAR0 && word < CONFIG_BAR0 + 24 && this.bars.has((word - CONFIG_BAR0) / 4)) {
      const b = this.bars.get((word - CONFIG_BAR0) / 4);
      // The size bits read back as zero and the type bits as the window's kind
      const mask = u32(~(b.size - 1)) & (b.io ? 0xFFFFFFFC : 0xFFFFFFF0);
      merged = u32((merged & mask) | (b.io ? 1 : (b.prefetch ? 8 : 0)));
    } else if (word === 0) {
      return;
    }
    this.dv.setUint32(word, merged, true);
    if (word === CONFIG_COMMAND || (word >= CONFIG_BAR0 && word < CONFIG_BAR0 + 24)) this.bus.remap();
    if (this.device && this.device.configWritten) this.device.configWritten(word, merged);
  }

  // The windows the function decodes while its command register enables them
  windows() {
    const cmd = this.dv.getUint16(CONFIG_COMMAND, true);
    const out = [];
    for (const [index, b] of this.bars) {
      const raw = this.dv.getUint32(CONFIG_BAR0 + 4 * index, true);
      const base = (b.io ? raw & 0xFFFFFFFC : raw & 0xFFFFFFF0) >>> 0;
      if (base === 0) continue;
      if (b.io ? (cmd & COMMAND_IO) === 0 : (cmd & COMMAND_MEMORY) === 0) continue;
      out.push({ base, size: b.size, io: b.io, alias: b.alias, index, fn: this });
    }
    return out;
  }
}

// The buses the processor reaches: port space, configuration space and the memory windows the devices decode
export class Bus {
  constructor(hardware, files) {
    this.hardware = hardware;
    this.files = files;
    this.m = null;
    this.functions = new Map();
    this.byName = new Map();
    for (const d of hardware.pci) {
      const fn = new PCIFunction(d, this);
      this.functions.set(`${d.bus}:${d.device}:${d.function}`, fn);
      this.byName.set(d.name, fn);
    }
    this.configAddress = 0;
    this.ports = new Map();
    this.mmio = [];
    this.aliases = [];
    this.log = null;
    this.pic = new PIC(this);
    this.pit = new PIT(this);
    this.rtc = new RTC(this);
    this.acpi = new ACPI(this);
    this.smbus = new SMBusHost(this);
    this.gpu = new NV2A(this);
    this.misc = new MiscPorts(this);
    this.ide = new IDE(this, files.disk);
    this.devices = [this.pic, this.pit, this.rtc, this.acpi, this.smbus, this.gpu, this.misc, this.ide];
    this.byName.get('smbus').device = this.smbus;
    this.byName.get('gpu').device = this.gpu;
    this.byName.get('lpc').device = this.acpi;
    this.byName.get('ide').device = this.ide;
    // The audio processor, its codec interface, the network controller and the USB controllers keep the registers
    // the kernel programs, every value read back as written and every unset register noted
    for (const name of ['nic', 'usb0', 'usb1']) {
      const fn = this.byName.get(name);
      fn.device = new RegisterFile(this, name);
      this.devices.push(fn.device);
    }
    this.apu = new APU(this);
    this.byName.get('apu').device = this.apu;
    this.devices.push(this.apu);
    this.aci = new AC97(this);
    this.byName.get('aci').device = this.aci;
    this.devices.push(this.aci);
    this.flash = files.flash;
    this.remap();
  }

  attach(m) {
    this.m = m;
    for (const d of this.devices) if (d.attach) d.attach(m);
  }

  // Rebuilds the port and memory maps from every device's configuration space
  remap() {
    this.ports = new Map();
    this.mmio = [];
    this.aliases = [];
    for (const d of this.devices) if (d.fixedPorts) for (const [port, n] of d.fixedPorts()) for (let i = 0; i < n; i++) this.ports.set(port + i, d);
    for (const fn of this.functions.values()) {
      for (const w of fn.windows()) {
        if (w.io) {
          for (let i = 0; i < w.size; i++) this.ports.set(w.base + i, { fn, index: w.index, base: w.base });
        } else if (w.alias) {
          this.aliases.push(w);
        } else {
          this.mmio.push(w);
        }
      }
    }
    if (this.acpi) for (const [port, n] of this.acpi.ports()) for (let i = 0; i < n; i++) this.ports.set(port + i, this.acpi);
  }

  note(message) {
    if (this.log) this.log(message);
  }

  // Port space

  portIn(port, size) {
    if (port === PCI_CONFIG_ADDRESS) return this.configAddress;
    if (port >= PCI_CONFIG_DATA && port < PCI_CONFIG_DATA + 4) return this.configRead(port - PCI_CONFIG_DATA, size);
    const h = this.ports.get(port);
    if (h === undefined) throw new MachineFault(`port ${port.toString(16)} read (${size} bytes) reaches no device ${this.m.describe()}`);
    if (h.fn !== undefined) {
      if (!h.fn.device) throw new MachineFault(`port ${port.toString(16)} read reaches ${h.fn.name} which has no model ${this.m.describe()}`);
      return h.fn.device.windowIn(h.index, port - h.base, size) >>> 0;
    }
    return h.portIn(port, size) >>> 0;
  }

  portOut(port, v, size) {
    if (port === PCI_CONFIG_ADDRESS) { this.configAddress = v >>> 0; return; }
    if (port >= PCI_CONFIG_DATA && port < PCI_CONFIG_DATA + 4) { this.configWrite(port - PCI_CONFIG_DATA, size, v); return; }
    const h = this.ports.get(port);
    if (h === undefined) throw new MachineFault(`port ${port.toString(16)} write of ${hex8(v)} (${size} bytes) reaches no device ${this.m.describe()}`);
    if (h.fn !== undefined) {
      if (!h.fn.device) throw new MachineFault(`port ${port.toString(16)} write reaches ${h.fn.name} which has no model ${this.m.describe()}`);
      h.fn.device.windowOut(h.index, port - h.base, size, v >>> 0);
      return;
    }
    h.portOut(port, size, v >>> 0);
  }

  // Configuration space, through the address the processor latched

  configTarget() {
    const a = this.configAddress;
    if ((a & 0x80000000) === 0) return null;
    const bus = (a >>> 16) & 0xFF, dev = (a >>> 11) & 0x1F, fn = (a >>> 8) & 7;
    return { fn: this.functions.get(`${bus}:${dev}:${fn}`) || null, reg: a & 0xFC };
  }

  configRead(offset, size) {
    const t = this.configTarget();
    if (t === null || t.fn === null) return size === 4 ? 0xFFFFFFFF : (size === 2 ? 0xFFFF : 0xFF);
    return t.fn.read(t.reg + offset, size);
  }

  configWrite(offset, size, v) {
    const t = this.configTarget();
    if (t === null || t.fn === null) return;
    t.fn.write(t.reg + offset, size, v);
  }

  // Memory space past the processor's memory

  // The memory offset a physical address past memory aliases, through the memory controller's mirror or a device
  // window opening onto memory, and -1 where it aliases none
  ramAlias(pa) {
    const ram = this.hardware.memory;
    if (pa < 2 * ram) return pa % ram;
    for (const w of this.aliases) if (pa >= w.base && pa < w.base + w.size) return (pa - w.base) % ram;
    return -1;
  }

  window(pa) {
    for (const w of this.mmio) if (pa >= w.base && pa < w.base + w.size) return w;
    return null;
  }

  read(pa, size) {
    const w = this.window(pa);
    if (w !== null) {
      if (!w.fn.device) throw new MachineFault(`read of ${hex8(pa)} reaches ${w.fn.name} which has no model ${this.m.describe()}`);
      return w.fn.device.read(w.index, pa - w.base, size) >>> 0;
    }
    if (pa >= this.hardware.flash_mirror_base) return this.flashRead(pa, size);
    throw new MachineFault(`read of ${hex8(pa)} (${size} bytes) reaches no memory or device ${this.m.describe()}`);
  }

  write(pa, size, v) {
    const w = this.window(pa);
    if (w !== null) {
      if (!w.fn.device) throw new MachineFault(`write of ${hex8(v)} to ${hex8(pa)} reaches ${w.fn.name} which has no model ${this.m.describe()}`);
      w.fn.device.write(w.index, pa - w.base, size, v >>> 0);
      return;
    }
    if (pa >= this.hardware.flash_mirror_base) {
      this.note(`flash write of ${hex8(v)} at ${hex8(pa)} ignored`);
      return;
    }
    throw new MachineFault(`write of ${hex8(v)} to ${hex8(pa)} (${size} bytes) reaches no memory or device ${this.m.describe()}`);
  }

  // The flash repeats through its window, the boot ROM covering its last bytes until the south bridge hides it
  flashRead(pa, size) {
    if (pa >= this.hardware.rom_overlay_base && (this.byName.get('lpc').config[LPC_ROM_HIDE] & LPC_ROM_HIDDEN) === 0) {
      throw new MachineFault(`read of the boot ROM at ${hex8(pa)} while it is still mapped ${this.m.describe()}`);
    }
    const off = (pa - this.hardware.flash_mirror_base) % this.flash.length;
    let v = 0;
    for (let i = size - 1; i >= 0; i--) v = (v * 256) + this.flash[(off + i) % this.flash.length];
    return v >>> 0;
  }

  // Interrupts

  raise(irq) { this.pic.raise(irq); }
  lower(irq) { this.pic.lower(irq); }
  acknowledge() { return this.pic.acknowledge(); }
}

// The pair of 8259 interrupt controllers, the second cascaded into line 2 of the first
class PIC {
  constructor(bus) {
    this.bus = bus;
    this.m = null;
    this.chips = [this.chip(), this.chip()];
    this.lines = 0;
    this.elcr = [0, 0];
  }

  chip() {
    return { icw: [0, 0, 0, 0], step: 0, imr: 0xFF, irr: 0, isr: 0, readISR: false, autoEOI: false, base: 0 };
  }

  attach(m) { this.m = m; }

  // The two controllers and the edge or level trigger selection registers beside them
  fixedPorts() { return [[0x20, 2], [0xA0, 2], [0x4D0, 2]]; }

  // Whether a line is level triggered, as the trigger registers select, an edge latching its request until taken
  level(irq) {
    return ((this.elcr[irq >> 3] >> (irq & 7)) & 1) !== 0;
  }

  // A line raised requests its interrupt, a level staying requested while the line is high
  raise(irq) {
    this.lines |= 1 << irq;
    this.chips[irq >> 3].irr |= 1 << (irq & 7);
    if (irq >= 8) this.chips[0].irr |= 4;
    this.update();
  }

  lower(irq) {
    this.lines &= ~(1 << irq);
    if (this.level(irq)) {
      this.chips[irq >> 3].irr &= ~(1 << (irq & 7));
      if (irq >= 8 && this.chips[1].irr === 0) this.chips[0].irr &= ~4;
    }
    this.update();
  }

  // The highest priority line requested, unmasked and not in service on a chip, or -1
  pending(c) {
    const ready = c.irr & ~c.imr;
    if (ready === 0) return -1;
    for (let i = 0; i < 8; i++) {
      if (c.isr & (1 << i)) return -1;
      if (ready & (1 << i)) return i;
    }
    return -1;
  }

  update() {
    const slave = this.pending(this.chips[1]);
    if (slave >= 0) this.chips[0].irr |= 4;
    const master = this.pending(this.chips[0]);
    this.m.setIRQ(master >= 0);
  }

  // Answers the processor's acknowledge with the vector of the line taken into service
  acknowledge() {
    const master = this.pending(this.chips[0]);
    if (master < 0) return -1;
    let chip = this.chips[0], line = master;
    if (master === 2) {
      const slave = this.pending(this.chips[1]);
      if (slave < 0) return -1;
      chip = this.chips[1];
      line = slave;
    }
    const irq = chip === this.chips[0] ? line : 8 + line;
    if (!chip.autoEOI) chip.isr |= 1 << line;
    // An edge is taken once, a level staying requested while its line is high
    if (!this.level(irq) || ((this.lines >> irq) & 1) === 0) chip.irr &= ~(1 << line);
    if (chip === this.chips[1]) {
      if (!chip.autoEOI) this.chips[0].isr |= 4;
      if (chip.irr === 0) this.chips[0].irr &= ~4;
    }
    this.update();
    return chip.base + line;
  }

  portIn(port, size) {
    if (port === 0x4D0 || port === 0x4D1) return this.elcr[port - 0x4D0];
    const c = this.chips[port >= 0xA0 ? 1 : 0];
    if ((port & 1) === 0) return c.readISR ? c.isr : c.irr;
    return c.imr;
  }

  portOut(port, size, v) {
    if (port === 0x4D0 || port === 0x4D1) { this.elcr[port - 0x4D0] = v & 0xFF; return; }
    const c = this.chips[port >= 0xA0 ? 1 : 0];
    v &= 0xFF;
    if ((port & 1) === 0) {
      if (v & 0x10) {
        // ICW1 starts the initialization sequence
        this.bus.note(`interrupt controller ${port >= 0xA0 ? 2 : 1} initializing with ${v.toString(16)}`);
        c.icw[0] = v;
        c.step = 1;
        c.imr = 0;
        c.isr = 0;
        c.readISR = false;
      } else if (v & 0x08) {
        // OCW3 selects the register reads return
        if (v & 2) c.readISR = (v & 1) !== 0;
      } else {
        // OCW2 ends an interrupt, the line named or the highest in service
        if ((v & 0xE0) === 0x60) c.isr &= ~(1 << (v & 7));
        else if ((v & 0xE0) === 0x20) { for (let i = 0; i < 8; i++) if (c.isr & (1 << i)) { c.isr &= ~(1 << i); break; } }
        else if ((v & 0xE0) === 0xA0 || (v & 0xE0) === 0xE0) { for (let i = 0; i < 8; i++) if (c.isr & (1 << i)) { c.isr &= ~(1 << i); break; } }
        else throw new MachineFault(`interrupt controller command ${v.toString(16)}`);
        if (c === this.chips[1] && c.isr === 0) this.chips[0].isr &= ~4;
      }
      this.update();
      return;
    }
    if (c.step === 1) { c.icw[1] = v; c.base = v & 0xF8; c.step = (c.icw[0] & 2) ? 3 : 2; return; }
    if (c.step === 2) { c.icw[2] = v; c.step = (c.icw[0] & 1) ? 3 : 0; return; }
    if (c.step === 3) { c.icw[3] = v; c.autoEOI = (v & 2) !== 0; c.step = 0; return; }
    c.imr = v;
    this.update();
  }
}

// The 8254 timer, its first channel raising line 0 at the rate the kernel programs
class PIT {
  constructor(bus) {
    this.bus = bus;
    this.m = null;
    this.channels = [this.channel(), this.channel(), this.channel()];
    this.timer = { deadline: Infinity, fire: (m) => this.tick(m) };
  }

  channel() { return { reload: 0, mode: 0, access: 3, latch: null, lowFirst: true, loaded: 0, started: 0, gate: true }; }

  attach(m) { this.m = m; m.schedule(this.timer); }

  fixedPorts() { return [[0x40, 4]]; }

  // Processor cycles one timer count takes
  get cyclesPerCount() { return this.bus.hardware.clock_hz / this.bus.hardware.pit_hz; }

  // The count a channel shows, counting down from its reload on the machine clock
  count(c) {
    if (c.reload === 0) return 0;
    const elapsed = Math.floor((this.m.tsc - c.started) / this.cyclesPerCount);
    const period = c.reload === 0 ? 65536 : c.reload;
    return ((period - (elapsed % period)) % 65536) >>> 0;
  }

  tick(m) {
    const c = this.channels[0];
    if (c.reload === 0) { this.timer.deadline = Infinity; return; }
    const period = c.reload * this.cyclesPerCount;
    this.bus.raise(0);
    this.bus.lower(0);
    this.timer.deadline = this.timer.deadline + period;
    if (this.timer.deadline <= m.tsc) this.timer.deadline = m.tsc + period;
  }

  portIn(port, size) {
    const n = port - 0x40;
    if (n === 3) return 0;
    const c = this.channels[n];
    const v = c.latch !== null ? c.latch : this.count(c);
    if (c.access === 1) { c.latch = null; return v & 0xFF; }
    if (c.access === 2) { c.latch = null; return v >>> 8; }
    if (c.lowFirst) { c.lowFirst = false; return v & 0xFF; }
    c.lowFirst = true;
    c.latch = null;
    return v >>> 8;
  }

  portOut(port, size, v) {
    const n = port - 0x40;
    v &= 0xFF;
    if (n === 3) {
      const sel = v >>> 6;
      if (sel === 3) return;
      const c = this.channels[sel];
      const access = (v >>> 4) & 3;
      if (access === 0) { c.latch = this.count(c); c.lowFirst = true; return; }
      c.access = access;
      c.mode = (v >>> 1) & 7;
      c.lowFirst = true;
      c.loaded = 0;
      return;
    }
    const c = this.channels[n];
    if (c.access === 1) c.reload = v;
    else if (c.access === 2) c.reload = v << 8;
    else if (c.lowFirst) { c.loaded = v; c.lowFirst = false; return; }
    else { c.reload = c.loaded | (v << 8); c.lowFirst = true; }
    c.started = this.m.tsc;
    if (n === 0) {
      this.timer.deadline = this.m.tsc + (c.reload === 0 ? 65536 : c.reload) * this.cyclesPerCount;
      this.m.rescheduleEvents();
      this.bus.note(`timer channel 0 reloads every ${c.reload} counts in mode ${c.mode}`);
    }
  }
}

// The CMOS clock of the south bridge, keeping the console's time of day
class RTC {
  constructor(bus) {
    this.bus = bus;
    this.index = 0;
    this.index2 = 0;
    this.ram = new Uint8Array(128);
    this.ram2 = new Uint8Array(128);
    this.ram[0x0A] = 0x26;
    this.ram[0x0B] = 0x02;
    this.ram[0x0D] = 0x80;
  }

  // The clock's registers and the south bridge's second bank of battery backed storage beside them
  fixedPorts() { return [[0x70, 2], [0x72, 2]]; }

  now() { return this.bus.files.now ? this.bus.files.now() : Date.now(); }

  bcd(v) { return (this.ram[0x0B] & 4) ? v : (Math.floor(v / 10) << 4) | (v % 10); }

  portIn(port, size) {
    if (port === 0x70) return this.index;
    if (port === 0x72) return this.index2;
    if (port === 0x73) { this.bus.note(`clock bank 2 read of register ${this.index2.toString(16)}`); return this.ram2[this.index2 & 0x7F]; }
    const i = this.index & 0x7F;
    const d = new Date(this.now());
    switch (i) {
      case 0: return this.bcd(d.getSeconds());
      case 2: return this.bcd(d.getMinutes());
      case 4: return this.bcd(d.getHours());
      case 6: return this.bcd(d.getDay() + 1);
      case 7: return this.bcd(d.getDate());
      case 8: return this.bcd(d.getMonth() + 1);
      case 9: return this.bcd(d.getFullYear() % 100);
      case 0x32: return this.bcd(Math.floor(d.getFullYear() / 100));
      case 0x0A: return this.ram[0x0A] & 0x7F;
      case 0x0C: return 0;
    }
    return this.ram[i];
  }

  portOut(port, size, v) {
    if (port === 0x70) { this.index = v & 0xFF; return; }
    if (port === 0x72) { this.index2 = v & 0xFF; return; }
    if (port === 0x73) { this.ram2[this.index2 & 0x7F] = v & 0xFF; return; }
    const i = this.index & 0x7F;
    if (i <= 9 || i === 0x32) { this.bus.note(`clock write of ${v.toString(16)} to register ${i.toString(16)}`); return; }
    this.ram[i] = v & 0xFF;
  }
}

// The power management block of the south bridge, at the base its configuration names, with the timer the kernel
// stalls on and the general purpose inputs the x-codes and the kernel read the board's straps from
class ACPI {
  constructor(bus) {
    this.bus = bus;
    this.regs = new Uint8Array(256);
    this.m = null;
    // The encoder strap on the general purpose input the x-codes read lies clear for the Conexant part
    this.regs[0xCE] = 0x00;
  }

  attach(m) { this.m = m; }

  get base() {
    const lpc = this.bus.byName.get('lpc');
    return lpc.dv.getUint32(LPC_ACPI_BASE, true) & 0xFFFFFF00;
  }

  ports() {
    const base = this.base;
    return base === 0 ? [] : [[base, 256]];
  }

  configWritten(reg) {
    if (reg === LPC_ACPI_BASE) this.bus.remap();
  }

  // The timer counts at the power management clock from the processor's
  timer() {
    return Math.floor(this.m.tsc * 3579545 / this.bus.hardware.clock_hz) & 0xFFFFFF;
  }

  portIn(port, size) {
    const off = port - this.base;
    if (off === 8 && size === 4) return this.timer();
    let v = 0;
    for (let i = size - 1; i >= 0; i--) v = (v << 8) | this.regs[off + i];
    return v >>> 0;
  }

  portOut(port, size, v) {
    const off = port - this.base;
    for (let i = 0; i < size; i++) this.regs[off + i] = (v >>> (8 * i)) & 0xFF;
  }
}

// Ports with no device behind them that the boot touches: the POST code, the speaker gate, the fast A20 gate, and
// the super I/O configuration ports the kernel probes for a debug serial port the console does not carry
class MiscPorts {
  constructor(bus) { this.bus = bus; this.post = 0; }
  fixedPorts() { return [[0x80, 1], [0x61, 1], [0x92, 1], [0xED, 1], [0xCF9, 1], [0x2E, 2]]; }
  portIn(port, size) {
    if (port === 0x80) return this.post;
    if (port === 0x2E || port === 0x2F) return 0xFF;
    return 0;
  }
  portOut(port, size, v) {
    if (port === 0x80) { this.post = v & 0xFF; return; }
    if (port === 0xCF9 && (v & 4)) throw new MachineFault('the processor was reset through the south bridge');
  }
}

// The system management bus host, running one transaction at a time against the parts on the bus
const SMB_STATUS = 0, SMB_CONTROL = 2, SMB_ADDRESS = 4, SMB_DATA = 6, SMB_COMMAND = 8, SMB_BLOCK = 9;
const SMB_DONE = 0x10, SMB_ERROR = 0x02;
const SMB_CYCLE_QUICK = 0, SMB_CYCLE_BYTE = 1, SMB_CYCLE_BYTE_DATA = 2, SMB_CYCLE_WORD_DATA = 3, SMB_CYCLE_BLOCK = 5, SMB_START = 8, SMB_INTERRUPT = 0x10;
const SMB_IRQ = 11;

class SMBusHost {
  constructor(bus) {
    this.bus = bus;
    this.status = 0;
    this.control = 0;
    this.address = 0;
    this.data = 0;
    this.command = 0;
    this.block = [];
    this.gpio = new Uint8Array(16);
    const parts = bus.hardware.smbus;
    this.parts = new Map();
    this.smc = new SMC(bus, parts.smc_version, parts.smc_challenge);
    this.eeprom = new EEPROM(bus.files.eeprom);
    this.encoder = new Encoder(bus);
    this.sensor = new TemperatureSensor();
    this.parts.set(parts.smc >>> 1, this.smc);
    this.parts.set(parts.eeprom >>> 1, this.eeprom);
    this.parts.set(parts.encoder >>> 1, this.encoder);
    this.parts.set(parts.sensor >>> 1, this.sensor);
  }

  attach(m) { this.smc.attach(m); }

  windowIn(index, off, size) {
    if (index === 2) return this.gpio[off];
    switch (off) {
      case SMB_STATUS: return this.status;
      case SMB_CONTROL: return this.control;
      case SMB_ADDRESS: return this.address;
      case SMB_DATA: return size === 2 ? this.data & 0xFFFF : this.data & 0xFF;
      case SMB_DATA + 1: return (this.data >>> 8) & 0xFF;
      case SMB_COMMAND: return this.command;
      case SMB_BLOCK: return this.block.length ? this.block.shift() : 0;
    }
    throw new MachineFault(`system management bus register ${off} read`);
  }

  windowOut(index, off, size, v) {
    if (index === 2) { this.gpio[off] = v & 0xFF; return; }
    switch (off) {
      case SMB_STATUS: this.status &= ~v; if ((this.status & SMB_DONE) === 0) this.bus.lower(SMB_IRQ); return;
      case SMB_CONTROL: this.control = v & 0xFF; if (v & SMB_START) this.transact(); return;
      case SMB_ADDRESS: this.address = v & 0xFF; return;
      case SMB_DATA: if (size === 2) this.data = v & 0xFFFF; else this.data = (this.data & 0xFF00) | (v & 0xFF); return;
      case SMB_DATA + 1: this.data = (this.data & 0xFF) | ((v & 0xFF) << 8); return;
      case SMB_COMMAND: this.command = v & 0xFF; return;
      case SMB_BLOCK: this.block.push(v & 0xFF); return;
    }
    throw new MachineFault(`system management bus register ${off} write of ${v.toString(16)}`);
  }

  // Runs the transaction the control register starts against the part the address names
  transact() {
    const part = this.parts.get(this.address >>> 1);
    const read = (this.address & 1) !== 0;
    const cycle = this.control & 7;
    if (part === undefined) {
      this.status |= SMB_ERROR | SMB_DONE;
      this.bus.note(`system management bus address ${this.address.toString(16)} answers nothing`);
      if (this.control & SMB_INTERRUPT) this.bus.raise(SMB_IRQ);
      return;
    }
    switch (cycle) {
      case SMB_CYCLE_QUICK: break;
      case SMB_CYCLE_BYTE:
        if (read) this.data = part.readByte(this.command);
        else part.writeByte(this.command, this.data & 0xFF);
        break;
      case SMB_CYCLE_BYTE_DATA:
        if (read) this.data = part.readByte(this.command);
        else part.writeByte(this.command, this.data & 0xFF);
        break;
      case SMB_CYCLE_WORD_DATA:
        if (read) this.data = part.readWord(this.command);
        else part.writeWord(this.command, this.data & 0xFFFF);
        break;
      case SMB_CYCLE_BLOCK:
        if (read) { this.block = part.readBlock(this.command); this.data = this.block.length; }
        else { part.writeBlock(this.command, this.block.splice(0, this.data & 0xFF)); }
        break;
      default:
        throw new MachineFault(`system management bus cycle ${cycle}`);
    }
    this.status |= SMB_DONE;
    if (this.control & SMB_INTERRUPT) this.bus.raise(SMB_IRQ);
  }
}

// A part with a byte register file, word accesses taking two registers
class SMBusPart {
  constructor() { this.regs = new Uint8Array(256); }
  readByte(reg) { return this.regs[reg & 0xFF]; }
  writeByte(reg, v) { this.regs[reg & 0xFF] = v & 0xFF; }
  readWord(reg) { return this.readByte(reg) | (this.readByte(reg + 1) << 8); }
  writeWord(reg, v) { this.writeByte(reg, v & 0xFF); this.writeByte(reg + 1, v >>> 8); }
  readBlock(reg) { throw new MachineFault(`block read of register ${reg.toString(16)} on a part without blocks`); }
  writeBlock(reg) { throw new MachineFault(`block write of register ${reg.toString(16)} on a part without blocks`); }
}

// Registers of the system management controller the kernel and the boot talk to
const SMC_VERSION = 0x01, SMC_TRAY_STATE = 0x03, SMC_AV_PACK = 0x04, SMC_CPU_TEMP = 0x09, SMC_BOARD_TEMP = 0x0A;
const SMC_INTERRUPT_REASON = 0x11, SMC_SCRATCH = 0x1B, SMC_CHALLENGE = 0x1C, SMC_RESPONSE = 0x20;
const TRAY_CLOSED_NO_MEDIA = 0x40, AV_PACK_STANDARD = 0x06;

// The system management controller, answering the registers the kernel reads and taking the ones it writes
class SMC extends SMBusPart {
  constructor(bus, version, challenge) {
    super();
    this.bus = bus;
    this.version = version;
    this.versionIndex = 0;
    // The challenge the controller poses at power on, which the 2BL must answer before the controller resets the console
    for (let i = 0; i < 4; i++) this.regs[SMC_CHALLENGE + i] = challenge[i];
    this.regs[SMC_TRAY_STATE] = TRAY_CLOSED_NO_MEDIA;
    this.regs[SMC_AV_PACK] = AV_PACK_STANDARD;
    this.regs[SMC_CPU_TEMP] = 40;
    this.regs[SMC_BOARD_TEMP] = 30;
    this.regs[SMC_SCRATCH] = 0;
    this.m = null;
  }

  attach(m) { this.m = m; }

  readByte(reg) {
    reg &= 0xFF;
    if (reg === SMC_VERSION) {
      const c = this.version.charCodeAt(this.versionIndex % this.version.length);
      this.versionIndex++;
      return c;
    }
    if (reg === SMC_INTERRUPT_REASON) { const v = this.regs[reg]; this.regs[reg] = 0; return v; }
    this.bus.note(`smc read of register ${reg.toString(16)} = ${this.regs[reg].toString(16)}`);
    return this.regs[reg];
  }

  writeByte(reg, v) {
    reg &= 0xFF;
    if (reg === SMC_VERSION) { this.versionIndex = 0; return; }
    this.bus.note(`smc write of ${v.toString(16)} to register ${reg.toString(16)}`);
    this.regs[reg] = v & 0xFF;
  }
}

// The serial EEPROM, read as the bytes the build wrote into it
class EEPROM extends SMBusPart {
  constructor(image) {
    super();
    if (!image || image.length !== 256) throw new MachineFault('the EEPROM image must hold 256 bytes');
    this.regs.set(image);
  }
}

// The television encoder, keeping whatever the kernel programs into it
class Encoder extends SMBusPart {
  constructor(bus) { super(); this.bus = bus; }
  writeByte(reg, v) { this.regs[reg & 0xFF] = v & 0xFF; }
}

// The temperature sensor beside the processor
class TemperatureSensor extends SMBusPart {
  constructor() {
    super();
    this.regs[0x00] = 30;
    this.regs[0x01] = 40;
    this.regs[0xFE] = 0x41;
    this.regs[0xFF] = 0x02;
  }
}

// A device keeping what the kernel writes into its windows, read back as written
class RegisterFile {
  constructor(bus, name) {
    this.bus = bus;
    this.name = name;
    this.regs = new Map();
  }

  key(index, off) { return index * 0x100000000 + (off & ~3); }

  read(index, off, size) {
    const v = this.regs.get(this.key(index, off));
    if (v === undefined) { this.bus.note(`${this.name} read of unset register ${index}:${hex8(off)}`); return 0; }
    if (size === 4) return v;
    return (v >>> (8 * (off & 3))) & (size === 1 ? 0xFF : 0xFFFF);
  }

  write(index, off, size, v) {
    const k = this.key(index, off);
    if (size !== 4) {
      const old = this.regs.get(k) || 0;
      const shift = 8 * (off & 3), mask = size === 1 ? 0xFF : 0xFFFF;
      v = u32((old & ~(mask << shift)) | ((v & mask) << shift));
    }
    this.regs.set(k, v >>> 0);
    this.bus.note(`${this.name} write of ${hex8(v)} to ${index}:${hex8(off & ~3)}`);
  }

  windowIn(index, off, size) { return this.read(index, off, size); }

  windowOut(index, off, size, v) { this.write(index, off, size, v); }
}

// The audio codec interface: the mixer registers of the codec on its link and the bus master channels, reached through
// two port windows or the memory window laying both out together
const AC97_MIXER_SIZE = 0x80, AC97_BUS_MASTER = 0x100;
const AC97_GLOB_CNT = 0x2C, AC97_GLOB_STA = 0x30, AC97_CAS = 0x34;
const AC97_STA_PRIMARY_READY = 0x100, AC97_STA_SECONDARY_READY = 0x200;
const AC97_CNT_COLD_RESET = 0x02;

class AC97 {
  constructor(bus) {
    this.bus = bus;
    this.mixer = new Uint16Array(AC97_MIXER_SIZE / 2);
    this.channels = new Map();
    this.globalControl = 0;
    this.resetCodec();
  }

  // The codec's registers after reset: full capabilities, every volume muted, the vendor words named
  resetCodec() {
    this.mixer.fill(0);
    this.mixer[0x00 / 2] = 0x0D50;
    for (const reg of [0x02, 0x04, 0x06, 0x0A, 0x0C, 0x0E, 0x10, 0x12, 0x14, 0x16, 0x18]) this.mixer[reg / 2] = 0x8000;
    this.mixer[0x1A / 2] = 0x0000;
    this.mixer[0x1C / 2] = 0x8000;
    this.mixer[0x26 / 2] = 0x000F;
    this.mixer[0x28 / 2] = 0x0001;
    this.mixer[0x2A / 2] = 0x0001;
    this.mixer[0x2C / 2] = 48000;
    this.mixer[0x32 / 2] = 48000;
    this.mixer[0x7C / 2] = 0x4144;
    this.mixer[0x7E / 2] = 0x5370;
  }

  mixerRead(off, size) {
    const w = this.mixer[(off & 0x7E) / 2];
    if (size === 2) return w;
    if (size === 1) return (w >>> (8 * (off & 1))) & 0xFF;
    return w | (this.mixer[((off & 0x7E) + 2) / 2] << 16);
  }

  mixerWrite(off, size, v) {
    const i = (off & 0x7E) / 2;
    if ((off & 0x7E) === 0) { this.resetCodec(); return; }
    if (size === 2 || size === 4) this.mixer[i] = v & 0xFFFF;
    else if (size === 1) this.mixer[i] = (this.mixer[i] & ~(0xFF << (8 * (off & 1)))) | ((v & 0xFF) << (8 * (off & 1)));
    if (size === 4) this.mixer[i + 1] = (v >>> 16) & 0xFFFF;
  }

  // The bus master block: three channels of a buffer descriptor base, the current and last valid indices, the
  // status, the position in the current buffer, the prefetched index and the control byte, then the global registers
  channel(off) {
    const base = off & 0xF0;
    let c = this.channels.get(base);
    if (c === undefined) {
      c = { bdbar: 0, civ: 0, lvi: 0, sr: 0x0001, picb: 0, piv: 0, cr: 0 };
      this.channels.set(base, c);
    }
    return c;
  }

  busMasterRead(off, size) {
    switch (off & ~3) {
      case AC97_GLOB_CNT: return this.globalControl;
      case AC97_GLOB_STA: return AC97_STA_PRIMARY_READY | AC97_STA_SECONDARY_READY | 0x00300000;
      case AC97_CAS: return 0;
    }
    if (off >= 0x30) return 0;
    const c = this.channel(off);
    const bytes = [c.bdbar & 0xFF, (c.bdbar >>> 8) & 0xFF, (c.bdbar >>> 16) & 0xFF, (c.bdbar >>> 24) & 0xFF, c.civ, c.lvi, c.sr & 0xFF, c.sr >>> 8, c.picb & 0xFF, c.picb >>> 8, c.piv, c.cr, 0, 0, 0, 0];
    let v = 0;
    for (let i = size - 1; i >= 0; i--) v = (v * 256) + bytes[(off & 0xF) + i];
    return v >>> 0;
  }

  busMasterWrite(off, size, v) {
    if ((off & ~3) === AC97_GLOB_CNT) {
      if ((v & AC97_CNT_COLD_RESET) === 0) this.resetCodec();
      this.globalControl = v & 0xFF;
      return;
    }
    if (off >= 0x30) return;
    const c = this.channel(off);
    for (let i = 0; i < size; i++) {
      const b = (v >>> (8 * i)) & 0xFF, reg = (off & 0xF) + i;
      switch (reg) {
        case 0: case 1: case 2: case 3: c.bdbar = u32((c.bdbar & ~(0xFF << (8 * reg))) | (b << (8 * reg))); break;
        case 5: c.lvi = b & 0x1F; break;
        case 6: c.sr &= ~(b & 0x1C); break;
        case 0x0B:
          // The reset bit clears the channel and itself at once
          if (b & 0x02) { c.civ = 0; c.lvi = 0; c.sr = 0x0001; c.picb = 0; c.piv = 0; c.cr = b & ~0x02; }
          else c.cr = b & 0x1D;
          break;
      }
    }
    this.bus.note(`aci bus master write of ${hex8(v)} to ${hex8(off)}`);
  }

  windowIn(index, off, size) {
    if (index === 0) return this.mixerRead(off, size);
    if (index === 1) return this.busMasterRead(off, size);
    return this.read(index, off, size);
  }

  windowOut(index, off, size, v) {
    if (index === 0) { this.mixerWrite(off, size, v); return; }
    if (index === 1) { this.busMasterWrite(off, size, v); return; }
    this.write(index, off, size, v);
  }

  read(index, off, size) {
    if (index !== 2) throw new MachineFault(`aci window ${index} read`);
    if (off < AC97_BUS_MASTER) return this.mixerRead(off, size);
    return this.busMasterRead(off - AC97_BUS_MASTER, size);
  }

  write(index, off, size, v) {
    if (index !== 2) throw new MachineFault(`aci window ${index} write`);
    if (off < AC97_BUS_MASTER) { this.mixerWrite(off, size, v); return; }
    this.busMasterWrite(off - AC97_BUS_MASTER, size, v);
  }
}

