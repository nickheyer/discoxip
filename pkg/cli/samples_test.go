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
	scenes, meshes     int
	textures, pngs     int
	materials          int
	programs           int
	nodeTypes          int
	font               int
	files              int
	consoleFiles       []string
	sceneErrors        []string
	kernelErrors       []string
}

var sampleBuilds = []sampleBuild{
	{
		source: "sample/4304", bios: "kernels/4034_orig.bin", mcpx: "kernels/mcpx-1.0.bin",
		xdk: 3944, scenes: 23, meshes: 1194, textures: 4, pngs: 39, materials: 112, programs: 5, nodeTypes: 60, font: 0, files: 92,
		consoleFiles: []string{
			`default.xip`, `dvd.xip`, `jkeyboard.xip`, `keyboard.xip`, `mainmenu5.xip`, `memory2.xip`, `memory_files2.xip`, `message.xip`,
			`music2.xip`, `music_copy3.xip`, `music_playedit2.xip`, `settings3.xip`, `settings_adoc.xip`, `settings_clock.xip`,
			`settings_language.xip`, `settings_list.xip`, `settings_panel.xip`, `settings_parental.xip`, `settings_timezone.xip`,
			`settings_video.xip`, `xbox book.xtf`, `xbox.xtf`, `xboxdash.xbe`,
		},
		sceneErrors:  []string{"00052B0C", "000B0350"},
		kernelErrors: []string{"IoFileObjectType"},
	},
	{
		source: "sample/5659", bios: "kernels/5713_orig.bin", mcpx: "kernels/mcpx-1.1.bin",
		xdk: 5659, scenes: 29, meshes: 1371, textures: 4, pngs: 50, materials: 127, programs: 5, nodeTypes: 61, font: 1, files: 107,
		consoleFiles: []string{
			`fonts\xbox book.xtf`, `fonts\xbox.xtf`, `xbox book.xtf`, `xbox.xtf`, `xboxdash.xbe`,
			`xboxdashdata.17f14d00\accountselection.xip`, `xboxdashdata.17f14d00\default.xip`, `xboxdashdata.17f14d00\dvd.xip`,
			`xboxdashdata.17f14d00\jkeyboard.xip`, `xboxdashdata.17f14d00\keyboard.xip`, `xboxdashdata.17f14d00\livetoday.xip`,
			`xboxdashdata.17f14d00\mainmenu5.xip`, `xboxdashdata.17f14d00\memory2.xip`, `xboxdashdata.17f14d00\memory_files2.xip`,
			`xboxdashdata.17f14d00\message.xip`, `xboxdashdata.17f14d00\music2.xip`, `xboxdashdata.17f14d00\music_copy3.xip`,
			`xboxdashdata.17f14d00\music_playedit2.xip`, `xboxdashdata.17f14d00\passcodeverify.xip`, `xboxdashdata.17f14d00\settings3.xip`,
			`xboxdashdata.17f14d00\settings_adoc.xip`, `xboxdashdata.17f14d00\settings_clock.xip`, `xboxdashdata.17f14d00\settings_language.xip`,
			`xboxdashdata.17f14d00\settings_list.xip`, `xboxdashdata.17f14d00\settings_panel.xip`, `xboxdashdata.17f14d00\settings_parental.xip`,
			`xboxdashdata.17f14d00\settings_timezone.xip`, `xboxdashdata.17f14d00\settings_video.xip`, `xboxdashdata.17f14d00\waitcursor.xip`,
			`xodash\audio\livenowaudio\friend offline.wav`, `xodash\audio\livenowaudio\friend online.wav`,
			`xodash\audio\livenowaudio\friend request.wav`, `xodash\audio\livenowaudio\game invite.wav`,
			`xodash\media\content\savedgame\saveimage.xbx`, `xodash\media\content\savedgame\titleimage.xbx`,
			`xodash\media\content\savedgame\titlemeta.xbx`, `xodash\update.xbe`, `xodash\xonlinedash.xbe`,
		},
		sceneErrors:  []string{"0006D774", "0006FA2C", "000728D2", "00073658", "000777A8", "000C5F25", "0011A2EC", "0011CA15", "0011D068", "00129BDD", "0013ECAC"},
		kernelErrors: []string{},
	},
}

var sampleMissing = []string{"default_memory3:Memory/Tunnel_01-FACES.xm", "default_music2:Music/Tunnel_02-FACES.xm", "default_music2:Music/Tunnel_01-FACES.xm"}

var sampleFormats = []float64{18, 82, 274, 536870914, 536870978, 536871170}

func TestSamplesBuild(t *testing.T) {
	root := filepath.Join("..", "..")
	for _, sb := range sampleBuilds {
		t.Run(filepath.Base(sb.source), func(t *testing.T) {
			out := t.TempDir()
			rootCmd.SetArgs([]string{"build", filepath.Join(root, sb.source), "-o", out, "--bios", filepath.Join(root, sb.bios), "--mcpx", filepath.Join(root, sb.mcpx)})
			if err := rootCmd.Execute(); err != nil {
				t.Fatalf("build: %v", err)
			}
			checkSampleConfig(t, sb, filepath.Join(out, "web", "data"))
			checkSampleRender(t, sb, filepath.Join(out, "web", "data"))
			checkSampleFiles(t, sb, filepath.Join(out, "web"))
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

func sampleStrings(v any) []string {
	var out []string
	for _, e := range v.([]any) {
		out = append(out, e.(string))
	}
	return out
}

func sampleKeys(v any) []string {
	out := []string{}
	for k := range v.(map[string]any) {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func checkSampleConfig(t *testing.T, sb sampleBuild, dataDir string) {
	t.Helper()
	cfg := readSampleJSON(t, filepath.Join(dataDir, "config.json"))
	if cfg["entry"] != "default_default" {
		t.Errorf("entry %v", cfg["entry"])
	}
	if n := len(cfg["scenes"].([]any)); n != sb.scenes {
		t.Errorf("scenes %d, want %d", n, sb.scenes)
	}
	if int(cfg["font_version"].(float64)) != sb.font {
		t.Errorf("font_version %v, want %d", cfg["font_version"], sb.font)
	}
	if got := sampleStrings(cfg["missing"]); !reflect.DeepEqual(got, sampleMissing) {
		t.Errorf("missing %v", got)
	}
	files := sampleStrings(cfg["files"])
	if len(files) != sb.files {
		t.Errorf("files %d, want %d", len(files), sb.files)
	}
	var other []string
	audio := 0
	for _, f := range files {
		if strings.HasPrefix(f, `audio\`) && strings.HasSuffix(f, ".wav") {
			audio++
			continue
		}
		other = append(other, f)
	}
	if !reflect.DeepEqual(other, sb.consoleFiles) {
		t.Errorf("console files %v", other)
	}
	if audio != sb.files-len(sb.consoleFiles) {
		t.Errorf("audio files %d", audio)
	}
	if !sort.StringsAreSorted(files) {
		t.Errorf("files are not sorted")
	}
}

func checkSampleRender(t *testing.T, sb sampleBuild, dataDir string) {
	t.Helper()
	r := readSampleJSON(t, filepath.Join(dataDir, "render.json"))
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
}

func checkSampleFiles(t *testing.T, sb sampleBuild, webDir string) {
	t.Helper()
	for _, name := range []string{"index.html", filepath.Join("data", "image.bin"), filepath.Join("data", "meshes", "manifest.json")} {
		if _, err := os.Stat(filepath.Join(webDir, name)); err != nil {
			t.Error(err)
		}
	}
	manifest := readSampleJSON(t, filepath.Join(webDir, "data", "meshes", "manifest.json"))
	if len(manifest) != sb.meshes {
		t.Errorf("meshes %d, want %d", len(manifest), sb.meshes)
	}
	pngs, _ := filepath.Glob(filepath.Join(webDir, "data", "textures", "*.png"))
	if len(pngs) != sb.pngs+sb.textures {
		t.Errorf("pngs %d, want %d", len(pngs), sb.pngs+sb.textures)
	}
	fonts := 0
	filepath.WalkDir(filepath.Join(webDir, "data", "files"), func(path string, e os.DirEntry, err error) error {
		if err == nil && !e.IsDir() && strings.HasSuffix(path, ".xtf") {
			fonts++
		}
		return err
	})
	want := 0
	for _, f := range sb.consoleFiles {
		if strings.HasSuffix(f, ".xtf") {
			want++
		}
	}
	if fonts != want {
		t.Errorf("fonts copied %d, want %d", fonts, want)
	}
}
