#ifndef _DM_H_
#define _DM_H_

typedef struct _DMINIT {
    PVOID *DebugRoutine;
    PLIST_ENTRY LoadedModuleList;
    ULONG Flags;
    PVOID *ClockIntRoutine;
    PVOID *ProfIntRoutine;
    ULONG (NTAPI *TellRoutine)(ULONG ulCode, PVOID pvData);
    PVOID *CtxSwapNotifyRoutine;
    PVOID XProfpDataPtr;
    PVOID D3DDriverData;
    PVOID XapiData;
    PUCHAR DisallowXbdm;
    VOID (NTAPI *HalStartProfileRoutine)(ULONG);
    VOID (NTAPI *HalStopProfileRoutine)(ULONG);
    VOID (NTAPI *HalProfileIntervalRoutine)(ULONG);
    PVOID *DpcDispatchNotifyRoutine;
    PVOID NextDVDSample;
} DMINIT, *PDMINIT;

#define DMTELL_INSERTALLOCATIONENTRY 10
#define DMTELL_REMOVEALLOCATIONENTRY 11

static __inline ULONG NTAPI DmTell(ULONG ulCode, PVOID pvData)
{
    PKPRCB Prcb = KeGetCurrentPrcb(); PDMINIT pdmi = (PDMINIT)Prcb->DebugMonitorData;
    if (pdmi) {
        return pdmi->TellRoutine(ulCode, pvData);
    }
    return 0;
}

static __forceinline HRESULT NTAPI DmTell_InsertAllocationEntry(PVOID AllocPtr, ULONG AllocSize, USHORT AllocType)
{
    ULONG Parms[3];
    HRESULT hr;
    Parms[0] = (ULONG)AllocPtr;
    Parms[1] = AllocSize;
    Parms[2] = AllocType;
    hr = DmTell(DMTELL_INSERTALLOCATIONENTRY, Parms);
    return hr;
}

static __forceinline HRESULT NTAPI DmTell_RemoveAllocationEntry(PVOID AllocPtr, ULONG AllocSize, USHORT AllocType)
{
    ULONG Parms[3];
    HRESULT hr;
    Parms[0] = (ULONG)AllocPtr;
    Parms[1] = AllocSize;
    Parms[2] = AllocType;
    hr = DmTell(DMTELL_REMOVEALLOCATIONENTRY, Parms);
    return hr;
}

#endif
