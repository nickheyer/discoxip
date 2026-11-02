package bios

// The DSP56300 the audio processor's global and encode processors run, each run of straight line program words
// translated to a JavaScript function when first executed and the flow instructions, repeats and interrupts
// interpreted, the 56 bit accumulators, address modifiers, condition codes, hardware loops and stack as the
// Motorola manual defines them and the xemu interpreter implements them, the
// peripheral window at the top of X memory holding the halt request, the interrupt status and the DMA engine's
// registers, the DMA engine moving words between the DSP's memories and the scratch or FIFO memory the host names
const dspCoreScript = `
const DSP_X_WORDS = 0x1000, DSP_Y_WORDS = 0x800, DSP_P_WORDS = 0x1000, DSP_MIX_BASE = 0x1400, DSP_MIX_ALIAS = 0xC00, DSP_MIX_WORDS = 0x400, DSP_PERIPH_BASE = 0xFFFF80, DSP_PP_BASE = 0xFFFFC0;
const DSP_X0 = 4, DSP_X1 = 5, DSP_Y0 = 6, DSP_Y1 = 7, DSP_A0 = 8, DSP_B0 = 9, DSP_A2 = 10, DSP_B2 = 11, DSP_A1 = 12, DSP_B1 = 13, DSP_A = 14, DSP_B = 15, DSP_R0 = 16, DSP_N0 = 24, DSP_M0 = 32, DSP_LCSAVE = 48, DSP_SR = 57, DSP_OMR = 58, DSP_SP = 59, DSP_SSH = 60, DSP_SSL = 61, DSP_LA = 62, DSP_LC = 63;
const SR_C = 0, SR_V = 1, SR_Z = 2, SR_N = 3, SR_U = 4, SR_E = 5, SR_L = 6, SR_I0 = 8, SR_I1 = 9, SR_S0 = 10, SR_S1 = 11, SR_LF = 15, SR_FV = 16, SP_SE = 4, SP_UF = 5;
const DSP_REG_MASK = [0, 0, 0, 0, 24, 24, 24, 24, 24, 24, 8, 8, 24, 24, 24, 24, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 16, 8, 6, 16, 16, 16, 16];
const DSP_INTER_VECTOR = [0x00, 0x04, 0x02, 0x08], DSP_INTER_ILLEGAL = 1, DSP_INTER_STACK = 2;
const DSP_INT_START_FRAME = 1 << 1, DSP_INT_DMA_EOL = 1 << 7;
const DSP_SPACE_X = 0, DSP_SPACE_Y = 1, DSP_SPACE_P = 2;
const M24 = 0xFFFFFF, M16 = 0xFFFF;
function dsp56300(name, io) {
  const c = { name, io, pc: 0, r: new Uint32Array(64), ssh: new Uint32Array(16), ssl: new Uint32Array(16), x: new Uint32Array(DSP_X_WORDS), y: new Uint32Array(DSP_Y_WORDS), p: new Uint32Array(DSP_P_WORDS), mix: new Uint32Array(DSP_MIX_WORDS), periph: new Uint32Array(128), ops: new Array(DSP_P_WORDS), blocks: new Array(DSP_P_WORDS), limited: false, len: 1, loopRep: 0, pcOnRep: 0, halted: false, steps: 0, interrupts: 0, intState: 0, intFetch: -1, intSavePC: -1, intCounter: 0, intIPLRaise: 0, intPipeline: 0, intIPL: [3, 3, 3, 3], intPending: [0, 0, 0, 0], popH: 0, popL: 0, dmaBytes: new Uint8Array(0x4000), dmaView: null, dmaSpace: 0, dmaAt: 0, dma: { configuration: 0, control: 0, startBlock: 0, nextBlock: 0, eol: false, readCount: 0 } };
  dspReset(c);
  return c;
}
function dspReset(c) {
  c.periph.fill(0); c.ssh.fill(0); c.ssl.fill(0); c.r.fill(0);
  c.pc = 0; c.r[DSP_OMR] = 0x02;
  for (let i = 0; i < 8; i++) c.r[DSP_M0 + i] = M16;
  c.intPending.fill(0); c.intState = 0; c.intFetch = -1; c.intSavePC = -1; c.intCounter = 0; c.intPipeline = 0; c.intIPL.fill(3);
  c.loopRep = 0; c.pcOnRep = 0; c.halted = false; c.interrupts = 0;
  c.dma.configuration = 0; c.dma.control = 0; c.dma.startBlock = 0; c.dma.nextBlock = 0; c.dma.eol = false; c.dma.readCount = 0;
}
// The program is loaded from the first 0x800 words of the scratch memory when the processor leaves reset
function dspBootstrap(c) {
  const bytes = new Uint8Array(0x800 * 4);
  c.io.scratch(bytes, 0, bytes.length, false);
  const words = new Uint32Array(bytes.buffer);
  for (let i = 0; i < 0x800; i++) c.p[i] = words[i] & M24;
  c.ops.fill(undefined); c.blocks.fill(undefined);
}
function dspStartFrame(c) { c.interrupts |= DSP_INT_START_FRAME; }
function dspReadMemory(c, space, a) {
  if (space === DSP_SPACE_X) {
    if (a >= DSP_PERIPH_BASE) return dspReadPeripheral(c, a);
    if (a >= DSP_MIX_BASE && a < DSP_MIX_BASE + DSP_MIX_WORDS) return c.mix[a - DSP_MIX_BASE];
    if (a >= DSP_MIX_ALIAS && a < DSP_MIX_ALIAS + DSP_MIX_WORDS) return c.mix[a - DSP_MIX_ALIAS];
    if (a < DSP_X_WORDS) return c.x[a];
    fault(c.name + " reads X memory at " + hex(a));
  }
  if (space === DSP_SPACE_Y) { if (a < DSP_Y_WORDS) return c.y[a]; fault(c.name + " reads Y memory at " + hex(a)); }
  if (a < DSP_P_WORDS) return c.p[a];
  fault(c.name + " reads P memory at " + hex(a));
}
function dspWriteMemory(c, space, a, v) {
  v &= M24;
  if (space === DSP_SPACE_X) {
    if (a >= DSP_PERIPH_BASE) { dspWritePeripheral(c, a, v); return; }
    if (a >= DSP_MIX_BASE && a < DSP_MIX_BASE + DSP_MIX_WORDS) { c.mix[a - DSP_MIX_BASE] = v; return; }
    if (a >= DSP_MIX_ALIAS && a < DSP_MIX_ALIAS + DSP_MIX_WORDS) { c.mix[a - DSP_MIX_ALIAS] = v; return; }
    if (a < DSP_X_WORDS) { c.x[a] = v; return; }
    fault(c.name + " writes X memory at " + hex(a));
  }
  if (space === DSP_SPACE_Y) { if (a < DSP_Y_WORDS) { c.y[a] = v; return; } fault(c.name + " writes Y memory at " + hex(a)); }
  if (a < DSP_P_WORDS) { if (c.p[a] !== v) dspProgramChanged(c, a, v); return; }
  fault(c.name + " writes P memory at " + hex(a));
}
// A changed program word drops its own translation and every translated run that reaches it
function dspProgramChanged(c, a, v) {
  c.p[a] = v; c.ops[a] = undefined;
  for (let start = Math.max(0, a - DSP_BLOCK_WORDS * 2); start <= a; start++) { const run = c.blocks[start]; if (run !== undefined && run.end > a) c.blocks[start] = undefined; }
}
// The peripheral window, the halt request, the interrupt status cleared by the bits written and the DMA registers
function dspReadPeripheral(c, a) {
  switch (a) {
  case 0xFFFFB3: return 0;
  case 0xFFFFC5: return (c.interrupts | (c.dma.eol ? DSP_INT_DMA_EOL : 0)) >>> 0;
  case 0xFFFFD4: return c.dma.nextBlock;
  case 0xFFFFD5: return c.dma.startBlock;
  case 0xFFFFD6: if ((c.dma.control & DMA_CONTROL_RUNNING) !== 0 && ++c.dma.readCount > 2) { c.dma.control = (c.dma.control & ~DMA_CONTROL_RUNNING) | DMA_CONTROL_STOPPED; c.dma.readCount = 0; } return c.dma.control;
  case 0xFFFFD7: return c.dma.configuration;
  }
  return c.periph[a - DSP_PERIPH_BASE];
}
function dspWritePeripheral(c, a, v) {
  switch (a) {
  case 0xFFFFC4: if ((v & 1) !== 0) c.halted = true; return;
  case 0xFFFFC5: c.interrupts &= ~v; if ((v & DSP_INT_DMA_EOL) !== 0) c.dma.eol = false; return;
  case 0xFFFFD4: c.dma.nextBlock = v; return;
  case 0xFFFFD5: c.dma.startBlock = v; return;
  case 0xFFFFD6: dspDMAControl(c, v); return;
  case 0xFFFFD7: c.dma.configuration = v; return;
  }
  c.periph[a - DSP_PERIPH_BASE] = v;
}
// ---- the DMA engine ----
const DMA_CONTROL_ACTION = 0x7, DMA_ACTION_START = 1, DMA_ACTION_STOP = 2, DMA_ACTION_FREEZE = 3, DMA_ACTION_UNFREEZE = 4, DMA_CONTROL_FROZEN = 1 << 3, DMA_CONTROL_RUNNING = 1 << 4, DMA_CONTROL_STOPPED = 1 << 5, DMA_NODE_EOL = 1 << 14, DMA_NODE_MASK = 0x3FFF, DMA_DIRECTION = 1 << 1;
function dspDMAControl(c, v) {
  const d = c.dma;
  switch (v & DMA_CONTROL_ACTION) {
  case DMA_ACTION_START: d.control = (d.control | DMA_CONTROL_RUNNING) & ~DMA_CONTROL_STOPPED; d.readCount = 0; break;
  case DMA_ACTION_STOP: d.control = (d.control | DMA_CONTROL_STOPPED) & ~DMA_CONTROL_RUNNING; break;
  case DMA_ACTION_FREEZE: d.control |= DMA_CONTROL_FROZEN; break;
  case DMA_ACTION_UNFREEZE: d.control &= ~DMA_CONTROL_FROZEN; break;
  default: fault(c.name + " writes DMA control action " + (v & DMA_CONTROL_ACTION));
  }
  dspDMARun(c);
}
// A DMA block's address within the DSP's spaces, X below 0x1800, Y from 0x1800 and P from 0x2800
function dspDMASpace(c, a, n) {
  if (a < 0x1800) { if (a + n > 0x1800) fault(c.name + " DMA block spans past X memory at " + hex(a)); c.dmaSpace = DSP_SPACE_X; c.dmaAt = a; return; }
  if (a >= 0x1800 && a < 0x2000) { if (a + n > 0x2000) fault(c.name + " DMA block spans past Y memory at " + hex(a)); c.dmaSpace = DSP_SPACE_Y; c.dmaAt = a - 0x1800; return; }
  if (a >= 0x2800 && a < 0x3800) { if (a + n > 0x3800) fault(c.name + " DMA block spans past P memory at " + hex(a)); c.dmaSpace = DSP_SPACE_P; c.dmaAt = a - 0x2800; return; }
  fault(c.name + " DMA names address " + hex(a) + " in no memory space");
}
function dspDMAWord(c, space, a) {
  if (space === DSP_SPACE_X && a < DSP_MIX_ALIAS) return c.x[a];
  if (space === DSP_SPACE_Y && a < DSP_Y_WORDS) return c.y[a];
  return dspReadMemory(c, space, a);
}
function dspDMABuffer(c, bytes) {
  if (c.dmaBytes.length < bytes) { c.dmaBytes = new Uint8Array(bytes * 2); c.dmaView = null; }
  if (c.dmaView === null) c.dmaView = new DataView(c.dmaBytes.buffer);
  return c.dmaBytes.subarray(0, bytes);
}
function dspDMARun(c) {
  const d = c.dma;
  if ((d.control & DMA_CONTROL_RUNNING) === 0 || (d.control & DMA_CONTROL_FROZEN) !== 0) return;
  let blocks = 0;
  while ((d.nextBlock & DMA_NODE_EOL) === 0) {
    if (++blocks > 4096) fault(c.name + " DMA runs past " + blocks + " blocks without reaching the end of its list");
    dspDMASpace(c, d.nextBlock & DMA_NODE_MASK, 7);
    const space = c.dmaSpace, at = c.dmaAt;
    const next = dspDMAWord(c, space, at), control = dspDMAWord(c, space, at + 1), count = dspDMAWord(c, space, at + 2), dspOffset = dspDMAWord(c, space, at + 3), scratchBase = dspDMAWord(c, space, at + 5), scratchSize = dspDMAWord(c, space, at + 6) + 1;
    let scratchOffset = dspDMAWord(c, space, at + 4);
    d.nextBlock = next;
    if ((next & DMA_NODE_EOL) !== 0) d.eol = true;
    const interleave = (control & 1) !== 0, toHost = (control & DMA_DIRECTION) !== 0, writeback = ((control >>> 4) & 1) !== 0, buffer = (control >>> 5) & 0xF, format = (control >>> 10) & 7;
    if (((control >>> 2) & 3) !== 0 || ((control >>> 13) & 1) !== 0) fault(c.name + " DMA control " + hex(control) + " sets bits the engine has no model of");
    let itemBytes, itemMask;
    switch (format) { case 0: itemBytes = 1; itemMask = 0xFF; break; case 1: itemBytes = 2; itemMask = 0xFFFF; break; case 2: case 6: itemBytes = 4; itemMask = M24; break; default: fault(c.name + " DMA format " + format); }
    dspDMASpace(c, dspOffset, count);
    const memSpace = c.dmaSpace, memAt = c.dmaAt;
    const channels = (count & 0xF) + 1, blockCount = count >>> 4;
    let bytes = count * itemBytes;
    if (toHost && interleave) bytes = blockCount * itemBytes * channels;
    const buf = dspDMABuffer(c, bytes), view = c.dmaView;
    if (toHost) {
      if (interleave) {
        for (let i = 0; i < blockCount; i++) for (let ch = 0; ch < channels; ch++) { const v = dspDMAWord(c, memSpace, memAt + ch * blockCount + i); if (itemBytes === 2) view.setUint16((i * channels + ch) * 2, v >>> 8, true); else if (itemBytes === 4) view.setUint32((i * channels + ch) * 4, v, true); else fault(c.name + " DMA interleaves bytes"); }
      } else {
        for (let i = 0; i < count; i++) { const v = dspDMAWord(c, memSpace, memAt + i); if (itemBytes === 2) view.setUint16(i * 2, v >>> 8, true); else if (itemBytes === 4) view.setUint32(i * 4, v, true); else fault(c.name + " DMA moves bytes to the host"); }
      }
      if (buffer <= 3) c.io.fifo(buf, buffer, bytes, true);
      else if (buffer === 0xE) scratchOffset = dspDMACircular(c, scratchBase, scratchOffset, scratchSize, buf, true);
      else if (buffer === 0xF) c.io.scratch(buf, scratchBase + scratchOffset, bytes, true);
      else fault(c.name + " DMA writes buffer " + hex(buffer));
    } else {
      if (interleave) fault(c.name + " DMA interleaves words read from the host");
      if (buffer === 0xE) scratchOffset = dspDMACircular(c, scratchBase, scratchOffset, scratchSize, buf, false);
      else if (buffer === 0xF) c.io.scratch(buf, scratchBase + scratchOffset, bytes, false);
      else if (buffer <= 3) c.io.fifo(buf, buffer, bytes, false);
      else fault(c.name + " DMA reads buffer " + hex(buffer));
      for (let i = 0; i < count; i++) { let v; if (itemBytes === 2) v = (view.getUint16(i * 2, true) << 8) >>> 0; else if (itemBytes === 4) v = view.getUint32(i * 4, true) & itemMask; else fault(c.name + " DMA moves bytes from the host"); dspWriteMemory(c, memSpace, memAt + i, v); }
    }
    if (writeback) dspWriteMemory(c, space, at + 4, scratchOffset);
  }
}
function dspDMACircular(c, base, offset, size, buf, toHost) {
  if (offset >= size) offset = 0;
  let moved = 0;
  while (moved < buf.length) {
    const chunk = Math.min(buf.length - moved, size - offset);
    c.io.scratch(buf.subarray(moved, moved + chunk), base + offset, chunk, toHost);
    offset += chunk; if (offset >= size) offset = 0;
    moved += chunk;
  }
  return offset;
}
// ---- execution ----
const DSP_BLOCK_WORDS = 48;
function dspRun(c, budget) {
  let n = 0;
  while (!c.halted && n < budget) {
    if (c.loopRep !== 0 || c.intState !== 0 || c.intCounter !== 0) { dspStep(c); n++; continue; }
    const pc = c.pc;
    if (pc >= DSP_P_WORDS) fault(c.name + " fetches from " + hex(pc) + " past its program memory");
    let b = c.blocks[pc];
    if (b === undefined) { b = dspCompileBlock(c, pc); c.blocks[pc] = b; }
    n += b(c);
  }
  c.steps += n;
  return n;
}
function dspStep(c) {
  const pc = c.pc;
  if (pc >= DSP_P_WORDS) fault(c.name + " fetches from " + hex(pc) + " past its program memory");
  const word = c.p[pc];
  let f = c.ops[pc];
  if (f === undefined || f.word !== word) { f = dspCompile(c, word); c.ops[pc] = f; }
  c.len = 1;
  f(c);
  dspPostPC(c);
  dspPostInterrupts(c);
}
// The repeat and the hardware loop decide where the next instruction is
function dspPostPC(c) {
  const r = c.r;
  if (c.loopRep !== 0) {
    if (c.pcOnRep === 0) {
      r[DSP_LC] = (r[DSP_LC] - 1) & M16;
      if (r[DSP_LC] > 0) c.len = 0; else { c.loopRep = 0; r[DSP_LC] = r[DSP_LCSAVE]; }
    } else { if (r[DSP_LC] === 0) r[DSP_LC] = 0x10000; c.pcOnRep = 0; }
  }
  c.pc = (c.pc + c.len) & M24;
  dspLoopCheck(c);
}
// The hardware loop's end at the address after the last instruction, the count stepped and the body re-entered or the loop popped
function dspLoopCheck(c) {
  const r = c.r;
  if ((r[DSP_SR] & (1 << SR_LF)) === 0 || c.pc !== r[DSP_LA] + 1) return;
  r[DSP_LC] = (r[DSP_LC] - 1) & M16;
  if (r[DSP_LC] === 0) dspLoopPop(c); else c.pc = r[DSP_SSH];
}
// The finished loop's status and the enclosing loop's address and count restored from the stack
function dspLoopPop(c) {
  const r = c.r;
  dspStackPop(c);
  r[DSP_SR] = (r[DSP_SR] & 0x7F) | (c.popL & (1 << SR_LF));
  dspStackPop(c);
  r[DSP_LA] = c.popH; r[DSP_LC] = c.popL;
}
function dspAddInterrupt(c, inter) { if (c.intIPL[inter] === -1) return; if (c.intPending[inter] === 0) { c.intPending[inter] = 1; c.intCounter++; } }
function dspLongInterruptAt(c, word) { return (word & 0xFFF000) === 0x0D0000 || (word & 0xFFC0FF) === 0x0BC080; }
function dspEnterLong(c) {
  c.intState = 2;
  dspStackPush(c, c.intSavePC, c.r[DSP_SR], 0);
  c.r[DSP_SR] = (c.r[DSP_SR] & ~((1 << SR_LF) | (1 << SR_FV) | (1 << SR_S1) | (1 << SR_S0) | (1 << SR_I0) | (1 << SR_I1))) | (c.intIPLRaise << SR_I0);
}
function dspPostInterrupts(c) {
  if (c.loopRep !== 0) return;
  if (c.intState === 1) {
    switch (c.intPipeline) {
    case 5: c.intPipeline--; return;
    case 4: c.intSavePC = c.pc; c.pc = c.intFetch; if (dspLongInterruptAt(c, dspReadMemory(c, DSP_SPACE_P, c.intFetch))) dspEnterLong(c); c.intPipeline--; return;
    case 3: if (c.pc === c.intFetch + 1 && dspLongInterruptAt(c, dspReadMemory(c, DSP_SPACE_P, c.pc))) dspEnterLong(c); c.intPipeline--; return;
    case 2: if (c.pc === c.intFetch + 2) c.pc = c.intSavePC; c.intPipeline--; return;
    case 1: c.intPipeline--; return;
    case 0: c.intSavePC = -1; c.intFetch = -1; c.intState = 0; break;
    }
  }
  if (c.intCounter === 0) return;
  const iplSR = (c.r[DSP_SR] >>> SR_I0) & 3;
  let index = -1, raise = -1;
  for (let i = 0; i < 4; i++) {
    if (c.intPending[i] !== 1) continue;
    if (c.intIPL[i] === 3) { index = i; break; }
    if (c.intIPL[i] < iplSR || c.intIPL[i] <= raise) continue;
    index = i; raise = c.intIPL[i];
  }
  if (index < 0) return;
  c.intPending[index] = 0; c.intCounter--;
  c.intIPLRaise = Math.min(c.intIPL[index] + 1, 3);
  c.intFetch = DSP_INTER_VECTOR[index]; c.intPipeline = 5; c.intState = 1;
}
// ---- the stack ----
function dspStackPush(c, pc, sr, sshOnly) {
  const r = c.r, error = r[DSP_SP] & (1 << SP_SE), underflow = r[DSP_SP] & (1 << SP_UF);
  let stack = (r[DSP_SP] & 0xF) + 1;
  if (error === 0 && (stack & (1 << SP_SE)) !== 0) fault(c.name + " overflows its stack");
  r[DSP_SP] = (underflow | error | stack) & 0x3F;
  stack &= 0xF;
  if (stack !== 0) { c.ssh[stack] = pc & M16; if (sshOnly === 0) c.ssl[stack] = sr & M16; } else { c.ssh[0] = 0; c.ssl[0] = 0; }
  r[DSP_SSH] = c.ssh[stack]; r[DSP_SSL] = c.ssl[stack];
}
function dspStackPop(c) {
  const r = c.r, error = r[DSP_SP] & (1 << SP_SE), underflow = r[DSP_SP] & (1 << SP_UF);
  let stack = (r[DSP_SP] & 0xF) - 1;
  if (error === 0 && (stack & (1 << SP_SE)) !== 0) fault(c.name + " underflows its stack");
  r[DSP_SP] = (underflow | error | stack) & 0x3F;
  stack &= 0xF;
  c.popH = r[DSP_SSH]; c.popL = r[DSP_SSL];
  r[DSP_SSH] = c.ssh[stack]; r[DSP_SSL] = c.ssl[stack];
}
function dspComputeSS(c) { const stack = c.r[DSP_SP] & 0xF; c.r[DSP_SSH] = c.ssh[stack]; c.r[DSP_SSL] = c.ssl[stack]; }
// ---- registers ----
function dspWriteReg(c, num, v) {
  const r = c.r;
  switch (num) {
  case DSP_A: r[DSP_A0] = 0; r[DSP_A1] = v & M24; r[DSP_A2] = (v & (1 << 23)) !== 0 ? 0xFF : 0; return;
  case DSP_B: r[DSP_B0] = 0; r[DSP_B1] = v & M24; r[DSP_B2] = (v & (1 << 23)) !== 0 ? 0xFF : 0; return;
  case DSP_OMR: r[DSP_OMR] = v & 0xC7; return;
  case DSP_SR: r[DSP_SR] = v & 0xAF7F; return;
  case DSP_SP: { const error = r[DSP_SP] & (3 << SP_SE); if (error === 0 && (v & (3 << SP_SE)) !== 0) fault(c.name + " sets a stack error through the stack pointer"); r[DSP_SP] = v & 0x3F; dspComputeSS(c); return; }
  case DSP_SSH: dspStackPush(c, v, 0, 1); return;
  case DSP_SSL: { const slot = r[DSP_SP] & 0xF; const value = slot === 0 ? 0 : v & M16; c.ssl[slot] = value; r[DSP_SSL] = value; return; }
  }
  const bits = DSP_REG_MASK[num];
  r[num] = bits === 0 ? v >>> 0 : (v & ((1 << bits) - 1)) >>> 0;
}
// An accumulator read as 24 bits, scaled by the scaling mode and limited to the 24 bit range
function dspReadAccu24(c, num) {
  const r = c.r, d = num & 1, scaling = (r[DSP_SR] >>> SR_S0) & 3;
  let v = ((r[DSP_A2 + d] << 24) >>> 0) + r[DSP_A1 + d];
  if (scaling === 1) v = Math.floor(v / 2); else if (scaling === 2) v = ((v * 2) + ((r[DSP_A0 + d] >>> 23) & 1)) >>> 0;
  v = v & M24;
  c.limited = false;
  if (r[DSP_A2 + d] === 0 && v <= 0x7FFFFF) return v;
  if (r[DSP_A2 + d] === 0xFF && v >= 0x800000) return v;
  c.limited = true;
  r[DSP_SR] |= 1 << SR_L;
  return (r[DSP_A2 + d] & 0x80) !== 0 ? 0x800000 : 0x7FFFFF;
}
function dspReadReg24(c, num) { return num === DSP_A || num === DSP_B ? dspReadAccu24(c, num) : c.r[num]; }
// ---- 56 bit arithmetic on [high byte, middle word, low word] ----
const ACC_S = new Uint32Array(3), ACC_D = new Uint32Array(3), ACC_Z = new Uint32Array(3), ACC_T = new Uint32Array(3);
function accLoad(c, d, v) { v[0] = c.r[DSP_A2 + d]; v[1] = c.r[DSP_A1 + d]; v[2] = c.r[DSP_A0 + d]; }
function accStore(c, d, v) { c.r[DSP_A2 + d] = v[0]; c.r[DSP_A1 + d] = v[1]; c.r[DSP_A0 + d] = v[2]; }
function reg48Load(c, lo, v) { v[2] = c.r[lo]; v[1] = c.r[lo + 1]; v[0] = (v[1] & (1 << 23)) !== 0 ? 0xFF : 0; }
function reg24Load(c, num, v) { v[2] = 0; v[1] = c.r[num]; v[0] = (v[1] & (1 << 23)) !== 0 ? 0xFF : 0; }
function add56(s, d) {
  const fs = (s[0] >>> 7) & 1, fd = (d[0] >>> 7) & 1;
  d[2] = d[2] + s[2]; d[1] = d[1] + s[1] + ((d[2] >>> 24) & 1); d[0] = d[0] + s[0] + ((d[1] >>> 24) & 1);
  const carry = (d[0] >>> 8) & 1;
  d[2] &= M24; d[1] &= M24; d[0] &= 0xFF;
  const fr = (d[0] >>> 7) & 1, overflow = (fs ^ fr) & (fd ^ fr);
  return (overflow << SR_L) | (overflow << SR_V) | (carry << SR_C);
}
function sub56(s, d) {
  const saved = d[0];
  d[2] = (d[2] - s[2]) >>> 0; d[1] = (d[1] - s[1] - ((d[2] >>> 24) & 1)) >>> 0; d[0] = (d[0] - s[0] - ((d[1] >>> 24) & 1)) >>> 0;
  const carry = (d[0] >>> 8) & 1;
  d[2] &= M24; d[1] &= M24; d[0] &= 0xFF;
  const fs = (s[0] >>> 7) & 1, fd = (saved >>> 7) & 1, fr = (d[0] >>> 7) & 1, overflow = (fs ^ fd) & (fr ^ fd);
  return (overflow << SR_L) | (overflow << SR_V) | (carry << SR_C);
}
function abs56(d) { if ((d[0] & 0x80) === 0) return 0; ACC_Z[0] = 0; ACC_Z[1] = 0; ACC_Z[2] = 0; const sr = sub56(d, ACC_Z); d[0] = ACC_Z[0]; d[1] = ACC_Z[1]; d[2] = ACC_Z[2]; return sr; }
function asl56(d, n) {
  let overflow = 0, carry = 0;
  const sign = (d[0] >>> 7) & 1;
  for (let i = 0; i < n; i++) {
    carry = (d[0] >>> 7) & 1; overflow |= carry;
    d[0] = ((d[0] << 1) | (d[1] >>> 23)) & 0xFF;
    d[1] = ((d[1] << 1) | (d[2] >>> 23)) & M24;
    d[2] = (d[2] << 1) & M24;
  }
  return (overflow << SR_L) | ((sign !== ((d[0] >>> 7) & 1) ? 1 : 0) << SR_V) | (carry << SR_C);
}
function asr56(d, n) {
  let carry = 0;
  for (let i = 0; i < n; i++) {
    carry = d[2] & 1;
    d[2] = (d[2] >>> 1) | ((d[1] & 1) << 23);
    d[1] = (d[1] >>> 1) | ((d[0] & 1) << 23);
    d[0] = (d[0] >>> 1) | (d[0] & 0x80);
  }
  return carry << SR_C;
}
function mul56(s1, s2, d, negate) {
  let p = ((s1 << 8) >> 8) * ((s2 << 8) >> 8) * 2;
  if (negate !== 0) p = -p;
  const q = Math.floor(p / 16777216), hi = Math.floor(q / 16777216);
  d[0] = hi & 0xFF; d[1] = q - hi * 16777216; d[2] = p - q * 16777216;
}
function rnd56(c, d) {
  const sr = c.r[DSP_SR];
  if ((sr & (1 << SR_S0)) !== 0) { ACC_T[0] = 0; ACC_T[1] = 1; ACC_T[2] = 0; add56(ACC_T, d); if (d[2] === 0 && (d[1] & 1) === 0) d[1] &= 0xFFFFFC; d[1] &= 0xFFFFFE; d[2] = 0; }
  else if ((sr & (1 << SR_S1)) !== 0) { ACC_T[0] = 0; ACC_T[1] = 0; ACC_T[2] = 1 << 22; add56(ACC_T, d); if ((d[2] & 0x7FFFFF) === 0) d[2] = 0; d[2] &= 0x800000; }
  else { ACC_T[0] = 0; ACC_T[1] = 0; ACC_T[2] = 1 << 23; add56(ACC_T, d); if (d[2] === 0) d[1] &= 0xFFFFFE; d[2] = 0; }
}
function ccrEUNZ(c, v) { ccrEUNZ3(c, v[0], v[1], v[2]); }
function ccrEUNZ3(c, v0, v1, v2) {
  const r = c.r;
  r[DSP_SR] &= ~((1 << SR_E) | (1 << SR_U) | (1 << SR_N) | (1 << SR_Z));
  const scaling = (r[DSP_SR] >>> SR_S0) & 3;
  if (scaling === 0) { const e = (v0 << 1) + (v1 >>> 23); if (e !== 0 && e !== 0x1FF) r[DSP_SR] |= 1 << SR_E; if ((v1 & 0xC00000) === 0 || (v1 & 0xC00000) === 0xC00000) r[DSP_SR] |= 1 << SR_U; }
  else if (scaling === 1) { if (v0 !== 0 && v0 !== 0xFF) r[DSP_SR] |= 1 << SR_E; const u = ((v0 << 1) + (v1 >>> 23)) & 3; if (u === 0 || u === 3) r[DSP_SR] |= 1 << SR_U; }
  else if (scaling === 2) { const e = (v0 << 2) + (v1 >>> 22); if (e !== 0 && e !== 0x3FF) r[DSP_SR] |= 1 << SR_E; if ((v1 & 0x600000) === 0 || (v1 & 0x600000) === 0x600000) r[DSP_SR] |= 1 << SR_U; }
  else return;
  if (v0 === 0 && v1 === 0 && v2 === 0) r[DSP_SR] |= 1 << SR_Z;
  r[DSP_SR] |= (v0 >>> 4) & 0x8;
}
function setVC(c, sr) { c.r[DSP_SR] = (c.r[DSP_SR] & ~((1 << SR_V) | (1 << SR_C))) | sr; }
function setNZ24(c, reg) { const r = c.r; r[DSP_SR] &= ~((1 << SR_N) | (1 << SR_Z) | (1 << SR_V)); r[DSP_SR] |= ((r[reg] >>> 23) & 1) << SR_N; r[DSP_SR] |= (r[reg] === 0 ? 1 : 0) << SR_Z; }
// ---- effective addresses ----
function updateRnBitReverse(c, n) {
  const r = c.r;
  let revbits = 0;
  const nreg = r[DSP_N0 + n];
  for (; revbits < 16; revbits++) if ((nreg & (1 << revbits)) !== 0) break;
  revbits++;
  let rreg = r[DSP_R0 + n];
  const high = M16 - ((1 << revbits) - 1);
  let value = rreg & high;
  for (let i = 0; i < revbits; i++) if ((rreg & (1 << i)) !== 0) value |= 1 << (revbits - i - 1);
  value = (value + 1) & ((1 << revbits) - 1);
  rreg = (rreg & high) | value;
  value = rreg & high;
  for (let i = 0; i < revbits; i++) if ((rreg & (1 << i)) !== 0) value |= 1 << (revbits - i - 1);
  r[DSP_R0 + n] = value;
}
function updateRnModulo(c, n, modifier) {
  const r = c.r, modulo = r[DSP_M0 + n] + 1;
  let bufsize = 1, bufmask = M16;
  while (bufsize < modulo) { bufsize <<= 1; bufmask = (bufmask << 1) & M16; }
  const lobound = r[DSP_R0 + n] & bufmask, hibound = lobound + modulo - 1;
  let rreg = (r[DSP_R0 + n] << 16) >> 16;
  const orig = modifier;
  if (orig > modulo) { while (modifier > bufsize) { rreg += bufsize; modifier -= bufsize; } while (modifier < -bufsize) { rreg -= bufsize; modifier += bufsize; } }
  rreg = ((rreg + modifier) << 16) >> 16;
  if (orig !== modulo) { if (rreg > hibound) rreg -= modulo; else if (rreg < lobound) rreg += modulo; }
  r[DSP_R0 + n] = rreg & M16;
}
function updateRn(c, n, modifier) {
  const m = c.r[DSP_M0 + n];
  if (m === M16) c.r[DSP_R0 + n] = (((c.r[DSP_R0 + n] << 16) >> 16) + modifier) & M16;
  else if (m === 0) updateRnBitReverse(c, n);
  else if (m <= 32767) updateRnModulo(c, n, modifier);
}
function signedN(c, n) { return (c.r[DSP_N0 + n] << 16) >> 16; }
let dspEAImmediate = false;
function calcEA(c, mode) {
  const kind = (mode >>> 3) & 7, n = mode & 7, r = c.r;
  dspEAImmediate = false;
  switch (kind) {
  case 0: { const a = r[DSP_R0 + n]; updateRn(c, n, -signedN(c, n)); return a; }
  case 1: { const a = r[DSP_R0 + n]; updateRn(c, n, signedN(c, n)); return a; }
  case 2: { const a = r[DSP_R0 + n]; updateRn(c, n, -1); return a; }
  case 3: { const a = r[DSP_R0 + n]; updateRn(c, n, 1); return a; }
  case 4: return r[DSP_R0 + n];
  case 5: { const saved = r[DSP_R0 + n]; updateRn(c, n, signedN(c, n)); const a = r[DSP_R0 + n]; r[DSP_R0 + n] = saved; return a; }
  case 6: { const a = dspReadMemory(c, DSP_SPACE_P, c.pc + 1); c.len++; dspEAImmediate = n !== 0; return a; }
  case 7: updateRn(c, n, -1); return r[DSP_R0 + n];
  }
}
function calcCC(c, code) {
  const sr = c.r[DSP_SR], C = sr & 1, V = (sr >>> SR_V) & 1, Z = (sr >>> SR_Z) & 1, N = (sr >>> SR_N) & 1, U = (sr >>> SR_U) & 1, E = (sr >>> SR_E) & 1, L = (sr >>> SR_L) & 1;
  switch (code) {
  case 0: return C === 0;
  case 1: return (N ^ V) === 0;
  case 2: return Z === 0;
  case 3: return N === 0;
  case 4: return (Z | ((U ^ 1) & (E ^ 1))) === 0;
  case 5: return E === 0;
  case 6: return L === 0;
  case 7: return (Z | (N ^ V)) === 0;
  case 8: return C === 1;
  case 9: return (N ^ V) === 1;
  case 10: return Z === 1;
  case 11: return N === 1;
  case 12: return (Z | ((U ^ 1) & (E ^ 1))) === 1;
  case 13: return E === 1;
  case 14: return L === 1;
  case 15: return (Z | (N ^ V)) === 1;
  }
}
// ---- the parallel arithmetic operations by opcode byte ----
function aluTfrAcc(c, s, d) { c.r[DSP_A0 + d] = c.r[DSP_A0 + s]; c.r[DSP_A1 + d] = c.r[DSP_A1 + s]; c.r[DSP_A2 + d] = c.r[DSP_A2 + s]; }
function aluTst(c, d) { accLoad(c, d, ACC_D); ccrEUNZ(c, ACC_D); c.r[DSP_SR] &= ~(1 << SR_V); }
function aluAccOp(c, s, d, shift, subtract, compare) {
  accLoad(c, d, ACC_D);
  let sr = 0;
  if (shift > 0) sr = asl56(ACC_D, 1); else if (shift < 0) sr = asr56(ACC_D, 1);
  accLoad(c, s, ACC_S);
  sr |= subtract ? sub56(ACC_S, ACC_D) : add56(ACC_S, ACC_D);
  if (!compare) accStore(c, d, ACC_D);
  ccrEUNZ(c, ACC_D);
  setVC(c, sr);
}
function aluCmpm(c, s, d) { accLoad(c, d, ACC_D); abs56(ACC_D); accLoad(c, s, ACC_S); abs56(ACC_S); const sr = sub56(ACC_S, ACC_D); ccrEUNZ(c, ACC_D); setVC(c, sr); }
function aluRnd(c, d) { accLoad(c, d, ACC_D); rnd56(c, ACC_D); accStore(c, d, ACC_D); ccrEUNZ(c, ACC_D); }
function aluClr(c, d) { c.r[DSP_A0 + d] = 0; c.r[DSP_A1 + d] = 0; c.r[DSP_A2 + d] = 0; c.r[DSP_SR] = (c.r[DSP_SR] & ~((1 << SR_E) | (1 << SR_N) | (1 << SR_V))) | (1 << SR_U) | (1 << SR_Z); }
function aluNot(c, d) { c.r[DSP_A1 + d] = (~c.r[DSP_A1 + d]) & M24; setNZ24(c, DSP_A1 + d); }
function aluMax(c) {
  accLoad(c, 0, ACC_D); accLoad(c, 1, ACC_S);
  sub56(ACC_S, ACC_D);
  const pass = (ACC_D[0] & 0x80) !== 0 || (ACC_D[0] === 0 && ACC_D[1] === 0 && ACC_D[2] === 0);
  if (pass) { c.r[DSP_B0] = c.r[DSP_A0]; c.r[DSP_B1] = c.r[DSP_A1]; c.r[DSP_B2] = c.r[DSP_A2]; }
  c.r[DSP_SR] = (c.r[DSP_SR] & ~(1 << SR_C)) | ((pass ? 1 : 0) << SR_C);
}
function aluXY(c, which, d, subtract, withCarry) {
  const carry = (c.r[DSP_SR] >>> SR_C) & 1;
  accLoad(c, d, ACC_D);
  reg48Load(c, which === 0 ? DSP_X0 : DSP_Y0, ACC_S);
  let sr = subtract ? sub56(ACC_S, ACC_D) : add56(ACC_S, ACC_D);
  if (withCarry && carry !== 0) { ACC_T[0] = 0; ACC_T[1] = 0; ACC_T[2] = 1; sr |= subtract ? sub56(ACC_T, ACC_D) : add56(ACC_T, ACC_D); }
  accStore(c, d, ACC_D);
  ccrEUNZ(c, ACC_D);
  setVC(c, sr);
}
function aluShift56(c, d, left) { accLoad(c, d, ACC_D); const sr = left ? asl56(ACC_D, 1) : asr56(ACC_D, 1); accStore(c, d, ACC_D); setVC(c, sr); ccrEUNZ(c, ACC_D); }
function aluLsl(c, d) { const r = c.r, carry = (r[DSP_A1 + d] >>> 23) & 1; r[DSP_A1 + d] = (r[DSP_A1 + d] << 1) & M24; r[DSP_SR] &= ~((1 << SR_C) | (1 << SR_N) | (1 << SR_Z) | (1 << SR_V)); r[DSP_SR] |= carry | (((r[DSP_A1 + d] >>> 23) & 1) << SR_N) | ((r[DSP_A1 + d] === 0 ? 1 : 0) << SR_Z); }
function aluLsr(c, d) { const r = c.r, carry = r[DSP_A1 + d] & 1; r[DSP_A1 + d] >>>= 1; r[DSP_SR] &= ~((1 << SR_C) | (1 << SR_N) | (1 << SR_Z) | (1 << SR_V)); r[DSP_SR] |= carry | ((r[DSP_A1 + d] === 0 ? 1 : 0) << SR_Z); }
function aluRol(c, d) { const r = c.r, carry = (r[DSP_A1 + d] >>> 23) & 1; r[DSP_A1 + d] = ((r[DSP_A1 + d] << 1) | carry) & M24; r[DSP_SR] &= ~((1 << SR_C) | (1 << SR_N) | (1 << SR_Z) | (1 << SR_V)); r[DSP_SR] |= carry | (((r[DSP_A1 + d] >>> 23) & 1) << SR_N) | ((r[DSP_A1 + d] === 0 ? 1 : 0) << SR_Z); }
function aluRor(c, d) { const r = c.r, carry = r[DSP_A1 + d] & 1; r[DSP_A1 + d] = (r[DSP_A1 + d] >>> 1) | (carry << 23); r[DSP_SR] &= ~((1 << SR_C) | (1 << SR_N) | (1 << SR_Z) | (1 << SR_V)); r[DSP_SR] |= carry | (carry << SR_N) | ((r[DSP_A1 + d] === 0 ? 1 : 0) << SR_Z); }
function aluAbs(c, d) { accLoad(c, d, ACC_D); const overflowed = ACC_D[2] === 0 && ACC_D[1] === 0 && ACC_D[0] === 0x80 ? 1 : 0; abs56(ACC_D); accStore(c, d, ACC_D); c.r[DSP_SR] = (c.r[DSP_SR] & ~(1 << SR_V)) | (overflowed << SR_L) | (overflowed << SR_V); ccrEUNZ(c, ACC_D); }
function aluNeg(c, d) { accLoad(c, d, ACC_S); const overflowed = ACC_S[2] === 0 && ACC_S[1] === 0 && ACC_S[0] === 0x80 ? 1 : 0; ACC_D[0] = 0; ACC_D[1] = 0; ACC_D[2] = 0; sub56(ACC_S, ACC_D); accStore(c, d, ACC_D); c.r[DSP_SR] = (c.r[DSP_SR] & ~(1 << SR_V)) | (overflowed << SR_L) | (overflowed << SR_V); ccrEUNZ(c, ACC_D); }
function aluRegArith(c, src, d, subtract, compare) { accLoad(c, d, ACC_D); reg24Load(c, src, ACC_S); const sr = subtract ? sub56(ACC_S, ACC_D) : add56(ACC_S, ACC_D); if (!compare) accStore(c, d, ACC_D); ccrEUNZ(c, ACC_D); setVC(c, sr); }
function aluRegCmpm(c, src, d) { accLoad(c, d, ACC_D); abs56(ACC_D); reg24Load(c, src, ACC_S); abs56(ACC_S); const sr = sub56(ACC_S, ACC_D); ccrEUNZ(c, ACC_D); setVC(c, sr); }
function aluRegTfr(c, src, d) { c.r[DSP_A0 + d] = 0; c.r[DSP_A1 + d] = c.r[src]; c.r[DSP_A2 + d] = (c.r[src] & (1 << 23)) !== 0 ? 0xFF : 0; }
function aluRegLogic(c, src, d, op) { const r = c.r; if (op === 0) r[DSP_A1 + d] |= r[src]; else if (op === 1) r[DSP_A1 + d] = (r[DSP_A1 + d] ^ r[src]) & M24; else r[DSP_A1 + d] &= r[src]; setNZ24(c, DSP_A1 + d); }
function aluMultiply(c, s1, s2, d, negate, round, accumulate) {
  const r = c.r;
  let p = ((r[s1] << 8) >> 8) * ((r[s2] << 8) >> 8) * 2;
  if (negate !== 0) p = -p;
  const q = Math.floor(p / 16777216), hi = Math.floor(q / 16777216);
  let v0 = hi & 0xFF, v1 = q - hi * 16777216, v2 = p - q * 16777216, flags = 0;
  if (accumulate) {
    const a0 = r[DSP_A2 + d], fs = v0 >>> 7, fd = a0 >>> 7;
    v2 += r[DSP_A0 + d]; v1 += r[DSP_A1 + d] + (v2 >>> 24); v0 += a0 + (v1 >>> 24);
    const carry = (v0 >>> 8) & 1;
    v2 &= M24; v1 &= M24; v0 &= 0xFF;
    const fr = v0 >>> 7, overflow = (fs ^ fr) & (fd ^ fr);
    flags = (overflow << SR_L) | (overflow << SR_V) | (carry << SR_C);
  }
  if (round) {
    const sr = r[DSP_SR];
    if ((sr & (1 << SR_S0)) !== 0) { v1 += 1; v0 = (v0 + (v1 >>> 24)) & 0xFF; v1 &= M24; if (v2 === 0 && (v1 & 1) === 0) v1 &= 0xFFFFFC; v1 &= 0xFFFFFE; v2 = 0; }
    else if ((sr & (1 << SR_S1)) !== 0) { v2 += 1 << 22; v1 += v2 >>> 24; v0 = (v0 + (v1 >>> 24)) & 0xFF; v2 &= M24; v1 &= M24; if ((v2 & 0x7FFFFF) === 0) v2 = 0; v2 &= 0x800000; }
    else { v2 += 1 << 23; v1 += v2 >>> 24; v0 = (v0 + (v1 >>> 24)) & 0xFF; v2 &= M24; v1 &= M24; if (v2 === 0) v1 &= 0xFFFFFE; v2 = 0; }
  }
  r[DSP_A2 + d] = v0; r[DSP_A1 + d] = v1; r[DSP_A0 + d] = v2;
  ccrEUNZ3(c, v0, v1, v2);
  if (accumulate) r[DSP_SR] = (r[DSP_SR] & ~(1 << SR_V)) | (flags & 0xFE); else r[DSP_SR] &= ~(1 << SR_V);
}
// The helpers a translated function or parallel operation calls, handed to it as its own locals
const DSP_HELPERS = { calcEA, updateRn, dspReadMemory, dspWriteMemory, dspReadAccu24, pmWriteAcc, pmWriteReg, dspWriteReg, dspLoopPop, dspStep, npShiftImm, npAddSub, npLogic, npCmp, npMovecReg, fault, hex, aluTfrAcc, aluTst, aluAccOp, aluCmpm, aluRnd, aluClr, aluNot, aluMax, aluXY, aluShift56, aluLsl, aluLsr, aluRol, aluRor, aluAbs, aluNeg, aluRegArith, aluRegCmpm, aluRegTfr, aluRegLogic, aluMultiply };
const DSP_HELPER_NAMES = Object.keys(DSP_HELPERS), DSP_HELPER_VALUES = DSP_HELPER_NAMES.map((k) => DSP_HELPERS[k]);
function dspFunction(src) { return new Function(...DSP_HELPER_NAMES, "return function (c) {\n" + src + "\n};")(...DSP_HELPER_VALUES); }
const DSP_ALU = new Array(256), DSP_ALU_SRC = new Array(256);
(function () {
  const undefinedOp = (op) => "fault(c.name + \" runs undefined parallel operation \" + hex(" + op + "));";
  const low = [
    "", "aluTfrAcc(c, 1, 0);", "aluAccOp(c, 1, 0, -1, false, false);", "aluTst(c, 0);", undefinedOp(4), "aluAccOp(c, 1, 0, 0, true, true);", "aluAccOp(c, 1, 0, -1, true, false);", "aluCmpm(c, 1, 0);",
    undefinedOp(8), "aluTfrAcc(c, 0, 1);", "aluAccOp(c, 0, 1, -1, false, false);", "aluTst(c, 1);", undefinedOp(12), "aluAccOp(c, 0, 1, 0, true, true);", "aluAccOp(c, 0, 1, -1, true, false);", "aluCmpm(c, 0, 1);",
    "aluAccOp(c, 1, 0, 0, false, false);", "aluRnd(c, 0);", "aluAccOp(c, 1, 0, 1, false, false);", "aluClr(c, 0);", "aluAccOp(c, 1, 0, 0, true, false);", undefinedOp(0x15), "aluAccOp(c, 1, 0, 1, true, false);", "aluNot(c, 0);",
    "aluAccOp(c, 0, 1, 0, false, false);", "aluRnd(c, 1);", "aluAccOp(c, 0, 1, 1, false, false);", "aluClr(c, 1);", "aluAccOp(c, 0, 1, 0, true, false);", "aluMax(c);", "aluAccOp(c, 0, 1, 1, true, false);", "aluNot(c, 1);",
    "aluXY(c, 0, 0, false, false);", "aluXY(c, 0, 0, false, true);", "aluShift56(c, 0, false);", "aluLsr(c, 0);", "aluXY(c, 0, 0, true, false);", "aluXY(c, 0, 0, true, true);", "aluAbs(c, 0);", "aluRor(c, 0);",
    "aluXY(c, 0, 1, false, false);", "aluXY(c, 0, 1, false, true);", "aluShift56(c, 1, false);", "aluLsr(c, 1);", "aluXY(c, 0, 1, true, false);", "aluXY(c, 0, 1, true, true);", "aluAbs(c, 1);", "aluRor(c, 1);",
    "aluXY(c, 1, 0, false, false);", "aluXY(c, 1, 0, false, true);", "aluShift56(c, 0, true);", "aluLsl(c, 0);", "aluXY(c, 1, 0, true, false);", "aluXY(c, 1, 0, true, true);", "aluNeg(c, 0);", "aluRol(c, 0);",
    "aluXY(c, 1, 1, false, false);", "aluXY(c, 1, 1, false, true);", "aluShift56(c, 1, true);", "aluLsl(c, 1);", "aluXY(c, 1, 1, true, false);", "aluXY(c, 1, 1, true, true);", "aluNeg(c, 1);", "aluRol(c, 1);",
  ];
  for (let i = 0; i < 64; i++) DSP_ALU_SRC[i] = low[i];
  const sources = [DSP_X0, DSP_Y0, DSP_X1, DSP_Y1];
  for (let s = 0; s < 4; s++) for (let d = 0; d < 2; d++) {
    const base = 0x40 + s * 16 + d * 8, src = sources[s];
    DSP_ALU_SRC[base] = "aluRegArith(c, " + src + ", " + d + ", false, false);";
    DSP_ALU_SRC[base + 1] = "aluRegTfr(c, " + src + ", " + d + ");";
    DSP_ALU_SRC[base + 2] = "aluRegLogic(c, " + src + ", " + d + ", 0);";
    DSP_ALU_SRC[base + 3] = "aluRegLogic(c, " + src + ", " + d + ", 1);";
    DSP_ALU_SRC[base + 4] = "aluRegArith(c, " + src + ", " + d + ", true, false);";
    DSP_ALU_SRC[base + 5] = "aluRegArith(c, " + src + ", " + d + ", true, true);";
    DSP_ALU_SRC[base + 6] = "aluRegLogic(c, " + src + ", " + d + ", 2);";
    DSP_ALU_SRC[base + 7] = "aluRegCmpm(c, " + src + ", " + d + ");";
  }
  const pairs = [[DSP_X0, DSP_X0], [DSP_Y0, DSP_Y0], [DSP_X1, DSP_X0], [DSP_Y1, DSP_Y0], [DSP_X0, DSP_Y1], [DSP_Y0, DSP_X0], [DSP_X1, DSP_Y0], [DSP_Y1, DSP_X1]];
  for (let p = 0; p < 8; p++) for (let d = 0; d < 2; d++) for (let op = 0; op < 8; op++) {
    const [s1, s2] = pairs[p], negate = op >= 4 ? 1 : 0, round = (op & 1) !== 0, accumulate = (op & 2) !== 0;
    DSP_ALU_SRC[0x80 + p * 16 + d * 8 + op] = "aluMultiply(c, " + s1 + ", " + s2 + ", " + d + ", " + negate + ", " + round + ", " + accumulate + ");";
  }
  for (let i = 0; i < 256; i++) DSP_ALU[i] = dspFunction(DSP_ALU_SRC[i]);
})();
// ---- the parallel move instructions ----
function pmWriteAcc(c, d, v) { c.r[DSP_A0 + d] = 0; c.r[DSP_A1 + d] = v & M24; c.r[DSP_A2 + d] = (v & (1 << 23)) !== 0 ? 0xFF : 0; }
function pmWriteReg(c, num, v) { if (num === DSP_A) pmWriteAcc(c, 0, v); else if (num === DSP_B) pmWriteAcc(c, 1, v); else { const bits = DSP_REG_MASK[num]; c.r[num] = bits === 0 ? v >>> 0 : (v & ((1 << bits) - 1)) >>> 0; } }
function pm0(c, w) {
  const space = (w >>> 15) & 1, d = (w >>> 16) & 1, addr = calcEA(c, (w >>> 8) & 0x3F);
  const saveAcc = dspReadAccu24(c, DSP_A + d), saveXY = c.r[DSP_X0 + (space << 1)];
  DSP_ALU[w & 0xFF](c);
  dspWriteMemory(c, space, addr, saveAcc);
  pmWriteAcc(c, d, saveXY);
}
function pm1(c, w) {
  const space = (w >>> 14) & 1, addr = calcEA(c, (w >>> 8) & 0x3F), immediate = dspEAImmediate;
  const sel = space !== 0 ? (w >>> 16) & 3 : (w >>> 18) & 3, reg1 = sel === 0 ? (space !== 0 ? DSP_Y0 : DSP_X0) : sel === 1 ? (space !== 0 ? DSP_Y1 : DSP_X1) : sel === 2 ? DSP_A : DSP_B;
  let save1;
  if ((w & (1 << 15)) !== 0) save1 = immediate ? addr : dspReadMemory(c, space, addr);
  else save1 = dspReadReg24(c, reg1);
  const reg2 = DSP_A + (space !== 0 ? (w >>> 19) & 1 : (w >>> 17) & 1), save2 = dspReadAccu24(c, reg2);
  DSP_ALU[w & 0xFF](c);
  if ((w & (1 << 15)) !== 0) pmWriteReg(c, reg1, save1); else dspWriteMemory(c, space, addr, save1);
  c.r[space !== 0 ? DSP_X0 + ((w >>> 18) & 1) : DSP_Y0 + ((w >>> 16) & 1)] = save2;
}
function pm2(c, w) {
  if ((w & 0xFFFF00) === 0x200000) { DSP_ALU[w & 0xFF](c); return; }
  if ((w & 0xFFE000) === 0x204000) { calcEA(c, (w >>> 8) & 0x1F); DSP_ALU[w & 0xFF](c); return; }
  if ((w & 0xFC0000) === 0x200000) {
    const src = (w >>> 13) & 0x1F, dst = (w >>> 8) & 0x1F, save = dspReadReg24(c, src);
    DSP_ALU[w & 0xFF](c);
    pmWriteReg(c, dst, save);
    return;
  }
  DSP_ALU[w & 0xFF](c);
  const dst = (w >>> 16) & 0x1F;
  let v = (w >>> 8) & 0xFF;
  if (dst === DSP_X0 || dst === DSP_X1 || dst === DSP_Y0 || dst === DSP_Y1 || dst === DSP_A || dst === DSP_B) v <<= 16;
  pmWriteReg(c, dst, v);
}
function pm4(c, w) {
  if ((w & 0xF40000) !== 0x400000) { pm5(c, w); return; }
  const value = (w >>> 8) & 0x3F, addr = (w & (1 << 14)) !== 0 ? calcEA(c, value) : value;
  const num = ((w >>> 16) & 3) | ((w >>> 17) & 4);
  let lx, ly;
  if ((w & (1 << 15)) !== 0) { lx = dspReadMemory(c, DSP_SPACE_X, addr); ly = dspReadMemory(c, DSP_SPACE_Y, addr); }
  else {
    switch (num) {
    case 0: lx = c.r[DSP_A1]; ly = c.r[DSP_A0]; break;
    case 1: lx = c.r[DSP_B1]; ly = c.r[DSP_B0]; break;
    case 2: lx = c.r[DSP_X1]; ly = c.r[DSP_X0]; break;
    case 3: lx = c.r[DSP_Y1]; ly = c.r[DSP_Y0]; break;
    case 4: lx = dspReadAccu24(c, DSP_A); ly = c.limited ? ((lx & (1 << 23)) !== 0 ? 0 : M24) : c.r[DSP_A0]; break;
    case 5: lx = dspReadAccu24(c, DSP_B); ly = c.limited ? ((lx & (1 << 23)) !== 0 ? 0 : M24) : c.r[DSP_B0]; break;
    case 6: lx = dspReadAccu24(c, DSP_A); ly = dspReadAccu24(c, DSP_B); break;
    case 7: lx = dspReadAccu24(c, DSP_B); ly = dspReadAccu24(c, DSP_A); break;
    }
  }
  DSP_ALU[w & 0xFF](c);
  if ((w & (1 << 15)) !== 0) {
    switch (num) {
    case 0: c.r[DSP_A1] = lx; c.r[DSP_A0] = ly; break;
    case 1: c.r[DSP_B1] = lx; c.r[DSP_B0] = ly; break;
    case 2: c.r[DSP_X1] = lx; c.r[DSP_X0] = ly; break;
    case 3: c.r[DSP_Y1] = lx; c.r[DSP_Y0] = ly; break;
    case 4: c.r[DSP_A0] = ly; c.r[DSP_A1] = lx; c.r[DSP_A2] = (lx & (1 << 23)) !== 0 ? 0xFF : 0; break;
    case 5: c.r[DSP_B0] = ly; c.r[DSP_B1] = lx; c.r[DSP_B2] = (lx & (1 << 23)) !== 0 ? 0xFF : 0; break;
    case 6: pmWriteAcc(c, 0, lx); pmWriteAcc(c, 1, ly); break;
    case 7: pmWriteAcc(c, 1, lx); pmWriteAcc(c, 0, ly); break;
    }
  } else { dspWriteMemory(c, DSP_SPACE_X, addr, lx); dspWriteMemory(c, DSP_SPACE_Y, addr, ly); }
}
function pm5(c, w) {
  const value = (w >>> 8) & 0x3F;
  let addr, immediate = false;
  if ((w & (1 << 14)) !== 0) { addr = calcEA(c, value); immediate = dspEAImmediate; } else addr = value;
  const space = (w >>> 19) & 1, num = ((w >>> 16) & 7) | ((w >>> 17) & 0x18);
  let v;
  if ((w & (1 << 15)) !== 0) v = immediate ? addr : dspReadMemory(c, space, addr); else v = dspReadReg24(c, num);
  DSP_ALU[w & 0xFF](c);
  if ((w & (1 << 15)) !== 0) pmWriteReg(c, num, v); else dspWriteMemory(c, space, addr, v);
}
function pm8(c, w) {
  let ea1 = (w >>> 8) & 0x1F, ea2 = ((w >>> 13) & 3) | ((w >>> 17) & 0x18);
  if ((ea1 >>> 3) === 0) ea1 |= 1 << 5;
  if ((ea1 & 4) === 0) ea2 |= 4;
  if ((ea2 >>> 3) === 0) ea2 |= 1 << 5;
  const xAddr = calcEA(c, ea1), yAddr = calcEA(c, ea2);
  const s1 = (w >>> 18) & 3, s2 = (w >>> 16) & 3, reg1 = s1 === 0 ? DSP_X0 : s1 === 1 ? DSP_X1 : s1 === 2 ? DSP_A : DSP_B, reg2 = s2 === 0 ? DSP_Y0 : s2 === 1 ? DSP_Y1 : s2 === 2 ? DSP_A : DSP_B;
  const save1 = (w & (1 << 15)) !== 0 ? dspReadMemory(c, DSP_SPACE_X, xAddr) : dspReadReg24(c, reg1);
  const save2 = (w & (1 << 22)) !== 0 ? dspReadMemory(c, DSP_SPACE_Y, yAddr) : dspReadReg24(c, reg2);
  DSP_ALU[w & 0xFF](c);
  if ((w & (1 << 15)) !== 0) { if (reg1 === DSP_A || reg1 === DSP_B) pmWriteAcc(c, reg1 - DSP_A, save1); else c.r[reg1] = save1; } else dspWriteMemory(c, DSP_SPACE_X, xAddr, save1);
  if ((w & (1 << 22)) !== 0) { if (reg2 === DSP_A || reg2 === DSP_B) pmWriteAcc(c, reg2 - DSP_A, save2); else c.r[reg2] = save2; } else dspWriteMemory(c, DSP_SPACE_Y, yAddr, save2);
}
const DSP_PM = [pm0, pm1, pm2, pm2, pm4, pm5, pm5, pm5, pm8, pm8, pm8, pm8, pm8, pm8, pm8, pm8];
// ---- the non parallel instructions ----
function npWord2(c) { return dspReadMemory(c, DSP_SPACE_P, c.pc + 1); }
function npAddSub(c, x, d, subtract) { accLoad(c, d, ACC_D); ACC_S[2] = 0; ACC_S[1] = x & M24; ACC_S[0] = (x & (1 << 23)) !== 0 ? 0xFF : 0; const sr = subtract ? sub56(ACC_S, ACC_D) : add56(ACC_S, ACC_D); accStore(c, d, ACC_D); ccrEUNZ(c, ACC_D); setVC(c, sr); }
function npLogic(c, x, d, op) { const reg = DSP_A1 + d; if (op === 0) c.r[reg] &= x; else if (op === 1) c.r[reg] |= x; else c.r[reg] = (c.r[reg] ^ x) & M24; setNZ24(c, reg); }
function npCmp(c, x, d) { accLoad(c, d, ACC_D); ACC_S[2] = 0; ACC_S[1] = x; ACC_S[0] = 0; const sr = sub56(ACC_S, ACC_D); ccrEUNZ(c, ACC_D); setVC(c, sr); }
function npAndi(c, w) { const v = (w >>> 8) & 0xFF; switch (w & 3) { case 0: c.r[DSP_SR] &= (v << 8) | 0xFF; return; case 1: c.r[DSP_SR] &= 0xFF00 | v; return; case 2: c.r[DSP_OMR] &= v; return; } fault(c.name + " runs andi on register " + (w & 3)); }
function npOri(c, w) { const v = (w >>> 8) & 0xFF; switch (w & 3) { case 0: c.r[DSP_SR] |= v << 8; return; case 1: c.r[DSP_SR] |= v; return; case 2: c.r[DSP_OMR] |= v; return; } fault(c.name + " runs ori on register " + (w & 3)); }
function npShiftImm(c, w, left) { const s = (w >>> 7) & 1, d = w & 1, n = (w >>> 1) & 0x3F; accLoad(c, s, ACC_D); const sr = left ? asl56(ACC_D, n) : asr56(ACC_D, n); accStore(c, d, ACC_D); setVC(c, sr); ccrEUNZ(c, ACC_D); }
function npBranch(c, displacement) { c.pc = (c.pc + displacement) & M24; c.len = 0; }
function npBccLong(c, w) { const x = npWord2(c); c.len++; if (calcCC(c, w & 0xF)) npBranch(c, x); }
function npBccImm(c, w) { const x = (w & 0x1F) + ((w & (0xF << 6)) >>> 1); if (calcCC(c, (w >>> 12) & 0xF)) npBranch(c, (x << 23) >> 23); }
function npBraLong(c, w) { const x = npWord2(c); c.len++; npBranch(c, x); }
function npBraImm(c, w) { const x = (w & 0x1F) + ((w & (0xF << 6)) >>> 1); npBranch(c, (x << 23) >> 23); }
function npBitMemory(c, w, addr, op) {
  const space = (w >>> 6) & 1, bit = w & 0x1F;
  let v = dspReadMemory(c, space, addr);
  const carry = (v >>> bit) & 1;
  if (op === 0) v = carry !== 0 ? v - (1 << bit) : v + (1 << bit); else if (op === 1) v &= ~(1 << bit); else if (op === 2) v |= 1 << bit;
  if (op !== 3) dspWriteMemory(c, space, addr, v);
  c.r[DSP_SR] = (c.r[DSP_SR] & ~(1 << SR_C)) | (carry << SR_C);
}
function npBitReg(c, w, op) {
  const num = (w >>> 8) & 0x3F, bit = w & 0x1F;
  let v = dspReadReg24(c, num);
  const carry = (v >>> bit) & 1;
  if (op === 0) v = carry !== 0 ? v - (1 << bit) : v + (1 << bit); else if (op === 1) v &= ~(1 << bit); else if (op === 2) v |= 1 << bit;
  if (op !== 3) dspWriteReg(c, num, v);
  c.r[DSP_SR] = (c.r[DSP_SR] & ~(1 << SR_C)) | (carry << SR_C);
}
function npBitAA(c, w, op) { npBitMemory(c, w, (w >>> 8) & 0x3F, op); }
function npBitEA(c, w, op) { npBitMemory(c, w, calcEA(c, (w >>> 8) & 0x3F), op); }
function npBitPP(c, w, op) { npBitMemory(c, w, DSP_PP_BASE + ((w >>> 8) & 0x3F), op); }
function npBrPP(c, w, set) { const x = npWord2(c); c.len++; const v = dspReadMemory(c, (w >>> 6) & 1, DSP_PP_BASE + ((w >>> 8) & 0x3F)); if (((v >>> (w & 0x1F)) & 1) === (set ? 1 : 0)) npBranch(c, x); }
function npBrReg(c, w, set) { const x = npWord2(c); c.len++; const v = dspReadReg24(c, (w >>> 8) & 0x3F); if (((v >>> (w & 0x1F)) & 1) === (set ? 1 : 0)) npBranch(c, x); }
function npCall(c, target) { if (c.intState !== 2) dspStackPush(c, c.pc + c.len, c.r[DSP_SR], 0); else c.intState = 1; c.pc = target; c.len = 0; }
function npBsrLong(c, w) { const x = npWord2(c); c.len++; npCall(c, (c.pc + x) & M24); }
function npBsrImm(c, w) { const x = (w & 0x1F) + ((w & (0xF << 6)) >>> 1); npCall(c, (c.pc + ((x << 23) >> 23)) & M24); }
function npCmpu(c, w) {
  const ggg = (w >>> 1) & 7, d = w & 1;
  const src = ggg === 0 ? (d !== 0 ? DSP_A : DSP_B) : ggg === 4 ? DSP_X0 : ggg === 5 ? DSP_Y0 : ggg === 6 ? DSP_X1 : ggg === 7 ? DSP_Y1 : -1;
  if (src < 0) fault(c.name + " runs cmpu with source " + ggg);
  accLoad(c, d, ACC_D);
  reg24Load(c, 0, ACC_S); ACC_S[1] = dspReadReg24(c, src); ACC_S[0] = (ACC_S[1] & (1 << 23)) !== 0 ? 0xFF : 0; ACC_S[2] = 0;
  const sr = sub56(ACC_S, ACC_D);
  c.r[DSP_SR] &= ~((1 << SR_V) | (1 << SR_C) | (1 << SR_Z) | (1 << SR_N));
  c.r[DSP_SR] |= sr & (1 << SR_C);
  if (ACC_D[0] === 0 && ACC_D[1] === 0 && ACC_D[2] === 0) c.r[DSP_SR] |= 1 << SR_Z;
  c.r[DSP_SR] |= (ACC_D[0] >>> 4) & 0x8;
}
function npIncDec(c, w, increment) { const d = w & 1; accLoad(c, d, ACC_D); ACC_S[2] = 1; ACC_S[1] = 0; ACC_S[0] = 0; const sr = increment ? add56(ACC_S, ACC_D) : sub56(ACC_S, ACC_D); accStore(c, d, ACC_D); ccrEUNZ(c, ACC_D); setVC(c, sr); }
function npDiv(c, w) {
  const srcs = [DSP_X0, DSP_Y0, DSP_X1, DSP_Y1], src = srcs[(w >>> 4) & 3], d = (w >>> 3) & 1;
  ACC_S[2] = 0; ACC_S[1] = c.r[src]; ACC_S[0] = (ACC_S[1] & (1 << 23)) !== 0 ? 0xFF : 0;
  accLoad(c, d, ACC_D);
  let sr;
  if ((((ACC_D[0] >>> 7) & 1) ^ ((ACC_S[1] >>> 23) & 1)) !== 0) { sr = asl56(ACC_D, 1); add56(ACC_S, ACC_D); } else { sr = asl56(ACC_D, 1); sub56(ACC_S, ACC_D); }
  ACC_D[2] |= (c.r[DSP_SR] >>> SR_C) & 1;
  accStore(c, d, ACC_D);
  c.r[DSP_SR] &= ~((1 << SR_C) | (1 << SR_V));
  c.r[DSP_SR] |= (1 - ((ACC_D[0] >>> 7) & 1)) << SR_C;
  c.r[DSP_SR] |= sr & ((1 << SR_L) | (1 << SR_V));
}
function npDoStart(c, count) { dspStackPush(c, c.r[DSP_LA], c.r[DSP_LC], 0); c.r[DSP_LA] = npWord2(c) & M16; c.len++; dspStackPush(c, c.pc + c.len, c.r[DSP_SR], 0); c.r[DSP_SR] |= 1 << SR_LF; c.r[DSP_LC] = count & M16; }
function npDoAA(c, w) { npDoStart(c, dspReadMemory(c, (w >>> 6) & 1, (w >>> 8) & 0x3F)); }
function npDoImm(c, w) { npDoStart(c, ((w >>> 8) & 0xFF) + ((w & 0xF) << 8)); }
function npDoEA(c, w) { const space = (w >>> 6) & 1, addr = calcEA(c, (w >>> 8) & 0x3F); npDoStart(c, dspReadMemory(c, space, addr)); }
function npDoReg(c, w) { dspStackPush(c, c.r[DSP_LA], c.r[DSP_LC], 0); c.r[DSP_LA] = npWord2(c) & M16; c.len++; c.r[DSP_LC] = dspReadReg24(c, (w >>> 8) & 0x3F) & M16; dspStackPush(c, c.pc + c.len, c.r[DSP_SR], 0); c.r[DSP_SR] |= 1 << SR_LF; }
function npDorStart(c, count) { const x = npWord2(c); c.len++; dspStackPush(c, c.r[DSP_LA], c.r[DSP_LC], 0); c.r[DSP_LA] = (c.pc + x) & M16; dspStackPush(c, c.pc + c.len, c.r[DSP_SR], 0); c.r[DSP_SR] |= 1 << SR_LF; c.r[DSP_LC] = count & M16; }
function npDorImm(c, w) { npDorStart(c, ((w >>> 8) & 0xFF) + ((w & 0xF) << 8)); }
function npDorReg(c, w) { npDorStart(c, dspReadReg24(c, (w >>> 8) & 0x3F)); }
function npEnddo(c, w) { dspLoopPop(c); }
function npIllegal(c, w) { dspAddInterrupt(c, DSP_INTER_ILLEGAL); }
function npJump(c, target) { c.pc = target; c.len = 0; }
function npJccImm(c, w) { if (calcCC(c, (w >>> 12) & 0xF)) npJump(c, w & 0xFFF); }
function npJccEA(c, w) { const target = calcEA(c, (w >>> 8) & 0x3F); if (calcCC(c, w & 0xF)) npJump(c, target); }
function npJmpEA(c, w) { npJump(c, calcEA(c, (w >>> 8) & 0x3F)); }
function npJmpImm(c, w) { npJump(c, w & 0xFFF); }
function npJsccImm(c, w) { if (calcCC(c, (w >>> 12) & 0xF)) { dspStackPush(c, c.pc + c.len, c.r[DSP_SR], 0); npJump(c, w & 0xFFF); } }
function npJsccEA(c, w) { const target = calcEA(c, (w >>> 8) & 0x3F); if (calcCC(c, w & 0xF)) { dspStackPush(c, c.pc + c.len, c.r[DSP_SR], 0); npJump(c, target); } }
function npJsrImm(c, w) { npCall(c, w & 0xFFF); }
function npJsrEA(c, w) { npCall(c, calcEA(c, (w >>> 8) & 0x3F)); }
// A jump on a bit of memory or a register, to the address in the second word, calling when asked
function npJBit(c, w, value, set, call) {
  const target = npWord2(c);
  if ((((value >>> (w & 0xF)) & 1) !== 0) === set) { if (call) { dspStackPush(c, c.pc + 2, c.r[DSP_SR], 0); } c.pc = target; c.len = 0; return; }
  c.len++;
}
function npJBitAA(c, w, set, call) { npJBit(c, w, dspReadMemory(c, (w >>> 6) & 1, (w >>> 8) & 0x3F), set, call); }
function npJBitEA(c, w, set, call) { const addr = calcEA(c, (w >>> 8) & 0x3F); npJBit(c, w, dspReadMemory(c, (w >>> 6) & 1, addr), set, call); }
function npJBitPP(c, w, set, call) { npJBit(c, w, dspReadMemory(c, (w >>> 6) & 1, DSP_PP_BASE + ((w >>> 8) & 0x3F)), set, call); }
function npJBitReg(c, w, set, call) { npJBit(c, w, dspReadReg24(c, (w >>> 8) & 0x3F), set, call); }
function npLslImm(c, w) { const d = w & 1; let n = (w >>> 1) & 0x1F; while (n-- > 0) aluLsl(c, d); }
function npLua(c, w) { const src = (w >>> 8) & 7, saved = c.r[DSP_R0 + src]; calcEA(c, (w >>> 8) & 0x1F); const v = c.r[DSP_R0 + src]; c.r[DSP_R0 + src] = saved; const dst = w & 7; if ((w & 8) !== 0) c.r[DSP_N0 + dst] = v; else c.r[DSP_R0 + dst] = v; }
function npLuaRel(c, w) { const aa = ((w >>> 4) & 0xF) + (((w >>> 11) & 7) << 4), v = (c.r[DSP_R0 + ((w >>> 8) & 7)] + ((aa << 25) >> 25)) & M24, dst = w & 7; if ((w & 8) !== 0) c.r[DSP_N0 + dst] = v; else c.r[DSP_R0 + dst] = v; }
function npReadControl(c, num) { if (num === DSP_SSH) { dspStackPop(c); return c.popH; } return c.r[num]; }
function npMovecReg(c, w) {
  const num2 = (w >>> 8) & 0x3F, num1 = w & 0x3F;
  if ((w & (1 << 15)) !== 0) { const bits = DSP_REG_MASK[num1]; let v = dspReadReg24(c, num2); if (bits !== 0) v &= (1 << bits) - 1; dspWriteReg(c, num1, v); return; }
  const v = npReadControl(c, num1);
  if (num2 === DSP_A || num2 === DSP_B) pmWriteAcc(c, num2 - DSP_A, v); else { const bits = DSP_REG_MASK[num2]; c.r[num2] = bits === 0 ? v >>> 0 : v & ((1 << bits) - 1); }
}
function npMovecMemory(c, w, addr) {
  const num = w & 0x3F, space = (w >>> 6) & 1;
  if ((w & (1 << 15)) !== 0) { const bits = DSP_REG_MASK[num]; let v = dspEAImmediate ? addr : dspReadMemory(c, space, addr); if (bits !== 0) v &= (1 << bits) - 1; dspWriteReg(c, num, v); return; }
  dspWriteMemory(c, space, addr, npReadControl(c, num));
}
function npMovecAA(c, w) { dspEAImmediate = false; npMovecMemory(c, w, (w >>> 8) & 0x3F); }
function npMovecEA(c, w) { const addr = calcEA(c, (w >>> 8) & 0x3F); npMovecMemory(c, w, addr); }
function npMovecImm(c, w) { const num = w & 0x3F, bits = DSP_REG_MASK[num]; let v = (w >>> 8) & 0xFF; if (bits !== 0) v &= (1 << bits) - 1; dspWriteReg(c, num, v); }
function npMovem(c, w, addr) {
  const num = w & 0x3F;
  if ((w & (1 << 15)) !== 0) { const bits = DSP_REG_MASK[num]; let v = dspReadMemory(c, DSP_SPACE_P, addr); if (bits !== 0) v &= (1 << bits) - 1; dspWriteReg(c, num, v); return; }
  let v;
  if (num === DSP_SSH) { dspStackPop(c); v = c.popH; } else v = dspReadReg24(c, num);
  dspWriteMemory(c, DSP_SPACE_P, addr, v);
}
function npMovemAA(c, w) { npMovem(c, w, (w >>> 8) & 0x3F); }
function npMovemEA(c, w) { npMovem(c, w, calcEA(c, (w >>> 8) & 0x3F)); }
function npMovep0(c, w) {
  const addr = DSP_PP_BASE + (w & 0x3F), space = (w >>> 16) & 1, num = (w >>> 8) & 0x3F;
  if ((w & (1 << 15)) !== 0) { let v; if (num === DSP_SSH) { dspStackPop(c); v = c.popH; } else v = dspReadReg24(c, num); dspWriteMemory(c, space, addr, v); return; }
  const bits = DSP_REG_MASK[num]; let v = dspReadMemory(c, space, addr); if (bits !== 0) v &= (1 << bits) - 1; dspWriteReg(c, num, v);
}
function npMovep1(c, w) {
  const xy = DSP_PP_BASE + (w & 0x3F), paddr = calcEA(c, (w >>> 8) & 0x3F), space = (w >>> 16) & 1;
  if ((w & (1 << 15)) !== 0) dspWriteMemory(c, space, xy, dspReadMemory(c, DSP_SPACE_P, paddr)); else dspWriteMemory(c, DSP_SPACE_P, paddr, dspReadMemory(c, space, xy));
}
function npMovep23(c, w) {
  const per = DSP_PP_BASE + (w & 0x3F), perSpace = (w >>> 16) & 1, eaSpace = (w >>> 6) & 1, addr = calcEA(c, (w >>> 8) & 0x3F), immediate = dspEAImmediate;
  if ((w & (1 << 15)) !== 0) dspWriteMemory(c, perSpace, per, immediate ? addr : dspReadMemory(c, eaSpace, addr)); else dspWriteMemory(c, eaSpace, addr, dspReadMemory(c, perSpace, per));
}
function npMovepXqq(c, w) {
  const xAddr = DSP_PERIPH_BASE + (w & 0x3F), eaSpace = (w >>> 6) & 1, addr = calcEA(c, (w >>> 8) & 0x3F), immediate = dspEAImmediate;
  if ((w & (1 << 15)) !== 0) dspWriteMemory(c, DSP_SPACE_X, xAddr, immediate ? addr : dspReadMemory(c, eaSpace, addr)); else dspWriteMemory(c, eaSpace, addr, dspReadMemory(c, DSP_SPACE_X, xAddr));
}
function npMoveXLong(c, w) {
  const x = npWord2(c); c.len++;
  const write = ((w >>> 6) & 1) !== 0, num = w & 0x3F, addr = (c.r[DSP_R0 + ((w >>> 8) & 7)] + x) & M24;
  if (!write) dspWriteMemory(c, DSP_SPACE_X, addr, dspReadReg24(c, num)); else dspWriteReg(c, num, dspReadMemory(c, DSP_SPACE_X, addr));
}
function npMoveXYImm(c, w, space) {
  const xxx = (((w >>> 11) & 0x3F) << 1) + ((w >>> 6) & 1), write = ((w >>> 4) & 1) !== 0, num = w & 0xF, addr = (c.r[DSP_R0 + ((w >>> 8) & 7)] + ((xxx << 25) >> 25)) & M24;
  if (!write) dspWriteMemory(c, space, addr, dspReadReg24(c, num)); else dspWriteReg(c, num, dspReadMemory(c, space, addr));
}
function npMpyi(c, w) {
  const x = npWord2(c); c.len++;
  const srcs = [DSP_X0, DSP_Y0, DSP_X1, DSP_Y1], src = srcs[(w >>> 4) & 3], negate = (w >>> 2) & 1, d = (w >>> 3) & 1;
  mul56(x, c.r[src], ACC_S, negate);
  accStore(c, d, ACC_S);
  ccrEUNZ(c, ACC_S);
  c.r[DSP_SR] &= ~(1 << SR_V);
}
function npNorm(c, w) {
  const sr = c.r[DSP_SR], e = (sr >>> SR_E) & 1, euz = (e ^ 1) & ((sr >>> SR_U) & 1) & (((sr >>> SR_Z) & 1) ^ 1), d = (w >>> 3) & 1, rreg = DSP_R0 + ((w >>> 8) & 7);
  accLoad(c, d, ACC_D);
  let newsr = 0;
  if (euz !== 0) { newsr = asl56(ACC_D, 1); c.r[rreg] = (c.r[rreg] - 1) & M16; } else if (e !== 0) { newsr = asr56(ACC_D, 1); c.r[rreg] = (c.r[rreg] + 1) & M16; }
  accStore(c, d, ACC_D);
  ccrEUNZ(c, ACC_D);
  setVC(c, newsr);
}
function npRepStart(c, count) { c.r[DSP_LCSAVE] = c.r[DSP_LC]; c.pcOnRep = 1; c.loopRep = 1; c.r[DSP_LC] = count & M16; }
function npRepAA(c, w) { npRepStart(c, dspReadMemory(c, (w >>> 6) & 1, (w >>> 8) & 0x3F)); }
function npRepImm(c, w) { npRepStart(c, ((w >>> 8) & 0xFF) + ((w & 0xF) << 8)); }
function npRepEA(c, w) { const space = (w >>> 6) & 1, addr = calcEA(c, (w >>> 8) & 0x3F); npRepStart(c, dspReadMemory(c, space, addr)); }
function npRepReg(c, w) { npRepStart(c, dspReadReg24(c, (w >>> 8) & 0x3F)); }
function npRti(c, w) { dspStackPop(c); c.pc = c.popH; c.r[DSP_SR] = c.popL; c.len = 0; }
function npRts(c, w) { dspStackPop(c); c.pc = c.popH; c.len = 0; }
const DSP_TCC_REGS = [[DSP_B, DSP_A], [DSP_A, DSP_B], null, null, null, null, null, null, [DSP_X0, DSP_A], [DSP_X0, DSP_B], [DSP_Y0, DSP_A], [DSP_Y0, DSP_B], [DSP_X1, DSP_A], [DSP_X1, DSP_B], [DSP_Y1, DSP_A], [DSP_Y1, DSP_B]];
function npTcc(c, w) {
  if (!calcCC(c, (w >>> 12) & 0xF)) return;
  const pair = DSP_TCC_REGS[(w >>> 3) & 0xF];
  if (pair === null) fault(c.name + " runs tcc with register pair " + ((w >>> 3) & 0xF));
  const [src, dst] = pair;
  let v0, v1, v2;
  if (src === DSP_A || src === DSP_B) { v0 = c.r[DSP_A0 + src - DSP_A]; v1 = c.r[DSP_A1 + src - DSP_A]; v2 = c.r[DSP_A2 + src - DSP_A]; } else { v0 = 0; v1 = c.r[src]; v2 = (v1 & (1 << 23)) !== 0 ? 0xFF : 0; }
  const d = dst - DSP_A;
  c.r[DSP_A2 + d] = v2; c.r[DSP_A1 + d] = v1; c.r[DSP_A0 + d] = v0;
  if ((w & (1 << 16)) !== 0) c.r[DSP_R0 + (w & 7)] = c.r[DSP_R0 + ((w >>> 8) & 7)];
}
function matchMMMRRR(w) { const rrr = (w >>> 8) & 7, mmm = (w >>> 11) & 7; return mmm !== 6 || rrr === 0 || rrr === 4; }
// The non parallel instruction templates in the order the hardware decodes them, each with its model or none
const DSP_TEMPLATES = [
  ["0000000101iiiiii1000d000", "add #xx, D", (c, w) => npAddSub(c, (w >>> 8) & 0x3F, (w >>> 3) & 1, false)],
  ["00000001010000001100d000", "add #xxxx, D", (c, w) => { const x = npWord2(c); c.len++; npAddSub(c, x, (w >>> 3) & 1, false); }],
  ["0000000101iiiiii1000d110", "and #xx, D", (c, w) => npLogic(c, (w >>> 8) & 0x3F, (w >>> 3) & 1, 0)],
  ["00000001010000001100d110", "and #xxxx, D", (c, w) => { const x = npWord2(c); c.len++; npLogic(c, x, (w >>> 3) & 1, 0); }],
  ["00000000iiiiiiii101110EE", "andi #xx, D", npAndi],
  ["0000110000011101SiiiiiiD", "asl #ii, S2, D", (c, w) => npShiftImm(c, w, true)],
  ["0000110000011110010SsssD", "asl S1, S2, D", null],
  ["0000110000011100SiiiiiiD", "asr #ii, S2, D", (c, w) => npShiftImm(c, w, false)],
  ["0000110000011110011SsssD", "asr S1, S2, D", null],
  ["00001101000100000100CCCC", "bcc xxxx", npBccLong],
  ["00000101CCCC01aaaa0aaaaa", "bcc xxx", npBccImm],
  ["0000110100011RRR0100CCCC", "bcc Rn", null],
  ["0000101101MMMRRR0S00bbbb", "bchg #n, [X or Y]:ea", (c, w) => npBitEA(c, w, 0), matchMMMRRR],
  ["0000101100aaaaaa0S00bbbb", "bchg #n, [X or Y]:aa", (c, w) => npBitAA(c, w, 0)],
  ["0000101110pppppp0S00bbbb", "bchg #n, [X or Y]:pp", (c, w) => npBitPP(c, w, 0)],
  ["0000000101qqqqqq0S0bbbbb", "bchg #n, [X or Y]:qq", null],
  ["0000101111DDDDDD010bbbbb", "bchg, #n, D", (c, w) => npBitReg(c, w, 0)],
  ["0000101001MMMRRR0S00bbbb", "bclr #n, [X or Y]:ea", (c, w) => npBitEA(c, w, 1), matchMMMRRR],
  ["0000101000aaaaaa0S00bbbb", "bclr #n, [X or Y]:aa", (c, w) => npBitAA(c, w, 1)],
  ["0000101010pppppp0S00bbbb", "bclr #n, [X or Y]:pp", (c, w) => npBitPP(c, w, 1)],
  ["0000000100qqqqqq0S00bbbb", "bclr #n, [X or Y]:qq", null],
  ["0000101011DDDDDD010bbbbb", "bclr #n, D", (c, w) => npBitReg(c, w, 1)],
  ["000011010001000011000000", "bra xxxx", npBraLong],
  ["00000101000011aaaa0aaaaa", "bra xxx", npBraImm],
  ["0000110100011RRR11000000", "bra Rn", null],
  ["0000110010MMMRRR0S0bbbbb", "brclr #n, [X or Y]:ea, xxxx", null, matchMMMRRR],
  ["0000110010aaaaaa1S0bbbbb", "brclr #n, [X or Y]:aa, xxxx", null],
  ["0000110011pppppp0S0bbbbb", "brclr #n, [X or Y]:pp, xxxx", (c, w) => npBrPP(c, w, false)],
  ["0000010010qqqqqq0S0bbbbb", "brclr #n, [X or Y]:qq, xxxx", null],
  ["0000110011DDDDDD100bbbbb", "brclr #n, S, xxxx", (c, w) => npBrReg(c, w, false)],
  ["00000000000000100001CCCC", "brkcc", null],
  ["0000110010MMMRRR0S1bbbbb", "brset #n, [X or Y]:ea, xxxx", null, matchMMMRRR],
  ["0000110010aaaaaa1S1bbbbb", "brset #n, [X or Y]:aa, xxxx", null],
  ["0000110011pppppp0S1bbbbb", "brset #n, [X or Y]:pp, xxxx", (c, w) => npBrPP(c, w, true)],
  ["0000010010qqqqqq0S1bbbbb", "brset #n, [X or Y]:qq, xxxx", null],
  ["0000110011DDDDDD101bbbbb", "brset #n, S, xxxx", (c, w) => npBrReg(c, w, true)],
  ["00001101000100000000CCCC", "bscc xxxx", null],
  ["00000101CCCC00aaaa0aaaaa", "bscc xxx", null],
  ["0000110100011RRR0000CCCC", "bscc Rn", null],
  ["0000110110MMMRRR0S0bbbbb", "bsclr #n, [X or Y]:ea, xxxx", null, matchMMMRRR],
  ["0000110110aaaaaa1S0bbbbb", "bsclr #n, [X or Y]:aa, xxxx", null],
  ["0000010010qqqqqq1S0bbbbb", "bsclr #n, [X or Y]:qq, xxxx", null],
  ["0000110111pppppp0S0bbbbb", "bsclr #n, [X or Y]:pp, xxxx", null],
  ["0000110111DDDDDD100bbbbb", "bsclr, #n, S, xxxx", null],
  ["0000101001MMMRRR0S1bbbbb", "bset #n, [X or Y]:ea", (c, w) => npBitEA(c, w, 2), matchMMMRRR],
  ["0000101000aaaaaa0S1bbbbb", "bset #n, [X or Y]:aa", (c, w) => npBitAA(c, w, 2)],
  ["0000101010pppppp0S1bbbbb", "bset #n, [X or Y]:pp", (c, w) => npBitPP(c, w, 2)],
  ["0000000100qqqqqq0S1bbbbb", "bset #n, [X or Y]:qq", null],
  ["0000101011DDDDDD011bbbbb", "bset, #n, D", (c, w) => npBitReg(c, w, 2)],
  ["000011010001000010000000", "bsr xxxx", npBsrLong],
  ["00000101000010aaaa0aaaaa", "bsr xxx", npBsrImm],
  ["0000110100011RRR10000000", "bsr Rn", null],
  ["0000110110MMMRRR0S1bbbbb", "bsset #n, [X or Y]:ea, xxxx", null, matchMMMRRR],
  ["0000110110aaaaaa1S1bbbbb", "bsset #n, [X or Y]:aa, xxxx", null],
  ["0000110111pppppp0S1bbbbb", "bsset #n, [X or Y]:pp, xxxx", null],
  ["0000010010qqqqqq1S1bbbbb", "bsset #n, [X or Y]:qq, xxxx", null],
  ["0000110111DDDDDD101bbbbb", "bsset #n, S, xxxx", null],
  ["0000101101MMMRRR0S10bbbb", "btst #n, [X or Y]:ea", (c, w) => npBitEA(c, w, 3), matchMMMRRR],
  ["0000101100aaaaaa0S10bbbb", "btst #n, [X or Y]:aa", (c, w) => npBitAA(c, w, 3)],
  ["0000101110pppppp0S10bbbb", "btst #n, [X or Y]:pp", (c, w) => npBitPP(c, w, 3)],
  ["0000000101qqqqqq0S10bbbb", "btst #n, [X or Y]:qq", null],
  ["0000101111DDDDDD0110bbbb", "btst #n, D", (c, w) => npBitReg(c, w, 3)],
  ["0000110000011110000000SD", "clb S, D", null],
  ["0000000101iiiiii1000d101", "cmp #xx, S2", (c, w) => npCmp(c, (w >>> 8) & 0x3F, (w >>> 3) & 1)],
  ["00000001010000001100d101", "cmp #xxxx, S2", (c, w) => { const x = npWord2(c); c.len++; npCmp(c, x, (w >>> 3) & 1); }],
  ["00001100000111111111gggd", "cmpu S1, S2", npCmpu],
  ["000000000000001000000000", "debug", null],
  ["00000000000000110000CCCC", "debugcc", null],
  ["00000000000000000000101d", "dec D", (c, w) => npIncDec(c, w, false)],
  ["000000011000000001JJd000", "div S, D", npDiv],
  ["000000010010010s1sdkQQQQ", "dmac S1, S2, D", null],
  ["0000011001MMMRRR0S000000", "do [X or Y]:ea, expr", npDoEA, matchMMMRRR],
  ["0000011000aaaaaa0S000000", "do [X or Y]:aa, expr", npDoAA],
  ["00000110iiiiiiii1000hhhh", "do #xxx, expr", npDoImm],
  ["0000011011DDDDDD00000000", "do S, expr", npDoReg],
  ["000000000000001000000011", "do_f", null],
  ["0000011001MMMRRR0S010000", "dor [X or Y]:ea, label", null, matchMMMRRR],
  ["0000011000aaaaaa0S010000", "dor [X or Y]:aa, label", null],
  ["00000110iiiiiiii1001hhhh", "dor #xxx, label", npDorImm],
  ["0000011011DDDDDD00010000", "dor S, label", npDorReg],
  ["000000000000001000000010", "dor_f", null],
  ["000000000000000010001100", "enddo", npEnddo],
  ["0000000101iiiiii1000d011", "eor #xx, D", (c, w) => npLogic(c, (w >>> 8) & 0x3F, (w >>> 3) & 1, 2)],
  ["00000001010000001100d011", "eor #xxxx, D", (c, w) => { const x = npWord2(c); c.len++; npLogic(c, x, (w >>> 3) & 1, 2); }],
  ["0000110000011010000sSSSD", "extract S1, S2, D", null],
  ["0000110000011000000s000D", "extract #CO, S2, D", null],
  ["0000110000011010100sSSSD", "extractu S1, S2, D", null],
  ["0000110000011000100s000D", "extractu #CO, S2, D", null],
  ["000000000000000000000101", "ill", npIllegal],
  ["00000000000000000000100d", "inc D", (c, w) => npIncDec(c, w, true)],
  ["00001100000110110qqqSSSD", "insert S1, S2, D", null],
  ["00001100000110010qqq000D", "insert #CO, S2, D", null],
  ["00001110CCCCaaaaaaaaaaaa", "jcc xxx", npJccImm],
  ["0000101011MMMRRR1010CCCC", "jcc ea", npJccEA, matchMMMRRR],
  ["0000101001MMMRRR1S00bbbb", "jclr #n, [X or Y]:ea, xxxx", (c, w) => npJBitEA(c, w, false, false), matchMMMRRR],
  ["0000101000aaaaaa1S00bbbb", "jclr #n, [X or Y]:aa, xxxx", (c, w) => npJBitAA(c, w, false, false)],
  ["0000101010pppppp1S00bbbb", "jclr #n, [X or Y]:pp, xxxx", (c, w) => npJBitPP(c, w, false, false)],
  ["0000000110qqqqqq1S00bbbb", "jclr #n, [X or Y]:qq, xxxx", null],
  ["0000101011DDDDDD0000bbbb", "jclr #n, S, xxxx", (c, w) => npJBitReg(c, w, false, false)],
  ["0000101011MMMRRR10000000", "jmp ea", npJmpEA, matchMMMRRR],
  ["000011000000aaaaaaaaaaaa", "jmp xxx", npJmpImm],
  ["00001111CCCCaaaaaaaaaaaa", "jscc xxx", npJsccImm],
  ["0000101111MMMRRR1010CCCC", "jscc ea", npJsccEA, matchMMMRRR],
  ["0000101101MMMRRR1S00bbbb", "jsclr #n, [X or Y]:ea, xxxx", (c, w) => npJBitEA(c, w, false, true), matchMMMRRR],
  ["0000101100MMMRRR1S00bbbb", "jsclr #n, [X or Y]:aa, xxxx", (c, w) => npJBitAA(c, w, false, true), matchMMMRRR],
  ["0000101110pppppp1S0bbbbb", "jsclr #n, [X or Y]:pp, xxxx", (c, w) => npJBitPP(c, w, false, true)],
  ["0000000111qqqqqq1S0bbbbb", "jsclr #n, [X or Y]:qq, xxxx", null],
  ["0000101111DDDDDD000bbbbb", "jsclr #n, S, xxxx", (c, w) => npJBitReg(c, w, false, true)],
  ["0000101001MMMRRR1S10bbbb", "jset #n, [X or Y]:ea, xxxx", (c, w) => npJBitEA(c, w, true, false), matchMMMRRR],
  ["0000101000MMMRRR1S10bbbb", "jset #n, [X or Y]:aa, xxxx", (c, w) => npJBitAA(c, w, true, false), matchMMMRRR],
  ["0000101010pppppp1S10bbbb", "jset #n, [X or Y]:pp, xxxx", (c, w) => npJBitPP(c, w, true, false)],
  ["0000000110qqqqqq1S10bbbb", "jset #n, [X or Y]:qq, xxxx", null],
  ["0000101011DDDDDD0010bbbb", "jset #n, S, xxxx", (c, w) => npJBitReg(c, w, true, false)],
  ["0000101111MMMRRR10000000", "jsr ea", npJsrEA, matchMMMRRR],
  ["000011010000aaaaaaaaaaaa", "jsr xxx", npJsrImm],
  ["0000101101MMMRRR1S10bbbb", "jsset #n, [X or Y]:ea, xxxx", (c, w) => npJBitEA(c, w, true, true), matchMMMRRR],
  ["0000101100aaaaaa1S10bbbb", "jsset #n, [X or Y]:aa, xxxx", (c, w) => npJBitAA(c, w, true, true)],
  ["0000101110pppppp1S1bbbbb", "jsset #n, [X or Y]:pp, xxxx", (c, w) => npJBitPP(c, w, true, true)],
  ["0000000111qqqqqq1S1bbbbb", "jsset #n, [X or Y]:qq, xxxx", null],
  ["0000101111DDDDDD001bbbbb", "jsset #n, S, xxxx", (c, w) => npJBitReg(c, w, true, true)],
  ["0000010011000RRR000ddddd", "lra Rn, D", null],
  ["0000010001000000010ddddd", "lra xxxx, D", null],
  ["000011000001111010iiiiiD", "lsl #ii, D", npLslImm],
  ["00001100000111100001sssD", "lsl S, D", null],
  ["000011000001111011iiiiiD", "lsr #ii, D", null],
  ["00001100000111100011sssD", "lsr S, D", null],
  ["00000100010MMRRR000ddddd", "lua ea, D", npLua],
  ["0000010000aaaRRRaaaadddd", "lua (Rn + aa), D", npLuaRel],
  ["00000001000sssss11QQdk10", "mac S, #n, D", null],
  ["000000010100000111qqdk10", "maci #xxxx, S, D", null],
  ["00000001001001101sdkQQQQ", "mac_s_u S1, S2, D", null],
  ["00000001000sssss11QQdk11", "macr S1, S2, D", null],
  ["000000010100000111qqdk11", "macri #xxxx, S, D", null],
  ["00001100000110111000sssD", "merge S, D", null],
  ["0000101001110RRR1WDDDDDD", "move X:(Rn + xxxx) <-> R", npMoveXLong],
  ["0000101101110RRR1WDDDDDD", "move Y:(Rn + xxxx) <-> R", null],
  ["0000001aaaaaaRRR1a0WDDDD", "move X:(Rn + xxx) <-> R", (c, w) => npMoveXYImm(c, w, DSP_SPACE_X)],
  ["0000001aaaaaaRRR1a1WDDDD", "move Y:(Rn + xxx) <-> R", (c, w) => npMoveXYImm(c, w, DSP_SPACE_Y)],
  ["00000101W1MMMRRR0s1ddddd", "movec [X or Y]:ea <-> R", npMovecEA, matchMMMRRR],
  ["00000101W0aaaaaa0s1ddddd", "movec [X or Y]:aa <-> R", npMovecAA, matchMMMRRR],
  ["00000100W1eeeeee101ddddd", "movec R1, R2", npMovecReg],
  ["00000101iiiiiiii101ddddd", "movec #xx, D1", npMovecImm],
  ["00000111W1MMMRRR10dddddd", "movem P:ea <-> R", npMovemEA, matchMMMRRR],
  ["00000111W0aaaaaa00dddddd", "movem P:aa <-> R", npMovemAA, matchMMMRRR],
  ["0000100sW1MMMRRR1Spppppp", "movep [X or Y]:ea <-> [X or Y]:pp", npMovep23, matchMMMRRR],
  ["00000111W1MMMRRR0Sqqqqqq", "movep [X or Y]:ea <-> X:qq", npMovepXqq, matchMMMRRR],
  ["00000111W0MMMRRR1Sqqqqqq", "movep [X or Y]:ea <-> Y:qq", null, matchMMMRRR],
  ["0000100sW1MMMRRR01pppppp", "movep [X or Y]:pp <-> P:ea", npMovep1, matchMMMRRR],
  ["000000001WMMMRRR0sqqqqqq", "movep [X or Y]:qq <-> P:ea", null, matchMMMRRR],
  ["0000100sW1dddddd00pppppp", "movep [X or Y]:pp <-> R", npMovep0],
  ["00000100W1dddddd1q0qqqqq", "movep X:qq <-> R", null],
  ["00000100W1dddddd0q1qqqqq", "movep Y:qq <-> R", null],
  ["00000001000sssss11QQdk00", "mpy S, #n, D", null],
  ["00000001001001111sdkQQQQ", "mpy_s_u S1, S2, D", null],
  ["000000010100000111qqdk00", "mpyi #xxxx, S, D", npMpyi],
  ["00000001000sssss11QQdk01", "mpyr S, #n, D", null],
  ["000000010100000111qqdk01", "mpyri #xxxx, S, D", null],
  ["000000000000000000000000", "nop", (c, w) => {}],
  ["0000000111011RRR0001d101", "norm Rn, D", npNorm],
  ["00001100000111100010sssD", "normf S, D", null],
  ["0000000101iiiiii1000d010", "or #xx, D", (c, w) => npLogic(c, (w >>> 8) & 0x3F, (w >>> 3) & 1, 1)],
  ["00000001010000001100d010", "or #xxxx, D", (c, w) => { const x = npWord2(c); c.len++; npLogic(c, x, (w >>> 3) & 1, 1); }],
  ["00000000iiiiiiii111110EE", "ori #xx, D", npOri],
  ["000000000000000000000011", "pflush", null],
  ["000000000000000000000001", "pflushun", null],
  ["000000000000000000000010", "pfree", null],
  ["0000101111MMMRRR10000001", "plock ea", null, matchMMMRRR],
  ["000000000000000000001111", "plockr xxxx", null],
  ["0000101011MMMRRR10000001", "punlock ea", null, matchMMMRRR],
  ["000000000000000000001110", "punlockr xxxx", null],
  ["0000011001MMMRRR0S100000", "rep [X or Y]:ea", npRepEA, matchMMMRRR],
  ["0000011000aaaaaa0S100000", "rep [X or Y]:aa", npRepAA],
  ["00000110iiiiiiii1010hhhh", "rep #xxx", npRepImm],
  ["0000011011dddddd00100000", "rep S", npRepReg],
  ["000000000000000010000100", "reset", (c, w) => {}],
  ["000000000000000000000100", "rti", npRti],
  ["000000000000000000001100", "rts", npRts],
  ["000000000000000010000111", "stop", (c, w) => {}],
  ["0000000101iiiiii1000d100", "sub #xx, D", (c, w) => npAddSub(c, (w >>> 8) & 0x3F, (w >>> 3) & 1, true)],
  ["00000001010000001100d100", "sub #xxxx, D", (c, w) => { const x = npWord2(c); c.len++; npAddSub(c, x, (w >>> 3) & 1, true); }],
  ["00000010CCCC00000JJJd000", "tcc S1, D1", npTcc],
  ["00000011CCCC0ttt0JJJdTTT", "tcc S1,D2 S2,D2", npTcc],
  ["00000010CCCC1ttt00000TTT", "tcc S2, D2", npTcc],
  ["000000000000000000000110", "trap", null],
  ["00000000000000000001CCCC", "trapcc", null],
  ["0000101S11MMMRRR110i0000", "vsl", null, matchMMMRRR],
  ["000000000000000010000110", "wait", (c, w) => {}],
];
const DSP_MATCHES = DSP_TEMPLATES.map(([template]) => { let mask = 0, match = 0; for (let j = 0; j < 24; j++) { const ch = template[j]; if (ch === "0" || ch === "1") { mask |= 1 << (23 - j); if (ch === "1") match |= 1 << (23 - j); } } return [mask >>> 0, match >>> 0]; });
function dspLookup(word) {
  for (let i = 0; i < DSP_TEMPLATES.length; i++) {
    if ((word & DSP_MATCHES[i][0]) !== DSP_MATCHES[i][1]) continue;
    const extra = DSP_TEMPLATES[i][3];
    if (extra !== undefined && !extra(word)) continue;
    return DSP_TEMPLATES[i];
  }
  return null;
}
// A program word translated to a function of the core, the non parallel instruction named by its template and the parallel move by its top bits
function dspCompile(c, word) {
  let f;
  if (word < 0x100000) {
    const entry = dspLookup(word);
    if (entry === null) fault(c.name + " runs instruction " + hex(word) + " at " + hex(c.pc) + " which no template names");
    if (entry[2] === null) fault(c.name + " runs instruction " + hex(word) + " (" + entry[1] + ") at " + hex(c.pc) + " which has no model");
    const body = entry[2];
    f = (core) => body(core, word);
  } else {
    const pm = DSP_PM[(word >>> 20) & 0xF];
    f = (core) => pm(core, word);
  }
  f.word = word;
  return f;
}
// ---- the translator ----
// A run of straight line instructions from one address becomes one function, each instruction's fields folded
// into its code, ended by a flow instruction, an immediate operand, a repeat or the run's word budget, the loop
// end checked after every instruction and a halt after every write that may reach the peripheral window
function dspCompileBlock(c, start) {
  const lines = ["const r = c.r, x = c.x, y = c.y; let a = 0, b = 0, v = 0, w = 0, n = 0;", "for (;;) {", "c.pc = " + start + ";", "const lf = (r[" + DSP_SR + "] & " + (1 << SR_LF) + ") !== 0, la = r[" + DSP_LA + "] + 1;"];
  let pc = start, count = 0, ended = false;
  while (count < DSP_BLOCK_WORDS) {
    const piece = pc < DSP_P_WORDS ? dspTranslate(c, c.p[pc], pc) : null;
    if (piece === null) { lines.push("c.pc = " + pc + "; dspStep(c); return n + 1;"); ended = true; break; }
    const next = (pc + piece.len) & M24;
    lines.push(piece.src, "n++;");
    lines.push("if (lf && la === " + next + ") { if ((r[" + DSP_LC + "] = (r[" + DSP_LC + "] - 1) & " + M16 + ") === 0) { dspLoopPop(c); c.pc = " + next + "; return n; } if (r[" + DSP_SSH + "] === " + start + ") continue; c.pc = r[" + DSP_SSH + "]; return n; }");
    if (piece.halts) lines.push("if (c.halted) { c.pc = " + next + "; return n; }");
    pc = next; count++;
  }
  if (!ended) lines.push("c.pc = " + pc + "; return n;");
  lines.push("}");
  const run = dspFunction(lines.join("\n"));
  run.end = pc;
  return run;
}
const DSP_REG_A = DSP_A, DSP_REG_B = DSP_B;
function regReadSrc(num) { return num === DSP_REG_A || num === DSP_REG_B ? "dspReadAccu24(c, " + num + ")" : "r[" + num + "]"; }
// A register written as pmWriteReg and dspWriteReg write it, the control registers through dspWriteReg
function regWriteSrc(num, value, control) {
  if (num >= DSP_SR) return null;
  if (num === DSP_REG_A) return "r[" + DSP_A0 + "] = 0; r[" + DSP_A1 + "] = " + value + " & 0xFFFFFF; r[" + DSP_A2 + "] = (" + value + " & 0x800000) !== 0 ? 0xFF : 0;";
  if (num === DSP_REG_B) return "r[" + DSP_B0 + "] = 0; r[" + DSP_B1 + "] = " + value + " & 0xFFFFFF; r[" + DSP_B2 + "] = (" + value + " & 0x800000) !== 0 ? 0xFF : 0;";
  const bits = DSP_REG_MASK[num];
  return "r[" + num + "] = " + (bits === 0 ? value + " >>> 0;" : value + " & " + ((1 << bits) - 1) + ";");
}
function memReadSrc(space, addr) { return space === 0 ? "(" + addr + " < " + DSP_MIX_ALIAS + " ? x[" + addr + "] : dspReadMemory(c, 0, " + addr + "))" : "(" + addr + " < " + DSP_Y_WORDS + " ? y[" + addr + "] : dspReadMemory(c, 1, " + addr + "))"; }
function memWriteSrc(space, addr, value) { return space === 0 ? "if (" + addr + " < " + DSP_MIX_ALIAS + ") x[" + addr + "] = " + value + " & 0xFFFFFF; else dspWriteMemory(c, 0, " + addr + ", " + value + ");" : "if (" + addr + " < " + DSP_Y_WORDS + ") y[" + addr + "] = " + value + " & 0xFFFFFF; else dspWriteMemory(c, 1, " + addr + ", " + value + ");"; }
// The effective address of a mode into a variable, null for the immediate and absolute modes the interpreter keeps
function eaSrc(mode, into) {
  const kind = (mode >>> 3) & 7, n = mode & 7, rn = DSP_R0 + n, mn = DSP_M0 + n;
  switch (kind) {
  case 4: return into + " = r[" + rn + "];";
  case 3: return into + " = r[" + rn + "]; if (r[" + mn + "] === " + M16 + ") r[" + rn + "] = (" + into + " + 1) & " + M16 + "; else updateRn(c, " + n + ", 1);";
  case 2: return into + " = r[" + rn + "]; if (r[" + mn + "] === " + M16 + ") r[" + rn + "] = (" + into + " - 1) & " + M16 + "; else updateRn(c, " + n + ", -1);";
  case 6: return null;
  }
  return into + " = calcEA(c, " + mode + ");";
}
function aluSrc(w) { const op = w & 0xFF; return op === 0 ? "" : DSP_ALU_SRC[op]; }
function pmTranslate(c, w) {
  const kind = (w >>> 20) & 0xF, alu = aluSrc(w);
  if (kind === 0) {
    const space = (w >>> 15) & 1, d = (w >>> 16) & 1, ea = eaSrc((w >>> 8) & 0x3F, "a");
    if (ea === null) return null;
    return { src: ea + " v = dspReadAccu24(c, " + (DSP_A + d) + "); w = r[" + (DSP_X0 + (space << 1)) + "]; " + alu + " " + memWriteSrc(space, "a", "v") + " pmWriteAcc(c, " + d + ", w);", len: 1, halts: space === 0 };
  }
  if (kind === 1) {
    const space = (w >>> 14) & 1, ea = eaSrc((w >>> 8) & 0x3F, "a"), read = (w & (1 << 15)) !== 0;
    if (ea === null) return null;
    const sel = space !== 0 ? (w >>> 16) & 3 : (w >>> 18) & 3, reg1 = sel === 0 ? (space !== 0 ? DSP_Y0 : DSP_X0) : sel === 1 ? (space !== 0 ? DSP_Y1 : DSP_X1) : sel === 2 ? DSP_A : DSP_B;
    const reg2 = DSP_A + (space !== 0 ? (w >>> 19) & 1 : (w >>> 17) & 1), into = space !== 0 ? DSP_X0 + ((w >>> 18) & 1) : DSP_Y0 + ((w >>> 16) & 1);
    const store = read ? regWriteSrc(reg1, "v", false) : memWriteSrc(space, "a", "v");
    if (store === null) return null;
    return { src: ea + " v = " + (read ? memReadSrc(space, "a") : regReadSrc(reg1)) + "; w = dspReadAccu24(c, " + reg2 + "); " + alu + " " + store + " r[" + into + "] = w;", len: 1, halts: !read && space === 0 };
  }
  if (kind === 2 || kind === 3) {
    if ((w & 0xFFFF00) === 0x200000) return { src: alu, len: 1, halts: false };
    if ((w & 0xFFE000) === 0x204000) return { src: eaSrc((w >>> 8) & 0x1F, "a") + " " + alu, len: 1, halts: false };
    if ((w & 0xFC0000) === 0x200000) { const src = (w >>> 13) & 0x1F, dst = (w >>> 8) & 0x1F, store = regWriteSrc(dst, "v", false); return store === null ? null : { src: "v = " + regReadSrc(src) + "; " + alu + " " + store, len: 1, halts: false }; }
    const dst = (w >>> 16) & 0x1F;
    let v = (w >>> 8) & 0xFF;
    if (dst === DSP_X0 || dst === DSP_X1 || dst === DSP_Y0 || dst === DSP_Y1 || dst === DSP_A || dst === DSP_B) v <<= 16;
    const store = regWriteSrc(dst, String(v), false);
    return store === null ? null : { src: alu + " " + store, len: 1, halts: false };
  }
  if (kind === 4 && (w & 0xF40000) === 0x400000) {
    const value = (w >>> 8) & 0x3F, ea = (w & (1 << 14)) !== 0 ? eaSrc(value, "a") : "a = " + value + ";", read = (w & (1 << 15)) !== 0, num = ((w >>> 16) & 3) | ((w >>> 17) & 4);
    if (ea === null) return null;
    const pairs = [[DSP_A1, DSP_A0], [DSP_B1, DSP_B0], [DSP_X1, DSP_X0], [DSP_Y1, DSP_Y0]];
    let load, store;
    if (read) load = "v = " + memReadSrc(0, "a") + "; w = " + memReadSrc(1, "a") + ";";
    else if (num < 4) load = "v = r[" + pairs[num][0] + "]; w = r[" + pairs[num][1] + "];";
    else if (num === 4 || num === 5) { const acc = num === 4 ? DSP_A : DSP_B, lo = num === 4 ? DSP_A0 : DSP_B0; load = "v = dspReadAccu24(c, " + acc + "); w = c.limited ? ((v & 0x800000) !== 0 ? 0 : 0xFFFFFF) : r[" + lo + "];"; }
    else load = "v = dspReadAccu24(c, " + (num === 6 ? DSP_A : DSP_B) + "); w = dspReadAccu24(c, " + (num === 6 ? DSP_B : DSP_A) + ");";
    if (!read) store = memWriteSrc(0, "a", "v") + " " + memWriteSrc(1, "a", "w");
    else if (num < 4) store = "r[" + pairs[num][0] + "] = v; r[" + pairs[num][1] + "] = w;";
    else if (num === 4) store = "r[" + DSP_A0 + "] = w; r[" + DSP_A1 + "] = v; r[" + DSP_A2 + "] = (v & 0x800000) !== 0 ? 0xFF : 0;";
    else if (num === 5) store = "r[" + DSP_B0 + "] = w; r[" + DSP_B1 + "] = v; r[" + DSP_B2 + "] = (v & 0x800000) !== 0 ? 0xFF : 0;";
    else if (num === 6) store = "pmWriteAcc(c, 0, v); pmWriteAcc(c, 1, w);";
    else store = "pmWriteAcc(c, 1, v); pmWriteAcc(c, 0, w);";
    return { src: ea + " " + load + " " + alu + " " + store, len: 1, halts: !read };
  }
  if (kind >= 4 && kind <= 7) {
    const value = (w >>> 8) & 0x3F, ea = (w & (1 << 14)) !== 0 ? eaSrc(value, "a") : "a = " + value + ";", space = (w >>> 19) & 1, num = ((w >>> 16) & 7) | ((w >>> 17) & 0x18), read = (w & (1 << 15)) !== 0;
    if (ea === null) return null;
    const store = read ? regWriteSrc(num, "v", false) : memWriteSrc(space, "a", "v");
    if (store === null) return null;
    return { src: ea + " v = " + (read ? memReadSrc(space, "a") : regReadSrc(num)) + "; " + alu + " " + store, len: 1, halts: !read && space === 0 };
  }
  let ea1 = (w >>> 8) & 0x1F, ea2 = ((w >>> 13) & 3) | ((w >>> 17) & 0x18);
  if ((ea1 >>> 3) === 0) ea1 |= 1 << 5;
  if ((ea1 & 4) === 0) ea2 |= 4;
  if ((ea2 >>> 3) === 0) ea2 |= 1 << 5;
  const xea = eaSrc(ea1, "a"), yea = eaSrc(ea2, "b");
  if (xea === null || yea === null) return null;
  const s1 = (w >>> 18) & 3, s2 = (w >>> 16) & 3, reg1 = s1 === 0 ? DSP_X0 : s1 === 1 ? DSP_X1 : s1 === 2 ? DSP_A : DSP_B, reg2 = s2 === 0 ? DSP_Y0 : s2 === 1 ? DSP_Y1 : s2 === 2 ? DSP_A : DSP_B;
  const read1 = (w & (1 << 15)) !== 0, read2 = (w & (1 << 22)) !== 0;
  const store1 = read1 ? (reg1 === DSP_A || reg1 === DSP_B ? "pmWriteAcc(c, " + (reg1 - DSP_A) + ", v);" : "r[" + reg1 + "] = v;") : memWriteSrc(0, "a", "v");
  const store2 = read2 ? (reg2 === DSP_A || reg2 === DSP_B ? "pmWriteAcc(c, " + (reg2 - DSP_A) + ", w);" : "r[" + reg2 + "] = w;") : memWriteSrc(1, "b", "w");
  return { src: xea + " " + yea + " v = " + (read1 ? memReadSrc(0, "a") : regReadSrc(reg1)) + "; w = " + (read2 ? memReadSrc(1, "b") : regReadSrc(reg2)) + "; " + alu + " " + store1 + " " + store2, len: 1, halts: !read1 };
}
// The short and long indexed moves and the immediate arithmetic the interpreter runs in place, the rest left to it
function dspTranslate(c, w, pc) {
  if (w >= 0x100000) return pmTranslate(c, w);
  const entry = dspLookup(w);
  if (entry === null || entry[2] === null) return null;
  const second = pc + 1 < DSP_P_WORDS ? c.p[pc + 1] : -1, d = (w >>> 3) & 1, imm = (w >>> 8) & 0x3F;
  switch (entry[1]) {
  case "nop": return { src: "", len: 1, halts: false };
  case "asl #ii, S2, D": return { src: "npShiftImm(c, " + w + ", true);", len: 1, halts: false };
  case "asr #ii, S2, D": return { src: "npShiftImm(c, " + w + ", false);", len: 1, halts: false };
  case "add #xx, D": return { src: "npAddSub(c, " + imm + ", " + d + ", false);", len: 1, halts: false };
  case "sub #xx, D": return { src: "npAddSub(c, " + imm + ", " + d + ", true);", len: 1, halts: false };
  case "add #xxxx, D": return second < 0 ? null : { src: "npAddSub(c, " + second + ", " + d + ", false);", len: 2, halts: false };
  case "sub #xxxx, D": return second < 0 ? null : { src: "npAddSub(c, " + second + ", " + d + ", true);", len: 2, halts: false };
  case "and #xx, D": return { src: "npLogic(c, " + imm + ", " + d + ", 0);", len: 1, halts: false };
  case "or #xx, D": return { src: "npLogic(c, " + imm + ", " + d + ", 1);", len: 1, halts: false };
  case "eor #xx, D": return { src: "npLogic(c, " + imm + ", " + d + ", 2);", len: 1, halts: false };
  case "and #xxxx, D": return second < 0 ? null : { src: "npLogic(c, " + second + ", " + d + ", 0);", len: 2, halts: false };
  case "or #xxxx, D": return second < 0 ? null : { src: "npLogic(c, " + second + ", " + d + ", 1);", len: 2, halts: false };
  case "eor #xxxx, D": return second < 0 ? null : { src: "npLogic(c, " + second + ", " + d + ", 2);", len: 2, halts: false };
  case "cmp #xx, S2": return { src: "npCmp(c, " + imm + ", " + d + ");", len: 1, halts: false };
  case "cmp #xxxx, S2": return second < 0 ? null : { src: "npCmp(c, " + second + ", " + d + ");", len: 2, halts: false };
  case "movec R1, R2": return (w & (1 << 15)) !== 0 && (w & 0x3F) >= DSP_SR ? null : { src: "npMovecReg(c, " + w + ");", len: 1, halts: false };
  case "move X:(Rn + xxx) <-> R": return moveShortSrc(w, 0);
  case "move Y:(Rn + xxx) <-> R": return moveShortSrc(w, 1);
  case "move X:(Rn + xxxx) <-> R": {
    if (second < 0) return null;
    const write = ((w >>> 6) & 1) !== 0, num = w & 0x3F, rn = DSP_R0 + ((w >>> 8) & 7);
    const store = write ? regWriteSrc(num, "v", true) : memWriteSrc(0, "a", "v");
    if (store === null) return null;
    return { src: "a = (r[" + rn + "] + " + second + ") & 0xFFFFFF; " + (write ? "v = " + memReadSrc(0, "a") + "; " : "v = " + regReadSrc(num) + "; ") + store, len: 2, halts: !write };
  }
  }
  return null;
}
function moveShortSrc(w, space) {
  const xxx = (((w >>> 11) & 0x3F) << 1) + ((w >>> 6) & 1), disp = (xxx << 25) >> 25, write = ((w >>> 4) & 1) !== 0, num = w & 0xF, rn = DSP_R0 + ((w >>> 8) & 7);
  const store = write ? regWriteSrc(num, "v", true) : memWriteSrc(space, "a", "v");
  if (store === null) return null;
  return { src: "a = (r[" + rn + "] + " + disp + ") & 0xFFFFFF; " + (write ? "v = " + memReadSrc(space, "a") + "; " : "v = " + regReadSrc(num) + "; ") + store, len: 1, halts: !write && space === 0 };
}
`

// Writes the DSP56300 core the audio processor's two signal processors run
func (e *emitter) writeDSP() {
	e.js("// ---- the DSP56300 ----")
	e.block(dspCoreScript)
}
