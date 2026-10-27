package mesh

import (
	"reflect"
	"testing"
)

const sceneText = "#VRML V2.0 utf8\n" +
	"DEF root Transform {\n" +
	"  children [\n" +
	"    DEF body Shape {\n" +
	"      appearance Appearance {\n" +
	"        material DEF red Material { diffuseColor 1 0 0 }\n" +
	"      }\n" +
	"      geometry DEF geo Mesh { url \"models/body.xm\" }\n" +
	"    }\n" +
	"    Shape { geometry Mesh { url\t\"models/wheel.xm\" } }\n" +
	"    Shape { geometry Mesh { url \"\" } }\n" +
	"    Inline { url\n      \"sub/inline.wrl\" }\n" +
	"  ]\n" +
	"}\n"

var (
	sceneNames = []string{"root", "body", "red", "geo"}
	sceneRefs  = []string{"models/body.xm", "models/wheel.xm", "sub/inline.wrl"}
)

func TestParseText(t *testing.T) {
	got := ParseText([]byte(sceneText))
	want := &TextMesh{Source: sceneText, NodeNames: sceneNames, MeshRefs: sceneRefs}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ParseText\n got %+v\nwant %+v", got, want)
	}
}

func TestParseTextPatterns(t *testing.T) {
	cases := []struct {
		name  string
		text  string
		names []string
		refs  []string
	}{
		{"empty", "", nil, nil},
		{"tab separated def", "DEF\tname Shape", []string{"name"}, nil},
		{"def without name", "DEF", nil, nil},
		{"def glued to name", "DEFx y", nil, nil},
		{"url with newline", "url\n\"a.xm\"", nil, []string{"a.xm"}},
		{"empty url", `url ""`, nil, nil},
		{"url without space", `url"a.xm"`, nil, nil},
		{"multiple", `DEF a X { url "1" } DEF b Y { url "2" }`, []string{"a", "b"}, []string{"1", "2"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ParseText([]byte(c.text))
			if got.Source != c.text {
				t.Fatalf("Source = %q want %q", got.Source, c.text)
			}
			if !reflect.DeepEqual(got.NodeNames, c.names) {
				t.Fatalf("NodeNames = %v want %v", got.NodeNames, c.names)
			}
			if !reflect.DeepEqual(got.MeshRefs, c.refs) {
				t.Fatalf("MeshRefs = %v want %v", got.MeshRefs, c.refs)
			}
		})
	}
}
