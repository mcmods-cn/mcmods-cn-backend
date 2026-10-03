package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/security"
)

func TestOCT02CUserBlockListReleasesRowsBeforeSettingsLookupIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 for PostgreSQL pool regression")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cfg, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	cfg.MaxConns, cfg.MinConns = 1, 1
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if _, err = pool.Exec(ctx, `create temporary table users(id bigint primary key,public_id text,username text,avatar_url text,signature text,status text);
		create temporary table user_blocks(blocker_id bigint,blocked_id bigint,created_at timestamptz);
		create temporary table system_settings(key text primary key,value jsonb);
		insert into users values(2,'blocked02','Blocked user','','','active');
		insert into user_blocks values(1,2,now())`); err != nil {
		t.Fatal(err)
	}
	server := &Server{db: pool}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/users/me/blocks", nil).WithContext(context.WithValue(ctx, claimsContextKey, security.Claims{Subject: 1}))
	response := httptest.NewRecorder()
	server.userBlockList(response, request)
	if err := ctx.Err(); err != nil {
		t.Fatalf("block list exhausted the request deadline while retaining its only connection: %v", err)
	}
	if response.Code != http.StatusOK {
		t.Fatalf("single-connection block list status=%d body=%s", response.Code, response.Body.String())
	}
}
