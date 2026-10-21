package xbe

import (
	"fmt"
	"sort"
	"strings"
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
		flag, err := pushedFlag(d, va)
		if err != nil {
			return nil, err
		}
		sm.ForceSetFlags[name] = flag
	}
	return sm, nil
}

// The disc drive constructor names the disc type through a string table indexed by the drive's type code
func (sm *SystemModel) deriveDiscTypes(d *Disassembly, types map[string]*NodeType) error {
	drive := types["DiscDrive"]
	if drive == nil || d.Functions[drive.Ctor] == nil {
		return fmt.Errorf("xbe: DiscDrive constructor is not disassembled")
	}
	base, ok := scaledTableBase(d.Functions[drive.Ctor], 4)
	if !ok {
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

// The text draw's clock formatter is its callee naming the time formats, the meridiem strings carrying no conversion
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
		var formats, plain []string
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
				if str == "" || !isPrintable(str) {
					continue
				}
				if strings.Contains(str, "%") {
					formats = append(formats, str)
				} else {
					plain = append(plain, str)
				}
			}
		}
		if len(formats) >= 4 && len(plain) == 2 {
			sm.Clock = ClockFormats{Formats: formats, Meridiem: plain}
			return nil
		}
	}
	return fmt.Errorf("xbe: Text draw %08X calls no clock formatter", text.Slots[16])
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

// Reads the launch flag a force-set native pushes to its shared check
func pushedFlag(d *Disassembly, va uint32) (int, error) {
	b, ok := d.Image.ReadBytes(va, 2)
	if !ok || b[0] != 0x6A {
		return 0, fmt.Errorf("xbe: native %08X does not start by pushing a flag", va)
	}
	return int(b[1]), nil
}

// The version native pushes its four numbers last first before the format string
func (sm *SystemModel) deriveDashboardVersion(d *Disassembly, va uint32) error {
	fn := d.Functions[va]
	if fn == nil {
		return fmt.Errorf("xbe: GetXdashVersion native %08X is not disassembled", va)
	}
	var pushes []int
	for i := range fn.Instructions {
		in := &fn.Instructions[i]
		if in.Inst.Op != x86asm.PUSH {
			continue
		}
		imm, ok := in.Inst.Args[0].(x86asm.Imm)
		if !ok {
			continue
		}
		if d.Image.ReadWideString(uint32(imm)) != "" {
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

// The recovery key helper hashes the disk key with a fixed key and maps digest words through a letter table
func (sm *SystemModel) deriveRecoveryKey(d *Disassembly, va uint32) error {
	fn := d.Functions[va]
	if fn == nil {
		return fmt.Errorf("xbe: GetRecoveryKey native %08X is not disassembled", va)
	}
	var helper *Function
	for _, t := range fn.CallTargets {
		if c := d.Functions[t]; c != nil && c.Symbol == nil {
			helper = c
			break
		}
	}
	if helper == nil {
		return fmt.Errorf("xbe: GetRecoveryKey native %08X calls no hashing helper", va)
	}
	var pushes []uint32
	var keyPtr, keyLen, modulus, table uint32
	for i := range helper.Instructions {
		in := &helper.Instructions[i]
		switch in.Inst.Op {
		case x86asm.PUSH:
			if imm, ok := in.Inst.Args[0].(x86asm.Imm); ok {
				pushes = append(pushes, uint32(imm))
			} else {
				pushes = append(pushes, 0)
			}
		case x86asm.CALL:
			if keyPtr == 0 && len(pushes) >= 2 {
				keyPtr, keyLen = pushes[len(pushes)-1], pushes[len(pushes)-2]
			}
		case x86asm.IDIV, x86asm.DIV:
			if len(pushes) > 0 {
				modulus = pushes[len(pushes)-1]
			}
		case x86asm.MOV:
			if m, ok := in.Inst.Args[1].(x86asm.Mem); ok && m.Base != 0 && m.Index == 0 && m.Disp > 0x10000 {
				table = uint32(m.Disp)
			}
		}
	}
	if keyPtr == 0 || keyLen == 0 || modulus == 0 || table == 0 {
		return fmt.Errorf("xbe: recovery key helper %08X reveals key %08X/%d modulus %d table %08X", helper.EntryVA, keyPtr, keyLen, modulus, table)
	}
	key, ok := d.Image.ReadBytes(keyPtr, int(keyLen))
	if !ok {
		return fmt.Errorf("xbe: recovery key constant at %08X lies outside the image", keyPtr)
	}
	alphabet, ok := d.Image.ReadBytes(table, int(modulus))
	if !ok {
		return fmt.Errorf("xbe: recovery letter table at %08X lies outside the image", table)
	}
	letters := 0
	for i := range helper.Instructions {
		in := &helper.Instructions[i]
		imm, ok := in.Inst.Args[0].(x86asm.Imm)
		if in.Inst.Op != x86asm.PUSH || !ok || imm <= 0 || imm >= 8 {
			continue
		}
		for j := i + 1; j < len(helper.Instructions) && j <= i+4; j++ {
			next := &helper.Instructions[j]
			if next.Inst.Op != x86asm.POP {
				continue
			}
			if r, ok := next.Inst.Args[0].(x86asm.Reg); ok && r == x86asm.ECX {
				letters = int(imm) + 1
			}
			break
		}
	}
	if letters == 0 {
		return fmt.Errorf("xbe: recovery key helper %08X reveals no letter count", helper.EntryVA)
	}
	sm.RecoveryKey = RecoveryKey{HMACKey: []int{}, Alphabet: string(alphabet), Letters: letters}
	for _, b := range key {
		sm.RecoveryKey.HMACKey = append(sm.RecoveryKey.HMACKey, int(b))
	}
	return nil
}

// Returns the native implementing a script method of a class
func (sm *SystemModel) function(class, name string) (uint32, error) {
	va, ok := sm.methodFunctions[class][name]
	if !ok {
		return 0, fmt.Errorf("xbe: class %s registers no script method %s", class, name)
	}
	return va, nil
}

// Finds every registration record store: the name pointer written to a record and the native copied into it
func (d *Disassembly) methodRecordStores() (map[uint32]string, map[uint32]uint32) {
	names := map[uint32]string{}
	fns := map[uint32]uint32{}
	for _, va := range d.functionVAs() {
		fn := d.Functions[va]
		var pending, dest uint32
		ins := fn.Instructions
		for i := range ins {
			in := &ins[i]
			if in.Inst.Op == x86asm.MOVSD && dest != 0 && pending != 0 {
				fns[dest] = pending
				dest = 0
				continue
			}
			if in.Inst.Op != x86asm.MOV || argCount(in) != 2 {
				continue
			}
			imm, isImm := in.Inst.Args[1].(x86asm.Imm)
			if !isImm {
				continue
			}
			switch a := in.Inst.Args[0].(type) {
			case x86asm.Mem:
				if a.Base == x86asm.EBP && a.Index == 0 && d.InsnByVA[uint32(imm)] != nil {
					pending = uint32(imm)
				}
				if a.Base == 0 && a.Index == 0 {
					if s := d.Image.ReadWideString(uint32(imm)); s != "" && isPrintable(s) {
						names[uint32(a.Disp)] = s
					}
				}
			case x86asm.Reg:
				if a == x86asm.EDI {
					dest = uint32(imm)
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

// The time zone text native scales the index by the entry size, the count native pushes the count
func (sm *SystemModel) deriveTimeZones(d *Disassembly, textVA, countVA uint32) error {
	text, count := d.Functions[textVA], d.Functions[countVA]
	if text == nil || count == nil {
		return fmt.Errorf("xbe: time zone natives %08X and %08X are not disassembled", textVA, countVA)
	}
	var stride, base uint32
	for i := range text.Instructions {
		in := &text.Instructions[i]
		if in.Inst.Op == x86asm.IMUL && argCount(in) == 3 {
			if imm, ok := in.Inst.Args[2].(x86asm.Imm); ok {
				stride = uint32(imm)
			}
		}
		if in.Inst.Op == x86asm.PUSH {
			if m, ok := in.Inst.Args[0].(x86asm.Mem); ok && m.Base != 0 && m.Index == 0 && m.Disp > 0 {
				base = uint32(m.Disp)
			}
		}
	}
	if stride == 0 || base == 0 {
		return fmt.Errorf("xbe: time zone text native %08X reveals no table (stride %d base %08X)", textVA, stride, base)
	}
	n := -1
	for i := range count.Instructions {
		in := &count.Instructions[i]
		if in.Inst.Op == x86asm.PUSH {
			if imm, ok := in.Inst.Args[0].(x86asm.Imm); ok {
				n = int(imm)
			}
		}
	}
	if n < 0 {
		return fmt.Errorf("xbe: time zone count native %08X pushes no count", countVA)
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
