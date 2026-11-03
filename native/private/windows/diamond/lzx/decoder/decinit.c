#include "decoder.h"

/* Counts the position slots the window needs and allocates the window with its mirrored tail */
bool allocate_decompression_memory(t_decoder_context *context)
{
    ulong pos_start;

    context->dec_num_position_slots = 4;
    pos_start = 4;

    while (1)
    {
        pos_start += 1L << dec_extra_bits[context->dec_num_position_slots];

        context->dec_num_position_slots++;

        if (pos_start >= context->dec_window_size)
            break;
    }

    context->dec_mem_window = (byte *) context->dec_malloc(context->dec_window_size + (MAX_MATCH + 4));

    return (context->dec_mem_window != NULL);
}

/* Frees the window when one is allocated */
void free_decompression_memory(t_decoder_context *context)
{
    if (context->dec_mem_window)
    {
        context->dec_free(context->dec_mem_window);
        context->dec_mem_window = NULL;
    }
}

/* Zeroes the current and previous path lengths of the main and length trees */
void reset_decoder_trees(t_decoder_context *context)
{
    memset(context->dec_main_tree_len, 0, MAIN_TREE_ELEMENTS);
    memset(context->dec_main_tree_prev_len, 0, MAIN_TREE_ELEMENTS);

    memset(context->dec_secondary_length_tree_len, 0, NUM_SECONDARY_LENGTHS);
    memset(context->dec_secondary_length_tree_prev_len, 0, NUM_SECONDARY_LENGTHS);
}

/* Resets the repeated offsets, the window position and the block state to start a new group */
void decoder_misc_init(t_decoder_context *context)
{
    context->dec_last_matchpos_offset[0] = 1;
    context->dec_last_matchpos_offset[1] = 1;
    context->dec_last_matchpos_offset[2] = 1;

    context->dec_bufpos = 0;
    context->dec_position_at_start = 0;

    context->dec_decoder_state = DEC_STATE_START_NEW_BLOCK;
    context->dec_block_size = 0;

    context->dec_block_type = BLOCKTYPE_INVALID;

    context->dec_first_time_this_group = true;
    context->dec_current_file_size = 0;

    context->dec_error_condition = false;
}

/* Restarts the instruction position the call translation counts from */
void init_decoder_translation(t_decoder_context *context)
{
    context->dec_instr_pos = 0;
}
