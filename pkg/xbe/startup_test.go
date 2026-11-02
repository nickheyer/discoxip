package xbe

import (
	"reflect"
	"strings"
	"testing"
)

// The CRT init walking its tables inline, as the optimizer lays it out, beside one calling the walker
func startupText() []chunk {
	return []chunk{
		lbl("cinitInline"), by(0x56), by(0x57), abs32([]byte{0xB8}, "tableA", 0), abs32([]byte{0xBF}, "tableA", 12), by(0x3B, 0xC7), by(0x8B, 0xF0), rel8(0x73, "inlineSecond"),
		lbl("inlineLoop"), by(0x8B, 0x06), by(0x85, 0xC0), je8("inlineNext"), by(0x83, 0xF8, 0xFF), je8("inlineNext"), by(0xFF, 0xD0),
		lbl("inlineNext"), by(0x83, 0xC6, 0x04), by(0x3B, 0xF7), rel8(0x72, "inlineLoop"),
		lbl("inlineSecond"), abs32([]byte{0xBE}, "tableB", 0), by(0x8B, 0xC6), abs32([]byte{0xBF}, "tableB", 8), by(0x3B, 0xC7), rel8(0x73, "inlineDone"),
		lbl("inlineLoop2"), by(0x8B, 0x06), by(0x85, 0xC0), je8("inlineNext2"), by(0xFF, 0xD0),
		lbl("inlineNext2"), by(0x83, 0xC6, 0x04), by(0x3B, 0xF7), rel8(0x72, "inlineLoop2"),
		lbl("inlineDone"), by(0x5F), by(0x5E), ret(), pad(),
		lbl("cinitCall"), abs32([]byte{0x68}, "tableB", 8), abs32([]byte{0x68}, "tableB", 0), call("initterm"), by(0x83, 0xC4, 0x08), ret(), pad(),
		lbl("initterm"), ret(), pad(),
		lbl("cinitEmpty"), movEAXImm(0), ret(), pad(),
		lbl("initOne"), movEAXImm(1), ret(), pad(),
		lbl("initTwo"), movEAXImm(2), ret(), pad(),
		lbl("initThree"), movEAXImm(3), ret(), pad(),
		lbl("registrar"), ret(), pad(),
		lbl("registers"), call("registrar"), ret(), pad(),
		lbl("viaHelper"), call("registers"), ret(), pad(),
	}
}

func startupData() []chunk {
	return []chunk{
		lbl("tableA"), wordAt("initOne", 0), word(0), wordAt("initTwo", 0),
		lbl("tableB"), word(0xFFFFFFFF), wordAt("initThree", 0),
	}
}

func startupImage(t *testing.T, cinit string) *testImage {
	t.Helper()
	ti := buildImage(t, startupText(), nil, startupData(), nil)
	for _, label := range []string{"cinitInline", "cinitCall", "cinitEmpty", "initOne", "initTwo", "initThree", "initterm", "registrar", "registers", "viaHelper"} {
		if ti.d.Functions[ti.at(t, label)] == nil {
			ti.d.Functions[ti.at(t, label)] = ti.d.buildFunction(ti.at(t, label), nil)
		}
	}
	fn := ti.d.Functions[ti.at(t, cinit)]
	fn.Symbol = &Symbol{VA: fn.EntryVA, Raw: crtInit, Name: crtInit, Lib: "xapilib", ArgBytes: 0}
	return ti
}

func TestStaticInitializersFollowBothWalks(t *testing.T) {
	ti := startupImage(t, "cinitInline")
	inits, err := ti.d.staticInitializers()
	if err != nil {
		t.Fatal(err)
	}
	want := []uint32{ti.at(t, "initOne"), ti.at(t, "initTwo"), ti.at(t, "initThree")}
	if !reflect.DeepEqual(inits, want) {
		t.Errorf("initializers %X, want %X", inits, want)
	}
	ti = startupImage(t, "cinitCall")
	inits, err = ti.d.staticInitializers()
	if err != nil {
		t.Fatal(err)
	}
	if want := []uint32{ti.at(t, "initThree")}; !reflect.DeepEqual(inits, want) {
		t.Errorf("initializers through the walker %X, want %X", inits, want)
	}
}

func TestStaticInitializersNeedTheInitAndItsTables(t *testing.T) {
	ti := startupImage(t, "cinitEmpty")
	if _, err := ti.d.staticInitializers(); err == nil || !strings.Contains(err.Error(), "walks no initializer table") {
		t.Errorf("init without tables: %v", err)
	}
	ti = buildImage(t, startupText(), nil, startupData(), nil)
	if _, err := ti.d.staticInitializers(); err == nil || !strings.Contains(err.Error(), "no _cinit routine") {
		t.Errorf("image without the init: %v", err)
	}
}

func TestCallsWithinFollowsCalleesToADepth(t *testing.T) {
	ti := startupImage(t, "cinitInline")
	registrar := ti.at(t, "registrar")
	if !ti.d.callsWithin(ti.at(t, "registers"), registrar, 0) {
		t.Error("a direct call is not seen at depth 0")
	}
	if ti.d.callsWithin(ti.at(t, "viaHelper"), registrar, 0) {
		t.Error("a call through a helper is seen at depth 0")
	}
	if !ti.d.callsWithin(ti.at(t, "viaHelper"), registrar, 1) {
		t.Error("a call through a helper is not seen at depth 1")
	}
	if ti.d.callsWithin(ti.at(t, "initOne"), registrar, 3) || ti.d.callsWithin(0xDEAD, registrar, 3) {
		t.Error("a routine calling nothing, or no routine, is seen calling the registrar")
	}
}
