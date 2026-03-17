// Xbox Dashboard Material System
// Derived from xboxdash.xbe CMaxMaterial class hierarchy

// Material object memory layout:
//   +0x00: vtable index (determines Apply method)
//   +0x04: name string pointer (UTF-16 VA in XBE memory)
//   +0x08: shader config
//   +0x0C: color1 (ARGB packed uint32 for combiner; 4 bytes R,G,B,A for flatcolor)
//   +0x10: color2 (ARGB packed uint32)
//   +0x14: texture index / blend mode
//   +0x220: key mode byte 1
//   +0x221: key mode byte 2

// Material registry
const materials = {};     // name → material object
const materialList = [];  // ordered list of all materials

class CMaxMaterial {
  constructor(name, shaderCfg) {
    this.name = name;
    this.shaderCfg = shaderCfg || 0;
    this.vtable = 'base';
    this.color1 = 0;         // packed ARGB
    this.color2 = 0;         // packed ARGB
    this.textureIndex = 0;
    this.colorBytes = null;  // [R,G,B,A] for flatcolor/panel/backing types
    this.keyMode1 = 0;
    this.keyMode2 = 0;

    // Register in global table
    materials[name] = this;
    materialList.push(this);
  }

  // Unpack color1 as ARGB components
  getARGB() {
    if (this.colorBytes) {
      return {
        r: this.colorBytes[0],
        g: this.colorBytes[1],
        b: this.colorBytes[2],
        a: this.colorBytes[3]
      };
    }
    const c = this.color1 >>> 0;
    return {
      a: (c >> 24) & 0xFF,
      r: (c >> 16) & 0xFF,
      g: (c >> 8) & 0xFF,
      b: c & 0xFF
    };
  }
}

// --- Constructors (from decompiled xboxdash.xbe) ---

function createFlatcolor(name, r, g, b, a, shaderCfg) {
  const m = new CMaxMaterial(name, shaderCfg);
  m.vtable = 'flatcolor';
  m.colorBytes = [r & 0xFF, g & 0xFF, b & 0xFF, a & 0xFF];
  return m;
}

function createCombiner(name, color1, color2, shaderCfg) {
  const m = new CMaxMaterial(name, shaderCfg);
  m.vtable = 'combiner';
  m.color1 = color1 >>> 0;
  m.color2 = color2 >>> 0;
  m.textureIndex = 1;
  return m;
}

function createWireframe(name, color1, color2, shaderCfg) {
  const m = new CMaxMaterial(name, shaderCfg);
  m.vtable = 'wireframe';
  m.color1 = color1 >>> 0;
  m.color2 = color2 >>> 0;
  m.textureIndex = 3;
  return m;
}

function createChrome(name, color1, color2, shaderCfg) {
  const m = new CMaxMaterial(name, shaderCfg);
  m.vtable = 'chrome';
  m.color1 = color1 >>> 0;
  m.color2 = color2 >>> 0;
  m.textureIndex = 2;
  return m;
}

function createInnerwall(name, color1, color2, shaderCfg) {
  const m = new CMaxMaterial(name, shaderCfg);
  m.vtable = 'innerwall';
  m.color1 = color1 >>> 0;
  m.color2 = color2 >>> 0;
  m.textureIndex = 3;
  return m;
}

function createPanel(name, r, g, b, a, shaderCfg) {
  const m = new CMaxMaterial(name, shaderCfg);
  m.vtable = 'panel';
  m.colorBytes = [r & 0xFF, g & 0xFF, b & 0xFF, a & 0xFF];
  return m;
}

function createBacking(name, r, g, b, a, shaderCfg) {
  const m = new CMaxMaterial(name, shaderCfg);
  m.vtable = 'backing';
  m.colorBytes = [r & 0xFF, g & 0xFF, b & 0xFF, a & 0xFF];
  return m;
}

function createOrb(name, shaderCfg) {
  const m = new CMaxMaterial(name, shaderCfg);
  m.vtable = 'orb';
  return m;
}

function createReflect(name) {
  const m = new CMaxMaterial(name, 0);
  m.vtable = 'reflect';
  m.color1 = 4;  // this+0x0C = 4 (from decompiled code)
  return m;
}

function createEggpulse(name) {
  // Inherits from combiner with color1=0, color2=0
  const m = new CMaxMaterial(name, 0);
  m.vtable = 'eggpulse';
  m.color1 = 0;
  m.color2 = 0;
  m.textureIndex = 1;
  return m;
}

function createEggglow(name, color1, color2, shaderCfg) {
  // Inherits from combiner
  const m = new CMaxMaterial(name, shaderCfg);
  m.vtable = 'eggglow';
  m.color1 = color1 >>> 0;
  m.color2 = color2 >>> 0;
  m.textureIndex = 1;
  return m;
}

function createKey(name, mode1, mode2) {
  // Inherits from combiner with color1=0, color2=0
  const m = new CMaxMaterial(name, 0);
  m.vtable = 'key';
  m.color1 = 0;
  m.color2 = 0;
  m.textureIndex = 1;
  m.keyMode1 = mode1 & 0xFF;
  m.keyMode2 = mode2 & 0xFF;
  return m;
}

function createIcon(name, texIdx1, texIdx2, c, d, e) {
  const m = new CMaxMaterial(name, 0);
  m.vtable = 'icon';
  m.colorBytes = [c & 0xFF, d & 0xFF, e & 0xFF, 0];
  m.color1 = texIdx1;
  m.color2 = texIdx2;
  return m;
}

function createNumbered(name) {
  const m = new CMaxMaterial(name, 0);
  m.vtable = 'numbered';
  return m;
}

function createBase(name, shaderCfg) {
  const m = new CMaxMaterial(name, shaderCfg);
  m.vtable = 'base';
  return m;
}

// --- Apply methods (from decompiled xboxdash.xbe Apply_* functions) ---
// These compute the effective color each frame. For animated materials
// (eggpulse, eggglow, key), the color changes over time.

// Global time state (updated by runtime each frame)
let _time = 0;
let _lastTime = 0;
let _deltaTime = 0;

export function updateTime(t) {
  _deltaTime = t - _time;
  _lastTime = _time;
  _time = t;
}

// Apply methods return { r, g, b, a } for the material's current color.
//
// The NV2A register combiner blends color1, color2, and texture:
//   - color1: primary combiner constant (ARGB)
//   - color2: secondary combiner constant (ARGB)
//   - The combiner interpolates between them using the texture as a factor
//   - shaderCfg & 2: alpha blending enabled (SrcAlpha/InvSrcAlpha)
//   - shaderCfg == 0: opaque (alpha modulates RGB intensity)
//
// For shapes WITH textures, Three.js multiplies material.color × texture.
// We return a blended color from color1 and color2 so the texture modulation
// produces dark-to-bright variation matching the Xbox register combiner output.

function _unpackARGB(packed) {
  const c = packed >>> 0;
  return {
    a: (c >> 24) & 0xFF,
    r: (c >> 16) & 0xFF,
    g: (c >> 8) & 0xFF,
    b: c & 0xFF
  };
}

// NV2A register combiner output formula, derived from x86 emulator trace of
// SetRegisterCombinerConstants (VA 0x46912). The function writes two constants
// to the NV2A push buffer:
//   c0 = (color1.r/255, color1.g/255, color1.b/255, color1.a/255)
//   c1 = (-(1 - color2.r/255), -(1 - color2.g/255), -(1 - color2.b/255), -(color1.a/255))
//
// The register combiner computes: output = c0 * texture + c1
// With texture = 1.0 (white, the default when no material texture is bound):
//   output = c0 + c1 = color1/255 - (1 - color2/255) = (color1 + color2 - 255) / 255
//
// Values are clamped to [0, 1]. This produces the characteristic dark green of
// the Xbox dashboard from bright color1 (yellow-green) and dark color2 (green).
function _applyCombiner(m) {
  const c1 = _unpackARGB(m.color1);
  const c2 = _unpackARGB(m.color2);

  // From x86 emulator trace of SetRegisterCombinerConstants (VA 0x46912):
  //   c0.rgb = color1.rgb / 255
  //   c0.a   = globalScale(1.0) * color1.a / 255
  //   c1     = color2 / 255 - color1 / 255
  //
  // NV2A combiner: output = c0.rgb * texture + c1
  //              = lerp(color2/255, color1/255, texture)
  //
  // The material texture (8x256 SG8SB8 gradient, textureIndex) provides
  // per-pixel brightness. color1.alpha encodes the overall brightness level
  // that the texture maps to. Using c0.alpha as the uniform texture value:
  //   output = c0.rgb * c0.alpha + c1
  // output = c0.rgb * c0.alpha + c1.rgb
  //        = (color1.rgb/255) * (color1.a/255) + (color2.rgb/255 - color1.rgb/255)
  // Simplified in 0-255: output_i = color1_i * color1_a / 65025 + (color2_i - color1_i) / 255
  // Multiply through by 255: output_i_byte = color1_i * color1_a / 255 + color2_i - color1_i
  const t = c1.a / 255;
  const R = Math.max(0, Math.min(255, Math.round(c1.r * t + c2.r - c1.r)));
  const G = Math.max(0, Math.min(255, Math.round(c1.g * t + c2.g - c1.g)));
  const B = Math.max(0, Math.min(255, Math.round(c1.b * t + c2.b - c1.b)));

  // shaderCfg & 2: ApplyAlpha enables SrcAlpha/InvSrcAlpha blending.
  // Combiner alpha output = c0.a * texture + c1.a
  //   = (color1.a/255) * texture + (color2.a/255 - color1.a/255)
  // At texture = t: alpha = t^2 + (color2.a - color1.a) / 255
  // This produces near-zero alpha for most materials → nearly transparent.
  if (m.shaderCfg & 2) {
    const outAlpha = Math.max(0, Math.min(1, t * t + (c2.a - c1.a) / 255));
    return { r: R, g: G, b: B, a: Math.round(outAlpha * 255) };
  }
  return { r: R, g: G, b: B, a: 255 };
}

const applyMethods = {
  flatcolor(m) {
    // Apply_flatcolor: packs R,G,B into TEXTUREFACTOR with alpha squared.
    // The squared alpha modulates the RGB intensity.
    const [r, g, b, a] = m.colorBytes;
    const alphaF = (a / 255);
    const intensity = alphaF * alphaF;
    // shaderCfg & 2: alpha blending enabled
    if (m.shaderCfg & 2) {
      return {
        r: Math.round(r * intensity),
        g: Math.round(g * intensity),
        b: Math.round(b * intensity),
        a
      };
    }
    return {
      r: Math.round(r * intensity),
      g: Math.round(g * intensity),
      b: Math.round(b * intensity),
      a: 255
    };
  },

  combiner(m) {
    return _applyCombiner(m);
  },

  wireframe(m) {
    return _applyCombiner(m);
  },

  chrome(m) {
    return _applyCombiner(m);
  },

  innerwall(m) {
    return _applyCombiner(m);
  },

  panel(m) {
    // Apply_panel: same structure as flatcolor — alpha squared modulates RGB
    const [r, g, b, a] = m.colorBytes;
    const alphaF = (a / 255);
    const intensity = alphaF * alphaF;
    if (m.shaderCfg & 2) {
      return {
        r: Math.round(r * intensity),
        g: Math.round(g * intensity),
        b: Math.round(b * intensity),
        a
      };
    }
    return {
      r: Math.round(r * intensity),
      g: Math.round(g * intensity),
      b: Math.round(b * intensity),
      a: 255
    };
  },

  backing(m) {
    const [r, g, b, a] = m.colorBytes;
    return { r, g, b, a };
  },

  orb(m) {
    // Apply_orb: green glow sphere
    return { r: 0xB2, g: 0xD0, b: 0x00, a: 255 };
  },

  reflect(m) {
    return { r: 0xE5, g: 0xE5, b: 0xE5, a: 0xFF };
  },

  eggpulse(m) {
    // Animated: color1 = 0x00B2D000, alpha pulsates via sin(t^2)
    const t = _time * 0.5;
    const alpha = Math.abs(Math.sin(t * t));
    m.color1 = (Math.round(alpha * 255) << 24) | 0x00B2D000;
    return _applyCombiner(m);
  },

  eggglow(m) {
    // Animated: fades from transparent to opaque over time
    const fade = Math.min(1.0, _time * 0.2);
    const alpha = Math.round((1.0 - fade) * 255);
    m.color2 = (alpha << 24) | 0x00FEFFBC;
    return _applyCombiner(m);
  },

  key(m) {
    if (!m.keyMode1) {
      m.color1 = 0xFFB2D000;
      m.color2 = 0xBEB2D000;
    } else {
      m.color1 = 0xFFF3FF6B;
      m.color2 = 0x2219E100;
    }
    return _applyCombiner(m);
  },

  icon(m) {
    return { r: 255, g: 255, b: 255, a: 255 };
  },

  numbered(m) {
    const alpha = Math.round(Math.abs(Math.sin(_time)) * 255);
    return { r: 255, g: 255, b: 255, a: alpha };
  },

  base(m) {
    return { r: 255, g: 255, b: 255, a: 255 };
  },
};

// Get the current RGBA for a material (calls its Apply method)
export function applyMaterial(name) {
  const m = materials[name];
  if (!m) return null;
  const fn = applyMethods[m.vtable];
  if (!fn) return null;
  return fn(m);
}

// Look up a material by name
export function getMaterial(name) {
  return materials[name] || null;
}

// Get the vtable type for a material (e.g. 'backing', 'combiner', 'flatcolor')
export function getVtable(name) {
  const m = materials[name];
  return m ? m.vtable : null;
}

// Get the material's own texture index (0 = none, 1 = combiner gradient,
// 2 = chrome/steel, 3 = cellwall). The NV2A register combiner binds this
// texture via LookupTexture(this+0x14) before rendering.
export function getTextureIndex(name) {
  const m = materials[name];
  return m ? m.textureIndex : 0;
}

export function getMaterialCount() {
  return materialList.length;
}

// --- CreateAllMaterials (from decompiled xboxdash.xbe 0x00043B30) ---
// Each call below is a direct translation of the transpiled constructor calls
// with their arguments read from the decompiled function.

export function createAllMaterials() {
  // The arguments below are read from the decompiled CreateAllMaterials function.
  // Push order in x86: last pushed = first stack arg.
  // For combiner: createCombiner(name, color1, color2, shaderCfg)
  // For flatcolor: createFlatcolor(name, R, G, B, A, shaderCfg)

  createInnerwall("InnerWall_01", 0xFFF3FF6B, 0x1428D414, 0);
  createCombiner("InnerWall_02", 0xFFF3FF6B, 0x1414C000, 2);
  createBase("MetaInfo_Text", 1);
  createBase("NamePanel_Text", 1);
  createBase("GameNameText_01", 1);
  createBase("GameNameText_02", 1);
  createBase("GameNameText_03", 1);
  createBase("GameIcon_01", 1);
  createBase("GameIcon_03", 1);
  createBase("Material #132", 1);
  createBase("u2 info", 1);
  createFlatcolor("Material #1334", 11, 32, 0, 192, 0);
  createFlatcolor("XBOXgreendark", 6, 33, 0, 255, 0);
  createFlatcolor("XBOXgreen", 140, 201, 25, 255, 0);
  createFlatcolor("XBoxGreen2", 139, 200, 24, 255, 0);
  createFlatcolor("GameHilite33", 221, 208, 120, 178, 0);
  createFlatcolor("Nothing", 128, 128, 128, 255, 0);
  createFlatcolor("NavType", 190, 250, 94, 178, 0);
  createFlatcolor("RedType", 200, 30, 30, 255, 0);
  createFlatcolor("XBoxGreen", 139, 200, 24, 255, 0);
  createFlatcolor("Type", 100, 200, 25, 255, 0);
  createFlatcolor("Typesdsafsda", 255, 255, 255, 179, 0);
  createFlatcolor("Material #133", 0, 0, 0, 192, 0);
  createFlatcolor("Material #1335", 76, 162, 0, 200, 0);
  createFlatcolor("Material #133511", 41, 87, 0, 255, 0);
  createFlatcolor("Material #1336", 0, 0, 0, 128, 0);
  createFlatcolor("HilightedType", 3, 44, 0, 255, 0);
  createFlatcolor("XBoxGreenq", 139, 200, 24, 255, 0);
  createFlatcolor("Black80", 0, 0, 0, 204, 0);
  createFlatcolor("CellEgg/Partsw", 77, 224, 57, 166, 2);
  createFlatcolor("CellEgg/Partsz", 170, 170, 170, 255, 2);
  createFlatcolor("Material #108", 160, 252, 0, 255, 2);
  createFlatcolor("ItemsType", 182, 245, 96, 255, 2);
  createFlatcolor("GameHiliteMemory", 178, 208, 0, 255, 0);
  createFlatcolor("red", 255, 0, 0, 255, 0);
  createCombiner("FlatSurfaces", 0xC0F3FF6B, 0x0014C000, 0);
  createCombiner("DarkSurfaces", 0x5ACBCD55, 0x0014C000, 0);
  createCombiner("DarkSurfaces2", 0x5ACBCD55, 0x000A6000, 0);
  createCombiner("FlatSurfacesSelected", 0xC0F3FF6B, 0x8014C000, 0);
  createCombiner("FlatSurfacesMemory", 0xC0F3FF6B, 0x8014C000, 0);
  createCombiner("FlatSurfaces2sided", 0xC0F3FF6B, 0x0014C000, 2);
  createCombiner("DetailLegSkin_Inner", 0xC0F3FF6B, 0x0014C000, 0);
  createCombiner("Screen", 0xC0F3FF6B, 0x0014C000, 2);
  createCombiner("Spout", 0xC0F3FF6B, 0x0014C000, 0);
  createCombiner("NavType34", 0xC0F3FF6B, 0x0014C000, 0);
  createCombiner("MetaFlatSurfaces", 0xC0F3FF6B, 0x0014C000, 0);
  createCombiner("SC_SavedGame_Row01", 0xC0F3FF6B, 0x0014C000, 0);
  createCombiner("HK_SavedGame_Row01", 0xC0F3FF6B, 0x0014C000, 0);
  createCombiner("SavedEgg_Selected", 0xC0F3FF6B, 0x0014C000, 0);
  createCombiner("FlatUnselected", 0xC0F3FF6B, 0x0014C000, 0);
  createCombiner("Mem_InnerWall_Outer", 0xC0F3FF6B, 0x0014C000, 0);
  createCombiner("SavedGameEgg", 0xC0F3FF6B, 0x0014C000, 0);
  createCombiner("GameMenuEgg", 0xC0F3FF6B, 0x0014C000, 0);
  createCombiner("Shell", 0xC0F3FF6B, 0x0014C000, 0);
  createCombiner("Material #133sdsfdsf", 0xC0F3FF6B, 0x0014C000, 0);
  createCombiner("IconParts", 0xC0F3FF6B, 0x0014C000, 2);
  createCombiner("IconParts1", 0xC0F3FF6B, 0x0014C000, 0);
  createCombiner("MU1Pod_HL", 0xC0F3FF6B, 0x0014C000, 0);
  createCombiner("MU1Pod", 0xC0F3FF6B, 0x0014C000, 0);
  createCombiner("GamePodb", 0xC0F3FF6B, 0x0014C000, 2);
  createCombiner("GamePod", 0xC0F3FF6B, 0x0014C000, 0);
  createCombiner("JwlSrfc01/InfoPnls", 0xC0F3FF6B, 0x0014C000, 0);
  createCombiner("MenuCell", 0xC0F3FF6B, 0x0014C000, 0);
  createWireframe("Wireframe", 0xC0F3FF6B, 0x007DD222, 0);
  createCombiner("Tubes", 0xD7F2FA99, 0x25076800, 2);
  createCombiner("JewelSurface02/PodMesh", 0xD7F2FA99, 0x25076800, 0);
  createCombiner("TubesFade", 0xD7F2FA99, 0x25076800, 2);
  createCombiner("TubesQ", 0xD7F2FA99, 0x25076800, 0);
  createCombiner("EmptyMU", 0xD7F2FA99, 0x25076800, 0);
  createCombiner("Tube", 0xD7F2FA99, 0x25076800, 0);
  createCombiner("MemoryHeader", 0x7A3CC643, 0x25076800, 2);
  createEggglow("EggGlow", 0x00FCFF00, 0xE4FEFFBC, 0);
  createCombiner("ButtonGlow", 0x00FCFF00, 0xE4FEFFBC, 0);
  createCombiner("gradient", 0x33BFFF6B, 0x0000FF12, 0);
  createCombiner("CellWallStructure", 0x33BFFF6B, 0x0000FF12, 0);
  createCombiner("FlatSrfc/PodParts", 0x33BFFF6B, 0x0000FF12, 0);
  createCombiner("Cell_Light", 0x33BFFF6B, 0x0000FF12, 0);
  createCombiner("Cell_Light/LegSkin", 0x33BFFF6B, 0x0000FF12, 0);
  createCombiner("CellEgg/Parts", 0xB2F3FF6B, 0x001EFF00, 0);
  createCombiner("GameEgg", 0xB2F3FF6B, 0x001EFF00, 0);
  createCombiner("FlatSurfaces2sided3", 0xFFFD1E00, 0x00F24800, 2);
  createCombiner("console_hilite", 0xFFFFAD6B, 0x00F8A100, 0);
  createIcon("TitleIcon", 128, 128, 0, 0, 0);
  createIcon("TitleSoundtrackIcon", 128, 128, 0, 1, 0);
  createIcon("SavedGameIcon", 2031360, 2031360, 0, 0, 1);
  createIcon("SoundtrackIcon", 2031360, 2031360, 0, 1, 1);
  createIcon("SelectedIcon", 128, 128, 1, 0, 0);
  createKey("Key", 0, 0);
  createKey("BrightKey", 0, 1);
  createKey("KeyText", 1, 0);
  createEggpulse("EggGlowPulse");
  createChrome("Metal_Chrome", 0x00E5E5E5, 0xFFE5E5E5, 0);
  createChrome("Tvbox", 0x00E5E5E5, 0xFFE5E5E5, 0);
  createChrome("AudioCD", 0x28FFFFFF, 0xB4FFFFFF, 0);
  createBacking("PanelBacking_01", 255, 0, 20, 0, 0);
  createBacking("PanelBacking_02", 255, 0, 0, 0, 0);
  createBacking("PanelBacking_03", 240, 0, 20, 0, 0);
  createBacking("PanelBacking_04", 255, 7, 46, 14, 0);
  createBacking("NameBacking", 255, 0, 8, 1, 0);
  createBacking("ModeBacking", 255, 0, 8, 1, 0);
  createBacking("SavedGameBacking", 255, 10, 18, 11, 0);
  createBacking("MemManMetaBacking", 255, 0, 25, 0, 0);
  createBacking("TextBacking", 255, 0, 0, 0, 6);
  createBacking("DarkenBacking", 255, 0, 50, 0, 10);
  createBacking("MetaBacking", 255, 0, 50, 0, 6);
  createBacking("DarkenBackingDark", 255, 0, 8, 1, 6);
  createPanel("GameHilite", 255, 255, 255, 255, 2);
  createOrb("equalizer", 32);
  createOrb("MainMenuOrb", 2031360);
  createReflect("ReflectSurface");
  createPanel("PanelBacking", 255, 0, 20, 0, 2);
  createNumbered("Material #10822");

  return materialList.length;
}
