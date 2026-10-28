package transport

import (
	"errors"
	"math"
	"testing"
	"time"

	"github.com/golemwx/sdk"
	"github.com/golemwx/sdk/message"
)

// TestValidateBusinessMessages 验证结构化消息不接受原始协议和异常数值
func TestValidateBusinessMessages(t *testing.T) {
	if err := ValidateLink(message.Link{Title: "标题", URL: "https://example.invalid/path"}); err != nil {
		t.Fatal(err)
	}
	for _, value := range []message.Link{
		{Title: "", URL: "https://example.invalid"},
		{Title: "x", URL: "file:///tmp/secret"},
		{Title: "x", URL: "https://user@example.invalid"},
		{Title: "x", URL: "javascript:alert(1)"},
	} {
		if err := ValidateLink(value); !failedInput(err) {
			t.Fatalf("非法链接被接受: %+v err=%v", value, err)
		}
	}
	if err := ValidateLocation(message.Location{Label: "地点", Longitude: 120, Latitude: 30, Scale: 16}); err != nil {
		t.Fatal(err)
	}
	for _, value := range []message.Location{
		{Label: "", Scale: 1},
		{Label: "x", Longitude: 181, Scale: 1},
		{Label: "x", Latitude: -91, Scale: 1},
		{Label: "x", Scale: math.NaN()},
	} {
		if err := ValidateLocation(value); !failedInput(err) {
			t.Fatalf("非法位置被接受: %+v err=%v", value, err)
		}
	}
	if err := ValidateNameCard(message.NameCard{Username: "wxid_fixture", Nickname: "示例"}); err != nil {
		t.Fatal(err)
	}
	if err := ValidateNameCard(message.NameCard{Username: "<msg>", Nickname: "x"}); !failedInput(err) {
		t.Fatalf("协议形状名片被接受: %v", err)
	}
}

// TestValidateOpaqueMessageRefs 验证空引用在发起 RPC 前明确拒绝
func TestValidateOpaqueMessageRefs(t *testing.T) {
	if err := ValidateForwardRef(sdk.ForwardRef{Token: "forward", ExpiresAt: time.Now().Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}
	if err := ValidateRevokeRef(sdk.RevokeRef{Token: "revoke", ExpiresAt: time.Now().Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}
	for _, err := range []error{
		ValidateForwardRef(sdk.ForwardRef{}),
		ValidateRevokeRef(sdk.RevokeRef{}),
	} {
		if !failedInput(err) {
			t.Fatalf("空引用未拒绝: %v", err)
		}
	}
}

// failedInput 判断错误为提交前确定失败
func failedInput(err error) bool {
	var value *sdk.Error
	return errors.As(err, &value) && value.Category == sdk.CategoryInvalidArgument &&
		value.Outcome == sdk.OutcomeFailed
}
