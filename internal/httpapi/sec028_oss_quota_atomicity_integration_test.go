package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
	"mcmods-cn-backend/tools/testing/postgrestarget"
)

func TestSEC028ConcurrentUploadSettlementsCannotOversubscribeIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, userID := newSEC028QuotaFixture(t, ctx)
	limits := ossUserQuotaLimits{single: 1_000, daily: 1_000, total: 1_000}
	settle := func(tx pgx.Tx, objectKey string) error {
		if err := settleUserOSSUploadQuotaTx(ctx, tx, userID, objectKey, 600, 600, limits); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `insert into oss_files(object_key,size_bytes,source_size_bytes,uploader_id,status)
			values($1,600,600,$2,'active')`, objectKey, userID)
		return err
	}

	first, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rollbackIntegrationTransaction(first)
	if err = settle(first, "sec028/complete-a"); err != nil {
		t.Fatal(err)
	}
	holderPID := int32(first.Conn().PgConn().PID())
	result := make(chan error, 1)
	go func() {
		second, err := pool.Begin(ctx)
		if err != nil {
			result <- err
			return
		}
		defer rollbackIntegrationTransaction(second)
		if err = settle(second, "sec028/complete-b"); err == nil {
			err = second.Commit(ctx)
		}
		result <- err
	}()

	// Observe actual contention before committing the first file and its quota
	// counters. A failure to open the second transaction is reported directly.
	poll := time.NewTicker(10 * time.Millisecond)
	defer poll.Stop()
	for {
		var waiting bool
		if err = pool.QueryRow(ctx, `select exists(select 1 from pg_stat_activity
			where datname=current_database() and wait_event_type='Lock' and $1=any(pg_blocking_pids(pid)))`, holderPID).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		select {
		case early := <-result:
			t.Fatalf("second settlement returned without waiting for the first transaction: %v", early)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-poll.C:
		}
	}
	if err = first.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	var quotaErr *ossUserQuotaError
	select {
	case err = <-result:
		if !errors.As(err, &quotaErr) {
			t.Fatalf("second settlement error = %v, want a quota rejection", err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	var files int
	if err = pool.QueryRow(ctx, `select count(*) from oss_files where uploader_id=$1 and status='active'`, userID).Scan(&files); err != nil {
		t.Fatal(err)
	}
	if files != 1 {
		t.Fatalf("settled files = %d, want one", files)
	}
	assertOSSQuotaCounters(t, ctx, pool, userID, 600, 600, 0, 0)
}

func TestSEC028QuotaStorageFaultsFailClosedIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pool, userID := newSEC028QuotaFixture(t, ctx)
	if _, err := pool.Exec(ctx, `alter table oss_user_daily_quota_usage
		add constraint sec028_reject_daily_quota_write check(false) not valid`); err != nil {
		t.Fatal(err)
	}
	server := &Server{db: pool}
	limits := ossUserQuotaLimits{single: 1_000, daily: 1_000, total: 1_000}
	before := accessFixtureBusinessRows(t, ctx, pool, "oss_user_quota_usage", "oss_user_daily_quota_usage", "oss_user_upload_quota_reservations")
	reservationErr := server.reserveUserOSSUploadQuota(ctx, userID, "sec028/fault", 100, 100, time.Now().Add(time.Minute), limits)
	requireAccessFixturePGError(t, reservationErr, "23514", "sec028_reject_daily_quota_write", "")
	var quotaErr *ossUserQuotaError
	if errors.As(reservationErr, &quotaErr) {
		t.Fatalf("storage fault was misclassified as an ordinary quota rejection: %v", reservationErr)
	}
	if after := accessFixtureBusinessRows(t, ctx, pool, "oss_user_quota_usage", "oss_user_daily_quota_usage", "oss_user_upload_quota_reservations"); after != before {
		t.Fatal("failed reservation changed quota facts")
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rollbackIntegrationTransaction(tx)
	settlementErr := settleUserOSSUploadQuotaTx(ctx, tx, userID, "sec028/fault", 100, 100, limits)
	requireAccessFixturePGError(t, settlementErr, "23514", "sec028_reject_daily_quota_write", "")
	if errors.As(settlementErr, &quotaErr) {
		t.Fatalf("storage-fault settlement error = %v, want a database failure", settlementErr)
	}
	if err = tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if after := accessFixtureBusinessRows(t, ctx, pool, "oss_user_quota_usage", "oss_user_daily_quota_usage", "oss_user_upload_quota_reservations"); after != before {
		t.Fatal("failed settlement changed quota facts")
	}
	recorder := httptest.NewRecorder()
	writeOSSUserQuotaError(recorder, settlementErr)
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("storage-fault HTTP status = %d, want 500", recorder.Code)
	}

	if _, err = pool.Exec(ctx, `alter table oss_user_daily_quota_usage drop constraint sec028_reject_daily_quota_write`); err != nil {
		t.Fatal(err)
	}
	if err = server.reserveUserOSSUploadQuota(ctx, userID, "sec028/fault", 100, 100, time.Now().Add(time.Minute), limits); err != nil {
		t.Fatalf("reservation after storage recovered: %v", err)
	}
	assertOSSQuotaCounters(t, ctx, pool, userID, 0, 0, 100, 100)
}

func newSEC028QuotaFixture(t *testing.T, ctx context.Context) (*pgxpool.Pool, int64) {
	t.Helper()
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify atomic PostgreSQL quota settlement")
	}
	databaseURL := config.Load().DB.ConnString()
	if _, err := postgrestarget.ParseSingleLoopback(databaseURL); err != nil {
		t.Fatal(err)
	}
	fixture, err := database.NewIsolatedTestSchema(ctx, databaseURL, 4)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := fixture.Close(cleanupCtx); err != nil {
			t.Error(err)
		}
	})
	var userID int64
	if err = fixture.Pool.QueryRow(ctx, `insert into users(username,email,password_hash,status)
		values('sec028-quota','sec028-quota@example.invalid','synthetic','active') returning id`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	return fixture.Pool, userID
}
