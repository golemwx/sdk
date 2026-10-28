package transport

import (
	"math"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/golemwx/sdk"
	"github.com/golemwx/sdk/message"
)

const (
	maxMessageTitleBytes = 512
	maxMessageTextBytes  = 4096
	maxMessageURLBytes   = 4096
	maxUsernameBytes     = 256
)

// ValidateForwardRef 校验转发引用的公开形状和期限
func ValidateForwardRef(ref sdk.ForwardRef) error {
	if strings.TrimSpace(ref.Token) == "" || ref.ExpiresAt.IsZero() {
		return messageInputError("messages.forward", "invalid forward reference")
	}
	return nil
}

// ValidateRevokeRef 校验撤回引用的公开形状
func ValidateRevokeRef(ref sdk.RevokeRef) error {
	if strings.TrimSpace(ref.Token) == "" || ref.ExpiresAt.IsZero() {
		return messageInputError("messages.revoke", "invalid revoke reference")
	}
	return nil
}

// ValidateLink 校验结构化链接，不允许借链接字段触发宿主远程取文件
func ValidateLink(value message.Link) error {
	if !validText(value.Title, maxMessageTitleBytes, true) ||
		!validText(value.Description, maxMessageTextBytes, false) ||
		len(value.URL) == 0 || len(value.URL) > maxMessageURLBytes {
		return messageInputError("messages.send_link", "invalid link")
	}
	target, err := url.Parse(value.URL)
	if err != nil || target.User != nil || target.Host == "" ||
		target.Scheme != "http" && target.Scheme != "https" {
		return messageInputError("messages.send_link", "invalid link URL")
	}
	return nil
}

// ValidateLocation 校验结构化经纬度和有限缩放级别
func ValidateLocation(value message.Location) error {
	if !validText(value.Label, maxMessageTextBytes, true) ||
		!validText(value.POIName, maxMessageTitleBytes, false) ||
		math.IsNaN(value.Longitude) || math.IsInf(value.Longitude, 0) ||
		math.IsNaN(value.Latitude) || math.IsInf(value.Latitude, 0) ||
		math.IsNaN(value.Scale) || math.IsInf(value.Scale, 0) ||
		value.Longitude < -180 || value.Longitude > 180 ||
		value.Latitude < -90 || value.Latitude > 90 || value.Scale <= 0 || value.Scale > 30 {
		return messageInputError("messages.send_location", "invalid location")
	}
	return nil
}

// ValidateNameCard 校验名片业务字段，不接收 XML 或任意协议属性
func ValidateNameCard(value message.NameCard) error {
	if !validUsername(value.Username) ||
		!validText(value.Nickname, maxMessageTitleBytes, true) ||
		!validText(value.Alias, maxMessageTitleBytes, false) {
		return messageInputError("messages.send_name_card", "invalid name card")
	}
	return nil
}

// validUsername 只检查公共 username 的安全形状，不解析其业务前缀
func validUsername(value string) bool {
	return validText(value, maxUsernameBytes, true) &&
		!strings.ContainsAny(value, "<>&\"' ,:\t\r\n")
}

// validText 校验有界 UTF-8 文本并拒绝控制字符
func validText(value string, limit int, required bool) bool {
	if required && strings.TrimSpace(value) == "" {
		return false
	}
	if len(value) > limit || !utf8.ValidString(value) {
		return false
	}
	return !strings.ContainsFunc(value, unicode.IsControl)
}

// messageInputError 标记消息请求尚未提交
func messageInputError(operation, reason string) *sdk.Error {
	value := sdk.NewError(sdk.CategoryInvalidArgument, operation, reason)
	value.Outcome = sdk.OutcomeFailed
	return value
}
