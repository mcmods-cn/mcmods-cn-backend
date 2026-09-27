package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	appconfig "mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
	"mcmods-cn-backend/internal/security"
)

func TestTEST034FavoriteExportArtifactCommitCompensationAndRecoveryIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("TEST-034 exercises an isolated PostgreSQL generation and fake OSS lifecycle")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool := newTEST034IsolatedDatabase(t, ctx)

	var putRequests atomic.Int64
	provider := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPut {
			t.Errorf("unexpected fake OSS request %s %s", request.Method, request.URL)
			response.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		putRequests.Add(1)
		response.Header().Set("ETag", `"test034"`)
		response.WriteHeader(http.StatusOK)
	}))
	defer provider.Close()

	cfg := appconfig.Load()
	server := &Server{db: pool, cfg: cfg}
	ossConfig := ossConfigPayload{
		Enabled: true, Region: "cn-test034", Endpoint: provider.URL, PublicEndpoint: provider.URL,
		Bucket: "test034-bucket", AccessKeyID: "test034-key", AccessKeySecret: "test034-secret",
		UseCName: true, Prefix: "mcmods",
	}
	sealed, err := server.sealSystemSetting(ossConfig)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into system_settings(key,value) values('oss.aliyun',$1::jsonb)
		on conflict(key) do update set value=excluded.value`, sealed); err != nil {
		t.Fatal(err)
	}
	var ownerID, ownerAuthVersion int64
	var ownerPublicID string
	if err = pool.QueryRow(ctx, `insert into users(username,email,password_hash,status)
		values('test034-owner','test034-owner@example.test','not-used','active')
		returning id,public_id,auth_version`).Scan(&ownerID, &ownerPublicID, &ownerAuthVersion); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into favorite_collections(public_id,user_id,name,is_public)
		values('t034priv1',$1,'TEST-034 private collection',false)`, ownerID); err != nil {
		t.Fatal(err)
	}
	ownerClaims, err := security.NewClaims(ownerPublicID, "test034-owner", "test034-owner@example.test", ownerAuthVersion, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into auth_sessions(session_hash,user_id,auth_version,expires_at)
		values($1,$2,$3,$4)`, security.SessionFingerprint(ownerClaims.SessionID), ownerID, ownerAuthVersion, time.Unix(ownerClaims.ExpiresAt, 0)); err != nil {
		t.Fatal(err)
	}
	ownerToken, err := security.SignToken(cfg.JWTSecret, ownerClaims)
	if err != nil {
		t.Fatal(err)
	}
	permissionRequest := func(target http.HandlerFunc) *httptest.ResponseRecorder {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/api/v1/users/me/modpack-exports?status=all&limit=10", nil).WithContext(ctx)
		request.Header.Set("Authorization", "Bearer "+ownerToken)
		target(response, request)
		return response
	}
	if response := permissionRequest(server.requirePermission("favorite.modpack_export.view_own", server.favoriteModpackExports)); response.Code != http.StatusForbidden {
		t.Fatalf("view route without permission status=%d body=%s", response.Code, response.Body.String())
	}
	if _, err = pool.Exec(ctx, `insert into user_permissions(user_id,permission_id,allow)
		select $1,id,true from permissions where code='favorite.modpack_export.view_own'`, ownerID); err != nil {
		t.Fatal(err)
	}
	if response := permissionRequest(server.requirePermission("favorite.modpack_export.view_own", server.favoriteModpackExports)); response.Code != http.StatusOK {
		t.Fatalf("view route with permission status=%d body=%s", response.Code, response.Body.String())
	}
	noOp := func(response http.ResponseWriter, _ *http.Request) { response.WriteHeader(http.StatusNoContent) }
	if response := permissionRequest(server.requirePermission("favorite.modpack_export", noOp)); response.Code != http.StatusForbidden {
		t.Fatalf("create route without export permission status=%d body=%s", response.Code, response.Body.String())
	}
	if _, err = pool.Exec(ctx, `insert into user_permissions(user_id,permission_id,allow)
		select $1,id,true from permissions where code='favorite.modpack_export'`, ownerID); err != nil {
		t.Fatal(err)
	}
	if response := permissionRequest(server.requirePermission("favorite.modpack_export", noOp)); response.Code != http.StatusNoContent {
		t.Fatalf("create route with export permission status=%d body=%s", response.Code, response.Body.String())
	}
	var foreignOwnerID int64
	if err = pool.QueryRow(ctx, `insert into users(username,email,password_hash,status)
		values('test034-foreign','test034-foreign@example.test','not-used','active') returning id`).Scan(&foreignOwnerID); err != nil {
		t.Fatal(err)
	}
	foreignPreflight := httptest.NewRecorder()
	foreignPreflightRequest := httptest.NewRequest(http.MethodPost, "/api/v1/users/me/favorite-collections/t034priv1/modpack-exports/preflight",
		strings.NewReader(`{"minecraftVersion":"1.21.1","loader":"fabric"}`))
	foreignPreflightRequest.Header.Set("Content-Type", "application/json")
	foreignPreflightRequest.SetPathValue("id", "t034priv1")
	foreignPreflightRequest = foreignPreflightRequest.WithContext(context.WithValue(ctx, claimsContextKey, security.Claims{Subject: foreignOwnerID}))
	server.preflightFavoriteModpackExport(foreignPreflight, foreignPreflightRequest)
	if foreignPreflight.Code != http.StatusNotFound || !strings.Contains(foreignPreflight.Body.String(), "FAVORITE_COLLECTION_NOT_FOUND") {
		t.Fatalf("private collection IDOR status=%d body=%s", foreignPreflight.Code, foreignPreflight.Body.String())
	}
	if _, err = pool.Exec(ctx, `
		create table test034_faults(code text primary key);
		create function test034_reject_ready() returns trigger language plpgsql as $$
		begin
			if new.status='ready' and exists(select 1 from test034_faults where code='ready:'||new.public_id) then
				raise exception 'TEST034 ready commit rejected for %',new.public_id;
			end if;
			return new;
		end $$;
		create trigger test034_reject_ready before update on favorite_modpack_export_tasks
			for each row execute function test034_reject_ready();
		create function test034_reject_compensation() returns trigger language plpgsql as $$
		begin
			if new.reason='favorite_modpack_export_finalize_failed'
			   and exists(select 1 from test034_faults where code='reject:compensation') then
				raise exception 'TEST034 compensation persistence rejected';
			end if;
			return new;
		end $$;
		create trigger test034_reject_compensation before insert on oss_object_deletion_outbox
			for each row execute function test034_reject_compensation()
	`); err != nil {
		t.Fatal(err)
	}
	for _, taskID := range []string{"t034ok001", "t034cmp01", "t034orp01"} {
		insertTEST034ExportTask(t, ctx, pool, ownerID, taskID)
	}
	worker := NewFavoriteModpackExportWorker(cfg, pool, nil)

	if err = worker.process(ctx, "t034ok001"); err != nil {
		t.Fatalf("successful worker lifecycle: %v", err)
	}
	var readyFileID int64
	var readyStatus, readyFileStatus string
	if err = pool.QueryRow(ctx, `select task.status,file.id,file.status
		from favorite_modpack_export_tasks task join oss_files file on file.id=task.result_file_id
		where task.public_id='t034ok001'`).Scan(&readyStatus, &readyFileID, &readyFileStatus); err != nil {
		t.Fatal(err)
	}
	if readyStatus != "ready" || readyFileStatus != "active" {
		t.Fatalf("successful task/file state=%s/%s", readyStatus, readyFileStatus)
	}
	if err = worker.compensateUnlinkedFavoriteExportArtifact(ctx, "t034ok001", readyFileID); err != nil {
		t.Fatalf("ambiguous-commit guard: %v", err)
	}
	if _, err = pool.Exec(ctx, `update oss_files set created_at=now()-interval '1 hour' where id=$1`, readyFileID); err != nil {
		t.Fatal(err)
	}
	if err = worker.recoverOrphanedArtifacts(ctx); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `select status from oss_files where id=$1`, readyFileID).Scan(&readyFileStatus); err != nil {
		t.Fatal(err)
	}
	if readyFileStatus != "active" {
		t.Fatalf("linked artifact was recovered as an orphan: %s", readyFileStatus)
	}

	detail := httptest.NewRecorder()
	detailRequest := httptest.NewRequest(http.MethodGet, "/api/v1/users/me/modpack-exports/t034ok001", nil)
	detailRequest.SetPathValue("taskId", "t034ok001")
	detailRequest = detailRequest.WithContext(context.WithValue(ctx, claimsContextKey, security.Claims{Subject: ownerID}))
	server.favoriteModpackExportDetail(detail, detailRequest)
	if detail.Code != http.StatusOK || !strings.Contains(detail.Body.String(), `"downloadAvailable":true`) {
		t.Fatalf("ready detail status=%d body=%s", detail.Code, detail.Body.String())
	}
	download := httptest.NewRecorder()
	downloadRequest := httptest.NewRequest(http.MethodGet, "/api/v1/users/me/modpack-exports/t034ok001/download", nil)
	downloadRequest.SetPathValue("taskId", "t034ok001")
	downloadRequest = downloadRequest.WithContext(context.WithValue(ctx, claimsContextKey, security.Claims{Subject: ownerID}))
	server.downloadFavoriteModpackExport(download, downloadRequest)
	if download.Code != http.StatusTemporaryRedirect || !strings.HasPrefix(download.Header().Get("Location"), provider.URL) {
		t.Fatalf("ready download status=%d location=%q body=%s", download.Code, download.Header().Get("Location"), download.Body.String())
	}
	foreignDetail := httptest.NewRecorder()
	foreignDetailRequest := httptest.NewRequest(http.MethodGet, "/api/v1/users/me/modpack-exports/t034ok001", nil)
	foreignDetailRequest.SetPathValue("taskId", "t034ok001")
	foreignDetailRequest = foreignDetailRequest.WithContext(context.WithValue(ctx, claimsContextKey, security.Claims{Subject: foreignOwnerID}))
	server.favoriteModpackExportDetail(foreignDetail, foreignDetailRequest)
	if foreignDetail.Code != http.StatusNotFound {
		t.Fatalf("foreign task detail status=%d body=%s", foreignDetail.Code, foreignDetail.Body.String())
	}
	foreignDownload := httptest.NewRecorder()
	foreignDownloadRequest := httptest.NewRequest(http.MethodGet, "/api/v1/users/me/modpack-exports/t034ok001/download", nil)
	foreignDownloadRequest.SetPathValue("taskId", "t034ok001")
	foreignDownloadRequest = foreignDownloadRequest.WithContext(context.WithValue(ctx, claimsContextKey, security.Claims{Subject: foreignOwnerID}))
	server.downloadFavoriteModpackExport(foreignDownload, foreignDownloadRequest)
	if foreignDownload.Code != http.StatusGone {
		t.Fatalf("foreign task download status=%d body=%s", foreignDownload.Code, foreignDownload.Body.String())
	}

	if _, err = pool.Exec(ctx, `insert into test034_faults(code) values('ready:t034cmp01')`); err != nil {
		t.Fatal(err)
	}
	err = worker.process(ctx, "t034cmp01")
	if err == nil || !strings.Contains(err.Error(), "TEST034 ready commit rejected") {
		t.Fatalf("ready persistence failure=%v", err)
	}
	var processingStatus string
	var unlinked bool
	var compensatedFileID int64
	if err = pool.QueryRow(ctx, `select task.status,task.result_file_id is null,file.id
		from favorite_modpack_export_tasks task join oss_files file
		  on file.object_key like '%/modpack-exports/'||task.public_id||'/%'
		where task.public_id='t034cmp01' and file.status='deleted'`).Scan(&processingStatus, &unlinked, &compensatedFileID); err != nil {
		t.Fatal(err)
	}
	var compensationReason, compensationStatus string
	if err = pool.QueryRow(ctx, `select reason,status from oss_object_deletion_outbox where oss_file_id=$1`, compensatedFileID).
		Scan(&compensationReason, &compensationStatus); err != nil {
		t.Fatal(err)
	}
	if processingStatus != "processing" || !unlinked || compensationReason != "favorite_modpack_export_finalize_failed" || compensationStatus != "pending" {
		t.Fatalf("immediate compensation task/unlinked/outbox=%s/%t/%s/%s", processingStatus, unlinked, compensationReason, compensationStatus)
	}
	if _, err = pool.Exec(ctx, `delete from test034_faults where code='ready:t034cmp01';
		update favorite_modpack_export_tasks set status='pending',stage='retry_wait',lease_token='',lease_expires_at=now()-interval '1 second'
		where public_id='t034cmp01'`); err != nil {
		t.Fatal(err)
	}
	if err = worker.process(ctx, "t034cmp01"); err != nil {
		t.Fatalf("retry after compensated completion: %v", err)
	}
	var activeArtifacts, deletedArtifacts, linkedArtifacts int
	if err = pool.QueryRow(ctx, `select
		count(*) filter(where file.status='active'),count(*) filter(where file.status='deleted'),
		count(*) filter(where task.result_file_id=file.id)
		from favorite_modpack_export_tasks task join oss_files file
		  on file.object_key like '%/modpack-exports/'||task.public_id||'/%'
		where task.public_id='t034cmp01'`).Scan(&activeArtifacts, &deletedArtifacts, &linkedArtifacts); err != nil {
		t.Fatal(err)
	}
	if activeArtifacts != 1 || deletedArtifacts != 1 || linkedArtifacts != 1 {
		t.Fatalf("retry artifacts active/deleted/linked=%d/%d/%d", activeArtifacts, deletedArtifacts, linkedArtifacts)
	}

	if _, err = pool.Exec(ctx, `insert into test034_faults(code) values('ready:t034orp01'),('reject:compensation')`); err != nil {
		t.Fatal(err)
	}
	err = worker.process(ctx, "t034orp01")
	if err == nil || !strings.Contains(err.Error(), "TEST034 ready commit rejected") || !strings.Contains(err.Error(), "TEST034 compensation persistence rejected") {
		t.Fatalf("joined completion/compensation failure=%v", err)
	}
	var orphanFileID int64
	if err = pool.QueryRow(ctx, `select file.id from oss_files file
		where file.object_key like '%/modpack-exports/t034orp01/%' and file.status='active'`).Scan(&orphanFileID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `delete from test034_faults`); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `update oss_files set created_at=now()-interval '1 hour' where id=$1`, orphanFileID); err != nil {
		t.Fatal(err)
	}
	if err = worker.recoverOrphanedArtifacts(ctx); err != nil {
		t.Fatalf("periodic orphan recovery: %v", err)
	}
	if err = pool.QueryRow(ctx, `select file.status,outbox.reason,outbox.status
		from oss_files file join oss_object_deletion_outbox outbox on outbox.oss_file_id=file.id
		where file.id=$1`, orphanFileID).Scan(&readyFileStatus, &compensationReason, &compensationStatus); err != nil {
		t.Fatal(err)
	}
	if readyFileStatus != "deleted" || compensationReason != "favorite_modpack_export_orphaned" || compensationStatus != "pending" {
		t.Fatalf("recovered orphan file/reason/outbox=%s/%s/%s", readyFileStatus, compensationReason, compensationStatus)
	}

	if _, err = pool.Exec(ctx, `update favorite_modpack_export_tasks set expires_at=now()-interval '1 second'
		where public_id='t034ok001'`); err != nil {
		t.Fatal(err)
	}
	if err = worker.expireCompleted(ctx); err != nil {
		t.Fatalf("expire completed artifact: %v", err)
	}
	var expiredStatus, expiredFileStatus, expiredReason string
	if err = pool.QueryRow(ctx, `select task.status,file.status,outbox.reason
		from favorite_modpack_export_tasks task join oss_files file on file.id=task.result_file_id
		join oss_object_deletion_outbox outbox on outbox.oss_file_id=file.id
		where task.public_id='t034ok001'`).Scan(&expiredStatus, &expiredFileStatus, &expiredReason); err != nil {
		t.Fatal(err)
	}
	if expiredStatus != "expired" || expiredFileStatus != "deleted" || expiredReason != "favorite_modpack_export_expired" {
		t.Fatalf("expiry task/file/reason=%s/%s/%s", expiredStatus, expiredFileStatus, expiredReason)
	}
	history := httptest.NewRecorder()
	historyRequest := httptest.NewRequest(http.MethodGet, "/api/v1/users/me/modpack-exports?status=all&limit=10", nil)
	historyRequest = historyRequest.WithContext(context.WithValue(ctx, claimsContextKey, security.Claims{Subject: ownerID}))
	server.favoriteModpackExports(history, historyRequest)
	if history.Code != http.StatusOK {
		t.Fatalf("history status=%d body=%s", history.Code, history.Body.String())
	}
	var historyEnvelope struct {
		Data struct {
			Items []favoriteModpackExportSummary `json:"items"`
		} `json:"data"`
	}
	if err = json.Unmarshal(history.Body.Bytes(), &historyEnvelope); err != nil {
		t.Fatal(err)
	}
	if len(historyEnvelope.Data.Items) != 3 {
		t.Fatalf("retained history tasks=%d body=%s", len(historyEnvelope.Data.Items), history.Body.String())
	}
	if got := putRequests.Load(); got != 4 {
		t.Fatalf("fake OSS PUT requests=%d want 4 (success, compensated, retry, recovered orphan)", got)
	}
	var reportSnapshotColumns int
	if err = pool.QueryRow(ctx, `select count(*) from information_schema.columns
		where table_schema='public' and table_name='favorite_modpack_export_tasks' and column_name='report_snapshot'`).Scan(&reportSnapshotColumns); err != nil {
		t.Fatal(err)
	}
	if reportSnapshotColumns != 0 {
		t.Fatal("dead report_snapshot column is still installed")
	}
}

func insertTEST034ExportTask(t *testing.T, ctx context.Context, pool *pgxpool.Pool, ownerID int64, taskID string) {
	t.Helper()
	var internalID int64
	if err := pool.QueryRow(ctx, `insert into favorite_modpack_export_tasks(
		public_id,owner_user_id,collection_public_id_snapshot,pack_name,pack_version_id,minecraft_version,
		loader_type,loader_version,status,stage,collection_item_count,exported_mod_count,final_file_count)
		values($1,$2,'t034col01','TEST-034 pack '||$1,'1.0.0','1.21.1','fabric','0.16.14',
		'pending','queued',1,1,1) returning id`, taskID, ownerID).Scan(&internalID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `insert into favorite_modpack_export_items(
		task_id,source_project_type,source_project_name_snapshot,result_type,modrinth_project_id,modrinth_version_id,
		selected_version_name,selected_file_name,minecraft_version,loader,release_type,env_client,env_server,
		file_size,sha1,sha512,download_url,dependency_of)
		values($1,'mod','TEST-034 dependency','exported','test034-project','test034-version','TEST-034 version',
		$2||'.jar','1.21.1','fabric','release','required','required',1024,repeat('1',40),repeat('2',128),
		'https://cdn.modrinth.com/data/test034-project/versions/test034-version/'||$2||'.jar','[]'::jsonb)`, internalID, taskID); err != nil {
		t.Fatal(err)
	}
}

func newTEST034IsolatedDatabase(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()
	baseConfig, err := pgxpool.ParseConfig(appconfig.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	adminPool, err := pgxpool.NewWithConfig(ctx, baseConfig)
	if err != nil {
		t.Fatal(err)
	}
	databaseName := fmt.Sprintf("test034_mrpack_%d", time.Now().UnixNano())
	namePattern := regexp.MustCompile(`^test034_mrpack_[0-9]+$`)
	if !namePattern.MatchString(databaseName) {
		adminPool.Close()
		t.Fatalf("refuse unsafe TEST034 database name %q", databaseName)
	}
	quotedName := pgx.Identifier{databaseName}.Sanitize()
	if _, err = adminPool.Exec(ctx, "create database "+quotedName); err != nil {
		adminPool.Close()
		t.Fatal(err)
	}
	var pool *pgxpool.Pool
	t.Cleanup(func() {
		if pool != nil {
			pool.Close()
		}
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		if !namePattern.MatchString(databaseName) {
			t.Errorf("refuse unsafe TEST034 cleanup database name %q", databaseName)
		} else if _, dropErr := adminPool.Exec(cleanupCtx, "drop database "+quotedName+" with (force)"); dropErr != nil {
			t.Errorf("drop isolated TEST034 database: %v", dropErr)
		}
		adminPool.Close()
	})
	scopedConfig, err := pgxpool.ParseConfig(appconfig.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	scopedConfig.ConnConfig.Database = databaseName
	scopedConfig.MaxConns, scopedConfig.MinConns = 8, 1
	pool, err = pgxpool.NewWithConfig(ctx, scopedConfig)
	if err != nil {
		t.Fatal(err)
	}
	if err = database.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	return pool
}
