#!/bin/sh
# cc.sh <toolchain> <out.obj> <src> [flags...]
tc=$1; out=$2; src=$3; shift 3
case $tc in */*) dir=$tc;; *) dir=references/toolchains/$tc;; esac; CL=$(ls $dir | grep -i '^cl.exe$')
WINEDEBUG=-all WINEPREFIX=$PWD/build/wine timeout 180 wine $dir/$CL /nologo /c "$@" /Fo"$(printf 'Z:%s' "$out" | sed 's|/|\\|g')" "$(printf '%s' "$src" | sed -e 's|^/|Z:/|' -e 's|/|\\|g')" 2>&1 | grep -v 'MESA\|pci id' | grep -v "^$(basename $src)$"
