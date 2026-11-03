package native

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/nickheyer/discoxip/pkg/native/coff"
)

const KindObject = "object"

// Compiles or assembles the entry's source with the toolchain under wine and returns the object bytes
func BuildObject(e Entry, work string) ([]byte, error) {
	if e.CL == "" {
		return nil, fmt.Errorf("native: %s names no compiler", e.File)
	}
	if err := os.MkdirAll(work, 0o755); err != nil {
		return nil, err
	}
	out := filepath.Join(work, strings.NewReplacer("/", "_", "\\", "_", ":", "").Replace(strings.TrimSuffix(e.File, filepath.Ext(e.File)))+".obj")
	os.Remove(out)
	tool := e.CL
	if strings.EqualFold(filepath.Ext(e.File), ".asm") {
		tool = filepath.Join(filepath.Dir(e.CL), "ml.exe")
	}
	args := append([]string{tool, "/nologo", "/c"}, e.Flags...)
	args = append(args, "/Fo"+winPath(out), winPath(e.File))
	cmd := exec.Command("wine", args...)
	prefix := os.Getenv("WINEPREFIX")
	if prefix == "" {
		prefix = filepath.Join("build", "wine")
	}
	prefix, err := filepath.Abs(prefix)
	if err != nil {
		return nil, err
	}
	cmd.Env = append(os.Environ(), "WINEDEBUG=-all", "WINEPREFIX="+prefix)
	msg, err := cmd.CombinedOutput()
	text := strings.TrimSpace(strings.Join(filterLines(string(msg)), "\n"))
	if err != nil {
		return nil, fmt.Errorf("%s %s: %v\n%s", e.CL, strings.Join(args[1:], " "), err, text)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		return nil, fmt.Errorf("%s produced no object\n%s", e.CL, text)
	}
	return data, nil
}

// Drops the display noise wine prints around the compiler's own output
func filterLines(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if strings.Contains(l, "MESA") || strings.Contains(l, "pci id for fd") || strings.TrimSpace(l) == "" {
			continue
		}
		out = append(out, l)
	}
	return out
}

// Converts a path for a Windows program run under wine, absolute paths through the Z drive
func winPath(p string) string {
	p = filepath.ToSlash(p)
	if strings.HasPrefix(p, "/") {
		p = "Z:" + p
	}
	return strings.ReplaceAll(p, "/", "\\")
}

var archives = map[string][]coff.Member{}

// Reads the reference member out of the entry's library, archives parsed once per path
func referenceObject(e Entry) (*coff.Object, error) {
	if e.Member == "" {
		data, err := os.ReadFile(e.Reference)
		if err != nil {
			return nil, err
		}
		return coff.Parse(data)
	}
	members, ok := archives[e.Reference]
	if !ok {
		data, err := os.ReadFile(e.Reference)
		if err != nil {
			return nil, err
		}
		if members, err = coff.ReadArchive(data); err != nil {
			return nil, fmt.Errorf("%s: %v", e.Reference, err)
		}
		archives[e.Reference] = members
	}
	m, ok := coff.Find(members, e.Member)
	if !ok {
		return nil, fmt.Errorf("%s holds no member %s", e.Reference, e.Member)
	}
	return coff.Parse(m.Data)
}

// Comparison is how far a compiled object agrees with its reference
type Comparison struct {
	Matched int
	Total   int
	Unit    string
	Exact   bool
	Detail  string
}

// Compares the code and data of two objects function by function, then section by section
func CompareObjects(ref, out *coff.Object) Comparison {
	c := Comparison{Unit: "functions"}
	fns := ref.Functions()
	ours := map[string]coff.Function{}
	for _, f := range out.Functions() {
		ours[f.Name] = f
	}
	var details []string
	for _, f := range fns {
		c.Total++
		g, ok := ours[f.Name]
		if !ok {
			details = append(details, f.Name+" missing")
			continue
		}
		rb, rr := ref.Body(f)
		ob, or := out.Body(g)
		if d := diffBodies(rb, rr, ob, or); d != "" {
			details = append(details, f.Name+" "+d)
			continue
		}
		c.Matched++
	}
	if c.Total == 0 {
		c.Unit = "bytes"
		for _, s := range ref.Sections {
			if !s.Debug() {
				c.Total += len(s.Data)
			}
		}
		c.Matched = matchedBytes(ref, out)
	}
	if d := diffSections(ref, out); d != "" {
		details = append(details, d)
	} else if d := diffExternals(ref, out); d != "" {
		details = append(details, d)
	} else {
		c.Exact = true
	}
	c.Detail = strings.Join(details, "; ")
	return c
}

// Explains the first difference between two routine bodies, empty when they agree
func diffBodies(rb []byte, rr []coff.Reloc, ob []byte, or []coff.Reloc) string {
	if len(rb) != len(ob) {
		return fmt.Sprintf("is %d bytes, ours %d", len(rb), len(ob))
	}
	for i := range rb {
		if rb[i] != ob[i] && !relocAt(rr, uint32(i)) {
			return fmt.Sprintf("differs at byte %#x (%02x, ours %02x)", i, rb[i], ob[i])
		}
	}
	if len(rr) != len(or) {
		return fmt.Sprintf("has %d relocations, ours %d", len(rr), len(or))
	}
	for i := range rr {
		if rr[i] != or[i] {
			return fmt.Sprintf("relocates %s at %#x, ours %s at %#x", rr[i].Symbol, rr[i].Offset, or[i].Symbol, or[i].Offset)
		}
	}
	return ""
}

func relocAt(rs []coff.Reloc, off uint32) bool {
	for _, r := range rs {
		if off >= r.Offset && off < r.Offset+4 {
			return true
		}
	}
	return false
}

// Counts equal bytes across the non debug sections taken in order
func matchedBytes(ref, out *coff.Object) int {
	var a, b []coff.Section
	for _, s := range ref.Sections {
		if !s.Debug() {
			a = append(a, s)
		}
	}
	for _, s := range out.Sections {
		if !s.Debug() {
			b = append(b, s)
		}
	}
	n := 0
	for i := 0; i < len(a) && i < len(b); i++ {
		for j := 0; j < len(a[i].Data) && j < len(b[i].Data); j++ {
			if a[i].Data[j] == b[i].Data[j] || relocAt(a[i].Relocs, uint32(j)) && relocAt(b[i].Relocs, uint32(j)) {
				n++
			}
		}
	}
	return n
}

// Explains the first difference in the non debug sections, empty when they agree
func diffSections(ref, out *coff.Object) string {
	var a, b []coff.Section
	for _, s := range ref.Sections {
		if !s.Debug() {
			a = append(a, s)
		}
	}
	for _, s := range out.Sections {
		if !s.Debug() {
			b = append(b, s)
		}
	}
	if len(a) != len(b) {
		return fmt.Sprintf("reference has %d sections, ours %d", len(a), len(b))
	}
	for i := range a {
		switch {
		case a[i].Name != b[i].Name:
			return fmt.Sprintf("section %d is %s, ours %s", i, a[i].Name, b[i].Name)
		case a[i].Flags != b[i].Flags:
			return fmt.Sprintf("section %s has flags %#x, ours %#x", a[i].Name, a[i].Flags, b[i].Flags)
		case len(a[i].Data) != len(b[i].Data):
			return fmt.Sprintf("section %s is %d bytes, ours %d", a[i].Name, len(a[i].Data), len(b[i].Data))
		}
		if d := diffBodies(a[i].Data, a[i].Relocs, b[i].Data, b[i].Relocs); d != "" {
			return "section " + a[i].Name + " " + d
		}
	}
	return ""
}

// Explains the first difference between the external symbols each object defines
func diffExternals(ref, out *coff.Object) string {
	a, b := externals(ref), externals(out)
	keys := map[string]bool{}
	for k := range a {
		keys[k] = true
	}
	for k := range b {
		keys[k] = true
	}
	sorted := make([]string, 0, len(keys))
	for k := range keys {
		sorted = append(sorted, k)
	}
	sort.Strings(sorted)
	for _, k := range sorted {
		switch {
		case a[k] == "":
			return "ours defines " + k
		case b[k] == "":
			return "ours lacks " + k
		case a[k] != b[k]:
			return fmt.Sprintf("%s is at %s, ours %s", k, a[k], b[k])
		}
	}
	return ""
}

func externals(o *coff.Object) map[string]string {
	m := map[string]string{}
	for _, s := range o.Symbols {
		if s.Class == coff.ClassExternal && s.Section > 0 && int(s.Section) <= len(o.Sections) {
			m[s.Name] = fmt.Sprintf("%s+%#x", o.Sections[s.Section-1].Name, s.Value)
		}
	}
	return m
}

// Rebuilds a library object and compares it with the reference member
func verifyObject(e Entry, work string) Result {
	r := Result{Entry: e, Unit: "functions"}
	if _, err := os.Stat(e.File); err != nil {
		r.Error = "no source"
		return r
	}
	ref, err := referenceObject(e)
	if err != nil {
		r.Error = err.Error()
		return r
	}
	data, err := BuildObject(e, work)
	if err != nil {
		r.Error = err.Error()
		return r
	}
	out, err := coff.Parse(data)
	if err != nil {
		r.Error = err.Error()
		return r
	}
	if ref.Machine != out.Machine {
		r.Error = fmt.Sprintf("reference is machine %#x, ours %#x", ref.Machine, out.Machine)
		return r
	}
	c := CompareObjects(ref, out)
	r.Matched, r.Total, r.Unit, r.Exact, r.Detail = c.Matched, c.Total, c.Unit, c.Exact, c.Detail
	return r
}
