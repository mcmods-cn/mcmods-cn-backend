package httpapi

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"mcmods-cn-backend/internal/config"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestSEC024FavoriteExportQuotaIsAtomicAcrossConcurrentTransactionsIntegration(t *testing.T) {
	pool := newSEC024FavoriteExportQuotaPool(t)
	type quotaCase struct {
		name                string
		ownerID             int64
		maxActive, maxDaily int
		collectionPublicID  string
		distinctCollections bool
		minecraftVersion    string
		loader              string
		wantAccepted        int
		wantRejection       error
	}
	cases := []quotaCase{
		{name: "active", ownerID: 24001, maxActive: 2, maxDaily: 100, collectionPublicID: "sec024a01", distinctCollections: true, minecraftVersion: "1.21.1", loader: "fabric", wantAccepted: 2, wantRejection: errFavoriteExportConcurrencyLimit},
		{name: "daily", ownerID: 24002, maxActive: 100, maxDaily: 3, collectionPublicID: "sec024d01", distinctCollections: true, minecraftVersion: "1.21.1", loader: "fabric", wantAccepted: 3, wantRejection: errFavoriteExportDailyLimit},
		{name: "duplicate", ownerID: 24003, maxActive: 100, maxDaily: 100, collectionPublicID: "sec024c01", minecraftVersion: "1.21.1", loader: "fabric", wantAccepted: 1, wantRejection: errFavoriteExportDuplicateCooldown},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			const attempts = 8
			start := make(chan struct{})
			results := make(chan error, attempts)
			var ready sync.WaitGroup
			ready.Add(attempts)
			for attempt := range attempts {
				go func() {
					collectionPublicID := testCase.collectionPublicID
					if testCase.distinctCollections {
						collectionPublicID = fmt.Sprintf("s024%c%04d", testCase.name[0], attempt)
					}
					ready.Done()
					<-start
					tx, err := pool.Begin(context.Background())
					if err == nil {
						defer tx.Rollback(context.Background())
						err = reserveFavoriteModpackExportQuota(context.Background(), tx, testCase.ownerID,
							collectionPublicID, testCase.minecraftVersion, testCase.loader, testCase.maxActive, testCase.maxDaily)
					}
					if err == nil {
						// Keep the pre-insert window open long enough for every unprotected
						// transaction to observe the same stale counts.
						_, err = tx.Exec(context.Background(), `select pg_sleep(0.08)`)
					}
					if err == nil {
						_, err = tx.Exec(context.Background(), `insert into favorite_modpack_export_tasks(
							owner_user_id,collection_public_id_snapshot,minecraft_version,loader_type,status)
							values($1,$2,$3,$4,'pending')`, testCase.ownerID, collectionPublicID,
							testCase.minecraftVersion, testCase.loader)
					}
					if err == nil {
						err = tx.Commit(context.Background())
					}
					results <- err
				}()
			}
			ready.Wait()
			close(start)
			accepted, rejected := 0, 0
			for range attempts {
				err := <-results
				if err == nil {
					accepted++
					continue
				}
				if !errors.Is(err, testCase.wantRejection) {
					t.Fatalf("unexpected quota result: %v", err)
				}
				rejected++
			}
			if accepted != testCase.wantAccepted || rejected != attempts-testCase.wantAccepted {
				t.Fatalf("concurrent quota accepted=%d rejected=%d, want %d/%d", accepted, rejected,
					testCase.wantAccepted, attempts-testCase.wantAccepted)
			}
			var persisted int
			if err := pool.QueryRow(context.Background(), `select count(*) from favorite_modpack_export_tasks where owner_user_id=$1`, testCase.ownerID).Scan(&persisted); err != nil {
				t.Fatal(err)
			}
			if persisted != testCase.wantAccepted {
				t.Fatalf("persisted tasks=%d, want %d", persisted, testCase.wantAccepted)
			}
		})
	}
}

func TestSEC024FavoriteExportHandlerHoldsOneUserQuotaLockThroughInsert(t *testing.T) {
	raw, err := os.ReadFile("favorite_modpack_export.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(raw)
	call := strings.Index(source, "reserveFavoriteModpackExportQuota(r.Context(), tx")
	insert := strings.Index(source, "insert into favorite_modpack_export_tasks")
	lock := strings.Index(source, "favorite-modpack-export-quota:")
	count := strings.Index(source, "count(*) filter(where status in ('pending','processing'))")
	if call < 0 || insert < 0 || call >= insert {
		t.Fatal("task creation must reserve the user quota in the same transaction before inserting")
	}
	if lock < 0 || count < 0 || lock >= count {
		t.Fatal("quota reservation must take the namespaced user advisory lock before reading counts")
	}
}

func newSEC024FavoriteExportQuotaPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 with isolated PostgreSQL to verify favorite export quotas")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	baseConfig, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	adminPool, err := pgxpool.NewWithConfig(ctx, baseConfig)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(adminPool.Close)
	schemaName := fmt.Sprintf("sec024_favorite_quota_%d", time.Now().UnixNano())
	quotedSchema := pgx.Identifier{schemaName}.Sanitize()
	if _, err = adminPool.Exec(ctx, "create schema "+quotedSchema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, dropErr := adminPool.Exec(context.Background(), "drop schema "+quotedSchema+" cascade"); dropErr != nil {
			t.Errorf("drop isolated SEC-024 schema: %v", dropErr)
		}
	})

	scopedConfig, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	scopedConfig.MaxConns = 16
	scopedConfig.ConnConfig.RuntimeParams["search_path"] = schemaName + ",pg_catalog"
	pool, err := pgxpool.NewWithConfig(ctx, scopedConfig)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if _, err = pool.Exec(ctx, `create table favorite_modpack_export_tasks (
		id bigserial primary key,
		owner_user_id bigint not null,
		collection_public_id_snapshot text not null,
		minecraft_version text not null,
		loader_type text not null,
		status text not null default 'pending',
		created_at timestamptz not null default now()
	)`); err != nil {
		t.Fatal(err)
	}
	return pool
}
