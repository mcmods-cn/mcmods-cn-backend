package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
)

type catalogDeleteRequest struct {
	BaseRevisionID *string `json:"baseRevisionId"`
	Reason         string  `json:"reason"`
}

func (s *Server) catalogResources(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	kindCode := strings.TrimSpace(r.URL.Query().Get("kindCode"))
	if kindCode == "" {
		kindCode = strings.TrimSpace(r.URL.Query().Get("kind"))
	}
	registry := strings.TrimSpace(r.URL.Query().Get("registry"))
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
	var total int
	if err := s.db.QueryRow(r.Context(), `with `+latestGlobalExportScopeCTE+`, `+latestGlobalResourceSnapshotCTE+`
		select count(*)::int from game_resources resource
		join catalog_entities entity on entity.id=resource.entity_id
		left join latest_resource_snapshots imported on imported.resource_id=resource.entity_id
		where entity.status='active' and ($1='' or resource.kind_code=$1) and ($3='' or resource.namespace=$3) and
		($2='' or entity.public_id=$2 or resource.canonical_id ilike '%'||$2||'%' or exists(select 1 from content_localizations localization
		 where localization.catalog_entity_id=entity.id and localization.name ilike '%'||$2||'%')
		 or coalesce(imported.names,'{}'::jsonb)::text ilike '%'||$2||'%')`, kindCode, query, registry).Scan(&total); err != nil {
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
		coalesce(owner.project_code,''),coalesce(owner.slug,''),coalesce(owner.primary_name,''),coalesce(imported.icon_path,'')
		from game_resources resource join catalog_entities entity on entity.id=resource.entity_id
		left join catalog_resource_definitions definition on definition.resource_id=resource.entity_id
		left join latest_resource_snapshots imported on imported.resource_id=resource.entity_id
		left join mods owner on owner.id=resource.owner_mod_id
		left join lateral (select candidate.* from content_localizations candidate where candidate.catalog_entity_id=entity.id
			 order by case candidate.locale when $4 then 0 when $5 then 1 when entity.default_locale then 2 when 'en-US' then 3 else 4 end limit 1) localization on true
		where entity.status='active' and ($1='' or resource.kind_code=$1) and ($3='' or resource.namespace=$3) and
		($2='' or entity.public_id=$2 or resource.canonical_id ilike '%'||$2||'%' or exists(select 1 from content_localizations candidate
		 where candidate.catalog_entity_id=entity.id and candidate.name ilike '%'||$2||'%')
		 or coalesce(imported.names,'{}'::jsonb)::text ilike '%'||$2||'%')
		order by resource.kind_code,resource.canonical_id limit $6 offset $7`, kindCode, query, registry, primary, secondary, limit, offset)
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
		if err = rows.Scan(&entityID, &publicID, &kind, &canonicalID, &namespace, &defaultLocale, &revisionID, &contentLocale, &name,
			&iconID, &renderID, &names, &provenances, &ownerPublicID, &ownerSiteID, &ownerName, &importedIconPath); err != nil {
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
		items = append(items, map[string]any{"entityId": publicID, "publicId": publicID, "id": canonicalID,
			"kind": kind, "kindCode": kind, "registry": namespace, "canonicalId": canonicalID, "names": json.RawMessage(names),
			"defaultLocale": defaultLocale, "publishedRevisionId": nullableCatalogString(revisionID), "locale": contentLocale,
			"name": name, "provenance": provenance, "iconUrl": iconURL, "renderUrl": renderURL,
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
	if err != nil || (entity.Status != "active" && currentClaims(r).Subject == 0) {
		writeError(w, http.StatusNotFound, "resource not found")
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
	if err != nil {
		writeError(w, http.StatusNotFound, "resource not found")
		return
	}
	localizations, err := s.catalogLocalizationRows(r.Context(), entity.ID)
	if err != nil {
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
	writeJSON(w, http.StatusOK, map[string]any{"publicId": entity.PublicID, "status": entity.Status, "defaultLocale": defaultLocale,
		"publishedRevisionId": revisionPublicIDValue(r.Context(), s.db, entity.PublishedRevisionID), "reviewStatus": s.catalogPendingReviewStatus(r.Context(), entity.ID),
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
		where entity.status='active' and ($1='' or tag.canonical_id ilike '%'||$1||'%' or exists(
		 select 1 from content_localizations localization where localization.catalog_entity_id=entity.id and localization.name ilike '%'||$1||'%'))`, query).Scan(&total); err != nil {
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
		  where snapshot.tag_id=tag.entity_id and revision.is_active and revision.status in ('ready','partial')),0))
		else (select count(*)::int from catalog_tag_members member where member.tag_id=tag.entity_id) end,
		coalesce(localization.locale,''),coalesce(localization.name,''),
		coalesce((select jsonb_object_agg(candidate.locale,candidate.name) from content_localizations candidate
		 where candidate.catalog_entity_id=entity.id and candidate.name<>''),'{}'::jsonb)
		from catalog_tags tag join catalog_entities entity on entity.id=tag.entity_id
		left join lateral (select candidate.locale,candidate.name from content_localizations candidate
		 where candidate.catalog_entity_id=entity.id order by case candidate.locale when $2 then 0 when $3 then 1
			 when entity.default_locale then 2 when 'en-US' then 3 else 4 end limit 1) localization on true
		where entity.status='active' and ($1='' or tag.canonical_id ilike '%'||$1||'%' or exists(
		 select 1 from content_localizations localization where localization.catalog_entity_id=entity.id and localization.name ilike '%'||$1||'%'))
		order by tag.registry,tag.canonical_id limit $4 offset $5`, query, primary, secondary, limit, offset)
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
		items = append(items, map[string]any{"entityId": publicID, "publicId": publicID, "registry": registry, "canonicalId": canonicalID,
			"defaultLocale": defaultLocale, "publishedRevisionId": nullableCatalogString(revisionID), "memberCount": count,
			"locale": contentLocale, "contentLocale": contentLocale, "name": name, "names": json.RawMessage(names)})
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
	 and revision.is_active and revision.status in ('ready','partial')
	 order by (snapshot.icon_path<>'') desc,coalesce(revision.activated_at,revision.created_at) desc limit 1) imported on true
	where member.preview_rank<=$2 order by member.tag_id,member.ordinal`, tagIDs, catalogTagPreviewLimit)
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
		where snapshot.tag_id=any($1::bigint[]) and revision.is_active and revision.status in ('ready','partial')
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
	 where snapshot.resource_id=resource.entity_id order by (snapshot.revision_id=member.revision_id) desc,
	 (snapshot.icon_path<>'') desc,snapshot.created_at desc limit 1) imported on true
	left join catalog_import_revisions revision on revision.id=imported.revision_id
	left join mods mod on mod.id=revision.mod_id
	where member.preview_rank<=$2 order by member.tag_id,member.ordinal`, fallbackIDs, catalogTagPreviewLimit)
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
			"entityId": publicID, "publicId": publicID, "id": canonicalID, "canonicalId": canonicalID,
			"kind": kindCode, "kindCode": kindCode, "registry": registry, "ordinal": ordinal,
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
	if err != nil || entity.Status == "archived" && r.Method == http.MethodGet {
		writeError(w, http.StatusNotFound, "tag not found")
		return
	}
	switch r.Method {
	case http.MethodGet:
		var registry, canonicalID string
		if err = s.db.QueryRow(r.Context(), `select registry,canonical_id from catalog_tags where entity_id=$1`, entity.ID).Scan(&registry, &canonicalID); err != nil {
			writeError(w, http.StatusNotFound, "tag not found")
			return
		}
		primary, secondary := s.catalogRequestedLocales(r)
		members, readErr := s.catalogTagMemberRows(r.Context(), entity.ID, primary, secondary, entity.PublishedRevisionID == nil)
		localizations, localeErr := s.catalogLocalizationRows(r.Context(), entity.ID)
		if readErr != nil || localeErr != nil {
			writeError(w, http.StatusInternalServerError, "failed to read tag")
			return
		}
		defaultLocale := entity.DefaultLocale
		if entity.PublishedRevisionID == nil && len(localizations) == 0 {
			localizations, defaultLocale = catalogImportedEditorLocalizations(nil, defaultLocale, canonicalID)
		}
		writeJSON(w, http.StatusOK, map[string]any{"publicId": entity.PublicID, "registry": registry, "canonicalId": canonicalID,
			"defaultLocale": defaultLocale, "publishedRevisionId": revisionPublicIDValue(r.Context(), s.db, entity.PublishedRevisionID),
			"reviewStatus": s.catalogPendingReviewStatus(r.Context(), entity.ID), "localizations": localizations,
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
	if err != nil || entity.Status == "archived" && r.Method == http.MethodGet {
		writeError(w, http.StatusNotFound, "recipe type not found")
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
		 where snapshot.recipe_type_id=type.entity_id and revision.is_active and revision.status in ('ready','partial')
		 order by coalesce(revision.activated_at,revision.created_at) desc,revision.created_at desc limit 1) imported on true
		where type.entity_id=$1`, entity.ID).
		Scan(&canonicalID, &definition, &importedNames); err != nil {
		writeError(w, http.StatusNotFound, "recipe type not found")
		return
	}
	var templateCount, recipeCount int
	_ = s.db.QueryRow(r.Context(), `select count(*)::int from recipe_layout_templates template join catalog_entities entity on entity.id=template.entity_id
		where template.recipe_type_id=$1 and entity.status='active'`, entity.ID).Scan(&templateCount)
	_ = s.db.QueryRow(r.Context(), `select count(*)::int from recipes recipe join catalog_entities entity on entity.id=recipe.entity_id
		where recipe.recipe_type_id=$1 and entity.status='active'`, entity.ID).Scan(&recipeCount)
	localizations, _ := s.catalogLocalizationRows(r.Context(), entity.ID)
	defaultLocale := entity.DefaultLocale
	if entity.PublishedRevisionID == nil && len(localizations) == 0 {
		localizations, defaultLocale = catalogImportedEditorLocalizations(importedNames, defaultLocale, canonicalID)
	}
	primary, secondary := s.catalogRequestedLocales(r)
	catalysts, _ := s.catalogRecipeTypeCatalysts(r.Context(), entity.ID, primary, secondary, entity.PublishedRevisionID == nil)
	writeJSON(w, http.StatusOK, map[string]any{"publicId": entity.PublicID, "canonicalId": canonicalID, "defaultLocale": defaultLocale,
		"publishedRevisionId": revisionPublicIDValue(r.Context(), s.db, entity.PublishedRevisionID), "reviewStatus": s.catalogPendingReviewStatus(r.Context(), entity.ID),
		"definition": json.RawMessage(definition), "localizations": localizations, "catalysts": catalysts,
		"templateCount": templateCount, "recipeCount": recipeCount})
}

func (s *Server) catalogRecipeTemplates(w http.ResponseWriter, r *http.Request) {
	typeEntity, err := s.catalogEditorEntityByPublicID(r.Context(), r.PathValue("typePublicId"), "recipe_type")
	if err != nil || typeEntity.Status != "active" {
		writeError(w, http.StatusNotFound, "recipe type not found")
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
		left join recipe_template_import_snapshots import_snapshot on import_snapshot.id=template.import_snapshot_id
		where template.recipe_type_id=$1 and entity.status='active'
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
	if err != nil || entity.Status == "archived" && r.Method == http.MethodGet {
		writeError(w, http.StatusNotFound, "recipe template not found")
		return
	}
	var typeID int64
	if err = s.db.QueryRow(r.Context(), `select recipe_type_id from recipe_layout_templates where entity_id=$1`, entity.ID).Scan(&typeID); err != nil && r.Method != http.MethodPut {
		writeError(w, http.StatusNotFound, "recipe template not found")
		return
	}
	if r.Method == http.MethodGet && !s.catalogRecipeTypeIsActive(r.Context(), typeID) {
		writeError(w, http.StatusNotFound, "recipe template not found")
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
		left join recipe_template_import_snapshots import_snapshot on import_snapshot.id=template.import_snapshot_id
		where template.entity_id=$1`, entity.ID).
		Scan(&typePublicID, &key, &width, &height, &scale, &backgroundID, &definition, &importSnapshotID,
			&importRevisionID, &backgroundPath, &backgroundContainsIngredients, &coordinateSpace, &imagePixels, &contentRect); err != nil {
		writeError(w, http.StatusNotFound, "recipe template not found")
		return
	}
	slots, _ := s.catalogTemplateSlotRows(r.Context(), entity.ID)
	localizations, _ := s.catalogLocalizationRows(r.Context(), entity.ID)
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
	writeJSON(w, http.StatusOK, map[string]any{"publicId": entity.PublicID, "recipeTypePublicId": typePublicID, "templateKey": key,
		"canvas": map[string]any{"width": width, "height": height, "imageScale": scale}, "backgroundFileId": nullableCatalogString(backgroundID),
		"backgroundUrl": backgroundURL, "backgroundSource": backgroundSource, "definition": json.RawMessage(definition), "slots": slots, "defaultLocale": defaultLocale,
		"localizations": localizations, "publishedRevisionId": revisionPublicIDValue(r.Context(), s.db, entity.PublishedRevisionID),
		"reviewStatus": s.catalogPendingReviewStatus(r.Context(), entity.ID), "source": source, "importMetadata": importMetadata})
}

func (s *Server) catalogRecipeSourceVersions(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	limit := boundedLimit(r.URL.Query().Get("limit"), 500, 1000)
	claims := currentClaims(r)
	rows, err := s.db.Query(r.Context(), `select version.public_id,mod.project_code,mod.slug,mod.primary_name,
		version.label,version.minecraft_versions,version.loaders,version.mod_version
		from mod_content_versions version join mods mod on mod.id=version.mod_id
		where version.status='active' and (mod.review_status='approved' or mod.created_by=$1)
		and ($2='' or mod.primary_name ilike '%'||$2||'%' or mod.secondary_name ilike '%'||$2||'%'
			or mod.slug ilike '%'||$2||'%' or version.label ilike '%'||$2||'%' or version.mod_version ilike '%'||$2||'%')
		order by lower(mod.primary_name),mod.id,version.updated_at desc,version.id desc limit $3`, claims.Subject, query, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read recipe source versions")
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var publicID, modPublicID, modSiteID, modName, label, modVersion string
		var minecraftVersions, loaders []string
		if err = rows.Scan(&publicID, &modPublicID, &modSiteID, &modName, &label, &minecraftVersions, &loaders, &modVersion); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to decode recipe source versions")
			return
		}
		items = append(items, map[string]any{"publicId": publicID, "modPublicId": modPublicID, "modSiteId": modSiteID,
			"modName": modName, "label": label, "minecraftVersions": minecraftVersions, "loaders": loaders, "modVersion": modVersion})
	}
	if err = rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read recipe source versions")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) createCatalogRecipe(w http.ResponseWriter, r *http.Request) {
	var edit catalogRecipeEdit
	if decodeJSON(r, &edit) != nil || edit.BaseRevisionID != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	typePublicID := strings.TrimSpace(r.PathValue("typePublicId"))
	if typePublicID == "" {
		typePublicID = strings.TrimSpace(edit.RecipeTypePublicID)
	}
	if typePublicID == "" || edit.RecipeTypePublicID != "" && !strings.EqualFold(typePublicID, edit.RecipeTypePublicID) {
		writeError(w, http.StatusUnprocessableEntity, "recipeTypePublicId is invalid")
		return
	}
	typeEntity, err := s.catalogEditorEntityByPublicID(r.Context(), typePublicID, "recipe_type")
	if err != nil || typeEntity.Status != "active" {
		writeError(w, http.StatusNotFound, "recipe type not found")
		return
	}
	edit.RecipeTypePublicID = typeEntity.PublicID
	if err = s.normalizeCatalogRecipeEdit(r.Context(), typeEntity.ID, &edit); err != nil {
		writeError(w, catalogEditorHTTPStatus(err), catalogEditorErrorMessage(err))
		return
	}
	if err = requireCatalogCreateDefaultLocalization(edit.DefaultLocale, edit.Localizations); err != nil {
		writeError(w, catalogEditorHTTPStatus(err), catalogEditorErrorMessage(err))
		return
	}
	identity := catalogEditorIdentityForRecipe(typeEntity.IdentityKey, edit.CanonicalSourceID)
	snapshot := catalogEditorSnapshot{Operation: "create", Reason: edit.Reason, Kind: "recipe", IdentityKey: identity.ID, PublicID: identity.PublicID,
		ParentEntityID: typeEntity.ID, ParentPublicID: typeEntity.PublicID,
		DefaultLocale: edit.DefaultLocale, Localizations: edit.Localizations, Recipe: &edit}
	result, submitErr := s.submitCatalogEditorMutation(r, snapshot, nil)
	writeCatalogMutationResult(w, result, submitErr)
}

func (s *Server) catalogRecipes(w http.ResponseWriter, r *http.Request) {
	typeEntity, err := s.catalogEditorEntityByPublicID(r.Context(), r.PathValue("typePublicId"), "recipe_type")
	if err != nil || typeEntity.Status != "active" {
		writeError(w, http.StatusNotFound, "recipe type not found")
		return
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	limit := boundedLimit(r.URL.Query().Get("limit"), 40, 100)
	offset := boundedOffset(r.URL.Query().Get("offset"))
	var total int
	if err = s.db.QueryRow(r.Context(), `select count(*)::int from recipes recipe
		join catalog_entities entity on entity.id=recipe.entity_id
		where recipe.recipe_type_id=$1 and entity.status='active' and ($2='' or coalesce(recipe.canonical_source_id,'') ilike '%'||$2||'%')`,
		typeEntity.ID, query).Scan(&total); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to count recipes")
		return
	}
	rows, err := s.db.Query(r.Context(), `select entity.public_id,coalesce(recipe.canonical_source_id,''),recipe.identity_source,
		(select revision.public_id from content_revisions revision where revision.id=entity.published_revision_id),
		coalesce(template_entity.public_id,imported_template_entity.public_id,''),
		coalesce(definition.definition,'{}'::jsonb),coalesce(observation.id,''),coalesce(observation.revision_id,''),
		case when definition.recipe_id is not null then (select count(*)::int from recipe_bindings binding where binding.recipe_id=recipe.entity_id)
		 else coalesce(observation.binding_count,0) end,
		coalesce((select jsonb_object_agg(localization.locale,localization.name) from content_localizations localization
		 where localization.catalog_entity_id=entity.id and localization.name<>''),'{}'::jsonb),definition.recipe_id is not null,
		coalesce(effective_source_version.public_id,''),coalesce(source_mod.project_code,''),coalesce(source_mod.slug,''),
		coalesce(source_mod.primary_name,''),coalesce(effective_source_version.label,''),
		coalesce(effective_source_version.minecraft_versions,'{}'::text[]),coalesce(effective_source_version.loaders,'{}'::text[]),
		coalesce(effective_source_version.mod_version,'')
		from recipes recipe join catalog_entities entity on entity.id=recipe.entity_id
		left join recipe_definitions definition on definition.recipe_id=recipe.entity_id
		left join catalog_entities template_entity on template_entity.id=definition.template_id
		left join lateral (select snapshot.*,version.public_id as source_version_public_id from recipe_import_snapshots snapshot
		 join catalog_import_revisions revision on revision.id=snapshot.revision_id
		 join mod_content_versions version on version.id=revision.target_version_id
		 where snapshot.recipe_id=recipe.entity_id and revision.is_active and revision.status in ('ready','partial')
		 order by coalesce(revision.activated_at,revision.created_at) desc,revision.created_at desc limit 1) observation on true
		left join recipe_template_import_snapshots import_template on import_template.id=observation.template_id
		left join catalog_entities imported_template_entity on imported_template_entity.id=import_template.canonical_template_id
		left join mod_content_versions canonical_source_version on canonical_source_version.id=definition.source_mod_content_version_id
		left join mod_content_versions effective_source_version on effective_source_version.public_id=
			case when definition.recipe_id is not null then canonical_source_version.public_id else observation.source_version_public_id end
		left join mods source_mod on source_mod.id=effective_source_version.mod_id
		where recipe.recipe_type_id=$1 and entity.status='active' and ($2='' or coalesce(recipe.canonical_source_id,'') ilike '%'||$2||'%')
		order by coalesce(recipe.canonical_source_id,''),entity.public_id limit $3 offset $4`, typeEntity.ID, query, limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read recipes")
		return
	}
	defer rows.Close()
	primary, secondary := s.catalogRequestedLocales(r)
	items := make([]map[string]any, 0, limit)
	for rows.Next() {
		var publicID, canonicalID, identitySource, templatePublicID, observationID, importRevisionID string
		var sourceVersionPublicID, sourceModPublicID, sourceModSiteID, sourceModName, sourceVersionLabel, sourceModVersion string
		var sourceMinecraftVersions, sourceLoaders []string
		var publishedRevisionID sql.NullString
		var definition, names []byte
		var bindingCount int
		var canonicalDefinition bool
		if err = rows.Scan(&publicID, &canonicalID, &identitySource, &publishedRevisionID, &templatePublicID, &definition,
			&observationID, &importRevisionID, &bindingCount, &names, &canonicalDefinition,
			&sourceVersionPublicID, &sourceModPublicID, &sourceModSiteID, &sourceModName, &sourceVersionLabel,
			&sourceMinecraftVersions, &sourceLoaders, &sourceModVersion); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to decode recipes")
			return
		}
		locale, name := catalogResolvedName(names, primary, secondary, "en-US")
		source := "canonical"
		if !canonicalDefinition && observationID != "" {
			source = "import"
		}
		var sourceVersion any
		if sourceVersionPublicID != "" {
			sourceVersion = map[string]any{"publicId": sourceVersionPublicID, "modPublicId": sourceModPublicID, "modSiteId": sourceModSiteID,
				"modName": sourceModName, "label": sourceVersionLabel, "minecraftVersions": sourceMinecraftVersions,
				"loaders": sourceLoaders, "modVersion": sourceModVersion}
		}
		items = append(items, map[string]any{"publicId": publicID, "canonicalSourceId": canonicalID,
			"identitySource": identitySource, "templatePublicId": templatePublicID, "publishedRevisionId": nullableCatalogString(publishedRevisionID),
			"definition": json.RawMessage(definition), "bindingCount": bindingCount, "source": source,
			"sourceVersionPublicId": sourceVersionPublicID, "sourceVersion": sourceVersion,
			"importRevisionId": importRevisionID, "locale": locale, "name": name, "names": json.RawMessage(names)})
	}
	if err = rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read recipes")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": total, "limit": limit, "offset": offset})
}

func (s *Server) normalizeCatalogRecipeEdit(ctx context.Context, typeID int64, edit *catalogRecipeEdit) error {
	var err error
	edit.TemplatePublicID, err = canonicalCatalogString(edit.TemplatePublicID, 32)
	if err != nil {
		return err
	}
	edit.SourceVersionPublicID, err = normalizeCatalogOptionalPublicID(edit.SourceVersionPublicID)
	if err != nil {
		return err
	}
	edit.DefaultLocale, edit.Localizations, err = normalizeCatalogLocalizations(edit.DefaultLocale, edit.Localizations)
	if err != nil {
		return err
	}
	// Recipe structure is represented by the selected template, normalized
	// bindings and candidates. Do not retain arbitrary client/exporter blobs.
	edit.Definition = map[string]any{}
	for slotKey, binding := range edit.Bindings {
		binding.Definition = map[string]any{}
		for index := range binding.Candidates {
			binding.Candidates[index].Definition = map[string]any{}
		}
		edit.Bindings[slotKey] = binding
	}
	rows, err := s.db.Query(ctx, `select slot.slot_key,slot.role from recipe_layout_templates template
		join catalog_entities entity on entity.id=template.entity_id and entity.status='active'
		join recipe_template_slots slot on slot.template_id=template.entity_id
		where entity.public_id=$1 and template.recipe_type_id=$2`, edit.TemplatePublicID, typeID)
	if err != nil {
		return err
	}
	defer rows.Close()
	roles := map[string]string{}
	for rows.Next() {
		var key, role string
		if err = rows.Scan(&key, &role); err != nil {
			return err
		}
		roles[key] = role
	}
	if len(roles) == 0 {
		return errCatalogEditorReference
	}
	return validateCatalogRecipeBindings(edit, roles)
}

func (s *Server) catalogRecipeDetail(w http.ResponseWriter, r *http.Request) {
	entity, err := s.catalogEditorEntityByPublicID(r.Context(), r.PathValue("publicId"), "recipe")
	if err != nil || entity.Status == "archived" && r.Method == http.MethodGet {
		writeError(w, http.StatusNotFound, "recipe not found")
		return
	}
	var typeID int64
	if err = s.db.QueryRow(r.Context(), `select recipe_type_id from recipes where entity_id=$1`, entity.ID).Scan(&typeID); err != nil && r.Method != http.MethodPut {
		writeError(w, http.StatusNotFound, "recipe not found")
		return
	}
	if r.Method == http.MethodGet && !s.catalogRecipeTypeIsActive(r.Context(), typeID) {
		writeError(w, http.StatusNotFound, "recipe not found")
		return
	}
	if r.Method == http.MethodPut {
		var edit catalogRecipeEdit
		if decodeJSON(r, &edit) != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		if err = s.normalizeCatalogRecipeEdit(r.Context(), typeID, &edit); err != nil {
			writeError(w, catalogEditorHTTPStatus(err), catalogEditorErrorMessage(err))
			return
		}
		var typePublicID string
		if err = s.db.QueryRow(r.Context(), `select public_id from catalog_entities where id=$1 and entity_type='recipe_type'`, typeID).Scan(&typePublicID); err != nil {
			writeError(w, http.StatusNotFound, "recipe type not found")
			return
		}
		snapshot := catalogEditorSnapshot{Operation: "edit", Reason: edit.Reason, Kind: "recipe", EntityID: entity.ID, PublicID: entity.PublicID,
			ParentEntityID: typeID, ParentPublicID: typePublicID,
			DefaultLocale: edit.DefaultLocale, Localizations: edit.Localizations, Recipe: &edit}
		result, submitErr := s.submitCatalogEditorMutation(r, snapshot, edit.BaseRevisionID)
		writeCatalogMutationResult(w, result, submitErr)
		return
	}
	if r.Method == http.MethodDelete {
		s.deleteCatalogEntity(w, r, entity, "recipe")
		return
	}
	var typePublicID, templatePublicID, canonicalID, observationID, importRevisionID, importTemplateKey string
	var sourceVersionPublicID, sourceModPublicID, sourceModSiteID, sourceModName, sourceVersionLabel, sourceModVersion string
	var sourceMinecraftVersions, sourceLoaders []string
	var canonicalDefinition bool
	var definition []byte
	if err = s.db.QueryRow(r.Context(), `select type_entity.public_id,coalesce(template_entity.public_id,imported_template_entity.public_id,''),coalesce(recipe.canonical_source_id,''),
		coalesce(definition.definition,'{}'::jsonb),coalesce(observation.id,''),coalesce(observation.revision_id,''),
		coalesce(import_template.source_template_id,''),definition.recipe_id is not null,
		coalesce(effective_source_version.public_id,''),coalesce(source_mod.project_code,''),coalesce(source_mod.slug,''),
		coalesce(source_mod.primary_name,''),coalesce(effective_source_version.label,''),
		coalesce(effective_source_version.minecraft_versions,'{}'::text[]),coalesce(effective_source_version.loaders,'{}'::text[]),
		coalesce(effective_source_version.mod_version,'')
		from recipes recipe join catalog_entities type_entity on type_entity.id=recipe.recipe_type_id
		left join recipe_definitions definition on definition.recipe_id=recipe.entity_id
		left join catalog_entities template_entity on template_entity.id=definition.template_id
		left join lateral (select snapshot.*,version.public_id as source_version_public_id from recipe_import_snapshots snapshot
		 join catalog_import_revisions revision on revision.id=snapshot.revision_id
		 join mod_content_versions version on version.id=revision.target_version_id
			where snapshot.recipe_id=recipe.entity_id and revision.is_active and revision.status in ('ready','partial')
			order by coalesce(revision.activated_at,revision.created_at) desc,revision.created_at desc limit 1) observation on true
		left join recipe_template_import_snapshots import_template on import_template.id=observation.template_id
		left join catalog_entities imported_template_entity on imported_template_entity.id=import_template.canonical_template_id
		left join mod_content_versions canonical_source_version on canonical_source_version.id=definition.source_mod_content_version_id
		left join mod_content_versions effective_source_version on effective_source_version.public_id=
			case when definition.recipe_id is not null then canonical_source_version.public_id else observation.source_version_public_id end
		left join mods source_mod on source_mod.id=effective_source_version.mod_id
		where recipe.entity_id=$1`, entity.ID).
		Scan(&typePublicID, &templatePublicID, &canonicalID, &definition, &observationID, &importRevisionID, &importTemplateKey, &canonicalDefinition,
			&sourceVersionPublicID, &sourceModPublicID, &sourceModSiteID, &sourceModName, &sourceVersionLabel,
			&sourceMinecraftVersions, &sourceLoaders, &sourceModVersion); err != nil {
		writeError(w, http.StatusNotFound, "recipe not found")
		return
	}
	bindings, bindingErr := s.catalogRecipeBindingRows(r.Context(), entity.ID)
	if bindingErr == nil && len(bindings) == 0 && observationID != "" {
		bindings, bindingErr = s.catalogImportedRecipeBindingRows(r.Context(), observationID)
	}
	if bindingErr != nil {
		writeError(w, http.StatusInternalServerError, "failed to read recipe bindings")
		return
	}
	localizations, _ := s.catalogLocalizationRows(r.Context(), entity.ID)
	defaultLocale := entity.DefaultLocale
	if entity.PublishedRevisionID == nil && len(localizations) == 0 {
		fallbackName := canonicalID
		if fallbackName == "" {
			fallbackName = entity.PublicID
		}
		localizations, defaultLocale = catalogImportedEditorLocalizations(nil, defaultLocale, fallbackName)
	}
	source := "canonical"
	if !canonicalDefinition && observationID != "" {
		source = "import"
	}
	var sourceVersion any
	if sourceVersionPublicID != "" {
		sourceVersion = map[string]any{"publicId": sourceVersionPublicID, "modPublicId": sourceModPublicID, "modSiteId": sourceModSiteID,
			"modName": sourceModName, "label": sourceVersionLabel, "minecraftVersions": sourceMinecraftVersions,
			"loaders": sourceLoaders, "modVersion": sourceModVersion}
	}
	writeJSON(w, http.StatusOK, map[string]any{"publicId": entity.PublicID, "recipeTypePublicId": typePublicID,
		"templatePublicId": templatePublicID, "sourceVersionPublicId": sourceVersionPublicID, "sourceVersion": sourceVersion,
		"canonicalSourceId": canonicalID, "definition": json.RawMessage(definition), "bindings": bindings,
		"defaultLocale": defaultLocale, "localizations": localizations, "publishedRevisionId": revisionPublicIDValue(r.Context(), s.db, entity.PublishedRevisionID),
		"reviewStatus": s.catalogPendingReviewStatus(r.Context(), entity.ID), "source": source,
		"importRevisionId": importRevisionID, "importTemplateKey": importTemplateKey})
}

func (s *Server) deleteCatalogEntity(w http.ResponseWriter, r *http.Request, entity catalogEditorEntity, kind string) {
	var request catalogDeleteRequest
	if decodeJSON(r, &request) != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	snapshot := catalogEditorSnapshot{Operation: "delete", Reason: request.Reason, Kind: kind, EntityID: entity.ID, PublicID: entity.PublicID,
		DefaultLocale: entity.DefaultLocale}
	result, err := s.submitCatalogEditorMutation(r, snapshot, request.BaseRevisionID)
	writeCatalogMutationResult(w, result, err)
}

func writeCatalogMutationResult(w http.ResponseWriter, result catalogEditResult, err error) {
	if err != nil {
		writeError(w, catalogEditorHTTPStatus(err), catalogEditorErrorMessage(err))
		return
	}
	writeJSON(w, http.StatusAccepted, result)
}

func nullableCatalogString(value sql.NullString) any {
	if !value.Valid {
		return nil
	}
	return value.String
}

func catalogResolvedName(raw []byte, primary, secondary, defaultLocale string) (string, string) {
	var encoded map[string]string
	if len(raw) == 0 || json.Unmarshal(raw, &encoded) != nil {
		return "", ""
	}
	names := make(map[string]string, len(encoded))
	available := make([]string, 0, len(encoded))
	for rawLocale, name := range encoded {
		locale := normalizeContentLocale(rawLocale)
		if locale == "" || strings.TrimSpace(name) == "" {
			continue
		}
		names[locale] = name
		available = append(available, locale)
	}
	resolution := resolveContentLocale(primary, secondary, defaultLocale, available)
	return resolution.ResolvedLocale, names[resolution.ResolvedLocale]
}

func (s *Server) catalogRequestedLocales(r *http.Request) (string, string) {
	primary, secondary := s.requestContentLocales(r)
	if requested := normalizeContentLocale(r.URL.Query().Get("locale")); requested != "" {
		primary = requested
	}
	if requested := normalizeContentLocale(r.URL.Query().Get("secondaryLocale")); requested != "" {
		secondary = requested
	}
	return primary, secondary
}

func (s *Server) catalogRecipeTypeIsActive(ctx context.Context, typeID int64) bool {
	var active bool
	return s.db.QueryRow(ctx, `select exists(select 1 from catalog_entities
		where id=$1 and entity_type='recipe_type' and status='active' and archived_at is null)`, typeID).Scan(&active) == nil && active
}

func (s *Server) catalogLocalizationRows(ctx context.Context, entityID int64) ([]map[string]any, error) {
	rows, err := s.db.Query(ctx, `select locale,name,summary,content_markdown,provenance,source_locale,
		(select task_uid from ai_tasks where id=content_localizations.ai_task_id),revision_no,
		editable,review_status,
		(select revision.public_id from content_revisions revision where revision.id=content_localizations.published_revision_id)
		from content_localizations where catalog_entity_id=$1 order by locale`, entityID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []map[string]any{}
	for rows.Next() {
		var locale, name, summary, markdown, provenance, sourceLocale, reviewStatus string
		var aiTaskID sql.NullString
		var publishedRevisionID sql.NullString
		var revisionNo int64
		var editable bool
		if err = rows.Scan(&locale, &name, &summary, &markdown, &provenance, &sourceLocale, &aiTaskID, &revisionNo, &editable,
			&reviewStatus, &publishedRevisionID); err != nil {
			return nil, err
		}
		result = append(result, map[string]any{"locale": locale, "name": name, "summary": summary, "contentMarkdown": markdown,
			"provenance": provenance, "sourceLocale": sourceLocale, "aiTaskId": nullableCatalogString(aiTaskID), "revisionNo": revisionNo,
			"editable": editable, "reviewStatus": reviewStatus, "publishedRevisionId": nullableCatalogString(publishedRevisionID)})
	}
	return result, rows.Err()
}

func (s *Server) catalogPendingReviewStatus(ctx context.Context, entityID int64) string {
	var status string
	err := s.db.QueryRow(ctx, `select status from change_requests where entity_id=$1 order by id desc limit 1`, entityID).Scan(&status)
	if err != nil {
		return "approved"
	}
	return status
}

func (s *Server) catalogTagMemberRows(ctx context.Context, tagID int64, primary, secondary string, allowImportFallback bool) ([]map[string]any, error) {
	rows, err := s.db.Query(ctx, `select entity.id,entity.public_id,resource.kind_code,resource.canonical_id,resource.namespace,member.ordinal,
		entity.default_locale,(select public_id from oss_files where id=definition.icon_file_id),coalesce(imported.revision_id,''),coalesce(imported.icon_path,''),coalesce(imported.mod_site_id,''),
		coalesce((select jsonb_object_agg(localization.locale,localization.name) from content_localizations localization
		 where localization.catalog_entity_id=entity.id and localization.name<>''),imported.names,'{}'::jsonb)
		from catalog_tag_members member join game_resources resource on resource.entity_id=member.resource_id
		left join catalog_resource_definitions definition on definition.resource_id=resource.entity_id
		left join lateral (select snapshot.names,snapshot.revision_id,snapshot.icon_path,mod.slug mod_site_id
		 from resource_import_snapshots snapshot join catalog_import_revisions revision on revision.id=snapshot.revision_id
		 join mods mod on mod.id=revision.mod_id where snapshot.resource_id=resource.entity_id
		 and revision.is_active and revision.status in ('ready','partial')
		 order by (snapshot.icon_path<>'') desc,coalesce(revision.activated_at,revision.created_at) desc limit 1) imported on true
		join catalog_entities entity on entity.id=resource.entity_id where member.tag_id=$1 order by member.ordinal`, tagID)
	if err != nil {
		return nil, err
	}
	items := []map[string]any{}
	for rows.Next() {
		var entityID int64
		var publicID, kindCode, canonicalID, registry, defaultLocale, revisionID, iconPath, modSiteID string
		var iconID sql.NullString
		var names []byte
		var ordinal int
		if err = rows.Scan(&entityID, &publicID, &kindCode, &canonicalID, &registry, &ordinal, &defaultLocale, &iconID, &revisionID, &iconPath, &modSiteID, &names); err != nil {
			return nil, err
		}
		locale, name := catalogResolvedName(names, primary, secondary, defaultLocale)
		iconURL := ""
		if iconID.Valid {
			iconURL = "/api/v1/catalog/resources/" + publicID + "/icon"
		}
		items = append(items, map[string]any{"entityId": publicID, "publicId": publicID, "id": canonicalID, "kind": kindCode, "kindCode": kindCode,
			"registry": registry, "canonicalId": canonicalID, "ordinal": ordinal, "locale": locale, "name": name,
			"names": json.RawMessage(names), "iconFileId": nullableCatalogString(iconID), "iconUrl": iconURL,
			"revisionId": revisionID, "iconPath": iconPath, "modSiteId": modSiteID})
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	if len(items) > 0 || !allowImportFallback {
		if err = s.decorateResourceVersionRows(ctx, items, primary, secondary); err != nil {
			return nil, err
		}
		return items, nil
	}
	rows, err = s.db.Query(ctx, `with selected as (
		select snapshot.id,snapshot.revision_id from tag_import_snapshots snapshot
		join catalog_import_revisions revision on revision.id=snapshot.revision_id
		where snapshot.tag_id=$1 and revision.is_active and revision.status in ('ready','partial')
	), selected_members as (
		select distinct on(member.resource_id) selected.revision_id,member.resource_id,member.ordinal
		from selected join tag_import_members member on member.tag_snapshot_id=selected.id
		order by member.resource_id,member.ordinal
	)
	select entity.id,entity.public_id,resource.kind_code,resource.canonical_id,resource.namespace,member.ordinal,entity.default_locale,
		(select public_id from oss_files where id=definition.icon_file_id),coalesce(resource_snapshot.revision_id,''),coalesce(resource_snapshot.icon_path,''),coalesce(mod.slug,''),
		coalesce((select jsonb_object_agg(localization.locale,localization.name)
		 from content_localizations localization where localization.catalog_entity_id=entity.id and localization.name<>''),
		 resource_snapshot.names,'{}'::jsonb)
	from selected_members member
	join game_resources resource on resource.entity_id=member.resource_id
	join catalog_entities entity on entity.id=resource.entity_id
	left join catalog_resource_definitions definition on definition.resource_id=resource.entity_id
	left join lateral (select candidate.names,candidate.revision_id,candidate.icon_path from resource_import_snapshots candidate where candidate.resource_id=resource.entity_id
	 order by (candidate.revision_id=member.revision_id) desc,(candidate.icon_path<>'') desc,candidate.created_at desc limit 1) resource_snapshot on true
	left join catalog_import_revisions revision on revision.id=resource_snapshot.revision_id
	left join mods mod on mod.id=revision.mod_id
	order by member.ordinal`, tagID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var entityID int64
		var publicID, kindCode, canonicalID, registry, defaultLocale, revisionID, iconPath, modSiteID string
		var iconID sql.NullString
		var names []byte
		var ordinal int
		if err = rows.Scan(&entityID, &publicID, &kindCode, &canonicalID, &registry, &ordinal, &defaultLocale, &iconID, &revisionID, &iconPath, &modSiteID, &names); err != nil {
			return nil, err
		}
		locale, name := catalogResolvedName(names, primary, secondary, defaultLocale)
		iconURL := ""
		if iconID.Valid {
			iconURL = "/api/v1/catalog/resources/" + publicID + "/icon"
		}
		items = append(items, map[string]any{"entityId": publicID, "publicId": publicID, "id": canonicalID, "kind": kindCode, "kindCode": kindCode,
			"registry": registry, "canonicalId": canonicalID, "ordinal": ordinal, "locale": locale, "name": name,
			"names": json.RawMessage(names), "iconFileId": nullableCatalogString(iconID), "iconUrl": iconURL,
			"revisionId": revisionID, "iconPath": iconPath, "modSiteId": modSiteID, "source": "import"})
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if err = s.decorateResourceVersionRows(ctx, items, primary, secondary); err != nil {
		return nil, err
	}
	return items, nil
}

func (s *Server) catalogRecipeTypeCatalysts(ctx context.Context, typeID int64, primary, secondary string, allowImportFallback bool) ([]map[string]any, error) {
	rows, err := s.db.Query(ctx, `select entity.public_id,resource.kind_code,resource.canonical_id,resource.namespace,catalyst.ordinal,
		entity.default_locale,(select public_id from oss_files where id=definition.icon_file_id),
		coalesce((select jsonb_object_agg(localization.locale,localization.name) from content_localizations localization
		 where localization.catalog_entity_id=entity.id and localization.name<>''),'{}'::jsonb)
		from recipe_type_catalysts catalyst join game_resources resource on resource.entity_id=catalyst.resource_id
		left join catalog_resource_definitions definition on definition.resource_id=resource.entity_id
		join catalog_entities entity on entity.id=resource.entity_id where catalyst.recipe_type_id=$1 order by catalyst.ordinal`, typeID)
	if err != nil {
		return nil, err
	}
	items := []map[string]any{}
	for rows.Next() {
		var publicID, kindCode, canonicalID, registry, defaultLocale string
		var iconID sql.NullString
		var names []byte
		var ordinal int
		if err = rows.Scan(&publicID, &kindCode, &canonicalID, &registry, &ordinal, &defaultLocale, &iconID, &names); err != nil {
			return nil, err
		}
		locale, name := catalogResolvedName(names, primary, secondary, defaultLocale)
		iconURL := ""
		if iconID.Valid {
			iconURL = "/api/v1/catalog/resources/" + publicID + "/icon"
		}
		items = append(items, map[string]any{"publicId": publicID, "id": canonicalID, "kind": kindCode, "kindCode": kindCode,
			"registry": registry, "canonicalId": canonicalID, "ordinal": ordinal, "locale": locale, "name": name,
			"names": json.RawMessage(names), "iconFileId": nullableCatalogString(iconID), "iconUrl": iconURL})
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	if len(items) > 0 || !allowImportFallback {
		return items, nil
	}
	var raw []byte
	var revisionID string
	err = s.db.QueryRow(ctx, `select snapshot.catalysts,snapshot.revision_id from recipe_type_import_snapshots snapshot
		join catalog_import_revisions revision on revision.id=snapshot.revision_id
		where snapshot.recipe_type_id=$1 and revision.is_active and revision.status in ('ready','partial')
		order by coalesce(revision.activated_at,revision.created_at) desc,revision.created_at desc limit 1`, typeID).Scan(&raw, &revisionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return items, nil
	}
	if err != nil {
		return nil, err
	}
	return s.decorateCatalysts(ctx, raw, revisionID)
}

func (s *Server) catalogTemplateSlotRows(ctx context.Context, templateID int64) ([]map[string]any, error) {
	rows, err := s.db.Query(ctx, `select slot_key,role,output_index,ordinal,x::float8,y::float8,width::float8,height::float8,definition
		from recipe_template_slots where template_id=$1 order by ordinal`, templateID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var key, role string
		var outputIndex sql.NullInt64
		var ordinal int
		var x, y, width, height float64
		var definition []byte
		if err = rows.Scan(&key, &role, &outputIndex, &ordinal, &x, &y, &width, &height, &definition); err != nil {
			return nil, err
		}
		var outputIndexValue any
		if outputIndex.Valid {
			outputIndexValue = outputIndex.Int64
		}
		items = append(items, map[string]any{"slotKey": key, "role": role, "outputIndex": outputIndexValue, "ordinal": ordinal,
			"rect": map[string]any{"x": x, "y": y, "width": width, "height": height}, "definition": json.RawMessage(definition)})
	}
	return items, rows.Err()
}

func (s *Server) catalogRecipeBindingRows(ctx context.Context, recipeID int64) (map[string]any, error) {
	rows, err := s.db.Query(ctx, `select binding.id,slot.slot_key,binding.definition
		from recipe_bindings binding join recipe_template_slots slot on slot.id=binding.template_slot_id
		where binding.recipe_id=$1 order by binding.ordinal`, recipeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[string]any{}
	for rows.Next() {
		var bindingID int64
		var slotKey string
		var definition []byte
		if err = rows.Scan(&bindingID, &slotKey, &definition); err != nil {
			return nil, err
		}
		candidates, candidateErr := s.catalogRecipeCandidateRows(ctx, bindingID)
		if candidateErr != nil {
			return nil, candidateErr
		}
		result[slotKey] = map[string]any{"definition": json.RawMessage(definition), "candidates": candidates}
	}
	return result, rows.Err()
}

func (s *Server) catalogImportedRecipeBindingRows(ctx context.Context, snapshotID string) (map[string]any, error) {
	rows, err := s.db.Query(ctx, `select binding.id,binding.source_slot_id,coalesce(nullif(binding.semantic_role,''),slot.role,''),
		candidate.alternative_index,coalesce(resource_entity.public_id,''),coalesce(candidate.raw_resource_id,''),candidate.amount,
		coalesce(candidate.chance,candidate.chance_percent/100.0),candidate.byproduct,
		coalesce(icon_file.public_id,''),coalesce(imported.revision_id,''),coalesce(imported.icon_path,'')
		from recipe_import_bindings binding
		join recipe_template_import_slots slot on slot.id=binding.template_slot_id
		left join recipe_import_binding_candidates candidate on candidate.binding_id=binding.id
		left join game_resources resource on resource.entity_id=candidate.resource_id
		left join catalog_entities resource_entity on resource_entity.id=resource.entity_id
		left join catalog_resource_definitions definition on definition.resource_id=resource.entity_id
		left join oss_files icon_file on icon_file.id=definition.icon_file_id and icon_file.status='active'
		left join lateral (select source.revision_id,source.icon_path from resource_import_snapshots source
		 where source.resource_id=resource.entity_id order by (source.icon_path<>'') desc,source.created_at desc limit 1) imported on true
		where binding.recipe_snapshot_id=$1 order by binding.ordinal,candidate.alternative_index`, snapshotID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[string]any{}
	for rows.Next() {
		var bindingID, slotKey, role, resourcePublicID, rawResourceID, iconFileID, revisionID, iconPath string
		var candidateIndex sql.NullInt64
		var amount, probability sql.NullFloat64
		var byproduct sql.NullBool
		if err = rows.Scan(&bindingID, &slotKey, &role, &candidateIndex, &resourcePublicID, &rawResourceID,
			&amount, &probability, &byproduct, &iconFileID, &revisionID, &iconPath); err != nil {
			return nil, err
		}
		binding, exists := result[slotKey].(map[string]any)
		if !exists {
			binding = map[string]any{"role": role, "definition": map[string]any{}, "candidates": []map[string]any{}}
			result[slotKey] = binding
		}
		if !candidateIndex.Valid {
			continue
		}
		candidate := map[string]any{"resourcePublicId": resourcePublicID, "rawResourceId": rawResourceID,
			"amount": nullableSQLFloat(amount), "probability": nullableSQLFloat(probability), "byproduct": byproduct.Valid && byproduct.Bool,
			"definition": map[string]any{}}
		if rawResourceID == "" {
			candidate["iconUrl"] = catalogRecipeResourceIconURL(resourcePublicID, iconFileID, revisionID, iconPath)
		}
		candidates, _ := binding["candidates"].([]map[string]any)
		binding["candidates"] = append(candidates, candidate)
	}
	return result, rows.Err()
}

func (s *Server) catalogRecipeCandidateRows(ctx context.Context, bindingID int64) ([]map[string]any, error) {
	rows, err := s.db.Query(ctx, `select entity.public_id,resource.kind_code,resource.canonical_id,resource.resolved,candidate.amount::float8,
		candidate.probability::float8,candidate.byproduct,candidate.definition,
		coalesce(icon_file.public_id,''),coalesce(imported.revision_id,''),coalesce(imported.icon_path,'')
		from recipe_binding_candidates candidate join game_resources resource on resource.entity_id=candidate.resource_id
		join catalog_entities entity on entity.id=resource.entity_id
		left join catalog_resource_definitions definition on definition.resource_id=resource.entity_id
		left join oss_files icon_file on icon_file.id=definition.icon_file_id and icon_file.status='active'
		left join lateral (select source.revision_id,source.icon_path from resource_import_snapshots source
		 where source.resource_id=resource.entity_id order by (source.icon_path<>'') desc,source.created_at desc limit 1) imported on true
		where candidate.binding_id=$1 order by candidate.candidate_index`, bindingID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var publicID, kindCode, canonicalID, iconFileID, revisionID, iconPath string
		var amount float64
		var resolved bool
		var probability sql.NullFloat64
		var byproduct bool
		var definition []byte
		if err = rows.Scan(&publicID, &kindCode, &canonicalID, &resolved, &amount, &probability, &byproduct, &definition,
			&iconFileID, &revisionID, &iconPath); err != nil {
			return nil, err
		}
		var probabilityValue any
		if probability.Valid {
			probabilityValue = probability.Float64
		}
		item := map[string]any{"resourcePublicId": publicID, "kindCode": kindCode, "canonicalId": canonicalID,
			"unresolved": !resolved, "rawResourceId": catalogUnresolvedRawID(resolved, canonicalID),
			"amount": amount, "probability": probabilityValue, "byproduct": byproduct, "definition": json.RawMessage(definition)}
		if resolved {
			item["iconUrl"] = catalogRecipeResourceIconURL(publicID, iconFileID, revisionID, iconPath)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func catalogRecipeResourceIconURL(publicID, iconFileID, revisionID, iconPath string) string {
	if publicID != "" && (iconFileID != "" || iconPath != "") {
		return "/api/v1/catalog/resources/" + url.PathEscape(publicID) + "/icon"
	}
	if revisionID != "" && iconPath != "" {
		return "/api/v1/export-revisions/" + url.PathEscape(revisionID) + "/assets/content?path=" + url.QueryEscape(iconPath)
	}
	return ""
}

func catalogUnresolvedRawID(resolved bool, canonicalID string) string {
	if resolved {
		return ""
	}
	return canonicalID
}
