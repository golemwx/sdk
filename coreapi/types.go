package coreapi

import (
	"encoding/json"
	"time"

	"github.com/golemwx/sdk"
)

// ResourceRef 是宿主保存 core 私有资源元数据（如 CDN 文件 ID/AES key）后签发的引用。
type ResourceRef struct {
	Token     string    `json:"token"`
	Kind      string    `json:"kind"`
	ExpiresAt time.Time `json:"expires_at"`
}

// LoginSession 是一次由宿主管理的扫码登录会话。
type LoginSession struct {
	Token     string    `json:"token"`
	QRCodeURL string    `json:"qrcode_url"`
	ExpiresAt time.Time `json:"expires_at"`
}

// LoginState 是扫码登录会话的当前状态。
type LoginState struct {
	SessionToken               string    `json:"session_token"`
	QRCodeURL                  string    `json:"qrcode_url,omitempty"`
	Status                     string    `json:"status"`
	RequiresManualVerification bool      `json:"requires_manual_verification,omitempty"`
	LoggedIn                   bool      `json:"logged_in"`
	UpdatedAt                  time.Time `json:"updated_at"`
}

// LoginAccountUser 是 core 当前账号的公开资料投影。
type LoginAccountUser struct {
	Username string `json:"username,omitempty"`
	Nickname string `json:"nickname,omitempty"`
	Alias    string `json:"alias,omitempty"`
	Avatar   string `json:"avatar,omitempty"`
	Email    string `json:"email,omitempty"`
	Mobile   string `json:"mobile,omitempty"`
}

// LoginAccountState 是 core GET /api/login/status 的账号状态结果。
type LoginAccountState struct {
	User    LoginAccountUser `json:"user"`
	Status  string           `json:"status"`
	Login   string           `json:"login"`
	Refresh string           `json:"refresh"`
}

// LoginCancelRequest 取消一个宿主登录会话。
type LoginCancelRequest struct {
	SessionToken string `json:"session_token"`
}

// ContactListRequest 预留空请求类型，便于未来增加过滤字段。
type ContactListRequest struct{}

// ContactDetailRequest 批量读取联系人资料。
type ContactDetailRequest struct {
	Usernames []string `json:"usernames"`
}

// ContactSearchRequest 按微信号或手机号搜索联系人。
type ContactSearchRequest struct {
	Keyword     string `json:"keyword"`
	FromScene   uint32 `json:"from_scene,omitempty"`
	SearchScene uint32 `json:"search_scene,omitempty"`
}

// ContactFriendRequest 发送好友申请。
type ContactFriendRequest struct {
	V1      string `json:"v1"`
	V2      string `json:"v2"`
	Content string `json:"content,omitempty"`
	Scene   uint32 `json:"scene"`
}

// ContactVerifyRequest 通过好友申请。
type ContactVerifyRequest struct {
	V1    string `json:"v1"`
	V2    string `json:"v2"`
	Scene uint32 `json:"scene"`
}

// ContactRemarkRequest 设置或清除联系人备注。
type ContactRemarkRequest struct {
	Username string  `json:"username"`
	Remark   *string `json:"remark"`
}

// UsernameRequest 只携带一个微信 username。
type UsernameRequest struct {
	Username string `json:"username"`
}

// ChatroomCreateRequest 创建群聊。
type ChatroomCreateRequest struct {
	Usernames []string `json:"usernames"`
}

// ChatroomRequest 只携带群 username。
type ChatroomRequest struct {
	Chatroom string `json:"chatroom"`
}

// ChatroomMembersRequest 批量群成员操作。
type ChatroomMembersRequest struct {
	Chatroom  string   `json:"chatroom"`
	Usernames []string `json:"usernames"`
}

// ChatroomTransferRequest 转让群主。
type ChatroomTransferRequest struct {
	Chatroom string `json:"chatroom"`
	Username string `json:"username"`
}

// ChatroomNameRequest 修改群名称。
type ChatroomNameRequest struct {
	Chatroom string `json:"chatroom"`
	Name     string `json:"name"`
}

// ChatroomRemarkRequest 设置或清除群备注。
type ChatroomRemarkRequest struct {
	Chatroom string  `json:"chatroom"`
	Remark   *string `json:"remark"`
}

// ChatroomAnnouncementRequest 修改群公告。
type ChatroomAnnouncementRequest struct {
	Chatroom    string  `json:"chatroom"`
	Content     *string `json:"content"`
	InfoVersion *uint32 `json:"info_version"`
}

// ChatroomDisplayNameRequest 修改本人群昵称。
type ChatroomDisplayNameRequest struct {
	Chatroom    string  `json:"chatroom"`
	DisplayName *string `json:"display_name"`
}

// ChatroomSavedRequest 设置群是否保存到通讯录。
type ChatroomSavedRequest struct {
	Chatroom string `json:"chatroom"`
	Enabled  bool   `json:"enabled"`
}

// MessageTextRequest 发送文本消息。
type MessageTextRequest struct {
	Receiver string   `json:"receiver"`
	Content  string   `json:"content"`
	Mentions []string `json:"mentions,omitempty"`
}

// MessageEmojiRequest 发送表情；Ref 与 MD5 至少提供一项。
type MessageEmojiRequest struct {
	Receiver string        `json:"receiver"`
	Ref      *sdk.MediaRef `json:"ref,omitempty"`
	MD5      string        `json:"md5,omitempty"`
	URL      string        `json:"url,omitempty"`
}

// MessageImageRequest 发送图片。
type MessageImageRequest struct {
	Receiver string        `json:"receiver"`
	Ref      *sdk.MediaRef `json:"ref,omitempty"`
	URL      string        `json:"url,omitempty"`
}

// MessageVoiceRequest 发送语音。
type MessageVoiceRequest struct {
	Receiver   string        `json:"receiver"`
	Ref        *sdk.MediaRef `json:"ref,omitempty"`
	URL        string        `json:"url,omitempty"`
	DurationMS int32         `json:"duration_ms"`
	Format     int32         `json:"format"`
}

// MessageVideoRequest 发送视频，可选独立封面。
type MessageVideoRequest struct {
	Receiver string        `json:"receiver"`
	Ref      *sdk.MediaRef `json:"ref,omitempty"`
	ThumbRef *sdk.MediaRef `json:"thumb_ref,omitempty"`
	VideoURL string        `json:"video_url,omitempty"`
	ThumbURL string        `json:"thumb_url,omitempty"`
	Duration uint32        `json:"duration,omitempty"`
}

// MessageAppRequest 发送调用方提供的应用 XML。
type MessageAppRequest struct {
	Receiver string `json:"receiver"`
	Type     int32  `json:"type"`
	XML      string `json:"xml"`
}

// MessageRecordRequest 发送合并聊天记录。
type MessageRecordRequest struct {
	Receiver string          `json:"receiver"`
	Record   json.RawMessage `json:"record"`
}

// MessageNoteRequest 发送图文笔记。
type MessageNoteRequest struct {
	Receiver string          `json:"receiver"`
	Note     json.RawMessage `json:"note"`
}

// MessageFileRequest 发送文件。
type MessageFileRequest struct {
	Receiver string        `json:"receiver"`
	Ref      *sdk.MediaRef `json:"ref,omitempty"`
	URL      string        `json:"url,omitempty"`
	Name     string        `json:"name,omitempty"`
}

// MessageLinkRequest 发送链接卡片。
type MessageLinkRequest struct {
	Receiver string `json:"receiver"`
	Title    string `json:"title"`
	Desc     string `json:"desc"`
	URL      string `json:"url"`
	ThumbURL string `json:"thumb_url,omitempty"`
}

// MessageMusicRequest 发送音乐卡片。
type MessageMusicRequest struct {
	Receiver string          `json:"receiver"`
	Music    json.RawMessage `json:"music"`
}

// MessagePositionRequest 发送位置。
type MessagePositionRequest struct {
	Receiver string  `json:"receiver"`
	Lon      float64 `json:"lon"`
	Lat      float64 `json:"lat"`
	Scale    float64 `json:"scale"`
	Name     string  `json:"name"`
	Label    string  `json:"label"`
}

// MessageNameCardRequest 发送联系人名片。
type MessageNameCardRequest struct {
	Receiver string `json:"receiver"`
	Username string `json:"username"`
	Nickname string `json:"nickname"`
	Alias    string `json:"alias"`
}

// MessageRevokeRequest 使用 core 的直接撤回字段。
type MessageRevokeRequest struct {
	Receiver   string `json:"receiver"`
	NewID      uint64 `json:"new_id"`
	ClientID   uint64 `json:"client_id,omitempty"`
	CreateTime uint64 `json:"create_time,omitempty"`
}

// MessageVoiceDownloadRequest 下载一条语音消息。
type MessageVoiceDownloadRequest struct {
	ID    uint64 `json:"id,omitempty"`
	NewID uint64 `json:"new_id,omitempty"`
	Size  uint32 `json:"size"`
	Name  string `json:"name,omitempty"`
}

// MessageTranscribeRequest 请求语音转文字。
type MessageTranscribeRequest struct {
	ID     uint32 `json:"id,omitempty"`
	NewID  uint64 `json:"new_id,omitempty"`
	Size   uint32 `json:"size"`
	Format int32  `json:"format,omitempty"`
}

// MessageForwardRequest 使用宿主签发的入站消息引用转发。
type MessageForwardRequest struct {
	Receiver string         `json:"receiver"`
	Ref      sdk.ForwardRef `json:"ref"`
}

// MessageCDNForwardRequest 使用宿主签发的 CDN 资源引用转发。
type MessageCDNForwardRequest struct {
	Receiver string      `json:"receiver"`
	Ref      ResourceRef `json:"ref"`
}

// FavoriteIDsRequest 批量收藏 ID。
type FavoriteIDsRequest struct {
	IDs []uint32 `json:"ids"`
}

// FavoriteSyncRequest 使用原始同步游标。
type FavoriteSyncRequest struct {
	Key []byte `json:"key,omitempty"`
}

// FavoriteShareRequest 分享收藏项。
type FavoriteShareRequest struct {
	Receiver string   `json:"receiver"`
	IDs      []uint32 `json:"ids"`
	Scene    uint32   `json:"scene,omitempty"`
}

// FavoriteCheckRequest 检查收藏附件 CDN 状态。
type FavoriteCheckRequest struct {
	Items json.RawMessage `json:"items"`
}

// CDNUploadImageRequest 上传聊天图片。
type CDNUploadImageRequest struct {
	Ref     *sdk.MediaRef `json:"ref,omitempty"`
	URL     string        `json:"url,omitempty"`
	Purpose string        `json:"purpose,omitempty"`
}

// CDNUploadVideoRequest 上传聊天视频。
type CDNUploadVideoRequest struct {
	Ref      *sdk.MediaRef `json:"ref,omitempty"`
	ThumbRef *sdk.MediaRef `json:"thumb_ref,omitempty"`
	VideoURL string        `json:"video_url,omitempty"`
	ThumbURL string        `json:"thumb_url,omitempty"`
	Purpose  string        `json:"purpose,omitempty"`
}

// CDNUploadFileRequest 上传文件。
type CDNUploadFileRequest struct {
	Ref  *sdk.MediaRef `json:"ref,omitempty"`
	URL  string        `json:"url,omitempty"`
	Name string        `json:"name,omitempty"`
}

// MomentsImageUpload 是朋友圈图片纯上传结果，只保留可直接用于发布接口的公开字段。
type MomentsImageUpload struct {
	FileURL     string `json:"file_url"`
	ThumbURL    string `json:"thumb_url,omitempty"`
	FileMD5     string `json:"file_md5,omitempty"`
	ImageWidth  uint32 `json:"image_width,omitempty"`
	ImageHeight uint32 `json:"image_height,omitempty"`
	FileSize    uint32 `json:"file_size,omitempty"`
}

// MomentsVideoUpload 是朋友圈视频纯上传结果，不包含 CDN 私有密钥或文件标识。
type MomentsVideoUpload struct {
	FileURL     string  `json:"file_url"`
	ThumbURL    string  `json:"thumb_url,omitempty"`
	FileMD5     string  `json:"file_md5,omitempty"`
	FileSize    uint32  `json:"file_size,omitempty"`
	VideoWidth  uint32  `json:"video_width,omitempty"`
	VideoHeight uint32  `json:"video_height,omitempty"`
	Duration    float64 `json:"duration,omitempty"`
}

// CDNDownloadRequest 使用宿主 CDN 资源引用下载。
type CDNDownloadRequest struct {
	Ref  ResourceRef `json:"ref"`
	Name string      `json:"name,omitempty"`
}

// LabelAddRequest 创建标签。
type LabelAddRequest struct {
	Name string `json:"name"`
}

// LabelDeleteRequest 删除标签。
type LabelDeleteRequest struct {
	IDs []uint32 `json:"ids"`
}

// LabelUpdateRequest 修改标签。
type LabelUpdateRequest struct {
	ID   uint32 `json:"id"`
	Name string `json:"name"`
}

// LabelModifyContactsRequest 覆盖联系人标签。
type LabelModifyContactsRequest struct {
	Usernames []string `json:"usernames"`
	IDs       []uint32 `json:"ids"`
}

// MiniappAppRequest 以 app_id 标识小程序。
type MiniappAppRequest struct {
	AppID string `json:"app_id"`
}

// MiniappQrcodeRequest 确认小程序二维码授权。
type MiniappQrcodeRequest struct {
	UUID string `json:"uuid"`
}

// MiniappJSONRequest 提交小程序原始 JSON。
type MiniappJSONRequest struct {
	AppID   string          `json:"app_id,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
	Operate int32           `json:"operate,omitempty"`
}

// MiniappVerifyCodeRequest 发送验证码。
type MiniappVerifyCodeRequest struct {
	AppID   string `json:"app_id"`
	Mobile  string `json:"mobile"`
	Operate int    `json:"operate"`
}

// MiniappCheckCodeRequest 校验验证码。
type MiniappCheckCodeRequest struct {
	AppID   string `json:"app_id"`
	Mobile  string `json:"mobile"`
	Code    string `json:"code"`
	Operate int    `json:"operate"`
}

// MiniappMobileRequest 绑定或解绑手机号。
type MiniappMobileRequest struct {
	AppID  string `json:"app_id"`
	Mobile string `json:"mobile"`
	Code   string `json:"code,omitempty"`
}

// MiniappAvatarRequest 设置小程序头像和昵称。
type MiniappAvatarRequest struct {
	AppID    string `json:"app_id"`
	Nickname string `json:"nickname"`
	FileKey  string `json:"file_key"`
}

// MiniappRandomAvatarResult 是小程序随机头像的公开结果。
type MiniappRandomAvatarResult struct {
	Nickname  string `json:"nickname,omitempty"`
	AvatarURL string `json:"avatar_url,omitempty"`
	FileKey   string `json:"file_key"`
}

// MiniappUploadAvatarRequest 上传小程序头像地址。
type MiniappUploadAvatarRequest struct {
	AppID    string `json:"app_id"`
	ImageURL string `json:"image_url"`
}

// MiniappAvatarUploadResult 是小程序头像图片上传后的公开结果。
type MiniappAvatarUploadResult struct {
	FileKey string `json:"file_key"`
	CDNURL  string `json:"cdn_url,omitempty"`
}

// MiniappRecordRequest 添加小程序使用记录。
type MiniappRecordRequest struct {
	Username string `json:"username"`
}

// MiniappOpenIDRequest 查询业务用户 OpenID。
type MiniappOpenIDRequest struct {
	AppID    string `json:"app_id"`
	Username string `json:"username"`
}

// MiniappRawRequest 保留 core 当前签发参数结构，但限制为有界 JSON。
type MiniappRawRequest struct {
	Data json.RawMessage `json:"data"`
}

// MomentsTimelineRequest 获取时间线。
type MomentsTimelineRequest struct {
	FirstPageMD5    string `json:"first_page_md5,omitempty"`
	MaxID           uint64 `json:"max_id,omitempty"`
	MinFilterID     uint64 `json:"min_filter_id,omitempty"`
	LastRequestTime uint32 `json:"last_request_time,omitempty"`
	ClientLatestID  uint64 `json:"client_latest_id,omitempty"`
	Session         []byte `json:"session,omitempty"`
	UpdateScene     uint32 `json:"update_scene,omitempty"`
	PullType        uint32 `json:"pull_type,omitempty"`
	MinIDForPrePage uint64 `json:"min_id_for_pre_page,omitempty"`
	MinIDForUnread  uint64 `json:"min_id_for_unread,omitempty"`
}

// MomentsUserPageRequest 获取用户朋友圈。
type MomentsUserPageRequest struct {
	Username        string `json:"username,omitempty"`
	FirstPageMD5    string `json:"first_page_md5,omitempty"`
	MaxID           uint64 `json:"max_id,omitempty"`
	Source          uint32 `json:"source,omitempty"`
	MinFilterID     uint64 `json:"min_filter_id,omitempty"`
	LastRequestTime uint32 `json:"last_request_time,omitempty"`
	FilterType      uint32 `json:"filter_type,omitempty"`
}

// MomentsIDRequest 指定朋友圈 ID。
type MomentsIDRequest struct {
	ID uint64 `json:"id"`
}

// MomentsCommentRequest 点赞或评论朋友圈。
type MomentsCommentRequest struct {
	ID               uint64 `json:"id"`
	Username         string `json:"username"`
	Type             uint32 `json:"type"`
	Content          string `json:"content,omitempty"`
	ClientID         string `json:"client_id,omitempty"`
	Receiver         string `json:"receiver,omitempty"`
	ReceiverNickname string `json:"receiver_nickname,omitempty"`
	ReplyCommentID   int32  `json:"reply_comment_id,omitempty"`
	ReplyCommentID2  uint64 `json:"reply_comment_id2,omitempty"`
	ObjectCreateTime int32  `json:"object_create_time,omitempty"`
}

// MomentsOperation 一项朋友圈操作。
type MomentsOperation struct {
	ID     uint64 `json:"id"`
	Type   uint32 `json:"type"`
	Extend []byte `json:"extend,omitempty"`
}

// MomentsOperateRequest 批量朋友圈操作。
type MomentsOperateRequest struct {
	Operations []MomentsOperation `json:"operations"`
}

// MomentsSyncRequest 原始同步游标。
type MomentsSyncRequest struct {
	Key []byte `json:"key,omitempty"`
}

// MomentsPrivacyRequest 修改朋友圈可见性。
type MomentsPrivacyRequest struct {
	ID      uint64 `json:"id"`
	Privacy bool   `json:"privacy"`
}

// MomentsPostRequest 发布朋友圈；图片和视频通过宿主媒体引用提交。
type MomentsPostRequest struct {
	Content      string         `json:"content,omitempty"`
	ClientID     string         `json:"client_id,omitempty"`
	Privacy      uint32         `json:"privacy,omitempty"`
	WithUsers    []string       `json:"with_users,omitempty"`
	VisibleUsers []string       `json:"visible_users,omitempty"`
	Blacklist    []string       `json:"blacklist,omitempty"`
	AppName      string         `json:"app_name,omitempty"`
	AppID        string         `json:"app_id,omitempty"`
	Images       []sdk.MediaRef `json:"images,omitempty"`
	ImagesURL    string         `json:"images_url,omitempty"`
	Video        *sdk.MediaRef  `json:"video,omitempty"`
	VideoURL     string         `json:"video_url,omitempty"`
	Cover        *sdk.MediaRef  `json:"cover,omitempty"`
	CoverURL     string         `json:"cover_url,omitempty"`
}

// OfficialUsernameRequest 指定公众号。
type OfficialUsernameRequest struct {
	Username string `json:"username"`
}

// OfficialURLRequest 指定公众号网页或文章 URL。
type OfficialURLRequest struct {
	URL string `json:"url"`
}

// OfficialJSAPIRequest 预留有界 JSAPI 参数。
type OfficialJSAPIRequest struct {
	Parameters json.RawMessage `json:"parameters"`
}

// OfficialOauthRequest 预留有界 OAuth 参数。
type OfficialOauthRequest struct {
	Parameters json.RawMessage `json:"parameters"`
}

// PaymentCollectRequest 接收转账。
type PaymentCollectRequest struct {
	InvalidTime   string `json:"invalid_time"`
	TransferID    string `json:"transfer_id"`
	TransactionID string `json:"transaction_id"`
	ToUsername    string `json:"to_username"`
}

// PaymentHongBaoDetailRequest 查询红包详情。
type PaymentHongBaoDetailRequest struct {
	NativeURL    string `json:"native_url"`
	SendUsername string `json:"send_username"`
}

// PaymentHongBaoListRequest 查询红包领取列表。
type PaymentHongBaoListRequest struct {
	NativeURL string `json:"native_url"`
	Offset    int32  `json:"offset,omitempty"`
	Limit     int32  `json:"limit,omitempty"`
}

// PaymentGrabHongBaoRequest 抢红包。
type PaymentGrabHongBaoRequest struct {
	NativeURL       string `json:"native_url"`
	InWay           int    `json:"in_way"`
	SessionUsername string `json:"session_username,omitempty"`
}

// UserUpdateProfileRequest 保留 core 当前资料字段的有界 JSON。
type UserUpdateProfileRequest struct {
	Profile json.RawMessage `json:"profile"`
}

// UserUploadAvatarRequest 使用宿主媒体引用上传头像。
type UserUploadAvatarRequest struct {
	Ref *sdk.MediaRef `json:"ref,omitempty"`
	URL string        `json:"url,omitempty"`
}

// UserQRCodeRequest 查询个人二维码。
type UserQRCodeRequest struct {
	Style uint32 `json:"style,omitempty"`
}

// UserPrivacyRequest 修改一个隐私选项。
type UserPrivacyRequest struct {
	Function int32 `json:"function"`
	Value    int32 `json:"value"`
}

// UserAliasRequest 修改微信号。
type UserAliasRequest struct {
	Alias string `json:"alias"`
}

// UserDeleteSafeDeviceRequest 删除安全设备。
type UserDeleteSafeDeviceRequest struct {
	UUID string `json:"uuid"`
}

// UserReportMotionRequest 上报运动步数。
type UserReportMotionRequest struct {
	DeviceID   string `json:"device_id"`
	DeviceType string `json:"device_type"`
	StepCount  int64  `json:"step_count"`
}
