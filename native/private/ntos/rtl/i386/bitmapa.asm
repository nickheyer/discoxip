        title  "Bit Map Routines"
        .586p

_TEXT   SEGMENT PARA USE32 PUBLIC 'CODE'
        ASSUME  DS:FLAT, ES:FLAT, SS:NOTHING, FS:NOTHING, GS:NOTHING

        public  @RtlFindFirstSetRightMember@4
@RtlFindFirstSetRightMember@4 proc
        bsf     eax, ecx
        ret
@RtlFindFirstSetRightMember@4 endp

_TEXT   ENDS
        end
