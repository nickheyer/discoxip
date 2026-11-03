#!/usr/bin/env python3
"""rd.py <refoff> <reflen> <oursoff> <ourslen> — side-by-side disassembly of two spans."""
import subprocess, sys
def dis(f, off, ln):
    o = subprocess.run(['objdump','-D','-b','binary','-m','i386','-M','intel',
                        f'--adjust-vma={0x400000+off}',
                        f'--start-address={0x400000+off}',
                        f'--stop-address={0x400000+off+ln}', f],
                       capture_output=True, text=True).stdout
    return [l.split('\t',1)[1].rstrip() for l in o.splitlines() if '\t' in l and 'file format' not in l]
ref='build/native/5659/kernel/2bl.bin'; ours='build/native/verify/5713/2bl.bin'
ro,rl = int(sys.argv[1],0), int(sys.argv[2],0)
oo,ol = int(sys.argv[3],0), int(sys.argv[4],0)
r=dis(ref,ro,rl); o=dis(ours,oo,ol)
w=max(len(x) for x in r+o)
for i in range(max(len(r),len(o))):
    a=r[i] if i<len(r) else ''
    b=o[i] if i<len(o) else ''
    print(f'{"  " if a==b else "<<"} {a:<{w}} | {b}')
