/*==========================================================================;
 *
 *  uix.h -- This module defines the Xbox Drop-In UI APIs
 *
 *  Copyright (C) Microsoft Corporation.  All Rights Reserved.
 *
 ***************************************************************************/

#ifndef __UIX__
#define __UIX__

#ifndef __XONLINE__
#include "xonline.h"
#endif

#pragma warning( push )
#pragma warning( disable : 4100 ) // unreferenced formal parameter

#ifndef UIXINLINE
#define UIXINLINE __forceinline
#endif

#ifdef __cplusplus
extern "C" {
#endif

//-----------------------------------------------------------------------------
//  Typedefs, Structures & Consts
//-----------------------------------------------------------------------------

#define UIX_MAX_LOGON_SERVICES          10
#define UIX_INVALID_OBJECT_ID           ((DWORD)-1)
#define UIX_INVALID_VALUE               ((DWORD)-1)
#define UIX_MAX_ICONS_IN_TEXT           20

//
// Features identifiers
//

typedef PVOID                         UIX_FEATURE;

extern const UIX_FEATURE              _uix_logon_feature;
extern const UIX_FEATURE              _uix_friends_feature;
#ifdef _DEBUG
extern const UIX_FEATURE              _uix_uitest_feature;
#endif

#define UIX_LOGON_FEATURE             _uix_logon_feature
#define UIX_FRIENDS_FEATURE           _uix_friends_feature
#ifdef _DEBUG
#define UIX_UITEST_FEATURE            _uix_uitest_feature
#endif

//
// Engine DoWork flags
//

#define UIX_DOWORK_NEED_TO_RENDER              0x00000001
#define UIX_DOWORK_NOTIFICATIONS               0x00000002
#define UIX_DOWORK_NEED_TO_REBOOT              0x00000004
#define UIX_DOWORK_FEATURE_EXIT                0x00000008
#define UIX_DOWORK_PROCESSING_INPUT            0x00000010

//
// Ports for notifications
//
#define UIX_PORT_0                             0x10000000
#define UIX_PORT_1                             0x20000000
#define UIX_PORT_2                             0x40000000
#define UIX_PORT_3                             0x80000000

//
// Notifications flags per user
//

#define UIX_DOWORK_NOTIFY_FRIEND_REQUEST       0x00010000
#define UIX_DOWORK_NOTIFY_GAME_INVITE          0x00020000

//
// Controller input translated to something the screens and plugin can work with
//

#define UIX_INPUT_DIRECTIONAL_FLAG             0x00001000
#define UIX_INPUT_NOTIFICATION_FLAG            0x00000100

//
// Information required to layout icons such as the A button bitmap
// in conjunction with text strings
//

#define UIX_ICON_INSIDE_TEXT                   0x00000001
#define UIX_ICON_RIGHT_JUSTIFIED               0x00000002

typedef enum _UIX_NOTIFICATION_PURPOSE
{
    UIX_NOTIFICATION_MENU               = 0,
    UIX_NOTIFICATION_IN_GAME_FLASH      = 1,

    UIX_NOTIFICATION_FORCE_DWORD        = 0x7fffffff
} UIX_NOTIFICATION_PURPOSE;


//
// Property types
//

typedef enum _UIX_PROPERTY_TYPE
{
    UIX_PROPERTY_DISPLAY_NOTIFICATIONS      = 0,
    UIX_PROPERTY_HOST_NO_MIGRATION          = 1,
    UIX_PROPERTY_DISPLAY_CONNECTION_ERRORS  = 2,
    UIX_PROPERTY_ALLOW_GAME_INVITES         = 3,

    UIX_PROPERTY_FORCE_DWORD                = 0x7fffffff
} UIX_PROPERTY_TYPE;

//
// Types used by the GetExitInfo function
//

typedef enum _UIX_EXIT_CODE_TYPE
{
    UIX_EXIT_NONE                               = 0,
    UIX_EXIT_LOGON_SUCCESSFUL                   = 1,
    UIX_EXIT_LOGON_FAILED                       = 2,
    UIX_EXIT_LOGON_USER_EXIT                    = 3,
    UIX_EXIT_FRIENDS_NORMAL_EXIT                = 4,
    UIX_EXIT_FRIENDS_JOIN_GAME                  = 5,
    UIX_EXIT_FRIENDS_JOIN_GAME_CROSS_TITLE      = 6,
    UIX_EXIT_FRIENDS_SIGNED_OUT                 = 7,

#ifdef _DEBUG
    UIX_EXIT_UITEST_NORMAL_EXIT                 = 8,
#endif

    UIX_EXIT_FORCE_DWORD                    = 0x7fffffff
} UIX_EXIT_CODE_TYPE;

//
// Object types for objects that are handled by a UIPlugin
//

typedef enum _UIX_OBJECT_TYPE
{
    UIX_OBJECT_NONE                     = 0,
    UIX_OBJECT_TEXTBOX                  = 1,
    UIX_OBJECT_LISTBOX                  = 2,
    UIX_OBJECT_BACKGROUND               = 3,

    UIX_OBJECT_FORCE_DWORD              = 0x7fffffff
} UIX_OBJECT_TYPE;


//
// Object states for objects that are handled by a UIPlugin
//

typedef enum _UIX_OBJSTATE_TYPE
{
    UIX_OBJSTATE_LIST_SELECTION_INDEX   = 0,
    UIX_OBJSTATE_LIST_ITEM_GREYED       = 1,
    UIX_OBJSTATE_WORD_WRAP              = 2,

    UIX_OBJSTATE_FORCE_DWORD            = 0x7fffffff
} UIX_OBJSTATE_TYPE;


//
// Input types used by a UIPlugin
//

typedef enum _UIX_INPUT_TYPE
{
    UIX_INPUT_NONE                      = 0x0,
    UIX_INPUT_A                         = 0x1,
    UIX_INPUT_B                         = 0x2,
    UIX_INPUT_X                         = 0x3,
    UIX_INPUT_Y                         = 0x4,
    UIX_INPUT_UP                        = 0x5 | UIX_INPUT_DIRECTIONAL_FLAG,
    UIX_INPUT_DOWN                      = 0x6 | UIX_INPUT_DIRECTIONAL_FLAG,
    UIX_INPUT_LEFT                      = 0x7 | UIX_INPUT_DIRECTIONAL_FLAG,
    UIX_INPUT_RIGHT                     = 0x8 | UIX_INPUT_DIRECTIONAL_FLAG,
    UIX_INPUT_DPAD_UP                   = 0x9 | UIX_INPUT_DIRECTIONAL_FLAG,
    UIX_INPUT_DPAD_DOWN                 = 0xA | UIX_INPUT_DIRECTIONAL_FLAG,
    UIX_INPUT_DPAD_LEFT                 = 0xB | UIX_INPUT_DIRECTIONAL_FLAG,
    UIX_INPUT_DPAD_RIGHT                = 0xC | UIX_INPUT_DIRECTIONAL_FLAG,
    UIX_INPUT_LEFT_TRIGGER              = 0xD,
    UIX_INPUT_RIGHT_TRIGGER             = 0xE,
    UIX_INPUT_CONTROLLER_IN             = 0x1 | UIX_INPUT_NOTIFICATION_FLAG,
    UIX_INPUT_CONTROLLER_OUT            = 0x2 | UIX_INPUT_NOTIFICATION_FLAG,

    UIX_INPUT_FORCE_DWORD               = 0x7fffffff
} UIX_INPUT_TYPE;


//
// Logon type
//

typedef enum _UIX_LOGON_TYPE
{
    UIX_LOGON_TYPE_NORMAL                = 0,
    UIX_LOGON_TYPE_SILENT                = 1,
    UIX_LOGON_TYPE_RETRIEVED_STATE       = 2,
    UIX_LOGON_TYPE_RETRIEVED_GAME_INVITE = 3,

    UIX_LOGON_FORCE_DWORD                = 0x7fffffff
} UIX_LOGON_TYPE;


#pragma pack(push, 4)

//
// Features input parameters structures.
// These structure must be zero-initialized and
// the size of the structure must be specified
// in the StructSize field.
//

//
// Logon feature parameters structure.
//
//      LogonType - Type of logon such as normal, silent etc
//
//      LogonUserCount - Number of users allowed to logon.  The
//          value can be 1, 2, 3 or 4.  The feature will select the
//          the correct screen for logon based on this value
//
//      LogonServiceIDs - Service IDs of the required services.  All
//          service values should be consecutively specified starting
//          from index zero.
//
//      pLogonState - Saved logon state to use for logon
//
//      pGameInvite - Game invite to use for logon
//

typedef struct _UIX_LOGON_PARAMS
{
    SIZE_T                       StructSize;
    UIX_LOGON_TYPE               LogonType;
    DWORD                        LogonUserCount;
    DWORD                        LogonServiceIDs[UIX_MAX_LOGON_SERVICES];
    XONLINE_LOGON_STATE*         pLogonState;
    XONLINE_ACCEPTED_GAMEINVITE* pGameInvite;

} UIX_LOGON_PARAMS, *PUIX_LOGON_PARAMS;

//
// Friends feature parameters structure.
//
//      UserPort - The port of the controller the user is logged on with
//
//      SelectedFriendXUID - Highlighted friend in initial friends list
//
//      SignOutEnabled - TRUE if the game wants th Sign Out button available
//                       from the friends screen
//

typedef struct _UIX_FRIENDS_PARAMS {

    SIZE_T      StructSize;
    DWORD       UserPort;
    XUID        SelectedFriendXUID;
    BOOL        SignOutEnabled;

} UIX_FRIENDS_PARAMS, *PUIX_FRIENDS_PARAMS;

#ifdef _DEBUG
typedef struct _UIX_UITEST_PARAMS {

    SIZE_T      StructSize;

} UIX_UITEST_PARAMS, *PUIX_UITEST_PARAMS;
#endif

//
// Exit info
//

typedef struct _UIX_EXIT_INFO
{
    UIX_FEATURE          FeatureID;
    UIX_EXIT_CODE_TYPE   ExitCode;
    HRESULT              hr;
    PVOID                pExitData;

} UIX_EXIT_INFO, *PUIX_EXIT_INFO;

#ifdef _DEBUG
//
// Counts of active allocations
//

extern DWORD UIX__EngineAllocations;
extern DWORD UIX__TextureAllocations;
#endif

#pragma pack(pop)


#pragma pack(push, 1)

typedef struct _UIX_SKIN_LAYOUT_INFO
{
    WORD        X;
    WORD        Y;
    WORD        Width;
    WORD        Height;
    DWORD       ImageOffset;
    D3DCOLOR    BackColor;
    D3DCOLOR    TextColor;
    D3DCOLOR    DisabledTextColor;
    D3DCOLOR    SelectionBackColor;
    WORD        FontHeight;
    WORD        Flags;
    DWORD       CustomParam;
} UIX_SKIN_LAYOUT_INFO, *PUIX_SKIN_LAYOUT_INFO;

typedef struct _UIX_SKIN_ICON_INFO
{
    DWORD       IconResID;
    DWORD       InsertPosInText;
    DWORD       Flags;
} UIX_SKIN_ICON_INFO, *PUIX_SKIN_ICON_INFO;

#pragma pack(pop)

//-----------------------------------------------------------------------------
//  Interfaces
//-----------------------------------------------------------------------------

typedef struct PluginSupport         PluginSupport;
typedef struct LiveFriendsList       LiveFriendsList;
typedef struct LiveEngine            LiveEngine;

//
// Compatibility typedefs.
//

#define IPluginSupport               PluginSupport
#define ILiveFriendsList             LiveFriendsList
#define ILiveEngine                  LiveEngine

//
// Pointer typedefs.
//

typedef struct PluginSupport         *LPPluginSupport, *PPluginSupport;
typedef struct LiveFriendsList       *LPLiveFriendsList, *PLiveFriendsList;
typedef struct LiveEngine            *LPLiveEngine, *PLiveEngine;

//-----------------------------------------------------------------------------
//  ITitleUIPlugin interface
//    - Interface implemented by a UIPlugin.  This interface is used
//      by the engine to do its UI related work
//

#undef  INTERFACE
#define INTERFACE   ITitleUIPlugin

DECLARE_INTERFACE(ITitleUIPlugin)
{
    STDMETHOD_(ULONG, Release)(THIS) PURE;

    STDMETHOD(DoWork)(THIS)          PURE;

    STDMETHOD(SetPluginSupport)(     THIS_ IN  IPluginSupport            *pPluginSupport) PURE;

    STDMETHOD(CreateObject)(         THIS_ IN  UIX_OBJECT_TYPE            ObjectType,
                                           IN  DWORD                      ScreenInstance,
                                           IN  DWORD                      ScreenResID,
                                           IN  DWORD                      ObjectResID,
                                           OUT DWORD                     *pObjectID)      PURE;

    STDMETHOD(DestroyObject)(        THIS_ IN  DWORD                      ObjectID)       PURE;

    STDMETHOD(DestroyScreenObjects)( THIS_ IN  DWORD                      ScreenInstance)    PURE;

    STDMETHOD(SetRenderTarget)(      THIS_ IN  IDirect3DSurface8         *pSurface)       PURE;

    STDMETHOD(RenderObject)(         THIS_ IN  DWORD                      ObjectID)       PURE;

    STDMETHOD(SetText)(              THIS_ IN  DWORD                      ObjectID,
                                           IN  DWORD                      ItemIndex,
                                           IN  LPCWSTR                    pText,
                                           IN  DWORD                      IconCount,
                                           IN  const UIX_SKIN_ICON_INFO  *pIconInfo)      PURE;

    STDMETHOD(InsertItem)(           THIS_ IN  DWORD                      ObjectID,
                                           IN  DWORD                      ItemIndex,
                                           OUT DWORD                     *pReturnIndex)   PURE;

    STDMETHOD(SetObjectState)(       THIS_ IN  DWORD                      ObjectID,
                                           IN  DWORD                      ItemIndex,
                                           IN  UIX_OBJSTATE_TYPE          State,
                                           IN  DWORD                      Value)          PURE;

    STDMETHOD(GetObjectState)(       THIS_ IN  DWORD                      ObjectID,
                                           IN  DWORD                      ItemIndex,
                                           IN  UIX_OBJSTATE_TYPE          State,
                                           OUT DWORD                     *pValue)         PURE;

    STDMETHOD(PassInputToObject)(    THIS_ IN  DWORD                      ObjectID,
                                           IN  UIX_INPUT_TYPE             InputKey)       PURE;

    STDMETHOD(Clear)(                THIS_ IN  DWORD                      ObjectID,
                                           IN  BOOL                       ResetSelectionIndex) PURE;
};


typedef struct ITitleUIPlugin                          *LPTitleUIPlugin, *PTitleUIPlugin;


//-----------------------------------------------------------------------------
//
//  ITitleFontRenderer interface
//    - Interface implemented by a font renderer enabling callers
//      to draw text
//
//      Release - Called when the client is finished using this interface
//
//      SetHeight - Set the height of the font
//
//      SetColor - Set the color of the font
//
//      DrawText - Draw pText (null-terminated) on the current render target
//                 at X, Y location in screen coordinates.  MaxWidth is maximum
//                 width allowed so the font can be faded or truncated if too
//                 long.  The value can also be (DWORD)-1 which means that
//                 there is no maximum width
//
//      GetTextSize - Return the width and height of pText (null terminated) in
//                    screen coordinates
//

#undef  INTERFACE
#define INTERFACE   ITitleFontRenderer

DECLARE_INTERFACE(ITitleFontRenderer)
{

    STDMETHOD_(ULONG, Release)(THIS) PURE;

    STDMETHOD(SetHeight)(      THIS_ IN  DWORD                Height)    PURE;

    STDMETHOD(SetColor)(       THIS_ IN  D3DCOLOR             Color)     PURE;

    STDMETHOD(DrawText)(       THIS_ IN  LPCWSTR              pText,
                                     IN  DWORD                X,
                                     IN  DWORD                Y,
                                     IN  DWORD                MaxWidth)     PURE;

    STDMETHOD(GetTextSize)(    THIS_ IN  LPCWSTR              pText,
                                     OUT DWORD               *pWidth,
                                     OUT DWORD               *pHeight)   PURE;
};

typedef struct ITitleFontRenderer         *LPTitleFontRenderer, *PTitleFontRenderer;


//-----------------------------------------------------------------------------
//
//  ITitleAudioPlugin interface
//    - Interface implemented by a sound system to allow a caller
//      to play simple sounds
//

#undef  INTERFACE
#define INTERFACE   ITitleAudioPlugin

DECLARE_INTERFACE(ITitleAudioPlugin)
{
    STDMETHOD_(ULONG, Release)( THIS)             PURE;

    STDMETHOD(PlaySound)(THIS_  LPCSTR SoundName) PURE;

    STDMETHOD(DoWork)(          THIS)             PURE;
};


typedef struct ITitleAudioPlugin      *LPTitleAudioPlugin, *PTitleAudioPlugin;


//-----------------------------------------------------------------------------
// NOTE: The C version of the methods for all of the interfaces
//       are named "<interfacename>_<method name>" and have an
//       explicit pointer to the interface as the first parameter.
//       The actual definition of these methods is at the end
//       of this file.
//-----------------------------------------------------------------------------

//-----------------------------------------------------------------------------
//
//  PluginSupport, IPluginSupport interface
//    - Interface used by the plugin to access services implemented by the engine
//      such as skin support and word wrapping
//

#ifdef __cplusplus

struct PluginSupport
{
    ULONG   WINAPI AddRef();

    ULONG   WINAPI Release();

    HRESULT WINAPI GetString(      IN  DWORD                   StringResID,
                                   OUT LPCWSTR                *ppString);

    HRESULT WINAPI GetLayout(      IN  DWORD                   ScreenResID,
                                   IN  DWORD                   ObjectResID,
                                   OUT UIX_SKIN_LAYOUT_INFO  **ppLayout);

    HRESULT WINAPI GetImage(       IN  DWORD                   ImageResID,
                                   OUT IDirect3DTexture8     **ppTexture);

    HRESULT WINAPI GetScreenImage( IN  DWORD                   ScreenResID,
                                   IN  DWORD                   ImageResID,
                                   OUT IDirect3DTexture8     **ppTexture);

    HRESULT WINAPI GetWordLength(  IN  LPCWSTR                 pString,
                                   OUT DWORD                  *pWordLength);
};

#endif __cplusplus


ULONG   WINAPI PluginSupport_AddRef(         IN  PluginSupport          *pThis);

ULONG   WINAPI PluginSupport_Release(        IN  PluginSupport          *pThis);

HRESULT WINAPI PluginSupport_GetString(      IN  PluginSupport          *pThis,
                                             IN  DWORD                   StringResID,
                                             OUT LPCWSTR                *ppString);

HRESULT WINAPI PluginSupport_GetLayout(      IN  PluginSupport          *pThis,
                                             IN  DWORD                   ScreenResID,
                                             IN  DWORD                   ObjectResID,
                                             OUT UIX_SKIN_LAYOUT_INFO  **ppLayout);

HRESULT WINAPI PluginSupport_GetImage(       IN  PluginSupport          *pThis,
                                             IN  DWORD                   ImageResID,
                                             OUT IDirect3DTexture8     **ppTexture);

HRESULT WINAPI PluginSupport_GetScreenImage( IN  PluginSupport          *pThis,
                                             IN  DWORD                   ScreenResID,
                                             IN  DWORD                   ImageResID,
                                             OUT IDirect3DTexture8     **ppTexture);

HRESULT WINAPI PluginSupport_GetWordLength(  IN  PluginSupport          *pThis,
                                             IN  LPCWSTR                 pString,
                                             OUT DWORD                  *pWordLength);

//-----------------------------------------------------------------------------
//
//  LiveFriendsList, ILiveFriendsList interface
//    - Interface exposed by the engine for the friends list
//

#ifdef __cplusplus

struct LiveFriendsList
{
    ULONG   WINAPI AddRef();

    ULONG   WINAPI Release();

    HRESULT WINAPI Refresh();

    BOOL    WINAPI IsReady();

    BOOL    WINAPI HasChanged();

    DWORD   WINAPI Count();

    HRESULT WINAPI GetFriendByIndex( IN  DWORD                  FriendIndex,
                                     OUT XONLINE_FRIEND        *pFriend);

    HRESULT WINAPI GetFriendByXUID(  IN  XUID                   FriendXUID,
                                     OUT XONLINE_FRIEND        *pFriend);

    HRESULT WINAPI Remove(           IN  const XONLINE_FRIEND  *pFriend);

    HRESULT WINAPI Request(          IN  XUID                   UserXUID);
};

#endif __cplusplus


ULONG   WINAPI LiveFriendsList_AddRef(           IN  LiveFriendsList      *pThis);

ULONG   WINAPI LiveFriendsList_Release(          IN  LiveFriendsList      *pThis);

HRESULT WINAPI LiveFriendsList_Refresh(          IN  LiveFriendsList      *pThis);

BOOL    WINAPI LiveFriendsList_IsReady(          IN  LiveFriendsList      *pThis);

BOOL    WINAPI LiveFriendsList_HasChanged(       IN  LiveFriendsList      *pThis);

DWORD   WINAPI LiveFriendsList_Count(            IN  LiveFriendsList      *pThis);

HRESULT WINAPI LiveFriendsList_GetFriendByIndex( IN  LiveFriendsList      *pThis,
                                                 IN  DWORD                 FriendIndex,
                                                 OUT XONLINE_FRIEND       *pFriend);

HRESULT WINAPI LiveFriendsList_GetFriendByXUID(  IN  LiveFriendsList      *pThis,
                                                 IN  XUID                  FriendXUID,
                                                 OUT XONLINE_FRIEND       *pFriend);

HRESULT WINAPI LiveFriendsList_Remove(           IN  LiveFriendsList      *pThis,
                                                 IN  const XONLINE_FRIEND *pFriend);

HRESULT WINAPI LiveFriendsList_Request(          IN  LiveFriendsList      *pThis,
                                                 IN  XUID                  UserXUID);

//-----------------------------------------------------------------------------
//
//  LiveEngine, ILiveEngine interface
//    - Main interface exposed by the live engine
//

#ifdef __cplusplus

struct LiveEngine
{
    ULONG   WINAPI AddRef();

    ULONG   WINAPI Release();

    HRESULT WINAPI DoWork(                OUT DWORD                    *pDoWorkFlags);

    HRESULT WINAPI SetUIPlugin(           IN  ITitleUIPlugin           *pUIPlugin);

    HRESULT WINAPI SetAudioPlugin(        IN  ITitleAudioPlugin        *pAudioPlugin);

    HRESULT WINAPI EnableFeature(         IN  UIX_FEATURE               FeatureID);

    HRESULT WINAPI StartFeature(          IN  UIX_FEATURE               FeatureID,
                                          IN  const VOID               *pFeatureParams);

    HRESULT WINAPI Render(                IN  IDirect3DSurface8        *pSurface);

    HRESULT WINAPI SetInput(              IN  DWORD                     Port,
                                          IN  const XINPUT_STATE       *pInputState);

    HRESULT WINAPI GetFriendsForUser(     IN  DWORD                     Port,
                                          OUT ILiveFriendsList        **ppFriendsList);

    HRESULT WINAPI NotificationSetState(  IN  DWORD                     Port,
                                          IN  DWORD                     StateFlags,
                                          IN  XNKID                     SessionID,
                                          IN  DWORD                     StateDataSize,
                                          IN  const PVOID               pStateData);

    HRESULT WINAPI GetNotifications(      IN  DWORD                     Port,
                                          IN  UIX_NOTIFICATION_PURPOSE  Purpose,
                                          OUT DWORD                    *pNotifications);

    HRESULT WINAPI SetProperty(           IN  UIX_PROPERTY_TYPE         Property,
                                          IN  DWORD                     Value);

    HRESULT WINAPI GetProperty(           IN  UIX_PROPERTY_TYPE         Property,
                                          OUT DWORD                    *pValue);

    HRESULT WINAPI GetExitInfo(           OUT UIX_EXIT_INFO            *pExitInfo);

    HRESULT WINAPI Reboot(                IN  DWORD                     Context);

    HRESULT WINAPI LogOff(                VOID);

};

#endif __cplusplus

ULONG   WINAPI LiveEngine_AddRef(                IN  LiveEngine               *pThis);

ULONG   WINAPI LiveEngine_Release(               IN  LiveEngine               *pThis);

HRESULT WINAPI LiveEngine_DoWork(                IN  LiveEngine               *pThis,
                                                 OUT DWORD                    *pDoWorkFlags);

HRESULT WINAPI LiveEngine_SetUIPlugin(           IN  LiveEngine               *pThis,
                                                 IN  ITitleUIPlugin           *pUIPlugin);

HRESULT WINAPI LiveEngine_SetAudioPlugin(        IN  LiveEngine               *pThis,
                                                 IN  ITitleAudioPlugin        *pAudioPlugin);

HRESULT WINAPI LiveEngine_EnableFeature(         IN  LiveEngine               *pThis,
                                                 IN  UIX_FEATURE               FeatureID);

HRESULT WINAPI LiveEngine_StartFeature(          IN  LiveEngine               *pThis,
                                                 IN  UIX_FEATURE               FeatureID,
                                                 IN  const VOID               *pFeatureParams);

HRESULT WINAPI LiveEngine_Render(                IN  LiveEngine               *pThis,
                                                 IN  IDirect3DSurface8        *pSurface);

HRESULT WINAPI LiveEngine_SetInput(              IN  LiveEngine               *pThis,
                                                 IN  DWORD                     Port,
                                                 IN  const XINPUT_STATE       *pInputState);

HRESULT WINAPI LiveEngine_GetFriendsForUser(     IN  LiveEngine               *pThis,
                                                 IN  DWORD                     Port,
                                                 OUT ILiveFriendsList        **ppFriendsList);

HRESULT WINAPI LiveEngine_NotificationSetState(  IN  LiveEngine               *pThis,
                                                 IN  DWORD                     Port,
                                                 IN  DWORD                     StateFlags,
                                                 IN  XNKID                     SessionID,
                                                 IN  DWORD                     StateDataSize,
                                                 IN  const PVOID               pStateData);

HRESULT WINAPI LiveEngine_GetNotifications(      IN  LiveEngine               *pThis,
                                                 IN  DWORD                     Port,
                                                 IN  UIX_NOTIFICATION_PURPOSE  Purpose,
                                                 OUT DWORD                    *pNotifications);

HRESULT WINAPI LiveEngine_SetProperty(           IN  LiveEngine               *pThis,
                                                 IN  UIX_PROPERTY_TYPE         Property,
                                                 IN  DWORD                     Value);

HRESULT WINAPI LiveEngine_GetProperty(           IN  LiveEngine               *pThis,
                                                 IN  UIX_PROPERTY_TYPE         Property,
                                                 OUT DWORD                    *pValue);

HRESULT WINAPI LiveEngine_GetExitInfo(           IN  LiveEngine               *pThis,
                                                 OUT UIX_EXIT_INFO            *pExitInfo);

HRESULT WINAPI LiveEngine_Reboot(                IN  LiveEngine               *pThis,
                                                 IN  DWORD                     Context);

HRESULT WINAPI LiveEngine_LogOff(                IN  LiveEngine               *pThis);

//-----------------------------------------------------------------------------
//
//  UIX APIs
//
//-----------------------------------------------------------------------------

XBOXAPI
HRESULT
WINAPI
UIXCreateLiveEngine(
    IN  LPCSTR                          pSkinFileName,
    IN  DWORD                           LanguageID,
    OUT ILiveEngine                   **ppEngine
    );

XBOXAPI
HRESULT
WINAPI
UIXCreateUIPlugin(
    IN  ITitleFontRenderer             *pFont,
    OUT ITitleUIPlugin                **ppUIPlugin
    );

XBOXAPI
HRESULT
WINAPI
UIXCreateAudioPlugin(
    IN  VOID                           *pXactEngine,
    IN  VOID                           *pXactWaveBank,
    OUT ITitleAudioPlugin             **ppAudioPlugin
    );

//-----------------------------------------------------------------------------
//
//  Compatibility wrappers for the interfaces
//
//-----------------------------------------------------------------------------

//-----------------------------------------------------------------------------
//
// Compatibility wrappers - IPluginSupport
//

UIXINLINE ULONG   WINAPI IPluginSupport_AddRef(PluginSupport *pThis)
                         { return PluginSupport_AddRef(pThis); }
UIXINLINE ULONG   WINAPI IPluginSupport_Release(PluginSupport *pThis)
                         { return PluginSupport_Release(pThis); }
UIXINLINE HRESULT WINAPI IPluginSupport_GetString(PluginSupport *pThis, DWORD StringResID, LPCWSTR *ppString)
                         { return PluginSupport_GetString(pThis, StringResID, ppString);}
UIXINLINE HRESULT WINAPI IPluginSupport_GetLayout( PluginSupport *pThis, DWORD ScreenResID, DWORD ObjectResID, UIX_SKIN_LAYOUT_INFO **ppLayout)
                         { return PluginSupport_GetLayout( pThis, ScreenResID, ObjectResID, ppLayout); }
UIXINLINE HRESULT WINAPI IPluginSupport_GetImage(PluginSupport *pThis, DWORD ImageResID, IDirect3DTexture8 **ppTexture)
                         { return PluginSupport_GetImage(pThis, ImageResID, ppTexture);}
UIXINLINE HRESULT WINAPI IPluginSupport_GetScreenImage( PluginSupport *pThis, DWORD ScreenResID, DWORD ImageResID, IDirect3DTexture8 **ppTexture)
                         { return PluginSupport_GetScreenImage(pThis, ScreenResID, ImageResID, ppTexture);}
UIXINLINE HRESULT WINAPI IPluginSupport_GetWordLength( PluginSupport *pThis, LPCWSTR pString, DWORD *pWordLength)
                         { return PluginSupport_GetWordLength( pThis, pString, pWordLength); }

#ifdef __cplusplus

UIXINLINE ULONG   WINAPI IPluginSupport::AddRef()
                         { return PluginSupport_AddRef(this); }
UIXINLINE ULONG   WINAPI IPluginSupport::Release()
                         { return PluginSupport_Release(this); }
UIXINLINE HRESULT WINAPI IPluginSupport::GetString(DWORD StringResID, LPCWSTR *ppString)
                         { return PluginSupport_GetString(this, StringResID, ppString);}
UIXINLINE HRESULT WINAPI IPluginSupport::GetLayout(DWORD ScreenResID, DWORD ObjectResID, UIX_SKIN_LAYOUT_INFO **ppLayout)
                         { return PluginSupport_GetLayout( this, ScreenResID, ObjectResID, ppLayout); }
UIXINLINE HRESULT WINAPI IPluginSupport::GetImage(DWORD ImageResID, IDirect3DTexture8 **ppTexture)
                         { return PluginSupport_GetImage(this, ImageResID, ppTexture);}
UIXINLINE HRESULT WINAPI IPluginSupport::GetScreenImage(DWORD ScreenResID, DWORD ImageResID, IDirect3DTexture8 **ppTexture)
                         { return PluginSupport_GetScreenImage(this, ScreenResID, ImageResID, ppTexture);}
UIXINLINE HRESULT WINAPI IPluginSupport::GetWordLength( LPCWSTR pString, DWORD *pWordLength)
                         { return PluginSupport_GetWordLength( this,  pString, pWordLength); }

#endif __cplusplus

//-----------------------------------------------------------------------------
//
// Compatibility wrappers - ILiveFriendsList
//

UIXINLINE ULONG   WINAPI ILiveFriendsList_AddRef(LiveFriendsList *pThis)
                         { return LiveFriendsList_AddRef(pThis); }
UIXINLINE ULONG   WINAPI ILiveFriendsList_Release(LiveFriendsList *pThis)
                         { return LiveFriendsList_Release(pThis); }
UIXINLINE HRESULT WINAPI ILiveFriendsList_Refresh(LiveFriendsList *pThis)
                         { return LiveFriendsList_Refresh(pThis); }
UIXINLINE BOOL    WINAPI ILiveFriendsList_IsReady(LiveFriendsList *pThis)
                         { return LiveFriendsList_IsReady(pThis); }
UIXINLINE BOOL    WINAPI ILiveFriendsList_HasChanged(LiveFriendsList *pThis)
                         { return LiveFriendsList_HasChanged(pThis); }
UIXINLINE DWORD   WINAPI ILiveFriendsList_Count(LiveFriendsList *pThis)
                         { return LiveFriendsList_Count(pThis); }
UIXINLINE HRESULT WINAPI ILiveFriendsList_GetFriendByIndex(LiveFriendsList *pThis, DWORD FriendIndex, XONLINE_FRIEND *pFriend)
                         { return LiveFriendsList_GetFriendByIndex(pThis, FriendIndex, pFriend); }
UIXINLINE HRESULT WINAPI ILiveFriendsList_GetFriendByXUID(LiveFriendsList *pThis, XUID FriendXUID, XONLINE_FRIEND *pFriend)
                         { return LiveFriendsList_GetFriendByXUID(pThis, FriendXUID, pFriend); }
UIXINLINE HRESULT WINAPI ILiveFriendsList_Remove(LiveFriendsList *pThis, const XONLINE_FRIEND *pFriend)
                         { return LiveFriendsList_Remove(pThis, pFriend); }
UIXINLINE HRESULT WINAPI ILiveFriendsList_Request(LiveFriendsList *pThis, XUID UserXUID)
                         { return LiveFriendsList_Request(pThis, UserXUID); }

#ifdef __cplusplus

UIXINLINE ULONG   WINAPI ILiveFriendsList::AddRef()
                         { return LiveFriendsList_AddRef(this); }
UIXINLINE ULONG   WINAPI ILiveFriendsList::Release()
                         { return LiveFriendsList_Release(this); }
UIXINLINE HRESULT WINAPI ILiveFriendsList::Refresh()
                         { return LiveFriendsList_Refresh(this); }
UIXINLINE BOOL    WINAPI ILiveFriendsList::IsReady()
                         { return LiveFriendsList_IsReady(this); }
UIXINLINE BOOL    WINAPI ILiveFriendsList::HasChanged()
                         { return LiveFriendsList_HasChanged(this); }
UIXINLINE DWORD   WINAPI ILiveFriendsList::Count()
                         { return LiveFriendsList_Count(this); }
UIXINLINE HRESULT WINAPI ILiveFriendsList::GetFriendByIndex(DWORD FriendIndex, XONLINE_FRIEND *pFriend)
                         { return LiveFriendsList_GetFriendByIndex(this, FriendIndex, pFriend); }
UIXINLINE HRESULT WINAPI ILiveFriendsList::GetFriendByXUID(XUID FriendXUID, XONLINE_FRIEND *pFriend)
                         { return LiveFriendsList_GetFriendByXUID(this, FriendXUID, pFriend); }
UIXINLINE HRESULT WINAPI ILiveFriendsList::Remove(const XONLINE_FRIEND *pFriend)
                         { return LiveFriendsList_Remove(this, pFriend); }
UIXINLINE HRESULT WINAPI ILiveFriendsList::Request(XUID UserXUID)
                         { return LiveFriendsList_Request(this, UserXUID); }

#endif __cplusplus

//-----------------------------------------------------------------------------
//
// Compatibility wrappers - ILiveEngine
//

UIXINLINE ULONG   WINAPI ILiveEngine_AddRef(LiveEngine *pThis)
                         { return LiveEngine_AddRef(pThis); }
UIXINLINE ULONG   WINAPI ILiveEngine_Release(LiveEngine *pThis)
                         { return LiveEngine_Release(pThis); }
UIXINLINE HRESULT WINAPI ILiveEngine_DoWork(LiveEngine *pThis, DWORD *pDoWorkFlags)
                         { return LiveEngine_DoWork(pThis, pDoWorkFlags); }
UIXINLINE HRESULT WINAPI ILiveEngine_SetUIPlugin(LiveEngine *pThis, ITitleUIPlugin *pUIPlugin)
                         { return LiveEngine_SetUIPlugin(pThis, pUIPlugin); }
UIXINLINE HRESULT WINAPI ILiveEngine_SetAudioPlugin(LiveEngine *pThis, ITitleAudioPlugin *pAudioPlugin)
                         { return LiveEngine_SetAudioPlugin(pThis, pAudioPlugin); }
UIXINLINE HRESULT WINAPI ILiveEngine_EnableFeature(LiveEngine *pThis, UIX_FEATURE FeatureID)
                         { return LiveEngine_EnableFeature(pThis, FeatureID); }
UIXINLINE HRESULT WINAPI ILiveEngine_StartFeature(LiveEngine *pThis, UIX_FEATURE FeatureID, const VOID *pFeatureParams)
                         { return LiveEngine_StartFeature(pThis, FeatureID, pFeatureParams); }
UIXINLINE HRESULT WINAPI ILiveEngine_Render(LiveEngine *pThis, IDirect3DSurface8 *pSurface)
                         { return LiveEngine_Render(pThis, pSurface); }
UIXINLINE HRESULT WINAPI ILiveEngine_SetInput(LiveEngine *pThis, DWORD Port, const XINPUT_STATE *pInputState)
                         { return LiveEngine_SetInput(pThis, Port, pInputState); }
UIXINLINE HRESULT WINAPI ILiveEngine_GetFriendsForUser(LiveEngine *pThis, DWORD Port, ILiveFriendsList **ppFriendsList)
                         { return LiveEngine_GetFriendsForUser(pThis, Port, ppFriendsList); }
UIXINLINE HRESULT WINAPI ILiveEngine_NotificationSetState(LiveEngine *pThis, DWORD Port, DWORD StateFlags, XNKID SessionID, DWORD StateDataSize, const PVOID pStateData)
                         { return LiveEngine_NotificationSetState(pThis, Port, StateFlags, SessionID, StateDataSize, pStateData); }
UIXINLINE HRESULT WINAPI ILiveEngine_GetNotifications(LiveEngine *pThis, DWORD Port, UIX_NOTIFICATION_PURPOSE Purpose, DWORD *pNotifications)
                         { return LiveEngine_GetNotifications(pThis, Port, Purpose, pNotifications); }
UIXINLINE HRESULT WINAPI ILiveEngine_SetProperty(LiveEngine *pThis, UIX_PROPERTY_TYPE Property, DWORD Value)
                         { return LiveEngine_SetProperty(pThis, Property, Value); }
UIXINLINE HRESULT WINAPI ILiveEngine_GetProperty(LiveEngine *pThis, UIX_PROPERTY_TYPE Property, DWORD* pValue)
                         { return LiveEngine_GetProperty(pThis, Property, pValue); }
UIXINLINE HRESULT WINAPI ILiveEngine_GetExitInfo(LiveEngine* pThis, UIX_EXIT_INFO* pExitInfo)
                         { return LiveEngine_GetExitInfo(pThis, pExitInfo); }
UIXINLINE HRESULT WINAPI ILiveEngine_Reboot(LiveEngine *pThis, DWORD Context)
                         { return LiveEngine_Reboot(pThis, Context); }
UIXINLINE HRESULT WINAPI ILiveEngine_LogOff(LiveEngine *pThis)
                         { return LiveEngine_LogOff(pThis); }

#ifdef __cplusplus

UIXINLINE ULONG   WINAPI ILiveEngine::AddRef()
                         { return LiveEngine_AddRef(this); }
UIXINLINE ULONG   WINAPI ILiveEngine::Release()
                         { return LiveEngine_Release(this); }
UIXINLINE HRESULT WINAPI ILiveEngine::DoWork(DWORD *pDoWorkFlags)
                         { return LiveEngine_DoWork(this, pDoWorkFlags); }
UIXINLINE HRESULT WINAPI ILiveEngine::SetUIPlugin(ITitleUIPlugin *pUIPlugin)
                         { return LiveEngine_SetUIPlugin(this, pUIPlugin); }
UIXINLINE HRESULT WINAPI ILiveEngine::SetAudioPlugin(ITitleAudioPlugin *pAudioPlugin)
                         { return LiveEngine_SetAudioPlugin(this, pAudioPlugin); }
UIXINLINE HRESULT WINAPI ILiveEngine::EnableFeature(UIX_FEATURE FeatureID)
                         { return LiveEngine_EnableFeature(this, FeatureID); }
UIXINLINE HRESULT WINAPI ILiveEngine::StartFeature(UIX_FEATURE FeatureID, const VOID *pFeatureParams)
                         { return LiveEngine_StartFeature(this, FeatureID, pFeatureParams); }
UIXINLINE HRESULT WINAPI ILiveEngine::Render(IDirect3DSurface8 *pSurface)
                         { return LiveEngine_Render(this, pSurface); }
UIXINLINE HRESULT WINAPI ILiveEngine::SetInput(DWORD Port, const XINPUT_STATE *pInputState)
                         { return LiveEngine_SetInput(this, Port, pInputState); }
UIXINLINE HRESULT WINAPI ILiveEngine::GetFriendsForUser(DWORD Port, ILiveFriendsList **ppFriendsList)
                         { return LiveEngine_GetFriendsForUser(this, Port, ppFriendsList); }
UIXINLINE HRESULT WINAPI ILiveEngine::NotificationSetState(DWORD Port, DWORD StateFlags, XNKID SessionID, DWORD StateDataSize, const PVOID pStateData)
                         { return LiveEngine_NotificationSetState(this, Port, StateFlags, SessionID, StateDataSize, pStateData); }
UIXINLINE HRESULT WINAPI ILiveEngine::GetNotifications(DWORD Port, UIX_NOTIFICATION_PURPOSE Purpose, DWORD *pNotifications)
                         { return LiveEngine_GetNotifications(this, Port, Purpose, pNotifications); }
UIXINLINE HRESULT WINAPI ILiveEngine::SetProperty(UIX_PROPERTY_TYPE Property, DWORD Value)
                         { return LiveEngine_SetProperty(this, Property, Value); }
UIXINLINE HRESULT WINAPI ILiveEngine::GetProperty(UIX_PROPERTY_TYPE Property, DWORD* pValue)
                         { return LiveEngine_GetProperty(this, Property, pValue); }
UIXINLINE HRESULT WINAPI ILiveEngine::GetExitInfo(UIX_EXIT_INFO* pExitInfo)
                         { return LiveEngine_GetExitInfo(this, pExitInfo); }
UIXINLINE HRESULT WINAPI ILiveEngine::Reboot(DWORD Context)
                         { return LiveEngine_Reboot(this, Context); }
UIXINLINE HRESULT WINAPI ILiveEngine::LogOff()
                         { return LiveEngine_LogOff(this); }

#endif __cplusplus

//
// Compatibility wrappers - End
//

#ifdef __cplusplus
}
#endif

#pragma warning( pop )

#endif // __UIX__
