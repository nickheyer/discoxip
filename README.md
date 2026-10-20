# discoxip
Unpacks Original Xbox dashboard XIP archives and rebuilds the dashboard as a Three.js web app.

## Install
```
go install github.com/nickheyer/discoxip/cmd/discoxip@latest
```
Prebuilt binaries are on the [releases page](https://github.com/nickheyer/discoxip/releases/latest).

## Usage
```
discoxip build <source-dir> [-o <dir>]          extract archives, decode the XBE, emit the web app
discoxip web export <extracted-dir> [-o <dir>]  emit the web app from an extracted directory

discoxip info <archive.xip>
discoxip extract [path] [-o <dir>] [-r] [-v]
discoxip pack <directory> -o <archive.xip>

discoxip xbe info <file.xbe>
discoxip xbe materials <file.xbe>
discoxip xbe disasm <file.xbe> [-o <file.c>]

discoxip scene info <file.xap>
discoxip scene export <file.xap> [-o <file.glb>]
discoxip scene build <directory> [-o <dir>]

discoxip xap info|dump <file.xap>
discoxip mesh info <file.xm|directory> [...]
discoxip buffer info|dump <file.vb>
discoxip buffer export <file.vb> <file.ib> [-o <file.obj>]
discoxip texture info|export <file.xbx>
discoxip texture export-all <directory>
discoxip font info|export <file.xtf>
```

## Development
```
make dev                       # build sample/4304 into build/dev, decompile the XBE, serve on :8000
make dev SRC=<dir> PORT=9000
```
