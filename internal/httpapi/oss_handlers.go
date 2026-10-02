package httpapi

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
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
	writeJSON(w, http.StatusOK, redactOSSConfig(s.ossConfigFromSettings(r.Context())))
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

	current := s.ossConfigFromSettings(r.Context())
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
	_, err = s.db.Exec(
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
		if coverBlueprintID > 0 {
			if existingInternalID > 0 {
				_, _ = s.db.Exec(r.Context(), `update blueprints set cover_file_id=$2,cover_object_key=$3,updated_at=now()
					where id=$1 and owner_id=$4`, coverBlueprintID, existingInternalID, fmt.Sprint(existing["objectKey"]), currentClaims(r).Subject)
			}
		}
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
		if blueprintUpload {
			blueprint, blueprintErr := s.blueprintForExistingFile(r.Context(), existingInternalID, currentClaims(r).Subject)
			if blueprintErr != nil {
				writeBlueprintUploadAssociationError(w, blueprintErr, "failed to register blueprint processing task")
				return
			}
			if blueprint != nil {
				response["blueprint"] = blueprint
				response["blueprintId"] = blueprint["id"]
			}
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
	blueprintID := int64(0)
	blueprintPublicID := ""
	if blueprintUpload {
		blueprintID, blueprintPublicID, err = s.createPendingBlueprint(r.Context(), currentClaims(r).Subject, req.OriginalName, uploadExpiresAt)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "创建蓝图记录失败")
			return
		}
		category = ossBlueprintReleaseCategory(blueprintPublicID, "original")
		objectCategory = category
		objectPrefix = ossRoot(cfg.Prefix)
	}
	objectKey := buildOSSObjectKeyForFile(objectPrefix, objectCategory, req.OriginalName)
	if blueprintID > 0 {
		command, updateErr := s.db.Exec(r.Context(), `update blueprints set original_object_key=$2,updated_at=now() where id=$1 and status='uploading'`, blueprintID, objectKey)
		if updateErr != nil || command.RowsAffected() != 1 {
			if discardErr := s.discardPendingBlueprintUpload(r.Context(), currentClaims(r).Subject, blueprintID, ""); discardErr != nil {
				log.Printf("discard blueprint upload %d after object-key persistence failure: %v", blueprintID, discardErr)
			}
			writeError(w, http.StatusInternalServerError, "保存蓝图上传会话失败")
			return
		}
	}
	_ = coverBlueprintID
	quotaReservationObjectKey := ""
	if scope == "user" {
		storedReservation := req.SizeBytes
		if deferStoredSizeCheck {
			storedReservation = 0
		}
		if err = s.reserveUserOSSUploadQuota(r.Context(), currentClaims(r).Subject, objectKey,
			req.SizeBytes, storedReservation, uploadExpiresAt, quotaLimits); err != nil {
			if blueprintID > 0 {
				if discardErr := s.discardPendingBlueprintUpload(r.Context(), currentClaims(r).Subject, blueprintID, ""); discardErr != nil {
					log.Printf("discard blueprint upload %d after quota rejection: %v", blueprintID, discardErr)
				}
			}
			writeOSSUserQuotaError(w, err)
			return
		}
		quotaReservationObjectKey = objectKey
	}
	releaseQuotaReservation := func() {
		if quotaReservationObjectKey == "" {
			return
		}
		if releaseErr := s.releaseUserOSSUploadQuotaReservation(r.Context(), currentClaims(r).Subject, quotaReservationObjectKey); releaseErr != nil {
			log.Printf("release OSS upload quota reservation for %s: %v", quotaReservationObjectKey, releaseErr)
		}
	}
	if shouldUseOSSMultipart(req.SizeBytes) {
		multipart, multipartErr := s.initiateOSSMultipartUpload(
			r.Context(), client, cfg, objectKey, contentType, req.SHA256, req.SizeBytes, expires,
		)
		if multipartErr != nil {
			releaseQuotaReservation()
			if blueprintID > 0 {
				if discardErr := s.discardPendingBlueprintUpload(r.Context(), currentClaims(r).Subject, blueprintID, ""); discardErr != nil {
					log.Printf("discard blueprint upload %d after multipart initialization failure: %v", blueprintID, discardErr)
				}
			}
			s.insertOSSUploadLog(r.Context(), nil, currentClaims(r).Subject, objectKey, req.OriginalName, req.SizeBytes, s.requestClientLocation(r).IP, r.UserAgent(), "failed", multipartErr.Error())
			writeError(w, http.StatusBadGateway, "生成 OSS 分片上传请求失败")
			return
		}
		sessionRequest := req
		sessionRequest.ContentType = contentType
		sessionRequest.Category = category
		sessionRequest.Source = source
		if multipartErr = s.registerOSSMultipartSession(r.Context(), currentClaims(r).Subject, cfg, sessionRequest,
			objectKey, multipart.UploadID, uploadExpiresAt); multipartErr != nil {
			abortErr := abortOSSMultipartUpload(r.Context(), client, cfg, objectKey, multipart.UploadID)
			releaseQuotaReservation()
			if blueprintID > 0 {
				if discardErr := s.discardPendingBlueprintUpload(r.Context(), currentClaims(r).Subject, blueprintID, ""); discardErr != nil {
					log.Printf("discard blueprint upload %d after multipart session persistence failure: %v", blueprintID, discardErr)
				}
			}
			s.insertOSSUploadLog(r.Context(), nil, currentClaims(r).Subject, objectKey, req.OriginalName, req.SizeBytes,
				s.requestClientLocation(r).IP, r.UserAgent(), "failed", errors.Join(multipartErr, abortErr).Error())
			writeError(w, http.StatusInternalServerError, "保存 OSS 分片上传会话失败")
			return
		}
		response := map[string]any{
			"method": "MULTIPART", "multipart": multipart,
			"bucket": cfg.Bucket, "objectKey": objectKey, "category": category, "source": source,
			"originalName": req.OriginalName, "contentType": contentType, "sizeBytes": req.SizeBytes,
			"sha256": req.SHA256, "uploadRequired": true, "expiresAt": uploadExpiresAt,
			"storageUrl": ossStoredObjectURL(cfg, objectKey),
		}
		if blueprintPublicID != "" {
			response["blueprintId"] = blueprintPublicID
			response["blueprint"] = map[string]any{"id": blueprintPublicID, "status": "uploading"}
		}
		writeJSON(w, http.StatusOK, response)
		return
	}
	result, err := client.Presign(
		r.Context(),
		&aliyunoss.PutObjectRequest{
			Bucket:          aliyunoss.Ptr(cfg.Bucket),
			Key:             aliyunoss.Ptr(objectKey),
			ContentType:     aliyunoss.Ptr(contentType),
			ForbidOverwrite: aliyunoss.Ptr("true"),
			Metadata:        map[string]string{"sha256": req.SHA256},
		},
		aliyunoss.PresignExpires(expires),
	)
	if err != nil {
		releaseQuotaReservation()
		if blueprintID > 0 {
			if discardErr := s.discardPendingBlueprintUpload(r.Context(), currentClaims(r).Subject, blueprintID, ""); discardErr != nil {
				log.Printf("discard blueprint upload %d after presign failure: %v", blueprintID, discardErr)
			}
		}
		s.insertOSSUploadLog(r.Context(), nil, currentClaims(r).Subject, objectKey, req.OriginalName, req.SizeBytes, s.requestClientLocation(r).IP, r.UserAgent(), "failed", err.Error())
		writeError(w, http.StatusBadGateway, "生成 OSS 上传链接失败")
		return
	}
	response := map[string]any{
		"method":         result.Method,
		"url":            result.URL,
		"headers":        result.SignedHeaders,
		"bucket":         cfg.Bucket,
		"objectKey":      objectKey,
		"category":       category,
		"source":         source,
		"originalName":   req.OriginalName,
		"contentType":    contentType,
		"sizeBytes":      req.SizeBytes,
		"sha256":         req.SHA256,
		"uploadRequired": true,
		"expiresAt":      uploadExpiresAt,
		"storageUrl":     ossStoredObjectURL(cfg, objectKey),
	}
	if blueprintPublicID != "" {
		response["blueprintId"] = blueprintPublicID
		response["blueprint"] = map[string]any{"id": blueprintPublicID, "status": "uploading"}
	}
	writeJSON(w, http.StatusOK, response)
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
		settlement, settlementErr := s.beginOSSMultipartSettlement(r.Context(), currentClaims(r).Subject, cfg, req, "abort")
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
		if scope == "user" {
			if releaseErr := s.releaseUserOSSUploadQuotaReservation(r.Context(), currentClaims(r).Subject, req.ObjectKey); releaseErr != nil {
				writeError(w, http.StatusInternalServerError, "释放上传额度预留失败")
				return
			}
			if discardErr := s.discardPendingBlueprintUpload(r.Context(), currentClaims(r).Subject, 0, req.ObjectKey); discardErr != nil {
				writeError(w, http.StatusInternalServerError, "清理已取消的蓝图上传失败")
				return
			}
		}
		s.insertOSSUploadLog(r.Context(), nil, currentClaims(r).Subject, req.ObjectKey, req.OriginalName, req.SizeBytes, s.requestClientLocation(r).IP, r.UserAgent(), "cancelled", "multipart upload aborted")
		writeJSON(w, http.StatusOK, map[string]any{"aborted": true})
		return
	}
	if isReportEvidence {
		var evidenceID, scanStatus string
		err = s.db.QueryRow(r.Context(), `select public_id,scan_status from report_evidence
			where uploader_id=$1 and object_key=$2 and sha256=$3 and ($4::bigint<=0 or byte_size=$4) and status='temporary'`,
			currentClaims(r).Subject, req.ObjectKey, req.SHA256, req.SizeBytes).Scan(&evidenceID, &scanStatus)
		if err == nil {
			writeJSON(w, http.StatusOK, map[string]any{"id": evidenceID, "evidenceId": evidenceID, "scanStatus": scanStatus, "idempotent": true})
			return
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusInternalServerError, "读取已登记举报附件失败")
			return
		}
	}
	if !isReportEvidence {
		existingFileID, existing, found, lookupErr := s.findCompletedOSSUpload(r.Context(), currentClaims(r).Subject, req)
		if lookupErr != nil {
			writeError(w, http.StatusInternalServerError, "读取已完成 OSS 上传失败")
			return
		}
		if found {
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
			writeJSON(w, http.StatusOK, response)
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
	var quotaLimits ossUserQuotaLimits
	if scope == "user" {
		quotaLimits, err = ossUserQuotaLimitsForRequest(r)
		if err == nil {
			err = quotaLimits.validateSingle(sourceSize)
		}
		if err != nil {
			s.deleteOSSObjectIfUnregistered(r.Context(), cfg, req.ObjectKey, "user-quota-validation-failed")
			if converted {
				s.deleteOSSObjectIfUnregistered(r.Context(), cfg, sourceObjectKey, "converted-source-quota-failed")
			}
			writeOSSUserQuotaError(w, err)
			return
		}
	}
	var fileID int64
	var filePublicID string
	var quotaTx pgx.Tx
	var evidenceTx pgx.Tx
	insertQueryRow := s.db.QueryRow
	if scope == "user" {
		quotaTx, err = s.db.Begin(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "开始上传额度结算失败")
			return
		}
		defer quotaTx.Rollback(r.Context())
		if err = settleUserOSSUploadQuotaTx(r.Context(), quotaTx, currentClaims(r).Subject, sourceObjectKey,
			sourceSize, size, quotaLimits); err != nil {
			s.deleteOSSObjectIfUnregistered(r.Context(), cfg, req.ObjectKey, "user-quota-settlement-failed")
			if converted {
				s.deleteOSSObjectIfUnregistered(r.Context(), cfg, sourceObjectKey, "converted-source-settlement-failed")
			}
			writeOSSUserQuotaError(w, err)
			return
		}
		insertQueryRow = quotaTx.QueryRow
	}
	if isReportEvidence {
		evidenceTx, err = s.db.Begin(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "开始举报附件登记失败")
			return
		}
		defer evidenceTx.Rollback(r.Context())
		insertQueryRow = evidenceTx.QueryRow
	}
	err = insertQueryRow(
		r.Context(),
		`insert into oss_files (bucket, endpoint, region, object_key, category, source, original_name, source_original_name, content_type, size_bytes, source_size_bytes, sha256, uploader_id, status, scan_status)
		 values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, 'active', $14)
		 on conflict (object_key) do nothing
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
	).Scan(&fileID, &filePublicID)
	if errors.Is(err, pgx.ErrNoRows) {
		if quotaTx != nil {
			_ = quotaTx.Rollback(r.Context())
			if releaseErr := s.releaseUserOSSUploadQuotaReservation(r.Context(), currentClaims(r).Subject, sourceObjectKey); releaseErr != nil {
				writeError(w, http.StatusInternalServerError, "释放重复上传额度预留失败")
				return
			}
		}
		if evidenceTx != nil {
			_ = evidenceTx.Rollback(r.Context())
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
			writeJSON(w, http.StatusOK, response)
			return
		}
	}
	if err != nil {
		if converted {
			s.deleteOSSObjectIfUnregistered(r.Context(), cfg, req.ObjectKey, "converted-file-registration-failed")
			s.deleteOSSObjectIfUnregistered(r.Context(), cfg, sourceObjectKey, "converted-source-registration-failed")
		}
		writeError(w, http.StatusInternalServerError, "保存 OSS 文件记录失败")
		return
	}
	var reportEvidenceID string
	if isReportEvidence {
		if err = evidenceTx.QueryRow(r.Context(), `insert into report_evidence(uploader_id,object_key,original_name,content_type,byte_size,sha256,scan_status)
			values($1,$2,$3,$4,$5,$6,$7) returning public_id`, currentClaims(r).Subject, req.ObjectKey, sourceOriginalName,
			contentType, sourceSize, req.SHA256, scanStatus).Scan(&reportEvidenceID); err != nil {
			_ = evidenceTx.Rollback(r.Context())
			writeError(w, http.StatusInternalServerError, "登记举报附件失败")
			return
		}
	}
	if quotaTx != nil {
		if err = quotaTx.Commit(r.Context()); err != nil {
			if converted {
				s.deleteOSSObjectIfUnregistered(r.Context(), cfg, req.ObjectKey, "converted-quota-commit-failed")
				s.deleteOSSObjectIfUnregistered(r.Context(), cfg, sourceObjectKey, "converted-source-quota-commit-failed")
			}
			writeError(w, http.StatusInternalServerError, "提交 OSS 文件额度结算失败")
			return
		}
	}
	if evidenceTx != nil {
		if err = evidenceTx.Commit(r.Context()); err != nil {
			writeError(w, http.StatusInternalServerError, "提交举报附件登记失败")
			return
		}
	}
	logMessage := "direct-to-oss"
	if converted {
		logMessage = "direct-to-oss; " + imageProcessLog
		s.deleteOSSObjectIfUnregistered(r.Context(), cfg, sourceObjectKey, "converted-source-superseded")
	}
	s.insertOSSUploadLog(r.Context(), &fileID, currentClaims(r).Subject, req.ObjectKey, sourceOriginalName, sourceSize, s.requestClientLocation(r).IP, r.UserAgent(), "success", logMessage)
	scanMessage := "等待接入文件查杀引擎"
	if scanStatus == "clean" {
		scanMessage = "上传时已完成同步图片字节校验"
	}
	s.recordOSSScanLog(r.Context(), fileID, req.ObjectKey, scanStatus, scanMessage)
	access, accessErr := s.resolveOSSObjectAccessWithConfig(r.Context(), cfg, req.ObjectKey, ossObjectAccessOptions{})
	if accessErr != nil {
		writeError(w, http.StatusBadGateway, "failed to generate OSS access URL")
		return
	}
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
		"scanStatus":         scanStatus,
		"url":                access.URL,
		"accessUrl":          access.URL,
		"storageUrl":         ossStoredObjectURL(cfg, req.ObjectKey),
	}
	if reportEvidenceID != "" {
		response["id"] = reportEvidenceID
		response["evidenceId"] = reportEvidenceID
	}
	if blueprint, blueprintErr := s.completeBlueprintUpload(r.Context(), fileID, currentClaims(r).Subject, req.ObjectKey, sourceOriginalName, contentType, size, req.SHA256); blueprintErr != nil {
		writeBlueprintUploadAssociationError(w, blueprintErr, "failed to register blueprint processing task")
		return
	} else if blueprint != nil {
		response["blueprint"] = blueprint
		response["blueprintId"] = blueprint["id"]
	}
	if coverPublicID, ok := blueprintCoverPublicID(req.Source); ok {
		result, associationErr := s.db.Exec(r.Context(), `update blueprints blueprint
			set cover_file_id=$2,cover_object_key=file.object_key,updated_at=now()
			from oss_files file
			where blueprint.public_id=$1 and blueprint.owner_id=$3 and file.id=$2
			  and file.uploader_id=$3 and file.status='active'
			  and file.scan_status in ('clean','trusted_generated')
			  and lower(split_part(file.content_type,';',1)) in ('image/png','image/jpeg','image/jpg','image/gif','image/webp')`,
			coverPublicID, fileID, currentClaims(r).Subject)
		if associationErr != nil || result.RowsAffected() == 0 {
			writeError(w, http.StatusInternalServerError, "登记蓝图封面失败")
			return
		}
		response["blueprintId"] = coverPublicID
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
		name := filepath.ToSlash(strings.TrimSpace(file.Name))
		clean := filepath.ToSlash(filepath.Clean(name))
		unsafeMode := file.Mode() & (os.ModeSymlink | os.ModeDevice | os.ModeCharDevice | os.ModeNamedPipe | os.ModeSocket)
		if name == "" || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "/") || filepath.IsAbs(name) || strings.Count(clean, "/") > 16 || unsafeMode != 0 || file.Flags&0x1 != 0 {
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

func (s *Server) findCompletedOSSUpload(ctx context.Context, uploaderID int64, req ossCompleteUploadRequest) (int64, map[string]any, bool, error) {
	if uploaderID <= 0 || req.ObjectKey == "" || req.SHA256 == "" || req.Category == "" {
		return 0, nil, false, nil
	}
	objectKeys := []string{req.ObjectKey}
	contentType := req.ContentType
	if contentType == "" {
		contentType = mime.TypeByExtension(strings.ToLower(filepath.Ext(req.OriginalName)))
	}
	if shouldPersistMarkdownImageAsWebP(req.OriginalName, contentType, req.Source, req.Category) {
		objectKeys = append(objectKeys, persistedWebPObjectKey(req.ObjectKey))
	}
	if isModResourceRenderUploadSource(req.Source) {
		objectKeys = append(objectKeys, persistedModResourceRenderObjectKey(req.ObjectKey))
	}
	var internalID, size, sourceSize int64
	var publicID, bucket, endpoint, region, objectKey, category, source, originalName, sourceOriginalName, storedContentType, sha, status, scanStatus string
	var createdAt, updatedAt time.Time
	query := `select id,public_id,bucket,endpoint,region,object_key,category,source,original_name,source_original_name,
			content_type,size_bytes,source_size_bytes,sha256,status,scan_status,created_at,updated_at
		from oss_files
		where uploader_id=$1 and category=$2 and source=$3 and sha256=$4 and status='active'
		  and object_key=any($5::text[])
		  and ($6::bigint <= 0 or coalesce(nullif(source_size_bytes,0),size_bytes)=$6)`
	if _, ok := blueprintCoverPublicID(req.Source); ok {
		query += ` and scan_status in ('clean','trusted_generated')
		  and lower(split_part(content_type,';',1)) in ('image/png','image/jpeg','image/jpg','image/gif','image/webp')`
	}
	query += ` order by created_at asc limit 1`
	err := s.db.QueryRow(ctx, query, uploaderID, req.Category, req.Source, req.SHA256, objectKeys, req.SizeBytes).
		Scan(&internalID, &publicID, &bucket, &endpoint, &region, &objectKey, &category, &source, &originalName, &sourceOriginalName,
			&storedContentType, &size, &sourceSize, &sha, &status, &scanStatus, &createdAt, &updatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil, false, nil
	}
	if err != nil {
		return 0, nil, false, fmt.Errorf("find completed OSS upload: %w", err)
	}
	return internalID, ossFileRecord(publicID, bucket, endpoint, region, objectKey, category, source, originalName, sourceOriginalName,
		storedContentType, size, sourceSize, sha, status, scanStatus, createdAt, updatedAt), true, nil
}

func (s *Server) restoreReportEvidenceForCompletedOSSFile(
	ctx context.Context,
	fileID int64,
	uploaderID int64,
	req ossCompleteUploadRequest,
) (string, string, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return "", "", err
	}
	defer tx.Rollback(ctx)
	var originalName, contentType, sha256, scanStatus string
	var byteSize int64
	err = tx.QueryRow(ctx, `select coalesce(nullif(source_original_name,''),original_name),content_type,
			coalesce(nullif(source_size_bytes,0),size_bytes),sha256,scan_status
		from oss_files
		where id=$1 and uploader_id=$2 and object_key=$3 and category=$4 and source=$5 and sha256=$6
		  and status='active' and ($7::bigint<=0 or coalesce(nullif(source_size_bytes,0),size_bytes)=$7)
		for update`, fileID, uploaderID, req.ObjectKey, req.Category, "report_evidence", req.SHA256, req.SizeBytes).
		Scan(&originalName, &contentType, &byteSize, &sha256, &scanStatus)
	if err != nil {
		return "", "", fmt.Errorf("lock completed report evidence OSS file: %w", err)
	}
	var evidenceID, evidenceOriginalName, evidenceContentType, evidenceSHA, evidenceScanStatus, evidenceStatus string
	var evidenceUploaderID, evidenceByteSize int64
	err = tx.QueryRow(ctx, `select public_id,uploader_id,original_name,content_type,byte_size,sha256,scan_status,status
		from report_evidence where object_key=$1 for update`, req.ObjectKey).
		Scan(&evidenceID, &evidenceUploaderID, &evidenceOriginalName, &evidenceContentType, &evidenceByteSize,
			&evidenceSHA, &evidenceScanStatus, &evidenceStatus)
	if err == nil {
		if evidenceUploaderID != uploaderID || evidenceOriginalName != originalName || evidenceContentType != contentType ||
			evidenceByteSize != byteSize || evidenceSHA != sha256 || evidenceScanStatus != scanStatus || evidenceStatus != "temporary" {
			return "", "", errors.New("completed report evidence identity conflicts with its OSS file")
		}
		if err = tx.Commit(ctx); err != nil {
			return "", "", err
		}
		return evidenceID, evidenceScanStatus, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", "", fmt.Errorf("read completed report evidence: %w", err)
	}
	if err = tx.QueryRow(ctx, `insert into report_evidence(uploader_id,object_key,original_name,content_type,byte_size,sha256,scan_status)
		values($1,$2,$3,$4,$5,$6,$7) returning public_id`, uploaderID, req.ObjectKey, originalName, contentType,
		byteSize, sha256, scanStatus).Scan(&evidenceID); err != nil {
		return "", "", fmt.Errorf("restore completed report evidence: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return "", "", err
	}
	return evidenceID, scanStatus, nil
}

func (s *Server) completedOSSUploadResponse(ctx context.Context, cfg ossConfigPayload, file map[string]any) (map[string]any, error) {
	objectKey := fmt.Sprint(file["objectKey"])
	access, err := s.resolveOSSObjectAccessWithConfig(ctx, cfg, objectKey, ossObjectAccessOptions{})
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"id":                 file["id"],
		"bucket":             file["bucket"],
		"objectKey":          file["objectKey"],
		"category":           file["category"],
		"source":             file["source"],
		"originalName":       file["originalName"],
		"sourceOriginalName": file["sourceOriginalName"],
		"contentType":        file["contentType"],
		"sizeBytes":          file["sizeBytes"],
		"sourceSizeBytes":    file["sourceSizeBytes"],
		"converted":          file["converted"],
		"sha256":             file["sha256"],
		"scanStatus":         file["scanStatus"],
		"url":                access.URL,
		"accessUrl":          access.URL,
		"storageUrl":         ossStoredObjectURL(cfg, objectKey),
		"idempotent":         true,
	}, nil
}

func (s *Server) attachCompletedOSSUploadAssociations(ctx context.Context, response map[string]any, fileID, ownerID int64, source string) error {
	blueprint, err := s.blueprintForExistingFile(ctx, fileID, ownerID)
	if err != nil {
		return err
	}
	if blueprint != nil {
		response["blueprint"] = blueprint
		response["blueprintId"] = blueprint["id"]
	}
	if coverPublicID, ok := blueprintCoverPublicID(source); ok {
		result, err := s.db.Exec(ctx, `update blueprints blueprint
			set cover_file_id=$2,cover_object_key=file.object_key,updated_at=now()
			from oss_files file
			where blueprint.public_id=$1 and blueprint.owner_id=$3 and file.id=$2
			  and file.uploader_id=$3 and file.status='active'
			  and file.scan_status in ('clean','trusted_generated')
			  and lower(split_part(file.content_type,';',1)) in ('image/png','image/jpeg','image/jpg','image/gif','image/webp')`,
			coverPublicID, fileID, ownerID)
		if err != nil {
			return err
		}
		if result.RowsAffected() == 0 {
			return errors.New("blueprint cover is not a trusted raster")
		}
		response["blueprintId"] = coverPublicID
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
