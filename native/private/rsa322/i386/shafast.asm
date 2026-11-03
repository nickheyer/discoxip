        title  "SHA-1 Compression Function"
        .486p

_TEXT   SEGMENT PARA USE32 PUBLIC 'CODE'
        ASSUME  DS:FLAT, ES:FLAT, SS:NOTHING, FS:NOTHING, GS:NOTHING

; Byte swaps the block, expands it to eighty words and folds all eighty rounds into the state
        public  _SHATransform@8
_SHATransform@8 proc
        push    esi
        push    edi
        push    ebx
        push    ebp
        mov     edx, [esp+18h]
        sub     esp, 140h

        mov     esi, 10h

        lea     ecx, [esp-4]

invert_endian_input:
        mov     eax, [edx]
        add     ecx, 4
        add     edx, 4
        bswap   eax
        mov     [ecx], eax
        dec     esi
        jne     invert_endian_input
        mov     ebp, [esp+154h]

        lea     ecx, [esp+8]
        mov     edx, 20h

        xor     eax, eax
        xor     ebx, ebx

expand_loop:
        mov     eax, [ecx-8]
        mov     ebx, [ecx-4]
        mov     esi, [ecx]
        mov     edi, [ecx+4]
        xor     eax, esi
        xor     ebx, edi
        mov     esi, [ecx+18h]
        mov     edi, [ecx+1Ch]
        xor     eax, esi
        xor     ebx, edi
        mov     esi, [ecx+2Ch]
        mov     edi, [ecx+30h]
        xor     eax, esi
        add     ecx, 8
        rol     eax, 1
        xor     ebx, edi
        rol     ebx, 1
        mov     [ecx+30h], eax
        dec     edx
        mov     [ecx+34h], ebx
        jne     expand_loop

        mov     eax, [ebp]
        mov     ebx, [ebp+4]
        mov     ecx, [ebp+8]
        mov     edx, [ebp+0Ch]
        mov     edi, [ebp+10h]
        mov     ebp, ecx

        xor     ebp, edx
        and     ebp, ebx
        mov     esi, eax
        rol     esi, 5
        add     edi, esi
        mov     esi, [esp]
        ror     ebx, 1
        xor     ebp, edx
        ror     ebx, 1
        lea     edi, [edi+esi+5A827999h]
        add     edi, ebp

        mov     esi, ebx
        xor     esi, ecx
        mov     ebp, edi
        rol     ebp, 5
        and     esi, eax
        add     edx, ebp
        ror     eax, 1
        mov     ebp, [esp+4]
        ror     eax, 1
        xor     esi, ecx
        lea     edx, [edx+ebp+5A827999h]
        mov     ebp, eax
        add     edx, esi

        xor     ebp, ebx
        and     ebp, edi
        mov     esi, edx
        rol     esi, 5
        add     ecx, esi
        mov     esi, [esp+8]
        ror     edi, 1
        xor     ebp, ebx
        ror     edi, 1
        lea     ecx, [ecx+esi+5A827999h]
        add     ecx, ebp

        mov     esi, edi
        xor     esi, eax
        mov     ebp, ecx
        rol     ebp, 5
        and     esi, edx
        add     ebx, ebp
        ror     edx, 1
        mov     ebp, [esp+0Ch]
        ror     edx, 1
        xor     esi, eax
        lea     ebx, [ebx+ebp+5A827999h]
        mov     ebp, edx
        add     ebx, esi

        xor     ebp, edi
        and     ebp, ecx
        mov     esi, ebx
        rol     esi, 5
        add     eax, esi
        mov     esi, [esp+10h]
        ror     ecx, 1
        xor     ebp, edi
        ror     ecx, 1
        lea     eax, [eax+esi+5A827999h]
        add     eax, ebp

        mov     esi, ecx
        xor     esi, edx
        mov     ebp, eax
        rol     ebp, 5
        and     esi, ebx
        add     edi, ebp
        ror     ebx, 1
        mov     ebp, [esp+14h]
        ror     ebx, 1
        xor     esi, edx
        lea     edi, [edi+ebp+5A827999h]
        mov     ebp, ebx
        add     edi, esi

        xor     ebp, ecx
        and     ebp, eax
        mov     esi, edi
        rol     esi, 5
        add     edx, esi
        mov     esi, [esp+18h]
        ror     eax, 1
        xor     ebp, ecx
        ror     eax, 1
        lea     edx, [edx+esi+5A827999h]
        add     edx, ebp

        mov     esi, eax
        xor     esi, ebx
        mov     ebp, edx
        rol     ebp, 5
        and     esi, edi
        add     ecx, ebp
        ror     edi, 1
        mov     ebp, [esp+1Ch]
        ror     edi, 1
        xor     esi, ebx
        lea     ecx, [ecx+ebp+5A827999h]
        mov     ebp, edi
        add     ecx, esi

        xor     ebp, eax
        and     ebp, edx
        mov     esi, ecx
        rol     esi, 5
        add     ebx, esi
        mov     esi, [esp+20h]
        ror     edx, 1
        xor     ebp, eax
        ror     edx, 1
        lea     ebx, [ebx+esi+5A827999h]
        add     ebx, ebp

        mov     esi, edx
        xor     esi, edi
        mov     ebp, ebx
        rol     ebp, 5
        and     esi, ecx
        add     eax, ebp
        ror     ecx, 1
        mov     ebp, [esp+24h]
        ror     ecx, 1
        xor     esi, edi
        lea     eax, [eax+ebp+5A827999h]
        mov     ebp, ecx
        add     eax, esi

        xor     ebp, edx
        and     ebp, ebx
        mov     esi, eax
        rol     esi, 5
        add     edi, esi
        mov     esi, [esp+28h]
        ror     ebx, 1
        xor     ebp, edx
        ror     ebx, 1
        lea     edi, [edi+esi+5A827999h]
        add     edi, ebp

        mov     esi, ebx
        xor     esi, ecx
        mov     ebp, edi
        rol     ebp, 5
        and     esi, eax
        add     edx, ebp
        ror     eax, 1
        mov     ebp, [esp+2Ch]
        ror     eax, 1
        xor     esi, ecx
        lea     edx, [edx+ebp+5A827999h]
        mov     ebp, eax
        add     edx, esi

        xor     ebp, ebx
        and     ebp, edi
        mov     esi, edx
        rol     esi, 5
        add     ecx, esi
        mov     esi, [esp+30h]
        ror     edi, 1
        xor     ebp, ebx
        ror     edi, 1
        lea     ecx, [ecx+esi+5A827999h]
        add     ecx, ebp

        mov     esi, edi
        xor     esi, eax
        mov     ebp, ecx
        rol     ebp, 5
        and     esi, edx
        add     ebx, ebp
        ror     edx, 1
        mov     ebp, [esp+34h]
        ror     edx, 1
        xor     esi, eax
        lea     ebx, [ebx+ebp+5A827999h]
        mov     ebp, edx
        add     ebx, esi

        xor     ebp, edi
        and     ebp, ecx
        mov     esi, ebx
        rol     esi, 5
        add     eax, esi
        mov     esi, [esp+38h]
        ror     ecx, 1
        xor     ebp, edi
        ror     ecx, 1
        lea     eax, [eax+esi+5A827999h]
        add     eax, ebp

        mov     esi, ecx
        xor     esi, edx
        mov     ebp, eax
        rol     ebp, 5
        and     esi, ebx
        add     edi, ebp
        ror     ebx, 1
        mov     ebp, [esp+3Ch]
        ror     ebx, 1
        xor     esi, edx
        lea     edi, [edi+ebp+5A827999h]
        mov     ebp, ebx
        add     edi, esi

        xor     ebp, ecx
        and     ebp, eax
        mov     esi, edi
        rol     esi, 5
        add     edx, esi
        mov     esi, [esp+40h]
        ror     eax, 1
        xor     ebp, ecx
        ror     eax, 1
        lea     edx, [edx+esi+5A827999h]
        add     edx, ebp

        mov     esi, eax
        xor     esi, ebx
        mov     ebp, edx
        rol     ebp, 5
        and     esi, edi
        add     ecx, ebp
        ror     edi, 1
        mov     ebp, [esp+44h]
        ror     edi, 1
        xor     esi, ebx
        lea     ecx, [ecx+ebp+5A827999h]
        mov     ebp, edi
        add     ecx, esi

        xor     ebp, eax
        and     ebp, edx
        mov     esi, ecx
        rol     esi, 5
        add     ebx, esi
        mov     esi, [esp+48h]
        ror     edx, 1
        xor     ebp, eax
        ror     edx, 1
        lea     ebx, [ebx+esi+5A827999h]
        add     ebx, ebp

        mov     esi, edx
        xor     esi, edi
        mov     ebp, ebx
        rol     ebp, 5
        and     esi, ecx
        add     eax, ebp
        ror     ecx, 1
        mov     ebp, [esp+4Ch]
        ror     ecx, 1
        xor     esi, edi
        lea     eax, [eax+ebp+5A827999h]
        mov     ebp, ecx
        add     eax, esi

        xor     ebp, ebx
        xor     ebp, edx
        mov     esi, eax
        rol     esi, 5
        ror     ebx, 1
        add     edi, esi
        ror     ebx, 1
        mov     esi, [esp+50h]
        lea     edi, [edi+ebp+6ED9EBA1h]
        mov     ebp, ebx
        add     edi, esi

        xor     ebp, eax
        xor     ebp, ecx
        mov     esi, edi
        rol     esi, 5
        ror     eax, 1
        add     edx, esi
        ror     eax, 1
        mov     esi, [esp+54h]
        lea     edx, [edx+ebp+6ED9EBA1h]
        mov     ebp, eax
        add     edx, esi

        xor     ebp, edi
        xor     ebp, ebx
        mov     esi, edx
        rol     esi, 5
        ror     edi, 1
        add     ecx, esi
        ror     edi, 1
        mov     esi, [esp+58h]
        lea     ecx, [ecx+ebp+6ED9EBA1h]
        mov     ebp, edi
        add     ecx, esi

        xor     ebp, edx
        xor     ebp, eax
        mov     esi, ecx
        rol     esi, 5
        ror     edx, 1
        add     ebx, esi
        ror     edx, 1
        mov     esi, [esp+5Ch]
        lea     ebx, [ebx+ebp+6ED9EBA1h]
        mov     ebp, edx
        add     ebx, esi

        xor     ebp, ecx
        xor     ebp, edi
        mov     esi, ebx
        rol     esi, 5
        ror     ecx, 1
        add     eax, esi
        ror     ecx, 1
        mov     esi, [esp+60h]
        lea     eax, [eax+ebp+6ED9EBA1h]
        mov     ebp, ecx
        add     eax, esi

        xor     ebp, ebx
        xor     ebp, edx
        mov     esi, eax
        rol     esi, 5
        ror     ebx, 1
        add     edi, esi
        ror     ebx, 1
        mov     esi, [esp+64h]
        lea     edi, [edi+ebp+6ED9EBA1h]
        mov     ebp, ebx
        add     edi, esi

        xor     ebp, eax
        xor     ebp, ecx
        mov     esi, edi
        rol     esi, 5
        ror     eax, 1
        add     edx, esi
        ror     eax, 1
        mov     esi, [esp+68h]
        lea     edx, [edx+ebp+6ED9EBA1h]
        mov     ebp, eax
        add     edx, esi

        xor     ebp, edi
        xor     ebp, ebx
        mov     esi, edx
        rol     esi, 5
        ror     edi, 1
        add     ecx, esi
        ror     edi, 1
        mov     esi, [esp+6Ch]
        lea     ecx, [ecx+ebp+6ED9EBA1h]
        mov     ebp, edi
        add     ecx, esi

        xor     ebp, edx
        xor     ebp, eax
        mov     esi, ecx
        rol     esi, 5
        ror     edx, 1
        add     ebx, esi
        ror     edx, 1
        mov     esi, [esp+70h]
        lea     ebx, [ebx+ebp+6ED9EBA1h]
        mov     ebp, edx
        add     ebx, esi

        xor     ebp, ecx
        xor     ebp, edi
        mov     esi, ebx
        rol     esi, 5
        ror     ecx, 1
        add     eax, esi
        ror     ecx, 1
        mov     esi, [esp+74h]
        lea     eax, [eax+ebp+6ED9EBA1h]
        mov     ebp, ecx
        add     eax, esi

        xor     ebp, ebx
        xor     ebp, edx
        mov     esi, eax
        rol     esi, 5
        ror     ebx, 1
        add     edi, esi
        ror     ebx, 1
        mov     esi, [esp+78h]
        lea     edi, [edi+ebp+6ED9EBA1h]
        mov     ebp, ebx
        add     edi, esi

        xor     ebp, eax
        xor     ebp, ecx
        mov     esi, edi
        rol     esi, 5
        ror     eax, 1
        add     edx, esi
        ror     eax, 1
        mov     esi, [esp+7Ch]
        lea     edx, [edx+ebp+6ED9EBA1h]
        mov     ebp, eax
        add     edx, esi

        xor     ebp, edi
        xor     ebp, ebx
        mov     esi, edx
        rol     esi, 5
        ror     edi, 1
        add     ecx, esi
        ror     edi, 1
        mov     esi, [esp+80h]
        lea     ecx, [ecx+ebp+6ED9EBA1h]
        mov     ebp, edi
        add     ecx, esi

        xor     ebp, edx
        xor     ebp, eax
        mov     esi, ecx
        rol     esi, 5
        ror     edx, 1
        add     ebx, esi
        ror     edx, 1
        mov     esi, [esp+84h]
        lea     ebx, [ebx+ebp+6ED9EBA1h]
        mov     ebp, edx
        add     ebx, esi

        xor     ebp, ecx
        xor     ebp, edi
        mov     esi, ebx
        rol     esi, 5
        ror     ecx, 1
        add     eax, esi
        ror     ecx, 1
        mov     esi, [esp+88h]
        lea     eax, [eax+ebp+6ED9EBA1h]
        mov     ebp, ecx
        add     eax, esi

        xor     ebp, ebx
        xor     ebp, edx
        mov     esi, eax
        rol     esi, 5
        ror     ebx, 1
        add     edi, esi
        ror     ebx, 1
        mov     esi, [esp+8Ch]
        lea     edi, [edi+ebp+6ED9EBA1h]
        mov     ebp, ebx
        add     edi, esi

        xor     ebp, eax
        xor     ebp, ecx
        mov     esi, edi
        rol     esi, 5
        ror     eax, 1
        add     edx, esi
        ror     eax, 1
        mov     esi, [esp+90h]
        lea     edx, [edx+ebp+6ED9EBA1h]
        mov     ebp, eax
        add     edx, esi

        xor     ebp, edi
        xor     ebp, ebx
        mov     esi, edx
        rol     esi, 5
        ror     edi, 1
        add     ecx, esi
        ror     edi, 1
        mov     esi, [esp+94h]
        lea     ecx, [ecx+ebp+6ED9EBA1h]
        mov     ebp, edi
        add     ecx, esi

        xor     ebp, edx
        xor     ebp, eax
        mov     esi, ecx
        rol     esi, 5
        ror     edx, 1
        add     ebx, esi
        ror     edx, 1
        mov     esi, [esp+98h]
        lea     ebx, [ebx+ebp+6ED9EBA1h]
        mov     ebp, edx
        add     ebx, esi

        xor     ebp, ecx
        xor     ebp, edi
        mov     esi, ebx
        rol     esi, 5
        ror     ecx, 1
        add     eax, esi
        ror     ecx, 1
        mov     esi, [esp+9Ch]
        lea     eax, [eax+ebp+6ED9EBA1h]
        mov     ebp, ecx
        add     eax, esi

        mov     ebp, ebx
        mov     esi, eax
        or      ebp, ecx
        rol     esi, 5
        and     ebp, edx
        add     edi, esi
        mov     esi, [esp+0A0h]
        add     edi, esi
        mov     esi, ebx
        and     esi, ecx
        add     edi, 8F1BBCDCh
        ror     ebx, 1
        or      esi, ebp
        ror     ebx, 1
        add     edi, esi

        mov     ebp, eax
        mov     esi, edi
        rol     esi, 5
        or      ebp, ebx
        add     edx, esi
        and     ebp, ecx
        mov     esi, [esp+0A4h]
        add     edx, esi
        mov     esi, eax
        and     esi, ebx
        add     edx, 8F1BBCDCh
        ror     eax, 1
        or      esi, ebp
        ror     eax, 1
        add     edx, esi

        mov     ebp, edi
        mov     esi, edx
        rol     esi, 5
        or      ebp, eax
        add     ecx, esi
        and     ebp, ebx
        mov     esi, [esp+0A8h]
        add     ecx, esi
        mov     esi, edi
        and     esi, eax
        add     ecx, 8F1BBCDCh
        ror     edi, 1
        or      esi, ebp
        ror     edi, 1
        add     ecx, esi

        mov     ebp, edx
        mov     esi, ecx
        rol     esi, 5
        or      ebp, edi
        add     ebx, esi
        and     ebp, eax
        mov     esi, [esp+0ACh]
        add     ebx, esi
        mov     esi, edx
        and     esi, edi
        add     ebx, 8F1BBCDCh
        ror     edx, 1
        or      esi, ebp
        ror     edx, 1
        add     ebx, esi

        mov     ebp, ecx
        mov     esi, ebx
        rol     esi, 5
        or      ebp, edx
        add     eax, esi
        and     ebp, edi
        mov     esi, [esp+0B0h]
        add     eax, esi
        mov     esi, ecx
        and     esi, edx
        add     eax, 8F1BBCDCh
        ror     ecx, 1
        or      esi, ebp
        ror     ecx, 1
        add     eax, esi

        mov     ebp, ebx
        mov     esi, eax
        rol     esi, 5
        or      ebp, ecx
        add     edi, esi
        and     ebp, edx
        mov     esi, [esp+0B4h]
        add     edi, esi
        mov     esi, ebx
        and     esi, ecx
        add     edi, 8F1BBCDCh
        ror     ebx, 1
        or      esi, ebp
        ror     ebx, 1
        add     edi, esi

        mov     ebp, eax
        mov     esi, edi
        rol     esi, 5
        or      ebp, ebx
        add     edx, esi
        and     ebp, ecx
        mov     esi, [esp+0B8h]
        add     edx, esi
        mov     esi, eax
        and     esi, ebx
        add     edx, 8F1BBCDCh
        ror     eax, 1
        or      esi, ebp
        ror     eax, 1
        add     edx, esi

        mov     ebp, edi
        mov     esi, edx
        rol     esi, 5
        or      ebp, eax
        add     ecx, esi
        and     ebp, ebx
        mov     esi, [esp+0BCh]
        add     ecx, esi
        mov     esi, edi
        and     esi, eax
        add     ecx, 8F1BBCDCh
        ror     edi, 1
        or      esi, ebp
        ror     edi, 1
        add     ecx, esi

        mov     ebp, edx
        mov     esi, ecx
        rol     esi, 5
        or      ebp, edi
        add     ebx, esi
        and     ebp, eax
        mov     esi, [esp+0C0h]
        add     ebx, esi
        mov     esi, edx
        and     esi, edi
        add     ebx, 8F1BBCDCh
        ror     edx, 1
        or      esi, ebp
        ror     edx, 1
        add     ebx, esi

        mov     ebp, ecx
        mov     esi, ebx
        rol     esi, 5
        or      ebp, edx
        add     eax, esi
        and     ebp, edi
        mov     esi, [esp+0C4h]
        add     eax, esi
        mov     esi, ecx
        and     esi, edx
        add     eax, 8F1BBCDCh
        ror     ecx, 1
        or      esi, ebp
        ror     ecx, 1
        add     eax, esi

        mov     ebp, ebx
        mov     esi, eax
        rol     esi, 5
        or      ebp, ecx
        add     edi, esi
        and     ebp, edx
        mov     esi, [esp+0C8h]
        add     edi, esi
        mov     esi, ebx
        and     esi, ecx
        add     edi, 8F1BBCDCh
        ror     ebx, 1
        or      esi, ebp
        ror     ebx, 1
        add     edi, esi

        mov     ebp, eax
        mov     esi, edi
        rol     esi, 5
        or      ebp, ebx
        add     edx, esi
        and     ebp, ecx
        mov     esi, [esp+0CCh]
        add     edx, esi
        mov     esi, eax
        and     esi, ebx
        add     edx, 8F1BBCDCh
        ror     eax, 1
        or      esi, ebp
        ror     eax, 1
        add     edx, esi

        mov     ebp, edi
        mov     esi, edx
        rol     esi, 5
        or      ebp, eax
        add     ecx, esi
        and     ebp, ebx
        mov     esi, [esp+0D0h]
        add     ecx, esi
        mov     esi, edi
        and     esi, eax
        add     ecx, 8F1BBCDCh
        ror     edi, 1
        or      esi, ebp
        ror     edi, 1
        add     ecx, esi

        mov     ebp, edx
        mov     esi, ecx
        rol     esi, 5
        or      ebp, edi
        add     ebx, esi
        and     ebp, eax
        mov     esi, [esp+0D4h]
        add     ebx, esi
        mov     esi, edx
        and     esi, edi
        add     ebx, 8F1BBCDCh
        ror     edx, 1
        or      esi, ebp
        ror     edx, 1
        add     ebx, esi

        mov     ebp, ecx
        mov     esi, ebx
        rol     esi, 5
        or      ebp, edx
        add     eax, esi
        and     ebp, edi
        mov     esi, [esp+0D8h]
        add     eax, esi
        mov     esi, ecx
        and     esi, edx
        add     eax, 8F1BBCDCh
        ror     ecx, 1
        or      esi, ebp
        ror     ecx, 1
        add     eax, esi

        mov     ebp, ebx
        mov     esi, eax
        rol     esi, 5
        or      ebp, ecx
        add     edi, esi
        and     ebp, edx
        mov     esi, [esp+0DCh]
        add     edi, esi
        mov     esi, ebx
        and     esi, ecx
        add     edi, 8F1BBCDCh
        ror     ebx, 1
        or      esi, ebp
        ror     ebx, 1
        add     edi, esi

        mov     ebp, eax
        mov     esi, edi
        rol     esi, 5
        or      ebp, ebx
        add     edx, esi
        and     ebp, ecx
        mov     esi, [esp+0E0h]
        add     edx, esi
        mov     esi, eax
        and     esi, ebx
        add     edx, 8F1BBCDCh
        ror     eax, 1
        or      esi, ebp
        ror     eax, 1
        add     edx, esi

        mov     ebp, edi
        mov     esi, edx
        rol     esi, 5
        or      ebp, eax
        add     ecx, esi
        and     ebp, ebx
        mov     esi, [esp+0E4h]
        add     ecx, esi
        mov     esi, edi
        and     esi, eax
        add     ecx, 8F1BBCDCh
        ror     edi, 1
        or      esi, ebp
        ror     edi, 1
        add     ecx, esi

        mov     ebp, edx
        mov     esi, ecx
        rol     esi, 5
        or      ebp, edi
        add     ebx, esi
        and     ebp, eax
        mov     esi, [esp+0E8h]
        add     ebx, esi
        mov     esi, edx
        and     esi, edi
        add     ebx, 8F1BBCDCh
        ror     edx, 1
        or      esi, ebp
        ror     edx, 1
        add     ebx, esi

        mov     ebp, ecx
        mov     esi, ebx
        rol     esi, 5
        or      ebp, edx
        add     eax, esi
        and     ebp, edi
        mov     esi, [esp+0ECh]
        add     eax, esi
        mov     esi, ecx
        and     esi, edx
        add     eax, 8F1BBCDCh
        ror     ecx, 1
        or      esi, ebp
        ror     ecx, 1
        add     eax, esi
        mov     ebp, ecx

        xor     ebp, ebx
        xor     ebp, edx
        mov     esi, eax
        rol     esi, 5
        ror     ebx, 1
        add     edi, esi
        ror     ebx, 1
        mov     esi, [esp+0F0h]
        lea     edi, [edi+ebp+0CA62C1D6h]
        mov     ebp, ebx
        add     edi, esi

        xor     ebp, eax
        xor     ebp, ecx
        mov     esi, edi
        rol     esi, 5
        ror     eax, 1
        add     edx, esi
        ror     eax, 1
        mov     esi, [esp+0F4h]
        lea     edx, [edx+ebp+0CA62C1D6h]
        mov     ebp, eax
        add     edx, esi

        xor     ebp, edi
        xor     ebp, ebx
        mov     esi, edx
        rol     esi, 5
        ror     edi, 1
        add     ecx, esi
        ror     edi, 1
        mov     esi, [esp+0F8h]
        lea     ecx, [ecx+ebp+0CA62C1D6h]
        mov     ebp, edi
        add     ecx, esi

        xor     ebp, edx
        xor     ebp, eax
        mov     esi, ecx
        rol     esi, 5
        ror     edx, 1
        add     ebx, esi
        ror     edx, 1
        mov     esi, [esp+0FCh]
        lea     ebx, [ebx+ebp+0CA62C1D6h]
        mov     ebp, edx
        add     ebx, esi

        xor     ebp, ecx
        xor     ebp, edi
        mov     esi, ebx
        rol     esi, 5
        ror     ecx, 1
        add     eax, esi
        ror     ecx, 1
        mov     esi, [esp+100h]
        lea     eax, [eax+ebp+0CA62C1D6h]
        mov     ebp, ecx
        add     eax, esi

        xor     ebp, ebx
        xor     ebp, edx
        mov     esi, eax
        rol     esi, 5
        ror     ebx, 1
        add     edi, esi
        ror     ebx, 1
        mov     esi, [esp+104h]
        lea     edi, [edi+ebp+0CA62C1D6h]
        mov     ebp, ebx
        add     edi, esi

        xor     ebp, eax
        xor     ebp, ecx
        mov     esi, edi
        rol     esi, 5
        ror     eax, 1
        add     edx, esi
        ror     eax, 1
        mov     esi, [esp+108h]
        lea     edx, [edx+ebp+0CA62C1D6h]
        mov     ebp, eax
        add     edx, esi

        xor     ebp, edi
        xor     ebp, ebx
        mov     esi, edx
        rol     esi, 5
        ror     edi, 1
        add     ecx, esi
        ror     edi, 1
        mov     esi, [esp+10Ch]
        lea     ecx, [ecx+ebp+0CA62C1D6h]
        mov     ebp, edi
        add     ecx, esi

        xor     ebp, edx
        xor     ebp, eax
        mov     esi, ecx
        rol     esi, 5
        ror     edx, 1
        add     ebx, esi
        ror     edx, 1
        mov     esi, [esp+110h]
        lea     ebx, [ebx+ebp+0CA62C1D6h]
        mov     ebp, edx
        add     ebx, esi

        xor     ebp, ecx
        xor     ebp, edi
        mov     esi, ebx
        rol     esi, 5
        ror     ecx, 1
        add     eax, esi
        ror     ecx, 1
        mov     esi, [esp+114h]
        lea     eax, [eax+ebp+0CA62C1D6h]
        mov     ebp, ecx
        add     eax, esi

        xor     ebp, ebx
        xor     ebp, edx
        mov     esi, eax
        rol     esi, 5
        ror     ebx, 1
        add     edi, esi
        ror     ebx, 1
        mov     esi, [esp+118h]
        lea     edi, [edi+ebp+0CA62C1D6h]
        mov     ebp, ebx
        add     edi, esi

        xor     ebp, eax
        xor     ebp, ecx
        mov     esi, edi
        rol     esi, 5
        ror     eax, 1
        add     edx, esi
        ror     eax, 1
        mov     esi, [esp+11Ch]
        lea     edx, [edx+ebp+0CA62C1D6h]
        mov     ebp, eax
        add     edx, esi

        xor     ebp, edi
        xor     ebp, ebx
        mov     esi, edx
        rol     esi, 5
        ror     edi, 1
        add     ecx, esi
        ror     edi, 1
        mov     esi, [esp+120h]
        lea     ecx, [ecx+ebp+0CA62C1D6h]
        mov     ebp, edi
        add     ecx, esi

        xor     ebp, edx
        xor     ebp, eax
        mov     esi, ecx
        rol     esi, 5
        ror     edx, 1
        add     ebx, esi
        ror     edx, 1
        mov     esi, [esp+124h]
        lea     ebx, [ebx+ebp+0CA62C1D6h]
        mov     ebp, edx
        add     ebx, esi

        xor     ebp, ecx
        xor     ebp, edi
        mov     esi, ebx
        rol     esi, 5
        ror     ecx, 1
        add     eax, esi
        ror     ecx, 1
        mov     esi, [esp+128h]
        lea     eax, [eax+ebp+0CA62C1D6h]
        mov     ebp, ecx
        add     eax, esi

        xor     ebp, ebx
        xor     ebp, edx
        mov     esi, eax
        rol     esi, 5
        ror     ebx, 1
        add     edi, esi
        ror     ebx, 1
        mov     esi, [esp+12Ch]
        lea     edi, [edi+ebp+0CA62C1D6h]
        mov     ebp, ebx
        add     edi, esi

        xor     ebp, eax
        xor     ebp, ecx
        mov     esi, edi
        rol     esi, 5
        ror     eax, 1
        add     edx, esi
        ror     eax, 1
        mov     esi, [esp+130h]
        lea     edx, [edx+ebp+0CA62C1D6h]
        mov     ebp, eax
        add     edx, esi

        xor     ebp, edi
        xor     ebp, ebx
        mov     esi, edx
        rol     esi, 5
        ror     edi, 1
        add     ecx, esi
        ror     edi, 1
        mov     esi, [esp+134h]
        lea     ecx, [ecx+ebp+0CA62C1D6h]
        mov     ebp, edi
        add     ecx, esi

        xor     ebp, edx
        xor     ebp, eax
        mov     esi, ecx
        rol     esi, 5
        ror     edx, 1
        add     ebx, esi
        ror     edx, 1
        mov     esi, [esp+138h]
        lea     ebx, [ebx+ebp+0CA62C1D6h]
        mov     ebp, edx
        add     ebx, esi

        xor     ebp, ecx
        xor     ebp, edi
        mov     esi, ebx
        rol     esi, 5
        ror     ecx, 1
        add     eax, esi
        ror     ecx, 1
        mov     esi, [esp+13Ch]
        add     eax, 0CA62C1D6h
        add     esi, ebp
        add     eax, esi
        mov     esi, [esp+154h]
        add     esp, 140h
        mov     ebp, [esi]
        add     eax, ebp
        mov     ebp, [esi+4]
        add     ebx, ebp
        mov     ebp, [esi+8]
        add     ecx, ebp
        mov     ebp, [esi+0Ch]
        add     edx, ebp
        mov     ebp, [esi+10h]
        add     edi, ebp
        mov     [esi], eax
        mov     [esi+4], ebx
        mov     [esi+8], ecx
        mov     [esi+0Ch], edx
        mov     [esi+10h], edi
        pop     ebp
        pop     ebx
        pop     edi
        pop     esi
        ret     8
_SHATransform@8 endp

_TEXT   ENDS
        end
