package httpapi

import (
	"net/http"
	"testing"
)

func TestOCT03CTEST046And047AdminReadModelsEnforceIndependentPermissionsIntegration(t *testing.T) {
	f := newTEST013Fixture(t)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "site_affairs.about.manage")
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.otherEditor], "site_affairs.changelog.manage")
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.reviewer], "reference.unresolved.read")
	var aboutID int64
	if err := f.db.QueryRow(f.ctx, `insert into site_pages(code,status,updated_by) values('about','published',$1)
		on conflict(code) do update set status='published',updated_by=excluded.updated_by returning id`, f.userIDs[f.editor]).Scan(&aboutID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(f.ctx, `insert into site_page_translations(page_id,locale,title,body_markdown,status,updated_by)
		values($1,'en-US','Oct03 about','Preserve this body','published',$2)
		on conflict(page_id,locale) do update set title=excluded.title,body_markdown=excluded.body_markdown`, aboutID, f.userIDs[f.editor]); err != nil {
		t.Fatal(err)
	}
	var changelogID int64
	var changelogPublicID string
	if err := f.db.QueryRow(f.ctx, `insert into site_changelogs(change_date,status,created_by)
		values(current_date,'published',$1) returning id,public_id`, f.userIDs[f.otherEditor]).Scan(&changelogID, &changelogPublicID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(f.ctx, `insert into site_changelog_translations(changelog_id,locale,title,body_markdown,status,updated_by)
		values($1,'en-US','Oct03 changelog','Preserve this changelog','published',$2)`, changelogID, f.userIDs[f.otherEditor]); err != nil {
		t.Fatal(err)
	}
	for _, route := range []struct {
		method, path, allowed, wrong string
	}{
		{http.MethodGet, "/api/v1/admin/site-affairs/about/en-US", f.editor, f.otherEditor},
		{http.MethodGet, "/api/v1/admin/site-affairs/changelogs?limit=1", f.otherEditor, f.editor},
		{http.MethodGet, "/api/v1/admin/site-affairs/changelogs/" + changelogPublicID, f.otherEditor, f.editor},
		{http.MethodGet, "/api/v1/admin/unresolved-references?status=pending&limit=1", f.reviewer, f.editor},
		{http.MethodGet, "/api/v1/admin/unresolved-reference-types", f.reviewer, f.otherEditor},
	} {
		f.require(t, "", route.method, route.path, nil, http.StatusUnauthorized)
		f.require(t, f.denied, route.method, route.path, nil, http.StatusForbidden)
		f.require(t, route.wrong, route.method, route.path, nil, http.StatusForbidden)
		f.require(t, route.allowed, route.method, route.path, nil, http.StatusOK)
	}
	for _, route := range []struct{ method, path string }{
		{http.MethodPut, "/api/v1/admin/site-affairs/about/en-US"},
		{http.MethodPost, "/api/v1/admin/site-affairs/changelogs"},
		{http.MethodPut, "/api/v1/admin/site-affairs/changelogs/" + changelogPublicID},
	} {
		f.require(t, "", route.method, route.path, map[string]string{"title": "unauthorized overwrite"}, http.StatusUnauthorized)
		f.require(t, f.denied, route.method, route.path, map[string]string{"title": "unauthorized overwrite"}, http.StatusForbidden)
	}
	var aboutTitle, changelogTitle string
	var changelogCount int
	if err := f.db.QueryRow(f.ctx, `select
		(select title from site_page_translations where page_id=$1 and locale='en-US'),
		(select title from site_changelog_translations where changelog_id=$2 and locale='en-US'),
		(select count(*) from site_changelogs)`, aboutID, changelogID).Scan(&aboutTitle, &changelogTitle, &changelogCount); err != nil {
		t.Fatal(err)
	}
	if aboutTitle != "Oct03 about" || changelogTitle != "Oct03 changelog" || changelogCount != 1 {
		t.Fatalf("rejected route changed persisted data: about=%q changelog=%q count=%d", aboutTitle, changelogTitle, changelogCount)
	}
}
