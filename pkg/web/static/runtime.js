import * as THREE from 'three';
import { DerivedModel, cloneState, makeBuffer, matMul, SceneObject, VertexBuffer, MatrixStack, bitsFromFloat, TS_WORLD, ARCHIVE_TYPE, HEAP_TYPE } from './derived.js';
import { DevicePipeline, describeGeometry, describeFVF, primitiveIndices } from './pipeline.js';
import { VirtualConsole, nativeClasses, dateMembers, NATIVE_DRAW_CLASSES } from './system.js';

// ============================================================================
// XAP Runtime — the dashboard executable's frame loop run over its scene files
// ============================================================================

const DATA_BASE = 'data';

// Capture console before XAP scripts can clobber globals
const nativeConsole = console;
const _console = { log: console.log, warn: console.warn, error: console.error };
const _log = _console.log.bind(console);
const _warn = _console.warn.bind(console);
const _error = _console.error.bind(console);

// Scene file field types as the executable's field tables number them
const FIELD_INT = 1, FIELD_BOOL = 2, FIELD_NUMBER = 3, FIELD_VEC3 = 5, FIELD_ROTATION = 6, FIELD_COLOR = 8, FIELD_NODE = 9, FIELD_STRING = 10;
const HOOKED_FIELD_TYPES = new Set([FIELD_INT, FIELD_BOOL, FIELD_NUMBER, FIELD_VEC3, FIELD_ROTATION, FIELD_COLOR, FIELD_NODE, FIELD_STRING]);

// D3DPRESENT_PARAMETERS words the device init fills before creating the device
const PRESENT_WIDTH = 0, PRESENT_HEIGHT = 4;
// XGetVideoFlags bit for a widescreen display
const VIDEO_FLAG_WIDESCREEN = 1;
// Display shapes a console shows its frame in
const DISPLAY_ASPECT_NORMAL = 4 / 3, DISPLAY_ASPECT_WIDE = 16 / 9;
// D3DCLEAR flags for the color channels
const CLEAR_TARGET = 0xF0;

// The script scope object a loaded scene's post load step creates and fires initialize on
const SCOPE_TYPE = '$scope';
// Classes whose fields drive subsystems the runtime provides itself: audio voices, controller input, glyph layout
// and the console hardware the natives in system.js stand in for
const RUNTIME_FIELD_CLASSES = new Set(['AudioClip', 'Joystick', 'Text', 'Keyboard', 'DVDPlayer', 'DynamicTexture']);

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
    this.renderer.autoClear = false;
    // The executable's draw order is the render order.
    this.renderer.sortObjects = false;
    document.body.appendChild(this.renderer.domElement);

    this.scene = new THREE.Scene();
    // The derived shaders carry their own transforms, the camera only drives the render call.
    this.camera = new THREE.Camera();
    this.drawRoot = new THREE.Group();
    this.scene.add(this.drawRoot);

    // Runtime state
    this.config = null;
    this.meshManifest = {};
    this.meshCache = {};       // key → Promise<THREE.BufferGeometry>
    this.textureCache = {};    // name → THREE.Texture
    this.derived = null;       // DerivedModel interpreting the executable's paths
    this.pipeline = null;      // DevicePipeline mapping device state onto materials
    this.device = null;        // D3D device state persisting across draws
    this.proceduralTextures = {}; // 'texture:VA' → THREE.Texture
    this.sceneData = {};       // sceneName → parsed JSON
    this.nodes = {};           // global DEF name → XAPNode
    this.root = null;          // the scene object the frame draws
    this.frameDraws = [];      // meshes drawn this frame in order
    this.quadMeshes = new Map(); // vertex buffer → mesh drawn from it
    this.presentParams = 0;    // address of the present parameters the device was created with
    this.tickBase = 0;
    this.archives = new Map(); // archive path → loaded archive object
    this.buildOwners = [];     // inline nodes whose scenes are being built, innermost last

    // Script engine state
    this.scriptScope = {};     // global scope for script execution
    this.pendingScripts = [];  // [{scope, script}] in scene order
    this.scopes = [];          // ScriptScope per loaded scene, parents first
    this.console = null;       // VirtualConsole the natives read
    this.natives = null;       // class name -> native methods and properties
    this.inputEnabled = true;
    this.boundJoysticks = [];  // joysticks bound by scripts, the last bound receiving input
    this.running = true;

    // Fonts
    this.fonts = {}; // fontName → parsed font JSON

    // Audio
    this.audioCtx = null;
    this.audioBuffers = {};
    this.playingClips = new Set();
    this.screenSaver = null;
    this.textRenderers = [];

    // Input
    this.joysticks = [];       // every node with event handlers, DEF named or not
    this.prevButtons = {};

    this.clock = new THREE.Clock();

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
      const configResp = await fetch(`${DATA_BASE}/config.json`);
      this.config = await configResp.json();

      const manifestResp = await fetch(`${DATA_BASE}/meshes/manifest.json`);
      this.meshManifest = await manifestResp.json();
      _log(`[XAP] ${Object.keys(this.meshManifest).length} meshes in manifest`);

      const renderResp = await fetch(`${DATA_BASE}/render.json`);
      const renderModel = await renderResp.json();
      this.console = new VirtualConsole(renderModel.system);
      installDate(this.console);
      this.derived = new DerivedModel(renderModel, this.deviceHost());
      this.pipeline = new DevicePipeline(THREE, renderModel, { resolve: (h) => this.resolveTextureHandle(h) });
      this.device = cloneState(this.derived.defaults);
      this.derived.onDraw = (state, env, quad) => this.onDerivedDraw(state, env, quad);
      this.natives = { ...nativeClasses(this, this.console), ...this.runtimeNativeClasses() };
      for (const cls of Object.keys(nativeClasses(this, this.console))) RUNTIME_FIELD_CLASSES.add(cls);
      this.installHooks();
      this.checkNatives(renderModel.system.methods);
      for (const t of renderModel.textures) {
        const key = 'texture:' + (t.va >>> 0).toString(16).toUpperCase().padStart(8, '0');
        this.proceduralTextures[key] = this.loadTexture(`proc_${key.slice(8)}.png`);
        // The producer's pixels were captured at derivation, so the call hands back the exported texture.
        this.derived.hook(t.va, () => ({ texture: key }));
      }
      _log(`[XBE] ${renderModel.materials.length} materials, ${Object.keys(renderModel.programs).length} vertex programs, ${renderModel.textures.length} procedural textures, ${Object.keys(renderModel.draw.functions).length} routines`);

      await this.loadFonts();

      const loadPromises = this.config.scenes.map(name => this.loadSceneData(name));
      await Promise.allSettled(loadPromises);
      _log(`[XAP] ${Object.keys(this.sceneData).length} scenes loaded`);

      // The device init runs on the executable's renderer object before any scene is built, then the startup
      // creates the matrix stack the node draws compose their world matrices on.
      this.runRenderer(this.derived.scene.init);
      // The main load fills the caller's path buffer with the scene file it opens.
      this.runRenderer(this.derived.scene.main_load, [{ obj: new SceneObject(HEAP_TYPE, null), off: 0 }]);
      this.derived.memory.store(this.derived.scene.matrix_stack, 4, { matrixStack: new MatrixStack() });
      this.rootGlobal = this.findRootGlobal();

      await this.loadScene(this.config.entry);
      _log(`[XAP] ${Object.keys(this.nodes).length} nodes registered`);

      this.executeScripts();
      this.derived.memory.store(this.rootGlobal, 4, { obj: this.root });
      this.runEntryInitialize();

      document.getElementById('loading').classList.add('hidden');
      this.animate();
    } catch (err) {
      _error('Runtime start failed:', err);
      document.getElementById('loading').textContent = 'Error: ' + err.message;
    }
  }

  // What the derived paths ask the runtime for: buffers, the console and the device surface
  deviceHost() {
    const runtime = this;
    return {
      get tickBase() { return runtime.tickBase; },
      get device() { return runtime.device; },
      console: this.console,
      createVertexBuffer: (size, fvf) => new VertexBuffer(size, fvf),
      backBufferWidth: () => this.derived.memory.load(this.presentParamsAddress(), 4),
      backBufferHeight: () => this.derived.memory.load(this.presentParamsAddress() + PRESENT_HEIGHT, 4),
      onClear: (state) => { this.frameClear = state.clear; },
    };
  }

  // The present parameters are the buffer the init hands the device creation
  presentParamsAddress() {
    if (!this.presentParams) throw new Error('the device was created with no present parameters');
    return this.presentParams;
  }

  // Runs a renderer routine on the executable's static renderer object
  runRenderer(va, args = []) {
    const sc = this.derived.scene;
    const parent = this.derived.newEnv(null);
    parent.state = this.device;
    return this.derived.run(va, sc.renderer, args, this.device, parent);
  }

  // The global the frame routine draws the scene through
  findRootGlobal() {
    const sc = this.derived.scene;
    const paths = this.derived.functions.get(sc.frame);
    for (const p of paths) {
      for (const e of p.effects) {
        if (e.kind !== 'call' || e.fn !== 'vcall@' + this.derived.draw.slot.toString(16).toUpperCase()) continue;
        for (const a of e.args || []) {
          const n = this.derived.nodes[a];
          if (n.k === 'load' && n.base === '' && n.addr === undefined) return n.off;
        }
      }
    }
    throw new Error('the frame routine draws no node loaded from a global');
  }

  // Runtime implementations of the routines the model keeps as calls
  installHooks() {
    const d = this.derived, sc = d.scene;
    d.hook(d.drawSlotFunction('Text'), (thisVal, args, state) => { this.onDerivedTextDraw(this.objectOf(thisVal), state); return 0; });
    d.hook(d.typeSlot('DynamicTexture', d.draw.texture_slot), (thisVal) => this.dynamicTextureHandle(this.objectOf(thisVal)));
    d.hook(sc.group.draw, (thisVal, args, state, env) => { this.drawChildren(this.objectOf(thisVal), state, env); return 0; });
    d.hook(sc.group.tick, (thisVal, args, state, env) => { this.tickChildren(this.objectOf(thisVal), args[0], state, env); return 0; });
    d.hook(sc.array_element, (thisVal, args) => this.arrayElement(thisVal, d.int(args[0])));
    d.hook(sc.fire_event, (thisVal, args) => this.fireDerivedEvent(args[0], d.string(args[1])));
    d.hook(sc.present, (thisVal, args, state) => { this.presentFrame(state); return 0; });
    d.hook(sc.script_tick, () => 0);
    // The ticks of the classes the runtime drives itself advance audio voices, input and glyph scrolling here.
    for (const cls of RUNTIME_FIELD_CLASSES) {
      const slots = sc.slots[cls];
      if (slots && slots.tick && slots.tick !== sc.group.tick) d.hook(slots.tick, () => 0);
    }
    for (const va of sc.tick_hooks || []) if (va !== sc.timers.advance) d.hook(va, () => 0);
    for (const va of sc.timer_hooks || []) if (va !== sc.timers.remove) d.hook(va, () => 0);
    d.hook(sc.timers.advance, (thisVal, args, state, env) => { this.advanceTimers(state, env); return 0; });
    d.hook(sc.timers.remove, (thisVal, args) => { this.removeTimers(args[0]); return 0; });
    d.hook(sc.loader, (thisVal, args) => this.loadArchive(d.string(args[0])));
    d.hook(sc.unloader, () => 0);
    for (const va of sc.image_load || []) d.hook(va, () => 0);
    d.hook(sc.scene_load.reset, () => 0);
    d.hook(sc.scene_load.resolve, (thisVal, args, state, env) => { d.storeString(args[0], d.string(args[1]), env); return args[0]; });
    d.hook(sc.scene_load.populate, (thisVal) => { this.populateInline(this.objectOf(thisVal).node); return 1; });
    d.hook(sc.scene_load.new_scope, (thisVal) => ({ obj: new SceneObject(SCOPE_TYPE, this.objectOf(thisVal).node), off: 0 }));
    d.hook(sc.scene_load.attach, () => 0);
    // Allocators hand back fresh zeroed memory the paths then fill by field offset.
    for (const va of d.model.allocators) d.hook(va, () => ({ obj: new SceneObject(HEAP_TYPE, null), off: 0 }));
    // The device creation keeps the present parameters the init filled in.
    d.hook = d.hook.bind(d);
    const perform = d.performCall.bind(d);
    d.performCall = (fn, argIdx, thisIdx, env, seq) => {
      if (fn === 'Direct3D_CreateDevice') {
        const params = d.eval(argIdx[4], env);
        if (typeof params !== 'number') throw new Error(`device created with present parameters at ${JSON.stringify(params)}`);
        this.presentParams = params;
        return 0;
      }
      return perform(fn, argIdx, thisIdx, env, seq);
    };
  }

  objectOf(v) {
    if (v && v.obj !== undefined) return v.obj;
    if (v instanceof SceneObject) return v;
    throw new Error(`hook called on ${JSON.stringify(v)} which is no scene object`);
  }

  // The children a group object draws, in scene order
  childObjects(obj) {
    const node = obj.node;
    if (!node) throw new Error(`${obj.type} object has no scene node to draw children from`);
    return node._xapChildren.map((c) => c.derivedObject());
  }

  // Draws a group's visible children through their class draw methods
  drawChildren(obj, state, env) {
    const d = this.derived, g = d.scene.group;
    for (const child of this.childObjects(obj)) {
      if (!d.int(d.readField(child, g.visible, 1))) continue;
      d.run(d.drawSlotFunction(child.type), child, [], state, env);
    }
  }

  // Advances a group's children by the frame time through their class tick methods
  tickChildren(obj, dt, state, env) {
    const d = this.derived;
    for (const child of this.childObjects(obj)) {
      d.run(d.tickSlotFunction(child.type), child, [dt], state, env);
    }
  }

  // An element of a node array field: the children array or another multi node field
  arrayElement(thisVal, index) {
    if (!thisVal || thisVal.obj === undefined) throw new Error(`array element of ${JSON.stringify(thisVal)}`);
    const obj = thisVal.obj, off = thisVal.off || 0;
    const d = this.derived;
    let list;
    if (off === d.scene.group.children) list = obj.node._xapChildren;
    else {
      const f = d.fieldAt(obj.type, off);
      if (!f) throw new Error(`${obj.type} has no array field at +0x${off.toString(16)}`);
      list = obj.node.fieldNodeList(f.name);
    }
    if (index < 0 || index >= list.length) return 0;
    return { obj: list[index].derivedObject() };
  }

  // Delivers a script event the executable fires to a node or a loaded scene
  fireDerivedEvent(target, name) {
    const obj = this.objectOf(target);
    if (obj.type === SCOPE_TYPE) {
      if (name === 'initialize') { this.runSceneInitialize(obj.node); return 1; }
      throw new Error(`event ${name} fired on the scene of ${obj.node.data.def || obj.node.data.type}`);
    }
    const handled = this.fireEvent(obj.node, name);
    return handled === undefined ? 0 : 1;
  }

  // Steps every field interpolation record on the executable's list, unlinking the finished ones
  advanceTimers(state, env) {
    const d = this.derived, t = d.scene.timers;
    let prev = null;
    let record = d.memory.load(t.head, 4);
    while (record && record.obj !== undefined) {
      const next = d.readField(record.obj, t.next, 4);
      const running = d.int(d.run(t.step, record.obj, [], state, env)) & 0xFF;
      if (!running) {
        if (prev) d.writeField(prev.obj, t.next, next);
        else d.memory.store(t.head, 4, next);
      } else {
        prev = record;
      }
      record = next;
    }
  }

  // Unlinks the interpolation records that write into a node's fields
  removeTimers(node) {
    const d = this.derived, t = d.scene.timers;
    let prev = null;
    let record = d.memory.load(t.head, 4);
    while (record && record.obj !== undefined) {
      const next = d.readField(record.obj, t.next, 4);
      const owner = d.readField(record.obj, t.node, 4);
      if (owner && owner.obj !== undefined && node && node.obj !== undefined && owner.obj === node.obj) {
        if (prev) d.writeField(prev.obj, t.next, next);
        else d.memory.store(t.head, 4, next);
      } else {
        prev = record;
      }
      record = next;
    }
  }

  // The archive an inline or level loads, which the runtime built with the scene
  loadArchive(path) {
    const key = path.toLowerCase();
    if (this.archives.has(key)) return { obj: this.archives.get(key) };
    const archive = new SceneObject(ARCHIVE_TYPE, null);
    const d = this.derived;
    const loader = d.functions.get(d.scene.inline_load);
    if (!loader) throw new Error('the inline load routine is not summarized');
    for (const [off, value] of this.loadedArchiveFields()) archive.slots.set(off, value);
    this.archives.set(key, archive);
    return { obj: archive };
  }

  // The archive fields the inline draw and tick read, valued as a finished load leaves them
  loadedArchiveFields() {
    const d = this.derived, sc = d.scene;
    const inline = d.types.Inline;
    const reads = new Map();
    const walk = (idx) => {
      const n = d.nodes[idx];
      if (!n || typeof n !== 'object') return;
      if (n.k === 'load' && n.addr !== undefined) {
        const a = d.nodes[n.addr];
        if (a && a.k === 'bin' && a.op === 'add') {
          const b = d.nodes[a.b];
          if (b && b.k === 'i' && this.loadsInlineArchive(a.a)) reads.set(b.v, n.size);
        }
      }
      for (const key of ['a', 'b', 'addr', 'this']) if (n[key] !== undefined) walk(n[key]);
      for (const key of ['args', 'elems']) if (n[key]) for (const x of n[key]) walk(x);
    };
    for (const va of [inline.slots[d.draw.slot / 4], inline.slots[sc.tick_slot / 4]]) {
      for (const p of d.functions.get(va)) {
        for (const g of p.guards) walk(g);
        for (const e of p.effects) { if (e.value !== undefined) walk(e.value); for (const a of e.args || []) walk(a); if (e.this !== undefined) walk(e.this); }
        walk(p.return);
      }
    }
    // A loaded archive holds data and runs no loading thread, which is what the inline paths test for.
    const fields = [];
    const offsets = [...reads.keys()].sort((a, b) => a - b);
    if (offsets.length < 2) throw new Error('the inline paths read no archive fields');
    fields.push([offsets[0], 1]);
    for (const off of offsets.slice(1)) fields.push([off, 0]);
    return fields;
  }

  // Reports whether an expression loads the inline's archive field
  loadsInlineArchive(idx) {
    const n = this.derived.nodes[idx];
    if (!n || n.k !== 'load' || n.base !== 'this') return false;
    const f = this.derived.fieldAt('Inline', n.off);
    return !f && n.size === 4;
  }

  // The scene loader hands each field of the inline's scene to its class hook as it parses the file
  populateInline(node) {
    if (node._populated) return;
    node._populated = true;
    for (const owned of node._ownedNodes) owned.applyFieldHooks();
  }

  // Runs a loaded scene's initialize once, the way the post load step fires it
  runSceneInitialize(node) {
    const scope = node._inlineScope;
    if (!scope) throw new Error(`${node.data.def || node.data.type} loaded no scene to initialize`);
    if (scope.initialized) return;
    scope.initialized = true;
    const init = scope.get('initialize');
    if (typeof init !== 'function') return;
    try {
      init();
    } catch (e) {
      _warn(`[XAP] initialize() error in ${scope.name()}:`, e.message);
    }
  }

  // The entry scene's initialize runs after its scripts, as the main scene load fires it
  runEntryInitialize() {
    const scope = this.scopes[0];
    scope.initialized = true;
    const init = scope.get('initialize');
    if (typeof init !== 'function') return;
    try {
      init();
    } catch (e) {
      _warn(`[XAP] initialize() error in ${scope.name()}:`, e.message);
    }
  }

  async loadFonts() {
    const fontFiles = [
      { file: 'Xbox', role: 'heading' },
      { file: 'XBox_Book', role: 'body' },
    ];
    for (const { file, role } of fontFiles) {
      const resp = await fetch(`${DATA_BASE}/fonts/${file}.json`);
      if (!resp.ok) throw new Error(`font ${file} is not exported`);
      const fontData = await resp.json();
      this.fonts[role] = fontData;
      _log(`[XTF] Loaded font "${fontData.name}" as ${role}`);
    }
  }

  async loadSceneData(name) {
    if (this.sceneData[name]) return this.sceneData[name];
    const resp = await fetch(`${DATA_BASE}/scenes/${name}.json`);
    const data = await resp.json();
    this.sceneData[name] = data;
    return data;
  }

  // Builds the entry scene under a root group the frame routine draws
  async loadScene(name) {
    const data = await this.loadSceneData(name);
    const archiveName = name.split('_').slice(0, -1).join('_');
    const rootNode = new XAPNode({ type: 'Group', def: '', fields: {} }, this, archiveName);
    this.root = rootNode.derivedObject();
    this.buildOwners.push(rootNode);
    this.buildScene(data, archiveName, rootNode);
    this.buildOwners.pop();
    // The main scene load hands every field to its class hook as it parses the file.
    for (const node of rootNode._ownedNodes) node.applyFieldHooks();
  }

  buildScene(data, archiveName, parentNode) {
    const scope = new ScriptScope(this, parentNode.scope, parentNode);
    this.scopes.push(scope);
    parentNode._inlineScope = scope;
    for (const item of data.items) {
      if (item.kind === 'node' && item.node) {
        const childNode = this.buildNode(item.node, archiveName, scope, parentNode);
        if (childNode) parentNode._xapChildren.push(childNode);
      } else if (item.kind === 'script') {
        this.pendingScripts.push({ scope, script: item.script });
      }
    }
  }

  // ========================================================================
  // Node builder: every scene node becomes the executable's object for its class
  // ========================================================================

  buildNode(data, archiveName, scope, parentNode) {
    const node = new XAPNode(data, this, archiveName);
    node.scope = scope;
    node.parentNode = parentNode || null;
    data._xapNode = node;
    this.ownNode(node);
    const f = data.fields || {};
    if (this.natives[data.type]) node._native = this.natives[data.type].$init ? this.natives[data.type].$init(node) : {};
    if (data.type === 'ScreenSaver') this.screenSaver = new ScreenSaverTimer(this, node);

    if (data.def) {
      this.nodes[data.def] = node;
      this.scriptScope[data.def] = node.proxy;
      if (scope) scope.defs.set(data.def, node);
    }

    const geomData = extractNode(f.geometry);
    const appData = extractNode(f.appearance);
    if (geomData) {
      this.buildGeometry(node, geomData, appData, archiveName);
    }

    if (f.font !== undefined && !geomData) {
      const tr = new TextRenderer(this, node, String(f.font), this.fonts, this.pipeline.createMaterial(), String(f.justify || 'left'));
      node._textRenderer = tr;
      this.textRenderers.push(tr);
    }

    if (f.url !== undefined && typeof f.url === 'string' && f.url.match(/\.wav$/i)) {
      node._audioUrl = f.url;
      node._audioLoop = !!f.loop;
      node._audioVolume = asNumber(f.volume !== undefined ? f.volume : 1);
      node._audioPan = asNumber(f.pan || 0);
      node._audioFade = asNumber(f.fade || 0);
      node._isPlaying = false;
    }

    if (f.period !== undefined) {
      node._period = asNumber(f.period);
      node._periodNoise = asNumber(f.periodNoise || 0);
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
      setTimeout(scheduleNext, node._period * 1000);
    }

    if (f.archive !== undefined) {
      node._archive = String(f.archive);
      const archiveBase = node._archive.replace(/\.xip$/i, '');
      node._sceneName = this.resolveArchive(archiveBase);
    }

    // An inline's scene is built in place, the executable's load step finding it already there.
    if (f.url !== undefined && typeof f.url === 'string' && f.url.match(/\.xap$/i)) {
      node._inlineUrl = f.url;
      const sceneName = this.resolveInlineUrl(String(f.url));
      node._sceneName = sceneName;
      if (!sceneName || !this.sceneData[sceneName]) throw new Error(`inline ${f.url} names no exported scene`);
      this.buildOwners.push(node);
      this.buildScene(this.sceneData[sceneName], extractArchivePrefix(sceneName), node);
      this.buildOwners.pop();
    }

    if (data.scripts && data.scripts.length > 0) {
      node._eventHandlers = {};
      node._eventSources = data.scripts;
      this.joysticks.push(node);
    }

    // Node valued fields such as a level's control, path and shell are built as nodes of their own.
    for (const [key, value] of Object.entries(f)) {
      if (key === 'geometry' || key === 'appearance') continue;
      const nodeData = extractNode(value);
      if (nodeData) {
        const childNode = this.buildNode(nodeData, archiveName, scope, node);
        node._fieldNodes[key] = childNode;
      } else if (Array.isArray(value) && value.some((v) => extractNode(v))) {
        node._fieldLists[key] = value.map((v) => extractNode(v)).filter(Boolean).map((v) => this.buildNode(v, archiveName, scope, node));
      }
    }

    if (data.children) {
      for (const childData of data.children) {
        if (childData) {
          const childNode = this.buildNode(childData, archiveName, scope, node);
          if (childNode) node._xapChildren.push(childNode);
        }
      }
    }

    // The executable constructs every node as the scene file names it, so the object exists in file order
    node.derivedObject();
    return node;
  }

  // Records a node as part of the scene being built, whose loader runs its field hooks
  ownNode(node) {
    const owner = this.buildOwners[this.buildOwners.length - 1];
    if (!owner) throw new Error(`node ${node.data.def || node.data.type} built outside any scene`);
    owner._ownedNodes.push(node);
  }

  // Build geometry from a node's geometry/appearance field pair
  buildGeometry(node, geomData, appData, archiveName) {
    const geomFields = geomData.fields || {};
    const geomNode = this.nodeFor(geomData, archiveName);
    if (appData) this.prepareAppearance(appData, archiveName);
    const material = this.pipeline.createMaterial();

    if (geomFields.url && typeof geomFields.url === 'string' && geomFields.url.match(/\.xm$/i)) {
      this.loadMesh(geomFields.url, archiveName).then(geometry => {
        if (!geometry) throw new Error(`mesh ${geomFields.url} is packed in no extracted archive`);
        const mesh = new THREE.Mesh(geometry, material);
        mesh.name = geomData.def || geomFields.url;
        detachFromThree(mesh);
        node._mesh = mesh;
        const entry = geometry.userData.entry;
        this.attachBuffer(geomNode, { format: entry.format, stride: entry.stride, primitiveCount: entry.indexCount / 3 }, mesh, geometry.userData.desc);
        this._meshesLoaded = (this._meshesLoaded || 0) + 1;
        if (this._meshesLoaded % 100 === 0) {
          _log(`[XAP] ${this._meshesLoaded} meshes loaded`);
        }
      }).catch(e => {
        _warn(`[XAP] Mesh load failed: ${geomFields.url}`, e.message);
      });
      return;
    }

    if (geomFields.font !== undefined) {
      const fontName = geomFields.font || 'Body';
      const justify = geomFields.justify || 'left';
      const tr = new TextRenderer(this, geomNode, String(fontName), this.fonts, material, String(justify));
      node._textRenderer = tr;
      geomNode._textRenderer = tr;
      this.textRenderers.push(tr);
      return;
    }

    if (geomFields.radius !== undefined) {
      const geometry = this.sphereGeometry(asNumber(geomFields.radius), geomFields.slices, geomFields.stacks);
      const sphere = new THREE.Mesh(geometry, material);
      detachFromThree(sphere);
      node._mesh = sphere;
      const sb = this.derived.draw.sphere;
      this.attachBuffer(geomNode, { format: sb.format, stride: sb.stride, primitiveCount: geometry.index.count / 3 }, sphere, geometry.userData.desc);
      return;
    }
    throw new Error(`geometry node ${geomData.type} has neither a mesh url, text nor a radius`);
  }

  // Gives a geometry node the loaded buffer object its draw path dereferences
  attachBuffer(geomNode, geometry, mesh, desc) {
    const buffer = makeBuffer(this.derived.model, geometry);
    buffer.mesh = mesh;
    buffer.desc = desc;
    geomNode.derivedObject().slots.set(this.derived.accessor.offset, { obj: buffer });
  }

  // Builds a sphere in the layout the executable stores: position, packed normal and a zero diffuse
  sphereGeometry(radius, slices, stacks) {
    const sb = this.derived.draw.sphere;
    const layout = this.derived.model.fixed_function.find((ff) => ff.format === sb.format);
    if (!layout) throw new Error(`sphere format 0x${sb.format.toString(16)} has no fixed function declaration`);
    const decl = this.derived.model.declarations[(layout.declaration >>> 0).toString(16).toUpperCase().padStart(8, '0')];
    const d = this.derived;
    const src = new THREE.SphereGeometry(radius, slices !== undefined ? asNumber(slices) : d.defaultField('Sphere', 'slices'), stacks !== undefined ? asNumber(stacks) : d.defaultField('Sphere', 'stacks'));
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
    if (this.buildOwners.length) this.ownNode(node);
    else node.applyFieldHooks();
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

  // Applies the device state at a draw call to the mesh being drawn and queues it for the frame
  onDerivedDraw(state, env, quad) {
    if (quad) {
      this.drawQuad(state, quad);
      return;
    }
    const buffer = env.this && env.this.obj !== undefined ? env.this.obj : env.this;
    if (!buffer || !buffer.mesh) throw new Error(`indexed draw from ${buffer ? buffer.type : 'nothing'} which holds no mesh`);
    this.pipeline.update(buffer.mesh.material, state, buffer.desc);
    this.frameDraws.push(buffer.mesh);
  }

  // Draws a range of the vertex buffer the executable filled through a lock
  drawQuad(state, quad) {
    const stream = state.stream;
    if (!stream || !stream.vb) throw new Error(`vertex draw from ${JSON.stringify(stream)} which is no vertex buffer`);
    const vb = stream.vb;
    const fvf = typeof state.program === 'number' ? state.program : vb.fvf;
    const desc = describeFVF(fvf);
    if (desc.stride !== state.streamStride) throw new Error(`vertex format 0x${fvf.toString(16)} is ${desc.stride} bytes but the stream stride is ${state.streamStride}`);
    const key = vb;
    let entry = this.quadMeshes.get(key);
    const indices = primitiveIndices(quad.primitive, quad.start, quad.count);
    if (!entry || entry.version !== vb.version || entry.fvf !== fvf || entry.indices !== indices.join(',')) {
      const count = vb.bytes.length / desc.stride;
      const geometry = new THREE.BufferGeometry();
      const view = vb.view;
      for (const a of desc.attributes) {
        const data = new Float32Array(count * a.comps);
        for (let v = 0; v < count; v++) {
          const base = v * desc.stride + a.offset;
          if (a.source === 'color') {
            const c = view.getUint32(base, true);
            data[v * 4] = ((c >>> 16) & 255) / 255; data[v * 4 + 1] = ((c >>> 8) & 255) / 255; data[v * 4 + 2] = (c & 255) / 255; data[v * 4 + 3] = (c >>> 24) / 255;
          } else {
            for (let k = 0; k < a.comps; k++) data[v * a.comps + k] = view.getFloat32(base + 4 * k, true);
          }
        }
        geometry.setAttribute('a_' + a.offset, new THREE.BufferAttribute(data, a.comps));
      }
      geometry.setIndex(indices);
      const mesh = new THREE.Mesh(geometry, entry ? entry.mesh.material : this.pipeline.createMaterial());
      detachFromThree(mesh);
      if (entry) entry.mesh.geometry.dispose();
      entry = { mesh, version: vb.version, fvf, indices: indices.join(',') };
      this.quadMeshes.set(key, entry);
    }
    this.pipeline.update(entry.mesh.material, state, desc);
    this.frameDraws.push(entry.mesh);
  }

  // The glyph draw happens between the render states the executable's text draw routine sets
  onDerivedTextDraw(obj, state) {
    const tr = obj.node._textRenderer;
    if (!tr) throw new Error(`text draw on ${obj.node.data.def || obj.node.data.type} which renders no text`);
    const td = this.derived.draw.text_draw;
    for (const [index, value] of td.before) this.derived.setRenderState(state, index, value);
    tr.update();
    if (tr.layoutMesh) {
      const snapshot = cloneState(state);
      snapshot.transforms[TS_WORLD] = matMul(Float32Array.from(tr.layoutMesh.matrix.elements), state.transforms[TS_WORLD]);
      this.pipeline.update(tr.material, snapshot, TEXT_GEOMETRY, true);
      this.frameDraws.push(tr.layoutMesh);
    }
    for (const [index, value] of td.after) this.derived.setRenderState(state, index, value);
  }

  // Shows the frame the executable just drew, fitted to the display shape the console would show it in
  presentFrame(state) {
    this.drawRoot.children = this.frameDraws;
    this.lastDraws = this.frameDraws.map((m) => m.name || m.type);
    this.frameDraws = [];
    const clear = this.frameClear;
    const canvas = this.renderer.domElement;
    const width = this.derived.memory.load(this.presentParamsAddress() + PRESENT_WIDTH, 4);
    const height = this.derived.memory.load(this.presentParamsAddress() + PRESENT_HEIGHT, 4);
    const aspect = (this.console.videoFlags() & VIDEO_FLAG_WIDESCREEN) ? DISPLAY_ASPECT_WIDE : DISPLAY_ASPECT_NORMAL;
    const cw = canvas.clientWidth, ch = canvas.clientHeight;
    let vw = cw, vh = cw / aspect;
    if (vh > ch) { vh = ch; vw = ch * aspect; }
    const vx = (cw - vw) / 2, vy = (ch - vh) / 2;
    this.renderer.setScissorTest(false);
    this.renderer.setClearColor(0x000000, 1);
    this.renderer.clear(true, true, true);
    this.renderer.setViewport(vx, vy, vw, vh);
    this.renderer.setScissor(vx, vy, vw, vh);
    this.renderer.setScissorTest(true);
    if (clear && (clear.flags & CLEAR_TARGET)) {
      this.renderer.setClearColor(clear.color & 0xFFFFFF, (clear.color >>> 24) / 255);
      this.renderer.clear(true, true, true);
    }
    this.renderer.render(this.scene, this.camera);
    this.frameClear = null;
    this.frameSize = { width, height };
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
      undefined,
      undefined,
      () => { _error(`[XAP] Texture not found: ${base}.png`); }
    );
    // The device sampled texels as stored, with the first image row at v = 0.
    tex.colorSpace = THREE.NoColorSpace;
    tex.flipY = false;
    tex.generateMipmaps = true;
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
    if (this.config.archives[archiveBase]) return this.config.archives[archiveBase];
    const lower = archiveBase.toLowerCase();
    if (this.config.archives[lower]) return this.config.archives[lower];
    for (const [key, val] of Object.entries(this.config.archives)) {
      if (key.toLowerCase() === lower) return val;
    }
    return null;
  }

  resolveInline(url) {
    if (!this.config) return null;
    if (this.config.inlines[url]) return this.config.inlines[url];
    const lower = url.toLowerCase();
    for (const [key, val] of Object.entries(this.config.inlines)) {
      if (key.toLowerCase() === lower) return val;
    }
    return null;
  }

  // Resolve Inline URL — handles both "scene.xap" and "ArchiveName/default.xap" patterns
  resolveInlineUrl(url) {
    const inline = this.resolveInline(url);
    if (inline) return inline;

    const parts = url.replace(/\\/g, '/').split('/');
    if (parts.length >= 2 && parts[parts.length - 1].toLowerCase() === 'default.xap') {
      const archiveName = parts[parts.length - 2];
      const archive = this.resolveArchive(archiveName);
      if (archive) return archive;
    }

    if (parts.length >= 2) {
      const archiveName = parts[0];
      const sceneName = parts[parts.length - 1].replace(/\.xap$/i, '');
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
    for (const [name, node] of Object.entries(this.nodes)) {
      if (name === 'console') continue;
      window[name] = node.proxy;
    }

    window.log = (...args) => _log('[XAP script]', ...args);
    window.EnableInput = (b) => { this.inputEnabled = !!b; _log('[XAP] EnableInput:', b); };

    const byScope = new Map();
    for (const { scope, script } of this.pendingScripts) {
      if (!byScope.has(scope)) byScope.set(scope, []);
      byScope.get(scope).push(script);
    }
    for (const scope of this.scopes) scope.declare(byScope.get(scope) || []);
    let ok = 0;
    for (const scope of this.scopes) {
      scope.evaluate(byScope.get(scope) || []);
      ok += (byScope.get(scope) || []).length;
    }
    for (const node of this.joysticks) this.compileHandlers(node);
    _log(`[XAP] Scripts: ${ok} evaluated in ${this.scopes.length} scene scopes`);
    if (window.console !== nativeConsole) {
      window.console = nativeConsole;
      _warn('[XAP] scripts replaced the console, restored it');
    }
  }

  // Compiles a node's handler functions and behaviors inside its fields and scene scope
  compileHandlers(node) {
    const fieldProxy = new Proxy({}, {
      has: (t, k) => typeof k === 'string' && (k === 'children' || (node.data.fields && Object.prototype.hasOwnProperty.call(node.data.fields, k))),
      get: (t, k) => node.proxy[k],
      set: (t, k, v) => { node.proxy[k] = v; return true; },
    });
    for (const raw of node._eventSources) {
      const src = rewriteScript(raw);
      const trimmed = src.trimStart();
      if (trimmed.startsWith('behavior')) {
        const bodyStart = trimmed.indexOf('{'), bodyEnd = trimmed.lastIndexOf('}');
        if (bodyStart < 0 || bodyEnd <= bodyStart) throw new Error(`behavior on ${node.data.def || node.data.type} has no body`);
        const body = trimmed.substring(bodyStart + 1, bodyEnd).replace(/\bsleep\s+([\d.]+)\s*;/g, 'await __sleep($1);');
        const loop = new Function('__scope', '__node', '__sleep', '__runtime', `with (__scope) { with (__node) { return (async function () { while (__runtime.running) { ${body}\n await __sleep(0); } }); } }`);
        const fn = loop(node.scope.handlerProxy, fieldProxy, (sec) => new Promise((r) => setTimeout(r, sec * 1000)), this);
        fn().catch((e) => _warn(`[XAP] behavior error on ${node.data.def || node.data.type}:`, e.message));
        continue;
      }
      const match = trimmed.match(/^function\s+(\w+)\s*\(/);
      if (!match) throw new Error(`script on ${node.data.def || node.data.type} is neither a handler nor a behavior: ${trimmed.slice(0, 40)}`);
      const make = new Function('__scope', '__node', `with (__scope) { with (__node) { return (${trimmed}); } }`);
      node._eventHandlers[match[1]] = make(node.scope.handlerProxy, fieldProxy);
    }
  }

  // Calls a node's handler, searching the parents when the node has none
  fireEvent(node, name, ...args) {
    for (let n = node; n; n = n.parentNode) {
      const handler = n._eventHandlers && n._eventHandlers[name];
      if (!handler) continue;
      try {
        return handler(...args);
      } catch (e) {
        _warn(`[XAP] ${name} on ${n.data.def || n.data.type}:`, e.message);
        return undefined;
      }
    }
    return undefined;
  }

  // Natives the runtime implements through its audio machinery and the executable's own native routines
  runtimeNativeClasses() {
    const unimplemented = (cls, names) => Object.fromEntries(names.map((n) => [n, () => { throw new Error(`${cls}.${n} is registered by the executable but the runtime does not implement it`); }]));
    const derivedNative = (va) => (node, ...args) => this.runNative(va, node, args);
    const classes = {
      AudioClip: {
        Play: (node) => node._play(),
        Stop: (node) => node._stop(),
        Pause: (node) => node._pause(),
        PlayOrPause: (node) => (node._transportMode === 0 ? node._play() : node._pause()),
        getMinutes: (node) => Math.floor(node._playbackSeconds() / 60),
        getSeconds: (node) => Math.floor(node._playbackSeconds()) % 60,
      },
      Keyboard: unimplemented('Keyboard', this.derived.model.system.methods.Keyboard),
      DVDPlayer: unimplemented('DVDPlayer', this.derived.model.system.methods.DVDPlayer),
    };
    for (const [cls, fns] of Object.entries(this.derived.scene.natives)) {
      classes[cls] = classes[cls] || {};
      for (const [name, va] of Object.entries(fns)) classes[cls][name] = derivedNative(va);
    }
    return classes;
  }

  // Runs one of the executable's script natives on a node with float arguments
  runNative(va, node, args) {
    const d = this.derived;
    const parent = d.newEnv(null);
    parent.state = this.device;
    const bits = args.map((a) => bitsFromFloat(Number(a)));
    return d.run(va, node.derivedObject(), bits, this.device, parent);
  }

  // Every method a class registers must be implemented, Date's own members counting as implemented
  checkNatives(methods) {
    const missing = [];
    for (const [cls, names] of Object.entries(methods)) {
      for (const name of names) {
        const impl = this.natives[cls] && this.natives[cls][name];
        if (impl) continue;
        if (cls === 'Date' && typeof Date.prototype[name] === 'function') continue;
        missing.push(`${cls}.${name}`);
      }
    }
    if (missing.length) throw new Error(`natives registered by the executable without a runtime implementation: ${missing.join(', ')}`);
  }

  // The joystick bound last receives input, unbinding returns it to the one bound before
  bindJoystick(node, bound) {
    this.boundJoysticks = this.boundJoysticks.filter((n) => n !== node);
    if (bound) this.boundJoysticks.push(node);
    node._isBound = bound;
  }

  // Replaces a mesh node's geometry from its url field
  reloadMesh(node) {
    const shape = node.parentNode;
    if (!shape || !shape._mesh) throw new Error(`mesh ${node.data.def || node.data.type} is not drawn by a shape`);
    this.loadMesh(String(node.proxy.url), node.archiveName).then((geometry) => {
      if (!geometry) throw new Error(`mesh ${node.proxy.url} is packed in no extracted archive`);
      shape._mesh.geometry = geometry;
      const entry = geometry.userData.entry;
      this.attachBuffer(node, { format: entry.format, stride: entry.stride, primitiveCount: entry.indexCount / 3 }, shape._mesh, geometry.userData.desc);
    });
  }

  // The texture handle a dynamic texture node hands the appearance draw
  dynamicTextureHandle(obj) {
    throw new Error(`dynamic texture ${obj.node.data.def || 'DynamicTexture'} is not rendered by the runtime`);
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

  // The joystick that receives input: the one a script bound last, else the active level's control
  activeJoystick() {
    if (this.boundJoysticks.length) return this.boundJoysticks[this.boundJoysticks.length - 1];
    const d = this.derived;
    const control = d.memory.load(this.controlGlobal(), 4);
    if (control && control.obj !== undefined) return control.obj.node;
    return null;
  }

  // The global the executable binds the arriving level's control into
  controlGlobal() {
    const va = this.derived.scene.control_global;
    if (!va) throw new Error('the executable binds no level control global');
    return va;
  }

  dispatchJoystickEvent(eventName) {
    if (this.screenSaver) this.screenSaver.activity();
    if (!this.inputEnabled) return;
    const node = this.activeJoystick();
    if (!node || !node._eventHandlers) return;
    const handler = node._eventHandlers[eventName];
    if (!handler) return;
    try {
      handler();
    } catch (e) {
      _warn(`Joystick ${eventName}:`, e.message);
    }
  }

  pollGamepad() {
    const gamepads = navigator.getGamepads ? navigator.getGamepads() : [];
    for (const gp of gamepads) {
      if (!gp) continue;

      const id = gp.index;
      const prev = this.prevButtons[id] || [];

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
  // Render loop: the executable's tick then its frame routine
  // ========================================================================

  animate() {
    if (!this.running) return;
    requestAnimationFrame(() => this.animate());
    const dt = this.clock.getDelta();
    this.pollGamepad();
    this.updateAudio();
    if (this.screenSaver) this.screenSaver.tick(dt);
    for (const tr of this.textRenderers) tr.tick(dt);
    try {
      this.frameDraws = [];
      this.runRenderer(this.derived.scene.tick);
      this.runRenderer(this.derived.scene.frame);
    } catch (err) {
      this.running = false;
      _error('Frame failed:', err);
      document.getElementById('loading').textContent = 'Error: ' + err.message;
      document.getElementById('loading').classList.remove('hidden');
      throw err;
    }
  }

  // Playback progress feeds the clip handlers the way the engine's voice callbacks do
  updateAudio() {
    for (const node of this.playingClips) {
      if (node._transportMode !== 1 || !node._source) continue;
      const seconds = node._playbackSeconds();
      const duration = node._source.buffer ? node._source.buffer.duration : 0;
      if (duration > 0 && node._fields.sendProgress) {
        const progress = Math.min(seconds / duration, 1);
        if (progress !== node._fields.progress) {
          node._fields.progress = progress;
          this.fireEvent(node, 'OnProgressChanged');
        }
      }
    }
  }

  onResize() {
    this.renderer.setSize(window.innerWidth, window.innerHeight);
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
    this._mesh = null;
    this._textRenderer = null;
    this._isBound = false;
    this._eventHandlers = null;
    this._audioUrl = '';
    this._audioLoop = false;
    this._audioVolume = 1;
    this._isPlaying = false;
    this._archive = '';
    this._sceneName = '';
    this._fields = {};
    this._xapChildren = []; // ordered list of XAPNodes from children[] and buildScene
    this._fieldNodes = {}; // node valued fields such as a level's control, path and shell
    this._fieldLists = {}; // multi node fields such as a switch's choices
    this._ownedNodes = []; // nodes of the scene this inline or root loads
    this._populated = false;
    this._derivedObject = null;
    this._inlineScope = null;
    this.scope = null;
    this.parentNode = null;
    this._native = null;
    this._eventSources = null;
    this._transportMode = 0;
    this._startedAt = 0;
    this._pausedAt = 0;

    if (data.fields) {
      for (const [key, value] of Object.entries(data.fields)) {
        this._fields[key] = value;
      }
    }

    this.proxy = new Proxy(this, {
      get: (target, prop) => {
        if (typeof prop === 'symbol') return target[prop];
        const s = String(prop);

        if (s === '__xapNode') return target;
        if (s === 'children') return target._getChildProxies();
        if (s === 'transportMode') return target._transportMode;
        if (s === 'text') return target._textRenderer ? target._textRenderer.text : (target._fields.text || '');
        if (s === 'string') return target._fields.string || '';
        if (s === 'isBound' && target.data.type === 'Joystick') return target._isBound;
        if (s === 'volume') return target._audioVolume;
        if (s === 'isActive' && target._audioUrl !== undefined && target.data.type === 'AudioClip') return target._isPlaying;

        const cls = target.runtime.natives[target.data.type];
        if (cls) {
          if (cls.$props && cls.$props[s]) return cls.$props[s].get(target);
          if (typeof cls[s] === 'function' && !s.startsWith('$')) return (...args) => cls[s](target, ...args);
        }
        if (s === 'Play') return () => target._play();
        if (s === 'Stop') return () => target._stop();
        if (s === 'SetVolume') return (v) => target._setVolume(v);
        if (s === 'length') return target._getChildProxies().length;

        if (s === 'appearance') return target._getAppearanceProxy();
        if (s === 'material') return target._getMaterialProxy();
        if (s === 'texture') return target._getTextureProxy();
        if (s === 'geometry') return target._getGeometryProxy();

        if (target._fields[s] !== undefined) return target._fields[s];
        const field = RUNTIME_FIELD_CLASSES.has(target.data.type) ? null : target.runtime.derived.fieldByName(target.data.type, s);
        if (field) return target.readDerivedField(field);

        const child = target._findChildByDef(s);
        if (child) return child.proxy;

        if (target.scope && target.scope.has(s)) return target.scope.get(s);

        return undefined;
      },

      set: (target, prop, value) => {
        const s = String(prop);

        const cls = target.runtime.natives[target.data.type];
        if (cls && cls.$props && cls.$props[s]) {
          cls.$props[s].set(target, value);
          return true;
        }
        if (s === 'brightness' && target.data.type === 'Screen') {
          target._writeField(s, Number(value));
          target.runtime.renderer.domElement.style.filter = `brightness(${Number(value)})`;
          return true;
        }
        if (s === 'url' && target.data.type === 'AudioClip') {
          target._audioUrl = String(value);
          target._fields.url = value;
          return true;
        }
        if (s === 'text') {
          target._fields.text = value;
          if (target._textRenderer) target._textRenderer.setText(String(value));
          return true;
        }
        if (s === 'string') {
          target._fields.string = value;
          return true;
        }
        if (s === 'isBound' && target.data.type === 'Joystick') {
          target.runtime.bindJoystick(target, !!value);
          return true;
        }
        if (s === 'volume') {
          target._audioVolume = Number(value);
          if (target._gainNode) {
            target._gainNode.gain.value = Number(value);
          }
          return true;
        }

        if (target.runtime.derived.fieldByName(target.data.type, s)) {
          target._writeField(s, value);
          return true;
        }

        if (target.scope && target.scope.has(s)) {
          target.scope.set(s, value);
          return true;
        }

        target._writeField(s, value);
        return true;
      }
    });
  }

  _getChildProxies() {
    return this._xapChildren.map(n => n.proxy);
  }

  // The nodes a multi node field holds
  fieldNodeList(name) {
    if (name === 'children') return this._xapChildren;
    if (this._fieldLists[name]) return this._fieldLists[name];
    if (this._fieldNodes[name]) return [this._fieldNodes[name]];
    return [];
  }

  _findChildByDef(name) {
    const searchNode = (xapNode) => {
      if (xapNode.data.def === name) return xapNode;
      for (const child of xapNode._xapChildren) {
        const found = searchNode(child);
        if (found) return found;
      }
      for (const child of Object.values(xapNode._fieldNodes)) {
        const found = searchNode(child);
        if (found) return found;
      }
      return null;
    };

    for (const child of [...this._xapChildren, ...Object.values(this._fieldNodes)]) {
      const found = searchNode(child);
      if (found) return found;
    }

    if (this.scope) {
      const scoped = this.scope.lookupDef(name);
      if (scoped) return scoped;
    }
    if (this.runtime.nodes[name]) return this.runtime.nodes[name];

    return null;
  }

  _fieldNodeProxy(name) {
    const data = extractNode(this._fields[name]);
    return data ? this.runtime.nodeFor(data, this.archiveName).proxy : undefined;
  }

  _getAppearanceProxy() { return this._fieldNodeProxy('appearance'); }
  _getMaterialProxy() { return this._fieldNodeProxy('material'); }
  _getTextureProxy() { return this._fieldNodeProxy('texture'); }
  _getGeometryProxy() { return this._fieldNodeProxy('geometry'); }

  // A field value the executable's object holds: a scene value, a hook written slot or the constructor default
  readDerivedField(field) {
    const d = this.runtime.derived;
    const obj = this.derivedObject();
    const v = d.readField(obj, field.off, field.type === FIELD_BOOL ? 1 : 4);
    switch (field.type) {
      case FIELD_BOOL: return !!(d.int(v) & 0xFF);
      case FIELD_INT: return d.int(v) | 0;
      case FIELD_NUMBER: return typeof v === 'number' ? floatFromBits(v) : d.scalar(v);
      case FIELD_STRING: return v && v.str !== undefined ? v.str : '';
      case FIELD_NODE: return v && v.obj !== undefined && v.obj.node ? v.obj.node.proxy : undefined;
    }
    return v;
  }

  // Hands every scene file field to the class hook, which derives the state the field implies
  applyFieldHooks() {
    const d = this.runtime.derived;
    if (!this.data.fields || !d.types[this.data.type] || RUNTIME_FIELD_CLASSES.has(this.data.type)) return;
    const obj = this.derivedObject();
    for (const [name, value] of Object.entries(this.data.fields)) {
      const field = d.fieldByName(this.data.type, name);
      if (!field || !HOOKED_FIELD_TYPES.has(field.type)) continue;
      if (!d.hasFieldHook(this.data.type)) throw new Error(`${this.data.type} field ${name} is set by the scene but its field hook is not summarized`);
      const hookValue = field.type === FIELD_NODE ? this.fieldNodeValue(value) : value;
      d.setField(obj, field, hookValue, this.runtime.device);
    }
  }

  // The object a node valued field refers to
  fieldNodeValue(value) {
    const data = extractNode(value);
    return data ? { obj: this.runtime.nodeFor(data, this.archiveName).derivedObject() } : 0;
  }

  // Stores a scene field the way the executable's field writer does, its class hook running first
  _writeField(name, value) {
    const d = this.runtime.derived;
    const field = d.fieldByName(this.data.type, name);
    const other = value && typeof value === 'object' && value.__xapNode;
    if (other) value = { _node: other.data };
    if (field && !RUNTIME_FIELD_CLASSES.has(this.data.type)) {
      if (!d.hasFieldHook(this.data.type)) throw new Error(`${this.data.type} field ${name} written by a script but its field hook is not summarized`);
      const hookValue = field.type === FIELD_NODE ? this.fieldNodeValue(value) : value;
      const store = d.setField(this._derivedObject || this.derivedObject(), field, hookValue, this.runtime.device);
      if (store) this.storeField(field, value);
    }
    this._fields[name] = value;
    if (this.data.type === 'ImageTexture' && name === 'url') {
      this.derivedObject().slots.set(d.draw.texture_field, { xapTexture: this.runtime.loadTexture(String(value)) });
    }
  }

  // The generic field store the executable performs after the hook allows it
  storeField(field, value) {
    const obj = this.derivedObject();
    switch (field.type) {
      case FIELD_BOOL: obj.slots.set(field.off, value ? 1 : 0); break;
      case FIELD_INT: obj.slots.set(field.off, Number(value) >>> 0); break;
      case FIELD_NUMBER: obj.slots.set(field.off, bitsFromFloat(Number(value))); break;
      case FIELD_STRING: obj.slots.set(field.off, { str: String(value) }); break;
      case FIELD_NODE: obj.slots.set(field.off, this.fieldNodeValue(value)); break;
      case FIELD_VEC3: case FIELD_COLOR: obj.slots.set(field.off, Float32Array.from([Number(value[0]), Number(value[1]), Number(value[2])])); break;
      case FIELD_ROTATION: obj.slots.set(field.off, Float32Array.from([Number(value[0]), Number(value[1]), Number(value[2]), Number(value[3])])); break;
      default: obj.slots.delete(field.off);
    }
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
    if (type === FIELD_NODE) {
      const data = extractNode(v);
      return data ? { obj: this.runtime.nodeFor(data, this.archiveName).derivedObject() } : 0;
    }
    if (type === FIELD_STRING) return String(Array.isArray(v) ? v[0] : v);
    if (type === FIELD_BOOL) return !!v;
    if (type === FIELD_VEC3 || type === FIELD_ROTATION || type === FIELD_COLOR) return Array.isArray(v) ? v : [v, 0, 0, 0];
    return asNumber(v);
  }

  // Seconds into the clip the voice has played
  _playbackSeconds() {
    const rt = this.runtime;
    if (!rt.audioCtx || this._transportMode === 0) return 0;
    const at = this._transportMode === 2 ? this._pausedAt : rt.audioCtx.currentTime;
    return Math.max(0, at - this._startedAt);
  }

  _setTransportMode(mode) {
    if (this._transportMode === mode) return;
    this._transportMode = mode;
    this.runtime.fireEvent(this, 'OnTransportModeChanged');
  }

  _pause() {
    const rt = this.runtime;
    if (!this._source || !rt.audioCtx) return;
    if (this._transportMode === 2) {
      rt.audioCtx.resume();
      this._startedAt += rt.audioCtx.currentTime - this._pausedAt;
      this._gainNode.gain.value = this._audioVolume;
      this._setTransportMode(1);
    } else if (this._transportMode === 1) {
      this._pausedAt = rt.audioCtx.currentTime;
      this._gainNode.gain.value = 0;
      this._setTransportMode(2);
    }
  }

  _play() {
    if (!this._audioUrl || /^(cd|st):/i.test(this._audioUrl)) {
      this._isPlaying = false;
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
          if (!resp.ok) return;
          const arrayBuf = await resp.arrayBuffer();
          rt.audioBuffers[cacheKey] = await rt.audioCtx.decodeAudioData(arrayBuf);
        }

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
        self._startedAt = rt.audioCtx.currentTime;
        self._setTransportMode(1);
        rt.playingClips.add(self);
        source.onended = () => {
          if (self._source !== source) return;
          self._isPlaying = false;
          rt.playingClips.delete(self);
          self._setTransportMode(0);
          if (!self._audioLoop) rt.fireEvent(self, 'OnEndOfAudio');
        };
      } catch (e) {
        _warn(`[XAP] audio ${self._audioUrl}:`, e.message);
      }
    })();
  }

  _stop() {
    this._isPlaying = false;
    if (this._source) {
      const source = this._source;
      this._source = null;
      try { source.stop(); } catch (_) {}
    }
    this.runtime.playingClips.delete(this);
    this._setTransportMode(0);
  }

  _setVolume(v) {
    this._audioVolume = Number(v);
    if (this._gainNode) {
      this._gainNode.gain.value = this._audioVolume;
    }
  }
}

// ============================================================================
// Script scopes — one per loaded scene, resolving names up through the scene that inlined it
// ============================================================================

// The engine's dialect calls length as a function on strings and child lists
function rewriteScript(src) {
  return src.replace(/\.length\s*\(\s*\)/g, '.length');
}

class ScriptScope {
  constructor(runtime, parent, owner) {
    this.runtime = runtime;
    this.parent = parent;
    this.owner = owner;
    this.names = new Set();
    this.defs = new Map();
    this.exports = null;
    this.initialized = false;
    // Inside the scene body only ancestor names come from the proxy, own names are its locals
    this.proxy = new Proxy(Object.create(null), {
      has: (t, k) => typeof k === 'string' && !this.names.has(k) && (this.defs.has(k) || (this.parent !== null && this.parent.resolves(k))),
      get: (t, k) => {
        if (typeof k !== 'string') return undefined;
        if (this.defs.has(k)) return this.defs.get(k).proxy;
        return this.parent !== null ? this.parent.lookup(k) : undefined;
      },
      set: (t, k, v) => typeof k === 'string' && !this.defs.has(k) && this.parent !== null && this.parent.assign(k, v),
    });
    // Handlers and behaviors on nodes see their own scene's names as well
    this.handlerProxy = new Proxy(Object.create(null), {
      has: (t, k) => typeof k === 'string' && this.resolves(k),
      get: (t, k) => (typeof k === 'string' ? this.lookup(k) : undefined),
      set: (t, k, v) => typeof k === 'string' && this.assign(k, v),
    });
  }

  name() {
    return this.owner && this.owner.data.def ? this.owner.data.def : (this.owner ? this.owner.data.type : 'entry');
  }

  // Top level declarations are one script item each, a function or a var statement
  declare(scripts) {
    for (const src of scripts) {
      const trimmed = src.trimStart();
      const fn = trimmed.match(/^function\s+([A-Za-z_$][\w$]*)/);
      if (fn) { this.names.add(fn[1]); continue; }
      const vars = trimmed.match(/^var\s+([^;]+)/);
      if (!vars) continue;
      for (const part of vars[1].split(',')) {
        const name = part.trim().split(/[\s=]/)[0];
        if (name) this.names.add(name);
      }
    }
  }

  has(k) {
    return this.names.has(k) || this.defs.has(k);
  }

  resolves(k) {
    return this.has(k) || (this.parent !== null && this.parent.resolves(k));
  }

  ownerOf(k) {
    if (this.has(k)) return this;
    return this.parent !== null ? this.parent.ownerOf(k) : null;
  }

  // A DEF name resolves to the node of the nearest scene declaring it
  lookupDef(k) {
    for (let sc = this; sc !== null; sc = sc.parent) {
      const node = sc.defs.get(k);
      if (node) return node;
    }
    return null;
  }

  lookup(k) {
    const o = this.ownerOf(k);
    return o ? o.get(k) : undefined;
  }

  assign(k, v) {
    const o = this.ownerOf(k);
    if (!o) return false;
    o.set(k, v);
    return true;
  }

  get(k) {
    const binding = this.exports && this.exports[k];
    if (binding) return binding[0]();
    const node = this.defs.get(k);
    return node ? node.proxy : undefined;
  }

  set(k, v) {
    const binding = this.exports && this.exports[k];
    if (!binding) throw new Error(`scene ${this.name()} declares no ${k}`);
    binding[1](v);
  }

  // Runs the scene's scripts as one function body whose declarations become live bindings
  evaluate(scripts) {
    const body = scripts.map(rewriteScript).join('\n');
    const bindings = [...this.names].map((n) => `${JSON.stringify(n)}: [function () { return ${n}; }, function (v) { ${n} = v; }]`).join(',\n');
    const make = new Function('__scope', `with (__scope) {\n${body}\nreturn {${bindings}};\n}`);
    this.exports = make(this.proxy);
  }
}

// Dates the scripts construct read the console clock and gain the engine's calendar members
function installDate(vc) {
  const NativeDate = Date;
  class ConsoleDate extends NativeDate {
    constructor(...args) {
      if (args.length === 0) super(vc.now());
      else super(...args);
    }
  }
  ConsoleDate.now = () => vc.now();
  Object.assign(ConsoleDate.prototype, dateMembers(vc));
  window.Date = ConsoleDate;
}

// Idle time starts the screen saver node's OnStart, input ends it with OnEnd
class ScreenSaverTimer {
  constructor(runtime, node) {
    this.runtime = runtime;
    this.node = node;
    this.idle = 0;
    this.active = false;
  }

  activity() {
    this.idle = 0;
    if (!this.active) return;
    this.active = false;
    this.runtime.fireEvent(this.node, 'OnEnd');
  }

  reset() {
    this.idle = 0;
  }

  // Scene values win, the constructor's defaults stand in for omitted ones
  field(name) {
    const v = this.node._fields[name];
    return v === undefined ? this.runtime.derived.defaultField('ScreenSaver', name) : v;
  }

  tick(dt) {
    if (!this.field('enabled') || this.active) return;
    this.idle += dt;
    if (this.idle < Number(this.field('delay'))) return;
    this.active = true;
    this.runtime.fireEvent(this.node, 'OnStart');
  }
}

// ============================================================================
// Text Renderer — glyph layout following the executable's text draw routine
// ============================================================================

const TEXT_LINE_ADVANCE = 1.0, TEXT_SOFT_LINE_ADVANCE = 1.5, TEXT_SCROLL_GAP = 1.5, TEXT_COLUMN_FADE = 2.0, TEXT_OVERFLOW_FADE = 1.0, TEXT_BOX_MARGIN = 2.0;
const TEXT_SMALL_GLYPH_INDEX = 190, TEXT_SMALL_GLYPH_SCALE = 0.9, TEXT_HIGHLIGHT_ALPHA = 0.5;
const CLOCK_TOKEN = '<clock>';
const TAB = String.fromCharCode(9), CR = String.fromCharCode(13), LF = String.fromCharCode(10);

class TextRenderer {
  constructor(runtime, node, fontName, fontCache, material, justify) {
    this.runtime = runtime;
    this.node = node;
    this.fontName = fontName;
    this.fontCache = fontCache;
    this.material = material;
    this.justify = justify;
    this.text = '';
    this.builtKey = null;
    this.bbox = { minX: 0, maxX: 0, minY: 0, maxY: 0 };
    this.scrollStart = 0;
    this.layoutMesh = null;
    this.basePositions = null;
    this.alphas = null;
    this.setText(String(node._fields.text ?? ''));
  }

  setText(text) {
    this.text = text;
    this.node._fields.scroll = 0;
    this.scrollStart = 0;
  }

  // Scene values win, constructor defaults stand in for omitted fields
  field(name) {
    const v = this.node._fields[name];
    return v === undefined ? this.runtime.derived.defaultField('Text', name) : v;
  }

  // The string the glyphs spell: the clock token becomes the time, translated text comes from the tables
  displayString() {
    if (this.text.startsWith(CLOCK_TOKEN)) return this.runtime.console.formatClock();
    return this.field('translate') ? this.runtime.console.translate(this.runtime.console.language, this.text) : this.text;
  }

  // Lays the text out again when its string, language, width or font changed
  update() {
    const str = this.displayString();
    const width = asNumber(this.field('width'));
    const key = `${str} ${width} ${this.fontName}`;
    if (key !== this.builtKey) {
      this.builtKey = key;
      this.rebuild(str, width);
    }
    this.animate();
  }

  rebuild(str, width) {
    if (this.layoutMesh) this.layoutMesh.geometry.dispose();
    this.layoutMesh = null;
    const role = this.fontName.toLowerCase() === 'heading' ? 'heading' : 'body';
    const fontData = this.fontCache[role];
    if (!fontData) throw new Error(`font ${this.fontName} is not loaded`);
    const laid = layoutText(str, fontData, width);
    this.bbox = laid.bbox;
    if (laid.positions.length === 0) return;
    this.basePositions = Float32Array.from(laid.positions);
    this.alphas = Float32Array.from(laid.alphas);
    const geometry = new THREE.BufferGeometry();
    geometry.setAttribute('a_0', new THREE.BufferAttribute(Float32Array.from(laid.positions), 3));
    geometry.setAttribute('a_12', new THREE.BufferAttribute(new Float32Array(laid.alphas.length * 4), 4));
    geometry.setIndex(laid.indices);
    this.layoutMesh = new THREE.Mesh(geometry, this.material);
    detachFromThree(this.layoutMesh);
    this.applyAlpha(this.alphas, 0, 0);
  }

  // Writes the glyph alpha and offsets the glyphs for justification and scroll
  applyAlpha(alphas, x, y) {
    const diffuse = this.layoutMesh.geometry.attributes.a_12;
    for (let i = 0; i < alphas.length; i++) diffuse.setXYZW(i, 1, 1, 1, alphas[i]);
    diffuse.needsUpdate = true;
    this.layoutMesh.matrix.makeTranslation(x, y, 0);
  }

  // Positions the text the way the draw routine does: justified and scrolled, fading at the box edges
  animate() {
    if (!this.layoutMesh) return;
    const width = asNumber(this.field('width'));
    let extent = this.bbox.maxX - this.bbox.minX;
    if (width !== 0 && Math.abs(width) < extent) extent = Math.abs(width);
    let x = 0;
    const justify = String(this.justify).toLowerCase();
    if (justify === 'middle') x = -0.5 * extent;
    else if (justify === 'end') x = -extent;
    const height = asNumber(this.field('height'));
    const textHeight = this.bbox.maxY - this.bbox.minY;
    const scroll = asNumber(this.node._fields.scroll ?? 0);
    if (height === 0 || textHeight <= height) {
      this.applyAlpha(this.alphas, x, scroll);
      return;
    }
    const top = TEXT_LINE_ADVANCE - scroll + (scroll === 0 ? TEXT_LINE_ADVANCE : 0);
    const bottom = TEXT_LINE_ADVANCE - scroll - height - TEXT_BOX_MARGIN;
    const faded = new Float32Array(this.alphas.length);
    for (let i = 0; i < faded.length; i++) faded[i] = this.alphas[i] * scrollFade(this.basePositions[3 * i + 1], top, bottom);
    this.applyAlpha(faded, x, scroll);
  }

  // Scrolls text taller than its box after the delay, wrapping past the end with a gap
  tick(dt) {
    const rate = asNumber(this.field('scrollRate'));
    if (rate === 0 || !this.layoutMesh) return;
    const now = this.runtime.clock.elapsedTime;
    if (this.scrollStart === 0) this.scrollStart = now + asNumber(this.field('scrollDelay'));
    if (now < this.scrollStart) return;
    const height = asNumber(this.field('height'));
    const textHeight = this.bbox.maxY - this.bbox.minY;
    if (textHeight <= height) return;
    let scroll = asNumber(this.node._fields.scroll ?? 0) + dt * height * rate;
    if (scroll > textHeight + TEXT_SCROLL_GAP) scroll -= textHeight + TEXT_SCROLL_GAP;
    this.node._fields.scroll = scroll;
  }
}

// Alpha of a glyph vertex at a height inside a scrolled box, fading over one line at both edges
function scrollFade(y, top, bottom) {
  if (y > top || y < bottom) return 0;
  return Math.min(1, top - y, y - bottom);
}

// Places glyph outlines like the executable: lines wrap at a positive width, overflow fades out at the edge,
// a tab starts a right aligned column and highlight markers halve the alpha of the text between them
function layoutText(str, fontData, width) {
  const glyphs = fontData.glyphs;
  const wrap = width > 0 ? width : 0;
  const limit = Math.abs(width);
  const positions = [], alphas = [], indices = [];
  const chars = [...str];
  const measure = measureColumns(chars, glyphs, limit);
  let x = 0, y = 0, vertexCount = 0;
  let lineStartVertex = 0, column = 1, lastSpace = -1, lastSpaceVertex = 0;
  let overflow = width < 0, highlight = false, hadTab = false;
  let minX = 0, maxX = 0, minY = 0, maxY = 0;
  const finishLine = (hardBreak) => {
    if (overflow && column === 1) fadeRange(positions, alphas, lineStartVertex, vertexCount, measure.lineWidth - TEXT_OVERFLOW_FADE, measure.lineWidth);
    x = 0;
    y -= hardBreak || hadTab || highlight ? TEXT_LINE_ADVANCE : TEXT_SOFT_LINE_ADVANCE;
    lineStartVertex = vertexCount;
    lastSpace = -1;
    column = 1;
    overflow = width < 0;
  };
  for (let i = 0; i < chars.length; i++) {
    const ch = chars[i];
    if (ch === '*' && markerAt(chars, i)) {
      highlight = !highlight;
      continue;
    }
    if (ch === CR || ch === LF) {
      if (ch === CR && chars[i + 1] === LF) i++;
      finishLine(ch === CR);
      continue;
    }
    if (ch === TAB && width >= 0) {
      if (overflow && column === 1) fadeRange(positions, alphas, lineStartVertex, vertexCount, measure.lineWidth - TEXT_COLUMN_FADE, measure.lineWidth);
      column = 2;
      hadTab = true;
      x = measure.lineWidth - measure.column2;
      continue;
    }
    const glyph = glyphs[ch];
    if (!glyph) continue;
    const scale = glyph.i > TEXT_SMALL_GLYPH_INDEX ? TEXT_SMALL_GLYPH_SCALE : 1;
    const advance = glyph.adv * scale;
    if (wrap > 0 && ch !== ' ' && column === 1 && x + advance > wrap) {
      if (lastSpace >= 0) {
        positions.length = lastSpaceVertex * 3;
        alphas.length = lastSpaceVertex;
        indices.length = indexCountFor(indices, lastSpaceVertex);
        vertexCount = lastSpaceVertex;
        i = lastSpace;
        x = 0;
        y -= TEXT_LINE_ADVANCE;
        lineStartVertex = vertexCount;
        lastSpace = -1;
        continue;
      }
      overflow = true;
    }
    if (glyph.verts && glyph.idx && glyph.idx.length > 0) {
      for (let k = 0; k < glyph.verts.length; k += 2) {
        const px = glyph.verts[k] * scale + x, py = glyph.verts[k + 1] * scale + y;
        positions.push(px, py, 0);
        alphas.push(highlight ? TEXT_HIGHLIGHT_ALPHA : 1);
        minX = Math.min(minX, px); maxX = Math.max(maxX, px); minY = Math.min(minY, py); maxY = Math.max(maxY, py);
      }
      for (let k = 0; k < glyph.idx.length; k++) indices.push(glyph.idx[k] + vertexCount);
      vertexCount += glyph.verts.length / 2;
    }
    x += advance;
    if (ch === ' ') { lastSpace = i; lastSpaceVertex = vertexCount; }
  }
  if (overflow && column === 1) fadeRange(positions, alphas, lineStartVertex, vertexCount, measure.lineWidth - TEXT_OVERFLOW_FADE, measure.lineWidth);
  return { positions, alphas, indices, bbox: { minX, maxX, minY, maxY } };
}

// A highlight marker is an asterisk whose run ends with another asterisk before a tab, newline or the end
function markerAt(chars, i) {
  for (let k = i + 1; k < chars.length; k++) {
    const c = chars[k];
    if (c === LF || c === CR || c === TAB) return k - 1 > i && chars[k - 1] === '*';
  }
  return chars.length - 1 > i && chars[chars.length - 1] === '*';
}

// Measures the widest first and second column so overflow and tab stops know the line width
function measureColumns(chars, glyphs, limit) {
  let col1 = 0, col2 = 0, x1 = 0, x2 = 0, column = 1, sawTab = false;
  for (let i = 0; i < chars.length; i++) {
    const ch = chars[i];
    if (ch === CR || ch === LF) { col1 = Math.max(col1, x1); col2 = Math.max(col2, x2); x1 = 0; x2 = 0; column = 1; continue; }
    if (ch === TAB) { column = 2; sawTab = true; continue; }
    if (ch === '*' && markerAt(chars, i)) continue;
    const glyph = glyphs[ch];
    if (!glyph) continue;
    const advance = glyph.adv * (glyph.i > TEXT_SMALL_GLYPH_INDEX ? TEXT_SMALL_GLYPH_SCALE : 1);
    if (column === 1) x1 += advance; else x2 += advance;
  }
  col1 = Math.max(col1, x1); col2 = Math.max(col2, x2);
  let lineWidth = col1;
  if (limit !== 0) lineWidth = sawTab ? (col1 + col2 > limit ? limit - col2 : lineWidth) : limit;
  return { lineWidth, column2: col2 };
}

// Fades vertices out between two x positions, the way the draw dims text past its width
function fadeRange(positions, alphas, from, to, start, end) {
  const span = end - start;
  for (let i = from; i < to; i++) {
    const t = span > 0 ? Math.min(1, Math.max(0, (positions[3 * i] - start) / span)) : (positions[3 * i] > start ? 1 : 0);
    alphas[i] *= 1 - t;
  }
}

function indexCountFor(indices, vertexCount) {
  let n = indices.length;
  while (n > 0 && indices[n - 1] >= vertexCount) n--;
  return n;
}

// ============================================================================
// Helper functions
// ============================================================================

// Text glyph geometry carries positions only, drawn through the fixed function path
const TEXT_GEOMETRY = describeGeometry({ format: 0, stride: 28, attributes: [{ register: 0, semantic: 'position', type: 'FLOAT3', offset: 0, comps: 3 }, { register: 3, semantic: 'diffuse', type: 'FLOAT4', offset: 12, comps: 4 }] });

// Three must not cull by or flip winding from a mesh world matrix the derived shaders ignore
function detachFromThree(mesh) {
  mesh.frustumCulled = false;
  mesh.matrixAutoUpdate = false;
  mesh.matrixWorldAutoUpdate = false;
}

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
