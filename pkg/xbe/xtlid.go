package xbe

import (
	"compress/gzip"
	"encoding/binary"
	"encoding/xml"
	"fmt"
	"strconv"
	"sync"
)

// The section the later linkers fill with the identity and address of every library function a title links
const xtlidSection = ".XTLID"

// xtlidDatabase is the XboxDev table naming each library function identity
type xtlidDatabase struct {
	Libs []struct {
		Name  string `xml:"name,attr"`
		Funcs []struct {
			ID   string `xml:"id,attr"`
			Name string `xml:"name,attr"`
		} `xml:"func"`
	} `xml:"lib"`
}

var (
	xtlidOnce  sync.Once
	xtlidNames map[uint32]string
	xtlidErr   error
)

// Parses the embedded identity database once
func loadXTLID() (map[uint32]string, error) {
	xtlidOnce.Do(func() {
		f, err := patternFS.Open("patterns/xtlid.xml.gz")
		if err != nil {
			xtlidErr = err
			return
		}
		defer f.Close()
		z, err := gzip.NewReader(f)
		if err != nil {
			xtlidErr = err
			return
		}
		var db xtlidDatabase
		if err := xml.NewDecoder(z).Decode(&db); err != nil {
			xtlidErr = fmt.Errorf("xtlid: %w", err)
			return
		}
		names := map[uint32]string{}
		for _, lib := range db.Libs {
			for _, fn := range lib.Funcs {
				id, err := strconv.ParseUint(fn.ID, 0, 32)
				if err != nil {
					xtlidErr = fmt.Errorf("xtlid: identity %q of %s: %w", fn.ID, fn.Name, err)
					return
				}
				names[uint32(id)] = fn.Name
			}
		}
		xtlidNames = names
	})
	return xtlidNames, xtlidErr
}

// Names every function the executable's own identity section lists, over whatever a signature guessed for it
func (d *Disassembly) nameXTLID() error {
	sec := d.Image.FindSection(xtlidSection)
	if sec == nil {
		return nil
	}
	names, err := loadXTLID()
	if err != nil {
		return err
	}
	for off := 0; off+8 <= len(sec.Data); off += 8 {
		id := binary.LittleEndian.Uint32(sec.Data[off:])
		va := binary.LittleEndian.Uint32(sec.Data[off+4:])
		if id == 0 {
			continue
		}
		name, known := names[id]
		if !known {
			return fmt.Errorf("xbe: library identity %08X at %08X is not in the identity database", id, va)
		}
		fn := d.Functions[va]
		if fn == nil {
			continue
		}
		if fn.Symbol == nil {
			sym := decorateSymbol(name)
			sym.VA, sym.Lib = va, "xtlid"
			fn.Symbol = &sym
		}
		fn.Symbol.Name, fn.Symbol.Raw, fn.Symbol.Alternates = name, name, nil
		fn.Name = name
		delete(d.conventions, va)
	}
	d.librarySections = nil
	return nil
}
