#!/usr/bin/env python3
"""shapes.py [toolchain] — compile a matrix of BldrSmcChallengeResponse source shapes
with the recorded build.json flags and score each against the reference 92 bytes."""
import os, sys, struct, subprocess, re

REPO = '/home/nick/code/discoxip'
SCRATCH = '/tmp/icebath-scratch/20261103-055159-4ec9b59a55def7f5'
REF = open(f'{REPO}/build/native/5659/kernel/2bl.bin','rb').read()[0x533:0x533+92]
BUILD = __import__('json').load(open(f'{REPO}/native/boot/2bl/build.json'))
IMG = BUILD['5713']['bldr']
FLAGS = [f for f in IMG['cflags']]
for o in IMG['objects']:
    if o['file'] == 'native/boot/2bl/bldr.c':
        FLAGS += o.get('flags', [])

HDR = '#include "bldr.h"\n'
BODY = {
 'A_current': """
VOID NTAPI BldrSmcChallengeResponse(UCHAR Challenge0, UCHAR Challenge1, UCHAR Challenge2, UCHAR Challenge3, PUCHAR Response0, PUCHAR Response1)
{
    UCHAR KeyA;
    UCHAR KeyB;
    ULONG Round;

    KeyA = (Challenge3 + 0x63) ^ (Challenge1 + 0x39) ^ (Challenge0 << 2) ^ (Challenge2 >> 2);
    KeyB = (Challenge0 + 0x0B) ^ (Challenge2 + 0x1B) ^ (Challenge1 >> 2);
    *Response0 = 0x33;
    *Response1 = 0xED;
    for (Round = 0; Round < 4; Round++) {
        *Response0 += *Response1 ^ KeyA;
        *Response1 += *Response0 ^ KeyB;
    }
}
""",
 'B_swap': """
VOID NTAPI BldrSmcChallengeResponse(UCHAR Challenge0, UCHAR Challenge1, UCHAR Challenge2, UCHAR Challenge3, PUCHAR Response0, PUCHAR Response1)
{
    UCHAR KeyA;
    UCHAR KeyB;
    ULONG Round;

    KeyA = (Challenge3 + 0x63) ^ (Challenge1 + 0x39) ^ (Challenge0 << 2) ^ (Challenge2 >> 2);
    KeyB = (Challenge0 + 0x0B) ^ (Challenge2 + 0x1B) ^ (Challenge1 >> 2);
    *Response0 = 0x33;
    *Response1 = 0xED;
    for (Round = 0; Round < 4; Round++) {
        *Response0 += *Response1 ^ KeyA;
        *Response1 += KeyB ^ *Response0;
    }
}
""",
 'C_down': """
VOID NTAPI BldrSmcChallengeResponse(UCHAR Challenge0, UCHAR Challenge1, UCHAR Challenge2, UCHAR Challenge3, PUCHAR Response0, PUCHAR Response1)
{
    UCHAR KeyA;
    UCHAR KeyB;
    ULONG Round;

    KeyA = (Challenge3 + 0x63) ^ (Challenge1 + 0x39) ^ (Challenge0 << 2) ^ (Challenge2 >> 2);
    KeyB = (Challenge0 + 0x0B) ^ (Challenge2 + 0x1B) ^ (Challenge1 >> 2);
    *Response0 = 0x33;
    *Response1 = 0xED;
    for (Round = 4; Round != 0; Round--) {
        *Response0 += *Response1 ^ KeyA;
        *Response1 += *Response0 ^ KeyB;
    }
}
""",
 'D_swap_down': """
VOID NTAPI BldrSmcChallengeResponse(UCHAR Challenge0, UCHAR Challenge1, UCHAR Challenge2, UCHAR Challenge3, PUCHAR Response0, PUCHAR Response1)
{
    UCHAR KeyA;
    UCHAR KeyB;
    ULONG Round;

    KeyA = (Challenge3 + 0x63) ^ (Challenge1 + 0x39) ^ (Challenge0 << 2) ^ (Challenge2 >> 2);
    KeyB = (Challenge0 + 0x0B) ^ (Challenge2 + 0x1B) ^ (Challenge1 >> 2);
    *Response0 = 0x33;
    *Response1 = 0xED;
    for (Round = 4; Round != 0; Round--) {
        *Response0 += *Response1 ^ KeyA;
        *Response1 += KeyB ^ *Response0;
    }
}
""",
 'E_ulong_keys': """
VOID NTAPI BldrSmcChallengeResponse(UCHAR Challenge0, UCHAR Challenge1, UCHAR Challenge2, UCHAR Challenge3, PUCHAR Response0, PUCHAR Response1)
{
    ULONG KeyA;
    ULONG KeyB;
    ULONG Round;

    KeyA = (Challenge3 + 0x63) ^ (Challenge1 + 0x39) ^ (Challenge0 << 2) ^ (Challenge2 >> 2);
    KeyB = (Challenge0 + 0x0B) ^ (Challenge2 + 0x1B) ^ (Challenge1 >> 2);
    *Response0 = 0x33;
    *Response1 = 0xED;
    for (Round = 0; Round < 4; Round++) {
        *Response0 += *Response1 ^ KeyA;
        *Response1 += KeyB ^ *Response0;
    }
}
""",
 'F_do_down': """
VOID NTAPI BldrSmcChallengeResponse(UCHAR Challenge0, UCHAR Challenge1, UCHAR Challenge2, UCHAR Challenge3, PUCHAR Response0, PUCHAR Response1)
{
    UCHAR KeyA;
    UCHAR KeyB;
    ULONG Round;

    KeyA = (Challenge3 + 0x63) ^ (Challenge1 + 0x39) ^ (Challenge0 << 2) ^ (Challenge2 >> 2);
    KeyB = (Challenge0 + 0x0B) ^ (Challenge2 + 0x1B) ^ (Challenge1 >> 2);
    *Response0 = 0x33;
    *Response1 = 0xED;
    Round = 4;
    do {
        *Response0 += *Response1 ^ KeyA;
        *Response1 += KeyB ^ *Response0;
        Round--;
    } while (Round != 0);
}
""",
 'G_inline': """
VOID NTAPI BldrSmcChallengeResponse(UCHAR Challenge0, UCHAR Challenge1, UCHAR Challenge2, UCHAR Challenge3, PUCHAR Response0, PUCHAR Response1)
{
    ULONG Round;

    *Response0 = 0x33;
    *Response1 = 0xED;
    for (Round = 0; Round < 4; Round++) {
        *Response0 += *Response1 ^ ((Challenge3 + 0x63) ^ (Challenge1 + 0x39) ^ (Challenge0 << 2) ^ (Challenge2 >> 2));
        *Response1 += ((*Response0) ^ ((Challenge0 + 0x0B) ^ (Challenge2 + 0x1B) ^ (Challenge1 >> 2)));
    }
}
""",
 'H_ptr_ulong': """
VOID NTAPI BldrSmcChallengeResponse(UCHAR Challenge0, UCHAR Challenge1, UCHAR Challenge2, UCHAR Challenge3, PUCHAR Response0, PUCHAR Response1)
{
    UCHAR KeyA;
    UCHAR KeyB;
    ULONG Round;

    KeyA = (Challenge3 + 0x63) ^ (Challenge1 + 0x39) ^ (Challenge0 << 2) ^ (Challenge2 >> 2);
    KeyB = (Challenge0 + 0x0B) ^ (Challenge2 + 0x1B) ^ (Challenge1 >> 2);
    *Response0 = 0x33;
    *Response1 = 0xED;
    for (Round = 0; Round < 4; Round++) {
        *Response0 = (UCHAR)(*Response0 + (*Response1 ^ KeyA));
        *Response1 = (UCHAR)(KeyB ^ *Response0) + *Response1;
    }
}
""",
}

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
        src = f'{SCRATCH}/shape_{name}.c'
        open(src, 'w').write(HDR + body)
        obj = f'{SCRATCH}/shape_{name}_{tc}.obj'
        if os.path.exists(obj): os.remove(obj)
        cmd = ['wine', f'{d}/{cl}', '/nologo', '/c'] + FLAGS + ['/Fo' + win(obj), win(src)]
        r = subprocess.run(cmd, capture_output=True, text=True, env=env, cwd=REPO)
        if not os.path.exists(obj):
            print(f'{name:16s} [{tc}] COMPILE FAILED'); print(r.stdout[-500:], r.stderr[-500:]); continue
        t = text_of(obj)
        i = t.find(bytes([0x55, 0x8b, 0xec]))
        body_ = t[i:i+92] if i >= 0 else b''
        n = sum(a == b for a, b in zip(body_, REF))
        flag = 'EXACT' if body_ == REF else '     '
        print(f'{name:16s} [{tc}] {n}/92 {flag}')
        if body_ != REF:
            open(f'{SCRATCH}/shape_{name}_{tc}.bin','wb').write(t[i:])

main()
