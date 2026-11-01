package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	aliyunoss "github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
)

var errOSSUploadTicketActivity = errors.New("persist OSS upload ticket activity")

// Remote preparation and signing precede every database lock. Only after the
// complete authoritative ticket and its original activity commit is it issued.
func (s *Server) issueOSSUploadTicket(w http.ResponseWriter, r *http.Request, client *aliyunoss.Client, cfg ossConfigPayload, req ossDirectUploadRequest, scope, category, source, objectPrefix, objectCategory, contentType string, expiresAt time.Time, expires time.Duration, blueprint bool, limits ossUserQuotaLimits, deferStored bool) {
	blueprintPublicID := ""
	if blueprint {
		blueprintPublicID = randomCatalogPublicID()
		category = ossBlueprintReleaseCategory(blueprintPublicID, "original")
		objectCategory = category
		objectPrefix = ossRoot(cfg.Prefix)
	}
	objectKey := buildOSSObjectKeyForFile(objectPrefix, objectCategory, req.OriginalName)
	if isOSSSealedObjectKey(objectKey) {
		writeError(w, http.StatusBadRequest, "reserved OSS upload namespace")
		return
	}
	req.Category, req.Source, req.ContentType = category, source, contentType
	response := map[string]any{"bucket": cfg.Bucket, "objectKey": objectKey, "category": category, "source": source, "originalName": req.OriginalName, "contentType": contentType, "sizeBytes": req.SizeBytes, "sha256": req.SHA256, "uploadRequired": true, "expiresAt": expiresAt, "storageUrl": ossStoredObjectURL(cfg, objectKey)}
	if blueprintPublicID != "" {
		response["blueprintId"] = blueprintPublicID
		response["blueprint"] = map[string]any{"id": blueprintPublicID, "status": "uploading"}
	}
	uploadID := ""
	if shouldUseOSSMultipart(req.SizeBytes) {
		multipart, err := s.initiateOSSMultipartUpload(r.Context(), client, cfg, objectKey, contentType, req.SHA256, req.SizeBytes, expires)
		if err != nil {
			s.insertOSSUploadLog(r.Context(), nil, currentClaims(r).Subject, objectKey, req.OriginalName, req.SizeBytes, s.requestClientLocation(r).IP, r.UserAgent(), "failed", err.Error())
			writeError(w, http.StatusBadGateway, "生成 OSS 分片上传请求失败")
			return
		}
		uploadID = multipart.UploadID
		response["method"], response["multipart"] = "MULTIPART", multipart
	} else {
		result, err := client.Presign(r.Context(), &aliyunoss.PutObjectRequest{Bucket: aliyunoss.Ptr(cfg.Bucket), Key: aliyunoss.Ptr(objectKey), ContentType: aliyunoss.Ptr(contentType), ForbidOverwrite: aliyunoss.Ptr("true"), Metadata: map[string]string{"sha256": req.SHA256}}, aliyunoss.PresignExpires(expires))
		if err != nil {
			writeError(w, http.StatusBadGateway, "生成 OSS 上传链接失败")
			return
		}
		response["method"], response["url"], response["headers"] = result.Method, result.URL, result.SignedHeaders
	}
	if err := s.persistOSSUploadTicket(r, cfg, req, scope, objectKey, uploadID, blueprintPublicID, expiresAt, limits, deferStored); err != nil {
		// The capability has not been disclosed. Abort only the multipart upload
		// created above, and persist retryable cleanup if its provider is unavailable.
		if uploadID != "" {
			s.cleanupUnissuedOSSMultipart(r, cfg, req, objectKey, uploadID, expiresAt, client)
		}
		if errors.Is(err, errOSSUploadTicketActivity) {
			writeError(w, http.StatusInternalServerError, "failed to persist OSS upload ticket activity")
			return
		}
		var quotaErr *ossUserQuotaError
		if errors.As(err, &quotaErr) {
			writeOSSUserQuotaError(w, err)
			return
		}
		writeError(w, http.StatusInternalServerError, "保存 OSS 上传会话失败")
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) persistOSSUploadTicket(r *http.Request, cfg ossConfigPayload, req ossDirectUploadRequest, scope, objectKey, uploadID, blueprintPublicID string, expiresAt time.Time, limits ossUserQuotaLimits, deferStored bool) error {
	ctx := r.Context()
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if blueprintPublicID != "" {
		id, _, createErr := createPendingBlueprintTx(ctx, tx, currentClaims(r).Subject, req.OriginalName, expiresAt, blueprintPublicID)
		if createErr != nil {
			return createErr
		}
		if _, err = tx.Exec(ctx, `update blueprints set original_object_key=$2 where id=$1`, id, objectKey); err != nil {
			return err
		}
	}
	if scope == "user" {
		stored := req.SizeBytes
		if deferStored {
			stored = 0
		}
		if err = reserveUserOSSUploadQuotaTx(ctx, tx, currentClaims(r).Subject, objectKey, req.SizeBytes, stored, expiresAt, limits); err != nil {
			return err
		}
	}
	if uploadID != "" {
		if err = registerOSSMultipartSessionTx(ctx, tx, currentClaims(r).Subject, cfg, req, objectKey, uploadID, expiresAt); err != nil {
			return err
		}
	}
	if err = enqueueRequestActivityTx(r, tx); err != nil {
		return fmt.Errorf("%w: %w", errOSSUploadTicketActivity, err)
	}
	return tx.Commit(ctx)
}

func (s *Server) cleanupUnissuedOSSMultipart(r *http.Request, cfg ossConfigPayload, req ossDirectUploadRequest, objectKey, uploadID string, expiresAt time.Time, client *aliyunoss.Client) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := abortOSSMultipartUpload(ctx, client, cfg, objectKey, uploadID); err == nil {
		return
	}
	tx, err := s.db.Begin(ctx)
	if err == nil {
		defer tx.Rollback(ctx)
		err = registerOSSMultipartSessionTx(ctx, tx, currentClaims(r).Subject, cfg, req, objectKey, uploadID, expiresAt)
		if err == nil {
			_, err = tx.Exec(ctx, `update oss_multipart_sessions set status='cleanup_pending',next_attempt_at=now() where bucket=$1 and endpoint=$2 and object_key=$3 and upload_id=$4`, cfg.Bucket, cfg.Endpoint, objectKey, uploadID)
		}
		if err == nil {
			err = tx.Commit(ctx)
		}
	}
	if err != nil {
		s.observeOSSWriteFailure("unissued_multipart_cleanup", objectKey, err)
	}
}

var errOSSReuseCoverBinding = errors.New("bind reused OSS cover")

func (s *Server) commitOSSReuseTicket(r *http.Request, fileID, coverID int64, filePublicID, objectKey string, blueprint bool) (map[string]any, error) {
	ctx := r.Context()
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if coverID > 0 {
		if err = bindReusableBlueprintCoverTx(ctx, tx, coverID, currentClaims(r).Subject, filePublicID, objectKey); err != nil {
			return nil, fmt.Errorf("%w: %w", errOSSReuseCoverBinding, err)
		}
	}
	var result map[string]any
	if blueprint {
		result, err = s.blueprintForExistingFileTx(ctx, tx, fileID, currentClaims(r).Subject)
		if err != nil {
			return nil, err
		}
	}
	if err = enqueueRequestActivityTx(r, tx); err != nil {
		return nil, fmt.Errorf("%w: %w", errOSSUploadTicketActivity, err)
	}
	return result, tx.Commit(ctx)
}
