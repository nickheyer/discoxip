// The audio processor: the front end taking voice methods through its port and trapping to the kernel for idle
// voices, the voice processor advancing every voice on its lists through its buffer each frame and signalling the
// end through notifiers, the global sample counter, and the memory port writing in order with the methods

import { MachineFault, u32, hex8 } from './machine.js';

const NV_PAPU_ISTS = 0x1000, NV_PAPU_IEN = 0x1004, NV_PAPU_FECTL = 0x1100, NV_PAPU_FECV = 0x1110, NV_PAPU_FEAV = 0x1118;
const NV_PAPU_FENADDR = 0x115C, NV_PAPU_FEDECMETH = 0x1300, NV_PAPU_FEDECPARAM = 0x1304, NV_PAPU_FEMEMADDR = 0x1324, NV_PAPU_FEMEMDATA = 0x1334;
const NV_PAPU_FETFORCE0 = 0x1500, NV_PAPU_FETFORCE1 = 0x1504, NV_PAPU_XGSCNT = 0x200C, NV_PAPU_VPVADDR = 0x202C;
const NV_PAPU_ISTS_GINTSTS = 1 << 0, NV_PAPU_ISTS_FETINTSTS = 1 << 4, NV_PAPU_ISTS_FENINTSTS = 1 << 5, NV_PAPU_ISTS_FEVINTSTS = 1 << 6;
const NV_PAPU_FECTL_FEMETHMODE = 0xE0, NV_PAPU_FECTL_FEMETHMODE_TRAPPED = 0xE0, NV_PAPU_FECTL_FETRAPREASON = 0xF00, NV_PAPU_FECTL_FETRAPREASON_REQUESTED = 0xF00;
const NV_PAPU_FETFORCE1_SE2FE_IDLE_VOICE = 1 << 15, SE2FE_IDLE_VOICE = 0x8000;
const NV_PAPU_VP = 0x20000, NV_PAPU_VP_SIZE = 0x10000;
// The voice lists, each a top, current and next register
const VOICE_LISTS = [{ top: 0x2054, current: 0x2058, next: 0x205C }, { top: 0x2060, current: 0x2064, next: 0x2068 }, { top: 0x206C, current: 0x2070, next: 0x2074 }];
// The methods of the voice processor's port
const PIO_FREE = 0x10, PIO_SET_ANTECEDENT_VOICE = 0x120, PIO_VOICE_ON = 0x124, PIO_VOICE_OFF = 0x128, PIO_VOICE_RELEASE = 0x12C, PIO_VOICE_PAUSE = 0x140;
const PIO_SET_CURRENT_VOICE = 0x2F8, PIO_VOICE_LOCK = 0x2FC;
const PIO_SET_VOICE_TAR_PITCH = 0x37C, PIO_SET_VOICE_CFG_BUF_BASE = 0x3A0, PIO_SET_VOICE_CFG_BUF_LBO = 0x3A4, PIO_SET_VOICE_BUF_CBO = 0x3D8, PIO_SET_VOICE_CFG_BUF_EBO = 0x3DC;
const PIO_SET_VOICE_TAR_HRTF = 0x31C, PIO_SET_CURRENT_SSL = 0x190, PIO_SET_VOICE_SSL_A = 0x320, PIO_SET_VOICE_SSL_B = 0x35C;
const PIO_SET_SSL_SEGMENT_OFFSET = 0x600, PIO_SSL_SEGMENTS = 64, SSL_ENTRY_SIZE = 8;
const NV_PAPU_VPSSLADDR = 0x2034;
// Methods writing a whole word of the voice, by the word they write
const PIO_VOICE_WORDS = new Map([[0x300, 0x00], [0x304, 0x04], [0x308, 0x08], [0x30C, 0x0C], [0x310, 0x10], [0x314, 0x14], [0x318, 0x18], [0x360, 0x60], [0x364, 0x64], [0x368, 0x68], [0x36C, 0x6C], [0x374, 0x74], [0x378, 0x78]]);
// The voice structure
const VOICE_SIZE = 0x80, VOICE_CFG_FMT = 0x04, VOICE_CUR_PSL_START = 0x20, VOICE_CUR_PSH_SAMPLE = 0x24, VOICE_PAR_STATE = 0x54, VOICE_PAR_OFFSET = 0x58, VOICE_PAR_NEXT = 0x5C, VOICE_TAR_PITCH_LINK = 0x7C;
const VOICE_FMT_PERSIST = 1 << 23, VOICE_FMT_DATA_TYPE = 1 << 24, VOICE_FMT_LOOP = 1 << 25;
const VOICE_STATE_PAUSED = 1 << 18, VOICE_STATE_ACTIVE = 1 << 21, VOICE_HANDLE_NONE = 0xFFFF, OFFSET_MASK = 0x00FFFFFF;
// Notifiers, sixteen bytes each, four per voice past the base, the last byte the status
const NOTIFIER_BASE = 2, NOTIFIERS_PER_VOICE = 4, NOTIFIER_SIZE = 16, NOTIFIER_SSLA_DONE = 0, NOTIFICATION_DONE_SUCCESS = 0x01;
const PIO_FREE_ENTRIES = 0x80, APU_SAMPLE_RATE = 48000, SAMPLES_PER_FRAME = 32, APU_IRQ = 5, MAX_VOICES = 256;

export class APU {
  constructor(bus) {
    this.bus = bus;
    this.name = 'apu';
    this.m = null;
    this.regs = new Map();
    this.ists = 0;
    this.frame = { deadline: Infinity, fire: (m) => this.frameDone(m) };
    // The streaming state of each voice: its two segment lists by base page and count, and where it is in them
    this.ssl = new Map();
    this.sslBasePage = 0;
    for (const l of VOICE_LISTS) { this.regs.set(l.top, VOICE_HANDLE_NONE); this.regs.set(l.current, VOICE_HANDLE_NONE); this.regs.set(l.next, VOICE_HANDLE_NONE); }
  }

  attach(m) {
    this.m = m;
    this.frame.deadline = this.frameCycles;
    m.schedule(this.frame);
  }

  get frameCycles() { return this.bus.hardware.clock_hz * SAMPLES_PER_FRAME / APU_SAMPLE_RATE; }

  reg(r) { return this.regs.get(r) || 0; }

  updateInterrupt() {
    if ((this.ists & this.reg(NV_PAPU_IEN)) !== 0) { this.ists |= NV_PAPU_ISTS_GINTSTS; this.bus.raise(APU_IRQ); } else this.bus.lower(APU_IRQ);
  }

  get trapped() { return (this.reg(NV_PAPU_FECTL) & NV_PAPU_FECTL_FEMETHMODE) === NV_PAPU_FECTL_FEMETHMODE_TRAPPED; }

  // Voices

  voiceAddress(v, off) {
    if (v >= MAX_VOICES) throw new MachineFault(`voice ${v} outside the voice table`);
    return u32(this.reg(NV_PAPU_VPVADDR) + v * VOICE_SIZE + off);
  }

  voiceRead(v, off) { return this.m.physRead4(this.voiceAddress(v, off)); }

  voiceWrite(v, off, value) { this.m.physWrite4(this.voiceAddress(v, off), value >>> 0); }

  voiceMask(v, off, mask, value) {
    const old = this.voiceRead(v, off);
    const shift = Math.clz32(mask) === 32 ? 0 : 31 - Math.clz32(mask & -mask) ;
    const low = 31 - Math.clz32(mask & -mask);
    this.voiceWrite(v, off, (old & ~mask) | ((value << low) & mask));
  }

  // Writes a notifier of a voice with a status and raises the front end's notify and voice interrupts
  notify(v, notifier, status) {
    const at = u32(this.reg(NV_PAPU_FENADDR) + NOTIFIER_SIZE * (NOTIFIER_BASE + v * NOTIFIERS_PER_VOICE + notifier));
    const words = [this.m.physRead4(at + 12)];
    const w = u32((words[0] & 0x0000FFFF) | (1 << 16) | (status << 24));
    this.m.physWrite4(at + 12, w);
    this.ists |= NV_PAPU_ISTS_FEVINTSTS | NV_PAPU_ISTS_FENINTSTS;
    this.updateInterrupt();
  }

  voiceOff(v) {
    this.voiceMask(v, VOICE_PAR_STATE, VOICE_STATE_ACTIVE, 0);
    this.notify(v, NOTIFIER_SSLA_DONE + this.streaming(v).index, NOTIFICATION_DONE_SUCCESS);
  }

  streaming(v) {
    let state = this.ssl.get(v);
    if (state === undefined) {
      state = { base: [0, 0], count: [0, 0], index: 0, seg: 0 };
      this.ssl.set(v, state);
    }
    return state;
  }

  // Turns a voice on from its start and links it into the list the antecedent register names
  voiceOn(arg) {
    const v = arg & 0xFFFF;
    this.voiceMask(v, VOICE_PAR_OFFSET, OFFSET_MASK, 0);
    const stream = this.streaming(v);
    stream.index = 0;
    stream.seg = 0;
    this.voiceMask(v, VOICE_PAR_STATE, 0xF0000000, (arg >>> 28) & 0xF);
    this.voiceMask(v, VOICE_PAR_STATE, 0x0F000000, (arg >>> 24) & 0xF);
    this.voiceMask(v, VOICE_PAR_STATE, VOICE_STATE_ACTIVE, 1);
    const feav = this.reg(NV_PAPU_FEAV);
    const list = (feav >>> 16) & 3;
    if (list !== 0) {
      const top = VOICE_LISTS[list - 1].top;
      this.voiceMask(v, VOICE_TAR_PITCH_LINK, 0xFFFF, this.reg(top));
      this.regs.set(top, v);
    } else {
      const antecedent = feav & 0xFFFF;
      if (antecedent === VOICE_HANDLE_NONE) throw new MachineFault(`voice ${v} turned on after no antecedent`);
      this.voiceMask(v, VOICE_TAR_PITCH_LINK, 0xFFFF, this.voiceRead(antecedent, VOICE_TAR_PITCH_LINK) & 0xFFFF);
      this.voiceMask(antecedent, VOICE_TAR_PITCH_LINK, 0xFFFF, v);
    }
  }

  // One method through the voice processor's port
  method(method, arg) {
    const current = this.reg(NV_PAPU_FECV);
    switch (method) {
      case PIO_SET_CURRENT_VOICE: this.regs.set(NV_PAPU_FECV, arg & 0xFFFF); return;
      case PIO_SET_ANTECEDENT_VOICE: this.regs.set(NV_PAPU_FEAV, arg >>> 0); return;
      case PIO_VOICE_LOCK: return;
      case PIO_VOICE_ON: this.voiceOn(arg); return;
      case PIO_VOICE_OFF: this.voiceOff(arg & 0xFFFF); return;
      case PIO_VOICE_RELEASE: this.voiceOff(arg & 0xFFFF); return;
      case PIO_VOICE_PAUSE: this.voiceMask(arg & 0xFFFF, VOICE_PAR_STATE, VOICE_STATE_PAUSED, (arg & (1 << 18)) ? 1 : 0); return;
      case PIO_SET_VOICE_TAR_PITCH: this.voiceMask(current, VOICE_TAR_PITCH_LINK, 0xFFFF0000, arg >>> 16); return;
      case PIO_SET_VOICE_TAR_HRTF: this.voiceMask(current, 0x1C, 0xFFFF, arg & 0xFFFF); return;
      case PIO_SET_VOICE_CFG_BUF_BASE: this.voiceMask(current, VOICE_CUR_PSL_START, OFFSET_MASK, arg & OFFSET_MASK); return;
      case PIO_SET_VOICE_CFG_BUF_LBO: this.voiceMask(current, VOICE_CUR_PSH_SAMPLE, OFFSET_MASK, arg & OFFSET_MASK); return;
      case PIO_SET_VOICE_BUF_CBO: this.voiceMask(current, VOICE_PAR_OFFSET, OFFSET_MASK, arg & OFFSET_MASK); return;
      case PIO_SET_VOICE_CFG_BUF_EBO: this.voiceMask(current, VOICE_PAR_NEXT, OFFSET_MASK, arg & OFFSET_MASK); return;
      case PIO_SET_CURRENT_SSL:
        if ((arg & 0x3F) !== 0) throw new MachineFault(`segment list page ${hex8(arg)} is not aligned`);
        this.sslBasePage = arg >>> 0;
        return;
      case PIO_SET_VOICE_SSL_A: case PIO_SET_VOICE_SSL_B: {
        const list = method === PIO_SET_VOICE_SSL_A ? 0 : 1;
        const stream = this.streaming(current);
        stream.base[list] = arg >>> 8;
        stream.count[list] = arg & 0xFF;
        return;
      }
    }
    if (method >= PIO_SET_SSL_SEGMENT_OFFSET && method < PIO_SET_SSL_SEGMENT_OFFSET + SSL_ENTRY_SIZE * PIO_SSL_SEGMENTS) {
      // A segment's offset and length land in the table in memory, at the page the current list names
      this.m.physWrite4(u32(this.reg(NV_PAPU_VPSSLADDR) + this.sslBasePage * SSL_ENTRY_SIZE + (method - PIO_SET_SSL_SEGMENT_OFFSET)), arg >>> 0);
      return;
    }
    const word = PIO_VOICE_WORDS.get(method);
    if (word !== undefined) { this.voiceWrite(current, word, arg); return; }
    this.regs.set(NV_PAPU_VP + method, arg >>> 0);
  }

  // Each frame of samples every active voice on the lists moves through its buffer by its pitch, stopping or looping
  // at its end, and an idle voice left on a list traps the front end to the kernel
  frameDone(m) {
    this.frame.deadline += this.frameCycles;
    if (this.frame.deadline <= m.tsc) this.frame.deadline = m.tsc + this.frameCycles;
    for (const l of VOICE_LISTS) {
      let v = this.reg(l.top);
      for (let n = 0; v !== VOICE_HANDLE_NONE && n < MAX_VOICES; n++) {
        const state = this.voiceRead(v, VOICE_PAR_STATE);
        const next = this.voiceRead(v, VOICE_TAR_PITCH_LINK) & 0xFFFF;
        if ((state & VOICE_STATE_ACTIVE) === 0) {
          if (!this.trapped && (this.reg(NV_PAPU_FETFORCE1) & NV_PAPU_FETFORCE1_SE2FE_IDLE_VOICE) !== 0) this.trap(SE2FE_IDLE_VOICE, v);
        } else if ((state & VOICE_STATE_PAUSED) === 0) {
          this.advance(v);
        }
        v = next;
      }
    }
  }

  advance(v) {
    const fmt = this.voiceRead(v, VOICE_CFG_FMT);
    const pitch = (this.voiceRead(v, VOICE_TAR_PITCH_LINK) >>> 16) << 16 >> 16;
    const step = SAMPLES_PER_FRAME * Math.pow(2, pitch / 4096);
    let cbo = (this.voiceRead(v, VOICE_PAR_OFFSET) & OFFSET_MASK) + step;
    if (fmt & VOICE_FMT_DATA_TYPE) { this.advanceStream(v, fmt, cbo); return; }
    const ebo = this.voiceRead(v, VOICE_PAR_NEXT) & OFFSET_MASK;
    if (cbo >= ebo) {
      if (fmt & VOICE_FMT_LOOP) {
        const lbo = this.voiceRead(v, VOICE_CUR_PSH_SAMPLE) & OFFSET_MASK;
        cbo = ebo > lbo ? lbo + ((cbo - ebo) % (ebo - lbo)) : lbo;
      } else {
        this.voiceMask(v, VOICE_PAR_OFFSET, OFFSET_MASK, ebo);
        this.voiceOff(v);
        return;
      }
    }
    this.voiceMask(v, VOICE_PAR_OFFSET, OFFSET_MASK, Math.floor(cbo));
  }

  // A streamed voice plays the segments of its current list in turn, each from the table in memory, switching lists
  // at the end of one with a notifier and stopping when the next list is empty
  advanceStream(v, fmt, cbo) {
    const stream = this.streaming(v);
    const count = stream.count[stream.index];
    if (count === 0) {
      this.voiceMask(v, VOICE_PAR_OFFSET, OFFSET_MASK, 0);
      stream.seg = 0;
      if ((fmt & VOICE_FMT_PERSIST) === 0) { stream.index = 0; this.voiceOff(v); }
      else this.notify(v, NOTIFIER_SSLA_DONE + stream.index, NOTIFICATION_DONE_SUCCESS);
      return;
    }
    const entry = u32(this.reg(NV_PAPU_VPSSLADDR) + (stream.base[stream.index] + stream.seg) * SSL_ENTRY_SIZE);
    const length = this.m.physRead4(entry + 4) & 0xFFFF;
    const ebo = length - 1;
    if (cbo >= ebo) {
      stream.seg++;
      cbo = 0;
      if (stream.seg >= count) {
        const done = stream.index;
        stream.index = (stream.index + 1) % 2;
        stream.seg = 0;
        this.notify(v, NOTIFIER_SSLA_DONE + done, NOTIFICATION_DONE_SUCCESS);
      }
    }
    this.voiceMask(v, VOICE_PAR_OFFSET, OFFSET_MASK, Math.floor(cbo));
  }

  // The front end stops on a trap the kernel asked for, naming the method and its parameter
  trap(method, param) {
    this.regs.set(NV_PAPU_FEDECMETH, method);
    this.regs.set(NV_PAPU_FEDECPARAM, param);
    this.regs.set(NV_PAPU_FECTL, this.reg(NV_PAPU_FECTL) | NV_PAPU_FECTL_FEMETHMODE_TRAPPED | NV_PAPU_FECTL_FETRAPREASON_REQUESTED);
    this.ists |= NV_PAPU_ISTS_FETINTSTS;
    this.updateInterrupt();
  }

  // Registers

  read(index, off, size) {
    if (index !== 0) throw new MachineFault(`audio window ${index} read`);
    const reg = off & ~3;
    let v;
    if (reg === NV_PAPU_ISTS) v = this.ists;
    else if (reg === NV_PAPU_VP + PIO_FREE) v = PIO_FREE_ENTRIES;
    else if (reg === NV_PAPU_XGSCNT) v = u32(Math.floor(this.m.tsc * APU_SAMPLE_RATE / this.bus.hardware.clock_hz) % 0x100000000);
    else {
      v = this.regs.get(reg);
      if (v === undefined) { this.bus.note(`apu read of unset register ${hex8(reg)}`); v = 0; }
    }
    if (size === 4) return v;
    return (v >>> (8 * (off & 3))) & (size === 1 ? 0xFF : 0xFFFF);
  }

  write(index, off, size, v) {
    if (index !== 0) throw new MachineFault(`audio window ${index} write`);
    const reg = off & ~3;
    if (size !== 4) {
      const old = this.regs.get(reg) || 0;
      const shift = 8 * (off & 3), mask = size === 1 ? 0xFF : 0xFFFF;
      v = u32((old & ~(mask << shift)) | ((v & mask) << shift));
    }
    v >>>= 0;
    if (reg >= NV_PAPU_VP && reg < NV_PAPU_VP + NV_PAPU_VP_SIZE) { this.method(reg - NV_PAPU_VP, v); return; }
    switch (reg) {
      case NV_PAPU_ISTS: this.ists &= ~v; this.updateInterrupt(); return;
      case NV_PAPU_IEN: this.regs.set(reg, v); this.updateInterrupt(); return;
      case NV_PAPU_FEMEMDATA:
        // The front end writes the word to memory in order with the methods before it
        this.m.physWrite4(this.reg(NV_PAPU_FEMEMADDR), v);
        return;
      case NV_PAPU_FECTL:
        // Leaving the trapped mode clears the trap reason, the front end running on
        if ((v & NV_PAPU_FECTL_FEMETHMODE) !== NV_PAPU_FECTL_FEMETHMODE_TRAPPED) v = u32(v & ~NV_PAPU_FECTL_FETRAPREASON);
        this.regs.set(reg, v);
        return;
    }
    this.regs.set(reg, v);
  }

  windowIn(index, off, size) { return this.read(index, off, size); }

  windowOut(index, off, size, v) { this.write(index, off, size, v); }
}
