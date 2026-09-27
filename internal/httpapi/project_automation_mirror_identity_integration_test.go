package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
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
	var mirrors, providerIdentityConstraints, contentConstraints int
	if err = pool.QueryRow(ctx, `select count(*)::int from mirrored_project_files
		where source_type='modrinth' and file_sha256='same-content' and byte_size=4096`).Scan(&mirrors); err != nil || mirrors != 2 {
		t.Fatalf("same-content mirror rows=%d err=%v", mirrors, err)
	}
	if err = pool.QueryRow(ctx, `select
		count(*) filter(where pg_get_constraintdef(oid)='UNIQUE (source_type, external_file_id)')::int,
		count(*) filter(where pg_get_constraintdef(oid)='UNIQUE (source_type, file_sha256, byte_size)')::int
		from pg_constraint where conrelid='mirrored_project_files'::regclass and contype='u'`).
		Scan(&providerIdentityConstraints, &contentConstraints); err != nil {
		t.Fatal(err)
	}
	if providerIdentityConstraints != 1 || contentConstraints != 0 {
		t.Fatalf("provider identity constraints=%d content constraints=%d", providerIdentityConstraints, contentConstraints)
	}

	var requestMu sync.Mutex
	requests := make([]string, 0)
	ossHTTP := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestMu.Lock()
		requests = append(requests, r.Method+" "+r.URL.Path)
		requestMu.Unlock()
		if strings.Contains(r.URL.Path, "cleanup-fails") {
			w.Header().Set("Content-Type", "application/xml")
			w.WriteHeader(http.StatusForbidden)
			_, _ = fmt.Fprint(w, `<Error><Code>AccessDenied</Code><Message>denied</Message></Error>`)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer ossHTTP.Close()
	ossConfig := ossConfigPayload{
		Enabled: true, Region: "cn-test", Endpoint: ossHTTP.URL, Bucket: "mirror-bucket",
		AccessKeyID: "test-access-key", AccessKeySecret: "test-secret", UseCName: true,
	}
	ossClient := newOSSClient(ossConfig, ossConfig.Endpoint, true)
	worker := &ProjectAutomationWorker{server: &Server{db: pool}}
	canceledContext, cancelCleanup := context.WithCancel(ctx)
	cancelCleanup()
	if err = worker.cleanupUnregisteredProjectMirror(canceledContext, ossClient, ossConfig, "project/cleanup-succeeds.jar"); err != nil {
		t.Fatalf("detached direct cleanup: %v", err)
	}
	requestMu.Lock()
	directRequests := len(requests)
	requestMu.Unlock()
	if directRequests != 1 {
		t.Fatalf("direct cleanup requests=%d want 1", directRequests)
	}

	if _, err = pool.Exec(ctx, `insert into oss_files(object_key) values('project/already-registered.jar')`); err != nil {
		t.Fatal(err)
	}
	if err = worker.cleanupUnregisteredProjectMirror(ctx, ossClient, ossConfig, "project/already-registered.jar"); err != nil {
		t.Fatal(err)
	}
	requestMu.Lock()
	requestsAfterRegistered := len(requests)
	requestMu.Unlock()
	if requestsAfterRegistered != directRequests {
		t.Fatal("cleanup deleted an object that already had a persistent OSS record")
	}

	err = worker.cleanupUnregisteredProjectMirror(ctx, ossClient, ossConfig, "project/cleanup-fails.jar")
	if err == nil || !strings.Contains(err.Error(), "durable deletion queued") {
		t.Fatalf("failed direct cleanup did not report its durable fallback: %v", err)
	}
	var cleanupStatus, cleanupReason string
	if err = pool.QueryRow(ctx, `select status,reason from oss_object_deletion_outbox
		where object_key='project/cleanup-fails.jar'`).Scan(&cleanupStatus, &cleanupReason); err != nil {
		t.Fatal(err)
	}
	if cleanupStatus != "pending" || cleanupReason != "project-automation-registration-failed" {
		t.Fatalf("durable cleanup status=%q reason=%q", cleanupStatus, cleanupReason)
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
