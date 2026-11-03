#include "sha.h"
#include "rc4.h"
#include "rsa.h"

#define VOID void
typedef unsigned char UCHAR, *PUCHAR, BOOLEAN;
typedef unsigned long ULONG, *PULONG;
typedef void *PVOID;

#define NTAPI __stdcall

int __cdecl memcmp(const void *Buffer1, const void *Buffer2, unsigned int Count);
#pragma intrinsic(memcpy, memset, memcmp)

#define FBL_MCPX_SECRET ((PUCHAR)0xFFFFFF9C)
#define FBL_KEY_BLOB_SECRET_LENGTH 12
#define FBL_KEY_BLOB_SIZE 0x11C
#define FBL_SECRET_LENGTH 16
#define FblHeader ((PFBL_HEADER)0xFFFFFD80)
#define FBL_HEADER_HASH_LENGTH 0x80
#define FBL_SIGNATURE ((PUCHAR)0xFFFFFC80)
#define FBL_SIGNATURE_LENGTH 0x100
#define FBL_NONCE ((PUCHAR)0xFFFFFDF0)
#define FBL_INIT_TABLE_MASK 0xFFFC0000
#define FBL_INIT_TABLE_BASE 0xFFF00000
#define FBL_BLDR_FLASH ((PUCHAR)0xFFFF9E00)
#define FBL_BLDR_SIZE 0x6000
#define FBL_BLDR_ENTRY_RECORD 0x35F8
#define FBL_HMAC_OPAD 0x5C

typedef struct _FBL_HEADER {
    ULONG InitTable;
    ULONG InitTableSize;
    ULONG BldrStart;
} FBL_HEADER, *PFBL_HEADER;

extern UCHAR FblKeyBlob[FBL_KEY_BLOB_SIZE];
extern ULONG FblHeapTop;
extern PUCHAR FblDigestInfos[];

// Carves 32 byte aligned blocks downward from the heap top for the RSA library
VOID * NTAPI RSA32Alloc(ULONG Size)
{
    FblHeapTop -= (Size + 31) & ~31;
    return (VOID *)FblHeapTop;
}

// Releases nothing since the heap only shrinks downward
VOID NTAPI RSA32Free(VOID *Buffer)
{
}

// Checks a PKCS 1 signature over a SHA1 digest under the public key, accepting any listed DigestInfo prefix
BOOLEAN NTAPI FblVerifySignature(LPBSAFE_PUB_KEY Key, PUCHAR Signature, PUCHAR Digest)
{
    UCHAR Input[FBL_SIGNATURE_LENGTH + 8];
    UCHAR Output[FBL_SIGNATURE_LENGTH + 8];
    UCHAR Reversed[A_SHA_DIGEST_LEN];
    PUCHAR *DigestInfo;
    ULONG DigestInfoLength;
    ULONG PaddingStart;
    ULONG Index;

    memcpy(Input, Signature, FBL_SIGNATURE_LENGTH);
    memset(Input + FBL_SIGNATURE_LENGTH, 0, 8);
    if (!BSafeEncPublic(Key, Input, Output)) {
        return FALSE;
    }
    PaddingStart = A_SHA_DIGEST_LEN;
    for (Index = 0; Index < PaddingStart; Index++) {
        Reversed[Index] = Digest[A_SHA_DIGEST_LEN - 1 - Index];
    }
    if (memcmp(Reversed, Output, A_SHA_DIGEST_LEN) != 0) {
        return FALSE;
    }
    for (DigestInfo = FblDigestInfos; (*DigestInfo)[0] != 0; DigestInfo++) {
        DigestInfoLength = (*DigestInfo)[0];
        if (memcmp(Output + A_SHA_DIGEST_LEN, *DigestInfo + 1, DigestInfoLength) == 0) {
            PaddingStart = DigestInfoLength + A_SHA_DIGEST_LEN;
            break;
        }
    }
    if (Output[PaddingStart] != 0) {
        return FALSE;
    }
    if (Output[Key->datalen] != 0 || Output[Key->datalen - 1] != 1) {
        return FALSE;
    }
    for (Index = PaddingStart + 1; Index < Key->datalen - 1; Index++) {
        if (Output[Index] != 0xFF) {
            return FALSE;
        }
    }
    return TRUE;
}

// Verifies the signed loader region, derives its RC4 key from the MCPX secret and nonce and decrypts it into the buffer
PUCHAR NTAPI FblMain(PUCHAR LoadBuffer)
{
    RC4_KEYSTRUCT KeyStruct;
    A_SHA_CTX ShaContext;
    UCHAR KeyBlob[FBL_KEY_BLOB_SIZE];
    UCHAR Digest[A_SHA_DIGEST_LEN];
    UCHAR Key[A_SHA_DIGEST_LEN];
    PUCHAR InitTableStart;
    PUCHAR InitTableEnd;
    PUCHAR BldrStart;
    int Index;
    PFBL_HEADER Header;

    rc4_key(&KeyStruct, FBL_KEY_BLOB_SECRET_LENGTH, FBL_MCPX_SECRET);
    memcpy(KeyBlob, FblKeyBlob, FBL_KEY_BLOB_SIZE);
    rc4(&KeyStruct, FBL_KEY_BLOB_SIZE, KeyBlob);
    Header = FblHeader;
    InitTableStart = (PUCHAR)((Header->InitTable & FBL_INIT_TABLE_MASK) | FBL_INIT_TABLE_BASE);
    InitTableEnd = InitTableStart + Header->InitTableSize;
    BldrStart = (PUCHAR)Header->BldrStart;
    if (InitTableEnd < InitTableStart || BldrStart < InitTableEnd) {
        return NULL;
    }
    A_SHAInit(&ShaContext);
    A_SHAUpdate(&ShaContext, (PUCHAR)Header, FBL_HEADER_HASH_LENGTH);
    A_SHAUpdate(&ShaContext, InitTableStart, InitTableEnd - InitTableStart);
    A_SHAUpdate(&ShaContext, BldrStart, FBL_SIGNATURE - BldrStart);
    A_SHAFinal(&ShaContext, Digest);
    if (!FblVerifySignature((LPBSAFE_PUB_KEY)KeyBlob, FBL_SIGNATURE, Digest)) {
        return NULL;
    }
    A_SHAInit(&ShaContext);
    A_SHAUpdate(&ShaContext, FBL_MCPX_SECRET, FBL_SECRET_LENGTH);
    A_SHAUpdate(&ShaContext, FBL_NONCE, FBL_SECRET_LENGTH);
    for (Index = 0; Index < FBL_SECRET_LENGTH; Index++) {
        Key[Index] = FBL_MCPX_SECRET[Index] ^ FBL_HMAC_OPAD;
    }
    A_SHAUpdate(&ShaContext, Key, FBL_SECRET_LENGTH);
    A_SHAFinal(&ShaContext, Key);
    rc4_key(&KeyStruct, A_SHA_DIGEST_LEN, Key);
    memset(Key, 0, A_SHA_DIGEST_LEN);
    memcpy(LoadBuffer, FBL_BLDR_FLASH, FBL_BLDR_SIZE);
    rc4(&KeyStruct, FBL_BLDR_SIZE, LoadBuffer);
    return LoadBuffer + *(PULONG)(LoadBuffer + FBL_BLDR_ENTRY_RECORD);
}
