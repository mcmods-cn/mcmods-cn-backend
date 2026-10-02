package httpapi

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/querycache"
	"mcmods-cn-backend/internal/security"
)

func TestOCT02PermissionMutationRollsBackWhenAuditCannotPersistIntegration(t *testing.T) {
	f := newTEST013Fixture(t)
	if _, err := f.db.Exec(f.ctx, `alter table permission_audit_logs add constraint oct02_reject_permission_audit
	 check(action<>'upsert_permission_node') not valid`); err != nil {
		t.Fatal(err)
	}
	cache := querycache.New(config.RedisConfig{})
	t.Cleanup(func() { _ = cache.Close() })
	s := &Server{db: f.db, cache: cache}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/permissions", bytes.NewBufferString(`{"code":"oct02.audit.required","module":"fixture","name":"Synthetic audit test","description":"fixture"}`))
	request = request.WithContext(context.WithValue(f.ctx, claimsContextKey, security.Claims{Subject: f.userIDs[f.editor]}))
	response := httptest.NewRecorder()
	s.createPermission(response, request)
	if response.Code != http.StatusInternalServerError {
		t.Errorf("audit rejection returned status=%d, want 500", response.Code)
	}
	var saved bool
	if err := f.db.QueryRow(f.ctx, `select exists(select 1 from permissions where code='oct02.audit.required')`).Scan(&saved); err != nil {
		t.Fatal(err)
	}
	if saved {
		t.Error("permission mutation committed without its required audit")
	}
	if _, err := f.db.Exec(f.ctx, `alter table permission_audit_logs drop constraint oct02_reject_permission_audit`); err != nil {
		t.Fatal(err)
	}
	request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/permissions", bytes.NewBufferString(`{"code":"oct02.audit.required","module":"fixture","name":"Original audited name","description":"fixture"}`))
	request = request.WithContext(context.WithValue(f.ctx, claimsContextKey, security.Claims{Subject: f.userIDs[f.editor]}))
	response = httptest.NewRecorder()
	s.createPermission(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("audited creation returned status=%d", response.Code)
	}
	var audits int
	if err := f.db.QueryRow(f.ctx, `select count(*) from permission_audit_logs where action='upsert_permission_node' and payload->>'code'='oct02.audit.required'`).Scan(&audits); err != nil || audits != 1 {
		t.Fatalf("required audit count=%d err=%v", audits, err)
	}
	if _, err := f.db.Exec(f.ctx, `alter table permission_audit_logs add constraint oct02_reject_permission_audit check(action<>'upsert_permission_node') not valid`); err != nil {
		t.Fatal(err)
	}
	request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/permissions", bytes.NewBufferString(`{"code":"oct02.audit.required","module":"fixture","name":"Unaudited replacement","description":"fixture"}`))
	request = request.WithContext(context.WithValue(f.ctx, claimsContextKey, security.Claims{Subject: f.userIDs[f.editor]}))
	response = httptest.NewRecorder()
	s.createPermission(response, request)
	if response.Code != http.StatusInternalServerError {
		t.Errorf("audit-rejected update returned status=%d", response.Code)
	}
	var name string
	if err := f.db.QueryRow(f.ctx, `select name from permissions where code='oct02.audit.required'`).Scan(&name); err != nil || name != "Original audited name" {
		t.Errorf("audit-rejected update changed previous permission name=%q err=%v", name, err)
	}
}
