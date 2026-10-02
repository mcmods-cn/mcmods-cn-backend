package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestSiteChangelogHandlersFailClosedOnSchemaErrorsIntegration(t *testing.T) {
	databaseURL := strings.TrimSpace(os.Getenv("MCMODS_TEST_DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("set MCMODS_TEST_DATABASE_URL to verify site changelog read failures")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	poolConfig, err := pgxpool.ParseConfig(databaseURL)
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
	if _, err = pool.Exec(ctx, `
		create temporary table site_changelogs(
			id bigint primary key,public_id text not null,change_date date not null,status text not null,updated_at timestamptz not null
		);
		create temporary table site_changelog_translations(
			changelog_id bigint not null,locale text not null,title text not null,body_markdown text not null,status text not null
		);
		insert into site_changelogs values(1,'arch029',date '2026-08-23','published',now());
		insert into site_changelog_translations values(1,'zh-CN','标题','正文','published')
	`); err != nil {
		t.Fatal(err)
	}
	server := &Server{db: pool}

	assertHandlerStatus(t, http.StatusOK, func(response *httptest.ResponseRecorder) {
		request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/site-affairs/changelogs/arch029", nil).WithContext(ctx)
		request.SetPathValue("id", "arch029")
		server.adminSiteChangelogDetail(response, request)
	})
	assertHandlerStatus(t, http.StatusOK, func(response *httptest.ResponseRecorder) {
		server.publicSiteChangelogs(response, httptest.NewRequest(http.MethodGet, "/api/v1/site-affairs/changelogs?locale=zh-CN", nil).WithContext(ctx))
	})
	assertHandlerStatus(t, http.StatusOK, func(response *httptest.ResponseRecorder) {
		server.adminSiteChangelogList(response, httptest.NewRequest(http.MethodGet, "/api/v1/admin/site-affairs/changelogs", nil).WithContext(ctx))
	})

	if _, err = pool.Exec(ctx, `alter table site_changelog_translations rename column body_markdown to arch029_broken_body`); err != nil {
		t.Fatal(err)
	}
	assertHandlerStatus(t, http.StatusInternalServerError, func(response *httptest.ResponseRecorder) {
		request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/site-affairs/changelogs/arch029", nil).WithContext(ctx)
		request.SetPathValue("id", "arch029")
		server.adminSiteChangelogDetail(response, request)
	})
	assertHandlerStatus(t, http.StatusInternalServerError, func(response *httptest.ResponseRecorder) {
		server.publicSiteChangelogs(response, httptest.NewRequest(http.MethodGet, "/api/v1/site-affairs/changelogs?locale=zh-CN", nil).WithContext(ctx))
	})
	assertHandlerStatus(t, http.StatusInternalServerError, func(response *httptest.ResponseRecorder) {
		server.adminSiteChangelogList(response, httptest.NewRequest(http.MethodGet, "/api/v1/admin/site-affairs/changelogs", nil).WithContext(ctx))
	})
}
