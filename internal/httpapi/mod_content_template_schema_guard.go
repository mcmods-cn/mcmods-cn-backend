package httpapi

import (
	"context"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
)

func removedModContentEntryTypeCodes(current, next modContentTemplateDefinition) []string {
	nextCodes := make(map[string]struct{}, len(next.EntryTypes))
	for _, entryType := range next.EntryTypes {
		if code := strings.ToLower(strings.TrimSpace(entryType.Code)); code != "" {
			nextCodes[code] = struct{}{}
		}
	}
	removed := make([]string, 0)
	for _, entryType := range current.EntryTypes {
		code := strings.ToLower(strings.TrimSpace(entryType.Code))
		if code == "" {
			continue
		}
		if _, exists := nextCodes[code]; !exists {
			removed = append(removed, code)
		}
	}
	sort.Strings(removed)
	return removed
}

func validateModContentTemplateSchemaChangeTx(
	ctx context.Context,
	tx pgx.Tx,
	templateID int64,
	current, next modContentTemplateDefinition,
) error {
	if !sameNormalizedStrings(current.ResourceKinds, next.ResourceKinds) ||
		preservesModContentAttributeSchema(current, next) != nil {
		return errCatalogEditorInvalid
	}
	removed := removedModContentEntryTypeCodes(current, next)
	if len(removed) == 0 {
		return nil
	}
	// Once deployed on a root page, subtype identities also describe pending
	// and historical details. Disabling a retained type is still allowed.
	return rejectTemplateSectionReferencesTx(ctx, tx, templateID, true)
}

func validateModContentTemplateDeletionTx(ctx context.Context, tx pgx.Tx, templateID int64) error {
	return rejectTemplateSectionReferencesTx(ctx, tx, templateID, false)
}

func rejectTemplateSectionReferencesTx(ctx context.Context, tx pgx.Tx, templateID int64, rootsOnly bool) error {
	// A concurrent section insert or reparent must not change deployment
	// between this check and the caller's template update.
	if _, err := tx.Exec(ctx, `lock table mod_content_sections in share row exclusive mode`); err != nil {
		return err
	}
	var referenced bool
	if err := tx.QueryRow(ctx, `select exists(
		select 1 from mod_content_sections where template_id=$1 and (not $2 or parent_id is null)
	)`, templateID, rootsOnly).Scan(&referenced); err != nil {
		return err
	}
	if referenced {
		return errCatalogEditorReference
	}
	return nil
}
