package xbe

import (
	"fmt"
	"sort"
	"strings"
	"unicode"
	"unicode/utf16"

	"golang.org/x/arch/x86/x86asm"
)

// SystemModel is the console side of the executable the runtime's virtual console reads.
type SystemModel struct {
	Methods          map[string][]string `json:"methods"`
	Languages        []string            `json:"languages"`
	Translations     []map[string]string `json:"translations"`
	TimeZones        []TimeZone          `json:"time_zones"`
	DaysInMonth      []int               `json:"days_in_month"`
	DeviceNavigation [][4][]int          `json:"device_navigation"`
	BlockShift       int                 `json:"block_shift"`
	DashboardVersion []int               `json:"dashboard_version"`
	LanguageCodes    []string            `json:"language_codes"`
	RecoveryKey      RecoveryKey         `json:"recovery_key"`
	SelectionClock   uint32              `json:"selection_clock"`
	Clock            ClockFormats        `json:"clock"`
	ForceSetFlags    map[string]int      `json:"force_set_flags"`
	DiscTypes        []string            `json:"disc_types"`
	MemoryUnitType   uint32              `json:"memory_unit_type"`
	methodFunctions  map[string]map[string]uint32
}

// ClockFormats are the strings the text draw formats the clock token with, in the order the formatter names them.
type ClockFormats struct {
	Formats  []string `json:"formats"`
	Meridiem []string `json:"meridiem"`
}

// RecoveryKey is how the executable turns the console's hard disk key into the recovery letters.
type RecoveryKey struct {
	HMACKey  []int  `json:"hmac_key"`
	Alphabet string `json:"alphabet"`
	Letters  int    `json:"letters"`
}

// TimeZone is one entry of the executable's time zone table.
type TimeZone struct {
	Key     string       `json:"key"`
	Bias    int          `json:"bias"`
	StdBias int          `json:"std_bias"`
	DltBias int          `json:"dlt_bias"`
	StdName string       `json:"std_name"`
	DltName string       `json:"dlt_name"`
	StdDate TimeZoneDate `json:"std_date"`
	DltDate TimeZoneDate `json:"dlt_date"`
}

// TimeZoneDate is a packed transition date the executable expands into a SYSTEMTIME.
type TimeZoneDate struct {
	Month     int `json:"month"`
	Week      int `json:"week"`
	DayOfWeek int `json:"day_of_week"`
	Hour      int `json:"hour"`
}

const (
	methodRecordSize   = 24
	methodRecordName   = 0x14
	methodTableSlot    = 20
	timeZoneEntryBias  = 6
	timeZoneEntryStd   = 0xC
	timeZoneEntryStdDt = 0x10
	timeZoneEntryDlt   = 0x14
	timeZoneEntryDltDt = 0x18
)

// DeriveSystem reads the script method tables and the data tables the natives index.
func DeriveSystem(d *Disassembly, types map[string]*NodeType) (*SystemModel, error) {
	sm := &SystemModel{Methods: map[string][]string{}, methodFunctions: map[string]map[string]uint32{}}
	if err := sm.deriveMethods(d, types); err != nil {
		return nil, err
	}
	if err := sm.deriveMemoryUnitType(types); err != nil {
		return nil, err
	}
	setLanguage, err := sm.function("Translator", "SetLanguage")
	if err != nil {
		return nil, err
	}
	if err := sm.deriveTranslations(d, setLanguage); err != nil {
		return nil, err
	}
	textFn, err := sm.function("Translator", "GetTimeZoneText")
	if err != nil {
		return nil, err
	}
	countFn, err := sm.function("Translator", "GetTimeZoneCount")
	if err != nil {
		return nil, err
	}
	if err := sm.deriveTimeZones(d, textFn, countFn); err != nil {
		return nil, err
	}
	daysFn, err := sm.function("Date", "getDaysInMonth")
	if err != nil {
		return nil, err
	}
	if err := sm.deriveDaysInMonth(d, daysFn); err != nil {
		return nil, err
	}
	navFn, err := sm.function("MemoryMonitor", "selectUp")
	if err != nil {
		return nil, err
	}
	if err := sm.deriveDeviceNavigation(d, navFn); err != nil {
		return nil, err
	}
	freeFn, err := sm.function("MemoryMonitor", "FormatFreeBlocks")
	if err != nil {
		return nil, err
	}
	if err := sm.deriveBlockShift(d, freeFn); err != nil {
		return nil, err
	}
	versionFn, err := sm.function("Config", "GetXdashVersion")
	if err != nil {
		return nil, err
	}
	if err := sm.deriveDashboardVersion(d, versionFn); err != nil {
		return nil, err
	}
	codeFn, err := sm.function("Translator", "GetLanguageCode")
	if err != nil {
		return nil, err
	}
	if err := sm.deriveLanguageCodes(d, codeFn); err != nil {
		return nil, err
	}
	recoveryFn, err := sm.function("Config", "GetRecoveryKey")
	if err != nil {
		return nil, err
	}
	if err := sm.deriveRecoveryKey(d, recoveryFn); err != nil {
		return nil, err
	}
	selectFn, err := sm.function("SavedGameGrid", "selectDown")
	if err != nil {
		return nil, err
	}
	if err := sm.deriveSelectionClock(d, selectFn); err != nil {
		return nil, err
	}
	if err := sm.deriveClockFormats(d, types); err != nil {
		return nil, err
	}
	if err := sm.deriveDiscTypes(d, types); err != nil {
		return nil, err
	}
	sm.ForceSetFlags = map[string]int{}
	for _, name := range []string{"ForceSetLanguage", "ForceSetTimeZone", "ForceSetClock"} {
		va, err := sm.function("Config", name)
		if err != nil {
			return nil, err
		}
		flag, err := launchFlag(d, va)
		if err != nil {
			return nil, err
		}
		sm.ForceSetFlags[name] = flag
	}
	return sm, nil
}

// The disc drive constructor or its initializer names the disc type through a string table indexed by type code
func (sm *SystemModel) deriveDiscTypes(d *Disassembly, types map[string]*NodeType) error {
	drive := types["DiscDrive"]
	if drive == nil || d.Functions[drive.Ctor] == nil {
		return fmt.Errorf("xbe: DiscDrive constructor is not disassembled")
	}
	var base uint32
	for _, va := range append([]uint32{drive.Ctor}, d.plainCallees(drive.Ctor)...) {
		if b, ok := scaledTableBase(d.Functions[va], 4); ok {
			if base != 0 && base != b {
				return fmt.Errorf("xbe: DiscDrive constructor %08X indexes two tables (%08X, %08X)", drive.Ctor, base, b)
			}
			base = b
		}
	}
	if base == 0 {
		return fmt.Errorf("xbe: DiscDrive constructor %08X indexes no disc type table", drive.Ctor)
	}
	for i := uint32(0); ; i++ {
		p, ok := d.Image.ReadU32(base + 4*i)
		if !ok || p == 0 {
			break
		}
		s := d.Image.ReadWideString(p)
		if s == "" || !isPrintable(s) {
			break
		}
		sm.DiscTypes = append(sm.DiscTypes, s)
	}
	if len(sm.DiscTypes) == 0 {
		return fmt.Errorf("xbe: disc type table %08X names no disc types", base)
	}
	return nil
}

// The clock formatter is the text draw callee naming both date orders and both hour formats plus the meridiem words
func (sm *SystemModel) deriveClockFormats(d *Disassembly, types map[string]*NodeType) error {
	text := types["Text"]
	if text == nil || len(text.Slots) <= 16 {
		return fmt.Errorf("xbe: Text draw slot is not registered")
	}
	draw := d.Functions[text.Slots[16]]
	if draw == nil {
		return fmt.Errorf("xbe: Text draw %08X is not disassembled", text.Slots[16])
	}
	for _, t := range draw.CallTargets {
		fn := d.Functions[t]
		if fn == nil || fn.Symbol != nil {
			continue
		}
		roles := map[string]string{}
		var meridiem []string
		for _, str := range wideStringsOf(d, fn) {
			role := ""
			switch {
			case strings.HasPrefix(str, "%d%c"):
				role = "ymd"
			case strings.HasPrefix(str, "%02d%c"):
				role = "dmy"
			case strings.Contains(str, ":") && strings.Contains(str, "%s"):
				role = "time12"
			case strings.Contains(str, ":") && strings.Contains(str, "%"):
				role = "time24"
			case !strings.Contains(str, "%") && strings.IndexFunc(str, unicode.IsLetter) >= 0:
				meridiem = append(meridiem, str)
				continue
			default:
				continue
			}
			if prev, dup := roles[role]; dup && prev != str {
				return fmt.Errorf("xbe: clock formatter %08X names two %s formats (%q, %q)", t, role, prev, str)
			}
			roles[role] = str
		}
		if len(roles) != 4 {
			continue
		}
		if len(meridiem) != 2 {
			return fmt.Errorf("xbe: clock formatter %08X names %d meridiem wordings (%q)", t, len(meridiem), meridiem)
		}
		sm.Clock = ClockFormats{Formats: []string{roles["ymd"], roles["dmy"], roles["time12"], roles["time24"]}, Meridiem: meridiem}
		return nil
	}
	return fmt.Errorf("xbe: Text draw %08X calls no clock formatter", text.Slots[16])
}

// Lists the distinct printable wide strings a function addresses, in instruction order
func wideStringsOf(d *Disassembly, fn *Function) []string {
	var out []string
	seen := map[string]bool{}
	for i := range fn.Instructions {
		in := &fn.Instructions[i]
		for _, a := range in.Inst.Args {
			var va uint32
			switch v := a.(type) {
			case x86asm.Imm:
				va = uint32(v)
			case x86asm.Mem:
				if v.Base == 0 && v.Index == 0 {
					va = uint32(v.Disp)
				}
			}
			if va == 0 {
				continue
			}
			str := d.Image.ReadWideString(va)
			if str == "" || !isPrintable(str) || seen[str] {
				continue
			}
			seen[str] = true
			out = append(out, str)
		}
	}
	return out
}

// The grid selection natives store the clock into the global the pulsing materials phase from
func (sm *SystemModel) deriveSelectionClock(d *Disassembly, va uint32) error {
	fn := d.Functions[va]
	if fn == nil {
		return fmt.Errorf("xbe: selectDown native %08X is not disassembled", va)
	}
	loadedClock := false
	for i := range fn.Instructions {
		in := &fn.Instructions[i]
		m, ok := in.Inst.Args[0].(x86asm.Mem)
		if !ok || m.Base != 0 || m.Index != 0 {
			continue
		}
		switch in.Inst.Op {
		case x86asm.FLD:
			loadedClock = true
		case x86asm.FSTP:
			if loadedClock {
				sm.SelectionClock = uint32(m.Disp)
				return nil
			}
		}
	}
	return fmt.Errorf("xbe: selectDown native %08X stores the clock into no global", va)
}

// Runs a native taking no inputs and returns the one constant every path of it returns
func constantResult(d *Disassembly, va uint32) (int, error) {
	ex := NewExecutorShared(d.baseExecutor())
	paths, err := ex.Run(va, func(s *execState) {})
	if err != nil {
		return 0, fmt.Errorf("native %08X: %w", va, err)
	}
	n := -1
	for _, p := range paths {
		c, ok := isConst(p.Return)
		if !ok {
			return 0, fmt.Errorf("xbe: native %08X returns %s rather than a constant", va, ExprString(p.Return))
		}
		if n >= 0 && int(c) != n {
			return 0, fmt.Errorf("xbe: native %08X returns both %d and %d", va, n, c)
		}
		n = int(c)
	}
	if n < 0 {
		return 0, fmt.Errorf("xbe: native %08X returns on no path", va)
	}
	return n, nil
}

// Reads the launch flag a force-set native tests against the launch data, itself or through its shared check
func launchFlag(d *Disassembly, va uint32) (int, error) {
	ex := NewExecutorShared(d.baseExecutor())
	paths, err := ex.Run(va, func(s *execState) {})
	if err != nil {
		return 0, fmt.Errorf("force-set native %08X: %w", va, err)
	}
	flags := map[uint32]bool{}
	for _, p := range paths {
		for _, g := range p.Guards {
			mapExpr(g, func(e Expr) (Expr, bool) {
				b, ok := e.(Bin)
				if !ok || b.Op != "and" {
					return nil, false
				}
				for _, side := range [][2]Expr{{b.A, b.B}, {b.B, b.A}} {
					c, isC := isConst(side[0])
					set := map[string]bool{}
					exprSyms(side[1], set)
					for name := range set {
						if isC && strings.HasPrefix(name, "@") {
							flags[c] = true
						}
					}
				}
				return nil, false
			})
		}
	}
	if len(flags) != 1 {
		return 0, fmt.Errorf("xbe: force-set native %08X tests %d launch flags against a global", va, len(flags))
	}
	for flag := range flags {
		return int(flag), nil
	}
	return 0, nil
}

// The version native pushes its four numbers last first in a row right before the format string
func (sm *SystemModel) deriveDashboardVersion(d *Disassembly, va uint32) error {
	fn := d.Functions[va]
	if fn == nil {
		return fmt.Errorf("xbe: GetXdashVersion native %08X is not disassembled", va)
	}
	var pushes []int
	for i := range fn.Instructions {
		in := &fn.Instructions[i]
		imm, ok := in.Inst.Args[0].(x86asm.Imm)
		if in.Inst.Op != x86asm.PUSH || !ok {
			pushes = nil
			continue
		}
		if s := d.Image.ReadWideString(uint32(imm)); s != "" && isPrintable(s) && strings.Contains(s, "%") {
			break
		}
		pushes = append(pushes, int(imm))
	}
	if len(pushes) != 4 {
		return fmt.Errorf("xbe: GetXdashVersion native %08X pushes %d numbers before its format", va, len(pushes))
	}
	for i := len(pushes) - 1; i >= 0; i-- {
		sm.DashboardVersion = append(sm.DashboardVersion, pushes[i])
	}
	return nil
}

// The language code native indexes a pointer table with the language
func (sm *SystemModel) deriveLanguageCodes(d *Disassembly, va uint32) error {
	fn := d.Functions[va]
	if fn == nil {
		return fmt.Errorf("xbe: GetLanguageCode native %08X is not disassembled", va)
	}
	base, ok := scaledTableBase(fn, 4)
	if !ok {
		return fmt.Errorf("xbe: GetLanguageCode native %08X indexes no code table", va)
	}
	for i := range sm.Languages {
		raw, ok := d.Image.ReadBytes(base+uint32(4*i), 4)
		if !ok {
			return fmt.Errorf("xbe: language code %d lies outside the image", i)
		}
		code := []uint16{uint16(raw[0]) | uint16(raw[1])<<8, uint16(raw[2]) | uint16(raw[3])<<8}
		sm.LanguageCodes = append(sm.LanguageCodes, string(utf16.Decode(code)))
	}
	return nil
}

// The recovery key native or its helper hashes the disk key with a fixed key and maps digest words to letters
func (sm *SystemModel) deriveRecoveryKey(d *Disassembly, va uint32) error {
	fn := d.Functions[va]
	if fn == nil {
		return fmt.Errorf("xbe: GetRecoveryKey native %08X is not disassembled", va)
	}
	var failures []string
	for _, c := range append([]uint32{va}, d.plainCallees(va)...) {
		rk, err := recoveryKeyOf(d, c)
		if err != nil {
			failures = append(failures, err.Error())
			continue
		}
		sm.RecoveryKey = rk
		return nil
	}
	return fmt.Errorf("xbe: GetRecoveryKey native %08X reveals no recovery key (%s)", va, strings.Join(failures, "; "))
}

// Runs a routine with its callees kept as calls and reads the hashing key, letter table, modulus and word count
func recoveryKeyOf(d *Disassembly, va uint32) (RecoveryKey, error) {
	ex := NewExecutorShared(d.baseExecutor())
	for _, c := range d.plainCallees(va) {
		ex.Opaque[c] = true
	}
	paths, err := ex.Run(va, func(s *execState) {
		s.regs[1] = Ptr{"this", 0}
		ex.seedArgs(s, va, "arg", 2)
	})
	if err != nil {
		return RecoveryKey{}, fmt.Errorf("%08X: %w", va, err)
	}
	var keyPtr, keyLen, table, modulus uint32
	lookups := map[string]bool{}
	for _, p := range paths {
		for _, e := range p.Effects {
			if e.Kind != "call" || e.Fn != "XcHMAC" || len(e.Args) < 2 {
				continue
			}
			k, ok1 := isConst(e.Args[0])
			n, ok2 := isConst(e.Args[1])
			if !ok1 || !ok2 {
				return RecoveryKey{}, fmt.Errorf("%08X hashes with the symbolic key %s of length %s", va, ExprString(e.Args[0]), ExprString(e.Args[1]))
			}
			keyPtr, keyLen = k, n
		}
		for _, x := range pathExprs(p) {
			mapExpr(x, func(e Expr) (Expr, bool) {
				if t, m, ok := letterLookup(e); ok {
					table, modulus = t, m
					lookups[ExprString(e)] = true
				}
				return nil, false
			})
		}
	}
	if keyPtr == 0 || keyLen == 0 || modulus == 0 || table == 0 || len(lookups) == 0 {
		return RecoveryKey{}, fmt.Errorf("%08X reveals key %08X/%d modulus %d table %08X over %d digest words", va, keyPtr, keyLen, modulus, table, len(lookups))
	}
	key, ok := d.Image.ReadBytes(keyPtr, int(keyLen))
	if !ok {
		return RecoveryKey{}, fmt.Errorf("recovery key constant at %08X lies outside the image", keyPtr)
	}
	alphabet, ok := d.Image.ReadBytes(table, int(modulus))
	if !ok {
		return RecoveryKey{}, fmt.Errorf("recovery letter table at %08X lies outside the image", table)
	}
	rk := RecoveryKey{HMACKey: []int{}, Alphabet: string(alphabet), Letters: len(lookups)}
	for _, b := range key {
		rk.HMACKey = append(rk.HMACKey, int(b))
	}
	return rk, nil
}

// Reads the table and modulus of a byte loaded from a letter table at the remainder of a digest word
func letterLookup(e Expr) (table, modulus uint32, ok bool) {
	l, isLoad := e.(Load)
	if !isLoad || l.Size != 1 || l.Addr == nil {
		return 0, 0, false
	}
	b, isBin := l.Addr.(Bin)
	if !isBin || b.Op != "add" {
		return 0, 0, false
	}
	for _, side := range [][2]Expr{{b.A, b.B}, {b.B, b.A}} {
		base, isC := isConst(side[0])
		rem, isRem := side[1].(Bin)
		if !isC || !isRem || !strings.HasSuffix(rem.Op, "_rem") {
			continue
		}
		if m, ok := isConst(rem.B); ok {
			return base, m, true
		}
	}
	return 0, 0, false
}

// Returns the native implementing a script method of a class
func (sm *SystemModel) function(class, name string) (uint32, error) {
	va, ok := sm.methodFunctions[class][name]
	if !ok {
		return 0, fmt.Errorf("xbe: class %s registers no script method %s", class, name)
	}
	return va, nil
}

// Finds each record's name pointer store and its native, copied from the stack, stored directly or staged in a register
func (d *Disassembly) methodRecordStores() (map[uint32]string, map[uint32]uint32) {
	names := map[uint32]string{}
	fns := map[uint32]uint32{}
	for _, va := range d.functionVAs() {
		fn := d.Functions[va]
		var pending, dest uint32
		held := map[x86asm.Reg]uint32{}
		ins := fn.Instructions
		for i := range ins {
			in := &ins[i]
			if in.Inst.Op == x86asm.MOVSD && dest != 0 && pending != 0 {
				fns[dest] = pending
				dest = 0
				continue
			}
			if in.Inst.Op != x86asm.MOV || argCount(in) != 2 {
				for _, r := range d.writtenBy(in) {
					delete(held, r)
				}
				continue
			}
			imm, isImm := in.Inst.Args[1].(x86asm.Imm)
			src, isReg := in.Inst.Args[1].(x86asm.Reg)
			switch a := in.Inst.Args[0].(type) {
			case x86asm.Mem:
				if !isImm {
					if v, ok := held[wideRegister(src)]; isReg && ok && a.Base == 0 && a.Index == 0 {
						fns[uint32(a.Disp)] = v
					}
					continue
				}
				if a.Base == x86asm.EBP && a.Index == 0 && d.InsnByVA[uint32(imm)] != nil {
					pending = uint32(imm)
				}
				if a.Base == 0 && a.Index == 0 {
					if s := d.Image.ReadWideString(uint32(imm)); s != "" && isPrintable(s) {
						names[uint32(a.Disp)] = s
					}
					if d.InsnByVA[uint32(imm)] != nil {
						fns[uint32(a.Disp)] = uint32(imm)
					}
				}
			case x86asm.Reg:
				delete(held, wideRegister(a))
				if !isImm {
					continue
				}
				if a == x86asm.EDI {
					dest = uint32(imm)
				}
				if d.InsnByVA[uint32(imm)] != nil {
					held[wideRegister(a)] = uint32(imm)
				}
			}
		}
	}
	return names, fns
}

// Reads the table a class's method accessor returns as a mov eax, imm32 followed by ret
func (d *Disassembly) methodTableBase(va uint32) (uint32, bool) {
	b, ok := d.Image.ReadBytes(va, 6)
	if !ok || b[0] != 0xB8 || b[5] != 0xC3 {
		return 0, false
	}
	return uint32(b[1]) | uint32(b[2])<<8 | uint32(b[3])<<16 | uint32(b[4])<<24, true
}

// Walks each class's registration records in table order
func (sm *SystemModel) deriveMethods(d *Disassembly, types map[string]*NodeType) error {
	names, fns := d.methodRecordStores()
	bases := map[string]uint32{}
	for name, nt := range types {
		if len(nt.Slots) <= methodTableSlot/4 {
			continue
		}
		if base, ok := d.methodTableBase(nt.Slots[methodTableSlot/4]); ok {
			bases[name] = base
		}
	}
	for name, nt := range types {
		base, ok := bases[name]
		if !ok || (nt.Parent != "" && bases[nt.Parent] == base) {
			continue
		}
		list := []string{}
		byName := map[string]uint32{}
		for rec := base; ; rec += methodRecordSize {
			method, ok := names[rec+methodRecordName]
			if !ok {
				break
			}
			fn, ok := fns[rec]
			if !ok {
				return fmt.Errorf("xbe: class %s method %s has no native copied into its record at %08X", name, method, rec)
			}
			list = append(list, method)
			byName[method] = fn
		}
		if len(list) == 0 {
			return fmt.Errorf("xbe: class %s has a method table at %08X with no records", name, base)
		}
		sm.Methods[name] = list
		sm.methodFunctions[name] = byName
	}
	return nil
}

// The device type the memory monitor constructor enumerates is the memory unit type
func (sm *SystemModel) deriveMemoryUnitType(types map[string]*NodeType) error {
	monitor := types["MemoryMonitor"]
	if monitor == nil {
		return fmt.Errorf("xbe: MemoryMonitor is not registered")
	}
	found := map[uint32]bool{}
	for _, p := range monitor.ctorPaths {
		for _, e := range p.Effects {
			if e.Kind == "call" && e.Fn == "XGetDevices" && len(e.Args) == 1 {
				if c, ok := isConst(e.Args[0]); ok {
					found[c] = true
				}
			}
		}
	}
	if len(found) != 1 {
		return fmt.Errorf("xbe: MemoryMonitor constructor enumerates %d device types", len(found))
	}
	for va := range found {
		sm.MemoryUnitType = va
	}
	return nil
}

// Finds the first scaled index load in a function, the table its instructions walk
func scaledTableBase(fn *Function, scale uint8) (uint32, bool) {
	for i := range fn.Instructions {
		in := &fn.Instructions[i]
		for _, a := range in.Inst.Args {
			if m, ok := a.(x86asm.Mem); ok && m.Base == 0 && m.Index != 0 && m.Scale == scale && m.Disp > 0 {
				return uint32(m.Disp), true
			}
		}
	}
	return 0, false
}

// Translator SetLanguage loads the section named by a pointer table indexed with the language
func (sm *SystemModel) deriveTranslations(d *Disassembly, va uint32) error {
	fn := d.Functions[va]
	if fn == nil {
		return fmt.Errorf("xbe: translator SetLanguage %08X is not disassembled", va)
	}
	table, ok := scaledTableBase(fn, 4)
	if !ok {
		return fmt.Errorf("xbe: translator SetLanguage %08X indexes no section name table", va)
	}
	for i := uint32(0); ; i++ {
		p, ok := d.Image.ReadU32(table + 4*i)
		if !ok || p == 0 {
			break
		}
		raw, ok := d.Image.ReadBytes(p, 64)
		if !ok {
			return fmt.Errorf("xbe: language %d section name at %08X lies outside the image", i, p)
		}
		name := readCString(raw, 0)
		sec := d.Image.FindSection(name)
		if sec == nil {
			return fmt.Errorf("xbe: language section %q is not in the image", name)
		}
		strings, err := parseTranslationSection(sec.Data[:sec.VirtualSize])
		if err != nil {
			return fmt.Errorf("xbe: language section %q: %w", name, err)
		}
		sm.Languages = append(sm.Languages, name)
		sm.Translations = append(sm.Translations, strings)
	}
	if len(sm.Languages) == 0 {
		return fmt.Errorf("xbe: translator section table %08X names no languages", table)
	}
	return nil
}

// Parses key = value lines the way the executable's loader does, keeping the first of duplicate keys
func parseTranslationSection(data []byte) (map[string]string, error) {
	if len(data) < 2 || data[0] != 0xFF || data[1] != 0xFE {
		return nil, fmt.Errorf("section does not start with a byte order mark")
	}
	p := make([]uint16, len(data)/2+1)
	for i := 0; i+1 < len(data); i += 2 {
		p[i/2] = uint16(data[i]) | uint16(data[i+1])<<8
	}
	out := map[string]string{}
	isBlank := func(c uint16) bool { return c == ' ' || c == '\t' }
	i := 1
	for p[i] != 0 {
		for isBlank(p[i]) {
			i++
		}
		if p[i] == '#' {
			for p[i] != 0 && p[i] != '\r' && p[i] != '\n' {
				i++
			}
		}
		if p[i] == '\r' {
			i++
			if p[i] == '\n' {
				i++
			}
			continue
		}
		var keyStart int
		if p[i] == '"' {
			i++
			keyStart = i
			for p[i] != 0 && p[i] != '\r' && p[i] != '\n' && p[i] != '"' {
				i++
			}
		} else {
			keyStart = i
			for p[i] > ' ' {
				if p[i] == '=' {
					break
				}
				if p[i] == '_' {
					if p[i+1] == '=' || p[i+1] == ' ' {
						p[i] = ':'
					} else {
						p[i] = ' '
					}
				}
				i++
			}
		}
		key := append([]uint16(nil), p[keyStart:i]...)
		if p[i] == '"' {
			i++
		}
		for isBlank(p[i]) {
			i++
		}
		if p[i] != '=' {
			break
		}
		i++
		for isBlank(p[i]) {
			i++
		}
		if p[i] == '"' {
			i++
		}
		valueStart := i
		for p[i] != 0 && p[i] != '\r' && p[i] != '\n' && p[i] != '"' {
			i++
		}
		value := append([]uint16(nil), p[valueStart:i]...)
		if p[i] == '"' {
			i++
		}
		for isBlank(p[i]) {
			i++
		}
		if p[i] == '#' {
			for p[i] != 0 && p[i] != '\r' {
				i++
			}
		}
		if p[i] != '\r' {
			break
		}
		i++
		if p[i] == '\n' {
			i++
		}
		if len(key) == 0 || len(value) == 0 {
			continue
		}
		unescaped, err := unescapeTranslation(value)
		if err != nil {
			break
		}
		k := string(utf16.Decode(key))
		if _, dup := out[k]; !dup {
			out[k] = string(utf16.Decode(unescaped))
		}
	}
	return out, nil
}

// Expands the backslash escapes the script string reader accepts
func unescapeTranslation(s []uint16) ([]uint16, error) {
	out := make([]uint16, 0, len(s))
	hexDigit := func(c uint16) (uint16, bool) {
		switch {
		case c >= '0' && c <= '9':
			return c - '0', true
		case c >= 'A' && c <= 'F':
			return c - 'A' + 10, true
		case c >= 'a' && c <= 'f':
			return c - 'a' + 10, true
		}
		return 0, false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != '\\' {
			out = append(out, c)
			continue
		}
		i++
		if i >= len(s) {
			return nil, fmt.Errorf("backslash at end of string")
		}
		c = s[i]
		switch c {
		case 'b':
			out = append(out, 8)
		case 'f':
			out = append(out, 12)
		case 'n':
			out = append(out, 10)
		case 'r':
			out = append(out, 13)
		case 't':
			out = append(out, 9)
		case 'u', 'x':
			digits := 4
			if c == 'x' {
				digits = 2
			}
			if len(s)-i-1 < digits {
				return nil, fmt.Errorf("invalid character escape sequence")
			}
			var v uint16
			for k := 0; k < digits; k++ {
				i++
				h, ok := hexDigit(s[i])
				if !ok {
					return nil, fmt.Errorf("invalid hex character")
				}
				v = v<<4 | h
			}
			out = append(out, v)
		default:
			if c >= '0' && c <= '7' {
				v := c - '0'
				for k := 0; k < 2 && i+1 < len(s) && s[i+1] >= '0' && s[i+1] <= '7'; k++ {
					i++
					v = v*8 + s[i] - '0'
				}
				out = append(out, v)
			} else {
				out = append(out, c)
			}
		}
	}
	return out, nil
}

// The text native scales the zone by the entry size into the table, the count native returning the count
func (sm *SystemModel) deriveTimeZones(d *Disassembly, textVA, countVA uint32) error {
	text, count := d.Functions[textVA], d.Functions[countVA]
	if text == nil || count == nil {
		return fmt.Errorf("xbe: time zone natives %08X and %08X are not disassembled", textVA, countVA)
	}
	var stride, base uint32
	var scaled x86asm.Reg
	for i := range text.Instructions {
		in := &text.Instructions[i]
		if in.Inst.Op == x86asm.IMUL && argCount(in) == 3 {
			imm, isImm := in.Inst.Args[2].(x86asm.Imm)
			if r, ok := in.Inst.Args[0].(x86asm.Reg); ok && isImm {
				stride, scaled = uint32(imm), wideRegister(r)
			}
			continue
		}
		if scaled == 0 || base != 0 {
			continue
		}
		for _, a := range in.Inst.Args {
			if m, ok := a.(x86asm.Mem); ok && m.Base == scaled && m.Index == 0 && m.Disp > 0 {
				base = uint32(m.Disp)
			}
		}
	}
	if stride == 0 || base == 0 {
		return fmt.Errorf("xbe: time zone text native %08X reveals no table (stride %d base %08X)", textVA, stride, base)
	}
	n, err := constantResult(d, countVA)
	if err != nil {
		return fmt.Errorf("xbe: time zone count native: %w", err)
	}
	for i := 0; i < n; i++ {
		e := base + uint32(i)*stride
		keyPtr, _ := d.Image.ReadU32(e)
		bias, _ := d.Image.ReadU16(e + timeZoneEntryBias)
		stdBias, _ := d.Image.ReadU16(e + timeZoneEntryBias + 2)
		dltBias, _ := d.Image.ReadU16(e + timeZoneEntryBias + 4)
		stdPtr, _ := d.Image.ReadU32(e + timeZoneEntryStd)
		dltPtr, _ := d.Image.ReadU32(e + timeZoneEntryDlt)
		stdDate, ok1 := d.Image.ReadBytes(e+timeZoneEntryStdDt, 4)
		dltDate, ok2 := d.Image.ReadBytes(e+timeZoneEntryDltDt, 4)
		if keyPtr == 0 || stdPtr == 0 || !ok1 || !ok2 {
			return fmt.Errorf("xbe: time zone entry %d at %08X is incomplete", i, e)
		}
		tz := TimeZone{Key: d.Image.ReadWideString(keyPtr), Bias: int(int16(bias)), StdBias: int(int16(stdBias)), DltBias: int(int16(dltBias)), StdName: d.Image.ReadWideString(stdPtr)}
		if dltPtr != 0 {
			tz.DltName = d.Image.ReadWideString(dltPtr)
		}
		tz.StdDate = TimeZoneDate{Month: int(stdDate[0]), Week: int(stdDate[1]), DayOfWeek: int(stdDate[2]), Hour: int(stdDate[3])}
		tz.DltDate = TimeZoneDate{Month: int(dltDate[0]), Week: int(dltDate[1]), DayOfWeek: int(dltDate[2]), Hour: int(dltDate[3])}
		sm.TimeZones = append(sm.TimeZones, tz)
	}
	return nil
}

// The month length native sign extends a word indexed by the month
func (sm *SystemModel) deriveDaysInMonth(d *Disassembly, va uint32) error {
	fn := d.Functions[va]
	if fn == nil {
		return fmt.Errorf("xbe: getDaysInMonth native %08X is not disassembled", va)
	}
	base, ok := scaledTableBase(fn, 2)
	if !ok {
		return fmt.Errorf("xbe: getDaysInMonth native %08X indexes no month table", va)
	}
	for m := 0; m <= 12; m++ {
		v, ok := d.Image.ReadU16(base + uint32(2*m))
		if !ok {
			return fmt.Errorf("xbe: month table entry %d lies outside the image", m)
		}
		sm.DaysInMonth = append(sm.DaysInMonth, int(int16(v)))
	}
	return nil
}

// The device select natives walk candidate lists per unit, one list per direction, ended by 0xFF
func (sm *SystemModel) deriveDeviceNavigation(d *Disassembly, va uint32) error {
	fn := d.Functions[va]
	if fn == nil {
		return fmt.Errorf("xbe: selectUp native %08X is not disassembled", va)
	}
	base, ok := scaledTableBase(fn, 4)
	if !ok {
		return fmt.Errorf("xbe: selectUp native %08X indexes no navigation table", va)
	}
	for unit := uint32(0); unit < 8; unit++ {
		entry, ok := d.Image.ReadU32(base + 4*unit)
		if !ok || entry == 0 {
			return fmt.Errorf("xbe: navigation entry %d lies outside the image", unit)
		}
		var lists [4][]int
		for dir := uint32(0); dir < 4; dir++ {
			list, ok := d.Image.ReadU32(entry + 4*dir)
			if !ok {
				return fmt.Errorf("xbe: navigation list %d of unit %d lies outside the image", dir, unit)
			}
			lists[dir] = []int{}
			for off := uint32(0); ; off++ {
				b, ok := d.Image.ReadU8(list + off)
				if !ok {
					return fmt.Errorf("xbe: navigation list %d of unit %d is unterminated", dir, unit)
				}
				if b == 0xFF {
					break
				}
				lists[dir] = append(lists[dir], int(b))
			}
		}
		sm.DeviceNavigation = append(sm.DeviceNavigation, lists)
	}
	return nil
}

// The free block native's callee shifts the byte counts the disk reports down to blocks
func (sm *SystemModel) deriveBlockShift(d *Disassembly, va uint32) error {
	fn := d.Functions[va]
	if fn == nil {
		return fmt.Errorf("xbe: FormatFreeBlocks native %08X is not disassembled", va)
	}
	candidates := append([]uint32{va}, fn.CallTargets...)
	sort.Slice(candidates, func(i, j int) bool { return candidates[i] < candidates[j] })
	for _, c := range candidates {
		callee := d.Functions[c]
		if callee == nil || callee.Symbol != nil {
			continue
		}
		for i := range callee.Instructions {
			in := &callee.Instructions[i]
			if in.Inst.Op == x86asm.SHRD {
				if imm, ok := in.Inst.Args[2].(x86asm.Imm); ok {
					sm.BlockShift = int(imm)
					return nil
				}
			}
		}
	}
	return fmt.Errorf("xbe: FormatFreeBlocks native %08X never shifts a byte count down to blocks", va)
}
