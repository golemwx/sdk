package wire

import (
	"errors"
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/golemwx/sdk"
	"github.com/golemwx/sdk/capability"
	"github.com/golemwx/sdk/chatroom"
	"github.com/golemwx/sdk/command"
	"github.com/golemwx/sdk/contact"
	"github.com/golemwx/sdk/event"
	"github.com/golemwx/sdk/message"
	"github.com/golemwx/sdk/plugin"
	"github.com/golemwx/sdk/session"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// TestMessageDataMustMatchType 验证类型化数据与消息类型不一致时不能静默通过。
func TestMessageDataMustMatchType(t *testing.T) {
	now := timestamppb.Now()
	tests := []struct {
		name    string
		convert func() error
	}{
		{
			name: "to wire",
			convert: func() error {
				_, err := ToWireMessage(&message.Message{
					Type:      message.TypeText,
					Timestamp: time.Now().UTC(),
					Data:      &message.Image{},
				})
				return err
			},
		},
		{
			name: "from wire",
			convert: func() error {
				_, err := FromWireMessage(&Message{
					Type:      MessageType_MESSAGE_TYPE_TEXT,
					Timestamp: now,
					Data:      &Message_Image{Image: &ImageData{}},
				})
				return err
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var business *sdk.Error
			if err := test.convert(); !errors.As(err, &business) || business.Category != sdk.CategoryInvalidArgument {
				t.Fatalf("conversion error = %v, want InvalidArgument", err)
			}
		})
	}
}

// TestUnknownEnumsNormalizeToZeroValues 验证未知 wire 枚举不会越界写入公开类型。
func TestUnknownEnumsNormalizeToZeroValues(t *testing.T) {
	now := timestamppb.Now()
	value, err := FromWireMessage(&Message{
		Type:      MessageType(99),
		Timestamp: now,
		Sender:    &Contact{Kind: ContactKind(99)},
	})
	if err != nil {
		t.Fatalf("FromWireMessage() error = %v", err)
	}
	if value.Type != message.TypeUnknown || value.Sender.Kind != contact.KindUnknown {
		t.Fatalf("unknown enums normalized to Type=%v Kind=%v", value.Type, value.Sender.Kind)
	}

	if got := fromWireSubscriptionMode(SubscriptionMode(99)); got != event.ModeUnknown {
		t.Fatalf("unknown subscription mode = %v", got)
	}
	if got := fromWireCommandScope(CommandScope(99)); got != command.ScopeUnknown {
		t.Fatalf("unknown command scope = %v", got)
	}
	if got := fromWireCategory(Category(99)); got != sdk.CategoryUnknown {
		t.Fatalf("unknown error category = %v", got)
	}
	if got := fromWireOutcome(WriteOutcome(99)); got != sdk.OutcomeUnspecified {
		t.Fatalf("unknown write outcome = %v", got)
	}
}

// TestWireBytesPreservePresence 验证 nil 与显式空载荷在包装类型中可区分。
func TestWireBytesPreservePresence(t *testing.T) {
	if value := ToWireBytes(nil); value != nil {
		t.Fatalf("ToWireBytes(nil) = %#v, want nil", value)
	}
	if value := FromWireBytes(nil); value != nil {
		t.Fatalf("FromWireBytes(nil) = %#v, want nil", value)
	}

	empty := []byte{}
	wireValue := ToWireBytes(empty)
	if wireValue == nil || wireValue.GetValue() == nil {
		t.Fatalf("empty payload lost explicit presence: %#v", wireValue)
	}
	decoded, err := proto.Marshal(wireValue)
	if err != nil {
		t.Fatalf("proto.Marshal() error = %v", err)
	}
	gotWire := &BytesValue{}
	if err := proto.Unmarshal(decoded, gotWire); err != nil {
		t.Fatalf("proto.Unmarshal() error = %v", err)
	}
	if value := FromWireBytes(gotWire); value == nil || len(value) != 0 {
		t.Fatalf("empty payload changed: %#v", value)
	}
}

// TestMessageRawPreservesPresence 验证消息原始数据缺失与显式空可区分。
func TestMessageRawPreservesPresence(t *testing.T) {
	now := time.Date(2026, 11, 6, 5, 10, 26, 0, time.UTC)
	tests := []struct {
		name string
		raw  []byte
	}{
		{name: "absent", raw: nil},
		{name: "explicit empty", raw: []byte{}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			want := &message.Message{Type: message.TypeText, Content: "正文", Raw: test.raw, Timestamp: now}
			wireValue, err := ToWireMessage(want)
			if err != nil {
				t.Fatalf("ToWireMessage() error = %v", err)
			}
			gotWire := roundTripProto(t, wireValue, &Message{})
			got, err := FromWireMessage(gotWire)
			if err != nil {
				t.Fatalf("FromWireMessage() error = %v", err)
			}
			if (got.Raw == nil) != (want.Raw == nil) || len(got.Raw) != len(want.Raw) {
				t.Fatalf("Raw presence changed: got %#v want %#v", got.Raw, want.Raw)
			}
		})
	}
}

// TestCommandSchemaRoundTripPreservesArgumentShape 验证命令 schema 能表达短参数和位置参数。
func TestCommandSchemaRoundTripPreservesArgumentShape(t *testing.T) {
	want := command.Schema{Roots: []command.Node{{
		Name:    "cron",
		Aliases: []string{"c"},
		Help:    "管理计划",
		Scope:   command.ScopeOwner,
		Arguments: []command.Argument{
			{Name: "name", Positional: true, Type: command.TypeString, Required: true},
			{Name: "target", Short: "t", Type: command.TypeString, Required: true},
			{Name: "labels", Short: "l", Type: command.TypeMap},
			{Name: "tags", Type: command.TypeString, Repeated: true},
			{Name: "mode", Type: command.TypeEnum, Default: "daily", Enum: []string{"daily", "weekly"}},
		},
		Children: []command.Node{
			{Name: "list", Aliases: []string{"ls"}, Help: "列出计划"},
		},
	}}}

	wireValue := ToWireCommandSchema(want)
	encoded, err := proto.Marshal(wireValue)
	if err != nil {
		t.Fatalf("proto.Marshal() error = %v", err)
	}
	decoded := &CommandSchema{}
	if err := proto.Unmarshal(encoded, decoded); err != nil {
		t.Fatalf("proto.Unmarshal() error = %v", err)
	}
	got := FromWireCommandSchema(decoded)
	gotWire := ToWireCommandSchema(got)
	if !proto.Equal(gotWire, wireValue) {
		t.Fatalf("schema changed: got %+v want %+v", got, want)
	}
}

// TestDescriptorRoundTripPreservesPolicyFields 验证描述中的订阅、能力和默认配置。
func TestDescriptorRoundTripPreservesPolicyFields(t *testing.T) {
	want := plugin.Descriptor{
		Metadata: plugin.Metadata{ID: "fixture", Name: "示例", Version: "1.0.0"},
		Subscriptions: []event.Subscription{
			{Topic: event.Message, Mode: event.Consume, Priority: -10},
			{Topic: event.Member, Mode: event.Observe},
		},
		Commands: command.Schema{Roots: []command.Node{{Name: "fixture", Scope: command.ScopeOwner}}},
		Capabilities: []capability.Spec{{
			Name:          "fixture.echo",
			InputType:     "text/plain",
			OutputType:    "text/plain",
			MaxInlineSize: capability.MaxInlineSize,
		}},
		HasConfig:     true,
		DefaultConfig: []byte{},
	}

	wireValue, err := ToWireDescriptor(want)
	if err != nil {
		t.Fatalf("ToWireDescriptor() error = %v", err)
	}
	encoded, err := proto.Marshal(wireValue)
	if err != nil {
		t.Fatalf("proto.Marshal() error = %v", err)
	}
	decoded := &Descriptor{}
	if err := proto.Unmarshal(encoded, decoded); err != nil {
		t.Fatalf("proto.Unmarshal() error = %v", err)
	}
	got := FromWireDescriptor(decoded)
	gotWire, err := ToWireDescriptor(got)
	if err != nil {
		t.Fatalf("ToWireDescriptor(round trip) error = %v", err)
	}
	if !proto.Equal(gotWire, wireValue) {
		t.Fatalf("descriptor changed: got %+v want %+v", got, want)
	}
}

// TestDescriptorRejectsOutOfRangeNumbers 验证整数转换失败而不是静默截断。
func TestDescriptorRejectsOutOfRangeNumbers(t *testing.T) {
	_, err := ToWireDescriptor(plugin.Descriptor{
		Subscriptions: []event.Subscription{{Priority: int(math.MaxInt32) + 1}},
	})
	assertInvalidArgument(t, err)

	_, err = ToWireCapabilitySpec(capability.Spec{MaxInlineSize: capability.MaxInlineSize + 1})
	assertInvalidArgument(t, err)
}

// TestMessageDurationValidation 验证非法时长的跨进程转换返回参数错误。
func TestMessageDurationValidation(t *testing.T) {
	_, err := FromWireMessage(&Message{
		Type:      MessageType_MESSAGE_TYPE_VOICE,
		Timestamp: timestamppb.Now(),
		Data: &Message_Voice{Voice: &VoiceData{
			Duration: &durationpb.Duration{Seconds: -1},
		}},
	})
	assertInvalidArgument(t, err)

	_, err = ToWireMessage(&message.Message{
		Type:      message.TypeVideo,
		Timestamp: time.Now().UTC(),
		Data:      &message.Video{Duration: -time.Second},
	})
	assertInvalidArgument(t, err)

	_, err = FromWireMessage(&Message{
		Type:      MessageType_MESSAGE_TYPE_VOICE,
		Timestamp: timestamppb.Now(),
		Data: &Message_Voice{Voice: &VoiceData{
			Duration: &durationpb.Duration{
				Seconds: int64(math.MaxInt64/int64(time.Second)) + 1,
			},
		}},
	})
	assertInvalidArgument(t, err)
}

// TestDownloadStreamCarriesStructuredError 验证下载流使用 data 分块和最终 Error 终止。
func TestDownloadStreamCarriesStructuredError(t *testing.T) {
	service := File_golem_sdk_host_proto.Services().ByName("HostMessagesService")
	if service == nil {
		t.Fatal("HostMessagesService is missing")
	}
	method := service.Methods().ByName("Download")
	if method == nil {
		t.Fatal("Download method is missing")
	}
	if !method.IsStreamingServer() || method.IsStreamingClient() {
		t.Fatal("Download must be a server-streaming RPC")
	}
	response := method.Output()
	oneofs := response.Oneofs()
	if oneofs.Len() != 1 || oneofs.Get(0).Name() != "value" {
		t.Fatalf("DownloadResponse oneofs = %v, want [value]", oneofs)
	}
	fields := response.Fields()
	dataField := fields.ByName("data")
	errorField := fields.ByName("error")
	if dataField == nil || errorField == nil {
		t.Fatalf("DownloadResponse fields = %v, want data and error", fields)
	}
	if dataField.Kind() != protoreflect.BytesKind || errorField.Message().FullName() != "golem.sdk.Error" {
		t.Fatalf("DownloadResponse field types = data:%v error:%v", dataField.Kind(), errorField.Message().FullName())
	}
}

// TestSendResultPreservesStringClientIDAndMissingTime 验证媒体回执的字符串 ID 和缺失时间。
func TestSendResultPreservesStringClientIDAndMissingTime(t *testing.T) {
	want := &message.SendResult{
		ID:       7,
		NewID:    8,
		ClientID: "media-client-id",
		Outcome:  sdk.OutcomeSucceeded,
	}
	wireValue, err := ToWireSendResult(want)
	if err != nil {
		t.Fatalf("ToWireSendResult() error = %v", err)
	}
	got, err := FromWireSendResult(wireValue)
	if err != nil {
		t.Fatalf("FromWireSendResult() error = %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("send result changed: got %+v want %+v", got, want)
	}
	if got.Revocable() {
		t.Fatal("string client ID must not be marked revocable by the current core contract")
	}
}

// TestSupportingWireModelsRoundTrip 验证事件、租约、错误、能力和命令调用契约。
func TestSupportingWireModelsRoundTrip(t *testing.T) {
	now := time.Date(2026, 11, 6, 5, 10, 26, 0, time.UTC)
	eventWant := event.Event{
		Topic:      event.Message,
		Message:    &message.Message{ID: 42, Type: message.TypeText, Content: "人工文本", Timestamp: now, Data: &message.Text{}},
		Contact:    &contact.Contact{Username: "wxid_fixture_peer", Kind: contact.KindPersonal},
		Chatroom:   &chatroom.Chatroom{Username: "fixture@chatroom", Name: "人工群"},
		Member:     &chatroom.Member{Username: "wxid_fixture_member", Chatroom: "fixture@chatroom"},
		OccurredAt: now,
	}
	wireEvent, err := ToWireEvent(eventWant)
	if err != nil {
		t.Fatalf("ToWireEvent() error = %v", err)
	}
	eventRoundTrip := roundTripProto(t, wireEvent, &Event{})
	eventGot, err := FromWireEvent(eventRoundTrip)
	if err != nil {
		t.Fatalf("FromWireEvent() error = %v", err)
	}
	if !reflect.DeepEqual(eventGot, &eventWant) {
		t.Fatalf("event changed: got %+v want %+v", eventGot, eventWant)
	}

	leaseWant := &session.Lease{
		ID:        "lease-fixture",
		Target:    "fixture@chatroom",
		Topics:    []event.Topic{event.Message, event.Contact},
		ExpiresAt: now.Add(time.Minute),
	}
	wireLease, err := ToWireLease(leaseWant)
	if err != nil {
		t.Fatalf("ToWireLease() error = %v", err)
	}
	leaseRoundTrip := roundTripProto(t, wireLease, &Lease{})
	leaseGot, err := FromWireLease(leaseRoundTrip)
	if err != nil {
		t.Fatalf("FromWireLease() error = %v", err)
	}
	if !reflect.DeepEqual(leaseGot, leaseWant) {
		t.Fatalf("lease changed: got %+v want %+v", leaseGot, leaseWant)
	}

	errorWant := &sdk.Error{
		Category:   sdk.CategoryPermissionDenied,
		Operation:  "fixture.operation",
		Outcome:    sdk.OutcomeUnknown,
		Reason:     "fixture reason",
		RetryAfter: 2 * time.Second,
	}
	wireError, err := ToWireError(errorWant)
	if err != nil {
		t.Fatalf("ToWireError() error = %v", err)
	}
	errorRoundTrip := roundTripProto(t, wireError, &Error{})
	errorGot, err := FromWireError(errorRoundTrip)
	if err != nil {
		t.Fatalf("FromWireError() error = %v", err)
	}
	if !reflect.DeepEqual(errorGot, errorWant) {
		t.Fatalf("error changed: got %+v want %+v", errorGot, errorWant)
	}

	requestWant := capability.Request{Name: "fixture.echo", ContentType: "text/plain", Payload: []byte{}}
	requestGot := FromWireCapabilityRequest(roundTripProto(t, ToWireCapabilityRequest(requestWant), &CapabilityRequest{}))
	if !reflect.DeepEqual(requestGot, requestWant) {
		t.Fatalf("capability request changed: got %+v want %+v", requestGot, requestWant)
	}

	resultWant := capability.Result{ContentType: "text/plain", Payload: []byte("reply")}
	resultGot := FromWireCapabilityResult(roundTripProto(t, ToWireCapabilityResult(resultWant), &CapabilityResult{}))
	if !reflect.DeepEqual(resultGot, resultWant) {
		t.Fatalf("capability result changed: got %+v want %+v", resultGot, resultWant)
	}

	commandWant := command.Invocation{
		Tokens:  []string{"cron", "list"},
		Raw:     "/cron list",
		Message: eventWant.Message,
		Owner:   true,
	}
	wireCommand, err := ToWireCommandInvocation(commandWant)
	if err != nil {
		t.Fatalf("ToWireCommandInvocation() error = %v", err)
	}
	commandRoundTrip := roundTripProto(t, wireCommand, &CommandInvocation{})
	commandGot, err := FromWireCommandInvocation(commandRoundTrip)
	if err != nil {
		t.Fatalf("FromWireCommandInvocation() error = %v", err)
	}
	if !reflect.DeepEqual(commandGot, &commandWant) {
		t.Fatalf("command invocation changed: got %+v want %+v", commandGot, commandWant)
	}

	accountWant := &sdk.Account{Username: "wxid_fixture_self", Nickname: "人工昵称", Online: true}
	accountGot := FromWireAccount(roundTripProto(t, ToWireAccount(accountWant), &Account{}))
	if !reflect.DeepEqual(accountGot, accountWant) {
		t.Fatalf("account changed: got %+v want %+v", accountGot, accountWant)
	}

	contactWant := contact.Contact{Username: "wxid_fixture_peer", Kind: contact.KindOfficial, Nickname: "公众号", Remark: "备注"}
	contactGot := FromWireContact(roundTripProto(t, ToWireContact(contactWant), &Contact{}))
	if !reflect.DeepEqual(contactGot, contactWant) {
		t.Fatalf("contact changed: got %+v want %+v", contactGot, contactWant)
	}

	chatroomWant := &chatroom.Chatroom{Username: "fixture@chatroom", Name: "人工群", OwnerUsername: "wxid_fixture_owner"}
	chatroomGot := FromWireChatroom(roundTripProto(t, ToWireChatroom(chatroomWant), &Chatroom{}))
	if !reflect.DeepEqual(chatroomGot, chatroomWant) {
		t.Fatalf("chatroom changed: got %+v want %+v", chatroomGot, chatroomWant)
	}

	memberWant := &chatroom.Member{Username: "wxid_fixture_member", Chatroom: "fixture@chatroom", DisplayName: "群昵称", Nickname: "人工昵称"}
	memberGot := FromWireMember(roundTripProto(t, ToWireMember(memberWant), &Member{}))
	if !reflect.DeepEqual(memberGot, memberWant) {
		t.Fatalf("member changed: got %+v want %+v", memberGot, memberWant)
	}
}

// TestErrorRetryAfterRejectsInvalidDuration 验证重试提示不会负数或溢出透传。
func TestErrorRetryAfterRejectsInvalidDuration(t *testing.T) {
	_, err := ToWireError(&sdk.Error{RetryAfter: -time.Second})
	assertInvalidArgument(t, err)

	_, err = FromWireError(&Error{
		RetryAfter: &durationpb.Duration{
			Seconds: int64(math.MaxInt64/int64(time.Second)) + 1,
		},
	})
	assertInvalidArgument(t, err)
}

func roundTripProto[T proto.Message](t *testing.T, value T, target T) T {
	t.Helper()
	encoded, err := proto.Marshal(value)
	if err != nil {
		t.Fatalf("proto.Marshal() error = %v", err)
	}
	if err := proto.Unmarshal(encoded, target); err != nil {
		t.Fatalf("proto.Unmarshal() error = %v", err)
	}
	return target
}

func assertInvalidArgument(t *testing.T, err error) {
	t.Helper()
	var business *sdk.Error
	if !errors.As(err, &business) || business.Category != sdk.CategoryInvalidArgument {
		t.Fatalf("error = %v, want InvalidArgument", err)
	}
}
