package xbe

import "testing"

func TestRunMemoryCopiesSharedPagesOnWrite(t *testing.T) {
	m := newRunMemory()
	m.set(0x1000, 1)
	m.set(0x1FFF, 2)
	m.set(0x5000, 3)
	c := m.clone()
	if m.pages[1].holders != 2 || m.pages[5].holders != 2 {
		t.Fatalf("holders %d %d", m.pages[1].holders, m.pages[5].holders)
	}
	c.set(0x1001, 9)
	if b, ok := m.get(0x1001); ok {
		t.Fatalf("original holds %d at 1001", b)
	}
	if b, ok := c.get(0x1000); !ok || b != 1 {
		t.Fatalf("clone lost 1000, %d %v", b, ok)
	}
	if m.pages[1] == c.pages[1] || m.pages[1].holders != 1 || c.pages[1].holders != 1 {
		t.Fatalf("page 1 still shared, holders %d %d", m.pages[1].holders, c.pages[1].holders)
	}
	if m.pages[5] != c.pages[5] {
		t.Fatal("page 5 copied without a write")
	}
	m.set(0x5000, 4)
	if b, _ := c.get(0x5000); b != 3 {
		t.Fatalf("clone sees %d at 5000", b)
	}
	if b, _ := m.get(0x5000); b != 4 {
		t.Fatalf("original sees %d at 5000", b)
	}
	if n := m.count(); n != 3 {
		t.Fatalf("count %d", n)
	}
	var addrs []uint32
	c.each(func(addr uint32, b byte) { addrs = append(addrs, addr) })
	want := []uint32{0x1000, 0x1001, 0x1FFF, 0x5000}
	if len(addrs) != len(want) {
		t.Fatalf("each %X", addrs)
	}
	for i := range want {
		if addrs[i] != want[i] {
			t.Fatalf("each %X", addrs)
		}
	}
	if _, ok := m.get(0x2000); ok {
		t.Fatal("unwritten byte reads as written")
	}
}

func TestRunVisitsLayersAndFolds(t *testing.T) {
	v := newRunVisits()
	v.set("a", runVisit{epoch: 1})
	c := v.clone()
	c.set("b", runVisit{epoch: 2})
	v.set("a", runVisit{epoch: 3})
	if last, ok := c.get("a"); !ok || last.epoch != 1 {
		t.Fatalf("clone sees a at %d %v", last.epoch, ok)
	}
	if _, ok := v.get("b"); ok {
		t.Fatal("original sees b")
	}
	if last, ok := v.get("a"); !ok || last.epoch != 3 {
		t.Fatalf("original sees a at %d %v", last.epoch, ok)
	}
	for i := 0; i < visitLayers+3; i++ {
		v.set("k", runVisit{epoch: uint64(i)})
		v.clone()
	}
	if len(v.layers) > visitLayers+1 {
		t.Fatalf("%d layers", len(v.layers))
	}
	if last, ok := v.get("k"); !ok || last.epoch != uint64(visitLayers+2) {
		t.Fatalf("k at %d %v after folding", last.epoch, ok)
	}
	if last, ok := v.get("a"); !ok || last.epoch != 3 {
		t.Fatalf("a at %d %v after folding", last.epoch, ok)
	}
}
