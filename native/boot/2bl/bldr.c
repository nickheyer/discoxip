#include "bldr.h"

#if BLDR_BUILD >= 5713
// Decrypts the shadowed loader region with the region key and copies the first stage loader beside the image
VOID NTAPI BldrPrepareReentry(VOID)
{
    BldrRc4Crypt((PUCHAR)BLDR_FLASH_BASE, BLDR_IMAGE_SIZE, BldrRegionKey, 16);
    memcpy((PVOID)BLDR_PRELDR_COPY, (PVOID)BLDR_PRELDR_BASE, BLDR_PRELDR_SIZE);
}
#endif

// Computes the two SMC response bytes from the four challenge bytes the SMC reported
VOID NTAPI BldrSmcChallengeResponse(UCHAR Challenge0, UCHAR Challenge1, UCHAR Challenge2, UCHAR Challenge3, PUCHAR Response0, PUCHAR Response1)
{
    ULONG Round;

    *Response0 = 0x33;
    *Response1 = 0xED;
    for (Round = 0; Round < 4; Round++) {
        *Response0 += *Response1 ^ ((Challenge3 + 0x63) ^ (Challenge1 + 0x39) ^ (Challenge0 << 2) ^ (Challenge2 >> 2));
        *Response1 += ((*Response0) ^ ((Challenge0 + 0x0B) ^ (Challenge2 + 0x1B) ^ (Challenge1 >> 2)));
    }
}

// Answers the SMC challenge, halting when the SMC offers none, then unlocks the MCPX
VOID NTAPI BldrSmcHandshake(VOID)
{
    ULONG DataValue;
    UCHAR Challenge0;
    UCHAR Challenge1;
    UCHAR Challenge2;
    UCHAR Challenge3;
    UCHAR Response0;
    UCHAR Response1;

    DataValue = 0;
    HalReadSMBusValue(SMC_SLAVE_ADDRESS, SMC_COMMAND_CHALLENGE_0, FALSE, &DataValue);
    Challenge0 = (UCHAR)DataValue;
    HalReadSMBusValue(SMC_SLAVE_ADDRESS, SMC_COMMAND_CHALLENGE_1, FALSE, &DataValue);
    Challenge1 = (UCHAR)DataValue;
    HalReadSMBusValue(SMC_SLAVE_ADDRESS, SMC_COMMAND_CHALLENGE_2, FALSE, &DataValue);
    Challenge2 = (UCHAR)DataValue;
    HalReadSMBusValue(SMC_SLAVE_ADDRESS, SMC_COMMAND_CHALLENGE_3, FALSE, &DataValue);
    Challenge3 = (UCHAR)DataValue;
    if (Challenge0 == 0 && Challenge1 == 0 && Challenge2 == 0 && Challenge3 == 0) {
        BldrHalt();
    }
    BldrSmcChallengeResponse(Challenge0, Challenge1, Challenge2, Challenge3, &Response0, &Response1);
    HalWriteSMBusValue(SMC_SLAVE_ADDRESS, SMC_COMMAND_RESPONSE_0, FALSE, Response0);
    HalWriteSMBusValue(SMC_SLAVE_ADDRESS, SMC_COMMAND_RESPONSE_1, FALSE, Response1);
    HalWriteSMBusValue(SMC_SLAVE_ADDRESS, SMC_COMMAND_RESET_ON_EJECT, FALSE, 0);
    _outpd(PCI_TYPE1_ADDR_PORT, PCI_CONFIG_ADDRESS(0, 1, 0, 0x8C));
    _outpd(PCI_TYPE1_DATA_PORT, 0x40000000);
}

#if BLDR_BUILD >= 5713
// Writes 8F00h to the MCPX bridge register C8h on bridge revisions D1h and later
VOID NTAPI BldrUpdateMcpx(VOID)
{
    _outpd(PCI_TYPE1_ADDR_PORT, PCI_CONFIG_ADDRESS(0, 1, 0, 0x08));
    if ((UCHAR)_inp(PCI_TYPE1_DATA_PORT) >= 0xD1) {
        _outpd(PCI_TYPE1_ADDR_PORT, PCI_CONFIG_ADDRESS(0, 1, 0, 0xC8));
        _outpd(PCI_TYPE1_DATA_PORT, 0x8F00);
    }
}
#endif

// Brings up the SMC, memory and PCI, loads the kernel image and enters it with the load parameters and keys
VOID NTAPI BldrMain(VOID)
{
    PIMAGE_NT_HEADERS NtHeaders;

    _outpd(PCI_TYPE1_ADDR_PORT, PCI_CONFIG_ADDRESS(0, 1, 0, 0x80));
    _outp(PCI_TYPE1_DATA_PORT, 2);
    BldrSmcHandshake();
    memset((PVOID)BLDR_LOAD_VIRTUAL, 0xCC, BLDR_IMAGE_SIZE);
    BldrInitializeMemory((PUCHAR)BLDR_NV2A_BASE);
    BldrInitializePci();
#if BLDR_BUILD >= 5713
    BldrUpdateMcpx();
#endif
    BldrLoadKernel(BldrKernelKey);
    NtHeaders = RtlImageNtHeader((PVOID)BLDR_KERNEL_BASE);
    if (NtHeaders == NULL) {
        BldrHalt();
    }
    ((PKERNEL_ENTRY)(BLDR_KERNEL_BASE + NtHeaders->OptionalHeader.AddressOfEntryPoint))((PVOID)BLDR_VIRTUAL(BldrLoadParameters), BldrKeys);
}
