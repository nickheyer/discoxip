        title  "RC4 Stream Cipher"
        .386p

_TEXT   SEGMENT PARA USE32 PUBLIC 'CODE'
        ASSUME  DS:FLAT, ES:FLAT, SS:NOTHING, FS:NOTHING, GS:NOTHING

; Encrypts the buffer in place with the key stream and saves the updated i and j indices
        public  _rc4@12
_rc4@12 proc
        push    ebx
        xor     edx, edx
        xor     ecx, ecx
        push    esi
        mov     eax, [esp+14h]
        mov     esi, [esp+0Ch]
        push    edi
        mov     edi, eax
        mov     ebx, [esp+14h]
        push    ebp
        mov     cl, [esi+100h]
        mov     ebp, ebx

        mov     dl, [esi+101h]
        test    ebx, ebx
        je      rc4_done

        shr     ebx, 2
        and     eax, 3

        test    ebx, ebx
        je      process_singles
        test    eax, eax
        jne     process_singles
        mov     eax, edi
        lea     edi, [edi+ebx*4]
        mov     [esp+1Ch], edi

        sub     eax, 4
        mov     ebp, ebx

rc4_loop_4:
        mov     edi, [eax+4]
        inc     ecx
        xor     eax, eax
        and     ecx, 0FFh
        mov     al, [ecx+esi]
        add     edx, eax
        and     edx, 0FFh
        mov     bl, [edx+esi]
        mov     [ecx+esi], bl
        inc     ecx
        mov     [edx+esi], al

        add     al, bl
        mov     al, [eax+esi]

        xor     edi, eax
        and     ecx, 0FFh

        xor     eax, eax
        mov     al, [ecx+esi]
        add     edx, eax
        and     edx, 0FFh
        mov     bl, [edx+esi]
        mov     [ecx+esi], bl
        inc     ecx
        mov     [edx+esi], al

        add     al, bl
        mov     al, [eax+esi]
        shl     eax, 8
        xor     edi, eax

        and     ecx, 0FFh
        xor     eax, eax
        mov     al, [ecx+esi]
        add     edx, eax
        and     edx, 0FFh
        mov     bl, [edx+esi]
        mov     [ecx+esi], bl
        inc     ecx
        mov     [edx+esi], al

        add     al, bl
        mov     al, [eax+esi]
        shl     eax, 10h
        xor     edi, eax

        and     ecx, 0FFh
        xor     eax, eax
        mov     al, [ecx+esi]
        add     edx, eax
        and     edx, 0FFh
        mov     bl, [edx+esi]
        mov     [ecx+esi], bl
        mov     [edx+esi], al

        add     al, bl
        mov     al, [eax+esi]
        shl     eax, 18h
        lea     ebx, [ebp*4]
        xor     edi, eax
        mov     eax, [esp+1Ch]
        sub     eax, ebx
        mov     [eax], edi
        dec     ebp
        jne     rc4_loop_4

        mov     ebp, [esp+18h]
        and     ebp, 3
        test    ebp, ebp
        je      rc4_done
        lea     edi, [eax+4]

process_singles:
rc4_loop:
        inc     ecx
        mov     ebx, 0FFh
        and     ecx, ebx
        xor     eax, eax
        mov     al, [ecx+esi]
        add     edx, eax
        and     edx, ebx
        xor     ebx, ebx
        mov     bl, [edx+esi]
        mov     [ecx+esi], bl
        mov     [edx+esi], al

        add     al, bl
        mov     bl, [edi]
        mov     al, [eax+esi]
        xor     bl, al
        mov     [edi], bl
        inc     edi
        dec     ebp
        jne     rc4_loop

rc4_done:
        pop     ebp
        pop     edi
        lea     eax, [esi+100h]
        pop     esi
        mov     [eax], cl
        pop     ebx
        mov     [eax+1], dl
        ret     0Ch
_rc4@12 endp

; Initializes the state table to the identity and permutes it with the repeated key bytes
        public  _rc4_key@12
_rc4_key@12 proc
        push    ebx
        mov     eax, 03020100h
        mov     ecx, 10h
        push    esi
        mov     esi, [esp+0Ch]
        push    edi
        mov     edi, 08080808h
        mov     edx, esi
        push    ebp

        mov     ebp, 07060504h

init_loop:
        mov     [edx], eax
        add     edx, 8
        add     eax, edi
        mov     [edx-4], ebp
        add     ebp, edi
        mov     [edx], eax
        add     edx, 8
        add     eax, edi
        mov     [edx-4], ebp
        add     ebp, edi
        dec     ecx
        jne     init_loop

        mov     ebp, [esp+1Ch]
        xor     eax, eax
        mov     [edx], cl
        mov     al, [esp+18h]
        lea     edi, [ebp+eax]
        mov     [edx+1], cl
        xor     ebx, ebx
        xor     edx, edx
        xor     ecx, ecx
        and     al, 3
        jne     not4key

permute_loop:
        mov     dl, [esi+ecx]
        mov     al, [ebp]
        add     bl, al
        add     bl, dl
        mov     al, [esi+ebx]
        mov     [esi+ecx], al
        mov     [esi+ebx], dl

        mov     al, [ebp+1]
        mov     dl, [esi+ecx+1]

        add     bl, al
        add     bl, dl

        mov     al, [esi+ebx]
        mov     [esi+ecx+1], al

        mov     [esi+ebx], dl

        mov     al, [ebp+2]
        mov     dl, [esi+ecx+2]

        add     bl, al
        add     bl, dl

        mov     al, [esi+ebx]
        mov     [esi+ecx+2], al

        mov     [esi+ebx], dl

        mov     al, [ebp+3]
        mov     dl, [esi+ecx+3]
        add     bl, al
        add     bl, dl
        mov     al, [esi+ebx]
        mov     [esi+ecx+3], al
        add     ebp, 4
        mov     [esi+ebx], dl
        cmp     ebp, edi
        jne     skip_zero_k
        mov     ebp, [esp+1Ch]

skip_zero_k:
        add     cl, 4
        jne     permute_loop
        pop     ebp
        pop     edi
        pop     esi
        pop     ebx
        ret     0Ch

        nop
        nop
        nop
        nop
        nop

not4key:
permute_loop_not4key:
        mov     dl, [esi+ecx]
        mov     al, [ebp]
        add     bl, al
        inc     ebp
        add     bl, dl

        mov     al, [esi+ebx]
        mov     [esi+ecx], al
        mov     [esi+ebx], dl
        cmp     ebp, edi
        jne     skip_zero_k_not4
        mov     ebp, [esp+1Ch]

skip_zero_k_not4:
        inc     ecx
        cmp     ecx, 100h
        jne     permute_loop_not4key
        pop     ebp
        pop     edi
        pop     esi
        pop     ebx
        ret     0Ch
_rc4_key@12 endp

_TEXT   ENDS
        end
