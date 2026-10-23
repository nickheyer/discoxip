package xbe

import (
	"encoding/binary"
	"fmt"
	"maps"
	"slices"

	"golang.org/x/arch/x86/x86asm"
)

// The CRT routine that calls every static initializer before main
const crtInit = "_cinit"

// The CRT routine a static initializer registers its destructor with
const crtAtExit = "atexit"

// Lists the static initializers in call order from the pointer tables the CRT init walks
func (d *Disassembly) staticInitializers() ([]uint32, error) {
	va := d.symbolVA(crtInit)
	if va == 0 {
		return nil, fmt.Errorf("xbe: the image names no %s routine", crtInit)
	}
	var bounds []uint32
	for _, in := range d.Functions[va].Instructions {
		if in.Inst.Op != x86asm.MOV || argCount(&in) != 2 {
			continue
		}
		if _, ok := in.Inst.Args[0].(x86asm.Reg); !ok {
			continue
		}
		imm, ok := in.Inst.Args[1].(x86asm.Imm)
		if sec := d.sectionOf(uint32(imm)); !ok || sec == nil || sec == d.sectionOf(d.Image.EntryPoint) {
			continue
		}
		bounds = append(bounds, uint32(imm))
	}
	if len(bounds) == 0 || len(bounds)%2 != 0 {
		return nil, fmt.Errorf("xbe: %s names %d table bounds, expected pairs", crtInit, len(bounds))
	}
	var out []uint32
	for i := 0; i < len(bounds); i += 2 {
		start, end := min(bounds[i], bounds[i+1]), max(bounds[i], bounds[i+1])
		if (end-start)%4 != 0 {
			return nil, fmt.Errorf("xbe: initializer table %08X..%08X is not a whole number of pointers", start, end)
		}
		for a := start; a < end; a += 4 {
			v, ok := d.Image.ReadU32(a)
			if !ok {
				return nil, fmt.Errorf("xbe: initializer table entry at %08X is outside the image", a)
			}
			if v == 0 || v == 0xFFFFFFFF {
				continue
			}
			if d.Functions[v] == nil {
				return nil, fmt.Errorf("xbe: initializer table entry %08X at %08X is no function", v, a)
			}
			out = append(out, v)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("xbe: %s walks no initializer", crtInit)
	}
	return out, nil
}

// Reports whether a function calls a target directly or through its own callees within a depth
func (d *Disassembly) callsWithin(va, target uint32, depth int) bool {
	for _, t := range d.plainCallees(va) {
		if t == target || (depth > 0 && d.callsWithin(t, target, depth-1)) {
			return true
		}
	}
	return false
}

// Replays the class registering static initializers into the startup memory the way the CRT init leaves it
func (rm *RenderModel) replayRegistrations(d *Disassembly, registrar uint32, types map[string]*NodeType) error {
	inits, err := d.staticInitializers()
	if err != nil {
		return err
	}
	ex := NewExecutor(d)
	for _, va := range inits {
		if !d.callsWithin(va, registrar, 1) {
			continue
		}
		paths, err := ex.Run(va, func(s *execState) {})
		if err != nil {
			return fmt.Errorf("xbe: static initializer %08X: %w", va, err)
		}
		if len(paths) != 1 {
			return fmt.Errorf("xbe: static initializer %08X has %d execution paths, expected one", va, len(paths))
		}
		for _, e := range paths[0].Effects {
			switch e.Kind {
			case "store":
				addr, ok := absoluteStore(e)
				if !ok {
					return fmt.Errorf("xbe: static initializer %08X stores through %s at %08X", va, ExprString(e.Addr), e.VA)
				}
				value, err := rm.startupValue(e.Value)
				if err != nil {
					return fmt.Errorf("xbe: static initializer %08X at %08X: %w", va, e.VA, err)
				}
				if err := rm.storeStartup(addr, e.Size, value); err != nil {
					return fmt.Errorf("xbe: static initializer %08X at %08X: %w", va, e.VA, err)
				}
			case "call":
				if e.Fn == crtAtExit {
					continue
				}
				return fmt.Errorf("xbe: static initializer %08X calls %s at %08X, which the registration replay does not run", va, e.Fn, e.VA)
			default:
				return fmt.Errorf("xbe: static initializer %08X has a %s effect at %08X, which the registration replay does not run", va, e.Kind, e.VA)
			}
		}
	}
	for _, name := range slices.Sorted(maps.Keys(types)) {
		word, err := rm.loadStartup(types[name].Desc, 4)
		if err != nil {
			return fmt.Errorf("xbe: node type %s: %w", name, err)
		}
		if word == 0 {
			return fmt.Errorf("xbe: no static initializer registers node type %s into its descriptor %08X", name, types[name].Desc)
		}
	}
	return nil
}

// The address a store writes when the path computed it as a constant
func absoluteStore(e Effect) (uint32, bool) {
	if e.Addr == nil {
		if e.Base != "" {
			return 0, false
		}
		return uint32(e.Off), true
	}
	return constAddr(e.Addr)
}

// A value a replayed store writes, a constant or a word of the startup memory as written so far
func (rm *RenderModel) startupValue(e Expr) (uint32, error) {
	if c, ok := constAddr(e); ok {
		return c, nil
	}
	if l, ok := e.(Load); ok && l.Base == "" && l.Addr == nil {
		return rm.loadStartup(uint32(l.Off), l.Size)
	}
	return 0, fmt.Errorf("the value %s is not fixed at startup", ExprString(e))
}

// The bytes of the startup memory a range lies in
func (rm *RenderModel) startupBytes(va uint32, size int) ([]byte, error) {
	for _, s := range rm.Memory.Sections {
		if va >= s.VA && va+uint32(size) <= s.VA+s.Size {
			off := s.Off + va - s.VA
			return rm.memory[off : off+uint32(size)], nil
		}
	}
	return nil, fmt.Errorf("%08X is outside the memory the image loads", va)
}

func (rm *RenderModel) loadStartup(va uint32, size int) (uint32, error) {
	b, err := rm.startupBytes(va, size)
	if err != nil {
		return 0, err
	}
	switch size {
	case 1:
		return uint32(b[0]), nil
	case 2:
		return uint32(binary.LittleEndian.Uint16(b)), nil
	case 4:
		return binary.LittleEndian.Uint32(b), nil
	}
	return 0, fmt.Errorf("a %d byte word at %08X has no startup value", size, va)
}

func (rm *RenderModel) storeStartup(va uint32, size int, value uint32) error {
	b, err := rm.startupBytes(va, size)
	if err != nil {
		return err
	}
	switch size {
	case 1:
		b[0] = byte(value)
	case 2:
		binary.LittleEndian.PutUint16(b, uint16(value))
	case 4:
		binary.LittleEndian.PutUint32(b, value)
	default:
		return fmt.Errorf("a %d byte store at %08X has no startup form", size, va)
	}
	return nil
}
