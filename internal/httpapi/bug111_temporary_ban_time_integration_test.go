package httpapi

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/security"
)

func TestTemporaryBanEndRequiresMinimumFutureDuration(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 24, 10, 0, 0, 0, time.UTC)
	for name, endsAt := range map[string]*time.Time{
		"permanent": nil,
		"minimum":   timePointer(now.Add(minimumTemporaryBanDuration)),
		"longer":    timePointer(now.Add(24 * time.Hour)),
	} {
		t.Run(name, func(t *testing.T) {
			if err := validateTemporaryBanEnd(endsAt, now); err != nil {
				t.Fatalf("valid ban end rejected: %v", err)
			}
		})
	}
	for name, endsAt := range map[string]*time.Time{
		"past":      timePointer(now.Add(-time.Second)),
		"present":   timePointer(now),
		"too short": timePointer(now.Add(minimumTemporaryBanDuration - time.Nanosecond)),
	} {
		t.Run(name, func(t *testing.T) {
			if err := validateTemporaryBanEnd(endsAt, now); err == nil {
				t.Fatalf("%s temporary ban end was accepted", name)
			}
		})
	}
}

func TestBanHandlersRejectElapsedEndsBeforeOpeningTransactions(t *testing.T) {
	t.Parallel()
	endsAt := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339Nano)
	server := &Server{}

	directRequest := httptest.NewRequest(http.MethodPost, "/api/v1/admin/bans", bytes.NewBufferString(
		`{"userId":"user00002","reasonCode":"spam","endsAt":"`+endsAt+`"}`,
	))
	directResponse := httptest.NewRecorder()
	server.adminBans(directResponse, directRequest)
	if directResponse.Code != http.StatusBadRequest {
		t.Fatalf("direct elapsed ban status = %d body=%s", directResponse.Code, directResponse.Body.String())
	}

	reportRequest := httptest.NewRequest(http.MethodPost, "/api/v1/admin/reports/report001/resolve", bytes.NewBufferString(
		`{"conclusion":"valid","banUserId":"user00002","banReasonCode":"spam","banEndsAt":"`+endsAt+`","idempotencyKey":"bug111"}`,
	))
	reportRequest.SetPathValue("id", "report001")
	reportRequest = reportRequest.WithContext(context.WithValue(reportRequest.Context(), claimsContextKey, security.Claims{
		Subject:         99,
		PermissionRules: []security.PermissionRule{{Code: "report.action.ban", Allow: true, Priority: 1}},
	}))
	reportResponse := httptest.NewRecorder()
	server.resolveUnifiedReport(reportResponse, reportRequest)
	if reportResponse.Code != http.StatusBadRequest {
		t.Fatalf("report elapsed ban status = %d body=%s", reportResponse.Code, reportResponse.Body.String())
	}
}

func TestTemporaryBanPersistenceAndExpirationLifecycleIntegration(t *testing.T) {
	databaseURL := strings.TrimSpace(os.Getenv("MCMODS_TEST_DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("set MCMODS_TEST_DATABASE_URL to verify temporary ban lifecycle")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	poolConfig, err := pgxpool.ParseConfig(databaseURL)
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
	if _, err = pool.Exec(ctx, `
		create temporary sequence bug111_ban_public_id;
		create temporary table users(
			id bigint primary key,public_id text not null,username text not null,avatar_url text not null,status text not null,
			auth_version bigint not null default 1,permission_version bigint not null default 1,updated_at timestamptz not null default now()
		);
		create temporary table roles(id bigint primary key,code text not null);
		create temporary table ban_records(
			id bigserial primary key,public_id text not null unique default ('b'||lpad(nextval('pg_temp.bug111_ban_public_id')::text,8,'0')),
			user_id bigint not null,moderator_id bigint not null,report_id bigint,reason_code text not null,custom_reason text not null default '',
			public_record_markdown text not null default '',internal_note text not null default '',username_snapshot text not null,
			avatar_snapshot text not null default '',status text not null default 'active',starts_at timestamptz not null default now(),
			ends_at timestamptz,created_at timestamptz not null default now(),
			check(ends_at is null or ends_at>=starts_at+interval '1 minute')
		);
		create unique index bug111_active_ban on ban_records(user_id) where status='active';
		create temporary table user_role_bindings(
			user_id bigint not null,role_id bigint not null,source text not null,source_key text not null,expires_at timestamptz
		);
		insert into users(id,public_id,username,avatar_url,status) values
			(1,'user00001','first','','active'),(2,'user00002','second','','active');
		insert into roles values(7,'banned')
	`); err != nil {
		t.Fatal(err)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-time.Hour)
	if _, _, err = createBanTx(ctx, tx, "user00001", 99, 0, "spam", "", "", "", &past); err == nil {
		t.Fatal("createBanTx accepted an already elapsed end")
	}
	var bans, bindings int
	if err = tx.QueryRow(ctx, `select (select count(*) from ban_records),(select count(*) from user_role_bindings)`).Scan(&bans, &bindings); err != nil {
		t.Fatal(err)
	}
	if bans != 0 || bindings != 0 {
		t.Fatalf("elapsed ban persisted partial facts: bans=%d bindings=%d", bans, bindings)
	}
	future := time.Now().Add(2 * minimumTemporaryBanDuration)
	if _, _, err = createBanTx(ctx, tx, "user00001", 99, 0, "spam", "", "", "", &future); err != nil {
		t.Fatalf("valid temporary ban failed: %v", err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	if _, err = pool.Exec(ctx, `insert into ban_records(id,public_id,user_id,moderator_id,reason_code,username_snapshot,starts_at,ends_at)
		values(20,'bexpired',2,99,'spam','second',now()-interval '2 hours',now()-interval '1 hour');
		insert into user_role_bindings(user_id,role_id,source,source_key,expires_at)
		values(2,7,'governance_ban','20',now()-interval '1 hour')`); err != nil {
		t.Fatal(err)
	}
	worker := &MaintenanceWorker{db: pool}
	worker.expireBans(ctx)
	var expiredStatus string
	if err = pool.QueryRow(ctx, `select status from ban_records where id=20`).Scan(&expiredStatus); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `select count(*) from user_role_bindings where user_id=2`).Scan(&bindings); err != nil {
		t.Fatal(err)
	}
	if expiredStatus != "expired" || bindings != 0 {
		t.Fatalf("elapsed ban cleanup status=%s bindings=%d", expiredStatus, bindings)
	}
}
