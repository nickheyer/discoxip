package native

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/nickheyer/discoxip/pkg/native/coff"
	"github.com/nickheyer/discoxip/pkg/native/cv"
	"github.com/nickheyer/discoxip/pkg/native/pdb"
	"golang.org/x/arch/x86/x86asm"
)

// Dump writes everything a reference object tells about its source, prototypes, types, data, lines and code
func Dump(w io.Writer, path, member string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if strings.HasPrefix(string(data[:min(len(data), 24)]), "Microsoft C/C++") {
		return dumpPDB(w, data, member)
	}
	if member != "" {
		members, err := coff.ReadArchive(data)
		if err != nil {
			return err
		}
		m, ok := coff.Find(members, member)
		if !ok {
			return fmt.Errorf("%s holds no member %s", path, member)
		}
		data = m.Data
	}
	obj, err := coff.Parse(data)
	if err != nil {
		return err
	}
	mod := cv.NewModule()
	var types *cv.Types
	for _, s := range obj.Sections {
		switch s.Name {
		case ".debug$S":
			if err := mod.AddSection(s.Data); err != nil {
				return err
			}
		case ".debug$T":
			if types, err = cv.ParseTypes(s.Data); err != nil {
				return err
			}
		}
	}
	mod.Finish()
	if types == nil {
		types, _ = cv.ParseTypes([]byte{4, 0, 0, 0})
	}
	fmt.Fprintf(w, "object   %s\n", mod.Object)
	fmt.Fprintf(w, "compiler %s %s\n", mod.Compiler, mod.Version)
	for _, s := range obj.Symbols {
		if s.Class == coff.ClassFile {
			fmt.Fprintf(w, "source   %s\n", s.Name)
		}
	}
	if types.Server != "" {
		fmt.Fprintf(w, "types    in %s\n", types.Server)
	}
	fmt.Fprintln(w)
	seen := map[uint32]bool{}
	var deps []uint32
	for _, p := range mod.Procs {
		deps = append(deps, types.Dependencies(p.Type, seen)...)
		for _, v := range p.Vars {
			deps = append(deps, types.Dependencies(v.Type, seen)...)
		}
	}
	for _, d := range mod.Data {
		deps = append(deps, types.Dependencies(d.Type, seen)...)
	}
	for _, u := range mod.UDTs {
		deps = append(deps, types.Dependencies(u.Type, seen)...)
	}
	if len(deps) > 0 {
		fmt.Fprintln(w, "// types")
		for _, d := range deps {
			fmt.Fprintln(w, types.Define(d))
			fmt.Fprintln(w)
		}
	}
	if len(mod.UDTs) > 0 {
		fmt.Fprintln(w, "// typedefs")
		for _, u := range mod.UDTs {
			fmt.Fprintf(w, "typedef %s;\n", types.Declare(u.Type, u.Name))
		}
		fmt.Fprintln(w)
	}
	if len(mod.Constants) > 0 {
		fmt.Fprintln(w, "// constants")
		for _, c := range mod.Constants {
			fmt.Fprintf(w, "%s = %d\n", c.Name, c.Value)
		}
		fmt.Fprintln(w)
	}
	if len(mod.Data) > 0 {
		fmt.Fprintln(w, "// data")
		for _, d := range mod.Data {
			scope := "static "
			if d.Global {
				scope = ""
			}
			fmt.Fprintf(w, "%s%s; // section %d offset %#x\n", scope, types.Declare(d.Type, d.Name), d.Segment, d.Offset)
		}
		fmt.Fprintln(w)
	}
	fmt.Fprintln(w, "// sections")
	for i, s := range obj.Sections {
		if s.Debug() {
			continue
		}
		fmt.Fprintf(w, "%d %-10s %6d bytes flags %08x relocs %d\n", i+1, s.Name, len(s.Data), s.Flags, len(s.Relocs))
		if s.Flags&0x20 == 0 && len(s.Data) > 0 {
			hexdump(w, s.Data, s.Relocs)
		}
	}
	fmt.Fprintln(w)
	fns := obj.Functions()
	sort.Slice(fns, func(a, b int) bool {
		if fns[a].Section != fns[b].Section {
			return fns[a].Section < fns[b].Section
		}
		return fns[a].Start < fns[b].Start
	})
	for _, f := range fns {
		p := findProc(mod, f, obj)
		lines := coffLines(obj, f)
		fmt.Fprintf(w, "// function %s, section %d, %d bytes\n", f.Name, f.Section+1, f.End-f.Start)
		if p != nil {
			lo, hi := span(lines)
			scope := "static "
			if p.Global {
				scope = ""
			}
			fmt.Fprintf(w, "%s%s // lines %d-%d\n", scope, prototype(types, *p), lo, hi)
			for _, v := range p.Locals() {
				fmt.Fprintf(w, "//   local %s at %s\n", types.Declare(v.Type, v.Name), place(v))
			}
		}
		body, relocs := obj.Body(f)
		disassemble(w, body, relocs, lines, f.Start)
		fmt.Fprintln(w)
	}
	return nil
}

// Renders a routine's prototype with parameter names from its symbols
func prototype(types *cv.Types, p cv.Proc) string {
	params := p.Params()
	args := types.Args(p.Type)
	parts := make([]string, 0, len(args))
	for i, a := range args {
		if a == 0 {
			parts = append(parts, "...")
			continue
		}
		name := fmt.Sprintf("arg%d", i)
		if i < len(params) {
			name = params[i].Name
		}
		parts = append(parts, types.Declare(a, name))
	}
	r := types.Decode(p.Type)
	call := map[uint8]string{0: "__cdecl", 4: "__fastcall", 7: "__stdcall", 11: "__thiscall"}[r.Call]
	list := strings.Join(parts, ", ")
	if len(args) == 0 {
		list = "void"
	}
	return fmt.Sprintf("%s;", types.Declare(r.Return, fmt.Sprintf("%s %s(%s)", call, p.Name, list)))
}

func place(v cv.Variable) string {
	if v.Register != 0 {
		if v.Offset != 0 {
			return fmt.Sprintf("reg%d%+d", v.Register, v.Offset)
		}
		return fmt.Sprintf("reg%d", v.Register)
	}
	return fmt.Sprintf("ebp%+d", v.Offset)
}

func findProc(mod *cv.Module, f coff.Function, obj *coff.Object) *cv.Proc {
	for i := range mod.Procs {
		p := &mod.Procs[i]
		if int(p.Segment) == f.Section+1 && p.Offset == f.Start {
			return p
		}
	}
	for i := range mod.Procs {
		if mod.Procs[i].Name == strings.TrimPrefix(strings.SplitN(f.Name, "@", 2)[0], "_") {
			return &mod.Procs[i]
		}
	}
	return nil
}

// Resolves a routine's absolute source lines from the section's table and its begin symbol
func coffLines(obj *coff.Object, f coff.Function) map[uint32]uint32 {
	base := uint32(0)
	for _, s := range obj.Symbols {
		if s.Name == ".bf" && int(s.Section) == f.Section+1 && s.Value == f.Start {
			base = uint32(s.Line)
		}
	}
	out := map[uint32]uint32{}
	for _, l := range obj.Sections[f.Section].Lines {
		if l.Line != 0 && l.Offset >= f.Start && l.Offset < f.End {
			out[l.Offset-f.Start] = base + uint32(l.Line) - 1
		}
	}
	return out
}

func span(lines map[uint32]uint32) (uint32, uint32) {
	lo, hi := uint32(0), uint32(0)
	for _, l := range lines {
		if lo == 0 || l < lo {
			lo = l
		}
		if l > hi {
			hi = l
		}
	}
	return lo, hi
}

// Writes Intel syntax disassembly with relocation targets and source lines interleaved
func disassemble(w io.Writer, body []byte, relocs []coff.Reloc, lines map[uint32]uint32, base uint32) {
	for pos := 0; pos < len(body); {
		if ln, ok := lines[uint32(pos)]; ok {
			fmt.Fprintf(w, "        ; line %d\n", ln)
		}
		inst, err := x86asm.Decode(body[pos:], 32)
		if err != nil || inst.Len == 0 {
			fmt.Fprintf(w, "  %04x  db %02x\n", base+uint32(pos), body[pos])
			pos++
			continue
		}
		text := x86asm.IntelSyntax(inst, uint64(pos), nil)
		var refs []string
		for _, r := range relocs {
			if int(r.Offset) >= pos && int(r.Offset) < pos+inst.Len {
				refs = append(refs, fmt.Sprintf("%s%s", r.Symbol, relocType(r.Type)))
			}
		}
		if len(refs) > 0 {
			text += "    ; " + strings.Join(refs, ", ")
		}
		fmt.Fprintf(w, "  %04x  %-24x %s\n", base+uint32(pos), body[pos:pos+inst.Len], text)
		pos += inst.Len
	}
}

func relocType(t uint16) string {
	switch t {
	case 0x06:
		return ""
	case 0x07:
		return " (rva)"
	case 0x0A:
		return " (section)"
	case 0x0B:
		return " (secrel)"
	case 0x14:
		return " (rel32)"
	}
	return fmt.Sprintf(" (type %#x)", t)
}

func hexdump(w io.Writer, data []byte, relocs []coff.Reloc) {
	for off := 0; off < len(data); off += 16 {
		end := off + 16
		if end > len(data) {
			end = len(data)
		}
		var text strings.Builder
		for _, b := range data[off:end] {
			if b >= 0x20 && b < 0x7F {
				text.WriteByte(b)
			} else {
				text.WriteByte('.')
			}
		}
		var refs []string
		for _, r := range relocs {
			if int(r.Offset) >= off && int(r.Offset) < end {
				refs = append(refs, fmt.Sprintf("+%x %s", int(r.Offset)-off, r.Symbol))
			}
		}
		fmt.Fprintf(w, "    %04x  %-48x %s %s\n", off, data[off:end], text.String(), strings.Join(refs, " "))
	}
}

// Lists a program database's modules, or one module's routines, variables and types when named
func dumpPDB(w io.Writer, data []byte, module string) error {
	f, err := pdb.Open(data)
	if err != nil {
		return err
	}
	if module == "" {
		for _, m := range f.Modules {
			fmt.Fprintf(w, "%-40s %-30s section %d offset %#x size %#x files %d\n", m.Name, m.Object, m.Section, m.Offset, m.Size, len(m.Files))
		}
		return nil
	}
	types, err := f.Types()
	if err != nil {
		return err
	}
	for _, m := range f.Modules {
		if !strings.EqualFold(m.Name, module) && !strings.HasSuffix(strings.ToLower(strings.ReplaceAll(m.Name, "\\", "/")), "/"+strings.ToLower(module)) {
			continue
		}
		mod, err := f.Symbols(m)
		if err != nil {
			return err
		}
		fmt.Fprintf(w, "module   %s\nlibrary  %s\nplaced   section %d offset %#x size %#x\n", m.Name, m.Object, m.Section, m.Offset, m.Size)
		for _, file := range m.Files {
			fmt.Fprintf(w, "file     %s\n", file)
		}
		fmt.Fprintf(w, "compiler %s %s\n\n", mod.Compiler, mod.Version)
		seen := map[uint32]bool{}
		var deps []uint32
		for _, p := range mod.Procs {
			deps = append(deps, types.Dependencies(p.Type, seen)...)
			for _, v := range p.Vars {
				deps = append(deps, types.Dependencies(v.Type, seen)...)
			}
		}
		for _, d := range mod.Data {
			deps = append(deps, types.Dependencies(d.Type, seen)...)
		}
		if len(deps) > 0 {
			fmt.Fprintln(w, "// types")
			for _, d := range deps {
				fmt.Fprintln(w, types.Define(d))
				fmt.Fprintln(w)
			}
		}
		if len(mod.Data) > 0 {
			fmt.Fprintln(w, "// data")
			for _, d := range mod.Data {
				scope := "static "
				if d.Global {
					scope = ""
				}
				fmt.Fprintf(w, "%s%s; // section %d offset %#x\n", scope, types.Declare(d.Type, d.Name), d.Segment, d.Offset)
			}
			fmt.Fprintln(w)
		}
		fmt.Fprintln(w, "// routines")
		for _, p := range mod.Procs {
			lo, hi := p.Span()
			scope := "static "
			if p.Global {
				scope = ""
			}
			fmt.Fprintf(w, "%s%s // section %d offset %#x, %d bytes, lines %d-%d\n", scope, prototype(types, p), p.Segment, p.Offset, p.Length, lo, hi)
			for _, v := range p.Locals() {
				fmt.Fprintf(w, "//   local %s at %s\n", types.Declare(v.Type, v.Name), place(v))
			}
		}
		return nil
	}
	return fmt.Errorf("no module named %s", module)
}
