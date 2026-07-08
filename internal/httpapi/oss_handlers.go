package httpapi

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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
	"github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss/credentials"
)

const maxOSSUploadBytes = 1024 << 20

type ossConfigPayload struct {
	Enabled               bool   `json:"enabled"`
	Region                string `json:"region"`
	Endpoint              string `json:"endpoint"`
	Bucket                string `json:"bucket"`
	AccessKeyID           string `json:"accessKeyId"`
	AccessKeySecret       string `json:"accessKeySecret,omitempty"`
	HasAccessKeySecret    bool   `json:"hasAccessKeySecret,omitempty"`
	UseCName              bool   `json:"useCName"`
	Prefix                string `json:"prefix"`
	DownloadURLTTLMinutes int    `json:"downloadUrlTtlMinutes"`
	BucketAccessPolicy    string `json:"bucketAccessPolicy,omitempty"`
	TemporaryDownload     string `json:"temporaryDownloadPolicy,omitempty"`
}

type ossPresignRequest struct {
	ObjectKey      string `json:"objectKey"`
	ExpiresMinutes int    `json:"expiresMinutes"`
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
	payload.Bucket = strings.TrimSpace(payload.Bucket)
	payload.AccessKeyID = strings.TrimSpace(payload.AccessKeyID)
	payload.AccessKeySecret = strings.TrimSpace(payload.AccessKeySecret)
	payload.Prefix = normalizeObjectPrefix(payload.Prefix)
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
	if payload.Enabled && (payload.Region == "" || payload.Endpoint == "" || payload.Bucket == "" || payload.AccessKeyID == "" || payload.AccessKeySecret == "") {
		writeError(w, http.StatusBadRequest, "启用 OSS 前需要填写 Region、Endpoint、Bucket、AccessKeyId 和 AccessKeySecret")
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

func (s *Server) uploadOSSFile(w http.ResponseWriter, r *http.Request) {
	client, cfg, err := s.ossClient(r.Context())
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxOSSUploadBytes)
	if err := r.ParseMultipartForm(maxOSSUploadBytes); err != nil {
		writeError(w, http.StatusBadRequest, "上传内容过大或表单格式不正确")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "请选择要上传的文件")
		return
	}
	defer file.Close()

	ext := strings.ToLower(filepath.Ext(header.Filename))
	if !allowedUploadExtension(ext) {
		writeError(w, http.StatusBadRequest, "当前文件类型不允许上传")
		return
	}
	category := normalizeObjectSegment(r.FormValue("category"))
	if category == "" {
		category = "misc"
	}
	source := strings.TrimSpace(r.FormValue("source"))
	objectPrefix := cfg.Prefix
	if requestedPrefix := normalizeObjectPrefix(r.FormValue("prefix")); requestedPrefix != "" {
		objectPrefix = requestedPrefix
	}
	objectKey := buildOSSObjectKey(objectPrefix, category, header.Filename)
	contentType := header.Header.Get("Content-Type")
	if contentType == "" {
		contentType = mime.TypeByExtension(ext)
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	tmp, err := os.CreateTemp("", "mcmods-oss-*"+ext)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "创建临时文件失败")
		return
	}
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
	}()
	hash := sha256.New()
	size, err := io.Copy(tmp, io.TeeReader(file, hash))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取上传文件失败")
		return
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		writeError(w, http.StatusInternalServerError, "准备上传文件失败")
		return
	}

	_, err = client.PutObject(
		r.Context(),
		&aliyunoss.PutObjectRequest{
			Bucket:        aliyunoss.Ptr(cfg.Bucket),
			Key:           aliyunoss.Ptr(objectKey),
			ContentType:   aliyunoss.Ptr(contentType),
			ContentLength: aliyunoss.Ptr(size),
			Body:          tmp,
		},
	)
	if err != nil {
		s.insertOSSUploadLog(r.Context(), nil, currentClaims(r).Subject, objectKey, header.Filename, size, requestIP(r), r.UserAgent(), "failed", err.Error())
		writeError(w, http.StatusBadGateway, "上传到阿里云 OSS 失败")
		return
	}

	var fileID int64
	err = s.db.QueryRow(
		r.Context(),
		`insert into oss_files (bucket, endpoint, region, object_key, category, source, original_name, content_type, size_bytes, sha256, uploader_id, status, scan_status)
		 values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, 'active', 'pending')
		 on conflict (object_key) do update
		 set category = excluded.category,
		     source = excluded.source,
		     original_name = excluded.original_name,
		     content_type = excluded.content_type,
		     size_bytes = excluded.size_bytes,
		     sha256 = excluded.sha256,
		     uploader_id = excluded.uploader_id,
		     updated_at = now()
		 returning id`,
		cfg.Bucket,
		cfg.Endpoint,
		cfg.Region,
		objectKey,
		category,
		source,
		header.Filename,
		contentType,
		size,
		hex.EncodeToString(hash.Sum(nil)),
		currentClaims(r).Subject,
	).Scan(&fileID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "保存 OSS 文件记录失败")
		return
	}
	s.insertOSSUploadLog(r.Context(), &fileID, currentClaims(r).Subject, objectKey, header.Filename, size, requestIP(r), r.UserAgent(), "success", "")
	_, _ = s.db.Exec(
		r.Context(),
		`insert into oss_scan_logs (file_id, object_key, engine, result, message)
		 values ($1, $2, 'manual', 'pending', '等待接入文件查杀引擎')`,
		fileID,
		objectKey,
	)
	writeJSON(w, http.StatusCreated, map[string]any{
		"id":           fileID,
		"bucket":       cfg.Bucket,
		"objectKey":    objectKey,
		"category":     category,
		"source":       source,
		"originalName": header.Filename,
		"contentType":  contentType,
		"sizeBytes":    size,
		"sha256":       hex.EncodeToString(hash.Sum(nil)),
		"scanStatus":   "pending",
	})
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
		`select id, bucket, endpoint, region, object_key, category, source, original_name, content_type, size_bytes, sha256, status, scan_status, created_at, updated_at
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
		var id, size int64
		var bucket, endpoint, region, objectKey, category, source, originalName, contentType, sha, status, scanStatus string
		var createdAt, updatedAt time.Time
		if err := rows.Scan(&id, &bucket, &endpoint, &region, &objectKey, &category, &source, &originalName, &contentType, &size, &sha, &status, &scanStatus, &createdAt, &updatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "解析 OSS 文件目录失败")
			return
		}
		files = append(files, map[string]any{
			"id":           id,
			"bucket":       bucket,
			"endpoint":     endpoint,
			"region":       region,
			"objectKey":    objectKey,
			"category":     category,
			"source":       source,
			"originalName": originalName,
			"contentType":  contentType,
			"sizeBytes":    size,
			"sha256":       sha,
			"status":       status,
			"scanStatus":   scanStatus,
			"createdAt":    createdAt,
			"updatedAt":    updatedAt,
		})
	}
	writeJSON(w, http.StatusOK, files)
}

func (s *Server) presignOSSFile(w http.ResponseWriter, r *http.Request) {
	var req ossPresignRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式不正确")
		return
	}
	req.ObjectKey = strings.TrimSpace(req.ObjectKey)
	if req.ObjectKey == "" {
		writeError(w, http.StatusBadRequest, "缺少 OSS ObjectKey")
		return
	}
	client, cfg, err := s.ossClient(r.Context())
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	if req.ExpiresMinutes <= 0 {
		req.ExpiresMinutes = cfg.DownloadURLTTLMinutes
	}
	if req.ExpiresMinutes > 10080 {
		req.ExpiresMinutes = 10080
	}
	expires := time.Duration(req.ExpiresMinutes) * time.Minute
	result, err := client.Presign(
		r.Context(),
		&aliyunoss.GetObjectRequest{
			Bucket: aliyunoss.Ptr(cfg.Bucket),
			Key:    aliyunoss.Ptr(req.ObjectKey),
		},
		aliyunoss.PresignExpires(expires),
	)
	if err != nil {
		writeError(w, http.StatusBadGateway, "生成 OSS 临时下载链接失败")
		return
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
		"url":       result.URL,
		"expiresAt": time.Now().Add(expires),
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
	payload := ossConfigPayload{Prefix: "mcmods", DownloadURLTTLMinutes: 10}
	var raw []byte
	err := s.db.QueryRow(ctx, `select value from system_settings where key = 'oss.aliyun'`).Scan(&raw)
	if err != nil {
		_ = ignoreNoRows(err)
		return payload
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return ossConfigPayload{Prefix: "mcmods", DownloadURLTTLMinutes: 10}
	}
	payload.Endpoint = normalizeOSSEndpoint(payload.Endpoint)
	payload.Prefix = normalizeObjectPrefix(payload.Prefix)
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
	ossCfg := aliyunoss.LoadDefaultConfig().
		WithCredentialsProvider(credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.AccessKeySecret)).
		WithRegion(cfg.Region).
		WithEndpoint(cfg.Endpoint).
		WithUseCName(cfg.UseCName || isCustomOSSEndpoint(cfg.Endpoint))
	return aliyunoss.NewClient(ossCfg), cfg, nil
}

func redactOSSConfig(payload ossConfigPayload) map[string]any {
	return map[string]any{
		"enabled":                 payload.Enabled,
		"region":                  payload.Region,
		"endpoint":                payload.Endpoint,
		"bucket":                  payload.Bucket,
		"accessKeyId":             payload.AccessKeyID,
		"hasAccessKeySecret":      strings.TrimSpace(payload.AccessKeySecret) != "",
		"useCName":                payload.UseCName || isCustomOSSEndpoint(payload.Endpoint),
		"prefix":                  payload.Prefix,
		"downloadUrlTtlMinutes":   payload.DownloadURLTTLMinutes,
		"bucketAccessPolicy":      "private-read-write",
		"temporaryDownloadPolicy": "presigned-url",
	}
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

func buildOSSObjectKey(prefix string, category string, filename string) string {
	now := time.Now()
	random := make([]byte, 8)
	_, _ = rand.Read(random)
	name := sanitizeFilename(filename)
	return path.Join(prefix, category, now.Format("2006/01/02"), hex.EncodeToString(random)+"-"+name)
}

func sanitizeFilename(filename string) string {
	filename = path.Base(strings.ReplaceAll(filename, "\\", "/"))
	if filename == "." || filename == "/" || filename == "" {
		return "file"
	}
	replacer := strings.NewReplacer(" ", "-", "\t", "-", "\n", "-", "\r", "-")
	filename = replacer.Replace(filename)
	var builder strings.Builder
	for _, r := range filename {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("._-+", r) {
			builder.WriteRune(r)
		}
	}
	if builder.Len() == 0 {
		return "file"
	}
	return builder.String()
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

func isCustomOSSEndpoint(endpoint string) bool {
	endpoint = strings.ToLower(endpoint)
	return endpoint != "" && !strings.Contains(endpoint, ".aliyuncs.com") && !strings.Contains(endpoint, ".aliyun.com")
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

func allowedUploadExtension(ext string) bool {
	allowed := map[string]struct{}{
		".jpg": {}, ".jpeg": {}, ".png": {}, ".gif": {}, ".webp": {}, ".bmp": {}, ".svg": {},
		".mp4": {}, ".webm": {}, ".mov": {}, ".avi": {},
		".pdf": {}, ".doc": {}, ".docx": {}, ".xls": {}, ".xlsx": {}, ".ppt": {}, ".pptx": {}, ".md": {},
		".txt": {}, ".log": {}, ".json": {}, ".nbt": {}, ".schem": {}, ".schematic": {},
		".zip": {}, ".rar": {}, ".7z": {}, ".jar": {}, ".gz": {}, ".tar": {},
	}
	_, ok := allowed[ext]
	return ok
}
