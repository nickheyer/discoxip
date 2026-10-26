// Boots the console outside a browser, for running the kernel under Node with the bus and block traces printed

import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { assemble, entryKey, hex8 } from './boot.js';

const dir = process.argv[2];
if (!dir) {
  console.error('usage: node headless.js <boot directory> [--trace] [--blocks N]');
  process.exit(2);
}
const json = (name) => JSON.parse(readFileSync(join(dir, name), 'utf8'));
const bytes = (name) => new Uint8Array(readFileSync(join(dir, name)));
const boot = json('boot.json');
const model = json('machine.json');
const hardware = json('hardware.json');
const diskIndex = json('disk.json');
const files = { flash: bytes(boot.flash.file), bldr: bytes(boot.bldr.file), kernel: bytes(boot.kernel.run.file), eeprom: bytes('eeprom.bin'), disk: { index: diskIndex, data: bytes(diskIndex.file) } };
const { m, bus } = assemble({ model, boot, hardware, files, now: () => Date.now() });
const recent = [];
const traceAll = process.argv.includes('--trace');
const limitArg = process.argv.indexOf('--blocks');
const limit = limitArg >= 0 ? Number(process.argv[limitArg + 1]) : Infinity;
bus.log = (message) => console.log(`[bus] ${message} at ${m.key} blocks=${m.blocksRun}`);
m.trace = (mm, key) => {
  recent.push(key);
  if (recent.length > 40) recent.shift();
  if (traceAll) console.log(`[block] ${key} esp=${hex8(mm.regs.ESP)} eax=${hex8(mm.regs.EAX)}`);
  if (mm.blocksRun >= limit) throw new Error(`stopped after ${limit} blocks ${mm.describe()}`);
};
const watchArg = process.argv.indexOf('--watch');
if (watchArg >= 0) {
  m.watch = new Map();
  for (const spec of process.argv[watchArg + 1].split(',')) {
    const pa = parseInt(spec, 16);
    m.watch.set(pa, (mm, va, v) => console.log(`[watch] ${hex8(pa)} = ${hex8(v)} through ${hex8(va)} at ${mm.key} blocks=${mm.blocksRun} ${mm.describe()}`));
  }
}
const started = Date.now();
try {
  m.run(entryKey(m, boot));
  console.log(`stopped: ${m.describe()}`);
} catch (err) {
  console.log(`FAULT: ${err.message}`);
  console.log(`recent blocks: ${recent.join(' ')}`);
}
console.log(`${m.blocksRun} blocks in ${((Date.now() - started) / 1000).toFixed(2)}s, tsc ${m.tsc}`);
if (process.argv.includes('--state')) {
  const pic = bus.pic, pit = bus.pit;
  console.log(`IF=${m.IF} irq=${m.irq} halted=${m.halted} nextEvent=${m.nextEvent} idt=${hex8(m.idtr.base)}/${m.idtr.limit}`);
  for (const [i, c] of pic.chips.entries()) console.log(`pic${i + 1} base=${c.base.toString(16)} imr=${c.imr.toString(16)} irr=${c.irr.toString(16)} isr=${c.isr.toString(16)} aeoi=${c.autoEOI} step=${c.step}`);
  console.log(`pit0 reload=${pit.channels[0].reload} mode=${pit.channels[0].mode} deadline=${pit.timer.deadline}`);
}
