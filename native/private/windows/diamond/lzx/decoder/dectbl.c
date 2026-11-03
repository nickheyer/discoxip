#include "decoder.h"

/* Builds the lookup table and overflow tree of a canonical Huffman code, false when the lengths are inconsistent */
bool make_table(
    t_decoder_context *context,
    int nchar,
    const byte *bitlen,
    byte tablebits,
    short *table,
    short *leftright)
{
    uint i;
    int ch;
    short *p;
    uint count[17], weight[17], start[18];
    int avail;
    uint nextcode;
    uint k;
    byte len;
    byte jutbits;

    for (i = 1; i <= 16; i++)
        count[i] = 0;

    for (i = 0; i < (uint) nchar; i++)
        count[bitlen[i]]++;

    start[1] = 0;

    for (i = 1; i <= 16; i++)
        start[i + 1] = start[i] + (count[i] << (16 - i));

    if (start[17] != 65536)
    {
        if (start[17] == 0)
        {
            memset(table, 0, sizeof(ushort) * (1 << tablebits));
            return true;
        }
        else
        {
            return false;
        }
    }

    jutbits = 16 - tablebits;

    for (i = 1; i <= tablebits; i++)
    {
        start[i] >>= jutbits;
        weight[i] = 1 << (tablebits - i);
    }

    while (i <= 16)
    {
        weight[i] = 1 << (16 - i);
        i++;
    }

    i = start[tablebits + 1] >> jutbits;

    if (i != 65536)
    {
        memset(&table[i], 0, sizeof(ushort) * ((1 << tablebits) - i));
    }

    avail = nchar;

    for (ch = 0; ch < nchar; ch++)
    {
        if ((len = bitlen[ch]) == 0)
            continue;

        nextcode = start[len] + weight[len];

        if (len <= tablebits)
        {
            if (nextcode > (uint) (1 << tablebits))
                return false;

            for (i = start[len]; i < nextcode; i++)
                table[i] = (short) ch;

            start[len] = nextcode;
        }
        else
        {
            byte bi;

            k = start[len];
            start[len] = nextcode;
            p = &table[k >> jutbits];

            bi = len - tablebits;
            k <<= tablebits;

            do
            {
                if (*p == 0)
                {
                    leftright[avail * 2] = leftright[avail * 2 + 1] = 0;
                    *p = (short) -avail;
                    avail++;
                }

                if ((signed short) k < 0)
                    p = &leftright[-(*p) * 2 + 1];
                else
                    p = &leftright[-(*p) * 2];

                k <<= 1;
                bi--;
            } while (bi);

            *p = (short) ch;
        }
    }

    return true;
}

/* Builds the byte lookup table of the eight symbol aligned offset code, false when the lengths are inconsistent */
bool make_table_8bit(t_decoder_context *context, byte *bitlen, byte *table)
{
    ushort count[17], weight[17], start[18];
    ushort i;
    ushort nextcode;
    byte len;
    byte ch;

    for (i = 1; i <= 16; i++)
        count[i] = 0;

    for (i = 0; i < 8; i++)
        count[bitlen[i]]++;

    start[1] = 0;

    for (i = 1; i <= 16; i++)
        start[i + 1] = start[i] + (count[i] << (16 - i));

    if (start[17] != 0)
        return false;

    for (i = 1; i <= 7; i++)
    {
        start[i] >>= 9;
        weight[i] = 1 << (7 - i);
    }

    while (i <= 16)
    {
        weight[i] = 1 << (16 - i);
        i++;
    }

    memset(table, 0, 1 << 7);

    for (ch = 0; ch < 8; ch++)
    {
        if ((len = bitlen[ch]) == 0)
            continue;

        nextcode = start[len] + weight[len];

        if (nextcode > (1 << 7))
            return false;

        for (i = start[len]; i < nextcode; i++)
            table[i] = ch;

        start[len] = nextcode;
    }

    return true;
}
