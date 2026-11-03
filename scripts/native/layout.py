#!/usr/bin/env python3
"""layout.py — walks the .text of a raw 2bl.bin and a built one, matching the
routine table in build/native/verify/5713/routines.txt, and reports size drift."""
import subprocess, sys, re

REF = 'build/native/5659/kernel/2bl.bin'
OURS = 'build/native/verify/5713/2bl.bin'
TABLE = 'build/native/verify/5713/routines.txt'

rows = []
for line in open(TABLE):
    m = re.match(r'\s*(0x)?([0-9a-f]{6})\s+(\d+)\s+(\S+)', line)
    if m:
        rows.append((int(m.group(2), 16), int(m.group(3)), m.group(4)))

def dis(f, off, ln):
    o = subprocess.run(['objdump','-D','-b','binary','-m','i386','-M','intel',
                        f'--adjust-vma={0x400000+off}',
                        f'--start-address={0x400000+off}',
                        f'--stop-address={0x400000+off+ln}', f],
                       capture_output=True, text=True).stdout
    return [l.split('\t',1)[1].rstrip() for l in o.splitlines() if '\t' in l and 'file format' not in l]

prev_end = 0
for off, ln, name in rows:
    r = dis(REF, off, ln)
    o = dis(OURS, off, ln)
    same = r == o
    if not same:
        w = max(len(x) for x in r+o)
        print(f'=== {name} @{off:#x} ref={ln} ours={len(o)*1}')
        for i in range(max(len(r), len(o))):
            a = r[i] if i < len(r) else ''
            b = o[i] if i < len(o) else ''
            print(f'{"  " if a==b else "<<"} {a:<{w}} | {b}')
    prev_end = off + ln
