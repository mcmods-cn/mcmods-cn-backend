package httpapi

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
	"mcmods-cn-backend/internal/security"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAIDailyBalanceDoesNotReportUnusedQuotaWhenStoreUnavailable(t *testing.T) {
	// A lazy, closed pool never dials this loopback address or any real service.
	pool, err := pgxpool.New(context.Background(), "postgres://test:test@127.0.0.1:1/test?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	pool.Close()
	server := &Server{db: pool}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/ai/daily-balance", nil)
	request = request.WithContext(context.WithValue(request.Context(), claimsContextKey, security.Claims{Subject: 1, PermissionRules: []security.PermissionRule{{Code: "user.ai.daily_token_limit.1000", Allow: true, Priority: 100}}}))
	response := httptest.NewRecorder()
	server.aiDailyBalance(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("unavailable balance returned %d, want 503", response.Code)
	}
	if strings.Contains(response.Body.String(), "remainingTokens") {
		t.Fatal("storage error returned fabricated quota balance")
	}
}
