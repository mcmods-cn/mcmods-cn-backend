package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

type contentTranslationTaskPayload struct {
	EntityID         int64   `json:"internalEntityId"`
	PublicID         string  `json:"publicId"`
	EntityType       string  `json:"entityType"`
	SourceLocale     string  `json:"sourceLocale"`
	SourceRevisionNo int64   `json:"sourceRevisionNo"`
	TargetLocale     string  `json:"targetLocale"`
	QuotaBacked      bool    `json:"quotaBacked"`
	TargetRevisionID *string `json:"targetRevisionId"`
}

func decodeContentTranslationTaskPayload(rawPayload []byte) (contentTranslationTaskPayload, error) {
	var payload contentTranslationTaskPayload
	if err := json.Unmarshal(rawPayload, &payload); err != nil {
		return contentTranslationTaskPayload{}, fmt.Errorf("decode catalog translation payload: %w", err)
	}
	payload.SourceLocale = normalizeContentLocale(payload.SourceLocale)
	payload.TargetLocale = normalizeContentLocale(payload.TargetLocale)
	if payload.EntityID <= 0 || !validCatalogPublicID(payload.PublicID) || payload.SourceLocale == "" || payload.TargetLocale == "" || payload.SourceRevisionNo <= 0 {
		return contentTranslationTaskPayload{}, errors.New("catalog translation payload is incomplete")
	}
	return payload, nil
}

func (worker *AIWorker) persistCatalogContentTranslation(
	ctx context.Context,
	taskID int64,
	createdBy int64,
	rawPayload []byte,
	result map[string]any,
) error {
	payload, err := decodeContentTranslationTaskPayload(rawPayload)
	if err != nil {
		return err
	}
	translated, err := strictTranslationItemsToMap(result, stringSet("name", "summary", "contentMarkdown"))
	if err != nil {
		return err
	}
	if translated["name"] == "" && translated["summary"] == "" && translated["contentMarkdown"] == "" {
		return errors.New("catalog translation result is empty")
	}
	reviewRequired := loadReviewConfig(ctx, worker.db).AITranslation

	tx, err := worker.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = lockAITaskExecutionTx(ctx, tx); err != nil {
		return err
	}
	var taskUID string
	if err = tx.QueryRow(ctx, `select task_uid from ai_tasks where id=$1`, taskID).Scan(&taskUID); err != nil {
		return fmt.Errorf("resolve AI task public identity: %w", err)
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
		return tx.Commit(ctx)
	}
	entity, entityID, err := loadEditableContentSubjectTx(ctx, tx, payload.PublicID)
	if err != nil {
		return fmt.Errorf("load translated content subject: %w", err)
	}
	entityType := entity.EntityType
	if entityID != payload.EntityID {
		return errors.New("content translation subject identity changed")
	}
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
	var currentTargetRevisionID *string
	var targetProvenance string
	err = tx.QueryRow(ctx, `
		select localization.published_revision_id,revision.public_id,localization.provenance from content_localizations localization
		left join content_revisions revision on revision.id=localization.published_revision_id
		where localization.subject_id=$1 and localization.subject_type=$2 and localization.locale=$3
		for update of localization`, entityID, entityType, payload.TargetLocale).Scan(&baseRevisionID, &currentTargetRevisionID, &targetProvenance)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if errors.Is(err, pgx.ErrNoRows) {
		baseRevisionID = nil
	} else if targetProvenance != "ai" || payload.TargetRevisionID == nil || currentTargetRevisionID == nil || *payload.TargetRevisionID != *currentTargetRevisionID {
		return errors.New("target localization was edited while translation was running")
	}
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
	return tx.Commit(ctx)
}
