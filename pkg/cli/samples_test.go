//go:build samples

package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

type sampleBuild struct {
	source, bios, mcpx string
	xdk                int
	scenes, pngs       int
	fonts              int
	materials          int
	programs           int
	textures           int
	nodeTypes          int
	sceneErrors        []string
	kernelErrors       []string
}

var sampleBuilds = []sampleBuild{
	{
		source: "sample/4304", bios: "kernels/4034_orig.bin", mcpx: "kernels/mcpx-1.0.bin",
		xdk: 3944, scenes: 23, pngs: 39, fonts: 2, materials: 112, programs: 5, textures: 4, nodeTypes: 60,
		sceneErrors:  []string{},
		kernelErrors: []string{},
	},
	{
		source: "sample/5659", bios: "kernels/5713_orig.bin", mcpx: "kernels/mcpx-1.1.bin",
		xdk: 5659, scenes: 29, pngs: 50, fonts: 2, materials: 127, programs: 5, textures: 4, nodeTypes: 61,
		sceneErrors:  []string{},
		kernelErrors: []string{},
	},
}

var sampleFormats = []float64{18, 82, 274, 536870914, 536870978, 536871170}

// The build extracts every archive the executable names beside the fonts and the executable itself
func TestSamplesBuild(t *testing.T) {
	root := filepath.Join("..", "..")
	for _, sb := range sampleBuilds {
		t.Run(filepath.Base(sb.source), func(t *testing.T) {
			out := t.TempDir()
			rootCmd.SetArgs([]string{"build", filepath.Join(root, sb.source), "-o", out, "--bios", filepath.Join(root, sb.bios), "--mcpx", filepath.Join(root, sb.mcpx)})
			if err := rootCmd.Execute(); err != nil {
				t.Fatalf("build: %v", err)
			}
			extracted := filepath.Join(out, ".extracted")
			counts := map[string]int{}
			filepath.WalkDir(extracted, func(path string, e os.DirEntry, err error) error {
				if err == nil && !e.IsDir() {
					counts[strings.ToLower(filepath.Ext(path))]++
				}
				return err
			})
			if counts[".xap"] != sb.scenes {
				t.Errorf("scenes %d, want %d", counts[".xap"], sb.scenes)
			}
			if counts[".png"] != sb.pngs {
				t.Errorf("pngs %d, want %d", counts[".png"], sb.pngs)
			}
			if counts[".xtf"] != sb.fonts {
				t.Errorf("fonts %d, want %d", counts[".xtf"], sb.fonts)
			}
			if counts[".xbe"] != 1 {
				t.Errorf("executables %d, want 1", counts[".xbe"])
			}
		})
	}
}

// The render derivation names the XDK build, the materials, programs, textures, classes and formats of each sample
func TestSamplesRender(t *testing.T) {
	root := filepath.Join("..", "..")
	for _, sb := range sampleBuilds {
		t.Run(filepath.Base(sb.source), func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "render.json")
			xbes, _ := filepath.Glob(filepath.Join(root, sb.source, "*.xbe"))
			if len(xbes) != 1 {
				t.Fatalf("executables %v", xbes)
			}
			rootCmd.SetArgs([]string{"xbe", "render", xbes[0], "-o", out, "--bios", filepath.Join(root, sb.bios), "--mcpx", filepath.Join(root, sb.mcpx)})
			if err := rootCmd.Execute(); err != nil {
				t.Fatalf("render: %v", err)
			}
			r := readSampleJSON(t, out)
			if int(r["xdk"].(float64)) != sb.xdk {
				t.Errorf("xdk %v", r["xdk"])
			}
			if n := len(r["materials"].([]any)); n != sb.materials {
				t.Errorf("materials %d, want %d", n, sb.materials)
			}
			if n := len(r["programs"].(map[string]any)); n != sb.programs {
				t.Errorf("programs %d, want %d", n, sb.programs)
			}
			if n := len(r["textures"].([]any)); n != sb.textures {
				t.Errorf("textures %d, want %d", n, sb.textures)
			}
			if n := len(r["node_types"].(map[string]any)); n != sb.nodeTypes {
				t.Errorf("node types %d, want %d", n, sb.nodeTypes)
			}
			var formats []float64
			for _, f := range r["vertex_formats"].([]any) {
				formats = append(formats, f.(float64))
			}
			if !reflect.DeepEqual(formats, sampleFormats) {
				t.Errorf("vertex formats %v", formats)
			}
			if r["gpu"].(float64) != 0xFD000000 {
				t.Errorf("gpu %v", r["gpu"])
			}
			if got := sampleKeys(r["scene"].(map[string]any)["errors"]); !reflect.DeepEqual(got, sb.sceneErrors) {
				t.Errorf("scene errors %v, want %v", got, sb.sceneErrors)
			}
			if got := sampleKeys(r["kernel"].(map[string]any)["errors"]); !reflect.DeepEqual(got, sb.kernelErrors) {
				t.Errorf("kernel errors %v, want %v", got, sb.kernelErrors)
			}
		})
	}
}

func readSampleJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var v map[string]any
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return v
}

func sampleKeys(v any) []string {
	out := []string{}
	for k := range v.(map[string]any) {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
