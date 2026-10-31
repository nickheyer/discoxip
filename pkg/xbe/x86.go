package xbe

// Bits of the flags word, the bits an interrupt gate clears, and the paging bit of the first control register
const (
	eflagsCarry           = 1 << 0
	eflagsReserved        = 1 << 1
	eflagsParity          = 1 << 2
	eflagsZero            = 1 << 6
	eflagsSign            = 1 << 7
	eflagsTrap            = 1 << 8
	eflagsInterrupt       = 1 << 9
	eflagsDirection       = 1 << 10
	eflagsOverflow        = 1 << 11
	cr0WriteProtect       = 1 << 16
	cr0Paging             = 1 << 31
	cr0TaskSwitched       = 1 << 3
	cr0MonitorCoprocessor = 1 << 1
	cr4PageSizeExtensions = 1 << 4
)

// The vector of the device not available trap a float instruction takes while the task switched bit is set
const vectorDeviceNotAvailable = 7

// Page table facts, a directory entry mapping a large page and the shifts of the two table levels
const (
	pagePresent    = 1 << 0
	pageWritable   = 1 << 1
	pageAccessed   = 1 << 5
	pageDirty      = 1 << 6
	pageLarge      = 1 << 7
	largePageShift = 22
	pageTableSlots = 1024
)

// Descriptor and gate layout, a selector's table index and the gate types that keep or clear the interrupt flag
const (
	descriptorBytes   = 8
	selectorIndexMask = 0xFFF8
	gateTypeMask      = 0x1F00
	gateInterrupt     = 0x0E00
	gateTrap          = 0x0F00
	gatePresentBit    = 0x8000
)

// Where an x87 save area keeps the control, status and tag words and where an extended save area keeps the
// registers and the vector lanes
const (
	fsaveControl  = 0
	fsaveStatus   = 4
	fsaveTag      = 8
	fsaveBytes    = 108
	fxsaveControl = 0
	fxsaveStatus  = 2
	fxsaveTag     = 4
	fxsaveST      = 32
	fxsaveXMM     = 160
	fxsaveSlot    = 16
	fxsaveBytes   = 512
)

// The extended precision format an x87 register is saved in
const (
	extendedBias     = 16383
	extendedMantissa = 63
)

// The status word bits a float compare sets, condition codes zero, two and three
const (
	fswC0 = 0x100
	fswC2 = 0x400
	fswC3 = 0x4000
)
