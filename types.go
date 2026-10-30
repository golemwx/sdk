package sdk

import "time"

// WriteOutcome 写操作副作用结果
type WriteOutcome int

const (
	// OutcomeUnspecified 未指定或未知结果，不能据此认定写入成功
	OutcomeUnspecified WriteOutcome = iota
	// OutcomeNotApplicable 只读操作，不涉及目标写入副作用
	OutcomeNotApplicable
	// OutcomeSucceeded 上游已明确确认目标写入成功
	OutcomeSucceeded
	// OutcomeFailed 已确认目标写入未完成
	OutcomeFailed
	// OutcomeUnknown 目标写入可能已生效，调用方不得自动重试
	OutcomeUnknown
)

// Completeness 当前资料或查询范围的完整性
type Completeness int

const (
	// CompletenessUnknown 资料或查询范围的完整性未知
	CompletenessUnknown Completeness = iota
	// CompletenessIncomplete 资料或查询范围尚不完整，不得据此授予管理权限
	CompletenessIncomplete
	// CompletenessComplete 资料或查询范围已完整，与是否还有下一页无关
	CompletenessComplete
)

// PageRequest 有界分页请求
type PageRequest struct {
	Limit  int    // 每页条数，0 使用默认 100，最大 1000，负数无效
	Cursor string // 不透明分页游标，空字符串从头开始，续页时原样回传
}

// Page 有界分页结果
type Page[T any] struct {
	Items        []T          // 当前页业务对象，成功的空结果使用非 nil 空切片
	NextCursor   string       // 下一页不透明游标，空字符串说明没有更多结果
	Total        int64        // 当前授权范围的总条数，-1 代表未知
	Completeness Completeness // 对象或查询范围的资料完整性，与分页是否结束无关
}

// NormalizePageRequest 校验并规范分页大小
func NormalizePageRequest(operation string, page PageRequest) (PageRequest, error) {
	if page.Limit < 0 {
		return PageRequest{}, NewError(CategoryInvalidArgument, operation, "limit must not be negative")
	}

	if page.Limit == 0 {
		page.Limit = 100
	} else if page.Limit > 1000 {
		page.Limit = 1000
	}

	return page, nil
}

// BatchResult 批量操作结果
type BatchResult[T any] struct {
	Items []BatchItem[T] // 按输入顺序保留的逐项结果
}

// BatchItem 单项结果
type BatchItem[T any] struct {
	SubjectID string       // 本项操作或资料变更的目标业务标识
	Value     T            // 单项业务值，仅在 Error 为空时读取
	Outcome   WriteOutcome // 目标写入副作用结果，只读操作使用 NotApplicable
	Error     *Error       // 业务错误，成功时为空；部分成功仍保留逐项结果
}

// AccountStatus 账号业务状态
type AccountStatus int

const (
	// AccountStatusUnknown 账号状态未知
	AccountStatusUnknown AccountStatus = iota
	// AccountStatusUnlogged 账号未登录
	AccountStatusUnlogged
	// AccountStatusLoggedIn 账号已登录
	AccountStatusLoggedIn
	// AccountStatusOnline 账号在线
	AccountStatusOnline
	// AccountStatusOffline 账号离线
	AccountStatusOffline
)

// Account 当前账号业务快照
type Account struct {
	Username  string        // 微信 username，不另建用户标识
	Nickname  *string       // 昵称，缺失与显式空字符串可区分
	Online    bool          // 账号当前是否在线
	Status    AccountStatus // 账号业务状态
	UpdatedAt time.Time     // 资料快照更新时间，必填并按 UTC 传递
}

// ConversationKind 会话种类
type ConversationKind int

const (
	// ConversationKindUnknown 未知会话种类
	ConversationKindUnknown ConversationKind = iota
	// ConversationKindDirect 联系人私聊会话
	ConversationKindDirect
	// ConversationKindRoom 整个群共用的群会话
	ConversationKindRoom
)

// Conversation 账号内的业务会话
type Conversation struct {
	AccountUsername string           // 所属账号的微信 username
	Kind            ConversationKind // 业务种类，未知枚举归一为对应零值
	ID              string           // 私聊对端或群的微信 username，不能使用群成员 ID 代替群会话
}

// ActorResolution 实际发言者解析结果
type ActorResolution int

const (
	// ActorResolutionUnknown 发言者解析状态未知
	ActorResolutionUnknown ActorResolution = iota
	// ActorResolutionResolved 已从群消息中解析出实际成员
	ActorResolutionResolved
	// ActorResolutionDirect 已确定私聊消息的实际发言者
	ActorResolutionDirect
	// ActorResolutionSystem 系统消息，不对应实际发言者
	ActorResolutionSystem
	// ActorResolutionParseFailed 实际发言者解析失败，不得用群身份代替
	ActorResolutionParseFailed
	// ActorResolutionNoSender 消息未提供发言者
	ActorResolutionNoSender
)

// Actor 实际发言者
type Actor struct {
	Username    string          // 微信 username，不另建用户标识
	DisplayName *string         // 展示名称，缺失与显式空字符串可区分
	Resolution  ActorResolution // 实际发言者解析状态
}

// MediaKind 媒体种类
type MediaKind int

const (
	// MediaKindUnknown 未知媒体种类
	MediaKindUnknown MediaKind = iota
	// MediaKindImage 图片媒体
	MediaKindImage
	// MediaKindVoice 语音媒体
	MediaKindVoice
	// MediaKindVideo 视频媒体
	MediaKindVideo
	// MediaKindFile 文件媒体
	MediaKindFile
	// MediaKindEmoji 表情媒体
	MediaKindEmoji
	// MediaKindOther 已知消息类型但暂未归类的媒体
	MediaKindOther
)

// MediaRef 宿主管理的媒体引用
type MediaRef struct {
	Token     string    // 绑定账号、插件实例和资源范围的不透明媒体引用凭据
	Kind      MediaKind // 业务种类，未知枚举归一为对应零值
	FileName  string    // 展示用文件名及扩展名，不得用于拼接宿主路径
	SizeBytes int64     // 媒体字节数，-1 代表未知
	ExpiresAt time.Time // 媒体引用到期时间，失效后使用返回 FailedPrecondition
}

// RevokeRef 宿主管理的撤回引用
type RevokeRef struct {
	Token     string    // 绑定账号、插件实例和目标消息的不透明撤回引用凭据
	ExpiresAt time.Time // 撤回引用到期时间，失效后使用返回 FailedPrecondition
}

// ForwardRef 宿主管理的消息转发引用
type ForwardRef struct {
	Token     string    // 绑定账号、插件实例和源消息的不透明转发引用凭据
	ExpiresAt time.Time // 转发引用到期时间，失效后使用返回 FailedPrecondition
}

// Permission 插件权限标识
type Permission string

const (
	// PermissionMessageSend 发送、回复和撤回消息权限
	PermissionMessageSend Permission = "message.send"
	// PermissionMessageRead 读取消息及下载授权媒体权限
	PermissionMessageRead Permission = "message.read"
	// PermissionMessageRaw 使用原始消息 XML 或消息 ID 发送、撤回、下载和转写权限
	PermissionMessageRaw Permission = "message.raw"
	// PermissionContactRead 读取联系人资料权限
	PermissionContactRead Permission = "contact.read"
	// PermissionContactWrite 显式刷新联系人资料等写入口权限
	PermissionContactWrite Permission = "contact.write"
	// PermissionContactManage 好友申请、验证、备注、黑名单和删除联系人权限
	PermissionContactManage Permission = "contact.manage"
	// PermissionChatroomRead 读取群资料和成员权限
	PermissionChatroomRead Permission = "chatroom.read"
	// PermissionChatroomWrite 显式刷新群资料等写入口权限
	PermissionChatroomWrite Permission = "chatroom.write"
	// PermissionChatroomManage 建群、成员、管理员和群资料管理权限
	PermissionChatroomManage Permission = "chatroom.manage"
	// PermissionLabelRead 读取联系人标签权限
	PermissionLabelRead Permission = "label.read"
	// PermissionLabelWrite 创建、修改、删除标签及设置联系人标签权限
	PermissionLabelWrite Permission = "label.write"
	// PermissionFavoriteRead 读取收藏容量、条目和同步变更权限
	PermissionFavoriteRead Permission = "favorite.read"
	// PermissionFavoriteWrite 删除、分享和检查收藏资源权限
	PermissionFavoriteWrite Permission = "favorite.write"
	// PermissionMomentsRead 读取朋友圈时间线、详情和同步变更权限
	PermissionMomentsRead Permission = "moments.read"
	// PermissionMomentsWrite 发布、互动、删除和设置朋友圈可见性权限
	PermissionMomentsWrite Permission = "moments.write"
	// PermissionMiniappUse 使用小程序登录、会话、数据和手机号能力权限
	PermissionMiniappUse Permission = "miniapp.use"
	// PermissionOfficialRead 读取公众号授权、文章和 JSAPI 信息权限
	PermissionOfficialRead Permission = "official.read"
	// PermissionOfficialWrite 关注、取关和点赞公众号文章权限
	PermissionOfficialWrite Permission = "official.write"
	// PermissionPaymentUse 查询支付信息、接收转账和处理红包权限
	PermissionPaymentUse Permission = "payment.use"
	// PermissionUserRead 读取当前用户资料、二维码和安全设备权限
	PermissionUserRead Permission = "user.read"
	// PermissionUserWrite 修改当前用户资料、头像、隐私、别名和设备状态权限
	PermissionUserWrite Permission = "user.write"
	// PermissionCDNUse 使用 core 的 CDN 上传、下载和资源检查能力权限
	PermissionCDNUse Permission = "cdn.use"
	// PermissionLoginManage 管理当前 core 账号登录生命周期权限
	PermissionLoginManage Permission = "login.manage"
	// PermissionSessionAcquire 申请、续期和释放自身会话租约权限
	PermissionSessionAcquire Permission = "session.acquire"
	// PermissionCapabilityCall 按能力名调用其他插件权限
	PermissionCapabilityCall Permission = "capability.call"
	// PermissionConfigSelfWrite 保存插件自身业务配置权限
	PermissionConfigSelfWrite Permission = "config.self.write"
	// PermissionBackgroundRun 在宿主授权范围内执行后台任务权限
	PermissionBackgroundRun Permission = "background.run"
)

// SensitivePermission 判断新接入的高风险权限是否必须由管理员显式授予。
// 旧版已发布权限保持原有自动声明授予行为，避免破坏现有插件升级。
func SensitivePermission(permission Permission) bool {
	switch permission {
	case PermissionMessageRaw, PermissionContactManage, PermissionChatroomManage,
		PermissionLabelRead, PermissionLabelWrite,
		PermissionFavoriteRead, PermissionFavoriteWrite,
		PermissionMomentsRead, PermissionMomentsWrite,
		PermissionMiniappUse, PermissionOfficialRead, PermissionOfficialWrite,
		PermissionPaymentUse, PermissionUserRead, PermissionUserWrite,
		PermissionCDNUse, PermissionLoginManage:
		return true
	default:
		return false
	}
}

// KnownPermission 判断权限是否属于当前公共契约。
func KnownPermission(permission Permission) bool {
	switch permission {
	case PermissionMessageSend, PermissionMessageRead, PermissionMessageRaw,
		PermissionContactRead, PermissionContactWrite, PermissionContactManage,
		PermissionChatroomRead, PermissionChatroomWrite, PermissionChatroomManage,
		PermissionLabelRead, PermissionLabelWrite,
		PermissionFavoriteRead, PermissionFavoriteWrite,
		PermissionMomentsRead, PermissionMomentsWrite,
		PermissionMiniappUse, PermissionOfficialRead, PermissionOfficialWrite,
		PermissionPaymentUse, PermissionUserRead, PermissionUserWrite,
		PermissionCDNUse, PermissionLoginManage,
		PermissionSessionAcquire, PermissionCapabilityCall,
		PermissionConfigSelfWrite, PermissionBackgroundRun:
		return true
	default:
		return false
	}
}
