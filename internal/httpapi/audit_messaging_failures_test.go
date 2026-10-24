package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestAuditMSG002DatabaseFailureIsNotMissingRecipient(t *testing.T) {
	pool, err := pgxpool.New(context.Background(), "postgres://unused@127.0.0.1:1/unused?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	pool.Close()
	server := &Server{db: pool}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/messages/conversations", strings.NewReader(`{"userId":"abc123xyz"}`))
	response := httptest.NewRecorder()
	server.startConversation(response, request)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("storage failure status=%d body=%s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "目标用户不存在") {
		t.Fatal("storage failure claimed a nonexistent user")
	}
}
