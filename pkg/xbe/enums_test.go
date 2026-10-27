package xbe

import "testing"

func TestRenderStateNamesByBuild(t *testing.T) {
	cases := []struct {
		build int
		count int
		names map[uint32]string
	}{
		{3424, 138, map[uint32]string{0: "PSALPHAINPUTS0", 56: "PSINPUTTEXTURE", 57: "ZFUNC", 81: "SOLIDOFFSETENABLE", 82: "FOGENABLE", 115: "PATCHSEGMENTS", 116: "PSTEXTUREMODES", 133: "MULTISAMPLEMASK", 134: "MULTISAMPLETYPE", 135: "SHADOWFUNC", 137: "DXT1NOISEENABLE"}},
		{3911, 144, map[uint32]string{57: "ZFUNC", 82: "FOGENABLE", 116: "PSTEXTUREMODES", 134: "MULTISAMPLETYPE", 137: "DXT1NOISEENABLE", 138: "YUVENABLE", 143: "DONOTCULLUNCOMPRESSED"}},
		{4034, 146, map[uint32]string{57: "ZFUNC", 82: "FOGENABLE", 116: "SWAPFILTER", 117: "PSTEXTUREMODES", 134: "MULTISAMPLEMASK", 135: "MULTISAMPLEMODE", 136: "MULTISAMPLERENDERTARGETMODE", 137: "SHADOWFUNC", 145: "DONOTCULLUNCOMPRESSED"}},
		{4432, 147, map[uint32]string{81: "SOLIDOFFSETENABLE", 82: "DEPTHCLIPCONTROL", 83: "FOGENABLE", 117: "SWAPFILTER", 118: "PSTEXTUREMODES", 146: "DONOTCULLUNCOMPRESSED"}},
		{4627, 166, map[uint32]string{82: "DEPTHCLIPCONTROL", 83: "STIPPLEENABLE", 84: "SIMPLE_UNUSED8", 91: "SIMPLE_UNUSED1", 92: "FOGENABLE", 126: "SWAPFILTER", 127: "PRESENTATIONINTERVAL", 128: "DEFERRED_UNUSED8", 135: "DEFERRED_UNUSED1", 136: "PSTEXTUREMODES", 153: "MULTISAMPLEMASK", 154: "MULTISAMPLEMODE", 157: "LINEWIDTH", 158: "SAMPLEALPHA", 159: "DXT1NOISEENABLE", 165: "DONOTCULLUNCOMPRESSED"}},
		{5933, 166, map[uint32]string{136: "PSTEXTUREMODES", 158: "SAMPLEALPHA", 165: "DONOTCULLUNCOMPRESSED"}},
	}
	for _, c := range cases {
		names := RenderStateNames(c.build)
		if len(names) != c.count {
			t.Errorf("build %d has %d states, want %d", c.build, len(names), c.count)
		}
		for idx, want := range c.names {
			if names[idx] != want {
				t.Errorf("build %d state %d is %q, want %q", c.build, idx, names[idx], want)
			}
		}
		for i := uint32(0); i < uint32(c.count); i++ {
			if names[i] == "" {
				t.Errorf("build %d leaves index %d unnamed", c.build, i)
			}
		}
	}
	for name, builds := range map[string][2]int{"MULTISAMPLETYPE": {4033, 4034}, "YUVENABLE": {3910, 3911}, "SWAPFILTER": {4033, 4034}, "STIPPLEENABLE": {4626, 4627}, "DEPTHCLIPCONTROL": {4431, 4432}} {
		before, after := RenderStateNames(builds[0]), RenderStateNames(builds[1])
		if name == "MULTISAMPLETYPE" {
			before, after = after, before
		}
		if hasValue(before, name) {
			t.Errorf("%s present at build %d", name, builds[0])
		}
		if !hasValue(after, name) {
			t.Errorf("%s absent at build %d", name, builds[1])
		}
	}
}

func hasValue(m map[uint32]string, want string) bool {
	for _, v := range m {
		if v == want {
			return true
		}
	}
	return false
}

func TestRenderStateSections(t *testing.T) {
	cases := map[int][3]uint32{3424: {57, 82, 116}, 3911: {57, 82, 116}, 4034: {57, 82, 117}, 4432: {57, 83, 118}, 4627: {57, 92, 136}, 5933: {57, 92, 136}}
	for build, want := range cases {
		if got := RenderStateSections(build); got != want {
			t.Errorf("build %d sections %v, want %v", build, got, want)
		}
		names := RenderStateNames(build)
		got := RenderStateSections(build)
		if names[got[0]] != "ZFUNC" || names[got[1]] != "FOGENABLE" || names[got[2]] != "PSTEXTUREMODES" {
			t.Errorf("build %d section starts name %q %q %q", build, names[got[0]], names[got[1]], names[got[2]])
		}
	}
}

func TestTextureStageStateNames(t *testing.T) {
	late := TextureStageStateNames(3949)
	if len(late) != 32 {
		t.Fatalf("late count %d", len(late))
	}
	for idx, want := range map[uint32]string{0: "ADDRESSU", 11: "ALPHAKILL", 12: "COLOROP", 21: "TEXTURETRANSFORMFLAGS", 22: "BUMPENVMAT00", 28: "TEXCOORDINDEX", 31: "UNSUPPORTED"} {
		if late[idx] != want {
			t.Errorf("3949 state %d is %q, want %q", idx, late[idx], want)
		}
	}
	early := TextureStageStateNames(3948)
	if len(early) != 32 {
		t.Fatalf("early count %d", len(early))
	}
	for idx, want := range map[uint32]string{0: "COLOROP", 1: "COLORARG0", 4: "ALPHAOP", 8: "RESULTARG", 9: "TEXTURETRANSFORMFLAGS", 10: "ADDRESSU", 13: "MAGFILTER", 21: "ALPHAKILL", 22: "BUMPENVMAT00", 31: "UNSUPPORTED"} {
		if early[idx] != want {
			t.Errorf("3948 state %d is %q, want %q", idx, early[idx], want)
		}
	}
	if m := TextureStageStateNames(3911); m[0] != "COLOROP" || m[10] != "ADDRESSU" {
		t.Errorf("3911 states %q %q", m[0], m[10])
	}
	if m := TextureStageStateNames(5933); m[0] != "ADDRESSU" || m[12] != "COLOROP" {
		t.Errorf("5933 states %q %q", m[0], m[12])
	}
}

func TestTransformStateNumbers(t *testing.T) {
	if transformView != 0 || transformProjection != 1 || transformWorld != 6 {
		t.Fatalf("transform numbers %d %d %d", transformView, transformProjection, transformWorld)
	}
}

func TestVertexDataTypeTable(t *testing.T) {
	want := map[uint32][3]interface{}{
		0x12: {"FLOAT1", 4, 1}, 0x22: {"FLOAT2", 8, 2}, 0x32: {"FLOAT3", 12, 3}, 0x42: {"FLOAT4", 16, 4},
		0x40: {"D3DCOLOR", 4, 4}, 0x25: {"SHORT2", 4, 2}, 0x45: {"SHORT4", 8, 4},
		0x11: {"NORMSHORT1", 2, 1}, 0x21: {"NORMSHORT2", 4, 2}, 0x31: {"NORMSHORT3", 6, 3}, 0x41: {"NORMSHORT4", 8, 4},
		0x16: {"NORMPACKED3", 4, 3}, 0x15: {"SHORT1", 2, 1}, 0x35: {"SHORT3", 6, 3},
		0x14: {"PBYTE1", 1, 1}, 0x24: {"PBYTE2", 2, 2}, 0x34: {"PBYTE3", 3, 3}, 0x44: {"PBYTE4", 4, 4},
		0x72: {"FLOAT2H", 12, 3}, 0x02: {"NONE", 0, 0},
	}
	if len(vertexDataTypes) != len(want) {
		t.Fatalf("table has %d types, want %d", len(vertexDataTypes), len(want))
	}
	for code, w := range want {
		dt, ok := vertexDataTypes[code]
		if !ok || dt.Name != w[0] || dt.Bytes != w[1] || dt.Comps != w[2] {
			t.Errorf("type %02X is %+v, want %v", code, dt, w)
		}
	}
}
