package httpapi

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	aliyunoss "github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
	"github.com/jackc/pgx/v5"
)

const maxExternalModIconBytes = int64(8 << 20)
const externalImageExpiryReason = "abandoned_external_image_import"

var errUnsupportedExternalModIcon = errors.New("unsupported external mod icon URL")

type mirroredExternalImage struct {
	URL            string
	FilePublicID   string
	FileInternalID int64
}

func (s *Server) mirrorExternalProjectIcon(ctx context.Context, sourceURL, projectType, projectUniqueID string, uploaderID int64) (string, error) {
	projectUniqueID = normalizeProjectObjectSegment(projectUniqueID)
	result, err := s.mirrorExternalImage(ctx, sourceURL, uploaderID, externalImageMirrorOptions{
		category:  ossProjectCategory(projectType, projectUniqueID, "icons", "project", "original"),
		source:    normalizeObjectSegment(projectType) + "_metadata_import",
		baseName:  "icon",
		userAgent: "mcmods.cn project metadata importer",
	})
	return result.URL, err
}

func (s *Server) mirrorExternalCreatorAvatar(ctx context.Context, sourceURL, kind, name string, uploaderID int64) (mirroredExternalImage, error) {
	identityDigest := sha256Hex([]byte(strings.ToLower(strings.TrimSpace(kind)) + "\x00" + normalizeCreatorName(name)))
	return s.mirrorExternalImage(ctx, sourceURL, uploaderID, externalImageMirrorOptions{
		category:  ossProjectCategory(kind, "import-"+identityDigest[:16], "icons", "profile", "original"),
		source:    "creator_metadata_import",
		baseName:  "avatar",
		userAgent: "mcmods.cn creator metadata importer",
	})
}

type externalImageMirrorOptions struct {
	category  string
	source    string
	baseName  string
	userAgent string
}

func (s *Server) mirrorExternalImage(ctx context.Context, sourceURL string, uploaderID int64, options externalImageMirrorOptions) (mirroredExternalImage, error) {
	sourceURL = strings.TrimSpace(sourceURL)
	if sourceURL == "" {
		return mirroredExternalImage{}, nil
	}
	if uploaderID <= 0 {
		return mirroredExternalImage{}, errUnsupportedExternalModIcon
	}
	ossClient, ossCfg, err := s.ossClient(ctx)
	if err != nil {
		return mirroredExternalImage{}, err
	}
	if objectKey, ok := ossObjectKeyUnderEndpoint(sourceURL, ossCfg.PublicEndpoint); ok {
		record, err := loadExternalImageRecord(ctx, s.db, ossCfg.Bucket, objectKey, options, uploaderID, false)
		if err != nil {
			return mirroredExternalImage{}, err
		}
		if err = record.verify(ctx, ossClient, ossCfg, objectKey); err != nil {
			return mirroredExternalImage{}, err
		}
		return record.result, nil
	}
	parsed, err := url.Parse(sourceURL)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.Port() != "" || !isAllowedExternalModIconHost(parsed.Hostname()) {
		return mirroredExternalImage{}, errUnsupportedExternalModIcon
	}
	client := &http.Client{
		Timeout: 20 * time.Second,
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			if len(via) >= 5 || request.URL.Scheme != "https" || request.URL.User != nil || request.URL.Port() != "" || !isAllowedExternalModIconHost(request.URL.Hostname()) {
				return errors.New("external mod icon redirect is not allowed")
			}
			return nil
		},
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		return mirroredExternalImage{}, err
	}
	request.Header.Set("Accept", "image/webp,image/png,image/jpeg,image/gif")
	request.Header.Set("User-Agent", options.userAgent)
	response, err := client.Do(request)
	if err != nil {
		return mirroredExternalImage{}, fmt.Errorf("download external image: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return mirroredExternalImage{}, fmt.Errorf("download external image: HTTP %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxExternalModIconBytes+1))
	if err != nil {
		return mirroredExternalImage{}, err
	}
	if len(data) == 0 || int64(len(data)) > maxExternalModIconBytes {
		return mirroredExternalImage{}, errors.New("external image is empty or too large")
	}
	return s.storeExternalImage(ctx, ossClient, ossCfg, parsed, options, uploaderID, data)
}

// Record ownership before a conditional upload. A failed transfer leaves a private
// pending receipt; replay verifies any already-written bytes before activating it.
func (s *Server) storeExternalImage(ctx context.Context, client *aliyunoss.Client, cfg ossConfigPayload, sourceURL *url.URL, options externalImageMirrorOptions, uploaderID int64, data []byte) (mirroredExternalImage, error) {
	contentType, extension, err := externalModIconFormat(data)
	if err != nil {
		return mirroredExternalImage{}, err
	}
	digest := sha256Hex(data)
	objectKey := externalImageObjectKey(cfg.Prefix, options, uploaderID, digest, extension)
	originalName := path.Base(sourceURL.Path)
	if originalName == "." || originalName == "/" || originalName == "" {
		originalName = digest + extension
	}
	objectKey, err = s.reserveExternalImage(ctx, cfg, options, uploaderID, digest, extension, objectKey, originalName, contentType, int64(len(data)))
	if err != nil {
		return mirroredExternalImage{}, err
	}
	// Serialize only an upload to this exact key and its lifecycle changes. A
	// bounded transaction prevents deletion or a rescan from crossing activation.
	ioCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	tx, err := s.db.Begin(ioCtx)
	if err != nil {
		return mirroredExternalImage{}, err
	}
	defer tx.Rollback(ioCtx)
	record, err := loadExternalImageRecord(ioCtx, tx, cfg.Bucket, objectKey, options, uploaderID, true)
	if err != nil {
		return mirroredExternalImage{}, err
	}
	if record.storedSHA256 != digest || record.size != int64(len(data)) || record.contentType != contentType {
		return mirroredExternalImage{}, errUnsupportedExternalModIcon
	}
	if !record.pending {
		if err = tx.Rollback(ioCtx); err != nil {
			return mirroredExternalImage{}, err
		}
		if err = record.verify(ioCtx, client, cfg, objectKey); err != nil {
			return mirroredExternalImage{}, err
		}
		return record.result, nil
	}
	_, err = client.PutObject(ioCtx, &aliyunoss.PutObjectRequest{
		Bucket: aliyunoss.Ptr(cfg.Bucket), Key: aliyunoss.Ptr(objectKey),
		ContentType: aliyunoss.Ptr(contentType), ContentLength: aliyunoss.Ptr(int64(len(data))),
		ForbidOverwrite: aliyunoss.Ptr("true"), Body: bytes.NewReader(data),
		Metadata: map[string]string{"sha256": digest, "source-host": strings.ToLower(sourceURL.Hostname())},
	})
	if err != nil {
		var service *aliyunoss.ServiceError
		if !errors.As(err, &service) || service.StatusCode != http.StatusConflict || service.Code != "FileAlreadyExists" {
			return mirroredExternalImage{}, fmt.Errorf("upload external image to OSS: %w", err)
		}
		if err = record.verify(ioCtx, client, cfg, objectKey); err != nil {
			return mirroredExternalImage{}, err
		}
	}
	_, err = tx.Exec(ioCtx, `update oss_files set status='active',scan_status='clean',updated_at=now() where id=$1`, record.result.FileInternalID)
	if err != nil {
		return mirroredExternalImage{}, err
	}
	// The same file→outbox order is used by expiry and administrative deletion.
	// A successful write no longer needs its delayed cleanup intent.
	if _, err = tx.Exec(ioCtx, `delete from oss_object_deletion_outbox where oss_file_id=$1 and reason=$2`, record.result.FileInternalID, externalImageExpiryReason); err != nil {
		return mirroredExternalImage{}, err
	}
	if err = tx.Commit(ioCtx); err != nil {
		return mirroredExternalImage{}, err
	}
	record.result.URL = ossStoredObjectURL(cfg, objectKey)
	return record.result, nil
}

func (s *Server) reserveExternalImage(ctx context.Context, cfg ossConfigPayload, options externalImageMirrorOptions, uploaderID int64, digest, extension, initialKey, name, contentType string, size int64) (string, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	var key, previousID string
	var expired bool
	err = tx.QueryRow(ctx, `select file.object_key,file.public_id,file.status='deleted' and exists(
        select 1 from oss_object_deletion_outbox where bucket=file.bucket and endpoint=file.endpoint and object_key=file.object_key
        and oss_file_id=file.id and reason=$7)
        from oss_files file where file.bucket=$1 and file.category=$2 and file.source=$3 and file.uploader_id=$4
        and file.sha256=$5 and file.size_bytes=$6 order by file.id desc limit 1`, cfg.Bucket, options.category, options.source, uploaderID, digest, size, externalImageExpiryReason).
		Scan(&key, &previousID, &expired)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		key = initialKey
	case err != nil:
		return "", err
	case expired:
		// Never reuse a key addressed by an old deletion job, even after replay.
		key = strings.TrimSuffix(initialKey, extension) + "-" + previousID + extension
	default:
		return key, nil
	}
	var fileID int64
	err = tx.QueryRow(ctx, `insert into oss_files(bucket,endpoint,region,object_key,category,source,original_name,source_original_name,
        content_type,size_bytes,source_size_bytes,sha256,uploader_id,status,scan_status)
        values($1,$2,$3,$4,$5,$6,$7,$7,$8,$9,$9,$10,$11,'pending','pending')
        on conflict(object_key) do nothing returning id`, cfg.Bucket, cfg.Endpoint, cfg.Region, key, options.category, options.source, name, contentType, size, digest, uploaderID).Scan(&fileID)
	if errors.Is(err, pgx.ErrNoRows) {
		return key, nil
	}
	if err != nil {
		return "", err
	}
	// Use the existing deletion queue and its due-time index. Failed or
	// interrupted transfers remain private and recoverable for a full day.
	_, err = scheduleOSSUploadExpiry(ctx, tx, cfg, key, externalImageExpiryReason, &fileID)
	if err != nil {
		return "", err
	}
	return key, tx.Commit(ctx)
}

func externalImageObjectKey(prefix string, options externalImageMirrorOptions, uploaderID int64, digest, extension string) string {
	scopedDigest := sha256Hex([]byte(fmt.Sprintf("%d\x00%s\x00%s\x00%s", uploaderID, options.source, options.category, digest)))
	name := normalizeObjectSegment(options.baseName)
	if name == "" {
		name = "image"
	}
	return path.Join(ossObjectPrefix(prefix, options.category), name+"-"+scopedDigest+extension)
}

func externalModIconFormat(data []byte) (string, string, error) {
	contentType := normalizeRasterContentType(http.DetectContentType(data))
	if _, err := validateRasterImageBytes(data, contentType); err != nil {
		return "", "", fmt.Errorf("invalid external mod icon: %w", err)
	}
	switch contentType {
	case "image/png":
		return contentType, ".png", nil
	case "image/jpeg":
		return contentType, ".jpg", nil
	case "image/gif":
		return contentType, ".gif", nil
	case "image/webp":
		return contentType, ".webp", nil
	default:
		return "", "", fmt.Errorf("unsupported external mod icon content type: %s", contentType)
	}
}

type reusableExternalImageRecord struct {
	pending      bool
	result       mirroredExternalImage
	contentType  string
	storedSHA256 string
	size         int64
}

func loadExternalImageRecord(ctx context.Context, query revisionQuery, bucket, objectKey string, options externalImageMirrorOptions, uploaderID int64, forImport bool) (reusableExternalImageRecord, error) {
	if uploaderID <= 0 || options.source == "" || options.category == "" {
		return reusableExternalImageRecord{}, errUnsupportedExternalModIcon
	}
	statement := `select id,public_id,content_type,sha256,size_bytes,(status='pending') from oss_files
        where object_key=$1 and category=$2 and source=$3 and uploader_id=$4 and bucket=$5
          and (status='active' and scan_status in ('clean','trusted_generated')
               or $6 and status='pending' and scan_status='pending')
          and lower(split_part(content_type,';',1)) in ('image/png','image/jpeg','image/jpg','image/gif','image/webp')`
	if forImport {
		statement += " for update"
	}
	var record reusableExternalImageRecord
	err := query.QueryRow(ctx, statement, objectKey, options.category, options.source, uploaderID, bucket, forImport).
		Scan(&record.result.FileInternalID, &record.result.FilePublicID, &record.contentType, &record.storedSHA256, &record.size, &record.pending)
	if errors.Is(err, pgx.ErrNoRows) {
		return reusableExternalImageRecord{}, errUnsupportedExternalModIcon
	}
	if err != nil {
		return reusableExternalImageRecord{}, err
	}
	if record.size <= 0 || record.size > maxExternalModIconBytes || normalizeSHA256(record.storedSHA256) == "" {
		return reusableExternalImageRecord{}, errUnsupportedExternalModIcon
	}
	return record, nil
}

func (record *reusableExternalImageRecord) verify(ctx context.Context, client *aliyunoss.Client, cfg ossConfigPayload, objectKey string) error {
	result, err := client.GetObject(ctx, &aliyunoss.GetObjectRequest{Bucket: aliyunoss.Ptr(cfg.Bucket), Key: aliyunoss.Ptr(objectKey)})
	if err != nil {
		return err
	}
	defer result.Body.Close()
	data, err := io.ReadAll(io.LimitReader(result.Body, record.size+1))
	if err != nil {
		return err
	}
	if int64(len(data)) != record.size || sha256Hex(data) != record.storedSHA256 {
		return errUnsupportedExternalModIcon
	}
	if _, err = validateRasterImageBytes(data, record.contentType); err != nil {
		return err
	}
	record.result.URL = ossStoredObjectURL(cfg, objectKey)
	return nil
}

func ossObjectKeyUnderEndpoint(rawURL, endpoint string) (string, bool) {
	parsedURL, urlErr := url.Parse(strings.TrimSpace(rawURL))
	parsedEndpoint, endpointErr := url.Parse(strings.TrimSpace(endpoint))
	if urlErr != nil || endpointErr != nil || parsedURL.Scheme != "https" || parsedEndpoint.Scheme != "https" ||
		!strings.EqualFold(parsedURL.Host, parsedEndpoint.Host) {
		return "", false
	}
	basePath := strings.TrimRight(parsedEndpoint.EscapedPath(), "/")
	escapedPath := parsedURL.EscapedPath()
	if basePath != "" {
		if !strings.HasPrefix(escapedPath, basePath+"/") {
			return "", false
		}
		escapedPath = strings.TrimPrefix(escapedPath, basePath+"/")
	} else {
		escapedPath = strings.TrimPrefix(escapedPath, "/")
	}
	objectKey, err := url.PathUnescape(escapedPath)
	if err != nil || objectKey == "" || strings.HasPrefix(objectKey, "/") || path.Clean(objectKey) != objectKey || strings.Contains(objectKey, `\`) {
		return "", false
	}
	for _, segment := range strings.Split(objectKey, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return "", false
		}
	}
	return objectKey, true
}

func isAllowedExternalModIconHost(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
	return host == "cdn.modrinth.com" || host == "avatars.githubusercontent.com" || host == "raw.githubusercontent.com" || host == "user-images.githubusercontent.com" || host == "forgecdn.net" || strings.HasSuffix(host, ".forgecdn.net")
}
