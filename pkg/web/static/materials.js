// Xbox Dashboard Material System
// Derived from xboxdash.xbe CMaxMaterial class hierarchy
// Material definitions loaded from materials.json (extracted from XBE binary)

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
    this.textureFile = null;
    this.colorBytes = null;  // [R,G,B,A] for flatcolor/panel/backing types
    this.keyMode1 = 0;
    this.keyMode2 = 0;

    // Register in global table
    materials[name] = this;
    materialList.push(this);
  }
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
// NV2A register combiner formula (derived from XBE binary at VA 0x00046912):
//   C0 = color1.rgb, C1 = (color2 - color1).rgb
//   result.rgb = C0 + C1 * T = lerp(color1, color2, T)
// where T is the texture sample (0 = color1, 1 = color2).
//   shaderCfg & 2: alpha blending via lerp(color1.a, color2.a, T)
//   shaderCfg == 0: opaque (alpha = 255)

function _unpackARGB(packed) {
  const c = packed >>> 0;
  return {
    a: (c >> 24) & 0xFF,
    r: (c >> 16) & 0xFF,
    g: (c >> 8) & 0xFF,
    b: c & 0xFF
  };
}

// NV2A register combiner equation derived from x86 binary at VA 0x00046912
// (SetRegisterCombinerConstants in xboxdash.xbe).
//
// The function unpacks color1 and color2 as float4, then writes:
//   C0 = {color1.R, color1.G, color1.B, globalScale * color1.A}
//   C1 = {color2.R - color1.R, color2.G - color1.G, color2.B - color1.B,
//          (color2.A - color1.A) * globalScale}
//
// Raw x86 at 0x4694D: FSUB DWORD [ESI] (d8 26), NOT FSUB ST0,ST0.
// ESI = color1 ptr, EAX = color2 ptr. C1 = color2 - color1.
//
// The NV2A combiner computes: result = C0 + C1 * T
//   = color1 + (color2 - color1) * T = lerp(color1, color2, T)
//
// Where T is the per-pixel texture sample from the bound material texture
// (index 1=gradient, 2=chrome, 3=cellwall).
function _applyCombiner(m) {
  const c1 = _unpackARGB(m.color1);
  const c2 = _unpackARGB(m.color2);

  // Uniform midpoint sample stands in for the per pixel texture value
  const t = 0.5;

  const r = Math.round(c1.r + (c2.r - c1.r) * t);
  const g = Math.round(c1.g + (c2.g - c1.g) * t);
  const b = Math.round(c1.b + (c2.b - c1.b) * t);
  const a = Math.round(c1.a + (c2.a - c1.a) * t);

  return { r: Math.max(0, Math.min(255, r)),
           g: Math.max(0, Math.min(255, g)),
           b: Math.max(0, Math.min(255, b)),
           a: Math.max(0, Math.min(255, a)) };
}

const applyMethods = {
  flatcolor(m) {
    // Apply_flatcolor: sub_00055B8C packs raw R,G,B into D3D TEXTUREFACTOR
    // with alpha^2 in the alpha channel. The 6 SetTextureStageState calls
    // configure COLORARG1=TEXTUREFACTOR, COLOROP=SELECTARG1, so the output
    // RGB IS the raw material color. Alpha^2 is used only for blending.
    const [r, g, b, a] = m.colorBytes;
    if (m.shaderCfg & 2) {
      const alphaSq = Math.round(a * a / 255);
      return { r, g, b, a: alphaSq };
    }
    return { r, g, b, a: 255 };
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
    // Apply_panel: same D3D TEXTUREFACTOR path as flatcolor — raw RGB output,
    // alpha^2 in alpha channel for blending only.
    const [r, g, b, a] = m.colorBytes;
    if (m.shaderCfg & 2) {
      const alphaSq = Math.round(a * a / 255);
      return { r, g, b, a: alphaSq };
    }
    return { r, g, b, a: 255 };
  },

  backing(m) {
    const [r, g, b, a] = m.colorBytes;
    return { r, g, b, a };
  },

  orb(m) {
    return _applyCombiner(m);
  },

  reflect(m) {
    return _applyCombiner(m);
  },

  eggpulse(m) {
    // Animated: alpha pulsates via sin(t²). The base color RGB comes from
    // the Apply method's init_writes (MOV [this+0xC], imm).
    const t = _time * 0.5;
    const alpha = Math.abs(Math.sin(t * t));
    const base = m._initColor1 || 0;
    m.color1 = (Math.round(alpha * 255) << 24) | (base & 0x00FFFFFF);
    return _applyCombiner(m);
  },

  eggglow(m) {
    // Animated: fades from transparent to opaque over time. The base color2
    // RGB comes from the Apply method's init_writes (OR imm).
    const fade = Math.min(1.0, _time * 0.2);
    const alpha = Math.round((1.0 - fade) * 255);
    const base = m._initMask || 0;
    m.color2 = (alpha << 24) | (base & 0x00FFFFFF);
    return _applyCombiner(m);
  },

  key(m) {
    // Mode-based color selection. The color pairs come from the Apply
    // method's init_writes (alternating MOV [this+0xC/0x10], imm pairs).
    if (m._colorPairs) {
      const pair = m._colorPairs[m.keyMode1 ? 1 : 0];
      if (pair) {
        m.color1 = pair[0];
        m.color2 = pair[1];
      }
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

// Returns the d3d_state object resolved from the Apply trace or null
export function getD3DState(name) {
  const m = materials[name];
  return m ? m._d3dState : null;
}

// Returns the extracted texture filename bound to a material or null
export function getTextureFile(name) {
  const m = materials[name];
  return m ? m.textureFile : null;
}

// --- Load materials from extracted JSON (materials.json) ---

// Maps materials.json fields to CMaxMaterial objects. The JSON is produced
// by matparse.go which extracts constructor args and Apply method traces
// directly from the XBE binary.
export function loadFromJSON(jsonArray) {
  for (const entry of jsonArray) {
    const m = new CMaxMaterial(entry.name, entry.shader_cfg || 0);
    m.vtable = entry.type || 'base';
    m.color1 = (entry.color1 || 0) >>> 0;
    m.color2 = (entry.color2 || 0) >>> 0;
    m.textureIndex = entry.texture_index || 0;
    m.textureFile = entry.texture_file || null;
    m.keyMode1 = entry.key_mode1 || 0;
    m.keyMode2 = entry.key_mode2 || 0;
    m.colorBytes = [entry.r, entry.g, entry.b, entry.a];

    // Extract constants from Apply method init_writes.
    // init_writes captures all MOV [this+offset], imm instructions from the
    // Apply function. Offset 12 (0x0C) = color1, offset 16 (0x10) = color2,
    // offset -1 = OR immediate (used for color masks).
    const iw = entry.init_writes;
    if (iw && iw.length > 0) {
      for (const w of iw) {
        if (w.offset === 12 && !m._initColor1) m._initColor1 = w.value >>> 0;
        else if (w.offset === 16 && !m._initColor2) m._initColor2 = w.value >>> 0;
        else if (w.offset === -1 && !m._initMask) m._initMask = w.value >>> 0;
      }
      // Collect all (offset=12, offset=16) pairs for mode-switching materials
      const pairs = [];
      let cur = null;
      for (const w of iw) {
        if (w.offset === 12) { cur = w.value >>> 0; }
        else if (w.offset === 16 && cur !== null) {
          pairs.push([cur, w.value >>> 0]);
          cur = null;
        }
      }
      if (pairs.length >= 2) m._colorPairs = pairs;
    }

    m._d3dState = entry.d3d_state;
  }
  return materialList.length;
}
