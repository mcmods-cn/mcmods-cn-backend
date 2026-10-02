package httpapi

import (
	"os"
	"strings"
	"testing"
)

func TestFavoriteExportPathCollisionsBecomeStablePerItemFailures(t *testing.T) {
	items := []favoriteModpackExportItem{
		{SourceProjectName: "Case A", ResultType: "exported", SelectedFileName: "Example.jar"},
		{SourceProjectName: "Case B", ResultType: "auto_dependency", SelectedFileName: "example.jar"},
		{SourceProjectName: "Unicode A", ResultType: "exported", SelectedFileName: "Caf\u00e9.jar"},
		{SourceProjectName: "Unicode B", ResultType: "exported", SelectedFileName: "Cafe\u0301.jar"},
		{SourceProjectName: "Unique", ResultType: "exported", SelectedFileName: "unique.jar"},
		{SourceProjectName: "Already skipped", ResultType: "skipped", SelectedFileName: "UNIQUE.jar"},
	}
	markFavoriteExportPathConflicts(items)
	for index := 0; index < 4; index++ {
		if items[index].ResultType != "failed" || items[index].ReasonCode != exportReasonFilePathConflict || items[index].ReasonDetail == "" {
			t.Fatalf("conflicting item %d = %+v", index, items[index])
		}
	}
	if items[4].ResultType != "exported" || items[4].ReasonCode != "" {
		t.Fatalf("unique exported item changed: %+v", items[4])
	}
	if items[5].ResultType != "skipped" || items[5].ReasonCode != "" {
		t.Fatalf("non-output item participated in conflicts: %+v", items[5])
	}
	if !favoriteExportHasPathConflict(items) {
		t.Fatal("marked preview did not expose its blocking path conflict")
	}
}

func TestFavoriteExportPreviewMarksConflictsBeforeCountsAndCreation(t *testing.T) {
	source, err := os.ReadFile("favorite_modpack_export.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	buildStart := strings.Index(text, "func (s *Server) buildFavoriteModpackExportPreview")
	if buildStart < 0 {
		t.Fatal("favorite export preview builder is missing")
	}
	buildSource := text[buildStart:]
	mark := strings.Index(buildSource, "markFavoriteExportPathConflicts(preview.Items)")
	count := strings.Index(buildSource, "for _, item := range preview.Items")
	if mark < 0 || count < 0 || mark > count {
		t.Fatal("favorite export preview does not mark portable path conflicts before computing result counts")
	}
	createStart := strings.Index(text, "func (s *Server) createFavoriteModpackExport")
	createEnd := strings.Index(text, "func decodeFavoriteModpackExportRequest")
	if createStart < 0 || createEnd < createStart {
		t.Fatal("favorite export create handler is missing")
	}
	createSource := text[createStart:createEnd]
	blocking := strings.Index(createSource, "favoriteExportHasPathConflict(preview.Items)")
	empty := strings.Index(createSource, "preview.ExportedModCount+preview.AutoDependencyCount == 0")
	if blocking < 0 || empty < 0 || blocking > empty || !strings.Contains(createSource, "MODPACK_EXPORT_FILE_PATH_CONFLICT") {
		t.Fatal("favorite export creation does not reject the confirmed conflict with a stable API error before empty-package fallback")
	}
}
