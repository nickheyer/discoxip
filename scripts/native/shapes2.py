#!/usr/bin/env python3
"""shapes2.py [toolchain] — narrower variants around shape G."""
import os, sys, struct, subprocess

REPO = '/home/nick/code/discoxip'
SCRATCH = '/tmp/icebath-scratch/20261103-055159-4ec9b59a55def7f5'
REF = open(f'{REPO}/build/native/5659/kernel/2bl.bin','rb').read()[0x533:0x533+92]
BUILD = __import__('json').load(open(f'{REPO}/native/boot/2bl/build.json'))
IMG = BUILD['5713']['bldr']
FLAGS = list(IMG['cflags'])
for o in IMG['objects']:
    if o['file'] == 'native/boot/2bl/bldr.c':
        FLAGS += o.get('flags', [])

HDR = '#include "bldr.h"\n'
SIG = 'VOID NTAPI BldrSmcChallengeResponse(UCHAR Challenge0, UCHAR Challenge1, UCHAR Challenge2, UCHAR Challenge3, PUCHAR Response0, PUCHAR Response1)\n'
KEYA = '((Challenge3 + 0x63) ^ (Challenge1 + 0x39) ^ (Challenge0 << 2) ^ (Challenge2 >> 2))'
KEYB = '((Challenge0 + 0x0B) ^ (Challenge2 + 0x1B) ^ (Challenge1 >> 2))'

BODY = {
 # G as tested: both keys inlined
 'G': f"""{{{
    ULONG Round;
    *Response0 = 0x33;
    *Response1 = 0xED;
    for (Round = 0; Round < 4; Round++) {{
        *Response0 += *Response1 ^ {KEYA};
        *Response1 += ((*Response0) ^ {KEYB});
    }}
}}""",
 # only KeyB inlined, KeyA a local
 'K1': f"""{{{
    UCHAR KeyA;
    ULONG Round;
    KeyA = {KEYA};
    *Response0 = 0x33;
    *Response1 = 0xED;
    for (Round = 0; Round < 4; Round++) {{
        *Response0 += *Response1 ^ KeyA;
        *Response1 += ((*Response0) ^ {KEYB});
    }}
}}""",
 # KeyA local assigned before stores, KeyB computed inside loop from locals
 'K2': f"""{{{
    UCHAR KeyA;
    ULONG Round;
    KeyA = {KEYA};
    *Response0 = 0x33;
    *Response1 = 0xED;
    for (Round = 0; Round < 4; Round++) {{
        *Response0 += *Response1 ^ KeyA;
        *Response1 += ((*Response0) ^ ((Challenge0 + 0x0B) ^ (Challenge2 + 0x1B) ^ (Challenge1 >> 2)));
    }}
}}""",
 # both locals, KeyB assignment after the stores
 'K3': f"""{{{
    UCHAR KeyA;
    UCHAR KeyB;
    ULONG Round;
    KeyA = {KEYA};
    *Response0 = 0x33;
    KeyB = {KEYB};
    *Response1 = 0xED;
    for (Round = 0; Round < 4; Round++) {{
        *Response0 += *Response1 ^ KeyA;
        *Response1 += ((*Response0) ^ KeyB);
    }}
}}""",
 # G with the second stmt using KeyB ^ *Response0
 'G2': f"""{{{
    ULONG Round;
    *Response0 = 0x33;
    *Response1 = 0xED;
    for (Round = 0; Round < 4; Round++) {{
        *Response0 += *Response1 ^ {KEYA};
        *Response1 += {KEYB} ^ *Response0;
    }}
}}""",
 # G, stores interleaved as in reference (0x33 store before KeyB finish)
 'G3': f"""{{{
    ULONG Round;
    *Response0 = 0x33;
    *Response1 = 0xED;
    for (Round = 0; Round < 4; Round++) {{
        *Response0 = (UCHAR)(*Response0 + (*Response1 ^ {KEYA}));
        *Response1 = (UCHAR)(KeyB) + *Response1;
    }}
}}""",
}
BODY['G3'] = BODY['G3'].replace('KeyB', KEYB)

def win(p): return 'Z:' + p.replace('/', '\\')
def text_of(obj):
    d = open(obj, 'rb').read()
    nsec = struct.unpack_from('<H', d, 2)[0]
    out = b''
    for i in range(nsec):
        name, vs, va, rs, ro = struct.unpack_from('<8sIIII', d, 20 + 40*i)
        nm = name.rstrip(b'\0').decode('latin1')
        if nm.startswith('.text'):
            out += d[ro:ro + (vs if vs else rs)]
    return out

def main():
    tc = sys.argv[1] if len(sys.argv) > 1 else 'vc7'
    d = f'{REPO}/references/toolchains/{tc}'
    cl = [f for f in os.listdir(d) if f.lower() == 'cl.exe'][0]
    env = dict(os.environ, WINEDEBUG='-all', WINEPREFIX=f'{REPO}/build/wine')
    for name, body in BODY.items():
        src = f'{SCRATCH}/v2_{name}.c'
        open(src, 'w').write(HDR + SIG + body + '\n')
        obj = f'{SCRATCH}/v2_{name}_{tc}.obj'
        if os.path.exists(obj): os.remove(obj)
        r = subprocess.run(['wine', f'{d}/{cl}', '/nologo', '/c'] + FLAGS + ['/Fo' + win(obj), win(src)],
                           capture_output=True, text=True, env=env, cwd=REPO)
        if not os.path.exists(obj):
            print(f'{name:6s} COMPILE FAILED'); print(r.stdout[-400:]); continue
        t = text_of(obj)
        i = t.find(bytes([0x55, 0x8b, 0xec]))
        body_ = t[i:i+92] if i >= 0 else b''
        n = sum(a == b for a, b in zip(body_, REF))
        print(f'{name:6s} [{tc}] {n}/92 {"EXACT" if body_==REF else ""}')
main()
