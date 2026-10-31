package xbe

import (
	"strings"
	"testing"
)

// A routine reading one stack input, a table lookup by it pushed to a sink after an equality or a bound test
func exploreTextChunks() []chunk {
	return []chunk{
		lbl("kPinned"), by(0x8B, 0x44, 0x24, 0x04), by(0x83, 0xF8, 0x01), jne8("kPinnedOut"),
		abs32([]byte{0x0F, 0xB6, 0x88}, "kTable", 0), by(0x51), call("kSink"), lbl("kPinnedOut"), retn(4), pad(),
		lbl("kBounded"), by(0x8B, 0x44, 0x24, 0x04), by(0x83, 0xF8, 0x02), ja8("kBoundedOut"),
		abs32([]byte{0x0F, 0xB6, 0x88}, "kTable", 0), by(0x51), call("kSink"), lbl("kBoundedOut"), retn(4), pad(),
		lbl("kSink"), movEAXImm(0), retn(4), pad(),
		// mov eax,[esp+4]; cmp eax,1; jne out; or byte [kTable+eax],80h; movzx ecx,byte [kTable+eax]; push ecx; call kSink
		lbl("kWriteBack"), by(0x8B, 0x44, 0x24, 0x04), by(0x83, 0xF8, 0x01), jne8("kWriteBackOut"),
		abs32([]byte{0x80, 0x88}, "kTable", 0, 0x80), abs32([]byte{0x0F, 0xB6, 0x88}, "kTable", 0), by(0x51), call("kSink"),
		lbl("kWriteBackOut"), retn(4), pad(),
		// mov eax,[esp+4]; cmp eax,1; jne out; movzx ecx,byte [windowTable+eax]; push ecx; call kSink
		lbl("kWindow"), by(0x8B, 0x44, 0x24, 0x04), by(0x83, 0xF8, 0x01), jne8("kWindowOut"),
		by(0x0F, 0xB6, 0x88), by(windowTable&0xFF, windowTable>>8&0xFF, windowTable>>16&0xFF, windowTable>>24), by(0x51), call("kSink"),
		lbl("kWindowOut"), retn(4), pad(),
	}
}

// A table at a device address an overlay answers, the way a flash window the kernel maps reads the flash
const windowTable = 0xC0FF0000

func exploreDataChunks() []chunk {
	return []chunk{lbl("kTable"), by(0x10, 0x20, 0x30, 0x40)}
}

type sinkCall struct {
	value  uint32
	pinned bool
	shadow string
}

// Explores a routine with its one input open, the sink recording what it is handed and whether the path pins it
func exploreRoutine(t *testing.T, label string, paths int) ([]sinkCall, *Explorer, error) {
	t.Helper()
	ti := buildKernelImage(t, exploreTextChunks(), exploreDataChunks(), map[uint32]string{104: "kPinned", 105: "kBounded", 106: "kSink", 107: "kWriteBack", 108: "kWindow"})
	mm := machineModel(t, ti, label)
	var calls []sinkCall
	ex := &Explorer{Model: mm, Steps: ExploreSteps, Paths: paths}
	ex.Prepare = func() *MachineRun {
		r := NewMachineRun(mm, ti.img, map[string]uint32{"ESP": ReplayStack})
		r.Overlays = append(r.Overlays, Overlay{Base: windowTable, Size: 0x1000, Data: []byte{0x50, 0x60, 0x70, 0x80}})
		r.OpenShadows(map[uint32]bool{}, func(addr uint32) bool { return addr >= 0xC0000000 })
		r.Stop[ReplayReturn] = true
		r.Hooks[ti.at(t, "kSink")] = func(r *MachineRun) uint32 {
			esp := r.Regs["ESP"]
			c := sinkCall{value: r.ReadU32(esp + 4)}
			if sh := r.OpenAt(esp+4, 4); sh != nil {
				v, pinned, err := r.Pinned(sh)
				if err != nil {
					t.Fatalf("pinned: %v", err)
				}
				if v != c.value {
					t.Fatalf("open value reads %X, the run holds %X", v, c.value)
				}
				c.pinned, c.shadow = pinned, ExprString(sh)
			}
			calls = append(calls, c)
			ret := r.Pop()
			r.Pop()
			r.Regs["EAX"] = 0
			return ret
		}
		return r
	}
	ex.Enter = func(r *MachineRun, entry uint32) {
		r.PushOpen()
		r.Push(ReplayReturn)
	}
	err := ex.Explore([]uint32{ti.at(t, label)})
	return calls, ex, err
}

func TestExplorerPinsTableLookupByEqualityTest(t *testing.T) {
	calls, ex, err := exploreRoutine(t, "kPinned", ExplorePaths)
	if err != nil {
		t.Fatal(err)
	}
	if _, paths, _ := ex.Stats(); paths != 2 {
		t.Fatalf("%d paths, want 2", paths)
	}
	if len(calls) != 1 || calls[0].value != 0x20 || !calls[0].pinned {
		t.Fatalf("sink calls %+v, want one pinned 0x20", calls)
	}
	if !strings.Contains(calls[0].shadow, "mem1[") || !strings.Contains(calls[0].shadow, openPrefix) {
		t.Fatalf("shadow %s is no load over the input", calls[0].shadow)
	}
}

func TestExplorerLeavesBoundedTableLookupOpen(t *testing.T) {
	calls, ex, err := exploreRoutine(t, "kBounded", ExplorePaths)
	if err != nil {
		t.Fatal(err)
	}
	if _, paths, _ := ex.Stats(); paths != 2 {
		t.Fatalf("%d paths, want 2", paths)
	}
	if len(calls) != 1 || calls[0].value != 0x10 || calls[0].pinned {
		t.Fatalf("sink calls %+v, want one open 0x10", calls)
	}
}

// A table byte read through the input, written back changed and read again reads the run's own store, the run pinning it
func TestExplorerReadsBackStoreThroughOpenAddress(t *testing.T) {
	calls, ex, err := exploreRoutine(t, "kWriteBack", ExplorePaths)
	if err != nil {
		t.Fatal(err)
	}
	if _, paths, _ := ex.Stats(); paths != 2 {
		t.Fatalf("%d paths, want 2", paths)
	}
	if len(calls) != 1 || calls[0].value != 0xA0 || !calls[0].pinned {
		t.Fatalf("sink calls %+v, want one pinned 0xA0", calls)
	}
	if strings.Count(calls[0].shadow, "mem1[") != 1 {
		t.Fatalf("shadow %s should hold one load over the input", calls[0].shadow)
	}
}

// A table read through a window over data the run overlays reads the data, not a device, so the lookup pins
func TestExplorerPinsTableLookupThroughOverlaidWindow(t *testing.T) {
	calls, ex, err := exploreRoutine(t, "kWindow", ExplorePaths)
	if err != nil {
		t.Fatal(err)
	}
	if _, paths, _ := ex.Stats(); paths != 2 {
		t.Fatalf("%d paths, want 2", paths)
	}
	if len(calls) != 1 || calls[0].value != 0x60 || !calls[0].pinned {
		t.Fatalf("sink calls %+v, want one pinned 0x60", calls)
	}
}

func TestExplorerPathBudget(t *testing.T) {
	_, _, err := exploreRoutine(t, "kPinned", 1)
	if err == nil || !strings.Contains(err.Error(), "1 paths open at once") {
		t.Fatalf("error %v", err)
	}
}
