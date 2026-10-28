// Package message 定义消息领域模型和宿主消息客户端
package message

import (
	"context"
	"io"
	"time"

	"github.com/golemwx/sdk"
)

// Direction 消息方向
type Direction int

const (
	// DirectionUnknown 未知消息方向
	DirectionUnknown Direction = iota
	// DirectionInbound 入站消息
	DirectionInbound
	// DirectionOutbound 出站消息
	DirectionOutbound
)

// MessageKind 消息种类
type MessageKind int

const (
	// MessageKindUnknown 未知消息种类
	MessageKindUnknown MessageKind = iota
	// MessageKindText 文本消息
	MessageKindText
	// MessageKindImage 图片消息
	MessageKindImage
	// MessageKindVoice 语音消息
	MessageKindVoice
	// MessageKindVideo 视频消息
	MessageKindVideo
	// MessageKindFile 文件消息
	MessageKindFile
	// MessageKindEmoji 表情消息
	MessageKindEmoji
	// MessageKindLink 链接消息
	MessageKindLink
	// MessageKindApp 应用消息
	MessageKindApp
	// MessageKindNote 笔记消息
	MessageKindNote
	// MessageKindRecord 聊天记录消息
	MessageKindRecord
	// MessageKindMusic 音乐消息
	MessageKindMusic
	// MessageKindPosition 位置消息
	MessageKindPosition
	// MessageKindNameCard 名片消息
	MessageKindNameCard
	// MessageKindSystem 系统消息
	MessageKindSystem
)

// Quote 引用摘要
type Quote struct {
	MessageID     string // 微信消息 ID，以字符串原样保存，避免大整数精度丢失
	ActorUsername string // 实际发言者的微信 username
	Digest        string // 引用消息的展示摘要
}

// Message 归一化业务消息
type Message struct {
	ID              string              // 微信消息 ID，以字符串原样保存，避免大整数精度丢失
	Conversation    sdk.Conversation    // 所属或目标业务会话，群内所有成员共用整群会话
	Actor           *sdk.Actor          // 实际发言者，无法确定或没有发言者时为空
	ActorResolution sdk.ActorResolution // 实际发言者解析结果，解析失败时不以群身份代替
	Direction       Direction           // 消息的入站或出站方向
	Kind            MessageKind         // 业务种类，未知枚举归一为对应零值
	CreatedAt       time.Time           // 消息创建时间，必填并按 UTC 传递
	Text            *string             // 文本内容，缺失与显式空字符串可区分
	Mentions        []string            // 被提及者的微信 username 列表，缺失与显式空集合可区分
	Quote           *Quote              // 引用消息摘要，缺失时为空
	Media           *sdk.MediaRef       // 宿主管理的媒体引用
	ForwardRef      *sdk.ForwardRef     // 宿主管理的转发引用，不包含原始 XML 或 CDN 参数
}

// SendReceipt 消息发送结果
type SendReceipt struct {
	MessageID    string           // 微信消息 ID，以字符串原样保存，避免大整数精度丢失
	Conversation sdk.Conversation // 所属或目标业务会话，群内所有成员共用整群会话
	SentAt       time.Time        // 消息发送时间，必填并按 UTC 传递
	Outcome      sdk.WriteOutcome // 目标写入副作用结果，只读操作使用 NotApplicable
	RevokeRef    *sdk.RevokeRef   // 由宿主签发的撤回引用，无法提供时为空
}

// MediaUpload 插件提供的媒体字节与元数据
type MediaUpload struct {
	Kind     sdk.MediaKind // 业务种类，未知枚举归一为对应零值
	FileName string        // 展示用文件名及扩展名，不得用于拼接宿主路径
	Size     int64         // 待上传媒体字节数，-1 代表未知
	Reader   io.Reader     // 由插件打开并负责关闭的媒体字节流，与 Ref 必须二选一
	Ref      *sdk.MediaRef // 复用的宿主管理媒体引用，与 Reader 必须二选一，Kind 须与引用一致
	Voice    *VoiceOptions // 语音时长与编码参数，Kind 为 Voice 时必填
}

// Link 结构化链接消息，不触发宿主下载远程 URL
type Link struct {
	Title       string // 链接标题
	Description string // 链接摘要
	URL         string // 作为消息内容投递的 HTTP/HTTPS 地址，不作为宿主取文件入口
}

// Location 结构化位置消息
type Location struct {
	Label     string  // 展示地址
	POIName   string  // 地点名称
	Longitude float64 // 经度，范围 [-180, 180]
	Latitude  float64 // 纬度，范围 [-90, 90]
	Scale     float64 // 地图缩放级别，必须为有限正数
}

// NameCard 结构化联系人名片
type NameCard struct {
	Username string // 联系人的微信 username
	Nickname string // 展示昵称
	Alias    string // 可选别名
}

// VoiceOptions 业务音频参数
type VoiceOptions struct {
	Duration time.Duration // 音频时长，必须为正，精确到毫秒，毫秒数不得超过 int32 上限
	Encoding VoiceEncoding // 公共音频编码格式
}

// VoiceEncoding 公共音频格式
type VoiceEncoding int

const (
	// VoiceEncodingUnknown 未知音频格式
	VoiceEncodingUnknown VoiceEncoding = iota
	// VoiceEncodingAMR AMR 音频格式
	VoiceEncodingAMR
	// VoiceEncodingSPEEX SPEEX 音频格式
	VoiceEncodingSPEEX
	// VoiceEncodingMP3 MP3 音频格式
	VoiceEncodingMP3
	// VoiceEncodingWAVE WAVE 音频格式
	VoiceEncodingWAVE
	// VoiceEncodingSILK SILK 音频格式
	VoiceEncodingSILK
)

// MessagesClient 宿主消息能力
type MessagesClient interface {
	SendText(ctx context.Context, conversation sdk.Conversation, text string, mentions []string) (*SendReceipt, error)
	SendMedia(ctx context.Context, conversation sdk.Conversation, media MediaUpload) (*SendReceipt, error)
	StoreMedia(ctx context.Context, media MediaUpload) (*sdk.MediaRef, error)
	Reply(ctx context.Context, conversation sdk.Conversation, quote Quote, text string) (*SendReceipt, error)
	Forward(ctx context.Context, conversation sdk.Conversation, ref sdk.ForwardRef) (*SendReceipt, error)
	SendLink(ctx context.Context, conversation sdk.Conversation, link Link) (*SendReceipt, error)
	SendLocation(ctx context.Context, conversation sdk.Conversation, location Location) (*SendReceipt, error)
	SendNameCard(ctx context.Context, conversation sdk.Conversation, card NameCard) (*SendReceipt, error)
	Revoke(ctx context.Context, ref sdk.RevokeRef) error
	Download(ctx context.Context, media sdk.MediaRef) (io.ReadCloser, error)
}
