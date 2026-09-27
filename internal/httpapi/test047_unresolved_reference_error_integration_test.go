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

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
)

func TestTEST047UnresolvedReferenceHandlersFailClosedOnDatabaseErrorsIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify unresolved-reference database errors")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	poolConfig, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
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
	if err = database.InstallEphemeralSchema(ctx, pool); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if dropErr := database.DropEphemeralSchema(context.Background(), pool); dropErr != nil {
			t.Errorf("drop TEST-047 ephemeral schema: %v", dropErr)
		}
	}()

	server := &Server{db: pool}
	if _, err = pool.Exec(ctx, `alter table unresolved_reference_catalog rename column source_label to source_label_broken`); err != nil {
		t.Fatal(err)
	}
	page := httptest.NewRecorder()
	server.adminUnresolvedReferences(page, httptest.NewRequest(http.MethodGet,
		"/api/v1/admin/unresolved-references?status=pending&limit=50", nil).WithContext(ctx))
	assertTEST047FailureEnvelope(t, page, "failed to load unresolved references")
	if _, err = pool.Exec(ctx, `alter table unresolved_reference_catalog rename column source_label_broken to source_label`); err != nil {
		t.Fatal(err)
	}

	if _, err = pool.Exec(ctx, `alter table resource_kinds rename column code to code_broken`); err != nil {
		t.Fatal(err)
	}
	types := httptest.NewRecorder()
	server.adminUnresolvedReferenceTypes(types, httptest.NewRequest(http.MethodGet,
		"/api/v1/admin/unresolved-reference-types", nil).WithContext(ctx))
	assertTEST047FailureEnvelope(t, types, "failed to load unresolved reference types")
}

func assertTEST047FailureEnvelope(t *testing.T, response *httptest.ResponseRecorder, message string) {
	t.Helper()
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d body=%s, want 500", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), message) || strings.Contains(response.Body.String(), `"items"`) {
		t.Fatalf("failure response exposed partial data or lost its diagnostic: %s", response.Body.String())
	}
}
