package cli

import (
	"bytes"
	"encoding/binary"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickheyer/discoxip/pkg/xip"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type cliResult struct {
	out    string
	errOut string
	err    error
}

// Returns every flag of the command tree to its default with its changed mark cleared
func resetFlags(c *cobra.Command) {
	for _, set := range []*pflag.FlagSet{c.Flags(), c.PersistentFlags()} {
		set.VisitAll(func(f *pflag.Flag) {
			f.Value.Set(f.DefValue)
			f.Changed = false
		})
	}
	for _, sub := range c.Commands() {
		resetFlags(sub)
	}
}

// Executes the root command on the arguments and captures what it writes to stdout and stderr
func runCLI(t *testing.T, args ...string) cliResult {
	t.Helper()
	resetFlags(rootCmd)
	t.Cleanup(func() { resetFlags(rootCmd) })
	oldOut, oldErr := os.Stdout, os.Stderr
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	done := make(chan struct{}, 2)
	go func() {
		io.Copy(&out, outR)
		done <- struct{}{}
	}()
	go func() {
		io.Copy(&errOut, errR)
		done <- struct{}{}
	}()
	os.Stdout, os.Stderr = outW, errW
	rootCmd.SetArgs(args)
	runErr := rootCmd.Execute()
	os.Stdout, os.Stderr = oldOut, oldErr
	outW.Close()
	errW.Close()
	<-done
	<-done
	outR.Close()
	errR.Close()
	return cliResult{out: out.String(), errOut: errOut.String(), err: runErr}
}

func writeFile(t *testing.T, path string, data []byte) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

type meshPatch struct {
	pool, indexStart, tris int
}

// Writes the entries as an archive and encodes the pool and index range into each named mesh record
func archiveBytes(t *testing.T, entries []xip.WriteEntry, meshes map[string]meshPatch) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := xip.NewWriter(&buf)
	for _, e := range entries {
		w.Add(e)
	}
	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}
	data := buf.Bytes()
	for name, m := range meshes {
		patchMeshEntry(t, data, name, m)
	}
	return data
}

func patchMeshEntry(t *testing.T, data []byte, name string, m meshPatch) {
	t.Helper()
	dataOffset := int(binary.LittleEndian.Uint32(data[4:]))
	numFiles := int(binary.LittleEndian.Uint16(data[8:]))
	numNames := int(binary.LittleEndian.Uint16(data[10:]))
	namesAt := 16 + numFiles*16
	block := data[namesAt+numNames*4 : dataOffset]
	for i := 0; i < numNames; i++ {
		rec := data[namesAt+i*4:]
		dataIndex := int(binary.LittleEndian.Uint16(rec))
		nameOff := int(binary.LittleEndian.Uint16(rec[2:]))
		end := bytes.IndexByte(block[nameOff:], 0)
		if string(block[nameOff:nameOff+end]) != strings.ReplaceAll(name, "/", `\`) {
			continue
		}
		fd := data[16+dataIndex*16:]
		binary.LittleEndian.PutUint32(fd, uint32(m.pool<<24|m.indexStart))
		binary.LittleEndian.PutUint32(fd[4:], uint32(m.tris))
		return
	}
	t.Fatalf("archive has no entry %q", name)
}

func entry(name string, typ xip.FileType, body []byte) xip.WriteEntry {
	return xip.WriteEntry{Name: name, Type: typ, Size: uint32(len(body)), Body: bytes.NewReader(body)}
}

func morton(x, y int) int {
	n := 0
	for b := 0; b < 8; b++ {
		n |= (x >> b & 1) << (2 * b)
		n |= (y >> b & 1) << (2*b + 1)
	}
	return n
}

func texturePixels(size int) []uint32 {
	px := make([]uint32, size*size)
	for i := range px {
		px[i] = 0xFF<<24 | uint32(i*16)<<16 | uint32(255-i*16)<<8 | uint32(i*7)
	}
	px[1] = 0x80<<24 | px[1]&0xFFFFFF
	return px
}

// Builds a square swizzled A8R8G8B8 XPR0 texture holding the row major ARGB pixels
func xprTexture(size int, argb []uint32) []byte {
	data := make([]byte, size*size*4)
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			px := argb[y*size+x]
			at := morton(x, y) * 4
			data[at], data[at+1], data[at+2], data[at+3] = byte(px), byte(px>>8), byte(px>>16), byte(px>>24)
		}
	}
	log2 := 0
	for 1<<log2 < size {
		log2++
	}
	formatReg := uint32(0x29) | 6<<8 | 1<<16 | uint32(log2)<<20 | uint32(log2)<<24
	out := []byte("XPR0")
	out = binary.LittleEndian.AppendUint32(out, uint32(32+len(data)))
	out = binary.LittleEndian.AppendUint32(out, 32)
	out = binary.LittleEndian.AppendUint16(out, 1)
	out = binary.LittleEndian.AppendUint16(out, 4)
	out = binary.LittleEndian.AppendUint32(out, 0)
	out = binary.LittleEndian.AppendUint32(out, 0)
	out = binary.LittleEndian.AppendUint32(out, formatReg)
	out = binary.LittleEndian.AppendUint32(out, 0)
	return append(out, data...)
}

func nrgbaPixels(argb []uint32) []byte {
	var out []byte
	for _, px := range argb {
		out = append(out, byte(px>>16), byte(px>>8), byte(px), byte(px>>24))
	}
	return out
}

func vbFile(count, format uint32, stride int) []byte {
	out := binary.LittleEndian.AppendUint32(nil, count)
	out = binary.LittleEndian.AppendUint32(out, format)
	for i := 0; i < int(count)*stride; i++ {
		out = append(out, byte(i))
	}
	return out
}

func ibFile(indices ...uint16) []byte {
	var out []byte
	for _, i := range indices {
		out = binary.LittleEndian.AppendUint16(out, i)
	}
	return out
}

type testGlyph struct {
	metrics [6]float32
	indices []uint16
	verts   [][2]float32
}

// Lays out an uncompressed XTF0 font with its glyph table followed by the meshes of the glyphs holding one
func xtfFont(name string, height int, ranges [][2]uint16, glyphs []testGlyph) []byte {
	out := []byte("XTF0")
	out = binary.LittleEndian.AppendUint32(out, uint32(height))
	nameField := make([]byte, 32)
	copy(nameField, name)
	out = append(out, nameField...)
	out = binary.LittleEndian.AppendUint32(out, uint32(16+4*len(ranges)))
	out = binary.LittleEndian.AppendUint32(out, 0)
	out = binary.LittleEndian.AppendUint32(out, uint32(len(glyphs)))
	out = binary.LittleEndian.AppendUint32(out, uint32(len(ranges)))
	for _, r := range ranges {
		out = binary.LittleEndian.AppendUint16(out, r[0])
		out = binary.LittleEndian.AppendUint16(out, r[1])
	}
	headerLen := len(out) + 28*len(glyphs)
	var meshes []byte
	offsets := make([]uint32, len(glyphs))
	for i, g := range glyphs {
		if len(g.indices) == 0 {
			continue
		}
		offsets[i] = uint32(headerLen + len(meshes))
		meshes = binary.LittleEndian.AppendUint16(meshes, uint16(len(g.indices)))
		meshes = binary.LittleEndian.AppendUint16(meshes, uint16(len(g.verts)))
		for _, idx := range g.indices {
			meshes = binary.LittleEndian.AppendUint16(meshes, idx)
		}
		for _, v := range g.verts {
			meshes = binary.LittleEndian.AppendUint32(meshes, math.Float32bits(v[0]))
			meshes = binary.LittleEndian.AppendUint32(meshes, math.Float32bits(v[1]))
		}
	}
	for i, g := range glyphs {
		for _, m := range g.metrics {
			out = binary.LittleEndian.AppendUint32(out, math.Float32bits(m))
		}
		out = binary.LittleEndian.AppendUint32(out, offsets[i])
	}
	return append(out, meshes...)
}

type xbeSection struct {
	name       string
	va, vsize  uint32
	raw, rsize uint32
	flags      uint32
}

// Lays out an executable of the size given with its section table and names right after the header
func xbeImage(base, entry, kthunk uint32, sections []xbeSection, size int) []byte {
	out := make([]byte, size)
	copy(out, "XBEH")
	put := func(off int, v uint32) { binary.LittleEndian.PutUint32(out[off:], v) }
	sectionsAt := 0x178
	nameOff := sectionsAt + 0x38*len(sections)
	for i, s := range sections {
		at := sectionsAt + 0x38*i
		put(at, s.flags)
		put(at+4, s.va)
		put(at+8, s.vsize)
		put(at+12, s.raw)
		put(at+16, s.rsize)
		put(at+20, base+uint32(nameOff))
		copy(out[nameOff:], s.name)
		nameOff += len(s.name) + 1
		for j := 0; j < int(s.rsize); j++ {
			out[int(s.raw)+j] = byte(j)
		}
	}
	put(0x104, base)
	put(0x108, uint32(nameOff))
	put(0x10C, uint32(size))
	put(0x110, 0x178)
	put(0x11C, uint32(len(sections)))
	put(0x120, base+uint32(sectionsAt))
	put(0x128, entry^0xA8FC57AB)
	put(0x158, kthunk^0x5B6D40B6)
	return out
}

// A zeroed flash bank whose 2BL carries a preldr jump when the revision asked for is 1
func flashBank(revision int) []byte {
	data := make([]byte, 0x40000)
	if revision == 1 {
		data[0x40000-0x200-0x2A00] = 0xE9
	}
	return data
}

const xapSceneText = `DEF Root Transform {
  children [
    Shape {
      appearance Appearance { material MaxMaterial { name "Chrome" } }
      geometry Mesh { url "box.xm" }
    }
    Shape {
      appearance Appearance { material MaxMaterial { name "Chrome" } }
      geometry Mesh { url "ball.xm" }
    }
  ]
}
USE Missing
`
