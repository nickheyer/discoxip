package xbe

import (
	"slices"
	"testing"
)

// A word landing on an instruction of the code is a root the page may enter at, one landing on zeroed bytes names data
func TestRuntimeRootsSkipZeroedMemory(t *testing.T) {
	ti := buildImage(t, []chunk{
		lbl("fn"), movEAXImm(1), ret(), pad(),
		lbl("mid"), movEAXImm(2), ret(), pad(),
		lbl("zeros"), rep(16, 0),
	}, []chunk{
		lbl("ptrs"), wordAt("mid", 0), wordAt("zeros", 0), wordAt("zeros", 4),
	}, nil, nil)
	roots := ti.d.runtimeRoots()
	for _, label := range []string{"fn", "mid"} {
		if !slices.Contains(roots, ti.at(t, label)) {
			t.Errorf("%s is no root among %X", label, roots)
		}
	}
	for _, va := range []uint32{ti.at(t, "zeros"), ti.at(t, "zeros") + 4} {
		if slices.Contains(roots, va) {
			t.Errorf("zeroed memory at %X is a root among %X", va, roots)
		}
	}
	if !ti.d.executableAt(ti.at(t, "zeros")) || !zeroedAt(ti.d.insnAt(ti.at(t, "zeros"))) {
		t.Errorf("the zeroed bytes do not decode as a zero encoded instruction")
	}
	if zeroedAt(ti.d.insnAt(ti.at(t, "mid"))) {
		t.Errorf("a routine's first instruction counts as zeroed")
	}
}
