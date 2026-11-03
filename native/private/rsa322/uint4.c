#include "sha.h"

// Stores each dword of the array as four bytes, most significant byte first
void RSA32API DWORDToBigEndian(unsigned char *block, unsigned long *x, unsigned int digits)
{
    unsigned int i, j;

    for (i = 0, j = 0; i < digits; i++, j += 4) {
        block[j] = (unsigned char)((x[i] >> 24) & 0xff);
        block[j + 1] = (unsigned char)((x[i] >> 16) & 0xff);
        block[j + 2] = (unsigned char)((x[i] >> 8) & 0xff);
        block[j + 3] = (unsigned char)(x[i] & 0xff);
    }
}
