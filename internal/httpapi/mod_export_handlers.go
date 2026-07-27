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
	modExportImporterVersion  = "1.5.0"
	modExportTaskCode         = "mod_export_import"
	maxExportFileCount        = 200_000
	maxExportUncompressedSize = int64(8 << 30)
	maxExportSingleFileSize   = int64(512 << 20)
	maxExportJSONSize         = int64(128 << 20)
	maxExportWriteBatchRows   = 500
	maxExportBulkWriteRows    = 50_000
	maxExportWriteBatchBytes  = int64(32 << 20)
	maxExportReadBatchFiles   = 24
	maxExportReadBatchBytes   = int64(64 << 20)
	maxExportReadConcurrency  = 6
	maxExportPNGConcurrency   = 8
	maxExportTagCount         = 250_000
	maxExportTagMemberCount   = 2_000_000
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
		Modules       []string `json:"modules"`
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
	OSSFileID                   string `json:"ossFileId"`
	TargetVersionPublicID       string `json:"targetVersionPublicId"`
	OverwriteExistingImportData bool   `json:"overwriteExistingImportData"`
}

type modExportJobResponse struct {
	ID                          string         `json:"id"`
	ModSiteID                   string         `json:"modSiteId"`
	PackageID                   string         `json:"packageId"`
	TargetVersionPublicID       string         `json:"targetVersionPublicId"`
	OverwriteExistingImportData bool           `json:"overwriteExistingImportData"`
	Status                      string         `json:"status"`
	Progress                    int            `json:"progress"`
	CurrentStage                string         `json:"currentStage"`
	ErrorCode                   string         `json:"errorCode"`
	ErrorDetail                 map[string]any `json:"errorDetail"`
	Deduplicated                bool           `json:"deduplicated"`
	ReviewRequired              bool           `json:"reviewRequired"`
	CreatedAt                   time.Time      `json:"createdAt"`
	UpdatedAt                   time.Time      `json:"updatedAt"`
}

type modExportWriteBatch struct {
	batch             *pgx.Batch
	catalogRows       []catalogResourceImportRow
	textAssets        []exportTextAssetWrite
	recipes           []recipeImportRecipeWrite
	recipeBindings    []recipeImportBindingWrite
	recipeCandidates  []recipeImportCandidateWrite
	onStage           func(string) error
	rows              int
	byteCount         int64
	flushCount        int
	recipeCount       int
	bindingCount      int
	candidateCount    int
	payloadBytes      int64
	queuedDuration    time.Duration
	catalogDuration   time.Duration
	textAssetDuration time.Duration
	recipeDuration    time.Duration
	bindingDuration   time.Duration
}

type modExportArchiveFile struct {
	Name string
	Data []byte
}

type preparedExportNormalizedFile struct {
	catalogRows    []catalogResourceImportRow
	tagRows        []exportTagRow
	tagMemberCount int
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
	rows, err := worker.server.db.Query(context.Background(), `select id from catalog_import_jobs where status='queued' order by created_at`)
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

type resumeModExportUploadRequest struct {
	ObjectKey         string `json:"objectKey"`
	OriginalName      string `json:"originalName"`
	ContentType       string `json:"contentType"`
	SizeBytes         int64  `json:"sizeBytes"`
	SHA256            string `json:"sha256"`
	MultipartUploadID string `json:"multipartUploadId"`
}

func (s *Server) resumeModExportUpload(w http.ResponseWriter, r *http.Request) {
	identity, ok := s.requireModEditor(w, r)
	if !ok {
		return
	}
	var request resumeModExportUploadRequest
	if decodeJSON(r, &request) != nil {
		writeError(w, http.StatusBadRequest, "Invalid upload task")
		return
	}
	request.ObjectKey = strings.TrimSpace(request.ObjectKey)
	request.OriginalName = strings.TrimSpace(request.OriginalName)
	request.ContentType = strings.TrimSpace(request.ContentType)
	request.SHA256 = normalizeSHA256(request.SHA256)
	request.MultipartUploadID = strings.TrimSpace(request.MultipartUploadID)
	expectedCategory := ossModImportCategory(identity.UniqueID, "mcmods-exporter", "packages")
	client, cfg, err := s.ossClient(r.Context())
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	if request.ObjectKey == "" ||
		!isAllowedObjectKey(request.ObjectKey, ossObjectPrefix(cfg.Prefix, expectedCategory)) ||
		request.OriginalName == "" ||
		strings.ToLower(filepath.Ext(request.OriginalName)) != ".zip" ||
		request.SizeBytes <= 0 ||
		request.SHA256 == "" ||
		(request.MultipartUploadID != "" && !validOSSMultipartUploadID(request.MultipartUploadID)) {
		writeError(w, http.StatusBadRequest, "Invalid upload task")
		return
	}
	if request.ContentType == "" {
		request.ContentType = "application/zip"
	}
	expires := 60 * time.Minute
	if request.MultipartUploadID == "" {
		result, presignErr := client.Presign(
			r.Context(),
			&aliyunoss.PutObjectRequest{
				Bucket:      aliyunoss.Ptr(cfg.Bucket),
				Key:         aliyunoss.Ptr(request.ObjectKey),
				ContentType: aliyunoss.Ptr(request.ContentType),
				Metadata:    map[string]string{"sha256": request.SHA256},
			},
			aliyunoss.PresignExpires(expires),
		)
		if presignErr != nil {
			writeError(w, http.StatusBadGateway, "Failed to resume the OSS upload")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"method": result.Method, "url": result.URL, "headers": result.SignedHeaders,
			"bucket": cfg.Bucket, "objectKey": request.ObjectKey,
			"category": expectedCategory, "source": "mcmods_exporter",
			"originalName": request.OriginalName, "contentType": request.ContentType,
			"sizeBytes": request.SizeBytes, "sha256": request.SHA256,
			"uploadRequired": true, "expiresAt": time.Now().Add(expires),
			"accessUrl": buildPublicOSSURL(cfg, request.ObjectKey),
		})
		return
	}
	multipart, err := presignExistingOSSMultipartUpload(
		r.Context(), client, cfg, request.ObjectKey, request.MultipartUploadID, request.SizeBytes, expires,
	)
	if err != nil {
		writeError(w, http.StatusBadGateway, "Failed to resume the OSS multipart upload")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"method": "MULTIPART", "multipart": multipart,
		"bucket": cfg.Bucket, "objectKey": request.ObjectKey,
		"category": expectedCategory, "source": "mcmods_exporter",
		"originalName": request.OriginalName, "contentType": request.ContentType,
		"sizeBytes": request.SizeBytes, "sha256": request.SHA256,
		"uploadRequired": true, "expiresAt": time.Now().Add(expires),
		"accessUrl": buildPublicOSSURL(cfg, request.ObjectKey),
	})
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
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "导入文件记录不正确")
		return
	}
	request.TargetVersionPublicID = strings.ToLower(strings.TrimSpace(request.TargetVersionPublicID))
	request.OSSFileID = strings.ToLower(strings.TrimSpace(request.OSSFileID))
	if !validCatalogPublicID(request.OSSFileID) || request.TargetVersionPublicID == "" {
		writeError(w, http.StatusBadRequest, "导入文件记录不正确")
		return
	}
	var targetVersionID int64
	if err := s.db.QueryRow(r.Context(), `select id from mod_content_versions where mod_id=$1 and public_id=$2 and status='active'`, identity.ID, request.TargetVersionPublicID).Scan(&targetVersionID); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "请选择当前模组中有效的资料版本")
		return
	}
	claims := currentClaims(r)
	var archiveName, objectKey, archiveHash, source, category string
	var archiveFileID, archiveSize int64
	err := s.db.QueryRow(
		r.Context(),
		`select id,original_name, object_key, sha256, size_bytes, source, category
		 from oss_files where public_id=$1 and uploader_id=$2 and status='active'`,
		request.OSSFileID, claims.Subject,
	).Scan(&archiveFileID, &archiveName, &objectKey, &archiveHash, &archiveSize, &source, &category)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusBadRequest, "导入文件不存在或不属于当前用户")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取导入文件失败")
		return
	}
	expectedPrefix := ossModImportCategory(identity.UniqueID, "mcmods-exporter", "packages")
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
		`insert into catalog_import_packages
		 (id,sha256,archive_file_id,archive_name,schema_version,exporter_version,minecraft_version,loader,manifest,namespaces,profile,uploaded_by)
		 values ($1,$2,$3,$4,'','','','unknown','{}'::jsonb,'{}'::text[],'all',$5)
		 on conflict (sha256) do update set archive_file_id=excluded.archive_file_id,archive_name=excluded.archive_name,uploaded_by=excluded.uploaded_by
		 returning id`,
		packageID, archiveHash, archiveFileID, archiveName, claims.Subject,
	).Scan(&packageID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "保存导出包失败")
		return
	}
	deduplicated := false
	shouldPublish := false
	var existingStatus string
	var existingStalled bool
	err = tx.QueryRow(r.Context(),
		`select id,status,status in ('validating','importing') and coalesce(heartbeat_at,updated_at) < now() - $6::interval
		 from catalog_import_jobs where mod_id=$1 and package_id=$2 and importer_version=$3 and target_version_id=$4 and overwrite_existing=$5 for update`,
		identity.ID, packageID, modExportImporterVersion, targetVersionID, request.OverwriteExistingImportData, pgInterval(modExportStaleAfter),
	).Scan(&jobID, &existingStatus, &existingStalled)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		jobID = newExportID()
		_, err = tx.Exec(r.Context(),
			`insert into catalog_import_jobs (id,mod_id,package_id,importer_version,target_version_id,overwrite_existing,created_by) values ($1,$2,$3,$4,$5,$6,$7)`,
			jobID, identity.ID, packageID, modExportImporterVersion, targetVersionID, request.OverwriteExistingImportData, claims.Subject,
		)
		shouldPublish = err == nil
	case err == nil && (shouldRestartModExportStatus(existingStatus) || existingStalled):
		_, err = tx.Exec(r.Context(),
			`update catalog_import_jobs set status='queued',progress=0,current_stage='recovery',error_code='',error_detail='{}'::jsonb,created_by=$2,started_at=null,finished_at=null,heartbeat_at=null,run_token='',updated_at=now() where id=$1`,
			jobID, claims.Subject,
		)
		shouldPublish = err == nil
	case err == nil:
		deduplicated = true
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
		if err = insertModExportImportActivityTx(r.Context(), tx, claims.Subject, jobID, request.TargetVersionPublicID, request.OverwriteExistingImportData); err != nil {
			writeError(w, http.StatusInternalServerError, "记录导入用户行为失败")
			return
		}
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "提交导入任务失败")
		return
	}
	if shouldPublish {
		s.dispatchModExportJob(r.Context(), jobID)
	}
	response, _ := s.modExportJobByID(r.Context(), jobID, identity.ID)
	response.Deduplicated = deduplicated
	writeJSON(w, http.StatusAccepted, response)
}

func shouldRestartModExportStatus(status string) bool {
	switch status {
	case "ready", "partial", "failed", "cancelled":
		return true
	default:
		return false
	}
}

// Embedded icon importers retry failed/cancelled jobs in place, but unlike a
// full mcmods_exporter upload they do not intentionally rerun completed jobs.
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

func (s *Server) getActiveModExportJob(w http.ResponseWriter, r *http.Request) {
	identity, ok := s.requireModEditor(w, r)
	if !ok {
		return
	}
	targetVersionPublicID := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("targetVersionId")))
	if !validCatalogPublicID(targetVersionPublicID) {
		writeError(w, http.StatusBadRequest, "Invalid target data version")
		return
	}
	var jobID string
	err := s.db.QueryRow(r.Context(), `select job.id
		from catalog_import_jobs job
		join catalog_import_packages package on package.id=job.package_id
		join mod_content_versions version on version.id=job.target_version_id
		where job.mod_id=$1 and version.public_id=$2 and version.status='active'
		  and package.profile='all'
		  and job.status in ('queued','validating','importing')
		order by job.created_at desc,job.id desc limit 1`,
		identity.ID, targetVersionPublicID).Scan(&jobID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeJSON(w, http.StatusOK, map[string]any{"job": nil})
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to inspect active import jobs")
		return
	}
	if stalled, markErr := s.markStalledModExportJob(r.Context(), jobID, identity.ID); markErr != nil {
		writeError(w, http.StatusInternalServerError, "Failed to inspect the active import job")
		return
	} else if stalled {
		s.notifyModExportResult(r.Context(), jobID, "failed", errors.New("import worker stopped responding"))
	}
	job, err := s.modExportJobByID(r.Context(), jobID, identity.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to read the active import job")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"job": job})
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
	tag, err := s.db.Exec(r.Context(), `update catalog_import_jobs
		set status='cancelled',progress=least(progress,99),current_stage='cancelled',
			finished_at=now(),heartbeat_at=now(),run_token='',updated_at=now()
		where id=$1 and mod_id=$2 and status in ('queued','validating','importing')`,
		r.PathValue("jobId"), identity.ID)
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
		`select j.id,m.slug,j.package_id,version.public_id,j.overwrite_existing,j.status,j.progress,j.current_stage,j.error_code,j.error_detail,j.created_at,j.updated_at,
		 exists(select 1 from catalog_import_revisions r where r.job_id=j.id and r.status in ('ready','partial') and not r.is_active)
		 from catalog_import_jobs j join mods m on m.id=j.mod_id
		 join mod_content_versions version on version.id=j.target_version_id
		 where j.id=$1 and j.mod_id=$2`,
		jobID, modID,
	).Scan(&result.ID, &result.ModSiteID, &result.PackageID, &result.TargetVersionPublicID, &result.OverwriteExistingImportData, &result.Status, &result.Progress, &result.CurrentStage, &result.ErrorCode, &detail, &result.CreatedAt, &result.UpdatedAt, &result.ReviewRequired)
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

func (s *Server) importMCModsExportJob(ctx context.Context, jobID string) (resultErr error) {
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
		if resultErr == nil {
			return
		}
		cleanupContext, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		_ = s.cleanupModExportStaging(cleanupContext, packageID, modID, runToken)
		cancel()
		if errors.Is(resultErr, errModExportLeaseLost) || errors.Is(context.Cause(ctx), errModExportLeaseLost) {
			return
		}
		s.failModExportJob(jobID, runToken, "import_failed", resultErr)
	}()

	var expectedHash, objectKey, archiveName, uniqueID, targetVersionPublicID string
	var targetVersionID int64
	var overwriteExistingImportData bool
	var sourceFileID, createdBy int64
	err = s.db.QueryRow(
		ctx,
		`select j.package_id,j.mod_id,coalesce(j.created_by,0),j.target_version_id,version.public_id,j.overwrite_existing,p.sha256,coalesce(f.id,0),coalesce(f.object_key,''),p.archive_name,m.project_code
		 from catalog_import_jobs j join catalog_import_packages p on p.id=j.package_id join mods m on m.id=j.mod_id
		 join mod_content_versions version on version.id=j.target_version_id
		 left join oss_files f on f.id=p.archive_file_id where j.id=$1`,
		jobID,
	).Scan(&packageID, &modID, &createdBy, &targetVersionID, &targetVersionPublicID, &overwriteExistingImportData, &expectedHash, &sourceFileID, &objectKey, &archiveName, &uniqueID)
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
		return errors.Join(fmt.Errorf("download export archive: %w", err), temporary.Close())
	}
	hasher := sha256.New()
	_, copyErr := io.Copy(io.MultiWriter(temporary, hasher), io.LimitReader(getResult.Body, maxOSSUploadBytes+1))
	bodyCloseErr := getResult.Body.Close()
	closeErr := temporary.Close()
	if copyErr != nil {
		return errors.Join(copyErr, bodyCloseErr, closeErr)
	}
	if bodyCloseErr != nil {
		return errors.Join(bodyCloseErr, closeErr)
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
	if err = validateModExportManifest(manifest); err != nil {
		return err
	}
	loader := normalizeExportLoader(manifest.Loader)
	var targetVersionCompatible bool
	if err = s.db.QueryRow(ctx, `select exists(select 1 from mod_content_versions where mod_id=$1 and id=$2 and status='active'
		and (cardinality(minecraft_versions)=0 or $3=any(minecraft_versions))
		and (cardinality(loaders)=0 or exists(select 1 from unnest(loaders) selected_loader where lower(selected_loader)=lower($4))))`,
		modID, targetVersionID, manifest.MinecraftVersion, loader).Scan(&targetVersionCompatible); err != nil {
		return err
	}
	if !targetVersionCompatible {
		return errors.New("export package Minecraft version or loader does not match the selected data version")
	}
	capabilitiesFile := files[modExportCapabilitiesPath]
	if capabilitiesFile == nil {
		return fmt.Errorf("%s is missing", modExportCapabilitiesPath)
	}
	capabilitiesRaw, err := readExportZIPFile(capabilitiesFile, 4<<20)
	if err != nil {
		return err
	}
	capabilities, err := decodeModExportCapabilities(capabilitiesRaw, manifest)
	if err != nil {
		return err
	}
	partial := manifest.ErrorCount > 0
	translationValuesSkipped := 0
	failedStageCount := 0
	for _, stage := range manifest.Stages {
		if stage.Status == "failed" {
			partial = true
			failedStageCount++
		}
	}
	degradedCapabilityCount := 0
	unavailableCapabilityCount := 0
	for _, capability := range capabilities {
		switch capability.Status {
		case "degraded":
			degradedCapabilityCount++
		case "unavailable":
			unavailableCapabilityCount++
		}
	}
	manifestJSON, _ := json.Marshal(manifest)
	namespaces := normalizeExportNamespaces(manifest.Configuration.Namespaces)
	if len(namespaces) == 0 {
		return errors.New("manifest contains no valid namespace")
	}
	_, err = s.db.Exec(
		ctx,
		`update catalog_import_packages set schema_version=$2,exporter_version=$3,minecraft_version=$4,loader=$5,manifest=$6::jsonb,namespaces=$7,profile=$8 where id=$1`,
		packageID, manifest.SchemaVersion, manifest.ExporterVersion, manifest.MinecraftVersion, normalizeExportLoader(manifest.Loader), string(manifestJSON), namespaces, manifest.Configuration.Profile,
	)
	if err != nil {
		return err
	}
	if err = s.updateModExportJob(ctx, jobID, runToken, "importing", 20, "database"); err != nil {
		return err
	}
	if _, err = s.db.Exec(ctx, `delete from catalog_import_revisions where job_id=$1 and mod_id=$2 and status='staging'`, jobID, modID); err != nil {
		return err
	}
	revisions := make(map[string]string, len(namespaces))
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
				`select coalesce(max(revision_no),0)+1 from catalog_import_revisions where mod_id=$1 and target_version_id=$2 and source_namespace=$3`,
				modID, targetVersionID, namespace,
			).Scan(&revisionNo); queryErr != nil {
				return queryErr
			}
			_, insertErr := tx.Exec(
				ctx,
				`insert into catalog_import_revisions(id,mod_id,package_id,job_id,target_version_id,revision_no,minecraft_version,loader,exporter_version,source_namespace,source_metadata,import_run_token)
				 values($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11::jsonb,$12)`,
				revisionID, modID, packageID, jobID, targetVersionID, revisionNo, manifest.MinecraftVersion, loader, manifest.ExporterVersion, namespace, string(manifestJSON), runToken,
			)
			if insertErr != nil {
				return insertErr
			}
			revisions[namespace] = revisionID
		}
		return importExportCapabilities(ctx, tx, revisions, capabilities)
	})
	if err != nil {
		return err
	}
	if err = s.updateModExportJob(ctx, jobID, runToken, "importing", 24, "database_flush"); err != nil {
		return err
	}
	resourceResolver, err := loadCatalogResourceIdentityResolver(ctx, s.db)
	if err != nil {
		return fmt.Errorf("load Mod ID aliases: %w", err)
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
	localeBundles := make([]modExportLocaleBundleMedia, 0)
	scheduledPNGObjects := make(map[string]struct{})
	textAssetCount := 0
	binaryAssetCount := 0
	supportedTranslations := make(map[string]map[string]string, len(exportContentLocaleByCode))
	flushWriteBatch := func() error {
		return s.runModExportTransaction(ctx, func(tx pgx.Tx) error {
			return writeBatch.flush(ctx, tx)
		})
	}
	baseRecipeFile := files["recipes/recipes.json"]
	if baseRecipeFile == nil {
		return fmt.Errorf("export package is missing recipes/recipes.json")
	}
	baseRecipeData, readErr := readExportZIPFile(baseRecipeFile, maxExportJSONSize)
	if readErr != nil {
		return fmt.Errorf("read recipes/recipes.json: %w", readErr)
	}
	if err = validateExportBaseRecipeDocument(baseRecipeData); err != nil {
		return err
	}
	categoryFile := files["recipes/jei/categories.json"]
	if categoryFile == nil {
		return fmt.Errorf("export package is missing recipes/jei/categories.json")
	}
	categoryData, readErr := readExportZIPFile(categoryFile, maxExportJSONSize)
	if readErr != nil {
		return fmt.Errorf("read recipes/jei/categories.json: %w", readErr)
	}
	if err = s.runModExportTransaction(ctx, func(tx pgx.Tx) error {
		return importExportRecipeTypes(ctx, tx, packageID, revisions, categoryData)
	}); err != nil {
		return err
	}
	if err = s.updateModExportJob(ctx, jobID, runToken, "importing", 30, "database_flush"); err != nil {
		return err
	}
	if err = processExportRecipeJSONFiles(ctx, files, "recipes/jei/templates/", decodeExportJEITemplateCollection,
		func(name string, document exportJEITemplateCollection) error {
			if queueErr := queueExportJEITemplateCollection(writeBatch, revisions, name, document); queueErr != nil {
				return queueErr
			}
			if writeBatch.shouldFlush() {
				return flushWriteBatch()
			}
			return nil
		}); err != nil {
		return err
	}
	if err = flushWriteBatch(); err != nil {
		return err
	}
	if err = s.updateModExportJob(ctx, jobID, runToken, "importing", 36, "database_flush"); err != nil {
		return err
	}
	writeBatch.onStage = func(stage string) error {
		progress := map[string]int{
			"recipes":           42,
			"recipe_bindings":   54,
			"recipe_candidates": 64,
		}[stage]
		if progress == 0 {
			return nil
		}
		return s.updateModExportJob(ctx, jobID, runToken, "importing", progress, "database_flush")
	}
	if err = processExportRecipeJSONFiles(ctx, files, "recipes/jei/recipes/", decodeExportJEIRecipeCollection,
		func(name string, document exportJEIRecipeCollection) error {
			if queueErr := queueExportJEIRecipeCollection(writeBatch, resourceResolver, packageID, revisions, name, document); queueErr != nil {
				return queueErr
			}
			if writeBatch.shouldFlush() {
				return flushWriteBatch()
			}
			return nil
		}); err != nil {
		return err
	}
	if err = flushWriteBatch(); err != nil {
		return err
	}
	writeBatch.onStage = nil
	if err = s.updateModExportJob(ctx, jobID, runToken, "importing", 70, "assets"); err != nil {
		return err
	}
	fallbackAssetPaths, err := normalizedExportFallbackAssetPaths(files)
	if err != nil {
		return err
	}
	assetFileNames := exportAssetFileNames(fileNames, files)
	processedFiles := 0
	for start := 0; start < len(assetFileNames); {
		end := exportReadBatchEnd(assetFileNames, files, start)
		loadedFiles, readErr := readExportZIPFiles(ctx, files, assetFileNames[start:end])
		if readErr != nil {
			return readErr
		}

		batchSkippedTranslations := 0
		preparedNormalized := make([]preparedExportNormalizedFile, 0)
		for _, loaded := range loadedFiles {
			switch {
			case isExportTranslationFile(loaded.Name):
				translation, prepareErr := prepareExportTranslation(loaded.Name, loaded.Data)
				if prepareErr != nil {
					return prepareErr
				}
				batchSkippedTranslations += translation.Total - len(translation.Values)
				if _, editable := supportedEditableContentLocales[translation.Locale]; editable {
					supportedTranslations[translation.Locale] = translation.Values
				}
				bundles, compressed, bundleErr := prepareExportLocaleBundles(cfg, uniqueID, revisions, translation)
				if bundleErr != nil {
					return bundleErr
				}
				for _, bundle := range bundles {
					localeBundles = append(localeBundles, bundle)
					pngPool.submitAsset(bundle.ObjectKey, bundle.AssetPath, bundle.Digest, bundle.ContentType, compressed)
				}
			case isExportRegistryFile(loaded.Name):
				rows, prepareErr := prepareExportRegistryResources(resourceResolver, revisions, loaded.Name, loaded.Data)
				if prepareErr != nil {
					return prepareErr
				}
				if len(rows) > 0 {
					preparedNormalized = append(preparedNormalized, preparedExportNormalizedFile{catalogRows: rows})
				}
			case loaded.Name == "tags/tags.json":
				rows, memberCount, prepareErr := decodeExportTags(revisions, loaded.Data)
				if prepareErr != nil {
					return prepareErr
				}
				if len(rows) > 0 {
					preparedNormalized = append(preparedNormalized, preparedExportNormalizedFile{
						tagRows: rows, tagMemberCount: memberCount,
					})
				}
			}
		}
		if len(preparedNormalized) > 0 {
			err = s.runModExportTransaction(ctx, func(tx pgx.Tx) error {
				for _, prepared := range preparedNormalized {
					switch {
					case len(prepared.catalogRows) > 0:
						if persistErr := persistCatalogResources(ctx, tx, prepared.catalogRows); persistErr != nil {
							return persistErr
						}
					case len(prepared.tagRows) > 0:
						if persistErr := persistExportTags(ctx, tx, resourceResolver, prepared.tagRows, prepared.tagMemberCount); persistErr != nil {
							return persistErr
						}
					}
				}
				return nil
			})
			if err != nil {
				return err
			}
		}
		if batchSkippedTranslations > 0 {
			partial = true
			translationValuesSkipped += batchSkippedTranslations
		}

		for _, loaded := range loadedFiles {
			name, data := loaded.Name, loaded.Data
			processedFiles++
			if isExportTranslationFile(name) {
				continue
			}
			if name == "tags/tags.json" {
				continue
			}
			if name == modExportCapabilitiesPath {
				for _, revisionID := range revisions {
					if err = queueExportTextAsset(writeBatch, revisionID, name, data); err != nil {
						return err
					}
					textAssetCount++
				}
				continue
			}
			extension := strings.ToLower(path.Ext(name))
			if extension == ".json" && exportDocumentKind(name) != "" {
				if int64(len(data)) > maxExportJSONSize {
					return fmt.Errorf("JSON asset too large: %s", name)
				}
				if err = queueExportDocumentEntries(writeBatch, resourceResolver, revisions, name, data); err != nil {
					return err
				}
				if retainExportTextAsset(name, fallbackAssetPaths) {
					for _, revisionID := range sortedExportRevisionIDs(revisions) {
						if err = queueExportTextAsset(writeBatch, revisionID, name, data); err != nil {
							return err
						}
						textAssetCount++
					}
				}
				if writeBatch.shouldFlush() {
					if err = flushWriteBatch(); err != nil {
						return err
					}
				}
				continue
			}
			revisionID := exportRevisionForPath(revisions, name)
			if revisionID == "" {
				continue
			}
			switch extension {
			case ".png":
				var media modExportPNGMedia
				if media, err = inspectExportPNG(cfg, revisionID, uniqueID, name, data, resourceResolver); err != nil {
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
				if extension != ".json" || retainExportTextAsset(name, fallbackAssetPaths) {
					if err = queueExportTextAsset(writeBatch, revisionID, name, data); err != nil {
						return err
					}
					textAssetCount++
				}
			}
			if writeBatch.shouldFlush() {
				if err = flushWriteBatch(); err != nil {
					return err
				}
			}
		}
		if (processedFiles-len(loadedFiles))/200 != processedFiles/200 {
			progress := 70 + int(float64(processedFiles)/float64(max(1, len(assetFileNames)))*16)
			if err = s.updateModExportJob(ctx, jobID, runToken, "importing", progress, "assets"); err != nil {
				return err
			}
		}
		start = end
	}
	if err = s.updateModExportJob(ctx, jobID, runToken, "importing", 88, "database_flush"); err != nil {
		return err
	}
	if err = flushWriteBatch(); err != nil {
		return err
	}
	if err = s.updateModExportJob(ctx, jobID, runToken, "importing", 90, "language_bundles"); err != nil {
		return err
	}
	if err = pngPool.wait(); err != nil {
		return err
	}
	for start := 0; start < len(pngMedia); start += maxExportWriteBatchRows {
		end := min(start+maxExportWriteBatchRows, len(pngMedia))
		if err = s.runModExportTransaction(ctx, func(tx pgx.Tx) error {
			return persistExportPNGMedia(ctx, tx, cfg, uniqueID, createdBy, "mcmods_exporter", pngMedia[start:end])
		}); err != nil {
			return err
		}
	}
	if err = s.runModExportTransaction(ctx, func(tx pgx.Tx) error {
		return persistExportLocaleBundles(ctx, tx, cfg, createdBy, "mcmods_exporter", localeBundles)
	}); err != nil {
		return fmt.Errorf("persist locale bundles: %w", err)
	}
	if err = s.runModExportTransaction(ctx, func(tx pgx.Tx) error {
		return importExportIconCatalogResources(ctx, tx, resourceResolver, pngMedia, supportedTranslations)
	}); err != nil {
		return fmt.Errorf("import normalized icon catalogs: %w", err)
	}
	blockBindings, err := deriveModExportBlockBindings(files, resourceResolver, revisions)
	if err != nil {
		return err
	}
	blockEntityModels, err := deriveModExportBlockEntityModels(files, resourceResolver, revisions)
	if err != nil {
		return err
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
	jobDetail, _ := json.Marshal(map[string]int{
		"translationValuesSkipped": translationValuesSkipped,
		"manifestErrors":           manifest.ErrorCount,
		"failedStages":             failedStageCount,
		"degradedCapabilities":     degradedCapabilityCount,
		"unavailableCapabilities":  unavailableCapabilityCount,
	})
	revisionIDs := make([]string, 0, len(revisions))
	for _, revisionID := range revisions {
		revisionIDs = append(revisionIDs, revisionID)
	}
	sort.Strings(revisionIDs)
	err = s.runModExportTransaction(ctx, func(tx pgx.Tx) error {
		if modelErr := persistModExportBlockEntityModels(ctx, tx, blockEntityModels); modelErr != nil {
			return modelErr
		}
		if bindingErr := persistModExportBlockBindings(ctx, tx, blockBindings); bindingErr != nil {
			return bindingErr
		}
		for _, revisionID := range revisionIDs {
			if canActivate {
				var namespace string
				var sourceKind string
				if queryErr := tx.QueryRow(ctx, `select source_namespace,source_kind from catalog_import_revisions where id=$1 and import_run_token=$2`, revisionID, runToken).Scan(&namespace, &sourceKind); queryErr != nil {
					return queryErr
				}
				if _, updateErr := tx.Exec(ctx, `update catalog_import_revisions set is_active=false,status='superseded' where mod_id=$1 and target_version_id=$2 and source_namespace=$3 and source_kind=$4 and id<>$5 and is_active`, modID, targetVersionID, namespace, sourceKind, revisionID); updateErr != nil {
					return updateErr
				}
			}
			if _, updateErr := tx.Exec(ctx, `update catalog_import_revisions set status=$2,is_active=$3,activated_at=case when $3 then now() else null end where id=$1 and import_run_token=$4`, revisionID, revisionStatus, canActivate, runToken); updateErr != nil {
				return updateErr
			}
			if canActivate {
				if promotionErr := promoteImportedRecipeTemplatesTx(ctx, tx, revisionID); promotionErr != nil {
					return fmt.Errorf("promote imported recipe templates: %w", promotionErr)
				}
			}
		}
		if canActivate {
			if syncErr := syncImportedResourcesToContentVersionTx(ctx, tx, revisionIDs, targetVersionID, overwriteExistingImportData, createdBy); syncErr != nil {
				return syncErr
			}
		}
		if statsErr := refreshModExportRevisionStats(ctx, tx, revisionIDs); statsErr != nil {
			return statsErr
		}
		if _, logErr := tx.Exec(ctx, `insert into catalog_import_job_logs(job_id,level,stage,message) values($1,'info','performance',$2)`, jobID, fmt.Sprintf("files=%d text_assets=%d binary_assets=%d png_assets=%d duration=%s", len(fileNames), textAssetCount, binaryAssetCount, len(pngMedia), time.Since(importStarted).Round(time.Millisecond))); logErr != nil {
			return logErr
		}
		tag, updateErr := tx.Exec(ctx, `update catalog_import_jobs set status=$3,progress=100,current_stage=case when $4 then 'complete' else 'review' end,error_detail=$5::jsonb,finished_at=now(),heartbeat_at=now(),updated_at=now(),run_token='' where id=$1 and run_token=$2`, jobID, runToken, jobStatus, canActivate, string(jobDetail))
		if updateErr != nil {
			return updateErr
		}
		if tag.RowsAffected() == 0 {
			return errModExportLeaseLost
		}
		_, updateErr = tx.Exec(ctx, `update catalog_import_packages set imported_at=now() where id=$1`, packageID)
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

func exportString(value any) string {
	result, _ := value.(string)
	return result
}

func exportIngredientKind(ingredientType string) string {
	value := strings.ToLower(strings.TrimSpace(ingredientType))
	switch {
	case value == "item_stack" || value == "minecraft:item_stack":
		return "item"
	case value == "fluid_stack" || strings.Contains(value, "fluidstack"):
		return "fluid"
	case strings.Contains(value, ".gas.") || strings.HasSuffix(value, "gasstack"):
		return "mekanism_gas"
	case strings.Contains(value, ".infuse.") || strings.Contains(value, "infusionstack"):
		return "mekanism_infuse_type"
	case strings.Contains(value, ".pigment.") || strings.HasSuffix(value, "pigmentstack"):
		return "mekanism_pigment"
	case strings.Contains(value, ".slurry.") || strings.HasSuffix(value, "slurrystack"):
		return "mekanism_slurry"
	case value == "":
		return "unknown"
	default:
		return value
	}
}

func exportRecipeAmount(alternative map[string]any) float64 {
	for _, key := range []string{"count", "amount"} {
		if value, ok := alternative[key].(float64); ok && value > 0 {
			return value
		}
	}
	return 1
}

func nonEmptyJSON(value json.RawMessage, fallback string) string {
	if len(value) == 0 || string(value) == "null" {
		return fallback
	}
	return string(value)
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
	if limit < 0 || file.UncompressedSize64 > uint64(limit) {
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

func exportAssetFileNames(fileNames []string, files map[string]*zip.File) []string {
	result := make([]string, 0, len(fileNames))
	for _, name := range fileNames {
		file := files[name]
		if file == nil || file.FileInfo().IsDir() || name == "recipes/jei/categories.json" ||
			strings.HasPrefix(name, "recipes/jei/templates/") || strings.HasPrefix(name, "recipes/jei/recipes/") {
			continue
		}
		result = append(result, name)
	}
	return result
}

func exportReadBatchEnd(names []string, files map[string]*zip.File, start int) int {
	end := start
	var byteCount uint64
	for end < len(names) && end-start < maxExportReadBatchFiles {
		fileSize := files[names[end]].UncompressedSize64
		if end > start && (fileSize > uint64(maxExportReadBatchBytes) || byteCount > uint64(maxExportReadBatchBytes)-fileSize) {
			break
		}
		byteCount += fileSize
		end++
	}
	return max(start+1, end)
}

func readExportZIPFiles(ctx context.Context, files map[string]*zip.File, names []string) ([]modExportArchiveFile, error) {
	if len(names) == 0 {
		return nil, nil
	}
	workerCount := min(len(names), maxExportReadConcurrency)
	type readJob struct {
		index int
		name  string
	}
	jobs := make(chan readJob)
	results := make([]modExportArchiveFile, len(names))
	var waitGroup sync.WaitGroup
	var firstErr error
	var errorMutex sync.Mutex
	for range workerCount {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			for job := range jobs {
				if context.Cause(ctx) != nil {
					continue
				}
				data, err := readExportZIPFile(files[job.name], maxExportSingleFileSize)
				if err != nil {
					errorMutex.Lock()
					if firstErr == nil {
						firstErr = fmt.Errorf("read %s: %w", job.name, err)
					}
					errorMutex.Unlock()
					continue
				}
				results[job.index] = modExportArchiveFile{Name: job.name, Data: data}
			}
		}()
	}
	for index, name := range names {
		if context.Cause(ctx) != nil {
			break
		}
		jobs <- readJob{index: index, name: name}
	}
	close(jobs)
	waitGroup.Wait()
	if err := context.Cause(ctx); err != nil {
		return nil, err
	}
	if firstErr != nil {
		return nil, firstErr
	}
	return results, nil
}

func isExportTranslationFile(name string) bool {
	return strings.HasPrefix(name, "translations/") && strings.HasSuffix(name, ".json") && path.Base(name) != "languages.json"
}

func isSupportedExportTranslationFile(name string) bool {
	if !isExportTranslationFile(name) {
		return false
	}
	_, supported := canonicalExportLocaleTag(strings.TrimSuffix(path.Base(name), path.Ext(name)))
	return supported
}

func isExportRegistryFile(name string) bool {
	return strings.HasPrefix(name, "registries/") && strings.HasSuffix(name, ".json")
}

type preparedExportTranslation struct {
	Name        string
	Locale      string
	Values      map[string]string
	SkippedKeys []string
	Total       int
}

func prepareExportTranslation(name string, raw []byte) (preparedExportTranslation, error) {
	sourceLocale := strings.TrimSuffix(path.Base(name), path.Ext(name))
	locale, valid := canonicalExportLocaleTag(sourceLocale)
	if !valid {
		return preparedExportTranslation{}, fmt.Errorf("invalid translation locale: %s", sourceLocale)
	}
	if supported, exists := exportContentLocale(sourceLocale); exists {
		locale = supported
	}
	prepared := preparedExportTranslation{
		Name:   name,
		Locale: locale,
	}
	values, skippedKeys, total, err := decodeExportTranslationValues(raw)
	if err != nil {
		return prepared, fmt.Errorf("decode translations %s: %w", name, err)
	}
	prepared.Values = values
	prepared.SkippedKeys = skippedKeys
	prepared.Total = total
	return prepared, nil
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

func prepareExportRegistryResources(resolver catalogResourceIdentityResolver, revisions map[string]string, name string, raw []byte) ([]catalogResourceImportRow, error) {
	var document struct {
		Registry string            `json:"registry"`
		Entries  []json.RawMessage `json:"entries"`
	}
	if err := json.Unmarshal(raw, &document); err != nil {
		return nil, fmt.Errorf("decode registry %s: %w", name, err)
	}
	if document.Registry == "" {
		document.Registry = strings.TrimSuffix(path.Base(name), path.Ext(name))
	}
	rowsByKey := make(map[string]catalogResourceImportRow, len(document.Entries))
	for _, entryRaw := range document.Entries {
		var entry map[string]any
		if err := json.Unmarshal(entryRaw, &entry); err != nil {
			return nil, err
		}
		objectID, _ := entry["id"].(string)
		namespace, resourcePath, valid := exportSourceResourceParts(document.Registry, objectID, "")
		if !valid {
			continue
		}
		revisionID := revisions[namespace]
		if revisionID == "" {
			continue
		}
		kindCode := resourceKindForRegistry(document.Registry)
		identity := resolveExportResourceIdentity(resolver, kindCode, document.Registry, objectID, namespace)
		translationKey, _ := entry["translation_key"].(string)
		names, _ := json.Marshal(supportedExportNames(entry["names"]))
		filterSupportedExportLocalizedFields(entry)
		filteredEntry, marshalErr := json.Marshal(entry)
		if marshalErr != nil {
			return nil, marshalErr
		}
		if string(names) == "null" || len(names) == 0 {
			names = []byte("{}")
		}
		iconPath, previewPath := exportRegistryMediaPaths(document.Registry, namespace, resourcePath)
		rowsByKey[revisionID+"\x00"+kindCode+"\x00"+objectID] = catalogResourceImportRow{
			EntityID: identity.ID, PublicID: identity.PublicID, KindCode: kindCode, CanonicalID: identity.CanonicalID, RawID: identity.RawID,
			Namespace: identity.Namespace, ResourcePath: identity.ResourcePath, RevisionID: revisionID,
			SnapshotID: catalogSnapshotID("resource", revisionID, identity.ID, ""), Registry: document.Registry,
			TranslationKey: translationKey, Names: string(names), Data: string(compactImportJSONObject(
				filteredEntry,
				"id", "namespace", "path", "translation_key", "names",
			)),
			IconPath: iconPath, PreviewPath: previewPath,
		}
	}
	rowKeys := make([]string, 0, len(rowsByKey))
	for key := range rowsByKey {
		rowKeys = append(rowKeys, key)
	}
	sort.Strings(rowKeys)
	if len(rowKeys) == 0 {
		return nil, nil
	}
	rows := make([]catalogResourceImportRow, len(rowKeys))
	for index, key := range rowKeys {
		rows[index] = rowsByKey[key]
	}
	return rows, nil
}

func exportRegistryMediaPaths(registry, namespace, resourcePath string) (string, string) {
	switch registry {
	case "items", "blocks", "mob_effects":
		return fmt.Sprintf("icons/%s/32/%s/%s.png", registry, namespace, resourcePath),
			fmt.Sprintf("icons/%s/256/%s/%s.png", registry, namespace, resourcePath)
	case "entity_types":
		return fmt.Sprintf("entities/renders/32/%s/%s.png", namespace, resourcePath),
			fmt.Sprintf("entities/renders/256/%s/%s.png", namespace, resourcePath)
	default:
		return "", ""
	}
}

type exportTagRow struct {
	RevisionID string
	Registry   string
	TagID      string
	Members    []string
}

func importExportTags(ctx context.Context, tx pgx.Tx, resolver catalogResourceIdentityResolver, revisions map[string]string, raw []byte) error {
	rows, memberCount, err := decodeExportTags(revisions, raw)
	if err != nil {
		return err
	}
	return persistExportTags(ctx, tx, resolver, rows, memberCount)
}

func persistExportTags(ctx context.Context, tx pgx.Tx, resolver catalogResourceIdentityResolver, rows []exportTagRow, memberCount int) error {
	if len(rows) == 0 {
		return nil
	}
	if _, err := tx.Exec(ctx, `create temporary table import_tag_stage(
		entity_id text,public_id text,snapshot_id text,revision_id text,registry text,canonical_id text,member_count integer
	) on commit drop`); err != nil {
		return err
	}
	copied, err := tx.CopyFrom(ctx, pgx.Identifier{"import_tag_stage"},
		[]string{"entity_id", "public_id", "snapshot_id", "revision_id", "registry", "canonical_id", "member_count"},
		pgx.CopyFromSlice(len(rows), func(index int) ([]any, error) {
			row := rows[index]
			identity := tagIdentity(row.Registry, row.TagID)
			return []any{identity.ID, identity.PublicID, catalogSnapshotID("tag", row.RevisionID, identity.ID, ""), row.RevisionID, row.Registry, row.TagID, len(row.Members)}, nil
		}))
	if err != nil {
		return err
	}
	if copied != int64(len(rows)) {
		return fmt.Errorf("copy tags: copied %d of %d rows", copied, len(rows))
	}
	if _, err = tx.Exec(ctx, `insert into catalog_entities(identity_key,public_id,entity_type,status)
		select distinct entity_id,public_id,'tag','active' from import_tag_stage on conflict(identity_key) do update set status='active',updated_at=now()`); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `insert into catalog_tags(entity_id,registry,canonical_id)
		select distinct entity.id,stage.registry,stage.canonical_id from import_tag_stage stage
		join catalog_entities entity on entity.identity_key=stage.entity_id
		on conflict(registry,canonical_id) do nothing`); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `insert into tag_import_snapshots(id,tag_id,revision_id,member_count)
		select stage.snapshot_id,entity.id,stage.revision_id,stage.member_count from import_tag_stage stage
		join catalog_entities entity on entity.identity_key=stage.entity_id
		on conflict(tag_id,revision_id) do update set member_count=excluded.member_count`); err != nil {
		return err
	}
	if memberCount == 0 {
		return nil
	}
	if _, err = tx.Exec(ctx, `create temporary table import_tag_member_stage(
		tag_snapshot_id text,resource_id text,resource_public_id text,kind_code text,raw_member_id text,canonical_id text,namespace text,resource_path text,ordinal integer
	) on commit drop`); err != nil {
		return err
	}
	rowIndex, memberIndex := 0, 0
	copied, err = tx.CopyFrom(ctx, pgx.Identifier{"import_tag_member_stage"},
		[]string{"tag_snapshot_id", "resource_id", "resource_public_id", "kind_code", "raw_member_id", "canonical_id", "namespace", "resource_path", "ordinal"},
		pgx.CopyFromFunc(func() ([]any, error) {
			for rowIndex < len(rows) && memberIndex >= len(rows[rowIndex].Members) {
				rowIndex++
				memberIndex = 0
			}
			if rowIndex >= len(rows) {
				return nil, nil
			}
			row := rows[rowIndex]
			tag := tagIdentity(row.Registry, row.TagID)
			memberID := row.Members[memberIndex]
			kindCode := resourceKindForRegistry(row.Registry)
			resource := resolver.resolve(kindCode, memberID)
			values := []any{catalogSnapshotID("tag", row.RevisionID, tag.ID, ""), resource.ID, resource.PublicID, kindCode, memberID, resource.CanonicalID, resource.Namespace, resource.ResourcePath, memberIndex}
			memberIndex++
			return values, nil
		}))
	if err != nil {
		return err
	}
	if copied != int64(memberCount) {
		return fmt.Errorf("copy tag members: copied %d of %d rows", copied, memberCount)
	}
	statements := []string{
		`insert into resource_kinds(code,family,user_visible) select distinct kind_code,split_part(kind_code,'.',1),true from import_tag_member_stage on conflict(code) do nothing`,
		`insert into catalog_entities(identity_key,public_id,entity_type,status)
		 select distinct stage.resource_id,stage.resource_public_id,'resource','placeholder'
		 from import_tag_member_stage stage
		 left join game_resources existing
		  on existing.kind_code=stage.kind_code and existing.canonical_id=stage.canonical_id
		 where existing.entity_id is null
		 on conflict(identity_key) do nothing`,
		`insert into game_resources(entity_id,kind_code,canonical_id,namespace,resource_path,resolved)
		 select distinct coalesce(existing.entity_id,entity.id),stage.kind_code,stage.canonical_id,stage.namespace,stage.resource_path,false
		 from import_tag_member_stage stage
		 left join game_resources existing
		  on existing.kind_code=stage.kind_code and existing.canonical_id=stage.canonical_id
		 left join catalog_entities entity on entity.identity_key=stage.resource_id
		 where existing.entity_id is not null or entity.id is not null
		 on conflict(kind_code,canonical_id) do nothing`,
		`insert into game_resource_aliases(kind_code,alias_id,resource_id,source)
		 select distinct stage.kind_code,stage.raw_member_id,resource.entity_id,'mod_id'
		 from import_tag_member_stage stage
		 join game_resources resource
		  on resource.kind_code=stage.kind_code and resource.canonical_id=stage.canonical_id
		 on conflict(kind_code,alias_id) do update set resource_id=excluded.resource_id,source=excluded.source,updated_at=now()`,
		`insert into tag_import_members(tag_snapshot_id,resource_id,raw_member_id,ordinal)
		 select stage.tag_snapshot_id,resource.entity_id,stage.raw_member_id,stage.ordinal
		 from import_tag_member_stage stage
		 join game_resources resource
		  on resource.kind_code=stage.kind_code and resource.canonical_id=stage.canonical_id
		 on conflict(tag_snapshot_id,raw_member_id) do update set resource_id=excluded.resource_id,ordinal=excluded.ordinal`,
	}
	for _, statement := range statements {
		if _, err = tx.Exec(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}

func decodeExportTags(revisions map[string]string, raw []byte) ([]exportTagRow, int, error) {
	var document struct {
		SchemaVersion string `json:"schema_version"`
		Registries    []struct {
			Registry string `json:"registry"`
			Tags     []struct {
				ID     string   `json:"id"`
				Values []string `json:"values"`
			} `json:"tags"`
		} `json:"registries"`
	}
	if err := json.Unmarshal(raw, &document); err != nil {
		return nil, 0, fmt.Errorf("decode tags/tags.json: %w", err)
	}
	if document.SchemaVersion != "" && document.SchemaVersion != "mcmods-tags/v1" {
		return nil, 0, fmt.Errorf("unsupported tags schema: %s", document.SchemaVersion)
	}
	rows := make([]exportTagRow, 0)
	memberCount := 0
	for _, registry := range document.Registries {
		registryName := strings.ToLower(strings.TrimSpace(registry.Registry))
		if registryName == "" {
			continue
		}
		for _, tag := range registry.Tags {
			tagID := strings.TrimSpace(tag.ID)
			if tagID == "" {
				continue
			}
			members := uniqueExportResourceIDs(tag.Values)
			relevant := make(map[string]string)
			if revisionID := revisions[exportResourceNamespace(tagID)]; revisionID != "" {
				relevant[revisionID] = revisionID
			}
			for _, memberID := range members {
				if revisionID := revisions[exportResourceNamespace(memberID)]; revisionID != "" {
					relevant[revisionID] = revisionID
				}
			}
			for revisionID := range relevant {
				rows = append(rows, exportTagRow{RevisionID: revisionID, Registry: registryName, TagID: tagID, Members: members})
				memberCount += len(members)
				if len(rows) > maxExportTagCount || memberCount > maxExportTagMemberCount {
					return nil, 0, errors.New("export tag data exceeds limit")
				}
			}
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].RevisionID != rows[j].RevisionID {
			return rows[i].RevisionID < rows[j].RevisionID
		}
		if rows[i].Registry != rows[j].Registry {
			return rows[i].Registry < rows[j].Registry
		}
		return rows[i].TagID < rows[j].TagID
	})
	return rows, memberCount, nil
}

func uniqueExportResourceIDs(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func exportResourceNamespace(value string) string {
	parts := strings.SplitN(value, ":", 2)
	if len(parts) != 2 {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(parts[0]))
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
	bulkRows := len(batch.catalogRows) + len(batch.textAssets) + len(batch.recipes) + len(batch.recipeBindings) + len(batch.recipeCandidates)
	return batch.rows >= maxExportWriteBatchRows ||
		bulkRows >= maxExportBulkWriteRows ||
		batch.byteCount >= maxExportWriteBatchBytes
}

func (batch *modExportWriteBatch) flush(ctx context.Context, tx pgx.Tx) error {
	batch.flushCount++
	batch.recipeCount += len(batch.recipes)
	batch.bindingCount += len(batch.recipeBindings)
	batch.candidateCount += len(batch.recipeCandidates)
	batch.payloadBytes += batch.byteCount
	if batch.rows > 0 {
		started := time.Now()
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
		if firstErr != nil {
			return firstErr
		}
		batch.queuedDuration += time.Since(started)
	}
	if len(batch.catalogRows) > 0 {
		started := time.Now()
		if err := persistCatalogResources(ctx, tx, batch.catalogRows); err != nil {
			return err
		}
		batch.catalogDuration += time.Since(started)
	}
	if len(batch.textAssets) > 0 {
		started := time.Now()
		if err := persistExportTextAssets(ctx, tx, batch.textAssets); err != nil {
			return err
		}
		batch.textAssetDuration += time.Since(started)
	}
	if len(batch.recipes) > 0 {
		if batch.onStage != nil {
			if err := batch.onStage("recipes"); err != nil {
				return err
			}
		}
		started := time.Now()
		if err := persistRecipeImports(ctx, tx, batch.recipes); err != nil {
			return err
		}
		batch.recipeDuration += time.Since(started)
	}
	if len(batch.recipeBindings) > 0 || len(batch.recipeCandidates) > 0 {
		started := time.Now()
		if err := persistRecipeImportBindings(ctx, tx, batch.recipeBindings, batch.recipeCandidates, batch.onStage); err != nil {
			return err
		}
		batch.bindingDuration += time.Since(started)
	}
	batch.batch = &pgx.Batch{}
	batch.catalogRows = batch.catalogRows[:0]
	batch.textAssets = batch.textAssets[:0]
	batch.recipes = batch.recipes[:0]
	batch.recipeBindings = batch.recipeBindings[:0]
	batch.recipeCandidates = batch.recipeCandidates[:0]
	batch.rows = 0
	batch.byteCount = 0
	return nil
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
		batch.textAssets = append(batch.textAssets, exportTextAssetWrite{
			RevisionID: revisionID, AssetPath: name, AssetKind: assetKind,
			ContentType: "application/json", Digest: digest, ByteLength: int64(len(data)),
			Content: string(canonical), JSON: true,
		})
		batch.byteCount += int64(len(canonical))
		return nil
	}
	if !utf8Text(data) {
		return fmt.Errorf("text asset is not valid UTF-8: %s", name)
	}
	batch.textAssets = append(batch.textAssets, exportTextAssetWrite{
		RevisionID: revisionID, AssetPath: name, AssetKind: assetKind,
		ContentType: contentType, Digest: digest, ByteLength: int64(len(data)),
		Content: string(data),
	})
	batch.byteCount += int64(len(data))
	return nil
}

func queueExportBinary(batch *modExportWriteBatch, revisionID, name string, data []byte) error {
	assetID := newExportID()
	extension := strings.TrimPrefix(strings.ToLower(path.Ext(name)), ".")
	batch.queue(`insert into catalog_import_binary_assets(id,revision_id,asset_path,asset_kind,sha256,byte_length,data) values($1,$2,$3,$4,$5,$6,$7)`, int64(len(data)), assetID, revisionID, name, extension, sha256Hex(data), len(data), data)
	if strings.Contains(strings.ToLower(name), "/structures/") || extension == "schem" || extension == "schematic" || extension == "litematic" {
		structureID := strings.TrimSuffix(strings.TrimPrefix(name, "data/"), path.Ext(name))
		batch.queue(`insert into catalog_import_structures(id,revision_id,structure_id,asset_path,source_format,template_blob_id) values($1,$2,$3,$4,$5,$6) on conflict(revision_id,structure_id) do nothing`, 0, newExportID(), revisionID, structureID, name, extension, assetID)
	}
	return nil
}

func inspectExportPNG(cfg ossConfigPayload, revisionID, uniqueID, name string, data []byte, resolver catalogResourceIdentityResolver) (modExportPNGMedia, error) {
	imageConfig, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || imageConfig.Width <= 0 || imageConfig.Height <= 0 || int64(imageConfig.Width)*int64(imageConfig.Height) > 100_000_000 {
		return modExportPNGMedia{}, fmt.Errorf("invalid or oversized PNG: %s", name)
	}
	digest := sha256Hex(data)
	return modExportPNGMedia{RevisionID: revisionID, AssetPath: name, ObjectKey: modExportResolvedMediaObjectKey(cfg.Prefix, uniqueID, revisionID, name, resolver), Digest: digest, ByteLength: int64(len(data)), Width: imageConfig.Width, Height: imageConfig.Height, Original: path.Base(name)}, nil
}

func modExportMediaObjectKey(prefix, projectUniqueID, revisionID, assetPath string) string {
	category := modExportMediaObjectCategory(projectUniqueID, revisionID, assetPath)
	return path.Join(ossObjectPrefix(prefix, category), modExportMediaObjectSuffix(assetPath))
}

func modExportResolvedMediaObjectKey(prefix, projectUniqueID, revisionID, assetPath string, resolver catalogResourceIdentityResolver) string {
	cleaned := sanitizeOSSAssetPath(assetPath)
	parts := strings.Split(cleaned, "/")
	if len(parts) >= 5 && parts[0] == "icons" {
		registry := parts[1]
		resourcePath := strings.TrimSuffix(path.Join(parts[4:]...), path.Ext(parts[len(parts)-1]))
		resolved := resolver.resolve(resourceKindForRegistry(registry), parts[3]+":"+resourcePath)
		if resolved.PublicID != "" {
			category := modExportMediaObjectCategory(projectUniqueID, revisionID, assetPath)
			return path.Join(ossObjectPrefix(prefix, category), resolved.PublicID+".png")
		}
	}
	return modExportMediaObjectKey(prefix, projectUniqueID, revisionID, assetPath)
}

func newModExportPNGUploadPool(ctx context.Context, client *aliyunoss.Client, bucket string) *modExportPNGUploadPool {
	return &modExportPNGUploadPool{ctx: ctx, client: client, bucket: bucket, semaphore: make(chan struct{}, maxExportPNGConcurrency)}
}

func (pool *modExportPNGUploadPool) submit(objectKey, name, digest string, data []byte) {
	pool.submitAsset(objectKey, name, digest, "image/png", data)
}

func (pool *modExportPNGUploadPool) submitAsset(objectKey, name, digest, contentType string, data []byte) {
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
		_, err := pool.client.PutObject(pool.ctx, &aliyunoss.PutObjectRequest{Bucket: aliyunoss.Ptr(pool.bucket), Key: aliyunoss.Ptr(objectKey), ContentType: aliyunoss.Ptr(contentType), ContentLength: aliyunoss.Ptr(int64(len(data))), Body: bytes.NewReader(data), Metadata: map[string]string{"sha256": digest}})
		if err != nil {
			pool.setError(fmt.Errorf("upload import asset %s: %w", name, err))
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

func persistExportPNGMedia(ctx context.Context, tx pgx.Tx, cfg ossConfigPayload, uniqueID string, uploaderID int64, source string, media []modExportPNGMedia) error {
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
			batch.Queue(`insert into oss_files(bucket,endpoint,region,object_key,category,source,original_name,source_original_name,content_type,size_bytes,source_size_bytes,sha256,uploader_id,status,scan_status) values($1,$2,$3,$4,$5,$6,$7,$7,'image/png',$8,$8,$9,$10,'active','clean') on conflict(object_key) do update set status='active',scan_status='clean',updated_at=now() returning id`, cfg.Bucket, cfg.displayEndpoint(), cfg.Region, item.ObjectKey, ossCategoryFromObjectKey(item.ObjectKey, cfg.Prefix), source, item.Original, item.ByteLength, item.Digest, nullableUserID(uploaderID))
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
	copied, err := tx.CopyFrom(ctx, pgx.Identifier{"catalog_import_media"}, []string{"revision_id", "asset_path", "media_kind", "oss_file_id", "sha256", "content_type", "byte_length", "width", "height", "has_alpha"}, pgx.CopyFromSlice(len(media), func(index int) ([]any, error) {
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

func sortedExportRevisionIDs(revisions map[string]string) []string {
	unique := make(map[string]struct{}, len(revisions))
	for _, revisionID := range revisions {
		if revisionID != "" {
			unique[revisionID] = struct{}{}
		}
	}
	result := make([]string, 0, len(unique))
	for revisionID := range unique {
		result = append(result, revisionID)
	}
	sort.Strings(result)
	return result
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
		// Import identifiers are visible outside the process. Continuing with a
		// timestamp fallback would make them predictable, so fail closed if the
		// operating system CSPRNG is unavailable.
		panic(fmt.Errorf("generate secure import identifier: %w", err))
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
		 from catalog_import_jobs j join mods m on m.id=j.mod_id where j.id=$1`, jobID,
	).Scan(&recipientID, &siteID, &modName, &skipped)
	if err != nil || recipientID <= 0 {
		return
	}
	code := "mod_import_success"
	values := map[string]string{"name": modName, "skipped": fmt.Sprintf("%d", skipped), "error": ""}
	if status == "partial" {
		code = "mod_import_partial"
	}
	if status == "failed" {
		code = "mod_import_failure"
		values["error"] = "未知错误"
		if failure != nil {
			values["error"] = failure.Error()
		}
	}
	s.sendTemplatedNotification(ctx, recipientID, code, values, map[string]any{
		"type": "mod_export_import", "jobId": jobID, "modSiteId": siteID, "status": status, "translationValuesSkipped": skipped,
	})
}
