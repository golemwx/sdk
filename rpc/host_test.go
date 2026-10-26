package rpc

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/golemwx/sdk"
	"github.com/golemwx/sdk/capability"
	"github.com/golemwx/sdk/chatroom"
	"github.com/golemwx/sdk/config"
	"github.com/golemwx/sdk/contact"
	"github.com/golemwx/sdk/internal/transport"
	"github.com/golemwx/sdk/internal/wire"
	"github.com/golemwx/sdk/message"
	"github.com/golemwx/sdk/plugin"
	"github.com/golemwx/sdk/session"
	"github.com/hashicorp/go-hclog"
	hplugin "github.com/hashicorp/go-plugin"
)

// fixtureTime 返回包含纳秒的固定人工时间
func fixtureTime() time.Time { return time.Date(2026, 10, 27, 0, 0, 0, 123, time.UTC) }

// fixtureMessages 提供消息一元 RPC 的人工结果
type fixtureMessages struct{}

// SendText 保留字符串大整数 ID 并回显目标会话
func (fixtureMessages) SendText(ctx context.Context, conversation sdk.Conversation, text string, mentions []string) (*message.SendReceipt, error) {
	return &message.SendReceipt{MessageID: "18446744073709551615", Conversation: conversation, SentAt: fixtureTime(), Outcome: sdk.OutcomeSucceeded}, nil
}

// Reply 提供引用回复的人工回执
func (f fixtureMessages) Reply(ctx context.Context, conversation sdk.Conversation, quote message.Quote, text string) (*message.SendReceipt, error) {
	return f.SendText(ctx, conversation, text, nil)
}

// Revoke 允许撤回人工引用，不执行真实业务
func (fixtureMessages) Revoke(context.Context, sdk.RevokeRef) error { return nil }

// SendMedia 不在握手测试中实现真实媒体上传
func (fixtureMessages) SendMedia(context.Context, sdk.Conversation, message.MediaUpload) (*message.SendReceipt, error) {
	return nil, sdk.NewError(sdk.CategoryUnimplemented, "fixture.media", "not available")
}

// Download 不在握手测试中实现真实媒体下载
func (fixtureMessages) Download(context.Context, sdk.MediaRef) (io.ReadCloser, error) {
	return nil, sdk.NewError(sdk.CategoryUnimplemented, "fixture.download", "not available")
}

// fixtureContacts 提供本地联系人及部分成功刷新结果
type fixtureContacts struct{}

// Get 回显指定人工联系人
func (fixtureContacts) Get(ctx context.Context, username string) (*contact.Contact, error) {
	return &contact.Contact{Username: username, UpdatedAt: fixtureTime()}, nil
}

// Find 提供显式空的有界查询结果
func (fixtureContacts) Find(context.Context, string, sdk.PageRequest) (*sdk.Page[contact.Contact], error) {
	return &sdk.Page[contact.Contact]{Items: []contact.Contact{}, Total: 0, Completeness: sdk.CompletenessComplete}, nil
}

// List 提供显式空联系人列表
func (f fixtureContacts) List(ctx context.Context, page sdk.PageRequest) (*sdk.Page[contact.Contact], error) {
	return f.Find(ctx, "", page)
}

// Refresh 返回逐项只读失败及同一整体错误
func (fixtureContacts) Refresh(ctx context.Context, usernames []string) (*sdk.BatchResult[contact.Contact], error) {
	err := sdk.NewError(sdk.CategoryUnavailable, "contacts.refresh", "fixture unavailable")
	return &sdk.BatchResult[contact.Contact]{Items: []sdk.BatchItem[contact.Contact]{{SubjectID: usernames[0], Outcome: sdk.OutcomeNotApplicable, Error: err}}}, err
}

// fixtureChatrooms 提供群与成员的人工快照
type fixtureChatrooms struct{}

// Get 提供必填群更新时间
func (fixtureChatrooms) Get(ctx context.Context, username string) (*chatroom.Room, error) {
	return &chatroom.Room{Username: username, UpdatedAt: fixtureTime()}, nil
}

// Members 提供显式空成员分页
func (fixtureChatrooms) Members(context.Context, string, sdk.PageRequest) (*sdk.Page[chatroom.Member], error) {
	return &sdk.Page[chatroom.Member]{Items: []chatroom.Member{}, Completeness: sdk.CompletenessComplete}, nil
}

// Member 回显人工群成员身份
func (fixtureChatrooms) Member(ctx context.Context, room, user string) (*chatroom.Member, error) {
	return &chatroom.Member{RoomUsername: room, UserUsername: user}, nil
}

// Refresh 提供群资料和完整空成员列表
func (f fixtureChatrooms) Refresh(ctx context.Context, username string) (*chatroom.ChatroomRefreshResult, error) {
	room, _ := f.Get(ctx, username)
	members, _ := f.Members(ctx, username, sdk.PageRequest{})
	return &chatroom.ChatroomRefreshResult{Room: room, Members: members, Stages: []chatroom.RefreshStageResult{}}, nil
}

// fixtureSessions 提供整个业务会话的人工租约
type fixtureSessions struct{}

// Acquire 保留会话与主题
func (fixtureSessions) Acquire(ctx context.Context, conversation sdk.Conversation, topics []string) (*session.Lease, error) {
	return &session.Lease{ID: "lease", Conversation: conversation, Topics: topics, ExpiresAt: fixtureTime().Add(time.Minute)}, nil
}

// Renew 提供固定人工租约，不访问真实会话
func (f fixtureSessions) Renew(ctx context.Context, id string, ttl time.Duration) (*session.Lease, error) {
	return f.Acquire(ctx, sdk.Conversation{}, nil)
}

// Release 接受人工租约释放
func (fixtureSessions) Release(context.Context, string) error { return nil }

// fixtureCaller 提供保留显式空的能力载荷
type fixtureCaller struct{}

// Call 回显内容类型及业务字节
func (fixtureCaller) Call(ctx context.Context, request capability.CallRequest) (*capability.CallResult, error) {
	return &capability.CallResult{ContentType: request.ContentType, Payload: request.Payload}, nil
}

// fixtureConfig 提供自身业务配置人工快照
type fixtureConfig struct{}

// fixtureEmptyAccount 模拟宿主返回不合法的空成功
type fixtureEmptyAccount struct{}

// Current 故意省略业务结果以验证传输层拒绝空成功
func (fixtureEmptyAccount) Current(context.Context) (*sdk.Account, error) { return nil, nil }

// Snapshot 返回显式空配置和不同的保存及生效修订
func (fixtureConfig) Snapshot(context.Context) (*config.ConfigSnapshot, error) {
	return &config.ConfigSnapshot{Revision: 2, ActiveRevision: 1, Data: []byte{}, ApplyStatus: config.ApplyStatusPending}, nil
}

// Save 验证 CAS 修订及显式空配置
func (fixtureConfig) Save(ctx context.Context, revision uint64, data []byte) (*config.ConfigSaveResult, error) {
	if revision != 2 || data == nil {
		return nil, sdk.NewError(sdk.CategoryConflict, "config.save", "unexpected fixture revision")
	}
	return &config.ConfigSaveResult{Revision: 3}, nil
}

// ConfigDefaults 返回显式空业务默认配置
func (p *fixturePlugin) ConfigDefaults() []byte { return []byte{} }

// ConfigValidate 校验候选值存在性，使 nil 与显式空具有不同结果
func (p *fixturePlugin) ConfigValidate(ctx context.Context, data []byte) error {
	if data == nil {
		return sdk.NewError(sdk.CategoryInvalidArgument, "config.validate", "missing candidate")
	}
	return nil
}

// ConfigApply 使用相同存在性条件模拟原子应用
func (p *fixturePlugin) ConfigApply(ctx context.Context, data []byte) error {
	return p.ConfigValidate(ctx, data)
}

// exerciseHost 在真实插件进程中调用全部已接入的一元宿主方法
func exerciseHost(ctx context.Context, host *plugin.Host) error {
	conversation := sdk.Conversation{AccountUsername: "owner", Kind: sdk.ConversationKindDirect, ID: "friend"}
	receipt, err := host.Messages.SendText(ctx, conversation, "hello", nil)
	if err != nil {
		return err
	}
	if receipt == nil || receipt.MessageID != "18446744073709551615" {
		return errors.New("message ID lost precision")
	}
	if _, err := host.Messages.Reply(ctx, conversation, message.Quote{MessageID: receipt.MessageID}, "reply"); err != nil {
		return err
	}
	if err := host.Messages.Revoke(ctx, sdk.RevokeRef{Token: "revoke", ExpiresAt: fixtureTime().Add(time.Minute)}); err != nil {
		return err
	}
	if _, err := host.Messages.SendMedia(ctx, conversation, message.MediaUpload{}); !hasCategory(err, sdk.CategoryUnimplemented) {
		return errors.New("upload silently succeeded")
	}
	if _, err := host.Messages.Download(ctx, sdk.MediaRef{}); !hasCategory(err, sdk.CategoryUnimplemented) {
		return errors.New("download silently succeeded")
	}
	if _, err := host.Contacts.Get(ctx, "friend"); err != nil {
		return err
	}
	if page, err := host.Contacts.Find(ctx, "", sdk.PageRequest{}); err != nil || page == nil || page.Items == nil {
		return errors.New("contact find failed")
	}
	if _, err := host.Contacts.List(ctx, sdk.PageRequest{}); err != nil {
		return err
	}
	batch, err := host.Contacts.Refresh(ctx, []string{"friend"})
	if !hasCategory(err, sdk.CategoryUnavailable) || batch == nil || len(batch.Items) != 1 || batch.Items[0].Error == nil {
		return errors.New("batch failure result lost")
	}
	if _, err := host.Chatrooms.Get(ctx, "room"); err != nil {
		return err
	}
	if _, err := host.Chatrooms.Members(ctx, "room", sdk.PageRequest{}); err != nil {
		return err
	}
	if _, err := host.Chatrooms.Member(ctx, "room", "friend"); err != nil {
		return err
	}
	if _, err := host.Chatrooms.Refresh(ctx, "room"); err != nil {
		return err
	}
	if _, err := host.Sessions.Acquire(ctx, conversation, []string{"message.*"}); err != nil {
		return err
	}
	if _, err := host.Sessions.Renew(ctx, "lease", time.Minute); err != nil {
		return err
	}
	if err := host.Sessions.Release(ctx, "lease"); err != nil {
		return err
	}
	call, err := host.Caller.Call(ctx, capability.CallRequest{CapabilityName: "fixture.echo", ContentType: "application/json", Payload: []byte{}})
	if err != nil || call == nil || call.Payload == nil {
		return errors.New("capability presence lost")
	}
	snapshot, err := host.Config.Snapshot(ctx)
	if err != nil || snapshot == nil || snapshot.Data == nil || snapshot.Revision != 2 || snapshot.ActiveRevision != 1 {
		return errors.New("configuration snapshot changed")
	}
	if _, err := host.Config.Save(ctx, 2, []byte{}); err != nil {
		return err
	}
	return nil
}

// hasCategory 判断业务错误类别，测试不依赖展示文本
func hasCategory(err error, category sdk.Category) bool {
	var business *sdk.Error
	return errors.As(err, &business) && business.Category == category
}

// TestHostUnaryContracts 验证七类服务共用真实反向连接及配置控制 RPC
func TestHostUnaryContracts(t *testing.T) {
	options := fixtureOptions(t, "host-contracts")
	options.Services.Host = &plugin.Host{Messages: fixtureMessages{}, Contacts: fixtureContacts{}, Chatrooms: fixtureChatrooms{}, Sessions: fixtureSessions{}, Caller: fixtureCaller{}, Config: fixtureConfig{}, Account: &fixtureAccount{}}
	client, err := Start(t.Context(), options)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	defaults, err := client.ConfigDefaults(t.Context())
	if err != nil || defaults == nil || len(defaults) != 0 {
		t.Fatalf("defaults: %v %v", defaults, err)
	}
	if err := client.ConfigValidate(t.Context(), nil); !hasCategory(err, sdk.CategoryInvalidArgument) {
		t.Fatalf("nil candidate: %v", err)
	}
	if err := client.ConfigValidate(t.Context(), []byte{}); err != nil {
		t.Fatal(err)
	}
	if err := client.ConfigApply(t.Context(), []byte{}); err != nil {
		t.Fatal(err)
	}
	response, err := client.endpoint.Control.Drain(t.Context(), &wire.DrainRequest{ControlToken: client.control.token, InstanceGeneration: 1})
	if err != nil || response.GetError().GetCategory() != wire.Category_CATEGORY_UNIMPLEMENTED {
		t.Fatal("unfinished execution tracking returned success")
	}
}

// TestMissingHostResult 验证宿主空成功会转换为明确的 Internal 错误
func TestMissingHostResult(t *testing.T) {
	options := fixtureOptions(t, "empty-host-result")
	options.Services.Host.Account = fixtureEmptyAccount{}
	client, err := Start(t.Context(), options)
	if err != nil {
		t.Fatal(err)
	}
	_ = client.Close()
	assertReaped(t, options.Command)
}

// TestReverseBrokerDialFailure 验证缺少反向监听时实际拨号失败并回收进程
func TestReverseBrokerDialFailure(t *testing.T) {
	options := fixtureOptions(t, "normal")
	process := hplugin.NewClient(&hplugin.ClientConfig{HandshakeConfig: HandshakeConfig(), Plugins: PluginSet(), Cmd: options.Command, AutoMTLS: true, SkipHostEnv: true, AllowedProtocols: []hplugin.Protocol{hplugin.ProtocolGRPC}, StartTimeout: 10 * time.Second, Logger: hclog.NewNullLogger()})
	defer process.Kill()
	protocol, err := process.Client()
	if err != nil {
		t.Fatal(err)
	}
	value, err := protocol.Dispense(PluginKey)
	if err != nil {
		t.Fatal(err)
	}
	endpoint := value.(*transport.Endpoint)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	response, err := endpoint.Control.Initialize(ctx, &wire.InitializeRequest{BrokerId: endpoint.Broker.NextId(), ControlToken: "fixture", InstanceGeneration: 1, DataDir: options.DataDir})
	if err != nil {
		t.Fatal(err)
	}
	if response.GetError().GetCategory() != wire.Category_CATEGORY_UNAVAILABLE {
		t.Fatalf("reverse dial unexpectedly succeeded: %v", response)
	}
	process.Kill()
	assertReaped(t, options.Command)
}
