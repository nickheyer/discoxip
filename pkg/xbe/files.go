package xbe

import (
	"sort"
	"strings"

	"golang.org/x/arch/x86/x86asm"
)

// wideString is a printable UTF-16 string found in the image with its address.
type wideString struct {
	VA   uint32
	Text string
}

// Collects every printable UTF-16 string of three or more characters in the image
func (img *Image) wideStrings() []wideString {
	var out []wideString
	for i := range img.Sections {
		sec := &img.Sections[i]
		data := sec.Data[:sec.RawSize]
		start := -1
		for off := 0; off+1 < len(data); off += 2 {
			lo, hi := data[off], data[off+1]
			printable := hi == 0 && lo >= 0x20 && lo <= 0x7E
			if printable {
				if start < 0 {
					start = off
				}
				continue
			}
			if start >= 0 && lo == 0 && hi == 0 && (off-start)/2 >= 3 {
				out = append(out, wideString{VA: sec.VirtualAddr + uint32(start), Text: img.ReadUTF16(sec.VirtualAddr + uint32(start))})
			}
			start = -1
		}
	}
	return out
}

// NamedFiles returns the bare file names with an extension the image names, in address order.
func (img *Image) NamedFiles(ext string) []string {
	var out []string
	seen := map[string]bool{}
	for _, ws := range img.wideStrings() {
		if len(ws.Text) <= len(ext) || !strings.HasSuffix(strings.ToLower(ws.Text), strings.ToLower(ext)) || strings.ContainsAny(ws.Text, `\/:%`) {
			continue
		}
		key := strings.ToLower(ws.Text)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, ws.Text)
	}
	return out
}

// DirectoryPreference lists the directories the code tries for files with an extension, first tried first.
func (d *Disassembly) DirectoryPreference(ext string) []string {
	refs := map[uint32]uint32{}
	for va, in := range d.InsnByVA {
		for _, a := range in.Inst.Args {
			var target uint32
			switch v := a.(type) {
			case x86asm.Imm:
				target = uint32(v)
			case x86asm.Mem:
				if v.Base == 0 && v.Index == 0 {
					target = uint32(v.Disp)
				}
			}
			if target == 0 {
				continue
			}
			if prev, ok := refs[target]; !ok || va < prev {
				refs[target] = va
			}
		}
	}
	first := map[string]uint32{}
	for _, ws := range d.Image.wideStrings() {
		if !strings.HasSuffix(strings.ToLower(ws.Text), strings.ToLower(ext)) {
			continue
		}
		va, referenced := refs[ws.VA]
		if !referenced {
			continue
		}
		path := strings.ReplaceAll(ws.Text, `\`, "/")
		path = strings.TrimPrefix(path, "%s")
		if len(path) > 2 && path[1] == ':' {
			path = path[3:]
		}
		dir := ""
		if i := strings.LastIndex(path, "/"); i >= 0 {
			dir = strings.ToLower(path[:i])
		}
		if prev, ok := first[dir]; !ok || va < prev {
			first[dir] = va
		}
	}
	dirs := make([]string, 0, len(first))
	for dir := range first {
		dirs = append(dirs, dir)
	}
	sort.Slice(dirs, func(i, j int) bool { return first[dirs[i]] < first[dirs[j]] })
	return dirs
}
