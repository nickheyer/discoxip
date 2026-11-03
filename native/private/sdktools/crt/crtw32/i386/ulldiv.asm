        title   ulldiv - unsigned long divide routine
        .386p

_TEXT   SEGMENT PARA USE32 PUBLIC 'CODE'
        ASSUME  DS:FLAT, ES:FLAT, SS:NOTHING, FS:NOTHING, GS:NOTHING

LOWORD  equ     [0]
HIWORD  equ     [4]

; Returns the 64 bit dividend divided by the 64 bit divisor in edx eax and pops both arguments
        public  C _aulldiv
_aulldiv proc   near C
        push    ebx
        push    esi

DVND    equ     [esp + 12]
DVSR    equ     [esp + 20]

        mov     eax, HIWORD(DVSR)
        or      eax, eax
        jnz     short L1
        mov     ecx, LOWORD(DVSR)
        mov     eax, HIWORD(DVND)
        xor     edx, edx
        div     ecx
        mov     ebx, eax
        mov     eax, LOWORD(DVND)
        div     ecx
        mov     edx, ebx
        jmp     short L2

L1:
        mov     ecx, eax
        mov     ebx, LOWORD(DVSR)
        mov     edx, HIWORD(DVND)
        mov     eax, LOWORD(DVND)
L3:
        shr     ecx, 1
        rcr     ebx, 1
        shr     edx, 1
        rcr     eax, 1
        or      ecx, ecx
        jnz     short L3
        div     ebx
        mov     esi, eax

        mul     dword ptr HIWORD(DVSR)
        mov     ecx, eax
        mov     eax, LOWORD(DVSR)
        mul     esi
        add     edx, ecx
        jc      short L4

        cmp     edx, HIWORD(DVND)
        ja      short L4
        jb      short L5
        cmp     eax, LOWORD(DVND)
        jbe     short L5
L4:
        dec     esi
L5:
        xor     edx, edx
        mov     eax, esi

L2:
        pop     esi
        pop     ebx

        ret     16

_aulldiv endp

_TEXT   ENDS
        end
