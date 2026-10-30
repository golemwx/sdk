package coreapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/golemwx/sdk"
)

const (
	// MaxRequestBytes 限制一次扩展调用可携带的 JSON 请求。
	MaxRequestBytes = 4 << 20
	// MaxResponseBytes 预留公众号正文等当前 core 有界返回值。
	MaxResponseBytes = 16 << 20
	// MaxInlineJSONBytes 限制小程序等调用方直接提交的协议 JSON。
	MaxInlineJSONBytes = 256 << 10
)

// Caller 是宿主实现的白名单调用入口。
type Caller interface {
	Invoke(context.Context, Method, []byte) ([]byte, error)
}

// Client 是插件可见的类型化 core 客户端集合。
type Client struct {
	caller Caller

	Login    *LoginClient
	Contact  *ContactClient
	Chatroom *ChatroomClient
	Message  *MessageClient
	Favorite *FavoriteClient
	CDN      *CDNClient
	Label    *LabelClient
	Miniapp  *MiniappClient
	Moments  *MomentsClient
	Official *OfficialClient
	Payment  *PaymentClient
	User     *UserClient
}

// New 使用宿主白名单入口构造类型化客户端集合。
func New(caller Caller) *Client {
	client := &Client{caller: caller}
	client.Login = (*LoginClient)(client)
	client.Contact = (*ContactClient)(client)
	client.Chatroom = (*ChatroomClient)(client)
	client.Message = (*MessageClient)(client)
	client.Favorite = (*FavoriteClient)(client)
	client.CDN = (*CDNClient)(client)
	client.Label = (*LabelClient)(client)
	client.Miniapp = (*MiniappClient)(client)
	client.Moments = (*MomentsClient)(client)
	client.Official = (*OfficialClient)(client)
	client.Payment = (*PaymentClient)(client)
	client.User = (*UserClient)(client)
	return client
}

// Invoke 仅供宿主 SDK 控制面转发已声明的方法，插件优先使用类型化子客户端。
func (c *Client) Invoke(ctx context.Context, method Method, request []byte) ([]byte, error) {
	if c == nil || c.caller == nil {
		return nil, sdk.NewError(sdk.CategoryFailedPrecondition, "core.invoke", "core client is not configured")
	}
	if !knownMethod(method) {
		return nil, sdk.NewError(sdk.CategoryNotFound, "core.invoke", "unknown core method")
	}
	if len(request) > MaxRequestBytes {
		return nil, sdk.NewError(sdk.CategoryResourceExhausted, "core.invoke", "core request is too large")
	}
	if len(request) != 0 && !json.Valid(request) {
		return nil, sdk.NewError(sdk.CategoryInvalidArgument, "core.invoke", "core request must be valid JSON")
	}
	result, err := c.caller.Invoke(ctx, method, request)
	if err != nil {
		return nil, err
	}
	if len(result) > MaxResponseBytes {
		return nil, sdk.NewError(sdk.CategoryResourceExhausted, "core.invoke", "core response is too large")
	}
	return result, nil
}

func knownMethod(method Method) bool {
	for _, candidate := range AllMethods() {
		if method == candidate {
			return true
		}
	}
	return false
}

func (c *Client) call(ctx context.Context, method Method, request any) (json.RawMessage, error) {
	payload, err := marshalRequest(method, request)
	if err != nil {
		return nil, err
	}
	result, err := c.Invoke(ctx, method, payload)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(result), nil
}

func (c *Client) callVoid(ctx context.Context, method Method, request any) error {
	_, err := c.call(ctx, method, request)
	return err
}

func marshalRequest(method Method, request any) ([]byte, error) {
	if request == nil {
		return nil, nil
	}
	payload, err := json.Marshal(request)
	if err != nil {
		return nil, sdk.NewError(sdk.CategoryInvalidArgument, string(method), "request cannot be encoded")
	}
	if len(payload) > MaxRequestBytes {
		return nil, sdk.NewError(sdk.CategoryResourceExhausted, string(method), "request is too large")
	}
	return payload, nil
}

func decodeResult[T any](method Method, data json.RawMessage) (T, error) {
	var result T
	if len(bytes.TrimSpace(data)) == 0 || bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return result, sdk.NewError(sdk.CategoryInternal, string(method), "core returned no result")
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return result, sdk.NewError(sdk.CategoryInternal, string(method), "core returned an invalid result")
	}
	return result, nil
}

func inlineJSON(name string, data json.RawMessage, required bool) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		if required {
			return sdk.NewError(sdk.CategoryInvalidArgument, name, "JSON payload is required")
		}
		return nil
	}
	if len(trimmed) > MaxInlineJSONBytes || !json.Valid(trimmed) {
		return sdk.NewError(sdk.CategoryInvalidArgument, name, "JSON payload is invalid or too large")
	}
	return nil
}

func required(name, value string) error {
	if value == "" {
		return sdk.NewError(sdk.CategoryInvalidArgument, name, "required value is missing")
	}
	return nil
}

func maxString(name, value string, limit int) error {
	if len(value) > limit {
		return sdk.NewError(sdk.CategoryResourceExhausted, name, "value is too large")
	}
	return nil
}

func containsBlank(values []string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			return true
		}
	}
	return false
}

func validHTTPURL(name, value string) error {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Hostname() == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return sdk.NewError(sdk.CategoryInvalidArgument, name, "URL must be a valid HTTP or HTTPS address")
	}
	return nil
}

func operation(method Method) string { return string(method) }

func unexpected(name string) error {
	return sdk.NewError(sdk.CategoryInternal, name, fmt.Sprintf("unexpected core client state"))
}
