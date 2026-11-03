#include "decoder.h"

#define DECODE_SMALL(item) \
{ \
    item = small_table[context->dec_bitbuf >> (32 - DS_TABLE_BITS)]; \
    if (item < 0) \
    { \
        mask = (1L << (32 - 1 - DS_TABLE_BITS)); \
        do \
        { \
            item = -item; \
            if (context->dec_bitbuf & mask) \
                item = leftright[2 * item + 1]; \
            else \
                item = leftright[2 * item]; \
            mask >>= 1; \
        } while (item < 0); \
    } \
    fillbuf(context, small_bitlen[item]); \
}

/* Reads path lengths coded as modulo 17 deltas from the previous lengths with zero and repeat runs through a pretree */
static bool read_rep_tree(
    t_decoder_context *context,
    int num_elements,
    byte *lastlen,
    byte *len)
{
    ulong mask;
    int i;
    int consecutive;
    byte small_bitlen[24];
    short small_table[1 << DS_TABLE_BITS];
    short leftright[2 * (2 * 24 - 1)];
    short Temp;

    for (i = 0; i < NUM_DECODE_SMALL; i++)
    {
        small_bitlen[i] = (byte) getbits(context, 4);
    }

    if (context->dec_error_condition)
        return false;

    make_table(
        context,
        NUM_DECODE_SMALL,
        small_bitlen,
        DS_TABLE_BITS,
        small_table,
        leftright);

    for (i = 0; i < num_elements; i++)
    {
        DECODE_SMALL(Temp);

        if (context->dec_error_condition)
            return false;

        if (Temp == 17)
        {
            consecutive = (byte) getbits(context, TREE_ENC_REPZ_FIRST_EXTRA_BITS);
            consecutive += TREE_ENC_REP_MIN;

            if (i + consecutive >= num_elements)
                consecutive = num_elements - i;

            while (consecutive-- > 0)
                len[i++] = 0;

            i--;
        }
        else if (Temp == 18)
        {
            consecutive = (byte) getbits(context, TREE_ENC_REPZ_SECOND_EXTRA_BITS);
            consecutive += (TREE_ENC_REP_MIN + TREE_ENC_REP_ZERO_FIRST);

            if (i + consecutive >= num_elements)
                consecutive = num_elements - i;

            while (consecutive-- > 0)
                len[i++] = 0;

            i--;
        }
        else if (Temp == 19)
        {
            byte value;

            consecutive = (byte) getbits(context, TREE_ENC_REP_SAME_EXTRA_BITS);
            consecutive += TREE_ENC_REP_MIN;

            if (i + consecutive >= num_elements)
                consecutive = num_elements - i;

            DECODE_SMALL(Temp);
            value = dec_modulo17_lookup[(lastlen[i] - Temp) + 17];

            while (consecutive-- > 0)
                len[i++] = value;

            i--;
        }
        else
        {
            len[i] = dec_modulo17_lookup[(lastlen[i] - Temp) + 17];
        }
    }

    if (context->dec_error_condition)
        return false;
    else
        return true;
}

/* Reads the main tree in its literal and match halves and the length tree and builds both lookup tables */
bool read_main_and_secondary_trees(t_decoder_context *context)
{
    if (false == read_rep_tree(
            context,
            256,
            context->dec_main_tree_prev_len,
            context->dec_main_tree_len))
    {
        return false;
    }

    if (false == read_rep_tree(
            context,
            context->dec_num_position_slots * (NUM_PRIMARY_LENGTHS + 1),
            &context->dec_main_tree_prev_len[256],
            &context->dec_main_tree_len[256]))
    {
        return false;
    }

    if (false == make_table(
            context,
            MAIN_TREE_ELEMENTS,
            context->dec_main_tree_len,
            MAIN_TREE_TABLE_BITS,
            context->dec_main_tree_table,
            context->dec_main_tree_left_right))
    {
        return false;
    }

    if (false == read_rep_tree(
            context,
            NUM_SECONDARY_LENGTHS,
            context->dec_secondary_length_tree_prev_len,
            context->dec_secondary_length_tree_len))
    {
        return false;
    }

    if (false == make_table(
            context,
            NUM_SECONDARY_LENGTHS,
            context->dec_secondary_length_tree_len,
            SECONDARY_LEN_TREE_TABLE_BITS,
            context->dec_secondary_length_tree_table,
            context->dec_secondary_length_tree_left_right))
    {
        return false;
    }

    return true;
}

/* Reads the three bit lengths of the eight aligned offset codes and builds their table */
bool read_aligned_offset_tree(t_decoder_context *context)
{
    int i;

    for (i = 0; i < ALIGNED_NUM_ELEMENTS; i++)
    {
        context->dec_aligned_len[i] = (byte) getbits(context, 3);
    }

    if (context->dec_error_condition)
        return false;

    if (false == make_table_8bit(context, context->dec_aligned_len, (byte *) context->dec_aligned_table))
        return false;

    return true;
}
