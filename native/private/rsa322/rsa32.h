#ifndef _RSA32_
#define _RSA32_

#define RSA32API __stdcall

typedef unsigned char BYTE;
typedef unsigned long DWORD;
typedef DWORD *LPDWORD;
typedef unsigned __int64 DWORDLONG;
typedef int BOOL;

typedef union {
    struct {
        DWORD LowPart;
        DWORD HighPart;
    };
    DWORDLONG QuadPart;
} ULARGE_INTEGER;

#define TRUE 1
#define FALSE 0
#define NULL ((void *)0)

void * __cdecl memcpy(void *dst, const void *src, unsigned int count);
void * __cdecl memset(void *dst, int val, unsigned int count);
#pragma intrinsic(memcpy, memset)

void * RSA32API RSA32Alloc(DWORD cb);
void RSA32API RSA32Free(void *pv);

#endif
