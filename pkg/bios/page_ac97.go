package bios

import "fmt"

// The audio codec interface as the page runs it, the codec's mixer registers answered with what a Sigmatel 9700
// holds and the controller's bus master channels streaming the buffers their descriptor lists name at the sample
// rate of the real clock, the output channel's samples handed to the bridge's audio sink, the SPDIF channel's
// consumed alongside, each completed buffer raising its status bits and the interrupt line the global status
// shows while the kernel enables them, the descriptor list a ring the channel wraps at its last valid index, writes
// to the read only index and position registers dropped
const codecInterfaceScript = `
function aciModel(w) {
  const inner = registerModel(w), regs = inner.registers;
  const s = shared(windowKey(w) + ":aci", () => aciState());
  for (const i of TABLES.hardware.interrupts) if (i.registers.includes((w.base + ACI.busMasterBase + ACI.globalStatus) >>> 0)) s.irq = i.irq;
  const channel = (off) => { const index = (off - ACI.busMasterBase) >>> 4; if (s.channels[index] === null) fault("codec interface channel " + index + " has no bus master"); return s.channels[index]; };
  const mixerLoad = (i) => s.mixer[i] | (s.mixer[i + 1] << 8);
  const mixerStore = (i, v) => { s.mixer[i] = v & 0xFF; s.mixer[i + 1] = (v >>> 8) & 0xFF; };
  function updateStatus(r, newSr) {
    const newMask = newSr & ACI.statusWriteClear, oldMask = r.sr & ACI.statusWriteClear;
    let event = false, level = false;
    if ((newMask ^ oldMask) !== 0) {
      if (newMask === 0) event = true;
      else {
        if ((newMask & ACI.statusLastValid) !== 0 && (r.cr & ACI.controlLastValidInterrupt) !== 0) { event = true; level = true; }
        if ((newMask & ACI.statusComplete) !== 0 && (r.cr & ACI.controlInterruptOnComplete) !== 0) { event = true; level = true; }
      }
    }
    r.sr = newSr;
    if (!event) return;
    if (level) s.globSta |= r.intBit; else s.globSta &= ~r.intBit;
    if (s.irq >= 0) setLine(s.irq, level);
  }
  function fetchDescriptor(r) {
    const at = (r.bdbar + r.civ * ACI.descriptorBytes) >>> 0;
    r.bdValid = true;
    r.bdAddr = physRead(at, 4) & ~3;
    r.bdCtl = physRead(at + 4, 4);
    r.picb = r.bdCtl & ACI.descriptorCountMask;
  }
  function resetChannel(r) {
    r.bdbar = 0; r.civ = 0; r.lvi = 0;
    updateStatus(r, ACI.statusHalted);
    r.picb = 0; r.piv = 0; r.cr &= ACI.controlKeptMask; r.bdValid = false; r.startedAt = -1; r.consumed = 0;
  }
  function mixerReset() {
    s.mixer.fill(0);
    for (const [i, v] of ACI.mixerResetValues) mixerStore(i, v);
    mixerStore(ACI.codecMasterVolume, ACI.codecMasterVolumeReset & ACI.codecMasterVolumeMask);
    mixerStore(ACI.codecPCMOutVolume, ACI.codecPCMOutVolumeReset & ACI.codecPCMOutVolumeMask);
    mixerStore(ACI.codecRecordGain, ACI.codecRecordGainReset & ACI.codecRecordGainMask);
  }
  if (!s.started) { s.started = true; for (const r of s.channels) if (r !== null) resetChannel(r); mixerReset(); }
  // The output volume the codec applies, the master and PCM attenuations multiplied, a mute silencing it
  function outputGain() {
    const master = mixerLoad(ACI.codecMasterVolume), pcm = mixerLoad(ACI.codecPCMOutVolume);
    if (((master | pcm) & ACI.codecMuteBit) !== 0) return 0;
    const steps = (vol, mask) => (mask - (vol & mask)) / mask;
    return steps(master, ACI.codecMasterVolumeSteps) * steps(pcm, ACI.codecPCMVolumeSteps);
  }
  const chunk = new Int16Array(4096);
  function sinkBytes(r, at, bytes) {
    if (r.sink) {
      surfaceFlushRange(at, bytes);
      const gain = outputGain(), view = new DataView(ram.buffer, at, bytes), n = bytes >>> 1;
      for (let i = 0; i < n; i += chunk.length) { const m = Math.min(chunk.length, n - i); for (let k = 0; k < m; k++) chunk[k] = view.getInt16((i + k) * 2, true) * gain; s.sink.push(chunk.subarray(0, m)); }
    }
  }
  function writeAudio(r, max) {
    const bytes = Math.min(r.picb << 1, max);
    if (bytes === 0) return 0;
    if (r.bdAddr + bytes > RAM_BYTES) fault("the codec interface streams " + bytes + " bytes at " + hex(r.bdAddr) + " past memory");
    sinkBytes(r, r.bdAddr, bytes);
    r.bdAddr = (r.bdAddr + bytes) >>> 0;
    return bytes;
  }
  // The descriptor list is a ring ending at the last valid index, the kernel keeping one descriptor with that
  // index at 0 while its completion handler advances nothing, so completing the last valid buffer wraps to the first
  function nextBuffer(r) {
    if (r.civ === r.lvi) { r.civ = 0; r.piv = 1 % ACI.descriptors; return true; }
    r.civ = r.piv; r.piv = (r.piv + 1) % ACI.descriptors;
    return false;
  }
  function transferAudio(r, elapsed) {
    if ((r.sr & ACI.statusHalted) !== 0) return;
    while ((elapsed >>> 1) !== 0) {
      if (!r.bdValid) fetchDescriptor(r);
      if (r.picb === 0) {
        r.sr &= ~ACI.statusLastValidCurrent;
        nextBuffer(r);
        fetchDescriptor(r);
        return;
      }
      const moved = writeAudio(r, elapsed);
      elapsed -= moved;
      r.picb -= moved >>> 1;
      if (r.picb === 0) {
        let newSr = r.sr & ~ACI.statusLastValidCurrent;
        if ((r.bdCtl & ACI.descriptorInterrupt) !== 0) newSr |= ACI.statusComplete;
        if (nextBuffer(r)) newSr |= ACI.statusLastValid;
        fetchDescriptor(r);
        updateStatus(r, newSr);
      }
    }
  }
  // Each channel consumes at the sample rate from the moment its run bit is set
  function advance(now) {
    for (const index of ACI.streamedChannels) {
      const r = s.channels[index];
      if ((r.cr & ACI.controlRun) === 0) { r.startedAt = -1; continue; }
      if (r.startedAt < 0) { r.startedAt = now; r.consumed = 0; }
      const due = Math.floor((now - r.startedAt) * ACI.sampleHz / 1000) - r.consumed;
      if (due <= 0) continue;
      r.consumed += due;
      transferAudio(r, due * ACI.sampleBytes * ACI.channels);
    }
  }
  if (s.advance === null) { s.advance = (now) => { s.nextAt = now + ACI.advanceMs; advance(now); }; TIMED.push(s); }
  function mixerWrite(i, v) {
    s.cas = 0;
    switch (i) {
    case ACI.codecReset: mixerReset(); return;
    case ACI.codecPowerdown: mixerStore(i, (v & ~ACI.codecPowerdownKeep) | (mixerLoad(i) & 0xF)); return;
    case ACI.codecMasterVolume: mixerStore(i, v & ACI.codecMasterVolumeMask); return;
    case ACI.codecPCMOutVolume: mixerStore(i, v & ACI.codecPCMOutVolumeMask); return;
    case ACI.codecRecordGain: mixerStore(i, v & ACI.codecRecordGainMask); return;
    case ACI.codecRecordSelect: mixerStore(i, v & ACI.codecRecordSelectMask); return;
    case ACI.codecVendorID1: case ACI.codecVendorID2: case ACI.codecExtendedID: return;
    case ACI.codecExtendedControl: if ((v & ACI.codecVariableRateBit) === 0) { mixerStore(ACI.codecFrontDACRate, ACI.codecRate48000); mixerStore(ACI.codecLRADCRate, ACI.codecRate48000); } if ((v & ACI.codecVariableMicBit) === 0) mixerStore(ACI.codecMicADCRate, ACI.codecRate48000); mixerStore(i, v); return;
    case ACI.codecFrontDACRate: case ACI.codecLRADCRate: if ((mixerLoad(ACI.codecExtendedControl) & ACI.codecVariableRateBit) !== 0) { if (v !== ACI.codecRate48000) fault("the codec is asked to convert at " + v + " Hz"); mixerStore(i, v); } return;
    case ACI.codecMicADCRate: if ((mixerLoad(ACI.codecExtendedControl) & ACI.codecVariableMicBit) !== 0) { if (v !== ACI.codecRate48000) fault("the codec's microphone is asked to convert at " + v + " Hz"); mixerStore(i, v); } return;
    }
    if (ACI.mixerReadOnly.includes(i)) return;
    fault("write of " + hex(v) + " to codec register " + hex(i));
  }
  function busMasterReadByte(off) {
    if (off === ACI.busMasterBase + ACI.semaphore) { const v = s.cas; s.cas = 1; return v; }
    const r = channel(off), at = off & 0xF;
    switch (at) { case ACI.currentIndex: return r.civ; case ACI.lastValidIndex: return r.lvi; case ACI.prefetchedIndex: return r.piv; case ACI.control: return r.cr; case ACI.status: return r.sr & 0xFF; case ACI.status + 1: return r.sr >>> 8; case ACI.positionInBuffer: return r.picb & 0xFF; case ACI.positionInBuffer + 1: return r.picb >>> 8; }
    fault("read of codec interface bus master byte " + hex(off));
  }
  function busMasterRead(off, size) {
    const at = off - ACI.busMasterBase;
    if (size === 4) {
      if (at === ACI.globalControl) return s.globCnt;
      if (at === ACI.globalStatus) return (s.globSta | ACI.globalCodecReady) >>> 0;
      if (s.channels[at >>> 4] !== null) { const r = channel(off); switch (at & 0xF) { case ACI.bufferList: return r.bdbar; case ACI.currentIndex: return (r.civ | (r.lvi << 8) | (r.sr << 16)) >>> 0; case ACI.positionInBuffer: return (r.picb | (r.piv << 16) | (r.cr << 24)) >>> 0; } }
      return inner.read(off, size);
    }
    if (size === 2 && s.channels[at >>> 4] !== null && ((at & 0xF) === ACI.status || (at & 0xF) === ACI.positionInBuffer)) { const r = channel(off); return (at & 0xF) === ACI.status ? r.sr : r.picb; }
    let v = 0;
    for (let i = 0; i < size; i++) v |= busMasterReadByte(off + i) << (8 * i);
    return v >>> 0;
  }
  function busMasterWriteByte(off, v) {
    const r = channel(off), at = off & 0xF;
    switch (at) {
    case ACI.lastValidIndex:
      if ((r.cr & ACI.controlRun) !== 0 && (r.sr & ACI.statusHalted) !== 0) { r.sr &= ~(ACI.statusHalted | ACI.statusLastValidCurrent); r.civ = r.piv; r.piv = (r.piv + 1) % ACI.descriptors; fetchDescriptor(r); }
      r.lvi = v % ACI.descriptors;
      return;
    case ACI.control:
      if ((v & ACI.controlReset) !== 0) { resetChannel(r); return; }
      r.cr = v & ACI.controlValidMask;
      if ((r.cr & ACI.controlRun) === 0) r.sr |= ACI.statusHalted;
      else { r.civ = r.piv; r.piv = (r.piv + 1) % ACI.descriptors; fetchDescriptor(r); r.sr &= ~ACI.statusHalted; }
      return;
    case ACI.status: r.sr |= v & ~(ACI.statusReadOnly | ACI.statusWriteClear); updateStatus(r, r.sr & ~(v & ACI.statusWriteClear)); return;
    case ACI.currentIndex: case ACI.prefetchedIndex: case ACI.positionInBuffer: case ACI.positionInBuffer + 1: return;
    }
    fault("write of " + hex(v) + " to codec interface bus master byte " + hex(off));
  }
  function busMasterWrite(off, size, v) {
    const at = off - ACI.busMasterBase;
    if (size === 4) {
      if (at === ACI.globalControl) { if ((v & (ACI.globalWarmReset | ACI.globalColdReset)) === 0) s.globCnt = v & ACI.globalControlMask; return; }
      if (at === ACI.globalStatus) { s.globSta = ((s.globSta & ~(v & ACI.globalWriteClear)) | ((v & ~(ACI.globalWriteClear | ACI.globalReadOnly)) & ACI.globalValidMask)) >>> 0; return; }
      if (s.channels[at >>> 4] !== null && (at & 0xF) === ACI.bufferList) { channel(off).bdbar = (v & ~3) >>> 0; return; }
      if (s.channels[at >>> 4] === null || (at & 0xF) > ACI.control) { inner.write(off, size, v); return; }
    }
    if (size === 2 && s.channels[at >>> 4] !== null && (at & 0xF) === ACI.status) { const r = channel(off); r.sr |= v & ~(ACI.statusReadOnly | ACI.statusWriteClear); updateStatus(r, r.sr & ~(v & ACI.statusWriteClear)); return; }
    for (let i = 0; i < size; i++) busMasterWriteByte(off + i, (v >>> (8 * i)) & 0xFF);
  }
  return {
    read(off, size) {
      if (off < ACI.busMasterBase) { s.cas = 0; if (size === 2) return mixerLoad(off); if (size === 1) return s.mixer[off]; return (mixerLoad(off) | (mixerLoad(off + 2) << 16)) >>> 0; }
      return busMasterRead(off, size);
    },
    write(off, size, v) {
      if (off < ACI.busMasterBase) { if (size !== 2) fault("write of " + size + " bytes to codec register " + hex(off)); mixerWrite(off, v & 0xFFFF); return; }
      busMasterWrite(off, size, v >>> 0);
    },
  };
}
function aciState() {
  const channels = [];
  for (let i = 0; i < ACI.channelsEnd; i++) channels.push(ACI.channelIntBits[i] === undefined ? null : { bdbar: 0, civ: 0, lvi: 0, sr: 0, picb: 0, piv: 0, cr: 0, bdValid: false, bdAddr: 0, bdCtl: 0, intBit: ACI.channelIntBits[i], sink: ACI.streamedChannels.indexOf(i) === 0, startedAt: -1, consumed: 0 });
  return { started: false, channels, mixer: new Uint8Array(256), globCnt: 0, globSta: 0, cas: 0, irq: -1, advance: null, nextAt: -1, sink: bridge.audio(ACI.sampleHz) };
}
`

// The codec interface's register layout, bits and the codec's reset values as the page names them
func (e *emitter) writeACIConstants() {
	fields := []struct {
		name  string
		value int
	}{
		{"busMasterBase", aciBusMasterBase}, {"channelBytes", aciChannelBytes}, {"channelsEnd", aciChannelsEnd}, {"bufferList", aciBufferList}, {"currentIndex", aciCurrentIndex}, {"lastValidIndex", aciLastValidIndex}, {"status", aciStatus}, {"positionInBuffer", aciPositionInBuffer}, {"prefetchedIndex", aciPrefetchedIndex}, {"control", aciControl},
		{"globalControl", aciGlobalControl}, {"globalStatus", aciGlobalStatus}, {"semaphore", aciSemaphore}, {"descriptors", aciDescriptors}, {"descriptorBytes", aciDescriptorBytes}, {"sampleBytes", aciSampleBytes}, {"channels", aciChannels}, {"sampleHz", aciSampleHz},
		{"statusComplete", aciStatusComplete}, {"statusLastValid", aciStatusLastValid}, {"statusLastValidCurrent", aciStatusLastValidCurrent}, {"statusHalted", aciStatusHalted}, {"statusWriteClear", aciStatusWriteClear}, {"statusReadOnly", aciStatusHalted | aciStatusLastValidCurrent},
		{"controlInterruptOnComplete", aciControlInterruptOnComplete}, {"controlLastValidInterrupt", aciControlLastValidInterrupt}, {"controlReset", aciControlReset}, {"controlRun", aciControlRun}, {"controlValidMask", aciControlValidMask}, {"controlKeptMask", aciControlKeptMask},
		{"globalWarmReset", aciGlobalWarmReset}, {"globalColdReset", aciGlobalColdReset}, {"globalControlMask", aciGlobalControlMask}, {"globalCodecReady", aciGlobalCodecReady}, {"globalWriteClear", aciGlobalWriteClear}, {"globalReadOnly", aciGlobalReadOnly}, {"globalValidMask", aciGlobalValidMask},
		{"descriptorInterrupt", aciDescriptorInterrupt}, {"descriptorCountMask", aciDescriptorCountMask}, {"advanceMs", aciAdvanceMs},
		{"codecReset", codecReset}, {"codecMasterVolume", codecMasterVolume}, {"codecPCMOutVolume", codecPCMOutVolume}, {"codecRecordSelect", codecRecordSelect}, {"codecRecordGain", codecRecordGain}, {"codecPowerdown", codecPowerdown}, {"codecExtendedID", codecExtendedID}, {"codecExtendedControl", codecExtendedControl}, {"codecFrontDACRate", codecFrontDACRate}, {"codecLRADCRate", codecLRADCRate}, {"codecMicADCRate", codecMicADCRate}, {"codecVendorID1", codecVendorID1}, {"codecVendorID2", codecVendorID2},
		{"codecMasterVolumeReset", codecMasterVolumeReset}, {"codecPCMOutVolumeReset", codecPCMOutVolumeReset}, {"codecRecordGainReset", codecRecordGainReset}, {"codecMasterVolumeMask", codecMasterVolumeMask}, {"codecPCMOutVolumeMask", codecPCMOutVolumeMask}, {"codecRecordGainMask", codecRecordGainMask}, {"codecRecordSelectMask", codecRecordSelectMask}, {"codecPowerdownKeep", codecPowerdownKeep}, {"codecVariableRateBit", codecVariableRateBit}, {"codecVariableMicBit", codecVariableMicBit}, {"codecRate48000", codecRate48000}, {"codecMuteBit", codecMuteBit}, {"codecMasterVolumeSteps", codecMasterVolumeSteps}, {"codecPCMVolumeSteps", codecPCMVolumeSteps},
	}
	parts := make([]string, 0, len(fields)+4)
	for _, f := range fields {
		parts = append(parts, fmt.Sprintf("%s: %d", f.name, f.value))
	}
	parts = append(parts, fmt.Sprintf("channelIntBits: { %d: 0x%X, %d: 0x%X, %d: 0x%X, %d: 0x%X }", aciPCMInChannel, aciGlobalPCMInInt, aciPCMOutChannel, aciGlobalPCMOutInt, aciMicChannel, aciGlobalMicInt, aciSPDIFOutChannel, aciGlobalSPDIFInt))
	parts = append(parts, fmt.Sprintf("streamedChannels: [%d, %d]", aciPCMOutChannel, aciSPDIFOutChannel))
	resets := ""
	for i, r := range codecMixerResets {
		if i > 0 {
			resets += ", "
		}
		resets += fmt.Sprintf("[0x%X, 0x%X]", r[0], r[1])
	}
	parts = append(parts, "mixerResetValues: ["+resets+"]")
	readOnly := ""
	for i, r := range codecReadOnlyRegisters {
		if i > 0 {
			readOnly += ", "
		}
		readOnly += fmt.Sprintf("0x%X", r)
	}
	parts = append(parts, "mixerReadOnly: ["+readOnly+"]")
	e.js("const ACI = { %s };", joinParts(parts))
}

// Writes the audio codec interface with its mixer and bus master channels
func (e *emitter) writeACI() {
	e.js("// ---- the audio codec interface ----")
	e.writeACIConstants()
	e.block(codecInterfaceScript)
}
