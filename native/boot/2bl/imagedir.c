#include "bldr.h"

// Returns the NT headers of a mapped image, or NULL when the DOS or PE signature is missing
PIMAGE_NT_HEADERS NTAPI RtlImageNtHeader(PVOID Base)
{
    PIMAGE_NT_HEADERS NtHeaders;

    NtHeaders = NULL;
    if (Base != NULL && Base != (PVOID)-1) {
        if (((PIMAGE_DOS_HEADER)Base)->e_magic == IMAGE_DOS_SIGNATURE) {
            NtHeaders = (PIMAGE_NT_HEADERS)((PUCHAR)Base + ((PIMAGE_DOS_HEADER)Base)->e_lfanew);
            if (NtHeaders->Signature != IMAGE_NT_SIGNATURE) {
                NtHeaders = NULL;
            }
        }
    }
    return NtHeaders;
}
