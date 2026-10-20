package xbe

import (
	"bufio"
	"compress/gzip"
	"embed"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// Dxbx and D3D8LTCG XDK library signatures in IDA pat form, vendored from cxbx-reloaded
//
//go:embed patterns/dxbx.pat.gz patterns/d3d8ltcg.pat.gz
var patternFS embed.FS

// Pattern is one XDK library function signature.
type Pattern struct {
	Symbol  string
	Version int
	Lib     string
	Length  int
	Prefix  []int16 // 32 entries, -1 is a wildcard byte
	CRCLen  int
	CRC     uint16
	Tail    []int16 // bytes following the CRC region, -1 is a wildcard
}

// Symbol is a library function identified in the image.
type Symbol struct {
	VA         uint32
	Raw        string // decorated name as it appears in the signature file
	Name       string // undecorated name
	Lib        string
	Version    int
	ArgBytes   int      // stack bytes cleaned by the callee, -1 when unknown
	FastCall   bool     // first two arguments arrive in ECX and EDX
	Cdecl      bool     // the caller removes the arguments
	Alternates []string // other symbols whose bytes are identical to this function
}

var (
	patternOnce sync.Once
	patternSet  []Pattern
	patternErr  error
)

// Parses both embedded signature files once
func loadPatterns() ([]Pattern, error) {
	patternOnce.Do(func() {
		var all []Pattern
		for _, name := range []string{"patterns/dxbx.pat.gz", "patterns/d3d8ltcg.pat.gz"} {
			f, err := patternFS.Open(name)
			if err != nil {
				patternErr = err
				return
			}
			zr, err := gzip.NewReader(f)
			if err != nil {
				patternErr = err
				return
			}
			sc := bufio.NewScanner(zr)
			sc.Buffer(make([]byte, 1<<20), 1<<24)
			ida := strings.Contains(name, "ltcg")
			for sc.Scan() {
				line := strings.TrimSpace(sc.Text())
				if line == "" || line == "---" {
					continue
				}
				var p Pattern
				var perr error
				if ida {
					p, perr = parseIDAPattern(line)
				} else {
					p, perr = parseDxbxPattern(line)
				}
				if perr == errMalformed {
					continue
				}
				if perr != nil {
					patternErr = fmt.Errorf("%s: %w", name, perr)
					return
				}
				all = append(all, p)
			}
			f.Close()
		}
		patternSet = all
	})
	return patternSet, patternErr
}

// Parses "symbol version lib length prefix crclen crc [tail] [refs]"
func parseDxbxPattern(line string) (Pattern, error) {
	f := strings.Fields(line)
	if len(f) < 7 {
		return Pattern{}, fmt.Errorf("short pattern line %q", line)
	}
	ver, err := strconv.Atoi(f[1])
	if err != nil {
		return Pattern{}, err
	}
	length, err := strconv.ParseInt(f[3], 16, 32)
	if err != nil {
		return Pattern{}, err
	}
	p := Pattern{Symbol: f[0], Version: ver, Lib: f[2], Length: int(length)}
	if err := p.fillSignature(f[4], f[5], f[6]); err != nil {
		return Pattern{}, err
	}
	if len(f) > 7 && !strings.HasPrefix(f[7], "^") && !strings.HasPrefix(f[7], ":") {
		p.Tail = parseHexMask(f[7])
	}
	return p, nil
}

// Parses the IDA form "prefix crclen crc length :0000 symbol [refs] [tail]"
func parseIDAPattern(line string) (Pattern, error) {
	f := strings.Fields(line)
	if len(f) < 6 {
		return Pattern{}, fmt.Errorf("short pattern line %q", line)
	}
	length, err := strconv.ParseInt(f[3], 16, 32)
	if err != nil {
		return Pattern{}, err
	}
	p := Pattern{Lib: "d3d8ltcg", Length: int(length)}
	if err := p.fillSignature(f[0], f[1], f[2]); err != nil {
		return Pattern{}, err
	}
	for i := 4; i+1 < len(f); i += 2 {
		if strings.HasPrefix(f[i], ":") {
			if p.Symbol == "" {
				p.Symbol = f[i+1]
			}
			continue
		}
		if strings.HasPrefix(f[i], "^") {
			continue
		}
		p.Tail = parseHexMask(f[i])
		break
	}
	if p.Symbol == "" {
		return Pattern{}, fmt.Errorf("pattern without symbol %q", line)
	}
	return p, nil
}

// errMalformed marks signature lines whose byte masks cannot be parsed
var errMalformed = fmt.Errorf("malformed signature")

func (p *Pattern) fillSignature(prefix, crcLen, crc string) error {
	p.Prefix = parseHexMask(prefix)
	if len(p.Prefix) != 32 {
		return errMalformed
	}
	n, err := strconv.ParseInt(crcLen, 16, 32)
	if err != nil {
		return err
	}
	c, err := strconv.ParseInt(crc, 16, 32)
	if err != nil {
		return err
	}
	p.CRCLen = int(n)
	p.CRC = uint16(c)
	return nil
}

// Converts "55..8B" style text to byte values with -1 for wildcards, nil when malformed
func parseHexMask(s string) []int16 {
	if len(s)%2 != 0 {
		return nil
	}
	out := make([]int16, 0, len(s)/2)
	for i := 0; i < len(s); i += 2 {
		pair := s[i : i+2]
		if pair == ".." {
			out = append(out, -1)
			continue
		}
		v, err := strconv.ParseUint(pair, 16, 8)
		if err != nil {
			return nil
		}
		out = append(out, int16(v))
	}
	return out
}

// FLIRT crc16 with polynomial 0x8408, complemented and byte swapped
func flirtCRC16(data []byte) uint16 {
	crc := uint32(0xFFFF)
	for _, b := range data {
		v := uint32(b)
		for i := 0; i < 8; i++ {
			if (crc^v)&1 != 0 {
				crc = (crc >> 1) ^ 0x8408
			} else {
				crc >>= 1
			}
			v >>= 1
		}
	}
	crc = ^crc & 0xFFFF
	return uint16((crc<<8)&0xFF00 | (crc>>8)&0xFF)
}

func maskMatch(mask []int16, data []byte) bool {
	if len(data) < len(mask) {
		return false
	}
	for i, m := range mask {
		if m >= 0 && byte(m) != data[i] {
			return false
		}
	}
	return true
}

func (p *Pattern) matches(code []byte) bool {
	if !maskMatch(p.Prefix, code) {
		return false
	}
	if p.CRCLen > 0 {
		if len(code) < 32+p.CRCLen || flirtCRC16(code[32:32+p.CRCLen]) != p.CRC {
			return false
		}
	}
	if len(p.Tail) > 0 {
		off := 32 + p.CRCLen
		if len(code) < off+len(p.Tail) || !maskMatch(p.Tail, code[off:]) {
			return false
		}
	}
	return true
}

// IdentifyLibraryFunctions names every discovered function whose bytes match a signature
// for the XBE's D3D8 build, preferring exact version matches and reporting ambiguities.
func (d *Disassembly) IdentifyLibraryFunctions() ([]Symbol, error) {
	pats, err := loadPatterns()
	if err != nil {
		return nil, err
	}
	build := int(d.Image.LibBuild("D3D8"))
	ltcg := d.Image.LibBuild("D3D8LTCG") != 0
	if build == 0 {
		build = int(d.Image.LibBuild("D3D8LTCG"))
	}
	if build == 0 {
		return nil, fmt.Errorf("xbe: no D3D8 library version in header")
	}
	exact := false
	for _, p := range pats {
		if p.Version == build {
			exact = true
			break
		}
	}

	var syms []Symbol
	for va, fn := range d.Functions {
		code, ok := d.Image.ReadBytes(va, 32+256)
		if !ok {
			code, ok = d.Image.ReadBytes(va, fn.Size)
			if !ok {
				continue
			}
		}
		var best *Pattern
		bestDist := 1 << 30
		var alternates []string
		for i := range pats {
			p := &pats[i]
			if p.Lib == "d3d8ltcg" && !ltcg {
				continue
			}
			if exact && p.Version != 0 && p.Version != build {
				continue
			}
			if !p.matches(code) {
				continue
			}
			dist := 0
			if p.Version != 0 {
				dist = p.Version - build
				if dist < 0 {
					dist = -dist
				}
			}
			switch {
			case best == nil || dist < bestDist:
				best, bestDist, alternates = p, dist, nil
			case dist == bestDist && p.Symbol != best.Symbol:
				if p.Symbol < best.Symbol {
					alternates = append(alternates, best.Symbol)
					best = p
				} else {
					alternates = append(alternates, p.Symbol)
				}
			}
		}
		if best == nil {
			continue
		}
		sym := decorateSymbol(best.Symbol)
		sym.VA, sym.Lib, sym.Version = va, best.Lib, best.Version
		sort.Strings(alternates)
		sym.Alternates = alternates
		fn.Name = sym.Name
		fn.Symbol = &sym
		syms = append(syms, sym)
	}
	sort.Slice(syms, func(i, j int) bool { return syms[i].VA < syms[j].VA })
	d.Symbols = syms
	return syms, nil
}

// Reports whether an MSVC mangled name declares the cdecl convention
func mangledIsCdecl(raw string) bool {
	for i := 0; i+2 < len(raw); i++ {
		if raw[i] != '@' {
			continue
		}
		switch raw[i+1] {
		case 'Y', 'S':
			// Free or static function: the next letter is the convention.
			return raw[i+2] == 'A'
		case 'Q', 'A', 'I', 'U', 'E', 'M', 'F':
			// Member function: access, then near/far, then convention letter.
			if i+3 < len(raw) && (raw[i+2] == 'A' || raw[i+2] == 'E') {
				return raw[i+3] == 'A'
			}
		}
	}
	return false
}

// Splits a decorated symbol into its calling convention, name and argument size
func decorateSymbol(raw string) Symbol {
	s := Symbol{Raw: raw, ArgBytes: -1}
	switch {
	case raw == "??2@YAPAXI@Z":
		s.Name = "operator_new"
		s.Cdecl = true
	case raw == "??3@YAXPAX@Z":
		s.Name = "operator_delete"
		s.Cdecl = true
	case strings.HasPrefix(raw, "?"):
		parts := strings.Split(strings.TrimPrefix(raw, "?"), "@")
		if len(parts) >= 2 && parts[1] != "" {
			s.Name = parts[1] + "::" + parts[0]
		} else {
			s.Name = parts[0]
		}
		s.Cdecl = mangledIsCdecl(raw)
	case strings.HasPrefix(raw, "@") || strings.HasPrefix(raw, "_"):
		s.FastCall = strings.HasPrefix(raw, "@")
		body := raw[1:]
		if at := strings.LastIndex(body, "@"); at >= 0 {
			if n, err := strconv.Atoi(body[at+1:]); err == nil {
				s.ArgBytes = n
				body = body[:at]
			}
		}
		s.Cdecl = s.ArgBytes < 0 && !s.FastCall
		s.Name = body
	default:
		s.Name = raw
	}
	return s
}
