#include "decoder.h"

/* Loads the first thirty two bits of a compressed frame unless an uncompressed block is in progress */
void initialise_decoder_bitbuf(t_decoder_context *context)
{
    byte *p;

    if (context->dec_block_type == BLOCKTYPE_UNCOMPRESSED)
        return;

    if ((context->dec_input_curpos + sizeof(ulong)) > context->dec_end_input_pos)
        return;

    p = context->dec_input_curpos;

    context->dec_bitbuf =
        ((ulong) p[2] | (((ulong) p[3]) << 8)) |
        ((((ulong) p[0] | (((ulong) p[1]) << 8))) << 16);

    context->dec_bitcount = 16;
    context->dec_input_curpos += 4;
}

/* Primes the bit buffer from a new input frame */
void init_decoder_input(t_decoder_context *context)
{
    initialise_decoder_bitbuf(context);
}

/* Shifts n bits out of the bit buffer and refills it, flagging an error when the input runs out */
void fillbuf(t_decoder_context *context, int n)
{
    context->dec_bitbuf <<= n;
    context->dec_bitcount -= (char) n;

    if (context->dec_bitcount <= 0)
    {
        if (context->dec_input_curpos >= context->dec_end_input_pos)
        {
            context->dec_error_condition = true;
            return;
        }

        context->dec_bitbuf |= ((((ulong) *context->dec_input_curpos | (((ulong) *(context->dec_input_curpos + 1)) << 8))) << (-context->dec_bitcount));
        context->dec_input_curpos += 2;
        context->dec_bitcount += 16;

        if (context->dec_bitcount <= 0)
        {
            if (context->dec_input_curpos >= context->dec_end_input_pos)
            {
                context->dec_error_condition = true;
                return;
            }

            context->dec_bitbuf |= ((((ulong) *context->dec_input_curpos | (((ulong) *(context->dec_input_curpos + 1)) << 8))) << (-context->dec_bitcount));
            context->dec_input_curpos += 2;
            context->dec_bitcount += 16;
        }
    }
}

/* Returns the next n bits of the input and consumes them */
ulong getbits(t_decoder_context *context, int n)
{
    ulong value;

    value = context->dec_bitbuf >> (32 - (n));
    fillbuf(context, n);

    return value;
}
