#include "rsa.h"
#include "benaloh.h"

// Raises the input block to the public exponent modulo the key modulus, failing on oversized input
BOOL RSA32API BSafeEncPublic(const LPBSAFE_PUB_KEY key, const BYTE *part_in, BYTE *part_out)
{
    LPDWORD modulus;
    DWORD dwTemp[34];
    LPDWORD pdwExp;
    LPDWORD pdwSlowBuffer = NULL;
    BOOL fResult = FALSE;
    DWORD PSize;
    DWORD halfBits;

    if (key->magic != RSA1) {
        return FALSE;
    }

    halfBits = key->bitlen / 2;
    PSize = halfBits / 32 + 1;
    if (halfBits % 32) {
        PSize++;
    }

    if (key->pubexp == 1) {
        memcpy(part_out, part_in, 2 * PSize * sizeof(DWORD));
        return TRUE;
    }

    modulus = (LPDWORD)(key + 1);
    if (Compare((LPDWORD)part_in, modulus, 2 * PSize) >= 0) {
        goto Cleanup;
    }

    if (2 * PSize * sizeof(DWORD) > sizeof(dwTemp)) {
        pdwSlowBuffer = RSA32Alloc(2 * PSize * sizeof(DWORD));
        if (pdwSlowBuffer == NULL) {
            goto Cleanup;
        }
        pdwExp = pdwSlowBuffer;
    } else {
        pdwExp = dwTemp;
    }

    memset(pdwExp, 0, 2 * PSize * sizeof(DWORD));
    pdwExp[0] = key->pubexp;
    fResult = BenalohModExp((LPDWORD)part_out, (LPDWORD)part_in, pdwExp, modulus, 2 * PSize);

Cleanup:
    if (pdwSlowBuffer) {
        RSA32Free(pdwSlowBuffer);
    }

    return fResult;
}
