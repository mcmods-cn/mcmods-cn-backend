package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
)

const (
	maxModContentCategories = 1000
	maxModContentResources  = 20000
)

var modContentPublicIDPattern = regexp.MustCompile(`^[a-z0-9]{9}$`)

type modContentLayoutCategoryEdit struct {
	PublicID       string                    `json:"publicId"`
	ParentPublicID string                    `json:"parentPublicId"`
	DefaultLocale  string                    `json:"defaultLocale"`
	Ordinal        int                       `json:"ordinal"`
	Localizations  []catalogLocalizationEdit `json:"localizations"`
}

type modContentLayoutResourceEdit struct {
	ResourcePublicID string                           `json:"resourcePublicId"`
	SectionPublicID  string                           `json:"sectionPublicId"`
	Ordinal          int                              `json:"ordinal"`
	Advancement      *modContentAdvancementLayoutEdit `json:"advancement,omitempty"`
}

type modContentAdvancementLayoutEdit struct {
	ParentResourcePublicID string  `json:"parentResourcePublicId"`
	X                      float64 `json:"x"`
	Y                      float64 `json:"y"`
}

type modContentLayoutEdit struct {
	VersionPublicID     string                         `json:"versionPublicId"`
	RootSectionPublicID string                         `json:"rootSectionPublicId"`
	Categories          []modContentLayoutCategoryEdit `json:"categories"`
	Resources           []modContentLayoutResourceEdit `json:"resources"`
	Reason              string                         `json:"reason"`
	BaseRevisionID      *string                        `json:"baseRevisionId,omitempty"`
}

type modContentLayoutResourceIdentity struct {
	KindCode                    string
	CanonicalID                 string
	BlockRepresentativeID       int64
	BlockRepresentativePublicID string
}

type modContentQueryRower interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

type modContentQuerier interface {
	modContentQueryRower
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func normalizeModContentLayoutEdit(edit *modContentLayoutEdit) error {
	edit.VersionPublicID = strings.ToLower(strings.TrimSpace(edit.VersionPublicID))
	edit.RootSectionPublicID = strings.ToLower(strings.TrimSpace(edit.RootSectionPublicID))
	edit.Reason = strings.TrimSpace(edit.Reason)
	if edit.VersionPublicID == "" || !modContentPublicIDPattern.MatchString(edit.RootSectionPublicID) ||
		len(edit.Reason) > 500 || len(edit.Categories) > maxModContentCategories || len(edit.Resources) > maxModContentResources {
		return errCatalogEditorInvalid
	}
	categoryIDs := make(map[string]struct{}, len(edit.Categories))
	for index := range edit.Categories {
		category := &edit.Categories[index]
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
	resourceIDs := make(map[string]struct{}, len(edit.Resources))
	for index := range edit.Resources {
		resource := &edit.Resources[index]
		resource.ResourcePublicID = strings.ToLower(strings.TrimSpace(resource.ResourcePublicID))
		resource.SectionPublicID = strings.ToLower(strings.TrimSpace(resource.SectionPublicID))
		if !modContentPublicIDPattern.MatchString(resource.ResourcePublicID) || resource.SectionPublicID == "" || resource.Ordinal < 0 {
			return errCatalogEditorInvalid
		}
		if _, duplicate := resourceIDs[resource.ResourcePublicID]; duplicate {
			return errCatalogEditorInvalid
		}
		if resource.Advancement != nil {
			resource.Advancement.ParentResourcePublicID = strings.ToLower(strings.TrimSpace(resource.Advancement.ParentResourcePublicID))
			if (resource.Advancement.ParentResourcePublicID != "" && !modContentPublicIDPattern.MatchString(resource.Advancement.ParentResourcePublicID)) ||
				resource.Advancement.ParentResourcePublicID == resource.ResourcePublicID ||
				math.IsNaN(resource.Advancement.X) || math.IsInf(resource.Advancement.X, 0) ||
				math.IsNaN(resource.Advancement.Y) || math.IsInf(resource.Advancement.Y, 0) ||
				math.Abs(resource.Advancement.X) > 1000 || math.Abs(resource.Advancement.Y) > 1000 {
				return errCatalogEditorInvalid
			}
		}
		resourceIDs[resource.ResourcePublicID] = struct{}{}
	}
	return nil
}

func (s *Server) modContentSectionLayout(w http.ResponseWriter, r *http.Request) {
	identity, ok := s.requireEditableMod(w, r)
	if !ok {
		return
	}
	rootPublicID := strings.ToLower(strings.TrimSpace(r.PathValue("sectionId")))
	var edit modContentLayoutEdit
	if decodeJSON(r, &edit) != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid content layout")
		return
	}
	if edit.RootSectionPublicID == "" {
		edit.RootSectionPublicID = rootPublicID
	}
	if edit.RootSectionPublicID != rootPublicID || normalizeModContentLayoutEdit(&edit) != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid content layout")
		return
	}
	var versionID int64
	var versionPublicID string
	var publishedRevisionID *int64
	err := s.db.QueryRow(r.Context(), `select section.version_id,version.public_id,section.published_revision_id
		from mod_content_sections section join mod_content_versions version on version.id=section.version_id and version.status='active'
		where section.public_id=$1 and section.mod_id=$2 and section.parent_id is null and section.status='active'`,
		rootPublicID, identity.ID).Scan(&versionID, &versionPublicID, &publishedRevisionID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "root content section not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read content layout")
		return
	}
	requestedBaseRevisionID, baseErr := resolveRevisionPublicID(r.Context(), s.db, edit.BaseRevisionID)
	if baseErr != nil || edit.VersionPublicID != versionPublicID || !sameRevision(requestedBaseRevisionID, publishedRevisionID) {
		writeError(w, http.StatusConflict, "content layout changed; reload the editor")
		return
	}
	if err = s.prepareModContentLayout(r.Context(), identity.ID, versionID, &edit); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid content category tree or resource assignment")
		return
	}
	s.submitExistingModContentMutation(w, r, identity, modContentSnapshot{
		Kind: "layout", Operation: "edit", ModID: identity.ID, ModSiteID: identity.SiteID,
		PublicID: rootPublicID, Layout: &edit,
	}, publishedRevisionID)
}

func (s *Server) prepareModContentLayout(ctx context.Context, modID, versionID int64, edit *modContentLayoutEdit) error {
	rows, err := s.db.Query(ctx, `with recursive subtree as (
			select id,public_id from mod_content_sections where public_id=$1 and mod_id=$2 and version_id=$3 and status='active'
			union all
			select child.id,child.public_id from mod_content_sections child join subtree parent on child.parent_id=parent.id
			where child.status='active'
		)
		select public_id from subtree where public_id<>$1`, edit.RootSectionPublicID, modID, versionID)
	if err != nil {
		return err
	}
	existing := make(map[string]struct{})
	for rows.Next() {
		var publicID string
		if err = rows.Scan(&publicID); err != nil {
			rows.Close()
			return err
		}
		existing[publicID] = struct{}{}
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	replacements := make(map[string]string)
	for index := range edit.Categories {
		publicID := edit.Categories[index].PublicID
		if modContentPublicIDPattern.MatchString(publicID) {
			if _, ok := existing[publicID]; !ok {
				return errCatalogEditorInvalid
			}
			continue
		}
		var generated string
		if err = s.db.QueryRow(ctx, `select new_public_id()`).Scan(&generated); err != nil {
			return err
		}
		replacements[publicID] = generated
		edit.Categories[index].PublicID = generated
	}
	for index := range edit.Categories {
		if replacement, ok := replacements[edit.Categories[index].ParentPublicID]; ok {
			edit.Categories[index].ParentPublicID = replacement
		}
	}
	for index := range edit.Resources {
		if replacement, ok := replacements[edit.Resources[index].SectionPublicID]; ok {
			edit.Resources[index].SectionPublicID = replacement
		}
	}

	categoryByID := make(map[string]*modContentLayoutCategoryEdit, len(edit.Categories))
	for index := range edit.Categories {
		category := &edit.Categories[index]
		if category.PublicID == edit.RootSectionPublicID {
			return errCatalogEditorInvalid
		}
		categoryByID[category.PublicID] = category
	}
	depthCache := make(map[string]int, len(categoryByID))
	visiting := make(map[string]bool, len(categoryByID))
	var categoryDepth func(string) (int, error)
	categoryDepth = func(publicID string) (int, error) {
		if publicID == edit.RootSectionPublicID {
			return 0, nil
		}
		if depth, ok := depthCache[publicID]; ok {
			return depth, nil
		}
		if visiting[publicID] {
			return 0, errCatalogEditorInvalid
		}
		category := categoryByID[publicID]
		if category == nil {
			return 0, errCatalogEditorInvalid
		}
		visiting[publicID] = true
		parentDepth, err := categoryDepth(category.ParentPublicID)
		delete(visiting, publicID)
		if err != nil || parentDepth >= 4 {
			return 0, errCatalogEditorInvalid
		}
		depthCache[publicID] = parentDepth + 1
		return parentDepth + 1, nil
	}
	for publicID := range categoryByID {
		if _, err = categoryDepth(publicID); err != nil {
			return err
		}
	}

	sort.SliceStable(edit.Categories, func(i, j int) bool {
		left, right := edit.Categories[i], edit.Categories[j]
		if left.ParentPublicID != right.ParentPublicID {
			return left.ParentPublicID < right.ParentPublicID
		}
		if left.Ordinal != right.Ordinal {
			return left.Ordinal < right.Ordinal
		}
		return left.PublicID < right.PublicID
	})
	nextCategoryOrdinal := make(map[string]int)
	for index := range edit.Categories {
		parentID := edit.Categories[index].ParentPublicID
		edit.Categories[index].Ordinal = nextCategoryOrdinal[parentID]
		nextCategoryOrdinal[parentID]++
	}

	allowedSections := map[string]struct{}{edit.RootSectionPublicID: {}}
	for publicID := range categoryByID {
		allowedSections[publicID] = struct{}{}
	}
	resourcePublicIDs := make([]string, 0, len(edit.Resources))
	for _, resource := range edit.Resources {
		if _, ok := allowedSections[resource.SectionPublicID]; !ok {
			return errCatalogEditorInvalid
		}
		resourcePublicIDs = append(resourcePublicIDs, resource.ResourcePublicID)
	}
	if len(resourcePublicIDs) > 0 {
		var templateDefinition []byte
		if err = s.db.QueryRow(ctx, `select template.definition
			from mod_content_sections section
			join mod_content_templates template on template.id=section.template_id
			where section.public_id=$1 and section.mod_id=$2 and section.version_id=$3
			  and section.parent_id is null and section.status='active'`,
			edit.RootSectionPublicID, modID, versionID).Scan(&templateDefinition); err != nil {
			return errCatalogEditorInvalid
		}
		var template struct {
			ResourceKinds []string `json:"resourceKinds"`
		}
		if err = json.Unmarshal(templateDefinition, &template); err != nil {
			return errCatalogEditorInvalid
		}
		allowedKinds := make(map[string]struct{}, len(template.ResourceKinds))
		for _, kindCode := range template.ResourceKinds {
			allowedKinds[strings.ToLower(strings.TrimSpace(kindCode))] = struct{}{}
		}

		rows, queryErr := s.db.Query(ctx, `select entity.public_id,resource.kind_code,resource.canonical_id,
			coalesce(representative.block_resource_id,0)::bigint,coalesce(representative.public_id,'')
			from catalog_entities entity
			join game_resources resource on resource.entity_id=entity.id
			join mod_resource_bindings mod_binding on mod_binding.resource_id=entity.id and mod_binding.mod_id=$2
			join mod_resource_version_details detail on detail.resource_id=entity.id and detail.version_id=$3 and detail.status='active'
			left join lateral (
				select asset_binding.block_resource_id,block_entity.public_id
				from game_resource_asset_bindings asset_binding
				join resource_import_snapshots block_snapshot on block_snapshot.id=asset_binding.snapshot_id
				join catalog_import_revisions revision on revision.id=block_snapshot.revision_id
				join catalog_entities block_entity on block_entity.id=asset_binding.block_resource_id
				  and block_entity.status='active'
				join mod_resource_version_details block_detail on block_detail.resource_id=asset_binding.block_resource_id
				  and block_detail.version_id=$3 and block_detail.status='active'
				where revision.target_version_id=$3
				  and revision.is_active and revision.status in ('ready','partial')
				  and (asset_binding.item_resource_id=entity.id or asset_binding.block_resource_id=entity.id)
				order by coalesce(revision.activated_at,revision.created_at) desc,asset_binding.block_resource_id
				limit 1
			) representative on true
			where entity.public_id=any($1::text[]) and entity.status='active'`,
			resourcePublicIDs, modID, versionID)
		if queryErr != nil {
			return errCatalogEditorInvalid
		}
		identities := make(map[string]modContentLayoutResourceIdentity, len(resourcePublicIDs))
		for rows.Next() {
			var publicID string
			var identity modContentLayoutResourceIdentity
			if err = rows.Scan(&publicID, &identity.KindCode, &identity.CanonicalID,
				&identity.BlockRepresentativeID, &identity.BlockRepresentativePublicID); err != nil {
				rows.Close()
				return errCatalogEditorInvalid
			}
			if len(allowedKinds) > 0 {
				if _, allowed := allowedKinds[identity.KindCode]; !allowed {
					rows.Close()
					return errCatalogEditorInvalid
				}
			}
			identities[publicID] = identity
		}
		if err = rows.Err(); err != nil {
			rows.Close()
			return errCatalogEditorInvalid
		}
		rows.Close()
		if len(identities) != len(resourcePublicIDs) {
			return errCatalogEditorInvalid
		}
		if err = validateModContentAdvancementLayout(edit.Resources, identities); err != nil {
			return err
		}
		edit.Resources = canonicalizeModContentLayoutResources(edit.Resources, identities)
	}
	sort.SliceStable(edit.Resources, func(i, j int) bool {
		left, right := edit.Resources[i], edit.Resources[j]
		if left.SectionPublicID != right.SectionPublicID {
			return left.SectionPublicID < right.SectionPublicID
		}
		if left.Ordinal != right.Ordinal {
			return left.Ordinal < right.Ordinal
		}
		return left.ResourcePublicID < right.ResourcePublicID
	})
	nextResourceOrdinal := make(map[string]int)
	for index := range edit.Resources {
		sectionID := edit.Resources[index].SectionPublicID
		edit.Resources[index].Ordinal = nextResourceOrdinal[sectionID]
		nextResourceOrdinal[sectionID]++
	}
	return nil
}

func validateModContentAdvancementLayout(resources []modContentLayoutResourceEdit, identities map[string]modContentLayoutResourceIdentity) error {
	parentByResource := make(map[string]string)
	for index := range resources {
		resource := &resources[index]
		if resource.Advancement == nil {
			continue
		}
		identity, exists := identities[resource.ResourcePublicID]
		if !exists || identity.KindCode != "minecraft.advancement" {
			return errCatalogEditorInvalid
		}
		parentID := resource.Advancement.ParentResourcePublicID
		if parentID != "" {
			parentIdentity, parentExists := identities[parentID]
			if !parentExists || parentIdentity.KindCode != "minecraft.advancement" {
				return errCatalogEditorInvalid
			}
		}
		parentByResource[resource.ResourcePublicID] = parentID
	}
	for resourceID := range parentByResource {
		current := resourceID
		visited := make(map[string]struct{})
		for current != "" {
			if _, duplicate := visited[current]; duplicate {
				return errCatalogEditorInvalid
			}
			visited[current] = struct{}{}
			current = parentByResource[current]
		}
	}
	return nil
}

func canonicalizeModContentLayoutResources(resources []modContentLayoutResourceEdit, identities map[string]modContentLayoutResourceIdentity) []modContentLayoutResourceEdit {
	result := make([]modContentLayoutResourceEdit, 0, len(resources))
	indexByIdentity := make(map[string]int, len(resources))
	kindByIdentity := make(map[string]string, len(resources))
	for _, resource := range resources {
		identity, exists := identities[resource.ResourcePublicID]
		if !exists {
			continue
		}
		identityKey := "resource:" + resource.ResourcePublicID
		if identity.KindCode == "minecraft.item" || identity.KindCode == "minecraft.block" {
			if identity.BlockRepresentativeID > 0 {
				identityKey = "item-block:resource:" + strconv.FormatInt(identity.BlockRepresentativeID, 10)
				if identity.BlockRepresentativePublicID != "" {
					resource.ResourcePublicID = identity.BlockRepresentativePublicID
				}
			} else {
				identityKey = "item-block:canonical:" + strings.ToLower(identity.CanonicalID)
			}
		}
		if existingIndex, duplicate := indexByIdentity[identityKey]; duplicate {
			if identity.KindCode == "minecraft.block" && kindByIdentity[identityKey] != "minecraft.block" {
				result[existingIndex] = resource
				kindByIdentity[identityKey] = identity.KindCode
			}
			continue
		}
		indexByIdentity[identityKey] = len(result)
		kindByIdentity[identityKey] = identity.KindCode
		result = append(result, resource)
	}
	return result
}

func readModContentDescendantSections(ctx context.Context, db modContentQuerier, modID, rootSectionID int64) ([]map[string]any, error) {
	rows, err := db.Query(ctx, `with recursive subtree as (
			select id,parent_id,array[ordinal::bigint,id] sort_path from mod_content_sections
			where id=$1 and mod_id=$2 and status='active'
			union all
			select child.id,child.parent_id,parent.sort_path||array[child.ordinal::bigint,child.id]
			from mod_content_sections child join subtree parent on child.parent_id=parent.id where child.status='active'
		)
		select section.public_id,version.public_id,template.public_id,template.code,template.builtin,template.i18n_key,
			coalesce(parent.public_id,''),section.system_key,section.default_locale,section.display_mode,section.ordinal,section.status,
			(select revision.public_id from content_revisions revision where revision.id=section.published_revision_id),
			coalesce((select jsonb_agg(jsonb_build_object('locale',localization.locale,'name',localization.name,
			 'summary',localization.description,'contentMarkdown','') order by localization.locale)
			 from mod_content_section_localizations localization where localization.section_id=section.id),'[]'::jsonb),
			coalesce((select count(*)::int from mod_content_section_resources resource where resource.section_id=section.id),0)
		from subtree join mod_content_sections section on section.id=subtree.id
		join mod_content_versions version on version.id=section.version_id
		join mod_content_templates template on template.id=section.template_id
		left join mod_content_sections parent on parent.id=section.parent_id
		where section.id<>$1 order by subtree.sort_path`, rootSectionID, modID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var publicID, versionPublicID, templatePublicID, templateCode, templateI18nKey, parentPublicID, systemKey, defaultLocale, displayMode, status string
		var templateBuiltin bool
		var ordinal, resourceCount int
		var revisionID *string
		var localizations []byte
		if err = rows.Scan(&publicID, &versionPublicID, &templatePublicID, &templateCode, &templateBuiltin, &templateI18nKey,
			&parentPublicID, &systemKey, &defaultLocale, &displayMode, &ordinal, &status, &revisionID, &localizations, &resourceCount); err != nil {
			return nil, err
		}
		items = append(items, map[string]any{
			"publicId": publicID, "versionPublicId": versionPublicID, "templatePublicId": templatePublicID,
			"templateCode": templateCode, "templateBuiltin": templateBuiltin, "templateI18nKey": templateI18nKey,
			"parentPublicId": parentPublicID, "systemKey": systemKey, "defaultLocale": defaultLocale, "displayMode": displayMode,
			"ordinal": ordinal, "status": status, "publishedRevisionId": revisionID,
			"localizations": json.RawMessage(localizations), "resourceCount": resourceCount,
		})
	}
	return items, rows.Err()
}

func publishModContentLayoutTx(ctx context.Context, tx pgx.Tx, revisionID int64, snapshot modContentSnapshot, actorID int64) error {
	layout := snapshot.Layout
	if layout == nil || layout.RootSectionPublicID != snapshot.PublicID {
		return errCatalogEditorInvalid
	}
	if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock(hashtext($1))`, modContentAggregateSection+":"+snapshot.PublicID); err != nil {
		return err
	}
	var rootID, versionID, templateID int64
	var displayMode string
	if err := tx.QueryRow(ctx, `select section.id,section.version_id,section.template_id,section.display_mode
		from mod_content_sections section join mod_content_versions version on version.id=section.version_id
		where section.public_id=$1 and section.mod_id=$2 and section.parent_id is null and section.status='active'
		and version.public_id=$3 for update`, snapshot.PublicID, snapshot.ModID, layout.VersionPublicID).
		Scan(&rootID, &versionID, &templateID, &displayMode); err != nil {
		return err
	}

	existingRows, err := tx.Query(ctx, `with recursive subtree as (
			select id,public_id from mod_content_sections where id=$1
			union all select child.id,child.public_id from mod_content_sections child join subtree parent on child.parent_id=parent.id
		)
		select id,public_id from subtree where id<>$1`, rootID)
	if err != nil {
		return err
	}
	existingIDs := make(map[string]int64)
	for existingRows.Next() {
		var id int64
		var publicID string
		if err = existingRows.Scan(&id, &publicID); err != nil {
			existingRows.Close()
			return err
		}
		existingIDs[publicID] = id
	}
	if err = existingRows.Err(); err != nil {
		existingRows.Close()
		return err
	}
	existingRows.Close()
	if _, err = tx.Exec(ctx, `with recursive subtree as (
			select id from mod_content_sections where id=$1
			union all select child.id from mod_content_sections child join subtree parent on child.parent_id=parent.id
		), ranked as (
			select id,row_number() over(order by id) position from subtree where id<>$1
		)
		update mod_content_sections section set ordinal=1000000000+ranked.position::int
		from ranked where section.id=ranked.id`, rootID); err != nil {
		return err
	}

	categoryByID := make(map[string]modContentLayoutCategoryEdit, len(layout.Categories))
	for _, category := range layout.Categories {
		categoryByID[category.PublicID] = category
	}
	depthCache := map[string]int{layout.RootSectionPublicID: 0}
	var depth func(string) int
	depth = func(publicID string) int {
		if value, ok := depthCache[publicID]; ok {
			return value
		}
		value := depth(categoryByID[publicID].ParentPublicID) + 1
		depthCache[publicID] = value
		return value
	}
	categories := append([]modContentLayoutCategoryEdit(nil), layout.Categories...)
	sort.SliceStable(categories, func(i, j int) bool {
		leftDepth, rightDepth := depth(categories[i].PublicID), depth(categories[j].PublicID)
		if leftDepth != rightDepth {
			return leftDepth < rightDepth
		}
		if categories[i].ParentPublicID != categories[j].ParentPublicID {
			return categories[i].ParentPublicID < categories[j].ParentPublicID
		}
		return categories[i].Ordinal < categories[j].Ordinal
	})
	sectionIDs := map[string]int64{layout.RootSectionPublicID: rootID}
	activeCategoryIDs := make(map[string]struct{}, len(categories))
	for _, category := range categories {
		parentID, ok := sectionIDs[category.ParentPublicID]
		if !ok {
			return errCatalogEditorInvalid
		}
		categoryID, exists := existingIDs[category.PublicID]
		if exists {
			err = tx.QueryRow(ctx, `update mod_content_sections set parent_id=$2,default_locale=$3,display_mode=$4,ordinal=$5,
				status='active',published_revision_id=$6,updated_by=$7,updated_at=now()
				where id=$1 and mod_id=$8 and version_id=$9 returning id`,
				categoryID, parentID, category.DefaultLocale, displayMode, category.Ordinal, revisionID, actorID, snapshot.ModID, versionID).Scan(&categoryID)
		} else {
			err = tx.QueryRow(ctx, `insert into mod_content_sections(public_id,mod_id,version_id,template_id,parent_id,default_locale,display_mode,
				ordinal,status,published_revision_id,created_by,updated_by)
				values($1,$2,$3,$4,$5,$6,$7,$8,'active',$9,$10,$10) returning id`,
				category.PublicID, snapshot.ModID, versionID, templateID, parentID, category.DefaultLocale, displayMode,
				category.Ordinal, revisionID, actorID).Scan(&categoryID)
		}
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `delete from mod_content_section_localizations where section_id=$1`, categoryID); err != nil {
			return err
		}
		for _, localization := range category.Localizations {
			if _, err = tx.Exec(ctx, `insert into mod_content_section_localizations(section_id,locale,name,description)
				values($1,$2,$3,$4)`, categoryID, localization.Locale, localization.Name, localization.Summary); err != nil {
				return err
			}
		}
		sectionIDs[category.PublicID] = categoryID
		activeCategoryIDs[category.PublicID] = struct{}{}
	}

	if _, err = tx.Exec(ctx, `with recursive subtree as (
			select id from mod_content_sections where id=$1
			union all select child.id from mod_content_sections child join subtree parent on child.parent_id=parent.id
		)
		delete from mod_content_section_resources where section_id in(select id from subtree)`, rootID); err != nil {
		return err
	}
	for _, resource := range layout.Resources {
		sectionID, ok := sectionIDs[resource.SectionPublicID]
		if !ok {
			return errCatalogEditorInvalid
		}
		var resourceID int64
		if err = tx.QueryRow(ctx, `select entity.id from catalog_entities entity
			join mod_resource_bindings binding on binding.resource_id=entity.id and binding.mod_id=$2
			join mod_resource_version_details detail on detail.resource_id=entity.id and detail.version_id=$3 and detail.status='active'
			where entity.public_id=$1 and entity.status='active'`, resource.ResourcePublicID, snapshot.ModID, versionID).Scan(&resourceID); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `insert into mod_content_section_resources(section_id,version_id,resource_id,ordinal,placement_source)
			values($1,$2,$3,$4,'manual')`, sectionID, versionID, resourceID, resource.Ordinal); err != nil {
			return err
		}
		if resource.Advancement != nil {
			if err = publishModContentAdvancementLayoutTx(ctx, tx, resourceID, versionID, revisionID, actorID, *resource.Advancement); err != nil {
				return err
			}
		}
	}
	for publicID, categoryID := range existingIDs {
		if _, keep := activeCategoryIDs[publicID]; keep {
			continue
		}
		if _, err = tx.Exec(ctx, `update mod_content_sections set status='archived',published_revision_id=$2,
			updated_by=$3,updated_at=now() where id=$1`, categoryID, revisionID, actorID); err != nil {
			return err
		}
	}
	_, err = tx.Exec(ctx, `update mod_content_sections set published_revision_id=$2,updated_by=$3,updated_at=now()
		where id=$1`, rootID, revisionID, actorID)
	return err
}

func publishModContentAdvancementLayoutTx(
	ctx context.Context,
	tx pgx.Tx,
	resourceID, versionID, revisionID, actorID int64,
	layout modContentAdvancementLayoutEdit,
) error {
	parentCanonicalID := ""
	if layout.ParentResourcePublicID != "" {
		if err := tx.QueryRow(ctx, `select resource.canonical_id
			from catalog_entities entity join game_resources resource on resource.entity_id=entity.id
			where entity.public_id=$1 and entity.status='active' and resource.kind_code='minecraft.advancement'`,
			layout.ParentResourcePublicID).Scan(&parentCanonicalID); err != nil {
			return err
		}
	}
	var definition []byte
	if err := tx.QueryRow(ctx, `select case
			when detail.definition is not null and detail.definition<>'{}'::jsonb then detail.definition
			else coalesce((select snapshot.data from resource_import_snapshots snapshot
				join catalog_import_revisions revision on revision.id=snapshot.revision_id
				where snapshot.resource_id=detail.resource_id and revision.target_version_id=detail.version_id
				 and revision.is_active and revision.status in ('ready','partial')
				order by coalesce(revision.activated_at,revision.created_at) desc limit 1),'{}'::jsonb)
			end
		from mod_resource_version_details detail
		where detail.resource_id=$1 and detail.version_id=$2 and detail.status='active'
		for update`, resourceID, versionID).Scan(&definition); err != nil {
		return err
	}
	value := make(map[string]any)
	if len(definition) > 0 {
		if err := json.Unmarshal(definition, &value); err != nil {
			return errCatalogEditorInvalid
		}
	}
	if parentCanonicalID == "" {
		delete(value, "parent")
	} else {
		value["parent"] = parentCanonicalID
	}
	display, _ := value["display"].(map[string]any)
	if display == nil {
		display = make(map[string]any)
	}
	display["x"] = layout.X
	display["y"] = layout.Y
	value["display"] = display
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `update mod_resource_version_details
		set definition=$3::jsonb,published_revision_id=$4,updated_by=$5,updated_at=now()
		where resource_id=$1 and version_id=$2 and status='active'`,
		resourceID, versionID, string(encoded), revisionID, actorID)
	return err
}

func validateModContentResourceSectionTx(ctx context.Context, tx pgx.Tx, modID, versionID int64, kindCode string, sectionPublicID *string) error {
	return validateModContentResourceSection(ctx, tx, modID, versionID, kindCode, sectionPublicID)
}

func validateModContentResourceSection(ctx context.Context, db modContentQueryRower, modID, versionID int64, kindCode string, sectionPublicID *string) error {
	if sectionPublicID == nil || *sectionPublicID == "" {
		return nil
	}
	var definition []byte
	if err := db.QueryRow(ctx, `select template.definition from mod_content_sections section
		join mod_content_templates template on template.id=section.template_id
		where section.public_id=$1 and section.mod_id=$2 and section.version_id=$3 and section.status='active'`,
		*sectionPublicID, modID, versionID).Scan(&definition); err != nil {
		return err
	}
	var template struct {
		ResourceKinds []string `json:"resourceKinds"`
	}
	if err := json.Unmarshal(definition, &template); err != nil {
		return err
	}
	if len(template.ResourceKinds) == 0 {
		return nil
	}
	for _, allowed := range template.ResourceKinds {
		if strings.EqualFold(strings.TrimSpace(allowed), kindCode) {
			return nil
		}
	}
	return errCatalogEditorInvalid
}

func moveModContentResourceToSectionTx(ctx context.Context, tx pgx.Tx, revisionID, modID, versionID, resourceID int64, sectionPublicID string, actorID int64) error {
	placementResourceID := resourceID
	var kindCode, canonicalID string
	if err := tx.QueryRow(ctx, `select kind_code,canonical_id from game_resources where entity_id=$1`,
		resourceID).Scan(&kindCode, &canonicalID); err != nil {
		return err
	}
	if kindCode == "minecraft.item" {
		representativeErr := tx.QueryRow(ctx, `select candidate.resource_id from (
				select binding.block_resource_id resource_id,0 preference
				from game_resource_asset_bindings binding
				join resource_import_snapshots block_snapshot on block_snapshot.id=binding.snapshot_id
				join catalog_import_revisions revision on revision.id=block_snapshot.revision_id
				where binding.item_resource_id=$1 and revision.target_version_id=$2
				  and revision.is_active and revision.status in ('ready','partial')
				union all
				select block.entity_id,1
				from game_resources item
				join game_resources block on block.kind_code='minecraft.block' and block.canonical_id=item.canonical_id
				where item.entity_id=$1
			) candidate
			join mod_resource_version_details detail on detail.resource_id=candidate.resource_id
			 and detail.version_id=$2 and detail.status='active'
			order by candidate.preference,candidate.resource_id limit 1`, resourceID, versionID).Scan(&placementResourceID)
		if representativeErr != nil && !errors.Is(representativeErr, pgx.ErrNoRows) {
			return representativeErr
		}
	}
	deleteLogicalPlacement := func() error {
		_, deleteErr := tx.Exec(ctx, `delete from mod_content_section_resources placement
			using mod_content_sections section,game_resources candidate
			where section.id=placement.section_id and section.mod_id=$1
			  and candidate.entity_id=placement.resource_id and placement.version_id=$2
			  and (
				candidate.entity_id in ($3,$4)
				or (
					candidate.kind_code in ('minecraft.item','minecraft.block')
					and candidate.canonical_id=$5
				)
				or exists(
					select 1
					from game_resource_asset_bindings binding
					join resource_import_snapshots block_snapshot on block_snapshot.id=binding.snapshot_id
					join catalog_import_revisions revision on revision.id=block_snapshot.revision_id
					where revision.target_version_id=$2 and revision.is_active
					  and revision.status in ('ready','partial')
					  and binding.block_resource_id=$4
					  and candidate.entity_id in (binding.item_resource_id,binding.block_resource_id)
				)
			  )`,
			modID, versionID, resourceID, placementResourceID, canonicalID)
		return deleteErr
	}
	if sectionPublicID == "" {
		if err := deleteLogicalPlacement(); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `update mod_content_sections set published_revision_id=$3,updated_by=$4,updated_at=now()
			where mod_id=$1 and version_id=$2 and parent_id is null and status='active'`, modID, versionID, revisionID, actorID)
		return err
	}
	var targetID, rootID int64
	if err := tx.QueryRow(ctx, `with recursive ancestors as (
			select id,parent_id,0 depth from mod_content_sections where public_id=$1 and mod_id=$2 and version_id=$3 and status='active'
			union all select parent.id,parent.parent_id,child.depth+1 from mod_content_sections parent join ancestors child on child.parent_id=parent.id
		)
		select (select id from ancestors where depth=0),
			(select id from ancestors where parent_id is null limit 1)`, sectionPublicID, modID, versionID).Scan(&targetID, &rootID); err != nil {
		return err
	}
	if err := deleteLogicalPlacement(); err != nil {
		return err
	}
	var ordinal int
	if err := tx.QueryRow(ctx, `select coalesce(max(ordinal)+1,0) from mod_content_section_resources
		where section_id=$1 and version_id=$2`, targetID, versionID).Scan(&ordinal); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `insert into mod_content_section_resources(section_id,version_id,resource_id,ordinal,placement_source)
		values($1,$2,$3,$4,'manual')`, targetID, versionID, placementResourceID, ordinal); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `update mod_content_sections set published_revision_id=$2,updated_by=$3,updated_at=now()
		where id=$1`, rootID, revisionID, actorID)
	return err
}
