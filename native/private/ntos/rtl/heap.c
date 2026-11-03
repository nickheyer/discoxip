#include "ntrtlp.h"
#include "i386.h"
#include "dm.h"
#include "heap.h"

SIZE_T MmHeapSegmentReserve = 1024 * 1024;
SIZE_T MmHeapSegmentCommit = PAGE_SIZE * 2;
SIZE_T MmHeapDeCommitTotalFreeThreshold = 64 * 1024;
SIZE_T MmHeapDeCommitFreeBlockThreshold = PAGE_SIZE;

ULONG NtGlobalFlag;
UCHAR CheckHeapFillPattern[CHECK_HEAP_TAIL_SIZE];

PVOID NTAPI RtlCreateHeap(
    IN ULONG Flags,
    IN PVOID HeapBase OPTIONAL,
    IN SIZE_T ReserveSize OPTIONAL,
    IN SIZE_T CommitSize OPTIONAL,
    IN PVOID Lock OPTIONAL,
    IN PRTL_HEAP_PARAMETERS Parameters OPTIONAL
    )
{
    PHEAP Heap = NULL;
    PLIST_ENTRY FreeListHead;
    ULONG SizeOfHeapHeader;
    ULONG SegmentFlags;
    PVOID CommittedBase;
    PVOID UnCommittedBase;
    MEMORY_BASIC_INFORMATION MemoryInformation;
    NTSTATUS Status;
    PHEAP_UNCOMMMTTED_RANGE UnCommittedRange, *pp;
    ULONG n;
    ULONG InitialCountOfUnusedUnCommittedRanges;
    PVOID NextHeapHeaderAddress;
    RTL_HEAP_PARAMETERS TempParameters;

    Status = STATUS_SUCCESS;

    RtlZeroMemory(&TempParameters, sizeof(TempParameters));

    if (Parameters != NULL) {
        try {
            if (Parameters->Length == sizeof(*Parameters)) {
                RtlMoveMemory(&TempParameters, Parameters, sizeof(TempParameters));
            }
        } except(EXCEPTION_EXECUTE_HANDLER) {
            Status = GetExceptionCode();
        }

        if (!NT_SUCCESS(Status)) {
            return NULL;
        }
    }

    if (NtGlobalFlag & FLG_HEAP_DISABLE_COALESCING) {
        Flags |= HEAP_DISABLE_COALESCE_ON_FREE;
    }

    if (TempParameters.SegmentReserve == 0) {
        TempParameters.SegmentReserve = MmHeapSegmentReserve;
    }

    if (TempParameters.SegmentCommit == 0) {
        TempParameters.SegmentCommit = MmHeapSegmentCommit;
    }

    if (TempParameters.DeCommitFreeBlockThreshold == 0) {
        TempParameters.DeCommitFreeBlockThreshold = MmHeapDeCommitFreeBlockThreshold;
    }

    if (TempParameters.DeCommitTotalFreeThreshold == 0) {
        TempParameters.DeCommitTotalFreeThreshold = MmHeapDeCommitTotalFreeThreshold;
    }

    if (TempParameters.MaximumAllocationSize == 0) {
        TempParameters.MaximumAllocationSize = (ULONG_PTR)MM_HIGHEST_USER_ADDRESS - (ULONG_PTR)MM_LOWEST_USER_ADDRESS - PAGE_SIZE;
    }

    if ((TempParameters.VirtualMemoryThreshold == 0) ||
        (TempParameters.VirtualMemoryThreshold > (HEAP_MAXIMUM_BLOCK_SIZE << HEAP_GRANULARITY_SHIFT))) {
        TempParameters.VirtualMemoryThreshold = HEAP_MAXIMUM_BLOCK_SIZE << HEAP_GRANULARITY_SHIFT;
    }

    if (CommitSize == 0) {
        CommitSize = PAGE_SIZE;

        if (ReserveSize == 0) {
            ReserveSize = 64 * PAGE_SIZE;
        } else {
            ReserveSize = ROUND_UP_TO_POWER2(ReserveSize, PAGE_SIZE);
        }
    } else {
        CommitSize = ROUND_UP_TO_POWER2(CommitSize, PAGE_SIZE);

        if (ReserveSize == 0) {
            ReserveSize = ROUND_UP_TO_POWER2(CommitSize, 16 * PAGE_SIZE);
        } else {
            ReserveSize = ROUND_UP_TO_POWER2(ReserveSize, PAGE_SIZE);
        }
    }

    SizeOfHeapHeader = sizeof(HEAP);

    if (!(Flags & HEAP_NO_SERIALIZE)) {
        if (Lock != NULL) {
            Flags |= HEAP_LOCK_USER_ALLOCATED;
        } else {
            SizeOfHeapHeader += sizeof(HEAP_LOCK);
            Lock = (PHEAP_LOCK)-1;
        }
    } else if (Lock != NULL) {
        return NULL;
    }

    if (HeapBase != NULL) {
        if (TempParameters.CommitRoutine != NULL) {
            if ((TempParameters.InitialCommit == 0) ||
                (TempParameters.InitialReserve == 0) ||
                (TempParameters.InitialCommit > TempParameters.InitialReserve) ||
                (Flags & HEAP_GROWABLE)) {
                return NULL;
            }

            CommittedBase = HeapBase;
            UnCommittedBase = (PCHAR)CommittedBase + TempParameters.InitialCommit;
            ReserveSize = TempParameters.InitialReserve;

            RtlZeroMemory(CommittedBase, PAGE_SIZE);
        } else {
            Status = NtQueryVirtualMemory(HeapBase, &MemoryInformation);

            if (!NT_SUCCESS(Status)) {
                return NULL;
            }

            if (MemoryInformation.BaseAddress != HeapBase) {
                return NULL;
            }

            if (MemoryInformation.State == MEM_FREE) {
                return NULL;
            }

            CommittedBase = MemoryInformation.BaseAddress;

            if (MemoryInformation.State == MEM_COMMIT) {
                RtlZeroMemory(CommittedBase, PAGE_SIZE);

                CommitSize = MemoryInformation.RegionSize;
                UnCommittedBase = (PCHAR)CommittedBase + CommitSize;

                Status = NtQueryVirtualMemory(UnCommittedBase, &MemoryInformation);

                ReserveSize = CommitSize;

                if ((NT_SUCCESS(Status)) && (MemoryInformation.State == MEM_RESERVE)) {
                    ReserveSize += MemoryInformation.RegionSize;
                }
            } else {
                CommitSize = PAGE_SIZE;
                UnCommittedBase = CommittedBase;
            }
        }

        SegmentFlags = HEAP_SEGMENT_USER_ALLOCATED;
        Heap = (PHEAP)HeapBase;
    } else {
        if (TempParameters.CommitRoutine != NULL) {
            return NULL;
        }

        Status = NtAllocateVirtualMemory((PVOID *)&Heap, 0, &ReserveSize, MEM_RESERVE, PAGE_READWRITE);

        if (!NT_SUCCESS(Status)) {
            return NULL;
        }

        SegmentFlags = 0;

        if (CommitSize == 0) {
            CommitSize = PAGE_SIZE;
        }

        CommittedBase = Heap;
        UnCommittedBase = Heap;
    }

    if (CommittedBase == UnCommittedBase) {
        Status = NtAllocateVirtualMemory((PVOID *)&CommittedBase, 0, &CommitSize, MEM_COMMIT, PAGE_READWRITE);

        if (!NT_SUCCESS(Status)) {
            if (HeapBase == NULL) {
                NtFreeVirtualMemory((PVOID *)&Heap, &ReserveSize, MEM_RELEASE);
            }

            return NULL;
        }

        UnCommittedBase = (PCHAR)UnCommittedBase + CommitSize;
    }

    NextHeapHeaderAddress = Heap + 1;

    UnCommittedRange = (PHEAP_UNCOMMMTTED_RANGE)ROUND_UP_TO_POWER2(NextHeapHeaderAddress, sizeof(unsigned __int64));

    InitialCountOfUnusedUnCommittedRanges = 8;

    SizeOfHeapHeader += InitialCountOfUnusedUnCommittedRanges * sizeof(*UnCommittedRange);

    pp = &Heap->UnusedUnCommittedRanges;

    while (InitialCountOfUnusedUnCommittedRanges--) {
        *pp = UnCommittedRange;
        pp = &UnCommittedRange->Next;
        UnCommittedRange += 1;
    }

    NextHeapHeaderAddress = UnCommittedRange;

    *pp = NULL;

    if (NtGlobalFlag & FLG_HEAP_ENABLE_TAGGING) {
        Heap->PseudoTagEntries = (PHEAP_PSEUDO_TAG_ENTRY)ROUND_UP_TO_POWER2(NextHeapHeaderAddress, sizeof(unsigned __int64));

        SizeOfHeapHeader += (HEAP_MAXIMUM_FREELISTS + 1) * sizeof(HEAP_PSEUDO_TAG_ENTRY);

        NextHeapHeaderAddress = Heap->PseudoTagEntries + (HEAP_MAXIMUM_FREELISTS + 1);
    }

    SizeOfHeapHeader = ROUND_UP_TO_POWER2(SizeOfHeapHeader, HEAP_GRANULARITY);

    Heap->Entry.Size = (USHORT)(SizeOfHeapHeader >> HEAP_GRANULARITY_SHIFT);
    Heap->Entry.Flags = HEAP_ENTRY_BUSY;

    Heap->Signature = HEAP_SIGNATURE;
    Heap->Flags = Flags;
    Heap->ForceFlags = (Flags & (HEAP_NO_SERIALIZE |
                                 HEAP_GENERATE_EXCEPTIONS |
                                 HEAP_ZERO_MEMORY |
                                 HEAP_REALLOC_IN_PLACE_ONLY |
                                 HEAP_VALIDATE_PARAMETERS_ENABLED |
                                 HEAP_VALIDATE_ALL_ENABLED |
                                 HEAP_TAIL_CHECKING_ENABLED |
                                 HEAP_CREATE_ALIGN_16 |
                                 HEAP_FREE_CHECKING_ENABLED));
    Heap->FreeListsInUseTerminate = 0xFFFF;
    Heap->HeaderValidateLength = (USHORT)((PCHAR)NextHeapHeaderAddress - (PCHAR)Heap);
    Heap->HeaderValidateCopy = NULL;

    FreeListHead = &Heap->FreeLists[0];
    n = HEAP_MAXIMUM_FREELISTS;

    while (n--) {
        InitializeListHead(FreeListHead);
        FreeListHead++;
    }

    InitializeListHead(&Heap->VirtualAllocdBlocks);

    if (Lock == (PHEAP_LOCK)-1) {
        Lock = (PHEAP_LOCK)NextHeapHeaderAddress;
        RtlInitializeLockRoutine((PHEAP_LOCK)Lock);
    }

    Heap->LockVariable = Lock;

    if (!RtlpInitializeHeapSegment(Heap,
                                   (PHEAP_SEGMENT)((PCHAR)Heap + SizeOfHeapHeader),
                                   0,
                                   SegmentFlags,
                                   CommittedBase,
                                   UnCommittedBase,
                                   (PCHAR)CommittedBase + ReserveSize)) {
        return NULL;
    }

    Heap->ProcessHeapsListIndex = 0;
    Heap->SegmentReserve = TempParameters.SegmentReserve;
    Heap->SegmentCommit = TempParameters.SegmentCommit;
    Heap->DeCommitFreeBlockThreshold = TempParameters.DeCommitFreeBlockThreshold >> HEAP_GRANULARITY_SHIFT;
    Heap->DeCommitTotalFreeThreshold = TempParameters.DeCommitTotalFreeThreshold >> HEAP_GRANULARITY_SHIFT;
    Heap->MaximumAllocationSize = TempParameters.MaximumAllocationSize;
    Heap->VirtualMemoryThreshold = ROUND_UP_TO_POWER2(TempParameters.VirtualMemoryThreshold, HEAP_GRANULARITY) >> HEAP_GRANULARITY_SHIFT;
    Heap->CommitRoutine = TempParameters.CommitRoutine;

    if (Flags & HEAP_CREATE_ALIGN_16) {
        Heap->AlignRound = 15 + sizeof(HEAP_ENTRY);
        Heap->AlignMask = ~(ULONG_PTR)15;
    } else {
        Heap->AlignRound = HEAP_GRANULARITY - 1 + sizeof(HEAP_ENTRY);
        Heap->AlignMask = ~((ULONG_PTR)HEAP_GRANULARITY - 1);
    }

    return (PVOID)Heap;
}

PVOID NTAPI RtlDestroyHeap(
    IN PVOID HeapHandle
    )
{
    PHEAP Heap = (PHEAP)HeapHandle;
    PHEAP_SEGMENT Segment;
    PHEAP_UCR_SEGMENT UCRSegments;
    PLIST_ENTRY Head, Next;
    PVOID BaseAddress;
    SIZE_T RegionSize;
    UCHAR SegmentIndex;

    if (HeapHandle == NULL) {
        return HeapHandle;
    }

    Head = &Heap->VirtualAllocdBlocks;
    Next = Head->Flink;

    while (Head != Next) {
        BaseAddress = CONTAINING_RECORD(Next, HEAP_VIRTUAL_ALLOC_ENTRY, Entry);
        Next = Next->Flink;
        RegionSize = 0;

        NtFreeVirtualMemory(&BaseAddress, &RegionSize, MEM_RELEASE);
    }

    if (!(Heap->Flags & HEAP_NO_SERIALIZE)) {
        Heap->LockVariable = NULL;
    }

    UCRSegments = Heap->UCRSegments;
    Heap->UCRSegments = NULL;

    while (UCRSegments) {
        BaseAddress = UCRSegments;
        UCRSegments = UCRSegments->Next;
        RegionSize = 0;

        NtFreeVirtualMemory(&BaseAddress, &RegionSize, MEM_RELEASE);
    }

    SegmentIndex = HEAP_MAXIMUM_SEGMENTS;

    while (SegmentIndex--) {
        Segment = Heap->Segments[SegmentIndex];

        if (Segment) {
            RtlpDestroyHeapSegment(Segment);
        }
    }

    return NULL;
}

PVOID NTAPI RtlAllocateHeap(
    IN PVOID HeapHandle,
    IN ULONG Flags,
    IN SIZE_T Size
    )
{
    PHEAP Heap = (PHEAP)HeapHandle;
    PULONG FreeListsInUse;
    ULONG FreeListsInUseUlong;
    SIZE_T AllocationSize;
    SIZE_T FreeSize, AllocationIndex;
    PLIST_ENTRY FreeListHead, Next;
    PHEAP_ENTRY BusyBlock;
    PHEAP_FREE_ENTRY FreeBlock, SplitBlock, SplitBlock2;
    ULONG InUseIndex;
    UCHAR FreeFlags;
    NTSTATUS Status;
    EXCEPTION_RECORD ExceptionRecord;
    PVOID ReturnValue;
    BOOLEAN LockAcquired = FALSE;
    PHEAP_SEGMENT Segment;

    Flags |= Heap->ForceFlags;

    AllocationSize = ((Size ? Size : 1) + HEAP_GRANULARITY - 1 + sizeof(HEAP_ENTRY)) & ~(HEAP_GRANULARITY - 1);
    AllocationIndex = AllocationSize >> HEAP_GRANULARITY_SHIFT;

    try {
        if (!(Flags & HEAP_NO_SERIALIZE)) {
            RtlAcquireLockRoutine(Heap->LockVariable);
            LockAcquired = TRUE;
        }

        if (AllocationIndex < HEAP_MAXIMUM_FREELISTS) {
            FreeListHead = &Heap->FreeLists[AllocationIndex];

            if (!IsListEmpty(FreeListHead)) {
                FreeBlock = CONTAINING_RECORD(FreeListHead->Blink, HEAP_FREE_ENTRY, FreeList);
                FreeFlags = FreeBlock->Flags;

                RtlpFastRemoveDedicatedFreeBlock(Heap, FreeBlock);

                Heap->TotalFreeSize -= AllocationIndex;

                BusyBlock = (PHEAP_ENTRY)FreeBlock;
                BusyBlock->Flags = HEAP_ENTRY_BUSY | (FreeFlags & HEAP_ENTRY_LAST_ENTRY);
                BusyBlock->UnusedBytes = (UCHAR)(AllocationSize - Size);
                BusyBlock->SmallTagIndex = 0;
            } else {
                InUseIndex = (ULONG)AllocationIndex >> 5;
                FreeListsInUse = &Heap->u.FreeListsInUseUlong[InUseIndex];

                FreeListsInUseUlong = *FreeListsInUse++ & ~((1 << ((ULONG)AllocationIndex & 0x1F)) - 1);

                switch (InUseIndex) {
                case 0:
                    if (FreeListsInUseUlong) {
                        FreeListHead = &Heap->FreeLists[0];
                        break;
                    }

                    FreeListsInUseUlong = *FreeListsInUse++;

                case 1:
                    if (FreeListsInUseUlong) {
                        FreeListHead = &Heap->FreeLists[32];
                        break;
                    }

                    FreeListsInUseUlong = *FreeListsInUse++;

                case 2:
                    if (FreeListsInUseUlong) {
                        FreeListHead = &Heap->FreeLists[64];
                        break;
                    }

                    FreeListsInUseUlong = *FreeListsInUse++;

                case 3:
                    if (FreeListsInUseUlong) {
                        FreeListHead = &Heap->FreeLists[96];
                        break;
                    }

                default:
                    goto LookInNonDedicatedList;
                }

                FreeListHead += RtlFindFirstSetRightMember(FreeListsInUseUlong);

                FreeBlock = CONTAINING_RECORD(FreeListHead->Blink, HEAP_FREE_ENTRY, FreeList);

                RtlpFastRemoveDedicatedFreeBlock(Heap, FreeBlock);

SplitFreeBlock:
                FreeFlags = FreeBlock->Flags;
                Heap->TotalFreeSize -= FreeBlock->Size;

                BusyBlock = (PHEAP_ENTRY)FreeBlock;
                BusyBlock->Flags = HEAP_ENTRY_BUSY;

                FreeSize = BusyBlock->Size - AllocationIndex;

                BusyBlock->Size = (USHORT)AllocationIndex;
                BusyBlock->UnusedBytes = (UCHAR)(AllocationSize - Size);
                BusyBlock->SmallTagIndex = 0;

                if (FreeSize != 0) {
                    if (FreeSize == 1) {
                        BusyBlock->Size += 1;
                        BusyBlock->UnusedBytes += HEAP_GRANULARITY;
                    } else {
                        SplitBlock = (PHEAP_FREE_ENTRY)(BusyBlock + AllocationIndex);

                        SplitBlock->Flags = FreeFlags;
                        SplitBlock->PreviousSize = (USHORT)AllocationIndex;
                        SplitBlock->SegmentIndex = BusyBlock->SegmentIndex;
                        SplitBlock->Size = (USHORT)FreeSize;

                        if (FreeFlags & HEAP_ENTRY_LAST_ENTRY) {
                            RtlpFastInsertFreeBlockDirect(Heap, SplitBlock, (USHORT)FreeSize);
                            Heap->TotalFreeSize += FreeSize;
                        } else {
                            SplitBlock2 = (PHEAP_FREE_ENTRY)((PHEAP_ENTRY)SplitBlock + FreeSize);

                            if (SplitBlock2->Flags & HEAP_ENTRY_BUSY) {
                                SplitBlock2->PreviousSize = (USHORT)FreeSize;

                                RtlpFastInsertFreeBlockDirect(Heap, SplitBlock, (USHORT)FreeSize);
                                Heap->TotalFreeSize += FreeSize;
                            } else {
                                SplitBlock->Flags = SplitBlock2->Flags;

                                RtlpFastRemoveFreeBlock(Heap, SplitBlock2);

                                Heap->TotalFreeSize -= SplitBlock2->Size;
                                FreeSize += SplitBlock2->Size;

                                if (FreeSize <= HEAP_MAXIMUM_BLOCK_SIZE) {
                                    SplitBlock->Size = (USHORT)FreeSize;

                                    if (!(SplitBlock->Flags & HEAP_ENTRY_LAST_ENTRY)) {
                                        ((PHEAP_ENTRY)SplitBlock + FreeSize)->PreviousSize = (USHORT)FreeSize;
                                    }

                                    RtlpFastInsertFreeBlockDirect(Heap, SplitBlock, (USHORT)FreeSize);
                                    Heap->TotalFreeSize += FreeSize;
                                } else {
                                    RtlpInsertFreeBlock(Heap, SplitBlock, FreeSize);
                                }
                            }
                        }

                        FreeFlags = 0;

                        if (SplitBlock->Flags & HEAP_ENTRY_LAST_ENTRY) {
                            Segment = Heap->Segments[SplitBlock->SegmentIndex];
                            Segment->LastEntryInSegment = (PHEAP_ENTRY)SplitBlock;
                        }
                    }
                }

                if (FreeFlags & HEAP_ENTRY_LAST_ENTRY) {
                    BusyBlock->Flags |= HEAP_ENTRY_LAST_ENTRY;
                }
            }

            ReturnValue = BusyBlock + 1;

            if (LockAcquired) {
                RtlReleaseLockRoutine(Heap->LockVariable);
                LockAcquired = FALSE;
            }

            if (Flags & HEAP_ZERO_MEMORY) {
                RtlZeroMemory(ReturnValue, Size);
            }

            leave;
        } else if (AllocationIndex <= Heap->VirtualMemoryThreshold) {
LookInNonDedicatedList:
            FreeListHead = &Heap->FreeLists[0];

            Next = FreeListHead->Blink;

            if (FreeListHead != Next) {
                FreeBlock = CONTAINING_RECORD(Next, HEAP_FREE_ENTRY, FreeList);

                if (FreeBlock->Size >= AllocationIndex) {
                    Next = FreeListHead->Flink;

                    while (FreeListHead != Next) {
                        FreeBlock = CONTAINING_RECORD(Next, HEAP_FREE_ENTRY, FreeList);

                        if (FreeBlock->Size >= AllocationIndex) {
                            RtlpFastRemoveNonDedicatedFreeBlock(Heap, FreeBlock);

                            goto SplitFreeBlock;
                        }

                        Next = Next->Flink;
                    }
                }
            }

            FreeBlock = RtlpExtendHeap(Heap, AllocationSize);

            if (FreeBlock != NULL) {
                RtlpFastRemoveNonDedicatedFreeBlock(Heap, FreeBlock);

                goto SplitFreeBlock;
            }

            Status = STATUS_NO_MEMORY;
        } else if (Heap->Flags & HEAP_GROWABLE) {
            PHEAP_VIRTUAL_ALLOC_ENTRY VirtualAllocBlock;

            VirtualAllocBlock = NULL;

            AllocationSize += FIELD_OFFSET(HEAP_VIRTUAL_ALLOC_ENTRY, BusyBlock);

            Status = NtAllocateVirtualMemory((PVOID *)&VirtualAllocBlock,
                                             0,
                                             &AllocationSize,
                                             MEM_COMMIT | ((Flags & HEAP_ZERO_MEMORY) ? 0 : MEM_NOZERO),
                                             PAGE_READWRITE);

            if (NT_SUCCESS(Status)) {
                RtlZeroMemory(VirtualAllocBlock, sizeof(*VirtualAllocBlock));

                VirtualAllocBlock->BusyBlock.Size = (USHORT)(AllocationSize - Size);
                VirtualAllocBlock->BusyBlock.Flags = HEAP_ENTRY_VIRTUAL_ALLOC | HEAP_ENTRY_EXTRA_PRESENT | HEAP_ENTRY_BUSY;
                VirtualAllocBlock->CommitSize = AllocationSize;
                VirtualAllocBlock->ReserveSize = AllocationSize;

                InsertTailList(&Heap->VirtualAllocdBlocks, &VirtualAllocBlock->Entry);

                ReturnValue = (PHEAP_ENTRY)(VirtualAllocBlock + 1);

                leave;
            }
        } else {
            Status = STATUS_BUFFER_TOO_SMALL;
        }

        if (Flags & HEAP_GENERATE_EXCEPTIONS) {
            ExceptionRecord.ExceptionCode = STATUS_NO_MEMORY;
            ExceptionRecord.ExceptionRecord = (PEXCEPTION_RECORD)NULL;
            ExceptionRecord.NumberParameters = 1;
            ExceptionRecord.ExceptionFlags = 0;
            ExceptionRecord.ExceptionInformation[0] = AllocationSize;

            RtlRaiseException(&ExceptionRecord);
        }

        ReturnValue = NULL;
    } finally {
        if (LockAcquired) {
            RtlReleaseLockRoutine(Heap->LockVariable);
        }
    }

    if (ReturnValue != NULL) {
        DmTell_InsertAllocationEntry(ReturnValue, Size, 0);
    }

    return ReturnValue;
}

PVOID NTAPI RtlAllocateHeapSlowly(
    IN PVOID HeapHandle,
    IN ULONG Flags,
    IN SIZE_T Size
    )
{
    PHEAP Heap = (PHEAP)HeapHandle;
    BOOLEAN LockAcquired = FALSE;
    PVOID ReturnValue = NULL;
    PULONG FreeListsInUse;
    ULONG FreeListsInUseUlong;
    SIZE_T AllocationSize;
    SIZE_T FreeSize, AllocationIndex;
    PLIST_ENTRY FreeListHead, Next;
    PHEAP_ENTRY BusyBlock;
    PHEAP_FREE_ENTRY FreeBlock, SplitBlock, SplitBlock2;
    PHEAP_ENTRY_EXTRA ExtraStuff;
    NTSTATUS Status;
    EXCEPTION_RECORD ExceptionRecord;
    SIZE_T ZeroSize = 0;
    UCHAR EntryFlags, FreeFlags;
    PHEAP_SEGMENT Segment;
    PHEAP_VIRTUAL_ALLOC_ENTRY VirtualAllocBlock;

    if (Size > 0x7fffffff) {
        return NULL;
    }

    AllocationSize = ((Size ? Size : 1) + Heap->AlignRound) & Heap->AlignMask;

    EntryFlags = (UCHAR)(HEAP_ENTRY_BUSY | ((Flags & HEAP_SETTABLE_USER_FLAGS) >> 4));

    if ((Flags & HEAP_NEED_EXTRA_FLAGS) || (Heap->PseudoTagEntries != NULL)) {
        EntryFlags |= HEAP_ENTRY_EXTRA_PRESENT;
        AllocationSize += sizeof(HEAP_ENTRY_EXTRA);
    }

    AllocationIndex = AllocationSize >> HEAP_GRANULARITY_SHIFT;

    try {
        if (!(Flags & HEAP_NO_SERIALIZE)) {
            RtlAcquireLockRoutine(Heap->LockVariable);
            LockAcquired = TRUE;
        }

        try {
            if (AllocationIndex < HEAP_MAXIMUM_FREELISTS) {
                FreeListHead = &Heap->FreeLists[AllocationIndex];

                if (!IsListEmpty(FreeListHead)) {
                    FreeBlock = CONTAINING_RECORD(FreeListHead->Flink, HEAP_FREE_ENTRY, FreeList);
                    FreeFlags = FreeBlock->Flags;

                    RtlpRemoveFreeBlock(Heap, FreeBlock);

                    Heap->TotalFreeSize -= AllocationIndex;

                    BusyBlock = (PHEAP_ENTRY)FreeBlock;
                    BusyBlock->Flags = EntryFlags | (FreeFlags & HEAP_ENTRY_LAST_ENTRY);
                    BusyBlock->UnusedBytes = (UCHAR)(AllocationSize - Size);
                } else {
                    if (AllocationIndex < 32) {
                        FreeListsInUse = &Heap->u.FreeListsInUseUlong[0];
                        FreeListsInUseUlong = *FreeListsInUse++ >> ((ULONG)AllocationIndex & 0x1F);

                        if (FreeListsInUseUlong) {
                            FreeListHead += RtlFindFirstSetRightMember(FreeListsInUseUlong);
                        } else {
                            FreeListsInUseUlong = *FreeListsInUse++;

                            if (FreeListsInUseUlong) {
                                FreeListHead += RtlFindFirstSetRightMember(FreeListsInUseUlong) + 32 - (AllocationIndex & 0x1F);
                            } else {
                                FreeListsInUseUlong = *FreeListsInUse++;

                                if (FreeListsInUseUlong) {
                                    FreeListHead += RtlFindFirstSetRightMember(FreeListsInUseUlong) + 64 - (AllocationIndex & 0x1F);
                                } else {
                                    FreeListsInUseUlong = *FreeListsInUse++;

                                    if (FreeListsInUseUlong) {
                                        FreeListHead += RtlFindFirstSetRightMember(FreeListsInUseUlong) + 96 - (AllocationIndex & 0x1F);
                                    } else {
                                        goto LookInNonDedicatedList;
                                    }
                                }
                            }
                        }
                    } else if (AllocationIndex < 64) {
                        FreeListsInUse = &Heap->u.FreeListsInUseUlong[1];
                        FreeListsInUseUlong = *FreeListsInUse++ >> ((ULONG)AllocationIndex & 0x1F);

                        if (FreeListsInUseUlong) {
                            FreeListHead += RtlFindFirstSetRightMember(FreeListsInUseUlong);
                        } else {
                            FreeListsInUseUlong = *FreeListsInUse++;

                            if (FreeListsInUseUlong) {
                                FreeListHead += RtlFindFirstSetRightMember(FreeListsInUseUlong) + 32 - (AllocationIndex & 0x1F);
                            } else {
                                FreeListsInUseUlong = *FreeListsInUse++;

                                if (FreeListsInUseUlong) {
                                    FreeListHead += RtlFindFirstSetRightMember(FreeListsInUseUlong) + 64 - (AllocationIndex & 0x1F);
                                } else {
                                    goto LookInNonDedicatedList;
                                }
                            }
                        }
                    } else if (AllocationIndex < 96) {
                        FreeListsInUse = &Heap->u.FreeListsInUseUlong[2];
                        FreeListsInUseUlong = *FreeListsInUse++ >> ((ULONG)AllocationIndex & 0x1F);

                        if (FreeListsInUseUlong) {
                            FreeListHead += RtlFindFirstSetRightMember(FreeListsInUseUlong);
                        } else {
                            FreeListsInUseUlong = *FreeListsInUse++;

                            if (FreeListsInUseUlong) {
                                FreeListHead += RtlFindFirstSetRightMember(FreeListsInUseUlong) + 32 - (AllocationIndex & 0x1F);
                            } else {
                                goto LookInNonDedicatedList;
                            }
                        }
                    } else {
                        FreeListsInUse = &Heap->u.FreeListsInUseUlong[3];
                        FreeListsInUseUlong = *FreeListsInUse++ >> ((ULONG)AllocationIndex & 0x1F);

                        if (FreeListsInUseUlong) {
                            FreeListHead += RtlFindFirstSetRightMember(FreeListsInUseUlong);
                        } else {
                            goto LookInNonDedicatedList;
                        }
                    }

                    FreeBlock = CONTAINING_RECORD(FreeListHead->Flink, HEAP_FREE_ENTRY, FreeList);

SplitFreeBlock:
                    FreeFlags = FreeBlock->Flags;

                    RtlpRemoveFreeBlock(Heap, FreeBlock);

                    Heap->TotalFreeSize -= FreeBlock->Size;

                    BusyBlock = (PHEAP_ENTRY)FreeBlock;
                    BusyBlock->Flags = EntryFlags;

                    FreeSize = BusyBlock->Size - AllocationIndex;

                    BusyBlock->Size = (USHORT)AllocationIndex;
                    BusyBlock->UnusedBytes = (UCHAR)(AllocationSize - Size);

                    if (FreeSize != 0) {
                        if (FreeSize == 1) {
                            BusyBlock->Size += 1;
                            BusyBlock->UnusedBytes += HEAP_GRANULARITY;
                        } else {
                            SplitBlock = (PHEAP_FREE_ENTRY)(BusyBlock + AllocationIndex);

                            SplitBlock->Flags = FreeFlags;
                            SplitBlock->PreviousSize = (USHORT)AllocationIndex;
                            SplitBlock->SegmentIndex = BusyBlock->SegmentIndex;
                            SplitBlock->Size = (USHORT)FreeSize;

                            if (FreeFlags & HEAP_ENTRY_LAST_ENTRY) {
                                RtlpInsertFreeBlockDirect(Heap, SplitBlock, (USHORT)FreeSize);
                                Heap->TotalFreeSize += FreeSize;
                            } else {
                                SplitBlock2 = (PHEAP_FREE_ENTRY)((PHEAP_ENTRY)SplitBlock + FreeSize);

                                if (SplitBlock2->Flags & HEAP_ENTRY_BUSY) {
                                    SplitBlock2->PreviousSize = (USHORT)FreeSize;

                                    RtlpInsertFreeBlockDirect(Heap, SplitBlock, (USHORT)FreeSize);
                                    Heap->TotalFreeSize += FreeSize;
                                } else {
                                    SplitBlock->Flags = SplitBlock2->Flags;

                                    RtlpRemoveFreeBlock(Heap, SplitBlock2);

                                    Heap->TotalFreeSize -= SplitBlock2->Size;
                                    FreeSize += SplitBlock2->Size;

                                    if (FreeSize <= HEAP_MAXIMUM_BLOCK_SIZE) {
                                        SplitBlock->Size = (USHORT)FreeSize;

                                        if (!(SplitBlock->Flags & HEAP_ENTRY_LAST_ENTRY)) {
                                            ((PHEAP_ENTRY)SplitBlock + FreeSize)->PreviousSize = (USHORT)FreeSize;
                                        }

                                        RtlpInsertFreeBlockDirect(Heap, SplitBlock, (USHORT)FreeSize);
                                        Heap->TotalFreeSize += FreeSize;
                                    } else {
                                        RtlpInsertFreeBlock(Heap, SplitBlock, FreeSize);
                                    }
                                }
                            }

                            FreeFlags = 0;

                            if (SplitBlock->Flags & HEAP_ENTRY_LAST_ENTRY) {
                                Segment = Heap->Segments[SplitBlock->SegmentIndex];
                                Segment->LastEntryInSegment = (PHEAP_ENTRY)SplitBlock;
                            }
                        }
                    }

                    if (FreeFlags & HEAP_ENTRY_LAST_ENTRY) {
                        BusyBlock->Flags |= HEAP_ENTRY_LAST_ENTRY;
                    }
                }

                ReturnValue = BusyBlock + 1;

                if (Flags & HEAP_ZERO_MEMORY) {
                    ZeroSize = Size;
                }

                BusyBlock->SmallTagIndex = 0;

                if (BusyBlock->Flags & HEAP_ENTRY_EXTRA_PRESENT) {
                    ExtraStuff = RtlpGetExtraStuffPointer(BusyBlock);
                    RtlZeroMemory(ExtraStuff, sizeof(*ExtraStuff));
                }

                leave;
            } else if (AllocationIndex <= Heap->VirtualMemoryThreshold) {
LookInNonDedicatedList:
                FreeListHead = &Heap->FreeLists[0];
                Next = FreeListHead->Flink;

                while (FreeListHead != Next) {
                    FreeBlock = CONTAINING_RECORD(Next, HEAP_FREE_ENTRY, FreeList);

                    if (FreeBlock->Size >= AllocationIndex) {
                        goto SplitFreeBlock;
                    } else {
                        Next = Next->Flink;
                    }
                }

                FreeBlock = RtlpExtendHeap(Heap, AllocationSize);

                if (FreeBlock != NULL) {
                    goto SplitFreeBlock;
                }

                Status = STATUS_NO_MEMORY;
            } else if (Heap->Flags & HEAP_GROWABLE) {
                VirtualAllocBlock = NULL;

                AllocationSize += FIELD_OFFSET(HEAP_VIRTUAL_ALLOC_ENTRY, BusyBlock);

                Status = NtAllocateVirtualMemory((PVOID *)&VirtualAllocBlock,
                                                 0,
                                                 &AllocationSize,
                                                 MEM_COMMIT,
                                                 PAGE_READWRITE);

                if (NT_SUCCESS(Status)) {
                    VirtualAllocBlock->BusyBlock.Size = (USHORT)(AllocationSize - Size);
                    VirtualAllocBlock->BusyBlock.Flags = EntryFlags | HEAP_ENTRY_VIRTUAL_ALLOC | HEAP_ENTRY_EXTRA_PRESENT;
                    VirtualAllocBlock->CommitSize = AllocationSize;
                    VirtualAllocBlock->ReserveSize = AllocationSize;

                    InsertTailList(&Heap->VirtualAllocdBlocks, &VirtualAllocBlock->Entry);

                    ReturnValue = (PHEAP_ENTRY)(VirtualAllocBlock + 1);

                    leave;
                }
            } else {
                Status = STATUS_BUFFER_TOO_SMALL;
            }

            if (Flags & HEAP_GENERATE_EXCEPTIONS) {
                ExceptionRecord.ExceptionCode = STATUS_NO_MEMORY;
                ExceptionRecord.ExceptionRecord = (PEXCEPTION_RECORD)NULL;
                ExceptionRecord.NumberParameters = 1;
                ExceptionRecord.ExceptionFlags = 0;
                ExceptionRecord.ExceptionInformation[0] = AllocationSize;

                RtlRaiseException(&ExceptionRecord);
            }
        } except(GetExceptionCode() == STATUS_NO_MEMORY ? EXCEPTION_CONTINUE_SEARCH : EXCEPTION_EXECUTE_HANDLER) {
        }

        if (ZeroSize) {
            RtlZeroMemory(ReturnValue, ZeroSize);
        }
    } finally {
        if (LockAcquired) {
            RtlReleaseLockRoutine(Heap->LockVariable);
        }
    }

    return ReturnValue;
}

PVOID NTAPI RtlReAllocateHeap(
    IN PVOID HeapHandle,
    IN ULONG Flags,
    IN PVOID BaseAddress,
    IN SIZE_T Size
    )
{
    PHEAP Heap = (PHEAP)HeapHandle;
    PHEAP_ENTRY BusyBlock, NewBusyBlock;
    PHEAP_ENTRY_EXTRA OldExtraStuff, NewExtraStuff;
    SIZE_T OldSize;
    SIZE_T AllocationSize;
    BOOLEAN LockAcquired = FALSE;
    PVOID OldBaseAddress;
    SIZE_T FreeSize;
    PHEAP_FREE_ENTRY SplitBlock, SplitBlock2;
    SIZE_T AllocationIndex;
    SIZE_T OldAllocationIndex;
    UCHAR FreeFlags;
    NTSTATUS Status;
    PVOID NewBaseAddress;
    EXCEPTION_RECORD ExceptionRecord;
    PVOID DeCommitAddress;
    SIZE_T DeCommitSize;

    if (BaseAddress == NULL) {
        return NULL;
    }

    OldBaseAddress = BaseAddress;

    Flags |= Heap->ForceFlags;

    if (Size > 0x7fffffff) {
        return NULL;
    }

    AllocationSize = ((Size ? Size : 1) + Heap->AlignRound) & Heap->AlignMask;

    if ((Flags & HEAP_NEED_EXTRA_FLAGS) ||
        (Heap->PseudoTagEntries != NULL) ||
        (((PHEAP_ENTRY)BaseAddress - 1)->Flags & HEAP_ENTRY_EXTRA_PRESENT)) {
        AllocationSize += sizeof(HEAP_ENTRY_EXTRA);
    }

    try {
        if (!(Flags & HEAP_NO_SERIALIZE)) {
            RtlAcquireLockRoutine(Heap->LockVariable);
            LockAcquired = TRUE;

            Flags ^= HEAP_NO_SERIALIZE;
        }

        try {
            BusyBlock = (PHEAP_ENTRY)BaseAddress - 1;

            if (!(BusyBlock->Flags & HEAP_ENTRY_BUSY)) {
                leave;
            }

            if (BusyBlock->Flags & HEAP_ENTRY_VIRTUAL_ALLOC) {
                OldSize = RtlpGetSizeOfBigBlock(BusyBlock);
                OldAllocationIndex = (OldSize + BusyBlock->Size) >> HEAP_GRANULARITY_SHIFT;

                AllocationSize += FIELD_OFFSET(HEAP_VIRTUAL_ALLOC_ENTRY, BusyBlock);
                AllocationSize = ROUND_UP_TO_POWER2(AllocationSize, PAGE_SIZE);
            } else {
                OldAllocationIndex = BusyBlock->Size;
                OldSize = (OldAllocationIndex << HEAP_GRANULARITY_SHIFT) - BusyBlock->UnusedBytes;
            }

            AllocationIndex = AllocationSize >> HEAP_GRANULARITY_SHIFT;

            if (AllocationIndex <= OldAllocationIndex) {
                if (AllocationIndex + 1 == OldAllocationIndex) {
                    AllocationIndex += 1;
                    AllocationSize += sizeof(HEAP_ENTRY);
                }

                if (BusyBlock->Flags & HEAP_ENTRY_VIRTUAL_ALLOC) {
                    BusyBlock->Size = (USHORT)(AllocationSize - Size);
                } else if (BusyBlock->Flags & HEAP_ENTRY_EXTRA_PRESENT) {
                    OldExtraStuff = (PHEAP_ENTRY_EXTRA)(BusyBlock + BusyBlock->Size - 1);
                    NewExtraStuff = (PHEAP_ENTRY_EXTRA)(BusyBlock + AllocationIndex - 1);

                    *NewExtraStuff = *OldExtraStuff;

                    BusyBlock->UnusedBytes = (UCHAR)(AllocationSize - Size);
                } else {
                    BusyBlock->UnusedBytes = (UCHAR)(AllocationSize - Size);
                }

                if (Size > OldSize) {
                    if (Flags & HEAP_ZERO_MEMORY) {
                        RtlZeroMemory((PCHAR)BaseAddress + OldSize, Size - OldSize);
                    }
                }

                if (AllocationIndex != OldAllocationIndex) {
                    FreeFlags = BusyBlock->Flags & ~HEAP_ENTRY_BUSY;

                    if (FreeFlags & HEAP_ENTRY_VIRTUAL_ALLOC) {
                        PHEAP_VIRTUAL_ALLOC_ENTRY VirtualAllocBlock;

                        VirtualAllocBlock = CONTAINING_RECORD(BusyBlock, HEAP_VIRTUAL_ALLOC_ENTRY, BusyBlock);

                        DeCommitAddress = (PCHAR)VirtualAllocBlock + AllocationSize;
                        DeCommitSize = (OldAllocationIndex << HEAP_GRANULARITY_SHIFT) - AllocationSize;

                        Status = NtFreeVirtualMemory(&DeCommitAddress, &DeCommitSize, MEM_RELEASE);

                        if (NT_SUCCESS(Status)) {
                            VirtualAllocBlock->CommitSize -= DeCommitSize;
                        }
                    } else {
                        SplitBlock = (PHEAP_FREE_ENTRY)(BusyBlock + AllocationIndex);

                        SplitBlock->Flags = FreeFlags;
                        SplitBlock->PreviousSize = (USHORT)AllocationIndex;
                        SplitBlock->SegmentIndex = BusyBlock->SegmentIndex;

                        FreeSize = BusyBlock->Size - AllocationIndex;

                        BusyBlock->Size = (USHORT)AllocationIndex;
                        BusyBlock->Flags &= ~HEAP_ENTRY_LAST_ENTRY;

                        if (FreeFlags & HEAP_ENTRY_LAST_ENTRY) {
                            PHEAP_SEGMENT Segment;

                            Segment = Heap->Segments[SplitBlock->SegmentIndex];
                            Segment->LastEntryInSegment = (PHEAP_ENTRY)SplitBlock;

                            SplitBlock->Size = (USHORT)FreeSize;

                            RtlpInsertFreeBlockDirect(Heap, SplitBlock, (USHORT)FreeSize);
                            Heap->TotalFreeSize += FreeSize;
                        } else {
                            SplitBlock2 = (PHEAP_FREE_ENTRY)((PHEAP_ENTRY)SplitBlock + FreeSize);

                            if (SplitBlock2->Flags & HEAP_ENTRY_BUSY) {
                                SplitBlock->Size = (USHORT)FreeSize;
                                ((PHEAP_ENTRY)SplitBlock + FreeSize)->PreviousSize = (USHORT)FreeSize;

                                RtlpInsertFreeBlockDirect(Heap, SplitBlock, (USHORT)FreeSize);
                                Heap->TotalFreeSize += FreeSize;
                            } else {
                                SplitBlock->Flags = SplitBlock2->Flags;

                                RtlpRemoveFreeBlock(Heap, SplitBlock2);

                                Heap->TotalFreeSize -= SplitBlock2->Size;
                                FreeSize += SplitBlock2->Size;

                                if (FreeSize <= HEAP_MAXIMUM_BLOCK_SIZE) {
                                    SplitBlock->Size = (USHORT)FreeSize;

                                    if (!(SplitBlock->Flags & HEAP_ENTRY_LAST_ENTRY)) {
                                        ((PHEAP_ENTRY)SplitBlock + FreeSize)->PreviousSize = (USHORT)FreeSize;
                                    } else {
                                        PHEAP_SEGMENT Segment;

                                        Segment = Heap->Segments[SplitBlock->SegmentIndex];
                                        Segment->LastEntryInSegment = (PHEAP_ENTRY)SplitBlock;
                                    }

                                    RtlpInsertFreeBlockDirect(Heap, SplitBlock, (USHORT)FreeSize);
                                    Heap->TotalFreeSize += FreeSize;
                                } else {
                                    RtlpInsertFreeBlock(Heap, SplitBlock, FreeSize);
                                }
                            }
                        }
                    }
                }
            } else {
                if (!(BusyBlock->Flags & HEAP_ENTRY_VIRTUAL_ALLOC) &&
                    RtlpGrowBlockInPlace(Heap, Flags, BusyBlock, Size, AllocationIndex)) {
                    ;
                } else if (Flags & HEAP_REALLOC_IN_PLACE_ONLY) {
                    BaseAddress = NULL;
                } else {
                    Flags &= ~HEAP_TAG_MASK;

                    if (BusyBlock->Flags & HEAP_ENTRY_EXTRA_PRESENT) {
                        Flags &= ~HEAP_SETTABLE_USER_FLAGS;
                        Flags |= HEAP_SETTABLE_USER_VALUE | ((BusyBlock->Flags & HEAP_ENTRY_SETTABLE_FLAGS) << 4);

                        OldExtraStuff = RtlpGetExtraStuffPointer(BusyBlock);

                        try {
                            if ((OldExtraStuff->TagIndex != 0) &&
                                !(OldExtraStuff->TagIndex & HEAP_PSEUDO_TAG_FLAG)) {
                                Flags |= OldExtraStuff->TagIndex << HEAP_TAG_SHIFT;
                            }
                        } except(EXCEPTION_EXECUTE_HANDLER) {
                            BusyBlock->Flags &= ~HEAP_ENTRY_EXTRA_PRESENT;
                        }
                    } else if (BusyBlock->SmallTagIndex) {
                        Flags |= BusyBlock->SmallTagIndex << HEAP_TAG_SHIFT;
                    }

                    NewBaseAddress = RtlAllocateHeap(HeapHandle, Flags & ~HEAP_ZERO_MEMORY, Size);

                    if (NewBaseAddress != NULL) {
                        NewBusyBlock = (PHEAP_ENTRY)NewBaseAddress - 1;

                        if (NewBusyBlock->Flags & HEAP_ENTRY_EXTRA_PRESENT) {
                            NewExtraStuff = RtlpGetExtraStuffPointer(NewBusyBlock);

                            if (BusyBlock->Flags & HEAP_ENTRY_EXTRA_PRESENT) {
                                OldExtraStuff = RtlpGetExtraStuffPointer(BusyBlock);

                                NewExtraStuff->Settable = OldExtraStuff->Settable;
                            } else {
                                RtlZeroMemory(NewExtraStuff, sizeof(*NewExtraStuff));
                            }
                        }

                        RtlMoveMemory(NewBaseAddress, BaseAddress, Size < OldSize ? Size : OldSize);

                        if ((Size > OldSize) && (Flags & HEAP_ZERO_MEMORY)) {
                            RtlZeroMemory((PCHAR)NewBaseAddress + OldSize, Size - OldSize);
                        }

                        RtlFreeHeap(HeapHandle, Flags, BaseAddress);
                    }

                    BaseAddress = NewBaseAddress;
                }
            }

            if ((BaseAddress == NULL) && (Flags & HEAP_GENERATE_EXCEPTIONS)) {
                ExceptionRecord.ExceptionCode = STATUS_NO_MEMORY;
                ExceptionRecord.ExceptionRecord = (PEXCEPTION_RECORD)NULL;
                ExceptionRecord.NumberParameters = 1;
                ExceptionRecord.ExceptionFlags = 0;
                ExceptionRecord.ExceptionInformation[0] = AllocationSize;

                RtlRaiseException(&ExceptionRecord);
            }
        } except(GetExceptionCode() == STATUS_NO_MEMORY ? EXCEPTION_CONTINUE_SEARCH : EXCEPTION_EXECUTE_HANDLER) {
            BaseAddress = NULL;
        }
    } finally {
        if (LockAcquired) {
            RtlReleaseLockRoutine(Heap->LockVariable);
        }
    }

    if (OldBaseAddress == BaseAddress) {
        DmTell_RemoveAllocationEntry(BaseAddress, 0, 0);
        DmTell_InsertAllocationEntry(BaseAddress, Size, 0);
    }

    return BaseAddress;
}

BOOLEAN NTAPI RtlpGrowBlockInPlace(
    IN PHEAP Heap,
    IN ULONG Flags,
    IN PHEAP_ENTRY BusyBlock,
    IN SIZE_T Size,
    IN SIZE_T AllocationIndex
    )
{
    SIZE_T FreeSize, OldSize;
    UCHAR EntryFlags, FreeFlags;
    PHEAP_FREE_ENTRY FreeBlock, SplitBlock, SplitBlock2;
    PHEAP_ENTRY_EXTRA OldExtraStuff, NewExtraStuff;

    if (AllocationIndex > Heap->VirtualMemoryThreshold) {
        return FALSE;
    }

    EntryFlags = BusyBlock->Flags;
    FreeBlock = (PHEAP_FREE_ENTRY)(BusyBlock + BusyBlock->Size);

    if (EntryFlags & HEAP_ENTRY_LAST_ENTRY) {
        FreeSize = (AllocationIndex - BusyBlock->Size) << HEAP_GRANULARITY_SHIFT;
        FreeSize = ROUND_UP_TO_POWER2(FreeSize, PAGE_SIZE);

        FreeBlock = RtlpFindAndCommitPages(Heap, Heap->Segments[BusyBlock->SegmentIndex], &FreeSize, (PHEAP_ENTRY)FreeBlock);

        if (FreeBlock == NULL) {
            return FALSE;
        }

        FreeSize = FreeSize >> HEAP_GRANULARITY_SHIFT;

        FreeBlock = RtlpCoalesceFreeBlocks(Heap, FreeBlock, &FreeSize, FALSE);

        FreeFlags = FreeBlock->Flags;

        if (FreeSize + BusyBlock->Size < AllocationIndex) {
            RtlpInsertFreeBlock(Heap, FreeBlock, FreeSize);
            Heap->TotalFreeSize += FreeSize;

            return FALSE;
        }

        FreeSize += BusyBlock->Size;
    } else {
        FreeFlags = FreeBlock->Flags;

        if (FreeFlags & HEAP_ENTRY_BUSY) {
            return FALSE;
        }

        FreeSize = BusyBlock->Size + FreeBlock->Size;

        if (FreeSize < AllocationIndex) {
            return FALSE;
        }

        RtlpRemoveFreeBlock(Heap, FreeBlock);

        Heap->TotalFreeSize -= FreeBlock->Size;
    }

    OldSize = (BusyBlock->Size << HEAP_GRANULARITY_SHIFT) - BusyBlock->UnusedBytes;

    FreeSize -= AllocationIndex;

    if (FreeSize <= 2) {
        AllocationIndex += FreeSize;
        FreeSize = 0;
    }

    if (EntryFlags & HEAP_ENTRY_EXTRA_PRESENT) {
        OldExtraStuff = (PHEAP_ENTRY_EXTRA)(BusyBlock + BusyBlock->Size - 1);
        NewExtraStuff = (PHEAP_ENTRY_EXTRA)(BusyBlock + AllocationIndex - 1);

        *NewExtraStuff = *OldExtraStuff;
    }

    if (FreeSize == 0) {
        BusyBlock->Flags |= FreeFlags & HEAP_ENTRY_LAST_ENTRY;
        BusyBlock->Size = (USHORT)AllocationIndex;
        BusyBlock->UnusedBytes = (UCHAR)((AllocationIndex << HEAP_GRANULARITY_SHIFT) - Size);

        if (!(FreeFlags & HEAP_ENTRY_LAST_ENTRY)) {
            (BusyBlock + BusyBlock->Size)->PreviousSize = BusyBlock->Size;
        } else {
            Heap->Segments[BusyBlock->SegmentIndex]->LastEntryInSegment = BusyBlock;
        }
    } else {
        BusyBlock->Size = (USHORT)AllocationIndex;
        BusyBlock->UnusedBytes = (UCHAR)((AllocationIndex << HEAP_GRANULARITY_SHIFT) - Size);

        SplitBlock = (PHEAP_FREE_ENTRY)(BusyBlock + AllocationIndex);

        SplitBlock->PreviousSize = (USHORT)AllocationIndex;
        SplitBlock->SegmentIndex = BusyBlock->SegmentIndex;

        if (FreeFlags & HEAP_ENTRY_LAST_ENTRY) {
            Heap->Segments[SplitBlock->SegmentIndex]->LastEntryInSegment = (PHEAP_ENTRY)SplitBlock;

            SplitBlock->Flags = FreeFlags;
            SplitBlock->Size = (USHORT)FreeSize;

            RtlpInsertFreeBlockDirect(Heap, SplitBlock, (USHORT)FreeSize);
            Heap->TotalFreeSize += FreeSize;
        } else {
            SplitBlock2 = (PHEAP_FREE_ENTRY)((PHEAP_ENTRY)SplitBlock + FreeSize);

            if (SplitBlock2->Flags & HEAP_ENTRY_BUSY) {
                SplitBlock->Flags = FreeFlags & ~HEAP_ENTRY_LAST_ENTRY;
                SplitBlock->Size = (USHORT)FreeSize;

                ((PHEAP_ENTRY)SplitBlock + FreeSize)->PreviousSize = (USHORT)FreeSize;

                RtlpInsertFreeBlockDirect(Heap, SplitBlock, (USHORT)FreeSize);
                Heap->TotalFreeSize += FreeSize;
            } else {
                FreeFlags = SplitBlock2->Flags;

                RtlpRemoveFreeBlock(Heap, SplitBlock2);

                Heap->TotalFreeSize -= SplitBlock2->Size;
                FreeSize += SplitBlock2->Size;

                SplitBlock->Flags = FreeFlags;

                if (FreeSize <= HEAP_MAXIMUM_BLOCK_SIZE) {
                    SplitBlock->Size = (USHORT)FreeSize;

                    if (!(FreeFlags & HEAP_ENTRY_LAST_ENTRY)) {
                        ((PHEAP_ENTRY)SplitBlock + FreeSize)->PreviousSize = (USHORT)FreeSize;
                    } else {
                        Heap->Segments[SplitBlock->SegmentIndex]->LastEntryInSegment = (PHEAP_ENTRY)SplitBlock;
                    }

                    RtlpInsertFreeBlockDirect(Heap, SplitBlock, (USHORT)FreeSize);
                    Heap->TotalFreeSize += FreeSize;
                } else {
                    RtlpInsertFreeBlock(Heap, SplitBlock, FreeSize);
                }
            }
        }
    }

    if (Flags & HEAP_ZERO_MEMORY) {
        if (Size > OldSize) {
            RtlZeroMemory((PCHAR)(BusyBlock + 1) + OldSize, Size - OldSize);
        }
    }

    BusyBlock->Flags &= ~HEAP_ENTRY_SETTABLE_FLAGS;
    BusyBlock->Flags |= ((Flags & HEAP_SETTABLE_USER_FLAGS) >> 4);

    return TRUE;
}

BOOLEAN NTAPI RtlFreeHeap(
    IN PVOID HeapHandle,
    IN ULONG Flags,
    IN PVOID BaseAddress
    )
{
    NTSTATUS Status;
    PHEAP Heap = (PHEAP)HeapHandle;
    PHEAP_ENTRY BusyBlock;
    SIZE_T FreeSize;
    BOOLEAN LockAcquired = FALSE;
    BOOLEAN ReturnValue = TRUE;

    if (BaseAddress == NULL) {
        return TRUE;
    }

    DmTell_RemoveAllocationEntry(BaseAddress, 0, 0);

    Flags |= Heap->ForceFlags;

    BusyBlock = (PHEAP_ENTRY)BaseAddress - 1;

    try {
        if (!(Flags & HEAP_NO_SERIALIZE)) {
            RtlAcquireLockRoutine(Heap->LockVariable);
            LockAcquired = TRUE;
        }

        if (!(BusyBlock->Flags & HEAP_ENTRY_VIRTUAL_ALLOC)) {
            FreeSize = BusyBlock->Size;

            BusyBlock = (PHEAP_ENTRY)RtlpCoalesceFreeBlocks(Heap, (PHEAP_FREE_ENTRY)BusyBlock, &FreeSize, FALSE);

            if (FreeSize < HEAP_MAXIMUM_FREELISTS) {
                RtlpFastInsertDedicatedFreeBlockDirect(Heap, (PHEAP_FREE_ENTRY)BusyBlock, (USHORT)FreeSize);

                Heap->TotalFreeSize += FreeSize;
            } else if ((FreeSize < Heap->DeCommitFreeBlockThreshold) ||
                       ((Heap->TotalFreeSize + FreeSize) < Heap->DeCommitTotalFreeThreshold)) {
                if (FreeSize <= HEAP_MAXIMUM_BLOCK_SIZE) {
                    RtlpFastInsertNonDedicatedFreeBlockDirect(Heap, (PHEAP_FREE_ENTRY)BusyBlock, (USHORT)FreeSize);

                    Heap->TotalFreeSize += FreeSize;
                } else {
                    RtlpInsertFreeBlock(Heap, (PHEAP_FREE_ENTRY)BusyBlock, FreeSize);
                }
            } else {
                RtlpDeCommitFreeBlock(Heap, (PHEAP_FREE_ENTRY)BusyBlock, FreeSize);
            }
        } else {
            PHEAP_VIRTUAL_ALLOC_ENTRY VirtualAllocBlock;

            VirtualAllocBlock = CONTAINING_RECORD(BusyBlock, HEAP_VIRTUAL_ALLOC_ENTRY, BusyBlock);

            RemoveEntryList(&VirtualAllocBlock->Entry);

            if (LockAcquired) {
                RtlReleaseLockRoutine(Heap->LockVariable);
                LockAcquired = FALSE;
            }

            FreeSize = 0;

            Status = NtFreeVirtualMemory((PVOID *)&VirtualAllocBlock, &FreeSize, MEM_RELEASE);

            if (!NT_SUCCESS(Status)) {
                ReturnValue = FALSE;
            }
        }
    } finally {
        if (LockAcquired) {
            RtlReleaseLockRoutine(Heap->LockVariable);
        }
    }

    return ReturnValue;
}

BOOLEAN NTAPI RtlFreeHeapSlowly(
    IN PVOID HeapHandle,
    IN ULONG Flags,
    IN PVOID BaseAddress
    )
{
    NTSTATUS Status;
    PHEAP Heap = (PHEAP)HeapHandle;
    PHEAP_ENTRY BusyBlock;
    SIZE_T FreeSize;
    PHEAP_VIRTUAL_ALLOC_ENTRY VirtualAllocBlock;
    BOOLEAN LockAcquired = FALSE;
    BOOLEAN Result;

    Result = FALSE;

    try {
        if (!(Flags & HEAP_NO_SERIALIZE)) {
            RtlAcquireLockRoutine(Heap->LockVariable);
            LockAcquired = TRUE;
        }

        try {
            BusyBlock = (PHEAP_ENTRY)BaseAddress - 1;

            if ((BusyBlock->Flags & HEAP_ENTRY_BUSY) &&
                (((ULONG_PTR)BaseAddress & 0x7) == 0) &&
                (BusyBlock->SegmentIndex < HEAP_MAXIMUM_SEGMENTS)) {
                if (BusyBlock->Flags & HEAP_ENTRY_VIRTUAL_ALLOC) {
                    VirtualAllocBlock = CONTAINING_RECORD(BusyBlock, HEAP_VIRTUAL_ALLOC_ENTRY, BusyBlock);

                    RemoveEntryList(&VirtualAllocBlock->Entry);

                    FreeSize = 0;

                    Status = NtFreeVirtualMemory((PVOID *)&VirtualAllocBlock, &FreeSize, MEM_RELEASE);

                    if (!NT_SUCCESS(Status)) {
                        leave;
                    }
                } else {
                    FreeSize = BusyBlock->Size;

                    BusyBlock = (PHEAP_ENTRY)RtlpCoalesceFreeBlocks(Heap, (PHEAP_FREE_ENTRY)BusyBlock, &FreeSize, FALSE);

                    if ((FreeSize < Heap->DeCommitFreeBlockThreshold) ||
                        ((Heap->TotalFreeSize + FreeSize) < Heap->DeCommitTotalFreeThreshold)) {
                        if (FreeSize <= HEAP_MAXIMUM_BLOCK_SIZE) {
                            RtlpInsertFreeBlockDirect(Heap, (PHEAP_FREE_ENTRY)BusyBlock, (USHORT)FreeSize);

                            Heap->TotalFreeSize += FreeSize;
                        } else {
                            RtlpInsertFreeBlock(Heap, (PHEAP_FREE_ENTRY)BusyBlock, FreeSize);
                        }
                    } else {
                        RtlpDeCommitFreeBlock(Heap, (PHEAP_FREE_ENTRY)BusyBlock, FreeSize);
                    }
                }

                Result = TRUE;
            }
        } except(EXCEPTION_EXECUTE_HANDLER) {
            Result = FALSE;
        }
    } finally {
        if (LockAcquired) {
            RtlReleaseLockRoutine(Heap->LockVariable);
        }
    }

    return Result;
}

SIZE_T NTAPI RtlSizeHeap(
    IN PVOID HeapHandle,
    IN ULONG Flags,
    IN PVOID BaseAddress
    )
{
    PHEAP_ENTRY BusyBlock;
    SIZE_T BusyBlockSize;

    BusyBlock = (PHEAP_ENTRY)BaseAddress - 1;

    if (!(BusyBlock->Flags & HEAP_ENTRY_BUSY)) {
        BusyBlockSize = -1;
    } else if (BusyBlock->Flags & HEAP_ENTRY_VIRTUAL_ALLOC) {
        BusyBlockSize = RtlpGetSizeOfBigBlock(BusyBlock);
    } else {
        BusyBlockSize = (BusyBlock->Size << HEAP_GRANULARITY_SHIFT) - BusyBlock->UnusedBytes;
    }

    return BusyBlockSize;
}

NTSTATUS NTAPI RtlZeroHeap(
    IN PVOID HeapHandle,
    IN ULONG Flags
    )
{
    PHEAP Heap = (PHEAP)HeapHandle;
    NTSTATUS Status;
    BOOLEAN LockAcquired = FALSE;
    PHEAP_SEGMENT Segment;
    ULONG SegmentIndex;
    PHEAP_ENTRY CurrentBlock;
    PHEAP_FREE_ENTRY FreeBlock;
    SIZE_T Size;
    PHEAP_UNCOMMMTTED_RANGE UnCommittedRange;

    Flags |= Heap->ForceFlags;

    Status = STATUS_SUCCESS;

    try {
        if (!(Flags & HEAP_NO_SERIALIZE)) {
            RtlAcquireLockRoutine(Heap->LockVariable);
            LockAcquired = TRUE;
        }

        try {
            for (SegmentIndex = 0; SegmentIndex < HEAP_MAXIMUM_SEGMENTS; SegmentIndex++) {
                Segment = Heap->Segments[SegmentIndex];

                if (!Segment) {
                    continue;
                }

                UnCommittedRange = Segment->UnCommittedRanges;
                CurrentBlock = Segment->FirstEntry;

                while (CurrentBlock < Segment->LastValidEntry) {
                    Size = CurrentBlock->Size << HEAP_GRANULARITY_SHIFT;

                    if (!(CurrentBlock->Flags & HEAP_ENTRY_BUSY)) {
                        FreeBlock = (PHEAP_FREE_ENTRY)CurrentBlock;

                        RtlFillMemoryUlong((PCHAR)(FreeBlock + 1), Size - sizeof(*FreeBlock), 0);
                    }

                    if (CurrentBlock->Flags & HEAP_ENTRY_LAST_ENTRY) {
                        CurrentBlock += CurrentBlock->Size;

                        if (UnCommittedRange == NULL) {
                            CurrentBlock = Segment->LastValidEntry;
                        } else {
                            CurrentBlock = (PHEAP_ENTRY)(UnCommittedRange->Address + UnCommittedRange->Size);

                            UnCommittedRange = UnCommittedRange->Next;
                        }
                    } else {
                        CurrentBlock += CurrentBlock->Size;
                    }
                }
            }
        } except(EXCEPTION_EXECUTE_HANDLER) {
            Status = GetExceptionCode();
        }
    } finally {
        if (LockAcquired) {
            RtlReleaseLockRoutine(Heap->LockVariable);
        }
    }

    return Status;
}

PHEAP_UNCOMMMTTED_RANGE NTAPI RtlpCreateUnCommittedRange(
    IN PHEAP_SEGMENT Segment
    )
{
    NTSTATUS Status;
    PVOID FirstEntry, LastEntry;
    PHEAP_UNCOMMMTTED_RANGE UnCommittedRange, *pp;
    SIZE_T ReserveSize, CommitSize;
    PHEAP_UCR_SEGMENT UCRSegment;

    pp = &Segment->Heap->UnusedUnCommittedRanges;

    if (*pp == NULL) {
        UCRSegment = Segment->Heap->UCRSegments;

        if ((UCRSegment == NULL) || (UCRSegment->CommittedSize == UCRSegment->ReservedSize)) {
            ReserveSize = 0x10000;
            UCRSegment = NULL;

            Status = NtAllocateVirtualMemory((PVOID *)&UCRSegment, 0, &ReserveSize, MEM_RESERVE, PAGE_READWRITE);

            if (!NT_SUCCESS(Status)) {
                return NULL;
            }

            CommitSize = PAGE_SIZE;

            Status = NtAllocateVirtualMemory((PVOID *)&UCRSegment, 0, &CommitSize, MEM_COMMIT, PAGE_READWRITE);

            if (!NT_SUCCESS(Status)) {
                NtFreeVirtualMemory((PVOID *)&UCRSegment, &ReserveSize, MEM_RELEASE);

                return NULL;
            }

            UCRSegment->Next = Segment->Heap->UCRSegments;
            Segment->Heap->UCRSegments = UCRSegment;

            UCRSegment->ReservedSize = ReserveSize;
            UCRSegment->CommittedSize = CommitSize;

            FirstEntry = (PCHAR)(UCRSegment + 1);
        } else {
            CommitSize = PAGE_SIZE;
            FirstEntry = (PCHAR)UCRSegment + UCRSegment->CommittedSize;

            Status = NtAllocateVirtualMemory((PVOID *)&FirstEntry, 0, &CommitSize, MEM_COMMIT, PAGE_READWRITE);

            if (!NT_SUCCESS(Status)) {
                return NULL;
            }

            UCRSegment->CommittedSize += CommitSize;
        }

        LastEntry = (PCHAR)UCRSegment + UCRSegment->CommittedSize;

        UnCommittedRange = (PHEAP_UNCOMMMTTED_RANGE)FirstEntry;

        pp = &Segment->Heap->UnusedUnCommittedRanges;

        while ((PCHAR)UnCommittedRange < (PCHAR)LastEntry) {
            *pp = UnCommittedRange;
            pp = &UnCommittedRange->Next;
            UnCommittedRange += 1;
        }

        *pp = NULL;

        pp = &Segment->Heap->UnusedUnCommittedRanges;
    }

    UnCommittedRange = *pp;
    *pp = UnCommittedRange->Next;

    return UnCommittedRange;
}

VOID NTAPI RtlpDestroyUnCommittedRange(
    IN PHEAP_SEGMENT Segment,
    IN PHEAP_UNCOMMMTTED_RANGE UnCommittedRange
    )
{
    UnCommittedRange->Next = Segment->Heap->UnusedUnCommittedRanges;
    Segment->Heap->UnusedUnCommittedRanges = UnCommittedRange;

    UnCommittedRange->Address = 0;
    UnCommittedRange->Size = 0;

    return;
}

VOID NTAPI RtlpInsertUnCommittedPages(
    IN PHEAP_SEGMENT Segment,
    IN ULONG_PTR Address,
    IN SIZE_T Size
    )
{
    PHEAP_UNCOMMMTTED_RANGE UnCommittedRange, *pp;

    pp = &Segment->UnCommittedRanges;

    while (UnCommittedRange = *pp) {
        if (UnCommittedRange->Address > Address) {
            if ((Address + Size) == UnCommittedRange->Address) {
                UnCommittedRange->Address = Address;
                UnCommittedRange->Size += Size;

                if (UnCommittedRange->Size > Segment->LargestUnCommittedRange) {
                    Segment->LargestUnCommittedRange = UnCommittedRange->Size;
                }

                return;
            }

            break;
        } else if ((UnCommittedRange->Address + UnCommittedRange->Size) == Address) {
            Address = UnCommittedRange->Address;
            Size += UnCommittedRange->Size;

            *pp = UnCommittedRange->Next;

            RtlpDestroyUnCommittedRange(Segment, UnCommittedRange);

            Segment->NumberOfUnCommittedRanges -= 1;

            if (Size > Segment->LargestUnCommittedRange) {
                Segment->LargestUnCommittedRange = Size;
            }
        } else {
            pp = &UnCommittedRange->Next;
        }
    }

    UnCommittedRange = RtlpCreateUnCommittedRange(Segment);

    if (UnCommittedRange == NULL) {
        return;
    }

    UnCommittedRange->Address = Address;
    UnCommittedRange->Size = Size;

    UnCommittedRange->Next = *pp;
    *pp = UnCommittedRange;

    Segment->NumberOfUnCommittedRanges += 1;

    if (Size >= Segment->LargestUnCommittedRange) {
        Segment->LargestUnCommittedRange = Size;
    }

    return;
}

PHEAP_FREE_ENTRY NTAPI RtlpFindAndCommitPages(
    IN PHEAP Heap,
    IN PHEAP_SEGMENT Segment,
    IN OUT PSIZE_T Size,
    IN PVOID AddressWanted OPTIONAL
    )
{
    NTSTATUS Status;
    PHEAP_ENTRY FirstEntry, LastEntry, PreviousLastEntry, Entry;
    PHEAP_UNCOMMMTTED_RANGE PreviousUnCommittedRange, UnCommittedRange, *pp;
    ULONG_PTR Address;

    PreviousUnCommittedRange = NULL;
    pp = &Segment->UnCommittedRanges;

    while (UnCommittedRange = *pp) {
        if ((UnCommittedRange->Size >= *Size) &&
            ((AddressWanted == NULL) || (UnCommittedRange->Address == (ULONG_PTR)AddressWanted))) {
            Address = UnCommittedRange->Address;

            if (Heap->CommitRoutine != NULL) {
                Status = (Heap->CommitRoutine)(Heap, (PVOID *)&Address, Size);
            } else {
                Status = NtAllocateVirtualMemory((PVOID *)&Address, 0, Size, MEM_COMMIT, PAGE_READWRITE);
            }

            if (!NT_SUCCESS(Status)) {
                return NULL;
            }

            Segment->NumberOfUnCommittedPages -= (ULONG)(*Size >> PAGE_SHIFT);

            if (Segment->LargestUnCommittedRange == UnCommittedRange->Size) {
                Segment->LargestUnCommittedRange = 0;
            }

            FirstEntry = (PHEAP_ENTRY)Address;

            if (!((Entry = Segment->LastEntryInSegment)->Flags & HEAP_ENTRY_LAST_ENTRY) ||
                ((Entry + Entry->Size) != (PHEAP_ENTRY)UnCommittedRange->Address)) {
                if (PreviousUnCommittedRange == NULL) {
                    LastEntry = Segment->FirstEntry;
                } else {
                    LastEntry = (PHEAP_ENTRY)(PreviousUnCommittedRange->Address + PreviousUnCommittedRange->Size);
                }

                while (!(LastEntry->Flags & HEAP_ENTRY_LAST_ENTRY)) {
                    PreviousLastEntry = LastEntry;
                    LastEntry += LastEntry->Size;

                    if (((PCHAR)LastEntry >= (PCHAR)Segment->LastValidEntry) || (LastEntry->Size == 0)) {
                        if (LastEntry == (PHEAP_ENTRY)Address) {
                            LastEntry = PreviousLastEntry;
                        } else {
                            return NULL;
                        }

                        break;
                    }
                }
            } else {
                LastEntry = Entry;
            }

            LastEntry->Flags &= ~HEAP_ENTRY_LAST_ENTRY;

            UnCommittedRange->Address += *Size;
            UnCommittedRange->Size -= *Size;

            if (UnCommittedRange->Size == 0) {
                if (UnCommittedRange->Address == (ULONG_PTR)Segment->LastValidEntry) {
                    FirstEntry->Flags = HEAP_ENTRY_LAST_ENTRY;
                    Segment->LastEntryInSegment = FirstEntry;
                } else {
                    FirstEntry->Flags = 0;
                    Segment->LastEntryInSegment = Segment->FirstEntry;
                }

                *pp = UnCommittedRange->Next;

                RtlpDestroyUnCommittedRange(Segment, UnCommittedRange);

                Segment->NumberOfUnCommittedRanges -= 1;
            } else {
                FirstEntry->Flags = HEAP_ENTRY_LAST_ENTRY;
                Segment->LastEntryInSegment = FirstEntry;
            }

            FirstEntry->SegmentIndex = LastEntry->SegmentIndex;
            FirstEntry->Size = (USHORT)(*Size >> HEAP_GRANULARITY_SHIFT);
            FirstEntry->PreviousSize = LastEntry->Size;

            if (!(FirstEntry->Flags & HEAP_ENTRY_LAST_ENTRY)) {
                (FirstEntry + FirstEntry->Size)->PreviousSize = FirstEntry->Size;
            }

            if (Segment->LargestUnCommittedRange == 0) {
                UnCommittedRange = Segment->UnCommittedRanges;

                while (UnCommittedRange != NULL) {
                    if (UnCommittedRange->Size >= Segment->LargestUnCommittedRange) {
                        Segment->LargestUnCommittedRange = UnCommittedRange->Size;
                    }

                    UnCommittedRange = UnCommittedRange->Next;
                }
            }

            return (PHEAP_FREE_ENTRY)FirstEntry;
        }

        PreviousUnCommittedRange = UnCommittedRange;
        pp = &UnCommittedRange->Next;
    }

    return NULL;
}

BOOLEAN NTAPI RtlpInitializeHeapSegment(
    IN PHEAP Heap,
    IN PHEAP_SEGMENT Segment,
    IN UCHAR SegmentIndex,
    IN ULONG Flags,
    IN PVOID BaseAddress,
    IN PVOID UnCommittedAddress,
    IN PVOID CommitLimitAddress
    )
{
    NTSTATUS Status;
    PHEAP_ENTRY FirstEntry;
    USHORT PreviousSize, Size;
    ULONG NumberOfPages;
    ULONG NumberOfUnCommittedPages;
    SIZE_T CommitSize;

    NumberOfPages = (ULONG)(((PCHAR)CommitLimitAddress - (PCHAR)BaseAddress) / PAGE_SIZE);

    FirstEntry = (PHEAP_ENTRY)ROUND_UP_TO_POWER2(Segment + 1, HEAP_GRANULARITY);

    if ((PVOID)Heap == BaseAddress) {
        PreviousSize = Heap->Entry.Size;
    } else {
        PreviousSize = 0;
    }

    Size = (USHORT)(((PCHAR)FirstEntry - (PCHAR)Segment) >> HEAP_GRANULARITY_SHIFT);

    if ((PCHAR)(FirstEntry + 1) >= (PCHAR)UnCommittedAddress) {
        if ((PCHAR)(FirstEntry + 1) >= (PCHAR)CommitLimitAddress) {
            return FALSE;
        }

        CommitSize = (PCHAR)(FirstEntry + 1) - (PCHAR)UnCommittedAddress;

        Status = NtAllocateVirtualMemory((PVOID *)&UnCommittedAddress, 0, &CommitSize, MEM_COMMIT, PAGE_READWRITE);

        if (!NT_SUCCESS(Status)) {
            return FALSE;
        }

        UnCommittedAddress = (PCHAR)UnCommittedAddress + CommitSize;
    }

    NumberOfUnCommittedPages = (ULONG)(((PCHAR)CommitLimitAddress - (PCHAR)UnCommittedAddress) / PAGE_SIZE);

    Segment->Entry.PreviousSize = PreviousSize;
    Segment->Entry.Size = Size;
    Segment->Entry.Flags = HEAP_ENTRY_BUSY;
    Segment->Entry.SegmentIndex = SegmentIndex;

    Segment->Signature = HEAP_SEGMENT_SIGNATURE;
    Segment->Flags = Flags;
    Segment->Heap = Heap;
    Segment->BaseAddress = BaseAddress;
    Segment->FirstEntry = FirstEntry;
    Segment->LastValidEntry = (PHEAP_ENTRY)((PCHAR)BaseAddress + (NumberOfPages * PAGE_SIZE));
    Segment->NumberOfPages = NumberOfPages;
    Segment->NumberOfUnCommittedPages = NumberOfUnCommittedPages;

    if (NumberOfUnCommittedPages) {
        RtlpInsertUnCommittedPages(Segment, (ULONG_PTR)UnCommittedAddress, NumberOfUnCommittedPages * PAGE_SIZE);
    }

    Heap->Segments[SegmentIndex] = Segment;

    PreviousSize = Segment->Entry.Size;

    FirstEntry->Flags = HEAP_ENTRY_LAST_ENTRY;
    Segment->LastEntryInSegment = FirstEntry;
    FirstEntry->PreviousSize = PreviousSize;
    FirstEntry->SegmentIndex = SegmentIndex;

    RtlpInsertFreeBlock(Heap, (PHEAP_FREE_ENTRY)FirstEntry, ((PCHAR)UnCommittedAddress - (PCHAR)FirstEntry) >> HEAP_GRANULARITY_SHIFT);

    return TRUE;
}

NTSTATUS NTAPI RtlpDestroyHeapSegment(
    IN PHEAP_SEGMENT Segment
    )
{
    PVOID BaseAddress;
    SIZE_T BytesToFree;

    if (!(Segment->Flags & HEAP_SEGMENT_USER_ALLOCATED)) {
        BaseAddress = Segment->BaseAddress;
        BytesToFree = 0;

        return NtFreeVirtualMemory(&BaseAddress, &BytesToFree, MEM_RELEASE);
    } else {
        return STATUS_SUCCESS;
    }
}

PHEAP_FREE_ENTRY NTAPI RtlpExtendHeap(
    IN PHEAP Heap,
    IN SIZE_T AllocationSize
    )
{
    NTSTATUS Status;
    PHEAP_SEGMENT Segment;
    PHEAP_FREE_ENTRY FreeBlock;
    UCHAR SegmentIndex, EmptySegmentIndex;
    ULONG NumberOfPages;
    SIZE_T CommitSize;
    SIZE_T ReserveSize;
    SIZE_T FreeSize;

    NumberOfPages = (ULONG)((AllocationSize + PAGE_SIZE - 1) / PAGE_SIZE);
    FreeSize = NumberOfPages * PAGE_SIZE;

    EmptySegmentIndex = HEAP_MAXIMUM_SEGMENTS;

    for (SegmentIndex = 0; SegmentIndex < HEAP_MAXIMUM_SEGMENTS; SegmentIndex++) {
        Segment = Heap->Segments[SegmentIndex];

        if ((Segment) &&
            (NumberOfPages <= Segment->NumberOfUnCommittedPages) &&
            (FreeSize <= Segment->LargestUnCommittedRange)) {
            FreeBlock = RtlpFindAndCommitPages(Heap, Segment, &FreeSize, NULL);

            if (FreeBlock != NULL) {
                FreeSize = FreeSize >> HEAP_GRANULARITY_SHIFT;

                FreeBlock = RtlpCoalesceFreeBlocks(Heap, FreeBlock, &FreeSize, FALSE);

                RtlpInsertFreeBlock(Heap, FreeBlock, FreeSize);

                return FreeBlock;
            }
        } else if (Segment == NULL) {
            if (EmptySegmentIndex == HEAP_MAXIMUM_SEGMENTS) {
                EmptySegmentIndex = SegmentIndex;
            }
        }
    }

    if ((EmptySegmentIndex != HEAP_MAXIMUM_SEGMENTS) && (Heap->Flags & HEAP_GROWABLE)) {
        Segment = NULL;

        if ((AllocationSize + PAGE_SIZE) > Heap->SegmentReserve) {
            ReserveSize = AllocationSize + PAGE_SIZE;
        } else {
            ReserveSize = Heap->SegmentReserve;
        }

        Status = NtAllocateVirtualMemory((PVOID *)&Segment, 0, &ReserveSize, MEM_RESERVE, PAGE_READWRITE);

        while (!NT_SUCCESS(Status) && (ReserveSize != (AllocationSize + PAGE_SIZE))) {
            ReserveSize = ReserveSize >> 1;

            if (ReserveSize < (AllocationSize + PAGE_SIZE)) {
                ReserveSize = AllocationSize + PAGE_SIZE;
            }

            Status = NtAllocateVirtualMemory((PVOID *)&Segment, 0, &ReserveSize, MEM_RESERVE, PAGE_READWRITE);
        }

        if (NT_SUCCESS(Status)) {
            Heap->SegmentReserve += ReserveSize;

            if ((AllocationSize + PAGE_SIZE) > Heap->SegmentCommit) {
                CommitSize = AllocationSize + PAGE_SIZE;
            } else {
                CommitSize = Heap->SegmentCommit;
            }

            Status = NtAllocateVirtualMemory((PVOID *)&Segment, 0, &CommitSize, MEM_COMMIT, PAGE_READWRITE);

            if (NT_SUCCESS(Status) &&
                !RtlpInitializeHeapSegment(Heap, Segment, EmptySegmentIndex, 0, Segment, (PCHAR)Segment + CommitSize, (PCHAR)Segment + ReserveSize)) {
                Status = STATUS_NO_MEMORY;
            }

            if (NT_SUCCESS(Status)) {
                return (PHEAP_FREE_ENTRY)Segment->FirstEntry;
            }

            NtFreeVirtualMemory((PVOID *)&Segment, &ReserveSize, MEM_RELEASE);
        }
    }

    return NULL;
}

PHEAP_FREE_ENTRY NTAPI RtlpCoalesceFreeBlocks(
    IN PHEAP Heap,
    IN PHEAP_FREE_ENTRY FreeBlock,
    IN OUT PSIZE_T FreeSize,
    IN BOOLEAN RemoveFromFreeList
    )
{
    PHEAP_FREE_ENTRY FreeBlock1, NextFreeBlock;

    FreeBlock1 = (PHEAP_FREE_ENTRY)((PHEAP_ENTRY)FreeBlock - FreeBlock->PreviousSize);

    if ((FreeBlock1 != FreeBlock) &&
        !(FreeBlock1->Flags & HEAP_ENTRY_BUSY) &&
        ((*FreeSize + FreeBlock1->Size) <= HEAP_MAXIMUM_BLOCK_SIZE)) {
        if (RemoveFromFreeList) {
            RtlpRemoveFreeBlock(Heap, FreeBlock);

            Heap->TotalFreeSize -= FreeBlock->Size;

            RemoveFromFreeList = FALSE;
        }

        RtlpRemoveFreeBlock(Heap, FreeBlock1);

        FreeBlock1->Flags = FreeBlock->Flags & HEAP_ENTRY_LAST_ENTRY;

        if (FreeBlock1->Flags & HEAP_ENTRY_LAST_ENTRY) {
            Heap->Segments[FreeBlock1->SegmentIndex]->LastEntryInSegment = (PHEAP_ENTRY)FreeBlock1;
        }

        *FreeSize += FreeBlock1->Size;

        Heap->TotalFreeSize -= FreeBlock1->Size;

        FreeBlock = FreeBlock1;
        FreeBlock1->Size = (USHORT)*FreeSize;

        if (!(FreeBlock1->Flags & HEAP_ENTRY_LAST_ENTRY)) {
            ((PHEAP_ENTRY)FreeBlock1 + *FreeSize)->PreviousSize = (USHORT)*FreeSize;
        }
    }

    if (!(FreeBlock->Flags & HEAP_ENTRY_LAST_ENTRY)) {
        NextFreeBlock = (PHEAP_FREE_ENTRY)((PHEAP_ENTRY)FreeBlock + *FreeSize);

        if (!(NextFreeBlock->Flags & HEAP_ENTRY_BUSY) &&
            ((*FreeSize + NextFreeBlock->Size) <= HEAP_MAXIMUM_BLOCK_SIZE)) {
            if (RemoveFromFreeList) {
                RtlpRemoveFreeBlock(Heap, FreeBlock);

                Heap->TotalFreeSize -= FreeBlock->Size;

                RemoveFromFreeList = FALSE;
            }

            FreeBlock->Flags = NextFreeBlock->Flags & HEAP_ENTRY_LAST_ENTRY;

            if (FreeBlock->Flags & HEAP_ENTRY_LAST_ENTRY) {
                Heap->Segments[FreeBlock->SegmentIndex]->LastEntryInSegment = (PHEAP_ENTRY)FreeBlock;
            }

            RtlpRemoveFreeBlock(Heap, NextFreeBlock);

            *FreeSize += NextFreeBlock->Size;

            Heap->TotalFreeSize -= NextFreeBlock->Size;

            FreeBlock->Size = (USHORT)*FreeSize;

            if (!(FreeBlock->Flags & HEAP_ENTRY_LAST_ENTRY)) {
                ((PHEAP_ENTRY)FreeBlock + *FreeSize)->PreviousSize = (USHORT)*FreeSize;
            }
        }
    }

    return FreeBlock;
}

VOID NTAPI RtlpDeCommitFreeBlock(
    IN PHEAP Heap,
    IN PHEAP_FREE_ENTRY FreeBlock,
    IN SIZE_T FreeSize
    )
{
    ULONG_PTR DeCommitAddress;
    SIZE_T DeCommitSize;
    USHORT LeadingFreeSize, TrailingFreeSize;
    PHEAP_SEGMENT Segment;
    PHEAP_FREE_ENTRY TrailingFreeBlock;
    PHEAP_ENTRY LeadingBusyBlock, TrailingBusyBlock;
    PHEAP_UNCOMMMTTED_RANGE UnCommittedRange;
    NTSTATUS Status;

    if (Heap->CommitRoutine != NULL) {
        RtlpInsertFreeBlock(Heap, FreeBlock, FreeSize);

        return;
    }

    Segment = Heap->Segments[FreeBlock->SegmentIndex];
    LeadingBusyBlock = NULL;

    DeCommitAddress = ROUND_UP_TO_POWER2(FreeBlock, PAGE_SIZE);
    LeadingFreeSize = (USHORT)((PHEAP_ENTRY)DeCommitAddress - (PHEAP_ENTRY)FreeBlock);

    if (LeadingFreeSize == 1) {
        DeCommitAddress += PAGE_SIZE;
        LeadingFreeSize += PAGE_SIZE >> HEAP_GRANULARITY_SHIFT;
    } else if (FreeBlock->PreviousSize != 0) {
        if (DeCommitAddress == (ULONG_PTR)FreeBlock) {
            LeadingBusyBlock = (PHEAP_ENTRY)FreeBlock - FreeBlock->PreviousSize;
        }
    }

    TrailingBusyBlock = NULL;
    TrailingFreeBlock = (PHEAP_FREE_ENTRY)((PHEAP_ENTRY)FreeBlock + FreeSize);

    DeCommitSize = ROUND_DOWN_TO_POWER2((ULONG_PTR)TrailingFreeBlock, PAGE_SIZE);
    TrailingFreeSize = (USHORT)((PHEAP_ENTRY)TrailingFreeBlock - (PHEAP_ENTRY)DeCommitSize);

    if (TrailingFreeSize == 1) {
        DeCommitSize -= PAGE_SIZE;
        TrailingFreeSize += PAGE_SIZE >> HEAP_GRANULARITY_SHIFT;
    } else if ((TrailingFreeSize == 0) && !(FreeBlock->Flags & HEAP_ENTRY_LAST_ENTRY)) {
        TrailingBusyBlock = (PHEAP_ENTRY)TrailingFreeBlock;
    }

    TrailingFreeBlock = (PHEAP_FREE_ENTRY)((PHEAP_ENTRY)TrailingFreeBlock - TrailingFreeSize);

    if (DeCommitSize > DeCommitAddress) {
        DeCommitSize -= DeCommitAddress;
    } else {
        DeCommitSize = 0;
    }

    if (DeCommitSize != 0) {
        UnCommittedRange = RtlpCreateUnCommittedRange(Segment);

        if (UnCommittedRange == NULL) {
            RtlpInsertFreeBlock(Heap, FreeBlock, FreeSize);

            return;
        }

        Status = NtFreeVirtualMemory((PVOID *)&DeCommitAddress, &DeCommitSize, MEM_DECOMMIT);

        RtlpDestroyUnCommittedRange(Segment, UnCommittedRange);

        if (NT_SUCCESS(Status)) {
            RtlpInsertUnCommittedPages(Segment, DeCommitAddress, DeCommitSize);

            Segment->NumberOfUnCommittedPages += (ULONG)(DeCommitSize / PAGE_SIZE);

            if (LeadingFreeSize != 0) {
                FreeBlock->Size = LeadingFreeSize;
                FreeBlock->Flags = HEAP_ENTRY_LAST_ENTRY;

                Heap->TotalFreeSize += LeadingFreeSize;

                Segment->LastEntryInSegment = (PHEAP_ENTRY)FreeBlock;

                RtlpInsertFreeBlockDirect(Heap, FreeBlock, LeadingFreeSize);
            } else if (LeadingBusyBlock != NULL) {
                LeadingBusyBlock->Flags |= HEAP_ENTRY_LAST_ENTRY;

                Segment->LastEntryInSegment = LeadingBusyBlock;
            } else if ((Segment->LastEntryInSegment >= (PHEAP_ENTRY)DeCommitAddress) &&
                       ((ULONG_PTR)Segment->LastEntryInSegment < (DeCommitAddress + DeCommitSize))) {
                Segment->LastEntryInSegment = Segment->FirstEntry;
            }

            if (TrailingFreeSize != 0) {
                TrailingFreeBlock->PreviousSize = 0;
                TrailingFreeBlock->SegmentIndex = Segment->Entry.SegmentIndex;
                TrailingFreeBlock->Flags = 0;
                TrailingFreeBlock->Size = TrailingFreeSize;

                ((PHEAP_ENTRY)TrailingFreeBlock + TrailingFreeSize)->PreviousSize = (USHORT)TrailingFreeSize;

                RtlpInsertFreeBlockDirect(Heap, TrailingFreeBlock, TrailingFreeSize);

                Heap->TotalFreeSize += TrailingFreeSize;
            } else if (TrailingBusyBlock != NULL) {
                TrailingBusyBlock->PreviousSize = 0;
            }
        } else {
            RtlpInsertFreeBlock(Heap, FreeBlock, FreeSize);
        }
    } else {
        RtlpInsertFreeBlock(Heap, FreeBlock, FreeSize);
    }

    return;
}

VOID NTAPI RtlpInsertFreeBlock(
    IN PHEAP Heap,
    IN PHEAP_FREE_ENTRY FreeBlock,
    IN SIZE_T FreeSize
    )
{
    USHORT PreviousSize, Size;
    UCHAR Flags;
    UCHAR SegmentIndex;
    PHEAP_SEGMENT Segment;

    PreviousSize = FreeBlock->PreviousSize;
    SegmentIndex = FreeBlock->SegmentIndex;
    Segment = Heap->Segments[SegmentIndex];

    Flags = FreeBlock->Flags;

    Heap->TotalFreeSize += FreeSize;

    while (FreeSize != 0) {
        if (FreeSize > (ULONG)HEAP_MAXIMUM_BLOCK_SIZE) {
            Size = HEAP_MAXIMUM_BLOCK_SIZE;

            if (FreeSize == ((ULONG)HEAP_MAXIMUM_BLOCK_SIZE + 1)) {
                Size -= 16;
            }

            FreeBlock->Flags = 0;
        } else {
            Size = (USHORT)FreeSize;

            FreeBlock->Flags = Flags;
        }

        FreeBlock->PreviousSize = PreviousSize;
        FreeBlock->SegmentIndex = SegmentIndex;
        FreeBlock->Size = Size;

        RtlpInsertFreeBlockDirect(Heap, FreeBlock, Size);

        PreviousSize = Size;
        FreeSize -= Size;
        FreeBlock = (PHEAP_FREE_ENTRY)((PHEAP_ENTRY)FreeBlock + Size);

        if ((PHEAP_ENTRY)FreeBlock >= Segment->LastValidEntry) {
            return;
        }
    }

    if (!(Flags & HEAP_ENTRY_LAST_ENTRY)) {
        FreeBlock->PreviousSize = PreviousSize;
    }

    return;
}

PHEAP_ENTRY_EXTRA NTAPI RtlpGetExtraStuffPointer(
    PHEAP_ENTRY BusyBlock
    )
{
    ULONG AllocationIndex;

    if (BusyBlock->Flags & HEAP_ENTRY_VIRTUAL_ALLOC) {
        PHEAP_VIRTUAL_ALLOC_ENTRY VirtualAllocBlock;

        VirtualAllocBlock = CONTAINING_RECORD(BusyBlock, HEAP_VIRTUAL_ALLOC_ENTRY, BusyBlock);

        return &VirtualAllocBlock->ExtraStuff;
    } else {
        AllocationIndex = BusyBlock->Size;

        return (PHEAP_ENTRY_EXTRA)(BusyBlock + AllocationIndex - 1);
    }
}

SIZE_T NTAPI RtlpGetSizeOfBigBlock(
    IN PHEAP_ENTRY BusyBlock
    )
{
    PHEAP_VIRTUAL_ALLOC_ENTRY VirtualAllocBlock;

    VirtualAllocBlock = CONTAINING_RECORD(BusyBlock, HEAP_VIRTUAL_ALLOC_ENTRY, BusyBlock);

    return VirtualAllocBlock->CommitSize - BusyBlock->Size;
}

BOOLEAN NTAPI RtlpCheckBusyBlockTail(
    IN PHEAP_ENTRY BusyBlock
    )
{
    PCHAR Tail;
    SIZE_T Size, cbEqual;

    if (BusyBlock->Flags & HEAP_ENTRY_VIRTUAL_ALLOC) {
        Size = RtlpGetSizeOfBigBlock(BusyBlock);
    } else {
        Size = (BusyBlock->Size << HEAP_GRANULARITY_SHIFT) - BusyBlock->UnusedBytes;
    }

    Tail = (PCHAR)(BusyBlock + 1) + Size;

    cbEqual = RtlCompareMemory(Tail, CheckHeapFillPattern, CHECK_HEAP_TAIL_SIZE);

    if (cbEqual != CHECK_HEAP_TAIL_SIZE) {
        return FALSE;
    } else {
        return TRUE;
    }
}
