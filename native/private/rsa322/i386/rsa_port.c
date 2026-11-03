#include "rsa_math.h"

// Stores the product of the N digit C and the single digit B in A and returns the carry digit
DWORD RSA32API BaseMult(LPDWORD A, DWORD B, LPDWORD C, DWORD N)
{
    DWORD OUT_HI = 0;

    if (B == 1) {
        memcpy(A, C, N * sizeof(DWORD));
    } else {
        __asm {
            mov esi, C
            mov ebx, A
            mov ecx, N
            shl ecx, 2
            mov N, ecx
            xor ecx, ecx
        mulloop:
            mov eax, B
            mov edx, [esi + ecx]
            mul edx
            add eax, OUT_HI
            adc edx, 0
            mov OUT_HI, edx
            mov [ebx + ecx], eax
            add ecx, 4
            cmp ecx, N
            jne mulloop
        }
    }

    return OUT_HI;
}

// Adds the product of the N digit C and the single digit B into A and returns the carry digit
DWORD RSA32API Accumulate(LPDWORD A, DWORD B, LPDWORD C, DWORD N)
{
    if (B == 1) {
        return Add(A, A, C, N);
    }

    __asm {
        mov ebx, B
        xor ecx, ecx
        test ebx, ebx
        je mulend
        push ebp
        mov eax, N
        mov esi, C
        xor ebp, ebp
        mov edi, [esp + 18h]
        sub ebp, eax
        lea esi, [esi + eax * 4]
        je mulpopend
        and ebp, 0FFFFFFFCh
        lea edi, [edi + eax * 4]
        and eax, 3
        je mul0
        dec eax
        je mul3
        dec eax
        je mul2
        jmp mul1
    mul0:
        mov eax, [esi + ebp * 4]
        mul ebx
        add ecx, eax
        mov eax, [edi + ebp * 4]
        adc edx, 0
        add eax, ecx
        adc edx, 0
        mov [edi + ebp * 4], eax
        mov ecx, edx
    mul1:
        mov eax, [esi + ebp * 4 + 4]
        mul ebx
        add ecx, eax
        mov eax, [edi + ebp * 4 + 4]
        adc edx, 0
        add eax, ecx
        adc edx, 0
        mov [edi + ebp * 4 + 4], eax
        mov ecx, edx
    mul2:
        mov eax, [esi + ebp * 4 + 8]
        mul ebx
        add ecx, eax
        mov eax, [edi + ebp * 4 + 8]
        adc edx, 0
        add eax, ecx
        adc edx, 0
        mov [edi + ebp * 4 + 8], eax
        mov ecx, edx
    mul3:
        mov eax, [esi + ebp * 4 + 12]
        mul ebx
        add ecx, eax
        mov eax, [edi + ebp * 4 + 12]
        adc edx, 0
        add eax, ecx
        adc edx, 0
        mov [edi + ebp * 4 + 12], eax
        mov ecx, edx
        add ebp, 4
        jne mul0
    mulpopend:
        pop ebp
    mulend:
        mov eax, ecx
    }
}

// Subtracts the product of the N digit C and the single digit B from A and returns the borrow digit
DWORD RSA32API Reduce(LPDWORD A, DWORD B, LPDWORD C, DWORD N)
{
    if (B == 1) {
        return Sub(A, A, C, N);
    }

    __asm {
        mov ebx, B
        xor ecx, ecx
        test ebx, ebx
        je mulend
        push ebp
        mov eax, N
        mov esi, C
        xor ebp, ebp
        mov edi, [esp + 18h]
        sub ebp, eax
        lea esi, [esi + eax * 4]
        je mulpopend
        and ebp, 0FFFFFFFCh
        lea edi, [edi + eax * 4]
        and eax, 3
        je mul0
        dec eax
        je mul3
        dec eax
        je mul2
        jmp mul1
    mul0:
        mov eax, [esi + ebp * 4]
        mul ebx
        add ecx, eax
        mov eax, [edi + ebp * 4]
        adc edx, 0
        sub eax, ecx
        adc edx, 0
        mov [edi + ebp * 4], eax
        mov ecx, edx
    mul1:
        mov eax, [esi + ebp * 4 + 4]
        mul ebx
        add ecx, eax
        mov eax, [edi + ebp * 4 + 4]
        adc edx, 0
        sub eax, ecx
        adc edx, 0
        mov [edi + ebp * 4 + 4], eax
        mov ecx, edx
    mul2:
        mov eax, [esi + ebp * 4 + 8]
        mul ebx
        add ecx, eax
        mov eax, [edi + ebp * 4 + 8]
        adc edx, 0
        sub eax, ecx
        adc edx, 0
        mov [edi + ebp * 4 + 8], eax
        mov ecx, edx
    mul3:
        mov eax, [esi + ebp * 4 + 12]
        mul ebx
        add ecx, eax
        mov eax, [edi + ebp * 4 + 12]
        adc edx, 0
        sub eax, ecx
        adc edx, 0
        mov [edi + ebp * 4 + 12], eax
        mov ecx, edx
        add ebp, 4
        jne mul0
    mulpopend:
        pop ebp
    mulend:
        mov eax, ecx
    }
}

// Adds the square of every digit of B into the matching double digit position of A
void RSA32API AccumulateSquares(LPDWORD A, LPDWORD B, DWORD blen)
{
    __asm {
        mov esi, B
        mov edi, A
        mov ecx, blen
        xor ebx, ebx
    sqloop:
        mov eax, [esi]
        add esi, 4
        mul eax
        add ebx, eax
        mov eax, [edi]
        adc edx, 0
        add eax, ebx
        mov ebx, [edi + 4]
        adc edx, 0
        mov [edi], eax
        add edx, ebx
        mov ebx, 0
        mov [edi + 4], edx
        adc ebx, 0
        add edi, 8
        dec ecx
        jne sqloop
    }
}
