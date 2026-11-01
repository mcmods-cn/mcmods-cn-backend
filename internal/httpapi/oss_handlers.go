package httpapi

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	aliyunoss "github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
	"github.com/jackc/pgx/v5"
)

var errOSSConfigurationUnavailable = errors.New("OSS configuration is unavailable")

const maxOSSUploadBytes = 2 << 30

const (
	ossReportEvidenceScope = "report_evidence"
	maxReportEvidenceBytes = int64(25 << 20)
)

const (
	ossDownloadModePresigned      = "oss_presigned"
	maxOSSDownloadURLTTLMinutes   = 60
	ossProjectIntroCategory       = "project/intro"
	ossProjectDownloadCategory    = "project/download"
	ossModExportScopePrefix       = "mod_export:"
	ossModCatalogScopePrefix      = "mod_catalog:"
	ossProjectDownloadScopePrefix = "project_download:"
)

var defaultOSSAllowedExtensions = []string{
	".jpg", ".jpeg", ".png", ".gif", ".webp", ".bmp", ".svg",
	".mp4", ".webm", ".mov", ".avi",
	".pdf", ".doc", ".docx", ".xls", ".xlsx", ".ppt", ".pptx", ".md",
	".txt", ".log", ".json", ".nbt", ".schem", ".schematic", ".litematic",
	".zip", ".rar", ".7z", ".jar", ".gz", ".tar",
}

type ossConfigPayload struct {
	Enabled               bool     `json:"enabled"`
	Region                string   `json:"region"`
	Endpoint              string   `json:"endpoint"`
	PublicEndpoint        string   `json:"publicEndpoint"`
	Bucket                string   `json:"bucket"`
	AccessKeyID           string   `json:"accessKeyId"`
	AccessKeySecret       string   `json:"accessKeySecret,omitempty"`
	SecurityToken         string   `json:"securityToken,omitempty"`
	HasAccessKeySecret    bool     `json:"hasAccessKeySecret,omitempty"`
	HasSecurityToken      bool     `json:"hasSecurityToken,omitempty"`
	UseCName              bool     `json:"useCName"`
	Prefix                string   `json:"prefix"`
	DownloadURLTTLMinutes int      `json:"downloadUrlTtlMinutes"`
	DownloadURLMode       string   `json:"downloadUrlMode"`
	AllowedExtensions     []string `json:"allowedExtensions"`
	BucketAccessPolicy    string   `json:"bucketAccessPolicy,omitempty"`
	TemporaryDownload     string   `json:"temporaryDownloadPolicy,omitempty"`
}

type ossPresignRequest struct {
	ObjectKey      string `json:"objectKey"`
	ExpiresMinutes int    `json:"expiresMinutes"`
}

type ossDirectUploadRequest struct {
	OriginalName    string `json:"originalName"`
	ContentType     string `json:"contentType"`
	SizeBytes       int64  `json:"sizeBytes"`
	SHA256          string `json:"sha256"`
	Category        string `json:"category"`
	Source          string `json:"source"`
	Prefix          string `json:"prefix"`
	ProjectUniqueID string `json:"projectUniqueId"`
	ProjectType     string `json:"projectType"`
	ContentPublicID string `json:"contentPublicId"`
	ExpiresMinutes  int    `json:"expiresMinutes"`
}

type ossCompleteUploadRequest struct {
	ObjectKey         string `json:"objectKey"`
	OriginalName      string `json:"originalName"`
	ContentType       string `json:"contentType"`
	SizeBytes         int64  `json:"sizeBytes"`
	SHA256            string `json:"sha256"`
	Category          string `json:"category"`
	Source            string `json:"source"`
	MultipartUploadID string `json:"multipartUploadId"`
	MultipartAction   string `json:"multipartAction"`
}

func (s *Server) getOSSConfig(w http.ResponseWriter, r *http.Request) {
	payload, err := s.ossConfigFromSettingsStrictWithQueryer(r.Context(), s.db)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "读取 OSS 配置失败")
		return
	}
	writeJSON(w, http.StatusOK, redactOSSConfig(payload))
}

func (s *Server) updateOSSConfig(w http.ResponseWriter, r *http.Request) {
	var payload ossConfigPayload
	if err := decodeJSON(r, &payload); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式不正确")
		return
	}
	payload.Region = strings.TrimSpace(payload.Region)
	payload.Endpoint = normalizeOSSEndpoint(payload.Endpoint)
	payload.PublicEndpoint = normalizeOSSEndpoint(payload.PublicEndpoint)
	payload.Bucket = strings.TrimSpace(payload.Bucket)
	payload.AccessKeyID = strings.TrimSpace(payload.AccessKeyID)
	payload.AccessKeySecret = strings.TrimSpace(payload.AccessKeySecret)
	payload.SecurityToken = strings.TrimSpace(payload.SecurityToken)
	payload.Prefix = normalizeObjectPrefix(payload.Prefix)
	payload.AllowedExtensions = normalizeAllowedExtensions(payload.AllowedExtensions)
	if payload.Endpoint == "" && payload.Region != "" {
		payload.Endpoint = defaultOSSEndpoint(payload.Region)
	}
	payload = normalizeOSSConfig(payload)
	if payload.DownloadURLTTLMinutes <= 0 {
		payload.DownloadURLTTLMinutes = 10
	}
	if payload.DownloadURLTTLMinutes > maxOSSDownloadURLTTLMinutes {
		payload.DownloadURLTTLMinutes = maxOSSDownloadURLTTLMinutes
	}

	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "读取 OSS 配置失败，原配置未更改")
		return
	}
	defer tx.Rollback(r.Context())
	// Serialize masked saves, including the initially absent setting, so an
	// omitted secret cannot restore a credential replaced by another admin.
	if _, err = tx.Exec(r.Context(), `select pg_advisory_xact_lock(hashtextextended('system-settings:oss.aliyun',0))`); err != nil {
		writeError(w, http.StatusServiceUnavailable, "读取 OSS 配置失败，原配置未更改")
		return
	}
	current, err := s.ossConfigFromSettingsStrictWithQueryer(r.Context(), tx)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "读取 OSS 配置失败，原配置未更改")
		return
	}
	if payload.AccessKeySecret == "" {
		payload.AccessKeySecret = current.AccessKeySecret
	}
	if payload.SecurityToken == "" {
		payload.SecurityToken = current.SecurityToken
	}
	if payload.Enabled && (payload.Region == "" || payload.Endpoint == "" || payload.Bucket == "" || payload.AccessKeyID == "" || payload.AccessKeySecret == "") {
		writeError(w, http.StatusBadRequest, "启用 OSS 前需要填写 Region、Endpoint、Bucket、AccessKeyId 和 AccessKeySecret")
		return
	}
	if payload.Enabled && requiresSecurityToken(payload.AccessKeyID) && payload.SecurityToken == "" {
		writeError(w, http.StatusBadRequest, "当前 AccessKey 是 STS 临时凭证，需要同时填写 SecurityToken")
		return
	}

	raw, err := s.sealSystemSetting(payload)
	if err != nil {
		writeError(w, http.StatusBadRequest, "OSS 配置格式不正确")
		return
	}
	_, err = tx.Exec(
		r.Context(),
		`insert into system_settings (key, value, updated_by, updated_at)
		 values ('oss.aliyun', $1::jsonb, $2, now())
		 on conflict (key) do update
		 set value = excluded.value, updated_by = excluded.updated_by, updated_at = now()`,
		raw,
		currentClaims(r).Subject,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "保存 OSS 配置失败")
		return
	}
	if err = enqueueRequestActivityTx(r, tx); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to persist OSS configuration activity")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "保存 OSS 配置失败")
		return
	}
	s.writeAppLog(r.Context(), "admin_operation", "info", "update_oss_config", payload.Bucket, currentClaims(r).Subject, r, http.StatusOK, 0, map[string]any{"enabled": payload.Enabled})
	writeJSON(w, http.StatusOK, redactOSSConfig(payload))
}

func (s *Server) createOSSDirectUpload(w http.ResponseWriter, r *http.Request) {
	s.createOSSDirectUploadWithScope(w, r, "")
}

func (s *Server) createUserOSSDirectUpload(w http.ResponseWriter, r *http.Request) {
	s.createOSSDirectUploadWithScope(w, r, "user")
}

func (s *Server) createOSSDirectUploadWithScope(w http.ResponseWriter, r *http.Request, scope string) {
	client, cfg, err := s.ossClient(r.Context())
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	var req ossDirectUploadRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式不正确")
		return
	}
	req.OriginalName = strings.TrimSpace(req.OriginalName)
	if req.OriginalName == "" {
		writeError(w, http.StatusBadRequest, "请选择要上传的文件")
		return
	}
	if req.SizeBytes <= 0 || req.SizeBytes > maxOSSUploadBytes {
		writeError(w, http.StatusBadRequest, "上传内容过大或文件大小不正确")
		return
	}
	req.SHA256 = normalizeSHA256(req.SHA256)
	if req.SHA256 == "" {
		writeError(w, http.StatusBadRequest, "缺少文件 SHA-256 哈希")
		return
	}
	ext := strings.ToLower(filepath.Ext(req.OriginalName))
	blueprintUpload := scope == "user" && isBlueprintExtension(ext)
	if blueprintUpload && (req.SizeBytes <= 0 || req.SizeBytes > maxBlueprintSourceBytes) {
		writeAPIError(w, http.StatusRequestEntityTooLarge, "BLUEPRINT_SOURCE_SIZE_LIMIT", "blueprint source exceeds the processing size limit", 0, nil)
		return
	}
	modExportUniqueID := strings.TrimPrefix(scope, ossModExportScopePrefix)
	isModExport := modExportUniqueID != scope && modExportUniqueID != ""
	modCatalogUniqueID := strings.TrimPrefix(scope, ossModCatalogScopePrefix)
	isModCatalog := modCatalogUniqueID != scope && modCatalogUniqueID != ""
	projectDownloadType, projectDownloadID, isProjectDownload := parseOSSProjectDownloadScope(scope)
	isReportEvidence := scope == ossReportEvidenceScope
	if isReportEvidence && (req.SizeBytes > maxReportEvidenceBytes || !reportEvidenceExtensionAllowed(ext)) {
		writeError(w, http.StatusBadRequest, "举报附件类型不支持或超过 25MB")
		return
	}
	if (isModExport && ext != ".zip") || (isModCatalog && ext != ".json") || (isProjectDownload && !projectFileExtensionAllowed(projectDownloadType, ext)) || (!isReportEvidence && !isModExport && !isModCatalog && !isProjectDownload && !allowedUploadExtension(ext, cfg.AllowedExtensions)) {
		writeError(w, http.StatusBadRequest, "当前文件类型不允许上传")
		return
	}
	rawCategory := strings.TrimSpace(req.Category)
	category := normalizeObjectSegment(rawCategory)
	if category == "" {
		category = "misc"
	}
	source := strings.TrimSpace(req.Source)
	isCatalogImageUpload := scope == "user" && requiresSynchronousCatalogImageValidation(source)
	coverBlueprintID, coverPublicID := int64(0), ""
	if parsedCoverPublicID, ok := blueprintCoverPublicID(source); scope == "user" && ok {
		coverPublicID = parsedCoverPublicID
		if err := s.db.QueryRow(r.Context(), `select id from blueprints where public_id=$1 and owner_id=$2 and status<>'deleted'`, coverPublicID, currentClaims(r).Subject).Scan(&coverBlueprintID); err != nil {
			writeError(w, http.StatusForbidden, "没有权限上传该蓝图的封面")
			return
		}
	}
	if isModExport {
		source = "mcmods_exporter"
	} else if isModCatalog {
		source = normalizeEmbeddedIconImportSource(source)
		if source == "" {
			writeError(w, http.StatusBadRequest, "unsupported catalog importer")
			return
		}
	} else if isProjectDownload {
		source = "project_download"
	} else if isReportEvidence {
		source = "report_evidence"
	}
	objectPrefix := ossRoot(cfg.Prefix)
	if requestedPrefix := normalizeObjectPrefix(req.Prefix); requestedPrefix != "" {
		objectPrefix = requestedPrefix
	}
	objectCategory := category
	if scope == "user" {
		category, err = s.resolveUserOSSUploadCategory(r, category, source)
		if err != nil {
			writeError(w, http.StatusForbidden, err.Error())
			return
		}
		objectPrefix = ossRoot(cfg.Prefix)
		objectCategory = category
		if coverBlueprintID > 0 {
			category = ossBlueprintTextCategory(coverPublicID, "cover")
			objectCategory = category
		}
	} else if isModExport {
		category = ossModImportCategory(modExportUniqueID, "mcmods-exporter", "packages")
		objectCategory = ossOwnerObjectCategory(category, currentClaims(r).Subject)
		objectPrefix = ossRoot(cfg.Prefix)
	} else if isModCatalog {
		category = ossModImportCategory(modCatalogUniqueID, source, "catalog")
		objectCategory = category
		objectPrefix = ossRoot(cfg.Prefix)
	} else if isProjectDownload {
		category = ossProjectReleaseCategory(projectDownloadType, projectDownloadID)
		objectCategory = ossOwnerObjectCategory(category, currentClaims(r).Subject)
		objectPrefix = ossRoot(cfg.Prefix)
	} else if isReportEvidence {
		category = path.Join("moderation", "report-evidence", strconv.FormatInt(currentClaims(r).Subject, 10))
		objectCategory = category
		objectPrefix = ossRoot(cfg.Prefix)
	} else if rawCategory == ossProjectIntroCategory || category == "project_intro" || category == "projectintro" {
		projectType := defaultString(normalizeObjectSegment(req.ProjectType), "mod")
		contentPublicID := defaultString(normalizeObjectSegment(req.ContentPublicID), req.ProjectUniqueID)
		category = ossProjectTextCategory(projectType, req.ProjectUniqueID, contentPublicID)
		objectCategory = category
		objectPrefix = ossRoot(cfg.Prefix)
	} else if rawCategory == ossProjectDownloadCategory || category == "project_download" || category == "projectdownload" {
		category = ossProjectReleaseCategory(defaultString(normalizeObjectSegment(req.ProjectType), "mod"), req.ProjectUniqueID)
		objectCategory = category
		objectPrefix = ossRoot(cfg.Prefix)
	}
	contentType := strings.TrimSpace(req.ContentType)
	if contentType == "" {
		contentType = mime.TypeByExtension(ext)
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	if blueprintUpload {
		blueprint, reuseErr := s.reusableBlueprintByHash(r.Context(), req.SHA256, req.SizeBytes, currentClaims(r).Subject)
		if reuseErr != nil {
			writeError(w, http.StatusInternalServerError, "读取可复用蓝图失败")
			return
		}
		if blueprint != nil {
			if _, activityErr := s.commitOSSReuseTicket(r, 0, 0, "", "", false); activityErr != nil {
				writeError(w, http.StatusInternalServerError, "failed to persist reused OSS upload ticket")
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"uploadRequired": false,
				"blueprint":      blueprint,
				"blueprintId":    blueprint["id"],
			})
			return
		}
	}
	var existing map[string]any
	var exists bool
	if isProjectDownload || isReportEvidence {
		// Project files keep their own immutable object path even when another
		// project happens to upload the same JAR.
		exists = false
	} else {
		lookup := ossFileHashLookup{
			Category:       category,
			Source:         source,
			ScanStatuses:   []string{"pending", "clean", "trusted_generated"},
			RequireRaster:  isCatalogImageUpload,
			RequireTrusted: isCatalogImageUpload,
			// A policy-cancelled attempt requires a genuinely new upload. Keep
			// its old immutable source for audit, but do not deduplicate back to it.
			ExcludePolicyCancelledSources: isModExport || isModCatalog,
		}
		if scope == "user" || isModExport || isModCatalog || isReportEvidence {
			uploaderID := currentClaims(r).Subject
			lookup.UploaderID = &uploaderID
		}
		existing, exists, err = s.findExistingOSSFileByHashExact(r.Context(), req.SHA256, req.SizeBytes, lookup)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "读取可复用 OSS 文件失败")
			return
		}
	}
	var existingInternalID int64
	if exists && (blueprintUpload || coverBlueprintID > 0) {
		if coverBlueprintID > 0 {
			var file trustedRasterOSSFile
			file, err = resolveTrustedRasterOSSFilePublicID(r.Context(), s.db, fmt.Sprint(existing["id"]), ossRasterBindingScope{UploaderID: currentClaims(r).Subject})
			existingInternalID = file.ID
		} else {
			existingInternalID, err = s.resolveActiveOSSFileInternalIDForUploader(r.Context(), fmt.Sprint(existing["id"]), currentClaims(r).Subject)
		}
		if errors.Is(err, pgx.ErrNoRows) {
			exists = false
		} else if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to resolve existing OSS file")
			return
		}
	}
	if exists {
		objectKey := fmt.Sprint(existing["objectKey"])
		access, accessErr := s.resolveOSSObjectAccessWithConfig(r.Context(), cfg, objectKey, ossObjectAccessOptions{})
		if accessErr != nil {
			writeError(w, http.StatusBadGateway, "failed to generate OSS access URL")
			return
		}
		response := map[string]any{
			"uploadRequired": false,
			"file":           existing,
			"id":             existing["id"],
			"bucket":         existing["bucket"],
			"objectKey":      existing["objectKey"],
			"category":       existing["category"],
			"source":         existing["source"],
			"originalName":   existing["originalName"],
			"contentType":    existing["contentType"],
			"sizeBytes":      existing["sizeBytes"],
			"sha256":         existing["sha256"],
			"accessUrl":      access.URL,
			"storageUrl":     ossStoredObjectURL(cfg, objectKey),
		}
		blueprint, commitErr := s.commitOSSReuseTicket(r, existingInternalID, coverBlueprintID, fmt.Sprint(existing["id"]), objectKey, blueprintUpload)
		if commitErr != nil {
			if errors.Is(commitErr, errReusableBlueprintCoverChanged) {
				writeError(w, http.StatusConflict, "蓝图或封面文件状态已变化，请重新提交")
				return
			}
			if errors.Is(commitErr, errOSSReuseCoverBinding) {
				writeError(w, http.StatusInternalServerError, "保存蓝图封面失败")
				return
			}
			writeBlueprintUploadAssociationError(w, commitErr, "failed to persist reused OSS upload ticket")
			return
		}
		if blueprint != nil {
			response["blueprint"] = blueprint
			response["blueprintId"] = blueprint["id"]
		}
		if coverPublicID != "" {
			response["blueprintId"] = coverPublicID
		}
		writeJSON(w, http.StatusOK, response)
		return
	}
	var quotaLimits ossUserQuotaLimits
	deferStoredSizeCheck := false
	if scope == "user" {
		quotaLimits, err = ossUserQuotaLimitsForRequest(r)
		if err == nil {
			err = quotaLimits.validateSingle(req.SizeBytes)
		}
		if err != nil {
			writeOSSUserQuotaError(w, err)
			return
		}
		deferStoredSizeCheck = shouldPersistMarkdownImageAsWebP(req.OriginalName, contentType, source, category) ||
			isModResourceRenderUploadSource(source)
	}
	expiresMinutes := req.ExpiresMinutes
	if expiresMinutes <= 0 {
		expiresMinutes = 10
	}
	if expiresMinutes > 60 {
		expiresMinutes = 60
	}
	expires := time.Duration(expiresMinutes) * time.Minute
	uploadExpiresAt := time.Now().Add(expires)
	s.issueOSSUploadTicket(w, r, client, cfg, req, scope, category, source, objectPrefix, objectCategory, contentType, uploadExpiresAt, expires, blueprintUpload, quotaLimits, deferStoredSizeCheck)
}

var errReusableBlueprintCoverChanged = errors.New("reusable blueprint cover changed")

func (s *Server) bindReusableBlueprintCover(ctx context.Context, blueprintID, ownerID int64, filePublicID, objectKey string) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = bindReusableBlueprintCoverTx(ctx, tx, blueprintID, ownerID, filePublicID, objectKey); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func bindReusableBlueprintCoverTx(ctx context.Context, tx pgx.Tx, blueprintID, ownerID int64, filePublicID, objectKey string) error {
	var err error
	var locked int64
	if err = tx.QueryRow(ctx, `select id from blueprints where id=$1 and owner_id=$2 and status<>'deleted' for update`, blueprintID, ownerID).Scan(&locked); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return errReusableBlueprintCoverChanged
		}
		return err
	}
	file, err := resolveTrustedRasterOSSFilePublicID(ctx, tx, filePublicID, ossRasterBindingScope{UploaderID: ownerID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, errOSSFileNotBindable) {
			return errReusableBlueprintCoverChanged
		}
		return err
	}
	if file.ObjectKey != objectKey {
		return errReusableBlueprintCoverChanged
	}
	command, err := tx.Exec(ctx, `update blueprints set cover_file_id=$2,cover_object_key=$3,updated_at=now() where id=$1`, locked, file.ID, file.ObjectKey)
	if err != nil {
		return err
	}
	if command.RowsAffected() != 1 {
		return errReusableBlueprintCoverChanged
	}
	return nil
}

func (s *Server) completeOSSDirectUpload(w http.ResponseWriter, r *http.Request) {
	s.completeOSSDirectUploadWithScope(w, r, "")
}

func (s *Server) completeUserOSSDirectUpload(w http.ResponseWriter, r *http.Request) {
	s.completeOSSDirectUploadWithScope(w, r, "user")
}

func (s *Server) completeOSSDirectUploadWithScope(w http.ResponseWriter, r *http.Request, scope string) {
	client, cfg, err := s.ossClient(r.Context())
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	var req ossCompleteUploadRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式不正确")
		return
	}
	req.ObjectKey = strings.TrimSpace(req.ObjectKey)
	req.OriginalName = strings.TrimSpace(req.OriginalName)
	req.ContentType = strings.TrimSpace(req.ContentType)
	req.SHA256 = normalizeSHA256(req.SHA256)
	req.MultipartUploadID = strings.TrimSpace(req.MultipartUploadID)
	req.MultipartAction = strings.ToLower(strings.TrimSpace(req.MultipartAction))
	rawCategory := strings.TrimSpace(req.Category)
	req.Category = normalizeObjectSegment(rawCategory)
	req.Source = strings.TrimSpace(req.Source)
	modExportUniqueID := strings.TrimPrefix(scope, ossModExportScopePrefix)
	isModExport := modExportUniqueID != scope && modExportUniqueID != ""
	modCatalogUniqueID := strings.TrimPrefix(scope, ossModCatalogScopePrefix)
	isModCatalog := modCatalogUniqueID != scope && modCatalogUniqueID != ""
	projectDownloadType, projectDownloadID, isProjectDownload := parseOSSProjectDownloadScope(scope)
	isReportEvidence := scope == ossReportEvidenceScope
	if isModExport {
		req.Source = "mcmods_exporter"
	} else if isModCatalog {
		req.Source = normalizeEmbeddedIconImportSource(req.Source)
		if req.Source == "" {
			writeError(w, http.StatusBadRequest, "unsupported catalog importer")
			return
		}
	} else if isProjectDownload {
		req.Source = "project_download"
	} else if isReportEvidence {
		req.Source = "report_evidence"
	}
	if err = s.canonicalOSSCompletedUploadAlias(r.Context(), cfg, currentClaims(r).Subject, &req); err != nil {
		if errors.Is(err, errOSSSealedReplayIdentity) {
			writeError(w, http.StatusBadRequest, "上传记录无效")
		} else {
			writeError(w, http.StatusInternalServerError, "读取上传记录失败")
		}
		return
	}
	if req.ObjectKey == "" || !isAllowedObjectKey(req.ObjectKey, cfg.Prefix) {
		writeError(w, http.StatusBadRequest, "OSS ObjectKey 不合法")
		return
	}
	if req.SHA256 == "" {
		writeError(w, http.StatusBadRequest, "缺少文件 SHA-256 哈希")
		return
	}
	if req.OriginalName == "" {
		req.OriginalName = path.Base(req.ObjectKey)
	}
	ext := strings.ToLower(filepath.Ext(req.OriginalName))
	if isReportEvidence && (req.SizeBytes > maxReportEvidenceBytes || !reportEvidenceExtensionAllowed(ext)) {
		writeError(w, http.StatusBadRequest, "举报附件类型不支持或超过 25MB")
		return
	}
	if (isModExport && ext != ".zip") || (isModCatalog && ext != ".json") || (isProjectDownload && !projectFileExtensionAllowed(projectDownloadType, ext)) || (!isReportEvidence && !isModExport && !isModCatalog && !isProjectDownload && !allowedUploadExtension(ext, cfg.AllowedExtensions)) {
		writeError(w, http.StatusBadRequest, "当前文件类型不允许上传")
		return
	}
	if req.Category == "" {
		req.Category = "misc"
	}
	if scope == "user" {
		blueprintPrefix := path.Join(ossRoot(cfg.Prefix), ossProjectDirectory, normalizeOSSProjectKind("blueprint"))
		isBlueprintObject := isAllowedObjectKey(req.ObjectKey, blueprintPrefix) && (s.userOwnsPendingBlueprintObject(r.Context(), currentClaims(r).Subject, req.ObjectKey) || s.userOwnsBlueprintObject(r.Context(), currentClaims(r).Subject, req.ObjectKey))
		// A successful cancellation removes its pending blueprint. The immutable
		// uploader-bound session still authorizes an idempotent abort response.
		if !isBlueprintObject && isAllowedObjectKey(req.ObjectKey, blueprintPrefix) && req.MultipartAction == "abort" && req.MultipartUploadID != "" {
			isBlueprintObject, err = s.ownsMultipartBlueprintAbort(r.Context(), currentClaims(r).Subject, cfg, req)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "读取 OSS 分片取消权限失败")
				return
			}
		}
		expectedCategory := ""
		if !isBlueprintObject {
			expectedCategory, err = s.resolveUserOSSUploadCategory(r, rawCategory, req.Source)
			if err != nil {
				writeError(w, http.StatusForbidden, err.Error())
				return
			}
		}
		if !isBlueprintObject && !isAllowedObjectKey(req.ObjectKey, ossObjectPrefix(cfg.Prefix, expectedCategory)) {
			writeError(w, http.StatusBadRequest, "OSS ObjectKey 不属于用户文件目录")
			return
		}
		if isBlueprintObject {
			req.Category = ossCategoryFromObjectKey(req.ObjectKey, cfg.Prefix)
		} else {
			req.Category = ossCategoryFromObjectKey(req.ObjectKey, cfg.Prefix)
		}
	} else if isModExport {
		exportCategory := ossModImportCategory(modExportUniqueID, "mcmods-exporter", "packages")
		exportPrefix := ossObjectPrefix(cfg.Prefix, exportCategory)
		if !isAllowedObjectKey(req.ObjectKey, exportPrefix) {
			writeError(w, http.StatusBadRequest, "OSS ObjectKey 不属于模组导入临时目录")
			return
		}
		req.Category = exportCategory
	} else if isModCatalog {
		catalogCategory := ossModImportCategory(modCatalogUniqueID, req.Source, "catalog")
		catalogPrefix := ossObjectPrefix(cfg.Prefix, catalogCategory)
		if !isAllowedObjectKey(req.ObjectKey, catalogPrefix) {
			writeError(w, http.StatusBadRequest, "OSS ObjectKey does not belong to this mod catalog import directory")
			return
		}
		req.Category = catalogCategory
	} else if isProjectDownload {
		downloadCategory := ossProjectReleaseCategory(projectDownloadType, projectDownloadID)
		downloadPrefix := ossObjectPrefix(cfg.Prefix, ossOwnerObjectCategory(downloadCategory, currentClaims(r).Subject))
		if !isAllowedObjectKey(req.ObjectKey, downloadPrefix) {
			writeError(w, http.StatusBadRequest, "OSS ObjectKey does not belong to this project download directory")
			return
		}
		req.Category = downloadCategory
	} else if isReportEvidence {
		evidenceCategory := path.Join("moderation", "report-evidence", strconv.FormatInt(currentClaims(r).Subject, 10))
		evidencePrefix := ossObjectPrefix(cfg.Prefix, evidenceCategory)
		if !isAllowedObjectKey(req.ObjectKey, evidencePrefix) {
			writeError(w, http.StatusBadRequest, "OSS ObjectKey 不属于举报附件目录")
			return
		}
		req.Category = evidenceCategory
	} else if strings.HasPrefix(req.ObjectKey, path.Join(ossRoot(cfg.Prefix), ossProjectDirectory)+"/") {
		if rawCategory == ossProjectIntroCategory || req.Category == "project_intro" || req.Category == "projectintro" {
			req.Category = ossCategoryFromObjectKey(req.ObjectKey, cfg.Prefix)
		}
		if rawCategory == ossProjectDownloadCategory || req.Category == "project_download" || req.Category == "projectdownload" {
			req.Category = ossCategoryFromObjectKey(req.ObjectKey, cfg.Prefix)
		}
	}
	if req.MultipartUploadID != "" && req.MultipartAction == "abort" {
		settlement, settlementErr := s.beginOSSMultipartSettlementWithRequest(r.Context(), currentClaims(r).Subject, cfg, req, "abort", r)
		if writeOSSMultipartSettlementError(w, settlementErr) {
			return
		}
		var abortErr error
		if !settlement.Already {
			abortErr = abortOSSMultipartUpload(r.Context(), client, cfg, req.ObjectKey, req.MultipartUploadID)
		}
		if settlementErr = s.finishOSSMultipartSettlement(r.Context(), settlement, "abort", abortErr); settlementErr != nil {
			writeError(w, http.StatusInternalServerError, "保存 OSS 分片取消状态失败")
			return
		}
		if abortErr != nil {
			writeError(w, http.StatusBadGateway, "取消 OSS 分片上传失败")
			return
		}

		s.insertOSSUploadLog(r.Context(), nil, currentClaims(r).Subject, req.ObjectKey, req.OriginalName, req.SizeBytes, s.requestClientLocation(r).IP, r.UserAgent(), "cancelled", "multipart upload aborted")
		writeJSON(w, http.StatusOK, map[string]any{"aborted": true})
		return
	}
	reverifiedBinding, reverifyErr := s.reverifyOSSCompletedByteBinding(r.Context(), client, cfg, currentClaims(r).Subject, req)
	if reverifyErr != nil {
		if errors.Is(reverifyErr, errOSSCompletedIdentityConflict) {
			writeOSSByteBindingMutationError(w, reverifyErr, "上传记录已变化")
			return
		}
		if errors.Is(reverifyErr, errOSSByteBindingPersistence) {
			writeError(w, http.StatusInternalServerError, "读取上传确认失败")
		} else {
			writeError(w, http.StatusBadGateway, "暂时无法核对已上传文件，请稍后重试")
		}
		return
	}
	if isReportEvidence {
		var evidenceID, scanStatus string
		err = s.db.QueryRow(r.Context(), `select public_id,scan_status from report_evidence
			where uploader_id=$1 and object_key=$2 and sha256=$3 and ($4::bigint<=0 or byte_size=$4) and status='temporary'`,
			currentClaims(r).Subject, req.ObjectKey, req.SHA256, req.SizeBytes).Scan(&evidenceID, &scanStatus)
		if err == nil {
			skipRequestActivity(r)
			writeJSON(w, http.StatusOK, map[string]any{"id": evidenceID, "evidenceId": evidenceID, "scanStatus": scanStatus, "idempotent": true})
			return
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusInternalServerError, "读取已登记举报附件失败")
			return
		}
	}
	{
		existingFileID, existing, found, lookupErr := s.findCompletedOSSUpload(r.Context(), currentClaims(r).Subject, req)
		if lookupErr != nil {
			writeError(w, http.StatusInternalServerError, "读取已完成 OSS 上传失败")
			return
		}
		if found {
			if isReportEvidence {
				s.writeCompletedReportEvidence(w, r, existingFileID, req)
				return
			}
			if scope == "user" {
				if releaseErr := s.releaseUserOSSUploadQuotaReservation(r.Context(), currentClaims(r).Subject, req.ObjectKey); releaseErr != nil {
					writeError(w, http.StatusInternalServerError, "释放已完成上传额度预留失败")
					return
				}
			}
			response, responseErr := s.completedOSSUploadResponse(r.Context(), cfg, existing)
			if responseErr != nil {
				writeError(w, http.StatusBadGateway, "failed to generate OSS access URL")
				return
			}
			if err := s.attachCompletedOSSUploadAssociations(r.Context(), response, existingFileID, currentClaims(r).Subject, req.Source); err != nil {
				writeBlueprintUploadAssociationError(w, err, "failed to restore completed upload associations")
				return
			}
			skipRequestActivity(r)
			if reverifiedBinding {
				writeJSON(w, http.StatusCreated, response)
			} else {
				writeJSON(w, http.StatusOK, response)
			}
			return
		}
	}
	var head *aliyunoss.HeadObjectResult
	if req.MultipartUploadID != "" {
		if req.MultipartAction != "" && req.MultipartAction != "complete" {
			writeError(w, http.StatusBadRequest, "OSS 分片上传操作不合法")
			return
		}
		settlement, settlementErr := s.beginOSSMultipartSettlement(r.Context(), currentClaims(r).Subject, cfg, req, "complete")
		if writeOSSMultipartSettlementError(w, settlementErr) {
			return
		}
		var completeErr error
		if !settlement.Already {
			completeErr = completeOSSMultipartUpload(r.Context(), client, cfg, req.ObjectKey, req.MultipartUploadID)
			if completeErr == nil {
				head, completeErr = verifyCompletedOSSMultipartObject(
					r.Context(), client, cfg, req.ObjectKey, req.SizeBytes, req.SHA256,
				)
			}
		}
		if settlementErr = s.finishOSSMultipartSettlement(r.Context(), settlement, "complete", completeErr); settlementErr != nil {
			writeError(w, http.StatusInternalServerError, "保存 OSS 分片完成状态失败")
			return
		}
		if completeErr != nil {
			writeError(w, http.StatusBadGateway, "合并 OSS 分片失败")
			return
		}
	}
	if head == nil {
		head, err = client.HeadObject(
			r.Context(),
			&aliyunoss.HeadObjectRequest{
				Bucket: aliyunoss.Ptr(cfg.Bucket),
				Key:    aliyunoss.Ptr(req.ObjectKey),
			},
		)
		if err != nil {
			s.insertOSSUploadLog(r.Context(), nil, currentClaims(r).Subject, req.ObjectKey, req.OriginalName, req.SizeBytes, s.requestClientLocation(r).IP, r.UserAgent(), "failed", err.Error())
			writeError(w, http.StatusBadGateway, "确认 OSS 文件失败")
			return
		}
	}
	if req.SizeBytes > 0 && head.ContentLength != req.SizeBytes {
		writeError(w, http.StatusBadRequest, "OSS 文件大小与上传记录不一致")
		return
	}
	if metadataHash := normalizeSHA256(metadataValue(head.Metadata, "sha256")); metadataHash == "" || metadataHash != req.SHA256 {
		writeError(w, http.StatusBadRequest, "OSS 文件哈希与上传记录不一致")
		return
	}
	if verifyErr := verifyOSSUploadedSourceBytes(r.Context(), client, cfg, req.ObjectKey, head.ContentLength, req.SHA256); verifyErr != nil {
		writeError(w, http.StatusUnprocessableEntity, "OSS source bytes do not match the declared size and SHA256")
		return
	}
	var quotaLimits ossUserQuotaLimits
	if scope == "user" {
		quotaLimits, err = ossUserQuotaLimitsForRequest(r)
		if err == nil {
			err = quotaLimits.validateSingle(head.ContentLength)
		}
		if err != nil {
			writeOSSUserQuotaError(w, err)
			return
		}
	}
	uploadObjectKey := req.ObjectKey
	originalRequestContext := r.Context()
	byteBinding, bindingErr := s.sealOSSUploadedSource(r.Context(), client, cfg, req, currentClaims(r).Subject, head)
	if bindingErr != nil {
		if errors.Is(bindingErr, errOSSByteBindingPersistence) {
			writeError(w, http.StatusInternalServerError, "保存上传确认失败")
		} else {
			writeError(w, http.StatusBadGateway, "暂时无法核对上传文件，请稍后重试")
		}
		return
	}
	defer s.discardOSSByteBinding(byteBinding)
	req.ObjectKey = byteBinding.SourceKey
	r = r.WithContext(byteBinding.ctx)
	size := head.ContentLength
	contentType := req.ContentType
	if head.ContentType != nil && *head.ContentType != "" {
		contentType = *head.ContentType
	}
	sourceObjectKey := req.ObjectKey
	sourceOriginalName := req.OriginalName
	sourceSize := size
	if isReportEvidence {
		if inspectErr := validateReportEvidenceObject(r.Context(), client, cfg, req.ObjectKey, req.OriginalName, contentType, size); inspectErr != nil {
			s.deleteOSSObjectIfUnregistered(r.Context(), cfg, req.ObjectKey, "report-evidence-validation-failed")
			writeError(w, http.StatusUnprocessableEntity, inspectErr.Error())
			return
		}
	}
	converted := false
	imageProcessLog := ""
	if isModResourceRenderUploadSource(req.Source) {
		if err = s.planOSSByteBindingStoredKey(byteBinding, persistedModResourceRenderObjectKey(req.ObjectKey)); err != nil {
			writeOSSByteBindingMutationError(w, err, "暂时无法确认上传，请重试")
			return
		}
		resizedObject, resized, resizeErr := s.persistOversizedModResourceRender(
			r.Context(), client, cfg, req.ObjectKey, contentType, size, req.SHA256,
		)
		if resizeErr != nil {
			s.deleteOSSObjectIfUnregistered(r.Context(), cfg, req.ObjectKey, "mod-resource-render-failed")
			s.insertOSSUploadLog(r.Context(), nil, currentClaims(r).Subject, req.ObjectKey, req.OriginalName, size, s.requestClientLocation(r).IP, r.UserAgent(), "failed", resizeErr.Error())
			writeError(w, http.StatusBadGateway, imageProcessErrorMessage(resizeErr))
			return
		}
		if resized {
			req.ObjectKey = resizedObject.ObjectKey
			contentType = "image/png"
			size = resizedObject.SizeBytes
			converted = true
			imageProcessLog = "render-resized-long-edge-to-1024"
		}
	}
	if shouldPersistMarkdownImageAsWebP(req.OriginalName, contentType, req.Source, req.Category) {
		if err = s.planOSSByteBindingStoredKey(byteBinding, persistedWebPObjectKey(req.ObjectKey)); err != nil {
			writeOSSByteBindingMutationError(w, err, "暂时无法确认上传，请重试")
			return
		}
		convertedObject, conversionErr := s.persistImageAsWebP(r.Context(), client, cfg, req.ObjectKey, req.OriginalName)
		if conversionErr != nil {
			s.deleteOSSObjectIfUnregistered(r.Context(), cfg, req.ObjectKey, "markdown-webp-conversion-failed")
			s.insertOSSUploadLog(r.Context(), nil, currentClaims(r).Subject, req.ObjectKey, req.OriginalName, size, s.requestClientLocation(r).IP, r.UserAgent(), "failed", conversionErr.Error())
			writeError(w, http.StatusBadGateway, imageProcessErrorMessage(conversionErr))
			return
		}
		req.ObjectKey = convertedObject.ObjectKey
		req.OriginalName = convertedObject.OriginalName
		contentType = "image/webp"
		size = convertedObject.SizeBytes
		converted = true
		imageProcessLog = "persisted-as-webp"
	}
	scanStatus := "pending"
	if requiresSynchronousCatalogImageValidation(req.Source) {
		expectedRasterHash := req.SHA256
		if converted {
			expectedRasterHash = ""
		}
		if inspectErr := validateOSSUploadedRaster(r.Context(), client, cfg, req.ObjectKey, contentType, size, expectedRasterHash, req.Source); inspectErr != nil {
			s.deleteOSSObjectIfUnregistered(r.Context(), cfg, req.ObjectKey, "synchronous-raster-validation-failed")
			if converted {
				s.deleteOSSObjectIfUnregistered(r.Context(), cfg, sourceObjectKey, "converted-source-validation-failed")
			}
			s.insertOSSUploadLog(r.Context(), nil, currentClaims(r).Subject, req.ObjectKey, req.OriginalName, size, s.requestClientLocation(r).IP, r.UserAgent(), "failed", inspectErr.Error())
			writeError(w, http.StatusUnprocessableEntity, "resource image content is invalid")
			return
		}
		scanStatus = "clean"
	}
	if err = s.planOSSByteBindingStoredKey(byteBinding, req.ObjectKey); err != nil {
		writeOSSByteBindingMutationError(w, err, "暂时无法确认上传，请重试")
		return
	}
	storedSHA256 := req.SHA256
	if converted {
		storedSHA256, err = hashOSSObjectBytes(r.Context(), client, cfg, req.ObjectKey, size)
		if err != nil {
			writeError(w, http.StatusBadGateway, "cannot verify converted object bytes")
			return
		}
	}

	// All remote reads/conversion/hash verification have completed. Signing is
	// also done before acquiring quota/object locks or a registration connection.
	access, accessErr := s.resolveOSSObjectAccessWithConfig(r.Context(), cfg, req.ObjectKey, ossObjectAccessOptions{})
	if accessErr != nil {
		writeError(w, http.StatusBadGateway, "failed to generate OSS access URL")
		return
	}
	byteBinding.stop()
	r = r.WithContext(originalRequestContext)
	registrationTx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "开始 OSS 文件登记失败")
		return
	}
	defer registrationTx.Rollback(r.Context())
	if _, err = registrationTx.Exec(r.Context(), `select pg_advisory_xact_lock(hashtextextended('oss-completion:'||$1,0))`, cfg.Bucket+":"+cfg.Endpoint+":"+uploadObjectKey); err != nil {
		writeError(w, http.StatusInternalServerError, "锁定 OSS 文件登记失败")
		return
	}
	committed, committedErr := completedOSSByteBindingForUploadTx(r.Context(), registrationTx, cfg, uploadObjectKey, currentClaims(r).Subject, req, sourceSize)
	if committedErr != nil {
		writeOSSByteBindingMutationError(w, committedErr, "cannot inspect completed OSS upload identity")
		return
	}
	if committed {
		_ = registrationTx.Rollback(r.Context())
		replay := req
		replay.ObjectKey, replay.OriginalName = uploadObjectKey, sourceOriginalName
		s.writeCompletedOSSByteBindingReplay(w, r, cfg, replay, isReportEvidence, scope)
		return
	}
	if err = lockOSSByteBindingTx(r.Context(), registrationTx, byteBinding); err != nil {
		writeOSSByteBindingMutationError(w, err, "cannot commit OSS upload byte binding")
		return
	}
	var wasRegistered, wasCompleted bool
	if err = registrationTx.QueryRow(r.Context(), `select exists(select 1 from oss_files where object_key=$1),exists(select 1 from oss_files where object_key=$1 and source_hash_verified)`, req.ObjectKey).Scan(&wasRegistered, &wasCompleted); err != nil {
		writeError(w, http.StatusInternalServerError, "读取 OSS 文件登记状态失败")
		return
	}
	if err = s.lockOSSCompletionAssociationsTx(r.Context(), registrationTx, currentClaims(r).Subject, uploadObjectKey, req.Source, wasRegistered); err != nil {
		writeError(w, http.StatusInternalServerError, "锁定上传关联对象失败")
		return
	}
	if scope == "user" {
		if wasRegistered {
			// An already registered row is already included in quota buckets.
			// Release only its reservation; never validate it as another file.
			_, err = lockOSSUserQuotaTx(r.Context(), registrationTx, currentClaims(r).Subject)
			if err == nil {
				_, err = releaseOSSUserQuotaReservationTx(r.Context(), registrationTx, currentClaims(r).Subject, uploadObjectKey)
			}
		} else {
			err = settleUserOSSUploadQuotaTx(r.Context(), registrationTx, currentClaims(r).Subject, uploadObjectKey, sourceSize, size, quotaLimits)
		}
		if err != nil {
			// Cleanup owns a separate transaction; release this connection first.
			_ = registrationTx.Rollback(r.Context())
			s.deleteOSSObjectIfUnregistered(r.Context(), cfg, req.ObjectKey, "user-quota-settlement-failed")
			if converted {
				s.deleteOSSObjectIfUnregistered(r.Context(), cfg, sourceObjectKey, "converted-source-settlement-failed")
			}
			writeOSSUserQuotaError(w, err)
			return
		}
	}
	var fileID int64
	var filePublicID string

	err = registrationTx.QueryRow(
		r.Context(),
		`insert into oss_files (bucket, endpoint, region, object_key, category, source, original_name, source_original_name, content_type, size_bytes, source_size_bytes, sha256, uploader_id, status, scan_status, source_hash_verified, stored_sha256)
		 values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, 'active', $14, true, $15)
		 on conflict (object_key) do update set source_hash_verified=true,stored_sha256=excluded.stored_sha256
        where oss_files.uploader_id=excluded.uploader_id and oss_files.status='active'
        and oss_files.bucket=excluded.bucket and oss_files.endpoint=excluded.endpoint
        and oss_files.sha256=excluded.sha256 and oss_files.source_size_bytes=excluded.source_size_bytes
        and oss_files.category=excluded.category and oss_files.source=excluded.source
		 returning id,public_id`,
		cfg.Bucket,
		cfg.displayEndpoint(),
		cfg.Region,
		req.ObjectKey,
		req.Category,
		req.Source,
		req.OriginalName,
		sourceOriginalName,
		contentType,
		size,
		sourceSize,
		req.SHA256,
		currentClaims(r).Subject,
		scanStatus,
		storedSHA256,
	).Scan(&fileID, &filePublicID)
	if errors.Is(err, pgx.ErrNoRows) {
		_ = registrationTx.Rollback(r.Context())
		if scope == "user" {
			if releaseErr := s.releaseUserOSSUploadQuotaReservation(r.Context(), currentClaims(r).Subject, uploadObjectKey); releaseErr != nil {
				writeError(w, http.StatusInternalServerError, "释放重复上传额度预留失败")
				return
			}
		}

		existingFileID, existing, found, lookupErr := s.findCompletedOSSUpload(r.Context(), currentClaims(r).Subject, req)
		if lookupErr != nil {
			writeError(w, http.StatusInternalServerError, "读取已完成 OSS 上传失败")
			return
		}
		if found {
			if isReportEvidence {
				evidenceID, evidenceScanStatus, recoveryErr := s.restoreReportEvidenceForCompletedOSSFile(
					r.Context(), existingFileID, currentClaims(r).Subject, req,
				)
				if recoveryErr != nil {
					writeError(w, http.StatusInternalServerError, "恢复已完成举报附件失败")
					return
				}
				writeJSON(w, http.StatusOK, map[string]any{
					"id": evidenceID, "evidenceId": evidenceID, "scanStatus": evidenceScanStatus, "idempotent": true,
				})
				return
			}
			response, responseErr := s.completedOSSUploadResponse(r.Context(), cfg, existing)
			if responseErr != nil {
				writeError(w, http.StatusBadGateway, "failed to generate OSS access URL")
				return
			}
			if associationErr := s.attachCompletedOSSUploadAssociations(r.Context(), response, existingFileID, currentClaims(r).Subject, req.Source); associationErr != nil {
				writeBlueprintUploadAssociationError(w, associationErr, "failed to restore completed upload associations")
				return
			}
			skipRequestActivity(r)
			writeJSON(w, http.StatusOK, response)
			return
		}
	}
	if err != nil {
		// Cleanup opens its own transaction; never wait while owning this one.
		_ = registrationTx.Rollback(r.Context())
		if converted {
			s.deleteOSSObjectIfUnregistered(r.Context(), cfg, req.ObjectKey, "converted-file-registration-failed")
			s.deleteOSSObjectIfUnregistered(r.Context(), cfg, sourceObjectKey, "converted-source-registration-failed")
		}
		writeError(w, http.StatusInternalServerError, "保存 OSS 文件记录失败")
		return
	}
	var reportEvidenceID string
	if isReportEvidence {
		if err = registrationTx.QueryRow(r.Context(), `insert into report_evidence(uploader_id,object_key,original_name,content_type,byte_size,sha256,scan_status)
			values($1,$2,$3,$4,$5,$6,$7) returning public_id`, currentClaims(r).Subject, req.ObjectKey, sourceOriginalName,
			contentType, sourceSize, req.SHA256, scanStatus).Scan(&reportEvidenceID); err != nil {
			_ = registrationTx.Rollback(r.Context())
			writeError(w, http.StatusInternalServerError, "登记举报附件失败")
			return
		}
	}
	blueprint, blueprintErr := s.completeBlueprintUploadTx(r.Context(), registrationTx, fileID, currentClaims(r).Subject, req.ObjectKey, sourceOriginalName, contentType, size, req.SHA256, uploadObjectKey)
	if blueprintErr != nil {
		writeBlueprintUploadAssociationError(w, blueprintErr, "failed to register blueprint processing task")
		return
	}
	coverPublicID, isCover := blueprintCoverPublicID(req.Source)
	if isCover {
		if err = s.bindOSSBlueprintCoverTx(r.Context(), registrationTx, coverPublicID, fileID, currentClaims(r).Subject); err != nil {
			writeError(w, http.StatusInternalServerError, "登记蓝图封面失败")
			return
		}
	}
	if wasRegistered {
		skipRequestActivity(r)
	} else if err = enqueueRequestActivityTx(r, registrationTx); err != nil {
		// Retain the uploaded source for retry. No file, quota, projection,
		// processing job or intent may commit when activity persistence fails.
		writeError(w, http.StatusInternalServerError, "failed to persist upload activity")
		return
	}
	if err = commitOSSByteBindingTx(r.Context(), registrationTx, byteBinding, fileID, req.SHA256, sourceSize, storedSHA256, size); err != nil {
		writeOSSByteBindingMutationError(w, err, "cannot commit OSS upload byte binding")
		return
	}
	if err = registrationTx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "提交 OSS 文件登记失败")
		return
	}

	logMessage := "direct-to-oss"
	if converted {
		logMessage = "direct-to-oss; " + imageProcessLog
		// The verified private source belongs to the committed byte receipt.
		// Keep it until the owning file is deleted, including future re-verification.
	}
	s.insertOSSUploadLog(r.Context(), &fileID, currentClaims(r).Subject, req.ObjectKey, sourceOriginalName, sourceSize, s.requestClientLocation(r).IP, r.UserAgent(), "success", logMessage)
	scanMessage := "等待接入文件查杀引擎"
	if scanStatus == "clean" {
		scanMessage = "上传时已完成同步图片字节校验"
	}
	s.recordOSSScanLog(r.Context(), fileID, req.ObjectKey, scanStatus, scanMessage)
	response := map[string]any{
		"id":                 filePublicID,
		"bucket":             cfg.Bucket,
		"objectKey":          req.ObjectKey,
		"category":           req.Category,
		"source":             req.Source,
		"originalName":       req.OriginalName,
		"sourceOriginalName": sourceOriginalName,
		"contentType":        contentType,
		"sizeBytes":          size,
		"sourceSizeBytes":    sourceSize,
		"converted":          converted,
		"sha256":             req.SHA256,
		"sourceSha256":       req.SHA256, "storedSha256": storedSHA256,
		"scanStatus": scanStatus,
		"url":        access.URL,
		"accessUrl":  access.URL,
		"storageUrl": ossStoredObjectURL(cfg, req.ObjectKey),
	}
	if reportEvidenceID != "" {
		response["id"] = reportEvidenceID
		response["evidenceId"] = reportEvidenceID
	}
	if blueprint != nil {
		response["blueprint"] = blueprint
		response["blueprintId"] = blueprint["id"]
	}
	if isCover {
		response["blueprintId"] = coverPublicID
	}

	if wasCompleted || (isReportEvidence && wasRegistered) {
		response["idempotent"] = true
		writeJSON(w, http.StatusOK, response)
		return
	}
	writeJSON(w, http.StatusCreated, response)
}

func reportEvidenceExtensionAllowed(extension string) bool {
	return stringSet(".png", ".jpg", ".jpeg", ".webp", ".gif", ".txt", ".log", ".pdf", ".zip")[strings.ToLower(extension)]
}

func validateReportEvidenceObject(ctx context.Context, client *aliyunoss.Client, cfg ossConfigPayload, objectKey, originalName, contentType string, expectedSize int64) error {
	result, err := client.GetObject(ctx, &aliyunoss.GetObjectRequest{Bucket: aliyunoss.Ptr(cfg.Bucket), Key: aliyunoss.Ptr(objectKey)})
	if err != nil {
		return errors.New("无法读取举报附件")
	}
	defer result.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(result.Body, maxReportEvidenceBytes+1))
	if err != nil || int64(len(raw)) > maxReportEvidenceBytes || expectedSize > 0 && int64(len(raw)) != expectedSize {
		return errors.New("举报附件大小不正确")
	}
	extension := strings.ToLower(filepath.Ext(filepath.Base(originalName)))
	detected := http.DetectContentType(raw)
	switch extension {
	case ".png":
		if detected != "image/png" {
			return errors.New("举报附件内容与扩展名不一致")
		}
	case ".jpg", ".jpeg":
		if detected != "image/jpeg" {
			return errors.New("举报附件内容与扩展名不一致")
		}
	case ".gif":
		if detected != "image/gif" {
			return errors.New("举报附件内容与扩展名不一致")
		}
	case ".webp":
		if len(raw) < 12 || string(raw[:4]) != "RIFF" || string(raw[8:12]) != "WEBP" {
			return errors.New("举报附件不是有效 WebP")
		}
	case ".pdf":
		if len(raw) < 5 || string(raw[:5]) != "%PDF-" {
			return errors.New("举报附件不是有效 PDF")
		}
	case ".txt", ".log":
		if bytes.IndexByte(raw, 0) >= 0 {
			return errors.New("举报文本附件包含二进制内容")
		}
	case ".zip":
		if err = validateReportEvidenceZIP(raw); err != nil {
			return err
		}
	default:
		return errors.New("举报附件类型不支持")
	}
	_ = contentType
	return nil
}

func validateReportEvidenceZIP(raw []byte) error {
	archive, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil || len(archive.File) == 0 || len(archive.File) > 100 {
		return errors.New("ZIP 无效或文件数量过多")
	}
	var total uint64
	seen := map[string]bool{}
	for _, file := range archive.File {
		clean, safePath := portableArchiveEntryName(file.Name, 16)
		unsafeMode := file.Mode() & (os.ModeSymlink | os.ModeDevice | os.ModeCharDevice | os.ModeNamedPipe | os.ModeSocket)
		if !safePath || unsafeMode != 0 || file.Flags&0x1 != 0 {
			return errors.New("ZIP 包含不安全路径、特殊文件或加密内容")
		}
		key := strings.ToLower(clean)
		if seen[key] || strings.EqualFold(filepath.Ext(clean), ".zip") {
			return errors.New("ZIP 包含重复文件名或嵌套压缩包")
		}
		seen[key] = true
		if file.FileInfo().IsDir() {
			continue
		}
		total += file.UncompressedSize64
		compressed := max(file.CompressedSize64, uint64(1))
		if file.UncompressedSize64 > uint64(maxReportEvidenceBytes) || total > uint64(100<<20) || file.UncompressedSize64/compressed > 100 {
			return errors.New("ZIP 解压大小或压缩比超过安全限制")
		}
	}
	return nil
}

func writeBlueprintUploadAssociationError(w http.ResponseWriter, err error, fallback string) {
	switch {
	case errors.Is(err, errBlueprintUserJobLimit):
		writeAPIError(w, http.StatusTooManyRequests, "BLUEPRINT_JOB_CONCURRENCY_LIMIT", "too many blueprint jobs are already active", 60, nil)
	case errors.Is(err, errBlueprintSourceTooLarge):
		writeAPIError(w, http.StatusRequestEntityTooLarge, "BLUEPRINT_SOURCE_SIZE_LIMIT", "blueprint source exceeds the processing size limit", 0, nil)
	default:
		writeError(w, http.StatusInternalServerError, fallback)
	}
}

func blueprintCoverPublicID(source string) (string, bool) {
	source = strings.ToLower(strings.TrimSpace(source))
	if !strings.HasPrefix(source, "blueprint_cover:") {
		return "", false
	}
	publicID := normalizeProjectObjectSegment(strings.TrimPrefix(source, "blueprint_cover:"))
	return publicID, publicID != ""
}

func persistedWebPObjectKey(sourceObjectKey string) string {
	destinationObjectKey := strings.TrimSuffix(sourceObjectKey, filepath.Ext(sourceObjectKey)) + ".webp"
	if destinationObjectKey == sourceObjectKey {
		destinationObjectKey += ".webp"
	}
	return destinationObjectKey
}

func isModResourceRenderUploadSource(source string) bool {
	source = strings.ToLower(strings.TrimSpace(source))
	if !strings.HasPrefix(source, "mod_resource:") {
		return false
	}
	return strings.HasSuffix(source, ":render") || strings.HasSuffix(source, ":render_256")
}

func persistedModResourceRenderObjectKey(sourceObjectKey string) string {
	extension := filepath.Ext(sourceObjectKey)
	destinationObjectKey := strings.TrimSuffix(sourceObjectKey, extension) + ".render-1024.png"
	if destinationObjectKey == sourceObjectKey {
		destinationObjectKey += ".render-1024.png"
	}
	return destinationObjectKey
}

func (s *Server) deleteOSSObjectIfUnregistered(ctx context.Context, cfg ossConfigPayload, objectKey, reason string) error {
	if strings.TrimSpace(objectKey) == "" {
		return nil
	}
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer cancel()
	if err := s.enqueueUnregisteredOSSObjectDeletion(cleanupCtx, cfg, objectKey, reason); err != nil {
		s.observeOSSWriteFailure("deletion_enqueue", objectKey, err)
		return err
	}
	return nil
}

func imageProcessErrorMessage(err error) string {
	var serviceError *aliyunoss.ServiceError
	if errors.As(err, &serviceError) {
		switch serviceError.Code {
		case "AccessDenied":
			return "OSS 图片处理失败，请确认 AccessKey 具有 oss:PostProcessTask 和 oss:PutObject 权限"
		case "ImageDamage":
			return "OSS 无法解析源图片，文件可能损坏或格式不受支持"
		default:
			return "OSS 图片处理失败: " + serviceError.Code
		}
	}
	return "OSS 图片处理失败"
}

// Stream rather than buffer ordinary files (up to the existing upload limit).
// The SHA column denotes original source bytes, even when image conversion
// stores different bytes. Metadata supplied during presigning is never proof.
func verifyOSSUploadedSourceBytes(ctx context.Context, client *aliyunoss.Client, cfg ossConfigPayload, objectKey string, expectedSize int64, expectedHash string) error {
	if normalizeSHA256(expectedHash) == "" {
		return errors.New("invalid source hash")
	}
	actual, err := hashOSSObjectBytes(ctx, client, cfg, objectKey, expectedSize)
	if err != nil {
		return err
	}
	if actual != expectedHash {
		return errors.New("source hash mismatch")
	}
	return nil
}

func hashOSSObjectBytes(ctx context.Context, client *aliyunoss.Client, cfg ossConfigPayload, objectKey string, expectedSize int64) (string, error) {
	if expectedSize <= 0 || expectedSize > maxOSSUploadBytes {
		return "", errors.New("invalid byte size")
	}
	result, err := client.GetObject(ctx, &aliyunoss.GetObjectRequest{Bucket: aliyunoss.Ptr(cfg.Bucket), Key: aliyunoss.Ptr(objectKey)})
	if err != nil {
		return "", fmt.Errorf("read source bytes: %w", err)
	}
	defer result.Body.Close()
	digest := sha256.New()
	size, err := io.Copy(digest, io.LimitReader(result.Body, expectedSize+1))
	if err != nil {
		return "", err
	}
	if size != expectedSize {
		return "", errors.New("object size mismatch")
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}
