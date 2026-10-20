package font

import (
	"encoding/json"
	"io"
)

// ExportJSON writes the font's glyph data as JSON for web rendering.
// Only glyphs with codepoints <= maxCodepoint are included to keep the
// output size manageable (ASCII + Latin-1 is sufficient for dashboard text).
func ExportJSON(w io.Writer, f *Font, maxCodepoint rune) error {
	if maxCodepoint <= 0 {
		maxCodepoint = 0x024F // Latin Extended-B covers most Western text
	}

	type jsonGlyph struct {
		Advance float32   `json:"adv"`
		BB      []float32 `json:"bb,omitempty"`
		Origin  []float32 `json:"org,omitempty"`
		Indices []uint16  `json:"idx,omitempty"`
		Verts   []float32 `json:"verts,omitempty"`
	}

	type jsonFont struct {
		Name   string               `json:"name"`
		Height int                  `json:"height"`
		Glyphs map[string]jsonGlyph `json:"glyphs"`
	}

	out := jsonFont{
		Name:   f.Name,
		Height: f.Height,
		Glyphs: make(map[string]jsonGlyph),
	}

	glyphIdx := 0
	for _, r := range f.Ranges {
		for j := range int(r.Count) {
			cp := rune(r.Start) + rune(j)
			if glyphIdx >= len(f.Glyphs) {
				break
			}
			g := f.Glyphs[glyphIdx]
			glyphIdx++

			if cp > maxCodepoint {
				continue
			}

			jg := jsonGlyph{
				Advance: g.Metrics.AdvanceX,
			}

			if len(g.Mesh.Indices) > 0 {
				jg.BB = []float32{g.Metrics.BlackBoxX, g.Metrics.BlackBoxY}
				jg.Origin = []float32{g.Metrics.OriginX, g.Metrics.OriginY}
				jg.Indices = g.Mesh.Indices

				verts := make([]float32, len(g.Mesh.Vertices)*2)
				for k, v := range g.Mesh.Vertices {
					verts[k*2] = v[0]
					verts[k*2+1] = v[1]
				}
				jg.Verts = verts
			}

			key := string(cp)
			out.Glyphs[key] = jg
		}
	}

	enc := json.NewEncoder(w)
	return enc.Encode(out)
}
