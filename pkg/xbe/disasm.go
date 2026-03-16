package xbe

import (
	"fmt"
	"sort"

	"golang.org/x/arch/x86/x86asm"
)

// Instruction is a decoded x86 instruction at a virtual address.
type Instruction struct {
	VA   uint32
	Inst x86asm.Inst
	Raw  []byte
}

// Function is a discovered function with its instructions and call targets.
type Function struct {
	EntryVA      uint32
	Name         string // resolved name (kernel import, symbol, or auto-generated)
	Instructions []Instruction
	CallTargets  []uint32 // VAs of functions this function calls
	CalledBy     []uint32 // VAs of functions that call this one
	Size         int      // total bytes
	jsName       string   // deduplicated JavaScript function name (set during transpile)
}

// Disassembly is the complete disassembled image.
type Disassembly struct {
	Image     *Image
	Functions map[uint32]*Function // entry VA → Function
	Imports   map[uint32]string    // thunk VA → kernel export name

	// All instructions indexed by VA for fast lookup
	InsnByVA map[uint32]*Instruction
}

// Disassemble performs full recursive disassembly of the XBE image.
// It starts from the entry point and all kernel thunk targets,
// recursively following CALL and JMP targets to discover all functions.
func Disassemble(img *Image) (*Disassembly, error) {
	d := &Disassembly{
		Image:     img,
		Functions: make(map[uint32]*Function),
		Imports:   make(map[uint32]string),
		InsnByVA:  make(map[uint32]*Instruction),
	}

	// Step 1: Resolve kernel imports from the thunk table.
	d.resolveImports()

	// Step 2: Disassemble ALL sections.
	for i := range img.Sections {
		sec := &img.Sections[i]
		if sec.RawSize == 0 {
			continue
		}
		d.linearSweep(sec)
	}

	// Step 3: Discover function boundaries across all code.
	d.discoverFunctions()

	// Step 4: Identify and name known D3D8 functions.
	d.IdentifyD3DFunctions()

	return d, nil
}

// resolveImports reads the kernel thunk table and maps import addresses to names.
func (d *Disassembly) resolveImports() {
	addr := d.Image.KernThunk
	for {
		val, ok := d.Image.ReadU32(addr)
		if !ok || val == 0 {
			break
		}

		ordinal := val & 0x7FFFFFFF
		if int(ordinal) < len(kernelExports) && kernelExports[ordinal] != "" {
			d.Imports[addr] = kernelExports[ordinal]
		}
		addr += 4
	}
}

// linearSweep disassembles the entire .text section instruction by instruction.
func (d *Disassembly) linearSweep(text *Section) {
	data := text.Data
	va := text.VirtualAddr
	offset := 0

	for offset < len(data) {
		inst, err := x86asm.Decode(data[offset:], 32)
		if err != nil {
			// Invalid instruction — skip one byte
			offset++
			va++
			continue
		}

		raw := make([]byte, inst.Len)
		copy(raw, data[offset:offset+inst.Len])

		insn := &Instruction{
			VA:   va,
			Inst: inst,
			Raw:  raw,
		}
		d.InsnByVA[va] = insn

		offset += inst.Len
		va += uint32(inst.Len)
	}
}

// discoverFunctions identifies function boundaries across all disassembled sections.
func (d *Disassembly) discoverFunctions() {
	// Collect all CALL targets as potential function entries.
	entries := make(map[uint32]bool)
	entries[d.Image.EntryPoint] = true

	for _, insn := range d.InsnByVA {
		if insn.Inst.Op == x86asm.CALL {
			target := resolveTarget(insn)
			if target != 0 && d.InsnByVA[target] != nil {
				entries[target] = true
			}
		}
	}

	// Also look for common function prologues as entry points.
	for va, insn := range d.InsnByVA {
		if insn.Inst.Op == x86asm.PUSH {
			if arg, ok := insn.Inst.Args[0].(x86asm.Reg); ok && arg == x86asm.EBP {
				if next, ok := d.InsnByVA[va+uint32(insn.Inst.Len)]; ok {
					if next.Inst.Op == x86asm.MOV {
						entries[va] = true
					}
				}
			}
		}
	}

	// Sort entries.
	sortedEntries := make([]uint32, 0, len(entries))
	for va := range entries {
		sortedEntries = append(sortedEntries, va)
	}
	sort.Slice(sortedEntries, func(i, j int) bool { return sortedEntries[i] < sortedEntries[j] })

	// Build functions: each spans from entry to next entry or RET.
	for i, entry := range sortedEntries {
		var endVA uint32
		if i+1 < len(sortedEntries) {
			endVA = sortedEntries[i+1]
		} else {
			endVA = entry + 0x10000 // cap at 64KB max
		}

		fn := &Function{EntryVA: entry}
		var callTargets []uint32

		for va := entry; va < endVA; {
			insn, ok := d.InsnByVA[va]
			if !ok {
				va++
				continue
			}

			fn.Instructions = append(fn.Instructions, *insn)

			if insn.Inst.Op == x86asm.CALL {
				target := resolveTarget(insn)
				if target != 0 {
					callTargets = append(callTargets, target)
				}
			}

			if insn.Inst.Op == x86asm.RET {
				va += uint32(insn.Inst.Len)
				break
			}

			va += uint32(insn.Inst.Len)
		}

		if len(fn.Instructions) == 0 {
			continue
		}

		fn.CallTargets = callTargets
		last := fn.Instructions[len(fn.Instructions)-1]
		fn.Size = int(last.VA - entry + uint32(last.Inst.Len))
		d.Functions[entry] = fn
	}

	// Build CalledBy cross-references.
	for callerVA, fn := range d.Functions {
		for _, target := range fn.CallTargets {
			if callee, ok := d.Functions[target]; ok {
				callee.CalledBy = append(callee.CalledBy, callerVA)
			}
		}
	}
}

// resolveTarget extracts the absolute target address from a CALL or JMP instruction.
func resolveTarget(insn *Instruction) uint32 {
	if len(insn.Inst.Args) == 0 {
		return 0
	}
	switch arg := insn.Inst.Args[0].(type) {
	case x86asm.Rel:
		return insn.VA + uint32(insn.Inst.Len) + uint32(int32(arg))
	case x86asm.Imm:
		return uint32(arg)
	}
	return 0
}

// FormatInsn returns a human-readable string for an instruction.
func (d *Disassembly) FormatInsn(insn *Instruction) string {
	text := x86asm.IntelSyntax(insn.Inst, uint64(insn.VA), d.symbolLookup)
	return fmt.Sprintf("%08X  %s", insn.VA, text)
}

// symbolLookup resolves an address to a symbol name for the disassembler.
func (d *Disassembly) symbolLookup(addr uint64) (string, uint64) {
	va := uint32(addr)

	// Check kernel imports
	if name, ok := d.Imports[va]; ok {
		return name, 0
	}

	// Check function entries
	if fn, ok := d.Functions[va]; ok && fn.Name != "" {
		return fn.Name, 0
	}

	return "", 0
}

// kernel export ordinal table (from ghidra-xbe XbeLoader.java)
var kernelExports = [379]string{
	"",                                     // 0
	"AvGetSavedDataAddress",                // 1
	"AvSendTVEncoderOption",                // 2
	"AvSetDisplayMode",                     // 3
	"AvSetSavedDataAddress",                // 4
	"DbgBreakPoint",                        // 5
	"DbgBreakPointWithStatus",              // 6
	"DbgLoadImageSymbols",                  // 7
	"DbgPrint",                             // 8
	"HalReadSMCTrayState",                  // 9
	"DbgPrompt",                            // 10
	"DbgUnLoadImageSymbols",                // 11
	"ExAcquireReadWriteLockExclusive",      // 12
	"ExAcquireReadWriteLockShared",         // 13
	"ExAllocatePool",                       // 14
	"ExAllocatePoolWithTag",                // 15
	"ExEventObjectType",                    // 16
	"ExFreePool",                           // 17
	"ExInitializeReadWriteLock",            // 18
	"ExInterlockedAddLargeInteger",         // 19
	"ExInterlockedAddLargeStatistic",       // 20
	"ExInterlockedCompareExchange64",       // 21
	"ExMutantObjectType",                   // 22
	"ExQueryPoolBlockSize",                 // 23
	"ExQueryNonVolatileSetting",            // 24
	"ExReadWriteRefurbInfo",                // 25
	"ExRaiseException",                     // 26
	"ExRaiseStatus",                        // 27
	"ExReleaseReadWriteLock",               // 28
	"ExSaveNonVolatileSetting",             // 29
	"ExSemaphoreObjectType",                // 30
	"ExTimerObjectType",                    // 31
	"ExfInterlockedInsertHeadList",         // 32
	"ExfInterlockedInsertTailList",         // 33
	"ExfInterlockedRemoveHeadList",         // 34
	"FscGetCacheSize",                      // 35
	"FscInvalidateIdleBlocks",              // 36
	"FscSetCacheSize",                      // 37
	"HalClearSoftwareInterrupt",            // 38
	"HalDisableSystemInterrupt",            // 39
	"HalDiskCachePartitionCount",           // 40
	"HalDiskModelNumber",                   // 41
	"HalDiskSerialNumber",                  // 42
	"HalEnableSystemInterrupt",             // 43
	"HalGetInterruptVector",                // 44
	"HalReadSMBusValue",                    // 45
	"HalReadWritePCISpace",                 // 46
	"HalRegisterShutdownNotification",      // 47
	"HalRequestSoftwareInterrupt",          // 48
	"HalReturnToFirmware",                  // 49
	"HalWriteSMBusValue",                   // 50
	"InterlockedCompareExchange",           // 51
	"InterlockedDecrement",                 // 52
	"InterlockedIncrement",                 // 53
	"InterlockedExchange",                  // 54
	"InterlockedExchangeAdd",               // 55
	"InterlockedFlushSList",                // 56
	"InterlockedPopEntrySList",             // 57
	"InterlockedPushEntrySList",            // 58
	"IoAllocateIrp",                        // 59
	"IoBuildAsynchronousFsdRequest",        // 60
	"IoBuildDeviceIoControlRequest",        // 61
	"IoBuildSynchronousFsdRequest",         // 62
	"IoCheckShareAccess",                   // 63
	"IoCompletionObjectType",               // 64
	"IoCreateDevice",                       // 65
	"IoCreateFile",                         // 66
	"IoCreateSymbolicLink",                 // 67
	"IoDeleteDevice",                       // 68
	"IoDeleteSymbolicLink",                 // 69
	"IoDeviceObjectType",                   // 70
	"IoFileObjectType",                     // 71
	"IoFreeIrp",                            // 72
	"IoInitializeIrp",                      // 73
	"IoInvalidDeviceRequest",               // 74
	"IoQueryFileInformation",               // 75
	"IoQueryVolumeInformation",             // 76
	"IoQueueThreadIrp",                     // 77
	"IoRemoveShareAccess",                  // 78
	"IoSetIoCompletion",                    // 79
	"IoSetShareAccess",                     // 80
	"IoStartNextPacket",                    // 81
	"IoStartNextPacketByKey",               // 82
	"IoStartPacket",                        // 83
	"IoSynchronousDeviceIoControlRequest",  // 84
	"IoSynchronousFsdRequest",              // 85
	"IofCallDriver",                        // 86
	"IofCompleteRequest",                   // 87
	"KdDebuggerEnabled",                    // 88
	"KdDebuggerNotPresent",                 // 89
	"IoDismountVolume",                     // 90
	"IoDismountVolumeByName",               // 91
	"KeAlertResumeThread",                  // 92
	"KeAlertThread",                        // 93
	"KeBoostPriorityThread",                // 94
	"KeBugCheck",                           // 95
	"KeBugCheckEx",                         // 96
	"KeCancelTimer",                        // 97
	"KeConnectInterrupt",                   // 98
	"KeDelayExecutionThread",               // 99
	"KeDisconnectInterrupt",                // 100
	"KeEnterCriticalRegion",                // 101
	"MmGlobalData",                         // 102
	"KeGetCurrentIrql",                     // 103
	"KeGetCurrentThread",                   // 104
	"KeInitializeApc",                      // 105
	"KeInitializeDeviceQueue",              // 106
	"KeInitializeDpc",                      // 107
	"KeInitializeEvent",                    // 108
	"KeInitializeInterrupt",                // 109
	"KeInitializeMutant",                   // 110
	"KeInitializeQueue",                    // 111
	"KeInitializeSemaphore",                // 112
	"KeInitializeTimerEx",                  // 113
	"KeInsertByKeyDeviceQueue",             // 114
	"KeInsertDeviceQueue",                  // 115
	"KeInsertHeadQueue",                    // 116
	"KeInsertQueue",                        // 117
	"KeInsertQueueApc",                     // 118
	"KeInsertQueueDpc",                     // 119
	"KeInterruptTime",                      // 120
	"KeIsExecutingDpc",                     // 121
	"KeLeaveCriticalRegion",                // 122
	"KePulseEvent",                         // 123
	"KeQueryBasePriorityThread",            // 124
	"KeQueryInterruptTime",                 // 125
	"KeQueryPerformanceCounter",            // 126
	"KeQueryPerformanceFrequency",          // 127
	"KeQuerySystemTime",                    // 128
	"KeRaiseIrqlToDpcLevel",                // 129
	"KeRaiseIrqlToSynchLevel",              // 130
	"KeReleaseMutant",                      // 131
	"KeReleaseSemaphore",                   // 132
	"KeRemoveByKeyDeviceQueue",             // 133
	"KeRemoveDeviceQueue",                  // 134
	"KeRemoveEntryDeviceQueue",             // 135
	"KeRemoveQueue",                        // 136
	"KeRemoveQueueDpc",                     // 137
	"KeResetEvent",                         // 138
	"KeRestoreFloatingPointState",          // 139
	"KeResumeThread",                       // 140
	"KeRundownQueue",                       // 141
	"KeSaveFloatingPointState",             // 142
	"KeSetBasePriorityThread",              // 143
	"KeSetDisableBoostThread",              // 144
	"KeSetEvent",                           // 145
	"KeSetEventBoostPriority",              // 146
	"KeSetPriorityProcess",                 // 147
	"KeSetPriorityThread",                  // 148
	"KeSetTimer",                           // 149
	"KeSetTimerEx",                         // 150
	"KeStallExecutionProcessor",            // 151
	"KeSuspendThread",                      // 152
	"KeSynchronizeExecution",               // 153
	"KeSystemTime",                         // 154
	"KeTestAlertThread",                    // 155
	"KeTickCount",                          // 156
	"KeTimeIncrement",                      // 157
	"KeWaitForMultipleObjects",             // 158
	"KeWaitForSingleObject",               // 159
	"KfRaiseIrql",                          // 160
	"KfLowerIrql",                          // 161
	"KiBugCheckData",                       // 162
	"KiUnlockDispatcherDatabase",           // 163
	"LaunchDataPage",                       // 164
	"MmAllocateContiguousMemory",           // 165
	"MmAllocateContiguousMemoryEx",         // 166
	"MmAllocateSystemMemory",               // 167
	"MmClaimGpuInstanceMemory",             // 168
	"MmCreateKernelStack",                  // 169
	"MmDeleteKernelStack",                  // 170
	"MmFreeContiguousMemory",               // 171
	"MmFreeSystemMemory",                   // 172
	"MmGetPhysicalAddress",                 // 173
	"MmIsAddressValid",                     // 174
	"MmLockUnlockBufferPages",              // 175
	"MmLockUnlockPhysicalPage",             // 176
	"MmMapIoSpace",                         // 177
	"MmPersistContiguousMemory",            // 178
	"MmQueryAddressProtect",                // 179
	"MmQueryAllocationSize",                // 180
	"MmQueryStatistics",                    // 181
	"MmSetAddressProtect",                  // 182
	"MmUnmapIoSpace",                       // 183
	"NtAllocateVirtualMemory",              // 184
	"NtCancelTimer",                        // 185
	"NtClearEvent",                         // 186
	"NtClose",                              // 187
	"NtCreateDirectoryObject",              // 188
	"NtCreateEvent",                        // 189
	"NtCreateFile",                         // 190
	"NtCreateIoCompletion",                 // 191
	"NtCreateMutant",                       // 192
	"NtCreateSemaphore",                    // 193
	"NtCreateTimer",                        // 194
	"NtDeleteFile",                         // 195
	"NtDeviceIoControlFile",                // 196
	"NtDuplicateObject",                    // 197
	"NtFlushBuffersFile",                   // 198
	"NtFreeVirtualMemory",                  // 199
	"NtFsControlFile",                      // 200
	"NtOpenDirectoryObject",                // 201
	"NtOpenFile",                           // 202
	"NtOpenSymbolicLinkObject",             // 203
	"NtProtectVirtualMemory",               // 204
	"NtPulseEvent",                         // 205
	"NtQueueApcThread",                     // 206
	"NtQueryDirectoryFile",                 // 207
	"NtQueryDirectoryObject",               // 208
	"NtQueryEvent",                         // 209
	"NtQueryFullAttributesFile",            // 210
	"NtQueryInformationFile",               // 211
	"NtQueryIoCompletion",                  // 212
	"NtQueryMutant",                        // 213
	"NtQuerySemaphore",                     // 214
	"NtQuerySymbolicLinkObject",            // 215
	"NtQueryTimer",                         // 216
	"NtQueryVirtualMemory",                 // 217
	"NtQueryVolumeInformationFile",         // 218
	"NtReadFile",                           // 219
	"NtReadFileScatter",                    // 220
	"NtReleaseMutant",                      // 221
	"NtReleaseSemaphore",                   // 222
	"NtRemoveIoCompletion",                 // 223
	"NtResumeThread",                       // 224
	"NtSetEvent",                           // 225
	"NtSetInformationFile",                 // 226
	"NtSetIoCompletion",                    // 227
	"NtSetSystemTime",                      // 228
	"NtSetTimerEx",                         // 229
	"NtSignalAndWaitForSingleObjectEx",     // 230
	"NtSuspendThread",                      // 231
	"NtUserIoApcDispatcher",                // 232
	"NtWaitForSingleObject",                // 233
	"NtWaitForSingleObjectEx",              // 234
	"NtWaitForMultipleObjectsEx",           // 235
	"NtWriteFile",                          // 236
	"NtWriteFileGather",                    // 237
	"NtYieldExecution",                     // 238
	"ObCreateObject",                       // 239
	"ObDirectoryObjectType",                // 240
	"ObInsertObject",                       // 241
	"ObMakeTemporaryObject",                // 242
	"ObOpenObjectByName",                   // 243
	"ObOpenObjectByPointer",                // 244
	"ObpObjectHandleTable",                 // 245
	"ObReferenceObjectByHandle",            // 246
	"ObReferenceObjectByName",              // 247
	"ObReferenceObjectByPointer",           // 248
	"ObSymbolicLinkObjectType",             // 249
	"ObfDereferenceObject",                 // 250
	"ObfReferenceObject",                   // 251
	"PhyGetLinkState",                      // 252
	"PhyInitialize",                        // 253
	"PsCreateSystemThread",                 // 254
	"PsCreateSystemThreadEx",               // 255
	"PsQueryStatistics",                    // 256
	"PsSetCreateThreadNotifyRoutine",       // 257
	"PsTerminateSystemThread",              // 258
	"PsThreadObjectType",                   // 259
	"RtlAnsiStringToUnicodeString",         // 260
	"RtlAppendStringToString",              // 261
	"RtlAppendUnicodeStringToString",       // 262
	"RtlAppendUnicodeToString",             // 263
	"RtlAssert",                            // 264
	"RtlCaptureContext",                    // 265
	"RtlCaptureStackBackTrace",             // 266
	"RtlCharToInteger",                     // 267
	"RtlCompareMemory",                     // 268
	"RtlCompareMemoryUlong",                // 269
	"RtlCompareString",                     // 270
	"RtlCompareUnicodeString",              // 271
	"RtlCopyString",                        // 272
	"RtlCopyUnicodeString",                 // 273
	"RtlCreateUnicodeString",               // 274
	"RtlDowncaseUnicodeChar",               // 275
	"RtlDowncaseUnicodeString",             // 276
	"RtlEnterCriticalSection",              // 277
	"RtlEnterCriticalSectionAndRegion",     // 278
	"RtlEqualString",                       // 279
	"RtlEqualUnicodeString",                // 280
	"RtlExtendedIntegerMultiply",           // 281
	"RtlExtendedLargeIntegerDivide",        // 282
	"RtlExtendedMagicDivide",               // 283
	"RtlFillMemory",                        // 284
	"RtlFillMemoryUlong",                   // 285
	"RtlFreeAnsiString",                    // 286
	"RtlFreeUnicodeString",                 // 287
	"RtlGetCallersAddress",                 // 288
	"RtlInitAnsiString",                    // 289
	"RtlInitUnicodeString",                 // 290
	"RtlInitializeCriticalSection",         // 291
	"RtlIntegerToChar",                     // 292
	"RtlIntegerToUnicodeString",            // 293
	"RtlLeaveCriticalSection",              // 294
	"RtlLeaveCriticalSectionAndRegion",     // 295
	"RtlLowerChar",                         // 296
	"RtlMapGenericMask",                    // 297
	"RtlMoveMemory",                        // 298
	"RtlMultiByteToUnicodeN",               // 299
	"RtlMultiByteToUnicodeSize",            // 300
	"RtlNtStatusToDosError",                // 301
	"RtlRaiseException",                    // 302
	"RtlRaiseStatus",                       // 303
	"RtlTimeFieldsToTime",                  // 304
	"RtlTimeToTimeFields",                  // 305
	"RtlTryEnterCriticalSection",           // 306
	"RtlUlongByteSwap",                     // 307
	"RtlUnicodeStringToAnsiString",         // 308
	"RtlUnicodeStringToInteger",            // 309
	"RtlUnicodeToMultiByteN",               // 310
	"RtlUnicodeToMultiByteSize",            // 311
	"RtlUnwind",                            // 312
	"RtlUpcaseUnicodeChar",                 // 313
	"RtlUpcaseUnicodeString",               // 314
	"RtlUpcaseUnicodeToMultiByteN",         // 315
	"RtlUpperChar",                         // 316
	"RtlUpperString",                       // 317
	"RtlUshortByteSwap",                    // 318
	"RtlWalkFrameChain",                    // 319
	"RtlZeroMemory",                        // 320
	"XboxEEPROMKey",                        // 321
	"XboxHardwareInfo",                     // 322
	"XboxHDKey",                            // 323
	"XboxKrnlVersion",                      // 324
	"XboxSignatureKey",                     // 325
	"XeImageFileName",                      // 326
	"XeLoadSection",                        // 327
	"XeUnloadSection",                      // 328
	"READ_PORT_BUFFER_UCHAR",               // 329
	"READ_PORT_BUFFER_USHORT",              // 330
	"READ_PORT_BUFFER_ULONG",              // 331
	"WRITE_PORT_BUFFER_UCHAR",              // 332
	"WRITE_PORT_BUFFER_USHORT",             // 333
	"WRITE_PORT_BUFFER_ULONG",              // 334
	"XcSHAInit",                            // 335
	"XcSHAUpdate",                          // 336
	"XcSHAFinal",                           // 337
	"XcRC4Key",                             // 338
	"XcRC4Crypt",                           // 339
	"XcHMAC",                               // 340
	"XcPKEncPublic",                        // 341
	"XcPKDecPrivate",                       // 342
	"XcPKGetKeyLen",                        // 343
	"XcVerifyPKCS1Signature",               // 344
	"XcModExp",                             // 345
	"XcDESKeyParity",                       // 346
	"XcKeyTable",                           // 347
	"XcBlockCrypt",                         // 348
	"XcBlockCryptCBC",                      // 349
	"XcCryptService",                       // 350
	"XcUpdateCrypto",                       // 351
	"RtlRip",                               // 352
	"XboxLANKey",                           // 353
	"XboxAlternateSignatureKeys",           // 354
	"XePublicKeyData",                      // 355
	"HalBootSMCVideoMode",                  // 356
	"IdexChannelObject",                    // 357
	"HalIsResetOrShutdownPending",          // 358
	"IoMarkIrpMustComplete",                // 359
	"HalInitiateShutdown",                  // 360
	"RtlSnprintf",                          // 361
	"RtlSprintf",                           // 362
	"RtlVsnprintf",                         // 363
	"RtlVsprintf",                          // 364
	"HalEnableSecureTrayEject",             // 365
	"HalWriteSMCScratchRegister",           // 366
	"",                                     // 367
	"",                                     // 368
	"",                                     // 369
	"",                                     // 370
	"",                                     // 371
	"",                                     // 372
	"",                                     // 373
	"MmDbgAllocateMemory",                  // 374
	"MmDbgFreeMemory",                      // 375
	"MmDbgQueryAvailablePages",             // 376
	"MmDbgReleaseAddress",                  // 377
	"MmDbgWriteCheck",                      // 378
}
