package httpapi

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"mcmods-cn-backend/internal/database"
)

func TestSharedMinecraftTextureBlobOwnershipReferenceGCAndFreshKeyIntegration(t *testing.T) {
	pool := openEconomySecurityTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := database.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}

	var puts atomic.Int64
	provider := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPut {
			t.Fatalf("unexpected provider method %s", request.Method)
		}
		puts.Add(1)
		response.Header().Set("ETag", `"bug088"`)
		response.WriteHeader(http.StatusOK)
	}))
	defer provider.Close()
	storageConfig := ossConfigPayload{
		Enabled: true, Region: "cn-test", Endpoint: provider.URL, PublicEndpoint: "https://public.example.test",
		Bucket: "bug088-bucket", AccessKeyID: "bug088-key", AccessKeySecret: "bug088-secret", UseCName: true, Prefix: "mcmods",
	}
	storageClient := newOSSClient(storageConfig, storageConfig.Endpoint, true)
	texture := bug088SanitizedTexture(t)

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	suffix := time.Now().UnixNano()
	owners := make([]int64, 2)
	for index := range owners {
		if err = tx.QueryRow(ctx, `insert into users(username,email,password_hash,status)
			values($1,$2,'not-used','active') returning id`,
			fmt.Sprintf("bug088_%d_%d", suffix, index), fmt.Sprintf("bug088_%d_%d@example.test", suffix, index)).Scan(&owners[index]); err != nil {
			t.Fatal(err)
		}
	}

	blob, pendingUpload, err := persistSanitizedMinecraftTextureTx(ctx, tx, storageClient, storageConfig, texture)
	if err != nil || pendingUpload == nil {
		t.Fatalf("first shared blob = %+v pending=%+v err=%v", blob, pendingUpload, err)
	}
	var uploaderIsNull bool
	var source, storedEndpoint string
	var sourceBytes, storedBytes int64
	if err = tx.QueryRow(ctx, `select uploader_id is null,source,source_size_bytes,size_bytes,endpoint
		from oss_files where id=$1`, blob.OSSFileID).Scan(&uploaderIsNull, &source, &sourceBytes, &storedBytes, &storedEndpoint); err != nil {
		t.Fatal(err)
	}
	if !uploaderIsNull || source != "minecraft_texture_derived" || sourceBytes != 0 || storedBytes != texture.SizeBytes || storedEndpoint != provider.URL {
		t.Fatalf("shared file ownership = null:%t source:%q bytes:%d/%d endpoint:%q", uploaderIsNull, source, sourceBytes, storedBytes, storedEndpoint)
	}
	for _, ownerID := range owners {
		var activeSource, activeStored int64
		if err = tx.QueryRow(ctx, `select
			coalesce((select active_source_bytes from oss_user_quota_usage where user_id=$1),0),
			coalesce((select active_stored_bytes from oss_user_quota_usage where user_id=$1),0)`, ownerID).
			Scan(&activeSource, &activeStored); err != nil || activeSource != 0 || activeStored != 0 {
			t.Fatalf("derived blob charged owner %d = %d/%d err=%v", ownerID, activeSource, activeStored, err)
		}
	}

	reused, reusedUpload, err := persistSanitizedMinecraftTextureTx(ctx, tx, storageClient, storageConfig, texture)
	if err != nil || reusedUpload != nil || reused.OSSFileID != blob.OSSFileID || puts.Load() != 1 {
		t.Fatalf("same-hash reuse = %+v pending=%+v puts=%d err=%v", reused, reusedUpload, puts.Load(), err)
	}
	assetIDs := make([]int64, 2)
	for index, ownerID := range owners {
		if err = tx.QueryRow(ctx, `insert into skin_assets(owner_id,blob_hash,kind,model,display_name,visibility,review_status,status)
			values($1,$2,'skin','default',$3,'private','approved','active') returning id`,
			ownerID, blob.Hash, fmt.Sprintf("BUG-088 shared %d", index)).Scan(&assetIDs[index]); err != nil {
			t.Fatal(err)
		}
	}
	server := &Server{db: pool}
	if err = server.softDeleteSkinAssetTx(ctx, tx, assetIDs[0], blob.Hash, "bug088_first_owner_deleted"); err != nil {
		t.Fatal(err)
	}
	assertBUG088BlobLifecycle(t, ctx, tx, blob.Hash, blob.OSSFileID, 1, "active", 0)
	if err = server.softDeleteSkinAssetTx(ctx, tx, assetIDs[1], blob.Hash, "bug088_last_owner_deleted"); err != nil {
		t.Fatal(err)
	}
	assertBUG088BlobLifecycle(t, ctx, tx, blob.Hash, blob.OSSFileID, 0, "deleted", 1)

	rehydrated, rehydratedUpload, err := persistSanitizedMinecraftTextureTx(ctx, tx, storageClient, storageConfig, texture)
	if err != nil || rehydratedUpload == nil {
		t.Fatalf("rehydrated blob = %+v pending=%+v err=%v", rehydrated, rehydratedUpload, err)
	}
	if rehydrated.ObjectKey == blob.ObjectKey || rehydrated.OSSFileID == blob.OSSFileID || puts.Load() != 2 {
		t.Fatalf("rehydration reused deletion target: old=%+v new=%+v puts=%d", blob, rehydrated, puts.Load())
	}
	var replacementAssetID int64
	if err = tx.QueryRow(ctx, `insert into skin_assets(owner_id,blob_hash,kind,model,display_name,visibility,review_status,status)
		values($1,$2,'skin','default','BUG-088 rehydrated','private','approved','active') returning id`, owners[1], blob.Hash).
		Scan(&replacementAssetID); err != nil {
		t.Fatal(err)
	}
	assertBUG088BlobLifecycle(t, ctx, tx, blob.Hash, rehydrated.OSSFileID, 1, "active", 0)
	var oldDeletionJobs int
	if err = tx.QueryRow(ctx, `select count(*) from oss_object_deletion_outbox where oss_file_id=$1`, blob.OSSFileID).Scan(&oldDeletionJobs); err != nil || oldDeletionJobs != 1 {
		t.Fatalf("old lifecycle deletion jobs = %d err=%v", oldDeletionJobs, err)
	}

	if _, err = tx.Exec(ctx, `update skin_texture_blobs set active_reference_count=9 where hash=$1`, blob.Hash); err != nil {
		t.Fatal(err)
	}
	var rebuilt int64
	if err = tx.QueryRow(ctx, `select rebuild_skin_texture_blob_reference_counts()`).Scan(&rebuilt); err != nil || rebuilt != 1 {
		t.Fatalf("reference calibration changed %d rows err=%v", rebuilt, err)
	}
	assertBUG088BlobLifecycle(t, ctx, tx, blob.Hash, rehydrated.OSSFileID, 1, "active", 0)
}

func TestMinecraftTextureRollbackQueuesDurableCompensationIntegration(t *testing.T) {
	pool := openEconomySecurityTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := database.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	var puts atomic.Int64
	provider := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPut {
			t.Fatalf("unexpected provider method %s", request.Method)
		}
		puts.Add(1)
		response.Header().Set("ETag", `"bug088-rollback"`)
		response.WriteHeader(http.StatusOK)
	}))
	defer provider.Close()
	storageConfig := ossConfigPayload{
		Enabled: true, Region: "cn-test", Endpoint: provider.URL, PublicEndpoint: "https://public.example.test",
		Bucket: "bug088-rollback", AccessKeyID: "bug088-key", AccessKeySecret: "bug088-secret", UseCName: true, Prefix: "mcmods",
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, pendingUpload, err := persistSanitizedMinecraftTextureTx(ctx, tx,
		newOSSClient(storageConfig, storageConfig.Endpoint, true), storageConfig, bug088SanitizedTexture(t))
	if err != nil || pendingUpload == nil {
		tx.Rollback(ctx)
		t.Fatalf("rollback candidate pending=%+v err=%v", pendingUpload, err)
	}
	if err = tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(context.Background(), `delete from oss_object_deletion_outbox where object_key=$1`, pendingUpload.ObjectKey)

	server := &Server{db: pool}
	server.compensateUncommittedMinecraftTextureUpload(pendingUpload)
	var status, reason string
	var fileIsNull bool
	if err = pool.QueryRow(ctx, `select status,reason,oss_file_id is null from oss_object_deletion_outbox
		where object_key=$1`, pendingUpload.ObjectKey).Scan(&status, &reason, &fileIsNull); err != nil {
		t.Fatal(err)
	}
	if status != "pending" || reason != "minecraft-texture-transaction-aborted" || !fileIsNull || puts.Load() != 1 {
		t.Fatalf("rollback compensation = %q/%q fileNull=%t puts=%d", status, reason, fileIsNull, puts.Load())
	}
	var registered, blobbed bool
	if err = pool.QueryRow(ctx, `select
		exists(select 1 from oss_files where object_key=$1),
		exists(select 1 from skin_texture_blobs where object_key=$1)`, pendingUpload.ObjectKey).Scan(&registered, &blobbed); err != nil {
		t.Fatal(err)
	}
	if registered || blobbed {
		t.Fatalf("rolled-back texture persisted file/blob = %t/%t", registered, blobbed)
	}
}

func TestMinecraftTextureGCCandidatesUseReferenceProjectionIndexIntegration(t *testing.T) {
	pool := openEconomySecurityTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `create temporary table oss_files(id bigint primary key,status text not null);
		create temporary table skin_texture_blobs(
			hash text primary key,oss_file_id bigint not null,active_reference_count bigint not null,created_at timestamptz not null);
		create index idx_skin_texture_blobs_unreferenced
			on skin_texture_blobs(created_at,hash) where active_reference_count=0;
		insert into oss_files select value,'active' from generate_series(1,100000) value;
		insert into skin_texture_blobs
		select lpad(to_hex(value),64,'0'),value,case when value>99997 then 0 else 1 end,
			timestamptz '2026-01-01 00:00:00+00'+make_interval(secs=>value)
		from generate_series(1,100000) value;
		analyze oss_files; analyze skin_texture_blobs`); err != nil {
		t.Fatal(err)
	}
	rows, err := tx.Query(ctx, `explain select blob.hash
		from skin_texture_blobs blob
		join oss_files file on file.id=blob.oss_file_id and file.status='active'
		where blob.active_reference_count=0
		order by blob.created_at,blob.hash limit 1000`)
	if err != nil {
		t.Fatal(err)
	}
	var plan strings.Builder
	for rows.Next() {
		var line string
		if err = rows.Scan(&line); err != nil {
			break
		}
		plan.WriteString(line)
		plan.WriteByte('\n')
	}
	rows.Close()
	if err == nil {
		err = rows.Err()
	}
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(plan.String(), "idx_skin_texture_blobs_unreferenced") {
		t.Fatalf("100k GC candidate plan missed the partial reference index:\n%s", plan.String())
	}
}

func TestMinecraftTexturePersistenceAndLastDeleteUseOneLockOrderIntegration(t *testing.T) {
	pool := openEconomySecurityTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := database.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	setupTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var ownerID, fileID, assetID int64
	if err = setupTx.QueryRow(ctx, `select id from users where status='active' order by id limit 1`).Scan(&ownerID); err != nil {
		setupTx.Rollback(ctx)
		t.Fatal(err)
	}
	suffix := time.Now().UnixNano()
	hash := fmt.Sprintf("%064x", suffix)
	objectKey := fmt.Sprintf("bug088-lock/%d.png", suffix)
	if err = setupTx.QueryRow(ctx, `insert into oss_files(
		bucket,endpoint,region,object_key,category,source,original_name,source_original_name,
		content_type,size_bytes,source_size_bytes,sha256,status,scan_status)
		values('bug088','https://oss.example.test','cn-test',$1,$2,'minecraft_texture_derived','lock.png','lock.png',
		'image/png',128,0,$3,'active','trusted_generated') returning id`, objectKey, ossSharedSkinTextureCategory(), hash).Scan(&fileID); err != nil {
		setupTx.Rollback(ctx)
		t.Fatal(err)
	}
	if _, err = setupTx.Exec(ctx, `insert into skin_texture_blobs(hash,oss_file_id,object_key,width,height,size_bytes)
		values($1,$2,$3,64,64,128)`, hash, fileID, objectKey); err != nil {
		setupTx.Rollback(ctx)
		t.Fatal(err)
	}
	if err = setupTx.QueryRow(ctx, `insert into skin_assets(owner_id,blob_hash,kind,model,display_name,visibility,review_status,status)
		values($1,$2,'skin','default','BUG-088 lock order','private','approved','active') returning id`, ownerID, hash).Scan(&assetID); err != nil {
		setupTx.Rollback(ctx)
		t.Fatal(err)
	}
	if err = setupTx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = pool.Exec(cleanupCtx, `delete from skin_assets where id=$1;
			delete from oss_object_deletion_outbox where oss_file_id=$2;
			delete from skin_texture_blobs where hash=$3;
			delete from oss_files where id=$2`, assetID, fileID, hash)
	}()

	persistTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer persistTx.Rollback(ctx)
	if err = lockMinecraftTextureLifecycleStore(ctx, persistTx, hash); err != nil {
		t.Fatal(err)
	}
	deleteStarted := make(chan struct{})
	deleteResult := make(chan error, 1)
	go func() {
		deleteTx, deleteErr := pool.Begin(ctx)
		if deleteErr != nil {
			deleteResult <- deleteErr
			return
		}
		defer deleteTx.Rollback(ctx)
		close(deleteStarted)
		server := &Server{db: pool}
		if deleteErr = server.softDeleteSkinAssetTx(ctx, deleteTx, assetID, hash, "bug088_lock_order"); deleteErr == nil {
			deleteErr = deleteTx.Commit(ctx)
		}
		deleteResult <- deleteErr
	}()
	<-deleteStarted
	time.Sleep(100 * time.Millisecond)
	lockCtx, lockCancel := context.WithTimeout(ctx, 3*time.Second)
	defer lockCancel()
	var lockedFileID int64
	if err = persistTx.QueryRow(lockCtx, `select oss_file_id from skin_texture_blobs where hash=$1 for update`, hash).Scan(&lockedFileID); err != nil {
		t.Fatalf("persistence could not lock the blob while deletion waited behind its lifecycle lock: %v", err)
	}
	if lockedFileID != fileID {
		t.Fatalf("locked file = %d, want %d", lockedFileID, fileID)
	}
	if err = persistTx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err = <-deleteResult:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("last-reference deletion deadlocked with texture persistence")
	}
	var references int64
	var assetStatus, fileStatus string
	var jobs int
	if err = pool.QueryRow(ctx, `select asset.status,blob.active_reference_count,file.status,
		(select count(*) from oss_object_deletion_outbox where oss_file_id=file.id)
		from skin_assets asset join skin_texture_blobs blob on blob.hash=asset.blob_hash
		join oss_files file on file.id=blob.oss_file_id where asset.id=$1`, assetID).
		Scan(&assetStatus, &references, &fileStatus, &jobs); err != nil {
		t.Fatal(err)
	}
	if assetStatus != "deleted" || references != 0 || fileStatus != "deleted" || jobs != 1 {
		t.Fatalf("concurrent delete state = %q refs=%d file=%q jobs=%d", assetStatus, references, fileStatus, jobs)
	}
}

func bug088SanitizedTexture(t *testing.T) sanitizedMinecraftTexture {
	t.Helper()
	imageData := image.NewNRGBA(image.Rect(0, 0, 64, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			imageData.SetNRGBA(x, y, color.NRGBA{R: uint8(x * 3), G: uint8(y * 3), B: 88, A: 255})
		}
	}
	var raw bytes.Buffer
	if err := png.Encode(&raw, imageData); err != nil {
		t.Fatal(err)
	}
	texture, err := sanitizeMinecraftTexture(bytes.NewReader(raw.Bytes()), "skin", maxMinecraftTextureUploadBytes)
	if err != nil {
		t.Fatal(err)
	}
	return texture
}

func assertBUG088BlobLifecycle(t *testing.T, ctx context.Context, queryer minecraftTextureStore, hash string, fileID, wantReferences int64, wantFileStatus string, wantCurrentFileDeletionJobs int) {
	t.Helper()
	var activeReferences int64
	var currentFileID int64
	var fileStatus string
	var deletionJobs int
	if err := queryer.QueryRow(ctx, `select blob.active_reference_count,blob.oss_file_id,file.status,
		(select count(*) from oss_object_deletion_outbox deletion where deletion.oss_file_id=blob.oss_file_id)
		from skin_texture_blobs blob join oss_files file on file.id=blob.oss_file_id where blob.hash=$1`, hash).
		Scan(&activeReferences, &currentFileID, &fileStatus, &deletionJobs); err != nil {
		t.Fatal(err)
	}
	if activeReferences != wantReferences || currentFileID != fileID || fileStatus != wantFileStatus || deletionJobs != wantCurrentFileDeletionJobs {
		t.Fatalf("blob lifecycle = refs:%d file:%d status:%q jobs:%d, want %d/%d/%q/%d",
			activeReferences, currentFileID, fileStatus, deletionJobs, wantReferences, fileID, wantFileStatus, wantCurrentFileDeletionJobs)
	}
}
