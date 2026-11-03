#ifndef _NTRTLP_
#define _NTRTLP_

#define IN
#define OUT
#define OPTIONAL
#define NTAPI __stdcall
#define FASTCALL __fastcall
#define NTSYSAPI __declspec(dllimport)
#define VOID void
#define CONST const
#define NULL ((void *)0)
#define FALSE 0
#define TRUE 1

typedef void *PVOID;
typedef char CHAR, *PCHAR, CCHAR;
typedef unsigned char UCHAR, *PUCHAR, BOOLEAN, *PBOOLEAN;
typedef unsigned short USHORT, *PUSHORT;
typedef long LONG, *PLONG, NTSTATUS, HRESULT;
typedef unsigned long ULONG, *PULONG, ULONG_PTR, SIZE_T, *PSIZE_T;
typedef unsigned long DWORD;

typedef struct _LIST_ENTRY {
    struct _LIST_ENTRY *Flink;
    struct _LIST_ENTRY *Blink;
} LIST_ENTRY, *PLIST_ENTRY;

typedef struct _RTL_CRITICAL_SECTION {
    union {
        struct {
            UCHAR Type;
            UCHAR Absolute;
            UCHAR Size;
            UCHAR Inserted;
            LONG SignalState;
            LIST_ENTRY WaitListHead;
        } Event;
        ULONG RawEvent[4];
    } Synchronization;
    LONG LockCount;
    LONG RecursionCount;
    PVOID OwningThread;
} RTL_CRITICAL_SECTION, *PRTL_CRITICAL_SECTION;

#define EXCEPTION_MAXIMUM_PARAMETERS 15

typedef struct _EXCEPTION_RECORD {
    NTSTATUS ExceptionCode;
    ULONG ExceptionFlags;
    struct _EXCEPTION_RECORD *ExceptionRecord;
    PVOID ExceptionAddress;
    ULONG NumberParameters;
    ULONG_PTR ExceptionInformation[EXCEPTION_MAXIMUM_PARAMETERS];
} EXCEPTION_RECORD, *PEXCEPTION_RECORD;

typedef struct _MEMORY_BASIC_INFORMATION {
    PVOID BaseAddress;
    PVOID AllocationBase;
    ULONG AllocationProtect;
    SIZE_T RegionSize;
    ULONG State;
    ULONG Protect;
    ULONG Type;
} MEMORY_BASIC_INFORMATION, *PMEMORY_BASIC_INFORMATION;

#define STATUS_SUCCESS ((NTSTATUS)0x00000000L)
#define STATUS_NO_MEMORY ((NTSTATUS)0xC0000017L)
#define STATUS_BUFFER_TOO_SMALL ((NTSTATUS)0xC0000023L)
#define NT_SUCCESS(Status) ((NTSTATUS)(Status) >= 0)

#define PAGE_SIZE 0x1000
#define PAGE_SHIFT 12L
#define MM_HIGHEST_USER_ADDRESS (PVOID)0x7FFEFFFF
#define MM_LOWEST_USER_ADDRESS (PVOID)0x00010000

#define MEM_COMMIT 0x1000
#define MEM_RESERVE 0x2000
#define MEM_DECOMMIT 0x4000
#define MEM_RELEASE 0x8000
#define MEM_FREE 0x10000
#define MEM_NOZERO 0x800000
#define PAGE_READWRITE 0x04

#define EXCEPTION_EXECUTE_HANDLER 1
#define EXCEPTION_CONTINUE_SEARCH 0
#define try __try
#define except __except
#define finally __finally
#define leave __leave
#define GetExceptionCode _exception_code
unsigned long __cdecl _exception_code(void);

#define FIELD_OFFSET(type, field) ((LONG)(LONG_PTR)&(((type *)0)->field))
#define CONTAINING_RECORD(address, type, field) ((type *)((PCHAR)(address) - (ULONG_PTR)(&((type *)0)->field)))
#define ROUND_UP_TO_POWER2(x, n) (((ULONG_PTR)(x) + ((n) - 1)) & ~((ULONG_PTR)(n) - 1))
#define ROUND_DOWN_TO_POWER2(x, n) ((ULONG_PTR)(x) & ~((ULONG_PTR)(n) - 1))

#define InitializeListHead(ListHead) ((ListHead)->Flink = (ListHead)->Blink = (ListHead))
#define IsListEmpty(ListHead) ((ListHead)->Flink == (ListHead))

#define RemoveEntryList(Entry) {\
    PLIST_ENTRY _EX_Blink;\
    PLIST_ENTRY _EX_Flink;\
    _EX_Flink = (Entry)->Flink;\
    _EX_Blink = (Entry)->Blink;\
    _EX_Blink->Flink = _EX_Flink;\
    _EX_Flink->Blink = _EX_Blink;\
    }

#define InsertTailList(ListHead, Entry) {\
    PLIST_ENTRY _EX_Blink;\
    PLIST_ENTRY _EX_ListHead;\
    _EX_ListHead = (ListHead);\
    _EX_Blink = _EX_ListHead->Blink;\
    (Entry)->Flink = _EX_ListHead;\
    (Entry)->Blink = _EX_Blink;\
    _EX_Blink->Flink = (Entry);\
    _EX_ListHead->Blink = (Entry);\
    }

typedef long LONG_PTR;

typedef unsigned int size_t;
void *__cdecl memmove(void *, const void *, size_t);
void *__cdecl memset(void *, int, size_t);
#pragma intrinsic(memset)

#define RtlMoveMemory(Destination, Source, Length) memmove((Destination), (Source), (Length))
#define RtlZeroMemory(Destination, Length) memset((Destination), 0, (Length))

NTSYSAPI SIZE_T NTAPI RtlCompareMemory(const VOID *Source1, const VOID *Source2, SIZE_T Length);
NTSYSAPI SIZE_T NTAPI RtlCompareMemoryUlong(PVOID Source, SIZE_T Length, ULONG Pattern);
NTSYSAPI VOID NTAPI RtlFillMemoryUlong(PVOID Destination, SIZE_T Length, ULONG Pattern);
NTSYSAPI VOID NTAPI RtlInitializeCriticalSection(PRTL_CRITICAL_SECTION CriticalSection);
NTSYSAPI VOID NTAPI RtlEnterCriticalSection(PRTL_CRITICAL_SECTION CriticalSection);
NTSYSAPI VOID NTAPI RtlLeaveCriticalSection(PRTL_CRITICAL_SECTION CriticalSection);
NTSYSAPI VOID NTAPI RtlRaiseException(PEXCEPTION_RECORD ExceptionRecord);
NTSYSAPI NTSTATUS NTAPI NtAllocateVirtualMemory(PVOID *BaseAddress, ULONG_PTR ZeroBits, PSIZE_T RegionSize, ULONG AllocationType, ULONG Protect);
NTSYSAPI NTSTATUS NTAPI NtFreeVirtualMemory(PVOID *BaseAddress, PSIZE_T RegionSize, ULONG FreeType);
NTSYSAPI NTSTATUS NTAPI NtQueryVirtualMemory(PVOID BaseAddress, PMEMORY_BASIC_INFORMATION MemoryInformation);

CCHAR FASTCALL RtlFindFirstSetRightMember(ULONG Set);

#define FLG_HEAP_ENABLE_TAGGING 0x00000800
#define FLG_HEAP_DISABLE_COALESCING 0x00200000

#endif
