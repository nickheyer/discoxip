# discoxip
Unpacks Original Xbox dashboard XIP archives and rebuilds the dashboard as a Three.js web app.

## Install
```
go install github.com/nickheyer/discoxip/cmd/discoxip@latest
```
Prebuilt binaries are on the [releases page](https://github.com/nickheyer/discoxip/releases/latest).

## Usage
```
discoxip build <source-dir> --bios <flash.bin> --mcpx <mcpx.bin> [-o <dir>]          extract archives, decode the XBE, emit the web app
discoxip web export <extracted-dir> --bios <flash.bin> --mcpx <mcpx.bin> [-o <dir>]  emit the web app from an extracted directory

discoxip info <archive.xip>
discoxip extract [path] [-o <dir>] [-r] [-v]
discoxip pack <directory> -o <archive.xip>

discoxip bios info <flash.bin> --mcpx <mcpx.bin>
discoxip bios extract <flash.bin> --mcpx <mcpx.bin> [-o <dir>]
discoxip bios lift <flash.bin> --mcpx <mcpx.bin> [--va <hex>]
discoxip bios machine <flash.bin> --mcpx <mcpx.bin> [--va <lo>-<hi>]
discoxip bios boot <flash.bin> --mcpx <mcpx.bin> --launch <dir> -o <dir>

discoxip xbe info <file.xbe>
discoxip xbe symbols <file.xbe>
discoxip xbe materials <file.xbe> [-v]
discoxip xbe render <file.xbe> --bios <flash.bin> --mcpx <mcpx.bin> [-o <file.json>]
discoxip xbe lift <file.xbe> <va>...
discoxip xbe disasm <file.xbe> [-o <file.c>]

discoxip scene info <file.xap> --xbe <file.xbe> --bios <flash.bin> --mcpx <mcpx.bin>
discoxip scene export <file.xap> --xbe <file.xbe> --bios <flash.bin> --mcpx <mcpx.bin> [-o <file.glb>]
discoxip scene build <directory> --xbe <file.xbe> --bios <flash.bin> --mcpx <mcpx.bin> [-o <dir>]

discoxip xap info|dump <file.xap>
discoxip mesh info <file.xm|directory> [...]
discoxip buffer info <file.vb>
discoxip buffer dump <file.vb> --xbe <file.xbe> --bios <flash.bin> --mcpx <mcpx.bin> [--limit <n>]
discoxip buffer export <file.vb> <file.ib> --xbe <file.xbe> --bios <flash.bin> --mcpx <mcpx.bin> [-o <file.obj>]
discoxip texture info|export <file.xbx>
discoxip texture export-all <directory>
discoxip font info|export <file.xtf>
```

## Development
```
make dev                       # build sample/5659 into build/dev, decompile the XBE, serve on :8000
make dev SRC=<dir> PORT=9000
make test
```
