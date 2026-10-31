package bios

import (
	"fmt"
	"slices"
)

// The operations the MCPX ROM interprets from the init table, by opcode
const (
	xcodePeek     = 0x02
	xcodePoke     = 0x03
	xcodePCIWrite = 0x04
	xcodePCIRead  = 0x05
	xcodeAndOr    = 0x06
	xcodeChain    = 0x07
	xcodeJNE      = 0x08
	xcodeJump     = 0x09
	xcodeOut      = 0x11
	xcodeIn       = 0x12
	xcodeSize     = 9
)

// The spaces an x-code addresses
const (
	SpaceMemory = "memory"
	SpacePCI    = "pci"
	SpacePort   = "port"
)

// InitRead is a register the init table reads, with every test the ROM applies to the value before deciding a jump
type InitRead struct {
	At      int        `json:"at"`
	Space   string     `json:"space"`
	Address uint32     `json:"address"`
	Tests   []InitTest `json:"tests,omitempty"`
}

// InitTest is one comparison of a masked and ored reading against a constant
type InitTest struct {
	And   uint32 `json:"and"`
	Or    uint32 `json:"or"`
	Value uint32 `json:"value"`
}

// InitWrite is a register the init table writes, the value fixed or a reading, indexed among the reads, masked and ored
type InitWrite struct {
	At      int    `json:"at"`
	Space   string `json:"space"`
	Address uint32 `json:"address"`
	Value   uint32 `json:"value"`
	Read    int    `json:"read"`
	And     uint32 `json:"and,omitempty"`
	Or      uint32 `json:"or,omitempty"`
}

// InitTable is every register the init table touches on any path the hardware's answers can take, in table order
type InitTable struct {
	Reads  []InitRead  `json:"reads"`
	Writes []InitWrite `json:"writes"`
}

// accValue is the accumulator as the interpreter tracks it, a constant or a reading masked and ored
type accValue struct {
	known bool
	value uint32
	read  int
	and   uint32
	or    uint32
}

// RunInitTable interprets the x-codes along every path, forking where a jump decides on a reading, an opcode the
// ROM has no operation for passed over
func RunInitTable(codes []XCode) (*InitTable, error) {
	t := &InitTable{}
	readAt := map[int]int{}
	type state struct {
		ip  int
		acc accValue
	}
	seen := map[state]bool{}
	queue := []state{{0, accValue{known: true}}}
	reading := func(ip int, space string, addr uint32) accValue {
		i, ok := readAt[ip]
		if !ok {
			i = len(t.Reads)
			t.Reads = append(t.Reads, InitRead{At: ip, Space: space, Address: addr})
			readAt[ip] = i
		}
		return accValue{read: i, and: 0xFFFFFFFF}
	}
	write := func(ip int, space string, addr uint32, v accValue) {
		w := InitWrite{At: ip, Space: space, Address: addr, Value: v.value, Read: -1}
		if !v.known {
			w.Read, w.And, w.Or = v.read, v.and, v.or
		}
		for _, prev := range t.Writes {
			if prev == w {
				return
			}
		}
		t.Writes = append(t.Writes, w)
	}
	for len(queue) > 0 {
		s := queue[0]
		queue = queue[1:]
		if s.ip < 0 || s.ip >= len(codes) {
			return nil, fmt.Errorf("bios: the init table jumps to x-code %d of %d", s.ip, len(codes))
		}
		if seen[s] {
			continue
		}
		seen[s] = true
		x := codes[s.ip]
		next := state{s.ip + 1, s.acc}
		switch x.Op {
		case xcodePeek:
			next.acc = reading(s.ip, SpaceMemory, x.A)
		case xcodePCIRead:
			next.acc = reading(s.ip, SpacePCI, x.A)
		case xcodeIn:
			next.acc = reading(s.ip, SpacePort, x.A)
		case xcodePoke:
			write(s.ip, SpaceMemory, x.A, accValue{known: true, value: x.B})
		case xcodePCIWrite:
			write(s.ip, SpacePCI, x.A, accValue{known: true, value: x.B})
		case xcodeOut:
			write(s.ip, SpacePort, x.A, accValue{known: true, value: x.B})
		case xcodeAndOr:
			if s.acc.known {
				next.acc.value = s.acc.value&x.A | x.B
			} else {
				next.acc.and = s.acc.and & x.A
				next.acc.or = s.acc.or&x.A | x.B
			}
		case xcodeChain:
			switch x.A {
			case xcodePoke:
				write(s.ip, SpaceMemory, x.B, s.acc)
			case xcodePCIWrite:
				write(s.ip, SpacePCI, x.B, s.acc)
			case xcodeOut:
				write(s.ip, SpacePort, x.B, s.acc)
			default:
				return nil, fmt.Errorf("bios: x-code %d chains opcode %02X", s.ip, x.A)
			}
		case xcodeJNE:
			target := state{s.ip + 1 + int(int32(x.B))/xcodeSize, s.acc}
			if s.acc.known {
				if s.acc.value != x.A {
					next = target
				}
				break
			}
			r := &t.Reads[s.acc.read]
			test := InitTest{And: s.acc.and, Or: s.acc.or, Value: x.A}
			if !containsTest(r.Tests, test) {
				r.Tests = append(r.Tests, test)
			}
			queue = append(queue, target)
		case xcodeJump:
			next.ip = s.ip + 1 + int(int32(x.B))/xcodeSize
		case xcodeExit:
			continue
		}
		queue = append(queue, next)
	}
	return t, nil
}

// The memory addresses the table writes a constant to and reads back whole expecting that constant, memory under
// test rather than registers
func (t *InitTable) memoryTests() map[uint32]bool {
	written := map[uint32]map[uint32]bool{}
	for _, w := range t.Writes {
		if w.Space != SpaceMemory || w.Read >= 0 {
			continue
		}
		if written[w.Address] == nil {
			written[w.Address] = map[uint32]bool{}
		}
		written[w.Address][w.Value] = true
	}
	out := map[uint32]bool{}
	for _, r := range t.Reads {
		if r.Space != SpaceMemory {
			continue
		}
		for _, test := range r.Tests {
			if test.And == ^uint32(0) && test.Or == 0 && written[r.Address][test.Value] {
				out[r.Address] = true
			}
		}
	}
	return out
}

func containsTest(tests []InitTest, t InitTest) bool {
	return slices.Contains(tests, t)
}
