package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/security"
)

func TestImportedModIconPreflightDoesNotHoldTheOnlyDatabaseConnectionIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	base := newOCT02IsolatedDatabase(t, ctx)
	poolConfig := base.Config()
	poolConfig.MaxConns = 1
	poolConfig.MinConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var actor int64
	if err = pool.QueryRow(ctx, `insert into users(username,email,password_hash)
		values('mod-image-pool','mod-image-pool@example.invalid','synthetic') returning id`).Scan(&actor); err != nil {
		t.Fatal(err)
	}
	payload := createModRequest{PrimaryName: "Imported image pool", Environment: "bothRequired",
		PrimaryCategory: "technology", OfficialStatus: "active", SourceStatus: "open", License: "MIT",
		SubmissionMethod: "modrinth", IconURL: "https://unsupported.example.invalid/icon.png"}
	if err = normalizeAndValidateModRequest(&payload); err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	requestContext, cancelRequest := context.WithTimeout(ctx, time.Second)
	defer cancelRequest()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/mods", bytes.NewReader(body))
	request = request.WithContext(context.WithValue(requestContext, claimsContextKey, security.Claims{Subject: actor}))
	response := httptest.NewRecorder()
	// The unsupported host is rejected before any network request. Actual settings,
	// identity allocation and transaction handling run against the real schema.
	(&Server{db: pool, cfg: config.Config{SettingsEncryptionKey: "synthetic-image-pool-key"}}).createMod(response, request)
	if response.Code != http.StatusBadGateway || requestContext.Err() != nil {
		t.Fatalf("import icon preflight status=%d requestError=%v body=%s", response.Code, requestContext.Err(), response.Body.String())
	}
	var stored int
	if err = pool.QueryRow(ctx, `select count(*) from mods where submitted_by=$1`, actor).Scan(&stored); err != nil || stored != 0 {
		t.Fatalf("failed preflight committed mods=%d error=%v", stored, err)
	}
}
