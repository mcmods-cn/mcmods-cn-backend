package httpapi

import (
	"encoding/json"
	"net/http"
	"net/url"
	"path/filepath"
	"testing"
	"time"
)

func auditGeometryAssetExpectations() []map[string]any {
	return []map[string]any{
		{"purpose": "background", "path": "recipes/jei/backgrounds/example/pressing/template_0123456789abcdef.png", "width": 528, "height": 328, "rgba": []int{210, 210, 210, 255}},
		{"purpose": "item", "path": "icons/items/32/example/geometry_item.png", "width": 32, "height": 32, "rgba": []int{240, 140, 30, 255}},
		{"purpose": "fluid", "path": "ingredients/net.minecraftforge.fluids.fluidstack/32/example/geometry_fluid--0123456789abcdef.png", "width": 32, "height": 32, "rgba": []int{30, 100, 240, 128}},
		{"purpose": "mekanism_gas", "path": "ingredients/mekanism.api.chemical.gas.gasstack/32/example/geometry_gas--0123456789abcdef.png", "width": 32, "height": 32, "rgba": []int{100, 210, 80, 192}},
	}
}

// This extra phase begins only after the external browser is finished and the
// owner writes STOP. It uses the supported ordinary version deletion followed
// by an independent review; it does not mutate import status through SQL.
func auditGeometryIndependentWithdrawalSeal(t *testing.T, f auditFixGRFixture, revision, directory string) {
	t.Helper()
	assets := auditGeometryAssetExpectations()
	for _, asset := range assets {
		path := "/api/v1/export-revisions/" + revision + "/assets/content?path=" + url.QueryEscape(asset["path"].(string))
		body := f.require(t, "", http.MethodGet, path, nil, 200)
		asset["anonymousBeforeStatus"], asset["beforeSHA256"] = 200, sha256Hex(body)
	}
	directoryPath := "/api/v1/recipe-types?canonicalId=example:pressing"
	before := decodeTEST022Data[map[string]any](t, f.require(t, "", http.MethodGet, directoryPath, nil, 200))
	if before["total"] != float64(1) || len(before["items"].([]any)) != 1 {
		t.Fatal("equipped source was not public before independent withdrawal")
	}
	config := defaultReviewConfig()
	config.CatalogDelete = true
	raw, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(f.ctx, `insert into system_settings(key,value) values($1,$2::jsonb) on conflict(key) do update set value=excluded.value`, reviewConfigSettingKey, raw); err != nil {
		t.Fatal(err)
	}
	generation := f.number(t, `select version from catalog_dataset_state where singleton`)
	mutation := decodeTEST022Data[modContentMutationResult](t, f.require(t, f.editor, http.MethodDelete, "/api/v1/mods/audit-gr-"+f.modCode+"/content-versions/"+f.versionPublic, nil, 200))
	if mutation.ReviewStatus != "pending" {
		t.Fatal("ordinary version withdrawal bypassed independent review")
	}
	f.require(t, f.reviewer, http.MethodPatch, "/api/v1/content-revisions/"+mutation.RevisionID, map[string]string{"status": "approved", "note": "Independent image-equipped source withdrawal"}, 200)
	for _, asset := range assets {
		path := "/api/v1/export-revisions/" + revision + "/assets/content?path=" + url.QueryEscape(asset["path"].(string))
		f.require(t, "", http.MethodGet, path, nil, 403)
		asset["anonymousAfterStatus"] = 403
	}
	after := decodeTEST022Data[map[string]any](t, f.require(t, "", http.MethodGet, directoryPath, nil, 200))
	if after["total"] != float64(0) || len(after["items"].([]any)) != 0 ||
		f.number(t, `select count(*) from mod_content_versions where id=$1 and status='archived'`, f.versionID) != 1 ||
		f.number(t, `select count(*) from catalog_import_revisions where id=$1 and not is_active and status='superseded'`, revision) != 1 ||
		f.number(t, `select version from catalog_dataset_state where singleton`) != generation+1 {
		t.Fatal("independent equipped withdrawal left a public directory or inconsistent source/cache generation")
	}
	auditJourneyWriteJSON(t, filepath.Join(directory, "independent-image-withdrawal-seal.json"), map[string]any{
		"status":     "PASS_ACTUAL_HTTP_PG_INDEPENDENT_VERSION_ARCHIVE_WARM_DIRECTORY_AND_FOUR_PNGS",
		"recordedAt": time.Now().UTC().Format(time.RFC3339Nano), "revisionId": revision, "versionPublicId": f.versionPublic,
		"withdrawalRevisionPublicId": mutation.RevisionID, "creatorInternalId": f.userIDs[f.editor], "reviewerInternalId": f.userIDs[f.reviewer],
		"assets": assets, "directoryTotalBefore": 1, "directoryTotalAfter": 0, "catalogGenerationBefore": generation, "catalogGenerationAfter": generation + 1,
		"scope": "Synthetic registered producer contract; actual HTTP/PG independent review. No production mutation or recall of already downloaded bytes.",
	})
}
