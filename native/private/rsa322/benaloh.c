#include "benaloh.h"

// Normalizes the modulus and precomputes the folding constants used by the reduction
BOOL RSA32API BenalohSetup(struct BenalohData *context, LPDWORD modulus, DWORD length)
{
    BYTE *SlowBuffer;
    DWORD cb;

    if (length == 0 || modulus[length - 1] == 0) {
        return FALSE;
    }

    cb = length * sizeof(DWORD);
    SlowBuffer = RSA32Alloc(cb * 3 + (length * 2 + 1) * sizeof(DWORD));
    if (SlowBuffer == NULL) {
        return FALSE;
    }

    context->M = (LPDWORD)SlowBuffer;
    context->U = context->M + length;
    context->V = context->U + length;
    context->product = context->V + length;

    memcpy(context->M, modulus, cb);
    while (!(context->M[length - 1] & HIGH_BIT)) {
        Add(context->M, context->M, context->M, length);
    }

    memset(context->product, 0, (length + 1) * sizeof(DWORD));
    context->product[length + 1] = 1;
    if (!Mod(context->product, modulus, context->U, length + 2, length)) {
        RSA32Free(SlowBuffer);
        return FALSE;
    }

    Sub(context->V, modulus, context->U, length);
    context->N = length;

    return TRUE;
}

// Reduces the double length A modulo the normalized modulus and stores the result in B
void RSA32API BenalohMod(struct BenalohData *context, LPDWORD A, LPDWORD B)
{
    LPDWORD U;
    LPDWORD V;
    LPDWORD M;
    LPDWORD p;
    DWORD N;
    DWORD borrow;

    U = context->U;
    V = context->V;
    M = context->M;
    N = context->N;

    borrow = 0;
    for (p = A + N - 2; p >= A; p--) {
        if (p[N + 1] < borrow) {
            borrow = Reduce(p, borrow - p[N + 1], U, N);
        } else {
            borrow = Reduce(p, p[N + 1] - borrow, V, N);
        }
    }

    if (A[N] < borrow) {
        A[N] -= borrow;
        while (!Add(A + 1, A + 1, M, N));
    } else {
        A[N] -= borrow;
        if (Compare(A + 1, M, N) >= 0) {
            Sub(A + 1, A + 1, M, N);
        }
    }

    A[N] -= Reduce(A, BenalohEstimateQuotient(A[N], A[N - 1], M[N - 1]), M, N);
    while (A[N] != 0) {
        A[N] += Add(A, A, M, N);
    }

    memcpy(B, A, N * sizeof(DWORD));
}

// Stores the square of B reduced modulo the context modulus in A
void RSA32API BenalohModSquare(struct BenalohData *context, LPDWORD A, LPDWORD B)
{
    Square(context->product, B, context->N);
    BenalohMod(context, context->product, A);
}

// Stores B raised to the power C modulo D in A using a sliding window of odd powers
BOOL RSA32API BenalohModExp(LPDWORD A, LPDWORD B, LPDWORD C, LPDWORD D, DWORD len)
{
    struct BenalohData BenalohContext;
    int cLen;
    int N;
    int squaresAfter;
    int powerTableSize;
    DWORD mask;
    int firstTime;
    int i;
    int prevPosition;
    int window;
    int squares;
    LPDWORD powerTable;
    LPDWORD oddPowers;

    cLen = BitLen(C, len);
    if (cLen == 0) {
        SetValDWORD(A, 1, len);
        return TRUE;
    }

    N = DigitLen(D, len);
    if (!BenalohSetup(&BenalohContext, D, N)) {
        return FALSE;
    }

    if (cLen <= 17) {
        powerTableSize = 1;
    } else if (cLen <= 32) {
        powerTableSize = 2;
    } else if (cLen <= 64) {
        powerTableSize = 4;
    } else if (cLen <= 128) {
        powerTableSize = 8;
    } else if (cLen <= 256) {
        powerTableSize = 16;
    } else {
        powerTableSize = 32;
    }

    powerTable = RSA32Alloc((powerTableSize + 1) * N * sizeof(DWORD));
    if (powerTable == NULL) {
        RSA32Free(BenalohContext.M);
        return FALSE;
    }

    oddPowers = powerTable + N;
    memcpy(oddPowers, B, N * sizeof(DWORD));
    if (powerTableSize > 1) {
        BenalohModSquare(&BenalohContext, powerTable, B);
        for (i = 1; i < powerTableSize; i++) {
            Multiply(BenalohContext.product, oddPowers + (i - 1) * N, powerTable, BenalohContext.N);
            BenalohMod(&BenalohContext, BenalohContext.product, oddPowers + i * N);
        }
    }

    mask = 1 << ((cLen - 1) % DIGIT_BITS);
    window = 0;
    firstTime = TRUE;
    prevPosition = cLen - 1;

    for (i = cLen - 1; i >= 0; i--) {
        window <<= 1;
        if (C[i / DIGIT_BITS] & mask) {
            window++;
        }
        mask = (mask << 31) | (mask >> 1);

        if (i != 0 && window < powerTableSize) {
            continue;
        }

        squares = prevPosition - i;
        squaresAfter = 0;
        prevPosition = i;
        while (window != 0 && (window % 2) == 0) {
            window /= 2;
            squares--;
            squaresAfter++;
        }

        if (firstTime) {
            memcpy(powerTable, oddPowers + (window - 1) * N / 2, N * sizeof(DWORD));
            firstTime = FALSE;
        } else {
            while (squares--) {
                Square(BenalohContext.product, powerTable, BenalohContext.N);
                BenalohMod(&BenalohContext, BenalohContext.product, powerTable);
            }
            if (window) {
                Multiply(BenalohContext.product, powerTable, oddPowers + (window - 1) * N / 2, BenalohContext.N);
                BenalohMod(&BenalohContext, BenalohContext.product, powerTable);
            }
        }

        while (squaresAfter--) {
            Square(BenalohContext.product, powerTable, BenalohContext.N);
            BenalohMod(&BenalohContext, BenalohContext.product, powerTable);
        }

        window = 0;
    }

    if (!Mod(powerTable, D, A, N, N)) {
        RSA32Free(BenalohContext.M);
        RSA32Free(powerTable);
        return FALSE;
    }

    memset(A + N, 0, (len - N) * sizeof(DWORD));
    RSA32Free(BenalohContext.M);
    RSA32Free(powerTable);

    return TRUE;
}

// Stores in M the CRT root of C from the primes PP QQ, exponents DP DQ and coefficient CR
BOOL RSA32API BenalohModRoot(LPDWORD M, LPDWORD C, LPDWORD PP, LPDWORD QQ, LPDWORD DP, LPDWORD DQ, LPDWORD CR, DWORD PSize)
{
    LPDWORD pdwSlowBuffer = NULL;
    LPDWORD t1;
    LPDWORD t2;
    LPDWORD t3;
    DWORD cbt1;
    BOOL fSuccess = FALSE;
    DWORD dwTemp[68];

    cbt1 = PSize * 4 * sizeof(DWORD);
    if (cbt1 > sizeof(dwTemp)) {
        pdwSlowBuffer = RSA32Alloc(cbt1);
        if (pdwSlowBuffer == NULL) {
            return FALSE;
        }
        t1 = pdwSlowBuffer;
    } else {
        t1 = dwTemp;
    }

    t2 = t1 + PSize;
    t3 = t2 + 2 * PSize;

    if (!Mod(C, PP, t3, 2 * PSize - 1, PSize)) {
        goto Cleanup;
    }
    if (!BenalohModExp(t1, t3, DP, PP, PSize)) {
        goto Cleanup;
    }
    if (!Mod(C, QQ, t3, 2 * PSize - 1, PSize)) {
        goto Cleanup;
    }
    if (!BenalohModExp(t2, t3, DQ, QQ, PSize)) {
        goto Cleanup;
    }

    if (Sub(t3, t1, t2, PSize)) {
        while (!Add(t3, t3, PP, PSize));
    }

    Multiply(M, t3, CR, PSize);
    if (!Mod(M, PP, t3, 2 * PSize - 1, PSize)) {
        goto Cleanup;
    }
    Multiply(M, t3, QQ, PSize);

    memset(t2 + PSize, 0, PSize * sizeof(DWORD));
    Add(M, M, t2, 2 * PSize);

    fSuccess = TRUE;

Cleanup:
    memset(t1, 0, cbt1);
    if (pdwSlowBuffer) {
        RSA32Free(pdwSlowBuffer);
    }

    return fSuccess;
}
