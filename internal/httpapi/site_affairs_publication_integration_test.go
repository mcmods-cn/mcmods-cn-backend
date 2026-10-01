package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"mcmods-cn-backend/internal/security"
)

func TestAboutDraftDoesNotUnpublishOtherLocalesIntegration(t *testing.T) {
	ctx, pool, cfg := isolatedAITestDatabase(t)
	var userID int64
	if err := pool.QueryRow(ctx, `insert into users(username,email,password_hash) values('site-editor','site-editor@example.invalid','synthetic') returning id`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	s := &Server{db: pool, cfg: cfg}
	save := func(locale string, publish bool) int {
		body, err := json.Marshal(updateSitePageRequest{Title: "Synthetic " + locale, BodyMarkdown: "Synthetic content", Publish: publish})
		if err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest(http.MethodPut, "/api/v1/admin/site-affairs/about/"+locale, bytes.NewReader(body))
		r.SetPathValue("locale", locale)
		r = r.WithContext(context.WithValue(ctx, claimsContextKey, security.Claims{Subject: userID}))
		w := httptest.NewRecorder()
		s.adminAboutPage(w, r)
		return w.Code
	}
	if code := save("zh-CN", true); code != 200 {
		t.Fatalf("publish returned %d", code)
	}
	if code := save("en-US", false); code != 200 {
		t.Fatalf("draft returned %d", code)
	}
	w := httptest.NewRecorder()
	s.publicAboutPage(w, httptest.NewRequest(http.MethodGet, "/api/v1/site-affairs/about?locale=zh-CN", nil))
	if w.Code != 200 {
		t.Fatalf("another language draft hid published Chinese: status=%d", w.Code)
	}
	if code := save("not-a-supported-locale", true); code != http.StatusBadRequest {
		t.Errorf("unsupported admin locale returned %d, want 400", code)
	}
	var title string
	if err := pool.QueryRow(ctx, `select title from site_page_translations where page_id=(select id from site_pages where code='about') and locale='zh-CN'`).Scan(&title); err != nil || title != "Synthetic zh-CN" {
		t.Fatalf("unsupported locale changed Chinese content: title=%q err=%v", title, err)
	}
	// The editor sees its translation's draft state even while the parent page
	// remains published for a different language.
	r := httptest.NewRequest(http.MethodGet, "/admin/about/en-US", nil)
	r.SetPathValue("locale", "en-US")
	w = httptest.NewRecorder()
	s.adminAboutPage(w, r)
	var envelope struct{ Data struct{ Status string } }
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil || envelope.Data.Status != "draft" {
		t.Fatalf("draft editor has incorrect status: status=%q err=%v", envelope.Data.Status, err)
	}
	if code := save("zh-CN", false); code != 200 {
		t.Fatal(code)
	}
	w = httptest.NewRecorder()
	s.publicAboutPage(w, httptest.NewRequest(http.MethodGet, "/site-affairs/about", nil))
	if w.Code != 404 {
		t.Fatalf("all translations are drafts but page returned %d", w.Code)
	}
}
