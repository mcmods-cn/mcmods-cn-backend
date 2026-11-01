package httpapi

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"mcmods-cn-backend/internal/database"
)

type OSSRawRetirementDiagnostic struct {
	Database              string                           `json:"database"`
	ReadOnly              bool                             `json:"readOnly"`
	ProviderBytesObserved bool                             `json:"providerBytesObserved"`
	Counts                map[string]int                   `json:"counts"`
	Files                 []OSSRawRetirementFileDiagnostic `json:"files"`
}

type OSSRawRetirementFileDiagnostic struct {
	PublicID      string     `json:"publicId"`
	Status        string     `json:"status"`
	ReceiptID     string     `json:"receiptId,omitempty"`
	IntentStatus  string     `json:"intentStatus,omitempty"`
	HoldReason    string     `json:"holdReason,omitempty"`
	IssuedMaximum *time.Time `json:"issuedMaximum,omitempty"`
	NextAttempt   *time.Time `json:"nextAttempt,omitempty"`
}

// Exact public IDs bound the read scope to at most100 files. This never infers
// historical authority, contacts the provider, enqueues/deletes anything or
// alters the seven-day active-file maintenance policy. A completed DB intent
// is reported as DB state, not a claim about physical provider bytes.
func InspectOSSRawRetirements(ctx context.Context, pool *pgxpool.Pool, publicIDs []string) (OSSRawRetirementDiagnostic, error) {
	report := OSSRawRetirementDiagnostic{Counts: map[string]int{}, Files: []OSSRawRetirementFileDiagnostic{}}
	if len(publicIDs) < 1 || len(publicIDs) > 100 {
		return report, errors.New("provide between1 and100 exact public file IDs")
	}
	seen := map[string]bool{}
	for _, id := range publicIDs {
		if id != strings.TrimSpace(id) || !validCatalogPublicID(id) || seen[id] {
			return report, errors.New("invalid or duplicate exact public file ID")
		}
		seen[id] = true
	}
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return report, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `set local search_path=pg_catalog,public,pg_temp`); err != nil {
		return report, err
	}
	if err = tx.QueryRow(ctx, `select current_database(),current_setting('transaction_read_only')='on'`).Scan(&report.Database, &report.ReadOnly); err != nil {
		return report, err
	}
	if err = database.RequireOSSUploadByteBindingSchema(ctx, tx); err != nil {
		return report, err
	}
	for _, id := range publicIDs {
		entry := OSSRawRetirementFileDiagnostic{PublicID: id}
		var fileID int64
		var status string
		err = tx.QueryRow(ctx, `select id,status from oss_files where public_id=$1`, id).Scan(&fileID, &status)
		if errors.Is(err, pgx.ErrNoRows) {
			entry.Status = "UNKNOWN_FILE"
		} else if err != nil {
			return report, err
		} else if status != "deleted" {
			entry.Status = "LIVE_FILE_NOT_TERMINAL"
		} else {
			var count int
			if err = tx.QueryRow(ctx, `select count(*) from oss_upload_byte_bindings where file_id=$1 and status='committed'`, fileID).Scan(&count); err != nil {
				return report, err
			}
			switch count {
			case 0:
				entry.Status = "HOLD_NO_TYPED_RECEIPT"
			case 1:
				var exact bool
				if err = tx.QueryRow(ctx, `select binding.id::text,cap.max_expires_at,coalesce(queue.status,''),queue.next_attempt_at,coalesce(queue.raw_hold_reason,''),
 binding.file_endpoint<>'' and binding.bucket=file.bucket and binding.file_endpoint=file.endpoint and binding.stored_object_key=file.object_key
 and binding.owner_id=file.uploader_id and binding.logical_category=file.category and binding.source=file.source
 and binding.source_verified_sha256=file.sha256 and binding.source_verified_size_bytes=file.source_size_bytes
 and binding.stored_verified_sha256=file.stored_sha256 and binding.stored_verified_size_bytes=file.size_bytes
 from oss_upload_byte_bindings binding join oss_files file on file.id=binding.file_id
 left join oss_upload_capabilities cap on cap.bucket=binding.bucket and cap.endpoint=binding.endpoint and cap.object_key=binding.upload_object_key
 left join oss_object_deletion_outbox queue on queue.raw_upload_binding_id=binding.id and queue.oss_file_id=file.id and queue.bucket=binding.bucket and queue.endpoint=binding.endpoint and queue.object_key=binding.upload_object_key
 where binding.file_id=$1 and binding.status='committed'`, fileID).Scan(&entry.ReceiptID, &entry.IssuedMaximum, &entry.IntentStatus, &entry.NextAttempt, &entry.HoldReason, &exact); err != nil {
					return report, err
				}
				if !exact {
					entry.Status = "HOLD_TERMINAL_PROOF_MISMATCH"
				} else if entry.IssuedMaximum == nil {
					entry.Status = "HOLD_CAPABILITY_UNKNOWN"
				} else if entry.IntentStatus == "" {
					entry.Status = "MISSING_DURABLE_INTENT"
				} else {
					entry.Status = "DB_INTENT_" + strings.ToUpper(entry.IntentStatus)
				}
			default:
				entry.Status = "HOLD_MULTIPLE_RECEIPTS_REVIEW_EXACT_SCOPE"
			}
		}
		report.Files = append(report.Files, entry)
		report.Counts[entry.Status]++
	}
	return report, tx.Commit(ctx)
}
