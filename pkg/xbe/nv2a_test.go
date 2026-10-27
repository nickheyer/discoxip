package xbe

import "testing"

func TestNV097StateName(t *testing.T) {
	cases := map[uint32]string{0x0300: "ALPHATESTENABLE", 0x030C: "ZENABLE", 0x0354: "ZFUNC", 0x039C: "CULLMODE", 0x09F8: "SWATHWIDTH", 0x17C0: "LOGICOP", 0x00040300: "ALPHATESTENABLE", 0x0004035C: "ZWRITEENABLE"}
	for word, want := range cases {
		got, ok := NV097StateName(word)
		if !ok || got != want {
			t.Errorf("%08X is %q %v, want %q", word, got, ok, want)
		}
	}
	for _, word := range []uint32{0x0380, 0x0000, 0x1FFC, 0x00040394} {
		if got, ok := NV097StateName(word); ok || got != "" {
			t.Errorf("%08X is %q %v, want unknown", word, got, ok)
		}
	}
}
