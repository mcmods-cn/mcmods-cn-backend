package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/security"
)

func TestBUG053LogReadsAndCleanupFailuresAreNotSuccessfulEmptyResultsIntegration(t *testing.T) {
	databaseURL := strings.TrimSpace(os.Getenv("MCMODS_TEST_DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("set MCMODS_TEST_DATABASE_URL to verify log failure observability")
	}
	poolConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	poolConfig.MaxConns = 1
	db, err := pgxpool.NewWithConfig(context.Background(), poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	ctx := context.Background()
	if _, err = db.Exec(ctx, `
		create temp table system_settings(key text primary key,value jsonb not null,updated_by bigint,updated_at timestamptz not null default now());
		create temp table app_logs(id bigserial primary key,category text not null,created_at timestamptz not null default now());
		create temp table permission_audit_logs(id bigserial primary key,created_at timestamptz not null default now());
		create temp table user_login_logs(id bigserial primary key,created_at timestamptz not null default now());
		create temp table oss_upload_logs(id bigserial primary key,created_at timestamptz not null default now());
		create temp table oss_scan_logs(id bigserial primary key,created_at timestamptz not null default now());
		create function pg_temp.reject_permission_log_delete() returns trigger language plpgsql as $$
		begin raise exception 'injected permission log delete failure'; end $$;
		create trigger reject_permission_log_delete before delete on permission_audit_logs
		for each statement execute function pg_temp.reject_permission_log_delete();
		insert into app_logs(category,created_at) values('api_access',now()-interval '10 days');
		insert into permission_audit_logs(created_at) values(now()-interval '10 days');
		insert into user_login_logs(created_at) values(now()-interval '10 days');
		insert into oss_upload_logs(created_at) values(now()-interval '10 days');
		insert into oss_scan_logs(created_at) values(now()-interval '10 days')`); err != nil {
		t.Fatal(err)
	}
	server := &Server{db: db}

	readResponse := httptest.NewRecorder()
	server.adminLogs(readResponse, httptest.NewRequest(http.MethodGet, "/api/v1/admin/logs?category=api_access", nil))
	if readResponse.Code != http.StatusInternalServerError {
		t.Fatalf("broken log projection returned %d instead of 500: %s", readResponse.Code, readResponse.Body.String())
	}

	payload := logRetentionConfig{
		Enabled: true, DefaultDays: 1,
		CategoryDays: map[string]int{
			"api_access": 1, "permission_change": 1, "login_security": 1, "file_upload": 1, "file_scan": 1,
		},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPut, "/api/v1/admin/logs/config", bytes.NewReader(raw))
	request = request.WithContext(context.WithValue(request.Context(), claimsContextKey, security.Claims{Subject: 5301}))
	response := httptest.NewRecorder()
	server.updateLogConfig(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("policy save returned %d: %s", response.Code, response.Body.String())
	}
	var rowsAfterSave int
	if err = db.QueryRow(ctx, `select count(*) from app_logs where category='api_access'`).Scan(&rowsAfterSave); err != nil || rowsAfterSave != 1 {
		t.Fatalf("policy save synchronously deleted logs: rows=%d err=%v", rowsAfterSave, err)
	}

	cleanupResponse := httptest.NewRecorder()
	server.runLogCleanup(cleanupResponse, httptest.NewRequest(http.MethodPost, "/api/v1/admin/logs/cleanup", nil))

	var envelope apiResponse
	if err = json.Unmarshal(cleanupResponse.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if cleanupResponse.Code != http.StatusInternalServerError || envelope.Code != "LOG_CLEANUP_FAILED" {
		t.Fatalf("cleanup failure returned %d/%q instead of stable 500: %s", cleanupResponse.Code, envelope.Code, cleanupResponse.Body.String())
	}
	details, ok := envelope.Details.(map[string]any)
	if !ok {
		t.Fatalf("cleanup failure omitted structured details: %#v", envelope.Details)
	}
	failed, ok := details["failedCategories"].([]any)
	if !ok || len(failed) != 1 || failed[0] != "permission_change" {
		t.Errorf("failed categories = %#v, want permission_change", details["failedCategories"])
	}
	deleted, ok := details["deleted"].(map[string]any)
	if !ok || deleted["api_access"] != float64(1) || deleted["login_security"] != float64(1) || deleted["file_upload"] != float64(1) || deleted["file_scan"] != float64(1) {
		t.Errorf("partial cleanup result is incomplete: %#v", details["deleted"])
	}

	var persisted bool
	if err = db.QueryRow(ctx, `select (value->>'enabled')::boolean from system_settings where key='logs.retention'`).Scan(&persisted); err != nil || !persisted {
		t.Errorf("saved policy was hidden by cleanup failure: %v/%v", persisted, err)
	}
	var permissionRows int
	if err = db.QueryRow(ctx, `select count(*) from permission_audit_logs`).Scan(&permissionRows); err != nil || permissionRows != 1 {
		t.Errorf("failed table rows = %d/%v, want 1", permissionRows, err)
	}

	if _, err = db.Exec(ctx, `update system_settings set value='[]'::jsonb where key='logs.retention';
		insert into app_logs(category,created_at) values('api_access',now()-interval '10 days')`); err != nil {
		t.Fatal(err)
	}
	configResponse := httptest.NewRecorder()
	server.getLogConfig(configResponse, httptest.NewRequest(http.MethodGet, "/api/v1/admin/logs/config", nil))
	if configResponse.Code != http.StatusInternalServerError || !strings.Contains(configResponse.Body.String(), "LOG_RETENTION_CONFIG_READ_FAILED") {
		t.Fatalf("corrupt retention config read returned %d: %s", configResponse.Code, configResponse.Body.String())
	}
	invalidCleanupResponse := httptest.NewRecorder()
	server.runLogCleanup(invalidCleanupResponse, httptest.NewRequest(http.MethodPost, "/api/v1/admin/logs/cleanup", nil))
	if invalidCleanupResponse.Code != http.StatusInternalServerError || !strings.Contains(invalidCleanupResponse.Body.String(), "LOG_RETENTION_CONFIG_READ_FAILED") {
		t.Fatalf("cleanup with corrupt retention config returned %d: %s", invalidCleanupResponse.Code, invalidCleanupResponse.Body.String())
	}
	var retainedAfterConfigFailure int
	if err = db.QueryRow(ctx, `select count(*) from app_logs where category='api_access'`).Scan(&retainedAfterConfigFailure); err != nil || retainedAfterConfigFailure != 1 {
		t.Fatalf("corrupt config cleanup changed logs: rows=%d err=%v", retainedAfterConfigFailure, err)
	}
}
