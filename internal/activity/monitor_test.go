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

func TestMarkdownDeltaBytes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name              string
		previous, current string
		added, deleted    int
	}{
		{name: "unchanged", previous: "abc", current: "abc"},
		{name: "append", previous: "abc", current: "abcdef", added: 3},
		{name: "delete", previous: "abcdef", current: "abc", deleted: 3},
		{name: "replace", previous: "hello old world", current: "hello new text world", added: 8, deleted: 3},
		{name: "utf8", previous: "old", current: "old新", added: len("新")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			added, deleted := MarkdownDeltaBytes(test.previous, test.current)
			if added != test.added || deleted != test.deleted {
				t.Fatalf("MarkdownDeltaBytes() = (%d,%d), want (%d,%d)", added, deleted, test.added, test.deleted)
			}
		})
	}
}
