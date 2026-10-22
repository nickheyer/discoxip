package web

import (
	"embed"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/nickheyer/discoxip/pkg/font"
	"github.com/nickheyer/discoxip/pkg/scene"
	"github.com/nickheyer/discoxip/pkg/xap"
	"github.com/nickheyer/discoxip/pkg/xbe"
)

//go:embed static/index.html static/runtime.js static/derived.js static/pipeline.js static/system.js
var staticFiles embed.FS

// Export generates a self-contained web application from extracted XIP data and the disassembled dashboard executable.
func Export(inputDir, outputDir string, d *xbe.Disassembly) error {
	for _, dir := range []string{
		outputDir,
		filepath.Join(outputDir, "data", "scenes"),
		filepath.Join(outputDir, "data", "meshes"),
		filepath.Join(outputDir, "data", "textures"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("web: mkdir %s: %w", dir, err)
		}
	}

	xapFiles, err := filepath.Glob(filepath.Join(inputDir, "*.xap"))
	if err != nil {
		return err
	}
	if len(xapFiles) == 0 {
		return fmt.Errorf("web: no .xap files found in %s", inputDir)
	}
	fmt.Fprintf(os.Stderr, "Found %d XAP scene(s)\n", len(xapFiles))

	fmt.Fprintf(os.Stderr, "Deriving render model from the executable\n")
	layouts, model, err := scene.LoadLayouts(d)
	if err != nil {
		return fmt.Errorf("web: %w", err)
	}
	if err := writeRenderModel(model, filepath.Join(outputDir, "data")); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "  %d materials, %d vertex programs, %d procedural textures\n", len(model.Materials), len(model.Programs), len(model.Textures))

	config := buildConfig(xapFiles)
	meshManifest := make(map[string]meshEntry)
	var missing []string

	for _, xapPath := range xapFiles {
		baseName := strings.TrimSuffix(filepath.Base(xapPath), ".xap")
		fmt.Fprintf(os.Stderr, "  Processing %s\n", baseName)

		ast, err := xap.ParseFile(xapPath)
		if err != nil {
			return fmt.Errorf("web: parsing %s: %w", baseName, err)
		}
		sceneJSON := serializeScene(ast)
		jsonData, err := json.MarshalIndent(sceneJSON, "", "  ")
		if err != nil {
			return fmt.Errorf("web: marshaling scene %s: %w", baseName, err)
		}
		jsonPath := filepath.Join(outputDir, "data", "scenes", baseName+".json")
		if err := os.WriteFile(jsonPath, jsonData, 0o644); err != nil {
			return err
		}

		s, err := scene.Load(xapPath, layouts)
		if err != nil {
			return fmt.Errorf("web: %s: %w", baseName, err)
		}
		for _, url := range s.Missing {
			fmt.Fprintf(os.Stderr, "    missing: %s is packed in no extracted archive\n", url)
			missing = append(missing, baseName+":"+url)
		}
		archiveName := extractArchiveName(baseName)
		for url, md := range s.Meshes {
			if md.Count == 0 {
				continue
			}
			key := archiveName + ":" + url
			if _, exists := meshManifest[key]; exists {
				continue
			}
			binName := sanitizeFilename(archiveName+"_"+strings.TrimSuffix(url, filepath.Ext(url))) + ".bin"
			binPath := filepath.Join(outputDir, "data", "meshes", binName)
			entry, err := exportMeshBinary(binPath, md, layouts[md.Format])
			if err != nil {
				return fmt.Errorf("web: mesh %s: %w", url, err)
			}
			meshManifest[key] = entry
		}
	}

	manifestData, err := json.MarshalIndent(meshManifest, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(outputDir, "data", "meshes", "manifest.json"), manifestData, 0o644); err != nil {
		return err
	}
	config.Missing = missing

	texCount := copyTextures(inputDir, filepath.Join(outputDir, "data", "textures"))
	fmt.Fprintf(os.Stderr, "  Copied %d textures\n", texCount)

	audioCount := copyAudio(inputDir, filepath.Join(outputDir, "data"))
	if audioCount > 0 {
		fmt.Fprintf(os.Stderr, "  Copied %d audio files\n", audioCount)
	}

	fontCount := exportFonts(inputDir, filepath.Join(outputDir, "data", "fonts"))
	if fontCount > 0 {
		fmt.Fprintf(os.Stderr, "  Exported %d fonts\n", fontCount)
	}

	configData, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(outputDir, "data", "config.json"), configData, 0o644); err != nil {
		return err
	}

	for _, name := range []string{"static/index.html", "static/runtime.js", "static/derived.js", "static/pipeline.js", "static/system.js"} {
		data, err := staticFiles.ReadFile(name)
		if err != nil {
			return fmt.Errorf("web: reading embedded %s: %w", name, err)
		}
		if err := os.WriteFile(filepath.Join(outputDir, filepath.Base(name)), data, 0o644); err != nil {
			return err
		}
	}

	fmt.Fprintf(os.Stderr, "Web app exported to %s\n", outputDir)
	fmt.Fprintf(os.Stderr, "  Scenes: %d, Meshes: %d, Textures: %d\n", len(xapFiles), len(meshManifest), texCount)
	return nil
}

// Writes render.json and the procedural textures the executable generates at runtime
func writeRenderModel(model *xbe.RenderModel, dataDir string) error {
	data, err := json.Marshal(model)
	if err != nil {
		return fmt.Errorf("web: encoding render model: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "render.json"), data, 0o644); err != nil {
		return err
	}
	for _, tex := range model.Textures {
		img := image.NewNRGBA(image.Rect(0, 0, tex.Width, tex.Height))
		for i, px := range tex.Pixels {
			img.Pix[i*4+0] = uint8(px >> 16)
			img.Pix[i*4+1] = uint8(px >> 8)
			img.Pix[i*4+2] = uint8(px)
			img.Pix[i*4+3] = uint8(px >> 24)
		}
		f, err := os.Create(filepath.Join(dataDir, "textures", fmt.Sprintf("proc_%08X.png", tex.VA)))
		if err != nil {
			return err
		}
		if err := png.Encode(f, img); err != nil {
			f.Close()
			return err
		}
		f.Close()
	}
	return nil
}

// --- Config ---

type exportConfig struct {
	Entry    string            `json:"entry"`
	Archives map[string]string `json:"archives"`
	Inlines  map[string]string `json:"inlines"`
	Scenes   []string          `json:"scenes"`
	Missing  []string          `json:"missing"`
}

func buildConfig(xapFiles []string) exportConfig {
	cfg := exportConfig{
		Archives: make(map[string]string),
		Inlines:  make(map[string]string),
	}

	var sceneNames []string
	for _, f := range xapFiles {
		name := strings.TrimSuffix(filepath.Base(f), ".xap")
		sceneNames = append(sceneNames, name)
	}
	cfg.Scenes = sceneNames

	// Find entry scene (default_default)
	for _, name := range sceneNames {
		if name == "default_default" {
			cfg.Entry = name
			break
		}
	}
	if cfg.Entry == "" && len(sceneNames) > 0 {
		cfg.Entry = sceneNames[0]
	}

	// Build archive mapping: "MainMenu5" → "mainmenu5_default"
	// XAP files named {archive}_{scene}.xap where scene="default" is the main entry
	for _, name := range sceneNames {
		parts := splitArchiveScene(name)
		if parts[1] == "default" {
			// This is the main scene for this archive
			// Map the archive name (case-insensitive) to this scene
			cfg.Archives[parts[0]] = name
			// Also map with original case from Level references
			cfg.Archives[strings.ToLower(parts[0])] = name
		}
	}

	// Build inline mapping: "Music2.xap" → "default_music2"
	// Inlines from default.xip reference other XAPs in the same archive
	for _, name := range sceneNames {
		parts := splitArchiveScene(name)
		if parts[0] == "default" && parts[1] != "default" {
			// e.g., default_music2 → inline URL "Music2.xap"
			inlineURL := parts[1] + ".xap"
			cfg.Inlines[inlineURL] = name
			// Also try capitalized variants
			cfg.Inlines[capitalize(parts[1])+".xap"] = name
		}
	}

	return cfg
}

func splitArchiveScene(name string) [2]string {
	// Split "mainmenu5_default" → ["mainmenu5", "default"]
	// Split "settings_Clock_default" → ["settings_Clock", "default"]
	// Last underscore-separated component is the scene name
	idx := strings.LastIndex(name, "_")
	if idx < 0 {
		return [2]string{name, "default"}
	}
	return [2]string{name[:idx], name[idx+1:]}
}

func extractArchiveName(baseName string) string {
	return splitArchiveScene(baseName)[0]
}

func capitalize(s string) string {
	if len(s) == 0 {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// --- Scene JSON serialization ---

type jsonScene struct {
	Items []jsonItem `json:"items"`
}

type jsonItem struct {
	Kind   string    `json:"kind"`
	Node   *jsonNode `json:"node,omitempty"`
	Script string    `json:"script,omitempty"`
}

type jsonNode struct {
	Type     string                 `json:"type"`
	Def      string                 `json:"def,omitempty"`
	Fields   map[string]interface{} `json:"fields,omitempty"`
	Children []*jsonNode            `json:"children,omitempty"`
	Scripts  []string               `json:"scripts,omitempty"`
}

func serializeScene(s *xap.Scene) jsonScene {
	var js jsonScene
	for _, item := range s.Items {
		switch item.Kind {
		case xap.SNode:
			if item.Node != nil {
				js.Items = append(js.Items, jsonItem{
					Kind: "node",
					Node: serializeNode(item.Node),
				})
			}
		case xap.SScript:
			js.Items = append(js.Items, jsonItem{
				Kind:   "script",
				Script: item.Script,
			})
		}
	}
	return js
}

func serializeNode(n *xap.Node) *jsonNode {
	if n == nil {
		return nil
	}

	jn := &jsonNode{
		Type: n.TypeName,
		Def:  n.DefName,
	}

	// Serialize fields
	if len(n.Fields) > 0 {
		jn.Fields = make(map[string]interface{})
		for _, f := range n.Fields {
			jn.Fields[f.Key] = serializeFieldValues(f.Values)
		}
	}

	// Serialize children
	for _, child := range n.Children {
		jn.Children = append(jn.Children, serializeNode(child))
	}

	// Scripts
	jn.Scripts = n.Scripts

	return jn
}

func serializeFieldValues(values []xap.Value) interface{} {
	if len(values) == 0 {
		return nil
	}

	// Single value
	if len(values) == 1 {
		return serializeValue(values[0])
	}

	// Multiple values — check if all numbers (vector)
	allNumbers := true
	for _, v := range values {
		if v.Kind != xap.VNumber {
			allNumbers = false
			break
		}
	}
	if allNumbers {
		nums := make([]float64, len(values))
		for i, v := range values {
			nums[i] = v.Num
		}
		return nums
	}

	// Mixed values
	var result []interface{}
	for _, v := range values {
		result = append(result, serializeValue(v))
	}
	return result
}

func serializeValue(v xap.Value) interface{} {
	switch v.Kind {
	case xap.VNumber:
		return v.Num
	case xap.VString:
		return v.Str
	case xap.VBool:
		return v.Bool
	case xap.VIdent:
		return v.Str
	case xap.VScript:
		return map[string]interface{}{"_script": v.Str}
	case xap.VNode:
		if v.Node != nil {
			return map[string]interface{}{"_node": serializeNode(v.Node)}
		}
		return nil
	case xap.VArray:
		var arr []interface{}
		for _, av := range v.Array {
			arr = append(arr, serializeValue(av))
		}
		return arr
	default:
		return nil
	}
}

// --- Mesh binary export ---

type meshEntry struct {
	File        string          `json:"file"`
	VertexCount int             `json:"vertexCount"`
	IndexCount  int             `json:"indexCount"`
	Format      uint32          `json:"format"`
	Stride      int             `json:"stride"`
	Attributes  []meshAttribute `json:"attributes"`
}

type meshAttribute struct {
	Register int    `json:"register"`
	Semantic string `json:"semantic"`
	Type     string `json:"type"`
	Offset   int    `json:"offset"`
	Comps    int    `json:"comps"`
}

// Writes every declared attribute as float32 arrays followed by uint16 indices
func exportMeshBinary(path string, md *scene.MeshData, layout scene.Layout) (meshEntry, error) {
	f, err := os.Create(path)
	if err != nil {
		return meshEntry{}, err
	}
	defer f.Close()
	entry := meshEntry{File: filepath.Base(path), VertexCount: md.Count, IndexCount: len(md.Indices), Format: md.Format, Stride: layout.Stride}
	buf := make([]byte, 4)
	for _, a := range md.Attributes {
		entry.Attributes = append(entry.Attributes, meshAttribute{Register: a.Element.Register, Semantic: layout.Semantics[a.Element.Register], Type: a.Element.Type, Offset: a.Element.Offset, Comps: a.Element.Comps})
		for _, v := range a.Data {
			binary.LittleEndian.PutUint32(buf, math.Float32bits(v))
			if _, err := f.Write(buf); err != nil {
				return meshEntry{}, err
			}
		}
	}
	buf2 := make([]byte, 2)
	for _, idx := range md.Indices {
		binary.LittleEndian.PutUint16(buf2, idx)
		if _, err := f.Write(buf2); err != nil {
			return meshEntry{}, err
		}
	}
	return entry, nil
}

// --- Texture copy ---

func copyTextures(srcDir, dstDir string) int {
	entries, _ := os.ReadDir(srcDir)
	count := 0
	for _, e := range entries {
		if e.IsDir() || strings.ToLower(filepath.Ext(e.Name())) != ".png" {
			continue
		}
		src := filepath.Join(srcDir, e.Name())
		dst := filepath.Join(dstDir, e.Name())
		data, err := os.ReadFile(src)
		if err != nil {
			continue
		}
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			continue
		}
		count++
	}
	return count
}

// --- Audio copy (Xbox ADPCM → PCM16 conversion) ---

// IMA ADPCM step table
var imaStepTable = [89]int16{
	7, 8, 9, 10, 11, 12, 13, 14, 16, 17,
	19, 21, 23, 25, 28, 31, 34, 37, 41, 45,
	50, 55, 60, 66, 73, 80, 88, 97, 107, 118,
	130, 143, 157, 173, 190, 209, 230, 253, 279, 307,
	337, 371, 408, 449, 494, 544, 598, 658, 724, 796,
	876, 963, 1060, 1166, 1282, 1411, 1552, 1707, 1878, 2066,
	2272, 2499, 2749, 3024, 3327, 3660, 4026, 4428, 4871, 5358,
	5894, 6484, 7132, 7845, 8630, 9493, 10442, 11487, 12635, 13899,
	15289, 16818, 18500, 20350, 22385, 24623, 27086, 29794, 32767,
}

var imaIndexTable = [16]int{
	-1, -1, -1, -1, 2, 4, 6, 8,
	-1, -1, -1, -1, 2, 4, 6, 8,
}

func clampIndex(idx int) int {
	if idx < 0 {
		return 0
	}
	if idx > 88 {
		return 88
	}
	return idx
}

func clampSample(s int32) int16 {
	if s > 32767 {
		return 32767
	}
	if s < -32768 {
		return -32768
	}
	return int16(s)
}

// decodeXboxADPCM converts Xbox ADPCM (format 0x0069) WAV data to PCM16 WAV.
//
// Xbox ADPCM block layout (e.g. stereo, blockAlign=72):
//
//	[ch0 header 4B][ch1 header 4B][interleaved nibble data 64B]
//
// Header: int16 predictor, uint8 stepIndex, uint8 reserved
// Data: groups of (channels × 4 bytes), each 4 bytes = 8 nibbles = 8 samples.
// The predictor seeds the decoder but is NOT emitted as an output sample.
func decodeXboxADPCM(data []byte) ([]byte, error) {
	if len(data) < 44 || string(data[0:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		return nil, fmt.Errorf("not a WAV file")
	}

	// Find fmt and data chunks
	pos := 12
	var fmtChunk, dataChunk []byte
	for pos+8 <= len(data) {
		id := string(data[pos : pos+4])
		size := int(binary.LittleEndian.Uint32(data[pos+4 : pos+8]))
		switch id {
		case "fmt ":
			fmtChunk = data[pos+8 : pos+8+size]
		case "data":
			dataChunk = data[pos+8 : pos+8+size]
		}
		pos += 8 + size
		if pos%2 != 0 {
			pos++
		}
	}
	if fmtChunk == nil || dataChunk == nil {
		return nil, fmt.Errorf("missing fmt or data chunk")
	}

	format := binary.LittleEndian.Uint16(fmtChunk[0:2])
	if format != 0x0069 {
		return data, nil // not Xbox ADPCM, pass through
	}

	channels := int(binary.LittleEndian.Uint16(fmtChunk[2:4]))
	sampleRate := binary.LittleEndian.Uint32(fmtChunk[4:8])
	blockAlign := int(binary.LittleEndian.Uint16(fmtChunk[12:14]))
	if channels == 0 || blockAlign == 0 {
		return nil, fmt.Errorf("invalid ADPCM params: ch=%d blockAlign=%d", channels, blockAlign)
	}

	headerSize := channels * 4
	dataPerBlock := blockAlign - headerSize
	samplesPerBlock := dataPerBlock / channels / 4 * 8 // 8 samples per 4-byte group

	numBlocks := len(dataChunk) / blockAlign
	pcmSamples := make([]int16, 0, numBlocks*samplesPerBlock*channels)

	predictors := make([]int32, channels)
	stepIdxs := make([]int, channels)
	chBuf := make([][]int16, channels)

	decodeNibble := func(ch int, nibble byte) int16 {
		step := int32(imaStepTable[stepIdxs[ch]])
		diff := step >> 3
		if nibble&4 != 0 {
			diff += step
		}
		if nibble&2 != 0 {
			diff += step >> 1
		}
		if nibble&1 != 0 {
			diff += step >> 2
		}
		if nibble&8 != 0 {
			predictors[ch] -= diff
		} else {
			predictors[ch] += diff
		}
		predictors[ch] = int32(clampSample(predictors[ch]))
		stepIdxs[ch] = clampIndex(stepIdxs[ch] + imaIndexTable[nibble])
		return int16(predictors[ch])
	}

	for off := 0; off+blockAlign <= len(dataChunk); off += blockAlign {
		block := dataChunk[off : off+blockAlign]

		// Read per-channel headers
		for ch := range channels {
			hdr := block[ch*4 : ch*4+4]
			predictors[ch] = int32(int16(binary.LittleEndian.Uint16(hdr[0:2])))
			stepIdxs[ch] = clampIndex(int(hdr[2]))
			if chBuf[ch] == nil {
				chBuf[ch] = make([]int16, 0, samplesPerBlock)
			} else {
				chBuf[ch] = chBuf[ch][:0]
			}
		}

		// Decode interleaved data: groups of (channels × 4 bytes)
		dataStart := headerSize
		numGroups := dataPerBlock / (channels * 4)
		for g := range numGroups {
			for ch := range channels {
				groupOff := dataStart + (g*channels+ch)*4
				for i := range 4 {
					b := block[groupOff+i]
					chBuf[ch] = append(chBuf[ch], decodeNibble(ch, b&0x0F))
					chBuf[ch] = append(chBuf[ch], decodeNibble(ch, (b>>4)&0x0F))
				}
			}
		}

		// Interleave into output
		n := len(chBuf[0])
		for i := range n {
			for ch := range channels {
				pcmSamples = append(pcmSamples, chBuf[ch][i])
			}
		}
	}

	// Build PCM16 WAV
	pcmDataSize := len(pcmSamples) * 2
	wavSize := 44 + pcmDataSize
	out := make([]byte, wavSize)

	copy(out[0:4], "RIFF")
	binary.LittleEndian.PutUint32(out[4:8], uint32(wavSize-8))
	copy(out[8:12], "WAVE")
	copy(out[12:16], "fmt ")
	binary.LittleEndian.PutUint32(out[16:20], 16)
	binary.LittleEndian.PutUint16(out[20:22], 1) // PCM
	binary.LittleEndian.PutUint16(out[22:24], uint16(channels))
	binary.LittleEndian.PutUint32(out[24:28], sampleRate)
	binary.LittleEndian.PutUint32(out[28:32], sampleRate*uint32(channels)*2)
	binary.LittleEndian.PutUint16(out[32:34], uint16(channels*2))
	binary.LittleEndian.PutUint16(out[34:36], 16)
	copy(out[36:40], "data")
	binary.LittleEndian.PutUint32(out[40:44], uint32(pcmDataSize))

	for i, s := range pcmSamples {
		binary.LittleEndian.PutUint16(out[44+i*2:44+i*2+2], uint16(s))
	}

	return out, nil
}

func copyAudio(srcDir, dstDir string) int {
	audioDir := filepath.Join(srcDir, "Audio")
	if _, err := os.Stat(audioDir); err != nil {
		return 0
	}

	count := 0
	filepath.Walk(audioDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if strings.ToLower(filepath.Ext(path)) != ".wav" {
			return nil
		}

		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return nil
		}
		dst := filepath.Join(dstDir, rel)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}

		// Convert Xbox ADPCM to PCM16 for browser compatibility
		pcm, err := decodeXboxADPCM(data)
		if err != nil {
			// Not ADPCM or decode failed — copy as-is
			pcm = data
		}

		if err := os.WriteFile(dst, pcm, 0o644); err != nil {
			return nil
		}
		count++
		return nil
	})
	return count
}

// --- Font export ---

func exportFonts(srcDir, dstDir string) int {
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return 0
	}

	count := 0
	for _, e := range entries {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".xtf") {
			continue
		}

		f, err := font.Open(filepath.Join(srcDir, e.Name()))
		if err != nil {
			fmt.Fprintf(os.Stderr, "    warning: font %s: %v\n", e.Name(), err)
			continue
		}

		if err := os.MkdirAll(dstDir, 0o755); err != nil {
			return count
		}

		baseName := sanitizeFilename(strings.TrimSuffix(e.Name(), filepath.Ext(e.Name())))
		jsonPath := filepath.Join(dstDir, baseName+".json")

		out, err := os.Create(jsonPath)
		if err != nil {
			continue
		}
		if err := font.ExportJSON(out, f, 0x024F); err != nil {
			out.Close()
			continue
		}
		out.Close()
		count++
	}
	return count
}

// --- Helpers ---

func sanitizeFilename(s string) string {
	s = strings.ReplaceAll(s, "/", "_")
	s = strings.ReplaceAll(s, "\\", "_")
	s = strings.ReplaceAll(s, " ", "_")
	s = strings.ReplaceAll(s, "~", "_")
	return s
}
