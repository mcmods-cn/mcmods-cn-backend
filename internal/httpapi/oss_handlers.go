package httpapi

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	aliyunoss "github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
	"github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss/credentials"
	"github.com/jackc/pgx/v5"
	xwebp "golang.org/x/image/webp"
)

const maxOSSUploadBytes = 2 << 30

const (
	ossDownloadModePresigned        = "oss_presigned"
	ossDownloadModeESAPrivateOrigin = "esa_private_origin"
	ossProjectIntroCategory         = "project/intro"
	ossProjectDownloadCategory      = "project/download"
	ossModExportScopePrefix         = "mod_export:"
	ossModCatalogScopePrefix        = "mod_catalog:"
	ossProjectDownloadScopePrefix   = "project_download:"
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
	PreferMultipart bool   `json:"preferMultipart"`
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
	if payload.DownloadURLTTLMinutes > 10080 {
		payload.DownloadURLTTLMinutes = 10080
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
	modExportUniqueID := strings.TrimPrefix(scope, ossModExportScopePrefix)
	isModExport := modExportUniqueID != scope && modExportUniqueID != ""
	modCatalogUniqueID := strings.TrimPrefix(scope, ossModCatalogScopePrefix)
	isModCatalog := modCatalogUniqueID != scope && modCatalogUniqueID != ""
	projectDownloadType, projectDownloadID, isProjectDownload := parseOSSProjectDownloadScope(scope)
	if (isModExport && ext != ".zip") || (isModCatalog && ext != ".json") || (isProjectDownload && ext != ".jar") || (!isModExport && !isModCatalog && !isProjectDownload && !allowedUploadExtension(ext, cfg.AllowedExtensions)) {
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
		objectCategory = category
		objectPrefix = ossRoot(cfg.Prefix)
	} else if isModCatalog {
		category = ossModImportCategory(modCatalogUniqueID, source, "catalog")
		objectCategory = category
		objectPrefix = ossRoot(cfg.Prefix)
	} else if isProjectDownload {
		category = ossProjectReleaseCategory(projectDownloadType, projectDownloadID)
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
	var existing map[string]any
	var exists bool
	if isProjectDownload {
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
		if scope == "user" || isModExport || isModCatalog {
			uploaderID := currentClaims(r).Subject
			lookup.UploaderID = &uploaderID
		}
		existing, exists = s.findExistingOSSFileByHashExact(r.Context(), req.SHA256, req.SizeBytes, lookup)
	}
	var existingInternalID int64
	if exists && (blueprintUpload || coverBlueprintID > 0) {
		if coverBlueprintID > 0 {
			existingInternalID, err = s.resolveActiveTrustedRasterOSSFileInternalIDForUploader(r.Context(), fmt.Sprint(existing["id"]), currentClaims(r).Subject)
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
			"accessUrl":      buildPublicOSSURL(cfg, fmt.Sprint(existing["objectKey"])),
		}
		if blueprintUpload {
			if blueprint := s.blueprintForExistingFile(r.Context(), existingInternalID, currentClaims(r).Subject); blueprint != nil {
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
	if scope == "user" {
		deferStoredSizeCheck := shouldPersistMarkdownImageAsWebP(req.OriginalName, contentType, source, category)
		if err := s.enforceUserFileUploadLimits(r, req.SizeBytes, deferStoredSizeCheck); err != nil {
			writeError(w, http.StatusForbidden, err.Error())
			return
		}
	}
	blueprintID := int64(0)
	blueprintPublicID := ""
	if blueprintUpload {
		blueprintID, blueprintPublicID, err = s.createPendingBlueprint(r.Context(), currentClaims(r).Subject, req.OriginalName)
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
		_, _ = s.db.Exec(r.Context(), `update blueprints set original_object_key=$2,updated_at=now() where id=$1`, blueprintID, objectKey)
	}
	_ = coverBlueprintID
	expiresMinutes := req.ExpiresMinutes
	if expiresMinutes <= 0 {
		expiresMinutes = 10
	}
	if expiresMinutes > 60 {
		expiresMinutes = 60
	}
	expires := time.Duration(expiresMinutes) * time.Minute
	if shouldUseOSSMultipart(req.PreferMultipart, req.SizeBytes) {
		multipart, multipartErr := s.initiateOSSMultipartUpload(
			r.Context(), client, cfg, objectKey, contentType, req.SHA256, req.SizeBytes, expires,
		)
		if multipartErr != nil {
			s.insertOSSUploadLog(r.Context(), nil, currentClaims(r).Subject, objectKey, req.OriginalName, req.SizeBytes, s.requestClientLocation(r).IP, r.UserAgent(), "failed", multipartErr.Error())
			writeError(w, http.StatusBadGateway, "生成 OSS 分片上传请求失败")
			return
		}
		response := map[string]any{
			"method": "MULTIPART", "multipart": multipart,
			"bucket": cfg.Bucket, "objectKey": objectKey, "category": category, "source": source,
			"originalName": req.OriginalName, "contentType": contentType, "sizeBytes": req.SizeBytes,
			"sha256": req.SHA256, "uploadRequired": true, "expiresAt": time.Now().Add(expires),
			"accessUrl": buildPublicOSSURL(cfg, objectKey),
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
		"expiresAt":      time.Now().Add(expires),
		"accessUrl":      buildPublicOSSURL(cfg, objectKey),
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
	if (isModExport && ext != ".zip") || (isModCatalog && ext != ".json") || (isProjectDownload && ext != ".jar") || (!isModExport && !isModCatalog && !isProjectDownload && !allowedUploadExtension(ext, cfg.AllowedExtensions)) {
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
		downloadPrefix := ossObjectPrefix(cfg.Prefix, downloadCategory)
		if !isAllowedObjectKey(req.ObjectKey, downloadPrefix) {
			writeError(w, http.StatusBadRequest, "OSS ObjectKey does not belong to this project download directory")
			return
		}
		req.Category = downloadCategory
	} else if strings.HasPrefix(req.ObjectKey, path.Join(ossRoot(cfg.Prefix), ossProjectDirectory)+"/") {
		if rawCategory == ossProjectIntroCategory || req.Category == "project_intro" || req.Category == "projectintro" {
			req.Category = ossCategoryFromObjectKey(req.ObjectKey, cfg.Prefix)
		}
		if rawCategory == ossProjectDownloadCategory || req.Category == "project_download" || req.Category == "projectdownload" {
			req.Category = ossCategoryFromObjectKey(req.ObjectKey, cfg.Prefix)
		}
	}
	if req.MultipartUploadID != "" && req.MultipartAction == "abort" {
		if abortErr := abortOSSMultipartUpload(r.Context(), client, cfg, req.ObjectKey, req.MultipartUploadID); abortErr != nil {
			writeError(w, http.StatusBadGateway, "取消 OSS 分片上传失败")
			return
		}
		s.insertOSSUploadLog(r.Context(), nil, currentClaims(r).Subject, req.ObjectKey, req.OriginalName, req.SizeBytes, s.requestClientLocation(r).IP, r.UserAgent(), "cancelled", "multipart upload aborted")
		writeJSON(w, http.StatusOK, map[string]any{"aborted": true})
		return
	}
	if existingFileID, existing, found := s.findCompletedOSSUpload(r.Context(), currentClaims(r).Subject, req); found {
		response := completedOSSUploadResponse(cfg, existing)
		if err := s.attachCompletedOSSUploadAssociations(r.Context(), response, existingFileID, currentClaims(r).Subject, req.Source); err != nil {
			writeError(w, http.StatusInternalServerError, "恢复已完成上传的关联数据失败")
			return
		}
		writeJSON(w, http.StatusOK, response)
		return
	}
	if req.MultipartUploadID != "" {
		if req.MultipartAction != "" && req.MultipartAction != "complete" {
			writeError(w, http.StatusBadRequest, "OSS 分片上传操作不合法")
			return
		}
		if completeErr := completeOSSMultipartUpload(r.Context(), client, cfg, req.ObjectKey, req.MultipartUploadID); completeErr != nil {
			writeError(w, http.StatusBadGateway, "合并 OSS 分片失败")
			return
		}
	}
	head, err := client.HeadObject(
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
	converted := false
	if shouldPersistMarkdownImageAsWebP(req.OriginalName, contentType, req.Source, req.Category) {
		convertedObject, conversionErr := persistImageAsWebP(r.Context(), client, cfg, req.ObjectKey, req.OriginalName)
		if conversionErr != nil {
			s.deleteOSSObjectIfUnregistered(r.Context(), client, cfg, req.ObjectKey)
			s.insertOSSUploadLog(r.Context(), nil, currentClaims(r).Subject, req.ObjectKey, req.OriginalName, size, s.requestClientLocation(r).IP, r.UserAgent(), "failed", conversionErr.Error())
			writeError(w, http.StatusBadGateway, imageProcessErrorMessage(conversionErr))
			return
		}
		req.ObjectKey = convertedObject.ObjectKey
		req.OriginalName = convertedObject.OriginalName
		contentType = "image/webp"
		size = convertedObject.SizeBytes
		converted = true
	}
	scanStatus := "pending"
	if requiresSynchronousCatalogImageValidation(req.Source) {
		expectedRasterHash := req.SHA256
		if converted {
			expectedRasterHash = ""
		}
		if inspectErr := validateOSSUploadedRaster(r.Context(), client, cfg, req.ObjectKey, contentType, size, expectedRasterHash); inspectErr != nil {
			s.deleteOSSObjectIfUnregistered(r.Context(), client, cfg, req.ObjectKey)
			if converted {
				s.deleteOSSObjectIfUnregistered(r.Context(), client, cfg, sourceObjectKey)
			}
			s.insertOSSUploadLog(r.Context(), nil, currentClaims(r).Subject, req.ObjectKey, req.OriginalName, size, s.requestClientLocation(r).IP, r.UserAgent(), "failed", inspectErr.Error())
			writeError(w, http.StatusUnprocessableEntity, "resource image content is invalid")
			return
		}
		scanStatus = "clean"
	}
	if scope == "user" {
		if err := s.enforceUserStoredFileLimit(r, size); err != nil {
			if existingFileID, existing, found := s.findCompletedOSSUpload(r.Context(), currentClaims(r).Subject, req); found {
				response := completedOSSUploadResponse(cfg, existing)
				if associationErr := s.attachCompletedOSSUploadAssociations(r.Context(), response, existingFileID, currentClaims(r).Subject, req.Source); associationErr != nil {
					writeError(w, http.StatusInternalServerError, "恢复已完成上传的关联数据失败")
					return
				}
				writeJSON(w, http.StatusOK, response)
				return
			}
			s.deleteOSSObjectIfUnregistered(r.Context(), client, cfg, req.ObjectKey)
			if converted {
				s.deleteOSSObjectIfUnregistered(r.Context(), client, cfg, sourceObjectKey)
			}
			writeError(w, http.StatusForbidden, err.Error())
			return
		}
	}
	var fileID int64
	var filePublicID string
	err = s.db.QueryRow(
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
		if existingFileID, existing, found := s.findCompletedOSSUpload(r.Context(), currentClaims(r).Subject, req); found {
			response := completedOSSUploadResponse(cfg, existing)
			if associationErr := s.attachCompletedOSSUploadAssociations(r.Context(), response, existingFileID, currentClaims(r).Subject, req.Source); associationErr != nil {
				writeError(w, http.StatusInternalServerError, "恢复已完成上传的关联数据失败")
				return
			}
			writeJSON(w, http.StatusOK, response)
			return
		}
	}
	if err != nil {
		if converted {
			s.deleteOSSObjectIfUnregistered(r.Context(), client, cfg, req.ObjectKey)
			s.deleteOSSObjectIfUnregistered(r.Context(), client, cfg, sourceObjectKey)
		}
		writeError(w, http.StatusInternalServerError, "保存 OSS 文件记录失败")
		return
	}
	logMessage := "direct-to-oss"
	if converted {
		logMessage = "direct-to-oss; persisted-as-webp"
		s.deleteOSSObjectIfUnregistered(r.Context(), client, cfg, sourceObjectKey)
	}
	s.insertOSSUploadLog(r.Context(), &fileID, currentClaims(r).Subject, req.ObjectKey, sourceOriginalName, sourceSize, s.requestClientLocation(r).IP, r.UserAgent(), "success", logMessage)
	scanMessage := "等待接入文件查杀引擎"
	if scanStatus == "clean" {
		scanMessage = "上传时已完成同步图片字节校验"
	}
	_, _ = s.db.Exec(
		r.Context(),
		`insert into oss_scan_logs (file_id, object_key, engine, result, message)
		 values ($1, $2, 'manual', $3, $4)`,
		fileID,
		req.ObjectKey,
		scanStatus,
		scanMessage,
	)
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
		"url":                buildPublicOSSURL(cfg, req.ObjectKey),
	}
	if blueprint, blueprintErr := s.completeBlueprintUpload(r.Context(), fileID, currentClaims(r).Subject, req.ObjectKey, sourceOriginalName, contentType, size, req.SHA256); blueprintErr != nil {
		writeError(w, http.StatusInternalServerError, "登记蓝图处理任务失败")
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

func (s *Server) findCompletedOSSUpload(ctx context.Context, uploaderID int64, req ossCompleteUploadRequest) (int64, map[string]any, bool) {
	if uploaderID <= 0 || req.ObjectKey == "" || req.SHA256 == "" || req.Category == "" {
		return 0, nil, false
	}
	objectKeys := []string{req.ObjectKey}
	contentType := req.ContentType
	if contentType == "" {
		contentType = mime.TypeByExtension(strings.ToLower(filepath.Ext(req.OriginalName)))
	}
	if shouldPersistMarkdownImageAsWebP(req.OriginalName, contentType, req.Source, req.Category) {
		objectKeys = append(objectKeys, persistedWebPObjectKey(req.ObjectKey))
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
	if err != nil {
		return 0, nil, false
	}
	return internalID, ossFileRecord(publicID, bucket, endpoint, region, objectKey, category, source, originalName, sourceOriginalName,
		storedContentType, size, sourceSize, sha, status, scanStatus, createdAt, updatedAt), true
}

func completedOSSUploadResponse(cfg ossConfigPayload, file map[string]any) map[string]any {
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
		"url":                buildPublicOSSURL(cfg, fmt.Sprint(file["objectKey"])),
		"idempotent":         true,
	}
}

func (s *Server) attachCompletedOSSUploadAssociations(ctx context.Context, response map[string]any, fileID, ownerID int64, source string) error {
	if blueprint := s.blueprintForExistingFile(ctx, fileID, ownerID); blueprint != nil {
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

func (s *Server) deleteOSSObjectIfUnregistered(ctx context.Context, client *aliyunoss.Client, cfg ossConfigPayload, objectKey string) {
	if strings.TrimSpace(objectKey) == "" {
		return
	}
	var registered bool
	if err := s.db.QueryRow(ctx, `select exists(select 1 from oss_files where object_key=$1)`, objectKey).Scan(&registered); err != nil || registered {
		return
	}
	_, _ = client.DeleteObject(ctx, &aliyunoss.DeleteObjectRequest{Bucket: aliyunoss.Ptr(cfg.Bucket), Key: aliyunoss.Ptr(objectKey)})
}

func imageProcessErrorMessage(err error) string {
	var serviceError *aliyunoss.ServiceError
	if errors.As(err, &serviceError) {
		switch serviceError.Code {
		case "AccessDenied":
			return "OSS 图片转换为 WebP 失败，请确认 AccessKey 具有 oss:PostProcessTask 和 oss:PutObject 权限"
		case "ImageDamage":
			return "OSS 无法解析源图片，文件可能损坏或格式不受支持"
		default:
			return "OSS 图片转换为 WebP 失败: " + serviceError.Code
		}
	}
	return "OSS 图片转换为 WebP 失败"
}

func (s *Server) ossFiles(w http.ResponseWriter, r *http.Request) {
	limit := boundedLimit(r.URL.Query().Get("limit"), 100, 500)
	category := strings.TrimSpace(r.URL.Query().Get("category"))
	prefix := strings.TrimSpace(r.URL.Query().Get("prefix"))
	args := []any{}
	where := []string{"1 = 1"}
	if category != "" {
		args = append(args, category)
		where = append(where, fmt.Sprintf("category = $%d", len(args)))
	}
	if prefix != "" {
		args = append(args, prefix+"%")
		where = append(where, fmt.Sprintf("object_key like $%d", len(args)))
	}
	args = append(args, limit)
	rows, err := s.db.Query(
		r.Context(),
		`select public_id, bucket, endpoint, region, object_key, category, source, original_name, source_original_name, content_type, size_bytes, source_size_bytes, sha256, status, scan_status, created_at, updated_at
		 from oss_files
		 where `+strings.Join(where, " and ")+`
		 order by created_at desc
		 limit $`+strconv.Itoa(len(args)),
		args...,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取 OSS 文件目录失败")
		return
	}
	defer rows.Close()

	files := make([]map[string]any, 0)
	for rows.Next() {
		var size, sourceSize int64
		var id, bucket, endpoint, region, objectKey, category, source, originalName, sourceOriginalName, contentType, sha, status, scanStatus string
		var createdAt, updatedAt time.Time
		if err := rows.Scan(&id, &bucket, &endpoint, &region, &objectKey, &category, &source, &originalName, &sourceOriginalName, &contentType, &size, &sourceSize, &sha, &status, &scanStatus, &createdAt, &updatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "解析 OSS 文件目录失败")
			return
		}
		files = append(files, ossFileRecord(id, bucket, endpoint, region, objectKey, category, source, originalName, sourceOriginalName, contentType, size, sourceSize, sha, status, scanStatus, createdAt, updatedAt))
	}
	writeJSON(w, http.StatusOK, files)
}

func (s *Server) updateOSSFileScanStatus(w http.ResponseWriter, r *http.Request) {
	publicID := strings.ToLower(strings.TrimSpace(r.PathValue("publicId")))
	if !validCatalogPublicID(publicID) {
		writeError(w, http.StatusBadRequest, "无效的 OSS 文件 ID")
		return
	}
	var request struct {
		Status string `json:"status"`
		Note   string `json:"note"`
	}
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "无效的扫描状态")
		return
	}
	request.Status = strings.ToLower(strings.TrimSpace(request.Status))
	request.Note = strings.TrimSpace(request.Note)
	if request.Status != "pending" && request.Status != "clean" && request.Status != "rejected" {
		writeError(w, http.StatusBadRequest, "扫描状态必须是 pending、clean 或 rejected")
		return
	}
	if len(request.Note) > 1000 {
		writeError(w, http.StatusBadRequest, "扫描备注过长")
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "无法更新扫描状态")
		return
	}
	defer tx.Rollback(r.Context())
	var internalID int64
	var previousStatus, previousFileStatus string
	err = tx.QueryRow(r.Context(), `select id,scan_status,status from oss_files where public_id=$1 for update`, publicID).
		Scan(&internalID, &previousStatus, &previousFileStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "OSS 文件不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "无法读取 OSS 文件")
		return
	}
	if previousFileStatus == "deleted" {
		writeError(w, http.StatusConflict, "deleted OSS files cannot be rescanned")
		return
	}
	fileStatus := "quarantined"
	if request.Status == "clean" {
		fileStatus = "active"
	} else if request.Status == "rejected" {
		fileStatus = "quarantined"
	}
	if _, err = tx.Exec(r.Context(), `update oss_files
		set scan_status=$2,status=$3,updated_at=now() where id=$1`, internalID, request.Status, fileStatus); err != nil {
		writeError(w, http.StatusInternalServerError, "无法更新扫描状态")
		return
	}
	metadata, _ := json.Marshal(map[string]any{
		"previousStatus":     previousStatus,
		"previousFileStatus": previousFileStatus,
		"status":             request.Status,
		"fileStatus":         fileStatus,
		"note":               request.Note,
	})
	if _, err = tx.Exec(r.Context(), `insert into audit_events(
		aggregate_type,aggregate_key,actor_id,action,ip,user_agent,metadata)
		values('oss_file',$1,$2,'scan_status_update',$3,$4,$5::jsonb)`,
		publicID, currentClaims(r).Subject, s.requestClientLocation(r).IP, r.UserAgent(), metadata); err != nil {
		writeError(w, http.StatusInternalServerError, "无法记录扫描状态变更")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "无法提交扫描状态变更")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": publicID, "scanStatus": request.Status, "status": fileStatus})
}

func (s *Server) findExistingOSSFileByHash(ctx context.Context, sha256 string, sizeBytes int64) (map[string]any, bool) {
	return s.findExistingOSSFileByHashExact(ctx, sha256, sizeBytes, ossFileHashLookup{
		ScanStatuses: []string{"pending", "clean", "trusted_generated"},
	})
}

func (s *Server) resolveActiveOSSFileInternalIDForUploader(ctx context.Context, publicID string, uploaderID int64) (int64, error) {
	var internalID int64
	err := s.db.QueryRow(ctx, `select id from oss_files
		where public_id=$1 and uploader_id=$2 and status='active'`, publicID, uploaderID).Scan(&internalID)
	return internalID, err
}

func (s *Server) resolveActiveTrustedRasterOSSFileInternalIDForUploader(ctx context.Context, publicID string, uploaderID int64) (int64, error) {
	var internalID int64
	err := s.db.QueryRow(ctx, `select id from oss_files
		where public_id=$1 and uploader_id=$2 and status='active'
		  and scan_status in ('clean','trusted_generated')
		  and lower(split_part(content_type,';',1)) in ('image/png','image/jpeg','image/jpg','image/gif','image/webp')`,
		publicID, uploaderID).Scan(&internalID)
	return internalID, err
}

func requiresSynchronousCatalogImageValidation(source string) bool {
	source = strings.ToLower(strings.TrimSpace(source))
	return strings.HasPrefix(source, "mod_resource:") ||
		strings.HasPrefix(source, "recipe_gui:") ||
		strings.HasPrefix(source, "catalog_resource:") ||
		strings.HasPrefix(source, "catalog_recipe:") ||
		strings.HasPrefix(source, "blueprint_cover:")
}

func validateOSSUploadedRaster(ctx context.Context, client *aliyunoss.Client, cfg ossConfigPayload, objectKey, declaredContentType string, expectedSize int64, expectedSHA256 string) error {
	const maximumBytes = int64(16 << 20)
	if expectedSize <= 0 || expectedSize > maximumBytes {
		return errors.New("resource image exceeds the upload limit")
	}
	result, err := client.GetObject(ctx, &aliyunoss.GetObjectRequest{
		Bucket: aliyunoss.Ptr(cfg.Bucket),
		Key:    aliyunoss.Ptr(objectKey),
	})
	if err != nil {
		return fmt.Errorf("read uploaded image: %w", err)
	}
	defer result.Body.Close()
	data, err := io.ReadAll(io.LimitReader(result.Body, maximumBytes+1))
	if err != nil {
		return fmt.Errorf("read uploaded image body: %w", err)
	}
	if int64(len(data)) != expectedSize || int64(len(data)) > maximumBytes {
		return errors.New("uploaded image size mismatch")
	}
	if expectedSHA256 != "" {
		digest := sha256.Sum256(data)
		if hex.EncodeToString(digest[:]) != expectedSHA256 {
			return errors.New("uploaded image hash mismatch")
		}
	}
	_, err = validateRasterImageBytes(data, declaredContentType)
	return err
}

func webPDimensions(data []byte) (int, int, bool) {
	if len(data) < 20 || !bytes.Equal(data[:4], []byte("RIFF")) || !bytes.Equal(data[8:12], []byte("WEBP")) {
		return 0, 0, false
	}
	// RIFF size is the number of bytes after the first eight bytes. Requiring
	// an exact match rejects both truncated files and data hidden after the
	// declared container.
	if uint64(binary.LittleEndian.Uint32(data[4:8]))+8 != uint64(len(data)) {
		return 0, 0, false
	}
	width, height := 0, 0
	canvasWidth, canvasHeight := 0, 0
	foundCanvasHeader := false
	foundImageData := false
	for offset := 12; offset < len(data); {
		if len(data)-offset < 8 {
			return 0, 0, false
		}
		chunkType := string(data[offset : offset+4])
		chunkSize := uint64(binary.LittleEndian.Uint32(data[offset+4 : offset+8]))
		payloadStart := uint64(offset + 8)
		payloadEnd := payloadStart + chunkSize
		paddedEnd := payloadEnd + chunkSize%2
		if payloadEnd < payloadStart || paddedEnd > uint64(len(data)) {
			return 0, 0, false
		}
		if chunkSize%2 != 0 && data[int(payloadEnd)] != 0 {
			return 0, 0, false
		}
		payload := data[int(payloadStart):int(payloadEnd)]
		switch chunkType {
		case "VP8X":
			// VP8X only describes the extended canvas. It is not image data
			// by itself and must be followed by a complete VP8/VP8L payload.
			if foundCanvasHeader || foundImageData || len(payload) != 10 {
				return 0, 0, false
			}
			canvasWidth = 1 + int(payload[4]) + int(payload[5])<<8 + int(payload[6])<<16
			canvasHeight = 1 + int(payload[7]) + int(payload[8])<<8 + int(payload[9])<<16
			foundCanvasHeader = true
		case "VP8L":
			if foundImageData || len(payload) < 10 || payload[0] != 0x2f {
				return 0, 0, false
			}
			width = 1 + ((int(payload[1]) | int(payload[2])<<8) & 0x3fff)
			height = 1 + ((int(payload[2])>>6 | int(payload[3])<<2 | int(payload[4])<<10) & 0x3fff)
			foundImageData = true
		case "VP8 ":
			if foundImageData || len(payload) < 11 || !bytes.Equal(payload[3:6], []byte{0x9d, 0x01, 0x2a}) {
				return 0, 0, false
			}
			frameTag := uint32(payload[0]) | uint32(payload[1])<<8 | uint32(payload[2])<<16
			firstPartitionSize := int(frameTag >> 5)
			if frameTag&1 != 0 || (frameTag>>1)&7 > 3 || (frameTag>>4)&1 == 0 ||
				firstPartitionSize <= 0 || 10+firstPartitionSize > len(payload) {
				return 0, 0, false
			}
			width = (int(payload[6]) | int(payload[7])<<8) & 0x3fff
			height = (int(payload[8]) | int(payload[9])<<8) & 0x3fff
			foundImageData = true
		case "ANIM", "ANMF":
			// Animated WebP needs frame-by-frame compressed-stream validation
			// that the standard library does not provide. Reject it instead of
			// treating a syntactically complete RIFF as a decoded image.
			return 0, 0, false
		}
		offset = int(paddedEnd)
	}
	if !foundImageData || width <= 0 || height <= 0 {
		return 0, 0, false
	}
	if foundCanvasHeader {
		if canvasWidth <= 0 || canvasHeight <= 0 || canvasWidth != width || canvasHeight != height {
			return 0, 0, false
		}
		width, height = canvasWidth, canvasHeight
	}
	return width, height, true
}

const maxValidatedRasterPixels = int64(16_777_216)

func validateRasterImageBytes(data []byte, declaredContentType string) (string, error) {
	if len(data) == 0 {
		return "", errors.New("empty raster image")
	}
	declared := normalizeRasterContentType(declaredContentType)
	detected := normalizeRasterContentType(http.DetectContentType(data))
	if !supportedRasterContentType(declared) {
		return "", errors.New("unsupported resource image content type")
	}
	if detected != declared {
		return "", fmt.Errorf("resource image type mismatch: declared %s, detected %s", declared, detected)
	}
	if declared == "image/webp" {
		width, height, ok := webPDimensions(data)
		if !ok || int64(width)*int64(height) > maxValidatedRasterPixels {
			return "", errors.New("invalid or oversized WebP image")
		}
		config, err := xwebp.DecodeConfig(bytes.NewReader(data))
		if err != nil || config.Width != width || config.Height != height {
			return "", errors.New("invalid WebP image bitstream")
		}
		if _, err = xwebp.Decode(bytes.NewReader(data)); err != nil {
			return "", errors.New("invalid or truncated WebP image")
		}
		return declared, nil
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || normalizeRasterContentType("image/"+format) != declared || config.Width <= 0 || config.Height <= 0 {
		return "", errors.New("invalid raster image")
	}
	if int64(config.Width)*int64(config.Height) > maxValidatedRasterPixels {
		return "", errors.New("resource image dimensions are too large")
	}
	switch declared {
	case "image/png":
		_, err = png.Decode(bytes.NewReader(data))
	case "image/jpeg":
		_, err = jpeg.Decode(bytes.NewReader(data))
	case "image/gif":
		// Decode only the first frame. DecodeAll would allocate every frame
		// before a total-pixel guard could run and makes compressed GIFs a
		// disproportionate memory-amplification vector.
		_, err = gif.Decode(bytes.NewReader(data))
	}
	if err != nil {
		return "", errors.New("invalid or truncated raster image")
	}
	return declared, nil
}

func normalizeRasterContentType(value string) string {
	value = strings.ToLower(strings.TrimSpace(strings.Split(value, ";")[0]))
	switch value {
	case "image/apng":
		return "image/png"
	case "image/jpg":
		return "image/jpeg"
	default:
		return value
	}
}

func supportedRasterContentType(value string) bool {
	switch normalizeRasterContentType(value) {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
		return true
	default:
		return false
	}
}

func (s *Server) findExistingOSSFileByHashForUploader(ctx context.Context, sha256 string, sizeBytes int64, uploaderID int64) (map[string]any, bool) {
	return s.findExistingOSSFileByHashExact(ctx, sha256, sizeBytes, ossFileHashLookup{
		UploaderID:   &uploaderID,
		ScanStatuses: []string{"pending", "clean", "trusted_generated"},
	})
}

type ossFileHashLookup struct {
	UploaderID    *int64
	Category      string
	Source        string
	ScanStatuses  []string
	RequireRaster bool
	// RequireTrusted prevents an unscanned upload from becoming a public
	// catalog image merely because its bytes match a later request.
	RequireTrusted bool
}

func (s *Server) findExistingOSSFileByHashExact(ctx context.Context, sha256 string, sizeBytes int64, lookup ossFileHashLookup) (map[string]any, bool) {
	if sha256 == "" || sizeBytes <= 0 || len(lookup.ScanStatuses) == 0 {
		return nil, false
	}
	var size, sourceSize int64
	var id, bucket, endpoint, region, objectKey, category, source, originalName, sourceOriginalName, contentType, sha, status, scanStatus string
	var createdAt, updatedAt time.Time
	query := `select public_id, bucket, endpoint, region, object_key, category, source, original_name, source_original_name, content_type, size_bytes, source_size_bytes, sha256, status, scan_status, created_at, updated_at
		from oss_files
		where sha256 = $1 and coalesce(nullif(source_size_bytes, 0), size_bytes) = $2 and status = 'active'
		  and scan_status = any($3::text[])`
	args := []any{sha256, sizeBytes, lookup.ScanStatuses}
	if lookup.UploaderID != nil {
		args = append(args, *lookup.UploaderID)
		query += fmt.Sprintf(` and uploader_id = $%d`, len(args))
	}
	if lookup.Category != "" {
		args = append(args, lookup.Category)
		query += fmt.Sprintf(` and category = $%d`, len(args))
	}
	if lookup.Source != "" {
		args = append(args, lookup.Source)
		query += fmt.Sprintf(` and source = $%d`, len(args))
	}
	if lookup.RequireTrusted {
		query += ` and scan_status in ('clean','trusted_generated')`
	}
	if lookup.RequireRaster {
		query += ` and lower(split_part(content_type,';',1)) in ('image/png','image/jpeg','image/jpg','image/gif','image/webp')`
	}
	query += ` order by created_at asc limit 1`
	err := s.db.QueryRow(ctx, query, args...).Scan(&id, &bucket, &endpoint, &region, &objectKey, &category, &source, &originalName, &sourceOriginalName, &contentType, &size, &sourceSize, &sha, &status, &scanStatus, &createdAt, &updatedAt)
	if err != nil {
		_ = ignoreNoRows(err)
		return nil, false
	}
	return ossFileRecord(id, bucket, endpoint, region, objectKey, category, source, originalName, sourceOriginalName, contentType, size, sourceSize, sha, status, scanStatus, createdAt, updatedAt), true
}

func ossFileRecord(id string, bucket string, endpoint string, region string, objectKey string, category string, source string, originalName string, sourceOriginalName string, contentType string, size int64, sourceSize int64, sha string, status string, scanStatus string, createdAt time.Time, updatedAt time.Time) map[string]any {
	if sourceOriginalName == "" {
		sourceOriginalName = originalName
	}
	if sourceSize <= 0 {
		sourceSize = size
	}
	sourceExtension := strings.ToLower(filepath.Ext(sourceOriginalName))
	converted := contentType == "image/webp" && (sourceExtension == ".jpg" || sourceExtension == ".jpeg" || sourceExtension == ".png")
	return map[string]any{
		"id":                 id,
		"bucket":             bucket,
		"endpoint":           endpoint,
		"region":             region,
		"objectKey":          objectKey,
		"category":           category,
		"source":             source,
		"originalName":       originalName,
		"sourceOriginalName": sourceOriginalName,
		"contentType":        contentType,
		"sizeBytes":          size,
		"sourceSizeBytes":    sourceSize,
		"converted":          converted,
		"sha256":             sha,
		"status":             status,
		"scanStatus":         scanStatus,
		"createdAt":          createdAt,
		"updatedAt":          updatedAt,
	}
}

func (s *Server) enforceUserFileUploadLimits(r *http.Request, sizeBytes int64, deferStoredSizeCheck bool) error {
	claims := currentClaims(r)
	singleLimit := permissionMiBToBytes(numericPermissionValue(claims.Permissions, "user.file.single_limit"))
	dailyLimit := permissionMiBToBytes(numericPermissionValue(claims.Permissions, "user.file.daily_limit"))
	totalLimit := permissionMiBToBytes(numericPermissionValue(claims.Permissions, "user.file.total_limit"))
	if singleLimit <= 0 {
		return errors.New("没有单文件上传权限")
	}
	if dailyLimit <= 0 {
		return errors.New("没有每日上传额度")
	}
	if totalLimit <= 0 {
		return errors.New("没有用户文件总容量额度")
	}
	if singleLimit != maxPermissionBytes && sizeBytes > singleLimit {
		return fmt.Errorf("文件超过单文件大小限制：%s", formatLimitBytes(singleLimit))
	}
	var dailyUsed, totalUsed int64
	_ = s.db.QueryRow(
		r.Context(),
		`select coalesce(sum(coalesce(nullif(source_size_bytes, 0), size_bytes)), 0)
		 from oss_files
		 where uploader_id = $1 and status = 'active' and created_at >= current_date`,
		claims.Subject,
	).Scan(&dailyUsed)
	_ = s.db.QueryRow(
		r.Context(),
		`select coalesce(sum(size_bytes), 0)
		 from oss_files
		 where uploader_id = $1 and status = 'active'`,
		claims.Subject,
	).Scan(&totalUsed)
	if dailyLimit != maxPermissionBytes && dailyUsed+sizeBytes > dailyLimit {
		return fmt.Errorf("超过每日上传额度：%s", formatLimitBytes(dailyLimit))
	}
	if totalLimit != maxPermissionBytes && !deferStoredSizeCheck && totalUsed+sizeBytes > totalLimit {
		return fmt.Errorf("超过用户文件总容量：%s", formatLimitBytes(totalLimit))
	}
	return nil
}

func (s *Server) enforceUserStoredFileLimit(r *http.Request, sizeBytes int64) error {
	claims := currentClaims(r)
	totalLimit := permissionMiBToBytes(numericPermissionValue(claims.Permissions, "user.file.total_limit"))
	if totalLimit <= 0 {
		return errors.New("没有用户文件总容量额度")
	}
	if totalLimit == maxPermissionBytes {
		return nil
	}
	var totalUsed int64
	_ = s.db.QueryRow(
		r.Context(),
		`select coalesce(sum(size_bytes), 0)
		 from oss_files
		 where uploader_id = $1 and status = 'active'`,
		claims.Subject,
	).Scan(&totalUsed)
	if totalUsed+sizeBytes > totalLimit {
		return fmt.Errorf("超过用户文件总容量：%s", formatLimitBytes(totalLimit))
	}
	return nil
}

func (s *Server) presignOSSFile(w http.ResponseWriter, r *http.Request) {
	var req ossPresignRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	s.presignOSSFileWithRequest(w, r, req)
}

func (s *Server) presignOSSFileWithRequest(w http.ResponseWriter, r *http.Request, req ossPresignRequest) bool {
	req.ObjectKey = strings.TrimSpace(req.ObjectKey)
	if req.ObjectKey == "" {
		writeError(w, http.StatusBadRequest, "missing OSS ObjectKey")
		return false
	}
	cfg := s.ossConfigFromSettings(r.Context())
	if req.ExpiresMinutes <= 0 {
		req.ExpiresMinutes = cfg.DownloadURLTTLMinutes
	}
	if req.ExpiresMinutes > 10080 {
		req.ExpiresMinutes = 10080
	}
	expires := time.Duration(req.ExpiresMinutes) * time.Minute
	downloadURL := ""
	originalName := s.originalNameForOSSObject(r.Context(), req.ObjectKey)
	contentDisposition := downloadContentDisposition(originalName)
	if cfg.DownloadURLMode == ossDownloadModeESAPrivateOrigin {
		downloadURL = buildPublicOSSURLWithDisposition(cfg, req.ObjectKey, contentDisposition)
	} else {
		client, err := s.ossDownloadClient(r.Context(), cfg)
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, err.Error())
			return false
		}
		result, err := client.Presign(
			r.Context(),
			&aliyunoss.GetObjectRequest{
				Bucket:                     aliyunoss.Ptr(cfg.Bucket),
				Key:                        aliyunoss.Ptr(req.ObjectKey),
				ResponseContentDisposition: aliyunoss.Ptr(contentDisposition),
			},
			aliyunoss.PresignExpires(expires),
		)
		if err != nil {
			writeError(w, http.StatusBadGateway, "failed to generate OSS download URL")
			return false
		}
		downloadURL = result.URL
	}
	_, _ = s.db.Exec(
		r.Context(),
		`insert into oss_download_stats (object_key, downloads, last_download_at)
		 values ($1, 1, now())
		 on conflict (object_key) do update
		 set downloads = oss_download_stats.downloads + 1, last_download_at = now()`,
		req.ObjectKey,
	)
	writeJSON(w, http.StatusOK, map[string]any{
		"url":             downloadURL,
		"expiresAt":       time.Now().Add(expires),
		"downloadUrlMode": cfg.DownloadURLMode,
		"filename":        originalName,
	})
	return true
}

func (s *Server) ossUploadLogs(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.querySimpleRows(r, `select id, file_id, uploader_id, object_key, original_name, size_bytes, ip, user_agent, result, message, created_at from oss_upload_logs order by created_at desc limit $1`, boundedLimit(r.URL.Query().Get("limit"), 100, 500)))
}

func (s *Server) ossScanLogs(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.querySimpleRows(r, `select id, file_id, object_key, engine, result, message, payload, created_at from oss_scan_logs order by created_at desc limit $1`, boundedLimit(r.URL.Query().Get("limit"), 100, 500)))
}

func (s *Server) ossDownloadStats(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.querySimpleRows(r, `select object_key, downloads, total_bytes, last_download_at from oss_download_stats order by downloads desc, last_download_at desc nulls last limit $1`, boundedLimit(r.URL.Query().Get("limit"), 100, 500)))
}

func (s *Server) ossConfigFromSettings(ctx context.Context) ossConfigPayload {
	payload := defaultOSSConfig()
	var raw []byte
	err := s.db.QueryRow(ctx, `select value from system_settings where key = 'oss.aliyun'`).Scan(&raw)
	if err != nil {
		_ = ignoreNoRows(err)
		return payload
	}
	if err := s.openSystemSetting(raw, &payload); err != nil {
		return defaultOSSConfig()
	}
	payload = normalizeOSSConfig(payload)
	if payload.DownloadURLTTLMinutes <= 0 {
		payload.DownloadURLTTLMinutes = 10
	}
	return payload
}

func (s *Server) ossClient(ctx context.Context) (*aliyunoss.Client, ossConfigPayload, error) {
	cfg := s.ossConfigFromSettings(ctx)
	if !cfg.Enabled {
		return nil, cfg, fmt.Errorf("OSS 尚未启用")
	}
	if cfg.Region == "" || cfg.Endpoint == "" || cfg.Bucket == "" || cfg.AccessKeyID == "" || cfg.AccessKeySecret == "" {
		return nil, cfg, fmt.Errorf("OSS 配置不完整")
	}
	if requiresSecurityToken(cfg.AccessKeyID) && cfg.SecurityToken == "" {
		return nil, cfg, fmt.Errorf("OSS STS 临时凭证缺少 SecurityToken")
	}
	return newOSSClient(cfg, cfg.Endpoint, cfg.UseCName), cfg, nil
}

func (s *Server) ossDownloadClient(ctx context.Context, cfg ossConfigPayload) (*aliyunoss.Client, error) {
	if !cfg.Enabled {
		return nil, fmt.Errorf("OSS 尚未启用")
	}
	if cfg.Region == "" || cfg.Endpoint == "" || cfg.Bucket == "" || cfg.AccessKeyID == "" || cfg.AccessKeySecret == "" {
		return nil, fmt.Errorf("OSS 配置不完整")
	}
	if requiresSecurityToken(cfg.AccessKeyID) && cfg.SecurityToken == "" {
		return nil, fmt.Errorf("OSS STS 临时凭证缺少 SecurityToken")
	}
	endpoint := cfg.Endpoint
	return newOSSClient(cfg, endpoint, isCustomOSSEndpoint(endpoint)), nil
}

func newOSSClient(cfg ossConfigPayload, endpoint string, useCName bool) *aliyunoss.Client {
	ossCfg := aliyunoss.LoadDefaultConfig().
		WithCredentialsProvider(credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.AccessKeySecret, cfg.SecurityToken)).
		WithRegion(cfg.Region).
		WithEndpoint(endpoint).
		WithUseCName(useCName || isCustomOSSEndpoint(endpoint))
	return aliyunoss.NewClient(ossCfg)
}

func redactOSSConfig(payload ossConfigPayload) map[string]any {
	return map[string]any{
		"enabled":                 payload.Enabled,
		"region":                  payload.Region,
		"endpoint":                payload.Endpoint,
		"publicEndpoint":          payload.PublicEndpoint,
		"bucket":                  payload.Bucket,
		"accessKeyId":             payload.AccessKeyID,
		"hasAccessKeySecret":      strings.TrimSpace(payload.AccessKeySecret) != "",
		"hasSecurityToken":        strings.TrimSpace(payload.SecurityToken) != "",
		"useCName":                payload.UseCName || isCustomOSSEndpoint(payload.Endpoint),
		"prefix":                  payload.Prefix,
		"downloadUrlTtlMinutes":   payload.DownloadURLTTLMinutes,
		"downloadUrlMode":         payload.DownloadURLMode,
		"allowedExtensions":       payload.AllowedExtensions,
		"bucketAccessPolicy":      "private-read-write",
		"temporaryDownloadPolicy": "presigned-url",
	}
}

func normalizeOSSConfig(payload ossConfigPayload) ossConfigPayload {
	payload.Region = strings.TrimSpace(payload.Region)
	payload.Endpoint = normalizeOSSEndpoint(payload.Endpoint)
	payload.PublicEndpoint = normalizeOSSEndpoint(payload.PublicEndpoint)
	payload.Bucket = strings.TrimSpace(payload.Bucket)
	payload.AccessKeyID = strings.TrimSpace(payload.AccessKeyID)
	payload.AccessKeySecret = strings.TrimSpace(payload.AccessKeySecret)
	payload.SecurityToken = strings.TrimSpace(payload.SecurityToken)
	payload.Prefix = normalizeObjectPrefix(payload.Prefix)
	if payload.Prefix == "" {
		payload.Prefix = "mcmods"
	}
	if payload.Region != "" && payload.Endpoint == "" {
		payload.Endpoint = defaultOSSEndpoint(payload.Region)
	}
	if payload.PublicEndpoint == "" {
		payload.PublicEndpoint = "https://oss.mcmods.cn"
	}
	if payload.PublicEndpoint == "" && isCustomOSSEndpoint(payload.Endpoint) {
		payload.PublicEndpoint = payload.Endpoint
		payload.Endpoint = defaultOSSEndpoint(payload.Region)
		payload.UseCName = false
	}
	if !isCustomOSSEndpoint(payload.Endpoint) {
		payload.UseCName = false
	}
	if payload.DownloadURLTTLMinutes <= 0 {
		payload.DownloadURLTTLMinutes = 10
	}
	payload.DownloadURLMode = normalizeOSSDownloadMode(payload.DownloadURLMode)
	return payload
}

func (payload ossConfigPayload) displayEndpoint() string {
	if payload.PublicEndpoint != "" {
		return payload.PublicEndpoint
	}
	return payload.Endpoint
}

func defaultOSSConfig() ossConfigPayload {
	return ossConfigPayload{
		Prefix:                "mcmods",
		PublicEndpoint:        "https://oss.mcmods.cn",
		DownloadURLTTLMinutes: 10,
		DownloadURLMode:       ossDownloadModeESAPrivateOrigin,
		AllowedExtensions:     defaultOSSAllowedExtensions,
	}
}

func normalizeOSSDownloadMode(value string) string {
	switch strings.TrimSpace(value) {
	case ossDownloadModePresigned, "presigned-url":
		return ossDownloadModePresigned
	case ossDownloadModeESAPrivateOrigin, "esa-private-origin":
		return ossDownloadModeESAPrivateOrigin
	default:
		return ossDownloadModeESAPrivateOrigin
	}
}

func buildPublicOSSURL(cfg ossConfigPayload, objectKey string) string {
	endpoint := strings.TrimRight(cfg.PublicEndpoint, "/")
	if endpoint == "" {
		endpoint = strings.TrimRight(cfg.Endpoint, "/")
	}
	if endpoint == "" {
		return strings.TrimLeft(objectKey, "/")
	}
	return endpoint + "/" + strings.TrimLeft(objectKey, "/")
}

func buildPublicOSSURLWithDisposition(cfg ossConfigPayload, objectKey string, contentDisposition string) string {
	rawURL := buildPublicOSSURL(cfg, objectKey)
	if contentDisposition == "" {
		return rawURL
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	query := parsed.Query()
	query.Set("response-content-disposition", contentDisposition)
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func (s *Server) originalNameForOSSObject(ctx context.Context, objectKey string) string {
	var originalName string
	err := s.db.QueryRow(ctx, `select original_name from oss_files where object_key = $1`, objectKey).Scan(&originalName)
	if err != nil || strings.TrimSpace(originalName) == "" {
		return path.Base(objectKey)
	}
	return originalName
}

func downloadContentDisposition(filename string) string {
	filename = strings.TrimSpace(filename)
	if filename == "" {
		filename = "download"
	}
	asciiFallback := strings.Map(func(r rune) rune {
		if r < 0x20 || r == '"' || r == '\\' || r == ';' {
			return '-'
		}
		if r > 0x7e {
			return '-'
		}
		return r
	}, filename)
	asciiFallback = strings.TrimSpace(asciiFallback)
	if asciiFallback == "" {
		asciiFallback = "download"
	}
	return fmt.Sprintf(`attachment; filename="%s"; filename*=UTF-8''%s`, asciiFallback, url.PathEscape(filename))
}

func (s *Server) insertOSSUploadLog(ctx context.Context, fileID *int64, uploaderID int64, objectKey string, originalName string, sizeBytes int64, ip string, userAgent string, result string, message string) {
	_, _ = s.db.Exec(
		ctx,
		`insert into oss_upload_logs (file_id, uploader_id, object_key, original_name, size_bytes, ip, user_agent, result, message)
		 values ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		fileID,
		uploaderID,
		objectKey,
		originalName,
		sizeBytes,
		ip,
		userAgent,
		result,
		message,
	)
}

func (s *Server) resolveUserOSSUploadCategory(r *http.Request, category, source string) (string, error) {
	claims := currentClaims(r)
	normalizedSource := strings.ToLower(strings.TrimSpace(source))
	if siteID, contentID, ok := parseModTextUploadSource(normalizedSource); ok {
		identity, err := s.modIdentity(r.Context(), siteID)
		if err != nil || !canEditMod(claims, identity) {
			return "", errors.New("没有权限向该模组资料正文目录上传文件")
		}
		return ossProjectTextCategory("mod", identity.UniqueID, contentID, "content"), nil
	}
	if strings.HasPrefix(normalizedSource, "mod_text:") {
		return "", errors.New("模组资料正文上传目标不完整")
	}
	if strings.HasPrefix(normalizedSource, "mod_resource:") {
		parts := strings.SplitN(strings.TrimPrefix(normalizedSource, "mod_resource:"), ":", 4)
		if len(parts) < 3 {
			return "", errors.New("模组资料图片上传目标不完整")
		}
		siteID := strings.TrimSpace(parts[0])
		resourceKind := normalizeObjectSegment(parts[1])
		assetKind := normalizeObjectSegment(parts[2])
		if resourceKind == "" {
			resourceKind = "resource"
		}
		if assetKind != "icon" && assetKind != "render" {
			return "", errors.New("模组资料图片类型不正确")
		}
		identity, err := s.modIdentity(r.Context(), siteID)
		if err != nil || !canEditMod(claims, identity) {
			return "", errors.New("没有权限向该模组资料目录上传文件")
		}
		return ossProjectCategory("mod", identity.UniqueID, "icons", resourceKind, "original"), nil
	}
	if strings.HasPrefix(normalizedSource, "recipe_gui:") {
		parts := strings.Split(strings.TrimPrefix(normalizedSource, "recipe_gui:"), ":")
		recipeTypePublicID := normalizeObjectSegment(parts[0])
		templatePublicID := "staging"
		if len(parts) > 1 && normalizeObjectSegment(parts[1]) != "" {
			templatePublicID = normalizeObjectSegment(parts[1])
		}
		var exists bool
		if recipeTypePublicID == "" || !hasPermission(claims.Permissions, "content.write") ||
			s.db.QueryRow(r.Context(), `select exists(select 1 from catalog_entities where public_id=$1 and entity_type='recipe_type' and status='active')`, recipeTypePublicID).Scan(&exists) != nil || !exists {
			return "", errors.New("没有权限向该配方模板目录上传文件")
		}
		return ossProjectCategory("catalog", "_shared", "recipe-gui", recipeTypePublicID, templatePublicID), nil
	}
	if strings.HasPrefix(normalizedSource, "creator_avatar:") {
		parts := strings.SplitN(strings.TrimPrefix(normalizedSource, "creator_avatar:"), ":", 2)
		if len(parts) != 2 {
			return "", errors.New("作者头像上传目标不完整")
		}
		kind, publicID := normalizeObjectSegment(parts[0]), normalizeObjectSegment(parts[1])
		var storedKind string
		var createdBy, claimedBy *int64
		if s.db.QueryRow(r.Context(), `select kind,created_by,claimed_by from creators where public_id=$1 and status='active'`, publicID).
			Scan(&storedKind, &createdBy, &claimedBy) != nil {
			return "", errors.New("作者或团队不存在")
		}
		canEdit := hasPermission(claims.Permissions, "admin.*") || hasPermission(claims.Permissions, "creator.edit") ||
			(createdBy != nil && *createdBy == claims.Subject) || (claimedBy != nil && *claimedBy == claims.Subject)
		if !canEdit || kind != normalizeObjectSegment(storedKind) {
			return "", errors.New("没有权限向该作者或团队目录上传文件")
		}
		return ossProjectCategory(kind, publicID, "icons", "avatar", "original"), nil
	}
	for _, candidate := range []struct {
		prefix      string
		destination string
	}{
		{prefix: "mod_gallery:", destination: "gallery"},
		{prefix: "iconexport:", destination: "iconexporter"},
	} {
		if !strings.HasPrefix(normalizedSource, candidate.prefix) {
			continue
		}
		remainder := strings.TrimSpace(strings.TrimPrefix(normalizedSource, candidate.prefix))
		siteID := strings.TrimSpace(strings.SplitN(remainder, ":", 2)[0])
		if siteID == "" || siteID == "draft" {
			return ossUserCategory(claims.Subject, normalizeOSSUserFileScope(category, source)), nil
		}
		identity, err := s.modIdentity(r.Context(), siteID)
		if err != nil && candidate.destination == "gallery" {
			return ossUserCategory(claims.Subject, normalizeOSSUserFileScope(category, source)), nil
		}
		if err != nil || !canEditMod(claims, identity) {
			return "", errors.New("没有权限向该模组目录上传文件")
		}
		if candidate.destination == "gallery" {
			return ossProjectTextCategory("mod", identity.UniqueID, identity.UniqueID, "gallery"), nil
		}
		return ossModImportCategory(identity.UniqueID, candidate.destination, "catalog"), nil
	}
	return ossUserCategory(claims.Subject, normalizeOSSUserFileScope(category, source)), nil
}

func parseModTextUploadSource(source string) (siteID string, contentID string, ok bool) {
	source = strings.ToLower(strings.TrimSpace(source))
	if !strings.HasPrefix(source, "mod_text:") {
		return "", "", false
	}
	parts := strings.SplitN(strings.TrimPrefix(source, "mod_text:"), ":", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	siteID = strings.TrimSpace(parts[0])
	contentID = normalizeObjectSegment(parts[1])
	if siteID == "" || contentID == "" {
		return "", "", false
	}
	return siteID, contentID, true
}

func ossProjectDownloadScope(projectType, projectID string) string {
	return ossProjectDownloadScopePrefix + normalizeOSSProjectKind(projectType) + ":" + normalizeProjectObjectSegment(projectID)
}

func parseOSSProjectDownloadScope(scope string) (string, string, bool) {
	if !strings.HasPrefix(scope, ossProjectDownloadScopePrefix) {
		return "", "", false
	}
	value := strings.TrimPrefix(scope, ossProjectDownloadScopePrefix)
	parts := strings.SplitN(value, ":", 2)
	if len(parts) == 1 {
		return "mod", normalizeProjectObjectSegment(parts[0]), parts[0] != ""
	}
	projectID := normalizeProjectObjectSegment(parts[1])
	return parts[0], projectID, parts[1] != ""
}

type persistedWebPObject struct {
	ObjectKey    string
	OriginalName string
	SizeBytes    int64
}

func shouldPersistMarkdownImageAsWebP(originalName string, contentType string, source string, category string) bool {
	extension := strings.ToLower(filepath.Ext(originalName))
	if extension != ".jpg" && extension != ".jpeg" && extension != ".png" {
		return false
	}
	contentType = strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	if contentType != "image/jpeg" && contentType != "image/jpg" && contentType != "image/png" {
		return false
	}
	source = normalizeObjectSegment(source)
	category = strings.ToLower(strings.TrimSpace(category))
	if source == "playground" || source == "comment" || source == "markdown" || source == "project_intro" || source == "projectintro" {
		return true
	}
	return strings.Contains(category, "/files/text/") ||
		strings.HasSuffix(category, "/files/playground") ||
		strings.HasSuffix(category, "/files/comments") ||
		category == "users/playground" ||
		category == "users/comments" ||
		strings.HasSuffix(category, "/description")
}

func persistImageAsWebP(ctx context.Context, client *aliyunoss.Client, cfg ossConfigPayload, sourceObjectKey string, sourceOriginalName string) (persistedWebPObject, error) {
	destinationObjectKey := persistedWebPObjectKey(sourceObjectKey)
	encodedDestination := base64.RawURLEncoding.EncodeToString([]byte(destinationObjectKey))
	result, err := client.ProcessObject(ctx, &aliyunoss.ProcessObjectRequest{
		Bucket:  aliyunoss.Ptr(cfg.Bucket),
		Key:     aliyunoss.Ptr(sourceObjectKey),
		Process: aliyunoss.Ptr("image/format,webp|sys/saveas,o_" + encodedDestination),
	})
	if err != nil {
		return persistedWebPObject{}, err
	}
	if result.ProcessStatus != "" && !strings.EqualFold(result.ProcessStatus, "OK") {
		return persistedWebPObject{}, fmt.Errorf("OSS image process status: %s", result.ProcessStatus)
	}
	head, err := client.HeadObject(ctx, &aliyunoss.HeadObjectRequest{
		Bucket: aliyunoss.Ptr(cfg.Bucket),
		Key:    aliyunoss.Ptr(destinationObjectKey),
	})
	if err != nil {
		return persistedWebPObject{}, err
	}
	baseName := strings.TrimSuffix(path.Base(sourceOriginalName), filepath.Ext(sourceOriginalName))
	if baseName == "" {
		baseName = "image"
	}
	return persistedWebPObject{
		ObjectKey:    destinationObjectKey,
		OriginalName: baseName + ".webp",
		SizeBytes:    head.ContentLength,
	}, nil
}

func randomObjectName() string {
	random := make([]byte, 16)
	_, _ = rand.Read(random)
	hexValue := hex.EncodeToString(random)
	return hexValue[0:8] + "-" + hexValue[8:12] + "-" + hexValue[12:16] + "-" + hexValue[16:20] + "-" + hexValue[20:32]
}

func normalizeObjectPrefix(value string) string {
	parts := strings.Split(strings.Trim(value, "/ "), "/")
	cleaned := make([]string, 0, len(parts))
	for _, part := range parts {
		if segment := normalizeObjectSegment(part); segment != "" {
			cleaned = append(cleaned, segment)
		}
	}
	return strings.Join(cleaned, "/")
}

func isAllowedObjectKey(objectKey string, configuredPrefix string) bool {
	objectKey = strings.TrimSpace(objectKey)
	if objectKey == "" || strings.HasPrefix(objectKey, "/") || strings.Contains(objectKey, "..") {
		return false
	}
	prefix := normalizeObjectPrefix(configuredPrefix)
	if prefix == "" {
		return true
	}
	return objectKey == prefix || strings.HasPrefix(objectKey, prefix+"/")
}

func normalizeSHA256(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if len(value) != 64 {
		return ""
	}
	for _, r := range value {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return ""
		}
	}
	return value
}

func metadataValue(metadata map[string]string, key string) string {
	for currentKey, value := range metadata {
		if strings.EqualFold(currentKey, key) {
			return value
		}
	}
	return ""
}

func normalizeOSSEndpoint(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if !strings.HasPrefix(value, "http://") && !strings.HasPrefix(value, "https://") {
		return "https://" + value
	}
	return value
}

func defaultOSSEndpoint(region string) string {
	region = strings.TrimSpace(region)
	if region == "" {
		return ""
	}
	return "https://oss-" + region + ".aliyuncs.com"
}

func isCustomOSSEndpoint(endpoint string) bool {
	endpoint = strings.ToLower(endpoint)
	return endpoint != "" && !strings.Contains(endpoint, ".aliyuncs.com") && !strings.Contains(endpoint, ".aliyun.com")
}

func requiresSecurityToken(accessKeyID string) bool {
	return strings.HasPrefix(strings.ToUpper(strings.TrimSpace(accessKeyID)), "STS.")
}

func normalizeObjectSegment(value string) string {
	value = strings.TrimSpace(value)
	value = strings.ToLower(value)
	var builder strings.Builder
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '-' {
			builder.WriteRune(r)
		}
	}
	return builder.String()
}

func normalizeProjectObjectSegment(value string) string {
	value = normalizeObjectSegment(value)
	if value == "" {
		return "unassigned"
	}
	return value
}

const maxPermissionBytes int64 = 1<<63 - 1

func permissionMiBToBytes(value int32) int64 {
	if value <= 0 {
		return 0
	}
	if value == maxPermissionValue {
		return maxPermissionBytes
	}
	return int64(value) * 1024 * 1024
}

func formatLimitBytes(value int64) string {
	if value == maxPermissionBytes {
		return "unlimited"
	}
	if value%(1024*1024) == 0 {
		return fmt.Sprintf("%d MiB", value/(1024*1024))
	}
	return fmt.Sprintf("%d bytes", value)
}

func allowedUploadExtension(ext string, allowedExtensions []string) bool {
	ext = normalizeExtension(ext)
	if ext == "" {
		return false
	}
	allowed := make(map[string]struct{}, len(allowedExtensions))
	for _, item := range normalizeAllowedExtensions(allowedExtensions) {
		allowed[item] = struct{}{}
	}
	_, ok := allowed[ext]
	return ok
}

func normalizeAllowedExtensions(values []string) []string {
	if len(values) == 0 {
		values = defaultOSSAllowedExtensions
	}
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		ext := normalizeExtension(value)
		if ext == "" {
			continue
		}
		if _, ok := seen[ext]; ok {
			continue
		}
		seen[ext] = struct{}{}
		result = append(result, ext)
	}
	if len(result) == 0 {
		return append([]string(nil), defaultOSSAllowedExtensions...)
	}
	return result
}

func normalizeExtension(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return ""
	}
	if !strings.HasPrefix(value, ".") {
		value = "." + value
	}
	if len(value) < 2 || strings.ContainsAny(value, `/\:*?"<>|`) {
		return ""
	}
	for _, r := range value[1:] {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '-' || r == '+') {
			return ""
		}
	}
	return value
}
