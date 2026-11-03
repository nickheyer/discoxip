# Native decompilation probe harness

Scripts for iterating on a single 2bl routine against the reference
`build/native/5659/kernel/2bl.bin` without paying the ~3 minute full verify.

All scripts run from the repo root. Toolchains live in `references/toolchains/{vc6,vc7,vc71}`
and run under wine with `WINEPREFIX=$PWD/build/wine`.

| Script | What it does |
| --- | --- |
| `probe.py [src.c]` | Compiles one source with the flags `build.json` records for it, extracts `.text`, diffs `BldrSmcChallengeResponse` against the reference 92 bytes at `0x533`, prints a side-by-side disassembly on mismatch. |
| `shapes.py [tc]` / `shapes2.py [tc]` | Compiles a matrix of `BldrSmcChallengeResponse` source shapes and scores each. The winning shape is `G` (key expressions inlined into the loop body so CSE hoists them), exact on vc6, vc7 and vc71. |
| `dec_shapes.py` / `dec_batch.py` / `dec_ref_shape.py` | Same idea for `BldrDecompress` (reference is 228 bytes at `0x716`). They extract the routine by COFF symbol, so the score is for the function alone with relocations unapplied. |
| `rd.py <refoff> <len> [oursoff] [ourslen]` | Side-by-side disassembly of any two spans, reference vs our current `build/native/verify/5713/2bl.bin`. The everyday tool. |
| `layout.py` | Walks the routine table in `build/native/verify/5713/routines.txt` and reports the first routine that diverges. |
| `fndiff.sh <off> <len> [name]` | Original single-routine diff from the previous session. `cc.sh` is its raw wine/CL wrapper. |

Fast loop for one image:

```
make native-build-5713 && python3 scripts/native/rd.py 0x716 228
```

Full verify (also rewrites the two docs, the slow part) is `make native-verify`.
