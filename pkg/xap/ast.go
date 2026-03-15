package xap

// Node is a scene graph node.
type Node interface {
	nodeType() string
}

// Scene is the top-level container of nodes.
type Scene struct {
	Nodes []Node
}

// Transform is a VRML Transform node with children and spatial properties.
type Transform struct {
	Name             string
	Children         []Node
	Rotation         [4]float64 // axis + angle
	Scale            [3]float64
	ScaleOrientation [4]float64
	Translation      [3]float64
	Fade             float64
	HasRotation      bool
	HasScale         bool
	HasScaleOri      bool
	HasTranslation   bool
	HasFade          bool
	Fields           map[string]interface{} // catch-all for unknown fields
}

func (Transform) nodeType() string { return "Transform" }

// Shape holds appearance and geometry.
type Shape struct {
	Appearance *Appearance
	Geometry   *MeshRef
}

func (Shape) nodeType() string { return "Shape" }

// Appearance wraps a material.
type Appearance struct {
	Material *Material
}

// Material holds material properties.
type Material struct {
	Type string // e.g. "MaxMaterial"
	Name string
}

// MeshRef references an external mesh file.
type MeshRef struct {
	Name string // DEF name
	URL  string // file path
}

func (MeshRef) nodeType() string { return "MeshRef" }
