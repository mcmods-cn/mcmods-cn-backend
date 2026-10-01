package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"mcmods-cn-backend/internal/security"
)

func TestRolePermissionReadFailureDoesNotBecomeEmptyPermissions(t *testing.T) {
	pool, err := pgxpool.New(context.Background(), "postgres://fixture:fixture@127.0.0.1:1/unused")
	if err != nil {
		t.Fatal(err)
	}
	pool.Close()
	server := &Server{db: pool}
	entries, err := server.rolePermissionEntries(context.Background(), "fixture")
	if err == nil || entries != nil {
		t.Fatalf("database outage became an empty successful role: entries=%v err=%v", entries, err)
	}
}

func TestPermissionCreationRollsBackWhenAuditFailsIntegration(t *testing.T) {
	ctx, server, _, _, _, _ := newRoleTrackFixture(t)
	code := "apia.audit." + randomHex(8)
	t.Cleanup(func() {
		if _, err := server.db.Exec(context.Background(), `delete from permissions where code=$1`, code); err != nil {
			t.Errorf("allocated permission cleanup: %v", err)
		}
	})
	request := httptest.NewRequest(http.MethodPost, "/permissions", strings.NewReader(`{"code":"`+code+`","module":"apia","name":"Fixture"}`))
	// A nonexistent synthetic actor makes the real audit FK reject the write;
	// the permission itself is valid and must roll back in the same transaction.
	request = request.WithContext(context.WithValue(ctx, claimsContextKey, security.Claims{Subject: -1}))
	response := httptest.NewRecorder()
	server.createPermission(response, request)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("audit failure was ignored: %d %s", response.Code, response.Body.String())
	}
	var count int
	if err := server.db.QueryRow(ctx, `select count(*) from permissions where code=$1`, code).Scan(&count); err != nil || count != 0 {
		t.Fatalf("audit failure left a permission committed: count=%d err=%v", count, err)
	}
}
