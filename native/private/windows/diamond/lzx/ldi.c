#include "ldi.h"
#include "decoder.h"

#define LDI_SIGNATURE 0x4349444C

typedef struct
{
    ULONG ContextSignature;
    PFNALLOC pfnAlloc;
    PFNFREE pfnFree;
    PFNOPEN pfnOpen;
    PFNREAD pfnRead;
    PFNWRITE pfnWrite;
    PFNCLOSE pfnClose;
    PFNSEEK pfnSeek;
    UINT cbDataBlockMax;
    UINT fCPUtype;
    t_decoder_context *decoder_context;
} LDI_CONTEXT, *PMDC_CONTEXT;

#define PMDCfromHMD(h) ((PMDC_CONTEXT) (h))

/* Allocates a decoder for the configured window, or only reports the source buffer size when no handle is wanted */
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
    PFNSEEK pfnseek)
{
    PMDC_CONTEXT context;
    PLZXDECOMPRESS pConfig;

    pConfig = pvConfiguration;

    *pcbSrcBufferMin = *pcbDataBlockMax + MAX_GROWTH;

    if (pmdhHandle == NULL)
        return MDI_ERROR_NO_ERROR;

    *pmdhHandle = (LDI_CONTEXT_HANDLE) 0;

    context = pfnma(sizeof(LDI_CONTEXT));
    if (context == NULL)
        return MDI_ERROR_NOT_ENOUGH_MEMORY;

    context->decoder_context = pfnma(sizeof(t_decoder_context));
    if (context->decoder_context == NULL)
    {
        pfnmf(context);
        return MDI_ERROR_NOT_ENOUGH_MEMORY;
    }

    context->pfnAlloc = pfnma;
    context->pfnFree = pfnmf;
    context->pfnOpen = pfnopen;
    context->pfnRead = pfnread;
    context->pfnWrite = pfnwrite;
    context->pfnClose = pfnclose;
    context->pfnSeek = pfnseek;
    context->cbDataBlockMax = *pcbDataBlockMax;
    context->fCPUtype = pConfig->fCPUtype;
    context->ContextSignature = LDI_SIGNATURE;

    if (LZX_DecodeInit(
            context->decoder_context,
            pConfig->WindowSize,
            pfnma,
            pfnmf,
            pfnopen,
            pfnread,
            pfnwrite,
            pfnclose,
            pfnseek) == false)
    {
        pfnmf(context);
        return MDI_ERROR_NOT_ENOUGH_MEMORY;
    }

    *pmdhHandle = (LDI_CONTEXT_HANDLE) context;

    return MDI_ERROR_NO_ERROR;
}

/* Expands one compressed block into the destination and stores the expanded size through pcbResult */
int FAR DIAMONDAPI LDIDecompress(
    LDI_CONTEXT_HANDLE hmd,
    void FAR *pbSrc,
    UINT cbSrc,
    void FAR *pbDst,
    UINT FAR *pcbResult)
{
    PMDC_CONTEXT context;
    int result;
    long bytes_to_decode;
    long total_bytes_written = 0;

    context = PMDCfromHMD(hmd);

    if (context->ContextSignature != LDI_SIGNATURE)
        return MDI_ERROR_BAD_PARAMETERS;

    if (*pcbResult > context->cbDataBlockMax)
        return MDI_ERROR_BUFFER_OVERFLOW;

    bytes_to_decode = (long) *pcbResult;

    result = LZX_Decode(
        context->decoder_context,
        bytes_to_decode,
        pbSrc,
        cbSrc,
        pbDst,
        bytes_to_decode,
        &total_bytes_written);

    *pcbResult = (UINT) total_bytes_written;

    if (result)
        return MDI_ERROR_FAILED;
    else
        return MDI_ERROR_NO_ERROR;
}

/* Releases the decoder window, the decoder and the context and clears the signature */
int FAR DIAMONDAPI LDIDestroyDecompression(LDI_CONTEXT_HANDLE hmd)
{
    PMDC_CONTEXT context;

    context = PMDCfromHMD(hmd);

    if (context->ContextSignature != LDI_SIGNATURE)
        return MDI_ERROR_BAD_PARAMETERS;

    LZX_DecodeFree(context->decoder_context);

    context->ContextSignature = 0;

    context->pfnFree(context->decoder_context);
    context->pfnFree(context);

    return MDI_ERROR_NO_ERROR;
}
