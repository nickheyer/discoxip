package scene

import (
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/nickheyer/discoxip/pkg/buffer"
	"github.com/nickheyer/discoxip/pkg/xbe"
)

func el(register int, typ string, code uint32, offset, bytes, comps int) buffer.Element {
	return buffer.Element{Register: register, Type: typ, TypeCode: code, Offset: offset, Bytes: bytes, Comps: comps}
}

func TestFVFLayout(t *testing.T) {
	cases := []struct {
		format    uint32
		stride    int
		elements  []buffer.Element
		semantics map[int]string
	}{
		{0x002, 12, []buffer.Element{el(0, "FLOAT3", 0x32, 0, 12, 3)}, map[int]string{0: "position"}},
		{0x012, 24, []buffer.Element{el(0, "FLOAT3", 0x32, 0, 12, 3), el(2, "FLOAT3", 0x32, 12, 12, 3)}, map[int]string{0: "position", 2: "normal"}},
		{0x042, 16, []buffer.Element{el(0, "FLOAT3", 0x32, 0, 12, 3), el(3, "D3DCOLOR", 0x40, 12, 4, 4)}, map[int]string{0: "position", 3: "diffuse"}},
		{0x082, 16, []buffer.Element{el(0, "FLOAT3", 0x32, 0, 12, 3), el(4, "D3DCOLOR", 0x40, 12, 4, 4)}, map[int]string{0: "position", 4: "specular"}},
		{0x052, 28, []buffer.Element{el(0, "FLOAT3", 0x32, 0, 12, 3), el(2, "FLOAT3", 0x32, 12, 12, 3), el(3, "D3DCOLOR", 0x40, 24, 4, 4)}, map[int]string{0: "position", 2: "normal", 3: "diffuse"}},
		{0x0D2, 32, []buffer.Element{el(0, "FLOAT3", 0x32, 0, 12, 3), el(2, "FLOAT3", 0x32, 12, 12, 3), el(3, "D3DCOLOR", 0x40, 24, 4, 4), el(4, "D3DCOLOR", 0x40, 28, 4, 4)}, map[int]string{0: "position", 2: "normal", 3: "diffuse", 4: "specular"}},
		{0x102, 20, []buffer.Element{el(0, "FLOAT3", 0x32, 0, 12, 3), el(9, "FLOAT2", 0x22, 12, 8, 2)}, map[int]string{0: "position", 9: "texcoord0"}},
		{0x142, 24, []buffer.Element{el(0, "FLOAT3", 0x32, 0, 12, 3), el(3, "D3DCOLOR", 0x40, 12, 4, 4), el(9, "FLOAT2", 0x22, 16, 8, 2)}, map[int]string{0: "position", 3: "diffuse", 9: "texcoord0"}},
		{0x212, 40, []buffer.Element{el(0, "FLOAT3", 0x32, 0, 12, 3), el(2, "FLOAT3", 0x32, 12, 12, 3), el(9, "FLOAT2", 0x22, 24, 8, 2), el(10, "FLOAT2", 0x22, 32, 8, 2)}, map[int]string{0: "position", 2: "normal", 9: "texcoord0", 10: "texcoord1"}},
		{0x1D2, 40, []buffer.Element{el(0, "FLOAT3", 0x32, 0, 12, 3), el(2, "FLOAT3", 0x32, 12, 12, 3), el(3, "D3DCOLOR", 0x40, 24, 4, 4), el(4, "D3DCOLOR", 0x40, 28, 4, 4), el(9, "FLOAT2", 0x22, 32, 8, 2)}, map[int]string{0: "position", 2: "normal", 3: "diffuse", 4: "specular", 9: "texcoord0"}},
	}
	for _, c := range cases {
		layout, err := fvfLayout(c.format)
		if err != nil {
			t.Errorf("fvfLayout(%03X): %v", c.format, err)
			continue
		}
		if layout.Stride != c.stride {
			t.Errorf("fvfLayout(%03X).Stride = %d, want %d", c.format, layout.Stride, c.stride)
		}
		if !reflect.DeepEqual(layout.Elements, c.elements) {
			t.Errorf("fvfLayout(%03X).Elements = %+v, want %+v", c.format, layout.Elements, c.elements)
		}
		if !reflect.DeepEqual(layout.Semantics, c.semantics) {
			t.Errorf("fvfLayout(%03X).Semantics = %v, want %v", c.format, layout.Semantics, c.semantics)
		}
	}
}

func TestFVFLayoutFifteenTexcoords(t *testing.T) {
	layout, err := fvfLayout(0xF02)
	if err != nil {
		t.Fatalf("fvfLayout: %v", err)
	}
	if layout.Stride != 12+15*8 || len(layout.Elements) != 16 {
		t.Fatalf("Stride %d with %d elements, want 132 and 16", layout.Stride, len(layout.Elements))
	}
	for i := range 15 {
		want := el(9+i, "FLOAT2", 0x22, 12+8*i, 8, 2)
		if layout.Elements[1+i] != want {
			t.Errorf("texcoord %d element = %+v, want %+v", i, layout.Elements[1+i], want)
		}
		if got := layout.Semantics[9+i]; got != "texcoord"+strconv.Itoa(i) {
			t.Errorf("register %d semantic = %q, want texcoord%d", 9+i, got, i)
		}
	}
}

func TestFVFLayoutRejectsNonFVF(t *testing.T) {
	for _, format := range []uint32{0x000, 0x001, 0x004, 0x010, 0x1002, 0x80000002, 0x40000112, 0x003} {
		layout, err := fvfLayout(format)
		if err == nil {
			t.Errorf("fvfLayout(%08X) = %+v, want an error", format, layout)
			continue
		}
		if !strings.Contains(err.Error(), "flexible vertex format") {
			t.Errorf("fvfLayout(%08X) err = %v", format, err)
		}
	}
}

func declaredModel() *xbe.RenderModel {
	return &xbe.RenderModel{
		Declarations: map[string]*xbe.VertexDeclaration{
			"00401000": {VA: 0x401000, Stride: 28, Elements: []xbe.VertexElement{
				{Register: 0, Type: "FLOAT3", TypeCode: 0x32, Offset: 0, Bytes: 12, Comps: 3},
				{Register: 2, Type: "NORMPACKED3", TypeCode: 0x16, Offset: 12, Bytes: 4, Comps: 3},
				{Register: 3, Type: "D3DCOLOR", TypeCode: 0x40, Offset: 16, Bytes: 4, Comps: 4},
				{Register: 9, Type: "FLOAT2", TypeCode: 0x22, Offset: 20, Bytes: 8, Comps: 2},
			}},
			"00402000": {VA: 0x402000, Stride: 24, Elements: []xbe.VertexElement{
				{Register: 0, Type: "FLOAT3", TypeCode: 0x32, Offset: 0, Bytes: 12, Comps: 3},
				{Register: 2, Type: "FLOAT3", TypeCode: 0x32, Offset: 12, Bytes: 12, Comps: 3},
			}},
			"00403000": {VA: 0x403000, Stride: 16, Elements: []xbe.VertexElement{
				{Register: 0, Type: "FLOAT3", TypeCode: 0x32, Offset: 0, Bytes: 12, Comps: 3},
				{Register: 2, Type: "NORMPACKED3", TypeCode: 0x16, Offset: 12, Bytes: 4, Comps: 3},
			}},
		},
		FixedFunction: []xbe.FFDeclaration{{Format: 0x80000001, Declaration: 0x401000}},
		Vertex:        map[int]string{0: "position", 2: "normal", 3: "diffuse", 4: "specular", 9: "texcoord0", 10: "texcoord1"},
		ShaderTable: []xbe.ShaderBinding{
			{Format: 0x80000001, Declaration: 0xDEAD, Program: 1},
			{Format: 0x112, Declaration: 0x402000, Program: 2},
		},
	}
}

func TestLayoutsFromModel(t *testing.T) {
	layouts, err := LayoutsFromModel(declaredModel())
	if err != nil {
		t.Fatalf("LayoutsFromModel: %v", err)
	}
	if len(layouts) != 2 {
		t.Fatalf("got %d layouts, want 2, %v", len(layouts), layouts)
	}
	ff := layouts[0x80000001]
	wantFF := Layout{
		Stride: 28,
		Elements: []buffer.Element{
			el(0, "FLOAT3", 0x32, 0, 12, 3),
			el(2, "NORMPACKED3", 0x16, 12, 4, 3),
			el(3, "D3DCOLOR", 0x40, 16, 4, 4),
			el(9, "FLOAT2", 0x22, 20, 8, 2),
		},
		Semantics: map[int]string{0: "position", 2: "normal", 3: "diffuse", 9: "texcoord0"},
	}
	if !reflect.DeepEqual(ff, wantFF) {
		t.Errorf("fixed function layout = %+v, want %+v", ff, wantFF)
	}
	fvf := layouts[0x112]
	wantFVF := Layout{
		Stride: 32,
		Elements: []buffer.Element{
			el(0, "FLOAT3", 0x32, 0, 12, 3),
			el(2, "FLOAT3", 0x32, 12, 12, 3),
			el(9, "FLOAT2", 0x22, 24, 8, 2),
		},
		Semantics: map[int]string{0: "position", 2: "normal", 9: "texcoord0"},
	}
	if !reflect.DeepEqual(fvf, wantFVF) {
		t.Errorf("shader table layout = %+v, want %+v", fvf, wantFVF)
	}
}

func TestLayoutsFromModelEmpty(t *testing.T) {
	layouts, err := LayoutsFromModel(&xbe.RenderModel{})
	if err != nil || len(layouts) != 0 {
		t.Errorf("LayoutsFromModel = %v, %v, want empty and nil", layouts, err)
	}
}

func TestLayoutsFromModelFullDeclarationMatchesFVF(t *testing.T) {
	rm := declaredModel()
	rm.FixedFunction = nil
	rm.ShaderTable = []xbe.ShaderBinding{{Format: 0x012, Declaration: 0x402000, Program: 2}}
	layouts, err := LayoutsFromModel(rm)
	if err != nil {
		t.Fatalf("LayoutsFromModel: %v", err)
	}
	if layouts[0x012].Stride != 24 || len(layouts[0x012].Elements) != 2 {
		t.Errorf("layout = %+v", layouts[0x012])
	}
}

func TestLayoutsFromModelErrors(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(rm *xbe.RenderModel)
		want   string
	}{
		{"fixed function declaration missing", func(rm *xbe.RenderModel) {
			rm.FixedFunction[0].Declaration = 0x409999
		}, "binds declaration 00409999 which was not decoded"},
		{"register without meaning", func(rm *xbe.RenderModel) {
			delete(rm.Vertex, 3)
		}, "uses register 3 which has no fixed function meaning"},
		{"shader declaration missing", func(rm *xbe.RenderModel) {
			rm.ShaderTable[1].Declaration = 0x409999
		}, "format 00000112 binds declaration 00409999 which was not decoded"},
		{"shader format not an FVF", func(rm *xbe.RenderModel) {
			rm.ShaderTable[1].Format = 0x004
		}, "vertex format 00000004 is neither"},
		{"shader declaration disagrees with FVF", func(rm *xbe.RenderModel) {
			rm.ShaderTable[1].Declaration = 0x403000
		}, "declaration 00403000 element 1 does not match the layout of vertex format 00000112"},
		{"shader declaration longer than FVF", func(rm *xbe.RenderModel) {
			rm.ShaderTable[1].Format = 0x002
		}, "declaration 00402000 element 1 does not match the layout of vertex format 00000002"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rm := declaredModel()
			c.mutate(rm)
			layouts, err := LayoutsFromModel(rm)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want one containing %q", err, c.want)
			}
			if layouts != nil {
				t.Errorf("layouts = %v, want nil", layouts)
			}
		})
	}
}
