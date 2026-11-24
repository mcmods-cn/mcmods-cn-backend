package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestAutomationSaveReceiptAndSettingsCommitTogetherIntegration(t *testing.T) {
	f := newTEST017Fixture(t)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "project.auto_update.view", "project.auto_update.configure")
	path := "/api/v1/projects/mod/" + f.modCode + "/automation"
	body := map[string]any{"items": []map[string]any{{"kind": "minecraft_versions", "interval": "week"}, {"kind": "changelog", "interval": "month"}, {"kind": "site_downloads", "interval": "never"}}}
	if _, err := f.db.Exec(f.ctx, `alter table project_auto_update_settings rename column last_error_code to held_synthetic_error_code`); err != nil {
		t.Fatal(err)
	}
	f.require(t, f.editor, http.MethodPut, path, body, http.StatusInternalServerError)
	var count int
	if err := f.db.QueryRow(f.ctx, `select count(*) from project_auto_update_settings s join public_routes r on r.id=s.project_route_id where r.public_id=$1`, f.modCode).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Errorf("save reported failure but committed %d settings", count)
	}
	if _, err := f.db.Exec(f.ctx, `alter table project_auto_update_settings rename column held_synthetic_error_code to last_error_code`); err != nil {
		t.Fatal(err)
	}
	decode := func(raw []byte) map[string]string {
		t.Helper()
		var result struct {
			Project  struct{ ID string }
			Settings []struct{ UpdateKind, Interval string }
		}
		if err := json.Unmarshal(raw, &result); err != nil {
			t.Fatal(err)
		}
		if result.Project.ID != f.modCode {
			t.Fatalf("receipt project=%q", result.Project.ID)
		}
		rows := map[string]string{}
		for _, r := range result.Settings {
			rows[r.UpdateKind] = r.Interval
		}
		return rows
	}
	for _, rows := range []map[string]string{decode(f.require(t, f.editor, http.MethodPut, path, body, http.StatusOK)), decode(f.require(t, f.editor, http.MethodGet, path, nil, http.StatusOK))} {
		if len(rows) != 3 || rows["minecraft_versions"] != "week" || rows["changelog"] != "month" || rows["site_downloads"] != "never" {
			t.Errorf("save/read receipt settings: %v", rows)
		}
	}
}
