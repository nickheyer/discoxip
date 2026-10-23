package xbe

import (
	"fmt"
	"strings"

	"golang.org/x/arch/x86/x86asm"
)

// Names of the XDK routines whose bodies the link time optimizer expands into the dashboard's own code
const (
	createVertexBuffer = "D3DDevice_CreateVertexBuffer2"
	lockVertexBuffer   = "D3DVertexBuffer_Lock2"
	setTextureCall     = "D3DDevice_SetTexture"
	requestSpace       = "D3D::MakeRequestedSpace"
	blockOnTime        = "D3D_BlockOnTime"
	destroyResource    = "D3D::DestroyResource"
	memAlloc           = "XMemAlloc"
	memFree            = "XMemFree"
)

// A command token carries its data word count above the method, so the smallest one is a single word
const commandTokenFloor = 1 << 18

// Effects of an inlined body the scheduler may place between a pushed command and the advance past it
const pushWindow = 8

// textureTokens is the command token one library routine pushes for texture stage zero, with its stage stride
type textureTokens struct {
	va     uint32
	base   uint32
	stride uint32
	slot   int32 // device field the routine stores the binding of stage zero in
	status int32 // device field the routine marks stage zero in when it binds nothing
}

// resourceCalls holds what the inlined XDK resource routines do, read out of the library copies of them
type resourceCalls struct {
	device      uint32 // global holding the device whose command space the routines push into
	headerSize  uint32 // bytes the creation allocates for the resource header
	headerAttrs uint32 // allocation attributes of the resource header
	dataAttrs   uint32 // allocation attributes of the resource data
	common      uint32 // the Common word the creation writes into the header
	dataMask    uint32 // the mask the creation applies to the data address
	alias       uint32 // the bit the lock sets to reach the data uncached
	flush       uint32 // the command token the lock pushes
	textures    []textureTokens
}

// Reads the entry of the library function carrying a name, or zero when the image has none
func (d *Disassembly) symbolVA(name string) uint32 {
	for _, va := range d.functionVAs() {
		if fn := d.Functions[va]; fn.Symbol != nil && fn.Symbol.Name == name {
			return va
		}
	}
	return 0
}

// Reads the resource routines the optimizer inlines out of the library copies the image still carries
func (d *Disassembly) scanResourceCalls() {
	create, lock := d.symbolVA(createVertexBuffer), d.symbolVA(lockVertexBuffer)
	if create == 0 || lock == 0 {
		return
	}
	r := &resourceCalls{}
	if !d.readCreationBody(d.Functions[create], r) || !d.readLockBody(d.Functions[lock], r) {
		return
	}
	for _, va := range d.functionVAs() {
		if !d.inLibrary(va) {
			continue
		}
		// The setter binds a resource into a per stage slot of the device, releasing the one the slot held
		fn := d.Functions[va]
		if len(fn.CalledBy) == 0 || !d.reaches(va, destroyResource, 0) {
			continue
		}
		if t, ok := d.stageTokens(fn, r.device); ok {
			t.va = va
			r.textures = append(r.textures, t)
			d.nameByUsage(va, setTextureCall)
		}
	}
	d.resources = r
}

// Reads the allocation sizes, attributes and header words of the vertex buffer creation
func (d *Disassembly) readCreationBody(fn *Function, r *resourceCalls) bool {
	var pushes []uint32
	allocs := 0
	for i := range fn.Instructions {
		in := &fn.Instructions[i]
		switch in.Inst.Op {
		case x86asm.PUSH:
			if v, ok := in.Inst.Args[0].(x86asm.Imm); ok {
				pushes = append(pushes, uint32(v))
			} else {
				pushes = append(pushes, 0)
			}
		case x86asm.CALL:
			callee := d.Functions[resolveTarget(in)]
			if callee == nil || callee.Symbol == nil || callee.Symbol.Name != memAlloc || len(pushes) < 2 {
				pushes = nil
				continue
			}
			attrs, size := pushes[len(pushes)-2], pushes[len(pushes)-1]
			if allocs == 0 {
				r.headerAttrs, r.headerSize = attrs, size
			} else if allocs == 1 {
				r.dataAttrs = attrs
			}
			allocs++
			pushes = nil
		case x86asm.AND:
			if v, ok := in.Inst.Args[1].(x86asm.Imm); ok && allocs == 2 {
				r.dataMask = uint32(v)
			}
		case x86asm.MOV:
			m, isMem := in.Inst.Args[0].(x86asm.Mem)
			v, isImm := in.Inst.Args[1].(x86asm.Imm)
			if isMem && isImm && m.Base != 0 && m.Index == 0 && in.Inst.MemBytes == 4 {
				r.common = uint32(v)
			}
		}
	}
	return allocs == 2 && r.headerSize != 0 && r.headerAttrs != 0 && r.dataAttrs != 0 && r.common != 0 && r.dataMask != 0
}

// Reads the device global, the command token and the uncached alias bit of the vertex buffer lock
func (d *Disassembly) readLockBody(fn *Function, r *resourceCalls) bool {
	for i := range fn.Instructions {
		in := &fn.Instructions[i]
		switch in.Inst.Op {
		case x86asm.MOV:
			if m, ok := in.Inst.Args[1].(x86asm.Mem); ok && m.Base == 0 && m.Index == 0 && in.Inst.MemBytes == 4 && r.device == 0 {
				r.device = uint32(m.Disp)
			}
			m, isMem := in.Inst.Args[0].(x86asm.Mem)
			v, isImm := in.Inst.Args[1].(x86asm.Imm)
			if isMem && isImm && m.Base != 0 && m.Index == 0 && uint32(v) >= commandTokenFloor && r.flush == 0 {
				r.flush = uint32(v)
			}
		case x86asm.OR:
			if v, ok := in.Inst.Args[1].(x86asm.Imm); ok && uint32(v)&0x80000000 != 0 {
				r.alias = uint32(v)
			}
		}
	}
	return r.device != 0 && r.flush != 0 && r.alias != 0
}

// Reads the token a routine builds from a stage, with the device field it binds into and the one it marks
func (d *Disassembly) stageTokens(fn *Function, device uint32) (textureTokens, bool) {
	sub := NewExecutorShared(d.baseExecutor())
	sub.PathLimit = 64
	sub.StepLimit = 20000
	for _, t := range fn.CallTargets {
		sub.Opaque[t] = true
	}
	paths, partial, _, _ := sub.RunWithRetries(fn.EntryVA, func(s *execState) { sub.seedArgs(s, fn.EntryVA, "in", 4) }, 0)
	var effects []Effect
	for _, p := range paths {
		effects = append(effects, p.Effects...)
	}
	effects = append(effects, partial...)
	slot, status := int32(0), int32(0)
	for _, e := range effects {
		if e.Kind != "store" || e.Size != 4 || e.Addr == nil {
			continue
		}
		field, perStage := stageArrayField(e.Addr, device)
		if !perStage {
			continue
		}
		if sym, isSym := e.Value.(Sym); isSym && strings.HasPrefix(sym.Name, "in") {
			slot = field
		} else if _, isConst := isConst(e.Value); isConst {
			status = field
		}
	}
	if slot == 0 {
		return textureTokens{}, false
	}
	shift := uint(0)
	for i := range fn.Instructions {
		in := &fn.Instructions[i]
		switch in.Inst.Op {
		case x86asm.SHL:
			if v, ok := in.Inst.Args[1].(x86asm.Imm); ok {
				shift = uint(v)
			}
		case x86asm.ADD:
			if v, ok := in.Inst.Args[1].(x86asm.Imm); ok && uint32(v) >= commandTokenFloor && shift != 0 && shift < 24 {
				return textureTokens{base: uint32(v), stride: 1 << shift, slot: slot, status: status}, true
			}
		}
	}
	return textureTokens{}, false
}

// The constant field an address names in the device, when it also steps by a word per stage
func stageArrayField(addr Expr, device uint32) (int32, bool) {
	var terms []Expr
	var flatten func(e Expr)
	flatten = func(e Expr) {
		if b, ok := e.(Bin); ok && b.Op == "add" {
			flatten(b.A)
			flatten(b.B)
			return
		}
		terms = append(terms, e)
	}
	flatten(addr)
	base, field, stepped := false, int32(0), false
	for _, t := range terms {
		switch v := t.(type) {
		case Load:
			if v.Addr == nil && v.Base == "" && uint32(v.Off) == device {
				base = true
			}
		case IConst:
			field = int32(v.V)
		case Bin:
			if c, ok := isConst(v.B); ok && ((v.Op == "mul" && c == 4) || (v.Op == "shl" && c == 2)) {
				stepped = true
			}
		}
	}
	return field, base && stepped && field != 0
}

// Reads the texture stage a command token binds, confirmed by the device field the same body writes
func (r *resourceCalls) textureStage(p Path, token uint32) (uint32, bool, error) {
	stage, found := uint32(0), false
	for _, t := range r.textures {
		if token < t.base || (token-t.base)%t.stride != 0 || (token-t.base)/t.stride >= textureStages || token>>13 != t.base>>13 {
			continue
		}
		s := (token - t.base) / t.stride
		if !r.bindsStage(p, t.slot+4*int32(s)) {
			continue
		}
		if found && s != stage {
			return 0, false, fmt.Errorf("xbe: command %08X binds texture stage %d and %d in different library routines", token, stage, s)
		}
		stage, found = s, true
	}
	return stage, found, nil
}

// Reports whether a path stores the binding of a stage into the device field holding it
func (r *resourceCalls) bindsStage(p Path, slot int32) bool {
	want := Bin{"add", Load{Base: "", Off: int32(r.device), Size: 4}, IConst{uint32(slot)}}
	for _, e := range p.Effects {
		if e.Kind == "store" && e.Addr != nil && sameExpr(e.Addr, want) {
			return true
		}
	}
	return false
}

// The device field a store goes through the contents of, nil for a store not through a device held pointer
func (r *resourceCalls) deviceHeld(e Expr) Expr {
	switch v := e.(type) {
	case Load:
		if v.Addr != nil && r.deviceField(v.Addr) {
			return v.Addr
		}
	case Bin:
		if v.Op == "add" {
			if f := r.deviceHeld(v.A); f != nil {
				return f
			}
			return r.deviceHeld(v.B)
		}
	}
	return nil
}

// The setter and stage whose binding slot a device field address names
func (r *resourceCalls) stageSlot(field Expr) (*textureTokens, uint32, bool) {
	b, ok := field.(Bin)
	if !ok || b.Op != "add" || !r.deviceField(b.A) {
		return nil, 0, false
	}
	if inner, isLoad := b.A.(Load); !isLoad || inner.Addr != nil {
		return nil, 0, false
	}
	off, isOff := isConst(b.B)
	if !isOff {
		return nil, 0, false
	}
	for i := range r.textures {
		t := &r.textures[i]
		if int32(off) >= t.slot && (int32(off)-t.slot)%4 == 0 && (int32(off)-t.slot)/4 < textureStages {
			return t, uint32(int32(off)-t.slot) / 4, true
		}
	}
	return nil, 0, false
}

// Reports whether a path stores into the binding slot of a stage or through the texture one holds
func (r *resourceCalls) touchesStages(p Path) bool {
	for _, e := range p.Effects {
		if e.Kind != "store" || e.Addr == nil {
			continue
		}
		if _, _, ok := r.stageSlot(e.Addr); ok {
			return true
		}
		if r.deviceHeld(e.Addr) != nil {
			return true
		}
	}
	return false
}

// Reports whether an expression is the constant zero
func isZero(e Expr) bool {
	c, ok := isConst(e)
	return ok && c == 0
}

// Reports whether an expression reads the device the resource routines push into
func (r *resourceCalls) readsDevice(e Expr) bool {
	found := false
	mapExpr(e, func(x Expr) (Expr, bool) {
		if l, ok := x.(Load); ok && l.Addr == nil && l.Base == "" && uint32(l.Off) == r.device {
			found = true
		}
		return nil, false
	})
	return found
}

// Reports whether an address names a field of the device rather than memory the device points at
func (r *resourceCalls) deviceField(e Expr) bool {
	switch v := e.(type) {
	case Load:
		return v.Addr == nil && v.Base == "" && uint32(v.Off) == r.device
	case Bin:
		return v.Op == "add" && r.deviceField(v.A)
	}
	return false
}

// Reports whether an address names the command space the device's first field points at
func (r *resourceCalls) commandSpace(e Expr) bool {
	switch v := e.(type) {
	case Load:
		inner, ok := v.Addr.(Load)
		return ok && inner.Addr == nil && inner.Base == "" && uint32(inner.Off) == r.device
	case Bin:
		return v.Op == "add" && r.commandSpace(v.A)
	}
	return false
}

// Reads the allocation size and attributes of a call to the console's allocator
func allocation(e Effect) (Expr, uint32, bool) {
	if e.Kind != "call" || e.Fn != memAlloc || len(e.Args) != 2 {
		return nil, 0, false
	}
	attrs, ok := isConst(e.Args[1])
	if !ok {
		return nil, 0, false
	}
	return e.Args[0], attrs, true
}

// Reports whether two stores address words a fixed distance apart, in an allocation or through a pointer
func adjacentStores(a, b Effect, delta int32) bool {
	if b.Kind != "store" {
		return false
	}
	if a.Addr == nil && b.Addr == nil {
		return a.Base == b.Base && a.Off+delta == b.Off
	}
	if a.Addr == nil || b.Addr == nil {
		return false
	}
	return sameExpr(b.Addr, Bin{"add", a.Addr, IConst{uint32(delta)}})
}

// push is one command an inlined body wrote into the device's command space
type push struct {
	token   uint32
	data    Expr
	va      uint32
	drop    []int
	stage   uint32
	texture bool
}

// Reads the command a body pushes at an effect: the token, its data word and the advance past the pair
func (r *resourceCalls) readPush(p Path, i int) (push, bool, error) {
	e := p.Effects[i]
	if e.Kind != "store" || e.Size != 4 {
		return push{}, false, nil
	}
	token, ok := isConst(e.Value)
	if !ok || token < commandTokenFloor {
		return push{}, false, nil
	}
	reserved := i > 0 && p.Effects[i-1].Kind == "call" && p.Effects[i-1].Fn == requestSpace && e.Base == fmt.Sprintf("res%d", p.Effects[i-1].Seq)
	if e.Addr != nil && !r.commandSpace(e.Addr) {
		return push{}, false, nil
	}
	if e.Addr == nil && !reserved {
		return push{}, false, nil
	}
	// The scheduler may place the body's other stores between the pair and the advance past it
	limit := i + 1 + pushWindow
	if limit > len(p.Effects) {
		limit = len(p.Effects)
	}
	dataAt := -1
	for j := i + 1; j < limit; j++ {
		if adjacentStores(e, p.Effects[j], 4) {
			dataAt = j
			break
		}
	}
	if dataAt < 0 {
		return push{}, false, fmt.Errorf("xbe: the command %08X pushed at %08X carries no data word", token, e.VA)
	}
	advanceAt := -1
	for j := dataAt + 1; j < limit; j++ {
		f := p.Effects[j]
		if f.Kind == "store" && f.Off == 0 && f.Addr != nil && sameExpr(f.Addr, Load{Base: "", Off: int32(r.device), Size: 4}) {
			advanceAt = j
			break
		}
	}
	if advanceAt < 0 {
		return push{}, false, fmt.Errorf("xbe: the command %08X pushed at %08X advances no command pointer", token, e.VA)
	}
	out := push{token: token, data: p.Effects[dataAt].Value, va: e.VA, drop: []int{i, dataAt, advanceAt}}
	if reserved {
		out.drop = append(out.drop, i-1)
	}
	if token == r.flush {
		return out, true, nil
	}
	stage, isTexture, err := r.textureStage(p, token)
	if err != nil {
		return push{}, false, err
	}
	if !isTexture {
		return push{}, false, fmt.Errorf("xbe: the dashboard pushes command %08X at %08X, which no XDK routine the derivation reads emits", token, e.VA)
	}
	out.texture, out.stage = true, stage
	return out, true, nil
}

// Folds the resource routines the optimizer inlined back into the XDK calls the runtime implements
func (ex *Executor) foldInlinedResources(paths []Path) ([]Path, error) {
	r := ex.d.resources
	if r == nil {
		return paths, nil
	}
	var out []Path
	for _, p := range paths {
		folded, keep, err := ex.foldResources(p, r)
		if err != nil {
			return nil, err
		}
		if keep {
			out = append(out, folded)
		}
	}
	return out, nil
}

func (ex *Executor) foldResources(p Path, r *resourceCalls) (Path, bool, error) {
	sizes, data, drop, seq := map[int]Expr{}, map[int]int{}, map[int]bool{}, 0
	for _, e := range p.Effects {
		if e.Seq > seq {
			seq = e.Seq
		}
	}
	// An allocation the XDK's own out of memory path tested to zero is a path the console never runs
	headers := map[int]bool{}
	for _, e := range p.Effects {
		if _, attrs, ok := allocation(e); ok && attrs == r.headerAttrs {
			headers[e.Seq] = true
		}
	}
	for _, g := range p.Guards {
		zero, tests := handleTest(g, func(e Expr) bool {
			s, ok := allocationOf(e)
			return ok && headers[s]
		})
		if tests && zero {
			return Path{}, false, nil
		}
	}
	// A creation is a header allocation paired with the data allocation whose address fills the header
	for i, e := range p.Effects {
		size, attrs, ok := allocation(e)
		if !ok || attrs != r.headerAttrs {
			continue
		}
		if c, isC := isConst(size); !isC || c != r.headerSize {
			return Path{}, false, fmt.Errorf("xbe: the resource header allocated at %08X is %s bytes, not the %d the XDK takes", e.VA, ExprString(size), r.headerSize)
		}
		paired := false
		for j := i + 1; j < len(p.Effects); j++ {
			dataSize, dataAttrs, isAlloc := allocation(p.Effects[j])
			if !isAlloc || dataAttrs != r.dataAttrs {
				continue
			}
			sizes[e.Seq], data[e.Seq] = dataSize, p.Effects[j].Seq
			drop[j], paired = true, true
			break
		}
		if !paired {
			return Path{}, false, fmt.Errorf("xbe: the resource header allocated at %08X is followed by no data allocation", e.VA)
		}
	}
	if len(sizes) == 0 && !r.pushes(p) && !r.touchesStages(p) {
		return p, true, nil
	}
	allocated := map[int]bool{}
	for hdr, dataSeq := range data {
		allocated[hdr], allocated[dataSeq] = true, true
	}
	for _, g := range p.Guards {
		zero, tests := handleTest(g, func(e Expr) bool {
			s, ok := allocationOf(e)
			return ok && allocated[s]
		})
		if tests && zero {
			return Path{}, false, nil
		}
	}
	handles, locks := map[int]CallResult{}, map[int]CallResult{}
	for hdr, size := range sizes {
		handles[hdr] = CallResult{Fn: createVertexBuffer, Args: []Expr{size}, Seq: hdr}
		// The data address only reaches the dashboard's own code through the lock, which returns it aliased
		if r.locksData(p, data[hdr]) {
			locks[data[hdr]] = CallResult{Fn: lockVertexBuffer, Args: []Expr{handles[hdr]}, Seq: data[hdr]}
		}
	}
	// The header the creation filled is the buffer, and the aliased data address is what the lock returns
	subst := func(e Expr) (Expr, bool) {
		// Memory the lock handed back reads and writes as the buffer of the call, the way a plain build has it
		if l, isLoad := e.(Load); isLoad && l.Addr != nil {
			if seq, off, found := r.dataAddress(l.Addr); found && locks[seq].Fn != "" {
				return Load{Base: fmt.Sprintf("res%d", seq), Off: off, Size: l.Size}, true
			}
		}
		if s, _, ok := maskedAllocation(e, r); ok && locks[s].Fn != "" {
			return locks[s], true
		}
		if s, ok := allocationOf(e); ok && handles[s].Fn != "" {
			return handles[s], true
		}
		return nil, false
	}
	var commands []push
	for i := range p.Effects {
		if drop[i] {
			continue
		}
		pushed, ok, err := r.readPush(p, i)
		if err != nil {
			return Path{}, false, err
		}
		if !ok {
			continue
		}
		commands = append(commands, pushed)
		for _, j := range pushed.drop {
			drop[j] = true
		}
	}
	emit := map[int][]Effect{}
	for _, pushed := range commands {
		if !pushed.texture {
			continue
		}
		seq++
		call := Effect{Kind: "call", VA: pushed.va, Fn: setTextureCall, Args: []Expr{IConst{pushed.stage}, mapExpr(pushed.data, subst)}, Seq: seq}
		emit[pushed.drop[0]] = append(emit[pushed.drop[0]], call)
	}
	// What is left of an inlined body is the bookkeeping it did on the device, which the folded call carries
	for i, e := range p.Effects {
		if drop[i] {
			continue
		}
		switch {
		case e.Kind == "call" && e.Fn == requestSpace:
			return Path{}, false, fmt.Errorf("xbe: the command space reserved at %08X is written with no command the derivation reads", e.VA)
		case e.Kind == "store" && e.Addr != nil && r.commandSpace(e.Addr):
			return Path{}, false, fmt.Errorf("xbe: the store at %08X writes the command space with no command the derivation reads", e.VA)
		case e.Kind == "call" && (e.Fn == blockOnTime || e.Fn == destroyResource):
			drop[i] = true
		case e.Kind == "call" && e.Fn == memFree && mentionsAllocation(e.Args, allocated):
			drop[i] = true
		case e.Kind == "store" && e.Addr != nil && r.deviceHeld(e.Addr) != nil:
			// The setter releases the texture the stage held, which the runtime keeps alive like every resource
			if _, _, isSlot := r.stageSlot(r.deviceHeld(e.Addr)); !isSlot {
				return Path{}, false, fmt.Errorf("xbe: the store at %08X writes through the pointer the device holds at %s, which no XDK routine the derivation reads touches", e.VA, ExprString(r.deviceHeld(e.Addr)))
			}
			drop[i] = true
		case e.Kind == "store" && e.Addr != nil && e.Size == 4 && isZero(e.Value):
			// Clearing the slot of a stage is the setter binding no texture, its command never pushed
			if _, stage, isSlot := r.stageSlot(e.Addr); isSlot {
				seq++
				emit[i] = append(emit[i], Effect{Kind: "call", VA: e.VA, Fn: setTextureCall, Args: []Expr{IConst{stage}, IConst{0}}, Seq: seq})
				drop[i] = true
			}
		case e.Kind == "store" && allocationBase(e.Base, allocated):
			drop[i] = true
		}
	}
	out := Path{Return: mapExpr(p.Return, subst)}
	kept := make([]int, len(p.Effects)+1)
	for i, e := range p.Effects {
		kept[i] = len(out.Effects)
		out.Effects = append(out.Effects, emit[i]...)
		if drop[i] {
			continue
		}
		if handle, isHeader := handles[e.Seq]; isHeader && e.Kind == "call" {
			out.Effects = append(out.Effects, Effect{Kind: "call", VA: e.VA, Fn: handle.Fn, Args: handle.Args, Seq: handle.Seq})
			if lock, locked := locks[data[e.Seq]]; locked {
				out.Effects = append(out.Effects, Effect{Kind: "call", VA: e.VA, Fn: lock.Fn, Args: lock.Args, Seq: lock.Seq})
			}
			continue
		}
		if e.Kind == "store" && e.Addr != nil {
			if seq, off, found := r.dataAddress(e.Addr); found && locks[seq].Fn != "" {
				e.Base, e.Off, e.Addr = fmt.Sprintf("res%d", seq), e.Off+off, nil
			}
		}
		e.Value = mapExpr(e.Value, subst)
		e.Addr = mapExpr(e.Addr, subst)
		e.This = mapExpr(e.This, subst)
		args := make([]Expr, len(e.Args))
		for k, a := range e.Args {
			args[k] = mapExpr(a, subst)
		}
		e.Args = args
		buffers := make([]ArgBuffer, len(e.Buffers))
		for k, b := range e.Buffers {
			elems := make([]Expr, len(b.Elems))
			for j, x := range b.Elems {
				elems[j] = mapExpr(x, subst)
			}
			buffers[k] = ArgBuffer{Index: b.Index, Elems: elems}
		}
		e.Buffers = buffers
		out.Effects = append(out.Effects, e)
	}
	kept[len(p.Effects)] = len(out.Effects)
	for i, g := range p.Guards {
		// The tests inside an inlined body are the XDK's own, on the device and on the memory it took
		if r.readsDevice(g) || mentionsAllocation([]Expr{g}, allocated) {
			continue
		}
		pos := len(p.Effects)
		if i < len(p.GuardPos) {
			pos = p.GuardPos[i]
		}
		out.Guards = append(out.Guards, mapExpr(g, subst))
		out.GuardPos = append(out.GuardPos, kept[pos])
	}
	return out, true, nil
}

// Reads the allocation a masked data address belongs to, and whether it carries the uncached alias
func maskedAllocation(e Expr, r *resourceCalls) (int, bool, bool) {
	b, ok := e.(Bin)
	if !ok {
		return 0, false, false
	}
	if c, isC := isConst(b.B); isC && b.Op == "or" && c == r.alias {
		seq, _, found := maskedAllocation(b.A, r)
		return seq, true, found
	}
	if c, isC := isConst(b.B); isC && b.Op == "and" && c == r.dataMask {
		seq, found := allocationOf(b.A)
		return seq, false, found
	}
	return 0, false, false
}

// Reads the allocation and offset a data address names, as the lock hands the memory back
func (r *resourceCalls) dataAddress(e Expr) (int, int32, bool) {
	if b, ok := e.(Bin); ok && b.Op == "add" {
		if off, isC := isConst(b.B); isC {
			if seq, _, found := maskedAllocation(b.A, r); found {
				return seq, int32(off), true
			}
		}
	}
	if seq, _, found := maskedAllocation(e, r); found {
		return seq, 0, true
	}
	return 0, 0, false
}

// Reports whether a path reads the data of an allocation through the address the lock hands back
func (r *resourceCalls) locksData(p Path, seq int) bool {
	found := false
	look := func(e Expr) {
		mapExpr(e, func(x Expr) (Expr, bool) {
			if s, aliased, ok := maskedAllocation(x, r); ok && aliased && s == seq {
				found = true
			}
			return nil, false
		})
	}
	for _, e := range p.Effects {
		look(e.Value)
		look(e.Addr)
		for _, a := range e.Args {
			look(a)
		}
		for _, b := range e.Buffers {
			for _, x := range b.Elems {
				look(x)
			}
		}
	}
	return found
}

// Reports whether a path pushes a command into the device's command space
func (r *resourceCalls) pushes(p Path) bool {
	for _, e := range p.Effects {
		if e.Kind == "call" && e.Fn == requestSpace {
			return true
		}
		if e.Kind == "store" && e.Addr != nil && r.commandSpace(e.Addr) {
			return true
		}
	}
	return false
}

// Reads the first global a path names outside the memory the image loads into
func (d *Disassembly) outsideImage(p Path) (uint32, bool) {
	found, outside := uint32(0), false
	note := func(va uint32) {
		if !outside && d.sectionOf(va) == nil {
			found, outside = va, true
		}
	}
	for _, e := range pathExprs(p) {
		mapExpr(e, func(x Expr) (Expr, bool) {
			switch v := x.(type) {
			case Load:
				if v.Addr == nil && v.Base == "" {
					note(uint32(v.Off))
				}
			case Ptr:
				if va, held := globalObjectAddress(Ptr{v.Base, 0}); held {
					note(va)
				}
			}
			return nil, false
		})
	}
	return found, outside
}
