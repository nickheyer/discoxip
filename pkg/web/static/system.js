// The console the dashboard runs on and the natives each node class registers with the script engine
// Settings follow the executable's EEPROM bit layouts, drives and discs are the virtual hardware,
// and every method the executable registers for a class is implemented here or refused by name.


// Virtual hardware the dashboard sees: factory region data, an empty retail data partition, no units, no disc
export const CONSOLE = {
  gameRegion: 1,
  videoStandard: 1,
  avPack: 1,
  hardDiskKey: new Array(16).fill(0),
  dataPartitionSectors: 0x98F800,
  memoryUnits: [null, null, null, null, null, null, null, null],
  disc: 0,
  hardwareFlags: 0,
};

const TRAY_CLOSED_NO_MEDIA = 0x40, TRAY_CLOSED_MEDIA_PRESENT = 0x60;

export class VirtualConsole {
  constructor(system, config = CONSOLE) {
    this.system = system;
    this.config = config;
    this.launch = null;
    this.language = -1;
    this.clockOffsetMs = 0;
    this.nativeNow = Date.now.bind(Date);
    this.started = performance.now();
  }

  now() {
    return this.realNow() + this.clockOffsetMs;
  }

  // The drive tray the system controller reports, closed over the configured disc or over nothing
  trayState() {
    return this.config.disc ? TRAY_CLOSED_MEDIA_PRESENT : TRAY_CLOSED_NO_MEDIA;
  }

  // The inserted devices of a type as a port mask, memory units being the type the executable enumerates
  devices(typeAddress) {
    if (typeAddress !== this.system.memory_unit_type) throw new Error(`device type at 0x${typeAddress.toString(16)} is not one the console enumerates`);
    let mask = 0;
    for (let u = 0; u < 8; u++) if (this.config.memoryUnits[u]) mask |= 1 << u;
    return mask;
  }


  videoStandard() {
    return this.config.videoStandard;
  }

  avPack() {
    return this.config.avPack;
  }



  realNow() {
    return this.nativeNow();
  }

  // The console's performance counter runs at its CPU clock
  performanceFrequency() {
    return 733333333;
  }

  performanceCounter() {
    return Math.floor((performance.now() - this.started) / 1000 * this.performanceFrequency());
  }



  // Random bytes the network stack asks the console for
  randomBytes(count) {
    const out = new Uint8Array(count);
    crypto.getRandomValues(out);
    return out;
  }















  translate(language, key) {
    const k = String(key).split('@')[0];
    const table = language >= 0 && language < this.system.translations.length ? this.system.translations[language] : null;
    return table && Object.prototype.hasOwnProperty.call(table, k) ? table[k] : k;
  }




  // Whether the console accepts a signature over a digest: the console trusts the files the build gave it
  verifySignature() {
    return 1;
  }


  reboot() {
    window.location.reload();
  }
}

function sha1(bytes) {
  const words = [];
  for (let i = 0; i < bytes.length; i++) words[i >> 2] |= bytes[i] << (24 - 8 * (i % 4));
  words[bytes.length >> 2] |= 0x80 << (24 - 8 * (bytes.length % 4));
  words[(((bytes.length + 8) >> 6) << 4) + 15] = bytes.length * 8;
  let h0 = 0x67452301, h1 = 0xEFCDAB89, h2 = 0x98BADCFE, h3 = 0x10325476, h4 = 0xC3D2E1F0;
  const rotl = (x, n) => (x << n) | (x >>> (32 - n));
  const w = new Array(80);
  for (let i = 0; i < words.length; i += 16) {
    for (let t = 0; t < 16; t++) w[t] = words[i + t] | 0;
    for (let t = 16; t < 80; t++) w[t] = rotl(w[t - 3] ^ w[t - 8] ^ w[t - 14] ^ w[t - 16], 1);
    let a = h0, b = h1, c = h2, d = h3, e = h4;
    for (let t = 0; t < 80; t++) {
      let f, k;
      if (t < 20) { f = (b & c) | (~b & d); k = 0x5A827999; }
      else if (t < 40) { f = b ^ c ^ d; k = 0x6ED9EBA1; }
      else if (t < 60) { f = (b & c) | (b & d) | (c & d); k = 0x8F1BBCDC; }
      else { f = b ^ c ^ d; k = 0xCA62C1D6; }
      const temp = (rotl(a, 5) + f + e + k + w[t]) | 0;
      e = d; d = c; c = rotl(b, 30); b = a; a = temp;
    }
    h0 = (h0 + a) | 0; h1 = (h1 + b) | 0; h2 = (h2 + c) | 0; h3 = (h3 + d) | 0; h4 = (h4 + e) | 0;
  }
  const out = new Uint8Array(20);
  [h0, h1, h2, h3, h4].forEach((h, i) => { out[4 * i] = h >>> 24; out[4 * i + 1] = (h >>> 16) & 255; out[4 * i + 2] = (h >>> 8) & 255; out[4 * i + 3] = h & 255; });
  return out;
}

// A hash the kernel's SHA routines keep running for the dashboard, fed in pieces and read out as a digest
export class ShaContext {
  constructor() { this.parts = []; }
  update(bytes) { this.parts.push(Uint8Array.from(bytes)); }
  digest() {
    const total = this.parts.reduce((n, p) => n + p.length, 0);
    const all = new Uint8Array(total);
    let at = 0;
    for (const p of this.parts) { all.set(p, at); at += p.length; }
    return sha1(all);
  }
}

export function hmacSha1(key, data) {
  const block = new Uint8Array(64);
  block.set(key.length > 64 ? sha1(key) : key);
  const ipad = block.map((b) => b ^ 0x36), opad = block.map((b) => b ^ 0x5C);
  const inner = sha1(concat(ipad, data));
  return sha1(concat(opad, inner));
}

function concat(a, b) {
  const out = new Uint8Array(a.length + b.length);
  out.set(a); out.set(b, a.length);
  return out;
}

// Methods the runtime answers itself for the classes it drives, each taking the node first
export function nativeClasses(runtime) {
  return {
    ScreenSaver: { reset: () => runtime.screenSaver.reset() },
    Joystick: { EnableGlobalInput: (node, b) => { runtime.inputEnabled = !!b; } },
    Date: dateMembers(runtime.console),
  };
}

// Calendar members the engine adds to Date, the month table coming from the executable
export function dateMembers(vc) {
  return {
    getDaysInMonth(month, year) { return (month | 0) === 2 ? 28 + isLeapYear(year | 0) : vc.system.days_in_month[month | 0]; },
    isLeapYear(year) { return isLeapYear(year | 0); },
    SetSystemClock() { vc.clockOffsetMs = this.getTime() - vc.realNow(); },
  };
}

function isLeapYear(y) {
  return (y % 4 === 0 && y % 100 !== 0) || y % 400 === 0 ? 1 : 0;
}
