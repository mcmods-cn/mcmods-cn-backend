package httpapi

import (
	"strings"
	"testing"
)

func TestHumanReadableOSSObjectKeys(t *testing.T) {
	t.Run("export media keeps asset path", func(t *testing.T) {
		got := modExportMediaObjectKey("mcmods", "m123abc", "revision-42", "assets/create/textures/block/brass.png")
		want := "mcmods/projects/m123abc/datasets/revision-42/media/assets/create/textures/block/brass.png"
		if got != want {
			t.Fatalf("unexpected exporter media key: got %q want %q", got, want)
		}
	})

	t.Run("imported icon has no hash shard directory", func(t *testing.T) {
		got := modIconObjectKey("mcmods", "m123abc", strings.Repeat("a", 64), ".png")
		want := "mcmods/projects/m123abc/branding/icons/icon-aaaaaaaaaaaaaaaa.png"
		if got != want {
			t.Fatalf("unexpected icon key: got %q want %q", got, want)
		}
	})

	t.Run("user category is readable", func(t *testing.T) {
		if got := normalizeOSSUserCategory("comment", "comment"); got != "users/comments" {
			t.Fatalf("unexpected comment category: %q", got)
		}
		key := buildOSSObjectKey("mcmods", "users/12/playground")
		if !strings.HasPrefix(key, "mcmods/users/12/playground/") {
			t.Fatalf("unexpected user object key: %q", key)
		}
	})
}
