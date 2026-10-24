package httpapi

import (
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"mcmods-cn-backend/internal/activity"
)

func auditFixSiteAffairsFacts(t *testing.T, f test013Fixture) map[string]string {
	t.Helper()
	facts := f.facts(t)
	for _, table := range []string{"site_pages", "site_page_translations", "site_changelogs", "site_changelog_translations"} {
		var raw string
		if err := f.db.QueryRow(f.ctx, `select coalesce(jsonb_agg(to_jsonb(fact) order by to_jsonb(fact)::text),'[]'::jsonb)::text from `+pgx.Identifier{table}.Sanitize()+` fact`).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		facts[table] = raw
	}
	return facts
}
func TestAuditFixCMP003SiteChangelogActivitySharesTranslationTransaction(t *testing.T) {
	f := newTEST013Fixture(t)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.reviewer], "site_affairs.changelog.manage")
	recover := func(method, path string, body any, want int, action int16) []byte {
		t.Helper()
		prior := auditFixActivityIntentCount(t, f, f.reviewer, action, activity.ObjectChangelog)
		auditFixInjectActivityFailure(t, f, f.userIDs[f.reviewer], true)
		before := auditFixSiteAffairsFacts(t, f)
		response := f.require(t, f.reviewer, method, path, body, 500)
		auditFixRequireSpecificActivityError(t, response, "failed to persist site affairs activity")
		auditFixRequireProjectFactsUnchanged(t, before, auditFixSiteAffairsFacts(t, f))
		auditFixInjectActivityFailure(t, f, f.userIDs[f.reviewer], false)
		raw := f.require(t, f.reviewer, method, path, body, want)
		if got := auditFixActivityIntentCount(t, f, f.reviewer, action, activity.ObjectChangelog); got != prior+1 {
			t.Fatalf("changelog activity=%d prior=%d", got, prior)
		}
		return raw
	}
	body := saveSiteChangelogRequest{ChangeDate: "2026-10-24", Locale: "en-US", Title: "Synthetic atomic changelog", BodyMarkdown: "Synthetic reviewed release notes", Publish: true}
	created := decodeTEST022Data[struct {
		ID string `json:"id"`
	}](t, recover(http.MethodPost, "/api/v1/admin/site-affairs/changelogs", body, 201, activity.ActionCreate))
	detail := decodeTEST022Data[struct {
		UpdatedAt time.Time `json:"updatedAt"`
	}](t, f.require(t, f.reviewer, http.MethodGet, "/api/v1/admin/site-affairs/changelogs/"+created.ID, nil, 200))
	body.BaseUpdatedAt = &detail.UpdatedAt
	body.Title = "Updated synthetic atomic changelog"
	recover(http.MethodPut, "/api/v1/admin/site-affairs/changelogs/"+created.ID, body, 200, activity.ActionEdit)
	public := decodeTEST022Data[struct {
		Title string `json:"title"`
	}](t, f.require(t, "", http.MethodGet, "/api/v1/site-affairs/changelogs/"+created.ID+"?locale=en-US", nil, 200))
	if public.Title != body.Title {
		t.Fatalf("public changelog title=%q", public.Title)
	}
	before := auditFixSiteAffairsFacts(t, f)
	f.require(t, f.reviewer, http.MethodPut, "/api/v1/admin/site-affairs/changelogs/"+created.ID, body, 409)
	auditFixRequireProjectFactsUnchanged(t, before, auditFixSiteAffairsFacts(t, f))
}
