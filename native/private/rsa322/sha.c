#include "sha.h"

// Loads the standard initial chaining values and clears the byte count
void RSA32API A_SHAInit(A_SHA_CTX *context)
{
    context->count[0] = 0;
    context->count[1] = 0;

    context->state[0] = 0x67452301;
    context->state[1] = 0xefcdab89;
    context->state[2] = 0x98badcfe;
    context->state[3] = 0x10325476;
    context->state[4] = 0xc3d2e1f0;
}

// Hashes every full 64 byte block of input and buffers the remainder
void RSA32API A_SHAUpdate(A_SHA_CTX *context, unsigned char *partIn, unsigned int partInLen)
{
    unsigned int bufferLen;

    bufferLen = (unsigned int)(context->count[1] & 0x3f);

    if ((context->count[1] += partInLen) < partInLen) {
        context->count[0]++;
    }

    if (bufferLen > 0 && bufferLen + partInLen >= 64) {
        memcpy(context->buffer + bufferLen, partIn, 64 - bufferLen);
        partIn += (64 - bufferLen);
        partInLen -= (64 - bufferLen);
        SHATransform(context->state, context->buffer);
        bufferLen = 0;
    }

    if ((unsigned long)partIn & 3) {
        while (partInLen >= 64) {
            memcpy(context->buffer, partIn, 64);
            SHATransform(context->state, context->buffer);
            partIn += 64;
            partInLen -= 64;
        }
    } else {
        while (partInLen >= 64) {
            SHATransform(context->state, partIn);
            partIn += 64;
            partInLen -= 64;
        }
    }

    if (partInLen) {
        memcpy(context->buffer + bufferLen, partIn, partInLen);
    }
}

// Pads the message with its bit count, writes the big endian digest and resets the context
void RSA32API A_SHAFinal(A_SHA_CTX *context, unsigned char *digest)
{
    unsigned long bitCount[2];
    unsigned char pad[72];
    unsigned int padLen;

    padLen = 64 - (context->count[1] & 0x3f);
    if (padLen <= 8) {
        padLen += 64;
    }

    memset(pad, 0, padLen - 6);
    pad[0] = 0x80;

    bitCount[0] = (context->count[0] << 3) | (context->count[1] >> 29);
    bitCount[1] = context->count[1] << 3;
    DWORDToBigEndian(pad + padLen - 8, bitCount, 2);

    A_SHAUpdate(context, pad, padLen);

    DWORDToBigEndian(digest, context->state, 5);

    A_SHAInit(context);
}
