package xbe

import (
	"encoding/binary"
	"fmt"
	"math"
	"testing"
	"unicode/utf16"
)

const (
	testBase   = 0x00010000
	testText   = 0x00011000
	testRData  = 0x00020000
	testData   = 0x00021000
	kernelBase = 0x80010000
	kernelText = 0x80011000
	kernelData = 0x80020000
	testBuild  = 5849
)

// One labeled run of bytes whose trailing word may name a label, absolute or relative to its end
type chunk struct {
	label  string
	bytes  []byte
	ref    string
	width  int
	rel    bool
	addend int32
	tail   []byte
}

func lbl(name string) chunk { return chunk{label: name} }

func by(b ...byte) chunk { return chunk{bytes: b} }

func rep(n int, b ...byte) chunk {
	var out []byte
	for i := 0; i < n; i++ {
		out = append(out, b...)
	}
	return chunk{bytes: out}
}

func rel8(op byte, label string) chunk {
	return chunk{bytes: []byte{op, 0}, ref: label, width: 1, rel: true}
}

func rel32(ops []byte, label string) chunk {
	return chunk{bytes: append(append([]byte{}, ops...), 0, 0, 0, 0), ref: label, width: 4, rel: true}
}

func abs32(ops []byte, label string, addend int32, tail ...byte) chunk {
	return chunk{bytes: append(append([]byte{}, ops...), 0, 0, 0, 0), ref: label, width: 4, addend: addend, tail: tail}
}

func word(v uint32) chunk {
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], v)
	return chunk{bytes: b[:]}
}

func wordAt(label string, addend int32) chunk {
	return chunk{bytes: []byte{0, 0, 0, 0}, ref: label, width: 4, addend: addend}
}

func f32(v float32) chunk { return word(math.Float32bits(v)) }

func wide(s string) chunk {
	var out []byte
	for _, u := range utf16.Encode([]rune(s)) {
		out = append(out, byte(u), byte(u>>8))
	}
	return chunk{bytes: append(out, 0, 0)}
}

func call(label string) chunk  { return rel32([]byte{0xE8}, label) }
func jmp8(label string) chunk  { return rel8(0xEB, label) }
func je8(label string) chunk   { return rel8(0x74, label) }
func jne8(label string) chunk  { return rel8(0x75, label) }
func ja8(label string) chunk   { return rel8(0x77, label) }
func ret() chunk               { return by(0xC3) }
func retn(n uint16) chunk      { return by(0xC2, byte(n), byte(n>>8)) }
func pad() chunk               { return by(0xCC) }
func movEAXImm(v uint32) chunk { return chunk{bytes: append([]byte{0xB8}, le32(v)...)} }
func movECXImm(v uint32) chunk { return chunk{bytes: append([]byte{0xB9}, le32(v)...)} }
func movEDXImm(v uint32) chunk { return chunk{bytes: append([]byte{0xBA}, le32(v)...)} }
func movEBXImm(v uint32) chunk { return chunk{bytes: append([]byte{0xBB}, le32(v)...)} }

func le32(v uint32) []byte {
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], v)
	return b[:]
}

// One section of a hand built image, its chunks laid out from its base
type codeSection struct {
	name   string
	base   uint32
	flags  uint32
	chunks []chunk
}

// Lays every section out, labels shared across them, and fixes up each reference
func layoutSections(t *testing.T, secs []codeSection) (map[string]uint32, []Section) {
	t.Helper()
	labels := map[string]uint32{}
	for _, sec := range secs {
		pos := sec.base
		for _, c := range sec.chunks {
			if c.label != "" {
				if _, dup := labels[c.label]; dup {
					t.Fatalf("label %s defined twice", c.label)
				}
				labels[c.label] = pos
			}
			pos += uint32(len(c.bytes) + len(c.tail))
		}
	}
	var out []Section
	for _, sec := range secs {
		var data []byte
		pos := sec.base
		for _, c := range sec.chunks {
			b := append([]byte{}, c.bytes...)
			if c.ref != "" {
				target, ok := labels[c.ref]
				if !ok {
					t.Fatalf("label %s is not defined", c.ref)
				}
				value := target + uint32(c.addend)
				if c.rel {
					value -= pos + uint32(len(b)+len(c.tail))
				}
				at := len(b) - c.width
				if c.width == 1 {
					if int32(value) < -128 || int32(value) > 127 {
						t.Fatalf("label %s is %d bytes away from a short reference", c.ref, int32(value))
					}
					b[at] = byte(value)
				} else {
					binary.LittleEndian.PutUint32(b[at:], value)
				}
			}
			b = append(b, c.tail...)
			data = append(data, b...)
			pos += uint32(len(b))
		}
		if len(data) == 0 {
			data = []byte{0, 0, 0, 0}
		}
		out = append(out, Section{Name: sec.name, VirtualAddr: sec.base, VirtualSize: uint32(len(data)), RawAddr: sec.base - secs[0].base, RawSize: uint32(len(data)), Flags: sec.flags, Data: data})
	}
	return labels, out
}

// A hand built image with its disassembly and the address of every label
type testImage struct {
	img     *Image
	d       *Disassembly
	labels  map[string]uint32
	thunk   map[string]uint32
	exports map[uint32]uint32
}

func (ti *testImage) at(t *testing.T, label string) uint32 {
	t.Helper()
	va, ok := ti.labels[label]
	if !ok {
		t.Fatalf("label %s is not defined", label)
	}
	return va
}

func kernelOrdinal(t *testing.T, name string) uint32 {
	t.Helper()
	for i, n := range kernelExports {
		if n == name {
			return uint32(i)
		}
	}
	t.Fatalf("%s is no kernel export", name)
	return 0
}

// Builds an executable text, a read only data and a writable data section holding a kernel thunk table, then disassembles
func buildImage(t *testing.T, text, rdata, data []chunk, imports []string) *testImage {
	t.Helper()
	thunks := []chunk{lbl("thunks")}
	for _, name := range imports {
		thunks = append(thunks, lbl("thunk."+name), word(kernelOrdinal(t, name)|0x80000000))
	}
	thunks = append(thunks, word(0))
	labels, secs := layoutSections(t, []codeSection{
		{".text", testText, 0x4, text},
		{".rdata", testRData, 0, rdata},
		{".data", testData, 0x1, append(thunks, data...)},
	})
	img := NewImage(testBase, testText, nil, secs)
	img.KernThunk = labels["thunks"]
	img.Libs = []LibVersion{{Name: "D3D8", Major: 1, Build: testBuild}}
	d, err := Disassemble(img)
	if err != nil {
		t.Fatalf("disassemble: %v", err)
	}
	for va, fn := range d.Functions {
		if fn.Symbol != nil && fn.Symbol.Lib != "usage" {
			t.Fatalf("hand built routine %08X was identified as %s", va, fn.Symbol.Name)
		}
	}
	ti := &testImage{img: img, d: d, labels: labels, thunk: map[string]uint32{}}
	for _, name := range imports {
		ti.thunk[name] = labels["thunk."+name]
	}
	return ti
}

// Builds a kernel style image in the identity mapped range and sweeps it with its exports as boundaries
func buildKernelImage(t *testing.T, text, data []chunk, exports map[uint32]string) *testImage {
	t.Helper()
	labels, secs := layoutSections(t, []codeSection{
		{".text", kernelText, 0x4, text},
		{".data", kernelData, 0x1, data},
	})
	img := NewImage(kernelBase, kernelText, nil, secs)
	exportVAs := map[uint32]uint32{}
	for ordinal, label := range exports {
		va, ok := labels[label]
		if !ok {
			t.Fatalf("label %s is not defined", label)
		}
		exportVAs[ordinal] = va
	}
	d, err := DisassembleKernel(img, exportVAs)
	if err != nil {
		t.Fatalf("disassemble kernel: %v", err)
	}
	return &testImage{img: img, d: d, labels: labels, thunk: map[string]uint32{}, exports: exportVAs}
}

func noInit(*execState) {}

// Runs a routine symbolically and fails the test on any error
func runPaths(t *testing.T, ti *testImage, label string, init func(*execState)) []Path {
	t.Helper()
	paths, err := NewExecutor(ti.d).Run(ti.at(t, label), init)
	if err != nil {
		t.Fatalf("run %s: %v", label, err)
	}
	return paths
}

func liftRoutine(t *testing.T, ti *testImage, label string) *LiftedFunction {
	t.Helper()
	lf, err := NewExecutor(ti.d).Lift(ti.at(t, label))
	if err != nil {
		t.Fatalf("lift %s: %v", label, err)
	}
	return lf
}

func machineModel(t *testing.T, ti *testImage, roots ...string) *MachineModel {
	t.Helper()
	var vas []uint32
	for _, label := range roots {
		vas = append(vas, ti.at(t, label))
	}
	mm, err := LiftMachineRooted(ti.d, vas, NewExprPool())
	if err != nil {
		t.Fatalf("lift machine: %v", err)
	}
	return mm
}

func blockOf(t *testing.T, mm *MachineModel, va uint32) *liftedBlock {
	t.Helper()
	b := mm.blocks[blockKey(va, nil)]
	if b == nil {
		t.Fatalf("no block %08X among %v", va, keysOf(mm.blocks))
	}
	return b
}

func keysOf(m map[string]*liftedBlock) []string {
	return sortedKeys(func() map[string]bool {
		out := map[string]bool{}
		for k := range m {
			out[k] = true
		}
		return out
	}())
}

func exprEq(t *testing.T, what string, got, want Expr) {
	t.Helper()
	if !equalExprs(got, want) {
		t.Fatalf("%s = %s, want %s", what, ExprString(got), ExprString(want))
	}
}

func effectsOf(p Path, kind string) []Effect {
	var out []Effect
	for _, e := range p.Effects {
		if e.Kind == kind {
			out = append(out, e)
		}
	}
	return out
}

func callsNamed(effects []Effect, fn string) []Effect {
	var out []Effect
	for _, e := range effects {
		if e.Kind == "call" && e.Fn == fn {
			out = append(out, e)
		}
	}
	return out
}

func onePath(t *testing.T, paths []Path) Path {
	t.Helper()
	if len(paths) != 1 {
		t.Fatalf("%d paths, want 1", len(paths))
	}
	return paths[0]
}

func subName(va uint32) string { return fmt.Sprintf("sub_%08X", va) }
