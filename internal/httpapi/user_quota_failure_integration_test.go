package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"mcmods-cn-backend/internal/security"
)

func TestUserOSSQuotaReadFailureIsNotZeroUsageIntegration(t *testing.T) {
	ctx, pool, cfg := isolatedAITestDatabase(t)
	server := &Server{cfg: cfg, db: pool}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/users/me/files/quota", nil)
	request = request.WithContext(context.WithValue(request.Context(), claimsContextKey, security.Claims{Subject: 1}))
	response := httptest.NewRecorder()
	server.userOSSFileQuota(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("healthy empty quota status: %d", response.Code)
	}
	if _, err := pool.Exec(ctx, `alter table oss_files rename to audit_preserved_oss_files`); err != nil {
		t.Fatal(err)
	}
	response = httptest.NewRecorder()
	server.userOSSFileQuota(response, request)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("quota database error was disguised as zero usage: %d", response.Code)
	}
}
