#include "rsa_math.h"

// Stores val in the low digit and sign extends it through the remaining digits
void RSA32API SetValDWORD(LPDWORD num, DWORD val, DWORD len)
{
    BYTE fill;

    num[0] = val;
    fill = (val & HIGH_BIT) ? 0xff : 0;
    memset(num + 1, fill, (len - 1) * sizeof(DWORD));
}

// Returns the count of significant digits, never less than one
DWORD RSA32API DigitLen(LPDWORD A, DWORD N)
{
    DWORD i;

    for (i = N; i > 0; i--) {
        if (A[i - 1] != 0) {
            return i;
        }
    }

    return 1;
}

// Returns the count of significant bits, zero for a zero value
DWORD RSA32API BitLen(LPDWORD A, DWORD N)
{
    int i;
    DWORD digit;
    DWORD length;

    i = N;
    while (i--) {
        if (A[i] != 0) {
            break;
        }
    }

    if (i == -1) {
        return 0;
    }

    digit = A[i];
    length = (i + 1) * DIGIT_BITS;
    while (!(digit & HIGH_BIT)) {
        digit <<= 1;
        length--;
    }

    return length;
}

// Returns 1, 0 or -1 as A is greater than, equal to or less than B
int RSA32API Compare(LPDWORD A, LPDWORD B, DWORD N)
{
    int i;

    for (i = N - 1; i >= 0; i--) {
        if (A[i] > B[i]) {
            return 1;
        }
        if (A[i] < B[i]) {
            return -1;
        }
    }

    return 0;
}

// Stores the double length product of B and C in A
void RSA32API Multiply(LPDWORD A, LPDWORD B, LPDWORD C, DWORD N)
{
    DWORD i;
    DWORD Clen;

    memset(A, 0, 2 * N * sizeof(DWORD));
    Clen = DigitLen(C, N);

    for (i = 0; i < N; i++) {
        A[Clen + i] = Accumulate(&A[i], B[i], C, Clen);
    }
}

// Stores the double length square of B in A
void RSA32API Square(LPDWORD A, LPDWORD B, DWORD N)
{
    DWORD i;
    DWORD Blen;
    LPDWORD pA;
    LPDWORD pB;

    memset(A, 0, 2 * N * sizeof(DWORD));
    Blen = DigitLen(B, N);

    pA = A + 1;
    pB = B;
    for (i = Blen - 1; i != 0; i--, pA += 2, pB++) {
        pA[i] = Accumulate(pA, pB[0], pB + 1, i);
    }

    Add(A, A, A, 2 * Blen);
    AccumulateSquares(A, B, Blen);
}

// Returns the next quotient digit of r1 r2 divided by m1 m2, saturating at all ones
DWORD RSA32API EstimateQuotient(DWORD r1, DWORD r2, DWORD m1, DWORD m2)
{
    ULARGE_INTEGER num;
    ULARGE_INTEGER den;
    DWORD q;
    DWORD bit;

    num.LowPart = r2;
    num.HighPart = r1;

    if (m2 != 0 && !(m1 & HIGH_BIT)) {
        den.LowPart = m2;
        den.HighPart = m1;
        if (num.QuadPart >= den.QuadPart) {
            return 0xffffffff;
        }

        q = 0;
        for (bit = HIGH_BIT; bit != 0; bit >>= 1) {
            num.QuadPart <<= 1;
            if (num.QuadPart >= den.QuadPart) {
                num.QuadPart -= den.QuadPart;
                q |= bit;
            }
        }
        return q;
    }

    if (r1 >= m1) {
        return 0xffffffff;
    }

    return (DWORD)(num.QuadPart / m1);
}

// Stores A modulo B in R, failing on a zero divisor or an allocation failure
BOOL RSA32API Mod(LPDWORD A, LPDWORD B, LPDWORD R, DWORD T, DWORD N)
{
    DWORD *pdwSlowBuffer = NULL;
    LPDWORD TempA;
    LPDWORD TempB;
    LPDWORD TempR;
    int i;
    DWORD dwTemp[16];
    DWORD Tdigits;
    DWORD Ndigits;
    DWORD q;
    DWORD cb;

    Tdigits = DigitLen(A, T);
    Ndigits = DigitLen(B, N);

    if (Ndigits <= 1 && B[0] == 0) {
        return FALSE;
    }

    if (Tdigits < Ndigits) {
        memcpy(R, A, N * sizeof(DWORD));
        return TRUE;
    }

    cb = (Tdigits + 2 * Ndigits + 3) * sizeof(DWORD);
    if (cb > sizeof(dwTemp)) {
        pdwSlowBuffer = RSA32Alloc(cb);
        if (pdwSlowBuffer == NULL) {
            return FALSE;
        }
        TempA = pdwSlowBuffer;
    } else {
        TempA = dwTemp;
    }

    TempB = TempA + Ndigits + 1;
    TempR = TempB + Ndigits + 1;

    memcpy(TempB, B, Ndigits * sizeof(DWORD));
    TempB[Ndigits] = 0;
    memcpy(TempR, A, Tdigits * sizeof(DWORD));
    TempR[Tdigits] = 0;

    for (i = Tdigits - Ndigits; i >= 0; i--) {
        q = EstimateQuotient(TempR[i + Ndigits], TempR[i + Ndigits - 1], B[Ndigits - 1], (Ndigits > 1) ? B[Ndigits - 2] : 0);
        if (q == 0) {
            q = 1;
        }
        TempA[Ndigits] = BaseMult(TempA, q, B, Ndigits);
        while (Compare(TempA, &TempR[i], Ndigits + 1) > 0) {
            Sub(TempA, TempA, TempB, Ndigits + 1);
        }
        Sub(&TempR[i], &TempR[i], TempA, Ndigits + 1);
        if (Compare(&TempR[i], TempB, Ndigits + 1) >= 0) {
            i++;
        }
    }

    memcpy(R, TempR, Ndigits * sizeof(DWORD));
    memset(R + Ndigits, 0, (N - Ndigits) * sizeof(DWORD));

    if (pdwSlowBuffer) {
        RSA32Free(pdwSlowBuffer);
    }

    return TRUE;
}
