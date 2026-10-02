package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/antiabuse"
	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
	"mcmods-cn-backend/internal/security"
)

func TestAntiAbuseBotRuleWriteContractIsAlwaysReadOnlyIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify the anti-abuse bot-rule write contract")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cfg := config.Load()
	poolConfig, err := pgxpool.ParseConfig(cfg.DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	poolConfig.MaxConns, poolConfig.MinConns = 1, 1
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
			t.Errorf("drop ephemeral schema: %v", dropErr)
		}
	}()

	var actorID int64
	stamp := time.Now().UnixNano()
	if err = pool.QueryRow(ctx, `insert into users(username,email,password_hash,email_verified)
		values($1,$2,'map002',true) returning id`, fmt.Sprintf("map002-%d", stamp), fmt.Sprintf("map002-%d@example.invalid", stamp)).Scan(&actorID); err != nil {
		t.Fatal(err)
	}
	cfg.AntiAbuse.Enabled = false
	server := &Server{db: pool, antiAbuse: antiabuse.New(ctx, cfg.AntiAbuse, pool, nil)}
	claimsContext := context.WithValue(ctx, claimsContextKey, security.Claims{Subject: actorID})

	legacyRequest := httptest.NewRequest(http.MethodPost, "/api/v1/admin/anti-abuse/bot-rules",
		strings.NewReader(`{"kind":"blocked_bot","label":"legacy","matcher":"legacy-agent","readOnly":false}`)).WithContext(claimsContext)
	legacyResponse := httptest.NewRecorder()
	server.adminAntiAbuseBotRules(legacyResponse, legacyRequest)
	if legacyResponse.Code != http.StatusBadRequest {
		t.Fatalf("legacy readOnly field status=%d body=%s", legacyResponse.Code, legacyResponse.Body.String())
	}

	canonicalRequest := httptest.NewRequest(http.MethodPost, "/api/v1/admin/anti-abuse/bot-rules",
		strings.NewReader(`{"kind":"blocked_bot","label":"canonical","matcher":"canonical-agent"}`)).WithContext(claimsContext)
	canonicalResponse := httptest.NewRecorder()
	server.adminAntiAbuseBotRules(canonicalResponse, canonicalRequest)
	if canonicalResponse.Code != http.StatusCreated {
		t.Fatalf("canonical bot-rule status=%d body=%s", canonicalResponse.Code, canonicalResponse.Body.String())
	}
	var count int
	var readOnly bool
	if err = pool.QueryRow(ctx, `select count(*)::int,bool_and(read_only) from anti_abuse_bot_rules where label in ('legacy','canonical')`).Scan(&count, &readOnly); err != nil {
		t.Fatal(err)
	}
	if count != 1 || !readOnly {
		t.Fatalf("stored bot-rule count/readOnly=%d/%t, want 1/true", count, readOnly)
	}
}
