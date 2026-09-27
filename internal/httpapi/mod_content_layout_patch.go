package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"
)

const maxModContentLayoutPatchResources = 1000

type modContentLayoutPatch struct {
	VersionPublicID     string                          `json:"versionPublicId"`
	RootSectionPublicID string                          `json:"rootSectionPublicId"`
	DisplayMode         string                          `json:"displayMode"`
	Categories          *[]modContentLayoutCategoryEdit `json:"categories,omitempty"`
	Resources           []modContentLayoutResourceEdit  `json:"resources"`
	Reason              string                          `json:"reason"`
	BaseRevisionID      *string                         `json:"baseRevisionId,omitempty"`
}

type currentModContentLayoutResource struct {
	Edit        modContentLayoutResourceEdit
	KindCode    string
	CanonicalID string
	Definition  []byte
}

func normalizeModContentLayoutPatch(patch *modContentLayoutPatch) error {
	patch.VersionPublicID = strings.ToLower(strings.TrimSpace(patch.VersionPublicID))
	patch.RootSectionPublicID = strings.ToLower(strings.TrimSpace(patch.RootSectionPublicID))
	patch.DisplayMode = strings.ToLower(strings.TrimSpace(patch.DisplayMode))
	patch.Reason = strings.TrimSpace(patch.Reason)
	if patch.VersionPublicID == "" || !modContentPublicIDPattern.MatchString(patch.RootSectionPublicID) ||
		(patch.DisplayMode != "compact" && patch.DisplayMode != "large") || len(patch.Reason) > 500 ||
		len(patch.Resources) > maxModContentLayoutPatchResources ||
		(patch.Categories != nil && len(*patch.Categories) > maxModContentCategories) {
		return errCatalogEditorInvalid
	}
	if patch.Categories != nil {
		categoryIDs := make(map[string]struct{}, len(*patch.Categories))
		for index := range *patch.Categories {
			category := &(*patch.Categories)[index]
			category.PublicID = strings.ToLower(strings.TrimSpace(category.PublicID))
			category.ParentPublicID = strings.ToLower(strings.TrimSpace(category.ParentPublicID))
			if category.PublicID == "" || len(category.PublicID) > 80 || category.ParentPublicID == "" || category.Ordinal < 0 {
				return errCatalogEditorInvalid
			}
			if _, duplicate := categoryIDs[category.PublicID]; duplicate {
				return errCatalogEditorInvalid
			}
			categoryIDs[category.PublicID] = struct{}{}
			defaultLocale, localizations, err := normalizeCatalogLocalizations(category.DefaultLocale, category.Localizations)
			if err != nil || len(localizations) == 0 || requireCatalogCreateDefaultLocalization(defaultLocale, localizations) != nil {
				return errCatalogEditorInvalid
			}
			category.DefaultLocale, category.Localizations = defaultLocale, localizations
		}
	}
	resourceIDs := make(map[string]struct{}, len(patch.Resources))
	for index := range patch.Resources {
		resource := &patch.Resources[index]
		resource.ResourcePublicID = strings.ToLower(strings.TrimSpace(resource.ResourcePublicID))
		resource.SectionPublicID = strings.ToLower(strings.TrimSpace(resource.SectionPublicID))
		resource.SimilarGroupID = strings.ToLower(strings.TrimSpace(resource.SimilarGroupID))
		if !modContentPublicIDPattern.MatchString(resource.ResourcePublicID) || resource.SectionPublicID == "" ||
			len(resource.SimilarGroupID) > 80 || resource.Ordinal < 0 {
			return errCatalogEditorInvalid
		}
		if _, duplicate := resourceIDs[resource.ResourcePublicID]; duplicate {
			return errCatalogEditorInvalid
		}
		resourceIDs[resource.ResourcePublicID] = struct{}{}
		if resource.Advancement == nil {
			continue
		}
		resource.Advancement.ParentResourcePublicID = strings.ToLower(strings.TrimSpace(resource.Advancement.ParentResourcePublicID))
		groupID, validGroupID := normalizeAdvancementLayoutGroupID(resource.Advancement.GroupID)
		resource.Advancement.GroupID = groupID
		if (resource.Advancement.ParentResourcePublicID != "" && !modContentPublicIDPattern.MatchString(resource.Advancement.ParentResourcePublicID)) ||
			resource.Advancement.ParentResourcePublicID == resource.ResourcePublicID || !validGroupID ||
			math.IsNaN(resource.Advancement.X) || math.IsInf(resource.Advancement.X, 0) ||
			math.IsNaN(resource.Advancement.Y) || math.IsInf(resource.Advancement.Y, 0) ||
			math.Abs(resource.Advancement.X) > 1000 || math.Abs(resource.Advancement.Y) > 1000 {
			return errCatalogEditorInvalid
		}
	}
	return nil
}

func applyModContentLayoutPatch(current *modContentLayoutEdit, patch modContentLayoutPatch) error {
	if patch.Categories != nil {
		current.Categories = append([]modContentLayoutCategoryEdit(nil), (*patch.Categories)...)
	}
	resourceIndexes := make(map[string]int, len(current.Resources))
	for index := range current.Resources {
		resourceIndexes[current.Resources[index].ResourcePublicID] = index
	}
	for _, resource := range patch.Resources {
		index, exists := resourceIndexes[resource.ResourcePublicID]
		if !exists {
			return errCatalogEditorInvalid
		}
		current.Resources[index] = resource
	}
	allowedSections := map[string]struct{}{current.RootSectionPublicID: {}}
	for _, category := range current.Categories {
		allowedSections[category.PublicID] = struct{}{}
	}
	for index := range current.Resources {
		if _, exists := allowedSections[current.Resources[index].SectionPublicID]; !exists {
			current.Resources[index].SectionPublicID = current.RootSectionPublicID
			current.Resources[index].Ordinal = 1_000_000_000
			current.Resources[index].SimilarGroupID = ""
		}
	}
	current.DisplayMode = patch.DisplayMode
	current.Reason = patch.Reason
	current.BaseRevisionID = patch.BaseRevisionID
	return nil
}

func loadCurrentModContentLayout(
	ctx context.Context,
	db modContentQuerier,
	modID, rootSectionID, versionID int64,
	rootPublicID, versionPublicID, displayMode string,
) (modContentLayoutEdit, error) {
	result := modContentLayoutEdit{
		VersionPublicID: versionPublicID, RootSectionPublicID: rootPublicID, DisplayMode: displayMode,
		Categories: []modContentLayoutCategoryEdit{}, Resources: []modContentLayoutResourceEdit{},
	}
	categoryRows, err := db.Query(ctx, `with recursive subtree as (
		select id from mod_content_sections where id=$1 and mod_id=$2 and status='active'
		union all select child.id from mod_content_sections child join subtree parent on child.parent_id=parent.id
		where child.status='active'
	)
	select section.public_id,coalesce(parent.public_id,''),section.default_locale,section.ordinal,
		coalesce((select jsonb_agg(jsonb_build_object('locale',localization.locale,'name',localization.name,
		 'summary',localization.description,'contentMarkdown','') order by localization.locale)
		 from mod_content_section_localizations localization where localization.section_id=section.id),'[]'::jsonb)
	from subtree join mod_content_sections section on section.id=subtree.id
	left join mod_content_sections parent on parent.id=section.parent_id
	where section.id<>$1 order by section.id`, rootSectionID, modID)
	if err != nil {
		return result, err
	}
	for categoryRows.Next() {
		var category modContentLayoutCategoryEdit
		var localizations []byte
		if err = categoryRows.Scan(&category.PublicID, &category.ParentPublicID, &category.DefaultLocale, &category.Ordinal, &localizations); err != nil {
			categoryRows.Close()
			return result, err
		}
		if err = json.Unmarshal(localizations, &category.Localizations); err != nil {
			categoryRows.Close()
			return result, err
		}
		result.Categories = append(result.Categories, category)
	}
	if err = categoryRows.Err(); err != nil {
		categoryRows.Close()
		return result, err
	}
	categoryRows.Close()

	resourceRows, err := db.Query(ctx, `with recursive subtree as (
		select id,public_id from mod_content_sections where id=$1 and mod_id=$2 and status='active'
		union all select child.id,child.public_id from mod_content_sections child join subtree parent on child.parent_id=parent.id
		where child.status='active'
	)
	select entity.public_id,resource.kind_code,resource.canonical_id,subtree.public_id,placement.ordinal,
		placement.similar_group_id,case when resource.kind_code='minecraft.advancement' then coalesce(effective.data,'{}'::jsonb) else '{}'::jsonb end
	from subtree join mod_content_section_resources placement on placement.section_id=subtree.id and placement.version_id=$3
	join catalog_entities entity on entity.id=placement.resource_id and entity.status='active' and entity.archived_at is null
	join game_resources resource on resource.entity_id=placement.resource_id
	left join mod_resource_version_details detail on detail.resource_id=placement.resource_id and detail.version_id=$3 and detail.status='active'
	left join lateral (select snapshot.data from resource_import_snapshots snapshot
	 join catalog_import_revisions revision on revision.id=snapshot.revision_id
	 where resource.kind_code='minecraft.advancement' and snapshot.resource_id=placement.resource_id
	  and revision.target_version_id=$3 and revision.is_active and revision.status in ('ready','partial')
	 order by coalesce(revision.activated_at,revision.created_at) desc limit 1) imported on true
	left join lateral (select case when detail.definition is not null and detail.definition<>'{}'::jsonb
	 then detail.definition else coalesce(imported.data,'{}'::jsonb) end data) effective on true
	order by subtree.id,placement.ordinal,placement.resource_id`, rootSectionID, modID, versionID)
	if err != nil {
		return result, err
	}
	currentResources := make([]currentModContentLayoutResource, 0)
	for resourceRows.Next() {
		var resource currentModContentLayoutResource
		if err = resourceRows.Scan(&resource.Edit.ResourcePublicID, &resource.KindCode, &resource.CanonicalID,
			&resource.Edit.SectionPublicID, &resource.Edit.Ordinal, &resource.Edit.SimilarGroupID, &resource.Definition); err != nil {
			resourceRows.Close()
			return result, err
		}
		currentResources = append(currentResources, resource)
	}
	if err = resourceRows.Err(); err != nil {
		resourceRows.Close()
		return result, err
	}
	resourceRows.Close()

	publicIDByCanonicalID := make(map[string]string, len(currentResources))
	importRows := make([]catalogResourceImportRow, 0)
	for _, resource := range currentResources {
		publicIDByCanonicalID[strings.ToLower(strings.TrimSpace(resource.CanonicalID))] = resource.Edit.ResourcePublicID
		if resource.KindCode == "minecraft.advancement" {
			importRows = append(importRows, catalogResourceImportRow{KindCode: resource.KindCode, CanonicalID: resource.CanonicalID, Data: string(resource.Definition)})
		}
	}
	derivedGroups := importedAdvancementLayoutGroups(importRows)
	for index, resource := range currentResources {
		if resource.KindCode == "minecraft.advancement" {
			var definition map[string]any
			if len(resource.Definition) > 0 && json.Unmarshal(resource.Definition, &definition) != nil {
				return result, errors.New("invalid stored advancement layout")
			}
			parentCanonicalID := strings.ToLower(strings.TrimSpace(stringValue(definition["parentId"])))
			if parentCanonicalID == "" {
				parentCanonicalID = strings.ToLower(strings.TrimSpace(stringValue(definition["parent"])))
			}
			display, _ := definition["display"].(map[string]any)
			groupID := strings.TrimSpace(stringValue(definition["layoutGroupId"]))
			if groupID == "" {
				groupID = derivedGroups[strings.ToLower(strings.TrimSpace(resource.CanonicalID))]
			}
			resource.Edit.Advancement = &modContentAdvancementLayoutEdit{
				ParentResourcePublicID: publicIDByCanonicalID[parentCanonicalID],
				GroupID:                groupID,
				X:                      numericLayoutValue(display["x"], float64(index%5)),
				Y:                      numericLayoutValue(display["y"], float64(index/5)),
			}
		}
		result.Resources = append(result.Resources, resource.Edit)
	}
	return result, nil
}

func numericLayoutValue(value any, fallback float64) float64 {
	if number, ok := value.(float64); ok && !math.IsNaN(number) && !math.IsInf(number, 0) {
		return number
	}
	return fallback
}
