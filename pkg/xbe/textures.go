package xbe

import (
	"fmt"
	"sort"
)

// ProducedTexture is a texture the dashboard generates procedurally at runtime.
type ProducedTexture struct {
	VA     uint32   `json:"va"`
	Width  int      `json:"width"`
	Height int      `json:"height"`
	Format uint32   `json:"format"`
	Pixels []uint32 `json:"-"`
}

// DeriveTextures executes each texture producer concretely and captures the pixels it writes.
func DeriveTextures(d *Disassembly, producers []uint32, allocators []uint32) ([]*ProducedTexture, error) {
	var out []*ProducedTexture
	for _, va := range producers {
		ex := NewExecutor(d)
		for _, a := range allocators {
			ex.Allocators[a] = true
			ex.Opaque[a] = true
		}
		ex.StepLimit = 50_000_000
		ex.FrameLimit = -1
		count := 0
		ex.allocHook = func() Expr {
			count++
			return Ptr{fmt.Sprintf("buf%d", count), 0}
		}
		paths, err := ex.Run(va, func(s *execState) {})
		if err != nil {
			return nil, fmt.Errorf("texture producer %08X: %w", va, err)
		}
		var tex *ProducedTexture
		for _, p := range paths {
			t, err := textureFromPath(p)
			if err != nil {
				return nil, fmt.Errorf("texture producer %08X: %w", va, err)
			}
			if t != nil {
				tex = t
				break
			}
		}
		if tex == nil {
			return nil, fmt.Errorf("texture producer %08X never fills a texture on any path", va)
		}
		tex.VA = va
		out = append(out, tex)
	}
	return out, nil
}

// Reads the created texture's dimensions and the pixel buffer written on a path
func textureFromPath(p Path) (*ProducedTexture, error) {
	var tex *ProducedTexture
	buffers := map[string]map[int32]uint32{}
	for _, e := range p.Effects {
		switch {
		case e.Kind == "call" && (e.Fn == "D3DDevice_CreateTexture" || e.Fn == "D3DXCreateTexture"):
			// D3DX takes the device first, so its size and format arguments sit one slot later.
			base := 0
			if e.Fn == "D3DXCreateTexture" {
				base = 1
			}
			if len(e.Args) < base+5 {
				return nil, fmt.Errorf("%s with %d arguments", e.Fn, len(e.Args))
			}
			w, ok1 := isConst(e.Args[base])
			h, ok2 := isConst(e.Args[base+1])
			f, ok3 := isConst(e.Args[base+4])
			if !ok1 || !ok2 || !ok3 {
				return nil, fmt.Errorf("%s with symbolic size or format", e.Fn)
			}
			tex = &ProducedTexture{Width: int(w), Height: int(h), Format: f}
		case e.Kind == "store" && len(e.Base) > 3 && (e.Base[:3] == "buf" || e.Base[:3] == "res") && e.Size == 4:
			v, ok := isConst(e.Value)
			if !ok {
				if f, isF := e.Value.(FConst); isF {
					v = uint32(f.V)
				} else {
					return nil, fmt.Errorf("pixel store at %s+%X is symbolic: %s", e.Base, e.Off, ExprString(e.Value))
				}
			}
			if buffers[e.Base] == nil {
				buffers[e.Base] = map[int32]uint32{}
			}
			buffers[e.Base][e.Off] = v
		}
	}
	if tex == nil {
		return nil, nil
	}
	n := tex.Width * tex.Height
	var best map[int32]uint32
	for _, b := range buffers {
		if len(b) == n && (best == nil || len(b) > len(best)) {
			best = b
		}
	}
	if best == nil {
		sizes := []int{}
		for _, b := range buffers {
			sizes = append(sizes, len(b))
		}
		sort.Ints(sizes)
		return nil, fmt.Errorf("no buffer holds %d pixels (buffers %v)", n, sizes)
	}
	tex.Pixels = make([]uint32, n)
	for off, v := range best {
		idx := int(off) / 4
		if off%4 != 0 || idx < 0 || idx >= n {
			return nil, fmt.Errorf("pixel store at offset %X outside the texture", off)
		}
		tex.Pixels[idx] = v
	}
	return tex, nil
}
