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

func lockModContentTemplateReferenceTablesTx(ctx context.Context, tx pgx.Tx) error {
	_, err := tx.Exec(ctx, `lock table mod_content_sections,mod_content_section_resources,mod_resource_version_details
		in share row exclusive mode`)
	return err
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
	if err := lockModContentTemplateReferenceTablesTx(ctx, tx); err != nil {
		return err
	}
	var deployed, referenced bool
	err := tx.QueryRow(ctx, `with recursive template_sections(id,version_id) as (
		select section.id,section.version_id
		from mod_content_sections section
		where section.template_id=$1 and section.parent_id is null
		union all
		select child.id,child.version_id
		from mod_content_sections child
		join template_sections parent on child.parent_id=parent.id and child.version_id=parent.version_id
	)
	select exists(select 1 from template_sections),exists(
		select 1
		from template_sections section
		join mod_content_section_resources placement
		  on placement.section_id=section.id and placement.version_id=section.version_id
		join mod_resource_version_details detail
		  on detail.resource_id=placement.resource_id and detail.version_id=placement.version_id
		where lower(detail.entry_type_code)=any($2::text[])
	)`, templateID, removed).Scan(&deployed, &referenced)
	if err != nil {
		return err
	}
	// Once a template has backed a page, its entry-type identities are immutable.
	// This also closes the gap where a pending detail has reserved a subtype but
	// has not yet received its placement. Disabling a retained type remains the
	// supported way to prevent new selections without invalidating history.
	if deployed || referenced {
		return errCatalogEditorReference
	}
	return nil
}

func validateModContentTemplateDeletionTx(ctx context.Context, tx pgx.Tx, templateID int64) error {
	if err := lockModContentTemplateReferenceTablesTx(ctx, tx); err != nil {
		return err
	}
	var referenced bool
	if err := tx.QueryRow(ctx, `select exists(
		select 1 from mod_content_sections where template_id=$1
	)`, templateID).Scan(&referenced); err != nil {
		return err
	}
	if referenced {
		return errCatalogEditorReference
	}
	return nil
}
