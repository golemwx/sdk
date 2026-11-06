package command

import (
	"strings"

	"github.com/golemwx/sdk"
)

// Lex 把一条命令文本拆成 frame 与插件共用的词法单元。
//
// 输入可以带一个前导斜杠；空白分隔参数，单双引号保留空白，反斜杠转义
// 下一个字符。返回结果保留空参数，便于 Kong 正确处理显式空字符串。
func Lex(input string) ([]string, error) {
	tokens := make([]string, 0, 8)
	var current strings.Builder
	started := false
	var quote rune
	escaped := false

	for _, character := range input {
		if escaped {
			current.WriteRune(character)
			started = true
			escaped = false
			continue
		}
		if character == '\\' {
			escaped = true
			started = true
			continue
		}
		if quote != 0 {
			if character == quote {
				quote = 0
				started = true
				continue
			}
			current.WriteRune(character)
			started = true
			continue
		}
		switch character {
		case '\'', '"':
			quote = character
			started = true
		case ' ', '\t', '\r', '\n':
			if started {
				tokens = append(tokens, current.String())
				current.Reset()
				started = false
			}
		default:
			current.WriteRune(character)
			started = true
		}
	}
	if escaped {
		return nil, sdk.NewError(sdk.CategoryInvalidArgument, "command.lex", "命令末尾存在未完成的转义")
	}
	if quote != 0 {
		return nil, sdk.NewError(sdk.CategoryInvalidArgument, "command.lex", "命令包含未闭合的引号")
	}
	if started {
		tokens = append(tokens, current.String())
	}
	if len(tokens) > 0 {
		tokens[0] = strings.TrimPrefix(tokens[0], "/")
	}
	return tokens, nil
}
