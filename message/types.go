// Package message 定义完整消息模型、类型化消息数据和宿主消息能力。
package message

import (
	"context"
	"io"
	"strconv"
	"time"

	"github.com/golemwx/sdk"
	"github.com/golemwx/sdk/chatroom"
	"github.com/golemwx/sdk/contact"
)

// Type 是微信消息业务类型。
type Type uint8

const (
	// TypeUnknown 表示未知或尚不支持的消息类型。
	TypeUnknown Type = iota
	// TypeText 表示文本消息。
	TypeText
	// TypeImage 表示图片消息。
	TypeImage
	// TypeVoice 表示语音消息。
	TypeVoice
	// TypeVideo 表示视频消息。
	TypeVideo
	// TypeEmoji 表示表情消息。
	TypeEmoji
	// TypeLocation 表示位置消息。
	TypeLocation
	// TypeApp 表示应用消息。
	TypeApp
	// TypeFile 表示文件消息。
	TypeFile
)

// Data 是八类消息之一的结构化业务数据。
//
// 解析失败或未知类型时 Message.Data 为 nil，但 Content 和 Raw 必须保留。
// Data 是封闭集合，插件不能实现该接口。
type Data interface {
	isMessageData()
}

// Text 表示文本消息的结构化数据；正文本身位于 Message.Content。
type Text struct{}

func (*Text) isMessageData() {}

// Media 是媒体消息共用的真实下载元数据。
//
// FileID、MD5、Key 和 URL 均来自已识别的同步数据，不是宿主签发的业务 token。
type Media struct {
	FileID string
	MD5    string
	Key    string
	URL    string
	Size   uint64
}

// Image 是图片消息的类型化数据。
type Image struct {
	Media
	Width  uint32
	Height uint32
}

func (*Image) isMessageData() {}

// Voice 是语音消息的类型化数据。
type Voice struct {
	Media
	Duration time.Duration
	Format   string
}

func (*Voice) isMessageData() {}

// Video 是视频消息的类型化数据。
type Video struct {
	Media
	Duration      time.Duration
	ThumbnailURL  string
	ThumbnailKey  string
	ThumbnailSize uint64
	NewMD5        string
}

func (*Video) isMessageData() {}

// Emoji 是表情消息的类型化数据。
type Emoji struct {
	MD5  string
	URL  string
	Size uint64
}

func (*Emoji) isMessageData() {}

// Location 是位置消息的类型化数据。
type Location struct {
	X       float64
	Y       float64
	Label   string
	POIName string
}

func (*Location) isMessageData() {}

// App 是应用消息的通用结构化数据。
type App struct {
	Type        uint32
	Title       string
	Description string
	URL         string
}

func (*App) isMessageData() {}

// File 是文件消息的类型化数据。
type File struct {
	Media
	Name      string
	Extension string
	AppID     string
	AttachID  string
}

func (*File) isMessageData() {}

// Message 是 frame 投递和插件引用的完整消息。
//
// Sender 和 Receiver 保留原始会话方向；群消息的实际发言者只放在 Member。
// Raw 是 frame 从已识别同步输入建立的原始数据副本，不要求 core 透传未知命令。
type Message struct {
	ID        uint64
	Type      Type
	Sender    contact.Contact
	Receiver  contact.Contact
	Member    *chatroom.Member
	Content   string
	Raw       []byte
	Timestamp time.Time
	Data      Data
}

// Text 返回文本数据；非文本消息返回 nil。
func (m *Message) Text() *Text {
	if m == nil {
		return nil
	}
	value, _ := m.Data.(*Text)
	return value
}

// Image 返回图片数据；非图片消息返回 nil。
func (m *Message) Image() *Image {
	if m == nil {
		return nil
	}
	value, _ := m.Data.(*Image)
	return value
}

// Voice 返回语音数据；非语音消息返回 nil。
func (m *Message) Voice() *Voice {
	if m == nil {
		return nil
	}
	value, _ := m.Data.(*Voice)
	return value
}

// Video 返回视频数据；非视频消息返回 nil。
func (m *Message) Video() *Video {
	if m == nil {
		return nil
	}
	value, _ := m.Data.(*Video)
	return value
}

// Emoji 返回表情数据；非表情消息返回 nil。
func (m *Message) Emoji() *Emoji {
	if m == nil {
		return nil
	}
	value, _ := m.Data.(*Emoji)
	return value
}

// Location 返回位置数据；非位置消息返回 nil。
func (m *Message) Location() *Location {
	if m == nil {
		return nil
	}
	value, _ := m.Data.(*Location)
	return value
}

// App 返回应用数据；非应用消息返回 nil。
func (m *Message) App() *App {
	if m == nil {
		return nil
	}
	value, _ := m.Data.(*App)
	return value
}

// File 返回文件数据；非文件消息返回 nil。
func (m *Message) File() *File {
	if m == nil {
		return nil
	}
	value, _ := m.Data.(*File)
	return value
}

// SendResult 是宿主真实发送结果，字段足以构造撤回请求。
//
// ClientID 保留上游原始文本形态：文本回执可能是数字，媒体回执可能是字符串。
// 媒体视频可能没有 CreatedAt；缺失字段不得由 SDK 编造。
type SendResult struct {
	ID         uint64
	NewID      uint64
	ClientID   string
	Sender     contact.Contact
	Receiver   contact.Contact
	Type       Type
	CreatedAt  *time.Time
	ServerTime *time.Time
	Outcome    sdk.WriteOutcome
}

// Revocable 判断当前公开字段是否足以构造撤回请求。
func (r *SendResult) Revocable() bool {
	if r == nil || r.NewID == 0 || r.ClientID == "" || r.Receiver.Username == "" || r.CreatedAt == nil || r.CreatedAt.IsZero() {
		return false
	}
	if year := r.CreatedAt.UTC().Year(); year < 1 || year > 9999 {
		return false
	}
	if r.Outcome != sdk.OutcomeSucceeded {
		return false
	}
	clientID, err := strconv.ParseUint(r.ClientID, 10, 64)
	return err == nil && clientID != 0
}

// Upload 是插件发送媒体的流式输入。
type Upload struct {
	Type Type
	Name string
	// Size 是 Reader 必须提供的字节数；-1 表示未知，其他负数无效。
	Size   int64
	Reader io.Reader
	Voice  *VoiceUpload
}

// VoiceUpload 是语音发送所需的业务参数。
type VoiceUpload struct {
	Duration time.Duration
	Format   string
}

// Client 是宿主消息能力。
//
// 所有方法都使用当前插件连接识别调用方，不接收账号、插件 ID 或业务 token。
// Download 接受完整消息，以便语音等类型使用消息 ID 和原始大小。
type Client interface {
	SendText(ctx context.Context, target, text string, mentions []string) (*SendResult, error)
	SendApp(ctx context.Context, target string, app App) (*SendResult, error)
	SendMedia(ctx context.Context, target string, upload Upload) (*SendResult, error)
	Forward(ctx context.Context, target string, source Message) (*SendResult, error)
	Revoke(ctx context.Context, sent SendResult) error
	Download(ctx context.Context, source Message) (io.ReadCloser, error)
}
