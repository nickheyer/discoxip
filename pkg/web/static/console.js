// The console's kernel and XDK services the executable's own natives reach: an EEPROM, the hard disk volumes,
// the clock, the empty device ports, a network with no link and the C runtime pieces the libraries leave to it

// Win32 errors and the values the file routines hand back on failure
const ERROR_FILE_NOT_FOUND = 2, ERROR_PATH_NOT_FOUND = 3, ERROR_ACCESS_DENIED = 5, ERROR_INVALID_HANDLE = 6, ERROR_NO_MORE_FILES = 18, ERROR_NOT_READY = 21;
const ERROR_FILE_EXISTS = 80, ERROR_DIR_NOT_EMPTY = 145, ERROR_ALREADY_EXISTS = 183, ERROR_DEVICE_NOT_CONNECTED = 1167;
const INVALID_HANDLE = 0xFFFFFFFF, INVALID_FILE_ATTRIBUTES = 0xFFFFFFFF;
// NT statuses the kernel routines return
const STATUS_SUCCESS = 0, STATUS_OBJECT_NAME_NOT_FOUND = 0xC0000034, STATUS_OBJECT_PATH_NOT_FOUND = 0xC000003A, STATUS_INVALID_HANDLE = 0xC0000008;
const STATUS_END_OF_FILE = 0xC0000011, STATUS_DEVICE_NOT_CONNECTED = 0xC000009D, STATUS_NOT_SUPPORTED = 0xC00000BB, STATUS_ACCESS_DENIED = 0xC0000022, STATUS_BUFFER_TOO_SMALL = 0xC0000023, STATUS_OBJECT_NAME_COLLISION = 0xC0000035;
// HRESULTs of the online library on a console with no link
const S_OK = 0, E_FAIL = 0x80004005, XONLINE_E_LOGON_NO_NETWORK_CONNECTION = 0x80151000, XONLINE_E_NO_USER = 0x80150016;
// Creation dispositions and attributes of the file routines
const CREATE_NEW = 1, CREATE_ALWAYS = 2, OPEN_EXISTING = 3, OPEN_ALWAYS = 4, TRUNCATE_EXISTING = 5;
const FILE_ATTRIBUTE_READONLY = 1, FILE_ATTRIBUTE_DIRECTORY = 0x10, FILE_ATTRIBUTE_NORMAL = 0x80, GENERIC_WRITE = 0x40000000;
const FILE_BEGIN = 0, FILE_CURRENT = 1, FILE_END = 2;
// WIN32_FIND_DATAA words
const FIND_ATTRIBUTES = 0, FIND_CREATED = 4, FIND_ACCESSED = 12, FIND_WRITTEN = 20, FIND_SIZE_HIGH = 28, FIND_SIZE_LOW = 32, FIND_NAME = 44;
// OBJECT_ATTRIBUTES, ANSI_STRING and IO_STATUS_BLOCK words of the kernel
const OA_NAME = 4, ANSI_LENGTH = 0, ANSI_MAXIMUM = 2, ANSI_BUFFER = 4, IOSB_STATUS = 0, IOSB_INFORMATION = 4;
// FILE_NETWORK_OPEN_INFORMATION words
const NET_CREATED = 0, NET_ACCESSED = 8, NET_WRITTEN = 16, NET_CHANGED = 24, NET_ALLOCATION = 32, NET_END = 40, NET_ATTRIBUTES = 48;
// The partition the dashboard was started from, which the kernel links the launch drive to
const TITLE_PARTITION = 2, TITLE_PARTITION_PATH = '\\Device\\Harddisk0\\Partition2', UTILITY_PARTITION_PATH = '\\Device\\Harddisk0\\Partition5';
// Handles the console hands out start past the values scripts and code compare against zero or minus one
const FIRST_HANDLE = 0x1000;
// The hundred nanosecond ticks between the file time epoch and the Unix one
const FILETIME_EPOCH = 116444736000000000n;
// EEPROM value types as the query routine reports them
const REG_SZ = 1, REG_BINARY = 3, REG_DWORD = 4;
// EEPROM value indices the dashboard reads and writes
const XC_TIMEZONE_BIAS = 0, XC_TZ_STD_NAME = 1, XC_TZ_DLT_NAME = 2, XC_TZ_STD_DATE = 3, XC_TZ_DLT_DATE = 4, XC_TZ_STD_BIAS = 5, XC_TZ_DLT_BIAS = 6;
const XC_LANGUAGE = 7, XC_VIDEO = 8, XC_AUDIO = 9, XC_P_CONTROL_GAMES = 10, XC_P_CONTROL_PASSWORD = 11, XC_P_CONTROL_MOVIES = 12, XC_ONLINE_IP_ADDRESS = 13, XC_ONLINE_DNS = 14, XC_ONLINE_DEFAULT_GATEWAY = 15, XC_ONLINE_SUBNET = 16, XC_MISC = 17, XC_DVD_REGION = 18;
const XC_FACTORY_SERIAL_NUMBER = 0x100, XC_FACTORY_ETHERNET_ADDR = 0x101, XC_FACTORY_ONLINE_KEY = 0x102, XC_FACTORY_AV_REGION = 0x103, XC_FACTORY_GAME_REGION = 0x104, XC_ENCRYPTED_HD_KEY = 0x201;
// The memory the console holds
const CONSOLE_MEMORY = 64 * 1024 * 1024;
// The kernel keeps its page frame database in the top pages of memory and the GPU's instance memory in the pages
// below it, where no contiguous allocation reaches
const PFN_DATABASE_BYTES = 0x10000, GPU_INSTANCE_BYTES = 0x10000;
// Interrupt vectors start at this one, and a bus level's priority counts down from the highest
const INTERRUPT_VECTOR_BASE = 0x30, HIGHEST_BUS_LEVEL = 27;
// The C runtime's random number recurrence
const RAND_MULTIPLIER = 214013, RAND_INCREMENT = 2531011;
// A file time as the number of hundred nanosecond ticks since 1601, from milliseconds since 1970
function fileTimeOf(ms) {
  return BigInt(Math.floor(ms)) * 10000n + FILETIME_EPOCH;
}

// Milliseconds since 1970 of a file time
function msOfFileTime(ft) {
  return Number((ft - FILETIME_EPOCH) / 10000n);
}

// A volume is one hard disk partition kept in memory as a directory tree, the title's files read only
class Volume {
  constructor(writable, totalBytes) {
    this.writable = writable;
    this.totalBytes = totalBytes;
    this.entries = new Map();
    this.entries.set('', { name: '', dir: true, attributes: FILE_ATTRIBUTE_DIRECTORY, data: null, created: 0n, written: 0n });
  }

  // Bytes the files of the volume take
  usedBytes() {
    let n = 0;
    for (const e of this.entries.values()) if (!e.dir) n += e.data.length;
    return n;
  }

  // Makes every directory along a path, which the title's files were copied without
  ensureDirectories(key, now) {
    const parts = key.split('\\');
    for (let i = 1; i < parts.length; i++) {
      const dir = parts.slice(0, i).join('\\');
      if (!this.entries.has(dir)) this.entries.set(dir, { name: parts[i - 1], dir: true, attributes: FILE_ATTRIBUTE_DIRECTORY, data: null, created: now, written: now });
    }
  }
}

export class ConsoleApi {
  constructor(vc) {
    this.vc = vc;
    this.d = null;
    this.lastError = 0;
    this.handles = new Map();
    this.nextHandle = FIRST_HANDLE;
    this.randState = 1;
    this.volumes = new Map();
    for (const [partition, writable, total] of [[1, true, vc.config.dataPartitionSectors * 512], [2, false, 0x1F400000], [3, true, 0x2EE00000], [4, true, 0x2EE00000], [5, true, 0x2EE00000]]) this.volumes.set(partition, new Volume(writable, total));
    // The kernel links the launch drive to the partition the executable started from, the executable's startup adding the rest
    this.links = new Map([['d', TITLE_PARTITION_PATH]]);
    this.eeprom = new Map();
    this.resetEeprom();
    // PCI configuration space by bus and slot, and the I/O ports
    this.pci = new Map();
    this.ports = new Map();
  }

  // Loads the title's files onto its partition, which the executable reaches through every one of its path forms
  attachFiles(files, fileData) {
    const vol = this.volumes.get(TITLE_PARTITION);
    const now = this.now();
    for (const name of files) {
      const key = name.toLowerCase().replace(/\//g, '\\');
      vol.ensureDirectories(key, now);
      const data = fileData.get(key) || new Uint8Array(0);
      vol.entries.set(key, { name: key.split('\\').pop(), dir: false, attributes: FILE_ATTRIBUTE_READONLY, data, created: now, written: now });
    }
    // The data partition of a retail console holds the title and user data roots from the factory
    const data = this.volumes.get(1);
    for (const dir of ['tdata', 'udata']) data.ensureDirectories(dir + '\\x', now);
  }

  // The EEPROM of a console that was never configured, its factory section naming the region and video standard
  resetEeprom() {
    const dword = (id, v) => this.eeprom.set(id, { type: REG_DWORD, data: Uint8Array.of(v & 255, (v >>> 8) & 255, (v >>> 16) & 255, (v >>> 24) & 255) });
    const bytes = (id, type, n) => this.eeprom.set(id, { type, data: new Uint8Array(n) });
    dword(XC_TIMEZONE_BIAS, 0);
    bytes(XC_TZ_STD_NAME, REG_SZ, 4);
    bytes(XC_TZ_DLT_NAME, REG_SZ, 4);
    dword(XC_TZ_STD_DATE, 0);
    dword(XC_TZ_DLT_DATE, 0);
    dword(XC_TZ_STD_BIAS, 0);
    dword(XC_TZ_DLT_BIAS, 0);
    dword(XC_LANGUAGE, 0);
    dword(XC_VIDEO, 0);
    dword(XC_AUDIO, 0);
    dword(XC_P_CONTROL_GAMES, 0);
    dword(XC_P_CONTROL_PASSWORD, 0);
    dword(XC_P_CONTROL_MOVIES, 0);
    dword(XC_ONLINE_IP_ADDRESS, 0);
    dword(XC_ONLINE_DNS, 0);
    dword(XC_ONLINE_DEFAULT_GATEWAY, 0);
    dword(XC_ONLINE_SUBNET, 0);
    dword(XC_MISC, 0);
    dword(XC_DVD_REGION, 0);
    bytes(XC_FACTORY_SERIAL_NUMBER, REG_BINARY, 12);
    bytes(XC_FACTORY_ETHERNET_ADDR, REG_BINARY, 6);
    bytes(XC_FACTORY_ONLINE_KEY, REG_BINARY, 16);
    dword(XC_FACTORY_AV_REGION, this.vc.config.videoStandard);
    dword(XC_FACTORY_GAME_REGION, this.vc.config.gameRegion);
    this.eeprom.set(XC_ENCRYPTED_HD_KEY, { type: REG_BINARY, data: Uint8Array.from(this.vc.config.hardDiskKey) });
  }

  // Bytes of physical memory the console holds
  memoryBytes() {
    return CONSOLE_MEMORY;
  }

  // Bytes at the top of physical memory the kernel keeps out of contiguous allocations
  reservedTopBytes() {
    return PFN_DATABASE_BYTES + GPU_INSTANCE_BYTES;
  }

  // A stored EEPROM value as the unsigned word its bytes make
  eepromWord(id) {
    const v = this.eeprom.get(id);
    return v ? (v.data[0] | (v.data[1] << 8) | (v.data[2] << 16) | (v.data[3] << 24)) >>> 0 : 0;
  }

  now() {
    return fileTimeOf(this.vc.now());
  }

  // The partition volume and the lower case path inside it that a path names, drive letters resolving through their links
  locate(path) {
    let p = String(path).replace(/\//g, '\\');
    for (let hops = 0; hops < 8; hops++) {
      let m;
      if ((m = /^(?:\\\\[.?]\\|\\\?\?\\)?([a-zA-Z]):(.*)$/.exec(p))) {
        const target = this.links.get(m[1].toLowerCase());
        if (target === undefined) return { vol: null, key: null, error: ERROR_PATH_NOT_FOUND };
        p = target + (m[2] === '' || m[2].startsWith('\\') ? m[2] : '\\' + m[2]);
        continue;
      }
      if ((m = /^\\Device\\Harddisk0\\Partition(\d+)(.*)$/i.exec(p))) {
        const vol = this.volumes.get(Number(m[1]));
        if (!vol) return { vol: null, key: null, error: ERROR_DEVICE_NOT_CONNECTED };
        const key = m[2].toLowerCase().replace(/^\\+/, '').replace(/\\+$/, '').replace(/\\\\+/g, '\\');
        return { vol, key, error: 0 };
      }
      if (/^\\Device\\CdRom0/i.test(p)) return { vol: null, key: null, error: ERROR_NOT_READY };
      if (/^\\Device\\MU_/i.test(p)) return { vol: null, key: null, error: ERROR_DEVICE_NOT_CONNECTED };
      return { vol: null, key: null, error: ERROR_PATH_NOT_FOUND };
    }
    throw new Error(`the drive links loop resolving ${path}`);
  }

  // The drive letter a link object name names, or null for any other object
  linkLetter(name) {
    const m = /^\\\?\?\\([a-zA-Z]):$/.exec(String(name).replace(/\//g, '\\'));
    return m ? m[1].toLowerCase() : null;
  }

  // Opens or creates a file the way the Win32 creation dispositions say, handing back a handle or the invalid one
  open(path, access, disposition) {
    const { vol, key, error } = this.locate(path);
    if (!vol) { this.lastError = error; return INVALID_HANDLE; }
    let entry = vol.entries.get(key);
    const exists = entry !== undefined;
    if (entry && entry.dir) {
      const handle = this.nextHandle++;
      this.handles.set(handle, { kind: 'dir', vol, key, entry });
      return handle;
    }
    const wants = (access & GENERIC_WRITE) !== 0 || disposition !== OPEN_EXISTING;
    if (wants && !vol.writable) { this.lastError = ERROR_ACCESS_DENIED; return INVALID_HANDLE; }
    switch (disposition) {
      case CREATE_NEW:
        if (exists) { this.lastError = ERROR_FILE_EXISTS; return INVALID_HANDLE; }
        break;
      case OPEN_EXISTING:
      case TRUNCATE_EXISTING:
        if (!exists) { this.lastError = ERROR_FILE_NOT_FOUND; return INVALID_HANDLE; }
        break;
    }
    if (!exists) {
      const parent = key.includes('\\') ? key.slice(0, key.lastIndexOf('\\')) : '';
      if (!vol.entries.has(parent)) { this.lastError = ERROR_PATH_NOT_FOUND; return INVALID_HANDLE; }
      entry = { name: key.split('\\').pop(), dir: false, attributes: FILE_ATTRIBUTE_NORMAL, data: new Uint8Array(0), created: this.now(), written: this.now() };
      vol.entries.set(key, entry);
    } else if (disposition === CREATE_ALWAYS || disposition === TRUNCATE_EXISTING) {
      entry.data = new Uint8Array(0);
      entry.written = this.now();
    }
    this.lastError = exists && (disposition === CREATE_ALWAYS || disposition === OPEN_ALWAYS) ? ERROR_ALREADY_EXISTS : 0;
    const handle = this.nextHandle++;
    this.handles.set(handle, { kind: 'file', vol, key, entry, pos: 0, access });
    return handle;
  }

  // The open file behind a handle
  file(handle) {
    const f = this.handles.get(handle);
    if (!f || f.kind !== 'file') throw new Error(`handle 0x${handle.toString(16)} is no open file`);
    return f;
  }

  // Reads up to a count of bytes from the file position, moving it past them
  read(handle, count) {
    const f = this.file(handle);
    const out = f.entry.data.subarray(f.pos, Math.min(f.pos + count, f.entry.data.length));
    f.pos += out.length;
    return out;
  }

  // Writes bytes at the file position, growing the file past its end
  write(handle, bytes) {
    const f = this.file(handle);
    if (!f.vol.writable) { this.lastError = ERROR_ACCESS_DENIED; return -1; }
    const end = f.pos + bytes.length;
    if (end > f.entry.data.length) {
      const grown = new Uint8Array(end);
      grown.set(f.entry.data);
      f.entry.data = grown;
    }
    f.entry.data.set(bytes, f.pos);
    f.pos = end;
    f.entry.written = this.now();
    return bytes.length;
  }

  // Moves the file position from the start, the current position or the end, returning the new position
  seek(handle, distance, method) {
    const f = this.file(handle);
    const base = method === FILE_BEGIN ? 0 : method === FILE_CURRENT ? f.pos : method === FILE_END ? f.entry.data.length : null;
    if (base === null) throw new Error(`seek method ${method} is not one the console serves`);
    f.pos = base + distance;
    return f.pos >>> 0;
  }

  close(handle) {
    if (!this.handles.has(handle)) { this.lastError = ERROR_INVALID_HANDLE; return 0; }
    this.handles.delete(handle);
    return 1;
  }

  // Starts a directory search by a pattern with wildcards, handing back the first match and a search handle
  findFirst(pattern) {
    const { vol, key, error } = this.locate(pattern);
    if (!vol) { this.lastError = error; return null; }
    const slash = key.lastIndexOf('\\');
    const dir = slash < 0 ? '' : key.slice(0, slash);
    const mask = key.slice(slash + 1);
    const re = new RegExp('^' + mask.replace(/[.+^${}()|[\]\\]/g, '\\$&').replace(/\*/g, '.*').replace(/\?/g, '.') + '$');
    const found = [];
    for (const [k, e] of vol.entries) {
      if (k === '' || (dir === '' ? k.includes('\\') : !k.startsWith(dir + '\\') || k.slice(dir.length + 1).includes('\\'))) continue;
      if (re.test(e.name)) found.push(e);
    }
    if (!found.length) { this.lastError = vol.entries.has(dir) ? ERROR_FILE_NOT_FOUND : ERROR_PATH_NOT_FOUND; return null; }
    const handle = this.nextHandle++;
    this.handles.set(handle, { kind: 'find', found, next: 1 });
    return { handle, entry: found[0] };
  }

  // The next match of a directory search, or null when it is exhausted
  findNext(handle) {
    const s = this.handles.get(handle);
    if (!s || s.kind !== 'find') { this.lastError = ERROR_INVALID_HANDLE; return null; }
    if (s.next >= s.found.length) { this.lastError = ERROR_NO_MORE_FILES; return null; }
    return s.found[s.next++];
  }

  // The directory entry a path names, or null with the error set
  entry(path) {
    const { vol, key, error } = this.locate(path);
    if (!vol) { this.lastError = error; return null; }
    const e = vol.entries.get(key);
    if (!e) { this.lastError = ERROR_FILE_NOT_FOUND; return null; }
    return { vol, key, entry: e };
  }

  // The next pseudo random number of the C runtime's recurrence
  rand() {
    this.randState = (Math.imul(this.randState, RAND_MULTIPLIER) + RAND_INCREMENT) >>> 0;
    return (this.randState >>> 16) & 0x7FFF;
  }

  // The Win32 error a kernel status maps to
  dosError(status) {
    switch (status >>> 0) {
      case STATUS_SUCCESS: return 0;
      case STATUS_OBJECT_NAME_NOT_FOUND: return ERROR_FILE_NOT_FOUND;
      case STATUS_OBJECT_PATH_NOT_FOUND: return ERROR_PATH_NOT_FOUND;
      case STATUS_INVALID_HANDLE: return ERROR_INVALID_HANDLE;
      case STATUS_ACCESS_DENIED: return ERROR_ACCESS_DENIED;
      case STATUS_DEVICE_NOT_CONNECTED: return ERROR_DEVICE_NOT_CONNECTED;
      case STATUS_NOT_SUPPORTED: return 50;
      case STATUS_END_OF_FILE: return 38;
      case STATUS_BUFFER_TOO_SMALL: return 122;
    }
    throw new Error(`kernel status 0x${(status >>> 0).toString(16)} has no Win32 error`);
  }

  // Serves a kernel, XDK or C runtime call with its evaluated arguments, undefined where the name is not the console's
  call(fn, args, argIdx, env) {
    const d = this.d;
    const u32 = (v) => v >>> 0;
    const word = (p) => d.int(d.loadLive(env, p, 4));
    const storeWord = (p, v) => d.storeThrough(p, 4, u32(v), env);
    const at = (p, off) => d.offsetPointer(p, off);
    const storeU64 = (p, big) => { storeWord(p, Number(big & 0xFFFFFFFFn)); storeWord(at(p, 4), Number((big >> 32n) & 0xFFFFFFFFn)); };
    const loadU64 = (p) => BigInt(word(p)) | (BigInt(word(at(p, 4))) << 32n);
    const storeSystemTime = (p, date, utc) => {
      const words = utc ? [date.getUTCFullYear(), date.getUTCMonth() + 1, date.getUTCDay(), date.getUTCDate(), date.getUTCHours(), date.getUTCMinutes(), date.getUTCSeconds(), date.getUTCMilliseconds()] : [date.getFullYear(), date.getMonth() + 1, date.getDay(), date.getDate(), date.getHours(), date.getMinutes(), date.getSeconds(), date.getMilliseconds()];
      words.forEach((w, i) => d.storeThrough(at(p, 2 * i), 2, w & 0xFFFF, env));
    };
    const loadSystemTime = (p) => {
      const w = (i) => d.int(d.loadLive(env, at(p, 2 * i), 2));
      return Date.UTC(w(0), w(1) - 1, w(3), w(4), w(5), w(6), w(7));
    };
    const storeFindData = (p, e) => {
      storeWord(at(p, FIND_ATTRIBUTES), e.attributes);
      storeU64(at(p, FIND_CREATED), e.created);
      storeU64(at(p, FIND_ACCESSED), e.written);
      storeU64(at(p, FIND_WRITTEN), e.written);
      storeWord(at(p, FIND_SIZE_HIGH), 0);
      storeWord(at(p, FIND_SIZE_LOW), e.dir ? 0 : e.data.length);
      d.storeAnsi(at(p, FIND_NAME), e.name, env);
    };
    const ansiStringAt = (p) => {
      const length = d.int(d.loadLive(env, at(p, ANSI_LENGTH), 2));
      const buffer = d.loadLive(env, at(p, ANSI_BUFFER), 4);
      let out = '';
      for (let i = 0; i < length; i++) out += String.fromCharCode(d.int(d.loadLive(env, at(buffer, i), 1)) & 0xFF);
      return out;
    };
    const ntPathOf = (oa) => ansiStringAt(d.loadLive(env, at(oa, OA_NAME), 4));
    const missingStatus = (vol, error) => u32(!vol ? (error === ERROR_DEVICE_NOT_CONNECTED ? STATUS_DEVICE_NOT_CONNECTED : STATUS_OBJECT_PATH_NOT_FOUND) : STATUS_OBJECT_NAME_NOT_FOUND);
    const timeZoneBias = () => this.eepromWord(XC_TIMEZONE_BIAS) | 0;
    switch (fn) {
      // --- the last error and the kernel statuses behind it
      case 'GetLastError': return this.lastError;
      case 'SetLastError': this.lastError = d.int(args[0]); return 0;
      case 'RtlNtStatusToDosError': return this.dosError(d.int(args[0]));
      case 'XapiSetLastNTError': this.lastError = this.dosError(d.int(args[0])); return this.lastError;

      // --- files by their Win32 names
      case 'CreateFileA': return this.open(d.ansiString(args[0], env), d.int(args[1]), d.int(args[4]));
      case 'CreateFileW': return this.open(d.string(args[0]), d.int(args[1]), d.int(args[4]));
      case 'ReadFile': {
        const bytes = this.read(d.int(args[0]), d.int(args[2]));
        d.storeBytes(args[1], bytes, env);
        if (d.int(args[3]) !== 0) storeWord(args[3], bytes.length);
        return 1;
      }
      case 'WriteFile': {
        const n = this.write(d.int(args[0]), d.readBytes(args[1], d.int(args[2]), env));
        if (n < 0) return 0;
        if (d.int(args[3]) !== 0) storeWord(args[3], n);
        return 1;
      }
      case 'SetFilePointer': {
        if (d.int(args[2]) !== 0) throw new Error('file seek with a high distance word is not one the console serves');
        return this.seek(d.int(args[0]), d.int(args[1]) | 0, d.int(args[3]));
      }
      case 'GetFileSize': {
        if (d.int(args[1]) !== 0) storeWord(args[1], 0);
        return u32(this.file(d.int(args[0])).entry.data.length);
      }
      case 'GetFileTime': {
        const e = this.file(d.int(args[0])).entry;
        for (const [i, t] of [[1, e.created], [2, e.written], [3, e.written]]) if (d.int(args[i]) !== 0) storeU64(args[i], t);
        return 1;
      }
      case 'CloseHandle': return this.close(d.int(args[0]));
      case 'GetFileAttributesA': {
        const found = this.entry(d.ansiString(args[0], env));
        return found ? found.entry.attributes : INVALID_FILE_ATTRIBUTES;
      }
      case 'SetFileAttributesA': {
        const found = this.entry(d.ansiString(args[0], env));
        if (!found) return 0;
        if (!found.vol.writable) { this.lastError = ERROR_ACCESS_DENIED; return 0; }
        found.entry.attributes = d.int(args[1]);
        return 1;
      }
      case 'DeleteFileA': {
        const found = this.entry(d.ansiString(args[0], env));
        if (!found) return 0;
        if (!found.vol.writable || found.entry.dir) { this.lastError = ERROR_ACCESS_DENIED; return 0; }
        found.vol.entries.delete(found.key);
        return 1;
      }
      case 'CreateDirectoryA': {
        const { vol, key, error } = this.locate(d.ansiString(args[0], env));
        if (!vol) { this.lastError = error; return 0; }
        if (!vol.writable) { this.lastError = ERROR_ACCESS_DENIED; return 0; }
        if (vol.entries.has(key)) { this.lastError = ERROR_ALREADY_EXISTS; return 0; }
        const parent = key.includes('\\') ? key.slice(0, key.lastIndexOf('\\')) : '';
        if (!vol.entries.has(parent)) { this.lastError = ERROR_PATH_NOT_FOUND; return 0; }
        vol.entries.set(key, { name: key.split('\\').pop(), dir: true, attributes: FILE_ATTRIBUTE_DIRECTORY, data: null, created: this.now(), written: this.now() });
        return 1;
      }
      case 'RemoveDirectoryA': {
        const found = this.entry(d.ansiString(args[0], env));
        if (!found) return 0;
        if (!found.vol.writable || !found.entry.dir) { this.lastError = ERROR_ACCESS_DENIED; return 0; }
        for (const k of found.vol.entries.keys()) if (k.startsWith(found.key + '\\')) { this.lastError = ERROR_DIR_NOT_EMPTY; return 0; }
        found.vol.entries.delete(found.key);
        return 1;
      }
      case 'FindFirstFileA': {
        const found = this.findFirst(d.ansiString(args[0], env));
        if (!found) return INVALID_HANDLE;
        storeFindData(args[1], found.entry);
        return u32(found.handle);
      }
      case 'FindNextFileA': {
        const e = this.findNext(d.int(args[0]));
        if (!e) return 0;
        storeFindData(args[1], e);
        return 1;
      }
      case 'FindClose': return this.close(d.int(args[0]));
      case 'GetDiskFreeSpaceExA': {
        const { vol, error } = this.locate(d.ansiString(args[0], env));
        if (!vol) { this.lastError = error; return 0; }
        const free = BigInt(Math.max(vol.totalBytes - vol.usedBytes(), 0));
        for (const [i, v] of [[1, free], [2, BigInt(vol.totalBytes)], [3, free]]) if (d.int(args[i]) !== 0) storeU64(args[i], v);
        return 1;
      }
      case 'GlobalMemoryStatus': {
        const words = [32, 50, CONSOLE_MEMORY, CONSOLE_MEMORY / 2, 0, 0, CONSOLE_MEMORY, CONSOLE_MEMORY / 2];
        words.forEach((w, i) => storeWord(at(args[0], 4 * i), w));
        return 0;
      }

      // --- files by their kernel names
      case 'NtOpenFile': {
        const path = ntPathOf(args[2]);
        const { vol, key, error } = this.locate(path);
        const entry = vol ? vol.entries.get(key) : undefined;
        if (!entry) {
          const status = missingStatus(vol, error);
          storeWord(args[3], status);
          return status;
        }
        const handle = this.nextHandle++;
        this.handles.set(handle, { kind: entry.dir ? 'dir' : 'file', vol, key, entry, pos: 0, access: d.int(args[1]) });
        storeWord(args[0], handle);
        storeWord(at(args[3], IOSB_STATUS), STATUS_SUCCESS);
        storeWord(at(args[3], IOSB_INFORMATION), 1);
        return STATUS_SUCCESS;
      }
      case 'NtQueryFullAttributesFile': {
        const { vol, key, error } = this.locate(ntPathOf(args[0]));
        const entry = vol ? vol.entries.get(key) : undefined;
        if (!entry) return missingStatus(vol, error);
        for (const off of [NET_CREATED, NET_ACCESSED, NET_WRITTEN, NET_CHANGED]) storeU64(at(args[1], off), off === NET_CREATED ? entry.created : entry.written);
        const size = BigInt(entry.dir ? 0 : entry.data.length);
        storeU64(at(args[1], NET_ALLOCATION), size);
        storeU64(at(args[1], NET_END), size);
        storeWord(at(args[1], NET_ATTRIBUTES), entry.attributes);
        return STATUS_SUCCESS;
      }
      case 'NtClose': return this.handles.delete(d.int(args[0])) ? STATUS_SUCCESS : u32(STATUS_INVALID_HANDLE);
      case 'NtReadFile': {
        const f = this.file(d.int(args[0]));
        if (d.int(args[7]) !== 0) f.pos = Number(loadU64(args[7]));
        const bytes = this.read(d.int(args[0]), d.int(args[6]));
        d.storeBytes(args[5], bytes, env);
        const status = bytes.length === 0 && d.int(args[6]) !== 0 ? STATUS_END_OF_FILE : STATUS_SUCCESS;
        storeWord(at(args[4], IOSB_STATUS), status);
        storeWord(at(args[4], IOSB_INFORMATION), bytes.length);
        return u32(status);
      }
      case 'NtWriteFile': {
        const f = this.file(d.int(args[0]));
        if (d.int(args[7]) !== 0) f.pos = Number(loadU64(args[7]));
        const n = this.write(d.int(args[0]), d.readBytes(args[5], d.int(args[6]), env));
        const status = n < 0 ? STATUS_ACCESS_DENIED : STATUS_SUCCESS;
        storeWord(at(args[4], IOSB_STATUS), status);
        storeWord(at(args[4], IOSB_INFORMATION), Math.max(n, 0));
        return u32(status);
      }
      // No memory unit volume is ever mounted, so there is no volume whose label could be changed
      case 'NtFsControlFile': storeWord(at(args[4], IOSB_STATUS), STATUS_NOT_SUPPORTED); return u32(STATUS_NOT_SUPPORTED);
      // --- the drive letters, links the executable's startup makes and later looks up
      case 'IoCreateSymbolicLink': {
        const name = ansiStringAt(args[0]), target = ansiStringAt(args[1]);
        const letter = this.linkLetter(name);
        if (letter === null) throw new Error(`symbolic link ${name} is not a drive letter`);
        if (this.links.has(letter)) return u32(STATUS_OBJECT_NAME_COLLISION);
        this.links.set(letter, target.replace(/\//g, '\\').replace(/\\+$/, ''));
        return STATUS_SUCCESS;
      }
      case 'NtOpenSymbolicLinkObject': {
        const letter = this.linkLetter(ntPathOf(args[2]));
        const target = letter === null ? undefined : this.links.get(letter);
        if (target === undefined) return u32(STATUS_OBJECT_NAME_NOT_FOUND);
        const handle = this.nextHandle++;
        this.handles.set(handle, { kind: 'link', target });
        storeWord(args[0], handle);
        return STATUS_SUCCESS;
      }
      case 'NtQuerySymbolicLinkObject': {
        const link = this.handles.get(d.int(args[0]));
        if (!link || link.kind !== 'link') return u32(STATUS_INVALID_HANDLE);
        const maximum = d.int(d.loadLive(env, at(args[1], ANSI_MAXIMUM), 2));
        if (d.int(args[2]) !== 0) storeWord(args[2], link.target.length);
        if (link.target.length > maximum) return u32(STATUS_BUFFER_TOO_SMALL);
        const buffer = d.loadLive(env, at(args[1], ANSI_BUFFER), 4);
        for (let i = 0; i < link.target.length; i++) d.storeThrough(at(buffer, i), 1, link.target.charCodeAt(i) & 0xFF, env);
        d.storeThrough(at(args[1], ANSI_LENGTH), 2, link.target.length, env);
        return STATUS_SUCCESS;
      }

      // --- the C runtime's streams over the same files
      case '_wfopen': {
        const mode = d.string(args[1]);
        const writing = /[wa+]/.test(mode);
        const handle = this.open(d.string(args[0]), writing ? GENERIC_WRITE : 0, mode.startsWith('w') ? CREATE_ALWAYS : (mode.startsWith('a') ? OPEN_ALWAYS : OPEN_EXISTING));
        if (handle === INVALID_HANDLE) return 0;
        if (mode.startsWith('a')) this.seek(handle, 0, FILE_END);
        return { stream: handle };
      }
      case 'fprintf': {
        const stream = args[0] && args[0].stream;
        if (!stream) throw new Error('fprintf to a value which is no open stream');
        const text = d.formatAnsi(d.ansiString(args[1], env), args.slice(2), env);
        const bytes = Uint8Array.from(text, (c) => c.charCodeAt(0) & 0xFF);
        return this.write(stream, bytes) < 0 ? -1 : bytes.length;
      }
      case 'fclose': {
        const stream = args[0] && args[0].stream;
        if (!stream) throw new Error('fclose of a value which is no open stream');
        this.close(stream);
        return 0;
      }

      // --- the clock
      case 'GetSystemTime': storeSystemTime(args[0], new Date(this.vc.now()), true); return 0;
      case 'KeQuerySystemTime': storeU64(args[0], this.now()); return 0;
      case 'SystemTimeToFileTime': storeU64(args[1], fileTimeOf(loadSystemTime(args[0]))); return 1;
      case 'FileTimeToSystemTime': storeSystemTime(args[1], new Date(msOfFileTime(loadU64(args[0]))), true); return 1;
      case 'FileTimeToLocalFileTime': storeU64(args[1], loadU64(args[0]) - BigInt(timeZoneBias()) * 600000000n); return 1;
      case 'CompareFileTime': {
        const a = loadU64(args[0]), b = loadU64(args[1]);
        return a < b ? u32(-1) : (a > b ? 1 : 0);
      }
      case 'RtlTimeFieldsToTime': {
        const w = (i) => d.int(d.loadLive(env, at(args[0], 2 * i), 2));
        storeU64(args[1], fileTimeOf(Date.UTC(w(0), w(1) - 1, w(2), w(3), w(4), w(5), w(6))));
        return 1;
      }
      case 'RtlTimeToTimeFields': {
        const t = new Date(msOfFileTime(loadU64(args[0])));
        [t.getUTCFullYear(), t.getUTCMonth() + 1, t.getUTCDate(), t.getUTCHours(), t.getUTCMinutes(), t.getUTCSeconds(), t.getUTCMilliseconds(), t.getUTCDay()].forEach((w, i) => d.storeThrough(at(args[1], 2 * i), 2, w, env));
        return 0;
      }
      case 'NtSetSystemTime': {
        if (d.int(args[1]) !== 0) storeU64(args[1], this.now());
        this.vc.clockOffsetMs = msOfFileTime(loadU64(args[0])) - this.vc.realNow();
        return STATUS_SUCCESS;
      }
      case 'XapipGetTimeZoneBias': return u32(timeZoneBias());
      case 'XapipQueryTimeZoneInformation': {
        // The zone the EEPROM keeps, laid out as the bias, the standard name, date and bias, then the daylight ones
        const name = (id) => { const v = this.eeprom.get(id); return v ? Array.from(v.data).map((b) => String.fromCharCode(b)).join('').replace(/\0.*$/, '') : ''; };
        const putName = (off, s) => { for (let i = 0; i < 32; i++) d.storeThrough(at(args[0], off + 2 * i), 2, i < s.length ? s.charCodeAt(i) : 0, env); };
        const putDate = (off, packed) => { const parts = [0, packed & 255, (packed >>> 16) & 255, (packed >>> 8) & 255, (packed >>> 24) & 255, 0, 0, 0]; parts.forEach((w, i) => d.storeThrough(at(args[0], off + 2 * i), 2, w, env)); };
        storeWord(args[0], timeZoneBias());
        putName(4, name(XC_TZ_STD_NAME));
        putDate(68, this.eepromWord(XC_TZ_STD_DATE));
        storeWord(at(args[0], 84), this.eepromWord(XC_TZ_STD_BIAS));
        putName(88, name(XC_TZ_DLT_NAME));
        putDate(152, this.eepromWord(XC_TZ_DLT_DATE));
        storeWord(at(args[0], 168), this.eepromWord(XC_TZ_DLT_BIAS));
        return 1;
      }
      // The refurbishing record of a console never serviced holds no power cycles and no first boot time
      case 'ExReadWriteRefurbInfo': {
        if (d.int(args[2]) === 0) for (let i = 0; i < d.int(args[1]); i += 4) storeWord(at(args[0], i), 0);
        return STATUS_SUCCESS;
      }

      // --- the EEPROM
      case 'XQueryValue': {
        const value = this.eeprom.get(d.int(args[0]));
        if (!value) throw new Error(`EEPROM value ${d.int(args[0])} is not one the console keeps`);
        if (d.int(args[1]) !== 0) storeWord(args[1], value.type);
        const room = d.int(args[3]);
        if (room < value.data.length) { if (d.int(args[4]) !== 0) storeWord(args[4], value.data.length); return 122; }
        d.storeBytes(args[2], value.data, env);
        if (d.int(args[4]) !== 0) storeWord(args[4], value.data.length);
        return 0;
      }
      case 'ExSaveNonVolatileSetting': {
        const id = d.int(args[0]);
        if (!this.eeprom.has(id)) throw new Error(`EEPROM value ${id} is not one the console keeps`);
        this.eeprom.set(id, { type: d.int(args[1]), data: d.readBytes(args[2], d.int(args[3]), env) });
        return STATUS_SUCCESS;
      }
      case 'XGetGameRegion': return this.eepromWord(XC_FACTORY_GAME_REGION);
      case 'XGetAudioFlags': return this.eepromWord(XC_AUDIO);
      case 'XGetVideoFlags': return this.eepromWord(XC_VIDEO) >>> 16;

      // --- the utility drive and the file cache, which a disk kept in memory sizes freely
      case 'XMountUtilityDrive': {
        if (!this.links.has('z')) this.links.set('z', UTILITY_PARTITION_PATH);
        return 1;
      }
      case 'XSetFileCacheSize': return 1;

      // --- devices on the ports, none being inserted, and the drive
      case 'XPeekDevices': return 0;
      // No keyboard is plugged in, so the languages registered for one reach no device
      case 'XInputRegisterLanguages': return 0;
      case 'XGetDeviceEnumerationStatus': return 0;
      case 'XMountMUA': case 'XMountMURootA': case 'XUnmountMU': return ERROR_DEVICE_NOT_CONNECTED;
      case 'XMUNameFromDriveLetter': return ERROR_DEVICE_NOT_CONNECTED;
      case 'MU_CreateDeviceObject': return u32(STATUS_DEVICE_NOT_CONNECTED);
      case 'MU_CloseDeviceObject': return STATUS_SUCCESS;
      case 'XapiFormatFATVolume': return 0;
      case 'DeviceIoControl': this.lastError = ERROR_NOT_READY; return 0;
      case 'HalEnableSecureTrayEject': return 0;
      case 'AvSendTVEncoderOption': if (d.int(args[3]) !== 0) storeWord(args[3], 0); return 0;

      // --- soundtracks and content, of which the data partition holds none
      case 'XFindFirstSoundtrack': this.lastError = ERROR_NO_MORE_FILES; return INVALID_HANDLE;
      case 'XFindNextSoundtrack': this.lastError = ERROR_NO_MORE_FILES; return 0;
      case 'XFindClose': return 1;
      case 'XGetSoundtrackSongInfo': this.lastError = ERROR_FILE_NOT_FOUND; return 0;
      case 'XOpenSoundtrackSong': this.lastError = ERROR_FILE_NOT_FOUND; return INVALID_HANDLE;
      case 'XLoadContentSignaturesWithFileName': this.lastError = ERROR_FILE_NOT_FOUND; return 0;
      case 'XCloseContentSignatures': return 0;

      // --- the online library on a console with no link
      case 'XNetGetEthernetLinkStatus': return 0;
      case 'XNetQosRelease': return 0;
      case 'XOnlineGetUsers': if (d.int(args[1]) !== 0) storeWord(args[1], 0); return S_OK;
      case '_XOnlineGetUsersFromHD': if (d.int(args[1]) !== 0) storeWord(args[1], 0); return S_OK;
      case '_XOnlineGetUserFromMU': return XONLINE_E_NO_USER;
      case '_XOnlineAddUserToHD': case '_XOnlineSetUserInMU': case '_XOnlineRemoveUserFromHD': return E_FAIL;
      case 'XOnlineLogon': return XONLINE_E_LOGON_NO_NETWORK_CONNECTION;
      case 'XOnlineTaskContinue': return XONLINE_E_LOGON_NO_NETWORK_CONNECTION;
      case 'XOnlineTaskClose': case 'XOnlineNotificationSetState': case 'XOnlineFriendsEnumerateFinish': return S_OK;
      case 'XOnlineDashUpdate': this.vc.reboot(); return S_OK;

      // --- launching another image restarts the console, which is the page
      case 'XLaunchNewImageA': case 'XWriteTitleInfoAndRebootA': this.vc.reboot(); return 0;

      // --- one thread runs the dashboard, so every wait is satisfied at once and every lock is free
      case 'WaitForSingleObject': case 'SignalObjectAndWait': return 0;
      case 'ExAcquireReadWriteLockExclusive': case 'ExAcquireReadWriteLockShared': case 'ExReleaseReadWriteLock': return 0;
      case 'NtReleaseMutant': case 'NtSetEvent': if (d.int(args[1]) !== 0) storeWord(args[1], 0); return STATUS_SUCCESS;
      case 'D3D_BlockOnTime': return 0;

      // --- memory
      case 'VirtualAlloc': return d.heapObject(d.int(args[1]));
      case 'VirtualFree': return 1;
      // The kernel's contiguous allocation, within the acceptable physical range at the alignment asked for
      case 'MmAllocateContiguousMemoryEx': return d.memory.physical.allocate(d.int(args[0]), d.int(args[1]), d.int(args[2]), d.int(args[3]));
      case 'MmFreeContiguousMemory': d.memory.physical.free(d.int(args[0])); return 0;
      // The GPU's instance memory ends below the page frame database, which the claim reports as padding
      case 'MmClaimGpuInstanceMemory': {
        storeWord(args[1], PFN_DATABASE_BYTES);
        return u32(d.memory.physical.alias + CONSOLE_MEMORY - PFN_DATABASE_BYTES);
      }
      // The runtime starts the console cold, so no earlier program left its display settings behind
      case 'AvGetSavedDataAddress': return 0;

      // --- interrupts
      case 'HalGetInterruptVector': {
        const level = d.int(args[0]);
        d.storeThrough(args[1], 1, HIGHEST_BUS_LEVEL - level, env);
        return INTERRUPT_VECTOR_BASE + level;
      }
      // The runtime's GPU drains its channel at once and raises no interrupt, and the console never shuts down
      case 'KeInitializeInterrupt': case 'KeInitializeDpc': case 'HalRegisterShutdownNotification': return 0;
      case 'KeConnectInterrupt': return 1;

      // --- the bus and ports of the console's hardware, which hold what is written to them
      case 'HalReadWritePCISpace': {
        const key = `${d.int(args[0])}:${d.int(args[1])}`, reg = d.int(args[2]), length = d.int(args[4]);
        if (!this.pci.has(key)) this.pci.set(key, new Uint8Array(256));
        const space = this.pci.get(key);
        if (d.int(args[5]) !== 0) space.set(d.readBytes(args[3], length, env), reg);
        else d.storeBytes(args[3], space.subarray(reg, reg + length), env);
        return 0;
      }
      case 'port_out': this.ports.set(d.int(args[0]), d.int(args[1])); return 0;
      case 'port_in': return ((this.ports.get(d.int(args[0])) ?? 0) & (d.int(args[1]) === 1 ? 0xFF : d.int(args[1]) === 2 ? 0xFFFF : 0xFFFFFFFF)) >>> 0;
      case 'IsBadStringPtrA': case 'IsBadWritePtr': return 0;

      // --- the C runtime pieces the libraries leave to the console
      case 'rand': return this.rand();
      case 'srand': this.randState = d.int(args[0]); return 0;
      case 'toupper': { const c = d.int(args[0]) & 0xFF; return c >= 97 && c <= 122 ? c - 32 : c; }
      case 'CharUpperW': d.storeString(args[0], d.string(args[0]).toUpperCase(), env); return args[0];
      case '_stricmp': { const a = d.ansiString(args[0], env).toLowerCase(), b = d.ansiString(args[1], env).toLowerCase(); return a === b ? 0 : (a < b ? u32(-1) : 1); }
      case '_strnicmp': { const n = d.int(args[2]); const a = d.ansiString(args[0], env).slice(0, n).toLowerCase(), b = d.ansiString(args[1], env).slice(0, n).toLowerCase(); return a === b ? 0 : (a < b ? u32(-1) : 1); }
      case 'lstrcpynW': { const s = d.string(args[1]).slice(0, Math.max(d.int(args[2]) - 1, 0)); d.storeString(args[0], s, env); return args[0]; }
      case 'sprintf': { const s = d.formatAnsi(d.ansiString(args[1], env), args.slice(2), env); d.storeAnsi(args[0], s, env); return s.length; }
      case '_snprintf': { const s = d.formatAnsi(d.ansiString(args[2], env), args.slice(3), env).slice(0, d.int(args[1])); d.storeAnsi(args[0], s, env); return s.length; }
      case 'vswprintf': {
        // The argument list is the stack words the caller's variable arguments occupy, read as the format needs them
        const list = [];
        for (let i = 0; i < 16; i++) list.push(d.loadLive(env, at(args[3], 4 * i), 4));
        const s = d.formatWide(d.string(args[2]), list, env).slice(0, d.int(args[1]));
        d.storeString(args[0], s, env);
        return s.length;
      }
      case 'MultiByteToWideChar': {
        const length = d.int(args[3]) | 0;
        const src = length < 0 ? d.ansiString(args[2], env) + '\0' : Array.from(d.readBytes(args[2], length, env), (b) => String.fromCharCode(b)).join('');
        if (d.int(args[5]) === 0) return src.length;
        const room = d.int(args[5]);
        if (src.length > room) { this.lastError = 122; return 0; }
        for (let i = 0; i < src.length; i++) d.storeThrough(at(args[4], 2 * i), 2, src.charCodeAt(i), env);
        return src.length;
      }
      case 'DbgPrint': console.log('[DbgPrint]', d.formatAnsi(d.ansiString(args[0], env), args.slice(1), env)); return 0;
    }
    return undefined;
  }

  // The high word of the EEPROM video flags, as the display shape comes from it
  videoFlags() {
    return this.eepromWord(XC_VIDEO) >>> 16;
  }
}
