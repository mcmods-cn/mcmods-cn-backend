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
)

const maxExternalModIconBytes = int64(8 << 20)

var errUnsupportedExternalModIcon = errors.New("unsupported external mod icon URL")

func (s *Server) mirrorExternalModIcon(ctx context.Context, sourceURL, projectUniqueID string, uploaderID int64) (string, error) {
	sourceURL = strings.TrimSpace(sourceURL)
	if sourceURL == "" {
		return "", nil
	}
	ossCfg := s.ossConfigFromSettings(ctx)
	if objectKey, ok := ossObjectKeyUnderEndpoint(sourceURL, ossCfg.PublicEndpoint); ok {
		ossClient, loadedCfg, clientErr := s.ossClient(ctx)
		if clientErr != nil {
			return "", clientErr
		}
		if s.reusableExternalModIconObject(ctx, ossClient, loadedCfg, objectKey, "", 0) {
			return buildPublicOSSURL(loadedCfg, objectKey), nil
		}
		return "", errUnsupportedExternalModIcon
	}
	parsed, err := url.Parse(sourceURL)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.Port() != "" || !isAllowedExternalModIconHost(parsed.Hostname()) {
		return "", errUnsupportedExternalModIcon
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
		return "", err
	}
	request.Header.Set("Accept", "image/webp,image/png,image/jpeg,image/gif")
	request.Header.Set("User-Agent", "mcmods.cn mod metadata importer")
	response, err := client.Do(request)
	if err != nil {
		return "", fmt.Errorf("download external mod icon: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("download external mod icon: HTTP %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxExternalModIconBytes+1))
	if err != nil {
		return "", err
	}
	if len(data) == 0 || int64(len(data)) > maxExternalModIconBytes {
		return "", errors.New("external mod icon is empty or too large")
	}
	contentType, extension, err := externalModIconFormat(data)
	if err != nil {
		return "", err
	}
	digest := sha256Hex(data)
	projectUniqueID = normalizeProjectObjectSegment(projectUniqueID)
	objectKey := modIconObjectKey(ossCfg.Prefix, projectUniqueID, digest, extension)
	ossClient, loadedCfg, clientErr := s.ossClient(ctx)
	if clientErr != nil {
		return "", clientErr
	}
	ossCfg = loadedCfg
	reusable := s.reusableExternalModIconObject(ctx, ossClient, ossCfg, objectKey, digest, int64(len(data)))
	if !reusable {
		_, err = ossClient.PutObject(ctx, &aliyunoss.PutObjectRequest{
			Bucket:        aliyunoss.Ptr(ossCfg.Bucket),
			Key:           aliyunoss.Ptr(objectKey),
			ContentType:   aliyunoss.Ptr(contentType),
			ContentLength: aliyunoss.Ptr(int64(len(data))),
			Body:          bytes.NewReader(data),
			Metadata:      map[string]string{"sha256": digest, "source-host": strings.ToLower(parsed.Hostname())},
		})
		if err != nil {
			return "", fmt.Errorf("upload mod icon to OSS: %w", err)
		}
		originalName := path.Base(parsed.Path)
		if originalName == "." || originalName == "/" || originalName == "" {
			originalName = digest + extension
		}
		_, err = s.db.Exec(ctx, `
			insert into oss_files(bucket,endpoint,region,object_key,category,source,original_name,source_original_name,content_type,size_bytes,source_size_bytes,sha256,uploader_id,status,scan_status)
			values($1,$2,$3,$4,$5,'mod_metadata_import',$6,$6,$7,$8,$8,$9,$10,'active','clean')
			on conflict(object_key) do update set updated_at=now(),status='active',scan_status='clean'`,
			ossCfg.Bucket, ossCfg.displayEndpoint(), ossCfg.Region, objectKey,
			ossProjectCategory("mod", projectUniqueID, "icons", "project", "original"), originalName, contentType, len(data), digest, nullableUserID(uploaderID))
		if err != nil {
			s.deleteOSSObjectIfUnregistered(context.Background(), ossClient, ossCfg, objectKey)
			return "", err
		}
	}
	return buildPublicOSSURL(ossCfg, objectKey), nil
}

func modIconObjectKey(prefix, projectUniqueID, digest, extension string) string {
	digest = strings.ToLower(strings.TrimSpace(digest))
	if len(digest) > 16 {
		digest = digest[:16]
	}
	return path.Join(ossObjectPrefix(prefix, ossProjectCategory("mod", projectUniqueID, "icons", "project", "original")), "icon-"+digest+extension)
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

func (s *Server) reusableExternalModIconObject(ctx context.Context, client *aliyunoss.Client, cfg ossConfigPayload, objectKey, expectedSHA256 string, expectedSize int64) bool {
	var contentType, storedSHA256 string
	var size int64
	query := `select content_type,sha256,size_bytes from oss_files
		where object_key=$1 and status='active' and scan_status in ('clean','trusted_generated')
		  and lower(split_part(content_type,';',1)) in ('image/png','image/jpeg','image/jpg','image/gif','image/webp')`
	args := []any{objectKey}
	if expectedSHA256 != "" {
		args = append(args, expectedSHA256)
		query += fmt.Sprintf(` and sha256=$%d`, len(args))
	}
	if expectedSize > 0 {
		args = append(args, expectedSize)
		query += fmt.Sprintf(` and size_bytes=$%d`, len(args))
	}
	if err := s.db.QueryRow(ctx, query, args...).Scan(&contentType, &storedSHA256, &size); err != nil || size <= 0 || size > maxExternalModIconBytes {
		return false
	}
	result, err := client.GetObject(ctx, &aliyunoss.GetObjectRequest{
		Bucket: aliyunoss.Ptr(cfg.Bucket),
		Key:    aliyunoss.Ptr(objectKey),
	})
	if err != nil {
		return false
	}
	defer result.Body.Close()
	data, err := io.ReadAll(io.LimitReader(result.Body, maxExternalModIconBytes+1))
	if err != nil || int64(len(data)) != size || sha256Hex(data) != storedSHA256 {
		return false
	}
	_, err = validateRasterImageBytes(data, contentType)
	return err == nil
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

func isURLUnderEndpoint(rawURL, endpoint string) bool {
	parsedURL, urlErr := url.Parse(strings.TrimSpace(rawURL))
	parsedEndpoint, endpointErr := url.Parse(strings.TrimSpace(endpoint))
	if urlErr != nil || endpointErr != nil || parsedURL.Scheme != "https" || parsedEndpoint.Scheme != "https" {
		return false
	}
	return strings.EqualFold(parsedURL.Host, parsedEndpoint.Host) && strings.HasPrefix(parsedURL.EscapedPath(), strings.TrimRight(parsedEndpoint.EscapedPath(), "/")+"/")
}
