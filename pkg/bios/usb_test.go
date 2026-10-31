//go:build samples

package bios

import (
	"bufio"
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/nickheyer/discoxip/pkg/xbe"
)

// The page's parts the host controller reaches, stood in for under node, the clock moved by hand, the bridge
// holding keys and gamepads the test sets, the line raised recorded
const usbPrelude = `
let clock = 0, held = new Set(), pads = [];
const TABLES = { hardware: { interrupts: [{ irq: 1, registers: [0xFED00000] }] }, console: { usb_hub: false } };
const bridge = { now: () => clock, keys: () => held, gamepads: () => pads };
function hwElapsedMs() { return clock; }
const TIMED = [], SHARED = new Map(), lines = [];
function shared(key, make) { let v = SHARED.get(key); if (v === undefined) { v = make(); SHARED.set(key, v); } return v; }
function windowKey(w) { return w.bus + ":" + w.device + ":" + w.fn + ":" + w.register + ":" + w.io; }
function registerModel(w) { return { read: () => 0, write: () => {} }; }
function setLine(irq, high) { lines.push([irq, high]); }
`

// Drives the controller through a port reset, the device descriptor, an address and the reports its endpoint
// answers with the keys held
const usbHarness = `
initMemory(1 << 20);
const expect = (what, ok) => { if (!ok) throw new Error(what); console.log(what); };
const w = { base: 0xFED00000, limit: 0xFED01000, bus: 0, device: 2, fn: 0, register: 0x10, io: false };
const hc = usbModel(w), port = HC.rhPortStatus + 4 * (CONTROLLER_PORT - 1);
const advance = (ms) => { clock += ms; TIMED[0].advance(clock); };
expect("the controller sits connected and powered on the console's first controller port", hc.read(port, 4) === (PORT_CONNECTED | PORT_POWERED) && hc.read(HC.rhPortStatus, 4) === PORT_POWERED);
expect("the revision and the root hub descriptor read as the console's", hc.read(HC.revision, 4) === HC_REVISION && hc.read(HC.rhDescriptorA, 4) === RH_DESCRIPTOR_A_RESET);
hc.write(HC.rhPortStatus, 4, PORT_RESET);
expect("resetting an empty port marks a connect change instead", hc.read(HC.rhPortStatus, 4) === (PORT_POWERED | PORT_CONNECT_CHANGE) && (hc.read(HC.interruptStatus, 4) & HC_INTERRUPT_ROOT_HUB) !== 0);
hc.write(HC.rhPortStatus, 4, PORT_CONNECT_CHANGE);
hc.write(HC.interruptStatus, 4, HC_INTERRUPT_ROOT_HUB);
hc.write(HC.rhPortStatus + 4 * (CONTROLLER_PORT - 1), 4, PORT_RESET);
expect("a port reset holds the reset bit until its time passes", (hc.read(port, 4) & PORT_RESET) !== 0 && (hc.read(HC.interruptStatus, 4) & HC_INTERRUPT_ROOT_HUB) === 0);
advance(PORT_RESET_MS + 1);
expect("the reset completes enabling the port and raising the root hub change", (hc.read(port, 4) & (PORT_ENABLED | PORT_RESET | PORT_RESET_CHANGE)) === (PORT_ENABLED | PORT_RESET_CHANGE) && (hc.read(HC.interruptStatus, 4) & HC_INTERRUPT_ROOT_HUB) !== 0);
expect("no line rises while the master enable is off", lines.every(([irq, high]) => !high));
hc.write(HC.interruptEnable, 4, HC_INTERRUPT_MASTER | HC_INTERRUPT_ROOT_HUB | HC_INTERRUPT_DONE_HEAD);
expect("enabling the change raises the line the executable connects", lines.length > 0 && lines[lines.length - 1][0] === 1 && lines[lines.length - 1][1] === true);
hc.write(HC.rhPortStatus + 4 * (CONTROLLER_PORT - 1), 4, PORT_RESET_CHANGE);
hc.write(HC.interruptStatus, 4, HC_INTERRUPT_ROOT_HUB);
expect("clearing the change drops the line", lines[lines.length - 1][1] === false);
const w32 = (at, v) => physWrite(at, 4, v >>> 0), r32 = (at) => physRead(at, 4);
const bytes = (at, list) => list.forEach((b, i) => physWrite(at + i, 1, b));
const td = (at, flags, cbp, next, be) => { w32(at, flags); w32(at + TD_CURRENT, cbp); w32(at + TD_NEXT, next); w32(at + TD_END, be); };
const notAccessed = 0xF << TD_CONDITION_SHIFT, dataFromTD = 2 << TD_TOGGLE_SHIFT, data1 = 3 << TD_TOGGLE_SHIFT;
function control(address, setup, buffer, length, rounding) {
  w32(0x2000, address | (8 << ED_MPS_SHIFT)); w32(0x2000 + ED_TAIL, 0x2400); w32(0x2000 + ED_HEAD, 0x2100); w32(0x2000 + ED_NEXT, 0);
  bytes(0x3000, setup);
  td(0x2100, notAccessed | dataFromTD | (TD_SETUP << TD_DIRECTION_SHIFT), 0x3000, 0x2200, 0x3007);
  const inbound = (setup[0] & USB_IN) !== 0;
  if (length > 0) td(0x2200, notAccessed | data1 | ((inbound ? TD_IN : TD_OUT) << TD_DIRECTION_SHIFT) | (rounding ? TD_BUFFER_ROUNDING : 0), buffer, 0x2300, buffer + length - 1);
  else td(0x2200, notAccessed | data1 | ((inbound ? TD_OUT : TD_IN) << TD_DIRECTION_SHIFT), 0, 0x2300, 0);
  td(0x2300, notAccessed | data1 | ((inbound ? TD_OUT : TD_IN) << TD_DIRECTION_SHIFT), 0, 0x2400, 0);
  if (length === 0) w32(0x2200 + TD_NEXT, 0x2400);
  hc.write(HC.hcca, 4, 0x1000); hc.write(HC.controlHeadED, 4, 0x2000);
  hc.write(HC.control, 4, HC_CONTROL_ENABLE | HC_PERIODIC_ENABLE | (HC_STATE_OPERATIONAL << HC_STATE_SHIFT));
  advance(1);
}
control(0, [0x80, USB_GET_DESCRIPTOR, 0, USB_DESCRIPTOR_DEVICE, 0, 0, 18, 0], 0x3100, 18, true);
const landed = []; for (let i = 0; i < 18; i++) landed.push(physRead(0x3100 + i, 1));
expect("the device descriptor lands in the buffer whole", landed.every((b, i) => b === GAMEPAD_DEVICE[i]));
expect("each descriptor completes without error and the endpoint's head reaches the tail with data 0 carried", (r32(0x2100) >>> TD_CONDITION_SHIFT) === 0 && (r32(0x2200) >>> TD_CONDITION_SHIFT) === 0 && (r32(0x2300) >>> TD_CONDITION_SHIFT) === 0 && (r32(0x2000 + ED_HEAD) & HC_POINTER_MASK) === 0x2400 && (r32(0x2000 + ED_HEAD) & 3) === 0);
expect("the buffer pointer of a whole transfer clears", r32(0x2200 + TD_CURRENT) === 0);
expect("the done queue chains the descriptors into the communications area and raises the done head interrupt", (r32(0x1000 + HCCA_DONE_HEAD) & HC_POINTER_MASK) === 0x2300 && r32(0x2300 + TD_NEXT) === 0x2200 && r32(0x2200 + TD_NEXT) === 0x2100 && r32(0x2100 + TD_NEXT) === 0 && (hc.read(HC.interruptStatus, 4) & HC_INTERRUPT_DONE_HEAD) !== 0 && physRead(0x1000 + HCCA_FRAME_NUMBER, 2) === hc.read(HC.fmNumber, 4));
hc.write(HC.interruptStatus, 4, HC_INTERRUPT_DONE_HEAD);
control(0, [0x80, USB_GET_DESCRIPTOR, 0, USB_DESCRIPTOR_CONFIG, 0, 0, 64, 0], 0x3100, 64, false);
expect("a short descriptor without rounding underruns and halts the endpoint", (r32(0x2200) >>> TD_CONDITION_SHIFT) === CC_DATA_UNDERRUN && (r32(0x2000 + ED_HEAD) & ED_HALTED) !== 0 && r32(0x2200 + TD_CURRENT) === 0x3100 + GAMEPAD_CONFIG.length);
hc.write(HC.interruptStatus, 4, HC_INTERRUPT_DONE_HEAD);
control(0, [0x00, USB_SET_ADDRESS, 5, 0, 0, 0, 0, 0], 0, 0, false);
expect("the address takes at the status stage", (r32(0x2200) >>> TD_CONDITION_SHIFT) === 0 && (r32(0x2000 + ED_HEAD) & HC_POINTER_MASK) === 0x2400);
hc.write(HC.interruptStatus, 4, HC_INTERRUPT_DONE_HEAD);
control(0, [0x80, USB_GET_DESCRIPTOR, 0, USB_DESCRIPTOR_DEVICE, 0, 0, 18, 0], 0x3100, 18, true);
expect("the old address no longer answers", (r32(0x2100) >>> TD_CONDITION_SHIFT) === CC_NOT_RESPONDING && (r32(0x2000 + ED_HEAD) & ED_HALTED) !== 0);
hc.write(HC.interruptStatus, 4, HC_INTERRUPT_DONE_HEAD);
control(5, [0xC1, USB_GET_DESCRIPTOR, 0, XID_DESCRIPTOR_TYPE, 0, 0, 16, 0], 0x3100, 16, true);
const xid = []; for (let i = 0; i < 16; i++) xid.push(physRead(0x3100 + i, 1));
expect("the new address answers the XID descriptor", xid.every((b, i) => b === GAMEPAD_XID[i]) && (r32(0x2200) >>> TD_CONDITION_SHIFT) === 0);
hc.write(HC.interruptStatus, 4, HC_INTERRUPT_DONE_HEAD);
control(5, [0x80, USB_GET_DESCRIPTOR, 0, 3, 0, 0, 8, 0], 0x3100, 8, true);
expect("a request the controller has no answer for stalls", (r32(0x2100) >>> TD_CONDITION_SHIFT) === CC_STALL && (r32(0x2000 + ED_HEAD) & ED_HALTED) !== 0);
hc.write(HC.interruptStatus, 4, HC_INTERRUPT_DONE_HEAD);
held = new Set(["Enter", "ArrowDown", "KeyL"]);
for (let slot = 0; slot < HCCA_INTERRUPT_SLOTS; slot++) w32(0x1000 + 4 * slot, 0x2800);
w32(0x2800, 5 | (XID_ENDPOINT << ED_ENDPOINT_SHIFT) | (2 << ED_DIRECTION_SHIFT) | (32 << ED_MPS_SHIFT)); w32(0x2800 + ED_TAIL, 0x2A00); w32(0x2800 + ED_HEAD, 0x2900); w32(0x2800 + ED_NEXT, 0);
td(0x2900, notAccessed | TD_BUFFER_ROUNDING | (TD_IN << TD_DIRECTION_SHIFT), 0x4000, 0x2A00, 0x401F);
hc.write(HC.controlHeadED, 4, 0);
advance(1);
const report = []; for (let i = 0; i < XID_INPUT_BYTES; i++) report.push(physRead(0x4000 + i, 1));
expect("the interrupt endpoint answers a report the size the descriptor names", (r32(0x2900) >>> TD_CONDITION_SHIFT) === 0 && report[1] === XID_INPUT_BYTES && r32(0x2900 + TD_CURRENT) === 0x4000 + XID_INPUT_BYTES);
expect("the keys held press down on the pad, the A button and the left stick right", report[XID_BUTTONS_AT] === 0x2 && report[XID_ANALOG_AT] === XID_ANALOG_MAX && (report[XID_THUMB_AT] | (report[XID_THUMB_AT + 1] << 8)) === XID_THUMB_MAX && report[XID_ANALOG_AT + 1] === 0);
held = new Set();
pads = [{ buttons: [{ value: 0 }, { value: 1 }], axes: [0, 0.5] }];
w32(0x2800 + ED_HEAD, 0x2900); w32(0x2900, notAccessed | TD_BUFFER_ROUNDING | (TD_IN << TD_DIRECTION_SHIFT)); w32(0x2900 + TD_CURRENT, 0x4000); w32(0x2900 + TD_NEXT, 0x2A00);
hc.write(HC.interruptStatus, 4, HC_INTERRUPT_DONE_HEAD);
advance(1);
const second = []; for (let i = 0; i < XID_INPUT_BYTES; i++) second.push(physRead(0x4000 + i, 1));
expect("a gamepad's B button and a pushed stick reach the report with the stick's up negated back", second[XID_ANALOG_AT + 1] === XID_ANALOG_MAX && second[XID_ANALOG_AT] === 0 && ((second[XID_THUMB_AT + 3] << 8) | second[XID_THUMB_AT + 2]) === 0x10000 + Math.round(-0.5 * XID_THUMB_MAX));
TABLES.console.usb_hub = true;
const w2 = { base: 0xFED08000, limit: 0xFED09000, bus: 0, device: 3, fn: 0, register: 0x10, io: false };
const hub = usbModel(w2), hubPort = HC.rhPortStatus + 4 * (DAUGHTERBOARD_PORT - 1);
const tick = (ms) => { clock += ms; TIMED[1].advance(clock); };
expect("a console with a daughterboard holds its hub on the first root port and nothing on the controller's", hub.read(hubPort, 4) === (PORT_CONNECTED | PORT_POWERED) && hub.read(port, 4) === PORT_POWERED);
hub.write(hubPort, 4, PORT_RESET); tick(PORT_RESET_MS + 1);
function transfer(model, address, setup, buffer, length, rounding) {
  w32(0x5000, address | (8 << ED_MPS_SHIFT)); w32(0x5000 + ED_TAIL, 0x5400); w32(0x5000 + ED_HEAD, 0x5100); w32(0x5000 + ED_NEXT, 0);
  bytes(0x6000, setup);
  td(0x5100, notAccessed | dataFromTD | (TD_SETUP << TD_DIRECTION_SHIFT), 0x6000, 0x5200, 0x6007);
  const inbound = (setup[0] & USB_IN) !== 0;
  if (length > 0) { td(0x5200, notAccessed | data1 | ((inbound ? TD_IN : TD_OUT) << TD_DIRECTION_SHIFT) | (rounding ? TD_BUFFER_ROUNDING : 0), buffer, 0x5300, buffer + length - 1); td(0x5300, notAccessed | data1 | ((inbound ? TD_OUT : TD_IN) << TD_DIRECTION_SHIFT), 0, 0x5400, 0); }
  else td(0x5200, notAccessed | data1 | ((inbound ? TD_OUT : TD_IN) << TD_DIRECTION_SHIFT), 0, 0x5400, 0);
  model.write(HC.hcca, 4, 0x1000); model.write(HC.controlHeadED, 4, 0x5000);
  model.write(HC.control, 4, HC_CONTROL_ENABLE | (HC_STATE_OPERATIONAL << HC_STATE_SHIFT));
  tick(1);
  return (r32(0x5100) >>> TD_CONDITION_SHIFT) === 0 && (r32(0x5200) >>> TD_CONDITION_SHIFT) === 0 && (length === 0 || (r32(0x5300) >>> TD_CONDITION_SHIFT) === 0);
}
const read = (at, n) => { const out = []; for (let i = 0; i < n; i++) out.push(physRead(at + i, 1)); return out; };
expect("the hub answers its device descriptor as a hub", transfer(hub, 0, [0x80, USB_GET_DESCRIPTOR, 0, USB_DESCRIPTOR_DEVICE, 0, 0, 18, 0], 0x6100, 18, true) && read(0x6100, 18).every((b, i) => b === HUB_DEVICE[i]));
expect("the hub takes an address", transfer(hub, 0, [0x00, USB_SET_ADDRESS, 2, 0, 0, 0, 0, 0], 0, 0, false));
expect("the hub class descriptor names the daughterboard's ports", transfer(hub, 2, [0xA0, USB_GET_DESCRIPTOR, 0, HUB_DESCRIPTOR_TYPE, 0, 0, 9, 0], 0x6100, 9, true) && read(0x6100, 9).every((b, i) => b === HUB_CLASS[i]) && HUB_CLASS[2] === HUB_PORTS);
expect("the controller's hub port reads connected and powered with its connection to announce", transfer(hub, 2, [0xA3, USB_GET_STATUS, 0, 0, CONTROLLER_PORT, 0, 4, 0], 0x6100, 4, true) && (read(0x6100, 2)[0] | (read(0x6100, 2)[1] << 8)) === (HUB_PORT_CONNECTED | HUB_PORT_POWERED) && read(0x6102, 2)[0] === HUB_CHANGE_CONNECTED);
expect("the hub's other ports announce nothing", transfer(hub, 2, [0xA3, USB_GET_STATUS, 0, 0, 1, 0, 4, 0], 0x6100, 4, true) && (read(0x6100, 2)[0] | (read(0x6100, 2)[1] << 8)) === HUB_PORT_POWERED && read(0x6102, 2)[0] === 0);
expect("clearing the connection change takes", transfer(hub, 2, [0x23, USB_CLEAR_FEATURE, HUB_FEATURE_CHANGE_BASE, 0, CONTROLLER_PORT, 0, 0, 0], 0, 0, false) && transfer(hub, 2, [0xA3, USB_GET_STATUS, 0, 0, CONTROLLER_PORT, 0, 4, 0], 0x6100, 4, true) && read(0x6102, 2)[0] === 0);
expect("the controller's hub port reset completes with its change", transfer(hub, 2, [0x23, USB_SET_FEATURE, HUB_FEATURE_PORT_RESET, 0, CONTROLLER_PORT, 0, 0, 0], 0, 0, false) && (tick(PORT_RESET_MS + 1), transfer(hub, 2, [0xA3, USB_GET_STATUS, 0, 0, CONTROLLER_PORT, 0, 4, 0], 0x6100, 4, true)) && (read(0x6100, 2)[0] & (HUB_PORT_ENABLED | HUB_PORT_RESET)) === HUB_PORT_ENABLED && (read(0x6102, 2)[0] & HUB_CHANGE_RESET) !== 0);
for (let slot = 0; slot < HCCA_INTERRUPT_SLOTS; slot++) w32(0x1000 + 4 * slot, 0x5800);
w32(0x5800, 2 | (HUB_ENDPOINT << ED_ENDPOINT_SHIFT) | (2 << ED_DIRECTION_SHIFT) | (1 << ED_MPS_SHIFT)); w32(0x5800 + ED_TAIL, 0x5A00); w32(0x5800 + ED_HEAD, 0x5900); w32(0x5800 + ED_NEXT, 0);
td(0x5900, notAccessed | TD_BUFFER_ROUNDING | (TD_IN << TD_DIRECTION_SHIFT), 0x6200, 0x5A00, 0x6200);
hub.write(HC.controlHeadED, 4, 0); hub.write(HC.control, 4, HC_PERIODIC_ENABLE | (HC_STATE_OPERATIONAL << HC_STATE_SHIFT)); tick(1);
expect("the hub's change endpoint flags the controller's port", (r32(0x5900) >>> TD_CONDITION_SHIFT) === 0 && physRead(0x6200, 1) === (1 << CONTROLLER_PORT));
expect("clearing the change leaves the endpoint quiet", transfer(hub, 2, [0x23, USB_CLEAR_FEATURE, HUB_FEATURE_CHANGE_BASE + 4, 0, CONTROLLER_PORT, 0, 0, 0], 0, 0, false) && (w32(0x5800 + ED_HEAD, 0x5900), w32(0x5900, notAccessed | TD_BUFFER_ROUNDING | (TD_IN << TD_DIRECTION_SHIFT)), hub.write(HC.controlHeadED, 4, 0), tick(1), (r32(0x5900) >>> TD_CONDITION_SHIFT) === 0xF));
expect("the controller behind the hub answers the default address once its port is enabled", transfer(hub, 0, [0x80, USB_GET_DESCRIPTOR, 0, USB_DESCRIPTOR_DEVICE, 0, 0, 18, 0], 0x6100, 18, true) && read(0x6100, 18).every((b, i) => b === GAMEPAD_DEVICE[i]));
console.log("ok");
`

// The host controller must move descriptors between the lists and the controller as the dashboard's driver expects
func TestHostController(t *testing.T) {
	var buf bytes.Buffer
	if err := xbe.WriteCore(&buf); err != nil {
		t.Fatal(err)
	}
	buf.WriteString(usbPrelude)
	bw := bufio.NewWriter(&buf)
	e := &emitter{w: bw}
	e.writeUSB()
	if err := bw.Flush(); err != nil {
		t.Fatal(err)
	}
	buf.WriteString(usbHarness)
	path := filepath.Join(t.TempDir(), "usb.js")
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(node, path).CombinedOutput()
	if err != nil || !bytes.HasSuffix(bytes.TrimSpace(out), []byte("ok")) {
		t.Fatalf("node: %v\n%s", err, out)
	}
}
