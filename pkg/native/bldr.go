package native

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/nickheyer/discoxip/pkg/bios"
)

// KindBootloader rebuilds a second bootloader and compares it with the plaintext image its flash bank encrypts
const KindBootloader = "bldr"

// bldrTarget is one bank's build description, the member of build.json the entry names
type bldrTarget struct {
	Flash     string     `json:"flash"`
	MCPX      string     `json:"mcpx"`
	Signature string     `json:"signature"`
	Nonce     string     `json:"nonce"`
	Header    string     `json:"header"`
	Reenter   string     `json:"reenter"`
	Bldr      bldrImage  `json:"bldr"`
	Preldr    *bldrImage `json:"preldr"`
}

// bldrImage is one linked flat image, its objects in link order and the constants its sources add to pointers
type bldrImage struct {
	Tools   string       `json:"tools"`
	Asm     string       `json:"asm"`
	Base    string       `json:"base"`
	CFlags  []string     `json:"cflags"`
	AFlags  []string     `json:"aflags"`
	Objects []bldrObject `json:"objects"`
	Link    []string     `json:"link"`
	Layout  []string     `json:"layout"`
	Biases  []string     `json:"biases"`
}

// Parses a hexadecimal address of the build description
func parseAddress(name, text string) (uint32, error) {
	v, err := strconv.ParseUint(strings.TrimPrefix(strings.ToLower(text), "0x"), 16, 32)
	if err != nil {
		return 0, fmt.Errorf("%s: %q is no address", name, text)
	}
	return uint32(v), nil
}

// bldrObject is one source of an image with the flags added to the image's own
type bldrObject struct {
	File  string   `json:"file"`
	Flags []string `json:"flags"`
	Tools string   `json:"tools"`
}

// Reads the target the entry names out of its build description
func loadBldrTarget(path, name string) (*bldrTarget, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var all map[string]*bldrTarget
	if err := json.Unmarshal(data, &all); err != nil {
		return nil, fmt.Errorf("%s: %v", path, err)
	}
	t, ok := all[name]
	if !ok {
		return nil, fmt.Errorf("%s describes no target %q", path, name)
	}
	return t, nil
}

// Finds a tool in a toolchain directory whatever the case of its file name
func findTool(toolchain, name string) (string, error) {
	dir := filepath.Join("references", "toolchains", toolchain)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	for _, e := range entries {
		if strings.EqualFold(e.Name(), name) {
			return filepath.Join(dir, e.Name()), nil
		}
	}
	return "", fmt.Errorf("%s holds no %s", dir, name)
}

// Runs a Windows tool under wine and returns its output with the display noise dropped
func runWine(args ...string) (string, error) {
	cmd := exec.Command("wine", args...)
	prefix := os.Getenv("WINEPREFIX")
	if prefix == "" {
		prefix = filepath.Join("build", "wine")
	}
	prefix, err := filepath.Abs(prefix)
	if err != nil {
		return "", err
	}
	cmd.Env = append(os.Environ(), "WINEDEBUG=-all", "WINEPREFIX="+prefix)
	msg, err := cmd.CombinedOutput()
	text := strings.TrimSpace(strings.Join(filterLines(string(msg)), "\n"))
	if err != nil {
		return text, fmt.Errorf("%s: %v\n%s", strings.Join(args, " "), err, text)
	}
	return text, nil
}

// Compiles or assembles one source of an image into the work directory
func buildBldrObject(img bldrImage, o bldrObject, work string) (string, error) {
	out := filepath.Join(work, strings.NewReplacer("/", "_", "\\", "_", ":", "").Replace(o.File)+".obj")
	os.Remove(out)
	tools := img.Tools
	if o.Tools != "" {
		tools = o.Tools
	}
	var args []string
	if strings.EqualFold(filepath.Ext(o.File), ".asm") {
		if img.Asm != "" {
			tools = img.Asm
		}
		ml, err := findTool(tools, "ml.exe")
		if err != nil {
			return "", err
		}
		args = append([]string{ml, "/nologo", "/c"}, img.AFlags...)
	} else {
		cl, err := findTool(tools, "cl.exe")
		if err != nil {
			return "", err
		}
		args = append([]string{cl, "/nologo", "/c"}, img.CFlags...)
	}
	args = append(args, o.Flags...)
	args = append(args, "/Fo"+winPath(out), winPath(o.File))
	if _, err := runWine(args...); err != nil {
		return "", err
	}
	if _, err := os.Stat(out); err != nil {
		return "", fmt.Errorf("%s produced no object", o.File)
	}
	return out, nil
}

// flatImage is a linked image laid flat, its symbols and relocation sites at their packed offsets
type flatImage struct {
	Data   []byte
	Entry  uint32
	Base   uint32
	Syms   map[string]uint32
	Relocs []uint32
}

// Links an image's objects and lays its sections flat from the first one
func buildFlat(img bldrImage, work, name string) (*flatImage, error) {
	if err := os.MkdirAll(work, 0o755); err != nil {
		return nil, err
	}
	var objs []string
	for _, o := range img.Objects {
		obj, err := buildBldrObject(img, o, work)
		if err != nil {
			return nil, err
		}
		objs = append(objs, obj)
	}
	link, err := findTool(img.Tools, "link.exe")
	if err != nil {
		return nil, err
	}
	exe := filepath.Join(work, name+".exe")
	mapFile := filepath.Join(work, name+".map")
	os.Remove(exe)
	args := append([]string{link, "/nologo"}, img.Link...)
	args = append(args, "/OUT:"+winPath(exe), "/MAP:"+winPath(mapFile))
	for _, o := range objs {
		args = append(args, winPath(o))
	}
	if _, err := runWine(args...); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(exe)
	if err != nil {
		return nil, fmt.Errorf("%s: the linker produced no image", name)
	}
	pe, err := bios.ParsePE(data)
	if err != nil {
		return nil, err
	}
	base, err := parseAddress(name, img.Base)
	if err != nil {
		return nil, err
	}
	biases := []uint32{0}
	for _, b := range img.Biases {
		v, err := parseAddress(name, b)
		if err != nil {
			return nil, err
		}
		biases = append(biases, v)
	}
	parts, syms, err := readMap(mapFile)
	if err != nil {
		return nil, err
	}
	flat, relocs, place, err := packSections(pe, data, parts, img.Layout, base, biases)
	if err != nil {
		return nil, fmt.Errorf("%s: %v", name, err)
	}
	entry, ok := place(pe.ImageBase + pe.EntryPoint)
	if !ok {
		return nil, fmt.Errorf("%s: the entry point %#x lies in no packed section", name, pe.ImageBase+pe.EntryPoint)
	}
	for k, v := range syms {
		off, ok := place(v)
		if !ok {
			delete(syms, k)
			continue
		}
		syms[k] = base + off
	}
	return &flatImage{Data: flat, Entry: entry, Base: base, Syms: syms, Relocs: relocs}, nil
}

// mapPart is one section of the linker map, held inside the numbered section of the image
type mapPart struct {
	Group  int
	Start  uint32
	Length uint32
	Name   string
}

// Packs the map's sections back to back at four byte alignment in the order the layout names, fixing every relocation up to the packed addresses
func packSections(pe *bios.PE, data []byte, parts []mapPart, layout []string, base uint32, biases []uint32) ([]byte, []uint32, func(uint32) (uint32, bool), error) {
	if len(layout) == 0 {
		layout = []string{".rdata", ".text", ".data", ".bss"}
	}
	type placed struct {
		sec bios.PESection
		va  uint32
		len uint32
		off uint32
	}
	var order []placed
	seen := map[string]bool{}
	size := uint32(0)
	for _, name := range layout {
		for _, p := range parts {
			if p.Name != name {
				continue
			}
			if p.Group < 1 || p.Group > len(pe.Sections) {
				return nil, nil, nil, fmt.Errorf("the map places %s in section %d, which the image lacks", p.Name, p.Group)
			}
			seen[name] = true
			sec := pe.Sections[p.Group-1]
			size = (size + 3) &^ 3
			order = append(order, placed{sec, pe.ImageBase + sec.VirtualAddress + p.Start, p.Length, size})
			size += p.Length
		}
	}
	for _, p := range parts {
		if !seen[p.Name] && p.Length != 0 {
			return nil, nil, nil, fmt.Errorf("section %s is not placed by the layout", p.Name)
		}
	}
	flat := make([]byte, size)
	for _, p := range order {
		from := int(p.sec.RawOffset) + int(p.va-pe.ImageBase-p.sec.VirtualAddress)
		n := min(int(p.len), int(p.sec.RawOffset)+int(p.sec.RawSize)-from, len(data)-from)
		if n > 0 {
			copy(flat[p.off:], data[from:from+n])
		}
	}
	// Maps an address of the linked image to its offset in the packed one
	place := func(va uint32) (uint32, bool) {
		for _, p := range order {
			if va >= p.va && va < p.va+p.len {
				return p.off + va - p.va, true
			}
		}
		return 0, false
	}
	var relocs []uint32
	for _, s := range pe.Sections {
		if s.Name != ".reloc" {
			continue
		}
		rel := data[s.RawOffset : s.RawOffset+s.RawSize]
		for len(rel) >= 8 {
			page := binary.LittleEndian.Uint32(rel)
			block := binary.LittleEndian.Uint32(rel[4:])
			if block < 8 || int(block) > len(rel) {
				break
			}
			for _, e := range splitWords(rel[8:block]) {
				if e>>12 != 3 {
					continue
				}
				off, ok := place(pe.ImageBase + page + uint32(e&0xfff))
				if !ok {
					return nil, nil, nil, fmt.Errorf("relocation at %#x lies in no packed section", page+uint32(e&0xfff))
				}
				relocs = append(relocs, off)
				value := binary.LittleEndian.Uint32(flat[off:])
				placed := false
				// A pointer the source offset by a constant keeps that constant over its packed address
				for _, bias := range biases {
					if target, ok := place(value - bias); ok {
						binary.LittleEndian.PutUint32(flat[off:], bias+base+target)
						placed = true
						break
					}
				}
				if !placed {
					return nil, nil, nil, fmt.Errorf("relocation at %#x points at %#x, outside every packed section", page+uint32(e&0xfff), value)
				}
			}
			rel = rel[block:]
		}
	}
	return flat, relocs, place, nil
}

// Splits a relocation block body into its entries
func splitWords(b []byte) []uint16 {
	out := make([]uint16, 0, len(b)/2)
	for i := 0; i+1 < len(b); i += 2 {
		out = append(out, binary.LittleEndian.Uint16(b[i:]))
	}
	return out
}

// Reads the sections of a linker map file and its public symbols by the address each landed at
func readMap(path string) ([]mapPart, map[string]uint32, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	var parts []mapPart
	syms := map[string]uint32{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 3 || len(fields[0]) != 13 || fields[0][4] != ':' {
			continue
		}
		group, err := strconv.ParseUint(fields[0][:4], 16, 16)
		if err != nil {
			continue
		}
		start, err := strconv.ParseUint(fields[0][5:], 16, 32)
		if err != nil {
			continue
		}
		if len(fields) == 4 && strings.HasSuffix(fields[1], "H") {
			length, err := strconv.ParseUint(strings.TrimSuffix(fields[1], "H"), 16, 32)
			if err != nil {
				continue
			}
			parts = append(parts, mapPart{int(group), uint32(start), uint32(length), fields[2]})
			continue
		}
		if len(fields[2]) != 8 {
			continue
		}
		v, err := strconv.ParseUint(fields[2], 16, 32)
		if err != nil {
			continue
		}
		syms[fields[1]] = uint32(v)
	}
	return parts, syms, sc.Err()
}

// Reads a data file the build takes as an input, empty when none is named
func optionalFile(path string) ([]byte, error) {
	if path == "" {
		return nil, nil
	}
	return os.ReadFile(path)
}

// Rebuilds the bootloader the entry names and compares the composed plaintext image with the reference byte for byte
func verifyBootloader(e Entry, work string) Result {
	r := Result{Entry: e, Unit: "bytes"}
	ref, err := os.ReadFile(e.Reference)
	if err != nil {
		r.Error = err.Error()
		return r
	}
	r.Total = len(ref)
	t, err := loadBldrTarget(e.File, e.Member)
	if err != nil {
		r.Error = err.Error()
		return r
	}
	for _, o := range t.Bldr.Objects {
		if _, err := os.Stat(o.File); err != nil {
			r.Error = "no source"
			return r
		}
	}
	bank, err := bios.Open(t.Flash)
	if err == nil {
		var mcpx *bios.MCPX
		if mcpx, err = bios.LoadMCPX(t.MCPX); err == nil {
			err = bank.Decrypt(mcpx)
		}
	}
	if err != nil {
		r.Error = err.Error()
		return r
	}
	dir := filepath.Join(work, e.Member)
	bldr, err := buildFlat(t.Bldr, dir, "bldr")
	if err != nil {
		r.Error = err.Error()
		return r
	}
	var preldr []byte
	var fbl *flatImage
	if t.Preldr != nil {
		if fbl, err = buildFlat(*t.Preldr, dir, "preldr"); err != nil {
			r.Error = err.Error()
			return r
		}
		preldr = fbl.Data
	}
	signature, err := optionalFile(t.Signature)
	if err != nil {
		r.Error = err.Error()
		return r
	}
	nonce, err := optionalFile(t.Nonce)
	if err != nil {
		r.Error = err.Error()
		return r
	}
	header, err := optionalFile(t.Header)
	if err != nil {
		r.Error = err.Error()
		return r
	}
	var reenter uint32
	if t.Reenter != "" {
		v, ok := bldr.Syms[t.Reenter]
		if !ok {
			r.Error = fmt.Sprintf("the map names no symbol %s", t.Reenter)
			return r
		}
		reenter = v
	}
	img, err := bank.Compose(bldr.Data, preldr, signature, nonce, header, bldr.Entry, reenter)
	if err != nil {
		r.Error = err.Error()
		return r
	}
	if err := os.WriteFile(filepath.Join(dir, "2bl.bin"), img, 0o644); err != nil {
		r.Error = err.Error()
		return r
	}
	r.Matched, r.Exact = Compare(img, ref)
	if r.Exact {
		return r
	}
	routines := compareRoutines(ref, img, 0, bldr)
	if fbl != nil {
		routines = append(routines, compareRoutines(ref, img, bios.LayoutFor(bank.MCPXRevision()).PreldrAt, fbl)...)
	}
	var lines []string
	matched := 0
	for _, rt := range routines {
		if rt.Detail == "" {
			matched++
			lines = append(lines, fmt.Sprintf("%06x %5d  %s", rt.Offset, rt.Size, rt.Name))
			continue
		}
		if r.Detail == "" {
			r.Detail = fmt.Sprintf("%s %s", rt.Name, rt.Detail)
		}
		lines = append(lines, fmt.Sprintf("%06x %5d  %s %s", rt.Offset, rt.Size, rt.Name, rt.Detail))
	}
	r.Detail = fmt.Sprintf("%d of %d routines match, %s", matched, len(routines), r.Detail)
	if err := os.WriteFile(filepath.Join(dir, "routines.txt"), []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		r.Error = err.Error()
	}
	return r
}

// Names the shift at which the span agrees most with the reference and how many of its bytes agree there
func bestFit(ref, img []byte, off, size int, masked map[int]bool) string {
	best, at := -1, 0
	for p := max(0, off-0x400); p+size <= len(ref) && p <= off+0x400; p++ {
		n := 0
		for j := 0; j < size && off+j < len(img); j++ {
			if masked[off+j] || ref[p+j] == img[off+j] {
				n++
			}
		}
		if n > best {
			best, at = n, p
		}
	}
	return fmt.Sprintf(", best fit at %+d with %d of %d bytes", at-off, best, size)
}

// routine is one symbol's span of the composed image and how it compares with the reference
type routine struct {
	Name   string
	Offset int
	Size   int
	Detail string
}

// Compares the image's symbols span by span, the bytes under a relocation taken as equal
func compareRoutines(ref, img []byte, at int, f *flatImage) []routine {
	type sym struct {
		name string
		off  int
	}
	var syms []sym
	for name, addr := range f.Syms {
		syms = append(syms, sym{name, at + int(addr-f.Base)})
	}
	sort.Slice(syms, func(i, j int) bool {
		if syms[i].off != syms[j].off {
			return syms[i].off < syms[j].off
		}
		return syms[i].name < syms[j].name
	})
	masked := map[int]bool{}
	for _, off := range f.Relocs {
		for i := 0; i < 4; i++ {
			masked[at+int(off)+i] = true
		}
	}
	var out []routine
	for i, s := range syms {
		end := at + len(f.Data)
		if i+1 < len(syms) {
			end = syms[i+1].off
		}
		rt := routine{Name: s.name, Offset: s.off, Size: end - s.off}
		for j := s.off; j < end && j < len(ref) && j < len(img); j++ {
			if ref[j] != img[j] && !masked[j] {
				rt.Detail = fmt.Sprintf("differs at +%#x (%02x, ours %02x)", j-s.off, ref[j], img[j])
				break
			}
		}
		if rt.Detail != "" && rt.Size > 0 {
			rt.Detail += bestFit(ref, img, s.off, rt.Size, masked)
		}
		out = append(out, rt)
	}
	return out
}
