package httpapi

import (
	"bytes"
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

func TestSiteAffairsLocaleSeparatesPublicFallbackFromAdminValidation(t *testing.T) {
	tests := []struct {
		input string
		want  string
		valid bool
	}{
		{input: "zh", want: "zh-CN", valid: true},
		{input: "zh_hant", want: "zh-TW", valid: true},
		{input: "en", want: "en-US", valid: true},
		{input: "ja", want: "ja-JP", valid: true},
		{input: "ru", want: "ru-RU", valid: true},
		{input: "fr_fr", want: "fr-FR", valid: true},
		{input: "de", want: "de-DE", valid: true},
		{input: "es", want: "es-ES", valid: true},
		{input: "", want: "", valid: false},
		{input: "ko-KR", want: "ko-KR", valid: false},
		{input: "not a locale", want: "not a locale", valid: false},
	}
	for _, test := range tests {
		t.Run(test.input, func(t *testing.T) {
			got, valid := normalizedSiteAffairsAdminLocale(test.input)
			if got != test.want || valid != test.valid {
				t.Fatalf("normalizedSiteAffairsAdminLocale(%q) = (%q,%v), want (%q,%v)", test.input, got, valid, test.want, test.valid)
			}
		})
	}
	if got := normalizedSiteAffairsLocale("ko-KR"); got != "zh-CN" {
		t.Fatalf("public fallback locale = %q, want zh-CN", got)
	}
}

func TestSiteAffairsAdminLocalesRejectInvalidValuesBeforeMutationIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify strict site-affairs admin locales")
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
			t.Errorf("drop BUG-102 ephemeral schema: %v", dropErr)
		}
	}()

	suffix := time.Now().UnixNano()
	var actorID, aboutPageID int64
	if err = pool.QueryRow(ctx, `insert into users(username,email,password_hash,status)
		values($1,$2,'not-used','active') returning id`, fmt.Sprintf("bug102_admin_%d", suffix), fmt.Sprintf("bug102_admin_%d@example.test", suffix)).Scan(&actorID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `insert into site_pages(code,status,updated_by) values('about','published',$1)
		on conflict(code) do update set status='published',updated_by=excluded.updated_by returning id`, actorID).Scan(&aboutPageID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into site_page_translations(page_id,locale,title,body_markdown,status,updated_by)
		values($1,'zh-CN','简中标题','简中正文','published',$2)
		on conflict(page_id,locale) do update set title=excluded.title,body_markdown=excluded.body_markdown,status=excluded.status`, aboutPageID, actorID); err != nil {
		t.Fatal(err)
	}

	server := &Server{db: pool}
	claims := security.Claims{Subject: actorID}
	invalidAboutRead := invokeBUG102About(t, ctx, server, claims, http.MethodGet, "ko-KR", nil)
	if invalidAboutRead.Code != http.StatusBadRequest {
		t.Fatalf("invalid about read status = %d body=%s, want 400", invalidAboutRead.Code, invalidAboutRead.Body.String())
	}
	invalidAboutWrite := invokeBUG102About(t, ctx, server, claims, http.MethodPut, "ko-KR", map[string]any{
		"title": "overwritten", "bodyMarkdown": "overwritten", "publish": true,
	})
	if invalidAboutWrite.Code != http.StatusBadRequest {
		t.Fatalf("invalid about write status = %d body=%s, want 400", invalidAboutWrite.Code, invalidAboutWrite.Body.String())
	}
	assertBUG102AboutTranslation(t, ctx, pool, aboutPageID, "zh-CN", "简中标题", "简中正文")

	validAliasWrite := invokeBUG102About(t, ctx, server, claims, http.MethodPut, "fr_fr", map[string]any{
		"title": "Titre", "bodyMarkdown": "Corps", "publish": false, "baseRevision": 0,
	})
	if validAliasWrite.Code != http.StatusOK {
		t.Fatalf("valid about alias status = %d body=%s", validAliasWrite.Code, validAliasWrite.Body.String())
	}
	assertBUG102AboutTranslation(t, ctx, pool, aboutPageID, "fr-FR", "Titre", "Corps")

	invalidCreate := invokeBUG102Changelog(t, ctx, server, claims, http.MethodPost, "", "ko-KR", "2026-08-20", "invalid create", true)
	if invalidCreate.Code != http.StatusBadRequest {
		t.Fatalf("invalid changelog create status = %d body=%s, want 400", invalidCreate.Code, invalidCreate.Body.String())
	}
	var changelogCount int
	if err = pool.QueryRow(ctx, `select count(*) from site_changelogs`).Scan(&changelogCount); err != nil || changelogCount != 0 {
		t.Fatalf("invalid create changelog count = %d err=%v, want 0", changelogCount, err)
	}

	var changelogID int64
	var changelogPublicID string
	if err = pool.QueryRow(ctx, `insert into site_changelogs(change_date,status,created_by)
		values(date '2026-08-01','published',$1) returning id,public_id`, actorID).Scan(&changelogID, &changelogPublicID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into site_changelog_translations(changelog_id,locale,title,body_markdown,status,updated_by)
		values($1,'zh-CN','原更新标题','原更新正文','published',$2)`, changelogID, actorID); err != nil {
		t.Fatal(err)
	}
	invalidUpdate := invokeBUG102Changelog(t, ctx, server, claims, http.MethodPut, changelogPublicID, "not a locale", "2026-08-21", "invalid update", false)
	if invalidUpdate.Code != http.StatusBadRequest {
		t.Fatalf("invalid changelog update status = %d body=%s, want 400", invalidUpdate.Code, invalidUpdate.Body.String())
	}
	var date string
	var status, title, body string
	if err = pool.QueryRow(ctx, `select changelog.change_date::text,changelog.status,translation.title,translation.body_markdown
		from site_changelogs changelog join site_changelog_translations translation on translation.changelog_id=changelog.id
		where changelog.id=$1 and translation.locale='zh-CN'`, changelogID).Scan(&date, &status, &title, &body); err != nil {
		t.Fatal(err)
	}
	if date != "2026-08-01" || status != "published" || title != "原更新标题" || body != "原更新正文" {
		t.Fatalf("invalid update mutated authority: date=%s status=%s title=%q body=%q", date, status, title, body)
	}
}

func invokeBUG102About(t *testing.T, ctx context.Context, server *Server, claims security.Claims, method, locale string, payload map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	var body []byte
	var err error
	if payload != nil {
		body, err = json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
	}
	request := httptest.NewRequest(method, "/api/v1/admin/site-affairs/about/"+locale, bytes.NewReader(body))
	request.SetPathValue("locale", locale)
	request = request.WithContext(context.WithValue(ctx, claimsContextKey, claims))
	response := httptest.NewRecorder()
	server.adminAboutPage(response, request)
	return response
}

func invokeBUG102Changelog(t *testing.T, ctx context.Context, server *Server, claims security.Claims, method, publicID, locale, changeDate, title string, publish bool, baseUpdatedAt ...time.Time) *httptest.ResponseRecorder {
	t.Helper()
	payload := map[string]any{
		"changeDate": changeDate, "locale": locale, "title": title, "bodyMarkdown": title + " body", "publish": publish,
	}
	if len(baseUpdatedAt) > 0 {
		payload["baseUpdatedAt"] = baseUpdatedAt[0]
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(method, "/api/v1/admin/site-affairs/changelogs/"+publicID, bytes.NewReader(body))
	request.SetPathValue("id", publicID)
	request = request.WithContext(context.WithValue(ctx, claimsContextKey, claims))
	response := httptest.NewRecorder()
	if method == http.MethodPost {
		server.adminSiteChangelogs(response, request)
	} else {
		server.adminSiteChangelogDetail(response, request)
	}
	return response
}

func assertBUG102AboutTranslation(t *testing.T, ctx context.Context, pool *pgxpool.Pool, pageID int64, locale, expectedTitle, expectedBody string) {
	t.Helper()
	var title, body string
	if err := pool.QueryRow(ctx, `select title,body_markdown from site_page_translations where page_id=$1 and locale=$2`, pageID, locale).Scan(&title, &body); err != nil {
		t.Fatal(err)
	}
	if title != expectedTitle || body != expectedBody {
		t.Fatalf("about %s = (%q,%q), want (%q,%q)", locale, title, body, expectedTitle, expectedBody)
	}
}
