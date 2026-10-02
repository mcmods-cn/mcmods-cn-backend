package database

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
)

func TestProjectFileIdentityStaysScopedWithoutPublicRouteIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify project file route ownership")
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
	if err = InstallEphemeralSchema(ctx, pool); err != nil {
		t.Fatal(err)
	}
	cleaned := false
	defer func() {
		if !cleaned {
			if dropErr := DropEphemeralSchema(context.Background(), pool); dropErr != nil {
				t.Errorf("drop ephemeral schema: %v", dropErr)
			}
		}
	}()
	var generation int
	if err = pool.QueryRow(ctx, `select generation from schema_metadata where singleton`).Scan(&generation); err != nil || generation != schemaGeneration {
		t.Fatalf("temporary schema generation=%d err=%v", generation, err)
	}

	var projectRouteID, ossFileID int64
	if err = pool.QueryRow(ctx, `insert into public_routes(public_id,entity_type,internal_id,canonical_path)
		values('db005rt01','mod',950001,'/mods/db005rt01') returning id`).Scan(&projectRouteID); err != nil {
		t.Fatal(err)
	}
	if projectRouteID == 0 {
		t.Fatal("project route was not created")
	}
	if err = pool.QueryRow(ctx, `insert into oss_files(object_key,status,scan_status)
		values('project/db005.jar','active','clean') returning id`).Scan(&ossFileID); err != nil {
		t.Fatal(err)
	}
	var filePublicID string
	if err = pool.QueryRow(ctx, `insert into project_files(
		project_type,project_internal_id,oss_file_id,display_name,file_name,status)
		values('mod',950001,$1,'DB-005 file','db005.jar','active') returning public_id`, ossFileID).Scan(&filePublicID); err != nil {
		t.Fatal(err)
	}
	var registryRows, routeRows, scopedActiveRows int
	if err = pool.QueryRow(ctx, `select count(*)::int from public_id_registry where public_id=$1`, filePublicID).Scan(&registryRows); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `select count(*)::int from public_routes
		where public_id=$1 or entity_type='project_file'`, filePublicID).Scan(&routeRows); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `select count(*)::int from project_files project_file
		join oss_files oss on oss.id=project_file.oss_file_id and oss.status='active' and oss.scan_status='clean'
		where project_file.public_id=$1 and project_file.project_type='mod'
		  and project_file.project_internal_id=950001 and project_file.status='active'`, filePublicID).Scan(&scopedActiveRows); err != nil {
		t.Fatal(err)
	}
	if registryRows != 1 || routeRows != 0 || scopedActiveRows != 1 {
		t.Fatalf("active file registry=%d generic routes=%d scoped rows=%d", registryRows, routeRows, scopedActiveRows)
	}
	if _, err = pool.Exec(ctx, `update project_files set status='deleted',updated_at=now() where public_id=$1`, filePublicID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `select
		(select count(*) from public_routes where public_id=$1 or entity_type='project_file')::int,
		(select count(*) from project_files project_file join oss_files oss on oss.id=project_file.oss_file_id
		 where project_file.public_id=$1 and project_file.status='active' and oss.status='active' and oss.scan_status='clean')::int`,
		filePublicID).Scan(&routeRows, &scopedActiveRows); err != nil {
		t.Fatal(err)
	}
	if routeRows != 0 || scopedActiveRows != 0 {
		t.Fatalf("soft-deleted file generic routes=%d scoped active rows=%d", routeRows, scopedActiveRows)
	}

	if err = DropEphemeralSchema(ctx, pool); err != nil {
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
