package xbe

import (
	"sort"

	"golang.org/x/arch/x86/x86asm"
)

// StructuredStmt represents a recovered high-level control flow statement.
type StructuredStmt interface {
	stmtTag()
}

// SeqStmt is a sequence of statements executed in order.
type SeqStmt struct {
	Stmts []StructuredStmt
}

func (SeqStmt) stmtTag() {}

// BlockStmt is a basic block of instructions (no internal control flow).
type BlockStmt struct {
	VA    uint32
	Insns []Instruction
}

func (BlockStmt) stmtTag() {}

// IfStmt is a conditional branch: if (cond) then else.
type IfStmt struct {
	CondVA   uint32         // VA of the conditional jump instruction
	CondInsn Instruction    // the actual Jcc instruction
	Negated  bool           // true if condition should be negated (jump skips the Then block)
	Then     StructuredStmt // taken branch
	Else     StructuredStmt // fall-through branch (may be nil)
}

func (IfStmt) stmtTag() {}

// WhileStmt is a loop: while (cond) body.
type WhileStmt struct {
	CondVA   uint32
	CondInsn Instruction
	Negated  bool
	Body     StructuredStmt
}

func (WhileStmt) stmtTag() {}

// DoWhileStmt is a do-while loop: do { body } while (cond).
// The back edge jumps when condition is TRUE, so the condition is NOT negated.
type DoWhileStmt struct {
	CondVA   uint32
	CondInsn Instruction
	Body     StructuredStmt
}

func (DoWhileStmt) stmtTag() {}

// BreakStmt exits the innermost loop.
type BreakStmt struct{}

func (BreakStmt) stmtTag() {}

// ContinueStmt jumps to the top of the innermost loop.
type ContinueStmt struct{}

func (ContinueStmt) stmtTag() {}

// ReturnStmt returns from the function.
type ReturnStmt struct {
	Insn Instruction
}

func (ReturnStmt) stmtTag() {}

// GotoStmt is an unresolvable jump (fallback when structural analysis fails).
type GotoStmt struct {
	TargetVA uint32
}

func (GotoStmt) stmtTag() {}

// LabelStmt marks a goto target.
type LabelStmt struct {
	VA uint32
}

func (LabelStmt) stmtTag() {}

// RecoverStructure converts a function's basic block CFG into structured
// control flow statements (if/else, while, do-while, break, continue).
//
// Algorithm: interval-based structural analysis.
// 1. Sort blocks by VA (topological-ish order for forward code)
// 2. Identify back edges (loops) — target VA < source VA
// 3. For each loop, identify the header (back edge target) and all blocks
//    in the loop body (blocks that can reach the back edge source without
//    leaving through the header)
// 4. Collapse loops into WhileStmt/DoWhileStmt
// 5. For remaining conditional branches, pattern-match if/else and if/then
// 6. Fall back to goto for irreducible control flow
func RecoverStructure(blocks []*BasicBlock, blockMap map[uint32]*BasicBlock) StructuredStmt {
	if len(blocks) == 0 {
		return &SeqStmt{}
	}

	// Sort blocks by VA
	sorted := make([]*BasicBlock, len(blocks))
	copy(sorted, blocks)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].StartVA < sorted[j].StartVA })

	// Build block index
	blockIdx := make(map[uint32]int)
	for i, b := range sorted {
		blockIdx[b.StartVA] = i
	}

	// Identify back edges (loops)
	type edge struct{ from, to uint32 }
	var backEdges []edge
	for _, b := range sorted {
		for _, succ := range b.Succs {
			if succIdx, ok := blockIdx[succ]; ok {
				srcIdx := blockIdx[b.StartVA]
				if succIdx <= srcIdx {
					backEdges = append(backEdges, edge{b.StartVA, succ})
				}
			}
		}
	}

	// Find blocks belonging to each loop (natural loop body)
	loopHeaders := make(map[uint32]bool)
	loopBodies := make(map[uint32]map[uint32]bool) // header VA → set of body block VAs
	for _, be := range backEdges {
		header := be.to
		loopHeaders[header] = true
		if loopBodies[header] == nil {
			loopBodies[header] = make(map[uint32]bool)
		}
		body := findNaturalLoop(sorted, blockMap, header, be.from)
		for va := range body {
			loopBodies[header][va] = true
		}
	}

	// Build the structured statement tree (inLoop=false at top level)
	return structureRegion(sorted, blockMap, blockIdx, loopHeaders, loopBodies, 0, len(sorted), false)
}

// findNaturalLoop finds all blocks in the natural loop with the given header
// and back edge source. Uses reverse DFS from the back edge source.
func findNaturalLoop(blocks []*BasicBlock, blockMap map[uint32]*BasicBlock, header, backEdgeSrc uint32) map[uint32]bool {
	body := map[uint32]bool{header: true, backEdgeSrc: true}
	worklist := []uint32{backEdgeSrc}

	for len(worklist) > 0 {
		va := worklist[len(worklist)-1]
		worklist = worklist[:len(worklist)-1]

		block, ok := blockMap[va]
		if !ok {
			continue
		}

		for _, pred := range block.Preds {
			if !body[pred] {
				body[pred] = true
				worklist = append(worklist, pred)
			}
		}
	}

	return body
}

// structureRegion converts a contiguous range of sorted blocks into structured statements.
// inLoop indicates whether this region is inside a loop body (break/continue are valid).
func structureRegion(sorted []*BasicBlock, blockMap map[uint32]*BasicBlock, blockIdx map[uint32]int,
	loopHeaders map[uint32]bool, loopBodies map[uint32]map[uint32]bool,
	startIdx, endIdx int, inLoop bool) StructuredStmt {

	var stmts []StructuredStmt
	i := startIdx

	for i < endIdx {
		block := sorted[i]

		// Check if this block is a loop header
		if loopHeaders[block.StartVA] {
			loopBody := loopBodies[block.StartVA]

			// Find the extent of the loop in sorted order
			loopEnd := i + 1
			for loopEnd < endIdx && loopBody[sorted[loopEnd].StartVA] {
				loopEnd++
			}

			// Determine loop type from the header block's last instruction
			lastInsn := block.Insns[len(block.Insns)-1]

			if isConditionalJump(lastInsn.Inst.Op) {
				target := resolveTarget(&lastInsn)

				// If the conditional jump exits the loop → while loop
				// If it stays in the loop → do-while (condition at bottom)
				if !loopBody[target] {
					// while (!cond) { body }
					// The jump exits when condition is TRUE, so loop continues when FALSE
					bodyStmt := structureRegion(sorted, blockMap, blockIdx, loopHeaders, loopBodies, i+1, loopEnd, true)
					headerInsns := block.Insns[:len(block.Insns)-1]
					if len(headerInsns) > 0 {
						stmts = append(stmts, &BlockStmt{VA: block.StartVA, Insns: headerInsns})
					}
					stmts = append(stmts, &WhileStmt{
						CondVA:   lastInsn.VA,
						CondInsn: lastInsn,
						Negated:  true,
						Body:     bodyStmt,
					})
				} else {
					// do { body } while (cond)
					// Emit the header block as the body start, then recurse for rest
					stmts = append(stmts, &BlockStmt{VA: block.StartVA, Insns: block.Insns[:len(block.Insns)-1]})
					var bodyStmt StructuredStmt = &SeqStmt{}
					if i+1 < loopEnd {
						bodyStmt = structureRegion(sorted, blockMap, blockIdx, loopHeaders, loopBodies, i+1, loopEnd, true)
					}
					stmts = append(stmts, &DoWhileStmt{
						CondVA:   lastInsn.VA,
						CondInsn: lastInsn,
						Body:     bodyStmt,
					})
				}
			} else {
				// Unconditional loop (infinite or break-controlled)
				// Emit header instructions, then recurse for body (skipping header)
				stmts = append(stmts, &BlockStmt{VA: block.StartVA, Insns: block.Insns})
				if i+1 < loopEnd {
					bodyStmt := structureRegion(sorted, blockMap, blockIdx, loopHeaders, loopBodies, i+1, loopEnd, true)
					stmts = append(stmts, &WhileStmt{
						CondVA: block.StartVA,
						Body:   bodyStmt,
					})
				} else {
					stmts = append(stmts, &WhileStmt{
						CondVA: block.StartVA,
						Body:   &SeqStmt{},
					})
				}
			}

			i = loopEnd
			continue
		}

		// Check if this block ends with a conditional jump (if/else)
		if len(block.Insns) > 0 {
			lastInsn := block.Insns[len(block.Insns)-1]

			if isConditionalJump(lastInsn.Inst.Op) && len(block.Succs) == 2 {
				fallThrough := block.Succs[0]
				jumpTarget := block.Succs[1]

				ftIdx, ftOk := blockIdx[fallThrough]
				jtIdx, jtOk := blockIdx[jumpTarget]

				// Emit the block's non-jump instructions
				headerInsns := block.Insns[:len(block.Insns)-1]
				if len(headerInsns) > 0 {
					stmts = append(stmts, &BlockStmt{VA: block.StartVA, Insns: headerInsns})
				}

				if ftOk && jtOk && ftIdx == i+1 {
					// Standard if pattern: fall-through is "then", jump target is after
					// Find where "then" ends (at the jump target or end of region)
					thenEnd := jtIdx
					if thenEnd > endIdx {
						thenEnd = endIdx
					}

					if thenEnd > i+1 {
						thenStmt := structureRegion(sorted, blockMap, blockIdx, loopHeaders, loopBodies, i+1, thenEnd, inLoop)

						// Check if there's an else branch
						// The "then" branch's last block may jump past the "else"
						// For now: if jumpTarget == thenEnd, it's if/then (no else)
						// If jumpTarget > thenEnd, the blocks between thenEnd and jumpTarget are else
						if jtIdx > thenEnd && jtIdx <= endIdx {
							elseStmt := structureRegion(sorted, blockMap, blockIdx, loopHeaders, loopBodies, thenEnd, jtIdx, inLoop)
							stmts = append(stmts, &IfStmt{
								CondVA:   lastInsn.VA,
								CondInsn: lastInsn,
								Then:     elseStmt, // note: jump taken = condition true → this is the "if" body
								Else:     thenStmt, // fall-through = condition false
							})
							i = jtIdx
						} else {
							// if/then only (jump skips the then block)
							// The condition must be NEGATED: the block runs when the jump is NOT taken
							stmts = append(stmts, &IfStmt{
								CondVA:   lastInsn.VA,
								CondInsn: lastInsn,
								Negated:  true,
								Then:     thenStmt,
							})
							i = thenEnd
						}
						continue
					}
				}

				// Couldn't pattern-match — emit as goto
				stmts = append(stmts, &IfStmt{
					CondVA:   lastInsn.VA,
					CondInsn: lastInsn,
					Then:     &GotoStmt{TargetVA: jumpTarget},
				})
				i++
				continue
			}

			// Unconditional jump
			if isUnconditionalJump(lastInsn.Inst.Op) {
				target := resolveTarget(&lastInsn)

				// Emit non-jump instructions
				bodyInsns := block.Insns[:len(block.Insns)-1]
				if len(bodyInsns) > 0 {
					stmts = append(stmts, &BlockStmt{VA: block.StartVA, Insns: bodyInsns})
				}

				// Classify the jump target
				if inLoop {
					if tIdx, ok := blockIdx[target]; ok && tIdx >= endIdx {
						stmts = append(stmts, &BreakStmt{})
					} else if target == sorted[startIdx].StartVA {
						stmts = append(stmts, &ContinueStmt{})
					} else {
						stmts = append(stmts, &GotoStmt{TargetVA: target})
					}
				} else {
					// Not inside a loop — forward jumps are gotos
					stmts = append(stmts, &GotoStmt{TargetVA: target})
				}
				i++
				continue
			}

			// Return instruction
			if isRetInsn(lastInsn.Inst.Op) {
				stmts = append(stmts, &BlockStmt{VA: block.StartVA, Insns: block.Insns[:len(block.Insns)-1]})
				stmts = append(stmts, &ReturnStmt{Insn: lastInsn})
				i++
				continue
			}
		}

		// Plain block — no control flow at end (falls through)
		stmts = append(stmts, &BlockStmt{VA: block.StartVA, Insns: block.Insns})
		i++
	}

	if len(stmts) == 1 {
		return stmts[0]
	}
	return &SeqStmt{Stmts: stmts}
}

// Use x86asm.RET directly via the imported package
func isRetInsn(op x86asm.Op) bool { return op == x86asm.RET }
