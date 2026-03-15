package xap

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

type parser struct {
	tokens   []token
	pos      int
	warnings []string
}

func (p *parser) warn(msg string) {
	p.warnings = append(p.warnings, msg)
}

// Parse parses XAP text into a Scene.
func Parse(input string) (*Scene, error) {
	tokens := lex(input)
	p := &parser{tokens: tokens}
	return p.parseScene()
}

// ParseFile reads and parses an XAP file.
func ParseFile(path string) (*Scene, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(string(data))
}

// ReadScene parses XAP from a reader.
func ReadScene(r io.Reader) (*Scene, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	return Parse(string(data))
}

func (p *parser) peek() token {
	if p.pos >= len(p.tokens) {
		return token{typ: tokEOF}
	}
	return p.tokens[p.pos]
}

func (p *parser) next() token {
	t := p.peek()
	if t.typ != tokEOF {
		p.pos++
	}
	return t
}

func (p *parser) expect(typ tokenType) (token, error) {
	t := p.next()
	if t.typ != typ {
		return t, fmt.Errorf("%w: expected %d, got %q at pos %d", ErrUnexpectedToken, typ, t.val, t.pos)
	}
	return t, nil
}

func (p *parser) parseScene() (*Scene, error) {
	scene := &Scene{}
	for p.peek().typ != tokEOF {
		node, err := p.parseNode()
		if err != nil {
			scene.Warnings = p.warnings
			return scene, err
		}
		if node != nil {
			scene.Nodes = append(scene.Nodes, node)
		}
	}
	scene.Warnings = p.warnings
	return scene, nil
}

func (p *parser) parseNode() (Node, error) {
	t := p.peek()

	switch t.val {
	case "DEF":
		return p.parseDEF()
	case "Transform":
		return p.parseTransform("")
	case "Shape":
		return p.parseShape()
	default:
		// Unknown top-level token — skip it but record a warning
		tok := p.next()
		if tok.typ == tokIdent {
			p.warn(fmt.Sprintf("skipping unknown node type %q at pos %d", tok.val, tok.pos))
			if p.peek().typ == tokLBrace {
				p.skipBlock()
			}
		}
		return nil, nil
	}
}

func (p *parser) parseDEF() (Node, error) {
	p.next() // consume DEF
	name, err := p.expect(tokIdent)
	if err != nil {
		return nil, err
	}
	typeTok := p.peek()
	switch typeTok.val {
	case "Transform":
		return p.parseTransform(name.val)
	case "Mesh":
		return p.parseMeshRef(name.val)
	default:
		// DEF name SomeType — try to parse as a generic block
		p.next() // consume type name
		if p.peek().typ == tokLBrace {
			p.skipBlock()
		}
		return nil, nil
	}
}

func (p *parser) parseTransform(name string) (*Transform, error) {
	p.next() // consume "Transform"
	// Skip redundant idents (e.g. "Transform Transform {")
	for p.peek().typ == tokIdent {
		p.next()
	}
	if _, err := p.expect(tokLBrace); err != nil {
		return nil, err
	}

	tf := &Transform{Name: name, Fields: make(map[string]interface{})}

	for p.peek().typ != tokRBrace && p.peek().typ != tokEOF {
		field := p.peek()
		if field.typ != tokIdent {
			// Try to skip unexpected tokens
			p.next()
			continue
		}

		switch field.val {
		case "children":
			p.next() // consume "children"
			children, err := p.parseChildren()
			if err != nil {
				return tf, err
			}
			tf.Children = children
		case "rotation":
			p.next()
			vals, err := p.parseFloats(4)
			if err != nil {
				return tf, err
			}
			copy(tf.Rotation[:], vals)
			tf.HasRotation = true
		case "scale":
			p.next()
			vals, err := p.parseFloats(3)
			if err != nil {
				return tf, err
			}
			copy(tf.Scale[:], vals)
			tf.HasScale = true
		case "scaleOrientation":
			p.next()
			vals, err := p.parseFloats(4)
			if err != nil {
				return tf, err
			}
			copy(tf.ScaleOrientation[:], vals)
			tf.HasScaleOri = true
		case "translation":
			p.next()
			vals, err := p.parseFloats(3)
			if err != nil {
				return tf, err
			}
			copy(tf.Translation[:], vals)
			tf.HasTranslation = true
		case "fade":
			p.next()
			vals, err := p.parseFloats(1)
			if err != nil {
				return tf, err
			}
			tf.Fade = vals[0]
			tf.HasFade = true
		case "DEF":
			child, err := p.parseDEF()
			if err != nil {
				return tf, err
			}
			if child != nil {
				tf.Children = append(tf.Children, child)
			}
		case "Shape":
			child, err := p.parseShape()
			if err != nil {
				return tf, err
			}
			tf.Children = append(tf.Children, child)
		case "Transform":
			child, err := p.parseTransform("")
			if err != nil {
				return tf, err
			}
			tf.Children = append(tf.Children, child)
		default:
			// Unknown field — skip value(s)
			p.next()
			p.skipFieldValue()
		}
	}

	p.next() // consume }
	return tf, nil
}

func (p *parser) parseChildren() ([]Node, error) {
	if _, err := p.expect(tokLBracket); err != nil {
		return nil, err
	}

	var children []Node
	for p.peek().typ != tokRBracket && p.peek().typ != tokEOF {
		t := p.peek()
		switch t.val {
		case "DEF":
			child, err := p.parseDEF()
			if err != nil {
				return children, err
			}
			if child != nil {
				children = append(children, child)
			}
		case "Transform":
			child, err := p.parseTransform("")
			if err != nil {
				return children, err
			}
			children = append(children, child)
		case "Shape":
			child, err := p.parseShape()
			if err != nil {
				return children, err
			}
			children = append(children, child)
		default:
			p.next() // skip unknown
		}
	}

	p.next() // consume ]
	return children, nil
}

func (p *parser) parseShape() (*Shape, error) {
	p.next() // consume "Shape"
	if _, err := p.expect(tokLBrace); err != nil {
		return nil, err
	}

	shape := &Shape{}

	for p.peek().typ != tokRBrace && p.peek().typ != tokEOF {
		field := p.peek()
		switch field.val {
		case "appearance":
			p.next()
			app, err := p.parseAppearance()
			if err != nil {
				return shape, err
			}
			shape.Appearance = app
		case "geometry":
			p.next()
			switch p.peek().val {
			case "DEF":
				node, err := p.parseDEF()
				if err != nil {
					return shape, err
				}
				if ref, ok := node.(*MeshRef); ok {
					shape.Geometry = ref
				}
			case "USE":
				p.next() // consume USE
				name := p.next() // consume name
				// Reconstruct URL from DEF name (convention: name + ".xm")
				shape.Geometry = &MeshRef{Name: name.val, URL: name.val + ".xm"}
			case "Mesh":
				ref, err := p.parseMeshRef("")
				if err != nil {
					return shape, err
				}
				if ref != nil {
					shape.Geometry = ref.(*MeshRef)
				}
			default:
				// Unknown geometry type (e.g. Text) — skip it
				p.next() // consume type name
				if p.peek().typ == tokLBrace {
					p.skipBlock()
				}
			}
		default:
			p.next()
			p.skipFieldValue()
		}
	}

	p.next() // consume }
	return shape, nil
}

func (p *parser) parseAppearance() (*Appearance, error) {
	p.next() // consume "Appearance"
	if _, err := p.expect(tokLBrace); err != nil {
		return nil, err
	}

	app := &Appearance{}

	for p.peek().typ != tokRBrace && p.peek().typ != tokEOF {
		field := p.peek()
		if field.val == "material" {
			p.next()
			var defName string
			if p.peek().val == "DEF" {
				p.next() // consume DEF
				defName = p.next().val
			}
			mat, err := p.parseMaterial()
			if err != nil {
				return app, err
			}
			mat.DefName = defName
			app.Material = mat
		} else {
			p.next()
			p.skipFieldValue()
		}
	}

	p.next() // consume }
	return app, nil
}

func (p *parser) parseMaterial() (*Material, error) {
	typeName := p.next() // e.g. "MaxMaterial"
	if _, err := p.expect(tokLBrace); err != nil {
		return nil, err
	}

	mat := &Material{Type: typeName.val}

	for p.peek().typ != tokRBrace && p.peek().typ != tokEOF {
		field := p.peek()
		if field.val == "name" {
			p.next()
			if p.peek().typ == tokString {
				mat.Name = p.next().val
			}
		} else {
			p.next()
			p.skipFieldValue()
		}
	}

	p.next() // consume }
	return mat, nil
}

func (p *parser) parseMeshRef(name string) (Node, error) {
	p.next() // consume "Mesh"
	if _, err := p.expect(tokLBrace); err != nil {
		return nil, err
	}

	ref := &MeshRef{Name: name}

	for p.peek().typ != tokRBrace && p.peek().typ != tokEOF {
		if p.peek().val == "url" {
			p.next()
			if p.peek().typ == tokString {
				ref.URL = p.next().val
			}
		} else {
			p.next()
		}
	}

	p.next() // consume }
	return ref, nil
}

func (p *parser) parseFloats(n int) ([]float64, error) {
	vals := make([]float64, n)
	for i := range n {
		t := p.next()
		if t.typ != tokNumber {
			return nil, fmt.Errorf("%w: expected number, got %q", ErrUnexpectedToken, t.val)
		}
		v, err := strconv.ParseFloat(t.val, 64)
		if err != nil {
			return nil, fmt.Errorf("xap: parsing float %q: %w", t.val, err)
		}
		vals[i] = v
	}
	return vals, nil
}

func (p *parser) skipBlock() {
	p.next() // consume {
	depth := 1
	for depth > 0 && p.peek().typ != tokEOF {
		t := p.next()
		switch t.typ {
		case tokLBrace:
			depth++
		case tokRBrace:
			depth--
		}
	}
}

func (p *parser) skipFieldValue() {
	t := p.peek()
	switch t.typ {
	case tokLBrace:
		p.skipBlock()
	case tokLBracket:
		p.next()
		depth := 1
		for depth > 0 && p.peek().typ != tokEOF {
			t := p.next()
			switch t.typ {
			case tokLBracket:
				depth++
			case tokRBracket:
				depth--
			}
		}
	case tokString, tokNumber:
		p.next()
	case tokIdent:
		// Consume the identifier value; if followed by a block, skip that too
		p.next()
		if p.peek().typ == tokLBrace {
			p.skipBlock()
		}
	}
}

// NodeCount returns total node count in the scene.
func (s *Scene) NodeCount() int {
	count := 0
	for _, n := range s.Nodes {
		count += countNodes(n)
	}
	return count
}

func countNodes(n Node) int {
	c := 1
	if tf, ok := n.(*Transform); ok {
		for _, child := range tf.Children {
			c += countNodes(child)
		}
	}
	return c
}

// MeshRefs returns all mesh URL references found in the scene.
func (s *Scene) MeshRefs() []string {
	var refs []string
	for _, n := range s.Nodes {
		collectMeshRefs(n, &refs)
	}
	return refs
}

func collectMeshRefs(n Node, refs *[]string) {
	switch v := n.(type) {
	case *Transform:
		for _, child := range v.Children {
			collectMeshRefs(child, refs)
		}
	case *Shape:
		if v.Geometry != nil && v.Geometry.URL != "" {
			*refs = append(*refs, v.Geometry.URL)
		}
	}
}

// Materials returns all unique material names found in the scene.
func (s *Scene) Materials() []string {
	seen := map[string]bool{}
	var mats []string
	for _, n := range s.Nodes {
		collectMaterials(n, seen, &mats)
	}
	return mats
}

func collectMaterials(n Node, seen map[string]bool, mats *[]string) {
	switch v := n.(type) {
	case *Transform:
		for _, child := range v.Children {
			collectMaterials(child, seen, mats)
		}
	case *Shape:
		if v.Appearance != nil && v.Appearance.Material != nil {
			name := v.Appearance.Material.Name
			if name != "" && !seen[name] {
				seen[name] = true
				*mats = append(*mats, name)
			}
		}
	}
}

// String returns a summary string for the scene.
func (s *Scene) String() string {
	refs := s.MeshRefs()
	mats := s.Materials()
	parts := []string{
		fmt.Sprintf("%d nodes", s.NodeCount()),
		fmt.Sprintf("%d mesh refs", len(refs)),
		fmt.Sprintf("%d materials", len(mats)),
	}
	return strings.Join(parts, ", ")
}
