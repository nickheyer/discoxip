package bios

import "fmt"

// The audio processor as the page runs it, the front end taking the kernel's methods into the voice table, the
// scatter gather tables and the stream segment lists in memory, the voice processor mixing every active voice of
// the three lists into the mixbins each frame of 32 samples, the samples read through the scatter gather table or
// the stream segments, decoded from their container, resampled by the pitch, shaped by the envelopes, filtered and
// attenuated into the bins, the mixbins handed to the global processor's mixbuf, the global and encode processors
// bootstrapped from their scratch memories when the kernel releases them and run to their halt every frame with
// their DMA moving words through the scratch and FIFO tables, the notifiers written for voices that end, the trap
// and notification bits raising the interrupt line while the kernel enables them, an inactive voice in a list
// trapping the front end with the current and next registers naming it and its link while the walk goes on
const audioProcessorScript = `
function apuModel(w) {
  const inner = registerModel(w), regs = inner.registers;
  const reg = (off) => regs.read(off, 4);
  const a = shared(windowKey(w) + ":apu", () => apuState(inner));
  a.table = reg(APU.voiceTable);
  for (const i of TABLES.hardware.interrupts) if (i.registers.includes((w.base + APU.status) >>> 0)) a.irq = i.irq;
  const LISTS = APU.lists;
  const inMethods = (off) => off >= APU.methodBase && off < APU.methodBase + APU.methodBytes;
  function voiceAt(v, what) { if (v >= APU.voices) fault("voice " + v + " named by " + what + " is beyond the processor's " + APU.voices + " voices"); if (a.table === 0) fault("the voice table address is unset while " + what + " names voice " + v); return (a.table + v * APU.voiceBytes) >>> 0; }
  function voiceWord(v, off, what) { return physRead((voiceAt(v, what) + off) >>> 0, 4); }
  function voiceSet(v, off, mask, value, what) { const at = (voiceAt(v, what) + off) >>> 0; physWrite(at, 4, ((physRead(at, 4) & ~mask) | (value & mask)) >>> 0); }
  function vget(v, off, mask, shift) { return ((voiceWord(v, off, "the engine") & mask) >>> shift) >>> 0; }
  function vset(v, off, mask, shift, value) { voiceSet(v, off, mask, (value << shift) >>> 0, "the engine"); }
  function updateLine() {
    let status = reg(APU.status); const enable = reg(APU.enable);
    const pending = (enable & APU.globalBit) !== 0 && (status & ~APU.globalBit & enable) !== 0;
    status = pending ? status | APU.globalBit : status & ~APU.globalBit;
    inner.write(APU.status, 4, status >>> 0);
    if (a.irq >= 0) setLine(a.irq, pending);
  }
  function raise(bits) { inner.write(APU.status, 4, (reg(APU.status) | bits) >>> 0); updateLine(); }
  function idleVoice(v) {
    if ((reg(APU.trapForce) & APU.trapForceIdle) === 0) fault("voice " + v + " went idle with the front end's idle voice trap off");
    inner.write(APU.frontControl, 4, ((reg(APU.frontControl) & ~(APU.methodModeMask | APU.trapReasonMask)) | APU.methodModeTrapped | APU.trapReasonRequested) >>> 0);
    inner.write(APU.trappedMethod, 4, APU.idleVoiceMethod); inner.write(APU.trappedParameter, 4, v);
    raise(APU.trapBit);
  }
  // A notifier's status byte written for a voice and the notification bits raised
  function notify(v, notifier, status) {
    const at = (reg(APU.notifyAddress) + APU.notifierBytes * (APU.notifierBase + v * APU.notifiersPerVoice + notifier)) >>> 0;
    physWrite(at + APU.notifierStatusAt, 1, status);
    physWrite(at + APU.notifierStatusAt - 1, 1, 1);
    raise(APU.notifyIntBit | APU.voiceIntBit);
  }
  function voiceState(v) { let s = a.voice[v]; if (s === undefined) { s = { phase: 0, prevL: 0, prevR: 0, curL: 0, curR: 0, primed: false, ssl: { base: [0, 0], count: [0, 0], index: 0, seg: 0 }, adpcmBlock: -1, adpcm: new Int16Array((APU.adpcmSamples + 1) * 2), svf: [svfNew(), svfNew()], hrtf: hrtfNew() }; a.voice[v] = s; } return s; }
  function resetVoiceFilters(v) { const s = voiceState(v); s.svf[0] = svfNew(); s.svf[1] = svfNew(); hrtfClear(s.hrtf); s.primed = false; s.phase = 0; s.adpcmBlock = -1; }
  function voiceOff(v) {
    vset(v, APU.voiceState, APU.voiceActiveBit, 0, 0);
    const stream = (voiceWord(v, APU.voiceFormat, "voice off") & APU.streamBit) !== 0;
    let notifier = 0;
    if (stream) notifier += voiceState(v).ssl.index;
    notify(v, notifier, APU.notifierDone);
  }
  // ---- the front end's methods ----
  function voiceOn(argument) {
    const v = argument & APU.handleMask, list = (reg(APU.antecedent) & APU.antecedentList) >>> APU.antecedentListShift;
    if (list !== 0) { const top = LISTS[list - 1][0]; voiceSet(v, APU.voiceLink, APU.voiceLinkMask, reg(top), "voice on"); inner.write(top, 4, v); }
    else {
      const before = reg(APU.antecedent) & APU.antecedentHandle;
      if (before === APU.noVoice) fault("voice " + v + " turned on after no antecedent voice");
      voiceSet(v, APU.voiceLink, APU.voiceLinkMask, voiceWord(before, APU.voiceLink, "the antecedent voice"), "voice on");
      voiceSet(before, APU.voiceLink, APU.voiceLinkMask, v, "voice on");
    }
    vset(v, APU.voiceOffset, APU.offsetMask, 0, 0);
    const s = voiceState(v); s.ssl.seg = 0; s.ssl.index = 0;
    const eaStart = (argument & APU.envelopeAmplitude) >>> APU.amplitudePhaseBit, efStart = (argument & APU.envelopeFilter) >>> APU.filterPhaseBit;
    vset(v, APU.voiceState, APU.amplitudePhaseMask, APU.amplitudePhaseBit, eaStart);
    if (eaStart === APU.phaseDelay) vset(v, APU.voiceCounts, APU.amplitudeCountMask, 0, vget(v, APU.envelope0, APU.rateMask << APU.timeShift, APU.timeShift) * APU.releaseFramesPer);
    else if (eaStart === APU.phaseAttack) vset(v, APU.voiceCounts, APU.amplitudeCountMask, 0, 0);
    else if (eaStart === APU.phaseHold) vset(v, APU.voiceCounts, APU.amplitudeCountMask, 0, vget(v, APU.envelopeA, APU.rateMask << APU.timeShift, APU.timeShift) * APU.releaseFramesPer);
    vset(v, APU.voiceState, APU.filterPhaseMask, APU.filterPhaseBit, efStart);
    if (efStart === APU.phaseDelay) vset(v, APU.voiceCounts, APU.filterCountMask, APU.filterCountShift, vget(v, APU.envelope1, APU.rateMask << APU.timeShift, APU.timeShift) * APU.releaseFramesPer);
    else if (efStart === APU.phaseAttack) vset(v, APU.voiceCounts, APU.filterCountMask, APU.filterCountShift, 0);
    else if (efStart === APU.phaseHold) vset(v, APU.voiceCounts, APU.filterCountMask, APU.filterCountShift, vget(v, APU.envelopeF, APU.rateMask << APU.timeShift, APU.timeShift) * APU.releaseFramesPer);
    resetVoiceFilters(v);
    vset(v, APU.voiceState, APU.voiceActiveBit, 0, APU.voiceActiveBit);
  }
  function voiceRelease(argument) {
    const v = argument & APU.handleMask;
    vset(v, APU.voiceCounts, APU.amplitudeCountMask, 0, vget(v, APU.voiceLFOEnvelope, APU.rateMask, 0) * APU.releaseFramesPer);
    vset(v, APU.voiceState, APU.amplitudePhaseMask, APU.amplitudePhaseBit, APU.phaseRelease);
    vset(v, APU.voiceCounts, APU.filterCountMask, APU.filterCountShift, vget(v, APU.voiceMisc, APU.rateMask, 0) * APU.releaseFramesPer);
    vset(v, APU.voiceState, APU.filterPhaseMask, APU.filterPhaseBit, APU.phaseRelease);
  }
  function hrtfEntry() { if (a.hrtf.current >= APU.hrtfEntries) fault("HRTF entry " + a.hrtf.current + " lies beyond the table"); return a.hrtf.entries[a.hrtf.current]; }
  function method(at, value) {
    inner.write(APU.trappedMethod, 4, at); inner.write(APU.trappedParameter, 4, value);
    const current = reg(APU.currentVoice);
    switch (at) {
    case APU.pioVoiceLock: return;
    case APU.pioAntecedent: inner.write(APU.antecedent, 4, value); return;
    case APU.pioVoiceOn: voiceOn(value); return;
    case APU.pioVoiceRelease: voiceRelease(value); return;
    case APU.pioVoiceOff: voiceOff(value & APU.handleMask); return;
    case APU.pioVoicePause: vset(value & APU.handleMask, APU.voiceState, APU.voicePausedBit, 0, (value & APU.pauseAction) !== 0 ? APU.voicePausedBit : 0); return;
    case APU.pioHRTFEntry: a.hrtf.current = value & APU.handleMask; return;
    case APU.pioCurrentVoice: inner.write(APU.currentVoice, 4, value); return;
    case APU.pioVoiceBins: case APU.pioVoiceFormat: case APU.pioVoiceEnvelope0: case APU.pioVoiceEnvelopeA: case APU.pioVoiceEnvelope1: case APU.pioVoiceEnvelopeF: case APU.pioVoiceMisc: voiceSet(current, at - APU.pioVoiceWordFirst, 0xFFFFFFFF, value, "method " + hex(at)); return;
    case APU.pioVoiceHRTF: {
      const handle = value & APU.handleMask;
      voiceSet(current, APU.voiceHRTFTarget, APU.handleMask, handle, "the HRTF target");
      if (current < APU.voices3D && handle !== APU.noVoice) { if (handle >= APU.hrtfEntries) fault("HRTF entry " + handle + " named by voice " + current + " lies beyond the table"); hrtfSetTarget(voiceState(current).hrtf, a.hrtf.entries[handle]); }
      return;
    }
    case APU.pioVoiceVolumeA: case APU.pioVoiceVolumeB: case APU.pioVoiceVolumeC: case APU.pioVoiceLFOEnvelope: case APU.pioVoiceFilterA: case APU.pioVoiceFilterB: voiceSet(current, at - APU.pioVoiceWordFirst, 0xFFFFFFFF, value, "method " + hex(at)); return;
    case APU.pioVoicePitch: voiceSet(current, APU.voiceLink, ~APU.voiceLinkMask >>> 0, value & ~APU.voiceLinkMask, "the pitch"); return;
    case APU.pioBufferBase: vset(current, APU.voiceBufferBase, APU.offsetMask, 0, value); return;
    case APU.pioBufferLoop: vset(current, APU.voiceBufferLoop, APU.offsetMask, 0, value); return;
    case APU.pioBufferCurrent: vset(current, APU.voiceOffset, APU.offsetMask, 0, value); return;
    case APU.pioBufferEnd: vset(current, APU.voiceNext, APU.offsetMask, 0, value); return;
    case APU.pioInputSGE: a.inbufSGE = value; return;
    case APU.pioInputSGEOffset: { const table = reg(APU.vpSGEAddress); if (table === 0) fault("a sample page entry is set while the scatter gather table address is unset"); physWrite((table + a.inbufSGE * APU.sgeBytes) >>> 0, 4, value & APU.sgeOffsetMask); return; }
    case APU.pioOutputSGE: a.outbufSGE = value; return;
    case APU.pioOutputSGEOffset: { const table = reg(APU.gpFIFOAddress); if (table === 0) fault("an output buffer page entry is set while the FIFO table address is unset"); physWrite((table + a.outbufSGE * APU.sgeBytes) >>> 0, 4, value & APU.sgeOffsetMask); return; }
    case APU.pioVoiceSSLA: case APU.pioVoiceSSLB: { const s = voiceState(current).ssl, i = at === APU.pioVoiceSSLA ? 0 : 1; s.base[i] = (value & APU.sslBaseMask) >>> APU.sslBaseShift; s.count[i] = value & APU.sslCountMask; return; }
    case APU.pioCurrentSSL: if ((value & 0x3F) !== 0) fault("the stream segment list base " + hex(value) + " is not aligned"); a.sslBasePage = value; return;
    case APU.pioHRTFSubmixes: for (let i = 0; i < 4; i++) a.hrtfSubmix[i] = (value >>> (8 * i)) & APU.binMask; return;
    case APU.pioHRTFHeadroom: a.hrtfHeadroom = value & APU.headroomMask; return;
    case APU.pioHRIRLast: { const e = hrtfEntry(); e.hrir[0][APU.hrtfTaps - 1] = int8(value & 0xFF) / 128; e.hrir[1][APU.hrtfTaps - 1] = int8((value >>> 8) & 0xFF) / 128; e.itd = ((value >>> 16) << 16 >> 16) / 512; return; }
    }
    if (at >= APU.pioHRIR && at < APU.pioHRIRLast) { const e = hrtfEntry(), slot = (at - APU.pioHRIR) >>> 2; e.hrir[0][slot * 2] = int8(value & 0xFF) / 128; e.hrir[1][slot * 2] = int8((value >>> 8) & 0xFF) / 128; e.hrir[0][slot * 2 + 1] = int8((value >>> 16) & 0xFF) / 128; e.hrir[1][slot * 2 + 1] = int8(value >>> 24) / 128; return; }
    if (at >= APU.pioSubmixHeadroom && at < APU.pioSubmixHeadroom + 4 * APU.mixbins) { a.submixHeadroom[(at - APU.pioSubmixHeadroom) >>> 2] = value & APU.headroomMask; return; }
    if (at >= APU.pioSSLSegmentOffset && at < APU.pioSSLSegmentOffset + 8 * APU.sslSegments) { const table = reg(APU.vpSSLAddress); if (table === 0) fault("a stream segment is set while the segment list address is unset"); physWrite((table + a.sslBasePage * APU.sgeBytes + (at - APU.pioSSLSegmentOffset)) >>> 0, 4, value); return; }
    if (at === APU.pioVoicePosition || at === APU.pioContextDMANotify || at === APU.pioSSLContextDMA) fault("front end method " + hex(at) + " with " + hex(value));
    inner.write(APU.methodBase + at, 4, value);
  }
  // ---- envelopes ----
  // One frame of an envelope, the level it leaves in its word and the gain it applies, by the phase in the state
  function stepEnvelope(v, reg0, regA, rrReg, rrMask, lvlReg, lvlMask, lvlShift, countMask, countShift, curMask, curShift) {
    let cur = vget(v, APU.voiceState, curMask, curShift);
    const count = () => vget(v, APU.voiceCounts, countMask, countShift), setCount = (n) => vset(v, APU.voiceCounts, countMask, countShift, n), setLevel = (l) => vset(v, lvlReg, lvlMask, lvlShift, l), setPhase = (p) => vset(v, APU.voiceState, curMask, curShift, p);
    switch (cur) {
    case APU.phaseOff: setCount(0); setLevel(APU.levelRange); return 1;
    case APU.phaseDelay: { let n = count(); setLevel(0); if (n === 0) setPhase(cur + 1); else n--; setCount(n); return 0; }
    case APU.phaseAttack: {
      let n = count(); const rate = vget(v, reg0, APU.rateMask, 0);
      let level;
      if (rate === 0) level = APU.levelRange; else if (n <= rate * APU.releaseFramesPer) level = Math.floor(n * APU.levelRange / (rate * APU.releaseFramesPer)); else level = APU.levelRange;
      setLevel(level);
      if (n === rate * APU.releaseFramesPer) { setPhase(cur + 1); n = vget(v, regA, APU.rateMask << APU.timeShift, APU.timeShift) * APU.releaseFramesPer; } else n++;
      setCount(n);
      return level / APU.levelRange;
    }
    case APU.phaseHold: { let n = count(); setLevel(APU.levelRange); if (n === 0) { setPhase(cur + 1); n = vget(v, regA, APU.rateMask, 0) * APU.releaseFramesPer; } else n--; setCount(n); return 1; }
    case APU.phaseDecay: {
      const n = count(), rate = vget(v, regA, APU.rateMask, 0), sustain = vget(v, regA, APU.levelMask << APU.levelShift, APU.levelShift);
      const level = rate === 0 ? 0 : APU.levelRange * Math.pow(APU.decayBase, (rate * APU.releaseFramesPer - n) * APU.decayScale / rate);
      if (level <= sustain + 0.2 || level > APU.levelRange) setPhase(cur + 1); else { setCount(n - 1); setLevel(level); }
      return level / APU.levelRange;
    }
    case APU.phaseSustain: { const sustain = vget(v, regA, APU.levelMask << APU.levelShift, APU.levelShift); setCount(0); setLevel(sustain); return sustain / APU.levelRange; }
    case APU.phaseRelease: {
      let n = count(); const rate = vget(v, rrReg, rrMask, 0);
      if (rate === 0) n = 0;
      let level = 0;
      if (n === 0) setPhase(cur + 1);
      else { const pos = Math.min(Math.max(1 - n / (rate * APU.releaseFramesPer), 0), 1); level = Math.exp(-APU.releaseDecay * pos) * vget(v, lvlReg, lvlMask, lvlShift); setCount(n - 1); }
      return level / APU.levelRange;
    }
    case APU.phaseForcedRelease: if (countMask === APU.amplitudeCountMask) voiceOff(v); return 0;
    }
    fault("voice " + v + " envelope phase " + cur);
  }
  // ---- samples ----
  function sgePointer(table, addr) { const entry = Math.floor(addr / APU.pageBytes), page = physRead((table + entry * APU.sgeBytes) >>> 0, 4); return (page + (addr % APU.pageBytes)) >>> 0; }
  const sample = new Float32Array(2), blockBytes = new Uint8Array(APU.adpcmBlockBytes * 2);
  // The next sample of a voice decoded from its buffer or stream, false once the voice stops or its stream waits
  function voiceNextSample(v, s, out) {
    const fmt = voiceWord(v, APU.voiceFormat, "the voice format");
    const stereo = (fmt & APU.stereoBit) !== 0, channels = stereo ? 2 : 1, stream = (fmt & APU.streamBit) !== 0, loop = (fmt & APU.loopBit) !== 0, persist = (fmt & APU.persistBit) !== 0, samplesPerBlock = ((fmt >>> APU.samplesShift) & APU.samplesMask) + 1;
    let sampleSize = (fmt >>> APU.sampleSizeShift) & 3, container = (fmt >>> APU.containerShift) & 3;
    let ebo = vget(v, APU.voiceNext, APU.offsetMask, 0), cbo = vget(v, APU.voiceOffset, APU.offsetMask, 0);
    const lbo = vget(v, APU.voiceBufferLoop, APU.offsetMask, 0), ba = vget(v, APU.voiceBufferBase, APU.offsetMask, 0);
    vset(v, APU.voiceState, APU.newVoiceBit, 0, 0);
    let segmentOffset = 0, count = 0;
    if (stream) {
      if (!persist && vget(v, APU.voiceState, APU.amplitudePhaseMask, APU.amplitudePhaseBit) < APU.phaseRelease) { voiceOff(v); return false; }
      const ssl = s.ssl, page = ssl.base[ssl.index] + ssl.seg;
      count = ssl.count[ssl.index];
      if (count === 0) { vset(v, APU.voiceOffset, APU.offsetMask, 0, 0); ssl.seg = 0; if (!persist) { ssl.index = 0; voiceOff(v); } else notify(v, ssl.index, APU.notifierDone); return false; }
      const table = reg(APU.vpSSLAddress), at = (table + page * APU.sgeBytes) >>> 0;
      segmentOffset = physRead(at, 4); const length = physRead(at + 4, 4);
      if (segmentOffset === 0 || length === 0) fault("voice " + v + " streams from an empty segment at " + hex(at));
      const segLen = length & 0xFFFF, cs = (length >>> 16) & 3, spb = (length >>> 18) & 0x1F, st = (length >>> 23) & 1;
      if (cs !== container || spb + 1 !== samplesPerBlock || (st !== 0) !== stereo) fault("voice " + v + " streams a segment whose format " + hex(length) + " disagrees with its own " + hex(fmt));
      if (cs === APU.containerADPCM) sampleSize = 2;
      ebo = segLen - 1;
    }
    const adpcm = container === APU.containerADPCM, containerBytes = [1, 2, 0, 4][container], blockSize = (adpcm ? APU.adpcmBlockBytes : containerBytes) * samplesPerBlock;
    let produced = false;
    if (cbo <= ebo) {
      if (adpcm) {
        const block = Math.floor(cbo / APU.adpcmSamples), position = cbo % APU.adpcmSamples;
        if (s.adpcmBlock !== block) {
          const linear = block * blockSize;
          for (let i = 0; i < blockSize; i++) blockBytes[i] = physRead(stream ? (segmentOffset + linear + i) >>> 0 : sgePointer(reg(APU.vpSGEAddress), ba + linear + i), 1);
          adpcmDecode(s.adpcm, blockBytes, blockSize, channels);
          s.adpcmBlock = block;
        }
        out[0] = s.adpcm[position * channels] / 32768;
        out[1] = stereo ? s.adpcm[position * channels + 1] / 32768 : out[0];
      } else {
        let addr = stream ? (segmentOffset + cbo * blockSize) >>> 0 : sgePointer(reg(APU.vpSGEAddress), ba + cbo * blockSize);
        for (let ch = 0; ch < channels; ch++) {
          let value;
          switch (sampleSize) {
          case 0: value = (physRead(addr, 1) - 0x80) / 128; break;
          case 1: value = ((physRead(addr, 2) << 16) >> 16) / 32768; break;
          case 2: value = ((physRead(addr, 4) << 8) | 0) / 2147483648; break;
          case 3: value = (physRead(addr, 4) | 0) / 2147483648; break;
          }
          out[ch] = value;
          addr = (addr + containerBytes) >>> 0;
        }
        if (!stereo) out[1] = out[0];
      }
      cbo++;
      produced = true;
    }
    if (cbo > ebo) {
      if (stream) {
        const ssl = s.ssl;
        ssl.seg++; cbo = 0;
        if (ssl.seg >= count) { const done = ssl.index; ssl.index = (ssl.index + 1) % 2; ssl.seg = 0; notify(v, done, APU.notifierDone); }
      } else if (loop) cbo = lbo;
      else { cbo = ebo; voiceOff(v); }
    }
    vset(v, APU.voiceOffset, APU.offsetMask, 0, cbo);
    return produced;
  }
  function active(v) { return (voiceWord(v, APU.voiceState, "the voice state") & APU.voiceActiveBit) !== 0; }
  // A frame of a voice resampled by linear interpolation at the rate the pitch sets, silent once it stops
  function voiceResample(v, s, rate, out) {
    const step = 1 / rate;
    if (!s.primed) {
      if (!active(v) || !voiceNextSample(v, s, sample)) return;
      s.prevL = sample[0]; s.prevR = sample[1];
      if (!active(v) || !voiceNextSample(v, s, sample)) return;
      s.curL = sample[0]; s.curR = sample[1]; s.phase = 0; s.primed = true;
    }
    for (let i = 0; i < APU.frameSamples; i++) {
      while (s.phase >= 1) {
        s.prevL = s.curL; s.prevR = s.curR;
        if (!active(v) || !voiceNextSample(v, s, sample)) return;
        s.curL = sample[0]; s.curR = sample[1];
        s.phase -= 1;
      }
      out[i * 2] = s.prevL + (s.curL - s.prevL) * s.phase;
      out[i * 2 + 1] = s.prevR + (s.curR - s.prevR) * s.phase;
      s.phase += step;
    }
  }
  function attenuate(vol) { vol &= APU.rateMask; return vol === APU.rateMask ? 0 : Math.pow(10, vol / (APU.volumeUnit * -APU.decibelsPerUnit)); }
  function multipassSamples(v, out) {
    const fmt = voiceWord(v, APU.voiceFormat, "the voice format"), bin = (fmt >>> APU.samplesShift) & APU.binMask;
    for (let i = 0; i < APU.frameSamples; i++) { out[i * 2] = a.mixbins[bin * APU.frameSamples + i]; out[i * 2 + 1] = out[i * 2]; }
    if ((fmt & APU.clearMixBit) !== 0) a.mixbins.fill(0, bin * APU.frameSamples, (bin + 1) * APU.frameSamples);
  }
  const bins = new Int32Array(8), vols = new Int32Array(8);
  function voiceProcess(v) {
    const s = voiceState(v);
    const fmt = voiceWord(v, APU.voiceFormat, "the voice format"), stereo = (fmt & APU.stereoBit) !== 0, channels = stereo ? 2 : 1;
    if ((voiceWord(v, APU.voiceState, "the voice state") & APU.voicePausedBit) !== 0) return;
    const ef = stepEnvelope(v, APU.envelope1, APU.envelopeF, APU.voiceMisc, APU.rateMask, APU.voiceNext, APU.levelMask << APU.levelShift, APU.levelShift, APU.filterCountMask, APU.filterCountShift, APU.filterPhaseMask, APU.filterPhaseBit);
    const pitch = (vget(v, APU.voiceLink, ~APU.voiceLinkMask >>> 0, APU.pitchShift) << 16) >> 16, scale = int8(vget(v, APU.envelope0, APU.levelMask << APU.pitchScaleShift, APU.pitchScaleShift));
    const rate = 1 / Math.pow(2, (pitch + scale * APU.pitchScaleUnit * ef) / APU.pitchUnit);
    const ea = stepEnvelope(v, APU.envelope0, APU.envelopeA, APU.voiceLFOEnvelope, APU.rateMask, APU.voiceOffset, APU.levelMask << APU.levelShift, APU.levelShift, APU.amplitudeCountMask, 0, APU.amplitudePhaseMask, APU.amplitudePhaseBit);
    const out = a.samples;
    out.fill(0);
    if ((fmt & APU.multipassBit) !== 0) multipassSamples(v, out); else voiceResample(v, s, rate, out);
    if (!active(v)) return;
    const vbin = voiceWord(v, APU.voiceBins, "the voice bins");
    bins[0] = vbin & APU.binMask; bins[1] = (vbin >>> 5) & APU.binMask; bins[2] = (vbin >>> 10) & APU.binMask; bins[3] = (vbin >>> 16) & APU.binMask; bins[4] = (vbin >>> 21) & APU.binMask; bins[5] = (vbin >>> 26) & APU.binMask;
    bins[6] = fmt & APU.binMask; bins[7] = (fmt >>> 5) & APU.binMask;
    if (v < APU.voices3D) for (let i = 0; i < 4; i++) bins[i] = a.hrtfSubmix[i];
    const volA = voiceWord(v, APU.voiceVolumeA, "the volumes"), volB = voiceWord(v, APU.voiceVolumeB, "the volumes"), volC = voiceWord(v, APU.voiceVolumeC, "the volumes");
    vols[0] = (volA >>> 4) & 0xFFF; vols[1] = (volA >>> 20) & 0xFFF; vols[2] = (volB >>> 4) & 0xFFF; vols[3] = (volB >>> 20) & 0xFFF; vols[4] = (volC >>> 4) & 0xFFF; vols[5] = (volC >>> 20) & 0xFFF;
    vols[6] = (((volC & 0xF) << 8) | ((volB & 0xF) << 4) | (volA & 0xF)) & 0xFFF; vols[7] = ((((volC >>> 16) & 0xF) << 8) | (((volB >>> 16) & 0xF) << 4) | ((volA >>> 16) & 0xF)) & 0xFFF;
    const fmode = (voiceWord(v, APU.voiceMisc, "the filter mode") >>> APU.filterModeShift) & 3;
    const lowPass = v < APU.voices3D ? fmode === 1 : stereo ? fmode === 1 : (fmode & 1) !== 0;
    if (lowPass) for (let ch = 0; ch < 2; ch++) {
      const word = voiceWord(v, (ch % channels) === 0 ? APU.voiceFilterA : APU.voiceFilterB, "the filter cutoff"), fc = (word << 16) >> 16, q = word >>> 16;
      const f = Math.min(Math.max(Math.pow(2, fc / APU.filterUnit), APU.filterMinCutoff), 1), qf = Math.min(Math.max(q / APU.filterQUnit, APU.filterMinQ), 1);
      const filter = s.svf[ch];
      svfSetup(filter, f, qf);
      for (let i = 0; i < APU.frameSamples; i++) out[i * 2 + ch] = Math.min(Math.max(svfRun(filter, out[i * 2 + ch]), -1), 1);
    }
    if (v < APU.voices3D && vget(v, APU.voiceHRTFTarget, APU.handleMask, 0) !== APU.noVoice) hrtfProcess(s.hrtf, out);
    for (let b = 0; b < 8; b++) {
      const headroom = v < APU.voices3D && b < 4 ? 1 << a.hrtfHeadroom : 1 << a.submixHeadroom[bins[b]];
      const g = ea * attenuate(vols[b]) / headroom, base = bins[b] * APU.frameSamples, ch = b % channels;
      for (let i = 0; i < APU.frameSamples; i++) a.mixbins[base + i] += g * out[i * 2 + ch];
    }
  }
  function floatTo24(value) { const scaled = value * 8388608; if (scaled >= 8388607) return 0x7FFFFF; if (scaled <= -8388608) return 0x800000; return Math.round(scaled) & 0xFFFFFF; }
  // ---- the processors ----
  function processorRuns(reset) { return (reset & APU.procResetRun) !== 0 && (reset & APU.procResetDSP) !== 0; }
  function runProcessor(c) { dspStartFrame(c); c.halted = false; let steps = 0; while (!c.halted) { steps += dspRun(c, 4096); if (steps > APU.frameSteps) fault(c.name + " runs " + steps + " steps in one frame without halting"); } }
  function frame() {
    const fectl = reg(APU.frontControl) & APU.methodModeMask;
    if ((reg(APU.soundEngine) & APU.counterModeMask) === 0 || fectl === APU.methodModeTrapped || fectl === APU.methodModeHalted) return;
    a.mixbins.fill(0);
    for (const [top, current, next] of LISTS) {
      let v = reg(top), steps = 0; inner.write(current, 4, v);
      while (v !== APU.noVoice) {
        if (++steps > APU.voices) fault("the voice list at " + hex(top) + " runs past " + steps + " voices");
        const link = voiceWord(v, APU.voiceLink, "the voice list at " + hex(top)) & APU.voiceLinkMask; inner.write(next, 4, link);
        if (!active(v)) idleVoice(v); else voiceProcess(v);
        v = link; inner.write(current, 4, v);
      }
    }
    for (let bin = 0; bin < APU.mixbins; bin++) for (let i = 0; i < APU.frameSamples; i++) a.gp.mix[bin * APU.frameSamples + i] = floatTo24(a.mixbins[bin * APU.frameSamples + i]);
    if (processorRuns(a.gpReset)) runProcessor(a.gp);
    if (processorRuns(a.epReset) && a.epFrameDiv % APU.epFrameDivider === 0) runProcessor(a.ep);
    a.epFrameDiv++;
  }
  function frameAdvance(now) {
    const period = APU.frameSamples * 1000 / APU.sampleHz;
    if (a.nextAt < 0) a.nextAt = now + period;
    let n = 0;
    while (now >= a.nextAt && n < APU.framesPerAdvance) { a.nextAt += period; frame(); n++; }
    if (now >= a.nextAt) a.nextAt = now + period;
  }
  if (a.advance === null) { a.advance = frameAdvance; a.slack = APU.frameSamples * 1000 / APU.sampleHz * (APU.framesPerAdvance / 2); TIMED.push(a); }
  function processorRead(c, pregs, gp, off, size) {
    if (size !== 4 || (off & 3) !== 0) fault(c.name + " window read of " + size + " bytes at " + hex(off));
    const xWords = gp ? APU.gpXWords : APU.epXWords, yWords = gp ? APU.gpYWords : APU.epYWords;
    if (off < APU.procXMemory + 4 * xWords) return c.x[(off - APU.procXMemory) >>> 2];
    if (gp && off >= APU.procMixbufMemory && off < APU.procMixbufMemory + 4 * APU.procMixbufWords) return c.mix[(off - APU.procMixbufMemory) >>> 2];
    if (off >= APU.procYMemory && off < APU.procYMemory + 4 * yWords) return c.y[(off - APU.procYMemory) >>> 2];
    if (off >= APU.procPMemory && off < APU.procPMemory + 4 * APU.procPWords) return c.p[(off - APU.procPMemory) >>> 2];
    if (off === APU.procReset) return gp ? a.gpReset : a.epReset;
    return pregs.read(off, 4);
  }
  function processorWrite(c, pregs, gp, off, size, v) {
    if (size !== 4 || (off & 3) !== 0) fault(c.name + " window write of " + size + " bytes at " + hex(off));
    const xWords = gp ? APU.gpXWords : APU.epXWords, yWords = gp ? APU.gpYWords : APU.epYWords;
    if (off < APU.procXMemory + 4 * xWords) { c.x[(off - APU.procXMemory) >>> 2] = v & 0xFFFFFF; return; }
    if (gp && off >= APU.procMixbufMemory && off < APU.procMixbufMemory + 4 * APU.procMixbufWords) { c.mix[(off - APU.procMixbufMemory) >>> 2] = v & 0xFFFFFF; return; }
    if (off >= APU.procYMemory && off < APU.procYMemory + 4 * yWords) { c.y[(off - APU.procYMemory) >>> 2] = v & 0xFFFFFF; return; }
    if (off >= APU.procPMemory && off < APU.procPMemory + 4 * APU.procPWords) { dspWriteMemory(c, DSP_SPACE_P, (off - APU.procPMemory) >>> 2, v); return; }
    if (off === APU.procReset) {
      const old = gp ? a.gpReset : a.epReset;
      if (!processorRuns(v)) dspReset(c); else if (!processorRuns(old)) dspBootstrap(c);
      if (gp) a.gpReset = v; else { a.epReset = v; a.epFrameDiv = 0; }
      return;
    }
    pregs.write(off, 4, v);
  }
  return {
    read(off, size) {
      if (off >= APU.gpBase && off < APU.gpBase + APU.processorBytes) return processorRead(a.gp, a.gpRegs, true, off - APU.gpBase, size);
      if (off >= APU.epBase && off < APU.epBase + APU.processorBytes) return processorRead(a.ep, a.epRegs, false, off - APU.epBase, size);
      return inner.read(off, size);
    },
    write(off, size, v) {
      if (off >= APU.gpBase && off < APU.gpBase + APU.processorBytes) { processorWrite(a.gp, a.gpRegs, true, off - APU.gpBase, size, v >>> 0); return; }
      if (off >= APU.epBase && off < APU.epBase + APU.processorBytes) { processorWrite(a.ep, a.epRegs, false, off - APU.epBase, size, v >>> 0); return; }
      if (inMethods(off)) { if (size !== 4) fault("write of " + size + " bytes to method " + hex(off - APU.methodBase) + " of the voice processor"); method(off - APU.methodBase, v >>> 0); return; }
      const at = off & ~3;
      if (at === APU.status) { const shift = 8 * (off & 3), mask = size === 4 ? 0xFFFFFFFF : ((size === 1 ? 0xFF : 0xFFFF) << shift) >>> 0; inner.write(at, 4, (reg(at) & ~((v << shift) & mask)) >>> 0); updateLine(); return; }
      if (at === APU.feMemoryData) { physWrite(reg(APU.feMemoryAddress), 4, v >>> 0); inner.write(off, size, v); return; }
      inner.write(off, size, v);
      if (at === APU.voiceTable) a.table = reg(APU.voiceTable);
      if (at === APU.enable || at === APU.frontControl || at === APU.trapForce) updateLine();
    },
  };
}
// The processor's shared state, the voices' resamplers and filters, the HRTF table, the two signal processors with
// their scratch and FIFO transfers through the tables the registers name
function apuState(inner) {
  const regs = inner.registers, reg = (off) => regs.read(off, 4);
  const a = { nextAt: -1, irq: -1, advance: null, table: 0, voice: new Array(APU.voices), hrtf: { current: 0, entries: [] }, hrtfSubmix: [0, 0, 0, 0], hrtfHeadroom: 0, submixHeadroom: new Uint8Array(APU.mixbins), inbufSGE: 0, outbufSGE: 0, sslBasePage: 0, mixbins: new Float32Array(APU.mixbins * APU.frameSamples), samples: new Float32Array(APU.frameSamples * 2), epFrameDiv: 0, gpReset: 0, epReset: 0, gpRegs: new Registers(), epRegs: new Registers(), gp: null, ep: null };
  for (let i = 0; i < APU.hrtfEntries; i++) a.hrtf.entries.push({ hrir: [new Float32Array(APU.hrtfTaps), new Float32Array(APU.hrtfTaps)], itd: 0 });
  function scatterGather(sgeBase, maxSGE, bytes, addr, len, toHost, what) {
    let moved = 0;
    while (moved < len) {
      const entry = Math.floor(addr / APU.pageBytes), inPage = addr % APU.pageBytes, chunk = Math.min(len - moved, APU.pageBytes - inPage);
      if (entry > maxSGE) fault(what + " reaches page entry " + entry + " beyond its table of " + (maxSGE + 1));
      const page = physRead((sgeBase + entry * APU.sgeBytes) >>> 0, 4), paddr = (page + inPage) >>> 0;
      if (paddr + chunk > RAM_BYTES) fault(what + " reaches " + hex(paddr) + " past memory through page entry " + entry);
      if (toHost) ramWriteBytes(paddr, bytes.subarray(moved, moved + chunk)); else { surfaceFlushRange(paddr, chunk); bytes.set(ram.subarray(paddr, paddr + chunk), moved); }
      addr += chunk; moved += chunk;
    }
  }
  function fifoTransfer(gp, bytes, index, len, toHost) {
    const outputs = gp ? APU.gpOutputFIFOBase : APU.epOutputFIFOBase, inputs = gp ? APU.gpInputFIFOBase : APU.epInputFIFOBase, name = gp ? "the global processor" : "the encode processor";
    if (index >= (toHost ? APU.outputFIFOs : APU.inputFIFOs)) fault(name + " moves " + (toHost ? "to output" : "from input") + " FIFO " + index);
    const block = (toHost ? outputs : inputs) + APU.fifoStride * index, base = reg(block) & APU.fifoValueMask, end = reg(block + 4) & APU.fifoValueMask, curReg = block + 8;
    let cur = reg(curReg) & APU.fifoValueMask;
    if (end <= base) fault(name + "'s FIFO " + index + " ends at " + hex(end) + " before its base " + hex(base));
    if (cur >= end) { if (gp) fault(name + "'s FIFO " + index + " current " + hex(cur) + " passes its end " + hex(end)); cur = cur % (end - base); }
    if (cur < base) cur = base;
    const table = gp ? APU.gpFIFOAddress : APU.epFIFOAddress, maxSGE = gp ? APU.gpFIFOMaxSGE : APU.epFIFOMaxSGE;
    let moved = 0;
    while (moved < len) {
      const chunk = Math.min(len - moved, end - cur);
      scatterGather(reg(table), reg(maxSGE), bytes.subarray(moved, moved + chunk), cur, chunk, toHost, name + "'s FIFO " + index);
      moved += chunk; cur += chunk;
      if (cur >= end) cur = base;
    }
    inner.write(curReg, 4, ((reg(curReg) & ~APU.fifoValueMask) | cur) >>> 0);
  }
  a.gp = dsp56300("the global processor", { scratch: (bytes, addr, len, toHost) => scatterGather(reg(APU.gpScratchAddress), reg(APU.gpScratchMaxSGE), bytes, addr, len, toHost, "the global processor's scratch"), fifo: (bytes, index, len, toHost) => fifoTransfer(true, bytes, index, len, toHost) });
  a.ep = dsp56300("the encode processor", { scratch: (bytes, addr, len, toHost) => scatterGather(reg(APU.epScratchAddress), reg(APU.epScratchMaxSGE), bytes, addr, len, toHost, "the encode processor's scratch"), fifo: (bytes, index, len, toHost) => fifoTransfer(false, bytes, index, len, toHost) });
  return a;
}
function int8(v) { return (v << 24) >> 24; }
// ---- the ADPCM decoder ----
const ADPCM_STEPS = [7, 8, 9, 10, 11, 12, 13, 14, 16, 17, 19, 21, 23, 25, 28, 31, 34, 37, 41, 45, 50, 55, 60, 66, 73, 80, 88, 97, 107, 118, 130, 143, 157, 173, 190, 209, 230, 253, 279, 307, 337, 371, 408, 449, 494, 544, 598, 658, 724, 796, 876, 963, 1060, 1166, 1282, 1411, 1552, 1707, 1878, 2066, 2272, 2499, 2749, 3024, 3327, 3660, 4026, 4428, 4871, 5358, 5894, 6484, 7132, 7845, 8630, 9493, 10442, 11487, 12635, 13899, 15289, 16818, 18500, 20350, 22385, 24623, 27086, 29794, 32767];
const ADPCM_INDEX = [-1, -1, -1, -1, 2, 4, 6, 8];
function adpcmDecode(out, data, size, channels) {
  const pcm = [0, 0], index = [0, 0];
  let at = 0, o = 0;
  for (let ch = 0; ch < channels; ch++) {
    pcm[ch] = (data[at] | (data[at + 1] << 8)) << 16 >> 16; out[o++] = pcm[ch]; index[ch] = data[at + 2];
    if (index[ch] > 88 || data[at + 3] !== 0) fault("an ADPCM block opens with step index " + index[ch] + " and byte " + data[at + 3]);
    at += 4;
  }
  const chunks = Math.floor((size - 4 * channels) / (channels * 4));
  for (let chunk = 0; chunk < chunks; chunk++) {
    for (let ch = 0; ch < channels; ch++) {
      for (let i = 0; i < 4; i++) {
        const byte = data[at++];
        for (let half = 0; half < 2; half++) {
          const nibble = half === 0 ? byte & 0xF : byte >>> 4, step = ADPCM_STEPS[index[ch]];
          let delta = step >> 3;
          if ((nibble & 1) !== 0) delta += step >> 2;
          if ((nibble & 2) !== 0) delta += step >> 1;
          if ((nibble & 4) !== 0) delta += step;
          if ((nibble & 8) !== 0) delta = -delta;
          pcm[ch] = Math.min(Math.max(pcm[ch] + delta, -32768), 32767);
          index[ch] = Math.min(Math.max(index[ch] + ADPCM_INDEX[nibble & 7], 0), 88);
          out[o + ((i * 2 + half) * channels) + ch] = pcm[ch];
        }
      }
    }
    o += 8 * channels;
  }
}
// ---- the state variable filter ----
function svfNew() { return { f: 0, q: 0, qnrm: 0, h: 0, b: 0, l: 0 }; }
function svfSetup(sv, f, q) { sv.f = f; sv.q = q; sv.qnrm = Math.sqrt(q / 2 + 0.01); }
function svfRun(sv, input) {
  const x = sv.qnrm * input;
  sv.b = sv.b - sv.b * sv.b * sv.b * 0.001;
  sv.h = x - sv.l - sv.q * sv.b;
  sv.b = sv.b + sv.f * sv.h;
  sv.l = sv.l + sv.f * sv.b;
  return sv.l;
}
// ---- the head related transfer function filter ----
function hrtfNew() { const bufLen = APU.hrtfTaps + APU.hrtfMaxDelay; return { pos: 0, buf: [new Float32Array(bufLen), new Float32Array(bufLen)], cur: [new Float32Array(APU.hrtfTaps), new Float32Array(APU.hrtfTaps)], tar: [new Float32Array(APU.hrtfTaps), new Float32Array(APU.hrtfTaps)], itdCur: 0, itdTar: 0 }; }
function hrtfClear(f) { f.pos = 0; f.buf[0].fill(0); f.buf[1].fill(0); }
function hrtfSetTarget(f, entry) {
  f.itdTar = Math.max(-APU.hrtfMaxDelay, Math.min(entry.itd, APU.hrtfMaxDelay));
  for (let ch = 0; ch < 2; ch++) {
    const coeff = f.tar[ch]; coeff.set(entry.hrir[ch]);
    let sum = 0;
    for (let k = 0; k < APU.hrtfTaps; k++) sum += Math.abs(coeff[k]);
    if (sum === 0 || sum === 1) break;
    for (let k = 0; k < APU.hrtfTaps; k++) coeff[k] /= sum;
  }
}
function hrtfProcess(f, samples) {
  const bufLen = APU.hrtfTaps + APU.hrtfMaxDelay;
  for (let n = 0; n < APU.frameSamples; n++) {
    for (let ch = 0; ch < 2; ch++) for (let k = 0; k < APU.hrtfTaps; k++) f.cur[ch][k] += APU.hrtfSmoothing * (f.tar[ch][k] - f.cur[ch][k]);
    f.itdCur += APU.hrtfSmoothing * (f.itdTar - f.itdCur);
    for (let ch = 0; ch < 2; ch++) {
      const buf = f.buf[ch], coeff = f.cur[ch];
      buf[f.pos] = samples[n * 2 + ch];
      let d = f.itdCur * (ch === 0 ? 1 : -1);
      if (d < 0) d = 0;
      const di = Math.floor(d), dfrac = d - di;
      let acc = 0;
      for (let k = 0; k < APU.hrtfTaps; k++) {
        const i1 = (f.pos - di - k + bufLen) % bufLen;
        let s = buf[i1];
        if (dfrac > 0) { const i2 = (i1 - 1 + bufLen) % bufLen; s = s * (1 - dfrac) + buf[i2] * dfrac; }
        acc += coeff[k] * s;
      }
      samples[n * 2 + ch] = acc;
    }
    f.pos = (f.pos + 1) % bufLen;
  }
}
`

// The audio processor's register layout, voice structure, front end methods and engine limits as the page names them
func (e *emitter) writeAudioConstants() {
	fields := []struct {
		name  string
		value any
	}{
		{"status", apuStatus}, {"enable", apuEnable}, {"globalBit", apuGlobalBit}, {"trapBit", apuTrapBit}, {"notifyIntBit", apuNotifyIntBit}, {"voiceIntBit", apuVoiceIntBit},
		{"frontControl", apuFrontControl}, {"methodModeMask", apuMethodModeMask}, {"methodModeTrapped", apuMethodModeTrapped}, {"methodModeHalted", apuMethodModeHalted}, {"trapReasonMask", apuTrapReasonMask}, {"trapReasonRequested", apuTrapReasonRequested},
		{"currentVoice", apuCurrentVoice}, {"antecedent", apuAntecedent}, {"antecedentHandle", apuAntecedentHandle}, {"antecedentList", apuAntecedentList}, {"antecedentListShift", apuAntecedentListShift},
		{"trappedMethod", apuTrappedMethod}, {"trappedParameter", apuTrappedParameter}, {"trapForce", apuTrapForce}, {"trapForceIdle", apuTrapForceIdle}, {"voiceTable", apuVoiceTable}, {"idleVoiceMethod", apuIdleVoiceMethod},
		{"notifyAddress", apuFENotifyAddress}, {"feMemoryAddress", apuFEMemoryAddress}, {"feMemoryData", apuFEMemoryData}, {"soundEngine", apuSoundEngine}, {"counterModeMask", apuCounterModeMask},
		{"vpSGEAddress", apuVPSGEAddress}, {"vpSSLAddress", apuVPSSLAddress}, {"gpScratchAddress", apuGPScratchAddress}, {"gpFIFOAddress", apuGPFIFOAddress}, {"epScratchAddress", apuEPScratchAddress}, {"epFIFOAddress", apuEPFIFOAddress},
		{"gpScratchMaxSGE", apuGPScratchMaxSGE}, {"gpFIFOMaxSGE", apuGPFIFOMaxSGE}, {"epScratchMaxSGE", apuEPScratchMaxSGE}, {"epFIFOMaxSGE", apuEPFIFOMaxSGE},
		{"gpOutputFIFOBase", apuGPOutputFIFOBase}, {"gpInputFIFOBase", apuGPInputFIFOBase}, {"epOutputFIFOBase", apuEPOutputFIFOBase}, {"epInputFIFOBase", apuEPInputFIFOBase}, {"fifoStride", apuFIFOStride}, {"fifoValueMask", apuFIFOValueMask}, {"outputFIFOs", apuOutputFIFOs}, {"inputFIFOs", apuInputFIFOs},
		{"mixbins", apuMixbins}, {"voices3D", apu3DVoices}, {"epFrameDivider", apuEPFrameDivider}, {"sgeBytes", apuSGEBytes}, {"pageBytes", apuPageBytes},
		{"notifierBase", apuNotifierBase}, {"notifiersPerVoice", apuNotifiersPerVoice}, {"notifierBytes", apuNotifierBytes}, {"notifierStatusAt", apuNotifierStatusAt}, {"notifierDone", apuNotifierDone},
		{"hrtfEntries", apuHRTFEntries}, {"hrtfTaps", apuHRTFTaps}, {"hrtfMaxDelay", apuHRTFMaxDelay}, {"hrtfSmoothing", voiceHRTFSmoothing},
		{"gpBase", apuGPBase}, {"epBase", apuEPBase}, {"processorBytes", apuProcessorBytes}, {"procXMemory", procXMemory}, {"procMixbufMemory", procMixbufMemory}, {"procYMemory", procYMemory}, {"procPMemory", procPMemory}, {"procReset", procReset}, {"procResetRun", procResetRun}, {"procResetDSP", procResetDSP},
		{"gpXWords", gpXWords}, {"gpYWords", gpYWords}, {"epXWords", epXWords}, {"epYWords", epYWords}, {"procPWords", procPWords}, {"procMixbufWords", procMixbufWords}, {"frameSteps", procFrameSteps},
		{"voices", apuVoices}, {"voiceBytes", apuVoiceBytes}, {"voiceState", apuVoiceState}, {"voiceActiveBit", apuVoiceActiveBit}, {"voicePausedBit", apuVoicePausedBit}, {"voiceLink", apuVoiceLink}, {"voiceLinkMask", apuVoiceLinkMask}, {"noVoice", apuNoVoice},
		{"methodBase", apuMethodBase}, {"methodBytes", apuMethodBytes}, {"frameSamples", apuFrameSamples}, {"sampleHz", apuSampleHz}, {"framesPerAdvance", apuFramesPerAdvance},
		{"pioAntecedent", pioAntecedent}, {"pioVoiceOn", pioVoiceOn}, {"pioVoiceOff", pioVoiceOff}, {"pioVoiceRelease", pioVoiceRelease}, {"pioVoicePause", pioVoicePause}, {"pauseAction", pioPauseAction}, {"pioCurrentVoice", pioCurrentVoice}, {"pioVoiceLock", pioVoiceLock}, {"pioVoiceWordFirst", pioVoiceWordFirst}, {"handleMask", pioHandleMask},
		{"pioFree", pioFree}, {"pioNotify", pioNotify}, {"pioNotifyMode", pioNotifyMode}, {"pioHRTFEntry", pioHRTFEntry}, {"pioVoicePosition", pioVoicePosition}, {"pioContextDMANotify", pioContextDMANotify}, {"pioSSLContextDMA", pioSSLContextDMA}, {"pioCurrentSSL", pioCurrentSSL}, {"pioSubmixHeadroom", pioSubmixHeadroom}, {"pioHRTFHeadroom", pioHRTFHeadroom}, {"pioHRTFSubmixes", pioHRTFSubmixes},
		{"pioVoiceBins", pioVoiceWordFirst + voiceBins}, {"pioVoiceFormat", pioVoiceWordFirst + voiceFormat}, {"pioVoiceEnvelope0", pioVoiceWordFirst + voiceEnvelope0}, {"pioVoiceEnvelopeA", pioVoiceWordFirst + voiceEnvelopeA}, {"pioVoiceEnvelope1", pioVoiceWordFirst + voiceEnvelope1}, {"pioVoiceEnvelopeF", pioVoiceWordFirst + voiceEnvelopeF}, {"pioVoiceMisc", pioVoiceWordFirst + apuVoiceMisc}, {"pioVoiceHRTF", pioVoiceWordFirst + voiceHRTFTarget},
		{"pioVoiceSSLA", pioVoiceSSLA}, {"pioVoiceSSLB", pioVoiceSSLB}, {"pioVoiceVolumeA", pioVoiceWordFirst + voiceVolumeA}, {"pioVoiceVolumeB", pioVoiceWordFirst + voiceVolumeB}, {"pioVoiceVolumeC", pioVoiceWordFirst + voiceVolumeC}, {"pioVoiceLFOEnvelope", pioVoiceWordFirst + apuVoiceLFOEnvelope}, {"pioVoiceFilterA", pioVoiceWordFirst + voiceFilterA}, {"pioVoiceFilterB", pioVoiceWordFirst + voiceFilterB}, {"pioVoicePitch", pioVoiceWordFirst + apuVoiceLink},
		{"pioBufferBase", pioBufferBase}, {"pioBufferLoop", pioBufferLoop}, {"pioBufferCurrent", pioBufferCurrent}, {"pioBufferEnd", pioBufferEnd}, {"pioHRIR", pioHRIR}, {"pioHRIRLast", pioHRIRLast}, {"pioSSLSegmentOffset", pioSSLSegmentOffset}, {"sslSegments", pioSSLSegments},
		{"pioInputSGE", pioInputSGE}, {"pioInputSGEOffset", pioInputSGEOffset}, {"pioOutputBufferBase", pioOutputBufferBase}, {"outputBuffers", pioOutputBuffers}, {"pioOutputSGE", pioOutputSGE}, {"pioOutputSGEOffset", pioOutputSGEOffset}, {"sgeOffsetMask", pioSGEOffsetMask}, {"sslBaseMask", pioSSLBaseMask}, {"sslCountMask", pioSSLCountMask}, {"sslBaseShift", pioSSLBaseShift}, {"headroomMask", pioHeadroomMask},
		{"envelopeFilter", pioEnvelopeFilter}, {"envelopeAmplitude", pioEnvelopeAmplitude},
		{"voiceBins", voiceBins}, {"voiceFormat", voiceFormat}, {"envelope0", voiceEnvelope0}, {"envelopeA", voiceEnvelopeA}, {"envelope1", voiceEnvelope1}, {"envelopeF", voiceEnvelopeF}, {"voiceMisc", apuVoiceMisc}, {"voiceHRTFTarget", voiceHRTFTarget}, {"voiceBufferBase", voiceBufferBase}, {"voiceBufferLoop", voiceBufferLoop}, {"voiceCounts", apuVoiceCounts}, {"voiceOffset", voiceOffset}, {"voiceNext", voiceNext}, {"voiceVolumeA", voiceVolumeA}, {"voiceVolumeB", voiceVolumeB}, {"voiceVolumeC", voiceVolumeC}, {"voiceLFOEnvelope", apuVoiceLFOEnvelope}, {"voiceFilterA", voiceFilterA}, {"voiceFilterB", voiceFilterB},
		{"pitchShift", voicePitchShift}, {"binMask", voiceBinMask}, {"samplesShift", voiceSamplesShift}, {"samplesMask", voiceSamplesMask}, {"multipassBit", voiceMultipassBit}, {"persistBit", voicePersistBit}, {"streamBit", voiceStreamBit}, {"loopBit", voiceLoopBit}, {"clearMixBit", voiceClearMixBit}, {"stereoBit", voiceStereoBit}, {"sampleSizeShift", voiceSampleSizeShift}, {"containerShift", voiceContainerShift}, {"containerADPCM", voiceContainerADPCM},
		{"rateMask", voiceRateMask}, {"timeShift", voiceTimeShift}, {"levelShift", voiceLevelShift}, {"pitchScaleShift", voicePitchScaleShift}, {"filterModeShift", voiceFilterModeShift}, {"newVoiceBit", voiceNewVoiceBit}, {"offsetMask", voiceOffsetMask}, {"levelMask", voiceLevelMask},
		{"amplitudeCountMask", apuAmplitudeCountMask}, {"filterCountMask", 0xFFFF << apuFilterCountShift}, {"filterCountShift", apuFilterCountShift}, {"amplitudePhaseMask", apuAmplitudePhaseMask}, {"amplitudePhaseBit", apuAmplitudePhaseBit}, {"filterPhaseMask", apuFilterPhaseMask}, {"filterPhaseBit", apuFilterPhaseBit},
		{"phaseOff", 0}, {"phaseDelay", 1}, {"phaseAttack", 2}, {"phaseHold", 3}, {"phaseDecay", 4}, {"phaseSustain", 5}, {"phaseRelease", apuPhaseRelease}, {"phaseForcedRelease", apuPhaseForcedRelease}, {"releaseFramesPer", apuReleaseFramesPer},
		{"adpcmBlockBytes", voiceADPCMBlockBytes}, {"adpcmSamples", voiceADPCMSamples}, {"levelRange", voiceEnvelopeRange}, {"volumeUnit", voiceVolumeUnit}, {"decibelsPerUnit", voiceDecibelsPerUnit}, {"pitchUnit", voicePitchUnit}, {"pitchScaleUnit", voicePitchScaleUnit}, {"filterUnit", voiceFilterUnit}, {"filterQUnit", voiceFilterQUnit}, {"filterMinCutoff", voiceFilterMinCutoff}, {"filterMinQ", voiceFilterMinQ}, {"decayBase", voiceDecayBase}, {"decayScale", voiceDecayScale}, {"releaseDecay", voiceReleaseDecay},
	}
	parts := make([]string, 0, len(fields)+1)
	for _, f := range fields {
		switch v := f.value.(type) {
		case float64:
			parts = append(parts, fmt.Sprintf("%s: %v", f.name, v))
		default:
			parts = append(parts, fmt.Sprintf("%s: %d", f.name, v))
		}
	}
	parts = append(parts, fmt.Sprintf("lists: [%s]", voiceListsJS()))
	e.js("const APU = { %s };", joinParts(parts))
}

func joinParts(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += ", "
		}
		out += p
	}
	return out
}

// Writes the audio processor with the voice processor and the two signal processors it runs
func (e *emitter) writeAPU() {
	e.js("// ---- the audio processor ----")
	e.writeAudioConstants()
	e.block(audioProcessorScript)
}
