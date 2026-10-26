package bios

// Hardware is the console the kernel boots on: the processor it identifies, the memory it finds, the devices on its
// buses with the identities and resources their configuration spaces hold, and the parts on the system management bus
type Hardware struct {
	Memory          uint32              `json:"memory"`
	CyclesPerInsn   int                 `json:"cycles_per_instruction"`
	ClockHz         uint32              `json:"clock_hz"`
	CPUID           map[string]CPUIDRow `json:"cpuid"`
	PCI             []PCIDevice         `json:"pci"`
	MCPXRevision    int                 `json:"mcpx_revision"`
	SMBus           SMBusParts          `json:"smbus"`
	GPU             GPUIdentity         `json:"gpu"`
	InterruptBase   uint32              `json:"interrupt_base"`
	PITHz           uint32              `json:"pit_hz"`
	FlashMirrorBase uint32              `json:"flash_mirror_base"`
	ROMOverlayBase  uint32              `json:"rom_overlay_base"`
}

// CPUIDRow is what one cpuid leaf answers in each register
type CPUIDRow struct {
	EAX uint32 `json:"EAX"`
	EBX uint32 `json:"EBX"`
	ECX uint32 `json:"ECX"`
	EDX uint32 `json:"EDX"`
}

// PCIDevice is one function on a bus with its configuration space identity and the resources it decodes
type PCIDevice struct {
	Name       string   `json:"name"`
	Bus        int      `json:"bus"`
	Device     int      `json:"device"`
	Function   int      `json:"function"`
	Vendor     uint16   `json:"vendor"`
	ID         uint16   `json:"id"`
	Revision   uint8    `json:"revision"`
	Class      uint32   `json:"class"`
	HeaderType uint8    `json:"header_type"`
	Subsystem  uint32   `json:"subsystem"`
	BARs       []PCIBAR `json:"bars"`
	IRQ        int      `json:"irq"`
	Pin        int      `json:"pin"`
	Bridge     *Bridge  `json:"bridge,omitempty"`
}

// PCIBAR is one base address register, decoding a window of the size given in memory or in port space
type PCIBAR struct {
	Index    int    `json:"index"`
	Size     uint32 `json:"size"`
	IO       bool   `json:"io"`
	Prefetch bool   `json:"prefetch"`
	Alias    bool   `json:"alias"` // the window opens onto memory itself
}

// Bridge is the secondary bus a bridge function leads to
type Bridge struct {
	Secondary int `json:"secondary"`
}

// SMBusParts are the parts answering on the system management bus by their write addresses
type SMBusParts struct {
	SMC        uint8    `json:"smc"`
	SMCVersion string   `json:"smc_version"`
	Challenge  [4]uint8 `json:"smc_challenge"`
	EEPROM     uint8    `json:"eeprom"`
	Encoder    uint8    `json:"encoder"`
	Sensor     uint8    `json:"sensor"`
}

// GPUIdentity is what the graphics chip reports in its identity and strap registers
type GPUIdentity struct {
	Boot0   uint32 `json:"boot0"`
	Strap   uint32 `json:"strap"`
	RAMType uint32 `json:"ram_type"`
}

// The console the retail kernels boot on, a 733 MHz Coppermine beside 64 MB and the MCPX south bridge
func (b *Bank) Hardware() Hardware {
	return Hardware{
		Memory:          64 << 20,
		CyclesPerInsn:   2,
		ClockHz:         733333333,
		MCPXRevision:    b.MCPXRevision(),
		InterruptBase:   0x30,
		PITHz:           1193182,
		FlashMirrorBase: 0xFF000000,
		ROMOverlayBase:  0xFFFFFE00,
		CPUID: map[string]CPUIDRow{
			"0": {EAX: 2, EBX: 0x756E6547, EDX: 0x49656E69, ECX: 0x6C65746E},
			"1": {EAX: 0x68A, EBX: 0, ECX: 0, EDX: 0x0383F9FF},
			"2": {EAX: 0x03020101, EBX: 0, ECX: 0, EDX: 0x0C040882},
		},
		SMBus: SMBusParts{SMC: 0x20, SMCVersion: "P01", Challenge: [4]uint8{0x52, 0x72, 0xEA, 0xF3}, EEPROM: 0xA8, Encoder: 0x8A, Sensor: 0x98},
		GPU:   GPUIdentity{Boot0: 0x02A000A2, Strap: 0, RAMType: 0},
		PCI: []PCIDevice{
			{Name: "host", Bus: 0, Device: 0, Function: 0, Vendor: 0x10DE, ID: 0x02A5, Revision: 0xA1, Class: 0x060000},
			{Name: "memory", Bus: 0, Device: 0, Function: 3, Vendor: 0x10DE, ID: 0x02A6, Revision: 0xA1, Class: 0x050000},
			{Name: "lpc", Bus: 0, Device: 1, Function: 0, Vendor: 0x10DE, ID: 0x01B2, Revision: 0xD1, Class: 0x060100},
			{Name: "smbus", Bus: 0, Device: 1, Function: 1, Vendor: 0x10DE, ID: 0x01B4, Revision: 0xD1, Class: 0x0C0500, BARs: []PCIBAR{{Index: 1, Size: 16, IO: true}, {Index: 2, Size: 16, IO: true}}, IRQ: 11, Pin: 1},
			{Name: "usb0", Bus: 0, Device: 2, Function: 0, Vendor: 0x10DE, ID: 0x01C2, Revision: 0xD1, Class: 0x0C0310, BARs: []PCIBAR{{Index: 0, Size: 4096}}, IRQ: 1, Pin: 1},
			{Name: "usb1", Bus: 0, Device: 3, Function: 0, Vendor: 0x10DE, ID: 0x01C2, Revision: 0xD1, Class: 0x0C0310, BARs: []PCIBAR{{Index: 0, Size: 4096}}, IRQ: 9, Pin: 1},
			{Name: "nic", Bus: 0, Device: 4, Function: 0, Vendor: 0x10DE, ID: 0x01C3, Revision: 0xD1, Class: 0x020000, BARs: []PCIBAR{{Index: 0, Size: 4096}, {Index: 1, Size: 8, IO: true}}, IRQ: 4, Pin: 1},
			{Name: "apu", Bus: 0, Device: 5, Function: 0, Vendor: 0x10DE, ID: 0x01B0, Revision: 0xD1, Class: 0x040100, BARs: []PCIBAR{{Index: 0, Size: 512 << 10}}, IRQ: 5, Pin: 1},
			{Name: "aci", Bus: 0, Device: 6, Function: 0, Vendor: 0x10DE, ID: 0x01B1, Revision: 0xD1, Class: 0x040100, BARs: []PCIBAR{{Index: 0, Size: 256, IO: true}, {Index: 1, Size: 128, IO: true}, {Index: 2, Size: 4096}}, IRQ: 6, Pin: 1},
			{Name: "ide", Bus: 0, Device: 9, Function: 0, Vendor: 0x10DE, ID: 0x01BC, Revision: 0xD1, Class: 0x01018A, BARs: []PCIBAR{{Index: 4, Size: 16, IO: true}}, IRQ: 14, Pin: 1},
			{Name: "agp", Bus: 0, Device: 30, Function: 0, Vendor: 0x10DE, ID: 0x01B7, Revision: 0xA1, Class: 0x060400, HeaderType: 1, Bridge: &Bridge{Secondary: 1}},
			{Name: "gpu", Bus: 1, Device: 0, Function: 0, Vendor: 0x10DE, ID: 0x02A0, Revision: 0xA1, Class: 0x030000, BARs: []PCIBAR{{Index: 0, Size: 16 << 20}, {Index: 1, Size: 128 << 20, Prefetch: true, Alias: true}}, IRQ: 3, Pin: 1},
		},
	}
}
