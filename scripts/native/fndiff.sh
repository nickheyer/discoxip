#!/bin/sh
# fndiff.sh <refoff> <ouroff> <reflen> <ourlen> : side by side instruction diff
R=${R:-build/native/5659/kernel/2bl.bin}; O=${O:-build/native/verify/5713/2bl.bin}
dis() { objdump -D -b binary -m i386 -M intel --adjust-vma=0x400000 --start-address=$((0x400000+$1)) --stop-address=$((0x400000+$1+$2)) $3 | tail -n +8 | sed 's/^ *[0-9a-f]*:\t//' | sed 's/   *$//'; }
diff --side-by-side --width=130 <(dis $1 $3 $R) <(dis $2 $4 $O) | grep -v '^.\{64\} *$' | head -${N:-80}
