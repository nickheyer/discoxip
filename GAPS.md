# Rendering Gaps

Reference: `CURRENT_NEW.png` (ours) against `ORIGINAL_TARGET.png`.

## Derived from the executable
Material constructors and Apply methods, the shape, appearance, material node and mesh draw methods, the per node vertex shader constants, the vertex programs and declarations, the render state defaults, the procedural textures, the node class registry with field tables and constructor defaults, the field write hook each draw chain class runs when a script assigns one of its fields, and the render states the text draw routine sets around its glyph draw.

## Not derived
- The text glyph layout. Glyphs are placed by the runtime from the font outlines and drawn with the fixed function transform.
- The keyboard node's per key selection flag and the memory browser's icon names. Both globals start at their image values and no runtime code writes them.
- The draw routines of node classes other than Shape, Appearance, Material, MaxMaterial, Mesh, Sphere, Texture and ImageTexture.
