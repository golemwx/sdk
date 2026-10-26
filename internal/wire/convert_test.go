package wire

import (
	"reflect"
	"testing"
	"time"

	"github.com/golemwx/sdk"
	"github.com/golemwx/sdk/capability"
	"github.com/golemwx/sdk/chatroom"
	"github.com/golemwx/sdk/command"
	"github.com/golemwx/sdk/config"
	"github.com/golemwx/sdk/contact"
	"github.com/golemwx/sdk/event"
	"github.com/golemwx/sdk/internal/contract"
	"github.com/golemwx/sdk/message"
	"github.com/golemwx/sdk/session"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// pointer 为往返样本创建独立可选值
func pointer[T any](value T) *T {
	return &value
}

// sampleTime 返回含纳秒精度的固定 UTC 测试时间
func sampleTime() time.Time {
	return time.Date(2026, 10, 26, 9, 8, 7, 654321987, time.UTC)
}

// assertEqual 按存在性比较往返后的完整业务值
func assertEqual[T any](t *testing.T, got, want T) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip mismatch\ngot:  %#v\nwant: %#v", got, want)
	}
}

// marshalMessage 经过 Proto 二进制往返后返回消息
func marshalMessage(t *testing.T, value *Message) *Message {
	t.Helper()
	data, err := proto.Marshal(value)
	if err != nil {
		t.Fatalf("marshal message: %v", err)
	}

	result := new(Message)
	if err := proto.Unmarshal(data, result); err != nil {
		t.Fatalf("unmarshal message: %v", err)
	}

	return result
}

// marshalConfigSnapshot 经过 Proto 二进制往返后返回配置快照
func marshalConfigSnapshot(t *testing.T, value *ConfigSnapshot) *ConfigSnapshot {
	t.Helper()
	data, err := proto.Marshal(value)
	if err != nil {
		t.Fatalf("marshal config snapshot: %v", err)
	}

	result := new(ConfigSnapshot)
	if err := proto.Unmarshal(data, result); err != nil {
		t.Fatalf("unmarshal config snapshot: %v", err)
	}

	return result
}

// TestCoreModelRoundTrips 验证账号、联系人、群、成员和消息模型完整字段往返
func TestCoreModelRoundTrips(t *testing.T) {
	now := sampleTime()
	account := &sdk.Account{
		Username:  "wxid_owner",
		Nickname:  pointer("机器人"),
		Online:    true,
		Status:    sdk.AccountStatusOnline,
		UpdatedAt: now,
	}
	wireAccount, err := ToWireAccount(account)
	if err != nil {
		t.Fatalf("encode account: %v", err)
	}
	decodedAccount, err := FromWireAccount(wireAccount)
	if err != nil {
		t.Fatalf("decode account: %v", err)
	}
	assertEqual(t, decodedAccount, account)

	contactValue := &contact.Contact{
		Username:     "wxid_friend",
		Kind:         contact.ContactKindFriend,
		Nickname:     pointer("好友"),
		Remark:       pointer(""),
		AvatarURL:    pointer("https://example.invalid/avatar"),
		Alias:        pointer("alias"),
		UpdatedAt:    now,
		Completeness: sdk.CompletenessComplete,
	}
	wireContact, err := ToWireContact(contactValue)
	if err != nil {
		t.Fatalf("encode contact: %v", err)
	}
	decodedContact, err := FromWireContact(wireContact)
	if err != nil {
		t.Fatalf("decode contact: %v", err)
	}
	assertEqual(t, decodedContact, contactValue)

	room := &chatroom.Room{
		Username:            "room@chatroom",
		Name:                pointer("测试群"),
		Remark:              pointer("群备注"),
		OwnerUsername:       pointer("wxid_owner"),
		Announcement:        pointer("公告"),
		AnnouncementVersion: pointer(int32(17)),
		MemberCount:         pointer(int32(2)),
		UpdatedAt:           now,
		Completeness:        sdk.CompletenessComplete,
	}
	wireRoom, err := ToWireRoom(room)
	if err != nil {
		t.Fatalf("encode room: %v", err)
	}
	decodedRoom, err := FromWireRoom(wireRoom)
	if err != nil {
		t.Fatalf("decode room: %v", err)
	}
	assertEqual(t, decodedRoom, room)

	member := &chatroom.Member{
		RoomUsername: "room@chatroom",
		UserUsername: "wxid_member",
		DisplayName:  pointer("群昵称"),
		Nickname:     pointer("昵称"),
		Role:         chatroom.MemberRoleMember,
		JoinedAt:     &now,
		Completeness: sdk.CompletenessComplete,
	}
	wireMember, err := ToWireMember(member)
	if err != nil {
		t.Fatalf("encode member: %v", err)
	}
	decodedMember, err := FromWireMember(wireMember)
	if err != nil {
		t.Fatalf("decode member: %v", err)
	}
	assertEqual(t, decodedMember, member)

	messageValue := &message.Message{
		ID: "18446744073709551615",
		Conversation: sdk.Conversation{
			AccountUsername: "wxid_owner",
			Kind:            sdk.ConversationKindRoom,
			ID:              "room@chatroom",
		},
		Actor: &sdk.Actor{
			Username:    "wxid_member",
			DisplayName: pointer("群昵称"),
			Resolution:  sdk.ActorResolutionResolved,
		},
		ActorResolution: sdk.ActorResolutionResolved,
		Direction:       message.DirectionInbound,
		Kind:            message.MessageKindText,
		CreatedAt:       now,
		Text:            pointer("你好"),
		Mentions:        []string{"wxid_owner"},
		Quote: &message.Quote{
			MessageID:     "9007199254740993",
			ActorUsername: "wxid_owner",
			Digest:        "引用",
		},
		Media: &sdk.MediaRef{
			Token:     "media-token",
			Kind:      sdk.MediaKindImage,
			FileName:  "image.png",
			SizeBytes: 42,
			ExpiresAt: now.Add(time.Minute),
		},
	}
	wireMessage, err := ToWireMessage(messageValue)
	if err != nil {
		t.Fatalf("encode message: %v", err)
	}
	decodedMessage, err := FromWireMessage(marshalMessage(t, wireMessage))
	if err != nil {
		t.Fatalf("decode message: %v", err)
	}
	assertEqual(t, decodedMessage, messageValue)

	receipt := &message.SendReceipt{
		MessageID:    "18446744073709551615",
		Conversation: messageValue.Conversation,
		SentAt:       now,
		Outcome:      sdk.OutcomeSucceeded,
		RevokeRef:    &sdk.RevokeRef{Token: "revoke-token", ExpiresAt: now.Add(time.Hour)},
	}
	wireReceipt, err := ToWireSendReceipt(receipt)
	if err != nil {
		t.Fatalf("encode receipt: %v", err)
	}
	decodedReceipt, err := FromWireSendReceipt(wireReceipt)
	if err != nil {
		t.Fatalf("decode receipt: %v", err)
	}
	assertEqual(t, decodedReceipt, receipt)
}

// TestMinimalModelRoundTrips 验证仅必填字段不会制造可选值
func TestMinimalModelRoundTrips(t *testing.T) {
	now := sampleTime()

	account := &sdk.Account{Username: "owner", Status: sdk.AccountStatusLoggedIn, UpdatedAt: now}
	wireAccount, err := ToWireAccount(account)
	if err != nil {
		t.Fatalf("encode minimal account: %v", err)
	}
	decodedAccount, err := FromWireAccount(wireAccount)
	if err != nil {
		t.Fatalf("decode minimal account: %v", err)
	}
	assertEqual(t, decodedAccount, account)

	contactValue := &contact.Contact{Username: "friend", Kind: contact.ContactKindFriend, UpdatedAt: now}
	wireContact, err := ToWireContact(contactValue)
	if err != nil {
		t.Fatalf("encode minimal contact: %v", err)
	}
	decodedContact, err := FromWireContact(wireContact)
	if err != nil {
		t.Fatalf("decode minimal contact: %v", err)
	}
	assertEqual(t, decodedContact, contactValue)

	room := &chatroom.Room{Username: "room", UpdatedAt: now}
	wireRoom, err := ToWireRoom(room)
	if err != nil {
		t.Fatalf("encode minimal room: %v", err)
	}
	decodedRoom, err := FromWireRoom(wireRoom)
	if err != nil {
		t.Fatalf("decode minimal room: %v", err)
	}
	assertEqual(t, decodedRoom, room)

	member := &chatroom.Member{RoomUsername: "room", UserUsername: "member"}
	wireMember, err := ToWireMember(member)
	if err != nil {
		t.Fatalf("encode minimal member: %v", err)
	}
	decodedMember, err := FromWireMember(wireMember)
	if err != nil {
		t.Fatalf("decode minimal member: %v", err)
	}
	assertEqual(t, decodedMember, member)

	messageValue := &message.Message{
		ID:              "1",
		Conversation:    sdk.Conversation{AccountUsername: "owner", Kind: sdk.ConversationKindDirect, ID: "friend"},
		ActorResolution: sdk.ActorResolutionNoSender,
		Direction:       message.DirectionInbound,
		Kind:            message.MessageKindUnknown,
		CreatedAt:       now,
	}
	wireMessage, err := ToWireMessage(messageValue)
	if err != nil {
		t.Fatalf("encode minimal message: %v", err)
	}
	decodedMessage, err := FromWireMessage(marshalMessage(t, wireMessage))
	if err != nil {
		t.Fatalf("decode minimal message: %v", err)
	}
	assertEqual(t, decodedMessage, messageValue)

	receipt := &message.SendReceipt{
		Conversation: sdk.Conversation{AccountUsername: "owner", Kind: sdk.ConversationKindDirect, ID: "friend"},
		SentAt:       now,
		Outcome:      sdk.OutcomeUnknown,
	}
	wireReceipt, err := ToWireSendReceipt(receipt)
	if err != nil {
		t.Fatalf("encode minimal receipt: %v", err)
	}
	decodedReceipt, err := FromWireSendReceipt(wireReceipt)
	if err != nil {
		t.Fatalf("decode minimal receipt: %v", err)
	}
	assertEqual(t, decodedReceipt, receipt)

	change := &event.Change{Domain: event.ChangeDomainContact, Action: event.ChangeActionCreated, SubjectID: "friend"}
	wireChange, err := ToWireChange(change)
	if err != nil {
		t.Fatalf("encode minimal change: %v", err)
	}
	decodedChange, err := FromWireChange(wireChange)
	if err != nil {
		t.Fatalf("decode minimal change: %v", err)
	}
	assertEqual(t, decodedChange, change)
}

// TestEventAndBusinessRoundTrips 验证变更、事件、命令、能力、配置和描述往返
func TestEventAndBusinessRoundTrips(t *testing.T) {
	now := sampleTime()
	change := &event.Change{
		Domain:        event.ChangeDomainRoomMember,
		Action:        event.ChangeActionUpdated,
		SubjectID:     "wxid_member",
		ParentID:      pointer("room@chatroom"),
		ChangedFields: []string{"display_name"},
		Member: &chatroom.Member{
			RoomUsername: "room@chatroom",
			UserUsername: "wxid_member",
			DisplayName:  pointer("新群昵称"),
			Role:         chatroom.MemberRoleMember,
			JoinedAt:     &now,
			Completeness: sdk.CompletenessComplete,
		},
	}
	eventValue := &event.Event{
		Topic:           "room.member.changed",
		AccountUsername: "wxid_owner",
		Change:          change,
		Lease: &session.Lease{
			ID: "lease-id",
			Conversation: sdk.Conversation{
				AccountUsername: "wxid_owner",
				Kind:            sdk.ConversationKindRoom,
				ID:              "room@chatroom",
			},
			Topics:    []string{"message.*"},
			ExpiresAt: now.Add(time.Minute),
		},
		OccurredAt: now,
	}
	wireEvent, err := ToWireEvent(eventValue)
	if err != nil {
		t.Fatalf("encode event: %v", err)
	}
	decodedEvent, err := FromWireEvent(wireEvent)
	if err != nil {
		t.Fatalf("decode event: %v", err)
	}
	assertEqual(t, decodedEvent, eventValue)

	commandValue := &command.Command{
		Path:       []string{"example", "echo"},
		Args:       map[string]string{"text": "hello"},
		Positional: []string{"hello"},
		Actor:      &sdk.Actor{Username: "wxid_member", Resolution: sdk.ActorResolutionDirect},
		Raw:        "echo hello",
	}
	wireCommand, err := ToWireCommand(commandValue)
	if err != nil {
		t.Fatalf("encode command: %v", err)
	}
	decodedCommand, err := FromWireCommand(wireCommand)
	if err != nil {
		t.Fatalf("decode command: %v", err)
	}
	assertEqual(t, decodedCommand, commandValue)

	call := capability.CallRequest{
		CapabilityName: "text.to.image",
		ContentType:    "application/json",
		Payload:        []byte{},
		Actor:          &sdk.Actor{Username: "wxid_member", Resolution: sdk.ActorResolutionDirect},
		Conversation:   &sdk.Conversation{AccountUsername: "wxid_owner", Kind: sdk.ConversationKindDirect, ID: "wxid_member"},
	}
	assertEqual(t, FromWireCallRequest(ToWireCallRequest(call)), call)

	configValue := &config.ConfigSnapshot{Revision: 4, ActiveRevision: 3, Data: []byte{}, ApplyStatus: config.ApplyStatusFailed}
	assertEqual(t, FromWireConfigSnapshot(ToWireConfigSnapshot(configValue)), configValue)

	descriptor := contract.Descriptor{
		ID:                   "example",
		Name:                 "示例",
		Description:          "描述",
		Author:               "作者",
		Version:              "1.0.0",
		RequestedPermissions: []sdk.Permission{sdk.PermissionMessageSend},
		Subscriptions:        []event.Subscription{{Topic: "message.text", Mode: event.SubscribeModeRespond, Priority: 10}},
		Commands: []command.CommandSpec{{
			Path:       []string{"example", "echo"},
			Permission: sdk.PermissionMessageSend,
			Args: []command.ArgSpec{{
				Name: "text", Type: command.ArgTypeString, Required: true,
				Enum: []string{"a", "b"}, Min: pointer(1.0), Max: pointer(10.0), Help: "文本",
			}},
			Help: "回显",
		}},
		Capabilities:     []capability.CapabilitySpec{{Name: "text.to.image", InputType: "application/json", OutputType: "image/png", Description: "渲染", MaxInlineSize: 1024}},
		UsesCapabilities: []string{"statistics.query_messages"},
	}
	assertEqual(t, FromWireDescriptor(ToWireDescriptor(descriptor)), descriptor)
}

// TestPresenceRoundTrips 验证缺失、显式空和空集合保持可区分
func TestPresenceRoundTrips(t *testing.T) {
	now := sampleTime()
	base := message.Message{
		ID:              "1",
		Conversation:    sdk.Conversation{AccountUsername: "owner", Kind: sdk.ConversationKindDirect, ID: "friend"},
		ActorResolution: sdk.ActorResolutionNoSender,
		Direction:       message.DirectionInbound,
		Kind:            message.MessageKindText,
		CreatedAt:       now,
	}

	missingWire, err := ToWireMessage(&base)
	if err != nil {
		t.Fatalf("encode missing fields: %v", err)
	}
	missing, err := FromWireMessage(marshalMessage(t, missingWire))
	if err != nil {
		t.Fatalf("decode missing fields: %v", err)
	}
	if missing.Text != nil || missing.Mentions != nil {
		t.Fatalf("missing values changed: %#v", missing)
	}

	base.Text = pointer("")
	base.Mentions = []string{}
	emptyWire, err := ToWireMessage(&base)
	if err != nil {
		t.Fatalf("encode empty fields: %v", err)
	}
	empty, err := FromWireMessage(marshalMessage(t, emptyWire))
	if err != nil {
		t.Fatalf("decode empty fields: %v", err)
	}
	if empty.Text == nil || *empty.Text != "" || empty.Mentions == nil || len(empty.Mentions) != 0 {
		t.Fatalf("explicit empty values changed: %#v", empty)
	}

	nilConfig := FromWireConfigSnapshot(marshalConfigSnapshot(t, ToWireConfigSnapshot(&config.ConfigSnapshot{Data: nil})))
	emptyConfig := FromWireConfigSnapshot(marshalConfigSnapshot(t, ToWireConfigSnapshot(&config.ConfigSnapshot{Data: []byte{}})))
	if nilConfig.Data != nil || emptyConfig.Data == nil || len(emptyConfig.Data) != 0 {
		t.Fatal("config data presence was not preserved")
	}

	nilFields, err := ToWireChange(&event.Change{Domain: event.ChangeDomainContact, Action: event.ChangeActionCreated, SubjectID: "a"})
	if err != nil {
		t.Fatalf("encode nil changed fields: %v", err)
	}
	emptyFields, err := ToWireChange(&event.Change{Domain: event.ChangeDomainContact, Action: event.ChangeActionCreated, SubjectID: "a", ChangedFields: []string{}})
	if err != nil {
		t.Fatalf("encode empty changed fields: %v", err)
	}
	nilChange, err := FromWireChange(nilFields)
	if err != nil {
		t.Fatalf("decode nil changed fields: %v", err)
	}
	emptyChange, err := FromWireChange(emptyFields)
	if err != nil {
		t.Fatalf("decode empty changed fields: %v", err)
	}
	if nilChange.ChangedFields != nil || emptyChange.ChangedFields == nil {
		t.Fatal("changed fields presence was not preserved")
	}
}

// TestUnknownEnumsMapToZero 验证未知枚举双向均降为 Unknown 或 Unspecified
func TestUnknownEnumsMapToZero(t *testing.T) {
	now := timestamppb.New(sampleTime())
	decoded, err := FromWireMessage(&Message{
		Id:              "1",
		Conversation:    &Conversation{AccountUsername: "owner", Kind: ConversationKind_CONVERSATION_KIND_DIRECT, Id: "friend"},
		ActorResolution: ActorResolution_ACTOR_RESOLUTION_NO_SENDER,
		Direction:       Direction_DIRECTION_INBOUND,
		Kind:            MessageKind(999),
		CreatedAt:       now,
	})
	if err != nil {
		t.Fatalf("decode unknown message kind: %v", err)
	}
	if decoded.Kind != message.MessageKindUnknown {
		t.Fatalf("message kind = %d", decoded.Kind)
	}

	member, err := FromWireMember(&Member{JoinedAt: nil, Role: MemberRole(999)})
	if err != nil {
		t.Fatalf("decode unknown member role: %v", err)
	}
	if member.Role != chatroom.MemberRoleUnknown {
		t.Fatalf("member role = %d", member.Role)
	}

	contactValue, err := FromWireContact(&Contact{Kind: ContactKind(999), UpdatedAt: now})
	if err != nil {
		t.Fatalf("decode unknown contact kind: %v", err)
	}
	if contactValue.Kind != contact.ContactKindUnknown {
		t.Fatalf("contact kind = %d", contactValue.Kind)
	}

	errorValue, err := FromWireError(&Error{Outcome: WriteOutcome(999)})
	if err != nil {
		t.Fatalf("decode unknown outcome: %v", err)
	}
	if errorValue.Outcome != sdk.OutcomeUnspecified {
		t.Fatalf("outcome = %d", errorValue.Outcome)
	}

	encoded, err := ToWireMessage(&message.Message{
		ID:              "1",
		Conversation:    sdk.Conversation{AccountUsername: "owner", Kind: sdk.ConversationKindDirect, ID: "friend"},
		ActorResolution: sdk.ActorResolutionNoSender,
		Direction:       message.DirectionInbound,
		Kind:            message.MessageKind(999),
		CreatedAt:       sampleTime(),
	})
	if err != nil {
		t.Fatalf("encode unknown message kind: %v", err)
	}
	if encoded.Kind != MessageKind_MESSAGE_KIND_UNKNOWN {
		t.Fatalf("wire message kind = %d", encoded.Kind)
	}
}

// TestInvalidRequiredTimes 验证缺失、零值和越界时间均被拒绝
func TestInvalidRequiredTimes(t *testing.T) {
	if _, err := ToWireAccount(&sdk.Account{}); err == nil {
		t.Fatal("zero required time was accepted")
	}
	if _, err := FromWireAccount(&Account{}); err == nil {
		t.Fatal("missing required time was accepted")
	}
	if _, err := FromWireAccount(&Account{UpdatedAt: &timestamppb.Timestamp{Seconds: 253402300800}}); err == nil {
		t.Fatal("out-of-range timestamp was accepted")
	}
}

// TestErrorAndBatchRoundTrips 验证错误结果与部分成功逐项状态往返
func TestErrorAndBatchRoundTrips(t *testing.T) {
	now := sampleTime()
	input := &sdk.BatchResult[contact.Contact]{Items: []sdk.BatchItem[contact.Contact]{
		{
			SubjectID: "ok",
			Value: contact.Contact{
				Username: "ok", Kind: contact.ContactKindFriend, UpdatedAt: now, Completeness: sdk.CompletenessComplete,
			},
			Outcome: sdk.OutcomeNotApplicable,
		},
		{
			SubjectID: "failed",
			Outcome:   sdk.OutcomeNotApplicable,
			Error: &sdk.Error{
				Category: sdk.CategoryUnavailable, Operation: "contacts.refresh", InvocationID: "call-1",
				Outcome: sdk.OutcomeNotApplicable, Reason: "temporarily unavailable", RetryAfter: time.Second,
			},
		},
	}}
	wireValue, err := ToWireContactBatchResult(input)
	if err != nil {
		t.Fatalf("encode batch: %v", err)
	}
	decoded, err := FromWireContactBatchResult(wireValue)
	if err != nil {
		t.Fatalf("decode batch: %v", err)
	}
	assertEqual(t, decoded, input)
}

// TestUnknownFieldCompatibility 验证新增字段可被旧结构忽略并由 Proto 保留
func TestUnknownFieldCompatibility(t *testing.T) {
	source := &Message{
		Id:              "9007199254740993",
		Conversation:    &Conversation{AccountUsername: "owner", Kind: ConversationKind_CONVERSATION_KIND_DIRECT, Id: "friend"},
		ActorResolution: ActorResolution_ACTOR_RESOLUTION_NO_SENDER,
		Direction:       Direction_DIRECTION_INBOUND,
		Kind:            MessageKind_MESSAGE_KIND_TEXT,
		CreatedAt:       timestamppb.New(sampleTime()),
		Text:            pointer("hello"),
	}
	data, err := proto.Marshal(source)
	if err != nil {
		t.Fatalf("marshal source: %v", err)
	}
	data = protowire.AppendTag(data, 200, protowire.BytesType)
	data = protowire.AppendString(data, "future-value")

	var decoded Message
	if err := proto.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal with unknown field: %v", err)
	}
	if len(decoded.ProtoReflect().GetUnknown()) == 0 {
		t.Fatal("unknown field was not preserved")
	}
	business, err := FromWireMessage(&decoded)
	if err != nil {
		t.Fatalf("convert message: %v", err)
	}
	if business.ID != source.Id || business.Text == nil || *business.Text != "hello" {
		t.Fatalf("known fields changed: %#v", business)
	}
}
