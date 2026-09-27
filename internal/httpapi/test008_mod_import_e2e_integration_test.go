package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/queue"
	"mcmods-cn-backend/internal/security"
)

func TestTEST008EmbeddedCatalogHTTPOutboxOSSWorkerLifecycleIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify the Mod import end-to-end lifecycle")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	// Reuse the existing random, automatically dropped database fixture so
	// concurrent upload/cancellation connections see the same real schema.
	pool := newTEST018IsolatedDatabase(t, ctx)

	entry := embeddedIconCatalogEntry{
		Name: "测试方块", EnglishName: "Test Block", RegisterName: "example:test_block", Type: "Block",
		SmallIcon: embeddedIconTestPNG(t, 32, 32), LargeIcon: embeddedIconTestPNG(t, 32, 32),
	}
	providerPayload, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	providerPayload = append(providerPayload, '\n')
	providerDigest := sha256.Sum256(providerPayload)
	providerSHA := hex.EncodeToString(providerDigest[:])
	var providerMu sync.Mutex
	providerGets, providerPuts, providerDeletes := 0, 0, 0
	provider := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		providerMu.Lock()
		switch request.Method {
		case http.MethodGet:
			providerGets++
		case http.MethodPut:
			providerPuts++
		case http.MethodDelete:
			providerDeletes++
		}
		providerMu.Unlock()
		switch request.Method {
		case http.MethodGet:
			response.Header().Set("Content-Length", fmt.Sprintf("%d", len(providerPayload)))
			response.Header().Set("ETag", `"test008-source"`)
			response.WriteHeader(http.StatusOK)
			_, _ = response.Write(providerPayload)
		case http.MethodPut:
			response.Header().Set("ETag", `"test008-derived"`)
			response.WriteHeader(http.StatusOK)
		case http.MethodDelete:
			response.WriteHeader(http.StatusNoContent)
		default:
			response.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer provider.Close()

	server := &Server{db: pool, cfg: config.Load()}
	ossConfig := ossConfigPayload{
		Enabled: true, Region: "cn-test", Endpoint: provider.URL, PublicEndpoint: "https://public.example.test",
		Bucket: "catalog-test", AccessKeyID: "test008-key", AccessKeySecret: "test008-secret", UseCName: true, Prefix: "mcmods",
	}
	sealed, err := server.sealSystemSetting(ossConfig)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into system_settings(key,value) values('oss.aliyun',$1::jsonb)
		on conflict(key) do update set value=excluded.value`, sealed); err != nil {
		t.Fatal(err)
	}

	nonce := fmt.Sprintf("%x", time.Now().UnixNano())
	var actorID, modID, versionID, sourceFileID int64
	var versionPublicID, sourceFilePublicID string
	projectCode := randomCatalogPublicID()
	modSiteID := "test008-" + nonce
	if err = pool.QueryRow(ctx, `insert into users(username,email,password_hash,email_verified)
		values($1,$2,'test-only',true) returning id`, "test008_"+nonce, "test008_"+nonce+"@example.invalid").Scan(&actorID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `insert into mods(project_code,slug,primary_name,review_status,submitted_by)
		values($1,$2,'TEST-008 import','approved',$3) returning id`, projectCode, modSiteID, actorID).Scan(&modID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into mod_identifiers(mod_id,identifier,is_primary) values($1,'example',true)`, modID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `insert into mod_content_versions(mod_id,label,status,created_by,updated_by)
		values($1,'TEST-008','active',$2,$2) returning id,public_id`, modID, actorID).Scan(&versionID, &versionPublicID); err != nil {
		t.Fatal(err)
	}
	objectKey := "mcmods/test008/source-" + nonce + ".json"
	category := ossModImportCategory(projectCode, iconRendererImportSource, "catalog")
	if err = pool.QueryRow(ctx, `insert into oss_files(
		bucket,endpoint,region,object_key,category,source,original_name,source_original_name,
		content_type,size_bytes,source_size_bytes,sha256,uploader_id,status,scan_status)
		values($1,$2,$3,$4,$5,$6,'catalog.json','catalog.json','application/json',$7,$7,$8,$9,'active','clean')
		returning id,public_id`, ossConfig.Bucket, ossConfig.Endpoint, ossConfig.Region, objectKey, category,
		iconRendererImportSource, len(providerPayload), providerSHA, actorID).Scan(&sourceFileID, &sourceFilePublicID); err != nil {
		t.Fatal(err)
	}

	requestBody := fmt.Sprintf(`{"ossFileId":%q,"targetVersionPublicId":%q,"source":"iconrenderer"}`,
		sourceFilePublicID, versionPublicID)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/mods/"+modSiteID+"/catalog-imports", bytes.NewBufferString(requestBody))
	request.SetPathValue("siteId", modSiteID)
	request = request.WithContext(context.WithValue(ctx, claimsContextKey, security.Claims{
		Subject: actorID, PermissionRules: []security.PermissionRule{{Code: "project.edit", Allow: true, Priority: 100}},
	}))
	response := httptest.NewRecorder()
	server.createEmbeddedIconImportJob(response, request)
	if response.Code != http.StatusAccepted {
		t.Fatalf("create import HTTP status=%d body=%s", response.Code, response.Body.String())
	}
	var jobID string
	var outboxEvents int
	if err = pool.QueryRow(ctx, `select id from catalog_import_jobs where mod_id=$1 and target_version_id=$2`, modID, versionID).Scan(&jobID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `select count(*) from nats_outbox
		where aggregate_type='mod_export_job' and aggregate_id=$1 and event_type='mod.embedded_icon_import.requested'`, jobID).Scan(&outboxEvents); err != nil {
		t.Fatal(err)
	}
	if outboxEvents != 1 {
		t.Fatalf("HTTP creation produced %d Outbox events, want one", outboxEvents)
	}

	queueClient := queue.New(ctx, config.NATSConfig{})
	defer queueClient.Close()
	worker := &ModExportWorker{server: server, queue: queueClient}
	if err = worker.Start(); err != nil {
		t.Fatal(err)
	}
	// Exercise the actual PostgreSQL dispatcher using its supported bounded
	// local delivery path. JetStream transport has separate queue tests.
	dispatcher := queue.NewOutboxDispatcher(pool, queueClient, true)
	if count, dispatchErr := dispatcher.DispatchBatch(ctx, 1); dispatchErr != nil || count != 1 || dispatcher.Metrics().Published != 1 {
		t.Fatalf("dispatch count=%d metrics=%+v error=%v", count, dispatcher.Metrics(), dispatchErr)
	}
	var outboxStatus string
	if err = pool.QueryRow(ctx, `select status from nats_outbox where aggregate_type='mod_export_job' and aggregate_id=$1`, jobID).Scan(&outboxStatus); err != nil || outboxStatus != "published" {
		t.Fatalf("outbox status=%s error=%v", outboxStatus, err)
	}
	var jobStatus, sourceStatus string
	var artifactCount, activeArtifacts, derivedFiles, sourceDeletions int
	if err = pool.QueryRow(ctx, `select status from catalog_import_jobs where id=$1`, jobID).Scan(&jobStatus); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `select status from oss_files where id=$1`, sourceFileID).Scan(&sourceStatus); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `select count(*),count(*) filter(where artifact.status='active'),
		count(*) filter(where file.status='active') from catalog_import_job_artifacts artifact
		join oss_files file on file.id=artifact.oss_file_id where artifact.job_id=$1`, jobID).
		Scan(&artifactCount, &activeArtifacts, &derivedFiles); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `select count(*) from oss_object_deletion_outbox
		where oss_file_id=$1 and reason='catalog-import-consumed'`, sourceFileID).Scan(&sourceDeletions); err != nil {
		t.Fatal(err)
	}
	providerMu.Lock()
	gets, puts := providerGets, providerPuts
	providerMu.Unlock()
	if jobStatus != "ready" || sourceStatus != "deleted" || artifactCount != 3 || activeArtifacts != 3 || derivedFiles != 3 ||
		sourceDeletions != 1 || gets != 1 || puts != 3 {
		t.Fatalf("lifecycle job/source/artifacts/files/deletion/provider=%s/%s/%d/%d/%d/%d/%dGET/%dPUT",
			jobStatus, sourceStatus, artifactCount, activeArtifacts, derivedFiles, sourceDeletions, gets, puts)
	}
	if !strings.Contains(response.Body.String(), jobID) {
		t.Fatalf("HTTP response did not expose created job %s: %s", jobID, response.Body.String())
	}

	cancelJobID, cancelRunToken := newExportID(), newExportID()
	if _, err = pool.Exec(ctx, `insert into catalog_import_jobs(
		id,mod_id,package_id,target_version_id,importer_version,status,progress,current_stage,created_by,run_token,heartbeat_at)
		select $1,mod_id,package_id,target_version_id,'test008-cancel','importing',70,'assets',created_by,$2,now()
		from catalog_import_jobs where id=$3`, cancelJobID, cancelRunToken, jobID); err != nil {
		t.Fatal(err)
	}
	cancelAsset := modExportUploadAsset{
		ObjectKey: "mcmods/test008/cancel-" + nonce + ".png", Original: "cancel.png",
		Digest: strings.Repeat("c", 64), ContentType: "image/png", ByteLength: 3, Data: []byte("png"),
	}
	if _, err = server.registerCatalogImportArtifacts(
		ctx, cancelJobID, cancelRunToken, ossConfig, actorID, iconRendererImportSource, []modExportUploadAsset{cancelAsset},
	); err != nil {
		t.Fatal(err)
	}
	putEntered, releasePut := make(chan struct{}), make(chan struct{})
	putResult := make(chan error, 1)
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releasePut) }) }
	defer release()
	go func() {
		putResult <- server.catalogImportUploadGuard(cancelJobID, cancelRunToken)(ctx, cancelAsset.ObjectKey, func(uploadCtx context.Context) error {
			close(putEntered)
			select {
			case <-releasePut:
				return nil
			case <-uploadCtx.Done():
				return uploadCtx.Err()
			}
		})
	}()
	select {
	case <-putEntered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	cancelTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = cancelTx.Exec(ctx, `set local lock_timeout='100ms'`); err != nil {
		_ = cancelTx.Rollback(ctx)
		t.Fatal(err)
	}
	err = server.compensateCatalogImportArtifactsTx(ctx, cancelTx, cancelJobID, cancelRunToken, "test008-concurrent-cancel")
	_ = cancelTx.Rollback(ctx)
	var lockError *pgconn.PgError
	if !errors.As(err, &lockError) || lockError.Code != "55P03" {
		t.Fatalf("compensation overtook an in-flight PUT: %v", err)
	}
	release()
	if err = <-putResult; err != nil {
		t.Fatal(err)
	}
	cancelRequest := httptest.NewRequest(http.MethodPost, "/api/v1/mods/"+modSiteID+"/catalog-imports/"+cancelJobID+"/cancel", nil)
	cancelRequest.SetPathValue("siteId", modSiteID)
	cancelRequest.SetPathValue("jobId", cancelJobID)
	cancelRequest = cancelRequest.WithContext(context.WithValue(ctx, claimsContextKey, security.Claims{
		Subject: actorID, PermissionRules: []security.PermissionRule{{Code: "project.edit", Allow: true, Priority: 100}},
	}))
	cancelResponse := httptest.NewRecorder()
	server.cancelModExportJob(cancelResponse, cancelRequest)
	if cancelResponse.Code != http.StatusOK {
		t.Fatalf("cancel import HTTP status=%d body=%s", cancelResponse.Code, cancelResponse.Body.String())
	}
	var cancelJobStatus, cancelArtifactStatus, cancelFileStatus string
	var cancelDeletion int
	if err = pool.QueryRow(ctx, `select job.status,artifact.status,file.status
		from catalog_import_jobs job join catalog_import_job_artifacts artifact on artifact.job_id=job.id
		join oss_files file on file.id=artifact.oss_file_id where job.id=$1`, cancelJobID).
		Scan(&cancelJobStatus, &cancelArtifactStatus, &cancelFileStatus); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `select count(*) from oss_object_deletion_outbox deletion
		join catalog_import_job_artifacts artifact on artifact.oss_file_id=deletion.oss_file_id
		where artifact.job_id=$1 and deletion.reason='catalog-import-cancelled'`, cancelJobID).Scan(&cancelDeletion); err != nil {
		t.Fatal(err)
	}
	if cancelJobStatus != "cancelled" || cancelArtifactStatus != "abandoned" || cancelFileStatus != "deleted" || cancelDeletion != 1 {
		t.Fatalf("cancel job/artifact/file/deletion=%s/%s/%s/%d", cancelJobStatus, cancelArtifactStatus, cancelFileStatus, cancelDeletion)
	}
	latePut := false
	err = server.catalogImportUploadGuard(cancelJobID, cancelRunToken)(ctx, cancelAsset.ObjectKey, func(context.Context) error {
		latePut = true
		return nil
	})
	if !errors.Is(err, errModExportLeaseLost) || latePut {
		t.Fatalf("cancelled attempt allowed late PUT: called=%t error=%v", latePut, err)
	}

	// Fail after every provider PUT but inside final activation. Existing
	// active artifacts remain valid; NOT VALID applies the injected check to
	// new writes only, in this automatically dropped test database.
	failedJobID := newExportID()
	var failedVersionID int64
	if err = pool.QueryRow(ctx, `insert into mod_content_versions(mod_id,label,status,created_by,updated_by)
		values($1,'TEST-008 failure','active',$2,$2) returning id`, modID, actorID).Scan(&failedVersionID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `update oss_files set status='active' where id=$1`, sourceFileID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into catalog_import_jobs(
		id,mod_id,package_id,target_version_id,importer_version,status,progress,current_stage,created_by)
		select $1,mod_id,package_id,$3,importer_version,'queued',0,'queued',created_by
		from catalog_import_jobs where id=$2`, failedJobID, jobID, failedVersionID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `alter table catalog_import_job_artifacts add constraint test008_reject_activation
		check(status<>'active') not valid`); err != nil {
		t.Fatal(err)
	}
	failedMessage, _ := json.Marshal(modExportJobMessage{JobID: failedJobID})
	if err = worker.handle(ctx, failedMessage); err == nil {
		t.Fatal("injected final activation failure was swallowed")
	}
	t.Logf("injected finalization result: %v", err)
	var failedStatus string
	var abandoned, deleted, deletionJobs, staging int
	if err = pool.QueryRow(ctx, `select status from catalog_import_jobs where id=$1`, failedJobID).Scan(&failedStatus); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `select count(*) filter(where artifact.status='abandoned'),
		count(*) filter(where file.status='deleted'),count(deletion.id)
		from catalog_import_job_artifacts artifact join oss_files file on file.id=artifact.oss_file_id
		left join oss_object_deletion_outbox deletion on deletion.oss_file_id=file.id
		where artifact.job_id=$1`, failedJobID).Scan(&abandoned, &deleted, &deletionJobs); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `select count(*) from catalog_import_revisions where job_id=$1 and status='staging'`, failedJobID).Scan(&staging); err != nil {
		t.Fatal(err)
	}
	providerMu.Lock()
	gets, puts = providerGets, providerPuts
	providerMu.Unlock()
	if failedStatus != "failed" || abandoned != 3 || deleted != 3 || deletionJobs != 3 || staging != 0 || gets != 2 || puts != 6 {
		t.Fatalf("failed job/artifacts/files/deletions/staging/provider=%s/%d/%d/%d/%d/%dGET/%dPUT",
			failedStatus, abandoned, deleted, deletionJobs, staging, gets, puts)
	}
	// Public/CDN endpoint differs deliberately from the provider endpoint.
	// Cleanup must use the persisted write target, never send DELETE to CDN.
	deletionWorker := &OSSDeletionWorker{server: server, workerID: "test008-deletion"}
	deletionWorker.drain(ctx)
	var completedDeletions int
	if err = pool.QueryRow(ctx, `select count(*) from oss_object_deletion_outbox deletion
		join catalog_import_job_artifacts artifact on artifact.oss_file_id=deletion.oss_file_id
		where artifact.job_id=$1 and deletion.status='completed'`, failedJobID).Scan(&completedDeletions); err != nil {
		t.Fatal(err)
	}
	providerMu.Lock()
	deletes := providerDeletes
	providerMu.Unlock()
	if completedDeletions != 3 || deletes != 5 {
		t.Fatalf("provider compensation completed=%d DELETE=%d, want 3/5", completedDeletions, deletes)
	}
}
