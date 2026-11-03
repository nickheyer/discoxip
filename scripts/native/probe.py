#!/usr/bin/env python3
"""probe.py [--obj FILE] [--tc TOOLCHAIN] [--flags "extra flags"] [src.c]

Compiles one source with the MSVC toolchain under wine using the exact flags
build.json records for it, extracts .text, and diffs the routine that starts
with push ebp/mov ebp,esp against the reference 2bl.bin span.
"""
import struct, subprocess, sys, os, re, json, shlex

REPO = '/home/nick/code/discoxip'
SCRATCH = '/tmp/icebath-scratch/20261103-055159-4ec9b59a55def7f5'
REFBIN = f'{REPO}/build/native/5659/kernel/2bl.bin'
REF = open(REFBIN, 'rb').read()
TARGET_OFF, TARGET_LEN = 0x533, 92
REF_ROUTINE = REF[TARGET_OFF:TARGET_OFF+TARGET_LEN]

BUILD = json.load(open(f'{REPO}/native/boot/2bl/build.json'))
IMG = BUILD['5713']['bldr']

def win(p): return 'Z:' + p.replace('/', '\\')

def obj_flags_for(src, tc=None, extra=()):
    base = [f for f in IMG['cflags']]
    for o in IMG['objects']:
        if o['file'] == src:
            base += o.get('flags', [])
            tc = tc or o.get('tools', IMG['tools'])
    tc = tc or IMG['tools']
    return tc, base + list(extra)

def compile_obj(src, tc, flags, obj):
    d = f'{REPO}/references/toolchains/{tc}'
    cl = [f for f in os.listdir(d) if f.lower() == 'cl.exe'][0]
    env = dict(os.environ, WINEDEBUG='-all', WINEPREFIX=f'{REPO}/build/wine')
    cmd = ['wine', f'{d}/{cl}', '/nologo', '/c'] + flags + ['/Fo' + win(obj), win(src)]
    r = subprocess.run(cmd, capture_output=True, text=True, env=env, cwd=REPO)
    out = '\n'.join(l for l in (r.stdout + r.stderr).splitlines()
                    if not re.search(r'MESA|pci id', l))
    return out.strip()

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

def disasm(data, vma=0):
    path = f'{SCRATCH}/_dis.bin'
    open(path, 'wb').write(data)
    o = subprocess.run(['objdump', '-D', '-b', 'binary', '-m', 'i386', '-M', 'intel',
                        f'--adjust-vma={vma}', path], capture_output=True, text=True).stdout
    return [l.split('\t', 1)[1].rstrip() for l in o.splitlines() if '\t' in l and 'file format' not in l]

def show_diff(body):
    rl = disasm(body)
    rd = disasm(REF_ROUTINE, vma=0x400533)
    w = max([len(x) for x in rl + rd] + [0])
    for i in range(max(len(rl), len(rd))):
        a = rd[i] if i < len(rd) else ''
        b = rl[i] if i < len(rl) else ''
        print(f'{"  " if a == b else "<<"} {a:<{w}} | {b}')

def main():
    argv = sys.argv[1:]
    def take(name, default=None):
        if name in argv:
            i = argv.index(name); v = argv[i+1]; del argv[i:i+2]; return v
        return default
    obj_arg = take('--obj'); tc_arg = take('--tc'); extra = shlex.split(take('--flags', '') or '')
    src = argv[0] if argv else 'native/boot/2bl/bldr.c'
    if obj_arg:
        t = text_of(obj_arg)
        tag, tc = os.path.basename(obj_arg), tc_arg or '?'
    else:
        tc, flags = obj_flags_for(src, tc_arg, extra)
        tag = os.path.basename(src).rsplit('.', 1)[0]
        obj = f'{SCRATCH}/{tag}_{tc}{"".join(extra)}.obj'.replace('/', '_')
        log = compile_obj(src, tc, flags, obj)
        if not os.path.exists(obj):
            print('COMPILE FAILED'); print(log); return
        if log: print('  log:', log.replace('\n', '\n       '))
        t = text_of(obj)
    idx = t.find(bytes([0x55, 0x8b, 0xec]))
    body = t[idx:] if idx >= 0 else b''
    exact = body[:TARGET_LEN] == REF_ROUTINE
    n = sum(a == b for a, b in zip(body, REF_ROUTINE))
    print(f'{tag} [{tc}]  routine@+{idx:#x}  {"EXACT" if exact else str(n)+"/"+str(TARGET_LEN)}')
    if not exact:
        show_diff(body[:TARGET_LEN])

main()
