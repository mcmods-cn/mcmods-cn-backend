package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
)

type catalogDeleteRequest struct {
	BaseRevisionID *string `json:"baseRevisionId"`
	Reason         string  `json:"reason"`
}

func logCatalogEditorReadFailure(queryContext string, err error) {
	log.Printf("catalog editor detail %s: %v", queryContext, err)
}

func (s *Server) catalogResources(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	kindCode := strings.TrimSpace(r.URL.Query().Get("kindCode"))
	if kindCode == "" {
		kindCode = strings.TrimSpace(r.URL.Query().Get("kind"))
	}
	registry := strings.TrimSpace(r.URL.Query().Get("registry"))
	status := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("status")))
	if status == "" {
		status = "active"
	}
	if !stringSet("active", "pending", "archived")[status] {
		writeError(w, http.StatusBadRequest, "invalid resource status")
		return
	}
	hasBindings := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("hasBindings")))
	if hasBindings != "" && hasBindings != "true" && hasBindings != "false" {
		writeError(w, http.StatusBadRequest, "invalid binding filter")
		return
	}
	primary, secondary := s.requestContentLocales(r)
	if requested := strings.TrimSpace(r.URL.Query().Get("locale")); requested != "" {
		var err error
		primary, err = normalizeCatalogLocale(requested)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid locale")
			return
		}
	}
	if requested := strings.TrimSpace(r.URL.Query().Get("secondaryLocale")); requested != "" {
		var err error
		secondary, err = normalizeCatalogLocale(requested)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid secondaryLocale")
			return
		}
	}
	limit := boundedLimit(r.URL.Query().Get("limit"), 40, 100)
	offset := boundedOffset(r.URL.Query().Get("offset"))
	indexed := indexedSearchPage{}
	if status == "active" && hasBindings == "" {
		indexed = s.searchResourcePage(r.Context(), query, kindCode, registry, limit, offset)
	}
	databaseOffset := offset
	if indexed.Used {
		databaseOffset = 0
	}
	var total int
	if indexed.Used {
		total = indexed.Total
	} else if err := s.db.QueryRow(r.Context(), `with `+latestGlobalExportScopeCTE+`, `+latestGlobalResourceSnapshotCTE+`
		select count(*)::int from game_resources resource
		join catalog_entities entity on entity.id=resource.entity_id
		left join latest_resource_snapshots imported on imported.resource_id=resource.entity_id
		where entity.status=$4 and ($1='' or resource.kind_code=$1) and ($3='' or resource.namespace=$3)
		and ($5='' or ($5='true' and exists(select 1 from mod_resource_version_details detail where detail.resource_id=resource.entity_id))
		 or ($5='false' and not exists(select 1 from mod_resource_version_details detail where detail.resource_id=resource.entity_id))) and
		($2='' or entity.public_id=$2 or resource.canonical_id ilike '%'||$2||'%' or exists(select 1 from content_localizations localization
		 where localization.catalog_entity_id=entity.id and localization.name ilike '%'||$2||'%')
		 or coalesce(imported.names,'{}'::jsonb)::text ilike '%'||$2||'%')`, kindCode, query, registry, status, hasBindings).Scan(&total); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to count resources")
		return
	}
	rows, err := s.db.Query(r.Context(), `with `+latestGlobalExportScopeCTE+`, `+latestGlobalResourceSnapshotCTE+`
		select entity.id,entity.public_id,resource.kind_code,resource.canonical_id,resource.namespace,entity.default_locale,
		(select revision.public_id from content_revisions revision where revision.id=entity.published_revision_id),
		coalesce(localization.locale,''),coalesce(localization.name,''),
		(select public_id from oss_files where id=definition.icon_file_id),
		(select public_id from oss_files where id=definition.render_file_id),
		coalesce(imported.names,'{}'::jsonb) || coalesce((select jsonb_object_agg(candidate.locale,candidate.name) from content_localizations candidate
		 where candidate.catalog_entity_id=entity.id and candidate.name<>''),'{}'::jsonb),
		coalesce((select jsonb_object_agg(candidate.locale,candidate.provenance) from content_localizations candidate
		 where candidate.catalog_entity_id=entity.id and candidate.name<>''),'{}'::jsonb),
		coalesce(owner.project_code,''),coalesce(owner.slug,''),coalesce(owner.primary_name,''),coalesce(imported.icon_path,''),
		(select count(*)::int from mod_resource_version_details detail where detail.resource_id=resource.entity_id)
		from game_resources resource join catalog_entities entity on entity.id=resource.entity_id
		left join catalog_resource_definitions definition on definition.resource_id=resource.entity_id
		left join latest_resource_snapshots imported on imported.resource_id=resource.entity_id
		left join mods owner on owner.id=resource.owner_mod_id and owner.review_status='approved'
		left join lateral (select candidate.* from content_localizations candidate where candidate.catalog_entity_id=entity.id
			 order by case candidate.locale when $4 then 0 when $5 then 1 when entity.default_locale then 2 when 'en-US' then 3 else 4 end limit 1) localization on true
		where entity.status=$10 and ($1='' or resource.kind_code=$1) and ($3='' or resource.namespace=$3)
		and ($11='' or ($11='true' and exists(select 1 from mod_resource_version_details detail where detail.resource_id=resource.entity_id))
		 or ($11='false' and not exists(select 1 from mod_resource_version_details detail where detail.resource_id=resource.entity_id))) and
		(($6 and entity.id=any($7::bigint[])) or (not $6 and ($2='' or entity.public_id=$2 or resource.canonical_id ilike '%'||$2||'%' or exists(select 1 from content_localizations candidate
		 where candidate.catalog_entity_id=entity.id and candidate.name ilike '%'||$2||'%')
		 or coalesce(imported.names,'{}'::jsonb)::text ilike '%'||$2||'%')))
		order by case when $6 then array_position($7::bigint[],entity.id) end,resource.kind_code,resource.canonical_id
		limit $8 offset $9`, kindCode, query, registry, primary, secondary, indexed.Used, indexed.IDs, limit, databaseOffset, status, hasBindings)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read resources")
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0, limit)
	for rows.Next() {
		var entityID int64
		var publicID, kind, canonicalID, namespace, defaultLocale, contentLocale, name string
		var ownerPublicID, ownerSiteID, ownerName, importedIconPath string
		var names, provenances []byte
		var revisionID sql.NullString
		var iconID, renderID sql.NullString
		var bindingCount int
		if err = rows.Scan(&entityID, &publicID, &kind, &canonicalID, &namespace, &defaultLocale, &revisionID, &contentLocale, &name,
			&iconID, &renderID, &names, &provenances, &ownerPublicID, &ownerSiteID, &ownerName, &importedIconPath, &bindingCount); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to decode resource")
			return
		}
		iconURL := catalogRecipeResourceIconURL(publicID, iconID.String, "", importedIconPath)
		renderURL := ""
		if renderID.Valid {
			renderURL = "/api/v1/catalog/resources/" + publicID + "/render"
		}
		if resolvedLocale, resolvedName := catalogResolvedName(names, primary, secondary, defaultLocale); resolvedName != "" {
			contentLocale, name = resolvedLocale, resolvedName
		}
		provenance := catalogLocalizationProvenance(provenances, contentLocale, name)
		items = append(items, map[string]any{"publicId": publicID, "id": canonicalID,
			"kind": kind, "registry": namespace, "names": json.RawMessage(names),
			"defaultLocale": defaultLocale, "publishedRevisionId": nullableCatalogString(revisionID), "locale": contentLocale,
			"name": name, "provenance": provenance, "iconUrl": iconURL, "renderUrl": renderURL, "bindingCount": bindingCount,
			"iconFileId": nullableCatalogString(iconID), "renderFileId": nullableCatalogString(renderID),
			"source": map[string]any{"publicId": ownerPublicID, "siteId": ownerSiteID, "name": ownerName, "type": "mod", "version": ""}})
	}
	if err = rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read resources")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": total, "limit": limit, "offset": offset})
}

func catalogLocalizationProvenance(raw []byte, locale, name string) string {
	if strings.TrimSpace(name) == "" {
		return ""
	}
	var values map[string]string
	if len(raw) > 0 && json.Unmarshal(raw, &values) == nil {
		if provenance := strings.TrimSpace(values[normalizeContentLocale(locale)]); provenance != "" {
			return provenance
		}
	}
	return "import"
}

func (s *Server) createCatalogResource(w http.ResponseWriter, r *http.Request) {
	var edit catalogResourceEdit
	if decodeJSON(r, &edit) != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if edit.BaseRevisionID != nil {
		writeError(w, http.StatusUnprocessableEntity, "baseRevisionId must be empty when creating")
		return
	}
	if err := normalizeCatalogResourceEdit(r.Context(), s.db, &edit); err != nil {
		writeError(w, catalogEditorHTTPStatus(err), catalogEditorErrorMessage(err))
		return
	}
	if err := requireCatalogCreateDefaultLocalization(edit.DefaultLocale, edit.Localizations); err != nil {
		writeError(w, catalogEditorHTTPStatus(err), catalogEditorErrorMessage(err))
		return
	}
	resolver, err := loadCatalogResourceIdentityResolver(r.Context(), s.db)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load Mod ID aliases")
		return
	}
	resolvedIdentity := resolver.resolve(edit.KindCode, edit.CanonicalID)
	edit.RawCanonicalID = resolvedIdentity.RawID
	edit.CanonicalID = resolvedIdentity.CanonicalID
	catalogIdentity := resolvedIdentity.catalogIdentity
	snapshot := catalogEditorSnapshot{Operation: "create", Reason: edit.Reason, Kind: "resource", IdentityKey: catalogIdentity.ID,
		PublicID: catalogIdentity.PublicID, DefaultLocale: edit.DefaultLocale, Localizations: edit.Localizations, Resource: &edit}
	result, err := s.submitCatalogEditorMutation(r, snapshot, nil)
	writeCatalogMutationResult(w, result, err)
}

func (s *Server) catalogResourceDetail(w http.ResponseWriter, r *http.Request) {
	entity, err := s.catalogEditorEntityByPublicID(r.Context(), r.PathValue("publicId"), "resource")
	if errors.Is(err, errCatalogEditorNotFound) || err == nil && entity.Status != "active" && currentClaims(r).Subject == 0 {
		writeError(w, http.StatusNotFound, "resource not found")
		return
	}
	if err != nil {
		logCatalogEditorReadFailure("resource entity", err)
		writeError(w, http.StatusInternalServerError, "failed to read resource")
		return
	}
	s.writeCatalogResourceDetail(w, r, entity)
}

func normalizeCatalogResourceEdit(ctx context.Context, query modContentTemplateRows, edit *catalogResourceEdit) error {
	var err error
	edit.KindCode, err = canonicalCatalogString(edit.KindCode, 160)
	if err != nil {
		return err
	}
	edit.CanonicalID, err = canonicalCatalogString(edit.CanonicalID, 512)
	if err != nil {
		return err
	}
	edit.DefaultLocale, edit.Localizations, err = normalizeCatalogLocalizations(edit.DefaultLocale, edit.Localizations)
	if err != nil {
		return err
	}
	edit.Definition = nonNilJSONObject(edit.Definition)
	edit.Definition, err = canonicalizeGlobalCatalogResourceDefinition(ctx, query, edit.KindCode, edit.CanonicalID, edit.Definition)
	if err != nil {
		return err
	}
	return validateCatalogResourceDefinition(edit.KindCode, edit.Definition)
}

func (s *Server) writeCatalogResourceDetail(w http.ResponseWriter, r *http.Request, entity catalogEditorEntity) {
	var kindCode, canonicalID string
	var definition, importedNames []byte
	var definitionSchemaVersion int
	var iconID, renderID sql.NullString
	err := s.db.QueryRow(r.Context(), `with `+latestGlobalExportScopeCTE+`, `+latestGlobalResourceSnapshotCTE+`
		select resource.kind_code,resource.canonical_id,
		case when definition.resource_id is not null then definition.definition
		 when imported.resource_id is not null then imported.data
		 else '{}'::jsonb end,
		coalesce(definition.definition_schema_version,imported.definition_schema_version,1),
		(select public_id from oss_files where id=definition.icon_file_id),
		(select public_id from oss_files where id=definition.render_file_id),coalesce(imported.names,'{}'::jsonb)
		from game_resources resource
		left join catalog_resource_definitions definition on definition.resource_id=resource.entity_id
		left join latest_resource_snapshots imported on imported.resource_id=resource.entity_id
		where resource.entity_id=$1`, entity.ID).
		Scan(&kindCode, &canonicalID, &definition, &definitionSchemaVersion, &iconID, &renderID, &importedNames)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "resource not found")
		return
	}
	if err != nil {
		logCatalogEditorReadFailure("resource definition", err)
		writeError(w, http.StatusInternalServerError, "failed to read resource")
		return
	}
	localizations, err := s.catalogLocalizationRows(r.Context(), entity.ID)
	if err != nil {
		logCatalogEditorReadFailure("resource localizations", err)
		writeError(w, http.StatusInternalServerError, "failed to read localizations")
		return
	}
	if len(localizations) == 0 {
		localizations = importedCatalogLocalizationRows(importedNames)
	}
	defaultLocale := entity.DefaultLocale
	if entity.PublishedRevisionID == nil && len(localizations) > 0 && !catalogLocalizationMapContains(localizations, defaultLocale) {
		defaultLocale, _ = localizations[0]["locale"].(string)
	}
	iconURL, renderURL := "", ""
	if iconID.Valid {
		iconURL = "/api/v1/catalog/resources/" + entity.PublicID + "/icon"
	}
	if renderID.Valid {
		renderURL = "/api/v1/catalog/resources/" + entity.PublicID + "/render"
	}
	publishedRevisionPublicID, revisionErr := revisionPublicIDValue(r.Context(), s.db, entity.PublishedRevisionID)
	if revisionErr != nil {
		logCatalogEditorReadFailure("resource published revision", revisionErr)
		writeError(w, http.StatusInternalServerError, "failed to resolve published revision")
		return
	}
	reviewStatus, reviewErr := s.catalogPendingReviewStatus(r.Context(), entity.ID)
	if reviewErr != nil {
		logCatalogEditorReadFailure("resource review status", reviewErr)
		writeError(w, http.StatusInternalServerError, "failed to read review status")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"publicId": entity.PublicID, "status": entity.Status, "defaultLocale": defaultLocale,
		"publishedRevisionId": publishedRevisionPublicID, "reviewStatus": reviewStatus,
		"kindCode": kindCode, "canonicalId": canonicalID, "definitionSchemaVersion": definitionSchemaVersion, "definition": json.RawMessage(definition), "iconFileId": nullableCatalogString(iconID),
		"renderFileId": nullableCatalogString(renderID), "iconUrl": iconURL, "renderUrl": renderURL, "localizations": localizations})
}

func catalogLocalizationMapContains(localizations []map[string]any, locale string) bool {
	locale = normalizeContentLocale(locale)
	for _, localization := range localizations {
		candidate, _ := localization["locale"].(string)
		if normalizeContentLocale(candidate) == locale {
			return true
		}
	}
	return false
}

func importedCatalogLocalizationRows(raw []byte) []map[string]any {
	var names map[string]string
	if len(raw) == 0 || json.Unmarshal(raw, &names) != nil {
		return []map[string]any{}
	}
	normalized := make(map[string]string, len(names))
	for rawLocale, rawName := range names {
		locale := normalizeContentLocale(rawLocale)
		name := strings.TrimSpace(rawName)
		if name == "" || locale == "" {
			continue
		}
		if !isEditableContentLocale(locale) {
			base, _, _ := strings.Cut(locale, "-")
			if isEditableContentLocale(base) {
				locale = base
			} else {
				continue
			}
		}
		if _, exists := normalized[locale]; !exists {
			normalized[locale] = name
		}
	}
	locales := make([]string, 0, len(normalized))
	for locale := range normalized {
		locales = append(locales, locale)
	}
	sort.Strings(locales)
	result := make([]map[string]any, 0, len(locales))
	for _, locale := range locales {
		result = append(result, map[string]any{
			"locale": locale, "name": normalized[locale], "summary": "", "contentMarkdown": "",
			"provenance": "import", "sourceLocale": "", "revisionNo": 0, "editable": true,
			"reviewStatus": "approved", "publishedRevisionId": nil,
		})
	}
	return result
}

// catalogImportedEditorLocalizations turns import-only metadata into the same
// editable shape as canonical localizations. It intentionally does not persist
// these rows: the first human PUT still creates a reviewed content revision and
// publishes the selected locales through the normal catalog editor workflow.
func catalogImportedEditorLocalizations(raw []byte, defaultLocale, fallbackName string) ([]map[string]any, string) {
	localizations := importedCatalogLocalizationRows(raw)
	defaultLocale = normalizeContentLocale(defaultLocale)
	if !isEditableContentLocale(defaultLocale) {
		defaultLocale = "en-US"
	}
	if len(localizations) == 0 {
		fallbackName = strings.TrimSpace(fallbackName)
		if fallbackName == "" {
			return localizations, defaultLocale
		}
		localizations = []map[string]any{{
			"locale": defaultLocale, "name": fallbackName, "summary": "", "contentMarkdown": "",
			"provenance": "import", "sourceLocale": "", "revisionNo": 0, "editable": true,
			"reviewStatus": "approved", "publishedRevisionId": nil,
		}}
		return localizations, defaultLocale
	}
	if catalogLocalizationMapContains(localizations, defaultLocale) {
		return localizations, defaultLocale
	}
	if catalogLocalizationMapContains(localizations, "en-US") {
		return localizations, "en-US"
	}
	resolved, _ := localizations[0]["locale"].(string)
	return localizations, resolved
}

func (s *Server) catalogTags(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		var edit catalogTagEdit
		if decodeJSON(r, &edit) != nil || edit.BaseRevisionID != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		if err := normalizeCatalogTagEdit(&edit); err != nil {
			writeError(w, catalogEditorHTTPStatus(err), catalogEditorErrorMessage(err))
			return
		}
		if err := requireCatalogCreateDefaultLocalization(edit.DefaultLocale, edit.Localizations); err != nil {
			writeError(w, catalogEditorHTTPStatus(err), catalogEditorErrorMessage(err))
			return
		}
		identity := tagIdentity(edit.Registry, edit.CanonicalID)
		snapshot := catalogEditorSnapshot{Operation: "create", Reason: edit.Reason, Kind: "tag", IdentityKey: identity.ID, PublicID: identity.PublicID,
			DefaultLocale: edit.DefaultLocale, Localizations: edit.Localizations, Tag: &edit}
		result, err := s.submitCatalogEditorMutation(r, snapshot, nil)
		writeCatalogMutationResult(w, result, err)
		return
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	canonicalID := strings.TrimSpace(r.URL.Query().Get("canonicalId"))
	registry := strings.TrimSpace(r.URL.Query().Get("registry"))
	if len(canonicalID) > 512 || len(registry) > 160 {
		writeError(w, http.StatusBadRequest, "invalid exact tag filter")
		return
	}
	primary, secondary := s.requestContentLocales(r)
	if requested := normalizeContentLocale(r.URL.Query().Get("locale")); requested != "" {
		primary = requested
	}
	if requested := normalizeContentLocale(r.URL.Query().Get("secondaryLocale")); requested != "" {
		secondary = requested
	}
	limit := boundedLimit(r.URL.Query().Get("limit"), 40, 100)
	offset := boundedOffset(r.URL.Query().Get("offset"))
	var total int
	if err := s.db.QueryRow(r.Context(), `select count(*)::int from catalog_tags tag
		join catalog_entities entity on entity.id=tag.entity_id
		where entity.status='active' and `+publicCatalogEntitySQL("entity", "tag")+` and ($2='' or tag.canonical_id=$2) and ($3='' or tag.registry=$3) and ($1='' or tag.canonical_id ilike '%'||$1||'%' or exists(
		 select 1 from content_localizations localization where localization.catalog_entity_id=entity.id and localization.name ilike '%'||$1||'%'))`, query, canonicalID, registry).Scan(&total); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to count tags")
		return
	}
	rows, err := s.db.Query(r.Context(), `select entity.id,entity.public_id,tag.registry,tag.canonical_id,entity.default_locale,
		(select revision.public_id from content_revisions revision where revision.id=entity.published_revision_id),
		case when entity.published_revision_id is null then greatest(
		 (select count(*)::int from catalog_tag_members member where member.tag_id=tag.entity_id),
		 coalesce((select count(distinct member.resource_id)::int from tag_import_snapshots snapshot
		  join catalog_import_revisions revision on revision.id=snapshot.revision_id
		  join tag_import_members member on member.tag_snapshot_id=snapshot.id
		  where snapshot.tag_id=tag.entity_id and revision.is_active and revision.status in ('ready','partial') and exists(select 1 from mods public_source where public_source.id=revision.mod_id and public_source.review_status='approved')),0))
		else (select count(*)::int from catalog_tag_members member where member.tag_id=tag.entity_id) end,
		coalesce(localization.locale,''),coalesce(localization.name,''),
		coalesce((select jsonb_object_agg(candidate.locale,candidate.name) from content_localizations candidate
		 where candidate.catalog_entity_id=entity.id and candidate.name<>''),'{}'::jsonb)
		from catalog_tags tag join catalog_entities entity on entity.id=tag.entity_id
		left join lateral (select candidate.locale,candidate.name from content_localizations candidate
		 where candidate.catalog_entity_id=entity.id order by case candidate.locale when $2 then 0 when $3 then 1
			 when entity.default_locale then 2 when 'en-US' then 3 else 4 end limit 1) localization on true
		where entity.status='active' and `+publicCatalogEntitySQL("entity", "tag")+` and ($6='' or tag.canonical_id=$6) and ($7='' or tag.registry=$7) and ($1='' or tag.canonical_id ilike '%'||$1||'%' or exists(
		 select 1 from content_localizations localization where localization.catalog_entity_id=entity.id and localization.name ilike '%'||$1||'%'))
		order by tag.registry,tag.canonical_id limit $4 offset $5`, query, primary, secondary, limit, offset, canonicalID, registry)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read tags")
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0, limit)
	previewTagIDs := make([]int64, 0, limit)
	previewFallback := make(map[int64]bool, limit)
	for rows.Next() {
		var entityID int64
		var publicID, registry, canonicalID, defaultLocale string
		var contentLocale, name string
		var names []byte
		var revisionID sql.NullString
		var count int
		if err = rows.Scan(&entityID, &publicID, &registry, &canonicalID, &defaultLocale, &revisionID, &count, &contentLocale, &name, &names); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to decode tags")
			return
		}
		if resolvedLocale, resolvedName := catalogResolvedName(names, primary, secondary, defaultLocale); resolvedName != "" {
			contentLocale, name = resolvedLocale, resolvedName
		}
		previewTagIDs = append(previewTagIDs, entityID)
		previewFallback[entityID] = !revisionID.Valid
		items = append(items, map[string]any{"publicId": publicID, "registry": registry, "canonicalId": canonicalID,
			"defaultLocale": defaultLocale, "publishedRevisionId": nullableCatalogString(revisionID), "memberCount": count,
			"locale": contentLocale, "name": name, "names": json.RawMessage(names)})
	}
	if err = rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read tags")
		return
	}
	rows.Close()
	previews, err := s.catalogTagPreviewRows(r.Context(), previewTagIDs, previewFallback, primary, secondary)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read tag previews")
		return
	}
	for index, item := range items {
		item["previews"] = previews[previewTagIDs[index]]
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": total, "limit": limit, "offset": offset})
}

const catalogTagPreviewLimit = 8

func (s *Server) catalogTagPreviewRows(ctx context.Context, tagIDs []int64, allowFallback map[int64]bool, primary, secondary string) (map[int64][]map[string]any, error) {
	result := make(map[int64][]map[string]any, len(tagIDs))
	if len(tagIDs) == 0 {
		return result, nil
	}
	rows, err := s.db.Query(ctx, `with selected_members as (
		select member.tag_id,member.resource_id,member.ordinal,
		 row_number() over(partition by member.tag_id order by member.ordinal,member.resource_id) preview_rank
		from catalog_tag_members member where member.tag_id=any($1::bigint[])
	)
	select member.tag_id,entity.id,entity.public_id,resource.kind_code,resource.canonical_id,resource.namespace,member.ordinal,
	 entity.default_locale,(select public_id from oss_files where id=definition.icon_file_id),coalesce(imported.revision_id,''),coalesce(imported.icon_path,''),coalesce(imported.mod_site_id,''),
	 coalesce((select jsonb_object_agg(localization.locale,localization.name) from content_localizations localization
	  where localization.catalog_entity_id=entity.id and localization.name<>''),imported.names,'{}'::jsonb)
	from selected_members member join game_resources resource on resource.entity_id=member.resource_id
	join catalog_entities entity on entity.id=resource.entity_id
	left join catalog_resource_definitions definition on definition.resource_id=resource.entity_id
	left join lateral (select snapshot.names,snapshot.revision_id,snapshot.icon_path,mod.slug mod_site_id
	 from resource_import_snapshots snapshot join catalog_import_revisions revision on revision.id=snapshot.revision_id
	 join mods mod on mod.id=revision.mod_id where snapshot.resource_id=resource.entity_id
	 and revision.is_active and revision.status in ('ready','partial') and exists(select 1 from mods public_source where public_source.id=revision.mod_id and public_source.review_status='approved')
	 order by (snapshot.icon_path<>'') desc,coalesce(revision.activated_at,revision.created_at) desc limit 1) imported on true
	where member.preview_rank<=$2 and `+publicCatalogEntitySQL("entity", "resource")+` order by member.tag_id,member.ordinal`, tagIDs, catalogTagPreviewLimit)
	if err != nil {
		return nil, err
	}
	if err = scanCatalogTagPreviewRows(rows, result, primary, secondary); err != nil {
		return nil, err
	}
	fallbackIDs := make([]int64, 0, len(tagIDs))
	for _, tagID := range tagIDs {
		if len(result[tagID]) == 0 && allowFallback[tagID] {
			fallbackIDs = append(fallbackIDs, tagID)
		}
	}
	if len(fallbackIDs) == 0 {
		return result, nil
	}
	rows, err = s.db.Query(ctx, `with selected as (
		select snapshot.tag_id,snapshot.id,snapshot.revision_id
		from tag_import_snapshots snapshot join catalog_import_revisions revision on revision.id=snapshot.revision_id
		where snapshot.tag_id=any($1::bigint[]) and revision.is_active and revision.status in ('ready','partial') and exists(select 1 from mods public_source where public_source.id=revision.mod_id and public_source.review_status='approved')
	), selected_members as (
		select distinct on(selected.tag_id,member.resource_id) selected.tag_id,selected.revision_id,member.resource_id,member.ordinal
		from selected join tag_import_members member on member.tag_snapshot_id=selected.id
		order by selected.tag_id,member.resource_id,member.ordinal
	), ranked_members as (
		select selected_members.*,row_number() over(partition by tag_id order by ordinal,resource_id) preview_rank from selected_members
	)
	select member.tag_id,entity.id,entity.public_id,resource.kind_code,resource.canonical_id,resource.namespace,member.ordinal,
	 entity.default_locale,(select public_id from oss_files where id=definition.icon_file_id),coalesce(imported.revision_id,''),coalesce(imported.icon_path,''),coalesce(mod.slug,''),
	 coalesce((select jsonb_object_agg(localization.locale,localization.name) from content_localizations localization
	  where localization.catalog_entity_id=entity.id and localization.name<>''),imported.names,'{}'::jsonb)
	from ranked_members member join game_resources resource on resource.entity_id=member.resource_id
	join catalog_entities entity on entity.id=resource.entity_id
	left join catalog_resource_definitions definition on definition.resource_id=resource.entity_id
	left join lateral (select snapshot.names,snapshot.revision_id,snapshot.icon_path from resource_import_snapshots snapshot
	 join catalog_import_revisions source_revision on source_revision.id=snapshot.revision_id
	 join mods source_mod on source_mod.id=source_revision.mod_id and source_mod.review_status='approved'
	 where source_revision.is_active and source_revision.status in ('ready','partial') and snapshot.resource_id=resource.entity_id order by (snapshot.revision_id=member.revision_id) desc,
	 (snapshot.icon_path<>'') desc,snapshot.created_at desc limit 1) imported on true
	left join catalog_import_revisions revision on revision.id=imported.revision_id
	left join mods mod on mod.id=revision.mod_id
	where member.preview_rank<=$2 and `+publicCatalogEntitySQL("entity", "resource")+` order by member.tag_id,member.ordinal`, fallbackIDs, catalogTagPreviewLimit)
	if err != nil {
		return nil, err
	}
	if err = scanCatalogTagPreviewRows(rows, result, primary, secondary); err != nil {
		return nil, err
	}
	return result, nil
}

func scanCatalogTagPreviewRows(rows pgx.Rows, result map[int64][]map[string]any, primary, secondary string) error {
	defer rows.Close()
	for rows.Next() {
		var tagID, entityID int64
		var publicID, kindCode, canonicalID, registry, defaultLocale, revisionID, iconPath, modSiteID string
		var iconID sql.NullString
		var names []byte
		var ordinal int
		if err := rows.Scan(&tagID, &entityID, &publicID, &kindCode, &canonicalID, &registry, &ordinal, &defaultLocale,
			&iconID, &revisionID, &iconPath, &modSiteID, &names); err != nil {
			return err
		}
		locale, name := catalogResolvedName(names, primary, secondary, defaultLocale)
		compactNames := map[string]string{}
		if name != "" {
			if locale == "" {
				locale = primary
			}
			compactNames[locale] = name
		}
		iconURL := ""
		if iconID.Valid {
			iconURL = "/api/v1/catalog/resources/" + publicID + "/icon"
		}
		result[tagID] = append(result[tagID], map[string]any{
			"publicId": publicID, "id": canonicalID, "kind": kindCode, "registry": registry, "ordinal": ordinal,
			"locale": locale, "name": name, "names": compactNames, "iconFileId": nullableCatalogString(iconID),
			"iconUrl": iconURL, "revisionId": revisionID, "iconPath": iconPath, "modSiteId": modSiteID,
		})
	}
	return rows.Err()
}

func normalizeCatalogTagEdit(edit *catalogTagEdit) error {
	var err error
	edit.Registry, err = canonicalCatalogString(edit.Registry, 160)
	if err != nil {
		return err
	}
	edit.CanonicalID, err = canonicalCatalogString(edit.CanonicalID, 512)
	if err != nil || len(edit.MemberResourcePublicIDs) > maxExportTagMemberCount {
		return errCatalogEditorInvalid
	}
	edit.DefaultLocale, edit.Localizations, err = normalizeCatalogLocalizations(edit.DefaultLocale, edit.Localizations)
	return err
}

func (s *Server) catalogTagDetail(w http.ResponseWriter, r *http.Request) {
	entity, err := s.catalogEditorEntityByPublicID(r.Context(), r.PathValue("publicId"), "tag")
	if errors.Is(err, errCatalogEditorNotFound) || err == nil && entity.Status == "archived" && r.Method == http.MethodGet {
		writeError(w, http.StatusNotFound, "tag not found")
		return
	}
	if err != nil {
		logCatalogEditorReadFailure("tag entity", err)
		writeError(w, http.StatusInternalServerError, "failed to read tag")
		return
	}
	if r.Method == http.MethodGet && !s.requirePublicCatalogEntity(w, r, entity) {
		return
	}

	switch r.Method {
	case http.MethodGet:
		var registry, canonicalID string
		if err = s.db.QueryRow(r.Context(), `select registry,canonical_id from catalog_tags where entity_id=$1`, entity.ID).Scan(&registry, &canonicalID); errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "tag not found")
			return
		}
		if err != nil {
			logCatalogEditorReadFailure("tag definition", err)
			writeError(w, http.StatusInternalServerError, "failed to read tag")
			return
		}
		primary, secondary := s.catalogRequestedLocales(r)
		members, readErr := s.catalogTagMemberRows(r.Context(), entity.ID, primary, secondary, entity.PublishedRevisionID == nil)
		localizations, localeErr := s.catalogLocalizationRows(r.Context(), entity.ID)
		if readErr != nil || localeErr != nil {
			logCatalogEditorReadFailure("tag members/localizations", errors.Join(readErr, localeErr))
			writeError(w, http.StatusInternalServerError, "failed to read tag")
			return
		}
		defaultLocale := entity.DefaultLocale
		if entity.PublishedRevisionID == nil && len(localizations) == 0 {
			localizations, defaultLocale = catalogImportedEditorLocalizations(nil, defaultLocale, canonicalID)
		}
		publishedRevisionPublicID, revisionErr := revisionPublicIDValue(r.Context(), s.db, entity.PublishedRevisionID)
		if revisionErr != nil {
			logCatalogEditorReadFailure("tag published revision", revisionErr)
			writeError(w, http.StatusInternalServerError, "failed to resolve published revision")
			return
		}
		reviewStatus, reviewErr := s.catalogPendingReviewStatus(r.Context(), entity.ID)
		if reviewErr != nil {
			logCatalogEditorReadFailure("tag review status", reviewErr)
			writeError(w, http.StatusInternalServerError, "failed to read review status")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"publicId": entity.PublicID, "registry": registry, "canonicalId": canonicalID,
			"defaultLocale": defaultLocale, "publishedRevisionId": publishedRevisionPublicID,
			"reviewStatus": reviewStatus, "localizations": localizations,
			"memberCount": len(members), "members": members})
	case http.MethodPut:
		var edit catalogTagEdit
		if decodeJSON(r, &edit) != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		if err = normalizeCatalogTagEdit(&edit); err != nil {
			writeError(w, catalogEditorHTTPStatus(err), catalogEditorErrorMessage(err))
			return
		}
		snapshot := catalogEditorSnapshot{Operation: "edit", Reason: edit.Reason, Kind: "tag", EntityID: entity.ID, PublicID: entity.PublicID,
			DefaultLocale: edit.DefaultLocale, Localizations: edit.Localizations, Tag: &edit}
		result, submitErr := s.submitCatalogEditorMutation(r, snapshot, edit.BaseRevisionID)
		writeCatalogMutationResult(w, result, submitErr)
	case http.MethodDelete:
		s.deleteCatalogEntity(w, r, entity, "tag")
	}
}

func (s *Server) createCatalogRecipeType(w http.ResponseWriter, r *http.Request) {
	var edit catalogRecipeTypeEdit
	if decodeJSON(r, &edit) != nil || edit.BaseRevisionID != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := normalizeCatalogRecipeTypeEdit(&edit); err != nil {
		writeError(w, catalogEditorHTTPStatus(err), catalogEditorErrorMessage(err))
		return
	}
	if err := requireCatalogCreateDefaultLocalization(edit.DefaultLocale, edit.Localizations); err != nil {
		writeError(w, catalogEditorHTTPStatus(err), catalogEditorErrorMessage(err))
		return
	}
	identity := recipeTypeIdentity(edit.CanonicalID)
	snapshot := catalogEditorSnapshot{Operation: "create", Reason: edit.Reason, Kind: "recipe_type", IdentityKey: identity.ID, PublicID: identity.PublicID,
		DefaultLocale: edit.DefaultLocale, Localizations: edit.Localizations, RecipeType: &edit}
	result, err := s.submitCatalogEditorMutation(r, snapshot, nil)
	writeCatalogMutationResult(w, result, err)
}

func normalizeCatalogRecipeTypeEdit(edit *catalogRecipeTypeEdit) error {
	var err error
	edit.CanonicalID, err = canonicalCatalogString(edit.CanonicalID, 512)
	if err != nil || len(edit.CatalystResourcePublicIDs) > 500 {
		return errCatalogEditorInvalid
	}
	edit.DefaultLocale, edit.Localizations, err = normalizeCatalogLocalizations(edit.DefaultLocale, edit.Localizations)
	edit.Definition = nonNilJSONObject(edit.Definition)
	return err
}

func (s *Server) catalogRecipeTypeDetail(w http.ResponseWriter, r *http.Request) {
	entity, err := s.catalogEditorEntityByPublicID(r.Context(), r.PathValue("publicId"), "recipe_type")
	if errors.Is(err, errCatalogEditorNotFound) || err == nil && entity.Status == "archived" && r.Method == http.MethodGet {
		writeError(w, http.StatusNotFound, "recipe type not found")
		return
	}
	if err != nil {
		logCatalogEditorReadFailure("recipe type entity", err)
		writeError(w, http.StatusInternalServerError, "failed to read recipe type")
		return
	}
	if r.Method == http.MethodGet && !s.requirePublicCatalogEntity(w, r, entity) {
		return
	}

	if r.Method == http.MethodPut {
		var edit catalogRecipeTypeEdit
		if decodeJSON(r, &edit) != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		if err = normalizeCatalogRecipeTypeEdit(&edit); err != nil {
			writeError(w, catalogEditorHTTPStatus(err), catalogEditorErrorMessage(err))
			return
		}
		snapshot := catalogEditorSnapshot{Operation: "edit", Reason: edit.Reason, Kind: "recipe_type", EntityID: entity.ID, PublicID: entity.PublicID,
			DefaultLocale: edit.DefaultLocale, Localizations: edit.Localizations, RecipeType: &edit}
		result, submitErr := s.submitCatalogEditorMutation(r, snapshot, edit.BaseRevisionID)
		writeCatalogMutationResult(w, result, submitErr)
		return
	}
	if r.Method == http.MethodDelete {
		s.deleteCatalogEntity(w, r, entity, "recipe_type")
		return
	}
	var canonicalID string
	var definition, importedNames []byte
	if err = s.db.QueryRow(r.Context(), `select type.canonical_id,coalesce(definition.definition,'{}'::jsonb),
		coalesce(imported.title_names,'{}'::jsonb)
		from recipe_types type left join recipe_type_definitions definition on definition.recipe_type_id=type.entity_id
		left join lateral (select snapshot.title_names from recipe_type_import_snapshots snapshot
		 join catalog_import_revisions revision on revision.id=snapshot.revision_id
		 where snapshot.recipe_type_id=type.entity_id and revision.is_active and revision.status in ('ready','partial') and exists(select 1 from mods public_source where public_source.id=revision.mod_id and public_source.review_status='approved')
		 order by coalesce(revision.activated_at,revision.created_at) desc,revision.created_at desc limit 1) imported on true
		where type.entity_id=$1`, entity.ID).
		Scan(&canonicalID, &definition, &importedNames); errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "recipe type not found")
		return
	}
	if err != nil {
		logCatalogEditorReadFailure("recipe type definition", err)
		writeError(w, http.StatusInternalServerError, "failed to read recipe type")
		return
	}
	var templateCount, recipeCount int
	if err = s.db.QueryRow(r.Context(), `select count(*)::int from recipe_layout_templates template join catalog_entities entity on entity.id=template.entity_id
		where template.recipe_type_id=$1 and entity.status='active' and `+publicCatalogEntitySQL("entity", "recipe_template")+``, entity.ID).Scan(&templateCount); err != nil {
		logCatalogEditorReadFailure("recipe type template count", err)
		writeError(w, http.StatusInternalServerError, "failed to count recipe templates")
		return
	}
	if err = s.db.QueryRow(r.Context(), `select count(*)::int from recipes recipe join catalog_entities entity on entity.id=recipe.entity_id
		where recipe.recipe_type_id=$1 and entity.status='active' and `+publicCatalogEntitySQL("entity", "recipe")+``, entity.ID).Scan(&recipeCount); err != nil {
		logCatalogEditorReadFailure("recipe type recipe count", err)
		writeError(w, http.StatusInternalServerError, "failed to count recipes")
		return
	}
	localizations, localeErr := s.catalogLocalizationRows(r.Context(), entity.ID)
	if localeErr != nil {
		logCatalogEditorReadFailure("recipe type localizations", localeErr)
		writeError(w, http.StatusInternalServerError, "failed to read localizations")
		return
	}
	defaultLocale := entity.DefaultLocale
	if entity.PublishedRevisionID == nil && len(localizations) == 0 {
		localizations, defaultLocale = catalogImportedEditorLocalizations(importedNames, defaultLocale, canonicalID)
	}
	primary, secondary := s.catalogRequestedLocales(r)
	catalysts, catalystErr := s.catalogRecipeTypeCatalysts(r.Context(), entity.ID, primary, secondary, entity.PublishedRevisionID == nil)
	if catalystErr != nil {
		logCatalogEditorReadFailure("recipe type catalysts", catalystErr)
		writeError(w, http.StatusInternalServerError, "failed to read recipe catalysts")
		return
	}
	publishedRevisionPublicID, revisionErr := revisionPublicIDValue(r.Context(), s.db, entity.PublishedRevisionID)
	if revisionErr != nil {
		logCatalogEditorReadFailure("recipe type published revision", revisionErr)
		writeError(w, http.StatusInternalServerError, "failed to resolve published revision")
		return
	}
	reviewStatus, reviewErr := s.catalogPendingReviewStatus(r.Context(), entity.ID)
	if reviewErr != nil {
		logCatalogEditorReadFailure("recipe type review status", reviewErr)
		writeError(w, http.StatusInternalServerError, "failed to read review status")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"publicId": entity.PublicID, "canonicalId": canonicalID, "defaultLocale": defaultLocale,
		"publishedRevisionId": publishedRevisionPublicID, "reviewStatus": reviewStatus,
		"definition": json.RawMessage(definition), "localizations": localizations, "catalysts": catalysts,
		"templateCount": templateCount, "recipeCount": recipeCount})
}

func (s *Server) catalogRecipeTemplates(w http.ResponseWriter, r *http.Request) {
	typeEntity, err := s.catalogEditorEntityByPublicID(r.Context(), r.PathValue("typePublicId"), "recipe_type")
	if err != nil || typeEntity.Status != "active" {
		writeError(w, http.StatusNotFound, "recipe type not found")
		return
	}
	if r.Method == http.MethodGet && !s.requirePublicCatalogEntity(w, r, typeEntity) {
		return
	}

	if r.Method == http.MethodPost {
		var edit catalogRecipeTemplateEdit
		if decodeJSON(r, &edit) != nil || edit.BaseRevisionID != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		if err = normalizeCatalogTemplateEdit(&edit); err != nil {
			writeError(w, catalogEditorHTTPStatus(err), catalogEditorErrorMessage(err))
			return
		}
		if err = requireCatalogCreateDefaultLocalization(edit.DefaultLocale, edit.Localizations); err != nil {
			writeError(w, catalogEditorHTTPStatus(err), catalogEditorErrorMessage(err))
			return
		}
		identity := catalogEditorIdentityForTemplate(typeEntity.IdentityKey, edit.TemplateKey)
		snapshot := catalogEditorSnapshot{Operation: "create", Reason: edit.Reason, Kind: "recipe_template", IdentityKey: identity.ID,
			PublicID: identity.PublicID, ParentEntityID: typeEntity.ID, ParentPublicID: typeEntity.PublicID,
			DefaultLocale: edit.DefaultLocale, Localizations: edit.Localizations, Template: &edit}
		result, submitErr := s.submitCatalogEditorMutation(r, snapshot, nil)
		writeCatalogMutationResult(w, result, submitErr)
		return
	}
	rows, err := s.db.Query(r.Context(), `select entity.public_id,template.template_key,template.canvas_width,template.canvas_height,
		template.image_scale,(select public_id from oss_files where id=template.background_file_id),
		(select revision.public_id from content_revisions revision where revision.id=entity.published_revision_id),
		(select count(*)::int from recipe_template_slots slot where slot.template_id=template.entity_id),
		coalesce(import_snapshot.id,''),coalesce(import_snapshot.revision_id,''),coalesce(import_snapshot.background_path,''),
		coalesce(import_snapshot.background_contains_ingredients,false),coalesce(import_snapshot.coordinate_space,''),
		coalesce(import_snapshot.image_pixels,'{}'::jsonb),coalesce(import_snapshot.content_rect,'{}'::jsonb)
		from recipe_layout_templates template join catalog_entities entity on entity.id=template.entity_id
		left join recipe_template_import_snapshots import_snapshot on import_snapshot.id=template.import_snapshot_id and exists(select 1 from catalog_import_revisions source_revision join mods source_mod on source_mod.id=source_revision.mod_id where source_revision.id=import_snapshot.revision_id and source_revision.is_active and source_revision.status in ('ready','partial') and source_mod.review_status='approved')
		where template.recipe_type_id=$1 and entity.status='active' and `+publicCatalogEntitySQL("entity", "recipe_template")+`
		order by template.template_key`, typeEntity.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read templates")
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var publicID, key, importSnapshotID, importRevisionID, backgroundPath, coordinateSpace string
		var width, height, scale, count int
		var backgroundContainsIngredients bool
		var backgroundID sql.NullString
		var revisionID sql.NullString
		var imagePixels, contentRect []byte
		if err = rows.Scan(&publicID, &key, &width, &height, &scale, &backgroundID, &revisionID, &count,
			&importSnapshotID, &importRevisionID, &backgroundPath, &backgroundContainsIngredients, &coordinateSpace,
			&imagePixels, &contentRect); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to decode templates")
			return
		}
		backgroundURL := ""
		if backgroundID.Valid || importSnapshotID != "" {
			backgroundURL = "/api/v1/recipe-templates/" + publicID + "/background"
		}
		source := "manual"
		backgroundSource := "none"
		var importMetadata any
		if backgroundID.Valid {
			backgroundSource = "upload"
		}
		if importSnapshotID != "" {
			if !revisionID.Valid {
				source = "import"
			}
			if !backgroundID.Valid {
				backgroundSource = "import"
			}
			importMetadata = map[string]any{"snapshotId": importSnapshotID, "revisionId": importRevisionID,
				"backgroundPath": backgroundPath, "backgroundContainsIngredients": backgroundContainsIngredients,
				"coordinateSpace": coordinateSpace, "imagePixels": json.RawMessage(imagePixels), "contentRect": json.RawMessage(contentRect)}
		}
		items = append(items, map[string]any{"publicId": publicID, "templateKey": key, "canvas": map[string]any{"width": width, "height": height, "imageScale": scale},
			"backgroundFileId": nullableCatalogString(backgroundID), "backgroundUrl": backgroundURL, "backgroundSource": backgroundSource,
			"publishedRevisionId": nullableCatalogString(revisionID), "slotCount": count, "source": source, "importMetadata": importMetadata})
	}
	if err = rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read templates")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": len(items)})
}

func normalizeCatalogTemplateEdit(edit *catalogRecipeTemplateEdit) error {
	var err error
	edit.TemplateKey, err = canonicalCatalogString(edit.TemplateKey, 256)
	if err != nil {
		return err
	}
	edit.DefaultLocale, edit.Localizations, err = normalizeCatalogLocalizations(edit.DefaultLocale, edit.Localizations)
	if err != nil {
		return err
	}
	edit.Definition = nonNilJSONObject(edit.Definition)
	return validateCatalogTemplate(edit)
}

func (s *Server) catalogRecipeTemplateDetail(w http.ResponseWriter, r *http.Request) {
	entity, err := s.catalogEditorEntityByPublicID(r.Context(), r.PathValue("publicId"), "recipe_template")
	if errors.Is(err, errCatalogEditorNotFound) || err == nil && entity.Status == "archived" && r.Method == http.MethodGet {
		writeError(w, http.StatusNotFound, "recipe template not found")
		return
	}
	if err != nil {
		logCatalogEditorReadFailure("recipe template entity", err)
		writeError(w, http.StatusInternalServerError, "failed to read recipe template")
		return
	}
	var typeID int64
	if err = s.db.QueryRow(r.Context(), `select recipe_type_id from recipe_layout_templates where entity_id=$1`, entity.ID).Scan(&typeID); errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "recipe template not found")
		return
	}
	if err != nil {
		logCatalogEditorReadFailure("recipe template parent", err)
		writeError(w, http.StatusInternalServerError, "failed to read recipe template")
		return
	}
	if r.Method == http.MethodGet {
		active, activeErr := s.catalogRecipeTypeIsActive(r.Context(), typeID)
		if activeErr != nil {
			logCatalogEditorReadFailure("recipe template parent activity", activeErr)
			writeError(w, http.StatusInternalServerError, "failed to read recipe type")
			return
		}
		if !active {
			writeError(w, http.StatusNotFound, "recipe template not found")
			return
		}
	}
	if r.Method == http.MethodGet && !s.requirePublicCatalogEntity(w, r, entity) {
		return
	}

	if r.Method == http.MethodPut {
		var edit catalogRecipeTemplateEdit
		if decodeJSON(r, &edit) != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		if err = normalizeCatalogTemplateEdit(&edit); err != nil {
			writeError(w, catalogEditorHTTPStatus(err), catalogEditorErrorMessage(err))
			return
		}
		var typePublicID string
		if err = s.db.QueryRow(r.Context(), `select public_id from catalog_entities where id=$1 and entity_type='recipe_type'`, typeID).Scan(&typePublicID); err != nil {
			writeError(w, http.StatusNotFound, "recipe type not found")
			return
		}
		snapshot := catalogEditorSnapshot{Operation: "edit", Reason: edit.Reason, Kind: "recipe_template", EntityID: entity.ID,
			PublicID: entity.PublicID, ParentEntityID: typeID, ParentPublicID: typePublicID,
			DefaultLocale: edit.DefaultLocale, Localizations: edit.Localizations, Template: &edit}
		result, submitErr := s.submitCatalogEditorMutation(r, snapshot, edit.BaseRevisionID)
		writeCatalogMutationResult(w, result, submitErr)
		return
	}
	if r.Method == http.MethodDelete {
		s.deleteCatalogEntity(w, r, entity, "recipe_template")
		return
	}
	var typePublicID, key, importSnapshotID, importRevisionID, backgroundPath, coordinateSpace string
	var width, height, scale int
	var backgroundContainsIngredients bool
	var backgroundID sql.NullString
	var definition, imagePixels, contentRect []byte
	if err = s.db.QueryRow(r.Context(), `select type_entity.public_id,template.template_key,template.canvas_width,template.canvas_height,
		template.image_scale,(select public_id from oss_files where id=template.background_file_id),template.definition,coalesce(import_snapshot.id,''),
		coalesce(import_snapshot.revision_id,''),coalesce(import_snapshot.background_path,''),
		coalesce(import_snapshot.background_contains_ingredients,false),coalesce(import_snapshot.coordinate_space,''),
		coalesce(import_snapshot.image_pixels,'{}'::jsonb),coalesce(import_snapshot.content_rect,'{}'::jsonb)
		from recipe_layout_templates template join catalog_entities type_entity on type_entity.id=template.recipe_type_id
		left join recipe_template_import_snapshots import_snapshot on import_snapshot.id=template.import_snapshot_id and exists(select 1 from catalog_import_revisions source_revision join mods source_mod on source_mod.id=source_revision.mod_id where source_revision.id=import_snapshot.revision_id and source_revision.is_active and source_revision.status in ('ready','partial') and source_mod.review_status='approved')
		where template.entity_id=$1`, entity.ID).
		Scan(&typePublicID, &key, &width, &height, &scale, &backgroundID, &definition, &importSnapshotID,
			&importRevisionID, &backgroundPath, &backgroundContainsIngredients, &coordinateSpace, &imagePixels, &contentRect); errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "recipe template not found")
		return
	}
	if err != nil {
		logCatalogEditorReadFailure("recipe template definition", err)
		writeError(w, http.StatusInternalServerError, "failed to read recipe template")
		return
	}
	slots, slotErr := s.catalogTemplateSlotRows(r.Context(), entity.ID)
	if slotErr != nil {
		logCatalogEditorReadFailure("recipe template slots", slotErr)
		writeError(w, http.StatusInternalServerError, "failed to read recipe template slots")
		return
	}
	localizations, localeErr := s.catalogLocalizationRows(r.Context(), entity.ID)
	if localeErr != nil {
		logCatalogEditorReadFailure("recipe template localizations", localeErr)
		writeError(w, http.StatusInternalServerError, "failed to read localizations")
		return
	}
	defaultLocale := entity.DefaultLocale
	if entity.PublishedRevisionID == nil && len(localizations) == 0 {
		localizations, defaultLocale = catalogImportedEditorLocalizations(nil, defaultLocale, key)
	}
	backgroundURL := ""
	if backgroundID.Valid || importSnapshotID != "" {
		backgroundURL = "/api/v1/recipe-templates/" + entity.PublicID + "/background"
	}
	source := "manual"
	backgroundSource := "none"
	var importMetadata any
	if backgroundID.Valid {
		backgroundSource = "upload"
	}
	if importSnapshotID != "" {
		if entity.PublishedRevisionID == nil {
			source = "import"
		}
		if !backgroundID.Valid {
			backgroundSource = "import"
		}
		importMetadata = map[string]any{"snapshotId": importSnapshotID, "revisionId": importRevisionID,
			"backgroundPath": backgroundPath, "backgroundContainsIngredients": backgroundContainsIngredients,
			"coordinateSpace": coordinateSpace, "imagePixels": json.RawMessage(imagePixels), "contentRect": json.RawMessage(contentRect)}
	}
	publishedRevisionPublicID, revisionErr := revisionPublicIDValue(r.Context(), s.db, entity.PublishedRevisionID)
	if revisionErr != nil {
		logCatalogEditorReadFailure("recipe template published revision", revisionErr)
		writeError(w, http.StatusInternalServerError, "failed to resolve published revision")
		return
	}
	reviewStatus, reviewErr := s.catalogPendingReviewStatus(r.Context(), entity.ID)
	if reviewErr != nil {
		logCatalogEditorReadFailure("recipe template review status", reviewErr)
		writeError(w, http.StatusInternalServerError, "failed to read review status")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"publicId": entity.PublicID, "recipeTypePublicId": typePublicID, "templateKey": key,
		"canvas": map[string]any{"width": width, "height": height, "imageScale": scale}, "backgroundFileId": nullableCatalogString(backgroundID),
		"backgroundUrl": backgroundURL, "backgroundSource": backgroundSource, "definition": json.RawMessage(definition), "slots": slots, "defaultLocale": defaultLocale,
		"localizations": localizations, "publishedRevisionId": publishedRevisionPublicID,
		"reviewStatus": reviewStatus, "source": source, "importMetadata": importMetadata})
}
