package web

import (
	"embed"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/nickheyer/discoxip/pkg/font"
	"github.com/nickheyer/discoxip/pkg/scene"
	"github.com/nickheyer/discoxip/pkg/xap"
	"github.com/nickheyer/discoxip/pkg/xbe"
)

//go:embed static/index.html static/runtime.js static/materials.js
var staticFiles embed.FS

// Export generates a self-contained web application from extracted XIP data.
func Export(inputDir, outputDir string) error {
	// Create output directories
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

	// Find all XAP files
	xapFiles, err := filepath.Glob(filepath.Join(inputDir, "*.xap"))
	if err != nil {
		return err
	}
	if len(xapFiles) == 0 {
		return fmt.Errorf("web: no .xap files found in %s", inputDir)
	}

	fmt.Fprintf(os.Stderr, "Found %d XAP scene(s)\n", len(xapFiles))

	// Build config: archive mappings, inline mappings
	config := buildConfig(xapFiles)

	// Global mesh manifest
	meshManifest := make(map[string]meshEntry)

	// Process each XAP
	for _, xapPath := range xapFiles {
		baseName := strings.TrimSuffix(filepath.Base(xapPath), ".xap")
		fmt.Fprintf(os.Stderr, "  Processing %s\n", baseName)

		// Parse XAP scene graph
		ast, err := xap.ParseFile(xapPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "    warning: parse error: %v\n", err)
			continue
		}

		// Serialize scene to JSON
		sceneJSON := serializeScene(ast)
		jsonData, err := json.MarshalIndent(sceneJSON, "", "  ")
		if err != nil {
			return fmt.Errorf("web: marshaling scene %s: %w", baseName, err)
		}
		jsonPath := filepath.Join(outputDir, "data", "scenes", baseName+".json")
		if err := os.WriteFile(jsonPath, jsonData, 0o644); err != nil {
			return err
		}

		// Resolve and export meshes
		s, err := scene.Load(xapPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "    warning: scene load error: %v\n", err)
			continue
		}

		archiveName := extractArchiveName(baseName)
		for url, md := range s.Meshes {
			if len(md.Vertices) == 0 {
				continue
			}
			key := archiveName + ":" + url
			if _, exists := meshManifest[key]; exists {
				continue
			}

			binName := sanitizeFilename(archiveName+"_"+strings.TrimSuffix(url, filepath.Ext(url))) + ".bin"
			binPath := filepath.Join(outputDir, "data", "meshes", binName)
			hasColors := exportMeshBinary(binPath, md)

			meshManifest[key] = meshEntry{
				File:        binName,
				VertexCount: len(md.Vertices),
				IndexCount:  len(md.Indices),
				HasColors:   hasColors,
			}
		}
	}

	// Write mesh manifest
	manifestData, err := json.MarshalIndent(meshManifest, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(outputDir, "data", "meshes", "manifest.json"), manifestData, 0o644); err != nil {
		return err
	}

	// Copy textures
	texCount := copyTextures(inputDir, filepath.Join(outputDir, "data", "textures"))
	fmt.Fprintf(os.Stderr, "  Copied %d textures\n", texCount)

	// Extract materials from XBE if present
	xbeFiles, _ := filepath.Glob(filepath.Join(inputDir, "*.xbe"))
	if len(xbeFiles) == 0 {
		// Check parent directory (XBE is alongside XIP files, not in extracted output)
		xbeFiles, _ = filepath.Glob(filepath.Join(filepath.Dir(inputDir), "*.xbe"))
	}
	if len(xbeFiles) > 0 {
		materials, err := xbe.ExtractMaterialsFromXBE(xbeFiles[0])
		if err != nil {
			fmt.Fprintf(os.Stderr, "  warning: XBE material extraction: %v\n", err)
		} else {
			matData, _ := json.MarshalIndent(materials, "", "  ")
			matPath := filepath.Join(outputDir, "data", "materials.json")
			os.WriteFile(matPath, matData, 0o644)
			fmt.Fprintf(os.Stderr, "  Extracted %d materials from %s\n", len(materials), filepath.Base(xbeFiles[0]))
		}
	}

	// Copy audio files
	audioCount := copyAudio(inputDir, filepath.Join(outputDir, "data"))
	if audioCount > 0 {
		fmt.Fprintf(os.Stderr, "  Copied %d audio files\n", audioCount)
	}

	// Export fonts
	fontCount := exportFonts(inputDir, filepath.Join(outputDir, "data", "fonts"))
	if fontCount > 0 {
		fmt.Fprintf(os.Stderr, "  Exported %d fonts\n", fontCount)
	}

	// Write config
	configData, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(outputDir, "data", "config.json"), configData, 0o644); err != nil {
		return err
	}

	// Write static files
	for _, name := range []string{"static/index.html", "static/runtime.js", "static/materials.js"} {
		data, err := staticFiles.ReadFile(name)
		if err != nil {
			return fmt.Errorf("web: reading embedded %s: %w", name, err)
		}
		outName := filepath.Base(name)
		if err := os.WriteFile(filepath.Join(outputDir, outName), data, 0o644); err != nil {
			return err
		}
	}

	fmt.Fprintf(os.Stderr, "Web app exported to %s\n", outputDir)
	fmt.Fprintf(os.Stderr, "  Scenes: %d, Meshes: %d, Textures: %d\n",
		len(xapFiles), len(meshManifest), texCount)
	fmt.Fprintf(os.Stderr, "  Run: cd %s && python3 -m http.server\n", outputDir)
	return nil
}

// --- Config ---

type exportConfig struct {
	Entry    string            `json:"entry"`
	Archives map[string]string `json:"archives"`
	Inlines  map[string]string `json:"inlines"`
	Scenes   []string          `json:"scenes"`
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
	File        string `json:"file"`
	VertexCount int    `json:"vertexCount"`
	IndexCount  int    `json:"indexCount"`
	HasColors   bool   `json:"hasColors"`
}

func exportMeshBinary(path string, md *scene.MeshData) bool {
	f, err := os.Create(path)
	if err != nil {
		return false
	}
	defer f.Close()

	hasColors := false
	for _, v := range md.Vertices {
		if v.HasColor {
			hasColors = true
			break
		}
	}

	buf := make([]byte, 4)

	// Positions
	for _, v := range md.Vertices {
		for _, c := range v.Pos {
			binary.LittleEndian.PutUint32(buf, math.Float32bits(c))
			f.Write(buf)
		}
	}

	// Normals
	for _, v := range md.Vertices {
		for _, c := range v.Normal {
			binary.LittleEndian.PutUint32(buf, math.Float32bits(c))
			f.Write(buf)
		}
	}

	// UVs
	for _, v := range md.Vertices {
		for _, c := range v.UV {
			binary.LittleEndian.PutUint32(buf, math.Float32bits(c))
			f.Write(buf)
		}
	}

	// Colors (if present)
	if hasColors {
		for _, v := range md.Vertices {
			for _, c := range v.Color {
				binary.LittleEndian.PutUint32(buf, math.Float32bits(c))
				f.Write(buf)
			}
		}
	}

	// Indices
	buf2 := make([]byte, 2)
	for _, idx := range md.Indices {
		binary.LittleEndian.PutUint16(buf2, idx)
		f.Write(buf2)
	}

	return hasColors
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
//   [ch0 header 4B][ch1 header 4B][interleaved nibble data 64B]
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
