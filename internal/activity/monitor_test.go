package activity

import "testing"

func TestAddedMarkdownBytes(t *testing.T) {
	tests := []struct {
		name     string
		previous string
		current  string
		want     int
	}{
		{name: "unchanged", previous: "abc", current: "abc", want: 0},
		{name: "append", previous: "abc", current: "abcdef", want: 3},
		{name: "delete", previous: "abcdef", current: "abc", want: 0},
		{name: "replace", previous: "hello old world", current: "hello new text world", want: len("new text")},
		{name: "unicode bytes", previous: "旧内容", current: "旧内容新增", want: len("新增")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := AddedMarkdownBytes(test.previous, test.current); got != test.want {
				t.Fatalf("AddedMarkdownBytes() = %d, want %d", got, test.want)
			}
		})
	}
}
