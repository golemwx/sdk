package contact

import (
	"errors"
	"testing"

	"github.com/golemwx/sdk"
)

// TestSelectorModes 验证无前缀和三种显式前缀的策略解析。
func TestSelectorModes(t *testing.T) {
	tests := []struct {
		name  string
		input string
		mode  SelectorMode
		value string
	}{
		{name: "implicit username", input: "wxid_fixture", mode: SelectorModeUsername, value: "wxid_fixture"},
		{name: "explicit username", input: "username::wxid_fixture", mode: SelectorModeUsername, value: "wxid_fixture"},
		{name: "nickname", input: "nickname::小明", mode: SelectorModeNickname, value: "小明"},
		{name: "remark", input: "remark::owner", mode: SelectorModeRemark, value: "owner"},
		{name: "split at first separator", input: "nickname::a::b", mode: SelectorModeNickname, value: "a::b"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mode, value, err := Selector(test.input).Split()
			if err != nil {
				t.Fatalf("Split() error = %v", err)
			}
			if mode != test.mode || value != test.value {
				t.Fatalf("Split() = (%v, %q), want (%v, %q)", mode, value, test.mode, test.value)
			}
		})
	}
}

// TestSelectorRejectsInvalidInput 验证空值和未知前缀返回 InvalidArgument。
func TestSelectorRejectsInvalidInput(t *testing.T) {
	for _, input := range []string{"", "nickname::", "unknown::value", "::value"} {
		t.Run(input, func(t *testing.T) {
			_, err := ParseSelector(input)
			var business *sdk.Error
			if !errors.As(err, &business) || business.Category != sdk.CategoryInvalidArgument {
				t.Fatalf("ParseSelector(%q) error = %v, want InvalidArgument", input, err)
			}
		})
	}
}

// TestSelectorTextRoundTrip 验证 TOML 和命令系统使用的文本编解码。
func TestSelectorTextRoundTrip(t *testing.T) {
	var selector Selector
	if err := selector.UnmarshalText([]byte("remark::owner")); err != nil {
		t.Fatalf("UnmarshalText() error = %v", err)
	}
	text, err := selector.MarshalText()
	if err != nil {
		t.Fatalf("MarshalText() error = %v", err)
	}
	if string(text) != "remark::owner" {
		t.Fatalf("MarshalText() = %q", text)
	}
}
