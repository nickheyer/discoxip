package xap

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"unicode"
)

type parser struct {
	tokens   []token
	pos      int
	input    string // raw input for verbatim script extraction
	warnings []string
	defs     map[string]*Node
}

func (p *parser) warn(msg string) {
	p.warnings = append(p.warnings, msg)
}

// Parse parses XAP text into a Scene.
func Parse(input string) (*Scene, error) {
	tokens := lex(input)
	p := &parser{tokens: tokens, input: input, defs: make(map[string]*Node)}
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
	scene := &Scene{Defs: make(map[string]*Node)}
	for p.peek().typ != tokEOF {
		t := p.peek()

		// Top-level script keywords
		if t.typ == tokIdent {
			switch t.val {
			case "function":
				script := p.captureScript()
				scene.Items = append(scene.Items, SceneItem{Kind: SScript, Script: script})
				continue
			case "var":
				script := p.captureVarDecl()
				scene.Items = append(scene.Items, SceneItem{Kind: SScript, Script: script})
				continue
			}
		}

		node, err := p.parseTopNode()
		if err != nil {
			scene.Warnings = p.warnings
			scene.Defs = p.defs
			return scene, err
		}
		if node != nil {
			scene.Items = append(scene.Items, SceneItem{Kind: SNode, Node: node})
		}
	}
	scene.Warnings = p.warnings
	scene.Defs = p.defs
	return scene, nil
}

func (p *parser) parseTopNode() (*Node, error) {
	t := p.peek()

	switch t.val {
	case "DEF":
		return p.parseDEF()
	case "USE":
		return p.parseUSE()
	default:
		if t.typ == tokIdent {
			return p.parseNodeStart("")
		}
		// Non-ident stray token — consume and warn
		tok := p.next()
		p.warn(fmt.Sprintf("skipping stray token %q at pos %d", tok.val, tok.pos))
		return nil, nil
	}
}

func (p *parser) parseDEF() (*Node, error) {
	p.next() // consume DEF
	name, err := p.expect(tokIdent)
	if err != nil {
		return nil, err
	}

	node, err := p.parseNodeStart(name.val)
	if err != nil {
		return nil, err
	}
	if node != nil {
		p.defs[name.val] = node
	}
	return node, nil
}

func (p *parser) parseUSE() (*Node, error) {
	p.next() // consume USE
	name := p.next()
	if name.typ != tokIdent {
		return nil, fmt.Errorf("%w: expected identifier after USE, got %q", ErrUnexpectedToken, name.val)
	}
	if defNode, ok := p.defs[name.val]; ok {
		return defNode, nil
	}
	p.warn(fmt.Sprintf("USE %q: no matching DEF found", name.val))
	// Return a placeholder node so it's not lost
	return &Node{TypeName: "USE", DefName: name.val}, nil
}

// parseNodeStart parses a node starting from its type name identifier.
// defName is set if this was preceded by DEF.
func (p *parser) parseNodeStart(defName string) (*Node, error) {
	typeTok := p.next() // consume type name
	if typeTok.typ != tokIdent {
		return nil, nil
	}

	node := &Node{TypeName: typeTok.val, DefName: defName}

	// No brace body → bare node reference (e.g. "StarField" in children array)
	if p.peek().typ != tokLBrace {
		return node, nil
	}

	p.next() // consume {
	if err := p.parseNodeBody(node); err != nil {
		return node, err
	}
	return node, nil
}

// parseNodeBody parses the interior of a node between { and }.
// This is the universal decision-tree parser.
func (p *parser) parseNodeBody(node *Node) error {
	for p.peek().typ != tokRBrace && p.peek().typ != tokEOF {
		t := p.peek()

		if t.typ != tokIdent {
			// Skip stray non-ident tokens (numbers, brackets, parens, etc.)
			p.next()
			continue
		}

		switch t.val {
		case "function":
			script := p.captureScript()
			node.Scripts = append(node.Scripts, script)

		case "behavior":
			script := p.captureScript()
			node.Scripts = append(node.Scripts, script)

		case "var":
			script := p.captureVarDecl()
			node.Scripts = append(node.Scripts, script)

		case "DEF":
			child, err := p.parseDEF()
			if err != nil {
				return err
			}
			if child != nil {
				node.Children = append(node.Children, child)
			}

		case "USE":
			child, err := p.parseUSE()
			if err != nil {
				return err
			}
			if child != nil {
				node.Children = append(node.Children, child)
			}

		case "children":
			p.next() // consume "children"
			children, err := p.parseChildrenArray()
			if err != nil {
				return err
			}
			node.Children = append(node.Children, children...)

		default:
			// Decide: field (lowercase start) vs child node (uppercase start)
			if isUpperStart(t.val) {
				// Could be a bare child node: TypeName { ... }
				child, err := p.parseNodeStart("")
				if err != nil {
					return err
				}
				if child != nil {
					node.Children = append(node.Children, child)
				}
			} else {
				// Lowercase → field key
				p.next() // consume field key
				field, err := p.parseFieldValues(t.val, node)
				if err != nil {
					return err
				}
				node.Fields = append(node.Fields, field)
			}
		}
	}

	if p.peek().typ == tokRBrace {
		p.next() // consume }
	}
	return nil
}

// parseFieldValues parses the value(s) after a field key.
func (p *parser) parseFieldValues(key string, parentNode *Node) (Field, error) {
	field := Field{Key: key}

	t := p.peek()

	switch {
	case t.typ == tokString:
		p.next()
		field.Values = append(field.Values, Value{Kind: VString, Str: t.val})

	case t.typ == tokNumber:
		// Collect consecutive numbers (for vectors: translation 1 2 3)
		for p.peek().typ == tokNumber {
			numTok := p.next()
			v, err := strconv.ParseFloat(numTok.val, 64)
			if err != nil {
				p.warn(fmt.Sprintf("bad number %q at pos %d", numTok.val, numTok.pos))
				continue
			}
			field.Values = append(field.Values, Value{Kind: VNumber, Num: v})
		}

	case t.typ == tokLBracket:
		// Array value
		arr, err := p.parseArrayValue()
		if err != nil {
			return field, err
		}
		field.Values = append(field.Values, Value{Kind: VArray, Array: arr})

	case t.typ == tokLBrace:
		// Block-valued field (e.g. behavior { ... })
		script := p.captureBlockVerbatim()
		field.Values = append(field.Values, Value{Kind: VScript, Str: script})

	case t.typ == tokIdent:
		switch t.val {
		case "true", "TRUE":
			p.next()
			field.Values = append(field.Values, Value{Kind: VBool, Bool: true})
		case "false", "FALSE":
			p.next()
			field.Values = append(field.Values, Value{Kind: VBool, Bool: false})
		case "DEF":
			// Node-valued field: DEF name Type { ... }
			child, err := p.parseDEF()
			if err != nil {
				return field, err
			}
			if child != nil {
				field.Values = append(field.Values, Value{Kind: VNode, Node: child})
			}
		case "USE":
			p.next() // consume USE
			name := p.next()
			if defNode, ok := p.defs[name.val]; ok {
				field.Values = append(field.Values, Value{Kind: VNode, Node: defNode})
			} else {
				p.warn(fmt.Sprintf("USE %q: no matching DEF found", name.val))
				field.Values = append(field.Values, Value{Kind: VIdent, Str: name.val})
			}
		default:
			// Could be a node-valued field (e.g. material MaxMaterial { ... })
			// or a bare identifier value
			if isUpperStart(t.val) && p.posAhead(1).typ == tokLBrace {
				// Node-valued field
				child, err := p.parseNodeStart("")
				if err != nil {
					return field, err
				}
				if child != nil {
					field.Values = append(field.Values, Value{Kind: VNode, Node: child})
				}
			} else if isUpperStart(t.val) && p.posAhead(1).typ == tokIdent && p.posAhead(2).typ == tokLBrace {
				// Implicit DEF in field context: name Type { ... }
				// e.g. "material MUmat MaxMaterial { ... }" — but that's unusual.
				// More likely: "geometry Mesh { ... }" where Mesh is just the type.
				child, err := p.parseNodeStart("")
				if err != nil {
					return field, err
				}
				if child != nil {
					field.Values = append(field.Values, Value{Kind: VNode, Node: child})
				}
			} else {
				// Bare identifier value
				p.next()
				field.Values = append(field.Values, Value{Kind: VIdent, Str: t.val})
			}
		}

	default:
		// Nothing recognizable — empty field
	}

	return field, nil
}

// parseArrayValue parses [...] returning a slice of Values.
func (p *parser) parseArrayValue() ([]Value, error) {
	p.next() // consume [
	var arr []Value
	for p.peek().typ != tokRBracket && p.peek().typ != tokEOF {
		t := p.peek()
		switch {
		case t.typ == tokNumber:
			numTok := p.next()
			v, _ := strconv.ParseFloat(numTok.val, 64)
			arr = append(arr, Value{Kind: VNumber, Num: v})
		case t.typ == tokString:
			p.next()
			arr = append(arr, Value{Kind: VString, Str: t.val})
		case t.typ == tokIdent:
			if t.val == "DEF" {
				child, err := p.parseDEF()
				if err != nil {
					return arr, err
				}
				if child != nil {
					arr = append(arr, Value{Kind: VNode, Node: child})
				}
			} else if t.val == "USE" {
				child, err := p.parseUSE()
				if err != nil {
					return arr, err
				}
				if child != nil {
					arr = append(arr, Value{Kind: VNode, Node: child})
				}
			} else if t.val == "true" || t.val == "TRUE" {
				p.next()
				arr = append(arr, Value{Kind: VBool, Bool: true})
			} else if t.val == "false" || t.val == "FALSE" {
				p.next()
				arr = append(arr, Value{Kind: VBool, Bool: false})
			} else {
				arr = append(arr, Value{Kind: VIdent, Str: t.val})
				p.next()
			}
		default:
			p.next() // skip unknown
		}
	}
	if p.peek().typ == tokRBracket {
		p.next() // consume ]
	}
	return arr, nil
}

// parseChildrenArray parses children [ ... ] with node entries.
func (p *parser) parseChildrenArray() ([]*Node, error) {
	if _, err := p.expect(tokLBracket); err != nil {
		return nil, err
	}

	var children []*Node
	for p.peek().typ != tokRBracket && p.peek().typ != tokEOF {
		t := p.peek()
		switch {
		case t.typ == tokIdent && t.val == "DEF":
			child, err := p.parseDEF()
			if err != nil {
				return children, err
			}
			if child != nil {
				children = append(children, child)
			}

		case t.typ == tokIdent && t.val == "USE":
			child, err := p.parseUSE()
			if err != nil {
				return children, err
			}
			if child != nil {
				children = append(children, child)
			}

		case t.typ == tokIdent && isUpperStart(t.val):
			// Check for implicit DEF: name Type { ... }
			// where name is lowercase and Type is uppercase
			// Actually in children arrays: could be "MUheader Transform { ... }"
			// where MUheader is the implicit def name
			if p.posAhead(1).typ == tokIdent && isUpperStart(p.posAhead(1).val) &&
				(p.posAhead(2).typ == tokLBrace || p.posAhead(2).typ != tokIdent) {
				// name Type { } pattern → implicit DEF
				nameTok := p.next() // consume name
				child, err := p.parseNodeStart(nameTok.val)
				if err != nil {
					return children, err
				}
				if child != nil {
					p.defs[nameTok.val] = child
					children = append(children, child)
				}
			} else {
				// Regular node: Type { ... } or bare Type
				child, err := p.parseNodeStart("")
				if err != nil {
					return children, err
				}
				if child != nil {
					children = append(children, child)
				}
			}

		case t.typ == tokIdent:
			// Lowercase ident in children array — could be implicit DEF name
			// e.g. "myNode Transform { ... }"
			if p.posAhead(1).typ == tokIdent && isUpperStart(p.posAhead(1).val) {
				nameTok := p.next() // consume name
				child, err := p.parseNodeStart(nameTok.val)
				if err != nil {
					return children, err
				}
				if child != nil {
					p.defs[nameTok.val] = child
					children = append(children, child)
				}
			} else {
				// Bare lowercase ident — skip
				p.next()
			}

		default:
			p.next() // skip non-ident tokens
		}
	}

	if p.peek().typ == tokRBracket {
		p.next() // consume ]
	}
	return children, nil
}

// captureScript captures a verbatim script block starting with "function" or "behavior".
// Uses raw input scanning for perfect fidelity.
func (p *parser) captureScript() string {
	startTok := p.peek()
	startPos := startTok.pos

	// Consume the keyword
	p.next()

	// For "function", skip ahead to the opening brace (past name, params, etc.)
	// For "behavior", skip to opening brace
	for p.peek().typ != tokLBrace && p.peek().typ != tokEOF {
		p.next()
	}

	if p.peek().typ != tokLBrace {
		// No body found
		return p.input[startPos:p.tokens[p.pos].pos]
	}

	// Capture the body using brace-depth scanning on raw input
	bracePos := p.peek().pos
	endPos := p.scanMatchingBrace(bracePos)

	// Skip all tokens that fall within this range
	for p.peek().typ != tokEOF && p.peek().pos <= endPos {
		p.next()
	}

	return strings.TrimRightFunc(p.input[startPos:endPos+1], unicode.IsSpace)
}

// captureVarDecl captures a var declaration up to the semicolon or end of statement.
func (p *parser) captureVarDecl() string {
	startTok := p.peek()
	startPos := startTok.pos

	// Scan raw input from startPos to the next semicolon or newline
	endPos := startPos
	for endPos < len(p.input) {
		ch := p.input[endPos]
		if ch == ';' {
			endPos++ // include the semicolon
			break
		}
		if ch == '\n' {
			break
		}
		endPos++
	}

	// Advance tokens past this range
	for p.peek().typ != tokEOF && p.peek().pos < endPos {
		p.next()
	}

	return strings.TrimSpace(p.input[startPos:endPos])
}

// captureBlockVerbatim captures a { ... } block as raw text.
func (p *parser) captureBlockVerbatim() string {
	if p.peek().typ != tokLBrace {
		return ""
	}

	bracePos := p.peek().pos
	endPos := p.scanMatchingBrace(bracePos)

	// Skip all tokens within this range
	for p.peek().typ != tokEOF && p.peek().pos <= endPos {
		p.next()
	}

	// Return content between braces (exclusive)
	if bracePos+1 < endPos {
		return strings.TrimSpace(p.input[bracePos+1 : endPos])
	}
	return ""
}

// scanMatchingBrace scans raw input from an opening brace to its matching close.
// Handles nested braces, string literals, and comments.
// Returns the position of the closing brace.
func (p *parser) scanMatchingBrace(openPos int) int {
	pos := openPos + 1 // skip opening {
	depth := 1

	for pos < len(p.input) && depth > 0 {
		ch := p.input[pos]
		switch ch {
		case '{':
			depth++
			pos++
		case '}':
			depth--
			if depth == 0 {
				return pos
			}
			pos++
		case '"':
			// Skip string literal
			pos++
			for pos < len(p.input) && p.input[pos] != '"' {
				pos++
			}
			if pos < len(p.input) {
				pos++ // skip closing "
			}
		case '/':
			if pos+1 < len(p.input) {
				if p.input[pos+1] == '/' {
					// Line comment
					for pos < len(p.input) && p.input[pos] != '\n' {
						pos++
					}
				} else if p.input[pos+1] == '*' {
					// Block comment
					pos += 2
					for pos+1 < len(p.input) {
						if p.input[pos] == '*' && p.input[pos+1] == '/' {
							pos += 2
							break
						}
						pos++
					}
				} else {
					pos++
				}
			} else {
				pos++
			}
		case '#':
			// VRML line comment
			for pos < len(p.input) && p.input[pos] != '\n' {
				pos++
			}
		default:
			pos++
		}
	}

	// If we ran out of input, return end
	return pos
}

// posAhead peeks n tokens ahead without consuming.
func (p *parser) posAhead(n int) token {
	idx := p.pos + n
	if idx >= len(p.tokens) {
		return token{typ: tokEOF}
	}
	return p.tokens[idx]
}

// isUpperStart returns true if the string starts with an uppercase letter.
func isUpperStart(s string) bool {
	if len(s) == 0 {
		return false
	}
	return s[0] >= 'A' && s[0] <= 'Z'
}
