package bios

import (
	"fmt"

	"github.com/nickheyer/discoxip/pkg/xbe"
)

// The section the flash keeps a copy of, which the 2BL writes back over the kernel on every boot
const dataSection = ".data"

// Image lays the kernel out as the 2BL leaves it in memory, the flash copy of the data section written over it
func (b *Bank) Image() (*xbe.Image, map[uint32]uint32, error) {
	pe, err := ParsePE(b.Kernel)
	if err != nil {
		return nil, nil, err
	}
	img := pe.XBEImage()
	found := false
	for i := range img.Sections {
		s := &img.Sections[i]
		if s.Name != dataSection {
			continue
		}
		if len(b.KernelData) > len(s.Data) {
			return nil, nil, fmt.Errorf("bios: the flash holds %d bytes of kernel data for a %d byte %s section", len(b.KernelData), len(s.Data), dataSection)
		}
		copy(s.Data, b.KernelData)
		found = true
	}
	if !found {
		return nil, nil, fmt.Errorf("bios: the kernel has no %s section for the flash data to fill", dataSection)
	}
	exports, err := pe.Exports()
	if err != nil {
		return nil, nil, err
	}
	byOrdinal := map[uint32]uint32{}
	for _, e := range exports {
		byOrdinal[e.Ordinal] = pe.ImageBase + e.RVA
	}
	return img, byOrdinal, nil
}
