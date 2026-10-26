# TODO

Goal. Go derives everything from the executable, the XIP data and a Go model of the kernel and BIOS, and writes it all as JSON. The browser runs one loader that carries no values of its own.

Measured on sample/5659 with the staged tree (build/dev of Oct 24). Lifted 1291 routines. Lifted 583 named library routines with no named failure. 35 address keyed routines still fail. 168 lifted library routines are shadowed by JavaScript cases that run before the image.

## 0. Lift the kernel and BIOS

The kernel runs from its own image the way the dashboard does. Nothing below this section is finished until the loader runs the kernel's code for every kernel export, and the stand-ins of sections 2 and 3 are replaced by hardware models under the lifted kernel.

- [ ] Boot. Go performs what the MCPX and the 2BL do, the x-code init of the chipset as the register writes they make, the decrypt, the decompress, the copy, and starts the loader at the kernel entry with the three keys the 2BL passes.
- [ ] Hardware under the kernel, each a loader device whose registers and semantics come from Go tables. PCI configuration space, the PIC and PIT, the SMBus with the SMC and EEPROM, the IDE controller with a disk image built from sample/ as FATX partitions, the DVD drive, the USB host controllers, the NIC, the APU, the NV2A, the flash itself.
- [ ] Threads. The kernel's scheduler runs several threads. The loader runs the kernel's own context switch on lifted code, with the thread stacks as memory.
- [ ] Launch. The kernel's XBE loader reads xboxdash.xbe from the disk image through the IDE model and starts it, which replaces the loader's own image mapping, startup sequence and the hand sequenced initializers, disk and init calls.

## 1. Call order in the loader

- [ ] performCall in derived.js runs its own switch, then ConsoleApi.call, then imageRoutine. Run the image body first for every name the library map carries.
- [ ] The fptr branch of performCall sends a pointer to a named library routine through the JavaScript stand-in before the image. Same fix.
- [ ] Emit the kernel import names from Go. The loader may answer a name only when it is a kernel import or a device call the renderer is the hardware for.
- [ ] callClass decides producer, routine, draw and device by name prefix (D3DX, XG, D3DDevice_Create, D3DDevice_Draw, D3D::). Emit the class per call from Go. XGetDevices and every XGet routine currently count as producers.
- [ ] Remove the 168 shadowing cases once the order is fixed. C runtime: rand, srand, toupper, towlower, sprintf, _snprintf, swprintf, _snwprintf, vswprintf, _stricmp, _strnicmp, wcscpy, wcscat, wcsncpy, wcsncat, wcslen, wcscmp, _wcsicmp, wcsncmp, _wcsnicmp, wcschr, wcsrchr, _wcslwr, _wtoi, _wtol, _itow, _ltow, bsearch, qsort, memcpy, memset, malloc, calloc, realloc, free, atexit, _wfopen, fprintf, fclose, operator_new_array, operator_delete, operator_delete_array, YGXPAXIHP6EX0::?_L, _security_check_cookie.
- [ ] XAPI: CreateFileA, CreateFileW, ReadFile, WriteFile, SetFilePointer, GetFileSize, GetFileTime, CloseHandle, FindFirstFileA, FindNextFileA, FindClose, GetFileAttributesA, SetFileAttributesA, DeleteFileA, CreateDirectoryA, RemoveDirectoryA, GetDiskFreeSpaceExA, GlobalMemoryStatus, GetLastError, SetLastError, XapiSetLastNTError, GetLocalTime, GetSystemTime, SystemTimeToFileTime, FileTimeToSystemTime, FileTimeToLocalFileTime, CompareFileTime, XapipGetTimeZoneBias, XapipQueryTimeZoneInformation, CharUpperW, lstrcpyW, lstrcpynW, lstrcatW, lstrcmpW, lstrcmpiW, lstrlenW, lstrcmpA, lstrcmpiA, lstrlenA, MultiByteToWideChar, IsBadStringPtrA, IsBadWritePtr, GetTickCount, QueryPerformanceCounter, QueryPerformanceFrequency, VirtualAlloc, VirtualFree, GlobalAlloc, LocalAlloc, HeapAlloc, GlobalLock, GlobalUnlock, GlobalFree, LocalFree, HeapFree, GetProcessHeap, WaitForSingleObject, SignalObjectAndWait, CreateEventA, CreateEventW, CreateMutexA, CreateSemaphoreA, SetEvent, ResetEvent, ReleaseMutex, ReleaseSemaphore, Sleep, SwitchToThread, GetCurrentThread, GetCurrentThreadId, GetCurrentProcessId, CreateThread, SetThreadPriority, SetUnhandledExceptionFilter, InitializeCriticalSection, EnterCriticalSection, LeaveCriticalSection, DeleteCriticalSection, DeviceIoControl.
- [ ] XDK: XQueryValue, XGetGameRegion, XGetAudioFlags, XGetVideoFlags, XGetVideoStandard, XGetAVPack, XGetLaunchInfo, XGetDevices, XGetDeviceEnumerationStatus, XPeekDevices, XInitDevices, XInputRegisterLanguages, XSetProcessQuantumLength, XAutoPowerDownResetTimer, XMountUtilityDrive, XMountMUA, XMountMURootA, XUnmountMU, XMUNameFromDriveLetter, XSetFileCacheSize, XLoadSectionA, XFreeSectionA, XGetSectionHandleA, XLoadSectionByHandle, XFreeSectionByHandle, XGetSectionSize, XFindFirstSoundtrack, XFindNextSoundtrack, XFindClose, XGetSoundtrackSongInfo, XOpenSoundtrackSong, XLoadContentSignaturesWithFileName, XCloseContentSignatures, XLaunchNewImageA, XWriteTitleInfoAndRebootA, XapiFormatFATVolume, MU_CreateDeviceObject, MU_CloseDeviceObject, XNetGetEthernetLinkStatus, XNetQosRelease, XNetRandom, XOnlineGetUsers, XOnlineLogon, XOnlineTaskContinue, XOnlineTaskClose, XOnlineNotificationSetState, XOnlineFriendsEnumerateFinish, XOnlineDashUpdate, _XOnlineGetUsersFromHD, _XOnlineGetUserFromMU, _XOnlineAddUserToHD, _XOnlineSetUserInMU, _XOnlineRemoveUserFromHD.
- [ ] D3D and D3DX: D3D_BlockOnTime, D3DResource_AddRef, D3DResource_Release, D3DVertexBuffer_Lock2, D3DDevice_CreateVertexBuffer2, D3DDevice_CreateVertexShader, D3DXCreateMatrixStack, D3DXCreateMeshFVF, D3DXComputeBoundingBox, D3DXMatrixMultiply, D3DXMatrixTranspose, D3DXMatrixInverse, D3DXMatrixScaling, D3DXMatrixTranslation, D3DXMatrixRotationAxis, D3DXMatrixRotationQuaternion, D3DXMatrixAffineTransformation, D3DXMatrixPerspectiveFovLH, D3DXMatrixOrthoLH, D3DXQuaternionMultiply, D3DXQuaternionRotationAxis, D3DXQuaternionRotationYawPitchRoll, D3DXQuaternionSlerp, D3DXVec3CatmullRom, D3DXVec3TransformNormal, D3DXVec4Normalize, and the rest of the D3DX math cases with the matMul to vecNormalize library behind them.

## 2. Kernel surface the loader lacks

The shadows above exist because the stand-in boundary sits at XAPI instead of at the kernel thunk. The kernel is a Go model whose behaviour reaches the loader as data.

- [ ] Serve the kernel exports the XAPI bodies call and the loader does not. NtCreateFile, NtQueryInformationFile, NtSetInformationFile, NtQueryDirectoryFile, NtQueryVolumeInformationFile, NtDeviceIoControlFile, NtFlushBuffersFile, NtDeleteFile, NtAllocateVirtualMemory, NtFreeVirtualMemory, NtQueryVirtualMemory, NtCreateEvent, NtCreateMutant, NtCreateSemaphore, NtWaitForSingleObject, NtWaitForSingleObjectEx, NtYieldExecution, NtDuplicateObject, ExQueryNonVolatileSetting, ExAllocatePool, ExFreePool, MmQueryStatistics, MmAllocateContiguousMemory, MmGetPhysicalAddress, KeQueryPerformanceCounter, KeQueryPerformanceFrequency, KeDelayExecutionThread, KeTickCount, PsCreateSystemThreadEx, HalReadSMBusValue, HalWriteSMBusValue, HalReadSMCTrayState, RtlInitAnsiString, RtlNtStatusToDosError, XeImageFileName, LaunchDataPage. The 379 export names already sit in disasm.go kernelExports.
- [ ] The kernel's own objects as data. The handle table, the object directory, the drive letter links (D to the launch partition, the dashboard's own links), the file system image of every partition with real contents, the EEPROM with its factory section, the clock, the SMC and the SMBus.
- [ ] console.js in full. Every value in it is kernel or hardware data that belongs in Go output. Win32 error codes, NT statuses, XONLINE and HRESULT codes, creation dispositions, file attributes, seek methods, WIN32_FIND_DATAA offsets, OBJECT_ATTRIBUTES and ANSI_STRING and IO_STATUS_BLOCK offsets, FILE_NETWORK_OPEN_INFORMATION offsets, SYSTEMTIME and TIME_FIELDS orders, TIME_ZONE_INFORMATION offsets 4 68 84 88 152 168, the partition device paths, partition 2 as the title partition, partition 5 as the utility drive, partition sizes 0x1F400000 and 0x2EE00000, FIRST_HANDLE 0x1000, FILETIME_EPOCH, REG types, every XC EEPROM index and the sizes of the factory values, 64 MB of memory, PFN database and GPU instance bytes of 0x10000 each, interrupt vector base 0x30 and bus level 27, the rand recurrence constants, the tdata and udata factory directories, the dosError table.
- [ ] console.js behaviours that are invented rather than modelled. GlobalMemoryStatus answers a 50 percent load. ExReadWriteRefurbInfo answers zeros. MmClaimGpuInstanceMemory, AvGetSavedDataAddress, AvSendTVEncoderOption, HalEnableSecureTrayEject, HalGetInterruptVector, KeInitializeInterrupt, KeInitializeDpc, KeConnectInterrupt, HalRegisterShutdownNotification, HalReadWritePCISpace over 256 byte spaces that start zeroed, port_in and port_out over a bare map, NtFsControlFile as not supported, every wait satisfied at once and every lock free, DeviceIoControl as not ready, every memory unit and soundtrack and content routine as absent, every XOnline routine as no link, XLaunchNewImageA and XWriteTitleInfoAndRebootA and XOnlineDashUpdate as a page reload, vswprintf reading 16 stack words past its list, MultiByteToWideChar as an identity map, DbgPrint to the browser console.
- [ ] XcVerifyPKCS1Signature answers 1 through verifySignature. Verify the signature with the key the executable or the kernel holds.
- [ ] XcSHAInit, XcSHAUpdate, XcSHAFinal and XcHMAC run a JavaScript SHA1. The kernel model owns these.
- [ ] XNetRandom and randomBytes draw from Math.random.
- [ ] kernelVariable in system.js names HalBootSMCVideoMode, XboxHardwareInfo, KeTickCount and LaunchDataPage by string. The kernel data exports are a table from Go.
- [ ] performanceFrequency is 733333333 in system.js. TRAY_CLOSED_NO_MEDIA 0x40 and TRAY_CLOSED_MEDIA_PRESENT 0x60 are SMC values in system.js.
- [ ] The CONSOLE object in system.js holds gameRegion, videoStandard, avPack, hardDiskKey, dataPartitionSectors, memoryUnits, disc and hardwareFlags. Console configuration is Go output.
- [ ] PhysicalMemory.allocate in derived.js is the kernel's contiguous allocator searching down from the highest page. Kernel behaviour, Go model.
- [ ] The fs segment model in derived.js fixes KPCR_SELF 0x1C, KPCR_PRCB 0x20 and KPCR_PRCB_DATA 0x28 and an exception chain end of 0xFFFFFFFF.

## 3. GPU and BIOS surface the loader lacks

- [ ] gpu.js tables DRAINED, SELF_CLEARING, ALWAYS_SET and INTERRUPT_STATUS were found by watching where the dashboard spun. Derive them at build time from the image's spin loops, each a load from the register base plus an offset, a mask test and a backward branch, as offset, mask and polarity. REGISTER_SPACE 0x1000000 belongs to the same table.
- [ ] pipeline.js decodes exactly one texel format, COLOR_SZ_I8_A8R8G8B8 through a 256 entry palette, and throws on every other. Texture and DynamicTexture both create their own textures in 5659. Emit the NV2A texel format table from Go and decode by it.
- [ ] describeDeviceTexture refuses linear formats.
- [ ] pipeline.js carries TA argument codes, the TOP name table, GL compare and cull codes, the transform state numbers, the Xbox primitive types, the FVF bits and the wrap modes. Hardware tables, Go output.
- [ ] pipeline.js builds the register combiner GLSL by hand from stage states. The combiner model is hardware behaviour to emit as data the loader compiles, with alphaTestGLSL beside it.
- [ ] scene/layout.go fixes the FVF bits and the NV2A vertex type codes 0x32, 0x40 and 0x22.
- [ ] The video flag bit for widescreen, the 4 by 3 and 16 by 9 display shapes, CLEAR_TARGET 0xF0 and the present parameter offsets 0 and 4 live in runtime.js.
- [ ] A BIOS model in Go. The boot path that maps the header, zero fills the section gaps, sets the launch data page, links the launch drive, fills the EEPROM and starts the executable at its entry point. The loader starts from that image.

## 4. The dashboard engine the loader reimplements

Each hook in installHooks replaces a routine the image carries with JavaScript. Each is a derivation to make.

- [ ] Scripts. runtime.js runs the scene scripts with new Function and with statements after rewriteScript, puts DEF names on window, invents log and EnableInput globals, rewrites behavior blocks and sleep into async loops, resolves names through ScriptScope and fires handlers by walking parents. The executable's own script engine is the thing to lift. Its known gaps follow from this stand-in, handlers compiling in isolation and OnActivate reading inline children before they exist.
- [ ] Script objects. SCRIPT_OBJECT_PROTOTYPES binds Date and Array to the browser's prototypes, installDate replaces Date with the console clock and dateMembers adds the calendar members from days_in_month.
- [ ] The script dispatcher is called with the argument order index, argv, count assumed in callNative. Derive the roles the way main_load_args is derived.
- [ ] Group traversal. sc.group.draw and sc.group.tick are hooked to drawChildren and tickChildren.
- [ ] Events. sc.fire_event is hooked to fireDerivedEvent and event names are strings in the loader, OnADown, OnAUp, OnBDown, OnBUp, OnXDown, OnYDown, OnMoveUp, OnMoveDown, OnMoveLeft, OnMoveRight, OnLeftDown, OnRightDown, OnLeftThumbDown, OnStart, OnEnd, OnEndOfAudio, OnProgressChanged, OnTransportModeChanged, initialize.
- [ ] Timers. sc.timers.advance and sc.timers.remove are hooked to advanceTimers, removeTimers and timerRecord.
- [ ] Scene loading. sc.loader, sc.unloader, sc.image_load, sc.scene_load.reset, join, populate, new_scope and attach are hooked. loadArchive, loadedArchiveFields, loadsInlineArchive and populateInline read the inline loader's paths to fake archive fields. joinPaths reimplements the path join whose roles derivePathJoin already derives.
- [ ] Scene graph construction. buildScene, buildNode, buildGeometry, prepareAppearance and nodeFor build the graph from the exporter's JSON instead of the executable's scene loader. The .xap, .xm, .xbx and .xtf readers in pkg/scene, pkg/xap, pkg/mesh, pkg/buffer, pkg/texture and pkg/font are Go reimplementations of loaders the image carries.
- [ ] Geometry. attachBuffer converts indices to 16 bit, sphereGeometry builds the sphere with slices and stacks, loadMesh reads the exporter's .bin and manifest, loadTexture reads PNG. The node load slot of every class but the own texture ones is hooked to hand back the held buffer.
- [ ] Allocators are hooked to fresh SceneObjects. Decide whether the image's heap runs or the allocator stays a kernel service.
- [ ] Every tick slot of AudioClip, PeriodicAudioGroup, Joystick and ScreenSaver, every tick hook but the timer advance and every input routine is hooked to nothing. RUNTIME_FIELD_CLASSES names them by string.
- [ ] Audio. AudioClip play, pause, stop, volume, loop and playbackSeconds run on WebAudio. PeriodicAudioGroup schedules with setTimeout and Math.random. The field names url, loop, volume, period, periodNoise, archive and the transportMode write into the object are the loader's.
- [ ] Input. onKeyDown and onKeyUp map Enter, Escape, KeyX, KeyY, the arrows and WASD to joystick events, pollGamepad maps buttons and axes, activeJoystick and controlGlobal pick the target, bindJoystick keeps the bound list. Back, Start, the triggers, white, black and the DVD remote are missing.
- [ ] ScreenSaverTimer and the ScreenSaver reset native are the loader's idle timer.
- [ ] The Date, ScreenSaver and Joystick natives in nativeClasses are the loader's.
- [ ] Field access. XAPNode intercepts url, string, isBound, volume, transportMode, texture, geometry, appearance, material, children, length, isActive and brightness by name, and the FIELD type codes 1 2 3 5 6 8 9 10 are repeated in runtime.js and derived.js.
- [ ] checkNatives, scriptValue boxing and scriptResult read the dispatcher's value objects through number_vtable and string_vtable. Keep, but the boxing of a browser value into a SceneObject with scriptValue is a loader construct.
- [ ] The matrix stack. STACK vtable offsets 0xC through 0x44 and STACK_METHODS name ID3DXMatrixStack methods, and matrixStackCall runs them on a JavaScript MatrixStack. The D3DX stack is in the image.
- [ ] D3DX meshes. MESH vtable offsets 0x4 through 0x44, D3DXMESH_32BIT and the D3DXMesh class stand in for ID3DXBaseMesh, which is in the image.
- [ ] bsearch answers the material registry from the Go side registry by name and halves over any other table.
- [ ] The present. sc.present is hooked to presentFrame, which fits the frame by the EEPROM widescreen flag.
- [ ] Page layout. DATA_BASE and the fixed file names config.json, render.json, meshes/manifest.json, scenes, textures, meshes, files and audio are a contract between export.go and the loader. One manifest from Go.
- [ ] index.html loads three from a CDN at a pinned version. Vendor it or emit the version from Go.

## 5. Go hardcodes and guesses

- [ ] SurfaceDesc is the constants 0, 0x14 and 0x18 from symexec.go wrapped as a struct in deriveScene. D3DSurface_GetDesc lifts cleanly and its stores through its second argument are the layout.
- [ ] outBuffers in symexec.go lists which routines write through which pointer with guessed sizes, wideBuffer 0x1000, 1 shifted 20, 320, eepromSize 256 and itowBuffer 66. Every routine in it is in the image. Read the written ranges off the lifted bodies' stores through their argument pointers.
- [ ] decorateSymbol renames the C++ swprintf overload to _snwprintf so outBuffers matches it.
- [ ] outParams, imageOutParams and textureCreations fix which argument of which creation receives the object and the width, height and format slots. Read them off the bodies.
- [ ] gpuRegisterBase takes the first 4 byte immediate store in CMiniport::MapRegisters. Match the field the register reads use.
- [ ] resourceLayout takes the first load followed by an or of a high bit in PixelJar::FindSurfaceWithinTexture.
- [ ] gpuRoutines names the seven routines the loader's GPU calls. Derive the texture reading path or emit the list as a role table.
- [ ] mathIntrinsics maps sin, cos, pow and the rest by name under Lib libcmt, powThunk recognizes the power thunk by shape, nameMemoryRoutines names _memcpy and _memset by shape, inlineSymbols names ?0D3DXCOLOR, kernelCdecl and kernelNoReturn list kernel exports by name, inputRoutines lists the input layer by name, surfaceDescribers lists three describers by name.
- [ ] requiredNatives names Level, Transform and Background. requiredSceneClasses names thirteen classes. The draw chain in deriveDraw names Node, Shape, Appearance, Material, MaxMaterial, Mesh, Sphere, Texture and ImageTexture and requires Transform. Class names in Go should come from the class registry roles, not literals.
- [ ] system.go finds its data through method names, Translator SetLanguage, GetTimeZoneText, GetTimeZoneCount and GetLanguageCode, Date getDaysInMonth, MemoryMonitor selectUp and FormatFreeBlocks, Config GetXdashVersion, GetRecoveryKey, ToggleNoisyCamera, ForceSetLanguage, ForceSetTimeZone and ForceSetClock, SavedGameGrid selectDown, DiscDrive, Text, XcHMAC and XGetDevices, and fixes methodRecordSize 24, methodRecordName 0x14, methodTableSlot 20 and the timeZoneEntry offsets.
- [ ] The Collisions_ prefix is string matched in calleeName, deriveRenderStates and stateNameFromSymbol. It is a name shape from d3d8ltcg.pat. Normalize it once when patterns load.
- [ ] callClobbers special cases _security_check_cookie by name. blockInvariant recognizes global bases by string length 9 and a leading g.
- [ ] convention.go answers an undeclared XDK routine with a guessed fixed register order where it used to fail, and matchParameters breaks ties by passedInOrder. xdkParameters is a hand table of parameter kinds per XDK routine.
- [ ] Arity heuristics. cleanupAfter counts pop eax, ecx and edx as cleanup, consumedByNextCall looks 40 instructions ahead, demandAfter walks 400, callReadsVtable looks 96 back, thunkTarget and cleanupOf follow 8 hops, deriveScriptCalls wants a switch of 8 or more cases and a conversion called twice.
- [ ] Budgets that change results silently. maxLoopUnroll 4, maxPaths 512, maxSteps 60000, maxInlineSize 4000, maxFrameSteps 2500, maxBlockSteps 20000, maxLiftedBlocks 4096, maxPathEffects 262144, stackBufferElems 1024, staticThisRounds 8, sceneCalleeDepth 2, apiConstantBias 96, sameExprBudget 512, and in the loader LIFTED_BLOCK_LIMIT, FRAME_WORK_BUDGET, CODE_BUDGET and WIDE_STRING_LIMIT. Each should fail loudly or be derived from the routine.
- [ ] d3dxBufferSize, argBufferSize and d3dxWritesArg0 size D3DX buffers by name prefix and name lists.
- [ ] Hand maintained reference tables. enums.go render state and texture stage tables keyed by XDK build, nv2a.go NV097 method names, vsh.go microcode fields and register names, derive.go fixedFunctionRegisters, disasm.go kernelExports. Derive from the image where the image carries them (the render state names already cross check the wrapper's method table) and keep the rest as the Go kernel and GPU definitions.
- [ ] export.go maps archives and inlines by file name convention, archive_scene.xap with default as the entry, default_default as the start scene, capitalized inline names, and probes fonts/Xbox.xtf for a font version nothing reads. Derive the archive layout from the executable's path join and the scene files' Inline urls.
- [ ] LIFTED_ENTRY_SP 0x10000 and the FVF bits are declared in both Go and the loader.

## 6. Softened results

- [ ] copyConsoleFiles copies only .xtf and attachFiles registers every listed file. 204 of 208 files in the 5659 build exist as zero length. Copy every file or list none that is missing.
- [ ] deriveConstantLoaders swallows a failing loader unless every loader fails.
- [ ] Natives outside Level, Transform and Background are enqueued as not required, so one that fails to lift is dropped and fails only when a script calls it. The same applies to Node methods, static initializers and vtable methods found through stores.
- [ ] textureFromPath handles stores, fills and copies of 4 byte elements only and has no default case. A 1 or 2 byte store, fill or copy is skipped and the texture is derived with holes.
- [ ] verifySignature answers 1 (see section 2).
- [ ] readIndices assumes 2 byte indices when a raw buffer carries no indexBytes.
- [ ] heapObject leaves a block unbounded when the allocation size is not a number.
- [ ] XLoadSectionA and XFreeSectionA share one case and both answer the section address.

## 7. Bugs and dead code

- [ ] callsLibrary in scene.go is unused.
- [ ] config.Font and font_version are emitted by export.go and read by nothing. fontVersion and the Xbox.xtf probe go with them.
- [ ] The error check after the terminator switch in Lift is unreachable.
- [ ] system.js keeps a run of blank lines where the natives were cut, and sha1, ShaContext, hmacSha1 and concat move to the kernel model.
- [ ] nameMemoryRoutines names routines _memcpy and _memset while the loader's cases are memcpy and memset.
- [ ] deviceTexture decodes every device texture again whenever any physical byte changed, by the store's write counter.
- [ ] GetCurrentThread answers the Win32 pseudo handle 0xFFFFFFFE.
- [ ] open in console.js hands back a directory handle whatever the disposition and access asked.
- [ ] NtOpenFile writes 1 as the status block information for every open.
- [ ] Rebuild sample/4304 with the staged tree. build/dev4304 predates the lifter, so parity with 5659 is unmeasured.

## 8. Dead derivations

- [ ] system.go derives languages, translations, time_zones, device_navigation, block_shift, dashboard_version, language_codes, recovery_key, selection_clock, camera_noise, clock, force_set_flags and disc_types, and the loader reads none of them since the natives run from the image. translate in system.js has no caller. Only methods, memory_unit_type and days_in_month are read. Delete the rest or point the kernel model at it.

## 9. Routines the 5659 build still fails to carry

- [ ] Lift grows past 4096 blocks at 0006D774, 0011A2EC, 0011CA15 and 0011D068.
- [ ] Stack probes reserving a frame of no fixed size at 000728D2, 00073658 and 00129BDD.
- [ ] The stack pointer leaves the frame at 00074584 (sub esp, eax), 000745BF (leave), 000777A8 (lea esp), 000C86A0, 00140CE0 and 0014BDF0 (and esp, -16).
- [ ] Unsupported instructions. STD at 00072C30, REPN SCASB at 00074510, RCR at 00074850, RDTSC at 000C5F25 and 001498A0, MOVNTQ at 00145510, REP CMPSB with a symbolic count at 00124CE4, 001302F4 and 00132BDB.
- [ ] Carry arithmetic after flags the lifter does not carry at 00077660 (sbb) and 0013ECAC (adc).
- [ ] Calls through unresolved memory targets at 00074EF5, 00075957, 0007A098, 000F4A01, 000F4A38, 000F4A58, 000F4A8C, 000F4AAE, 000F5E62 and 000F7C76.
- [ ] XapiFormatTimeOut at 0006FA2C reads EDX on entry and no caller passes it.

## 10. Still open from before

- [ ] The music scene's DynamicTexture. Palette, StarField, ImageFader and AudioVisualizer plot into an eight bit surface. Own texture loads now run from the image, so verify playback end to end.
- [ ] xonlinedash.xbe and update.xbe are second programs with their own scene format, 48 XBG scenes, 45 textures and audio. Xbox Live New Account, Account Recovery and Network Settings hand off to them and the loader reloads the page.
- [ ] Virtual hardware. No memory units, disc, saved games, soundtracks, accounts or network link, and the EEPROM resets on reload. Launching a disc, the launcher, an entry point, the dash update and finishing recovery reload the page.
- [ ] The Memory and Music tunnel meshes used by level transitions are absent from both sample sets. settings_adoc.xip is named by neither executable and never extracted.
- [ ] Rendering fidelity. Flicker filter, soft display filter, gamma ramp, overscan colour, back buffer scale, screen space offset and persist display are dropped in deviceCall. Fog, specular output, point sprites, back face colours and texture coordinate generation throw. Mip, filter, anisotropy, antialias, stencil and z bias states are ignored. No composite or CRT look.
- [ ] Audio. AudioClip pan, frequency, fade and pause on moving are parsed but not applied. CD and soundtrack urls do not play.
- [ ] Verify Japanese, Korean and Chinese menus now that the dashboard loads its own fonts.
