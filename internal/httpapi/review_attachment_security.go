package httpapi

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

type reviewAttachmentLookupKind uint8

const (
	reviewAttachmentByUploader reviewAttachmentLookupKind = iota + 1
	reviewAttachmentForCreatorClaim
	reviewAttachmentForMinecraftServer
	reviewAttachmentForProjectEditorApplication
)

const safeReviewAttachmentPredicate = `file.status='active'
	and file.scan_status in ('clean','trusted_generated')`

type reviewAttachmentLookup struct {
	Kind            reviewAttachmentLookupKind
	UploaderID      int64
	SubjectPublicID string
}

type reviewAttachmentFile struct {
	InternalID   int64
	PublicID     string
	ObjectKey    string
	OriginalName string
	SizeBytes    int64
}

func lookupReviewAttachment(
	ctx context.Context,
	query revisionQuery,
	lookup reviewAttachmentLookup,
	filePublicID string,
) (reviewAttachmentFile, error) {
	filePublicID = strings.ToLower(strings.TrimSpace(filePublicID))
	if !validCatalogPublicID(filePublicID) {
		return reviewAttachmentFile{}, pgx.ErrNoRows
	}

	if lookup.Kind != reviewAttachmentByUploader {
		lookup.SubjectPublicID = strings.ToLower(strings.TrimSpace(lookup.SubjectPublicID))
		if !validCatalogPublicID(lookup.SubjectPublicID) {
			return reviewAttachmentFile{}, pgx.ErrNoRows
		}
	}

	var row pgx.Row
	switch lookup.Kind {
	case reviewAttachmentByUploader:
		if lookup.UploaderID <= 0 {
			return reviewAttachmentFile{}, pgx.ErrNoRows
		}
		row = query.QueryRow(ctx, `select file.id,file.public_id,file.object_key,file.original_name,
			greatest(file.size_bytes,file.source_size_bytes)
			from oss_files file
			where file.public_id=$1 and file.uploader_id=$2 and `+safeReviewAttachmentPredicate+`
			for update`, filePublicID, lookup.UploaderID)
	case reviewAttachmentForCreatorClaim:
		row = query.QueryRow(ctx, `select file.id,file.public_id,file.object_key,file.original_name,
			greatest(file.size_bytes,file.source_size_bytes)
			from creator_claim_attachments attachment
			join creator_claims claim on claim.id=attachment.claim_id
			join oss_files file on file.id=attachment.oss_file_id
			where claim.public_id=$1 and file.public_id=$2 and `+safeReviewAttachmentPredicate,
			lookup.SubjectPublicID, filePublicID)
	case reviewAttachmentForMinecraftServer:
		row = query.QueryRow(ctx, `select file.id,file.public_id,file.object_key,file.original_name,
			greatest(file.size_bytes,file.source_size_bytes)
			from minecraft_server_proof_files proof
			join minecraft_servers server on server.id=proof.server_id
			join oss_files file on file.id=proof.oss_file_id
			where server.public_id=$1 and file.public_id=$2 and `+safeReviewAttachmentPredicate,
			lookup.SubjectPublicID, filePublicID)
	case reviewAttachmentForProjectEditorApplication:
		row = query.QueryRow(ctx, `select file.id,file.public_id,file.object_key,file.original_name,
			greatest(file.size_bytes,file.source_size_bytes)
			from project_editor_application_attachments attachment
			join project_editor_applications application on application.id=attachment.application_id
			join oss_files file on file.id=attachment.oss_file_id
			where application.public_id=$1 and file.public_id=$2 and `+safeReviewAttachmentPredicate,
			lookup.SubjectPublicID, filePublicID)
	default:
		return reviewAttachmentFile{}, fmt.Errorf("unsupported review attachment lookup kind %d", lookup.Kind)
	}

	var file reviewAttachmentFile
	err := row.Scan(&file.InternalID, &file.PublicID, &file.ObjectKey, &file.OriginalName, &file.SizeBytes)
	return file, err
}
