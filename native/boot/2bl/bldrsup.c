#include "bldr.h"

ULONG BldrShadowRomEnd = 0;
ULONG BldrHeapUsed = 0;
#if BLDR_BUILD >= 5713
PFBL_CRYPTO_TABLE BldrCryptoTable = NULL;
#endif

// Hands out the next block of the heap that follows the loader image
PVOID __cdecl BldrAllocate(ULONG Size)
{
    PVOID Buffer;

    Buffer = (PVOID)(BLDR_HEAP_BASE + BldrHeapUsed);
    BldrHeapUsed += Size;
    return Buffer;
}

// Releases nothing since the heap only grows
VOID __cdecl BldrFree(PVOID Buffer)
{
}

// Expands the framed LZX blocks of a compressed image into the destination
BOOLEAN NTAPI BldrDecompress(PUCHAR Source, PUCHAR Destination, ULONG SourceSize)
{
    UINT MaxBlockSize;
    LZXDECOMPRESS Configuration;
    UINT SourceBufferMin;
    LDI_CONTEXT_HANDLE Handle;
    UINT BytesOut;
    BOOLEAN Result;
    PBLDR_LZX_BLOCK Block;
    PUCHAR Data;

    Result = FALSE;
    MaxBlockSize = 0x8000;
    Configuration.WindowSize = 0x20000;
    Configuration.fCPUtype = 1;
    if (LDICreateDecompression(&MaxBlockSize, &Configuration, BldrAllocate, BldrFree, &SourceBufferMin, &Handle, NULL, NULL, NULL, NULL, NULL) != MDI_ERROR_NO_ERROR) {
        return FALSE;
    }
    Block = (PBLDR_LZX_BLOCK)Source;
    Data = Source + sizeof(BLDR_LZX_BLOCK);
    BytesOut = Block->UncompressedSize;
    if (LDIDecompress(Handle, Data, Block->CompressedSize, Destination, &BytesOut) == MDI_ERROR_NO_ERROR) {
        for (;;) {
            Destination += Block->UncompressedSize;
            Data += Block->CompressedSize;
            if ((ULONG)(Data - Source) >= SourceSize) {
                Result = TRUE;
                break;
            }
            Block = (PBLDR_LZX_BLOCK)Data;
            BytesOut = Block->UncompressedSize;
            Data += sizeof(BLDR_LZX_BLOCK);
            if (LDIDecompress(Handle, Data, Block->CompressedSize, Destination, &BytesOut) != MDI_ERROR_NO_ERROR) {
                break;
            }
        }
    }
    LDIDestroyDecompression(Handle);
    return Result;
}

// Resets the system through the MCPX and stops the processor
VOID NTAPI BldrHalt(VOID)
{
    _outpd(PCI_TYPE1_ADDR_PORT, PCI_CONFIG_ADDRESS(0, 0, 3, 0x6C));
    _outpd(PCI_TYPE1_DATA_PORT, 0x01000000);
    __asm hlt
}

// Encrypts or decrypts a buffer in place with RC4 under the given key
VOID NTAPI BldrRc4Crypt(PUCHAR Buffer, ULONG Length, PUCHAR Key, ULONG KeyLength)
{
    RC4_KEYSTRUCT KeyStruct;

    rc4_key(&KeyStruct, KeyLength, Key);
    rc4(&KeyStruct, Length, Buffer);
}

#if BLDR_BUILD >= 5713
// Copies the kernel from flash hashing it page by page, checks the digest, decrypts and expands it
VOID NTAPI BldrLoadKernel(PUCHAR Key)
{
    PBLDR_BOOT_PARAMETERS Parameters;
    A_SHA_CTX ShaContext;
    UCHAR Digest[A_SHA_DIGEST_LEN];
    ULONG Count;
    PUCHAR Buffer;
    PUCHAR Source;
    PUCHAR Destination;
    ULONG Remaining;
    PUCHAR PageEnd;

    Parameters = BldrBootParameters;
    Source = (PUCHAR)(BLDR_FLASH_BASE - Parameters->KernelSize - Parameters->KernelDataSize);
    Buffer = BldrAllocate(Parameters->KernelSize);
    Remaining = Parameters->KernelSize + Parameters->KernelDataSize;
    Destination = Buffer;
    PageEnd = (PUCHAR)(((ULONG)Source + BLDR_PAGE_SIZE) & ~(BLDR_PAGE_SIZE - 1));
    BldrCryptoTable = BLDR_PRELDR_CRYPTO_TABLE;
    BldrCryptoTable->ShaInit(&ShaContext);
    while (Remaining != 0) {
        if ((ULONG)(PageEnd - Source) > Remaining) {
            PageEnd = Source + Remaining;
        }
        Count = PageEnd - Source;
        memcpy(Destination, Source, Count);
        BldrCryptoTable->ShaUpdate(&ShaContext, Destination, Count);
        Destination += Count;
        Remaining -= PageEnd - Source;
        Source = PageEnd;
        PageEnd += BLDR_PAGE_SIZE;
    }
    BldrCryptoTable->ShaFinal(&ShaContext, Digest);
    if (memcmp(Digest, Parameters->Digest, A_SHA_DIGEST_LEN) != 0) {
        BldrHalt();
    }
    BldrRc4Crypt(Buffer, Parameters->KernelSize, Key, 16);
    BldrDecompress(Buffer, (PUCHAR)BLDR_KERNEL_BASE, Parameters->KernelSize);
    *(PULONG)BLDR_KERNEL_RELOCATION += BldrShadowRomEnd;
}
#else
// Checks the keyed digest of kernel, data and init table in flash, then decrypts and expands the kernel
VOID NTAPI BldrLoadKernel(PUCHAR Key)
{
    PBLDR_BOOT_PARAMETERS Parameters;
    A_SHA_CTX ShaContext;
    UCHAR Digest[A_SHA_DIGEST_LEN];
    PUCHAR Buffer;
    PUCHAR KernelData;
    PUCHAR Kernel;

    Parameters = BldrBootParameters;
    KernelData = (PUCHAR)(BLDR_FLASH_BASE - Parameters->KernelDataSize);
    Kernel = KernelData - Parameters->KernelSize;
    A_SHAInit(&ShaContext);
    A_SHAUpdate(&ShaContext, Key, 16);
    A_SHAUpdate(&ShaContext, (PUCHAR)&Parameters->KernelSize, sizeof(ULONG));
    A_SHAUpdate(&ShaContext, Kernel, Parameters->KernelSize);
    A_SHAUpdate(&ShaContext, (PUCHAR)&Parameters->KernelDataSize, sizeof(ULONG));
    A_SHAUpdate(&ShaContext, KernelData, Parameters->KernelDataSize);
    A_SHAUpdate(&ShaContext, (PUCHAR)&Parameters->InitTableSize, sizeof(ULONG));
    A_SHAUpdate(&ShaContext, (PUCHAR)BLDR_FLASH_INIT_TABLE, Parameters->InitTableSize);
    A_SHAFinal(&ShaContext, Digest);
    A_SHAInit(&ShaContext);
    A_SHAUpdate(&ShaContext, Key, 16);
    A_SHAUpdate(&ShaContext, Digest, A_SHA_DIGEST_LEN);
    A_SHAFinal(&ShaContext, Digest);
    if (memcmp(Digest, Parameters->Digest, A_SHA_DIGEST_LEN) != 0) {
        BldrHalt();
    }
    Buffer = BldrAllocate(Parameters->KernelSize);
    memcpy(Buffer, Kernel, Parameters->KernelSize);
    BldrRc4Crypt(Buffer, Parameters->KernelSize, Key, 16);
    BldrDecompress(Buffer, (PUCHAR)BLDR_KERNEL_BASE, Parameters->KernelSize);
    *(PULONG)BLDR_KERNEL_RELOCATION += BldrShadowRomEnd;
}
#endif

// Spins on the time stamp counter until the given number of cycles has passed
VOID NTAPI BldrStallCycles(ULONG Cycles)
{
    ULARGE_INTEGER Now;
    ULONG EndLow;
    ULONG EndHigh;

    __asm {
        rdtsc
        mov Now.LowPart, eax
        mov Now.HighPart, edx
    }
    EndLow = Now.LowPart + Cycles;
    EndHigh = Now.HighPart;
    if (EndLow < Now.LowPart) {
        EndHigh++;
    }
    do {
        __asm {
            rdtsc
            mov Now.LowPart, eax
            mov Now.HighPart, edx
        }
    } while (Now.HighPart <= EndHigh && Now.LowPart < EndLow);
}

// Packs two nibbles into eight alternating nibbles with the second one on top
static ULONG BldrPackNibbles(ULONG High, ULONG Low)
{
    return (((((((High << 4 | Low) << 4 | High) << 4 | Low) << 4 | High) << 4 | Low) << 4 | High) << 4) | Low;
}

// Rounds a 20 bit fixed point value to the nearest integer and keeps its low nibble
static ULONG BldrRoundNibble(ULONG Value)
{
    return ((Value + BLDR_FIXED_HALF) >> BLDR_FIXED_SHIFT) & 0xF;
}

#if BLDR_BUILD >= 5713
// Reads the four timing counters the NV2A reports for consecutive test patterns
VOID NTAPI BldrSampleTiming(ULONG Pattern, PULONG Samples)
{
    ULONG Index;

    for (Index = 0; Index < 4; Index++) {
        *(volatile ULONG *)(BLDR_NV2A_BASE + NV_PFB_TIMING_PATTERN) = Pattern + Index;
        BldrStallCycles(10000);
        Samples[Index] = *(volatile ULONG *)(BLDR_NV2A_BASE + NV_PFB_TIMING_RESULT);
    }
}

// Averages whichever sample set varies less between neighbouring samples
ULONG NTAPI BldrTimingScore(PULONG First, PULONG Second)
{
    ULONG Index;
    ULONG FirstSpread;
    ULONG SecondSpread;
    ULONG Sum;
    PULONG Samples;

    FirstSpread = 0;
    SecondSpread = 0;
    Sum = 0;
    for (Index = 0; Index < 3; Index++) {
        FirstSpread += abs(First[Index] - First[Index + 1]);
        SecondSpread += abs(Second[Index] - Second[Index + 1]);
    }
    Samples = First;
    if (FirstSpread >= SecondSpread) {
        Samples = Second;
    }
    for (Index = 0; Index < 4; Index++) {
        Sum += Samples[Index];
    }
    return Sum / 4;
}
#endif

// Measures the memory timing, interpolates pad settings from the flash table rows and programs the NV2A
VOID NTAPI BldrInitializeMemory(PUCHAR NvBase)
{
    PUCHAR Table;
    PUCHAR Set;
    PUCHAR Low;
    PUCHAR High;
    ULONG Fraction;
    ULONG Score1;
    ULONG Score2;
    ULONG Index;
    ULONG Pass;
    BLDR_TIMING_REGISTER Registers[9];
    ULONG Value0, Value1, Value2, Value3, Value4, Value5, Value6, Value7;
    ULONG Value8, Value9, Value10, Value11, Value12, Value13, Value14, Value15;
    ULONG Bias0, Bias1, Bias2;
#if BLDR_BUILD >= 5713
    ULONG FirstHigh[4];
    ULONG FirstLow[4];
    ULONG SecondHigh[4];
    ULONG SecondLow[4];
#else
    ULONG Sample0, Sample1, Sample2, Sample3;
#endif

#if BLDR_BUILD >= 5713
    Table = (PUCHAR)(*(PULONG)BLDR_FLASH_DATA_TABLE - BLDR_FLASH_DATA_BIAS);
    switch ((*(PULONG)(NvBase + NV_PEXTDEV_BOOT_0) >> 25) & 3) {
    case 0:
        Set = Table + 0x6B;
        break;
    case 1:
        Set = Table + 0xCA;
        break;
    default:
        Set = Table + 0x0C;
        break;
    }
    *(volatile ULONG *)(NvBase + NV_PFB_TIMING_CONTROL) = 0x000F0F0F;
    BldrSampleTiming(0x40004, FirstHigh);
    BldrSampleTiming(0x40000, FirstLow);
    BldrSampleTiming(0x40004, SecondHigh);
    BldrSampleTiming(0x40000, SecondLow);
    *(volatile ULONG *)(NvBase + NV_PFB_TIMING_CONTROL) = 0;
    Score1 = BldrTimingScore(FirstHigh, SecondHigh);
    Score2 = BldrTimingScore(FirstLow, SecondLow);
#else
    Table = (PUCHAR)(*(PULONG)BLDR_FLASH_DATA_TABLE - BLDR_FLASH_DATA_BIAS);
    if (*(PULONG)(NvBase + NV_PEXTDEV_BOOT_0) & 0x000C0000) {
        Set = Table + 0x0C;
    } else {
        Set = Table + 0x6B;
    }
    *(volatile ULONG *)(NvBase + NV_PFB_TIMING_CONTROL) = 0x000F0F0F;
    *(volatile ULONG *)(NvBase + NV_PFB_TIMING_PATTERN) = 0x40004;
    BldrStallCycles(10000);
    Sample0 = *(volatile ULONG *)(NvBase + NV_PFB_TIMING_RESULT);
    *(volatile ULONG *)(NvBase + NV_PFB_TIMING_PATTERN) = 0x40005;
    BldrStallCycles(10000);
    Sample1 = *(volatile ULONG *)(NvBase + NV_PFB_TIMING_RESULT);
    *(volatile ULONG *)(NvBase + NV_PFB_TIMING_PATTERN) = 0x40006;
    BldrStallCycles(10000);
    Sample2 = *(volatile ULONG *)(NvBase + NV_PFB_TIMING_RESULT);
    *(volatile ULONG *)(NvBase + NV_PFB_TIMING_PATTERN) = 0x40007;
    BldrStallCycles(10000);
    Sample3 = *(volatile ULONG *)(NvBase + NV_PFB_TIMING_RESULT);
    Score1 = (Sample0 + Sample1 + Sample2 + Sample3) / 4;
    *(volatile ULONG *)(NvBase + NV_PFB_TIMING_PATTERN) = 0x40000;
    BldrStallCycles(10000);
    Sample0 = *(volatile ULONG *)(NvBase + NV_PFB_TIMING_RESULT);
    *(volatile ULONG *)(NvBase + NV_PFB_TIMING_PATTERN) = 0x40001;
    BldrStallCycles(10000);
    Sample1 = *(volatile ULONG *)(NvBase + NV_PFB_TIMING_RESULT);
    *(volatile ULONG *)(NvBase + NV_PFB_TIMING_PATTERN) = 0x40002;
    BldrStallCycles(10000);
    Sample2 = *(volatile ULONG *)(NvBase + NV_PFB_TIMING_RESULT);
    *(volatile ULONG *)(NvBase + NV_PFB_TIMING_PATTERN) = 0x40003;
    BldrStallCycles(10000);
    Sample3 = *(volatile ULONG *)(NvBase + NV_PFB_TIMING_RESULT);
    *(volatile ULONG *)(NvBase + NV_PFB_TIMING_CONTROL) = 0;
    Score2 = (Sample0 + Sample1 + Sample2 + Sample3) / 4;
#endif

    if (Score1 <= Table[4]) {
#if BLDR_BUILD >= 5713
        Fraction = (LONG)((Score1 - Table[2]) << BLDR_FIXED_SHIFT) / (LONG)(Table[4] - Table[2]);
#else
        Fraction = ((Score1 - Table[2]) << BLDR_FIXED_SHIFT) / (Table[4] - Table[2]);
#endif
        High = Set + 4 * BLDR_TIMING_ROW_SIZE;
        Low = Set + 3 * BLDR_TIMING_ROW_SIZE;
        if ((LONG)Fraction < 0) {
            Fraction = 0;
        }
    } else if (Score1 >= Table[8]) {
        Fraction = ((Score1 - Table[8]) << BLDR_FIXED_SHIFT) / (Table[10] - Table[8]);
        High = Set + 1 * BLDR_TIMING_ROW_SIZE;
        Low = Set;
        if (Fraction > BLDR_FIXED_ONE) {
            Fraction = BLDR_FIXED_CLAMP;
        }
    } else if (Score1 < Table[6]) {
        Fraction = ((Score1 - Table[4]) << BLDR_FIXED_SHIFT) / (Table[6] - Table[4]);
        High = Set + 3 * BLDR_TIMING_ROW_SIZE;
        Low = Set + 2 * BLDR_TIMING_ROW_SIZE;
    } else {
        Fraction = ((Score1 - Table[6]) << BLDR_FIXED_SHIFT) / (Table[8] - Table[6]);
        High = Set + 2 * BLDR_TIMING_ROW_SIZE;
        Low = Set + 1 * BLDR_TIMING_ROW_SIZE;
    }
    Value0 = BldrRoundNibble(Low[0] * Fraction + High[0] * (BLDR_FIXED_ONE - Fraction));
    Value1 = BldrRoundNibble(Low[1] * Fraction + High[1] * (BLDR_FIXED_ONE - Fraction));
    Value2 = BldrRoundNibble(Low[2] * Fraction + High[2] * (BLDR_FIXED_ONE - Fraction));
    Value3 = BldrRoundNibble(Low[3] * Fraction + High[3] * (BLDR_FIXED_ONE - Fraction));
    Value4 = BldrRoundNibble(Low[4] * Fraction + High[4] * (BLDR_FIXED_ONE - Fraction));
    Value5 = BldrRoundNibble(Low[5] * Fraction + High[5] * (BLDR_FIXED_ONE - Fraction));
    Value6 = BldrRoundNibble(Low[6] * Fraction + High[6] * (BLDR_FIXED_ONE - Fraction));
    Value7 = BldrRoundNibble(Low[7] * Fraction + High[7] * (BLDR_FIXED_ONE - Fraction));
    Value8 = BldrRoundNibble(Low[8] * Fraction + High[8] * (BLDR_FIXED_ONE - Fraction));
    Value9 = BldrRoundNibble(Low[9] * Fraction + High[9] * (BLDR_FIXED_ONE - Fraction));
    Value10 = BldrRoundNibble(Low[10] * Fraction + High[10] * (BLDR_FIXED_ONE - Fraction));
    Value11 = BldrRoundNibble(Low[11] * Fraction + High[11] * (BLDR_FIXED_ONE - Fraction));
    Value12 = BldrRoundNibble(Low[12] * Fraction + High[12] * (BLDR_FIXED_ONE - Fraction));
    Value13 = BldrRoundNibble(Low[13] * Fraction + High[13] * (BLDR_FIXED_ONE - Fraction));
    Value14 = BldrRoundNibble(Low[14] * Fraction + High[14] * (BLDR_FIXED_ONE - Fraction));
    Value15 = BldrRoundNibble(Low[15] * Fraction + High[15] * (BLDR_FIXED_ONE - Fraction));

    if (Score2 <= Table[5]) {
#if BLDR_BUILD >= 5713
        Fraction = (LONG)((Score2 - Table[3]) << BLDR_FIXED_SHIFT) / (LONG)(Table[5] - Table[3]);
#else
        Fraction = ((Score2 - Table[3]) << BLDR_FIXED_SHIFT) / (Table[5] - Table[3]);
#endif
        High = Set + 4 * BLDR_TIMING_ROW_SIZE;
        Low = Set + 3 * BLDR_TIMING_ROW_SIZE;
        if ((LONG)Fraction < 0) {
            Fraction = 0;
        }
    } else if (Score2 >= Table[9]) {
        Fraction = ((Score2 - Table[9]) << BLDR_FIXED_SHIFT) / (Table[11] - Table[9]);
        High = Set + 1 * BLDR_TIMING_ROW_SIZE;
        Low = Set;
        if (Fraction > BLDR_FIXED_ONE) {
            Fraction = BLDR_FIXED_CLAMP;
        }
    } else if (Score2 < Table[7]) {
        Fraction = ((Score2 - Table[5]) << BLDR_FIXED_SHIFT) / (Table[7] - Table[5]);
        High = Set + 3 * BLDR_TIMING_ROW_SIZE;
        Low = Set + 2 * BLDR_TIMING_ROW_SIZE;
    } else {
        Fraction = ((Score2 - Table[7]) << BLDR_FIXED_SHIFT) / (Table[9] - Table[7]);
        High = Set + 2 * BLDR_TIMING_ROW_SIZE;
        Low = Set + 1 * BLDR_TIMING_ROW_SIZE;
    }
    Bias2 = BldrRoundNibble(High[18] * (BLDR_FIXED_ONE - Fraction) + Low[18] * Fraction);
    Bias0 = BldrRoundNibble(High[16] * (BLDR_FIXED_ONE - Fraction) + Low[16] * Fraction);
    Bias1 = BldrRoundNibble(High[17] * (BLDR_FIXED_ONE - Fraction) + Low[17] * Fraction);

    __asm wbinvd

    Registers[0].Offset = NV_PFB_PAD_0;
    Registers[0].Value = BldrPackNibbles(Value1, Value0);
    Registers[1].Offset = NV_PFB_PAD_4;
    Registers[1].Value = BldrPackNibbles(Value3, Value2);
    Registers[2].Offset = NV_PFB_PAD_1;
    Registers[2].Value = BldrPackNibbles(Value5, Value4);
    Registers[3].Offset = NV_PFB_PAD_5;
    Registers[3].Value = BldrPackNibbles(Value7, Value6);
    Registers[4].Offset = NV_PFB_PAD_2;
    Registers[4].Value = BldrPackNibbles(Value9, Value8);
    Registers[5].Offset = NV_PFB_PAD_6;
    Registers[5].Value = BldrPackNibbles(Value11, Value10);
    Registers[6].Offset = NV_PFB_PAD_3;
    Registers[6].Value = BldrPackNibbles(Value13, Value12);
    Registers[7].Offset = NV_PFB_PAD_7;
    Registers[7].Value = BldrPackNibbles(Value15, Value14);
    Registers[8].Offset = NV_PFB_PAD_BIAS;
    Registers[8].Value = (((((((((((Bias2 << 2 | Bias1) << 3 | Bias0) << 3 | Bias2) << 2 | Bias1) << 3 | Bias0) << 3 | Bias2) << 2 | Bias1) << 3 | Bias0) << 3 | Bias2) << 2 | Bias1) << 3) | Bias0;

    for (Index = 0; Index < 9; Index++) {
#if BLDR_BUILD >= 5713
        if (Registers[Index].Offset == 0 && Registers[Index].Value == 0) {
#else
        if (Registers[Index].Offset == 0 || Registers[Index].Value == 0) {
#endif
            Registers[Index].Offset = 0;
        }
    }
    for (Pass = 0; Pass < 2; Pass++) {
        for (Index = 0; Index < 9; Index++) {
            if (Pass == 1) {
                *(volatile ULONG *)(NvBase + Registers[Index].Offset) = Registers[Index].Value;
            }
        }
    }
}

// Enables the MCPX and host bridge decode bits, pulses host bridge register 6Ch and sets register 80h
VOID NTAPI BldrInitializePci(VOID)
{
    ULONG Value;

    _outpd(PCI_TYPE1_ADDR_PORT, PCI_CONFIG_ADDRESS(0, 1, 0, 0x54));
    _outpd(PCI_TYPE1_DATA_PORT, _inpd(PCI_TYPE1_DATA_PORT) | 0x88000000);
    _outpd(PCI_TYPE1_ADDR_PORT, PCI_CONFIG_ADDRESS(0, 0, 0, 0x64));
    _outpd(PCI_TYPE1_DATA_PORT, _inpd(PCI_TYPE1_DATA_PORT) | 0x88000000);
    _outpd(PCI_TYPE1_ADDR_PORT, PCI_CONFIG_ADDRESS(0, 0, 0, 0x6C));
    Value = _inpd(PCI_TYPE1_DATA_PORT);
    _outpd(PCI_TYPE1_DATA_PORT, Value & ~1);
    _outpd(PCI_TYPE1_DATA_PORT, Value);
    _outpd(PCI_TYPE1_ADDR_PORT, PCI_CONFIG_ADDRESS(0, 0, 0, 0x80));
    _outpd(PCI_TYPE1_DATA_PORT, 0x100);
}
