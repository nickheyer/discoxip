#ifndef DECODER_H
#define DECODER_H

#include "lzxconst.h"

void * __cdecl memcpy(void *dst, const void *src, unsigned int count);
void * __cdecl memset(void *dst, int value, unsigned int count);
#pragma intrinsic(memcpy, memset)

typedef enum
{
    BLOCKTYPE_INVALID = 0,
    BLOCKTYPE_VERBATIM = 1,
    BLOCKTYPE_ALIGNED = 2,
    BLOCKTYPE_UNCOMPRESSED = 3
} lzx_block_type;

typedef enum
{
    DEC_STATE_START_NEW_BLOCK = 1,
    DEC_STATE_DECODING_DATA = 2
} decoder_state;

struct t_decoder_context
{
    byte            *dec_mem_window;
    ulong           dec_window_size;
    ulong           dec_window_mask;
    ulong           dec_last_matchpos_offset[NUM_REPEATED_OFFSETS];
    short           dec_main_tree_table[1 << MAIN_TREE_TABLE_BITS];
    short           dec_secondary_length_tree_table[1 << SECONDARY_LEN_TREE_TABLE_BITS];
    byte            dec_main_tree_len[MAX_MAIN_TREE_ELEMENTS];
    byte            dec_secondary_length_tree_len[NUM_SECONDARY_LENGTHS];
    byte            pad1[3];
    char            dec_aligned_table[1 << ALIGNED_TABLE_BITS];
    byte            dec_aligned_len[ALIGNED_NUM_ELEMENTS];
    short           dec_main_tree_left_right[MAX_MAIN_TREE_ELEMENTS * 4];
    short           dec_secondary_length_tree_left_right[NUM_SECONDARY_LENGTHS * 4];
    byte            *dec_input_curpos;
    byte            *dec_end_input_pos;
    byte            *dec_output_buffer;
    long            dec_position_at_start;
    byte            dec_main_tree_prev_len[MAX_MAIN_TREE_ELEMENTS];
    byte            dec_secondary_length_tree_prev_len[NUM_SECONDARY_LENGTHS];
    ulong           dec_bitbuf;
    char            dec_bitcount;
    byte            dec_num_position_slots;
    bool            dec_first_time_this_group;
    bool            dec_error_condition;
    long            dec_bufpos;
    ulong           dec_current_file_size;
    ulong           dec_instr_pos;
    ulong           dec_num_cfdata_frames;
    long            dec_original_block_size;
    long            dec_block_size;
    lzx_block_type  dec_block_type;
    decoder_state   dec_decoder_state;
    PFNALLOC        dec_malloc;
    PFNFREE         dec_free;
    PFNOPEN         dec_open;
    PFNREAD         dec_read;
    PFNWRITE        dec_write;
    PFNCLOSE        dec_close;
    PFNSEEK         dec_seek;
};

#define MAIN_TREE_ELEMENTS (NUM_CHARS + (context->dec_num_position_slots << NL_SHIFT))

bool allocate_decompression_memory(t_decoder_context *context);
void free_decompression_memory(t_decoder_context *context);
void reset_decoder_trees(t_decoder_context *context);
void decoder_misc_init(t_decoder_context *context);
void init_decoder_translation(t_decoder_context *context);
void decoder_translate_e8(t_decoder_context *context, byte *mem, long bytes);
long decode_data(t_decoder_context *context, long bytes_to_decode);
void initialise_decoder_bitbuf(t_decoder_context *context);
void init_decoder_input(t_decoder_context *context);
void fillbuf(t_decoder_context *context, int n);
ulong getbits(t_decoder_context *context, int n);
long decode_uncompressed_block(t_decoder_context *context, long bufpos, int amount_to_decode);
bool handle_beginning_of_uncompressed_block(t_decoder_context *context);
long fast_decode_verbatim_block(t_decoder_context *context, long BufPos, int amount_to_decode);
long decode_verbatim_block(t_decoder_context *context, long bufpos, int amount_to_decode);
long fast_decode_aligned_offset_block(t_decoder_context *context, long bufpos, int amount_to_decode);
long decode_aligned_offset_block(t_decoder_context *context, long bufpos, int amount_to_decode);
void copy_data_to_output(t_decoder_context *context, long amount, const byte *data);
bool read_main_and_secondary_trees(t_decoder_context *context);
bool read_aligned_offset_tree(t_decoder_context *context);
bool make_table(t_decoder_context *context, int nchar, const byte *bitlen, byte tablebits, short *table, short *leftright);
bool make_table_8bit(t_decoder_context *context, byte *bitlen, byte *table);

/* Inline bit reads and Huffman decodes over the local copies of the bit buffer, bit count and input pointer */
#define FILL_BUF_NOEOFCHECK(N) \
{ \
    dec_bitbuf <<= (N); \
    dec_bitcount -= (N); \
    if (dec_bitcount <= 0) \
    { \
        dec_bitbuf |= ((((ulong) *dec_input_curpos | (((ulong) *(dec_input_curpos + 1)) << 8))) << (-dec_bitcount)); \
        dec_input_curpos += 2; \
        dec_bitcount += 16; \
    } \
}

#define FILL_BUF_FULLCHECK(N) \
{ \
    if (dec_input_curpos >= dec_end_input_pos) \
        return -1; \
    FILL_BUF_NOEOFCHECK(N) \
}

#define FILL_BUF17_NOEOFCHECK(N) \
{ \
    dec_bitbuf <<= (N); \
    dec_bitcount -= (N); \
    if (dec_bitcount <= 0) \
    { \
        dec_bitbuf |= ((((ulong) *dec_input_curpos | (((ulong) *(dec_input_curpos + 1)) << 8))) << (-dec_bitcount)); \
        dec_input_curpos += 2; \
        dec_bitcount += 16; \
        if (dec_bitcount <= 0) \
        { \
            dec_bitbuf |= ((((ulong) *dec_input_curpos | (((ulong) *(dec_input_curpos + 1)) << 8))) << (-dec_bitcount)); \
            dec_input_curpos += 2; \
            dec_bitcount += 16; \
        } \
    } \
}

#define GET_BITS_NOEOFCHECK(N, DEST_VAR) \
{ \
    DEST_VAR = dec_bitbuf >> (32 - (N)); \
    FILL_BUF_NOEOFCHECK((N)); \
}

#define GET_BITS17_NOEOFCHECK(N, DEST_VAR) \
{ \
    DEST_VAR = dec_bitbuf >> (32 - (N)); \
    FILL_BUF17_NOEOFCHECK((N)); \
}

#define DECODE_MAIN_TREE(j) \
    j = context->dec_main_tree_table[dec_bitbuf >> (32 - MAIN_TREE_TABLE_BITS)]; \
    if (j < 0) \
    { \
        ulong mask = (1L << (32 - 1 - MAIN_TREE_TABLE_BITS)); \
        do \
        { \
            j = -j; \
            if (dec_bitbuf & mask) \
                j = context->dec_main_tree_left_right[j * 2 + 1]; \
            else \
                j = context->dec_main_tree_left_right[j * 2]; \
            mask >>= 1; \
        } while (j < 0); \
    } \
    FILL_BUF_FULLCHECK(context->dec_main_tree_len[j]);

#define DECODE_LEN_TREE_NOEOFCHECK(matchlen) \
    matchlen = context->dec_secondary_length_tree_table[dec_bitbuf >> (32 - SECONDARY_LEN_TREE_TABLE_BITS)]; \
    if (matchlen < 0) \
    { \
        ulong mask = (1L << (32 - 1 - SECONDARY_LEN_TREE_TABLE_BITS)); \
        do \
        { \
            matchlen = -matchlen; \
            if (dec_bitbuf & mask) \
                matchlen = context->dec_secondary_length_tree_left_right[matchlen * 2 + 1]; \
            else \
                matchlen = context->dec_secondary_length_tree_left_right[matchlen * 2]; \
            mask >>= 1; \
        } while (matchlen < 0); \
    } \
    FILL_BUF_NOEOFCHECK(context->dec_secondary_length_tree_len[matchlen]); \
    matchlen += NUM_PRIMARY_LENGTHS;

#define DECODE_ALIGNED_NOEOFCHECK(j) \
    (j) = context->dec_aligned_table[dec_bitbuf >> (32 - ALIGNED_TABLE_BITS)]; \
    FILL_BUF_NOEOFCHECK(context->dec_aligned_len[(j)]);

#endif
