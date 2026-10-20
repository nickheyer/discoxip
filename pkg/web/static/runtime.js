import * as THREE from 'three';
import { DerivedModel, cloneState, makeBuffer } from './derived.js';
import { DevicePipeline, describeGeometry } from './pipeline.js';

// ============================================================================
// XAP Runtime — Full-parity Three.js recreation of Xbox Dashboard
// ============================================================================

const DATA_BASE = 'data';

// Capture console before XAP scripts can clobber globals
const _console = { log: console.log, warn: console.warn, error: console.error };
const _log = _console.log.bind(console);
const _warn = _console.warn.bind(console);
const _error = _console.error.bind(console);

// ============================================================================
// Main entry point
// ============================================================================

class XAPRuntime {
  constructor() {
    this.renderer = new THREE.WebGLRenderer({ antialias: true });
    this.renderer.setPixelRatio(window.devicePixelRatio);
    this.renderer.setSize(window.innerWidth, window.innerHeight);
    this.renderer.setClearColor(0x000000);
    this.renderer.outputColorSpace = THREE.SRGBColorSpace;
    // The dashboard draws in scene order, which the derived device state depends on.
    this.renderer.sortObjects = false;
    document.body.appendChild(this.renderer.domElement);

    this.scene = new THREE.Scene();
    this.camera = new THREE.PerspectiveCamera(35, window.innerWidth / window.innerHeight, 0.1, 10000);
    this.camera.position.set(0, 0, 6);

    // No scene lights — the Xbox dashboard uses MeshBasicMaterial with vertex
    // colors and textures. Lighting is baked into the assets.

    // Runtime state
    this.config = null;
    this.meshManifest = {};
    this.meshCache = {};       // key → Promise<THREE.BufferGeometry>
    this.textureCache = {};    // name → THREE.Texture
    this.derived = null;       // DerivedModel interpreting the executable's draw paths
    this.pipeline = null;      // DevicePipeline mapping device state onto materials
    this.device = null;        // D3D device state persisting across draws
    this.proceduralTextures = {}; // 'texture:VA' → THREE.Texture
    this.drawTarget = null;    // shape being drawn by the derived path this instant
    this.sceneData = {};       // sceneName → parsed JSON
    this.nodes = {};           // global DEF name → XAPNode
    this._viewpointSet = false;   // only apply the first Viewpoint to camera
    this._backgroundSet = false;  // only apply the first Background to scene
    this.rootGroup = new THREE.Group();
    this.scene.add(this.rootGroup);

    // Script engine state
    this.scriptScope = {};     // global scope for script execution
    this.pendingScripts = [];

    // Fonts
    this.fonts = {}; // fontName → parsed font JSON

    // Audio
    this.audioCtx = null;
    this.audioBuffers = {};

    // Input
    this.activeJoysticks = []; // bound joystick nodes
    this.prevButtons = {};

    // Animation
    this.animations = [];      // active animations [{target, prop, from, to, duration, elapsed}]
    this.clock = new THREE.Clock();

    // Levels
    this.levels = {};          // DEF name → { node, loaded, active }
    this.activeLevel = null;

    window.addEventListener('resize', () => this.onResize());
    window.addEventListener('keydown', (e) => this.onKeyDown(e));
    window.addEventListener('keyup', (e) => this.onKeyUp(e));

    // Create AudioContext eagerly (starts suspended until user gesture).
    // Play() calls during init will buffer; they start when the context resumes.
    try { this.audioCtx = new AudioContext(); } catch (_) {}
    const resumeAudio = () => {
      if (this.audioCtx && this.audioCtx.state === 'suspended') {
        this.audioCtx.resume();
      }
    };
    for (const evt of ['click', 'keydown', 'touchstart']) {
      window.addEventListener(evt, resumeAudio, { once: false });
    }

  }

  async start() {
    try {
      // Load config
      const configResp = await fetch(`${DATA_BASE}/config.json`);
      this.config = await configResp.json();

      // Load mesh manifest
      const manifestResp = await fetch(`${DATA_BASE}/meshes/manifest.json`);
      this.meshManifest = await manifestResp.json();
      _log(`[XAP] ${Object.keys(this.meshManifest).length} meshes in manifest`);

      // Load the render model derived from the executable
      const renderResp = await fetch(`${DATA_BASE}/render.json`);
      const renderModel = await renderResp.json();
      this.derived = new DerivedModel(renderModel);
      this.pipeline = new DevicePipeline(THREE, renderModel, { resolve: (h) => this.resolveTextureHandle(h) });
      this.device = cloneState(this.derived.defaults);
      this.derived.onDraw = (state) => this.onDerivedDraw(state);
      // Text geometry is drawn by the vector font renderer with the state the appearance leaves.
      this.derived.hook(this.derived.drawSlotFunction('Text'), (obj, args, state) => { this.onDerivedTextDraw(state); return 0; });
      for (const t of renderModel.textures) {
        const key = 'texture:' + (t.va >>> 0).toString(16).toUpperCase().padStart(8, '0');
        this.proceduralTextures[key] = this.loadTexture(`proc_${key.slice(8)}.png`);
      }
      _log(`[XBE] ${renderModel.materials.length} materials, ${Object.keys(renderModel.programs).length} vertex programs, ${renderModel.textures.length} procedural textures`);

      // Load XTF vector fonts
      await this.loadFonts();

      // Load ALL scene data FIRST (Level/Inline nodes need sub-scene data during build)
      const loadPromises = this.config.scenes.map(name => this.loadSceneData(name));
      await Promise.allSettled(loadPromises);
      _log(`[XAP] ${Object.keys(this.sceneData).length} scenes loaded`);

      // Now build the entry scene (sub-scenes will be available for Level/Inline)
      await this.loadScene(this.config.entry);
      _log(`[XAP] ${Object.keys(this.nodes).length} nodes registered`);

      // Execute all pending scripts
      this.executeScripts();

      // Auto-call initialize() — Xbox runtime calls this after scene loads
      if (typeof window.initialize === 'function') {
        try {
          window.initialize();
          _log('[XAP] initialize() completed');
        } catch (e) {
          _warn('[XAP] initialize() error:', e.message);
        }
      }

      document.getElementById('loading').classList.add('hidden');
      this.animate();
    } catch (err) {
      _error('Runtime start failed:', err);
      document.getElementById('loading').textContent = 'Error: ' + err.message;
    }
  }

  async loadFonts() {
    const fontFiles = [
      { file: 'Xbox', role: 'heading' },
      { file: 'XBox_Book', role: 'body' },
    ];
    for (const { file, role } of fontFiles) {
      try {
        const resp = await fetch(`${DATA_BASE}/fonts/${file}.json`);
        if (!resp.ok) continue;
        const fontData = await resp.json();
        this.fonts[role] = fontData;
        _log(`[XTF] Loaded font "${fontData.name}" as ${role}`);
      } catch (e) {
        _warn(`[XTF] Failed to load font ${file}: ${e.message}`);
      }
    }
  }

  async loadSceneData(name) {
    if (this.sceneData[name]) return this.sceneData[name];
    const resp = await fetch(`${DATA_BASE}/scenes/${name}.json`);
    const data = await resp.json();
    this.sceneData[name] = data;
    return data;
  }

  async loadScene(name) {
    const data = await this.loadSceneData(name);
    const archiveName = name.split('_').slice(0, -1).join('_');
    this.buildScene(data, this.rootGroup, archiveName);
  }

  buildScene(data, parent, archiveName, parentNode) {
    for (const item of data.items) {
      if (item.kind === 'node' && item.node) {
        const childNode = this.buildNode(item.node, parent, archiveName);
        // Register as XAP child of the parent node (for children[] traversal)
        if (childNode && parentNode) {
          parentNode._xapChildren.push(childNode);
        }
      } else if (item.kind === 'script') {
        this.pendingScripts.push(item.script);
      }
    }
  }

  // ========================================================================
  // Node builder — FIELD-DRIVEN, no type switches. Every node is a Group.
  // Its behavior is determined solely by what fields it has.
  // ========================================================================

  buildNode(data, parent, archiveName) {
    const node = new XAPNode(data, this, archiveName);
    const f = data.fields || {};
    const group = new THREE.Group();
    group.name = data.def || data.type;
    node.threeObj = group;

    // Register DEF name globally
    if (data.def) {
      this.nodes[data.def] = node;
      this.scriptScope[data.def] = node.proxy;
    }

    // --- Transform fields (any node can have these) ---
    if (f.translation) {
      const t = asVec3(f.translation);
      group.position.set(t[0], t[1], t[2]);
    }
    if (f.rotation) {
      const r = asVec4(f.rotation);
      const axis = new THREE.Vector3(r[0], r[1], r[2]).normalize();
      group.quaternion.setFromAxisAngle(axis, r[3]);
    }
    if (f.scale) {
      const s = asVec3(f.scale);
      group.scale.set(s[0], s[1], s[2]);
    }
    if (f.scaleOrientation) {
      node._scaleOrientation = asVec4(f.scaleOrientation);
    }
    if (f.visible !== undefined) {
      group.visible = !!f.visible;
    }
    if (f.fade !== undefined) {
      node._fadeDuration = asNumber(f.fade);
    }

    // --- Geometry: if node has appearance/geometry fields, build a mesh ---
    const geomData = extractNode(f.geometry);
    const appData = extractNode(f.appearance);
    if (geomData) {
      this.buildGeometry(node, group, geomData, appData, archiveName);
    }

    // --- Viewpoint: store data on node, apply first one to camera ---
    if (f.fieldOfView !== undefined) {
      node._viewpoint = {
        fov: asNumber(f.fieldOfView),
        position: f.position ? asVec3(f.position) : null,
        orientation: f.orientation ? asVec4(f.orientation) : null,
      };
      // Only apply the FIRST Viewpoint to the camera during scene build.
      // Subsequent Viewpoints are stored on nodes for script-driven navigation
      // (GoTo/GoBackTo calls applyViewpoint on the target level).
      if (!this._viewpointSet) {
        this._viewpointSet = true;
        this._applyViewpoint(node._viewpoint);
      }
    }

    // --- Background: store on node, apply only the first ---
    // The Xbox dashboard has multiple Background nodes (4:3, 16:9 variants).
    // Scripts switch between them. Apply only the first one initially.
    if (f.skyColor || f.backdrop) {
      node._background = { skyColor: f.skyColor, backdrop: extractNode(f.backdrop) };
      if (!this._backgroundSet) {
        this._backgroundSet = true;
        if (f.skyColor) {
          const c = asVec3(f.skyColor);
          this.scene.background = new THREE.Color(c[0], c[1], c[2]);
        }
        const backdropData = extractNode(f.backdrop);
        if (backdropData) {
          const url = getField(backdropData, 'url');
          if (url) this.scene.background = this.loadTexture(url);
        }
      }
    }

    // --- Screen dimensions ---
    if (f.width !== undefined && f.height !== undefined && data.type === 'Screen') {
      node._screenWidth = asNumber(f.width);
      node._screenHeight = asNumber(f.height);
    }

    // --- Text: if node has font field, render text ---
    // (All dashboard text lives inside Shape geometry with an Appearance parent,
    // so buildGeometry handles it. This path handles any bare Text node that has
    // appearance data on a parent.)
    if (f.font !== undefined) {
      const text = f.text || f.string || '';
      const fontName = f.font || 'Body';
      const justify = f.justify || 'left';
      const tr = new TextRenderer(String(text), String(fontName), this.fonts, this.pipeline.createMaterial(), String(justify));
      group.add(tr.mesh);
      node._textRenderer = tr;
    }

    // --- Audio: if node has url pointing to audio ---
    if (f.url !== undefined && typeof f.url === 'string' && f.url.match(/\.wav$/i)) {
      node._audioUrl = f.url;
      node._audioLoop = !!f.loop;
      node._audioVolume = asNumber(f.volume !== undefined ? f.volume : 1);
      node._audioPan = asNumber(f.pan || 0);
      node._audioFade = asNumber(f.fade || 0);
      node._isPlaying = false;
    }

    // --- Periodic audio: play random child AudioClip on interval ---
    if (f.period !== undefined) {
      node._period = asNumber(f.period);
      node._periodNoise = asNumber(f.periodNoise || 0);
      const rt = this;
      const scheduleNext = () => {
        const delay = (node._period + Math.random() * node._periodNoise) * 1000;
        setTimeout(() => {
          const children = node._getChildProxies();
          const len = typeof children.length === 'function' ? children.length() : children.length;
          if (len > 0) {
            const idx = Math.floor(Math.random() * len);
            const child = children[idx];
            if (child && typeof child.Play === 'function') child.Play();
          }
          scheduleNext();
        }, delay);
      };
      // Start periodic audio after a brief delay
      setTimeout(scheduleNext, node._period * 1000);
    }

    // --- Waver: rotational oscillation ---
    if (f.rpm !== undefined) {
      node._waverRPM = asNumber(f.rpm);
      node._waverField = asNumber(f.field || 0.5);
      this.animations.push({
        type: 'waver', node, group,
        rpm: node._waverRPM, field: node._waverField,
        elapsed: Math.random() * 100, // randomize phase
      });
    }

    // --- Layer: starts hidden, activated by scripts ---
    if (data.type === 'Layer') {
      group.visible = false;
    }

    // --- Level: register as navigable level ---
    // The Level's archive field tells which XIP the content came from.
    // The actual content is loaded via the Level's Inline child node, not here.
    if (f.archive !== undefined) {
      node._archive = String(f.archive);
      node._levelActive = false;
      const archiveBase = node._archive.replace(/\.xip$/i, '');
      node._sceneName = this.resolveArchive(archiveBase);
      if (data.def) {
        this.levels[data.def] = node;
        // All levels start hidden — initialize() calls GoTo() on the correct one
        group.visible = false;
        node._levelActive = false;
      }
    }

    // --- Inline: if node has url pointing to .xap ---
    if (f.url !== undefined && typeof f.url === 'string' && f.url.match(/\.xap$/i)) {
      node._inlineUrl = f.url;
      const sceneName = this.resolveInlineUrl(String(f.url));
      node._sceneName = sceneName;
      if (sceneName && this.sceneData[sceneName]) {
        this.buildScene(this.sceneData[sceneName], group, extractArchivePrefix(sceneName), node);
      }
    }

    // --- Joystick event handlers: extract from node scripts ---
    if (data.scripts && data.scripts.length > 0) {
      node._eventHandlers = {};
      for (const script of data.scripts) {
        const match = script.match(/function\s+(\w+)\s*\(/);
        if (match) {
          node._eventHandlers[match[1]] = script;
        }
      }
    }

    // --- Build ALL node-valued fields as children (shell, path, tunnel, control, frame, etc.) ---
    for (const [key, value] of Object.entries(f)) {
      if (key === 'geometry' || key === 'appearance') continue; // handled by buildGeometry
      const nodeData = extractNode(value);
      if (nodeData) {
        const childNode = this.buildNode(nodeData, group, archiveName);
        // Capture control node (Joystick) for Level input binding
        if (key === 'control' && childNode) {
          node._controlNode = childNode;
        }
      }
    }

    // --- Sphere geometry (standalone, not inside Shape) ---
    if (f.radius !== undefined && !geomData) {
      const sphere = new THREE.Mesh(
        new THREE.SphereGeometry(asNumber(f.radius), 32, 16),
        new THREE.MeshBasicMaterial({ side: THREE.DoubleSide })
      );
      group.add(sphere);
      node._mesh = sphere;
    }

    // Add to parent
    parent.add(group);

    // Build children — these are the XAP children[] array items
    if (data.children) {
      for (const childData of data.children) {
        if (childData) {
          const childNode = this.buildNode(childData, group, archiveName);
          if (childNode) {
            node._xapChildren.push(childNode);
          }
        }
      }
    }

    // Store node reference on Three.js object
    group.userData.xapNode = node;
    return node;
  }

  // Build geometry from a node's geometry/appearance field pair
  buildGeometry(node, group, geomData, appData, archiveName) {
    const geomFields = geomData.fields || {};
    const geomNode = this.nodeFor(geomData, archiveName);
    if (appData) this.prepareAppearance(appData, archiveName);
    const material = this.pipeline.createMaterial();

    // Mesh reference (url field pointing to .xm)
    if (geomFields.url && typeof geomFields.url === 'string' && geomFields.url.match(/\.xm$/i)) {
      this.loadMesh(geomFields.url, archiveName).then(geometry => {
        if (!geometry) return;
        const mesh = new THREE.Mesh(geometry, material);
        mesh.name = geomData.def || geomFields.url;
        group.add(mesh);
        node._mesh = mesh;
        const entry = geometry.userData.entry;
        this.attachBuffer(geomNode, { format: entry.format, stride: entry.stride, primitiveCount: entry.indexCount / 3 });
        node._drawMesh = mesh;
        node._drawGeometry = geometry.userData.desc;
        this._meshesLoaded = (this._meshesLoaded || 0) + 1;
        if (this._meshesLoaded % 100 === 0) {
          _log(`[XAP] ${this._meshesLoaded} meshes loaded`);
        }
      }).catch(e => {
        _warn(`[XAP] Mesh load failed: ${geomFields.url}`, e.message);
      });
      return;
    }

    // Text geometry (has font field)
    if (geomFields.font !== undefined) {
      const text = geomFields.text || geomFields.string || '';
      const fontName = geomFields.font || 'Body';
      const justify = geomFields.justify || 'left';
      const tr = new TextRenderer(String(text), String(fontName), this.fonts, material, String(justify));
      group.add(tr.mesh);
      node._textRenderer = tr;
      node._drawMesh = tr.mesh;
      node._drawGeometry = TEXT_GEOMETRY;
      return;
    }

    // Sphere geometry (has radius field): the executable builds it with its own buffer layout
    if (geomFields.radius !== undefined) {
      const geometry = this.sphereGeometry(asNumber(geomFields.radius), geomFields.slices, geomFields.stacks);
      const sphere = new THREE.Mesh(geometry, material);
      group.add(sphere);
      node._mesh = sphere;
      const sb = this.derived.draw.sphere;
      this.attachBuffer(geomNode, { format: sb.format, stride: sb.stride, primitiveCount: geometry.index.count / 3 });
      node._drawMesh = sphere;
      node._drawGeometry = geometry.userData.desc;
      return;
    }
    throw new Error(`geometry node ${geomData.type} has neither a mesh url, text nor a radius`);
  }

  // Gives a geometry node the loaded buffer object its draw path dereferences
  attachBuffer(geomNode, geometry) {
    geomNode.derivedObject().slots.set(this.derived.accessor.offset, { obj: makeBuffer(this.derived.model, geometry) });
  }

  // Builds a sphere in the layout the executable stores: position, packed normal and a zero diffuse
  sphereGeometry(radius, slices, stacks) {
    const sb = this.derived.draw.sphere;
    const layout = this.derived.model.fixed_function.find((ff) => ff.format === sb.format);
    if (!layout) throw new Error(`sphere format 0x${sb.format.toString(16)} has no fixed function declaration`);
    const decl = this.derived.model.declarations[(layout.declaration >>> 0).toString(16).toUpperCase().padStart(8, '0')];
    const src = new THREE.SphereGeometry(radius, slices !== undefined ? asNumber(slices) : 32, stacks !== undefined ? asNumber(stacks) : 16);
    const count = src.attributes.position.count;
    const geometry = new THREE.BufferGeometry();
    const attributes = [];
    for (const el of decl.elements) {
      const semantic = this.derived.model.vertex_semantics[String(el.register)];
      let data;
      if (semantic === 'position') data = src.attributes.position.array;
      else if (semantic === 'normal') data = src.attributes.normal.array;
      // D3DX fills the vertex components the source mesh lacks with zero when it clones it.
      else if (semantic === 'diffuse') data = new Float32Array(count * el.comps);
      else throw new Error(`sphere declaration element ${semantic} is not produced by the runtime`);
      geometry.setAttribute('a_' + el.offset, new THREE.BufferAttribute(data, el.comps));
      attributes.push({ register: el.register, semantic, type: el.type, offset: el.offset, comps: el.comps });
    }
    geometry.setIndex(src.index);
    geometry.userData.desc = describeGeometry({ format: sb.format, stride: sb.stride, attributes });
    return geometry;
  }

  // Registers the appearance, material and texture nodes a shape draws through
  prepareAppearance(appData, archiveName) {
    const appNode = this.nodeFor(appData, archiveName);
    const matData = extractNode(getFieldRaw(appData, 'material'));
    if (matData) this.nodeFor(matData, archiveName);
    const texData = extractNode(getFieldRaw(appData, 'texture'));
    if (texData) {
      const texNode = this.nodeFor(texData, archiveName);
      const url = getField(texData, 'url');
      if (url) {
        const tex = this.loadTexture(url);
        texNode.derivedObject().slots.set(this.derived.draw.texture_field, { xapTexture: tex });
      }
    }
    return appNode;
  }

  // Returns the runtime node for a scene node object, creating and registering it once
  nodeFor(data, archiveName) {
    if (data._xapNode) return data._xapNode;
    const node = new XAPNode(data, this, archiveName || '');
    data._xapNode = node;
    if (data.def) {
      this.nodes[data.def] = node;
      this.scriptScope[data.def] = node.proxy;
    }
    return node;
  }

  // Resolves a texture handle the derived paths bound at a stage
  resolveTextureHandle(handle) {
    if (handle && handle.texture !== undefined) {
      const tex = this.proceduralTextures[handle.texture];
      if (!tex) throw new Error(`procedural ${handle.texture} was not exported`);
      return tex;
    }
    if (handle && handle.xapTexture !== undefined) return handle.xapTexture;
    throw new Error(`texture handle ${JSON.stringify(handle)} has no runtime texture`);
  }

  // Applies the device state at an indexed draw to the shape being drawn
  onDerivedDraw(state) {
    const t = this.drawTarget;
    if (!t) throw new Error('derived draw without a shape');
    this.pipeline.update(t.node._drawMesh.material, state, t.node._drawGeometry, t.matrices);
  }

  onDerivedTextDraw(state) {
    const t = this.drawTarget;
    if (!t) throw new Error('derived text draw without a shape');
    this.pipeline.update(t.node._textRenderer.material, state, TEXT_GEOMETRY, t.matrices, true);
  }

  // Runs the executable's draw paths over the scene in draw order, updating every mesh material
  updateDerivedDraws() {
    const d = this.derived;
    d.clock = this.clock.elapsedTime;
    this.scene.updateMatrixWorld(true);
    this.camera.updateMatrixWorld();
    const view = new Float32Array(this.camera.matrixWorldInverse.elements);
    const projection = new Float32Array(this.camera.projectionMatrix.elements);
    const alphaField = Object.entries(d.types.Transform.fields).find(([, f]) => f.name === 'alpha');
    if (!alphaField) throw new Error('Transform has no alpha field in the executable');
    const alphaOff = Number(alphaField[0]);
    const visit = (obj, alpha) => {
      if (!obj.visible) return;
      const node = obj.userData.xapNode;
      if (node && node.data.type === 'Transform') {
        alpha *= floatFromBits(d.readField(node.derivedObject(), alphaOff, 4));
      }
      if (node && node.data.type === 'Shape' && node._drawMesh) {
        const matrices = { world: new Float32Array(obj.matrixWorld.elements), view, projection };
        d.hierarchyAlpha = alpha;
        d.nodeSetup(this.device, matrices);
        this.drawTarget = { node, matrices };
        d.drawShape(node.derivedObject(), this.device, matrices);
        this.drawTarget = null;
      }
      for (const child of obj.children) visit(child, alpha);
    };
    visit(this.rootGroup, 1);
  }

  loadTexture(url) {
    let base = String(url);
    const slashIdx = Math.max(base.lastIndexOf('/'), base.lastIndexOf('\\'));
    if (slashIdx >= 0) base = base.substring(slashIdx + 1);
    const dotIdx = base.lastIndexOf('.');
    if (dotIdx >= 0) base = base.substring(0, dotIdx);

    if (this.textureCache[base]) return this.textureCache[base];

    const loader = new THREE.TextureLoader();
    const tex = loader.load(
      `${DATA_BASE}/textures/${base}.png`,
      (t) => { t.colorSpace = THREE.SRGBColorSpace; },
      undefined,
      () => { _warn(`[XAP] Texture not found: ${base}.png`); }
    );
    tex.wrapS = THREE.RepeatWrapping;
    tex.wrapT = THREE.RepeatWrapping;
    this.textureCache[base] = tex;
    return tex;
  }

  async loadMesh(url, archiveName) {
    const key = archiveName + ':' + url;
    if (this.meshCache[key]) return this.meshCache[key];

    const entry = this.meshManifest[key] || this.meshManifest[url];
    if (!entry) {
      for (const k of Object.keys(this.meshManifest)) {
        if (k.endsWith(':' + url)) return this.loadMeshFromEntry(k, this.meshManifest[k]);
      }
      return null;
    }
    return this.loadMeshFromEntry(key, entry);
  }

  async loadMeshFromEntry(key, entry) {
    if (this.meshCache[key]) return this.meshCache[key];

    const promise = (async () => {
      const resp = await fetch(`${DATA_BASE}/meshes/${entry.file}`);
      const buffer = await resp.arrayBuffer();
      const vc = entry.vertexCount, ic = entry.indexCount;

      // Attributes are stored in declaration order as float arrays, then the indices.
      let offset = 0;
      const geometry = new THREE.BufferGeometry();
      for (const a of entry.attributes) {
        const data = new Float32Array(buffer, offset, vc * a.comps);
        offset += vc * a.comps * 4;
        geometry.setAttribute('a_' + a.offset, new THREE.BufferAttribute(data, a.comps));
      }
      const indices = new Uint16Array(buffer, offset, ic);
      geometry.setIndex(new THREE.BufferAttribute(indices, 1));
      geometry.userData.entry = entry;
      geometry.userData.desc = describeGeometry(entry);
      return geometry;
    })();

    this.meshCache[key] = promise;
    return promise;
  }

  resolveArchive(archiveBase) {
    if (!this.config) return null;
    // Try exact match
    if (this.config.archives[archiveBase]) return this.config.archives[archiveBase];
    // Try lowercase
    const lower = archiveBase.toLowerCase();
    if (this.config.archives[lower]) return this.config.archives[lower];
    // Try all keys case-insensitive
    for (const [key, val] of Object.entries(this.config.archives)) {
      if (key.toLowerCase() === lower) return val;
    }
    return null;
  }

  resolveInline(url) {
    if (!this.config) return null;
    if (this.config.inlines[url]) return this.config.inlines[url];
    // Try case-insensitive
    const lower = url.toLowerCase();
    for (const [key, val] of Object.entries(this.config.inlines)) {
      if (key.toLowerCase() === lower) return val;
    }
    return null;
  }

  // Resolve Inline URL — handles both "scene.xap" and "ArchiveName/default.xap" patterns
  resolveInlineUrl(url) {
    // Try direct inline mapping first
    const inline = this.resolveInline(url);
    if (inline) return inline;

    // Handle "ArchiveName/default.xap" → resolve via archive mapping
    const parts = url.replace(/\\/g, '/').split('/');
    if (parts.length >= 2 && parts[parts.length - 1].toLowerCase() === 'default.xap') {
      const archiveName = parts[parts.length - 2];
      const archive = this.resolveArchive(archiveName);
      if (archive) return archive;
    }

    // Handle "ArchiveName/scene.xap" → try archive prefix + scene name
    if (parts.length >= 2) {
      const archiveName = parts[0];
      const sceneName = parts[parts.length - 1].replace(/\.xap$/i, '');
      // Try "archivename_scenename" pattern
      const candidate = archiveName.toLowerCase() + '_' + sceneName.toLowerCase();
      for (const name of (this.config.scenes || [])) {
        if (name.toLowerCase() === candidate) return name;
      }
    }

    return null;
  }

  // ========================================================================
  // Script Engine — XAP scripts are JS. We set DEF'd nodes on window
  // and use indirect eval so function/var declarations go to global scope.
  // ========================================================================

  executeScripts() {
    // Expose all DEF'd node proxies as window globals
    for (const [name, node] of Object.entries(this.nodes)) {
      window[name] = node.proxy;
    }

    // Inject system API stubs on typed nodes
    this._injectSystemMethods();

    // Provide stub globals for system APIs that don't exist in browser
    window.log = (...args) => _log('[XAP script]', ...args);
    window.EnableInput = (b) => { _log('[XAP] EnableInput:', b); };
    window.BlockMemoryUnitInsert = () => {};
    window.UnblockMemoryUnitInsert = () => {};
    window.BlockMemoryUnitEnumeration = () => {};
    window.UnblockMemoryUnitEnumeration = () => {};
    window.BlockUser = (msg) => { _log('[XAP] BlockUser:', msg); };
    window.TellUser = (msg, fn) => { _log('[XAP] TellUser:', msg); };
    window.AskUser = (msg, yesFn, noFn) => { _log('[XAP] AskUser:', msg); };

    // Keep console accessible — do NOT lock it down, we need logs for debugging.

    // Bind the first Joystick with event handlers so input works
    for (const node of Object.values(this.nodes)) {
      if (node._eventHandlers && Object.keys(node._eventHandlers).length > 0) {
        node._isBound = true;
        _log(`[XAP] Auto-bound joystick: ${node.data.def || node.data.type}`);
        break;
      }
    }

    let ok = 0, fail = 0, skipped = 0;
    for (const script of this.pendingScripts) {
      const trimmed = script.trimStart();

      // Convert behavior blocks: replace sleep N; with async await
      if (trimmed.startsWith('behavior')) {
        const bodyStart = trimmed.indexOf('{');
        const bodyEnd = trimmed.lastIndexOf('}');
        if (bodyStart >= 0 && bodyEnd > bodyStart) {
          let body = trimmed.substring(bodyStart + 1, bodyEnd);
          body = body.replace(/sleep\s+([\d.]+)\s*;/g,
            'await new Promise(r => setTimeout(r, $1 * 1000));');
          try {
            (0, eval)(`(async () => { ${body} })()`);
            ok++;
          } catch (e) {
            fail++;
            _warn('Behavior error:', e.message);
          }
        } else {
          skipped++;
        }
        continue;
      }

      try {
        (0, eval)(script);
        ok++;
      } catch (e) {
        fail++;
        _warn('Script error:', e.message, '\n', script.substring(0, 200));
      }
    }
    _log(`[XAP] Scripts: ${ok} ok, ${fail} failed, ${skipped} skipped (of ${this.pendingScripts.length})`);
  }

  // Inject system API methods on nodes based on their type
  _injectSystemMethods() {
    const systemMethods = {
      'Config': {
        GetLanguage: () => 1,
        SetLanguage: (n) => { _log('[XAP] Config.SetLanguage:', n); },
        GetVideoMode: () => 0,
        GetAudioMode: () => 0,
        SetVideoMode: (n) => {},
        SetAudioMode: (n) => {},
        GetLaunchReason: () => '',
        GetLaunchParameter1: () => 0,
        GetGameRegion: () => 1,
        ForceSetLanguage: () => false,
        ForceSetTimeZone: () => false,
        ForceSetClock: () => false,
        CanDriveBeCleanup: (n) => false,
        BackToLauncher: () => { _log('[XAP] Config.BackToLauncher'); },
        BackToLauncher2: () => { _log('[XAP] Config.BackToLauncher2'); },
        SetMusicVolume: (n) => {},
        GetMusicVolume: () => 100,
        GetTimeZone: () => 0,
        SetTimeZone: (n) => {},
        GetDaylightSavings: () => false,
        SetDaylightSavings: (b) => {},
        Get24Hour: () => false,
        Set24Hour: (b) => {},
        GetDolbyDigital: () => false,
        SetDolbyDigital: (b) => {},
        GetDTS: () => false,
        SetDTS: (b) => {},
        GetPAL60: () => false,
        SetPAL60: (b) => {},
        Get480p: () => false,
        Set480p: (b) => {},
        Get720p: () => false,
        Set720p: (b) => {},
        Get1080i: () => false,
        Set1080i: (b) => {},
        GetWidescreen: () => false,
        SetWidescreen: (b) => {},
        GetLetterbox: () => false,
        SetLetterbox: (b) => {},
        GetMoviePCFlags: () => 0,
        SetMoviePCFlags: (n) => {},
        GetMovieRating: () => 0,
        SetMovieRating: (n) => {},
        GetMachineAccount: () => '',
        SetMachineAccount: (s) => {},
      },
      'Translator': {
        SetLanguage: (n) => {},
        Translate: (key) => String(key),
        GetDateSeparator: () => '/',
        FormatNumber: (n) => String(n),
        FormatDate: (y, m, d) => `${m}/${d}/${y}`,
        FormatTime: (h, m) => `${h}:${String(m).padStart(2, '0')}`,
      },
      'DiscDrive': {
        LaunchDisc: () => { _log('[XAP] DiscDrive.LaunchDisc'); },
      },
      'MemoryMonitor': {
        HaveDeviceTop: (n) => 0,
        HaveDeviceBottom: (n) => 0,
        FormatDeviceName: (n) => '',
        GetDeviceSize: (n) => 0,
        GetDeviceFreeSize: (n) => 0,
      },
      'Recovery': {
        GetRecoveryKey: () => '',
      },
    };

    // System node properties (readable/writable)
    const systemProperties = {
      'DiscDrive': { discType: 'none', locked: false },
      'MemoryMonitor': { blockInsertion: false, enumerationOn: false },
    };

    for (const node of Object.values(this.nodes)) {
      const type = node.data.type;
      if (systemMethods[type]) {
        node._systemMethods = systemMethods[type];
      }
      if (systemProperties[type]) {
        for (const [k, v] of Object.entries(systemProperties[type])) {
          if (node._fields[k] === undefined) {
            node._fields[k] = v;
          }
        }
      }
    }
  }

  // ========================================================================
  // Input System (Keyboard → Joystick events)
  // ========================================================================

  onKeyDown(e) {
    if (e.repeat) return;

    const mapping = {
      'Enter': 'OnADown',
      'Escape': 'OnBDown',
      'KeyX': 'OnXDown',
      'KeyY': 'OnYDown',
      'ArrowUp': 'OnMoveUp',
      'ArrowDown': 'OnMoveDown',
      'ArrowLeft': 'OnMoveLeft',
      'ArrowRight': 'OnMoveRight',
      'KeyW': 'OnMoveUp',
      'KeyS': 'OnMoveDown',
      'KeyA': 'OnMoveLeft',
      'KeyD': 'OnMoveRight',
    };

    const event = mapping[e.code];
    if (event) {
      e.preventDefault();
      this.dispatchJoystickEvent(event);
    }
  }

  onKeyUp(e) {
    const mapping = {
      'Enter': 'OnAUp',
      'Escape': 'OnBUp',
    };

    const event = mapping[e.code];
    if (event) {
      this.dispatchJoystickEvent(event);
    }
  }

  dispatchJoystickEvent(eventName) {
    // Find all bound joystick nodes and call their event handler
    for (const node of Object.values(this.nodes)) {
      if (node._eventHandlers && node._isBound) {
        const handler = node._eventHandlers[eventName];
        if (handler) {
          try {
            (0, eval)(handler);
            // The handler defines a function — now call it
            if (typeof window[eventName] === 'function') {
              window[eventName]();
            }
          } catch (e) {
            _warn(`Joystick ${eventName}:`, e.message);
          }
        }
      }
    }
  }

  pollGamepad() {
    const gamepads = navigator.getGamepads ? navigator.getGamepads() : [];
    for (const gp of gamepads) {
      if (!gp) continue;

      const id = gp.index;
      const prev = this.prevButtons[id] || [];

      // Button mapping (Xbox layout)
      const buttonMap = [
        'OnADown', 'OnBDown', 'OnXDown', 'OnYDown',
        'OnLeftDown', 'OnRightDown', null, null,
        null, null, 'OnLeftThumbDown', null,
        'OnMoveUp', 'OnMoveDown', 'OnMoveLeft', 'OnMoveRight',
      ];

      for (let i = 0; i < gp.buttons.length && i < buttonMap.length; i++) {
        const pressed = gp.buttons[i].pressed;
        const wasPressed = prev[i] || false;
        if (pressed && !wasPressed && buttonMap[i]) {
          this.dispatchJoystickEvent(buttonMap[i]);
        }
      }

      // D-pad from axes
      const deadzone = 0.5;
      if (gp.axes.length >= 2) {
        const ax = gp.axes[0], ay = gp.axes[1];
        if (ax < -deadzone) this.dispatchJoystickEvent('OnMoveLeft');
        if (ax > deadzone) this.dispatchJoystickEvent('OnMoveRight');
        if (ay < -deadzone) this.dispatchJoystickEvent('OnMoveUp');
        if (ay > deadzone) this.dispatchJoystickEvent('OnMoveDown');
      }

      this.prevButtons[id] = gp.buttons.map(b => b.pressed);
    }
  }

  // ========================================================================
  // Animation System
  // ========================================================================

  updateAnimations(dt) {
    for (let i = this.animations.length - 1; i >= 0; i--) {
      const anim = this.animations[i];
      anim.elapsed += dt;

      // Waver: continuous sinusoidal Y-axis oscillation
      if (anim.type === 'waver') {
        const angle = Math.sin(anim.elapsed * anim.rpm * 2 * Math.PI / 60) * anim.field;
        anim.group.rotation.y = angle;
        continue; // wavers never finish
      }

      // Standard property interpolation
      const t = Math.min(anim.elapsed / anim.duration, 1);
      if (typeof anim.from === 'number') {
        anim.target[anim.prop] = anim.from + (anim.to - anim.from) * t;
      }

      if (t >= 1) {
        this.animations.splice(i, 1);
      }
    }
  }

  // ========================================================================
  // Render loop
  // ========================================================================

  animate() {
    requestAnimationFrame(() => this.animate());
    const dt = this.clock.getDelta();

    this.updateAnimations(dt);
    this.pollGamepad();
    this.updateDerivedDraws();

    this.renderer.render(this.scene, this.camera);
  }

  onResize() {
    this.camera.aspect = window.innerWidth / window.innerHeight;
    this.camera.updateProjectionMatrix();
    this.renderer.setSize(window.innerWidth, window.innerHeight);
  }

  _applyViewpoint(vp) {
    this.camera.fov = THREE.MathUtils.radToDeg(vp.fov);
    this.camera.updateProjectionMatrix();
    if (vp.position) {
      this.camera.position.set(vp.position[0], vp.position[1], vp.position[2]);
    }
    if (vp.orientation) {
      const axis = new THREE.Vector3(vp.orientation[0], vp.orientation[1], vp.orientation[2]).normalize();
      this.camera.quaternion.setFromAxisAngle(axis, vp.orientation[3]);
    }
    _log(`[XAP] Camera: fov=${vp.fov.toFixed(2)} pos=[${vp.position}]`);
  }
}

// ============================================================================
// XAPNode — Proxy-wrapped scene node for script compatibility
// ============================================================================

class XAPNode {
  constructor(data, runtime, archiveName) {
    this.data = data;
    this.runtime = runtime;
    this.archiveName = archiveName;
    this.threeObj = null;
    this._mesh = null;
    this._textRenderer = null;
    this._fadeDuration = 0; // transition duration in seconds, NOT opacity
    this._isBound = false;
    this._eventHandlers = null;
    this._audioUrl = '';
    this._audioLoop = false;
    this._audioVolume = 1;
    this._isPlaying = false;
    this._archive = '';
    this._sceneName = '';
    this._fields = {};
    this._systemMethods = null;
    this._onLoadFired = false;
    this._xapChildren = []; // ordered list of XAPNodes from children[] and buildScene
    this._drawMesh = null;
    this._drawGeometry = null;
    this._derivedObject = null;

    // Store original field values
    if (data.fields) {
      for (const [key, value] of Object.entries(data.fields)) {
        this._fields[key] = value;
      }
    }

    // Create Proxy for script compatibility
    this.proxy = new Proxy(this, {
      get: (target, prop) => {
        if (typeof prop === 'symbol') return target[prop];
        const s = String(prop);

        // Core properties
        if (s === 'children') return target._getChildProxies();
        if (s === 'visible') return target.threeObj ? target.threeObj.visible : true;
        if (s === 'text') return target._textRenderer ? target._textRenderer.text : (target._fields.text || '');
        if (s === 'string') return target._fields.string || '';
        if (s === 'isBound') return target._isBound;
        if (s === 'volume') return target._audioVolume;
        if (s === 'isActive') return target._isPlaying;
        if (s === 'moving') return false; // animation state
        if (s === 'fade') return target._fadeDuration;

        // Methods
        if (s === 'SetAlpha') return (v) => target._setAlpha(v);
        if (s === 'SetTranslation') return (x, y, z) => target._setTranslation(x, y, z);
        if (s === 'SetRotation') return (ax, ay, az, angle) => target._setRotation(ax, ay, az, angle);
        if (s === 'SetScale') return (x, y, z) => target._setScale(x, y, z);
        if (s === 'Play') return () => target._play();
        if (s === 'Stop') return () => target._stop();
        if (s === 'SetVolume') return (v) => target._setVolume(v);
        if (s === 'GoTo') return () => target._goTo();
        if (s === 'GoBackTo') return () => target._goBackTo();
        if (s === 'length') return target._getChildProxies().length;

        // Appearance / material chain
        if (s === 'appearance') return target._getAppearanceProxy();
        if (s === 'material') return target._getMaterialProxy();
        if (s === 'texture') return target._getTextureProxy();
        if (s === 'geometry') return target._getGeometryProxy();

        // System API methods (Config, Translator, DiscDrive, etc.)
        if (target._systemMethods && target._systemMethods[s]) {
          return target._systemMethods[s];
        }

        // Field access
        if (target._fields[s] !== undefined) return target._fields[s];

        // DEF'd child lookup
        const child = target._findChildByDef(s);
        if (child) return child.proxy;

        // Method on runtime scope
        if (typeof target.runtime.scriptScope[s] === 'function') {
          return target.runtime.scriptScope[s];
        }

        return undefined;
      },

      set: (target, prop, value) => {
        const s = String(prop);

        if (s === 'visible') {
          if (target.threeObj) target.threeObj.visible = !!value;
          // Fire onLoad callback for Inline nodes when first made visible
          if (!!value && !target._onLoadFired && target._eventHandlers) {
            const handler = target._eventHandlers['onLoad'];
            if (handler) {
              target._onLoadFired = true;
              setTimeout(() => {
                try {
                  (0, eval)(handler);
                  if (typeof window.onLoad === 'function') {
                    window.onLoad();
                    delete window.onLoad; // prevent cross-contamination
                  }
                } catch (e) {
                  _warn(`[XAP] onLoad error for ${target.data.def}:`, e.message);
                }
              }, 0);
            }
          }
          return true;
        }
        if (s === 'text') {
          if (target._textRenderer) target._textRenderer.setText(String(value));
          target._fields.text = value;
          return true;
        }
        if (s === 'string') {
          target._fields.string = value;
          return true;
        }
        if (s === 'isBound') {
          target._isBound = !!value;
          return true;
        }
        if (s === 'volume') {
          target._audioVolume = Number(value);
          if (target._gainNode) {
            target._gainNode.gain.value = Number(value);
          }
          return true;
        }
        if (s === 'fade') {
          target._fadeDuration = Number(value);
          return true;
        }
        if (s === 'name') {
          // The derived material lookup reads the field on the next draw.
          target._fields.name = String(value);
          return true;
        }
        if (s === 'param') {
          // Material animation parameter
          target._fields.param = Number(value);
          return true;
        }
        if (s === 'transparency') {
          target._fields.transparency = Number(value);
          return true;
        }

        // Generic field storage
        target._fields[s] = value;
        return true;
      }
    });
  }

  _getChildProxies() {
    // Return only XAP children (from children[] array and buildScene),
    // NOT field-child nodes (control, shell, path, etc.)
    const proxies = this._xapChildren.map(n => n.proxy);
    // XAP scripts use children.length() as a function call
    return new Proxy(proxies, {
      get(t, p) {
        if (p === 'length') {
          const len = t.length;
          const fn = () => len;
          fn.valueOf = () => len;
          fn[Symbol.toPrimitive] = () => len;
          return fn;
        }
        return t[p];
      }
    });
  }

  _findChildByDef(name) {
    // Search XAP children and all descendants
    const searchNode = (xapNode) => {
      if (xapNode.data.def === name) return xapNode;
      for (const child of xapNode._xapChildren) {
        const found = searchNode(child);
        if (found) return found;
      }
      return null;
    };

    for (const child of this._xapChildren) {
      const found = searchNode(child);
      if (found) return found;
    }

    // Also check global DEF registry as fallback
    if (this.runtime.nodes[name]) return this.runtime.nodes[name];

    return null;
  }

  // Scripts reach the appearance, material and texture nodes a shape draws through
  _fieldNodeProxy(name) {
    const data = extractNode(this._fields[name]);
    return data ? this.runtime.nodeFor(data, this.archiveName).proxy : undefined;
  }

  _getAppearanceProxy() { return this._fieldNodeProxy('appearance'); }
  _getMaterialProxy() { return this._fieldNodeProxy('material'); }
  _getTextureProxy() { return this._fieldNodeProxy('texture'); }
  _getGeometryProxy() { return this._fieldNodeProxy('geometry'); }

  // The transform's alpha field scales the hierarchy alpha the materials read
  _setAlpha(value) {
    this._fields.alpha = Number(value);
  }

  // The executable's object for this node, read by the derived draw paths
  derivedObject() {
    if (!this._derivedObject) this._derivedObject = this.runtime.derived.newObject(this.data.type, this);
    return this._derivedObject;
  }

  // Reads a scene field the way the executable's field table types it
  derivedField(name, type) {
    const v = this._fields[name];
    if (v === undefined) return undefined;
    if (type === 9) {
      const data = extractNode(v);
      return data ? { obj: this.runtime.nodeFor(data, this.archiveName).derivedObject() } : 0;
    }
    if (type === 10) return String(Array.isArray(v) ? v[0] : v);
    if (type === 2) return !!v;
    return asNumber(v);
  }

  _setTranslation(x, y, z) {
    if (this.threeObj) {
      this.threeObj.position.set(Number(x), Number(y), Number(z));
    }
  }

  _setRotation(ax, ay, az, angle) {
    if (this.threeObj) {
      const axis = new THREE.Vector3(Number(ax), Number(ay), Number(az)).normalize();
      this.threeObj.quaternion.setFromAxisAngle(axis, Number(angle));
    }
  }

  _setScale(x, y, z) {
    if (this.threeObj) {
      this.threeObj.scale.set(Number(x), Number(y), Number(z));
    }
  }

  _play() {
    if (!this._audioUrl) {
      this._isPlaying = true;
      return;
    }
    this._isPlaying = true;

    const rt = this.runtime;
    if (!rt.audioCtx) return;

    const url = `${DATA_BASE}/${this._audioUrl}`;
    const cacheKey = this._audioUrl;
    const self = this;

    (async () => {
      try {
        if (!rt.audioBuffers[cacheKey]) {
          const resp = await fetch(url);
          if (!resp.ok) return; // audio file not available
          const arrayBuf = await resp.arrayBuffer();
          rt.audioBuffers[cacheKey] = await rt.audioCtx.decodeAudioData(arrayBuf);
        }

        // Stop previous source if still playing
        if (self._source) {
          try { self._source.stop(); } catch (_) {}
        }

        const source = rt.audioCtx.createBufferSource();
        source.buffer = rt.audioBuffers[cacheKey];
        source.loop = self._audioLoop;

        const gainNode = rt.audioCtx.createGain();
        gainNode.gain.value = self._audioVolume;

        source.connect(gainNode);
        gainNode.connect(rt.audioCtx.destination);
        source.start(0);

        self._source = source;
        self._gainNode = gainNode;
        source.onended = () => { self._isPlaying = false; };
      } catch (e) {
        // Audio not critical — don't crash on missing files
      }
    })();
  }

  _stop() {
    this._isPlaying = false;
    if (this._source) {
      try { this._source.stop(); } catch (_) {}
      this._source = null;
    }
  }

  _setVolume(v) {
    this._audioVolume = Number(v);
    if (this._gainNode) {
      this._gainNode.gain.value = this._audioVolume;
    }
  }

  _goTo() {
    // Hide all other levels, show this one
    for (const [name, lvl] of Object.entries(this.runtime.levels)) {
      if (lvl.threeObj) lvl.threeObj.visible = false;
      lvl._levelActive = false;
    }
    if (this.threeObj) this.threeObj.visible = true;
    this._levelActive = true;

    // Apply this level's Viewpoint to the camera
    this._applyLevelViewpoint();

    // Bind the joystick control for this level
    this._bindLevelControl();

    // Fire onLoad handlers on descendant nodes (Inline children etc.)
    this._fireDescendantOnLoad();

    _log(`[Level] GoTo: ${this.data.def}`);
  }

  _goBackTo() {
    // Return to this level from a sub-level
    for (const [name, lvl] of Object.entries(this.runtime.levels)) {
      if (lvl.threeObj) lvl.threeObj.visible = false;
      lvl._levelActive = false;
    }
    if (this.threeObj) this.threeObj.visible = true;
    this._levelActive = true;

    // Apply this level's Viewpoint to the camera
    this._applyLevelViewpoint();

    // Bind the joystick control for this level
    this._bindLevelControl();

    // Fire onLoad handlers on descendant nodes
    this._fireDescendantOnLoad();

    _log(`[Level] GoBackTo: ${this.data.def}`);
  }

  _applyLevelViewpoint() {
    // Find the first Viewpoint node in this level's subtree
    const findVP = (xapNode) => {
      if (xapNode._viewpoint) return xapNode._viewpoint;
      for (const child of xapNode._xapChildren) {
        const vp = findVP(child);
        if (vp) return vp;
      }
      return null;
    };
    const vp = findVP(this);
    if (vp) {
      this.runtime._applyViewpoint(vp);
    }
  }

  _fireDescendantOnLoad() {
    const fire = (xapNode) => {
      if (xapNode._eventHandlers && !xapNode._onLoadFired) {
        const handler = xapNode._eventHandlers['onLoad'];
        if (handler) {
          xapNode._onLoadFired = true;
          try {
            (0, eval)(handler);
            if (typeof window.onLoad === 'function') {
              window.onLoad();
              delete window.onLoad;
            }
          } catch (e) {
            _warn(`[XAP] onLoad error for ${xapNode.data.def}:`, e.message);
          }
        }
      }
      for (const child of xapNode._xapChildren) {
        fire(child);
      }
    };
    for (const child of this._xapChildren) {
      fire(child);
    }
  }

  _bindLevelControl() {
    // Unbind all joysticks, then bind this level's control joystick
    for (const node of Object.values(this.runtime.nodes)) {
      if (node._eventHandlers) node._isBound = false;
    }

    // Find control node (Joystick) for this level
    let ctrl = this._controlNode;

    // If this level has no explicit control, search descendants for a joystick
    if (!ctrl) {
      const findJoystick = (xapNode) => {
        if (xapNode._eventHandlers && Object.keys(xapNode._eventHandlers).some(
          k => k.startsWith('On') && k !== 'onLoad')) {
          return xapNode;
        }
        for (const child of xapNode._xapChildren) {
          const found = findJoystick(child);
          if (found) return found;
        }
        return null;
      };
      ctrl = findJoystick(this);
    }

    if (ctrl) {
      ctrl._isBound = true;
      _log(`[Level] Bound joystick: ${ctrl.data.def || 'unnamed'}`);
    } else {
      _warn(`[Level] No joystick found for ${this.data.def}`);
    }
  }
}

// ============================================================================
// Text Renderer — Canvas-based text in 3D space
// ============================================================================

class TextRenderer {
  constructor(text, fontName, fontCache, material, justify) {
    this.text = text;
    this.fontName = fontName;
    this.fontCache = fontCache;
    this.material = material;
    this.justify = justify;

    this.mesh = new THREE.Group();
    this.mesh.name = 'Text';

    this.rebuild();
  }

  setText(text) {
    this.text = text;
    this.rebuild();
  }

  rebuild() {
    // Clear previous geometry
    while (this.mesh.children.length > 0) {
      const c = this.mesh.children[0];
      this.mesh.remove(c);
      if (c.geometry) c.geometry.dispose();
    }

    const role = this.fontName.toLowerCase().includes('heading') ? 'heading' : 'body';
    const fontData = this.fontCache[role];
    if (!fontData || !this.text) return;

    // Merge all glyph triangles into a single geometry
    const allPositions = [];
    const allIndices = [];
    let cursorX = 0;
    let vertexOffset = 0;

    for (const ch of this.text) {
      const glyph = fontData.glyphs[ch];
      if (!glyph) {
        // Use space advance for unknown characters
        const space = fontData.glyphs[' '];
        cursorX += space ? space.adv : 0.2;
        continue;
      }

      if (glyph.verts && glyph.idx && glyph.idx.length > 0) {
        for (let i = 0; i < glyph.verts.length; i += 2) {
          allPositions.push(glyph.verts[i] + cursorX, glyph.verts[i + 1], 0);
        }
        for (let i = 0; i < glyph.idx.length; i++) {
          allIndices.push(glyph.idx[i] + vertexOffset);
        }
        vertexOffset += glyph.verts.length / 2;
      }

      cursorX += glyph.adv;
    }

    if (allPositions.length === 0) return;

    // Apply justify offset
    const totalWidth = cursorX;
    let xShift = 0;
    if (this.justify === 'middle' || this.justify === 'center') {
      xShift = -totalWidth / 2;
    }
    if (xShift !== 0) {
      for (let i = 0; i < allPositions.length; i += 3) {
        allPositions[i] += xShift;
      }
    }

    const geometry = new THREE.BufferGeometry();
    geometry.setAttribute('a_0', new THREE.Float32BufferAttribute(allPositions, 3));
    geometry.setIndex(allIndices);

    const textMesh = new THREE.Mesh(geometry, this.material);
    this.mesh.add(textMesh);
  }
}

// ============================================================================
// Helper functions
// ============================================================================

// Text glyph geometry carries positions only, drawn through the fixed function path
const TEXT_GEOMETRY = describeGeometry({ format: 0, stride: 12, attributes: [{ register: 0, semantic: 'position', type: 'FLOAT3', offset: 0, comps: 3 }] });

const floatBuf = new DataView(new ArrayBuffer(4));
function floatFromBits(bits) {
  floatBuf.setUint32(0, bits >>> 0, true);
  return floatBuf.getFloat32(0, true);
}

function asNumber(v) {
  if (typeof v === 'number') return v;
  if (Array.isArray(v) && v.length > 0) return Number(v[0]);
  return Number(v) || 0;
}

function asVec3(v) {
  if (Array.isArray(v)) {
    return [Number(v[0]) || 0, Number(v[1]) || 0, Number(v[2]) || 0];
  }
  return [0, 0, 0];
}

function asVec4(v) {
  if (Array.isArray(v)) {
    return [Number(v[0]) || 0, Number(v[1]) || 0, Number(v[2]) || 0, Number(v[3]) || 0];
  }
  return [0, 0, 0, 0];
}

// Extract an inline node from a field value (handles {_node: {...}} wrapper)
function extractNode(v) {
  if (!v) return null;
  if (typeof v === 'object' && v._node) return v._node;
  if (typeof v === 'object' && v.type) return v;
  return null;
}

// Get a field value from a node data object
function getField(nodeData, key) {
  if (!nodeData || !nodeData.fields) return undefined;
  const v = nodeData.fields[key];
  if (v === undefined) return undefined;
  if (typeof v === 'object' && v._node) return v._node;
  return v;
}

function getFieldRaw(nodeData, key) {
  if (!nodeData || !nodeData.fields) return undefined;
  return nodeData.fields[key];
}

function extractArchivePrefix(sceneName) {
  const idx = sceneName.lastIndexOf('_');
  return idx >= 0 ? sceneName.substring(0, idx) : sceneName;
}

// ============================================================================
// Boot
// ============================================================================

const runtime = new XAPRuntime();
window.xapRuntime = runtime;
runtime.start();
