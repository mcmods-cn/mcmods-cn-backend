package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
)

func TestSEC028ConcurrentUploadSettlementsCannotOversubscribeIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify atomic upload quota settlement")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, cleanup := newSEC028QuotaPool(t, ctx)
	defer cleanup()

	if _, err := pool.Exec(ctx, `create table oss_user_quota_usage(
		user_id bigint primary key,active_source_bytes bigint not null default 0,active_stored_bytes bigint not null default 0,
		reserved_source_bytes bigint not null default 0,reserved_stored_bytes bigint not null default 0,updated_at timestamptz not null default now());
		create table oss_user_daily_quota_usage(
		user_id bigint not null,usage_date date not null,active_source_bytes bigint not null default 0,active_stored_bytes bigint not null default 0,
		reserved_source_bytes bigint not null default 0,reserved_stored_bytes bigint not null default 0,updated_at timestamptz not null default now(),
		primary key(user_id,usage_date));
		create table oss_user_upload_quota_reservations(
		user_id bigint not null,object_key text not null,usage_date date not null,source_bytes bigint not null,stored_bytes bigint not null,
		expires_at timestamptz not null,created_at timestamptz not null default now(),primary key(user_id,object_key));
		create table oss_files(
		id bigint generated always as identity primary key,object_key text not null unique,size_bytes bigint not null,
		source_size_bytes bigint not null,uploader_id bigint not null,status text not null,created_at timestamptz not null default now());
		create function maintain_sec028_quota_usage() returns trigger language plpgsql as $$
		begin
			if new.status='active' then
				insert into oss_user_quota_usage(user_id,active_source_bytes,active_stored_bytes)
				values(new.uploader_id,coalesce(nullif(new.source_size_bytes,0),new.size_bytes),new.size_bytes)
				on conflict(user_id) do update set
					active_source_bytes=oss_user_quota_usage.active_source_bytes+excluded.active_source_bytes,
					active_stored_bytes=oss_user_quota_usage.active_stored_bytes+excluded.active_stored_bytes;
				insert into oss_user_daily_quota_usage(user_id,usage_date,active_source_bytes,active_stored_bytes)
				values(new.uploader_id,new.created_at::date,coalesce(nullif(new.source_size_bytes,0),new.size_bytes),new.size_bytes)
				on conflict(user_id,usage_date) do update set
					active_source_bytes=oss_user_daily_quota_usage.active_source_bytes+excluded.active_source_bytes,
					active_stored_bytes=oss_user_daily_quota_usage.active_stored_bytes+excluded.active_stored_bytes;
			end if;
			return new;
		end $$;
		create trigger trg_sec028_quota_usage after insert on oss_files
		for each row execute function maintain_sec028_quota_usage()`); err != nil {
		t.Fatal(err)
	}

	userID := time.Now().UnixNano() & 0x3fffffffffffffff
	limits := ossUserQuotaLimits{single: 1_000, daily: 1_000, total: 1_000}
	type settlementResult struct {
		objectKey string
		err       error
	}
	results := make(chan settlementResult, 2)
	ready := make(chan struct{}, 2)
	start := make(chan struct{})
	for _, objectKey := range []string{"sec028/complete-a", "sec028/complete-b"} {
		objectKey := objectKey
		go func() {
			tx, err := pool.Begin(ctx)
			if err != nil {
				results <- settlementResult{objectKey: objectKey, err: err}
				return
			}
			defer tx.Rollback(ctx)
			ready <- struct{}{}
			<-start
			if err = settleUserOSSUploadQuotaTx(ctx, tx, userID, objectKey, 600, 600, limits); err == nil {
				_, err = tx.Exec(ctx, `insert into oss_files(object_key,size_bytes,source_size_bytes,uploader_id,status)
					values($1,600,600,$2,'active')`, objectKey, userID)
			}
			if err == nil {
				time.Sleep(50 * time.Millisecond)
				err = tx.Commit(ctx)
			}
			results <- settlementResult{objectKey: objectKey, err: err}
		}()
	}
	<-ready
	<-ready
	close(start)

	successes, quotaFailures := 0, 0
	for range 2 {
		result := <-results
		if result.err == nil {
			successes++
			continue
		}
		var quotaErr *ossUserQuotaError
		if errors.As(result.err, &quotaErr) {
			quotaFailures++
			continue
		}
		t.Fatalf("settlement %s failed unexpectedly: %v", result.objectKey, result.err)
	}
	var files int
	var totalSource, totalStored, dailySource, dailyStored int64
	if err := pool.QueryRow(ctx, `select count(*) from oss_files`).Scan(&files); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `select active_source_bytes,active_stored_bytes
		from oss_user_quota_usage where user_id=$1`, userID).Scan(&totalSource, &totalStored); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `select active_source_bytes,active_stored_bytes
		from oss_user_daily_quota_usage where user_id=$1 and usage_date=current_date`, userID).Scan(&dailySource, &dailyStored); err != nil {
		t.Fatal(err)
	}
	if successes != 1 || quotaFailures != 1 || files != 1 || totalSource != 600 || totalStored != 600 || dailySource != 600 || dailyStored != 600 {
		t.Fatalf("concurrent completion = success %d quota %d files %d total %d/%d daily %d/%d, want 1/1/1 and 600/600",
			successes, quotaFailures, files, totalSource, totalStored, dailySource, dailyStored)
	}
}

func TestSEC028QuotaStorageFaultsFailClosedIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify quota storage failure semantics")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pool, cleanup := newSEC028QuotaPool(t, ctx)
	defer cleanup()

	if _, err := pool.Exec(ctx, `create table oss_user_quota_usage(
		user_id bigint primary key,active_source_bytes bigint not null default 0,active_stored_bytes bigint not null default 0,
		reserved_source_bytes bigint not null default 0,reserved_stored_bytes bigint not null default 0,updated_at timestamptz not null default now())`); err != nil {
		t.Fatal(err)
	}
	server := &Server{db: pool}
	userID := time.Now().UnixNano() & 0x3fffffffffffffff
	limits := ossUserQuotaLimits{single: 1_000, daily: 1_000, total: 1_000}
	reservationErr := server.reserveUserOSSUploadQuota(ctx, userID, "sec028/fault", 100, 100, time.Now().Add(time.Minute), limits)
	if reservationErr == nil {
		t.Fatal("missing daily quota storage was accepted as zero usage")
	}
	var quotaErr *ossUserQuotaError
	if errors.As(reservationErr, &quotaErr) {
		t.Fatalf("storage fault was misclassified as an ordinary quota rejection: %v", reservationErr)
	}
	var totalRows int
	if err := pool.QueryRow(ctx, `select count(*) from oss_user_quota_usage where user_id=$1`, userID).Scan(&totalRows); err != nil {
		t.Fatal(err)
	}
	if totalRows != 0 {
		t.Fatalf("failed reservation left %d quota rows, want transaction rollback", totalRows)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	settlementErr := settleUserOSSUploadQuotaTx(ctx, tx, userID, "sec028/fault", 100, 100, limits)
	_ = tx.Rollback(ctx)
	if settlementErr == nil || errors.As(settlementErr, &quotaErr) {
		t.Fatalf("storage-fault settlement error = %v, want fail-closed database error", settlementErr)
	}

	recorder := httptest.NewRecorder()
	writeOSSUserQuotaError(recorder, settlementErr)
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("storage-fault HTTP status = %d, want 500", recorder.Code)
	}
}

func newSEC028QuotaPool(t *testing.T, ctx context.Context) (*pgxpool.Pool, func()) {
	t.Helper()
	baseConfig, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	adminPool, err := pgxpool.NewWithConfig(ctx, baseConfig)
	if err != nil {
		t.Fatal(err)
	}
	schemaName := fmt.Sprintf("sec028_quota_%d", time.Now().UnixNano())
	quotedSchema := pgx.Identifier{schemaName}.Sanitize()
	if _, err = adminPool.Exec(ctx, "create schema "+quotedSchema); err != nil {
		adminPool.Close()
		t.Fatal(err)
	}
	scopedConfig, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
	if err != nil {
		adminPool.Close()
		t.Fatal(err)
	}
	scopedConfig.MaxConns = 4
	scopedConfig.ConnConfig.RuntimeParams["search_path"] = schemaName + ",pg_catalog"
	pool, err := pgxpool.NewWithConfig(ctx, scopedConfig)
	if err != nil {
		_, _ = adminPool.Exec(context.Background(), "drop schema "+quotedSchema+" cascade")
		adminPool.Close()
		t.Fatal(err)
	}
	cleanup := func() {
		pool.Close()
		dropCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, dropErr := adminPool.Exec(dropCtx, "drop schema "+quotedSchema+" cascade"); dropErr != nil {
			t.Errorf("drop isolated SEC028 quota schema: %v", dropErr)
		}
		adminPool.Close()
	}
	return pool, cleanup
}
