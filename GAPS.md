# Rendering Gaps: Current vs Target

Side-by-side reference: `./CURRENT.png` (ours on left, target on right)

---

## COMPLETED

### 1–4, 9. Material system rewrite

- matparse.go decodes ctor_args into struct fields via binary instruction
  analysis (no type-name switches — reads MOV patterns from constructor x86)
- materials.json now contains fully populated color1/color2/R/G/B/A/shaderCfg/
  textureIndex/keyMode fields, extracted from the XBE binary
- runtime.js loads materials.json and calls `loadFromJSON()` instead of the
  deleted `createAllMaterials()` (no hardcoded material definitions)
- Apply method constants (eggpulse base color, eggglow base color2, key color
  pairs) read from init_writes in the JSON, not hardcoded literals
- orb/reflect Apply methods fall through to combiner formula using extracted
  color1/color2 (correct colors require Gap #8 — D3D state interpretation)
- Stale transpile comments cleaned up

### 5. Vertex colors

- Meshes with vertex colors get a cloned material with `vertexColors: true`

### 7. Xbox XTF vector fonts

- XTF font files parsed and exported as JSON glyph data (verts, indices, advance)
- TextRenderer renders text as 3D triangle meshes using parsed glyph geometry
- Heading (Xbox) and body (Xbox Book) fonts loaded and bound by role

---

## REMAINING

## 6. Material textures (indices 1/2/3) never loaded

Materials reference internal textures by index:
- 1 = combiner gradient (8x256 SG8SB8, THIS IS A RUNTIME ALGORITHM FROM THE GPU — no file - DO NOT TRY TO REPLICATE)
- 2 = chrome/steel reflection (`steel.png` exists in extracted output)
- 3 = cellwall structure (`cellwall.png` exists in extracted output)

`getTextureIndex()` returns the index but nothing in the runtime ever loads or
binds these textures. The combiner formula in `_applyCombiner()` approximates
the effect by using `color1.a / 255` as a uniform texture value, but this
produces flat colors instead of the per-pixel gradient variation visible in the
target.

**Blocked on:** The XBE has a texture lookup table that maps index → loaded
texture. We don't extract this table, so we can't derive the index → filename
mapping from the binary. Requires tracing the XBE's texture loading code to
resolve which XIP texture gets assigned which index.

---

## 8. D3D state calls extracted but unused

`materials.json` contains the full Apply method trace for each material —
`SetTextureStageState_Deferred`, `SetRenderState`, `SetRegisterCombinerConstants`
calls with arguments. These define:
- Blend modes (SrcBlend/DestBlend)
- Texture filtering and addressing
- Alpha test thresholds
- Color/alpha operations

None of this data is read or applied by the runtime. The `_applyCombiner()`
formula is a manual approximation of what these calls configure. Implementing
this would also fix orb/reflect Apply methods which currently fall through to
the combiner formula with insufficient extracted data.

---

## Priority Order

1. **#6** — Load/bind material textures by index
2. **#8** — Map D3D state calls to Three.js material properties
