package bios

import (
	"fmt"
	"os"
	"testing"

	"github.com/nickheyer/discoxip/pkg/xbe"
)

func TestProbeFlow(t *testing.T) {
	if os.Getenv("PROBE_FLOW") == "" {
		t.Skip()
	}
	b, err := Open("../../kernels/5713_orig.bin")
	if err != nil {
		t.Fatal(err)
	}
	mcpx, err := LoadMCPX("../../kernels/mcpx-1.1.bin")
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Decrypt(mcpx); err != nil {
		t.Fatal(err)
	}
	lifted, err := b.Machine()
	if err != nil {
		t.Fatal(err)
	}
	dv, err := newDeriver(b, lifted)
	if err != nil {
		t.Fatal(err)
	}
	var va uint32
	fmt.Sscanf(os.Getenv("PROBE_FLOW"), "%x", &va)
	_, entry := dv.flowOf(va)
	st := dv.blockState(va)
	fmt.Printf("routine %08X block %08X stack %v esp %d\n", entry, va, st.stack, st.esp)
	for name, v := range st.regs {
		fmt.Printf("  %s = %s settled %v\n", name, xbe.ExprString(v), settled(v))
	}
	bl := dv.blocks[va]
	cond := dv.inState(bl.Cond, st)
	fmt.Printf("raw cond %s\nresolved %s\n", xbe.ExprString(bl.Cond), xbe.ExprString(cond))
	for _, p := range dv.blocks[va-0x22].Regs {
		_ = p
	}
	prev := dv.blocks[0x8004F4E9]
	pst := dv.blockState(0x8004F4E9)
	for name, e := range prev.Regs {
		v := dv.inBlock(e, pst, prev, dv.viewOf(prev, pst))
		fmt.Printf("  prev %s = %s settled %v\n", name, xbe.ExprString(v), settled(v))
	}
	fmt.Printf("field values %v\n", dv.kernel.fieldValues(entry, xbe.Sym{Name: "reg.ECX"}, 0, 3))
}
