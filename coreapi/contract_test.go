package coreapi

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/golemwx/sdk"
)

// recordingCaller 记录宿主入口收到的稳定方法和原始请求。
type recordingCaller struct {
	response []byte
	err      error
	calls    int
	method   Method
	request  []byte
}

// Invoke 返回预设响应并保留最后一次调用。
func (c *recordingCaller) Invoke(_ context.Context, method Method, request []byte) ([]byte, error) {
	c.calls++
	c.method = method
	c.request = append([]byte(nil), request...)
	return append([]byte(nil), c.response...), c.err
}

// TestAllMethodsAreCompleteAndUnique 固定当前 core 业务页面的完整方法集合。
func TestAllMethodsAreCompleteAndUnique(t *testing.T) {
	methods := AllMethods()
	if len(methods) != 123 {
		t.Fatalf("method count=%d want 123", len(methods))
	}
	seen := make(map[Method]bool, len(methods))
	for _, method := range methods {
		if method == "" || seen[method] {
			t.Fatalf("method %q is empty or duplicated", method)
		}
		seen[method] = true
	}
	if !seen[LoginAccountStatus] {
		t.Fatal("login.account_status is missing")
	}
}

// TestInvokeRejectsUnknownMethod 验证 SDK 不会把未声明方法转发给宿主。
func TestInvokeRejectsUnknownMethod(t *testing.T) {
	caller := &recordingCaller{}
	client := New(caller)
	_, err := client.Invoke(context.Background(), Method("core.unknown"), []byte(`{}`))
	var value *sdk.Error
	if !errors.As(err, &value) || value.Category != sdk.CategoryNotFound || caller.calls != 0 {
		t.Fatalf("error=%v calls=%d", err, caller.calls)
	}
}

// TestURLRequestsKeepCoreWireFields 验证 URL 来源使用 core 当前字段且不伪造媒体引用。
func TestURLRequestsKeepCoreWireFields(t *testing.T) {
	refJSON, _ := json.Marshal(ResourceRef{
		Token: "opaque", Kind: "image", ExpiresAt: time.Now().UTC().Add(time.Minute),
	})
	caller := &recordingCaller{response: refJSON}
	client := New(caller)

	if _, err := client.Message.SendImageURL(context.Background(), "wxid_target", "https://example.com/a.png"); err != nil {
		t.Fatal(err)
	}
	requireWireFields(t, caller, MessageSendImage, "receiver", "url")

	if _, err := client.CDN.UploadImageURL(context.Background(), "https://example.com/a.png", "record"); err != nil {
		t.Fatal(err)
	}
	requireWireFields(t, caller, CDNUploadImage, "purpose", "url")

	if _, err := client.User.UploadAvatarURL(context.Background(), "https://example.com/avatar.png"); err != nil {
		t.Fatal(err)
	}
	requireWireFields(t, caller, UserUploadAvatar, "url")
}

// TestMomentsURLSourcesMatchCoreForm 验证纯 URL 发布和朋友圈上传结果可直接组合。
func TestMomentsURLSourcesMatchCoreForm(t *testing.T) {
	caller := &recordingCaller{response: []byte(`{}`)}
	client := New(caller)
	ctx := context.Background()

	if _, err := client.Moments.Post(ctx, MomentsPostRequest{
		Content:   "hello",
		ImagesURL: "https://example.com/a.jpg, https://example.com/b.jpg",
	}); err != nil {
		t.Fatal(err)
	}
	requireWireFields(t, caller, MomentsPost, "content", "images_url")

	caller.response = []byte(`{"file_url":"https://cdn.example.com/a.jpg","thumb_url":"https://cdn.example.com/a-thumb.jpg","file_key":"private","image_width":12}`)
	image, err := client.CDN.UploadMomentsImageURL(ctx, "https://example.com/a.jpg")
	if err != nil {
		t.Fatal(err)
	}
	if caller.method != CDNUploadMomentsImage || image.FileURL == "" || image.ImageWidth != 12 {
		t.Fatalf("method=%s result=%+v", caller.method, image)
	}
}

// TestMomentsPostRejectsConflictingURLMedia 验证纯 URL 模式与结构字段使用相同约束。
func TestMomentsPostRejectsConflictingURLMedia(t *testing.T) {
	caller := &recordingCaller{}
	client := New(caller)
	ctx := context.Background()
	cases := []MomentsPostRequest{
		{ImagesURL: "https://example.com/a.jpg", VideoURL: "https://example.com/a.mp4"},
		{CoverURL: "https://example.com/cover.jpg"},
		{ImagesURL: "https://example.com/a.jpg,https://example.com/b.jpg,https://example.com/c.jpg,https://example.com/d.jpg,https://example.com/e.jpg,https://example.com/f.jpg,https://example.com/g.jpg,https://example.com/h.jpg,https://example.com/i.jpg,https://example.com/j.jpg"},
	}
	for _, request := range cases {
		if _, err := client.Moments.Post(ctx, request); err == nil {
			t.Fatalf("request %+v unexpectedly accepted", request)
		}
	}
	if caller.calls != 0 {
		t.Fatalf("invalid requests reached host: %d", caller.calls)
	}
}

// TestAccountStatusUsesAccountEndpointResult 验证账号状态不会被误当成扫码会话状态。
func TestAccountStatusUsesAccountEndpointResult(t *testing.T) {
	caller := &recordingCaller{response: []byte(`{"user":{"username":"bot","nickname":"Bot"},"status":"在线","login":"2026-10-30 12:00:00","refresh":"2026-10-30 12:01:00"}`)}
	state, err := New(caller).Login.AccountStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if caller.method != LoginAccountStatus || state.User.Username != "bot" || state.Status != "在线" {
		t.Fatalf("method=%q state=%+v", caller.method, state)
	}
}

// TestMiniappAvatarResultsPreserveFileKey 验证头像上传链路可取得后续设置头像所需的标识。
func TestMiniappAvatarResultsPreserveFileKey(t *testing.T) {
	caller := &recordingCaller{response: []byte(`{"nickname":"Bot","avatar_url":"https://cdn.example.com/avatar.png","file_key":"avatar-key"}`)}
	client := New(caller)
	random, err := client.Miniapp.RandomAvatar(context.Background(), "wx_app")
	if err != nil {
		t.Fatal(err)
	}
	if caller.method != MiniappRandomAvatar || random.FileKey != "avatar-key" || random.AvatarURL == "" {
		t.Fatalf("method=%s result=%+v", caller.method, random)
	}

	caller.response = []byte(`{"file_key":"uploaded-key","cdn_url":"https://cdn.example.com/avatar.jpg"}`)
	uploaded, err := client.Miniapp.UploadAvatar(context.Background(), MiniappUploadAvatarRequest{
		AppID: "wx_app", ImageURL: "https://example.com/avatar.jpg",
	})
	if err != nil {
		t.Fatal(err)
	}
	if caller.method != MiniappUploadAvatar || uploaded.FileKey != "uploaded-key" || uploaded.CDNURL == "" {
		t.Fatalf("method=%s result=%+v", caller.method, uploaded)
	}
}

// TestInputValidationRejectsAmbiguousOrUnsafeFields 覆盖常用必填字段和 URL 来源。
func TestInputValidationRejectsAmbiguousOrUnsafeFields(t *testing.T) {
	caller := &recordingCaller{}
	client := New(caller)
	ctx := context.Background()

	cases := []struct {
		name string
		call func() error
	}{
		{"link-desc", func() error {
			_, err := client.Message.SendLink(ctx, MessageLinkRequest{Receiver: "wxid", Title: "title", URL: "https://example.com"})
			return err
		}},
		{"name-card-alias", func() error {
			_, err := client.Message.SendNameCard(ctx, MessageNameCardRequest{Receiver: "wxid", Username: "card", Nickname: "Card"})
			return err
		}},
		{"contact-scene", func() error {
			_, err := client.Contact.SendRequest(ctx, ContactFriendRequest{V1: "v1", V2: "v2"})
			return err
		}},
		{"blank-chatroom-member", func() error {
			_, err := client.Chatroom.AddMembers(ctx, "room@chatroom", []string{" "})
			return err
		}},
		{"unsafe-image-url", func() error {
			_, err := client.Message.SendImageURL(ctx, "wxid", "file:///private")
			return err
		}},
		{"unsafe-avatar-url", func() error {
			_, err := client.User.UploadAvatarURL(ctx, "not-a-url")
			return err
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var value *sdk.Error
			err := tc.call()
			if !errors.As(err, &value) || value.Category != sdk.CategoryInvalidArgument {
				t.Fatalf("error=%v", err)
			}
		})
	}
	if caller.calls != 0 {
		t.Fatalf("invalid requests reached host: %d", caller.calls)
	}
}

// requireWireFields 验证编码后的请求字段集合完全匹配。
func requireWireFields(t *testing.T, caller *recordingCaller, method Method, fields ...string) {
	t.Helper()
	if caller.method != method {
		t.Fatalf("method=%q want %q", caller.method, method)
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(caller.request, &body); err != nil {
		t.Fatal(err)
	}
	if len(body) != len(fields) {
		t.Fatalf("body=%s want fields %v", caller.request, fields)
	}
	for _, field := range fields {
		if _, ok := body[field]; !ok {
			t.Fatalf("body=%s missing %q", caller.request, field)
		}
	}
}
