package httpapi

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"

	"mcmods-cn-backend/internal/security"
)

func translationSourceFields(raw []byte, keys ...string) (map[string]string, error) {
	var source map[string]any
	if err := json.Unmarshal(raw, &source); err != nil {
		return nil, err
	}
	fields, err := strictTranslationItemsToMap(source, stringSet(keys...))
	if err != nil || len(fields) != len(keys) {
		return nil, errAIExecutionPolicy
	}
	return fields, nil
}

func verifyClaimedTranslationSourceTx(ctx context.Context, tx pgx.Tx, actor security.Claims, prepared preparedAITask) error {
	switch prepared.taskType {
	case aiTaskNotificationTranslation:
		return verifyNotificationTranslationSourceTx(ctx, tx, actor.Subject, prepared.rawPayload)
	case aiTaskContentTranslation:
		scope, err := decodeAITaskContentScope(prepared.rawPayload)
		if err != nil {
			return err
		}
		if scope == "community_post" {
			source, err := decodeCommunityPostTranslationTaskPayload(prepared.rawPayload)
			if err != nil {
				return err
			}
			return lockCommunityTranslationSourceTx(ctx, tx, source, prepared.rawPayload)
		}
		source, err := decodeContentTranslationTaskPayload(prepared.rawPayload)
		if err != nil {
			return err
		}
		entityType, err := verifyCatalogTranslationVisibilityTx(ctx, tx, actor, source)
		if err != nil {
			return err
		}
		fields, err := translationSourceFields(prepared.rawPayload, "name", "summary", "contentMarkdown")
		if err != nil {
			return err
		}
		var matches bool
		err = tx.QueryRow(ctx, `select revision_no=$4 and name=$5 and summary=$6 and content_markdown=$7
			from content_localizations where subject_id=$1 and subject_type=$2 and locale=$3 and review_status='approved'
			for share`, source.EntityID, entityType, source.SourceLocale, source.SourceRevisionNo,
			fields["name"], fields["summary"], fields["contentMarkdown"]).Scan(&matches)
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && !matches) {
			return errAIExecutionPolicy
		}
		return err
	default:
		return nil // UI and permission labels are supplied by the authorized enqueuer.
	}
}

func verifyCatalogTranslationVisibilityTx(ctx context.Context, tx pgx.Tx, actor security.Claims, source contentTranslationTaskPayload) (string, error) {
	entity, err := loadVisibleLocalizationSubject(ctx, tx, source.PublicID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", errAIExecutionPolicy
	}
	if err != nil {
		return "", err
	}
	if entity.EntityID != source.EntityID || (source.EntityType != "" && entity.EntityType != source.EntityType) ||
		(entity.EntityType == "resource" && !claimsAllow(actor, "global_resource.view")) {
		return "", errAIExecutionPolicy
	}
	return entity.EntityType, nil
}
