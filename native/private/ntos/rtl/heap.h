#ifndef _RTL_HEAP_
#define _RTL_HEAP_

#define HEAP_NO_SERIALIZE 0x00000001
#define HEAP_GROWABLE 0x00000002
#define HEAP_GENERATE_EXCEPTIONS 0x00000004
#define HEAP_ZERO_MEMORY 0x00000008
#define HEAP_REALLOC_IN_PLACE_ONLY 0x00000010
#define HEAP_TAIL_CHECKING_ENABLED 0x00000020
#define HEAP_FREE_CHECKING_ENABLED 0x00000040
#define HEAP_DISABLE_COALESCE_ON_FREE 0x00000080
#define HEAP_CREATE_ALIGN_16 0x00010000
#define HEAP_SETTABLE_USER_VALUE 0x00000100
#define HEAP_SETTABLE_USER_FLAG1 0x00000200
#define HEAP_SETTABLE_USER_FLAG2 0x00000400
#define HEAP_SETTABLE_USER_FLAG3 0x00000800
#define HEAP_SETTABLE_USER_FLAGS 0x00000E00
#define HEAP_CAPTURE_STACK_BACKTRACES 0x08000000
#define HEAP_VALIDATE_ALL_ENABLED 0x20000000
#define HEAP_VALIDATE_PARAMETERS_ENABLED 0x40000000
#define HEAP_LOCK_USER_ALLOCATED 0x80000000

#define HEAP_MAXIMUM_TAG 0x0FFF
#define HEAP_PSEUDO_TAG_FLAG 0x8000
#define HEAP_TAG_SHIFT 18
#define HEAP_MAXIMUM_SMALL_TAG 0xFF
#define HEAP_TAG_MASK (HEAP_MAXIMUM_TAG << HEAP_TAG_SHIFT)
#define HEAP_SMALL_TAG_MASK (HEAP_MAXIMUM_SMALL_TAG << HEAP_TAG_SHIFT)
#define HEAP_NEED_EXTRA_FLAGS ((HEAP_TAG_MASK ^ HEAP_SMALL_TAG_MASK) | HEAP_CAPTURE_STACK_BACKTRACES | HEAP_SETTABLE_USER_VALUE)

#define HEAP_GRANULARITY ((LONG)sizeof(HEAP_ENTRY))
#define HEAP_GRANULARITY_SHIFT 4
#define HEAP_MAXIMUM_FREELISTS 128
#define HEAP_MAXIMUM_SEGMENTS 64
#define HEAP_MAXIMUM_BLOCK_SIZE (USHORT)(((0x10000 << HEAP_GRANULARITY_SHIFT) - PAGE_SIZE) >> HEAP_GRANULARITY_SHIFT)
#define HEAP_SEGMENT_SIGNATURE 0xFFEEFFEE
#define HEAP_SIGNATURE (ULONG)0xEEFFEEFF
#define HEAP_MAXIMUM_SEGMENT_SIZE 0x1000000

#define HEAP_ENTRY_BUSY 0x01
#define HEAP_ENTRY_EXTRA_PRESENT 0x02
#define HEAP_ENTRY_FILL_PATTERN 0x04
#define HEAP_ENTRY_VIRTUAL_ALLOC 0x08
#define HEAP_ENTRY_LAST_ENTRY 0x10
#define HEAP_ENTRY_SETTABLE_FLAG1 0x20
#define HEAP_ENTRY_SETTABLE_FLAG2 0x40
#define HEAP_ENTRY_SETTABLE_FLAG3 0x80
#define HEAP_ENTRY_SETTABLE_FLAGS 0xE0

#define HEAP_SEGMENT_USER_ALLOCATED 0x00000001

#define FREE_HEAP_FILL 0xFEEEFEEE
#define ALLOC_HEAP_FILL 0xBAADF00D
#define CHECK_HEAP_TAIL_SIZE HEAP_GRANULARITY
#define CHECK_HEAP_TAIL_FILL 0xAB

typedef struct _HEAP_ENTRY {
    USHORT Size;
    USHORT PreviousSize;
    UCHAR SegmentIndex;
    UCHAR Flags;
    UCHAR UnusedBytes;
    UCHAR SmallTagIndex;
    union {
        unsigned __int64 Reserved1;
        ULONG AllocAttributes;
    };
} HEAP_ENTRY, *PHEAP_ENTRY;

typedef struct _HEAP_ENTRY_EXTRA {
    union {
        struct {
            USHORT AllocatorBackTraceIndex;
            USHORT TagIndex;
            ULONG_PTR Settable;
        };
        unsigned __int64 ZeroInit;
    };
    unsigned __int64 ZeroInit1;
} HEAP_ENTRY_EXTRA, *PHEAP_ENTRY_EXTRA;

typedef struct _HEAP_FREE_ENTRY {
    USHORT Size;
    USHORT PreviousSize;
    UCHAR SegmentIndex;
    UCHAR Flags;
    UCHAR Index;
    UCHAR Mask;
    LIST_ENTRY FreeList;
    unsigned __int64 Reserved1;
} HEAP_FREE_ENTRY, *PHEAP_FREE_ENTRY;

typedef struct _HEAP_FREE_ENTRY_EXTRA {
    USHORT TagIndex;
    USHORT FreeBackTraceIndex;
} HEAP_FREE_ENTRY_EXTRA, *PHEAP_FREE_ENTRY_EXTRA;

typedef struct _HEAP_VIRTUAL_ALLOC_ENTRY {
    LIST_ENTRY Entry;
    HEAP_ENTRY_EXTRA ExtraStuff;
    SIZE_T CommitSize;
    SIZE_T ReserveSize;
    HEAP_ENTRY BusyBlock;
} HEAP_VIRTUAL_ALLOC_ENTRY, *PHEAP_VIRTUAL_ALLOC_ENTRY;

typedef struct _HEAP_UNCOMMMTTED_RANGE {
    struct _HEAP_UNCOMMMTTED_RANGE *Next;
    ULONG_PTR Address;
    SIZE_T Size;
    ULONG filler;
} HEAP_UNCOMMMTTED_RANGE, *PHEAP_UNCOMMMTTED_RANGE;

typedef struct _HEAP_SEGMENT {
    HEAP_ENTRY Entry;
    ULONG Signature;
    ULONG Flags;
    struct _HEAP *Heap;
    SIZE_T LargestUnCommittedRange;
    PVOID BaseAddress;
    ULONG NumberOfPages;
    PHEAP_ENTRY FirstEntry;
    PHEAP_ENTRY LastValidEntry;
    ULONG NumberOfUnCommittedPages;
    ULONG NumberOfUnCommittedRanges;
    PHEAP_UNCOMMMTTED_RANGE UnCommittedRanges;
    USHORT AllocatorBackTraceIndex;
    USHORT Reserved;
    PHEAP_ENTRY LastEntryInSegment;
} HEAP_SEGMENT, *PHEAP_SEGMENT;

typedef struct _HEAP_LOCK {
    union {
        RTL_CRITICAL_SECTION CriticalSection;
    } Lock;
} HEAP_LOCK, *PHEAP_LOCK;

typedef struct _HEAP_UCR_SEGMENT {
    struct _HEAP_UCR_SEGMENT *Next;
    SIZE_T ReservedSize;
    SIZE_T CommittedSize;
    ULONG filler;
} HEAP_UCR_SEGMENT, *PHEAP_UCR_SEGMENT;

typedef struct _HEAP_TAG_ENTRY {
    ULONG Allocs;
    ULONG Frees;
    SIZE_T Size;
    USHORT TagIndex;
    USHORT CreatorBackTraceIndex;
    unsigned short TagName[24];
} HEAP_TAG_ENTRY, *PHEAP_TAG_ENTRY;

typedef struct _HEAP_PSEUDO_TAG_ENTRY {
    ULONG Allocs;
    ULONG Frees;
    SIZE_T Size;
} HEAP_PSEUDO_TAG_ENTRY, *PHEAP_PSEUDO_TAG_ENTRY;

typedef NTSTATUS (NTAPI *PRTL_HEAP_COMMIT_ROUTINE)(PVOID Base, PVOID *CommitAddress, PSIZE_T CommitSize);

typedef struct _RTL_HEAP_PARAMETERS {
    ULONG Length;
    SIZE_T SegmentReserve;
    SIZE_T SegmentCommit;
    SIZE_T DeCommitFreeBlockThreshold;
    SIZE_T DeCommitTotalFreeThreshold;
    SIZE_T MaximumAllocationSize;
    SIZE_T VirtualMemoryThreshold;
    SIZE_T InitialCommit;
    SIZE_T InitialReserve;
    PRTL_HEAP_COMMIT_ROUTINE CommitRoutine;
    SIZE_T Reserved[2];
} RTL_HEAP_PARAMETERS, *PRTL_HEAP_PARAMETERS;

typedef struct _HEAP {
    HEAP_ENTRY Entry;
    ULONG Signature;
    ULONG Flags;
    ULONG ForceFlags;
    ULONG VirtualMemoryThreshold;
    SIZE_T SegmentReserve;
    SIZE_T SegmentCommit;
    SIZE_T DeCommitFreeBlockThreshold;
    SIZE_T DeCommitTotalFreeThreshold;
    SIZE_T TotalFreeSize;
    SIZE_T MaximumAllocationSize;
    USHORT ProcessHeapsListIndex;
    USHORT HeaderValidateLength;
    PVOID HeaderValidateCopy;
    USHORT NextAvailableTagIndex;
    USHORT MaximumTagIndex;
    PHEAP_TAG_ENTRY TagEntries;
    PHEAP_UCR_SEGMENT UCRSegments;
    PHEAP_UNCOMMMTTED_RANGE UnusedUnCommittedRanges;
    ULONG_PTR AlignRound;
    ULONG_PTR AlignMask;
    LIST_ENTRY VirtualAllocdBlocks;
    PHEAP_SEGMENT Segments[HEAP_MAXIMUM_SEGMENTS];
    union {
        ULONG FreeListsInUseUlong[HEAP_MAXIMUM_FREELISTS / 32];
        UCHAR FreeListsInUseBytes[HEAP_MAXIMUM_FREELISTS / 8];
    } u;
    USHORT FreeListsInUseTerminate;
    USHORT AllocatorBackTraceIndex;
    ULONG Reserved1[2];
    PHEAP_PSEUDO_TAG_ENTRY PseudoTagEntries;
    LIST_ENTRY FreeLists[HEAP_MAXIMUM_FREELISTS];
    PHEAP_LOCK LockVariable;
    PRTL_HEAP_COMMIT_ROUTINE CommitRoutine;
    PVOID Lookaside;
    ULONG LookasideLockCount;
} HEAP, *PHEAP;

#define RtlAcquireLockRoutine(L) RtlEnterCriticalSection(&(L)->Lock.CriticalSection)
#define RtlReleaseLockRoutine(L) RtlLeaveCriticalSection(&(L)->Lock.CriticalSection)
#define RtlInitializeLockRoutine(L) RtlInitializeCriticalSection(&(L)->Lock.CriticalSection)

#define SET_FREELIST_BIT(H, FB) {\
    ULONG _Index_;\
    ULONG _Bit_;\
    _Index_ = (FB)->Size >> 3;\
    _Bit_ = (1 << ((FB)->Size & 7));\
    (H)->u.FreeListsInUseBytes[_Index_] |= _Bit_;\
    }

#define CLEAR_FREELIST_BIT(H, FB) {\
    ULONG _Index_;\
    ULONG _Bit_;\
    _Index_ = (FB)->Size >> 3;\
    _Bit_ = (1 << ((FB)->Size & 7));\
    (H)->u.FreeListsInUseBytes[_Index_] ^= _Bit_;\
    }

#define RtlpFastInsertFreeBlockDirect(H, FB, SIZE) {\
    if ((SIZE) < HEAP_MAXIMUM_FREELISTS) {\
        RtlpFastInsertDedicatedFreeBlockDirect(H, FB, SIZE)\
    } else {\
        RtlpFastInsertNonDedicatedFreeBlockDirect(H, FB, SIZE)\
    }\
    }

#define RtlpFastInsertDedicatedFreeBlockDirect(H, FB, SIZE) {\
    PLIST_ENTRY _HEAD, _NEXT;\
    (FB)->Flags &= HEAP_ENTRY_LAST_ENTRY;\
    _HEAD = &(H)->FreeLists[SIZE];\
    if (IsListEmpty(_HEAD)) {\
        SET_FREELIST_BIT(H, FB);\
    }\
    InsertTailList(_HEAD, &(FB)->FreeList);\
    }

#define RtlpFastInsertNonDedicatedFreeBlockDirect(H, FB, SIZE) {\
    PLIST_ENTRY _HEAD, _NEXT;\
    PHEAP_FREE_ENTRY _FB1;\
    (FB)->Flags &= HEAP_ENTRY_LAST_ENTRY;\
    _HEAD = &(H)->FreeLists[0];\
    _NEXT = _HEAD->Flink;\
    while (_HEAD != _NEXT) {\
        _FB1 = CONTAINING_RECORD(_NEXT, HEAP_FREE_ENTRY, FreeList);\
        if ((SIZE) <= _FB1->Size) {\
            break;\
        } else {\
            _NEXT = _NEXT->Flink;\
        }\
    }\
    InsertTailList(_NEXT, &(FB)->FreeList);\
    }

#define RtlpInsertFreeBlockDirect(H, FB, SIZE) {\
    PLIST_ENTRY _HEAD, _NEXT;\
    PHEAP_FREE_ENTRY _FB1;\
    (FB)->Flags &= ~(HEAP_ENTRY_FILL_PATTERN | HEAP_ENTRY_EXTRA_PRESENT | HEAP_ENTRY_BUSY);\
    if ((SIZE) < HEAP_MAXIMUM_FREELISTS) {\
        _HEAD = &(H)->FreeLists[SIZE];\
        if (IsListEmpty(_HEAD)) {\
            SET_FREELIST_BIT(H, FB);\
        }\
    } else {\
        _HEAD = &(H)->FreeLists[0];\
        _NEXT = _HEAD->Flink;\
        while (_HEAD != _NEXT) {\
            _FB1 = CONTAINING_RECORD(_NEXT, HEAP_FREE_ENTRY, FreeList);\
            if ((SIZE) <= _FB1->Size) {\
                break;\
            } else {\
                _NEXT = _NEXT->Flink;\
            }\
        }\
        _HEAD = _NEXT;\
    }\
    InsertTailList(_HEAD, &(FB)->FreeList);\
    }

#define RtlpFastRemoveFreeBlock(H, FB) {\
    PLIST_ENTRY _EX_Blink;\
    PLIST_ENTRY _EX_Flink;\
    _EX_Flink = (FB)->FreeList.Flink;\
    _EX_Blink = (FB)->FreeList.Blink;\
    _EX_Blink->Flink = _EX_Flink;\
    _EX_Flink->Blink = _EX_Blink;\
    if ((_EX_Flink == _EX_Blink) && ((FB)->Size < HEAP_MAXIMUM_FREELISTS)) {\
        CLEAR_FREELIST_BIT(H, FB);\
    }\
    }

#define RtlpFastRemoveDedicatedFreeBlock(H, FB) {\
    PLIST_ENTRY _EX_Blink;\
    PLIST_ENTRY _EX_Flink;\
    _EX_Flink = (FB)->FreeList.Flink;\
    _EX_Blink = (FB)->FreeList.Blink;\
    _EX_Blink->Flink = _EX_Flink;\
    _EX_Flink->Blink = _EX_Blink;\
    if (_EX_Flink == _EX_Blink) {\
        CLEAR_FREELIST_BIT(H, FB);\
    }\
    }

#define RtlpFastRemoveNonDedicatedFreeBlock(H, FB) RemoveEntryList(&(FB)->FreeList)

#define RtlpRemoveFreeBlock(H, FB) {\
    RtlpFastRemoveFreeBlock(H, FB)\
    if ((FB)->Flags & HEAP_ENTRY_FILL_PATTERN) {\
        SIZE_T cb, cbEqual;\
        cb = ((FB)->Size << HEAP_GRANULARITY_SHIFT) - sizeof(*(FB));\
        if (((FB)->Flags & HEAP_ENTRY_EXTRA_PRESENT) && (cb > sizeof(HEAP_FREE_ENTRY_EXTRA))) {\
            cb -= sizeof(HEAP_FREE_ENTRY_EXTRA);\
        }\
        cbEqual = RtlCompareMemoryUlong((PCHAR)((FB) + 1), cb, FREE_HEAP_FILL);\
    }\
    }

PVOID NTAPI RtlCreateHeap(ULONG Flags, PVOID HeapBase, SIZE_T ReserveSize, SIZE_T CommitSize, PVOID Lock, PRTL_HEAP_PARAMETERS Parameters);
PVOID NTAPI RtlDestroyHeap(PVOID HeapHandle);
PVOID NTAPI RtlAllocateHeap(PVOID HeapHandle, ULONG Flags, SIZE_T Size);
PVOID NTAPI RtlAllocateHeapSlowly(PVOID HeapHandle, ULONG Flags, SIZE_T Size);
PVOID NTAPI RtlReAllocateHeap(PVOID HeapHandle, ULONG Flags, PVOID BaseAddress, SIZE_T Size);
BOOLEAN NTAPI RtlFreeHeap(PVOID HeapHandle, ULONG Flags, PVOID BaseAddress);
BOOLEAN NTAPI RtlFreeHeapSlowly(PVOID HeapHandle, ULONG Flags, PVOID BaseAddress);
SIZE_T NTAPI RtlSizeHeap(PVOID HeapHandle, ULONG Flags, PVOID BaseAddress);
NTSTATUS NTAPI RtlZeroHeap(PVOID HeapHandle, ULONG Flags);

PHEAP_UNCOMMMTTED_RANGE NTAPI RtlpCreateUnCommittedRange(PHEAP_SEGMENT Segment);
VOID NTAPI RtlpDestroyUnCommittedRange(PHEAP_SEGMENT Segment, PHEAP_UNCOMMMTTED_RANGE UnCommittedRange);
VOID NTAPI RtlpInsertUnCommittedPages(PHEAP_SEGMENT Segment, ULONG_PTR Address, SIZE_T Size);
PHEAP_FREE_ENTRY NTAPI RtlpFindAndCommitPages(PHEAP Heap, PHEAP_SEGMENT Segment, PSIZE_T Size, PVOID AddressWanted);
BOOLEAN NTAPI RtlpInitializeHeapSegment(PHEAP Heap, PHEAP_SEGMENT Segment, UCHAR SegmentIndex, ULONG Flags, PVOID BaseAddress, PVOID UnCommittedAddress, PVOID CommitLimitAddress);
NTSTATUS NTAPI RtlpDestroyHeapSegment(PHEAP_SEGMENT Segment);
PHEAP_FREE_ENTRY NTAPI RtlpExtendHeap(PHEAP Heap, SIZE_T AllocationSize);
PHEAP_FREE_ENTRY NTAPI RtlpCoalesceFreeBlocks(PHEAP Heap, PHEAP_FREE_ENTRY FreeBlock, PSIZE_T FreeSize, BOOLEAN RemoveFromFreeList);
VOID NTAPI RtlpDeCommitFreeBlock(PHEAP Heap, PHEAP_FREE_ENTRY FreeBlock, SIZE_T FreeSize);
VOID NTAPI RtlpInsertFreeBlock(PHEAP Heap, PHEAP_FREE_ENTRY FreeBlock, SIZE_T FreeSize);
PHEAP_ENTRY_EXTRA NTAPI RtlpGetExtraStuffPointer(PHEAP_ENTRY BusyBlock);
SIZE_T NTAPI RtlpGetSizeOfBigBlock(PHEAP_ENTRY BusyBlock);
BOOLEAN NTAPI RtlpCheckBusyBlockTail(PHEAP_ENTRY BusyBlock);
BOOLEAN NTAPI RtlpGrowBlockInPlace(PHEAP Heap, ULONG Flags, PHEAP_ENTRY BusyBlock, SIZE_T Size, SIZE_T AllocationIndex);

extern SIZE_T MmHeapSegmentReserve;
extern SIZE_T MmHeapSegmentCommit;
extern SIZE_T MmHeapDeCommitTotalFreeThreshold;
extern SIZE_T MmHeapDeCommitFreeBlockThreshold;

#endif
