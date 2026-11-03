#ifndef MDI_H
#define MDI_H

#define FAR
#define DIAMONDAPI __cdecl

#ifndef NULL
#define NULL ((void *) 0)
#endif

typedef unsigned int UINT;
typedef unsigned long ULONG;

#define MDI_ERROR_NO_ERROR              0
#define MDI_ERROR_NOT_ENOUGH_MEMORY     1
#define MDI_ERROR_BAD_PARAMETERS        2
#define MDI_ERROR_BUFFER_OVERFLOW       3
#define MDI_ERROR_FAILED                4

typedef void FAR *(FAR DIAMONDAPI *PFNALLOC)(ULONG cb);
typedef void (FAR DIAMONDAPI *PFNFREE)(void FAR *pv);
typedef int (FAR DIAMONDAPI *PFNOPEN)(char FAR *pszFile, int oflag, int pmode);
typedef UINT (FAR DIAMONDAPI *PFNREAD)(int hf, void FAR *pv, UINT cb);
typedef UINT (FAR DIAMONDAPI *PFNWRITE)(int hf, void FAR *pv, UINT cb);
typedef int (FAR DIAMONDAPI *PFNCLOSE)(int hf);
typedef long (FAR DIAMONDAPI *PFNSEEK)(int hf, long dist, int seektype);

#endif
