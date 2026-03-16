package xap

import "strconv"

// ValueKind discriminates property value types.
type ValueKind int

const (
	VNumber ValueKind = iota // float64
	VString                  // quoted string
	VBool                    // true/false/TRUE/FALSE
	VIdent                   // bare identifier (includes USE references)
	VNode                    // *Node (inline child node)
	VArray                   // []Value (bracket-delimited)
	VScript                  // verbatim script text
)

// Value is a typed property value.
type Value struct {
	Kind   ValueKind
	Num    float64
	Str    string
	Bool   bool
	Node   *Node
	Array  []Value
}

// Field is a named property with one or more values.
// Multi-value fields (e.g. rotation 0 1 0 3.14) store each component separately.
type Field struct {
	Key    string
	Values []Value
}

// Node is the universal scene graph element.
type Node struct {
	TypeName string   // "Transform", "Level", "AudioClip", etc.
	DefName  string   // from DEF or implicit naming
	Fields   []Field  // ordered properties
	Children []*Node  // child nodes (from children [...] or inline)
	Scripts  []string // verbatim script blocks (function/behavior/var)
}

// SceneItemKind discriminates top-level scene items.
type SceneItemKind int

const (
	SNode   SceneItemKind = iota
	SScript
)

// SceneItem is a top-level element (node or script).
type SceneItem struct {
	Kind   SceneItemKind
	Node   *Node
	Script string
}

// Scene is the top-level container.
type Scene struct {
	Items    []SceneItem
	Warnings []string
	Defs     map[string]*Node
}

// --- Accessor helpers on *Node ---

// GetField returns the first field with the given key, or nil.
func (n *Node) GetField(key string) *Field {
	for i := range n.Fields {
		if n.Fields[i].Key == key {
			return &n.Fields[i]
		}
	}
	return nil
}

// String returns the first string value of the named field.
func (n *Node) String(key string) string {
	f := n.GetField(key)
	if f == nil || len(f.Values) == 0 {
		return ""
	}
	v := f.Values[0]
	if v.Kind == VString {
		return v.Str
	}
	if v.Kind == VIdent {
		return v.Str
	}
	return ""
}

// Float returns the first numeric value of the named field.
func (n *Node) Float(key string) (float64, bool) {
	f := n.GetField(key)
	if f == nil || len(f.Values) == 0 {
		return 0, false
	}
	if f.Values[0].Kind == VNumber {
		return f.Values[0].Num, true
	}
	return 0, false
}

// Floats returns all numeric values of the named field as a slice.
func (n *Node) Floats(key string) []float64 {
	f := n.GetField(key)
	if f == nil {
		return nil
	}
	var out []float64
	for _, v := range f.Values {
		if v.Kind == VNumber {
			out = append(out, v.Num)
		}
	}
	return out
}

// Bool returns the first boolean value of the named field.
func (n *Node) Bool(key string) (bool, bool) {
	f := n.GetField(key)
	if f == nil || len(f.Values) == 0 {
		return false, false
	}
	if f.Values[0].Kind == VBool {
		return f.Values[0].Bool, true
	}
	return false, false
}

// ChildNode returns the first node-valued field with the given key.
func (n *Node) ChildNode(key string) *Node {
	f := n.GetField(key)
	if f == nil || len(f.Values) == 0 {
		return nil
	}
	if f.Values[0].Kind == VNode {
		return f.Values[0].Node
	}
	return nil
}

// ChildrenByType returns all child nodes matching the given type name.
func (n *Node) ChildrenByType(typeName string) []*Node {
	var out []*Node
	for _, c := range n.Children {
		if c.TypeName == typeName {
			out = append(out, c)
		}
	}
	return out
}

// --- Scene convenience methods ---

// Nodes returns all top-level nodes.
func (s *Scene) Nodes() []*Node {
	var out []*Node
	for _, item := range s.Items {
		if item.Kind == SNode && item.Node != nil {
			out = append(out, item.Node)
		}
	}
	return out
}

// NodeCount returns total node count in the scene.
func (s *Scene) NodeCount() int {
	count := 0
	for _, n := range s.Nodes() {
		count += countNodes(n)
	}
	return count
}

func countNodes(n *Node) int {
	c := 1
	for _, child := range n.Children {
		c += countNodes(child)
	}
	// Count node-valued fields
	for _, f := range n.Fields {
		for _, v := range f.Values {
			if v.Kind == VNode && v.Node != nil {
				c += countNodes(v.Node)
			}
			if v.Kind == VArray {
				for _, av := range v.Array {
					if av.Kind == VNode && av.Node != nil {
						c += countNodes(av.Node)
					}
				}
			}
		}
	}
	return c
}

// MeshRefs returns all mesh URL references found in the scene.
func (s *Scene) MeshRefs() []string {
	var refs []string
	for _, n := range s.Nodes() {
		collectMeshRefs(n, &refs)
	}
	return refs
}

func collectMeshRefs(n *Node, refs *[]string) {
	// If this node is a Mesh, grab its url
	if n.TypeName == "Mesh" {
		url := n.String("url")
		if url != "" {
			*refs = append(*refs, url)
		}
	}
	// Check node-valued fields (geometry, material, appearance, etc.)
	for _, f := range n.Fields {
		for _, v := range f.Values {
			if v.Kind == VNode && v.Node != nil {
				collectMeshRefs(v.Node, refs)
			}
			if v.Kind == VArray {
				for _, av := range v.Array {
					if av.Kind == VNode && av.Node != nil {
						collectMeshRefs(av.Node, refs)
					}
				}
			}
		}
	}
	for _, child := range n.Children {
		collectMeshRefs(child, refs)
	}
}

// Materials returns all unique material names found in the scene.
func (s *Scene) Materials() []string {
	seen := map[string]bool{}
	var mats []string
	for _, n := range s.Nodes() {
		collectMaterials(n, seen, &mats)
	}
	return mats
}

func collectMaterials(n *Node, seen map[string]bool, mats *[]string) {
	if n.TypeName == "Material" || n.TypeName == "MaxMaterial" {
		name := n.String("name")
		if name != "" && !seen[name] {
			seen[name] = true
			*mats = append(*mats, name)
		}
	}
	for _, f := range n.Fields {
		for _, v := range f.Values {
			if v.Kind == VNode && v.Node != nil {
				collectMaterials(v.Node, seen, mats)
			}
			if v.Kind == VArray {
				for _, av := range v.Array {
					if av.Kind == VNode && av.Node != nil {
						collectMaterials(av.Node, seen, mats)
					}
				}
			}
		}
	}
	for _, child := range n.Children {
		collectMaterials(child, seen, mats)
	}
}

// Stringer for Scene summary
func (s *Scene) GoString() string {
	refs := s.MeshRefs()
	mats := s.Materials()
	return strconv.Itoa(s.NodeCount()) + " nodes, " +
		strconv.Itoa(len(refs)) + " mesh refs, " +
		strconv.Itoa(len(mats)) + " materials"
}
