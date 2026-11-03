        title  "First Stage Loader Startup"
        .586p
        OPTION CASEMAP:NONE

FBL_STACK               equ 0008F000h
FBL_LOAD_BUFFER         equ 003FA000h
KERNEL_SEGMENT_DS       equ 10h
PCI_TYPE1_ADDR_PORT     equ 0CF8h
MCPX_RESET_CONTROL      equ 8000036Ch

        extrn   _FblMain@4:near
        extrn   _FblVerifySignature@12:near
        extrn   _A_SHAInit@4:near
        extrn   _A_SHAUpdate@12:near
        extrn   _A_SHAFinal@8:near

_TEXT   SEGMENT PARA PUBLIC USE32 'CODE'
        ASSUME  DS:FLAT, ES:FLAT, SS:FLAT, FS:NOTHING, GS:NOTHING

FblHeader:
        jmp     _FblEntry

        ORG     08h
        public  _FblCryptoTablePointer
_FblCryptoTablePointer  dd offset FblCryptoTable
                        dd 0

        public  _FblKeyBlob
_FblKeyBlob     label byte
                db      0A4h, 01Dh, 02Bh, 086h, 00Bh, 0FBh, 0F6h, 089h, 06Bh, 087h, 009h, 0CDh, 06Eh, 005h, 0FBh, 0FEh
                db      00Ch, 044h, 02Ch, 0F1h, 0A6h, 031h, 03Eh, 0BDh, 022h, 091h, 026h, 062h, 0A4h, 09Ah, 024h, 098h
                db      0BBh, 043h, 03Ah, 081h, 048h, 045h, 09Fh, 03Ch, 075h, 0D4h, 063h, 06Ch, 0BBh, 066h, 0BEh, 01Dh
                db      03Dh, 0F6h, 0CFh, 038h, 055h, 023h, 00Dh, 04Ah, 029h, 083h, 0D1h, 02Fh, 098h, 0FEh, 082h, 08Bh
                db      0FFh, 008h, 0C7h, 048h, 039h, 003h, 053h, 037h, 095h, 0FDh, 07Bh, 00Ah, 079h, 05Ch, 01Ch, 08Fh
                db      00Bh, 090h, 0A3h, 04Dh, 0EBh, 0FFh, 097h, 096h, 08Ch, 046h, 042h, 065h, 00Dh, 0C6h, 003h, 0A8h
                db      038h, 014h, 0E8h, 0C4h, 0E6h, 091h, 0DFh, 09Dh, 071h, 079h, 056h, 0DEh, 0E5h, 0EBh, 0C4h, 0C3h
                db      0FEh, 0C1h, 0B2h, 018h, 06Bh, 087h, 005h, 0F8h, 09Bh, 0DBh, 05Ch, 034h, 075h, 03Fh, 0B8h, 06Dh
                db      09Fh, 00Fh, 0D1h, 0C0h, 00Bh, 085h, 0EBh, 0D5h, 0C5h, 01Ah, 052h, 068h, 01Fh, 048h, 002h, 0BFh
                db      0FEh, 0DBh, 0A2h, 08Eh, 00Fh, 016h, 0DCh, 04Bh, 090h, 009h, 0F6h, 0FFh, 059h, 049h, 052h, 017h
                db      075h, 010h, 02Fh, 00Ah, 04Ah, 066h, 04Eh, 004h, 0ABh, 083h, 054h, 0D4h, 053h, 0C8h, 023h, 091h
                db      0E0h, 0BDh, 0CAh, 012h, 081h, 0FBh, 0E9h, 0B8h, 020h, 046h, 0CAh, 040h, 0E2h, 006h, 0F6h, 06Fh
                db      0AAh, 0BAh, 00Dh, 0DAh, 0CAh, 0F4h, 016h, 080h, 0F9h, 082h, 0C6h, 055h, 0E2h, 030h, 073h, 0E4h
                db      089h, 064h, 012h, 083h, 04Dh, 05Ah, 0D6h, 030h, 035h, 009h, 08Dh, 026h, 08Fh, 080h, 08Ah, 092h
                db      08Ah, 011h, 0F7h, 026h, 0EAh, 042h, 018h, 0DFh, 020h, 0F0h, 0D9h, 0DDh, 0C4h, 05Dh, 059h, 043h
                db      036h, 000h, 04Ah, 05Bh, 098h, 0B2h, 02Ah, 017h, 003h, 056h, 073h, 038h, 066h, 0DCh, 044h, 065h
                db      0D9h, 00Dh, 03Fh, 0E0h, 0ABh, 07Ah, 030h, 0E0h, 060h, 00Fh, 04Eh, 09Bh, 076h, 082h, 0C2h, 078h
                db      0C6h, 087h, 078h, 0B7h, 0A2h, 06Fh, 072h, 07Eh, 0BEh, 0DAh, 0EFh, 086h

        public  _FblHeapTop
_FblHeapTop     dd 00400000h

FblCryptoTable  dd offset _A_SHAInit@4
                dd offset _A_SHAUpdate@12
                dd offset _A_SHAFinal@8
                dd offset _FblVerifySignature@12

        public  _FblDigestInfos
_FblDigestInfos dd offset FblDigestInfoSha1
                dd offset FblDigestInfoSha1NoParameters
                dd offset FblDigestInfoNone
                dd 0

FblDigestInfoNone               db 0, 0, 0, 0
FblDigestInfoSha1NoParameters   db 0Dh, 14h, 04h, 1Ah, 02h, 03h, 0Eh, 2Bh, 05h, 06h, 07h, 30h, 1Fh, 30h, 0, 0
FblDigestInfoSha1               db 0Fh, 14h, 04h, 00h, 05h, 1Ah, 02h, 03h, 0Eh, 2Bh, 05h, 06h, 09h, 30h, 21h, 30h

        ORG     180h
        dd      offset _FblKeyBlob
        dd      offset FblCryptoTable

; Loads flat segments and a stack, decrypts the loader into its buffer and enters it, resetting on failure
        public  _FblEntry
_FblEntry:
        mov     eax, KERNEL_SEGMENT_DS
        mov     ds, eax
        mov     es, eax
        mov     ss, eax
        mov     esp, FBL_STACK
        xor     eax, eax
        mov     fs, eax
        mov     gs, eax
        mov     esi, FBL_LOAD_BUFFER
        push    esi
        call    _FblMain@4
        test    eax, eax
        je      FblReset
        mov     ebp, esi
        jmp     eax
FblReset:
        mov     edx, PCI_TYPE1_ADDR_PORT
        mov     eax, MCPX_RESET_CONTROL
        out     dx, eax
        add     edx, 4
        mov     eax, 01000000h
        out     dx, eax
        cli
        hlt

_TEXT   ENDS
        end
