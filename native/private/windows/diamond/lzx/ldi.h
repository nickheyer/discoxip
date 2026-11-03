#ifndef LDI_H
#define LDI_H

#include "mdi.h"

typedef void FAR *LDI_CONTEXT_HANDLE;

typedef struct
{
    UINT WindowSize;
    UINT fCPUtype;
} LZXDECOMPRESS, *PLZXDECOMPRESS;

int FAR DIAMONDAPI LDICreateDecompression(
    UINT FAR *pcbDataBlockMax,
    void FAR *pvConfiguration,
    PFNALLOC pfnma,
    PFNFREE pfnmf,
    UINT FAR *pcbSrcBufferMin,
    LDI_CONTEXT_HANDLE FAR *pmdhHandle,
    PFNOPEN pfnopen,
    PFNREAD pfnread,
    PFNWRITE pfnwrite,
    PFNCLOSE pfnclose,
    PFNSEEK pfnseek);

int FAR DIAMONDAPI LDIDecompress(
    LDI_CONTEXT_HANDLE hmd,
    void FAR *pbSrc,
    UINT cbSrc,
    void FAR *pbDst,
    UINT FAR *pcbResult);

int FAR DIAMONDAPI LDIDestroyDecompression(LDI_CONTEXT_HANDLE hmd);

#endif
