#ifndef _i386_
#define _i386_

typedef struct _KPRCB {
    struct _KTHREAD *CurrentThread;
    struct _KTHREAD *NextThread;
    struct _KTHREAD *IdleThread;
    struct _KTHREAD *NpxThread;
    ULONG InterruptCount;
    ULONG DpcTime;
    ULONG InterruptTime;
    ULONG DebugDpcTime;
    ULONG KeContextSwitches;
    ULONG DpcInterruptRequested;
    LIST_ENTRY DpcListHead;
    ULONG DpcRoutineActive;
    PVOID DpcStack;
    ULONG QuantumEnd;
    UCHAR NpxSaveArea[528];
    PVOID DmEnetFunc;
    PVOID DebugMonitorData;
    PVOID DebugHaltThread;
    PVOID DebugDoubleFault;
} KPRCB, *PKPRCB;

#define PcPrcb 0x20

static __inline PKPRCB NTAPI KeGetCurrentPrcb(VOID)
{
    __asm mov eax, fs:[PcPrcb]
}

#endif
