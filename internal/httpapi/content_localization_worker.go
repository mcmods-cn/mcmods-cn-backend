package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

type contentTranslationTaskPayload struct {
	EntityID         int64  `json:"internalEntityId"`
	PublicID         string `json:"publicId"`
	EntityType       string `json:"entityType"`
	SourceLocale     string `json:"sourceLocale"`
	SourceRevisionNo int64  `json:"sourceRevisionNo"`
	TargetLocale     string `json:"targetLocale"`
	TargetRevisionNo *int64 `json:"targetRevisionNo"`
	QuotaBacked      bool   `json:"quotaBacked"`
}

func (worker *AIWorker) persistCatalogContentTranslation(
	ctx context.Context,
	taskID int64,
	createdBy int64,
	rawPayload []byte,
	result map[string]any,
) error {
	var payload contentTranslationTaskPayload
	if err := json.Unmarshal(rawPayload, &payload); err != nil {
		return fmt.Errorf("decode catalog translation payload: %w", err)
	}
	payload.SourceLocale = normalizeContentLocale(payload.SourceLocale)
	payload.TargetLocale = normalizeContentLocale(payload.TargetLocale)
	if payload.EntityID <= 0 || !validCatalogPublicID(payload.PublicID) || payload.SourceLocale == "" || payload.TargetLocale == "" || payload.SourceRevisionNo <= 0 {
		return errors.New("catalog translation payload is incomplete")
	}
	translated := translationItemsToMap(result)
	if translated["name"] == "" && translated["summary"] == "" && translated["contentMarkdown"] == "" {
		return errors.New("catalog translation result is empty")
	}

	tx, err := worker.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var taskUID string
	var taskStatus string
	if err = tx.QueryRow(ctx, `select task_uid,status from ai_tasks where id=$1 for update`, taskID).Scan(&taskUID, &taskStatus); err != nil {
		return fmt.Errorf("resolve AI task public identity: %w", err)
	}
	if taskStatus != "running" {
		return errors.New("content translation task is no longer running")
	}
	var alreadyRecorded bool
	if err = tx.QueryRow(ctx, `
		select exists(
		 select 1 from change_requests
		 where aggregate_type=$1 and metadata->>'aiTaskId'=$2
		)`, catalogAggregateLocalization, taskUID).Scan(&alreadyRecorded); err != nil {
		return err
	}
	if alreadyRecorded {
		if err = completeAITaskTx(ctx, tx, taskID); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	entity, _, loadErr := loadEditableContentSubjectTx(ctx, tx, payload.PublicID)
	if loadErr != nil || entity.EntityID != payload.EntityID {
		return errors.New("translated content subject is no longer available")
	}
	entityType, entityID := entity.EntityType, entity.EntityID
	if payload.EntityType != "" && payload.EntityType != entityType {
		return errors.New("content translation subject type changed")
	}
	var currentSourceRevisionNo int64
	if err = tx.QueryRow(ctx, `select revision_no from content_localizations
		where subject_id=$1 and subject_type=$2 and locale=$3 and review_status='approved' for update`,
		entityID, entityType, payload.SourceLocale).Scan(&currentSourceRevisionNo); err != nil {
		return fmt.Errorf("load content translation source revision: %w", err)
	}
	if currentSourceRevisionNo != payload.SourceRevisionNo {
		return errors.New("content translation source changed while the task was running")
	}
	revisionEntityID := entityID
	var baseRevisionID *int64
	var targetRevisionNo int64
	var targetProvenance string
	err = tx.QueryRow(ctx, `
		select published_revision_id,revision_no,provenance from content_localizations
		where subject_id=$1 and subject_type=$2 and locale=$3 for update`, entityID, entityType, payload.TargetLocale).Scan(&baseRevisionID, &targetRevisionNo, &targetProvenance)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if errors.Is(err, pgx.ErrNoRows) {
		baseRevisionID = nil
	}
	if targetProvenance == "human" || targetProvenance == "human_corrected" {
		return errors.New("human translation is protected")
	}
	// Old queued tasks have no target snapshot and cannot safely overwrite an
	// existing translation. Fresh tasks must match both source and target.
	if payload.TargetRevisionNo == nil && targetRevisionNo != 0 || payload.TargetRevisionNo != nil && *payload.TargetRevisionNo != targetRevisionNo {
		return errors.New("content translation target changed while the task was running")
	}
	reviewRequired := loadReviewConfig(ctx, tx).AITranslation
	reviewStatus := "approved"
	if reviewRequired {
		reviewStatus = "pending"
	}
	taskIDCopy := taskID
	taskUIDCopy := taskUID
	snapshot := catalogLocalizationSnapshot{
		SubjectPublicID:  payload.PublicID,
		SubjectType:      entityType,
		EntityID:         payload.EntityID,
		Locale:           payload.TargetLocale,
		Name:             translated["name"],
		Summary:          translated["summary"],
		ContentMarkdown:  translated["contentMarkdown"],
		Provenance:       "ai",
		SourceLocale:     payload.SourceLocale,
		SourceRevisionNo: payload.SourceRevisionNo,
		Editable:         isEditableContentLocale(payload.TargetLocale),
		ReviewStatus:     reviewStatus,
		AITaskID:         &taskIDCopy,
		AITaskPublicID:   &taskUIDCopy,
	}
	rawSnapshot, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	created, err := createContentRevisionTx(ctx, tx, createContentRevisionParams{
		EntityType:    entityType,
		EntityID:      revisionEntityID,
		AggregateType: catalogAggregateLocalization,
		AggregateKey:  contentLocalizationAggregateKey(payload.PublicID, entityType, payload.TargetLocale),
		BaseRevision:  baseRevisionID,
		Snapshot:      rawSnapshot,
		Reason:        "AI content translation",
		ActorID:       createdBy,
		Source:        "ai",
		Status:        reviewStatus,
		Metadata: map[string]any{
			"aiTaskId": taskUID, "publicId": payload.PublicID, "entityType": entityType,
			"sourceLocale": payload.SourceLocale, "targetLocale": payload.TargetLocale,
			"quotaBacked": payload.QuotaBacked,
		},
	})
	if err != nil {
		return err
	}
	if reviewStatus == "approved" {
		if err = publishCatalogLocalizationSnapshotTx(ctx, tx, created.RevisionID, rawSnapshot, createdBy); err != nil {
			return err
		}
		if err = appendReviewResolutionTx(ctx, tx, created.ChangeRequestID, "approved", createdBy, "automatic AI translation approval", nil); err != nil {
			return err
		}
		if err = appendCatalogPublishedReviewEventTx(ctx, tx, created.ChangeRequestID, createdBy, "automatic AI translation publication", nil); err != nil {
			return err
		}
	}
	if err = completeAITaskTx(ctx, tx, taskID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
