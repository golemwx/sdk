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
	// MessageID 微信消息 ID，以字符串原样保存，避免大整数精度丢失
	MessageID string
	// ActorUsername 实际发言者的微信 username
	ActorUsername string
	// Digest 引用消息的展示摘要
	Digest string
}

// Message 归一化业务消息
type Message struct {
	// ID 微信消息 ID，以字符串原样保存，避免大整数精度丢失
	ID string
	// Conversation 所属或目标业务会话，群内所有成员共用整群会话
	Conversation sdk.Conversation
	// Actor 实际发言者，无法确定或没有发言者时为空
	Actor *sdk.Actor
	// ActorResolution 实际发言者解析结果，解析失败时不以群身份代替
	ActorResolution sdk.ActorResolution
	// Direction 消息的入站或出站方向
	Direction Direction
	// Kind 业务种类，未知枚举归一为对应零值
	Kind MessageKind
	// CreatedAt 消息创建时间，必填并按 UTC 传递
	CreatedAt time.Time
	// Text 文本内容，缺失与显式空字符串可区分
	Text *string
	// Mentions 被提及者的微信 username 列表，缺失与显式空集合可区分
	Mentions []string
	// Quote 引用消息摘要，缺失时为空
	Quote *Quote
	// Media 宿主管理的媒体引用
	Media *sdk.MediaRef
}

// SendReceipt 消息发送结果
type SendReceipt struct {
	// MessageID 微信消息 ID，以字符串原样保存，避免大整数精度丢失
	MessageID string
	// Conversation 所属或目标业务会话，群内所有成员共用整群会话
	Conversation sdk.Conversation
	// SentAt 消息发送时间，必填并按 UTC 传递
	SentAt time.Time
	// Outcome 目标写入副作用结果，只读操作使用 NotApplicable
	Outcome sdk.WriteOutcome
	// RevokeRef 由宿主签发的撤回引用，无法提供时为空
	RevokeRef *sdk.RevokeRef
}

// MediaUpload 插件提供的媒体字节与元数据
type MediaUpload struct {
	// Kind 业务种类，未知枚举归一为对应零值
	Kind sdk.MediaKind
	// FileName 展示用文件名及扩展名，不得用于拼接宿主路径
	FileName string
	// Size 待上传媒体字节数，-1 代表未知
	Size int64
	// Reader 由插件打开并负责关闭的媒体字节流，与 Ref 必须二选一
	Reader io.Reader
	// Ref 复用的宿主管理媒体引用，与 Reader 必须二选一，Kind 须与引用一致
	Ref *sdk.MediaRef
	// Voice 语音时长与编码参数，Kind 为 Voice 时必填
	Voice *VoiceOptions
}

// VoiceOptions 业务音频参数
type VoiceOptions struct {
	// Duration 音频时长，必须为正，精确到毫秒，毫秒数不得超过 int32 上限
	Duration time.Duration
	// Encoding 公共音频编码格式
	Encoding VoiceEncoding
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
	Reply(ctx context.Context, conversation sdk.Conversation, quote Quote, text string) (*SendReceipt, error)
	Revoke(ctx context.Context, ref sdk.RevokeRef) error
	Download(ctx context.Context, media sdk.MediaRef) (io.ReadCloser, error)
}
