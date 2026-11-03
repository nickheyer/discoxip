        title  "Second Bootloader Startup"
        .586p
        OPTION CASEMAP:NONE
        OPTION DOTNAME

BLDR_BASE               equ 00400000h
BLDR_LOAD               equ 00090000h
BLDR_IMAGE_DWORDS       equ 1800h
BLDR_STACK              equ 80400000h
PAGE_DIRECTORY          equ 0000F000h
KERNEL_SEGMENT_CS       equ 08h
KERNEL_SEGMENT_DS       equ 10h
SUPERIO_CONFIG_PORT     equ 2Eh

if BLDR_BUILD ge 5713
MTRR_RAM_MASK           equ 0F8000800h
else
MTRR_RAM_MASK           equ 0FC000800h
endif

        extrn   _BldrMain@0:near
if BLDR_BUILD ge 5713
        extrn   _BldrPrepareReentry@0:near
        extrn   _BldrShadowRomEnd:dword
endif

_DATA   SEGMENT DWORD PUBLIC USE32 'DATA'

        public  _BldrHeader
        public  _BldrLoadParameters
        public  _BldrRegionKey
        public  _BldrKeys
        public  _BldrEepromKey
        public  _BldrCertificateKey
        public  _BldrKernelKey

_BldrHeader             dd offset _BldrEntry - BLDR_BASE + BLDR_LOAD
_BldrLoadParameters     db 40h dup (0)
                        dd 3F8h
                        dd 19200
if BLDR_BUILD lt 5713
                        dd 0, 0, 9875h, 9873h
endif
_BldrRegionKey          db 057h, 042h, 029h, 00Ch, 030h, 01Eh, 0D3h, 001h, 0B3h, 0E5h, 05Dh, 028h, 050h, 031h, 0E1h, 0CEh
_BldrKeys               label byte
if BLDR_BUILD ge 5713
_BldrEepromKey          db 01Dh, 0F3h, 05Ch, 083h, 08Eh, 0C9h, 0B6h, 0FCh, 0BDh, 0F6h, 061h, 0ABh, 04Fh, 006h, 033h, 0E4h
else
_BldrEepromKey          db 02Ah, 03Bh, 0ADh, 02Ch, 0B1h, 094h, 04Fh, 093h, 0AAh, 0CDh, 0CDh, 07Eh, 00Ah, 0C2h, 0EEh, 05Ah
endif
_BldrCertificateKey     db 05Ch, 007h, 033h, 0AEh, 004h, 001h, 0F7h, 0E8h, 0BAh, 079h, 093h, 0FDh, 0CDh, 02Fh, 01Fh, 0E0h
if BLDR_BUILD ge 5713
_BldrKernelKey          db 032h, 0E3h, 03Ch, 09Bh, 079h, 0A5h, 00Bh, 07Eh, 035h, 0B5h, 012h, 00Fh, 0E1h, 03Fh, 0FDh, 0DBh
else
_BldrKernelKey          db 06Bh, 030h, 02Bh, 076h, 068h, 0D4h, 0D5h, 047h, 0E5h, 086h, 00Ch, 030h, 0D9h, 0C3h, 092h, 084h
endif

_DATA   ENDS

_TEXT   SEGMENT DWORD PUBLIC USE32 'CODE'
        ASSUME  DS:FLAT, ES:FLAT, SS:FLAT, FS:NOTHING, GS:NOTHING

        dd      offset _BldrKeys
        dd      offset _BldrReenter

; Programs the MTRRs, relocates the image, maps memory with large pages and calls the loader on its own stack
        public  _BldrEntry
_BldrEntry:
        mov     eax, cr0
        or      eax, 60000000h
        mov     cr0, eax
        wbinvd
        mov     eax, cr3
        mov     cr3, eax
        mov     ecx, 2FFh
        xor     eax, eax
        xor     edx, edx
        wrmsr
        mov     ecx, 200h
        mov     eax, 6
        xor     edx, edx
        wrmsr
        inc     ecx
        mov     eax, MTRR_RAM_MASK
        mov     edx, 0Fh
        wrmsr
        inc     ecx
        mov     eax, 0FFF80005h
        xor     edx, edx
        wrmsr
        inc     ecx
        mov     eax, 0FFF80800h
        mov     edx, 0Fh
        wrmsr
        inc     ecx
        xor     eax, eax
        xor     edx, edx
@@:     wrmsr
        inc     ecx
        cmp     ecx, 20Fh
        jbe     @B
        mov     ecx, 2FFh
        mov     eax, 800h
        wrmsr
        mov     eax, cr0
        and     eax, 9FFFFFFFh
        mov     cr0, eax
        mov     eax, KERNEL_SEGMENT_DS
        mov     ds, eax
        mov     es, eax
        mov     ss, eax
        mov     esp, BLDR_BASE
        xor     eax, eax
        mov     fs, eax
        mov     gs, eax
        cld
if BLDR_BUILD ge 5713
        mov     esi, ebp
        mov     edi, BLDR_BASE
        cmp     esi, edi
        je      @F
        mov     ecx, BLDR_IMAGE_DWORDS
        rep movsd
@@:
else
        mov     esi, BLDR_LOAD
        mov     edi, BLDR_BASE
        mov     ecx, BLDR_IMAGE_DWORDS
        rep movsd
endif
        mov     edi, PAGE_DIRECTORY
        mov     ecx, 40h
        mov     eax, 0E3h
@@:     mov     [edi+800h], eax
        stosd
        add     eax, 400000h
        loop    @B
        mov     ecx, 1C0h
        xor     eax, eax
@@:     mov     [edi+800h], eax
        stosd
        loop    @B
        mov     edi, PAGE_DIRECTORY
        mov     eax, PAGE_DIRECTORY + 63h
        mov     [edi+0C00h], eax
        mov     eax, 0FFC000E3h
        mov     [edi+0FFCh], eax
        mov     eax, 0FD0000FBh
        mov     ebx, eax
        shr     ebx, 20
        add     edi, ebx
        mov     [edi], eax
        add     edi, 4
        add     eax, 400000h
        mov     [edi], eax
        add     edi, 4
        add     eax, 400000h
        mov     [edi], eax
        add     edi, 4
        add     eax, 400000h
        mov     [edi], eax
        mov     eax, cr0
        mov     ebx, eax
        and     eax, 0DFFFFFFFh
        or      eax, 40000000h
        mov     cr0, eax
        wbinvd
        mov     ecx, 277h
        mov     eax, 70106h
        mov     edx, eax
        wrmsr
        wbinvd
        mov     cr0, ebx
        mov     eax, cr4
        or      eax, 610h
        mov     cr4, eax
        mov     eax, PAGE_DIRECTORY
        mov     cr3, eax
        mov     eax, cr0
        or      eax, 80010020h
        mov     cr0, eax
        jmp     short @F
@@:     mov     esp, BLDR_STACK
        lea     eax, _BldrMain@0
        call    eax
        jmp     $

if BLDR_BUILD ge 5713
; Maps the flash window onto the shadow copy at ecx, reloads segments and restarts the loader
        public  _BldrReenter
_BldrReenter:
        cli
        mov     edx, ecx
        mov     edi, PAGE_DIRECTORY + 80000000h
        mov     ecx, 40h
        mov     eax, 0E3h
@@:     mov     [edi+800h], eax
        stosd
        add     eax, 400000h
        loop    @B
        mov     ecx, 1C0h
        xor     eax, eax
@@:     mov     [edi+800h], eax
        stosd
        loop    @B
        mov     edi, PAGE_DIRECTORY + 80000000h
        mov     eax, PAGE_DIRECTORY + 63h
        mov     [edi+0C00h], eax
        sub     eax, 1000h
        mov     [edi+0FFCh], eax
        sub     edi, 400h
        lea     eax, [edx+63h]
        mov     ecx, 100h
@@:     stosd
        add     eax, 1000h
        loop    @B
        mov     eax, PAGE_DIRECTORY
        mov     cr3, eax
        lgdt    fword ptr _BldrGdtr
        db      0EAh
        dd      offset BldrReenterFlat
        dw      KERNEL_SEGMENT_CS
BldrReenterFlat:
        mov     eax, KERNEL_SEGMENT_DS
        mov     ds, eax
        mov     es, eax
        mov     ss, eax
        mov     esp, BLDR_STACK
        xor     eax, eax
        mov     fs, eax
        mov     gs, eax
        add     edx, 80100000h
        mov     _BldrShadowRomEnd, edx
        call    _BldrPrepareReentry@0
        lea     eax, _BldrMain@0
        call    eax
        jmp     $
else
; Spins forever since this loader has no second entry
        public  _BldrReenter
_BldrReenter:
        jmp     $
endif

        ALIGN   4
        public  _BldrGdt
_BldrGdt                dq 0
                        dq 00CF9B000000FFFFh
                        dq 00CF93000000FFFFh
        public  _BldrGdtr
_BldrGdtr               dw 18h
                        dd offset _BldrGdt

_TEXT   ENDS

.text   SEGMENT DWORD PUBLIC USE32 'CODE'
        ASSUME  DS:FLAT, ES:FLAT, SS:FLAT, FS:NOTHING, GS:NOTHING

; Enables the Super IO serial port at 3F8h through the configuration port at 2Eh
        public  _BldrEnableSerialPort@0
_BldrEnableSerialPort@0 proc
        mov     edx, SUPERIO_CONFIG_PORT
        mov     al, 55h
        out     dx, al
        jmp     $+2
        mov     al, 07h
        out     dx, al
        jmp     $+2
        inc     edx
        mov     al, 04h
        out     dx, al
        jmp     $+2
        dec     edx
        mov     al, 30h
        out     dx, al
        jmp     $+2
        inc     edx
        mov     al, 01h
        out     dx, al
        jmp     $+2
        dec     edx
        mov     al, 60h
        out     dx, al
        jmp     $+2
        inc     edx
        mov     al, 03h
        out     dx, al
        jmp     $+2
        dec     edx
        mov     al, 61h
        out     dx, al
        jmp     $+2
        inc     edx
        mov     al, 0F8h
        out     dx, al
        jmp     $+2
        dec     edx
        mov     al, 0AAh
        out     dx, al
        ret
_BldrEnableSerialPort@0 endp

.text   ENDS
        end
