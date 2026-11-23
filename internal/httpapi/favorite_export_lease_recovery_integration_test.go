package httpapi

import (
	"context"
	"os"
	"testing"
	"time"

	"mcmods-cn-backend/internal/config"
)

func TestFavoriteExportExhaustedLeaseRecoveryIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify export lease recovery against PostgreSQL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cfg := config.Load()
	pool, userID := newPostgresUserFixture(t, ctx, "favorite-export-lease-owner")
	var collectionID int64
	const name = "Exhausted export lease"
	if err := pool.QueryRow(ctx, `insert into favorite_collections(user_id,name) values($1,$2) returning id`, userID, name).Scan(&collectionID); err != nil {
		t.Fatalf("create integration collection: %v", err)
	}
	var taskID string
	if err := pool.QueryRow(ctx, `insert into favorite_modpack_export_tasks(owner_user_id,collection_id,collection_public_id_snapshot,pack_name,pack_version_id,
		minecraft_version,loader_type,loader_version,status,attempt_count,lease_token,lease_expires_at)
		values($1,$2,(select public_id from favorite_collections where id=$2),$3,'1.0.0','1.21.1','fabric','0.16.14','processing',$4,'expired-test',now()-interval '1 minute')
		returning public_id`, userID, collectionID, name, favoriteExportMaxAttempts(cfg.FavoriteExport.MaxBuildAttempts)).Scan(&taskID); err != nil {
		t.Fatalf("create expired export task: %v", err)
	}
	worker := NewFavoriteModpackExportWorker(cfg, pool, nil)
	if err := worker.failExhaustedLeases(ctx); err != nil {
		t.Fatalf("fail exhausted export lease: %v", err)
	}
	var status, code, detail string
	if err := pool.QueryRow(ctx, `select status,error_code,error_detail from favorite_modpack_export_tasks where public_id=$1`, taskID).Scan(&status, &code, &detail); err != nil {
		t.Fatalf("read recovered task: %v", err)
	}
	if status != "failed" || code != "WORKER_LEASE_EXHAUSTED" || detail != "export processing failed" {
		t.Fatalf("unexpected recovered task state: status=%q code=%q detail=%q", status, code, detail)
	}
}
