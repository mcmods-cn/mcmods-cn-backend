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

func TestSiteAffairsEditsRequireCurrentAboutRevisionAndChangelogTimestampIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify site-affairs optimistic concurrency")
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
			t.Errorf("drop BUG-104 ephemeral schema: %v", dropErr)
		}
	}()

	suffix := time.Now().UnixNano()
	var actorID, aboutPageID int64
	if err = pool.QueryRow(ctx, `insert into users(username,email,password_hash,status)
		values($1,$2,'not-used','active') returning id`, fmt.Sprintf("bug104_admin_%d", suffix), fmt.Sprintf("bug104_admin_%d@example.test", suffix)).Scan(&actorID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `insert into site_pages(code,status,updated_by) values('about','published',$1)
		on conflict(code) do update set status='published',updated_by=excluded.updated_by returning id`, actorID).Scan(&aboutPageID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into site_page_translations(page_id,locale,title,body_markdown,status,revision,updated_by)
		values($1,'zh-CN','baseline','baseline body','published',1,$2)
		on conflict(page_id,locale) do update set title=excluded.title,body_markdown=excluded.body_markdown,status=excluded.status,revision=1`, aboutPageID, actorID); err != nil {
		t.Fatal(err)
	}

	server := &Server{db: pool}
	claims := security.Claims{Subject: actorID}
	firstAbout := invokeBUG104Mutation(t, ctx, claims, http.MethodPut, "/api/v1/admin/site-affairs/about/zh-CN", map[string]any{
		"title": "first", "bodyMarkdown": "first body", "publish": true, "baseRevision": 1,
	}, func(response *httptest.ResponseRecorder, request *http.Request) {
		request.SetPathValue("locale", "zh-CN")
		server.adminAboutPage(response, request)
	})
	if firstAbout.Code != http.StatusOK {
		t.Fatalf("first about edit status=%d body=%s", firstAbout.Code, firstAbout.Body.String())
	}
	firstAboutRevision := BUG104Int64DataField(t, firstAbout, "revision")
	if firstAboutRevision != 2 {
		t.Fatalf("first about revision=%d want 2", firstAboutRevision)
	}
	staleAbout := invokeBUG104Mutation(t, ctx, claims, http.MethodPut, "/api/v1/admin/site-affairs/about/zh-CN", map[string]any{
		"title": "stale", "bodyMarkdown": "stale body", "publish": false, "baseRevision": 1,
	}, func(response *httptest.ResponseRecorder, request *http.Request) {
		request.SetPathValue("locale", "zh-CN")
		server.adminAboutPage(response, request)
	})
	assertBUG104Conflict(t, staleAbout, "SITE_PAGE_EDIT_CONFLICT", 2, "first")
	assertBUG104AboutContent(t, ctx, pool, aboutPageID, "first", "first body", "published", 2)
	retryAbout := invokeBUG104Mutation(t, ctx, claims, http.MethodPut, "/api/v1/admin/site-affairs/about/zh-CN", map[string]any{
		"title": "retry", "bodyMarkdown": "retry body", "publish": false, "baseRevision": firstAboutRevision,
	}, func(response *httptest.ResponseRecorder, request *http.Request) {
		request.SetPathValue("locale", "zh-CN")
		server.adminAboutPage(response, request)
	})
	if retryAbout.Code != http.StatusOK || BUG104Int64DataField(t, retryAbout, "revision") != 3 {
		t.Fatalf("about retry status=%d body=%s", retryAbout.Code, retryAbout.Body.String())
	}

	var changelogID int64
	var changelogPublicID string
	var changelogBaseline time.Time
	if err = pool.QueryRow(ctx, `insert into site_changelogs(change_date,status,created_by)
		values(date '2026-08-01','published',$1) returning id,public_id,updated_at`, actorID).Scan(&changelogID, &changelogPublicID, &changelogBaseline); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into site_changelog_translations(changelog_id,locale,title,body_markdown,status,updated_by)
		values($1,'zh-CN','baseline log','baseline log body','published',$2)`, changelogID, actorID); err != nil {
		t.Fatal(err)
	}
	firstChangelog := invokeBUG104Mutation(t, ctx, claims, http.MethodPut, "/api/v1/admin/site-affairs/changelogs/"+changelogPublicID, map[string]any{
		"changeDate": "2026-08-02", "locale": "zh-CN", "title": "first log", "bodyMarkdown": "first log body", "publish": true, "baseUpdatedAt": changelogBaseline,
	}, func(response *httptest.ResponseRecorder, request *http.Request) {
		request.SetPathValue("id", changelogPublicID)
		server.adminSiteChangelogDetail(response, request)
	})
	if firstChangelog.Code != http.StatusOK {
		t.Fatalf("first changelog edit status=%d body=%s", firstChangelog.Code, firstChangelog.Body.String())
	}
	firstChangelogUpdatedAt := BUG104TimeDataField(t, firstChangelog, "updatedAt")
	if !firstChangelogUpdatedAt.After(changelogBaseline) {
		t.Fatalf("first changelog updatedAt=%s baseline=%s", firstChangelogUpdatedAt, changelogBaseline)
	}
	staleChangelog := invokeBUG104Mutation(t, ctx, claims, http.MethodPut, "/api/v1/admin/site-affairs/changelogs/"+changelogPublicID, map[string]any{
		"changeDate": "2026-08-03", "locale": "zh-CN", "title": "stale log", "bodyMarkdown": "stale log body", "publish": false, "baseUpdatedAt": changelogBaseline,
	}, func(response *httptest.ResponseRecorder, request *http.Request) {
		request.SetPathValue("id", changelogPublicID)
		server.adminSiteChangelogDetail(response, request)
	})
	assertBUG104Conflict(t, staleChangelog, "SITE_CHANGELOG_EDIT_CONFLICT", 0, "first log")
	assertBUG104ChangelogContent(t, ctx, pool, changelogID, "2026-08-02", "first log", "first log body", "published", firstChangelogUpdatedAt)
	retryChangelog := invokeBUG104Mutation(t, ctx, claims, http.MethodPut, "/api/v1/admin/site-affairs/changelogs/"+changelogPublicID, map[string]any{
		"changeDate": "2026-08-04", "locale": "zh-CN", "title": "retry log", "bodyMarkdown": "retry log body", "publish": false, "baseUpdatedAt": firstChangelogUpdatedAt,
	}, func(response *httptest.ResponseRecorder, request *http.Request) {
		request.SetPathValue("id", changelogPublicID)
		server.adminSiteChangelogDetail(response, request)
	})
	if retryChangelog.Code != http.StatusOK || !BUG104TimeDataField(t, retryChangelog, "updatedAt").After(firstChangelogUpdatedAt) {
		t.Fatalf("changelog retry status=%d body=%s", retryChangelog.Code, retryChangelog.Body.String())
	}
}

func invokeBUG104Mutation(t *testing.T, ctx context.Context, claims security.Claims, method, path string, payload map[string]any, handler func(*httptest.ResponseRecorder, *http.Request)) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(method, path, bytes.NewReader(body))
	request = request.WithContext(context.WithValue(ctx, claimsContextKey, claims))
	response := httptest.NewRecorder()
	handler(response, request)
	return response
}

func assertBUG104Conflict(t *testing.T, response *httptest.ResponseRecorder, expectedCode string, expectedRevision int64, expectedTitle string) {
	t.Helper()
	if response.Code != http.StatusConflict {
		t.Fatalf("conflict status=%d body=%s", response.Code, response.Body.String())
	}
	var envelope struct {
		Code    string `json:"code"`
		Details struct {
			Revision     int64                               `json:"revision"`
			Title        string                              `json:"title"`
			Translations map[string]siteChangelogTranslation `json:"translations"`
		} `json:"details"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Code != expectedCode || expectedRevision > 0 && envelope.Details.Revision != expectedRevision {
		t.Fatalf("conflict envelope=%+v want code=%s revision=%d", envelope, expectedCode, expectedRevision)
	}
	if expectedRevision > 0 && envelope.Details.Title != expectedTitle {
		t.Fatalf("about conflict title=%q want %q", envelope.Details.Title, expectedTitle)
	}
	if expectedRevision == 0 && envelope.Details.Translations["zh-CN"].Title != expectedTitle {
		t.Fatalf("changelog conflict translations=%+v want title %q", envelope.Details.Translations, expectedTitle)
	}
}

func BUG104Int64DataField(t *testing.T, response *httptest.ResponseRecorder, name string) int64 {
	t.Helper()
	var envelope struct {
		Data map[string]json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	var value int64
	if err := json.Unmarshal(envelope.Data[name], &value); err != nil {
		t.Fatalf("decode data.%s: %v body=%s", name, err, response.Body.String())
	}
	return value
}

func BUG104TimeDataField(t *testing.T, response *httptest.ResponseRecorder, name string) time.Time {
	t.Helper()
	var envelope struct {
		Data map[string]json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	var value time.Time
	if err := json.Unmarshal(envelope.Data[name], &value); err != nil {
		t.Fatalf("decode data.%s: %v body=%s", name, err, response.Body.String())
	}
	return value
}

func assertBUG104AboutContent(t *testing.T, ctx context.Context, pool *pgxpool.Pool, pageID int64, expectedTitle, expectedBody, expectedStatus string, expectedRevision int64) {
	t.Helper()
	var title, body, status string
	var revision int64
	if err := pool.QueryRow(ctx, `select title,body_markdown,status,revision from site_page_translations where page_id=$1 and locale='zh-CN'`, pageID).Scan(&title, &body, &status, &revision); err != nil {
		t.Fatal(err)
	}
	if title != expectedTitle || body != expectedBody || status != expectedStatus || revision != expectedRevision {
		t.Fatalf("about content=(%q,%q,%q,%d), want (%q,%q,%q,%d)", title, body, status, revision, expectedTitle, expectedBody, expectedStatus, expectedRevision)
	}
}

func assertBUG104ChangelogContent(t *testing.T, ctx context.Context, pool *pgxpool.Pool, changelogID int64, expectedDate, expectedTitle, expectedBody, expectedStatus string, expectedUpdatedAt time.Time) {
	t.Helper()
	var date, title, body, status string
	var updatedAt time.Time
	if err := pool.QueryRow(ctx, `select changelog.change_date::text,translation.title,translation.body_markdown,translation.status,changelog.updated_at
		from site_changelogs changelog join site_changelog_translations translation on translation.changelog_id=changelog.id
		where changelog.id=$1 and translation.locale='zh-CN'`, changelogID).Scan(&date, &title, &body, &status, &updatedAt); err != nil {
		t.Fatal(err)
	}
	if date != expectedDate || title != expectedTitle || body != expectedBody || status != expectedStatus || !updatedAt.Equal(expectedUpdatedAt) {
		t.Fatalf("changelog=(%s,%q,%q,%q,%s), want (%s,%q,%q,%q,%s)", date, title, body, status, updatedAt, expectedDate, expectedTitle, expectedBody, expectedStatus, expectedUpdatedAt)
	}
}
