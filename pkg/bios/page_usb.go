package bios

import (
	"fmt"
	"strings"
)

// Endpoint descriptors a list may hold before the page treats it as never ending, transfer descriptors one
// endpoint moves a frame and frames caught up per advance
const (
	usbListLimit         = 1024
	usbTransfersPerFrame = 8
	usbFramesPerAdvance  = 8
)

// The host controller as the page runs it, its lists walked every millisecond of the real clock, each transfer
// descriptor moved whole between its buffer and the device at the endpoint's address, completed descriptors
// queued to the communications area, the root hub holding the controller on the port the console wires its
// first controller port to, behind the daughterboard's hub on a console that carries one, every device sharing
// one control endpoint answering the standard requests, the hub answering the hub class requests and reporting
// its ports' changes, and the controller answering the XID descriptor, capabilities and reports with what the
// bridge's keys and gamepads hold
const usbScript = `
function controllerReport() {
  const r = new Uint8Array(XID_INPUT_BYTES);
  r[1] = XID_INPUT_BYTES;
  let buttons = 0;
  const analog = new Float32Array(8), axes = new Float32Array(4);
  const control = (name, value) => {
    const button = XID_BUTTONS.get(name);
    if (button !== undefined) { if (value > 0.5) buttons |= button; return; }
    const index = XID_ANALOG.get(name);
    if (index !== undefined) { analog[index] = Math.max(analog[index], value); return; }
    const axis = XID_AXES.get(name.slice(0, 2));
    if (axis === undefined) fault("control " + name + " of the controller");
    const signed = name.endsWith("-") ? -value : value;
    if (Math.abs(signed) > Math.abs(axes[axis])) axes[axis] = signed;
  };
  for (const code of bridge.keys()) { const name = CONTROLLER_KEYS.get(code); if (name !== undefined) control(name, 1); }
  for (const g of bridge.gamepads()) {
    for (let i = 0; i < g.buttons.length && i < GAMEPAD_BUTTONS.length; i++) control(GAMEPAD_BUTTONS[i], g.buttons[i].value);
    for (let i = 0; i < g.axes.length && i < GAMEPAD_AXES.length; i++) { const v = (i & 1) !== 0 ? -g.axes[i] : g.axes[i]; if (Math.abs(v) > Math.abs(axes[i])) axes[i] = v; }
  }
  r[XID_BUTTONS_AT] = buttons & 0xFF; r[XID_BUTTONS_AT + 1] = buttons >>> 8;
  for (let i = 0; i < 8; i++) r[XID_ANALOG_AT + i] = Math.round(Math.min(1, analog[i]) * XID_ANALOG_MAX);
  for (let i = 0; i < 4; i++) { const v = Math.round(Math.max(-1, Math.min(1, axes[i])) * XID_THUMB_MAX); r[XID_THUMB_AT + 2 * i] = v & 0xFF; r[XID_THUMB_AT + 2 * i + 1] = (v >> 8) & 0xFF; }
  return r;
}
function usbDevice(kind) {
  const d = { address: 0, configuration: 0, setup: null, data: null, at: 0, zero: false, out: null, outAt: 0, stalled: false };
  const le16 = (b, i) => b[i] | (b[i + 1] << 8);
  function standard(s) {
    const inbound = (s.type & USB_IN) !== 0, descriptor = s.value >>> 8;
    switch (s.request) {
    case USB_GET_STATUS: return inbound ? [0, 0] : null;
    case USB_CLEAR_FEATURE: case USB_SET_FEATURE: case USB_SET_ADDRESS: case USB_SET_CONFIGURATION: case USB_SET_INTERFACE: return inbound ? null : [];
    case USB_GET_DESCRIPTOR: if (!inbound) return null; if (descriptor === USB_DESCRIPTOR_DEVICE) return kind.device; if (descriptor === USB_DESCRIPTOR_CONFIG) return kind.config; return null;
    case USB_GET_CONFIGURATION: return inbound ? [d.configuration] : null;
    case USB_GET_INTERFACE: return inbound ? [0] : null;
    }
    return null;
  }
  function finish() {
    const s = d.setup, type = s.type & USB_TYPE_MASK;
    if (type === USB_TYPE_STANDARD && s.request === USB_SET_ADDRESS) d.address = s.value & 0x7F;
    if (type === USB_TYPE_STANDARD && s.request === USB_SET_CONFIGURATION) d.configuration = s.value & 0xFF;
    if (d.out !== null) kind.take(s, d.out);
    d.setup = null;
  }
  return {
    reset() { d.address = 0; d.configuration = 0; d.setup = null; d.data = null; d.stalled = false; kind.reset(); },
    answers(address) { return address === d.address; },
    children: kind.children,
    advance: kind.advance,
    setup(bytes) {
      const s = { type: bytes[0], request: bytes[1], value: le16(bytes, 2), index: le16(bytes, 4), length: le16(bytes, 6) };
      d.setup = s; d.at = 0; d.out = null; d.outAt = 0; d.zero = false;
      const answer = (s.type & USB_TYPE_MASK) === USB_TYPE_STANDARD && (s.type & USB_RECIPIENT_MASK) !== USB_RECIPIENT_OTHER ? standard(s) : kind.request(s);
      d.stalled = answer === null;
      if (d.stalled) { d.data = null; return "stall"; }
      d.data = Uint8Array.from(answer).subarray(0, Math.min(answer.length, s.length));
      return "ack";
    },
    in(endpoint, max) {
      if (endpoint !== 0) return kind.in(endpoint, max);
      if (d.stalled || d.setup === null) return "stall";
      const s = d.setup;
      if ((s.type & USB_IN) !== 0) {
        if (d.at < d.data.length) { const chunk = d.data.subarray(d.at, Math.min(d.data.length, d.at + max)); d.at += chunk.length; d.zero = d.at === d.data.length && chunk.length === max && d.data.length < s.length; return chunk; }
        if (d.zero) { d.zero = false; return new Uint8Array(0); }
        d.setup = null;
        return "stall";
      }
      if (s.length > 0 && d.outAt < s.length) return "nak";
      finish();
      return new Uint8Array(0);
    },
    out(endpoint, bytes) {
      if (endpoint !== 0) return kind.out(endpoint, bytes);
      if (d.stalled || d.setup === null) return "stall";
      const s = d.setup;
      if ((s.type & USB_IN) === 0 && s.length > 0) { if (d.out === null) d.out = new Uint8Array(s.length); const n = Math.min(bytes.length, s.length - d.outAt); d.out.set(bytes.subarray(0, n), d.outAt); d.outAt += n; return "ack"; }
      d.setup = null;
      return "ack";
    },
  };
}
function gamepadDevice() {
  const rumble = new Uint8Array(XID_OUTPUT_BYTES);
  const capabilities = (n) => { const c = new Uint8Array(n).fill(0xFF); c[0] = 0; c[1] = n; return c; };
  return usbDevice({
    device: GAMEPAD_DEVICE, config: GAMEPAD_CONFIG,
    request(s) {
      const type = s.type & USB_TYPE_MASK, inbound = (s.type & USB_IN) !== 0, descriptor = s.value >>> 8;
      if (inbound && s.request === USB_GET_DESCRIPTOR && descriptor === XID_DESCRIPTOR_TYPE) return GAMEPAD_XID;
      if (type === USB_TYPE_VENDOR && inbound && s.request === XID_GET_CAPABILITIES) return s.value === XID_REPORT_INPUT ? capabilities(XID_INPUT_BYTES) : s.value === XID_REPORT_OUTPUT ? capabilities(XID_OUTPUT_BYTES) : null;
      if (type === USB_TYPE_CLASS && inbound && s.request === XID_GET_REPORT && s.value === XID_REPORT_INPUT) return controllerReport();
      if (type === USB_TYPE_CLASS && !inbound && s.request === XID_SET_REPORT && s.value === XID_REPORT_OUTPUT && s.length === XID_OUTPUT_BYTES) return [];
      return null;
    },
    take(s, bytes) { rumble.set(bytes.subarray(0, XID_OUTPUT_BYTES)); },
    in(endpoint, max) { return endpoint === XID_ENDPOINT ? controllerReport().subarray(0, max) : "stall"; },
    out(endpoint, bytes) { if (endpoint !== XID_ENDPOINT) return "stall"; rumble.set(bytes.subarray(0, XID_OUTPUT_BYTES)); return "ack"; },
    reset() {},
    children() { return []; },
    advance(now) {},
  });
}
function hubDevice(devices) {
  const ports = devices.map((device) => ({ device, status: HUB_PORT_POWERED | (device !== null ? HUB_PORT_CONNECTED : 0), change: device !== null ? HUB_CHANGE_CONNECTED : 0, resetAt: 0 }));
  const portOf = (s) => { const i = s.index & 0xFF; if (i < 1 || i > ports.length) return null; return ports[i - 1]; };
  function portFeature(s, set) {
    const p = portOf(s);
    if (p === null) return null;
    switch (s.value) {
    case HUB_FEATURE_PORT_ENABLE: if (set) { if ((p.status & HUB_PORT_CONNECTED) !== 0) p.status |= HUB_PORT_ENABLED; } else p.status &= ~HUB_PORT_ENABLED; return [];
    case HUB_FEATURE_PORT_SUSPEND: if (set) p.status |= HUB_PORT_SUSPENDED; else if ((p.status & HUB_PORT_SUSPENDED) !== 0) { p.status &= ~HUB_PORT_SUSPENDED; p.change |= HUB_CHANGE_SUSPENDED; } return [];
    case HUB_FEATURE_PORT_RESET: if (!set) return []; if ((p.status & HUB_PORT_CONNECTED) !== 0) { p.status |= HUB_PORT_RESET; p.resetAt = hwElapsedMs() + PORT_RESET_MS; } return [];
    case HUB_FEATURE_PORT_POWER: if (set && (p.status & HUB_PORT_CONNECTED) !== 0) p.change |= HUB_CHANGE_CONNECTED; return [];
    }
    if (!set && s.value >= HUB_FEATURE_CHANGE_BASE && s.value < HUB_FEATURE_CHANGE_BASE + 5) { p.change &= ~(1 << (s.value - HUB_FEATURE_CHANGE_BASE)); return []; }
    return null;
  }
  return usbDevice({
    device: HUB_DEVICE, config: HUB_CONFIG,
    request(s) {
      const inbound = (s.type & USB_IN) !== 0, recipient = s.type & ~USB_IN;
      if (recipient === HUB_RECIPIENT_HUB && inbound && s.request === USB_GET_DESCRIPTOR && (s.value >>> 8) === HUB_DESCRIPTOR_TYPE) return HUB_CLASS;
      if (recipient === HUB_RECIPIENT_HUB && inbound && s.request === USB_GET_STATUS) return [0, 0, 0, 0];
      if (recipient === HUB_RECIPIENT_HUB && !inbound && (s.request === USB_CLEAR_FEATURE || s.request === USB_SET_FEATURE)) return [];
      if (recipient === HUB_RECIPIENT_PORT && inbound && s.request === USB_GET_STATUS) { const p = portOf(s); return p === null ? null : [p.status & 0xFF, p.status >>> 8, p.change & 0xFF, p.change >>> 8]; }
      if (recipient === HUB_RECIPIENT_PORT && !inbound && s.request === USB_SET_FEATURE) return portFeature(s, true);
      if (recipient === HUB_RECIPIENT_PORT && !inbound && s.request === USB_CLEAR_FEATURE) return portFeature(s, false);
      return null;
    },
    take(s, bytes) {},
    in(endpoint, max) {
      if (endpoint !== HUB_ENDPOINT) return "stall";
      let bits = 0;
      ports.forEach((p, i) => { if (p.change !== 0) bits |= 1 << (i + 1); });
      return bits === 0 ? "nak" : Uint8Array.from([bits]).subarray(0, max);
    },
    out(endpoint, bytes) { return "stall"; },
    reset() { for (const p of ports) { p.status &= ~(HUB_PORT_ENABLED | HUB_PORT_SUSPENDED | HUB_PORT_RESET); p.change = p.device !== null ? HUB_CHANGE_CONNECTED : 0; p.resetAt = 0; if (p.device !== null) p.device.reset(); } },
    children() { const out = []; for (const p of ports) if (p.device !== null && (p.status & HUB_PORT_ENABLED) !== 0) out.push(p.device); return out; },
    advance(now) { for (const p of ports) if (p.resetAt !== 0 && now >= p.resetAt) { p.resetAt = 0; p.status = (p.status & ~HUB_PORT_RESET) | HUB_PORT_ENABLED; p.change |= HUB_CHANGE_RESET; if (p.device !== null) p.device.reset(); } },
  });
}
function usbModel(w) {
  const inner = registerModel(w);
  const u = shared(windowKey(w) + ":usb", () => {
    const ports = [], behind = [];
    for (let i = 0; i < HUB_PORTS; i++) behind.push(i === CONTROLLER_PORT - 1 ? gamepadDevice() : null);
    for (let i = 0; i < USB_PORTS; i++) ports.push({ status: PORT_POWERED, resetAt: 0, device: TABLES.console.usb_hub ? (i === DAUGHTERBOARD_PORT - 1 ? hubDevice(behind) : null) : (i === CONTROLLER_PORT - 1 ? gamepadDevice() : null) });
    for (const p of ports) if (p.device !== null) p.status |= PORT_CONNECTED;
    return { control: 0, command: 0, status: 0, enable: 0, hcca: 0, periodCurrent: 0, controlHead: 0, controlCurrent: 0, bulkHead: 0, bulkCurrent: 0, done: 0, interval: HC_FRAME_INTERVAL_RESET, number: 0, periodicStart: 0, threshold: 0, descriptorA: RH_DESCRIPTOR_A_RESET, descriptorB: 0, hub: 0, ports, irq: -1, nextAt: hwElapsedMs() + USB_FRAME_MS, advance: null };
  });
  for (const i of TABLES.hardware.interrupts) if (i.registers.includes(w.base) || i.registers.some((r) => r >= w.base && r < w.limit)) u.irq = i.irq;
  const state = () => (u.control & HC_STATE_MASK) >>> HC_STATE_SHIFT;
  function updateLine() {
    const pending = (u.enable & HC_INTERRUPT_MASTER) !== 0 && (u.status & u.enable & ~HC_INTERRUPT_MASTER) !== 0;
    if (u.irq < 0) { if (pending) fault("the host controller at " + hex(w.base) + " raises an interrupt on no line the executable connects"); return; }
    setLine(u.irq, pending);
  }
  function deviceAt(address) {
    for (const p of u.ports) {
      if (p.device === null || (p.status & PORT_ENABLED) === 0) continue;
      if (p.device.answers(address)) return p.device;
      for (const child of p.device.children()) if (child.answers(address)) return child;
    }
    return null;
  }
  function serve(ed, flags, headP) {
    const td = (headP & HC_POINTER_MASK) >>> 0, fa = flags & ED_FUNCTION_MASK, en = (flags >>> ED_ENDPOINT_SHIFT) & ED_ENDPOINT_MASK, mps = (flags >>> ED_MPS_SHIFT) & ED_MPS_MASK;
    if ((flags & ED_ISOCHRONOUS) !== 0) fault("an isochronous endpoint of the device at address " + fa + " on the host controller");
    const tdFlags = physRead(td, 4), cbp = physRead(td + TD_CURRENT, 4), nextTD = physRead(td + TD_NEXT, 4), be = physRead(td + TD_END, 4);
    let direction = (flags >>> ED_DIRECTION_SHIFT) & ED_DIRECTION_MASK;
    if (direction !== TD_OUT && direction !== TD_IN) direction = (tdFlags >>> TD_DIRECTION_SHIFT) & TD_DIRECTION_MASK;
    const toggleField = (tdFlags >>> TD_TOGGLE_SHIFT) & TD_TOGGLE_MASK;
    let toggle = (toggleField & TD_TOGGLE_FROM_TD) !== 0 ? toggleField & 1 : (headP >>> 1) & 1;
    const first = TD_PAGE_MASK + 1 - (cbp & TD_PAGE_MASK);
    const length = cbp === 0 ? 0 : ((cbp & ~TD_PAGE_MASK) === (be & ~TD_PAGE_MASK) ? be - cbp + 1 : first + (be & TD_PAGE_MASK) + 1);
    const at = (i) => i < first ? (cbp + i) >>> 0 : ((be & ~TD_PAGE_MASK) + (i - first)) >>> 0;
    const device = deviceAt(fa);
    let moved = 0, code = CC_NO_ERROR, halt = false;
    if (device === null) { code = CC_NOT_RESPONDING; halt = true; }
    else if (direction === TD_IN) {
      let short = false;
      for (;;) {
        const want = Math.min(mps, length - moved), got = device.in(en, want);
        if (got === "nak") { if (moved === 0) return -1; break; }
        if (got === "stall") { code = CC_STALL; halt = true; break; }
        for (let i = 0; i < got.length; i++) physWrite(at(moved + i), 1, got[i]);
        moved += got.length; toggle ^= 1;
        if (got.length < want) { short = true; break; }
        if (moved >= length) break;
      }
      if (short && (tdFlags & TD_BUFFER_ROUNDING) === 0 && code === CC_NO_ERROR) { code = CC_DATA_UNDERRUN; halt = true; }
    } else {
      const bytes = new Uint8Array(length);
      for (let i = 0; i < length; i++) bytes[i] = physRead(at(i), 1);
      if (direction === TD_SETUP) { if (device.setup(bytes) === "stall") { code = CC_STALL; halt = true; } else { moved = length; toggle ^= 1; } }
      else do {
        const n = Math.min(mps, length - moved), r = device.out(en, bytes.subarray(moved, moved + n));
        if (r === "nak") { if (moved === 0) return -1; break; }
        if (r === "stall") { code = CC_STALL; halt = true; break; }
        moved += n; toggle ^= 1;
      } while (moved < length);
    }
    physWrite(td, 4, ((tdFlags & ~((TD_CONDITION_MASK << TD_CONDITION_SHIFT) | (TD_ERROR_COUNT_MASK << TD_ERROR_COUNT_SHIFT))) | (code << TD_CONDITION_SHIFT)) >>> 0);
    physWrite(td + TD_CURRENT, 4, moved >= length ? 0 : at(moved));
    const head = ((nextTD & HC_POINTER_MASK) | (toggle << 1) | (halt ? ED_HALTED : 0)) >>> 0;
    physWrite(ed + ED_HEAD, 4, head);
    physWrite(td + TD_NEXT, 4, u.done);
    u.done = td;
    return halt ? -1 : head;
  }
  function walk(head, perEndpoint) {
    let ed = (head & HC_POINTER_MASK) >>> 0, found = false;
    for (let n = 0; ed !== 0; n++) {
      if (n > USB_LIST_LIMIT) fault("the endpoint list at " + hex(head) + " of the host controller never ends");
      const flags = physRead(ed, 4), tail = (physRead(ed + ED_TAIL, 4) & HC_POINTER_MASK) >>> 0;
      let headP = physRead(ed + ED_HEAD, 4);
      if ((flags & ED_SKIP) === 0) for (let i = 0; i < perEndpoint && (headP & HC_POINTER_MASK) >>> 0 !== tail && (headP & ED_HALTED) === 0; i++) {
        found = true;
        const next = serve(ed, flags, headP);
        if (next < 0) break;
        headP = next;
      }
      ed = (physRead(ed + ED_NEXT, 4) & HC_POINTER_MASK) >>> 0;
    }
    return found;
  }
  function frame(now) {
    for (const p of u.ports) {
      if (p.resetAt !== 0 && now >= p.resetAt) { p.resetAt = 0; p.status = ((p.status & ~PORT_RESET) | PORT_ENABLED | PORT_RESET_CHANGE) >>> 0; if (p.device !== null) p.device.reset(); u.status |= HC_INTERRUPT_ROOT_HUB; }
      if (p.device !== null) p.device.advance(now);
    }
    if (state() !== HC_STATE_OPERATIONAL) { updateLine(); return; }
    const was = u.number;
    u.number = (u.number + 1) & HC_FRAME_NUMBER_MASK;
    let raised = HC_INTERRUPT_FRAME;
    if (((u.number ^ was) & HC_FRAME_NUMBER_HALF) !== 0) raised |= HC_INTERRUPT_OVERFLOW;
    if (u.hcca !== 0) { physWrite((u.hcca + HCCA_FRAME_NUMBER) >>> 0, 2, u.number); physWrite((u.hcca + HCCA_FRAME_NUMBER + 2) >>> 0, 2, 0); }
    if ((u.control & HC_PERIODIC_ENABLE) !== 0 && u.hcca !== 0) walk(physRead((u.hcca + 4 * (u.number % HCCA_INTERRUPT_SLOTS)) >>> 0, 4), 1);
    if ((u.control & HC_CONTROL_ENABLE) !== 0 && !walk(u.controlHead, USB_TRANSFERS_PER_FRAME)) u.command &= ~HC_CONTROL_FILLED;
    if ((u.control & HC_BULK_ENABLE) !== 0 && !walk(u.bulkHead, USB_TRANSFERS_PER_FRAME)) u.command &= ~HC_BULK_FILLED;
    if (u.done !== 0 && (u.status & HC_INTERRUPT_DONE_HEAD) === 0 && u.hcca !== 0) {
      physWrite((u.hcca + HCCA_DONE_HEAD) >>> 0, 4, (u.done | (((u.status | raised) & u.enable & ~HC_INTERRUPT_MASTER) !== 0 ? 1 : 0)) >>> 0);
      u.done = 0;
      raised |= HC_INTERRUPT_DONE_HEAD;
    }
    u.status = (u.status | raised) >>> 0;
    updateLine();
  }
  function frames(now) {
    let n = 0;
    while (now >= u.nextAt && n < USB_FRAMES_PER_ADVANCE) { u.nextAt += USB_FRAME_MS; frame(now); n++; }
    if (now >= u.nextAt) u.nextAt = now + USB_FRAME_MS;
  }
  if (u.advance === null) { u.advance = frames; u.slack = USB_FRAME_MS * (USB_FRAMES_PER_ADVANCE / 2); TIMED.push(u); }
  function reset() {
    u.control = (u.control & ~HC_STATE_MASK) >>> 0; u.status = 0; u.enable = 0; u.hcca = 0; u.periodCurrent = 0; u.controlHead = 0; u.controlCurrent = 0; u.bulkHead = 0; u.bulkCurrent = 0; u.done = 0; u.interval = HC_FRAME_INTERVAL_RESET; u.number = 0; u.periodicStart = 0;
  }
  function remaining() {
    const interval = u.interval & HC_FRAME_INTERVAL_MASK, elapsed = Math.min(1, Math.max(0, (hwElapsedMs() - (u.nextAt - USB_FRAME_MS)) / USB_FRAME_MS));
    return (Math.floor(interval * (1 - elapsed)) | (u.interval & HC_FRAME_REMAIN_TOGGLE)) >>> 0;
  }
  function portWrite(n, v) {
    const p = u.ports[n], was = p.status;
    if ((v & PORT_CONNECTED) !== 0) p.status &= ~PORT_ENABLED;
    if ((v & PORT_ENABLED) !== 0) { if ((p.status & PORT_CONNECTED) !== 0) p.status |= PORT_ENABLED; else p.status |= PORT_CONNECT_CHANGE; }
    if ((v & PORT_SUSPENDED) !== 0) { if ((p.status & PORT_CONNECTED) !== 0) p.status |= PORT_SUSPENDED; else p.status |= PORT_CONNECT_CHANGE; }
    if ((v & PORT_CLEAR_SUSPEND) !== 0 && (p.status & PORT_SUSPENDED) !== 0) p.status = (p.status & ~PORT_SUSPENDED) | PORT_SUSPEND_CHANGE;
    if ((v & PORT_RESET) !== 0) { if ((p.status & PORT_CONNECTED) !== 0) { p.status |= PORT_RESET; p.resetAt = hwElapsedMs() + PORT_RESET_MS; } else p.status |= PORT_CONNECT_CHANGE; }
    p.status = (p.status & ~(v & PORT_CHANGE_MASK)) >>> 0;
    if ((p.status & PORT_CHANGE_MASK) !== 0 && (p.status & PORT_CHANGE_MASK) !== (was & PORT_CHANGE_MASK)) u.status |= HC_INTERRUPT_ROOT_HUB;
    updateLine();
  }
  function read(at) {
    switch (at) {
    case HC.revision: return HC_REVISION;
    case HC.control: return u.control;
    case HC.commandStatus: return u.command;
    case HC.interruptStatus: return u.status;
    case HC.interruptEnable: case HC.interruptDisable: return u.enable;
    case HC.hcca: return u.hcca;
    case HC.periodCurrentED: return u.periodCurrent;
    case HC.controlHeadED: return u.controlHead;
    case HC.controlCurrentED: return u.controlCurrent;
    case HC.bulkHeadED: return u.bulkHead;
    case HC.bulkCurrentED: return u.bulkCurrent;
    case HC.doneHead: return u.done;
    case HC.fmInterval: return u.interval;
    case HC.fmRemaining: return remaining();
    case HC.fmNumber: return u.number;
    case HC.periodicStart: return u.periodicStart;
    case HC.lsThreshold: return u.threshold;
    case HC.rhDescriptorA: return u.descriptorA;
    case HC.rhDescriptorB: return u.descriptorB;
    case HC.rhStatus: return u.hub;
    }
    return u.ports[(at - HC.rhPortStatus) >> 2].status;
  }
  function write(at, v) {
    switch (at) {
    case HC.control: u.control = v; if (state() === HC_STATE_OPERATIONAL) u.nextAt = hwElapsedMs() + USB_FRAME_MS; updateLine(); return;
    case HC.commandStatus: if ((v & HC_COMMAND_RESET) !== 0) reset(); u.command = ((u.command | v) & ~HC_COMMAND_RESET) >>> 0; return;
    case HC.interruptStatus: u.status = (u.status & ~v) >>> 0; updateLine(); return;
    case HC.interruptEnable: u.enable = (u.enable | v) >>> 0; updateLine(); return;
    case HC.interruptDisable: u.enable = (u.enable & ~v) >>> 0; updateLine(); return;
    case HC.hcca: u.hcca = (v & HC_HCCA_MASK) >>> 0; return;
    case HC.periodCurrentED: u.periodCurrent = (v & HC_POINTER_MASK) >>> 0; return;
    case HC.controlHeadED: u.controlHead = (v & HC_POINTER_MASK) >>> 0; return;
    case HC.controlCurrentED: u.controlCurrent = (v & HC_POINTER_MASK) >>> 0; return;
    case HC.bulkHeadED: u.bulkHead = (v & HC_POINTER_MASK) >>> 0; return;
    case HC.bulkCurrentED: u.bulkCurrent = (v & HC_POINTER_MASK) >>> 0; return;
    case HC.doneHead: return;
    case HC.fmInterval: u.interval = v; return;
    case HC.fmRemaining: return;
    case HC.fmNumber: return;
    case HC.periodicStart: u.periodicStart = v; return;
    case HC.lsThreshold: u.threshold = v; return;
    case HC.rhDescriptorA: u.descriptorA = ((v & RH_DESCRIPTOR_A_WRITTEN) | USB_PORTS) >>> 0; return;
    case HC.rhDescriptorB: u.descriptorB = v; return;
    case HC.rhStatus: if ((v & RH_SET_REMOTE_WAKEUP) !== 0) u.hub |= RH_REMOTE_WAKEUP; if ((v & RH_CLEAR_REMOTE_WAKEUP) !== 0) u.hub &= ~RH_REMOTE_WAKEUP; return;
    }
    portWrite((at - HC.rhPortStatus) >> 2, v);
  }
  const own = (at) => at < HC.rhPortStatus + 4 * USB_PORTS;
  return {
    read(off, size) { const at = off & ~3; if (!own(at)) return inner.read(off, size); const word = read(at) >>> 0; return size === 4 ? word : (word >>> (8 * (off & 3))) & (size === 1 ? 0xFF : 0xFFFF); },
    write(off, size, v) { const at = off & ~3; if (!own(at)) { inner.write(off, size, v); return; } if (size !== 4) fault("write of " + size + " bytes to register " + hex(at) + " of the host controller"); write(at, v >>> 0); },
  };
}
`

// Writes the host controller's register layout and bits, the descriptor and transfer fields, the controller's
// descriptors and report layout and the bridge's keys and gamepads standing in for it, then the model itself
func (e *emitter) writeUSB() {
	e.js("// ---- the host controller and the controller on its root hub ----")
	e.js("const HC = { revision: 0x%X, control: 0x%X, commandStatus: 0x%X, interruptStatus: 0x%X, interruptEnable: 0x%X, interruptDisable: 0x%X, hcca: 0x%X, periodCurrentED: 0x%X, controlHeadED: 0x%X, controlCurrentED: 0x%X, bulkHeadED: 0x%X, bulkCurrentED: 0x%X, doneHead: 0x%X, fmInterval: 0x%X, fmRemaining: 0x%X, fmNumber: 0x%X, periodicStart: 0x%X, lsThreshold: 0x%X, rhDescriptorA: 0x%X, rhDescriptorB: 0x%X, rhStatus: 0x%X, rhPortStatus: 0x%X };",
		hcRevision, hcControl, hcCommandStatus, hcInterruptStatus, hcInterruptEnable, hcInterruptDisable, hcHCCA, hcPeriodCurrentED, hcControlHeadED, hcControlCurrentED, hcBulkHeadED, hcBulkCurrentED, hcDoneHead, hcFmInterval, hcFmRemaining, hcFmNumber, hcPeriodicStart, hcLSThreshold, hcRhDescriptorA, hcRhDescriptorB, hcRhStatus, hcRhPortStatus)
	e.js("const HC_REVISION = 0x%X, HC_PERIODIC_ENABLE = 0x%X, HC_CONTROL_ENABLE = 0x%X, HC_BULK_ENABLE = 0x%X, HC_STATE_MASK = 0x%X, HC_STATE_SHIFT = %d, HC_STATE_OPERATIONAL = %d, HC_COMMAND_RESET = 0x%X, HC_CONTROL_FILLED = 0x%X, HC_BULK_FILLED = 0x%X;",
		hcRevisionValue, hcPeriodicEnable, hcControlEnable, hcBulkEnable, hcStateMask, hcStateShift, hcStateOperational, hcCommandReset, hcControlFilled, hcBulkFilled)
	e.js("const HC_INTERRUPT_DONE_HEAD = 0x%X, HC_INTERRUPT_FRAME = 0x%X, HC_INTERRUPT_OVERFLOW = 0x%X, HC_INTERRUPT_ROOT_HUB = 0x%X, HC_INTERRUPT_MASTER = 0x%X, HC_FRAME_INTERVAL_MASK = 0x%X, HC_FRAME_INTERVAL_RESET = 0x%X, HC_FRAME_REMAIN_TOGGLE = 0x%X, HC_FRAME_NUMBER_MASK = 0x%X, HC_FRAME_NUMBER_HALF = 0x%X, HC_HCCA_MASK = 0x%X, HC_POINTER_MASK = 0x%X;",
		hcInterruptDoneHead, hcInterruptFrame, hcInterruptOverflow, hcInterruptRootHub, hcInterruptMaster, hcFrameIntervalMask, hcFrameIntervalReset, hcFrameRemainToggle, hcFrameNumberMask, hcFrameNumberHalf, hcHCCAMask, hcPointerMask)
	e.js("const USB_PORTS = %d, CONTROLLER_PORT = %d, DAUGHTERBOARD_PORT = %d, RH_DESCRIPTOR_A_RESET = 0x%X, RH_DESCRIPTOR_A_WRITTEN = 0x%X, RH_REMOTE_WAKEUP = 0x%X, RH_SET_REMOTE_WAKEUP = 0x%X, RH_CLEAR_REMOTE_WAKEUP = 0x%X;",
		rhPorts, controllerPort, daughterboardPort, rhDescriptorAReset, rhDescriptorAWritten, rhRemoteWakeup, rhSetRemoteWakeup, rhClearRemoteWakeup)
	e.js("const HUB_DEVICE = Uint8Array.from([%s]), HUB_CONFIG = Uint8Array.from([%s]), HUB_CLASS = Uint8Array.from([%s]);", byteList(hubDeviceDescriptor), byteList(hubConfigDescriptor), byteList(hubClassDescriptor))
	e.js("const HUB_PORTS = %d, HUB_DESCRIPTOR_TYPE = 0x%X, HUB_RECIPIENT_HUB = 0x%X, HUB_RECIPIENT_PORT = 0x%X, HUB_ENDPOINT = %d, HUB_FEATURE_PORT_ENABLE = %d, HUB_FEATURE_PORT_SUSPEND = %d, HUB_FEATURE_PORT_RESET = %d, HUB_FEATURE_PORT_POWER = %d, HUB_FEATURE_CHANGE_BASE = %d;",
		hubPorts, hubDescriptorType, hubRecipientHub, hubRecipientPort, hubEndpoint, hubFeaturePortEnable, hubFeaturePortSuspend, hubFeaturePortReset, hubFeaturePortPower, hubFeatureChangeBase)
	e.js("const HUB_PORT_CONNECTED = 0x%X, HUB_PORT_ENABLED = 0x%X, HUB_PORT_SUSPENDED = 0x%X, HUB_PORT_RESET = 0x%X, HUB_PORT_POWERED = 0x%X, HUB_CHANGE_CONNECTED = 0x%X, HUB_CHANGE_SUSPENDED = 0x%X, HUB_CHANGE_RESET = 0x%X;",
		hubPortConnected, hubPortEnabled, hubPortSuspended, hubPortReset, hubPortPowered, hubChangeConnected, hubChangeSuspended, hubChangeReset)
	e.js("const PORT_CONNECTED = 0x%X, PORT_ENABLED = 0x%X, PORT_SUSPENDED = 0x%X, PORT_CLEAR_SUSPEND = 0x%X, PORT_RESET = 0x%X, PORT_POWERED = 0x%X, PORT_CONNECT_CHANGE = 0x%X, PORT_SUSPEND_CHANGE = 0x%X, PORT_RESET_CHANGE = 0x%X, PORT_CHANGE_MASK = 0x%X, PORT_RESET_MS = %d, USB_FRAME_MS = %d;",
		portConnected, portEnabled, portSuspended, portClearSuspend, portReset, portPowered, portConnectChange, portSuspendChange, portResetChange, portChangeMask, portResetMs, usbFrameMs)
	e.js("const HCCA_INTERRUPT_SLOTS = %d, HCCA_FRAME_NUMBER = 0x%X, HCCA_DONE_HEAD = 0x%X, ED_FUNCTION_MASK = 0x%X, ED_ENDPOINT_SHIFT = %d, ED_ENDPOINT_MASK = 0x%X, ED_DIRECTION_SHIFT = %d, ED_DIRECTION_MASK = 0x%X, ED_SKIP = 0x%X, ED_ISOCHRONOUS = 0x%X, ED_MPS_SHIFT = %d, ED_MPS_MASK = 0x%X, ED_TAIL = %d, ED_HEAD = %d, ED_NEXT = %d, ED_HALTED = 0x%X;",
		hccaInterruptSlots, hccaFrameNumber, hccaDoneHead, edFunctionMask, edEndpointShift, edEndpointMask, edDirectionShift, edDirectionMask, edSkip, edIsochronous, edMaxPacketShift, edMaxPacketMask, edTailPointer, edHeadPointer, edNextED, edHalted)
	e.js("const TD_BUFFER_ROUNDING = 0x%X, TD_DIRECTION_SHIFT = %d, TD_DIRECTION_MASK = 0x%X, TD_SETUP = %d, TD_OUT = %d, TD_IN = %d, TD_TOGGLE_SHIFT = %d, TD_TOGGLE_MASK = 0x%X, TD_TOGGLE_FROM_TD = 0x%X, TD_ERROR_COUNT_SHIFT = %d, TD_ERROR_COUNT_MASK = 0x%X, TD_CONDITION_SHIFT = %d, TD_CONDITION_MASK = 0x%X, TD_CURRENT = %d, TD_NEXT = %d, TD_END = %d, TD_PAGE_MASK = 0x%X;",
		tdBufferRounding, tdDirectionShift, tdDirectionMask, tdDirectionSetup, tdDirectionOut, tdDirectionIn, tdToggleShift, tdToggleMask, tdToggleFromTD, tdErrorCountShift, tdErrorCountMask, tdConditionShift, tdConditionMask, tdCurrentBuffer, tdNextTD, tdBufferEnd, tdPageMask)
	e.js("const CC_NO_ERROR = %d, CC_STALL = %d, CC_NOT_RESPONDING = %d, CC_DATA_UNDERRUN = %d, USB_LIST_LIMIT = %d, USB_TRANSFERS_PER_FRAME = %d, USB_FRAMES_PER_ADVANCE = %d;",
		ccNoError, ccStall, ccNotResponding, ccDataUnderrun, usbListLimit, usbTransfersPerFrame, usbFramesPerAdvance)
	e.js("const USB_GET_STATUS = %d, USB_CLEAR_FEATURE = %d, USB_SET_FEATURE = %d, USB_SET_ADDRESS = %d, USB_GET_DESCRIPTOR = %d, USB_GET_CONFIGURATION = %d, USB_SET_CONFIGURATION = %d, USB_GET_INTERFACE = %d, USB_SET_INTERFACE = %d, USB_IN = 0x%X, USB_TYPE_MASK = 0x%X, USB_TYPE_STANDARD = 0x%X, USB_TYPE_CLASS = 0x%X, USB_TYPE_VENDOR = 0x%X, USB_RECIPIENT_MASK = 0x%X, USB_RECIPIENT_OTHER = 0x%X, USB_DESCRIPTOR_DEVICE = %d, USB_DESCRIPTOR_CONFIG = %d;",
		usbGetStatus, usbClearFeature, usbSetFeature, usbSetAddress, usbGetDescriptor, usbGetConfiguration, usbSetConfiguration, usbGetInterface, usbSetInterface, usbDirectionIn, usbTypeMask, usbTypeStandard, usbTypeClass, usbTypeVendor, usbRecipientMask, usbRecipientOther, usbDescriptorDevice, usbDescriptorConfig)
	e.js("const GAMEPAD_DEVICE = Uint8Array.from([%s]), GAMEPAD_CONFIG = Uint8Array.from([%s]), GAMEPAD_XID = Uint8Array.from([%s]);", byteList(gamepadDeviceDescriptor), byteList(gamepadConfigDescriptor), byteList(gamepadXIDDescriptor))
	e.js("const XID_DESCRIPTOR_TYPE = 0x%X, XID_GET_REPORT = 0x%X, XID_SET_REPORT = 0x%X, XID_GET_CAPABILITIES = 0x%X, XID_REPORT_INPUT = 0x%X, XID_REPORT_OUTPUT = 0x%X, XID_INPUT_BYTES = %d, XID_OUTPUT_BYTES = %d, XID_ENDPOINT = %d, XID_BUTTONS_AT = %d, XID_ANALOG_AT = %d, XID_THUMB_AT = %d, XID_ANALOG_MAX = %d, XID_THUMB_MAX = %d;",
		xidDescriptorType, xidGetReport, xidSetReport, xidGetCapabilities, xidReportInput, xidReportOutput, xidInputReportBytes, xidOutputBytes, xidEndpoint, xidButtonsAt, xidAnalogAt, xidThumbAt, xidAnalogMax, xidThumbMax)
	e.js("const XID_BUTTONS = new Map([[\"up\", 0x%X], [\"down\", 0x%X], [\"left\", 0x%X], [\"right\", 0x%X], [\"start\", 0x%X], [\"back\", 0x%X], [\"lthumb\", 0x%X], [\"rthumb\", 0x%X]]);", xidUp, xidDown, xidLeft, xidRight, xidStart, xidBack, xidLeftThumb, xidRightThumb)
	e.js("const XID_ANALOG = new Map([[\"a\", %d], [\"b\", %d], [\"x\", %d], [\"y\", %d], [\"black\", %d], [\"white\", %d], [\"lt\", %d], [\"rt\", %d]]);", xidA, xidB, xidX, xidY, xidBlack, xidWhite, xidLeftTrigger, xidRightTrigger)
	e.js("const XID_AXES = new Map([%s]);", axisList())
	e.js("const CONTROLLER_KEYS = new Map([%s]);", keyList())
	e.js("const GAMEPAD_BUTTONS = [%s], GAMEPAD_AXES = [%s];", quotedList(gamepadButtons), quotedList(gamepadAxes))
	e.block(usbScript)
}

// Bytes as a JavaScript list of hex literals
func byteList(b []byte) string {
	parts := make([]string, len(b))
	for i, v := range b {
		parts[i] = fmt.Sprintf("0x%02X", v)
	}
	return strings.Join(parts, ", ")
}

// Strings as a JavaScript list of quoted names
func quotedList(names []string) string {
	parts := make([]string, len(names))
	for i, n := range names {
		parts[i] = fmt.Sprintf("%q", n)
	}
	return strings.Join(parts, ", ")
}

// The thumbstick axes by name and index as JavaScript map entries
func axisList() string {
	parts := make([]string, len(gamepadAxes))
	for i, n := range gamepadAxes {
		parts[i] = fmt.Sprintf("[%q, %d]", n, i)
	}
	return strings.Join(parts, ", ")
}

// The key bindings as JavaScript map entries of key code to control
func keyList() string {
	parts := make([]string, len(controllerKeys))
	for i, k := range controllerKeys {
		parts[i] = fmt.Sprintf("[%q, %q]", k[0], k[1])
	}
	return strings.Join(parts, ", ")
}
