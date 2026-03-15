# discoxip
Multi-format Xbox dashboard toolkit for XIP archives and their embedded assets.

## Getting Started

### Download Latest
Download the latest release for your platform from [Releases](https://github.com/nickheyer/discoxip/releases/latest), extract, and add to your PATH.

### Go-Install
```
go install github.com/nickheyer/discoxip/cmd/discoxip@latest
```
Make sure `$(go env GOPATH)/bin` is in your `PATH`.

### Build From Source
>Requires Go 1.26+ and Make
```
git clone https://github.com/nickheyer/discoxip.git
cd discoxip && make build
sudo mv ./build/discoxip /usr/local/bin/discoxip
```

## Commands

### Archive
```
discoxip info <archive.xip>
```
Display archive metadata and file listing.

```
discoxip extract <archive.xip> [-o <dir>] [-v] [--all]
```
Extract files from a XIP archive.

```
discoxip pack <directory> -o <archive.xip>
```
Pack a directory into a XIP archive.

### Buffer
```
discoxip buffer info <file.vb>
```
Display vertex buffer header, format, stride, and vertex count.

```
discoxip buffer dump <file.vb> [--limit <n>]
```
Dump decoded vertex data in tabular form, or raw hex for unknown formats.

```
discoxip buffer export <file.vb> <file.ib> [-f obj] [-o <output.obj>]
```
Export a vertex/index buffer pair to Wavefront OBJ.

### Mesh
```
discoxip mesh info <file.xm> [file.xm...]
```
Detect content type (empty, text, binary) and show summary for XM mesh files.

```
discoxip mesh export-all <directory>
```
Batch info for all XM files in a directory.

### Texture
```
discoxip texture info <file.xbx> [file.xbx...]
```
Display XPR0 texture dimensions, pixel format, and compression info.

```
discoxip texture export <file.xbx> [-o <output.png>]
```
Decompress and decode an XBX texture to PNG.

```
discoxip texture export-all <directory>
```
Batch export all XBX textures in a directory to PNG.

### XAP
```
discoxip xap info <file.xap>
```
Display scene graph summary: node count, mesh references, and materials.

```
discoxip xap dump <file.xap>
```
Pretty-print the parsed VRML scene graph.

### Font
```
discoxip font info <file.xtf>
```
Display XTF font metadata, glyph count, and Unicode range coverage.

```
discoxip font export <file.xtf> [-o <output.png>] [--width <n>]
```
Export glyph bitmap data as a grayscale PNG atlas.

### Scene
```
discoxip scene info <file.xap>
```
Show resolved scene assembly: mesh references linked to VB/IB buffer pools.

```
discoxip scene export <file.xap> [-o <output.glb>]
```
Assemble XAP scene graph with VB/IB geometry and export as binary glTF.
