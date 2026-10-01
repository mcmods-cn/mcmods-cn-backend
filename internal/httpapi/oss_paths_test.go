package httpapi

import (
	"path"
	"regexp"
	"strings"
	"testing"
)

func TestHumanReadableOSSObjectKeys(t *testing.T) {
	t.Run("export media keeps asset path", func(t *testing.T) {
		got := modExportMediaObjectKey("mcmods", "m123abc", "revision-42", "assets/create/textures/block/brass.png")
		want := "mcmods/project/mods/m123abc/models/revision-42/assets/create/textures/block/brass.png"
		if got != want {
			t.Fatalf("unexpected exporter media key: got %q want %q", got, want)
		}
	})

	t.Run("imported icon has no hash shard directory", func(t *testing.T) {
		got := externalImageObjectKey(
			"mcmods",
			ossProjectCategory("mod", "m123abc", "icons", "project", "original"),
			"icon", strings.Repeat("a", 64), ".png",
		)
		want := "mcmods/project/mods/m123abc/icons/project/original/icon-aaaaaaaaaaaaaaaa.png"
		if got != want {
			t.Fatalf("unexpected icon key: got %q want %q", got, want)
		}
	})

	t.Run("user category is readable", func(t *testing.T) {
		if got := normalizeOSSUserFileScope("comment", "comment"); got != "comments" {
			t.Fatalf("unexpected comment scope: %q", got)
		}
		key := buildOSSObjectKeyForFile("mcmods", ossUserCategory(12, "playground"), "preview.png")
		if !strings.HasPrefix(key, "mcmods/user/12/files/playground/") || !strings.HasSuffix(key, ".png") {
			t.Fatalf("unexpected user object key: %q", key)
		}
	})

	t.Run("project release and blueprint paths are isolated", func(t *testing.T) {
		if got, want := ossObjectPrefix("mcmods", ossProjectReleaseCategory("mod", "abc234567")),
			"mcmods/project/mods/abc234567/files/releases"; got != want {
			t.Fatalf("unexpected project release prefix: got %q want %q", got, want)
		}
		if got, want := ossObjectPrefix("mcmods", ossBlueprintTextCategory("bcd345678", "cover")),
			"mcmods/project/blueprints/bcd345678/files/text/bcd345678/cover"; got != want {
			t.Fatalf("unexpected blueprint text prefix: got %q want %q", got, want)
		}
	})

	t.Run("resource icon is named by its public id", func(t *testing.T) {
		got := modExportResolvedMediaObjectKey(
			"mcmods",
			"abc234567",
			"revision-42",
			"icons/items/128/minecraft/stick.png",
			catalogResourceIdentityResolver{},
		)
		pattern := regexp.MustCompile(`^mcmods/project/mods/abc234567/icons/items/128/revision-42/[a-z2-9]{9}\.png$`)
		if !pattern.MatchString(got) {
			t.Fatalf("unexpected resource icon key: %q", got)
		}
	})

	t.Run("import asset path cannot escape its project directory", func(t *testing.T) {
		got := modExportMediaObjectKey("mcmods", "abc234567", "revision-42", `..\..\secrets\AccessKey.txt`)
		expectedPrefix := "mcmods/project/mods/abc234567/assets/revision-42/"
		if !strings.HasPrefix(got, expectedPrefix) || strings.Contains(path.Clean(strings.TrimPrefix(got, expectedPrefix)), "..") {
			t.Fatalf("unsafe imported asset key: %q", got)
		}
	})

	t.Run("download scope keeps project kind and id", func(t *testing.T) {
		scope := ossProjectDownloadScope("shader", "shd234567")
		projectKind, publicID, ok := parseOSSProjectDownloadScope(scope)
		if !ok || projectKind != "shader_pack" || publicID != "shd234567" {
			t.Fatalf("unexpected project download scope: %q, %q, %q, %v", scope, projectKind, publicID, ok)
		}
	})
}
