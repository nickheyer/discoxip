package xbe

import (
	"fmt"
	"sort"
	"strings"

	"golang.org/x/arch/x86/x86asm"
)

// The flag record kinds the page's processor keeps live, by the code the blocks store, the float compare record
// holding the two numbers compared
const jsFlagFsw = 8

var liveFlagKinds = []struct {
	code int
	kind string
}{{1, "cmp"}, {2, "sub"}, {3, "add"}, {4, "test"}, {5, "alu"}, {6, "eflags"}, {jsFlagFsw, "comiss"}}

// The conditional jumps the page decides from a live record, each with the code the blocks name it by
var jsConditionOps = map[string]x86asm.Op{"JE": x86asm.JE, "JNE": x86asm.JNE, "JB": x86asm.JB, "JAE": x86asm.JAE, "JBE": x86asm.JBE, "JA": x86asm.JA, "JL": x86asm.JL, "JGE": x86asm.JGE, "JLE": x86asm.JLE, "JG": x86asm.JG, "JS": x86asm.JS, "JNS": x86asm.JNS, "JP": x86asm.JP, "JNP": x86asm.JNP, "JO": x86asm.JO, "JNO": x86asm.JNO}

var jsConditionCodes = map[string]int{"JE": 0, "JNE": 1, "JB": 2, "JAE": 3, "JBE": 4, "JA": 5, "JL": 6, "JGE": 7, "JLE": 8, "JG": 9, "JS": 10, "JNS": 11, "JP": 12, "JNP": 13, "JO": 14, "JNO": 15}

// The record a block entered from anywhere decides on, every operand read live
func liveFlagState(kind string) flagState {
	switch kind {
	case "comiss":
		return flagState{kind: kind, a: Sym{"flags.fsw_a"}, b: Sym{"flags.fsw_b"}}
	case "alu":
		return flagState{kind: kind, a: Sym{"flags.a"}, b: Sym{"flags.b"}, r: Sym{"flags.r"}, cf: Sym{"flags.cf"}}
	}
	return flagState{kind: kind, a: Sym{"flags.a"}, b: Sym{"flags.b"}, r: Sym{"flags.r"}}
}

// A condition built by the executor's own rules from a live record, nothing where the executor has no rule
func liveCondition(ex *Executor, kind string, op x86asm.Op) (e Expr, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%v", r)
		}
	}()
	s := &execState{fl: liveFlagState(kind)}
	if kind == "comiss" {
		return ex.comissCondition(s, op), nil
	}
	return ex.condition(s, op), nil
}

// The flags word a live record stands for, built from the executor's own conditions and carry
func liveFlagsWord(ex *Executor, kind string) (Expr, error) {
	fl := liveFlagState(kind)
	if kind == "eflags" {
		return fl.a, nil
	}
	bit := func(op x86asm.Op, mask uint32) (Expr, error) {
		c, err := liveCondition(ex, kind, op)
		if err != nil {
			return nil, err
		}
		return mkBin("mul", c, IConst{mask}), nil
	}
	zf, err := bit(x86asm.JE, eflagsZero)
	if err != nil {
		return nil, err
	}
	pf, err := bit(x86asm.JP, eflagsParity)
	if err != nil {
		return nil, err
	}
	var cf, sf, of Expr = IConst{0}, IConst{0}, IConst{0}
	if kind == "comiss" {
		if cf, err = bit(x86asm.JB, eflagsCarry); err != nil {
			return nil, err
		}
	} else {
		carry, ok := carryOf(fl)
		if !ok {
			return nil, fmt.Errorf("no carry after %s flags", kind)
		}
		cf = mkBin("mul", mkCond("ne", carry, IConst{0}), IConst{eflagsCarry})
		if sf, err = bit(x86asm.JS, eflagsSign); err != nil {
			return nil, err
		}
		// The processor leaves the overflow flag undefined after the operations the executor has no rule for
		if of, err = bit(x86asm.JO, eflagsOverflow); err != nil {
			of = IConst{0}
		}
	}
	return mkBin("or", cf, mkBin("or", pf, mkBin("or", zf, mkBin("or", sf, of)))), nil
}

// Writes the page's condition and flags word functions from the executor's definitions, each live kind a case
func (g *jsGen) writeFlagDefinitions() error {
	ex := &Executor{}
	names := make([]string, 0, len(jsConditionCodes))
	for name := range jsConditionCodes {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool { return jsConditionCodes[names[i]] < jsConditionCodes[names[j]] })
	fmt.Fprintf(g.w, "function condOf(op, %s) {\nswitch (FK) {\n", strings.Join(liveStateNames[:7], ", "))
	for _, k := range liveFlagKinds {
		fmt.Fprintf(g.w, "case %d: switch (op) {\n", k.code)
		for _, name := range names {
			e, err := liveCondition(ex, k.kind, jsConditionOps[name])
			if err != nil {
				continue
			}
			g.resetBlock()
			g.countRefs([]Expr{e})
			v := g.ival(e)
			fmt.Fprintf(g.w, "case %d: { %sreturn %s; }\n", jsConditionCodes[name], g.body.String(), v)
		}
		fmt.Fprintf(g.w, "} break;\n")
	}
	fmt.Fprintf(g.w, "}\nfault(\"condition \" + op + \" after flags of kind \" + FK);\n}\n")
	fmt.Fprintf(g.w, "function flagsWordOf(%s) {\nlet w = 0x%X;\nif (IF) w |= 0x%X;\nif (DF) w |= 0x%X;\nswitch (FK) {\n", strings.Join(liveStateNames[:7], ", "), eflagsReserved, eflagsInterrupt, eflagsDirection)
	for _, k := range liveFlagKinds {
		e, err := liveFlagsWord(ex, k.kind)
		if err != nil {
			return fmt.Errorf("flags word after %s flags: %w", k.kind, err)
		}
		g.resetBlock()
		g.countRefs([]Expr{e})
		v := g.ival(e)
		mask := uint32(eflagsCarry | eflagsParity | eflagsZero | eflagsSign | eflagsOverflow)
		if k.kind == "eflags" {
			mask = ^uint32(eflagsInterrupt | eflagsDirection | eflagsReserved)
		}
		fmt.Fprintf(g.w, "case %d: { %sreturn ((%s & 0x%X) | w) >>> 0; }\n", k.code, g.body.String(), v, mask)
	}
	fmt.Fprintf(g.w, "case 0: return w >>> 0;\n}\nfault(\"flags word after flags of kind \" + FK);\n}\n")
	return nil
}

// Clears what a block's emission memoized, for the next function body
func (g *jsGen) resetBlock() {
	g.body.Reset()
	g.memoI, g.memoF, g.results = map[exprKey]string{}, map[exprKey]string{}, map[int]string{}
	g.refs, g.changed, g.copied = map[exprKey]int{}, map[string]bool{}, map[string]bool{}
	g.temps = 0
	g.block = nil
}

// The processor operations the blocks call, with the parameters each takes before the address of its instruction
var cpuOperations = []struct {
	name   string
	params string
}{
	{cpuCLI, ""}, {cpuSTI, ""}, {cpuCLD, ""}, {cpuSTD, ""}, {cpuCLTS, ""},
	{cpuPushFD, "kind, a, b, r, c"}, {cpuPopFD, "v"},
	{cpuFLDCW, "v"}, {cpuFNSTCW, ""}, {cpuFNINIT, ""}, {cpuFNCLEX, ""},
	{cpuRDTSCLo, ""}, {cpuRDTSCHi, ""},
	{cpuSCAS, "ptr, value, count, size, step, repne"}, {cpuCMPS, "a, b, count, size, step, repne"},
	{cpuINS, "port, ptr, count, size, step"}, {cpuOUTS, "port, ptr, count, size, step"},
	{portIn, "port, size"}, {portOut, "port, value, size"},
	{cpuWRMSR, "index, lo, hi"}, {cpuRDMSRLo, "index"}, {cpuRDMSRHi, "index"},
	{cpuLGDT, "at"}, {cpuLIDT, "at"}, {cpuSGDT, "at"}, {cpuSIDT, "at"}, {cpuLTR, "selector"},
	{cpuWriteCR, "n, v"}, {cpuReadCR, "n"}, {cpuInvlpg, "at"},
	{cpuFXSave, "at"}, {cpuFXRstor, "at"}, {cpuFNSave, "at"}, {cpuFRstor, "at"}, {cpuFRstorST, "at, index"}, {cpuFXRstorST, "at, index"},
}

// The names the blocks reach the processor operations by, for a check that every operation the lift names is emitted
func cpuOperationNames() []string {
	out := make([]string, 0, len(cpuOperations))
	for _, op := range cpuOperations {
		out = append(out, op.name)
	}
	out = append(out, strings.TrimSuffix(cpuCPUID, "_"))
	sort.Strings(out)
	return out
}
