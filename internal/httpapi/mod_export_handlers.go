package httpapi

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/png"
	"io"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	aliyunoss "github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/queue"
)

const (
	modExportImporterVersion  = "1.1.0"
	modExportTaskCode         = "mod_export_import"
	maxExportFileCount        = 200_000
	maxExportUncompressedSize = int64(8 << 30)
	maxExportSingleFileSize   = int64(512 << 20)
	maxExportJSONSize         = int64(128 << 20)
	maxExportWriteBatchRows   = 500
	maxExportWriteBatchBytes  = int64(32 << 20)
	maxExportPNGConcurrency   = 8
)

type modExportManifest struct {
	SchemaVersion    string `json:"schema_version"`
	ExporterVersion  string `json:"exporter_version"`
	Status           string `json:"status"`
	MinecraftVersion string `json:"minecraft_version"`
	Loader           string `json:"loader"`
	PackageFormat    string `json:"package_format"`
	Configuration    struct {
		Namespaces    []string `json:"namespaces"`
		AllNamespaces bool     `json:"all_namespaces"`
		Languages     []string `json:"languages"`
		ImageSizes    []int    `json:"image_sizes"`
		Profile       string   `json:"profile"`
		Automatic     bool     `json:"automatic"`
	} `json:"configuration"`
	Stages []struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	} `json:"stages"`
	Errors     []json.RawMessage `json:"errors"`
	ErrorCount int               `json:"error_count"`
}

type modExportJobMessage struct {
	JobID string `json:"jobId"`
}

type createModExportJobRequest struct {
	OSSFileID int64 `json:"ossFileId"`
}

type modExportJobResponse struct {
	ID             string         `json:"id"`
	ModSiteID      string         `json:"modSiteId"`
	PackageID      string         `json:"packageId"`
	Status         string         `json:"status"`
	Progress       int            `json:"progress"`
	CurrentStage   string         `json:"currentStage"`
	ErrorCode      string         `json:"errorCode"`
	ErrorDetail    map[string]any `json:"errorDetail"`
	Deduplicated   bool           `json:"deduplicated"`
	ReviewRequired bool           `json:"reviewRequired"`
	CreatedAt      time.Time      `json:"createdAt"`
	UpdatedAt      time.Time      `json:"updatedAt"`
}

type modExportWriteBatch struct {
	batch     *pgx.Batch
	rows      int
	byteCount int64
}

type modExportPNGMedia struct {
	RevisionID string
	AssetPath  string
	ObjectKey  string
	Digest     string
	ByteLength int64
	Width      int
	Height     int
	Original   string
	FileID     int64
}

type modExportPNGUploadPool struct {
	ctx       context.Context
	client    *aliyunoss.Client
	bucket    string
	semaphore chan struct{}
	waitGroup sync.WaitGroup
	mu        sync.Mutex
	firstErr  error
}

type ModExportWorker struct {
	server *Server
	queue  *queue.Client
}

func NewModExportWorker(cfg config.Config, db *pgxpool.Pool, queueClient *queue.Client) *ModExportWorker {
	return &ModExportWorker{server: &Server{cfg: cfg, db: db, queue: queueClient}, queue: queueClient}
}

func (worker *ModExportWorker) Start() error {
	if worker == nil || worker.server == nil {
		return queue.ErrUnavailable
	}
	if err := worker.server.recoverStaleModExportJobs(context.Background()); err != nil {
		return err
	}
	subscribeErr := queue.ErrUnavailable
	if worker.queue != nil {
		subscribeErr = worker.queue.SubscribeTask(modExportTaskCode, worker.handle)
	}
	rows, err := worker.server.db.Query(context.Background(), `select id from mod_export_jobs where status='queued' order by created_at`)
	if err != nil {
		if subscribeErr != nil {
			return subscribeErr
		}
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var jobID string
		if rows.Scan(&jobID) != nil {
			continue
		}
		message := modExportJobMessage{JobID: jobID}
		if subscribeErr == nil && worker.queue.PublishTask(context.Background(), modExportTaskCode, message) == nil {
			_, _ = worker.server.db.Exec(context.Background(), `update nats_outbox set published_at=now() where aggregate_type='mod_export_job' and aggregate_id=$1 and published_at is null`, jobID)
			continue
		}
		go func(id string) { _ = worker.server.importModExportJob(context.Background(), id) }(jobID)
	}
	return subscribeErr
}

func (worker *ModExportWorker) handle(ctx context.Context, raw []byte) error {
	var message modExportJobMessage
	if err := json.Unmarshal(raw, &message); err != nil || message.JobID == "" {
		return errors.New("invalid mod export import message")
	}
	return worker.server.importModExportJob(ctx, message.JobID)
}

func (s *Server) createModExportUpload(w http.ResponseWriter, r *http.Request) {
	identity, ok := s.requireModEditor(w, r)
	if !ok {
		return
	}
	s.createOSSDirectUploadWithScope(w, r, ossModExportScopePrefix+identity.UniqueID)
}

func (s *Server) completeModExportUpload(w http.ResponseWriter, r *http.Request) {
	identity, ok := s.requireModEditor(w, r)
	if !ok {
		return
	}
	s.completeOSSDirectUploadWithScope(w, r, ossModExportScopePrefix+identity.UniqueID)
}

func (s *Server) createModExportJob(w http.ResponseWriter, r *http.Request) {
	identity, ok := s.requireModEditor(w, r)
	if !ok {
		return
	}
	var request createModExportJobRequest
	if err := decodeJSON(r, &request); err != nil || request.OSSFileID <= 0 {
		writeError(w, http.StatusBadRequest, "导入文件记录不正确")
		return
	}
	claims := currentClaims(r)
	var archiveName, objectKey, archiveHash, source, category string
	var archiveSize int64
	err := s.db.QueryRow(
		r.Context(),
		`select original_name, object_key, sha256, size_bytes, source, category
		 from oss_files where id=$1 and uploader_id=$2 and status='active'`,
		request.OSSFileID, claims.Subject,
	).Scan(&archiveName, &objectKey, &archiveHash, &archiveSize, &source, &category)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusBadRequest, "导入文件不存在或不属于当前用户")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取导入文件失败")
		return
	}
	expectedPrefix := path.Join("project", identity.UniqueID, "import-staging")
	if source != "mcmods_exporter" || !strings.HasPrefix(category, expectedPrefix) || strings.ToLower(filepath.Ext(archiveName)) != ".zip" || archiveSize <= 0 {
		writeError(w, http.StatusBadRequest, "文件不是当前模组的 mcmods_exporter 导入包")
		return
	}

	packageID := newExportID()
	jobID := newExportID()
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "创建导入任务失败")
		return
	}
	defer tx.Rollback(r.Context())
	err = tx.QueryRow(
		r.Context(),
		`insert into mod_export_packages
		 (id,sha256,archive_file_id,archive_name,schema_version,exporter_version,minecraft_version,loader,manifest,namespaces,profile,uploaded_by)
		 values ($1,$2,$3,$4,'','','','unknown','{}'::jsonb,'{}'::text[],'all',$5)
		 on conflict (sha256) do update set archive_file_id=excluded.archive_file_id,archive_name=excluded.archive_name,uploaded_by=excluded.uploaded_by
		 returning id`,
		packageID, archiveHash, request.OSSFileID, archiveName, claims.Subject,
	).Scan(&packageID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "保存导出包失败")
		return
	}
	deduplicated := false
	shouldPublish := false
	cleanupDuplicate := false
	var existingStatus string
	var existingStalled bool
	err = tx.QueryRow(r.Context(),
		`select id,status,status in ('validating','importing') and coalesce(heartbeat_at,updated_at) < now() - $4::interval
		 from mod_export_jobs where mod_id=$1 and package_id=$2 and importer_version=$3 for update`,
		identity.ID, packageID, modExportImporterVersion, pgInterval(modExportStaleAfter),
	).Scan(&jobID, &existingStatus, &existingStalled)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		jobID = newExportID()
		_, err = tx.Exec(r.Context(),
			`insert into mod_export_jobs (id,mod_id,package_id,importer_version,created_by) values ($1,$2,$3,$4,$5)`,
			jobID, identity.ID, packageID, modExportImporterVersion, claims.Subject,
		)
		shouldPublish = err == nil
	case err == nil && (shouldRetryModExportStatus(existingStatus) || existingStalled):
		_, err = tx.Exec(r.Context(),
			`update mod_export_jobs set status='queued',progress=0,current_stage='recovery',error_code='',error_detail='{}'::jsonb,created_by=$2,started_at=null,finished_at=null,heartbeat_at=null,run_token='',updated_at=now() where id=$1`,
			jobID, claims.Subject,
		)
		shouldPublish = err == nil
	case err == nil:
		deduplicated = true
		cleanupDuplicate = existingStatus == "ready" || existingStatus == "partial"
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "保存导入任务失败")
		return
	}
	if shouldPublish {
		eventID := newExportID()
		payload, _ := json.Marshal(modExportJobMessage{JobID: jobID})
		_, err = tx.Exec(r.Context(), `insert into nats_outbox(event_id,subject,aggregate_type,aggregate_id,payload) values($1,$2,'mod_export_job',$3,$4::jsonb)`, eventID, modExportTaskCode, jobID, string(payload))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "保存导入队列事件失败")
			return
		}
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "提交导入任务失败")
		return
	}
	if shouldPublish {
		s.dispatchModExportJob(r.Context(), jobID)
	} else if cleanupDuplicate {
		// The already-imported package is immutable; the repeated staging object is no longer needed.
		if client, cfg, clientErr := s.ossClient(r.Context()); clientErr == nil {
			_, _ = client.DeleteObject(r.Context(), &aliyunoss.DeleteObjectRequest{Bucket: aliyunoss.Ptr(cfg.Bucket), Key: aliyunoss.Ptr(objectKey)})
		}
		_, _ = s.db.Exec(r.Context(), `update oss_files set status='deleted',updated_at=now() where id=$1`, request.OSSFileID)
		_, _ = s.db.Exec(r.Context(), `update mod_export_packages set archive_file_id=null where id=$1 and archive_file_id=$2`, packageID, request.OSSFileID)
	}
	response, _ := s.modExportJobByID(r.Context(), jobID, identity.ID)
	response.Deduplicated = deduplicated
	writeJSON(w, http.StatusAccepted, response)
}

func shouldRetryModExportStatus(status string) bool {
	return status == "failed" || status == "cancelled"
}

func (s *Server) getModExportJob(w http.ResponseWriter, r *http.Request) {
	identity, ok := s.requireModEditor(w, r)
	if !ok {
		return
	}
	jobID := r.PathValue("jobId")
	if stalled, markErr := s.markStalledModExportJob(r.Context(), jobID, identity.ID); markErr != nil {
		writeError(w, http.StatusInternalServerError, "Failed to inspect the import job")
		return
	} else if stalled {
		s.notifyModExportResult(r.Context(), jobID, "failed", errors.New("import worker stopped responding"))
	}
	job, err := s.modExportJobByID(r.Context(), jobID, identity.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "导入任务不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取导入任务失败")
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func (s *Server) retryModExportJob(w http.ResponseWriter, r *http.Request) {
	identity, ok := s.requireModEditor(w, r)
	if !ok {
		return
	}
	jobID := r.PathValue("jobId")
	retried, err := s.resetModExportJobForRetry(r.Context(), jobID, identity.ID, currentClaims(r).Subject)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to retry the import job")
		return
	}
	if !retried {
		writeError(w, http.StatusConflict, "The import job is still running or cannot be retried")
		return
	}
	s.dispatchModExportJob(r.Context(), jobID)
	job, err := s.modExportJobByID(r.Context(), jobID, identity.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to read the retried import job")
		return
	}
	writeJSON(w, http.StatusAccepted, job)
}

func (s *Server) cancelModExportJob(w http.ResponseWriter, r *http.Request) {
	identity, ok := s.requireModEditor(w, r)
	if !ok {
		return
	}
	tag, err := s.db.Exec(r.Context(), `update mod_export_jobs set status='cancelled',finished_at=now(),updated_at=now() where id=$1 and mod_id=$2 and status='queued'`, r.PathValue("jobId"), identity.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "取消导入任务失败")
		return
	}
	if tag.RowsAffected() == 0 {
		writeError(w, http.StatusConflict, "任务已经开始或不存在")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "cancelled"})
}

func (s *Server) modExportJobByID(ctx context.Context, jobID string, modID int64) (modExportJobResponse, error) {
	var result modExportJobResponse
	var detail []byte
	err := s.db.QueryRow(
		ctx,
		`select j.id,m.slug,j.package_id,j.status,j.progress,j.current_stage,j.error_code,j.error_detail,j.created_at,j.updated_at,
		 exists(select 1 from mod_export_revisions r where r.package_id=j.package_id and r.mod_id=j.mod_id and r.status in ('ready','partial') and not r.is_active)
		 from mod_export_jobs j join mods m on m.id=j.mod_id where j.id=$1 and j.mod_id=$2`,
		jobID, modID,
	).Scan(&result.ID, &result.ModSiteID, &result.PackageID, &result.Status, &result.Progress, &result.CurrentStage, &result.ErrorCode, &detail, &result.CreatedAt, &result.UpdatedAt, &result.ReviewRequired)
	if len(detail) > 0 {
		_ = json.Unmarshal(detail, &result.ErrorDetail)
	}
	if result.ErrorDetail == nil {
		result.ErrorDetail = map[string]any{}
	}
	return result, err
}

func (s *Server) requireModEditor(w http.ResponseWriter, r *http.Request) (modIdentityRecord, bool) {
	identity, err := s.modIdentity(r.Context(), r.PathValue("siteId"))
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "模组不存在")
		return identity, false
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取模组失败")
		return identity, false
	}
	if !canEditMod(currentClaims(r), identity) {
		writeError(w, http.StatusForbidden, "没有导入该模组资料的权限")
		return identity, false
	}
	return identity, true
}

func (s *Server) importModExportJob(ctx context.Context, jobID string) (resultErr error) {
	importStarted := time.Now()
	runToken, err := s.claimModExportJob(ctx, jobID)
	if errors.Is(err, errModExportLeaseLost) {
		return nil
	}
	if err != nil {
		return err
	}
	ctx, stopHeartbeat := s.startModExportHeartbeat(ctx, jobID, runToken)
	var packageID string
	var modID int64
	defer func() {
		stopHeartbeat()
		if resultErr == nil || errors.Is(resultErr, errModExportLeaseLost) || errors.Is(context.Cause(ctx), errModExportLeaseLost) {
			return
		}
		cleanupContext, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		_ = s.cleanupModExportStaging(cleanupContext, packageID, modID, runToken)
		cancel()
		s.failModExportJob(jobID, runToken, "import_failed", resultErr)
	}()

	var expectedHash, objectKey, archiveName, uniqueID string
	var sourceFileID, createdBy int64
	err = s.db.QueryRow(
		ctx,
		`select j.package_id,j.mod_id,coalesce(j.created_by,0),p.sha256,coalesce(f.id,0),coalesce(f.object_key,''),p.archive_name,m.project_code
		 from mod_export_jobs j join mod_export_packages p on p.id=j.package_id join mods m on m.id=j.mod_id
		 left join oss_files f on f.id=p.archive_file_id where j.id=$1`,
		jobID,
	).Scan(&packageID, &modID, &createdBy, &expectedHash, &sourceFileID, &objectKey, &archiveName, &uniqueID)
	if err != nil {
		return err
	}
	if objectKey == "" {
		return errors.New("export archive is unavailable")
	}
	client, cfg, err := s.ossClient(ctx)
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp("", "mcmods-export-*.zip")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	getResult, err := client.GetObject(ctx, &aliyunoss.GetObjectRequest{Bucket: aliyunoss.Ptr(cfg.Bucket), Key: aliyunoss.Ptr(objectKey)})
	if err != nil {
		temporary.Close()
		return fmt.Errorf("download export archive: %w", err)
	}
	hasher := sha256.New()
	_, copyErr := io.Copy(io.MultiWriter(temporary, hasher), io.LimitReader(getResult.Body, maxOSSUploadBytes+1))
	getResult.Body.Close()
	closeErr := temporary.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if hex.EncodeToString(hasher.Sum(nil)) != expectedHash {
		return errors.New("export archive SHA-256 mismatch")
	}
	info, err := os.Stat(temporaryPath)
	if err != nil || info.Size() > maxOSSUploadBytes {
		return errors.New("export archive exceeds size limit")
	}
	reader, err := zip.OpenReader(temporaryPath)
	if err != nil {
		return fmt.Errorf("open export archive: %w", err)
	}
	defer reader.Close()
	files, err := validateExportZIP(reader.File)
	if err != nil {
		return err
	}
	manifestFile := files["manifest.json"]
	if manifestFile == nil {
		return errors.New("manifest.json is missing")
	}
	manifestRaw, err := readExportZIPFile(manifestFile, 4<<20)
	if err != nil {
		return err
	}
	var manifest modExportManifest
	if err = json.Unmarshal(manifestRaw, &manifest); err != nil {
		return fmt.Errorf("decode manifest: %w", err)
	}
	if manifest.SchemaVersion != "mcmods-export/v1" {
		return errors.New("unsupported_schema")
	}
	if manifest.Status != "complete" {
		return fmt.Errorf("manifest status is %s", manifest.Status)
	}
	if manifest.MinecraftVersion == "" || manifest.ExporterVersion == "" || len(manifest.Configuration.Namespaces) == 0 {
		return errors.New("manifest required fields are missing")
	}
	partial := manifest.ErrorCount > 0
	translationValuesSkipped := 0
	for _, stage := range manifest.Stages {
		if stage.Status == "failed" {
			partial = true
		}
	}
	manifestJSON, _ := json.Marshal(manifest)
	namespaces := normalizeExportNamespaces(manifest.Configuration.Namespaces)
	if len(namespaces) == 0 {
		return errors.New("manifest contains no valid namespace")
	}
	_, err = s.db.Exec(
		ctx,
		`update mod_export_packages set schema_version=$2,exporter_version=$3,minecraft_version=$4,loader=$5,manifest=$6::jsonb,namespaces=$7,profile=$8 where id=$1`,
		packageID, manifest.SchemaVersion, manifest.ExporterVersion, manifest.MinecraftVersion, normalizeExportLoader(manifest.Loader), string(manifestJSON), namespaces, manifest.Configuration.Profile,
	)
	if err != nil {
		return err
	}
	if err = s.updateModExportJob(ctx, jobID, runToken, "importing", 20, "database"); err != nil {
		return err
	}
	if _, err = s.db.Exec(ctx, `delete from mod_export_revisions where package_id=$1 and mod_id=$2 and status='staging'`, packageID, modID); err != nil {
		return err
	}
	revisions := make(map[string]string, len(namespaces))
	loader := normalizeExportLoader(manifest.Loader)
	err = s.runModExportTransaction(ctx, func(tx pgx.Tx) error {
		for _, namespace := range namespaces {
			lockKey := fmt.Sprintf("mod-export:%d:%s:%s:%s", modID, manifest.MinecraftVersion, loader, namespace)
			if _, lockErr := tx.Exec(ctx, `select pg_advisory_xact_lock(hashtextextended($1,0))`, lockKey); lockErr != nil {
				return lockErr
			}
			revisionID := newExportID()
			var revisionNo int64
			if queryErr := tx.QueryRow(
				ctx,
				`select coalesce(max(revision_no),0)+1 from mod_export_revisions where mod_id=$1 and minecraft_version=$2 and loader=$3 and source_namespace=$4`,
				modID, manifest.MinecraftVersion, loader, namespace,
			).Scan(&revisionNo); queryErr != nil {
				return queryErr
			}
			_, insertErr := tx.Exec(
				ctx,
				`insert into mod_export_revisions(id,mod_id,package_id,revision_no,minecraft_version,loader,exporter_version,source_namespace,source_metadata,import_run_token)
				 values($1,$2,$3,$4,$5,$6,$7,$8,$9::jsonb,$10)`,
				revisionID, modID, packageID, revisionNo, manifest.MinecraftVersion, loader, manifest.ExporterVersion, namespace, string(manifestJSON), runToken,
			)
			if insertErr != nil {
				return insertErr
			}
			revisions[namespace] = revisionID
		}
		return nil
	})
	if err != nil {
		return err
	}

	fileNames := make([]string, 0, len(files))
	for name := range files {
		if name != "manifest.json" {
			fileNames = append(fileNames, name)
		}
	}
	sort.Strings(fileNames)
	writeBatch := newModExportWriteBatch()
	uploadContext, cancelUploads := context.WithCancel(ctx)
	pngPool := newModExportPNGUploadPool(uploadContext, client, cfg.Bucket)
	defer func() {
		cancelUploads()
		_ = pngPool.wait()
	}()
	pngMedia := make([]modExportPNGMedia, 0)
	scheduledPNGObjects := make(map[string]struct{})
	textAssetCount := 0
	binaryAssetCount := 0
	flushWriteBatch := func() error {
		return s.runModExportTransaction(ctx, func(tx pgx.Tx) error {
			return writeBatch.flush(ctx, tx)
		})
	}
	for index, name := range fileNames {
		if err = context.Cause(ctx); err != nil {
			return err
		}
		file := files[name]
		if file.FileInfo().IsDir() {
			continue
		}
		data, readErr := readExportZIPFile(file, maxExportSingleFileSize)
		if readErr != nil {
			return fmt.Errorf("read %s: %w", name, readErr)
		}
		if strings.HasPrefix(name, "translations/") && strings.HasSuffix(name, ".json") && path.Base(name) != "languages.json" {
			var skipped int
			err = s.runModExportTransaction(ctx, func(tx pgx.Tx) error {
				var importErr error
				skipped, importErr = importExportTranslations(ctx, tx, revisions, jobID, name, data)
				return importErr
			})
			if err != nil {
				return err
			}
			if skipped > 0 {
				partial = true
				translationValuesSkipped += skipped
			}
			continue
		}
		if strings.HasPrefix(name, "registries/") && strings.HasSuffix(name, ".json") {
			if err = s.runModExportTransaction(ctx, func(tx pgx.Tx) error {
				return importExportRegistry(ctx, tx, revisions, name, data)
			}); err != nil {
				return err
			}
		}
		revisionID := exportRevisionForPath(revisions, name)
		if revisionID == "" {
			continue
		}
		extension := strings.ToLower(path.Ext(name))
		switch extension {
		case ".png":
			var media modExportPNGMedia
			if media, err = inspectExportPNG(cfg, revisionID, uniqueID, name, data); err != nil {
				return err
			}
			pngMedia = append(pngMedia, media)
			if _, scheduled := scheduledPNGObjects[media.ObjectKey]; !scheduled {
				scheduledPNGObjects[media.ObjectKey] = struct{}{}
				pngPool.submit(media.ObjectKey, name, media.Digest, data)
			}
		case ".nbt", ".schem", ".schematic", ".litematic":
			if err = queueExportBinary(writeBatch, revisionID, name, data); err != nil {
				return err
			}
			binaryAssetCount++
		case ".json", ".obj", ".mtl", ".snbt", ".mcmeta":
			if int64(len(data)) > maxExportJSONSize && extension == ".json" {
				return fmt.Errorf("JSON asset too large: %s", name)
			}
			if err = queueExportTextAsset(writeBatch, revisionID, name, data); err != nil {
				return err
			}
			textAssetCount++
		}
		if writeBatch.shouldFlush() {
			if err = flushWriteBatch(); err != nil {
				return err
			}
		}
		if index%200 == 0 {
			progress := 20 + int(float64(index+1)/float64(max(1, len(fileNames)))*65)
			if err = s.updateModExportJob(ctx, jobID, runToken, "importing", progress, "assets"); err != nil {
				return err
			}
		}
	}
	if err = s.updateModExportJob(ctx, jobID, runToken, "importing", 88, "database_flush"); err != nil {
		return err
	}
	if err = flushWriteBatch(); err != nil {
		return err
	}
	if err = pngPool.wait(); err != nil {
		return err
	}
	for start := 0; start < len(pngMedia); start += maxExportWriteBatchRows {
		end := min(start+maxExportWriteBatchRows, len(pngMedia))
		if err = s.runModExportTransaction(ctx, func(tx pgx.Tx) error {
			return persistExportPNGMedia(ctx, tx, cfg, uniqueID, createdBy, pngMedia[start:end])
		}); err != nil {
			return err
		}
	}
	if err = s.updateModExportJob(ctx, jobID, runToken, "importing", 96, "finalizing"); err != nil {
		return err
	}

	revisionStatus := "ready"
	jobStatus := "ready"
	if partial {
		revisionStatus = "partial"
		jobStatus = "partial"
	}
	canActivate := createdBy > 0 && s.userHasPermission(ctx, createdBy, "project.no-review."+uniqueID)
	jobDetail, _ := json.Marshal(map[string]int{"translationValuesSkipped": translationValuesSkipped})
	err = s.runModExportTransaction(ctx, func(tx pgx.Tx) error {
		for _, revisionID := range revisions {
			if canActivate {
				var minecraftVersion, revisionLoader, namespace string
				if queryErr := tx.QueryRow(ctx, `select minecraft_version,loader,source_namespace from mod_export_revisions where id=$1 and import_run_token=$2`, revisionID, runToken).Scan(&minecraftVersion, &revisionLoader, &namespace); queryErr != nil {
					return queryErr
				}
				if _, updateErr := tx.Exec(ctx, `update mod_export_revisions set is_active=false,status='superseded' where mod_id=$1 and minecraft_version=$2 and loader=$3 and source_namespace=$4 and is_active`, modID, minecraftVersion, revisionLoader, namespace); updateErr != nil {
					return updateErr
				}
			}
			if _, updateErr := tx.Exec(ctx, `update mod_export_revisions set status=$2,is_active=$3,activated_at=case when $3 then now() else null end where id=$1 and import_run_token=$4`, revisionID, revisionStatus, canActivate, runToken); updateErr != nil {
				return updateErr
			}
		}
		if _, logErr := tx.Exec(ctx, `insert into mod_export_job_logs(job_id,level,stage,message) values($1,'info','performance',$2)`, jobID, fmt.Sprintf("files=%d text_assets=%d binary_assets=%d png_assets=%d duration=%s", len(fileNames), textAssetCount, binaryAssetCount, len(pngMedia), time.Since(importStarted).Round(time.Millisecond))); logErr != nil {
			return logErr
		}
		tag, updateErr := tx.Exec(ctx, `update mod_export_jobs set status=$3,progress=100,current_stage=case when $4 then 'complete' else 'review' end,error_detail=$5::jsonb,finished_at=now(),heartbeat_at=now(),updated_at=now(),run_token='' where id=$1 and run_token=$2`, jobID, runToken, jobStatus, canActivate, string(jobDetail))
		if updateErr != nil {
			return updateErr
		}
		if tag.RowsAffected() == 0 {
			return errModExportLeaseLost
		}
		_, updateErr = tx.Exec(ctx, `update mod_export_packages set imported_at=now() where id=$1`, packageID)
		return updateErr
	})
	if err != nil {
		return err
	}
	_, _ = client.DeleteObject(context.Background(), &aliyunoss.DeleteObjectRequest{Bucket: aliyunoss.Ptr(cfg.Bucket), Key: aliyunoss.Ptr(objectKey)})
	_, _ = s.db.Exec(context.Background(), `update oss_files set status='deleted',updated_at=now() where id=$1`, sourceFileID)
	s.notifyModExportResult(context.Background(), jobID, jobStatus, nil)
	return nil
}

func validateExportZIP(files []*zip.File) (map[string]*zip.File, error) {
	if len(files) == 0 || len(files) > maxExportFileCount {
		return nil, errors.New("export ZIP file count is invalid")
	}
	result := make(map[string]*zip.File, len(files))
	seenFolded := make(map[string]struct{}, len(files))
	var total int64
	for _, file := range files {
		name, err := normalizeExportPath(file.Name)
		if err != nil {
			return nil, err
		}
		folded := strings.ToLower(name)
		if _, exists := seenFolded[folded]; exists {
			return nil, fmt.Errorf("duplicate ZIP path: %s", name)
		}
		seenFolded[folded] = struct{}{}
		if file.UncompressedSize64 > uint64(maxExportSingleFileSize) {
			return nil, fmt.Errorf("ZIP entry too large: %s", name)
		}
		total += int64(file.UncompressedSize64)
		if total > maxExportUncompressedSize {
			return nil, errors.New("export ZIP uncompressed size exceeds limit")
		}
		if file.CompressedSize64 > 0 && file.UncompressedSize64/file.CompressedSize64 > 1000 {
			return nil, fmt.Errorf("ZIP compression ratio is unsafe: %s", name)
		}
		result[name] = file
	}
	return result, nil
}

func normalizeExportPath(value string) (string, error) {
	if strings.ContainsRune(value, 0) || strings.Contains(value, "\\") || strings.HasPrefix(value, "/") || filepath.IsAbs(value) || strings.Contains(value, ":") {
		return "", fmt.Errorf("unsafe ZIP path: %q", value)
	}
	cleaned := path.Clean(strings.TrimPrefix(value, "./"))
	if cleaned == "." || cleaned == "" || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", fmt.Errorf("unsafe ZIP path: %q", value)
	}
	return cleaned, nil
}

func readExportZIPFile(file *zip.File, limit int64) ([]byte, error) {
	if int64(file.UncompressedSize64) > limit {
		return nil, errors.New("ZIP entry exceeds read limit")
	}
	reader, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	data, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errors.New("ZIP entry exceeds read limit")
	}
	return data, nil
}

func importExportTranslations(ctx context.Context, tx pgx.Tx, revisions map[string]string, jobID, name string, raw []byte) (int, error) {
	locale := strings.ToLower(strings.TrimSuffix(path.Base(name), path.Ext(name)))
	values, skippedKeys, total, err := decodeExportTranslationValues(raw)
	if err != nil {
		return 0, fmt.Errorf("decode translations %s: %w", name, err)
	}
	skipped := total - len(values)
	if skipped > 0 {
		message := fmt.Sprintf("%s skipped %d non-string translation values", name, skipped)
		if len(skippedKeys) > 0 {
			message += ": " + strings.Join(skippedKeys, ", ")
		}
		if _, err := tx.Exec(ctx, `insert into mod_export_job_logs(job_id,level,stage,message) values($1,'warning','translations',$2)`, jobID, message); err != nil {
			return 0, err
		}
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, revisionID := range revisions {
		_, err := tx.Exec(ctx, `insert into mod_export_locales(revision_id,locale,translation_count) values($1,$2,$3) on conflict(revision_id,locale) do update set translation_count=excluded.translation_count`, revisionID, locale, len(values))
		if err != nil {
			return 0, err
		}
		if len(keys) == 0 {
			continue
		}
		copied, err := tx.CopyFrom(ctx, pgx.Identifier{"mod_export_translations"}, []string{"revision_id", "locale", "translation_key", "value"}, pgx.CopyFromSlice(len(keys), func(index int) ([]any, error) {
			key := keys[index]
			return []any{revisionID, locale, key, values[key]}, nil
		}))
		if err != nil {
			return 0, err
		}
		if copied != int64(len(keys)) {
			return 0, fmt.Errorf("copy translations %s: copied %d of %d rows", name, copied, len(keys))
		}
	}
	return skipped, nil
}

func decodeExportTranslationValues(raw []byte) (map[string]string, []string, int, error) {
	var rawValues map[string]json.RawMessage
	if err := json.Unmarshal(raw, &rawValues); err != nil {
		return nil, nil, 0, err
	}
	// Exporter v1 wraps the effective language map with locale/namespace metadata.
	// Older packages may contain the effective map directly, so both forms remain valid.
	if wrapped, hasWrapped := rawValues["translations"]; hasWrapped {
		if _, hasCount := rawValues["translation_count"]; hasCount {
			var translations map[string]json.RawMessage
			if err := json.Unmarshal(wrapped, &translations); err != nil {
				return map[string]string{}, []string{"translations"}, 1, nil
			}
			rawValues = translations
		}
	}
	values := make(map[string]string, len(rawValues))
	skippedKeys := make([]string, 0, 8)
	for key, rawValue := range rawValues {
		var value string
		if bytes.Equal(bytes.TrimSpace(rawValue), []byte("null")) || json.Unmarshal(rawValue, &value) != nil {
			if len(skippedKeys) < 8 {
				skippedKeys = append(skippedKeys, key)
			}
			continue
		}
		values[key] = value
	}
	sort.Strings(skippedKeys)
	return values, skippedKeys, len(rawValues), nil
}

func importExportRegistry(ctx context.Context, tx pgx.Tx, revisions map[string]string, name string, raw []byte) error {
	var document struct {
		Registry string            `json:"registry"`
		Entries  []json.RawMessage `json:"entries"`
	}
	if err := json.Unmarshal(raw, &document); err != nil {
		return fmt.Errorf("decode registry %s: %w", name, err)
	}
	if document.Registry == "" {
		document.Registry = strings.TrimSuffix(path.Base(name), path.Ext(name))
	}
	rowsByKey := make(map[string][]any, len(document.Entries))
	for _, entryRaw := range document.Entries {
		var entry map[string]any
		if err := json.Unmarshal(entryRaw, &entry); err != nil {
			return err
		}
		objectID, _ := entry["id"].(string)
		parts := strings.SplitN(objectID, ":", 2)
		if len(parts) != 2 {
			continue
		}
		revisionID := revisions[strings.ToLower(parts[0])]
		if revisionID == "" {
			continue
		}
		translationKey, _ := entry["translation_key"].(string)
		names, _ := json.Marshal(entry["names"])
		if string(names) == "null" || len(names) == 0 {
			names = []byte("{}")
		}
		rowsByKey[revisionID+"\x00"+objectID] = []any{revisionID, document.Registry, objectID, strings.ToLower(parts[0]), parts[1], translationKey, string(names), string(entryRaw)}
	}
	rowKeys := make([]string, 0, len(rowsByKey))
	for key := range rowsByKey {
		rowKeys = append(rowKeys, key)
	}
	sort.Strings(rowKeys)
	if len(rowKeys) == 0 {
		return nil
	}
	copied, err := tx.CopyFrom(ctx, pgx.Identifier{"mod_export_registry_entries"}, []string{"revision_id", "registry", "object_id", "namespace", "object_path", "translation_key", "names", "data"}, pgx.CopyFromSlice(len(rowKeys), func(index int) ([]any, error) {
		return rowsByKey[rowKeys[index]], nil
	}))
	if err != nil {
		return err
	}
	if copied != int64(len(rowKeys)) {
		return fmt.Errorf("copy registry %s: copied %d of %d rows", name, copied, len(rowKeys))
	}
	return nil
}

func newModExportWriteBatch() *modExportWriteBatch {
	return &modExportWriteBatch{batch: &pgx.Batch{}}
}

func (batch *modExportWriteBatch) queue(query string, byteCount int64, arguments ...any) {
	batch.batch.Queue(query, arguments...)
	batch.rows++
	batch.byteCount += byteCount
}

func (batch *modExportWriteBatch) shouldFlush() bool {
	return batch.rows >= maxExportWriteBatchRows || batch.byteCount >= maxExportWriteBatchBytes
}

func (batch *modExportWriteBatch) flush(ctx context.Context, tx pgx.Tx) error {
	if batch.rows == 0 {
		return nil
	}
	results := tx.SendBatch(ctx, batch.batch)
	var firstErr error
	for index := 0; index < batch.rows; index++ {
		if _, err := results.Exec(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if err := results.Close(); err != nil && firstErr == nil {
		firstErr = err
	}
	batch.batch = &pgx.Batch{}
	batch.rows = 0
	batch.byteCount = 0
	return firstErr
}

func queueExportTextAsset(batch *modExportWriteBatch, revisionID, name string, data []byte) error {
	digest := sha256Hex(data)
	extension := strings.ToLower(path.Ext(name))
	contentType := mime.TypeByExtension(extension)
	if contentType == "" {
		contentType = "text/plain; charset=utf-8"
	}
	assetKind := strings.TrimPrefix(extension, ".")
	if extension == ".json" || extension == ".mcmeta" {
		var value any
		if err := json.Unmarshal(data, &value); err != nil {
			return fmt.Errorf("decode JSON asset %s: %w", name, err)
		}
		canonical, _ := json.Marshal(value)
		batch.queue(`insert into mod_export_text_assets(revision_id,asset_path,asset_kind,content_type,sha256,byte_length,json_content) values($1,$2,$3,'application/json',$4,$5,$6::jsonb) on conflict(revision_id,asset_path) do nothing`, int64(len(canonical)), revisionID, name, assetKind, digest, len(data), string(canonical))
		return nil
	}
	if !utf8Text(data) {
		return fmt.Errorf("text asset is not valid UTF-8: %s", name)
	}
	batch.queue(`insert into mod_export_text_assets(revision_id,asset_path,asset_kind,content_type,sha256,byte_length,text_content) values($1,$2,$3,$4,$5,$6,$7) on conflict(revision_id,asset_path) do nothing`, int64(len(data)), revisionID, name, assetKind, contentType, digest, len(data), string(data))
	return nil
}

func queueExportBinary(batch *modExportWriteBatch, revisionID, name string, data []byte) error {
	assetID := newExportID()
	extension := strings.TrimPrefix(strings.ToLower(path.Ext(name)), ".")
	batch.queue(`insert into mod_export_binary_assets(id,revision_id,asset_path,asset_kind,sha256,byte_length,data) values($1,$2,$3,$4,$5,$6,$7)`, int64(len(data)), assetID, revisionID, name, extension, sha256Hex(data), len(data), data)
	if strings.Contains(strings.ToLower(name), "/structures/") || extension == "schem" || extension == "schematic" || extension == "litematic" {
		structureID := strings.TrimSuffix(strings.TrimPrefix(name, "data/"), path.Ext(name))
		batch.queue(`insert into mod_export_structures(id,revision_id,structure_id,asset_path,source_format,template_blob_id) values($1,$2,$3,$4,$5,$6) on conflict(revision_id,structure_id) do nothing`, 0, newExportID(), revisionID, structureID, name, extension, assetID)
	}
	return nil
}

func inspectExportPNG(cfg ossConfigPayload, revisionID, uniqueID, name string, data []byte) (modExportPNGMedia, error) {
	imageConfig, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || imageConfig.Width <= 0 || imageConfig.Height <= 0 || int64(imageConfig.Width)*int64(imageConfig.Height) > 100_000_000 {
		return modExportPNGMedia{}, fmt.Errorf("invalid or oversized PNG: %s", name)
	}
	digest := sha256Hex(data)
	return modExportPNGMedia{RevisionID: revisionID, AssetPath: name, ObjectKey: path.Join(cfg.Prefix, "project", uniqueID, "export-media", digest[:2], digest+".png"), Digest: digest, ByteLength: int64(len(data)), Width: imageConfig.Width, Height: imageConfig.Height, Original: path.Base(name)}, nil
}

func newModExportPNGUploadPool(ctx context.Context, client *aliyunoss.Client, bucket string) *modExportPNGUploadPool {
	return &modExportPNGUploadPool{ctx: ctx, client: client, bucket: bucket, semaphore: make(chan struct{}, maxExportPNGConcurrency)}
}

func (pool *modExportPNGUploadPool) submit(objectKey, name, digest string, data []byte) {
	select {
	case pool.semaphore <- struct{}{}:
	case <-pool.ctx.Done():
		pool.setError(pool.ctx.Err())
		return
	}
	pool.waitGroup.Add(1)
	go func() {
		defer pool.waitGroup.Done()
		defer func() { <-pool.semaphore }()
		_, err := pool.client.PutObject(pool.ctx, &aliyunoss.PutObjectRequest{Bucket: aliyunoss.Ptr(pool.bucket), Key: aliyunoss.Ptr(objectKey), ContentType: aliyunoss.Ptr("image/png"), ContentLength: aliyunoss.Ptr(int64(len(data))), Body: bytes.NewReader(data), Metadata: map[string]string{"sha256": digest}})
		if err != nil {
			pool.setError(fmt.Errorf("upload PNG %s: %w", name, err))
		}
	}()
}

func (pool *modExportPNGUploadPool) setError(err error) {
	pool.mu.Lock()
	defer pool.mu.Unlock()
	if pool.firstErr == nil {
		pool.firstErr = err
	}
}

func (pool *modExportPNGUploadPool) wait() error {
	pool.waitGroup.Wait()
	pool.mu.Lock()
	defer pool.mu.Unlock()
	return pool.firstErr
}

func persistExportPNGMedia(ctx context.Context, tx pgx.Tx, cfg ossConfigPayload, uniqueID string, uploaderID int64, media []modExportPNGMedia) error {
	if len(media) == 0 {
		return nil
	}
	sort.Slice(media, func(left, right int) bool {
		if media[left].RevisionID == media[right].RevisionID {
			return media[left].AssetPath < media[right].AssetPath
		}
		return media[left].RevisionID < media[right].RevisionID
	})
	for start := 0; start < len(media); start += maxExportWriteBatchRows {
		end := min(start+maxExportWriteBatchRows, len(media))
		batch := &pgx.Batch{}
		for index := start; index < end; index++ {
			item := &media[index]
			batch.Queue(`insert into oss_files(bucket,endpoint,region,object_key,category,source,original_name,source_original_name,content_type,size_bytes,source_size_bytes,sha256,uploader_id,status,scan_status) values($1,$2,$3,$4,$5,'mcmods_exporter',$6,$6,'image/png',$7,$7,$8,$9,'active','pending') on conflict(object_key) do update set updated_at=now() returning id`, cfg.Bucket, cfg.displayEndpoint(), cfg.Region, item.ObjectKey, path.Join("project", uniqueID, "export-media"), item.Original, item.ByteLength, item.Digest, nullableUserID(uploaderID))
		}
		results := tx.SendBatch(ctx, batch)
		for index := start; index < end; index++ {
			if err := results.QueryRow().Scan(&media[index].FileID); err != nil {
				_ = results.Close()
				return err
			}
		}
		if err := results.Close(); err != nil {
			return err
		}
	}
	copied, err := tx.CopyFrom(ctx, pgx.Identifier{"mod_export_media"}, []string{"revision_id", "asset_path", "media_kind", "oss_file_id", "sha256", "content_type", "byte_length", "width", "height", "has_alpha"}, pgx.CopyFromSlice(len(media), func(index int) ([]any, error) {
		item := media[index]
		return []any{item.RevisionID, item.AssetPath, "png", item.FileID, item.Digest, "image/png", item.ByteLength, item.Width, item.Height, true}, nil
	}))
	if err != nil {
		return err
	}
	if copied != int64(len(media)) {
		return fmt.Errorf("copy PNG media: copied %d of %d rows", copied, len(media))
	}
	return nil
}

func exportRevisionForPath(revisions map[string]string, name string) string {
	parts := strings.Split(strings.ToLower(name), "/")
	for _, part := range parts {
		if revisionID := revisions[part]; revisionID != "" {
			return revisionID
		}
	}
	if len(revisions) == 1 {
		for _, revisionID := range revisions {
			return revisionID
		}
	}
	return ""
}

func normalizeExportNamespaces(values []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" || seen[value] {
			continue
		}
		valid := true
		for _, char := range value {
			if (char < 'a' || char > 'z') && (char < '0' || char > '9') && char != '_' && char != '-' && char != '.' {
				valid = false
				break
			}
		}
		if valid {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
}

func normalizeExportLoader(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value != "fabric" && value != "forge" && value != "neoforge" {
		return "unknown"
	}
	return value
}

func newExportID() string {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	encoded := hex.EncodeToString(value)
	return encoded[:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:]
}

func sha256Hex(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func utf8Text(value []byte) bool {
	return utf8.Valid(value)
}

func nullableUserID(value int64) any {
	if value <= 0 {
		return nil
	}
	return value
}

func (s *Server) notifyModExportResult(ctx context.Context, jobID, status string, failure error) {
	var recipientID int64
	var siteID, modName string
	var skipped int
	err := s.db.QueryRow(ctx,
		`select coalesce(j.created_by,0),m.slug,m.primary_name,coalesce((j.error_detail->>'translationValuesSkipped')::int,0)
		 from mod_export_jobs j join mods m on m.id=j.mod_id where j.id=$1`, jobID,
	).Scan(&recipientID, &siteID, &modName, &skipped)
	if err != nil || recipientID <= 0 {
		return
	}
	title := "模组资料导入完成"
	body := fmt.Sprintf("%s 的 mcmods_exporter 资料已经导入完成。", modName)
	if status == "partial" {
		title = "模组资料部分导入完成"
		body = fmt.Sprintf("%s 的资料已导入，但导入过程中存在警告。", modName)
		if skipped > 0 {
			body = fmt.Sprintf("%s 的资料已导入，但有 %d 条非字符串翻译被跳过。", modName, skipped)
		}
	}
	if status == "failed" {
		title = "模组资料导入失败"
		body = fmt.Sprintf("%s 的 mcmods_exporter 资料导入失败。", modName)
		if failure != nil {
			body += " " + failure.Error()
		}
	}
	s.enqueueOrCreateDirectNotification(ctx, recipientID, 0, "system", title, body, map[string]any{
		"type": "mod_export_import", "jobId": jobID, "modSiteId": siteID, "status": status, "translationValuesSkipped": skipped,
	})
}
