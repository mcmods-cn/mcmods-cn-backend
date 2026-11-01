package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"
)

var errOSSCompletedIdentityConflict = errors.New("completed OSS upload identity conflicts")

// The upload key, not an attempt's random candidate, is the publication identity.
func completedOSSByteBindingForUploadTx(ctx context.Context, tx pgx.Tx, cfg ossConfigPayload, key string, owner int64, req ossCompleteUploadRequest, size int64) (bool, error) {
	var actualOwner, actualSize int64
	var category, source, sha, fileStatus string
	var currentFileProof bool
	err := tx.QueryRow(ctx, `select binding.owner_id,binding.logical_category,binding.source,binding.declared_sha256,binding.declared_size_bytes,file.status,`+ossByteBindingCurrentFileProofSQL+`
 from oss_upload_byte_bindings binding join oss_files file on file.id=binding.file_id
 where binding.bucket=$1 and binding.endpoint=$2 and binding.upload_object_key=$3 and binding.status='committed'`, cfg.Bucket, cfg.Endpoint, key).Scan(&actualOwner, &category, &source, &sha, &actualSize, &fileStatus, &currentFileProof)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !currentFileProof || actualOwner != owner || category != req.Category || source != req.Source || sha != req.SHA256 || actualSize != size || fileStatus != "active" {
		return false, errOSSCompletedIdentityConflict
	}
	return true, nil
}

func (s *Server) writeCompletedReportEvidence(w http.ResponseWriter, r *http.Request, fileID int64, req ossCompleteUploadRequest) {
	evidenceID, status, err := s.restoreReportEvidenceForCompletedOSSFile(r.Context(), fileID, currentClaims(r).Subject, req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "恢复已完成举报附件失败")
		return
	}
	skipRequestActivity(r)
	writeJSON(w, http.StatusOK, map[string]any{"id": evidenceID, "evidenceId": evidenceID, "scanStatus": status, "idempotent": true})
}

// The winning registration transaction has been released before these lookups,
// association recovery or URL preparation, including on a single-slot pool.
func (s *Server) writeCompletedOSSByteBindingReplay(w http.ResponseWriter, r *http.Request, cfg ossConfigPayload, req ossCompleteUploadRequest, report bool, scope string) {
	fileID, file, found, err := s.findCompletedOSSUpload(r.Context(), currentClaims(r).Subject, req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "cannot load completed OSS upload")
		return
	}
	if !found {
		writeError(w, http.StatusConflict, "completed OSS upload is no longer available")
		return
	}
	if report {
		s.writeCompletedReportEvidence(w, r, fileID, req)
		return
	}
	if scope == "user" {
		if err = s.releaseUserOSSUploadQuotaReservation(r.Context(), currentClaims(r).Subject, req.ObjectKey); err != nil {
			writeError(w, http.StatusInternalServerError, "释放已完成上传额度预留失败")
			return
		}
	}
	response, err := s.completedOSSUploadResponse(r.Context(), cfg, file)
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed to generate OSS access URL")
		return
	}
	if err = s.attachCompletedOSSUploadAssociations(r.Context(), response, fileID, currentClaims(r).Subject, req.Source); err != nil {
		writeBlueprintUploadAssociationError(w, err, "failed to restore completed upload associations")
		return
	}
	skipRequestActivity(r)
	writeJSON(w, http.StatusOK, response)
}

func writeOSSByteBindingMutationError(w http.ResponseWriter, err error, fallback string) {
	if errors.Is(err, errOSSRawUploadRetired) {
		writeError(w, http.StatusConflict, "原始上传已结束，请重新上传")
		return
	}
	if errors.Is(err, errOSSCompletedIdentityConflict) {
		writeError(w, http.StatusConflict, "此上传已使用不同文件信息完成，请重新上传")
		return
	}
	if errors.Is(err, errOSSByteBindingLeaseLost) {
		writeError(w, http.StatusConflict, "上传确认已过期，请重试")
		return
	}
	writeError(w, http.StatusInternalServerError, fallback)
}
