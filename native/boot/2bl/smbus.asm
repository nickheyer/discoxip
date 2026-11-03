        title  "SMBus Polled Access"
        .586p
        OPTION CASEMAP:NONE

SMB_GLOBAL_STATUS               equ 0C000h
SMB_GLOBAL_ENABLE               equ 0C002h
SMB_HOST_ADDRESS                equ 0C004h
SMB_HOST_DATA                   equ 0C006h
SMB_HOST_COMMAND                equ 0C008h
SMB_PROTOCOL_BYTE_DATA          equ 0Ah
SMB_STATUS_ACTIVE               equ 08h
SMB_STATUS_COLLISION            equ 02h
SMB_STATUS_TIMEOUT              equ 20h
SMB_STATUS_PROTOCOL_ERROR       equ 04h
SMB_STATUS_COMPLETE             equ 10h
STATUS_IO_TIMEOUT               equ 0C00000B5h
STATUS_DEVICE_PROTOCOL_ERROR    equ 0C0000186h
STATUS_IO_DEVICE_ERROR          equ 0C0000185h

_TEXT   SEGMENT DWORD PUBLIC USE32 'CODE'
        ASSUME  DS:FLAT, ES:FLAT, SS:FLAT, FS:NOTHING, GS:NOTHING

; Reads a byte or word register of an SMBus device, restarting the transfer after a bus collision
        public  _HalReadSMBusValue@16
_HalReadSMBusValue@16 proc
        xor     eax, eax
        mov     al, [esp+4]
        mov     edx, SMB_HOST_ADDRESS
        or      al, 1
        out     dx, al
        mov     al, [esp+8]
        add     edx, SMB_HOST_COMMAND - SMB_HOST_ADDRESS
        out     dx, al
        sub     edx, SMB_HOST_COMMAND - SMB_GLOBAL_STATUS
        in      ax, dx
        out     dx, ax
        add     edx, SMB_GLOBAL_ENABLE - SMB_GLOBAL_STATUS
        mov     al, SMB_PROTOCOL_BYTE_DATA
        cmp     byte ptr [esp+0Ch], 0
        je      ReadStart
        inc     al
ReadStart:
        out     dx, al
        sub     edx, SMB_GLOBAL_ENABLE - SMB_GLOBAL_STATUS
ReadPoll:
        in      al, dx
        test    al, SMB_STATUS_ACTIVE
        jnz     ReadPoll
        test    al, SMB_STATUS_COLLISION
        jnz     _HalReadSMBusValue@16
        mov     edx, STATUS_IO_TIMEOUT
        test    al, SMB_STATUS_TIMEOUT
        jnz     ReadExit
        mov     edx, STATUS_DEVICE_PROTOCOL_ERROR
        test    al, SMB_STATUS_PROTOCOL_ERROR
        jnz     ReadExit
        mov     edx, STATUS_IO_DEVICE_ERROR
        test    al, SMB_STATUS_COMPLETE
        jz      ReadExit
        xor     eax, eax
        mov     edx, SMB_HOST_DATA
        in      ax, dx
        cmp     byte ptr [esp+0Ch], 0
        jne     ReadStore
        xor     ah, ah
ReadStore:
        mov     edx, [esp+10h]
        mov     [edx], eax
        xor     edx, edx
ReadExit:
        mov     eax, edx
        ret     10h
_HalReadSMBusValue@16 endp

        ALIGN   4

; Writes a byte or word register of an SMBus device, restarting the transfer after a bus collision
        public  _HalWriteSMBusValue@16
_HalWriteSMBusValue@16 proc
        xor     eax, eax
        mov     al, [esp+4]
        mov     edx, SMB_HOST_ADDRESS
        and     al, 0FEh
        out     dx, al
        mov     al, [esp+8]
        add     edx, SMB_HOST_COMMAND - SMB_HOST_ADDRESS
        out     dx, al
        mov     eax, [esp+10h]
        sub     edx, SMB_HOST_COMMAND - SMB_HOST_DATA
        out     dx, ax
        sub     edx, SMB_HOST_DATA - SMB_GLOBAL_STATUS
        in      ax, dx
        out     dx, ax
        add     edx, SMB_GLOBAL_ENABLE - SMB_GLOBAL_STATUS
        mov     al, SMB_PROTOCOL_BYTE_DATA
        cmp     byte ptr [esp+0Ch], 0
        je      WriteStart
        inc     al
WriteStart:
        out     dx, al
        sub     edx, SMB_GLOBAL_ENABLE - SMB_GLOBAL_STATUS
WritePoll:
        in      al, dx
        test    al, SMB_STATUS_ACTIVE
        jnz     WritePoll
        test    al, SMB_STATUS_COLLISION
        jnz     _HalWriteSMBusValue@16
        mov     edx, STATUS_IO_TIMEOUT
        test    al, SMB_STATUS_TIMEOUT
        jnz     WriteExit
        mov     edx, STATUS_DEVICE_PROTOCOL_ERROR
        test    al, SMB_STATUS_PROTOCOL_ERROR
        jnz     WriteExit
        test    al, SMB_STATUS_COMPLETE
        mov     edx, STATUS_IO_DEVICE_ERROR
        jz      WriteExit
        xor     edx, edx
WriteExit:
        mov     eax, edx
        ret     10h
_HalWriteSMBusValue@16 endp

_TEXT   ENDS
        end
