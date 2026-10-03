package httpapi

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
)

func TestOCT02FavoriteExportTerminalStateAndNotificationCommitTogetherIntegration(t *testing.T) {
	target := os.Getenv("MCMODS_DB_FUNCTION_REPAIR_TEST_TARGET")
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" || target == "" {
		t.Skip("requires an explicitly confirmed exclusively owned PostgreSQL test database")
	}
	cfg := config.Load()
	configured, err := cfg.DB.EffectiveName()
	if err != nil || configured != target {
		t.Fatal("configured database does not match the exclusively owned target")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	poolConfig, err := pgxpool.ParseConfig(cfg.DB.ConnString())
	if err != nil {
		t.Fatal("parse isolated database configuration")
	}
	poolConfig.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	var actual string
	if err := pool.QueryRow(ctx, "select current_database()").Scan(&actual); err != nil || actual != target {
		t.Fatal("connected database does not match the exclusively owned target")
	}
	if err := database.InstallEphemeralSchema(ctx, pool); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := database.DropEphemeralSchema(context.Background(), pool); err != nil {
			t.Errorf("drop exclusively owned ephemeral schema: %v", err)
		}
	})
	var ownerID int64
	if err := pool.QueryRow(ctx, `insert into users(username,email,password_hash,status)
		values('oct02-db012','oct02-db012@example.test','not-used','active') returning id`).Scan(&ownerID); err != nil {
		t.Fatal(err)
	}
	worker := NewFavoriteModpackExportWorker(cfg, pool, nil)
	for _, scenario := range []struct {
		name, taskID, status, terminal, template string
		skipped                                  bool
	}{
		{"completed", "o2d12fin1", "processing", "ready", "modpack_export_completed", false},
		{"completed with skips", "o2d12skp1", "processing", "ready", "modpack_export_completed_with_skips", true},
		{"expired", "o2d12exp1", "ready", "expired", "modpack_export_expired", false},
		{"terminal failure", "o2d12err1", "processing", "failed", "modpack_export_failed", false},
		{"exhausted lease", "o2d12lck1", "processing", "failed", "modpack_export_failed", false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			failedCount := 0
			if scenario.skipped {
				failedCount = 1
			}
			if _, err := pool.Exec(ctx, `insert into favorite_modpack_export_tasks(public_id,owner_user_id,
				collection_public_id_snapshot,pack_name,pack_version_id,minecraft_version,loader_type,loader_version,
				status,stage,attempt_count,lease_token,lease_expires_at,collection_item_count,exported_mod_count,
				failed_item_count,final_file_count,expires_at)
				values($1,$2,'o2d12col1','Synthetic export','v1','1.21.1','fabric','0.16.14',$3,'building',3,
				'oct02-lease',now()+interval '1 hour',1+$4,1,$4,1,now()-interval '1 minute')`,
				scenario.taskID, ownerID, scenario.status, failedCount); err != nil {
				t.Fatal(err)
			}
			var fileID int64
			if err := pool.QueryRow(ctx, `insert into oss_files(bucket,endpoint,region,object_key,source,status,scan_status)
				values('fixture-bucket','https://oss.example.test','fixture-region',$1,'favorite_modpack_export',
				'active','trusted_generated') returning id`, "temporary/modpack-exports/"+scenario.taskID+"/fixture.mrpack").Scan(&fileID); err != nil {
				t.Fatal(err)
			}
			if scenario.name == "expired" {
				if _, err := pool.Exec(ctx, `update favorite_modpack_export_tasks set result_file_id=$2
					where public_id=$1`, scenario.taskID, fileID); err != nil {
					t.Fatal(err)
				}
			}
			if scenario.name == "exhausted lease" {
				if _, err := pool.Exec(ctx, `update favorite_modpack_export_tasks set lease_expires_at=now()-interval '1 minute'
					where public_id=$1`, scenario.taskID); err != nil {
					t.Fatal(err)
				}
			}
			invoke := func() error {
				switch scenario.name {
				case "expired":
					return worker.expireCompleted(ctx)
				case "terminal failure":
					cause := errors.New("synthetic export build failure")
					err := worker.fail(ctx, scenario.taskID, "oct02-lease", "SYNTHETIC_FAILURE", cause)
					if !errors.Is(err, cause) {
						t.Fatalf("terminal failure lost its cause: %v", err)
					}
					return err
				case "exhausted lease":
					return worker.failExhaustedLeases(ctx)
				default:
					_, err := worker.finalizeFavoriteExportArtifact(ctx, scenario.taskID, "oct02-lease", fileID,
						1024, strings.Repeat("a", 64), time.Now().Add(time.Hour))
					return err
				}
			}
			if _, err := pool.Exec(ctx, `alter table nats_outbox add constraint oct02_db012_reject_notification
				check(coalesce(payload->>'templateKey','') not like 'modpack_export_%') not valid`); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if _, err := pool.Exec(context.Background(), `alter table nats_outbox drop constraint if exists oct02_db012_reject_notification`); err != nil {
					t.Errorf("remove notification fault: %v", err)
				}
			})
			if err := invoke(); err == nil {
				t.Error("rejected notification intent was not returned")
			}
			assertState := func(wantStatus string, wantEvents int, wantFileStatus string) {
				t.Helper()
				var status, fileStatus string
				var events int
				if err := pool.QueryRow(ctx, `select task.status,file.status,
					(select count(*) from nats_outbox where payload#>>'{data,taskId}'=$1 and payload->>'templateKey'=$3)
					from favorite_modpack_export_tasks task cross join oss_files file
					where task.public_id=$1 and file.id=$2`, scenario.taskID, fileID, scenario.template).
					Scan(&status, &fileStatus, &events); err != nil {
					t.Fatal(err)
				}
				if status != wantStatus || events != wantEvents || fileStatus != wantFileStatus {
					t.Fatalf("state/notification/file=%s/%d/%s, want %s/%d/%s",
						status, events, fileStatus, wantStatus, wantEvents, wantFileStatus)
				}
			}
			assertState(scenario.status, 0, "active")
			if _, err := pool.Exec(ctx, `alter table nats_outbox drop constraint oct02_db012_reject_notification`); err != nil {
				t.Fatal(err)
			}
			if err := invoke(); err != nil && scenario.name != "terminal failure" {
				t.Fatalf("retry after releasing notification fault: %v", err)
			}
			fileStatus := "active"
			if scenario.name == "expired" {
				fileStatus = "deleted"
			}
			assertState(scenario.terminal, 1, fileStatus)
			var notificationURL, eventType string
			if err := pool.QueryRow(ctx, `select payload#>>'{data,url}',event_type from nats_outbox
				where payload#>>'{data,taskId}'=$1 and payload->>'templateKey'=$2`, scenario.taskID, scenario.template).
				Scan(&notificationURL, &eventType); err != nil {
				t.Fatal(err)
			}
			if notificationURL != "/user?section=favorites&exportTask="+scenario.taskID || eventType != "notification.direct" {
				t.Fatalf("notification lost its supported link or original event type: %s/%s", notificationURL, eventType)
			}
			if err := worker.process(ctx, scenario.taskID); err != nil {
				t.Fatalf("terminal redelivery: %v", err)
			}
			assertState(scenario.terminal, 1, fileStatus)
		})
	}
}
