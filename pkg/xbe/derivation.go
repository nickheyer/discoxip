package xbe

import (
	"fmt"
	"sort"

	"golang.org/x/arch/x86/x86asm"
)

// Paths runs a routine from its entry with every input symbolic, callees that exhaust their budget made opaque, an
// instruction the executor cannot express failing the run
func (ex *Executor) Paths(entry uint32, retries int) (paths []Path, partial []Effect, err error) {
	defer func() {
		if r := recover(); r != nil {
			paths, partial, err = nil, nil, fmt.Errorf("symexec: %08X: %v", entry, r)
		}
	}()
	paths, partial, _, err = ex.RunWithRetries(entry, func(*execState) {}, retries)
	return paths, partial, err
}

// StoreSite is one block storing at a fixed address, with the value it stores
type StoreSite struct {
	VA    uint32
	Value Expr
}

// FixedAddress reports the address a load or store names outright, by offset under an empty base or constant pointer
func FixedAddress(base string, addr Expr, off int32) (uint32, bool) {
	if addr != nil {
		if base != "" && base != "?" {
			return 0, false
		}
		return isConst(addr)
	}
	if base != "" {
		return 0, false
	}
	return uint32(off), true
}

// StoresAt collects the blocks storing at a fixed address with the size given
func (mm *MachineModel) StoresAt(addr uint32, size int) []StoreSite {
	var out []StoreSite
	for _, b := range mm.blocks {
		for _, e := range b.effects {
			if e.Kind != "store" || e.Size != size {
				continue
			}
			if at, ok := FixedAddress(e.Base, e.Addr, e.Off); ok && at == addr {
				out = append(out, StoreSite{VA: b.va, Value: e.Value})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].VA < out[j].VA })
	return out
}

// ConstOf reports the value of a constant expression
func ConstOf(e Expr) (uint32, bool) {
	return isConst(e)
}

// MapExpr rebuilds an expression top down, a mapped node replacing its subtree
func MapExpr(e Expr, f func(Expr) (Expr, bool)) Expr {
	return mapExpr(e, f)
}

// FoldExpr rebuilds an expression bottom up, f replacing each node once its children were rebuilt, so one pass
// folds a tree of operations between constants down to its value
func FoldExpr(e Expr, f func(Expr) (Expr, bool)) Expr {
	return NewFoldMemo().Fold(e, f)
}

// SameExpr reports whether two expressions are structurally identical
func SameExpr(a, b Expr) bool {
	return sameExpr(a, b)
}

// EqualExprs reports whether two expressions are structurally identical however large they are
func EqualExprs(a, b Expr) bool {
	return equalExprs(a, b)
}

// WalkExpr calls visit on every node of an expression once
func WalkExpr(e Expr, visit func(Expr)) {
	walkExpr(e, map[exprKey]bool{}, visit)
}

// EachChild calls f on each node an expression is built from directly
func EachChild(e Expr, f func(Expr)) {
	eachChild(e, f)
}

// ExprKey is the identity of a node, the same for two references to one node, for memoizing walks over shared trees
type ExprKey struct{ k exprKey }

// KeyOf is the identity of a node
func KeyOf(e Expr) ExprKey {
	return ExprKey{keyOf(e)}
}

// RegisterInputs names the registers an unnamed routine reads before writing, in the order its callers fill them
func (d *Disassembly) RegisterInputs(va uint32) []string {
	var out []string
	for _, r := range d.registerInputs(va) {
		out = append(out, "reg."+r.String())
	}
	return out
}

// StackInputs counts the stack argument slots a routine reads above its return address
func (d *Disassembly) StackInputs(va uint32) int {
	fn := d.Functions[va]
	if fn == nil {
		return 0
	}
	return d.stackInputs(fn)
}

// FunctionAt reports the routine whose instructions include an address, the nearest entry below it where
// routines overlap
func (d *Disassembly) FunctionAt(va uint32) *Function {
	entries, span := d.sortedEntries()
	for i := sort.Search(len(entries), func(i int) bool { return entries[i] > va }) - 1; i >= 0 && entries[i]+span > va; i-- {
		if fn := d.Functions[entries[i]]; va < fn.EntryVA+uint32(fn.Size) {
			return fn
		}
	}
	return nil
}

// The routine entries in address order with the longest routine's size, rebuilt once routines were added
func (d *Disassembly) sortedEntries() ([]uint32, uint32) {
	if d.entries == nil || d.entriesOf != len(d.Functions) {
		d.entries, d.entrySpan, d.entriesOf = make([]uint32, 0, len(d.Functions)), 0, len(d.Functions)
		for va, fn := range d.Functions {
			d.entries = append(d.entries, va)
			d.entrySpan = max(d.entrySpan, uint32(fn.Size))
		}
		sort.Slice(d.entries, func(i, j int) bool { return d.entries[i] < d.entries[j] })
	}
	return d.entries, d.entrySpan
}

// SectionOf reports the section holding an address
func (d *Disassembly) SectionOf(va uint32) *Section {
	return d.sectionOf(va)
}

// RoutineEffects collects the effects of a routine's own blocks in address order
func (mm *MachineModel) RoutineEffects(entry uint32) []Effect {
	blocks, _ := mm.RoutineBlocks(entry)
	sort.Slice(blocks, func(i, j int) bool { return blocks[i].va < blocks[j].va })
	var out []Effect
	for _, b := range blocks {
		out = append(out, b.effects...)
	}
	return out
}

// RoutineCallees reports the routines a routine's blocks call
func (mm *MachineModel) RoutineCallees(entry uint32) []uint32 {
	_, callees := mm.RoutineBlocks(entry)
	return callees
}

// StoredValues collects every value the model's blocks store at a fixed address with the size given
func (mm *MachineModel) StoredValues(addr uint32, size int) []Expr {
	var out []Expr
	for _, site := range mm.StoresAt(addr, size) {
		out = append(out, site.Value)
	}
	return out
}

// CallTargets reports every address a call instruction of the image reaches directly
func (d *Disassembly) CallTargets() map[uint32]bool {
	out := map[uint32]bool{}
	for _, in := range d.InsnByVA {
		if in.Inst.Op == x86asm.CALL {
			if t := resolveTarget(in); t != 0 {
				out[t] = true
			}
		}
	}
	return out
}

// BlockInfo is one lifted block as a derivation reads it, its effects, exit registers and how it ends
type BlockInfo struct {
	VA      uint32
	Effects []Effect
	Cond    Expr
	Kind    string
	Target  uint32
	Ret     uint32
	Taken   uint32
	Fall    uint32
	Targets []uint32
	Regs    map[string]Expr
	ESP     Expr
}

// EachBlock visits every lifted block in address order
func (mm *MachineModel) EachBlock(f func(b BlockInfo)) {
	keys := make([]string, 0, len(mm.blocks))
	for key := range mm.blocks {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		b := mm.blocks[key]
		info := BlockInfo{VA: b.va, Effects: b.effects, Regs: map[string]Expr{}, ESP: b.spExpr}
		if t := b.term; t != nil {
			info.Cond, info.Kind, info.Target, info.Ret, info.Taken, info.Fall, info.Targets = t.cond, t.kind, t.target, t.ret, t.taken, t.fall, t.targets
		}
		for i, v := range b.regs {
			if v != nil && i != 4 {
				info.Regs["reg."+registerNames[i]] = v
			}
		}
		f(info)
	}
}

// RoutineBlockVAs reports the addresses of a routine's own blocks
func (mm *MachineModel) RoutineBlockVAs(entry uint32) []uint32 {
	blocks, _ := mm.RoutineBlocks(entry)
	var out []uint32
	for _, b := range blocks {
		out = append(out, b.va)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// Cleanup reports the stack bytes a routine's returns remove beyond the return address
func (d *Disassembly) Cleanup(va uint32) int {
	fn := d.Functions[va]
	if fn == nil {
		return 0
	}
	return d.cleanupOf(fn)
}

// LiftRoutine lifts the blocks a routine reaches into the model where the model lacks them
func (mm *MachineModel) LiftRoutine(d *Disassembly, entry uint32) error {
	if _, lifted := mm.blocks[blockKey(entry, nil)]; lifted {
		return nil
	}
	more, err := LiftMachineRooted(d, []uint32{entry}, mm.Exprs)
	if err != nil {
		return err
	}
	return mm.Absorb(more)
}
