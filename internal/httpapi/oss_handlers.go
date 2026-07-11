package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
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
)

const maxOSSUploadBytes = 1024 << 20

const (
	ossDownloadModePresigned        = "oss_presigned"
	ossDownloadModeESAPrivateOrigin = "esa_private_origin"
	ossProjectIntroCategory         = "project/intro"
	ossProjectDownloadCategory      = "project/download"
	ossUserPlaygroundCategory       = "user/playground"
	ossUserCommentCategory          = "user/comment"
)

var defaultOSSAllowedExtensions = []string{
	".jpg", ".jpeg", ".png", ".gif", ".webp", ".bmp", ".svg",
	".mp4", ".webm", ".mov", ".avi",
	".pdf", ".doc", ".docx", ".xls", ".xlsx", ".ppt", ".pptx", ".md",
	".txt", ".log", ".json", ".nbt", ".schem", ".schematic",
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
	ExpiresMinutes  int    `json:"expiresMinutes"`
}

type ossCompleteUploadRequest struct {
	ObjectKey    string `json:"objectKey"`
	OriginalName string `json:"originalName"`
	ContentType  string `json:"contentType"`
	SizeBytes    int64  `json:"sizeBytes"`
	SHA256       string `json:"sha256"`
	Category     string `json:"category"`
	Source       string `json:"source"`
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

	raw, err := json.Marshal(payload)
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
		string(raw),
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
	if !allowedUploadExtension(ext, cfg.AllowedExtensions) {
		writeError(w, http.StatusBadRequest, "当前文件类型不允许上传")
		return
	}
	rawCategory := strings.TrimSpace(req.Category)
	category := normalizeObjectSegment(rawCategory)
	if category == "" {
		category = "misc"
	}
	source := strings.TrimSpace(req.Source)
	objectPrefix := cfg.Prefix
	if requestedPrefix := normalizeObjectPrefix(req.Prefix); requestedPrefix != "" {
		objectPrefix = requestedPrefix
	}
	objectCategory := category
	if scope == "user" {
		category = normalizeOSSUserCategory(category, source)
		objectPrefix = cfg.Prefix
		objectCategory = path.Join("user", strconv.FormatInt(currentClaims(r).Subject, 10), strings.TrimPrefix(category, "user/"))
	} else if rawCategory == ossProjectIntroCategory || category == "project_intro" || category == "projectintro" {
		category = path.Join("project", normalizeProjectObjectSegment(req.ProjectUniqueID), "intro")
		objectCategory = category
		objectPrefix = cfg.Prefix
	} else if rawCategory == ossProjectDownloadCategory || category == "project_download" || category == "projectdownload" {
		category = path.Join("project", normalizeProjectObjectSegment(req.ProjectUniqueID), "download")
		objectCategory = category
		objectPrefix = cfg.Prefix
	}
	objectKey := buildOSSObjectKey(objectPrefix, objectCategory)
	contentType := strings.TrimSpace(req.ContentType)
	if contentType == "" {
		contentType = mime.TypeByExtension(ext)
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	var existing map[string]any
	var exists bool
	if scope == "user" {
		existing, exists = s.findExistingOSSFileByHashForUploader(r.Context(), req.SHA256, req.SizeBytes, currentClaims(r).Subject)
	} else {
		existing, exists = s.findExistingOSSFileByHash(r.Context(), req.SHA256, req.SizeBytes)
	}
	if exists {
		writeJSON(w, http.StatusOK, map[string]any{
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
		})
		return
	}
	if scope == "user" {
		deferStoredSizeCheck := shouldPersistMarkdownImageAsWebP(req.OriginalName, contentType, source, category)
		if err := s.enforceUserFileUploadLimits(r, req.SizeBytes, deferStoredSizeCheck); err != nil {
			writeError(w, http.StatusForbidden, err.Error())
			return
		}
	}
	expiresMinutes := req.ExpiresMinutes
	if expiresMinutes <= 0 {
		expiresMinutes = 10
	}
	if expiresMinutes > 60 {
		expiresMinutes = 60
	}
	expires := time.Duration(expiresMinutes) * time.Minute
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
		s.insertOSSUploadLog(r.Context(), nil, currentClaims(r).Subject, objectKey, req.OriginalName, req.SizeBytes, requestIP(r), r.UserAgent(), "failed", err.Error())
		writeError(w, http.StatusBadGateway, "生成 OSS 上传链接失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
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
	})
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
	rawCategory := strings.TrimSpace(req.Category)
	req.Category = normalizeObjectSegment(rawCategory)
	req.Source = strings.TrimSpace(req.Source)
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
	if !allowedUploadExtension(strings.ToLower(filepath.Ext(req.OriginalName)), cfg.AllowedExtensions) {
		writeError(w, http.StatusBadRequest, "当前文件类型不允许上传")
		return
	}
	if req.Category == "" {
		req.Category = "misc"
	}
	if scope == "user" {
		userPrefix := path.Join(cfg.Prefix, "user", strconv.FormatInt(currentClaims(r).Subject, 10))
		if !isAllowedObjectKey(req.ObjectKey, userPrefix) {
			writeError(w, http.StatusBadRequest, "OSS ObjectKey 不属于用户文件目录")
			return
		}
		req.Category = userCategoryFromObjectKey(req.ObjectKey, userPrefix)
	} else if strings.HasPrefix(req.ObjectKey, path.Join(cfg.Prefix, "project")+"/") {
		if rawCategory == ossProjectIntroCategory || req.Category == "project_intro" || req.Category == "projectintro" {
			req.Category = projectCategoryFromObjectKey(req.ObjectKey, cfg.Prefix, "intro")
		}
		if rawCategory == ossProjectDownloadCategory || req.Category == "project_download" || req.Category == "projectdownload" {
			req.Category = projectCategoryFromObjectKey(req.ObjectKey, cfg.Prefix, "download")
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
		s.insertOSSUploadLog(r.Context(), nil, currentClaims(r).Subject, req.ObjectKey, req.OriginalName, req.SizeBytes, requestIP(r), r.UserAgent(), "failed", err.Error())
		writeError(w, http.StatusBadGateway, "确认 OSS 文件失败")
		return
	}
	if req.SizeBytes > 0 && head.ContentLength != req.SizeBytes {
		writeError(w, http.StatusBadRequest, "OSS 文件大小与上传记录不一致")
		return
	}
	if metadataHash := normalizeSHA256(metadataValue(head.Metadata, "sha256")); metadataHash != "" && metadataHash != req.SHA256 {
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
			_, _ = client.DeleteObject(r.Context(), &aliyunoss.DeleteObjectRequest{Bucket: aliyunoss.Ptr(cfg.Bucket), Key: aliyunoss.Ptr(req.ObjectKey)})
			s.insertOSSUploadLog(r.Context(), nil, currentClaims(r).Subject, req.ObjectKey, req.OriginalName, size, requestIP(r), r.UserAgent(), "failed", conversionErr.Error())
			writeError(w, http.StatusBadGateway, imageProcessErrorMessage(conversionErr))
			return
		}
		req.ObjectKey = convertedObject.ObjectKey
		req.OriginalName = convertedObject.OriginalName
		contentType = "image/webp"
		size = convertedObject.SizeBytes
		converted = true
	}
	if scope == "user" {
		if err := s.enforceUserStoredFileLimit(r, size); err != nil {
			_, _ = client.DeleteObject(r.Context(), &aliyunoss.DeleteObjectRequest{Bucket: aliyunoss.Ptr(cfg.Bucket), Key: aliyunoss.Ptr(req.ObjectKey)})
			if converted {
				_, _ = client.DeleteObject(r.Context(), &aliyunoss.DeleteObjectRequest{Bucket: aliyunoss.Ptr(cfg.Bucket), Key: aliyunoss.Ptr(sourceObjectKey)})
			}
			writeError(w, http.StatusForbidden, err.Error())
			return
		}
	}
	var fileID int64
	err = s.db.QueryRow(
		r.Context(),
		`insert into oss_files (bucket, endpoint, region, object_key, category, source, original_name, source_original_name, content_type, size_bytes, source_size_bytes, sha256, uploader_id, status, scan_status)
		 values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, 'active', 'pending')
		 on conflict (object_key) do update
		 set category = excluded.category,
		     source = excluded.source,
		     original_name = excluded.original_name,
		     source_original_name = excluded.source_original_name,
		     content_type = excluded.content_type,
		     size_bytes = excluded.size_bytes,
		     source_size_bytes = excluded.source_size_bytes,
		     sha256 = excluded.sha256,
		     uploader_id = excluded.uploader_id,
		     updated_at = now()
		 returning id`,
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
	).Scan(&fileID)
	if err != nil {
		if converted {
			_, _ = client.DeleteObject(r.Context(), &aliyunoss.DeleteObjectRequest{Bucket: aliyunoss.Ptr(cfg.Bucket), Key: aliyunoss.Ptr(req.ObjectKey)})
			_, _ = client.DeleteObject(r.Context(), &aliyunoss.DeleteObjectRequest{Bucket: aliyunoss.Ptr(cfg.Bucket), Key: aliyunoss.Ptr(sourceObjectKey)})
		}
		writeError(w, http.StatusInternalServerError, "保存 OSS 文件记录失败")
		return
	}
	logMessage := "direct-to-oss"
	if converted {
		logMessage = "direct-to-oss; persisted-as-webp"
		_, _ = client.DeleteObject(r.Context(), &aliyunoss.DeleteObjectRequest{Bucket: aliyunoss.Ptr(cfg.Bucket), Key: aliyunoss.Ptr(sourceObjectKey)})
	}
	s.insertOSSUploadLog(r.Context(), &fileID, currentClaims(r).Subject, req.ObjectKey, sourceOriginalName, sourceSize, requestIP(r), r.UserAgent(), "success", logMessage)
	_, _ = s.db.Exec(
		r.Context(),
		`insert into oss_scan_logs (file_id, object_key, engine, result, message)
		 values ($1, $2, 'manual', 'pending', '等待接入文件查杀引擎')`,
		fileID,
		req.ObjectKey,
	)
	writeJSON(w, http.StatusCreated, map[string]any{
		"id":                 fileID,
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
		"scanStatus":         "pending",
		"url":                buildPublicOSSURL(cfg, req.ObjectKey),
	})
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
		`select id, bucket, endpoint, region, object_key, category, source, original_name, source_original_name, content_type, size_bytes, source_size_bytes, sha256, status, scan_status, created_at, updated_at
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
		var id, size, sourceSize int64
		var bucket, endpoint, region, objectKey, category, source, originalName, sourceOriginalName, contentType, sha, status, scanStatus string
		var createdAt, updatedAt time.Time
		if err := rows.Scan(&id, &bucket, &endpoint, &region, &objectKey, &category, &source, &originalName, &sourceOriginalName, &contentType, &size, &sourceSize, &sha, &status, &scanStatus, &createdAt, &updatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "解析 OSS 文件目录失败")
			return
		}
		files = append(files, ossFileRecord(id, bucket, endpoint, region, objectKey, category, source, originalName, sourceOriginalName, contentType, size, sourceSize, sha, status, scanStatus, createdAt, updatedAt))
	}
	writeJSON(w, http.StatusOK, files)
}

func (s *Server) findExistingOSSFileByHash(ctx context.Context, sha256 string, sizeBytes int64) (map[string]any, bool) {
	return s.findExistingOSSFileByHashWithOwner(ctx, sha256, sizeBytes, nil)
}

func (s *Server) findExistingOSSFileByHashForUploader(ctx context.Context, sha256 string, sizeBytes int64, uploaderID int64) (map[string]any, bool) {
	return s.findExistingOSSFileByHashWithOwner(ctx, sha256, sizeBytes, &uploaderID)
}

func (s *Server) findExistingOSSFileByHashWithOwner(ctx context.Context, sha256 string, sizeBytes int64, uploaderID *int64) (map[string]any, bool) {
	if sha256 == "" || sizeBytes <= 0 {
		return nil, false
	}
	var id, size, sourceSize int64
	var bucket, endpoint, region, objectKey, category, source, originalName, sourceOriginalName, contentType, sha, status, scanStatus string
	var createdAt, updatedAt time.Time
	query := `select id, bucket, endpoint, region, object_key, category, source, original_name, source_original_name, content_type, size_bytes, source_size_bytes, sha256, status, scan_status, created_at, updated_at
		from oss_files
		where sha256 = $1 and coalesce(nullif(source_size_bytes, 0), size_bytes) = $2 and status = 'active'`
	args := []any{sha256, sizeBytes}
	if uploaderID != nil {
		args = append(args, *uploaderID)
		query += ` and uploader_id = $3`
	}
	query += ` order by created_at asc limit 1`
	err := s.db.QueryRow(ctx, query, args...).Scan(&id, &bucket, &endpoint, &region, &objectKey, &category, &source, &originalName, &sourceOriginalName, &contentType, &size, &sourceSize, &sha, &status, &scanStatus, &createdAt, &updatedAt)
	if err != nil {
		_ = ignoreNoRows(err)
		return nil, false
	}
	return ossFileRecord(id, bucket, endpoint, region, objectKey, category, source, originalName, sourceOriginalName, contentType, size, sourceSize, sha, status, scanStatus, createdAt, updatedAt), true
}

func ossFileRecord(id int64, bucket string, endpoint string, region string, objectKey string, category string, source string, originalName string, sourceOriginalName string, contentType string, size int64, sourceSize int64, sha string, status string, scanStatus string, createdAt time.Time, updatedAt time.Time) map[string]any {
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
	userPrefix := path.Join(s.ossConfigFromSettings(r.Context()).Prefix, "user", strconv.FormatInt(claims.Subject, 10)) + "/%"
	var dailyUsed, totalUsed int64
	_ = s.db.QueryRow(
		r.Context(),
		`select coalesce(sum(coalesce(nullif(source_size_bytes, 0), size_bytes)), 0)
		 from oss_files
		 where uploader_id = $1 and object_key like $2 and status = 'active' and created_at >= current_date`,
		claims.Subject,
		userPrefix,
	).Scan(&dailyUsed)
	_ = s.db.QueryRow(
		r.Context(),
		`select coalesce(sum(size_bytes), 0)
		 from oss_files
		 where uploader_id = $1 and object_key like $2 and status = 'active'`,
		claims.Subject,
		userPrefix,
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
	userPrefix := path.Join(s.ossConfigFromSettings(r.Context()).Prefix, "user", strconv.FormatInt(claims.Subject, 10)) + "/%"
	var totalUsed int64
	_ = s.db.QueryRow(
		r.Context(),
		`select coalesce(sum(size_bytes), 0)
		 from oss_files
		 where uploader_id = $1 and object_key like $2 and status = 'active'`,
		claims.Subject,
		userPrefix,
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

func (s *Server) presignOSSFileWithRequest(w http.ResponseWriter, r *http.Request, req ossPresignRequest) {
	req.ObjectKey = strings.TrimSpace(req.ObjectKey)
	if req.ObjectKey == "" {
		writeError(w, http.StatusBadRequest, "missing OSS ObjectKey")
		return
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
			return
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
			return
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
	if err := json.Unmarshal(raw, &payload); err != nil {
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
	endpoint := cfg.PublicEndpoint
	if endpoint == "" {
		endpoint = cfg.Endpoint
	}
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

func buildOSSObjectKey(prefix string, category string) string {
	return path.Join(prefix, category, randomObjectName())
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
	return category == "user/playground" || category == "user/comment" || strings.HasSuffix(category, "/intro")
}

func persistImageAsWebP(ctx context.Context, client *aliyunoss.Client, cfg ossConfigPayload, sourceObjectKey string, sourceOriginalName string) (persistedWebPObject, error) {
	destinationObjectKey := strings.TrimSuffix(sourceObjectKey, filepath.Ext(sourceObjectKey)) + ".webp"
	if destinationObjectKey == sourceObjectKey {
		destinationObjectKey += ".webp"
	}
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

func userCategoryFromObjectKey(objectKey string, userPrefix string) string {
	relative := strings.TrimPrefix(objectKey, strings.TrimSuffix(userPrefix, "/")+"/")
	parts := strings.Split(relative, "/")
	if len(parts) == 0 || normalizeObjectSegment(parts[0]) == "" {
		return "user/misc"
	}
	return path.Join("user", normalizeObjectSegment(parts[0]))
}

func projectCategoryFromObjectKey(objectKey string, prefix string, fallback string) string {
	relative := strings.TrimPrefix(objectKey, path.Join(prefix, "project")+"/")
	parts := strings.Split(relative, "/")
	if len(parts) >= 2 {
		projectID := normalizeProjectObjectSegment(parts[0])
		section := normalizeObjectSegment(parts[1])
		if section != "" {
			return path.Join("project", projectID, section)
		}
	}
	return path.Join("project", "unassigned", fallback)
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
