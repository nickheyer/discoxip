#ifndef LZXCONST_H
#define LZXCONST_H

#include "lzx.h"

#define MIN_MATCH                       2
#define MAX_MATCH                       (MIN_MATCH + 255)
#define NUM_CHARS                       256
#define NUM_PRIMARY_LENGTHS             7
#define NUM_SECONDARY_LENGTHS           ((MAX_MATCH - MIN_MATCH + 1) - NUM_PRIMARY_LENGTHS)
#define NL_SHIFT                        3
#define NUM_REPEATED_OFFSETS            3
#define MAX_POSITION_SLOTS              52
#define MAX_MAIN_TREE_ELEMENTS          (NUM_CHARS + (MAX_POSITION_SLOTS << NL_SHIFT))

#define MAIN_TREE_TABLE_BITS            10
#define SECONDARY_LEN_TREE_TABLE_BITS   8
#define ALIGNED_TABLE_BITS              7
#define ALIGNED_NUM_ELEMENTS            8

#define NUM_DECODE_SMALL                20
#define DS_TABLE_BITS                   8

#define TREE_ENC_REP_MIN                4
#define TREE_ENC_REP_ZERO_FIRST         16
#define TREE_ENC_REPZ_FIRST_EXTRA_BITS  4
#define TREE_ENC_REPZ_SECOND_EXTRA_BITS 5
#define TREE_ENC_REP_SAME_EXTRA_BITS    1

#define MAX_GROWTH                      6144
#define E8_CFDATA_FRAME_THRESHOLD       32768

extern const byte dec_extra_bits[];
extern const long MP_POS_minus2[];
extern const byte dec_modulo17_lookup[];

#endif
