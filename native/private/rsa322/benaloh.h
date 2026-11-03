#ifndef _RSA32_BENALOH_
#define _RSA32_BENALOH_

#include "rsa_math.h"

struct BenalohData {
    DWORD N;
    LPDWORD M;
    LPDWORD U;
    LPDWORD V;
    LPDWORD product;
};

BOOL RSA32API BenalohSetup(struct BenalohData *context, LPDWORD modulus, DWORD length);
void RSA32API BenalohMod(struct BenalohData *context, LPDWORD A, LPDWORD B);
void RSA32API BenalohModSquare(struct BenalohData *context, LPDWORD A, LPDWORD B);
BOOL RSA32API BenalohModExp(LPDWORD A, LPDWORD B, LPDWORD C, LPDWORD D, DWORD len);
BOOL RSA32API BenalohModRoot(LPDWORD M, LPDWORD C, LPDWORD PP, LPDWORD QQ, LPDWORD DP, LPDWORD DQ, LPDWORD CR, DWORD PSize);

#endif
