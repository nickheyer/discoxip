#include "decoder.h"

/* Converts the absolute targets of E8 calls back to relative offsets within the current file */
void decoder_translate_e8(t_decoder_context *context, byte *mem, long bytes)
{
    ulong end_instr_pos;
    byte temp[6];
    byte *mem_backup;

    if (bytes <= 6)
    {
        context->dec_instr_pos += bytes;
        return;
    }

    mem_backup = mem;

    memcpy(temp, &mem[bytes - 6], 6);
    memset(&mem[bytes - 6], 0xE8, 6);

    end_instr_pos = context->dec_instr_pos + bytes - 10;

    while (1)
    {
        ulong absolute;
        ulong offset;

        while (*mem++ != 0xE8)
            context->dec_instr_pos++;

        if (context->dec_instr_pos >= end_instr_pos)
            break;

        absolute = *(ulong *) mem;

        if (absolute < context->dec_current_file_size)
        {
            offset = absolute - context->dec_instr_pos;
            *(ulong *) mem = offset;
        }
        else if ((ulong) (-(long) absolute) <= context->dec_instr_pos)
        {
            offset = absolute + context->dec_current_file_size;
            *(ulong *) mem = offset;
        }

        mem += 4;
        context->dec_instr_pos += 5;
    }

    context->dec_instr_pos = end_instr_pos + 10;

    memcpy(&mem_backup[bytes - 6], temp, 6);
}
