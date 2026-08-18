package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	aliyunoss "github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
	"github.com/jackc/pgx/v5"
)

type ossObjectAccessOptions struct {
	Expires            time.Duration
	ContentDisposition string
}

type ossObjectAccess struct {
	URL       string
	Mode      string
	ExpiresAt time.Time
}

// writeGeneratedOSSObject is the shared boundary for trusted files generated
// by backend workers. Business workers provide bytes and ownership metadata;
// OSS configuration and the persistent file record stay centralized here.
func (s *Server) writeGeneratedOSSObject(ctx context.Context, objectKey, originalName, contentType string, data []byte, uploaderID int64, source string) (int64, error) {
	client, cfg, err := s.ossClient(ctx)
	if err != nil {
		return 0, err
	}
	digest := sha256.Sum256(data)
	sha := hex.EncodeToString(digest[:])
	_, err = client.PutObject(ctx, &aliyunoss.PutObjectRequest{
		Bucket: aliyunoss.Ptr(cfg.Bucket), Key: aliyunoss.Ptr(objectKey), ContentType: aliyunoss.Ptr(contentType),
		ContentLength: aliyunoss.Ptr(int64(len(data))), Body: bytes.NewReader(data), Metadata: map[string]string{"sha256": sha},
	})
	if err != nil {
		return 0, err
	}
	var fileID int64
	err = s.db.QueryRow(ctx, `insert into oss_files(bucket,endpoint,region,object_key,category,source,original_name,source_original_name,content_type,size_bytes,source_size_bytes,sha256,uploader_id,status,scan_status)
		values($1,$2,$3,$4,$5,$6,$7,$7,$8,$9,$9,$10,$11,'active','trusted_generated')
		on conflict(object_key) do update set bucket=excluded.bucket,endpoint=excluded.endpoint,region=excluded.region,
			category=excluded.category,source=excluded.source,original_name=excluded.original_name,source_original_name=excluded.source_original_name,
			content_type=excluded.content_type,size_bytes=excluded.size_bytes,source_size_bytes=excluded.source_size_bytes,sha256=excluded.sha256,
			uploader_id=excluded.uploader_id,status='active',scan_status='trusted_generated' returning id`,
		cfg.Bucket, cfg.displayEndpoint(), cfg.Region, objectKey, ossCategoryFromObjectKey(objectKey, cfg.Prefix), source, originalName, contentType, len(data), sha, uploaderID).Scan(&fileID)
	return fileID, err
}

// resolveOSSObjectAccess is the single policy boundary for URLs that read an
// OSS object. It intentionally does not handle upload (PUT/multipart) signing.
func (s *Server) resolveOSSObjectAccess(ctx context.Context, objectKey string, options ossObjectAccessOptions) (ossObjectAccess, error) {
	cfg := s.ossConfigFromSettings(ctx)
	return s.resolveOSSObjectAccessWithConfig(ctx, cfg, objectKey, options)
}

func (s *Server) resolveOSSObjectAccessWithConfig(
	ctx context.Context,
	cfg ossConfigPayload,
	objectKey string,
	options ossObjectAccessOptions,
) (ossObjectAccess, error) {
	objectKey = strings.TrimSpace(objectKey)
	if objectKey == "" {
		return ossObjectAccess{}, fmt.Errorf("missing OSS ObjectKey")
	}
	expires := options.Expires
	if expires <= 0 {
		expires = time.Duration(cfg.DownloadURLTTLMinutes) * time.Minute
	}
	if expires <= 0 {
		expires = 10 * time.Minute
	}
	access := ossObjectAccess{
		Mode:      normalizeOSSDownloadMode(cfg.DownloadURLMode),
		ExpiresAt: time.Now().Add(expires),
	}
	if access.Mode == ossDownloadModeESAPrivateOrigin {
		access.URL = ossStoredObjectURLWithDisposition(cfg, objectKey, options.ContentDisposition)
		return access, nil
	}
	client, err := s.ossDownloadClient(ctx, cfg)
	if err != nil {
		return ossObjectAccess{}, err
	}
	request := &aliyunoss.GetObjectRequest{
		Bucket: aliyunoss.Ptr(cfg.Bucket),
		Key:    aliyunoss.Ptr(objectKey),
	}
	if options.ContentDisposition != "" {
		request.ResponseContentDisposition = aliyunoss.Ptr(options.ContentDisposition)
	}
	result, err := client.Presign(ctx, request, aliyunoss.PresignExpires(expires))
	if err != nil {
		return ossObjectAccess{}, fmt.Errorf("presign OSS object access: %w", err)
	}
	access.URL = result.URL
	return access, nil
}

func (s *Server) redirectOSSObjectAccess(w http.ResponseWriter, r *http.Request, objectKey string, options ossObjectAccessOptions) bool {
	access, err := s.resolveOSSObjectAccess(r.Context(), objectKey, options)
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed to generate OSS access URL")
		return false
	}
	w.Header().Set("Cache-Control", "private, no-store")
	http.Redirect(w, r, access.URL, http.StatusTemporaryRedirect)
	return true
}

// resolveStoredOSSObjectAccessURL upgrades a stable URL stored in a business
// record to the currently configured access mode. External URLs are returned
// unchanged.
func (s *Server) resolveStoredOSSObjectAccessURL(ctx context.Context, storedURL string) (string, error) {
	cfg := s.ossConfigFromSettings(ctx)
	return s.resolveStoredOSSObjectAccessURLWithConfig(ctx, cfg, storedURL)
}

func (s *Server) resolveStoredOSSObjectAccessURLWithConfig(ctx context.Context, cfg ossConfigPayload, storedURL string) (string, error) {
	storedURL = strings.TrimSpace(storedURL)
	if storedURL == "" {
		return "", nil
	}
	endpoints := []string{cfg.PublicEndpoint, cfg.Endpoint}
	for _, endpoint := range endpoints {
		if objectKey, ok := ossObjectKeyUnderEndpoint(storedURL, endpoint); ok {
			access, err := s.resolveOSSObjectAccessWithConfig(ctx, cfg, objectKey, ossObjectAccessOptions{})
			if err != nil {
				return "", err
			}
			return access.URL, nil
		}
	}
	return storedURL, nil
}

func (s *Server) redirectStoredRasterURL(w http.ResponseWriter, r *http.Request, storedURL string) {
	storedURL = strings.TrimSpace(storedURL)
	if storedURL == "" {
		writeError(w, http.StatusNotFound, "image does not exist")
		return
	}
	var objectKey, contentType string
	err := s.db.QueryRow(r.Context(), `select object_key,content_type from oss_files
		where status='active' and scan_status in ('clean','trusted_generated')
		  and $1=rtrim(endpoint,'/')||'/'||ltrim(object_key,'/')
		order by updated_at desc limit 1`, storedURL).Scan(&objectKey, &contentType)
	if err == nil {
		s.redirectCatalogOSSAsset(w, r, objectKey, contentType)
		return
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusInternalServerError, "failed to resolve stored image")
		return
	}
	if !validHTTPURL(storedURL) {
		writeError(w, http.StatusNotFound, "image does not exist")
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	http.Redirect(w, r, storedURL, http.StatusTemporaryRedirect)
}

func (s *Server) publicInlineOSSFile(w http.ResponseWriter, r *http.Request) {
	publicID := strings.ToLower(strings.TrimSpace(r.PathValue("publicId")))
	if publicID == "" {
		writeError(w, http.StatusBadRequest, "OSS file ID is invalid")
		return
	}
	var objectKey, source string
	err := s.db.QueryRow(r.Context(), `select object_key,source from oss_files
		where public_id=$1 and status='active' and scan_status in ('clean','trusted_generated')`, publicID).
		Scan(&objectKey, &source)
	if errors.Is(err, pgx.ErrNoRows) || !isPublicInlineOSSFileSource(source) {
		writeError(w, http.StatusNotFound, "public OSS file does not exist")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load public OSS file")
		return
	}
	s.redirectOSSObjectAccess(w, r, objectKey, ossObjectAccessOptions{})
}

func isPublicInlineOSSFileSource(source string) bool {
	source = strings.ToLower(strings.TrimSpace(source))
	return source == "playground" || source == "server-content" ||
		source == "sticker" ||
		strings.Contains(source, "_text:") || strings.HasPrefix(source, "mod_text:")
}

// ossStoredObjectURL is only a stable storage reference. Code serving or
// downloading the object must use resolveOSSObjectAccess instead so that the
// configured download URL mode is respected at request time.
func ossStoredObjectURL(cfg ossConfigPayload, objectKey string) string {
	endpoint := strings.TrimRight(cfg.PublicEndpoint, "/")
	if endpoint == "" {
		endpoint = strings.TrimRight(cfg.Endpoint, "/")
	}
	if endpoint == "" {
		return strings.TrimLeft(objectKey, "/")
	}
	return endpoint + "/" + strings.TrimLeft(objectKey, "/")
}
