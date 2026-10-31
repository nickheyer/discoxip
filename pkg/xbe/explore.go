package xbe

import (
	"fmt"
	"sort"
)

// Follows every way open inputs take a sequence of routines, forking at tests on them with the inputs reseeded
type Explorer struct {
	Model    *MachineModel
	Prepare  func() *MachineRun
	Enter    func(r *MachineRun, entry uint32)
	Halts    map[uint32]bool
	Steps    int
	Paths    int
	Progress func(steps, paths, open int)
	steps    int
	paths    int
	open     int
	most     int
}

// Blocks between two reports of an exploration's progress
const progressSteps = 1 << 20

// explorePath is one way through the routines, the run it holds and the tests it has turned
type explorePath struct {
	run     *MachineRun
	key     string
	stage   int
	forked  map[uint32]bool
	pending *pendingTurn
	spinAt  uint32
}

// The other way of a test a path could not turn again, with the run as it stood there
type pendingTurn struct {
	path  *explorePath
	block *liftedBlock
	way   openWay
}

// openWay is one way an open test can go, the test it decides and where it leads
type openWay struct {
	cond   Expr
	want   bool
	target uint32
}

// openGuard is a test on open inputs with the way the path took it
type openGuard struct {
	cond Expr
	want bool
}

// Stats reports the blocks run, the paths followed and the most paths held open at once so far
func (ex *Explorer) Stats() (steps, paths, most int) {
	return ex.steps, ex.paths, ex.most
}

// Explore follows every path through the routines in turn, each entered once the one before returned
func (ex *Explorer) Explore(entries []uint32) error {
	if len(entries) == 0 {
		return fmt.Errorf("explore: nothing to run")
	}
	first := ex.Prepare()
	ex.Enter(first, entries[0])
	work := []*explorePath{{run: first, key: blockKey(entries[0], nil), forked: map[uint32]bool{}}}
	ex.paths, ex.open, ex.most = 1, 1, 1
	for len(work) > 0 {
		p := work[len(work)-1]
		work = work[:len(work)-1]
		ex.open = len(work) + 1
		for p != nil {
			more, err := ex.step(p, entries, &work)
			if err != nil {
				return err
			}
			if !more {
				break
			}
		}
	}
	return nil
}

// Runs one block of a path, forking at an open test, reporting whether the path goes on
func (ex *Explorer) step(p *explorePath, entries []uint32, work *[]*explorePath) (bool, error) {
	ex.steps++
	if ex.steps > ex.Steps {
		return false, fmt.Errorf("explore: %d blocks run without the paths ending", ex.Steps)
	}
	if ex.Progress != nil && ex.steps%progressSteps == 0 {
		ex.Progress(ex.steps, ex.paths, ex.open)
	}
	r := p.run
	if err := r.visit(p.key); err != nil {
		if p.pending != nil {
			resumed, err := ex.turn(p.pending.path, p.pending.block, p.pending.way)
			if err != nil {
				return false, err
			}
			if resumed == nil {
				return false, nil
			}
			*p = *resumed
			return true, nil
		}
		for _, va := range r.Trail {
			if va == p.spinAt {
				return false, nil
			}
		}
		return false, fmt.Errorf("explore: %w", err)
	}
	b := r.mm.blocks[p.key]
	if b == nil {
		if why, failed := r.mm.Errors[p.key]; failed {
			return false, fmt.Errorf("explore: block %s was not lifted: %s", p.key, why)
		}
		return false, fmt.Errorf("explore: no block %s", p.key)
	}
	r.Trail = append(r.Trail, b.va)
	if len(r.Trail) > 32 {
		r.Trail = r.Trail[1:]
	}
	next, err := r.block(b)
	if err != nil {
		return false, fmt.Errorf("explore: block %s: %w (after %X)", p.key, err, r.Trail)
	}
	if r.sh.failed != nil {
		return false, fmt.Errorf("explore: block %s: %w", p.key, r.sh.failed)
	}
	if b.term.ret != 0 && r.OnCall != nil {
		r.OnCall(r, next, b.term.ret)
	}
	if br := r.sh.branch; br != nil {
		r.sh.branch = nil
		if err := ex.fork(p, b, br, work); err != nil {
			return false, err
		}
	}
	if ex.Halts[next] {
		return false, nil
	}
	if r.Stop[next] {
		return ex.leave(p, entries)
	}
	if hook, hooked := r.Hooks[next]; hooked {
		next = hook(r)
		if r.Failed != nil {
			return false, fmt.Errorf("explore: %w", r.Failed)
		}
		if ex.Halts[next] {
			return false, nil
		}
		if r.Stop[next] {
			return ex.leave(p, entries)
		}
		p.key = blockKey(next, nil)
		return true, nil
	}
	p.key = r.keyFor(b, next)
	return true, nil
}

// Enters the next routine of the sequence once one returned, ending the path after the last
func (ex *Explorer) leave(p *explorePath, entries []uint32) (bool, error) {
	p.stage++
	if p.stage >= len(entries) {
		return false, nil
	}
	ex.Enter(p.run, entries[p.stage])
	p.key = blockKey(entries[p.stage], nil)
	p.pending = nil
	return true, nil
}

// Records the way a path took an open test, forking the other ways, kept pending where the block was turned before
func (ex *Explorer) fork(p *explorePath, b *liftedBlock, br *openBranch, work *[]*explorePath) error {
	var taken openWay
	var others []openWay
	switch br.kind {
	case "branch":
		taken = openWay{cond: br.cond, want: br.outcome}
		target := br.taken
		if br.outcome {
			target = br.fall
		}
		others = []openWay{{cond: br.cond, want: !br.outcome, target: target}}
	case "switch":
		taken = openWay{cond: Cond{Op: "eq", A: br.cond, B: IConst{V: br.index}}, want: true}
		for k, target := range br.targets {
			if uint32(k) != br.index {
				others = append(others, openWay{cond: Cond{Op: "eq", A: br.cond, B: IConst{V: uint32(k)}}, want: true, target: target})
			}
		}
	}
	p.spinAt = b.va
	defer func() { p.run.sh.guards = append(p.run.sh.guards, openGuard{cond: taken.cond, want: taken.want}) }()
	for _, w := range others {
		c := &explorePath{run: p.run.clone(), stage: p.stage, forked: make(map[uint32]bool, len(p.forked)+1)}
		for va := range p.forked {
			c.forked[va] = true
		}
		c.forked[b.va] = true
		if p.forked[b.va] {
			p.pending = &pendingTurn{path: c, block: b, way: w}
			continue
		}
		turned, err := ex.turn(c, b, w)
		if err != nil {
			return err
		}
		if turned == nil {
			continue
		}
		if ex.open+1 > ex.Paths {
			return fmt.Errorf("explore: block %08X: %d paths open at once", b.va, ex.Paths)
		}
		ex.paths++
		ex.open++
		ex.most = max(ex.most, ex.open)
		*work = append(*work, turned)
	}
	return nil
}

// Takes a path the other way of a test with inputs reseeded, nothing where no seed takes it
func (ex *Explorer) turn(c *explorePath, b *liftedBlock, w openWay) (*explorePath, error) {
	seeds, decided, err := c.run.seedsFor(w.cond, w.want)
	if err != nil {
		return nil, fmt.Errorf("explore: block %08X: %w", b.va, err)
	}
	if decided {
		if seeds == nil {
			return nil, nil
		}
		if err := c.run.reseed(seeds); err != nil {
			return nil, fmt.Errorf("explore: block %08X: %w", b.va, err)
		}
		c.run.sh.guards = append(c.run.sh.guards, openGuard{cond: w.cond, want: w.want})
	}
	c.key = c.run.keyFor(b, w.target)
	return c, nil
}

// Seeds taking a test the way wanted with every earlier test holding, undecided past the input count searched
func (r *MachineRun) seedsFor(cond Expr, want bool) (seeds map[int]uint32, decided bool, err error) {
	leaves := openLeaves(cond)
	if len(leaves) == 0 {
		return nil, false, fmt.Errorf("a test reads no open input")
	}
	if len(leaves) > seedLeaves {
		return nil, false, nil
	}
	mentions := func(e Expr) bool {
		for _, id := range openLeaves(e) {
			for _, l := range leaves {
				if id == l {
					return true
				}
			}
		}
		return false
	}
	var relevant []openGuard
	consts, masks, shifts := map[uint32]bool{}, map[uint32]bool{}, map[uint32]bool{}
	collect := func(e Expr) {
		WalkExpr(e, func(n Expr) {
			switch v := n.(type) {
			case IConst:
				consts[v.V] = true
			case Bin:
				c, ok := v.B.(IConst)
				if !ok {
					return
				}
				switch v.Op {
				case "and":
					masks[c.V] = true
				case "shl", "shr", "sar":
					shifts[c.V&31] = true
				}
			}
		})
	}
	collect(cond)
	for _, g := range r.sh.guards {
		if mentions(g.cond) {
			relevant = append(relevant, g)
			collect(g.cond)
		}
	}
	holds := func(seeds map[int]uint32) bool {
		read := r.readUnder(seeds)
		v, err := evalShadow(cond, seeds, read)
		if err != nil || (v != 0) != want {
			return false
		}
		for _, g := range relevant {
			v, err := evalShadow(g.cond, seeds, read)
			if err != nil || (v != 0) != g.want {
				return false
			}
		}
		return true
	}
	seeds = make(map[int]uint32, len(r.sh.seeds))
	for id, v := range r.sh.seeds {
		seeds[id] = v
	}
	whole := make([][]uint32, len(leaves))
	for i, id := range leaves {
		whole[i] = wholeValues(r.sh.sizes[id], consts)
	}
	for i, id := range leaves {
		for _, v := range append(whole[i], fieldValues(r.sh.sizes[id], r.sh.seeds[id], consts, masks, shifts)...) {
			seeds[id] = v
			if holds(seeds) {
				return map[int]uint32{id: v}, true, nil
			}
		}
		seeds[id] = r.sh.seeds[id]
	}
	if len(leaves) == 2 {
		for _, a := range whole[0] {
			for _, b := range whole[1] {
				seeds[leaves[0]], seeds[leaves[1]] = a, b
				if holds(seeds) {
					return map[int]uint32{leaves[0]: a, leaves[1]: b}, true, nil
				}
			}
		}
	}
	return nil, true, nil
}

// Open inputs a test may decide on for seeds to be searched over
const seedLeaves = 2

// The width mask of an open input
func widthMask(size int) uint32 {
	if size >= 4 {
		return 0xFFFFFFFF
	}
	return uint32(1)<<(8*uint(size)) - 1
}

// The constants of a set in order
func sortedValues(set map[uint32]bool) []uint32 {
	out := make([]uint32, 0, len(set))
	for c := range set {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// Whole values tried, 0, every single bit, all bits and each named constant with neighbours and complement
func wholeValues(size int, consts map[uint32]bool) []uint32 {
	mask := widthMask(size)
	seen := map[uint32]bool{}
	var out []uint32
	add := func(v uint32) {
		v &= mask
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	add(0)
	add(mask)
	for k := 0; k < 8*size; k++ {
		add(1 << uint(k))
	}
	for _, c := range sortedValues(consts) {
		add(c)
		add(c - 1)
		add(c + 1)
		add(^c)
	}
	return out
}

// Values keeping the seed but a field, bit flips, bytes and words set to constants, masked fields at named shifts
func fieldValues(size int, seed uint32, consts, masks, shifts map[uint32]bool) []uint32 {
	mask := widthMask(size)
	seen := map[uint32]bool{}
	var out []uint32
	add := func(v uint32) {
		v &= mask
		if !seen[v] && v != seed {
			seen[v] = true
			out = append(out, v)
		}
	}
	for i := 0; i < 8*size; i++ {
		add(seed ^ 1<<uint(i))
		for j := i + 1; j < 8*size; j++ {
			add(seed ^ 1<<uint(i) ^ 1<<uint(j))
		}
	}
	values := sortedValues(consts)
	for _, c := range values {
		for b := 0; b < size; b++ {
			add(seed&^(0xFF<<uint(8*b)) | (c&0xFF)<<uint(8*b))
		}
		for b := 0; b+1 < size; b++ {
			add(seed&^(0xFFFF<<uint(8*b)) | (c&0xFFFF)<<uint(8*b))
		}
	}
	fields := sortedValues(masks)
	fields = append(fields, mask)
	at := sortedValues(shifts)
	at = append(at, 0)
	for _, m := range fields {
		for _, k := range at {
			for _, c := range values {
				add(seed&^(m<<k) | (c<<k)&(m<<k))
			}
		}
	}
	return out
}
