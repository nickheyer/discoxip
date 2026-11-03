#include "decoder.h"

/* Dispatches a run of bytes to the decoder of the current block type */
static long decode_block(t_decoder_context *context, lzx_block_type block_type, long bufpos, long amount_to_decode)
{
    long result;

    if (block_type == BLOCKTYPE_ALIGNED)
        result = decode_aligned_offset_block(context, bufpos, (int) amount_to_decode);
    else if (block_type == BLOCKTYPE_VERBATIM)
        result = decode_verbatim_block(context, bufpos, (int) amount_to_decode);
    else if (block_type == BLOCKTYPE_UNCOMPRESSED)
        result = decode_uncompressed_block(context, bufpos, (int) amount_to_decode);
    else
        result = -1;

    return result;
}

/* Decodes the requested bytes across block boundaries and returns how many were produced or -1 */
long decode_data(t_decoder_context *context, long bytes_to_decode)
{
    long amount_can_decode;
    long total_decoded;

    total_decoded = 0;

    while (bytes_to_decode > 0)
    {
        if (context->dec_decoder_state == DEC_STATE_START_NEW_BLOCK)
        {
            ulong temp1;
            ulong temp2;
            ulong temp3;
            bool do_translation;

            if (context->dec_first_time_this_group)
            {
                context->dec_first_time_this_group = false;

                do_translation = (bool) getbits(context, 1);

                if (do_translation)
                {
                    ulong high, low;

                    high = getbits(context, 16);
                    low = getbits(context, 16);
                    context->dec_current_file_size = (high << 16) | low;
                }
                else
                {
                    context->dec_current_file_size = 0;
                }
            }

            if (context->dec_block_type == BLOCKTYPE_UNCOMPRESSED)
            {
                if (context->dec_original_block_size & 1)
                {
                    if (context->dec_input_curpos < context->dec_end_input_pos)
                        context->dec_input_curpos++;
                }

                context->dec_block_type = BLOCKTYPE_INVALID;

                initialise_decoder_bitbuf(context);
            }

            context->dec_block_type = (lzx_block_type) getbits(context, 3);

            temp1 = getbits(context, 8);
            temp2 = getbits(context, 8);
            temp3 = getbits(context, 8);

            context->dec_block_size =
            context->dec_original_block_size = (temp1 << 16) + (temp2 << 8) + (temp3);

            if (context->dec_block_type == BLOCKTYPE_ALIGNED)
                read_aligned_offset_tree(context);

            if (context->dec_block_type == BLOCKTYPE_VERBATIM ||
                context->dec_block_type == BLOCKTYPE_ALIGNED)
            {
                memcpy(
                    context->dec_main_tree_prev_len,
                    context->dec_main_tree_len,
                    MAIN_TREE_ELEMENTS);

                memcpy(
                    context->dec_secondary_length_tree_prev_len,
                    context->dec_secondary_length_tree_len,
                    NUM_SECONDARY_LENGTHS);

                read_main_and_secondary_trees(context);
            }
            else if (context->dec_block_type == BLOCKTYPE_UNCOMPRESSED)
            {
                if (handle_beginning_of_uncompressed_block(context) == false)
                    return -1;
            }
            else
            {
                return -1;
            }

            context->dec_decoder_state = DEC_STATE_DECODING_DATA;
        }

        while ((context->dec_block_size > 0) && (bytes_to_decode > 0))
        {
            long decode_residue;

            amount_can_decode = (context->dec_block_size < bytes_to_decode) ? context->dec_block_size : bytes_to_decode;

            if (amount_can_decode == 0)
                return -1;

            decode_residue = decode_block(
                context,
                context->dec_block_type,
                context->dec_bufpos,
                amount_can_decode);

            if (decode_residue != 0)
                return -1;

            context->dec_block_size -= amount_can_decode;
            bytes_to_decode -= amount_can_decode;
            total_decoded += amount_can_decode;
        }

        if (context->dec_block_size == 0)
        {
            context->dec_decoder_state = DEC_STATE_START_NEW_BLOCK;
        }

        if (bytes_to_decode == 0)
        {
            initialise_decoder_bitbuf(context);
        }
    }

    copy_data_to_output(
        context,
        total_decoded,
        context->dec_bufpos ?
            &context->dec_mem_window[context->dec_bufpos - total_decoded] :
            &context->dec_mem_window[context->dec_window_size - total_decoded]);

    return total_decoded;
}
