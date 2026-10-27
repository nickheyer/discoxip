package xbe

import "testing"

func fileStringsImage(t *testing.T) (*Image, map[string]uint32) {
	t.Helper()
	strs := newAsm(0x13000)
	strs.label("menu")
	strs.emit(wideBytes("menu.xip")...)
	strs.label("dashPath")
	strs.emit(wideBytes(`D:\xboxdash\menu.xip`)...)
	strs.label("skins")
	strs.emit(wideBytes(`%s\skins\logo.xip`)...)
	strs.label("font")
	strs.emit(wideBytes("font.xtf")...)
	strs.label("dupe")
	strs.emit(wideBytes("MENU.XIP")...)
	strs.label("short")
	strs.emit(wideBytes("ab")...)
	strs.label("bare")
	strs.emit(wideBytes(".xip")...)
	strs.label("tiny")
	strs.emit(wideBytes("x.xip")...)
	strs.label("broken")
	strs.emit(wideBytes("bro")[:6]...)
	strs.emit(0x07, 0x00)
	strs.emit(wideBytes("ken.xip")...)
	strs.label("unref")
	strs.emit(wideBytes(`E:\unused\extra.xip`)...)
	strs.label("sub")
	strs.emit(wideBytes(`Z:\Dash\Sub\deep.xip`)...)
	strs.label("tail")
	strs.emit(wideBytes("tail.xip")[:14]...)
	code := newAsm(0x11000)
	for name, va := range strs.labels {
		code.labels[name] = va
	}
	code.label("entry")
	code.abs32([]byte{0x68}, "skins")
	code.abs32([]byte{0x68}, "dashPath")
	code.abs32([]byte{0xA1}, "sub")
	code.abs32([]byte{0x68}, "menu")
	code.abs32([]byte{0x68}, "dashPath")
	code.emit(0xC3)
	img := parseXBE(t, xbeSpec{
		entry:  0x11000,
		kthunk: 0x12000,
		sections: []testSection{
			{name: ".text", va: 0x11000, flags: flagExecutable, data: code.bytes(), vsize: 0x100},
			{name: ".data", va: 0x12000, flags: flagWritable, data: thunkTable(145), vsize: 0x100},
			{name: ".rdata", va: 0x13000, data: strs.bytes(), vsize: 0x400},
		},
	})
	return img, strs.labels
}

func TestWideStrings(t *testing.T) {
	img, at := fileStringsImage(t)
	found := map[uint32]string{}
	for _, ws := range img.wideStrings() {
		found[ws.VA] = ws.Text
	}
	want := map[string]string{"menu": "menu.xip", "dashPath": `D:\xboxdash\menu.xip`, "skins": `%s\skins\logo.xip`, "font": "font.xtf", "dupe": "MENU.XIP", "bare": ".xip", "tiny": "x.xip", "unref": `E:\unused\extra.xip`, "sub": `Z:\Dash\Sub\deep.xip`}
	for label, text := range want {
		if found[at[label]] != text {
			t.Errorf("%s at %08X is %q, want %q", label, at[label], found[at[label]], text)
		}
	}
	for _, label := range []string{"short", "broken", "tail"} {
		if text, ok := found[at[label]]; ok {
			t.Errorf("%s collected as %q", label, text)
		}
	}
	if text, ok := found[at["broken"]+8]; !ok || text != "ken.xip" {
		t.Errorf("string after the break is %q %v", text, ok)
	}
	if len(found) != 10 {
		t.Errorf("collected %d strings %v", len(found), found)
	}
}

func TestNamedFiles(t *testing.T) {
	img, _ := fileStringsImage(t)
	if got := img.NamedFiles(".xip"); !equalStrings(got, []string{"menu.xip", "x.xip", "ken.xip"}) {
		t.Fatalf("xip files %v", got)
	}
	if got := img.NamedFiles(".XTF"); !equalStrings(got, []string{"font.xtf"}) {
		t.Fatalf("xtf files %v", got)
	}
	if got := img.NamedFiles(".bin"); got != nil {
		t.Fatalf("bin files %v", got)
	}
}

func TestDirectoryPreference(t *testing.T) {
	img, _ := fileStringsImage(t)
	d := disassembleImage(t, img)
	if got := d.DirectoryPreference(".xip"); !equalStrings(got, []string{"/skins", "xboxdash", "dash/sub", ""}) {
		t.Fatalf("directories %v", got)
	}
	if got := d.DirectoryPreference(".xtf"); len(got) != 0 {
		t.Fatalf("unreferenced extension lists %v", got)
	}
}
