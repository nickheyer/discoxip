# Rendering Gaps

Reference: `CURRENT_NEW.png` (ours) against `ORIGINAL_TARGET.png`.

## Derived from the executable
Material constructors and Apply methods, the shape, appearance, material node and mesh draw methods, the per node vertex shader constants, the vertex programs and declarations, the render state defaults, the procedural textures and the node class registry with field tables and constructor defaults.

## Not derived
- The text geometry draw routine. Text draws with the fixed function transform and the state its appearance leaves.
- The keyboard node's per key selection flag and the memory browser's icon names. Both globals start at their image values and no runtime code writes them.
- The draw routines of node classes other than Shape, Appearance, Material, MaxMaterial, Mesh, Sphere, Texture and ImageTexture.
