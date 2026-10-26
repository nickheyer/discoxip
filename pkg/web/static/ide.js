// The IDE channel of the south bridge with the hard disk and the DVD drive on it, the disk answering from the volumes
// the build laid out and the drive reporting no media, both moving data through the bus master engine

import { MachineFault, u32, hex8 } from './machine.js';

const SECTOR = 512;
const IDE_IRQ = 14;
// Status register bits
const ST_ERR = 0x01, ST_DRQ = 0x08, ST_DSC = 0x10, ST_DF = 0x20, ST_DRDY = 0x40, ST_BSY = 0x80;
// Error register bits
const ER_ABRT = 0x04;
// Device control bits
const DC_NIEN = 0x02, DC_SRST = 0x04;
// Bus master registers and bits
const BM_COMMAND = 0, BM_STATUS = 2, BM_PRD = 4;
const BM_START = 0x01, BM_READ = 0x08, BM_ACTIVE = 0x01, BM_ERROR = 0x02, BM_INTERRUPT = 0x04, BM_DRIVE0_DMA = 0x20, BM_DRIVE1_DMA = 0x40;
const PRD_END = 0x8000;
// Packet interrupt reason bits
const IR_COD = 0x01, IR_IO = 0x02;
// Sense keys and additional sense codes the drive reports
const SENSE_NOT_READY = 0x02, SENSE_ILLEGAL_REQUEST = 0x05, ASC_MEDIUM_NOT_PRESENT = 0x3A, ASC_INVALID_COMMAND = 0x20, ASC_INVALID_FIELD = 0x24;

// The sectors of the disk, read from the build's runs and written into an overlay the session keeps
class DiskImage {
  constructor(index, data) {
    this.sectors = index.sectors;
    this.runs = index.runs;
    this.data = data;
    this.overlay = new Map();
    for (const r of this.runs) if (r.offset + r.size > data.length) throw new MachineFault('the disk index names data past the disk file');
  }

  run(sector) {
    let lo = 0, hi = this.runs.length - 1;
    while (lo <= hi) {
      const mid = (lo + hi) >> 1, r = this.runs[mid];
      if (sector < r.sector) hi = mid - 1;
      else if (sector >= r.sector + r.size / SECTOR) lo = mid + 1;
      else return r;
    }
    return null;
  }

  read(sector, out) {
    const written = this.overlay.get(sector);
    if (written !== undefined) { out.set(written); return; }
    const r = this.run(sector);
    if (r === null) { out.fill(0); return; }
    const at = r.offset + (sector - r.sector) * SECTOR;
    out.set(this.data.subarray(at, at + SECTOR));
  }

  write(sector, bytes) {
    this.overlay.set(sector, Uint8Array.from(bytes));
  }
}

// One device on the channel, holding the task file results of its last command
class Drive {
  constructor(ide, identify) {
    this.ide = ide;
    this.identify = Uint16Array.from(identify);
    this.reset();
  }

  reset() {
    this.status = ST_DRDY | ST_DSC;
    this.error = 0;
    this.count = 1;
    this.lbaLow = 1;
    this.lbaMid = 0;
    this.lbaHigh = 0;
    this.head = 0;
    this.features = 0;
    this.buffer = null;
    this.bufferAt = 0;
    this.afterData = null;
    this.dma = null;
  }

  // Hands back a buffer through the data register and raises the interrupt
  pioIn(bytes, after = null) {
    this.buffer = bytes;
    this.bufferAt = 0;
    this.afterData = after;
    this.status = ST_DRDY | ST_DSC | ST_DRQ;
    this.ide.interrupt();
  }

  // Takes a buffer through the data register, the function running once it is full
  pioOut(size, after) {
    this.buffer = new Uint8Array(size);
    this.bufferAt = 0;
    this.afterData = after;
    this.status = ST_DRDY | ST_DSC | ST_DRQ;
  }

  done() {
    this.buffer = null;
    this.status = ST_DRDY | ST_DSC;
    this.ide.interrupt();
  }

  abort() {
    this.buffer = null;
    this.error = ER_ABRT;
    this.status = ST_DRDY | ST_DSC | ST_ERR;
    this.ide.interrupt();
  }

  dataRead(size) {
    if (this.buffer === null || this.bufferAt >= this.buffer.length) return 0;
    let v = 0;
    for (let i = size - 1; i >= 0; i--) v = (v * 256) + (this.buffer[this.bufferAt + i] || 0);
    this.bufferAt += size;
    if (this.bufferAt >= this.buffer.length) {
      const after = this.afterData;
      this.buffer = null;
      this.afterData = null;
      if (after) after();
      else this.status = ST_DRDY | ST_DSC;
    }
    return v >>> 0;
  }

  dataWrite(size, v) {
    if (this.buffer === null || this.bufferAt >= this.buffer.length) return;
    for (let i = 0; i < size; i++) this.buffer[this.bufferAt + i] = (v >>> (8 * i)) & 0xFF;
    this.bufferAt += size;
    if (this.bufferAt >= this.buffer.length) {
      const after = this.afterData, filled = this.buffer;
      this.buffer = null;
      this.afterData = null;
      this.status = ST_DRDY | ST_DSC | ST_BSY;
      after(filled);
    }
  }
}

// The hard disk, addressed by logical block over the disk image
class HardDisk extends Drive {
  constructor(ide, image, identify) {
    super(ide, identify);
    this.image = image;
  }

  get lba() {
    return u32(((this.head & 0x0F) << 24) | (this.lbaHigh << 16) | (this.lbaMid << 8) | this.lbaLow);
  }

  get sectors() {
    return this.count === 0 ? 256 : this.count;
  }

  command(cmd) {
    switch (cmd) {
      case 0xEC: { // identify device
        const bytes = new Uint8Array(512);
        for (let i = 0; i < 256; i++) { bytes[2 * i] = this.identify[i] & 0xFF; bytes[2 * i + 1] = this.identify[i] >>> 8; }
        this.pioIn(bytes);
        return;
      }
      case 0xA1: this.abort(); return; // identify packet device, which a disk is not
      case 0xEF: // set features
        if (this.features === 0x03) {
          // The transfer mode selected shows in the identify words
          const mode = this.count & 0x07, kind = this.count & 0xF8;
          if (kind === 0x40) { this.identify[88] = (this.identify[88] & 0x00FF) | (1 << (8 + mode)); this.identify[63] &= 0x00FF; }
          else if (kind === 0x20) { this.identify[63] = (this.identify[63] & 0x00FF) | (1 << (8 + mode)); this.identify[88] &= 0x00FF; }
        }
        this.done();
        return;
      case 0x91: case 0xC6: case 0xE7: case 0xE0: case 0xE1: case 0xE3: case 0x10: this.done(); return;
      case 0xE5: this.count = 0xFF; this.done(); return; // check power mode: active
      case 0x40: case 0x41: this.done(); return; // read verify
      case 0x20: case 0x21: case 0xC4: { // read sectors
        const n = this.sectors, lba = this.lba;
        const bytes = new Uint8Array(n * SECTOR);
        for (let i = 0; i < n; i++) this.image.read(lba + i, bytes.subarray(i * SECTOR, (i + 1) * SECTOR));
        this.advance(n);
        this.pioIn(bytes);
        return;
      }
      case 0x30: case 0x31: case 0xC5: { // write sectors
        const n = this.sectors, lba = this.lba;
        this.pioOut(n * SECTOR, (filled) => {
          for (let i = 0; i < n; i++) this.image.write(lba + i, filled.subarray(i * SECTOR, (i + 1) * SECTOR));
          this.advance(n);
          this.done();
        });
        return;
      }
      case 0xC8: case 0xC9: { // read dma
        const n = this.sectors, lba = this.lba;
        const bytes = new Uint8Array(n * SECTOR);
        for (let i = 0; i < n; i++) this.image.read(lba + i, bytes.subarray(i * SECTOR, (i + 1) * SECTOR));
        this.advance(n);
        this.dma = { read: true, bytes };
        this.status = ST_DRDY | ST_DSC | ST_DRQ;
        this.ide.dmaReady();
        return;
      }
      case 0xCA: case 0xCB: { // write dma
        const n = this.sectors, lba = this.lba;
        this.dma = { read: false, size: n * SECTOR, after: (filled) => { for (let i = 0; i < n; i++) this.image.write(lba + i, filled.subarray(i * SECTOR, (i + 1) * SECTOR)); this.advance(n); } };
        this.status = ST_DRDY | ST_DSC | ST_DRQ;
        this.ide.dmaReady();
        return;
      }
    }
    this.ide.bus.note(`hard disk command ${cmd.toString(16)} aborted`);
    this.abort();
  }

  // Leaves the task file at the block after the last one moved
  advance(n) {
    const next = this.lba + n;
    this.lbaLow = next & 0xFF;
    this.lbaMid = (next >>> 8) & 0xFF;
    this.lbaHigh = (next >>> 16) & 0xFF;
    this.head = (this.head & 0xF0) | ((next >>> 24) & 0x0F);
    this.count = 0;
  }
}

// The DVD drive, a packet device with its tray closed on nothing
class DVDDrive extends Drive {
  constructor(ide, identify) {
    super(ide, identify);
    this.sense = [0, 0, 0];
  }

  reset() {
    super.reset();
    // The packet device signature
    this.count = 1;
    this.lbaLow = 1;
    this.lbaMid = 0x14;
    this.lbaHigh = 0xEB;
    this.status = 0;
  }

  command(cmd) {
    switch (cmd) {
      case 0xA1: { // identify packet device
        const bytes = new Uint8Array(512);
        for (let i = 0; i < 256; i++) { bytes[2 * i] = this.identify[i] & 0xFF; bytes[2 * i + 1] = this.identify[i] >>> 8; }
        this.pioIn(bytes);
        return;
      }
      case 0xEC: this.abort(); return;
      case 0x08: this.reset(); this.status = ST_DRDY; this.ide.interrupt(); return; // device reset
      case 0xEF: this.done(); return;
      case 0xA0: // packet: the command bytes follow through the data register
        this.count = IR_COD;
        this.pioOut(12, (packet) => this.packet(packet));
        return;
    }
    this.ide.bus.note(`dvd drive command ${cmd.toString(16)} aborted`);
    this.abort();
  }

  // Answers a packet command with data through the data register, or finishes it
  answer(bytes) {
    const dma = (this.features & 1) !== 0;
    if (bytes === null) {
      this.count = IR_COD | IR_IO;
      this.done();
      return;
    }
    if (dma) {
      this.dma = { read: true, bytes };
      this.count = IR_IO;
      this.status = ST_DRDY | ST_DSC | ST_DRQ;
      this.ide.dmaReady();
      return;
    }
    const limit = this.lbaMid | (this.lbaHigh << 8);
    const sent = limit > 0 && bytes.length > limit ? bytes.subarray(0, limit) : bytes;
    this.lbaMid = sent.length & 0xFF;
    this.lbaHigh = sent.length >>> 8;
    this.count = IR_IO;
    this.pioIn(sent, () => { this.count = IR_COD | IR_IO; this.done(); });
  }

  // Finishes a packet command with a check condition the next sense request reports
  check(key, asc, ascq = 0) {
    this.sense = [key, asc, ascq];
    this.error = (key << 4) | ER_ABRT;
    this.count = IR_COD | IR_IO;
    this.buffer = null;
    this.status = ST_DRDY | ST_DSC | ST_ERR;
    this.ide.interrupt();
  }

  packet(p) {
    const op = p[0];
    switch (op) {
      case 0x00: this.check(SENSE_NOT_READY, ASC_MEDIUM_NOT_PRESENT); return; // test unit ready
      case 0x03: { // request sense
        const s = new Uint8Array(18);
        s[0] = 0x70;
        s[2] = this.sense[0];
        s[7] = 10;
        s[12] = this.sense[1];
        s[13] = this.sense[2];
        this.sense = [0, 0, 0];
        this.answer(s);
        return;
      }
      case 0x12: { // inquiry
        const s = new Uint8Array(36);
        s[0] = 0x05;
        s[1] = 0x80;
        s[2] = 0x00;
        s[3] = 0x21;
        s[4] = 31;
        const text = (at, n, str) => { for (let i = 0; i < n; i++) s[at + i] = i < str.length ? str.charCodeAt(i) : 0x20; };
        text(8, 8, 'DISCOXIP');
        text(16, 16, 'VIRTUAL DVD-ROM');
        text(32, 4, '1.0');
        this.answer(s);
        return;
      }
      case 0x1B: case 0x1E: case 0xBB: this.answer(null); return; // start stop, prevent removal, set speed
      case 0x4A: { // get event status notification: no event
        const s = new Uint8Array(8);
        s[0] = 0; s[1] = 6; s[2] = 0x80 | (p[4] & 0x7F ? 0 : 0); s[3] = 0;
        this.answer(s);
        return;
      }
      case 0x25: case 0x28: case 0xA8: case 0x43: case 0x51: case 0xAD: case 0xBE: // capacity and reads need media
        this.check(SENSE_NOT_READY, ASC_MEDIUM_NOT_PRESENT);
        return;
      case 0x5A: case 0x1A: // mode sense
        this.check(SENSE_NOT_READY, ASC_MEDIUM_NOT_PRESENT);
        return;
    }
    this.ide.bus.note(`dvd packet command ${op.toString(16)} rejected`);
    this.check(SENSE_ILLEGAL_REQUEST, ASC_INVALID_COMMAND);
  }
}

export class IDE {
  constructor(bus, disk) {
    this.bus = bus;
    this.m = null;
    this.image = new DiskImage(disk.index, disk.data);
    this.drives = [new HardDisk(this, this.image, disk.index.identify), new DVDDrive(this, disk.index.dvd_identify)];
    this.selected = 0;
    this.control = 0;
    this.bm = { command: 0, status: BM_DRIVE0_DMA | BM_DRIVE1_DMA, prd: 0 };
  }

  attach(m) { this.m = m; }

  fixedPorts() { return [[0x1F0, 8], [0x3F6, 1]]; }

  get drive() { return this.drives[this.selected]; }

  // The drive's interrupt line sets the bus master's interrupt bit and raises the channel's interrupt
  interrupt() {
    if ((this.control & DC_NIEN) !== 0) return;
    this.bm.status |= BM_INTERRUPT;
    this.bus.raise(IDE_IRQ);
  }

  // Legacy task file ports

  portIn(port, size) {
    if (port === 0x3F6) return this.drive.status;
    const d = this.drive;
    switch (port - 0x1F0) {
      case 0: return d.dataRead(size);
      case 1: return d.error;
      case 2: return d.count;
      case 3: return d.lbaLow;
      case 4: return d.lbaMid;
      case 5: return d.lbaHigh;
      case 6: return 0xA0 | (this.selected << 4) | (d.head & 0x0F);
      case 7:
        // Reading the status takes the interrupt down
        this.bus.lower(IDE_IRQ);
        return d.status;
    }
    throw new MachineFault(`ide port ${port.toString(16)} read`);
  }

  portOut(port, size, v) {
    if (port === 0x3F6) {
      const was = this.control;
      this.control = v & 0xFF;
      if ((v & DC_SRST) && !(was & DC_SRST)) for (const d of this.drives) d.reset();
      return;
    }
    const d = this.drive;
    switch (port - 0x1F0) {
      case 0: d.dataWrite(size, v); return;
      case 1: d.features = v & 0xFF; return;
      case 2: d.count = v & 0xFF; return;
      case 3: d.lbaLow = v & 0xFF; return;
      case 4: d.lbaMid = v & 0xFF; return;
      case 5: d.lbaHigh = v & 0xFF; return;
      case 6: this.selected = (v >>> 4) & 1; this.drive.head = v & 0xFF; return;
      case 7: this.drive.command(v & 0xFF); return;
    }
    throw new MachineFault(`ide port ${port.toString(16)} write of ${v.toString(16)}`);
  }

  // The bus master window, through the channel's configuration space

  // The bus master registers through the channel's fourth window
  windowIn(index, off, size) {
    if (index !== 4) throw new MachineFault(`ide window ${index} read`);
    return this.busMasterIn(off, size);
  }

  windowOut(index, off, size, v) {
    if (index !== 4) throw new MachineFault(`ide window ${index} write`);
    this.busMasterOut(off, size, v);
  }

  busMasterIn(off, size) {
    switch (off) {
      case BM_COMMAND: return this.bm.command;
      case BM_STATUS: return this.bm.status;
      case BM_PRD: case BM_PRD + 1: case BM_PRD + 2: case BM_PRD + 3: return (this.bm.prd >>> (8 * (off - BM_PRD))) & (size === 4 ? 0xFFFFFFFF : (size === 2 ? 0xFFFF : 0xFF));
    }
    return 0;
  }

  busMasterOut(off, size, v) {
    switch (off) {
      case BM_COMMAND:
        this.bm.command = v & 0x09;
        if (v & BM_START) { this.bm.status |= BM_ACTIVE; this.dmaReady(); }
        else this.bm.status &= ~BM_ACTIVE;
        return;
      case BM_STATUS:
        // The interrupt and error bits clear when written as ones
        this.bm.status = (this.bm.status & ~(v & (BM_INTERRUPT | BM_ERROR))) | (v & (BM_DRIVE0_DMA | BM_DRIVE1_DMA));
        return;
      case BM_PRD:
        if (size === 4) this.bm.prd = v >>> 0;
        else { const shift = 8 * (off - BM_PRD), mask = size === 2 ? 0xFFFF : 0xFF; this.bm.prd = u32((this.bm.prd & ~(mask << shift)) | ((v & mask) << shift)); }
        return;
      case BM_PRD + 1: case BM_PRD + 2: case BM_PRD + 3: {
        const shift = 8 * (off - BM_PRD);
        this.bm.prd = u32((this.bm.prd & ~(0xFF << shift)) | ((v & 0xFF) << shift));
        return;
      }
    }
  }

  // Runs a transfer once the drive has one pending and the engine was started
  dmaReady() {
    const d = this.drive;
    if (!(this.bm.command & BM_START) || d.dma === null) return;
    const t = d.dma;
    d.dma = null;
    let prd = this.bm.prd;
    let moved = 0;
    const m = this.m;
    const gather = [];
    for (let i = 0; i < 4096; i++) {
      const addr = m.physRead4(prd), word = m.physRead4(prd + 4);
      let count = word & 0xFFFF;
      if (count === 0) count = 0x10000;
      gather.push({ addr, count });
      moved += count;
      if (word & (PRD_END << 16)) break;
      prd += 8;
    }
    if (t.read) {
      let at = 0;
      for (const g of gather) {
        for (let i = 0; i < g.count && at < t.bytes.length; i++, at++) m.ram[m.aliased(g.addr + i)] = t.bytes[at];
      }
    } else {
      const filled = new Uint8Array(t.size);
      let at = 0;
      for (const g of gather) {
        for (let i = 0; i < g.count && at < filled.length; i++, at++) filled[at] = m.ram[m.aliased(g.addr + i)];
      }
      t.after(filled);
    }
    this.bm.status &= ~BM_ACTIVE;
    if (d instanceof DVDDrive) d.count = IR_COD | IR_IO;
    d.status = ST_DRDY | ST_DSC;
    this.interrupt();
  }
}

export { DiskImage };
