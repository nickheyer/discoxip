# Gaps

## RENDERING

### Derived from the executable
Material constructors and Apply methods, the shape, appearance, material node and mesh draw methods, the per node vertex shader constants, the vertex programs and declarations, the render state defaults, the procedural textures, the node class registry with field tables and constructor defaults, the field write hook each draw chain class runs when a script assigns one of its fields, the render states the text draw routine sets around its glyph draw, the scene path join and its input roles, the library function identities the linker records in the executable, the chunked font layout and its LZX streams, the static device with the surfaces its creation fills, the texture setter the optimizer inlines, the controller routines the runtime's own input stands in for, and the class descriptors the static initializers register, linked into the class list the way the CRT init leaves them.

### Not yet derived
- The text glyph layout. Glyphs are placed by the runtime from the font outlines and drawn with the fixed function transform.
- The keyboard node's per key selection flag and the memory browser's icon names. Both globals start at their image values and no runtime code writes them.
- The draw routines of node classes other than Shape, Appearance, Material, MaxMaterial, Mesh, Sphere, Texture and ImageTexture.
- The dynamic texture of the music scene. Its palette, star field, image fader and audio visualizer children plot into an eight bit surface through loops the path summaries do not carry, so the runtime renders no dynamic texture.
