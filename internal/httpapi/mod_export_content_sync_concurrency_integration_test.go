package httpapi

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
)

func TestImportedContentVersionLockSerializesConcurrentSyncIntegration(t *testing.T) {
	db := openModExportConcurrentTestDB(t)
	ctx := context.Background()
	firstConn, err := db.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer firstConn.Release()
	secondConn, err := db.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer secondConn.Release()
	for _, conn := range []*pgxpool.Conn{firstConn, secondConn} {
		if _, err = conn.Exec(ctx, `create temp table mod_content_versions(id bigint primary key,mod_id bigint not null,status text not null);
			insert into mod_content_versions values(7001,9001,'active')`); err != nil {
			t.Fatal(err)
		}
	}
	firstTx, err := firstConn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer firstTx.Rollback(ctx)
	secondTx, err := secondConn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer secondTx.Rollback(ctx)
	if modID, lockErr := lockImportedContentVersionTx(ctx, firstTx, 7001); lockErr != nil || modID != 9001 {
		t.Fatalf("first version lock mod=%d err=%v", modID, lockErr)
	}
	type lockResult struct {
		modID int64
		err   error
	}
	done := make(chan lockResult, 1)
	lockCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	go func() {
		modID, lockErr := lockImportedContentVersionTx(lockCtx, secondTx, 7001)
		done <- lockResult{modID: modID, err: lockErr}
	}()
	select {
	case result := <-done:
		t.Fatalf("second version sync bypassed the active transaction lock: %+v", result)
	case <-time.After(250 * time.Millisecond):
	}
	if err = firstTx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-done:
		if result.err != nil || result.modID != 9001 {
			t.Fatalf("second version lock after release mod=%d err=%v", result.modID, result.err)
		}
	case <-lockCtx.Done():
		t.Fatal("second version sync did not resume after the first transaction released its lock")
	}
}

func TestImportedPlacementConflictTargetOnlyIgnoresSameResourceIntegration(t *testing.T) {
	db := openGlobalCatalogTestDB(t)
	ctx := context.Background()
	if _, err := db.Exec(ctx, `create temp table mod_content_section_resources(
		section_id bigint not null,version_id bigint not null,resource_id bigint not null,
		placement_identity_key text not null,ordinal integer not null,
		primary key(section_id,version_id,resource_id),
		unique(section_id,version_id,ordinal),unique(version_id,resource_id),unique(version_id,placement_identity_key));
		insert into mod_content_section_resources values(10,20,30,'identity-a',0)`); err != nil {
		t.Fatal(err)
	}
	tag, err := db.Exec(ctx, `insert into mod_content_section_resources values(11,20,30,'identity-b',0)
		on conflict(version_id,resource_id) do nothing`)
	if err != nil || tag.RowsAffected() != 0 {
		t.Fatalf("same resource was not the explicit idempotent conflict: rows=%d err=%v", tag.RowsAffected(), err)
	}
	if _, err = db.Exec(ctx, `insert into mod_content_section_resources values(10,20,31,'identity-b',0)
		on conflict(version_id,resource_id) do nothing`); !isUniqueViolation(err) {
		t.Fatalf("ordinal collision was silently ignored: %v", err)
	}
	if _, err = db.Exec(ctx, `insert into mod_content_section_resources values(11,20,31,'identity-a',1)
		on conflict(version_id,resource_id) do nothing`); !isUniqueViolation(err) {
		t.Fatalf("placement identity collision was silently ignored: %v", err)
	}
}

func openModExportConcurrentTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	databaseURL := strings.TrimSpace(os.Getenv("MCMODS_TEST_DATABASE_URL"))
	if databaseURL == "" {
		if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
			t.Skip("set MCMODS_TEST_DATABASE_URL or MCMODS_RUN_DB_INTEGRATION=1 to run Mod export concurrency tests")
		}
		databaseURL = config.Load().DB.ConnString()
	}
	poolConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	poolConfig.MaxConns = 2
	pool, err := pgxpool.NewWithConfig(context.Background(), poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}
