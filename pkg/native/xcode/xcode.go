// Package xcode assembles and disassembles Xbox init tables, the x-code records the boot ROM interprets
package xcode

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"strconv"
	"strings"
)

const (
	HeaderSize = 0x80
	recordSize = 9
	opMemRead  = 0x02
	opMemWrite = 0x03
	opPCIWrite = 0x04
	opPCIRead  = 0x05
	opAndOr    = 0x06
	opUseRes   = 0x07
	opJNE      = 0x08
	opJMP      = 0x09
	opAccum    = 0x10
	opIOWrite  = 0x11
	opIORead   = 0x12
	opExit     = 0xEE
)

var mnemonics = map[byte]string{
	opMemRead: "mem_read", opMemWrite: "mem_write", opPCIWrite: "pci_write", opPCIRead: "pci_read",
	opAndOr: "and_or", opUseRes: "use_result", opJNE: "jne", opJMP: "jmp",
	opAccum: "accum", opIOWrite: "io_write", opIORead: "io_read", opExit: "exit",
}

var opcodes = func() map[string]byte {
	m := map[string]byte{}
	for op, name := range mnemonics {
		m[name] = op
	}
	return m
}()

// Multi byte no-op sequences MASM emits for code alignment, seven bytes and under
var npad = [][]byte{
	nil,
	{0x90},
	{0x8B, 0xFF},
	{0x8D, 0x49, 0x00},
	{0x8D, 0x64, 0x24, 0x00},
	{0x05, 0x00, 0x00, 0x00, 0x00},
	{0x8D, 0x9B, 0x00, 0x00, 0x00, 0x00},
	{0x8D, 0xA4, 0x24, 0x00, 0x00, 0x00, 0x00},
}

// Returns the MASM padding sequence of the given length, longer runs chaining seven byte pads
func Pad(n int) []byte {
	var out []byte
	for n > 7 {
		out = append(out, npad[7]...)
		n -= 7
	}
	return append(out, npad[n]...)
}

type item struct {
	line  int
	pos   int
	size  int
	bytes []byte
	op    byte
	a     uint32
	b     uint32
	jump  string
}

// Translates init table source into the bytes the flash carries
func Assemble(src []byte) ([]byte, error) {
	var items []*item
	labels := map[string]int{}
	pos := 0
	for n, raw := range strings.Split(string(src), "\n") {
		line := strings.TrimSpace(raw)
		if i := strings.IndexByte(line, ';'); i >= 0 && !strings.Contains(line[:i], "\"") {
			line = strings.TrimSpace(line[:i])
		}
		if line == "" {
			continue
		}
		if strings.HasSuffix(line, ":") && !strings.ContainsAny(line, " \t\"") {
			name := line[:len(line)-1]
			if _, dup := labels[name]; dup {
				return nil, fmt.Errorf("xcode: line %d: label %s defined twice", n+1, name)
			}
			labels[name] = pos
			continue
		}
		it, err := parse(line, pos)
		if err != nil {
			return nil, fmt.Errorf("xcode: line %d: %v", n+1, err)
		}
		it.line, it.pos = n+1, pos
		pos += it.size
		items = append(items, it)
	}
	out := make([]byte, 0, pos)
	for _, it := range items {
		if it.jump == "" {
			out = append(out, it.bytes...)
			continue
		}
		target, ok := labels[it.jump]
		if !ok {
			v, err := strconv.ParseInt(it.jump, 0, 64)
			if err != nil || v < 0 || v > 0xFFFFFFFF {
				return nil, fmt.Errorf("xcode: line %d: unknown label %s", it.line, it.jump)
			}
			target = int(v)
		}
		out = append(out, record(it.op, it.a, uint32(int32(target-(it.pos+recordSize))))...)
	}
	return out, nil
}

// Parses one source line into the bytes or record it stands for
func parse(line string, pos int) (*item, error) {
	word, rest, _ := strings.Cut(line, " ")
	word = strings.ToLower(word)
	rest = strings.TrimSpace(rest)
	switch word {
	case "db", "dw", "dd":
		width := map[string]int{"db": 1, "dw": 2, "dd": 4}[word]
		vals, err := numbers(rest)
		if err != nil {
			return nil, err
		}
		var out []byte
		for _, v := range vals {
			if v>>(8*uint(width)) != 0 {
				return nil, fmt.Errorf("%#x does not fit %s", v, word)
			}
			out = binary.LittleEndian.AppendUint32(out, uint32(v))[:len(out)+width]
		}
		return &item{size: len(out), bytes: out}, nil
	case "ascii", "asciz":
		s, err := strconv.Unquote(rest)
		if err != nil {
			return nil, fmt.Errorf("%s wants a quoted string, got %s", word, rest)
		}
		out := []byte(s)
		if word == "asciz" {
			out = append(out, 0)
		}
		return &item{size: len(out), bytes: out}, nil
	case "space":
		vals, err := numbers(rest)
		if err != nil || len(vals) != 1 {
			return nil, fmt.Errorf("space wants one count, got %s", rest)
		}
		return &item{size: int(vals[0]), bytes: make([]byte, vals[0])}, nil
	case "align":
		fields := split(rest)
		if len(fields) == 0 || len(fields) > 2 || (len(fields) == 2 && fields[1] != "code") {
			return nil, fmt.Errorf("align wants a boundary and optionally code, got %s", rest)
		}
		vals, err := numbers(fields[0])
		if err != nil || len(vals) != 1 || vals[0] == 0 {
			return nil, fmt.Errorf("align wants a boundary, got %s", fields[0])
		}
		n := (int(vals[0]) - pos%int(vals[0])) % int(vals[0])
		if len(fields) == 2 {
			return &item{size: n, bytes: Pad(n)}, nil
		}
		return &item{size: n, bytes: make([]byte, n)}, nil
	case "xcode":
		vals, err := numbers(rest)
		if err != nil || len(vals) != 3 || vals[0] > 0xFF {
			return nil, fmt.Errorf("xcode wants an opcode and two operands, got %s", rest)
		}
		return &item{size: recordSize, bytes: record(byte(vals[0]), uint32(vals[1]), uint32(vals[2]))}, nil
	}
	op, ok := opcodes[word]
	if !ok {
		return nil, fmt.Errorf("unknown directive %s", word)
	}
	fields := split(rest)
	switch op {
	case opJNE, opJMP:
		want := 2
		if op == opJMP {
			want = 1
		}
		if len(fields) != want {
			return nil, fmt.Errorf("%s wants %d operands, got %s", word, want, rest)
		}
		var a uint32
		if op == opJNE {
			vals, err := numbers(fields[0])
			if err != nil {
				return nil, err
			}
			a = uint32(vals[0])
		}
		return &item{size: recordSize, op: op, a: a, jump: fields[len(fields)-1]}, nil
	case opExit:
		if rest == "" {
			return &item{size: recordSize, bytes: record(op, 0, 0)}, nil
		}
		vals, err := numbers(rest)
		if err != nil || len(vals) != 1 {
			return nil, fmt.Errorf("exit wants at most one operand, got %s", rest)
		}
		return &item{size: recordSize, bytes: record(op, uint32(vals[0]), 0)}, nil
	case opMemRead, opPCIRead, opIORead:
		vals, err := numbers(rest)
		if err != nil || len(vals) != 1 {
			return nil, fmt.Errorf("%s wants one operand, got %s", word, rest)
		}
		return &item{size: recordSize, bytes: record(op, uint32(vals[0]), 0)}, nil
	}
	vals, err := numbers(rest)
	if err != nil || len(vals) != 2 {
		return nil, fmt.Errorf("%s wants two operands, got %s", word, rest)
	}
	return &item{size: recordSize, bytes: record(op, uint32(vals[0]), uint32(vals[1]))}, nil
}

func split(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	fields := strings.Split(s, ",")
	for i := range fields {
		fields[i] = strings.TrimSpace(fields[i])
	}
	return fields
}

// Parses a comma separated list of integers, each within thirty two bits
func numbers(s string) ([]uint64, error) {
	var out []uint64
	for _, f := range split(s) {
		v, err := strconv.ParseInt(f, 0, 64)
		if err != nil || v < -0x80000000 || v > 0xFFFFFFFF {
			return nil, fmt.Errorf("bad number %s", f)
		}
		out = append(out, uint64(uint32(v)))
	}
	if out == nil {
		return nil, fmt.Errorf("missing operands")
	}
	return out, nil
}

func record(op byte, a, b uint32) []byte {
	out := []byte{op}
	out = binary.LittleEndian.AppendUint32(out, a)
	return binary.LittleEndian.AppendUint32(out, b)
}

// Renders a flash init table as source that assembles back to the same bytes
func Disassemble(bin []byte) ([]byte, error) {
	if len(bin) < HeaderSize {
		return nil, fmt.Errorf("xcode: %d bytes is shorter than the %d byte header", len(bin), HeaderSize)
	}
	var w strings.Builder
	dwords(&w, bin[:HeaderSize])
	end := -1
	for pos := HeaderSize; pos+recordSize <= len(bin); pos += recordSize {
		if bin[pos] == opExit {
			end = pos + recordSize
			break
		}
	}
	if end < 0 {
		return nil, fmt.Errorf("xcode: no exit record follows the header")
	}
	targets := map[int]bool{}
	for pos := HeaderSize; pos < end; pos += recordSize {
		if op := bin[pos]; op == opJNE || op == opJMP {
			t := pos + recordSize + int(int32(binary.LittleEndian.Uint32(bin[pos+5:])))
			if t >= HeaderSize && t <= end && (t-HeaderSize)%recordSize == 0 {
				targets[t] = true
			}
		}
	}
	for pos := HeaderSize; pos < end; pos += recordSize {
		if targets[pos] {
			fmt.Fprintf(&w, "L_%04X:\n", pos)
		}
		op, a, b := bin[pos], binary.LittleEndian.Uint32(bin[pos+1:]), binary.LittleEndian.Uint32(bin[pos+5:])
		switch {
		case op == opJNE || op == opJMP:
			t := pos + recordSize + int(int32(b))
			target := fmt.Sprintf("0x%X", t)
			if targets[t] {
				target = fmt.Sprintf("L_%04X", t)
			}
			if t < 0 {
				fmt.Fprintf(&w, "xcode 0x%X, 0x%X, 0x%X\n", op, a, b)
			} else if op == opJNE {
				fmt.Fprintf(&w, "jne 0x%X, %s\n", a, target)
			} else if a == 0 {
				fmt.Fprintf(&w, "jmp %s\n", target)
			} else {
				fmt.Fprintf(&w, "xcode 0x%X, 0x%X, 0x%X\n", op, a, b)
			}
		case op == opExit && b == 0:
			if a == 0 {
				w.WriteString("exit\n")
			} else {
				fmt.Fprintf(&w, "exit 0x%X\n", a)
			}
		case (op == opMemRead || op == opPCIRead || op == opIORead) && b == 0:
			fmt.Fprintf(&w, "%s 0x%X\n", mnemonics[op], a)
		case op == opMemWrite || op == opPCIWrite || op == opAndOr || op == opUseRes || op == opAccum || op == opIOWrite:
			fmt.Fprintf(&w, "%s 0x%X, 0x%X\n", mnemonics[op], a, b)
		default:
			fmt.Fprintf(&w, "xcode 0x%X, 0x%X, 0x%X\n", op, a, b)
		}
	}
	if targets[end] {
		fmt.Fprintf(&w, "L_%04X:\n", end)
	}
	pos := end
	if n := (16 - pos%16) % 16; n > 0 && pos+n <= len(bin) {
		if bytes.Equal(bin[pos:pos+n], Pad(n)) {
			w.WriteString("align 16, code\n")
			pos += n
		} else if bytes.Equal(bin[pos:pos+n], make([]byte, n)) {
			w.WriteString("align 16\n")
			pos += n
		}
	}
	data(&w, bin[pos:])
	return []byte(w.String()), nil
}

// Writes dword directives, folding runs of four or more zero dwords into space
func dwords(w *strings.Builder, b []byte) {
	for i := 0; i < len(b); {
		if zeros := zeroRun(b[i:]) / 4 * 4; zeros >= 16 {
			fmt.Fprintf(w, "space %d\n", zeros)
			i += zeros
			continue
		}
		var vals []string
		for ; i+4 <= len(b) && len(vals) < 4 && zeroRun(b[i:])/4*4 < 16; i += 4 {
			vals = append(vals, fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(b[i:])))
		}
		if len(vals) == 0 {
			bytesLine(w, b[i:])
			return
		}
		fmt.Fprintf(w, "dd %s\n", strings.Join(vals, ", "))
	}
}

// Writes byte directives, folding zero runs into space and text into asciz
func data(w *strings.Builder, b []byte) {
	for i := 0; i < len(b); {
		if zeros := zeroRun(b[i:]); zeros >= 8 {
			fmt.Fprintf(w, "space %d\n", zeros)
			i += zeros
			continue
		}
		if n, terminated := textRun(b[i:]); n >= 8 {
			if terminated {
				fmt.Fprintf(w, "asciz %s\n", strconv.Quote(string(b[i:i+n])))
				i++
			} else {
				fmt.Fprintf(w, "ascii %s\n", strconv.Quote(string(b[i:i+n])))
			}
			i += n
			continue
		}
		j := i + 1
		for j < len(b) && j-i < 16 && zeroRun(b[j:]) < 8 && !isText(b[j:]) {
			j++
		}
		bytesLine(w, b[i:j])
		i = j
	}
}

func bytesLine(w *strings.Builder, b []byte) {
	vals := make([]string, len(b))
	for i, v := range b {
		vals[i] = fmt.Sprintf("0x%02X", v)
	}
	fmt.Fprintf(w, "db %s\n", strings.Join(vals, ", "))
}

func zeroRun(b []byte) int {
	n := 0
	for n < len(b) && b[n] == 0 {
		n++
	}
	return n
}

// Counts printable bytes that run to a terminator or the end, zero when they stop at other data
func textRun(b []byte) (int, bool) {
	n := 0
	for n < len(b) && b[n] >= 0x20 && b[n] < 0x7F {
		n++
	}
	if n == len(b) {
		return n, false
	}
	if b[n] != 0 {
		return 0, false
	}
	return n, true
}

func isText(b []byte) bool {
	n, _ := textRun(b)
	return n >= 8
}
