package httpapi

import (
	"os"
	"strings"
	"testing"
)

func TestBlueprintDerivedReadsDoNotHideDatabaseFailures(t *testing.T) {
	raw, err := os.ReadFile("blueprint_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(raw)
	for _, forbidden := range []string{
		"func (s *Server) blueprintAssetRevisions(ctx context.Context, blueprintID int64) []map[string]any",
		"if revisionErr == nil {",
		"if revisionRows.Scan(&namespace, &revisionID) == nil {",
		"resolved, _ := s.resolveExportResources(ctx, keys)",
		"if rows.Scan(&revisionID, &siteID, &paths) == nil {",
		"err != nil || objectKey == \"\"",
		"errors.Is(err, pgx.ErrNoRows) || objectKey == \"\"",
	} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("blueprint read path still hides a required failure: %q", forbidden)
		}
	}
	for _, required := range []string{
		"func (s *Server) blueprintAssetRevisions(ctx context.Context, blueprintID int64) ([]map[string]any, error)",
		"assetRevisions, err := s.blueprintAssetRevisions",
		"if err = revisionRows.Scan(&namespace, &revisionID); err != nil",
		"if err = revisionRows.Err(); err != nil",
		"resolved, err := s.resolveExportResources(ctx, keys)",
		"if errors.Is(err, pgx.ErrNoRows)",
		"logBlueprintReadFailure(publicID, \"cover\", err)",
		"logBlueprintReadFailure(publicID, \"render_data\", err)",
	} {
		if !strings.Contains(source, required) {
			t.Fatalf("blueprint read path is missing failure contract %q", required)
		}
	}
}
