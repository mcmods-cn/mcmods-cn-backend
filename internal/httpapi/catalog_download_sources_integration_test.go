package httpapi

import (
	"net/http"
	"testing"
)

func TestCatalogDownloadSourcesTrackTheFileListAndProviderBindingsIntegration(t *testing.T) {
	f := newTEST016FileFixture(t)
	var packID string
	if _, err := f.db.Exec(f.ctx, `update mods set curseforge_project_id='',modrinth_project_id='' where id=$1`, f.modID); err != nil {
		t.Fatal(err)
	}
	if err := f.db.QueryRow(f.ctx, `insert into modpacks(slug,primary_name,review_status,submitted_by)
		values('catalog-download-fixture','Catalog download fixture','approved',$1) returning public_id`, f.userIDs[f.editor]).Scan(&packID); err != nil {
		t.Fatal(err)
	}
	check := func(collection, id string, want bool) {
		t.Helper()
		before := f.requests.Load()
		catalog := decodeTEST022Data[struct {
			Items []struct {
				ID string `json:"id"`
			} `json:"items"`
		}](t,
			f.require(t, "", http.MethodGet, "/api/v1/"+collection+"?feature=downloads", nil, http.StatusOK))
		var found bool
		for _, item := range catalog.Items {
			if item.ID == id {
				found = true
			}
		}
		if found != want || f.requests.Load() != before {
			t.Fatalf("%s source filter: found=%v, want=%v, storage requests=%d", collection, found, want, f.requests.Load()-before)
		}
	}
	check("mods", f.modCode, false)
	check("modpacks", packID, false)
	base := "/api/v1/projects/mod/" + f.modCode + "/files"
	file, _ := f.register(t, f.editor, base, "source-filter.jar")
	id := f.attach(t, f.editor, base, file)
	f.assertList(t, "", base)
	check("mods", f.modCode, false)
	f.scan(t, file, "clean", http.StatusOK)
	f.assertList(t, "", base, id)
	check("mods", f.modCode, true)
	f.scan(t, file, "rejected", http.StatusOK)
	f.assertList(t, "", base)
	check("mods", f.modCode, false)
	if _, err := f.db.Exec(f.ctx, `update modpacks set modrinth_project_id='fixture-provider' where public_id=$1`, packID); err != nil {
		t.Fatal(err)
	}
	check("modpacks", packID, true)
}
