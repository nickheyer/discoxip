# Rendering Gaps

Reference: `CURRENT.png` (ours left, target right)

## Material textures
Indices 2 (steel) and 3 (cellwall) are bound as the material map. Index 1 is the NV2A gradient generated on the GPU with no file, so combiner materials use a uniform midpoint sample instead of a per pixel gradient.

## D3D state
Applied from materials.json: alpha blend enable, source and destination blend factors, depth write. Traced but not applied: texture stage states, alpha test, register combiner constants. The orb and reflect Apply methods fall through to the combiner formula until those are mapped.
