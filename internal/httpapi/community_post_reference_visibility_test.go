package httpapi

import (
	"os"
	"strings"
	"testing"
)

func TestCommunityPostReferencePathsShareCurrentViewerVisibility(t *testing.T) {
	raw, err := os.ReadFile("community_post_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	pagination, err := os.ReadFile("community_post_pagination.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(raw) + string(pagination)
	for _, required := range []string{
		"BeginTx(r.Context(), pgx.TxOptions{IsoLevel: pgx.RepeatableRead})",
		"loadFollowProjectTargetsWithQueryer(ctx, tx, publicIDs, claims)",
		"loadFollowProjectTargetsWithQueryer(ctx, queryer, publicIDs, claims)",
		`followProjectTargetVisibilitySQL("route", "$2", "$3")`,
		"from community_post_catalog post",
		"ref.Unavailable = true",
	} {
		if !strings.Contains(source, required) {
			t.Fatalf("community reference visibility boundary is missing %q", required)
		}
	}
	if strings.Contains(source, "coalesce(mod.slug,modpack.slug,project.slug,'')") {
		t.Fatal("community reference reads still join target metadata before visibility is resolved")
	}

	projectSource, err := os.ReadFile("project_follow_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{
		"func followProjectTargetVisibilitySQL(",
		"followProjectTargetSelectSQL(\"route\")",
		"route.public_id=any($1::text[])",
	} {
		if !strings.Contains(string(projectSource), required) {
			t.Fatalf("shared project visibility resolver is missing %q", required)
		}
	}
}
