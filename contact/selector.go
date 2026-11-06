package contact

import (
	"fmt"
	"strings"

	"github.com/golemwx/sdk"
)

// SelectorMode 是联系人策略选择器的检索字段。
type SelectorMode uint8

const (
	// SelectorModeUnknown 表示选择器字段未知。
	SelectorModeUnknown SelectorMode = iota
	// SelectorModeUsername 表示按唯一 username 精确匹配。
	SelectorModeUsername
	// SelectorModeNickname 表示按 nickname 精确匹配。
	SelectorModeNickname
	// SelectorModeRemark 表示按 remark 精确匹配。
	SelectorModeRemark
)

// Selector 是配置、命令和宿主查询共用的联系人选择器原文。
//
// 无前缀或 username:: 按 username 查询；nickname:: 和 remark:: 按对应字段
// 精确查询。空值、空检索值或未知前缀返回 InvalidArgument。
type Selector string

// ParseSelector 校验选择器原文。
func ParseSelector(value string) (Selector, error) {
	selector := Selector(value)
	if _, _, err := selector.Split(); err != nil {
		return "", err
	}
	return selector, nil
}

// Split 返回选择器的检索字段和值。
func (s Selector) Split() (SelectorMode, string, error) {
	raw := string(s)
	if raw == "" {
		return SelectorModeUnknown, "", sdk.NewError(sdk.CategoryInvalidArgument, "contact.selector", "selector must not be empty")
	}

	mode := SelectorModeUsername
	value := raw
	if prefix, rest, found := strings.Cut(raw, "::"); found {
		value = rest
		switch prefix {
		case "username":
			mode = SelectorModeUsername
		case "nickname":
			mode = SelectorModeNickname
		case "remark":
			mode = SelectorModeRemark
		default:
			return SelectorModeUnknown, "", sdk.NewError(
				sdk.CategoryInvalidArgument,
				"contact.selector",
				fmt.Sprintf("unknown selector prefix %q", prefix),
			)
		}
	}
	if value == "" {
		return SelectorModeUnknown, "", sdk.NewError(sdk.CategoryInvalidArgument, "contact.selector", "selector value must not be empty")
	}
	return mode, value, nil
}

// String 返回保留原始写法的选择器。
func (s Selector) String() string {
	return string(s)
}

// MarshalText 实现 encoding.TextMarshaler，供 TOML 和命令系统使用。
func (s Selector) MarshalText() ([]byte, error) {
	if _, _, err := s.Split(); err != nil {
		return nil, err
	}
	return []byte(s), nil
}

// UnmarshalText 实现 encoding.TextUnmarshaler，并在解码时校验选择器。
func (s *Selector) UnmarshalText(text []byte) error {
	if s == nil {
		return sdk.NewError(sdk.CategoryInvalidArgument, "contact.selector", "selector receiver must not be nil")
	}
	value, err := ParseSelector(string(text))
	if err != nil {
		return err
	}
	*s = value
	return nil
}
