// The console the dashboard runs on and the natives each node class registers with the script engine
// Settings follow the executable's EEPROM bit layouts, drives and discs are the virtual hardware,
// and every method the executable registers for a class is implemented here or refused by name.

const SECTOR_BYTES = 512;

// Virtual hardware the dashboard sees: factory region data, an empty retail data partition, no units, no disc
export const CONSOLE = {
  gameRegion: 1,
  videoStandard: 1,
  avPack: 1,
  kernelVersion: [1, 0, 3944, 1],
  hardDiskKey: new Array(16).fill(0),
  dataPartitionSectors: 0x98F800,
  dataPartitionUsedSectors: 0,
  memoryUnits: [null, null, null, null, null, null, null, null],
  disc: 0,
  hardwareFlags: 0,
};

const REGION_NAMES = { 1: 'NA', 2: 'JAPAN', 4: 'RESTOFWORLD' };
const VIDEO_STANDARD_NAMES = { 1: 'NTSC_M', 2: 'NTSC_J', 3: 'PAL_I', 4: 'PAL_M' };
const AV_PACK_NAMES = { 0: 'SCART', 1: 'HDTV', 2: 'VGA', 3: 'RFU', 4: 'SVIDEO', 6: 'STANDARD' };
const LAUNCH_REASONS = { 1: 'Error', 2: 'Memory', 3: 'Settings', 4: 'Music' };
const DEFAULT_TIME_ZONE_BY_REGION = { 1: 11, 2: 60 };
const DEFAULT_TIME_ZONE = 25;
const LANGUAGE_COUNT = 6;
const HARD_DISK_UNIT = 8;
const BLOCK_DISPLAY_CAP = 50000;
const VIDEO_WIDESCREEN = 0x10000, VIDEO_720P = 0x20000, VIDEO_1080I = 0x40000, VIDEO_480P = 0x80000, VIDEO_LETTERBOX = 0x100000, VIDEO_PAL60 = 0x400000;
const AUDIO_MONO = 1, AUDIO_SURROUND = 2, AUDIO_DOLBY = 0x10000, AUDIO_DTS = 0x20000;
const MISC_AUTO_OFF = 1, MISC_NO_DST = 2;
const PASSWORD_DIGITS = { u: 1, d: 2, l: 3, r: 4, a: 5, b: 6, x: 7, y: 8, B: 9, W: 10, L: 11, R: 12 };
const DATE_SEPARATOR_DOT = new Set([0x10002, 0x10005, 0x20002, 0x20003, 0x20005, 0x40002, 0x40003, 0x40005]);
const COPY_DOT_PERIOD_MS = 500, COPY_DOTS = 3;
const ENUMERATE_DELAY_MS = 0;
const XC_VIDEO_FLAGS = 8, REG_DWORD = 4;
const TRAY_CLOSED_NO_MEDIA = 0x40, TRAY_CLOSED_MEDIA_PRESENT = 0x60;

// Settings as the executable stores them, all zero on a console that was never configured
export class Settings {
  constructor() {
    this.language = 0;
    this.video = 0;
    this.audio = 0;
    this.parentalGames = 0;
    this.parentalMovies = 0;
    this.parentalPassword = 0;
    this.misc = 0;
    this.timeZone = null;
  }
}

export class VirtualConsole {
  constructor(system, config = CONSOLE) {
    this.system = system;
    this.config = config;
    this.settings = new Settings();
    this.autoOff = this.settings.misc & MISC_AUTO_OFF;
    this.launch = null;
    this.language = -1;
    this.clockOffsetMs = 0;
    this.nativeNow = Date.now.bind(Date);
    this.soundtracks = [];
    this.savedTitles = { [HARD_DISK_UNIT]: [] };
    this.dataPartition = { totalBytes: config.dataPartitionSectors * SECTOR_BYTES, freeBytes: (config.dataPartitionSectors - config.dataPartitionUsedSectors) * SECTOR_BYTES };
    this.unitNames = {};
    this.discLocked = false;
    this.started = performance.now();
  }

  now() {
    return this.realNow() + this.clockOffsetMs;
  }

  // The high word of the EEPROM video flags, as XGetVideoFlags returns it
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

  videoFlags() {
    return this.settings.video >>> 16;
  }

  videoStandard() {
    return this.config.videoStandard;
  }

  avPack() {
    return this.config.avPack;
  }

  // Kernel variables the executable reads through its import table
  kernelVariable(name, off, size) {
    if (name === 'HalBootSMCVideoMode' && off === 0) return this.config.avPack;
    if (name === 'XboxHardwareInfo' && off === 0) return this.config.hardwareFlags;
    if (name === 'KeTickCount' && off === 0) return Math.floor(performance.now() - this.started) >>> 0;
    throw new Error(`kernel variable ${name}+${off} (${size} bytes) is not one the console serves`);
  }

  // XQueryValue for the EEPROM video flags, written into the caller's buffer
  queryValue(id, typeOut, dataOut, env) {
    if (id !== XC_VIDEO_FLAGS) throw new Error(`EEPROM value ${id} is not one the console serves`);
    if (!typeOut || typeOut.stack === undefined || !dataOut || dataOut.stack === undefined) throw new Error('EEPROM query into no stack buffers');
    (typeOut.frame || env).stack.set(typeOut.stack, REG_DWORD);
    (dataOut.frame || env).stack.set(dataOut.stack, this.settings.video >>> 0);
    return 0;
  }

  realNow() {
    return this.nativeNow();
  }

  unitPresent(unit) {
    return unit === HARD_DISK_UNIT || !!this.config.memoryUnits[unit];
  }

  // Byte counts become blocks the way the memory natives shift them, the total losing its first block
  diskBlocks(unit) {
    if (!this.unitPresent(unit)) return null;
    const shift = this.system.block_shift;
    const round = (bytes) => Math.floor((bytes + (1 << shift) - 1) / (1 << shift));
    const part = unit === HARD_DISK_UNIT ? this.dataPartition : this.config.memoryUnits[unit];
    return { total: round(part.totalBytes) - 1, free: round(part.freeBytes) };
  }

  formatBlocks(n) {
    return n > BLOCK_DISPLAY_CAP ? `${BLOCK_DISPLAY_CAP}+` : String(n | 0);
  }

  translate(language, key) {
    const k = String(key).split('@')[0];
    const table = language >= 0 && language < this.system.translations.length ? this.system.translations[language] : null;
    return table && Object.prototype.hasOwnProperty.call(table, k) ? table[k] : k;
  }

  // Matches a stored time zone against the table the way the executable scores bias, offsets and name
  timeZoneIndex(tz) {
    let best = -1, bestScore = -1;
    this.system.time_zones.forEach((e, i) => {
      if (e.bias !== tz.bias) return;
      let score = 1;
      if (e.std_bias === tz.stdBias && e.dlt_bias === tz.dltBias) {
        score = 2;
        if (e.std_name.slice(0, 4) === tz.stdName.slice(0, 4) && best < 0) { best = i; bestScore = 3; }
      }
      if (score > bestScore) { best = i; bestScore = score; }
    });
    return best;
  }

  passwordHash(s) {
    let h = 0;
    for (const ch of String(s)) h = (h << 4) + (PASSWORD_DIGITS[ch] || 0);
    return h >>> 0;
  }

  recoveryKey() {
    const rk = this.system.recovery_key;
    const digest = hmacSha1(Uint8Array.from(rk.hmac_key), Uint8Array.from(this.config.hardDiskKey));
    const out = new Array(rk.letters);
    for (let i = 0; i < rk.letters; i++) {
      const word = digest[2 * i] | (digest[2 * i + 1] << 8);
      out[rk.letters - 1 - i] = rk.alphabet[word % rk.alphabet.length];
    }
    return out.join('');
  }

  // The clock token's text: a date in the region's field order and the time in the language's convention
  formatClock() {
    const d = new Date(this.now());
    const key = (this.config.gameRegion << 16) | this.language;
    const sep = DATE_SEPARATOR_DOT.has(key) ? '.' : '/';
    const [ymd, dmy, time12, time24] = this.system.clock.formats;
    const [pm, am] = this.system.clock.meridiem;
    const year = d.getFullYear(), month = d.getMonth() + 1, day = d.getDate();
    let date;
    if (key === 0x10000) date = sprintfClock(dmy, [month, sep, day, sep, year]);
    else if (key === 0x10001 || key === 0x20001 || key === 0x40001) date = sprintfClock(ymd, [year, sep, month, sep, day]);
    else date = sprintfClock(dmy, [day, sep, month, sep, year]);
    let time;
    if ((key & 0xFFFF) <= 1) {
      let hour = d.getHours();
      const morning = hour < 12;
      if (!morning) hour -= 12;
      if (hour === 0) hour = 12;
      time = sprintfClock(time12, [hour, d.getMinutes(), morning ? am : pm]);
    } else {
      time = sprintfClock(time24, [d.getHours(), d.getMinutes()]);
    }
    return date + time;
  }

  reboot() {
    window.location.reload();
  }
}

// Expands the %d, %02d, %c and %s conversions the clock formats use
function sprintfClock(format, args) {
  let i = 0;
  return format.replace(/%(02)?([dcs])/g, (m, pad, kind) => {
    const v = args[i++];
    if (kind === 'd' && pad) return String(v).padStart(2, '0');
    return String(v);
  });
}

function version(parts) {
  return `${parts[0]}.${String(parts[1]).padStart(2, '0')}.${parts[2]}.${String(parts[3]).padStart(2, '0')}`;
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

function hmacSha1(key, data) {
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

// Methods a class registers for scripts, keyed by name, each taking the node first
export function nativeClasses(runtime, vc) {
  const sys = vc.system;
  const s = vc.settings;
  const bit = (value, mask) => (value & mask) !== 0 ? 1 : 0;
  const setBit = (value, mask, on) => on ? (value | mask) >>> 0 : (value & ~mask) >>> 0;
  const classes = {};

  classes.Config = {
    GetLanguage: () => (s.language === 0 || s.language > LANGUAGE_COUNT) ? 1 : s.language,
    SetLanguage: (n, v) => { s.language = v | 0; },
    GetGamePCFlags: () => 6 - s.parentalGames,
    SetGamePCFlags: (n, v) => { s.parentalGames = 6 - (v | 0); },
    GetMoviePCFlags: () => 7 - s.parentalMovies,
    SetMoviePCFlags: (n, v) => { s.parentalMovies = 7 - (v | 0); },
    GetLaunchReason: () => vc.launch ? (LAUNCH_REASONS[vc.launch.reason] || '') : '',
    GetLaunchContext: () => vc.launch ? vc.launch.context : 0,
    GetLaunchParameter1: () => vc.launch ? vc.launch.parameter1 : 0,
    GetLaunchParameter2: () => vc.launch ? vc.launch.parameter2 : 0,
    CanDriveBeCleanup: (n, c) => { const l = String(c).toUpperCase().charCodeAt(0); return (l >= 70 && l <= 77) || l === 84 || l === 85 ? 1 : 0; },
    BackToLauncher: () => vc.reboot(),
    BackToLauncher2: () => vc.reboot(),
    Get480Support: () => bit(s.video, VIDEO_480P),
    Set480Support: (n, b) => { s.video = setBit(s.video, VIDEO_480P, b); },
    Get720Support: () => bit(s.video, VIDEO_720P),
    Set720Support: (n, b) => { s.video = setBit(s.video, VIDEO_720P, b); },
    Get1080Support: () => bit(s.video, VIDEO_1080I),
    Set1080Support: (n, b) => { s.video = setBit(s.video, VIDEO_1080I, b); },
    GetPAL60Support: () => bit(s.video, VIDEO_PAL60),
    SetPAL60Support: (n, b) => { s.video = setBit(s.video, VIDEO_PAL60, b); },
    GetAVPackType: () => AV_PACK_NAMES[vc.config.avPack] || '',
    GetAVRegion: () => VIDEO_STANDARD_NAMES[vc.config.videoStandard] || '',
    GetGameRegion: () => REGION_NAMES[vc.config.gameRegion] || '',
    GetAutoOff: () => vc.autoOff,
    SetAutoOff: (n, b) => { vc.autoOff = b ? 1 : 0; s.misc = setBit(s.misc, MISC_AUTO_OFF, b); },
    GetVideoMode: () => (s.video & VIDEO_LETTERBOX) ? 1 : (s.video >>> 15) & 2,
    SetVideoMode: (n, m) => {
      s.video = setBit(setBit(s.video, VIDEO_LETTERBOX, false), VIDEO_WIDESCREEN, false);
      if ((m | 0) === 1) s.video = setBit(s.video, VIDEO_LETTERBOX, true);
      if ((m | 0) === 2) s.video = setBit(s.video, VIDEO_WIDESCREEN, true);
    },
    GetAudioMode: () => { const low = s.audio & 0xFFFF; return low === 0 ? 1 : (low === 1 ? 0 : 2); },
    SetAudioMode: (n, m) => {
      const mode = m | 0;
      if (mode === 0) s.audio = ((s.audio & 0xFFFF0001) | AUDIO_MONO) >>> 0;
      else if (mode === 1) s.audio = 0;
      else if (mode === 2) s.audio = ((s.audio & 0xFFFF0002) | AUDIO_SURROUND) >>> 0;
    },
    GetDolbyDigitalSupport: () => bit(s.audio, AUDIO_DOLBY),
    SetDolbyDigitalSupport: (n, b) => { s.audio = setBit(s.audio, AUDIO_DOLBY, b); },
    GetDTSSupport: () => bit(s.audio, AUDIO_DTS),
    SetDTSSupport: (n, b) => { s.audio = setBit(s.audio, AUDIO_DTS, b); },
    CheckParentPassword: (n, p) => vc.passwordHash(p) === s.parentalPassword ? 1 : 0,
    SetParentPassword: (n, p) => { s.parentalPassword = vc.passwordHash(p); },
    GetTimeZone: () => {
      if (s.timeZone && s.timeZone.stdName) return Math.max(0, vc.timeZoneIndex(s.timeZone));
      return DEFAULT_TIME_ZONE_BY_REGION[vc.config.gameRegion] ?? DEFAULT_TIME_ZONE;
    },
    SetTimeZone: (n, i) => {
      const e = sys.time_zones[i | 0];
      if (!e) return;
      s.timeZone = { bias: e.bias, stdBias: e.std_bias, dltBias: e.dlt_bias, stdName: e.std_name, dltName: e.dlt_name || e.std_name, dstAllowed: e.dlt_name !== '' };
    },
    GetDSTAllowed: () => s.timeZone && s.timeZone.dstAllowed ? 1 : 0,
    GetDST: () => s.timeZone && s.timeZone.dstAllowed && !(s.misc & MISC_NO_DST) ? 1 : 0,
    SetDST: (n, b) => { s.misc = setBit(s.misc, MISC_NO_DST, !b); },
    ForceSetLanguage: () => forceSet(vc, 'ForceSetLanguage'),
    ForceSetTimeZone: () => forceSet(vc, 'ForceSetTimeZone'),
    ForceSetClock: () => forceSet(vc, 'ForceSetClock'),
    GetRecoveryKey: () => vc.recoveryKey(),
    GetROMVersion: () => version(vc.config.kernelVersion),
    GetXdashVersion: () => version(sys.dashboard_version),
  };

  classes.Translator = {
    SetLanguage: (node, n) => { const i = n | 0; if (i >= 0 && i < sys.translations.length) vc.language = i; },
    Translate: (node, key) => vc.translate(vc.language, key),
    GetLanguageCode: () => sys.language_codes[vc.language] ?? '',
    GetDateSeparator: () => DATE_SEPARATOR_DOT.has((vc.config.gameRegion << 16) | vc.language) ? '.' : '/',
    FormatNumber: (node, v) => String(v | 0),
    GetTimeZoneCount: () => sys.time_zones.length,
    GetTimeZoneText: (node, i) => { const e = sys.time_zones[i | 0]; return e ? vc.translate(vc.language, e.key) : ''; },
    TranslateStripColon: (node, key) => { const t = vc.translate(vc.language, key); return t.endsWith(':') ? t.slice(0, -1) : t; },
  };

  classes.MemoryMonitor = memoryMonitorNatives(runtime, vc);
  classes.SavedGameGrid = savedGameGridNatives(runtime, vc);
  classes.CopyDestination = copyDestinationNatives(runtime, vc);
  classes.MusicCollection = musicCollectionNatives(runtime, vc);

  classes.DiscDrive = {
    $props: {
      discType: { get: () => sys.disc_types[vc.config.disc], set: () => { throw new Error('discType is set by the drive'); } },
      locked: { get: () => vc.discLocked, set: (n, v) => { vc.discLocked = !!v; } },
    },
    getTrackCount: () => 0,
    FormatTotalTime: () => '',
    FormatTrackTime: () => '',
    LaunchDisc: () => vc.reboot(),
  };

  classes.Recovery = {
    StartRecovery: (node) => {
      node.proxy.recoveryProgress = 1;
      runtime.fireEvent(node, 'OnRecoveryProgressChanged');
      runtime.fireEvent(node, 'OnRecoveryComplete');
    },
    FinishRecovery: () => vc.reboot(),
  };

  classes.ScreenSaver = { reset: (node) => runtime.screenSaver.reset() };
  classes.Joystick = { EnableGlobalInput: (node, b) => { runtime.inputEnabled = !!b; } };
  classes.Settings = {
    GetValue: (node, key) => node._native.values[String(key)] ?? '',
    SetValue: (node, key, value) => { node._native.values[String(key)] = String(value); },
  };
  classes.Settings.$init = () => ({ values: {} });
  classes.File = { readText: () => '' };
  classes.Folder = {
    sortByName: (node) => sortFiles(node, (a, b) => String(a.name).localeCompare(String(b.name))),
    sortByType: (node) => sortFiles(node, (a, b) => String(a.type).localeCompare(String(b.type))),
    sortByDate: (node) => sortFiles(node, (a, b) => Number(a.date) - Number(b.date)),
    sortByLength: (node) => sortFiles(node, (a, b) => Number(a.length) - Number(b.length)),
  };
  classes.Mesh = { load: (node) => runtime.reloadMesh(node) };
  classes.Date = dateMembers(vc);
  return classes;
}

// Calendar members the engine adds to Date, the month table coming from the executable
export function dateMembers(vc) {
  return {
    getDaysInMonth(month, year) { return (month | 0) === 2 ? 28 + isLeapYear(year | 0) : vc.system.days_in_month[month | 0]; },
    isLeapYear(year) { return isLeapYear(year | 0); },
    SetSystemClock() { vc.clockOffsetMs = this.getTime() - vc.realNow(); },
  };
}

function forceSet(vc, name) {
  return vc.launch && vc.launch.reason === 3 && (vc.launch.parameter1 & vc.system.force_set_flags[name]) ? 1 : 0;
}

function isLeapYear(y) {
  return (y % 4 === 0 && y % 100 !== 0) || y % 400 === 0 ? 1 : 0;
}

function sortFiles(node, compare) {
  const files = node.proxy.files;
  if (Array.isArray(files)) files.sort(compare);
}

// Device units are numbered by controller slot, eight memory units then the hard disk
function memoryMonitorNatives(runtime, vc) {
  const state = (node) => node._native;
  const present = (unit) => vc.unitPresent(unit);
  const unitLetter = (c) => {
    const l = String(c).toUpperCase().charCodeAt(0);
    if (l === 84 || l === 85) return HARD_DISK_UNIT;
    if (l >= 70 && l <= 77) return l - 70;
    return -1;
  };
  const fire = (node) => runtime.fireEvent(node, 'OnCurDevUnitChange');
  const walk = (node, dir) => {
    const st = state(node);
    for (const cand of vc.system.device_navigation[st.curDevUnit][dir]) {
      if (cand === HARD_DISK_UNIT || present(cand)) { st.curDevUnit = cand; return; }
    }
  };
  const placeRow = (node) => {
    const st = state(node);
    if (st.curDevUnit === HARD_DISK_UNIT) return;
    st.unitLow = st.curDevUnit & 1;
    st.padHigh = (st.curDevUnit >> 1) >= 2 ? 1 : 0;
  };
  const placeColumn = (node) => {
    const st = state(node);
    if (st.curDevUnit === HARD_DISK_UNIT) return;
    st.unitLow = st.curDevUnit & 1;
    const pad = st.curDevUnit >> 1;
    st.rowLow = pad === 1 || pad === 2 ? 1 : 0;
  };
  const tryUnits = (node, units) => {
    const st = state(node);
    for (const u of units) { st.curDevUnit = u; if (present(u)) return true; }
    st.curDevUnit = HARD_DISK_UNIT;
    return false;
  };
  const blocks = (unit) => vc.diskBlocks(unit);
  const refreshTotals = (node) => {
    const st = state(node);
    const b = blocks(st.curDevUnit);
    const total = b ? b.total : -1, free = b ? b.free : -1;
    if (total !== st.total || free !== st.free) {
      st.total = total; st.free = free;
      runtime.fireEvent(node, 'OnTotalFreeChanged');
    }
  };
  const natives = {
    selectUp: (node) => {
      const st = state(node);
      if (st.curDevUnit === HARD_DISK_UNIT) return;
      walk(node, 0);
      st.rowLow = 0;
      placeRow(node);
      fire(node);
    },
    selectDown: (node) => {
      const st = state(node);
      if (st.curDevUnit === HARD_DISK_UNIT) return;
      walk(node, 1);
      st.rowLow = 1;
      placeRow(node);
      fire(node);
    },
    selectLeft: (node) => {
      const st = state(node);
      if (st.curDevUnit === HARD_DISK_UNIT) {
        const first = st.rowLow ? 2 : 0, second = st.rowLow ? 0 : 2;
        if (!tryUnits(node, [first + 1, first, second + 1, second])) return;
      } else {
        walk(node, 2);
      }
      st.padHigh = 0;
      placeColumn(node);
      fire(node);
    },
    selectRight: (node) => {
      const st = state(node);
      if (st.curDevUnit === HARD_DISK_UNIT) {
        const first = st.rowLow ? 4 : 6, second = st.rowLow ? 6 : 4;
        if (!tryUnits(node, [first, first + 1, second, second + 1])) return;
      } else {
        walk(node, 3);
      }
      st.padHigh = 1;
      placeColumn(node);
      fire(node);
    },
    selectDevUnit: (node, c) => {
      const unit = unitLetter(c);
      if (unit < 0) return 0;
      if (unit !== HARD_DISK_UNIT && !present(unit)) return 0;
      state(node).curDevUnit = unit;
      refreshTotals(node);
      return 1;
    },
    FormatDeviceName: (node, unit) => {
      const u = (unit | 0) === -1 ? state(node).curDevUnit : unit | 0;
      return u >= 0 && u <= 7 && present(u) ? (vc.unitNames[u] ?? vc.config.memoryUnits[u].name ?? '') : '';
    },
    FormatTotalBlocks: (node) => vc.formatBlocks(state(node).total),
    FormatFreeBlocks: (node, unit) => {
      if ((unit | 0) === -1) return vc.formatBlocks(state(node).free);
      const b = blocks(unit | 0);
      return vc.formatBlocks(b ? b.free : unit | 0);
    },
    GetTotalFreeBlocks: (node, unit) => {
      if ((unit | 0) === -1) return state(node).free;
      const b = blocks(unit | 0);
      return b ? b.free : unit | 0;
    },
    HaveDeviceTop: (node, pad) => pad >= 0 && pad <= 3 && present(pad * 2) ? 1 : 0,
    HaveDeviceBottom: (node, pad) => pad >= 0 && pad <= 3 && present(pad * 2 + 1) ? 1 : 0,
    GetFreeTotalRatio: (node, unit) => {
      const b = blocks(unit | 0);
      return b ? Math.fround(b.free / b.total) : 0;
    },
    SetMUName: (node, unit, name) => { if (present(unit | 0) && (unit | 0) !== HARD_DISK_UNIT) vc.unitNames[unit | 0] = String(name); },
    FormatMemoryUnit: (node, unit) => {
      const u = unit | 0;
      if (!present(u) || u === HARD_DISK_UNIT) return 0;
      const mu = vc.config.memoryUnits[u];
      mu.freeBytes = mu.totalBytes;
      delete vc.unitNames[u];
      refreshTotals(node);
      return 1;
    },
  };
  natives.$init = (node) => ({ curDevUnit: HARD_DISK_UNIT, invalidDevUnit: -1, total: -1, free: -1, rowLow: 0, padHigh: 0, unitLow: 0 });
  natives.$props = {
    curDevUnit: { get: (node) => state(node).curDevUnit, set: (node, v) => { state(node).curDevUnit = v | 0; refreshTotals(node); } },
    invalidDevUnit: { get: (node) => state(node).invalidDevUnit, set: (node, v) => { state(node).invalidDevUnit = v | 0; } },
    enumerationOn: { get: (node) => !!node._fields.enumerationOn, set: (node, v) => { node._fields.enumerationOn = !!v; if (v) refreshTotals(node); } },
  };
  return natives;
}

// The grid shows one device's titles with their saves, none on a data partition that holds no games
function savedGameGridNatives(runtime, vc) {
  const state = (node) => node._native;
  const titles = (unit) => vc.savedTitles[unit] ?? [];
  const notReady = (node, unit) => vc.unitPresent(unit) && state(node).enumerating[unit] === true;
  const fire = (node, name) => runtime.fireEvent(node, name);
  const selectionChanged = (node) => {
    fire(node, 'OnSelChange');
    runtime.derived.globals.set(vc.system.selection_clock, { f64: runtime.derived.clock });
  };
  const curTitle = (node) => node._fields.curTitle | 0;
  const curSave = (node) => node._fields.curSavedGame | 0;
  const perRow = (node) => node._fields.iconsPerRow | 0;
  const titleCount = (node) => {
    const st = state(node);
    const unit = st.curDevUnit;
    let n = notReady(node, unit) ? 0 : titles(unit).length;
    if (unit === HARD_DISK_UNIT) {
      st.soundtrackRow = n;
      st.soundtrackCount = vc.soundtracks.length;
      if (st.soundtrackCount > 0) n++;
    } else {
      st.soundtrackRow = -1;
      st.soundtrackCount = 0;
    }
    return n;
  };
  const savedCount = (node, title) => {
    const st = state(node);
    if (title < 0 || notReady(node, st.curDevUnit)) return 0;
    if (st.soundtrackRow >= 0 && title >= st.soundtrackRow) return st.soundtrackCount;
    const t = titles(st.curDevUnit)[title];
    return t ? t.saves.length : 0;
  };
  const selectTitle = (node, title) => {
    const st = state(node);
    st.prevTitle = curTitle(node);
    node._fields.curTitle = title;
    if (notReady(node, st.curDevUnit)) return;
    const count = titleCount(node);
    if (curTitle(node) >= count) node._fields.curTitle = count - 1;
    const saves = savedCount(node, curTitle(node));
    const cols = perRow(node);
    const rows = Math.floor((saves + cols - 1) / cols);
    let pref = node._fields.nPrefColumn | 0;
    if (pref > -1) {
      if (curSave(node) !== -1) pref = curSave(node) % cols;
    } else if (pref < -1) {
      pref = 0;
    }
    node._fields.nPrefColumn = pref;
    if (pref === -1) {
      node._fields.curSavedGame = -1;
      node._fields.iconRowScroll = 0;
      return;
    }
    if (curTitle(node) > st.prevTitle) {
      node._fields.curSavedGame = Math.min(pref, saves - 1);
      node._fields.iconRowScroll = 0;
    } else {
      node._fields.curSavedGame = saves === 0 ? -1 : Math.min(pref + (rows - 1) * cols, saves - 1);
      node._fields.iconRowScroll = Math.max(rows - 3, 0);
    }
  };
  const enumerate = (node, unit) => {
    const st = state(node);
    if (!vc.unitPresent(unit)) return;
    st.enumerating[unit] = true;
    fire(node, 'OnUpdatingTitlesBegin');
    setTimeout(() => {
      st.enumerating[unit] = false;
      const b = vc.diskBlocks(unit);
      node._fields.freeBlocks = b.free;
      st.totalBlocks = b.total;
      fire(node, 'OnTotalFreeChanged');
      fire(node, 'OnUpdatingTitlesEnd');
    }, ENUMERATE_DELAY_MS);
  };
  const natives = {
    selectUp: (node) => {
      const st = state(node);
      const save = curSave(node);
      const cols = perRow(node);
      const row = save === -1 ? 0 : Math.floor(save / cols);
      if (save !== -1 && row > 0) {
        const next = (node._fields.nPrefColumn | 0) + (row - 1) * cols;
        node._fields.curSavedGame = next;
        if (row - 1 < (node._fields.iconRowScroll | 0)) node._fields.iconRowScroll = row - 1;
        node._fields.nPrefColumn = next % cols;
        selectionChanged(node);
        return;
      }
      const floor = st.curDevUnit === HARD_DISK_UNIT ? 0 : -1;
      if (curTitle(node) > floor) {
        selectTitle(node, curTitle(node) - 1);
        selectionChanged(node);
      }
      node._fields.nPrefColumn = curSave(node) % cols;
    },
    selectDown: (node) => {
      const save = curSave(node);
      const cols = perRow(node);
      if (save !== -1) {
        const saves = savedCount(node, curTitle(node));
        const row = Math.floor(save / cols), lastRow = Math.floor((saves + cols - 1) / cols) - 1;
        if (row < lastRow) {
          node._fields.curSavedGame = Math.min(save + cols, saves - 1);
          if (row + 1 >= (node._fields.iconRowScroll | 0) + 2) node._fields.iconRowScroll = row - 1;
          node._fields.nPrefColumn = curSave(node) % cols;
          selectionChanged(node);
          return;
        }
      }
      if (curTitle(node) < titleCount(node) - 1) {
        selectTitle(node, curTitle(node) + 1);
        selectionChanged(node);
      }
      node._fields.nPrefColumn = curSave(node) % cols;
    },
    selectLeft: (node) => {
      const save = curSave(node);
      if (save >= 0) {
        if ((node._fields.nPrefColumn | 0) === 0) {
          node._fields.nPrefColumn = -1;
          node._fields.curSavedGame = -1;
          node._fields.iconRowScroll = 0;
        } else {
          node._fields.curSavedGame = save - 1;
          node._fields.nPrefColumn = (save - 1) % perRow(node);
        }
      }
      if (curSave(node) !== save) selectionChanged(node);
    },
    selectRight: (node) => {
      const title = curTitle(node), save = curSave(node);
      const cols = perRow(node);
      const saves = savedCount(node, title);
      if (save < saves - 1 && (node._fields.nPrefColumn | 0) < 3) {
        const next = save + 1;
        node._fields.curSavedGame = next;
        node._fields.nPrefColumn = next % cols;
        if (next >= ((node._fields.iconRowScroll | 0) + 3) * cols) node._fields.iconRowScroll = Math.floor(next / cols) - 2;
      }
      if (curTitle(node) !== title || curSave(node) !== save) selectionChanged(node);
    },
    setSelImage: (node) => { state(node).detachedImage = { title: curTitle(node), save: curSave(node) }; },
    FormatSavedGameName: (node) => {
      const st = state(node);
      const save = curSave(node);
      if (save < 0) return '';
      if (st.soundtrackRow >= 0 && curTitle(node) === st.soundtrackRow) return vc.soundtracks[save] ? vc.soundtracks[save].name : '';
      const t = titles(st.curDevUnit)[curTitle(node)];
      return t && t.saves[save] ? t.saves[save].name : '';
    },
    FormatSavedGameTime: (node) => {
      const t = titles(state(node).curDevUnit)[curTitle(node)];
      const save = t && t.saves[curSave(node)];
      return save ? save.time : '';
    },
    FormatSavedGameSize: (node) => {
      const t = titles(state(node).curDevUnit)[curTitle(node)];
      const save = t && t.saves[curSave(node)];
      return String(save ? save.blocks : -1);
    },
    FormatTitleSize: (node) => {
      const t = titles(state(node).curDevUnit)[curTitle(node)];
      return String(t ? t.blocks : -1);
    },
    FormatTotalBlocks: (node) => vc.formatBlocks(state(node).totalBlocks),
    GetTotalBlocks: (node) => state(node).totalBlocks,
    FormatFreeBlocks: (node) => vc.formatBlocks(node._fields.freeBlocks | 0),
    CanDetachIcon: (node) => state(node).scrollStart === 0 && state(node).iconLoaded ? 1 : 0,
    GetSavedGameCount: (node, title) => savedCount(node, title | 0),
    GetTitleName: (node, title) => { const t = titles(state(node).curDevUnit)[title | 0]; return t ? t.name : ''; },
    GetTitleID: (node, title) => { const t = titles(state(node).curDevUnit)[title | 0]; return t ? t.id : 0; },
    StartCopy: (node, dest) => { node._fields.error = 1; fire(node, 'OnCopyError'); },
    StartDelete: (node) => {
      const st = state(node);
      const list = titles(st.curDevUnit);
      if (st.soundtrackRow >= 0 && curTitle(node) === st.soundtrackRow) {
        if (curSave(node) >= 0) vc.soundtracks.splice(curSave(node), 1); else vc.soundtracks.length = 0;
      } else if (list[curTitle(node)]) {
        if (curSave(node) >= 0) list[curTitle(node)].saves.splice(curSave(node), 1); else list.splice(curTitle(node), 1);
      }
      node._fields.curSavedGame = -1;
      fire(node, 'OnDeleteEnd');
    },
    DoesSavedGameExists: (node, dest) => {
      const t = titles(state(node).curDevUnit)[curTitle(node)];
      const save = t && t.saves[curSave(node)];
      if (!save) return 0;
      const target = titles(dest | 0).find((x) => x.id === t.id);
      return target && target.saves.some((x) => x.name === save.name) ? 1 : 0;
    },
    GetSavedGamePath: (node) => {
      const t = titles(state(node).curDevUnit)[curTitle(node)];
      const save = t && t.saves[curSave(node)];
      return save ? save.path : '';
    },
    CanCopy: (node) => {
      const t = titles(state(node).curDevUnit)[curTitle(node)];
      const save = t && t.saves[curSave(node)];
      return save && !save.noCopy ? 1 : 0;
    },
    IsSoundtrackSelected: (node) => state(node).soundtrackRow >= 0 && curTitle(node) === state(node).soundtrackRow ? 1 : 0,
    IsDevUnitReady: (node, unit) => notReady(node, unit | 0) ? 0 : 1,
    GetTitleCount: (node) => titleCount(node),
    GetUpdateString: (node) => {
      const st = state(node);
      const now = performance.now();
      if (now - st.dotTime > COPY_DOT_PERIOD_MS) {
        st.dotTime = now;
        st.dots = st.dots + 1 > COPY_DOTS ? 0 : st.dots + 1;
      }
      return vc.translate(vc.language, 'Copying') + '.'.repeat(st.dots);
    },
  };
  natives.$init = () => ({ curDevUnit: -1, enumerating: {}, soundtrackRow: -1, soundtrackCount: 0, totalBlocks: -1, prevTitle: -1, scrollStart: 0, iconLoaded: false, dots: 0, dotTime: 0, detachedImage: null });
  natives.$props = {
    curDevUnit: { get: (node) => state(node).curDevUnit, set: (node, v) => { const unit = v | 0; if (state(node).curDevUnit !== unit) { state(node).curDevUnit = unit; enumerate(node, unit); } } },
  };
  natives.$drawsNothing = (node) => titleCount(node) === 0 || !node._fields.isActive;
  return natives;
}

// A copy target cycles through the units that can take the selected save
function copyDestinationNatives(runtime, vc) {
  const state = (node) => node._native;
  const units = (node) => {
    const source = node._fields.sourceDevUnit | 0;
    const list = [];
    for (let u = 0; u <= HARD_DISK_UNIT; u++) if (u !== source && vc.unitPresent(u)) list.push(u);
    return list;
  };
  const move = (node, delta) => {
    const list = units(node);
    if (list.length < 2) return;
    const next = (node._fields.select | 0) + delta;
    if (next < 0 || next >= list.length) return;
    node._fields.select = next;
    node._fields.curDevUnit = list[next];
    runtime.fireEvent(node, 'OnCurDevUnitChange');
  };
  const natives = {
    selectUp: (node) => move(node, -1),
    selectDown: (node) => move(node, 1),
  };
  natives.$init = () => ({});
  natives.$props = {
    isActive: { get: (node) => !!node._fields.isActive, set: (node, v) => {
      node._fields.isActive = !!v;
      if (!v) return;
      const list = units(node);
      node._fields.select = 0;
      node._fields.curDevUnit = list.length ? list[0] : -1;
      node._fields.selDevUnit = node._fields.curDevUnit;
    } },
  };
  natives.$drawsNothing = () => true;
  return natives;
}

// Soundtracks live on the data partition, a console that never copied an audio CD has none
function musicCollectionNatives(runtime, vc) {
  const st = (i) => vc.soundtracks[i | 0];
  const song = (i, j) => { const t = st(i); return t ? t.songs[j | 0] : undefined; };
  const time = (ms) => { const s = Math.floor(ms / 1000); return `${String(Math.floor(s / 60)).padStart(2, '0')}:${String(s % 60).padStart(2, '0')}`; };
  let nextId = 1;
  const natives = {
    GetSoundtrackCount: () => vc.soundtracks.length,
    GetSoundtrackID: (node, i) => { const t = st(i); return t ? t.id : -1; },
    GetSoundtrackIndexFromID: (node, id) => vc.soundtracks.findIndex((t) => t.id === (id | 0)),
    GetSoundtrackName: (node, i) => { const t = st(i); return t ? t.name : ''; },
    FormatSoundtrackTime: (node, i) => { const t = st(i); return t ? time(t.songs.reduce((a, s) => a + s.ms, 0)) : ''; },
    GetSoundtrackSongCount: (node, i) => { const t = st(i); return t ? t.songs.length : 0; },
    GetSoundtrackSongID: (node, i, j) => { const s = song(i, j); return s ? s.id : -1; },
    GetSoundtrackSongName: (node, i, j) => { const s = song(i, j); return s ? s.name : ''; },
    FormatSoundtrackSongTime: (node, i, j) => { const s = song(i, j); return s ? time(s.ms) : ''; },
    AddSoundtrack: (node, name) => { vc.soundtracks.push({ id: nextId++, name: String(name), songs: [] }); return vc.soundtracks.length - 1; },
    DeleteSoundtrack: (node, i) => { if (st(i)) vc.soundtracks.splice(i | 0, 1); },
    ClearCopyList: (node) => { node._native.copyList = []; },
    AddSongToCopyList: (node, i, j) => { node._native.copyList.push({ soundtrack: i | 0, song: j | 0 }); },
    StartCopy: (node, dest) => {
      node._fields.error = node._native.copyList.length === 0 ? 3 : 0;
      for (const entry of node._native.copyList) {
        const s = entry.soundtrack < 0 ? null : song(entry.soundtrack, entry.song);
        if (!s) { node._fields.error = 3; break; }
        const target = st(dest);
        if (!target) { node._fields.error = 1; break; }
        target.songs.push({ ...s, id: nextId++ });
      }
      node._fields.copyProgress = 1;
      runtime.fireEvent(node, 'OnCopyProgressChanged');
      runtime.fireEvent(node, 'OnCopyComplete');
    },
    SetSongName: (node, i, j, name) => { const s = song(i, j); if (s) s.name = String(name); },
    SetSoundtrackName: (node, i, name) => { const t = st(i); if (t) t.name = String(name); },
    MoveSongUp: (node, i, j) => { const t = st(i); if (t && j > 0 && j < t.songs.length) [t.songs[j - 1], t.songs[j]] = [t.songs[j], t.songs[j - 1]]; },
    MoveSongDown: (node, i, j) => { const t = st(i); if (t && j >= 0 && j < t.songs.length - 1) [t.songs[j + 1], t.songs[j]] = [t.songs[j], t.songs[j + 1]]; },
    DeleteSong: (node, i, j) => { const t = st(i); if (t && song(i, j)) t.songs.splice(j | 0, 1); },
    CreateSoundtrackName: (node, prefix) => {
      for (let n = 1; ; n++) {
        const name = `${prefix} ${n}`;
        if (!vc.soundtracks.some((t) => t.name === name)) return name;
      }
    },
    GetUpdateString: (node) => {
      const s = node._native;
      const now = performance.now();
      if (now - s.dotTime > COPY_DOT_PERIOD_MS) {
        s.dotTime = now;
        s.dots = s.dots + 1 > COPY_DOTS ? 0 : s.dots + 1;
      }
      return vc.translate(vc.language, 'Copying') + '.'.repeat(s.dots);
    },
  };
  natives.$init = () => ({ copyList: [], dots: 0, dotTime: 0 });
  return natives;
}

export const NATIVE_DRAW_CLASSES = ['SavedGameGrid', 'CopyDestination', 'Keyboard'];
