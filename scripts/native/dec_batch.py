#!/usr/bin/env python3
"""dec_batch.py [toolchain] — scores many peeled-loop BldrDecompress shapes
against the reference 228 bytes at 0x716."""
import os, sys, struct, subprocess, json, re

REPO = '/home/nick/code/discoxip'
SCRATCH = '/tmp/icebath-scratch/20261103-055159-4ec9b59a55def7f5'
REF = open(f'{REPO}/build/native/5659/kernel/2bl.bin', 'rb').read()[0x716:0x716+228]
BUILD = json.load(open(f'{REPO}/native/boot/2bl/build.json'))
IMG = BUILD['5713']['bldr']
FLAGS = list(IMG['cflags'])
for o in IMG['objects']:
    if o['file'] == 'native/boot/2bl/bldrsup.c':
        FLAGS += o.get('flags', [])

HDR = '#include "bldr.h"\n\n'
PRE = 'ULONG BldrShadowRomEnd = 0;\nULONG BldrHeapUsed = 0;\nPFBL_CRYPTO_TABLE BldrCryptoTable = NULL;\n\n'
ALLOC = '''PVOID __cdecl BldrAllocate(ULONG Size)
{
    PVOID Buffer;

    Buffer = (PVOID)(BLDR_HEAP_BASE + BldrHeapUsed);
    BldrHeapUsed += Size;
    return Buffer;
}

VOID __cdecl BldrFree(PVOID Buffer)
{
}

'''
DECLS = '''    UINT MaxBlockSize;
    LZXDECOMPRESS Configuration;
    UINT SourceBufferMin;
    LDI_CONTEXT_HANDLE Handle;
    UINT BytesOut;
    BOOLEAN Result;
    PBLDR_LZX_BLOCK Block;
    PUCHAR Data;

'''
HEAD = '''    Result = FALSE;
    MaxBlockSize = 0x8000;
    Configuration.WindowSize = 0x20000;
    Configuration.fCPUtype = 1;
    if (LDICreateDecompression(&MaxBlockSize, &Configuration, BldrAllocate, BldrFree, &SourceBufferMin, &Handle, NULL, NULL, NULL, NULL, NULL) != MDI_ERROR_NO_ERROR) {
        return FALSE;
    }
'''
TAIL = '''    LDIDestroyDecompression(Handle);
    return Result;
}
'''
SIG = 'BOOLEAN NTAPI BldrDecompress(PUCHAR Source, PUCHAR Destination, ULONG SourceSize)\n{\n'

# body variants: peel + loop
BODIES = {
 # v1: Block/Data cursors, peel, do-while with call in condition
 'v1': '''    Block = (PBLDR_LZX_BLOCK)Source;
    Data = Source + sizeof(BLDR_LZX_BLOCK);
    BytesOut = Block->UncompressedSize;
    if (LDIDecompress(Handle, Data, Block->CompressedSize, Destination, &BytesOut) == MDI_ERROR_NO_ERROR) {
        do {
            Destination += Block->UncompressedSize;
            Data += Block->CompressedSize;
            if (Data - Source >= (PUCHAR)SourceSize) {
                Result = TRUE;
                break;
            }
            Block = (PBLDR_LZX_BLOCK)(Data - sizeof(BLDR_LZX_BLOCK));
            BytesOut = Block->UncompressedSize;
            Data = (PUCHAR)Block + sizeof(BLDR_LZX_BLOCK);
        } while (LDIDecompress(Handle, Data, Block->CompressedSize, Destination, &BytesOut) == MDI_ERROR_NO_ERROR);
    }
''',
 # v2: same, ULONG cast on the size compare
 'v2': '''    Block = (PBLDR_LZX_BLOCK)Source;
    Data = Source + sizeof(BLDR_LZX_BLOCK);
    BytesOut = Block->UncompressedSize;
    if (LDIDecompress(Handle, Data, Block->CompressedSize, Destination, &BytesOut) == MDI_ERROR_NO_ERROR) {
        do {
            Destination += Block->UncompressedSize;
            Data += Block->CompressedSize;
            if ((ULONG)(Data - Source) >= SourceSize) {
                Result = TRUE;
                break;
            }
            Block = (PBLDR_LZX_BLOCK)(Data - sizeof(BLDR_LZX_BLOCK));
            BytesOut = Block->UncompressedSize;
            Data = (PUCHAR)Block + sizeof(BLDR_LZX_BLOCK);
        } while (LDIDecompress(Handle, Data, Block->CompressedSize, Destination, &BytesOut) == MDI_ERROR_NO_ERROR);
    }
''',
 # v3: for(;;) with the call as the loop tail, Block cursor walked from Data
 'v3': '''    Block = (PBLDR_LZX_BLOCK)Source;
    Data = Source + sizeof(BLDR_LZX_BLOCK);
    BytesOut = Block->UncompressedSize;
    if (LDIDecompress(Handle, Data, Block->CompressedSize, Destination, &BytesOut) == MDI_ERROR_NO_ERROR) {
        for (;;) {
            Destination += Block->UncompressedSize;
            Data += Block->CompressedSize;
            if ((ULONG)(Data - Source) >= SourceSize) {
                Result = TRUE;
                break;
            }
            Block = (PBLDR_LZX_BLOCK)(Data - sizeof(BLDR_LZX_BLOCK));
            BytesOut = Block->UncompressedSize;
            Data = (PUCHAR)Block + sizeof(BLDR_LZX_BLOCK);
            if (LDIDecompress(Handle, Data, Block->CompressedSize, Destination, &BytesOut) != MDI_ERROR_NO_ERROR) {
                break;
            }
        }
    }
''',
 # v4: Block advanced by the sizes directly, Data = payload pointer
 'v4': '''    Block = (PBLDR_LZX_BLOCK)Source;
    Data = Source + sizeof(BLDR_LZX_BLOCK);
    BytesOut = Block->UncompressedSize;
    if (LDIDecompress(Handle, Data, Block->CompressedSize, Destination, &BytesOut) == MDI_ERROR_NO_ERROR) {
        for (;;) {
            Destination += Block->UncompressedSize;
            Data += Block->CompressedSize;
            if ((ULONG)(Data - Source) >= SourceSize) {
                Result = TRUE;
                break;
            }
            Block = (PBLDR_LZX_BLOCK)Data;
            BytesOut = Block->UncompressedSize;
            Data += sizeof(BLDR_LZX_BLOCK);
            if (LDIDecompress(Handle, Data, Block->CompressedSize, Destination, &BytesOut) != MDI_ERROR_NO_ERROR) {
                break;
            }
        }
    }
''',
 # v5: v4 as do/while
 'v5': '''    Block = (PBLDR_LZX_BLOCK)Source;
    Data = Source + sizeof(BLDR_LZX_BLOCK);
    BytesOut = Block->UncompressedSize;
    if (LDIDecompress(Handle, Data, Block->CompressedSize, Destination, &BytesOut) == MDI_ERROR_NO_ERROR) {
        do {
            Destination += Block->UncompressedSize;
            Data += Block->CompressedSize;
            if ((ULONG)(Data - Source) >= SourceSize) {
                Result = TRUE;
                break;
            }
            Block = (PBLDR_LZX_BLOCK)Data;
            BytesOut = Block->UncompressedSize;
            Data += sizeof(BLDR_LZX_BLOCK);
        } while (LDIDecompress(Handle, Data, Block->CompressedSize, Destination, &BytesOut) == MDI_ERROR_NO_ERROR);
    }
''',
 # v6: peel inside a Status variable
 'v6': '''    Block = (PBLDR_LZX_BLOCK)Source;
    Data = Source + sizeof(BLDR_LZX_BLOCK);
    BytesOut = Block->UncompressedSize;
    if (LDIDecompress(Handle, Data, Block->CompressedSize, Destination, &BytesOut) == MDI_ERROR_NO_ERROR) {
        for (;;) {
            Destination += Block->UncompressedSize;
            Data += Block->CompressedSize;
            if ((ULONG)(Data - Source) < SourceSize) {
                Block = (PBLDR_LZX_BLOCK)Data;
                BytesOut = Block->UncompressedSize;
                Data += sizeof(BLDR_LZX_BLOCK);
                if (LDIDecompress(Handle, Data, Block->CompressedSize, Destination, &BytesOut) == MDI_ERROR_NO_ERROR) {
                    continue;
                }
            } else {
                Result = TRUE;
            }
            break;
        }
    }
''',
}

def win(p): return 'Z:' + p.replace('/', '\\')

def sym_bytes(obj, sym):
    o = subprocess.run(['objdump', '-t', obj], capture_output=True, text=True).stdout
    m = re.search(r'\(sec\s+(\d+)\).*\(scl\s+2\).*0x([0-9a-f]+)\s+' + re.escape(sym) + r'\s*$', o, re.M)
    if not m:
        return b''
    secnum, value = int(m.group(1)), int(m.group(2), 16)
    d = open(obj, 'rb').read()
    name, vs, va, rs, ro = struct.unpack_from('<8sIIII', d, 20 + 40*(secnum-1))
    size = vs if vs else rs
    return d[ro + value:ro + size]

def main():
    tc = sys.argv[1] if len(sys.argv) > 1 else 'vc7'
    d = f'{REPO}/references/toolchains/{tc}'
    cl = [f for f in os.listdir(d) if f.lower() == 'cl.exe'][0]
    env = dict(os.environ, WINEDEBUG='-all', WINEPREFIX=f'{REPO}/build/wine')
    for name, body in BODIES.items():
        src = f'{SCRATCH}/db_{name}.c'
        open(src, 'w').write(HDR + PRE + ALLOC + SIG + DECLS + HEAD + body + TAIL)
        obj = f'{SCRATCH}/db_{name}_{tc}.obj'
        if os.path.exists(obj): os.remove(obj)
        r = subprocess.run(['wine', f'{d}/{cl}', '/nologo', '/c'] + FLAGS + ['/Fo' + win(obj), win(src)],
                           capture_output=True, text=True, env=env, cwd=REPO)
        if not os.path.exists(obj):
            print(f'{name} COMPILE FAILED'); print(r.stdout[-300:]); continue
        t = sym_bytes(obj, '_BldrDecompress@12')
        n = sum(a == b for a, b in zip(t, REF))
        print(f'{name} [{tc}] {n}/228 len={len(t)} {"EXACT" if t==REF else ""}')
main()
