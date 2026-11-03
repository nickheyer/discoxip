/*==========================================================================;
 *
 *  xonline.h -- This module defines the XBox Live APIs
 *
 *  Copyright (C) Microsoft Corporation.  All Rights Reserved.
 *
 ***************************************************************************/

#ifndef __XONLINE__
#define __XONLINE__

#ifdef __cplusplus
extern "C" {
#endif


//
// XOnline Startup & Cleanup
//

typedef struct _XONLINE_STARTUP_PARAMS {

    DWORD           dwMaxPrivatePool;

} XONLINE_STARTUP_PARAMS, * PXONLINE_STARTUP_PARAMS;

XBOXAPI
HRESULT
WINAPI
XOnlineStartup(
    IN const XONLINE_STARTUP_PARAMS*  pxosp
    );

XBOXAPI
HRESULT
WINAPI
XOnlineCleanup();


//
// XOnline Error Codes
//

#define FACILITY_XONLINE                               21

// Generic Errors
#define XONLINE_E_OVERFLOW                             _HRESULT_TYPEDEF_(0x80150001L)
#define XONLINE_E_NO_SESSION                           _HRESULT_TYPEDEF_(0x80150002L)
#define XONLINE_E_USER_NOT_LOGGED_ON                   _HRESULT_TYPEDEF_(0x80150003L)
#define XONLINE_E_NO_GUEST_ACCESS                      _HRESULT_TYPEDEF_(0x80150004L)
#define XONLINE_E_NOT_INITIALIZED                      _HRESULT_TYPEDEF_(0x80150005L)
#define XONLINE_E_NO_USER                              _HRESULT_TYPEDEF_(0x80150006L)
#define XONLINE_E_INTERNAL_ERROR                       _HRESULT_TYPEDEF_(0x80150007L)
#define XONLINE_E_OUT_OF_MEMORY                        _HRESULT_TYPEDEF_(0x80150008L)
#define XONLINE_E_TASK_BUSY                            _HRESULT_TYPEDEF_(0x80150009L)
#define XONLINE_E_SERVER_ERROR                         _HRESULT_TYPEDEF_(0x8015000AL)
#define XONLINE_E_IO_ERROR                             _HRESULT_TYPEDEF_(0x8015000BL)
#define XONLINE_E_BAD_CONTENT_TYPE                     _HRESULT_TYPEDEF_(0x8015000CL)
#define XONLINE_E_USER_NOT_PRESENT                     _HRESULT_TYPEDEF_(0x8015000DL)
#define XONLINE_E_PROTOCOL_MISMATCH                    _HRESULT_TYPEDEF_(0x8015000EL)
#define XONLINE_E_INVALID_SERVICE_ID                   _HRESULT_TYPEDEF_(0x8015000FL)
#define XONLINE_E_INVALID_REQUEST                      _HRESULT_TYPEDEF_(0x80150010L)

// Failures from XOnlineLogon
#define XONLINE_E_LOGON_NO_NETWORK_CONNECTION          _HRESULT_TYPEDEF_(0x80151000L)

// XOnlineLogon task successful return states
#define XONLINE_S_LOGON_CONNECTION_ESTABLISHED         _HRESULT_TYPEDEF_(0x001510F0L)

// XOnlineLogon task failure return values
#define XONLINE_E_LOGON_CANNOT_ACCESS_SERVICE          _HRESULT_TYPEDEF_(0x80151001L)
#define XONLINE_E_LOGON_UPDATE_REQUIRED                _HRESULT_TYPEDEF_(0x80151002L)
#define XONLINE_E_LOGON_SERVERS_TOO_BUSY               _HRESULT_TYPEDEF_(0x80151003L)
#define XONLINE_E_LOGON_CONNECTION_LOST                _HRESULT_TYPEDEF_(0x80151004L)
#define XONLINE_E_LOGON_KICKED_BY_DUPLICATE_LOGON      _HRESULT_TYPEDEF_(0x80151005L)
#define XONLINE_E_LOGON_INVALID_USER                   _HRESULT_TYPEDEF_(0x80151006L)

// Failures from XOnlineSilentLogon
#define XONLINE_E_SILENT_LOGON_DISABLED                _HRESULT_TYPEDEF_(0x80151080L)
#define XONLINE_E_SILENT_LOGON_NO_ACCOUNTS             _HRESULT_TYPEDEF_(0x80151081L)
#define XONLINE_E_SILENT_LOGON_PASSCODE_REQUIRED       _HRESULT_TYPEDEF_(0x80151082L)

// Service errors after XOnlineLogon task completion
#define XONLINE_E_LOGON_SERVICE_NOT_REQUESTED               _HRESULT_TYPEDEF_(0x80151100L)
#define XONLINE_E_LOGON_SERVICE_NOT_AUTHORIZED              _HRESULT_TYPEDEF_(0x80151101L)
#define XONLINE_E_LOGON_SERVICE_TEMPORARILY_UNAVAILABLE     _HRESULT_TYPEDEF_(0x80151102L)

// User warnings after XOnlineLogon task completion
#define XONLINE_S_LOGON_USER_HAS_MESSAGE                    _HRESULT_TYPEDEF_(0x001512F0L)

// User errors after XOnlineLogon task completion
#define XONLINE_E_LOGON_USER_ACCOUNT_REQUIRES_MANAGEMENT        _HRESULT_TYPEDEF_(0x80151200L)

// XOnlineChangeLogonUsers task successful return states
#define XONLINE_S_LOGON_COMMIT_USER_CHANGE              _HRESULT_TYPEDEF_(0x001513F0L)
#define XONLINE_S_LOGON_USER_CHANGE_COMPLETE            _HRESULT_TYPEDEF_(0x001513F1L)

// XOnlineChangeLogonUsers task failure return values
#define XONLINE_E_LOGON_CHANGE_USER_FAILED              _HRESULT_TYPEDEF_(0x80151300L)

// Other generic auth related errors
#define XONLINE_E_LOGON_MU_NOT_MOUNTED                 _HRESULT_TYPEDEF_(0x80151800L)
#define XONLINE_E_LOGON_MU_IO_ERROR                    _HRESULT_TYPEDEF_(0x80151801L)
#define XONLINE_E_LOGON_NOT_LOGGED_ON                  _HRESULT_TYPEDEF_(0x80151802L)

// Errors returned by Presence/Notification
#define XONLINE_E_NOTIFICATION_BAD_CONTENT_TYPE        _HRESULT_TYPEDEF_(0x80155000L)
#define XONLINE_E_NOTIFICATION_REQUEST_TOO_SMALL       _HRESULT_TYPEDEF_(0x80155001L)
#define XONLINE_E_NOTIFICATION_INVALID_MESSAGE_TYPE    _HRESULT_TYPEDEF_(0x80155002L)
#define XONLINE_E_NOTIFICATION_NO_ADDRESS              _HRESULT_TYPEDEF_(0x80155003L)
#define XONLINE_E_NOTIFICATION_INVALID_PUID            _HRESULT_TYPEDEF_(0x80155004L)
#define XONLINE_E_NOTIFICATION_NO_CONNECTION           _HRESULT_TYPEDEF_(0x80155005L)
#define XONLINE_E_NOTIFICATION_SEND_FAILED             _HRESULT_TYPEDEF_(0x80155006L)
#define XONLINE_E_NOTIFICATION_RECV_FAILED             _HRESULT_TYPEDEF_(0x80155007L)
#define XONLINE_E_NOTIFICATION_MESSAGE_TRUNCATED       _HRESULT_TYPEDEF_(0x80155008L)
#define XONLINE_E_NOTIFICATION_SERVER_BUSY             _HRESULT_TYPEDEF_(0x80152001L)
#define XONLINE_E_NOTIFICATION_LIST_FULL               _HRESULT_TYPEDEF_(0x80152002L)
#define XONLINE_E_NOTIFICATION_BLOCKED                 _HRESULT_TYPEDEF_(0x80152003L)
#define XONLINE_E_NOTIFICATION_FRIEND_PENDING          _HRESULT_TYPEDEF_(0x80152004L)
#define XONLINE_E_NOTIFICATION_FLUSH_TICKETS           _HRESULT_TYPEDEF_(0x80152005L)
#define XONLINE_E_NOTIFICATION_TOO_MANY_REQUESTS       _HRESULT_TYPEDEF_(0x80152006L)
#define XONLINE_E_NOTIFICATION_USER_ALREADY_EXISTS     _HRESULT_TYPEDEF_(0x80152007L)
#define XONLINE_E_NOTIFICATION_USER_NOT_FOUND          _HRESULT_TYPEDEF_(0x80152008L)
#define XONLINE_E_NOTIFICATION_OTHER_LIST_FULL         _HRESULT_TYPEDEF_(0x80152009L)
#define XONLINE_E_NOTIFICATION_SELF                    _HRESULT_TYPEDEF_(0x8015200AL)
#define XONLINE_E_NOTIFICATION_SAME_TITLE              _HRESULT_TYPEDEF_(0x8015200BL)
#define XONLINE_E_NOTIFICATION_NO_TASK                 _HRESULT_TYPEDEF_(0x8015200CL)

//  Errors returned by matchmaking
#define XONLINE_E_MATCH_INVALID_SESSION_ID             _HRESULT_TYPEDEF_(0x80155100L)  // specified session id does not exist
#define XONLINE_E_MATCH_INVALID_TITLE_ID               _HRESULT_TYPEDEF_(0x80155101L)  // specified title id is zero, or does not exist
#define XONLINE_E_MATCH_INVALID_DATA_TYPE              _HRESULT_TYPEDEF_(0x80155102L)  // attribute ID or parameter type specifies an invalid data type
#define XONLINE_E_MATCH_REQUEST_TOO_SMALL              _HRESULT_TYPEDEF_(0x80155103L)  // the request did not meet the minimum length for a valid request
#define XONLINE_E_MATCH_REQUEST_TRUNCATED              _HRESULT_TYPEDEF_(0x80155104L)  // the self described length is greater than the actual buffer size
#define XONLINE_E_MATCH_INVALID_SEARCH_REQ             _HRESULT_TYPEDEF_(0x80155105L)  // the search request was invalid
#define XONLINE_E_MATCH_INVALID_OFFSET                 _HRESULT_TYPEDEF_(0x80155106L)  // one of the attribute/parameter offsets in the request was invalid.  Will be followed by the zero based offset number.
#define XONLINE_E_MATCH_INVALID_ATTR_TYPE              _HRESULT_TYPEDEF_(0x80155107L)  // the attribute type was something other than user or session
#define XONLINE_E_MATCH_INVALID_VERSION                _HRESULT_TYPEDEF_(0x80155108L)  // bad protocol version in request
#define XONLINE_E_MATCH_OVERFLOW                       _HRESULT_TYPEDEF_(0x80155109L)  // an attribute or parameter flowed past the end of the request
#define XONLINE_E_MATCH_INVALID_RESULT_COL             _HRESULT_TYPEDEF_(0x8015510AL)  // referenced stored procedure returned a column with an unsupported data type
#define XONLINE_E_MATCH_INVALID_STRING                 _HRESULT_TYPEDEF_(0x8015510BL)  // string with length-prefix of zero, or string with no terminating null
#define XONLINE_E_MATCH_STRING_TOO_LONG                _HRESULT_TYPEDEF_(0x8015510CL)  // string exceeded 400 characters
#define XONLINE_E_MATCH_BLOB_TOO_LONG                  _HRESULT_TYPEDEF_(0x8015510DL)  // blob exceeded 800 bytes
#define XONLINE_E_MATCH_INVALID_ATTRIBUTE_ID           _HRESULT_TYPEDEF_(0x80155110L)  // attribute id is invalid
#define XONLINE_E_MATCH_SESSION_ALREADY_EXISTS         _HRESULT_TYPEDEF_(0x80155112L)  // session id already exists in the db
#define XONLINE_E_MATCH_CRITICAL_DB_ERR                _HRESULT_TYPEDEF_(0x80155115L)  // critical error in db
#define XONLINE_E_MATCH_NOT_ENOUGH_COLUMNS             _HRESULT_TYPEDEF_(0x80155116L)  // search result set had too few columns
#define XONLINE_E_MATCH_PERMISSION_DENIED              _HRESULT_TYPEDEF_(0x80155117L)  // incorrect permissions set on search sp
#define XONLINE_E_MATCH_INVALID_PART_SCHEME            _HRESULT_TYPEDEF_(0x80155118L)  // title specified an invalid partitioning scheme
#define XONLINE_E_MATCH_INVALID_PARAM                  _HRESULT_TYPEDEF_(0x80155119L)  // bad parameter passed to sp
#define XONLINE_E_MATCH_DATA_TYPE_MISMATCH             _HRESULT_TYPEDEF_(0x8015511DL)  // data type specified in attr id did not match type of attr being set
#define XONLINE_E_MATCH_SERVER_ERROR                   _HRESULT_TYPEDEF_(0x8015511EL)  // error on server not correctable by client
#define XONLINE_E_MATCH_NO_USERS                       _HRESULT_TYPEDEF_(0x8015511FL)  // no authenticated users in search request.
#define XONLINE_E_MATCH_INVALID_BLOB                   _HRESULT_TYPEDEF_(0x80155120L)  // invalid blob attribute

// Errors returned by billing/offerings
#define XONLINE_S_OFFERING_NEW_CONTENT                 _HRESULT_TYPEDEF_(0x00153101L)  // new content is available
#define XONLINE_S_OFFERING_NO_NEW_CONTENT              _HRESULT_TYPEDEF_(0x00153102L)  // no new content is available

#define XONLINE_E_OFFERING_BAD_REQUEST                 _HRESULT_TYPEDEF_(0x80153001L)  // server received incorrectly formatted request
#define XONLINE_E_OFFERING_INVALID_USER                _HRESULT_TYPEDEF_(0x80153002L)  // cannot find account for this user
#define XONLINE_E_OFFERING_INVALID_OFFER_ID            _HRESULT_TYPEDEF_(0x80153003L)  // offer does not exist
#define XONLINE_E_OFFERING_INELIGIBLE_FOR_OFFER        _HRESULT_TYPEDEF_(0x80153004L)  // )] private /title not allowed to purchase offer
#define XONLINE_E_OFFERING_OFFER_EXPIRED               _HRESULT_TYPEDEF_(0x80153005L)  // offer no longer available
#define XONLINE_E_OFFERING_SERVICE_UNREACHABLE         _HRESULT_TYPEDEF_(0x80153006L)  // apparent connectivity problems
#define XONLINE_E_OFFERING_PURCHASE_BLOCKED            _HRESULT_TYPEDEF_(0x80153007L)  // this user is not allowed to make purchases
#define XONLINE_E_OFFERING_PURCHASE_DENIED             _HRESULT_TYPEDEF_(0x80153008L)  // this user's payment is denied by billing provider
#define XONLINE_E_OFFERING_BILLING_SERVER_ERROR        _HRESULT_TYPEDEF_(0x80153009L)  // nonspecific billing provider error
#define XONLINE_E_OFFERING_OFFER_NOT_CANCELABLE        _HRESULT_TYPEDEF_(0x8015300AL)  // either this offer doesn't exist, or it's marked as un-cancelable
#define XONLINE_E_OFFERING_NOTHING_TO_CANCEL           _HRESULT_TYPEDEF_(0x8015300BL)  // this user doesn't have one of these anyways
#define XONLINE_E_OFFERING_ALREADY_OWN_MAX             _HRESULT_TYPEDEF_(0x8015300CL)  // this user already owns the maximum allowed
#define XONLINE_E_OFFERING_NO_CHARGE                   _HRESULT_TYPEDEF_(0x8015300DL)  // this is a free offer; no purchase is necessary
#define XONLINE_E_OFFERING_PERMISSION_DENIED           _HRESULT_TYPEDEF_(0x8015300EL)  // permission denied
#define XONLINE_E_OFFERING_NAME_TAKEN                  _HRESULT_TYPEDEF_(0x8015300FL)  // Name given to XOnlineVerifyNickname is taken (dosen't vet)

#define XONLINE_E_BILLING_AUTHORIZATION_FAILED              _HRESULT_TYPEDEF_(0x80167611) // Credit card authorization failed; user should update credit card info in Dash.
#define XONLINE_E_BILLING_CREDIT_CARD_EXPIRED               _HRESULT_TYPEDEF_(0x80167531) // The credit card has expired or will expire this month; user should update card info in Dash.
#define XONLINE_E_BILLING_NON_ACTIVE_ACCOUNT                _HRESULT_TYPEDEF_(0x80169d94) // The account specified is no longer active; user should call customer service
#define XONLINE_E_BILLING_INVALID_PAYMENT_INSTRUMENT_STATUS _HRESULT_TYPEDEF_(0x80169e7f) // User's payment instrument is in a bad state. They should call customer service to rectify the issue.

// Errors returned by uodb procs
#define XONLINE_E_UODB_KEY_ALREADY_EXISTS               _HRESULT_TYPEDEF_(0x80156000L)  // service key already exists when attempting to insert key

// Errors returned by the Message Service
#define XONLINE_E_MSGSVR_INVALID_REQUEST                _HRESULT_TYPEDEF_(0x80157001L)  // an invalid request type was received

// Errors returned by the Feedback Service
#define XONLINE_E_FEEDBACK_NULL_TARGET                  _HRESULT_TYPEDEF_(0x80158001L) // target PUID of feedback is NULL
#define XONLINE_E_FEEDBACK_BAD_TYPE                     _HRESULT_TYPEDEF_(0x80158002L) // bad feedback type
#define XONLINE_E_FEEDBACK_CANNOT_LOG                   _HRESULT_TYPEDEF_(0x80158006L) // cannot write to feedback log

// Errors returned by the Statistics Service
#define XONLINE_E_STAT_BAD_REQUEST                      _HRESULT_TYPEDEF_(0x80159001L)   // server received incorrectly formatted request.
#define XONLINE_E_STAT_INVALID_TITLE_OR_LEADERBOARD     _HRESULT_TYPEDEF_(0x80159002L)   // title or leaderboard id were not recognized by the server.
#define XONLINE_E_STAT_TOO_MANY_SPECS                   _HRESULT_TYPEDEF_(0x80159004L)   // too many stat specs in a request.
#define XONLINE_E_STAT_TOO_MANY_STATS                   _HRESULT_TYPEDEF_(0x80159005L)   // too many stats in a spec or already stored for the user.
#define XONLINE_E_STAT_USER_NOT_FOUND                   _HRESULT_TYPEDEF_(0x80159003L)   // user not found.
#define XONLINE_E_STAT_SET_FAILED_0                     _HRESULT_TYPEDEF_(0x80159100L)   // set operation failed on spec index 0
#define XONLINE_E_STAT_PERMISSION_DENIED                _HRESULT_TYPEDEF_(0x80159200L)   // operation failed because of credentials. UserId is not logged in or this operation is not supported in production (e.g. userId=0 in XOnlineStatReset)
#define XONLINE_E_STAT_LEADERBOARD_WAS_RESET            _HRESULT_TYPEDEF_(0x80159201L)   // operation failed because user was logged on before the leaderboard was reset.
#define XONLINE_E_STAT_INVALID_ATTACHMENT               _HRESULT_TYPEDEF_(0x80159202L)    // attachment is invalid.
#define XONLINE_S_STAT_CAN_UPLOAD_ATTACHMENT            _HRESULT_TYPEDEF_(0x00159203L)    // Use XOnlineStatWriteGetResults to get a handle to upload a attachment.

// Errors returned by the Storage services
#define XONLINE_E_STORAGE_INVALID_REQUEST               _HRESULT_TYPEDEF_(0x8015c001L)  // Request is invalid
#define XONLINE_E_STORAGE_ACCESS_DENIED                 _HRESULT_TYPEDEF_(0x8015c002L)  // Client doesn't have the rights to upload the file
#define XONLINE_E_STORAGE_FILE_IS_TOO_BIG               _HRESULT_TYPEDEF_(0x8015c003L)  // File is too big
#define XONLINE_E_STORAGE_FILE_NOT_FOUND                _HRESULT_TYPEDEF_(0x8015c004L)  // File not found
#define XONLINE_E_STORAGE_INVALID_ACCESS_TOKEN          _HRESULT_TYPEDEF_(0x8015c005L)  // Access token signature is invalid
#define XONLINE_E_STORAGE_CANNOT_FIND_PATH              _HRESULT_TYPEDEF_(0x8015c006L)  // name resolution failed
#define XONLINE_E_STORAGE_FILE_IS_ELSEWHERE             _HRESULT_TYPEDEF_(0x8015c007L)  // redirection request
#define XONLINE_E_STORAGE_INVALID_STORAGE_PATH          _HRESULT_TYPEDEF_(0x8015c008L)  // Invalid storage path
#define XONLINE_E_STORAGE_INVALID_FACILITY              _HRESULT_TYPEDEF_(0x8015c009L)  // Invalid facility code
#define XONLINE_E_STORAGE_UNKNOWN_DOMAIN                _HRESULT_TYPEDEF_(0x8015c00AL)  // Bad pathname
#define XONLINE_E_STORAGE_SYNC_TIME_SKEW                _HRESULT_TYPEDEF_(0x8015c00BL)  // SyncDomain timestamp skew
#define XONLINE_E_STORAGE_SYNC_TIME_SKEW_LOCALTIME      _HRESULT_TYPEDEF_(0x8015c00CL)  // SyncDomain timestamp appears to be localtime












//
// XOnline Task Pump
//

DECLARE_HANDLE(XONLINETASK_HANDLE);
typedef XONLINETASK_HANDLE* PXONLINETASK_HANDLE;

#define XONLINETASK_S_RUNNING                   (S_OK)
#define XONLINETASK_S_SUCCESS                   _HRESULT_TYPEDEF_(0x001500F0L)
#define XONLINETASK_S_RESULTS_AVAIL             _HRESULT_TYPEDEF_(0x001500F1L)
#define XONLINETASK_S_RUNNING_IDLE              _HRESULT_TYPEDEF_(0x001500F2L)


XBOXAPI
HRESULT
WINAPI
XOnlineTaskContinue(
    IN XONLINETASK_HANDLE hTask
    );

XBOXAPI
HRESULT
WINAPI
XOnlineTaskClose(
    IN XONLINETASK_HANDLE hTask
    );



//
// XOnline Authentication
//

#define XONLINE_GAMERTAG_SIZE                   16
#define XONLINE_MAX_GAMERTAG_LENGTH             (XONLINE_GAMERTAG_SIZE - 1)
#define XONLINE_PASSCODE_LENGTH                  4
#define XONLINE_MAX_LOGON_USERS                  4

typedef enum {
    XONLINE_PASSCODE_DPAD_UP = 1,
    XONLINE_PASSCODE_DPAD_DOWN,
    XONLINE_PASSCODE_DPAD_LEFT,
    XONLINE_PASSCODE_DPAD_RIGHT,
    XONLINE_PASSCODE_GAMEPAD_X,
    XONLINE_PASSCODE_GAMEPAD_Y,
    XONLINE_PASSCODE_GAMEPAD_LEFT_TRIGGER = 9,
    XONLINE_PASSCODE_GAMEPAD_RIGHT_TRIGGER
} XONLINE_PASSCODE_TYPE;


#define XONLINE_CONTENT_AVAILABLE_SERVICE       ((DWORD)4)
#define XONLINE_MATCHMAKING_SERVICE             ((DWORD)6)
#define XONLINE_STATISTICS_SERVICE              ((DWORD)7)
#define XONLINE_FEEDBACK_SERVICE                ((DWORD)8)
#define XONLINE_BILLING_OFFERING_SERVICE        ((DWORD)9)
#define XONLINE_NICKNAME_VERIFICATION_SERVICE   ((DWORD)9)
#define XONLINE_DIAGNOSTIC_LOGGING_SERVICE      ((DWORD)10)
#define XONLINE_SIGNATURE_SERVICE               ((DWORD)12)
#define XONLINE_STORAGE_SERVICE                 ((DWORD)15)

#define XONLINE_INVALID_SERVICE                 ((DWORD)0)


#define XONLINE_USER_COUNTRY_MASK           0x0000ff00
#define XONLINE_USER_GUEST_MASK             0x00000003
#define XONLINE_USER_VOICE_NOT_ALLOWED      0x00010000
#define XONLINE_USER_PURCHASE_NOT_ALLOWED   0x00020000
#define XONLINE_USER_NICKNAME_NOT_ALLOWED   0x00040000

#define XOnlineUserCountryId(dwUserFlags) ((BYTE)(((dwUserFlags) & XONLINE_USER_COUNTRY_MASK) >> 8))

#define XOnlineIsUserVoiceAllowed(dwUserFlags) (((dwUserFlags) & XONLINE_USER_VOICE_NOT_ALLOWED) == 0)

#define XOnlineIsUserPurchaseAllowed(dwUserFlags) (((dwUserFlags) & XONLINE_USER_PURCHASE_NOT_ALLOWED) == 0)

#define XOnlineIsUserNicknameAllowed(dwUserFlags) (((dwUserFlags) & XONLINE_USER_NICKNAME_NOT_ALLOWED) == 0)

#define XOnlineIsUserGuest(dwUserFlags) (((dwUserFlags) & XONLINE_USER_GUEST_MASK) != 0)

#define XOnlineUserGuestNumber(dwUserFlags) ((dwUserFlags) & XONLINE_USER_GUEST_MASK)

#define XOnlineSetUserGuestNumber(dwUserFlags,guestNumber) ((dwUserFlags) = ((dwUserFlags) & ~XONLINE_USER_GUEST_MASK) | (guestNumber & XONLINE_USER_GUEST_MASK))


#pragma pack(push, 4)

typedef struct _XUID {
    ULONGLONG qwUserID;
    DWORD dwUserFlags;
} XUID;

#pragma pack(pop)

#define XOnlineAreUsersIdentical(pXUID1, pXUID2) (((pXUID1)->qwUserID == (pXUID2)->qwUserID) && \
                (XOnlineUserGuestNumber((pXUID1)->dwUserFlags) == XOnlineUserGuestNumber((pXUID2)->dwUserFlags)))


typedef ULONGLONG XOFFERING_ID;


#pragma pack(push, 4)

#define XONLINE_USER_RESERVED_SIZE              72
#define XONLINE_MAX_STORED_ONLINE_USERS         16


#define XONLINE_USER_OPTION_REQUIRE_PASSCODE    0x00000001
#define XONLINE_USER_OPTION_CAME_FROM_MU        0x80000000
#define XONLINE_USER_OPTION_MU_PORT_MASK        0x60000000
#define XONLINE_USER_OPTION_MU_PORT_SHIFT               29
#define XONLINE_USER_OPTION_MU_SLOT_MASK        0x10000000
#define XONLINE_USER_OPTION_MU_SLOT_SHIFT               28

typedef struct _XONLINE_USER {
    XUID xuid;
    CHAR szGamertag[XONLINE_GAMERTAG_SIZE];
    DWORD dwUserOptions;
    BYTE passcode[XONLINE_PASSCODE_LENGTH];
    BYTE reserved[XONLINE_USER_RESERVED_SIZE];
    HRESULT hr;
} XONLINE_USER, *PXONLINE_USER;

typedef struct _XONLINE_SERVICE_INFO{
    DWORD          dwServiceID;
    IN_ADDR        serviceIP;
    WORD           wServicePort;
    WORD           wReserved;
} XONLINE_SERVICE_INFO, *PXONLINE_SERVICE_INFO;

#pragma pack(pop)


XBOXAPI
HRESULT
WINAPI
XOnlineGetUsers (
    OUT PXONLINE_USER pUsers,
    OUT DWORD *pdwUsers
    );

XBOXAPI
HRESULT
WINAPI
XOnlineLogon(
    IN const XONLINE_USER* pUsers,
    IN const DWORD* pdwServiceIDs,
    IN DWORD dwServices,
    IN HANDLE hEvent,
    OUT PXONLINETASK_HANDLE pHandle
    );

XBOXAPI
HRESULT
WINAPI
XOnlineLogonTaskGetResults(
    IN XONLINETASK_HANDLE hLogonTask
    );


XBOXAPI
HRESULT
WINAPI
XOnlineChangeLogonUsers(
    IN const XONLINE_USER* pUsers,
    IN HANDLE hEvent,
    OUT PXONLINETASK_HANDLE pHandle
    );

XBOXAPI
HRESULT
WINAPI
XOnlineChangeLogonUsersTaskGetResults(
    IN XONLINETASK_HANDLE hLogonTask,
    OUT HRESULT* phr
    );

XBOXAPI
PXONLINE_USER
WINAPI
XOnlineGetLogonUsers();

XBOXAPI
HRESULT
WINAPI
XOnlineGetServiceInfo(
    IN DWORD dwServiceID,
    OUT PXONLINE_SERVICE_INFO pServiceInfo
    );

XBOXAPI
HRESULT
WINAPI
XOnlineSilentLogon(
    IN const DWORD* pdwServiceIDs,
    IN DWORD dwServices,
    IN HANDLE hEvent,
    OUT PXONLINETASK_HANDLE pHandle
    );

//
// Preserving state across reboots
//

#define XONLINE_MAX_LOGON_STATE_SERVICES    12
#define XONLINE_LOGON_STATE_SIZE    (XONLINE_MAX_LOGON_USERS * sizeof(XONLINE_USER) + XONLINE_MAX_LOGON_STATE_SERVICES * sizeof(DWORD))

#define XONLINE_LOGON_STATE_TYPE 0x4C
#define XONLINE_LOGON_STATE_VERSION 1

typedef struct _XONLINE_LOGON_STATE {
    BYTE bType;
    BYTE bVersion;
    WORD cbSize;
    BYTE Data[XONLINE_LOGON_STATE_SIZE];
} XONLINE_LOGON_STATE, * PXONLINE_LOGON_STATE;

//
// Launch data passed to Downloader.XBE
//

#define LAUNCH_DATA_DOWNLOADER_ID  'dl01'

typedef struct _LD_DOWNLOADER {
    DWORD dwID;
    DWORD dwBitFilter;
    BYTE  bPremiumLogonPort;
    BYTE  Reserved1[3];
    DWORD Reserved2;
    XONLINE_LOGON_STATE LogonState;
    BYTE  UserDefined[MAX_LAUNCH_DATA_SIZE - (20 + XONLINE_LOGON_STATE_SIZE)];
} LD_DOWNLOADER, *PLD_DOWNLOADER;

C_ASSERT(sizeof(LD_DOWNLOADER) == MAX_LAUNCH_DATA_SIZE);


XBOXAPI
HRESULT
WINAPI
XOnlineSaveLogonState(
    OUT PXONLINE_LOGON_STATE pLogonState
    );

XBOXAPI
HRESULT
WINAPI
XOnlineRetrieveLogonState(
    IN const XONLINE_LOGON_STATE* pLogonState,
    OUT PXONLINE_USER pUsers,
    OUT DWORD* pdwServiceIDs,
    IN OUT DWORD* pdwServices
    );


//
// XOnline Title Update (Security updates)
//

XBOXAPI
HRESULT
WINAPI
XOnlineTitleUpdate(
    IN DWORD dwContext
    );

//
// Signature Service APIs
//

typedef struct
{
    DWORD   cbDigest;
    PBYTE   pbDigest;
    DWORD   cbOnlineSignature;
    PBYTE   pbOnlineSignature;

} XONLINE_SIGNATURE_TO_VERIFY;

typedef XONLINE_SIGNATURE_TO_VERIFY *PXONLINE_SIGNATURE_TO_VERIFY;

XBOXAPI
HRESULT
WINAPI
XOnlineSignatureVerify(
    IN PXONLINE_SIGNATURE_TO_VERIFY rgSignaturesToVerify,
    IN DWORD cSignaturesToVerify,
    IN HANDLE hWorkEvent,
    OUT XONLINETASK_HANDLE *phTask
    );

XBOXAPI
HRESULT
WINAPI
XOnlineSignatureVerifyGetResults(
    IN XONLINETASK_HANDLE hTask,
    OUT HRESULT **prgHresults,
    OUT DWORD *pcHresults
    );

XBOXAPI
HRESULT
WINAPI
XOnlineStorageUpload(
    IN HANDLE hServerFileReference,
    IN LPCSTR szDirectory,
    IN DWORD dwUploadFlags,
    IN HANDLE hWorkEvent,
    OUT XONLINETASK_HANDLE *phTask
    );

#define XONLINESTORAGE_FACILITY_STATS           ((DWORD)1)

XBOXAPI
HRESULT
WINAPI
XOnlineStorageDownload(
    IN DWORD dwFacility,
    IN DWORD dwUserIndex,
    IN LPCWSTR wszStoragePath,
    IN LPCSTR szInstallDirectory,
    IN DWORD dwDownloadFlags,
    IN HANDLE hWorkEvent,
    OUT XONLINETASK_HANDLE *phTask
    );

#define XONLINESTORAGE_MAX_PATH                 256

XBOXAPI
HRESULT
WINAPI
XOnlineStorageGetProgress(
    IN XONLINETASK_HANDLE  hTask,
    OUT DWORD *pdwPercentDone,
    OUT ULONGLONG *pqwNumerator,
    OUT ULONGLONG *pqwDenominator
    );

XBOXAPI
HRESULT
WINAPI
XOnlineStorageGetInstallLocation(
    IN DWORD dwFacility,
    IN LPCWSTR wszStoragePath,
    OUT PSTR szLocation,
    IN OUT DWORD *pcbLocation
    );


typedef struct
{
    DWORD   dwContext;
    DWORD   dwReserved[6];
    BYTE    Data[MAX_LAUNCH_DATA_SIZE - 28];

} LD_UPDATE, *PLD_UPDATE;

XBOXAPI
HRESULT
WINAPI
XOnlineTitleUpdateEx(
    IN PLD_UPDATE pldUpdate
    );

//
// XOnline Offerings
//

#define XO_CURRENCY_EUR     1
#define XO_CURRENCY_GBP     2
#define XO_CURRENCY_JPY     4
#define XO_CURRENCY_KRW     8


typedef enum {
    NO_TAX = 0,
    DEFAULT,
    GST,
    VAT,
    TAX_NOT_APPLICABLE
} XONLINE_TAX_TYPE;



typedef struct _XONLINE_PRICE{
    DWORD               dwWholePart;
    DWORD               dwFractionalPart;
    WCHAR               rgwchISOCurrencyCode[3];
    BOOL                fOfferingIsFree;
    XONLINE_TAX_TYPE    Tax;
    BYTE                bCurrencyFormat;
} XONLINE_PRICE, *PXONLINE_PRICE;



typedef enum {
    ONE_TIME_CHARGE = 0,
    MONTHLY,
    QUARTERLY,
    BIANNUALLY,
    ANNUALLY
} XONLINE_OFFERING_FREQUENCY;


typedef struct _XONLINEOFFERING_DETAILS{
    PBYTE                       pbDetailsBuffer;            // Pointer to buffer of details blob
    DWORD                       dwDetailsBuffer;            // Length of details blob
    DWORD                       dwInstances;                // Count of currently-owned instances
    XONLINE_PRICE               Price;                      // price strcture
    DWORD                       dwFreeMonthsBeforeCharge;   // free months before charge begins
    DWORD                       dwDuration;                 // duration of the recurring charge (months)
    XONLINE_OFFERING_FREQUENCY  Frequency;                  // how often charges are made
} XONLINEOFFERING_DETAILS, *PXONLINEOFFERING_DETAILS;


XBOXAPI
HRESULT
WINAPI
XOnlineOfferingPurchase(
    IN DWORD dwUserIndex,
    IN XOFFERING_ID OfferingId,
    IN HANDLE hWorkEvent,
    OUT XONLINETASK_HANDLE *phTask
    );

XBOXAPI
HRESULT
WINAPI
XOnlineOfferingCancel(
    IN DWORD dwUserIndex,
    IN XOFFERING_ID OfferingId,
    IN HANDLE hWorkEvent,
    OUT XONLINETASK_HANDLE *phTask
    );

XBOXAPI
HRESULT
WINAPI
XOnlineOfferingDetails (
    IN DWORD dwUserIndex,
    IN XOFFERING_ID OfferingId,
    IN DWORD dwLanguage,
    IN DWORD dwDescriptionIndex,
    OUT OPTIONAL PBYTE pbBuffer,
    IN DWORD dwBuffer,
    IN HANDLE hWorkEvent,
    OUT XONLINETASK_HANDLE *phTask
    );

XBOXAPI
HRESULT
WINAPI
XOnlineOfferingDetailsGetResults (
    IN XONLINETASK_HANDLE hTask,
    OUT PXONLINEOFFERING_DETAILS pDetails
    );

XBOXAPI
DWORD
WINAPI
XOnlineOfferingDetailsMaxSize(
    IN DWORD dwTitleSpecificDataMaxSize
    );

XBOXAPI
HRESULT
WINAPI
XOnlineOfferingPriceFormat(
    IN OUT XONLINE_PRICE *pPrice,
    IN LPWSTR        lpFormattedPrice,
    IN OUT DWORD     *pdwLength,
    IN DWORD         dwExtendedCharsFilter
    );




//
// Offering enumeration APIs
//




#define XONLINE_COUNTRY_UNITED_ARAB_EMIRATES 1
#define XONLINE_COUNTRY_ALBANIA              2
#define XONLINE_COUNTRY_ARMENIA              3
#define XONLINE_COUNTRY_ARGENTINA            4
#define XONLINE_COUNTRY_AUSTRIA              5
#define XONLINE_COUNTRY_AUSTRALIA            6
#define XONLINE_COUNTRY_AZERBAIJAN           7
#define XONLINE_COUNTRY_BELGIUM              8
#define XONLINE_COUNTRY_BULGARIA             9
#define XONLINE_COUNTRY_BAHRAIN              10
#define XONLINE_COUNTRY_BRUNEI_DARUSSALAM    11
#define XONLINE_COUNTRY_BOLIVIA              12
#define XONLINE_COUNTRY_BRAZIL               13
#define XONLINE_COUNTRY_BELARUS              14
#define XONLINE_COUNTRY_BELIZE               15
#define XONLINE_COUNTRY_CANADA               16
#define XONLINE_COUNTRY_SWITZERLAND          18
#define XONLINE_COUNTRY_CHILE                19
#define XONLINE_COUNTRY_CHINA                20
#define XONLINE_COUNTRY_COLOMBIA             21
#define XONLINE_COUNTRY_COSTA_RICA           22
#define XONLINE_COUNTRY_CZECH_REPUBLIC       23
#define XONLINE_COUNTRY_GERMANY              24
#define XONLINE_COUNTRY_DENMARK              25
#define XONLINE_COUNTRY_DOMINICAN_REPUBLIC   26
#define XONLINE_COUNTRY_ALGERIA              27
#define XONLINE_COUNTRY_ECUADOR              28
#define XONLINE_COUNTRY_ESTONIA              29
#define XONLINE_COUNTRY_EGYPT                30
#define XONLINE_COUNTRY_SPAIN                31
#define XONLINE_COUNTRY_FINLAND              32
#define XONLINE_COUNTRY_FAROE_ISLANDS        33
#define XONLINE_COUNTRY_FRANCE               34
#define XONLINE_COUNTRY_GREAT_BRITAIN        35
#define XONLINE_COUNTRY_GEORGIA              36
#define XONLINE_COUNTRY_GREECE               37
#define XONLINE_COUNTRY_GUATEMALA            38
#define XONLINE_COUNTRY_HONG_KONG            39
#define XONLINE_COUNTRY_HONDURAS             40
#define XONLINE_COUNTRY_CROATIA              41
#define XONLINE_COUNTRY_HUNGARY              42
#define XONLINE_COUNTRY_INDONESIA            43
#define XONLINE_COUNTRY_IRELAND              44
#define XONLINE_COUNTRY_ISRAEL               45
#define XONLINE_COUNTRY_INDIA                46
#define XONLINE_COUNTRY_IRAQ                 47
#define XONLINE_COUNTRY_IRAN                 48
#define XONLINE_COUNTRY_ICELAND              49
#define XONLINE_COUNTRY_ITALY                50
#define XONLINE_COUNTRY_JAMAICA              51
#define XONLINE_COUNTRY_JORDAN               52
#define XONLINE_COUNTRY_JAPAN                53
#define XONLINE_COUNTRY_KENYA                54
#define XONLINE_COUNTRY_KYRGYZSTAN           55
#define XONLINE_COUNTRY_KOREA                56
#define XONLINE_COUNTRY_KUWAIT               57
#define XONLINE_COUNTRY_KAZAKHSTAN           58
#define XONLINE_COUNTRY_LEBANON              59
#define XONLINE_COUNTRY_LIECHTENSTEIN        60
#define XONLINE_COUNTRY_LITHUANIA            61
#define XONLINE_COUNTRY_LUXEMBOURG           62
#define XONLINE_COUNTRY_LATVIA               63
#define XONLINE_COUNTRY_LIBYA                64
#define XONLINE_COUNTRY_MOROCCO              65
#define XONLINE_COUNTRY_MONACO               66
#define XONLINE_COUNTRY_MACEDONIA            67
#define XONLINE_COUNTRY_MONGOLIA             68
#define XONLINE_COUNTRY_MACAU                69
#define XONLINE_COUNTRY_MALDIVES             70
#define XONLINE_COUNTRY_MEXICO               71
#define XONLINE_COUNTRY_MALAYSIA             72
#define XONLINE_COUNTRY_NICARAGUA            73
#define XONLINE_COUNTRY_NETHERLANDS          74
#define XONLINE_COUNTRY_NORWAY               75
#define XONLINE_COUNTRY_NEW_ZEALAND          76
#define XONLINE_COUNTRY_OMAN                 77
#define XONLINE_COUNTRY_PANAMA               78
#define XONLINE_COUNTRY_PERU                 79
#define XONLINE_COUNTRY_PHILIPPINES          80
#define XONLINE_COUNTRY_PAKISTAN             81
#define XONLINE_COUNTRY_POLAND               82
#define XONLINE_COUNTRY_PUERTO_RICO          83
#define XONLINE_COUNTRY_PORTUGAL             84
#define XONLINE_COUNTRY_PARAGUAY             85
#define XONLINE_COUNTRY_QATAR                86
#define XONLINE_COUNTRY_ROMANIA              87
#define XONLINE_COUNTRY_RUSSIAN_FEDERATION   88
#define XONLINE_COUNTRY_SAUDI_ARABIA         89
#define XONLINE_COUNTRY_SWEDEN               90
#define XONLINE_COUNTRY_SINGAPORE            91
#define XONLINE_COUNTRY_SLOVENIA             92
#define XONLINE_COUNTRY_SLOVAK_REPUBLIC      93
#define XONLINE_COUNTRY_EL_SALVADOR          95
#define XONLINE_COUNTRY_SYRIA                96
#define XONLINE_COUNTRY_THAILAND             97
#define XONLINE_COUNTRY_TUNISIA              98
#define XONLINE_COUNTRY_TURKEY               99
#define XONLINE_COUNTRY_TRINIDAD_AND_TOBAGO  100
#define XONLINE_COUNTRY_TAIWAN               101
#define XONLINE_COUNTRY_UKRAINE              102
#define XONLINE_COUNTRY_UNITED_STATES        103
#define XONLINE_COUNTRY_URUGUAY              104
#define XONLINE_COUNTRY_UZBEKISTAN           105
#define XONLINE_COUNTRY_VENEZUELA            106
#define XONLINE_COUNTRY_VIET_NAM             107
#define XONLINE_COUNTRY_YEMEN                108
#define XONLINE_COUNTRY_SOUTH_AFRICA         109
#define XONLINE_COUNTRY_ZIMBABWE             110





#define    XONLINE_OFFERING_SUBSCRIPTION        0x1
#define    XONLINE_OFFERING_CONTENT             0x2




#define XONLINE_OFFERING_BITFILTER_ALL          0xffffffff
#define XONLINE_OFFERING_TYPE_ALL               0xffffffff



#define    XONLINE_OFFERING_IS_NOT_FREE         0x1


#define XOnlineOfferingIsFree(x)    (((x) & XONLINE_OFFERING_IS_NOT_FREE) == 0)


#pragma pack(push, 1)
typedef struct _XONLINEOFFERING_ENUM_PARAMS
{
    DWORD       dwOfferingType;    // Filter on offering type
    DWORD       dwBitFilter;       // Bitfield for filtering offerings
    DWORD       dwDescriptionIndex;// Publisher-specific index
    WORD        wStartingIndex;    // Starting index to enumerate
    WORD        wMaxResults;       // Desired max number of results
} XONLINEOFFERING_ENUM_PARAMS, *PXONLINEOFFERING_ENUM_PARAMS;
#pragma pack(pop)


#pragma pack(push, 1)
typedef struct _XONLINEOFFERING_INFO{
    XOFFERING_ID        OfferingId;             // Offering ID
    DWORD               dwOfferingType;         // Offering type
    DWORD               dwBitFlags;             // Package-specific flags
    DWORD               dwPackageSize;          // Package wire size (bytes)
    DWORD               dwInstallSize;          // Installed size (blocks)
    FILETIME            ftActivationDate;       // Activation date of package
    DWORD               dwRating;               // Package rating
    WORD                fOfferingFlags;         // Per-offering flags
    DWORD               dwTitleSpecificData;    // Size of data blob (bytes)
    PBYTE               pbTitleSpecificData;    // Pointer to data blob
} XONLINEOFFERING_INFO, *PXONLINEOFFERING_INFO;
#pragma pack(pop)

#define XONLINEOFFERING_ENUM_MAX_TITLE_DATA_SIZE    (8000)



XBOXAPI
HRESULT
WINAPI
XOnlineOfferingIsNewContentAvailable(
    IN DWORD dwBitFilter,
    IN OPTIONAL HANDLE hWorkEvent,
    OUT XONLINETASK_HANDLE *phTask
    );



XBOXAPI
HRESULT
WINAPI
XOnlineOfferingEnumerate(
    IN DWORD dwUserIndex,
    IN const XONLINEOFFERING_ENUM_PARAMS* pEnumParams,
    OUT OPTIONAL PBYTE pbBuffer,
    IN DWORD dwBuffer,
    IN OPTIONAL HANDLE hWorkEvent,
    OUT XONLINETASK_HANDLE *phTask
    );


XBOXAPI
HRESULT
WINAPI
XOnlineOfferingEnumerateGetResults(
    IN XONLINETASK_HANDLE hTask,
    OUT PXONLINEOFFERING_INFO **prgpOfferingInfo,
    OUT DWORD *pdwReturnedResults,
    OUT BOOL *pfMoreResults
    );


XBOXAPI
DWORD
WINAPI
XOnlineOfferingEnumerateMaxSize(
    IN const XONLINEOFFERING_ENUM_PARAMS* pEnumParams,
    IN OPTIONAL DWORD dwTitleSpecificDataMaxSize
    );


XBOXAPI
HRESULT
WINAPI
XOnlineContentInstall(
    IN XOFFERING_ID OfferingId,
    IN OPTIONAL HANDLE hWorkEvent,
    OUT XONLINETASK_HANDLE *phTask
    );


XBOXAPI
HRESULT
WINAPI
XOnlineContentInstallGetProgress(
    IN XONLINETASK_HANDLE hTask,
    OUT OPTIONAL DWORD *pdwPercentDone,
    OUT OPTIONAL ULONGLONG *pqwNumerator,
    OUT OPTIONAL ULONGLONG *pqwDenominator
    );

XBOXAPI
HRESULT
WINAPI
XOnlineContentSetSecurityKey(
    IN const BYTE* pbSecretKey
    );




//
// XOnline Matchmaking
//

typedef struct _XONLINE_ATTRIBUTE {
    DWORD dwAttributeID;
    BOOL fChanged;
    union {
        struct {
            ULONGLONG      qwValue;
        } integer;
        struct {
            LPWSTR         lpValue;
        } string;
        struct {
            PVOID          pvValue;
            DWORD          dwLength;
        } blob;
    } info;
} XONLINE_ATTRIBUTE, *PXONLINE_ATTRIBUTE;

typedef struct _XONLINE_ATTRIBUTE_SPEC {
    DWORD            dwType;
    DWORD            dwLength;
} XONLINE_ATTRIBUTE_SPEC, *PXONLINE_ATTRIBUTE_SPEC;

#define X_MAX_STRING_ATTRIBUTE_LEN         400
#define X_MAX_BLOB_ATTRIBUTE_LEN           800

#define X_ATTRIBUTE_SCOPE_TITLE_SPECIFIC   0x00000000

#define X_ATTRIBUTE_DATATYPE_MASK          0x00F00000
#define X_ATTRIBUTE_DATATYPE_INTEGER       0x00000000
#define X_ATTRIBUTE_DATATYPE_STRING        0x00100000
#define X_ATTRIBUTE_DATATYPE_BLOB          0x00200000
#define X_ATTRIBUTE_DATATYPE_NULL          0x00F00000

#define X_ATTRIBUTE_ID_MASK                0x0000FFFF

#pragma pack(push, 1)

typedef struct _XONLINE_MATCH_SEARCHRESULT
{
    DWORD     dwReserved;
    XNKID     SessionID;
    XNADDR    HostAddress;
    XNKEY     KeyExchangeKey;
    DWORD     dwPublicOpen;
    DWORD     dwPrivateOpen;
    DWORD     dwPublicFilled;
    DWORD     dwPrivateFilled;
    DWORD     dwNumAttributes;
} XONLINE_MATCH_SEARCHRESULT, *PXONLINE_MATCH_SEARCHRESULT;

#pragma pack(pop)

XBOXAPI
HRESULT
WINAPI
XOnlineMatchSessionCreate(
    IN DWORD dwPublicFilled,
    IN DWORD dwPublicOpen,
    IN DWORD dwPrivateFilled,
    IN DWORD dwPrivateOpen,
    IN DWORD dwNumAttributes,
    IN PXONLINE_ATTRIBUTE pAttributes,
    IN HANDLE hWorkEvent,
    OUT PXONLINETASK_HANDLE phTask
    );

XBOXAPI
HRESULT
WINAPI
XOnlineMatchSessionUpdate(
    IN XNKID SessionID,
    IN DWORD dwPublicFilled,
    IN DWORD dwPublicOpen,
    IN DWORD dwPrivateFilled,
    IN DWORD dwPrivateOpen,
    IN DWORD dwNumAttributes,
    IN PXONLINE_ATTRIBUTE pAttributes,
    IN HANDLE hWorkEvent,
    OUT PXONLINETASK_HANDLE phTask
    );

XBOXAPI
HRESULT
WINAPI
XOnlineMatchSessionGetInfo(
    IN XONLINETASK_HANDLE hTask,
    OUT XNKID *pSessionID,
    OUT XNKEY *pKeyExchangeKey
    );

XBOXAPI
HRESULT
WINAPI
XOnlineMatchSessionDelete(
    IN XNKID SessionID,
    IN HANDLE hWorkEvent,
    OUT PXONLINETASK_HANDLE phTask
    );

XBOXAPI
HRESULT
WINAPI
XOnlineMatchSessionFindFromID(
    IN XNKID SessionID,
    IN HANDLE hWorkEvent,
    OUT PXONLINETASK_HANDLE phTask
    );

XBOXAPI
HRESULT
WINAPI
XOnlineMatchSearch(
    IN DWORD dwProcedureIndex,
    IN DWORD dwNumResults,
    IN DWORD dwNumAttributes,
    IN const XONLINE_ATTRIBUTE* pAttributes,
    IN DWORD dwResultsLen,
    IN HANDLE hWorkEvent,
    OUT PXONLINETASK_HANDLE phTask
    );

XBOXAPI
HRESULT
WINAPI
XOnlineMatchSearchGetResults(
    IN XONLINETASK_HANDLE hTask,
    OUT PXONLINE_MATCH_SEARCHRESULT **prgpSearchResults,
    OUT DWORD *pdwReturnedResults
    );

XBOXAPI
HRESULT
WINAPI
XOnlineMatchSearchParse(
    IN const XONLINE_MATCH_SEARCHRESULT* pSearchResult,
    IN DWORD dwNumSessionAttributes,
    IN const XONLINE_ATTRIBUTE_SPEC* pSessionAttributeSpec,
    OUT PVOID pQuerySession
    );

XBOXAPI
DWORD
WINAPI
XOnlineMatchSearchResultsLen(
    IN DWORD dwNumResults,
    IN DWORD dwNumSessionAttributes,
    IN const XONLINE_ATTRIBUTE_SPEC* pSessionAttributeSpec
    );

//
// Title Name
//

#define MAX_TITLENAME_LEN 40
#define MAX_TITLENAME_SIZE (MAX_TITLENAME_LEN * sizeof(WCHAR))


BOOL
WINAPI
XOnlineTitleIdIsSameTitle(
    IN DWORD dwTitleID
    );

BOOL
WINAPI
XOnlineTitleIdIsSamePublisher(
    IN DWORD dwTitleID
    );

//
// Presence & Notification
//

#define XONLINE_FRIENDSTATE_FLAG_NONE              0x00000000
#define XONLINE_FRIENDSTATE_FLAG_ONLINE            0x00000001
#define XONLINE_FRIENDSTATE_FLAG_PLAYING           0x00000002
#define XONLINE_FRIENDSTATE_FLAG_VOICE             0x00000008
#define XONLINE_FRIENDSTATE_FLAG_JOINABLE          0x00000010
#define XONLINE_FRIENDSTATE_MASK_GUESTS            0x00000060
#define XONLINE_FRIENDSTATE_FLAG_RESERVED0         0x00000080
#define XONLINE_FRIENDSTATE_FLAG_SENTINVITE        0x04000000
#define XONLINE_FRIENDSTATE_FLAG_RECEIVEDINVITE    0x08000000
#define XONLINE_FRIENDSTATE_FLAG_INVITEACCEPTED    0x10000000
#define XONLINE_FRIENDSTATE_FLAG_INVITEREJECTED    0x20000000
#define XONLINE_FRIENDSTATE_FLAG_SENTREQUEST       0x40000000
#define XONLINE_FRIENDSTATE_FLAG_RECEIVEDREQUEST   0x80000000

#define XOnlineGetGuests(dwState) ((dwState & XONLINE_FRIENDSTATE_MASK_GUESTS) >> 5)

typedef enum {
    XONLINE_NOTIFICATION_FRIEND_REQUEST     = 0,
    XONLINE_NOTIFICATION_GAME_INVITE        = 1,
    XONLINE_NOTIFICATION_NEW_GAME_INVITE    = 2,
    XONLINE_NOTIFICATION_GAME_INVITE_ANSWER = 3,
    XONLINE_NOTIFICATION_NUM                = 4
} XONLINE_NOTIFICATION_TYPE;

XBOXAPI
HRESULT
WINAPI
XOnlineNotificationSetState(
    IN DWORD dwUserIndex,
    IN DWORD dwStateFlags,
    IN XNKID sessionID,
    IN DWORD dwStateData,
    IN const BYTE* pbStateData
    );

XBOXAPI
BOOL
WINAPI
XOnlineGetNotification(
    IN DWORD dwUserIndex,
    IN XONLINE_NOTIFICATION_TYPE NotificationType
    );


//
// Peer Session
//

XBOXAPI
HRESULT
WINAPI
XOnlineGetSession(
    XNADDR *pxnaddr,
    XNKID *pxnkid,
    XNKEY *pxnkey
    );

typedef struct _PXONLINE_PEER_SESSION_RESULTS {
    XUID xuid;
    DWORD dwTitleID;
    DWORD dwTitleVersion;
    DWORD dwTitleRegion;
    XNADDR xnaddr;
    XNKID xkid;
    XNKEY xnkey;
} XONLINE_PEER_SESSION_RESULTS, *PXONLINE_PEER_SESSION_RESULTS;

XBOXAPI
HRESULT
WINAPI
XOnlineGetUserSession(
    DWORD dwUserIndex,
    XUID xuidPeer,
    IN HANDLE hEvent,
    PXONLINETASK_HANDLE phTask,
    PXONLINE_PEER_SESSION_RESULTS pResults
    );

//
// Friends
//

typedef enum {
    XONLINE_REQUEST_NO,
    XONLINE_REQUEST_YES,
    XONLINE_REQUEST_BLOCK
} XONLINE_REQUEST_ANSWER_TYPE;

typedef enum {
    XONLINE_GAMEINVITE_NO,
    XONLINE_GAMEINVITE_YES,
    XONLINE_GAMEINVITE_REMOVE
} XONLINE_GAMEINVITE_ANSWER_TYPE;

#define MAX_FRIENDS         100
#define MAX_STATEDATA_SIZE  32
#define MAX_USERDATA_SIZE   0

#pragma pack(push, 1)
typedef struct _XONLINE_FRIEND {
    XUID                    xuid;
    CHAR                    szGamertag[XONLINE_GAMERTAG_SIZE];
    DWORD                   dwFriendState;
    FILETIME                gameinviteTime;
    XNKID                   sessionID;
    DWORD                   dwTitleID;
    BYTE                    StateDataSize;
    BYTE                    StateData[MAX_STATEDATA_SIZE];
    BYTE                    bReserved;
} XONLINE_FRIEND, *PXONLINE_FRIEND;
#pragma pack(pop)

typedef struct _XONLINE_ACCEPTED_GAMEINVITE{
    XONLINE_FRIEND InvitingFriend;
    XUID           xuidAcceptedFriend;
    FILETIME       InviteAcceptTime;
    XUID           xuidLogonUsers[XONLINE_MAX_LOGON_USERS];
} XONLINE_ACCEPTED_GAMEINVITE, *PXONLINE_ACCEPTED_GAMEINVITE;

XBOXAPI
HRESULT
WINAPI
XOnlineFriendsStartup(
    IN HANDLE hEvent,
    OUT PXONLINETASK_HANDLE phTask
    );

XBOXAPI
HRESULT
WINAPI
XOnlineFriendsEnumerate(
    IN DWORD dwUserIndex,
    IN HANDLE hEvent,
    OUT PXONLINETASK_HANDLE phTask
    );

XBOXAPI
HRESULT
WINAPI
XOnlineFriendsEnumerateFinish(
    IN XONLINETASK_HANDLE hTask
    );

XBOXAPI
DWORD
WINAPI
XOnlineFriendsGetLatest(
    IN DWORD dwUserIndex,
    IN DWORD dwFriendBufferCount,
    OUT PXONLINE_FRIEND pFriendBuffer
    );

XBOXAPI
HRESULT
WINAPI
XOnlineFriendsGetLatestByRange(
    IN DWORD dwUserIndex,
    IN DWORD dwRangeStart,
    IN OUT DWORD *pdwFriendBuffer,
    OUT PXONLINE_FRIEND pFriendBuffer,
    OUT DWORD *pdwFriendsBefore,
    OUT DWORD *pdwFriendsAfter
    );

XBOXAPI
HRESULT
WINAPI
XOnlineFriendsGetLatestByFocus(
    IN DWORD dwUserIndex,
    IN XUID xuidFriendFocus,
    IN DWORD dwBeforeFocus,
    IN OUT DWORD *pdwFriendBuffer,
    OUT PXONLINE_FRIEND pFriendBuffer,
    OUT DWORD *pdwFriendsBefore,
    OUT DWORD *pdwFriendsAfter
    );

XBOXAPI
HRESULT
WINAPI
XOnlineFriendsGetTitleName(
    IN DWORD dwTitleId,
    IN DWORD dwLanguage,
    IN DWORD cTitleName,
    OUT LPWSTR lpTitleName
    );

XBOXAPI
HRESULT
WINAPI
XOnlineFriendsRemove(
    IN DWORD dwUserIndex,
    IN const XONLINE_FRIEND* pFriend
    );

XBOXAPI
HRESULT
WINAPI
XOnlineFriendsRequest(
    IN DWORD dwUserIndex,
    IN XUID xuidToUser
    );

XBOXAPI
HRESULT
WINAPI
XOnlineFriendsRequestByName(
    IN DWORD dwUserIndex,
    IN PSTR lpUserName,
    IN HANDLE hEvent,
    OUT PXONLINETASK_HANDLE phTask
    );

XBOXAPI
HRESULT
WINAPI
XOnlineFriendsGameInvite(
    IN DWORD dwUserIndex,
    IN XNKID SessionID,
    IN DWORD dwFriendListCount,
    IN const XONLINE_FRIEND* pToFriendList
    );

XBOXAPI
HRESULT
WINAPI
XOnlineFriendsRevokeGameInvite(
    IN DWORD dwUserIndex,
    IN XNKID SessionID,
    IN DWORD dwFriendListCount,
    IN const XONLINE_FRIEND* pToFriendList
    );

XBOXAPI
HRESULT
WINAPI
XOnlineFriendsAnswerRequest(
    IN DWORD dwUserIndex,
    IN const XONLINE_FRIEND* pToFriend,
    IN XONLINE_REQUEST_ANSWER_TYPE Answer
    );

XBOXAPI
HRESULT
WINAPI
XOnlineFriendsAnswerGameInvite(
    IN DWORD dwUserIndex,
    IN const XONLINE_FRIEND* pToFriend,
    IN XONLINE_GAMEINVITE_ANSWER_TYPE Answer
    );

XBOXAPI
HRESULT
WINAPI
XOnlineFriendsJoinGame(
    IN DWORD dwUserIndex,
    IN const XONLINE_FRIEND* pToFriend
    );

XBOXAPI
HRESULT
WINAPI
XOnlineFriendsGetAcceptedGameInvite(
    OUT PXONLINE_ACCEPTED_GAMEINVITE pAcceptedGameInvite
    );


//
// Mute List
//

#define MAX_MUTELISTUSERS      250


typedef struct _XONLINE_MUTELISTUSER {
    XUID                    xuid;
    DWORD                   dwReserved;
} XONLINE_MUTELISTUSER, *PXONLINE_MUTELISTUSER;

XBOXAPI
HRESULT
WINAPI
XOnlineMutelistGet(
    IN DWORD dwUserIndex,
    IN DWORD dwMutelistUserBufferCount,
    IN HANDLE hEvent,
    OUT PXONLINETASK_HANDLE phTask,
    OUT PXONLINE_MUTELISTUSER pMutelistUsersBuffer,
    OUT DWORD *pdwNumMustlistUsers
    );

XBOXAPI
HRESULT
WINAPI
XOnlineMutelistStartup(
    IN HANDLE hEvent,
    OUT PXONLINETASK_HANDLE phTask
    );

XBOXAPI
HRESULT
WINAPI
XOnlineMutelistAdd(
    IN DWORD dwUserIndex,
    IN XUID xUserID
    );

XBOXAPI
HRESULT
WINAPI
XOnlineMutelistRemove(
    IN DWORD dwUserIndex,
    IN XUID xUserID
    );

//
// XOnline Nicknames
//

#define XONLINE_MAX_NICKNAME_SIZE 63

XBOXAPI
HRESULT
WINAPI
XOnlineVerifyNickname(
    IN LPCWSTR lpNickname,
    IN HANDLE hEvent,
    OUT PXONLINETASK_HANDLE phTask
    );

//
// XOnline Feedback
//

typedef enum {
    XONLINE_FEEDBACK_NEG_NICKNAME,
    XONLINE_FEEDBACK_NEG_GAMEPLAY,
    XONLINE_FEEDBACK_NEG_SCREAMING,
    XONLINE_FEEDBACK_NEG_HARASSMENT,
    XONLINE_FEEDBACK_NEG_LEWDNESS,
    XONLINE_FEEDBACK_POS_ATTITUDE,
    XONLINE_FEEDBACK_POS_SESSION,
    XONLINE_FEEDBACK_POS_STATS_ATTACHMENT,
    XONLINE_FEEDBACK_NEG_STATS_ATTACHMENT,
    XONLINE_FEEDBACK_NEG_STATS_ATTACHMENT_CHEATING,

    // Add new public types here

    NUM_XONLINE_FEEDBACK_TYPES,


} XONLINE_FEEDBACK_TYPE;

typedef struct
{
    LPCWSTR lpStringParam;
} XONLINE_FEEDBACK_PARAMS, *PXONLINE_FEEDBACK_PARAMS;

XBOXAPI
HRESULT
WINAPI
XOnlineFeedbackSend(
    IN DWORD dwUserIndex,
    IN XUID xTargetUser,
    IN XONLINE_FEEDBACK_TYPE FeedbackType,
    IN PXONLINE_FEEDBACK_PARAMS pParams,
    IN HANDLE hWorkEvent,
    OUT PXONLINETASK_HANDLE phTask
    );


//
// Statistics
//

#define XONLINE_STAT_MAX_SPECS_IN_WRITE_REQUEST 20
#define XONLINE_STAT_MAX_STATS_IN_SPEC 64
#define XONLINE_STAT_MAX_NICKNAME_LENGTH 32

#define XONLINE_STAT_RANK               ((WORD)0xFFFF)
#define XONLINE_STAT_RATING             ((WORD)0xFFFE)
#define XONLINE_STAT_NICKNAME           ((WORD)0xFFFD)
#define XONLINE_STAT_LEADERBOARD_SIZE   ((WORD)0xFFFC)
#define XONLINE_STAT_ATTACHMENT_PATH    ((WORD)0xFFFB)
#define XONLINE_STAT_ATTACHMENT_SIZE    ((WORD)0xFFFA)

typedef enum _XONLINE_STAT_TYPE {
    XONLINE_STAT_NONE,
    XONLINE_STAT_LONG,
    XONLINE_STAT_LONGLONG,
    XONLINE_STAT_DOUBLE,
    XONLINE_STAT_LPCWSTR
} XONLINE_STAT_TYPE;

typedef struct _XONLINE_STAT {
    WORD wID;
    XONLINE_STAT_TYPE type;
    union {
        LONG lValue;
        LONGLONG llValue;
        double dValue;
        LPCWSTR lpString;
    };
} XONLINE_STAT, *PXONLINE_STAT;

typedef struct _XONLINE_STAT_SPEC {
    XUID xuidUser;
    DWORD dwLeaderBoardID;
    DWORD dwNumStats;
    PXONLINE_STAT pStats;
} XONLINE_STAT_SPEC, *PXONLINE_STAT_SPEC;

typedef struct _XONLINE_STAT_USER {
    XUID xuidUser;
    CHAR szGamertag[XONLINE_GAMERTAG_SIZE];
} XONLINE_STAT_USER, *PXONLINE_STAT_USER;

#pragma pack(push, 1)

typedef struct {
    DWORD dwLeaderboardIndex;
    ULONGLONG qwUserPuid;
} XONLINE_STAT_ATTACHMENT_REFERENCE, *PXONLINE_STAT_ATTACHMENT_REFERENCE;

#pragma pack(pop)

XBOXAPI
HRESULT
WINAPI
XOnlineStatWrite(
    IN DWORD dwNumStatSpecs,
    IN const XONLINE_STAT_SPEC* pStatSpecs,
    IN HANDLE hWorkEvent,
    OUT PXONLINETASK_HANDLE phTask
);

XBOXAPI
HRESULT
WINAPI
XOnlineStatWriteGetResult(
    IN XONLINETASK_HANDLE hTask,
    OUT HANDLE *phServerFileReference,
    OUT PXONLINE_STAT_ATTACHMENT_REFERENCE *prgReferences,
    OUT DWORD *pdwReferences
);

XBOXAPI
HRESULT
WINAPI
XOnlineStatRead(
    IN DWORD dwNumStatSpecs,
    IN const XONLINE_STAT_SPEC* pStatSpecs,
    IN HANDLE hWorkEvent,
    OUT PXONLINETASK_HANDLE phTask
);

XBOXAPI
HRESULT
WINAPI
XOnlineStatReadGetResult(
    IN XONLINETASK_HANDLE hTask,
    IN DWORD dwNumStatSpecs,
    OUT PXONLINE_STAT_SPEC pStatSpecs,
    IN DWORD dwExtraBufferSize,
    IN OUT BYTE* pExtraBuffer);

XBOXAPI
HRESULT
WINAPI
XOnlineStatLeaderEnumerate(
    IN const XUID* pxuidPagePivot,
    IN DWORD dwPageStart,
    IN DWORD dwPageSize,
    IN DWORD dwLeaderboardID,
    IN DWORD dwNumStatsPerUser,
    IN const WORD* pStatsPerUser,
    IN HANDLE hWorkEvent,
    OUT PXONLINETASK_HANDLE phTask
);

XBOXAPI
HRESULT
WINAPI
XOnlineStatLeaderEnumerateGetResults(
    IN XONLINETASK_HANDLE hTask,
    IN DWORD dwUserCount,
    OUT PXONLINE_STAT_USER pUsers,
    IN DWORD dwStatCount,
    OUT PXONLINE_STAT pStats,
    OUT DWORD *pdwLeaderboardSize,
    OUT DWORD *pdwReturnedResults,
    IN DWORD dwExtraBufferSize,
    IN OUT BYTE* pExtraBuffer
);

XBOXAPI
HRESULT
WINAPI
XOnlineStatReset(
    XUID xuid,
    DWORD dwLeaderBoardId,
    IN HANDLE hWorkEvent,
    OUT PXONLINETASK_HANDLE phTask
);







#ifdef __cplusplus
}
#endif

#endif
