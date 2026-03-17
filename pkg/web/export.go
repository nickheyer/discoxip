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

// --- Audio copy ---

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

		// Preserve relative path: Audio/SubDir/file.wav → data/Audio/SubDir/file.wav
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
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			return nil
		}
		count++
		return nil
	})
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
