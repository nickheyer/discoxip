package xbe

import (
	"fmt"
	"sort"
	"strings"

	"golang.org/x/arch/x86/x86asm"
)

// paramKind is how a library function uses one of its parameters.
type paramKind int

const (
	kindIgnored  paramKind = iota // the implementation never reads it
	kindIndex                     // scaled into an address or table
	kindPointer                   // dereferenced or copied from
	kindOut                       // written through
	kindStored                    // stored as a whole word
	kindCombined                  // folded into a word with other values
	kindCount                     // compared against a bound and stepped
	kindFlags                     // tested bit by bit
)

var kindNames = map[paramKind]string{kindIgnored: "ignored", kindIndex: "index", kindPointer: "pointer", kindOut: "out", kindStored: "stored", kindCombined: "combined", kindCount: "count", kindFlags: "flags"}

// XDK functions whose link time optimized builds take parameters in registers, with how each parameter is used
var xdkParameters = map[string][]paramKind{
	"D3DDevice_SetTransform":                  {kindIndex, kindPointer},
	"D3DDevice_SetTextureState_TexCoordIndex": {kindIndex, kindStored},
	"D3DDevice_SetTextureState_BorderColor":   {kindIndex, kindStored},
	"D3DDevice_SetTextureState_ColorKeyColor": {kindIndex, kindStored},
	"D3DDevice_SetTextureState_BumpEnv":       {kindIndex, kindCombined, kindStored},
	"D3DDevice_Swap":                          {kindFlags},
	"D3DDevice_DrawVertices":                  {kindStored, kindCombined, kindCount},
	"D3DDevice_DrawIndexedVertices":           {kindStored, kindCount, kindPointer},
	"D3DDevice_SetTexture":                    {kindIndex, kindPointer},
	"D3DDevice_SetPalette":                    {kindIndex, kindPointer},
	"D3DDevice_SetStreamSource":               {kindIndex, kindPointer, kindStored},
	"D3DDevice_SetIndices":                    {kindPointer, kindStored},
	"D3DDevice_SetVertexShader":               {kindFlags},
	"D3DDevice_GetTransform":                  {kindIndex, kindOut},
	"D3DDevice_SetMaterial":                   {kindPointer},
	"D3DDevice_SetViewport":                   {kindPointer},
	"D3DDevice_CreateVertexShader":            {kindPointer, kindPointer, kindOut, kindFlags},
	"Direct3D_CreateDevice":                   {kindIgnored, kindIgnored, kindIgnored, kindFlags, kindPointer, kindOut},
}

// callConvention is where a library function takes each parameter and how it leaves the stack.
type callConvention struct {
	params     []argLocation // canonical order, a location marked none for an ignored parameter
	stackBytes int
	known      bool // the parameter locations are derived, otherwise the stack order of the signature applies
}

// roleSet records every way a function's input is used
type roleSet struct {
	scaled, loadBase, storeBase, stored, combined, compared, tested, forwarded bool
}

func (r roleSet) kind() paramKind {
	switch {
	case r.storeBase:
		return kindOut
	case r.loadBase:
		return kindPointer
	case r.scaled:
		return kindIndex
	case r.stored && !r.compared:
		return kindStored
	case r.tested && !r.stored && !r.combined:
		return kindFlags
	case r.compared:
		return kindCount
	case r.combined:
		return kindCombined
	case r.stored:
		return kindStored
	}
	return kindIgnored
}

// Returns the stack bytes the non optimized build of a library function takes, from the signature database
func canonicalArgBytes(name string) (int, bool) {
	pats, err := loadPatterns()
	if err != nil {
		return 0, false
	}
	best, found := -1, false
	for i := range pats {
		if pats[i].Lib == "d3d8ltcg" {
			continue
		}
		sym := decorateSymbol(pats[i].Symbol)
		if sym.Name == name && sym.ArgBytes > best {
			best, found = sym.ArgBytes, true
		}
	}
	return best, found
}

// Derives where a library function takes its parameters, matching the registers it reads to its declared uses
func (d *Disassembly) convention(fn *Function) (callConvention, error) {
	if d.conventions == nil {
		d.conventions = map[uint32]callConvention{}
	}
	if c, done := d.conventions[fn.EntryVA]; done {
		return c, nil
	}
	sym := fn.Symbol
	stackBytes := calleeCleanupOf(fn)
	regs := d.liveInRegisters(fn)
	conv := callConvention{stackBytes: stackBytes}
	if sym == nil {
		// An unnamed library helper lists its register inputs in a fixed order before the stack.
		for _, r := range regs {
			conv.params = append(conv.params, argLocation{Reg: r})
		}
		for i := 0; i < stackBytes/4; i++ {
			conv.params = append(conv.params, argLocation{Stack: i})
		}
		conv.known = true
		d.conventions[fn.EntryVA] = conv
		return conv, nil
	}
	canonical, hasCanonical := canonicalArgBytes(sym.Name)
	switch {
	case sym.Cdecl && len(regs) > 0:
		// A caller cleaned routine whose body reads registers on entry is passed them by the optimized build, ahead of
		// the stack arguments its body reads
		for _, r := range regs {
			conv.params = append(conv.params, argLocation{Reg: r})
		}
		for i := 0; i < d.stackInputs(fn); i++ {
			conv.params = append(conv.params, argLocation{Stack: i})
		}
		conv.known = true
	case sym.Cdecl || sym.FastCall:
	case len(regs) == 0, hasCanonical && canonical == stackBytes:
	case !hasCanonical && len(regs) == 1 && regs[0] == x86asm.ECX:
		// A member function reading only ecx is a plain thiscall.
	case hasCanonical && stackBytes == 0 && len(regs) == 1 && canonical == 4:
		// The optimized build passes the signature's one parameter in the one register the body reads
		conv.params = []argLocation{{Reg: regs[0]}}
		conv.known = true
	case !hasCanonical:
		// An internal helper has no signature, so its register inputs are listed in a fixed order before the stack.
		for _, r := range regs {
			conv.params = append(conv.params, argLocation{Reg: r})
		}
		for i := 0; i < stackBytes/4; i++ {
			conv.params = append(conv.params, argLocation{Stack: i})
		}
		conv.known = true
	default:
		kinds, declared := xdkParameters[sym.Name]
		if !declared {
			// An undeclared routine is only ever run from the image, so its callers pass the registers it reads in a fixed
			// order before its stack arguments, as an internal helper's do
			for _, r := range regs {
				conv.params = append(conv.params, argLocation{Reg: r})
			}
			for i := 0; i < stackBytes/4; i++ {
				conv.params = append(conv.params, argLocation{Stack: i})
			}
			conv.known = true
			break
		}
		roles, err := d.inputRoles(fn, regs, stackBytes/4)
		if err != nil {
			return callConvention{}, err
		}
		params, err := matchParameters(sym.Name, kinds, roles)
		if err != nil {
			return callConvention{}, err
		}
		conv.params, conv.known = params, true
	}
	d.conventions[fn.EntryVA] = conv
	return conv, nil
}

// Reports every declared use an input's features support
func (r roleSet) supports(k paramKind) bool {
	switch k {
	case kindIndex:
		return r.scaled
	case kindPointer:
		return r.loadBase || r.forwarded
	case kindOut:
		return r.storeBase
	case kindStored:
		return r.stored
	case kindCombined:
		return r.combined || r.stored
	case kindCount:
		return r.compared
	case kindFlags:
		return r.tested || r.compared
	}
	return false
}

// Assigns the inputs to the declared parameters, every parameter taking an input whose uses include its own
func matchParameters(name string, kinds []paramKind, roles map[argLocation]roleSet) ([]argLocation, error) {
	locs := make([]argLocation, 0, len(roles))
	for loc := range roles {
		locs = append(locs, loc)
	}
	sort.Slice(locs, func(i, j int) bool {
		if (locs[i].Reg != 0) != (locs[j].Reg != 0) {
			return locs[i].Reg != 0
		}
		if locs[i].Reg != locs[j].Reg {
			return locs[i].Reg < locs[j].Reg
		}
		return locs[i].Stack < locs[j].Stack
	})
	var wanted []int
	for i, k := range kinds {
		if k != kindIgnored {
			wanted = append(wanted, i)
		}
	}
	var describe []string
	for _, loc := range locs {
		describe = append(describe, fmt.Sprintf("%s=%s", loc, kindNames[roles[loc].kind()]))
	}
	if len(wanted) != len(locs) {
		return nil, fmt.Errorf("xbe: %s declares %d parameters but takes %d inputs (%s)", name, len(wanted), len(locs), strings.Join(describe, " "))
	}
	var solutions [][]argLocation
	var exact [][]argLocation
	assignment := make([]argLocation, len(kinds))
	for i := range assignment {
		assignment[i] = argLocation{None: true}
	}
	used := make([]bool, len(locs))
	var search func(pos int)
	search = func(pos int) {
		if pos == len(wanted) {
			sol := append([]argLocation(nil), assignment...)
			solutions = append(solutions, sol)
			primary := true
			for _, i := range wanted {
				if roles[assignment[i]].kind() != kinds[i] {
					primary = false
				}
			}
			if primary {
				exact = append(exact, sol)
			}
			return
		}
		param := wanted[pos]
		for j, loc := range locs {
			if used[j] || !roles[loc].supports(kinds[param]) {
				continue
			}
			used[j] = true
			assignment[param] = loc
			search(pos + 1)
			used[j] = false
		}
	}
	search(0)
	// The optimizer passes a routine's leading parameters in registers and the rest on the stack in order
	var ordered [][]argLocation
	for _, sol := range solutions {
		if passedInOrder(sol) {
			ordered = append(ordered, sol)
		}
	}
	switch {
	case len(solutions) == 1:
		return solutions[0], nil
	case len(solutions) > 1 && len(exact) == 1:
		return exact[0], nil
	case len(solutions) > 1 && len(ordered) == 1:
		return ordered[0], nil
	case len(solutions) == 0:
		return nil, fmt.Errorf("xbe: no input of %s fits each declared parameter (%s)", name, strings.Join(describe, " "))
	}
	return nil, fmt.Errorf("xbe: %d assignments of the inputs of %s fit its declared parameters (%s)", len(solutions), name, strings.Join(describe, " "))
}

// Reports whether an assignment puts every register parameter before every stack one and the stack ones in order
func passedInOrder(sol []argLocation) bool {
	stacked, last := false, -1
	for _, loc := range sol {
		switch {
		case loc.None:
		case loc.Reg != 0:
			if stacked {
				return false
			}
		default:
			if loc.Stack <= last {
				return false
			}
			stacked, last = true, loc.Stack
		}
	}
	return true
}

func (l argLocation) String() string {
	if l.Reg != 0 {
		return l.Reg.String()
	}
	return fmt.Sprintf("stack%d", l.Stack)
}

// Runs a library function on symbolic inputs and records how each register and stack argument is used
func (d *Disassembly) inputRoles(fn *Function, regs []x86asm.Reg, stackArgs int) (map[argLocation]roleSet, error) {
	ex := NewExecutorShared(d.baseExecutor())
	ex.StepLimit = 20000
	ex.PathLimit = 64
	for _, t := range fn.CallTargets {
		ex.Opaque[t] = true
	}
	names := map[string]argLocation{}
	paths, partial, _, _ := ex.RunWithRetries(fn.EntryVA, func(s *execState) {
		for _, r := range regs {
			name := "in." + r.String()
			s.regs[regIndex[r]] = Sym{name}
			names[name] = argLocation{Reg: r}
		}
		args := make([]Expr, stackArgs)
		for i := range args {
			name := fmt.Sprintf("in.stack%d", i)
			args[i] = Sym{name}
			names[name] = argLocation{Stack: i}
		}
		s.pushArgs(args...)
	}, 4)
	roles := map[argLocation]roleSet{}
	for name, loc := range names {
		roles[loc] = roleSet{}
		_ = name
	}
	note := func(e Expr, f func(loc *roleSet)) {
		set := map[string]bool{}
		exprSyms(e, set)
		for name := range set {
			if loc, ok := names[name]; ok {
				r := roles[loc]
				f(&r)
				roles[loc] = r
			}
		}
	}
	var effects []Effect
	var guards []Expr
	for _, p := range paths {
		effects = append(effects, p.Effects...)
		guards = append(guards, p.Guards...)
	}
	effects = append(effects, partial...)
	for _, e := range effects {
		if e.Addr != nil && e.Kind == "store" {
			bases, indexes := addressBases(d, e.Addr, names)
			for _, loc := range bases {
				r := roles[loc]
				r.storeBase = true
				roles[loc] = r
			}
			for _, loc := range indexes {
				r := roles[loc]
				r.scaled = true
				roles[loc] = r
			}
			walkInputUses(d, e.Addr, names, roles)
		}
		if e.Value != nil {
			if s, ok := e.Value.(Sym); ok && names[s.Name] != (argLocation{}) || isInputSym(e.Value, names) {
				note(e.Value, func(r *roleSet) { r.stored = true })
			} else {
				note(e.Value, func(r *roleSet) { r.combined = true })
			}
			walkInputUses(d, e.Value, names, roles)
		}
		for _, a := range e.Args {
			walkInputUses(d, a, names, roles)
			note(a, func(r *roleSet) { r.combined = true })
			// An input handed on whole is the pointer or handle the callee works through
			if isInputSym(a, names) {
				note(a, func(r *roleSet) { r.forwarded = true })
			}
		}
	}
	for _, g := range guards {
		walkInputUses(d, g, names, roles)
		if c, ok := g.(Cond); ok {
			_, ac := isConst(c.A)
			_, bc := isConst(c.B)
			if ac || bc {
				note(g, func(r *roleSet) { r.compared = true })
			}
		}
		if n, ok := g.(Not); ok {
			if c, ok := n.A.(Cond); ok {
				_, ac := isConst(c.A)
				_, bc := isConst(c.B)
				if ac || bc {
					note(g, func(r *roleSet) { r.compared = true })
				}
			}
		}
	}
	return roles, nil
}

func isInputSym(e Expr, names map[string]argLocation) bool {
	s, ok := e.(Sym)
	if !ok {
		return false
	}
	_, ok = names[s.Name]
	return ok
}

// Records loads through an input, scaling of an input and bit tests of an input found inside an expression
func walkInputUses(d *Disassembly, e Expr, names map[string]argLocation, roles map[argLocation]roleSet) {
	mark := func(sub Expr, f func(r *roleSet)) {
		set := map[string]bool{}
		exprSyms(sub, set)
		for name := range set {
			if loc, ok := names[name]; ok {
				r := roles[loc]
				f(&r)
				roles[loc] = r
			}
		}
	}
	var walk func(e Expr)
	walk = func(e Expr) {
		switch v := e.(type) {
		case Load:
			if v.Addr != nil {
				bases, indexes := addressBases(d, v.Addr, names)
				for _, loc := range bases {
					r := roles[loc]
					r.loadBase = true
					roles[loc] = r
				}
				for _, loc := range indexes {
					r := roles[loc]
					r.scaled = true
					roles[loc] = r
				}
				walk(v.Addr)
			}
		case Bin:
			if v.Op == "mul" || v.Op == "shl" {
				if _, ok := isConst(v.B); ok {
					mark(v.A, func(r *roleSet) { r.scaled = true })
				}
			}
			if v.Op == "and" {
				if _, ok := isConst(v.B); ok {
					mark(v.A, func(r *roleSet) { r.tested = true })
				}
			}
			walk(v.A)
			walk(v.B)
		case Un:
			walk(v.A)
		case Cond:
			walk(v.A)
			walk(v.B)
		case Not:
			walk(v.A)
		case FCmpSet:
			walk(v.A)
			walk(v.B)
		case CallResult:
			for _, a := range v.Args {
				walk(a)
			}
		case Block:
			for _, a := range v.Elems {
				walk(a)
			}
		case Elem:
			walk(v.A)
		case Sel:
			walk(v.C)
			walk(v.A)
			walk(v.B)
		}
	}
	walk(e)
}

// Splits the inputs an address adds in unscaled into base pointers and indexes into a table at a constant address
func addressBases(d *Disassembly, e Expr, names map[string]argLocation) (bases, indexes []argLocation) {
	var walk func(e Expr, table bool)
	walk = func(e Expr, table bool) {
		switch v := e.(type) {
		case Sym:
			if loc, ok := names[v.Name]; ok {
				if table {
					indexes = append(indexes, loc)
				} else {
					bases = append(bases, loc)
				}
			}
		case Bin:
			if v.Op == "add" || v.Op == "sub" || v.Op == "or" {
				inTable := table
				if c, ok := isConst(v.B); ok && d.sectionOf(c) != nil {
					inTable = true
				}
				if c, ok := isConst(v.A); ok && d.sectionOf(c) != nil {
					inTable = true
				}
				walk(v.A, inTable)
				walk(v.B, inTable)
			}
		case Un:
			walk(v.A, table)
		}
	}
	walk(e, false)
	return bases, indexes
}

// Returns one executor whose precomputed image knowledge every probe can share
func (d *Disassembly) baseExecutor() *Executor {
	if d.probe == nil {
		d.probe = NewExecutor(d)
	}
	return d.probe
}
