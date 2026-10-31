package xbe

import (
	"math/bits"
	"sort"
)

// Bytes a page of run memory holds and the shift from an address to its page
const (
	pageShift = 12
	pageBytes = 1 << pageShift
)

// Layers of block visits a run keeps before folding them into one
const visitLayers = 32

// memPage is one page of bytes a run wrote, shared between runs until one of them writes it again
type memPage struct {
	data    [pageBytes]byte
	written [pageBytes / 64]uint64
	holders int
}

// runMemory is every byte a run wrote, by page, a page shared with a clone copied before it is written
type runMemory struct {
	pages map[uint32]*memPage
}

func newRunMemory() runMemory {
	return runMemory{pages: map[uint32]*memPage{}}
}

// The byte written at an address and whether one was
func (m *runMemory) get(addr uint32) (byte, bool) {
	p := m.pages[addr>>pageShift]
	if p == nil {
		return 0, false
	}
	off := addr & (pageBytes - 1)
	if p.written[off/64]&(1<<(off%64)) == 0 {
		return 0, false
	}
	return p.data[off], true
}

// Writes a byte, copying a page another run holds first
func (m *runMemory) set(addr uint32, b byte) {
	idx := addr >> pageShift
	p := m.pages[idx]
	switch {
	case p == nil:
		p = &memPage{holders: 1}
		m.pages[idx] = p
	case p.holders > 1:
		p.holders--
		own := *p
		own.holders = 1
		p = &own
		m.pages[idx] = p
	}
	off := addr & (pageBytes - 1)
	p.data[off] = b
	p.written[off/64] |= 1 << (off % 64)
}

// A copy holding every page with this memory until either side writes it
func (m *runMemory) clone() runMemory {
	c := runMemory{pages: make(map[uint32]*memPage, len(m.pages))}
	for idx, p := range m.pages {
		p.holders++
		c.pages[idx] = p
	}
	return c
}

// Written bytes in all
func (m *runMemory) count() int {
	n := 0
	for _, p := range m.pages {
		for _, w := range p.written {
			n += bits.OnesCount64(w)
		}
	}
	return n
}

// Calls f with every written byte in address order
func (m *runMemory) each(f func(addr uint32, b byte)) {
	idxs := make([]uint32, 0, len(m.pages))
	for idx := range m.pages {
		idxs = append(idxs, idx)
	}
	sort.Slice(idxs, func(i, j int) bool { return idxs[i] < idxs[j] })
	for _, idx := range idxs {
		p := m.pages[idx]
		for off := uint32(0); off < pageBytes; off++ {
			if p.written[off/64]&(1<<(off%64)) != 0 {
				f(idx<<pageShift|off, p.data[off])
			}
		}
	}
}

// runVisits records the blocks a run entered, layers below the top shared with clones and never written again
type runVisits struct {
	layers []map[string]runVisit
}

func newRunVisits() runVisits {
	return runVisits{layers: []map[string]runVisit{{}}}
}

// The record of a block's last entry and whether it was entered
func (v *runVisits) get(key string) (runVisit, bool) {
	for i := len(v.layers) - 1; i >= 0; i-- {
		if last, ok := v.layers[i][key]; ok {
			return last, true
		}
	}
	return runVisit{}, false
}

// Records a block's entry in the top layer
func (v *runVisits) set(key string, last runVisit) {
	if len(v.layers) == 0 {
		v.layers = []map[string]runVisit{{}}
	}
	v.layers[len(v.layers)-1][key] = last
}

// A copy sharing every layer so far, both sides writing new layers from here on
func (v *runVisits) clone() runVisits {
	if len(v.layers) >= visitLayers {
		v.fold()
	}
	shared := append([]map[string]runVisit(nil), v.layers...)
	v.layers = append(shared, map[string]runVisit{})
	return runVisits{layers: append(append([]map[string]runVisit(nil), shared...), map[string]runVisit{})}
}

// Merges every layer into one, newer entries over older
func (v *runVisits) fold() {
	one := map[string]runVisit{}
	for _, layer := range v.layers {
		for key, last := range layer {
			one[key] = last
		}
	}
	v.layers = []map[string]runVisit{one}
}
