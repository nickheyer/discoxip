# Gaps

> Priority #1: Deriving data from the reference material at build time, then using that data to generate the transpiled bundle programatically. This must be done generically, as a pipeline formula so that it may be reused for similar projects.


## RENDERING

### Derived from the executable
Material constructors and Apply methods, the shape, appearance, material node and mesh draw methods, the per node vertex shader constants, the vertex programs and declarations, the render state defaults, the procedural textures, the node class registry with field tables and constructor defaults, the field write hook each draw chain class runs when a script assigns one of its fields, the render states the text draw routine sets around its glyph draw, the scene path join and its input roles, the library function identities the linker records in the executable, the chunked font layout and its LZX streams, the static device with the surfaces its creation fills, the texture setter the optimizer inlines, the controller routines the runtime's own input stands in for, and the class descriptors the static initializers register, linked into the class list the way the CRT init leaves them.

### Not yet derived (TODO)
- The text glyph layout. Glyphs are placed by the runtime from the font outlines and drawn with the fixed function transform.
- The keyboard node's per key selection flag and the memory browser's icon names. Both globals start at their image values and no runtime code writes them.
- The draw routines of node classes other than Shape, Appearance, Material, MaxMaterial, Mesh, Sphere, Texture and ImageTexture.
- The dynamic texture of the music scene. Its palette, star field, image fader and audio visualizer children plot into an eight bit surface through loops the path summaries do not carry, so the runtime renders no dynamic texture.

## CODE


┌────────────────────────────────────┬──────┬──────┐
│                                    │ 4304 │ 5659 │
├────────────────────────────────────┼──────┼──────┤
│ XDK build                          │ 3944 │ 5659 │
├────────────────────────────────────┼──────┼──────┤
│ node classes                       │ 60   │ 61   │
├────────────────────────────────────┼──────┼──────┤
│ materials                          │ 112  │ 127  │
├────────────────────────────────────┼──────┼──────┤
│ scene files                        │ 23   │ 29   │
├────────────────────────────────────┼──────┼──────┤
│ routines summarized                │ 438  │ 395  │
├────────────────────────────────────┼──────┼──────┤
│ routines that failed summarization │ 44   │ 43   │
└────────────────────────────────────┴──────┴──────┘

- Music playback crashes both builds. Selecting the audio CD and playing stops the frame loop because the music scene draws a DynamicTexture whose Palette, StarField, ImageFader and AudioVisualizer children plot into an eight-bit surface through loops the path summaries do not carry. Nothing past that screen has been exercised.
- The online dashboard does not exist here. In 5659, Xbox Live New Account, Account Recovery and Settings Network Settings all hand off to xonlinedash.xbe, which the runtime turns into a page reload. That executable is a second program with its own scene format, 48 XBG scenes, 45 textures and its own audio, and nothing has been started on it. The same applies to update.xbe.
- Runtime stand-ins instead of derivations. Text draw uses the runtime's own glyph layout. Keyboard draw, hook and all its natives throw, so naming a memory unit, a soundtrack or an account cannot work. DVDPlayer tick, hook and its natives throw. AudioClip, Joystick, SavedGameGrid, CopyDestination, DiscDrive and LiveAccounts ticks are hooked out and replaced by JavaScript reimplementations, as are the MemoryMonitor, SavedGameGrid, MusicCollection, LiveAccounts, Config and Translator natives. Each is an approximation of the executable, not a derivation of it.
- Executor limits behind those. Symbolic-count rep movs and stos, CMOVcc, SHRD, FPATAN and FYL2X are unsupported, and loop-heavy routines hit path and step limits. Lifting those would unblock the stand-ins above and the roughly thirty helper routines per build that failed to summarize.
- Script engine gaps the sweep exposed. The memory files menu and the language menu read an inline's children in OnActivate before they exist. A handler calling a sibling handler on the same node fails because handlers compile in isolation.
- Fonts and languages. Glyphs are exported only up to U+024F, so switching to Japanese, Korean or Chinese renders blank menus even though the translation tables are present.
- Virtual hardware. No memory units, no disc, no saved games, no soundtracks, no accounts, no network link, and settings reset on reload. Launching a disc, the online dash, the launcher, an entry point, the dash update and finishing recovery are all page reloads.
- Data. The Memory and Music tunnel meshes used by level transitions are absent from both sample sets, so transitions skip the tunnel. settings_adoc.xip is named by neither executable and never extracted.
- Rendering fidelity. Flicker filter, soft display filter, gamma ramp, overscan colour, back buffer scale, screen space offset and persist display are ignored. Fog, specular output, point sprites, back-face colours and texture coordinate generation throw if a path uses them. Mip, filter, anisotropy, antialias, stencil and z-bias states are ignored, and there is no composite or CRT look.
- Audio and input. AudioClip pan, frequency, fade and pause-on-moving are parsed but not applied. CD and soundtrack URLs do not play. The controller mapping lacks Back, Start, triggers, white and black, the secret key, and the DVD remote.
