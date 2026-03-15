package xap

import (
	"fmt"
	"io"
	"strings"
)

// PrettyPrint writes an indented representation of the scene.
func PrettyPrint(w io.Writer, scene *Scene) {
	for _, n := range scene.Nodes {
		printNode(w, n, 0)
	}
}

func printNode(w io.Writer, n Node, depth int) {
	indent := strings.Repeat("  ", depth)

	switch v := n.(type) {
	case *Transform:
		if v.Name != "" {
			fmt.Fprintf(w, "%sDEF %s Transform {\n", indent, v.Name)
		} else {
			fmt.Fprintf(w, "%sTransform {\n", indent)
		}
		if v.HasFade {
			fmt.Fprintf(w, "%s  fade %g\n", indent, v.Fade)
		}
		if v.HasTranslation {
			fmt.Fprintf(w, "%s  translation %g %g %g\n", indent,
				v.Translation[0], v.Translation[1], v.Translation[2])
		}
		if v.HasRotation {
			fmt.Fprintf(w, "%s  rotation %g %g %g %g\n", indent,
				v.Rotation[0], v.Rotation[1], v.Rotation[2], v.Rotation[3])
		}
		if v.HasScale {
			fmt.Fprintf(w, "%s  scale %g %g %g\n", indent,
				v.Scale[0], v.Scale[1], v.Scale[2])
		}
		if v.HasScaleOri {
			fmt.Fprintf(w, "%s  scaleOrientation %g %g %g %g\n", indent,
				v.ScaleOrientation[0], v.ScaleOrientation[1],
				v.ScaleOrientation[2], v.ScaleOrientation[3])
		}
		if len(v.Children) > 0 {
			fmt.Fprintf(w, "%s  children [\n", indent)
			for _, child := range v.Children {
				printNode(w, child, depth+2)
			}
			fmt.Fprintf(w, "%s  ]\n", indent)
		}
		fmt.Fprintf(w, "%s}\n", indent)

	case *Shape:
		fmt.Fprintf(w, "%sShape {\n", indent)
		if v.Appearance != nil && v.Appearance.Material != nil {
			mat := v.Appearance.Material
			fmt.Fprintf(w, "%s  material %s { name %q }\n", indent, mat.Type, mat.Name)
		}
		if v.Geometry != nil {
			if v.Geometry.Name != "" {
				fmt.Fprintf(w, "%s  geometry DEF %s Mesh { url %q }\n", indent, v.Geometry.Name, v.Geometry.URL)
			} else {
				fmt.Fprintf(w, "%s  geometry Mesh { url %q }\n", indent, v.Geometry.URL)
			}
		}
		fmt.Fprintf(w, "%s}\n", indent)

	case *MeshRef:
		if v.Name != "" {
			fmt.Fprintf(w, "%sDEF %s Mesh { url %q }\n", indent, v.Name, v.URL)
		} else {
			fmt.Fprintf(w, "%sMesh { url %q }\n", indent, v.URL)
		}
	}
}
