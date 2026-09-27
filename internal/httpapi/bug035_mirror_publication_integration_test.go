package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
)

func TestBUG035CleanMirrorPromotionPublishesOneAtomicProjectUpdateIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify automated mirror publication")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
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

	catalog := `{"versions":[{"code":"1.21.1","type":"release"}],"commonVersions":["1.21.1"],"loaders":[{"code":"Fabric","name":"Fabric","versions":["1.21.1"]}]}`
	if _, err = pool.Exec(ctx, `insert into system_settings(key,value) values($1,$2::jsonb)
		on conflict(key) do update set value=excluded.value`, minecraftVersionsSettingKey, catalog); err != nil {
		t.Fatal(err)
	}
	var projectInternalID, routeID int64
	if err = pool.QueryRow(ctx, `insert into mods(project_code,slug,primary_name,review_status)
		values('b035m0001','bug035-project','BUG035 project','approved') returning id`).Scan(&projectInternalID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `select id from public_routes where entity_type='mod' and internal_id=$1`, projectInternalID).Scan(&routeID); err != nil {
		t.Fatal(err)
	}
	insertBUG035CleanMirror(t, ctx, pool, routeID, "bug035-file")
	worker := &ProjectAutomationWorker{server: &Server{db: pool}}

	if _, err = pool.Exec(ctx, `create function pg_temp.bug035_reject_project_update() returns trigger language plpgsql as $$
		begin raise exception 'BUG035 forced project update failure'; end $$;
		create trigger bug035_reject_project_update before insert on project_update_events
		for each row execute function pg_temp.bug035_reject_project_update()`); err != nil {
		t.Fatal(err)
	}
	worker.promoteCleanMirrors(ctx)
	var mirrorStatus string
	var fileCount int
	if err = pool.QueryRow(ctx, `select status from mirrored_project_files where external_file_id='bug035-file'`).Scan(&mirrorStatus); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `select count(*) from project_files where project_type='mod' and project_internal_id=$1`, projectInternalID).Scan(&fileCount); err != nil {
		t.Fatal(err)
	}
	if mirrorStatus != "scanning" || fileCount != 0 {
		t.Fatalf("failed event enqueue left mirror=%q files=%d, want scanning and zero files", mirrorStatus, fileCount)
	}
	if _, err = pool.Exec(ctx, `drop trigger bug035_reject_project_update on project_update_events;
		drop function pg_temp.bug035_reject_project_update()`); err != nil {
		t.Fatal(err)
	}

	worker.promoteCleanMirrors(ctx)
	var filePublicID, fileStatus string
	var publicationGeneration int
	if err = pool.QueryRow(ctx, `select mirror.status,project_file.public_id,project_file.status,project_file.publication_generation
		from mirrored_project_files mirror join project_files project_file on project_file.oss_file_id=mirror.oss_file_id
		where mirror.external_file_id='bug035-file'`).Scan(&mirrorStatus, &filePublicID, &fileStatus, &publicationGeneration); err != nil {
		t.Fatal(err)
	}
	if mirrorStatus != "ready" || fileStatus != "active" || publicationGeneration != 1 {
		t.Fatalf("successful promotion mirror=%q file=%q generation=%d, want ready/active/1", mirrorStatus, fileStatus, publicationGeneration)
	}

	assertBUG035PublicationArtifacts(t, ctx, pool, routeID, filePublicID, 1)
	if _, err = pool.Exec(ctx, `update mirrored_project_files set status='scanning' where external_file_id='bug035-file'`); err != nil {
		t.Fatal(err)
	}
	worker.promoteCleanMirrors(ctx)
	assertBUG035PublicationArtifacts(t, ctx, pool, routeID, filePublicID, 1)
}

func insertBUG035CleanMirror(t *testing.T, ctx context.Context, pool *pgxpool.Pool, routeID int64, externalID string) {
	t.Helper()
	metadata, err := json.Marshal(providerProjectFile{ID: externalID, Source: "modrinth", DisplayName: "BUG035 release",
		FileName: "bug035.jar", VersionName: "1.0.0", ReleaseChannel: "release", GameVersions: []string{"1.21.1"}, Loaders: []string{"Fabric"}})
	if err != nil {
		t.Fatal(err)
	}
	var ossID int64
	if err = pool.QueryRow(ctx, `insert into oss_files(object_key,category,source,original_name,content_type,size_bytes,source_size_bytes,sha256,status,scan_status)
		values($1,'project/download/automated','project_auto_update','bug035.jar','application/java-archive',128,128,$2,'active','clean') returning id`,
		fmt.Sprintf("bug035/%d/bug035.jar", time.Now().UnixNano()), "sha-"+externalID).Scan(&ossID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into mirrored_project_files(project_route_id,source_type,external_file_id,file_sha256,byte_size,oss_file_id,status,metadata)
		values($1,'modrinth',$2,$3,128,$4,'scanning',$5::jsonb)`, routeID, externalID, "sha-"+externalID, ossID, metadata); err != nil {
		t.Fatal(err)
	}
}

func assertBUG035PublicationArtifacts(t *testing.T, ctx context.Context, pool *pgxpool.Pool, routeID int64, filePublicID string, wantCount int) {
	t.Helper()
	var eventCount, taskCount, outboxCount int
	var updateKind, publicationBatchID string
	var sections []string
	if err := pool.QueryRow(ctx, `select count(*),coalesce(min(update_kind),''),coalesce(min(publication_batch_id),'')
		from project_update_events where project_route_id=$1`, routeID).Scan(&eventCount, &updateKind, &publicationBatchID); err != nil {
		t.Fatal(err)
	}
	if eventCount > 0 {
		if err := pool.QueryRow(ctx, `select changed_sections from project_update_events where project_route_id=$1 limit 1`, routeID).Scan(&sections); err != nil {
			t.Fatal(err)
		}
	}
	if err := pool.QueryRow(ctx, `select count(*) from project_update_notification_tasks
		where event_id in (select id from project_update_events where project_route_id=$1)`, routeID).Scan(&taskCount); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `select count(*) from nats_outbox where aggregate_type='project_update_event'
		and aggregate_id in (select id::text from project_update_events where project_route_id=$1)`, routeID).Scan(&outboxCount); err != nil {
		t.Fatal(err)
	}
	wantBatchID := fmt.Sprintf("project-file:download_added:%s:1", filePublicID)
	if eventCount != wantCount || taskCount != wantCount || outboxCount != wantCount || updateKind != "download_added" ||
		publicationBatchID != wantBatchID || !reflect.DeepEqual(sections, []string{"download_files"}) {
		t.Fatalf("publication artifacts events=%d tasks=%d outbox=%d kind=%q sections=%v batch=%q, want one download_added transaction",
			eventCount, taskCount, outboxCount, updateKind, sections, publicationBatchID)
	}
}
