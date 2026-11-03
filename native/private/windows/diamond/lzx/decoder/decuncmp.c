#include "decoder.h"

/* Copies stored bytes from the input into the window and mirrors the window head past its end */
long decode_uncompressed_block(t_decoder_context *context, long bufpos, int amount_to_decode)
{
    long bufpos_end;
    long decode_residue;
    ulong mirror_pos;
    ulong mirror_end;
    byte *p;

    mirror_pos = bufpos;
    p = context->dec_input_curpos;
    bufpos_end = bufpos + amount_to_decode;

    while (bufpos < bufpos_end)
    {
        if (p >= context->dec_end_input_pos)
            return -1;

        context->dec_mem_window[bufpos++] = *p++;
    }

    context->dec_input_curpos = p;

    mirror_end = (bufpos_end > MAX_MATCH) ? MAX_MATCH : bufpos_end;

    for (; mirror_pos < mirror_end; mirror_pos++)
        context->dec_mem_window[context->dec_window_size + mirror_pos] = context->dec_mem_window[mirror_pos];

    context->dec_bufpos = bufpos & context->dec_window_mask;

    decode_residue = bufpos - bufpos_end;

    return decode_residue;
}

/* Realigns the input after the block header and reads the three repeated offsets stored in it */
bool handle_beginning_of_uncompressed_block(t_decoder_context *context)
{
    int i;

    context->dec_input_curpos -= 2;

    if (context->dec_input_curpos + 4 >= context->dec_end_input_pos)
        return false;

    for (i = 0; i < NUM_REPEATED_OFFSETS; i++)
    {
        context->dec_last_matchpos_offset[i] =
            ((ulong) *((byte *) context->dec_input_curpos)) |
            ((ulong) *(((byte *) context->dec_input_curpos) + 1) << 8) |
            ((ulong) *(((byte *) context->dec_input_curpos) + 2) << 16) |
            ((ulong) *(((byte *) context->dec_input_curpos) + 3) << 24);

        context->dec_input_curpos += 4;
    }

    return true;
}
