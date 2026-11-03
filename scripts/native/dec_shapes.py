#!/usr/bin/env python3
"""dec_shapes.py [toolchain] — compiles BldrDecompress shapes and diffs each
against the reference 228-byte routine at 0x716."""
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

SHAPES = {
 'S0_current': '''BOOLEAN NTAPI BldrDecompress(PUCHAR Source, PUCHAR Destination, ULONG SourceSize)
{
    UINT MaxBlockSize;
    LZXDECOMPRESS Configuration;
    UINT SourceBufferMin;
    LDI_CONTEXT_HANDLE Handle;
    UINT BytesOut;
    BOOLEAN Result;
    PBLDR_LZX_BLOCK Block;
    PUCHAR Data;

    Result = FALSE;
    MaxBlockSize = 0x8000;
    Configuration.WindowSize = 0x20000;
    Configuration.fCPUtype = 1;
    if (LDICreateDecompression(&MaxBlockSize, &Configuration, BldrAllocate, BldrFree, &SourceBufferMin, &Handle, NULL, NULL, NULL, NULL, NULL) != MDI_ERROR_NO_ERROR) {
        return FALSE;
    }
    Block = (PBLDR_LZX_BLOCK)Source;
    Data = Source + sizeof(BLDR_LZX_BLOCK);
    BytesOut = Block->UncompressedSize;
    while (LDIDecompress(Handle, Data, Block->CompressedSize, Destination, &BytesOut) == MDI_ERROR_NO_ERROR) {
        Destination += Block->UncompressedSize;
        Data += Block->CompressedSize;
        if ((ULONG)(Data - Source) >= SourceSize) {
            Result = TRUE;
            break;
        }
        Block = (PBLDR_LZX_BLOCK)Data;
        Data += sizeof(BLDR_LZX_BLOCK);
        BytesOut = Block->UncompressedSize;
    }
    LDIDestroyDecompression(Handle);
    return Result;
}
''',
 # do/while(TRUE) with the guard as first statement
 'S1_dowhile': '''BOOLEAN NTAPI BldrDecompress(PUCHAR Source, PUCHAR Destination, ULONG SourceSize)
{
    UINT MaxBlockSize;
    LZXDECOMPRESS Configuration;
    UINT SourceBufferMin;
    LDI_CONTEXT_HANDLE Handle;
    UINT BytesOut;
    BOOLEAN Result;
    PBLDR_LZX_BLOCK Block;
    PUCHAR Data;

    Result = FALSE;
    MaxBlockSize = 0x8000;
    Configuration.WindowSize = 0x20000;
    Configuration.fCPUtype = 1;
    if (LDICreateDecompression(&MaxBlockSize, &Configuration, BldrAllocate, BldrFree, &SourceBufferMin, &Handle, NULL, NULL, NULL, NULL, NULL) != MDI_ERROR_NO_ERROR) {
        return FALSE;
    }
    Block = (PBLDR_LZX_BLOCK)Source;
    Data = Source + sizeof(BLDR_LZX_BLOCK);
    BytesOut = Block->UncompressedSize;
    do {
        if (LDIDecompress(Handle, Data, Block->CompressedSize, Destination, &BytesOut) != MDI_ERROR_NO_ERROR) {
            break;
        }
        Destination += Block->UncompressedSize;
        Data += Block->CompressedSize;
        if ((ULONG)(Data - Source) >= SourceSize) {
            Result = TRUE;
            break;
        }
        Block = (PBLDR_LZX_BLOCK)Data;
        Data += sizeof(BLDR_LZX_BLOCK);
        BytesOut = Block->UncompressedSize;
    } while (TRUE);
    LDIDestroyDecompression(Handle);
    return Result;
}
''',
 # Data kept as PBLDR_LZX_BLOCK cursor, Destination advanced from the header
 'S2_blockcursor': '''BOOLEAN NTAPI BldrDecompress(PUCHAR Source, PUCHAR Destination, ULONG SourceSize)
{
    UINT MaxBlockSize;
    LZXDECOMPRESS Configuration;
    UINT SourceBufferMin;
    LDI_CONTEXT_HANDLE Handle;
    UINT BytesOut;
    BOOLEAN Result;
    PBLDR_LZX_BLOCK Block;
    PUCHAR Data;

    Result = FALSE;
    MaxBlockSize = 0x8000;
    Configuration.WindowSize = 0x20000;
    Configuration.fCPUtype = 1;
    if (LDICreateDecompression(&MaxBlockSize, &Configuration, BldrAllocate, BldrFree, &SourceBufferMin, &Handle, NULL, NULL, NULL, NULL, NULL) != MDI_ERROR_NO_ERROR) {
        return FALSE;
    }
    Block = (PBLDR_LZX_BLOCK)Source;
    Data = Source + sizeof(BLDR_LZX_BLOCK);
    BytesOut = Block->UncompressedSize;
    while (LDIDecompress(Handle, Data, Block->CompressedSize, Destination, &BytesOut) == MDI_ERROR_NO_ERROR) {
        Destination += BytesOut;
        Data += Block->CompressedSize;
        if (Data - Source >= (PUCHAR)SourceSize) {
            Result = TRUE;
            break;
        }
        Block = (PBLDR_LZX_BLOCK)Data;
        Data += sizeof(BLDR_LZX_BLOCK);
        BytesOut = Block->UncompressedSize;
    }
    LDIDestroyDecompression(Handle);
    return Result;
}
''',
 # for(;;) with the call in the body
 'S3_for': '''BOOLEAN NTAPI BldrDecompress(PUCHAR Source, PUCHAR Destination, ULONG SourceSize)
{
    UINT MaxBlockSize;
    LZXDECOMPRESS Configuration;
    UINT SourceBufferMin;
    LDI_CONTEXT_HANDLE Handle;
    UINT BytesOut;
    BOOLEAN Result;
    PBLDR_LZX_BLOCK Block;
    PUCHAR Data;

    Result = FALSE;
    MaxBlockSize = 0x8000;
    Configuration.WindowSize = 0x20000;
    Configuration.fCPUtype = 1;
    if (LDICreateDecompression(&MaxBlockSize, &Configuration, BldrAllocate, BldrFree, &SourceBufferMin, &Handle, NULL, NULL, NULL, NULL, NULL) != MDI_ERROR_NO_ERROR) {
        return FALSE;
    }
    Block = (PBLDR_LZX_BLOCK)Source;
    Data = Source + sizeof(BLDR_LZX_BLOCK);
    BytesOut = Block->UncompressedSize;
    for (;;) {
        if (LDIDecompress(Handle, Data, Block->CompressedSize, Destination, &BytesOut) != MDI_ERROR_NO_ERROR) {
            break;
        }
        Destination += Block->UncompressedSize;
        Data += Block->CompressedSize;
        if ((ULONG)(Data - Source) >= SourceSize) {
            Result = TRUE;
            break;
        }
        Block = (PBLDR_LZX_BLOCK)Data;
        Data += sizeof(BLDR_LZX_BLOCK);
        BytesOut = Block->UncompressedSize;
    }
    LDIDestroyDecompression(Handle);
    return Result;
}
''',
 # if(entry call) { do { body } while (call) }
 'S4_if_dowhile': '''BOOLEAN NTAPI BldrDecompress(PUCHAR Source, PUCHAR Destination, ULONG SourceSize)
{
    UINT MaxBlockSize;
    LZXDECOMPRESS Configuration;
    UINT SourceBufferMin;
    LDI_CONTEXT_HANDLE Handle;
    UINT BytesOut;
    BOOLEAN Result;
    PBLDR_LZX_BLOCK Block;
    PUCHAR Data;

    Result = FALSE;
    MaxBlockSize = 0x8000;
    Configuration.WindowSize = 0x20000;
    Configuration.fCPUtype = 1;
    if (LDICreateDecompression(&MaxBlockSize, &Configuration, BldrAllocate, BldrFree, &SourceBufferMin, &Handle, NULL, NULL, NULL, NULL, NULL) != MDI_ERROR_NO_ERROR) {
        return FALSE;
    }
    Block = (PBLDR_LZX_BLOCK)Source;
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
            Data += sizeof(BLDR_LZX_BLOCK);
            BytesOut = Block->UncompressedSize;
        } while (LDIDecompress(Handle, Data, Block->CompressedSize, Destination, &BytesOut) == MDI_ERROR_NO_ERROR);
    }
    LDIDestroyDecompression(Handle);
    return Result;
}
''',
 # same, with Data as the cursor advanced past the header (no Block aliasing)
 'S5_if_dowhile_datacursor': '''BOOLEAN NTAPI BldrDecompress(PUCHAR Source, PUCHAR Destination, ULONG SourceSize)
{
    UINT MaxBlockSize;
    LZXDECOMPRESS Configuration;
    UINT SourceBufferMin;
    LDI_CONTEXT_HANDLE Handle;
    UINT BytesOut;
    BOOLEAN Result;
    PBLDR_LZX_BLOCK Block;
    PUCHAR Data;

    Result = FALSE;
    MaxBlockSize = 0x8000;
    Configuration.WindowSize = 0x20000;
    Configuration.fCPUtype = 1;
    if (LDICreateDecompression(&MaxBlockSize, &Configuration, BldrAllocate, BldrFree, &SourceBufferMin, &Handle, NULL, NULL, NULL, NULL, NULL) != MDI_ERROR_NO_ERROR) {
        return FALSE;
    }
    Data = Source;
    Block = (PBLDR_LZX_BLOCK)Data;
    BytesOut = Block->UncompressedSize;
    if (LDIDecompress(Handle, Data + sizeof(BLDR_LZX_BLOCK), Block->CompressedSize, Destination, &BytesOut) == MDI_ERROR_NO_ERROR) {
        do {
            Destination += Block->UncompressedSize;
            Data = (PUCHAR)Block + sizeof(BLDR_LZX_BLOCK) + Block->CompressedSize;
            if (Data - Source >= (PUCHAR)SourceSize) {
                Result = TRUE;
                break;
            }
            Block = (PBLDR_LZX_BLOCK)Data;
            BytesOut = Block->UncompressedSize;
        } while (LDIDecompress(Handle, Data + sizeof(BLDR_LZX_BLOCK), Block->CompressedSize, Destination, &BytesOut) == MDI_ERROR_NO_ERROR);
    }
    LDIDestroyDecompression(Handle);
    return Result;
}
''',
 # if (call ok) { for(;;) { body; if (call != ok) break; } }
 'S6_if_for': '''BOOLEAN NTAPI BldrDecompress(PUCHAR Source, PUCHAR Destination, ULONG SourceSize)
{
    UINT MaxBlockSize;
    LZXDECOMPRESS Configuration;
    UINT SourceBufferMin;
    LDI_CONTEXT_HANDLE Handle;
    UINT BytesOut;
    BOOLEAN Result;
    PBLDR_LZX_BLOCK Block;
    PUCHAR Data;

    Result = FALSE;
    MaxBlockSize = 0x8000;
    Configuration.WindowSize = 0x20000;
    Configuration.fCPUtype = 1;
    if (LDICreateDecompression(&MaxBlockSize, &Configuration, BldrAllocate, BldrFree, &SourceBufferMin, &Handle, NULL, NULL, NULL, NULL, NULL) != MDI_ERROR_NO_ERROR) {
        return FALSE;
    }
    Block = (PBLDR_LZX_BLOCK)Source;
    Data = Source + sizeof(BLDR_LZX_BLOCK);
    BytesOut = Block->UncompressedSize;
    if (LDIDecompress(Handle, Data, Block->CompressedSize, Destination, &BytesOut) == MDI_ERROR_NO_ERROR) {
        for (;;) {
            Destination += Block->UncompressedSize;
            Data += Block->CompressedSize;
            if (Data - Source >= (PUCHAR)SourceSize) {
                Result = TRUE;
                break;
            }
            Block = (PBLDR_LZX_BLOCK)Data;
            Data = (PUCHAR)Block + sizeof(BLDR_LZX_BLOCK);
            BytesOut = Block->UncompressedSize;
            if (LDIDecompress(Handle, Data, Block->CompressedSize, Destination, &BytesOut) != MDI_ERROR_NO_ERROR) {
                break;
            }
        }
    }
    LDIDestroyDecompression(Handle);
    return Result;
}
''',
 # Status variable, do/while
 'S7_status_dowhile': '''BOOLEAN NTAPI BldrDecompress(PUCHAR Source, PUCHAR Destination, ULONG SourceSize)
{
    UINT MaxBlockSize;
    LZXDECOMPRESS Configuration;
    UINT SourceBufferMin;
    LDI_CONTEXT_HANDLE Handle;
    UINT BytesOut;
    MDI_STATUS Status;
    BOOLEAN Result;
    PBLDR_LZX_BLOCK Block;
    PUCHAR Data;

    Result = FALSE;
    MaxBlockSize = 0x8000;
    Configuration.WindowSize = 0x20000;
    Configuration.fCPUtype = 1;
    if (LDICreateDecompression(&MaxBlockSize, &Configuration, BldrAllocate, BldrFree, &SourceBufferMin, &Handle, NULL, NULL, NULL, NULL, NULL) != MDI_ERROR_NO_ERROR) {
        return FALSE;
    }
    Block = (PBLDR_LZX_BLOCK)Source;
    Data = Source + sizeof(BLDR_LZX_BLOCK);
    BytesOut = Block->UncompressedSize;
    Status = LDIDecompress(Handle, Data, Block->CompressedSize, Destination, &BytesOut);
    while (Status == MDI_ERROR_NO_ERROR) {
        Destination += Block->UncompressedSize;
        Data += Block->CompressedSize;
        if (Data - Source >= (PUCHAR)SourceSize) {
            Result = TRUE;
            break;
        }
        Block = (PBLDR_LZX_BLOCK)Data;
        Data = (PUCHAR)Block + sizeof(BLDR_LZX_BLOCK);
        BytesOut = Block->UncompressedSize;
        Status = LDIDecompress(Handle, Data, Block->CompressedSize, Destination, &BytesOut);
    }
    LDIDestroyDecompression(Handle);
    return Result;
}
''',
 # Data cursor as PBLDR_LZX_BLOCK, Source advanced through the block stream
 'S8_block_stream': '''BOOLEAN NTAPI BldrDecompress(PUCHAR Source, PUCHAR Destination, ULONG SourceSize)
{
    UINT MaxBlockSize;
    LZXDECOMPRESS Configuration;
    UINT SourceBufferMin;
    LDI_CONTEXT_HANDLE Handle;
    UINT BytesOut;
    BOOLEAN Result;
    PBLDR_LZX_BLOCK Block;

    Result = FALSE;
    MaxBlockSize = 0x8000;
    Configuration.WindowSize = 0x20000;
    Configuration.fCPUtype = 1;
    if (LDICreateDecompression(&MaxBlockSize, &Configuration, BldrAllocate, BldrFree, &SourceBufferMin, &Handle, NULL, NULL, NULL, NULL, NULL) != MDI_ERROR_NO_ERROR) {
        return FALSE;
    }
    Block = (PBLDR_LZX_BLOCK)Source;
    BytesOut = Block->UncompressedSize;
    if (LDIDecompress(Handle, (PUCHAR)Block + sizeof(BLDR_LZX_BLOCK), Block->CompressedSize, Destination, &BytesOut) == MDI_ERROR_NO_ERROR) {
        for (;;) {
            Destination += Block->UncompressedSize;
            Block = (PBLDR_LZX_BLOCK)((PUCHAR)Block + sizeof(BLDR_LZX_BLOCK) + Block->CompressedSize);
            if ((PUCHAR)Block - Source >= (PUCHAR)SourceSize) {
                Result = TRUE;
                break;
            }
            BytesOut = Block->UncompressedSize;
            if (LDIDecompress(Handle, (PUCHAR)Block + sizeof(BLDR_LZX_BLOCK), Block->CompressedSize, Destination, &BytesOut) != MDI_ERROR_NO_ERROR) {
                break;
            }
        }
    }
    LDIDestroyDecompression(Handle);
    return Result;
}
''',
 # S9: sizes swapped per ref, Data cursor over block payloads, peeled entry call
 'S9_ref': '''BOOLEAN NTAPI BldrDecompress(PUCHAR Source, PUCHAR Destination, ULONG SourceSize)
{
    UINT MaxBlockSize;
    LZXDECOMPRESS Configuration;
    UINT SourceBufferMin;
    LDI_CONTEXT_HANDLE Handle;
    UINT BytesOut;
    BOOLEAN Result;
    PBLDR_LZX_BLOCK Block;
    PUCHAR Data;

    Result = FALSE;
    MaxBlockSize = 0x8000;
    Configuration.WindowSize = 0x20000;
    Configuration.fCPUtype = 1;
    if (LDICreateDecompression(&MaxBlockSize, &Configuration, BldrAllocate, BldrFree, &SourceBufferMin, &Handle, NULL, NULL, NULL, NULL, NULL) != MDI_ERROR_NO_ERROR) {
        return FALSE;
    }
    Block = (PBLDR_LZX_BLOCK)Source;
    Data = Source + sizeof(BLDR_LZX_BLOCK);
    BytesOut = Block->CompressedSize;
    if (LDIDecompress(Handle, Data, Block->UncompressedSize, Destination, &BytesOut) == MDI_ERROR_NO_ERROR) {
        do {
            Destination += Block->CompressedSize;
            Data += Block->UncompressedSize;
            if (Data - Source >= (PUCHAR)SourceSize) {
                Result = TRUE;
                break;
            }
            Block = (PBLDR_LZX_BLOCK)(Data - sizeof(BLDR_LZX_BLOCK));
            BytesOut = Block->CompressedSize;
        } while (LDIDecompress(Handle, Data, Block->UncompressedSize, Destination, &BytesOut) == MDI_ERROR_NO_ERROR);
    }
    LDIDestroyDecompression(Handle);
    return Result;
}
''',
 # S10: as S9 with ULONG cast on the delta
 'S10_ref_ulong': '''BOOLEAN NTAPI BldrDecompress(PUCHAR Source, PUCHAR Destination, ULONG SourceSize)
{
    UINT MaxBlockSize;
    LZXDECOMPRESS Configuration;
    UINT SourceBufferMin;
    LDI_CONTEXT_HANDLE Handle;
    UINT BytesOut;
    BOOLEAN Result;
    PBLDR_LZX_BLOCK Block;
    PUCHAR Data;

    Result = FALSE;
    MaxBlockSize = 0x8000;
    Configuration.WindowSize = 0x20000;
    Configuration.fCPUtype = 1;
    if (LDICreateDecompression(&MaxBlockSize, &Configuration, BldrAllocate, BldrFree, &SourceBufferMin, &Handle, NULL, NULL, NULL, NULL, NULL) != MDI_ERROR_NO_ERROR) {
        return FALSE;
    }
    Block = (PBLDR_LZX_BLOCK)Source;
    Data = Source + sizeof(BLDR_LZX_BLOCK);
    BytesOut = Block->CompressedSize;
    if (LDIDecompress(Handle, Data, Block->UncompressedSize, Destination, &BytesOut) == MDI_ERROR_NO_ERROR) {
        do {
            Destination += Block->CompressedSize;
            Data += Block->UncompressedSize;
            if ((ULONG)(Data - Source) >= SourceSize) {
                Result = TRUE;
                break;
            }
            Block = (PBLDR_LZX_BLOCK)(Data - sizeof(BLDR_LZX_BLOCK));
            BytesOut = Block->CompressedSize;
        } while (LDIDecompress(Handle, Data, Block->UncompressedSize, Destination, &BytesOut) == MDI_ERROR_NO_ERROR);
    }
    LDIDestroyDecompression(Handle);
    return Result;
}
''',
}

def win(p): return 'Z:' + p.replace('/', '\\')

def text_of(obj, sym):
    # section index + offset from objdump, then raw bytes from that section
    o = subprocess.run(['objdump', '-t', obj], capture_output=True, text=True).stdout
    m = re.search(r'\(sec\s+(\d+)\).*\(scl\s+2\).*0x([0-9a-f]+)\s+' + re.escape(sym) + r'\s*$', o, re.M)
    if not m:
        return b''
    secnum, value = int(m.group(1)), int(m.group(2), 16)
    d = open(obj, 'rb').read()
    nsec = struct.unpack_from('<H', d, 2)[0]
    name, vs, va, rs, ro = struct.unpack_from('<8sIIII', d, 20 + 40*(secnum-1))
    size = vs if vs else rs
    return d[ro + value:ro + size]

def main():
    tc = sys.argv[1] if len(sys.argv) > 1 else 'vc6'
    d = f'{REPO}/references/toolchains/{tc}'
    cl = [f for f in os.listdir(d) if f.lower() == 'cl.exe'][0]
    env = dict(os.environ, WINEDEBUG='-all', WINEPREFIX=f'{REPO}/build/wine')
    for name, body in SHAPES.items():
        src = f'{SCRATCH}/dec_{name}.c'
        open(src, 'w').write(HDR + PRE + ALLOC + body)
        obj = f'{SCRATCH}/dec_{name}_{tc}.obj'
        if os.path.exists(obj): os.remove(obj)
        r = subprocess.run(['wine', f'{d}/{cl}', '/nologo', '/c'] + FLAGS + ['/Fo' + win(obj), win(src)],
                           capture_output=True, text=True, env=env, cwd=REPO)
        if not os.path.exists(obj):
            print(f'{name:16s} COMPILE FAILED'); print(r.stdout[-400:]); continue
        t = text_of(obj, '_BldrDecompress@12')
        n = sum(a == b for a, b in zip(t, REF))
        flag = 'EXACT' if t[:228] == REF else ''
        print(f'{name:16s} [{tc}] {n}/228 len={len(t)} {flag}')
        if not flag:
            open(f'{SCRATCH}/dec_{name}_{tc}.bin', 'wb').write(t)

main()
