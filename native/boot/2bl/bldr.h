#ifndef _BLDR_
#define _BLDR_

#include "sha.h"
#include "rc4.h"
#include "ldi.h"

#define VOID void
typedef unsigned char UCHAR, *PUCHAR, BOOLEAN;
typedef unsigned short USHORT, *PUSHORT;
typedef unsigned long ULONG, *PULONG;
typedef long LONG, NTSTATUS;
typedef void *PVOID;

#define NTAPI __stdcall
#define STATUS_SUCCESS ((NTSTATUS)0x00000000L)

int __cdecl _inp(unsigned short Port);
int __cdecl _outp(unsigned short Port, int Value);
unsigned long __cdecl _inpd(unsigned short Port);
unsigned long __cdecl _outpd(unsigned short Port, unsigned long Value);
int __cdecl memcmp(const void *Buffer1, const void *Buffer2, unsigned int Count);
int __cdecl abs(int Value);
#pragma intrinsic(_inp, _outp, _inpd, _outpd, memcpy, memset, memcmp, abs)

#define PCI_TYPE1_ADDR_PORT 0x0CF8
#define PCI_TYPE1_DATA_PORT 0x0CFC
#define PCI_CONFIG_ADDRESS(Bus, Device, Function, Register) \
    (0x80000000 | ((Bus) << 16) | ((Device) << 11) | ((Function) << 8) | (Register))

#define SMC_SLAVE_ADDRESS 0x20
#define SMC_COMMAND_RESET_ON_EJECT 0x01
#define SMC_COMMAND_CHALLENGE_0 0x1C
#define SMC_COMMAND_CHALLENGE_1 0x1D
#define SMC_COMMAND_CHALLENGE_2 0x1E
#define SMC_COMMAND_CHALLENGE_3 0x1F
#define SMC_COMMAND_RESPONSE_0 0x20
#define SMC_COMMAND_RESPONSE_1 0x21

#define BLDR_VIRTUAL(Address) ((ULONG)(Address) + 0x80000000)
#define BLDR_LOAD_VIRTUAL 0x80090000
#define BLDR_IMAGE_SIZE 0x6000
#define BLDR_FLASH_BASE 0xFFFF9E00
#define BLDR_FLASH_INIT_TABLE 0xFFF00000
#define BLDR_FLASH_DATA_TABLE 0xFFC0007C
#define BLDR_FLASH_DATA_BIAS 0x00400000
#define BLDR_KERNEL_BASE 0x80010000
#define BLDR_KERNEL_RELOCATION (BLDR_KERNEL_BASE + 0x30)
#define BLDR_NV2A_BASE 0xFD000000
#define BLDR_PAGE_SIZE 0x1000
#define BLDR_TIMING_ROW_SIZE 0x13
#define BLDR_FIXED_SHIFT 20
#define BLDR_FIXED_ONE (1 << BLDR_FIXED_SHIFT)
#define BLDR_FIXED_HALF (1 << (BLDR_FIXED_SHIFT - 1))

#define NV_PEXTDEV_BOOT_0 0x101000
#define NV_PFB_PAD_BIAS 0x1214
#define NV_PFB_TIMING_RESULT 0x121C
#define NV_PFB_TIMING_CONTROL 0x1220
#define NV_PFB_TIMING_PATTERN 0x1228
#define NV_PFB_PAD_0 0x122C
#define NV_PFB_PAD_1 0x1230
#define NV_PFB_PAD_2 0x1234
#define NV_PFB_PAD_3 0x1238
#define NV_PFB_PAD_4 0x123C
#define NV_PFB_PAD_5 0x1240
#define NV_PFB_PAD_6 0x1244
#define NV_PFB_PAD_7 0x1248

#if BLDR_BUILD >= 5713
#define BLDR_HEAP_BASE 0x80410000
#define BLDR_FIXED_CLAMP BLDR_FIXED_ONE
#define BldrBootParameters ((PBLDR_BOOT_PARAMETERS)0x80405FCC)
#define BLDR_PRELDR_BASE 0xFFFFD400
#define BLDR_PRELDR_COPY 0x00408000
#define BLDR_PRELDR_SIZE 0x2DD00
#define BLDR_PRELDR_CRYPTO_TABLE (*(PFBL_CRYPTO_TABLE *)(BLDR_PRELDR_BASE + 8))
#else
#define BLDR_HEAP_BASE 0x80406000
#define BLDR_FIXED_CLAMP 1
#define BldrBootParameters ((PBLDR_BOOT_PARAMETERS)0x80405FDC)
#endif

typedef struct _BLDR_BOOT_PARAMETERS {
    ULONG KernelDataSize;
    ULONG InitTableSize;
    ULONG Signature;
    ULONG KernelSize;
    UCHAR Digest[A_SHA_DIGEST_LEN];
} BLDR_BOOT_PARAMETERS, *PBLDR_BOOT_PARAMETERS;

typedef struct _BLDR_LZX_BLOCK {
    USHORT CompressedSize;
    USHORT UncompressedSize;
} BLDR_LZX_BLOCK, *PBLDR_LZX_BLOCK;

typedef struct _BLDR_TIMING_REGISTER {
    ULONG Offset;
    ULONG Value;
} BLDR_TIMING_REGISTER, *PBLDR_TIMING_REGISTER;

typedef struct _FBL_CRYPTO_TABLE {
    VOID (NTAPI *ShaInit)(A_SHA_CTX *Context);
    VOID (NTAPI *ShaUpdate)(A_SHA_CTX *Context, PUCHAR Input, ULONG Length);
    VOID (NTAPI *ShaFinal)(A_SHA_CTX *Context, PUCHAR Digest);
} FBL_CRYPTO_TABLE, *PFBL_CRYPTO_TABLE;

typedef struct _IMAGE_FILE_HEADER {
    USHORT Machine;
    USHORT NumberOfSections;
    ULONG TimeDateStamp;
    ULONG PointerToSymbolTable;
    ULONG NumberOfSymbols;
    USHORT SizeOfOptionalHeader;
    USHORT Characteristics;
} IMAGE_FILE_HEADER, *PIMAGE_FILE_HEADER;

typedef struct _IMAGE_DATA_DIRECTORY {
    ULONG VirtualAddress;
    ULONG Size;
} IMAGE_DATA_DIRECTORY, *PIMAGE_DATA_DIRECTORY;

#define IMAGE_NUMBEROF_DIRECTORY_ENTRIES 16

typedef struct _IMAGE_OPTIONAL_HEADER {
    USHORT Magic;
    UCHAR MajorLinkerVersion;
    UCHAR MinorLinkerVersion;
    ULONG SizeOfCode;
    ULONG SizeOfInitializedData;
    ULONG SizeOfUninitializedData;
    ULONG AddressOfEntryPoint;
    ULONG BaseOfCode;
    ULONG BaseOfData;
    ULONG ImageBase;
    ULONG SectionAlignment;
    ULONG FileAlignment;
    USHORT MajorOperatingSystemVersion;
    USHORT MinorOperatingSystemVersion;
    USHORT MajorImageVersion;
    USHORT MinorImageVersion;
    USHORT MajorSubsystemVersion;
    USHORT MinorSubsystemVersion;
    ULONG Win32VersionValue;
    ULONG SizeOfImage;
    ULONG SizeOfHeaders;
    ULONG CheckSum;
    USHORT Subsystem;
    USHORT DllCharacteristics;
    ULONG SizeOfStackReserve;
    ULONG SizeOfStackCommit;
    ULONG SizeOfHeapReserve;
    ULONG SizeOfHeapCommit;
    ULONG LoaderFlags;
    ULONG NumberOfRvaAndSizes;
    IMAGE_DATA_DIRECTORY DataDirectory[IMAGE_NUMBEROF_DIRECTORY_ENTRIES];
} IMAGE_OPTIONAL_HEADER, *PIMAGE_OPTIONAL_HEADER;

typedef struct _IMAGE_NT_HEADERS {
    ULONG Signature;
    IMAGE_FILE_HEADER FileHeader;
    IMAGE_OPTIONAL_HEADER OptionalHeader;
} IMAGE_NT_HEADERS, *PIMAGE_NT_HEADERS;

typedef struct _IMAGE_DOS_HEADER {
    USHORT e_magic;
    USHORT e_cblp;
    USHORT e_cp;
    USHORT e_crlc;
    USHORT e_cparhdr;
    USHORT e_minalloc;
    USHORT e_maxalloc;
    USHORT e_ss;
    USHORT e_sp;
    USHORT e_csum;
    USHORT e_ip;
    USHORT e_cs;
    USHORT e_lfarlc;
    USHORT e_ovno;
    USHORT e_res[4];
    USHORT e_oemid;
    USHORT e_oeminfo;
    USHORT e_res2[10];
    LONG e_lfanew;
} IMAGE_DOS_HEADER, *PIMAGE_DOS_HEADER;

#define IMAGE_DOS_SIGNATURE 0x5A4D
#define IMAGE_NT_SIGNATURE 0x00004550

typedef VOID (__cdecl *PKERNEL_ENTRY)(PVOID LoadParameters, PUCHAR Keys);

PIMAGE_NT_HEADERS NTAPI RtlImageNtHeader(PVOID Base);

NTSTATUS NTAPI HalReadSMBusValue(UCHAR SlaveAddress, UCHAR CommandCode, BOOLEAN ReadWordValue, PULONG DataValue);
NTSTATUS NTAPI HalWriteSMBusValue(UCHAR SlaveAddress, UCHAR CommandCode, BOOLEAN WriteWordValue, ULONG DataValue);

extern UCHAR BldrLoadParameters[];
extern UCHAR BldrKeys[];
extern UCHAR BldrRegionKey[];
extern UCHAR BldrKernelKey[];
extern ULONG BldrShadowRomEnd;

VOID NTAPI BldrMain(VOID);
VOID NTAPI BldrHalt(VOID);
VOID NTAPI BldrSmcHandshake(VOID);
VOID NTAPI BldrSmcChallengeResponse(UCHAR Challenge0, UCHAR Challenge1, UCHAR Challenge2, UCHAR Challenge3, PUCHAR Response0, PUCHAR Response1);
VOID NTAPI BldrRc4Crypt(PUCHAR Buffer, ULONG Length, PUCHAR Key, ULONG KeyLength);
BOOLEAN NTAPI BldrDecompress(PUCHAR Source, PUCHAR Destination, ULONG SourceSize);
VOID NTAPI BldrLoadKernel(PUCHAR Key);
VOID NTAPI BldrStallCycles(ULONG Cycles);
VOID NTAPI BldrInitializeMemory(PUCHAR NvBase);
VOID NTAPI BldrInitializePci(VOID);
PVOID __cdecl BldrAllocate(ULONG Size);
VOID __cdecl BldrFree(PVOID Buffer);
#if BLDR_BUILD >= 5713
VOID NTAPI BldrPrepareReentry(VOID);
VOID NTAPI BldrUpdateMcpx(VOID);
VOID NTAPI BldrSampleTiming(ULONG Pattern, PULONG Samples);
ULONG NTAPI BldrTimingScore(PULONG First, PULONG Second);
#endif

#endif
