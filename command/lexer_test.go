package command

import (
	"reflect"
	"testing"
)

// TestLexQuotesEscapesAndWhitespace 验证 frame 与插件共用的词法规则。
func TestLexQuotesEscapesAndWhitespace(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{
			name:  "quotes and escapes",
			input: `/echo "hello world" 'single quoted' plain\ value`,
			want:  []string{"echo", "hello world", "single quoted", "plain value"},
		},
		{
			name:  "escaped quotes",
			input: `/echo "a \"b\" c"`,
			want:  []string{"echo", `a "b" c`},
		},
		{
			name:  "empty quoted value",
			input: `/echo "" tail`,
			want:  []string{"echo", "", "tail"},
		},
		{
			name:  "quoted flag value",
			input: `/echo --target="room one"`,
			want:  []string{"echo", "--target=room one"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := Lex(test.input)
			if err != nil {
				t.Fatalf("Lex() error = %v", err)
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("Lex() = %#v, want %#v", got, test.want)
			}
		})
	}
}

// TestLexRejectsIncompleteInput 验证未闭合引号和缺失转义字符不会被静默接受。
func TestLexRejectsIncompleteInput(t *testing.T) {
	for _, input := range []string{`/echo "unfinished`, `/echo trailing\`} {
		if _, err := Lex(input); err == nil {
			t.Fatalf("Lex(%q) unexpectedly succeeded", input)
		}
	}
}
