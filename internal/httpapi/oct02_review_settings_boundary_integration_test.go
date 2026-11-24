package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/security"
)

func TestOCT02UnavailableReviewPolicyNeverDisablesModerationIntegration(t *testing.T) {
	requireOCT02DatabaseIntegration(t)
	db := openNotificationTemplateTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := db.Exec(ctx, `create temp table system_settings(key text primary key,value jsonb not null)`); err != nil {
		t.Fatal(err)
	}
	if got := loadReviewConfig(ctx, db); !reflect.DeepEqual(got, defaultReviewConfig()) {
		t.Fatal("a genuinely absent policy changed normal product defaults")
	}
	checkModerationRequired := func(config reviewConfig) {
		t.Helper()
		fields := reflect.ValueOf(config)
		for i := 0; i < fields.NumField(); i++ {
			if !fields.Field(i).Bool() {
				t.Errorf("unreadable review policy did not require moderation for %s", fields.Type().Field(i).Name)
			}
		}
	}
	for _, raw := range []string{`{"blueprintCreate":false,"aiTranslation":"broken"}`, `null`, `[]`} {
		if _, err := db.Exec(ctx, `insert into system_settings(key,value) values($1,$2::jsonb) on conflict(key) do update set value=excluded.value`, reviewConfigSettingKey, raw); err != nil {
			t.Fatal(err)
		}
		checkModerationRequired(loadReviewConfig(ctx, db))
		w := httptest.NewRecorder()
		(&Server{db: db}).getReviewConfig(w, httptest.NewRequest(http.MethodGet, "/api/v1/admin/review-config", nil).WithContext(ctx))
		if w.Code != http.StatusServiceUnavailable {
			t.Errorf("malformed policy GET status=%d want503", w.Code)
		}
	}
	if _, err := db.Exec(ctx, `alter table system_settings drop column value`); err != nil {
		t.Fatal(err)
	}
	checkModerationRequired(loadReviewConfig(ctx, db))
}

func TestOCT02DuplicateNotificationTemplateKeysDoNotSilentlyOverwriteIntegration(t *testing.T) {
	requireOCT02DatabaseIntegration(t)
	db := openNotificationTemplateTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := db.Exec(ctx, `create temp table system_settings(key text primary key,value jsonb not null,updated_by bigint,updated_at timestamptz)`); err != nil {
		t.Fatal(err)
	}
	templates := defaultNotificationTemplateConfig()
	template := templates.Templates[0]
	second := template
	second.Code = " " + template.Code + " "
	raw, err := json.Marshal(notificationTemplateConfig{Templates: []notificationTemplateDefinition{template, second}})
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPut, "/api/v1/admin/notification-templates", strings.NewReader(string(raw)))
	r = r.WithContext(context.WithValue(ctx, claimsContextKey, security.Claims{Subject: 1}))
	w := httptest.NewRecorder()
	(&Server{db: db}).updateNotificationTemplates(w, r)
	if w.Code != http.StatusBadRequest {
		t.Errorf("duplicate normalized template IDs status=%d want400", w.Code)
	}
	var stored int
	if err := db.QueryRow(ctx, `select count(*) from system_settings`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != 0 {
		t.Fatal("invalid template list changed persistent configuration")
	}
}

func TestOCT02ConcurrentNotificationTemplateSavesKeepDistinctSourceVersionsIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	db := newOCT02IsolatedDatabase(t, ctx)
	var actorID int64
	if err := db.QueryRow(ctx, `insert into users(username,email,password_hash) values('oct02-template-reviewer','template-reviewer@example.invalid','synthetic-unused') returning id`).Scan(&actorID); err != nil {
		t.Fatal(err)
	}
	configuration := defaultNotificationTemplateConfig()
	raw, err := json.Marshal(configuration)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(ctx, `insert into system_settings(key,value) values($1,$2::jsonb) on conflict(key) do update set value=excluded.value`, notificationTemplatesSettingKey, raw); err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	var group sync.WaitGroup
	statuses := make(chan int, 2)
	for _, title := range []string{"First synthetic template", "Second synthetic template"} {
		requestConfig := defaultNotificationTemplateConfig()
		template := requestConfig.Templates[0]
		template.Translations["en-US"] = localizedNotificationTemplate{Title: title, Body: "Synthetic unchanged body"}
		requestConfig.Templates[0] = template
		body, err := json.Marshal(requestConfig)
		if err != nil {
			t.Fatal(err)
		}
		group.Go(func() {
			<-started
			r := httptest.NewRequest(http.MethodPut, "/api/v1/admin/notification-templates", strings.NewReader(string(body)))
			r = r.WithContext(context.WithValue(ctx, claimsContextKey, security.Claims{Subject: actorID}))
			w := httptest.NewRecorder()
			(&Server{db: db}).updateNotificationTemplates(w, r)
			statuses <- w.Code
		})
	}
	close(started)
	group.Wait()
	close(statuses)
	for status := range statuses {
		if status != http.StatusOK {
			t.Fatalf("concurrent template save status=%d want200", status)
		}
	}
	current, err := loadNotificationTemplateConfig(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if current.Templates[0].Version != 3 {
		t.Fatalf("two different committed source edits share version %d", current.Templates[0].Version)
	}
	// A pool with one connection must also save without a nested pool acquisition.
	pc := db.Config()
	pc.MaxConns = 1
	pc.MinConns = 0
	single, err := pgxpool.NewWithConfig(ctx, pc)
	if err != nil {
		t.Fatal(err)
	}
	defer single.Close()
	r := httptest.NewRequest(http.MethodPut, "/api/v1/admin/notification-templates", strings.NewReader(string(raw)))
	r = r.WithContext(context.WithValue(ctx, claimsContextKey, security.Claims{Subject: actorID}))
	w := httptest.NewRecorder()
	(&Server{db: single}).updateNotificationTemplates(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("single connection template save status=%d want200", w.Code)
	}
}

func TestReviewPolicyReplacementRequiresEveryBooleanIntegration(t *testing.T) {
	requireOCT02DatabaseIntegration(t)
	db := openNotificationTemplateTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := db.Exec(ctx, `create temp table system_settings(
		key text primary key, value jsonb not null, updated_by bigint not null, updated_at timestamptz not null)`); err != nil {
		t.Fatal(err)
	}
	handler := &Server{db: db}
	put := func(raw string) int {
		t.Helper()
		r := httptest.NewRequest(http.MethodPut, "/api/v1/admin/config/reviews", strings.NewReader(raw))
		r = r.WithContext(context.WithValue(ctx, claimsContextKey, security.Claims{Subject: 1}))
		w := httptest.NewRecorder()
		handler.updateReviewConfig(w, r)
		return w.Code
	}
	intended := requireAllContentReviews()
	intended.BlueprintCreate = false
	full, err := json.Marshal(intended)
	if err != nil {
		t.Fatal(err)
	}
	if status := put(string(full)); status != http.StatusOK {
		t.Fatalf("complete replacement with explicit false returned %d", status)
	}
	for _, raw := range []string{`{}`, `null`, `{"blueprintCreate":false}`,
		strings.Replace(string(full), `"modEdit":true`, `"modEdit":null`, 1),
		strings.Replace(string(full), `"modEdit":true`, `"nonexistent":true`, 1)} {
		if status := put(raw); status != http.StatusBadRequest {
			t.Errorf("incomplete or non-boolean replacement returned %d", status)
		}
		var unchanged bool
		if err := db.QueryRow(ctx, `select value=$2::jsonb from system_settings where key=$1`, reviewConfigSettingKey, string(full)).Scan(&unchanged); err != nil {
			t.Fatal(err)
		}
		if !unchanged {
			t.Fatal("incomplete replacement changed previously saved moderation rules")
		}
	}
}
