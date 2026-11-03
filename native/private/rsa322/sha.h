#ifndef _RSA32_SHA_
#define _RSA32_SHA_

#include "rsa32.h"

#define A_SHA_DIGEST_LEN 20

typedef struct {
    unsigned long FinishFlag;
    unsigned char HashVal[A_SHA_DIGEST_LEN];
    unsigned long state[5];
    unsigned long count[2];
    unsigned char buffer[64];
} A_SHA_CTX;

void RSA32API A_SHAInit(A_SHA_CTX *context);
void RSA32API A_SHAUpdate(A_SHA_CTX *context, unsigned char *partIn, unsigned int partInLen);
void RSA32API A_SHAFinal(A_SHA_CTX *context, unsigned char *digest);
void RSA32API SHATransform(unsigned long *state, unsigned char *block);
void RSA32API DWORDToBigEndian(unsigned char *block, unsigned long *x, unsigned int digits);

#endif
