package httpapi

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
)

func TestOSSQuotaConcurrentReservationsCannotOversubscribeIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify concurrent OSS quota reservations")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	baseConfig, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	adminPool, err := pgxpool.NewWithConfig(ctx, baseConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer adminPool.Close()
	schemaName := fmt.Sprintf("perf044_quota_%d", time.Now().UnixNano())
	quotedSchema := pgx.Identifier{schemaName}.Sanitize()
	if _, err = adminPool.Exec(ctx, "create schema "+quotedSchema); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, dropErr := adminPool.Exec(context.Background(), "drop schema "+quotedSchema+" cascade"); dropErr != nil {
			t.Errorf("drop isolated quota schema: %v", dropErr)
		}
	}()
	scopedConfig, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	scopedConfig.MaxConns = 4
	scopedConfig.ConnConfig.RuntimeParams["search_path"] = schemaName + ",pg_catalog"
	pool, err := pgxpool.NewWithConfig(ctx, scopedConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if _, err = pool.Exec(ctx, `create table oss_user_quota_usage(
		user_id bigint primary key,active_source_bytes bigint not null default 0,active_stored_bytes bigint not null default 0,
		reserved_source_bytes bigint not null default 0,reserved_stored_bytes bigint not null default 0,updated_at timestamptz not null default now());
		create table oss_user_daily_quota_usage(
		user_id bigint not null,usage_date date not null,active_source_bytes bigint not null default 0,active_stored_bytes bigint not null default 0,
		reserved_source_bytes bigint not null default 0,reserved_stored_bytes bigint not null default 0,updated_at timestamptz not null default now(),
		primary key(user_id,usage_date));
		create table oss_user_upload_quota_reservations(
		user_id bigint not null,object_key text not null,usage_date date not null,source_bytes bigint not null,stored_bytes bigint not null,
		expires_at timestamptz not null,created_at timestamptz not null default now(),primary key(user_id,object_key))`); err != nil {
		t.Fatal(err)
	}
	server := &Server{db: pool}
	limits := ossUserQuotaLimits{single: 1_000, daily: 1_000, total: 1_000}
	results := make(chan error, 2)
	started := time.Now()
	for _, objectKey := range []string{"concurrent/a", "concurrent/b"} {
		objectKey := objectKey
		go func() {
			results <- server.reserveUserOSSUploadQuota(ctx, 42, objectKey, 600, 600, time.Now().Add(10*time.Minute), limits)
		}()
	}
	successes, quotaFailures := 0, 0
	for range 2 {
		result := <-results
		if result == nil {
			successes++
			continue
		}
		var quotaErr *ossUserQuotaError
		if errors.As(result, &quotaErr) {
			quotaFailures++
			continue
		}
		t.Fatalf("concurrent reservation failed unexpectedly: %v", result)
	}
	var reservations int
	var reservedSource, reservedStored int64
	if err = pool.QueryRow(ctx, `select count(*) from oss_user_upload_quota_reservations`).Scan(&reservations); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `select reserved_source_bytes,reserved_stored_bytes from oss_user_quota_usage where user_id=42`).Scan(
		&reservedSource, &reservedStored,
	); err != nil {
		t.Fatal(err)
	}
	if successes != 1 || quotaFailures != 1 || reservations != 1 || reservedSource != 600 || reservedStored != 600 || time.Since(started) > 2*time.Second {
		t.Fatalf("concurrent quota = successes %d failures %d reservations %d bytes %d/%d duration %s",
			successes, quotaFailures, reservations, reservedSource, reservedStored, time.Since(started))
	}
	t.Logf("concurrent 600+600 reservations serialized to one success in %s", time.Since(started))
}

func TestOSSQuotaReservationSettlementTriggersAndRebuildIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify OSS quota bucket behavior")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
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
			t.Errorf("drop ephemeral schema: %v", dropErr)
		}
	}()

	var userID int64
	if err = pool.QueryRow(ctx, `insert into users(username,email,password_hash,status)
		values('perf044-owner','perf044-owner@example.test','not-used','active') returning id`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	server := &Server{db: pool}
	limits := ossUserQuotaLimits{single: 1_000, daily: 1_000, total: 1_000}
	if err = server.reserveUserOSSUploadQuota(ctx, userID, "quota/key-a", 600, 600, time.Now().Add(10*time.Minute), limits); err != nil {
		t.Fatal(err)
	}
	err = server.reserveUserOSSUploadQuota(ctx, userID, "quota/key-b", 500, 500, time.Now().Add(10*time.Minute), limits)
	var quotaErr *ossUserQuotaError
	if !errors.As(err, &quotaErr) {
		t.Fatalf("over-limit concurrent reservation error = %v", err)
	}
	if err = server.reserveUserOSSUploadQuota(ctx, userID, "quota/key-b", 400, 400, time.Now().Add(10*time.Minute), limits); err != nil {
		t.Fatal(err)
	}
	assertOSSQuotaCounters(t, ctx, pool, userID, 0, 0, 1_000, 1_000)
	if err = server.releaseUserOSSUploadQuotaReservation(ctx, userID, "quota/key-b"); err != nil {
		t.Fatal(err)
	}
	assertOSSQuotaCounters(t, ctx, pool, userID, 0, 0, 600, 600)

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = settleUserOSSUploadQuotaTx(ctx, tx, userID, "quota/key-a", 600, 550, limits); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(err)
	}
	var fileID int64
	if err = tx.QueryRow(ctx, `insert into oss_files(object_key,size_bytes,source_size_bytes,uploader_id,status)
		values('quota/key-a',550,600,$1,'active') returning id`, userID).Scan(&fileID); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	assertOSSQuotaCounters(t, ctx, pool, userID, 600, 550, 0, 0)
	if _, err = pool.Exec(ctx, `update oss_files set size_bytes=500 where id=$1`, fileID); err != nil {
		t.Fatal(err)
	}
	assertOSSQuotaCounters(t, ctx, pool, userID, 600, 500, 0, 0)
	if _, err = pool.Exec(ctx, `update oss_files set status='deleted' where id=$1`, fileID); err != nil {
		t.Fatal(err)
	}
	assertOSSQuotaCounters(t, ctx, pool, userID, 0, 0, 0, 0)

	if _, err = pool.Exec(ctx, `insert into oss_files(object_key,size_bytes,source_size_bytes,uploader_id,status)
		values('quota/key-c',200,250,$1,'active')`, userID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `update oss_user_quota_usage set active_source_bytes=999,active_stored_bytes=999 where user_id=$1`, userID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `update oss_user_daily_quota_usage set active_source_bytes=999,active_stored_bytes=999
		where user_id=$1 and usage_date=current_date`, userID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `select pg_temp.rebuild_oss_user_quota_usage($1::bigint)`, userID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `select pg_temp.rebuild_oss_user_daily_quota_usage($1::bigint,current_date)`, userID); err != nil {
		t.Fatal(err)
	}
	assertOSSQuotaCounters(t, ctx, pool, userID, 250, 200, 0, 0)
	usage, err := loadUserOSSFileQuotaUsage(ctx, pool, userID)
	if err != nil || usage.DailySourceUsed != 250 || usage.DailyStoredUsed != 200 ||
		usage.TotalSourceUsed != 250 || usage.TotalStoredUsed != 200 {
		t.Fatalf("rebuilt quota API usage = (%+v, %v)", usage, err)
	}
}

func TestOSSQuotaReadsIgnoreMillionFileHistoryIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify OSS quota reads at scale")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	poolConfig, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	poolConfig.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if _, err = pool.Exec(ctx, `create temporary table oss_files(id bigint primary key);
		create temporary table oss_user_quota_usage(
			user_id bigint primary key,active_source_bytes bigint,active_stored_bytes bigint,
			reserved_source_bytes bigint,reserved_stored_bytes bigint);
		create temporary table oss_user_daily_quota_usage(
			user_id bigint,usage_date date,active_source_bytes bigint,active_stored_bytes bigint,
			reserved_source_bytes bigint,reserved_stored_bytes bigint,primary key(user_id,usage_date));
		insert into oss_files select value from generate_series(1,1000000) value;
		insert into oss_user_quota_usage values(42,700,500,30,20);
		insert into oss_user_daily_quota_usage values(42,current_date,300,200,30,20)`); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	usage, err := loadUserOSSFileQuotaUsage(ctx, pool, 42)
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{db: pool}
	total, daily, err := server.loadOSSUserQuotaAdmissionSnapshots(ctx, 42)
	if err != nil {
		t.Fatal(err)
	}
	duration := time.Since(started)
	if duration > 2*time.Second || usage.TotalSourceUsed != 700 || usage.TotalStoredUsed != 500 ||
		usage.DailySourceUsed != 300 || usage.DailyStoredUsed != 200 ||
		total.reservedSource != 30 || total.reservedStored != 20 || daily.reservedSource != 30 || daily.reservedStored != 20 {
		t.Fatalf("million-history quota read = usage=%+v total=%+v daily=%+v duration=%s", usage, total, daily, duration)
	}
	t.Logf("million-file quota buckets loaded in %s without touching the one-column history trap", duration)
}

func assertOSSQuotaCounters(t *testing.T, ctx context.Context, pool *pgxpool.Pool, userID, activeSource, activeStored, reservedSource, reservedStored int64) {
	t.Helper()
	var totalSource, totalStored, totalReservedSource, totalReservedStored int64
	if err := pool.QueryRow(ctx, `select active_source_bytes,active_stored_bytes,reserved_source_bytes,reserved_stored_bytes
		from oss_user_quota_usage where user_id=$1`, userID).Scan(
		&totalSource, &totalStored, &totalReservedSource, &totalReservedStored,
	); err != nil {
		t.Fatal(err)
	}
	if totalSource != activeSource || totalStored != activeStored || totalReservedSource != reservedSource || totalReservedStored != reservedStored {
		t.Fatalf("total quota counters = %d/%d active %d/%d reserved, want %d/%d active %d/%d reserved",
			totalSource, totalStored, totalReservedSource, totalReservedStored,
			activeSource, activeStored, reservedSource, reservedStored)
	}
	var dailySource, dailyStored, dailyReservedSource, dailyReservedStored int64
	if err := pool.QueryRow(ctx, `select active_source_bytes,active_stored_bytes,reserved_source_bytes,reserved_stored_bytes
		from oss_user_daily_quota_usage where user_id=$1 and usage_date=current_date`, userID).Scan(
		&dailySource, &dailyStored, &dailyReservedSource, &dailyReservedStored,
	); err != nil {
		t.Fatal(err)
	}
	if dailySource != activeSource || dailyStored != activeStored || dailyReservedSource != reservedSource || dailyReservedStored != reservedStored {
		t.Fatalf("daily quota counters = %d/%d active %d/%d reserved, want %d/%d active %d/%d reserved",
			dailySource, dailyStored, dailyReservedSource, dailyReservedStored,
			activeSource, activeStored, reservedSource, reservedStored)
	}
}
