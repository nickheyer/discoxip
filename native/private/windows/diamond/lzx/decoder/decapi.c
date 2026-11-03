#include "decoder.h"

/* Releases the window of the decoder */
void LZX_DecodeFree(t_decoder_context *context)
{
    free_decompression_memory(context);
}

/* Clears the trees, the repeated offsets and the call translation state for a new group of frames */
void LZX_DecodeNewGroup(t_decoder_context *context)
{
    reset_decoder_trees(context);
    decoder_misc_init(context);
    init_decoder_translation(context);

    context->dec_num_cfdata_frames = 0;
}

/* Decodes one frame into the output buffer and returns zero on success */
int LZX_Decode(
    t_decoder_context *context,
    long bytes_to_decode,
    byte *compressed_input_buffer,
    long compressed_input_size,
    byte *uncompressed_output_buffer,
    long uncompressed_output_size,
    long *bytes_decoded)
{
    long result;

    context->dec_input_curpos = compressed_input_buffer;
    context->dec_end_input_pos = (compressed_input_buffer + compressed_input_size + 4);

    context->dec_output_buffer = uncompressed_output_buffer;

    init_decoder_input(context);

    result = decode_data(context, bytes_to_decode);

    context->dec_num_cfdata_frames++;

    if (result < 0)
    {
        *bytes_decoded = 0;
        return 1;
    }
    else
    {
        *bytes_decoded = result;
        context->dec_position_at_start += result;
        return 0;
    }
}

/* Records the callbacks and allocates a window of the given power of two size */
bool LZX_DecodeInit(
    t_decoder_context *context,
    long compression_window_size,
    PFNALLOC pfnma,
    PFNFREE pfnmf,
    PFNOPEN pfnopen,
    PFNREAD pfnread,
    PFNWRITE pfnwrite,
    PFNCLOSE pfnclose,
    PFNSEEK pfnseek)
{
    context->dec_malloc = pfnma;
    context->dec_free = pfnmf;
    context->dec_open = pfnopen;
    context->dec_read = pfnread;
    context->dec_write = pfnwrite;
    context->dec_close = pfnclose;
    context->dec_seek = pfnseek;

    context->dec_window_size = compression_window_size;
    context->dec_window_mask = context->dec_window_size - 1;

    if (context->dec_window_size & context->dec_window_mask)
        return false;

    if (allocate_decompression_memory(context) == false)
        return false;

    LZX_DecodeNewGroup(context);

    return true;
}
