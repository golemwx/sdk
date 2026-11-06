package wire

import (
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/golemwx/sdk"
	"github.com/golemwx/sdk/chatroom"
	"github.com/golemwx/sdk/contact"
	"github.com/golemwx/sdk/message"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// TestMessageProtoRoundTripAllDataTypes 验证八类消息经真实 Proto 序列化后
// 仍保留 Content、Raw、类型和时间，且 oneof 分支不丢失。
func TestMessageProtoRoundTripAllDataTypes(t *testing.T) {
	now := time.Date(2026, 11, 6, 5, 10, 26, 123456789, time.UTC)
	raw := []byte(`{"type":"sync::new_message","data":{"recognized":true}}`)
	tests := []struct {
		name    string
		message message.Message
	}{
		{
			name: "text",
			message: message.Message{
				Type:    message.TypeText,
				Content: "人工文本",
				Data:    &message.Text{},
			},
		},
		{
			name: "image",
			message: message.Message{
				Type:    message.TypeImage,
				Content: "[32 x 32] 1024",
				Data: &message.Image{
					Media:  message.Media{FileID: "fixture-image", MD5: "md5-image", Key: "key-image", URL: "https://example.invalid/image", Size: 1024},
					Width:  32,
					Height: 32,
				},
			},
		},
		{
			name: "voice",
			message: message.Message{
				Type:    message.TypeVoice,
				Content: "[1.5s] 512",
				Data: &message.Voice{
					Media:    message.Media{FileID: "fixture-voice", Size: 512},
					Duration: 1500 * time.Millisecond,
					Format:   "4",
				},
			},
		},
		{
			name: "video",
			message: message.Message{
				Type:    message.TypeVideo,
				Content: "[3s] 4096",
				Data: &message.Video{
					Media:         message.Media{FileID: "fixture-video", MD5: "md5-video", Key: "key-video", URL: "https://example.invalid/video", Size: 4096},
					Duration:      3 * time.Second,
					ThumbnailURL:  "https://example.invalid/cover",
					ThumbnailKey:  "key-cover",
					ThumbnailSize: 128,
					NewMD5:        "new-md5-video",
				},
			},
		},
		{
			name: "emoji",
			message: message.Message{
				Type:    message.TypeEmoji,
				Content: "md5-emoji",
				Data:    &message.Emoji{MD5: "md5-emoji", URL: "https://example.invalid/emoji", Size: 256},
			},
		},
		{
			name: "location",
			message: message.Message{
				Type:    message.TypeLocation,
				Content: "人工地点",
				Data:    &message.Location{X: 121.5, Y: 31.2, Label: "人工地址", POIName: "人工地点"},
			},
		},
		{
			name: "app",
			message: message.Message{
				Type:    message.TypeApp,
				Content: "人工链接",
				Data:    &message.App{Type: 5, Title: "人工链接", Description: "人工描述", URL: "https://example.invalid/app"},
			},
		},
		{
			name: "file",
			message: message.Message{
				Type:    message.TypeFile,
				Content: "人工文件.txt",
				Data: &message.File{
					Media:     message.Media{FileID: "fixture-file", MD5: "md5-file", Key: "key-file", URL: "https://example.invalid/file", Size: 2048},
					Name:      "人工文件.txt",
					Extension: "txt",
					AppID:     "fixture-app",
					AttachID:  "fixture-attach",
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			want := test.message
			want.ID = 18446744073709551615
			want.Sender = contact.Contact{Username: "wxid_fixture_peer", Kind: contact.KindPersonal, Nickname: "人工昵称", Remark: "人工备注"}
			want.Receiver = contact.Contact{Username: "wxid_fixture_self", Kind: contact.KindSelf}
			want.Member = &chatroom.Member{Username: "wxid_fixture_member", Chatroom: "fixture@chatroom", DisplayName: "群昵称", Nickname: "人工昵称"}
			want.Raw = raw
			want.Timestamp = now

			wireValue, err := ToWireMessage(&want)
			if err != nil {
				t.Fatalf("ToWireMessage() error = %v", err)
			}
			encoded, err := proto.Marshal(wireValue)
			if err != nil {
				t.Fatalf("proto.Marshal() error = %v", err)
			}
			decoded := &Message{}
			if err := proto.Unmarshal(encoded, decoded); err != nil {
				t.Fatalf("proto.Unmarshal() error = %v", err)
			}
			got, err := FromWireMessage(decoded)
			if err != nil {
				t.Fatalf("FromWireMessage() error = %v", err)
			}

			if got.ID != want.ID || got.Type != want.Type || got.Content != want.Content {
				t.Fatalf("message identity changed: got ID=%d Type=%v Content=%q", got.ID, got.Type, got.Content)
			}
			if !reflect.DeepEqual(got.Raw, want.Raw) {
				t.Fatalf("Raw changed: got %q want %q", got.Raw, want.Raw)
			}
			if !got.Timestamp.Equal(want.Timestamp) {
				t.Fatalf("Timestamp changed: got %v want %v", got.Timestamp, want.Timestamp)
			}
			if !reflect.DeepEqual(got.Sender, want.Sender) || !reflect.DeepEqual(got.Receiver, want.Receiver) || !reflect.DeepEqual(got.Member, want.Member) {
				t.Fatalf("message participants changed: %+v", got)
			}
			if !reflect.DeepEqual(got.Data, want.Data) {
				t.Fatalf("Data changed: got %#v want %#v", got.Data, want.Data)
			}
		})
	}
}

// TestMessageProtoRoundTripPreservesFallbackText 验证解析失败正文和 Raw 不因 Data 缺失而丢失。
func TestMessageProtoRoundTripPreservesFallbackText(t *testing.T) {
	now := time.Date(2026, 11, 6, 5, 10, 26, 0, time.UTC)
	body := []byte("<msg><appmsg><title>保留损坏正文")
	want := &message.Message{
		ID:        110,
		Type:      message.TypeFile,
		Sender:    contact.Contact{Username: "wxid_fixture_peer", Kind: contact.KindPersonal},
		Receiver:  contact.Contact{Username: "wxid_fixture_self", Kind: contact.KindSelf},
		Content:   "<msg><appmsg><title>保留损坏正文",
		Raw:       body,
		Timestamp: now,
		Data:      nil,
	}

	wireValue, err := ToWireMessage(want)
	if err != nil {
		t.Fatalf("ToWireMessage() error = %v", err)
	}
	encoded, err := proto.Marshal(wireValue)
	if err != nil {
		t.Fatalf("proto.Marshal() error = %v", err)
	}
	decoded := &Message{}
	if err := proto.Unmarshal(encoded, decoded); err != nil {
		t.Fatalf("proto.Unmarshal() error = %v", err)
	}
	got, err := FromWireMessage(decoded)
	if err != nil {
		t.Fatalf("FromWireMessage() error = %v", err)
	}
	if got.Content != want.Content || !reflect.DeepEqual(got.Raw, want.Raw) || got.Data != nil {
		t.Fatalf("fallback fields changed: got Content=%q Raw=%q Data=%#v", got.Content, got.Raw, got.Data)
	}
}

// TestUnknownMessagePreservesBodyAndRaw 验证未知消息类型仍保留可展示正文和原始副本。
func TestUnknownMessagePreservesBodyAndRaw(t *testing.T) {
	now := time.Date(2026, 11, 6, 5, 10, 26, 0, time.UTC)
	want := &message.Message{
		ID:        111,
		Type:      message.TypeUnknown,
		Content:   "未识别正文",
		Raw:       []byte(`{"type":99,"content":{"value":"未识别正文"}}`),
		Timestamp: now,
	}

	wireValue, err := ToWireMessage(want)
	if err != nil {
		t.Fatalf("ToWireMessage() error = %v", err)
	}
	got, err := FromWireMessage(roundTripProto(t, wireValue, &Message{}))
	if err != nil {
		t.Fatalf("FromWireMessage() error = %v", err)
	}
	if got.Type != message.TypeUnknown || got.Content != want.Content || !reflect.DeepEqual(got.Raw, want.Raw) || got.Data != nil {
		t.Fatalf("unknown message changed: got Type=%v Content=%q Raw=%q Data=%#v", got.Type, got.Content, got.Raw, got.Data)
	}
}

// TestSendResultRoundTripPreservesRevokeFields 验证发送结果保留撤回所需真实字段。
func TestSendResultRoundTripPreservesRevokeFields(t *testing.T) {
	createdAt := time.Date(2026, 11, 6, 5, 10, 26, 0, time.UTC)
	serverTime := createdAt.Add(time.Second)
	want := &message.SendResult{
		ID:         100,
		NewID:      9007199254741009,
		ClientID:   "1692038969",
		Sender:     contact.Contact{Username: "wxid_fixture_self", Kind: contact.KindSelf},
		Receiver:   contact.Contact{Username: "wxid_fixture_peer", Kind: contact.KindPersonal},
		Type:       message.TypeText,
		CreatedAt:  &createdAt,
		ServerTime: &serverTime,
		Outcome:    sdk.OutcomeSucceeded,
	}

	wireValue, err := ToWireSendResult(want)
	if err != nil {
		t.Fatalf("ToWireSendResult() error = %v", err)
	}
	encoded, err := proto.Marshal(wireValue)
	if err != nil {
		t.Fatalf("proto.Marshal() error = %v", err)
	}
	decoded := &SendResult{}
	if err := proto.Unmarshal(encoded, decoded); err != nil {
		t.Fatalf("proto.Unmarshal() error = %v", err)
	}
	got, err := FromWireSendResult(decoded)
	if err != nil {
		t.Fatalf("FromWireSendResult() error = %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("send result changed: got %+v want %+v", got, want)
	}
	if !got.Revocable() {
		t.Fatal("complete send result must be revocable")
	}

	withoutTime := &message.SendResult{
		NewID:    1,
		ClientID: "1",
		Receiver: contact.Contact{Username: "wxid_fixture_peer"},
		Outcome:  sdk.OutcomeSucceeded,
	}
	if withoutTime.Revocable() {
		t.Fatal("send result without source time must not be marked revocable")
	}

	nonNumericClientID := *want
	nonNumericClientID.ClientID = "fixture-string-client-id"
	if nonNumericClientID.Revocable() {
		t.Fatal("send result with non-numeric client ID must not be marked revocable")
	}

	zeroClientID := *want
	zeroClientID.ClientID = "0"
	if zeroClientID.Revocable() {
		t.Fatal("send result with zero client ID must not be marked revocable")
	}

	unknownOutcome := *want
	unknownOutcome.Outcome = sdk.OutcomeUnknown
	if unknownOutcome.Revocable() {
		t.Fatal("send result with unknown outcome must not be marked revocable")
	}

	zeroCreatedAt := *want
	zeroTime := time.Time{}
	zeroCreatedAt.CreatedAt = &zeroTime
	if zeroCreatedAt.Revocable() {
		t.Fatal("send result with zero created_at must not be marked revocable")
	}

	outOfRangeCreatedAt := *want
	outOfRangeTime := time.Date(10000, time.January, 1, 0, 0, 0, 0, time.UTC)
	outOfRangeCreatedAt.CreatedAt = &outOfRangeTime
	if outOfRangeCreatedAt.Revocable() {
		t.Fatal("send result with timestamp outside the wire range must not be marked revocable")
	}
}

// TestGeneratedMessageContractShape 验证 oneof 和生成服务方法形状。
func TestGeneratedMessageContractShape(t *testing.T) {
	messageDescriptor := File_golem_sdk_model_proto.Messages().ByName("Message")
	if messageDescriptor == nil {
		t.Fatal("Message descriptor is missing")
	}
	oneofs := messageDescriptor.Oneofs()
	if oneofs.Len() != 1 || oneofs.Get(0).Name() != "data" {
		t.Fatalf("Message oneofs = %v, want [data]", oneofs)
	}
	wantDataFields := []string{"text", "image", "voice", "video", "emoji", "location", "app", "file"}
	var gotDataFields []string
	fields := messageDescriptor.Fields()
	for index := 0; index < fields.Len(); index++ {
		field := fields.Get(index)
		if field.ContainingOneof() != nil {
			gotDataFields = append(gotDataFields, string(field.Name()))
		}
	}
	if !slices.Equal(gotDataFields, wantDataFields) {
		t.Fatalf("Message data fields = %v, want %v", gotDataFields, wantDataFields)
	}

	tests := []struct {
		file    protoreflect.FileDescriptor
		service protoreflect.Name
		methods []protoreflect.Name
	}{
		{File_golem_sdk_plugin_proto, "PluginControlService", []protoreflect.Name{"Describe", "Initialize", "Load", "Start", "Stop", "Unload", "ValidateConfig", "SetConfig"}},
		{File_golem_sdk_plugin_proto, "PluginBusinessService", []protoreflect.Name{"HandleEvent", "HandleCommand", "HandleCapability"}},
		{File_golem_sdk_host_proto, "HostAccountService", []protoreflect.Name{"Current"}},
		{File_golem_sdk_host_proto, "HostMessagesService", []protoreflect.Name{"SendText", "SendApp", "SendMedia", "Forward", "Revoke", "Download"}},
		{File_golem_sdk_host_proto, "HostContactsService", []protoreflect.Name{"Find"}},
		{File_golem_sdk_host_proto, "HostChatroomsService", []protoreflect.Name{"Get", "Members"}},
		{File_golem_sdk_host_proto, "HostSessionsService", []protoreflect.Name{"Acquire", "Renew", "Release"}},
		{File_golem_sdk_host_proto, "HostCallerService", []protoreflect.Name{"Call"}},
		{File_golem_sdk_host_proto, "HostConfigService", []protoreflect.Name{"Read", "Save"}},
	}
	for _, test := range tests {
		service := test.file.Services().ByName(test.service)
		if service == nil {
			t.Fatalf("service %s is missing", test.service)
		}
		var got []protoreflect.Name
		for index := 0; index < service.Methods().Len(); index++ {
			got = append(got, service.Methods().Get(index).Name())
		}
		if !slices.Equal(got, test.methods) {
			t.Fatalf("%s methods = %v, want %v", test.service, got, test.methods)
		}
	}
}

// TestGeneratedContractHasNoLegacyFields 验证生成契约不再包含旧 token、revision 和账号代次。
func TestGeneratedContractHasNoLegacyFields(t *testing.T) {
	forbidden := map[protoreflect.Name]struct{}{
		"token":            {},
		"background_token": {},
		"media_token":      {},
		"forward_token":    {},
		"revoke_token":     {},
		"revision":         {},
		"config_revision":  {},
		"active_revision":  {},
		"apply_status":     {},
		"resume":           {},
		"drain":            {},
		"tenant":           {},
		"tenant_id":        {},
		"account_epoch":    {},
		"media_ref":        {},
		"forward_ref":      {},
		"revoke_ref":       {},
	}
	files := []protoreflect.FileDescriptor{
		File_golem_sdk_model_proto,
		File_golem_sdk_media_proto,
		File_golem_sdk_host_proto,
		File_golem_sdk_plugin_proto,
	}
	for _, file := range files {
		assertNoForbiddenMessageFields(t, file.Messages(), forbidden)
	}
}

func assertNoForbiddenMessageFields(t *testing.T, messages protoreflect.MessageDescriptors, forbidden map[protoreflect.Name]struct{}) {
	t.Helper()
	for index := 0; index < messages.Len(); index++ {
		messageDescriptor := messages.Get(index)
		fields := messageDescriptor.Fields()
		for fieldIndex := 0; fieldIndex < fields.Len(); fieldIndex++ {
			field := fields.Get(fieldIndex)
			if _, exists := forbidden[field.Name()]; exists {
				t.Fatalf("%s contains forbidden legacy field %s", messageDescriptor.FullName(), field.Name())
			}
		}
		assertNoForbiddenMessageFields(t, messageDescriptor.Messages(), forbidden)
	}
}
