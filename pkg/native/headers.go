package native

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/nickheyer/discoxip/pkg/native/coff"
	"github.com/nickheyer/discoxip/pkg/native/cv"
	"github.com/nickheyer/discoxip/pkg/native/pdb"
)

// Definition is one aggregate type rendered from debugging records
type Definition struct {
	Name     string
	Kind     string
	Size     uint64
	Body     string
	Aliases  []string
	Pointers []string
	Needs    []string
	Source   string
}

// Universe collects every type, typedef, routine and global the inputs describe
type Universe struct {
	Defs      map[string]*Definition
	Typedefs  map[string]string
	Routines  map[string]string
	Globals   map[string]string
	Conflicts []string
	order     []string
}

// NewUniverse makes an empty collection
func NewUniverse() *Universe {
	return &Universe{Defs: map[string]*Definition{}, Typedefs: map[string]string{}, Routines: map[string]string{}, Globals: map[string]string{}}
}

// AddDatabase takes every module of a kernel program database
func (u *Universe) AddDatabase(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	f, err := pdb.Open(data)
	if err != nil {
		return err
	}
	types, err := f.Types()
	if err != nil {
		return err
	}
	var mods []*cv.Module
	for _, m := range f.Modules {
		mod, err := f.Symbols(m)
		if err != nil {
			return err
		}
		mods = append(mods, mod)
	}
	u.add(types, mods, filepath.Base(path))
	return nil
}

// AddLibrary takes every object of a library archive
func (u *Universe) AddLibrary(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	members, err := coff.ReadArchive(data)
	if err != nil {
		return err
	}
	for _, m := range members {
		obj, err := coff.Parse(m.Data)
		if err != nil {
			continue
		}
		mod := cv.NewModule()
		var types *cv.Types
		for _, s := range obj.Sections {
			switch s.Name {
			case ".debug$S":
				if err := mod.AddSection(s.Data); err != nil {
					return fmt.Errorf("%s %s: %v", path, m.Name, err)
				}
			case ".debug$T":
				if types, err = cv.ParseTypes(s.Data); err != nil {
					return fmt.Errorf("%s %s: %v", path, m.Name, err)
				}
			}
		}
		if types == nil {
			continue
		}
		mod.Finish()
		u.add(types, []*cv.Module{mod}, filepath.Base(path)+" "+filepath.Base(strings.ReplaceAll(m.Name, "\\", "/")))
	}
	return nil
}

// Renders one type table's aggregates, typedefs, routines and globals into the collection
func (u *Universe) add(types *cv.Types, mods []*cv.Module, source string) {
	names := map[uint32]string{}
	pointers := map[uint32][]string{}
	for _, mod := range mods {
		for _, udt := range mod.UDTs {
			if types.Kind(udt.Type) == 0x1002 {
				pointers[types.Complete(types.Decode(udt.Type).Underlying)] = appendUnique(pointers[types.Complete(types.Decode(udt.Type).Underlying)], udt.Name)
				continue
			}
			full := types.Complete(udt.Type)
			if r := types.Decode(full); r.Name != "" && r.Name != udt.Name && strings.TrimPrefix(r.Name, "_") != udt.Name {
				continue
			}
			if _, taken := names[full]; !taken {
				names[full] = udt.Name
			}
		}
	}
	types.Names = names
	seen := map[uint32]bool{}
	var defs []uint32
	for _, mod := range mods {
		for _, p := range mod.Procs {
			defs = append(defs, types.Dependencies(p.Type, seen)...)
			for _, v := range p.Vars {
				defs = append(defs, types.Dependencies(v.Type, seen)...)
			}
		}
		for _, d := range mod.Data {
			defs = append(defs, types.Dependencies(d.Type, seen)...)
		}
		for _, udt := range mod.UDTs {
			defs = append(defs, types.Dependencies(udt.Type, seen)...)
		}
	}
	for _, ti := range defs {
		r := types.Decode(ti)
		if r.Name == "" || strings.Contains(r.Name, "<") || strings.HasPrefix(r.Name, "__unnamed") {
			continue
		}
		d := &Definition{Name: r.Name, Kind: cv.KindWord(r.Kind), Size: r.Size, Body: types.Body(ti), Source: source}
		if alias, ok := names[ti]; ok {
			d.Aliases = []string{alias}
		}
		d.Pointers = pointers[ti]
		for _, need := range types.Embedded(ti) {
			if n := types.Decode(need).Name; n != "" && n != r.Name {
				d.Needs = appendUnique(d.Needs, n)
			}
		}
		u.define(d)
	}
	for _, mod := range mods {
		for _, udt := range mod.UDTs {
			full := types.Complete(udt.Type)
			if r := types.Decode(full); r.Name != "" && (r.Name == udt.Name || strings.TrimPrefix(r.Name, "_") == udt.Name) {
				continue
			}
			if types.Kind(udt.Type) == 0x1002 {
				continue
			}
			decl := types.Declare(udt.Type, udt.Name)
			if old, ok := u.Typedefs[udt.Name]; ok && old != decl {
				u.Conflicts = append(u.Conflicts, fmt.Sprintf("typedef %s: %s versus %s (%s)", udt.Name, old, decl, source))
				continue
			}
			u.Typedefs[udt.Name] = decl
		}
		for _, p := range mod.Procs {
			if !p.Global {
				continue
			}
			proto := prototype(types, p)
			if old, ok := u.Routines[p.Name]; ok && old != proto {
				u.Conflicts = append(u.Conflicts, fmt.Sprintf("routine %s: %s versus %s (%s)", p.Name, old, proto, source))
				continue
			}
			u.Routines[p.Name] = proto
		}
		for _, d := range mod.Data {
			if !d.Global {
				continue
			}
			decl := types.Declare(d.Type, d.Name)
			if old, ok := u.Globals[d.Name]; ok && old != decl {
				u.Conflicts = append(u.Conflicts, fmt.Sprintf("global %s: %s versus %s (%s)", d.Name, old, decl, source))
				continue
			}
			u.Globals[d.Name] = decl
		}
	}
}

func (u *Universe) define(d *Definition) {
	old, ok := u.Defs[d.Name]
	if !ok {
		u.Defs[d.Name] = d
		u.order = append(u.order, d.Name)
		return
	}
	if old.Body != d.Body || old.Size != d.Size {
		u.Conflicts = append(u.Conflicts, fmt.Sprintf("%s %s: %d bytes in %s versus %d bytes in %s", d.Kind, d.Name, old.Size, old.Source, d.Size, d.Source))
		return
	}
	for _, a := range d.Aliases {
		old.Aliases = appendUnique(old.Aliases, a)
	}
	for _, p := range d.Pointers {
		old.Pointers = appendUnique(old.Pointers, p)
	}
	for _, n := range d.Needs {
		old.Needs = appendUnique(old.Needs, n)
	}
}

func appendUnique(list []string, s string) []string {
	for _, x := range list {
		if x == s {
			return list
		}
	}
	return append(list, s)
}

// Families route names onto header files by prefix, the first match winning
var families = []struct {
	re   *regexp.Regexp
	file string
}{
	{regexp.MustCompile(`^_?IMAGE_`), "ntimage.h"},
	{regexp.MustCompile(`^_?(K[A-Z_]|Ke|Ki|Kf|Kx|KIRQL|KPRIORITY|KPROCESSOR_MODE|KSPIN_LOCK|KWAIT|KTIMER|KDPC|KAPC|KEVENT|KMUTANT|KSEMAPHORE|KTHREAD|KPROCESS|KPRCB|KPCR|KINTERRUPT|KSYSTEM_TIME|KSERVICE)`), "ke.h"},
	{regexp.MustCompile(`^_?(ERESOURCE|EX_|Ex|EXCEPTION_|ETIMER|EEVENT)`), "ex.h"},
	{regexp.MustCompile(`^_?(ETHREAD|EPROCESS|Ps|PS_)`), "ps.h"},
	{regexp.MustCompile(`^_?(MM|Mm|Mi[A-Z]|PFN|MEMORY_|MDL|SECTION_)`), "mm.h"},
	{regexp.MustCompile(`^_?(IRP|IO_|Io|FILE_|DEVICE_|DRIVER_|FS_|SHARE_ACCESS|VPB|IDE|IDEX|Ide|Idex|SCSI|CDROM|DISK_|PARTITION_|STORAGE_)`), "io.h"},
	{regexp.MustCompile(`^_?(OBJECT_|Ob|OB_)`), "ob.h"},
	{regexp.MustCompile(`^_?(RTL_|Rtl|STRING$|ANSI_STRING|UNICODE_STRING|OEM_STRING|TIME_FIELDS|HEAP|Heap|GENERIC_MAPPING|CRITICAL_SECTION|OSVERSIONINFO)`), "ntrtl.h"},
	{regexp.MustCompile(`^_?(HAL|Hal|SMC|Smc|PCI|Pci)`), "hal.h"},
	{regexp.MustCompile(`^_?(KD_|Kd|DBGKD|DBGKM|Dbg|DEBUG_|KdPrint)`), "kd.h"},
	{regexp.MustCompile(`^_?(FAT|Fat|GDF|Gdf|UDF|Udf|RAW|Raw|Fsc|FSC|FS_|BOOT_SECTOR|DIRENT)`), "fs.h"},
	{regexp.MustCompile(`^_?(XC|Xc|XCRYPT|Av|AV_|XBOX|Xbox|XBE|Xbe|XE_|Xe|LDR|Ldr|XPR|D3D|NV2A|Nv|XPP|Xpp|XID|Xid|USB|Usb|XKEY|Xkey|XONLINE|XNET|Xnet|XAUDIO|XAUD|XSTREAM|Ani|ANI|CDS|Cds|HDKEY|Hd|Launch|LAUNCH|XINPUT|XInput|XIP|Xip|MU_|Mu)`), "xbox.h"},
	{regexp.MustCompile(`^(Nt|Zw)`), "ntapi.h"},
	{regexp.MustCompile(`^_?(LIST_ENTRY|SINGLE_LIST_ENTRY|LARGE_INTEGER|ULARGE_INTEGER|LUID|GUID|_GUID|OBJECT_ATTRIBUTES|PHYSICAL_ADDRESS|EVENT_TYPE|WAIT_TYPE|TIMER_TYPE|IO_STATUS_BLOCK|CONTEXT|FLOATING_SAVE_AREA|EXCEPTION_RECORD|EXCEPTION_POINTERS|NT_TIB|TEB|PEB|CLIENT_ID|MODE|_MODE)`), "ntdef.h"},
}

func fileFor(name string) string {
	for _, f := range families {
		if f.re.MatchString(name) {
			return f.file
		}
	}
	return "misc.h"
}

var fileOrder = []string{"ntdef.h", "ntimage.h", "ke.h", "ex.h", "ps.h", "mm.h", "ob.h", "io.h", "ntrtl.h", "hal.h", "kd.h", "fs.h", "xbox.h", "misc.h", "ntapi.h"}

// Write emits the headers into a directory plus an umbrella header including them in order
func (u *Universe) Write(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	sorted := u.sorted()
	file := map[string]string{}
	for _, name := range sorted {
		file[name] = fileFor(name)
	}
	for changed := true; changed; {
		changed = false
		for _, name := range sorted {
			for _, need := range u.Defs[name].Needs {
				if _, ok := u.Defs[need]; ok && rank(file[need]) > rank(file[name]) {
					file[need] = file[name]
					changed = true
				}
			}
		}
	}
	bodies := map[string]*strings.Builder{}
	for _, f := range fileOrder {
		b := &strings.Builder{}
		guard := "_" + strings.ToUpper(strings.ReplaceAll(f, ".", "_")) + "_"
		fmt.Fprintf(b, "#ifndef %s\n#define %s\n\n", guard, guard)
		bodies[f] = b
	}
	nt := bodies["ntdef.h"]
	names := make([]string, 0, len(u.Typedefs))
	for n := range u.Typedefs {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		fmt.Fprintf(nt, "typedef %s;\n", u.Typedefs[n])
	}
	nt.WriteString("\n")
	for _, name := range sorted {
		d := u.Defs[name]
		fmt.Fprintf(nt, "%s %s;\n", d.Kind, d.Name)
	}
	nt.WriteString("\n")
	for _, name := range sorted {
		d := u.Defs[name]
		b := bodies[file[name]]
		fmt.Fprintf(b, "typedef %s %s %s", d.Kind, d.Name, d.Body)
		tail := append([]string{}, d.Aliases...)
		for _, p := range d.Pointers {
			tail = append(tail, "*"+p)
		}
		if len(tail) == 0 {
			tail = []string{strings.TrimPrefix(d.Name, "_")}
		}
		fmt.Fprintf(b, " %s;\n\n", strings.Join(tail, ", "))
	}
	names = names[:0]
	for n := range u.Globals {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		fmt.Fprintf(bodies[fileFor(n)], "extern %s;\n", u.Globals[n])
	}
	names = names[:0]
	for n := range u.Routines {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		fmt.Fprintf(bodies[fileFor(n)], "%s\n", u.Routines[n])
	}
	var umbrella strings.Builder
	umbrella.WriteString("#ifndef _NTOS_\n#define _NTOS_\n\n")
	for _, f := range fileOrder {
		b := bodies[f]
		fmt.Fprintf(b, "\n#endif\n")
		if err := os.WriteFile(filepath.Join(dir, f), []byte(b.String()), 0o644); err != nil {
			return err
		}
		fmt.Fprintf(&umbrella, "#include \"%s\"\n", f)
	}
	umbrella.WriteString("\n#endif\n")
	return os.WriteFile(filepath.Join(dir, "ntos.h"), []byte(umbrella.String()), 0o644)
}

func rank(f string) int {
	for i, x := range fileOrder {
		if x == f {
			return i
		}
	}
	return len(fileOrder)
}

// Orders definitions so every type embedded by value is defined before its user
func (u *Universe) sorted() []string {
	state := map[string]int{}
	var out []string
	var visit func(string)
	visit = func(n string) {
		if state[n] != 0 {
			return
		}
		state[n] = 1
		for _, need := range u.Defs[n].Needs {
			if _, ok := u.Defs[need]; ok {
				visit(need)
			}
		}
		state[n] = 2
		out = append(out, n)
	}
	for _, n := range u.order {
		visit(n)
	}
	return out
}
