package xbe

import (
	"fmt"
	"strings"
	"testing"

	"golang.org/x/arch/x86/x86asm"
)

func TestDecompileFunction(t *testing.T) {
	d, at := analysis(t)
	fn := mustFunction(t, d, at["decomp"])
	df := d.Decompile(fn)
	if df.EntryVA != at["decomp"] || df.Name != fmt.Sprintf("sub_%08X", at["decomp"]) || df.Args != 2 || df.Locals != 16 {
		t.Fatalf("decompiled %+v", df)
	}
	wantFrame := []StackVar{{-8, 4, "var_8"}, {-4, 4, "var_4"}, {-2, 2, "var_2"}, {-1, 1, "var_1"}, {8, 4, "arg_0"}}
	if len(df.StackFrame) != len(wantFrame) {
		t.Fatalf("frame %+v", df.StackFrame)
	}
	for i, w := range wantFrame {
		if df.StackFrame[i] != w {
			t.Errorf("frame %d %+v, want %+v", i, df.StackFrame[i], w)
		}
	}
	base := at["decomp"]
	afterVCall := at["decompAfterVCall"]
	afterJne := at["decompLea"]
	skip := at["decompSkip"]
	want := strings.Join([]string{
		fmt.Sprintf("sub_%08X(arg0, arg1) {", base),
		"    int var_8;",
		"    int var_4;",
		"    short var_2;",
		"    byte var_1;",
		"    push(ebp);",
		"    ebp = esp;",
		"    // alloc 16 bytes on stack",
		"    eax = arg_0;",
		"    var_4 = eax;",
		"    var_1 = 7;",
		"    var_2 = 0x1234;",
		`    push("Hello" /* 0x13000 */);`,
		"    push(5);",
		"    (*0x12000)(); // vtable call",
		"",
		fmt.Sprintf("loc_%08X:", at["decompArgsCall"]),
		fmt.Sprintf("    sub_%08X();", at["args"]),
		"",
		fmt.Sprintf("loc_%08X:", at["decompAfterArgs"]),
		"    edx = *(eax);",
		"    (*edx+0x8)(); // vtable call",
		"",
		fmt.Sprintf("loc_%08X:", afterVCall),
		"    eax = 0;",
		"    // cmp eax, 0x100",
		fmt.Sprintf("    if (!=) goto loc_%08X;", skip),
		"",
		fmt.Sprintf("loc_%08X:", afterJne),
		"    eax = &var_4;",
		"    var_8 = eax;",
		"",
		fmt.Sprintf("loc_%08X:", skip),
		"    // leave (esp = ebp; pop ebp)",
		"    return; // cleanup 8 bytes",
		"}",
	}, "\n") + "\n"
	if got := df.Format(); got != want {
		t.Fatalf("decompiled text\n%s\nwant\n%s", got, want)
	}
}

func TestDecompileBlocks(t *testing.T) {
	d, at := analysis(t)
	df := d.Decompile(mustFunction(t, d, at["decomp"]))
	base := at["decomp"]
	starts := []uint32{base, at["decompArgsCall"], at["decompAfterArgs"], at["decompAfterVCall"], at["decompLea"], at["decompSkip"]}
	if len(df.Blocks) != len(starts) {
		t.Fatalf("%d blocks", len(df.Blocks))
	}
	for i, s := range starts {
		b := df.Blocks[i]
		if b.StartVA != s || df.BlockMap[s] != b {
			t.Errorf("block %d starts at %08X, want %08X", i, b.StartVA, s)
		}
		if i+1 < len(starts) && b.EndVA != starts[i+1] {
			t.Errorf("block %d ends at %08X", i, b.EndVA)
		}
	}
	last := df.Blocks[5]
	if last.EndVA != at["decompSkip"]+4 || len(last.Succs) != 0 || !equalU32(last.Preds, []uint32{at["decompAfterVCall"], at["decompLea"]}) {
		t.Errorf("last block %+v", last)
	}
	branch := df.Blocks[3]
	if !equalU32(branch.Succs, []uint32{at["decompLea"], at["decompSkip"]}) || !equalU32(branch.Preds, []uint32{at["decompAfterArgs"]}) {
		t.Errorf("branch block %+v", branch)
	}
	if !equalU32(df.Blocks[0].Succs, []uint32{at["decompArgsCall"]}) || len(df.Blocks[0].Preds) != 0 || len(df.Blocks[0].Insns) != 10 {
		t.Errorf("entry block %+v", df.Blocks[0])
	}
	sw := d.Decompile(mustFunction(t, d, at["switcher"]))
	if len(sw.Blocks) != 6 || !equalU32(sw.BlockMap[at["switcher"]].Succs, []uint32{at["switchJump"], at["default"]}) || len(sw.BlockMap[at["switchJump"]].Succs) != 0 {
		t.Errorf("switch blocks %d %+v", len(sw.Blocks), sw.BlockMap[at["switcher"]])
	}
	if sw.Args != 0 || sw.Locals != 0 || len(sw.StackFrame) != 0 || sw.Stmts[0].Text != fmt.Sprintf("sub_%08X() {", at["switcher"]) {
		t.Errorf("switch frame %+v", sw)
	}
	if text := sw.Format(); !strings.Contains(text, "    goto **(eax*4+0x"+fmt.Sprintf("%X", at["table"])+");\n") || !strings.Contains(text, "    if (> (unsigned)) goto loc_"+fmt.Sprintf("%08X", at["default"])+";\n") {
		t.Errorf("switch text\n%s", text)
	}
	empty := d.Decompile(&Function{EntryVA: 0x15000})
	if len(empty.Blocks) != 0 || empty.Format() != "sub_00015000() {\n}\n" {
		t.Errorf("empty function\n%s", empty.Format())
	}
}

func TestDecompileNamedFunction(t *testing.T) {
	d, at := analysis(t)
	df := d.Decompile(mustFunction(t, d, at["cookieCheck"]))
	if df.Name != "_security_check_cookie" || df.Stmts[0].Text != "_security_check_cookie() {" {
		t.Fatalf("named %+v", df.Stmts[0])
	}
	want := "_security_check_cookie() {\n    // cmp ecx, *(0x12100)\n" + fmt.Sprintf("    if (!=) goto loc_%08X;\n", at["cookieCheck"]+9) + "\n" + fmt.Sprintf("loc_%08X:\n", at["cookieCheck"]+8) + "    return;\n\n" + fmt.Sprintf("loc_%08X:\n", at["cookieCheck"]+9) + "    /* int3 */\n}\n"
	if got := df.Format(); got != want {
		t.Fatalf("text\n%s\nwant\n%s", got, want)
	}
	user := d.Decompile(mustFunction(t, d, at["cdeclUser"]))
	text := user.Format()
	for _, line := range []string{"    push(1);", "    push(2);", "    (*0x12008)(); // vtable call", "    esp += 8;", fmt.Sprintf("    sub_%08X();", at["args"]), fmt.Sprintf("    sub_%08X();", at["thunk"])} {
		if !strings.Contains(text, line+"\n") {
			t.Errorf("missing %q in\n%s", line, text)
		}
	}
	probe := d.Decompile(mustFunction(t, d, at["probeUser"]))
	if !strings.Contains(probe.Format(), "    chkstk();\n") || !strings.Contains(probe.Format(), "    eax = 0x2000;\n") {
		t.Errorf("probe user\n%s", probe.Format())
	}
}

func TestDecompileAll(t *testing.T) {
	d, at := analysis(t)
	all := d.DecompileAll()
	if len(all) != len(d.Functions) {
		t.Fatalf("%d of %d", len(all), len(d.Functions))
	}
	for i := range all {
		if i > 0 && all[i].EntryVA <= all[i-1].EntryVA {
			t.Fatalf("unsorted at %d", i)
		}
		if d.Functions[all[i].EntryVA] == nil {
			t.Fatalf("unknown function %08X", all[i].EntryVA)
		}
	}
	if all[0].EntryVA != at["entry"] || all[len(all)-1].EntryVA != at["libFn"] {
		t.Fatalf("range %08X..%08X", all[0].EntryVA, all[len(all)-1].EntryVA)
	}
}

func TestDecompileInsn(t *testing.T) {
	d, at := analysis(t)
	df := &DecompiledFunc{StackFrame: []StackVar{{-4, 4, "var_4"}, {8, 4, "arg_0"}}}
	cases := []struct {
		seq  []byte
		want string
	}{
		{[]byte{0x55}, "push(ebp);"},
		{[]byte{0x6A, 0x11}, "push(17);"},
		{[]byte{0x68, 0x00, 0x30, 0x01, 0x00}, `push("Hello" /* 0x13000 */);`},
		{[]byte{0x68, 0x00, 0x21, 0x01, 0x00}, "push(0x12100);"},
		{[]byte{0x68, 0x00, 0x01, 0x00, 0x00}, "push(0x100);"},
		{[]byte{0x6A, 0xFF}, "push(0xFFFFFFFF);"},
		{[]byte{0x58}, "eax = pop();"},
		{[]byte{0x8B, 0xC1}, "eax = ecx;"},
		{[]byte{0x8B, 0x45, 0x08}, "eax = arg_0;"},
		{[]byte{0x89, 0x45, 0xFC}, "var_4 = eax;"},
		{[]byte{0x8B, 0x45, 0x0C}, "eax = *(ebp+0xC);"},
		{[]byte{0xA1, 0x00, 0x21, 0x01, 0x00}, "eax = *(0x12100);"},
		{[]byte{0xA1, 0x00, 0x20, 0x01, 0x00}, "eax = KeSetEvent;"},
		{[]byte{0x8B, 0x04, 0x8D, 0x00, 0x21, 0x01, 0x00}, "eax = *(ecx*4+0x12100);"},
		{[]byte{0x8B, 0x44, 0x88, 0x04}, "eax = *(eax+ecx*4+0x4);"},
		{[]byte{0x8B, 0x46, 0xFC}, "eax = *(esi-0x4);"},
		{[]byte{0x8D, 0x45, 0xFC}, "eax = &var_4;"},
		{[]byte{0x8D, 0x44, 0x24, 0x08}, "eax = &esp+0x8;"},
		{[]byte{0x03, 0xC1}, "eax += ecx;"},
		{[]byte{0x83, 0xEC, 0x10}, "// alloc 16 bytes on stack"},
		{[]byte{0x83, 0xE8, 0x01}, "eax -= 1;"},
		{[]byte{0x33, 0xC0}, "eax = 0;"},
		{[]byte{0x33, 0xC1}, "eax ^= ecx;"},
		{[]byte{0x23, 0xC1}, "eax &= ecx;"},
		{[]byte{0x0B, 0xC1}, "eax |= ecx;"},
		{[]byte{0xF7, 0xD0}, "eax = ~eax;"},
		{[]byte{0xF7, 0xD8}, "eax = -eax;"},
		{[]byte{0xC1, 0xE0, 0x02}, "eax <<= 2;"},
		{[]byte{0xC1, 0xE8, 0x02}, "eax >>= 2;"},
		{[]byte{0xC1, 0xF8, 0x02}, "eax = (signed)eax >> 2;"},
		{[]byte{0x0F, 0xAF, 0xC1}, "eax *= ecx;"},
		{[]byte{0x40}, "eax++;"},
		{[]byte{0x48}, "eax--;"},
		{[]byte{0x3D, 0x00, 0x01, 0x00, 0x00}, "// cmp eax, 0x100"},
		{[]byte{0x85, 0xC0}, "// test eax, eax"},
		{[]byte{0xE8, 0x00, 0x00, 0x00, 0x00}, "sub_00011005();"},
		{[]byte{0xFF, 0x15, 0x00, 0x20, 0x01, 0x00}, "(*0x12000)(); // vtable call"},
		{[]byte{0xFF, 0x52, 0x08}, "(*edx+0x8)(); // vtable call"},
		{[]byte{0xFF, 0xD0}, "(*eax)();"},
		{[]byte{0xC3}, "return;"},
		{[]byte{0xC2, 0x08, 0x00}, "return; // cleanup 8 bytes"},
		{[]byte{0x90}, ""},
		{[]byte{0xC9}, "// leave (esp = ebp; pop ebp)"},
		{[]byte{0xEB, 0x10}, "goto loc_00011012;"},
		{[]byte{0xFF, 0xE0}, "goto *eax;"},
		{[]byte{0xFF, 0x24, 0x85, 0x00, 0x21, 0x01, 0x00}, "goto **(eax*4+0x12100);"},
		{[]byte{0x74, 0x05}, "if (==) goto loc_00011007;"},
		{[]byte{0x75, 0x05}, "if (!=) goto loc_00011007;"},
		{[]byte{0x7C, 0x05}, "if (< (signed)) goto loc_00011007;"},
		{[]byte{0x7E, 0x05}, "if (<= (signed)) goto loc_00011007;"},
		{[]byte{0x7F, 0x05}, "if (> (signed)) goto loc_00011007;"},
		{[]byte{0x7D, 0x05}, "if (>= (signed)) goto loc_00011007;"},
		{[]byte{0x72, 0x05}, "if (< (unsigned)) goto loc_00011007;"},
		{[]byte{0x76, 0x05}, "if (<= (unsigned)) goto loc_00011007;"},
		{[]byte{0x77, 0x05}, "if (> (unsigned)) goto loc_00011007;"},
		{[]byte{0x73, 0x05}, "if (>= (unsigned)) goto loc_00011007;"},
		{[]byte{0x78, 0x05}, "if (sign) goto loc_00011007;"},
		{[]byte{0x79, 0x05}, "if (!sign) goto loc_00011007;"},
		{[]byte{0x70, 0x05}, "if (overflow) goto loc_00011007;"},
		{[]byte{0x71, 0x05}, "if (!overflow) goto loc_00011007;"},
		{[]byte{0x7A, 0x05}, "if (parity) goto loc_00011007;"},
		{[]byte{0x7B, 0x05}, "if (!parity) goto loc_00011007;"},
		{[]byte{0x0F, 0xB6, 0xC0}, "eax = (unsigned)al;"},
		{[]byte{0x0F, 0xBE, 0xC0}, "eax = (signed)al;"},
		{[]byte{0x99}, "edx = (eax >> 31); // sign-extend eax to edx:eax"},
		{[]byte{0x94}, "swap(esp, eax);"},
		{[]byte{0xF3, 0xA5}, "*(int*)edi = *(int*)esi; esi += 4; edi += 4;"},
		{[]byte{0xA4}, "*edi = *esi; esi++; edi++;"},
		{[]byte{0xF3, 0xAB}, "*(int*)edi = eax; edi += 4;"},
		{[]byte{0xAA}, "*edi = al; edi++;"},
		{[]byte{0xA6}, "// cmpsb (compare *esi, *edi)"},
		{[]byte{0xAE}, "// scasb (scan for al in *edi)"},
		{[]byte{0xDD, 0x45, 0x08}, "fpu_push(arg_0);"},
		{[]byte{0xD9, 0x5D, 0xFC}, "var_4 = fpu_pop();"},
		{[]byte{0xD9, 0x55, 0xFC}, "var_4 = fpu_top();"},
		{[]byte{0xDB, 0x45, 0x08}, "fpu_push((float)arg_0);"},
		{[]byte{0xDB, 0x5D, 0xFC}, "var_4 = (int)fpu_pop();"},
		{[]byte{0xD8, 0xC1}, "fpu_top() += f1;"},
		{[]byte{0xD8, 0x45, 0x08}, "fpu_top() += arg_0;"},
		{[]byte{0xDE, 0xC1}, "st1 += st0; fpu_pop();"},
		{[]byte{0xD8, 0xE1}, "fpu_top() -= f1;"},
		{[]byte{0xDE, 0xE9}, "st1 -= st0; fpu_pop();"},
		{[]byte{0xD8, 0xC9}, "fpu_top() *= f1;"},
		{[]byte{0xDE, 0xC9}, "st1 *= st0; fpu_pop();"},
		{[]byte{0xD8, 0xF1}, "fpu_top() /= f1;"},
		{[]byte{0xDE, 0xF9}, "st1 /= st0; fpu_pop();"},
		{[]byte{0xD8, 0xD1}, "// fcompare st0, f1"},
		{[]byte{0xD8, 0xD9}, "// fcompare st0, f1"},
		{[]byte{0xDE, 0xD9}, "// fcompare st0, st1"},
		{[]byte{0xD9, 0xE0}, "st0 = -st0;"},
		{[]byte{0xD9, 0xE1}, "st0 = fabs(st0);"},
		{[]byte{0xD9, 0xFA}, "st0 = sqrt(st0);"},
		{[]byte{0xD9, 0xEE}, "fpu_push(0.0);"},
		{[]byte{0xD9, 0xE8}, "fpu_push(1.0);"},
		{[]byte{0xD9, 0xC9}, "swap(st0, st1);"},
		{[]byte{0xDF, 0xE0}, "ax = fpu_status();"},
		{[]byte{0xD9, 0x7D, 0xFE}, "*(ebp-0x2) = fpu_control();"},
		{[]byte{0xD9, 0x6D, 0xFE}, "fpu_set_control(*(ebp-0x2));"},
		{[]byte{0x9E}, "flags = ah;"},
		{[]byte{0x0F, 0xC8}, "/* bswap eax */"},
		{[]byte{0xD9, 0xFE}, "/* fsin st0 */"},
	}
	for _, c := range cases {
		in := decodeAt(t, 0x11000, c.seq...)
		if got := df.decompileInsn(d, &in); got != c.want {
			t.Errorf("%x decompiles to %q, want %q", c.seq, got, c.want)
		}
	}
	named := decodeAt(t, at["cookieCall"], 0xE8, 0, 0, 0, 0)
	named.Inst.Args[0] = x86asm.Rel(int32(at["cookieCheck"] - (at["cookieCall"] + 5)))
	if got := df.decompileInsn(d, &named); got != "_security_check_cookie();" {
		t.Errorf("named call %q", got)
	}
	if got := df.decompileInsn(d, &Instruction{Inst: x86asm.Inst{Op: x86asm.CALL, Args: x86asm.Args{x86asm.Imm(0x12000)}}}); got != "KeSetEvent();" {
		t.Errorf("import call %q", got)
	}
}

func TestFormatArguments(t *testing.T) {
	d, _ := analysis(t)
	df := &DecompiledFunc{StackFrame: []StackVar{{-8, 4, "var_8"}}}
	in := &Instruction{VA: 0x11000, Inst: x86asm.Inst{Len: 5}}
	if got := df.fmtArg(d, nil, in); got != "?" {
		t.Errorf("nil %q", got)
	}
	if got := df.fmtArg(d, x86asm.Rel(0x10), in); got != "0x11015" {
		t.Errorf("rel %q", got)
	}
	if got := df.fmtArg(d, x86asm.Imm(255), in); got != "255" {
		t.Errorf("small %q", got)
	}
	if got := df.fmtArg(d, x86asm.Imm(256), in); got != "0x100" {
		t.Errorf("hex %q", got)
	}
	if got := df.fmtArg(d, x86asm.Imm(-1), in); got != "0xFFFFFFFF" {
		t.Errorf("negative %q", got)
	}
	if got := df.fmtArg(d, x86asm.Imm(0x13000), in); got != `"Hello" /* 0x13000 */` {
		t.Errorf("string %q", got)
	}
	if got := df.fmtArg(d, x86asm.Imm(0x13008), in); got != "0x13008" {
		t.Errorf("short string %q", got)
	}
	if got := df.fmtArg(d, x86asm.Imm(0x12010), in); got != "0x12010" {
		t.Errorf("non string data %q", got)
	}
	if got := df.fmtArg(d, x86asm.ECX, in); got != "ecx" {
		t.Errorf("register %q", got)
	}
	if got := df.fmtMemAddr(x86asm.Mem{Base: x86asm.EBP, Disp: -8}); got != "var_8" {
		t.Errorf("frame variable %q", got)
	}
	if got := df.fmtMemAddr(x86asm.Mem{Base: x86asm.EBP, Disp: -12}); got != "ebp-0xC" {
		t.Errorf("unnamed frame slot %q", got)
	}
	if got := df.fmtMemAddr(x86asm.Mem{Base: x86asm.EBP, Disp: 16}); got != "ebp+0x10" {
		t.Errorf("positive frame slot %q", got)
	}
	if got := df.fmtMemAddr(x86asm.Mem{Base: x86asm.EAX, Index: x86asm.ECX, Scale: 2, Disp: -4}); got != "eax+ecx*2-0x4" {
		t.Errorf("scaled %q", got)
	}
	if got := df.fmtMemAddr(x86asm.Mem{Index: x86asm.ECX, Scale: 1}); got != "ecx" {
		t.Errorf("unscaled index %q", got)
	}
	if got := df.fmtMemAddr(x86asm.Mem{}); got != "0" {
		t.Errorf("empty %q", got)
	}
	if got := df.fmtMemAddr(x86asm.Mem{Disp: -1}); got != "0xFFFFFFFF" {
		t.Errorf("absolute %q", got)
	}
	if got := df.fmtMem(d, x86asm.Mem{Disp: 0x12008}); got != "DbgPrint" {
		t.Errorf("import cell %q", got)
	}
	if got := df.fmtMem(d, x86asm.Mem{Base: x86asm.ESI, Disp: 0x12008}); got != "*(esi+0x12008)" {
		t.Errorf("based cell %q", got)
	}
	if got := df.fmtArgOrST(d, x86asm.Args{x86asm.F0, x86asm.F3}); got != "f3" {
		t.Errorf("two registers %q", got)
	}
	if got := df.fmtArgOrST(d, x86asm.Args{x86asm.F2}); got != "f2" {
		t.Errorf("one register %q", got)
	}
	if got := df.fmtArgOrST(d, x86asm.Args{}); got != "st1" {
		t.Errorf("no registers %q", got)
	}
	if got := df.fmtArgOrST(d, x86asm.Args{x86asm.Mem{Base: x86asm.EBP, Disp: -8}}); got != "var_8" {
		t.Errorf("memory %q", got)
	}
	if !isPrintable("Hello world!~") || isPrintable("tab\there") || isPrintable("caf\u00e9") || !isPrintable("") {
		t.Error("printable judgement")
	}
}

func TestResolveCallTarget(t *testing.T) {
	d, at := analysis(t)
	df := &DecompiledFunc{}
	in := &Instruction{VA: 0x11000, Inst: x86asm.Inst{Op: x86asm.CALL, Len: 6, Args: x86asm.Args{x86asm.Mem{Disp: 0x12000}}}}
	if got := df.resolveCallTarget(d, 0, in); got != "(*KeSetEvent)" {
		t.Errorf("indirect import %q", got)
	}
	if got := df.resolveCallTarget(d, 0x12008, in); got != "DbgPrint" {
		t.Errorf("import %q", got)
	}
	if got := df.resolveCallTarget(d, at["chkstk"], in); got != "chkstk" {
		t.Errorf("named %q", got)
	}
	direct := &Instruction{VA: 0x11000, Inst: x86asm.Inst{Op: x86asm.CALL, Len: 5, Args: x86asm.Args{x86asm.Rel(0x10)}}}
	if got := df.resolveCallTarget(d, at["args"], direct); got != fmt.Sprintf("sub_%08X", at["args"]) {
		t.Errorf("unnamed %q", got)
	}
	if got := df.resolveCallTarget(d, 0x15000, direct); got != "sub_00015000" {
		t.Errorf("unknown %q", got)
	}
	if got := df.resolveCallTarget(d, 0x15000, &Instruction{Inst: x86asm.Inst{Op: x86asm.CALL, Args: x86asm.Args{x86asm.Mem{Disp: 0x12008}}}}); got != "DbgPrint" {
		t.Errorf("unknown target through an import cell %q", got)
	}
}

func TestCondNamesAndJumps(t *testing.T) {
	for op, want := range map[x86asm.Op]string{x86asm.JE: "==", x86asm.JNE: "!=", x86asm.JL: "< (signed)", x86asm.JLE: "<= (signed)", x86asm.JG: "> (signed)", x86asm.JGE: ">= (signed)", x86asm.JB: "< (unsigned)", x86asm.JBE: "<= (unsigned)", x86asm.JA: "> (unsigned)", x86asm.JAE: ">= (unsigned)", x86asm.JS: "sign", x86asm.JNS: "!sign", x86asm.JO: "overflow", x86asm.JNO: "!overflow", x86asm.JP: "parity", x86asm.JNP: "!parity", x86asm.JMP: "?", x86asm.JECXZ: "?"} {
		if got := condName(op); got != want {
			t.Errorf("%v names %q, want %q", op, got, want)
		}
	}
	for _, op := range []x86asm.Op{x86asm.JE, x86asm.JNE, x86asm.JA, x86asm.JCXZ, x86asm.JECXZ, x86asm.JNP} {
		if !isConditionalJump(op) || !isJump(op) || isUnconditionalJump(op) {
			t.Errorf("%v classification", op)
		}
	}
	if !isUnconditionalJump(x86asm.JMP) || !isJump(x86asm.JMP) || isConditionalJump(x86asm.JMP) {
		t.Error("jmp classification")
	}
	for _, op := range []x86asm.Op{x86asm.CALL, x86asm.RET, x86asm.MOV, x86asm.LOOP} {
		if isJump(op) {
			t.Errorf("%v a jump", op)
		}
	}
}
