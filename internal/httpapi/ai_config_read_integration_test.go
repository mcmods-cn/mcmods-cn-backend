package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"mcmods-cn-backend/internal/security"
)

func TestAIAdminConfigurationReadFailureDoesNotOverwriteIntegration(t *testing.T) {
	ctx, pool, cfg := isolatedAITestDatabase(t)
	server := &Server{db: pool, cfg: cfg}
	var actor int64
	if err := pool.QueryRow(ctx, `insert into users(username,email,password_hash) values('aiconfigaudit','aiconfigaudit@example.invalid','synthetic') returning id`).Scan(&actor); err != nil {
		t.Fatal(err)
	}
	ctx = context.WithValue(ctx, claimsContextKey, security.Claims{Subject: actor})
	requestPayload, err := json.Marshal(defaultAIConfig())
	if err != nil {
		t.Fatal(err)
	}
	assertUnavailable := func(t *testing.T) {
		t.Helper()
		for _, method := range []string{http.MethodGet, http.MethodPut} {
			request := httptest.NewRequest(method, "/api/v1/admin/ai/config", bytes.NewReader(requestPayload)).WithContext(ctx)
			response := httptest.NewRecorder()
			if method == http.MethodGet {
				server.getAIConfig(response, request)
			} else {
				server.updateAIConfig(response, request)
			}
			if response.Code != http.StatusServiceUnavailable {
				t.Errorf("%s returned %d instead of unavailable: %s", method, response.Code, response.Body.String())
			}
		}
	}
	for _, scenario := range []string{"invalid encrypted envelope", "valid envelope with invalid config JSON"} {
		t.Run(scenario, func(t *testing.T) {
			stored := []byte(`{"_encryption":"aes-gcm-v1","nonce":"invalid","ciphertext":"invalid"}`)
			if scenario == "valid envelope with invalid config JSON" {
				stored, err = security.EncryptSetting(cfg.SettingsEncryptionKey, []byte(`{"providers":`))
				if err != nil {
					t.Fatal(err)
				}
			}
			if _, err := pool.Exec(ctx, `insert into system_settings(key,value) values('ai.config',$1::jsonb) on conflict(key) do update set value=excluded.value`, stored); err != nil {
				t.Fatal(err)
			}
			var before, after string
			if err := pool.QueryRow(ctx, `select value::text from system_settings where key='ai.config'`).Scan(&before); err != nil {
				t.Fatal(err)
			}
			assertUnavailable(t)
			if err := pool.QueryRow(ctx, `select value::text from system_settings where key='ai.config'`).Scan(&after); err != nil || before != after {
				t.Errorf("unreadable configuration was overwritten: unchanged=%v error=%v", before == after, err)
			}
		})
	}
	t.Run("absent config uses existing disabled defaults", func(t *testing.T) {
		if _, err := pool.Exec(ctx, `delete from system_settings where key='ai.config'`); err != nil {
			t.Fatal(err)
		}
		response := httptest.NewRecorder()
		server.getAIConfig(response, httptest.NewRequest(http.MethodGet, "/api/v1/admin/ai/config", nil).WithContext(ctx))
		if response.Code != http.StatusOK {
			t.Fatalf("absent configuration=%d %s", response.Code, response.Body.String())
		}
	})
	t.Run("closed database is unavailable and keeps encrypted secret", func(t *testing.T) {
		setAITestConfig(t, ctx, pool, cfg, aiTestConfig("https://example.invalid"))
		observer, err := pgxpool.NewWithConfig(ctx, pool.Config().Copy())
		if err != nil {
			t.Fatal(err)
		}
		defer observer.Close()
		var before, after string
		if err := observer.QueryRow(ctx, `select value::text from system_settings where key='ai.config'`).Scan(&before); err != nil {
			t.Fatal(err)
		}
		pool.Close()
		assertUnavailable(t)
		if err := observer.QueryRow(context.Background(), `select value::text from system_settings where key='ai.config'`).Scan(&after); err != nil || before != after {
			t.Errorf("stored encrypted configuration changed: unchanged=%v error=%v", before == after, err)
		}
	})
}
