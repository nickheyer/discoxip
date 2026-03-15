package buffer

import (
	"fmt"
	"io"
)

// ExportOBJ writes vertices and indices in Wavefront OBJ format.
func ExportOBJ(w io.Writer, verts []Vertex, indices []uint16) error {
	// Positions
	for _, v := range verts {
		if _, err := fmt.Fprintf(w, "v %f %f %f\n", v.Pos[0], v.Pos[1], v.Pos[2]); err != nil {
			return err
		}
	}

	// Normals
	hasNormals := false
	for _, v := range verts {
		if v.Normal[0] != 0 || v.Normal[1] != 0 || v.Normal[2] != 0 {
			hasNormals = true
			break
		}
	}
	if hasNormals {
		for _, v := range verts {
			if _, err := fmt.Fprintf(w, "vn %f %f %f\n", v.Normal[0], v.Normal[1], v.Normal[2]); err != nil {
				return err
			}
		}
	}

	// UVs
	hasUVs := false
	for _, v := range verts {
		if v.UV[0] != 0 || v.UV[1] != 0 {
			hasUVs = true
			break
		}
	}
	if hasUVs {
		for _, v := range verts {
			if _, err := fmt.Fprintf(w, "vt %f %f\n", v.UV[0], v.UV[1]); err != nil {
				return err
			}
		}
	}

	// Faces (OBJ indices are 1-based)
	for i := 0; i+2 < len(indices); i += 3 {
		i0 := int(indices[i]) + 1
		i1 := int(indices[i+1]) + 1
		i2 := int(indices[i+2]) + 1

		switch {
		case hasNormals && hasUVs:
			_, err := fmt.Fprintf(w, "f %d/%d/%d %d/%d/%d %d/%d/%d\n",
				i0, i0, i0, i1, i1, i1, i2, i2, i2)
			if err != nil {
				return err
			}
		case hasNormals:
			_, err := fmt.Fprintf(w, "f %d//%d %d//%d %d//%d\n",
				i0, i0, i1, i1, i2, i2)
			if err != nil {
				return err
			}
		case hasUVs:
			_, err := fmt.Fprintf(w, "f %d/%d %d/%d %d/%d\n",
				i0, i0, i1, i1, i2, i2)
			if err != nil {
				return err
			}
		default:
			if _, err := fmt.Fprintf(w, "f %d %d %d\n", i0, i1, i2); err != nil {
				return err
			}
		}
	}

	return nil
}
