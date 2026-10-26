import * as THREE from 'three';
import { DerivedModel, cloneState, makeBuffer, SceneObject, VertexBuffer, IndexBuffer, MatrixStack, bitsFromFloat, ARCHIVE_TYPE, HEAP_TYPE, SCRIPT_VALUE_TYPE, ARRAY_ELEMENT } from './derived.js';
import { DevicePipeline, describeGeometry, describeFVF, primitiveTriangles, decodeTexels, paletteBytes } from './pipeline.js';
import { VirtualConsole, nativeClasses, dateMembers } from './system.js';

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

// Script objects the engine runs as the host's own, their members the ones the executable registers
const SCRIPT_OBJECT_PROTOTYPES = { Date: Date.prototype, Array: Array.prototype };

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
// Classes whose fields drive subsystems the runtime provides itself: audio voices, controller input and the idle timer
const RUNTIME_FIELD_CLASSES = new Set(['AudioClip', 'PeriodicAudioGroup', 'Joystick', 'ScreenSaver']);

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
    this.streamMeshes = new Map(); // vertex buffer → mesh drawn from it
    this.deviceTextures = new Map(); // the description and Three texture of each texture header the executable created
    this.unswizzleTables = new Map(); // the swizzled texel each texel reads, by level size
    this.rangeMeshes = new Map();  // loaded buffer → mesh drawn from a run of its indices
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

    // Audio
    this.audioCtx = null;
    this.audioBuffers = {};
    this.playingClips = new Set();
    this.screenSaver = null;
    this.meshLoads = [];

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
      const memoryResp = await fetch(`${DATA_BASE}/${renderModel.memory.file}`);
      const startupMemory = new Uint8Array(await memoryResp.arrayBuffer());
      this.console = new VirtualConsole(renderModel.system);
      installDate(this.console);
      this.derived = new DerivedModel(renderModel, this.deviceHost(), startupMemory);
      // Routines and library calls named in the page's trace parameter log every call they get
      for (const name of (new URLSearchParams(window.location.search).get('trace') || '').split(',').filter(Boolean)) {
        this.derived.traced.add(/^[0-9A-Fa-f]+$/.test(name) ? parseInt(name, 16) : name);
      }
      // The page's verify parameter checks every compiled expression against its interpretation
      this.derived.verify = new URLSearchParams(window.location.search).has('verify');
      // The page's interp parameter evaluates expressions by interpretation instead of their compiled forms
      this.derived.interpret = new URLSearchParams(window.location.search).has('interp');
      this.pipeline = new DevicePipeline(THREE, renderModel, { resolve: (h, palette) => this.resolveTextureHandle(h, palette) });
      this.device = cloneState(this.derived.defaults);
      this.derived.onDraw = (state, env, quad) => this.onDerivedDraw(state, env, quad);
      this.natives = { ...nativeClasses(this), ...this.runtimeNativeClasses() };
      this.installHooks();
      this.checkNatives(renderModel.system.methods);
      for (const t of renderModel.textures) {
        const key = 'texture:' + (t.va >>> 0).toString(16).toUpperCase().padStart(8, '0');
        this.proceduralTextures[key] = this.loadTexture(`proc_${key.slice(8)}.png`);
        // The producer's pixels were captured at derivation, so the call hands back the exported texture.
        this.derived.hook(t.va, () => ({ texture: key }));
      }
      _log(`[XBE] ${(startupMemory.length / 1024) | 0} KB of startup memory in ${renderModel.memory.sections.length} sections`);
      _log(`[XBE] ${renderModel.materials.length} materials, ${Object.keys(renderModel.programs).length} vertex programs, ${renderModel.textures.length} procedural textures, ${Object.keys(renderModel.draw.functions).length} routines`);

      await this.loadConsoleFiles();

      const loadPromises = this.config.scenes.map(name => this.loadSceneData(name));
      await Promise.allSettled(loadPromises);
      _log(`[XAP] ${Object.keys(this.sceneData).length} scenes loaded`);

      // The static initializers run first, the way the CRT init runs them before main, then the device init runs
      // on the executable's renderer object before any scene is built, and the startup creates the matrix stack the
      // node draws compose their world matrices on.
      for (const va of this.derived.scene.initializers) this.runStartup(va);
      // The startup links the drive letters and makes the data directories before the renderer is initialized
      this.runStartup(this.derived.scene.disk);
      this.runRenderer(this.derived.scene.init);
      // The main load fills the caller's path buffer with the scene file it opens, taking the renderer again where
      // its startup passes that too
      const sc = this.derived.scene;
      this.runRenderer(sc.main_load, sc.main_load_args.map((role) => (role === 'renderer' ? sc.renderer : { obj: new SceneObject(HEAP_TYPE, null), off: 0 })));
      this.derived.memory.store(this.derived.scene.matrix_stack, 4, { matrixStack: new MatrixStack() });
      this.rootGlobal = this.findRootGlobal();

      await this.loadScene(this.config.entry);
      _log(`[XAP] ${Object.keys(this.nodes).length} nodes registered`);

      this.executeScripts();
      this.derived.memory.store(this.rootGlobal, 4, { obj: this.root });
      this.runEntryInitialize();
      // The console's loader fills the scene before the first frame, so no frame draws a mesh still loading
      await this.settleMeshes();

      document.getElementById('loading').classList.add('hidden');
      this.animate();
    } catch (err) {
      _error('Runtime start failed:', err);
      document.getElementById('loading').textContent = 'Error: ' + err.message;
    }
  }

  // Waits for the meshes the scenes being built asked for, the ones they start in turn included
  async settleMeshes() {
    while (this.meshLoads.length) {
      const pending = this.meshLoads;
      this.meshLoads = [];
      await Promise.allSettled(pending);
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
      onPresent: (state) => this.presentFrame(state),
    };
  }

  // The present parameters are the buffer the init hands the device creation
  presentParamsAddress() {
    if (!this.presentParams) throw new Error('the device was created with no present parameters');
    return this.presentParams;
  }

  // Runs a startup routine that takes no object, as the CRT init calls the static initializers
  runStartup(va) {
    const parent = this.derived.newEnv(null);
    parent.state = this.device;
    return this.derived.run(va, null, [], this.device, parent);
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
    d.hook(sc.group.draw, (thisVal, args, state, env) => { this.drawChildren(this.objectOf(thisVal), state, env); return 0; });
    d.hook(sc.group.tick, (thisVal, args, state, env) => { this.tickChildren(this.objectOf(thisVal), args[0], state, env); return 0; });
    const element = (thisVal, args) => this.arrayElement(thisVal, d.int(args[0]));
    if (sc.array_element) d.hook(sc.array_element, element);
    d.hook(ARRAY_ELEMENT, element);
    d.hook(sc.fire_event, (thisVal, args) => this.fireDerivedEvent(args[0], d.string(args[1])));
    d.hook(sc.present, (thisVal, args, state) => { this.presentFrame(state); return 0; });
    d.hook(sc.script_tick, () => 0);
    // The ticks of the classes the runtime drives itself advance audio voices, input and glyph scrolling here.
    for (const cls of RUNTIME_FIELD_CLASSES) {
      const slots = sc.slots[cls];
      if (slots && slots.tick && slots.tick !== sc.group.tick) d.hook(slots.tick, () => 0);
    }
    for (const va of sc.tick_hooks || []) if (va !== sc.timers.advance) d.hook(va, () => 0);
    // The controllers are read by the runtime's own input system, so the executable's input layer has nothing to do
    for (const va of sc.input || []) d.hook(va, () => 0);
    d.hook(sc.timers.advance, (thisVal, args, state, env) => { this.advanceTimers(state, env); return 0; });
    d.hook(sc.timers.remove, (thisVal, args) => { this.removeTimers(args[0]); return 0; });
    d.hook(sc.loader, (thisVal, args) => this.loadArchive(this.stringOf(thisVal, args)));
    d.hook(sc.unloader, () => 0);
    for (const va of sc.image_load || []) d.hook(va, () => 0);
    d.hook(sc.scene_load.reset, () => 0);
    d.hook(sc.scene_load.join.va, (thisVal, args, state, env) => this.joinPath(thisVal, args, env));
    d.hook(sc.scene_load.populate, (thisVal, args) => { this.populateInline(this.receiverOf(thisVal, args).node); return 1; });
    d.hook(sc.scene_load.new_scope, (thisVal, args) => ({ obj: new SceneObject(SCOPE_TYPE, this.receiverOf(thisVal, args).node), off: 0 }));
    d.hook(sc.scene_load.attach, () => 0);
    // The runtime loads geometry and image files itself, so a node's load hands back the buffer it holds and nothing
    // else, except in a class that creates a device texture its own routines fill, whose load runs from the image
    for (const nt of Object.values(d.types)) {
      const slots = nt.slots || [];
      const va = slots[d.draw.load_slot / 4];
      if (va && !nt.own_texture) d.hook(va, (thisVal) => d.readField(this.objectOf(thisVal), d.accessor.offset, 4) || 0);
    }
    // Allocators hand back fresh zeroed memory the paths then fill by field offset.
    for (const va of d.model.allocators) d.hook(va, () => ({ obj: new SceneObject(HEAP_TYPE, null), off: 0 }));
    // The device creation runs from the image, the runtime keeping the present parameters the init filled in
    d.hook = d.hook.bind(d);
    const perform = d.performCall.bind(d);
    d.performCall = (fn, argIdx, thisIdx, env, seq, target) => {
      if (fn === 'Direct3D_CreateDevice') {
        const params = d.eval(argIdx[4], env);
        if (typeof params !== 'number') throw new Error(`device created with present parameters at ${JSON.stringify(params)}`);
        this.presentParams = params;
      }
      return perform(fn, argIdx, thisIdx, env, seq, target);
    };
  }

  objectOf(v) {
    if (v && v.obj !== undefined) return v.obj;
    if (v instanceof SceneObject) return v;
    throw new Error(`hook called on ${JSON.stringify(v)} which is no scene object`);
  }

  // Joins the path a helper was handed onto its base and writes the result where its output points
  joinPath(thisVal, args, env) {
    const d = this.derived, j = d.scene.scene_load.join;
    const input = (i) => {
      const v = i === 0 ? thisVal : args[i - 1];
      return v instanceof SceneObject ? { obj: v, off: 0 } : v;
    };
    const base = input(j.base);
    const joined = joinPaths(d.string(input(j.src)), typeof base === 'number' && base === 0 ? null : d.string(base));
    d.storeString(input(j.out), joined, env);
    return input(j.out);
  }

  // The string a helper was handed, in the register or the argument its build's convention passes it in
  stringOf(thisVal, args) {
    for (const v of [thisVal, ...(args || [])]) {
      const s = this.derived.stringOrNull(v);
      if (s !== null) return s;
    }
    throw new Error(`hook called with no string in ${JSON.stringify(args)}`);
  }

  // The object a helper was handed, in the register or the argument its build's convention passes it in
  receiverOf(thisVal, args) {
    for (const v of [thisVal, ...(args || [])]) {
      if (v && v.obj !== undefined) return v.obj;
      if (v instanceof SceneObject) return v;
    }
    throw new Error(`hook called on ${JSON.stringify(thisVal)} which is no scene object`);
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

  // Steps every field interpolation record on the list through its walk, moving the cursor past the ones it leaves linked
  advanceTimers(state, env) {
    const d = this.derived, t = d.scene.timers;
    let cursor = t.head;
    for (let record = this.timerRecord(cursor); record && record.obj !== undefined; record = this.timerRecord(cursor)) {
      d.run(t.walk, null, [cursor], state, env);
      const link = this.timerRecord(cursor);
      if (link && link.obj === record.obj) cursor = { obj: record.obj, off: (record.off || 0) + t.next };
    }
  }

  // The record a cursor points at, the cursor being the list head or a record's link field
  timerRecord(cursor) {
    const d = this.derived;
    return cursor.obj === undefined ? d.memory.load(cursor, 4) : d.readField(cursor.obj, cursor.off, 4);
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
    for (const [off, value] of this.loadedArchiveFields()) archive.mem.store(off, 4, value);
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
      for (const key of ['a', 'b', 'c', 'addr', 'this']) if (n[key] !== undefined) walk(n[key]);
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

  // Fetches the font files the dashboard opens through the console's file routines, which the build copied, and
  // lays the partition the dashboard runs from out of them and the other files the build listed
  async loadConsoleFiles() {
    const files = this.config.files || [];
    const data = new Map();
    for (const name of files) {
      if (!name.toLowerCase().endsWith('.xtf')) continue;
      const resp = await fetch(`${DATA_BASE}/files/${name.replace(/\\/g, '/')}`);
      if (!resp.ok) throw new Error(`console file ${name} was not copied by the build`);
      data.set(name.toLowerCase(), new Uint8Array(await resp.arrayBuffer()));
      _log(`[XTF] Loaded ${name} for the dashboard's own font loader`);
    }
    this.derived.consoleApi.attachFiles(files, data);
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
      this.meshLoads.push(this.loadMesh(geomFields.url, archiveName).then(geometry => {
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
      }));
      return;
    }

    if (geomFields.font !== undefined) {
      // A text geometry draws itself through its own class draw, which the shape draw dispatches to
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

  // Gives a geometry node the loaded buffer object its draw path dereferences, its indices as the bytes the executable addresses
  attachBuffer(geomNode, geometry, mesh, desc) {
    const index = mesh.geometry.index.array;
    const indices = index instanceof Uint16Array ? index : Uint16Array.from(index);
    geometry.indexBuffer = new IndexBuffer(0, 2, new Uint8Array(indices.buffer, indices.byteOffset, indices.byteLength));
    const buffer = makeBuffer(this.derived.model, geometry);
    buffer.mesh = mesh;
    buffer.desc = desc;
    geomNode.derivedObject().mem.store(this.derived.accessor.offset, 4, { obj: buffer });
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
        texNode.derivedObject().mem.store(this.derived.draw.texture_field, 4, { xapTexture: tex });
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

  // Resolves a texture handle the derived paths bound at a stage, a texture the executable created through the
  // palette bound beside it
  resolveTextureHandle(handle, palette) {
    if (handle && handle.texture !== undefined) {
      const tex = this.proceduralTextures[handle.texture];
      if (!tex) throw new Error(`procedural ${handle.texture} was not exported`);
      return tex;
    }
    if (handle && handle.xapTexture !== undefined) return handle.xapTexture;
    if (handle && handle.obj !== undefined) return this.deviceTexture(handle, palette);
    throw new Error(`texture handle ${JSON.stringify(handle)} has no runtime texture`);
  }

  // The texture Three samples for one the executable created and filled, decoded again whenever physical memory changed
  deviceTexture(header, palette) {
    const phys = this.derived.memory.physical;
    let tex = this.deviceTextures.get(header.obj);
    if (!tex) {
      tex = this.describeDeviceTexture(header);
      this.deviceTextures.set(header.obj, tex);
    }
    if (tex.stamp === phys.store.writes) return tex.three;
    const texels = phys.store.bytes(phys.physical(tex.data), tex.width * tex.height * tex.bpp);
    const entries = palette === undefined ? null : this.paletteEntries(palette, tex.format);
    decodeTexels(tex.format, tex.bpp, texels, tex.unswizzled, entries, tex.three.image.data);
    tex.stamp = phys.store.writes;
    tex.three.needsUpdate = true;
    return tex.three;
  }

  // Describes the top level of a texture the executable created through the image's own routines: its description,
  // the bytes a texel of its format takes, whether that format is swizzled and the texel each swizzled one lands on
  describeDeviceTexture(header) {
    const d = this.derived, layout = d.model.surface_desc, resource = d.model.resource;
    if (!resource) throw new Error('the image carries no resource routine naming where a texture keeps its data');
    const desc = { obj: new SceneObject(HEAP_TYPE, null), off: 0 };
    if (d.imageRoutine('D3DTexture_GetLevelDesc') !== undefined) d.callImage('D3DTexture_GetLevelDesc', [header, 0, desc], this.device);
    else {
      // A build without the level description describes the level's surface, which it hands out and takes back
      const slot = { obj: new SceneObject(HEAP_TYPE, null), off: 0 };
      d.callImage('D3DTexture_GetSurfaceLevel', [header, 0, slot], this.device);
      const surface = d.readField(slot.obj, 0, 4);
      d.callImage('D3DSurface_GetDesc', [surface, desc], this.device);
      d.callImage('D3DResource_Release', [surface], this.device);
    }
    const format = d.int(d.readField(desc.obj, layout.format, 4));
    const width = d.int(d.readField(desc.obj, layout.width, 4)), height = d.int(d.readField(desc.obj, layout.height, 4));
    const bpp = d.int(d.callImage('XGBytesPerPixelFromFormat', [format], this.device));
    if (d.int(d.callImage('XGIsSwizzledFormat', [format], this.device)) === 0) throw new Error(`texture format 0x${format.toString(16)} is linear, and the GPU model reads swizzled textures by the image's unswizzle`);
    const three = new THREE.DataTexture(new Uint8Array(width * height * 4), width, height, THREE.RGBAFormat, THREE.UnsignedByteType);
    three.flipY = false;
    three.colorSpace = THREE.NoColorSpace;
    three.generateMipmaps = false;
    three.minFilter = THREE.LinearFilter;
    three.magFilter = THREE.LinearFilter;
    const data = (d.int(d.readField(header.obj, resource.data, 4)) | resource.alias) >>> 0;
    return { format, width, height, bpp, data, unswizzled: this.unswizzling(width, height, bpp), three, stamp: -1 };
  }

  // The swizzled texel each texel of a level reads, found by having the image's own XGUnswizzleRect move texels whose
  // bytes carry their own swizzled index, one byte of that index per run
  unswizzling(width, height, bpp) {
    const key = `${width}x${height}x${bpp}`;
    const known = this.unswizzleTables.get(key);
    if (known) return known;
    const d = this.derived, phys = d.memory.physical, count = width * height, bytes = count * bpp;
    const table = new Uint32Array(count);
    const src = phys.allocate(bytes, 0, 0xFFFFFFFF, 0), dst = phys.allocate(bytes, 0, 0xFFFFFFFF, 0);
    if (src === 0 || dst === 0) throw new Error(`no physical memory holds the ${bytes} bytes the unswizzle of a ${key} texture moves`);
    try {
      const from = new Uint8Array(bytes);
      for (let shift = 0; shift === 0 || (count - 1) >>> shift > 0; shift += 8) {
        for (let t = 0; t < count; t++) from.fill((t >>> shift) & 255, t * bpp, t * bpp + bpp);
        phys.store.setBytes(phys.physical(src), from);
        d.callImage('XGUnswizzleRect', [src, width, height, 0, dst, width * bpp, 0, bpp], this.device);
        const to = phys.store.bytes(phys.physical(dst), bytes);
        for (let p = 0; p < count; p++) table[p] += to[p * bpp] * 2 ** shift;
      }
    } finally {
      phys.free(src);
      phys.free(dst);
    }
    this.unswizzleTables.set(key, table);
    return table;
  }

  // The palette entries a texture of a format can index, read where the image's own resource routines reach a
  // resource's data
  paletteEntries(palette, format) {
    const d = this.derived, resource = d.model.resource, phys = d.memory.physical;
    if (!palette || palette.obj === undefined) throw new Error(`palette ${JSON.stringify(palette)} is no resource the executable created`);
    const at = (d.int(d.readField(palette.obj, resource.data, 4)) | resource.alias) >>> 0;
    return phys.store.bytes(phys.physical(at), paletteBytes(format));
  }

  // Applies the device state at a draw call to the geometry it draws and queues that for the frame
  onDerivedDraw(state, env, draw) {
    const base = state.baseVertex || 0;
    const buffer = env.this && env.this.obj !== undefined ? env.this.obj : env.this;
    if (buffer instanceof SceneObject && buffer.mesh) { this.drawLoaded(buffer, state, draw, env, base); return; }
    const run = draw.indices === undefined ? Array.from({ length: draw.count }, (_, i) => draw.start + i) : this.readIndices(draw.indices, draw.count, env).map((i) => i + base);
    this.drawStream(state, primitiveTriangles(draw.primitive, run));
  }

  // The indices an indexed draw reads from the pointer it was handed
  readIndices(p, count, env) {
    const out = new Array(count);
    const bytes = p && p.raw !== undefined && p.raw.indexBytes ? p.raw.indexBytes : 2;
    if (p && p.raw !== undefined) {
      const view = p.raw.view, off = p.off || 0;
      if (off + count * bytes > p.raw.bytes.length) throw new Error(`index run of ${count} at +0x${off.toString(16)} outside the ${p.raw.bytes.length} bytes of its buffer`);
      for (let i = 0; i < count; i++) out[i] = bytes === 4 ? view.getUint32(off + 4 * i, true) : view.getUint16(off + 2 * i, true);
      return out;
    }
    for (let i = 0; i < count; i++) out[i] = this.derived.int(this.derived.loadLive(env, this.derived.offsetPointer(p, 2 * i), 2));
    return out;
  }

  // Draws a loaded mesh, the whole of it through its own geometry and a run of its indices through a copy
  drawLoaded(buffer, state, draw, env, base) {
    const whole = buffer.mesh.geometry.index.count;
    const own = draw.indices !== undefined && draw.indices.raw === buffer.geometry.indexBuffer;
    const off = draw.indices === undefined ? 0 : (draw.indices.off || 0);
    if (own && off === 0 && draw.count === whole && !base) {
      this.pipeline.update(buffer.mesh.material, state, buffer.desc);
      this.frameDraws.push(buffer.mesh);
      return;
    }
    // A run of the mesh's own indices is named by where it lies, indices from elsewhere by what they hold
    let entry = this.rangeMeshes.get(buffer);
    const key = own ? `${draw.primitive}:${off}:${draw.count}:${base}` : null;
    if (entry && key !== null && entry.key === key) {
      this.pipeline.update(entry.mesh.material, state, buffer.desc);
      this.frameDraws.push(entry.mesh);
      return;
    }
    const run = draw.indices === undefined ? Array.from({ length: draw.count }, (_, i) => draw.start + i) : this.readIndices(draw.indices, draw.count, env).map((i) => i + base);
    const triangles = primitiveTriangles(draw.primitive, run);
    const fullKey = key !== null ? key : triangles.join(',');
    if (!entry || entry.key !== fullKey) {
      const geometry = buffer.mesh.geometry.clone();
      geometry.setIndex(triangles);
      const mesh = new THREE.Mesh(geometry, entry ? entry.mesh.material : this.pipeline.createMaterial());
      detachFromThree(mesh);
      if (entry) entry.mesh.geometry.dispose();
      entry = { mesh, key: fullKey };
      this.rangeMeshes.set(buffer, entry);
    }
    this.pipeline.update(entry.mesh.material, state, buffer.desc);
    this.frameDraws.push(entry.mesh);
  }

  // Draws triangles over the vertex buffer the executable filled through a lock, its geometry refreshed in place
  drawStream(state, triangles) {
    const stream = state.stream;
    if (!stream || !stream.vb) throw new Error(`vertex draw from ${JSON.stringify(stream)} which is no vertex buffer`);
    const vb = stream.vb;
    const fvf = typeof state.program === 'number' ? state.program : vb.fvf;
    const desc = describeFVF(fvf);
    if (desc.stride !== state.streamStride) throw new Error(`vertex format 0x${fvf.toString(16)} is ${desc.stride} bytes but the stream stride is ${state.streamStride}`);
    let entry = this.streamMeshes.get(vb);
    if (!entry || entry.fvf !== fvf) {
      const count = vb.bytes.length / desc.stride;
      const geometry = new THREE.BufferGeometry();
      for (const a of desc.attributes) geometry.setAttribute('a_' + a.offset, new THREE.BufferAttribute(new Float32Array(count * a.comps), a.comps));
      const mesh = new THREE.Mesh(geometry, entry ? entry.mesh.material : this.pipeline.createMaterial());
      detachFromThree(mesh);
      if (entry) entry.mesh.geometry.dispose();
      entry = { mesh, version: -1, fvf, wide: count > 0x10000 };
      this.streamMeshes.set(vb, entry);
    }
    if (vb.dirtyHi > vb.dirtyLo) {
      fillStreamAttributes(entry.mesh.geometry, vb, desc);
      vb.dirtyLo = vb.bytes.length;
      vb.dirtyHi = 0;
    }
    setStreamIndex(entry.mesh.geometry, triangles, entry.wide);
    this.pipeline.update(entry.mesh.material, state, desc);
    this.frameDraws.push(entry.mesh);
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
    const aspect = (this.derived.consoleApi.videoFlags() & VIDEO_FLAG_WIDESCREEN) ? DISPLAY_ASPECT_WIDE : DISPLAY_ASPECT_NORMAL;
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
    const classes = {
      AudioClip: {
        Play: (node) => node._play(),
        Stop: (node) => node._stop(),
        Pause: (node) => node._pause(),
        PlayOrPause: (node) => (node._transportMode === 0 ? node._play() : node._pause()),
        getMinutes: (node) => Math.floor(node._playbackSeconds() / 60),
        getSeconds: (node) => Math.floor(node._playbackSeconds()) % 60,
      },
    };
    for (const [cls, names] of Object.entries(this.derived.model.system.methods)) {
      if (RUNTIME_FIELD_CLASSES.has(cls) || SCRIPT_OBJECT_PROTOTYPES[cls]) continue;
      classes[cls] = {};
      for (const name of names) classes[cls][name] = (node, ...args) => this.callNative(cls, name, node, args);
    }
    return classes;
  }

  // An environment for routines the runtime enters on the executable's behalf
  hostEnv() {
    const parent = this.derived.newEnv(null);
    parent.state = this.device;
    return parent;
  }

  // Calls a script native through the executable's own dispatcher, which converts the boxed arguments for it
  callNative(cls, name, node, args) {
    const d = this.derived, s = d.scene.script;
    const index = d.model.system.methods[cls].indexOf(name);
    if (index < 0) throw new Error(`${cls} registers no native ${name}`);
    const argv = d.heapObject(4 * Math.max(args.length, 1)).obj;
    args.forEach((a, i) => d.writeField(argv, 4 * i, this.scriptValue(a), 4));
    return this.scriptResult(d.run(s.call, node.derivedObject(), [index, { obj: argv, off: 0 }, args.length], this.device, this.hostEnv()));
  }

  // A script argument as the dispatcher receives it: a node as its object, anything else as the engine's value
  scriptValue(a) {
    if (a === undefined || a === null) return 0;
    if (typeof a === 'object' && a.__xapNode) return { obj: a.__xapNode.derivedObject(), off: 0 };
    const obj = new SceneObject(SCRIPT_VALUE_TYPE, null);
    obj.scriptValue = a;
    return { obj, off: 0 };
  }

  // Reads the value object a native handed back as the script value it boxes
  scriptResult(v) {
    const d = this.derived, s = d.scene.script;
    if (v === 0 || v === undefined || v === null) return undefined;
    if (!v || v.obj === undefined) throw new Error(`a native returned ${JSON.stringify(v)} which is no value object`);
    const vt = d.int(d.readField(v.obj, 0, 4));
    if (vt === s.number_vtable) return floatFromBits(d.int(d.readField(v.obj, s.number_field, 4)));
    if (vt === s.string_vtable) {
      // A value whose characters were never set holds the empty string
      const chars = d.readField(v.obj, s.string_field, 4);
      return d.int(chars) === 0 ? '' : d.string(chars);
    }
    throw new Error(`a native returned a value with vtable ${vt.toString(16)}, neither a number nor a string`);
  }

  // Every method a class registers must be implemented, the script object classes being the engine's own
  checkNatives(methods) {
    const missing = [];
    for (const [cls, names] of Object.entries(methods)) {
      for (const name of names) {
        const impl = this.natives[cls] && this.natives[cls][name];
        if (impl) continue;
        const script = SCRIPT_OBJECT_PROTOTYPES[cls];
        if (script && typeof script[name] === 'function') continue;
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
    try {
      this.frameDraws = [];
      this.derived.work = 0;
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
          node._fieldsVersion++;
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
    this._isBound = false;
    this._eventHandlers = null;
    this._audioUrl = '';
    this._audioLoop = false;
    this._audioVolume = 1;
    this._isPlaying = false;
    this._archive = '';
    this._sceneName = '';
    this._fields = {};
    this._fieldsVersion = 0;
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

        const field = RUNTIME_FIELD_CLASSES.has(target.data.type) ? null : target.runtime.derived.fieldByName(target.data.type, s);
        if (target._fields[s] !== undefined) return target.sceneFieldValue(s, field);
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
          target._fieldsVersion++;
          return true;
        }
        if (s === 'string') {
          target._fields.string = value;
          target._fieldsVersion++;
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

  // A scene file field as a script reads it, a node valued one being the node the object points at
  sceneFieldValue(name, field) {
    if (field && field.type === FIELD_NODE) return this.readDerivedField(field);
    const raw = this._fields[name];
    const data = extractNode(raw);
    if (data) return this.runtime.nodeFor(data, this.archiveName).proxy;
    if (Array.isArray(raw) && raw.some((v) => extractNode(v))) return this.fieldNodeList(name).map((n) => n.proxy);
    return raw;
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
    this._fieldsVersion++;
    if (this.data.type === 'ImageTexture' && name === 'url') {
      this.derivedObject().mem.store(d.draw.texture_field, 4, { xapTexture: this.runtime.loadTexture(String(value)) });
    }
  }

  // The generic field store the executable performs after the hook allows it
  storeField(field, value) {
    const mem = this.derivedObject().mem;
    switch (field.type) {
      case FIELD_BOOL: mem.store(field.off, 1, value ? 1 : 0); break;
      case FIELD_INT: mem.store(field.off, 4, Number(value) >>> 0); break;
      case FIELD_NUMBER: mem.store(field.off, 4, bitsFromFloat(Number(value))); break;
      case FIELD_STRING: mem.store(field.off, 4, { str: String(value) }); break;
      case FIELD_NODE: mem.store(field.off, 4, this.fieldNodeValue(value)); break;
      case FIELD_VEC3: case FIELD_COLOR: mem.store(field.off, 12, Float32Array.from([Number(value[0]), Number(value[1]), Number(value[2])])); break;
      case FIELD_ROTATION: mem.store(field.off, 16, Float32Array.from([Number(value[0]), Number(value[1]), Number(value[2]), Number(value[3])])); break;
      default: throw new Error(`field ${field.name} of ${this.data.type} has scene type ${field.type} which the runtime does not store`);
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

  // Keeps the mode in the executable's clip object as well, where its visualizer reads it
  _setTransportMode(mode) {
    if (this._transportMode === mode) return;
    this._transportMode = mode;
    const d = this.runtime.derived, field = d.fieldByName(this.data.type, 'transportMode');
    if (!field) throw new Error(`${this.data.type} declares no transportMode field`);
    this.derivedObject().mem.store(field.off, 4, mode >>> 0);
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
// Helper functions
// ============================================================================

// Joins a path onto a base the way the executable's helper does, parent steps cutting the base at its separators
function joinPaths(src, base) {
  const absolute = src.includes(':') || src.startsWith('\\\\');
  if (absolute || base === null || (!base.includes(':') && !base.includes('/'))) return src;
  const lastSeparator = (s) => { const i = s.lastIndexOf('/'); return i >= 0 ? i : s.lastIndexOf('\\'); };
  let out = base, rest = src;
  let sep = lastSeparator(out);
  if (sep < 0) throw new Error(`base path ${JSON.stringify(base)} holds no separator to join ${JSON.stringify(src)} after`);
  while (rest.startsWith('../') || rest.startsWith('..\\')) {
    rest = rest.slice(3);
    out = out.slice(0, sep);
    sep = lastSeparator(out);
    if (sep >= 0) continue;
    if (!out.length) throw new Error(`parent steps of ${JSON.stringify(src)} climb past the start of ${JSON.stringify(base)}`);
    sep = out.length - 1;
    break;
  }
  return out.slice(0, sep) + '/' + rest;
}

// Three must not cull by or flip winding from a mesh world matrix the derived shaders ignore
function detachFromThree(mesh) {
  mesh.frustumCulled = false;
  mesh.matrixAutoUpdate = false;
  mesh.matrixWorldAutoUpdate = false;
}

// Converts the vertices written since the last conversion into the geometry's attribute arrays and marks that
// range for upload
function fillStreamAttributes(geometry, vb, desc) {
  const view = vb.view;
  const first = Math.floor(vb.dirtyLo / desc.stride), last = Math.min(Math.ceil(vb.dirtyHi / desc.stride), vb.bytes.length / desc.stride);
  for (const a of desc.attributes) {
    const attribute = geometry.getAttribute('a_' + a.offset), data = attribute.array;
    for (let v = first; v < last; v++) {
      const base = v * desc.stride + a.offset;
      if (a.source === 'color') {
        const c = view.getUint32(base, true);
        data[v * 4] = ((c >>> 16) & 255) / 255; data[v * 4 + 1] = ((c >>> 8) & 255) / 255; data[v * 4 + 2] = (c & 255) / 255; data[v * 4 + 3] = (c >>> 24) / 255;
      } else {
        for (let k = 0; k < a.comps; k++) data[v * a.comps + k] = view.getFloat32(base + 4 * k, true);
      }
    }
    attribute.clearUpdateRanges();
    attribute.addUpdateRange(first * a.comps, (last - first) * a.comps);
    attribute.needsUpdate = true;
  }
}

// Writes a triangle list into the geometry's index, growing the index when the list outgrows it
function setStreamIndex(geometry, triangles, wide) {
  const index = geometry.index;
  if (index && index.array.length >= triangles.length) {
    let same = geometry.drawRange.count === triangles.length;
    for (let i = 0; same && i < triangles.length; i++) same = index.array[i] === triangles[i];
    if (same) return;
    index.array.set(triangles);
    index.needsUpdate = true;
  } else {
    geometry.dispose();
    geometry.setIndex(new THREE.BufferAttribute((wide ? Uint32Array : Uint16Array).from(triangles), 1));
  }
  geometry.setDrawRange(0, triangles.length);
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
