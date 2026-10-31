package xbe

import (
	"bufio"
	"fmt"
	"io"
	"maps"
	"math"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// BlockTable is what the page needs beside the block functions to run them
type BlockTable struct {
	Blocks   int
	Files    int
	Routines []uint32
	Errors   map[string]string
}

// jsGen writes machine blocks as functions of the page, a node several expressions share named once, the rest inlined
type jsGen struct {
	w        *bufio.Writer
	mm       *MachineModel
	names    map[string]string
	plain    map[uint32]string
	routines map[uint32]bool
	body     strings.Builder
	memoI    map[exprKey]string
	memoF    map[exprKey]string
	refs     map[exprKey]int
	results  map[int]string
	loadsAt  map[int][]Expr
	changed  map[string]bool
	copied   map[string]bool
	temps    int
	block    *liftedBlock
	prefix   string
}

// The live state names in the order the entry copies and the condition function take them
var liveStateNames = []string{"FK", "FA", "FB", "FR", "FC", "FFA", "FFB", "CW", "ST", "DF"}

// WriteBlocks writes every lifted block as a function returning the function to run next into scripts of at most
// perFile blocks each, opened in turn, each script registering its plain blocks in the dispatch table named, the
// blocks of a code template kept apart under the template, a jump to a routine or to an address the page may hook
// going through the hook table
func (mm *MachineModel) WriteBlocks(open func(index int) (io.WriteCloser, error), perFile int, prefix, table string, hookable map[uint32]bool) (*BlockTable, error) {
	routines := map[uint32]bool{}
	for va := range mm.routines {
		routines[va] = true
	}
	for va := range hookable {
		routines[va] = true
	}
	g := &jsGen{mm: mm, names: map[string]string{}, plain: map[uint32]string{}, routines: routines, prefix: prefix}
	keys := slices.Sorted(maps.Keys(mm.blocks))
	templated := map[string]*CodeTemplate{}
	for _, key := range keys {
		b := mm.blocks[key]
		name := fmt.Sprintf("B%s_%08X", prefix, b.va)
		if strings.Contains(key, "@") {
			name = fmt.Sprintf("B%s_%08X_%08X", prefix, b.va, mm.blocks[key].predVA(key))
		} else if t := mm.templateOf(b.va); t != nil {
			templated[key] = t
		} else {
			g.plain[b.va] = name
		}
		g.names[key] = name
	}
	if perFile <= 0 {
		perFile = len(keys) + 1
	}
	tbl := &BlockTable{Blocks: len(mm.blocks), Errors: mm.Errors, Routines: mm.Routines()}
	var out io.WriteCloser
	var registered []string
	begin := func() error {
		f, err := open(tbl.Files)
		if err != nil {
			return err
		}
		out, tbl.Files = f, tbl.Files+1
		g.w = bufio.NewWriterSize(f, 1<<20)
		return nil
	}
	finish := func() error {
		fmt.Fprintf(g.w, "registerBlocks(%s, [%s]);\n", table, strings.Join(registered, ","))
		registered = nil
		if err := g.w.Flush(); err != nil {
			return err
		}
		err := out.Close()
		out = nil
		return err
	}
	for i, key := range keys {
		if i%perFile == 0 {
			if out != nil {
				if err := finish(); err != nil {
					return nil, err
				}
			}
			if err := begin(); err != nil {
				return nil, err
			}
		}
		b := mm.blocks[key]
		if err := g.writeBlock(key, b); err != nil {
			return nil, fmt.Errorf("block %s: %w", key, err)
		}
		if g.plain[b.va] == g.names[key] {
			registered = append(registered, fmt.Sprintf("0x%X,%s", b.va, g.names[key]))
		}
	}
	if out == nil {
		if err := begin(); err != nil {
			return nil, err
		}
	}
	for _, t := range mm.Templates {
		patched := make([]string, t.Length)
		for i := range patched {
			patched[i] = "0"
		}
		for _, slot := range t.Slots {
			for i := uint32(0); i < 4 && slot+i < t.Length; i++ {
				patched[slot+i] = "1"
			}
		}
		bytes := make([]string, len(t.Bytes))
		for i, b := range t.Bytes {
			bytes[i] = strconv.Itoa(int(b))
		}
		var entries []string
		for _, key := range keys {
			if templated[key] == t {
				entries = append(entries, fmt.Sprintf("[0x%X,%s]", mm.blocks[key].va-t.Base, g.names[key]))
			}
		}
		fmt.Fprintf(g.w, "TEMPLATES.push({ base: 0x%X, length: %d, bytes: [%s], patched: [%s], blocks: new Map([%s]) });\n", t.Base, t.Length, strings.Join(bytes, ","), strings.Join(patched, ","), strings.Join(entries, ","))
	}
	for _, key := range slices.Sorted(maps.Keys(mm.Errors)) {
		fmt.Fprintf(g.w, "LIFT_ERRORS.set(0x%X,%s);\n", blockKeyVA(key), strconv.Quote(mm.Errors[key]))
	}
	if err := finish(); err != nil {
		return nil, err
	}
	return tbl, nil
}

// The template a block lies in, where the model lifted one over it
func (mm *MachineModel) templateOf(va uint32) *CodeTemplate {
	for _, t := range mm.Templates {
		if va >= t.Base && va < t.Base+t.Length {
			return t
		}
	}
	return nil
}

// The predecessor address a flag carrying key names
func (b *liftedBlock) predVA(key string) uint32 {
	var va, pred uint32
	fmt.Sscanf(key, "%X@%X", &va, &pred)
	return pred
}

// Writes one block as a function, every value it leaves computed from the entry state before any is assigned
func (g *jsGen) writeBlock(key string, b *liftedBlock) error {
	g.resetBlock()
	g.block = b
	g.changed = liveStateChanged(b.effects)
	g.countRefs(b.emitted())
	g.loadsAt = loadsByEpoch(b)
	for k, e := range b.effects {
		g.preloads(k)
		g.effect(e)
	}
	for _, epoch := range slices.Sorted(maps.Keys(g.loadsAt)) {
		if epoch >= len(b.effects) {
			for _, l := range g.loadsAt[epoch] {
				g.preload(l)
			}
		}
	}
	var assigns []string
	for i, v := range b.regs {
		if v == nil || i == 4 || isSymbol(v, "reg."+registerNames[i]) {
			continue
		}
		assigns = append(assigns, fmt.Sprintf("%s = %s;", registerNames[i], g.hoist(g.ival(v))))
	}
	if b.spExpr == nil {
		return fmt.Errorf("the block leaves no stack pointer")
	}
	if !isSymbol(b.spExpr, "reg.ESP") {
		assigns = append(assigns, fmt.Sprintf("ESP = %s;", g.hoist(g.ival(b.spExpr))))
	}
	for _, name := range segNames {
		if v, ok := b.segs[name]; ok && !isSymbol(v, "reg."+name) {
			assigns = append(assigns, fmt.Sprintf("setSeg_%s(%s);", name, g.hoist(g.ival(v))))
		}
	}
	if len(b.fpu) > 0 && !stackUnchanged(b.fpu) {
		parts := make([]string, len(b.fpu))
		for i, e := range b.fpu {
			parts[i] = g.hoist(g.fval(e))
		}
		assigns = append(assigns, fmt.Sprintf("ST = [%s];", strings.Join(parts, ",")))
	}
	if b.cw != nil && !isSymbol(b.cw, "fpu.cw") {
		assigns = append(assigns, fmt.Sprintf("CW = %s;", g.hoist(g.ival(b.cw))))
	}
	for _, name := range slices.Sorted(maps.Keys(b.xmm)) {
		if isSymbol(b.xmm[name], name) {
			continue
		}
		var reg, lane int
		fmt.Sscanf(name, "xmm%d.%d", &reg, &lane)
		assigns = append(assigns, fmt.Sprintf("XMM[%d] = %s;", reg*4+lane, g.hoist(g.fval(b.xmm[name]))))
	}
	for _, name := range slices.Sorted(maps.Keys(b.mm)) {
		if isSymbol(b.mm[name], name) {
			continue
		}
		var reg, lane int
		fmt.Sscanf(name, "mm%d.%d", &reg, &lane)
		assigns = append(assigns, fmt.Sprintf("MMX[%d] = %s;", reg*2+lane, g.hoist(g.ival(b.mm[name]))))
	}
	flags, err := g.flagAssigns(b)
	if err != nil {
		return err
	}
	assigns = append(assigns, flags...)
	if b.fcmp != nil && !(isSymbol(b.fcmp.a, "fcmp.a") && isSymbol(b.fcmp.b, "fcmp.b")) {
		assigns = append(assigns, fmt.Sprintf("FCA = %s; FCB = %s;", g.hoist(g.fval(b.fcmp.a)), g.hoist(g.fval(b.fcmp.b))))
	}
	next, err := g.terminator(b)
	if err != nil {
		return err
	}
	fmt.Fprintf(g.w, "function %s(){\n%s%s%s%s\nreturn %s;\n}\n", g.names[key], g.notAvailable(b), g.entryCopies(), g.body.String(), strings.Join(assigns, "\n"), next)
	if b.term.kind == "switch" {
		targets := make([]string, len(b.term.targets))
		for i, target := range b.term.targets {
			targets[i] = fmt.Sprintf("0x%X", target)
		}
		fmt.Fprintf(g.w, "const S_%s = [%s];\n", g.names[key], strings.Join(targets, ","))
	}
	return nil
}

// The flag record assignments of a block, none where it passes its predecessor's record through unchanged
func (g *jsGen) flagAssigns(b *liftedBlock) ([]string, error) {
	fl := b.fl
	switch {
	case fl.kind == "fsw" && fl.fsw != nil:
		if isSymbol(fl.fsw.a, "flags.fsw_a") && isSymbol(fl.fsw.b, "flags.fsw_b") {
			return nil, nil
		}
		return []string{fmt.Sprintf("FK = %d; FFA = %s; FFB = %s;", jsFlagFsw, g.hoist(g.fval(fl.fsw.a)), g.hoist(g.fval(fl.fsw.b)))}, nil
	case fl.kind == "comiss":
		if isSymbol(fl.a, "flags.fsw_a") && isSymbol(fl.b, "flags.fsw_b") {
			return nil, nil
		}
		return []string{fmt.Sprintf("FK = %d; FFA = %s; FFB = %s;", jsFlagFsw, g.hoist(g.fval(fl.a)), g.hoist(g.fval(fl.b)))}, nil
	case fl.kind == "" || fl.kind == "machine":
		return nil, nil
	}
	code, known := flagKindCodes[fl.kind]
	if !known {
		return nil, fmt.Errorf("flag record of kind %s", fl.kind)
	}
	operands := []struct {
		e    Expr
		name string
		sym  string
	}{{fl.a, "FA", "flags.a"}, {fl.b, "FB", "flags.b"}, {fl.r, "FR", "flags.r"}, {fl.cf, "FC", "flags.cf"}}
	passed := true
	for _, p := range operands {
		if p.e != nil && !isSymbol(p.e, p.sym) {
			passed = false
		}
	}
	if passed {
		return nil, nil
	}
	parts := []string{fmt.Sprintf("FK = %d;", code)}
	for _, p := range operands {
		if p.e != nil {
			parts = append(parts, fmt.Sprintf("%s = %s;", p.name, g.hoist(g.ival(p.e))))
		} else {
			parts = append(parts, fmt.Sprintf("%s = 0;", p.name))
		}
	}
	return []string{strings.Join(parts, " ")}, nil
}

// Reports whether an expression is the named symbol
func isSymbol(e Expr, name string) bool {
	s, ok := e.(Sym)
	return ok && s.Name == name
}

// The names of the eight float stack slots as a block reads them at entry
var fpuSlotNames = [8]string{"fpu.st0", "fpu.st1", "fpu.st2", "fpu.st3", "fpu.st4", "fpu.st5", "fpu.st6", "fpu.st7"}

// Reports whether a float stack leaves every slot holding what the block entered with
func stackUnchanged(fpu []Expr) bool {
	if len(fpu) != len(fpuSlotNames) {
		return false
	}
	for i, e := range fpu {
		if !isSymbol(e, fpuSlotNames[i]) {
			return false
		}
	}
	return true
}

// The live state names the effects of a block change before it ends, read from an entry copy where they do
func liveStateChanged(effects []Effect) map[string]bool {
	out := map[string]bool{}
	for _, e := range effects {
		if e.Kind != "call" {
			continue
		}
		switch e.Fn {
		case cpuFLDCW, cpuFNINIT, cpuFRstor, cpuFXRstor, cpuFNSave:
			out["CW"], out["ST"] = true, true
		case cpuPopFD:
			for _, name := range []string{"FK", "FA", "FB", "FR", "FC", "FFA", "FFB", "DF"} {
				out[name] = true
			}
		case cpuCLD, cpuSTD:
			out["DF"] = true
		}
	}
	return out
}

// The entry trap of a block while the task switched bit is set, a wait only with the monitor bit
func (g *jsGen) notAvailable(b *liftedBlock) string {
	switch b.npx {
	case npxUnit:
		return fmt.Sprintf("if ((CR[0] & 0x%X) !== 0) return trap(%d, 0x%X, 0x%X);\n", uint32(cr0TaskSwitched), vectorDeviceNotAvailable, b.va, b.va)
	case npxWait:
		return fmt.Sprintf("if ((CR[0] & 0x%X) === 0x%X) return trap(%d, 0x%X, 0x%X);\n", uint32(cr0TaskSwitched|cr0MonitorCoprocessor), uint32(cr0TaskSwitched|cr0MonitorCoprocessor), vectorDeviceNotAvailable, b.va, b.va)
	}
	return ""
}

// The copies of live state a block takes at entry, for the names an effect of the block changes before a read
func (g *jsGen) entryCopies() string {
	var out []string
	for _, name := range liveStateNames {
		if g.copied[name] {
			out = append(out, fmt.Sprintf("%s0 = %s", name, name))
		}
	}
	if len(out) == 0 {
		return ""
	}
	return "const " + strings.Join(out, ", ") + ";\n"
}

// The name a live state value is read by, the entry copy where an effect of the block changes it
func (g *jsGen) state(name string) string {
	if g.changed[name] {
		g.copied[name] = true
		return name + "0"
	}
	return name
}

// The seven flag record values as the condition function takes them
func (g *jsGen) flagArguments() string {
	parts := make([]string, 7)
	for i, name := range liveStateNames[:7] {
		parts[i] = g.state(name)
	}
	return strings.Join(parts, ", ")
}

// The expression naming the function the block continues at
func (g *jsGen) terminator(b *liftedBlock) (string, error) {
	t := b.term
	switch t.kind {
	case "jump":
		return g.successor(b.succs[0], t.target), nil
	case "branch":
		return fmt.Sprintf("(%s !== 0 ? %s : %s)", g.hoist(g.ival(t.cond)), g.successor(b.succs[0], t.taken), g.successor(b.succs[1], t.fall)), nil
	case "switch":
		return fmt.Sprintf("switchTo(%s, S_%s, 0x%X)", g.hoist(g.ival(t.index)), g.names[blockKey(b.va, nil)], b.va), nil
	case "jumpind":
		return fmt.Sprintf("go(%s)", g.hoist(g.ival(t.index))), nil
	case "hlt":
		return fmt.Sprintf("halt(0x%X)", t.target), nil
	case "int":
		return fmt.Sprintf("trap(%d, 0x%X, 0x%X)", t.vector, t.target, b.va), nil
	case "iret":
		return fmt.Sprintf("iret(0x%X)", b.va), nil
	}
	return "", fmt.Errorf("block ends with %s", t.kind)
}

// The function a static successor runs, a pending interrupt delivered first, through the hook table where the
// target is a routine's entry and through the dispatcher where the block lies outside this model
func (g *jsGen) successor(key string, va uint32) string {
	name, ok := g.names[key]
	if !ok {
		return fmt.Sprintf("go(0x%X)", va)
	}
	if g.routines[va] {
		return fmt.Sprintf("enter(0x%X, %s)", va, name)
	}
	return fmt.Sprintf("step(0x%X, %s)", va, name)
}

// Emits the loads recorded at an epoch, each read before the effect that follows them
func (g *jsGen) preloads(epoch int) {
	for _, l := range g.loadsAt[epoch] {
		g.preload(l)
	}
}

func (g *jsGen) preload(l Expr) {
	if ld, ok := l.(Load); ok && ld.Size == 8 {
		g.loadDouble(ld)
		return
	}
	g.ival(l)
}

// Emits one effect
func (g *jsGen) effect(e Effect) {
	switch e.Kind {
	case "store":
		addr := fmt.Sprintf("0x%X", uint32(e.Off))
		if e.Addr != nil {
			addr = g.ival(e.Addr)
		} else if e.Base != "" {
			g.fail("store to %s", e.Base)
			return
		}
		if e.Size == 8 {
			g.line("stf64(%s, %s);", addr, g.fval(e.Value))
			return
		}
		if e.Size > 4 {
			g.fail("store of %d bytes", e.Size)
			return
		}
		g.line("st%d(%s, %s);", e.Size*8, addr, g.ival(e.Value))
	case "copy":
		g.line("memcopy(%s, %s, %s, %d, %s);", g.ival(e.Addr), g.ival(e.Args[0]), g.ival(e.Value), e.Size, g.ival(e.Args[1]))
	case "fill":
		g.line("memfill(%s, %s, %s, %d, %s);", g.ival(e.Addr), g.ival(e.Value), g.ival(e.Args[0]), e.Size, g.ival(e.Args[1]))
	case "call":
		args := make([]string, len(e.Args))
		for i, a := range e.Args {
			args[i] = g.ival(a)
		}
		name := fmt.Sprintf("c%d", e.Seq)
		g.results[e.Seq] = name
		switch {
		case e.Fn == cpuFRstorST || e.Fn == cpuFXRstorST:
			g.line("const %s = 0;", name)
		case strings.HasPrefix(e.Fn, cpuCPUID):
			g.line("const %s = cpu_cpuid(%s, %s, 0x%X);", name, strconv.Quote(strings.TrimPrefix(e.Fn, cpuCPUID)), strings.Join(args, ", "), e.VA)
		case strings.HasPrefix(e.Fn, "cpu_") || e.Fn == portIn || e.Fn == portOut:
			g.line("const %s = %s(%s%s0x%X);", name, e.Fn, strings.Join(args, ", "), comma(len(args)), e.VA)
		default:
			g.line("const %s = libcall(%s, [%s], 0x%X);", name, strconv.Quote(e.Fn), strings.Join(args, ", "), e.VA)
		}
	default:
		g.fail("%s effect", e.Kind)
	}
}

func comma(n int) string {
	if n > 0 {
		return ", "
	}
	return ""
}

func (g *jsGen) line(format string, args ...any) {
	fmt.Fprintf(&g.body, format+"\n", args...)
}

// A value the page cannot compute faults when it is reached, naming the block
func (g *jsGen) fault(format string, args ...any) string {
	va := uint32(0)
	if g.block != nil {
		va = g.block.va
	}
	return fmt.Sprintf("fault(%s, 0x%X)", strconv.Quote(fmt.Sprintf(format, args...)), va)
}

// An effect the page cannot perform faults when it is reached
func (g *jsGen) fail(format string, args ...any) {
	g.line("%s;", g.fault(format, args...))
}

// Names a fresh temporary holding an expression
func (g *jsGen) temp(expr string) string {
	g.temps++
	name := fmt.Sprintf("t%d", g.temps)
	g.line("const %s = %s;", name, expr)
	return name
}

// A value computed into a temporary before any assignment of the block, unless it is a literal or a name already held
func (g *jsGen) hoist(expr string) string {
	if jsAtom(expr) {
		return expr
	}
	return g.temp(expr)
}

// Reports whether an expression is a literal, a temporary or a call result, which no assignment of the block changes
func jsAtom(expr string) bool {
	switch expr {
	case "NaN", "Infinity", "(-Infinity)":
		return true
	}
	if strings.HasPrefix(expr, "0x") {
		return len(expr) > 2 && strings.Trim(expr[2:], "0123456789ABCDEF") == ""
	}
	if len(expr) > 1 && (expr[0] == 't' || expr[0] == 'c') && strings.Trim(expr[1:], "0123456789") == "" {
		return true
	}
	n := strings.TrimSuffix(strings.TrimPrefix(expr, "("), ")")
	return len(n) > 0 && (n[0] == '-' || n[0] >= '0' && n[0] <= '9') && strings.ContainsAny(n, "0123456789") && strings.Trim(n, "0123456789.eE+-") == ""
}

// Counts the references each node of the expressions receives, descending into a node on its first reference
func (g *jsGen) countRefs(exprs []Expr) {
	var visit func(e Expr)
	visit = func(e Expr) {
		if e == nil {
			return
		}
		k := keyOf(e)
		g.refs[k]++
		if g.refs[k] == 1 {
			eachChild(e, visit)
		}
	}
	for _, e := range exprs {
		visit(e)
	}
}

// Reads an eight byte load as the double it holds, once per block
func (g *jsGen) loadDouble(ld Load) string {
	k := keyOf(Expr(ld))
	if name, ok := g.memoF[k]; ok {
		return name
	}
	addr := fmt.Sprintf("0x%X", uint32(ld.Off))
	if ld.Addr != nil {
		addr = g.ival(ld.Addr)
	} else if ld.Base != "" {
		return g.fault("load from %s", ld.Base)
	}
	name := g.temp(fmt.Sprintf("ldf64(%s)", addr))
	g.memoF[k] = name
	return name
}

// The integer value of an expression, as the processor's evaluator reads it, a load named where it is read, a node
// shared by several expressions named once and every other node written where it is used
func (g *jsGen) ival(e Expr) string {
	if e == nil {
		return g.fault("undefined value")
	}
	switch v := e.(type) {
	case IConst:
		return fmt.Sprintf("0x%X", v.V)
	case Sym:
		if name, ok := g.simpleSymbol(v.Name); ok {
			return name
		}
	}
	k := keyOf(e)
	if name, ok := g.memoI[k]; ok {
		return name
	}
	expr := g.inode(e)
	if _, isLoad := e.(Load); !isLoad && g.refs[k] < 2 {
		return expr
	}
	name := g.temp(expr)
	g.memoI[k] = name
	return name
}

// The floating point value of an expression
func (g *jsGen) fval(e Expr) string {
	if e == nil {
		return g.fault("undefined float value")
	}
	switch v := e.(type) {
	case FConst:
		return jsFloat(v.V)
	case Sym:
		if name, ok := g.simpleFloatSymbol(v.Name); ok {
			return name
		}
	}
	k := keyOf(e)
	if name, ok := g.memoF[k]; ok {
		return name
	}
	expr := g.fnode(e)
	if g.refs[k] < 2 {
		return expr
	}
	name := g.temp(expr)
	g.memoF[k] = name
	return name
}

func jsFloat(v float64) string {
	switch {
	case math.IsNaN(v):
		return "NaN"
	case math.IsInf(v, 1):
		return "Infinity"
	case math.IsInf(v, -1):
		return "(-Infinity)"
	}
	s := strconv.FormatFloat(v, 'g', -1, 64)
	if v < 0 {
		return "(" + s + ")"
	}
	return s
}

func (g *jsGen) inode(e Expr) string {
	if isFloatExpr(e) {
		return fmt.Sprintf("fbits(%s)", g.fval(e))
	}
	switch v := e.(type) {
	case FCmpSet:
		return fmt.Sprintf("fcmpset(%s, %s, %d)", g.fval(v.A), g.fval(v.B), v.Set)
	case Sym:
		return g.symbol(v.Name)
	case Ptr:
		if v.Base != "" {
			return g.fault("address in %s", v.Base)
		}
		return fmt.Sprintf("0x%X", uint32(v.Off))
	case Load:
		if v.Size > 4 {
			return g.fault("load of %d bytes", v.Size)
		}
		addr := fmt.Sprintf("0x%X", uint32(v.Off))
		if v.Addr != nil {
			addr = g.ival(v.Addr)
		} else if v.Base != "" {
			return g.fault("load from %s", v.Base)
		}
		return fmt.Sprintf("ld%d(%s)", v.Size*8, addr)
	case CallResult:
		name, ok := g.results[v.Seq]
		if !ok {
			return g.fault("result of %s before it ran", v.Fn)
		}
		return name
	case Bin:
		return g.ibin(v)
	case Un:
		return g.iun(v)
	case Cond:
		ops := map[string]string{"eq": "===", "ne": "!==", "ult": "<", "ule": "<=", "ugt": ">", "uge": ">="}
		if op, ok := ops[v.Op]; ok {
			return fmt.Sprintf("(%s %s %s ? 1 : 0)", g.ival(v.A), op, g.ival(v.B))
		}
		signed := map[string]string{"lt": "<", "le": "<=", "gt": ">", "ge": ">="}
		if op, ok := signed[v.Op]; ok {
			return fmt.Sprintf("((%s | 0) %s (%s | 0) ? 1 : 0)", g.ival(v.A), op, g.ival(v.B))
		}
		return g.fault("comparison %s", v.Op)
	case Not:
		return fmt.Sprintf("(%s === 0 ? 1 : 0)", g.ival(v.A))
	case Sel:
		return fmt.Sprintf("(%s !== 0 ? %s : %s)", g.ival(v.C), g.ival(v.A), g.ival(v.B))
	}
	return g.fault("%T value", e)
}

func (g *jsGen) ibin(v Bin) string {
	if strings.HasPrefix(v.Op, "udiv64") || strings.HasPrefix(v.Op, "idiv64") {
		pair, ok := v.A.(Bin)
		if !ok || pair.Op != "hilo" {
			return g.fault("%s of a dividend that is no hilo pair", v.Op)
		}
		return fmt.Sprintf("%s(%s, %s, %s)", v.Op, g.ival(pair.A), g.ival(pair.B), g.ival(v.B))
	}
	switch v.Op {
	case "hilo":
		return g.fault("a 64 bit dividend outside a division")
	case "f2icw":
		return fmt.Sprintf("f2icw(%s, %s)", g.fval(v.A), g.ival(v.B))
	}
	a, b := g.ival(v.A), g.ival(v.B)
	switch v.Op {
	case "add":
		return fmt.Sprintf("((%s + %s) >>> 0)", a, b)
	case "sub":
		return fmt.Sprintf("((%s - %s) >>> 0)", a, b)
	case "mul":
		return fmt.Sprintf("(Math.imul(%s, %s) >>> 0)", a, b)
	case "and":
		return fmt.Sprintf("((%s & %s) >>> 0)", a, b)
	case "or":
		return fmt.Sprintf("((%s | %s) >>> 0)", a, b)
	case "xor":
		return fmt.Sprintf("((%s ^ %s) >>> 0)", a, b)
	case "shl":
		return fmt.Sprintf("((%s << (%s & 31)) >>> 0)", a, b)
	case "shr":
		return fmt.Sprintf("(%s >>> (%s & 31))", a, b)
	case "sar":
		return fmt.Sprintf("((%s >> (%s & 31)) >>> 0)", a, b)
	case "udiv", "udiv_rem", "idiv", "idiv_rem", "mulhi", "imulhi":
		return fmt.Sprintf("%s(%s, %s)", v.Op, a, b)
	}
	return g.fault("operation %s", v.Op)
}

func (g *jsGen) iun(v Un) string {
	switch v.Op {
	case "f2bits":
		return fmt.Sprintf("fbits(%s)", g.fval(v.A))
	case "f2i":
		return fmt.Sprintf("f2i(%s)", g.fval(v.A))
	case "f2i_rn":
		return fmt.Sprintf("f2irn(%s)", g.fval(v.A))
	}
	a := g.ival(v.A)
	switch v.Op {
	case "zx8":
		return fmt.Sprintf("(%s & 0xFF)", a)
	case "zx16":
		return fmt.Sprintf("(%s & 0xFFFF)", a)
	case "parity":
		return fmt.Sprintf("parity8(%s)", a)
	case "sx8":
		return fmt.Sprintf("(((%s << 24) >> 24) >>> 0)", a)
	case "sx16":
		return fmt.Sprintf("(((%s << 16) >> 16) >>> 0)", a)
	case "neg":
		return fmt.Sprintf("((-%s) >>> 0)", a)
	case "not":
		return fmt.Sprintf("((~%s) >>> 0)", a)
	case "bsf", "bsr":
		return fmt.Sprintf("%s(%s)", v.Op, a)
	}
	return g.fault("operation %s", v.Op)
}

// The name a runtime input is read by where that is one variable or element, which costs nothing to read again
func (g *jsGen) simpleSymbol(name string) (string, bool) {
	switch {
	case strings.HasPrefix(name, "reg."):
		r := name[4:]
		if slices.Contains(registerNames[:], r) {
			return r, true
		}
		if slices.Contains(segNames[:], r) {
			return "SEG_" + r, true
		}
	case name == "flags.a":
		return g.state("FA"), true
	case name == "flags.b":
		return g.state("FB"), true
	case name == "flags.r":
		return g.state("FR"), true
	case name == "flags.cf":
		return g.state("FC"), true
	case name == "fpu.cw":
		return g.state("CW"), true
	case strings.HasPrefix(name, "segbase."):
		return "BASE_" + name[8:], true
	case strings.HasPrefix(name, "mm"):
		var reg, lane int
		if n, _ := fmt.Sscanf(name, "mm%d.%d", &reg, &lane); n == 2 {
			return fmt.Sprintf("MMX[%d]", reg*2+lane), true
		}
	case name == "self":
		return "SELF", true
	}
	return "", false
}

// The value a runtime input names
func (g *jsGen) symbol(name string) string {
	if simple, ok := g.simpleSymbol(name); ok {
		return simple
	}
	switch {
	case strings.HasPrefix(name, "dfstep"):
		return fmt.Sprintf("(%s ? %s : %s)", g.state("DF"), jsInt(-atoi(name[6:])), jsInt(atoi(name[6:])))
	case name == "fsw":
		return "fswRegister(EAX)"
	case strings.HasPrefix(name, "cond."):
		code, ok := jsConditionCodes[name[5:]]
		if ok {
			return fmt.Sprintf("condOf(%d, %s)", code, g.flagArguments())
		}
	}
	return g.fault("input %s", name)
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

func jsInt(n int) string {
	return fmt.Sprintf("0x%X", uint32(int32(n)))
}

// The name a float input is read by where that is one variable, element or stack slot
func (g *jsGen) simpleFloatSymbol(name string) (string, bool) {
	switch {
	case name == "fpu.empty":
		return "NaN", true
	case strings.HasPrefix(name, "fpu.st"):
		i := atoi(name[6:])
		if i < 0 {
			return g.fault("float register %s", name), true
		}
		return fmt.Sprintf("stOf(%s, %d)", g.state("ST"), i), true
	case strings.HasPrefix(name, "xmm"):
		var reg, lane int
		if n, _ := fmt.Sscanf(name, "xmm%d.%d", &reg, &lane); n == 2 {
			return fmt.Sprintf("XMM[%d]", reg*4+lane), true
		}
	case name == "fcmp.a":
		return "FCA", true
	case name == "fcmp.b":
		return "FCB", true
	case name == "flags.fsw_a":
		return g.state("FFA"), true
	case name == "flags.fsw_b":
		return g.state("FFB"), true
	}
	return "", false
}

func (g *jsGen) fnode(e Expr) string {
	switch v := e.(type) {
	case Sym:
		if name, ok := g.simpleFloatSymbol(v.Name); ok {
			return name
		}
		return fmt.Sprintf("fromBits(%s)", g.ival(e))
	case Sel:
		return fmt.Sprintf("(%s !== 0 ? %s : %s)", g.ival(v.C), g.fval(v.A), g.fval(v.B))
	case CallResult:
		if v.Fn == cpuFRstorST || v.Fn == cpuFXRstorST {
			if len(v.Args) < 2 {
				return g.fault("a restored register without a save area")
			}
			return fmt.Sprintf("%s(%s, %s)", v.Fn, g.ival(v.Args[0]), g.ival(v.Args[1]))
		}
	case Bin:
		if floatBinOps[v.Op] {
			a, b := g.fval(v.A), g.fval(v.B)
			switch v.Op {
			case "fadd":
				return fmt.Sprintf("(%s + %s)", a, b)
			case "fsub":
				return fmt.Sprintf("(%s - %s)", a, b)
			case "fmul":
				return fmt.Sprintf("(%s * %s)", a, b)
			case "fdiv":
				return fmt.Sprintf("(%s / %s)", a, b)
			case "fmin":
				return fmt.Sprintf("Math.min(%s, %s)", a, b)
			case "fmax":
				return fmt.Sprintf("Math.max(%s, %s)", a, b)
			case "fmod":
				return fmt.Sprintf("(%s %% %s)", a, b)
			case "fpow":
				return fmt.Sprintf("Math.pow(%s, %s)", a, b)
			case "fatan2":
				return fmt.Sprintf("Math.atan2(%s, %s)", a, b)
			case "frem":
				return fmt.Sprintf("frem(%s, %s)", a, b)
			}
			return g.fault("float operation %s", v.Op)
		}
	case Un:
		switch v.Op {
		case "f64":
			if ld, ok := v.A.(Load); ok && ld.Size == 8 {
				return g.loadDouble(ld)
			}
			return g.fval(v.A)
		case "bits2f":
			return fmt.Sprintf("fromBits(%s)", g.ival(v.A))
		case "i2f":
			if pair, ok := v.A.(Bin); ok && pair.Op == "hilo" {
				return fmt.Sprintf("i64f(%s, %s)", g.ival(pair.A), g.ival(pair.B))
			}
			return fmt.Sprintf("(%s | 0)", g.ival(v.A))
		case "bits2d":
			if pair, ok := v.A.(Bin); ok && pair.Op == "hilo" {
				return fmt.Sprintf("bits2d(%s, %s)", g.ival(pair.A), g.ival(pair.B))
			}
			if sym, ok := v.A.(Sym); ok && strings.HasPrefix(sym.Name, "fpu.") {
				return g.fval(v.A)
			}
			return g.fault("a double built from %s", ExprString(v.A))
		}
		a := g.fval(v.A)
		switch v.Op {
		case "f32":
			return fmt.Sprintf("Math.fround(%s)", a)
		case "fneg":
			return fmt.Sprintf("(-%s)", a)
		case "fabs":
			return fmt.Sprintf("Math.abs(%s)", a)
		case "fsin":
			return fmt.Sprintf("Math.sin(%s)", a)
		case "fcos":
			return fmt.Sprintf("Math.cos(%s)", a)
		case "fsqrt":
			return fmt.Sprintf("Math.sqrt(%s)", a)
		case "ftan":
			return fmt.Sprintf("Math.tan(%s)", a)
		case "ffloor":
			return fmt.Sprintf("Math.floor(%s)", a)
		case "fceil":
			return fmt.Sprintf("Math.ceil(%s)", a)
		case "fexp":
			return fmt.Sprintf("Math.exp(%s)", a)
		case "flog":
			return fmt.Sprintf("Math.log(%s)", a)
		case "flog10":
			return fmt.Sprintf("Math.log10(%s)", a)
		case "flog2":
			return fmt.Sprintf("Math.log2(%s)", a)
		case "ftrunc":
			return fmt.Sprintf("Math.trunc(%s)", a)
		case "fround":
			return fmt.Sprintf("roundEven(%s)", a)
		case "fatan":
			return fmt.Sprintf("Math.atan(%s)", a)
		case "fasin":
			return fmt.Sprintf("Math.asin(%s)", a)
		case "facos":
			return fmt.Sprintf("Math.acos(%s)", a)
		case "fsinh":
			return fmt.Sprintf("Math.sinh(%s)", a)
		case "fcosh":
			return fmt.Sprintf("Math.cosh(%s)", a)
		case "ftanh":
			return fmt.Sprintf("Math.tanh(%s)", a)
		}
		return g.fault("float operation %s", v.Op)
	case Elem:
		return g.fault("vector element %d", v.Index)
	}
	return fmt.Sprintf("fromBits(%s)", g.ival(e))
}

// BlockVAs lists the addresses of every plain block, for tests reading the generated functions by address
func (mm *MachineModel) BlockVAs() []uint32 {
	var out []uint32
	for key, b := range mm.blocks {
		if !strings.Contains(key, "@") {
			out = append(out, b.va)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
