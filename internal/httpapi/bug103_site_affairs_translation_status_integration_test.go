package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
	"mcmods-cn-backend/internal/security"
)

func TestSiteAffairsTranslationDraftsDoNotChangeOtherLocalesPublicationIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify independent site-affairs translation publication")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	poolConfig, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	poolConfig.MaxConns = 1
	poolConfig.MinConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err = database.InstallEphemeralSchema(ctx, pool); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if dropErr := database.DropEphemeralSchema(context.Background(), pool); dropErr != nil {
			t.Errorf("drop BUG-103 ephemeral schema: %v", dropErr)
		}
	}()

	suffix := time.Now().UnixNano()
	var actorID, aboutPageID int64
	if err = pool.QueryRow(ctx, `insert into users(username,email,password_hash,status)
		values($1,$2,'not-used','active') returning id`, fmt.Sprintf("bug103_admin_%d", suffix), fmt.Sprintf("bug103_admin_%d@example.test", suffix)).Scan(&actorID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `insert into site_pages(code,status,updated_by) values('about','published',$1)
		on conflict(code) do update set status='published',updated_by=excluded.updated_by returning id`, actorID).Scan(&aboutPageID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into site_page_translations(page_id,locale,title,body_markdown,status,updated_by)
		values($1,'zh-CN','简中标题','简中正文','published',$2),($1,'en-US','English title','English body','published',$2)
		on conflict(page_id,locale) do update set title=excluded.title,body_markdown=excluded.body_markdown,status=excluded.status`, aboutPageID, actorID); err != nil {
		t.Fatal(err)
	}

	server := &Server{db: pool}
	claims := security.Claims{Subject: actorID}
	draftAbout := invokeBUG102About(t, ctx, server, claims, http.MethodPut, "fr-FR", map[string]any{
		"title": "Brouillon", "bodyMarkdown": "Corps brouillon", "publish": false, "baseRevision": 0,
	})
	if draftAbout.Code != http.StatusOK {
		t.Fatalf("save about draft status = %d body=%s", draftAbout.Code, draftAbout.Body.String())
	}
	publicAbout := httptest.NewRecorder()
	server.publicAboutPage(publicAbout, httptest.NewRequest(http.MethodGet, "/api/v1/site-affairs/about?locale=en-US", nil).WithContext(ctx))
	if publicAbout.Code != http.StatusOK {
		t.Fatalf("English about disappeared after French draft: status=%d body=%s", publicAbout.Code, publicAbout.Body.String())
	}
	assertBUG103AboutStatuses(t, ctx, pool, aboutPageID, "published", map[string]string{
		"zh-CN": "published", "en-US": "published", "fr-FR": "draft",
	})

	var changelogID int64
	var changelogPublicID string
	var changelogUpdatedAt time.Time
	if err = pool.QueryRow(ctx, `insert into site_changelogs(change_date,status,created_by)
		values(date '2026-08-01','published',$1) returning id,public_id,updated_at`, actorID).Scan(&changelogID, &changelogPublicID, &changelogUpdatedAt); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into site_changelog_translations(changelog_id,locale,title,body_markdown,status,updated_by)
		values($1,'zh-CN','简中更新','简中正文','published',$2)`, changelogID, actorID); err != nil {
		t.Fatal(err)
	}
	draftFrench := invokeBUG102Changelog(t, ctx, server, claims, http.MethodPut, changelogPublicID, "fr-FR", "2026-08-02", "French draft", false, changelogUpdatedAt)
	if draftFrench.Code != http.StatusOK {
		t.Fatalf("save French changelog draft status = %d body=%s", draftFrench.Code, draftFrench.Body.String())
	}
	assertBUG103PublicChangelog(t, ctx, server, changelogPublicID, "zh-CN", http.StatusOK, "zh-CN", "简中更新")
	assertBUG103ChangelogStatuses(t, ctx, pool, changelogID, "published", map[string]string{"zh-CN": "published", "fr-FR": "draft"})
	if err = pool.QueryRow(ctx, `select updated_at from site_changelogs where id=$1`, changelogID).Scan(&changelogUpdatedAt); err != nil {
		t.Fatal(err)
	}

	publishFrench := invokeBUG102Changelog(t, ctx, server, claims, http.MethodPut, changelogPublicID, "fr-FR", "2026-08-03", "French published", true, changelogUpdatedAt)
	if publishFrench.Code != http.StatusOK {
		t.Fatalf("publish French changelog status = %d body=%s", publishFrench.Code, publishFrench.Body.String())
	}
	if err = pool.QueryRow(ctx, `select updated_at from site_changelogs where id=$1`, changelogID).Scan(&changelogUpdatedAt); err != nil {
		t.Fatal(err)
	}
	draftChinese := invokeBUG102Changelog(t, ctx, server, claims, http.MethodPut, changelogPublicID, "zh-CN", "2026-08-04", "Chinese draft", false, changelogUpdatedAt)
	if draftChinese.Code != http.StatusOK {
		t.Fatalf("save Chinese changelog draft status = %d body=%s", draftChinese.Code, draftChinese.Body.String())
	}
	assertBUG103PublicChangelog(t, ctx, server, changelogPublicID, "zh-CN", http.StatusOK, "fr-FR", "French published")
	assertBUG103ChangelogStatuses(t, ctx, pool, changelogID, "published", map[string]string{"zh-CN": "draft", "fr-FR": "published"})
	adminDetail, err := loadAdminSiteChangelogDetail(ctx, pool, changelogPublicID)
	if err != nil {
		t.Fatal(err)
	}
	if adminDetail.Translations["zh-CN"].Status != "draft" || adminDetail.Translations["fr-FR"].Status != "published" {
		t.Fatalf("admin detail lost translation publication facts: %+v", adminDetail.Translations)
	}

	draftOnly := invokeBUG102Changelog(t, ctx, server, claims, http.MethodPost, "", "es-ES", "2026-08-05", "Spanish draft", false)
	if draftOnly.Code != http.StatusCreated {
		t.Fatalf("create draft-only changelog status = %d body=%s", draftOnly.Code, draftOnly.Body.String())
	}
	var created struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err = json.Unmarshal(draftOnly.Body.Bytes(), &created); err != nil || created.Data.ID == "" {
		t.Fatalf("decode draft-only changelog: id=%q err=%v body=%s", created.Data.ID, err, draftOnly.Body.String())
	}
	assertBUG103PublicChangelog(t, ctx, server, created.Data.ID, "es-ES", http.StatusNotFound, "", "")

	list := httptest.NewRecorder()
	server.publicSiteChangelogs(list, httptest.NewRequest(http.MethodGet, "/api/v1/site-affairs/changelogs?locale=zh-CN&limit=30", nil).WithContext(ctx))
	if list.Code != http.StatusOK {
		t.Fatalf("public changelog list status = %d body=%s", list.Code, list.Body.String())
	}
	var page struct {
		Data siteChangelogPublicPage `json:"data"`
	}
	if err = json.Unmarshal(list.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Data.Items) != 1 || page.Data.Items[0].ID != changelogPublicID || page.Data.Items[0].Locale != "fr-FR" {
		t.Fatalf("public page leaked draft or lost published fallback: %+v", page.Data.Items)
	}
}

func assertBUG103AboutStatuses(t *testing.T, ctx context.Context, pool *pgxpool.Pool, pageID int64, expectedParent string, expectedTranslations map[string]string) {
	t.Helper()
	var parentStatus string
	if err := pool.QueryRow(ctx, `select status from site_pages where id=$1`, pageID).Scan(&parentStatus); err != nil {
		t.Fatal(err)
	}
	if parentStatus != expectedParent {
		t.Fatalf("about parent status = %q, want %q", parentStatus, expectedParent)
	}
	for locale, expected := range expectedTranslations {
		var status string
		if err := pool.QueryRow(ctx, `select status from site_page_translations where page_id=$1 and locale=$2`, pageID, locale).Scan(&status); err != nil {
			t.Fatal(err)
		}
		if status != expected {
			t.Fatalf("about %s status = %q, want %q", locale, status, expected)
		}
	}
}

func assertBUG103ChangelogStatuses(t *testing.T, ctx context.Context, pool *pgxpool.Pool, changelogID int64, expectedParent string, expectedTranslations map[string]string) {
	t.Helper()
	var parentStatus string
	if err := pool.QueryRow(ctx, `select status from site_changelogs where id=$1`, changelogID).Scan(&parentStatus); err != nil {
		t.Fatal(err)
	}
	if parentStatus != expectedParent {
		t.Fatalf("changelog parent status = %q, want %q", parentStatus, expectedParent)
	}
	for locale, expected := range expectedTranslations {
		var status string
		if err := pool.QueryRow(ctx, `select status from site_changelog_translations where changelog_id=$1 and locale=$2`, changelogID, locale).Scan(&status); err != nil {
			t.Fatal(err)
		}
		if status != expected {
			t.Fatalf("changelog %s status = %q, want %q", locale, status, expected)
		}
	}
}

func assertBUG103PublicChangelog(t *testing.T, ctx context.Context, server *Server, publicID, locale string, expectedStatus int, expectedLocale, expectedTitle string) {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/site-affairs/changelogs/"+publicID+"?locale="+locale, nil).WithContext(ctx)
	request.SetPathValue("id", publicID)
	response := httptest.NewRecorder()
	server.publicSiteChangelogDetail(response, request)
	if response.Code != expectedStatus {
		t.Fatalf("public changelog %s status = %d body=%s, want %d", publicID, response.Code, response.Body.String(), expectedStatus)
	}
	if expectedStatus != http.StatusOK {
		return
	}
	var envelope struct {
		Data struct {
			Locale string `json:"locale"`
			Title  string `json:"title"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Data.Locale != expectedLocale || envelope.Data.Title != expectedTitle {
		t.Fatalf("public changelog = locale %q title %q, want %q/%q", envelope.Data.Locale, envelope.Data.Title, expectedLocale, expectedTitle)
	}
}
