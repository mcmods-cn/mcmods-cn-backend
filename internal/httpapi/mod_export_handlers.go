package httpapi

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

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
	ID                          string           `json:"id"`
	ModSiteID                   string           `json:"modSiteId"`
	PackageID                   string           `json:"packageId"`
	TargetVersionPublicID       string           `json:"targetVersionPublicId"`
	OverwriteExistingImportData bool             `json:"overwriteExistingImportData"`
	Status                      string           `json:"status"`
	Progress                    int              `json:"progress"`
	CurrentStage                string           `json:"currentStage"`
	ErrorCode                   string           `json:"errorCode"`
	ErrorDetail                 map[string]any   `json:"errorDetail"`
	Deduplicated                bool             `json:"deduplicated"`
	ReviewRequired              bool             `json:"reviewRequired"`
	ConfiguredModIDs            []string         `json:"configuredModids"`
	DetectedModIDs              []modIDCandidate `json:"detectedModids"`
	PrimaryDetectedModID        string           `json:"primaryDetectedModid"`
	MODIDConfirmationRequired   bool             `json:"modidConfirmationRequired"`
	MODIDAnalysisHash           string           `json:"modidAnalysisHash,omitempty"`
	CreatedAt                   time.Time        `json:"createdAt"`
	UpdatedAt                   time.Time        `json:"updatedAt"`
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
	uploadGuard func(context.Context, string, func(context.Context) error) error
	ctx         context.Context
	client      *aliyunoss.Client
	bucket      string
	semaphore   chan struct{}
	waitGroup   sync.WaitGroup
	mu          sync.Mutex
	firstErr    error
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
	if worker.queue != nil {
		// SubscribeTask registers the handler before it attempts a NATS
		// subscription. The PostgreSQL dispatcher can therefore execute the
		// same bounded handler locally while NATS or JetStream is unavailable.
		_ = worker.queue.SubscribeTask(modExportTaskCode, worker.handle)
	}
	return nil
}

func (worker *ModExportWorker) handle(ctx context.Context, raw []byte) error {
	var message modExportJobMessage
	if err := json.Unmarshal(raw, &message); err != nil || message.JobID == "" {
		return errors.New("invalid mod export import message")
	}
	return modExportDeliveryResult(worker.server.importModExportJob(ctx, message.JobID))
}

func modExportDeliveryResult(err error) error {
	if errors.Is(err, errModExportLeaseLost) {
		return nil
	}
	return err
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
	expectedObjectCategory := ossOwnerObjectCategory(expectedCategory, currentClaims(r).Subject)
	client, cfg, err := s.ossClient(r.Context())
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	if request.ObjectKey == "" ||
		!isAllowedObjectKey(request.ObjectKey, ossObjectPrefix(cfg.Prefix, expectedObjectCategory)) ||
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
			"storageUrl": ossStoredObjectURL(cfg, request.ObjectKey),
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
		"storageUrl": ossStoredObjectURL(cfg, request.ObjectKey),
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
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "创建导入任务失败")
		return
	}
	defer tx.Rollback(r.Context())
	var archiveName, objectKey, archiveHash, source, category string
	var archiveFileID, archiveSize int64
	err = tx.QueryRow(
		r.Context(),
		`select id,original_name, object_key, sha256, size_bytes, source, category
		 from oss_files where public_id=$1 and uploader_id=$2 and status='active' for key share`,
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
	packageID, err = insertCatalogImportPackage(r.Context(), tx, catalogImportPackageInput{
		ID: packageID, SHA256: archiveHash, ArchiveFileID: archiveFileID, ArchiveName: archiveName,
		Loader: "unknown", Manifest: json.RawMessage(`{}`), Profile: "all", UploadedBy: claims.Subject,
	})
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
		if err = enqueueModExportAttemptTx(r.Context(), tx, jobID, "mod.catalog_import.requested"); err != nil {
			writeError(w, http.StatusInternalServerError, "保存导入队列事件失败")
			return
		}
		if err = insertModExportImportActivityTx(r.Context(), tx, claims.Subject, request.TargetVersionPublicID); err != nil {
			writeError(w, http.StatusInternalServerError, "记录导入用户行为失败")
			return
		}
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "提交导入任务失败")
		return
	}
	response, err := s.modExportJobByID(r.Context(), jobID, identity.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取导入任务失败")
		return
	}
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
	job, err := s.modExportJobByIDForCreator(r.Context(), jobID, identity.ID, currentClaims(r).Subject)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "导入任务不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取导入任务失败")
		return
	}
	if stalled, markErr := s.markStalledModExportJob(r.Context(), jobID, identity.ID); markErr != nil {
		writeError(w, http.StatusInternalServerError, "Failed to inspect the import job")
		return
	} else if stalled {
		s.notifyModExportResult(r.Context(), jobID, "failed", errors.New("import worker stopped responding"))
		job, err = s.modExportJobByIDForCreator(r.Context(), jobID, identity.ID, currentClaims(r).Subject)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "读取导入任务失败")
			return
		}
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
	jobID, err := s.activeModExportJobID(r.Context(), identity.ID, targetVersionPublicID, currentClaims(r).Subject)
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
	job, err := s.modExportJobByIDForCreator(r.Context(), jobID, identity.ID, currentClaims(r).Subject)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to read the active import job")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"job": job})
}

func (s *Server) activeModExportJobID(ctx context.Context, modID int64, targetVersionPublicID string, creatorID int64) (string, error) {
	var jobID string
	err := s.db.QueryRow(ctx, `select job.id
		from catalog_import_jobs job
		join catalog_import_packages package on package.id=job.package_id
		join mod_content_versions version on version.id=job.target_version_id
		where job.mod_id=$1 and version.public_id=$2 and version.status='active'
		  and job.created_by=$3
		  and package.profile='all'
		  and job.status in ('queued','validating','confirmation_required','importing')
		order by job.created_at desc,job.id desc limit 1`,
		modID, targetVersionPublicID, creatorID).Scan(&jobID)
	return jobID, err
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
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "取消导入任务失败")
		return
	}
	defer tx.Rollback(r.Context())
	jobID := r.PathValue("jobId")
	var runToken string
	if err = tx.QueryRow(r.Context(), `select run_token from catalog_import_jobs
		where id=$1 and mod_id=$2 and status in ('queued','validating','confirmation_required','importing')
		for update`, jobID, identity.ID).Scan(&runToken); errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusConflict, "任务已经开始或不存在")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "取消导入任务失败")
		return
	}
	if err = s.compensateCatalogImportArtifactsTx(
		r.Context(), tx, jobID, runToken, "catalog-import-cancelled",
	); err != nil {
		writeError(w, http.StatusInternalServerError, "取消导入任务失败")
		return
	}
	if runToken != "" {
		if _, err = tx.Exec(r.Context(), `delete from catalog_import_revisions
			where job_id=$1 and status='staging' and import_run_token=$2`, jobID, runToken); err != nil {
			writeError(w, http.StatusInternalServerError, "取消导入任务失败")
			return
		}
	}
	tag, err := tx.Exec(r.Context(), `update catalog_import_jobs
		set status='cancelled',progress=least(progress,99),current_stage='cancelled',
			finished_at=now(),heartbeat_at=now(),run_token='',updated_at=now()
		where id=$1 and mod_id=$2 and status in ('queued','validating','confirmation_required','importing')`,
		jobID, identity.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "取消导入任务失败")
		return
	}
	if tag.RowsAffected() == 0 {
		writeError(w, http.StatusConflict, "任务已经开始或不存在")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "取消导入任务失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "cancelled"})
}

func (s *Server) modExportJobByID(ctx context.Context, jobID string, modID int64) (modExportJobResponse, error) {
	return s.modExportJobByIDWithCreator(ctx, jobID, modID, nil)
}

func (s *Server) modExportJobByIDForCreator(ctx context.Context, jobID string, modID, creatorID int64) (modExportJobResponse, error) {
	return s.modExportJobByIDWithCreator(ctx, jobID, modID, &creatorID)
}

func (s *Server) modExportJobByIDWithCreator(ctx context.Context, jobID string, modID int64, creatorID *int64) (modExportJobResponse, error) {
	var result modExportJobResponse
	var detail []byte
	var configuredMODIDs []string
	var detectedMODIDs []byte
	err := s.db.QueryRow(
		ctx,
		`select j.id,m.slug,j.package_id,version.public_id,j.overwrite_existing,j.status,j.progress,j.current_stage,j.error_code,j.error_detail,j.created_at,j.updated_at,
		 j.configured_modids,j.detected_modids,j.primary_detected_modid,j.modid_confirmation_required,j.modid_analysis_hash,
		 exists(select 1 from catalog_import_revisions r where r.job_id=j.id and r.status in ('ready','partial') and not r.is_active)
		 from catalog_import_jobs j join mods m on m.id=j.mod_id
		 join mod_content_versions version on version.id=j.target_version_id
		 where j.id=$1 and j.mod_id=$2 and ($3::bigint is null or j.created_by=$3)`,
		jobID, modID, creatorID,
	).Scan(&result.ID, &result.ModSiteID, &result.PackageID, &result.TargetVersionPublicID, &result.OverwriteExistingImportData, &result.Status, &result.Progress, &result.CurrentStage, &result.ErrorCode, &detail, &result.CreatedAt, &result.UpdatedAt,
		&configuredMODIDs, &detectedMODIDs, &result.PrimaryDetectedModID, &result.MODIDConfirmationRequired, &result.MODIDAnalysisHash, &result.ReviewRequired)
	if err != nil {
		return result, err
	}
	result.ConfiguredModIDs = configuredMODIDs
	if result.ConfiguredModIDs == nil {
		return result, fmt.Errorf("configured mod IDs for job %s must be a PostgreSQL text array", jobID)
	}
	if err = decodeModExportJSON(detectedMODIDs, &result.DetectedModIDs, "detected mod IDs for job "+jobID); err != nil || result.DetectedModIDs == nil {
		if err == nil {
			err = fmt.Errorf("detected mod IDs for job %s must be a JSON array", jobID)
		}
		return result, err
	}
	if len(detail) > 0 {
		if err = decodeModExportJSON(detail, &result.ErrorDetail, "error detail for job "+jobID); err != nil || result.ErrorDetail == nil {
			if err == nil {
				err = fmt.Errorf("error detail for job %s must be a JSON object", jobID)
			}
			return result, err
		}
	}
	if result.ErrorDetail == nil {
		result.ErrorDetail = map[string]any{}
	}
	return result, nil
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
		if cleanupErr := s.cleanupModExportStaging(cleanupContext, jobID, packageID, modID, runToken); cleanupErr != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("compensate catalog import artifacts: %w", cleanupErr))
		}
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
	if err = markCatalogImportPackageContentVerified(ctx, s.db, packageID, sourceFileID, expectedHash); err != nil {
		return err
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
	paused, err := s.pauseCatalogImportForMODIDConfirmation(ctx, jobID, runToken, modID, expectedHash, importNamespaceCounts(namespaces))
	if err != nil {
		return err
	}
	if paused {
		return nil
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
	pngPool.uploadGuard = s.catalogImportUploadGuard(jobID, runToken)
	defer func() {
		cancelUploads()
		_ = pngPool.wait()
	}()
	pngMedia := make([]modExportPNGMedia, 0)
	localeBundles := make([]modExportLocaleBundleMedia, 0)
	scheduledObjects := make(map[string]struct{})
	artifactFileIDs := make(map[string]int64)
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
		pendingUploads := make([]modExportUploadAsset, 0)
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
					if _, scheduled := scheduledObjects[bundle.ObjectKey]; !scheduled {
						scheduledObjects[bundle.ObjectKey] = struct{}{}
						pendingUploads = append(pendingUploads, modExportUploadAsset{
							ObjectKey: bundle.ObjectKey, Original: bundle.Original, Digest: bundle.Digest,
							ContentType: bundle.ContentType, ByteLength: bundle.ByteLength, Data: compressed,
						})
					}
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
				if media, err = inspectExportPNG(ctx, cfg, revisionID, uniqueID, name, data, resourceResolver); err != nil {
					return err
				}
				pngMedia = append(pngMedia, media)
				if _, scheduled := scheduledObjects[media.ObjectKey]; !scheduled {
					scheduledObjects[media.ObjectKey] = struct{}{}
					pendingUploads = append(pendingUploads, modExportUploadAsset{
						ObjectKey: media.ObjectKey, Original: media.Original, Digest: media.Digest,
						ContentType: "image/png", ByteLength: media.ByteLength, Data: data,
					})
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
		if len(pendingUploads) > 0 {
			registered, registerErr := s.registerCatalogImportArtifacts(
				ctx, jobID, runToken, cfg, createdBy, "mcmods_exporter", pendingUploads,
			)
			if registerErr != nil {
				return registerErr
			}
			for objectKey, fileID := range registered {
				artifactFileIDs[objectKey] = fileID
			}
			for _, upload := range pendingUploads {
				pngPool.submitAsset(upload.ObjectKey, upload.Original, upload.Digest, upload.ContentType, upload.Data)
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
	if err = bindCatalogImportArtifactFileIDs(pngMedia, localeBundles, artifactFileIDs); err != nil {
		return err
	}
	for start := 0; start < len(pngMedia); start += maxExportWriteBatchRows {
		end := min(start+maxExportWriteBatchRows, len(pngMedia))
		if err = s.runModExportTransaction(ctx, func(tx pgx.Tx) error {
			return persistExportPNGMedia(ctx, tx, pngMedia[start:end])
		}); err != nil {
			return err
		}
	}
	if err = s.runModExportTransaction(ctx, func(tx pgx.Tx) error {
		return persistExportLocaleBundles(ctx, tx, localeBundles)
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
		if lockErr := lockCatalogImportAttemptTx(ctx, tx, jobID, runToken); lockErr != nil {
			return lockErr
		}
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
			if versionErr := bumpCatalogDatasetVersionTx(ctx, tx); versionErr != nil {
				return versionErr
			}
		}
		if statsErr := refreshModExportRevisionStats(ctx, tx, revisionIDs); statsErr != nil {
			return statsErr
		}
		if artifactErr := activateCatalogImportArtifactsTx(ctx, tx, jobID, runToken); artifactErr != nil {
			return artifactErr
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
		if _, updateErr = tx.Exec(ctx, `update catalog_import_packages set imported_at=now() where id=$1`, packageID); updateErr != nil {
			return updateErr
		}
		return s.tombstoneOSSFileTx(ctx, tx, sourceFileID, "mod-export-import-consumed")
	})
	if err != nil {
		return err
	}
	s.notifyModExportResult(context.Background(), jobID, jobStatus, nil)
	return nil
}
