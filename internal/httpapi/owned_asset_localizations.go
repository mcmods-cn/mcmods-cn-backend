package httpapi

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

func replaceOwnedAssetLocalizationsTx(
	ctx context.Context,
	tx pgx.Tx,
	subjectType string,
	subjectID, revisionID, actorID int64,
	defaultLocale string,
	localizations []catalogLocalizationEdit,
) error {
	if subjectType != "skin" && subjectType != "blueprint" {
		return fmt.Errorf("unsupported owned asset type %q", subjectType)
	}
	if _, err := tx.Exec(ctx, `update content_subjects set default_locale=$3,updated_at=now()
		where subject_type=$1 and subject_id=$2`, subjectType, subjectID, defaultLocale); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `delete from content_localizations where subject_type=$1 and subject_id=$2`, subjectType, subjectID); err != nil {
		return err
	}
	for _, localization := range localizations {
		if _, err := tx.Exec(ctx, `insert into content_localizations(
			subject_type,subject_id,locale,name,summary,content_markdown,provenance,editable,review_status,published_revision_id,updated_by)
			values($1,$2,$3,$4,$5,$6,'human',true,'approved',$7,$8)`,
			subjectType, subjectID, localization.Locale, localization.Name, localization.Summary,
			localization.ContentMarkdown, revisionID, actorID); err != nil {
			return err
		}
	}
	return nil
}
