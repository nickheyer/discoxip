// Package native rebuilds the reference source tree and compares each result with the original bytes
package native

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/nickheyer/discoxip/pkg/native/xcode"
)

const (
	KindROM       = "rom"
	KindInitTable = "inittbl"
	KindFile      = "file"
	romBase       = "0xFFFFFE00"
)

// Entry names one source file and the reference bytes it must rebuild, objects adding their member and compiler
type Entry struct {
	Kind      string   `json:"kind"`
	File      string   `json:"file"`
	Reference string   `json:"reference"`
	Member    string   `json:"member,omitempty"`
	CL        string   `json:"cl,omitempty"`
	Flags     []string `json:"flags,omitempty"`
}

// Result is the outcome of rebuilding one entry
type Result struct {
	Entry
	Exact   bool   `json:"exact"`
	Matched int    `json:"matched"`
	Total   int    `json:"total"`
	Unit    string `json:"unit"`
	Detail  string `json:"detail,omitempty"`
	Error   string `json:"error,omitempty"`
}

// Rebuilds one entry and compares it with the reference, the work directory holding intermediates
func Verify(e Entry, work string) Result {
	switch e.Kind {
	case KindObject:
		return verifyObject(e, work)
	case KindBootloader:
		return verifyBootloader(e, work)
	}
	r := Result{Entry: e, Unit: "bytes"}
	ref, err := os.ReadFile(e.Reference)
	if err != nil {
		r.Error = err.Error()
		return r
	}
	r.Total = len(ref)
	if _, err := os.Stat(e.File); err != nil {
		r.Error = "no source"
		return r
	}
	var out []byte
	switch e.Kind {
	case KindROM:
		out, err = BuildROM(e.File, work)
	case KindInitTable:
		out, err = BuildInitTable(e.File)
	case KindFile:
		out, err = os.ReadFile(e.File)
	default:
		err = fmt.Errorf("native: no verifier for kind %q", e.Kind)
	}
	if err != nil {
		r.Error = err.Error()
		return r
	}
	r.Matched, r.Exact = Compare(out, ref)
	return r
}

// Verifies every entry in order
func VerifyAll(entries []Entry, work string) []Result {
	out := make([]Result, len(entries))
	for i, e := range entries {
		out[i] = Verify(e, work)
	}
	return out
}

// Counts the positions whose bytes agree and reports whether the two are identical
func Compare(out, ref []byte) (int, bool) {
	n := 0
	for i := 0; i < len(out) && i < len(ref); i++ {
		if out[i] == ref[i] {
			n++
		}
	}
	return n, bytes.Equal(out, ref)
}

// Assembles a boot ROM source with GNU as and lays it out at the top of the address space
func BuildROM(src, work string) ([]byte, error) {
	if err := os.MkdirAll(work, 0o755); err != nil {
		return nil, err
	}
	base := filepath.Join(work, strings.TrimSuffix(filepath.Base(src), filepath.Ext(src)))
	steps := [][]string{
		{"as", "--32", "-o", base + ".o", src},
		{"ld", "-m", "elf_i386", "-Ttext=" + romBase, "-e", "_start", "-o", base + ".elf", base + ".o"},
		{"objcopy", "-O", "binary", "-j", ".text", base + ".elf", base + ".bin"},
	}
	for _, step := range steps {
		cmd := exec.Command(step[0], step[1:]...)
		if msg, err := cmd.CombinedOutput(); err != nil {
			return nil, fmt.Errorf("%s: %v\n%s", strings.Join(step, " "), err, strings.TrimSpace(string(msg)))
		}
	}
	return os.ReadFile(base + ".bin")
}

// Assembles init table source into flash bytes
func BuildInitTable(src string) ([]byte, error) {
	text, err := os.ReadFile(src)
	if err != nil {
		return nil, err
	}
	return xcode.Assemble(text)
}
