package httpapi

import (
	"os"
	"strings"
	"testing"
)

func TestModExportPublicTagAndAssetIndexesHaveNoUnboundedSwitch(t *testing.T) {
	tagRaw, err := os.ReadFile("mod_export_tag_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	tagSource := string(tagRaw)
	for _, forbidden := range []string{`Get("all")`, `Get("offset")`, "limit = 10000", "make([]map[string]any, 0, memberCount)"} {
		if strings.Contains(tagSource, forbidden) {
			t.Errorf("public tag handler still contains unbounded/deep-page path %q", forbidden)
		}
	}
	queryRaw, err := os.ReadFile("mod_export_query_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	querySource := string(queryRaw)
	if strings.Contains(querySource, `Get("all")`) || strings.Contains(querySource, "limit = 10000") {
		t.Fatal("Mod export public indexes still expose a 10,000-row all switch")
	}
	start := strings.Index(querySource, "func (s *Server) modExportAssetIndex")
	end := strings.Index(querySource, "func (s *Server) modExportAssetContent")
	if start < 0 || end <= start {
		t.Fatal("asset index function inventory changed")
	}
	assetSource := querySource[start:end]
	for _, forbidden := range []string{"make([]string, 0, 4096)", "union all select asset_path", "order by 1`"} {
		if strings.Contains(assetSource, forbidden) {
			t.Errorf("public asset handler bypasses bounded cursor loader with %q", forbidden)
		}
	}
	for _, required := range []string{"decodeModExportPageCursor", "loadModExportAssetPage", "nextCursor", "hasMore"} {
		if !strings.Contains(assetSource, required) {
			t.Errorf("public asset handler is missing bounded paging step %q", required)
		}
	}
}

func TestModExportQueryPathsDoNotSilentlyDiscardRowOrJSONErrors(t *testing.T) {
	for _, name := range []string{"mod_export_tag_handlers.go", "mod_export_query_handlers.go", "mod_export_entry_handlers.go", "mod_export_resources.go", "mod_export_handlers.go"} {
		raw, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), "_ = json.Unmarshal") {
			t.Errorf("%s still discards a JSON decoding error", name)
		}
	}
	queryRaw, err := os.ReadFile("mod_export_query_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(queryRaw), "for rows.Next()") != strings.Count(string(queryRaw), "rows.Err()") {
		t.Fatalf("query handler row loops and terminal error checks diverged: loops=%d checks=%d",
			strings.Count(string(queryRaw), "for rows.Next()"), strings.Count(string(queryRaw), "rows.Err()"))
	}
}

func TestModExportPublicResourcesUseOnePublicIDContract(t *testing.T) {
	checks := map[string][]string{
		"blueprint_handlers.go":        {`"entityId": source.EntityID`},
		"mod_export_entry_handlers.go": {`json:"entityId"`, `Get("entityId")`},
		"mod_export_tag_handlers.go":   {`"entityId": row.PublicID`, `Get("entityId")`, `"entityId": publicID`},
		"mod_export_query_handlers.go": {`"entityId": publicID`},
		"mod_export_resources.go":      {"EntityID        string", `"entityId": source.EntityID`, "coalesce(source.public_id,''),coalesce(source.public_id,'')"},
	}
	for name, forbidden := range checks {
		raw, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		for _, value := range forbidden {
			if strings.Contains(string(raw), value) {
				t.Errorf("%s retained duplicate public identity contract %q", name, value)
			}
		}
	}
}

func TestModExportReviewRecordsBoundedNoteInBothDecisions(t *testing.T) {
	raw, err := os.ReadFile("mod_export_query_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(raw)
	for _, required := range []string{
		"maxModExportReviewNoteBytes", "request.Note = strings.TrimSpace(request.Note)",
		"insert into audit_events", "'decision'", "'note'", "'beforeStatus'", "'afterStatus'",
	} {
		if !strings.Contains(source, required) {
			t.Errorf("Mod export review audit is missing %q", required)
		}
	}
	if count := strings.Count(source, "recordModExportRevisionReviewTx("); count != 3 {
		t.Fatalf("review audit must be invoked by approved and rejected branches exactly once: references=%d", count)
	}
}

func TestModExportEntryLocaleDoesNotReplacePreferencesWithImplicitDefault(t *testing.T) {
	raw, err := os.ReadFile("mod_export_entry_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(raw)
	start := strings.Index(source, "func (s *Server) modExportEntryDetail")
	end := strings.Index(source, "func decodeModExportEntryData")
	if start < 0 || end <= start {
		t.Fatal("entry detail handler inventory changed")
	}
	handler := source[start:end]
	if strings.Contains(handler, `locale := normalizeExportContentLocale(r.URL.Query().Get("locale"))`) {
		t.Fatal("empty entry locale is still normalized into a forced default")
	}
	preference := strings.Index(handler, "primary, secondary := s.requestContentLocales(r)")
	override := strings.Index(handler, `optionalExportContentLocale(r.URL.Query().Get("locale"))`)
	if preference < 0 || override <= preference {
		t.Fatal("entry detail must establish request/user locales before an explicit valid query override")
	}
}
