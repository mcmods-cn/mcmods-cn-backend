package httpapi

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
)

func TestProjectMirrorIdentityAndUploadCompensationIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify project mirror identity and cleanup")
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
	var publicGenerationBefore int
	if err = pool.QueryRow(ctx, `select generation from public.schema_metadata where singleton`).Scan(&publicGenerationBefore); err != nil {
		t.Fatal(err)
	}
	if err = database.InstallEphemeralSchema(ctx, pool); err != nil {
		t.Fatal(err)
	}
	cleaned := false
	defer func() {
		if !cleaned {
			if dropErr := database.DropEphemeralSchema(context.Background(), pool); dropErr != nil {
				t.Errorf("drop ephemeral schema: %v", dropErr)
			}
		}
	}()
	var generation int
	if err = pool.QueryRow(ctx, `select generation from schema_metadata where singleton`).Scan(&generation); err != nil || generation < 148 {
		t.Fatalf("temporary schema generation=%d err=%v", generation, err)
	}

	if _, err = pool.Exec(ctx, `insert into public_routes(public_id,entity_type,internal_id) values
		('db006rt01','mod',900001),('db006rt02','mod',900002)`); err != nil {
		t.Fatal(err)
	}
	var firstRouteID, secondRouteID int64
	if err = pool.QueryRow(ctx, `select id from public_routes where public_id='db006rt01'`).Scan(&firstRouteID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `select id from public_routes where public_id='db006rt02'`).Scan(&secondRouteID); err != nil {
		t.Fatal(err)
	}
	var firstOSSFileID, secondOSSFileID int64
	if err = pool.QueryRow(ctx, `insert into oss_files(object_key,sha256,size_bytes,source_size_bytes) values
		('project/one.jar','same-content',4096,4096) returning id`).Scan(&firstOSSFileID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `insert into oss_files(object_key,sha256,size_bytes,source_size_bytes) values
		('project/two.jar','same-content',4096,4096) returning id`).Scan(&secondOSSFileID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into mirrored_project_files(
		project_route_id,source_type,external_file_id,file_sha256,byte_size,oss_file_id,status) values
		($1,'modrinth','file-one','same-content',4096,$2,'scanning'),
		($3,'modrinth','file-two','same-content',4096,$4,'scanning')`,
		firstRouteID, firstOSSFileID, secondRouteID, secondOSSFileID); err != nil {
		t.Fatalf("distinct provider files with identical bytes did not coexist: %v", err)
	}
	var mirrors, providerIdentityConstraints, globalConstraints int
	if err = pool.QueryRow(ctx, `select count(*)::int from mirrored_project_files
		where source_type='modrinth' and file_sha256='same-content' and byte_size=4096`).Scan(&mirrors); err != nil || mirrors != 2 {
		t.Fatalf("same-content mirror rows=%d err=%v", mirrors, err)
	}
	if err = pool.QueryRow(ctx, `select
		count(*) filter(where pg_get_constraintdef(oid)='UNIQUE (project_route_id, source_type, external_file_id)')::int,
		count(*) filter(where pg_get_constraintdef(oid) in ('UNIQUE (source_type, external_file_id)','UNIQUE (source_type, file_sha256, byte_size)'))::int
		from pg_constraint where conrelid='mirrored_project_files'::regclass and contype='u'`).
		Scan(&providerIdentityConstraints, &globalConstraints); err != nil {
		t.Fatal(err)
	}
	if providerIdentityConstraints != 1 || globalConstraints != 0 {
		t.Fatalf("project-scoped provider identity constraints=%d global constraints=%d", providerIdentityConstraints, globalConstraints)
	}

	// Registration identity remains independent of content identity. Unbound transfers
	// keep their own expiry even if an unrelated file has exactly the same bytes.
	cfg := ossConfigPayload{Bucket: "owned", Endpoint: "https://owned.invalid", Region: "cn-test"}
	for _, key := range []string{"project/first-attempt.jar", "project/second-attempt.jar"} {
		if _, err = scheduleOSSUploadExpiry(ctx, pool, cfg, key, projectMirrorUploadExpiryReason, nil); err != nil {
			t.Fatal(err)
		}
	}
	var receipts int
	if err = pool.QueryRow(ctx, `select count(*) from oss_object_deletion_outbox where bucket=$1 and reason=$2
		and oss_file_id is null and status='pending' and attempts=0
		and next_attempt_at>now()+interval '23 hours' and object_key in ('project/first-attempt.jar','project/second-attempt.jar')`,
		cfg.Bucket, projectMirrorUploadExpiryReason).Scan(&receipts); err != nil || receipts != 2 {
		t.Fatalf("independent delayed upload receipts=%d err=%v", receipts, err)
	}

	if err = database.DropEphemeralSchema(ctx, pool); err != nil {
		t.Fatal(err)
	}
	cleaned = true
	var publicGenerationAfter int
	if err = pool.QueryRow(ctx, `select generation from public.schema_metadata where singleton`).Scan(&publicGenerationAfter); err != nil {
		t.Fatal(err)
	}
	if publicGenerationAfter != publicGenerationBefore {
		t.Fatalf("public generation changed from %d to %d", publicGenerationBefore, publicGenerationAfter)
	}
}
