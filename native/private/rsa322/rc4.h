#ifndef _RSA32_RC4_
#define _RSA32_RC4_

#include "rsa32.h"

typedef struct RC4_KEYSTRUCT {
    unsigned char S[256];
    unsigned char i;
    unsigned char j;
} RC4_KEYSTRUCT;

void RSA32API rc4_key(struct RC4_KEYSTRUCT *pKS, unsigned int dwLen, unsigned char *pbKey);
void RSA32API rc4(struct RC4_KEYSTRUCT *pKS, unsigned int dwLen, unsigned char *pbuf);

#endif
