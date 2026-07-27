package httpapi

import (
	"bytes"
	"encoding/binary"
	"reflect"
	"testing"
	"time"

	"mcmods-cn-backend/internal/domain"
)

func TestOSSFileRecordExposesConversionDetails(t *testing.T) {
	record := ossFileRecord(
		"file23456", "bucket", "endpoint", "region", "mcmods/user/1/files/playground/id.webp", "user/1/files/playground", "playground",
		"image.webp", "image.png", "image/webp", 70, 158, "hash", "active", "pending", time.Time{}, time.Time{},
	)
	if record["converted"] != true {
		t.Fatalf("converted = %#v, want true", record["converted"])
	}
	if record["sourceSizeBytes"] != int64(158) || record["sizeBytes"] != int64(70) {
		t.Fatalf("unexpected conversion sizes: %#v", record)
	}
}

func TestMatchPermissionCatalogUsesVariableTemplate(t *testing.T) {
	catalog := []domain.Permission{
		{Code: "project.edit.<ProjectID>", Name: "Edit project"},
		{Code: "user.file.daily_limit.<num>", Name: "Daily upload limit"},
	}
	for code, expected := range map[string]string{
		"project.edit.112345":       "Edit project",
		"project.edit.*":            "Edit project",
		"user.file.daily_limit.150": "Daily upload limit",
	} {
		permission := matchPermissionCatalog(code, catalog)
		if permission == nil || permission.Name != expected {
			t.Fatalf("matchPermissionCatalog(%q) = %#v, want %q", code, permission, expected)
		}
	}
}

func TestResolvedPermissionAllowsDirectDenyOverridesRoleGrant(t *testing.T) {
	entries := []effectivePermission{
		{Code: "user.message.receive", Allow: true, Priority: 10, Source: "group.member"},
		{Code: "user.message.receive", Allow: false, Priority: directUserPermissionPriority, Source: "user"},
	}
	if resolvedPermissionAllows(entries, "user.message.receive") {
		t.Fatal("expected direct deny to override role grant")
	}
}

func TestReadAPNGAnimationControl(t *testing.T) {
	png := bytes.NewBuffer([]byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a})
	for _, chunkType := range []string{"IHDR", "acTL"} {
		_ = binary.Write(png, binary.BigEndian, uint32(0))
		png.WriteString(chunkType)
		png.Write(make([]byte, 4))
	}
	animated, err := readAPNGAnimationControl(png)
	if err != nil {
		t.Fatalf("readAPNGAnimationControl() error = %v", err)
	}
	if !animated {
		t.Fatal("expected acTL chunk to identify an animated PNG")
	}
}

func TestShouldPersistMarkdownImageAsWebP(t *testing.T) {
	tests := []struct {
		name        string
		original    string
		contentType string
		source      string
		category    string
		want        bool
	}{
		{name: "playground png", original: "image.png", contentType: "image/png", source: "playground", category: "user/1/files/playground", want: true},
		{name: "comment jpeg", original: "photo.jpg", contentType: "image/jpeg", source: "comment", category: "user/1/files/comments", want: true},
		{name: "project text attachment", original: "cover.jpeg", contentType: "image/jpeg", category: "project/mods/abc234567/files/text/def345678", want: true},
		{name: "legacy project description", original: "legacy.png", contentType: "image/png", category: "projects/42/description", want: true},
		{name: "avatar remains original", original: "avatar.png", contentType: "image/png", source: "avatar", category: "user/1/files/avatars", want: false},
		{name: "webp remains original", original: "image.webp", contentType: "image/webp", source: "playground", category: "user/1/files/playground", want: false},
		{name: "non image remains original", original: "notes.png", contentType: "text/plain", source: "playground", category: "user/1/files/playground", want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := shouldPersistMarkdownImageAsWebP(test.original, test.contentType, test.source, test.category); got != test.want {
				t.Fatalf("shouldPersistMarkdownImageAsWebP() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestMergePermissionComparisonRows(t *testing.T) {
	left := []effectivePermission{{Code: "a.read", Allow: true}, {Code: "shared", Allow: true}}
	right := []effectivePermission{{Code: "b.read", Allow: true}, {Code: "shared", Allow: false}}
	rows := mergePermissionComparisonRows(left, right)
	if len(rows) != 3 || rows[0].Code != "a.read" || rows[1].Code != "b.read" || rows[2].Code != "shared" {
		t.Fatalf("unexpected comparison rows: %#v", rows)
	}
	if rows[0].Left == nil || rows[0].Right != nil {
		t.Fatalf("expected a.read to exist only on the left: %#v", rows[0])
	}
	if rows[2].Left == nil || rows[2].Right == nil || rows[2].Left.Allow == rows[2].Right.Allow {
		t.Fatalf("expected shared permission values to differ: %#v", rows[2])
	}
}

func TestResolvedPermissionAllowsSpecificGrantOverridesWildcardAtSamePriority(t *testing.T) {
	entries := []effectivePermission{
		{Code: "user.message.*", Allow: false, Priority: 10},
		{Code: "user.message.receive", Allow: true, Priority: 10},
	}
	if !resolvedPermissionAllows(entries, "user.message.receive") {
		t.Fatal("expected the more specific permission to win")
	}
}

func TestShiftRoleTrackRoles(t *testing.T) {
	track := []string{"lv1", "lv2", "lv3"}
	tests := []struct {
		name      string
		current   []string
		direction int
		want      []string
	}{
		{name: "upgrade multiple", current: []string{"lv1", "lv2"}, direction: 1, want: []string{"lv2", "lv3"}},
		{name: "downgrade multiple", current: []string{"lv2", "lv3"}, direction: -1, want: []string{"lv1", "lv2"}},
		{name: "clamp upper boundary", current: []string{"lv3"}, direction: 1, want: []string{"lv3"}},
		{name: "deduplicate targets", current: []string{"lv2", "lv3"}, direction: 1, want: []string{"lv3"}},
		{name: "ignore unrelated roles", current: []string{"other"}, direction: 1, want: []string{}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := shiftRoleTrackRoles(track, test.current, test.direction); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("shiftRoleTrackRoles() = %#v, want %#v", got, test.want)
			}
		})
	}
}
