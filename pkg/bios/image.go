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

// Lifted is the 2BL and the kernel as the processor runs them, pooled together, beside the kernel image they came from
type Lifted struct {
	Model   *xbe.MachineModel
	Kernel  *xbe.Image
	Exports map[uint32]uint32
}

// Machine lifts the 2BL and the kernel as the processor runs them, pooled together for the loader
func (b *Bank) Machine() (*Lifted, error) {
	pool := xbe.NewExprPool()
	bldr, err := xbe.DisassembleKernel(b.BldrImage(), map[uint32]uint32{})
	if err != nil {
		return nil, err
	}
	mm, err := xbe.LiftMachineInto(bldr, b.BldrRoots(), pool)
	if err != nil {
		return nil, err
	}
	img, exports, err := b.Image()
	if err != nil {
		return nil, err
	}
	d, err := xbe.DisassembleKernel(img, exports)
	if err != nil {
		return nil, err
	}
	kernel, err := xbe.LiftMachineInto(d, xbe.KernelRoots(d, exports), pool)
	if err != nil {
		return nil, err
	}
	// The interrupt dispatch stub runs from copies the kernel patches, so its blocks read the patched words live
	template, err := xbe.DeriveInterruptTemplate(d, exports)
	if err != nil {
		return nil, err
	}
	if err := kernel.LiftTemplate(d, template); err != nil {
		return nil, err
	}
	if err := mm.Merge(kernel); err != nil {
		return nil, err
	}
	return &Lifted{Model: mm, Kernel: img, Exports: exports}, nil
}
