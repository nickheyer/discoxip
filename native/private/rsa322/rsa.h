#ifndef _RSA32_RSA_
#define _RSA32_RSA_

#include "rsa32.h"

#define RSA1 ((DWORD)'1' << 24 | (DWORD)'A' << 16 | (DWORD)'S' << 8 | (DWORD)'R')

typedef struct {
    DWORD magic;
    DWORD keylen;
    DWORD bitlen;
    DWORD datalen;
    DWORD pubexp;
} BSAFE_PUB_KEY, *LPBSAFE_PUB_KEY;

BOOL RSA32API BSafeEncPublic(const LPBSAFE_PUB_KEY key, const BYTE *part_in, BYTE *part_out);

#endif
