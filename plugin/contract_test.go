package plugin

import (
	"context"
	"io"
	"io/fs"
	"os"
	"testing"
	"time"

	"github.com/golemwx/sdk"
	"github.com/golemwx/sdk/capability"
	"github.com/golemwx/sdk/chatroom"
	"github.com/golemwx/sdk/command"
	"github.com/golemwx/sdk/contact"
	"github.com/golemwx/sdk/event"
	"github.com/golemwx/sdk/message"
	"github.com/golemwx/sdk/session"
)

type contractPlugin struct {
	*Base
	*Config[contractConfig]
}

type contractConfig struct {
	Enabled bool
}

func (*contractPlugin) Metadata() Metadata {
	return Metadata{ID: "fixture"}
}

func (*contractPlugin) Subscriptions() []event.Subscription {
	return nil
}

func (*contractPlugin) OnEvent(context.Context, event.Event) (event.Result, error) {
	return event.Result{}, nil
}

func (*contractPlugin) Commands() any {
	return struct{}{}
}

func (*contractPlugin) Capabilities() []capability.Spec {
	return nil
}

func (*contractPlugin) OnCapability(context.Context, capability.Request) (capability.Result, error) {
	return capability.Result{}, nil
}

func (*contractPlugin) OnLoad(context.Context) error {
	return nil
}

func (*contractPlugin) OnStart(context.Context) error {
	return nil
}

func (*contractPlugin) OnStop(context.Context) error {
	return nil
}

func (*contractPlugin) OnUnload(context.Context) error {
	return nil
}

func (*contractPlugin) OnConfigChanged(context.Context, contractConfig, contractConfig) error {
	return nil
}

var (
	_ Plugin                              = (*contractPlugin)(nil)
	_ Loader                              = (*contractPlugin)(nil)
	_ Starter                             = (*contractPlugin)(nil)
	_ Stopper                             = (*contractPlugin)(nil)
	_ Unloader                            = (*contractPlugin)(nil)
	_ EventHandler                        = (*contractPlugin)(nil)
	_ CommandProvider                     = (*contractPlugin)(nil)
	_ CapabilityProvider                  = (*contractPlugin)(nil)
	_ ConfigChangeHandler[contractConfig] = (*contractPlugin)(nil)
)

type contractRuntime struct{}

func (*contractRuntime) Describe(context.Context) (*Descriptor, error) {
	return nil, nil
}

func (*contractRuntime) Initialize(context.Context, InitializeOptions) error {
	return nil
}

func (*contractRuntime) Load(context.Context) error {
	return nil
}

func (*contractRuntime) Start(context.Context) error {
	return nil
}

func (*contractRuntime) Stop(context.Context) error {
	return nil
}

func (*contractRuntime) Unload(context.Context) error {
	return nil
}

func (*contractRuntime) ValidateConfig(context.Context, []byte) error {
	return nil
}

func (*contractRuntime) SetConfig(context.Context, []byte) error {
	return nil
}

func (*contractRuntime) HandleEvent(context.Context, event.Event) (event.Result, error) {
	return event.Result{}, nil
}

func (*contractRuntime) HandleCommand(context.Context, command.Invocation) error {
	return nil
}

func (*contractRuntime) HandleCapability(context.Context, capability.Request) (capability.Result, error) {
	return capability.Result{}, nil
}

var _ Runtime = (*contractRuntime)(nil)

type contractAccount struct{}

func (*contractAccount) Current(context.Context) (*sdk.Account, error) {
	return nil, nil
}

type contractMessages struct{}

func (*contractMessages) SendText(context.Context, string, string, []string) (*message.SendResult, error) {
	return nil, nil
}

func (*contractMessages) SendApp(context.Context, string, message.App) (*message.SendResult, error) {
	return nil, nil
}

func (*contractMessages) SendMedia(context.Context, string, message.Upload) (*message.SendResult, error) {
	return nil, nil
}

func (*contractMessages) Forward(context.Context, string, message.Message) (*message.SendResult, error) {
	return nil, nil
}

func (*contractMessages) Revoke(context.Context, message.SendResult) error {
	return nil
}

func (*contractMessages) Download(context.Context, message.Message) (io.ReadCloser, error) {
	return nil, nil
}

type contractContacts struct{}

func (*contractContacts) Find(context.Context, contact.Selector) (*contact.Contact, error) {
	return nil, nil
}

type contractRooms struct{}

func (*contractRooms) Get(context.Context, string) (*chatroom.Chatroom, error) {
	return nil, nil
}

func (*contractRooms) Members(context.Context, string) ([]chatroom.Member, error) {
	return nil, nil
}

type contractSessions struct{}

func (*contractSessions) Acquire(context.Context, string, []event.Topic) (*session.Lease, error) {
	return nil, nil
}

func (*contractSessions) Renew(context.Context, string, time.Duration) (*session.Lease, error) {
	return nil, nil
}

func (*contractSessions) Release(context.Context, string) error {
	return nil
}

type contractCaller struct{}

func (*contractCaller) Call(context.Context, capability.Request) (*capability.Result, error) {
	return nil, nil
}

type contractData struct{}

func (*contractData) Open(string, int, fs.FileMode) (*os.File, error) {
	return nil, nil
}

func (*contractData) ReadFile(string) ([]byte, error) {
	return nil, nil
}

func (*contractData) WriteFile(string, []byte, fs.FileMode) error {
	return nil
}

func (*contractData) Remove(string) error {
	return nil
}

// TestHostContractComposesSmallClients 验证 Host 仅由显式小接口组成。
func TestHostContractComposesSmallClients(t *testing.T) {
	host := NewHost(
		&contractAccount{},
		&contractMessages{},
		&contractContacts{},
		&contractRooms{},
		&contractSessions{},
		&contractCaller{},
		&contractData{},
	)
	if host.Account == nil || host.Messages == nil || host.Contacts == nil ||
		host.Rooms == nil || host.Sessions == nil || host.Caller == nil || host.Data == nil {
		t.Fatalf("NewHost() left a capability unset: %+v", host)
	}
	if NewBase().Context() != nil {
		t.Fatal("unbound Base.Context() must be nil")
	}
}
