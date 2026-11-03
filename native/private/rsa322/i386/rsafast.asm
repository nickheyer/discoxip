        title  "Multiple Precision Add and Subtract"
        .386p

_TEXT   SEGMENT PARA USE32 PUBLIC 'CODE'
        ASSUME  DS:FLAT, ES:FLAT, SS:NOTHING, FS:NOTHING, GS:NOTHING

; Stores the N digit sum of B and C in A and returns the carry out
        public  _Add@16
_Add@16 proc
        mov     ecx, [esp+16]
        xor     eax, eax
        test    ecx, ecx
        jbe     done_add
        push    edi
        mov     edi, [esp+12]
        push    esi
        mov     esi, [esp+20]
        push    ebx
        push    ebp
        mov     ebx, [esp+20]
        sub     edi, esi
        sub     ebx, esi
add_loop:
        mov     edx, [edi+esi]
        mov     ebp, [esi]
        add     edx, eax
        mov     eax, 0
        adc     eax, eax
        add     edx, ebp
        adc     eax, 0
        mov     [ebx+esi], edx
        and     eax, 1
        add     esi, 4
        dec     ecx
        jne     add_loop
        pop     ebp
        pop     ebx
        pop     esi
        pop     edi
done_add:
        ret     16
_Add@16 endp

; Stores the N digit difference of B minus C in A and returns the borrow out
        public  _Sub@16
_Sub@16 proc
        mov     ecx, [esp+16]
        xor     eax, eax
        test    ecx, ecx
        jbe     done_sub
        push    edi
        mov     edi, [esp+12]
        push    esi
        mov     esi, [esp+20]
        push    ebx
        push    ebp
        mov     ebx, [esp+20]
        sub     edi, esi
        sub     ebx, esi
sub_loop:
        mov     edx, [edi+esi]
        mov     ebp, [esi]
        sub     edx, eax
        mov     eax, 0
        sbb     eax, eax
        sub     edx, ebp
        sbb     eax, 0
        mov     [ebx+esi], edx
        and     eax, 1
        add     esi, 4
        dec     ecx
        jne     sub_loop
        pop     ebp
        pop     ebx
        pop     esi
        pop     edi
done_sub:
        ret     16
_Sub@16 endp

; Returns the two digit r1 r2 divided by m1, saturating to all ones when the quotient overflows
        public  _BenalohEstimateQuotient@12
_BenalohEstimateQuotient@12 proc
        mov     edx, [esp+4]
        mov     ecx, [esp+12]
        mov     eax, [esp+8]
        cmp     edx, ecx
        jae     allones
        div     ecx
        ret     12
allones:
        mov     eax, 0FFFFFFFFh
        ret     12
_BenalohEstimateQuotient@12 endp

_TEXT   ENDS
        end
