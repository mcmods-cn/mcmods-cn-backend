package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"mcmods-cn-backend/internal/security"
)

func TestLogCleanupReportsSavedPolicyAndPartialFailureIntegration(t *testing.T) {
	ctx, pool, _ := isolatedAITestDatabase(t)
	if _, err := pool.Exec(ctx, `insert into permission_audit_logs(action,created_at) values('synthetic-retention',now()-interval '5 years');
		insert into app_logs(category,action,created_at) values('system','synthetic-retention',now()-interval '5 years')`); err != nil {
		t.Fatal(err)
	}
	server := &Server{db: pool}
	var actorID int64
	if err := pool.QueryRow(ctx, `insert into users(username,email,password_hash) values('synthetic-cleanup','synthetic-cleanup@example.invalid','synthetic') returning id`).Scan(&actorID); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPut, "/", strings.NewReader(`{"enabled":true,"defaultDays":30,"categoryDays":{"permission_change":30,"system":30}}`))
	request = request.WithContext(context.WithValue(request.Context(), claimsContextKey, security.Claims{Subject: actorID}))
	response := httptest.NewRecorder()
	server.updateLogConfig(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("immutable audit cleanup failure reported %d instead of partial failure", response.Code)
	}
	var envelope struct {
		Code    string `json:"code"`
		Details struct {
			Saved   bool             `json:"saved"`
			Deleted map[string]int64 `json:"deleted"`
		} `json:"details"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Code != "LOG_CLEANUP_FAILED" || !envelope.Details.Saved || envelope.Details.Deleted["system"] != 1 {
		t.Fatalf("partial outcome was lost: code=%s saved=%t deleted=%v", envelope.Code, envelope.Details.Saved, envelope.Details.Deleted)
	}
	var auditRows, settingsRows int
	if err := pool.QueryRow(ctx, `select (select count(*) from permission_audit_logs where action='synthetic-retention'),
		(select count(*) from system_settings where key='logs.retention' and (value->>'defaultDays')::int=30)`).Scan(&auditRows, &settingsRows); err != nil {
		t.Fatal(err)
	}
	if auditRows != 1 || settingsRows != 1 {
		t.Fatalf("saved policy/audit retention changed: audits=%d settings=%d", auditRows, settingsRows)
	}
}
