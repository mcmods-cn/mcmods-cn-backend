package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var (
	errOSSMultipartSessionForbidden = errors.New("OSS multipart session does not belong to the current user")
	errOSSMultipartSessionConflict  = errors.New("OSS multipart session cannot perform this action")
	errOSSMultipartSessionLeaseLost = errors.New("OSS multipart session lease was lost")
)

type ossMultipartSettlement struct {
	ID      int64
	Token   string
	Already bool
}

func (s *Server) registerOSSMultipartSession(
	ctx context.Context,
	ownerID int64,
	cfg ossConfigPayload,
	req ossDirectUploadRequest,
	objectKey, uploadID string,
	expiresAt time.Time,
) error {
	return registerOSSMultipartSessionTx(ctx, s.db, ownerID, cfg, req, objectKey, uploadID, expiresAt)
}

func registerOSSMultipartSessionTx(ctx context.Context, writer ossDownloadStatWriter, ownerID int64, cfg ossConfigPayload, req ossDirectUploadRequest, objectKey, uploadID string, expiresAt time.Time) error {
	_, err := writer.Exec(ctx, `insert into oss_multipart_sessions(
		owner_id,bucket,endpoint,region,use_cname,object_key,upload_id,original_name,content_type,
		size_bytes,sha256,category,source,expires_at)
		values($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`,
		ownerID, cfg.Bucket, cfg.Endpoint, cfg.Region, cfg.UseCName, objectKey, uploadID,
		req.OriginalName, req.ContentType, req.SizeBytes, req.SHA256, req.Category, req.Source, expiresAt)
	return err
}

func (s *Server) beginOSSMultipartSettlement(
	ctx context.Context,
	ownerID int64,
	cfg ossConfigPayload,
	req ossCompleteUploadRequest,
	action string,
) (ossMultipartSettlement, error) {
	return s.beginOSSMultipartSettlementWithRequest(ctx, ownerID, cfg, req, action, nil)
}

func (s *Server) beginOSSMultipartSettlementWithRequest(ctx context.Context, ownerID int64, cfg ossConfigPayload, req ossCompleteUploadRequest, action string, request *http.Request) (ossMultipartSettlement, error) {
	var settlement ossMultipartSettlement
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return settlement, err
	}
	defer tx.Rollback(ctx)
	var sessionOwner *int64
	var status, originalName, contentType, sha256, category, source string
	var sizeBytes int64
	var intentRaw []byte
	var leaseActive bool
	err = tx.QueryRow(ctx, `select id,owner_id,status,original_name,content_type,size_bytes,sha256,category,source,coalesce(lease_expires_at>clock_timestamp(),false),abort_activity_intent
		from oss_multipart_sessions where bucket=$1 and endpoint=$2 and object_key=$3 and upload_id=$4 for update`,
		cfg.Bucket, cfg.Endpoint, req.ObjectKey, req.MultipartUploadID).
		Scan(&settlement.ID, &sessionOwner, &status, &originalName, &contentType, &sizeBytes, &sha256, &category, &source, &leaseActive, &intentRaw)
	if err != nil {
		return settlement, err
	}
	if sessionOwner == nil || *sessionOwner != ownerID {
		return settlement, errOSSMultipartSessionForbidden
	}
	if originalName != req.OriginalName || contentType != req.ContentType || sizeBytes != req.SizeBytes || sha256 != req.SHA256 || category != req.Category || source != req.Source {
		return settlement, errOSSMultipartSessionForbidden
	}
	existingIntent, err := decodeOSSMultipartAbortIntent(intentRaw, ownerID)
	if err != nil {
		return settlement, err
	}
	if action == "complete" && existingIntent != nil && !existingIntent.Queued {
		return settlement, errOSSMultipartSessionConflict
	}
	terminal := "completed"
	processing := "completing"
	if action == "abort" {
		terminal = "aborted"
		processing = "aborting"
	}
	if status == terminal {
		settlement.Already = true
		err = tx.Commit(ctx)
		if err == nil && action == "abort" && request != nil {
			skipRequestActivity(request)
		}
		return settlement, err
	}
	if (status == "completing" || status == "aborting") && leaseActive {
		return settlement, errOSSMultipartSessionConflict
	}
	allowedStale := (action == "complete" && status == "completing") || (action == "abort" && (status == "completing" || status == "aborting"))
	if status != "active" && status != "cleanup_pending" && !allowedStale {
		return settlement, errOSSMultipartSessionConflict
	}
	if action == "abort" && request != nil && existingIntent == nil {
		intentRaw, err = newOSSMultipartAbortIntent(request, ownerID)
		if err != nil {
			return settlement, err
		}
	}
	settlement.Token = "multipart-http:" + newExportID()
	tag, err := tx.Exec(ctx, `update oss_multipart_sessions set status=$2,locked_by=$3,
		lease_expires_at=now()+interval '2 minutes',last_error='',failure_class='',abort_activity_intent=$5::jsonb,updated_at=now()
		where id=$1 and status=$4`, settlement.ID, processing, settlement.Token, status, intentRaw)
	if err != nil {
		return settlement, err
	}
	if tag.RowsAffected() != 1 {
		return settlement, errOSSMultipartSessionConflict
	}
	err = tx.Commit(ctx)
	if err == nil && action == "abort" && request != nil {
		skipRequestActivity(request)
	}
	return settlement, err
}

func (s *Server) finishOSSMultipartSettlement(ctx context.Context, settlement ossMultipartSettlement, action string, operationErr error) error {
	if action == "abort" && operationErr == nil {
		return s.finishOSSMultipartAbort(ctx, settlement)
	}
	if settlement.Already {
		return nil
	}
	status := "completed"
	timeColumn := "completed_at"
	if action == "abort" {
		status = "aborted"
		timeColumn = "aborted_at"
	}
	var tag pgconn.CommandTag
	var err error
	if operationErr == nil {
		query := fmt.Sprintf(`update oss_multipart_sessions set status=$3,locked_by='',lease_expires_at=null,
			last_error='',failure_class='',%s=now(),updated_at=now() where id=$1 and locked_by=$2 and status='completing' and lease_expires_at>clock_timestamp()`, timeColumn)
		tag, err = s.db.Exec(ctx, query, settlement.ID, settlement.Token, status)
	} else if action == "abort" {
		decision := classifyOSSDeletionFailure(operationErr)
		tag, err = s.db.Exec(ctx, `update oss_multipart_sessions set status='cleanup_pending',locked_by='',lease_expires_at=null,
			next_attempt_at=now(),last_error=$3,failure_class=$4,updated_at=now() where id=$1 and locked_by=$2 and status='aborting' and lease_expires_at>clock_timestamp()`,
			settlement.ID, settlement.Token, truncateOSSDeletionError(operationErr), decision.class)
	} else {
		tag, err = s.db.Exec(ctx, `update oss_multipart_sessions set status='active',locked_by='',lease_expires_at=null,
			last_error=$3,failure_class='',updated_at=now() where id=$1 and locked_by=$2 and status='completing' and lease_expires_at>clock_timestamp()`,
			settlement.ID, settlement.Token, truncateOSSDeletionError(operationErr))
	}
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return errOSSMultipartSessionLeaseLost
	}
	return nil
}

func writeOSSMultipartSettlementError(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		writeError(w, http.StatusNotFound, "OSS 分片上传会话不存在")
	case errors.Is(err, errOSSMultipartSessionForbidden):
		writeError(w, http.StatusForbidden, "OSS 分片上传会话不属于当前用户")
	case errors.Is(err, errOSSMultipartSessionConflict):
		writeError(w, http.StatusConflict, "OSS 分片上传会话状态不允许该操作")
	default:
		writeError(w, http.StatusInternalServerError, "读取 OSS 分片上传会话失败")
	}
	return true
}
