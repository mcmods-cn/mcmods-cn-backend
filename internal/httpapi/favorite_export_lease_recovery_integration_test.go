package httpapi

import (
	"context"
	"errors"
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
	var resultFile int64
	if err := pool.QueryRow(ctx, `insert into oss_files(bucket,endpoint,region,object_key,source,status,scan_status)
		values('fixture','oss.example','region','tmp/late.mrpack','favorite_modpack_export','active','trusted_generated') returning id`).Scan(&resultFile); err != nil {
		t.Fatal(err)
	}
	if published, err := worker.finalizeFavoriteExportArtifact(ctx, taskID, "expired-test", resultFile, 100, "fixture", time.Now().Add(time.Hour)); published || err == nil {
		t.Fatalf("expired attempt published artifact %t, error %v", published, err)
	}
	cause := errors.New("fixture build failure")
	for _, attempt := range []int{1, favoriteExportMaxAttempts(cfg.FavoriteExport.MaxBuildAttempts)} {
		if _, err := pool.Exec(ctx, "update favorite_modpack_export_tasks set attempt_count=$2 where public_id=$1", taskID, attempt); err != nil {
			t.Fatal(err)
		}
		if err := worker.fail(ctx, taskID, "expired-test", "BUILD_FAILED", cause); !errors.Is(err, cause) {
			t.Fatalf("attempt %d cause = %v", attempt, err)
		}
		var stillOwned bool
		if err := pool.QueryRow(ctx, `select status='processing' and lease_token='expired-test' and result_file_id is null and not exists(
			select 1 from nats_outbox where payload#>>'{data,taskId}'=$1) from favorite_modpack_export_tasks where public_id=$1`, taskID).Scan(&stillOwned); err != nil || !stillOwned {
			t.Fatalf("expired attempt %d altered task/notification = %t, %v", attempt, !stillOwned, err)
		}
	}
	if err := worker.renewFavoriteExportLease(ctx, taskID, "expired-test", time.Minute); !errors.Is(err, errFavoriteExportLeaseLost) {
		t.Fatalf("expired heartbeat = %v", err)
	}
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
	if _, err := pool.Exec(ctx, `update favorite_modpack_export_tasks set status='processing',lease_token='new-attempt',
		lease_expires_at=clock_timestamp()+interval '20 seconds' where public_id=$1`, taskID); err != nil {
		t.Fatal(err)
	}
	renewed := make(chan error, 1)
	_, stop := startDatabaseJobHeartbeat(ctx, 10*time.Millisecond, func(work context.Context) error {
		err := worker.renewFavoriteExportLease(work, taskID, "new-attempt", time.Minute)
		select {
		case renewed <- err:
		default:
		}
		return err
	})
	select {
	case err := <-renewed:
		stop()
		if err != nil {
			t.Fatalf("active heartbeat: %v", err)
		}
	case <-ctx.Done():
		stop()
		t.Fatal(ctx.Err())
	}
	var active bool
	if err := pool.QueryRow(ctx, `select lease_token='new-attempt' and lease_expires_at>clock_timestamp()+interval '25 seconds'
		from favorite_modpack_export_tasks where public_id=$1`, taskID).Scan(&active); err != nil || !active {
		t.Fatalf("active lease renewed = %t, error %v", active, err)
	}
	if err := worker.renewFavoriteExportLease(ctx, taskID, "expired-test", time.Hour); !errors.Is(err, errFavoriteExportLeaseLost) {
		t.Fatalf("old token renewed a new attempt: %v", err)
	}

}
