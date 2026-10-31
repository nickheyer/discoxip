package bios

// The interrupt controller's command words, automatic end of interrupt bit, read status and the master's level control port
const (
	picEndOfInterrupt         = 0x20
	picSpecificEndOfInterrupt = 0x60
	picCommandMask            = 0xE0
	picReadRequests           = 0x0A
	picReadService            = 0x0B
	picOperationBit           = 0x08
	picAutoEndOfInterrupt     = 0x02
	picLinesPerController     = 8
	picLevelControlPort       = 0x4D0
)

// The interval timer's command word fields and the port its first channel is read back on
const (
	pitChannelShift = 6
	pitAccessShift  = 4
	pitAccessMask   = 3
	pitAccessLow    = 1
	pitAccessHigh   = 2
	pitAccessBoth   = 3
	pitLastPort     = 0x43
)

// The bus controller's status after a refused transaction and the control bit asking for its interrupt
const (
	smbusRefused          = 0x04
	smbusControlInterrupt = 0x10
)

// The system controller's commands, its tray state with no disc, its version characters and its challenge
const (
	smcVersion           = 0x01
	smcPower             = 0x02
	smcAVPack            = 0x04
	smcCPUTemperature    = 0x09
	smcBoardTemperature  = 0x0A
	smcInterruptReason   = 0x11
	smcScratch           = 0x1B
	smcTrayClosedNoMedia = 0x40
	smcChallenge         = 0x1C
	smcChallengeBytes    = 4
	smcPowerReset        = 0x01
	smcPowerCycle        = 0x40
	smcPowerOff          = 0x80
)

// The first channel's registers of the disk controller, the status and error bits, the drive select bits and the
// control bits
const (
	ataData         = 0x1F0
	ataError        = 0x1F1
	ataCount        = 0x1F2
	ataLBALow       = 0x1F3
	ataLBAMid       = 0x1F4
	ataLBAHigh      = 0x1F5
	ataDrive        = 0x1F6
	ataStatus       = 0x1F7
	ataAltStatus    = 0x3F6
	ataStatusBusy   = 0x80
	ataStatusReady  = 0x40
	ataStatusSeek   = 0x10
	ataStatusData   = 0x08
	ataStatusError  = 0x01
	ataErrorAbort   = 0x04
	ataDriveSlave   = 0x10
	ataControlNoIRQ = 0x02
	ataControlReset = 0x04
	ataSectorBytes  = 512
)

// The disk commands the kernel issues
const (
	ataIdentifyCommand  = 0xEC
	ataIdentifyPacket   = 0xA1
	ataReadDMA          = 0xC8
	ataWriteDMA         = 0xCA
	ataReadSectors      = 0x20
	ataWriteSectors     = 0x30
	ataReadMultiple     = 0xC4
	ataWriteMultiple    = 0xC5
	ataSetMultiple      = 0xC6
	ataSetFeatures      = 0xEF
	ataFlushCache       = 0xE7
	ataStandby          = 0xE0
	ataPacket           = 0xA0
	ataSecurityFreeze   = 0xF5
	ataSecurityUnlock   = 0xF2
	ataSecurityDisable  = 0xF6
	ataSecuritySetPass  = 0xF1
	ataDeviceReset      = 0x08
	ataRecalibrate      = 0x10
	ataInitializeParams = 0x91
	ataSetFeaturesXfer  = 0x03
	ataPacketSignature1 = 0x14
	ataPacketSignature2 = 0xEB
)

// Security feature bit of the identify words, status word bits, password place and size, unlock tries
const (
	ataFeatureWord        = 82
	ataFeatureEnabledWord = 85
	ataSecurityFeature    = 0x0002
	ataSecurityWord       = 128
	ataSecuritySupported  = 0x0001
	ataSecurityEnabled    = 0x0002
	ataSecurityLocked     = 0x0004
	ataSecurityFrozen     = 0x0008
	ataSecurityExpired    = 0x0010
	ataPasswordAt         = 2
	ataPasswordBytes      = 32
	ataUnlockTries        = 5
	ataDigestBytes        = 20
)

// The bus master's registers and bits, and the descriptor table's end mark
const (
	busMasterCommand   = 0
	busMasterStatus    = 2
	busMasterTable     = 4
	busMasterStart     = 0x01
	busMasterToMemory  = 0x08
	busMasterActive    = 0x01
	busMasterFailed    = 0x02
	busMasterInterrupt = 0x04
	busMasterEnd       = 0x80000000
	busMasterSpan      = 0x10000
)

// The packet commands the kernel sends the drive with no disc in it and the sense the drive answers them with
const (
	packetBytes           = 12
	packetTestUnitReady   = 0x00
	packetRequestSense    = 0x03
	packetInquiry         = 0x12
	packetStartStop       = 0x1B
	packetPreventAllow    = 0x1E
	packetReadCapacity    = 0x25
	packetRead10          = 0x28
	packetReadTOC         = 0x43
	packetEventStatus     = 0x4A
	packetModeSense       = 0x5A
	packetReadCD          = 0xBE
	senseNotReady         = 0x02
	senseNoMedium         = 0x3A
	senseBytes            = 18
	packetInterruptBit    = 0x02
	packetInputBit        = 0x01
	packetDataRegister    = 0x1F4
	packetErrorSenseShift = 4
	packetFeatureDMA      = 0x01
)

// The real time clock's ports and registers, binary coded unless the format bit says binary
const (
	cmosAddressPort = 0x70
	cmosDataPort    = 0x71
	cmosSeconds     = 0x00
	cmosMinutes     = 0x02
	cmosHours       = 0x04
	cmosWeekday     = 0x06
	cmosDay         = 0x07
	cmosMonth       = 0x08
	cmosYear        = 0x09
	cmosStatusA     = 0x0A
	cmosStatusB     = 0x0B
	cmosStatusC     = 0x0C
	cmosStatusD     = 0x0D
	cmosCentury     = 0x7F
	cmosUpdating    = 0x80
	cmos24Hour      = 0x02
	cmosBinary      = 0x04
	cmosPowerGood   = 0x80
	cmosPM          = 0x80
)

// The address the MCPX ROM shows its last bytes at and the bus the init table's port accesses are a byte wide on
const (
	romBase        = 0xFFFFFE00
	romBytes       = 0x200
	initAccessSize = 1
	flashTop       = 0xFFFFFFFF
)

// The graphics processor's FIFO, the channel windows aliasing the running channel's put, get and reference
// registers, the channel modes, the cache's access, status and instance registers and the instance memory
const (
	fifoUserBase        = 0x800000
	fifoChannels        = 32
	fifoChannelBytes    = 0x10000
	fifoUserPut         = 0x40
	fifoUserGet         = 0x44
	fifoUserRef         = 0x48
	fifoModeRegister    = 0x2504
	fifoPush0           = 0x3200
	fifoPush1           = 0x3204
	fifoStatus          = 0x3214
	fifoDMAPush         = 0x3220
	fifoDMAInstance     = 0x322C
	fifoDMAPut          = 0x3240
	fifoDMAGet          = 0x3244
	fifoReference       = 0x3248
	fifoPull0           = 0x3250
	fifoAccessBit       = 0x1
	fifoChannelMask     = 0x1F
	fifoEmptyBit        = 0x10
	fifoInstanceMask    = 0xFFFF
	fifoInstanceShift   = 4
	instanceMemoryBase  = 0x700000
	instanceMemoryBytes = 0x100000
)

// The memory objects of the instance memory with their class, adjust and address fields, and the push buffer's
// commands, the jumps, the call and return, the method headers and the FIFO's own reference method
const (
	dmaClassMask        = 0xFFF
	dmaInMemoryClass    = 0x3D
	dmaFromMemoryClass  = 0x02
	dmaToMemoryClass    = 0x03
	dmaAdjustShift      = 20
	dmaAddressMask      = 0xFFFFF000
	pushOldJumpMask     = 0xE0000003
	pushOldJump         = 0x20000000
	pushOldJumpTarget   = 0x1FFFFFFC
	pushFlowMask        = 0x3
	pushJump            = 0x1
	pushCall            = 0x2
	pushTargetMask      = 0xFFFFFFFC
	pushReturn          = 0x00020000
	pushMethodMask      = 0xE0030003
	pushIncreasing      = 0x0
	pushNonIncreasing   = 0x40000000
	pushCountShift      = 18
	pushCountMask       = 0x7FF
	pushSubchannelShift = 13
	pushSubchannelMask  = 0x7
	pushMethodAddress   = 0x1FFC
	pushSubchannels     = 8
	fifoReferenceMethod = 0x50
)

// The graphics processor's master interrupt status with its software bit, each engine's enable register's distance
// from its status register, and the FIFO's runout status
const (
	gpuMasterStatus  = 0x100
	gpuSoftwareBit   = 31
	gpuEnableOffset  = 0x40
	fifoRunoutStatus = 0x2400
)

// The engines' interrupt status registers by the bit each shows in the master status
var gpuInterruptBits = map[uint32]uint32{0x1100: 28, 0x2100: 8, 0x400100: 12, 0x600100: 24, 0x8100: 16, 0x9100: 20}

// The FIFO's hash table register with its base and size fields, the entries' instance, engine, channel and status
// fields, the class in an object's first instance word, the methods taking handles, the 3D class's semaphore
// methods and the pattern class's colour method landing in the graphics engine's pattern colour register
const (
	ramhtRegister        = 0x2210
	ramhtBaseMask        = 0x1F0
	ramhtBaseShift       = 4
	ramhtBaseMove        = 12
	ramhtSizeShift       = 16
	ramhtSizeMask        = 0x3
	ramhtSizeMove        = 12
	ramhtEntryBytes      = 8
	ramhtInstanceMask    = 0xFFFF
	ramhtEngineShift     = 16
	ramhtEngineMask      = 0x3
	ramhtEngineGraphics  = 1
	ramhtStatusBit       = 0x80000000
	objectClassMask      = 0xFF
	handleMethodFirst    = 0x180
	handleMethodLast     = 0x1FC
	kelvinClass          = 0x97
	kelvinSemaphoreDMA   = 0x1A4
	kelvinSemaphoreAt    = 0x1D6C
	kelvinSemaphoreWrite = 0x1D70
	patternClass         = 0x44
	patternColorMethod   = 0x310
	pgraphPatternColor   = 0x400B10
	kelvinClearValue     = 0x1D8C
	pgraphClearValue     = 0x401A88
)

// The refresh flags of the console's video standard and the field rates of the standards they name
const (
	videoRefresh60   = 0x00400000
	videoRefresh50   = 0x00800000
	fieldHz60Scaled  = 60000
	fieldHz60Divisor = 1001
	fieldHz50        = 50
)

// The display's interrupt status register and its vertical blank bit
const (
	pcrtcStatus    = 0x600100
	pcrtcVBlankBit = 0x1
)

// The audio processor's interrupt status and enable registers with the global and front end trap bits, the front
// end's control, current voice, antecedent voice, trapped method and parameter and trap force registers, the voice
// table address and the top, current and next registers of the 2D, 3D and multipass voice lists
const (
	apuStatus              = 0x1000
	apuEnable              = 0x1004
	apuGlobalBit           = 0x1
	apuTrapBit             = 0x10
	apuFrontControl        = 0x1100
	apuMethodModeMask      = 0xE0
	apuMethodModeTrapped   = 0xE0
	apuTrapReasonMask      = 0xF00
	apuTrapReasonRequested = 0xF00
	apuCurrentVoice        = 0x1110
	apuAntecedent          = 0x1118
	apuAntecedentHandle    = 0xFFFF
	apuAntecedentList      = 0x30000
	apuAntecedentListShift = 16
	apuTrappedMethod       = 0x1300
	apuTrappedParameter    = 0x1304
	apuTrapForce           = 0x1504
	apuTrapForceIdle       = 0x8000
	apuVoiceTable          = 0x202C
	apuIdleVoiceMethod     = 0x8000
)

// The voice lists' registers in top, current and next order
var apuVoiceLists = [][3]uint32{{0x2054, 0x2058, 0x205C}, {0x2060, 0x2064, 0x2068}, {0x206C, 0x2070, 0x2074}}

// The voices, their 128 byte structures with the state and link words, the programmed IO methods of the front
// end at the voice processor's window, the voice words the configuration methods fill and the engine's frame
const (
	apuVoices         = 256
	apuVoiceBytes     = 0x80
	apuVoiceState     = 0x54
	apuVoiceActiveBit = 1 << 21
	apuVoicePausedBit = 1 << 18
	apuVoiceLink      = 0x7C
	apuVoiceLinkMask  = 0xFFFF
	apuNoVoice        = 0xFFFF
	apuMethodBase     = 0x20000
	apuMethodBytes    = 0x1000
	pioAntecedent     = 0x120
	pioVoiceOn        = 0x124
	pioVoiceOff       = 0x128
	pioVoiceRelease   = 0x12C
	pioVoicePause     = 0x140
	pioPauseAction    = 1 << 18
	pioCurrentVoice   = 0x2F8
	pioVoiceLock      = 0x2FC
	pioVoiceWordFirst = 0x300
	pioVoiceWordLast  = 0x37C
	pioHandleMask     = 0xFFFF
	apuFrameSamples   = 32
	apuSampleHz       = 48000
)

// The voice's envelope count word with its amplitude and filter counts, the envelope phases in the state word,
// the release and forced release phases and the release rate fields of the amplitude and filter envelopes
const (
	apuVoiceCounts        = 0x34
	apuAmplitudeCountMask = 0xFFFF
	apuFilterCountShift   = 16
	apuAmplitudePhaseMask = 0xF0000000
	apuAmplitudePhaseBit  = 28
	apuFilterPhaseMask    = 0x0F000000
	apuFilterPhaseBit     = 24
	apuPhaseRelease       = 6
	apuPhaseForcedRelease = 7
	apuVoiceLFOEnvelope   = 0x6C
	apuVoiceMisc          = 0x18
	apuReleaseRateMask    = 0xFFF
	apuReleaseFramesPer   = 16
)

// The graphics engine's interrupt status, notify source, trapped address and trapped data registers, the error
// interrupt and notification source bits, the trapped address fields and the no operation method the kernel sends
// with a parameter as a software method
const (
	pgraphInterrupt       = 0x400100
	pgraphNotifySource    = 0x400108
	pgraphTrappedAddress  = 0x400704
	pgraphTrappedData     = 0x400708
	pgraphInterruptError  = 0x100000
	pgraphNotification    = 0x1
	trappedChannelShift   = 20
	trappedSubchannelShft = 16
	methodNoOperation     = 0x100
)

// The graphics engine's surface register with the 3D flip write, read and modulo fields, the increment register
// with its read index bit and the 3D class's flip methods
const (
	pgraphSurface       = 0x400710
	surfaceWriteShift   = 20
	surfaceReadShift    = 24
	surfaceModuloShift  = 28
	surfaceFieldMask    = 0x7
	pgraphIncrement     = 0x40071C
	incrementRead3D     = 0x2
	kelvinFlipRead      = 0x120
	kelvinFlipWrite     = 0x124
	kelvinFlipModulo    = 0x128
	kelvinFlipIncrement = 0x12C
	kelvinFlipStall     = 0x130
	stalledOnMethod     = "method"
	stalledOnFlip       = "flip"
)

// The display controller's start address register, which names the surface the display scans out
const pcrtcStart = 0x600800

// The 3D class's methods the graphics engine executes, by the names the nouveau and xemu register definitions give them
var kelvinMethods = []struct {
	name  string
	value uint32
}{
	{"noOperation", 0x0100}, {"waitForIdle", 0x0110},
	{"contextDMAA", 0x0184}, {"contextDMAB", 0x0188}, {"contextDMAColor", 0x0194}, {"contextDMAZeta", 0x0198}, {"contextDMAVertexA", 0x019C}, {"contextDMAVertexB", 0x01A0},
	{"surfaceClipHorizontal", 0x0200}, {"surfaceClipVertical", 0x0204}, {"surfaceFormat", 0x0208}, {"surfacePitch", 0x020C}, {"surfaceColorOffset", 0x0210}, {"surfaceZetaOffset", 0x0214},
	{"combinerAlphaICW", 0x0260}, {"combinerSpecularFogCW0", 0x0288}, {"combinerSpecularFogCW1", 0x028C}, {"control0", 0x0290}, {"lightControl", 0x0294},
	{"colorMaterial", 0x0298}, {"fogMode", 0x029C}, {"fogGenMode", 0x02A0}, {"fogEnable", 0x02A4}, {"fogColor", 0x02A8}, {"windowClipType", 0x02B4}, {"windowClipHorizontal", 0x02C0}, {"windowClipVertical", 0x02E0},
	{"alphaTestEnable", 0x0300}, {"blendEnable", 0x0304}, {"cullFaceEnable", 0x0308}, {"depthTestEnable", 0x030C}, {"ditherEnable", 0x0310}, {"lightingEnable", 0x0314},
	{"pointParamsEnable", 0x0318}, {"pointSmoothEnable", 0x031C}, {"lineSmoothEnable", 0x0320}, {"polySmoothEnable", 0x0324}, {"skinMode", 0x0328}, {"stencilTestEnable", 0x032C},
	{"polyOffsetPointEnable", 0x0330}, {"polyOffsetLineEnable", 0x0334}, {"polyOffsetFillEnable", 0x0338}, {"alphaFunc", 0x033C}, {"alphaRef", 0x0340},
	{"blendFuncSource", 0x0344}, {"blendFuncDestination", 0x0348}, {"blendColor", 0x034C}, {"blendEquation", 0x0350}, {"depthFunc", 0x0354}, {"colorMask", 0x0358}, {"depthMask", 0x035C},
	{"stencilMask", 0x0360}, {"stencilFunc", 0x0364}, {"stencilFuncRef", 0x0368}, {"stencilFuncMask", 0x036C}, {"stencilOpFail", 0x0370}, {"stencilOpZFail", 0x0374}, {"stencilOpZPass", 0x0378},
	{"shadeMode", 0x037C}, {"lineWidth", 0x0380}, {"polyOffsetScale", 0x0384}, {"polyOffsetBias", 0x0388}, {"frontPolygonMode", 0x038C}, {"backPolygonMode", 0x0390},
	{"clipMin", 0x0394}, {"clipMax", 0x0398}, {"cullFace", 0x039C}, {"frontFace", 0x03A0}, {"normalizationEnable", 0x03A4}, {"materialEmission", 0x03A8}, {"materialAlpha", 0x03B4}, {"specularEnable", 0x03B8}, {"lightEnableMask", 0x03BC},
	{"texgen", 0x03C0}, {"textureMatrixEnable", 0x0420}, {"pointSize", 0x043C},
	{"projectionMatrix", 0x0440}, {"modelViewMatrix", 0x0480}, {"inverseModelViewMatrix", 0x0580}, {"compositeMatrix", 0x0680}, {"textureMatrix", 0x06C0}, {"texgenPlane", 0x0840},
	{"fogParams", 0x09C0}, {"texgenViewModel", 0x09CC}, {"fogPlane", 0x09D0}, {"specularParams", 0x09E0}, {"swathWidth", 0x09F8}, {"flatShadeOp", 0x09FC}, {"sceneAmbient", 0x0A10},
	{"viewportOffset", 0x0A20}, {"pointParams", 0x0A30}, {"eyePosition", 0x0A50}, {"combinerFactor0", 0x0A60}, {"combinerFactor1", 0x0A80}, {"combinerAlphaOCW", 0x0AA0}, {"combinerColorICW", 0x0AC0}, {"colorKey", 0x0AE0}, {"viewportScale", 0x0AF0},
	{"transformProgram", 0x0B00}, {"transformConstant", 0x0B80}, {"backLight", 0x0C00}, {"light", 0x1000},
	{"polygonStippleEnable", 0x147C}, {"edgeFlagEnable", 0x16BC}, {"invalidateVertexCache", 0x1710},
	{"vertexDataArrayOffset", 0x1720}, {"vertexDataArrayFormat", 0x1760}, {"backMaterialAlpha", 0x17AC}, {"backMaterialEmission", 0x17B0}, {"logicOpEnable", 0x17BC}, {"logicOp", 0x17C0}, {"twoSidedLighting", 0x17C4},
	{"eyeDirection", 0x17E0}, {"shaderClipPlaneMode", 0x17F8}, {"beginEnd", 0x17FC}, {"arrayElement16", 0x1800}, {"arrayElement32", 0x1808}, {"drawArrays", 0x1810}, {"inlineArray", 0x1818}, {"eyeVector", 0x181C},
	{"vertexData2F", 0x1880}, {"vertexData2S", 0x1900}, {"vertexData4UB", 0x1940}, {"vertexData4S", 0x1980}, {"vertexData4F", 0x1A00},
	{"textureOffset", 0x1B00}, {"textureFormat", 0x1B04}, {"textureAddress", 0x1B08}, {"textureControl0", 0x1B0C}, {"textureControl1", 0x1B10}, {"textureFilter", 0x1B14}, {"textureImageRect", 0x1B1C}, {"texturePalette", 0x1B20}, {"textureBorderColor", 0x1B24},
	{"textureBumpMatrix", 0x1B28}, {"textureBumpScale", 0x1B38}, {"textureBumpOffset", 0x1B3C},
	{"zMinMaxControl", 0x1D78}, {"antiAliasingControl", 0x1D7C}, {"compressZBuffer", 0x1D80}, {"occludeZStencilEnable", 0x1D84}, {"zStencilClearValue", 0x1D8C}, {"colorClearValue", 0x1D90}, {"clearSurface", 0x1D94}, {"clearRectHorizontal", 0x1D98}, {"clearRectVertical", 0x1D9C},
	{"specularFogFactor", 0x1E20}, {"specularParamsBack", 0x1E28}, {"combinerColorOCW", 0x1E40}, {"combinerControl", 0x1E60}, {"shadowZSlopeThreshold", 0x1E68}, {"shadowDepthFunc", 0x1E6C}, {"shaderStageProgram", 0x1E70}, {"shaderDotMapping", 0x1E74}, {"shaderOtherStageInput", 0x1E78},
	{"transformExecutionMode", 0x1E94}, {"programWriteEnable", 0x1E98}, {"transformProgramLoad", 0x1E9C}, {"transformProgramStart", 0x1EA0}, {"transformConstantLoad", 0x1EA4},
}

// The 3D class's notify, state and report contexts, which the engine keeps without drawing by them
const (
	kelvinContextDMANotifies = 0x0180
	kelvinContextDMAState    = 0x0190
	kelvinContextDMAReport   = 0x01A8
)

// The 3D class method the dashboards write that the register definitions give no name, kept as state and drawn without
var kelvinUnnamed = []uint32{0x1E64}

// The engine's limits and layouts, attribute, program, constant, stage, matrix, combiner, clip, texgen, light, specular, point, skin and cube map sizes
const (
	kelvinAttributes        = 16
	kelvinProgramSlots      = 136
	kelvinConstants         = 192
	kelvinStages            = 4
	kelvinStageStride       = 0x40
	kelvinMatrixWords       = 16
	kelvinCombiners         = 8
	kelvinWindowClips       = 8
	kelvinTexgenStride      = 0x10
	kelvinTexgenPlaneStride = 0x40
	kelvinLights            = 8
	kelvinLightStride       = 0x80
	kelvinBackLightStride   = 0x40
	kelvinSpecularWords     = 6
	kelvinPointParams       = 8
	kelvinSkinMatrices      = 4
	kelvinCubeFaces         = 6
	kelvinCubeFaceAlign     = 128
)

// The transform context slots the matrix, eye, fog plane and viewport methods fill, which vertex programs read as constants
const (
	kelvinXFComposite      = 0x00
	kelvinXFProjection     = 0x04
	kelvinXFModelView      = 0x08
	kelvinXFInverseMV      = 0x0C
	kelvinXFEye            = 0x38
	kelvinXFFogPlane       = 0x39
	kelvinXFViewportScale  = 0x3A
	kelvinXFViewportOffset = 0x3B
	kelvinXFTexgenPlane    = 0x40
	kelvinXFTextureMatrix  = 0x44
	kelvinXFMatrixStride   = 8
)

// The network controller's function on the bus and the register its window is assigned through
const (
	nicBus      = 0
	nicDevice   = 4
	nicFunction = 0
	nicWindow   = 0x10
)

// The network controller's management interface, its status, control and data registers, the control register's
// in use and write bits with its register and address fields, as the forcedeth driver documents them, and the
// address of the console's PHY
const (
	miiStatus       = 0x180
	miiControl      = 0x190
	miiData         = 0x194
	miiInUse        = 0x8000
	miiWrite        = 0x400
	miiRegisterMask = 0x1F
	miiAddressShift = 5
	miiAddressMask  = 0x1F
	miiPHYAddress   = 1
	miiNoPart       = 0xFFFF
)

// The PHY's control register with its self clearing reset bit and its status register as a PHY able to
// autonegotiate every speed with no link partner reads it
const (
	phyControl      = 0
	phyControlReset = 0x8000
	phyStatus       = 1
	phyStatusNoLink = 0x7809
	phyRegisters    = 32
)

// The host controller's function on the bus and the register its window is assigned through
const (
	usbBus      = 0
	usbDevice   = 2
	usbFunction = 0
	usbWindow   = 0x10
)

// The host controller's registers as the OpenHCI specification lays them out and the revision it reports
const (
	hcRevision         = 0x00
	hcControl          = 0x04
	hcCommandStatus    = 0x08
	hcInterruptStatus  = 0x0C
	hcInterruptEnable  = 0x10
	hcInterruptDisable = 0x14
	hcHCCA             = 0x18
	hcPeriodCurrentED  = 0x1C
	hcControlHeadED    = 0x20
	hcControlCurrentED = 0x24
	hcBulkHeadED       = 0x28
	hcBulkCurrentED    = 0x2C
	hcDoneHead         = 0x30
	hcFmInterval       = 0x34
	hcFmRemaining      = 0x38
	hcFmNumber         = 0x3C
	hcPeriodicStart    = 0x40
	hcLSThreshold      = 0x44
	hcRhDescriptorA    = 0x48
	hcRhDescriptorB    = 0x4C
	hcRhStatus         = 0x50
	hcRhPortStatus     = 0x54
	hcRevisionValue    = 0x10
	hcRegisterBytes    = 0x64
)

// The control register's list enables and functional state, the command status bits, the interrupt bits, the
// frame interval fields and the masks of the communications area and descriptor pointers
const (
	hcPeriodicEnable     = 0x4
	hcIsochronousEnable  = 0x8
	hcControlEnable      = 0x10
	hcBulkEnable         = 0x20
	hcStateMask          = 0xC0
	hcStateShift         = 6
	hcStateOperational   = 2
	hcCommandReset       = 0x1
	hcControlFilled      = 0x2
	hcBulkFilled         = 0x4
	hcInterruptOverrun   = 0x1
	hcInterruptDoneHead  = 0x2
	hcInterruptFrame     = 0x4
	hcInterruptOverflow  = 0x20
	hcInterruptRootHub   = 0x40
	hcInterruptMaster    = 0x80000000
	hcFrameIntervalMask  = 0x3FFF
	hcFrameIntervalReset = 0x2EDF
	hcFrameRemainToggle  = 0x80000000
	hcFrameNumberMask    = 0xFFFF
	hcFrameNumberHalf    = 0x8000
	hcHCCAMask           = 0xFFFFFF00
	hcPointerMask        = 0xFFFFFFF0
)

// The root hub with the console's 4 ports powered together without overcurrent protection, the descriptor
// bits the driver may write, the status register's remote wakeup enable and each port's status and change bits
const (
	rhPorts              = 4
	rhDescriptorAReset   = 0x01001204
	rhDescriptorAWritten = 0xFF001F00
	rhRemoteWakeup       = 0x8000
	rhSetRemoteWakeup    = 0x8000
	rhClearRemoteWakeup  = 0x80000000
	portConnected        = 0x1
	portEnabled          = 0x2
	portSuspended        = 0x4
	portClearSuspend     = 0x8
	portReset            = 0x10
	portPowered          = 0x100
	portLowSpeed         = 0x200
	portConnectChange    = 0x10000
	portEnableChange     = 0x20000
	portSuspendChange    = 0x40000
	portResetChange      = 0x100000
	portChangeMask       = 0x1F0000
	portResetMs          = 10
)

// The communications area's interrupt table, frame number and done head, the endpoint descriptor's fields and
// the transfer descriptor's fields with the completion codes the controller writes
const (
	hccaInterruptSlots = 32
	hccaFrameNumber    = 0x80
	hccaDoneHead       = 0x84
	edFunctionMask     = 0x7F
	edEndpointShift    = 7
	edEndpointMask     = 0xF
	edDirectionShift   = 11
	edDirectionMask    = 0x3
	edSkip             = 0x4000
	edIsochronous      = 0x8000
	edMaxPacketShift   = 16
	edMaxPacketMask    = 0x7FF
	edTailPointer      = 4
	edHeadPointer      = 8
	edNextED           = 12
	edHalted           = 0x1
	edToggleCarry      = 0x2
	tdBufferRounding   = 0x40000
	tdDirectionShift   = 19
	tdDirectionMask    = 0x3
	tdDirectionSetup   = 0
	tdDirectionOut     = 1
	tdDirectionIn      = 2
	tdToggleShift      = 24
	tdToggleMask       = 0x3
	tdToggleFromTD     = 0x2
	tdErrorCountShift  = 26
	tdErrorCountMask   = 0x3
	tdConditionShift   = 28
	tdConditionMask    = 0xF
	tdCurrentBuffer    = 4
	tdNextTD           = 8
	tdBufferEnd        = 12
	tdPageMask         = 0xFFF
	ccNoError          = 0
	ccStall            = 4
	ccNotResponding    = 5
	ccDataUnderrun     = 9
	usbFrameMs         = 1
)

// The standard requests a device answers, the request type's direction, type and recipient fields, the other
// recipient a hub's ports answer as, the descriptor types and the setup packet's size
const (
	usbGetStatus        = 0
	usbClearFeature     = 1
	usbSetFeature       = 3
	usbSetAddress       = 5
	usbGetDescriptor    = 6
	usbGetConfiguration = 8
	usbSetConfiguration = 9
	usbGetInterface     = 10
	usbSetInterface     = 11
	usbDirectionIn      = 0x80
	usbTypeMask         = 0x60
	usbTypeStandard     = 0x00
	usbTypeClass        = 0x20
	usbTypeVendor       = 0x40
	usbRecipientMask    = 0x1F
	usbRecipientOther   = 0x03
	usbDescriptorDevice = 1
	usbDescriptorConfig = 2
	usbSetupBytes       = 8
)

// The controller's descriptors as the XID class documents them, vendor 0x045E product 0x0202, one interface
// of class 0x58 subclass 0x42 with endpoint 2 in and out of 32 bytes every 4 ms, and the XID descriptor of type
// 0x42 naming a gamepad with 20 byte input and 6 byte output reports
var (
	gamepadDeviceDescriptor = []byte{0x12, 0x01, 0x10, 0x01, 0x00, 0x00, 0x00, 0x40, 0x5E, 0x04, 0x02, 0x02, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01}
	gamepadConfigDescriptor = []byte{0x09, 0x02, 0x20, 0x00, 0x01, 0x01, 0x00, 0x80, 0x32, 0x09, 0x04, 0x00, 0x00, 0x02, 0x58, 0x42, 0x00, 0x00, 0x07, 0x05, 0x82, 0x03, 0x20, 0x00, 0x04, 0x07, 0x05, 0x02, 0x03, 0x20, 0x00, 0x04}
	gamepadXIDDescriptor    = []byte{0x10, 0x42, 0x00, 0x01, 0x01, 0x01, 0x14, 0x06, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}
)

// The XID requests, the class descriptor type, the report requests and types, the report sizes, the endpoint
// the reports move through and the input report's layout, buttons word, analog buttons and thumbsticks
const (
	xidDescriptorType   = 0x42
	xidGetReport        = 0x01
	xidSetReport        = 0x09
	xidGetCapabilities  = 0x01
	xidReportInput      = 0x0100
	xidReportOutput     = 0x0200
	xidInputReportBytes = 20
	xidOutputBytes      = 6
	xidEndpoint         = 2
	xidButtonsAt        = 2
	xidAnalogAt         = 4
	xidThumbAt          = 12
	xidUp               = 0x1
	xidDown             = 0x2
	xidLeft             = 0x4
	xidRight            = 0x8
	xidStart            = 0x10
	xidBack             = 0x20
	xidLeftThumb        = 0x40
	xidRightThumb       = 0x80
	xidA                = 0
	xidB                = 1
	xidX                = 2
	xidY                = 3
	xidBlack            = 4
	xidWhite            = 5
	xidLeftTrigger      = 6
	xidRightTrigger     = 7
	xidAnalogMax        = 255
	xidThumbMax         = 32767
)

// The port the console wires its first controller port to, of the root hub or of the daughterboard's hub, and
// the root hub port a v1.0 console's daughterboard hangs off
const (
	controllerPort    = 3
	daughterboardPort = 1
)

// The daughterboard's hub as the QEMU hub xemu attaches presents it, a 4 port full speed hub of class 9 with one
// interrupt endpoint carrying its change bitmap, and the hub class requests, descriptor, features and port bits
// of the USB specification's hub chapter
var (
	hubDeviceDescriptor = []byte{0x12, 0x01, 0x10, 0x01, 0x09, 0x00, 0x00, 0x08, 0x09, 0x04, 0xAA, 0x55, 0x01, 0x01, 0x00, 0x00, 0x00, 0x01}
	hubConfigDescriptor = []byte{0x09, 0x02, 0x19, 0x00, 0x01, 0x01, 0x00, 0xE0, 0x00, 0x09, 0x04, 0x00, 0x00, 0x01, 0x09, 0x00, 0x00, 0x00, 0x07, 0x05, 0x81, 0x03, 0x01, 0x00, 0xFF}
	hubClassDescriptor  = []byte{0x09, 0x29, 0x04, 0x0A, 0x00, 0x01, 0x00, 0x00, 0xFF}
)

const (
	hubPorts              = 4
	hubDescriptorType     = 0x29
	hubRecipientHub       = 0x20
	hubRecipientPort      = 0x23
	hubEndpoint           = 1
	hubFeaturePortEnable  = 1
	hubFeaturePortSuspend = 2
	hubFeaturePortReset   = 4
	hubFeaturePortPower   = 8
	hubFeatureChangeBase  = 16
	hubPortConnected      = 0x1
	hubPortEnabled        = 0x2
	hubPortSuspended      = 0x4
	hubPortReset          = 0x10
	hubPortPowered        = 0x100
	hubChangeConnected    = 0x1
	hubChangeEnabled      = 0x2
	hubChangeSuspended    = 0x4
	hubChangeReset        = 0x10
)

// The browser's keys standing in for the controller, by the code the browser reports each key as
var controllerKeys = [][2]string{
	{"ArrowUp", "up"}, {"ArrowDown", "down"}, {"ArrowLeft", "left"}, {"ArrowRight", "right"},
	{"Enter", "a"}, {"KeyA", "a"}, {"Backspace", "b"}, {"KeyB", "b"}, {"KeyX", "x"}, {"KeyY", "y"},
	{"Space", "start"}, {"Escape", "back"}, {"KeyQ", "white"}, {"KeyE", "black"},
	{"KeyZ", "lt"}, {"KeyC", "rt"}, {"KeyV", "lthumb"}, {"KeyN", "rthumb"},
	{"KeyI", "ly+"}, {"KeyK", "ly-"}, {"KeyJ", "lx-"}, {"KeyL", "lx+"},
	{"Numpad8", "ry+"}, {"Numpad5", "ry-"}, {"Numpad4", "rx-"}, {"Numpad6", "rx+"},
}

// The browser's standard gamepad mapping's buttons and axes standing in for the controller, by index
var (
	gamepadButtons = []string{"a", "b", "x", "y", "white", "black", "lt", "rt", "back", "start", "lthumb", "rthumb", "up", "down", "left", "right"}
	gamepadAxes    = []string{"lx", "ly", "rx", "ry"}
)

// The southbridge's function on the bus, the configuration register holding a function's revision, the PCI
// revision the southbridge of each MCPX ROM revision reports with the system controller version string beside
// it and whether a USB daughterboard carries its controller ports, from the Xboxen info the xboxdevwiki keeps,
// and the graphics processor's boot register naming an NV2A A02
const (
	mcpxBus             = 0
	mcpxDevice          = 1
	mcpxFunction        = 0
	pciRevisionRegister = 0x08
	pmcBoot             = 0x0
	pmcBootValue        = 0x02A000A2
)

var mcpxConsoles = []struct {
	revision      uint8
	smcVersion    string
	daughterboard bool
}{{0xB2, "P01", true}, {0xD4, "P05", false}}
