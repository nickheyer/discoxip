package scene

import (
	"fmt"

	"github.com/nickheyer/discoxip/pkg/buffer"
	"github.com/nickheyer/discoxip/pkg/xbe"
)

// LayoutsFromModel maps each vertex format to the declaration the executable's draw path binds to it.
func LayoutsFromModel(rm *xbe.RenderModel) (map[uint32]Layout, error) {
	out := map[uint32]Layout{}
	for _, ff := range rm.FixedFunction {
		decl, ok := rm.Declarations[fmt.Sprintf("%08X", ff.Declaration)]
		if !ok {
			return nil, fmt.Errorf("scene: format %08X binds declaration %08X which was not decoded", ff.Format, ff.Declaration)
		}
		layout := Layout{Stride: decl.Stride, Semantics: map[int]string{}}
		for _, el := range decl.Elements {
			layout.Elements = append(layout.Elements, buffer.Element{Register: el.Register, Type: el.Type, TypeCode: el.TypeCode, Offset: el.Offset, Bytes: el.Bytes, Comps: el.Comps})
			sem, ok := rm.Vertex[el.Register]
			if !ok {
				return nil, fmt.Errorf("scene: declaration %08X uses register %d which has no fixed function meaning", ff.Declaration, el.Register)
			}
			layout.Semantics[el.Register] = sem
		}
		out[ff.Format] = layout
	}
	// Formats without the executable's mesh format bit are D3D flexible vertex formats whose
	// layout the FVF bits define, and the declaration a material program binds to them must agree.
	for _, sb := range rm.ShaderTable {
		if _, done := out[sb.Format]; done {
			continue
		}
		layout, err := fvfLayout(sb.Format)
		if err != nil {
			return nil, err
		}
		decl, ok := rm.Declarations[fmt.Sprintf("%08X", sb.Declaration)]
		if !ok {
			return nil, fmt.Errorf("scene: format %08X binds declaration %08X which was not decoded", sb.Format, sb.Declaration)
		}
		// The stream stride comes from the buffer, so a declaration may read a prefix of the format.
		for i, el := range decl.Elements {
			if i >= len(layout.Elements) || el.Offset != layout.Elements[i].Offset || el.Bytes != layout.Elements[i].Bytes {
				return nil, fmt.Errorf("scene: declaration %08X element %d does not match the layout of vertex format %08X", sb.Declaration, i, sb.Format)
			}
		}
		out[sb.Format] = layout
	}
	return out, nil
}

// D3D flexible vertex format bits
const (
	fvfXYZ      = 0x002
	fvfNormal   = 0x010
	fvfDiffuse  = 0x040
	fvfSpecular = 0x080
	fvfTexMask  = 0xF00
	fvfTexShift = 8
	fvfKnown    = fvfXYZ | fvfNormal | fvfDiffuse | fvfSpecular | fvfTexMask
)

// Expands a flexible vertex format code into its stream layout
func fvfLayout(format uint32) (Layout, error) {
	if format&^uint32(fvfKnown) != 0 || format&fvfXYZ == 0 {
		return Layout{}, fmt.Errorf("scene: vertex format %08X is neither a mesh format the executable declares nor a flexible vertex format", format)
	}
	layout := Layout{Semantics: map[int]string{}}
	add := func(register int, semantic, typ string, code uint32, bytes, comps int) {
		layout.Elements = append(layout.Elements, buffer.Element{Register: register, Type: typ, TypeCode: code, Offset: layout.Stride, Bytes: bytes, Comps: comps})
		layout.Semantics[register] = semantic
		layout.Stride += bytes
	}
	add(0, "position", "FLOAT3", 0x32, 12, 3)
	if format&fvfNormal != 0 {
		add(2, "normal", "FLOAT3", 0x32, 12, 3)
	}
	if format&fvfDiffuse != 0 {
		add(3, "diffuse", "D3DCOLOR", 0x40, 4, 4)
	}
	if format&fvfSpecular != 0 {
		add(4, "specular", "D3DCOLOR", 0x40, 4, 4)
	}
	for i := 0; i < int((format&fvfTexMask)>>fvfTexShift); i++ {
		add(9+i, fmt.Sprintf("texcoord%d", i), "FLOAT2", 0x22, 8, 2)
	}
	return layout, nil
}

// LoadLayouts derives the render model from an executable and returns its vertex layouts.
func LoadLayouts(xbePath string) (map[uint32]Layout, *xbe.RenderModel, error) {
	img, err := xbe.Open(xbePath)
	if err != nil {
		return nil, nil, err
	}
	d, err := xbe.Disassemble(img)
	if err != nil {
		return nil, nil, err
	}
	rm, err := xbe.DeriveRender(d)
	if err != nil {
		return nil, nil, err
	}
	layouts, err := LayoutsFromModel(rm)
	if err != nil {
		return nil, nil, err
	}
	return layouts, rm, nil
}
