#include "decoder.h"

/* Decodes verbatim items near the window start, mirroring each byte written below MAX_MATCH past the window end */
static long special_decode_verbatim_block(t_decoder_context *context, long BufPos, int amount_to_decode)
{
    ulong match_pos;
    long bufpos_end;
    int match_length;
    int c;
    ulong dec_bitbuf;
    byte *dec_input_curpos;
    byte *dec_end_input_pos;
    char dec_bitcount;
    char m;

    dec_bitcount = context->dec_bitcount;
    dec_bitbuf = context->dec_bitbuf;
    dec_input_curpos = context->dec_input_curpos;
    dec_end_input_pos = context->dec_end_input_pos;

    bufpos_end = BufPos + amount_to_decode;

    while (BufPos < bufpos_end)
    {
        DECODE_MAIN_TREE(c);

        if ((c -= NUM_CHARS) < 0)
        {
            context->dec_mem_window[BufPos] = (byte) c;
            context->dec_mem_window[context->dec_window_size + BufPos] = (byte) c;
            BufPos++;
        }
        else
        {
            if ((match_length = c & NUM_PRIMARY_LENGTHS) == NUM_PRIMARY_LENGTHS)
            {
                DECODE_LEN_TREE_NOEOFCHECK(match_length);
            }

            m = c >> NL_SHIFT;

            if (m > 2)
            {
                if (m > 3)
                {
                    GET_BITS17_NOEOFCHECK(dec_extra_bits[m], match_pos);
                    match_pos += MP_POS_minus2[m];
                }
                else
                {
                    match_pos = 1;
                }

                context->dec_last_matchpos_offset[2] = context->dec_last_matchpos_offset[1];
                context->dec_last_matchpos_offset[1] = context->dec_last_matchpos_offset[0];
                context->dec_last_matchpos_offset[0] = match_pos;
            }
            else
            {
                match_pos = context->dec_last_matchpos_offset[m];

                if (m)
                {
                    context->dec_last_matchpos_offset[m] = context->dec_last_matchpos_offset[0];
                    context->dec_last_matchpos_offset[0] = match_pos;
                }
            }

            match_length += MIN_MATCH;

            do
            {
                context->dec_mem_window[BufPos] = context->dec_mem_window[(BufPos - match_pos) & context->dec_window_mask];

                if (BufPos < MAX_MATCH)
                    context->dec_mem_window[context->dec_window_size + BufPos] = context->dec_mem_window[BufPos];

                BufPos++;
            } while (--match_length > 0);
        }
    }

    context->dec_bitcount = dec_bitcount;
    context->dec_bitbuf = dec_bitbuf;
    context->dec_input_curpos = dec_input_curpos;

    return BufPos;
}

/* Decodes verbatim items away from the window start and returns the overshoot past the requested end */
long fast_decode_verbatim_block(t_decoder_context *context, long BufPos, int amount_to_decode)
{
    ulong match_pos;
    ulong match_ptr;
    long bufpos_end;
    long decode_residue;
    int match_length;
    int c;
    ulong dec_bitbuf;
    byte *dec_input_curpos;
    byte *dec_end_input_pos;
    char dec_bitcount;
    char m;

    dec_bitcount = context->dec_bitcount;
    dec_bitbuf = context->dec_bitbuf;
    dec_input_curpos = context->dec_input_curpos;
    dec_end_input_pos = context->dec_end_input_pos;

    bufpos_end = BufPos + amount_to_decode;

    while (BufPos < bufpos_end)
    {
        DECODE_MAIN_TREE(c);

        if ((c -= NUM_CHARS) < 0)
        {
            context->dec_mem_window[BufPos++] = (byte) c;
        }
        else
        {
            if ((match_length = c & NUM_PRIMARY_LENGTHS) == NUM_PRIMARY_LENGTHS)
            {
                DECODE_LEN_TREE_NOEOFCHECK(match_length);
            }

            m = c >> NL_SHIFT;

            if (m > 2)
            {
                if (m > 3)
                {
                    GET_BITS17_NOEOFCHECK(dec_extra_bits[m], match_pos);
                    match_pos += MP_POS_minus2[m];
                }
                else
                {
                    match_pos = MP_POS_minus2[3];
                }

                context->dec_last_matchpos_offset[2] = context->dec_last_matchpos_offset[1];
                context->dec_last_matchpos_offset[1] = context->dec_last_matchpos_offset[0];
                context->dec_last_matchpos_offset[0] = match_pos;
            }
            else
            {
                match_pos = context->dec_last_matchpos_offset[m];

                if (m)
                {
                    context->dec_last_matchpos_offset[m] = context->dec_last_matchpos_offset[0];
                    context->dec_last_matchpos_offset[0] = match_pos;
                }
            }

            match_length += MIN_MATCH;
            match_ptr = (BufPos - match_pos) & context->dec_window_mask;

            do
            {
                context->dec_mem_window[BufPos++] = context->dec_mem_window[match_ptr++];
            } while (--match_length > 0);
        }
    }

    context->dec_bitcount = dec_bitcount;
    context->dec_bitbuf = dec_bitbuf;
    context->dec_input_curpos = dec_input_curpos;

    decode_residue = BufPos - bufpos_end;

    BufPos &= context->dec_window_mask;
    context->dec_bufpos = BufPos;

    return decode_residue;
}

/* Decodes a run of a verbatim block, taking the mirroring path while the position is below MAX_MATCH */
long decode_verbatim_block(t_decoder_context *context, long BufPos, int amount_to_decode)
{
    if (BufPos < MAX_MATCH)
    {
        long new_bufpos;
        long amount_to_slowly_decode;

        amount_to_slowly_decode = ((MAX_MATCH - BufPos) < amount_to_decode) ? (MAX_MATCH - BufPos) : amount_to_decode;

        new_bufpos = special_decode_verbatim_block(context, BufPos, amount_to_slowly_decode);

        amount_to_decode -= (new_bufpos - BufPos);

        context->dec_bufpos = BufPos = new_bufpos;

        if (amount_to_decode <= 0)
            return amount_to_decode;
    }

    return fast_decode_verbatim_block(context, BufPos, amount_to_decode);
}
